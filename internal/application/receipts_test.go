package application_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
	"github.com/passoz/aluxen/internal/infrastructure/db"
)

// ── Apoio ──────────────────────────────────────────────────────────
//
// Os testes rodam sobre o SQLite em memória (as mesmas queries do sistema),
// porque a idempotência da geração mensal depende do ON CONFLICT da chave de
// geração — um dublê de repositório não provaria isso.

type receiptsClock struct{ now time.Time }

func (c receiptsClock) Now() time.Time { return c.now }

type receiptsIDs struct{ next int }

func (g *receiptsIDs) New() string {
	g.next++
	return fmt.Sprintf("novo-%d", g.next)
}

const receiptsStamp = "2026-09-01 10:00:00"

// receiptsFixture é o ambiente dos testes de lançamento: banco migrado, um
// locatário, um imóvel e um relógio fixo.
type receiptsFixture struct {
	deps     application.Deps
	now      time.Time
	tenantID string
	propID   string
}

func newReceiptsFixture(t *testing.T, now time.Time) *receiptsFixture {
	t.Helper()
	ctx := context.Background()

	store, err := db.Open(ctx, ":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	fixture := &receiptsFixture{
		now:      now,
		tenantID: "tenant-1",
		propID:   "property-1",
		deps: application.Deps{
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
			Clock: receiptsClock{now: now},
			IDs:   &receiptsIDs{},
			Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
	}

	require.NoError(t, store.Tenants.Create(ctx, &domain.Tenant{
		ID: fixture.tenantID, Name: "Maria Souza", Email: "maria@example.com",
		CPF: domain.StrPtr("123.456.789-00"), CreatedAt: receiptsStamp, UpdatedAt: receiptsStamp,
	}))
	require.NoError(t, store.Properties.Create(ctx, &domain.Property{
		ID: fixture.propID, Name: "Apto 12", Address: "Rua A, 100", City: "São Paulo", State: "SP",
		CreatedAt: receiptsStamp, UpdatedAt: receiptsStamp,
	}))
	return fixture
}

// seedLease grava um contrato completando o que o teste não informar.
func (f *receiptsFixture) seedLease(t *testing.T, lease domain.Lease) domain.Lease {
	t.Helper()
	if lease.TenantID == "" {
		lease.TenantID = f.tenantID
	}
	if lease.PropertyID == "" {
		lease.PropertyID = f.propID
	}
	if lease.StartDate == "" {
		lease.StartDate = "2026-01-01"
	}
	if lease.EndDate == "" {
		lease.EndDate = "2026-12-31"
	}
	if lease.Status == "" {
		lease.Status = domain.LeaseStatusActive
	}
	if lease.RentAmount == 0 {
		lease.RentAmount = 2500
	}
	if lease.DueDay == 0 {
		lease.DueDay = 10
	}
	lease.CreatedAt = receiptsStamp
	lease.UpdatedAt = receiptsStamp
	require.NoError(t, f.deps.Repos.Leases.Create(context.Background(), &lease))
	return lease
}

func (f *receiptsFixture) receipt(t *testing.T, id string) *domain.Receipt {
	t.Helper()
	receipt, err := f.deps.Repos.Receipts.ByID(context.Background(), id)
	require.NoError(t, err)
	return receipt
}

func (f *receiptsFixture) allReceipts(t *testing.T) []domain.Receipt {
	t.Helper()
	receipts, err := f.deps.Repos.Receipts.List(context.Background())
	require.NoError(t, err)
	return receipts
}

func (f *receiptsFixture) auditFor(t *testing.T, action string) []domain.AuditLog {
	t.Helper()
	entries, err := f.deps.Repos.Audit.List(context.Background(), domain.AuditFilter{Action: action, Limit: 100})
	require.NoError(t, err)
	return entries
}

func (f *receiptsFixture) auditActions(t *testing.T) []string {
	t.Helper()
	entries, err := f.deps.Repos.Audit.List(context.Background(), domain.AuditFilter{Limit: 100})
	require.NoError(t, err)
	actions := make([]string, 0, len(entries))
	for _, entry := range entries {
		actions = append(actions, entry.Action)
	}
	return actions
}

// auditContext é o contexto de auditoria usado pelos testes (painel, sem IP).
func receiptsAuditContext() application.AuditContext {
	return application.AuditContext{Origin: domain.AuditOriginWeb}
}

func receiptsChanges(t *testing.T, entry domain.AuditLog) []domain.AuditChange {
	t.Helper()
	if entry.Changes == nil {
		return nil
	}
	var changes []domain.AuditChange
	require.NoError(t, json.Unmarshal([]byte(*entry.Changes), &changes))
	return changes
}

func receiptsMetadata(t *testing.T, entry domain.AuditLog) map[string]any {
	t.Helper()
	if entry.Metadata == nil {
		return nil
	}
	var metadata map[string]any
	require.NoError(t, json.Unmarshal([]byte(*entry.Metadata), &metadata))
	return metadata
}

func receiptsChangeFor(t *testing.T, changes []domain.AuditChange, field string) domain.AuditChange {
	t.Helper()
	for _, change := range changes {
		if change.Field == field {
			return change
		}
	}
	t.Fatalf("diff da trilha sem o campo %q: %+v", field, changes)
	return domain.AuditChange{}
}

func requireReceiptsInvalid(t *testing.T, err error, message string) {
	t.Helper()
	require.Error(t, err)
	assert.True(t, domain.IsValidation(err), "erro deveria ser de validação: %v", err)
	assert.Equal(t, message, domain.ValidationMessage(err))
}

// ── Lançamento ─────────────────────────────────────────────────────

func TestReceiptCreateDerivesLinksFromLease(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1"})

	receipt, err := application.ReceiptCreate(context.Background(), fixture.deps, receiptsAuditContext(), application.ReceiptInput{
		ID:       "receipt-1",
		LeaseID:  lease.ID,
		Category: "rent",
		DueDate:  "2026-09-25",
		Amount:   "2.500,00",
	})
	require.NoError(t, err)

	assert.Equal(t, "receipt-1", receipt.ID)
	assert.Equal(t, domain.ReceiptCategoryRent, receipt.Category)
	assert.Equal(t, lease.TenantID, domain.Str(receipt.TenantID), "inquilino deve vir do contrato")
	assert.Equal(t, lease.PropertyID, domain.Str(receipt.PropertyID), "imóvel deve vir do contrato")
	assert.Equal(t, 2500.0, receipt.Amount)
	assert.Nil(t, receipt.PaidAt)
	assert.Nil(t, receipt.PaidAmount)
	assert.Nil(t, receipt.Description)
	assert.Equal(t, format.FormatUTCStamp(fixture.now), receipt.CreatedAt)
	assert.Equal(t, domain.ReceiptStatusDue, domain.ReceiptStatusOf(*receipt, fixture.now))

	stored := fixture.receipt(t, "receipt-1")
	assert.Equal(t, "Maria Souza", stored.TenantName, "o rótulo da trilha vem do join")

	entries := fixture.auditFor(t, domain.ActionReceiptCreate)
	require.Len(t, entries, 1)
	assert.Equal(t, domain.EntityReceipt, entries[0].Entity)
	assert.Equal(t, "receipt-1", domain.Str(entries[0].EntityID))
	assert.Equal(t, "Maria Souza · Apto 12 · 09/2026", domain.Str(entries[0].EntityLabel))
	assert.Equal(t, "Pagamento lançado para Maria Souza · Apto 12 · 09/2026", entries[0].Summary)
	assert.Equal(t, "25/09/2026", domain.Str(receiptsChangeFor(t, receiptsChanges(t, entries[0]), "dueDate").To))
}

func TestReceiptCreateRequiresContractForRent(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))

	_, err := application.ReceiptCreate(context.Background(), fixture.deps, receiptsAuditContext(), application.ReceiptInput{
		Category: "rent",
		DueDate:  "2026-09-10",
		Amount:   "2500",
	})
	requireReceiptsInvalid(t, err, "Selecione um contrato para recebimento de aluguel.")
	assert.Empty(t, fixture.allReceipts(t))
	assert.Empty(t, fixture.auditActions(t))
}

