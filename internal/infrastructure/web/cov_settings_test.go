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

// covFormPost envia um POST autenticado (opcional) com o token de CSRF válido.
func covFormPost(panel *covPanel, target string, form url.Values, userID string) *httptest.ResponseRecorder {
	page := httptest.NewRecorder()
	panel.handler.Router().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/login", nil))
	token := cookieValue(page.Result().Cookies(), csrfCookieName)
	if token == "" {
		panel.t.Fatal("cookie de CSRF não emitido")
	}
	if form == nil {
		form = url.Values{}
	}
	form.Set(csrfFieldName, token)

	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range page.Result().Cookies() {
		request.AddCookie(cookie)
	}
	if userID != "" {
		request.AddCookie(panel.sessionCookie(userID, true))
	}
	return panel.do(request)
}

func TestCovSettingsPageRequiresAdminSession(t *testing.T) {
	panel := newCovPanel(t)

	recorder := panel.as(http.MethodGet, "/settings", nil, "")
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, esperado 302", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "/login" {
		t.Fatalf("Location = %q, esperado /login", location)
	}
}

func TestCovSettingsPageRendersForAdmin(t *testing.T) {
	panel := newCovPanel(t)
	panel.settings.values[domain.SettingLandlordName] = "Maria Locadora"
	panel.settings.values[domain.SettingAppURL] = "https://aluxen.example"

	recorder := panel.as(http.MethodGet, "/settings", nil, panel.admin.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("Content-Type = %q", contentType)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `name="csrf_token"`) {
		t.Fatal("formulário de configurações sem campo csrf_token")
	}
	if !strings.Contains(body, "Maria Locadora") {
		t.Fatal("página de configurações sem os dados do locador")
	}
}

func TestCovSettingsSaveLandlordPersistsData(t *testing.T) {
	panel := newCovPanel(t)

	recorder := covFormPost(panel, "/settings", url.Values{
		"intent": {"save-landlord"},
		"name":   {"Maria Locadora"},
		"cpf":    {"12345678900"},
		"email":  {"maria@example.com"},
	}, panel.admin.ID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	if panel.settings.values[domain.SettingLandlordName] != "Maria Locadora" {
		t.Fatalf("nome do locador não gravado: %v", panel.settings.values)
	}
	if panel.settings.values[domain.SettingLandlordCPF] != "12345678900" {
		t.Fatalf("CPF do locador não gravado: %v", panel.settings.values)
	}
	if panel.settings.values[domain.SettingLandlordEmail] != "maria@example.com" {
		t.Fatalf("e-mail do locador não gravado: %v", panel.settings.values)
	}
	if actions := panel.audit.actions(); len(actions) == 0 {
		t.Fatal("alteração de configurações não registrada na trilha")
	}
}

func TestCovSettingsSaveLandlordRejectsInvalidInput(t *testing.T) {
	cases := map[string]struct {
		form    url.Values
		message string
	}{
		"nome vazio": {
			form:    url.Values{"intent": {"save-landlord"}, "name": {"   "}, "email": {"maria@example.com"}},
			message: "Informe o nome do locador.",
		},
		"e-mail inválido": {
			form:    url.Values{"intent": {"save-landlord"}, "name": {"Maria"}, "email": {"nao-e-email"}},
			message: "Informe um e-mail válido.",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			panel := newCovPanel(t)
			recorder := covFormPost(panel, "/settings", testCase.form, panel.admin.ID)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, esperado 400", recorder.Code)
			}
			if !strings.Contains(recorder.Body.String(), testCase.message) {
				t.Fatalf("página sem a mensagem de validação %q", testCase.message)
			}
			if len(panel.settings.values) != 0 {
				t.Fatalf("dados inválidos foram gravados: %v", panel.settings.values)
			}
		})
	}
}

func TestCovSettingsSaveAppURL(t *testing.T) {
	panel := newCovPanel(t)

	recorder := covFormPost(panel, "/settings", url.Values{
		"intent":  {"save-app-url"},
		"app_url": {"https://aluxen.example/"},
	}, panel.admin.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	if panel.settings.values[domain.SettingAppURL] != "https://aluxen.example/" {
		t.Fatalf("URL pública não gravada: %v", panel.settings.values)
	}

	invalid := covFormPost(panel, "/settings", url.Values{
		"intent":  {"save-app-url"},
		"app_url": {"aluxen.example"},
	}, panel.admin.ID)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("URL sem esquema: status = %d, esperado 400", invalid.Code)
	}
	if !strings.Contains(invalid.Body.String(), "Informe uma URL começando por http:// ou https://.") {
		t.Fatalf("página sem a mensagem de URL inválida")
	}
	if panel.settings.values[domain.SettingAppURL] != "https://aluxen.example/" {
		t.Fatalf("URL inválida sobrescreveu a configuração: %v", panel.settings.values)
	}
}

