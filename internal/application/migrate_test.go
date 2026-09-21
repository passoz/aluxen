package application_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/infrastructure/db"
)

// ── Apoio ──────────────────────────────────────────────────────────
//
// A migração é exercitada de ponta a ponta sobre o SQLite real: a origem é um
// arquivo temporário migrado pelo próprio produto, e o destino é um banco
// migrado e vazio. Sem TEST_DATABASE_URL o destino é SQLite em memória (mesmo
// esquema, marcador de parâmetro diferente): a cópia, a verificação e o
// rollback são provados sem rede. Com TEST_DATABASE_URL apontando para um
// Postgres vazio, o mesmo teste roda contra o dialeto de produção.

// migratedTables é a ordem de cópia esperada (dependências primeiro).
var migratedTables = []string{
	"users", "properties", "tenants", "leases", "receipts", "settings", "audit_logs",
	"inspections", "whatsapp_messages", "jobs", "payment_proofs", "document_intakes",
}

// seedRows é uma linha por tabela, cobrindo todos os tipos (texto, inteiro,
// real, nulo) e as duas tabelas novas do tier assistido.
var seedRows = []string{
	`INSERT INTO users (id, username, password_hash, password_changed_at, created_at)
	 VALUES ('u1','admin','hash',NULL,'2026-01-01 10:00:00')`,
	`INSERT INTO properties (id, name, address, city, state, zip, created_at, updated_at)
	 VALUES ('p1','Apto 101','Rua A, 1','Cabo Frio','RJ','28900-000','2026-01-01 10:00:00','2026-01-01 10:00:00')`,
	`INSERT INTO tenants (id, name, email, phone, cpf, password_hash, password_changed_at, created_at, updated_at)
	 VALUES ('t1','Carlos','carlos@exemplo.com.br','(22) 99999-0000','111.222.333-44','hash',NULL,'2026-01-01 10:00:00','2026-01-01 10:00:00')`,
	`INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, status, auth_token,
	 created_at, updated_at, deposit_amount, deposit_status, deposit_received_at, deposit_refunded_at,
	 deposit_refunded_amount, deposit_notes)
	 VALUES ('l1','t1','p1','2026-01-01','2026-12-31',10,2500.5,'active','tok','2026-01-01 10:00:00','2026-01-01 10:00:00',
	 2500.5,'held','2026-01-02',NULL,NULL,NULL)`,
	`INSERT INTO receipts (id, lease_id, tenant_id, property_id, category, description, due_date, paid_at, amount,
	 paid_amount, pdf_path, generated_key, auth_token, created_at, updated_at)
	 VALUES ('r1','l1','t1','p1','rent','Aluguel','2026-07-10','2026-07-09T14:30',2500.5,2500.5,NULL,'l1:2026-07','tok2',
	 '2026-07-01 10:00:00',NULL)`,
	`INSERT INTO settings (key, value) VALUES ('landlord_name','Locador Demonstração')`,
	`INSERT INTO audit_logs (id, created_at, actor_id, actor_name, action, entity, entity_id, entity_label, summary,
	 changes, metadata, origin, ip, user_agent, request_id)
	 VALUES ('a1','2026-01-01 10:00:00','u1','Admin','payment.received','payment','r1','Aluguel','Baixa registrada',
	 NULL,NULL,'web','127.0.0.1','teste','req-1')`,
	`INSERT INTO inspections (id, lease_id, kind, performed_at, condition, notes, items, photos, auth_token,
	 created_at, updated_at)
	 VALUES ('i1','l1','move-in','2026-01-05','bom',NULL,NULL,NULL,'tok3','2026-01-05 10:00:00','2026-01-05 10:00:00')`,
	`INSERT INTO whatsapp_messages (id, created_at, direction, chat_id, phone, tenant_id, body, status, external_id, metadata)
	 VALUES ('w1','2026-01-06 10:00:00','in','5522999990000@c.us','5522999990000','t1','bom dia','received','ext-1',NULL)`,
	`INSERT INTO jobs (id, created_at, updated_at, kind, status, attempts, max_attempts, payload, result, error,
	 scheduled_at, started_at, finished_at)
	 VALUES ('j1','2026-01-07 10:00:00','2026-01-07 10:00:00','whatsapp-send','queued',0,3,NULL,NULL,NULL,
	 '2026-01-07 10:00:00',NULL,NULL)`,
	`INSERT INTO payment_proofs (id, created_at, updated_at, source, phone, tenant_id, receipt_id, file_name, file_path,
	 mime, extracted_amount, extracted_date, extracted_payer, extracted_payee, extracted_identifier, confidence,
	 status, decision, decision_reason, decided_by, decided_at, raw_response)
	 VALUES ('pp1','2026-01-08 10:00:00','2026-01-08 10:00:00','whatsapp','5522999990000','t1','r1','recibo.jpg',
	 '/data/recibo.jpg','image/jpeg',2500.5,'2026-07-09','Carlos','Locador','E2E123',0.92,'accepted','auto',NULL,NULL,NULL,
	 '{"amount":2500.5}')`,
	`INSERT INTO document_intakes (id, created_at, updated_at, kind, file_name, file_path, mime, status, extracted,
	 confidence, created_entity, created_entity_id, raw_response, notes)
	 VALUES ('d1','2026-01-09 10:00:00','2026-01-09 10:00:00','lease','contrato.pdf','/data/contrato.pdf','application/pdf',
	 'pending','{"tenantName":"Carlos"}',0.8,NULL,NULL,'{}',NULL)`,
}

