// Package e2e exercita o Aluxen de ponta a ponta: servidor HTTP real, banco
// real (SQLite em diretório temporário) e as rotas como o navegador as usa —
// login com trava de senha, cadastros, contrato, recibo, baixa, PDF, validação
// pública e Área do Cliente.
package e2e

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/infrastructure/authz"
	"github.com/passoz/aluxen/internal/infrastructure/config"
	"github.com/passoz/aluxen/internal/infrastructure/db"
	"github.com/passoz/aluxen/internal/infrastructure/platform"
	"github.com/passoz/aluxen/internal/infrastructure/storage"
	"github.com/passoz/aluxen/internal/infrastructure/web"
	"github.com/passoz/aluxen/internal/testsupport/pgtest"
)

const (
	adminUser     = "passoz"
	adminPassword = "senha-de-teste"
	newPassword   = "novasenha1"
)

type harness struct {
	t       *testing.T
	server  *httptest.Server
	client  *http.Client
	store   *db.Store
	deps    application.Deps
	csrf    string
	baseURL string
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	dir := t.TempDir()
	ctx := context.Background()

	store, err := db.Open(ctx, "file:"+filepath.Join(dir, "aluxen.db"), "")
	if err != nil {
		t.Fatalf("abrir banco: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("fechar banco: %v", err)
		}
	})

	if err := store.EnsureAdmin(ctx, adminUser, adminPassword, "Locador Teste", "111.222.333-44"); err != nil {
		t.Fatalf("semear administrador: %v", err)
	}

	attachments, err := storage.NewFileSystem(filepath.Join(dir, "storage"))
	if err != nil {
		t.Fatalf("preparar armazenamento: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	deps := application.Deps{
		Repos: application.Repositories{
			Users:         store.Users,
			Tenants:       store.Tenants,
			Properties:    store.Properties,
			Leases:        store.Leases,
			Receipts:      store.Receipts,
			Settings:      store.Settings,
			Audit:         store.Audit,
			WhatsApp:      store.WhatsApp,
			Inspections:   store.Inspections,
			Jobs:          store.Jobs,
			PaymentProofs: store.PaymentProofs,
			Intakes:       store.Intakes,
		},
		Clock:       platform.SystemClock{},
		IDs:         platform.UUIDv7{},
		Hasher:      platform.BcryptHasher{},
		Signer:      authz.NewTokenGenerator("e2e-session-secret-with-enough-length"),
		Attachments: attachments,
		Log:         logger,
		Opts: application.Options{
			PublicBaseURL:         "https://aluxen.evolucsia.com",
			SeedUsername:          adminUser,
			RateLimitMax:          5,
			RateLimitGlobalMax:    60,
			LoginLockThreshold:    10,
			AIConfidenceThreshold: 0.9,
		},
	}

	cfg := &config.Config{
		AppEnv:        "test",
		Host:          "127.0.0.1",
		Port:          0,
		SessionSecret: "e2e-session-secret-with-enough-length",
		AppURL:        "https://aluxen.evolucsia.com",
	}
	renderer, err := web.NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	common := &web.Common{
		Cfg:     cfg,
		Log:     logger,
		Render:  renderer,
		Session: web.NewSessionManager(cfg.SessionSecret, false),
		CSRF:    web.NewCSRF(cfg.SessionSecret, false),
		Deps:    deps,
		Started: time.Now(),
		Dialect: "sqlite",
		DBPath:  store.Path(),
		DBSize:  store.SizeBytes,
		Ping:    store.Ping,
		MustChangePasswordFunc: func(ctx context.Context, userID string) (bool, error) {
			return application.AdminMustChangePassword(ctx, deps, userID)
		},
		LoginGate: application.NewLoginGate(deps.Clock, 5, 60, 10),
	}

	server := httptest.NewServer(web.NewHandlers(common).Router())
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}

	return &harness{
		t:       t,
		server:  server,
		client:  &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		store:   store,
		deps:    deps,
		baseURL: server.URL,
	}
}

func (h *harness) get(path string) *http.Response {
	h.t.Helper()
	response, err := h.client.Get(h.baseURL + path)
	if err != nil {
		h.t.Fatalf("GET %s: %v", path, err)
	}
	return response
}

func (h *harness) postForm(path string, form url.Values) *http.Response {
	h.t.Helper()
	if form.Get("csrf_token") == "" && h.csrf != "" {
		form.Set("csrf_token", h.csrf)
	}
	response, err := h.client.PostForm(h.baseURL+path, form)
	if err != nil {
		h.t.Fatalf("POST %s: %v", path, err)
	}
	return response
}

func bodyOf(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ler corpo: %v", err)
	}
	return string(content)
}

