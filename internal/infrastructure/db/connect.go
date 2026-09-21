// Package db é a camada de banco do Aluxen: resolve o dialeto, abre a conexão,
// aplica as migrações, semeia o administrador e entrega os repositórios de
// domínio (portas de internal/domain) implementados sobre o código gerado pelo
// sqlc. Nenhum SQL de negócio vive aqui: as consultas estão em queries/queries.sql.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // driver "pgx"
	_ "modernc.org/sqlite"             // driver "sqlite"
)

// Dialect identifica o banco em uso.
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
)

// defaultDBFile é o nome do arquivo usado quando não há DATABASE_URL.
const defaultDBFile = "aluxen.db"

// ResolveDialect decide o dialeto e o destino da conexão a partir da
// configuração, com a mesma precedência do app atual (`src/db/index.ts`):
//
//   - `postgres://` ou `postgresql://` → Postgres;
//   - `file:<caminho>` ou um caminho → SQLite no caminho informado;
//   - vazio → `<DB_DIR>/aluxen.db` (criando o diretório) ou `aluxen.db` no
//     diretório de trabalho.
//
// O segundo valor devolvido é a string de conexão: o caminho do arquivo no
// SQLite e a DSN no Postgres.
func ResolveDialect(databaseURL, dbDir string) (Dialect, string, error) {
	switch target := strings.TrimSpace(databaseURL); {
	case target == "":
		// segue para as alternativas de diretório
	case strings.HasPrefix(target, "postgres://"), strings.HasPrefix(target, "postgresql://"):
		return DialectPostgres, target, nil
	case strings.HasPrefix(target, "file:"):
		return DialectSQLite, strings.TrimPrefix(target, "file:"), nil
	default:
		return DialectSQLite, target, nil
	}

	if dir := strings.TrimSpace(dbDir); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", "", fmt.Errorf("criar diretório do banco %q: %w", dir, err)
		}
		return DialectSQLite, filepath.Join(dir, defaultDBFile), nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("descobrir o diretório de trabalho: %w", err)
	}
	return DialectSQLite, filepath.Join(cwd, defaultDBFile), nil
}

// Open resolve o dialeto, abre a conexão, aplica as migrações e devolve o Store
// pronto para uso.
func Open(ctx context.Context, databaseURL, dbDir string) (*Store, error) {
	dialect, target, err := ResolveDialect(databaseURL, dbDir)
	if err != nil {
		return nil, err
	}

	sqlDB, err := open(ctx, dialect, target)
	if err != nil {
		return nil, err
	}

	store := newStore(dialect, sqlDB, target)
	if err := store.Migrate(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return store, nil
}

// open abre a conexão do dialeto e confirma que ela responde.
func open(ctx context.Context, dialect Dialect, target string) (*sql.DB, error) {
	if dialect == DialectPostgres {
		sqlDB, err := sql.Open("pgx", target)
		if err != nil {
			return nil, fmt.Errorf("abrir conexão Postgres: %w", err)
		}
		if err := sqlDB.PingContext(ctx); err != nil {
			_ = sqlDB.Close()
			return nil, fmt.Errorf("conectar ao Postgres: %w", err)
		}
		return sqlDB, nil
	}

	sqlDB, err := sql.Open("sqlite", sqliteDSN(target))
	if err != nil {
		return nil, fmt.Errorf("abrir conexão SQLite: %w", err)
	}
	// Uma única conexão reproduz a semântica do app atual (um handle bun:sqlite),
	// evita SQLITE_BUSY entre goroutines e é o que torna `:memory:` coerente —
	// cada conexão nova de um banco em memória seria um banco diferente.
	sqlDB.SetMaxOpenConns(1)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("conectar ao SQLite %q: %w", target, err)
	}
	return sqlDB, nil
}

// sqliteDSN acrescenta os PRAGMAs do app (WAL e chaves estrangeiras) ao caminho.
// O driver os aplica em toda conexão nova.
func sqliteDSN(path string) string {
	params := url.Values{}
	params.Add("_pragma", "journal_mode(WAL)")
	params.Add("_pragma", "foreign_keys(ON)")
	return path + "?" + params.Encode()
}
