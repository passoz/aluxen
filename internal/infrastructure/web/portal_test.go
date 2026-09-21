package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
)

// ── Dublês da Área do Cliente ──────────────────────────────────────

// portalTenants é o repositório de locatários em memória: as buscas por CPF
// ignoram a pontuação, como o `replace(...)` da query do port.
type portalTenants struct{ byID map[string]*domain.Tenant }

func newPortalTenants(tenants ...*domain.Tenant) *portalTenants {
	repo := &portalTenants{byID: map[string]*domain.Tenant{}}
	for _, tenant := range tenants {
		repo.byID[tenant.ID] = tenant
	}
	return repo
}

func (r *portalTenants) List(context.Context) ([]domain.Tenant, error) { return nil, nil }

func (r *portalTenants) ByID(_ context.Context, id string) (*domain.Tenant, error) {
	tenant, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return tenant, nil
}

func (r *portalTenants) ByCPF(_ context.Context, digits string) (*domain.Tenant, error) {
	for _, tenant := range r.byID {
		cpf := portalDigits(domain.Str(tenant.CPF))
		if cpf != "" && cpf == digits {
			return tenant, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *portalTenants) Create(_ context.Context, tenant *domain.Tenant) error {
	r.byID[tenant.ID] = tenant
	return nil
}

func (r *portalTenants) Update(context.Context, *domain.Tenant) error { return nil }

func (r *portalTenants) UpdatePassword(_ context.Context, id, passwordHash, changedAt, updatedAt string) error {
	tenant, ok := r.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	tenant.PasswordHash = &passwordHash
	tenant.PasswordChangedAt = &changedAt
	return nil
}

func (r *portalTenants) Delete(context.Context, string) error { return nil }

func (r *portalTenants) Count(context.Context) (int, error) { return len(r.byID), nil }

type portalProperties struct{ byID map[string]*domain.Property }

func newPortalProperties(properties ...*domain.Property) *portalProperties {
	repo := &portalProperties{byID: map[string]*domain.Property{}}
	for _, property := range properties {
		repo.byID[property.ID] = property
	}
	return repo
}

func (r *portalProperties) List(context.Context) ([]domain.Property, error) { return nil, nil }

func (r *portalProperties) ByID(_ context.Context, id string) (*domain.Property, error) {
	property, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return property, nil
}

func (r *portalProperties) Create(context.Context, *domain.Property) error { return nil }
func (r *portalProperties) Update(context.Context, *domain.Property) error { return nil }
func (r *portalProperties) Delete(context.Context, string) error           { return nil }
func (r *portalProperties) Count(context.Context) (int, error)             { return len(r.byID), nil }

type portalLeases struct{ byTenant map[string][]domain.Lease }

func newPortalLeases(leases ...domain.Lease) *portalLeases {
	repo := &portalLeases{byTenant: map[string][]domain.Lease{}}
	for _, lease := range leases {
		repo.byTenant[lease.TenantID] = append(repo.byTenant[lease.TenantID], lease)
	}
	return repo
}

func (r *portalLeases) List(context.Context) ([]domain.Lease, error) { return nil, nil }

func (r *portalLeases) ByID(_ context.Context, id string) (*domain.Lease, error) {
	for _, leases := range r.byTenant {
		for index := range leases {
			if leases[index].ID == id {
				return &leases[index], nil
			}
		}
	}
	return nil, domain.ErrNotFound
}

func (r *portalLeases) ByTenant(_ context.Context, tenantID string) ([]domain.Lease, error) {
	return r.byTenant[tenantID], nil
}

func (r *portalLeases) ByProperty(context.Context, string) ([]domain.Lease, error) { return nil, nil }

func (r *portalLeases) Overlapping(context.Context, string, string, string, string) ([]domain.Lease, error) {
	return nil, nil
}

func (r *portalLeases) ActiveInRange(context.Context, string, string) ([]domain.Lease, error) {
	return nil, nil
}

func (r *portalLeases) Create(context.Context, *domain.Lease) error { return nil }
func (r *portalLeases) Update(context.Context, *domain.Lease) error { return nil }

func (r *portalLeases) UpdateStatus(context.Context, string, domain.LeaseStatus, string) error {
	return nil
}

func (r *portalLeases) Delete(context.Context, string) error { return nil }
func (r *portalLeases) Count(context.Context) (int, error)   { return 0, nil }

func (r *portalLeases) CountByStatus(context.Context, domain.LeaseStatus) (int, error) { return 0, nil }

type portalReceipts struct{ byTenant map[string][]domain.Receipt }

func newPortalReceipts(receipts ...domain.Receipt) *portalReceipts {
	repo := &portalReceipts{byTenant: map[string][]domain.Receipt{}}
	for _, receipt := range receipts {
		tenantID := domain.Str(receipt.TenantID)
		repo.byTenant[tenantID] = append(repo.byTenant[tenantID], receipt)
	}
	return repo
}

func (r *portalReceipts) List(context.Context) ([]domain.Receipt, error) { return nil, nil }

func (r *portalReceipts) ByID(_ context.Context, id string) (*domain.Receipt, error) {
	for _, receipts := range r.byTenant {
		for index := range receipts {
			if receipts[index].ID == id {
				return &receipts[index], nil
			}
		}
	}
	return nil, domain.ErrNotFound
}

func (r *portalReceipts) ByTenant(_ context.Context, tenantID string) ([]domain.Receipt, error) {
	return r.byTenant[tenantID], nil
}

func (r *portalReceipts) ByLease(context.Context, string) ([]domain.Receipt, error) { return nil, nil }

func (r *portalReceipts) Create(context.Context, *domain.Receipt) error { return nil }

func (r *portalReceipts) CreateIfAbsent(context.Context, *domain.Receipt) (int64, error) {
	return 0, nil
}

func (r *portalReceipts) Update(context.Context, *domain.Receipt) error { return nil }

func (r *portalReceipts) UpdatePayment(context.Context, string, string, float64, string) error {
	return nil
}

func (r *portalReceipts) Delete(context.Context, string) error { return nil }
func (r *portalReceipts) Count(context.Context) (int, error)   { return 0, nil }

// portalSigner emite chaves determinísticas para os links de validação, com os
// mesmos caminhos por tipo do gerador de produção (`recibo`/`contrato`).
type portalSigner struct{}

func (portalSigner) Generate(kind, id string) string { return kind + "-token-" + id }

func (portalSigner) FormatCode(token string) string { return "AUTH-" + strings.ToUpper(token) }

func (portalSigner) ValidationURL(base, kind, id, token string) string {
	return strings.TrimSuffix(base, "/") + "/validar/" + portalValidationPath(kind) + "/" + id + "?token=" + token
}

func portalValidationPath(kind string) string {
	if kind == "lease" {
		return "contrato"
	}
	return "recibo"
}

func portalDigits(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ── Cenário ────────────────────────────────────────────────────────

const (
	portalTenantID   = "tenant-a"
	portalOtherID    = "tenant-b"
	portalCPF        = "123.456.789-00"
	portalOtherCPF   = "987.654.321-00"
	portalPassword   = "senha-da-ana"
	portalOtherPass  = "senha-do-bruno"
	portalLeaseID    = "lease-a"
	portalOtherLease = "lease-b"
	portalPaidID     = "receipt-a-paid"
	portalOtherIDRec = "receipt-b-paid"
)

// newPortalServer monta o servidor de teste com os cadastros do portal: um
// titular com senha própria e outro no primeiro acesso (senha = CPF), cada um
// com o seu contrato, imóvel e recibo quitado.
func newPortalServer(t *testing.T) *testServer {
	t.Helper()

	ownHash := "hash:" + portalPassword
	changedAt := "2026-08-02 10:00:00"
	otherHash := "hash:" + portalOtherPass

	tenants := newPortalTenants(
		&domain.Tenant{
			ID: portalTenantID, Name: "Ana Alves", CPF: domain.StrPtr(portalCPF), Email: "ana@exemplo.com",
			PasswordHash: &ownHash, PasswordChangedAt: &changedAt,
		},
		&domain.Tenant{
			ID: portalOtherID, Name: "Bruno Barros", CPF: domain.StrPtr(portalOtherCPF), Email: "bruno@exemplo.com",
			PasswordHash: &otherHash, PasswordChangedAt: &changedAt,
		},
	)

	properties := newPortalProperties(
		&domain.Property{ID: "prop-a", Name: "Casa A", Address: "Rua Um, 100", City: "Curitiba", State: "PR"},
		&domain.Property{ID: "prop-b", Name: "Casa B", Address: "Rua Dois, 200", City: "Curitiba", State: "PR"},
	)

	leases := newPortalLeases(
		domain.Lease{
			ID: portalLeaseID, TenantID: portalTenantID, PropertyID: "prop-a", StartDate: "2026-01-01", EndDate: "2026-12-31",
			DueDay: 10, RentAmount: 1500, Status: domain.LeaseStatusActive, PropertyName: "Casa A",
		},
		domain.Lease{
			ID: portalOtherLease, TenantID: portalOtherID, PropertyID: "prop-b", StartDate: "2026-03-01", EndDate: "2027-02-28",
			DueDay: 5, RentAmount: 2400, Status: domain.LeaseStatusTerminated, PropertyName: "Casa B",
		},
	)

	paidAt := "2026-09-08T10:15"
	otherPaidAt := "2026-09-05T08:00"
	leaseA, leaseB := portalLeaseID, portalOtherLease
	tenantA, tenantB := portalTenantID, portalOtherID
	propB := "prop-b"

	receipts := newPortalReceipts(
		domain.Receipt{
			ID: portalPaidID, LeaseID: &leaseA, TenantID: &tenantA, Category: domain.ReceiptCategoryRent,
			DueDate: "2026-09-10", PaidAt: &paidAt, Amount: 1500, PropertyName: "Casa A",
		},
		domain.Receipt{
			ID: "receipt-a-open", LeaseID: &leaseA, TenantID: &tenantA, Category: domain.ReceiptCategoryRent,
			DueDate: "2026-10-10", Amount: 1500, PropertyName: "Casa A",
		},
		domain.Receipt{
			ID: portalOtherIDRec, LeaseID: &leaseB, TenantID: &tenantB, PropertyID: &propB,
			Category: domain.ReceiptCategoryRent, DueDate: "2026-09-05", PaidAt: &otherPaidAt, Amount: 2400,
			PropertyName: "Casa B",
		},
	)

	server := newTestServer(t)
	server.handler.Deps.Repos.Tenants = tenants
	server.handler.Deps.Repos.Properties = properties
	server.handler.Deps.Repos.Leases = leases
	server.handler.Deps.Repos.Receipts = receipts
	server.handler.Deps.Signer = portalSigner{}
	server.handler.Deps.Opts.PublicBaseURL = "https://aluxen.test"
	return server
}

// portalSession devolve o cookie de sessão da Área do Cliente emitido em uma
// resposta de login.
func portalSession(t *testing.T, recorder *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()

	value := cookieValue(recorder.Result().Cookies(), clientCookieName)
	if value == "" {
		t.Fatal("login do locatário não emitiu o cookie de sessão")
	}
	return &http.Cookie{Name: clientCookieName, Value: value}
}

// portalPost envia um formulário do portal com o token CSRF do cookie emitido
// pelo GET da página de acesso.
func (s *testServer) portalPost(t *testing.T, target string, form url.Values, session []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	page := httptest.NewRecorder()
	s.handler.Router().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/portal/login", nil))
	token := cookieValue(page.Result().Cookies(), csrfCookieName)
	if token == "" {
		t.Fatal("cookie de CSRF não foi emitido no GET /portal/login")
	}
	form.Set(csrfFieldName, token)

	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range page.Result().Cookies() {
		request.AddCookie(cookie)
	}
	for _, cookie := range session {
		request.AddCookie(cookie)
	}
	return s.do(request)
}

// portalGet abre uma página da Área do Cliente com a sessão informada.
func (s *testServer) portalGet(t *testing.T, target string, session []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, target, nil)
	for _, cookie := range session {
		request.AddCookie(cookie)
	}
	return s.do(request)
}

// portalLogin entra na Área do Cliente e devolve a resposta do POST.
func (s *testServer) portalLogin(t *testing.T, cpf, password string) *httptest.ResponseRecorder {
	t.Helper()

	return s.portalPost(t, "/portal/login", url.Values{"cpf": {cpf}, "password": {password}}, nil)
}

// ── Testes ─────────────────────────────────────────────────────────

func TestPortalLoginPageRendersFormAndAdminEntry(t *testing.T) {
	server := newPortalServer(t)

	recorder := server.portalGet(t, "/portal/login", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}
	body := recorder.Body.String()
	// O app original mostra a porta de entrada do painel nesta tela
	// (src/client/portal.tsx): a paridade exige o link, e o isolamento é do
	// acesso — não da tela.
	for _, expected := range []string{`name="csrf_token"`, `action="/portal/login"`, `name="cpf"`, `name="password"`, "Área do Cliente", `href="/login"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("página de acesso sem %s", expected)
		}
	}
}

func TestPortalSessionDoesNotOpenTheAdminPanel(t *testing.T) {
	server := newPortalServer(t)

	// A sessão do locatário é o cookie do portal; ela não autoriza o painel.
	portalCookie := portalSession(t, server.portalLogin(t, portalCPF, portalPassword))
	recorder := server.portalGet(t, "/dashboard?tab=dashboard", []*http.Cookie{portalCookie})
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/login" {
		t.Fatalf("sessão do locatário não pode abrir o painel: status %d, location %q",
			recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestPortalLoginRedirectsWhenAlreadySignedIn(t *testing.T) {
	server := newPortalServer(t)

	session := portalSession(t, server.portalLogin(t, portalCPF, portalPassword))
	recorder := server.portalGet(t, "/portal/login", []*http.Cookie{session})
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/portal" {
		t.Fatalf("status %d, location %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestPortalLoginForcesPasswordChangeOnDefaultPassword(t *testing.T) {
	server := newPortalServer(t)

	fresh := &domain.Tenant{ID: "tenant-novo", Name: "Novo Locatário", CPF: domain.StrPtr("321.654.987-00")}
	server.handler.Deps.Repos.Tenants.(*portalTenants).Create(context.Background(), fresh)

	recorder := server.portalLogin(t, "321.654.987-00", "32165498700")
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, esperado 302 (corpo: %s)", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "/portal/password" {
		t.Fatalf("redirecionamento = %q, esperado /portal/password", location)
	}
	portalSession(t, recorder)

	actions := server.audit.actions()
	last := server.audit.entries[len(server.audit.entries)-1]
	if len(actions) == 0 || actions[len(actions)-1] != domain.ActionClientLogin {
		t.Fatalf("trilha de auditoria sem %s: %v", domain.ActionClientLogin, actions)
	}
	if last.Origin != domain.AuditOriginClient {
		t.Fatalf("origem = %q, esperada %q", last.Origin, domain.AuditOriginClient)
	}
	if domain.Str(last.ActorName) != "Novo Locatário" || domain.Str(last.ActorID) != fresh.ID {
		t.Fatalf("ator = %q / %q", domain.Str(last.ActorID), domain.Str(last.ActorName))
	}
}

func TestPortalLoginWithOwnPasswordOpensPortal(t *testing.T) {
	server := newPortalServer(t)

	recorder := server.portalLogin(t, "123.456.789-00", portalPassword)
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/portal" {
		t.Fatalf("status %d, location %q", recorder.Code, recorder.Header().Get("Location"))
	}
	portalSession(t, recorder)
}

func TestPortalLoginRejectsInvalidCredentials(t *testing.T) {
	server := newPortalServer(t)

	recorder := server.portalLogin(t, portalCPF, "senha-errada")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "CPF ou senha inválidos.") {
		t.Fatalf("corpo inesperado: %s", recorder.Body.String())
	}
	if cookieValue(recorder.Result().Cookies(), clientCookieName) != "" {
		t.Fatal("login inválido não pode abrir sessão do locatário")
	}
	last := server.audit.entries[len(server.audit.entries)-1]
	if last.Action != domain.ActionClientLoginFailed || last.Origin != domain.AuditOriginClient {
		t.Fatalf("evento = %q (%q)", last.Action, last.Origin)
	}
	if domain.Str(last.EntityLabel) != portalCPF {
		t.Fatalf("rótulo = %q, esperado o CPF informado", domain.Str(last.EntityLabel))
	}
}

func TestPortalLoginRequiresBothFields(t *testing.T) {
	server := newPortalServer(t)

	recorder := server.portalPost(t, "/portal/login", url.Values{"cpf": {""}, "password": {""}}, nil)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Informe CPF e senha.") {
		t.Fatalf("corpo inesperado: %s", recorder.Body.String())
	}
}

func TestPortalLoginSharesTheRateLimitGate(t *testing.T) {
	server := newPortalServer(t)

	for attempt := range 5 {
		if recorder := server.portalLogin(t, portalCPF, "senha-errada"); recorder.Code != http.StatusUnauthorized {
			t.Fatalf("tentativa %d: status = %d, esperado 401", attempt+1, recorder.Code)
		}
	}

	recorder := server.portalLogin(t, portalCPF, "senha-errada")
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("sexta tentativa: status = %d, esperado 429", recorder.Code)
	}
	if recorder.Header().Get("Retry-After") == "" {
		t.Fatal("429 sem header Retry-After")
	}
	if !strings.Contains(recorder.Body.String(), "Muitas tentativas") {
		t.Fatalf("corpo inesperado: %s", recorder.Body.String())
	}
	last := server.audit.entries[len(server.audit.entries)-1]
	if last.Action != domain.ActionClientLoginFailed || last.Origin != domain.AuditOriginClient {
		t.Fatalf("evento do bloqueio = %q (%q)", last.Action, last.Origin)
	}
}

func TestPortalHomeRequiresSession(t *testing.T) {
	server := newPortalServer(t)

	recorder := server.portalGet(t, "/portal", nil)
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/portal/login" {
		t.Fatalf("status %d, location %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestPortalHomeRedirectsToPasswordOnFirstAccess(t *testing.T) {
	server := newPortalServer(t)

	fresh := &domain.Tenant{ID: "tenant-novo", Name: "Novo Locatário", CPF: domain.StrPtr("321.654.987-00")}
	server.handler.Deps.Repos.Tenants.(*portalTenants).Create(context.Background(), fresh)
	session := portalSession(t, server.portalLogin(t, "32165498700", "32165498700"))

	recorder := server.portalGet(t, "/portal", []*http.Cookie{session})
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/portal/password" {
		t.Fatalf("status %d, location %q", recorder.Code, recorder.Header().Get("Location"))
	}

	page := server.portalGet(t, "/portal/password", []*http.Cookie{session})
	if page.Code != http.StatusOK {
		t.Fatalf("tela de senha: status %d", page.Code)
	}
	for _, expected := range []string{
		"Você entrou com a senha padrão (seu CPF). Defina uma senha pessoal para continuar.",
		`name="current_password"`, `name="new_password"`, `name="confirm_password"`,
	} {
		if !strings.Contains(page.Body.String(), expected) {
			t.Fatalf("tela do primeiro acesso sem %s", expected)
		}
	}
}

func TestPortalHomeShowsOnlyOwnDocuments(t *testing.T) {
	server := newPortalServer(t)

	session := portalSession(t, server.portalLogin(t, "123.456.789-00", portalPassword))
	recorder := server.portalGet(t, "/portal", []*http.Cookie{session})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}

	body := recorder.Body.String()
	for _, expected := range []string{
		"Ana Alves", "CPF 123.456.789-00", "Casa A", "R$ 1.500,00",
		"/pdf?id=" + portalPaidID, "/contract-pdf?id=" + portalLeaseID,
		"/validar/recibo/" + portalPaidID + "?token=receipt-token-" + portalPaidID,
		"/validar/contrato/" + portalLeaseID + "?token=lease-token-" + portalLeaseID,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("página do titular sem %s", expected)
		}
	}
	for _, forbidden := range []string{"Bruno Barros", "Casa B", portalOtherLease, portalOtherIDRec, "R$ 2.400,00"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("página do titular A vazou %q do titular B", forbidden)
		}
	}
	// O recibo em aberto do próprio titular não aparece: o portal só mostra o
	// que foi quitado.
	if strings.Contains(body, "receipt-a-open") {
		t.Fatal("recibo em aberto não pode aparecer na Área do Cliente")
	}
}

func TestPortalHomeShowsSuccessMessageOnOkSenha(t *testing.T) {
	server := newPortalServer(t)

	session := portalSession(t, server.portalLogin(t, portalCPF, portalPassword))
	recorder := server.portalGet(t, "/portal?ok=senha", []*http.Cookie{session})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Senha alterada com sucesso.") {
		t.Fatalf("página sem a confirmação de senha alterada: %s", recorder.Body.String())
	}
}

func TestPortalLogoutClearsSession(t *testing.T) {
	server := newPortalServer(t)

	session := portalSession(t, server.portalLogin(t, portalCPF, portalPassword))
	recorder := server.portalPost(t, "/portal/logout", url.Values{}, []*http.Cookie{session})
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/portal/login" {
		t.Fatalf("status %d, location %q", recorder.Code, recorder.Header().Get("Location"))
	}
	cleared := false
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == clientCookieName && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("logout não limpou o cookie da Área do Cliente")
	}

	last := server.audit.entries[len(server.audit.entries)-1]
	if last.Action != domain.ActionClientLogout || last.Origin != domain.AuditOriginClient {
		t.Fatalf("evento = %q (%q)", last.Action, last.Origin)
	}
	if domain.Str(last.ActorName) != "Ana Alves" {
		t.Fatalf("ator = %q", domain.Str(last.ActorName))
	}
}

func TestPortalPasswordRequiresSession(t *testing.T) {
	server := newPortalServer(t)

	recorder := server.portalPost(t, "/portal/password", url.Values{
		"current_password": {portalPassword}, "new_password": {"outra-senha"}, "confirm_password": {"outra-senha"},
	}, nil)
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/portal/login" {
		t.Fatalf("status %d, location %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestPortalPasswordChangeRedirectsToOkSenha(t *testing.T) {
	server := newPortalServer(t)

	session := portalSession(t, server.portalLogin(t, portalCPF, portalPassword))
	recorder := server.portalPost(t, "/portal/password", url.Values{
		"current_password": {portalPassword}, "new_password": {"senha-nova"}, "confirm_password": {"senha-nova"},
	}, []*http.Cookie{session})
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, esperado 302 (corpo: %s)", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "/portal?ok=senha" {
		t.Fatalf("redirecionamento = %q, esperado /portal?ok=senha", location)
	}

	// A tela seguinte exibe a confirmação e o login passa a aceitar a senha nova.
	page := server.portalGet(t, "/portal?ok=senha", []*http.Cookie{session})
	if !strings.Contains(page.Body.String(), "Senha alterada com sucesso.") {
		t.Fatalf("página sem a confirmação de senha alterada: %s", page.Body.String())
	}

	last := server.audit.entries[len(server.audit.entries)-1]
	if last.Action != domain.ActionClientPassword || last.Origin != domain.AuditOriginClient {
		t.Fatalf("evento = %q (%q)", last.Action, last.Origin)
	}
	if domain.Str(last.ActorName) != "Ana Alves" {
		t.Fatalf("ator = %q", domain.Str(last.ActorName))
	}

	if login := server.portalLogin(t, portalCPF, "senha-nova"); login.Code != http.StatusFound {
		t.Fatalf("login com a senha nova: status %d", login.Code)
	}
}

func TestPortalPasswordRejectsMismatchAndOwnCPF(t *testing.T) {
	server := newPortalServer(t)

	cases := []struct {
		name                                    string
		currentPassword, newPassword, confirmed string
		message                                 string
	}{
		{"confirmação divergente", portalPassword, "senha-nova", "outra-senha", "A confirmação não corresponde à nova senha."},
		{"nova senha igual ao CPF", portalPassword, portalCPF, portalCPF, "A nova senha não pode ser o seu CPF."},
		{"senha atual incorreta", "errada", "senha-nova", "senha-nova", "Senha atual incorreta."},
	}
	for _, testCase := range cases {
		session := portalSession(t, server.portalLogin(t, portalCPF, portalPassword))
		recorder := server.portalPost(t, "/portal/password", url.Values{
			"current_password": {testCase.currentPassword},
			"new_password":     {testCase.newPassword},
			"confirm_password": {testCase.confirmed},
		}, []*http.Cookie{session})
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, esperado 400", testCase.name, recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), testCase.message) {
			t.Fatalf("%s: página sem %q", testCase.name, testCase.message)
		}
	}
}