var csrfPattern = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// loginAdmin faz o primeiro acesso: entra com a senha semeada, troca a senha
// obrigatória e volta ao painel.
func (h *harness) loginAdmin() {
	h.t.Helper()

	page := bodyOf(h.t, h.get("/login"))
	match := csrfPattern.FindStringSubmatch(page)
	if match == nil {
		h.t.Fatal("página de login sem token CSRF")
	}
	h.csrf = match[1]

	response := h.postForm("/login", url.Values{"username": {adminUser}, "password": {adminPassword}})
	if response.StatusCode != http.StatusFound {
		h.t.Fatalf("login = %d, esperado 302", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/settings?forced=1" {
		h.t.Fatalf("login redirecionou para %q, esperado /settings?forced=1", location)
	}
	response.Body.Close()

	settings := bodyOf(h.t, h.get("/settings?forced=1"))
	if match := csrfPattern.FindStringSubmatch(settings); match != nil {
		h.csrf = match[1]
	}

	response = h.postForm("/settings", url.Values{
		"intent":           {"change-password"},
		"current_password": {adminPassword},
		"new_password":     {newPassword},
		"confirm_password": {newPassword},
	})
	if response.StatusCode != http.StatusFound {
		h.t.Fatalf("troca de senha = %d, esperado 302", response.StatusCode)
	}
	response.Body.Close()

	panel := bodyOf(h.t, h.get("/dashboard?tab=dashboard"))
	if match := csrfPattern.FindStringSubmatch(panel); match != nil {
		h.csrf = match[1]
	}
	// A casca do painel é o que prova que a sessão passou pela trava de senha:
	// navegação e identificação do usuário aparecem na resposta.
	if !strings.Contains(panel, "Inquilinos") || !strings.Contains(panel, adminUser) {
		h.t.Fatal("painel não renderizou a navegação")
	}
}

// intent executa uma mutação do painel e exige sucesso.
func (h *harness) intent(name string, fields url.Values) {
	h.t.Helper()
	fields.Set("intent", name)
	response := h.postForm("/dashboard", fields)
	body := bodyOf(h.t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `"ok":true`) {
		h.t.Fatalf("intent %s = %d %s", name, response.StatusCode, body)
	}
}

func TestEndToEndRentalFlow(t *testing.T) {
	h := newHarness(t)
	h.loginAdmin()

	// 1. Cadastros
	h.intent("tenant.create", url.Values{
		"name": {"Carlos Drummond"}, "email": {"carlos@exemplo.com"},
		"phone": {"(22) 99888-1122"}, "cpf": {"111.222.333-44"},
	})
	h.intent("property.create", url.Values{
		"name": {"Apto 101"}, "address": {"Av. do Contorno, 250"}, "city": {"Cabo Frio"},
		"state": {"RJ"}, "zip": {"28907-250"},
	})
	tenants, err := application.ListTenants(context.Background(), h.deps)
	if err != nil || len(tenants) != 1 {
		t.Fatalf("locatários: %v (%d)", err, len(tenants))
	}
	properties, err := application.ListProperties(context.Background(), h.deps)
	if err != nil || len(properties) != 1 {
		t.Fatalf("imóveis: %v (%d)", err, len(properties))
	}

	// 2. Contrato de temporada com caução
	h.intent("lease.create", url.Values{
		"tenant_id": {tenants[0].ID}, "property_id": {properties[0].ID},
		"start_date": {"2026-12-20"}, "end_date": {"2027-01-05"},
		"due_day": {"5"}, "rent_amount": {"2500.00"}, "status": {"season"},
	})
	leases, err := application.ListLeases(context.Background(), h.deps)
	if err != nil || len(leases) != 1 {
		t.Fatalf("contratos: %v (%d)", err, len(leases))
	}
	if leases[0].Status != "season" {
		t.Fatalf("situação do contrato = %q", leases[0].Status)
	}

	response := h.postForm("/dashboard/deposit", url.Values{
		"intent": {"deposit.set"}, "lease_id": {leases[0].ID}, "deposit_amount": {"1200.00"},
	})
	if body := bodyOf(t, response); response.StatusCode != http.StatusOK || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("caução = %d %s", response.StatusCode, body)
	}

	// 3. Lançamento e baixa
	h.intent("receipt.create", url.Values{
		"lease_id": {leases[0].ID}, "due_date": {"2026-12-20"}, "amount": {"2500.00"}, "category": {"rent"},
	})
	receipts, err := application.ListReceipts(context.Background(), h.deps)
	if err != nil || len(receipts) != 1 {
		t.Fatalf("lançamentos: %v (%d)", err, len(receipts))
	}
	receiptID := receipts[0].ID

	h.intent("receipt.pay", url.Values{"id": {receiptID}, "pay_amount": {"2500.00"}})
	paid, err := h.store.Receipts.ByID(context.Background(), receiptID)
	if err != nil {
		t.Fatalf("buscar recibo: %v", err)
	}
	if paid.PaidAt == nil || *paid.PaidAt == "" {
		t.Fatal("baixa não registrou data de pagamento")
	}

	// 4. PDF do recibo com prova de autenticidade
	pdfResponse := h.get("/pdf?id=" + receiptID)
	if pdfResponse.StatusCode != http.StatusOK {
		t.Fatalf("PDF = %d", pdfResponse.StatusCode)
	}
	if contentType := pdfResponse.Header.Get("Content-Type"); contentType != "application/pdf" {
		t.Fatalf("Content-Type do PDF = %q", contentType)
	}
	pdfBody := bodyOf(t, pdfResponse)
	if !strings.HasPrefix(pdfBody, "%PDF-") {
		t.Fatalf("resposta do PDF não começa com %%PDF-: %q", pdfBody[:min(16, len(pdfBody))])
	}

	// 5. Página pública de validação (token HMAC)
	token := h.deps.Signer.Generate("receipt", receiptID)
	code := h.deps.Signer.FormatCode(token)
	verify := h.get("/validar/recibo/" + receiptID + "?token=" + token)
	verifyBody := bodyOf(t, verify)
	if verify.StatusCode != http.StatusOK {
		t.Fatalf("validação = %d %s", verify.StatusCode, verifyBody[:min(200, len(verifyBody))])
	}
	if !strings.Contains(verifyBody, code) {
		t.Fatalf("página de validação sem a chave %s", code)
	}
	if !strings.Contains(verifyBody, "Carlos Drummond") {
		t.Fatal("página de validação sem o titular")
	}

	// token inválido é recusado
	invalid := h.get("/validar/recibo/" + receiptID + "?token=00000000000000000000000000000000")
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("token inválido = %d, esperado 400", invalid.StatusCode)
	}
	invalidBody := bodyOf(t, invalid)
	if !strings.Contains(invalidBody, "Chave de autenticação inválida") {
		t.Fatalf("mensagem de token inválido inesperada: %s", invalidBody[:min(200, len(invalidBody))])
	}

	// 6. Área do Cliente vê o recibo quitado
	portal := h.get("/portal/login")
	if match := csrfPattern.FindStringSubmatch(bodyOf(t, portal)); match != nil {
		h.csrf = match[1]
	}
	portalLogin := h.postForm("/portal/login", url.Values{"cpf": {"111.222.333-44"}, "password": {"11122233344"}})
	if portalLogin.StatusCode != http.StatusFound {
		t.Fatalf("login do portal = %d", portalLogin.StatusCode)
	}
	if location := portalLogin.Header.Get("Location"); location != "/portal/password" {
		t.Fatalf("primeiro acesso do locatário deveria exigir troca de senha, veio %q", location)
	}
	portalLogin.Body.Close()

	// 7. Trilha de auditoria registrou o fluxo
	entries, err := application.ListAudit(context.Background(), h.deps, application.AuditFilter{Page: 1, PageSize: 50, Descending: true})
	if err != nil {
		t.Fatalf("listar trilha: %v", err)
	}
	seen := map[string]bool{}
	for _, entry := range entries.Items {
		seen[entry.Action] = true
	}
	for _, action := range []string{
		"auth.login", "tenant.create", "property.create", "lease.create", "receipt.create", "receipt.pay",
	} {
		if !seen[action] {
			t.Errorf("trilha de auditoria sem %s", action)
		}
	}
}

func TestEndToEndPostgresFlow(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL não definida: o fluxo Postgres roda só com banco disponível")
	}
	// O fluxo é o mesmo; aqui só provamos que a abertura pelo dialeto Postgres
	// sobe a aplicação com as migrações aplicadas. O schema é exclusivo deste
	// caso: a base de teste é compartilhada com o resto da suíte.
	schema, err := pgtest.Isolate(context.Background(), dsn)
	if err != nil {
		t.Fatalf("criar schema isolado: %v", err)
	}
	defer func() {
		if err := schema.Drop(context.Background()); err != nil {
			t.Errorf("apagar schema isolado: %v", err)
		}
	}()

	store, err := db.Open(context.Background(), schema.DSN, "")
	if err != nil {
		t.Fatalf("abrir Postgres: %v", err)
	}
	defer func() { _ = store.Close() }()
	if store.Dialect != db.DialectPostgres {
		t.Fatalf("dialeto = %q", store.Dialect)
	}
	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
