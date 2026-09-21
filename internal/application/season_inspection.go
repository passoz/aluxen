package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Auto de vistoria: a peça que resolve conflito de dano na saída da temporada.
// A vistoria pertence a um contrato, tem itens conferidos, fotos anexadas e
// estado geral; o PDF usa o mesmo token de autenticidade dos demais documentos.

// AttachmentStore guarda e devolve arquivos anexados (fotos de vistoria,
// documentos enviados). O port fica aqui porque o caso de uso precisa decidir o
// nome do arquivo; a implementação vive na infraestrutura.
type AttachmentStore interface {
	// Save grava o conteúdo e devolve o nome armazenado.
	Save(prefix, filename string, data []byte) (string, error)
	// Open devolve o conteúdo do arquivo armazenado.
	Open(name string) ([]byte, error)
	// Delete remove o arquivo armazenado (ausente não é erro).
	Delete(name string) error
}

// InspectionInput é o que o painel envia para criar ou editar uma vistoria.
type InspectionInput struct {
	ID          string
	LeaseID     string
	Kind        domain.InspectionKind
	PerformedAt string
	Condition   domain.InspectionCondition
	Notes       string
	Items       []domain.InspectionItem
	Photos      []domain.InspectionPhoto
}

// InspectionView é a vistoria pronta para a tela e para o PDF.
type InspectionView struct {
	Inspection *domain.Inspection
	Lease      *domain.Lease
	KindLabel  string
	Condition  string
}

// InspectionCreate registra um auto de vistoria de entrada ou de saída.
func InspectionCreate(ctx context.Context, deps Deps, actx AuditContext, input InspectionInput) (*domain.Inspection, error) {
	lease, err := deps.Repos.Leases.ByID(ctx, input.LeaseID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.Invalid("Contrato não encontrado.")
		}
		return nil, fmt.Errorf("buscar contrato da vistoria: %w", err)
	}
	if err := domain.ValidateInspection(input.Kind, input.Condition, input.PerformedAt); err != nil {
		return nil, err
	}

	now := deps.Clock.Now()
	inspection := &domain.Inspection{
		ID:          deps.IDs.New(),
		LeaseID:     lease.ID,
		Kind:        input.Kind,
		PerformedAt: input.PerformedAt,
		Condition:   input.Condition,
		Notes:       optionalString(strings.TrimSpace(input.Notes)),
		Items:       input.Items,
		Photos:      input.Photos,
		CreatedAt:   format.FormatUTCStamp(now),
		UpdatedAt:   format.FormatUTCStamp(now),
	}
	if err := deps.Repos.Inspections.Create(ctx, inspection); err != nil {
		return nil, fmt.Errorf("gravar vistoria: %w", err)
	}
	inspection.TenantName = lease.TenantName
	inspection.PropertyName = lease.PropertyName

	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionInspectionCreate,
		Entity:      domain.EntityInspection,
		EntityID:    inspection.ID,
		EntityLabel: DescribeLease(lease),
		Summary:     domain.InspectionKindLabel(input.Kind) + " · " + lease.PropertyName,
		Changes:     domain.BuildAuditChanges(domain.InspectionAuditFields, nil, InspectionSnapshot(inspection)),
		Metadata:    map[string]any{"kind": string(input.Kind), "count": len(input.Items)},
	}); err != nil {
		return nil, err
	}
	return inspection, nil
}

