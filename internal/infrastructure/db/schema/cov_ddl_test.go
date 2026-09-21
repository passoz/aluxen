package schema

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func covSchemaNames(tables []Table) []string {
	names := make([]string, 0, len(tables))
	for _, table := range tables {
		names = append(names, table.Name)
	}
	return names
}

func covSchemaColumn(table Table, name string) (Column, bool) {
	for _, column := range table.Columns {
		if column.Name == name {
			return column, true
		}
	}
	return Column{}, false
}

func TestCovSchemaParseSQLBasic(t *testing.T) {
	source := `
-- um comentário solto
CREATE TABLE IF NOT EXISTS "tenants" (
  id text PRIMARY KEY NOT NULL,
  name text NOT NULL,
  cpf text,
  created_at text NOT NULL DEFAULT 'x'
);

CREATE TABLE leases (
  id text PRIMARY KEY,
  tenant_id text NOT NULL,
  amount real,
  FOREIGN KEY (tenant_id) REFERENCES tenants (id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX "leases_tenant_idx" ON "leases" (tenant_id, id);
`
	got, err := ParseSQL(source, SQLite)
	if err != nil {
		t.Fatalf("ParseSQL: %v", err)
	}
	if names := covSchemaNames(got.Tables); strings.Join(names, ",") != "leases,tenants" {
		t.Fatalf("tabelas = %v (esperava ordenado)", names)
	}

	tenants := got.Tables[1]
	if tenants.DDL == "" {
		t.Fatal("o DDL bruto deveria ficar guardado para o rebuild")
	}
	id, ok := covSchemaColumn(tenants, "id")
	if !ok || id.Type != "text" || !id.Primary || !id.NotNull {
		t.Fatalf("coluna id inesperada: %+v", id)
	}
	cpf, _ := covSchemaColumn(tenants, "cpf")
	if cpf.NotNull {
		t.Fatalf("cpf deveria ser anulável: %+v", cpf)
	}

	leases := got.Tables[0]
	amount, _ := covSchemaColumn(leases, "amount")
	if amount.Type != "real" {
		t.Fatalf("real deveria virar real canônico: %+v", amount)
	}
	if len(leases.ForeignKeys) != 1 {
		t.Fatalf("esperava 1 chave estrangeira, veio %+v", leases.ForeignKeys)
	}
	fk := leases.ForeignKeys[0]
	if fk.Column != "tenant_id" || fk.Table != "tenants" || fk.OnDelete != "CASCADE" {
		t.Fatalf("chave estrangeira inesperada: %+v", fk)
	}

	if len(got.Indexes) != 1 {
		t.Fatalf("esperava 1 índice, veio %+v", got.Indexes)
	}
	index := got.Indexes[0]
	if index.Name != "leases_tenant_idx" || index.Table != "leases" || !index.Unique {
		t.Fatalf("índice inesperado: %+v", index)
	}
	if strings.Join(index.Columns, ",") != "tenant_id,id" {
		t.Fatalf("colunas do índice = %v", index.Columns)
	}
}

func TestCovSchemaParseSQLForeignKeyDefaultsAndCompositePK(t *testing.T) {
	source := `
CREATE TABLE parent (id integer PRIMARY KEY);

CREATE TABLE child (
  a integer NOT NULL,
  b integer NOT NULL,
  parent_id integer,
  PRIMARY KEY (a, b),
  FOREIGN KEY (parent_id) REFERENCES "parent" (id)
);
`
	got, err := ParseSQL(source, SQLite)
	if err != nil {
		t.Fatalf("ParseSQL: %v", err)
	}
	child := got.Tables[0]
	if child.Name != "child" {
		t.Fatalf("tabela = %q", child.Name)
	}
	fk := child.ForeignKeys[0]
	if fk.OnDelete != "NO ACTION" {
		t.Fatalf("sem ON DELETE o padrão é NO ACTION: %+v", fk)
	}
	// a chave primária composta é registrada nas colunas
	a, _ := covSchemaColumn(child, "a")
	b, _ := covSchemaColumn(child, "b")
	if a.Primary || b.Primary {
		t.Fatal("chave primária composta não marca as colunas individualmente")
	}
	if len(child.Columns) != 3 {
		t.Fatalf("colunas = %+v", child.Columns)
	}
}

