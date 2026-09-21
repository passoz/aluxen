package application

import (
	"context"
	"errors"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/stretchr/testify/require"
)

// Indicadores do painel e o instantâneo que o painel carrega em uma passada.
//
// O "hoje" é 21/09/2026: os lançamentos são montados em torno dessa data para
// que cada situação (pago, parcial, a vencer, vencido) seja provada.

func covMetricsEnv(t *testing.T) *covEnv {
	t.Helper()
	env := newCovEnv(t)
	env.tenants.items = []domain.Tenant{
		covTenantOf("t1", "Ana"),
		covTenantOf("t2", "Bia"),
		covTenantOf("t5", "Eva"),
	}
	env.props.items = []domain.Property{covPropertyOf("p1", "Casa"), covPropertyOf("p2", "Loja")}
	env.leases.items = []domain.Lease{
		covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 1000),
		covLeaseOf("l2", "t2", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 2000),
		covLeaseOf("l3", "t1", "p2", domain.LeaseStatusTerminated, "2025-01-01", "2025-12-31", 500),
		covLeaseOf("l4", "t2", "p2", domain.LeaseStatusSuspended, "2026-01-01", "2026-12-31", 700),
	}

	paid := covPaidReceipt(covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000), "2026-09-05T09:00", 1000)
	partial := covReceiptOf("r2", "t2", "l1", "2026-09-15", 1000)
	partial.PaidAt = domain.StrPtr("2026-09-15T09:00")
	partial.PaidAmount = domain.FloatPtr(400)
	due := covReceiptOf("r3", "t1", "l1", "2026-10-10", 300)
	overdue := covReceiptOf("r4", "t2", "l1", "2026-08-10", 200)
	// Baixa antiga sem valor explícito: o valor pago é o total do lançamento.
	oldPaid := covReceiptOf("r5", "t5", "l2", "2026-06-05", 500)
	oldPaid.PaidAt = domain.StrPtr("2026-06-05T10:00")

	// Vencimento com barra: a competência continua sendo setembro/2026.
	slash := covReceiptOf("r6", "t6", "l2", "2026/09/20", 100)
	slash.Category = domain.ReceiptCategoryExtra
	// Lançamento avulso, sem titular e sem categoria (cai em aluguel).
	orphan := covReceiptOf("r7", "", "", "2026-09-01", 50)
	orphan.TenantID = nil
	orphan.Category = ""

	env.receipts.items = []domain.Receipt{
		paid, partial, due, overdue, oldPaid, slash, orphan,
		covPaidReceipt(covReceiptOf("r8", "t3", "l2", "2026-07-01", 60), "2026-07-01T08:00", 60),
		covPaidReceipt(covReceiptOf("r9", "t4", "l2", "2026-07-02", 70), "2026-07-02T08:00", 70),
		covPaidReceipt(covReceiptOf("r10", "t6", "l2", "2026-07-03", 80), "2026-07-03T08:00", 80),
	}
	return env
}

func TestCovComputeMetricsTotals(t *testing.T) {
	env := covMetricsEnv(t)

	payload, err := ComputeMetrics(context.Background(), env.deps)
	require.NoError(t, err)

	totals := payload.Totals
	require.Equal(t, 3, totals.Tenants)
	require.Equal(t, 2, totals.Properties)
	require.Equal(t, 10, totals.Receipts)
	require.Equal(t, 2, totals.ActiveLeases)
	require.Equal(t, 1, totals.TerminatedLeases)
	require.Equal(t, 3000.0, totals.ExpectedMonthly, "só contratos ativos entram na expectativa mensal")
	require.Equal(t, 3360.0, totals.TotalAmount)
	require.Equal(t, 5, totals.PaidReceipts)
	require.Equal(t, 1, totals.PartialReceipts)
	require.Equal(t, 2, totals.DueReceipts, "vencimento fora do formato ISO cai em 'a vencer'")
	require.Equal(t, 2, totals.OverdueReceipts)
	require.Equal(t, 400.0, totals.OpenAmount, "só o que está a vencer entra em aberto")
	require.Equal(t, 250.0, totals.OverdueAmount)
	require.Equal(t, 2110.0, totals.PaidAmount, "parcials contam pelo valor efetivamente pago")
}

func TestCovComputeMetricsStatusSlices(t *testing.T) {
	env := covMetricsEnv(t)

	payload, err := ComputeMetrics(context.Background(), env.deps)
	require.NoError(t, err)
	require.Equal(t, []MetricsStatus{
		{Status: "paid", Label: "Pago", Count: 5},
		{Status: "partial", Label: "Parcial", Count: 1},
		{Status: "due", Label: "A vencer", Count: 2},
		{Status: "overdue", Label: "Vencido", Count: 2},
	}, payload.Status)
}

