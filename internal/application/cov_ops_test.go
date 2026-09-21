package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// ── Fila de trabalho (jobs.go) ─────────────────────────────────────

// copsFila monta um trabalho na fila em memória.
func copsFila(id string, kind domain.JobKind, status domain.JobStatus, attempts, maxAttempts int, scheduledAt string) domain.Job {
	return domain.Job{
		ID: id, Kind: kind, Status: status,
		Attempts: attempts, MaxAttempts: maxAttempts,
		ScheduledAt: scheduledAt,
	}
}

// copsWorker monta o worker do teste sobre as dependências do ambiente.
func copsWorker(env *covEnv, batch int) *Worker {
	return NewWorker(env.deps, WorkerOptions{Interval: time.Minute, BatchSize: batch})
}

// copsLogBuffer troca o logger do ambiente por um que guarda o texto.
func copsLogBuffer(env *covEnv) *bytes.Buffer {
	buffer := &bytes.Buffer{}
	env.deps.Log = slog.New(slog.NewTextHandler(buffer, nil))
	return buffer
}

// copsJobsSeletivo recusa a N-ésima gravação da fila, para provar o desfecho
// quando o banco recusa o resultado do trabalho.
type copsJobsSeletivo struct {
	*covJobs
	falharNaChamada int
	chamadas        int
}

func (r *copsJobsSeletivo) Update(ctx context.Context, job *domain.Job) error {
	r.chamadas++
	if r.chamadas == r.falharNaChamada {
		return errors.New("banco indisponível")
	}
	return r.covJobs.Update(ctx, job)
}

// copsWorkerSeletivo monta o worker sobre a fila com falha na N-ésima gravação.
func copsWorkerSeletivo(env *covEnv, falharNaChamada int, jobs ...domain.Job) (*Worker, *copsJobsSeletivo) {
	selective := &copsJobsSeletivo{covJobs: env.jobs, falharNaChamada: falharNaChamada}
	selective.items = append([]domain.Job(nil), jobs...)
	env.deps.Repos.Jobs = selective
	return copsWorker(env, 10), selective
}

// TestCopsEnqueueWithAttempts cobre o enfileiramento: validação das dependências,
// payload serializado, tentativas configuradas (ou padrão) e agendamento.
func TestCopsEnqueueWithAttempts(t *testing.T) {
	ctx := context.Background()

	t.Run("sem fila", func(t *testing.T) {
		env := newCovEnv(t)
		deps := env.deps
		deps.Repos.Jobs = nil
		job, err := EnqueueWithAttempts(ctx, deps, domain.JobKindCollection, nil, 0, 0)
		require.Error(t, err)
		assert.Nil(t, job)
		assert.Contains(t, err.Error(), "fila de trabalho indisponível")
	})

	t.Run("sem id ou relógio", func(t *testing.T) {
		env := newCovEnv(t)
		semIDs := env.deps
		semIDs.IDs = nil
		_, err := EnqueueWithAttempts(ctx, semIDs, domain.JobKindCollection, nil, 0, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sem gerador de id ou relógio")

		semRelogio := env.deps
		semRelogio.Clock = nil
		_, err = EnqueueWithAttempts(ctx, semRelogio, domain.JobKindCollection, nil, 0, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sem gerador de id ou relógio")
	})

	t.Run("tipo obrigatório", func(t *testing.T) {
		env := newCovEnv(t)
		_, err := EnqueueWithAttempts(ctx, env.deps, "", nil, 0, 3)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tipo de trabalho obrigatório")
		assert.Empty(t, env.jobs.items, "nada é gravado sem tipo")
	})

	t.Run("payload impossível de serializar", func(t *testing.T) {
		env := newCovEnv(t)
		_, err := EnqueueWithAttempts(ctx, env.deps, domain.JobKindCollection, make(chan int), 0, 3)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "serializar payload do trabalho")
		assert.Empty(t, env.jobs.items)
	})

	t.Run("grava payload, tentativas padrão e agendamento", func(t *testing.T) {
		env := newCovEnv(t)
		job, err := EnqueueWithAttempts(ctx, env.deps, domain.JobKindCollection, map[string]any{"receipt": "r-1"}, 15*time.Minute, 0)
		require.NoError(t, err)
		require.NotNil(t, job)

		agora := format.FormatUTCStamp(covToday())
		assert.Equal(t, "cov-001", job.ID)
		assert.Equal(t, domain.JobKindCollection, job.Kind)
		assert.Equal(t, domain.JobStatusQueued, job.Status)
		assert.Equal(t, 0, job.Attempts)
		assert.Equal(t, DefaultJobMaxAttempts, job.MaxAttempts, "tentativas não positivas caem no padrão")
		assert.Equal(t, agora, job.CreatedAt)
		assert.Equal(t, agora, job.UpdatedAt)
		assert.Equal(t, agora, job.ScheduledAt)
		require.NotNil(t, job.Payload)
		assert.JSONEq(t, `{"receipt":"r-1"}`, *job.Payload)

		require.Len(t, env.jobs.items, 1, "o trabalho precisa ter sido gravado na fila")
		assert.Equal(t, *job, env.jobs.items[0])

		agendado, err := EnqueueWithAttempts(ctx, env.deps, domain.JobKindCollection, "texto", 15*time.Minute, 7)
		require.NoError(t, err)
		assert.Equal(t, 7, agendado.MaxAttempts)
		assert.Equal(t, format.FormatUTCStamp(covToday().Add(15*time.Minute)), agendado.ScheduledAt,
			"o trabalho agendado só vence no futuro")
		require.NotNil(t, agendado.Payload)
		assert.Equal(t, `"texto"`, *agendado.Payload)

		semPayload, err := EnqueueWithAttempts(ctx, env.deps, domain.JobKindCollection, nil, -time.Hour, 3)
		require.NoError(t, err)
		assert.Nil(t, semPayload.Payload, "payload nulo não é gravado")
		assert.Equal(t, agora, semPayload.ScheduledAt, "atraso negativo vence agora")
	})

	t.Run("falha da fila", func(t *testing.T) {
		env := newCovEnv(t)
		env.jobs.createErr = errors.New("disco cheio")
		_, err := EnqueueWithAttempts(ctx, env.deps, domain.JobKindCollection, nil, 0, 3)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "enfileirar trabalho")
	})
}

// TestCopsRetryJob cobre a reexecução pelo painel: só trabalho falho volta para
// a fila, com as tentativas zeradas.
func TestCopsRetryJob(t *testing.T) {
	ctx := context.Background()

	t.Run("sem fila ou relógio", func(t *testing.T) {
		env := newCovEnv(t)
		semFila := env.deps
		semFila.Repos.Jobs = nil
		_, err := RetryJob(ctx, semFila, "j1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fila de trabalho indisponível")

		semRelogio := env.deps
		semRelogio.Clock = nil
		_, err = RetryJob(ctx, semRelogio, "j1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sem relógio")
	})

	t.Run("trabalho inexistente", func(t *testing.T) {
		env := newCovEnv(t)
		_, err := RetryJob(ctx, env.deps, "nao-existe")
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrNotFound)
	})

	t.Run("falha ao buscar", func(t *testing.T) {
		env := newCovEnv(t)
		env.jobs.byIDErr = errors.New("banco fora")
		_, err := RetryJob(ctx, env.deps, "j1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "buscar trabalho")
	})

	t.Run("não reexecuta o que não falhou", func(t *testing.T) {
		env := newCovEnv(t)
		env.jobs.items = []domain.Job{
			copsFila("j-run", domain.JobKindCollection, domain.JobStatusRunning, 1, 3, ""),
			copsFila("j-queued", domain.JobKindCollection, domain.JobStatusQueued, 0, 3, ""),
			copsFila("j-done", domain.JobKindCollection, domain.JobStatusDone, 1, 3, ""),
		}
		_, err := RetryJob(ctx, env.deps, "j-run")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "está em execução")

		_, err = RetryJob(ctx, env.deps, "j-queued")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "não está falho (situação: queued)")

		_, err = RetryJob(ctx, env.deps, "j-done")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "não está falho (situação: done)")
		assert.Empty(t, env.jobs.updates, "nenhum trabalho foi reescrito")
	})

	t.Run("reenfileira o trabalho falho", func(t *testing.T) {
		env := newCovEnv(t)
		iniciado, terminado, falha := "2026-09-20 10:00:00", "2026-09-20 10:01:00", "fila fora do ar"
		resultado := `{"status":"done","attempts":1}`
		job := copsFila("j-falho", domain.JobKindCollection, domain.JobStatusFailed, 3, 3, "2026-09-20 09:00:00")
		job.StartedAt, job.FinishedAt, job.Error, job.Result = &iniciado, &terminado, &falha, &resultado
		env.jobs.items = []domain.Job{job}

		devolvido, err := RetryJob(ctx, env.deps, "j-falho")
		require.NoError(t, err)
		agora := format.FormatUTCStamp(covToday())
		assert.Equal(t, domain.JobStatusQueued, devolvido.Status)
		assert.Equal(t, 0, devolvido.Attempts, "as tentativas voltam a zero")
		assert.Nil(t, devolvido.Error)
		assert.Nil(t, devolvido.Result)
		assert.Nil(t, devolvido.StartedAt)
		assert.Nil(t, devolvido.FinishedAt)
		assert.Equal(t, agora, devolvido.ScheduledAt)
		assert.Equal(t, agora, devolvido.UpdatedAt)

		require.Len(t, env.jobs.updates, 1)
		assert.Equal(t, domain.JobStatusQueued, env.jobs.updates[0].Status)
		assert.Equal(t, 0, env.jobs.items[0].Attempts, "a fila guardou o trabalho reenfileirado")
		assert.Nil(t, env.jobs.items[0].StartedAt)
	})

	t.Run("falha ao reenfileirar", func(t *testing.T) {
		env := newCovEnv(t)
		env.jobs.items = []domain.Job{copsFila("j-falho", domain.JobKindCollection, domain.JobStatusFailed, 1, 3, "")}
		env.jobs.updateErr = errors.New("banco fora")
		_, err := RetryJob(ctx, env.deps, "j-falho")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reenfileirar trabalho")
	})
}

// TestCopsJobDue cobre o vencimento: horário ausente ou ilegível conta como
// vencido, o futuro não.
func TestCopsJobDue(t *testing.T) {
	env := newCovEnv(t)
	worker := copsWorker(env, 10)
	agora := covToday()

	casos := []struct {
		nome     string
		agendado string
		vencido  bool
	}{
		{"sem agendamento", "", true},
		{"ilegível", "ontem à tarde", true},
		{"futuro", format.FormatUTCStamp(agora.Add(time.Hour)), false},
		{"exatamente agora", format.FormatUTCStamp(agora), true},
		{"vencido", format.FormatUTCStamp(agora.Add(-48 * time.Hour)), true},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			assert.Equal(t, caso.vencido, worker.jobDue(copsFila("j1", domain.JobKindCollection, domain.JobStatusQueued, 0, 3, caso.agendado), agora))
		})
	}
}

