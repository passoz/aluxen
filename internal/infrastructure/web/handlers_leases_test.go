package web

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
)

// ── Dublês das portas de contrato ──────────────────────────────────
//
// O embed da porta cobre os métodos que o painel não usa neste fluxo; só as
// leituras e escritas exercitadas pelos intents de contrato são implementadas.

type leaseTenants struct {
	domain.TenantRepository
	items map[string]domain.Tenant
}

func newLeaseTenants(tenants ...domain.Tenant) *leaseTenants {
	repo := &leaseTenants{items: map[string]domain.Tenant{}}
	for _, tenant := range tenants {
		repo.items[tenant.ID] = tenant
	}
	return repo
}

func (r *leaseTenants) List(context.Context) ([]domain.Tenant, error) {
	out := make([]domain.Tenant, 0, len(r.items))
	for _, tenant := range r.items {
		out = append(out, tenant)
	}
	return out, nil
}

func (r *leaseTenants) ByID(_ context.Context, id string) (*domain.Tenant, error) {
	tenant, ok := r.items[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &tenant, nil
}

func (r *leaseTenants) Count(context.Context) (int, error) { return len(r.items), nil }

type leaseProperties struct {
	domain.PropertyRepository
	items map[string]domain.Property
}

func newLeaseProperties(properties ...domain.Property) *leaseProperties {
	repo := &leaseProperties{items: map[string]domain.Property{}}
	for _, property := range properties {
		repo.items[property.ID] = property
	}
	return repo
}

func (r *leaseProperties) List(context.Context) ([]domain.Property, error) {
	out := make([]domain.Property, 0, len(r.items))
	for _, property := range r.items {
		out = append(out, property)
	}
	return out, nil
}

func (r *leaseProperties) ByID(_ context.Context, id string) (*domain.Property, error) {
	property, ok := r.items[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &property, nil
}

func (r *leaseProperties) Count(context.Context) (int, error) { return len(r.items), nil }

type leaseLeases struct {
	domain.LeaseRepository
	items      map[string]domain.Lease
	order      []string
	tenants    *leaseTenants
	properties *leaseProperties
}

func newLeaseLeases(tenants *leaseTenants, properties *leaseProperties) *leaseLeases {
	return &leaseLeases{items: map[string]domain.Lease{}, tenants: tenants, properties: properties}
}

// seed insere um contrato direto no repositório, sem passar pelo caso de uso.
func (r *leaseLeases) seed(lease domain.Lease) {
	r.items[lease.ID] = lease
	r.order = append(r.order, lease.ID)
}

func (r *leaseLeases) named(lease domain.Lease) domain.Lease {
	if tenant, ok := r.tenants.items[lease.TenantID]; ok {
		lease.TenantName = tenant.Name
	}
	if property, ok := r.properties.items[lease.PropertyID]; ok {
		lease.PropertyName = property.Name
	}
	return lease
}

func (r *leaseLeases) List(context.Context) ([]domain.Lease, error) {
	out := make([]domain.Lease, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.named(r.items[id]))
	}
	return out, nil
}

func (r *leaseLeases) ByID(_ context.Context, id string) (*domain.Lease, error) {
	lease, ok := r.items[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	out := r.named(lease)
	return &out, nil
}

func (r *leaseLeases) Overlapping(_ context.Context, propertyID, excludeID, startDate, endDate string) ([]domain.Lease, error) {
	out := []domain.Lease{}
	for _, id := range r.order {
		lease := r.items[id]
		if lease.PropertyID != propertyID || lease.ID == excludeID || !lease.Status.BlocksOverlap() {
			continue
		}
		if domain.Overlaps(lease.StartDate, lease.EndDate, startDate, endDate) {
			out = append(out, r.named(lease))
		}
	}
	return out, nil
}

func (r *leaseLeases) Create(_ context.Context, lease *domain.Lease) error {
	r.seed(*lease)
	return nil
}

func (r *leaseLeases) Update(_ context.Context, lease *domain.Lease) error {
	if _, ok := r.items[lease.ID]; !ok {
		return domain.ErrNotFound
	}
	r.items[lease.ID] = *lease
	return nil
}

func (r *leaseLeases) UpdateStatus(_ context.Context, id string, status domain.LeaseStatus, updatedAt string) error {
	lease, ok := r.items[id]
	if !ok {
		return domain.ErrNotFound
	}
	lease.Status = status
	lease.UpdatedAt = updatedAt
	r.items[id] = lease
	return nil
}

func (r *leaseLeases) Delete(_ context.Context, id string) error {
	if _, ok := r.items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(r.items, id)
	for index, candidate := range r.order {
		if candidate == id {
			r.order = append(r.order[:index], r.order[index+1:]...)
			break
		}
	}
	return nil
}

func (r *leaseLeases) Count(context.Context) (int, error) { return len(r.items), nil }

func (r *leaseLeases) CountByStatus(_ context.Context, status domain.LeaseStatus) (int, error) {
	total := 0
	for _, lease := range r.items {
		if lease.Status == status {
			total++
		}
	}
	return total, nil
}

type leaseReceipts struct {
	domain.ReceiptRepository
	items []domain.Receipt
}

func (r *leaseReceipts) List(context.Context) ([]domain.Receipt, error) { return r.items, nil }

func (r *leaseReceipts) Count(context.Context) (int, error) { return len(r.items), nil }

// leaseInspections cobre a leitura de vistorias que o painel carrega em toda
// requisição (visão de temporada); os contratos deste fluxo não têm vistoria.
type leaseInspections struct {
	domain.InspectionRepository
	items []domain.Inspection
}

func (r *leaseInspections) List(context.Context) ([]domain.Inspection, error) { return r.items, nil }

func (r *leaseInspections) ByLease(_ context.Context, leaseID string) ([]domain.Inspection, error) {
	out := []domain.Inspection{}
	for _, inspection := range r.items {
		if inspection.LeaseID == leaseID {
			out = append(out, inspection)
		}
	}
	return out, nil
}

func (r *leaseInspections) Create(_ context.Context, inspection *domain.Inspection) error {
	r.items = append(r.items, *inspection)
	return nil
}

// ── Painel de teste ────────────────────────────────────────────────

type leasePanel struct {
	server     *testServer
	leases     *leaseLeases
	tenants    *leaseTenants
	properties *leaseProperties
}

func newLeasePanel(t *testing.T, tenants []domain.Tenant, properties []domain.Property) *leasePanel {
	t.Helper()

	changedAt := "2026-09-21 12:00:00"
	server := newTestServer(t, &domain.User{
		ID: "u1", Username: "passoz", PasswordHash: "hash:segredo", PasswordChangedAt: &changedAt,
	})

	tenantRepo := newLeaseTenants(tenants...)
	propertyRepo := newLeaseProperties(properties...)
	leaseRepo := newLeaseLeases(tenantRepo, propertyRepo)

	server.handler.Deps.Repos.Tenants = tenantRepo
	server.handler.Deps.Repos.Properties = propertyRepo
	server.handler.Deps.Repos.Leases = leaseRepo
	server.handler.Deps.Repos.Receipts = &leaseReceipts{}
	server.handler.Deps.Repos.Inspections = &leaseInspections{}

	return &leasePanel{server: server, leases: leaseRepo, tenants: tenantRepo, properties: propertyRepo}
}

// authenticate faz o login do administrador e devolve a sessão e o token CSRF
// emitido ao renderizar a aba de contratos.
func (p *leasePanel) authenticate(t *testing.T) (session string, csrf string) {
	t.Helper()

	loginRecorder := p.server.login(t, "passoz", "segredo")
	if loginRecorder.Code != http.StatusFound {
		t.Fatalf("login: status %d, esperado 302", loginRecorder.Code)
	}
	session = cookieValue(loginRecorder.Result().Cookies(), adminCookieName)
	if session == "" {
		t.Fatal("login não emitiu sessão")
	}

	page := httptest.NewRequest(http.MethodGet, "/dashboard?tab=leases", nil)
	page.AddCookie(&http.Cookie{Name: adminCookieName, Value: session})
	recorder := p.server.do(page)
	if recorder.Code != http.StatusOK {
		t.Fatalf("aba de contratos: status %d", recorder.Code)
	}
	csrf = cookieValue(recorder.Result().Cookies(), csrfCookieName)
	if csrf == "" {
		t.Fatal("aba de contratos não emitiu token de CSRF")
	}
	return session, csrf
}

// mutation envia um POST /dashboard como o formulário do painel faria.
func (p *leasePanel) mutation(t *testing.T, session, csrf string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set("csrf_token", csrf)
	request := httptest.NewRequest(http.MethodPost, "/dashboard", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.AddCookie(&http.Cookie{Name: adminCookieName, Value: session})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrf})
	return p.server.do(request)
}

// tab abre a aba de contratos autenticada, opcionalmente com filtro de situação.
func (p *leasePanel) tab(t *testing.T, session, status string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/dashboard?tab=leases"
	if status != "" {
		target += "&status=" + url.QueryEscape(status)
	}
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.AddCookie(&http.Cookie{Name: adminCookieName, Value: session})
	return p.server.do(request)
}

func leaseMutationError(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("resposta inválida (%d): %s", recorder.Code, recorder.Body.String())
	}
	return body.Error
}

