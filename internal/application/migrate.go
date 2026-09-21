package application

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	_ "modernc.org/sqlite" // driver "sqlite" da origem
)

// ── Migração de dialeto (SQLite → Postgres) ────────────────────────
//
// Este arquivo é a ferramenta de operação que troca uma instância de SQLite para
// Postgres (ARQUITETURA-DEPLOY.md §10): pré-checagem, dry-run, cópia em
// transação, verificação por contagem e checksum, rollback e a origem
// preservada. É o único ponto do pacote que fala SQL — e não é acesso a dado de
// negócio: as tabelas e colunas vêm de uma especificação fixa
// (`migrationTables`), os valores sempre viajam por marcadores e nenhum nome de
// tabela ou coluna vem de entrada do usuário.
//
// A garantia central: nada é escrito no destino fora de uma transação, e a
// transação só é confirmada depois de a contagem e o checksum de cada tabela
// baterem com a origem.

// Dialect identifica o dialeto de um extremo da migração.
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
)

// Erros sentinela da migração. O operador distingue "origem ruim" de "destino
// sujo" sem ler a mensagem.
var (
	// ErrMigrateSource indica origem ausente, ilegível ou fora do schema.
	ErrMigrateSource = errors.New("migração: origem SQLite ilegível")
	// ErrMigrateDestination indica destino ausente ou sem conexão utilizável.
	ErrMigrateDestination = errors.New("migração: destino indisponível")
	// ErrMigrateDestinationNotMigrated indica destino sem o schema final.
	ErrMigrateDestinationNotMigrated = errors.New("migração: destino não está migrado")
	// ErrMigrateDestinationNotEmpty indica destino com dados: copiar por cima
	// duplicaria linha, então a migração recusa.
	ErrMigrateDestinationNotEmpty = errors.New("migração: destino já tem dados")
	// ErrMigrateVerification indica divergência entre origem e destino depois
	// da cópia (a transação é revertida).
	ErrMigrateVerification = errors.New("migração: verificação falhou")
)

// MigrateOptions são os parâmetros da migração.
type MigrateOptions struct {
	// Source é o arquivo SQLite de origem. Aceita o prefixo `file:` usado na
	// configuração da instância.
	Source string
	// Destination é a conexão do destino, já aberta e migrada. O destino é
	// injetado (e não aberto aqui) para que quem chama decida o dialeto e o
	// ciclo de vida da conexão.
	Destination *sql.DB
	// DestinationDialect é o dialeto da conexão do destino. Define o marcador de
	// parâmetro da cópia (`?` no SQLite, `$n` no Postgres).
	DestinationDialect Dialect
	// DryRun conta as linhas por tabela sem escrever nada no destino.
	DryRun bool
}

// MigrateTableReport é o resultado de uma tabela.
type MigrateTableReport struct {
	Table               string
	SourceRows          int64
	DestinationRows     int64
	SourceChecksum      string
	DestinationChecksum string
	Copied              int64
}

// Verified informa se a tabela bateu com a origem.
func (t MigrateTableReport) Verified() bool {
	return t.DestinationRows == t.SourceRows && t.DestinationChecksum == t.SourceChecksum
}

// MigrateReport é o relatório da migração, com o ponto de falha quando houver.
type MigrateReport struct {
	Source      string
	Dialect     Dialect
	DryRun      bool
	Tables      []MigrateTableReport
	FailedTable string
	Duration    time.Duration
}

// Rows devolve o total de linhas lidas da origem.
func (r MigrateReport) Rows() int64 {
	var total int64
	for _, table := range r.Tables {
		total += table.SourceRows
	}
	return total
}