func TestCovComputeMetricsMonthlyWindow(t *testing.T) {
	env := covMetricsEnv(t)

	payload, err := ComputeMetrics(context.Background(), env.deps)
	require.NoError(t, err)
	require.Len(t, payload.Monthly, 6, "a janela do gráfico tem seis meses")

	byKey := map[string]MetricsMonth{}
	for _, month := range payload.Monthly {
		byKey[month.Key] = month
	}
	require.Equal(t, "jun/26", byKey["2026-06"].Label)
	require.Equal(t, MetricsMonth{Key: "2026-06", Label: "jun/26", Received: 500, Expected: 500}, byKey["2026-06"])
	require.Equal(t, MetricsMonth{Key: "2026-07", Label: "jul/26", Received: 210, Expected: 210}, byKey["2026-07"])
	require.Equal(t, MetricsMonth{Key: "2026-08", Label: "ago/26", Open: 200, Expected: 200}, byKey["2026-08"])
	require.Equal(t, MetricsMonth{Key: "2026-09", Label: "set/26", Received: 1400, Open: 150, Expected: 2150}, byKey["2026-09"])
	require.Equal(t, MetricsMonth{Key: "2026-10", Label: "out/26", Open: 300, Expected: 300}, byKey["2026-10"])
	require.Equal(t, MetricsMonth{Key: "2026-11", Label: "nov/26"}, byKey["2026-11"])
	// A ordem das colunas vai do mês mais antigo para o mais novo.
	require.Equal(t, "2026-06", payload.Monthly[0].Key)
	require.Equal(t, "2026-11", payload.Monthly[5].Key)
}

func TestCovComputeMetricsCategories(t *testing.T) {
	env := covMetricsEnv(t)

	payload, err := ComputeMetrics(context.Background(), env.deps)
	require.NoError(t, err)
	require.Equal(t, []MetricsCategory{
		{Category: "rent", Label: "Aluguel", Count: 9, Total: 3260},
		{Category: "extra", Label: "Avulsa", Count: 1, Total: 100},
	}, payload.Categories, "categoria vazia conta como aluguel e a ordem é pelo total")
}

func TestCovComputeMetricsPropertiesAndTopTenants(t *testing.T) {
	env := covMetricsEnv(t)

	payload, err := ComputeMetrics(context.Background(), env.deps)
	require.NoError(t, err)
	require.Equal(t, []MetricsProperty{
		{ID: "p1", Name: "Casa", ActiveLeases: 2},
		{ID: "p2", Name: "Loja", ActiveLeases: 0},
	}, payload.Properties)

	require.Len(t, payload.TopTenants, 5, "o ranking mostra no máximo cinco titulares")
	require.Equal(t, []string{"t1", "t5", "t2", "t6", "t4"}, []string{
		payload.TopTenants[0].ID, payload.TopTenants[1].ID, payload.TopTenants[2].ID,
		payload.TopTenants[3].ID, payload.TopTenants[4].ID,
	})
	require.Equal(t, 1000.0, payload.TopTenants[0].PaidTotal)
	require.Equal(t, "Ana", payload.TopTenants[0].Name)
	require.Equal(t, 1, payload.TopTenants[0].Receipts)
	require.Equal(t, "Inquilino", payload.TopTenants[3].Name, "titular fora do cadastro aparece como Inquilino")
	require.Equal(t, 80.0, payload.TopTenants[3].PaidTotal)
}

func TestCovComputeMetricsEmptyDatabase(t *testing.T) {
	env := newCovEnv(t)

	payload, err := ComputeMetrics(context.Background(), env.deps)
	require.NoError(t, err)
	require.Equal(t, MetricsTotals{}, payload.Totals)
	require.Empty(t, payload.Categories)
	require.Empty(t, payload.Properties)
	require.Empty(t, payload.TopTenants)
	require.Len(t, payload.Monthly, 6)
	require.Len(t, payload.Status, 4)
}

func TestCovComputeMetricsPropagatesFailure(t *testing.T) {
	failure := errors.New("leitura falhou")
	cases := map[string]func(*covEnv){
		"locatários":  func(env *covEnv) { env.tenants.listErr = failure },
		"imóveis":     func(env *covEnv) { env.props.listErr = failure },
		"contratos":   func(env *covEnv) { env.leases.listErr = failure },
		"lançamentos": func(env *covEnv) { env.receipts.listErr = failure },
	}
	for label, breakIt := range cases {
		t.Run(label, func(t *testing.T) {
			env := covMetricsEnv(t)
			breakIt(env)

			payload, err := ComputeMetrics(context.Background(), env.deps)
			require.ErrorIs(t, err, failure)
			require.Nil(t, payload)
		})
	}
}

