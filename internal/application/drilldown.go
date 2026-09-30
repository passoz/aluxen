package application

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Detalhamento filtrado do painel: o recorte de lançamentos por trás dos
// cartões financeiros e dos gráficos. A agregação acontece em Go sobre as
// listagens — como ComputeMetrics — então não existe SQL específico de
// dialeto, e a regra de situação é a mesma usada no resto do painel. Filtro
// desconhecido ou fora de faixa é ignorado em vez de virar erro.

// DrilldownFilter é o recorte pedido ao detalhamento. Tudo é opcional: sem
// filtro, o recorte é a carteira inteira.
type DrilldownFilter struct {
	// From e To limitam o vencimento (aaaa-mm-dd, parte de data do DueDate).
	From string
	To   string
	// Status é paid, partial, due ou overdue. Outro valor é ignorado.
	Status string
	// Category é a categoria do lançamento. Outro valor é ignorado; categoria
	// vazia no lançamento conta como aluguel, como nos indicadores.
	Category string
	// TenantID e PropertyID filtram pelo titular e pelo imóvel efetivos: o
	// lançamento de aluguel guarda só o contrato, então o titular e o imóvel
	// vêm do contrato quando o lançamento não informa.
	TenantID   string
	PropertyID string
	// Query é a busca livre por descrição, sem diferenciar maiúsculas.
	Query string
}

// DrilldownRow é um lançamento do recorte, com o recibo para o PDF.
type DrilldownRow struct {
	ID            string  `json:"id"`
	Description   string  `json:"description"`
	DueDate       string  `json:"dueDate"`
	DueLabel      string  `json:"dueLabel"`
	PaidAt        string  `json:"paidAt"`
	Amount        float64 `json:"amount"`
	PaidAmount    float64 `json:"paidAmount"`
	Status        string  `json:"status"`
	StatusLabel   string  `json:"statusLabel"`
	Category      string  `json:"category"`
	CategoryLabel string  `json:"categoryLabel"`
	TenantName    string  `json:"tenantName"`
	PropertyName  string  `json:"propertyName"`
	ReceiptID     string  `json:"receiptId"`
}

// DrilldownTotals são os totais do recorte, com a mesma convenção dos
// indicadores: parcial conta o valor pago, em aberto soma a vencer e vencido
// soma o vencido.
type DrilldownTotals struct {
	Count   int     `json:"count"`
	Total   float64 `json:"total"`
	Paid    float64 `json:"paid"`
	Open    float64 `json:"open"`
	Overdue float64 `json:"overdue"`
}

// DrilldownResult é o recorte com seus totais.
type DrilldownResult struct {
	Totals DrilldownTotals `json:"totals"`
	Rows   []DrilldownRow  `json:"rows"`
}

// DrilldownReceipts recorta os lançamentos pelo filtro e soma os totais do
// recorte. A ordem é por vencimento crescente, para a cobrança ler do mais
// antigo ao mais novo.
func DrilldownReceipts(ctx context.Context, deps Deps, filter DrilldownFilter) (*DrilldownResult, error) {
	leases, err := ListLeases(ctx, deps)
	if err != nil {
		return nil, err
	}
	receipts, err := ListReceipts(ctx, deps)
	if err != nil {
		return nil, err
	}

	leaseTenant := make(map[string]string, len(leases))
	leaseProperty := make(map[string]string, len(leases))
	for _, lease := range leases {
		leaseTenant[lease.ID] = lease.TenantID
		leaseProperty[lease.ID] = lease.PropertyID
	}

	filter.Status = drillStatusOrEmpty(filter.Status)
	filter.Category = drillCategoryOrEmpty(filter.Category)
	from, hasFrom := drillDateOrZero(filter.From)
	to, hasTo := drillDateOrZero(filter.To)
	query := strings.ToLower(strings.TrimSpace(filter.Query))

	today := deps.Clock.Now()
	midnight := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())

	result := &DrilldownResult{Rows: []DrilldownRow{}}
	for _, receipt := range receipts {
		status := domain.ReceiptStatusOf(receipt, midnight)
		if filter.Status != "" && string(status) != filter.Status {
			continue
		}
		if filter.Category != "" && drillCategoryOf(receipt) != filter.Category {
			continue
		}
		dueKey := drillDateKey(receipt.DueDate)
		if hasFrom && (dueKey == "" || dueKey < from) {
			continue
		}
		if hasTo && (dueKey == "" || dueKey > to) {
			continue
		}
		if filter.TenantID != "" && drillTenantOf(receipt, leaseTenant) != filter.TenantID {
			continue
		}
		if filter.PropertyID != "" && drillPropertyOf(receipt, leaseProperty) != filter.PropertyID {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(domain.Str(receipt.Description)), query) {
			continue
		}

		paid := paidAmountOf(receipt)
		row := DrilldownRow{
			ID:            receipt.ID,
			Description:   domain.Str(receipt.Description),
			DueDate:       receipt.DueDate,
			DueLabel:      format.FormatDateBR(receipt.DueDate),
			PaidAt:        domain.Str(receipt.PaidAt),
			Amount:        receipt.Amount,
			PaidAmount:    paid,
			Status:        string(status),
			StatusLabel:   domain.ReceiptStatusLabel(status),
			Category:      drillCategoryOf(receipt),
			CategoryLabel: domain.ReceiptCategoryLabel(domain.ReceiptCategory(drillCategoryOf(receipt))),
			TenantName:    receipt.TenantName,
			PropertyName:  receipt.PropertyName,
			ReceiptID:     receipt.ID,
		}
		result.Rows = append(result.Rows, row)

		result.Totals.Count++
		result.Totals.Total += receipt.Amount
		switch status {
		case domain.ReceiptStatusPaid, domain.ReceiptStatusPartial:
			result.Totals.Paid += paid
		case domain.ReceiptStatusDue:
			result.Totals.Open += receipt.Amount
		case domain.ReceiptStatusOverdue:
			result.Totals.Overdue += receipt.Amount
		}
	}

	sort.Slice(result.Rows, func(a, b int) bool {
		if result.Rows[a].DueDate == result.Rows[b].DueDate {
			return result.Rows[a].ID < result.Rows[b].ID
		}
		return result.Rows[a].DueDate < result.Rows[b].DueDate
	})
	return result, nil
}

