package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/config"
	"github.com/passoz/aluxen/internal/infrastructure/whatsapp"
)

// ── Dublês das rotas do WhatsApp ───────────────────────────────────
//
// Prefixo `wa` para não colidir com os dublês das outras áreas do mesmo pacote.

type waReceiptsRepo struct {
	domain.ReceiptRepository
	byID map[string]*domain.Receipt
}

func (r *waReceiptsRepo) ByID(_ context.Context, id string) (*domain.Receipt, error) {
	receipt, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return receipt, nil
}

type waLeasesRepo struct {
	domain.LeaseRepository
	byID map[string]*domain.Lease
}

func (r *waLeasesRepo) ByID(_ context.Context, id string) (*domain.Lease, error) {
	lease, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return lease, nil
}

type waTenantsRepo struct {
	domain.TenantRepository
	tenants []domain.Tenant
}

func (r *waTenantsRepo) List(context.Context) ([]domain.Tenant, error) { return r.tenants, nil }

func (r *waTenantsRepo) ByID(_ context.Context, id string) (*domain.Tenant, error) {
	for index := range r.tenants {
		if r.tenants[index].ID == id {
			return &r.tenants[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

// waMessagesStore guarda as mensagens recebidas pelo webhook.
type waMessagesStore struct {
	mu       sync.Mutex
	messages []domain.WhatsAppMessage
}

func (s *waMessagesStore) InsertIfAbsent(_ context.Context, message *domain.WhatsAppMessage) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stored := range s.messages {
		if domain.Str(stored.ExternalID) == domain.Str(message.ExternalID) && domain.Str(message.ExternalID) != "" {
			return 0, nil
		}
	}
	s.messages = append(s.messages, *message)
	return 1, nil
}

func (s *waMessagesStore) ByTenant(context.Context, string, int) ([]domain.WhatsAppMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.WhatsAppMessage(nil), s.messages...), nil
}

func (s *waMessagesStore) stored() []domain.WhatsAppMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.WhatsAppMessage(nil), s.messages...)
}

// waTestSigner emite tokens previsíveis (o HMAC real é testado no pacote authz).
type waTestSigner struct{}

func (waTestSigner) Generate(kind, id string) string { return fmt.Sprintf("token-%s-%s", kind, id) }

func (waTestSigner) FormatCode(token string) string { return "AUTH-" + token }

func (waTestSigner) ValidationURL(base, kind, id, token string) string {
	path := "recibo"
	if kind == application.WhatsAppKindLease {
		path = "contrato"
	}
	return fmt.Sprintf("%s/validar/%s/%s?token=%s", base, path, id, token)
}

// waFakeWaha é o WAHA de mentira: responde sessão, QR Code e envio, e guarda o
// que recebeu. Nenhum teste toca a rede real.
type waFakeWaha struct {
	mu       sync.Mutex
	server   *httptest.Server
	state    string
	requests []string
}

func newWAFakeWaha(t *testing.T) *waFakeWaha {
	t.Helper()
	fake := &waFakeWaha{state: "WORKING"}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		fake.requests = append(fake.requests, r.Method+" "+r.URL.Path)
		state := fake.state
		fake.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/sessions/aluxen":
			_, _ = fmt.Fprintf(w, `{"name":"aluxen","status":%q}`, state)
		case "/api/aluxen/auth/qr":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G'})
		case "/api/sendText":
			_, _ = w.Write([]byte(`{"id":{"_serialized":"false_5511999999999@c.us_ABC"}}`))
		default:
			_, _ = w.Write([]byte(`{"status":` + fmt.Sprintf("%q", state) + `}`))
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *waFakeWaha) setState(state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = state
}

func (f *waFakeWaha) called(method, path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, request := range f.requests {
		if request == method+" "+path {
			return true
		}
	}
	return false
}

// ── Montagem do servidor de teste ──────────────────────────────────