func TestReceiptCreateRequiresDescriptionForExtraCharge(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))

	_, err := application.ReceiptCreate(context.Background(), fixture.deps, receiptsAuditContext(), application.ReceiptInput{
		Category: "extra",
		DueDate:  "2026-09-10",
		Amount:   "150",
	})
	requireReceiptsInvalid(t, err, "Informe a descrição para cobrança avulsa.")
	assert.Empty(t, fixture.allReceipts(t))
}

func TestReceiptCreateKeepsOptionalLinksForExtraCharge(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))

	receipt, err := application.ReceiptCreate(context.Background(), fixture.deps, receiptsAuditContext(), application.ReceiptInput{
		Category:    "extra",
		Description: "  Taxa de condomínio  ",
		TenantID:    fixture.tenantID,
		PropertyID:  fixture.propID,
		DueDate:     "2026-09-05",
		Amount:      "150.50",
	})
	require.NoError(t, err)

	assert.Equal(t, domain.ReceiptCategoryExtra, receipt.Category)
	assert.Equal(t, "Taxa de condomínio", domain.Str(receipt.Description))
	assert.Nil(t, receipt.LeaseID)
	assert.Equal(t, fixture.tenantID, domain.Str(receipt.TenantID))
	assert.Equal(t, fixture.propID, domain.Str(receipt.PropertyID))
	assert.Equal(t, 150.5, receipt.Amount)
}

