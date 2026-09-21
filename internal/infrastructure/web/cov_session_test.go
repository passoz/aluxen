package web

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// covSignedToken monta um token de sessão assinado com a chave do gerenciador,
// para exercitar validade e expiração sem depender do relógio real.
func covSignedToken(m *SessionManager, value string, deadline time.Time) string {
	payload := value + "|" + strconv.FormatInt(deadline.Unix(), 10)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encoded + "." + m.mac(encoded)
}

func covRequestWithCookie(name, value string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: name, Value: value})
	return request
}

func TestCovSessionAdminRoundTripAndCookieFlags(t *testing.T) {
	manager := NewSessionManager("cov-secret", false)

	recorder := httptest.NewRecorder()
	manager.SetAdmin(recorder, "user-1")

	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, esperado 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != adminCookieName || cookie.Path != "/" || !cookie.HttpOnly {
		t.Fatalf("cookie inesperado: %+v", cookie)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, esperado Lax", cookie.SameSite)
	}
	if cookie.Secure {
		t.Fatal("cookie sem Secure foi emitido com Secure")
	}
	if want := int(sessionTTL / time.Second); cookie.MaxAge != want {
		t.Fatalf("MaxAge = %d, esperado %d", cookie.MaxAge, want)
	}

	request := covRequestWithCookie(adminCookieName, cookie.Value)
	if id := manager.AdminID(request); id != "user-1" {
		t.Fatalf("AdminID = %q, esperado user-1", id)
	}
	if id := manager.ClientID(request); id != "" {
		t.Fatalf("ClientID = %q, esperado vazio para cookie de administrador", id)
	}
}

func TestCovSessionClientIsSeparateFromAdmin(t *testing.T) {
	manager := NewSessionManager("cov-secret", false)

	recorder := httptest.NewRecorder()
	manager.SetClient(recorder, "tenant-9")
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != clientCookieName {
		t.Fatalf("cookie de locatário inesperado: %+v", cookies)
	}

	request := covRequestWithCookie(clientCookieName, cookies[0].Value)
	if id := manager.ClientID(request); id != "tenant-9" {
		t.Fatalf("ClientID = %q, esperado tenant-9", id)
	}
	if id := manager.AdminID(request); id != "" {
		t.Fatalf("AdminID = %q, esperado vazio para cookie de locatário", id)
	}
}

func TestCovSessionSecureManagerMarksCookiesSecure(t *testing.T) {
	manager := NewSessionManager("cov-secret", true)

	recorder := httptest.NewRecorder()
	manager.SetAdmin(recorder, "user-1")
	if cookie := recorder.Result().Cookies()[0]; !cookie.Secure {
		t.Fatal("gerenciador seguro emitiu cookie sem Secure")
	}

	cleared := httptest.NewRecorder()
	manager.ClearAdmin(cleared)
	if cookie := cleared.Result().Cookies()[0]; !cookie.Secure {
		t.Fatal("limpeza de sessão emitiu cookie sem Secure")
	}
}

func TestCovSessionClearAdminExpiresCookie(t *testing.T) {
	manager := NewSessionManager("cov-secret", false)

	recorder := httptest.NewRecorder()
	manager.ClearAdmin(recorder)

	cookie := recorder.Result().Cookies()[0]
	if cookie.Name != adminCookieName || cookie.Value != "" {
		t.Fatalf("cookie de limpeza inesperado: %+v", cookie)
	}
	if cookie.MaxAge != -1 {
		t.Fatalf("MaxAge = %d, esperado -1", cookie.MaxAge)
	}
	if id := manager.AdminID(covRequestWithCookie(adminCookieName, cookie.Value)); id != "" {
		t.Fatalf("AdminID = %q após limpeza, esperado vazio", id)
	}
}

func TestCovSessionClearClientExpiresCookie(t *testing.T) {
	manager := NewSessionManager("cov-secret", false)

	recorder := httptest.NewRecorder()
	manager.ClearClient(recorder)

	cookie := recorder.Result().Cookies()[0]
	if cookie.Name != clientCookieName || cookie.Value != "" || cookie.MaxAge != -1 {
		t.Fatalf("cookie de limpeza inesperado: %+v", cookie)
	}
}

func TestCovSessionRejectsMissingCookie(t *testing.T) {
	manager := NewSessionManager("cov-secret", false)
	if id := manager.AdminID(httptest.NewRequest(http.MethodGet, "/", nil)); id != "" {
		t.Fatalf("AdminID = %q sem cookie, esperado vazio", id)
	}
}

