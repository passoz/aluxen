package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
	"github.com/passoz/aluxen/internal/infrastructure/config"
	"github.com/passoz/aluxen/internal/infrastructure/db"
)

// A área de lançamentos é exercitada pelo roteador completo (sessão, CSRF,
// render das abas) sobre o SQLite em memória — o mesmo caminho do sistema.

type rcptClock struct{ now time.Time }

func (c rcptClock) Now() time.Time { return c.now }

type rcptIDs struct{ next int }

func (g *rcptIDs) New() string {
	g.next++
	return fmt.Sprintf("novo-%d", g.next)
}

// rcptHasher evita o custo do bcrypt: o login não é o alvo destes testes.
type rcptHasher struct{}

func (rcptHasher) Hash(password string) (string, error) { return "hash:" + password, nil }

func (rcptHasher) Compare(hash, password string) error {
	if hash == "hash:"+password {
		return nil
	}
	return domain.ErrUnauthorized
}

const (
	rcptStamp   = "2026-09-01 10:00:00"
	rcptUserID  = "user-1"
	rcptLeaseID = "lease-1"
	rcptDueID   = "receipt-1"
	rcptPaidID  = "receipt-2"
)

type rcptPanel struct {
	handler   *Handlers
	store     *db.Store
	now       time.Time
	session   *http.Cookie
	csrf      *http.Cookie
	csrfToken string
}

func newRcptPanel(t *testing.T) *rcptPanel {
	t.Helper()
	ctx := context.Background()

	store, err := db.Open(ctx, ":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	clock := rcptClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)}
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

	panel := &rcptPanel{
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
	}

	require.NoError(t, store.Users.Create(ctx, &domain.User{
		ID: rcptUserID, Username: "passoz", PasswordHash: "hash:segredo",
		PasswordChangedAt: domain.StrPtr(rcptStamp), CreatedAt: rcptStamp,
	}))
	require.NoError(t, store.Tenants.Create(ctx, &domain.Tenant{
		ID: "tenant-1", Name: "Maria Souza", Email: "maria@example.com",
		CPF: domain.StrPtr("123.456.789-00"), CreatedAt: rcptStamp, UpdatedAt: rcptStamp,
	}))
	require.NoError(t, store.Properties.Create(ctx, &domain.Property{
		ID: "property-1", Name: "Apto 12", Address: "Rua A, 100", City: "São Paulo", State: "SP",
		CreatedAt: rcptStamp, UpdatedAt: rcptStamp,
	}))
	require.NoError(t, store.Leases.Create(ctx, &domain.Lease{
		ID: rcptLeaseID, TenantID: "tenant-1", PropertyID: "property-1",
		StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 2500,
		Status: domain.LeaseStatusActive, CreatedAt: rcptStamp, UpdatedAt: rcptStamp,
	}))
	require.NoError(t, store.Receipts.Create(ctx, &domain.Receipt{
		ID: rcptDueID, LeaseID: domain.StrPtr(rcptLeaseID), TenantID: domain.StrPtr("tenant-1"),
		PropertyID: domain.StrPtr("property-1"), Category: domain.ReceiptCategoryRent,
		DueDate: "2026-09-10", Amount: 2500, CreatedAt: rcptStamp,
	}))
	require.NoError(t, store.Receipts.Create(ctx, &domain.Receipt{
		ID: rcptPaidID, LeaseID: domain.StrPtr(rcptLeaseID), TenantID: domain.StrPtr("tenant-1"),
		PropertyID: domain.StrPtr("property-1"), Category: domain.ReceiptCategoryRent,
		Description: domain.StrPtr("Taxa de condomínio"), DueDate: "2026-08-10", Amount: 180,
		PaidAt: domain.StrPtr("2026-08-09T10:00"), PaidAmount: domain.FloatPtr(180), CreatedAt: rcptStamp,
	}))

	// Sessão e CSRF válidos: emitidos pelo próprio gerenciador do sistema.
	sessionRecorder := httptest.NewRecorder()
	panel.handler.Session.SetAdmin(sessionRecorder, rcptUserID)
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

func (p *rcptPanel) do(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	p.handler.Router().ServeHTTP(recorder, request)
	return recorder
}

func (p *rcptPanel) get(target string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.AddCookie(p.session)
	request.AddCookie(p.csrf)
	return p.do(request)
}

// post dispara o intent informado no painel, com CSRF e sessão válidos.
func (p *rcptPanel) post(intent string, fields map[string]string) *httptest.ResponseRecorder {
	form := url.Values{"intent": {intent}, csrfFieldName: {p.csrfToken}}
	for key, value := range fields {
		form.Set(key, value)
	}
	request := httptest.NewRequest(http.MethodPost, "/dashboard", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.AddCookie(p.session)
	request.AddCookie(p.csrf)
	return p.do(request)
}

func (p *rcptPanel) receipt(t *testing.T, id string) *domain.Receipt {
	t.Helper()
	receipt, err := p.store.Receipts.ByID(context.Background(), id)
	require.NoError(t, err)
	return receipt
}

func (p *rcptPanel) receiptCount(t *testing.T) int {
	t.Helper()
	total, err := p.store.Receipts.Count(context.Background())
	require.NoError(t, err)
	return total
}

func (p *rcptPanel) auditActions(t *testing.T) []string {
	t.Helper()
	entries, err := p.store.Audit.List(context.Background(), domain.AuditFilter{Limit: 100})
	require.NoError(t, err)
	actions := make([]string, 0, len(entries))
	for _, entry := range entries {
		actions = append(actions, entry.Action)
	}
	return actions
}

func requireRcptOK(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, true, body["ok"])
}

func requireRcptError(t *testing.T, recorder *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	require.Equal(t, status, recorder.Code, recorder.Body.String())
	var body map[string]string
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, message, body["error"])
}

// rcptModalFill extrai o JSON de `data-modal-fill` do primeiro botão que abre o
// modal informado. O html/template escapa as aspas do JSON na forma `&#34;`; o
// navegador desfaz isso ao ler o atributo, então o teste faz o mesmo.
func rcptModalFill(t *testing.T, body, modal string) map[string]string {
	t.Helper()
	// O botão pode ter outros atributos entre os dois (e quebrar linha entre
	// eles), então o vínculo é "abre este modal" e "carrega este preenchimento",
	// não a adjacência literal dos atributos.
	pattern := regexp.MustCompile(`(?s)data-modal-open="` + modal + `".{0,600}?data-modal-fill='([^']*)'`)
	match := pattern.FindStringSubmatch(body)
	require.NotNil(t, match, "botão do modal %q sem data-modal-fill", modal)
	var fill map[string]string
	require.NoError(t, json.Unmarshal([]byte(html.UnescapeString(match[1])), &fill))
	return fill
}

// ── Intents ────────────────────────────────────────────────────────

func TestReceiptPayIntentRegistersPartialPayment(t *testing.T) {
	panel := newRcptPanel(t)

	recorder := panel.post("receipt.pay", map[string]string{
		"id":         rcptDueID,
		"pay_amount": "1.500,00",
		"paid_at":    "2026-09-20T10:30",
	})
	requireRcptOK(t, recorder)

	receipt := panel.receipt(t, rcptDueID)
	require.NotNil(t, receipt.PaidAmount)
	assert.Equal(t, 1500.0, *receipt.PaidAmount)
	require.NotNil(t, receipt.PaidAt)
	assert.Equal(t, "2026-09-20T10:30", *receipt.PaidAt)
	assert.Equal(t, domain.ReceiptStatusPartial, domain.ReceiptStatusOf(*receipt, panel.now))

	entries, err := panel.store.Audit.List(context.Background(), domain.AuditFilter{Action: domain.ActionReceiptPay, Limit: 10})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "passoz", domain.Str(entries[0].ActorName), "a trilha registra o administrador da sessão")
	assert.Equal(t, domain.AuditOriginWeb, entries[0].Origin)
}