func TestReceiptCreateFillsPaidAtWhenPaidAmountInformed(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1"})

	receipt, err := application.ReceiptCreate(context.Background(), fixture.deps, receiptsAuditContext(), application.ReceiptInput{
		LeaseID:    lease.ID,
		DueDate:    "2026-09-10",
		Amount:     "2500",
		PaidAmount: "1000",
	})
	require.NoError(t, err)

	require.NotNil(t, receipt.PaidAt)
	assert.Equal(t, format.FormatLocalDateTime(fixture.now), *receipt.PaidAt, "pagamento sem data assume agora")
	require.NotNil(t, receipt.PaidAmount)
	assert.Equal(t, 1000.0, *receipt.PaidAmount)
	assert.Equal(t, domain.ReceiptStatusPartial, domain.ReceiptStatusOf(*receipt, fixture.now))
}

func TestReceiptCreateAssumesFullAmountWhenPaidAtInformed(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1"})

	receipt, err := application.ReceiptCreate(context.Background(), fixture.deps, receiptsAuditContext(), application.ReceiptInput{
		LeaseID: lease.ID,
		DueDate: "2026-09-10",
		Amount:  "2500",
		PaidAt:  "2026-09-09T08:15",
	})
	require.NoError(t, err)

	require.NotNil(t, receipt.PaidAt)
	assert.Equal(t, "2026-09-09T08:15", *receipt.PaidAt, "data informada é preservada")
	require.NotNil(t, receipt.PaidAmount)
	assert.Equal(t, 2500.0, *receipt.PaidAmount, "pagamento sem valor assume o total")
	assert.Equal(t, domain.ReceiptStatusPaid, domain.ReceiptStatusOf(*receipt, fixture.now))
}

func TestReceiptCreateRequiresDueDateAndAmount(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1"})

	for _, input := range []application.ReceiptInput{
		{LeaseID: lease.ID, Amount: "2500"},
		{LeaseID: lease.ID, DueDate: "2026-09-10", Amount: "abc"},
	} {
		_, err := application.ReceiptCreate(context.Background(), fixture.deps, receiptsAuditContext(), input)
		requireReceiptsInvalid(t, err, "Preencha vencimento e valor.")
	}
	assert.Empty(t, fixture.allReceipts(t))
}

// ── Alteração ──────────────────────────────────────────────────────

func TestReceiptUpdateKeepsIdentityAndGenerationKey(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1", DueDay: 10})

	inserted, err := application.ReceiptGenerateMonthly(context.Background(), fixture.deps, receiptsAuditContext())
	require.NoError(t, err)
	require.Equal(t, 1, inserted)
	generated := fixture.allReceipts(t)[0]
	require.Equal(t, "lease-1:2026-09", domain.Str(generated.GeneratedKey))

	updated, err := application.ReceiptUpdate(context.Background(), fixture.deps, receiptsAuditContext(), application.ReceiptInput{
		ID: generated.ID, LeaseID: lease.ID, DueDate: "2026-09-15", Amount: "2600",
	})
	require.NoError(t, err)

	assert.Equal(t, generated.ID, updated.ID, "a alteração não troca o identificador")
	assert.Equal(t, generated.CreatedAt, updated.CreatedAt, "a criação é preservada")
	assert.Equal(t, "2026-09-15", updated.DueDate)
	assert.Equal(t, 2600.0, updated.Amount)
	assert.Equal(t, "lease-1:2026-09", domain.Str(updated.GeneratedKey), "chave da geração é preservada")
	require.NotNil(t, updated.UpdatedAt)
	assert.Equal(t, format.FormatUTCStamp(fixture.now), *updated.UpdatedAt)

	// Com a chave preservada, gerar o mês de novo continua não duplicando.
	again, err := application.ReceiptGenerateMonthly(context.Background(), fixture.deps, receiptsAuditContext())
	require.NoError(t, err)
	assert.Equal(t, 0, again)
	assert.Len(t, fixture.allReceipts(t), 1)
}

