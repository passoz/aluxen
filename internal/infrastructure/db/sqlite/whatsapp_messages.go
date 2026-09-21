package sqlite

import (
	"context"
	"database/sql"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db/sqlite/sqlcgen"
)

// WhatsAppMessages implementa domain.WhatsAppMessageRepository sobre o SQL
// gerado pelo sqlc.
type WhatsAppMessages struct {
	queries *sqlcgen.Queries
}

// NewWhatsAppMessages monta o repositório das mensagens do WhatsApp.
func NewWhatsAppMessages(db sqlcgen.DBTX) *WhatsAppMessages {
	return &WhatsAppMessages{queries: sqlcgen.New(db)}
}

var _ domain.WhatsAppMessageRepository = (*WhatsAppMessages)(nil)

// InsertIfAbsent grava a mensagem e devolve quantas linhas entraram: zero
// quando o `external_id` já existe, o que torna idempotente a reentrega do
// webhook.
func (r *WhatsAppMessages) InsertIfAbsent(ctx context.Context, message *domain.WhatsAppMessage) (int64, error) {
	rows, err := r.queries.InsertWhatsAppMessage(ctx, sqlcgen.InsertWhatsAppMessageParams{
		ID:         message.ID,
		CreatedAt:  message.CreatedAt,
		Direction:  string(message.Direction),
		ChatID:     message.ChatID,
		Phone:      message.Phone,
		TenantID:   nullString(message.TenantID),
		Body:       message.Body,
		Status:     message.Status,
		ExternalID: nullString(message.ExternalID),
		Metadata:   nullString(message.Metadata),
	})
	return rows, wrapError(err)
}

// ByTenant devolve as mensagens do locatário, da mais recente para a mais antiga.
func (r *WhatsAppMessages) ByTenant(ctx context.Context, tenantID string, limit int) ([]domain.WhatsAppMessage, error) {
	rows, err := r.queries.ListWhatsAppMessagesByTenant(ctx, sqlcgen.ListWhatsAppMessagesByTenantParams{
		TenantID:  sql.NullString{String: tenantID, Valid: true},
		PageLimit: int64(limit),
	})
	if err != nil {
		return nil, wrapError(err)
	}
	messages := make([]domain.WhatsAppMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, whatsAppMessageFromRow(row))
	}
	return messages, nil
}

// whatsAppMessageFromRow converte a linha do banco na entidade de domínio.
func whatsAppMessageFromRow(row sqlcgen.WhatsappMessage) domain.WhatsAppMessage {
	return domain.WhatsAppMessage{
		ID:         row.ID,
		CreatedAt:  row.CreatedAt,
		Direction:  domain.WhatsAppDirection(row.Direction),
		ChatID:     row.ChatID,
		Phone:      row.Phone,
		TenantID:   stringPtr(row.TenantID),
		Body:       row.Body,
		Status:     row.Status,
		ExternalID: stringPtr(row.ExternalID),
		Metadata:   stringPtr(row.Metadata),
	}
}
