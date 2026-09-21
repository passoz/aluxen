package web

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/config"
)

// ── Dublês ─────────────────────────────────────────────────────────

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeIDs struct{ next int }

func (g *fakeIDs) New() string {
	g.next++
	return "id-" + string(rune('0'+g.next))
}

// fakeHasher aceita qualquer senha e devolve um "hash" determinístico — rápido e
// suficiente para exercitar o fluxo HTTP.
type fakeHasher struct{}

func (fakeHasher) Hash(password string) (string, error) { return "hash:" + password, nil }

func (fakeHasher) Compare(hash, password string) error {
	if hash == "hash:"+password {
		return nil
	}
	return domain.ErrUnauthorized
}

type fakeUsers struct{ byID map[string]*domain.User }

func newFakeUsers(users ...*domain.User) *fakeUsers {
	repo := &fakeUsers{byID: map[string]*domain.User{}}
	for _, user := range users {
		repo.byID[user.ID] = user
	}
	return repo
}

func (r *fakeUsers) ByID(_ context.Context, id string) (*domain.User, error) {
	user, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return user, nil
}

func (r *fakeUsers) ByUsername(_ context.Context, username string) (*domain.User, error) {
	for _, user := range r.byID {
		if user.Username == username {
			return user, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *fakeUsers) Create(_ context.Context, user *domain.User) error {
	r.byID[user.ID] = user
	return nil
}

func (r *fakeUsers) UpdateUsername(_ context.Context, id, username string) error {
	user, ok := r.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	user.Username = username
	return nil
}

func (r *fakeUsers) UpdatePassword(_ context.Context, id, passwordHash, changedAt string) error {
	user, ok := r.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	user.PasswordHash = passwordHash
	user.PasswordChangedAt = &changedAt
	return nil
}

type fakeAudit struct{ entries []domain.AuditLog }

func (r *fakeAudit) Insert(_ context.Context, entry *domain.AuditLog) error {
	r.entries = append(r.entries, *entry)
	return nil
}

func (r *fakeAudit) List(context.Context, domain.AuditFilter) ([]domain.AuditLog, error) {
	return r.entries, nil
}

func (r *fakeAudit) Count(context.Context, domain.AuditFilter) (int, error) {
	return len(r.entries), nil
}

func (r *fakeAudit) Actors(context.Context) ([]string, error) { return nil, nil }

func (r *fakeAudit) actions() []string {
	actions := make([]string, 0, len(r.entries))
	for _, entry := range r.entries {
		actions = append(actions, entry.Action)
	}
	return actions
}

type fakeSettings struct{ values map[string]string }

func newFakeSettings() *fakeSettings {
	return &fakeSettings{values: map[string]string{}}
}

func (r *fakeSettings) Get(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", domain.ErrNotFound
	}
	return value, nil
}

func (r *fakeSettings) List(context.Context) (map[string]string, error) { return r.values, nil }

func (r *fakeSettings) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

// ── Montagem do servidor de teste ──────────────────────────────────

type testServer struct {
	handler *Handlers
	audit   *fakeAudit
	users   *fakeUsers
	clock   *fakeClock
}

func newTestServer(t *testing.T, users ...*domain.User) *testServer {
	t.Helper()

	userRepo := newFakeUsers(users...)
	auditRepo := &fakeAudit{}
	clock := &fakeClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}

	cfg := &config.Config{
		AppEnv:             "test",
		Host:               "127.0.0.1",
		Port:               0,
		SessionSecret:      "test-session-secret-with-enough-length",
		RateLimitMax:       5,
		RateLimitGlobalMax: 60,
		LoginLockThreshold: 10,
		SeedUsername:       "passoz",
	}

	deps := application.Deps{
		Repos: application.Repositories{
			Users:       userRepo,
			Settings:    newFakeSettings(),
			Audit:       auditRepo,
			Inspections: &emptyInspections{},
		},
		Clock:  clock,
		IDs:    &fakeIDs{},
		Hasher: fakeHasher{},
		Log:    slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	common := &Common{
		Cfg:       cfg,
		Log:       deps.Log,
		Render:    renderer,
		Session:   NewSessionManager(cfg.SessionSecret, false),
		CSRF:      NewCSRF(cfg.SessionSecret, false),
		Deps:      deps,
		Started:   clock.now,
		Dialect:   "sqlite",
		LoginGate: application.NewLoginGate(clock, cfg.RateLimitMax, cfg.RateLimitGlobalMax, cfg.LoginLockThreshold),
		MustChangePasswordFunc: func(ctx context.Context, userID string) (bool, error) {
			return application.AdminMustChangePassword(ctx, deps, userID)
		},
	}

	return &testServer{handler: NewHandlers(common), audit: auditRepo, users: userRepo, clock: clock}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(payload []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(payload), "\n"))
	return len(payload), nil
}

