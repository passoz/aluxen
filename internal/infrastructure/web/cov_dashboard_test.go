package web

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/domain"
)

// covSeedPanel carrega o painel com um conjunto pequeno e coerente de dados.
func covSeedPanel(t *testing.T) *covPanel {
	t.Helper()
	panel := newCovPanel(t)
	panel.tenants.items = []domain.Tenant{{ID: "tenant-1", Name: "Ana", Email: "ana@example.com"}}
	panel.properties.items = []domain.Property{{ID: "property-1", Name: "Casa da praia", City: "Ubatuba"}}
	panel.leases.items = []domain.Lease{{
		ID:            "lease-1",
		TenantID:      "tenant-1",
		PropertyID:    "property-1",
		TenantName:    "Ana",
		PropertyName:  "Casa da praia",
		StartDate:     "2026-01-10",
		EndDate:       "2026-12-10",
		DueDay:        10,
		RentAmount:    2500,
		Status:        domain.LeaseStatusActive,
		DepositAmount: covFloat(2500),
		DepositStatus: domain.DepositStatusHeld,
	}}
	panel.receipts.items = []domain.Receipt{{
		ID:           "receipt-1",
		LeaseID:      covString("lease-1"),
		TenantID:     covString("tenant-1"),
		PropertyID:   covString("property-1"),
		Category:     domain.ReceiptCategoryRent,
		DueDate:      "2026-09-10",
		Amount:       2500,
		TenantName:   "Ana",
		PropertyName: "Casa da praia",
	}}
	return panel
}

func TestCovDashboardJSONModeDescribesTheSnapshot(t *testing.T) {
	panel := covSeedPanel(t)

	recorder := panel.as(http.MethodGet, "/dashboard?json=true", nil, panel.admin.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	payload := covDecodeJSON(t, recorder)
	if payload["ok"] != true {
		t.Fatalf("payload = %v", payload)
	}
	user, _ := payload["user"].(map[string]any)
	if user["username"] != panel.admin.Username || user["id"] != panel.admin.ID {
		t.Fatalf("user = %v", user)
	}

	tenants, _ := payload["tenants"].([]any)
	if len(tenants) != 1 {
		t.Fatalf("tenants = %v", payload["tenants"])
	}
	tenant, _ := tenants[0].(map[string]any)
	if tenant["id"] != "tenant-1" || tenant["name"] != "Ana" || tenant["email"] != "ana@example.com" {
		t.Fatalf("tenant = %v", tenant)
	}

	properties, _ := payload["properties"].([]any)
	property, _ := properties[0].(map[string]any)
	if property["id"] != "property-1" || property["city"] != "Ubatuba" {
		t.Fatalf("property = %v", property)
	}

	leases, _ := payload["leases"].([]any)
	lease, _ := leases[0].(map[string]any)
	if lease["id"] != "lease-1" || lease["tenant_name"] != "Ana" || lease["rent_amount"] != float64(2500) {
		t.Fatalf("lease = %v", lease)
	}
	if lease["status"] != string(domain.LeaseStatusActive) || lease["deposit_status"] != string(domain.DepositStatusHeld) {
		t.Fatalf("lease = %v, esperado estado e caução", lease)
	}

	receipts, _ := payload["receipts"].([]any)
	receipt, _ := receipts[0].(map[string]any)
	if receipt["id"] != "receipt-1" || receipt["amount"] != float64(2500) {
		t.Fatalf("receipt = %v", receipt)
	}
	if receipt["status"] == "" || receipt["status_label"] == "" {
		t.Fatalf("recibo sem estado calculado: %v", receipt)
	}

	if _, ok := payload["metrics"].(map[string]any); !ok {
		t.Fatalf("metrics ausente: %v", payload["metrics"])
	}
}

func TestCovDashboardJSONOmitsDepositForLeasesWithoutIt(t *testing.T) {
	panel := newCovPanel(t)
	panel.leases.items = []domain.Lease{{ID: "lease-1", TenantID: "tenant-1", PropertyID: "property-1", RentAmount: 1000, Status: domain.LeaseStatusActive}}

	payload := covDecodeJSON(t, panel.as(http.MethodGet, "/dashboard?json=true", nil, panel.admin.ID))
	leases, _ := payload["leases"].([]any)
	if len(leases) != 1 {
		t.Fatalf("leases = %v", payload["leases"])
	}
	lease, _ := leases[0].(map[string]any)
	if _, present := lease["deposit_status"]; present {
		t.Fatalf("contrato sem caução respondeu deposit_status: %v", lease)
	}
}

func TestCovDashboardJSONAcceptsTheAcceptHeader(t *testing.T) {
	panel := covSeedPanel(t)

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept", "application/json")
	request.AddCookie(panel.sessionCookie(panel.admin.ID, true))

	recorder := panel.do(request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}
	if !strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("Content-Type = %q", recorder.Header().Get("Content-Type"))
	}
}

