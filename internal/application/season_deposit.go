package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Caução de temporada: registrar o valor retido, devolver (total ou parcial) e
// registrar a devolução na trilha financeira. Contrato sem caução continua
// idêntico ao de locação longa — nenhum campo novo é obrigatório.

// DepositInput é o registro (ou a correção) da caução de um contrato.
type DepositInput struct {
	LeaseID    string
	Amount     float64
	ReceivedAt string
	Notes      string
}

// DepositView é o estado da caução para a tela e para o recibo.
type DepositView struct {
	Lease        *domain.Lease
	Amount       float64
	Status       domain.DepositStatus
	ReceivedAt   string
	RefundedAt   string
	Refunded     float64
	Outstanding  float64
	Notes        string
	TenantName   string
	PropertyName string
}

// SetLeaseDeposit registra a caução recebida de um contrato de temporada.
func SetLeaseDeposit(ctx context.Context, deps Deps, actx AuditContext, input DepositInput) (*domain.Lease, error) {
	lease, err := deps.Repos.Leases.ByID(ctx, input.LeaseID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.Invalid("Contrato não encontrado.")
		}
		return nil, fmt.Errorf("buscar contrato: %w", err)
	}
	if input.Amount <= 0 {
		return nil, domain.Invalid("Informe o valor da caução.")
	}
	if lease.DepositStatus == domain.DepositStatusRefunded || lease.DepositStatus == domain.DepositStatusPartiallyRefunded {
		return nil, domain.Invalid("A caução deste contrato já foi devolvida.")
	}

	before := depositSnapshot(lease)
	receivedAt := input.ReceivedAt
	if receivedAt == "" {
		receivedAt = format.FormatLocalDateTime(deps.Clock.Now())
	}

	lease.DepositAmount = domain.FloatPtr(input.Amount)
	lease.DepositStatus = domain.DepositStatusHeld
	lease.DepositReceivedAt = domain.StrPtr(receivedAt)
	lease.DepositNotes = optionalString(input.Notes)
	lease.UpdatedAt = format.FormatUTCStamp(deps.Clock.Now())

	if err := deps.Repos.Leases.Update(ctx, lease); err != nil {
		return nil, fmt.Errorf("gravar caução: %w", err)
	}

	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionLeaseUpdate,
		Entity:      domain.EntityLease,
		EntityID:    lease.ID,
		EntityLabel: DescribeLease(lease),
		Summary:     "Caução registrada",
		Changes:     domain.BuildAuditChanges(domain.DepositAuditFields, before, depositSnapshot(lease)),
		Metadata:    map[string]any{"amount": domain.FormatNumber(input.Amount), "kind": "deposit"},
	}); err != nil {
		return nil, err
	}
	return lease, nil
}

// RefundDeposit devolve a caução (total ou parcialmente) e registra a devolução
// na trilha financeira, gerando o lançamento que sustenta o recibo em PDF.
func RefundDeposit(ctx context.Context, deps Deps, actx AuditContext, leaseID string, requested float64, notes string) (*DepositView, error) {
	lease, err := deps.Repos.Leases.ByID(ctx, leaseID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.Invalid("Contrato não encontrado.")
		}
		return nil, fmt.Errorf("buscar contrato: %w", err)
	}

	held := domain.Float(lease.DepositAmount)
	if lease.DepositStatus != domain.DepositStatusHeld {
		return nil, domain.Invalid("Este contrato não tem caução retida para devolver.")
	}

	refund, err := domain.ComputeDepositRefund(held, requested)
	if err != nil {
		return nil, err
	}

	now := deps.Clock.Now()
	before := depositSnapshot(lease)
	lease.DepositStatus = refund.Status
	lease.DepositRefundedAt = domain.StrPtr(format.FormatLocalDateTime(now))
	lease.DepositRefundedAmt = domain.FloatPtr(refund.RefundedAmount)
	if notes != "" {
		lease.DepositNotes = domain.StrPtr(notes)
	}
	lease.UpdatedAt = format.FormatUTCStamp(now)

	if err := deps.Repos.Leases.Update(ctx, lease); err != nil {
		return nil, fmt.Errorf("gravar devolução da caução: %w", err)
	}

	if refund.RefundedAmount > 0 {
		receipt := &domain.Receipt{
			ID:          deps.IDs.New(),
			LeaseID:     domain.StrPtr(lease.ID),
			TenantID:    domain.StrPtr(lease.TenantID),
			PropertyID:  domain.StrPtr(lease.PropertyID),
			Category:    domain.ReceiptCategoryDepositRefund,
			Description: domain.StrPtr("Devolução de caução · " + lease.PropertyName),
			DueDate:     format.FormatLocalDate(now),
			PaidAt:      domain.StrPtr(format.FormatLocalDateTime(now)),
			Amount:      refund.RefundedAmount,
			PaidAmount:  domain.FloatPtr(refund.RefundedAmount),
			CreatedAt:   format.FormatUTCStamp(now),
		}
		if err := deps.Repos.Receipts.Create(ctx, receipt); err != nil {
			return nil, fmt.Errorf("lançar devolução da caução: %w", err)
		}
	}

	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionDepositRefund,
		Entity:      domain.EntityDeposit,
		EntityID:    lease.ID,
		EntityLabel: DescribeLease(lease),
		Summary:     "Caução devolvida · " + domain.FormatNumber(refund.RefundedAmount),
		Changes:     domain.BuildAuditChanges(domain.DepositAuditFields, before, depositSnapshot(lease)),
		Metadata: map[string]any{
			"amount": domain.FormatNumber(held),
			"count":  1,
		},
	}); err != nil {
		return nil, err
	}

	return DepositViewOf(lease), nil
}

// LoadDeposit devolve o estado da caução do contrato.
func LoadDeposit(ctx context.Context, deps Deps, leaseID string) (*DepositView, error) {
	lease, err := deps.Repos.Leases.ByID(ctx, leaseID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("buscar contrato: %w", err)
	}
	return DepositViewOf(lease), nil
}

// DepositViewOf monta a visão da caução a partir do contrato.
func DepositViewOf(lease *domain.Lease) *DepositView {
	amount := domain.Float(lease.DepositAmount)
	refunded := domain.Float(lease.DepositRefundedAmt)
	outstanding := amount - refunded
	if outstanding < 0 {
		outstanding = 0
	}
	status := lease.DepositStatus
	if status == "" {
		status = domain.DepositStatusNone
	}
	return &DepositView{
		Lease:        lease,
		Amount:       amount,
		Status:       status,
		ReceivedAt:   domain.Str(lease.DepositReceivedAt),
		RefundedAt:   domain.Str(lease.DepositRefundedAt),
		Refunded:     refunded,
		Outstanding:  outstanding,
		Notes:        domain.Str(lease.DepositNotes),
		TenantName:   lease.TenantName,
		PropertyName: lease.PropertyName,
	}
}

func depositSnapshot(lease *domain.Lease) map[string]string {
	if lease == nil {
		return nil
	}
	snapshot := map[string]string{
		"status":     string(lease.DepositStatus),
		"refundedAt": domain.Str(lease.DepositRefundedAt),
		"notes":      domain.Str(lease.DepositNotes),
	}
	if lease.DepositAmount != nil {
		snapshot["amount"] = domain.FormatNumber(*lease.DepositAmount)
	} else {
		snapshot["amount"] = ""
	}
	if lease.DepositRefundedAmt != nil {
		snapshot["refundedAmount"] = domain.FormatNumber(*lease.DepositRefundedAmt)
	} else {
		snapshot["refundedAmount"] = ""
	}
	return snapshot
}
