package application_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db"
)

// ── Apoio ──────────────────────────────────────────────────────────
//
// A esteira do comprovante roda sobre o SQLite em memória (as mesmas queries do
// sistema) com o provedor de leitura e o armazenamento de arquivos dublados. O
// que o teste prova é a decisão e o que ela grava: a IA é dublê de propósito,
// porque quem decide a baixa é `domain.EvaluatePaymentMatch`.

type proofClock struct{ now time.Time }

func (c proofClock) Now() time.Time { return c.now }

type proofIDs struct{ next int }

func (g *proofIDs) New() string {
	g.next++
	return fmt.Sprintf("novo-%d", g.next)
}

// proofFiles é o armazenamento de anexos em memória.
type proofFiles struct {
	files map[string][]byte
	next  int
}

func newProofFiles() *proofFiles { return &proofFiles{files: map[string][]byte{}} }

func (f *proofFiles) Save(prefix, filename string, data []byte) (string, error) {
	f.next++
	name := fmt.Sprintf("%s/%d-%s", prefix, f.next, filename)
	f.files[name] = append([]byte(nil), data...)
	return name, nil
}

func (f *proofFiles) Open(name string) ([]byte, error) {
	data, ok := f.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (f *proofFiles) Delete(name string) error {
	delete(f.files, name)
	return nil
}

// proofExtractor é o provedor de leitura dublado: devolve a extração (ou o erro)
// que o teste mandar, e guarda o documento recebido.
type proofExtractor struct {
	extraction domain.PaymentExtraction
	raw        string
	err        error
	calls      int
	document   application.DocumentFile
}

func (e *proofExtractor) ExtractPaymentProof(_ context.Context, doc application.DocumentFile) (domain.PaymentExtraction, string, error) {
	e.calls++
	e.document = doc
	if e.err != nil {
		return domain.PaymentExtraction{}, e.raw, e.err
	}
	return e.extraction, e.raw, nil
}

const (
	proofStamp     = "2026-09-01 10:00:00"
	proofTenantID  = "tenant-1"
	proofLeaseID   = "lease-1"
	proofReceiptID = "receipt-1"
)

type proofFixture struct {
	deps      application.Deps
	store     *db.Store
	files     *proofFiles
	extractor *proofExtractor
	now       time.Time
}

func newProofFixture(t *testing.T) *proofFixture {
	t.Helper()
	ctx := context.Background()

	store, err := db.Open(ctx, ":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)
	files := newProofFiles()
	extractor := &proofExtractor{extraction: domain.PaymentExtraction{
		Amount:     2500,
		Date:       "2026-09-09",
		Payer:      "Maria Souza",
		Payee:      "Carlos Drummond",
		Identifier: "PIX-9988",
		Confidence: 0.95,
	}, raw: `{"amount":2500}`}

	fixture := &proofFixture{
		store:     store,
		files:     files,
		extractor: extractor,
		now:       now,
		deps: application.Deps{
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
			Clock:       proofClock{now: now},
			IDs:         &proofIDs{},
			Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
			Attachments: files,
			Proofs:      extractor,
			Opts:        application.Options{AIConfidenceThreshold: 0.9, AIEnabled: true},
		},
	}

	require.NoError(t, store.Tenants.Create(ctx, &domain.Tenant{
		ID: proofTenantID, Name: "Maria Souza", Email: "maria@example.com",
		CPF: domain.StrPtr("123.456.789-00"), CreatedAt: proofStamp, UpdatedAt: proofStamp,
	}))
	require.NoError(t, store.Tenants.Create(ctx, &domain.Tenant{
		ID: "tenant-2", Name: "João Lima", Email: "joao@example.com",
		CreatedAt: proofStamp, UpdatedAt: proofStamp,
	}))
	require.NoError(t, store.Properties.Create(ctx, &domain.Property{
		ID: "property-1", Name: "Apto 12", Address: "Rua A, 100", City: "São Paulo", State: "SP",
		CreatedAt: proofStamp, UpdatedAt: proofStamp,
	}))
	require.NoError(t, store.Leases.Create(ctx, &domain.Lease{
		ID: proofLeaseID, TenantID: proofTenantID, PropertyID: "property-1",
		StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 2500,
		Status: domain.LeaseStatusActive, CreatedAt: proofStamp, UpdatedAt: proofStamp,
	}))
	require.NoError(t, store.Receipts.Create(ctx, &domain.Receipt{
		ID: proofReceiptID, LeaseID: domain.StrPtr(proofLeaseID), TenantID: domain.StrPtr(proofTenantID),
		PropertyID: domain.StrPtr("property-1"), Category: domain.ReceiptCategoryRent,
		DueDate: "2026-09-10", Amount: 2500, CreatedAt: proofStamp,
	}))
	require.NoError(t, store.Settings.Set(ctx, domain.SettingLandlordName, "Carlos Drummond"))
	require.NoError(t, store.Settings.Set(ctx, domain.SettingLandlordCPF, "123.456.789-00"))
	return fixture
}

// submit entrega o comprovante da Maria pelo painel.
func (f *proofFixture) submit(t *testing.T) *application.PaymentProofOutcome {
	t.Helper()
	return f.submitAs(t, proofTenantID)
}

func (f *proofFixture) submitAs(t *testing.T, tenantID string) *application.PaymentProofOutcome {
	t.Helper()
	outcome, err := application.SubmitPaymentProof(context.Background(), f.deps,
		application.AuditContext{Origin: domain.AuditOriginWeb},
		application.PaymentProofInput{
			Source:   domain.PaymentProofSourcePanel,
			TenantID: tenantID,
			FileName: "comprovante.jpg",
			Mime:     "image/jpeg",
			Bytes:    []byte("bytes-do-comprovante"),
		})
	require.NoError(t, err)
	require.NotEmpty(t, outcome.ProofID)
	return outcome
}

func (f *proofFixture) proof(t *testing.T, id string) *domain.PaymentProof {
	t.Helper()
	proof, err := f.store.PaymentProofs.ByID(context.Background(), id)
	require.NoError(t, err)
	return proof
}

func (f *proofFixture) receipt(t *testing.T, id string) *domain.Receipt {
	t.Helper()
	receipt, err := f.store.Receipts.ByID(context.Background(), id)
	require.NoError(t, err)
	return receipt
}

func (f *proofFixture) auditActions(t *testing.T) []string {
	t.Helper()
	entries, err := f.store.Audit.List(context.Background(), domain.AuditFilter{Limit: 100})
	require.NoError(t, err)
	actions := make([]string, 0, len(entries))
	for _, entry := range entries {
		actions = append(actions, entry.Action)
	}
	return actions
}

func (f *proofFixture) auditOrigins(t *testing.T, action string) []domain.AuditOrigin {
	t.Helper()
	entries, err := f.store.Audit.List(context.Background(), domain.AuditFilter{Limit: 100})
	require.NoError(t, err)
	origins := make([]domain.AuditOrigin, 0, len(entries))
	for _, entry := range entries {
		if entry.Action == action {
			origins = append(origins, entry.Origin)
		}
	}
	return origins
}

func countAction(actions []string, action string) int {
	total := 0
	for _, candidate := range actions {
		if candidate == action {
			total++
		}
	}
	return total
}

// ── Envio ──────────────────────────────────────────────────────────

func TestSubmitPaymentProofStoresFileAndEnqueuesReading(t *testing.T) {
	fixture := newProofFixture(t)

	outcome := fixture.submit(t)

	proof := fixture.proof(t, outcome.ProofID)
	assert.Equal(t, domain.PaymentProofStatusPending, proof.Status)
	assert.Equal(t, domain.PaymentDecisionPending, proof.Decision)
	assert.Equal(t, domain.PaymentProofSourcePanel, proof.Source)
	assert.Equal(t, "comprovante.jpg", domain.Str(proof.FileName))
	assert.Equal(t, "image/jpeg", domain.Str(proof.Mime))
	assert.Equal(t, proofTenantID, domain.Str(proof.TenantID))
	assert.Equal(t, []byte("bytes-do-comprovante"), fixture.files.files[domain.Str(proof.FilePath)])

	job, err := fixture.store.Jobs.ByID(context.Background(), outcome.JobID)
	require.NoError(t, err)
	assert.Equal(t, domain.JobKindPaymentProof, job.Kind)
	assert.Equal(t, domain.JobStatusQueued, job.Status)
	require.NotNil(t, job.Payload)
	assert.Contains(t, *job.Payload, outcome.ProofID)
	assert.Zero(t, fixture.extractor.calls, "o envio não pode esperar a leitura")
}

func TestSubmitPaymentProofRejectsEmptyAndUnsupportedFiles(t *testing.T) {
	fixture := newProofFixture(t)
	actx := application.AuditContext{Origin: domain.AuditOriginWeb}

	_, err := application.SubmitPaymentProof(context.Background(), fixture.deps, actx, application.PaymentProofInput{
		FileName: "vazio.jpg", Mime: "image/jpeg",
	})
	require.Error(t, err)
	assert.True(t, domain.IsValidation(err), "arquivo vazio é erro de validação: %v", err)

	_, err = application.SubmitPaymentProof(context.Background(), fixture.deps, actx, application.PaymentProofInput{
		FileName: "planilha.xlsx", Mime: "application/vnd.ms-excel", Bytes: []byte("x"),
	})
	require.Error(t, err)
	assert.True(t, domain.IsValidation(err), "formato não suportado é erro de validação: %v", err)
}

// ── Baixa automática ───────────────────────────────────────────────

func TestProcessPaymentProofPaysReceiptAndEmitsIt(t *testing.T) {
	fixture := newProofFixture(t)
	outcome := fixture.submit(t)

	job, err := fixture.store.Jobs.ByID(context.Background(), outcome.JobID)
	require.NoError(t, err)
	require.NoError(t, application.ProcessPaymentProofJob(context.Background(), fixture.deps, *job))

	proof := fixture.proof(t, outcome.ProofID)
	assert.Equal(t, domain.PaymentProofStatusAccepted, proof.Status)
	assert.Equal(t, domain.PaymentDecisionAuto, proof.Decision)
	assert.Equal(t, proofReceiptID, domain.Str(proof.ReceiptID))
	assert.Equal(t, "valor, destinatário e data conferem", domain.Str(proof.DecisionReason))
	require.NotNil(t, proof.Confidence)
	assert.InDelta(t, 0.95, *proof.Confidence, 0.0001)
	require.NotNil(t, proof.ExtractedAmount)
	assert.InDelta(t, 2500.0, *proof.ExtractedAmount, 0.0001)
	assert.Equal(t, "2026-09-09", domain.Str(proof.ExtractedDate))
	assert.Equal(t, `{"amount":2500}`, domain.Str(proof.RawResponse))
	assert.Equal(t, []byte("bytes-do-comprovante"), fixture.extractor.document.Bytes)
	assert.Equal(t, "image/jpeg", fixture.extractor.document.Mime)

	// A baixa é a do painel: o lançamento fica quitado no repositório.
	receipt := fixture.receipt(t, proofReceiptID)
	require.NotNil(t, receipt.PaidAt)
	assert.Equal(t, "2026-09-09T00:00", *receipt.PaidAt)
	require.NotNil(t, receipt.PaidAmount)
	assert.InDelta(t, 2500.0, *receipt.PaidAmount, 0.0001)

	actions := fixture.auditActions(t)
	assert.Equal(t, 1, countAction(actions, domain.ActionPaymentProofAccepted))
	assert.Equal(t, 1, countAction(actions, domain.ActionReceiptPay))
	assert.Equal(t, []domain.AuditOrigin{domain.AuditOriginJob},
		fixture.auditOrigins(t, domain.ActionPaymentProofAccepted))
}

func TestProcessPaymentProofDoesNotDecideTwice(t *testing.T) {
	fixture := newProofFixture(t)
	outcome := fixture.submit(t)
	_, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.NoError(t, err)

	_, err = application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.NoError(t, err)

	actions := fixture.auditActions(t)
	assert.Equal(t, 1, countAction(actions, domain.ActionPaymentProofAccepted))
	assert.Equal(t, 1, countAction(actions, domain.ActionReceiptPay))
}

// ── Revisão humana pela decisão determinística ─────────────────────

func TestProcessPaymentProofDivergentAmountGoesToReview(t *testing.T) {
	fixture := newProofFixture(t)
	fixture.extractor.extraction.Amount = 2000
	outcome := fixture.submit(t)

	result, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentProofStatusReview, result.Status)

	proof := fixture.proof(t, outcome.ProofID)
	assert.Equal(t, domain.PaymentProofStatusReview, proof.Status)
	assert.Equal(t, domain.PaymentDecisionPending, proof.Decision)
	assert.Equal(t, "valor do comprovante diferente do lançamento", domain.Str(proof.DecisionReason))
	assert.Equal(t, proofReceiptID, domain.Str(proof.ReceiptID), "o lançamento conferido fica gravado")
	assert.Nil(t, fixture.receipt(t, proofReceiptID).PaidAt, "revisão nunca dá baixa")
	assert.Equal(t, []domain.AuditOrigin{domain.AuditOriginJob},
		fixture.auditOrigins(t, domain.ActionPaymentProofReview))
}

func TestProcessPaymentProofWrongPayeeGoesToReview(t *testing.T) {
	fixture := newProofFixture(t)
	fixture.extractor.extraction.Payee = "João Lima"
	outcome := fixture.submit(t)

	result, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentProofStatusReview, result.Status)
	assert.Equal(t, "destinatário do comprovante não é o locador", result.Reason)
	assert.Nil(t, fixture.receipt(t, proofReceiptID).PaidAt)
}