func TestCovSchemaParseSQLAddColumnAndGooseDown(t *testing.T) {
	source := `
-- +goose Up
CREATE TABLE leases (id text PRIMARY KEY, start_date text NOT NULL);

-- +goose StatementBegin
ALTER TABLE leases ADD COLUMN due_day integer NOT NULL DEFAULT 1;
ALTER TABLE "leases" ADD due_date text;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE leases DROP COLUMN due_day;
CREATE TABLE ignorada (id text);
`
	got, err := ParseSQL(source, SQLite)
	if err != nil {
		t.Fatalf("ParseSQL: %v", err)
	}
	if len(got.Tables) != 1 || got.Tables[0].Name != "leases" {
		t.Fatalf("o bloco Down não pode virar schema: %+v", covSchemaNames(got.Tables))
	}
	lease := got.Tables[0]
	if names := covSchemaColumns(lease); strings.Join(names, ",") != "id,start_date,due_day,due_date" {
		t.Fatalf("colunas após ALTER = %v", names)
	}
	dueDay, _ := covSchemaColumn(lease, "due_day")
	if !dueDay.NotNull || dueDay.Type != "integer" {
		t.Fatalf("coluna adicionada inesperada: %+v", dueDay)
	}
}

func covSchemaColumns(table Table) []string {
	names := make([]string, 0, len(table.Columns))
	for _, column := range table.Columns {
		names = append(names, column.Name)
	}
	return names
}

func TestCovSchemaParseSQLGooseUpResumesAfterDown(t *testing.T) {
	// dois arquivos concatenados: o Down de um não pode esconder o Up do outro
	source := `
-- +goose Up
CREATE TABLE a (id text);

-- +goose Down
DROP TABLE a;

-- +goose Up
CREATE TABLE b (id text);

-- +goose Down
DROP TABLE b;
`
	got, err := ParseSQL(source, SQLite)
	if err != nil {
		t.Fatalf("ParseSQL: %v", err)
	}
	if names := covSchemaNames(got.Tables); strings.Join(names, ",") != "a,b" {
		t.Fatalf("tabelas = %v", names)
	}
}

func TestCovSchemaParseSQLPostgresTypes(t *testing.T) {
	source := `CREATE TABLE receipts (id text PRIMARY KEY, amount double precision NOT NULL);`
	got, err := ParseSQL(source, Postgres)
	if err != nil {
		t.Fatalf("ParseSQL: %v", err)
	}
	amount, ok := covSchemaColumn(got.Tables[0], "amount")
	if !ok || amount.Type != "real" {
		t.Fatalf("double precision deveria virar real canônico: %+v", amount)
	}
}

func TestCovSchemaParseSQLRejectsForeignDialectTypes(t *testing.T) {
	// `real` no Postgres é float de 4 bytes: recusado com a dica do dialeto certo
	if _, err := ParseSQL(`CREATE TABLE t (amount real);`, Postgres); err == nil {
		t.Fatal("real no Postgres deveria ser recusado")
	} else if !strings.Contains(err.Error(), "double precision") {
		t.Fatalf("erro deveria explicar a alternativa: %v", err)
	}
	if _, err := ParseSQL(`CREATE TABLE t (amount double precision);`, SQLite); err == nil {
		t.Fatal("double precision no SQLite deveria ser recusado")
	} else if !strings.Contains(err.Error(), "real") {
		t.Fatalf("erro deveria explicar a alternativa: %v", err)
	}
}

func TestCovSchemaParseSQLRejectsUnknownType(t *testing.T) {
	_, err := ParseSQL(`CREATE TABLE t (amount blob);`, SQLite)
	if err == nil {
		t.Fatal("tipo desconhecido deveria falhar")
	}
	if !strings.Contains(err.Error(), "blob") || !strings.Contains(err.Error(), "integer") {
		t.Fatalf("erro deveria citar o tipo e listar os aceitos: %v", err)
	}
}

func TestCovSchemaParseSQLMalformed(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{"parêntese de fechamento ausente", `CREATE TABLE t (id text`},
		{"coluna sem tipo", `CREATE TABLE t (id)`},
		{"alter em tabela ausente", `ALTER TABLE ausente ADD COLUMN x text`},
		{"alter com tipo desconhecido", `CREATE TABLE t (id text); ALTER TABLE t ADD COLUMN x blob;`},
		{"create index sem on", `CREATE INDEX sem_on`},
		{"create table sem nome", `CREATE TABLE`},
	}
	for _, tc := range cases {
		if _, err := ParseSQL(tc.source, SQLite); err == nil {
			t.Fatalf("%s: deveria falhar", tc.name)
		}
	}
}