// String descreve a migração para o operador.
func (r MigrateReport) String() string {
	var builder strings.Builder
	mode := "cópia em transação"
	if r.DryRun {
		mode = "dry-run (nada foi escrito)"
	}
	dialect := string(r.Dialect)
	if dialect == "" {
		dialect = "destino"
	}
	fmt.Fprintf(&builder, "migração SQLite → %s\n", dialect)
	fmt.Fprintf(&builder, "origem: %s\n", r.Source)
	fmt.Fprintf(&builder, "modo:   %s\n\n", mode)

	writer := tabwriter.NewWriter(&builder, 0, 0, 2, ' ', 0)
	if len(r.Tables) > 0 {
		fmt.Fprintln(writer, "TABELA\tORIGEM\tDESTINO\tCHECKSUNK")
	}
	for _, table := range r.Tables {
		destination, checksum := "-", "-"
		if !r.DryRun {
			destination = strconv.FormatInt(table.DestinationRows, 10)
			checksum = "confere"
			if !table.Verified() {
				checksum = "DIVERGE"
			}
		}
		fmt.Fprintf(writer, "%s\t%d\t%s\t%s\n", table.Table, table.SourceRows, destination, checksum)
	}
	_ = writer.Flush()

	fmt.Fprintf(&builder, "\ntotal: %d linha(s) em %d tabela(s)\n", r.Rows(), len(r.Tables))
	if r.FailedTable != "" {
		fmt.Fprintf(&builder, "falhou em: %s (destino revertido)\n", r.FailedTable)
	}
	return builder.String()
}

// MigrateSQLiteToPostgres copia a base SQLite para o destino já migrado.
//
// O destino precisa estar vazio: a cópia não tem upsert e uma linha repetida
// seria erro silencioso de duplicação. A origem é aberta em modo somente
// consulta e nunca é apagada — o volume fica como backup.
func MigrateSQLiteToPostgres(ctx context.Context, options MigrateOptions) (MigrateReport, error) {
	started := time.Now()
	report := MigrateReport{Dialect: options.DestinationDialect, DryRun: options.DryRun}

	sourcePath, err := resolveSourcePath(options.Source)
	if err != nil {
		return report, err
	}
	report.Source = sourcePath

	if options.Destination == nil {
		return report, fmt.Errorf("%w: conexão do destino não informada", ErrMigrateDestination)
	}
	if options.DestinationDialect == "" {
		return report, fmt.Errorf("%w: dialeto do destino não informado", ErrMigrateDestination)
	}
	if err := options.Destination.PingContext(ctx); err != nil {
		return report, fmt.Errorf("%w: %v", ErrMigrateDestination, err)
	}

	source, err := openSource(sourcePath)
	if err != nil {
		return report, err
	}
	defer func() { _ = source.Close() }()

	// Pré-checagem: as duas pontas precisam ter o schema final. A origem precisa
	// ser legível; o destino precisa estar migrado e vazio.
	if err := checkSchema(ctx, source, ErrMigrateSource); err != nil {
		return report, err
	}
	if err := checkSchema(ctx, options.Destination, ErrMigrateDestinationNotMigrated); err != nil {
		return report, err
	}
	if err := checkEmpty(ctx, options.Destination); err != nil {
		return report, err
	}

	// Leitura da origem: contagem e checksum por tabela.
	for _, table := range migrationTables {
		count, checksum, err := measure(ctx, source, table)
		if err != nil {
			return report, fmt.Errorf("%w: medir %s: %v", ErrMigrateSource, table.name, err)
		}
		report.Tables = append(report.Tables, MigrateTableReport{
			Table: table.name, SourceRows: count, SourceChecksum: checksum,
		})
	}
	if options.DryRun {
		report.Duration = time.Since(started)
		return report, nil
	}

	// Cópia em transação, na ordem de dependência das chaves estrangeiras.
	transaction, err := options.Destination.BeginTx(ctx, nil)
	if err != nil {
		return report, fmt.Errorf("%w: abrir transação: %v", ErrMigrateDestination, err)
	}

	for index, table := range migrationTables {
		entry := &report.Tables[index]

		copied, err := copyTable(ctx, source, transaction, options.DestinationDialect, table)
		if err != nil {
			report.FailedTable = table.name
			report.Duration = time.Since(started)
			_ = transaction.Rollback()
			return report, fmt.Errorf("copiar %s: %w", table.name, err)
		}
		entry.Copied = copied

		count, checksum, err := measure(ctx, transaction, table)
		if err != nil {
			report.FailedTable = table.name
			report.Duration = time.Since(started)
			_ = transaction.Rollback()
			return report, fmt.Errorf("verificar %s: %w", table.name, err)
		}
		entry.DestinationRows = count
		entry.DestinationChecksum = checksum

		if !entry.Verified() {
			report.FailedTable = table.name
			report.Duration = time.Since(started)
			_ = transaction.Rollback()
			return report, fmt.Errorf("%w: %s (origem %d linha(s), destino %d)", ErrMigrateVerification,
				table.name, entry.SourceRows, entry.DestinationRows)
		}
	}

	if err := transaction.Commit(); err != nil {
		report.FailedTable = lastTable()
		report.Duration = time.Since(started)
		return report, fmt.Errorf("%w: confirmar transação: %v", ErrMigrateDestination, err)
	}

	report.Duration = time.Since(started)
	return report, nil
}

