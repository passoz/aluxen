package domain

import (
	"strconv"
	"strings"

	"github.com/passoz/aluxen/internal/domain/format"
)

// Diff campo a campo da trilha de auditoria. A comparação acontece sobre o valor
// JÁ FORMATADO: campos iguais são omitidos, e o que aparece na tela é exatamente
// o que o locador lê.

// AuditValueFormat descreve como um campo é apresentado no diff.
type AuditValueFormat string

const (
	AuditFormatText            AuditValueFormat = "text"
	AuditFormatCurrency        AuditValueFormat = "currency"
	AuditFormatDate            AuditValueFormat = "date"
	AuditFormatDateTime        AuditValueFormat = "datetime"
	AuditFormatLeaseStatus     AuditValueFormat = "leaseStatus"
	AuditFormatReceiptCategory AuditValueFormat = "receiptCategory"
	AuditFormatDepositStatus   AuditValueFormat = "depositStatus"
	AuditFormatInspectionKind  AuditValueFormat = "inspectionKind"
	AuditFormatCondition       AuditValueFormat = "inspectionCondition"
)

// AuditFieldSpec liga a chave do campo ao rótulo e ao formato de apresentação.
type AuditFieldSpec struct {
	Key    string
	Label  string
	Format AuditValueFormat
}

// AuditChange é uma linha do diff persistida em `audit_logs.changes`.
type AuditChange struct {
	Field string  `json:"field"`
	Label string  `json:"label"`
	From  *string `json:"from"`
	To    *string `json:"to"`
}

// Especificações de campos por entidade.
var (
	TenantAuditFields = []AuditFieldSpec{
		{Key: "name", Label: "Nome", Format: AuditFormatText},
		{Key: "email", Label: "E-mail", Format: AuditFormatText},
		{Key: "phone", Label: "Telefone", Format: AuditFormatText},
		{Key: "cpf", Label: "CPF", Format: AuditFormatText},
	}

	PropertyAuditFields = []AuditFieldSpec{
		{Key: "name", Label: "Nome", Format: AuditFormatText},
		{Key: "address", Label: "Endereço", Format: AuditFormatText},
		{Key: "city", Label: "Cidade", Format: AuditFormatText},
		{Key: "state", Label: "UF", Format: AuditFormatText},
		{Key: "zip", Label: "CEP", Format: AuditFormatText},
	}

	LeaseAuditFields = []AuditFieldSpec{
		{Key: "tenant_name", Label: "Inquilino", Format: AuditFormatText},
		{Key: "property_name", Label: "Imóvel", Format: AuditFormatText},
		{Key: "startDate", Label: "Início", Format: AuditFormatDate},
		{Key: "endDate", Label: "Término", Format: AuditFormatDate},
		{Key: "dueDay", Label: "Dia de vencimento", Format: AuditFormatText},
		{Key: "rentAmount", Label: "Aluguel", Format: AuditFormatCurrency},
		{Key: "status", Label: "Situação", Format: AuditFormatLeaseStatus},
	}

	ReceiptAuditFields = []AuditFieldSpec{
		{Key: "tenant_name", Label: "Inquilino", Format: AuditFormatText},
		{Key: "property_name", Label: "Imóvel", Format: AuditFormatText},
		{Key: "category", Label: "Categoria", Format: AuditFormatReceiptCategory},
		{Key: "description", Label: "Descrição", Format: AuditFormatText},
		{Key: "dueDate", Label: "Vencimento", Format: AuditFormatDate},
		{Key: "paidAt", Label: "Pago em", Format: AuditFormatDateTime},
		{Key: "amount", Label: "Valor", Format: AuditFormatCurrency},
		{Key: "paidAmount", Label: "Valor pago", Format: AuditFormatCurrency},
	}

	LandlordAuditFields = []AuditFieldSpec{
		{Key: "name", Label: "Nome", Format: AuditFormatText},
		{Key: "cpf", Label: "CPF", Format: AuditFormatText},
		{Key: "email", Label: "E-mail", Format: AuditFormatText},
	}

	AppURLAuditFields = []AuditFieldSpec{
		{Key: "url", Label: "URL pública", Format: AuditFormatText},
	}

	DepositAuditFields = []AuditFieldSpec{
		{Key: "amount", Label: "Caução", Format: AuditFormatCurrency},
		{Key: "refundedAmount", Label: "Valor devolvido", Format: AuditFormatCurrency},
		{Key: "status", Label: "Situação", Format: AuditFormatDepositStatus},
		{Key: "refundedAt", Label: "Devolvida em", Format: AuditFormatDateTime},
		{Key: "notes", Label: "Observações", Format: AuditFormatText},
	}

	InspectionAuditFields = []AuditFieldSpec{
		{Key: "kind", Label: "Tipo", Format: AuditFormatInspectionKind},
		{Key: "performedAt", Label: "Realizada em", Format: AuditFormatDateTime},
		{Key: "tenant_name", Label: "Inquilino", Format: AuditFormatText},
		{Key: "property_name", Label: "Imóvel", Format: AuditFormatText},
		{Key: "condition", Label: "Estado geral", Format: AuditFormatCondition},
		{Key: "notes", Label: "Observações", Format: AuditFormatText},
	}
)

// FormatAuditValue formata o valor bruto do campo. Vazio devolve nulo (exibido
// como travessão na tela).
func FormatAuditValue(valueFormat AuditValueFormat, raw string) *string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	var formatted string
	switch valueFormat {
	case AuditFormatCurrency:
		formatted = format.FormatCurrencyFromString(trimmed)
	case AuditFormatDate:
		formatted = format.FormatDateBR(trimmed)
	case AuditFormatDateTime:
		formatted = format.FormatDateTimeBR(trimmed)
	case AuditFormatLeaseStatus:
		formatted = LeaseStatusLabel(LeaseStatus(trimmed))
	case AuditFormatReceiptCategory:
		formatted = ReceiptCategoryLabel(ReceiptCategory(trimmed))
	case AuditFormatDepositStatus:
		formatted = DepositStatusLabel(DepositStatus(trimmed))
	case AuditFormatInspectionKind:
		formatted = InspectionKindLabel(InspectionKind(trimmed))
	case AuditFormatCondition:
		formatted = InspectionConditionLabel(InspectionCondition(trimmed))
	default:
		formatted = trimmed
	}
	return &formatted
}

// BuildAuditChanges compara dois snapshots campo a campo e devolve apenas o que
// mudou. Snapshots ausentes (nil) contam como vazio — criação ou exclusão.
func BuildAuditChanges(specs []AuditFieldSpec, before, after map[string]string) []AuditChange {
	changes := make([]AuditChange, 0, len(specs))
	for _, spec := range specs {
		from := FormatAuditValue(spec.Format, before[spec.Key])
		to := FormatAuditValue(spec.Format, after[spec.Key])
		if sameAuditValue(from, to) {
			continue
		}
		changes = append(changes, AuditChange{Field: spec.Key, Label: spec.Label, From: from, To: to})
	}
	return changes
}

func sameAuditValue(left, right *string) bool {
	if left == nil && right == nil {
		return true
	}
	if left == nil || right == nil {
		return false
	}
	return *left == *right
}

// FormatNumber formata um número para o snapshot de auditoria (sem separador).
func FormatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
