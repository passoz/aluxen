package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
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

// As rotas do comprovante são exercitadas pelo roteador completo (sessão, CSRF,
// multipart) sobre o SQLite em memória — o mesmo caminho do sistema.

type payClock struct{ now time.Time }

func (c payClock) Now() time.Time { return c.now }

type payIDs struct{ next int }

func (g *payIDs) New() string {
	g.next++
	return fmt.Sprintf("pay-%d", g.next)
}

// payHasher evita o custo do bcrypt: o login não é o alvo destes testes.
type payHasher struct{}

func (payHasher) Hash(password string) (string, error) { return "hash:" + password, nil }

func (payHasher) Compare(hash, password string) error {
	if hash == "hash:"+password {
		return nil
	}
	return domain.ErrUnauthorized
}

// payFiles é o armazenamento de anexos em memória.
type payFiles struct {
	files map[string][]byte
	next  int
}

func newPayFiles() *payFiles { return &payFiles{files: map[string][]byte{}} }

func (f *payFiles) Save(prefix, filename string, data []byte) (string, error) {
	f.next++
	name := fmt.Sprintf("%s/%d-%s", prefix, f.next, filename)
	f.files[name] = append([]byte(nil), data...)
	return name, nil
}

func (f *payFiles) Open(name string) ([]byte, error) {
	data, ok := f.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (f *payFiles) Delete(name string) error {
	delete(f.files, name)
	return nil
}

// payExtractor é o provedor de leitura dublado.
type payExtractor struct {
	extraction domain.PaymentExtraction
	err        error
}

func (e *payExtractor) ExtractPaymentProof(context.Context, application.DocumentFile) (domain.PaymentExtraction, string, error) {
	if e.err != nil {
		return domain.PaymentExtraction{}, "", e.err
	}
	return e.extraction, `{"amount":2000}`, nil
}

const (
	payStamp     = "2026-09-01 10:00:00"
	payUserID    = "user-1"
	payTenantID  = "tenant-1"
	payReceiptID = "receipt-1"
)

type payPanel struct {
	handler   *Handlers
	store     *db.Store
	files     *payFiles
	extractor *payExtractor
	session   *http.Cookie
	csrf      *http.Cookie
	csrfToken string
}

func newPayPanel(t *testing.T) *payPanel {
	t.Helper()
	ctx := context.Background()

	store, err := db.Open(ctx, ":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	clock := payClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)}
	files := newPayFiles()
	// Valor lido diferente do lançamento: é o caso que cai na revisão humana.
	extractor := &payExtractor{extraction: domain.PaymentExtraction{
		Amount:     2000,
		Date:       "2026-09-09",
		Payer:      "Maria Souza",
		Payee:      "Carlos Drummond",
		Identifier: "PIX-9988",
		Confidence: 0.95,
	}}

	deps := application.Deps{
		Repos: application.Repositories{
			Users:         store.Users,
			Tenants:       store.Tenants,
			Properties:    store.Properties,
			Leases:        store.Leases,
			Receipts:      store.Receipts,
			Settings:      store.Settings,
			Audit:         store.Audit,
			WhatsApp:      store.WhatsApp,
			Inspections:   store.Inspections,
			Jobs:          store.Jobs,
			PaymentProofs: store.PaymentProofs,
			Intakes:       store.Intakes,
		},
		Clock:       clock,
		IDs:         &payIDs{},
		Hasher:      payHasher{},
		Log:         slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelError})),
		Attachments: files,
		Proofs:      extractor,
		Opts:        application.Options{AIConfidenceThreshold: 0.9, AIEnabled: true},
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

	panel := &payPanel{
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
		store:     store,
		files:     files,
		extractor: extractor,
	}

	require.NoError(t, store.Users.Create(ctx, &domain.User{
		ID: payUserID, Username: "passoz", PasswordHash: "hash:segredo",
		PasswordChangedAt: domain.StrPtr(payStamp), CreatedAt: payStamp,
	}))
	require.NoError(t, store.Tenants.Create(ctx, &domain.Tenant{
		ID: payTenantID, Name: "Maria Souza", Email: "maria@example.com",
		CPF: domain.StrPtr("123.456.789-00"), CreatedAt: payStamp, UpdatedAt: payStamp,
	}))
	require.NoError(t, store.Properties.Create(ctx, &domain.Property{
		ID: "property-1", Name: "Apto 12", Address: "Rua A, 100", City: "São Paulo", State: "SP",
		CreatedAt: payStamp, UpdatedAt: payStamp,
	}))
	require.NoError(t, store.Leases.Create(ctx, &domain.Lease{
		ID: "lease-1", TenantID: payTenantID, PropertyID: "property-1",
		StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 2500,
		Status: domain.LeaseStatusActive, CreatedAt: payStamp, UpdatedAt: payStamp,
	}))
	require.NoError(t, store.Receipts.Create(ctx, &domain.Receipt{
		ID: payReceiptID, LeaseID: domain.StrPtr("lease-1"), TenantID: domain.StrPtr(payTenantID),
		PropertyID: domain.StrPtr("property-1"), Category: domain.ReceiptCategoryRent,
		DueDate: "2026-09-10", Amount: 2500, CreatedAt: payStamp,
	}))
	require.NoError(t, store.Settings.Set(ctx, domain.SettingLandlordName, "Carlos Drummond"))
	require.NoError(t, store.Settings.Set(ctx, domain.SettingLandlordCPF, "123.456.789-00"))

	sessionRecorder := httptest.NewRecorder()
	panel.handler.Session.SetAdmin(sessionRecorder, payUserID)
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

func (p *payPanel) do(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	p.handler.Router().ServeHTTP(recorder, request)
	return recorder
}

// authed prepara a requisição com a sessão e o CSRF do painel.
func (p *payPanel) authed(request *http.Request) *http.Request {
	request.AddCookie(p.session)
	request.AddCookie(p.csrf)
	return request
}

// upload envia o comprovante como multipart, com o campo `file`.
func (p *payPanel) upload(t *testing.T, fields map[string]string, filename, mimeType string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := payMultipart(t, p.csrfToken, fields, filename, mimeType, content)

	request := httptest.NewRequest(http.MethodPost, "/api/payments/proof", body)
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", "application/json")
	return p.do(p.authed(request))
}

// payMultipart monta o corpo multipart do upload (CSRF + campos + arquivo).
func payMultipart(t *testing.T, csrfToken string, fields map[string]string, filename, mimeType string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField(csrfFieldName, csrfToken))
	for key, value := range fields {
		require.NoError(t, writer.WriteField(key, value))
	}
	if filename != "" {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
		header.Set("Content-Type", mimeType)
		part, err := writer.CreatePart(header)
		require.NoError(t, err)
		_, err = part.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return &body, writer.FormDataContentType()
}

// review envia a decisão humana como o formulário do painel.
func (p *payPanel) review(fields map[string]string) *httptest.ResponseRecorder {
	form := url.Values{csrfFieldName: {p.csrfToken}}
	for key, value := range fields {
		form.Set(key, value)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/payments/review", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	return p.do(p.authed(request))
}

// queue é um comprovante que já foi lido e ficou em revisão.
func (p *payPanel) queue(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	outcome, err := application.SubmitPaymentProof(ctx, p.handler.Deps,
		application.AuditContext{Origin: domain.AuditOriginWeb},
		application.PaymentProofInput{
			Source: domain.PaymentProofSourcePanel, TenantID: payTenantID,
			FileName: "comprovante.jpg", Mime: "image/jpeg", Bytes: []byte("bytes-do-comprovante"),
		})
	require.NoError(t, err)
	result, err := application.ProcessPaymentProof(ctx, p.handler.Deps, outcome.ProofID)
	require.NoError(t, err)
	require.Equal(t, domain.PaymentProofStatusReview, result.Status)
	return outcome.ProofID
}

func decodeJSON(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), recorder.Body.String())
	return body
}