func TestReceiptUpdateRecordsFieldDiff(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1"})
	_, err := application.ReceiptCreate(context.Background(), fixture.deps, receiptsAuditContext(), application.ReceiptInput{
		ID: "receipt-1", LeaseID: lease.ID, DueDate: "2026-09-10", Amount: "2500",
	})
	require.NoError(t, err)

	_, err = application.ReceiptUpdate(context.Background(), fixture.deps, receiptsAuditContext(), application.ReceiptInput{
		ID: "receipt-1", LeaseID: lease.ID, DueDate: "2026-09-15", Amount: "2600",
	})
	require.NoError(t, err)

	entries := fixture.auditFor(t, domain.ActionReceiptUpdate)
	require.Len(t, entries, 1)
	assert.Equal(t, "Pagamento de Maria Souza · Apto 12 · 09/2026 alterado", entries[0].Summary)

	changes := receiptsChanges(t, entries[0])
	dueDate := receiptsChangeFor(t, changes, "dueDate")
	assert.Equal(t, "Vencimento", dueDate.Label)
	assert.Equal(t, "10/09/2026", domain.Str(dueDate.From))
	assert.Equal(t, "15/09/2026", domain.Str(dueDate.To))
	amount := receiptsChangeFor(t, changes, "amount")
	assert.Equal(t, "R$ 2.500,00", domain.Str(amount.From))
	assert.Equal(t, "R$ 2.600,00", domain.Str(amount.To))
}

// ── Baixa ──────────────────────────────────────────────────────────

func TestReceiptPayDefaultsToFullAmountAndNow(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1"})
	_, err := application.ReceiptCreate(context.Background(), fixture.deps, receiptsAuditContext(), application.ReceiptInput{
		ID: "receipt-1", LeaseID: lease.ID, DueDate: "2026-09-10", Amount: "2500",
	})
	require.NoError(t, err)

	paid, err := application.ReceiptPay(context.Background(), fixture.deps, receiptsAuditContext(), application.ReceiptPayInput{ID: "receipt-1"})
	require.NoError(t, err)

	require.NotNil(t, paid.PaidAmount)
	assert.Equal(t, 2500.0, *paid.PaidAmount)
	require.NotNil(t, paid.PaidAt)
	assert.Equal(t, format.FormatLocalDateTime(fixture.now), *paid.PaidAt)
	assert.Equal(t, domain.ReceiptStatusPaid, domain.ReceiptStatusOf(*paid, fixture.now))

	entries := fixture.auditFor(t, domain.ActionReceiptPay)
	require.Len(t, entries, 1)
	assert.Equal(t, "Baixa de Maria Souza · Apto 12 · 09/2026 registrada", entries[0].Summary)
	receiptsChangeFor(t, receiptsChanges(t, entries[0]), "paidAmount")
}

func TestReceiptPayRegistersPartialAmountAndInformedDate(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1"})
	_, err := application.ReceiptCreate(context.Background(), fixture.deps, receiptsAuditContext(), application.ReceiptInput{
		ID: "receipt-1", LeaseID: lease.ID, DueDate: "2026-09-10", Amount: "2500",
	})
	require.NoError(t, err)

	paid, err := application.ReceiptPay(context.Background(), fixture.deps, receiptsAuditContext(), application.ReceiptPayInput{
		ID: "receipt-1", PayAmount: "1.000,00", PaidAt: "2026-09-12T09:30",
	})
	require.NoError(t, err)

	require.NotNil(t, paid.PaidAmount)
	assert.Equal(t, 1000.0, *paid.PaidAmount)
	require.NotNil(t, paid.PaidAt)
	assert.Equal(t, "2026-09-12T09:30", *paid.PaidAt)
	assert.Equal(t, domain.ReceiptStatusPartial, domain.ReceiptStatusOf(*paid, fixture.now))
}

