package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain"
)

// Indicadores do painel. A agregação acontece em Go sobre as listagens — assim
// não existe SQL específico de dialeto para somar e agrupar, e a regra de
// status é a mesma usada nas telas.

// MetricsTotals são os totais exibidos nos cartões.
type MetricsTotals struct {
	Tenants          int     `json:"tenants"`
	Properties       int     `json:"properties"`
	ActiveLeases     int     `json:"activeLeases"`
	TerminatedLeases int     `json:"terminatedLeases"`
	Receipts         int     `json:"receipts"`
	PaidReceipts     int     `json:"paidReceipts"`
	PartialReceipts  int     `json:"partialReceipts"`
	DueReceipts      int     `json:"dueReceipts"`
	OverdueReceipts  int     `json:"overdueReceipts"`
	TotalAmount      float64 `json:"totalAmount"`
	PaidAmount       float64 `json:"paidAmount"`
	OpenAmount       float64 `json:"openAmount"`
	OverdueAmount    float64 `json:"overdueAmount"`
	ExpectedMonthly  float64 `json:"expectedMonthly"`
}

// MetricsMonth é uma coluna do gráfico mensal.
type MetricsMonth struct {
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Received float64 `json:"received"`
	Expected float64 `json:"expected"`
	Open     float64 `json:"open"`
}

// MetricsStatus é uma fatia do gráfico de status.
type MetricsStatus struct {
	Status string `json:"status"`
	Label  string `json:"label"`
	Count  int    `json:"count"`
}

// MetricsCategory agrupa lançamentos por categoria.
type MetricsCategory struct {
	Category string  `json:"category"`
	Label    string  `json:"label"`
	Count    int     `json:"count"`
	Total    float64 `json:"total"`
}

// MetricsProperty mostra a ocupação por imóvel.
type MetricsProperty struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ActiveLeases int    `json:"activeLeases"`
}

// MetricsTenant é uma linha do ranking de inquilinos.
type MetricsTenant struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	PaidTotal float64 `json:"paidTotal"`
	Receipts  int     `json:"receipts"`
}

// MetricsPayload é o corpo de GET /metrics.
type MetricsPayload struct {
	Totals     MetricsTotals     `json:"totals"`
	Monthly    []MetricsMonth    `json:"monthly"`
	Status     []MetricsStatus   `json:"status"`
	Categories []MetricsCategory `json:"categories"`
	Properties []MetricsProperty `json:"properties"`
	TopTenants []MetricsTenant   `json:"topTenants"`
}

var monthLabels = []string{"jan", "fev", "mar", "abr", "mai", "jun", "jul", "ago", "set", "out", "nov", "dez"}

