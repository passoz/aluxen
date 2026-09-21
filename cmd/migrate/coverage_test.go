package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/infrastructure/db"
)

// covSeed cria a origem SQLite migrada com um cadastro de cada tabela copiada.
func covSeed(t *testing.T, path string) {
	t.Helper()
	ctx := context.Background()

	store, err := db.Open(ctx, path, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	for _, statement := range []string{
		`INSERT INTO users (id, username, password_hash, password_changed_at, created_at)
		 VALUES ('u1','admin','hash',NULL,'2026-01-01 10:00:00')`,
		`INSERT INTO settings (key, value) VALUES ('landlord_name','Locador')`,
	} {
		_, err := store.DB().ExecContext(ctx, statement)
		require.NoError(t, err, statement)
	}
}

// Um argumento desconhecido é erro de linha de comando, não de banco: o comando
// nem chega a abrir o destino.
func TestCovMigrateRejectsUnknownFlags(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"--nada"}, &out)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nada")
	assert.Empty(t, out.String(), "o uso sai pelo erro, não pelo relatório")
}

// Um destino que não pode ser aberto interrompe a migração antes de copiar
// qualquer linha: a mensagem diz qual passo falhou.
func TestCovMigrateReportsUnopenableDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "origem.db")
	covSeed(t, source)

	// O diretório do destino não existe: o SQLite não abre o arquivo.
	destination := filepath.Join(dir, "pasta-inexistente", "destino.db")

	var out bytes.Buffer
	err := run([]string{"--from", "file:" + source, "--to", destination}, &out)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "abrir destino")
	assert.Empty(t, out.String())
}

// Migrar duas vezes para o mesmo destino é recusado: a segunda cópia duplicaria
// as linhas, então o comando exige um banco novo e não escreve nada.
func TestCovMigrateRefusesANonEmptyDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "origem.db")
	destination := filepath.Join(dir, "destino.db")
	covSeed(t, source)

	var out bytes.Buffer
	require.NoError(t, run([]string{"--from", "file:" + source, "--to", destination}, &out))
	assert.Contains(t, out.String(), "cópia concluída")

	out.Reset()
	err := run([]string{"--from", "file:" + source, "--to", destination}, &out)

	require.Error(t, err)
	assert.ErrorIs(t, err, application.ErrMigrateDestinationNotEmpty)
	assert.Contains(t, out.String(), "o destino precisa estar vazio")
	assert.NotContains(t, out.String(), "cópia concluída")

	store, err := db.Open(context.Background(), destination, "")
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	var users int64
	require.NoError(t, store.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM users`).Scan(&users))
	assert.Equal(t, int64(1), users, "a recusa não pode duplicar as linhas no destino")
}

// main é a borda do processo: com argumento faltando ele imprime o erro e sai
// com código 1 para o operador (e para o sistema que chamou) perceberem a falha.
func TestCovMainExitsWithFailureOnMissingArguments(t *testing.T) {
	if os.Getenv("COV_MIGRATE_MAIN") == "1" {
		os.Args = []string{"migrate", "--from", "file:/data/aluxen.db"}
		main()
		return
	}

	child := exec.Command(os.Args[0], "-test.run=TestCovMainExitsWithFailureOnMissingArguments")
	child.Env = append(os.Environ(), "COV_MIGRATE_MAIN=1")
	output, err := child.CombinedOutput()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "saída do processo filho: %s", output)
	assert.Equal(t, 1, exitErr.ExitCode())
	assert.Contains(t, string(output), "erro:")
	assert.Contains(t, string(output), "--from e --to são obrigatórios")
}