// TestCopsJobBackoff cobre o adiamento da retentativa e o teto de 30 minutos.
func TestCopsJobBackoff(t *testing.T) {
	casos := map[int]time.Duration{
		0:  30 * time.Second,
		1:  30 * time.Second,
		2:  time.Minute,
		3:  2 * time.Minute,
		4:  4 * time.Minute,
		5:  8 * time.Minute,
		6:  16 * time.Minute,
		7:  30 * time.Minute, // 32min pedidos, 30min de teto
		20: 30 * time.Minute,
	}
	for attempts, esperado := range casos {
		assert.Equal(t, esperado, jobBackoff(attempts), "tentativa %d", attempts)
	}
}

// TestCopsWorkerRunsRegisteredHandlers prova que a varredura executa um trabalho
// de cada tipo registrado e grava o desfecho de sucesso.
func TestCopsWorkerRunsRegisteredHandlers(t *testing.T) {
	ctx := context.Background()
	env := newCovEnv(t)
	env.jobs.items = []domain.Job{
		copsFila("j-proof", domain.JobKindPaymentProof, domain.JobStatusQueued, 0, 3, ""),
		copsFila("j-done", domain.JobKindCollection, domain.JobStatusDone, 1, 3, ""),
		copsFila("j-send", domain.JobKindWhatsAppSend, domain.JobStatusQueued, 0, 3, format.FormatUTCStamp(covToday().Add(-time.Minute))),
	}
	worker := copsWorker(env, 10)

	var vistos []string
	for _, kind := range []domain.JobKind{domain.JobKindPaymentProof, domain.JobKindWhatsAppSend} {
		k := kind
		worker.Register(k, func(_ context.Context, _ Deps, job domain.Job) error {
			vistos = append(vistos, string(job.Kind))
			return nil
		})
	}

	tratados, err := worker.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, tratados, "os dois trabalhos vencidos foram tratados")
	assert.Equal(t, []string{"payment-proof", "whatsapp-send"}, vistos)

	concluido := env.jobs.items[0]
	assert.Equal(t, domain.JobStatusDone, concluido.Status)
	assert.Equal(t, 1, concluido.Attempts)
	assert.Nil(t, concluido.Error)
	require.NotNil(t, concluido.FinishedAt)
	assert.Equal(t, format.FormatUTCStamp(covToday()), *concluido.FinishedAt)
	require.NotNil(t, concluido.Result)
	assert.JSONEq(t, `{"status":"done","attempts":1}`, *concluido.Result, "jobResult deixa o rastro da fila")

	assert.Equal(t, domain.JobStatusDone, env.jobs.items[1].Status, "trabalho já concluído não é mexido")
	assert.Equal(t, 1, env.jobs.items[1].Attempts)
}

// TestCopsWorkerHandlerFailure prova o reagendamento com backoff e a falha
// definitiva quando as tentativas acabam.
func TestCopsWorkerHandlerFailure(t *testing.T) {
	ctx := context.Background()

	t.Run("reagenda a próxima tentativa", func(t *testing.T) {
		env := newCovEnv(t)
		env.jobs.items = []domain.Job{copsFila("j1", domain.JobKindCollection, domain.JobStatusQueued, 0, 3, "")}
		worker := copsWorker(env, 10)
		worker.Register(domain.JobKindCollection, func(context.Context, Deps, domain.Job) error {
			return errors.New("fila fora do ar")
		})

		tratados, err := worker.RunOnce(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, tratados)

		job := env.jobs.items[0]
		assert.Equal(t, domain.JobStatusQueued, job.Status, "o trabalho volta para a fila")
		assert.Equal(t, 1, job.Attempts)
		assert.Equal(t, format.FormatUTCStamp(covToday().Add(jobBackoff(1))), job.ScheduledAt,
			"a primeira falha adia 30s")
		assert.Nil(t, job.StartedAt)
		assert.Nil(t, job.Result)
		require.NotNil(t, job.Error)
		assert.Equal(t, "fila fora do ar", *job.Error)
	})

	t.Run("tentativas esgotadas falham em definitivo", func(t *testing.T) {
		env := newCovEnv(t)
		env.jobs.items = []domain.Job{copsFila("j1", domain.JobKindDocumentIntake, domain.JobStatusQueued, 2, 3, "")}
		worker := copsWorker(env, 10)
		worker.Register(domain.JobKindDocumentIntake, func(context.Context, Deps, domain.Job) error {
			return errors.New("documento ilegível")
		})

		_, err := worker.RunOnce(ctx)
		require.NoError(t, err)

		job := env.jobs.items[0]
		assert.Equal(t, domain.JobStatusFailed, job.Status)
		assert.Equal(t, 3, job.Attempts, "a terceira tentativa foi contabilizada")
		assert.Equal(t, "", job.ScheduledAt, "trabalho falho não é reagendado")
		assert.Nil(t, job.StartedAt)
		require.NotNil(t, job.Error)
		assert.Equal(t, "documento ilegível", *job.Error)
		require.NotNil(t, job.FinishedAt)
		assert.Equal(t, format.FormatUTCStamp(covToday()), *job.FinishedAt)
	})
}

// TestCopsWorkerPanicIsJobFailure prova que o pânico do handler vira falha do
// trabalho, e não queda do processo.
func TestCopsWorkerPanicIsJobFailure(t *testing.T) {
	env := newCovEnv(t)
	env.jobs.items = []domain.Job{copsFila("j1", domain.JobKindCollection, domain.JobStatusQueued, 2, 3, "")}
	worker := copsWorker(env, 10)
	worker.Register(domain.JobKindCollection, func(context.Context, Deps, domain.Job) error {
		panic("boom")
	})

	tratados, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, tratados)
	job := env.jobs.items[0]
	assert.Equal(t, domain.JobStatusFailed, job.Status)
	require.NotNil(t, job.Error)
	assert.Contains(t, *job.Error, "pânico ao executar o trabalho: boom")
}

// TestCopsWorkerUnknownKindFailsJob prova que um tipo sem handler falha em
// definitivo, com o motivo gravado.
func TestCopsWorkerUnknownKindFailsJob(t *testing.T) {
	env := newCovEnv(t)
	env.jobs.items = []domain.Job{copsFila("j1", domain.JobKind("tipo-novo"), domain.JobStatusQueued, 0, 3, "")}
	worker := copsWorker(env, 10)

	_, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	job := env.jobs.items[0]
	assert.Equal(t, domain.JobStatusFailed, job.Status)
	require.NotNil(t, job.Error)
	assert.Equal(t, `nenhum handler registrado para o tipo "tipo-novo"`, *job.Error)
	assert.Equal(t, 1, job.Attempts, "o trabalho foi reivindicado antes de falhar")
}

// TestCopsWorkerDueSelection prova o recorte da varredura: o lote é respeitado e
// o primeiro trabalho ainda não vencido interrompe a fila.
func TestCopsWorkerDueSelection(t *testing.T) {
	ctx := context.Background()

	t.Run("lote limita a varredura", func(t *testing.T) {
		env := newCovEnv(t)
		env.jobs.items = []domain.Job{
			copsFila("j1", domain.JobKindCollection, domain.JobStatusQueued, 0, 3, ""),
			copsFila("j2", domain.JobKindCollection, domain.JobStatusQueued, 0, 3, ""),
		}
		worker := copsWorker(env, 1)
		worker.Register(domain.JobKindCollection, func(context.Context, Deps, domain.Job) error { return nil })

		tratados, err := worker.RunOnce(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, tratados)
		assert.Equal(t, domain.JobStatusDone, env.jobs.items[0].Status)
		assert.Equal(t, domain.JobStatusQueued, env.jobs.items[1].Status, "o excedente do lote fica para a próxima varredura")
	})

	t.Run("agendamento futuro no topo interrompe", func(t *testing.T) {
		env := newCovEnv(t)
		env.jobs.items = []domain.Job{
			copsFila("j-futuro", domain.JobKindCollection, domain.JobStatusQueued, 0, 3, format.FormatUTCStamp(covToday().Add(time.Hour))),
			copsFila("j-vencido", domain.JobKindCollection, domain.JobStatusQueued, 0, 3, ""),
		}
		worker := copsWorker(env, 10)
		worker.Register(domain.JobKindCollection, func(context.Context, Deps, domain.Job) error { return nil })

		tratados, err := worker.RunOnce(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0, tratados, "a fila está ordenada por agendamento")
		assert.Equal(t, domain.JobStatusQueued, env.jobs.items[1].Status)
	})

	t.Run("contexto cancelado não pega trabalho novo", func(t *testing.T) {
		env := newCovEnv(t)
		env.jobs.items = []domain.Job{copsFila("j1", domain.JobKindCollection, domain.JobStatusQueued, 0, 3, "")}
		worker := copsWorker(env, 10)
		worker.Register(domain.JobKindCollection, func(context.Context, Deps, domain.Job) error { return nil })

		cancelado, cancel := context.WithCancel(ctx)
		cancel()
		tratados, err := worker.RunOnce(cancelado)
		require.NoError(t, err)
		assert.Equal(t, 0, tratados)
		assert.Equal(t, domain.JobStatusQueued, env.jobs.items[0].Status)
		assert.Equal(t, 0, env.jobs.items[0].Attempts)
	})
}

// TestCopsWorkerRunOnceErrors prova os erros de infraestrutura da varredura.
func TestCopsWorkerRunOnceErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("sem fila", func(t *testing.T) {
		env := newCovEnv(t)
		deps := env.deps
		deps.Repos.Jobs = nil
		worker := NewWorker(deps, WorkerOptions{Interval: time.Minute, BatchSize: 10})
		tratados, err := worker.RunOnce(ctx)
		require.Error(t, err)
		assert.Equal(t, 0, tratados)
		assert.Contains(t, err.Error(), "fila de trabalho indisponível")
	})

	t.Run("falha ao listar", func(t *testing.T) {
		env := newCovEnv(t)
		env.jobs.listErr = errors.New("banco fora")
		worker := copsWorker(env, 10)
		tratados, err := worker.RunOnce(ctx)
		require.Error(t, err)
		assert.Equal(t, 0, tratados)
		assert.Contains(t, err.Error(), "listar trabalhos na fila")
	})
}

// TestCopsWorkerClaimFailure prova que, sem conseguir reivindicar o trabalho
// (marcar `running`), nada é executado nem gravado.
func TestCopsWorkerClaimFailure(t *testing.T) {
	env := newCovEnv(t)
	worker, _ := copsWorkerSeletivo(env, 1, copsFila("j1", domain.JobKindCollection, domain.JobStatusQueued, 0, 3, ""))
	buffer := copsLogBuffer(env)
	executou := false
	worker.Register(domain.JobKindCollection, func(context.Context, Deps, domain.Job) error {
		executou = true
		return nil
	})

	tratados, err := worker.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, tratados)
	assert.False(t, executou, "sem reivindicar o trabalho, o handler não roda")
	assert.Empty(t, env.jobs.updates)
	assert.Equal(t, domain.JobStatusQueued, env.jobs.items[0].Status)
	assert.Contains(t, buffer.String(), "não foi possível marcar trabalho como em execução")
}

