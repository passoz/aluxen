package application_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// ── Apoio ──────────────────────────────────────────────────────────
//
// A fila é exercitada sobre um repositório em memória: o que importa aqui é o
// comportamento do worker (tentativas, backoff, encerramento), não o dialeto
// do banco — este já é coberto pelos testes do pacote de infraestrutura.

type jobsClock struct{ now time.Time }

func (c *jobsClock) Now() time.Time { return c.now }

func (c *jobsClock) advance(duration time.Duration) { c.now = c.now.Add(duration) }

type jobsIDs struct{ next int }

func (g *jobsIDs) New() string {
	g.next++
	return fmt.Sprintf("job-%d", g.next)
}

type jobsPayload struct {
	ReceiptID string `json:"receipt_id"`
}

// jobsRepo é um domain.JobRepository em memória, com a mesma ordenação da
// consulta real (`scheduled_at`, depois `created_at`) e cópias defensivas.
type jobsRepo struct {
	mu    sync.Mutex
	items map[string]*domain.Job
	order []string
}

func newJobsRepo() *jobsRepo { return &jobsRepo{items: make(map[string]*domain.Job)} }

func (r *jobsRepo) Create(_ context.Context, job *domain.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[job.ID]; exists {
		return fmt.Errorf("trabalho %s já existe", job.ID)
	}
	r.items[job.ID] = jobsClone(job)
	r.order = append(r.order, job.ID)
	return nil
}

func (r *jobsRepo) ByID(_ context.Context, id string) (*domain.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, exists := r.items[id]
	if !exists {
		return nil, fmt.Errorf("trabalho %s não encontrado", id)
	}
	return jobsClone(job), nil
}