func TestProcessPaymentProofFromOtherTenantFindsNoReceipt(t *testing.T) {
	fixture := newProofFixture(t)
	outcome := fixture.submitAs(t, "tenant-2")

	result, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentProofStatusReview, result.Status)
	assert.Equal(t, "nenhum lançamento em aberto para este titular", result.Reason)
	assert.Nil(t, fixture.receipt(t, proofReceiptID).PaidAt)
}

func TestProcessPaymentProofLowConfidenceGoesToReview(t *testing.T) {
	fixture := newProofFixture(t)
	fixture.extractor.extraction.Confidence = 0.5
	outcome := fixture.submit(t)

	result, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentProofStatusReview, result.Status)
	assert.Equal(t, "confiança da leitura abaixo do limite", result.Reason)
	assert.Nil(t, fixture.receipt(t, proofReceiptID).PaidAt)
}

func TestProcessPaymentProofOutsideWindowGoesToReview(t *testing.T) {
	fixture := newProofFixture(t)
	fixture.extractor.extraction.Date = "2026-06-01"
	outcome := fixture.submit(t)

	result, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentProofStatusReview, result.Status)
	assert.Equal(t, "data do comprovante fora da janela do lançamento", result.Reason)
	assert.Nil(t, fixture.receipt(t, proofReceiptID).PaidAt)
}

