package postgres

import (
	"context"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db/postgres/sqlcgen"
)

// Leases implementa domain.LeaseRepository sobre o SQL gerado pelo sqlc.
type Leases struct {
	queries *sqlcgen.Queries
}

// NewLeases monta o repositório de contratos.
func NewLeases(db sqlcgen.DBTX) *Leases {
	return &Leases{queries: sqlcgen.New(db)}
}

var _ domain.LeaseRepository = (*Leases)(nil)

func (r *Leases) List(ctx context.Context) ([]domain.Lease, error) {
	rows, err := r.queries.ListLeases(ctx)
	if err != nil {
		return nil, wrapError(err)
	}
	return leasesFromRows(rows), nil
}

func (r *Leases) ByID(ctx context.Context, id string) (*domain.Lease, error) {
	row, err := r.queries.GetLeaseByID(ctx, id)
	if err != nil {
		return nil, wrapError(err)
	}
	lease := leaseFromRow(sqlcgen.ListLeasesRow(row))
	return &lease, nil
}

func (r *Leases) ByTenant(ctx context.Context, tenantID string) ([]domain.Lease, error) {
	rows, err := r.queries.ListLeasesByTenant(ctx, tenantID)
	if err != nil {
		return nil, wrapError(err)
	}
	leases := make([]domain.Lease, 0, len(rows))
	for _, row := range rows {
		leases = append(leases, leaseFromRow(sqlcgen.ListLeasesRow(row)))
	}
	return leases, nil
}

func (r *Leases) ByProperty(ctx context.Context, propertyID string) ([]domain.Lease, error) {
	rows, err := r.queries.ListLeasesByProperty(ctx, propertyID)
	if err != nil {
		return nil, wrapError(err)
	}
	leases := make([]domain.Lease, 0, len(rows))
	for _, row := range rows {
		leases = append(leases, leaseFromRow(sqlcgen.ListLeasesRow(row)))
	}
	return leases, nil
}

func (r *Leases) Overlapping(ctx context.Context, propertyID, excludeID, startDate, endDate string) ([]domain.Lease, error) {
	rows, err := r.queries.ListLeasesOverlapping(ctx, sqlcgen.ListLeasesOverlappingParams{
		PropertyID: propertyID,
		ExcludeID:  excludeID,
		EndDate:    endDate,
		StartDate:  startDate,
	})
	if err != nil {
		return nil, wrapError(err)
	}
	leases := make([]domain.Lease, 0, len(rows))
	for _, row := range rows {
		leases = append(leases, leaseFromRow(sqlcgen.ListLeasesRow(row)))
	}
	return leases, nil
}

func (r *Leases) ActiveInRange(ctx context.Context, startDate, endDate string) ([]domain.Lease, error) {
	rows, err := r.queries.ListLeasesActiveInRange(ctx, sqlcgen.ListLeasesActiveInRangeParams{
		EndDate:   endDate,
		StartDate: startDate,
	})
	if err != nil {
		return nil, wrapError(err)
	}
	leases := make([]domain.Lease, 0, len(rows))
	for _, row := range rows {
		leases = append(leases, leaseFromRow(sqlcgen.ListLeasesRow(row)))
	}
	return leases, nil
}

func (r *Leases) Create(ctx context.Context, lease *domain.Lease) error {
	return wrapError(r.queries.InsertLease(ctx, sqlcgen.InsertLeaseParams{
		ID:                    lease.ID,
		TenantID:              lease.TenantID,
		PropertyID:            lease.PropertyID,
		StartDate:             lease.StartDate,
		EndDate:               lease.EndDate,
		DueDay:                int32(dueDayOrDefault(lease.DueDay)),
		RentAmount:            lease.RentAmount,
		Status:                leaseStatusOrDefault(lease.Status),
		AuthToken:             nullString(lease.AuthToken),
		DepositAmount:         nullFloat(lease.DepositAmount),
		DepositStatus:         depositStatusColumn(lease.DepositStatus),
		DepositReceivedAt:     nullString(lease.DepositReceivedAt),
		DepositRefundedAt:     nullString(lease.DepositRefundedAt),
		DepositRefundedAmount: nullFloat(lease.DepositRefundedAmt),
		DepositNotes:          nullString(lease.DepositNotes),
		CreatedAt:             lease.CreatedAt,
		UpdatedAt:             lease.UpdatedAt,
	}))
}