// ComputeMetrics agrega os indicadores do painel.
func ComputeMetrics(ctx context.Context, deps Deps) (*MetricsPayload, error) {
	tenants, err := ListTenants(ctx, deps)
	if err != nil {
		return nil, err
	}
	properties, err := ListProperties(ctx, deps)
	if err != nil {
		return nil, err
	}
	leases, err := ListLeases(ctx, deps)
	if err != nil {
		return nil, err
	}
	receipts, err := ListReceipts(ctx, deps)
	if err != nil {
		return nil, err
	}

	today := deps.Clock.Now()
	midnight := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())

	totals := MetricsTotals{
		Tenants:    len(tenants),
		Properties: len(properties),
		Receipts:   len(receipts),
	}
	for _, lease := range leases {
		switch lease.Status {
		case domain.LeaseStatusActive:
			totals.ActiveLeases++
			totals.ExpectedMonthly += lease.RentAmount
		case domain.LeaseStatusTerminated:
			totals.TerminatedLeases++
		}
	}

	leaseTenant := make(map[string]string, len(leases))
	for _, lease := range leases {
		leaseTenant[lease.ID] = lease.TenantID
	}

	type monthAccumulator struct {
		received float64
		expected float64
		open     float64
	}
	monthly := map[string]*monthAccumulator{}
	categories := map[string]*MetricsCategory{}
	tenantPaid := map[string]*MetricsTenant{}

	for _, receipt := range receipts {
		status := domain.ReceiptStatusOf(receipt, midnight)
		paidAmount := paidAmountOf(receipt)

		totals.TotalAmount += receipt.Amount
		switch status {
		case domain.ReceiptStatusPaid:
			totals.PaidReceipts++
		case domain.ReceiptStatusPartial:
			totals.PartialReceipts++
		case domain.ReceiptStatusDue:
			totals.DueReceipts++
			totals.OpenAmount += receipt.Amount
		case domain.ReceiptStatusOverdue:
			totals.OverdueReceipts++
			totals.OverdueAmount += receipt.Amount
		}
		if status == domain.ReceiptStatusPaid || status == domain.ReceiptStatusPartial {
			totals.PaidAmount += paidAmount
		}

		key := dueMonthKey(receipt.DueDate)
		entry, ok := monthly[key]
		if !ok {
			entry = &monthAccumulator{}
			monthly[key] = entry
		}
		if status == domain.ReceiptStatusPaid || status == domain.ReceiptStatusPartial {
			entry.received += paidAmount
		} else {
			entry.open += receipt.Amount
		}
		entry.expected += receipt.Amount

		category := string(receipt.Category)
		if category == "" {
			category = string(domain.ReceiptCategoryRent)
		}
		categoryEntry, ok := categories[category]
		if !ok {
			categoryEntry = &MetricsCategory{Category: category, Label: domain.ReceiptCategoryLabel(domain.ReceiptCategory(category))}
			categories[category] = categoryEntry
		}
		categoryEntry.Count++
		categoryEntry.Total += receipt.Amount

		// O titular efetivo vem do recibo OU do contrato: lançamentos de aluguel
		// guardam só o contrato (o app antigo resolvia isso em SQL, com
		// `coalesce(receipts.tenant_id, leases.tenant_id)`).
		if status == domain.ReceiptStatusPaid || status == domain.ReceiptStatusPartial {
			if tenantID := effectiveTenantID(receipt, leaseTenant); tenantID != "" {
				tenantEntry, ok := tenantPaid[tenantID]
				if !ok {
					tenantEntry = &MetricsTenant{ID: tenantID}
					tenantPaid[tenantID] = tenantEntry
				}
				tenantEntry.PaidTotal += paidAmount
				tenantEntry.Receipts++
			}
		}
	}

	payload := &MetricsPayload{Totals: totals}
	for offset := 3; offset >= -2; offset-- {
		reference := time.Date(midnight.Year(), midnight.Month(), 1, 0, 0, 0, 0, midnight.Location()).AddDate(0, -offset, 0)
		key := reference.Format("2006-01")
		entry := monthly[key]
		month := MetricsMonth{
			Key:   key,
			Label: fmt.Sprintf("%s/%s", monthLabels[int(reference.Month())-1], reference.Format("06")),
		}
		if entry != nil {
			month.Received = entry.received
			month.Expected = entry.expected
			month.Open = entry.open
		}
		payload.Monthly = append(payload.Monthly, month)
	}

	payload.Status = []MetricsStatus{
		{Status: string(domain.ReceiptStatusPaid), Label: "Pago", Count: totals.PaidReceipts},
		{Status: string(domain.ReceiptStatusPartial), Label: "Parcial", Count: totals.PartialReceipts},
		{Status: string(domain.ReceiptStatusDue), Label: "A vencer", Count: totals.DueReceipts},
		{Status: string(domain.ReceiptStatusOverdue), Label: "Vencido", Count: totals.OverdueReceipts},
	}

	payload.Categories = make([]MetricsCategory, 0, len(categories))
	for _, category := range categories {
		payload.Categories = append(payload.Categories, *category)
	}
	sort.Slice(payload.Categories, func(a, b int) bool {
		return payload.Categories[a].Total > payload.Categories[b].Total
	})

	activeByProperty := map[string]int{}
	for _, lease := range leases {
		if lease.Status == domain.LeaseStatusActive {
			activeByProperty[lease.PropertyID]++
		}
	}
	payload.Properties = make([]MetricsProperty, 0, len(properties))
	for _, property := range properties {
		payload.Properties = append(payload.Properties, MetricsProperty{
			ID: property.ID, Name: property.Name, ActiveLeases: activeByProperty[property.ID],
		})
	}

	tenantNames := map[string]string{}
	for _, tenant := range tenants {
		tenantNames[tenant.ID] = tenant.Name
	}
	payload.TopTenants = make([]MetricsTenant, 0, len(tenantPaid))
	for _, entry := range tenantPaid {
		name := tenantNames[entry.ID]
		if name == "" {
			name = "Inquilino"
		}
		payload.TopTenants = append(payload.TopTenants, MetricsTenant{
			ID: entry.ID, Name: name, PaidTotal: entry.PaidTotal, Receipts: entry.Receipts,
		})
	}
	sort.Slice(payload.TopTenants, func(a, b int) bool {
		return payload.TopTenants[a].PaidTotal > payload.TopTenants[b].PaidTotal
	})
	if len(payload.TopTenants) > 5 {
		payload.TopTenants = payload.TopTenants[:5]
	}

	return payload, nil
}

// effectiveTenantID devolve o titular do lançamento: o próprio, quando houver, ou
// o do contrato associado.
func effectiveTenantID(receipt domain.Receipt, leaseTenant map[string]string) string {
	if receipt.TenantID != nil && *receipt.TenantID != "" {
		return *receipt.TenantID
	}
	if receipt.LeaseID == nil {
		return ""
	}
	return leaseTenant[*receipt.LeaseID]
}

func paidAmountOf(receipt domain.Receipt) float64 {
	if receipt.PaidAmount != nil {
		return *receipt.PaidAmount
	}
	if receipt.PaidAt != nil && *receipt.PaidAt != "" {
		return receipt.Amount
	}
	return 0
}

func dueMonthKey(dueDate string) string {
	if len(dueDate) >= 7 {
		return strings.ReplaceAll(dueDate[:7], "/", "-")
	}
	return ""
}
