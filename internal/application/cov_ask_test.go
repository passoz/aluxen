package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/domain"
)

// ── Consulta em linguagem natural e relatório do mês (`cask`) ──────
//
// Os testes provam o que sai das ferramentas e do relatório dado o estado dos
// repositórios: somas, contagens, nomes, ordenação e o texto entregue ao
// modelo. O provedor de IA é roteirizado (`caskChat`): ele devolve as respostas
// enfileiradas e guarda cada pedido, de modo que o teste confira tanto a
// resposta do agente quanto o contrato de conversa que ele recebeu.

// caskChat é o provedor de IA roteirizado dos testes.
type caskChat struct {
	responses []ChatResponse
	requests  []ChatRequest
	err       error
}

func (c *caskChat) Complete(_ context.Context, request ChatRequest) (ChatResponse, error) {
	c.requests = append(c.requests, request)
	if c.err != nil {
		return ChatResponse{}, c.err
	}
	if len(c.responses) == 0 {
		return ChatResponse{}, nil
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

// caskToolCall monta a resposta do provedor que pede uma ferramenta.
func caskToolCall(name string, arguments map[string]any) ChatResponse {
	return ChatResponse{ToolCalls: []ChatToolCall{{Name: name, Arguments: arguments}}}
}

// caskReceipt monta um lançamento com o imóvel vinculado.
func caskReceipt(id, tenantID, propertyID, dueDate string, amount float64) domain.Receipt {
	receipt := covReceiptOf(id, tenantID, "lease-"+id, dueDate, amount)
	receipt.PropertyID = domain.StrPtr(propertyID)
	return receipt
}

// caskSeedMonth monta a competência padrão dos testes: as quatro situações, um
// lançamento fora do mês, um sem titular/imóvel e um com os nomes do join.
func caskSeedMonth(env *covEnv) {
	env.tenants.items = []domain.Tenant{
		covTenantOf("t1", "Ana"),
		covTenantOf("t2", "Bruno"),
		covTenantOf("t3", "Caio"),
	}
	env.props.items = []domain.Property{
		covPropertyOf("p1", "Casa A"),
		covPropertyOf("p2", "Casa B"),
		covPropertyOf("p3", "Casa C"),
	}
	// Nomes do join mandam sobre a listagem; ids fora da listagem exercitam o
	// apoio e o rótulo de ausência.
	joined := caskReceipt("r6", "t7", "p7", "2026-09-28", 300)
	joined.TenantName = "Heitor Join"
	joined.PropertyName = "Sobrado Join"

	env.receipts.items = []domain.Receipt{
		covPaidReceipt(caskReceipt("r1", "t1", "p1", "2026-09-10", 1000), "2026-09-08", 1000),
		covPaidReceipt(caskReceipt("r2", "t1", "p1", "2026-09-12", 800), "2026-09-12", 300),
		caskReceipt("r3", "t2", "p2", "2026-09-30", 1200),
		caskReceipt("r4", "t3", "p3", "2026-09-05", 500),
		caskReceipt("r5", "", "", "2026-09-25", 250),
		joined,
		caskReceipt("r9", "t1", "p1", "2026-08-15", 9999),
	}
}

// caskMonthArgs é o argumento das ferramentas de competência.
func caskMonthArgs(year, month float64) map[string]any {
	return map[string]any{"year": year, "month": month}
}

// ── Relatório do mês ──────────────────────────────────────────────

func TestCaskMonthlyReportAggregatesMonth(t *testing.T) {
	env := newCovEnv(t)
	caskSeedMonth(env)

	payload, err := MonthlyReport(context.Background(), env.deps, 2026, 9)
	if err != nil {
		t.Fatalf("MonthlyReport devolveu erro: %v", err)
	}
	if payload.Year != 2026 || payload.Month != 9 || payload.MonthKey != "2026-09" || payload.Label != "setembro/2026" {
		t.Fatalf("competência inesperada: %+v", payload)
	}
	if payload.Receipts != 6 {
		t.Fatalf("lançamentos da competência: esperado 6, veio %d", payload.Receipts)
	}
	if payload.Expected != 4050 || payload.Received != 1300 || payload.Open != 1750 || payload.Overdue != 500 {
		t.Fatalf("somas inesperadas: esperado %.2f recebido %.2f aberto %.2f vencido %.2f",
			payload.Expected, payload.Received, payload.Open, payload.Overdue)
	}
	if payload.PaidCount != 2 {
		t.Fatalf("baixas contadas: esperado 2, veio %d", payload.PaidCount)
	}

	// As quatro situações aparecem sempre, mesmo as zeradas.
	wantCounts := []ReportStatusCount{
		{Status: "paid", Label: "Pago", Count: 1, Amount: 1000, AmountLabel: "R$ 1.000,00"},
		{Status: "partial", Label: "Parcial", Count: 1, Amount: 800, AmountLabel: "R$ 800,00"},
		{Status: "due", Label: "A vencer", Count: 3, Amount: 1750, AmountLabel: "R$ 1.750,00"},
		{Status: "overdue", Label: "Vencido", Count: 1, Amount: 500, AmountLabel: "R$ 500,00"},
	}
	if len(payload.Counts) != len(wantCounts) {
		t.Fatalf("contagens: esperado %d situações, veio %d", len(wantCounts), len(payload.Counts))
	}
	for index, want := range wantCounts {
		if payload.Counts[index] != want {
			t.Fatalf("contagem %d inesperada: %+v", index, payload.Counts[index])
		}
	}

	wantProps := []ReportGroup{
		{ID: "p1", Name: "Casa A", Receipts: 2, Expected: 1800, Received: 1300},
		{ID: "p2", Name: "Casa B", Receipts: 1, Expected: 1200, Open: 1200},
		{ID: "p3", Name: "Casa C", Receipts: 1, Expected: 500, Overdue: 500},
		{ID: "p7", Name: "Sobrado Join", Receipts: 1, Expected: 300, Open: 300},
		{ID: "", Name: "Sem imóvel", Receipts: 1, Expected: 250, Open: 250},
	}
	if len(payload.ByProperty) != len(wantProps) {
		t.Fatalf("grupos por imóvel: esperado %d, veio %d", len(wantProps), len(payload.ByProperty))
	}
	for index, want := range wantProps {
		if payload.ByProperty[index] != want {
			t.Fatalf("grupo por imóvel %d inesperado: %+v", index, payload.ByProperty[index])
		}
	}

	wantTenants := []ReportGroup{
		{ID: "t1", Name: "Ana", Receipts: 2, Expected: 1800, Received: 1300},
		{ID: "t2", Name: "Bruno", Receipts: 1, Expected: 1200, Open: 1200},
		{ID: "t3", Name: "Caio", Receipts: 1, Expected: 500, Overdue: 500},
		{ID: "t7", Name: "Heitor Join", Receipts: 1, Expected: 300, Open: 300},
		{ID: "", Name: "Sem titular", Receipts: 1, Expected: 250, Open: 250},
	}
	if len(payload.ByTenant) != len(wantTenants) {
		t.Fatalf("grupos por titular: esperado %d, veio %d", len(wantTenants), len(payload.ByTenant))
	}
	for index, want := range wantTenants {
		if payload.ByTenant[index] != want {
			t.Fatalf("grupo por titular %d inesperado: %+v", index, payload.ByTenant[index])
		}
	}
}

func TestCaskMonthlyReportValidatesYearMonth(t *testing.T) {
	env := newCovEnv(t)
	caskSeedMonth(env)

	cases := []struct {
		name        string
		year, month int
	}{
		{"ano zero", 0, 9},
		{"ano antes da faixa", 1969, 9},
		{"ano acima da faixa", 10000, 9},
		{"mês zero", 2026, 0},
		{"mês treze", 2026, 13},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := MonthlyReport(context.Background(), env.deps, tc.year, tc.month)
			if payload != nil || err == nil || !strings.Contains(err.Error(), "Informe um ano e um mês válidos.") {
				t.Fatalf("ano/mês %d/%d: payload %+v erro %v", tc.year, tc.month, payload, err)
			}
		})
	}

	// A validação acontece antes de qualquer leitura: repositório quebrado não
	// muda a mensagem devolvida ao usuário.
	env.receipts.listErr = errors.New("banco fora")
	if _, err := MonthlyReport(context.Background(), env.deps, 2026, 13); err == nil ||
		!strings.Contains(err.Error(), "Informe um ano e um mês válidos.") {
		t.Fatalf("validação devia preceder a leitura: %v", err)
	}
}