// ── Upload ─────────────────────────────────────────────────────────

func TestPaymentProofUploadRequiresAdminSession(t *testing.T) {
	panel := newPayPanel(t)

	// Sem sessão: o CSRF passa (token válido) e a rota recusa o anônimo.
	body, contentType := payMultipart(t, panel.csrfToken, nil, "comprovante.jpg", "image/jpeg", []byte("bytes-do-comprovante"))
	request := httptest.NewRequest(http.MethodPost, "/api/payments/proof", body)
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", "application/json")
	request.AddCookie(panel.csrf)
	recorder := panel.do(request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.Equal(t, "Não autorizado.", decodeJSON(t, recorder)["error"])
}

func TestPaymentProofUploadStoresFileAndQueuesReading(t *testing.T) {
	panel := newPayPanel(t)

	recorder := panel.upload(t, map[string]string{"tenant_id": payTenantID}, "comprovante.jpg", "image/jpeg", []byte("bytes-do-comprovante"))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	body := decodeJSON(t, recorder)
	assert.Equal(t, true, body["ok"])
	proofID, _ := body["proofId"].(string)
	jobID, _ := body["jobId"].(string)
	require.NotEmpty(t, proofID)
	require.NotEmpty(t, jobID)

	proof, err := panel.store.PaymentProofs.ByID(context.Background(), proofID)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentProofStatusPending, proof.Status)
	assert.Equal(t, "comprovante.jpg", domain.Str(proof.FileName))
	assert.Equal(t, []byte("bytes-do-comprovante"), panel.files.files[domain.Str(proof.FilePath)])

	job, err := panel.store.Jobs.ByID(context.Background(), jobID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobKindPaymentProof, job.Kind)
	assert.Equal(t, domain.JobStatusQueued, job.Status)
}

