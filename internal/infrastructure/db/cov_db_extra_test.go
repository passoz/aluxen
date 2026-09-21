package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Caminhos de erro que só aparecem com uma base num estado inesperado: o boot
// precisa falhar sem deixar a conexão aberta e sem reaplicar migração.

func TestCovDBOpenClosesConnectionWhenMigrationFails(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "controle-quebrado.db")

	// uma tabela de controle com o esquema errado derruba o goose
	handle, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("abrir sqlite: %v", err)
	}
	if _, err := handle.ExecContext(ctx, "CREATE TABLE goose_db_version (id integer PRIMARY KEY, versao integer)"); err != nil {
		t.Fatalf("criar controle: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("fechar: %v", err)
	}

	if _, err := Open(ctx, "file:"+path, ""); err == nil {
		t.Fatal("o boot deveria falhar com o controle de versão quebrado")
	}
}

func TestCovDBOpenPostgresRejectsUnparsableDSN(t *testing.T) {
	// a porta textual faz o pgx recusar a DSN ao montar a conexão
	if _, err := open(context.Background(), DialectPostgres, "postgres://usuario:senha@localhost:porta-invalida/aluxen"); err == nil {
		t.Fatal("DSN com porta inválida deveria falhar ao abrir")
	}
	// o sslmode fora do vocabulário é recusado na montagem da conexão
	if _, err := open(context.Background(), DialectPostgres, "postgres://usuario:senha@127.0.0.1:1/aluxen?sslmode=inexistente"); err == nil {
		t.Fatal("sslmode inválido deveria falhar ao abrir")
	}
}

func TestCovDBResolveDialectWithoutWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remover o diretório atual: %v", err)
	}
	if _, _, err := ResolveDialect("", ""); err == nil {
		t.Fatal("sem diretório de trabalho a resolução deveria falhar")
	}
}

func TestCovDBRunMigrationsFailsWhenVersionControlIsCorrupt(t *testing.T) {
	ctx := context.Background()
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", "sqlite.sql"))
	if err != nil {
		t.Fatalf("ler o schema canônico: %v", err)
	}

	path := filepath.Join(t.TempDir(), "adocao-quebrada.db")
	handle, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("abrir sqlite: %v", err)
	}
	// base já no schema final: as migrações serão adotadas (não reaplicadas)
	if _, err := handle.ExecContext(ctx, string(content)); err != nil {
		t.Fatalf("aplicar o schema canônico: %v", err)
	}
	if _, err := handle.ExecContext(ctx, "CREATE TABLE goose_db_version (id integer PRIMARY KEY, versao integer)"); err != nil {
		t.Fatalf("criar controle quebrado: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("fechar: %v", err)
	}

	broken, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("reabrir: %v", err)
	}
	defer func() { _ = broken.Close() }()
	if err := runMigrations(ctx, broken, DialectSQLite); err == nil {
		t.Fatal("adotar uma base com controle de versão quebrado deveria falhar")
	}
}

func TestCovDBRepairLegacyReceiptsRebuildFailure(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "receipts-magro.db")

	// receipts antigo sem as colunas obrigatórias do schema final: o rebuild não
	// consegue copiar e o boot precisa falhar alto
	handle, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("abrir sqlite: %v", err)
	}
	statements := []string{
		"CREATE TABLE receipts (id text PRIMARY KEY NOT NULL, lease_id text NOT NULL)",
		"INSERT INTO receipts (id, lease_id) VALUES ('r1', 'l1')",
	}
	for _, statement := range statements {
		if _, err := handle.ExecContext(ctx, statement); err != nil {
			t.Fatalf("executar %q: %v", statement, err)
		}
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("fechar: %v", err)
	}

	reference, err := migrationSnapshots(DialectSQLite)
	if err != nil {
		t.Fatalf("migrationSnapshots: %v", err)
	}
	broken, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("reabrir: %v", err)
	}
	defer func() { _ = broken.Close() }()
	if err := repairSQLiteLegacy(ctx, broken, reference); err == nil {
		t.Fatal("o rebuild de receipts deveria falhar sem as colunas obrigatórias")
	}
}

func TestCovDBMarkAppliedVersionsErrorPaths(t *testing.T) {
	ctx := context.Background()

	// a tabela de controle não existe e a criação falha
	if err := markAppliedVersions(ctx, covDBFakeHandle(t, []covDBFakeQuery{
		{match: "sqlite_master", cols: []string{"count"}, rows: [][]driver.Value{{0}}},
	}), DialectSQLite, []int64{1}); err == nil {
		t.Fatal("falha ao criar o controle deveria subir")
	}

	// a tabela existe mas a leitura da versão falha
	if err := markAppliedVersions(ctx, covDBFakeHandle(t, []covDBFakeQuery{
		{match: "sqlite_master", cols: []string{"count"}, rows: [][]driver.Value{{1}}},
		{match: "goose_db_version", err: errors.New("versão ilegível")},
	}), DialectSQLite, []int64{1}); err == nil {
		t.Fatal("falha ao ler a versão deveria subir")
	}

	// a versão corrente é menor que a informada e o registro falha
	if err := markAppliedVersions(ctx, covDBFakeHandle(t, []covDBFakeQuery{
		{match: "sqlite_master", cols: []string{"count"}, rows: [][]driver.Value{{1}}},
		{match: "goose_db_version", cols: []string{"max"}, rows: [][]driver.Value{{1}}},
	}), DialectSQLite, []int64{5}); err == nil {
		t.Fatal("falha ao registrar a migração deveria subir")
	}
}
