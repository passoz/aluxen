package sqlite

import (
	"database/sql"

	"github.com/passoz/aluxen/internal/domain"
)

// Conversão entre as linhas geradas pelo sqlc (colunas anuláveis em
// sql.NullString/sql.NullFloat64, contadores em int64) e as entidades de
// domínio (anuláveis em ponteiro, contadores em int).

// nullString converte um campo anulável da entidade na coluna do banco: ponteiro
// nulo é NULL e string vazia é string vazia (a entidade distingue os dois casos).
func nullString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

// nullText converte um texto simples em coluna anulável, tratando vazio como
// NULL — usado onde a porta só oferece string (troca de senha, pagamento).
func nullText(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

// nullFloat converte um valor anulável da entidade na coluna do banco.
func nullFloat(value *float64) sql.NullFloat64 {
	if value == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *value, Valid: true}
}

// stringPtr converte a coluna anulável na representação de domínio.
func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	text := value.String
	return &text
}

// floatPtr converte a coluna anulável na representação de domínio.
func floatPtr(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	amount := value.Float64
	return &amount
}

// depositStatusColumn converte a situação da caução na coluna do banco: o
// contrato sem caução grava NULL, que é o que mantém o uso atual (locação longa)
// idêntico ao de antes da temporada.
func depositStatusColumn(status domain.DepositStatus) sql.NullString {
	if status == "" || status == domain.DepositStatusNone {
		return sql.NullString{}
	}
	return sql.NullString{String: string(status), Valid: true}
}

// depositStatusFromColumn lê a situação da caução: NULL ou vazio é "sem caução".
func depositStatusFromColumn(value sql.NullString) domain.DepositStatus {
	if !value.Valid || value.String == "" {
		return domain.DepositStatusNone
	}
	return domain.DepositStatus(value.String)
}

// categoryOrDefault preserva o default que o app atual tinha no banco
// (`receipts.category DEFAULT 'rent'`): o DDL canônico não declara defaults, mas
// a categoria nunca é vazia.
func categoryOrDefault(category domain.ReceiptCategory) string {
	if category == "" {
		return string(domain.ReceiptCategoryRent)
	}
	return string(category)
}

// leaseStatusOrDefault preserva o default `leases.status DEFAULT 'active'`.
func leaseStatusOrDefault(status domain.LeaseStatus) string {
	if status == "" {
		return string(domain.LeaseStatusActive)
	}
	return string(status)
}

// dueDayOrDefault preserva o default `leases.due_day DEFAULT 1`.
func dueDayOrDefault(dueDay int) int {
	if dueDay <= 0 {
		return 1
	}
	return dueDay
}

// auditOriginOrDefault preserva o default `audit_logs.origin DEFAULT 'web'`.
func auditOriginOrDefault(origin domain.AuditOrigin) string {
	if origin == "" {
		return string(domain.AuditOriginWeb)
	}
	return string(origin)
}
