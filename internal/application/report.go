package application

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Relatório do mês (tier 2).
//
// A agregação acontece em Go sobre as listagens — nada de SQL de agregação por
// dialeto — e a regra de situação é a mesma das telas (`domain.ReceiptStatusOf`).
// A competência do lançamento é o mês do vencimento.

// reportMonthNames são os nomes dos meses usados no rótulo da competência.
var reportMonthNames = []string{
	"janeiro", "fevereiro", "março", "abril", "maio", "junho",
	"julho", "agosto", "setembro", "outubro", "novembro", "dezembro",
}

// reportMonthLabel devolve o rótulo da competência (`setembro/2026`).
func reportMonthLabel(year, month int) string {
	if month < 1 || month > 12 {
		return ""
	}
	return fmt.Sprintf("%s/%d", reportMonthNames[month-1], year)
}

// ReportStatusCount é a linha da contagem por situação.
type ReportStatusCount struct {
	Status      string  `json:"status"`
	Label       string  `json:"label"`
	Count       int     `json:"count"`
	Amount      float64 `json:"amount"`
	AmountLabel string  `json:"amountLabel"`
}

// ReportGroup agrega os lançamentos da competência por imóvel ou por inquilino.
type ReportGroup struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Receipts int     `json:"receipts"`
	Expected float64 `json:"expected"`
	Received float64 `json:"received"`
	Open     float64 `json:"open"`
	Overdue  float64 `json:"overdue"`
}

// ReportPayload é o corpo do relatório mensal.
//
// `Expected` é a soma dos valores lançados na competência, `Received` a soma do
// que foi efetivamente pago, `Open` o que ainda está a vencer e `Overdue` o que
// já venceu sem baixa.
type ReportPayload struct {
	Year      int     `json:"year"`
	Month     int     `json:"month"`
	MonthKey  string  `json:"monthKey"`
	Label     string  `json:"label"`
	Receipts  int     `json:"receipts"`
	Expected  float64 `json:"expected"`
	Received  float64 `json:"received"`
	Open      float64 `json:"open"`
	Overdue   float64 `json:"overdue"`
	PaidCount int     `json:"paidCount"`
	// Counts é a contagem por situação, sempre nas quatro situações conhecidas.
	Counts     []ReportStatusCount `json:"counts"`
	ByProperty []ReportGroup       `json:"byProperty"`
	ByTenant   []ReportGroup       `json:"byTenant"`
}

// ReportReceiptRow é um lançamento da competência apresentado em lista (usado
// pelas consultas em linguagem natural).
type ReportReceiptRow struct {
	ID              string  `json:"id"`
	TenantName      string  `json:"tenantName"`
	PropertyName    string  `json:"propertyName"`
	Status          string  `json:"status"`
	StatusLabel     string  `json:"statusLabel"`
	DueDate         string  `json:"dueDate"`
	DueDateLabel    string  `json:"dueDateLabel"`
	Amount          float64 `json:"amount"`
	AmountLabel     string  `json:"amountLabel"`
	PaidAmount      float64 `json:"paidAmount"`
	PaidAmountLabel string  `json:"paidAmountLabel"`
	PaidAt          string  `json:"paidAt"`
	PaidAtLabel     string  `json:"paidAtLabel"`
}

// MonthlyReport monta o relatório da competência informada.
func MonthlyReport(ctx context.Context, deps Deps, year, month int) (*ReportPayload, error) {
	selection, err := reportMonthOf(ctx, deps, year, month)
	if err != nil {
		return nil, err
	}

	payload := &ReportPayload{
		Year:     selection.year,
		Month:    selection.month,
		MonthKey: selection.key,
		Label:    selection.label,
		Receipts: len(selection.receipts),
	}

	// As quatro situações aparecem sempre, mesmo zeradas: o painel não precisa
	// saber quais existem no mês.
	order := []domain.ReceiptStatus{
		domain.ReceiptStatusPaid,
		domain.ReceiptStatusPartial,
		domain.ReceiptStatusDue,
		domain.ReceiptStatusOverdue,
	}
	counts := make(map[domain.ReceiptStatus]*ReportStatusCount, len(order))
	payload.Counts = make([]ReportStatusCount, 0, len(order))
	for _, status := range order {
		count := ReportStatusCount{Status: string(status), Label: domain.ReceiptStatusLabel(status)}
		payload.Counts = append(payload.Counts, count)
		counts[status] = &payload.Counts[len(payload.Counts)-1]
	}

	properties := map[string]*ReportGroup{}
	tenants := map[string]*ReportGroup{}

	for _, receipt := range selection.receipts {
		status := selection.statusOf(receipt)
		paid := paidAmountOf(receipt)

		payload.Expected += receipt.Amount
		switch status {
		case domain.ReceiptStatusPaid, domain.ReceiptStatusPartial:
			payload.Received += paid
			payload.PaidCount++
		case domain.ReceiptStatusDue:
			payload.Open += receipt.Amount
		case domain.ReceiptStatusOverdue:
			payload.Overdue += receipt.Amount
		}

		if count := counts[status]; count != nil {
			count.Count++
			count.Amount += receipt.Amount
		}

		property := reportGroupOf(properties, domain.Str(receipt.PropertyID), selection.propertyName(receipt))
		property.Receipts++
		property.Expected += receipt.Amount
		tenant := reportGroupOf(tenants, domain.Str(receipt.TenantID), selection.tenantName(receipt))
		tenant.Receipts++
		tenant.Expected += receipt.Amount

		switch status {
		case domain.ReceiptStatusPaid, domain.ReceiptStatusPartial:
			property.Received += paid
			tenant.Received += paid
		case domain.ReceiptStatusDue:
			property.Open += receipt.Amount
			tenant.Open += receipt.Amount
		case domain.ReceiptStatusOverdue:
			property.Overdue += receipt.Amount
			tenant.Overdue += receipt.Amount
		}
	}

	for _, count := range payload.Counts {
		count.AmountLabel = format.FormatCurrency(count.Amount)
	}
	payload.ByProperty = reportGroupsSorted(properties)
	payload.ByTenant = reportGroupsSorted(tenants)
	return payload, nil
}