// TestCopsWorkerOutcomeNotRecorded prova que a falha ao gravar o desfecho não
// inventa resultado: a fila fica no estado de execução e o motivo vai para o log.
func TestCopsWorkerOutcomeNotRecorded(t *testing.T) {
	ctx := context.Background()

	t.Run("sucesso não gravado", func(t *testing.T) {
		env := newCovEnv(t)
		worker, _ := copsWorkerSeletivo(env, 2, copsFila("j1", domain.JobKindCollection, domain.JobStatusQueued, 0, 3, ""))
		buffer := copsLogBuffer(env)
		worker.Register(domain.JobKindCollection, func(context.Context, Deps, domain.Job) error { return nil })

		_, err := worker.RunOnce(ctx)
		require.NoError(t, err)
		job := env.jobs.items[0]
		assert.Equal(t, domain.JobStatusRunning, job.Status, "o desfecho não foi gravado")
		assert.Nil(t, job.Result)
		assert.Nil(t, job.FinishedAt)
		assert.Contains(t, buffer.String(), "não foi possível gravar trabalho concluído")
	})

	t.Run("reagendamento não gravado", func(t *testing.T) {
		env := newCovEnv(t)
		worker, _ := copsWorkerSeletivo(env, 2, copsFila("j1", domain.JobKindCollection, domain.JobStatusQueued, 0, 3, ""))
		buffer := copsLogBuffer(env)
		worker.Register(domain.JobKindCollection, func(context.Context, Deps, domain.Job) error {
			return errors.New("tentativa falha")
		})

		_, err := worker.RunOnce(ctx)
		require.NoError(t, err)
		job := env.jobs.items[0]
		assert.Equal(t, domain.JobStatusRunning, job.Status)
		assert.Equal(t, "", job.ScheduledAt, "sem gravação não há reagendamento")
		assert.Nil(t, job.Error)
		assert.Contains(t, buffer.String(), "não foi possível reagendar trabalho")
	})

	t.Run("falha definitiva não gravada", func(t *testing.T) {
		env := newCovEnv(t)
		worker, _ := copsWorkerSeletivo(env, 2, copsFila("j1", domain.JobKind("sem-handler"), domain.JobStatusQueued, 0, 3, ""))
		buffer := copsLogBuffer(env)

		_, err := worker.RunOnce(ctx)
		require.NoError(t, err)
		job := env.jobs.items[0]
		assert.Equal(t, domain.JobStatusRunning, job.Status)
		assert.Nil(t, job.Error)
		assert.Nil(t, job.FinishedAt)
		assert.Contains(t, buffer.String(), "não foi possível gravar falha do trabalho")
	})
}

// TestCopsWorkerRecoverOrphans prova que trabalho `running` de um reinício
// volta para a fila.
func TestCopsWorkerRecoverOrphans(t *testing.T) {
	ctx := context.Background()

	t.Run("sem fila", func(t *testing.T) {
		env := newCovEnv(t)
		deps := env.deps
		deps.Repos.Jobs = nil
		worker := NewWorker(deps, WorkerOptions{Interval: time.Minute, BatchSize: 10})
		recuperados, err := worker.RecoverOrphans(ctx)
		require.Error(t, err)
		assert.Equal(t, 0, recuperados)
		assert.Contains(t, err.Error(), "fila de trabalho indisponível")
	})

	t.Run("falha ao listar", func(t *testing.T) {
		env := newCovEnv(t)
		env.jobs.listErr = errors.New("banco fora")
		worker := copsWorker(env, 10)
		recuperados, err := worker.RecoverOrphans(ctx)
		require.Error(t, err)
		assert.Equal(t, 0, recuperados)
		assert.Contains(t, err.Error(), "listar trabalhos em execução")
	})

	t.Run("devolve os órfãos", func(t *testing.T) {
		env := newCovEnv(t)
		iniciado, falha := "2026-09-20 10:00:00", "processo morreu"
		orfao := copsFila("j-orfao", domain.JobKindCollection, domain.JobStatusRunning, 1, 3, "2026-09-20 10:00:00")
		orfao.StartedAt, orfao.Error = &iniciado, &falha
		env.jobs.items = []domain.Job{
			orfao,
			copsFila("j-espera", domain.JobKindCollection, domain.JobStatusQueued, 0, 3, ""),
		}
		worker := copsWorker(env, 10)

		recuperados, err := worker.RecoverOrphans(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, recuperados)
		devolvido := env.jobs.items[0]
		assert.Equal(t, domain.JobStatusQueued, devolvido.Status)
		assert.Nil(t, devolvido.StartedAt, "o trabalho volta a estar pronto para rodar")
		assert.Nil(t, devolvido.Error)
		assert.Equal(t, format.FormatUTCStamp(covToday()), devolvido.UpdatedAt)
		assert.Equal(t, domain.JobStatusQueued, env.jobs.items[1].Status)
	})

	t.Run("falha ao devolver", func(t *testing.T) {
		env := newCovEnv(t)
		env.jobs.items = []domain.Job{copsFila("j-orfao", domain.JobKindCollection, domain.JobStatusRunning, 1, 3, "")}
		env.jobs.updateErr = errors.New("banco fora")
		worker := copsWorker(env, 10)
		recuperados, err := worker.RecoverOrphans(ctx)
		require.Error(t, err)
		assert.Equal(t, 0, recuperados)
		assert.Contains(t, err.Error(), "devolver trabalho j-orfao para a fila")
	})
}

// TestCopsWorkerRunCycle prova o ciclo em segundo plano: não sobrepõe, drena no
// encerramento e só registra a falha quando o contexto está vivo.
func TestCopsWorkerRunCycle(t *testing.T) {
	ctx := context.Background()

	t.Run("roda em segundo plano e drena", func(t *testing.T) {
		env := newCovEnv(t)
		env.jobs.items = []domain.Job{copsFila("j1", domain.JobKindCollection, domain.JobStatusQueued, 0, 3, "")}
		worker := copsWorker(env, 10)
		executou := false
		worker.Register(domain.JobKindCollection, func(context.Context, Deps, domain.Job) error {
			executou = true
			return nil
		})

		worker.runCycle(ctx)
		worker.inflight.Wait()
		assert.True(t, executou)
		assert.Equal(t, domain.JobStatusDone, env.jobs.items[0].Status)
	})

	t.Run("não sobrepõe o ciclo em andamento", func(t *testing.T) {
		env := newCovEnv(t)
		env.jobs.items = []domain.Job{copsFila("j1", domain.JobKindCollection, domain.JobStatusQueued, 0, 3, "")}
		worker := copsWorker(env, 10)
		executou := false
		worker.Register(domain.JobKindCollection, func(context.Context, Deps, domain.Job) error {
			executou = true
			return nil
		})

		worker.busy <- struct{}{} // ciclo anterior ainda em andamento
		worker.runCycle(ctx)
		worker.inflight.Wait()
		assert.False(t, executou, "varredura não sobrepõe a anterior")
		assert.Equal(t, domain.JobStatusQueued, env.jobs.items[0].Status)
	})

	t.Run("registra a varredura falha e cala no cancelamento", func(t *testing.T) {
		env := newCovEnv(t)
		env.jobs.listErr = errors.New("banco fora")
		worker := copsWorker(env, 10)
		buffer := copsLogBuffer(env)

		worker.runCycle(ctx)
		worker.inflight.Wait()
		assert.Contains(t, buffer.String(), "fila: varredura falhou")

		buffer.Reset()
		cancelado, cancel := context.WithCancel(ctx)
		cancel()
		worker.runCycle(cancelado)
		worker.inflight.Wait()
		assert.Empty(t, buffer.String(), "falha esperada no encerramento não vira alarme")
	})
}

// TestCopsWorkerClockAndLogger prova os padrões do worker: relógio injetado,
// queda para o relógio do sistema e logger padrão.
func TestCopsWorkerClockAndLogger(t *testing.T) {
	env := newCovEnv(t)
	assert.Equal(t, covToday(), copsWorker(env, 10).now(), "o relógio injetado manda")

	semDeps := NewWorker(Deps{}, WorkerOptions{})
	assert.WithinDuration(t, time.Now(), semDeps.now(), time.Minute, "sem relógio, usa o do sistema")
	assert.NotNil(t, semDeps.log(), "sem logger, usa o padrão")
	assert.Equal(t, DefaultWorkerInterval, semDeps.interval)
	assert.Equal(t, DefaultWorkerBatchSize, semDeps.batch)
	_, registrado := semDeps.handler(domain.JobKindCollection)
	assert.False(t, registrado)
}

// ── Gate de login (logingate.go) ───────────────────────────────────

// copsGateClock é o relógio avançável dos testes do gate.
type copsGateClock struct{ now time.Time }

func (c *copsGateClock) Now() time.Time          { return c.now }
func (c *copsGateClock) avancar(d time.Duration) { c.now = c.now.Add(d) }

// copsGate monta o gate e o relógio do teste.
func copsGate(maxPerIP, maxGlobal, threshold int) (*LoginGate, *copsGateClock) {
	clock := &copsGateClock{now: covToday()}
	return NewLoginGate(clock, maxPerIP, maxGlobal, threshold), clock
}

// TestCopsLoginDecisionMessage cobre os textos mostrados ao usuário.
func TestCopsLoginDecisionMessage(t *testing.T) {
	assert.Equal(t, "", LoginDecision{Allowed: true}.Message())
	assert.Equal(t, "Muitas tentativas. Aguarde 1 minuto para tentar novamente.",
		LoginDecision{Reason: "ip", RetryAfterSeconds: 42}.Message())
	assert.Equal(t, "Muitas tentativas. Aguarde 1 minuto para tentar novamente.",
		LoginDecision{Reason: "global", RetryAfterSeconds: 60}.Message())
	assert.Equal(t, "Muitas tentativas. Novas tentativas bloqueadas por 1 minuto(s).",
		LoginDecision{Reason: "locked", RetryAfterSeconds: 30}.Message(), "menos de um minuto arredonda para 1")
	assert.Equal(t, "Muitas tentativas. Novas tentativas bloqueadas por 5 minuto(s).",
		LoginDecision{Reason: "locked", RetryAfterSeconds: 300}.Message())
	assert.Equal(t, "Muitas tentativas. Novas tentativas bloqueadas por 60 minuto(s).",
		LoginDecision{Reason: "locked", RetryAfterSeconds: 3600}.Message())
}

// TestCopsLoginGateLimits cobre o limite por IP e o global, com o retry-after
// observável e a janela que se renova.
func TestCopsLoginGateLimits(t *testing.T) {
	t.Run("limite por IP", func(t *testing.T) {
		gate, clock := copsGate(3, 100, 3)
		for attempt := 1; attempt <= 3; attempt++ {
			assert.True(t, gate.Check("10.0.0.1").Allowed, "tentativa %d", attempt)
		}
		negado := gate.Check("10.0.0.1")
		assert.False(t, negado.Allowed)
		assert.Equal(t, "ip", negado.Reason)
		assert.Equal(t, 60, negado.RetryAfterSeconds, "a janela de 60s é informada ao usuário")
		assert.True(t, gate.Check("10.0.0.2").Allowed, "o limite é por IP")

		clock.avancar(rateLimitWindow + time.Second)
		assert.True(t, gate.Check("10.0.0.1").Allowed, "janela nova libera o IP")
	})

	t.Run("limite global", func(t *testing.T) {
		gate, clock := copsGate(5, 2, 3)
		assert.True(t, gate.Check("10.0.0.1").Allowed)
		assert.True(t, gate.Check("10.0.0.2").Allowed)
		negado := gate.Check("10.0.0.3")
		assert.False(t, negado.Allowed)
		assert.Equal(t, "global", negado.Reason)
		assert.Equal(t, 60, negado.RetryAfterSeconds)

		clock.avancar(rateLimitWindow + time.Second)
		assert.True(t, gate.Check("10.0.0.3").Allowed, "a janela global se renova")
	})
}

