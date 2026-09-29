package db_test

import (
	"sort"
	"strconv"
	"strings"

	"context"
	"database/sql"
	"github.com/passoz/aluxen/migrations"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db"
	"github.com/passoz/aluxen/internal/infrastructure/db/schema"
)

// As migrações do SQLite são adotivas: a base de produção já existe e não pode
// ser recriada. Estes testes cobrem os três estados possíveis — banco vazio,
// banco no schema antigo (pré-0002) e banco já no schema final — além da
// idempotência (rodar duas vezes não pode falhar nem duplicar nada).

func TestMigrateEmptyDatabase(t *testing.T) {
	ctx := context.Background()
	store, err := db.Open(ctx, "file:"+filepath.Join(t.TempDir(), "novo.db"), "")
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	assertNoSchemaDiff(t, ctx, store, "banco vazio")

	// os dados de referência funcionam depois das migrações
	require.NoError(t, store.Settings.Set(ctx, domain.SettingLandlordName, "Locador"))
	value, err := store.Settings.Get(ctx, domain.SettingLandlordName)
	require.NoError(t, err)
	assert.Equal(t, "Locador", value)
}

func TestMigrateLegacyDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legado.db")
	writeLegacyDatabase(t, path)

	store, err := db.Open(ctx, "file:"+path, "")
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	// nenhuma linha se perdeu
	receipts, err := store.Receipts.List(ctx)
	require.NoError(t, err)
	require.Len(t, receipts, 2)
	// a listagem vem do vencimento mais recente para o mais antigo
	assert.Equal(t, []string{"receipt-2", "receipt-1"}, []string{receipts[0].ID, receipts[1].ID})

	paid := receipts[1]
	assert.Equal(t, 2500.0, paid.Amount)
	assert.Equal(t, "2026-07-10", paid.DueDate)
	assert.Equal(t, "2026-07-09T14:30", domain.Str(paid.PaidAt))
	assert.Equal(t, "2026-07-01 10:00:00", paid.CreatedAt)
	assert.Equal(t, "lease-1:2026-07", domain.Str(paid.GeneratedKey))
	assert.Equal(t, "Carlos Drummond", paid.TenantName)
	assert.Nil(t, receipts[0].PaidAt)

	// a categoria das linhas antigas vira 'rent' (o default do app atual)
	assert.Equal(t, domain.ReceiptCategoryRent, paid.Category)
	assert.Equal(t, domain.ReceiptCategoryRent, receipts[0].Category)

	// `receipts.lease_id` deixou de ser obrigatório: lançamento avulso entra
	require.NoError(t, store.Receipts.Create(ctx, &domain.Receipt{
		ID: "receipt-avulso", TenantID: domain.StrPtr("tenant-1"), Category: domain.ReceiptCategoryExtra,
		DueDate: "2026-08-15", Amount: 120, CreatedAt: "2026-08-01 10:00:00",
	}))

	// colunas que faltavam existem e são utilizáveis
	require.NoError(t, store.Leases.UpdateStatus(ctx, "lease-1", domain.LeaseStatusSuspended, "2026-07-01 09:00:00"))
	lease, err := store.Leases.ByID(ctx, "lease-1")
	require.NoError(t, err)
	assert.Equal(t, domain.LeaseStatusSuspended, lease.Status)

	require.NoError(t, store.Tenants.UpdatePassword(ctx, "tenant-1", "hash", "2026-07-01 09:00:00", "2026-07-01 09:00:00"))
	tenant, err := store.Tenants.ByID(ctx, "tenant-1")
	require.NoError(t, err)
	require.NotNil(t, tenant.PasswordHash)

	user, err := store.Users.ByUsername(ctx, "passoz")
	require.NoError(t, err)
	assert.True(t, user.MustChangePassword())

	assert.Equal(t, 1, countTable(t, ctx, store, "leases"))
	assert.Equal(t, 1, countTable(t, ctx, store, "tenants"))
	assert.Equal(t, 1, countTable(t, ctx, store, "properties"))
	assert.Equal(t, 3, countTable(t, ctx, store, "receipts"))

	// o schema adotado tem tudo o que o canônico define (a ordem das colunas de
	// uma base viva difere, porque o ALTER TABLE acrescenta no fim)
	assertSchemaCovers(t, ctx, store, "base legada adotada")

	// a migração inicial foi registrada como aplicada, sem recriar tabelas
	assert.Contains(t, appliedVersions(t, ctx, store), int64(1))
}

func TestMigrateAdoptsFinalSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "final.db")
	writeCanonicalDatabase(t, path)

	store, err := db.Open(ctx, "file:"+path, "")
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	// nada foi recriado: os dados continuam lá e o schema é exatamente o canônico
	assert.Equal(t, 1, countTable(t, ctx, store, "tenants"))
	assert.Equal(t, 1, countTable(t, ctx, store, "leases"))
	assertNoSchemaDiff(t, ctx, store, "base já no schema final")

	// como a base já cobria todas as migrações, todas foram apenas registradas
	// (sem a linha 0, que só existe quando o goose cria a tabela de controle):
	// executar o SQL aqui falharia no ADD COLUMN da migração de temporada.
	// A expectativa é derivada das migrações embutidas para que acrescentar uma
	// migração nova não quebre este teste.
	assert.Equal(t, embeddedVersions(t), appliedVersions(t, ctx, store))
}

func TestMigrateTwiceIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "repetido.db")
	writeLegacyDatabase(t, path)

	store, err := db.Open(ctx, "file:"+path, "")
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	before := countTable(t, ctx, store, "receipts")
	require.NoError(t, store.Migrate(ctx))
	require.NoError(t, store.Migrate(ctx))

	assert.Equal(t, before, countTable(t, ctx, store, "receipts"))
	assert.Equal(t, 1, countTable(t, ctx, store, "tenants"))
	assertSchemaCovers(t, ctx, store, "migrações repetidas")
	// a linha 0 é a criação da tabela de controle; depois vêm todas as migrações
	assert.Equal(t, append([]int64{0}, embeddedVersions(t)...), appliedVersions(t, ctx, store))
}

// TestMigratePartialDatabase cobre a base que já reflete as primeiras migrações
// e perdeu a tabela de controle do goose (dump restaurado de uma versão
// anterior): o que falta roda, o que já estava lá é apenas registrado — sem
// reaplicar o ADD COLUMN da temporada.
func TestMigratePartialDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "parcial.db")
	writeCanonicalDatabase(t, path)

	execStatements(t, path, []string{
		"DROP TABLE IF EXISTS document_intakes",
		"DROP TABLE IF EXISTS payment_proofs",
		"DROP TABLE IF EXISTS jobs",
		"DROP TABLE IF EXISTS goose_db_version",
	})

	store, err := db.Open(ctx, "file:"+path, "")
	require.NoError(t, err, "a migração que falta precisa rodar sem reaplicar o que já estava lá")
	defer func() { require.NoError(t, store.Close()) }()

	for _, table := range []string{"jobs", "payment_proofs", "document_intakes", "inspections"} {
		assert.Contains(t, sqliteTables(t, ctx, store), table)
	}
	assert.Equal(t, embeddedVersions(t), appliedVersions(t, ctx, store))
	assert.Equal(t, 1, countTable(t, ctx, store, "tenants"), "os dados que já existiam continuam lá")
	assertNoSchemaDiff(t, ctx, store, "base parcial adotada")

	// a fila e os comprovantes voltam a funcionar na base adotada
	require.NoError(t, store.Jobs.Create(ctx, &domain.Job{
		ID: "job-1", CreatedAt: "2026-03-01 10:00:00", UpdatedAt: "2026-03-01 10:00:00",
		Kind: domain.JobKindPaymentProof, Status: domain.JobStatusQueued, MaxAttempts: 3,
		ScheduledAt: "2026-03-01 10:00:00",
	}))
}

// ── Adoção no Postgres ─────────────────────────────────────────────

