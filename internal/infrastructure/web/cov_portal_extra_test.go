package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// covTenantFixture é o locatário usado nas rotas da Área do Cliente.
func covTenantFixture() *domain.Tenant {
	return &domain.Tenant{
		ID:                "tenant-1",
		Name:              "Ana",
		Email:             "ana@example.com",
		CPF:               covString("12345678900"),
		PasswordHash:      covString("covhash:pessoal"),
		PasswordChangedAt: covString("2026-02-01T00:00:00Z"),
	}
}

// covPortalPanel monta o painel com o locatário e o contrato/titular coerentes.
func covPortalPanel(t *testing.T) *covPanel {
	t.Helper()
	panel := covSeedPanel(t)
	panel.tenants.items = []domain.Tenant{*covTenantFixture()}
	return panel
}

// covClientLogin autentica na Área do Cliente e devolve a resposta.
func covClientLogin(panel *covPanel, cpf, password string) *httptest.ResponseRecorder {
	page := httptest.NewRecorder()
	panel.handler.Router().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/portal/login", nil))
	token := covCookieValue(page.Result().Cookies(), csrfCookieName)
	if token == "" {
		panel.t.Fatal("cookie de CSRF não emitido em /portal/login")
	}

	form := url.Values{"cpf": {cpf}, "password": {password}, csrfFieldName: {token}}
	request := httptest.NewRequest(http.MethodPost, "/portal/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range page.Result().Cookies() {
		request.AddCookie(cookie)
	}
	return panel.do(request)
}

func TestCovPortalLoginSucceedsAndOpensTheSession(t *testing.T) {
	panel := covPortalPanel(t)

	recorder := covClientLogin(panel, "123.456.789-00", "pessoal")
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, esperado 302: %s", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "/portal" {
		t.Fatalf("Location = %q, esperado /portal", location)
	}
	cookie := covCookieValue(recorder.Result().Cookies(), clientCookieName)
	if cookie == "" {
		t.Fatal("login do locatário não emitiu sessão")
	}

	// A sessão emitida abre a Área do Cliente.
	request := httptest.NewRequest(http.MethodGet, "/portal", nil)
	request.AddCookie(&http.Cookie{Name: clientCookieName, Value: cookie})
	if home := panel.do(request); home.Code != http.StatusOK {
		t.Fatalf("Área do Cliente: status = %d, esperado 200", home.Code)
	}

	actions := panel.audit.actions()
	if len(actions) == 0 || actions[len(actions)-1] != domain.ActionClientLogin {
		t.Fatalf("acesso do locatário não auditado: %v", actions)
	}
}

func TestCovPortalLoginSendsFirstAccessToThePasswordScreen(t *testing.T) {
	panel := covPortalPanel(t)
	panel.tenants.items[0].PasswordChangedAt = nil

	recorder := covClientLogin(panel, "12345678900", "pessoal")
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, esperado 302", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "/portal/password" {
		t.Fatalf("Location = %q, esperado /portal/password", location)
	}
}

func TestCovPortalLoginRejectsInvalidCredentials(t *testing.T) {
	panel := covPortalPanel(t)

	empty := covClientLogin(panel, "  ", "")
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("credenciais ausentes: status = %d, esperado 400", empty.Code)
	}
	if !strings.Contains(empty.Body.String(), "Informe CPF e senha.") {
		t.Fatal("página sem a mensagem de campos obrigatórios")
	}

	wrong := covClientLogin(panel, "12345678900", "errada")
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("senha errada: status = %d, esperado 401", wrong.Code)
	}
	if covCookieValue(wrong.Result().Cookies(), clientCookieName) != "" {
		t.Fatal("tentativa negada emitiu sessão")
	}
	actions := panel.audit.actions()
	if len(actions) == 0 || actions[len(actions)-1] != domain.ActionClientLoginFailed {
		t.Fatalf("recusa não auditada: %v", actions)
	}
}