// TestCopsLoginGateLock cobre o bloqueio progressivo: 5min · 2^strikes, com teto
// de 60min.
func TestCopsLoginGateLock(t *testing.T) {
	gate, clock := copsGate(100, 100, 3)

	gate.RegisterFailure("10.0.0.1")
	gate.RegisterFailure("10.0.0.1")
	assert.True(t, gate.Check("10.0.0.1").Allowed, "duas falhas ainda não bloqueiam")

	gate.RegisterFailure("10.0.0.1") // terceira falha: bloqueia
	bloqueado := gate.Check("10.0.0.1")
	assert.False(t, bloqueado.Allowed)
	assert.Equal(t, "locked", bloqueado.Reason)
	assert.Equal(t, 300, bloqueado.RetryAfterSeconds)
	assert.Equal(t, "Muitas tentativas. Novas tentativas bloqueadas por 5 minuto(s).", bloqueado.Message())

	esperados := []int{600, 1200, 2400, 3600, 3600}
	for _, esperado := range esperados {
		for strike := 0; strike < 3; strike++ {
			gate.RegisterFailure("10.0.0.1")
		}
		decisao := gate.Check("10.0.0.1")
		assert.Equal(t, "locked", decisao.Reason)
		assert.Equal(t, esperado, decisao.RetryAfterSeconds, "o bloqueio dobra até o teto de 60min")
		clock.avancar(time.Duration(esperado)*time.Second + time.Second) // deixa o bloqueio vencer
	}

	assert.True(t, gate.Check("10.0.0.1").Allowed, "bloqueio vencido libera o IP")
}

// TestCopsLoginGateRegisterSuccess prova que o login bem-sucedido apaga o
// histórico de falhas do IP.
func TestCopsLoginGateRegisterSuccess(t *testing.T) {
	gate, _ := copsGate(100, 100, 3)
	gate.RegisterFailure("10.0.0.1")
	gate.RegisterFailure("10.0.0.1")
	gate.RegisterSuccess("10.0.0.1")
	gate.RegisterFailure("10.0.0.1")

	assert.True(t, gate.Check("10.0.0.1").Allowed, "as falhas anteriores foram esquecidas")
}

// TestCopsLoginGateReset prova que o Reset limpa janelas, bloqueios e o contador
// global.
func TestCopsLoginGateReset(t *testing.T) {
	gate, clock := copsGate(1, 1, 1)
	assert.True(t, gate.Check("10.0.0.1").Allowed)
	assert.Equal(t, "ip", gate.Check("10.0.0.2").Reason)
	gate.RegisterFailure("10.0.0.1")
	assert.Equal(t, "locked", gate.Check("10.0.0.1").Reason)

	gate.Reset()
	assert.Empty(t, gate.perIP)
	assert.Empty(t, gate.failureByIP)
	assert.Equal(t, rateWindow{}, gate.global)
	assert.True(t, gate.Check("10.0.0.1").Allowed, "o bloqueio também foi embora")
	assert.Equal(t, "ip", gate.Check("10.0.0.1").Reason, "mas a janela recomeça a contar")
	clock.avancar(rateLimitWindow + time.Second)
	assert.True(t, gate.Check("10.0.0.1").Allowed)
}

// TestCopsLoginGatePrune prova a limpeza das entradas expiradas: só acontece
// acima do teto, e só remove o que já venceu.
func TestCopsLoginGatePrune(t *testing.T) {
	t.Run("abaixo do teto não limpa", func(t *testing.T) {
		gate, clock := copsGate(100, 100, 3)
		vencido := covToday().Add(-time.Hour)
		gate.perIP["ip-antigo"] = &rateWindow{count: 2, resetAt: vencido}
		gate.failureByIP["ip-antigo"] = &failureState{lockedUntil: vencido}
		clock.avancar(time.Minute)

		assert.True(t, gate.Check("ip-novo").Allowed)
		assert.Len(t, gate.perIP, 2, "sem estouro do teto nada é removido")
		assert.Len(t, gate.failureByIP, 1)
	})

	t.Run("acima do teto remove o que venceu", func(t *testing.T) {
		gate, clock := copsGate(100, 100, 3)
		for index := 0; index <= loginGateMaxSize; index++ {
			gate.perIP[fmt.Sprintf("ip-%d", index)] = &rateWindow{count: 1, resetAt: covToday().Add(time.Minute)}
			gate.failureByIP[fmt.Sprintf("ip-%d", index)] = &failureState{
				lockedUntil: covToday().Add(loginLockBase), failures: 0,
			}
		}
		// Uma falha ainda em curso não pode ser esquecida pela limpeza.
		gate.failureByIP["ip-em-curso"] = &failureState{lockedUntil: covToday().Add(-time.Minute), failures: 2}
		clock.avancar(2 * time.Hour)

		assert.True(t, gate.Check("ip-visitante").Allowed)
		assert.Len(t, gate.perIP, 1, "as janelas expiradas foram removidas")
		_, visitante := gate.perIP["ip-visitante"]
		assert.True(t, visitante)
		assert.Contains(t, gate.failureByIP, "ip-em-curso", "falha em contagem não é descartada")
		assert.Len(t, gate.failureByIP, 1, "os bloqueios vencidos e já contabilizados foram removidos")
	})
}

// TestCopsSecondsUntil cobre o arredondamento para cima e o piso em zero.
func TestCopsSecondsUntil(t *testing.T) {
	agora := covToday()
	assert.Equal(t, 60, secondsUntil(agora.Add(time.Minute), agora))
	assert.Equal(t, 2, secondsUntil(agora.Add(1500*time.Millisecond), agora))
	assert.Equal(t, 0, secondsUntil(agora, agora))
	assert.Equal(t, 0, secondsUntil(agora.Add(-time.Hour), agora))
}

// ── Trilha de auditoria (audit_query.go) ───────────────────────────

// copsAuditFalha injeta falha em cada leitura da trilha e devolve os atores
// configurados (o dublê do harness não tem lista de atores).
type copsAuditFalha struct {
	*covAudit
	listErr, countErr, actorsErr error
	actors                       []string
}

func (r *copsAuditFalha) List(ctx context.Context, filter domain.AuditFilter) ([]domain.AuditLog, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.covAudit.List(ctx, filter)
}

func (r *copsAuditFalha) Count(ctx context.Context, filter domain.AuditFilter) (int, error) {
	if r.countErr != nil {
		return 0, r.countErr
	}
	return r.covAudit.Count(ctx, filter)
}

func (r *copsAuditFalha) Actors(ctx context.Context) ([]string, error) {
	if r.actorsErr != nil {
		return nil, r.actorsErr
	}
	return r.actors, nil
}

// copsAuditEntry monta um evento da trilha com o JSON das colunas.
func copsAuditEntry(id, action string, changes, metadata *string) domain.AuditLog {
	return domain.AuditLog{
		ID: id, CreatedAt: "2026-09-21 12:00:00", Action: action, Entity: "receipt",
		ActorID: domain.StrPtr("user-1"), ActorName: domain.StrPtr("Admin"),
		Summary: "Baixa registrada", Changes: changes, Metadata: metadata,
		Origin: domain.AuditOriginWeb, IP: domain.StrPtr("10.0.0.1"),
	}
}

// TestCopsListAuditFilters prova que o filtro normalizado chega ao repositório
// com a paginação e a ordem certas, e que a página devolvida traz a trilha
// desserializada.
func TestCopsListAuditFilters(t *testing.T) {
	ctx := context.Background()
	mudancas := `[{"field":"amount","label":"Valor","from":"100","to":"250"}]`
	metadados := `{"origem":"import"}`
	env := newCovEnv(t)
	env.audit.entries = []domain.AuditLog{
		copsAuditEntry("a1", "payment.received", &mudancas, &metadados),
		copsAuditEntry("a2", "lease.created", nil, nil),
	}

	filtro := AuditFilter{
		Page: 2, PageSize: 10, Entity: "receipt", Action: "payment.received",
		Actor: "Admin", From: "2026-09-01", To: "2026-09-30", Search: "baixa",
		Descending: false,
	}
	pagina, err := ListAudit(ctx, env.deps, filtro)
	require.NoError(t, err)

	assert.Equal(t, domain.AuditFilter{
		Entity: "receipt", Action: "payment.received", Actor: "Admin",
		From: "2026-09-01", To: "2026-09-30", Search: "baixa",
		Descending: false, Limit: 10, Offset: 10,
	}, env.audit.filter, "a segunda página de 10 começa no 11º evento")
	assert.Equal(t, 2, pagina.Total)
	assert.Equal(t, 2, pagina.Page)
	assert.Equal(t, 10, pagina.PageSize)
	assert.NotNil(t, pagina.Actors, "a lista de autores nunca é nula")
	assert.Empty(t, pagina.Actors)
	require.Len(t, pagina.Items, 2)
	assert.Equal(t, "Admin", domain.Str(pagina.Items[0].ActorName))
	assert.Equal(t, []domain.AuditChange{{Field: "amount", Label: "Valor", From: domain.StrPtr("100"), To: domain.StrPtr("250")}}, pagina.Items[0].Changes)
	assert.Equal(t, map[string]any{"origem": "import"}, pagina.Items[0].Metadata)
	assert.Empty(t, pagina.Items[1].Changes, "coluna nula vira lista vazia")
	assert.Empty(t, pagina.Items[1].Metadata, "coluna nula vira objeto vazio")
	assert.Equal(t, "web", pagina.Items[0].Origin)
}

// TestCopsListAuditNormalization prova os limites da paginação pedida pela tela.
func TestCopsListAuditNormalization(t *testing.T) {
	ctx := context.Background()
	casos := []struct {
		nome             string
		filtro           AuditFilter
		pagina, tamanho  int
		limite, deslocam int
	}{
		{"padrões", AuditFilter{}, 1, DefaultAuditPageSize, DefaultAuditPageSize, 0},
		{"página negativa", AuditFilter{Page: -3}, 1, DefaultAuditPageSize, DefaultAuditPageSize, 0},
		{"tamanho abaixo do mínimo", AuditFilter{Page: 3, PageSize: 3}, 3, MinAuditPageSize, MinAuditPageSize, 10},
		{"tamanho acima do máximo", AuditFilter{Page: 1, PageSize: 5000}, 1, MaxAuditPageSize, MaxAuditPageSize, 0},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			env := newCovEnv(t)
			pagina, err := ListAudit(ctx, env.deps, caso.filtro)
			require.NoError(t, err)
			assert.Equal(t, caso.pagina, pagina.Page)
			assert.Equal(t, caso.tamanho, pagina.PageSize)
			assert.Equal(t, caso.limite, env.audit.filter.Limit)
			assert.Equal(t, caso.deslocam, env.audit.filter.Offset)
		})
	}
}

