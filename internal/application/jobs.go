package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Fila de trabalho assíncrono.
//
// A fila é persistida na tabela `jobs`, de modo que sobrevive ao reinício do
// processo (o que estava `running` volta para `queued` no boot). Os casos de
// uso enfileiram trabalho por `Enqueue`; o `Worker` consome a fila, tenta de
// novo em falha transitória com backoff exponencial e encerra de forma graciosa
// quando o contexto do processo é cancelado.
//
// Nada aqui conhece as regras de negócio de cada tipo de trabalho: o worker só
// transporta `domain.Job` até o handler registrado para o `kind`.

const (
	// DefaultJobMaxAttempts é o número de tentativas de um trabalho enfileirado
	// sem configuração explícita.
	DefaultJobMaxAttempts = 3
	// DefaultWorkerInterval é o intervalo de varredura da fila.
	DefaultWorkerInterval = 30 * time.Second
	// DefaultWorkerWorkers é quantos trabalhos o worker busca por varredura.
	DefaultWorkerBatchSize = 20
	// DefaultWorkerWorkers é quantos trabalhos rodam em paralelo por varredura.
	DefaultWorkerWorkers = 4
	// workerOrphanLimit limita quantos trabalhos órfãos o boot recupera por vez.
	workerOrphanLimit = 500

	// jobBackoffBase e jobBackoffMax definem o adiamento da retentativa:
	// 30s · 2^(tentativa-1), com teto de 30min.
	jobBackoffBase = 30 * time.Second
	jobBackoffMax  = 30 * time.Minute
)

// jobStamp é o formato de horário gravado na tabela `jobs`.
const jobStampLayout = "2006-01-02 15:04:05"

// ── Enfileiramento ─────────────────────────────────────────────────

// Enqueue acrescenta um trabalho à fila e devolve o registro criado.
//
// O payload é serializado em JSON; `delay` adia a primeira execução (zero
// executa assim que houver worker). O trabalho nasce `queued`, sem tentativas,
// com `DefaultJobMaxAttempts` tentativas e horários em UTC.
func Enqueue(ctx context.Context, deps Deps, kind domain.JobKind, payload any, delay time.Duration) (*domain.Job, error) {
	return EnqueueWithAttempts(ctx, deps, kind, payload, delay, DefaultJobMaxAttempts)
}

// EnqueueWithAttempts é o `Enqueue` com o número de tentativas configurado por
// chamada. Valor não positivo cai no padrão (`DefaultJobMaxAttempts`).
func EnqueueWithAttempts(ctx context.Context, deps Deps, kind domain.JobKind, payload any, delay time.Duration, maxAttempts int) (*domain.Job, error) {
	if deps.Repos.Jobs == nil {
		return nil, errors.New("fila de trabalho indisponível")
	}
	if deps.IDs == nil || deps.Clock == nil {
		return nil, errors.New("fila de trabalho sem gerador de id ou relógio")
	}
	if kind == "" {
		return nil, errors.New("tipo de trabalho obrigatório")
	}
	if maxAttempts <= 0 {
		maxAttempts = DefaultJobMaxAttempts
	}
	if delay < 0 {
		delay = 0
	}

	var body *string
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("serializar payload do trabalho: %w", err)
		}
		encoded := string(raw)
		body = &encoded
	}

	now := deps.Clock.Now().UTC()
	stamp := format.FormatUTCStamp(now)
	job := &domain.Job{
		ID:          deps.IDs.New(),
		CreatedAt:   stamp,
		UpdatedAt:   stamp,
		Kind:        kind,
		Status:      domain.JobStatusQueued,
		Attempts:    0,
		MaxAttempts: maxAttempts,
		Payload:     body,
		ScheduledAt: format.FormatUTCStamp(now.Add(delay)),
	}
	if err := deps.Repos.Jobs.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("enfileirar trabalho: %w", err)
	}
	return job, nil
}