func TestReceiptPayIntentDefaultsToFullAmount(t *testing.T) {
	panel := newRcptPanel(t)

	requireRcptOK(t, panel.post("receipt.pay", map[string]string{"id": rcptDueID}))

	receipt := panel.receipt(t, rcptDueID)
	require.NotNil(t, receipt.PaidAmount)
	assert.Equal(t, 2500.0, *receipt.PaidAmount, "sem valor informado a baixa é pelo total")
	require.NotNil(t, receipt.PaidAt)
	assert.Equal(t, format.FormatLocalDateTime(panel.now), *receipt.PaidAt)
}

func TestReceiptPayIntentRequiresSession(t *testing.T) {
	panel := newRcptPanel(t)

	form := url.Values{"intent": {"receipt.pay"}, "id": {rcptDueID}, csrfFieldName: {panel.csrfToken}}
	request := httptest.NewRequest(http.MethodPost, "/dashboard", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.AddCookie(panel.csrf)

	requireRcptError(t, panel.do(request), http.StatusUnauthorized, "Não autorizado.")
}

func TestReceiptGenerateMonthlyIntentCreatesReceiptsOnce(t *testing.T) {
	panel := newRcptPanel(t)
	require.Equal(t, 2, panel.receiptCount(t))

	requireRcptOK(t, panel.post("receipt.generate-monthly", nil))
	require.Equal(t, 3, panel.receiptCount(t))

	requireRcptOK(t, panel.post("receipt.generate-monthly", nil))
	assert.Equal(t, 3, panel.receiptCount(t), "gerar o mês de novo não duplica")

	generated, err := panel.store.Receipts.List(context.Background())
	require.NoError(t, err)
	found := false
	for _, receipt := range generated {
		if domain.Str(receipt.GeneratedKey) != "lease-1:2026-09" {
			continue
		}
		found = true
		assert.Equal(t, "2026-09-10", receipt.DueDate)
		assert.Equal(t, 2500.0, receipt.Amount)
		assert.Equal(t, domain.ReceiptCategoryRent, receipt.Category)
	}
	assert.True(t, found, "lançamento do mês não foi gerado")

	assert.Contains(t, panel.auditActions(t), domain.ActionReceiptGenerateMonth)
}

func TestReceiptCreateIntentRejectsRentWithoutContract(t *testing.T) {
	panel := newRcptPanel(t)

	recorder := panel.post("receipt.create", map[string]string{
		"category": "rent",
		"due_date": "2026-09-25",
		"amount":   "2500.00",
	})
	requireRcptError(t, recorder, http.StatusBadRequest, "Selecione um contrato para recebimento de aluguel.")
	assert.Equal(t, 2, panel.receiptCount(t))
}

func TestReceiptCreateIntentDerivesLinksFromContract(t *testing.T) {
	panel := newRcptPanel(t)

	recorder := panel.post("receipt.create", map[string]string{
		"id":       "receipt-3",
		"lease_id": rcptLeaseID,
		"category": "rent",
		"due_date": "2026-10-10",
		"amount":   "2500.00",
	})
	requireRcptOK(t, recorder)

	created := panel.receipt(t, "receipt-3")
	assert.Equal(t, "tenant-1", domain.Str(created.TenantID))
	assert.Equal(t, "property-1", domain.Str(created.PropertyID))
	assert.Equal(t, domain.ReceiptStatusDue, domain.ReceiptStatusOf(*created, panel.now))
}

// ── Aba ────────────────────────────────────────────────────────────

func TestReceiptsTabRendersTableFiltersAndModals(t *testing.T) {
	panel := newRcptPanel(t)

	recorder := panel.get("/dashboard?tab=receipts")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := recorder.Body.String()

	for _, expected := range []string{
		"Pagamentos e Recibos",
		"Maria Souza",
		"Apto 12",
		"Aluguel",
		"Taxa de condomínio",
		`data-table data-page-size="10"`,
		`data-filter="recibos"`,
		`data-filter-select="month"`,
		`data-filter-select="status"`,
		`data-batch-item value="receipt-1"`,
		`data-batch-all`,
		`data-batch-action="receipt.batch-pay"`,
		`data-batch-action="receipt.batch-delete"`,
		`data-batch-count`,
		`value="receipt.generate-monthly"`,
		`data-pagination`,
		`data-sort-value="2026-09-10"`,
		`id="receipt-create"`,
		`data-receipt-mode="extra"`,
		`id="receipt-pay"`,
		`id="receipt-edit"`,
		`value="receipt.delete"`,
		`name="csrf_token"`,
		`data-datepicker`,
		`data-mask="money"`,
	} {
		assert.Contains(t, body, expected, "aba de recibos sem %q", expected)
	}

	// Lançamento vencido (10/09 contra hoje 21/09) e lançamento pago.
	assert.Contains(t, body, `data-status="overdue"`)
	assert.Contains(t, body, `data-status="paid"`)
	assert.Contains(t, body, `class="status-badge overdue"`)
	assert.Contains(t, body, `class="status-badge paid"`)
	assert.Contains(t, body, `href="/pdf?id=receipt-2"`)
	assert.Contains(t, body, `data-whatsapp-send="receipt" data-whatsapp-id="receipt-2"`)
	assert.NotContains(t, body, `data-whatsapp-send="receipt" data-whatsapp-id="receipt-1"`,
		"recibo sem baixa não oferece envio")

	// O modal de baixa abre preenchido com o valor total do lançamento.
	fill := rcptModalFill(t, body, "receipt-pay")
	assert.Equal(t, rcptDueID, fill["id"])
	assert.Equal(t, "2500.00", fill["pay_amount"])

	edit := rcptModalFill(t, body, "receipt-edit")
	assert.Equal(t, rcptDueID, edit["id"])
	assert.Equal(t, rcptLeaseID, edit["lease_id"])
	assert.Equal(t, "rent", edit["category"])
	assert.Equal(t, "2026-09-10", edit["due_date"])
	assert.Equal(t, "2500.00", edit["amount"])
	assert.Empty(t, edit["paid_amount"], "lançamento sem baixa não pré-preenche valor pago")
}

func TestReceiptsTabShowsEmptyStateWithoutReceipts(t *testing.T) {
	panel := newRcptPanel(t)
	require.NoError(t, panel.store.Receipts.Delete(context.Background(), rcptDueID))
	require.NoError(t, panel.store.Receipts.Delete(context.Background(), rcptPaidID))

	recorder := panel.get("/dashboard?tab=receipts")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "Cadastre um contrato para gerar recibos.")
}
