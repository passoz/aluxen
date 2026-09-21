package web

import (
	"context"
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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/config"
	"github.com/passoz/aluxen/internal/infrastructure/db"
	"github.com/passoz/aluxen/internal/infrastructure/whatsapp"
)

// Conversas e régua de cobrança são exercitadas pelo roteador completo (sessão,
// CSRF, render da aba) sobre o SQLite em memória, com um servidor falso no lugar
// do WAHA — o canal da instância fala HTTP com ele como falaria com o WAHA real.

const (
	agentUserID    = "user-1"
	agentTenantID  = "tenant-1"
	agentReceiptID = "receipt-1"
	agentPhone     = "(11) 99999-9999"
)

// agentClock, agentIDs e agentHasher vêm de handlers_receipts_test.go (o mesmo
// pacote já os define para o painel); aqui só o canal é novo.

// ── Servidor falso do WAHA ────────────────────────────────────────

type agentWahaMessage struct {
	chatID string
	text   string
}

type agentWaha struct {
	mu     sync.Mutex
	sent   []agentWahaMessage
	fail   bool
	server *httptest.Server
}

func newAgentWaha(t *testing.T) *agentWaha {
	t.Helper()
	waha := &agentWaha{}
	waha.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/sendText" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		waha.mu.Lock()
		failing := waha.fail
		if !failing {
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				waha.mu.Unlock()
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			waha.sent = append(waha.sent, agentWahaMessage{chatID: payload["chatId"], text: payload["text"]})
		}
		waha.mu.Unlock()

		if failing {
			http.Error(w, "sessão indisponível", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"wamid-1"}`))
	}))
	t.Cleanup(waha.server.Close)
	return waha
}

func (w *agentWaha) sentMessages() []agentWahaMessage {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]agentWahaMessage(nil), w.sent...)
}

// ── Painel do teste ───────────────────────────────────────────────

type agentPanel struct {
	handler   *Handlers
	store     *db.Store
	waha      *agentWaha
	now       time.Time
	session   *http.Cookie
	csrf      *http.Cookie
	csrfToken string
}

func newAgentPanel(t *testing.T) *agentPanel {
	t.Helper()
	ctx := context.Background()

	store, err := db.Open(ctx, ":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	clock := rcptClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)}
	log := agentLogger(t)
	waha := newAgentWaha(t)
	runtime := whatsapp.NewRuntime(
		whatsapp.ClientConfig{BaseURL: waha.server.URL, HTTPClient: waha.server.Client()},
		clock, &rcptIDs{}, log, whatsapp.LoginConfig{},
	)

	deps := application.Deps{
		Repos: application.Repositories{
			Users:       store.Users,
			Tenants:     store.Tenants,
			Properties:  store.Properties,
			Leases:      store.Leases,
			Receipts:    store.Receipts,
			Settings:    store.Settings,
			Audit:       store.Audit,
			WhatsApp:    store.WhatsApp,
			Inspections: store.Inspections,
		},
		Clock:  clock,
		IDs:    &rcptIDs{},
		Hasher: rcptHasher{},
		// A régua monta o texto dela: a superfície do canal só entra na resposta
		// manual, que reaproveita o envio avulso.
		Messages: whatsapp.Messages{},
		Log:      log,
	}

	renderer, err := NewRenderer()
	require.NoError(t, err)

	panel := &agentPanel{
		handler: NewHandlers(&Common{
			Cfg: &config.Config{
				AppEnv:        "test",
				Host:          "127.0.0.1",
				Port:          0,
				SessionSecret: "test-session-secret-with-enough-length",
				AppURL:        "http://localhost:3000",
			},
			Log:      log,
			Render:   renderer,
			Session:  NewSessionManager("test-session-secret-with-enough-length", false),
			CSRF:     NewCSRF("test-session-secret-with-enough-length", false),
			Deps:     deps,
			Started:  clock.now,
			Dialect:  "sqlite",
			WhatsApp: runtime,
			// O portão do login é do painel: sem ele o formulário de entrada
			// responderia 500 (o handler usa o portão para o rate limit).
			LoginGate: application.NewLoginGate(clock, 10, 100, 5),
		}),
		store: store,
		waha:  waha,
		now:   clock.now,
	}

	require.NoError(t, store.Users.Create(ctx, &domain.User{
		ID: agentUserID, Username: "passoz", PasswordHash: "hash:segredo",
		PasswordChangedAt: domain.StrPtr("2026-09-01 10:00:00"), CreatedAt: "2026-09-01 10:00:00",
	}))
	require.NoError(t, store.Tenants.Create(ctx, &domain.Tenant{
		ID: agentTenantID, Name: "Maria Souza", Email: "maria@example.com",
		Phone: domain.StrPtr(agentPhone), CreatedAt: "2026-09-01 10:00:00", UpdatedAt: "2026-09-01 10:00:00",
	}))
	require.NoError(t, store.Properties.Create(ctx, &domain.Property{
		ID: "property-1", Name: "Apto 12", Address: "Rua A, 100", City: "São Paulo", State: "SP",
		CreatedAt: "2026-09-01 10:00:00", UpdatedAt: "2026-09-01 10:00:00",
	}))
	require.NoError(t, store.Leases.Create(ctx, &domain.Lease{
		ID: "lease-1", TenantID: agentTenantID, PropertyID: "property-1",
		StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 2000,
		Status: domain.LeaseStatusActive, CreatedAt: "2026-09-01 10:00:00", UpdatedAt: "2026-09-01 10:00:00",
	}))
	require.NoError(t, store.Receipts.Create(ctx, &domain.Receipt{
		ID: agentReceiptID, LeaseID: domain.StrPtr("lease-1"), TenantID: domain.StrPtr(agentTenantID),
		PropertyID: domain.StrPtr("property-1"), Category: domain.ReceiptCategoryRent,
		DueDate: "2026-09-01", Amount: 2000, CreatedAt: "2026-09-01 10:00:00",
	}))
	require.NoError(t, store.Receipts.Create(ctx, &domain.Receipt{
		ID: "receipt-paid", LeaseID: domain.StrPtr("lease-1"), TenantID: domain.StrPtr(agentTenantID),
		PropertyID: domain.StrPtr("property-1"), Category: domain.ReceiptCategoryRent,
		DueDate: "2026-08-01", Amount: 2000, CreatedAt: "2026-08-01 10:00:00",
		PaidAt: domain.StrPtr("2026-08-05T10:00"), PaidAmount: domain.FloatPtr(2000),
	}))
	_, err = store.WhatsApp.InsertIfAbsent(ctx, &domain.WhatsAppMessage{
		ID: "message-1", CreatedAt: "2026-09-20 10:00:00", Direction: domain.WhatsAppDirectionIn,
		ChatID: "5511999999999@c.us", Phone: "5511999999999", TenantID: domain.StrPtr(agentTenantID),
		Body: "Já paguei o aluguel, viu?", Status: domain.WhatsAppStatusReceived,
	})
	require.NoError(t, err)

	sessionRecorder := httptest.NewRecorder()
	panel.handler.Session.SetAdmin(sessionRecorder, agentUserID)
	panel.session = sessionRecorder.Result().Cookies()[0]

	csrfRecorder := httptest.NewRecorder()
	panel.csrfToken = panel.handler.CSRF.Token(csrfRecorder, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	for _, cookie := range csrfRecorder.Result().Cookies() {
		if cookie.Name == csrfCookieName {
			panel.csrf = cookie
		}
	}
	require.NotNil(t, panel.csrf, "cookie de CSRF não emitido")
	return panel
}

// agentLogger evita poluir a saída do teste com o log do painel.
func agentLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelError}))
}

func (p *agentPanel) do(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	p.handler.Router().ServeHTTP(recorder, request)
	return recorder
}

func (p *agentPanel) get(target string, authenticated bool) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	if authenticated {
		request.AddCookie(p.session)
		request.AddCookie(p.csrf)
	}
	return p.do(request)
}

// post dispara um formulário do painel, com CSRF e sessão válidos.
func (p *agentPanel) post(target, intent string, fields map[string]string) *httptest.ResponseRecorder {
	form := url.Values{csrfFieldName: {p.csrfToken}}
	if intent != "" {
		form.Set("intent", intent)
	}
	for key, value := range fields {
		form.Set(key, value)
	}
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.AddCookie(p.session)
	request.AddCookie(p.csrf)
	return p.do(request)
}

func (p *agentPanel) postJSON(target, body string) *httptest.ResponseRecorder {
	// O token vai na query: o corpo é JSON e o CSRF lê o campo do formulário.
	target = target + "?" + csrfFieldName + "=" + url.QueryEscape(p.csrfToken)
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.AddCookie(p.session)
	request.AddCookie(p.csrf)
	return p.do(request)
}

func (p *agentPanel) auditFor(t *testing.T, action string) []domain.AuditLog {
	t.Helper()
	entries, err := p.store.Audit.List(context.Background(), domain.AuditFilter{Action: action, Limit: 100})
	require.NoError(t, err)
	return entries
}

// waitAudit espera a linha da trilha que o envio em segundo plano grava.
func (p *agentPanel) waitAudit(t *testing.T, action string) domain.AuditLog {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if entries := p.auditFor(t, action); len(entries) > 0 {
			return entries[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("evento %q não registrado na trilha", action)
	return domain.AuditLog{}
}

func requireAgentJSON(t *testing.T, recorder *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	require.Equal(t, status, recorder.Code, recorder.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	return body
}

// ── Conversas ─────────────────────────────────────────────────────

func TestConversationListReturnsConversations(t *testing.T) {
	panel := newAgentPanel(t)

	body := requireAgentJSON(t, panel.get("/api/conversations", true), http.StatusOK)
	assert.Equal(t, true, body["ok"])

	conversations, ok := body["conversations"].([]any)
	require.True(t, ok)
	require.Len(t, conversations, 1)

	row, ok := conversations[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, agentTenantID, row["tenantId"])
	assert.Equal(t, "Maria Souza", row["tenantName"])
	assert.Equal(t, "Já paguei o aluguel, viu?", row["lastMessage"])
	assert.Equal(t, true, row["awaitingReply"])

	overdue, ok := row["overdue"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, agentReceiptID, overdue["receiptId"])
	assert.Equal(t, float64(20), overdue["daysLate"])
	assert.Equal(t, "Proposta de parcelamento", overdue["stageLabel"])
	assert.Equal(t, float64(3), overdue["stage"])

	// O lançamento pago não entra na conta do atraso.
	assert.Equal(t, float64(1), row["overdueCount"])
}

func TestConversationListRequiresAdmin(t *testing.T) {
	panel := newAgentPanel(t)

	requireAgentJSON(t, panel.get("/api/conversations", false), http.StatusUnauthorized)
}

func TestConversationListRejectsInvalidLimit(t *testing.T) {
	panel := newAgentPanel(t)

	body := requireAgentJSON(t, panel.get("/api/conversations?limit=muitas", true), http.StatusBadRequest)
	assert.Equal(t, "Limite inválido.", body["error"])
}

// ── Atendimento manual ────────────────────────────────────────────

func TestConversationReplySendsThroughTheChannel(t *testing.T) {
	panel := newAgentPanel(t)

	body := requireAgentJSON(t, panel.post("/dashboard/conversation", "conversation.reply", map[string]string{
		"tenant_id": agentTenantID,
		"message":   "Pode pagar no dia 10, sem problema.",
	}), http.StatusOK)
	require.Equal(t, true, body["ok"])
	require.NotEmpty(t, body["jobId"])

	// O envio avulso do canal registra o telefone já normalizado para o WhatsApp.
	entry := panel.waitAudit(t, domain.ActionWhatsAppSend)
	assert.Equal(t, "5511999999999", domain.Str(entry.EntityLabel))

	sent := panel.waha.sentMessages()
	require.Len(t, sent, 1)
	assert.Equal(t, "5511999999999@c.us", sent[0].chatID)
	assert.Equal(t, "Pode pagar no dia 10, sem problema.", sent[0].text)
}

func TestConversationReplyValidatesDestination(t *testing.T) {
	panel := newAgentPanel(t)
	ctx := context.Background()
	require.NoError(t, panel.store.Tenants.Update(ctx, &domain.Tenant{
		ID: agentTenantID, Name: "Maria Souza", Email: "maria@example.com",
		CreatedAt: "2026-09-01 10:00:00", UpdatedAt: "2026-09-01 10:00:00",
	}))

	body := requireAgentJSON(t, panel.post("/dashboard/conversation", "conversation.reply", map[string]string{
		"tenant_id": agentTenantID,
		"message":   "Oi",
	}), http.StatusBadRequest)
	assert.Contains(t, body["error"], "telefone")

	body = requireAgentJSON(t, panel.post("/dashboard/conversation", "conversation.reply", map[string]string{
		"tenant_id": "tenant-9",
		"message":   "Oi",
	}), http.StatusNotFound)
	assert.Equal(t, "Registro não encontrado.", body["error"])

	assert.Empty(t, panel.waha.sentMessages())
}

func TestConversationEscalateRegistersTheEvent(t *testing.T) {
	panel := newAgentPanel(t)

	body := requireAgentJSON(t, panel.post("/dashboard/conversation", "conversation.escalate", map[string]string{
		"tenant_id": agentTenantID,
		"reason":    "Assumida pelo painel",
	}), http.StatusOK)
	assert.Equal(t, true, body["ok"])

	entries := panel.auditFor(t, domain.ActionWhatsAppSend)
	require.Len(t, entries, 1)
	assert.Equal(t, "Maria Souza", domain.Str(entries[0].EntityLabel))
	assert.Equal(t, "passoz", domain.Str(entries[0].ActorName))
	assert.Contains(t, domain.Str(entries[0].Metadata), "manual-escalation")
	assert.Contains(t, entries[0].Summary, "assumida pelo locador")
	assert.Empty(t, panel.waha.sentMessages(), "escalonar não é enviar")
}

func TestConversationMutationRequiresAdmin(t *testing.T) {
	panel := newAgentPanel(t)

	// Sem a sessão do administrador, com o CSRF do formulário.
	form := url.Values{
		"intent":      {conversationIntentReply},
		"tenant_id":   {agentTenantID},
		"message":     {"Oi"},
		csrfFieldName: {panel.csrfToken},
	}
	request := httptest.NewRequest(http.MethodPost, "/dashboard/conversation", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.AddCookie(panel.csrf)
	requireAgentJSON(t, panel.do(request), http.StatusUnauthorized)

	assert.Empty(t, panel.waha.sentMessages())
	assert.Empty(t, panel.auditFor(t, domain.ActionWhatsAppSend))
}

func TestConversationMutationRejectsUnknownIntent(t *testing.T) {
	panel := newAgentPanel(t)

	body := requireAgentJSON(t, panel.post("/dashboard/conversation", "conversation.apagar", nil), http.StatusBadRequest)
	assert.Equal(t, "Ação inválida.", body["error"])
}

// ── Régua ─────────────────────────────────────────────────────────

func TestCollectionRunDryRunDoesNotSendOrAudit(t *testing.T) {
	panel := newAgentPanel(t)

	body := requireAgentJSON(t, panel.post("/api/collection/run", "", map[string]string{"dry_run": "1"}), http.StatusOK)
	assert.Equal(t, true, body["ok"])
	assert.Equal(t, true, body["dryRun"])

	outcomes, ok := body["outcomes"].([]any)
	require.True(t, ok)
	require.Len(t, outcomes, 1)
	outcome, ok := outcomes[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, false, outcome["delivered"])
	assert.Contains(t, outcome["message"], "R$ 2.000,00")

	assert.Empty(t, panel.waha.sentMessages())
	assert.Empty(t, panel.auditFor(t, domain.ActionCollectionSent))
}

func TestCollectionRunSendsThroughTheChannel(t *testing.T) {
	panel := newAgentPanel(t)

	body := requireAgentJSON(t, panel.post("/api/collection/run", "", nil), http.StatusOK)
	assert.Equal(t, false, body["dryRun"])

	outcomes, ok := body["outcomes"].([]any)
	require.True(t, ok)
	require.Len(t, outcomes, 1)
	outcome := outcomes[0].(map[string]any)
	assert.Equal(t, true, outcome["delivered"])
	assert.Equal(t, "whatsapp", outcome["channel"])

	sent := panel.waha.sentMessages()
	require.Len(t, sent, 1)
	assert.Equal(t, "5511999999999@c.us", sent[0].chatID)
	assert.Contains(t, sent[0].text, "R$ 2.000,00")
	assert.Contains(t, sent[0].text, "01/09/2026")

	entries := panel.auditFor(t, domain.ActionCollectionSent)
	require.Len(t, entries, 1)
	assert.Equal(t, agentReceiptID, domain.Str(entries[0].EntityID))
	assert.Contains(t, domain.Str(entries[0].Metadata), `"channel":"whatsapp"`)
	assert.Contains(t, domain.Str(entries[0].Metadata), `"stage":3`)

	// Rodar de novo não repete o estágio já enviado.
	second := requireAgentJSON(t, panel.post("/api/collection/run", "", nil), http.StatusOK)
	assert.Empty(t, second["outcomes"].([]any))
	assert.Len(t, panel.waha.sentMessages(), 1)
}

func TestCollectionRunAcceptsJSONBody(t *testing.T) {
	panel := newAgentPanel(t)

	body := requireAgentJSON(t, panel.postJSON("/api/collection/run", `{"dry_run": true}`), http.StatusOK)
	assert.Equal(t, true, body["dryRun"])
	assert.Empty(t, panel.waha.sentMessages())
}

func TestCollectionRunRequiresAdmin(t *testing.T) {
	panel := newAgentPanel(t)

	// Sem a sessão do administrador — mas com o CSRF do formulário, que é
	// conferido antes da sessão.
	form := url.Values{"dry_run": {"1"}, csrfFieldName: {panel.csrfToken}}
	request := httptest.NewRequest(http.MethodPost, "/api/collection/run", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.AddCookie(panel.csrf)
	requireAgentJSON(t, panel.do(request), http.StatusUnauthorized)

	assert.Empty(t, panel.waha.sentMessages())
	assert.Empty(t, panel.auditFor(t, domain.ActionCollectionSent))
}

func TestCollectionRunReportsChannelFailure(t *testing.T) {
	panel := newAgentPanel(t)
	panel.waha.mu.Lock()
	panel.waha.fail = true
	panel.waha.mu.Unlock()

	body := requireAgentJSON(t, panel.post("/api/collection/run", "", nil), http.StatusOK)
	outcomes, ok := body["outcomes"].([]any)
	require.True(t, ok)
	require.Len(t, outcomes, 1)
	outcome := outcomes[0].(map[string]any)
	assert.Equal(t, false, outcome["delivered"])
	assert.Contains(t, outcome["reason"], "Falha")
	assert.Empty(t, panel.auditFor(t, domain.ActionCollectionSent))
}

// ── Aba ───────────────────────────────────────────────────────────

func TestConversationsTabShowsConversationsAndCollectionPlan(t *testing.T) {
	panel := newAgentPanel(t)

	recorder := panel.get("/dashboard?tab=conversations", true)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := recorder.Body.String()

	// O conteúdo é o dado do banco: a conversa do titular, o que ele escreveu por
	// último e o lançamento em atraso com o estágio que a régua sustenta.
	assert.Contains(t, body, "Maria Souza")
	assert.Contains(t, body, agentPhone)
	assert.Contains(t, body, "Já paguei o aluguel, viu?")
	assert.Contains(t, body, domain.CollectionStageLabel(domain.CollectionStageInstallment))
	assert.Contains(t, body, "R$ 2.000,00")
	assert.Contains(t, body, "20 dia(s)")

	// A aba consegue disparar os três comandos: simular/rodar a régua e responder
	// ou escalonar a conversa — cada formulário com o CSRF da sessão.
	assert.Contains(t, body, `action="/api/collection/run"`)
	assert.Contains(t, body, `action="/dashboard/conversation"`)
	assert.Contains(t, body, `name="intent" value="conversation.reply"`)
	assert.Contains(t, body, `name="intent" value="conversation.escalate"`)
	assert.Contains(t, body, fmt.Sprintf(`value="%s"`, panel.csrfToken))
}

func TestConversationsContextIsLoadedOnlyForItsTab(t *testing.T) {
	panel := newAgentPanel(t)

	recorder := panel.get("/dashboard?tab=tenants", true)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	// O que a régua cobraria só aparece na aba de conversas: as outras abas não
	// pagam o contexto de mensageria.
	assert.NotContains(t, recorder.Body.String(), "Podemos combinar o pagamento em partes")
}
