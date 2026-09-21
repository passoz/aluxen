package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/config"
	"github.com/passoz/aluxen/internal/infrastructure/db"
)

// ── Montagem do servidor de teste sobre um SQLite real ─────────────

type auditTestServer struct {
	handlers *Handlers
	store    *db.Store
}

func newAuditTestServer(t *testing.T) *auditTestServer {
	t.Helper()

	ctx := context.Background()
	store, err := db.Open(ctx, ":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	require.NoError(t, store.Users.Create(ctx, &domain.User{
		ID: "admin-1", Username: "passoz", PasswordHash: "hash:segredo",
		PasswordChangedAt: domain.StrPtr("2026-01-01 00:00:00"), CreatedAt: "2026-01-01 00:00:00",
	}))

	clock := &fakeClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	deps := application.Deps{
		Repos: application.Repositories{
			Users:       store.Users,
			Tenants:     store.Tenants,
			Properties:  store.Properties,
			Leases:      store.Leases,
			Receipts:    store.Receipts,
			Settings:    store.Settings,
			Audit:       store.Audit,
			Inspections: &emptyInspections{},
		},
		Clock:  clock,
		IDs:    &fakeIDs{},
		Hasher: fakeHasher{},
		Log:    slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelError})),
	}

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

	renderer, err := NewRenderer()
	require.NoError(t, err)

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
	return &auditTestServer{handlers: NewHandlers(common), store: store}
}

func (s *auditTestServer) do(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	s.handlers.Router().ServeHTTP(recorder, request)
	return recorder
}

// login autentica o administrador semeado e devolve os cookies (sessão + CSRF).
func (s *auditTestServer) login(t *testing.T) []*http.Cookie {
	t.Helper()

	page := httptest.NewRecorder()
	s.handlers.Router().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/login", nil))
	csrf := cookieValue(page.Result().Cookies(), csrfCookieName)
	require.NotEmpty(t, csrf, "cookie de CSRF emitido no GET /login")

	form := url.Values{"username": {"passoz"}, "password": {"segredo"}, csrfFieldName: {csrf}}
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range page.Result().Cookies() {
		request.AddCookie(cookie)
	}
	response := s.do(request).Result()
	require.NotEmpty(t, cookieValue(response.Cookies(), adminCookieName), "sessão emitida no login")

	return append(page.Result().Cookies(), response.Cookies()...)
}

func (s *auditTestServer) get(t *testing.T, target string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	return s.do(request)
}

func (s *auditTestServer) auditEntries(t *testing.T) []domain.AuditLog {
	t.Helper()
	entries, err := s.store.Audit.List(context.Background(), domain.AuditFilter{Limit: 100, Descending: true})
	require.NoError(t, err)
	return entries
}

func (s *auditTestServer) auditActions(t *testing.T) []string {
	t.Helper()
	actions := []string{}
	for _, entry := range s.auditEntries(t) {
		actions = append(actions, entry.Action)
	}
	return actions
}

func seedServerAudit(t *testing.T, server *auditTestServer, entries ...*domain.AuditLog) {
	t.Helper()
	for _, entry := range entries {
		require.NoError(t, server.store.Audit.Insert(context.Background(), entry))
	}
}

// ── GET /audit ─────────────────────────────────────────────────────

func TestDashboardHistoryTabRendersShell(t *testing.T) {
	server := newAuditTestServer(t)
	cookies := server.login(t)

	recorder := server.get(t, "/dashboard?tab=history", cookies)
	require.Equal(t, http.StatusOK, recorder.Code)

	html := recorder.Body.String()
	assert.Contains(t, html, "Histórico e Auditoria")
	assert.Contains(t, html, "Trilha de auditoria")
	assert.Contains(t, html, "history-table")
	assert.Contains(t, html, "Exportar CSV")
	assert.Contains(t, html, `id="history-detail"`)
	assert.Contains(t, html, `fetch('/audit?'`, "a aba carrega a trilha por fetch em GET /audit")
	assert.Contains(t, html, `data-entity="tenant"`)
	assert.Contains(t, html, `value="tenant.create"`)
}

func TestAuditListRequiresAuthentication(t *testing.T) {
	server := newAuditTestServer(t)

	recorder := server.do(httptest.NewRequest(http.MethodGet, "/audit", nil))
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Não autorizado.")
}

