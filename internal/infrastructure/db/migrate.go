package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"sync"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"

	"github.com/passoz/aluxen/migrations"
)

// initialMigrationVersion é a versão da migração consolidada (00001_init.sql),
// que descreve o schema final do app atual.
const initialMigrationVersion int64 = 1

// gooseMu serializa o estado global do goose (SetBaseFS/SetDialect), que é
// compartilhado por todas as conexões do processo.
var gooseMu sync.Mutex

// runMigrations leva o banco ao schema final. No SQLite a base viva é adotada
// antes do goose: o app atual mantém dados de produção no mesmo arquivo, então
// nada é recriado e nenhuma linha é perdida.
func runMigrations(ctx context.Context, sqlDB *sql.DB, dialect Dialect) error {
	if dialect == DialectSQLite {
		if err := adoptSQLiteLegacy(ctx, sqlDB); err != nil {
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

// adoptSQLiteLegacy prepara uma base já existente para receber a migração
// consolidada:
//
//  1. acrescenta as colunas que faltam (tolerando coluna já existente);
//  2. recria `receipts` quando ela ainda vem do schema antigo (lease_id NOT
//     NULL), preservando as linhas — equivalente à migração Drizzle 0002;
//  3. se o schema já está completo, apenas registra a migração inicial como
//     aplicada, sem tocar em nada.
//
// Banco novo (sem tabelas) não passa por aqui: o goose cria o schema final.
func adoptSQLiteLegacy(ctx context.Context, sqlDB *sql.DB) error {
	existing, err := readSQLiteSchema(ctx, sqlDB)
	if err != nil {
		return err
	}
	if len(existing.columns) == 0 {
		return nil
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
			reference, err := readSQLiteSchemaFromMigration(ctx)
			if err != nil {
				return err
			}
			if err := rebuildReceipts(ctx, sqlDB, existing.columns["receipts"], reference.ddl["receipts"]); err != nil {
				return err
			}
		}
	}

	actual, err := readSQLiteSchema(ctx, sqlDB)
	if err != nil {
		return err
	}
	reference, err := readSQLiteSchemaFromMigration(ctx)
	if err != nil {
		return err
	}
	if reference.coveredBy(actual) {
		return markInitialVersionApplied(ctx, sqlDB)
	}
	return nil
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

// markInitialVersionApplied registra a migração inicial como aplicada sem
// executá-la: a base já tem o schema final e recriar tabelas seria destrutivo.
func markInitialVersionApplied(ctx context.Context, sqlDB *sql.DB) error {
	store, err := database.NewStore(database.DialectSQLite3, goose.TableName())
	if err != nil {
		return fmt.Errorf("abrir controle de versão do goose: %w", err)
	}

	exists, err := sqliteTableExists(ctx, sqlDB, goose.TableName())
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
	if current >= initialMigrationVersion {
		return nil
	}
	if err := store.Insert(ctx, sqlDB, database.InsertRequest{Version: initialMigrationVersion}); err != nil {
		return fmt.Errorf("registrar migração inicial: %w", err)
	}
	slog.Info("schema existente adotado", "scope", "db.migrate", "version", initialMigrationVersion)
	return nil
}

// ── Leitura de schema (SQLite) ─────────────────────────────────────

// sqliteSchema é a fotografia do schema de um banco SQLite: o DDL de cada
// tabela, suas colunas e os índices nomeados.
type sqliteSchema struct {
	ddl     map[string]string
	columns map[string]map[string]bool
	indexes map[string]bool
}

// readSQLiteSchema lê tabelas, colunas e índices, ignorando as tabelas internas
// do SQLite e as de controle de migração.
func readSQLiteSchema(ctx context.Context, sqlDB *sql.DB) (sqliteSchema, error) {
	schema := sqliteSchema{
		ddl:     map[string]string{},
		columns: map[string]map[string]bool{},
		indexes: map[string]bool{},
	}

	rows, err := sqlDB.QueryContext(ctx, "SELECT name, sql FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return schema, fmt.Errorf("listar tabelas do SQLite: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var ddl sql.NullString
		if err := rows.Scan(&name, &ddl); err != nil {
			return schema, fmt.Errorf("ler tabela do SQLite: %w", err)
		}
		if isControlTable(name) {
			continue
		}
		schema.ddl[name] = ddl.String
	}
	if err := rows.Err(); err != nil {
		return schema, fmt.Errorf("percorrer tabelas do SQLite: %w", err)
	}

	for name := range schema.ddl {
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

// sqliteTableExists informa se a tabela existe.
func sqliteTableExists(ctx context.Context, sqlDB *sql.DB, table string) (bool, error) {
	row := sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table)
	var total int
	if err := row.Scan(&total); err != nil {
		return false, fmt.Errorf("procurar a tabela %s: %w", table, err)
	}
	return total > 0, nil
}

// readSQLiteSchemaFromMigration monta o schema final aplicando o SQL embutido
// num banco descartável em memória: a expectativa nunca diverge da migração.
func readSQLiteSchemaFromMigration(ctx context.Context) (sqliteSchema, error) {
	scratch, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return sqliteSchema{}, fmt.Errorf("abrir banco de referência: %w", err)
	}
	defer func() { _ = scratch.Close() }()

	gooseMu.Lock()
	err = prepareGoose(DialectSQLite)
	if err == nil {
		err = goose.UpContext(ctx, scratch, ".")
	}
	gooseMu.Unlock()
	if err != nil {
		return sqliteSchema{}, fmt.Errorf("aplicar migração de referência: %w", err)
	}
	return readSQLiteSchema(ctx, scratch)
}

// coveredBy informa se `actual` já contém tudo o que o schema de referência
// define: todas as tabelas com todas as colunas e todos os índices nomeados.
func (s sqliteSchema) coveredBy(actual sqliteSchema) bool {
	for table, expected := range s.columns {
		columns, ok := actual.columns[table]
		if !ok {
			return false
		}
		for column := range expected {
			if !columns[column] {
				return false
			}
		}
	}
	for index := range s.indexes {
		if !actual.indexes[index] {
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
