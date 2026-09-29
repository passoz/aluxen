package web

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
)

// covAttrs converte os atributos do registro de log em mapa.
func covAttrs(record slog.Record) map[string]any {
	attributes := map[string]any{}
	record.Attrs(func(attribute slog.Attr) bool {
		attributes[attribute.Key] = attribute.Value.Any()
		return true
	})
	return attributes
}

func TestCovClientIPResolution(t *testing.T) {
	cases := []struct {
		name      string
		forwarded string
		realIP    string
		want      string
	}{
		{name: "primeiro do X-Forwarded-For", forwarded: "203.0.113.9, 10.0.0.1, 10.0.0.2", want: "203.0.113.9"},
		{name: "X-Forwarded-For com espaços", forwarded: "  203.0.113.9  ", want: "203.0.113.9"},
		{name: "X-Real-IP quando não há forwarded", realIP: "198.51.100.7", want: "198.51.100.7"},
		{name: "forwarded tem precedência", forwarded: "203.0.113.9", realIP: "198.51.100.7", want: "203.0.113.9"},
		{name: "sem cabeçalhos", want: "desconhecido"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			if testCase.forwarded != "" {
				request.Header.Set("X-Forwarded-For", testCase.forwarded)
			}
			if testCase.realIP != "" {
				request.Header.Set("X-Real-IP", testCase.realIP)
			}
			if got := clientIP(request); got != testCase.want {
				t.Fatalf("clientIP = %q, esperado %q", got, testCase.want)
			}
		})
	}
}

func TestCovLooksLikeStatic(t *testing.T) {
	cases := map[string]bool{
		"/":                  false,
		"/dashboard":         false,
		"/favicon.svg":       true,
		"/styles/panel.css":  true,
		"/assets/assinatura": true,
		"/sw.js":             true,
		"/portal/../x.yaml":  true,
	}
	for path, want := range cases {
		if got := looksLikeStatic(path); got != want {
			t.Fatalf("looksLikeStatic(%q) = %v, esperado %v", path, got, want)
		}
	}
}

func TestCovGateExempt(t *testing.T) {
	cases := map[string]bool{
		"/login":                true,
		"/logout":               true,
		"/settings":             true,
		"/api/health":           true,
		"/portal":               true,
		"/portal/password":      true,
		"/validar/recibo/abc":   true,
		"/favicon.svg":          true,
		"/dashboard":            false,
		"/api/info":             false,
		"/api/tenants":          false,
		"/dashboard/inspection": false,
	}
	for path, want := range cases {
		if got := gateExempt(path); got != want {
			t.Fatalf("gateExempt(%q) = %v, esperado %v", path, got, want)
		}
	}
}

func TestCovWantsJSON(t *testing.T) {
	cases := []struct {
		name   string
		target string
		accept string
		want   bool
	}{
		{name: "query json=true", target: "/dashboard?json=true", want: true},
		{name: "Accept application/json", target: "/dashboard", accept: "application/json", want: true},
		{name: "Accept com peso", target: "/dashboard", accept: "text/html, application/json;q=0.9", want: true},
		{name: "Accept html", target: "/dashboard", accept: "text/html", want: false},
		{name: "sem nada", target: "/dashboard", want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, testCase.target, nil)
			if testCase.accept != "" {
				request.Header.Set("Accept", testCase.accept)
			}
			if got := wantsJSON(request); got != testCase.want {
				t.Fatalf("wantsJSON = %v, esperado %v", got, testCase.want)
			}
		})
	}
}

func TestCovSecurityHeadersOnEveryResponse(t *testing.T) {
	panel := newCovPanel(t)

	recorder := panel.do(httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}
	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "same-origin",
	}
	for header, value := range want {
		if got := recorder.Header().Get(header); got != value {
			t.Fatalf("%s = %q, esperado %q", header, got, value)
		}
	}

	notFound := panel.do(httptest.NewRequest(http.MethodGet, "/rota-inexistente", nil))
	if got := notFound.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("resposta 404 sem cabeçalho de segurança: %q", got)
	}
}

