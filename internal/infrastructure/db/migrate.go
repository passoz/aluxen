package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"

	"github.com/passoz/aluxen/internal/infrastructure/db/schema"
	"github.com/passoz/aluxen/migrations"
)

// gooseMu serializa o estado global do goose (SetBaseFS/SetDialect), que é
// compartilhado por todas as conexões do processo.
var gooseMu sync.Mutex

// runMigrations leva o banco ao schema final, adotando a base que já existe:
//
//  1. no SQLite, uma base viva ganha primeiro os remendos do app atual (colunas
//     que faltam e o rebuild de `receipts`) — nada é recriado e nenhuma linha se
//     perde;
//  2. o schema atual é comparado com o que cada migração embutida deve produzir:
//     as migrações que já estão refletidas na base são apenas registradas como
//     aplicadas (o SQLite não tem `ADD COLUMN IF NOT EXISTS`, e reaplicar um
//     `ADD COLUMN` derruba o boot);
//  3. o goose aplica o que falta.
//
// O passo 2 existe porque o produto roda por instância: restaurar um dump numa
// base vazia deixa o schema final presente e a tabela de controle do goose
// ausente.
func runMigrations(ctx context.Context, sqlDB *sql.DB, dialect Dialect) error {
	reference, err := migrationSnapshots(dialect)
	if err != nil {
		return err
	}
	if dialect == DialectSQLite {
		if err := repairSQLiteLegacy(ctx, sqlDB, reference); err != nil {
			return err
		}
	}

	actual, err := readSchema(ctx, sqlDB, dialect)
	if err != nil {
		return err
	}
	if covered := coveredVersions(reference, actual); len(covered) > 0 {
		if err := markAppliedVersions(ctx, sqlDB, dialect, covered); err != nil {
			return err
		}
	}
	return applyGoose(ctx, sqlDB, dialect)
}

// applyGoose aplica as migrações pendentes do dialeto.
func applyGoose(ctx context.Context, sqlDB *sql.DB, dialect Dialect) error {
	gooseMu.Lock()
	defer gooseMu.Unlock()

	if err := prepareGoose(dialect); err != nil {
		return err
	}
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		return fmt.Errorf("aplicar migrações de %s: %w", dialect, err)
	}
	return nil
}

// prepareGoose aponta o goose para o SQL embutido do dialeto. Chamar com gooseMu
// travado.
func prepareGoose(dialect Dialect) error {
	dir, name := "sqlite", "sqlite"
	if dialect == DialectPostgres {
		dir, name = "postgres", "postgres"
	}
	embedded, err := fs.Sub(migrations.FS, dir)
	if err != nil {
		return fmt.Errorf("abrir migrações embutidas de %s: %w", dir, err)
	}
	goose.SetBaseFS(embedded)
	if err := goose.SetDialect(name); err != nil {
		return fmt.Errorf("selecionar dialeto %s no goose: %w", name, err)
	}
	return nil
}

// ── Adoção de bases SQLite legadas ─────────────────────────────────

// legacyColumn é uma coluna acrescentada ao schema depois da versão inicial do
// app (migrações Drizzle 0001/0004/0005 e os remendos de `src/db/index.ts`).
type legacyColumn struct {
	table      string
	column     string
	definition string
}

// legacyColumns reproduz, na mesma ordem, os ALTER TABLE que o app atual aplica
// de forma tolerante a erro a cada boot.
var legacyColumns = []legacyColumn{
	{"leases", "due_day", "integer NOT NULL DEFAULT 1"},
	{"tenants", "cpf", "text"},
	{"leases", "status", "text NOT NULL DEFAULT 'active'"},
	{"receipts", "generated_key", "text"},
	{"receipts", "updated_at", "text"},
	{"leases", "auth_token", "text"},
	{"receipts", "auth_token", "text"},
	{"receipts", "tenant_id", "text"},
	{"receipts", "property_id", "text"},
	{"receipts", "category", "text NOT NULL DEFAULT 'rent'"},
	{"receipts", "description", "text"},
	{"receipts", "paid_amount", "real"},
	{"tenants", "password_hash", "text"},
	{"tenants", "password_changed_at", "text"},
	{"users", "password_changed_at", "text"},
}