func TestCovSchemaParseSQLIgnoresOtherStatements(t *testing.T) {
	source := `
INSERT INTO t VALUES (1);
DROP TABLE t;
PRAGMA foreign_keys = ON;
`
	got, err := ParseSQL(source, SQLite)
	if err != nil {
		t.Fatalf("instruções fora do vocabulário devem ser ignoradas: %v", err)
	}
	if len(got.Tables) != 0 || len(got.Indexes) != 0 {
		t.Fatalf("nada deveria ter sido lido: %+v", got)
	}
}

func TestCovSchemaParseSQLQuotedIdentifiers(t *testing.T) {
	source := "CREATE TABLE `Reservado` (ID INTEGER, `Nome` TEXT NOT NULL);"
	got, err := ParseSQL(source, SQLite)
	if err != nil {
		t.Fatalf("ParseSQL: %v", err)
	}
	if got.Tables[0].Name != "reservado" {
		t.Fatalf("aspa deve ser removida e o nome normalizado: %q", got.Tables[0].Name)
	}
	if names := covSchemaColumns(got.Tables[0]); strings.Join(names, ",") != "id,nome" {
		t.Fatalf("colunas = %v", names)
	}
}

func TestCovSchemaLoadSQLite(t *testing.T) {
	ctx := context.Background()
	handle, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("abrir sqlite: %v", err)
	}
	defer func() { _ = handle.Close() }()

	statements := []string{
		`CREATE TABLE tenants (id text PRIMARY KEY, name text NOT NULL)`,
		`CREATE TABLE leases (id text PRIMARY KEY, tenant_id text NOT NULL REFERENCES tenants (id) ON DELETE CASCADE)`,
		`CREATE TABLE goose_db_version (id integer PRIMARY KEY, version_id integer)`,
		`CREATE UNIQUE INDEX leases_tenant_idx ON leases (tenant_id)`,
	}
	for _, statement := range statements {
		if _, err := handle.ExecContext(ctx, statement); err != nil {
			t.Fatalf("executar %q: %v", statement, err)
		}
	}

	got, err := LoadSQLite(ctx, handle, map[string]bool{"goose_db_version": true})
	if err != nil {
		t.Fatalf("LoadSQLite: %v", err)
	}
	if names := covSchemaNames(got.Tables); strings.Join(names, ",") != "leases,tenants" {
		t.Fatalf("a tabela de controle deveria ser omitida: %v", names)
	}
	if len(got.Indexes) != 1 || got.Indexes[0].Name != "leases_tenant_idx" {
		t.Fatalf("índices lidos = %+v", got.Indexes)
	}
}

func TestCovSchemaLoadSQLiteIgnoresIndexOfIgnoredTable(t *testing.T) {
	ctx := context.Background()
	handle, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("abrir sqlite: %v", err)
	}
	defer func() { _ = handle.Close() }()

	for _, statement := range []string{
		`CREATE TABLE controle (id text PRIMARY KEY)`,
		`CREATE INDEX controle_idx ON controle (id)`,
		`CREATE TABLE manter (id text PRIMARY KEY)`,
	} {
		if _, err := handle.ExecContext(ctx, statement); err != nil {
			t.Fatalf("executar %q: %v", statement, err)
		}
	}

	got, err := LoadSQLite(ctx, handle, map[string]bool{"controle": true})
	if err != nil {
		t.Fatalf("LoadSQLite: %v", err)
	}
	if names := covSchemaNames(got.Tables); strings.Join(names, ",") != "manter" {
		t.Fatalf("tabelas = %v", names)
	}
	if len(got.Indexes) != 0 {
		t.Fatalf("índice de tabela ignorada não pode aparecer: %+v", got.Indexes)
	}
}

func TestCovSchemaLoadSQLiteQueryError(t *testing.T) {
	handle, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("abrir sqlite: %v", err)
	}
	_ = handle.Close()
	if _, err := LoadSQLite(context.Background(), handle, nil); err == nil {
		t.Fatal("banco fechado deveria devolver erro")
	}
}

