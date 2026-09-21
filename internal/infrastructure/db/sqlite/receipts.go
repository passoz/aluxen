package sqlite

import (
	"context"
	"database/sql"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db/sqlite/sqlcgen"
)

// Receipts implementa domain.ReceiptRepository sobre o SQL gerado pelo sqlc.
type Receipts struct {
	queries *sqlcgen.Queries
}

// NewReceipts monta o repositório de lançamentos.
func NewReceipts(db sqlcgen.DBTX) *Receipts {
	return &Receipts{queries: sqlcgen.New(db)}
}

var _ domain.ReceiptRepository = (*Receipts)(nil)

func (r *Receipts) List(ctx context.Context) ([]domain.Receipt, error) {
	rows, err := r.queries.ListReceipts(ctx)
	if err != nil {
		return nil, wrapError(err)
	}
	receipts := make([]domain.Receipt, 0, len(rows))
	for _, row := range rows {
		receipts = append(receipts, receiptFromListRow(row))
	}
	return receipts, nil
}

func (r *Receipts) ByID(ctx context.Context, id string) (*domain.Receipt, error) {
	row, err := r.queries.GetReceiptByID(ctx, id)
	if err != nil {
		return nil, wrapError(err)
	}
	receipt := receiptFromGetRow(row)
	return &receipt, nil
}

func (r *Receipts) ByTenant(ctx context.Context, tenantID string) ([]domain.Receipt, error) {
	rows, err := r.queries.ListReceiptsByTenant(ctx, sql.NullString{String: tenantID, Valid: true})
	if err != nil {
		return nil, wrapError(err)
	}
	receipts := make([]domain.Receipt, 0, len(rows))
	for _, row := range rows {
		receipts = append(receipts, receiptFromTenantRow(row))
	}
	return receipts, nil
}

func (r *Receipts) ByLease(ctx context.Context, leaseID string) ([]domain.Receipt, error) {
	rows, err := r.queries.ListReceiptsByLease(ctx, sql.NullString{String: leaseID, Valid: true})
	if err != nil {
		return nil, wrapError(err)
	}
	receipts := make([]domain.Receipt, 0, len(rows))
	for _, row := range rows {
		receipts = append(receipts, receiptFromLeaseRow(row))
	}
	return receipts, nil
}

func (r *Receipts) Create(ctx context.Context, receipt *domain.Receipt) error {
	return wrapError(r.queries.InsertReceipt(ctx, insertReceiptParams(receipt)))
}

// CreateIfAbsent insere ignorando o conflito na chave de geração mensal e
// devolve quantas linhas entraram (0 = o lançamento já existia).
func (r *Receipts) CreateIfAbsent(ctx context.Context, receipt *domain.Receipt) (int64, error) {
	rows, err := r.queries.InsertReceiptIfAbsent(ctx, sqlcgen.InsertReceiptIfAbsentParams(insertReceiptParams(receipt)))
	return rows, wrapError(err)
}

func (r *Receipts) Update(ctx context.Context, receipt *domain.Receipt) error {
	return wrapError(r.queries.UpdateReceipt(ctx, sqlcgen.UpdateReceiptParams{
		LeaseID:     nullString(receipt.LeaseID),
		TenantID:    nullString(receipt.TenantID),
		PropertyID:  nullString(receipt.PropertyID),
		Category:    categoryOrDefault(receipt.Category),
		Description: nullString(receipt.Description),
		DueDate:     receipt.DueDate,
		PaidAt:      nullString(receipt.PaidAt),
		Amount:      receipt.Amount,
		PaidAmount:  nullFloat(receipt.PaidAmount),
		UpdatedAt:   nullString(receipt.UpdatedAt),
		ID:          receipt.ID,
	}))
}

func (r *Receipts) UpdatePayment(ctx context.Context, id, paidAt string, paidAmount float64, updatedAt string) error {
	return wrapError(r.queries.UpdateReceiptPayment(ctx, sqlcgen.UpdateReceiptPaymentParams{
		PaidAt:     nullText(paidAt),
		PaidAmount: sql.NullFloat64{Float64: paidAmount, Valid: true},
		UpdatedAt:  nullText(updatedAt),
		ID:         id,
	}))
}

func (r *Receipts) Delete(ctx context.Context, id string) error {
	return wrapError(r.queries.DeleteReceipt(ctx, id))
}

func (r *Receipts) Count(ctx context.Context) (int, error) {
	total, err := r.queries.CountReceipts(ctx)
	if err != nil {
		return 0, wrapError(err)
	}
	return int(total), nil
}