// ── Especificação das tabelas ──────────────────────────────────────

// migrationTable descreve uma tabela na ordem de cópia.
type migrationTable struct {
	name    string
	columns []string
	// key é a chave primária: define a ordem estável do checksum.
	key []string
}

// migrationTables é a ordem de dependência da cópia: cada tabela vem depois de
// tudo o que as chaves estrangeiras dela referenciam
// (`users → properties → tenants → leases → receipts → settings → audit_logs` e
// depois as tabelas novas).
var migrationTables = []migrationTable{
	{name: "users", key: []string{"id"}, columns: []string{
		"id", "username", "password_hash", "password_changed_at", "created_at",
	}},
	{name: "properties", key: []string{"id"}, columns: []string{
		"id", "name", "address", "city", "state", "zip", "created_at", "updated_at",
	}},
	{name: "tenants", key: []string{"id"}, columns: []string{
		"id", "name", "email", "phone", "cpf", "password_hash", "password_changed_at", "created_at", "updated_at",
	}},
	{name: "leases", key: []string{"id"}, columns: []string{
		"id", "tenant_id", "property_id", "start_date", "end_date", "due_day", "rent_amount", "status", "auth_token",
		"created_at", "updated_at", "deposit_amount", "deposit_status", "deposit_received_at", "deposit_refunded_at",
		"deposit_refunded_amount", "deposit_notes",
	}},
	{name: "receipts", key: []string{"id"}, columns: []string{
		"id", "lease_id", "tenant_id", "property_id", "category", "description", "due_date", "paid_at", "amount",
		"paid_amount", "pdf_path", "generated_key", "auth_token", "created_at", "updated_at",
	}},
	{name: "settings", key: []string{"key"}, columns: []string{"key", "value"}},
	{name: "audit_logs", key: []string{"id"}, columns: []string{
		"id", "created_at", "actor_id", "actor_name", "action", "entity", "entity_id", "entity_label", "summary",
		"changes", "metadata", "origin", "ip", "user_agent", "request_id",
	}},
	{name: "inspections", key: []string{"id"}, columns: []string{
		"id", "lease_id", "kind", "performed_at", "condition", "notes", "items", "photos", "auth_token",
		"created_at", "updated_at",
	}},
	{name: "whatsapp_messages", key: []string{"id"}, columns: []string{
		"id", "created_at", "direction", "chat_id", "phone", "tenant_id", "body", "status", "external_id", "metadata",
	}},
	{name: "jobs", key: []string{"id"}, columns: []string{
		"id", "created_at", "updated_at", "kind", "status", "attempts", "max_attempts", "payload", "result", "error",
		"scheduled_at", "started_at", "finished_at",
	}},
	{name: "payment_proofs", key: []string{"id"}, columns: []string{
		"id", "created_at", "updated_at", "source", "phone", "tenant_id", "receipt_id", "file_name", "file_path",
		"mime", "extracted_amount", "extracted_date", "extracted_payer", "extracted_payee", "extracted_identifier",
		"confidence", "status", "decision", "decision_reason", "decided_by", "decided_at", "raw_response",
	}},
	{name: "document_intakes", key: []string{"id"}, columns: []string{
		"id", "created_at", "updated_at", "kind", "file_name", "file_path", "mime", "status", "extracted",
		"confidence", "created_entity", "created_entity_id", "raw_response", "notes",
	}},
}