// drillStatusOrEmpty devolve o código de situação quando conhecido, e vazio
// quando o filtro não informa ou informa valor desconhecido.
func drillStatusOrEmpty(status string) string {
	switch domain.ReceiptStatus(strings.TrimSpace(status)) {
	case domain.ReceiptStatusPaid, domain.ReceiptStatusPartial, domain.ReceiptStatusDue, domain.ReceiptStatusOverdue:
		return strings.TrimSpace(status)
	default:
		return ""
	}
}

// drillCategoryOrEmpty devolve a categoria quando conhecida, e vazio quando o
// filtro não informa ou informa valor desconhecido.
func drillCategoryOrEmpty(category string) string {
	switch domain.ReceiptCategory(strings.TrimSpace(category)) {
	case domain.ReceiptCategoryRent, domain.ReceiptCategoryExtra, domain.ReceiptCategoryCleaning,
		domain.ReceiptCategoryConsumables, domain.ReceiptCategoryDepositRefund:
		return strings.TrimSpace(category)
	default:
		return ""
	}
}

// drillCategoryOf devolve a categoria efetiva do lançamento: vazia conta como
// aluguel, como nos indicadores.
func drillCategoryOf(receipt domain.Receipt) string {
	if receipt.Category == "" {
		return string(domain.ReceiptCategoryRent)
	}
	return string(receipt.Category)
}

// drillTenantOf devolve o titular efetivo: o do lançamento, ou o do contrato.
func drillTenantOf(receipt domain.Receipt, leaseTenant map[string]string) string {
	if receipt.TenantID != nil && *receipt.TenantID != "" {
		return *receipt.TenantID
	}
	if receipt.LeaseID == nil {
		return ""
	}
	return leaseTenant[*receipt.LeaseID]
}

// drillPropertyOf devolve o imóvel efetivo: o do lançamento, ou o do contrato.
func drillPropertyOf(receipt domain.Receipt, leaseProperty map[string]string) string {
	if receipt.PropertyID != nil && *receipt.PropertyID != "" {
		return *receipt.PropertyID
	}
	if receipt.LeaseID == nil {
		return ""
	}
	return leaseProperty[*receipt.LeaseID]
}

// drillDateOrZero interpreta o limite de período (aaaa-mm-dd, aceitando barra
// como o vencimento com barra). Fora de faixa, o limite é ignorado.
func drillDateOrZero(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) < 10 {
		return "", false
	}
	key := strings.ReplaceAll(trimmed[:10], "/", "-")
	if _, err := time.Parse("2006-01-02", key); err != nil {
		return "", false
	}
	return key, true
}

// drillDateKey normaliza o vencimento para comparação de período.
func drillDateKey(dueDate string) string {
	key, ok := drillDateOrZero(dueDate)
	if !ok {
		return ""
	}
	return key
}