// TestPostgresAdoption cobre os três estados de uma instância Postgres: schema
// final sem a tabela de controle do goose (o caso do pg_dump restaurado noutra
// base), base vazia e base parcial (dump de antes da última migração). Sem a
// adoção, o boot reaplicaria o ADD COLUMN e morreria.
func TestPostgresAdoption(t *testing.T) {
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL não definida: sem servidor Postgres para o teste de adoção")
	}
	ctx := context.Background()

	t.Run("base já no schema final sem tabela do goose", func(t *testing.T) {
		dsn := isolatedDSN(t, base)

		first, err := db.Open(ctx, dsn, "")
		require.NoError(t, err)
		require.NoError(t, first.Tenants.Create(ctx, &domain.Tenant{
			ID: "tenant-1", Name: "Carlos Drummond", Email: "carlos@exemplo.com",
			CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
		}))
		require.NoError(t, first.Close())

		// o dump trouxe o schema final, mas não a tabela de controle do goose
		execPostgres(t, dsn, `DROP TABLE goose_db_version`)

		second, err := db.Open(ctx, dsn, "")
		require.NoError(t, err, "o boot não pode reaplicar o ADD COLUMN numa base já final")
		defer func() { require.NoError(t, second.Close()) }()

		// nenhuma migração rodou: todas foram apenas registradas (sem a linha 0,
		// que só existe quando o goose cria a tabela de controle)
		assert.Equal(t, postgresVersions(t), appliedVersions(t, ctx, second))
		count, err := second.Tenants.Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "os dados do dump continuam lá")
		assert.Equal(t, 1, countTable(t, ctx, second, "tenants"))
	})

	t.Run("base vazia aplica todas as migrações", func(t *testing.T) {
		dsn := isolatedDSN(t, base)

		store, err := db.Open(ctx, dsn, "")
		require.NoError(t, err)
		defer func() { require.NoError(t, store.Close()) }()

		for _, table := range []string{
			"users", "tenants", "properties", "leases", "receipts", "settings", "audit_logs",
			"whatsapp_messages", "inspections", "jobs", "payment_proofs", "document_intakes",
		} {
			assert.Contains(t, postgresTables(t, dsn), table)
		}
		versions := appliedVersions(t, ctx, store)
		for _, version := range postgresVersions(t) {
			assert.Contains(t, versions, version)
		}
		// as portas respondem: a base vazia é utilizável
		count, err := store.Leases.Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("base parcial aplica só o que falta", func(t *testing.T) {
		dsn := isolatedDSN(t, base)

		first, err := db.Open(ctx, dsn, "")
		require.NoError(t, err)
		require.NoError(t, first.Tenants.Create(ctx, &domain.Tenant{
			ID: "tenant-1", Name: "Carlos Drummond", Email: "carlos@exemplo.com",
			CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
		}))
		require.NoError(t, first.Close())

		// dump de uma instância anterior à temporada: sem as tabelas da IA, sem a
		// vistoria, sem as colunas de caução e sem a tabela de controle
		execPostgres(t, dsn,
			`DROP TABLE IF EXISTS document_intakes`,
			`DROP TABLE IF EXISTS payment_proofs`,
			`DROP TABLE IF EXISTS jobs`,
			`DROP TABLE IF EXISTS inspections`,
			`ALTER TABLE leases DROP COLUMN IF EXISTS deposit_amount`,
			`ALTER TABLE leases DROP COLUMN IF EXISTS deposit_status`,
			`ALTER TABLE leases DROP COLUMN IF EXISTS deposit_received_at`,
			`ALTER TABLE leases DROP COLUMN IF EXISTS deposit_refunded_at`,
			`ALTER TABLE leases DROP COLUMN IF EXISTS deposit_refunded_amount`,
			`ALTER TABLE leases DROP COLUMN IF EXISTS deposit_notes`,
			`DROP TABLE goose_db_version`,
		)

		store, err := db.Open(ctx, dsn, "")
		require.NoError(t, err, "a migração que falta precisa rodar sem reaplicar o que já estava lá")
		defer func() { require.NoError(t, store.Close()) }()

		tables := postgresTables(t, dsn)
		for _, table := range []string{"jobs", "payment_proofs", "document_intakes", "inspections"} {
			assert.Contains(t, tables, table)
		}
		assert.Equal(t, postgresVersions(t), appliedVersions(t, ctx, store))

		count, err := store.Tenants.Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "o locatário que já existia continua lá")

		// a caução volta a funcionar na base adotada
		require.NoError(t, store.Properties.Create(ctx, &domain.Property{
			ID: "property-1", Name: "Casa de Búzios", Address: "Rua das Conchas, 45",
			City: "Búzios", State: "RJ", CreatedAt: "2026-12-01 10:00:00", UpdatedAt: "2026-12-01 10:00:00",
		}))
		require.NoError(t, store.Leases.Create(ctx, &domain.Lease{
			ID: "lease-1", TenantID: "tenant-1", PropertyID: "property-1",
			StartDate: "2026-12-20", EndDate: "2027-01-05", DueDay: 20, RentAmount: 4800,
			Status: domain.LeaseStatusSeason, DepositAmount: domain.FloatPtr(4800),
			DepositStatus: domain.DepositStatusHeld, CreatedAt: "2026-12-01 10:00:00", UpdatedAt: "2026-12-01 10:00:00",
		}))
	})

	t.Run("segunda abertura não faz nada", func(t *testing.T) {
		dsn := isolatedDSN(t, base)

		first, err := db.Open(ctx, dsn, "")
		require.NoError(t, err)
		require.NoError(t, first.Close())

		second, err := db.Open(ctx, dsn, "")
		require.NoError(t, err)
		defer func() { require.NoError(t, second.Close()) }()
		require.NoError(t, second.Migrate(ctx))

		// aqui o goose criou a tabela de controle (linha 0) e aplicou tudo
		assert.Equal(t, append([]int64{0}, postgresVersions(t)...), appliedVersions(t, ctx, second))
	})
}