type waTestServer struct {
	handler  *Handlers
	audit    *fakeAudit
	messages *waMessagesStore
	waha     *waFakeWaha
	clock    *fakeClock
	csrf     string
	session  *http.Cookie
}

// newWATestServer monta o servidor com o canal injetado no Common, como o
// composition root faz. `configure` ajusta a configuração antes da montagem.
func newWATestServer(t *testing.T, configure func(cfg *config.Config), channel bool) *waTestServer {
	t.Helper()

	waha := newWAFakeWaha(t)
	userRepo := newFakeUsers(&domain.User{ID: "u1", Username: "passoz", PasswordHash: "hash:segredo"})
	auditRepo := &fakeAudit{}
	messages := &waMessagesStore{}
	clock := &fakeClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}

	paidAt := "2026-10-05T14:30"
	paidAmount := 1200.0
	leaseID := "l1"
	phone := "(11) 99999-9999"
	receipts := &waReceiptsRepo{byID: map[string]*domain.Receipt{
		"r1": {
			ID:           "r1",
			LeaseID:      &leaseID,
			Category:     domain.ReceiptCategoryRent,
			DueDate:      "2026-10-10",
			PaidAt:       &paidAt,
			Amount:       1200,
			PaidAmount:   &paidAmount,
			TenantName:   "Maria Silva",
			PropertyName: "Casa Centro",
		},
		"r2": {
			ID:           "r2",
			Category:     domain.ReceiptCategoryRent,
			DueDate:      "2026-11-10",
			Amount:       1200,
			TenantName:   "Maria Silva",
			PropertyName: "Casa Centro",
		},
	}}
	leases := &waLeasesRepo{byID: map[string]*domain.Lease{
		"l1": {
			ID: "l1", TenantID: "t1", PropertyID: "p1",
			StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 5, RentAmount: 1500,
			Status: domain.LeaseStatusActive, TenantName: "Maria Silva", PropertyName: "Casa Centro",
		},
	}}
	tenants := &waTenantsRepo{tenants: []domain.Tenant{{ID: "t1", Name: "Maria Silva", Phone: &phone}}}

	cfg := &config.Config{
		AppEnv:             "test",
		Host:               "127.0.0.1",
		SessionSecret:      "test-session-secret-with-enough-length",
		WahaAPIURL:         waha.server.URL,
		WahaAPIKey:         "chave",
		WahaSession:        "aluxen",
		WhatsAppMode:       "client",
		RateLimitMax:       5,
		RateLimitGlobalMax: 60,
		LoginLockThreshold: 10,
	}
	if configure != nil {
		configure(cfg)
	}

	logger := slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelError}))
	settings := newFakeSettings()
	settings.values[domain.SettingAppURL] = "https://aluxen.test"

	deps := application.Deps{
		Repos: application.Repositories{
			Users:       userRepo,
			Tenants:     tenants,
			Leases:      leases,
			Receipts:    receipts,
			Settings:    settings,
			Audit:       auditRepo,
			Inspections: &emptyInspections{},
			WhatsApp:    messages,
		},
		Clock:    clock,
		IDs:      &fakeIDs{},
		Hasher:   fakeHasher{},
		Signer:   waTestSigner{},
		Messages: whatsapp.Messages{},
		Log:      logger,
	}

	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	common := &Common{
		Cfg:       cfg,
		Log:       logger,
		Render:    renderer,
		Session:   NewSessionManager(cfg.SessionSecret, false),
		CSRF:      NewCSRF(cfg.SessionSecret, false),
		Deps:      deps,
		Started:   clock.now,
		Dialect:   "sqlite",
		LoginGate: application.NewLoginGate(clock, cfg.RateLimitMax, cfg.RateLimitGlobalMax, cfg.LoginLockThreshold),
		MustChangePasswordFunc: func(ctx context.Context, userID string) (bool, error) {
			return false, nil
		},
	}
	if channel {
		common.WhatsApp = whatsapp.NewRuntime(
			whatsapp.ClientConfig{BaseURL: cfg.WahaAPIURL, APIKey: cfg.WahaAPIKey, Session: cfg.WahaSession},
			clock,
			&fakeIDs{},
			logger,
			// Intervalos curtos: o pareamento precisa reagir rápido no teste.
			whatsapp.LoginConfig{PollInterval: 5 * time.Millisecond, Timeout: 2 * time.Second},
		)
	}

	server := &waTestServer{handler: NewHandlers(common), audit: auditRepo, messages: messages, waha: waha, clock: clock}

	// Sessão de administrador e token de CSRF, obtidos pelas próprias rotas.
	page := httptest.NewRecorder()
	server.handler.Router().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/login", nil))
	server.csrf = cookieValue(page.Result().Cookies(), csrfCookieName)
	if server.csrf == "" {
		t.Fatal("GET /login não emitiu cookie de CSRF")
	}

	sessionRecorder := httptest.NewRecorder()
	server.handler.Session.SetAdmin(sessionRecorder, "u1")
	server.session = sessionRecorder.Result().Cookies()[0]
	return server
}