func TestCaskMonthlyReportPropagatesRepoErrors(t *testing.T) {
	env := newCovEnv(t)
	caskSeedMonth(env)

	env.receipts.listErr = errors.New("banco fora")
	if _, err := MonthlyReport(context.Background(), env.deps, 2026, 9); err == nil ||
		!strings.Contains(err.Error(), "listar pagamentos") {
		t.Fatalf("erro dos lançamentos não propagado: %v", err)
	}

	env.receipts.listErr = nil
	env.props.listErr = errors.New("banco fora")
	if _, err := MonthlyReport(context.Background(), env.deps, 2026, 9); err == nil ||
		!strings.Contains(err.Error(), "listar imóveis") {
		t.Fatalf("erro dos imóveis não propagado: %v", err)
	}

	env.props.listErr = nil
	env.tenants.listErr = errors.New("banco fora")
	if _, err := MonthlyReport(context.Background(), env.deps, 2026, 9); err == nil ||
		!strings.Contains(err.Error(), "listar locatários") {
		t.Fatalf("erro dos locatários não propagado: %v", err)
	}
}

func TestCaskReportMonthLabel(t *testing.T) {
	cases := []struct {
		year, month int
		want        string
	}{
		{2026, 1, "janeiro/2026"},
		{2026, 9, "setembro/2026"},
		{2026, 12, "dezembro/2026"},
		{2026, 0, ""},
		{2026, 13, ""},
	}
	for _, tc := range cases {
		if got := reportMonthLabel(tc.year, tc.month); got != tc.want {
			t.Fatalf("rótulo de %d/%d: esperado %q, veio %q", tc.year, tc.month, tc.want, got)
		}
	}
}

func TestCaskReportGroupsSorted(t *testing.T) {
	groups := map[string]*ReportGroup{}
	first := reportGroupOf(groups, "a", "Alfa")
	first.Receipts = 2
	first.Expected = 100

	// O grupo é criado na primeira vez e o nome já definido prevalece.
	reused := reportGroupOf(groups, "a", "Nome Tardio")
	if reused != first || reused.Name != "Alfa" || reused.Receipts != 2 {
		t.Fatalf("grupo reaproveitado inesperado: %+v", reused)
	}

	reportGroupOf(groups, "b", "Beta").Expected = 100
	reportGroupOf(groups, "c", "Gama").Expected = 300

	sorted := reportGroupsSorted(groups)
	if len(sorted) != 3 || sorted[0].Name != "Gama" || sorted[1].Name != "Alfa" || sorted[2].Name != "Beta" {
		t.Fatalf("ordenação inesperada: %+v", sorted)
	}
	if len(reportGroupsSorted(map[string]*ReportGroup{})) != 0 {
		t.Fatal("mapa vazio devia devolver lista vazia")
	}
}