func TestCovSchemaLoadSQLiteUnsupportedType(t *testing.T) {
	ctx := context.Background()
	handle, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("abrir sqlite: %v", err)
	}
	defer func() { _ = handle.Close() }()

	// um tipo fora dos três canônicos faz a leitura do schema falhar
	if _, err := handle.ExecContext(ctx, `CREATE TABLE estranha (id blob)`); err != nil {
		t.Fatalf("criar tabela: %v", err)
	}
	if _, err := LoadSQLite(ctx, handle, nil); err == nil {
		t.Fatal("tipo não canônico deveria falhar na leitura do schema")
	}
}

func TestCovSchemaParseSQLSkipsEmptyItemAndOrdersMultipleIndexesAndFKs(t *testing.T) {
	source := `
CREATE TABLE filha (
  id text NOT NULL,
  tenant_id text,
  property_id text,
  receita_id text,
  FOREIGN KEY (property_id) REFERENCES properties (id) ON DELETE SET NULL,
  FOREIGN KEY (tenant_id) REFERENCES tenants (id) ON DELETE CASCADE,
  FOREIGN KEY (receita_id) REFERENCES receipts (id),
);

CREATE INDEX z_idx ON filha (id);
CREATE UNIQUE INDEX a_idx ON filha (tenant_id);
`
	got, err := ParseSQL(source, SQLite)
	if err != nil {
		t.Fatalf("ParseSQL: %v", err)
	}
	table := got.Tables[0]
	if len(table.Columns) != 4 {
		t.Fatalf("item vazio não deveria virar coluna: %+v", table.Columns)
	}
	if names := covSchemaFKColumns(table); strings.Join(names, ",") != "property_id,receita_id,tenant_id" {
		t.Fatalf("chaves estrangeiras deveriam vir ordenadas: %v", names)
	}
	if names := covSchemaIndexNames(got.Indexes); strings.Join(names, ",") != "a_idx,z_idx" {
		t.Fatalf("índices deveriam vir ordenados: %v", names)
	}
}

func covSchemaFKColumns(table Table) []string {
	names := make([]string, 0, len(table.ForeignKeys))
	for _, fk := range table.ForeignKeys {
		names = append(names, fk.Column)
	}
	return names
}

func covSchemaIndexNames(indexes []Index) []string {
	names := make([]string, 0, len(indexes))
	for _, index := range indexes {
		names = append(names, index.Name)
	}
	return names
}

// ── Diff ───────────────────────────────────────────────────────────

func covSchemaTable(name string, columns ...Column) Table {
	return Table{Name: name, Columns: columns}
}

func TestCovSchemaDiffReportsEveryDivergence(t *testing.T) {
	expected := Schema{
		Tables: []Table{
			covSchemaTable("only_expected",
				Column{Name: "id", Type: "text", Primary: true},
			),
			covSchemaTable("with_index",
				Column{Name: "id", Type: "text", Primary: true, NotNull: true},
				Column{Name: "amount", Type: "real"},
			),
			covSchemaTable("fk_table",
				Column{Name: "id", Type: "text"},
				Column{Name: "tenant_id", Type: "text"},
			),
		},
		Indexes: []Index{{Name: "expected_index", Table: "with_index", Unique: true, Columns: []string{"id"}}},
	}
	expected.Tables[2].ForeignKeys = []ForeignKey{{Column: "tenant_id", Table: "tenants", OnDelete: "CASCADE"}}

	actual := Schema{
		Tables: []Table{
			{Name: "only_expected", Columns: []Column{{Name: "id", Type: "text", Primary: true}, {Name: "extra", Type: "text"}}},
			{Name: "with_index", Columns: []Column{
				{Name: "amount", Type: "text", NotNull: true},
				{Name: "id", Type: "text"}, // ordem trocada, tipo e nulidade/chave divergem
			}},
			{Name: "only_actual", Columns: []Column{{Name: "id", Type: "text"}}},
			{Name: "fk_table", Columns: []Column{{Name: "id", Type: "text"}, {Name: "tenant_id", Type: "text"}, {Name: "outra", Type: "text"}}},
		},
	}
	actual.Tables[3].ForeignKeys = []ForeignKey{{Column: "tenant_id", Table: "outro", OnDelete: "SET NULL"}}

	diffs := Diff(expected, actual, "esperado", "real")
	joined := strings.Join(diffs, "\n")
	wants := []string{
		`tabela "fk_table"`,
		`tabela "only_actual"`,
		`coluna "extra" sobra`,
		`coluna "amount" é "real" em esperado e "text" em real`,
		`nulidade de "amount" difere`,
		`chave primária de "id" difere`,
		`ordem das colunas difere`,
		`coluna "outra" sobra`,
		`chave estrangeira de "tenant_id" aponta para "tenants"`,
		`ON DELETE de "tenant_id"`,
		`índice "expected_index" existe em esperado e falta em real`,
	}
	for _, want := range wants {
		if !strings.Contains(joined, want) {
			t.Fatalf("faltou a divergência %q em:\n%s", want, joined)
		}
	}
}