// sqliteTables lista as tabelas de um banco SQLite aberto.
func sqliteTables(t *testing.T, ctx context.Context, store *db.Store) []string {
	t.Helper()
	rows, err := store.DB().QueryContext(ctx,
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())
	return tables
}

// execPostgres roda instruções diretas no banco de teste (preparação de estado).
func execPostgres(t *testing.T, dsn string, statements ...string) {
	t.Helper()
	handle, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, handle.Close()) }()

	for _, statement := range statements {
		_, err := handle.ExecContext(context.Background(), statement)
		require.NoError(t, err, "executar: %s", statement)
	}
}

// postgresTables lista as tabelas do schema corrente.
func postgresTables(t *testing.T, dsn string) []string {
	t.Helper()
	handle, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, handle.Close()) }()

	rows, err := handle.QueryContext(context.Background(),
		`SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() ORDER BY table_name`)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())
	return tables
}

// postgresVersions são as migrações embutidas do dialeto Postgres.
func postgresVersions(t *testing.T) []int64 {
	t.Helper()
	entries, err := migrations.FS.ReadDir("postgres")
	require.NoError(t, err)

	versions := make([]int64, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		version, err := strconv.ParseInt(strings.SplitN(name, "_", 2)[0], 10, 64)
		require.NoError(t, err)
		versions = append(versions, version)
	}
	sort.Slice(versions, func(a, b int) bool { return versions[a] < versions[b] })
	return versions
}

// ── Apoio ──────────────────────────────────────────────────────────

// assertNoSchemaDiff compara o schema do banco aberto com o DDL canônico.
func assertNoSchemaDiff(t *testing.T, ctx context.Context, store *db.Store, label string) {
	t.Helper()
	actual, err := schema.LoadSQLite(ctx, store.DB(), map[string]bool{"goose_db_version": true})
	require.NoError(t, err)
	expected := canonicalSchema(t)

	diffs := schema.Diff(expected, actual, "schema/sqlite.sql", label)
	if len(diffs) > 0 {
		t.Fatalf("schema de %s difere do canônico:\n  %s", label, joinLines(diffs))
	}
}