// do executa a requisição com a sessão de administrador.
func (s *waTestServer) do(t *testing.T, method, path string, form url.Values, admin bool) *httptest.ResponseRecorder {
	t.Helper()

	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	request := httptest.NewRequest(method, path, body)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: s.csrf})
	if admin {
		request.AddCookie(s.session)
	}

	recorder := httptest.NewRecorder()
	s.handler.Router().ServeHTTP(recorder, request)
	return recorder
}

func (s *waTestServer) form(values map[string]string) url.Values {
	form := url.Values{csrfFieldName: {s.csrf}}
	for key, value := range values {
		form.Set(key, value)
	}
	return form
}

// signWebhook assina o corpo como o WAHA faz: HMAC-SHA256 em hex.
func signWebhook(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// webhookRequest monta a chamada do WAHA, sem sessão e sem CSRF.
func (s *waTestServer) webhookRequest(body []byte, signature string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/whatsapp/webhook", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	if signature != "" {
		request.Header.Set(whatsAppSignatureHeader, signature)
	}
	return request
}

func (s *waTestServer) doWebhook(body []byte, signature string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	s.handler.Router().ServeHTTP(recorder, s.webhookRequest(body, signature))
	return recorder
}

func whatsAppMessageBody(id, phone, text string) []byte {
	return []byte(fmt.Sprintf(`{
		"event": "message",
		"session": "aluxen",
		"payload": {
			"id": %q,
			"from": "%s@c.us",
			"body": %q,
			"timestamp": 1790000000,
			"fromMe": false,
			"hasMedia": false
		}
	}`, id, phone, text))
}

func decodeWhatsAppJSON(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("resposta não é JSON (%v): %s", err, recorder.Body.String())
	}
	return payload
}

// ── Testes ────────────────────────────────────────────────────────

func TestWhatsAppRoutesRequireAdminSession(t *testing.T) {
	server := newWATestServer(t, nil, true)

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/whatsapp/status"},
		{http.MethodPost, "/api/whatsapp/login/start"},
		{http.MethodGet, "/api/whatsapp/login/status"},
		{http.MethodPost, "/api/whatsapp/login/cancel"},
		{http.MethodPost, "/api/whatsapp/logout"},
		{http.MethodPost, "/api/whatsapp/preview"},
		{http.MethodPost, "/api/whatsapp/send"},
		{http.MethodGet, "/api/whatsapp/send/status?jobId=x"},
	}
	for _, route := range routes {
		recorder := server.do(t, route.method, route.path, server.form(nil), false)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: status = %d, esperado 401", route.method, route.path, recorder.Code)
		}
		if payload := decodeWhatsAppJSON(t, recorder); payload["error"] != "Não autorizado." {
			t.Fatalf("%s %s: corpo = %v, esperado o erro de não autorizado", route.method, route.path, payload)
		}
	}
}