func TestCovPortalLoginGateReasonsReachTheAuditTrail(t *testing.T) {
	clock := func() *covClock { return &covClock{now: covPanelNow()} }

	cases := []struct {
		name   string
		build  func(clock *covClock) *application.LoginGate
		run    func(panel *covPanel)
		reason string
	}{
		{
			name:  "limite por IP",
			build: func(clock *covClock) *application.LoginGate { return application.NewLoginGate(clock, 1, 100, 50) },
			run: func(panel *covPanel) {
				covClientLogin(panel, "12345678900", "pessoal")
				covClientLogin(panel, "12345678900", "pessoal")
			},
			reason: "Limite de tentativas por minuto excedido",
		},
		{
			name:  "limite global",
			build: func(clock *covClock) *application.LoginGate { return application.NewLoginGate(clock, 100, 1, 50) },
			run: func(panel *covPanel) {
				covClientLogin(panel, "12345678900", "pessoal")
				covClientLogin(panel, "12345678900", "pessoal")
			},
			reason: "Limite global de tentativas por minuto excedido",
		},
		{
			name:  "bloqueio progressivo",
			build: func(clock *covClock) *application.LoginGate { return application.NewLoginGate(clock, 100, 100, 1) },
			run: func(panel *covPanel) {
				covClientLogin(panel, "12345678900", "errada")
				covClientLogin(panel, "12345678900", "pessoal")
			},
			reason: "IP bloqueado temporariamente por tentativas repetidas",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			panel := covPortalPanel(t)
			panel.common.LoginGate = testCase.build(clock())
			testCase.run(panel)

			entry, ok := covLastAuditEntry(panel)
			if !ok {
				t.Fatal("recusa do gate não auditada")
			}
			if entry.Metadata == nil {
				t.Fatal("recusa do gate sem metadados")
			}
			var metadata map[string]any
			if err := json.Unmarshal([]byte(*entry.Metadata), &metadata); err != nil {
				t.Fatalf("metadados da trilha não são JSON: %v", err)
			}
			if metadata["reason"] != testCase.reason {
				t.Fatalf("motivo registrado = %v, esperado %q", metadata["reason"], testCase.reason)
			}
		})
	}
}

func TestCovPortalLoginGateBlocksWithRetryAfter(t *testing.T) {
	panel := covPortalPanel(t)
	panel.common.LoginGate = application.NewLoginGate(&covClock{now: covPanelNow()}, 1, 100, 50)

	covClientLogin(panel, "12345678900", "pessoal")
	recorder := covClientLogin(panel, "12345678900", "pessoal")

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, esperado 429", recorder.Code)
	}
	if recorder.Header().Get("Retry-After") == "" {
		t.Fatal("resposta 429 sem Retry-After")
	}
}

func TestCovPortalHomeRedirectsFirstAccessToPassword(t *testing.T) {
	panel := covPortalPanel(t)
	panel.tenants.items[0].PasswordChangedAt = nil

	request := httptest.NewRequest(http.MethodGet, "/portal", nil)
	request.AddCookie(panel.sessionCookie(panel.tenants.items[0].ID, false))

	recorder := panel.do(request)
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/portal/password" {
		t.Fatalf("status = %d location = %q, esperado 302 /portal/password", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestCovPortalHomeShowsPasswordConfirmation(t *testing.T) {
	panel := covPortalPanel(t)

	request := httptest.NewRequest(http.MethodGet, "/portal?ok=senha", nil)
	request.AddCookie(panel.sessionCookie(panel.tenants.items[0].ID, false))

	recorder := panel.do(request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("Content-Type = %q", contentType)
	}
	if !strings.Contains(recorder.Body.String(), "Ana") {
		t.Fatal("Área do Cliente sem os dados do titular")
	}
}

func TestCovPortalPagesRequireClientSession(t *testing.T) {
	panel := covPortalPanel(t)

	for _, target := range []string{"/portal", "/portal/password"} {
		recorder := panel.do(httptest.NewRequest(http.MethodGet, target, nil))
		if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/portal/login" {
			t.Fatalf("%s: status = %d location = %q, esperado 302 /portal/login", target, recorder.Code, recorder.Header().Get("Location"))
		}
	}
}

func TestCovPortalDropsRemovedTenantSession(t *testing.T) {
	panel := covPortalPanel(t)

	request := httptest.NewRequest(http.MethodGet, "/portal", nil)
	request.AddCookie(panel.sessionCookie("tenant-removido", false))

	recorder := panel.do(request)
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/portal/login" {
		t.Fatalf("status = %d location = %q", recorder.Code, recorder.Header().Get("Location"))
	}
	cleared := false
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == clientCookieName && cookie.MaxAge == -1 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("sessão do locatário removido não foi limpa")
	}
}