// assertSchemaCovers confere que o banco tem todas as tabelas, colunas e
// índices do canônico, sem exigir a mesma ordem de colunas (uma base adotada
// ganha as colunas novas no fim da tabela).
func assertSchemaCovers(t *testing.T, ctx context.Context, store *db.Store, label string) {
	t.Helper()
	actual, err := schema.LoadSQLite(ctx, store.DB(), map[string]bool{"goose_db_version": true})
	require.NoError(t, err)
	expected := canonicalSchema(t)

	actualTables := map[string]schema.Table{}
	for _, table := range actual.Tables {
		actualTables[table.Name] = table
	}
	for _, table := range expected.Tables {
		got, ok := actualTables[table.Name]
		if !assert.True(t, ok, "%s: falta a tabela %q", label, table.Name) {
			continue
		}
		columns := map[string]schema.Column{}
		for _, column := range got.Columns {
			columns[column.Name] = column
		}
		for _, column := range table.Columns {
			other, ok := columns[column.Name]
			if !assert.True(t, ok, "%s: tabela %q sem a coluna %q", label, table.Name, column.Name) {
				continue
			}
			assert.Equal(t, column.Type, other.Type, "%s: tipo de %s.%s", label, table.Name, column.Name)
			assert.Equal(t, column.NotNull, other.NotNull, "%s: nulidade de %s.%s", label, table.Name, column.Name)
		}
		for _, fk := range table.ForeignKeys {
			found := false
			for _, other := range got.ForeignKeys {
				if other.Column == fk.Column && other.Table == fk.Table && other.OnDelete == fk.OnDelete {
					found = true
				}
			}
			assert.True(t, found, "%s: tabela %q sem a chave estrangeira de %q", label, table.Name, fk.Column)
		}
	}

	actualIndexes := map[string]schema.Index{}
	for _, index := range actual.Indexes {
		actualIndexes[index.Name] = index
	}
	for _, index := range expected.Indexes {
		got, ok := actualIndexes[index.Name]
		if !assert.True(t, ok, "%s: falta o índice %q", label, index.Name) {
			continue
		}
		assert.Equal(t, index.Unique, got.Unique, "%s: unicidade do índice %q", label, index.Name)
		assert.Equal(t, index.Table, got.Table, "%s: tabela do índice %q", label, index.Name)
	}
}

func canonicalSchema(t *testing.T) schema.Schema {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(repoRoot(t), "schema", "sqlite.sql"))
	require.NoError(t, err)
	schema, err := schema.ParseSQL(string(content), schema.SQLite)
	require.NoError(t, err)
	return schema
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "go.mod não encontrado")
		dir = parent
	}
}

// embeddedVersions devolve as versões das migrações embutidas, na ordem.
func embeddedVersions(t *testing.T) []int64 {
	t.Helper()
	entries, err := migrations.FS.ReadDir("sqlite")
	require.NoError(t, err)

	versions := make([]int64, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		version, err := strconv.ParseInt(strings.SplitN(name, "_", 2)[0], 10, 64)
		require.NoError(t, err)
		versions = append(versions, version)
	}
	sort.Slice(versions, func(a, b int) bool { return versions[a] < versions[b] })
	return versions
}

// appliedVersions lê as migrações registradas pelo goose.
func appliedVersions(t *testing.T, ctx context.Context, store *db.Store) []int64 {
	t.Helper()
	rows, err := store.DB().QueryContext(ctx, "SELECT version_id FROM goose_db_version ORDER BY version_id")
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	var versions []int64
	for rows.Next() {
		var version int64
		require.NoError(t, rows.Scan(&version))
		versions = append(versions, version)
	}
	require.NoError(t, rows.Err())
	return versions
}

func countTable(t *testing.T, ctx context.Context, store *db.Store, table string) int {
	t.Helper()
	var total int
	row := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table)
	require.NoError(t, row.Scan(&total))
	return total
}

func joinLines(lines []string) string {
	joined := ""
	for i, line := range lines {
		if i > 0 {
			joined += "\n  "
		}
		joined += line
	}
	return joined
}

