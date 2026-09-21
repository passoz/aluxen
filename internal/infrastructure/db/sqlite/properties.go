package sqlite

import (
	"context"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db/sqlite/sqlcgen"
)

// Properties implementa domain.PropertyRepository sobre o SQL gerado pelo sqlc.
type Properties struct {
	queries *sqlcgen.Queries
}

// NewProperties monta o repositório de imóveis.
func NewProperties(db sqlcgen.DBTX) *Properties {
	return &Properties{queries: sqlcgen.New(db)}
}

var _ domain.PropertyRepository = (*Properties)(nil)

func (r *Properties) List(ctx context.Context) ([]domain.Property, error) {
	rows, err := r.queries.ListProperties(ctx)
	if err != nil {
		return nil, wrapError(err)
	}
	properties := make([]domain.Property, 0, len(rows))
	for _, row := range rows {
		properties = append(properties, propertyFromRow(row))
	}
	return properties, nil
}

func (r *Properties) ByID(ctx context.Context, id string) (*domain.Property, error) {
	row, err := r.queries.GetPropertyByID(ctx, id)
	if err != nil {
		return nil, wrapError(err)
	}
	property := propertyFromRow(row)
	return &property, nil
}

func (r *Properties) Create(ctx context.Context, property *domain.Property) error {
	return wrapError(r.queries.InsertProperty(ctx, sqlcgen.InsertPropertyParams{
		ID:        property.ID,
		Name:      property.Name,
		Address:   property.Address,
		City:      property.City,
		State:     property.State,
		Zip:       nullString(property.Zip),
		CreatedAt: property.CreatedAt,
		UpdatedAt: property.UpdatedAt,
	}))
}

func (r *Properties) Update(ctx context.Context, property *domain.Property) error {
	return wrapError(r.queries.UpdateProperty(ctx, sqlcgen.UpdatePropertyParams{
		Name:      property.Name,
		Address:   property.Address,
		City:      property.City,
		State:     property.State,
		Zip:       nullString(property.Zip),
		UpdatedAt: property.UpdatedAt,
		ID:        property.ID,
	}))
}

func (r *Properties) Delete(ctx context.Context, id string) error {
	return wrapError(r.queries.DeleteProperty(ctx, id))
}

func (r *Properties) Count(ctx context.Context) (int, error) {
	total, err := r.queries.CountProperties(ctx)
	if err != nil {
		return 0, wrapError(err)
	}
	return int(total), nil
}

// propertyFromRow converte a linha do banco na entidade de domínio.
func propertyFromRow(row sqlcgen.Property) domain.Property {
	return domain.Property{
		ID:        row.ID,
		Name:      row.Name,
		Address:   row.Address,
		City:      row.City,
		State:     row.State,
		Zip:       stringPtr(row.Zip),
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}