func TestAuditListReturnsFilteredPage(t *testing.T) {
	server := newAuditTestServer(t)
	cookies := server.login(t)

	seedServerAudit(t, server,
		&domain.AuditLog{
			ID: "audit-tenant", CreatedAt: "2026-03-05T08:00:00.000", Action: domain.ActionTenantCreate,
			Entity: domain.EntityTenant, EntityID: domain.StrPtr("tenant-1"), EntityLabel: domain.StrPtr("Carlos"),
			ActorName: domain.StrPtr("passoz"), Summary: "Inquilino cadastrado",
			Changes: domain.StrPtr(`[{"field":"name","label":"Nome","from":null,"to":"Carlos"}]`),
			Origin:  domain.AuditOriginWeb, IP: domain.StrPtr("127.0.0.1"),
		},
		&domain.AuditLog{
			ID: "audit-receipt", CreatedAt: "2026-03-05T09:00:00.000", Action: domain.ActionReceiptPay,
			Entity: domain.EntityReceipt, Summary: "Baixa registrada", ActorName: domain.StrPtr("passoz"),
			Origin: domain.AuditOriginWeb,
		},
	)

	recorder := server.get(t, "/audit?entity=tenant&page=1&pageSize=5", cookies)
	require.Equal(t, http.StatusOK, recorder.Code)

	var page application.AuditPage
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &page))
	assert.Equal(t, 1, page.Total)
	assert.Equal(t, 1, page.Page)
	assert.Equal(t, 5, page.PageSize)
	assert.Equal(t, []string{"passoz"}, page.Actors)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "audit-tenant", page.Items[0].ID)
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}$`, page.Items[0].CreatedAt)
	assert.Equal(t, "Inquilino", domain.EntityLabel(page.Items[0].Entity))
	require.Len(t, page.Items[0].Changes, 1)
	assert.Nil(t, page.Items[0].Changes[0].From)
}

// ── GET /audit/export ──────────────────────────────────────────────

func TestAuditExportStreamsCSVAndRecordsAudit(t *testing.T) {
	server := newAuditTestServer(t)
	cookies := server.login(t)

	seedServerAudit(t, server,
		&domain.AuditLog{
			ID: "audit-tenant", CreatedAt: "2026-03-05T08:00:00.000", Action: domain.ActionTenantCreate,
			Entity: domain.EntityTenant, EntityLabel: domain.StrPtr("Carlos"), ActorName: domain.StrPtr("passoz"),
			Summary: "Inquilino cadastrado", Origin: domain.AuditOriginWeb,
			Changes: domain.StrPtr(`[{"field":"name","label":"Nome","from":null,"to":"Carlos"}]`),
		},
		&domain.AuditLog{
			ID: "audit-lease", CreatedAt: "2026-03-05T09:00:00.000", Action: domain.ActionLeaseCreate,
			Entity: domain.EntityLease, Summary: "Contrato criado", Origin: domain.AuditOriginWeb,
		},
	)

	recorder := server.get(t, "/audit/export?entity=tenant", cookies)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Content-Type"), "text/csv")
	assert.Contains(t, recorder.Header().Get("Content-Disposition"), `attachment; filename="aluxen-historico-2026-09-21.csv"`)

	csv := recorder.Body.String()
	assert.True(t, strings.HasPrefix(csv, "\uFEFF"), "CSV começa com BOM")
	assert.Contains(t, csv, `"Data/Hora";"Usuário";"Ação";"Área";"Registro";"ID do registro";"Resumo";"Alterações";"Metadados";"Origem";"IP";"User-Agent";"Request"`)
	assert.Contains(t, csv, "tenant.create")
	assert.NotContains(t, csv, "lease.create")
	assert.Contains(t, csv, `"Inquilino"`)
	assert.Contains(t, csv, `"Nome: — → Carlos"`)

	assert.Contains(t, server.auditActions(t), domain.ActionHistoryExport)
	exported, err := server.store.Audit.List(context.Background(), domain.AuditFilter{Action: domain.ActionHistoryExport, Limit: 5})
	require.NoError(t, err)
	require.Len(t, exported, 1)
	assert.Contains(t, exported[0].Summary, "1 evento(s)")
}

// ── GET /export e POST /import ─────────────────────────────────────

func TestBackupExportDownloadsJSONAndRecordsAudit(t *testing.T) {
	server := newAuditTestServer(t)
	cookies := server.login(t)

	tenant := &domain.Tenant{ID: "tenant-1", Name: "Carlos", Email: "carlos@aluxen.test", CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00"}
	require.NoError(t, server.store.Tenants.Create(context.Background(), tenant))

	recorder := server.get(t, "/export", cookies)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, recorder.Header().Get("Content-Disposition"), `attachment; filename="aluxen-backup-2026-09-21.json"`)

	var backup application.Backup
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &backup))
	assert.Equal(t, "2026-09-21T12:00:00.000Z", backup.ExportedAt)
	require.Len(t, backup.Tenants, 1)
	assert.Equal(t, "Carlos", backup.Tenants[0].Name)

	assert.Contains(t, server.auditActions(t), domain.ActionBackupExport)
}

func TestBackupImportRestoresAndRedirects(t *testing.T) {
	server := newAuditTestServer(t)
	cookies := server.login(t)

	payload := `{
		"exportedAt": "2026-09-21T12:00:00.000Z",
		"tenants": [{"id": "t1", "name": "Locatário A", "email": "a@aluxen.test", "createdAt": "2026-01-01 00:00:00", "updatedAt": "2026-01-02 00:00:00"}],
		"properties": [{"id": "p1", "name": "Imóvel A", "address": "Rua 1", "city": "Cabo Frio", "state": "RJ", "createdAt": "2026-01-01 00:00:00", "updatedAt": "2026-01-02 00:00:00"}],
		"leases": [{"id": "l1", "tenantId": "t1", "propertyId": "p1", "startDate": "2026-01-01", "endDate": "2026-12-31", "dueDay": 5, "rentAmount": 900, "createdAt": "2026-01-01 00:00:00", "updatedAt": "2026-01-02 00:00:00"}],
		"receipts": [{"id": "r1", "leaseId": "l1", "dueDate": "2026-02-10", "amount": 900, "createdAt": "2026-01-05 00:00:00"}]
	}`

	request := multipartRequest(t, "/import", cookies, "backup.json", payload)
	recorder := server.do(request)
	require.Equal(t, http.StatusFound, recorder.Code)
	assert.Equal(t, "/dashboard?tab=dashboard&toast=importado", recorder.Header().Get("Location"))

	tenants, err := server.store.Tenants.List(context.Background())
	require.NoError(t, err)
	require.Len(t, tenants, 1)
	assert.NotEqual(t, "t1", tenants[0].ID)

	leases, err := server.store.Leases.List(context.Background())
	require.NoError(t, err)
	require.Len(t, leases, 1)
	assert.Equal(t, tenants[0].ID, leases[0].TenantID, "FK remapeada na importação")

	imported, err := server.store.Audit.List(context.Background(), domain.AuditFilter{Action: domain.ActionBackupImport, Limit: 5})
	require.NoError(t, err)
	require.Len(t, imported, 1)
	assert.Contains(t, imported[0].Summary, "1 inquilino(s)")
	assert.Contains(t, imported[0].Summary, "1 pagamento(s)")
	assert.Equal(t, "backup.json", domain.Str(imported[0].EntityLabel))
	assert.JSONEq(t, `{"fileName":"backup.json","tenants":1,"properties":1,"leases":1,"receipts":1}`, domain.Str(imported[0].Metadata))
}

func TestBackupImportRejectsInvalidPayload(t *testing.T) {
	server := newAuditTestServer(t)
	cookies := server.login(t)

	recorder := server.do(multipartRequest(t, "/import", cookies, "backup.json", `{}`))
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Formato de backup inválido.")
}

func TestBackupImportRejectsMissingFile(t *testing.T) {
	server := newAuditTestServer(t)
	cookies := server.login(t)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField(csrfFieldName, csrfFromCookies(cookies)))
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, "/import", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	recorder := server.do(request)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Arquivo não enviado.")
}

func multipartRequest(t *testing.T, target string, cookies []*http.Cookie, filename, content string) *http.Request {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField(csrfFieldName, csrfFromCookies(cookies)))
	part, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = io.WriteString(part, content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, target, body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	return request
}

func csrfFromCookies(cookies []*http.Cookie) string {
	return cookieValue(cookies, csrfCookieName)
}
