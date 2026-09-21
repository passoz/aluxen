package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/infrastructure/db"
)

// openStore abre um banco migrado no caminho informado.
func openStore(t *testing.T, path string) *db.Store {
	t.Helper()
	store, err := db.Open(context.Background(), path, "")
	require.NoError(t, err)
	return store
}

// seedSource cria a origem SQLite migrada com alguns cadastros.
func seedSource(t *testing.T, path string) {
	t.Helper()
	ctx := context.Background()

	store := openStore(t, path)
	t.Cleanup(func() { _ = store.Close() })
	statements := []string{
		`INSERT INTO users (id, username, password_hash, password_changed_at, created_at)
		 VALUES ('u1','admin','hash',NULL,'2026-01-01 10:00:00')`,
		`INSERT INTO properties (id, name, address, city, state, zip, created_at, updated_at)
		 VALUES ('p1','Apto 101','Rua A, 1','Cabo Frio','RJ','28900-000','2026-01-01 10:00:00','2026-01-01 10:00:00')`,
		`INSERT INTO tenants (id, name, email, phone, cpf, password_hash, password_changed_at, created_at, updated_at)
		 VALUES ('t1','Carlos','carlos@exemplo.com.br','(22) 99999-0000',NULL,NULL,NULL,'2026-01-01 10:00:00','2026-01-01 10:00:00')`,
		`INSERT INTO settings (key, value) VALUES ('landlord_name','Locador')`,
	}
	for _, statement := range statements {
		_, err := store.DB().ExecContext(ctx, statement)
		require.NoError(t, err, statement)
	}
}

// countRows conta as linhas de uma tabela.
func countRows(t *testing.T, store *db.Store, table string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, store.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM "`+table+`"`).Scan(&count))
	return count
}

func TestMigrateCopiesSQLiteToSQLiteEndToEnd(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "aluxen.db")
	destinationPath := filepath.Join(dir, "destino.db")
	seedSource(t, sourcePath)

	var out bytes.Buffer
	err := run([]string{"--from", "file:" + sourcePath, "--to", destinationPath}, &out)
	require.NoError(t, err, out.String())

	assert.Contains(t, out.String(), "cópia em transação")
	assert.Contains(t, out.String(), "confere")
	assert.Contains(t, out.String(), "Próximo passo")

	destination := openStore(t, destinationPath)
	defer func() { require.NoError(t, destination.Close()) }()
	assert.Equal(t, int64(1), countRows(t, destination, "tenants"))
	assert.Equal(t, int64(1), countRows(t, destination, "properties"))
	assert.Equal(t, int64(1), countRows(t, destination, "settings"))

	source := openStore(t, sourcePath)
	defer func() { require.NoError(t, source.Close()) }()
	assert.Equal(t, int64(1), countRows(t, source, "tenants"), "a origem nunca é apagada")
}

func TestMigrateDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "aluxen.db")
	destinationPath := filepath.Join(dir, "destino.db")
	seedSource(t, sourcePath)

	var out bytes.Buffer
	require.NoError(t, run([]string{"--from", sourcePath, "--to", destinationPath, "--dry-run"}, &out))

	assert.Contains(t, out.String(), "dry-run")
	assert.Contains(t, out.String(), "nada foi escrito")

	destination := openStore(t, destinationPath)
	defer func() { require.NoError(t, destination.Close()) }()
	assert.Equal(t, int64(0), countRows(t, destination, "tenants"))
}

func TestMigrateRequiresFromAndTo(t *testing.T) {
	var out bytes.Buffer

	err := run([]string{"--from", "file:/data/aluxen.db"}, &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--from e --to são obrigatórios")
}
