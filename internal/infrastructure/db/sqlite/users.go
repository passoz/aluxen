package sqlite

import (
	"context"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db/sqlite/sqlcgen"
)

// Users implementa domain.UserRepository sobre o SQL gerado pelo sqlc.
type Users struct {
	queries *sqlcgen.Queries
}

// NewUsers monta o repositório de administradores.
func NewUsers(db sqlcgen.DBTX) *Users {
	return &Users{queries: sqlcgen.New(db)}
}

var _ domain.UserRepository = (*Users)(nil)

func (r *Users) ByID(ctx context.Context, id string) (*domain.User, error) {
	row, err := r.queries.GetUserByID(ctx, id)
	if err != nil {
		return nil, wrapError(err)
	}
	user := userFromRow(row)
	return &user, nil
}

func (r *Users) ByUsername(ctx context.Context, username string) (*domain.User, error) {
	row, err := r.queries.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, wrapError(err)
	}
	user := userFromRow(row)
	return &user, nil
}

func (r *Users) Create(ctx context.Context, user *domain.User) error {
	return wrapError(r.queries.InsertUser(ctx, sqlcgen.InsertUserParams{
		ID:                user.ID,
		Username:          user.Username,
		PasswordHash:      user.PasswordHash,
		PasswordChangedAt: nullString(user.PasswordChangedAt),
		CreatedAt:         user.CreatedAt,
	}))
}

func (r *Users) UpdateUsername(ctx context.Context, id, username string) error {
	return wrapError(r.queries.UpdateUserUsername(ctx, sqlcgen.UpdateUserUsernameParams{
		Username: username,
		ID:       id,
	}))
}

func (r *Users) UpdatePassword(ctx context.Context, id, passwordHash, changedAt string) error {
	return wrapError(r.queries.UpdateUserPassword(ctx, sqlcgen.UpdateUserPasswordParams{
		PasswordHash:      passwordHash,
		PasswordChangedAt: nullText(changedAt),
		ID:                id,
	}))
}

// userFromRow converte a linha do banco na entidade de domínio.
func userFromRow(row sqlcgen.User) domain.User {
	return domain.User{
		ID:                row.ID,
		Username:          row.Username,
		PasswordHash:      row.PasswordHash,
		PasswordChangedAt: stringPtr(row.PasswordChangedAt),
		CreatedAt:         row.CreatedAt,
	}
}