// ── Ferramentas tipadas ───────────────────────────────────────────

func TestCaskAskResumoMes(t *testing.T) {
	env := newCovEnv(t)
	caskSeedMonth(env)

	result, summary, err := askResumoMes(context.Background(), env.deps, caskMonthArgs(2026, 9))
	if err != nil {
		t.Fatalf("askResumoMes devolveu erro: %v", err)
	}
	payload, ok := result.(*ReportPayload)
	if !ok {
		t.Fatalf("resultado inesperado: %T", result)
	}
	if payload.Expected != 4050 || payload.Received != 1300 || payload.Open != 1750 || payload.Overdue != 500 {
		t.Fatalf("payload inesperado: %+v", payload)
	}
	want := "Em setembro/2026: esperado R$ 4.050,00, recebido R$ 1.300,00, em aberto R$ 1.750,00 e vencido R$ 500,00, em 6 lançamento(s)."
	if summary != want {
		t.Fatalf("resumo inesperado:\n  esperado %q\n  veio     %q", want, summary)
	}
}

func TestCaskAskResumoMesPropagatesError(t *testing.T) {
	env := newCovEnv(t)
	caskSeedMonth(env)
	env.receipts.listErr = errors.New("banco fora")

	if _, _, err := askResumoMes(context.Background(), env.deps, nil); err == nil ||
		!strings.Contains(err.Error(), "listar pagamentos") {
		t.Fatalf("erro não propagado: %v", err)
	}
}

func TestCaskAskRecebidosMes(t *testing.T) {
	env := newCovEnv(t)
	caskSeedMonth(env)

	result, summary, err := askRecebidosMes(context.Background(), env.deps, caskMonthArgs(2026, 9))
	if err != nil {
		t.Fatalf("askRecebidosMes devolveu erro: %v", err)
	}
	if summary != "Em setembro/2026: 2 lançamento(s) recebido, total R$ 1.800,00." {
		t.Fatalf("resumo inesperado: %q", summary)
	}
	data, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("resultado inesperado: %T", result)
	}
	if data["count"] != 2 || data["total"] != 1800.0 || data["month"] != "2026-09" || data["label"] != "setembro/2026" {
		t.Fatalf("cabeçalho inesperado: %+v", data)
	}
	rows, ok := data["receipts"].([]ReportReceiptRow)
	if !ok || len(rows) != 2 {
		t.Fatalf("lançamentos inesperados: %+v", data["receipts"])
	}
	want := []ReportReceiptRow{
		{
			ID: "r1", TenantName: "Ana", PropertyName: "Casa A",
			Status: "paid", StatusLabel: "Pago",
			DueDate: "2026-09-10", DueDateLabel: "10/09/2026",
			Amount: 1000, AmountLabel: "R$ 1.000,00",
			PaidAmount: 1000, PaidAmountLabel: "R$ 1.000,00",
			PaidAt: "2026-09-08", PaidAtLabel: "08/09/2026",
		},
		{
			ID: "r2", TenantName: "Ana", PropertyName: "Casa A",
			Status: "partial", StatusLabel: "Parcial",
			DueDate: "2026-09-12", DueDateLabel: "12/09/2026",
			Amount: 800, AmountLabel: "R$ 800,00",
			PaidAmount: 300, PaidAmountLabel: "R$ 300,00",
			PaidAt: "2026-09-12", PaidAtLabel: "12/09/2026",
		},
	}
	for index, wantRow := range want {
		if rows[index] != wantRow {
			t.Fatalf("lançamento %d inesperado:\n  esperado %+v\n  veio     %+v", index, wantRow, rows[index])
		}
	}
}

func TestCaskAskAbertos(t *testing.T) {
	env := newCovEnv(t)
	caskSeedMonth(env)

	result, summary, err := askAbertos(context.Background(), env.deps, caskMonthArgs(2026, 9))
	if err != nil {
		t.Fatalf("askAbertos devolveu erro: %v", err)
	}
	if summary != "Em setembro/2026: 3 lançamento(s) em aberto, total R$ 1.750,00." {
		t.Fatalf("resumo inesperado: %q", summary)
	}
	data := result.(map[string]any)
	if data["count"] != 3 || data["total"] != 1750.0 {
		t.Fatalf("cabeçalho inesperado: %+v", data)
	}
	rows := data["receipts"].([]ReportReceiptRow)
	// Ordem de vencimento: 25/09, 28/09 e 30/09.
	want := []ReportReceiptRow{
		{
			ID: "r5", TenantName: "Sem titular", PropertyName: "Sem imóvel",
			Status: "due", StatusLabel: "A vencer",
			DueDate: "2026-09-25", DueDateLabel: "25/09/2026",
			Amount: 250, AmountLabel: "R$ 250,00",
		},
		{
			ID: "r6", TenantName: "Heitor Join", PropertyName: "Sobrado Join",
			Status: "due", StatusLabel: "A vencer",
			DueDate: "2026-09-28", DueDateLabel: "28/09/2026",
			Amount: 300, AmountLabel: "R$ 300,00",
		},
		{
			ID: "r3", TenantName: "Bruno", PropertyName: "Casa B",
			Status: "due", StatusLabel: "A vencer",
			DueDate: "2026-09-30", DueDateLabel: "30/09/2026",
			Amount: 1200, AmountLabel: "R$ 1.200,00",
		},
	}
	if len(rows) != len(want) {
		t.Fatalf("lançamentos: esperado %d, veio %d", len(want), len(rows))
	}
	for index, wantRow := range want {
		if rows[index] != wantRow {
			t.Fatalf("lançamento %d inesperado:\n  esperado %+v\n  veio     %+v", index, wantRow, rows[index])
		}
	}
}

