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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/config"
	"github.com/passoz/aluxen/internal/infrastructure/db"
)

// O tier 2 assistido é exercitado pelo roteador completo (sessão, CSRF,
// render da aba) sobre o SQLite em memória — o mesmo caminho do sistema.

type tier2Clock struct{ now time.Time }

func (c tier2Clock) Now() time.Time { return c.now }

type tier2IDs struct{ next int }

func (g *tier2IDs) New() string {
	g.next++
	return fmt.Sprintf("novo-%d", g.next)
}

// tier2Hasher evita o custo do bcrypt: o login não é o alvo destes testes.
type tier2Hasher struct{}

func (tier2Hasher) Hash(password string) (string, error) { return "hash:" + password, nil }

func (tier2Hasher) Compare(hash, password string) error {
	if hash == "hash:"+password {
		return nil
	}
	return domain.ErrUnauthorized
}

// tier2FakeChat é o dublê da porta de conversa: devolve o roteiro combinado.
type tier2FakeChat struct {
	responses []application.ChatResponse
}

func (c *tier2FakeChat) Complete(_ context.Context, _ application.ChatRequest) (application.ChatResponse, error) {
	if len(c.responses) == 0 {
		return application.ChatResponse{}, nil
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

const (
	tier2Stamp   = "2026-09-01 10:00:00"
	tier2UserID  = "user-1"
	tier2LeaseID = "lease-1"
)

type tier2Panel struct {
	handler   *Handlers
	store     *db.Store
	now       time.Time
	session   *http.Cookie
	csrf      *http.Cookie
	csrfToken string
	chat      *tier2FakeChat
}

func newTier2Panel(t *testing.T) *tier2Panel {
	t.Helper()
	ctx := context.Background()

	store, err := db.Open(ctx, ":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	clock := tier2Clock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)}
	chat := &tier2FakeChat{}
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
		IDs:    &tier2IDs{},
		Hasher: tier2Hasher{},
		Chat:   chat,
		Log:    slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	renderer, err := NewRenderer()
	require.NoError(t, err)

	cfg := &config.Config{
		AppEnv:        "test",
		Host:          "127.0.0.1",
		Port:          0,
		SessionSecret: "test-session-secret-with-enough-length",
		AppURL:        "http://localhost:3000",
	}

	panel := &tier2Panel{
		handler: NewHandlers(&Common{
			Cfg:     cfg,
			Log:     deps.Log,
			Render:  renderer,
			Session: NewSessionManager(cfg.SessionSecret, false),
			CSRF:    NewCSRF(cfg.SessionSecret, false),
			Deps:    deps,
			Started: clock.now,
			Dialect: "sqlite",
		}),
		store: store,
		now:   clock.now,
		chat:  chat,
	}

	require.NoError(t, store.Users.Create(ctx, &domain.User{
		ID: tier2UserID, Username: "passoz", PasswordHash: "hash:segredo",
		PasswordChangedAt: domain.StrPtr(tier2Stamp), CreatedAt: tier2Stamp,
	}))
	require.NoError(t, store.Tenants.Create(ctx, &domain.Tenant{
		ID: "tenant-1", Name: "Maria Souza", Email: "maria@example.com",
		Phone: domain.StrPtr("(22) 99999-1234"), CPF: domain.StrPtr("123.456.789-00"),
		CreatedAt: tier2Stamp, UpdatedAt: tier2Stamp,
	}))
	require.NoError(t, store.Properties.Create(ctx, &domain.Property{
		ID: "property-1", Name: "Apto 12", Address: "Rua A, 100", City: "São Paulo", State: "SP",
		CreatedAt: tier2Stamp, UpdatedAt: tier2Stamp,
	}))
	require.NoError(t, store.Leases.Create(ctx, &domain.Lease{
		ID: tier2LeaseID, TenantID: "tenant-1", PropertyID: "property-1",
		StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 2500,
		Status: domain.LeaseStatusActive, CreatedAt: tier2Stamp, UpdatedAt: tier2Stamp,
	}))
	require.NoError(t, store.Receipts.Create(ctx, &domain.Receipt{
		ID: "receipt-1", LeaseID: domain.StrPtr(tier2LeaseID), TenantID: domain.StrPtr("tenant-1"),
		PropertyID: domain.StrPtr("property-1"), Category: domain.ReceiptCategoryRent,
		DueDate: "2026-09-10", Amount: 2500, PaidAt: domain.StrPtr("2026-09-09T10:00"),
		PaidAmount: domain.FloatPtr(2500), CreatedAt: tier2Stamp,
	}))
	require.NoError(t, store.Receipts.Create(ctx, &domain.Receipt{
		ID: "receipt-2", LeaseID: domain.StrPtr(tier2LeaseID), TenantID: domain.StrPtr("tenant-1"),
		PropertyID: domain.StrPtr("property-1"), Category: domain.ReceiptCategoryRent,
		DueDate: "2026-09-05", Amount: 300, CreatedAt: tier2Stamp,
	}))

	sessionRecorder := httptest.NewRecorder()
	panel.handler.Session.SetAdmin(sessionRecorder, tier2UserID)
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

// useProvider liga o provedor de conversa com o roteiro informado.
func (p *tier2Panel) useProvider(responses ...application.ChatResponse) {
	p.chat.responses = responses
}

// dropProvider desliga o provedor de conversa.
func (p *tier2Panel) dropProvider() {
	p.handler.Deps.Chat = nil
}

func (p *tier2Panel) do(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	p.handler.Router().ServeHTTP(recorder, request)
	return recorder
}

func (p *tier2Panel) get(target string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("Accept", "application/json")
	request.AddCookie(p.session)
	request.AddCookie(p.csrf)
	return p.do(request)
}

// getPage busca a página HTML do painel (sem o Accept de JSON).
func (p *tier2Panel) getPage(target string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.AddCookie(p.session)
	request.AddCookie(p.csrf)
	return p.do(request)
}

// post dispara um POST urlencoded (formulário do painel) na rota informada.
func (p *tier2Panel) post(target string, fields map[string]string) *httptest.ResponseRecorder {
	form := url.Values{csrfFieldName: {p.csrfToken}}
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

func (p *tier2Panel) lease(t *testing.T) *domain.Lease {
	t.Helper()
	lease, err := p.store.Leases.ByID(context.Background(), tier2LeaseID)
	require.NoError(t, err)
	return lease
}

func (p *tier2Panel) auditActions(t *testing.T) []string {
	t.Helper()
	entries, err := p.store.Audit.List(context.Background(), domain.AuditFilter{Limit: 100})
	require.NoError(t, err)
	actions := make([]string, 0, len(entries))
	for _, entry := range entries {
		actions = append(actions, entry.Action)
	}
	return actions
}

func requireTier2JSON(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, true, body["ok"])
	return body
}

func requireTier2Error(t *testing.T, recorder *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	require.Equal(t, status, recorder.Code, recorder.Body.String())
	var body map[string]string
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, message, body["error"])
}

// ── Relatório do mês ───────────────────────────────────────────────

func TestTier2ReportMonthlyRoute(t *testing.T) {
	panel := newTier2Panel(t)

	recorder := panel.get("/api/report/monthly?year=2026&month=9")
	body := requireTier2JSON(t, recorder)

	report, ok := body["report"].(map[string]any)
	require.True(t, ok, "resposta sem o relatório: %v", body)
	assert.Equal(t, "setembro/2026", report["label"])
	assert.Equal(t, "2026-09", report["monthKey"])
	assert.Equal(t, 2800.0, report["expected"])
	assert.Equal(t, 2500.0, report["received"])
	assert.Equal(t, 0.0, report["open"])
	assert.Equal(t, 300.0, report["overdue"])
}

func TestTier2ReportMonthlyRouteDefaultsToCurrentMonth(t *testing.T) {
	panel := newTier2Panel(t)

	recorder := panel.get("/api/report/monthly")
	body := requireTier2JSON(t, recorder)

	report, ok := body["report"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "2026-09", report["monthKey"], "sem competência na query vale o mês corrente")
}

func TestTier2ReportMonthlyRouteRejectsInvalidCompetence(t *testing.T) {
	panel := newTier2Panel(t)

	requireTier2Error(t, panel.get("/api/report/monthly?year=2026&month=13"),
		http.StatusBadRequest, "Informe um ano e um mês válidos.")
	requireTier2Error(t, panel.get("/api/report/monthly?year=abc&month=9"),
		http.StatusBadRequest, "Informe um ano e um mês válidos.")
}

// ── Consulta em linguagem natural ──────────────────────────────────

func TestTier2ReportAskRouteAnswersFromTools(t *testing.T) {
	panel := newTier2Panel(t)
	panel.useProvider(
		application.ChatResponse{ToolCalls: []application.ChatToolCall{{
			Name:      "resumo_mes",
			Arguments: map[string]any{"year": float64(2026), "month": float64(9)},
		}}},
		application.ChatResponse{},
	)

	recorder := panel.post("/api/report/ask", map[string]string{"question": "Quanto recebi em setembro?"})
	body := requireTier2JSON(t, recorder)

	answer, ok := body["answer"].(map[string]any)
	require.True(t, ok, "resposta sem a consulta: %v", body)
	assert.Equal(t, false, answer["refused"])
	assert.Contains(t, answer["answer"], "R$ 2.500,00")
	assert.Equal(t, []any{"resumo_mes"}, answer["tools"])
}

func TestTier2ReportAskRouteRefusesQuestionWithoutTool(t *testing.T) {
	panel := newTier2Panel(t)
	panel.useProvider(application.ChatResponse{Message: "Não sei."})

	recorder := panel.post("/api/report/ask", map[string]string{"question": "Qual a cor do céu?"})
	body := requireTier2JSON(t, recorder)

	answer, ok := body["answer"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, answer["refused"])
	assert.Equal(t, application.AskRefusalMessage, answer["answer"])
}

func TestTier2ReportAskRouteWithoutProvider(t *testing.T) {
	panel := newTier2Panel(t)
	panel.dropProvider()

	recorder := panel.post("/api/report/ask", map[string]string{"question": "Quanto recebi?"})
	requireTier2Error(t, recorder, http.StatusServiceUnavailable,
		"A consulta em linguagem natural não está configurada nesta instância.")
}

func TestTier2ReportAskRouteRejectsEmptyQuestion(t *testing.T) {
	panel := newTier2Panel(t)

	requireTier2Error(t, panel.post("/api/report/ask", map[string]string{"question": "  "}),
		http.StatusBadRequest, "Escreva a pergunta.")
}

func TestTier2ReportAskRouteAcceptsJSONBody(t *testing.T) {
	panel := newTier2Panel(t)
	panel.useProvider(
		application.ChatResponse{ToolCalls: []application.ChatToolCall{{Name: "abertos"}}},
		application.ChatResponse{Message: "Em setembro/2026 há R$ 300,00 vencido."},
	)

	request := httptest.NewRequest(http.MethodPost, "/api/report/ask?csrf_token="+url.QueryEscape(panel.csrfToken),
		strings.NewReader(`{"question":"O que está vencido?"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.AddCookie(panel.session)
	request.AddCookie(panel.csrf)

	body := requireTier2JSON(t, panel.do(request))
	answer, ok := body["answer"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Em setembro/2026 há R$ 300,00 vencido.", answer["answer"])
}

// ── Reajuste ───────────────────────────────────────────────────────

func TestTier2AdjustmentPreviewRoute(t *testing.T) {
	panel := newTier2Panel(t)

	recorder := panel.post("/api/adjustment/preview", map[string]string{
		"lease_id":       tier2LeaseID,
		"index":          "igpm",
		"rate":           "4,5",
		"effective_date": "2026-10-01",
	})
	body := requireTier2JSON(t, recorder)

	preview, ok := body["preview"].(map[string]any)
	require.True(t, ok, "resposta sem a prévia: %v", body)
	assert.Equal(t, 2500.0, preview["previousAmount"])
	assert.Equal(t, 2612.5, preview["newAmount"])
	assert.Equal(t, "R$ 2.612,50", preview["newAmountLabel"])
	assert.Equal(t, "IGP-M", preview["indexLabel"])
	assert.Equal(t, "4,5%", preview["rateLabel"])
	assert.Equal(t, "01/10/2026", preview["effectiveDateLabel"])

	assert.Equal(t, 2500.0, panel.lease(t).RentAmount, "a prévia não grava nada")
	assert.Empty(t, panel.auditActions(t))
}

func TestTier2AdjustmentPreviewRouteRejectsBadInput(t *testing.T) {
	panel := newTier2Panel(t)

	cases := []struct {
		name    string
		fields  map[string]string
		status  int
		message string
	}{
		{
			"sem contrato",
			map[string]string{"index": "igpm", "rate": "4,5", "effective_date": "2026-10-01"},
			http.StatusBadRequest, "Selecione o contrato do reajuste.",
		},
		{
			"índice fora do vocabulário",
			map[string]string{"lease_id": tier2LeaseID, "index": "inpc", "rate": "4,5", "effective_date": "2026-10-01"},
			http.StatusBadRequest, "Escolha o índice do reajuste (IGP-M ou IPCA).",
		},
		{
			"percentual vazio",
			map[string]string{"lease_id": tier2LeaseID, "index": "igpm", "effective_date": "2026-10-01"},
			http.StatusBadRequest, "Informe o percentual do reajuste.",
		},
		{
			"percentual zerado",
			map[string]string{"lease_id": tier2LeaseID, "index": "igpm", "rate": "0", "effective_date": "2026-10-01"},
			http.StatusBadRequest, "Informe um percentual de reajuste maior que zero.",
		},
		{
			"percentual ilegível",
			map[string]string{"lease_id": tier2LeaseID, "index": "igpm", "rate": "abc", "effective_date": "2026-10-01"},
			http.StatusBadRequest, "Informe um percentual de reajuste válido.",
		},
		{
			"vigência vazia",
			map[string]string{"lease_id": tier2LeaseID, "index": "igpm", "rate": "4,5"},
			http.StatusBadRequest, "Informe a data de vigência do reajuste.",
		},
		{
			"contrato desconhecido",
			map[string]string{"lease_id": "lease-inexistente", "index": "igpm", "rate": "4,5", "effective_date": "2026-10-01"},
			http.StatusNotFound, "Registro não encontrado.",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			requireTier2Error(t, panel.post("/api/adjustment/preview", testCase.fields), testCase.status, testCase.message)
		})
	}
}

func TestTier2AdjustmentApplyRoute(t *testing.T) {
	panel := newTier2Panel(t)

	fields := map[string]string{
		"lease_id":       tier2LeaseID,
		"index":          "igpm",
		"rate":           "4,5",
		"effective_date": "2026-10-01",
	}

	requireTier2Error(t, panel.post("/api/adjustment/apply", fields),
		http.StatusBadRequest, "Confirme o reajuste para aplicar.")
	assert.Equal(t, 2500.0, panel.lease(t).RentAmount, "sem confirmação nada é gravado")

	fields["confirm"] = "1"
	body := requireTier2JSON(t, panel.post("/api/adjustment/apply", fields))

	result, ok := body["result"].(map[string]any)
	require.True(t, ok, "resposta sem o resultado: %v", body)
	assert.Equal(t, "deterministico", result["noticeSource"])
	notice, _ := result["notice"].(string)
	assert.Contains(t, notice, "R$ 2.612,50")
	assert.Contains(t, notice, "01/10/2026")
	assert.Contains(t, notice, "Maria Souza")

	assert.Equal(t, 2612.5, panel.lease(t).RentAmount)
	assert.Contains(t, panel.auditActions(t), domain.ActionAdjustmentApplied)
}

func TestTier2AdjustmentApplyRouteUsesProviderForNotice(t *testing.T) {
	panel := newTier2Panel(t)
	panel.useProvider(application.ChatResponse{
		Message: "Prezada Maria Souza, o aluguel passa a R$ 2.612,50 a partir de 01/10/2026.",
	})

	body := requireTier2JSON(t, panel.post("/api/adjustment/apply", map[string]string{
		"lease_id":       tier2LeaseID,
		"index":          "ipca",
		"rate":           "4.5",
		"effective_date": "2026-10-01",
		"confirm":        "1",
	}))

	result, ok := body["result"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ia", result["noticeSource"])
	assert.Equal(t, "Prezada Maria Souza, o aluguel passa a R$ 2.612,50 a partir de 01/10/2026.", result["notice"])
	assert.Equal(t, 2612.5, panel.lease(t).RentAmount, "o valor gravado é o calculado, não o do modelo")
}

// ── Aba de reajuste ────────────────────────────────────────────────

func TestTier2AdjustmentTabRendersContractsAndHistory(t *testing.T) {
	panel := newTier2Panel(t)

	recorder := panel.getPage("/dashboard?tab=adjustment")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	page := recorder.Body.String()

	assert.Contains(t, page, "Reajuste assistido")
	assert.Contains(t, page, "Apto 12")
	assert.Contains(t, page, "R$ 2.500,00")
	assert.Contains(t, page, "Nunca reajustado")
	assert.Contains(t, page, `data-adjustment-select="`+tier2LeaseID+`"`)

	requireTier2JSON(t, panel.post("/api/adjustment/apply", map[string]string{
		"lease_id":       tier2LeaseID,
		"index":          "igpm",
		"rate":           "4,5",
		"effective_date": "2026-10-01",
		"confirm":        "1",
	}))

	recorder = panel.getPage("/dashboard?tab=adjustment")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	page = recorder.Body.String()

	assert.Contains(t, page, "R$ 2.612,50", "a aba mostra o valor atualizado")
	assert.Contains(t, page, "Histórico", "o reajuste aplicado aparece na trilha da aba")
	assert.Contains(t, page, "01/10/2026")
	assert.NotContains(t, page, "Nunca reajustado")
}

func TestTier2AdjustmentTabOnlyLoadsForItsOwnTab(t *testing.T) {
	panel := newTier2Panel(t)

	recorder := panel.getPage("/dashboard?tab=tenants")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), "Reajuste assistido", "a aba de inquilinos não carrega o reajuste")
}

// ── Sessão ─────────────────────────────────────────────────────────

func TestTier2RoutesRequireAdminSession(t *testing.T) {
	panel := newTier2Panel(t)

	for _, target := range []string{"/api/report/monthly"} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Accept", "application/json")
		recorder := panel.do(request)
		requireTier2Error(t, recorder, http.StatusUnauthorized, "Não autorizado.")
	}

	for _, target := range []string{"/api/report/ask", "/api/adjustment/preview", "/api/adjustment/apply"} {
		request := httptest.NewRequest(http.MethodPost, target, strings.NewReader("csrf_token="+url.QueryEscape(panel.csrfToken)))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		request.AddCookie(panel.csrf)
		recorder := panel.do(request)
		requireTier2Error(t, recorder, http.StatusUnauthorized, "Não autorizado.")
	}
}
