package application

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/passoz/aluxen/internal/domain"
)

// Gate de login: mesma semântica do app atual — janela fixa de 60s com limite
// por IP e limite global, mais bloqueio progressivo a partir de N falhas
// (5min * 2^strikes, teto de 60min). Compartilhado por /login e /portal/login.
//
// O app antigo guardava isso em Map de processo, sem lock (Node é single
// threaded). Aqui o estado é protegido por mutex, porque o servidor Go atende
// requisições em paralelo.

const (
	rateLimitWindow  = time.Minute
	loginLockBase    = 5 * time.Minute
	loginLockMax     = 60 * time.Minute
	loginGateMaxSize = 1000
)

type rateWindow struct {
	count   int
	resetAt time.Time
}

type failureState struct {
	failures    int
	strikes     int
	lockedUntil time.Time
}

// LoginGate decide se uma tentativa de login é permitida.
type LoginGate struct {
	mu            sync.Mutex
	clock         domain.Clock
	maxPerIP      int
	maxGlobal     int
	lockThreshold int
	perIP         map[string]*rateWindow
	global        rateWindow
	failureByIP   map[string]*failureState
}

// NewLoginGate cria o gate com os limites configurados na instância.
func NewLoginGate(clock domain.Clock, maxPerIP, maxGlobal, lockThreshold int) *LoginGate {
	return &LoginGate{
		clock:         clock,
		maxPerIP:      maxPerIP,
		maxGlobal:     maxGlobal,
		lockThreshold: lockThreshold,
		perIP:         make(map[string]*rateWindow),
		failureByIP:   make(map[string]*failureState),
	}
}

// LoginDecision é o resultado da checagem.
type LoginDecision struct {
	Allowed           bool
	Reason            string
	RetryAfterSeconds int
}

// Message devolve o texto exibido ao usuário, igual ao do app atual.
func (d LoginDecision) Message() string {
	if d.Allowed {
		return ""
	}
	if d.Reason == "locked" {
		minutes := d.RetryAfterSeconds / 60
		if minutes < 1 {
			minutes = 1
		}
		return fmt.Sprintf("Muitas tentativas. Novas tentativas bloqueadas por %d minuto(s).", minutes)
	}
	return "Muitas tentativas. Aguarde 1 minuto para tentar novamente."
}

// Check avalia a tentativa e consome a janela de rate limit. A semântica é a do
// app atual: a primeira tentativa da janela conta 1; a tentativa seguinte ao
// limite é recusada sem incrementar.
func (g *LoginGate) Check(ip string) LoginDecision {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.clock.Now()
	g.prune(now)

	if state, ok := g.failureByIP[ip]; ok && state.lockedUntil.After(now) {
		return LoginDecision{Reason: "locked", RetryAfterSeconds: secondsUntil(state.lockedUntil, now)}
	}

	g.global = advanceWindow(g.global, now)
	g.global.count++
	if g.global.count > g.maxGlobal {
		return LoginDecision{Reason: "global", RetryAfterSeconds: secondsUntil(g.global.resetAt, now)}
	}

	window, ok := g.perIP[ip]
	if !ok || !window.resetAt.After(now) {
		g.perIP[ip] = &rateWindow{count: 1, resetAt: now.Add(rateLimitWindow)}
		return LoginDecision{Allowed: true}
	}
	if window.count >= g.maxPerIP {
		return LoginDecision{Reason: "ip", RetryAfterSeconds: secondsUntil(window.resetAt, now)}
	}
	window.count++
	return LoginDecision{Allowed: true}
}

// RegisterFailure contabiliza a falha e aplica o bloqueio progressivo.
func (g *LoginGate) RegisterFailure(ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	state, ok := g.failureByIP[ip]
	if !ok {
		state = &failureState{}
		g.failureByIP[ip] = state
	}
	state.failures++
	if state.failures >= g.lockThreshold {
		lock := loginLockBase << state.strikes
		if lock > loginLockMax {
			lock = loginLockMax
		}
		state.lockedUntil = g.clock.Now().Add(lock)
		state.strikes++
		state.failures = 0
	}
}

// RegisterSuccess limpa o histórico de falhas do IP.
func (g *LoginGate) RegisterSuccess(ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.failureByIP, ip)
}

// Reset limpa todo o estado (usado nos testes).
func (g *LoginGate) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.perIP = make(map[string]*rateWindow)
	g.failureByIP = make(map[string]*failureState)
	g.global = rateWindow{}
}

func (g *LoginGate) prune(now time.Time) {
	if len(g.perIP) <= loginGateMaxSize && len(g.failureByIP) <= loginGateMaxSize {
		return
	}
	for ip, window := range g.perIP {
		if window.resetAt.Before(now) {
			delete(g.perIP, ip)
		}
	}
	for ip, state := range g.failureByIP {
		if state.lockedUntil.Before(now) && state.failures == 0 {
			delete(g.failureByIP, ip)
		}
	}
}

func advanceWindow(window rateWindow, now time.Time) rateWindow {
	if window.resetAt.After(now) {
		return window
	}
	return rateWindow{count: 0, resetAt: now.Add(rateLimitWindow)}
}

func secondsUntil(deadline, now time.Time) int {
	seconds := int(math.Ceil(deadline.Sub(now).Seconds()))
	if seconds < 0 {
		return 0
	}
	return seconds
}