func TestReceiptPayRejectsUnknownReceipt(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))

	_, err := application.ReceiptPay(context.Background(), fixture.deps, receiptsAuditContext(), application.ReceiptPayInput{ID: "nao-existe"})
	require.ErrorIs(t, err, domain.ErrNotFound)
}

// ── Lote ───────────────────────────────────────────────────────────

func TestReceiptBatchPayMarksEveryReceiptWithTheSameTimestamp(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1"})
	for _, input := range []application.ReceiptInput{
		{ID: "receipt-1", LeaseID: lease.ID, DueDate: "2026-09-10", Amount: "2500"},
		{ID: "receipt-2", LeaseID: lease.ID, DueDate: "2026-10-10", Amount: "2500"},
	} {
		_, err := application.ReceiptCreate(context.Background(), fixture.deps, receiptsAuditContext(), input)
		require.NoError(t, err)
	}

	require.NoError(t, application.ReceiptBatchPay(context.Background(), fixture.deps, receiptsAuditContext(), []string{"receipt-1", "receipt-2"}))

	first := fixture.receipt(t, "receipt-1")
	second := fixture.receipt(t, "receipt-2")
	require.NotNil(t, first.PaidAt)
	require.NotNil(t, second.PaidAt)
	assert.Equal(t, *first.PaidAt, *second.PaidAt, "o lote usa o mesmo instante de pagamento")
	assert.Equal(t, format.FormatLocalDateTime(fixture.now), *first.PaidAt)
	require.NotNil(t, first.PaidAmount)
	require.NotNil(t, second.PaidAmount)
	assert.Equal(t, 2500.0, *first.PaidAmount)
	assert.Equal(t, 2500.0, *second.PaidAmount)

	entries := fixture.auditFor(t, domain.ActionReceiptBatchPay)
	require.Len(t, entries, 2, "uma linha de trilha por lançamento do lote")
	for _, entry := range entries {
		assert.Equal(t, 2.0, receiptsMetadata(t, entry)["batch"])
		assert.Contains(t, entry.Summary, "Baixa em lote de")
	}
}

func TestReceiptBatchPayRejectsEmptySelection(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))

	err := application.ReceiptBatchPay(context.Background(), fixture.deps, receiptsAuditContext(), []string{"", "  "})
	requireReceiptsInvalid(t, err, "Nenhum recibo selecionado.")
}

func TestReceiptBatchDeleteRemovesEverySelectedReceipt(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1"})
	for _, input := range []application.ReceiptInput{
		{ID: "receipt-1", LeaseID: lease.ID, DueDate: "2026-09-10", Amount: "2500"},
		{ID: "receipt-2", LeaseID: lease.ID, DueDate: "2026-10-10", Amount: "2500"},
		{ID: "receipt-3", LeaseID: lease.ID, DueDate: "2026-11-10", Amount: "2500"},
	} {
		_, err := application.ReceiptCreate(context.Background(), fixture.deps, receiptsAuditContext(), input)
		require.NoError(t, err)
	}

	require.NoError(t, application.ReceiptBatchDelete(context.Background(), fixture.deps, receiptsAuditContext(), []string{"receipt-1", "receipt-3"}))

	remaining := fixture.allReceipts(t)
	require.Len(t, remaining, 1)
	assert.Equal(t, "receipt-2", remaining[0].ID)

	entries := fixture.auditFor(t, domain.ActionReceiptBatchDelete)
	require.Len(t, entries, 2)
	for _, entry := range entries {
		assert.Equal(t, 2.0, receiptsMetadata(t, entry)["batch"])
		assert.Contains(t, entry.Summary, "excluído em lote")
	}
}

func TestReceiptBatchDeleteRejectsEmptySelection(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))

	err := application.ReceiptBatchDelete(context.Background(), fixture.deps, receiptsAuditContext(), nil)
	requireReceiptsInvalid(t, err, "Nenhum recibo selecionado.")
}