func TestWhatsAppStatusReportsChannelState(t *testing.T) {
	server := newWATestServer(t, nil, true)

	recorder := server.do(t, http.MethodGet, "/api/whatsapp/status", nil, true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}
	payload := decodeWhatsAppJSON(t, recorder)
	if payload["isLoggedIn"] != true || payload["hasQRCode"] != false {
		t.Fatalf("payload = %v, esperado sessão conectada", payload)
	}
	if payload["chromiumAvailable"] != true {
		t.Fatalf("chromiumAvailable = %v, esperado true com WAHA configurado", payload["chromiumAvailable"])
	}
	if payload["profileDir"] != "" {
		t.Fatalf("profileDir = %v, esperado vazio sem perfil de navegador", payload["profileDir"])
	}

	server.waha.setState("SCAN_QR_CODE")
	payload = decodeWhatsAppJSON(t, server.do(t, http.MethodGet, "/api/whatsapp/status", nil, true))
	if payload["isLoggedIn"] != false || payload["hasQRCode"] != true {
		t.Fatalf("payload = %v, esperado pareamento em leitura de QR Code", payload)
	}
}

func TestWhatsAppStatusWithoutChannelIsUnavailable(t *testing.T) {
	server := newWATestServer(t, nil, false)

	recorder := server.do(t, http.MethodGet, "/api/whatsapp/status", nil, true)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, esperado 503", recorder.Code)
	}
	if payload := decodeWhatsAppJSON(t, recorder); payload["error"] != "Canal de WhatsApp indisponível." {
		t.Fatalf("corpo = %v", payload)
	}
}

func TestWhatsAppStatusWithoutWAHAConfigured(t *testing.T) {
	server := newWATestServer(t, func(cfg *config.Config) { cfg.WahaAPIURL = "" }, true)

	payload := decodeWhatsAppJSON(t, server.do(t, http.MethodGet, "/api/whatsapp/status", nil, true))
	if payload["chromiumAvailable"] != false || payload["isLoggedIn"] != false {
		t.Fatalf("payload = %v, esperado canal indisponível sem WAHA_API_URL", payload)
	}
}

func TestWhatsAppLoginFlowPublishesQRCodeAndCancels(t *testing.T) {
	server := newWATestServer(t, nil, true)
	server.waha.setState("SCAN_QR_CODE")

	started := server.do(t, http.MethodPost, "/api/whatsapp/login/start", server.form(nil), true)
	if started.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", started.Code)
	}
	payload := decodeWhatsAppJSON(t, started)
	state, _ := payload["state"].(map[string]any)
	if payload["ok"] != true || state["status"] != "starting" {
		t.Fatalf("payload = %v, esperado ok com estado starting", payload)
	}
	if !server.waha.called(http.MethodPost, "/api/sessions/aluxen/start") {
		t.Fatal("o start não chamou o WAHA")
	}

	// O acompanhamento é em segundo plano: o QR Code aparece na consulta seguinte.
	var scan map[string]any
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		payload = decodeWhatsAppJSON(t, server.do(t, http.MethodGet, "/api/whatsapp/login/status", nil, true))
		scan, _ = payload["state"].(map[string]any)
		if scan["status"] == "scan_qr" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if scan["status"] != "scan_qr" {
		t.Fatalf("estado = %v, esperado scan_qr", scan)
	}
	if qr, _ := scan["qrCode"].(string); !strings.HasPrefix(qr, "data:image/png;base64,") {
		t.Fatalf("qrCode = %q, esperado data URL PNG", scan["qrCode"])
	}
	if message, _ := scan["message"].(string); message != whatsapp.QRCodeMessage {
		t.Fatalf("message = %q, esperado o texto do app atual", scan["message"])
	}

	cancelled := decodeWhatsAppJSON(t, server.do(t, http.MethodPost, "/api/whatsapp/login/cancel", server.form(nil), true))
	cancelledState, _ := cancelled["state"].(map[string]any)
	if cancelledState["status"] != "idle" {
		t.Fatalf("estado após cancelar = %v, esperado idle", cancelledState)
	}
	if !server.waha.called(http.MethodPost, "/api/sessions/aluxen/stop") {
		t.Fatal("o cancelamento não parou a sessão no WAHA")
	}
}

