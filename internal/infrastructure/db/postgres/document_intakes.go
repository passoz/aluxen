package postgres

import (
	"context"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db/postgres/sqlcgen"
)

// DocumentIntakes implementa domain.DocumentIntakeRepository sobre o SQL gerado
// pelo sqlc: guarda o cadastro proposto a partir de um documento, sempre sujeito
// a conferência humana antes de virar registro.
type DocumentIntakes struct {
	queries *sqlcgen.Queries
}

// NewDocumentIntakes monta o repositório dos cadastros propostos.
func NewDocumentIntakes(db sqlcgen.DBTX) *DocumentIntakes {
	return &DocumentIntakes{queries: sqlcgen.New(db)}
}

var _ domain.DocumentIntakeRepository = (*DocumentIntakes)(nil)

func (r *DocumentIntakes) Create(ctx context.Context, intake *domain.DocumentIntake) error {
	return wrapError(r.queries.InsertDocumentIntake(ctx, sqlcgen.InsertDocumentIntakeParams{
		ID:              intake.ID,
		CreatedAt:       intake.CreatedAt,
		UpdatedAt:       intake.UpdatedAt,
		Kind:            string(intake.Kind),
		FileName:        nullString(intake.FileName),
		FilePath:        nullString(intake.FilePath),
		Mime:            nullString(intake.Mime),
		Status:          string(intake.Status),
		Extracted:       nullString(intake.Extracted),
		Confidence:      nullFloat(intake.Confidence),
		CreatedEntity:   nullString(intake.CreatedEntity),
		CreatedEntityID: nullString(intake.CreatedEntityID),
		RawResponse:     nullString(intake.RawResponse),
		Notes:           nullString(intake.Notes),
	}))
}

func (r *DocumentIntakes) ByID(ctx context.Context, id string) (*domain.DocumentIntake, error) {
	row, err := r.queries.GetDocumentIntakeByID(ctx, id)
	if err != nil {
		return nil, wrapError(err)
	}
	intake := documentIntakeFromRow(row)
	return &intake, nil
}

func (r *DocumentIntakes) List(ctx context.Context, limit int) ([]domain.DocumentIntake, error) {
	rows, err := r.queries.ListDocumentIntakes(ctx, int32(limit))
	if err != nil {
		return nil, wrapError(err)
	}
	return documentIntakesFromRows(rows), nil
}

func (r *DocumentIntakes) ListByStatus(ctx context.Context, status domain.IntakeStatus, limit int) ([]domain.DocumentIntake, error) {
	rows, err := r.queries.ListDocumentIntakesByStatus(ctx, sqlcgen.ListDocumentIntakesByStatusParams{
		Status:    string(status),
		PageLimit: int32(limit),
	})
	if err != nil {
		return nil, wrapError(err)
	}
	return documentIntakesFromRows(rows), nil
}

// Update grava a conferência: situação, campos extraídos, confiança e o registro
// criado a partir do cadastro.
func (r *DocumentIntakes) Update(ctx context.Context, intake *domain.DocumentIntake) error {
	return wrapError(r.queries.UpdateDocumentIntake(ctx, sqlcgen.UpdateDocumentIntakeParams{
		UpdatedAt:       intake.UpdatedAt,
		Status:          string(intake.Status),
		Extracted:       nullString(intake.Extracted),
		Confidence:      nullFloat(intake.Confidence),
		CreatedEntity:   nullString(intake.CreatedEntity),
		CreatedEntityID: nullString(intake.CreatedEntityID),
		RawResponse:     nullString(intake.RawResponse),
		Notes:           nullString(intake.Notes),
		ID:              intake.ID,
	}))
}

func documentIntakesFromRows(rows []sqlcgen.DocumentIntake) []domain.DocumentIntake {
	intakes := make([]domain.DocumentIntake, 0, len(rows))
	for _, row := range rows {
		intakes = append(intakes, documentIntakeFromRow(row))
	}
	return intakes
}

// documentIntakeFromRow converte a linha do banco na entidade de domínio.
func documentIntakeFromRow(row sqlcgen.DocumentIntake) domain.DocumentIntake {
	return domain.DocumentIntake{
		ID:              row.ID,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		Kind:            domain.IntakeKind(row.Kind),
		FileName:        stringPtr(row.FileName),
		FilePath:        stringPtr(row.FilePath),
		Mime:            stringPtr(row.Mime),
		Status:          domain.IntakeStatus(row.Status),
		Extracted:       stringPtr(row.Extracted),
		Confidence:      floatPtr(row.Confidence),
		CreatedEntity:   stringPtr(row.CreatedEntity),
		CreatedEntityID: stringPtr(row.CreatedEntityID),
		RawResponse:     stringPtr(row.RawResponse),
		Notes:           stringPtr(row.Notes),
	}
}