func TestCovPortalSurvivesTenantLookupFailure(t *testing.T) {
	panel := covPortalPanel(t)
	panel.tenants.err = errCovBoom

	request := httptest.NewRequest(http.MethodGet, "/portal", nil)
	request.AddCookie(panel.sessionCookie(panel.tenants.items[0].ID, false))

	recorder := panel.do(request)
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/portal/login" {
		t.Fatalf("status = %d location = %q, esperado 302 /portal/login", recorder.Code, recorder.Header().Get("Location"))
	}
	if _, ok := panel.logs.find("falha ao carregar o locatário"); !ok {
		t.Fatal("falha de leitura do locatário não registrada")
	}
}

func TestCovPortalPasswordChange(t *testing.T) {
	panel := covPortalPanel(t)
	tenantID := panel.tenants.items[0].ID

	page := panel.asClient(http.MethodGet, "/portal/password", tenantID)
	if page.Code != http.StatusOK {
		t.Fatalf("tela de senha: status = %d, esperado 200", page.Code)
	}

	mismatch := panel.csrfPostAs("/portal/password", url.Values{
		"current_password": {"pessoal"},
		"new_password":     {"novasenha"},
		"confirm_password": {"outra"},
	}, tenantID, false)
	if mismatch.Code != http.StatusBadRequest {
		t.Fatalf("confirmação divergente: status = %d, esperado 400", mismatch.Code)
	}
	if !strings.Contains(mismatch.Body.String(), "A confirmação não corresponde à nova senha.") {
		t.Fatal("página sem a mensagem de confirmação divergente")
	}

	wrongCurrent := panel.csrfPostAs("/portal/password", url.Values{
		"current_password": {"errada"},
		"new_password":     {"novasenha"},
		"confirm_password": {"novasenha"},
	}, tenantID, false)
	if wrongCurrent.Code != http.StatusBadRequest {
		t.Fatalf("senha atual errada: status = %d, esperado 400", wrongCurrent.Code)
	}

	success := panel.csrfPostAs("/portal/password", url.Values{
		"current_password": {"pessoal"},
		"new_password":     {"novasenha"},
		"confirm_password": {"novasenha"},
	}, tenantID, false)
	if success.Code != http.StatusFound {
		t.Fatalf("troca válida: status = %d, esperado 302: %s", success.Code, success.Body.String())
	}
	if location := success.Header().Get("Location"); location != "/portal?ok=senha" {
		t.Fatalf("Location = %q, esperado /portal?ok=senha", location)
	}
	if hash := domain.Str(panel.tenants.items[0].PasswordHash); hash != "covhash:novasenha" {
		t.Fatalf("senha do locatário = %q, esperado o novo hash", hash)
	}
	if actions := panel.audit.actions(); len(actions) == 0 || actions[len(actions)-1] != domain.ActionClientPassword {
		t.Fatalf("troca de senha do locatário não auditada: %v", actions)
	}
}

func TestCovPortalPasswordChangeReportsStorageFailure(t *testing.T) {
	panel := covPortalPanel(t)
	panel.tenants.updatePasswordErr = errCovBoom
	tenantID := panel.tenants.items[0].ID

	recorder := panel.csrfPostAs("/portal/password", url.Values{
		"current_password": {"pessoal"},
		"new_password":     {"novasenha"},
		"confirm_password": {"novasenha"},
	}, tenantID, false)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando a gravação falha", recorder.Code)
	}
}

func TestCovPortalLogoutEndsTheSession(t *testing.T) {
	panel := covPortalPanel(t)
	tenantID := panel.tenants.items[0].ID

	recorder := panel.csrfPostAs("/portal/logout", nil, tenantID, false)
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/portal/login" {
		t.Fatalf("status = %d location = %q, esperado 302 /portal/login", recorder.Code, recorder.Header().Get("Location"))
	}
	cleared := false
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == clientCookieName && cookie.MaxAge == -1 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("logout do locatário não limpou a sessão")
	}
	entry, ok := covLastAuditEntry(panel)
	if !ok || entry.Action != domain.ActionClientLogout {
		t.Fatalf("saída não auditada: %+v", entry)
	}
	if domain.Str(entry.EntityLabel) != "Ana" {
		t.Fatalf("rótulo da entidade = %q, esperado o nome do locatário", domain.Str(entry.EntityLabel))
	}
}