func TestWhatsAppLogoutAuditsAndAnswersTheAppContract(t *testing.T) {
	server := newWATestServer(t, nil, true)

	recorder := server.do(t, http.MethodPost, "/api/whatsapp/logout", server.form(nil), true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}
	payload := decodeWhatsAppJSON(t, recorder)
	if payload["ok"] != true || payload["message"] != "WhatsApp desconectado com sucesso." {
		t.Fatalf("payload = %v", payload)
	}
	if !server.waha.called(http.MethodPost, "/api/sessions/aluxen/logout") {
		t.Fatal("o logout não chamou o WAHA")
	}
	if actions := server.audit.actions(); len(actions) != 1 || actions[0] != domain.ActionWhatsAppLogout {
		t.Fatalf("trilha = %v, esperado whatsapp.logout", actions)
	}
}

func TestWhatsAppPreviewReturnsTheAppPayload(t *testing.T) {
	server := newWATestServer(t, nil, true)

	recorder := server.do(t, http.MethodPost, "/api/whatsapp/preview", server.form(map[string]string{
		"type": "receipt",
		"id":   "r1",
	}), true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}

	payload := decodeWhatsAppJSON(t, recorder)
	if payload["ok"] != true || payload["type"] != "receipt" || payload["id"] != "r1" {
		t.Fatalf("payload = %v", payload)
	}
	if payload["phone"] != "(11) 99999-9999" || payload["formattedPhone"] != "5511999999999" {
		t.Fatalf("telefones = %v / %v", payload["phone"], payload["formattedPhone"])
	}
	if payload["tenantName"] != "Maria Silva" || payload["propertyName"] != "Casa Centro" {
		t.Fatalf("titular/imóvel = %v / %v", payload["tenantName"], payload["propertyName"])
	}

	message, _ := payload["message"].(string)
	if !strings.HasPrefix(message, "Olá, *Maria Silva*! Tudo bem?") {
		t.Fatalf("mensagem = %q", message)
	}
	if !strings.Contains(message, "📄 *Recibo de Aluguel*") ||
		!strings.Contains(message, "💰 *Valor*: R$ 1.200,00") ||
		!strings.Contains(message, "📅 *Vencimento*: 10/10/2026") ||
		!strings.Contains(message, "✅ *Status*: Quitado em 05/10/2026 14:30") {
		t.Fatalf("mensagem sem os campos do recibo:\n%s", message)
	}
	if !strings.Contains(message, "https://aluxen.test/pdf?id=r1&token=token-receipt-r1") ||
		!strings.Contains(message, "https://aluxen.test/validar/recibo/r1?token=token-receipt-r1") {
		t.Fatalf("mensagem sem os links do documento:\n%s", message)
	}

	directURL, _ := payload["directUrl"].(string)
	if !strings.HasPrefix(directURL, "https://wa.me/5511999999999?text=Ol%C3%A1%2C%20*Maria%20Silva*") {
		t.Fatalf("directUrl = %q", directURL)
	}
}

func TestWhatsAppPreviewLease(t *testing.T) {
	server := newWATestServer(t, nil, true)

	recorder := server.do(t, http.MethodPost, "/api/whatsapp/preview", server.form(map[string]string{
		"type": "lease",
		"id":   "l1",
	}), true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}

	payload := decodeWhatsAppJSON(t, recorder)
	message, _ := payload["message"].(string)
	if payload["type"] != "lease" || !strings.Contains(message, "🗓️ *Vigência*: 01/01/2026 a 31/12/2026") {
		t.Fatalf("payload = %v", payload)
	}
	if !strings.Contains(message, "https://aluxen.test/contract-pdf?id=l1&token=token-lease-l1") {
		t.Fatalf("mensagem sem o link do contrato:\n%s", message)
	}
}