// insertReceiptParams monta as colunas de inserção (a mesma lista serve para a
// inserção simples e para a idempotente).
func insertReceiptParams(receipt *domain.Receipt) sqlcgen.InsertReceiptParams {
	return sqlcgen.InsertReceiptParams{
		ID:           receipt.ID,
		LeaseID:      nullString(receipt.LeaseID),
		TenantID:     nullString(receipt.TenantID),
		PropertyID:   nullString(receipt.PropertyID),
		Category:     categoryOrDefault(receipt.Category),
		Description:  nullString(receipt.Description),
		DueDate:      receipt.DueDate,
		PaidAt:       nullString(receipt.PaidAt),
		Amount:       receipt.Amount,
		PaidAmount:   nullFloat(receipt.PaidAmount),
		PdfPath:      nullString(receipt.PdfPath),
		GeneratedKey: nullString(receipt.GeneratedKey),
		AuthToken:    nullString(receipt.AuthToken),
		CreatedAt:    receipt.CreatedAt,
		UpdatedAt:    nullString(receipt.UpdatedAt),
	}
}

func receiptFromListRow(row sqlcgen.ListReceiptsRow) domain.Receipt {
	return domain.Receipt{
		ID:           row.ID,
		LeaseID:      stringPtr(row.LeaseID),
		TenantID:     stringPtr(row.TenantID),
		PropertyID:   stringPtr(row.PropertyID),
		Category:     domain.ReceiptCategory(row.Category),
		Description:  stringPtr(row.Description),
		DueDate:      row.DueDate,
		PaidAt:       stringPtr(row.PaidAt),
		Amount:       row.Amount,
		PaidAmount:   floatPtr(row.PaidAmount),
		PdfPath:      stringPtr(row.PdfPath),
		GeneratedKey: stringPtr(row.GeneratedKey),
		AuthToken:    stringPtr(row.AuthToken),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    stringPtr(row.UpdatedAt),
		TenantName:   row.TenantName,
		PropertyName: row.PropertyName,
	}
}

func receiptFromGetRow(row sqlcgen.GetReceiptByIDRow) domain.Receipt {
	return domain.Receipt{
		ID:           row.ID,
		LeaseID:      stringPtr(row.LeaseID),
		TenantID:     stringPtr(row.TenantID),
		PropertyID:   stringPtr(row.PropertyID),
		Category:     domain.ReceiptCategory(row.Category),
		Description:  stringPtr(row.Description),
		DueDate:      row.DueDate,
		PaidAt:       stringPtr(row.PaidAt),
		Amount:       row.Amount,
		PaidAmount:   floatPtr(row.PaidAmount),
		PdfPath:      stringPtr(row.PdfPath),
		GeneratedKey: stringPtr(row.GeneratedKey),
		AuthToken:    stringPtr(row.AuthToken),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    stringPtr(row.UpdatedAt),
		TenantName:   row.TenantName,
		PropertyName: row.PropertyName,
	}
}

func receiptFromTenantRow(row sqlcgen.ListReceiptsByTenantRow) domain.Receipt {
	return domain.Receipt{
		ID:           row.ID,
		LeaseID:      stringPtr(row.LeaseID),
		TenantID:     stringPtr(row.TenantID),
		PropertyID:   stringPtr(row.PropertyID),
		Category:     domain.ReceiptCategory(row.Category),
		Description:  stringPtr(row.Description),
		DueDate:      row.DueDate,
		PaidAt:       stringPtr(row.PaidAt),
		Amount:       row.Amount,
		PaidAmount:   floatPtr(row.PaidAmount),
		PdfPath:      stringPtr(row.PdfPath),
		GeneratedKey: stringPtr(row.GeneratedKey),
		AuthToken:    stringPtr(row.AuthToken),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    stringPtr(row.UpdatedAt),
		TenantName:   row.TenantName,
		PropertyName: row.PropertyName,
	}
}

func receiptFromLeaseRow(row sqlcgen.ListReceiptsByLeaseRow) domain.Receipt {
	return domain.Receipt{
		ID:           row.ID,
		LeaseID:      stringPtr(row.LeaseID),
		TenantID:     stringPtr(row.TenantID),
		PropertyID:   stringPtr(row.PropertyID),
		Category:     domain.ReceiptCategory(row.Category),
		Description:  stringPtr(row.Description),
		DueDate:      row.DueDate,
		PaidAt:       stringPtr(row.PaidAt),
		Amount:       row.Amount,
		PaidAmount:   floatPtr(row.PaidAmount),
		PdfPath:      stringPtr(row.PdfPath),
		GeneratedKey: stringPtr(row.GeneratedKey),
		AuthToken:    stringPtr(row.AuthToken),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    stringPtr(row.UpdatedAt),
		TenantName:   row.TenantName,
		PropertyName: row.PropertyName,
	}
}
