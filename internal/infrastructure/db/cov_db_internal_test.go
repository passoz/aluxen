package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/passoz/aluxen/internal/infrastructure/db/schema"
)

// ── driver falso ───────────────────────────────────────────────────
//
// O leitor de schema do Postgres (information_schema/pg_indexes) e o
// `tableExists` do Postgres não têm servidor no gate de cobertura. Este driver
// devolve as linhas que o banco devolveria — e injeta os erros — para provar a
// tradução das linhas em fotografia de schema sem depender de rede.

type covDBFakeQuery struct {
	match   string // trecho da consulta que identifica a resposta
	cols    []string
	rows    [][]driver.Value
	err     error // falha da própria consulta
	nextErr error // falha no meio da leitura das linhas
}

var covDBFakeState = struct {
	sync.Mutex
	queries []covDBFakeQuery
}{}

func init() { sql.Register("cov-db-fake", covDBFakeDriver{}) }

type covDBFakeDriver struct{}

func (covDBFakeDriver) Open(string) (driver.Conn, error) { return covDBFakeConn{}, nil }

type covDBFakeConn struct{}

func (covDBFakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare não previsto")
}
func (covDBFakeConn) Close() error              { return nil }
func (covDBFakeConn) Begin() (driver.Tx, error) { return nil, errors.New("transação não prevista") }

func (covDBFakeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	covDBFakeState.Lock()
	defer covDBFakeState.Unlock()
	for _, candidate := range covDBFakeState.queries {
		if strings.Contains(query, candidate.match) {
			if candidate.err != nil {
				return nil, candidate.err
			}
			rows := make([][]driver.Value, len(candidate.rows))
			copy(rows, candidate.rows)
			return &covDBFakeRows{cols: candidate.cols, rows: rows, nextErr: candidate.nextErr}, nil
		}
	}
	return nil, fmt.Errorf("consulta não prevista no teste: %s", query)
}

func (covDBFakeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	return nil, fmt.Errorf("execução não prevista no teste: %s", query)
}

type covDBFakeRows struct {
	cols    []string
	rows    [][]driver.Value
	next    int
	nextErr error
}

func (r *covDBFakeRows) Columns() []string { return r.cols }
func (r *covDBFakeRows) Close() error      { return nil }

func (r *covDBFakeRows) Next(dest []driver.Value) error {
	if r.next < len(r.rows) {
		copy(dest, r.rows[r.next])
		r.next++
		return nil
	}
	if r.nextErr != nil {
		err := r.nextErr
		r.nextErr = nil
		return err
	}
	return io.EOF
}

func covDBFakeHandle(t *testing.T, queries []covDBFakeQuery) *sql.DB {
	t.Helper()
	covDBFakeState.Lock()
	covDBFakeState.queries = queries
	covDBFakeState.Unlock()
	handle, err := sql.Open("cov-db-fake", "fake")
	if err != nil {
		t.Fatalf("abrir driver falso: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	return handle
}

func covDBClosed(t *testing.T) *sql.DB {
	t.Helper()
	handle, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("abrir sqlite: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("fechar sqlite: %v", err)
	}
	return handle
}

// ── ResolveDialect / Open / open ───────────────────────────────────

func TestCovDBResolveDialectEmptyFallsBackToWorkingDirectory(t *testing.T) {
	for _, databaseURL := range []string{"", "   "} {
		dialect, target, err := ResolveDialect(databaseURL, "")
		if err != nil {
			t.Fatalf("ResolveDialect(%q, \"\"): %v", databaseURL, err)
		}
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Getwd: %v", err)
		}
		if dialect != DialectSQLite || target != filepath.Join(cwd, defaultDBFile) {
			t.Fatalf("ResolveDialect(%q) = %q/%q", databaseURL, dialect, target)
		}
	}
}

func TestCovDBResolveDialectReportsDirectoryError(t *testing.T) {
	// um arquivo comum no lugar do diretório impede a criação
	file := filepath.Join(t.TempDir(), "arquivo")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("preparar arquivo: %v", err)
	}
	if _, _, err := ResolveDialect("", filepath.Join(file, "sub")); err == nil {
		t.Fatal("diretório impossível deveria falhar")
	}
}

