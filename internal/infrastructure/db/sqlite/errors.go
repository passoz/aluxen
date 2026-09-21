package sqlite

import (
	"database/sql"
	"errors"
	"fmt"

	sqlitedriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/passoz/aluxen/internal/domain"
)

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

	var sqliteErr *sqlitedriver.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return fmt.Errorf("%w: %s", domain.ErrConflict, sqliteErr.Error())
		}
	}
	return err
}