// RetryJob devolve um trabalho falho para a fila, com as tentativas zeradas,
// para que o operador o reexecute pelo painel. Trabalho em execução ou já
// concluído não é reexecutado: a baixa de um lançamento não pode acontecer duas
// vezes.
func RetryJob(ctx context.Context, deps Deps, jobID string) (*domain.Job, error) {
	if deps.Repos.Jobs == nil {
		return nil, errors.New("fila de trabalho indisponível")
	}
	if deps.Clock == nil {
		return nil, errors.New("fila de trabalho sem relógio")
	}
	job, err := deps.Repos.Jobs.ByID(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("buscar trabalho: %w", err)
	}
	switch job.Status {
	case domain.JobStatusFailed:
	case domain.JobStatusRunning:
		return nil, fmt.Errorf("trabalho %s está em execução; aguarde o término", job.ID)
	default:
		return nil, fmt.Errorf("trabalho %s não está falho (situação: %s)", job.ID, string(job.Status))
	}

	now := format.FormatUTCStamp(deps.Clock.Now().UTC())
	job.Status = domain.JobStatusQueued
	job.Attempts = 0
	job.Error = nil
	job.Result = nil
	job.StartedAt = nil
	job.FinishedAt = nil
	job.ScheduledAt = now
	job.UpdatedAt = now
	if err := deps.Repos.Jobs.Update(ctx, job); err != nil {
		return nil, fmt.Errorf("reenfileirar trabalho: %w", err)
	}
	return job, nil
}

// ── Worker ─────────────────────────────────────────────────────────

// JobHandler executa um trabalho da fila. Erro devolvido significa falha: o
// worker decide, pelas tentativas do próprio trabalho, se adia ou encerra.
type JobHandler func(ctx context.Context, deps Deps, job domain.Job) error

// WorkerOptions ajusta o laço do worker.
type WorkerOptions struct {
	// Interval é o intervalo entre varreduras da fila.
	Interval time.Duration
	// BatchSize é quantos trabalhos cada varredura busca na fila.
	BatchSize int
	// Workers é quantos trabalhos rodam ao mesmo tempo dentro de uma varredura.
	// Trabalhos da fila são independentes e dominados por I/O (provedor de IA,
	// WhatsApp), então rodar em paralelo multiplica a vazão. Com SQLite o banco
	// atende uma conexão por vez: ali o ganho vem do I/O externo, não da query.
	Workers int
}

// Worker consome a fila persistida. Handlers são registrados por tipo de
// trabalho; um tipo sem handler falha definitivamente com motivo claro.
type Worker struct {
	deps     Deps
	interval time.Duration
	batch    int
	workers  int

	mu       sync.RWMutex
	handlers map[domain.JobKind]JobHandler

	// inflight acompanha o ciclo em andamento para que o encerramento drene o
	// trabalho em execução antes de `Run` retornar.
	inflight sync.WaitGroup
	// busy garante um ciclo por vez — varredura lenta não sobrepõe a seguinte.
	busy chan struct{}
}

// NewWorker monta o worker da fila. Opções zeradas usam os padrões.
func NewWorker(deps Deps, opts WorkerOptions) *Worker {
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultWorkerInterval
	}
	batch := opts.BatchSize
	if batch <= 0 {
		batch = DefaultWorkerBatchSize
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = DefaultWorkerWorkers
	}
	if workers > batch {
		workers = batch
	}
	worker := &Worker{
		deps:     deps,
		interval: interval,
		batch:    batch,
		workers:  workers,
		handlers: make(map[domain.JobKind]JobHandler),
		busy:     make(chan struct{}, 1),
	}
	return worker
}

// Register associa um tipo de trabalho ao handler que o executa.
func (w *Worker) Register(kind domain.JobKind, handler JobHandler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handlers[kind] = handler
}

// Run consome a fila até o contexto ser cancelado. O cancelamento não
// interrompe o trabalho em execução: o laço para de pegar trabalho novo, espera
// o ciclo corrente terminar (segurando o trabalho que já estava rodando) e só
// então retorna.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			break
		}
		w.runCycle(ctx)
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	w.inflight.Wait()
}

// runCycle dispara um ciclo de varredura em segundo plano, sem sobrepor o
// anterior. O ciclo fica contabilizado em `inflight` para o encerramento
// gracioso.
func (w *Worker) runCycle(ctx context.Context) {
	select {
	case w.busy <- struct{}{}:
	default:
		return
	}
	w.inflight.Add(1)
	go func() {
		defer w.inflight.Done()
		defer func() { <-w.busy }()
		if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			w.log().Warn("fila: varredura falhou", "error", err.Error())
		}
	}()
}

// RunOnce executa uma varredura completa da fila e devolve quantos trabalhos
// foram tratados. Sem trabalho pendente devolve (0, nil).
//
// O contexto cancelado impede pegar trabalho novo, mas o trabalho já iniciado
// roda até o fim (o handler recebe um contexto desligado do cancelamento) —
// é o que garante o encerramento gracioso.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	if w.deps.Repos.Jobs == nil {
		return 0, errors.New("fila de trabalho indisponível")
	}
	// Cancelamento em curso não pega trabalho novo: o que já estava rodando
	// termina, e o resto continua na fila para o próximo ciclo.
	if ctx.Err() != nil {
		return 0, nil
	}
	due, err := w.due(ctx)
	if err != nil {
		return 0, err
	}

	return w.dispatch(ctx, due), nil
}

