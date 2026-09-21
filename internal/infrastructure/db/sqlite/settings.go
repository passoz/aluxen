package sqlite

import (
	"context"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db/sqlite/sqlcgen"
)

// Settings implementa domain.SettingRepository sobre o SQL gerado pelo sqlc.
type Settings struct {
	queries *sqlcgen.Queries
}

// NewSettings monta o repositório de configurações.
func NewSettings(db sqlcgen.DBTX) *Settings {
	return &Settings{queries: sqlcgen.New(db)}
}

var _ domain.SettingRepository = (*Settings)(nil)

// Get devolve o valor da chave ou domain.ErrNotFound quando ela não existe.
func (r *Settings) Get(ctx context.Context, key string) (string, error) {
	value, err := r.queries.GetSetting(ctx, key)
	if err != nil {
		return "", wrapError(err)
	}
	return value, nil
}

func (r *Settings) List(ctx context.Context) (map[string]string, error) {
	rows, err := r.queries.ListSettings(ctx)
	if err != nil {
		return nil, wrapError(err)
	}
	settings := make(map[string]string, len(rows))
	for _, row := range rows {
		settings[row.Key] = row.Value
	}
	return settings, nil
}

func (r *Settings) Set(ctx context.Context, key, value string) error {
	return wrapError(r.queries.UpsertSetting(ctx, sqlcgen.UpsertSettingParams{Key: key, Value: value}))
}