func TestPaymentProofUploadWithoutFileIsRejected(t *testing.T) {
	panel := newPayPanel(t)

	// Multipart sem o campo `file`: falta o comprovante.
	body, contentType := payMultipart(t, panel.csrfToken, map[string]string{"tenant_id": payTenantID}, "", "", nil)
	request := httptest.NewRequest(http.MethodPost, "/api/payments/proof", body)
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", "application/json")
	recorder := panel.do(panel.authed(request))

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, "Envie o arquivo do comprovante.", decodeJSON(t, recorder)["error"])
}

// ── Fila de revisão ────────────────────────────────────────────────

func TestPaymentProofReviewListReturnsTheQueue(t *testing.T) {
	panel := newPayPanel(t)
	proofID := panel.queue(t)

	request := httptest.NewRequest(http.MethodGet, "/api/payments/review", nil)
	request.Header.Set("Accept", "application/json")
	recorder := panel.do(panel.authed(request))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	body := decodeJSON(t, recorder)
	assert.Equal(t, true, body["ok"])
	proofs, ok := body["proofs"].([]any)
	require.True(t, ok, "resposta sem a fila: %v", body)
	require.Len(t, proofs, 1)

	row, ok := proofs[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, proofID, row["id"])
	assert.Equal(t, "Maria Souza", row["tenantName"])
	assert.Equal(t, 2000.0, row["extractedAmount"])
	assert.Equal(t, "valor do comprovante diferente do lançamento", row["reason"])
	assert.Equal(t, "/api/payments/proof/"+proofID+"/file", row["fileUrl"])
}

func TestPaymentProofReviewListRequiresAdminSession(t *testing.T) {
	panel := newPayPanel(t)

	request := httptest.NewRequest(http.MethodGet, "/api/payments/review", nil)
	request.Header.Set("Accept", "application/json")
	recorder := panel.do(request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.Equal(t, "Não autorizado.", decodeJSON(t, recorder)["error"])
}

// ── Decisão humana ─────────────────────────────────────────────────

func TestPaymentProofReviewDecisionApprovesAndPaysReceipt(t *testing.T) {
	panel := newPayPanel(t)
	proofID := panel.queue(t)

	recorder := panel.review(map[string]string{
		"proof_id": proofID,
		"approve":  "1",
		"reason":   "valor confere no extrato",
	})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, true, decodeJSON(t, recorder)["ok"])

	proof, err := panel.store.PaymentProofs.ByID(context.Background(), proofID)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentProofStatusAccepted, proof.Status)
	assert.Equal(t, domain.PaymentDecisionManual, proof.Decision)
	assert.Equal(t, "passoz", domain.Str(proof.DecidedBy))

	receipt, err := panel.store.Receipts.ByID(context.Background(), payReceiptID)
	require.NoError(t, err)
	require.NotNil(t, receipt.PaidAt)
	require.NotNil(t, receipt.PaidAmount)
	assert.InDelta(t, 2500.0, *receipt.PaidAmount, 0.0001)

	entries, err := panel.store.Audit.List(context.Background(), domain.AuditFilter{
		Action: domain.ActionPaymentProofAccepted, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, domain.AuditOriginWeb, entries[0].Origin)
}