func TestCovForceHTTPSRedirectsInProduction(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.Cfg.AppEnv = "production"

	request := httptest.NewRequest(http.MethodGet, "http://aluxen.example/api/health", nil)
	request.Host = "aluxen.example"
	recorder := panel.do(request)

	if recorder.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, esperado 301 em produção sem TLS", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "https://aluxen.example/api/health" {
		t.Fatalf("Location = %q", location)
	}
}

func TestCovForceHTTPSHonoursForwardedProto(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.Cfg.AppEnv = "production"

	request := httptest.NewRequest(http.MethodGet, "http://aluxen.example/api/health", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	if recorder := panel.do(request); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d com X-Forwarded-Proto=https, esperado 200", recorder.Code)
	}

	other := httptest.NewRequest(http.MethodGet, "http://aluxen.example/api/health", nil)
	other.Header.Set("X-Forwarded-Proto", "http")
	if recorder := panel.do(other); recorder.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d com X-Forwarded-Proto=http, esperado 301", recorder.Code)
	}
}

func TestCovForceHTTPSIgnoresRequestWithTLS(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.Cfg.AppEnv = "production"

	request := httptest.NewRequest(http.MethodGet, "https://aluxen.example/api/health", nil)
	request.TLS = &tls.ConnectionState{}
	if recorder := panel.do(request); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d com TLS, esperado 200", recorder.Code)
	}
}

func TestCovForceHTTPSOnlyAppliesInProduction(t *testing.T) {
	panel := newCovPanel(t)

	request := httptest.NewRequest(http.MethodGet, "http://aluxen.example/api/health", nil)
	request.Host = "aluxen.example"
	if recorder := panel.do(request); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d fora de produção, esperado 200", recorder.Code)
	}
}

// As sondas de saúde (k8s liveness/readiness) chegam por HTTP simples, direto no
// pod: sem a isenção o redirect para HTTPS deixaria o pod unhealthy para sempre.
func TestCovProbePathsAreExemptFromTheHTTPSRedirect(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.Cfg.AppEnv = "production"

	for _, path := range []string{"/healthz", "/readyz"} {
		request := httptest.NewRequest(http.MethodGet, "http://aluxen.example"+path, nil)
		if recorder := panel.do(request); recorder.Code != http.StatusOK {
			t.Fatalf("%s: status = %d sem X-Forwarded-Proto, esperado 200", path, recorder.Code)
		}
	}

	// fora da lista de sondas a trava continua valendo
	request := httptest.NewRequest(http.MethodGet, "http://aluxen.example/api/health", nil)
	request.Host = "aluxen.example"
	if recorder := panel.do(request); recorder.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d em /api/health, esperado 301", recorder.Code)
	}
}

func TestCovStaticFilesAreServedBeforeTheHTTPSLockAndPasswordGate(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.Cfg.AppEnv = "production"

	for _, testCase := range []struct{ path, contentType string }{
		{"/favicon.svg", "image/svg+xml"},
		{"/styles/panel.css", "text/css; charset=utf-8"},
		{"/manifest.webmanifest", "application/manifest+json"},
	} {
		recorder := panel.do(httptest.NewRequest(http.MethodGet, "http://aluxen.example"+testCase.path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, esperado 200 (estático servido antes da trava)", testCase.path, recorder.Code)
		}
		if got := recorder.Header().Get("Content-Type"); got != testCase.contentType {
			t.Fatalf("%s: Content-Type = %q, esperado %q", testCase.path, got, testCase.contentType)
		}
	}
}

func TestCovServeStaticIgnoresNonStaticRequestsAndMutations(t *testing.T) {
	panel := newCovPanel(t)

	missing := panel.do(httptest.NewRequest(http.MethodGet, "/nao-existe.js", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("estático inexistente: status = %d, esperado 404", missing.Code)
	}

	mutation := panel.csrfPost("/favicon.svg", nil)
	if mutation.Code != http.StatusNotFound {
		t.Fatalf("POST em estático: status = %d, esperado 404 (não é servido)", mutation.Code)
	}

	page := panel.do(httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusFound {
		t.Fatalf("raiz: status = %d, esperado 302 para o login", page.Code)
	}
}

func TestCovRecoverPanicAnswersInternalErrorAndLogs(t *testing.T) {
	panel := newCovPanel(t)

	panicking := panel.handler.recoverPanic(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("cov: pânico proposital")
	}))

	recorder := httptest.NewRecorder()
	panicking.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500", recorder.Code)
	}
	if body := recorder.Body.String(); body == "" || body == "cov: pânico proposital" {
		t.Fatalf("corpo = %q, esperado mensagem interna sem vazar o pânico", body)
	}
	record, ok := panel.logs.last()
	if !ok || record.Level != slog.LevelError {
		t.Fatalf("pânico não foi registrado como erro: %+v", record)
	}
	if path, _ := covAttrs(record)["path"].(string); path != "/dashboard" {
		t.Fatalf("log do pânico sem o path da requisição: %v", covAttrs(record))
	}
}

