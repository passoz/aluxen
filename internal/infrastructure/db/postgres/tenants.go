package postgres

import (
	"context"
	"database/sql"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db/postgres/sqlcgen"
)

// Tenants implementa domain.TenantRepository sobre o SQL gerado pelo sqlc.
type Tenants struct {
	queries *sqlcgen.Queries
}

// NewTenants monta o repositório de locatários.
func NewTenants(db sqlcgen.DBTX) *Tenants {
	return &Tenants{queries: sqlcgen.New(db)}
}

var _ domain.TenantRepository = (*Tenants)(nil)

func (r *Tenants) List(ctx context.Context) ([]domain.Tenant, error) {
	rows, err := r.queries.ListTenants(ctx)
	if err != nil {
		return nil, wrapError(err)
	}
	tenants := make([]domain.Tenant, 0, len(rows))
	for _, row := range rows {
		tenants = append(tenants, tenantFromRow(row))
	}
	return tenants, nil
}

func (r *Tenants) ByID(ctx context.Context, id string) (*domain.Tenant, error) {
	row, err := r.queries.GetTenantByID(ctx, id)
	if err != nil {
		return nil, wrapError(err)
	}
	tenant := tenantFromRow(row)
	return &tenant, nil
}

func (r *Tenants) ByCPF(ctx context.Context, digits string) (*domain.Tenant, error) {
	row, err := r.queries.GetTenantByCPF(ctx, sql.NullString{String: digits, Valid: true})
	if err != nil {
		return nil, wrapError(err)
	}
	tenant := tenantFromRow(row)
	return &tenant, nil
}

func (r *Tenants) Create(ctx context.Context, tenant *domain.Tenant) error {
	return wrapError(r.queries.InsertTenant(ctx, sqlcgen.InsertTenantParams{
		ID:                tenant.ID,
		Name:              tenant.Name,
		Email:             tenant.Email,
		Phone:             nullString(tenant.Phone),
		Cpf:               nullString(tenant.CPF),
		PasswordHash:      nullString(tenant.PasswordHash),
		PasswordChangedAt: nullString(tenant.PasswordChangedAt),
		CreatedAt:         tenant.CreatedAt,
		UpdatedAt:         tenant.UpdatedAt,
	}))
}

func (r *Tenants) Update(ctx context.Context, tenant *domain.Tenant) error {
	return wrapError(r.queries.UpdateTenant(ctx, sqlcgen.UpdateTenantParams{
		Name:      tenant.Name,
		Email:     tenant.Email,
		Phone:     nullString(tenant.Phone),
		Cpf:       nullString(tenant.CPF),
		UpdatedAt: tenant.UpdatedAt,
		ID:        tenant.ID,
	}))
}

func (r *Tenants) UpdatePassword(ctx context.Context, id, passwordHash, changedAt, updatedAt string) error {
	return wrapError(r.queries.UpdateTenantPassword(ctx, sqlcgen.UpdateTenantPasswordParams{
		PasswordHash:      nullText(passwordHash),
		PasswordChangedAt: nullText(changedAt),
		UpdatedAt:         updatedAt,
		ID:                id,
	}))
}

func (r *Tenants) Delete(ctx context.Context, id string) error {
	return wrapError(r.queries.DeleteTenant(ctx, id))
}

func (r *Tenants) Count(ctx context.Context) (int, error) {
	total, err := r.queries.CountTenants(ctx)
	if err != nil {
		return 0, wrapError(err)
	}
	return int(total), nil
}

// tenantFromRow converte a linha do banco na entidade de domínio.
func tenantFromRow(row sqlcgen.Tenant) domain.Tenant {
	return domain.Tenant{
		ID:                row.ID,
		Name:              row.Name,
		Email:             row.Email,
		Phone:             stringPtr(row.Phone),
		CPF:               stringPtr(row.Cpf),
		PasswordHash:      stringPtr(row.PasswordHash),
		PasswordChangedAt: stringPtr(row.PasswordChangedAt),
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}