func TestCovSettingsChangePassword(t *testing.T) {
	panel := newCovPanel(t)

	recorder := covFormPost(panel, "/settings", url.Values{
		"intent":           {"change-password"},
		"current_password": {"segredo"},
		"new_password":     {"novasegredo"},
		"confirm_password": {"novasegredo"},
	}, panel.admin.ID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	if panel.admin.PasswordHash != "covhash:novasegredo" {
		t.Fatalf("senha não trocada: %q", panel.admin.PasswordHash)
	}
	if !strings.Contains(recorder.Body.String(), "Senha alterada com sucesso.") {
		t.Fatal("página sem a confirmação da troca de senha")
	}
	if actions := panel.audit.actions(); len(actions) == 0 || actions[len(actions)-1] != domain.ActionUserPassword {
		t.Fatalf("troca de senha não auditada: %v", actions)
	}
}

func TestCovSettingsChangePasswordRejectsInvalidCombinations(t *testing.T) {
	cases := map[string]struct {
		current, fresh, confirm, message string
	}{
		"senha atual errada":     {current: "errada", fresh: "novasegredo", confirm: "novasegredo", message: "Senha atual incorreta."},
		"confirmação divergente": {current: "segredo", fresh: "novasegredo", confirm: "outra", message: "A confirmação não corresponde à nova senha."},
		"senha curta":            {current: "segredo", fresh: "123", confirm: "123", message: "Nova senha deve ter no mínimo 6 caracteres."},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			panel := newCovPanel(t)
			recorder := covFormPost(panel, "/settings", url.Values{
				"intent":           {"change-password"},
				"current_password": {testCase.current},
				"new_password":     {testCase.fresh},
				"confirm_password": {testCase.confirm},
			}, panel.admin.ID)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, esperado 400", recorder.Code)
			}
			if !strings.Contains(recorder.Body.String(), testCase.message) {
				t.Fatalf("página sem a mensagem %q", testCase.message)
			}
			if panel.admin.PasswordHash != "covhash:segredo" {
				t.Fatalf("senha alterada em caso inválido: %q", panel.admin.PasswordHash)
			}
		})
	}
}

func TestCovSettingsChangePasswordSendsForcedAdminToTheDashboard(t *testing.T) {
	seeded := &domain.User{ID: "u-seed", Username: "passoz", PasswordHash: "covhash:pessoal"}
	panel := newCovPanelWith(t, seeded)

	recorder := covFormPost(panel, "/settings", url.Values{
		"intent":           {"change-password"},
		"current_password": {"pessoal"},
		"new_password":     {"novasegredo"},
		"confirm_password": {"novasegredo"},
	}, seeded.ID)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, esperado 302 depois da troca obrigatória: %s", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "/dashboard?tab=dashboard" {
		t.Fatalf("Location = %q, esperado /dashboard?tab=dashboard", location)
	}
	if seeded.PasswordHash != "covhash:novasegredo" {
		t.Fatalf("senha não trocada: %q", seeded.PasswordHash)
	}
}

func TestCovSettingsUnknownIntentIsRejected(t *testing.T) {
	panel := newCovPanel(t)

	recorder := covFormPost(panel, "/settings", url.Values{"intent": {"intent.desconhecido"}}, panel.admin.ID)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Ação inválida.") {
		t.Fatal("página sem a mensagem de ação inválida")
	}
}

func TestCovSettingsRejectsAnonymousMutation(t *testing.T) {
	panel := newCovPanel(t)

	recorder := covFormPost(panel, "/settings", url.Values{"intent": {"save-app-url"}, "app_url": {"https://x.example"}}, "")
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/login" {
		t.Fatalf("status = %d location = %q, esperado 302 /login", recorder.Code, recorder.Header().Get("Location"))
	}
	if len(panel.settings.values) != 0 {
		t.Fatalf("mutation anônima gravou configuração: %v", panel.settings.values)
	}
}

func TestCovSettingsSurfacesStorageFailure(t *testing.T) {
	panel := newCovPanel(t)
	panel.settings.setErr = errCovBoom

	recorder := covFormPost(panel, "/settings", url.Values{
		"intent": {"save-landlord"},
		"name":   {"Maria"},
		"email":  {"maria@example.com"},
	}, panel.admin.ID)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando a gravação falha", recorder.Code)
	}
}

func TestCovSettingsPageSurfacesLoadFailure(t *testing.T) {
	panel := newCovPanel(t)
	panel.settings.err = errCovBoom

	recorder := panel.as(http.MethodGet, "/settings", nil, panel.admin.ID)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando as configurações não carregam", recorder.Code)
	}
}

func TestCovSettingsMutationSurfacesPasswordStateFailure(t *testing.T) {
	panel := newCovPanel(t)
	// A sessão já foi validada uma vez; a checagem seguinte (estado da senha)
	// encontra o banco fora do ar.
	panel.users.failAfterIDReads = 1

	recorder := covFormPost(panel, "/settings", url.Values{
		"intent":           {"change-password"},
		"current_password": {"segredo"},
		"new_password":     {"novasegredo"},
		"confirm_password": {"novasegredo"},
	}, panel.admin.ID)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando o estado da senha não carrega", recorder.Code)
	}
}

func TestCovSettingsPasswordChangeWithNoUsersHook(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.MustChangePasswordFunc = nil

	if recorder := panel.as(http.MethodGet, "/settings", nil, panel.admin.ID); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}
}

func TestCovSettingsAuditFailureDoesNotBreakPasswordChange(t *testing.T) {
	panel := newCovPanel(t)
	panel.audit.err = errCovBoom

	recorder := covFormPost(panel, "/settings", url.Values{
		"intent":           {"change-password"},
		"current_password": {"segredo"},
		"new_password":     {"novasegredo"},
		"confirm_password": {"novasegredo"},
	}, panel.admin.ID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 mesmo com a auditoria fora do ar: %s", recorder.Code, recorder.Body.String())
	}
	if panel.admin.PasswordHash != "covhash:novasegredo" {
		t.Fatalf("senha não trocada: %q", panel.admin.PasswordHash)
	}
}

func TestCovSettingsPageShowsForcedFlagFromQuery(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.MustChangePasswordFunc = func(context.Context, string) (bool, error) { return true, nil }

	recorder := panel.as(http.MethodGet, "/settings?forced=1", nil, panel.admin.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}
}
