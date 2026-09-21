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
	leases := make([]domain.Lease, 0, len(rows))
	for _, row := range rows {
		leases = append(leases, leaseFromListRow(row))
	}
	return leases, nil
}

func (r *Leases) ByID(ctx context.Context, id string) (*domain.Lease, error) {
	row, err := r.queries.GetLeaseByID(ctx, id)
	if err != nil {
		return nil, wrapError(err)
	}
	lease := leaseFromGetRow(row)
	return &lease, nil
}

func (r *Leases) ByTenant(ctx context.Context, tenantID string) ([]domain.Lease, error) {
	rows, err := r.queries.ListLeasesByTenant(ctx, tenantID)
	if err != nil {
		return nil, wrapError(err)
	}
	leases := make([]domain.Lease, 0, len(rows))
	for _, row := range rows {
		leases = append(leases, leaseFromTenantRow(row))
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
		leases = append(leases, leaseFromPropertyRow(row))
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
		leases = append(leases, leaseFromOverlapRow(row))
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
		leases = append(leases, leaseFromRangeRow(row))
	}
	return leases, nil
}

func (r *Leases) Create(ctx context.Context, lease *domain.Lease) error {
	return wrapError(r.queries.InsertLease(ctx, sqlcgen.InsertLeaseParams{
		ID:         lease.ID,
		TenantID:   lease.TenantID,
		PropertyID: lease.PropertyID,
		StartDate:  lease.StartDate,
		EndDate:    lease.EndDate,
		DueDay:     int32(dueDayOrDefault(lease.DueDay)),
		RentAmount: lease.RentAmount,
		Status:     leaseStatusOrDefault(lease.Status),
		AuthToken:  nullString(lease.AuthToken),
		CreatedAt:  lease.CreatedAt,
		UpdatedAt:  lease.UpdatedAt,
	}))
}

func (r *Leases) Update(ctx context.Context, lease *domain.Lease) error {
	return wrapError(r.queries.UpdateLease(ctx, sqlcgen.UpdateLeaseParams{
		TenantID:   lease.TenantID,
		PropertyID: lease.PropertyID,
		StartDate:  lease.StartDate,
		EndDate:    lease.EndDate,
		DueDay:     int32(dueDayOrDefault(lease.DueDay)),
		RentAmount: lease.RentAmount,
		Status:     leaseStatusOrDefault(lease.Status),
		UpdatedAt:  lease.UpdatedAt,
		ID:         lease.ID,
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

func leaseFromListRow(row sqlcgen.ListLeasesRow) domain.Lease {
	return domain.Lease{
		ID:           row.ID,
		TenantID:     row.TenantID,
		PropertyID:   row.PropertyID,
		StartDate:    row.StartDate,
		EndDate:      row.EndDate,
		DueDay:       int(row.DueDay),
		RentAmount:   row.RentAmount,
		Status:       domain.LeaseStatus(row.Status),
		AuthToken:    stringPtr(row.AuthToken),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		TenantName:   row.TenantName,
		PropertyName: row.PropertyName,
	}
}

func leaseFromGetRow(row sqlcgen.GetLeaseByIDRow) domain.Lease {
	return domain.Lease{
		ID:           row.ID,
		TenantID:     row.TenantID,
		PropertyID:   row.PropertyID,
		StartDate:    row.StartDate,
		EndDate:      row.EndDate,
		DueDay:       int(row.DueDay),
		RentAmount:   row.RentAmount,
		Status:       domain.LeaseStatus(row.Status),
		AuthToken:    stringPtr(row.AuthToken),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		TenantName:   row.TenantName,
		PropertyName: row.PropertyName,
	}
}

func leaseFromTenantRow(row sqlcgen.ListLeasesByTenantRow) domain.Lease {
	return domain.Lease{
		ID:           row.ID,
		TenantID:     row.TenantID,
		PropertyID:   row.PropertyID,
		StartDate:    row.StartDate,
		EndDate:      row.EndDate,
		DueDay:       int(row.DueDay),
		RentAmount:   row.RentAmount,
		Status:       domain.LeaseStatus(row.Status),
		AuthToken:    stringPtr(row.AuthToken),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		TenantName:   row.TenantName,
		PropertyName: row.PropertyName,
	}
}

func leaseFromPropertyRow(row sqlcgen.ListLeasesByPropertyRow) domain.Lease {
	return domain.Lease{
		ID:           row.ID,
		TenantID:     row.TenantID,
		PropertyID:   row.PropertyID,
		StartDate:    row.StartDate,
		EndDate:      row.EndDate,
		DueDay:       int(row.DueDay),
		RentAmount:   row.RentAmount,
		Status:       domain.LeaseStatus(row.Status),
		AuthToken:    stringPtr(row.AuthToken),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		TenantName:   row.TenantName,
		PropertyName: row.PropertyName,
	}
}

func leaseFromOverlapRow(row sqlcgen.ListLeasesOverlappingRow) domain.Lease {
	return domain.Lease{
		ID:           row.ID,
		TenantID:     row.TenantID,
		PropertyID:   row.PropertyID,
		StartDate:    row.StartDate,
		EndDate:      row.EndDate,
		DueDay:       int(row.DueDay),
		RentAmount:   row.RentAmount,
		Status:       domain.LeaseStatus(row.Status),
		AuthToken:    stringPtr(row.AuthToken),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		TenantName:   row.TenantName,
		PropertyName: row.PropertyName,
	}
}

func leaseFromRangeRow(row sqlcgen.ListLeasesActiveInRangeRow) domain.Lease {
	return domain.Lease{
		ID:           row.ID,
		TenantID:     row.TenantID,
		PropertyID:   row.PropertyID,
		StartDate:    row.StartDate,
		EndDate:      row.EndDate,
		DueDay:       int(row.DueDay),
		RentAmount:   row.RentAmount,
		Status:       domain.LeaseStatus(row.Status),
		AuthToken:    stringPtr(row.AuthToken),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		TenantName:   row.TenantName,
		PropertyName: row.PropertyName,
	}
}