func TestCovDashboardRendersEveryPublishedTab(t *testing.T) {
	tabs := []string{"dashboard", "tenants", "properties", "leases", "receipts", "season", "calendar", "review", "intake", "conversations", "adjustment", "history", "metrics"}

	for _, tab := range tabs {
		t.Run(tab, func(t *testing.T) {
			panel := covSeedPanel(t)
			panel.inspections.items = []domain.Inspection{{ID: "insp-1", LeaseID: "lease-1", Kind: domain.InspectionKindCheckIn}}

			recorder := panel.as(http.MethodGet, "/dashboard?tab="+tab, nil, panel.admin.ID)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
			}
			if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
				t.Fatalf("Content-Type = %q", contentType)
			}
			if !strings.Contains(recorder.Body.String(), "<html") {
				t.Fatal("aba não devolveu a casca do painel")
			}
		})
	}
}

func TestCovDashboardFallsBackToTheDefaultTab(t *testing.T) {
	panel := covSeedPanel(t)

	for _, target := range []string{"/dashboard", "/dashboard?tab=inexistente", "/dashboard?tab="} {
		recorder := panel.as(http.MethodGet, target, nil, panel.admin.ID)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, esperado 200", target, recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), "<html") {
			t.Fatalf("%s: resposta sem a casca do painel", target)
		}
	}
}

func TestCovDashboardReportsSnapshotFailure(t *testing.T) {
	panel := newCovPanel(t)
	panel.tenants.err = errCovBoom

	recorder := panel.as(http.MethodGet, "/dashboard", nil, panel.admin.ID)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando o instantâneo falha", recorder.Code)
	}
}

func TestCovDashboardReportsTabContextFailure(t *testing.T) {
	panel := newCovPanel(t)
	panel.inspections.err = errCovBoom

	recorder := panel.as(http.MethodGet, "/dashboard?tab=season", nil, panel.admin.ID)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando o contexto da aba falha", recorder.Code)
	}
}

func TestCovDashboardReportsRenderFailure(t *testing.T) {
	panel := covSeedPanel(t)
	panel.common.Render = &Renderer{templates: template.Must(template.New("cov-vazio").Parse(""))}

	recorder := panel.as(http.MethodGet, "/dashboard", nil, panel.admin.ID)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando a aba não renderiza", recorder.Code)
	}
	record, ok := panel.logs.last()
	if !ok || record.Level.String() != "ERROR" {
		t.Fatalf("falha de renderização não registrada como erro: %+v", record)
	}
}

func TestCovDashboardRejectsAnonymousUser(t *testing.T) {
	panel := covSeedPanel(t)

	recorder := panel.as(http.MethodGet, "/dashboard", nil, "")
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/login" {
		t.Fatalf("status = %d location = %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestCovDashboardMetricsEndpoint(t *testing.T) {
	panel := covSeedPanel(t)

	recorder := panel.as(http.MethodGet, "/metrics", nil, panel.admin.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	payload := covDecodeJSON(t, recorder)
	if len(payload) == 0 {
		t.Fatal("payload de métricas vazio")
	}

	panel.receipts.err = errCovBoom
	failing := panel.as(http.MethodGet, "/metrics", nil, panel.admin.ID)
	if failing.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando o cálculo falha", failing.Code)
	}
}

func TestCovParseCalendarQueryFallsBackOutsideTheRange(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		year, month string
		wantYear    int
		wantMonth   int
	}{
		{year: "2025", month: "3", wantYear: 2025, wantMonth: 3},
		{year: "1970", month: "1", wantYear: 1970, wantMonth: 1},
		{year: "9999", month: "12", wantYear: 9999, wantMonth: 12},
		{year: "1969", month: "1", wantYear: 2026, wantMonth: 1},
		{year: "10000", month: "1", wantYear: 2026, wantMonth: 1},
		{year: "abc", month: "1", wantYear: 2026, wantMonth: 1},
		{year: "2025", month: "0", wantYear: 2025, wantMonth: 9},
		{year: "2025", month: "13", wantYear: 2025, wantMonth: 9},
		{year: "2025", month: "setembro", wantYear: 2025, wantMonth: 9},
		{year: " 2025 ", month: " 2 ", wantYear: 2025, wantMonth: 2},
		{year: "", month: "", wantYear: 2026, wantMonth: 9},
	}

	for _, testCase := range cases {
		year, month := parseCalendarQuery(testCase.year, testCase.month, now)
		if year != testCase.wantYear || month != testCase.wantMonth {
			t.Fatalf("parseCalendarQuery(%q, %q) = (%d, %d), esperado (%d, %d)", testCase.year, testCase.month, year, month, testCase.wantYear, testCase.wantMonth)
		}
	}
}