func TestWhatsAppPreviewErrors(t *testing.T) {
	server := newWATestServer(t, nil, true)

	cases := []struct {
		name   string
		form   map[string]string
		status int
		error  string
	}{
		{
			name:   "tipo inválido",
			form:   map[string]string{"type": "contract", "id": "r1"},
			status: http.StatusBadRequest,
			error:  "Parâmetros inválidos.",
		},
		{
			name:   "sem id",
			form:   map[string]string{"type": "receipt"},
			status: http.StatusBadRequest,
			error:  "Parâmetros inválidos.",
		},
		{
			name:   "recibo não pago",
			form:   map[string]string{"type": "receipt", "id": "r2"},
			status: http.StatusBadRequest,
			error:  "Recibo ainda não pago.",
		},
		{
			name:   "recibo inexistente",
			form:   map[string]string{"type": "receipt", "id": "r9"},
			status: http.StatusNotFound,
			error:  "Registro não encontrado.",
		},
		{
			name:   "contrato inexistente",
			form:   map[string]string{"type": "lease", "id": "l9"},
			status: http.StatusNotFound,
			error:  "Registro não encontrado.",
		},
	}
	for _, testCase := range cases {
		recorder := server.do(t, http.MethodPost, "/api/whatsapp/preview", server.form(testCase.form), true)
		if recorder.Code != testCase.status {
			t.Fatalf("%s: status = %d, esperado %d (%s)", testCase.name, recorder.Code, testCase.status, recorder.Body.String())
		}
		if payload := decodeWhatsAppJSON(t, recorder); payload["error"] != testCase.error {
			t.Fatalf("%s: erro = %v, esperado %q", testCase.name, payload["error"], testCase.error)
		}
	}
}

func TestWhatsAppSendCreatesJobAndReportsStatus(t *testing.T) {
	server := newWATestServer(t, nil, true)

	recorder := server.do(t, http.MethodPost, "/api/whatsapp/send", server.form(map[string]string{
		"to":      "(11) 99999-9999",
		"message": "Segue o recibo",
	}), true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	jobID, _ := decodeWhatsAppJSON(t, recorder)["jobId"].(string)
	if jobID == "" {
		t.Fatal("resposta do envio sem jobId")
	}

	var job map[string]any
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		payload := decodeWhatsAppJSON(t, server.do(t, http.MethodGet, "/api/whatsapp/send/status?jobId="+jobID, nil, true))
		job, _ = payload["job"].(map[string]any)
		if job["status"] == "done" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if job["status"] != "done" || job["stage"] != "done" {
		t.Fatalf("job = %v, esperado concluído", job)
	}
	result, _ := job["result"].(map[string]any)
	if result["success"] != true || result["to"] != "5511999999999" || result["message"] != "Segue o recibo" {
		t.Fatalf("resultado = %v", result)
	}
	if job["userId"] != "u1" {
		t.Fatalf("userId = %v, esperado o administrador da sessão", job["userId"])
	}
	if actions := server.audit.actions(); len(actions) != 1 || actions[0] != domain.ActionWhatsAppSend {
		t.Fatalf("trilha = %v, esperado whatsapp.send", actions)
	}

	missing := server.do(t, http.MethodGet, "/api/whatsapp/send/status?jobId=nao-existe", nil, true)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("status de job desconhecido = %d, esperado 404", missing.Code)
	}
}

func TestWhatsAppSendValidatesRequest(t *testing.T) {
	server := newWATestServer(t, nil, true)

	cases := []struct {
		name string
		form map[string]string
		want string
	}{
		{
			name: "sem destinatário",
			form: map[string]string{"message": "oi"},
			want: "Destinatário e mensagem são obrigatórios.",
		},
		{
			name: "número inválido",
			form: map[string]string{"to": "1234", "message": "oi"},
			want: "Número de telefone \"1234\" inválido para envio via WhatsApp.",
		},
	}
	for _, testCase := range cases {
		recorder := server.do(t, http.MethodPost, "/api/whatsapp/send", server.form(testCase.form), true)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, esperado 400", testCase.name, recorder.Code)
		}
		if payload := decodeWhatsAppJSON(t, recorder); payload["error"] != testCase.want {
			t.Fatalf("%s: erro = %v, esperado %q", testCase.name, payload["error"], testCase.want)
		}
	}
}

