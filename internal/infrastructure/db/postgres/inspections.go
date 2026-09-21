package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db/postgres/sqlcgen"
)

// Inspections implementa domain.InspectionRepository sobre o SQL gerado pelo
// sqlc. Os itens e as fotos são gravados como JSON nas colunas `items`/`photos`,
// serializados pelas funções do domínio.
type Inspections struct {
	queries *sqlcgen.Queries
}

// NewInspections monta o repositório dos autos de vistoria.
func NewInspections(db sqlcgen.DBTX) *Inspections {
	return &Inspections{queries: sqlcgen.New(db)}
}

var _ domain.InspectionRepository = (*Inspections)(nil)

func (r *Inspections) List(ctx context.Context) ([]domain.Inspection, error) {
	rows, err := r.queries.ListInspections(ctx)
	if err != nil {
		return nil, wrapError(err)
	}
	inspections := make([]domain.Inspection, 0, len(rows))
	for _, row := range rows {
		inspection, err := inspectionFromRow(row)
		if err != nil {
			return nil, err
		}
		inspections = append(inspections, inspection)
	}
	return inspections, nil
}

func (r *Inspections) ByID(ctx context.Context, id string) (*domain.Inspection, error) {
	row, err := r.queries.GetInspectionByID(ctx, id)
	if err != nil {
		return nil, wrapError(err)
	}
	inspection, err := inspectionFromRow(sqlcgen.ListInspectionsRow(row))
	if err != nil {
		return nil, err
	}
	return &inspection, nil
}

func (r *Inspections) ByLease(ctx context.Context, leaseID string) ([]domain.Inspection, error) {
	rows, err := r.queries.ListInspectionsByLease(ctx, leaseID)
	if err != nil {
		return nil, wrapError(err)
	}
	inspections := make([]domain.Inspection, 0, len(rows))
	for _, row := range rows {
		inspection, err := inspectionFromRow(sqlcgen.ListInspectionsRow(row))
		if err != nil {
			return nil, err
		}
		inspections = append(inspections, inspection)
	}
	return inspections, nil
}

func (r *Inspections) Create(ctx context.Context, inspection *domain.Inspection) error {
	items, photos, err := encodeInspectionPayload(inspection)
	if err != nil {
		return err
	}
	return wrapError(r.queries.InsertInspection(ctx, sqlcgen.InsertInspectionParams{
		ID:          inspection.ID,
		LeaseID:     inspection.LeaseID,
		Kind:        string(inspection.Kind),
		PerformedAt: inspection.PerformedAt,
		Condition:   string(inspection.Condition),
		Notes:       nullString(inspection.Notes),
		Items:       items,
		Photos:      photos,
		AuthToken:   nullString(inspection.AuthToken),
		CreatedAt:   inspection.CreatedAt,
		UpdatedAt:   inspection.UpdatedAt,
	}))
}

func (r *Inspections) Update(ctx context.Context, inspection *domain.Inspection) error {
	items, photos, err := encodeInspectionPayload(inspection)
	if err != nil {
		return err
	}
	return wrapError(r.queries.UpdateInspection(ctx, sqlcgen.UpdateInspectionParams{
		Kind:        string(inspection.Kind),
		PerformedAt: inspection.PerformedAt,
		Condition:   string(inspection.Condition),
		Notes:       nullString(inspection.Notes),
		Items:       items,
		Photos:      photos,
		UpdatedAt:   inspection.UpdatedAt,
		ID:          inspection.ID,
	}))
}

func (r *Inspections) Delete(ctx context.Context, id string) error {
	return wrapError(r.queries.DeleteInspection(ctx, id))
}

// encodeInspectionPayload serializa itens e fotos: lista vazia vira NULL, para
// que uma vistoria sem fotos fique idêntica a uma gravada antes da funcionalidade.
func encodeInspectionPayload(inspection *domain.Inspection) (items, photos sql.NullString, err error) {
	encodedItems, err := domain.EncodeInspectionItems(inspection.Items)
	if err != nil {
		return items, photos, fmt.Errorf("serializar itens da vistoria %q: %w", inspection.ID, err)
	}
	encodedPhotos, err := domain.EncodeInspectionPhotos(inspection.Photos)
	if err != nil {
		return items, photos, fmt.Errorf("serializar fotos da vistoria %q: %w", inspection.ID, err)
	}
	return nullString(encodedItems), nullString(encodedPhotos), nil
}

// inspectionFromRow converte a linha do banco na entidade de domínio. As três
// consultas de vistoria devolvem exatamente as mesmas colunas, então o mesmo
// conversor (e a conversão de tipo entre as linhas) serve para todas.
func inspectionFromRow(row sqlcgen.ListInspectionsRow) (domain.Inspection, error) {
	items, err := domain.DecodeInspectionItems(stringPtr(row.Items))
	if err != nil {
		return domain.Inspection{}, fmt.Errorf("ler itens da vistoria %q: %w", row.ID, err)
	}
	photos, err := domain.DecodeInspectionPhotos(stringPtr(row.Photos))
	if err != nil {
		return domain.Inspection{}, fmt.Errorf("ler fotos da vistoria %q: %w", row.ID, err)
	}
	return domain.Inspection{
		ID:           row.ID,
		LeaseID:      row.LeaseID,
		Kind:         domain.InspectionKind(row.Kind),
		PerformedAt:  row.PerformedAt,
		Condition:    domain.InspectionCondition(row.Condition),
		Notes:        stringPtr(row.Notes),
		Items:        items,
		Photos:       photos,
		AuthToken:    stringPtr(row.AuthToken),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		TenantName:   row.TenantName,
		PropertyName: row.PropertyName,
	}, nil
}