func TestCovDBOpenPropagatesErrors(t *testing.T) {
	file := filepath.Join(t.TempDir(), "arquivo")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("preparar arquivo: %v", err)
	}
	if _, err := Open(context.Background(), "", filepath.Join(file, "sub")); err == nil {
		t.Fatal("erro de resolução de dialeto deveria subir")
	}

	// diretório inexistente: o SQLite não abre o arquivo
	missing := filepath.Join(t.TempDir(), "sem-dir", defaultDBFile)
	if _, err := Open(context.Background(), "file:"+missing, ""); err == nil {
		t.Fatal("caminho sem diretório deveria falhar ao conectar")
	}
}

func TestCovDBOpenPostgresWithoutServer(t *testing.T) {
	// DSN que o pgx recusa na hora de montar a conexão
	if _, err := open(context.Background(), DialectPostgres, "não-é-uma-dsn"); err == nil {
		t.Fatal("DSN inválida deveria falhar")
	}
	// DSN válida apontando para um soquete que não existe: falha ao conectar
	_, err := open(context.Background(), DialectPostgres, "postgres://usuario@/aluxen?host=/tmp/aluxen-sem-servidor")
	if err == nil {
		t.Fatal("servidor ausente deveria falhar ao conectar")
	}
}

func TestCovDBNewStoreWiresPostgresRepositories(t *testing.T) {
	handle, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("abrir sqlite: %v", err)
	}
	defer func() { _ = handle.Close() }()

	store := newStore(DialectPostgres, handle, "postgres://exemplo")
	if store.Dialect != DialectPostgres {
		t.Fatalf("dialeto = %q", store.Dialect)
	}
	if store.Path() != "" {
		t.Fatalf("Postgres não tem arquivo: %q", store.Path())
	}
	if store.SizeBytes() != 0 {
		t.Fatalf("Postgres não tem tamanho de arquivo: %d", store.SizeBytes())
	}
	if store.Users == nil || store.Intakes == nil || store.PaymentProofs == nil || store.Jobs == nil {
		t.Fatal("os repositórios do Postgres deveriam estar montados")
	}
}

func TestCovDBSizeBytesWithoutFile(t *testing.T) {
	if got := (&Store{path: ""}).SizeBytes(); got != 0 {
		t.Fatalf("sem caminho = %d", got)
	}
	if got := (&Store{path: filepath.Join(t.TempDir(), "nada.db")}).SizeBytes(); got != 0 {
		t.Fatalf("arquivo ausente = %d", got)
	}
}

func TestCovDBStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vida.db")
	store, err := Open(ctx, "file:"+path, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if store.Path() != path {
		t.Fatalf("Path = %q, quer %q", store.Path(), path)
	}
	if store.SizeBytes() <= 0 {
		t.Fatalf("SizeBytes = %d", store.SizeBytes())
	}
	if err := store.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	// reaplicar as migrações é idempotente
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate repetido: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := store.Ping(ctx); err == nil {
		t.Fatal("Ping depois do Close deveria falhar")
	}
}

// ── runMigrations / applyGoose ─────────────────────────────────────

func TestCovDBRunMigrationsOnClosedDatabase(t *testing.T) {
	handle := covDBClosed(t)
	if err := runMigrations(context.Background(), handle, DialectSQLite); err == nil {
		t.Fatal("o remendo da base viva deveria falhar no banco fechado")
	}
	if err := runMigrations(context.Background(), handle, DialectPostgres); err == nil {
		t.Fatal("a leitura do schema Postgres deveria falhar no banco fechado")
	}
}

func TestCovDBApplyGooseOnClosedDatabase(t *testing.T) {
	if err := applyGoose(context.Background(), covDBClosed(t), DialectSQLite); err == nil {
		t.Fatal("o goose deveria falhar no banco fechado")
	}
}

