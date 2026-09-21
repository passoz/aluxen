package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// covCSRFProbe conta as vezes que a requisição chegou ao handler protegido.
func covCSRFProbe(csrf *CSRF) (http.Handler, *int) {
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	})
	return csrf.Middleware(next), &calls
}

func covCSRFRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return request
}

func TestCovCSRFTokenIssuesCookieOnFirstVisit(t *testing.T) {
	csrf := NewCSRF("cov-secret", false)

	recorder := httptest.NewRecorder()
	token := csrf.Token(recorder, httptest.NewRequest(http.MethodGet, "/login", nil))

	if token == "" {
		t.Fatal("token vazio na primeira visita")
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, esperado 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != csrfCookieName || cookie.Value != token || !cookie.HttpOnly || cookie.Path != "/" {
		t.Fatalf("cookie de CSRF inesperado: %+v", cookie)
	}
	if cookie.Secure {
		t.Fatal("cookie de CSRF sem Secure foi emitido com Secure")
	}
	if cookie.MaxAge != 86400 {
		t.Fatalf("MaxAge = %d, esperado 86400", cookie.MaxAge)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, esperado Lax", cookie.SameSite)
	}
}

func TestCovCSRFTokenReusesValidCookie(t *testing.T) {
	csrf := NewCSRF("cov-secret", false)

	first := httptest.NewRecorder()
	token := csrf.Token(first, httptest.NewRequest(http.MethodGet, "/login", nil))

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.AddCookie(first.Result().Cookies()[0])

	second := httptest.NewRecorder()
	if reused := csrf.Token(second, request); reused != token {
		t.Fatalf("token = %q, esperado o mesmo cookie válido", reused)
	}
	if len(second.Result().Cookies()) != 0 {
		t.Fatal("token válido reaproveitado emitiu cookie novo")
	}
}

func TestCovCSRFTokenReplacesInvalidCookie(t *testing.T) {
	csrf := NewCSRF("cov-secret", false)
	other := NewCSRF("cov-outro-secret", false)

	foreign := httptest.NewRecorder()
	foreignToken := other.Token(foreign, httptest.NewRequest(http.MethodGet, "/login", nil))

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: foreignToken})

	recorder := httptest.NewRecorder()
	token := csrf.Token(recorder, request)
	if token == "" || token == foreignToken {
		t.Fatalf("token = %q, esperado token novo para cookie com assinatura inválida", token)
	}
	if len(recorder.Result().Cookies()) != 1 {
		t.Fatal("cookie inválido não foi substituído")
	}
}

func TestCovCSRFTokenIsUnpredictable(t *testing.T) {
	csrf := NewCSRF("cov-secret", false)

	seen := map[string]bool{}
	for range 16 {
		recorder := httptest.NewRecorder()
		token := csrf.Token(recorder, httptest.NewRequest(http.MethodGet, "/login", nil))
		if seen[token] {
			t.Fatal("token repetido entre visitas sem cookie")
		}
		seen[token] = true
	}
}

func TestCovCSRFMiddlewareRejectsMutatingRequestsWithoutToken(t *testing.T) {
	csrf := NewCSRF("cov-secret", false)
	handler, calls := covCSRFProbe(csrf)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, covCSRFRequest(method, "/dashboard", "intent=tenant.create"))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s sem token: status = %d, esperado 403", method, recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), "csrf") {
			t.Fatalf("%s sem token: corpo = %q, esperado erro de CSRF", method, recorder.Body.String())
		}
	}
	if *calls != 0 {
		t.Fatalf("handler protegido foi chamado %d vezes sem token", *calls)
	}
}