func TestPaymentProofReviewDecisionRejectsWithReason(t *testing.T) {
	panel := newPayPanel(t)
	proofID := panel.queue(t)

	recorder := panel.review(map[string]string{
		"proof_id": proofID,
		"approve":  "0",
		"reason":   "comprovante de outro mês",
	})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	proof, err := panel.store.PaymentProofs.ByID(context.Background(), proofID)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentProofStatusRejected, proof.Status)
	assert.Equal(t, "comprovante de outro mês", domain.Str(proof.DecisionReason))
	assert.Equal(t, "passoz", domain.Str(proof.DecidedBy))

	receipt, err := panel.store.Receipts.ByID(context.Background(), payReceiptID)
	require.NoError(t, err)
	assert.Nil(t, receipt.PaidAt, "recusa não dá baixa")
}

func TestPaymentProofReviewDecisionRequiresReasonToReject(t *testing.T) {
	panel := newPayPanel(t)
	proofID := panel.queue(t)

	recorder := panel.review(map[string]string{"proof_id": proofID, "approve": "0"})
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, "Informe o motivo da recusa.", decodeJSON(t, recorder)["error"])
}

func TestPaymentProofReviewDecisionAcceptsJSONBody(t *testing.T) {
	panel := newPayPanel(t)
	proofID := panel.queue(t)

	// O token do double-submit vai na query para clientes que mandam JSON.
	target := "/api/payments/review?csrf_token=" + url.QueryEscape(panel.csrfToken)
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(
		fmt.Sprintf(`{"proofId":%q,"approve":true,"reason":"conferido no extrato"}`, proofID)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	recorder := panel.do(panel.authed(request))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	proof, err := panel.store.PaymentProofs.ByID(context.Background(), proofID)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentProofStatusAccepted, proof.Status)
}

// ── Arquivo do comprovante ─────────────────────────────────────────

func TestPaymentProofFileServesTheAttachment(t *testing.T) {
	panel := newPayPanel(t)
	recorder := panel.upload(t, map[string]string{"tenant_id": payTenantID}, "comprovante.jpg", "image/jpeg", []byte("bytes-do-comprovante"))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	proofID, _ := decodeJSON(t, recorder)["proofId"].(string)

	request := httptest.NewRequest(http.MethodGet, "/api/payments/proof/"+proofID+"/file", nil)
	request.Header.Set("Accept", "application/json")
	served := panel.do(panel.authed(request))

	require.Equal(t, http.StatusOK, served.Code, served.Body.String())
	assert.Equal(t, "image/jpeg", served.Header().Get("Content-Type"))
	assert.Contains(t, served.Header().Get("Content-Disposition"), "inline")
	assert.Contains(t, served.Header().Get("Content-Disposition"), "comprovante.jpg")
	assert.Equal(t, "bytes-do-comprovante", served.Body.String())
}