func TestCaskAskMonthRowsPropagatesErrors(t *testing.T) {
	env := newCovEnv(t)
	caskSeedMonth(env)

	env.receipts.listErr = errors.New("banco fora")
	if _, _, err := askAbertos(context.Background(), env.deps, nil); err == nil ||
		!strings.Contains(err.Error(), "listar pagamentos") {
		t.Fatalf("erro dos lançamentos não propagado: %v", err)
	}

	env.receipts.listErr = nil
	env.props.listErr = errors.New("banco fora")
	if _, _, err := askAbertos(context.Background(), env.deps, nil); err == nil ||
		!strings.Contains(err.Error(), "listar imóveis") {
		t.Fatalf("erro dos imóveis não propagado: %v", err)
	}

	env.props.listErr = nil
	env.tenants.listErr = errors.New("banco fora")
	if _, _, err := askAbertos(context.Background(), env.deps, nil); err == nil ||
		!strings.Contains(err.Error(), "listar locatários") {
		t.Fatalf("erro dos locatários não propagado: %v", err)
	}
}

func TestCaskAskInadimplentes(t *testing.T) {
	env := newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t2", "Bruno"), covTenantOf("t3", "Caio")}
	env.receipts.items = []domain.Receipt{
		caskReceipt("d1", "t3", "p3", "2026-09-05", 500),
		caskReceipt("d2", "t3", "p3", "2026-09-01", 300),
		caskReceipt("d3", "t2", "p2", "2026-09-15", 1000),
		caskReceipt("d4", "", "", "2026-09-10", 50),
		caskReceipt("d5", "t2", "p2", "2026-09-30", 700), // a vencer: fora da lista
	}

	result, summary, err := askInadimplentes(context.Background(), env.deps, nil)
	if err != nil {
		t.Fatalf("askInadimplentes devolveu erro: %v", err)
	}
	data := result.(map[string]any)
	if data["count"] != 3 || data["total"] != 1850.0 || data["referenceDay"] != "2026-09-21" {
		t.Fatalf("cabeçalho inesperado: %+v", data)
	}
	// O resumo cita o mais antigo da maior dívida (o primeiro da ordem), não o
	// vencimento mais antigo de todos.
	want := "3 inquilino(s) com lançamento vencido, total R$ 1.850,00. " +
		"O mais antigo venceu em 15/09/2026 (6 dia(s) de atraso)."
	if summary != want {
		t.Fatalf("resumo inesperado:\n  esperado %q\n  veio     %q", want, summary)
	}
	rows := data["delinquents"].([]reportDelinquentRow)
	wantRows := []reportDelinquentRow{
		{
			TenantID: "t2", TenantName: "Bruno", Receipts: 1,
			Amount: 1000, AmountLabel: "R$ 1.000,00",
			OldestDueDate: "2026-09-15", OldestDueDateLabel: "15/09/2026", DaysLate: 6,
		},
		{
			TenantID: "t3", TenantName: "Caio", Receipts: 2,
			Amount: 800, AmountLabel: "R$ 800,00",
			OldestDueDate: "2026-09-01", OldestDueDateLabel: "01/09/2026", DaysLate: 20,
		},
		{
			TenantID: "", TenantName: "Sem titular", Receipts: 1,
			Amount: 50, AmountLabel: "R$ 50,00",
			OldestDueDate: "2026-09-10", OldestDueDateLabel: "10/09/2026", DaysLate: 11,
		},
	}
	if len(rows) != len(wantRows) {
		t.Fatalf("inquilinos: esperado %d, veio %d", len(wantRows), len(rows))
	}
	for index, wantRow := range wantRows {
		if rows[index] != wantRow {
			t.Fatalf("inquilino %d inesperado:\n  esperado %+v\n  veio     %+v", index, wantRow, rows[index])
		}
	}
}

func TestCaskAskInadimplentesSemAtraso(t *testing.T) {
	env := newCovEnv(t)
	env.receipts.items = []domain.Receipt{caskReceipt("d1", "t1", "p1", "2026-09-30", 700)}

	result, summary, err := askInadimplentes(context.Background(), env.deps, nil)
	if err != nil {
		t.Fatalf("askInadimplentes devolveu erro: %v", err)
	}
	if summary != "0 inquilino(s) com lançamento vencido, total R$ 0,00." {
		t.Fatalf("resumo inesperado: %q", summary)
	}
	if rows := result.(map[string]any)["delinquents"].([]reportDelinquentRow); len(rows) != 0 {
		t.Fatalf("lista devia vir vazia: %+v", rows)
	}
}

func TestCaskAskInadimplentesPropagatesErrors(t *testing.T) {
	env := newCovEnv(t)

	env.receipts.listErr = errors.New("banco fora")
	if _, _, err := askInadimplentes(context.Background(), env.deps, nil); err == nil ||
		!strings.Contains(err.Error(), "listar pagamentos") {
		t.Fatalf("erro dos lançamentos não propagado: %v", err)
	}

	env.receipts.listErr = nil
	env.tenants.listErr = errors.New("banco fora")
	if _, _, err := askInadimplentes(context.Background(), env.deps, nil); err == nil ||
		!strings.Contains(err.Error(), "listar locatários") {
		t.Fatalf("erro dos locatários não propagado: %v", err)
	}
}

func TestCaskReportDaysLate(t *testing.T) {
	midnight := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name, due string
		want      int
	}{
		{"atraso", "2026-09-01", 20},
		{"vencimento hoje", "2026-09-21", 0},
		{"vencimento futuro", "2026-09-25", 0},
		{"data inválida", "sem-data", 0},
		{"espaços em volta", "  2026-09-10  ", 11},
		{"vazio", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reportDaysLate(tc.due, midnight); got != tc.want {
				t.Fatalf("dias de atraso de %q: esperado %d, veio %d", tc.due, tc.want, got)
			}
		})
	}
}