func TestCovRequestLoggerLevelsByStatusAndIdentifiesRequest(t *testing.T) {
	panel := newCovPanel(t)

	panel.do(httptest.NewRequest(http.MethodGet, "/healthz", nil))
	record, ok := panel.logs.last()
	if !ok || record.Level != slog.LevelInfo {
		t.Fatalf("200 registrado em nível %v, esperado Info", record.Level)
	}
	attributes := covAttrs(record)
	if status, _ := attributes["status"].(int64); status != http.StatusOK {
		t.Fatalf("status registrado = %v", attributes["status"])
	}
	requestID, _ := attributes["request_id"].(string)
	if len(requestID) != 8 {
		t.Fatalf("request_id = %q, esperado 8 caracteres hexadecimais", requestID)
	}
	if attributes["path"] != "/healthz" || attributes["method"] != http.MethodGet {
		t.Fatalf("log sem método/path da requisição: %v", attributes)
	}
	if attributes["ip"] != "desconhecido" {
		t.Fatalf("ip = %v, esperado desconhecido sem cabeçalhos de proxy", attributes["ip"])
	}

	panel.do(httptest.NewRequest(http.MethodGet, "/rota-inexistente", nil))
	record, _ = panel.logs.last()
	if record.Level != slog.LevelWarn {
		t.Fatalf("404 registrado em nível %v, esperado Warn", record.Level)
	}

	panel.tenants.err = errCovBoom
	panel.as(http.MethodGet, "/api/info", nil, panel.admin.ID)
	record, _ = panel.logs.last()
	if record.Level != slog.LevelError {
		t.Fatalf("500 registrado em nível %v, esperado Error", record.Level)
	}
}

func TestCovRequestLoggerAddsAuthenticatedUserID(t *testing.T) {
	panel := newCovPanel(t)

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.AddCookie(panel.sessionCookie(panel.admin.ID, true))
	request.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	panel.do(request)

	record, ok := panel.logs.last()
	if !ok {
		t.Fatal("requisição não registrada")
	}
	attributes := covAttrs(record)
	if attributes["user_id"] != panel.admin.ID {
		t.Fatalf("user_id = %v, esperado %q", attributes["user_id"], panel.admin.ID)
	}
	if attributes["ip"] != "203.0.113.9" {
		t.Fatalf("ip = %v, esperado o primeiro do X-Forwarded-For", attributes["ip"])
	}
}