func TestCovCSRFMiddlewareRejectsMismatchedOrUnsignedToken(t *testing.T) {
	csrf := NewCSRF("cov-secret", false)
	handler, calls := covCSRFProbe(csrf)

	valid := httptest.NewRecorder()
	token := csrf.Token(valid, httptest.NewRequest(http.MethodGet, "/login", nil))

	cases := map[string]string{
		"campo ausente":       "",
		"campo divergente":    "outro-valor." + token,
		"sem assinatura":      strings.SplitN(token, ".", 2)[0],
		"assinatura truncada": token[:len(token)-1],
	}
	for name, sent := range cases {
		request := covCSRFRequest(http.MethodPost, "/dashboard", url.Values{csrfFieldName: {sent}}.Encode())
		request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s: status = %d, esperado 403", name, recorder.Code)
		}
	}
	if *calls != 0 {
		t.Fatalf("handler protegido foi chamado %d vezes com token inválido", *calls)
	}
}

func TestCovCSRFMiddlewareAcceptsMatchingSignedToken(t *testing.T) {
	csrf := NewCSRF("cov-secret", false)
	handler, calls := covCSRFProbe(csrf)

	issued := httptest.NewRecorder()
	token := csrf.Token(issued, httptest.NewRequest(http.MethodGet, "/login", nil))

	request := covCSRFRequest(http.MethodPost, "/dashboard", url.Values{csrfFieldName: {token}}.Encode())
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, esperado 204: %s", recorder.Code, recorder.Body.String())
	}
	if *calls != 1 {
		t.Fatalf("handler protegido chamado %d vezes, esperado 1", *calls)
	}
}

func TestCovCSRFMiddlewareLetsSafeMethodsThrough(t *testing.T) {
	csrf := NewCSRF("cov-secret", false)
	handler, calls := covCSRFProbe(csrf)

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, covCSRFRequest(method, "/dashboard", ""))
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("%s: status = %d, esperado 204 sem CSRF", method, recorder.Code)
		}
	}
	if *calls != 3 {
		t.Fatalf("handler chamado %d vezes, esperado 3", *calls)
	}
}

func TestCovCSRFMiddlewareExemptsWebhook(t *testing.T) {
	csrf := NewCSRF("cov-secret", false)
	handler, calls := covCSRFProbe(csrf)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, covCSRFRequest(http.MethodPost, "/api/whatsapp/webhook", `{"event":"message"}`))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("webhook: status = %d, esperado 204 sem CSRF (a autenticação é a assinatura do WAHA)", recorder.Code)
	}
	if *calls != 1 {
		t.Fatalf("webhook não chegou ao handler (%d chamadas)", *calls)
	}
}

func TestCovCSRFMiddlewareTokenCannotComeFromAnotherSession(t *testing.T) {
	csrf := NewCSRF("cov-secret", false)
	other := NewCSRF("cov-outro-secret", false)
	handler, calls := covCSRFProbe(csrf)

	foreign := httptest.NewRecorder()
	foreignToken := other.Token(foreign, httptest.NewRequest(http.MethodGet, "/login", nil))

	request := covCSRFRequest(http.MethodPost, "/dashboard", url.Values{csrfFieldName: {foreignToken}}.Encode())
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: foreignToken})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("token de outra instância: status = %d, esperado 403", recorder.Code)
	}
	if *calls != 0 {
		t.Fatal("token de outra instância passou pelo middleware")
	}
}

func TestCovCSRFMiddlewareOnRouterRequiresTokenForMutations(t *testing.T) {
	panel := newCovPanel(t)

	form := url.Values{"intent": {"tenant.create"}, "name": {"Ana"}}
	request := httptest.NewRequest(http.MethodPost, "/dashboard", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(panel.sessionCookie(panel.admin.ID, true))

	recorder := panel.do(request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403 sem CSRF no roteador", recorder.Code)
	}

	accepted := panel.csrfPost("/dashboard", url.Values{"intent": {"intent.desconhecido"}})
	if accepted.Code != http.StatusBadRequest {
		t.Fatalf("status = %d com CSRF válido, esperado 400 do handler de destino: %s", accepted.Code, accepted.Body.String())
	}
}