func TestCaskAskOcupacao(t *testing.T) {
	env := newCovEnv(t)
	env.props.items = []domain.Property{
		covPropertyOf("p1", "Casa A"),
		covPropertyOf("p2", "Casa B"),
		covPropertyOf("p3", "Casa C"),
	}
	primeiro := covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 1500)
	primeiro.TenantName = "Ana"
	segundo := covLeaseOf("l2", "t2", "p2", domain.LeaseStatusActive, "2026-01-01", "2026-10-31", 2000)
	segundo.TenantName = "Bruno"
	coabitante := covLeaseOf("l6", "t4", "p1", domain.LeaseStatusActive, "2026-02-01", "2026-11-30", 700)
	coabitante.TenantName = "Dora"
	env.leases.items = []domain.Lease{
		primeiro,
		segundo,
		coabitante,
		covLeaseOf("l3", "t1", "p1", domain.LeaseStatusTerminated, "2026-01-01", "2026-06-30", 999),
		covLeaseOf("l4", "t3", "p3", domain.LeaseStatusActive, "2027-01-01", "2027-12-31", 900),
		covLeaseOf("l5", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-08-31", 800),
	}

	result, summary, err := askOcupacao(context.Background(), env.deps, nil)
	if err != nil {
		t.Fatalf("askOcupacao devolveu erro: %v", err)
	}
	want := "3 contrato(s) ativo(s) hoje, 1 de 3 imóvel(is) vago(s); receita mensal esperada R$ 4.200,00."
	if summary != want {
		t.Fatalf("resumo inesperado:\n  esperado %q\n  veio     %q", want, summary)
	}
	data := result.(map[string]any)
	if data["activeLeases"] != 3 || data["vacantProperties"] != 1 || data["properties"] != 3 ||
		data["expectedMonthly"] != 4200.0 || data["expectedLabel"] != "R$ 4.200,00" ||
		data["referenceDay"] != "2026-09-21" {
		t.Fatalf("cabeçalho inesperado: %+v", data)
	}
	rows := data["occupancy"].([]reportOccupancyRow)
	wantRows := []reportOccupancyRow{
		{
			PropertyID: "p1", PropertyName: "Casa A", ActiveLeases: 2,
			TenantName: "Ana", RentAmount: 1500, RentAmountLabel: "R$ 1.500,00",
			EndDate: "2026-12-31", EndDateLabel: "31/12/2026",
		},
		{
			PropertyID: "p2", PropertyName: "Casa B", ActiveLeases: 1,
			TenantName: "Bruno", RentAmount: 2000, RentAmountLabel: "R$ 2.000,00",
			EndDate: "2026-10-31", EndDateLabel: "31/10/2026",
		},
		{PropertyID: "p3", PropertyName: "Casa C", Vacant: true},
	}
	if len(rows) != len(wantRows) {
		t.Fatalf("imóveis: esperado %d, veio %d", len(wantRows), len(rows))
	}
	for index, wantRow := range wantRows {
		if rows[index] != wantRow {
			t.Fatalf("imóvel %d inesperado:\n  esperado %+v\n  veio     %+v", index, wantRow, rows[index])
		}
	}
}

func TestCaskAskOcupacaoVazia(t *testing.T) {
	env := newCovEnv(t)

	_, summary, err := askOcupacao(context.Background(), env.deps, nil)
	if err != nil {
		t.Fatalf("askOcupacao devolveu erro: %v", err)
	}
	want := "0 contrato(s) ativo(s) hoje, 0 de 0 imóvel(is) vago(s); receita mensal esperada R$ 0,00."
	if summary != want {
		t.Fatalf("resumo inesperado: %q", summary)
	}
}

func TestCaskAskOcupacaoPropagatesErrors(t *testing.T) {
	env := newCovEnv(t)

	env.leases.listErr = errors.New("banco fora")
	if _, _, err := askOcupacao(context.Background(), env.deps, nil); err == nil ||
		!strings.Contains(err.Error(), "listar contratos") {
		t.Fatalf("erro dos contratos não propagado: %v", err)
	}

	env.leases.listErr = nil
	env.props.listErr = errors.New("banco fora")
	if _, _, err := askOcupacao(context.Background(), env.deps, nil); err == nil ||
		!strings.Contains(err.Error(), "listar imóveis") {
		t.Fatalf("erro dos imóveis não propagado: %v", err)
	}
}

func TestCaskAskYearMonth(t *testing.T) {
	env := newCovEnv(t) // relógio em 2026-09-21
	cases := []struct {
		name        string
		args        map[string]any
		year, month int
	}{
		{"ausente cai no mês corrente", nil, 2026, 9},
		{"número", caskMonthArgs(2025, 3), 2025, 3},
		{"inteiro", map[string]any{"year": 2024, "month": 2}, 2024, 2},
		{"texto", map[string]any{"year": "2023", "month": "11"}, 2023, 11},
		{"aliases em português", map[string]any{"ano": "2022", "mês": 7.0}, 2022, 7},
		{"ano abaixo da faixa cai no corrente", map[string]any{"year": 1969.0}, 2026, 9},
		{"ano acima da faixa cai no corrente", map[string]any{"year": 10000.0}, 2026, 9},
		{"mês zero cai no corrente", map[string]any{"month": 0.0}, 2026, 9},
		{"mês treze cai no corrente", map[string]any{"month": 13.0}, 2026, 9},
		{"texto inválido cai no corrente", map[string]any{"year": "abc"}, 2026, 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			year, month := askYearMonth(env.deps, tc.args)
			if year != tc.year || month != tc.month {
				t.Fatalf("competência: esperado %d/%d, veio %d/%d", tc.year, tc.month, year, month)
			}
		})
	}
}