// TestCopsListAuditErrors prova que a falha de cada leitura da trilha sai
// identificada.
func TestCopsListAuditErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("listar", func(t *testing.T) {
		env := newCovEnv(t)
		env.deps.Repos.Audit = &copsAuditFalha{covAudit: env.audit, listErr: errors.New("banco fora")}
		_, err := ListAudit(ctx, env.deps, AuditFilter{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "listar trilha de auditoria")
	})

	t.Run("contar", func(t *testing.T) {
		env := newCovEnv(t)
		env.deps.Repos.Audit = &copsAuditFalha{covAudit: env.audit, countErr: errors.New("banco fora")}
		_, err := ListAudit(ctx, env.deps, AuditFilter{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "contar trilha de auditoria")
	})

	t.Run("autores", func(t *testing.T) {
		env := newCovEnv(t)
		env.deps.Repos.Audit = &copsAuditFalha{covAudit: env.audit, actorsErr: errors.New("banco fora")}
		_, err := ListAudit(ctx, env.deps, AuditFilter{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "listar autores da trilha")
	})

	t.Run("autores preenchidos", func(t *testing.T) {
		env := newCovEnv(t)
		env.deps.Repos.Audit = &copsAuditFalha{covAudit: env.audit, actors: []string{"Admin", "Sistema"}}
		pagina, err := ListAudit(ctx, env.deps, AuditFilter{})
		require.NoError(t, err)
		assert.Equal(t, []string{"Admin", "Sistema"}, pagina.Actors)
	})
}

// TestCopsDecodeAuditColumns prova a desserialização das colunas JSON da trilha:
// nula ou quebrada continua legível como lista/objeto vazio.
func TestCopsDecodeAuditColumns(t *testing.T) {
	valido := `[{"field":"amount","label":"Valor","from":null,"to":"250"}]`
	casosChanges := []struct {
		nome     string
		raw      *string
		esperado int
	}{
		{"nula", nil, 0},
		{"vazia", domain.StrPtr(""), 0},
		{"quebrada", domain.StrPtr("{não é json"), 0},
		{"nula literal", domain.StrPtr("null"), 0},
		{"válida", &valido, 1},
	}
	for _, caso := range casosChanges {
		t.Run("changes "+caso.nome, func(t *testing.T) {
			changes := decodeAuditChanges(caso.raw)
			assert.NotNil(t, changes)
			assert.Len(t, changes, caso.esperado)
			if caso.esperado == 1 {
				assert.Equal(t, "amount", changes[0].Field)
				assert.Nil(t, changes[0].From)
				assert.Equal(t, "250", domain.Str(changes[0].To))
			}
		})
	}

	objeto := `{"origem":"import","linhas":2}`
	casosMetadata := []struct {
		nome     string
		raw      *string
		esperado int
	}{
		{"nula", nil, 0},
		{"vazia", domain.StrPtr(""), 0},
		{"quebrada", domain.StrPtr("["), 0},
		{"nula literal", domain.StrPtr("null"), 0},
		{"válida", &objeto, 2},
	}
	for _, caso := range casosMetadata {
		t.Run("metadata "+caso.nome, func(t *testing.T) {
			metadata := decodeAuditMetadata(caso.raw)
			assert.NotNil(t, metadata)
			assert.Len(t, metadata, caso.esperado)
			if caso.esperado == 2 {
				assert.Equal(t, "import", metadata["origem"])
				assert.Equal(t, float64(2), metadata["linhas"])
			}
		})
	}
}

// TestCopsAuditCSVDateTime prova a conversão do carimbo para a leitura humana,
// preservando o valor quando ele não é uma data reconhecível.
func TestCopsAuditCSVDateTime(t *testing.T) {
	assert.Equal(t, "", auditCSVDateTime(""))
	assert.Equal(t, "21/09/2026 12:00", auditCSVDateTime("2026-09-21 12:00:00"))
	assert.Equal(t, "21/09/2026", auditCSVDateTime("2026-09-21"))
	assert.Equal(t, "21/09/2026 14:30", auditCSVDateTime("2026-09-21T14:30:00"))
	assert.Equal(t, "ontem", auditCSVDateTime("ontem"), "valor irreconhecível não se perde")
}

// copsBackupSeed enche o ambiente com um cadastro de cada tipo.
func copsBackupSeed(env *covEnv) {
	env.tenants.items = []domain.Tenant{
		{ID: "t1", Name: "Carlos", Email: "carlos@exemplo.com.br", Phone: domain.StrPtr("(22) 99999-0000"), CPF: domain.StrPtr("111.222.333-44"), CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-02 10:00:00"},
	}
	env.props.items = []domain.Property{
		{ID: "p1", Name: "Apto 101", Address: "Rua A, 1", City: "Cabo Frio", State: "RJ", Zip: domain.StrPtr("28900-000"), CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00"},
	}
	env.leases.items = []domain.Lease{
		{ID: "l1", TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 2500.5, Status: domain.LeaseStatusActive, CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00"},
	}
	env.receipts.items = []domain.Receipt{
		{ID: "r1", LeaseID: domain.StrPtr("l1"), TenantID: domain.StrPtr("t1"), DueDate: "2026-07-10", PaidAt: domain.StrPtr("2026-07-09"), Amount: 2500.5, CreatedAt: "2026-07-01 10:00:00"},
	}
}

// TestCopsExportBackup prova que o arquivo de backup leva os cadastros com os
// campos que o produto promete.
func TestCopsExportBackup(t *testing.T) {
	env := newCovEnv(t)
	copsBackupSeed(env)

	backup, err := ExportBackup(context.Background(), env.deps)
	require.NoError(t, err)
	assert.Equal(t, "2026-09-21T12:00:00.000Z", backup.ExportedAt)
	require.Len(t, backup.Tenants, 1)
	assert.Equal(t, "Carlos", backup.Tenants[0].Name)
	assert.Equal(t, "carlos@exemplo.com.br", backup.Tenants[0].Email)
	assert.Equal(t, "(22) 99999-0000", domain.Str(backup.Tenants[0].Phone))
	require.Len(t, backup.Properties, 1)
	assert.Equal(t, "Apto 101", backup.Properties[0].Name)
	require.Len(t, backup.Leases, 1)
	assert.Equal(t, domain.LeaseStatusActive, domain.LeaseStatus(backup.Leases[0].Status))
	assert.Equal(t, 2500.5, backup.Leases[0].RentAmount)
	assert.Equal(t, 10, backup.Leases[0].DueDay)
	require.Len(t, backup.Receipts, 1)
	assert.Equal(t, "2026-07-09", domain.Str(backup.Receipts[0].PaidAt))
	assert.Equal(t, "r1", backup.Receipts[0].ID)

	// O payload é o que o painel grava no arquivo `aluxen-backup-*.json`.
	raw, err := json.Marshal(backup)
	require.NoError(t, err)
	var voltado Backup
	require.NoError(t, json.Unmarshal(raw, &voltado))
	assert.Equal(t, backup.ExportedAt, voltado.ExportedAt)
	assert.Equal(t, backup.Tenants, voltado.Tenants)
}

// TestCopsExportBackupErrors prova que a falha de leitura de qualquer cadastro
// aborta a exportação.
func TestCopsExportBackupErrors(t *testing.T) {
	ctx := context.Background()
	quebrado := errors.New("banco fora")

	casos := []struct {
		nome string
		prep func(*covEnv)
	}{
		{"locatários", func(env *covEnv) { env.tenants.listErr = quebrado }},
		{"imóveis", func(env *covEnv) { env.props.listErr = quebrado }},
		{"contratos", func(env *covEnv) { env.leases.listErr = quebrado }},
		{"lançamentos", func(env *covEnv) { env.receipts.listErr = quebrado }},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			env := newCovEnv(t)
			caso.prep(env)
			backup, err := ExportBackup(ctx, env.deps)
			require.Error(t, err)
			assert.Nil(t, backup)
			assert.ErrorIs(t, err, quebrado)
		})
	}
}

// TestCopsImportBackupRoundTrip prova que o backup exportado volta preservando os
// dados, com ids novos e as chaves estrangeiras remapeadas.
func TestCopsImportBackupRoundTrip(t *testing.T) {
	origem := newCovEnv(t)
	copsBackupSeed(origem)
	backup, err := ExportBackup(context.Background(), origem.deps)
	require.NoError(t, err)
	raw, err := json.Marshal(backup)
	require.NoError(t, err)

	destino := newCovEnv(t)
	resultado, err := ImportBackup(context.Background(), destino.deps, covCtxOf(domain.AuditOriginJob), raw)
	require.NoError(t, err)
	assert.Equal(t, ImportResult{Tenants: 1, Properties: 1, Leases: 1, Receipts: 1}, *resultado)

	require.Len(t, destino.tenants.items, 1)
	locatario := destino.tenants.items[0]
	assert.NotEqual(t, "t1", locatario.ID, "o locatário entra com id novo")
	assert.Equal(t, "Carlos", locatario.Name)
	assert.Equal(t, "2026-01-01 10:00:00", locatario.CreatedAt, "o carimbo original é preservado")
	assert.Equal(t, "2026-01-02 10:00:00", locatario.UpdatedAt)

	require.Len(t, destino.props.items, 1)
	imovel := destino.props.items[0]
	assert.NotEqual(t, "p1", imovel.ID)
	assert.Equal(t, "Apto 101", imovel.Name)

	require.Len(t, destino.leases.items, 1)
	contrato := destino.leases.items[0]
	assert.NotEqual(t, "l1", contrato.ID)
	assert.Equal(t, locatario.ID, contrato.TenantID, "o contrato aponta para o locatário novo")
	assert.Equal(t, imovel.ID, contrato.PropertyID, "o contrato aponta para o imóvel novo")
	assert.Equal(t, 10, contrato.DueDay)
	assert.Equal(t, 2500.5, contrato.RentAmount)
	assert.Equal(t, domain.LeaseStatusActive, contrato.Status)
	assert.Equal(t, "2026-12-31", contrato.EndDate)

	require.Len(t, destino.receipts.items, 1)
	lancamento := destino.receipts.items[0]
	assert.NotEqual(t, "r1", lancamento.ID)
	require.NotNil(t, lancamento.LeaseID)
	assert.Equal(t, contrato.ID, *lancamento.LeaseID, "o lançamento aponta para o contrato novo")
	assert.Equal(t, domain.ReceiptCategoryRent, lancamento.Category)
	assert.Equal(t, 2500.5, lancamento.Amount)
	assert.Equal(t, "2026-07-09", domain.Str(lancamento.PaidAt))
	assert.Equal(t, "2026-07-01 10:00:00", lancamento.CreatedAt)
}

// TestCopsImportBackupFallbacks prova o que o formato antigo deixa em branco:
// `dueDay` cai para 1, situação vazia vira ativa e carimbo ausente vira o
// instante da importação.
func TestCopsImportBackupFallbacks(t *testing.T) {
	ctx := context.Background()
	destino := newCovEnv(t)
	conteudo := `{
	  "tenants": [{"id":"t1","name":"Carlos","email":"c@exemplo.com.br","phone":null,"cpf":null,"createdAt":"","updatedAt":""}],
	  "properties": [{"id":"p1","name":"Apto","address":"Rua A, 1","city":"Cabo Frio","state":"RJ","zip":null,"createdAt":"","updatedAt":""}],
	  "leases": [{"id":"l1","tenantId":"t1","propertyId":"nao-importado","startDate":"2026-01-01","endDate":"2026-12-31","dueDay":0,"rentAmount":1000,"status":"","createdAt":"2026-01-01 10:00:00","updatedAt":""}],
	  "receipts": [{"id":"r1","leaseId":null,"dueDate":"2026-07-10","paidAt":null,"amount":1000,"createdAt":""}]
	}`

	_, err := ImportBackup(ctx, destino.deps, covCtx(), []byte(conteudo))
	require.NoError(t, err)
	agora := format.FormatUTCStamp(covToday())

	require.Len(t, destino.tenants.items, 1)
	assert.Equal(t, agora, destino.tenants.items[0].CreatedAt, "carimbo ausente cai no instante da importação")
	assert.Equal(t, agora, destino.tenants.items[0].UpdatedAt)

	contrato := destino.leases.items[0]
	assert.Equal(t, 1, contrato.DueDay, "dueDay ausente cai para 1")
	assert.Equal(t, domain.LeaseStatusActive, contrato.Status, "situação vazia vira ativa")
	assert.Equal(t, "2026-01-01 10:00:00", contrato.CreatedAt)
	assert.Equal(t, agora, contrato.UpdatedAt)
	assert.Equal(t, "nao-importado", contrato.PropertyID, "referência desconhecida é mantida para o banco acusar")
	assert.Equal(t, destino.tenants.items[0].ID, contrato.TenantID)

	lancamento := destino.receipts.items[0]
	assert.Nil(t, lancamento.LeaseID, "lançamento sem contrato continua sem contrato")
	assert.Equal(t, agora, lancamento.CreatedAt)
	assert.Len(t, destino.props.items, 1, "o imóvel entrou")
}

// TestCopsImportBackupValidation prova as recusas do arquivo: JSON quebrado ou
// seção ausente.
func TestCopsImportBackupValidation(t *testing.T) {
	ctx := context.Background()

	destino := newCovEnv(t)
	_, err := ImportBackup(ctx, destino.deps, covCtx(), []byte("{não é json"))
	require.Error(t, err)
	assert.True(t, domain.IsValidation(err))
	assert.Equal(t, "Arquivo JSON inválido.", domain.ValidationMessage(err))
	assert.Empty(t, destino.tenants.items)

	destino = newCovEnv(t)
	_, err = ImportBackup(ctx, destino.deps, covCtx(), []byte(`{"tenants":[]}`))
	require.Error(t, err)
	assert.True(t, domain.IsValidation(err))
	assert.Equal(t, "Formato de backup inválido.", domain.ValidationMessage(err))

	destino = newCovEnv(t)
	_, err = ImportBackup(ctx, destino.deps, covCtx(), []byte(`{"tenants":[],"properties":[],"leases":[],"receipts":null}`))
	require.Error(t, err)
	assert.Equal(t, "Formato de backup inválido.", domain.ValidationMessage(err))
}

// TestCopsImportBackupErrors prova que a falha de gravação de cada cadastro
// aborta a importação com o registro identificado.
func TestCopsImportBackupErrors(t *testing.T) {
	ctx := context.Background()
	quebrado := errors.New("banco fora")
	conteudo := `{
	  "tenants": [{"id":"t1","name":"Carlos","email":"c@exemplo.com.br","createdAt":"","updatedAt":""}],
	  "properties": [{"id":"p1","name":"Apto","address":"Rua A, 1","city":"Cabo Frio","state":"RJ","createdAt":"","updatedAt":""}],
	  "leases": [{"id":"l1","tenantId":"t1","propertyId":"p1","startDate":"2026-01-01","endDate":"2026-12-31","dueDay":10,"rentAmount":1000,"status":"active","createdAt":"","updatedAt":""}],
	  "receipts": [{"id":"r1","leaseId":"l1","dueDate":"2026-07-10","amount":1000,"createdAt":""}]
	}`

	casos := []struct {
		nome   string
		prep   func(*covEnv, error)
		contém string
	}{
		{"locatário", func(env *covEnv, err error) { env.tenants.createErr = err }, "importar locatário"},
		{"imóvel", func(env *covEnv, err error) { env.props.createErr = err }, "importar imóvel"},
		{"contrato", func(env *covEnv, err error) { env.leases.createErr = err }, "importar contrato"},
		{"lançamento", func(env *covEnv, err error) { env.receipts.createErr = err }, "importar lançamento"},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			env := newCovEnv(t)
			caso.prep(env, quebrado)
			resultado, err := ImportBackup(ctx, env.deps, covCtx(), []byte(conteudo))
			require.Error(t, err)
			assert.Nil(t, resultado)
			assert.ErrorIs(t, err, quebrado)
			assert.Contains(t, err.Error(), caso.contém)
		})
	}
}

// ── Migração de dialeto (migrate.go) ───────────────────────────────

// copsRepoRoot sobe até a raiz do módulo.
func copsRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "raiz do módulo não encontrada a partir de %s", dir)
		dir = parent
	}
}