func (r *jobsRepo) ListByStatus(_ context.Context, status domain.JobStatus, limit int) ([]domain.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := make([]domain.Job, 0, len(r.order))
	for _, id := range r.order {
		job := r.items[id]
		if job.Status != status {
			continue
		}
		all = append(all, *jobsClone(job))
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].ScheduledAt != all[j].ScheduledAt {
			return all[i].ScheduledAt < all[j].ScheduledAt
		}
		return all[i].CreatedAt < all[j].CreatedAt
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func (r *jobsRepo) List(_ context.Context, limit int) ([]domain.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	all := make([]domain.Job, 0, len(r.order))
	for _, id := range r.order {
		all = append(all, *jobsClone(r.items[id]))
	}
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func (r *jobsRepo) Update(_ context.Context, job *domain.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[job.ID]; !exists {
		return fmt.Errorf("trabalho %s não encontrado", job.ID)
	}
	r.items[job.ID] = jobsClone(job)
	return nil
}

func (r *jobsRepo) stored(t *testing.T, id string) *domain.Job {
	t.Helper()
	job, err := r.ByID(context.Background(), id)
	require.NoError(t, err)
	return job
}

func (r *jobsRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.order)
}

func jobsClone(job *domain.Job) *domain.Job {
	clone := *job
	clone.Payload = jobsCloneString(job.Payload)
	clone.Result = jobsCloneString(job.Result)
	clone.Error = jobsCloneString(job.Error)
	clone.StartedAt = jobsCloneString(job.StartedAt)
	clone.FinishedAt = jobsCloneString(job.FinishedAt)
	return &clone
}

func jobsCloneString(value *string) *string {
	if value == nil {
		return nil
	}
	text := *value
	return &text
}

type jobsFixture struct {
	deps  application.Deps
	repo  *jobsRepo
	clock *jobsClock
	now   time.Time
}

func newJobsFixture(t *testing.T) *jobsFixture {
	t.Helper()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	repo := newJobsRepo()
	clock := &jobsClock{now: now}
	return &jobsFixture{
		now:   now,
		repo:  repo,
		clock: clock,
		deps: application.Deps{
			Repos: application.Repositories{Jobs: repo},
			Clock: clock,
			IDs:   &jobsIDs{},
			Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
	}
}

func (f *jobsFixture) enqueue(t *testing.T, kind domain.JobKind, payload any, delay time.Duration) *domain.Job {
	t.Helper()
	job, err := application.Enqueue(context.Background(), f.deps, kind, payload, delay)
	require.NoError(t, err)
	return job
}

func (f *jobsFixture) worker(opts application.WorkerOptions) *application.Worker {
	return application.NewWorker(f.deps, opts)
}

// ── Enfileiramento ─────────────────────────────────────────────────

func TestJobsEnqueueCreatesQueuedJob(t *testing.T) {
	f := newJobsFixture(t)

	job, err := application.Enqueue(context.Background(), f.deps, domain.JobKindPaymentProof,
		jobsPayload{ReceiptID: "rec-1"}, 2*time.Minute)
	require.NoError(t, err)

	assert.NotEmpty(t, job.ID)
	assert.Equal(t, domain.JobKindPaymentProof, job.Kind)
	assert.Equal(t, domain.JobStatusQueued, job.Status)
	assert.Equal(t, 0, job.Attempts)
	assert.Equal(t, application.DefaultJobMaxAttempts, job.MaxAttempts)
	assert.Equal(t, format.FormatUTCStamp(f.now), job.CreatedAt)
	assert.Equal(t, format.FormatUTCStamp(f.now), job.UpdatedAt)
	assert.Equal(t, format.FormatUTCStamp(f.now.Add(2*time.Minute)), job.ScheduledAt)
	assert.Nil(t, job.StartedAt)
	assert.Nil(t, job.FinishedAt)
	assert.Nil(t, job.Result)
	assert.Nil(t, job.Error)
	require.NotNil(t, job.Payload)
	var payload jobsPayload
	require.NoError(t, json.Unmarshal([]byte(*job.Payload), &payload))
	assert.Equal(t, "rec-1", payload.ReceiptID)

	stored := f.repo.stored(t, job.ID)
	assert.Equal(t, domain.JobStatusQueued, stored.Status)
	assert.Equal(t, *job.Payload, *stored.Payload)
}

func TestJobsEnqueueWithAttemptsAndNilPayload(t *testing.T) {
	f := newJobsFixture(t)

	job, err := application.EnqueueWithAttempts(context.Background(), f.deps, domain.JobKindCollection, nil, 0, 5)
	require.NoError(t, err)
	assert.Equal(t, 5, job.MaxAttempts)
	assert.Equal(t, format.FormatUTCStamp(f.now), job.ScheduledAt)
	assert.Nil(t, job.Payload)

	// Tentativas não positivas caem no padrão.
	def, err := application.EnqueueWithAttempts(context.Background(), f.deps, domain.JobKindCollection, nil, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, application.DefaultJobMaxAttempts, def.MaxAttempts)
}

func TestJobsEnqueueRejectsInvalidInput(t *testing.T) {
	f := newJobsFixture(t)

	_, err := application.Enqueue(context.Background(), f.deps, "", nil, 0)
	require.Error(t, err)

	withoutQueue := f.deps
	withoutQueue.Repos.Jobs = nil
	_, err = application.Enqueue(context.Background(), withoutQueue, domain.JobKindCollection, nil, 0)
	require.Error(t, err)
	assert.Equal(t, 0, f.repo.count())
}

// ── Execução ───────────────────────────────────────────────────────

func TestJobsWorkerRunsJobToCompletion(t *testing.T) {
	f := newJobsFixture(t)
	job := f.enqueue(t, domain.JobKindPaymentProof, jobsPayload{ReceiptID: "rec-9"}, 0)

	worker := f.worker(application.WorkerOptions{Interval: time.Millisecond})
	var received domain.Job
	worker.Register(domain.JobKindPaymentProof, func(_ context.Context, _ application.Deps, got domain.Job) error {
		assert.Equal(t, domain.JobStatusRunning, got.Status)
		assert.Equal(t, 1, got.Attempts)
		require.NotNil(t, got.StartedAt)
		received = got
		return nil
	})

	handled, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, handled)
	assert.Equal(t, job.ID, received.ID)

	stored := f.repo.stored(t, job.ID)
	assert.Equal(t, domain.JobStatusDone, stored.Status)
	assert.Equal(t, 1, stored.Attempts)
	require.NotNil(t, stored.StartedAt)
	require.NotNil(t, stored.FinishedAt)
	assert.Equal(t, format.FormatUTCStamp(f.now), *stored.FinishedAt)
	assert.Nil(t, stored.Error)
	require.NotNil(t, stored.Result)
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(*stored.Result), &result))
	assert.Equal(t, "done", result["status"])
	assert.Equal(t, float64(1), result["attempts"])
	require.NotNil(t, stored.Payload)
	assert.Equal(t, "rec-9", jobsDecodePayload(t, stored).ReceiptID)
}