func TestCaskAskArgumentInt(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want int
		ok   bool
	}{
		{"ausente", map[string]any{}, 0, false},
		{"chave desconhecida", map[string]any{"outro": 1.0}, 0, false},
		{"número", map[string]any{"year": 2026.0}, 2026, true},
		{"inteiro", map[string]any{"year": 2025}, 2025, true},
		{"texto com espaços", map[string]any{"year": "  2024  "}, 2024, true},
		{"texto inválido", map[string]any{"year": "abc"}, 0, false},
		{"texto inválido cai para o alias seguinte", map[string]any{"year": "abc", "ano": "2023"}, 2023, true},
		{"tipo não numérico", map[string]any{"year": true}, 0, false},
		{"primeira chave vence", map[string]any{"year": 2026, "ano": 2025}, 2026, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := askArgumentInt(tc.args, "year", "ano")
			if got != tc.want || ok != tc.ok {
				t.Fatalf("argumento: esperado (%d,%t), veio (%d,%t)", tc.want, tc.ok, got, ok)
			}
		})
	}
}

func TestCaskAskToolbox(t *testing.T) {
	toolbox := askToolboxOf()
	wantNames := []string{"resumo_mes", "recebidos_mes", "abertos", "inadimplentes", "ocupacao"}
	if len(toolbox.ordered) != len(wantNames) {
		t.Fatalf("ferramentas: esperado %d, veio %d", len(wantNames), len(toolbox.ordered))
	}
	for index, name := range wantNames {
		tool := toolbox.ordered[index]
		if tool.name != name || tool.description == "" || tool.parameters == nil || tool.run == nil {
			t.Fatalf("ferramenta %d inesperada: %+v", index, tool)
		}
		resolved, ok := toolbox.byName[name]
		if !ok || resolved.name != name {
			t.Fatalf("ferramenta %q não resolvida pelo nome", name)
		}
		declaration := tool.declaration()
		if declaration.Name != name || declaration.Description != tool.description ||
			declaration.Parameters == nil {
			t.Fatalf("declaração de %q inesperada: %+v", name, declaration)
		}
	}
	if _, ok := toolbox.byName["inexistente"]; ok {
		t.Fatal("ferramenta não declarada não devia ser resolvida")
	}
}

func TestCaskAskParameterSchemas(t *testing.T) {
	month := askMonthParameters()
	if month["type"] != "object" {
		t.Fatalf("schema do mês devia ser objeto: %+v", month)
	}
	properties, ok := month["properties"].(map[string]any)
	if !ok || len(properties) != 2 {
		t.Fatalf("propriedades do schema do mês inesperadas: %+v", month["properties"])
	}
	for _, key := range []string{"year", "month"} {
		field, ok := properties[key].(map[string]any)
		if !ok || field["type"] != "integer" || field["description"] == "" {
			t.Fatalf("argumento %q inesperado: %+v", key, properties[key])
		}
	}

	none := askNoParameters()
	if none["type"] != "object" {
		t.Fatalf("schema sem argumentos devia ser objeto: %+v", none)
	}
	empty, ok := none["properties"].(map[string]any)
	if !ok || len(empty) != 0 {
		t.Fatalf("schema sem argumentos devia ser vazio: %+v", none["properties"])
	}
}

func TestCaskAskToolResultTurn(t *testing.T) {
	message := askToolResultTurn("resumo_mes", `{"count":2}`)
	want := ChatMessage{Role: "user", Content: `Resultado da ferramenta resumo_mes: {"count":2}`}
	if message != want {
		t.Fatalf("turno inesperado: %+v", message)
	}
}

func TestCaskAskAnswerFrom(t *testing.T) {
	refused := askAnswerFrom(ChatResponse{Message: "texto solto do modelo"},
		&AskAnswer{Question: "q", Tools: []string{}})
	if !refused.Refused || refused.Answer != AskRefusalMessage {
		t.Fatalf("recusa inesperada: %+v", refused)
	}

	withText := &AskAnswer{Question: "q", Tools: []string{"resumo_mes"}, summaries: []string{"resumo"}}
	answered := askAnswerFrom(ChatResponse{Message: "  Recebido R$ 1.300,00.  "}, withText)
	if answered.Refused || answered.Answer != "Recebido R$ 1.300,00." {
		t.Fatalf("resposta inesperada: %+v", answered)
	}

	fromSummary := &AskAnswer{Question: "q", Tools: []string{"resumo_mes"}, summaries: []string{"linha um", "linha dois"}}
	fallback := askAnswerFrom(ChatResponse{}, fromSummary)
	if fallback.Refused || fallback.Answer != "linha um\nlinha dois" {
		t.Fatalf("resumo de apoio inesperado: %+v", fallback)
	}

	noSummary := &AskAnswer{Question: "q", Tools: []string{"resumo_mes"}}
	empty := askAnswerFrom(ChatResponse{Message: "   "}, noSummary)
	if !empty.Refused || empty.Answer != AskRefusalMessage {
		t.Fatalf("recusa sem resumo inesperada: %+v", empty)
	}
}

// ── Laço da consulta ──────────────────────────────────────────────