func (s *testServer) do(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	s.handler.Router().ServeHTTP(recorder, request)
	return recorder
}

func (s *testServer) login(t *testing.T, username, password string) *httptest.ResponseRecorder {
	t.Helper()

	page := httptest.NewRecorder()
	s.handler.Router().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/login", nil))
	token := cookieValue(page.Result().Cookies(), csrfCookieName)
	if token == "" {
		t.Fatal("cookie de CSRF não foi emitido no GET /login")
	}

	form := url.Values{"username": {username}, "password": {password}, csrfFieldName: {token}}
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range page.Result().Cookies() {
		request.AddCookie(cookie)
	}
	return s.do(request)
}

func cookieValue(cookies []*http.Cookie, name string) string {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

// ── Testes ─────────────────────────────────────────────────────────

func TestLoginPageRendersWithCSRFToken(t *testing.T) {
	server := newTestServer(t)

	recorder := server.do(httptest.NewRequest(http.MethodGet, "/login", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `name="csrf_token"`) {
		t.Fatal("formulário de login sem campo csrf_token")
	}
	if !strings.Contains(body, "/portal/login") {
		t.Fatal("página de login sem link para a Área do Cliente")
	}
}

func TestLoginRejectsMissingCSRF(t *testing.T) {
	server := newTestServer(t, &domain.User{ID: "u1", Username: "passoz", PasswordHash: "hash:segredo"})

	form := url.Values{"username": {"passoz"}, "password": {"segredo"}}
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	recorder := server.do(request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403 sem CSRF", recorder.Code)
	}
}

func TestLoginForcesPasswordChangeOnSeededAccount(t *testing.T) {
	server := newTestServer(t, &domain.User{ID: "u1", Username: "passoz", PasswordHash: "hash:segredo"})

	recorder := server.login(t, "passoz", "segredo")
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, esperado 302", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "/settings?forced=1" {
		t.Fatalf("redirecionamento = %q, esperado /settings?forced=1", location)
	}
	if cookieValue(recorder.Result().Cookies(), adminCookieName) == "" {
		t.Fatal("cookie de sessão não foi emitido")
	}
	if actions := server.audit.actions(); len(actions) == 0 || actions[len(actions)-1] != domain.ActionLogin {
		t.Fatalf("trilha de auditoria sem auth.login: %v", actions)
	}
}

func TestLoginWithWrongPasswordRecordsFailure(t *testing.T) {
	server := newTestServer(t, &domain.User{ID: "u1", Username: "passoz", PasswordHash: "hash:segredo"})

	recorder := server.login(t, "passoz", "errada")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401", recorder.Code)
	}
	if actions := server.audit.actions(); len(actions) != 1 || actions[0] != domain.ActionLoginFailed {
		t.Fatalf("trilha de auditoria inesperada: %v", actions)
	}
	if cookieValue(recorder.Result().Cookies(), adminCookieName) != "" {
		t.Fatal("login inválido não pode emitir sessão")
	}
}

func TestLoginRateLimitBlocksSixthAttempt(t *testing.T) {
	server := newTestServer(t, &domain.User{ID: "u1", Username: "passoz", PasswordHash: "hash:segredo"})

	for attempt := range 5 {
		if recorder := server.login(t, "passoz", "errada"); recorder.Code != http.StatusUnauthorized {
			t.Fatalf("tentativa %d: status = %d, esperado 401", attempt+1, recorder.Code)
		}
	}

	recorder := server.login(t, "passoz", "errada")
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("sexta tentativa: status = %d, esperado 429", recorder.Code)
	}
	if recorder.Header().Get("Retry-After") == "" {
		t.Fatal("429 sem header Retry-After")
	}
	if !strings.Contains(recorder.Body.String(), "Muitas tentativas") {
		t.Fatal("página de bloqueio sem mensagem de muitas tentativas")
	}
}