// repairSQLiteLegacy deixa uma base SQLite viva o mais perto possível do schema
// atual antes de o goose entrar em cena:
//
//  1. acrescenta as colunas que faltam (tolerando coluna já existente);
//  2. recria `receipts` quando ela ainda vem do schema antigo (lease_id NOT
//     NULL), preservando as linhas — equivalente à migração Drizzle 0002.
//
// Nada é recriado e nenhuma linha se perde: a base de produção é o mesmo
// arquivo. O que ainda faltar depois daqui o goose aplica.
func repairSQLiteLegacy(ctx context.Context, sqlDB *sql.DB, reference []migrationSnapshot) error {
	existing, err := readSQLiteSchema(ctx, sqlDB)
	if err != nil {
		return err
	}
	if len(existing.columns) == 0 {
		return nil // banco novo: o goose cria o schema final
	}

	for _, column := range legacyColumns {
		columns, ok := existing.columns[column.table]
		if !ok || columns[column.column] {
			continue
		}
		if err := addLegacyColumn(ctx, sqlDB, column); err != nil {
			return err
		}
	}

	if existing.columns["receipts"]["lease_id"] {
		required, err := sqliteColumnRequired(ctx, sqlDB, "receipts", "lease_id")
		if err != nil {
			return err
		}
		if required {
			ddl, err := receiptTableDDL(reference)
			if err != nil {
				return err
			}
			if err := rebuildReceipts(ctx, sqlDB, existing.columns["receipts"], ddl); err != nil {
				return err
			}
		}
	}
	return nil
}

// receiptTableDDL devolve o CREATE TABLE de `receipts` como a migração embutida
// o escreve: o rebuild da base antiga usa a mesma definição, sem uma terceira
// cópia do DDL. Vale a definição mais recente, que é o estado atual.
func receiptTableDDL(reference []migrationSnapshot) (string, error) {
	for index := len(reference) - 1; index >= 0; index-- {
		for _, table := range reference[index].schema.Tables {
			if table.Name == "receipts" && table.DDL != "" {
				return table.DDL, nil
			}
		}
	}
	return "", errors.New("definição de receipts ausente na migração embutida")
}

// migrationSource abre o SQL embutido do dialeto.
func migrationSource(dialect Dialect) (fs.FS, string, error) {
	dir, name := "sqlite", "sqlite"
	if dialect == DialectPostgres {
		dir, name = "postgres", "postgres"
	}
	embedded, err := fs.Sub(migrations.FS, dir)
	if err != nil {
		return nil, "", fmt.Errorf("abrir migrações embutidas de %s: %w", dir, err)
	}
	return embedded, name, nil
}

// migrationSnapshot é o schema que uma migração embutida deve produzir, já
// considerando as anteriores.
type migrationSnapshot struct {
	version int64
	schema  schema.Schema
}

// migrationSnapshots lê as migrações embutidas em ordem e devolve, para cada
// uma, o schema acumulado até ela. É a expectativa da adoção: nada aqui é
// copiado do DDL canônico, tudo vem do próprio SQL que o goose aplicaria.
func migrationSnapshots(dialect Dialect) ([]migrationSnapshot, error) {
	embedded, _, err := migrationSource(dialect)
	if err != nil {
		return nil, err
	}
	names, err := fs.Glob(embedded, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("listar migrações embutidas de %s: %w", dialect, err)
	}
	sort.Strings(names)

	parserDialect := schema.SQLite
	if dialect == DialectPostgres {
		parserDialect = schema.Postgres
	}

	var accumulated strings.Builder
	snapshots := make([]migrationSnapshot, 0, len(names))
	for _, name := range names {
		content, err := fs.ReadFile(embedded, name)
		if err != nil {
			return nil, fmt.Errorf("ler migração %q: %w", name, err)
		}
		accumulated.Write(content)
		accumulated.WriteString("\n")

		parsed, err := schema.ParseSQL(accumulated.String(), parserDialect)
		if err != nil {
			return nil, fmt.Errorf("ler o schema da migração %q: %w", name, err)
		}
		prefix, _, found := strings.Cut(path.Base(name), "_")
		if !found {
			return nil, fmt.Errorf("migração %q sem versão no nome", name)
		}
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("migração %q com versão inválida: %w", name, err)
		}
		snapshots = append(snapshots, migrationSnapshot{version: version, schema: parsed})
	}
	return snapshots, nil
}