func TestCaskAskReportRejectsEmptyQuestion(t *testing.T) {
	env := newCovEnv(t)
	chat := &caskChat{}
	env.deps.Chat = chat

	answer, err := AskReport(context.Background(), env.deps, "   ")
	if answer != nil || err == nil || !strings.Contains(err.Error(), "Escreva a pergunta.") {
		t.Fatalf("pergunta vazia: answer %+v erro %v", answer, err)
	}
	if len(chat.requests) != 0 {
		t.Fatal("provedor não devia ser consultado com pergunta vazia")
	}
}

func TestCaskAskReportUnavailableWithoutProvider(t *testing.T) {
	env := newCovEnv(t)

	answer, err := AskReport(context.Background(), env.deps, "quanto recebemos?")
	if answer != nil || !errors.Is(err, ErrAskUnavailable) {
		t.Fatalf("sem provedor: answer %+v erro %v", answer, err)
	}
}

func TestCaskAskReportRefusesWithoutTool(t *testing.T) {
	env := newCovEnv(t)
	chat := &caskChat{responses: []ChatResponse{{Message: "Não sei."}}}
	env.deps.Chat = chat

	answer, err := AskReport(context.Background(), env.deps, "  quanto recebemos em setembro?  ")
	if err != nil {
		t.Fatalf("AskReport devolveu erro: %v", err)
	}
	if answer.Question != "quanto recebemos em setembro?" {
		t.Fatalf("pergunta não normalizada: %q", answer.Question)
	}
	if !answer.Refused || answer.Answer != AskRefusalMessage || len(answer.Tools) != 0 {
		t.Fatalf("resposta sem ferramenta devia recusar: %+v", answer)
	}
	if len(chat.requests) != 1 {
		t.Fatalf("rodadas: esperado 1, veio %d", len(chat.requests))
	}
	request := chat.requests[0]
	if request.System != askSystemPrompt || request.Temperature != 0 {
		t.Fatalf("instrução do pedido inesperada: %+v", request)
	}
	if len(request.Tools) != 5 || request.Tools[0].Name != "resumo_mes" {
		t.Fatalf("ferramentas declaradas inesperadas: %+v", request.Tools)
	}
	if len(request.Messages) != 1 ||
		request.Messages[0] != (ChatMessage{Role: "user", Content: "quanto recebemos em setembro?"}) {
		t.Fatalf("mensagens iniciais inesperadas: %+v", request.Messages)
	}
}

func TestCaskAskReportRunsToolAndFallsBackToSummary(t *testing.T) {
	env := newCovEnv(t)
	caskSeedMonth(env)
	chat := &caskChat{responses: []ChatResponse{
		caskToolCall("resumo_mes", caskMonthArgs(2026, 9)),
		{Message: ""},
	}}
	env.deps.Chat = chat

	answer, err := AskReport(context.Background(), env.deps, "como foi setembro?")
	if err != nil {
		t.Fatalf("AskReport devolveu erro: %v", err)
	}
	want := "Em setembro/2026: esperado R$ 4.050,00, recebido R$ 1.300,00, em aberto R$ 1.750,00 e vencido R$ 500,00, em 6 lançamento(s)."
	if answer.Refused || answer.Answer != want {
		t.Fatalf("resumo determinístico inesperado: %+v", answer)
	}
	if len(answer.Tools) != 1 || answer.Tools[0] != "resumo_mes" {
		t.Fatalf("ferramentas executadas inesperadas: %+v", answer.Tools)
	}

	// O resultado da ferramenta volta ao modelo como turno de usuário com o
	// payload JSON — é o número que sustenta a resposta.
	if len(chat.requests) != 2 {
		t.Fatalf("rodadas: esperado 2, veio %d", len(chat.requests))
	}
	messages := chat.requests[1].Messages
	last := messages[len(messages)-1]
	const prefix = "Resultado da ferramenta resumo_mes: "
	if last.Role != "user" || !strings.HasPrefix(last.Content, prefix) {
		t.Fatalf("turno do resultado inesperado: %+v", last)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(last.Content, prefix)), &payload); err != nil {
		t.Fatalf("payload do resultado não é JSON: %v", err)
	}
	if payload["expected"] != 4050.0 || payload["received"] != 1300.0 || payload["receipts"] != 6.0 {
		t.Fatalf("payload do resultado inesperado: %+v", payload)
	}
}

func TestCaskAskReportUsesModelText(t *testing.T) {
	env := newCovEnv(t)
	caskSeedMonth(env)
	chat := &caskChat{responses: []ChatResponse{
		caskToolCall("abertos", caskMonthArgs(2026, 9)),
		{Message: "Há R$ 1.750,00 a vencer em setembro."},
	}}
	env.deps.Chat = chat

	answer, err := AskReport(context.Background(), env.deps, "o que vence?")
	if err != nil {
		t.Fatalf("AskReport devolveu erro: %v", err)
	}
	if answer.Refused || answer.Answer != "Há R$ 1.750,00 a vencer em setembro." {
		t.Fatalf("texto do modelo devia valer: %+v", answer)
	}
	if len(answer.Tools) != 1 || answer.Tools[0] != "abertos" {
		t.Fatalf("ferramentas executadas inesperadas: %+v", answer.Tools)
	}
}

func TestCaskAskReportRefusesUnknownTool(t *testing.T) {
	env := newCovEnv(t)
	chat := &caskChat{responses: []ChatResponse{caskToolCall("apagar_tudo", nil)}}
	env.deps.Chat = chat

	answer, err := AskReport(context.Background(), env.deps, "apague tudo")
	if err != nil {
		t.Fatalf("AskReport devolveu erro: %v", err)
	}
	if !answer.Refused || answer.Answer != AskRefusalMessage || len(answer.Tools) != 0 {
		t.Fatalf("ferramenta fora do vocabulário devia recusar: %+v", answer)
	}
	if len(chat.requests) != 1 {
		t.Fatalf("o laço devia parar na ferramenta desconhecida: %d rodada(s)", len(chat.requests))
	}
}