func TestCovDBMigrationSourceAndSnapshotsPerDialect(t *testing.T) {
	embedded, name, err := migrationSource(DialectPostgres)
	if err != nil {
		t.Fatalf("migrationSource: %v", err)
	}
	if name != "postgres" || embedded == nil {
		t.Fatalf("fonte do Postgres = %q", name)
	}

	snapshots, err := migrationSnapshots(DialectPostgres)
	if err != nil {
		t.Fatalf("migrationSnapshots: %v", err)
	}
	if len(snapshots) == 0 {
		t.Fatal("as migrações embutidas do Postgres deveriam ser lidas")
	}
	for i := 1; i < len(snapshots); i++ {
		if snapshots[i].version <= snapshots[i-1].version {
			t.Fatalf("as versões deveriam vir em ordem: %d antes de %d",
				snapshots[i-1].version, snapshots[i].version)
		}
	}
}

// ── Adoção da base SQLite ──────────────────────────────────────────

func TestCovDBAddLegacyColumnToleratesFailure(t *testing.T) {
	store, err := Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "col.db"), "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// tabela ausente: o ajuste é ignorado e o boot continua
	if err := addLegacyColumn(context.Background(), store.DB(), legacyColumn{table: "tabela_ausente", column: "x", definition: "text"}); err != nil {
		t.Fatalf("falha deveria ser tolerada: %v", err)
	}
}

func TestCovDBRebuildReceiptsRejectsBadDefinition(t *testing.T) {
	handle, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("abrir sqlite: %v", err)
	}
	defer func() { _ = handle.Close() }()

	if err := rebuildReceipts(context.Background(), handle, nil, ""); err == nil {
		t.Fatal("DDL vazio deveria falhar")
	}
	if err := rebuildReceipts(context.Background(), handle, nil, "CREATE TABLE outra (id text)"); err == nil {
		t.Fatal("DDL sem receipts deveria falhar")
	}
	if err := rebuildReceipts(context.Background(), covDBClosed(t), nil, "CREATE TABLE receipts (id text)"); err == nil {
		t.Fatal("o PRAGMA deveria falhar no banco fechado")
	}
	// a cópia falha quando a tabela de origem não existe
	if err := rebuildReceipts(context.Background(), handle, map[string]bool{"id": true}, "CREATE TABLE receipts (id text)"); err == nil {
		t.Fatal("a cópia deveria falhar sem a tabela de origem")
	}
}

func TestCovDBReceiptCopyColumnsFallbacks(t *testing.T) {
	target, selection := receiptCopyColumns(map[string]bool{"id": true})
	if strings.Join(target, ",") != "id,category,created_at" {
		t.Fatalf("colunas de destino = %v", target)
	}
	if strings.Join(selection, ",") != "id,'rent',datetime('now')" {
		t.Fatalf("origem = %v", selection)
	}

	full := map[string]bool{
		"id": true, "lease_id": true, "tenant_id": true, "property_id": true, "category": true,
		"description": true, "due_date": true, "paid_at": true, "amount": true, "paid_amount": true,
		"pdf_path": true, "generated_key": true, "auth_token": true, "created_at": true, "updated_at": true,
	}
	target, selection = receiptCopyColumns(full)
	if len(target) != 15 || len(selection) != 15 || strings.Join(target, ",") != strings.Join(selection, ",") {
		t.Fatalf("com todas as colunas a cópia é direta: %v / %v", target, selection)
	}
}

func TestCovDBCoveredVersionsStopsAtFirstGap(t *testing.T) {
	reference := []migrationSnapshot{
		{version: 1, schema: schema.Schema{Tables: []schema.Table{{Name: "t", Columns: []schema.Column{{Name: "id"}}}}}},
		{version: 2, schema: schema.Schema{Tables: []schema.Table{{Name: "u", Columns: []schema.Column{{Name: "id"}}}}}},
	}
	// banco vazio não cobre nada
	if covered := coveredVersions(reference, schemaSnapshot{columns: map[string]map[string]bool{}, indexes: map[string]bool{}}); len(covered) != 0 {
		t.Fatalf("banco vazio cobriu %v", covered)
	}
	// banco com a tabela t cobre só a primeira
	actual := schemaSnapshot{columns: map[string]map[string]bool{"t": {"id": true}}, indexes: map[string]bool{}}
	if covered := coveredVersions(reference, actual); len(covered) != 1 || covered[0] != 1 {
		t.Fatalf("parou errado: %v", covered)
	}
}