func TestCovPaidAmountOfAndDueMonthKey(t *testing.T) {
	require.Equal(t, 0.0, paidAmountOf(covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)))

	withPaidAt := covReceiptOf("r2", "t1", "l1", "2026-09-10", 1000)
	withPaidAt.PaidAt = domain.StrPtr("2026-09-09")
	require.Equal(t, 1000.0, paidAmountOf(withPaidAt), "sem valor explícito a baixa vale o total")

	partial := withPaidAt
	partial.PaidAmount = domain.FloatPtr(350)
	require.Equal(t, 350.0, paidAmountOf(partial), "o valor explícito manda sobre o total")

	require.Equal(t, "2026-09", dueMonthKey("2026-09-10"))
	require.Equal(t, "2026-09", dueMonthKey("2026/09/10"))
	require.Equal(t, "", dueMonthKey("2026"))
}

// ── queries.go ────────────────────────────────────────────────────

func TestCovListQueries(t *testing.T) {
	env := covMetricsEnv(t)
	ctx := context.Background()

	tenants, err := ListTenants(ctx, env.deps)
	require.NoError(t, err)
	require.Len(t, tenants, 3)

	properties, err := ListProperties(ctx, env.deps)
	require.NoError(t, err)
	require.Len(t, properties, 2)

	leases, err := ListLeases(ctx, env.deps)
	require.NoError(t, err)
	require.Len(t, leases, 4)

	receipts, err := ListReceipts(ctx, env.deps)
	require.NoError(t, err)
	require.Len(t, receipts, 10)
}

func TestCovListQueriesPropagateFailure(t *testing.T) {
	failure := errors.New("leitura falhou")
	cases := map[string]struct {
		breakIt func(*covEnv)
		call    func(context.Context, Deps) error
	}{
		"locatários": {func(env *covEnv) { env.tenants.listErr = failure }, func(ctx context.Context, deps Deps) error {
			_, err := ListTenants(ctx, deps)
			return err
		}},
		"imóveis": {func(env *covEnv) { env.props.listErr = failure }, func(ctx context.Context, deps Deps) error {
			_, err := ListProperties(ctx, deps)
			return err
		}},
		"contratos": {func(env *covEnv) { env.leases.listErr = failure }, func(ctx context.Context, deps Deps) error {
			_, err := ListLeases(ctx, deps)
			return err
		}},
		"lançamentos": {func(env *covEnv) { env.receipts.listErr = failure }, func(ctx context.Context, deps Deps) error {
			_, err := ListReceipts(ctx, deps)
			return err
		}},
	}
	for label, scenario := range cases {
		t.Run(label, func(t *testing.T) {
			env := covMetricsEnv(t)
			scenario.breakIt(env)
			require.ErrorIs(t, scenario.call(context.Background(), env.deps), failure)
		})
	}
}

func TestCovDashboardSnapshot(t *testing.T) {
	env := covMetricsEnv(t)

	data, err := DashboardSnapshot(context.Background(), env.deps)
	require.NoError(t, err)
	require.Len(t, data.Tenants, 3)
	require.Len(t, data.Properties, 2)
	require.Len(t, data.Leases, 4)
	require.Len(t, data.Receipts, 10)
	require.Equal(t, 3360.0, data.Metrics.Totals.TotalAmount)
	require.Equal(t, 2, data.Metrics.Totals.ActiveLeases)
}

func TestCovDashboardSnapshotPropagatesFailure(t *testing.T) {
	failure := errors.New("leitura falhou")
	cases := map[string]func(*covEnv){
		"locatários":  func(env *covEnv) { env.tenants.listErr = failure },
		"imóveis":     func(env *covEnv) { env.props.listErr = failure },
		"contratos":   func(env *covEnv) { env.leases.listErr = failure },
		"lançamentos": func(env *covEnv) { env.receipts.listErr = failure },
	}
	for label, breakIt := range cases {
		t.Run(label, func(t *testing.T) {
			env := covMetricsEnv(t)
			breakIt(env)

			data, err := DashboardSnapshot(context.Background(), env.deps)
			require.ErrorIs(t, err, failure)
			require.Nil(t, data)
		})
	}
}