func TestPasswordGateRedirectsAndBlocksAPI(t *testing.T) {
	server := newTestServer(t, &domain.User{ID: "u1", Username: "passoz", PasswordHash: "hash:segredo"})

	loginRecorder := server.login(t, "passoz", "segredo")
	sessionCookie := cookieValue(loginRecorder.Result().Cookies(), adminCookieName)
	if sessionCookie == "" {
		t.Fatal("sem cookie de sessão")
	}

	pageRequest := httptest.NewRequest(http.MethodGet, "/dashboard?tab=dashboard", nil)
	pageRequest.AddCookie(&http.Cookie{Name: adminCookieName, Value: sessionCookie})
	pageRecorder := server.do(pageRequest)
	if pageRecorder.Code != http.StatusFound || pageRecorder.Header().Get("Location") != "/settings?forced=1" {
		t.Fatalf("trava de senha: status %d, location %q", pageRecorder.Code, pageRecorder.Header().Get("Location"))
	}

	apiRequest := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	apiRequest.AddCookie(&http.Cookie{Name: adminCookieName, Value: sessionCookie})
	apiRecorder := server.do(apiRequest)
	if apiRecorder.Code != http.StatusForbidden {
		t.Fatalf("trava de senha em /api/info: status %d, esperado 403", apiRecorder.Code)
	}
	if !strings.Contains(apiRecorder.Body.String(), "Troca de senha obrigatória.") {
		t.Fatalf("corpo inesperado: %s", apiRecorder.Body.String())
	}
}

func TestPasswordGateReleasesAfterPasswordChange(t *testing.T) {
	changedAt := "2026-09-21 12:00:00"
	server := newTestServer(t, &domain.User{
		ID: "u1", Username: "passoz", PasswordHash: "hash:segredo", PasswordChangedAt: &changedAt,
	})

	recorder := server.login(t, "passoz", "segredo")
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/dashboard?tab=dashboard" {
		t.Fatalf("login sem troca pendente deveria ir ao painel: %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestSystemInfoRequiresSession(t *testing.T) {
	server := newTestServer(t)

	recorder := server.do(httptest.NewRequest(http.MethodGet, "/api/info", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Não autorizado.") {
		t.Fatalf("corpo inesperado: %s", recorder.Body.String())
	}
}

func TestHealthEndpoints(t *testing.T) {
	server := newTestServer(t)

	health := server.do(httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || health.Body.String() != "ok" {
		t.Fatalf("healthz = %d %q", health.Code, health.Body.String())
	}

	ready := server.do(httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusOK || ready.Body.String() != "ready" {
		t.Fatalf("readyz = %d %q", ready.Code, ready.Body.String())
	}

	api := server.do(httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if api.Code != http.StatusOK {
		t.Fatalf("api/health = %d", api.Code)
	}
	for _, expected := range []string{`"status":"ok"`, `"runtime":"go"`, `"database":"sqlite (modernc)"`, `"orm":"sqlc"`} {
		if !strings.Contains(api.Body.String(), expected) {
			t.Fatalf("api/health sem %s: %s", expected, api.Body.String())
		}
	}
}

func TestSessionCookieIsSigned(t *testing.T) {
	server := newTestServer(t, &domain.User{ID: "u1", Username: "passoz", PasswordHash: "hash:segredo"})

	forged := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	forged.AddCookie(&http.Cookie{Name: adminCookieName, Value: "u1"})
	recorder := server.do(forged)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("cookie forjado com id cru deveria ser recusado, status = %d", recorder.Code)
	}

	loginRecorder := server.login(t, "passoz", "segredo")
	session := cookieValue(loginRecorder.Result().Cookies(), adminCookieName)
	tampered := session[:len(session)-2] + "xy"
	request := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	request.AddCookie(&http.Cookie{Name: adminCookieName, Value: tampered})
	if recorder := server.do(request); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("cookie adulterado deveria ser recusado, status = %d", recorder.Code)
	}
}
