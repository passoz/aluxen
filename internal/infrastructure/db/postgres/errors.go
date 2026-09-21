package postgres

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/passoz/aluxen/internal/domain"
)

// pgUniqueViolation é o código SQLSTATE de violação de chave única/primária.
const pgUniqueViolation = "23505"

// wrapError traduz os erros do driver para os erros de domínio, para que a
// aplicação não dependa do dialeto: linha inexistente vira ErrNotFound e
// violação de unicidade vira ErrConflict.
func wrapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return fmt.Errorf("%w: %s", domain.ErrConflict, pgErr.Message)
	}
	return err
}