// InspectionUpdate altera uma vistoria existente.
func InspectionUpdate(ctx context.Context, deps Deps, actx AuditContext, input InspectionInput) (*domain.Inspection, error) {
	current, err := deps.Repos.Inspections.ByID(ctx, input.ID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("buscar vistoria: %w", err)
	}
	if err := domain.ValidateInspection(input.Kind, input.Condition, input.PerformedAt); err != nil {
		return nil, err
	}

	before := InspectionSnapshot(current)
	current.Kind = input.Kind
	current.PerformedAt = input.PerformedAt
	current.Condition = input.Condition
	current.Notes = optionalString(strings.TrimSpace(input.Notes))
	current.Items = input.Items
	// Edição sem upload novo preserva as fotos já anexadas: o formulário do
	// painel não reenvia arquivo nenhum quando o locador só corrige um item.
	if len(input.Photos) > 0 {
		current.Photos = input.Photos
	}
	current.UpdatedAt = format.FormatUTCStamp(deps.Clock.Now())

	if err := deps.Repos.Inspections.Update(ctx, current); err != nil {
		return nil, fmt.Errorf("gravar vistoria: %w", err)
	}

	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionInspectionUpdate,
		Entity:      domain.EntityInspection,
		EntityID:    current.ID,
		EntityLabel: DescribeLease(&domain.Lease{TenantName: current.TenantName, PropertyName: current.PropertyName}),
		Summary:     "Vistoria alterada · " + current.PropertyName,
		Changes:     domain.BuildAuditChanges(domain.InspectionAuditFields, before, InspectionSnapshot(current)),
	}); err != nil {
		return nil, err
	}
	return current, nil
}

// InspectionDelete remove a vistoria e as fotos que ela referenciava.
func InspectionDelete(ctx context.Context, deps Deps, actx AuditContext, id string) error {
	current, err := deps.Repos.Inspections.ByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrNotFound
		}
		return fmt.Errorf("buscar vistoria: %w", err)
	}

	if err := deps.Repos.Inspections.Delete(ctx, id); err != nil {
		return fmt.Errorf("excluir vistoria: %w", err)
	}
	if deps.Attachments != nil {
		for _, photo := range current.Photos {
			if err := deps.Attachments.Delete(photo.Name); err != nil {
				deps.Log.Warn("foto de vistoria não removida", "scope", "application", "photo", photo.Name, "err", err)
			}
		}
	}

	return Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionInspectionDelete,
		Entity:      domain.EntityInspection,
		EntityID:    current.ID,
		EntityLabel: DescribeLease(&domain.Lease{TenantName: current.TenantName, PropertyName: current.PropertyName}),
		Summary:     "Vistoria excluída · " + current.PropertyName,
		Changes:     domain.BuildAuditChanges(domain.InspectionAuditFields, InspectionSnapshot(current), nil),
	})
}

// LoadInspection devolve a vistoria com o contrato associado.
func LoadInspection(ctx context.Context, deps Deps, id string) (*InspectionView, error) {
	inspection, err := deps.Repos.Inspections.ByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("buscar vistoria: %w", err)
	}
	return InspectionViewOf(ctx, deps, inspection)
}

// InspectionViewOf monta a visão da vistoria (com o contrato, quando existir).
func InspectionViewOf(ctx context.Context, deps Deps, inspection *domain.Inspection) (*InspectionView, error) {
	view := &InspectionView{
		Inspection: inspection,
		KindLabel:  domain.InspectionKindLabel(inspection.Kind),
		Condition:  domain.InspectionConditionLabel(inspection.Condition),
	}
	lease, err := deps.Repos.Leases.ByID(ctx, inspection.LeaseID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return view, nil
		}
		return nil, fmt.Errorf("buscar contrato da vistoria: %w", err)
	}
	view.Lease = lease
	return view, nil
}

// ListInspections devolve todas as vistorias.
func ListInspections(ctx context.Context, deps Deps) ([]domain.Inspection, error) {
	inspections, err := deps.Repos.Inspections.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listar vistorias: %w", err)
	}
	return inspections, nil
}

// ListInspectionsByLease devolve as vistorias de um contrato.
func ListInspectionsByLease(ctx context.Context, deps Deps, leaseID string) ([]domain.Inspection, error) {
	inspections, err := deps.Repos.Inspections.ByLease(ctx, leaseID)
	if err != nil {
		return nil, fmt.Errorf("listar vistorias do contrato: %w", err)
	}
	return inspections, nil
}

// InspectionSnapshot devolve os campos auditáveis da vistoria.
func InspectionSnapshot(inspection *domain.Inspection) map[string]string {
	if inspection == nil {
		return nil
	}
	return map[string]string{
		"kind":          string(inspection.Kind),
		"performedAt":   inspection.PerformedAt,
		"tenant_name":   inspection.TenantName,
		"property_name": inspection.PropertyName,
		"condition":     string(inspection.Condition),
		"notes":         domain.Str(inspection.Notes),
	}
}