func TestCovDBCoversRequiresEveryColumnAndIndex(t *testing.T) {
	reference := schema.Schema{
		Tables:  []schema.Table{{Name: "t", Columns: []schema.Column{{Name: "id"}, {Name: "nome"}}}},
		Indexes: []schema.Index{{Name: "t_idx"}},
	}
	cases := map[string]schemaSnapshot{
		"tabela ausente": {columns: map[string]map[string]bool{}, indexes: map[string]bool{"t_idx": true}},
		"coluna ausente": {columns: map[string]map[string]bool{"t": {"id": true}}, indexes: map[string]bool{"t_idx": true}},
		"índice ausente": {columns: map[string]map[string]bool{"t": {"id": true, "nome": true}}, indexes: map[string]bool{}},
		"tudo presente":  {columns: map[string]map[string]bool{"t": {"id": true, "nome": true}}, indexes: map[string]bool{"t_idx": true}},
	}
	wants := map[string]bool{
		"tabela ausente": false, "coluna ausente": false, "índice ausente": false, "tudo presente": true,
	}
	for name, snapshot := range cases {
		if got := snapshot.covers(reference); got != wants[name] {
			t.Fatalf("%s: covers = %v", name, got)
		}
	}
}

// ── markAppliedVersions ────────────────────────────────────────────

func TestCovDBMarkAppliedVersions(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, "file:"+filepath.Join(t.TempDir(), "versao.db"), "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// nada a registrar
	if err := markAppliedVersions(ctx, store.DB(), DialectSQLite, nil); err != nil {
		t.Fatalf("lista vazia: %v", err)
	}

	// registra e é idempotente (a segunda passada não reinsere)
	versions := appliedVersionsFromMigrations(t, DialectSQLite)
	if err := markAppliedVersions(ctx, store.DB(), DialectSQLite, versions); err != nil {
		t.Fatalf("registrar: %v", err)
	}
	if err := markAppliedVersions(ctx, store.DB(), DialectSQLite, versions); err != nil {
		t.Fatalf("registrar de novo: %v", err)
	}

	// o dialeto Postgres não tem tabela de controle no SQLite: falha ao procurar
	if err := markAppliedVersions(ctx, store.DB(), DialectPostgres, []int64{1}); err == nil {
		t.Fatal("o controle do Postgres deveria falhar no SQLite")
	}
}

func appliedVersionsFromMigrations(t *testing.T, dialect Dialect) []int64 {
	t.Helper()
	snapshots, err := migrationSnapshots(dialect)
	if err != nil {
		t.Fatalf("migrationSnapshots: %v", err)
	}
	versions := make([]int64, 0, len(snapshots))
	for _, snapshot := range snapshots {
		versions = append(versions, snapshot.version)
	}
	return versions
}

// ── Leitura do schema atual ────────────────────────────────────────

func TestCovDBSqliteColumnRequiredMissingColumn(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, "file:"+filepath.Join(t.TempDir(), "nulidade.db"), "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	required, err := sqliteColumnRequired(ctx, store.DB(), "receipts", "coluna_inexistente")
	if err != nil || required {
		t.Fatalf("coluna ausente não é obrigatória: %v %v", required, err)
	}
	if _, err := sqliteColumnRequired(ctx, covDBClosed(t), "receipts", "id"); err == nil {
		t.Fatal("banco fechado deveria falhar")
	}
}

func TestCovDBReadSchemaPostgresOnSqlite(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, "file:"+filepath.Join(t.TempDir(), "misto.db"), "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// a consulta do Postgres não existe no SQLite
	if _, err := readSchema(ctx, store.DB(), DialectPostgres); err == nil {
		t.Fatal("o leitor do Postgres deveria falhar no SQLite")
	}
}

func TestCovDBReadPostgresSchemaReadsColumnsAndIndexes(t *testing.T) {
	handle := covDBFakeHandle(t, []covDBFakeQuery{
		{
			match: "information_schema.columns",
			cols:  []string{"table_name", "column_name"},
			rows: [][]driver.Value{
				{"tenants", "id"}, {"tenants", "name"}, {"leases", "id"},
			},
		},
		{
			match: "pg_indexes",
			cols:  []string{"indexname"},
			rows:  [][]driver.Value{{"tenants_username_unique"}, {"leases_tenant_idx"}},
		},
	})

	snapshot, err := readPostgresSchema(context.Background(), handle)
	if err != nil {
		t.Fatalf("readPostgresSchema: %v", err)
	}
	if !snapshot.columns["tenants"]["id"] || !snapshot.columns["tenants"]["name"] || !snapshot.columns["leases"]["id"] {
		t.Fatalf("colunas lidas = %+v", snapshot.columns)
	}
	if !snapshot.indexes["leases_tenant_idx"] || !snapshot.indexes["tenants_username_unique"] {
		t.Fatalf("índices lidos = %+v", snapshot.indexes)
	}
}