func TestProcessPaymentProofUnreadableFileGoesToReviewAndFailsTheJob(t *testing.T) {
	fixture := newProofFixture(t)
	fixture.extractor.err = errors.New("provedor devolveu resposta inválida")
	outcome := fixture.submit(t)

	_, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.Error(t, err, "falha de leitura é falha do trabalho (tenta de novo)")

	proof := fixture.proof(t, outcome.ProofID)
	assert.Equal(t, domain.PaymentProofStatusReview, proof.Status)
	assert.Equal(t, "não foi possível ler o comprovante", domain.Str(proof.DecisionReason))
	assert.Nil(t, fixture.receipt(t, proofReceiptID).PaidAt)
	assert.Equal(t, []domain.AuditOrigin{domain.AuditOriginJob},
		fixture.auditOrigins(t, domain.ActionPaymentProofReview))
}

func TestProcessPaymentProofWithoutExtractorGoesToReview(t *testing.T) {
	fixture := newProofFixture(t)
	fixture.deps.Proofs = nil
	outcome := fixture.submit(t)

	result, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.NoError(t, err, "instância sem IA não é erro: o comprovante vai para a revisão")
	assert.Equal(t, domain.PaymentProofStatusReview, result.Status)
	assert.Equal(t, "provedor de leitura não configurado", result.Reason)
}

func TestProcessPaymentProofMissingFileGoesToReview(t *testing.T) {
	fixture := newProofFixture(t)
	outcome := fixture.submit(t)
	proof := fixture.proof(t, outcome.ProofID)
	require.NoError(t, fixture.files.Delete(domain.Str(proof.FilePath)))

	_, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.Error(t, err)

	proof = fixture.proof(t, outcome.ProofID)
	assert.Equal(t, domain.PaymentProofStatusReview, proof.Status)
	assert.Equal(t, "arquivo do comprovante indisponível", domain.Str(proof.DecisionReason))
}