func TestPaymentProofFileWithoutAdminSessionIsUnauthorized(t *testing.T) {
	panel := newPayPanel(t)
	proofID := panel.queue(t)

	request := httptest.NewRequest(http.MethodGet, "/api/payments/proof/"+proofID+"/file", nil)
	request.Header.Set("Accept", "application/json")
	recorder := panel.do(request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.Equal(t, "Não autorizado.", decodeJSON(t, recorder)["error"])
}

func TestPaymentProofFileUnknownProofIsNotFound(t *testing.T) {
	panel := newPayPanel(t)

	request := httptest.NewRequest(http.MethodGet, "/api/payments/proof/inexistente/file", nil)
	request.Header.Set("Accept", "application/json")
	recorder := panel.do(panel.authed(request))

	require.Equal(t, http.StatusNotFound, recorder.Code, recorder.Body.String())
}

func TestPaymentProofFileMissingAttachmentIsNotFound(t *testing.T) {
	panel := newPayPanel(t)
	recorder := panel.upload(t, map[string]string{"tenant_id": payTenantID}, "comprovante.jpg", "image/jpeg", []byte("bytes-do-comprovante"))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	proofID, _ := decodeJSON(t, recorder)["proofId"].(string)

	proof, err := panel.store.PaymentProofs.ByID(context.Background(), proofID)
	require.NoError(t, err)
	require.NoError(t, panel.files.Delete(domain.Str(proof.FilePath)))

	request := httptest.NewRequest(http.MethodGet, "/api/payments/proof/"+proofID+"/file", nil)
	request.Header.Set("Accept", "application/json")
	served := panel.do(panel.authed(request))

	require.Equal(t, http.StatusNotFound, served.Code, served.Body.String())
}

func TestPaymentProofFileRefusesContentOutsideImageOrPDF(t *testing.T) {
	panel := newPayPanel(t)
	ctx := context.Background()

	// Registro com tipo de conteúdo arbitrário: esta rota não serve.
	panel.files.files["payment-proofs/nota.txt"] = []byte("<script>alert(1)</script>")
	require.NoError(t, panel.store.PaymentProofs.Create(ctx, &domain.PaymentProof{
		ID: "proof-nota", CreatedAt: payStamp, UpdatedAt: payStamp,
		Source: domain.PaymentProofSourcePanel, FileName: domain.StrPtr("nota.txt"),
		FilePath: domain.StrPtr("payment-proofs/nota.txt"), Mime: domain.StrPtr("text/html"),
		Status: domain.PaymentProofStatusReview, Decision: domain.PaymentDecisionPending,
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/payments/proof/proof-nota/file", nil)
	request.Header.Set("Accept", "application/json")
	served := panel.do(panel.authed(request))

	require.Equal(t, http.StatusNotFound, served.Code, served.Body.String())
	assert.NotContains(t, served.Body.String(), "script")
}

// reviewTabData é o contexto que o painel entrega ao template da aba de
// revisão (mesmos campos de tabData usados pelo template).
type reviewTabData struct {
	CSRFToken string
	Review    []application.PaymentProofRow
}

func TestReviewTabRendersQueueWithDecisions(t *testing.T) {
	panel := newPayPanel(t)
	proofID := panel.queue(t)

	rows, err := application.ListPaymentProofReview(context.Background(), panel.handler.Deps, 50)
	require.NoError(t, err)

	content, err := panel.handler.Render.Execute("tab_review", &reviewTabData{
		CSRFToken: panel.csrfToken,
		Review:    rows,
	})
	require.NoError(t, err)

	assert.Contains(t, content, `data-table`)
	assert.Contains(t, content, proofID)
	assert.Contains(t, content, "/api/payments/proof/"+proofID+"/file")
	assert.Contains(t, content, "valor do comprovante diferente do lançamento")
	assert.Contains(t, content, panel.csrfToken)
	assert.Contains(t, content, `data-modal-open="review-reject"`)
	assert.Contains(t, content, "R$ 2.000,00")
	assert.Contains(t, content, "R$ 2.500,00")
	assert.Contains(t, content, `action="/api/payments/review"`)
}

func TestReviewTabRendersEmptyQueue(t *testing.T) {
	panel := newPayPanel(t)

	content, err := panel.handler.Render.Execute("tab_review", &reviewTabData{CSRFToken: panel.csrfToken})
	require.NoError(t, err)
	assert.Contains(t, content, "Nenhum comprovante aguardando decisão.")
}

func TestReviewTabRendersThroughTheDashboardRoute(t *testing.T) {
	panel := newPayPanel(t)
	proofID := panel.queue(t)

	request := httptest.NewRequest(http.MethodGet, "/dashboard?tab=review", nil)
	recorder := panel.do(panel.authed(request))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := recorder.Body.String()
	assert.Contains(t, body, "Revisão de comprovantes")
	assert.Contains(t, body, proofID)
	assert.Contains(t, body, "/api/payments/proof/"+proofID+"/file")
	assert.Contains(t, body, "valor do comprovante diferente do lançamento")
}