// openStore abre um banco migrado no caminho informado.
func openStore(t *testing.T, dsn string) *db.Store {
	t.Helper()
	store, err := db.Open(context.Background(), dsn, "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

// testDestination devolve o destino da migração: o Postgres de TEST_DATABASE_URL
// quando existir (precisa estar vazio), senão um SQLite em memória migrado.
func testDestination(t *testing.T) (*db.Store, application.Dialect) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		store := openStore(t, ":memory:")
		return store, application.DialectSQLite
	}
	store := openStore(t, dsn)
	return store, application.DialectPostgres
}

// writeSource cria a base SQLite de origem, migrada e com as linhas semeadas.
func writeSource(t *testing.T, rows []string) string {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "aluxen.db")

	store, err := db.Open(ctx, path, "")
	require.NoError(t, err)
	for _, statement := range rows {
		_, err := store.DB().ExecContext(ctx, statement)
		require.NoError(t, err, statement)
	}
	require.NoError(t, store.Close())
	return path
}

// countRows conta as linhas de uma tabela numa conexão já aberta.
func countRows(t *testing.T, store *db.Store, table string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, store.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM "`+table+`"`).Scan(&count))
	return count
}

// countRowsInFile reabre um arquivo SQLite e conta as linhas da tabela.
func countRowsInFile(t *testing.T, path, table string) int64 {
	t.Helper()
	return countRows(t, openStore(t, path), table)
}

// migrateOptions monta as opções padrão do teste.
func migrateOptions(sourcePath string, store *db.Store, dialect application.Dialect) application.MigrateOptions {
	return application.MigrateOptions{
		Source:             sourcePath,
		Destination:        store.DB(),
		DestinationDialect: dialect,
	}
}

// ── Cópia ──────────────────────────────────────────────────────────

func TestMigrateCopiesEveryTableAndVerifiesChecksum(t *testing.T) {
	sourcePath := writeSource(t, seedRows)
	destination, dialect := testDestination(t)

	report, err := application.MigrateSQLiteToPostgres(context.Background(), migrateOptions(sourcePath, destination, dialect))
	require.NoError(t, err)

	assert.False(t, report.DryRun)
	assert.Equal(t, sourcePath, report.Source)
	assert.Equal(t, int64(len(seedRows)), report.Rows())
	require.Len(t, report.Tables, len(migratedTables))
	for index, table := range report.Tables {
		assert.Equal(t, migratedTables[index], table.Table, "a ordem de cópia precisa respeitar as dependências")
		assert.True(t, table.Verified(), "tabela %s precisa conferir contagem e checksum", table.Table)
		assert.Equal(t, int64(1), table.SourceRows)
		assert.Equal(t, int64(1), table.Copied)
		assert.NotEmpty(t, table.SourceChecksum)
		assert.Equal(t, table.SourceChecksum, table.DestinationChecksum)
	}

	// Os valores chegaram: texto, real com casa decimal e o JSON da leitura por IA.
	assert.Equal(t, int64(1), countRows(t, destination, "tenants"))
	assert.Equal(t, int64(1), countRows(t, destination, "payment_proofs"))
	assert.Equal(t, int64(1), countRows(t, destination, "document_intakes"))

	var amount float64
	var payer, raw string
	require.NoError(t, destination.DB().QueryRowContext(context.Background(),
		`SELECT extracted_amount, extracted_payer, raw_response FROM payment_proofs WHERE id = 'pp1'`).
		Scan(&amount, &payer, &raw))
	assert.Equal(t, 2500.5, amount)
	assert.Equal(t, "Carlos", payer)
	assert.Equal(t, `{"amount":2500.5}`, raw)

	// A origem continua intacta e no lugar.
	assert.FileExists(t, sourcePath)
	assert.Equal(t, int64(1), countRowsInFile(t, sourcePath, "receipts"))
	assert.Equal(t, int64(1), countRowsInFile(t, sourcePath, "jobs"))
}

func TestMigrateRefusesSecondRunOnFilledDestination(t *testing.T) {
	sourcePath := writeSource(t, seedRows)
	destination, dialect := testDestination(t)

	_, err := application.MigrateSQLiteToPostgres(context.Background(), migrateOptions(sourcePath, destination, dialect))
	require.NoError(t, err)

	_, err = application.MigrateSQLiteToPostgres(context.Background(), migrateOptions(sourcePath, destination, dialect))
	require.ErrorIs(t, err, application.ErrMigrateDestinationNotEmpty)
	assert.Equal(t, int64(1), countRows(t, destination, "tenants"), "a cópia recusada não mexe no que já estava lá")
}

// ── Dry-run ────────────────────────────────────────────────────────

func TestDryRunCountsWithoutWriting(t *testing.T) {
	sourcePath := writeSource(t, seedRows)
	destination, dialect := testDestination(t)

	options := migrateOptions(sourcePath, destination, dialect)
	options.DryRun = true
	report, err := application.MigrateSQLiteToPostgres(context.Background(), options)
	require.NoError(t, err)

	assert.True(t, report.DryRun)
	assert.Equal(t, int64(len(seedRows)), report.Rows())
	for _, table := range report.Tables {
		assert.Equal(t, int64(1), table.SourceRows, table.Table)
		assert.Equal(t, int64(0), table.Copied, table.Table)
		assert.Equal(t, int64(0), table.DestinationRows, table.Table)
	}
	for _, table := range migratedTables {
		assert.Equal(t, int64(0), countRows(t, destination, table), "o dry-run não pode escrever em %s", table)
	}
	assert.Contains(t, report.String(), "dry-run")
}

// ── Pré-checagem ───────────────────────────────────────────────────

func TestMissingSourceIsRefused(t *testing.T) {
	destination, dialect := testDestination(t)

	_, err := application.MigrateSQLiteToPostgres(context.Background(),
		migrateOptions(filepath.Join(t.TempDir(), "nao-existe.db"), destination, dialect))
	assert.ErrorIs(t, err, application.ErrMigrateSource)
}

func TestDestinationWithoutSchemaIsRefused(t *testing.T) {
	sourcePath := writeSource(t, seedRows)
	raw, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "vazio.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, raw.Close()) })

	_, err = application.MigrateSQLiteToPostgres(context.Background(), application.MigrateOptions{
		Source: sourcePath, Destination: raw, DestinationDialect: application.DialectSQLite,
	})
	assert.ErrorIs(t, err, application.ErrMigrateDestinationNotMigrated)
}

func TestDestinationWithoutConnectionIsRefused(t *testing.T) {
	sourcePath := writeSource(t, seedRows)

	_, err := application.MigrateSQLiteToPostgres(context.Background(), application.MigrateOptions{Source: sourcePath})
	assert.ErrorIs(t, err, application.ErrMigrateDestination)
}

func TestMissingSourcePathIsRefused(t *testing.T) {
	destination, dialect := testDestination(t)

	_, err := application.MigrateSQLiteToPostgres(context.Background(),
		migrateOptions("   ", destination, dialect))
	assert.ErrorIs(t, err, application.ErrMigrateSource)
}

// ── Rollback ───────────────────────────────────────────────────────

func TestFailureMidCopyRollsBackDestination(t *testing.T) {
	// A origem tem dois jobs do mesmo tipo; o destino ganha um índice único só
	// dele, então a segunda linha é recusada no meio da cópia (a tabela `jobs` é
	// a décima de doze).
	duplicate := `INSERT INTO jobs (id, created_at, updated_at, kind, status, attempts, max_attempts, payload, result,
	 error, scheduled_at, started_at, finished_at)
	 VALUES ('j2','2026-01-07 11:00:00','2026-01-07 11:00:00','whatsapp-send','queued',0,3,NULL,NULL,NULL,
	 '2026-01-07 11:00:00',NULL,NULL)`
	sourcePath := writeSource(t, append(append([]string{}, seedRows...), duplicate))
	destination, dialect := testDestination(t)
	_, err := destination.DB().ExecContext(context.Background(),
		`CREATE UNIQUE INDEX destino_somente_jobs_kind ON jobs (kind)`)
	require.NoError(t, err)

	report, err := application.MigrateSQLiteToPostgres(context.Background(), migrateOptions(sourcePath, destination, dialect))
	require.Error(t, err)
	assert.Equal(t, "jobs", report.FailedTable)
	assert.Contains(t, report.String(), "falhou em: jobs")

	// A transação foi revertida: nenhuma tabela ficou com dado parcial.
	for _, table := range migratedTables {
		assert.Equal(t, int64(0), countRows(t, destination, table), "a cópia de %s precisa ser revertida", table)
	}

	// A origem segue intacta.
	assert.Equal(t, int64(2), countRowsInFile(t, sourcePath, "jobs"))
	assert.Equal(t, int64(1), countRowsInFile(t, sourcePath, "payment_proofs"))
}

func TestReportListsFailuresInPortuguese(t *testing.T) {
	sourcePath := writeSource(t, seedRows)
	destination, dialect := testDestination(t)

	report, err := application.MigrateSQLiteToPostgres(context.Background(), migrateOptions(sourcePath, destination, dialect))
	require.NoError(t, err)

	text := report.String()
	assert.Contains(t, text, "migração SQLite →")
	assert.Contains(t, text, "cópia em transação")
	assert.True(t, strings.Contains(text, "confere"), "o relatório precisa dizer que o checksum confere")
}
