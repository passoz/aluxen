package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Trilha de auditoria — parte compartilhada por todos os casos de uso.
//
// A gravação é best-effort: falha ao auditar nunca desfaz a operação de negócio,
// mas é registrada no log. O diff é montado sobre valores já formatados, e um
// diff vazio não gera linha (evita "salvei sem mudar nada").

// AuditContext identifica quem provocou a mudança e de onde.
type AuditContext struct {
	ActorID   *string
	ActorName *string
	Origin    domain.AuditOrigin
	IP        string
	UserAgent string
	RequestID string
}

// AuditEntry é o evento a registrar.
type AuditEntry struct {
	Action      string
	Entity      string
	EntityID    string
	EntityLabel string
	Summary     string
	Changes     []domain.AuditChange
	Metadata    map[string]any
}

// Record grava o evento na trilha. Devolve erro apenas para falha de gravação.
func Record(ctx context.Context, deps Deps, actx AuditContext, entry AuditEntry) error {
	if len(entry.Changes) == 0 && entry.Metadata == nil {
		return nil
	}

	changesJSON, err := marshalOptional(entry.Changes)
	if err != nil {
		return fmt.Errorf("serializar alterações da auditoria: %w", err)
	}
	metadataJSON, err := marshalOptional(entry.Metadata)
	if err != nil {
		return fmt.Errorf("serializar metadados da auditoria: %w", err)
	}

	origin := actx.Origin
	if origin == "" {
		origin = domain.AuditOriginWeb
	}

	record := &domain.AuditLog{
		ID:          deps.IDs.New(),
		CreatedAt:   format.FormatLocalDateTimeMillis(deps.Clock.Now()),
		ActorID:     actx.ActorID,
		ActorName:   actx.ActorName,
		Action:      entry.Action,
		Entity:      entry.Entity,
		EntityID:    optionalString(entry.EntityID),
		EntityLabel: optionalString(entry.EntityLabel),
		Summary:     entry.Summary,
		Changes:     changesJSON,
		Metadata:    metadataJSON,
		Origin:      origin,
		IP:          optionalString(actx.IP),
		UserAgent:   optionalString(actx.UserAgent),
		RequestID:   optionalString(actx.RequestID),
	}

	if err := deps.Repos.Audit.Insert(ctx, record); err != nil {
		deps.Log.Error("falha ao gravar evento de auditoria",
			"action", entry.Action, "entity", entry.Entity, "entity_id", entry.EntityID, "err", err)
		return fmt.Errorf("gravar auditoria: %w", err)
	}

	deps.Log.Info("evento de auditoria",
		"scope", "audit", "action", entry.Action, "entity", entry.Entity,
		"entity_id", entry.EntityID, "entity_label", entry.EntityLabel,
		"origin", origin, "changes", len(entry.Changes))
	return nil
}

func marshalOptional(value any) (*string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case []domain.AuditChange:
		if len(typed) == 0 {
			return nil, nil
		}
	case map[string]any:
		if len(typed) == 0 {
			return nil, nil
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	text := string(encoded)
	return &text, nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// ── Snapshots para o diff ──────────────────────────────────────────

// TenantSnapshot devolve os campos auditáveis do locatário.
func TenantSnapshot(tenant *domain.Tenant) map[string]string {
	if tenant == nil {
		return nil
	}
	return map[string]string{
		"name":  tenant.Name,
		"email": tenant.Email,
		"phone": domain.Str(tenant.Phone),
		"cpf":   domain.Str(tenant.CPF),
	}
}

// PropertySnapshot devolve os campos auditáveis do imóvel.
func PropertySnapshot(property *domain.Property) map[string]string {
	if property == nil {
		return nil
	}
	return map[string]string{
		"name":    property.Name,
		"address": property.Address,
		"city":    property.City,
		"state":   property.State,
		"zip":     domain.Str(property.Zip),
	}
}

// LeaseSnapshot devolve os campos auditáveis do contrato.
func LeaseSnapshot(lease *domain.Lease) map[string]string {
	if lease == nil {
		return nil
	}
	return map[string]string{
		"tenant_name":   lease.TenantName,
		"property_name": lease.PropertyName,
		"startDate":     lease.StartDate,
		"endDate":       lease.EndDate,
		"dueDay":        strconv.Itoa(lease.DueDay),
		"rentAmount":    domain.FormatNumber(lease.RentAmount),
		"status":        string(lease.Status),
	}
}

// ReceiptSnapshot devolve os campos auditáveis do lançamento.
func ReceiptSnapshot(receipt *domain.Receipt) map[string]string {
	if receipt == nil {
		return nil
	}
	snapshot := map[string]string{
		"tenant_name":   receipt.TenantName,
		"property_name": receipt.PropertyName,
		"category":      string(receipt.Category),
		"description":   domain.Str(receipt.Description),
		"dueDate":       receipt.DueDate,
		"paidAt":        domain.Str(receipt.PaidAt),
		"amount":        domain.FormatNumber(receipt.Amount),
	}
	if receipt.PaidAmount != nil {
		snapshot["paidAmount"] = domain.FormatNumber(*receipt.PaidAmount)
	} else {
		snapshot["paidAmount"] = ""
	}
	return snapshot
}

// DescribeLease monta o rótulo legível do contrato usado na trilha.
func DescribeLease(lease *domain.Lease) string {
	if lease == nil {
		return ""
	}
	return strings.TrimSpace(lease.TenantName + " · " + lease.PropertyName)
}

// DescribeReceipt monta o rótulo legível do lançamento usado na trilha:
// `Inquilino · Imóvel · MM/AAAA`, caindo para a descrição quando não há titular.
func DescribeReceipt(receipt *domain.Receipt) string {
	if receipt == nil {
		return ""
	}
	month := ""
	if len(receipt.DueDate) >= 7 {
		month = receipt.DueDate[5:7] + "/" + receipt.DueDate[0:4]
	}
	tenant := receipt.TenantName
	property := receipt.PropertyName
	if tenant == "" && property == "" {
		if description := domain.Str(receipt.Description); description != "" {
			return description + " · " + month
		}
		return "Competência " + month
	}
	return tenant + " · " + property + " · " + month
}