func TestCovPasswordGateBlocksSeededAdmin(t *testing.T) {
	panel := newCovPanel(t)
	panel.mustChange = func(context.Context, string) (bool, error) { return true, nil }

	redirection := panel.as(http.MethodGet, "/dashboard", nil, panel.admin.ID)
	if redirection.Code != http.StatusFound {
		t.Fatalf("status = %d, esperado 302", redirection.Code)
	}
	if location := redirection.Header().Get("Location"); location != "/settings?forced=1" {
		t.Fatalf("Location = %q, esperado /settings?forced=1", location)
	}

	api := panel.as(http.MethodGet, "/api/info", nil, panel.admin.ID)
	if api.Code != http.StatusForbidden {
		t.Fatalf("/api/info: status = %d, esperado 403", api.Code)
	}
	if payload := covDecodeJSON(t, api); payload["error"] != "Troca de senha obrigatória." {
		t.Fatalf("payload = %v", payload)
	}

	jsonQuery := panel.as(http.MethodGet, "/dashboard?json=true", nil, panel.admin.ID)
	if jsonQuery.Code != http.StatusForbidden {
		t.Fatalf("?json=true: status = %d, esperado 403", jsonQuery.Code)
	}

	exempt := panel.as(http.MethodGet, "/settings", nil, panel.admin.ID)
	if exempt.Code != http.StatusOK {
		t.Fatalf("/settings: status = %d, esperado 200 (rota isenta)", exempt.Code)
	}

	portal := panel.as(http.MethodGet, "/portal/login", nil, "")
	if portal.Code != http.StatusOK {
		t.Fatalf("/portal/login: status = %d, esperado 200", portal.Code)
	}
}

func TestCovPasswordGateLetsOrdinaryAdminThrough(t *testing.T) {
	panel := newCovPanel(t)
	panel.mustChange = func(context.Context, string) (bool, error) { return false, nil }

	if recorder := panel.as(http.MethodGet, "/api/info", nil, panel.admin.ID); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}
}

func TestCovPasswordGateFailsOpenWhenCheckErrors(t *testing.T) {
	panel := newCovPanel(t)
	panel.mustChange = func(context.Context, string) (bool, error) { return false, errCovBoom }

	if recorder := panel.as(http.MethodGet, "/api/info", nil, panel.admin.ID); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 quando a verificação falha", recorder.Code)
	}
}

func TestCovPasswordGateIgnoresAnonymousRequests(t *testing.T) {
	panel := newCovPanel(t)
	panel.mustChange = func(context.Context, string) (bool, error) { return true, nil }

	if recorder := panel.as(http.MethodGet, "/healthz", nil, ""); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 sem sessão", recorder.Code)
	}
}

func TestCovUnauthorizedRoutesAnonymousUser(t *testing.T) {
	panel := newCovPanel(t)

	page := panel.as(http.MethodGet, "/dashboard", nil, "")
	if page.Code != http.StatusFound || page.Header().Get("Location") != "/login" {
		t.Fatalf("página: status = %d location = %q, esperado 302 /login", page.Code, page.Header().Get("Location"))
	}

	api := panel.as(http.MethodGet, "/api/info", nil, "")
	if api.Code != http.StatusUnauthorized {
		t.Fatalf("API: status = %d, esperado 401", api.Code)
	}
	if payload := covDecodeJSON(t, api); payload["error"] != "Não autorizado." {
		t.Fatalf("payload = %v", payload)
	}
}

func TestCovRequireAdminDropsStaleSession(t *testing.T) {
	panel := newCovPanel(t)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	request.AddCookie(panel.sessionCookie("usuario-removido", true))
	panel.handler.Router().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 para sessão de usuário inexistente", recorder.Code)
	}
	cleared := false
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == adminCookieName && cookie.MaxAge == -1 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("sessão obsoleta não foi limpa na resposta")
	}
}

func TestCovRequireAdminFailsWhenTheUserLookupBreaks(t *testing.T) {
	panel := newCovPanel(t)
	panel.users.err = errCovBoom

	if recorder := panel.as(http.MethodGet, "/api/info", nil, panel.admin.ID); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 quando a leitura do usuário falha", recorder.Code)
	}
}

func TestCovWriteJSONReportsUnserialisablePayload(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeJSON(recorder, http.StatusOK, map[string]any{"valor": func() {}})

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType == "application/json; charset=utf-8" {
		t.Fatalf("resposta de erro manteve Content-Type JSON: %q", contentType)
	}
}