func TestCovNormalizeTabAcceptsOnlyKnownTabs(t *testing.T) {
	for _, tab := range dashboardTabs {
		if got := normalizeTab(tab); got != tab {
			t.Fatalf("normalizeTab(%q) = %q", tab, got)
		}
	}
	for _, tab := range []string{"", "inexistente", "Dashboard"} {
		if got := normalizeTab(tab); got != "dashboard" {
			t.Fatalf("normalizeTab(%q) = %q, esperado dashboard", tab, got)
		}
	}
}

func TestCovRenderTabIgnoresTabsWithoutTemplate(t *testing.T) {
	panel := newCovPanel(t)

	data := &tabData{}
	rendered, err := panel.handler.renderTab("inexistente", data)
	if err != nil {
		t.Fatalf("renderTab: %v", err)
	}
	if rendered != data || rendered.HTML != "" {
		t.Fatalf("renderTab devolveu %+v, esperado o mesmo contexto sem HTML", rendered)
	}
}

func TestCovMetricsRequiresAdminSession(t *testing.T) {
	panel := newCovPanel(t)

	if recorder := panel.as(http.MethodGet, "/metrics", nil, ""); recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, esperado 302 para o login", recorder.Code)
	}
}

func TestCovDashboardMutationDispatchesIntents(t *testing.T) {
	panel := covSeedPanel(t)

	unknown := panel.csrfPost("/dashboard", url.Values{"intent": {"intent.desconhecido"}})
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 para intent desconhecido", unknown.Code)
	}

	if recorder := panel.csrfPost("/dashboard", nil); recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 sem intent", recorder.Code)
	}
}

func TestCovAuthRootAndLoginPageRouting(t *testing.T) {
	panel := newCovPanel(t)

	anonymous := panel.as(http.MethodGet, "/", nil, "")
	if anonymous.Code != http.StatusFound || anonymous.Header().Get("Location") != "/login" {
		t.Fatalf("raiz anônima: status = %d location = %q", anonymous.Code, anonymous.Header().Get("Location"))
	}

	authenticated := panel.as(http.MethodGet, "/", nil, panel.admin.ID)
	if authenticated.Code != http.StatusFound || authenticated.Header().Get("Location") != "/dashboard?tab=dashboard" {
		t.Fatalf("raiz autenticada: status = %d location = %q", authenticated.Code, authenticated.Header().Get("Location"))
	}

	page := panel.as(http.MethodGet, "/login", nil, "")
	if page.Code != http.StatusOK {
		t.Fatalf("login: status = %d, esperado 200", page.Code)
	}
	if !strings.Contains(page.Body.String(), `name="csrf_token"`) {
		t.Fatal("formulário de login sem token de CSRF")
	}

	alreadyIn := panel.as(http.MethodGet, "/login", nil, panel.admin.ID)
	if alreadyIn.Code != http.StatusFound || alreadyIn.Header().Get("Location") != "/dashboard?tab=dashboard" {
		t.Fatalf("login com sessão: status = %d location = %q", alreadyIn.Code, alreadyIn.Header().Get("Location"))
	}
}

func TestCovAuthLogoutClearsTheSessionAndAudits(t *testing.T) {
	panel := newCovPanel(t)

	recorder := covFormPost(panel, "/logout", nil, panel.admin.ID)
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/login" {
		t.Fatalf("status = %d location = %q, esperado 302 /login", recorder.Code, recorder.Header().Get("Location"))
	}

	cleared := false
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == adminCookieName && cookie.MaxAge == -1 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("logout não limpou o cookie de sessão")
	}

	actions := panel.audit.actions()
	if len(actions) == 0 || actions[len(actions)-1] != domain.ActionLogout {
		t.Fatalf("logout não auditado: %v", actions)
	}
}

