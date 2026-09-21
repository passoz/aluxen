package application

import (
	"testing"
	"time"
)

// fakeClock permite avançar o tempo nos testes do gate de login.
type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) advance(duration time.Duration) { c.now = c.now.Add(duration) }

func newTestGate(clock *fakeClock) *LoginGate {
	return NewLoginGate(clock, 5, 60, 10)
}

func TestLoginGateAllowsFiveAttemptsPerIPInWindow(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	gate := newTestGate(clock)

	for attempt := range 5 {
		if decision := gate.Check("10.0.0.1"); !decision.Allowed {
			t.Fatalf("tentativa %d deveria ser permitida, motivo %q", attempt+1, decision.Reason)
		}
	}

	decision := gate.Check("10.0.0.1")
	if decision.Allowed {
		t.Fatal("sexta tentativa no mesmo minuto deveria ser bloqueada")
	}
	if decision.Reason != "ip" {
		t.Fatalf("motivo = %q, esperado \"ip\"", decision.Reason)
	}
	if decision.RetryAfterSeconds <= 0 {
		t.Fatalf("Retry-After deve ser positivo, veio %d", decision.RetryAfterSeconds)
	}
	if decision.Message() == "" {
		t.Fatal("mensagem de bloqueio não pode ser vazia")
	}

	clock.advance(time.Minute + time.Second)
	if decision := gate.Check("10.0.0.1"); !decision.Allowed {
		t.Fatalf("nova janela deveria permitir, motivo %q", decision.Reason)
	}
}

func TestLoginGateEnforcesGlobalLimit(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	gate := NewLoginGate(clock, 100, 3, 10)

	for attempt := range 3 {
		if decision := gate.Check("10.0.0." + string(rune('1'+attempt))); !decision.Allowed {
			t.Fatalf("tentativa global %d deveria ser permitida", attempt+1)
		}
	}

	decision := gate.Check("10.0.0.9")
	if decision.Allowed || decision.Reason != "global" {
		t.Fatalf("limite global deveria bloquear, veio %+v", decision)
	}
}

func TestLoginGateLocksProgressivelyAfterFailures(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	gate := newTestGate(clock)

	for range 10 {
		gate.RegisterFailure("10.0.0.2")
		clock.advance(time.Second)
	}

	decision := gate.Check("10.0.0.2")
	if decision.Allowed {
		t.Fatal("após 10 falhas o IP deve estar bloqueado")
	}
	if decision.Reason != "locked" {
		t.Fatalf("motivo = %q, esperado \"locked\"", decision.Reason)
	}
	if decision.RetryAfterSeconds < 60 {
		t.Fatalf("primeiro bloqueio deve durar pelo menos um minuto, veio %d", decision.RetryAfterSeconds)
	}

	clock.advance(6 * time.Minute)
	if decision := gate.Check("10.0.0.2"); !decision.Allowed {
		t.Fatalf("após o bloqueio expirar a tentativa deve ser permitida, motivo %q", decision.Reason)
	}

	for range 10 {
		gate.RegisterFailure("10.0.0.2")
		clock.advance(time.Second)
	}
	second := gate.Check("10.0.0.2")
	if second.Allowed {
		t.Fatal("segundo bloqueio deveria ocorrer")
	}
	if second.RetryAfterSeconds < decision.RetryAfterSeconds {
		t.Fatalf("bloqueio deve crescer: primeiro %d, segundo %d", decision.RetryAfterSeconds, second.RetryAfterSeconds)
	}
}

func TestLoginGateSuccessClearsFailures(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	gate := newTestGate(clock)

	for range 9 {
		gate.RegisterFailure("10.0.0.3")
	}
	gate.RegisterSuccess("10.0.0.3")
	for range 9 {
		gate.RegisterFailure("10.0.0.3")
	}

	if decision := gate.Check("10.0.0.3"); !decision.Allowed {
		t.Fatalf("sucesso deveria zerar o contador de falhas, veio %+v", decision)
	}
}
