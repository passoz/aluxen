package application_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// seedReportReceipts monta a carteira usada pelos testes de relatório e de
// consulta em linguagem natural: dois titulares, cinco lançamentos na
// competência (pago, parcial, dois a vencer e um vencido) e um fora dela.
func seedReportReceipts(t *testing.T, fixture *receiptsFixture) {
	t.Helper()
	ctx := context.Background()

	require.NoError(t, fixture.deps.Repos.Tenants.Create(ctx, &domain.Tenant{
		ID: "tenant-2", Name: "João Lima", Email: "joao@example.com",
		CPF: domain.StrPtr("987.654.321-00"), CreatedAt: receiptsStamp, UpdatedAt: receiptsStamp,
	}))
	require.NoError(t, fixture.deps.Repos.Properties.Create(ctx, &domain.Property{
		ID: "property-2", Name: "Casa 7", Address: "Rua B, 20", City: "Cabo Frio", State: "RJ",
		CreatedAt: receiptsStamp, UpdatedAt: receiptsStamp,
	}))

	receipts := []domain.Receipt{
		{
			ID: "receipt-1", TenantID: domain.StrPtr("tenant-1"), PropertyID: domain.StrPtr(fixture.propID),
			Category: domain.ReceiptCategoryRent, DueDate: "2026-09-10", Amount: 2500,
			PaidAt: domain.StrPtr("2026-09-09T10:00"), PaidAmount: domain.FloatPtr(2500), CreatedAt: receiptsStamp,
		},
		{
			ID: "receipt-2", TenantID: domain.StrPtr("tenant-1"), PropertyID: domain.StrPtr(fixture.propID),
			Category: domain.ReceiptCategoryRent, DueDate: "2026-09-15", Amount: 1000,
			PaidAt: domain.StrPtr("2026-09-16T09:00"), PaidAmount: domain.FloatPtr(400), CreatedAt: receiptsStamp,
		},
		{
			ID: "receipt-3", TenantID: domain.StrPtr("tenant-1"), PropertyID: domain.StrPtr(fixture.propID),
			Category: domain.ReceiptCategoryRent, DueDate: "2026-09-25", Amount: 180, CreatedAt: receiptsStamp,
		},
		{
			ID: "receipt-4", TenantID: domain.StrPtr("tenant-1"), PropertyID: domain.StrPtr(fixture.propID),
			Category: domain.ReceiptCategoryRent, DueDate: "2026-09-05", Amount: 300, CreatedAt: receiptsStamp,
		},
		{
			ID: "receipt-5", TenantID: domain.StrPtr("tenant-2"), PropertyID: domain.StrPtr("property-2"),
			Category: domain.ReceiptCategoryRent, DueDate: "2026-09-30", Amount: 500, CreatedAt: receiptsStamp,
		},
		{
			ID: "receipt-6", TenantID: domain.StrPtr("tenant-1"), PropertyID: domain.StrPtr(fixture.propID),
			Category: domain.ReceiptCategoryRent, DueDate: "2026-08-10", Amount: 999, CreatedAt: receiptsStamp,
		},
	}
	for index := range receipts {
		require.NoError(t, fixture.deps.Repos.Receipts.Create(ctx, &receipts[index]))
	}
}

func TestMonthlyReportAggregatesMonthReceipts(t *testing.T) {
	fixture := newReceiptsFixture(t, adjustmentToday)
	seedReportReceipts(t, fixture)

	report, err := application.MonthlyReport(context.Background(), fixture.deps, 2026, 9)
	require.NoError(t, err)

	assert.Equal(t, 2026, report.Year)
	assert.Equal(t, 9, report.Month)
	assert.Equal(t, "2026-09", report.MonthKey)
	assert.Equal(t, "setembro/2026", report.Label)
	assert.Equal(t, 5, report.Receipts, "o lançamento de agosto fica fora da competência")

	assert.Equal(t, 4480.0, report.Expected)
	assert.Equal(t, 2900.0, report.Received, "recebido soma a baixa integral e a parcial")
	assert.Equal(t, 680.0, report.Open, "em aberto é o que ainda está a vencer")
	assert.Equal(t, 300.0, report.Overdue)
	assert.Equal(t, 2, report.PaidCount)

	require.Len(t, report.Counts, 4)
	assert.Equal(t, application.ReportStatusCount{Status: "paid", Label: "Pago", Count: 1, Amount: 2500, AmountLabel: "R$ 2.500,00"}, report.Counts[0])
	assert.Equal(t, application.ReportStatusCount{Status: "partial", Label: "Parcial", Count: 1, Amount: 1000, AmountLabel: "R$ 1.000,00"}, report.Counts[1])
	assert.Equal(t, application.ReportStatusCount{Status: "due", Label: "A vencer", Count: 2, Amount: 680, AmountLabel: "R$ 680,00"}, report.Counts[2])
	assert.Equal(t, application.ReportStatusCount{Status: "overdue", Label: "Vencido", Count: 1, Amount: 300, AmountLabel: "R$ 300,00"}, report.Counts[3])

	require.Len(t, report.ByProperty, 2)
	assert.Equal(t, application.ReportGroup{
		ID: fixture.propID, Name: "Apto 12", Receipts: 4,
		Expected: 3980, Received: 2900, Open: 180, Overdue: 300,
	}, report.ByProperty[0], "o imóvel com maior esperado vem primeiro")
	assert.Equal(t, application.ReportGroup{
		ID: "property-2", Name: "Casa 7", Receipts: 1,
		Expected: 500, Open: 500,
	}, report.ByProperty[1])

	require.Len(t, report.ByTenant, 2)
	assert.Equal(t, application.ReportGroup{
		ID: "tenant-1", Name: "Maria Souza", Receipts: 4,
		Expected: 3980, Received: 2900, Open: 180, Overdue: 300,
	}, report.ByTenant[0])
	assert.Equal(t, application.ReportGroup{
		ID: "tenant-2", Name: "João Lima", Receipts: 1,
		Expected: 500, Open: 500,
	}, report.ByTenant[1])
}

func TestMonthlyReportOfMonthWithoutReceipts(t *testing.T) {
	fixture := newReceiptsFixture(t, adjustmentToday)
	seedReportReceipts(t, fixture)

	report, err := application.MonthlyReport(context.Background(), fixture.deps, 2026, 7)
	require.NoError(t, err)

	assert.Equal(t, "julho/2026", report.Label)
	assert.Zero(t, report.Receipts)
	assert.Zero(t, report.Expected)
	assert.Zero(t, report.Received)
	assert.Zero(t, report.Open)
	assert.Zero(t, report.Overdue)
	require.Len(t, report.Counts, 4, "as quatro situações aparecem mesmo zeradas")
	for _, count := range report.Counts {
		assert.Zero(t, count.Count)
		assert.Equal(t, "R$ 0,00", count.AmountLabel)
	}
	assert.Empty(t, report.ByProperty)
	assert.Empty(t, report.ByTenant)
}

func TestMonthlyReportRejectsInvalidMonth(t *testing.T) {
	fixture := newReceiptsFixture(t, adjustmentToday)

	for _, testCase := range []struct {
		name  string
		year  int
		month int
	}{
		{"mês acima da faixa", 2026, 13},
		{"mês zerado", 2026, 0},
		{"ano fora da faixa", 1900, 9},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := application.MonthlyReport(context.Background(), fixture.deps, testCase.year, testCase.month)
			requireAdjustmentInvalid(t, err, "Informe um ano e um mês válidos.")
		})
	}
}