func TestCovPortalLogoutWithoutSession(t *testing.T) {
	panel := covPortalPanel(t)

	recorder := panel.csrfPostAs("/portal/logout", nil, "", false)
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/portal/login" {
		t.Fatalf("status = %d location = %q", recorder.Code, recorder.Header().Get("Location"))
	}
	entry, ok := covLastAuditEntry(panel)
	if !ok || entry.Action != domain.ActionClientLogout {
		t.Fatalf("saída anônima não auditada: %+v", entry)
	}
	if domain.Str(entry.EntityLabel) != "" {
		t.Fatalf("saída anônima com rótulo %q, esperado vazio", domain.Str(entry.EntityLabel))
	}
}

func TestCovPortalLoginPageRedirectsWhenAlreadySignedIn(t *testing.T) {
	panel := covPortalPanel(t)
	tenantID := panel.tenants.items[0].ID

	request := httptest.NewRequest(http.MethodGet, "/portal/login", nil)
	request.AddCookie(panel.sessionCookie(tenantID, false))

	recorder := panel.do(request)
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/portal" {
		t.Fatalf("status = %d location = %q, esperado 302 /portal", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestCovPortalRenderFallbackWhenTemplateFails(t *testing.T) {
	panel := covPortalPanel(t)
	panel.common.Render = &Renderer{templates: template.Must(template.New("cov-sem-portal").Parse(""))}

	recorder := panel.do(httptest.NewRequest(http.MethodGet, "/portal/login", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}
	if body := recorder.Body.String(); strings.Contains(body, "<html") {
		t.Fatalf("fallback da Área do Cliente devolveu HTML: %s", body)
	}
	if _, ok := panel.logs.find("falha ao renderizar a Área do Cliente"); !ok {
		t.Fatal("falha de renderização da Área do Cliente não registrada")
	}
}

// covLastAuditEntry devolve a última linha gravada na trilha.
func covLastAuditEntry(panel *covPanel) (domain.AuditLog, bool) {
	if len(panel.audit.entries) == 0 {
		return domain.AuditLog{}, false
	}
	return panel.audit.entries[len(panel.audit.entries)-1], true
}

// covPanelNow é o instante fixo do relógio dos dublês.
func covPanelNow() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }

func TestCovPortalAuditFailureDoesNotBreakTheLogin(t *testing.T) {
	panel := covPortalPanel(t)
	panel.audit.err = errCovBoom

	recorder := covClientLogin(panel, "12345678900", "pessoal")
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, esperado 302 mesmo com a auditoria fora do ar", recorder.Code)
	}
	if covCookieValue(recorder.Result().Cookies(), clientCookieName) == "" {
		t.Fatal("login do locatário não emitiu sessão")
	}
	if _, ok := panel.logs.find("auditoria da Área do Cliente não gravada"); !ok {
		t.Fatal("falha de auditoria da Área do Cliente não registrada")
	}
}

func TestCovClientGateReasonLabels(t *testing.T) {
	cases := map[string]string{
		"locked": "IP bloqueado temporariamente por tentativas repetidas",
		"ip":     "Limite de tentativas por minuto excedido",
		"global": "Limite global de tentativas por minuto excedido",
	}
	for reason, want := range cases {
		if got := clientGateReasonLabel(reason); got != want {
			t.Fatalf("clientGateReasonLabel(%q) = %q, esperado %q", reason, got, want)
		}
	}

	// Motivo novo do limitador não pode sair sem descrição na trilha.
	if got := clientGateReasonLabel("motivo-desconhecido"); strings.TrimSpace(got) == "" {
		t.Fatal("motivo desconhecido ficou sem descrição")
	}
}

func TestCovPortalHomeReportsAreaLoadFailure(t *testing.T) {
	panel := covPortalPanel(t)
	panel.leases.err = errCovBoom

	request := httptest.NewRequest(http.MethodGet, "/portal", nil)
	request.AddCookie(panel.sessionCookie(panel.tenants.items[0].ID, false))

	recorder := panel.do(request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando os dados do titular não carregam", recorder.Code)
	}
}