// copsDDL devolve os comandos do schema canônico, na ordem do arquivo.
func copsDDL(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(copsRepoRoot(t), "schema", "sqlite.sql"))
	require.NoError(t, err)

	var statements []string
	for _, chunk := range strings.Split(string(raw), ";") {
		if trimmed := strings.TrimSpace(chunk); trimmed != "" {
			statements = append(statements, trimmed)
		}
	}
	require.NotEmpty(t, statements)
	return statements
}

// copsSQLite abre a conexão de trabalho em arquivo.
func copsSQLite(t *testing.T, path string) *sql.DB {
	t.Helper()
	handle, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	handle.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, handle.Close()) })
	return handle
}

// copsExec executa os comandos na conexão.
func copsExec(t *testing.T, handle *sql.DB, statements []string) {
	t.Helper()
	for _, statement := range statements {
		_, err := handle.Exec(statement)
		require.NoError(t, err, statement)
	}
}

// copsOrigem cria a base SQLite de origem, migrada e com as linhas semeadas.
func copsOrigem(t *testing.T, rows []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "origem.db")
	handle := copsSQLite(t, path)
	copsExec(t, handle, copsDDL(t))
	copsExec(t, handle, rows)
	return path
}

// copsDestino cria o destino já migrado e vazio, com o DDL opcionalmente
// ajustado (o ajuste existe para provar o comportamento em schema divergente).
func copsDestino(t *testing.T, ajuste func([]string) []string) *sql.DB {
	t.Helper()
	statements := copsDDL(t)
	if ajuste != nil {
		statements = ajuste(statements)
	}
	handle := copsSQLite(t, filepath.Join(t.TempDir(), "destino.db"))
	copsExec(t, handle, statements)
	return handle
}