func TestCovSchemaDiffColumnCountAndMissingPieces(t *testing.T) {
	expected := Schema{
		Tables: []Table{
			{
				Name:    "leases",
				Columns: []Column{{Name: "id", Type: "text"}, {Name: "tenant_id", Type: "text"}},
				ForeignKeys: []ForeignKey{
					{Column: "tenant_id", Table: "tenants", OnDelete: "CASCADE"},
					{Column: "id", Table: "receipts", OnDelete: "NO ACTION"},
				},
			},
			{Name: "so_na_esperanca", Columns: []Column{{Name: "id", Type: "text"}}},
		},
	}
	actual := Schema{
		Tables: []Table{{Name: "leases", Columns: []Column{{Name: "id", Type: "text"}}}},
	}

	diffs := Diff(expected, actual, "esperado", "real")
	joined := strings.Join(diffs, "\n")
	for _, want := range []string{
		`tem 2 colunas e real tem 1`,
		`coluna "tenant_id" falta em real`,
		`chave estrangeira de "tenant_id" falta em real`,
		`chave estrangeira de "id" falta em real`,
		`tabela "so_na_esperanca" existe em esperado e falta em real`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("faltou %q em:\n%s", want, joined)
		}
	}

	// banco com chave estrangeira a mais
	extra := Schema{Tables: []Table{{
		Name:        "leases",
		Columns:     []Column{{Name: "id", Type: "text"}, {Name: "tenant_id", Type: "text"}},
		ForeignKeys: []ForeignKey{{Column: "tenant_id", Table: "tenants", OnDelete: "CASCADE"}, {Column: "id", Table: "receipts", OnDelete: "NO ACTION"}},
	}}}
	extra.Tables[0].ForeignKeys = append(extra.Tables[0].ForeignKeys, ForeignKey{Column: "sobra", Table: "x", OnDelete: "CASCADE"})
	diffJoined := strings.Join(Diff(expected, extra, "esperado", "real"), "\n")
	if !strings.Contains(diffJoined, `chave estrangeira de "sobra" sobra em real`) {
		t.Fatalf("chave estrangeira a mais não foi apontada:\n%s", diffJoined)
	}
}

func TestCovSchemaDiffIndexDivergences(t *testing.T) {
	expected := Schema{Indexes: []Index{
		{Name: "idx_a", Table: "t", Unique: true, Columns: []string{"a", "b"}},
	}}
	actual := Schema{Indexes: []Index{
		{Name: "idx_a", Table: "outra", Unique: false, Columns: []string{"b"}},
		{Name: "idx_extra", Table: "t", Columns: []string{"c"}},
	}}

	diffs := Diff(expected, actual, "esperado", "real")
	joined := strings.Join(diffs, "\n")
	for _, want := range []string{
		`tabela é "t" em esperado e "outra" em real`,
		`unicidade difere`,
		`colunas são (a, b) em esperado e (b) em real`,
		`índice "idx_extra" existe em real e falta em esperado`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("faltou %q em:\n%s", want, joined)
		}
	}
}

func TestCovSchemaDiffEqualSchemas(t *testing.T) {
	schema := Schema{
		Tables: []Table{{
			Name:        "t",
			Columns:     []Column{{Name: "id", Type: "text", Primary: true, NotNull: true}},
			ForeignKeys: []ForeignKey{{Column: "id", Table: "x", OnDelete: "CASCADE"}},
		}},
		Indexes: []Index{{Name: "idx", Table: "t", Unique: true, Columns: []string{"id"}}},
	}
	if diffs := Diff(schema, schema, "a", "b"); len(diffs) != 0 {
		t.Fatalf("schemas iguais não podem divergir: %v", diffs)
	}
}

func TestCovSchemaSplitStatementsHandlesComments(t *testing.T) {
	statements := splitStatements("CREATE TABLE a (id text); -- criação\n-- linha inteira\nCREATE TABLE b (id text);")
	if len(statements) != 2 {
		t.Fatalf("esperava 2 instruções, veio %d: %v", len(statements), statements)
	}
}