func TestWhatsAppWebhookRequiresSignature(t *testing.T) {
	server := newWATestServer(t, nil, true)
	body := whatsAppMessageBody("m1", "5511999999999", "já paguei")

	recorder := server.doWebhook(body, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 sem assinatura", recorder.Code)
	}
	if stored := server.messages.stored(); len(stored) != 0 {
		t.Fatalf("mensagens gravadas = %d, esperado 0", len(stored))
	}

	wrong := server.doWebhook(body, signWebhook("outro-segredo", body))
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 com assinatura inválida", wrong.Code)
	}
}

func TestWhatsAppWebhookStoresSignedMessage(t *testing.T) {
	server := newWATestServer(t, func(cfg *config.Config) { cfg.WahaWebhookSecret = "s3gredo" }, true)
	body := whatsAppMessageBody("false_5511999999999@c.us_ABC", "5511999999999", "já paguei o aluguel")

	recorder := server.doWebhook(body, signWebhook("s3gredo", body))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}

	stored := server.messages.stored()
	if len(stored) != 1 {
		t.Fatalf("mensagens gravadas = %d, esperado 1", len(stored))
	}
	message := stored[0]
	if message.Direction != domain.WhatsAppDirectionIn || domain.Str(message.TenantID) != "t1" {
		t.Fatalf("mensagem = %+v, esperado recebida e vinculada ao locatário", message)
	}
	if message.Body != "já paguei o aluguel" || message.Phone != "5511999999999" {
		t.Fatalf("mensagem = %+v", message)
	}
	if actions := server.audit.actions(); len(actions) != 1 || actions[0] != domain.ActionWhatsAppMessageReceived {
		t.Fatalf("trilha = %v, esperado whatsapp.message-received", actions)
	}
}

func TestWhatsAppWebhookDemoWithoutSecretIsAccepted(t *testing.T) {
	server := newWATestServer(t, func(cfg *config.Config) {
		cfg.WhatsAppMode = "demo"
		cfg.WahaWebhookSecret = ""
	}, true)
	body := whatsAppMessageBody("m1", "5511999999999", "bom dia")

	recorder := server.doWebhook(body, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 no modo demo: %s", recorder.Code, recorder.Body.String())
	}
	if stored := server.messages.stored(); len(stored) != 1 {
		t.Fatalf("mensagens gravadas = %d, esperado 1", len(stored))
	}
}

func TestWhatsAppWebhookIgnoresOtherEvents(t *testing.T) {
	server := newWATestServer(t, func(cfg *config.Config) { cfg.WahaWebhookSecret = "s3gredo" }, true)
	body := []byte(`{"event":"session.status","session":"aluxen","payload":{"status":"WORKING"}}`)

	recorder := server.doWebhook(body, signWebhook("s3gredo", body))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}
	if payload := decodeWhatsAppJSON(t, recorder); payload["ignored"] != true {
		t.Fatalf("corpo = %v, esperado ignored", payload)
	}
	if stored := server.messages.stored(); len(stored) != 0 {
		t.Fatalf("mensagens gravadas = %d, esperado 0", len(stored))
	}
}

func TestWhatsAppWebhookRejectsMalformedBody(t *testing.T) {
	server := newWATestServer(t, func(cfg *config.Config) { cfg.WahaWebhookSecret = "s3gredo" }, true)
	body := []byte(`{"event":`)

	recorder := server.doWebhook(body, signWebhook("s3gredo", body))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", recorder.Code)
	}
}