// coveredVersions devolve as migrações que o banco já reflete, em ordem,
// parando na primeira que ele não cobre: o que vem depois dela ainda precisa
// rodar.
func coveredVersions(reference []migrationSnapshot, actual schemaSnapshot) []int64 {
	var covered []int64
	for _, snapshot := range reference {
		if !actual.covers(snapshot.schema) {
			break
		}
		covered = append(covered, snapshot.version)
	}
	return covered
}

// addLegacyColumn acrescenta a coluna, ignorando a falha: uma base viva pode
// estar num estado intermediário (coluna criada por outro caminho) e o boot não
// deve ser interrompido por isso.
func addLegacyColumn(ctx context.Context, sqlDB *sql.DB, column legacyColumn) error {
	statement := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", column.table, column.column, column.definition)
	if _, err := sqlDB.ExecContext(ctx, statement); err != nil {
		slog.Warn("ajuste de schema ignorado",
			"scope", "db.migrate", "table", column.table, "column", column.column, "err", err)
		return nil
	}
	slog.Info("coluna adicionada", "scope", "db.migrate", "table", column.table, "column", column.column)
	return nil
}

// rebuildReceipts troca a tabela `receipts` antiga pela do schema final,
// copiando as colunas que existirem na origem. A definição da tabela nova vem do
// próprio SQL da migração, para não haver uma terceira cópia do DDL.
func rebuildReceipts(ctx context.Context, sqlDB *sql.DB, source map[string]bool, referenceDDL string) error {
	if referenceDDL == "" {
		return errors.New("definição de receipts ausente na migração embutida")
	}
	create := strings.Replace(referenceDDL, "receipts", "receipts_new", 1)
	if create == referenceDDL {
		return errors.New("não foi possível derivar o DDL de receipts")
	}

	// A troca de tabela exige chaves estrangeiras desligadas; a conexão é única
	// (ver Open), então o PRAGMA vale para toda a operação.
	if _, err := sqlDB.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("desligar chaves estrangeiras: %w", err)
	}
	defer func() {
		if _, err := sqlDB.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
			slog.Error("religar chaves estrangeiras", "scope", "db.migrate", "err", err)
		}
	}()

	columns, selectList := receiptCopyColumns(source)
	statements := []string{
		"DROP TABLE IF EXISTS receipts_new",
		create,
		fmt.Sprintf("INSERT INTO receipts_new (%s) SELECT %s FROM receipts", strings.Join(columns, ", "), strings.Join(selectList, ", ")),
		"DROP TABLE receipts",
		"ALTER TABLE receipts_new RENAME TO receipts",
		"CREATE UNIQUE INDEX IF NOT EXISTS receipts_generated_key_idx ON receipts (generated_key)",
	}
	for _, statement := range statements {
		if _, err := sqlDB.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("recriar a tabela receipts: %w", err)
		}
	}
	slog.Info("tabela receipts recriada no schema atual", "scope", "db.migrate", "columns", len(columns))
	return nil
}

// receiptCopyColumns monta as listas de colunas de destino e de origem da cópia.
// Colunas obrigatórias que não existem na origem recebem o valor que o app atual
// gravaria (categoria 'rent' e created_at de agora).
func receiptCopyColumns(source map[string]bool) (target, selection []string) {
	for _, column := range []string{
		"id", "lease_id", "tenant_id", "property_id", "category", "description",
		"due_date", "paid_at", "amount", "paid_amount", "pdf_path", "generated_key",
		"auth_token", "created_at", "updated_at",
	} {
		switch {
		case source[column]:
			target = append(target, column)
			selection = append(selection, column)
		case column == "category":
			target = append(target, column)
			selection = append(selection, "'rent'")
		case column == "created_at":
			target = append(target, column)
			selection = append(selection, "datetime('now')")
		}
	}
	return target, selection
}

