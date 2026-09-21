package sqlite

import (
	"context"
	"database/sql"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db/sqlite/sqlcgen"
)

// PaymentProofs implementa domain.PaymentProofRepository sobre o SQL gerado pelo
// sqlc: guarda o comprovante recebido, o que a leitura extraiu e a decisão.
type PaymentProofs struct {
	queries *sqlcgen.Queries
}

// NewPaymentProofs monta o repositório dos comprovantes.
func NewPaymentProofs(db sqlcgen.DBTX) *PaymentProofs {
	return &PaymentProofs{queries: sqlcgen.New(db)}
}

var _ domain.PaymentProofRepository = (*PaymentProofs)(nil)

func (r *PaymentProofs) Create(ctx context.Context, proof *domain.PaymentProof) error {
	return wrapError(r.queries.InsertPaymentProof(ctx, sqlcgen.InsertPaymentProofParams{
		ID:                  proof.ID,
		CreatedAt:           proof.CreatedAt,
		UpdatedAt:           proof.UpdatedAt,
		Source:              string(proof.Source),
		Phone:               nullString(proof.Phone),
		TenantID:            nullString(proof.TenantID),
		ReceiptID:           nullString(proof.ReceiptID),
		FileName:            nullString(proof.FileName),
		FilePath:            nullString(proof.FilePath),
		Mime:                nullString(proof.Mime),
		ExtractedAmount:     nullFloat(proof.ExtractedAmount),
		ExtractedDate:       nullString(proof.ExtractedDate),
		ExtractedPayer:      nullString(proof.ExtractedPayer),
		ExtractedPayee:      nullString(proof.ExtractedPayee),
		ExtractedIdentifier: nullString(proof.ExtractedIdentifier),
		Confidence:          nullFloat(proof.Confidence),
		Status:              string(proof.Status),
		Decision:            string(proof.Decision),
		DecisionReason:      nullString(proof.DecisionReason),
		DecidedBy:           nullString(proof.DecidedBy),
		DecidedAt:           nullString(proof.DecidedAt),
		RawResponse:         nullString(proof.RawResponse),
	}))
}

func (r *PaymentProofs) ByID(ctx context.Context, id string) (*domain.PaymentProof, error) {
	row, err := r.queries.GetPaymentProofByID(ctx, id)
	if err != nil {
		return nil, wrapError(err)
	}
	proof := paymentProofFromRow(row)
	return &proof, nil
}

func (r *PaymentProofs) ListByStatus(ctx context.Context, status domain.PaymentProofStatus, limit int) ([]domain.PaymentProof, error) {
	rows, err := r.queries.ListPaymentProofsByStatus(ctx, sqlcgen.ListPaymentProofsByStatusParams{
		Status:    string(status),
		PageLimit: int64(limit),
	})
	if err != nil {
		return nil, wrapError(err)
	}
	return paymentProofsFromRows(rows), nil
}

func (r *PaymentProofs) List(ctx context.Context, limit int) ([]domain.PaymentProof, error) {
	rows, err := r.queries.ListPaymentProofs(ctx, int64(limit))
	if err != nil {
		return nil, wrapError(err)
	}
	return paymentProofsFromRows(rows), nil
}

func (r *PaymentProofs) ByReceipt(ctx context.Context, receiptID string) ([]domain.PaymentProof, error) {
	rows, err := r.queries.ListPaymentProofsByReceipt(ctx, sql.NullString{String: receiptID, Valid: true})
	if err != nil {
		return nil, wrapError(err)
	}
	return paymentProofsFromRows(rows), nil
}

// UpdateDecision grava a leitura do documento e o veredito de uma vez: o worker
// só conhece a extração depois de ler o comprovante, então a mesma escrita
// registra valor, data, pagador, confiança e — quando a baixa é aceita — o
// recibo em que ela resultou.
func (r *PaymentProofs) UpdateDecision(ctx context.Context, proof *domain.PaymentProof) error {
	return wrapError(r.queries.UpdatePaymentProofDecision(ctx, sqlcgen.UpdatePaymentProofDecisionParams{
		UpdatedAt:           proof.UpdatedAt,
		Status:              string(proof.Status),
		Decision:            string(proof.Decision),
		DecisionReason:      nullString(proof.DecisionReason),
		DecidedBy:           nullString(proof.DecidedBy),
		DecidedAt:           nullString(proof.DecidedAt),
		ReceiptID:           nullString(proof.ReceiptID),
		ExtractedAmount:     nullFloat(proof.ExtractedAmount),
		ExtractedDate:       nullString(proof.ExtractedDate),
		ExtractedPayer:      nullString(proof.ExtractedPayer),
		ExtractedPayee:      nullString(proof.ExtractedPayee),
		ExtractedIdentifier: nullString(proof.ExtractedIdentifier),
		Confidence:          nullFloat(proof.Confidence),
		RawResponse:         nullString(proof.RawResponse),
		ID:                  proof.ID,
	}))
}

func paymentProofsFromRows(rows []sqlcgen.PaymentProof) []domain.PaymentProof {
	proofs := make([]domain.PaymentProof, 0, len(rows))
	for _, row := range rows {
		proofs = append(proofs, paymentProofFromRow(row))
	}
	return proofs
}

// paymentProofFromRow converte a linha do banco na entidade de domínio.
func paymentProofFromRow(row sqlcgen.PaymentProof) domain.PaymentProof {
	return domain.PaymentProof{
		ID:                  row.ID,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		Source:              domain.PaymentProofSource(row.Source),
		Phone:               stringPtr(row.Phone),
		TenantID:            stringPtr(row.TenantID),
		ReceiptID:           stringPtr(row.ReceiptID),
		FileName:            stringPtr(row.FileName),
		FilePath:            stringPtr(row.FilePath),
		Mime:                stringPtr(row.Mime),
		ExtractedAmount:     floatPtr(row.ExtractedAmount),
		ExtractedDate:       stringPtr(row.ExtractedDate),
		ExtractedPayer:      stringPtr(row.ExtractedPayer),
		ExtractedPayee:      stringPtr(row.ExtractedPayee),
		ExtractedIdentifier: stringPtr(row.ExtractedIdentifier),
		Confidence:          floatPtr(row.Confidence),
		Status:              domain.PaymentProofStatus(row.Status),
		Decision:            domain.PaymentDecision(row.Decision),
		DecisionReason:      stringPtr(row.DecisionReason),
		DecidedBy:           stringPtr(row.DecidedBy),
		DecidedAt:           stringPtr(row.DecidedAt),
		RawResponse:         stringPtr(row.RawResponse),
	}
}