// writeLegacyDatabase cria um banco no schema anterior à migração 0002:
// `receipts.lease_id` obrigatório, sem categoria, e sem as colunas que o app
// ganhou depois (auth_token, credenciais do locatário, troca de senha do admin).
func writeLegacyDatabase(t *testing.T, path string) {
	t.Helper()
	statements := []string{
		`CREATE TABLE users (
			id text PRIMARY KEY NOT NULL, username text NOT NULL,
			password_hash text NOT NULL, created_at text NOT NULL)`,
		`CREATE UNIQUE INDEX users_username_unique ON users (username)`,
		`CREATE TABLE tenants (
			id text PRIMARY KEY NOT NULL, name text NOT NULL, email text NOT NULL,
			phone text, cpf text, created_at text NOT NULL, updated_at text NOT NULL)`,
		`CREATE TABLE properties (
			id text PRIMARY KEY NOT NULL, name text NOT NULL, address text NOT NULL,
			city text NOT NULL, state text NOT NULL, zip text,
			created_at text NOT NULL, updated_at text NOT NULL)`,
		`CREATE TABLE leases (
			id text PRIMARY KEY NOT NULL, tenant_id text NOT NULL, property_id text NOT NULL,
			start_date text NOT NULL, end_date text NOT NULL, due_day integer DEFAULT 1 NOT NULL,
			rent_amount real NOT NULL, status text DEFAULT 'active' NOT NULL,
			created_at text NOT NULL, updated_at text NOT NULL,
			FOREIGN KEY (tenant_id) REFERENCES tenants (id) ON UPDATE no action ON DELETE cascade,
			FOREIGN KEY (property_id) REFERENCES properties (id) ON UPDATE no action ON DELETE cascade)`,
		`CREATE TABLE receipts (
			id text PRIMARY KEY NOT NULL, lease_id text NOT NULL, due_date text NOT NULL,
			paid_at text, amount real NOT NULL, pdf_path text, generated_key text,
			created_at text NOT NULL, updated_at text,
			FOREIGN KEY (lease_id) REFERENCES leases (id) ON UPDATE no action ON DELETE cascade)`,
		`CREATE TABLE settings (key text PRIMARY KEY NOT NULL, value text NOT NULL)`,
		`INSERT INTO users (id, username, password_hash, created_at)
			VALUES ('user-1', 'passoz', 'hash-antigo', '2026-01-01 10:00:00')`,
		`INSERT INTO tenants (id, name, email, phone, cpf, created_at, updated_at)
			VALUES ('tenant-1', 'Carlos Drummond', 'carlos@exemplo.com', '(22) 99888-1122', '111.222.333-44',
			        '2026-01-01 10:00:00', '2026-01-01 10:00:00')`,
		`INSERT INTO properties (id, name, address, city, state, zip, created_at, updated_at)
			VALUES ('property-1', 'Apto 101', 'Av. do Contorno, 250', 'Cabo Frio', 'RJ', '28907-250',
			        '2026-01-01 10:00:00', '2026-01-01 10:00:00')`,
		`INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, status,
		                     created_at, updated_at)
			VALUES ('lease-1', 'tenant-1', 'property-1', '2026-01-01', '2026-12-31', 10, 2500, 'active',
			        '2026-01-01 10:00:00', '2026-01-01 10:00:00')`,
		`INSERT INTO receipts (id, lease_id, due_date, paid_at, amount, generated_key, created_at)
			VALUES ('receipt-1', 'lease-1', '2026-07-10', '2026-07-09T14:30', 2500, 'lease-1:2026-07',
			        '2026-07-01 10:00:00')`,
		`INSERT INTO receipts (id, lease_id, due_date, amount, generated_key, created_at)
			VALUES ('receipt-2', 'lease-1', '2026-08-10', 2500, 'lease-1:2026-08', '2026-08-01 10:00:00')`,
	}
	execStatements(t, path, statements)
}

// writeCanonicalDatabase cria um banco já no schema final, como uma instalação
// que passou por todas as migrações antes do port.
func writeCanonicalDatabase(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(repoRoot(t), "schema", "sqlite.sql"))
	require.NoError(t, err)

	statements := []string{string(content)}
	statements = append(statements,
		`INSERT INTO tenants (id, name, email, created_at, updated_at)
			VALUES ('tenant-1', 'Carlos Drummond', 'carlos@exemplo.com', '2026-01-01 10:00:00', '2026-01-01 10:00:00')`,
		`INSERT INTO properties (id, name, address, city, state, created_at, updated_at)
			VALUES ('property-1', 'Apto 101', 'Av. do Contorno, 250', 'Cabo Frio', 'RJ', '2026-01-01 10:00:00', '2026-01-01 10:00:00')`,
		`INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, status,
		                     created_at, updated_at)
			VALUES ('lease-1', 'tenant-1', 'property-1', '2026-01-01', '2026-12-31', 10, 2500, 'active',
			        '2026-01-01 10:00:00', '2026-01-01 10:00:00')`,
	)
	execStatements(t, path, statements)
}

func execStatements(t *testing.T, path string, statements []string) {
	t.Helper()
	handle, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)
	defer func() { require.NoError(t, handle.Close()) }()

	for _, statement := range statements {
		_, err := handle.ExecContext(context.Background(), statement)
		require.NoError(t, err, "executar: %.60s", statement)
	}
}