func TestJobsWorkerRetriesWithBackoffThenFails(t *testing.T) {
	f := newJobsFixture(t)
	job := f.enqueue(t, domain.JobKindDocumentIntake, jobsPayload{ReceiptID: "intake-1"}, 0)

	worker := f.worker(application.WorkerOptions{Interval: time.Millisecond})
	attempts := 0
	worker.Register(domain.JobKindDocumentIntake, func(context.Context, application.Deps, domain.Job) error {
		attempts++
		return fmt.Errorf("leitura falhou")
	})

	// 1ª tentativa: falha transitória volta para a fila com 30s de espera.
	handled, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, handled)
	retried := f.repo.stored(t, job.ID)
	assert.Equal(t, domain.JobStatusQueued, retried.Status)
	assert.Equal(t, 1, retried.Attempts)
	require.NotNil(t, retried.Error)
	assert.Equal(t, "leitura falhou", *retried.Error)
	assert.Nil(t, retried.StartedAt)
	assert.Nil(t, retried.FinishedAt)
	assert.Equal(t, format.FormatUTCStamp(f.clock.now.Add(30*time.Second)), retried.ScheduledAt)

	// Ainda não venceu: nada roda.
	handled, err = worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, handled)
	assert.Equal(t, 1, attempts)

	// 2ª tentativa: 60s de espera.
	f.clock.advance(30 * time.Second)
	handled, err = worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, handled)
	retried = f.repo.stored(t, job.ID)
	assert.Equal(t, domain.JobStatusQueued, retried.Status)
	assert.Equal(t, 2, retried.Attempts)
	assert.Equal(t, format.FormatUTCStamp(f.clock.now.Add(60*time.Second)), retried.ScheduledAt)

	// Um segundo antes do vencimento, nada roda.
	f.clock.advance(59 * time.Second)
	handled, err = worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, handled)

	// 3ª tentativa esgota o limite: falha definitiva com o motivo preservado.
	f.clock.advance(1 * time.Second)
	handled, err = worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, handled)
	assert.Equal(t, 3, attempts)

	failed := f.repo.stored(t, job.ID)
	assert.Equal(t, domain.JobStatusFailed, failed.Status)
	assert.Equal(t, 3, failed.Attempts)
	require.NotNil(t, failed.FinishedAt)
	assert.Equal(t, format.FormatUTCStamp(f.clock.now), *failed.FinishedAt)
	require.NotNil(t, failed.Error)
	assert.Equal(t, "leitura falhou", *failed.Error)
	assert.Nil(t, failed.StartedAt)
	assert.Nil(t, failed.Result)
}

func TestJobsWorkerFailsKindWithoutHandler(t *testing.T) {
	f := newJobsFixture(t)
	job := f.enqueue(t, domain.JobKindWhatsAppSend, nil, 0)

	worker := f.worker(application.WorkerOptions{Interval: time.Millisecond})
	handled, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, handled)

	stored := f.repo.stored(t, job.ID)
	assert.Equal(t, domain.JobStatusFailed, stored.Status)
	require.NotNil(t, stored.Error)
	assert.Contains(t, *stored.Error, string(domain.JobKindWhatsAppSend))
	assert.Contains(t, *stored.Error, "handler")
	require.NotNil(t, stored.FinishedAt)
}

func TestJobsWorkerSkipsJobScheduledAhead(t *testing.T) {
	f := newJobsFixture(t)
	job := f.enqueue(t, domain.JobKindCollection, nil, time.Hour)

	worker := f.worker(application.WorkerOptions{Interval: time.Millisecond})
	called := false
	worker.Register(domain.JobKindCollection, func(context.Context, application.Deps, domain.Job) error {
		called = true
		return nil
	})

	handled, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, handled)
	assert.False(t, called, "trabalho adiado não deveria rodar")

	stored := f.repo.stored(t, job.ID)
	assert.Equal(t, domain.JobStatusQueued, stored.Status)
	assert.Equal(t, 0, stored.Attempts)

	f.clock.advance(time.Hour)
	handled, err = worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, handled)
	assert.True(t, called)
	assert.Equal(t, domain.JobStatusDone, f.repo.stored(t, job.ID).Status)
}

func TestJobsWorkerRunOnceWithoutPendingJobIsNoop(t *testing.T) {
	f := newJobsFixture(t)
	worker := f.worker(application.WorkerOptions{Interval: time.Millisecond})

	for i := 0; i < 2; i++ {
		handled, err := worker.RunOnce(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 0, handled)
	}
	assert.Equal(t, 0, f.repo.count())
}

// ── Recuperação e encerramento ─────────────────────────────────────

func TestJobsRecoverOrphansReturnsRunningJobsToQueue(t *testing.T) {
	f := newJobsFixture(t)
	startedAt := format.FormatUTCStamp(f.now.Add(-time.Minute))
	orphan := &domain.Job{
		ID:          "job-orfao",
		CreatedAt:   format.FormatUTCStamp(f.now.Add(-2 * time.Minute)),
		UpdatedAt:   startedAt,
		Kind:        domain.JobKindPaymentProof,
		Status:      domain.JobStatusRunning,
		Attempts:    1,
		MaxAttempts: 3,
		ScheduledAt: format.FormatUTCStamp(f.now.Add(-2 * time.Minute)),
		StartedAt:   &startedAt,
	}
	require.NoError(t, f.repo.Create(context.Background(), orphan))

	worker := f.worker(application.WorkerOptions{Interval: time.Millisecond})
	recovered, err := worker.RecoverOrphans(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, recovered)

	stored := f.repo.stored(t, orphan.ID)
	assert.Equal(t, domain.JobStatusQueued, stored.Status)
	assert.Nil(t, stored.StartedAt)

	// E o trabalho recuperado volta a rodar.
	worker.Register(domain.JobKindPaymentProof, func(context.Context, application.Deps, domain.Job) error {
		return nil
	})
	handled, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, handled)
	assert.Equal(t, domain.JobStatusDone, f.repo.stored(t, orphan.ID).Status)
}