func leaseRequireOK(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"ok":true`) {
		t.Fatalf("resposta sem ok: %s", recorder.Body.String())
	}
}

func leaseForm(intent string, fields map[string]string) url.Values {
	form := url.Values{"intent": {intent}}
	for key, value := range fields {
		form.Set(key, value)
	}
	return form
}

func leaseFixtures() ([]domain.Tenant, []domain.Property) {
	tenants := []domain.Tenant{
		{ID: "t1", Name: "Ana Souza", Email: "ana@teste.local"},
		{ID: "t2", Name: "Bruno Lima", Email: "bruno@teste.local"},
	}
	properties := []domain.Property{
		{ID: "p1", Name: "Casa da Praia", Address: "Rua A", City: "Niterói", State: "RJ"},
		{ID: "p2", Name: "Apto Centro", Address: "Rua B", City: "Niterói", State: "RJ"},
	}
	return tenants, properties
}

func leaseCreateForm(tenantID, propertyID, startDate, endDate, dueDay, rent, status string) url.Values {
	return leaseForm("lease.create", map[string]string{
		"tenant_id":   tenantID,
		"property_id": propertyID,
		"start_date":  startDate,
		"end_date":    endDate,
		"due_day":     dueDay,
		"rent_amount": rent,
		"status":      status,
	})
}

// ── Intent lease.create ────────────────────────────────────────────

func TestLeaseCreateIntentCreatesLease(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, csrf := panel.authenticate(t)

	recorder := panel.mutation(t, session, csrf, leaseCreateForm("t1", "p1", "2026-01-01", "2026-12-31", "10", "2500", "active"))
	leaseRequireOK(t, recorder)

	if len(panel.leases.items) != 1 {
		t.Fatalf("contratos gravados = %d, esperado 1", len(panel.leases.items))
	}
	stored, err := panel.leases.List(context.Background())
	if err != nil {
		t.Fatalf("listar contratos: %v", err)
	}
	for _, lease := range stored {
		if lease.Status != domain.LeaseStatusActive || lease.RentAmount != 2500 || lease.DueDay != 10 {
			t.Fatalf("contrato inesperado: %+v", lease)
		}
		if lease.TenantName != "Ana Souza" || lease.PropertyName != "Casa da Praia" {
			t.Fatalf("nomes do join inesperados: %q / %q", lease.TenantName, lease.PropertyName)
		}
	}
	if actions := panel.server.audit.actions(); !containsAction(actions, domain.ActionLeaseCreate) {
		t.Fatalf("trilha sem lease.create: %v", actions)
	}
}

func TestLeaseCreateIntentDefaultsStatusToActive(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, csrf := panel.authenticate(t)

	form := leaseCreateForm("t1", "p1", "2026-01-01", "2026-12-31", "10", "2500", "")
	recorder := panel.mutation(t, session, csrf, form)
	leaseRequireOK(t, recorder)

	for _, lease := range panel.leases.items {
		if lease.Status != domain.LeaseStatusActive {
			t.Fatalf("status = %q, esperado active", lease.Status)
		}
	}
}

func TestLeaseCreateIntentRejectsOverlap(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, csrf := panel.authenticate(t)

	leaseRequireOK(t, panel.mutation(t, session, csrf, leaseCreateForm("t1", "p1", "2026-01-01", "2026-12-31", "10", "2500", "active")))

	recorder := panel.mutation(t, session, csrf, leaseCreateForm("t2", "p1", "2026-06-01", "2026-06-30", "10", "1800", "active"))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d, esperado 400: %s", recorder.Code, recorder.Body.String())
	}
	if message := leaseMutationError(t, recorder); message != "Este imóvel já possui contrato vigente no período." {
		t.Fatalf("mensagem = %q", message)
	}
	if len(panel.leases.items) != 1 {
		t.Fatalf("contrato sobreposto foi gravado: %d", len(panel.leases.items))
	}
}

func TestLeaseCreateIntentRejectsSeasonOverlap(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, csrf := panel.authenticate(t)

	leaseRequireOK(t, panel.mutation(t, session, csrf, leaseCreateForm("t1", "p1", "2026-01-01", "2026-12-31", "10", "2500", "active")))

	recorder := panel.mutation(t, session, csrf, leaseCreateForm("t2", "p1", "2026-06-01", "2026-06-20", "5", "900", "season"))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d, esperado 400: %s", recorder.Code, recorder.Body.String())
	}
	if message := leaseMutationError(t, recorder); !strings.Contains(message, "Temporada só pode ser criada") {
		t.Fatalf("mensagem = %q", message)
	}
}

func TestLeaseCreateIntentRejectsInvalidStatus(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, csrf := panel.authenticate(t)

	recorder := panel.mutation(t, session, csrf, leaseCreateForm("t1", "p1", "2026-01-01", "2026-12-31", "10", "2500", "invalid-status"))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d, esperado 400: %s", recorder.Code, recorder.Body.String())
	}
	if message := leaseMutationError(t, recorder); message != "Status de contrato inválido." {
		t.Fatalf("mensagem = %q", message)
	}
}

func TestLeaseSetStatusIntentUpdatesOnlyStatus(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, csrf := panel.authenticate(t)

	panel.leases.seed(domain.Lease{
		ID: "l1", TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31",
		DueDay: 10, RentAmount: 2500, Status: domain.LeaseStatusActive,
	})

	recorder := panel.mutation(t, session, csrf, leaseForm("lease.suspend", map[string]string{"id": "l1"}))
	leaseRequireOK(t, recorder)
	if got := panel.leases.items["l1"].Status; got != domain.LeaseStatusSuspended {
		t.Fatalf("status = %q, esperado suspended", got)
	}

	recorder = panel.mutation(t, session, csrf, leaseForm("lease.reactivate", map[string]string{"id": "l1"}))
	leaseRequireOK(t, recorder)
	if got := panel.leases.items["l1"].Status; got != domain.LeaseStatusActive {
		t.Fatalf("status = %q, esperado active", got)
	}

	recorder = panel.mutation(t, session, csrf, leaseForm("lease.terminate", map[string]string{"id": "l1"}))
	leaseRequireOK(t, recorder)
	if got := panel.leases.items["l1"].Status; got != domain.LeaseStatusTerminated {
		t.Fatalf("status = %q, esperado terminated", got)
	}
	if actions := panel.server.audit.actions(); !containsAction(actions, domain.ActionLeaseSuspend) || !containsAction(actions, domain.ActionLeaseReactivate) || !containsAction(actions, domain.ActionLeaseTerminate) {
		t.Fatalf("trilha incompleta: %v", actions)
	}
}

func TestLeaseDeleteIntentRemovesLease(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, csrf := panel.authenticate(t)

	panel.leases.seed(domain.Lease{
		ID: "l1", TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31",
		DueDay: 10, RentAmount: 2500, Status: domain.LeaseStatusActive,
	})

	recorder := panel.mutation(t, session, csrf, leaseForm("lease.delete", map[string]string{"id": "l1"}))
	leaseRequireOK(t, recorder)
	if len(panel.leases.items) != 0 {
		t.Fatalf("contrato não foi excluído: %+v", panel.leases.items)
	}
	if actions := panel.server.audit.actions(); !containsAction(actions, domain.ActionLeaseDelete) {
		t.Fatalf("trilha sem lease.delete: %v", actions)
	}
}

func TestLeaseIntentRejectsUnknownIntent(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, csrf := panel.authenticate(t)

	recorder := panel.mutation(t, session, csrf, leaseForm("lease.promote", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d, esperado 400", recorder.Code)
	}
	if message := leaseMutationError(t, recorder); message != "Ação inválida." {
		t.Fatalf("mensagem = %q", message)
	}
}

// ── Renderização da aba ────────────────────────────────────────────

func TestLeaseTabRendersTableAndModals(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, _ := panel.authenticate(t)

	panel.leases.seed(domain.Lease{
		ID: "l1", TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31",
		DueDay: 10, RentAmount: 2500, Status: domain.LeaseStatusActive,
	})
	panel.leases.seed(domain.Lease{
		ID: "l2", TenantID: "t2", PropertyID: "p2", StartDate: "2026-03-01", EndDate: "2026-03-15",
		DueDay: 5, RentAmount: 900, Status: domain.LeaseStatusSeason,
	})
	panel.leases.seed(domain.Lease{
		ID: "l3", TenantID: "t2", PropertyID: "p2", StartDate: "2026-04-01", EndDate: "2026-10-31",
		DueDay: 5, RentAmount: 1500, Status: domain.LeaseStatusSuspended,
	})

	recorder := panel.tab(t, session, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, esperado 200", recorder.Code)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		"Gerenciar Contratos",
		"Ana Souza",
		"Casa da Praia",
		"R$ 2.500,00",
		"Dia 10",
		"Encerrar temporada",
		"lease-create-modal",
		"lease-edit-modal",
		`name="intent" value="lease.create"`,
		`name="intent" value="lease.reactivate"`,
		`name="intent" value="lease.terminate"`,
		`name="intent" value="lease.delete"`,
		"/contract-pdf?id=l1",
		`data-whatsapp-send="lease"`,
		`data-whatsapp-id="l1"`,
		// O filtro de situação é um seletor que navega para a URL (como no app
		// original): o que se afirma é que as quatro situações são oferecidas.
		`value="/dashboard?tab=leases&amp;status=active"`,
		`value="/dashboard?tab=leases&amp;status=suspended"`,
		`value="/dashboard?tab=leases&amp;status=season"`,
		`value="/dashboard?tab=leases&amp;status=terminated"`,
		"Temporada",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("aba de contratos sem %q", expected)
		}
	}
}

func TestLeaseTabEditModalCarriesLeaseFields(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, _ := panel.authenticate(t)

	panel.leases.seed(domain.Lease{
		ID: "l1", TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31",
		DueDay: 10, RentAmount: 2500, Status: domain.LeaseStatusSuspended,
	})

	body := panel.tab(t, session, "").Body.String()
	payload := modalFillFor(t, body, `data-modal-fill='([^']*l1[^']*)'`)
	for key, want := range map[string]string{
		"id":          "l1",
		"tenant_id":   "t1",
		"property_id": "p1",
		"start_date":  "2026-01-01",
		"end_date":    "2026-12-31",
		"due_day":     "10",
		"rent_amount": "2500.00",
		"status":      "suspended",
	} {
		if payload[key] != want {
			t.Fatalf("data-modal-fill[%q] = %q, esperado %q", key, payload[key], want)
		}
	}
}

func TestLeaseTabFiltersByStatus(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, _ := panel.authenticate(t)

	panel.leases.seed(domain.Lease{
		ID: "lease-active", TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31",
		DueDay: 10, RentAmount: 2500, Status: domain.LeaseStatusActive,
	})
	panel.leases.seed(domain.Lease{
		ID: "lease-terminated", TenantID: "t2", PropertyID: "p2", StartDate: "2025-01-01", EndDate: "2025-12-31",
		DueDay: 5, RentAmount: 1800, Status: domain.LeaseStatusTerminated,
	})

	terminated := panel.tab(t, session, "terminated").Body.String()
	if !strings.Contains(terminated, "lease-terminated") {
		t.Fatal("filtro de encerrados sem o contrato encerrado")
	}
	if strings.Contains(terminated, "lease-active") {
		t.Fatal("filtro de encerrados incluiu o contrato ativo")
	}

	active := panel.tab(t, session, "active").Body.String()
	if !strings.Contains(active, "lease-active") {
		t.Fatal("filtro de ativos sem o contrato ativo")
	}
	if strings.Contains(active, "lease-terminated") {
		t.Fatal("filtro de ativos incluiu o contrato encerrado")
	}

	all := panel.tab(t, session, "").Body.String()
	if !strings.Contains(all, "lease-active") || !strings.Contains(all, "lease-terminated") {
		t.Fatal("sem filtro deveria mostrar todos os contratos")
	}
}

func TestLeaseTabRendersEmptyState(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, _ := panel.authenticate(t)

	body := panel.tab(t, session, "").Body.String()
	if !strings.Contains(body, "Cadastre um inquilino e um imóvel para criar contratos.") {
		t.Fatal("aba sem contratos não mostrou o estado vazio")
	}
}

// ── Auxiliares ─────────────────────────────────────────────────────

func containsAction(actions []string, want string) bool {
	for _, action := range actions {
		if action == want {
			return true
		}
	}
	return false
}

// modalFillFor lê o JSON entregue ao JS do painel pelo botão de edição que casa
// com o padrão informado.
func modalFillFor(t *testing.T, body, pattern string) map[string]string {
	t.Helper()
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("nenhum data-modal-fill casou com %q", pattern)
	}
	payload := map[string]string{}
	if err := json.Unmarshal([]byte(html.UnescapeString(match[1])), &payload); err != nil {
		t.Fatalf("data-modal-fill inválido: %v (%s)", err, match[1])
	}
	return payload
}

// ── Caução e vistoria opcionais no formulário (Work 0007, task 7.5) ──

func TestLeaseCreateFormExposesDepositFields(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, _ := panel.authenticate(t)

	recorder := panel.tab(t, session, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("aba de contratos: status %d, esperado 200", recorder.Code)
	}
	body := recorder.Body.String()
	for _, field := range []string{`name="deposit_amount"`, `name="deposit_notes"`, `name="inspection_checkin"`, `name="inspection_condition"`, `name="inspection_performed_at"`} {
		if !strings.Contains(body, field) {
			t.Fatalf("formulário de contrato sem o campo %s", field)
		}
	}
}

func TestLeaseCreateIntentPersistsDeposit(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, csrf := panel.authenticate(t)

	form := leaseCreateForm("t1", "p1", "2026-01-01", "2026-12-31", "10", "2500", "active")
	form.Set("deposit_amount", "900")
	form.Set("deposit_notes", "caução do fixo")
	recorder := panel.mutation(t, session, csrf, form)
	leaseRequireOK(t, recorder)

	stored, err := panel.leases.List(context.Background())
	if err != nil {
		t.Fatalf("listar contratos: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("contratos = %d, esperado 1", len(stored))
	}
	if stored[0].DepositAmount == nil || *stored[0].DepositAmount != 900 {
		t.Fatalf("caução = %v, esperada 900", stored[0].DepositAmount)
	}
	if stored[0].DepositStatus != domain.DepositStatusHeld {
		t.Fatalf("situação = %q, esperada held", stored[0].DepositStatus)
	}
	if actions := panel.server.audit.actions(); !containsAction(actions, domain.ActionLeaseCreate) || !containsAction(actions, domain.ActionLeaseUpdate) {
		t.Fatalf("trilha sem criação e caução: %v", actions)
	}
}

func TestLeaseCreateIntentWithCheckInRegistersInspection(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, csrf := panel.authenticate(t)

	form := leaseCreateForm("t1", "p1", "2026-01-01", "2026-12-31", "10", "2500", "active")
	form.Set("inspection_checkin", "on")
	form.Set("inspection_condition", "boa")
	form.Set("inspection_performed_at", "2026-01-01")
	recorder := panel.mutation(t, session, csrf, form)
	leaseRequireOK(t, recorder)

	stored, err := panel.leases.List(context.Background())
	if err != nil {
		t.Fatalf("listar contratos: %v", err)
	}
	found, err := panel.server.handler.Deps.Repos.Inspections.ByLease(context.Background(), stored[0].ID)
	if err != nil {
		t.Fatalf("listar vistorias: %v", err)
	}
	if len(found) != 1 || found[0].Kind != domain.InspectionKindCheckIn {
		t.Fatalf("vistorias = %+v, esperada 1 de entrada", found)
	}
}

func TestLeaseUpdateIntentBlankDepositPreserves(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, csrf := panel.authenticate(t)

	panel.leases.seed(domain.Lease{
		ID: "l1", TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31",
		DueDay: 10, RentAmount: 2500, Status: domain.LeaseStatusActive,
		DepositAmount: domain.FloatPtr(500), DepositStatus: domain.DepositStatusHeld,
	})

	form := leaseForm("lease.update", map[string]string{
		"id": "l1", "tenant_id": "t1", "property_id": "p1",
		"start_date": "2026-01-01", "end_date": "2026-12-31",
		"due_day": "10", "rent_amount": "2500", "status": "active",
	})
	recorder := panel.mutation(t, session, csrf, form)
	leaseRequireOK(t, recorder)

	if got := panel.leases.items["l1"].DepositAmount; got == nil || *got != 500 {
		t.Fatalf("caução apagada na edição: %v", got)
	}
	if got := panel.leases.items["l1"].DepositStatus; got != domain.DepositStatusHeld {
		t.Fatalf("situação = %q, esperada held", got)
	}
}

func TestLeaseEditModalCarriesDeposit(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, _ := panel.authenticate(t)

	panel.leases.seed(domain.Lease{
		ID: "l1", TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31",
		DueDay: 10, RentAmount: 2500, Status: domain.LeaseStatusActive,
		DepositAmount: domain.FloatPtr(500), DepositStatus: domain.DepositStatusHeld,
	})

	recorder := panel.tab(t, session, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("aba de contratos: status %d, esperado 200", recorder.Code)
	}
	fill := modalFillFor(t, recorder.Body.String(), `data-modal-fill='([^']*l1[^']*)'`)
	if fill["deposit_amount"] != "500.00" {
		t.Fatalf("payload sem a caução existente: %v", fill)
	}
	if _, ok := fill["deposit_notes"]; !ok {
		t.Fatalf("payload sem as observações da caução: %v", fill)
	}
}
