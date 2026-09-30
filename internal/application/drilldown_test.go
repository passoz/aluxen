package application

import (
	"context"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
)

// Detalhamento filtrado do painel (Work 0007, task 7.4): o recorte de
// lançamentos por período, situação, categoria, inquilino, imóvel e descrição.
//
// O "hoje" do ambiente é 21/09/2026: r1 e r5 estão pagos, r2 parcial, r3 e r6
// a vencer e r4 vencido.

func drillEnv(t *testing.T) *covEnv {
	t.Helper()
	env := newCovEnv(t)
	env.tenants.items = []domain.Tenant{
		covTenantOf("t1", "Ana"),
		covTenantOf("t2", "Bia"),
	}
	env.props.items = []domain.Property{
		covPropertyOf("p1", "Casa"),
		covPropertyOf("p2", "Loja"),
	}
	env.leases.items = []domain.Lease{
		covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 1000),
		covLeaseOf("l2", "t2", "p2", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 2000),
	}

	r1 := covPaidReceipt(covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000), "2026-09-05T09:00", 1000)
	r2 := covReceiptOf("r2", "t2", "l2", "2026-09-15", 2000)
	r2.PaidAt = domain.StrPtr("2026-09-15T09:00")
	r2.PaidAmount = domain.FloatPtr(800)
	r3 := covReceiptOf("r3", "t1", "l1", "2026-10-10", 300)
	r4 := covReceiptOf("r4", "t2", "l2", "2026-08-10", 200)
	r5 := covPaidReceipt(covReceiptOf("r5", "t1", "l1", "2026-09-20", 100), "2026-09-20T10:00", 100)
	r5.Category = domain.ReceiptCategoryExtra
	r6 := covReceiptOf("r6", "t2", "", "2026-09-25", 50)
	r6.LeaseID = nil
	r6.Category = domain.ReceiptCategoryExtra
	r6.Description = domain.StrPtr("Limpeza pós-obra")

	env.receipts.items = []domain.Receipt{r1, r2, r3, r4, r5, r6}
	return env
}

func drillIDs(result *DrilldownResult) []string {
	ids := make([]string, 0, len(result.Rows))
	for _, row := range result.Rows {
		ids = append(ids, row.ID)
	}
	return ids
}

func drillEqualIDs(t *testing.T, result *DrilldownResult, want []string) {
	t.Helper()
	if len(result.Rows) != len(want) {
		t.Fatalf("drill: linhas = %v, esperadas %v", drillIDs(result), want)
	}
	for index, id := range want {
		if result.Rows[index].ID != id {
			t.Fatalf("drill: linhas = %v, esperadas %v", drillIDs(result), want)
		}
	}
}

func TestDrillFiltersByStatus(t *testing.T) {
	env := drillEnv(t)

	result, err := DrilldownReceipts(context.Background(), env.deps, DrilldownFilter{Status: "overdue"})
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	drillEqualIDs(t, result, []string{"r4"})
	if result.Totals.Count != 1 || result.Totals.Overdue != 200 {
		t.Fatalf("drill: totais = %+v, esperados count 1 e overdue 200", result.Totals)
	}
}

func TestDrillFiltersByPeriod(t *testing.T) {
	env := drillEnv(t)

	result, err := DrilldownReceipts(context.Background(), env.deps, DrilldownFilter{From: "2026-09-01", To: "2026-09-30"})
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	drillEqualIDs(t, result, []string{"r1", "r2", "r5", "r6"})
}

func TestDrillFiltersByCategory(t *testing.T) {
	env := drillEnv(t)

	result, err := DrilldownReceipts(context.Background(), env.deps, DrilldownFilter{Category: "extra"})
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	drillEqualIDs(t, result, []string{"r5", "r6"})
}

func TestDrillFiltersByTenant(t *testing.T) {
	env := drillEnv(t)

	// O titular do aluguel vem do contrato: r1 e r3 guardam só o lease l1.
	result, err := DrilldownReceipts(context.Background(), env.deps, DrilldownFilter{TenantID: "t1"})
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	drillEqualIDs(t, result, []string{"r1", "r5", "r3"})
}

func TestDrillFiltersByProperty(t *testing.T) {
	env := drillEnv(t)

	// O imóvel do aluguel vem do contrato: r2 e r4 guardam só o lease l2.
	result, err := DrilldownReceipts(context.Background(), env.deps, DrilldownFilter{PropertyID: "p2"})
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	drillEqualIDs(t, result, []string{"r4", "r2"})
}

func TestDrillSearchesDescription(t *testing.T) {
	env := drillEnv(t)

	result, err := DrilldownReceipts(context.Background(), env.deps, DrilldownFilter{Query: "LIMPEZA"})
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	drillEqualIDs(t, result, []string{"r6"})
}

func TestDrillIgnoresUnknownFilters(t *testing.T) {
	env := drillEnv(t)

	// Filtro desconhecido ou fora de faixa não derruba o detalhamento: o
	// recorte ignora o filtro inválido em vez de devolver erro.
	result, err := DrilldownReceipts(context.Background(), env.deps, DrilldownFilter{
		From:     "quando",
		To:       "2026-13-99",
		Status:   "quitado",
		Category: "permuta",
	})
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	if len(result.Rows) != 6 {
		t.Fatalf("drill: linhas = %d, esperadas 6 (filtros inválidos ignorados)", len(result.Rows))
	}
}

func TestDrillTotalsMatchRows(t *testing.T) {
	env := drillEnv(t)

	result, err := DrilldownReceipts(context.Background(), env.deps, DrilldownFilter{})
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	totals := result.Totals
	if totals.Count != 6 {
		t.Fatalf("drill: count = %d, esperado 6", totals.Count)
	}
	if totals.Total != 3650 {
		t.Fatalf("drill: total = %v, esperado 3650", totals.Total)
	}
	if totals.Paid != 1900 {
		t.Fatalf("drill: pago = %v, esperado 1900 (parcial conta o valor pago)", totals.Paid)
	}
	if totals.Open != 350 {
		t.Fatalf("drill: em aberto = %v, esperado 350", totals.Open)
	}
	if totals.Overdue != 200 {
		t.Fatalf("drill: vencido = %v, esperado 200", totals.Overdue)
	}
	for _, row := range result.Rows {
		if row.StatusLabel == "" || row.CategoryLabel == "" {
			t.Fatalf("drill: linha sem rótulo: %+v", row)
		}
		if row.ReceiptID == "" {
			t.Fatalf("drill: linha sem recibo: %+v", row)
		}
	}
}

func TestDrillEmptyResult(t *testing.T) {
	env := drillEnv(t)

	result, err := DrilldownReceipts(context.Background(), env.deps, DrilldownFilter{Status: "paid", Query: "zzz"})
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Fatalf("drill: linhas = %v, esperado recorte vazio", drillIDs(result))
	}
	if result.Totals.Count != 0 || result.Totals.Total != 0 {
		t.Fatalf("drill: totais = %+v, esperados zerados", result.Totals)
	}
}