// dispatch roda os trabalhos da varredura num pool de goroutines alimentado por
// canal: os trabalhos são independentes entre si e o gargalo é I/O externo.
//
// O contexto entregue ao handler é desligado do cancelamento
// (`context.WithoutCancel`): o encerramento para de pegar trabalho novo, mas o
// que já começou termina — é o que torna o desligamento gracioso.
func (w *Worker) dispatch(ctx context.Context, jobs []domain.Job) int {
	if len(jobs) == 0 {
		return 0
	}

	workers := w.workers
	if workers > len(jobs) {
		workers = len(jobs)
	}

	queue := make(chan domain.Job)
	var handled atomic.Int64
	var group sync.WaitGroup

	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for job := range queue {
				w.runJob(context.WithoutCancel(ctx), job)
				handled.Add(1)
			}
		}()
	}

	for _, job := range jobs {
		if ctx.Err() != nil {
			break
		}
		queue <- job
	}
	close(queue)
	group.Wait()

	return int(handled.Load())
}

// RecoverOrphans devolve para `queued` os trabalhos que ficaram `running` por
// causa de um reinício do processo. Deve ser chamado no boot, antes de `Run`.
func (w *Worker) RecoverOrphans(ctx context.Context) (int, error) {
	if w.deps.Repos.Jobs == nil {
		return 0, errors.New("fila de trabalho indisponível")
	}
	orphans, err := w.deps.Repos.Jobs.ListByStatus(ctx, domain.JobStatusRunning, workerOrphanLimit)
	if err != nil {
		return 0, fmt.Errorf("listar trabalhos em execução: %w", err)
	}
	stamp := format.FormatUTCStamp(w.now().UTC())
	recovered := 0
	for _, job := range orphans {
		job.Status = domain.JobStatusQueued
		job.StartedAt = nil
		job.UpdatedAt = stamp
		job.Error = nil
		if err := w.deps.Repos.Jobs.Update(ctx, &job); err != nil {
			return recovered, fmt.Errorf("devolver trabalho %s para a fila: %w", job.ID, err)
		}
		recovered++
		w.log().Info("fila: trabalho órfão devolvido à fila", "job", job.ID, "kind", string(job.Kind))
	}
	return recovered, nil
}

// due lista os trabalhos prontos para rodar: `queued` e com `scheduled_at` já
// vencido no relógio do sistema. A consulta ordena por `scheduled_at`, então
// basta parar no primeiro que ainda não venceu.
func (w *Worker) due(ctx context.Context) ([]domain.Job, error) {
	queued, err := w.deps.Repos.Jobs.ListByStatus(ctx, domain.JobStatusQueued, w.batch)
	if err != nil {
		return nil, fmt.Errorf("listar trabalhos na fila: %w", err)
	}
	now := w.now().UTC()
	ready := make([]domain.Job, 0, len(queued))
	for _, job := range queued {
		if !w.jobDue(job, now) {
			break
		}
		ready = append(ready, job)
	}
	return ready, nil
}

// jobDue informa se o trabalho já pode rodar. Horário ausente ou ilegível conta
// como vencido: melhor tentar do que deixar o trabalho preso.
func (w *Worker) jobDue(job domain.Job, now time.Time) bool {
	if job.ScheduledAt == "" {
		return true
	}
	scheduled, err := time.ParseInLocation(jobStampLayout, job.ScheduledAt, time.UTC)
	if err != nil {
		return true
	}
	return !scheduled.After(now)
}

// runJob reivindica o trabalho (marca `running`, incrementa tentativas), chama
// o handler e grava o desfecho.
func (w *Worker) runJob(ctx context.Context, job domain.Job) {
	handler, registered := w.handler(job.Kind)

	claimedAt := format.FormatUTCStamp(w.now().UTC())
	job.Status = domain.JobStatusRunning
	job.Attempts++
	job.StartedAt = &claimedAt
	job.UpdatedAt = claimedAt
	job.Error = nil
	job.Result = nil
	if err := w.deps.Repos.Jobs.Update(ctx, &job); err != nil {
		w.log().Error("fila: não foi possível marcar trabalho como em execução", "job", job.ID, "error", err.Error())
		return
	}

	if !registered {
		w.finishFailed(ctx, job, fmt.Sprintf("nenhum handler registrado para o tipo %q", job.Kind))
		return
	}

	err := w.execute(ctx, job, handler)
	if err == nil {
		w.finishDone(ctx, job)
		return
	}
	w.finishError(ctx, job, err)
}