// lastTable devolve o nome da última tabela da ordem de cópia.
func lastTable() string {
	return migrationTables[len(migrationTables)-1].name
}

// ── Pré-checagem ───────────────────────────────────────────────────

// resolveSourcePath normaliza o caminho da origem.
func resolveSourcePath(source string) (string, error) {
	target := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(source), "file:"))
	if target == "" {
		return "", fmt.Errorf("%w: caminho da origem não informado", ErrMigrateSource)
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("%w: resolver caminho %q: %v", ErrMigrateSource, target, err)
	}
	return absolute, nil
}

// openSource abre a origem SQLite em modo somente consulta: a migração lê e
// nunca escreve, então o arquivo de origem fica intacto por construção.
func openSource(path string) (*sql.DB, error) {
	sqlDB, err := sql.Open("sqlite", "file:"+path+"?_pragma=query_only(true)")
	if err != nil {
		return nil, fmt.Errorf("%w: abrir %q: %v", ErrMigrateSource, path, err)
	}
	// Uma conexão só: a origem é lida em sequência e não há concorrência aqui.
	sqlDB.SetMaxOpenConns(1)
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("%w: abrir %q: %v", ErrMigrateSource, path, err)
	}
	return sqlDB, nil
}

// checkSchema confirma que todas as tabelas e colunas esperadas existem, com o
// erro sentinela do extremo verificado.
func checkSchema(ctx context.Context, sqlDB queryer, sentinel error) error {
	for _, table := range migrationTables {
		columns, err := tableColumns(ctx, sqlDB, table.name)
		if err != nil {
			return fmt.Errorf("%w: tabela %s: %v", sentinel, table.name, err)
		}
		for _, column := range table.columns {
			if !columns[column] {
				return fmt.Errorf("%w: tabela %s sem a coluna %s", sentinel, table.name, column)
			}
		}
	}
	return nil
}

// checkEmpty confirma que o destino não tem nenhuma linha nas tabelas do
// domínio: copiar por cima duplicaria dado.
func checkEmpty(ctx context.Context, sqlDB queryer) error {
	for _, table := range migrationTables {
		count, err := countRows(ctx, sqlDB, table.name)
		if err != nil {
			return fmt.Errorf("%w: contar %s: %v", ErrMigrateDestination, table.name, err)
		}
		if count > 0 {
			return fmt.Errorf("%w: tabela %s tem %d linha(s)", ErrMigrateDestinationNotEmpty, table.name, count)
		}
	}
	return nil
}

// tableColumns lê os nomes das colunas de uma tabela sem depender do dialeto:
// uma consulta sem linhas devolve os nomes das colunas nos dois drivers.
func tableColumns(ctx context.Context, sqlDB queryer, table string) (map[string]bool, error) {
	rows, err := sqlDB.QueryContext(ctx, "SELECT * FROM "+quoteIdent(table)+" WHERE 1 = 0")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	names, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	columns := make(map[string]bool, len(names))
	for _, name := range names {
		columns[name] = true
	}
	return columns, nil
}

// ── Medição ────────────────────────────────────────────────────────

// queryer é o mínimo que a leitura precisa: vale tanto para a conexão quanto
// para a transação, e é o que permite verificar o destino dentro da própria
// transação da cópia.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// measure conta as linhas e calcula o checksum de uma tabela.
func measure(ctx context.Context, sqlDB queryer, table migrationTable) (int64, string, error) {
	count, err := countRows(ctx, sqlDB, table.name)
	if err != nil {
		return 0, "", err
	}
	checksum, err := tableChecksum(ctx, sqlDB, table)
	if err != nil {
		return 0, "", err
	}
	return count, checksum, nil
}