func TestCovDBReadPostgresSchemaErrorPaths(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		queries []covDBFakeQuery
	}{
		{"falha ao listar colunas", []covDBFakeQuery{
			{match: "information_schema.columns", err: errors.New("sem colunas")},
		}},
		{"falha ao ler coluna", []covDBFakeQuery{
			{match: "information_schema.columns", cols: []string{"table_name", "column_name"}, rows: [][]driver.Value{{struct{}{}, "id"}}, nextErr: nil},
		}},
		{"falha ao percorrer colunas", []covDBFakeQuery{
			{match: "information_schema.columns", cols: []string{"table_name", "column_name"}, rows: [][]driver.Value{{"t", "id"}}, nextErr: errors.New("meio da leitura")},
		}},
		{"falha ao listar índices", []covDBFakeQuery{
			{match: "information_schema.columns", cols: []string{"table_name", "column_name"}},
			{match: "pg_indexes", err: errors.New("sem índices")},
		}},
		{"falha ao ler índice", []covDBFakeQuery{
			{match: "information_schema.columns", cols: []string{"table_name", "column_name"}},
			{match: "pg_indexes", cols: []string{"indexname"}, rows: [][]driver.Value{{struct{}{}}}},
		}},
		{"falha ao percorrer índices", []covDBFakeQuery{
			{match: "information_schema.columns", cols: []string{"table_name", "column_name"}},
			{match: "pg_indexes", cols: []string{"indexname"}, rows: [][]driver.Value{{"idx"}}, nextErr: errors.New("meio da leitura")},
		}},
	}
	for _, tc := range cases {
		handle := covDBFakeHandle(t, tc.queries)
		if _, err := readPostgresSchema(ctx, handle); err == nil {
			t.Fatalf("%s: deveria falhar", tc.name)
		}
	}
}