func (r *Leases) Update(ctx context.Context, lease *domain.Lease) error {
	return wrapError(r.queries.UpdateLease(ctx, sqlcgen.UpdateLeaseParams{
		TenantID:              lease.TenantID,
		PropertyID:            lease.PropertyID,
		StartDate:             lease.StartDate,
		EndDate:               lease.EndDate,
		DueDay:                int32(dueDayOrDefault(lease.DueDay)),
		RentAmount:            lease.RentAmount,
		Status:                leaseStatusOrDefault(lease.Status),
		DepositAmount:         nullFloat(lease.DepositAmount),
		DepositStatus:         depositStatusColumn(lease.DepositStatus),
		DepositReceivedAt:     nullString(lease.DepositReceivedAt),
		DepositRefundedAt:     nullString(lease.DepositRefundedAt),
		DepositRefundedAmount: nullFloat(lease.DepositRefundedAmt),
		DepositNotes:          nullString(lease.DepositNotes),
		UpdatedAt:             lease.UpdatedAt,
		ID:                    lease.ID,
	}))
}

func (r *Leases) UpdateStatus(ctx context.Context, id string, status domain.LeaseStatus, updatedAt string) error {
	return wrapError(r.queries.UpdateLeaseStatus(ctx, sqlcgen.UpdateLeaseStatusParams{
		Status:    string(status),
		UpdatedAt: updatedAt,
		ID:        id,
	}))
}

func (r *Leases) Delete(ctx context.Context, id string) error {
	return wrapError(r.queries.DeleteLease(ctx, id))
}

func (r *Leases) Count(ctx context.Context) (int, error) {
	total, err := r.queries.CountLeases(ctx)
	if err != nil {
		return 0, wrapError(err)
	}
	return int(total), nil
}

func (r *Leases) CountByStatus(ctx context.Context, status domain.LeaseStatus) (int, error) {
	total, err := r.queries.CountLeasesByStatus(ctx, string(status))
	if err != nil {
		return 0, wrapError(err)
	}
	return int(total), nil
}

func leasesFromRows(rows []sqlcgen.ListLeasesRow) []domain.Lease {
	leases := make([]domain.Lease, 0, len(rows))
	for _, row := range rows {
		leases = append(leases, leaseFromRow(row))
	}
	return leases
}

// leaseFromRow converte a linha do banco na entidade de domínio. Todas as
// consultas de contrato devolvem as mesmas colunas (`l.*` mais os nomes do
// join), então este conversor serve para todas e as demais linhas são
// convertidas para este tipo.
func leaseFromRow(row sqlcgen.ListLeasesRow) domain.Lease {
	return domain.Lease{
		ID:                 row.ID,
		TenantID:           row.TenantID,
		PropertyID:         row.PropertyID,
		StartDate:          row.StartDate,
		EndDate:            row.EndDate,
		DueDay:             int(row.DueDay),
		RentAmount:         row.RentAmount,
		Status:             domain.LeaseStatus(row.Status),
		AuthToken:          stringPtr(row.AuthToken),
		DepositAmount:      floatPtr(row.DepositAmount),
		DepositStatus:      depositStatusFromColumn(row.DepositStatus),
		DepositReceivedAt:  stringPtr(row.DepositReceivedAt),
		DepositRefundedAt:  stringPtr(row.DepositRefundedAt),
		DepositRefundedAmt: floatPtr(row.DepositRefundedAmount),
		DepositNotes:       stringPtr(row.DepositNotes),
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
		TenantName:         row.TenantName,
		PropertyName:       row.PropertyName,
	}
}