func TestCovWriteJSONWithHeadersSetsBoth(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeJSONWith(recorder, http.StatusCreated, map[string]string{"ok": "true"}, map[string]string{"Retry-After": "7"})

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, esperado 201", recorder.Code)
	}
	if recorder.Header().Get("Retry-After") != "7" {
		t.Fatalf("cabeçalho extra não aplicado: %v", recorder.Header())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
}

func TestCovNewRequestIDIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 32 {
		id := newRequestID()
		if len(id) != 8 {
			t.Fatalf("request id = %q, esperado 8 caracteres", id)
		}
		if seen[id] {
			t.Fatalf("request id repetido: %q", id)
		}
		seen[id] = true
	}
}

func TestCovTruncateKeepsLimitBoundary(t *testing.T) {
	short := "aluxen"
	if got := truncate(short, 300); got != short {
		t.Fatalf("truncate = %q, esperado valor intacto", got)
	}
	exact := string(make([]byte, 300))
	if got := truncate(exact, 300); len(got) != 300 {
		t.Fatalf("truncate no limite devolveu %d caracteres", len(got))
	}
	long := string(make([]byte, 301))
	if got := truncate(long, 300); len(got) != 300 {
		t.Fatalf("truncate acima do limite devolveu %d caracteres, esperado 300", len(got))
	}
}

func TestCovAuditContextCarriesRequestData(t *testing.T) {
	panel := newCovPanel(t)

	request := httptest.NewRequest(http.MethodPost, "/dashboard", nil)
	request.Header.Set("User-Agent", string(make([]byte, 400)))
	request.Header.Set("X-Real-IP", "198.51.100.7")
	request.AddCookie(panel.sessionCookie(panel.admin.ID, true))

	actx := panel.handler.auditContext(request, domain.AuditOriginWeb)
	if actx.Origin != domain.AuditOriginWeb {
		t.Fatalf("origem = %q", actx.Origin)
	}
	if actx.IP != "198.51.100.7" {
		t.Fatalf("IP = %q", actx.IP)
	}
	if len(actx.UserAgent) != 300 {
		t.Fatalf("user agent truncado em %d caracteres, esperado 300", len(actx.UserAgent))
	}
	if actx.ActorID == nil || *actx.ActorID != panel.admin.ID {
		t.Fatalf("actor = %v", actx.ActorID)
	}
	if actx.ActorName == nil || *actx.ActorName != panel.admin.Username {
		t.Fatalf("actor name = %v", actx.ActorName)
	}
}

func TestCovAuditContextWithoutSession(t *testing.T) {
	panel := newCovPanel(t)

	actx := panel.handler.auditContext(httptest.NewRequest(http.MethodPost, "/portal", nil), "portal")
	if actx.ActorID != nil {
		t.Fatalf("actor = %v, esperado nulo sem sessão", actx.ActorID)
	}
	if actx.IP != "desconhecido" {
		t.Fatalf("IP = %q, esperado desconhecido", actx.IP)
	}
}

func TestCovTimezoneNameIsInformative(t *testing.T) {
	if name := timezoneName(); name == "" {
		t.Fatal("timezoneName vazio")
	}
}

func TestCovMustChangePasswordWithoutConfiguredHook(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.MustChangePasswordFunc = nil

	must, err := panel.handler.MustChangePassword(context.Background(), panel.admin.ID)
	if err != nil || must {
		t.Fatalf("MustChangePassword = (%v, %v), esperado (false, nil) sem hook", must, err)
	}
}

func TestCovPanicRecoveryKeepsServerAliveForNextRequest(t *testing.T) {
	panel := newCovPanel(t)

	panicking := panel.handler.recoverPanic(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(errors.New("cov: pânico"))
	}))
	recorder := httptest.NewRecorder()
	panicking.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/x", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500", recorder.Code)
	}

	if recorder := panel.do(httptest.NewRequest(http.MethodGet, "/healthz", nil)); recorder.Code != http.StatusOK {
		t.Fatalf("servidor não respondeu depois do pânico: %d", recorder.Code)
	}
}