func TestReceiptDeleteRemovesReceiptAndRecordsAudit(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1"})
	_, err := application.ReceiptCreate(context.Background(), fixture.deps, receiptsAuditContext(), application.ReceiptInput{
		ID: "receipt-1", LeaseID: lease.ID, DueDate: "2026-09-10", Amount: "2500",
	})
	require.NoError(t, err)

	require.NoError(t, application.ReceiptDelete(context.Background(), fixture.deps, receiptsAuditContext(), "receipt-1"))
	assert.Empty(t, fixture.allReceipts(t))

	entries := fixture.auditFor(t, domain.ActionReceiptDelete)
	require.Len(t, entries, 1)
	assert.Equal(t, "Pagamento de Maria Souza · Apto 12 · 09/2026 excluído", entries[0].Summary)
	assert.Nil(t, entries[0].Metadata)
}

// ── Geração mensal ─────────────────────────────────────────────────

func TestReceiptGenerateMonthlyCreatesOneReceiptPerActiveLease(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))
	fixture.seedLease(t, domain.Lease{ID: "lease-dia-10", DueDay: 10})
	fixture.seedLease(t, domain.Lease{ID: "lease-dia-31", DueDay: 31, RentAmount: 3200})
	fixture.seedLease(t, domain.Lease{ID: "lease-suspenso", Status: domain.LeaseStatusSuspended})
	fixture.seedLease(t, domain.Lease{ID: "lease-fora-do-mes", StartDate: "2026-10-01", EndDate: "2026-12-31"})

	inserted, err := application.ReceiptGenerateMonthly(context.Background(), fixture.deps, receiptsAuditContext())
	require.NoError(t, err)
	assert.Equal(t, 2, inserted, "só contratos ativos vigentes no mês geram lançamento")

	generated := map[string]domain.Receipt{}
	for _, receipt := range fixture.allReceipts(t) {
		generated[domain.Str(receipt.GeneratedKey)] = receipt
	}
	require.Len(t, generated, 2)

	first := generated["lease-dia-10:2026-09"]
	assert.Equal(t, "2026-09-10", first.DueDate)
	assert.Equal(t, 2500.0, first.Amount)
	assert.Equal(t, domain.ReceiptCategoryRent, first.Category)
	assert.Equal(t, fixture.tenantID, domain.Str(first.TenantID))
	assert.Equal(t, fixture.propID, domain.Str(first.PropertyID))
	assert.Nil(t, first.PaidAt)
	assert.Nil(t, first.Description)

	second := generated["lease-dia-31:2026-09"]
	assert.Equal(t, "2026-09-30", second.DueDate, "dia 31 é limitado ao último dia do mês")
	assert.Equal(t, 3200.0, second.Amount)

	entries := fixture.auditFor(t, domain.ActionReceiptGenerateMonth)
	require.Len(t, entries, 1)
	assert.Equal(t, "2026-09", domain.Str(entries[0].EntityID))
	assert.Equal(t, "Competência 09/2026", domain.Str(entries[0].EntityLabel))
	assert.Equal(t, "Geração automática de 2 pagamento(s) para 09/2026", entries[0].Summary)
	metadata := receiptsMetadata(t, entries[0])
	assert.Equal(t, "2026-09", metadata["month"])
	assert.Equal(t, 2.0, metadata["count"])
}

func TestReceiptGenerateMonthlyIsIdempotent(t *testing.T) {
	fixture := newReceiptsFixture(t, time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local))
	fixture.seedLease(t, domain.Lease{ID: "lease-1", DueDay: 10})

	first, err := application.ReceiptGenerateMonthly(context.Background(), fixture.deps, receiptsAuditContext())
	require.NoError(t, err)
	assert.Equal(t, 1, first)

	second, err := application.ReceiptGenerateMonthly(context.Background(), fixture.deps, receiptsAuditContext())
	require.NoError(t, err)
	assert.Equal(t, 0, second, "repetir a geração não duplica lançamento")
	assert.Len(t, fixture.allReceipts(t), 1)

	entries := fixture.auditFor(t, domain.ActionReceiptGenerateMonth)
	require.Len(t, entries, 2)
	counts := []float64{receiptsMetadata(t, entries[0])["count"].(float64), receiptsMetadata(t, entries[1])["count"].(float64)}
	sort.Float64s(counts)
	assert.Equal(t, []float64{0, 1}, counts, "a segunda geração registra count 0")
}
