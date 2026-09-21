package whatsapp

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// testClock é um relógio controlado pelos testes.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
}

// testIDs gera identificadores previsíveis.
type testIDs struct {
	next int
}

func (g *testIDs) New() string {
	g.next++
	return fmt.Sprintf("job-%d", g.next)
}

func TestJobStoreCreateStartsPreparing(t *testing.T) {
	store := NewJobStore(newTestClock(), &testIDs{})

	job := store.Create("u1")
	if job.ID != "job-1" || job.UserID != "u1" {
		t.Fatalf("job = %+v", job)
	}
	if job.Status != JobRunning || job.Stage != StagePreparing {
		t.Fatalf("status/etapa = %s/%s, esperado running/preparing", job.Status, job.Stage)
	}
	if job.CreatedAt == 0 || job.CreatedAt != job.UpdatedAt {
		t.Fatalf("timestamps = %d/%d", job.CreatedAt, job.UpdatedAt)
	}

	stored, ok := store.Get(job.ID, "u1")
	if !ok || stored.ID != job.ID {
		t.Fatalf("job não foi encontrado depois de criado: %+v", stored)
	}
}

func TestJobStoreTracksStagesAndCompletion(t *testing.T) {
	clock := newTestClock()
	store := NewJobStore(clock, &testIDs{})
	job := store.Create("u1")

	for _, stage := range []SendStage{StageConnecting, StageOpeningChat, StageSending, StageConfirming} {
		clock.advance(time.Second)
		store.Stage(job.ID, stage)
		updated, ok := store.Get(job.ID, "u1")
		if !ok {
			t.Fatalf("job sumiu na etapa %s", stage)
		}
		if updated.Stage != stage {
			t.Fatalf("etapa = %s, esperado %s", updated.Stage, stage)
		}
		if updated.UpdatedAt <= updated.CreatedAt {
			t.Fatalf("UpdatedAt não avançou em %s", stage)
		}
	}

	clock.advance(time.Second)
	store.Complete(job.ID, SendResult{
		Success:   true,
		To:        "5511999999999",
		Message:   "Olá",
		Timestamp: clock.Now().UTC().Format(time.RFC3339),
	})

	done, ok := store.Get(job.ID, "u1")
	if !ok {
		t.Fatal("job sumiu ao concluir")
	}
	if done.Status != JobDone || done.Stage != StageDone || done.Result == nil || !done.Result.Success {
		t.Fatalf("job concluído = %+v", done)
	}
}

func TestJobStoreRecordsFailure(t *testing.T) {
	store := NewJobStore(newTestClock(), &testIDs{})
	job := store.Create("u1")

	store.Stage(job.ID, StageConnecting)
	store.Fail(job.ID, "O número não está registrado no WhatsApp.")

	failed, ok := store.Get(job.ID, "u1")
	if !ok {
		t.Fatal("job sumiu ao falhar")
	}
	if failed.Status != JobError || failed.Error == "" {
		t.Fatalf("job falho = %+v", failed)
	}
	if failed.Stage != StageConnecting {
		t.Fatalf("etapa = %s, esperado a etapa em que falhou", failed.Stage)
	}
}

func TestJobStoreExpiresAfterTTL(t *testing.T) {
	clock := newTestClock()
	store := NewJobStore(clock, &testIDs{})
	job := store.Create("u1")

	clock.advance(JobTTL - time.Second)
	if _, ok := store.Get(job.ID, "u1"); !ok {
		t.Fatal("job venceu antes do TTL de 5 minutos")
	}

	clock.advance(2 * time.Second)
	if _, ok := store.Get(job.ID, "u1"); ok {
		t.Fatal("job continuou disponível depois do TTL")
	}
}

func TestJobStorePrunesOnCreate(t *testing.T) {
	clock := newTestClock()
	store := NewJobStore(clock, &testIDs{})
	old := store.Create("u1")

	clock.advance(JobTTL + time.Second)
	recent := store.Create("u1")

	store.mu.Lock()
	_, hasOld := store.jobs[old.ID]
	_, hasRecent := store.jobs[recent.ID]
	store.mu.Unlock()

	if hasOld {
		t.Fatal("job vencido não foi limpo na criação de outro")
	}
	if !hasRecent {
		t.Fatal("job recém-criado não está no mapa")
	}
}

func TestJobStoreHidesJobFromOtherOwner(t *testing.T) {
	store := NewJobStore(newTestClock(), &testIDs{})
	job := store.Create("u1")

	if _, ok := store.Get(job.ID, "u2"); ok {
		t.Fatal("job de outro administrador não pode ser consultado")
	}
	if _, ok := store.Get("inexistente", "u1"); ok {
		t.Fatal("job inexistente devolveu resultado")
	}
}

func TestJobStoreIgnoresUpdatesToUnknownJob(t *testing.T) {
	store := NewJobStore(newTestClock(), &testIDs{})

	store.Stage("nada", StageSending)
	store.Complete("nada", SendResult{Success: true})
	store.Fail("nada", "erro")

	if _, ok := store.Get("nada", "u1"); ok {
		t.Fatal("job desconhecido apareceu no registro")
	}
}