func TestCovAuthLogoutWithoutSessionStillRedirects(t *testing.T) {
	panel := newCovPanel(t)

	recorder := panel.as(http.MethodGet, "/logout", nil, "")
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/login" {
		t.Fatalf("status = %d location = %q", recorder.Code, recorder.Header().Get("Location"))
	}
	if len(panel.audit.actions()) != 0 {
		t.Fatalf("logout anônimo gerou trilha: %v", panel.audit.actions())
	}
}

func TestCovLoginPageFallsBackWhenTheTemplateFails(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.Render = &Renderer{templates: template.Must(template.New("cov-sem-login").Parse(""))}

	recorder := panel.as(http.MethodGet, "/login", nil, "")
	if body := recorder.Body.String(); strings.Contains(body, "<html") {
		t.Fatalf("fallback do login devolveu HTML: %s", body)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 mesmo sem template", recorder.Code)
	}
	if _, ok := panel.logs.find("falha ao renderizar login"); !ok {
		t.Fatal("falha de renderização do login não registrada")
	}
}

func TestCovLoginRejectsEmptyCredentials(t *testing.T) {
	panel := newCovPanel(t)

	recorder := covFormPost(panel, "/login", url.Values{"username": {"  "}, "password": {""}}, "")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Informe usuário e senha.") {
		t.Fatal("página sem a mensagem de credenciais ausentes")
	}
}

func TestCovLoginSucceedsWithValidCredentials(t *testing.T) {
	panel := newCovPanel(t)

	recorder := covFormPost(panel, "/login", url.Values{"username": {"passoz"}, "password": {"segredo"}}, "")
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, esperado 302: %s", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "/dashboard?tab=dashboard" {
		t.Fatalf("Location = %q", location)
	}
	if cookieValue(recorder.Result().Cookies(), adminCookieName) == "" {
		t.Fatal("login não emitiu o cookie de sessão")
	}
	if actions := panel.audit.actions(); len(actions) == 0 || actions[len(actions)-1] != domain.ActionLogin {
		t.Fatalf("login não auditado: %v", actions)
	}
}

func TestCovAuditAuthSurvivesAuditFailure(t *testing.T) {
	panel := newCovPanel(t)
	panel.audit.err = errCovBoom

	recorder := covFormPost(panel, "/login", url.Values{"username": {"passoz"}, "password": {"segredo"}}, "")
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, esperado 302 mesmo com a auditoria fora do ar", recorder.Code)
	}
	if cookieValue(recorder.Result().Cookies(), adminCookieName) == "" {
		t.Fatal("login não emitiu o cookie de sessão")
	}
}

func TestCovAuditContextSurvivesUserLookupFailure(t *testing.T) {
	panel := newCovPanel(t)
	panel.users.err = errCovBoom

	request := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	request.AddCookie(panel.sessionCookie(panel.admin.ID, true))

	actx := panel.handler.auditContext(request, domain.AuditOriginWeb)
	if actx.ActorName != nil {
		t.Fatalf("actor name = %v, esperado nulo quando a leitura falha", actx.ActorName)
	}
	if actx.ActorID == nil || *actx.ActorID != panel.admin.ID {
		t.Fatalf("actor = %v", actx.ActorID)
	}
}

func TestCovLoginPageForForcedPasswordChange(t *testing.T) {
	seeded := &domain.User{ID: "u-seed", Username: "passoz", PasswordHash: "covhash:pessoal"}
	panel := newCovPanelWith(t, seeded)
	panel.common.MustChangePasswordFunc = func(context.Context, string) (bool, error) { return true, nil }

	recorder := covFormPost(panel, "/login", url.Values{"username": {"passoz"}, "password": {"pessoal"}}, "")
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, esperado 302", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "/settings?forced=1" {
		t.Fatalf("Location = %q, esperado /settings?forced=1", location)
	}
}

func TestCovLoginRegistersFailureForRateLimit(t *testing.T) {
	panel := newCovPanel(t)

	recorder := covFormPost(panel, "/login", url.Values{"username": {"passoz"}, "password": {"errada"}}, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Usuário ou senha inválidos.") {
		t.Fatal("página sem a mensagem de credenciais inválidas")
	}
	if actions := panel.audit.actions(); len(actions) == 0 || actions[len(actions)-1] != domain.ActionLoginFailed {
		t.Fatalf("falha de login não auditada: %v", actions)
	}
}