// reportMonth é a competência selecionada: os lançamentos com vencimento no mês
// e os nomes de apoio para quem não tem contrato.
type reportMonth struct {
	year     int
	month    int
	key      string
	label    string
	today    time.Time
	receipts []domain.Receipt

	propertyNames map[string]string
	tenantNames   map[string]string
}

// reportMonthOf carrega a competência: valida ano e mês, lê os lançamentos e
// separa os que vencem no mês.
func reportMonthOf(ctx context.Context, deps Deps, year, month int) (*reportMonth, error) {
	if year < 1970 || year > 9999 || month < 1 || month > 12 {
		return nil, domain.Invalid("Informe um ano e um mês válidos.")
	}

	receipts, err := ListReceipts(ctx, deps)
	if err != nil {
		return nil, err
	}
	properties, err := ListProperties(ctx, deps)
	if err != nil {
		return nil, err
	}
	tenants, err := ListTenants(ctx, deps)
	if err != nil {
		return nil, err
	}

	selection := &reportMonth{
		year:          year,
		month:         month,
		key:           domain.MonthKey(year, month),
		label:         reportMonthLabel(year, month),
		today:         deps.Clock.Now(),
		propertyNames: make(map[string]string, len(properties)),
		tenantNames:   make(map[string]string, len(tenants)),
	}
	for _, property := range properties {
		selection.propertyNames[property.ID] = property.Name
	}
	for _, tenant := range tenants {
		selection.tenantNames[tenant.ID] = tenant.Name
	}
	for _, receipt := range receipts {
		if dueMonthKey(receipt.DueDate) == selection.key {
			selection.receipts = append(selection.receipts, receipt)
		}
	}
	return selection, nil
}

// statusOf deriva a situação do lançamento no instante de referência.
func (m *reportMonth) statusOf(receipt domain.Receipt) domain.ReceiptStatus {
	return domain.ReceiptStatusOf(receipt, m.today)
}

// propertyName resolve o nome do imóvel: o do join manda, a listagem é o apoio.
func (m *reportMonth) propertyName(receipt domain.Receipt) string {
	if receipt.PropertyName != "" {
		return receipt.PropertyName
	}
	if name := m.propertyNames[domain.Str(receipt.PropertyID)]; name != "" {
		return name
	}
	return "Sem imóvel"
}

// tenantName resolve o nome do titular: o do join manda, a listagem é o apoio.
func (m *reportMonth) tenantName(receipt domain.Receipt) string {
	if receipt.TenantName != "" {
		return receipt.TenantName
	}
	if name := m.tenantNames[domain.Str(receipt.TenantID)]; name != "" {
		return name
	}
	return "Sem titular"
}

// rows devolve os lançamentos da competência que satisfazem o recorte, em ordem
// de vencimento e depois de titular.
func (m *reportMonth) rows(want func(domain.ReceiptStatus) bool) []ReportReceiptRow {
	rows := make([]ReportReceiptRow, 0, len(m.receipts))
	for _, receipt := range m.receipts {
		status := m.statusOf(receipt)
		if !want(status) {
			continue
		}
		paid := paidAmountOf(receipt)
		row := ReportReceiptRow{
			ID:           receipt.ID,
			TenantName:   m.tenantName(receipt),
			PropertyName: m.propertyName(receipt),
			Status:       string(status),
			StatusLabel:  domain.ReceiptStatusLabel(status),
			DueDate:      receipt.DueDate,
			DueDateLabel: format.FormatDateBR(receipt.DueDate),
			Amount:       receipt.Amount,
			AmountLabel:  format.FormatCurrency(receipt.Amount),
		}
		if paid > 0 {
			row.PaidAmount = paid
			row.PaidAmountLabel = format.FormatCurrency(paid)
		}
		if receipt.PaidAt != nil && *receipt.PaidAt != "" {
			row.PaidAt = *receipt.PaidAt
			row.PaidAtLabel = format.FormatDateTimeBR(*receipt.PaidAt)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(a, b int) bool {
		if rows[a].DueDate != rows[b].DueDate {
			return rows[a].DueDate < rows[b].DueDate
		}
		return rows[a].TenantName < rows[b].TenantName
	})
	return rows
}

// reportGroupOf devolve (criando na primeira vez) o grupo do imóvel/titular.
func reportGroupOf(groups map[string]*ReportGroup, id, name string) *ReportGroup {
	group, ok := groups[id]
	if !ok {
		group = &ReportGroup{ID: id, Name: name}
		groups[id] = group
	}
	return group
}

// reportGroupsSorted ordena os grupos pelo esperado e, no empate, pelo nome.
func reportGroupsSorted(groups map[string]*ReportGroup) []ReportGroup {
	sorted := make([]ReportGroup, 0, len(groups))
	for _, group := range groups {
		sorted = append(sorted, *group)
	}
	sort.SliceStable(sorted, func(a, b int) bool {
		if sorted[a].Expected != sorted[b].Expected {
			return sorted[a].Expected > sorted[b].Expected
		}
		return sorted[a].Name < sorted[b].Name
	})
	return sorted
}