func TestCovDBTableExistsPostgres(t *testing.T) {
	ctx := context.Background()
	handle := covDBFakeHandle(t, []covDBFakeQuery{
		{match: "information_schema.tables", cols: []string{"count"}, rows: [][]driver.Value{{1}}},
	})
	exists, err := tableExists(ctx, handle, DialectPostgres, "tenants")
	if err != nil || !exists {
		t.Fatalf("tableExists = %v %v", exists, err)
	}

	// erro na consulta
	if _, err := tableExists(ctx, covDBClosed(t), DialectPostgres, "tenants"); err == nil {
		t.Fatal("banco fechado deveria falhar")
	}
	if _, err := tableExists(ctx, covDBClosed(t), DialectSQLite, "tenants"); err == nil {
		t.Fatal("banco fechado deveria falhar")
	}

	// presente e ausente no SQLite
	store, err := Open(ctx, "file:"+filepath.Join(t.TempDir(), "existencia.db"), "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = store.Close() }()
	if exists, err := tableExists(ctx, store.DB(), DialectSQLite, "tenants"); err != nil || !exists {
		t.Fatalf("tenants deveria existir: %v %v", exists, err)
	}
	if exists, err := tableExists(ctx, store.DB(), DialectSQLite, "tabela_ausente"); err != nil || exists {
		t.Fatalf("tabela ausente: %v %v", exists, err)
	}
}

func TestCovDBReadSQLiteSchemaErrorPaths(t *testing.T) {
	ctx := context.Background()
	closed := covDBClosed(t)
	// a primeira consulta falha
	if _, err := readSQLiteSchema(ctx, closed); err == nil {
		t.Fatal("banco fechado deveria falhar")
	}
	// a leitura das colunas de uma tabela falha
	if _, err := readSQLiteSchema(ctx, covDBFakeHandle(t, []covDBFakeQuery{
		{match: "type = 'table'", cols: []string{"name"}, rows: [][]driver.Value{{"t"}}},
		{match: "pragma_table_info", err: errors.New("sem colunas")},
	})); err == nil {
		t.Fatal("falha ao ler colunas deveria subir")
	}
	// a listagem de índices falha
	if _, err := readSQLiteSchema(ctx, covDBFakeHandle(t, []covDBFakeQuery{
		{match: "type = 'table'", cols: []string{"name"}},
		{match: "type = 'index'", err: errors.New("sem índices")},
	})); err == nil {
		t.Fatal("falha ao listar índices deveria subir")
	}
	// erro ao ler o nome da tabela
	if _, err := readSQLiteSchema(ctx, covDBFakeHandle(t, []covDBFakeQuery{
		{match: "type = 'table'", cols: []string{"name"}, rows: [][]driver.Value{{struct{}{}}}},
	})); err == nil {
		t.Fatal("nome de tabela inválido deveria falhar")
	}
	// erro no meio da listagem de tabelas
	if _, err := readSQLiteSchema(ctx, covDBFakeHandle(t, []covDBFakeQuery{
		{match: "type = 'table'", cols: []string{"name"}, nextErr: errors.New("caiu no meio")},
	})); err == nil {
		t.Fatal("erro de percurso deveria subir")
	}
	// erro ao ler o nome do índice
	if _, err := readSQLiteSchema(ctx, covDBFakeHandle(t, []covDBFakeQuery{
		{match: "type = 'table'", cols: []string{"name"}},
		{match: "type = 'index'", cols: []string{"name"}, rows: [][]driver.Value{{struct{}{}}}},
	})); err == nil {
		t.Fatal("nome de índice inválido deveria falhar")
	}
	// erro no meio da listagem de índices
	if _, err := readSQLiteSchema(ctx, covDBFakeHandle(t, []covDBFakeQuery{
		{match: "type = 'table'", cols: []string{"name"}},
		{match: "type = 'index'", cols: []string{"name"}, nextErr: errors.New("caiu no meio")},
	})); err == nil {
		t.Fatal("erro de percurso dos índices deveria subir")
	}
}

func TestCovDBSqliteColumnsErrorPaths(t *testing.T) {
	ctx := context.Background()
	if _, err := sqliteColumns(ctx, covDBClosed(t), "t"); err == nil {
		t.Fatal("banco fechado deveria falhar")
	}
	// nome de coluna inválido
	if _, err := sqliteColumns(ctx, covDBFakeHandle(t, []covDBFakeQuery{
		{match: "pragma_table_info", cols: []string{"name"}, rows: [][]driver.Value{{struct{}{}}}},
	}), "t"); err == nil {
		t.Fatal("coluna inválida deveria falhar")
	}
	// erro no meio da leitura
	if _, err := sqliteColumns(ctx, covDBFakeHandle(t, []covDBFakeQuery{
		{match: "pragma_table_info", cols: []string{"name"}, nextErr: errors.New("caiu no meio")},
	}), "t"); err == nil {
		t.Fatal("erro de percurso deveria subir")
	}
}

func TestCovDBReadSQLiteSchemaIgnoresControlTables(t *testing.T) {
	ctx := context.Background()
	handle := covDBFakeHandle(t, []covDBFakeQuery{
		{match: "type = 'table'", cols: []string{"name"}, rows: [][]driver.Value{
			{"goose_db_version"}, {"__drizzle_migrations"}, {"tenants"},
		}},
		{match: "type = 'index'", cols: []string{"name"}},
		{match: "pragma_table_info", cols: []string{"name"}, rows: [][]driver.Value{{"id"}}},
	})
	snapshot, err := readSQLiteSchema(ctx, handle)
	if err != nil {
		t.Fatalf("readSQLiteSchema: %v", err)
	}
	if _, ok := snapshot.columns["goose_db_version"]; ok {
		t.Fatal("tabela de controle do goose não deveria entrar")
	}
	if _, ok := snapshot.columns["__drizzle_migrations"]; ok {
		t.Fatal("tabela de controle do drizzle não deveria entrar")
	}
	if _, ok := snapshot.columns["tenants"]; !ok {
		t.Fatal("a tabela de domínio deveria entrar")
	}
}