// markAppliedVersions registra as migrações já refletidas na base como
// aplicadas, sem executar o SQL delas: reaplicar um `ADD COLUMN` derruba o boot
// (o SQLite não tem `ADD COLUMN IF NOT EXISTS`) e recriar tabelas seria
// destrutivo.
func markAppliedVersions(ctx context.Context, sqlDB *sql.DB, dialect Dialect, versions []int64) error {
	if len(versions) == 0 {
		return nil
	}
	gooseDialect := database.DialectSQLite3
	if dialect == DialectPostgres {
		gooseDialect = database.DialectPostgres
	}
	store, err := database.NewStore(gooseDialect, goose.TableName())
	if err != nil {
		return fmt.Errorf("abrir controle de versão do goose: %w", err)
	}

	exists, err := tableExists(ctx, sqlDB, dialect, goose.TableName())
	if err != nil {
		return err
	}
	if !exists {
		if err := store.CreateVersionTable(ctx, sqlDB); err != nil {
			return fmt.Errorf("criar controle de versão do goose: %w", err)
		}
	}

	current, err := store.GetLatestVersion(ctx, sqlDB)
	if err != nil && !errors.Is(err, database.ErrVersionNotFound) {
		return fmt.Errorf("ler versão do schema: %w", err)
	}
	for _, version := range versions {
		if version <= current {
			continue
		}
		if err := store.Insert(ctx, sqlDB, database.InsertRequest{Version: version}); err != nil {
			return fmt.Errorf("registrar migração %d: %w", version, err)
		}
	}
	slog.Info("schema existente adotado", "scope", "db.migrate", "dialect", dialect, "versions", versions)
	return nil
}

// ── Leitura do schema atual ────────────────────────────────────────

// schemaSnapshot é a fotografia do schema de um banco: as colunas de cada tabela
// e os índices nomeados. É o que decide se uma migração já está refletida na base.
type schemaSnapshot struct {
	columns map[string]map[string]bool
	indexes map[string]bool
}

// readSchema lê o schema atual no dialeto informado.
func readSchema(ctx context.Context, sqlDB *sql.DB, dialect Dialect) (schemaSnapshot, error) {
	if dialect == DialectPostgres {
		return readPostgresSchema(ctx, sqlDB)
	}
	return readSQLiteSchema(ctx, sqlDB)
}

// readSQLiteSchema lê tabelas, colunas e índices, ignorando as tabelas internas
// do SQLite e as de controle de migração.
func readSQLiteSchema(ctx context.Context, sqlDB *sql.DB) (schemaSnapshot, error) {
	schema := schemaSnapshot{
		columns: map[string]map[string]bool{},
		indexes: map[string]bool{},
	}

	rows, err := sqlDB.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return schema, fmt.Errorf("listar tabelas do SQLite: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return schema, fmt.Errorf("ler tabela do SQLite: %w", err)
		}
		if isControlTable(name) {
			continue
		}
		schema.columns[name] = map[string]bool{}
	}
	if err := rows.Err(); err != nil {
		return schema, fmt.Errorf("percorrer tabelas do SQLite: %w", err)
	}

	for name := range schema.columns {
		columns, err := sqliteColumns(ctx, sqlDB, name)
		if err != nil {
			return schema, err
		}
		schema.columns[name] = columns
	}

	indexRows, err := sqlDB.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'index' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return schema, fmt.Errorf("listar índices do SQLite: %w", err)
	}
	defer indexRows.Close()
	for indexRows.Next() {
		var name string
		if err := indexRows.Scan(&name); err != nil {
			return schema, fmt.Errorf("ler índice do SQLite: %w", err)
		}
		schema.indexes[name] = true
	}
	if err := indexRows.Err(); err != nil {
		return schema, fmt.Errorf("percorrer índices do SQLite: %w", err)
	}

	return schema, nil
}