// copsConta conta as linhas de uma tabela.
func copsConta(t *testing.T, handle *sql.DB, table string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, handle.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM "`+table+`"`).Scan(&count))
	return count
}

// copsComConstraint acrescenta uma restrição ao CREATE TABLE da tabela alvo.
func copsComConstraint(statements []string, alvo, constraint string) []string {
	out := append([]string(nil), statements...)
	for index, statement := range out {
		if !strings.Contains(statement, "CREATE TABLE "+alvo+" ") {
			continue
		}
		if last := strings.LastIndex(statement, "\n)"); last >= 0 {
			out[index] = statement[:last] + ",\n  " + constraint + statement[last:]
		}
	}
	return out
}

// copsLinhaRecibo monta o INSERT do lançamento; o valor entra por parâmetro para
// os testes que precisam de dado recusado pelo destino.
func copsLinhaRecibo(amount float64) string {
	return fmt.Sprintf(`INSERT INTO receipts (id, lease_id, tenant_id, property_id, category, description, due_date, paid_at,
	 amount, paid_amount, pdf_path, generated_key, auth_token, created_at, updated_at)
	 VALUES ('r1','l1','t1','p1','rent','Aluguel','2026-07-10','2026-07-09T14:30',%g,2500.5,NULL,'l1:2026-07','tok2',
	 '2026-07-01 10:00:00',NULL)`, amount)
}

// copsLinhas é o conteúdo da origem: uma linha por tabela, cobrindo texto,
// inteiro, real e nulo, e dois locatários (o checksum depende da ordem da chave).
func copsLinhas() []string {
	return []string{
		`INSERT INTO users (id, username, password_hash, password_changed_at, created_at)
		 VALUES ('u1','admin','hash',NULL,'2026-01-01 10:00:00')`,
		`INSERT INTO properties (id, name, address, city, state, zip, created_at, updated_at)
		 VALUES ('p1','Apto 101','Rua A, 1','Cabo Frio','RJ','28900-000','2026-01-01 10:00:00','2026-01-01 10:00:00')`,
		`INSERT INTO tenants (id, name, email, phone, cpf, password_hash, password_changed_at, created_at, updated_at)
		 VALUES ('t1','Carlos','carlos@exemplo.com.br','(22) 99999-0000','111.222.333-44','hash',NULL,'2026-01-01 10:00:00','2026-01-01 10:00:00')`,
		`INSERT INTO tenants (id, name, email, phone, cpf, password_hash, password_changed_at, created_at, updated_at)
		 VALUES ('t2','Ana','ana@exemplo.com.br',NULL,NULL,NULL,NULL,'2026-01-03 10:00:00','2026-01-03 10:00:00')`,
		`INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, status, auth_token,
		 created_at, updated_at, deposit_amount, deposit_status, deposit_received_at, deposit_refunded_at,
		 deposit_refunded_amount, deposit_notes)
		 VALUES ('l1','t1','p1','2026-01-01','2026-12-31',10,2500.5,'active','tok','2026-01-01 10:00:00','2026-01-01 10:00:00',
		 2500.5,'held','2026-01-02',NULL,NULL,NULL)`,
		copsLinhaRecibo(2500.5),
		`INSERT INTO settings (key, value) VALUES ('landlord_name','Locador Demonstração')`,
		`INSERT INTO audit_logs (id, created_at, actor_id, actor_name, action, entity, entity_id, entity_label, summary,
		 changes, metadata, origin, ip, user_agent, request_id)
		 VALUES ('a1','2026-01-01 10:00:00','u1','Admin','payment.received','payment','r1','Aluguel','Baixa registrada',
		 NULL,NULL,'web','127.0.0.1','teste','req-1')`,
		`INSERT INTO inspections (id, lease_id, kind, performed_at, condition, notes, items, photos, auth_token,
		 created_at, updated_at)
		 VALUES ('i1','l1','move-in','2026-01-05','bom',NULL,NULL,NULL,'tok3','2026-01-05 10:00:00','2026-01-05 10:00:00')`,
		`INSERT INTO whatsapp_messages (id, created_at, direction, chat_id, phone, tenant_id, body, status, external_id, metadata)
		 VALUES ('w1','2026-01-06 10:00:00','in','5522999990000@c.us','5522999990000','t1','bom dia','received','ext-1',NULL)`,
		`INSERT INTO jobs (id, created_at, updated_at, kind, status, attempts, max_attempts, payload, result, error,
		 scheduled_at, started_at, finished_at)
		 VALUES ('j1','2026-01-07 10:00:00','2026-01-07 10:00:00','whatsapp-send','queued',0,3,NULL,NULL,NULL,
		 '2026-01-07 10:00:00',NULL,NULL)`,
		`INSERT INTO payment_proofs (id, created_at, updated_at, source, phone, tenant_id, receipt_id, file_name, file_path,
		 mime, extracted_amount, extracted_date, extracted_payer, extracted_payee, extracted_identifier, confidence,
		 status, decision, decision_reason, decided_by, decided_at, raw_response)
		 VALUES ('pp1','2026-01-08 10:00:00','2026-01-08 10:00:00','whatsapp','5522999990000','t1','r1','recibo.jpg',
		 '/data/recibo.jpg','image/jpeg',2500.5,'2026-07-09','Carlos','Locador','E2E123',0.92,'accepted','auto',NULL,NULL,NULL,
		 '{"amount":2500.5}')`,
		`INSERT INTO document_intakes (id, created_at, updated_at, kind, file_name, file_path, mime, status, extracted,
		 confidence, created_entity, created_entity_id, raw_response, notes)
		 VALUES ('d1','2026-01-09 10:00:00','2026-01-09 10:00:00','lease','contrato.pdf','/data/contrato.pdf','application/pdf',
		 'pending','{"tenantName":"Carlos"}',0.8,NULL,NULL,'{}',NULL)`,
	}
}

// copsInvertidas devolve as mesmas linhas em ordem inversa.
func copsInvertidas(rows []string) []string {
	out := make([]string, 0, len(rows))
	for index := len(rows) - 1; index >= 0; index-- {
		out = append(out, rows[index])
	}
	return out
}

// copsContagens conta as linhas semeadas por tabela.
func copsContagens(rows []string) map[string]int64 {
	counts := map[string]int64{}
	for _, row := range rows {
		table := strings.Fields(strings.TrimPrefix(row, "INSERT INTO "))[0]
		counts[table]++
	}
	return counts
}

// copsQueryerFalha é o banco que recusa as leituras da medição.
type copsQueryerFalha struct {
	queryErr error
	row      *sql.Row
}

func (q copsQueryerFalha) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, q.queryErr
}

func (q copsQueryerFalha) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return q.row
}

// TestCopsResolveSourcePath cobre a normalização do caminho da origem.
func TestCopsResolveSourcePath(t *testing.T) {
	for _, entrada := range []string{"", "   ", "file:", "file:   "} {
		_, err := resolveSourcePath(entrada)
		require.Error(t, err, "origem %q", entrada)
		assert.ErrorIs(t, err, ErrMigrateSource)
		assert.Contains(t, err.Error(), "caminho da origem não informado")
	}

	caminho, err := resolveSourcePath("  file:/tmp/aluxen.db  ")
	require.NoError(t, err)
	assert.Equal(t, filepath.FromSlash("/tmp/aluxen.db"), caminho)

	caminho, err = resolveSourcePath("origem.db")
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(caminho), "o caminho relativo vira absoluto")
	assert.Equal(t, "origem.db", filepath.Base(caminho))
}

// TestCopsMigrateSourceResolvedFirst prova que a origem é resolvida antes de
// qualquer contato com o destino.
func TestCopsMigrateSourceResolvedFirst(t *testing.T) {
	report, err := MigrateSQLiteToPostgres(context.Background(), MigrateOptions{Source: "  "})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMigrateSource)
	assert.Empty(t, report.Tables)
	assert.Equal(t, "", report.Source)
}

// TestCopsMigrateDestinationGuards cobre as recusas do destino.
func TestCopsMigrateDestinationGuards(t *testing.T) {
	ctx := context.Background()
	origem := copsOrigem(t, copsLinhas())

	t.Run("sem conexão", func(t *testing.T) {
		report, err := MigrateSQLiteToPostgres(ctx, MigrateOptions{Source: origem, DestinationDialect: DialectSQLite})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMigrateDestination)
		assert.Contains(t, err.Error(), "conexão do destino não informada")
		assert.Equal(t, origem, report.Source, "a origem já foi resolvida")
	})

	t.Run("sem dialeto", func(t *testing.T) {
		destino := copsDestino(t, nil)
		_, err := MigrateSQLiteToPostgres(ctx, MigrateOptions{Source: origem, Destination: destino})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMigrateDestination)
		assert.Contains(t, err.Error(), "dialeto do destino não informado")
	})

	t.Run("conexão fechada", func(t *testing.T) {
		destino := copsDestino(t, nil)
		require.NoError(t, destino.Close())
		_, err := MigrateSQLiteToPostgres(ctx, MigrateOptions{
			Source: origem, Destination: destino, DestinationDialect: DialectSQLite,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMigrateDestination)
	})

	t.Run("destino sem schema", func(t *testing.T) {
		vazio := copsSQLite(t, filepath.Join(t.TempDir(), "cru.db"))
		_, err := MigrateSQLiteToPostgres(ctx, MigrateOptions{
			Source: origem, Destination: vazio, DestinationDialect: DialectSQLite,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMigrateDestinationNotMigrated)
		assert.Contains(t, err.Error(), "users")
	})

	t.Run("destino com dados", func(t *testing.T) {
		destino := copsDestino(t, nil)
		copsExec(t, destino, []string{`INSERT INTO settings (key, value) VALUES ('landlord_name','Já migrado')`})
		report, err := MigrateSQLiteToPostgres(ctx, MigrateOptions{
			Source: origem, Destination: destino, DestinationDialect: DialectSQLite,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMigrateDestinationNotEmpty)
		assert.Contains(t, err.Error(), "settings")
		assert.Empty(t, report.Tables, "nada é copiado com destino sujo")
	})
}

// TestCopsMigrateSourceGuards cobre as recusas da origem.
func TestCopsMigrateSourceGuards(t *testing.T) {
	ctx := context.Background()

	t.Run("arquivo inexistente", func(t *testing.T) {
		destino := copsDestino(t, nil)
		_, err := MigrateSQLiteToPostgres(ctx, MigrateOptions{
			Source:             filepath.Join(t.TempDir(), "nao-existe.db"),
			Destination:        destino,
			DestinationDialect: DialectSQLite,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMigrateSource)
	})

	t.Run("arquivo ilegível", func(t *testing.T) {
		destino := copsDestino(t, nil)
		_, err := MigrateSQLiteToPostgres(ctx, MigrateOptions{
			Source:             filepath.Join(t.TempDir(), "pasta-inexistente", "origem.db"),
			Destination:        destino,
			DestinationDialect: DialectSQLite,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMigrateSource)
		assert.Contains(t, err.Error(), "abrir")
	})

	t.Run("origem vazia", func(t *testing.T) {
		caminho := filepath.Join(t.TempDir(), "vazia.db")
		require.NoError(t, os.WriteFile(caminho, nil, 0o600))
		destino := copsDestino(t, nil)
		_, err := MigrateSQLiteToPostgres(ctx, MigrateOptions{
			Source: caminho, Destination: destino, DestinationDialect: DialectSQLite,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMigrateSource)
		assert.Contains(t, err.Error(), "users")
	})

	t.Run("origem sem coluna do schema", func(t *testing.T) {
		origem := copsOrigem(t, copsLinhas())
		handle := copsSQLite(t, origem)
		copsExec(t, handle, []string{`ALTER TABLE properties DROP COLUMN zip`})
		require.NoError(t, handle.Close())

		destino := copsDestino(t, nil)
		_, err := MigrateSQLiteToPostgres(ctx, MigrateOptions{
			Source: origem, Destination: destino, DestinationDialect: DialectSQLite,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMigrateSource)
		assert.Contains(t, err.Error(), "sem a coluna zip")
	})
}

// TestCopsMigrateDryRun prova que o ensaio mede a origem sem escrever no destino.
func TestCopsMigrateDryRun(t *testing.T) {
	origem := copsOrigem(t, copsLinhas())
	destino := copsDestino(t, nil)

	report, err := MigrateSQLiteToPostgres(context.Background(), MigrateOptions{
		Source: origem, Destination: destino, DestinationDialect: DialectSQLite, DryRun: true,
	})
	require.NoError(t, err)
	assert.True(t, report.DryRun)
	assert.Equal(t, origem, report.Source)
	assert.Equal(t, DialectSQLite, report.Dialect)
	assert.Empty(t, report.FailedTable)
	assert.GreaterOrEqual(t, report.Duration, time.Duration(0))

	esperadas := copsContagens(copsLinhas())
	require.Len(t, report.Tables, len(migrationTables))
	var total int64
	for _, tabela := range report.Tables {
		assert.Equal(t, esperadas[tabela.Table], tabela.SourceRows, "linhas de %s", tabela.Table)
		assert.NotEmpty(t, tabela.SourceChecksum, "checksum de %s", tabela.Table)
		assert.Equal(t, int64(0), tabela.DestinationRows)
		assert.Equal(t, int64(0), tabela.Copied)
		total += tabela.SourceRows
	}
	assert.Equal(t, total, report.Rows())

	descricao := report.String()
	assert.Contains(t, descricao, "modo:   dry-run (nada foi escrito)")
	assert.Contains(t, descricao, "document_intakes")
	assert.Contains(t, descricao, fmt.Sprintf("total: %d linha(s) em %d tabela(s)", total, len(migrationTables)))

	for _, table := range migrationTables {
		assert.Equal(t, int64(0), copsConta(t, destino, table.name), "dry-run não escreve em %s", table.name)
	}
}

// TestCopsMigrateCopiesEveryTable prova a cópia completa: contagem e checksum
// batem em todas as tabelas, na origem e no destino.
func TestCopsMigrateCopiesEveryTable(t *testing.T) {
	ctx := context.Background()
	origem := copsOrigem(t, copsLinhas())
	esperadas := copsContagens(copsLinhas())

	destino := copsDestino(t, nil)
	report, err := MigrateSQLiteToPostgres(ctx, MigrateOptions{
		Source: origem, Destination: destino, DestinationDialect: DialectSQLite,
	})
	require.NoError(t, err)
	assert.False(t, report.DryRun)
	assert.Empty(t, report.FailedTable)

	require.Len(t, report.Tables, len(migrationTables))
	var total int64
	for _, tabela := range report.Tables {
		assert.True(t, tabela.Verified(), "%s divergiu (origem %d/%s, destino %d/%s)",
			tabela.Table, tabela.SourceRows, tabela.SourceChecksum, tabela.DestinationRows, tabela.DestinationChecksum)
		assert.Equal(t, tabela.SourceRows, tabela.Copied)
		assert.Equal(t, esperadas[tabela.Table], tabela.SourceRows)
		assert.Equal(t, tabela.SourceRows, copsConta(t, destino, tabela.Table), "linhas no destino de %s", tabela.Table)
		total += tabela.SourceRows
	}
	assert.Equal(t, total, report.Rows())

	// O dado chegou: o valor copiado é o da origem, não só a contagem.
	var nome string
	require.NoError(t, destino.QueryRowContext(ctx, `SELECT name FROM "tenants" WHERE id = 't2'`).Scan(&nome))
	assert.Equal(t, "Ana", nome)
	var amount float64
	require.NoError(t, destino.QueryRowContext(ctx, `SELECT amount FROM "receipts" WHERE id = 'r1'`).Scan(&amount))
	assert.Equal(t, 2500.5, amount)

	descricao := report.String()
	assert.Contains(t, descricao, "modo:   cópia em transação")
	assert.Contains(t, descricao, "confere")
	assert.NotContains(t, descricao, "DIVERGE")
	assert.NotContains(t, descricao, "falhou em")
}

// TestCopsMigratePostgresPlaceholders prova que o marcador do dialeto de destino
// continua ligando os valores às colunas certas (a cópia roda em SQLite com a
// mesma contagem de parâmetros).
func TestCopsMigratePostgresPlaceholders(t *testing.T) {
	ctx := context.Background()
	origem := copsOrigem(t, copsLinhas())
	destino := copsDestino(t, nil)

	report, err := MigrateSQLiteToPostgres(ctx, MigrateOptions{
		Source: origem, Destination: destino, DestinationDialect: DialectPostgres,
	})
	require.NoError(t, err)
	require.Len(t, report.Tables, len(migrationTables))
	for _, tabela := range report.Tables {
		assert.True(t, tabela.Verified(), "%s divergiu com o marcador do destino", tabela.Table)
	}

	var nome string
	require.NoError(t, destino.QueryRowContext(ctx, `SELECT name FROM "tenants" WHERE id = 't1'`).Scan(&nome))
	assert.Equal(t, "Carlos", nome)
}

// TestCopsMigrateChecksumIgnoresInsertionOrder prova que o checksum depende da
// chave, e não da ordem de inserção: é o que torna a verificação entre dialetos
// confiável.
func TestCopsMigrateChecksumIgnoresInsertionOrder(t *testing.T) {
	ctx := context.Background()
	linhas := copsLinhas()

	direta := copsOrigem(t, linhas)
	invertida := copsOrigem(t, copsInvertidas(linhas))

	primeiro := copsDestino(t, nil)
	reportA, err := MigrateSQLiteToPostgres(ctx, MigrateOptions{
		Source: direta, Destination: primeiro, DestinationDialect: DialectSQLite, DryRun: true,
	})
	require.NoError(t, err)

	segundo := copsDestino(t, nil)
	reportB, err := MigrateSQLiteToPostgres(ctx, MigrateOptions{
		Source: invertida, Destination: segundo, DestinationDialect: DialectSQLite, DryRun: true,
	})
	require.NoError(t, err)

	require.Equal(t, len(reportA.Tables), len(reportB.Tables))
	for index := range reportA.Tables {
		assert.Equal(t, reportA.Tables[index].SourceRows, reportB.Tables[index].SourceRows, reportA.Tables[index].Table)
		assert.Equal(t, reportA.Tables[index].SourceChecksum, reportB.Tables[index].SourceChecksum,
			"a ordem de inserção não muda o checksum de %s", reportA.Tables[index].Table)
	}
}

// TestCopsMigrateRollsBackOnCopyFailure prova que a cópia recusada pelo destino
// não deixa metade do dado gravado.
func TestCopsMigrateRollsBackOnCopyFailure(t *testing.T) {
	ctx := context.Background()
	linhas := []string{
		`INSERT INTO properties (id, name, address, city, state, zip, created_at, updated_at)
		 VALUES ('p1','Apto 101','Rua A, 1','Cabo Frio','RJ','28900-000','2026-01-01 10:00:00','2026-01-01 10:00:00')`,
		`INSERT INTO tenants (id, name, email, phone, cpf, password_hash, password_changed_at, created_at, updated_at)
		 VALUES ('t1','Carlos','carlos@exemplo.com.br',NULL,NULL,NULL,NULL,'2026-01-01 10:00:00','2026-01-01 10:00:00')`,
		`INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, status, auth_token,
		 created_at, updated_at, deposit_amount, deposit_status, deposit_received_at, deposit_refunded_at,
		 deposit_refunded_amount, deposit_notes)
		 VALUES ('l1','t1','p1','2026-01-01','2026-12-31',10,2500.5,'active',NULL,'2026-01-01 10:00:00','2026-01-01 10:00:00',
		 NULL,NULL,NULL,NULL,NULL,NULL)`,
		copsLinhaRecibo(-1),
	}
	origem := copsOrigem(t, linhas)
	destino := copsDestino(t, func(statements []string) []string {
		return copsComConstraint(statements, "receipts", "CHECK (amount >= 0)")
	})

	report, err := MigrateSQLiteToPostgres(ctx, MigrateOptions{
		Source: origem, Destination: destino, DestinationDialect: DialectSQLite,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "copiar receipts")
	assert.Equal(t, "receipts", report.FailedTable)
	assert.GreaterOrEqual(t, report.Duration, time.Duration(0))

	for _, table := range []string{"users", "properties", "tenants", "leases"} {
		assert.Equal(t, int64(0), copsConta(t, destino, table),
			"a transação revertida não deixa linha em %s", table)
	}
}

// TestCopsMigrateRejectsChecksumMismatch prova que a divergência entre origem e
// destino derruba a migração com a transação revertida.
func TestCopsMigrateRejectsChecksumMismatch(t *testing.T) {
	ctx := context.Background()
	linhas := []string{
		`INSERT INTO users (id, username, password_hash, password_changed_at, created_at)
		 VALUES ('u1','admin','hash',NULL,'2026-01-01 10:00:00')`,
		`INSERT INTO tenants (id, name, email, phone, cpf, password_hash, password_changed_at, created_at, updated_at)
		 VALUES ('t1','12345','carlos@exemplo.com.br',NULL,NULL,NULL,NULL,'2026-01-01 10:00:00','2026-01-01 10:00:00')`,
	}
	origem := copsOrigem(t, linhas)
	// O destino guarda `name` como número: a linha entra, mas volta diferente da
	// origem — exatamente o caso que a verificação existe para pegar.
	destino := copsDestino(t, func(statements []string) []string {
		out := append([]string(nil), statements...)
		for index, statement := range out {
			if strings.Contains(statement, "CREATE TABLE tenants ") {
				out[index] = strings.Replace(statement, "name text NOT NULL", "name integer NOT NULL", 1)
			}
		}
		return out
	})

	report, err := MigrateSQLiteToPostgres(ctx, MigrateOptions{
		Source: origem, Destination: destino, DestinationDialect: DialectSQLite,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMigrateVerification)
	assert.Contains(t, err.Error(), "tenants")
	assert.Equal(t, "tenants", report.FailedTable)
	require.Len(t, report.Tables, len(migrationTables))
	assert.Equal(t, int64(1), report.Tables[2].SourceRows)
	assert.Equal(t, int64(1), report.Tables[2].DestinationRows, "a linha entrou, mas com outro conteúdo")
	assert.False(t, report.Tables[2].Verified())
	assert.Equal(t, int64(0), copsConta(t, destino, "users"), "a transação foi revertida")
}

// TestCopsWriteCanonical prova a codificação estável entre dialetos: o mesmo
// dado vira o mesmo byte em qualquer driver.
func TestCopsWriteCanonical(t *testing.T) {
	momento := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	casos := []struct {
		nome     string
		valor    any
		esperado string
	}{
		{"nulo", nil, "\x00\x1f"},
		{"texto", "Aluguel", "s:Aluguel\x1f"},
		{"texto vazio", "", "s:\x1f"},
		{"binário", []byte{0x01, 0xff}, "b:\x01\xff\x1f"},
		{"inteiro", int64(10), "i:10\x1f"},
		{"real", 2500.5, "f:2500.5\x1f"},
		{"booleano", true, "t:true\x1f"},
		{"data", momento, "d:2026-09-21T12:00:00Z\x1f"},
		{"tipo inesperado", 7, "?:7\x1f"},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			var buffer bytes.Buffer
			writeCanonical(&buffer, caso.valor)
			assert.Equal(t, caso.esperado, buffer.String())
		})
	}

	// O hash da tabela é o SHA-256 do conteúdo canônico: o mesmo valor em tipos
	// diferentes de driver precisa produzir o mesmo resumo.
	var primeiro, segundo bytes.Buffer
	writeCanonical(&primeiro, "Aluguel")
	writeCanonical(&segundo, "Aluguel")
	assert.Equal(t, sha256.Sum256(primeiro.Bytes()), sha256.Sum256(segundo.Bytes()))

	// E o checksum publicado é hex do SHA-256, não outra coisa.
	origem := copsOrigem(t, []string{`INSERT INTO settings (key, value) VALUES ('landlord_name','Locador')`})
	handle := copsSQLite(t, origem)
	count, checksum, err := measure(context.Background(), handle, migrationTable{
		name: "settings", columns: []string{"key", "value"}, key: []string{"key"},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	decodificado, err := hex.DecodeString(checksum)
	require.NoError(t, err)
	assert.Len(t, decodificado, sha256.Size)
}

// TestCopsMeasureAndSchemaErrors prova a tradução dos erros de medição e de
// pré-checagem quando o banco recusa a leitura.
func TestCopsMeasureAndSchemaErrors(t *testing.T) {
	ctx := context.Background()
	tabela := migrationTable{name: "settings", columns: []string{"key", "value"}, key: []string{"key"}}

	origem := copsOrigem(t, []string{`INSERT INTO settings (key, value) VALUES ('landlord_name','Locador')`})
	handle := copsSQLite(t, origem)
	linhaBoa := handle.QueryRowContext(ctx, `SELECT COUNT(*) FROM "settings"`)

	fechada := copsSQLite(t, filepath.Join(t.TempDir(), "fechada.db"))
	require.NoError(t, fechada.Close())
	linhaRuim := fechada.QueryRowContext(ctx, `SELECT 1`)

	t.Run("contagem recusada", func(t *testing.T) {
		count, checksum, err := measure(ctx, copsQueryerFalha{row: linhaRuim}, tabela)
		require.Error(t, err)
		assert.Equal(t, int64(0), count)
		assert.Equal(t, "", checksum)
	})

	t.Run("checksum recusado", func(t *testing.T) {
		count, checksum, err := measure(ctx, copsQueryerFalha{queryErr: errors.New("sem coluna"), row: linhaBoa}, tabela)
		require.Error(t, err)
		assert.Equal(t, int64(0), count)
		assert.Equal(t, "", checksum)
	})

	t.Run("tabela inexistente", func(t *testing.T) {
		_, _, err := measure(ctx, handle, migrationTable{name: "inexistente", columns: []string{"id"}, key: []string{"id"}})
		require.Error(t, err)
	})

	t.Run("schema recusado", func(t *testing.T) {
		err := checkSchema(ctx, copsQueryerFalha{queryErr: errors.New("banco fora")}, ErrMigrateDestinationNotMigrated)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMigrateDestinationNotMigrated)
		assert.Contains(t, err.Error(), "users")
	})

	t.Run("destino não vazio recusado", func(t *testing.T) {
		err := checkEmpty(ctx, copsQueryerFalha{row: linhaRuim})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMigrateDestination)
		assert.Contains(t, err.Error(), "contar users")
	})
}

// TestCopsLastTable prova que a última tabela da ordem é a reportada quando a
// transação não confirma.
func TestCopsLastTable(t *testing.T) {
	assert.Equal(t, migrationTables[len(migrationTables)-1].name, lastTable())
	assert.Equal(t, "document_intakes", lastTable())
}

// TestCopsInsertStatementBindsEveryColumn prova que o INSERT montado para cada
// dialeto tem um marcador por coluna: com um a menos, o destino recusaria a
// linha; com um a mais, os valores sairiam trocados.
func TestCopsInsertStatementBindsEveryColumn(t *testing.T) {
	destino := copsDestino(t, nil)
	for _, table := range migrationTables {
		for _, dialect := range []Dialect{DialectSQLite, DialectPostgres} {
			statement, err := destino.Prepare(insertStatement(table, dialect))
			require.NoError(t, err, "%s/%s", table.name, dialect)
			assert.Equal(t, len(table.columns), statement.NumInput(), "%s/%s", table.name, dialect)
			require.NoError(t, statement.Close())
		}
	}
}