func TestJobsWorkerRunDrainsRunningJobOnShutdown(t *testing.T) {
	f := newJobsFixture(t)
	job := f.enqueue(t, domain.JobKindPaymentProof, jobsPayload{ReceiptID: "rec-shutdown"}, 0)

	worker := f.worker(application.WorkerOptions{Interval: 5 * time.Millisecond})
	handlerStarted := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	worker.Register(domain.JobKindPaymentProof, func(ctx context.Context, _ application.Deps, _ domain.Job) error {
		once.Do(func() { close(handlerStarted) })
		<-release
		// O encerramento não pode abortar o trabalho em execução: o handler
		// recebe um contexto desligado do cancelamento do processo.
		if ctx.Err() != nil {
			return fmt.Errorf("contexto cancelado durante o trabalho: %v", ctx.Err())
		}
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		worker.Run(ctx)
	}()

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("o handler do trabalho não iniciou")
	}

	cancel()

	select {
	case <-stopped:
		t.Fatal("Run retornou antes de drenar o trabalho em execução")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Run não retornou depois que o trabalho em execução terminou")
	}

	stored := f.repo.stored(t, job.ID)
	assert.Equal(t, domain.JobStatusDone, stored.Status)
	require.NotNil(t, stored.FinishedAt)
	assert.Nil(t, stored.Error)
}

func TestJobsRetryJobRequeuesFailedJob(t *testing.T) {
	f := newJobsFixture(t)
	job := f.enqueue(t, domain.JobKindPaymentProof, jobsPayload{ReceiptID: "rec-retry"}, 0)

	worker := f.worker(application.WorkerOptions{Interval: time.Millisecond})
	worker.Register(domain.JobKindPaymentProof, func(context.Context, application.Deps, domain.Job) error {
		return fmt.Errorf("leitura falhou")
	})
	for i := 0; i < application.DefaultJobMaxAttempts; i++ {
		_, err := worker.RunOnce(context.Background())
		require.NoError(t, err)
		f.clock.advance(time.Hour)
	}
	require.Equal(t, domain.JobStatusFailed, f.repo.stored(t, job.ID).Status)

	requeued, err := application.RetryJob(context.Background(), f.deps, job.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobStatusQueued, requeued.Status)
	assert.Equal(t, 0, requeued.Attempts)
	assert.Nil(t, requeued.Error)
	assert.Nil(t, requeued.Result)
	assert.Nil(t, requeued.StartedAt)
	assert.Nil(t, requeued.FinishedAt)
	assert.Equal(t, format.FormatUTCStamp(f.clock.now), requeued.ScheduledAt)

	// Reexecuta com um handler que agora funciona.
	worker.Register(domain.JobKindPaymentProof, func(context.Context, application.Deps, domain.Job) error {
		return nil
	})
	handled, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, handled)
	assert.Equal(t, domain.JobStatusDone, f.repo.stored(t, job.ID).Status)
}

func TestJobsRetryJobRejectsNonFailedJob(t *testing.T) {
	f := newJobsFixture(t)
	job := f.enqueue(t, domain.JobKindPaymentProof, nil, 0)

	_, err := application.RetryJob(context.Background(), f.deps, job.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "não está falho")

	runningAt := format.FormatUTCStamp(f.now)
	require.NoError(t, f.repo.Update(context.Background(), &domain.Job{
		ID: job.ID, CreatedAt: job.CreatedAt, UpdatedAt: runningAt,
		Kind: job.Kind, Status: domain.JobStatusRunning,
		Attempts: 1, MaxAttempts: 3, ScheduledAt: job.ScheduledAt, StartedAt: &runningAt,
	}))
	_, err = application.RetryJob(context.Background(), f.deps, job.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "em execução")

	_, err = application.RetryJob(context.Background(), f.deps, "job-inexistente")
	require.Error(t, err)
}

func jobsDecodePayload(t *testing.T, job *domain.Job) jobsPayload {
	t.Helper()
	require.NotNil(t, job.Payload)
	var payload jobsPayload
	require.NoError(t, json.Unmarshal([]byte(*job.Payload), &payload))
	return payload
}