// sqliteColumns lê as colunas de uma tabela.
func sqliteColumns(ctx context.Context, sqlDB *sql.DB, table string) (map[string]bool, error) {
	rows, err := sqlDB.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, fmt.Errorf("listar colunas de %s: %w", table, err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("ler coluna de %s: %w", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("percorrer colunas de %s: %w", table, err)
	}
	return columns, nil
}

// sqliteColumnRequired informa se a coluna é NOT NULL.
func sqliteColumnRequired(ctx context.Context, sqlDB *sql.DB, table, column string) (bool, error) {
	row := sqlDB.QueryRowContext(ctx, "SELECT \"notnull\" FROM pragma_table_info(?) WHERE name = ?", table, column)
	var notNull int
	if err := row.Scan(&notNull); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("ler nulidade de %s.%s: %w", table, column, err)
	}
	return notNull == 1, nil
}

// tableExists informa se a tabela existe no dialeto informado.
func tableExists(ctx context.Context, sqlDB *sql.DB, dialect Dialect, table string) (bool, error) {
	if dialect == DialectPostgres {
		row := sqlDB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = $1`, table)
		var total int
		if err := row.Scan(&total); err != nil {
			return false, fmt.Errorf("procurar a tabela %s: %w", table, err)
		}
		return total > 0, nil
	}
	row := sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table)
	var total int
	if err := row.Scan(&total); err != nil {
		return false, fmt.Errorf("procurar a tabela %s: %w", table, err)
	}
	return total > 0, nil
}

// readPostgresSchema lê tabelas, colunas e índices do schema corrente via
// information_schema (índices em pg_indexes). Tabelas internas do Postgres não
// aparecem em information_schema, e as de controle do goose só somam: a
// comparação pergunta se o esperado está presente, nunca o contrário.
func readPostgresSchema(ctx context.Context, sqlDB *sql.DB) (schemaSnapshot, error) {
	snapshot := schemaSnapshot{columns: map[string]map[string]bool{}, indexes: map[string]bool{}}

	rows, err := sqlDB.QueryContext(ctx,
		`SELECT table_name, column_name FROM information_schema.columns WHERE table_schema = current_schema()`)
	if err != nil {
		return snapshot, fmt.Errorf("listar colunas do Postgres: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			return snapshot, fmt.Errorf("ler coluna do Postgres: %w", err)
		}
		if snapshot.columns[table] == nil {
			snapshot.columns[table] = map[string]bool{}
		}
		snapshot.columns[table][column] = true
	}
	if err := rows.Err(); err != nil {
		return snapshot, fmt.Errorf("percorrer colunas do Postgres: %w", err)
	}

	indexRows, err := sqlDB.QueryContext(ctx,
		`SELECT indexname FROM pg_indexes WHERE schemaname = current_schema()`)
	if err != nil {
		return snapshot, fmt.Errorf("listar índices do Postgres: %w", err)
	}
	defer indexRows.Close()
	for indexRows.Next() {
		var name string
		if err := indexRows.Scan(&name); err != nil {
			return snapshot, fmt.Errorf("ler índice do Postgres: %w", err)
		}
		snapshot.indexes[name] = true
	}
	if err := indexRows.Err(); err != nil {
		return snapshot, fmt.Errorf("percorrer índices do Postgres: %w", err)
	}
	return snapshot, nil
}

// covers informa se o banco já tem tudo o que o schema de referência define:
// todas as tabelas com todas as colunas e todos os índices nomeados.
func (s schemaSnapshot) covers(reference schema.Schema) bool {
	for _, table := range reference.Tables {
		columns, ok := s.columns[table.Name]
		if !ok {
			return false
		}
		for _, column := range table.Columns {
			if !columns[column.Name] {
				return false
			}
		}
	}
	for _, index := range reference.Indexes {
		if !s.indexes[index.Name] {
			return false
		}
	}
	return true
}

// isControlTable informa se a tabela pertence ao controle de migração (e não ao
// domínio).
func isControlTable(name string) bool {
	return name == goose.TableName() || name == "__drizzle_migrations"
}