// execute isola o handler para que um pânico vire falha do trabalho, e não a
// queda do processo.
func (w *Worker) execute(ctx context.Context, job domain.Job, handler JobHandler) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("pânico ao executar o trabalho: %v", recovered)
		}
	}()
	return handler(ctx, w.deps, job)
}

// finishDone grava o desfecho de sucesso.
func (w *Worker) finishDone(ctx context.Context, job domain.Job) {
	finishedAt := format.FormatUTCStamp(w.now().UTC())
	job.Status = domain.JobStatusDone
	job.FinishedAt = &finishedAt
	job.UpdatedAt = finishedAt
	job.Error = nil
	job.Result = jobResult(job)
	if err := w.deps.Repos.Jobs.Update(ctx, &job); err != nil {
		w.log().Error("fila: não foi possível gravar trabalho concluído", "job", job.ID, "error", err.Error())
		return
	}
	w.log().Info("fila: trabalho concluído", "job", job.ID, "kind", string(job.Kind), "attempts", job.Attempts)
}

// finishError decide entre adiar a próxima tentativa e encerrar o trabalho.
func (w *Worker) finishError(ctx context.Context, job domain.Job, failure error) {
	reason := failure.Error()
	job.Error = &reason

	if !job.CanRetry() {
		w.finishFailed(ctx, job, reason)
		return
	}

	delay := jobBackoff(job.Attempts)
	now := w.now().UTC()
	job.Status = domain.JobStatusQueued
	job.StartedAt = nil
	job.ScheduledAt = format.FormatUTCStamp(now.Add(delay))
	job.UpdatedAt = format.FormatUTCStamp(now)
	job.Result = nil
	if err := w.deps.Repos.Jobs.Update(ctx, &job); err != nil {
		w.log().Error("fila: não foi possível reagendar trabalho", "job", job.ID, "error", err.Error())
		return
	}
	w.log().Warn("fila: trabalho adiado para nova tentativa",
		"job", job.ID, "kind", string(job.Kind), "attempts", job.Attempts, "retry_in", delay.String())
}

// finishFailed encerra o trabalho em definitivo, com o motivo gravado.
func (w *Worker) finishFailed(ctx context.Context, job domain.Job, reason string) {
	finishedAt := format.FormatUTCStamp(w.now().UTC())
	job.Status = domain.JobStatusFailed
	job.FinishedAt = &finishedAt
	job.UpdatedAt = finishedAt
	job.StartedAt = nil
	job.Result = nil
	failure := reason
	job.Error = &failure
	if err := w.deps.Repos.Jobs.Update(ctx, &job); err != nil {
		w.log().Error("fila: não foi possível gravar falha do trabalho", "job", job.ID, "error", err.Error())
		return
	}
	w.log().Warn("fila: trabalho falhou em definitivo",
		"job", job.ID, "kind", string(job.Kind), "attempts", job.Attempts, "reason", reason)
}

func (w *Worker) handler(kind domain.JobKind) (JobHandler, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	handler, ok := w.handlers[kind]
	return handler, ok
}

func (w *Worker) now() time.Time {
	if w.deps.Clock != nil {
		return w.deps.Clock.Now()
	}
	return time.Now()
}

func (w *Worker) log() *slog.Logger {
	if w.deps.Log != nil {
		return w.deps.Log
	}
	return slog.Default()
}

// jobBackoff devolve o adiamento da retentativa: 30s · 2^(tentativas-1), com
// teto de 30min. A primeira falha espera 30s; a segunda, 60s; e assim por
// diante.
func jobBackoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := jobBackoffBase
	for i := 1; i < attempts && delay < jobBackoffMax; i++ {
		delay *= 2
	}
	if delay > jobBackoffMax {
		return jobBackoffMax
	}
	return delay
}

// jobResult resume a execução bem-sucedida. O dado de negócio fica nas tabelas
// próprias de cada área; aqui fica só o rastro da fila.
func jobResult(job domain.Job) *string {
	raw, err := json.Marshal(map[string]any{
		"status":   "done",
		"attempts": job.Attempts,
	})
	if err != nil {
		return nil
	}
	text := string(raw)
	return &text
}