// countRows conta as linhas da tabela.
func countRows(ctx context.Context, sqlDB queryer, table string) (int64, error) {
	var count int64
	err := sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteIdent(table)).Scan(&count)
	return count, err
}

// tableChecksum calcula o SHA-256 do conteúdo da tabela, linha a linha, na
// ordem da chave primária. Hash de todas as colunas (e não só das chaves): é o
// que prova que o valor copiado chegou igual ao destino.
func tableChecksum(ctx context.Context, sqlDB queryer, table migrationTable) (string, error) {
	query := "SELECT " + identList(table.columns) + " FROM " + quoteIdent(table.name) +
		" ORDER BY " + identList(table.key)

	rows, err := sqlDB.QueryContext(ctx, query)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()

	hash := sha256.New()
	values := make([]any, len(table.columns))
	pointers := make([]any, len(table.columns))
	for i := range values {
		pointers[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(pointers...); err != nil {
			return "", err
		}
		for _, value := range values {
			writeCanonical(hash, value)
		}
		hash.Write([]byte{0x1e}) // separador de linha
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// writeCanonical escreve um valor de forma estável entre os dialetos: o mesmo
// dado lido do SQLite e do Postgres produz o mesmo byte.
func writeCanonical(writer io.Writer, value any) {
	switch typed := value.(type) {
	case nil:
		_, _ = io.WriteString(writer, "\x00")
	case string:
		_, _ = io.WriteString(writer, "s:"+typed)
	case []byte:
		_, _ = writer.Write(append([]byte("b:"), typed...))
	case int64:
		_, _ = io.WriteString(writer, "i:"+strconv.FormatInt(typed, 10))
	case float64:
		_, _ = io.WriteString(writer, "f:"+strconv.FormatFloat(typed, 'g', -1, 64))
	case bool:
		_, _ = io.WriteString(writer, "t:"+strconv.FormatBool(typed))
	case time.Time:
		_, _ = io.WriteString(writer, "d:"+typed.UTC().Format(time.RFC3339Nano))
	default:
		_, _ = fmt.Fprintf(writer, "?:%v", typed)
	}
	_, _ = io.WriteString(writer, "\x1f")
}

// ── Cópia ──────────────────────────────────────────────────────────

// copyTable copia as linhas de uma tabela da origem para a transação do destino.
func copyTable(ctx context.Context, source *sql.DB, destination *sql.Tx, dialect Dialect, table migrationTable) (int64, error) {
	rows, err := source.QueryContext(ctx, "SELECT "+identList(table.columns)+" FROM "+quoteIdent(table.name)+
		" ORDER BY "+identList(table.key))
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	statement, err := destination.PrepareContext(ctx, insertStatement(table, dialect))
	if err != nil {
		return 0, err
	}
	defer func() { _ = statement.Close() }()

	values := make([]any, len(table.columns))
	pointers := make([]any, len(table.columns))
	for i := range values {
		pointers[i] = &values[i]
	}

	var copied int64
	for rows.Next() {
		if err := rows.Scan(pointers...); err != nil {
			return copied, err
		}
		if _, err := statement.ExecContext(ctx, values...); err != nil {
			return copied, err
		}
		copied++
	}
	return copied, rows.Err()
}

// insertStatement monta o INSERT da tabela com o marcador do dialeto.
func insertStatement(table migrationTable, dialect Dialect) string {
	placeholders := make([]string, len(table.columns))
	for i := range table.columns {
		if dialect == DialectPostgres {
			placeholders[i] = "$" + strconv.Itoa(i+1)
		} else {
			placeholders[i] = "?"
		}
	}
	return "INSERT INTO " + quoteIdent(table.name) + " (" + identList(table.columns) + ") VALUES (" +
		strings.Join(placeholders, ", ") + ")"
}

// identList cita e junta uma lista de identificadores.
func identList(columns []string) string {
	quoted := make([]string, len(columns))
	for i, column := range columns {
		quoted[i] = quoteIdent(column)
	}
	return strings.Join(quoted, ", ")
}

// quoteIdent cita um identificador nos dois dialetos.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
