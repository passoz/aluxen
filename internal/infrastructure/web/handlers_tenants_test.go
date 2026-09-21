package web

import (
	"context"
	"encoding/json"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/config"
	"github.com/stretchr/testify/require"
)

// ── Dublês das portas de cadastro ──────────────────────────────────

type catalogTenants struct{ items map[string]domain.Tenant }

func newCatalogTenants(tenants ...domain.Tenant) *catalogTenants {
	repo := &catalogTenants{items: map[string]domain.Tenant{}}
	for _, tenant := range tenants {
		repo.items[tenant.ID] = tenant
	}
	return repo
}

func (r *catalogTenants) List(context.Context) ([]domain.Tenant, error) {
	tenants := make([]domain.Tenant, 0, len(r.items))
	for _, tenant := range r.items {
		tenants = append(tenants, tenant)
	}
	return tenants, nil
}

func (r *catalogTenants) ByID(_ context.Context, id string) (*domain.Tenant, error) {
	tenant, ok := r.items[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &tenant, nil
}

func (r *catalogTenants) ByCPF(context.Context, string) (*domain.Tenant, error) {
	return nil, domain.ErrNotFound
}

func (r *catalogTenants) Create(_ context.Context, tenant *domain.Tenant) error {
	if _, exists := r.items[tenant.ID]; exists {
		return domain.ErrConflict
	}
	r.items[tenant.ID] = *tenant
	return nil
}

func (r *catalogTenants) Update(_ context.Context, tenant *domain.Tenant) error {
	if _, exists := r.items[tenant.ID]; !exists {
		return domain.ErrNotFound
	}
	r.items[tenant.ID] = *tenant
	return nil
}

func (r *catalogTenants) UpdatePassword(context.Context, string, string, string, string) error {
	return nil
}

func (r *catalogTenants) Delete(_ context.Context, id string) error {
	if _, exists := r.items[id]; !exists {
		return domain.ErrNotFound
	}
	delete(r.items, id)
	return nil
}

func (r *catalogTenants) Count(context.Context) (int, error) { return len(r.items), nil }

type catalogProperties struct{ items map[string]domain.Property }

func newCatalogProperties(properties ...domain.Property) *catalogProperties {
	repo := &catalogProperties{items: map[string]domain.Property{}}
	for _, property := range properties {
		repo.items[property.ID] = property
	}
	return repo
}

func (r *catalogProperties) List(context.Context) ([]domain.Property, error) {
	properties := make([]domain.Property, 0, len(r.items))
	for _, property := range r.items {
		properties = append(properties, property)
	}
	return properties, nil
}

func (r *catalogProperties) ByID(_ context.Context, id string) (*domain.Property, error) {
	property, ok := r.items[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &property, nil
}

func (r *catalogProperties) Create(_ context.Context, property *domain.Property) error {
	if _, exists := r.items[property.ID]; exists {
		return domain.ErrConflict
	}
	r.items[property.ID] = *property
	return nil
}

func (r *catalogProperties) Update(_ context.Context, property *domain.Property) error {
	if _, exists := r.items[property.ID]; !exists {
		return domain.ErrNotFound
	}
	r.items[property.ID] = *property
	return nil
}

func (r *catalogProperties) Delete(_ context.Context, id string) error {
	if _, exists := r.items[id]; !exists {
		return domain.ErrNotFound
	}
	delete(r.items, id)
	return nil
}

func (r *catalogProperties) Count(context.Context) (int, error) { return len(r.items), nil }

// catalogLeases e catalogReceipts implementam apenas as leituras que o painel
// usa; o embed cobre o resto do port (nunca chamado aqui).
type catalogLeases struct {
	domain.LeaseRepository
	items []domain.Lease
}

func (r *catalogLeases) List(context.Context) ([]domain.Lease, error) { return r.items, nil }

func (r *catalogLeases) ByTenant(_ context.Context, tenantID string) ([]domain.Lease, error) {
	leases := make([]domain.Lease, 0, len(r.items))
	for _, lease := range r.items {
		if lease.TenantID == tenantID {
			leases = append(leases, lease)
		}
	}
	return leases, nil
}

type catalogReceipts struct {
	domain.ReceiptRepository
	items []domain.Receipt
}

func (r *catalogReceipts) List(context.Context) ([]domain.Receipt, error) { return r.items, nil }

// ── Painel de teste ────────────────────────────────────────────────

type catalogPanel struct {
	*testServer
	tenants    *catalogTenants
	properties *catalogProperties
	leases     *catalogLeases
}

func newCatalogPanel(t *testing.T, users ...*domain.User) *catalogPanel {
	t.Helper()

	userRepo := newFakeUsers(users...)
	auditRepo := &fakeAudit{}
	clock := &fakeClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	tenants := newCatalogTenants()
	properties := newCatalogProperties()
	leases := &catalogLeases{}

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
			Tenants:     tenants,
			Properties:  properties,
			Leases:      leases,
			Receipts:    &catalogReceipts{},
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

	return &catalogPanel{
		testServer: &testServer{handler: NewHandlers(common), audit: auditRepo, users: userRepo, clock: clock},
		tenants:    tenants,
		properties: properties,
		leases:     leases,
	}
}

// authenticate faz o login do administrador e devolve a sessão e o token CSRF
// emitido na renderização da aba de inquilinos.
func (p *catalogPanel) authenticate(t *testing.T) (session string, csrf string) {
	t.Helper()

	login := p.login(t, "passoz", "segredo")
	require.Equal(t, http.StatusFound, login.Code, "login do administrador deveria redirecionar")
	session = cookieValue(login.Result().Cookies(), adminCookieName)
	require.NotEmpty(t, session, "login sem cookie de sessão")

	page := httptest.NewRequest(http.MethodGet, "/dashboard?tab=tenants", nil)
	page.AddCookie(&http.Cookie{Name: adminCookieName, Value: session})
	recorder := p.do(page)
	require.Equal(t, http.StatusOK, recorder.Code, "aba de inquilinos deveria renderizar")

	csrf = cookieValue(recorder.Result().Cookies(), csrfCookieName)
	require.NotEmpty(t, csrf, "aba sem cookie de CSRF")
	return session, csrf
}

// mutation envia um POST /dashboard como o formulário do painel faria: o JS
// manda `Accept: application/json`, então o servidor responde JSON.
func (p *catalogPanel) mutation(t *testing.T, session, csrf string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	form.Set(csrfFieldName, csrf)
	request := httptest.NewRequest(http.MethodPost, "/dashboard", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.AddCookie(&http.Cookie{Name: adminCookieName, Value: session})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrf})
	return p.do(request)
}

// dashboardTab abre uma aba autenticada.
func (p *catalogPanel) dashboardTab(t *testing.T, session, tab string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/dashboard?tab="+tab, nil)
	request.AddCookie(&http.Cookie{Name: adminCookieName, Value: session})
	return p.do(request)
}

func catalogUser() *domain.User {
	changedAt := "2026-09-21 12:00:00"
	return &domain.User{ID: "u1", Username: "passoz", PasswordHash: "hash:segredo", PasswordChangedAt: &changedAt}
}

var modalFillPattern = regexp.MustCompile(`data-modal-fill='([^']*)'`)

// firstModalFill lê o JSON que o botão de edição entrega ao JS do painel.
func firstModalFill(t *testing.T, body string) map[string]string {
	t.Helper()

	match := modalFillPattern.FindStringSubmatch(body)
	require.NotNil(t, match, "nenhum botão de edição com data-modal-fill no HTML")

	var payload map[string]string
	require.NoError(t, json.Unmarshal([]byte(html.UnescapeString(match[1])), &payload),
		"data-modal-fill não é JSON válido: %q", match[1])
	return payload
}

func auditActionsOf(entries []domain.AuditLog) []string {
	actions := make([]string, 0, len(entries))
	for _, entry := range entries {
		actions = append(actions, entry.Action)
	}
	return actions
}

// mutationEntries devolve os eventos de cadastro, sem os acessos do login.
func (p *catalogPanel) mutationEntries() []domain.AuditLog {
	entries := make([]domain.AuditLog, 0, len(p.audit.entries))
	for _, entry := range p.audit.entries {
		if strings.HasPrefix(entry.Action, "auth.") {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// mutationError lê a mensagem de erro devolvida pelo POST /dashboard.
func mutationError(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()

	var payload struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload), "corpo inesperado: %s", recorder.Body.String())
	return payload.Error
}

// requireMutationOK confirma a resposta `{"ok":true}` do painel.
func requireMutationOK(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()

	require.Equal(t, http.StatusOK, recorder.Code, "corpo: %s", recorder.Body.String())
	var payload struct {
		OK bool `json:"ok"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload), "corpo inesperado: %s", recorder.Body.String())
	require.True(t, payload.OK, "resposta sem ok: %s", recorder.Body.String())
}

// ── Renderização das abas ──────────────────────────────────────────

func TestDashboardTenantsTabRendersWithSession(t *testing.T) {
	panel := newCatalogPanel(t, catalogUser())
	panel.tenants.items["tenant-1"] = domain.Tenant{
		ID: "tenant-1", Name: "Carlos Drummond", Email: "carlos@poesia.com.br",
		Phone: domain.StrPtr("(22) 99888-7766"), CPF: domain.StrPtr("111.222.333-44"),
	}
	panel.leases.items = []domain.Lease{{ID: "lease-1", TenantID: "tenant-1", PropertyID: "property-1", Status: domain.LeaseStatusActive}}

	session, _ := panel.authenticate(t)
	recorder := panel.dashboardTab(t, session, "tenants")
	require.Equal(t, http.StatusOK, recorder.Code)

	body := recorder.Body.String()
	for _, expected := range []string{
		"Gerenciar Inquilinos",
		"Novo inquilino",
		`data-modal-open="tenant-create-modal"`,
		`data-modal-open="tenant-edit-modal"`,
		`id="tenant-create-modal"`,
		`id="tenant-edit-modal"`,
		`data-filter="tenants"`,
		"data-table",
		"data-pagination",
		"Carlos Drummond",
		"carlos@poesia.com.br",
		"111.222.333-44",
		"(22) 99888-7766",
		"Em contrato",
		`name="intent" value="tenant.delete"`,
		`data-confirm="Excluir o inquilino Carlos Drummond?"`,
		`name="intent" value="tenant.create"`,
		`name="intent" value="tenant.update"`,
		`name="csrf_token"`,
	} {
		require.Contains(t, body, expected, "aba de inquilinos sem %q", expected)
	}

	fill := firstModalFill(t, body)
	require.Equal(t, map[string]string{
		"id":    "tenant-1",
		"name":  "Carlos Drummond",
		"email": "carlos@poesia.com.br",
		"cpf":   "111.222.333-44",
		"phone": "(22) 99888-7766",
	}, fill)
}

func TestDashboardTenantsTabRendersEmptyState(t *testing.T) {
	panel := newCatalogPanel(t, catalogUser())

	session, _ := panel.authenticate(t)
	recorder := panel.dashboardTab(t, session, "tenants")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Nenhum inquilino cadastrado.")
}

func TestDashboardPropertiesTabRendersWithSession(t *testing.T) {
	panel := newCatalogPanel(t, catalogUser())
	panel.properties.items["property-1"] = domain.Property{
		ID: "property-1", Name: "Casa de Praia 202", Address: "Av. Atlântica, 500",
		City: "Cabo Frio", State: "RJ", Zip: domain.StrPtr("28900-000"),
	}
	panel.leases.items = []domain.Lease{
		{ID: "lease-1", TenantID: "tenant-1", PropertyID: "property-1", Status: domain.LeaseStatusActive},
		{ID: "lease-2", TenantID: "tenant-2", PropertyID: "property-1", Status: domain.LeaseStatusTerminated},
	}

	session, _ := panel.authenticate(t)
	recorder := panel.dashboardTab(t, session, "properties")
	require.Equal(t, http.StatusOK, recorder.Code)

	body := recorder.Body.String()
	for _, expected := range []string{
		"Gerenciar Imóveis",
		"Novo imóvel",
		`id="property-create-modal"`,
		`id="property-edit-modal"`,
		`data-filter="properties"`,
		"Casa de Praia 202",
		"Av. Atlântica, 500",
		"Cabo Frio/RJ",
		"28900-000",
		`data-mask="cep"`,
		`name="intent" value="property.delete"`,
		`data-confirm="Excluir o imóvel Casa de Praia 202?"`,
		`name="intent" value="property.create"`,
		`name="intent" value="property.update"`,
	} {
		require.Contains(t, body, expected, "aba de imóveis sem %q", expected)
	}
	require.Contains(t, body, `<td data-label="Contratos">2</td>`, "coluna de contratos deveria contar os dois contratos do imóvel")

	fill := firstModalFill(t, body)
	require.Equal(t, map[string]string{
		"id":      "property-1",
		"name":    "Casa de Praia 202",
		"address": "Av. Atlântica, 500",
		"city":    "Cabo Frio",
		"state":   "RJ",
		"zip":     "28900-000",
	}, fill)
}

// ── Intents de cadastro ────────────────────────────────────────────

func TestCatalogIntentCreatesTenant(t *testing.T) {
	panel := newCatalogPanel(t, catalogUser())
	session, csrf := panel.authenticate(t)

	recorder := panel.mutation(t, session, csrf, url.Values{
		"intent": {"tenant.create"},
		"name":   {"Carlos Drummond"},
		"email":  {"carlos@poesia.com.br"},
		"phone":  {"22998887766"},
		"cpf":    {"11122233344"},
	})
	requireMutationOK(t, recorder)

	require.Len(t, panel.tenants.items, 1)
	for _, tenant := range panel.tenants.items {
		require.Equal(t, "Carlos Drummond", tenant.Name)
		require.Equal(t, "carlos@poesia.com.br", tenant.Email)
		require.Equal(t, "(22) 99888-7766", domain.Str(tenant.Phone))
		require.Equal(t, "111.222.333-44", domain.Str(tenant.CPF))
		require.Equal(t, "2026-09-21 12:00:00", tenant.CreatedAt)
	}

	entries := panel.mutationEntries()
	require.Len(t, entries, 1)
	entry := entries[0]
	require.Equal(t, domain.ActionTenantCreate, entry.Action)
	require.Equal(t, domain.EntityTenant, entry.Entity)
	require.Equal(t, "Carlos Drummond", domain.Str(entry.EntityLabel))
	require.Equal(t, `Inquilino "Carlos Drummond" cadastrado`, entry.Summary)
	require.Equal(t, "passoz", domain.Str(entry.ActorName))
	require.Equal(t, "u1", domain.Str(entry.ActorID))
}

func TestCatalogIntentRejectsInvalidTenantEmail(t *testing.T) {
	panel := newCatalogPanel(t, catalogUser())
	session, csrf := panel.authenticate(t)

	recorder := panel.mutation(t, session, csrf, url.Values{
		"intent": {"tenant.create"},
		"name":   {"Email Inválido"},
		"email":  {"sem-arroba.teste"},
	})
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "Informe um e-mail válido.", mutationError(t, recorder))
	require.Empty(t, panel.tenants.items)
	require.Empty(t, panel.mutationEntries())
}

func TestCatalogIntentBlocksTenantDeletionWithActiveLease(t *testing.T) {
	panel := newCatalogPanel(t, catalogUser())
	panel.tenants.items["tenant-1"] = domain.Tenant{ID: "tenant-1", Name: "Carlos Drummond", Email: "carlos@poesia.com.br"}
	panel.leases.items = []domain.Lease{{ID: "lease-1", TenantID: "tenant-1", PropertyID: "property-1", Status: domain.LeaseStatusActive}}
	session, csrf := panel.authenticate(t)

	recorder := panel.mutation(t, session, csrf, url.Values{
		"intent": {"tenant.delete"},
		"id":     {"tenant-1"},
	})
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "Não é possível excluir inquilino com contrato ativo. Encerre o contrato primeiro.", mutationError(t, recorder))
	require.Contains(t, panel.tenants.items, "tenant-1")
	require.Empty(t, panel.mutationEntries())
}

func TestCatalogIntentUpdatesAndDeletesTenant(t *testing.T) {
	panel := newCatalogPanel(t, catalogUser())
	panel.tenants.items["tenant-1"] = domain.Tenant{
		ID: "tenant-1", Name: "Carlos Drummond", Email: "carlos@poesia.com.br",
		CPF: domain.StrPtr("111.222.333-44"), CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
	}
	session, csrf := panel.authenticate(t)

	update := panel.mutation(t, session, csrf, url.Values{
		"intent": {"tenant.update"},
		"id":     {"tenant-1"},
		"name":   {"Carlos Drummond de Andrade"},
		"email":  {"carlos@poesia.com.br"},
		"cpf":    {"111.222.333-44"},
	})
	requireMutationOK(t, update)
	require.Equal(t, "Carlos Drummond de Andrade", panel.tenants.items["tenant-1"].Name)

	remove := panel.mutation(t, session, csrf, url.Values{
		"intent": {"tenant.delete"},
		"id":     {"tenant-1"},
	})
	requireMutationOK(t, remove)
	require.Empty(t, panel.tenants.items)

	require.Equal(t, []string{domain.ActionTenantUpdate, domain.ActionTenantDelete}, auditActionsOf(panel.mutationEntries()))
}

func TestCatalogIntentManagesProperty(t *testing.T) {
	panel := newCatalogPanel(t, catalogUser())
	session, csrf := panel.authenticate(t)

	create := panel.mutation(t, session, csrf, url.Values{
		"intent":  {"property.create"},
		"id":      {"property-1"},
		"name":    {"Casa de Praia 202"},
		"address": {"Av. Atlântica, 500"},
		"city":    {"Cabo Frio"},
		"state":   {"RJ"},
		"zip":     {"28900000"},
	})
	requireMutationOK(t, create)
	require.Equal(t, "28900-000", domain.Str(panel.properties.items["property-1"].Zip))

	update := panel.mutation(t, session, csrf, url.Values{
		"intent":  {"property.update"},
		"id":      {"property-1"},
		"name":    {"Casa de Praia 202"},
		"address": {"Av. Atlântica, 500"},
		"city":    {"Arraial do Cabo"},
		"state":   {"RJ"},
	})
	requireMutationOK(t, update)
	require.Equal(t, "Arraial do Cabo", panel.properties.items["property-1"].City)

	remove := panel.mutation(t, session, csrf, url.Values{
		"intent": {"property.delete"},
		"id":     {"property-1"},
	})
	requireMutationOK(t, remove)
	require.Empty(t, panel.properties.items)

	require.Equal(t,
		[]string{domain.ActionPropertyCreate, domain.ActionPropertyUpdate, domain.ActionPropertyDelete},
		auditActionsOf(panel.mutationEntries()))
}

func TestCatalogIntentRejectsMissingPropertyFields(t *testing.T) {
	panel := newCatalogPanel(t, catalogUser())
	session, csrf := panel.authenticate(t)

	recorder := panel.mutation(t, session, csrf, url.Values{
		"intent": {"property.create"},
		"name":   {"Casa sem endereço"},
	})
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "Preencha nome, endereço, cidade e estado.", mutationError(t, recorder))
	require.Empty(t, panel.properties.items)
	require.Empty(t, panel.mutationEntries())
}

func TestCatalogIntentRejectsUnknownIntent(t *testing.T) {
	panel := newCatalogPanel(t, catalogUser())
	session, csrf := panel.authenticate(t)

	recorder := panel.mutation(t, session, csrf, url.Values{"intent": {"tenant.arquivar"}})
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "Ação inválida.", mutationError(t, recorder))
}