func TestCaskAskReportStopsAtRoundLimit(t *testing.T) {
	env := newCovEnv(t)
	caskSeedMonth(env)
	responses := make([]ChatResponse, 0, askMaxRounds)
	for round := 0; round < askMaxRounds; round++ {
		responses = append(responses, caskToolCall("resumo_mes", caskMonthArgs(2026, 9)))
	}
	chat := &caskChat{responses: responses}
	env.deps.Chat = chat

	answer, err := AskReport(context.Background(), env.deps, "resuma setembro")
	if err != nil {
		t.Fatalf("AskReport devolveu erro: %v", err)
	}
	if len(chat.requests) != askMaxRounds || len(answer.Tools) != askMaxRounds {
		t.Fatalf("teto de rodadas inesperado: %d pedido(s), %d ferramenta(s)",
			len(chat.requests), len(answer.Tools))
	}
	if answer.Refused || !strings.Contains(answer.Answer, "esperado R$ 4.050,00") {
		t.Fatalf("resumo determinístico inesperado: %+v", answer)
	}
}

func TestCaskReportMonthRowsTieBreakByTenant(t *testing.T) {
	env := newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t1", "Zélia"), covTenantOf("t2", "Ana")}
	env.props.items = []domain.Property{covPropertyOf("p1", "Casa A")}
	env.receipts.items = []domain.Receipt{
		caskReceipt("a1", "t1", "p1", "2026-09-30", 100),
		caskReceipt("a2", "t2", "p1", "2026-09-30", 200),
	}

	selection, err := reportMonthOf(context.Background(), env.deps, 2026, 9)
	if err != nil {
		t.Fatalf("reportMonthOf devolveu erro: %v", err)
	}
	rows := selection.rows(func(status domain.ReceiptStatus) bool {
		return status == domain.ReceiptStatusDue
	})
	// Mesmo vencimento: o desempate é pelo nome do titular.
	if len(rows) != 2 || rows[0].ID != "a2" || rows[1].ID != "a1" {
		t.Fatalf("ordem inesperada: %+v", rows)
	}
	if rows[0].TenantName != "Ana" || rows[1].TenantName != "Zélia" {
		t.Fatalf("nomes inesperados: %+v", rows)
	}
}

func TestCaskAskInadimplentesEmpatePorNome(t *testing.T) {
	env := newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t1", "Zélia"), covTenantOf("t2", "Ana")}
	env.receipts.items = []domain.Receipt{
		caskReceipt("d1", "t1", "p1", "2026-09-05", 400),
		caskReceipt("d2", "t2", "p1", "2026-09-06", 400),
	}

	result, _, err := askInadimplentes(context.Background(), env.deps, nil)
	if err != nil {
		t.Fatalf("askInadimplentes devolveu erro: %v", err)
	}
	rows := result.(map[string]any)["delinquents"].([]reportDelinquentRow)
	// Mesma dívida: o desempate é pelo nome do titular.
	if len(rows) != 2 || rows[0].TenantName != "Ana" || rows[1].TenantName != "Zélia" {
		t.Fatalf("ordem inesperada: %+v", rows)
	}
}

func TestCaskAskReportKeepsModelTextBetweenRounds(t *testing.T) {
	env := newCovEnv(t)
	caskSeedMonth(env)
	chat := &caskChat{responses: []ChatResponse{
		{
			Message:   "Vou consultar o resumo.",
			ToolCalls: []ChatToolCall{{Name: "resumo_mes", Arguments: caskMonthArgs(2026, 9)}},
		},
		{Message: "Feito."},
	}}
	env.deps.Chat = chat

	answer, err := AskReport(context.Background(), env.deps, "resuma setembro")
	if err != nil {
		t.Fatalf("AskReport devolveu erro: %v", err)
	}
	if answer.Answer != "Feito." {
		t.Fatalf("resposta inesperada: %+v", answer)
	}
	// O texto do modelo entra na conversa antes do resultado da ferramenta.
	messages := chat.requests[1].Messages
	if len(messages) != 3 {
		t.Fatalf("conversa: esperado 3 mensagens, veio %d", len(messages))
	}
	if messages[1] != (ChatMessage{Role: "assistant", Content: "Vou consultar o resumo."}) {
		t.Fatalf("turno do modelo inesperado: %+v", messages[1])
	}
	if messages[2].Role != "user" ||
		!strings.HasPrefix(messages[2].Content, "Resultado da ferramenta resumo_mes: ") {
		t.Fatalf("turno do resultado inesperado: %+v", messages[2])
	}
}

func TestCaskAskReportWrapsProviderError(t *testing.T) {
	env := newCovEnv(t)
	env.deps.Chat = &caskChat{err: errors.New("timeout")}

	answer, err := AskReport(context.Background(), env.deps, "quanto recebemos?")
	if answer != nil || err == nil || !strings.Contains(err.Error(), "consultar o provedor de IA: timeout") {
		t.Fatalf("erro do provedor não propagado: answer %+v erro %v", answer, err)
	}
}

func TestCaskAskReportPropagatesToolError(t *testing.T) {
	env := newCovEnv(t)
	caskSeedMonth(env)
	env.receipts.listErr = errors.New("banco fora")
	env.deps.Chat = &caskChat{responses: []ChatResponse{caskToolCall("resumo_mes", nil)}}

	answer, err := AskReport(context.Background(), env.deps, "resuma setembro")
	if answer != nil || err == nil || !strings.Contains(err.Error(), "listar pagamentos") {
		t.Fatalf("erro da ferramenta não propagado: answer %+v erro %v", answer, err)
	}
}