func TestCovSessionRejectsTamperedCookie(t *testing.T) {
	manager := NewSessionManager("cov-secret", false)

	recorder := httptest.NewRecorder()
	manager.SetAdmin(recorder, "user-1")
	valid := recorder.Result().Cookies()[0].Value

	tampered := valid[:len(valid)-1] + "A"
	if tampered == valid {
		tampered = valid[:len(valid)-1] + "B"
	}
	if id := manager.AdminID(covRequestWithCookie(adminCookieName, tampered)); id != "" {
		t.Fatalf("AdminID = %q com assinatura adulterada, esperado vazio", id)
	}
}

func TestCovSessionRejectsForgedCookie(t *testing.T) {
	manager := NewSessionManager("cov-secret", false)
	other := NewSessionManager("cov-outro-secret", false)

	forged := []string{
		"user-1|9999999999",
		base64.RawURLEncoding.EncodeToString([]byte("user-1|9999999999")),
		base64.RawURLEncoding.EncodeToString([]byte("user-1|9999999999")) + ".assinatura-falsa",
		"sem-base64.assinatura",
	}
	for _, value := range forged {
		if id := manager.AdminID(covRequestWithCookie(adminCookieName, value)); id != "" {
			t.Fatalf("AdminID = %q para cookie forjado %q, esperado vazio", id, value)
		}
	}

	foreign := httptest.NewRecorder()
	other.SetAdmin(foreign, "user-forjado")
	if id := manager.AdminID(covRequestWithCookie(adminCookieName, foreign.Result().Cookies()[0].Value)); id != "" {
		t.Fatalf("AdminID = %q com token de outra chave, esperado vazio", id)
	}
}

func TestCovSessionRejectsMalformedPayloads(t *testing.T) {
	manager := NewSessionManager("cov-secret", false)

	cases := map[string]string{
		// payload válido em base64 mas sem o separador `|`.
		"sem separador":   manager.mac("x"),
		"sem valor":       covSignedToken(manager, "", time.Now().Add(time.Hour)),
		"prazo não é int": covSignedToken(manager, "user-1", time.Time{}),
	}
	// O caso sem separador precisa do envelope do token: base64("user-1") sem `|`.
	encoded := base64.RawURLEncoding.EncodeToString([]byte("user-1"))
	cases["sem separador"] = encoded + "." + manager.mac(encoded)

	for name, value := range cases {
		if id := manager.AdminID(covRequestWithCookie(adminCookieName, value)); id != "" {
			t.Fatalf("%s: AdminID = %q, esperado vazio", name, id)
		}
	}
}

func TestCovSessionRejectsExpiredCookie(t *testing.T) {
	manager := NewSessionManager("cov-secret", false)

	expired := covSignedToken(manager, "user-1", time.Now().Add(-time.Minute))
	if id := manager.AdminID(covRequestWithCookie(adminCookieName, expired)); id != "" {
		t.Fatalf("AdminID = %q com sessão expirada, esperado vazio", id)
	}

	alive := covSignedToken(manager, "user-1", time.Now().Add(time.Minute))
	if id := manager.AdminID(covRequestWithCookie(adminCookieName, alive)); id != "user-1" {
		t.Fatalf("AdminID = %q com sessão válida, esperado user-1", id)
	}
}

func TestCovSessionProtectedRouteAcceptsOnlyItsOwnCookie(t *testing.T) {
	panel := newCovPanel(t)

	if recorder := panel.as(http.MethodGet, "/api/info", nil, ""); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d sem sessão, esperado 401", recorder.Code)
	}
	if recorder := panel.as(http.MethodGet, "/api/info", nil, "outro-usuario"); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d com sessão de usuário inexistente, esperado 401", recorder.Code)
	}
	if recorder := panel.as(http.MethodGet, "/api/info", nil, panel.admin.ID); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d com sessão válida, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
}

func TestCovSessionCookieDoesNotLeakIDOutsideSignature(t *testing.T) {
	manager := NewSessionManager("cov-secret", false)

	recorder := httptest.NewRecorder()
	manager.SetAdmin(recorder, "user-1")
	cookie := recorder.Result().Cookies()[0]

	if strings.Contains(cookie.Value, "user-1") {
		t.Fatalf("valor do cookie expõe o id cru: %q", cookie.Value)
	}
}
