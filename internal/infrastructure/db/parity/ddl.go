// Package parity mantém o schema dos dois dialetos em sincronia: o mesmo
// arquivo SQL alimenta o sqlc do SQLite e o do Postgres, então uma divergência
// de tabela, coluna, nulidade, chave ou índice precisa quebrar o build.
//
// O parser de DDL daqui é deliberadamente pequeno e legível: ele entende apenas
// as instruções que o projeto usa (CREATE TABLE, CREATE INDEX) e normaliza os
// tipos pelo mapa canônico text→text, integer→integer, real→double precision.
package parity

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Column é uma coluna do schema, já normalizada.
type Column struct {
	Name    string
	Type    string // canônico: text, integer ou real
	NotNull bool
	Primary bool
}

// ForeignKey é uma chave estrangeira de tabela.
type ForeignKey struct {
	Column   string
	Table    string
	OnDelete string // CASCADE, SET NULL, NO ACTION, RESTRICT ou SET DEFAULT
}

// Index é um índice nomeado.
type Index struct {
	Name    string
	Table   string
	Unique  bool
	Columns []string
}

// Table é uma tabela do schema.
type Table struct {
	Name        string
	Columns     []Column
	ForeignKeys []ForeignKey
}

// Schema é o conjunto de tabelas e índices de um banco.
type Schema struct {
	Tables  []Table
	Indexes []Index
}

// canonicalTypes mapeia os tipos de cada dialeto para o nome canônico.
var canonicalTypes = map[string]string{
	"text":             "text",
	"integer":          "integer",
	"real":             "real",
	"double precision": "real",
}

// ParseSQL lê o schema descrito por um texto SQL. Entende CREATE TABLE,
// CREATE INDEX e ALTER TABLE ... ADD COLUMN (as migrações acrescentam colunas
// assim); qualquer outra instrução é ignorada, e tudo a partir do marcador
// `-- +goose Down` é descartado — o que precisa casar com o schema canônico é o
// caminho de subida.
func ParseSQL(source string) (Schema, error) {
	var schema Schema
	for _, statement := range splitStatements(source) {
		switch {
		case createTablePattern.MatchString(statement):
			table, err := parseCreateTable(statement)
			if err != nil {
				return Schema{}, err
			}
			schema.Tables = append(schema.Tables, table)
		case createIndexPattern.MatchString(statement):
			index, err := parseCreateIndex(statement)
			if err != nil {
				return Schema{}, err
			}
			schema.Indexes = append(schema.Indexes, index)
		case alterAddColumnPattern.MatchString(statement):
			if err := applyAddColumn(&schema, statement); err != nil {
				return Schema{}, err
			}
		}
	}
	sortSchema(&schema)
	return schema, nil
}

// applyAddColumn acrescenta a coluna ao modelo da tabela alterada. Coluna
// adicionada depois é sempre a última da tabela, exatamente como o banco faz.
func applyAddColumn(schema *Schema, statement string) error {
	matches := alterAddColumnPattern.FindStringSubmatch(statement)
	if matches == nil {
		return fmt.Errorf("não foi possível ler o ALTER TABLE: %s", statement)
	}
	tableName := normalizeIdentifier(matches[1])
	// Grupo 2 é o `COLUMN` opcional; grupo 3 é a definição da coluna.
	column, err := parseColumn(strings.TrimSpace(matches[3]))
	if err != nil {
		return err
	}
	for index := range schema.Tables {
		if schema.Tables[index].Name == tableName {
			schema.Tables[index].Columns = append(schema.Tables[index].Columns, column)
			return nil
		}
	}
	return fmt.Errorf("ALTER TABLE em tabela ausente do schema: %s", tableName)
}

// LoadSQLite monta o schema efetivamente presente num banco SQLite, lendo o DDL
// guardado em sqlite_master. Tabelas e índices internos são ignorados, assim
// como os nomes listados em ignore (controle de migração, por exemplo).
func LoadSQLite(ctx context.Context, db *sql.DB, ignore map[string]bool) (Schema, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT name, sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return Schema{}, fmt.Errorf("ler o schema do SQLite: %w", err)
	}
	defer rows.Close()

	var statements []string
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			return Schema{}, fmt.Errorf("ler o schema do SQLite: %w", err)
		}
		if ignore[name] {
			continue
		}
		statements = append(statements, ddl)
	}
	if err := rows.Err(); err != nil {
		return Schema{}, fmt.Errorf("percorrer o schema do SQLite: %w", err)
	}

	schema, err := ParseSQL(strings.Join(statements, ";\n"))
	if err != nil {
		return Schema{}, err
	}
	filtered := Schema{}
	for _, table := range schema.Tables {
		if !ignore[table.Name] {
			filtered.Tables = append(filtered.Tables, table)
		}
	}
	for _, index := range schema.Indexes {
		if !ignore[index.Name] && !ignore[index.Table] {
			filtered.Indexes = append(filtered.Indexes, index)
		}
	}
	return filtered, nil
}

// Diff compara dois schemas e devolve uma lista legível de divergências.
func Diff(expected, actual Schema, expectedLabel, actualLabel string) []string {
	var diffs []string

	expectedTables := map[string]Table{}
	for _, table := range expected.Tables {
		expectedTables[table.Name] = table
	}
	actualTables := map[string]Table{}
	for _, table := range actual.Tables {
		actualTables[table.Name] = table
	}

	for _, name := range tableNames(expected.Tables) {
		table, ok := actualTables[name]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("tabela %q existe em %s e falta em %s", name, expectedLabel, actualLabel))
			continue
		}
		diffs = append(diffs, diffTable(expectedTables[name], table, expectedLabel, actualLabel)...)
	}
	for _, name := range tableNames(actual.Tables) {
		if _, ok := expectedTables[name]; !ok {
			diffs = append(diffs, fmt.Sprintf("tabela %q existe em %s e falta em %s", name, actualLabel, expectedLabel))
		}
	}

	expectedIndexes := map[string]Index{}
	for _, index := range expected.Indexes {
		expectedIndexes[index.Name] = index
	}
	actualIndexes := map[string]Index{}
	for _, index := range actual.Indexes {
		actualIndexes[index.Name] = index
	}
	for _, name := range indexNames(expected.Indexes) {
		index, ok := actualIndexes[name]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("índice %q existe em %s e falta em %s", name, expectedLabel, actualLabel))
			continue
		}
		diffs = append(diffs, diffIndex(expectedIndexes[name], index, expectedLabel, actualLabel)...)
	}
	for _, name := range indexNames(actual.Indexes) {
		if _, ok := expectedIndexes[name]; !ok {
			diffs = append(diffs, fmt.Sprintf("índice %q existe em %s e falta em %s", name, actualLabel, expectedLabel))
		}
	}

	return diffs
}

func diffTable(expected, actual Table, expectedLabel, actualLabel string) []string {
	var diffs []string
	where := fmt.Sprintf("tabela %q", expected.Name)

	if len(expected.Columns) != len(actual.Columns) {
		diffs = append(diffs, fmt.Sprintf("%s: %s tem %d colunas e %s tem %d",
			where, expectedLabel, len(expected.Columns), actualLabel, len(actual.Columns)))
	}

	expectedColumns := map[string]Column{}
	for _, column := range expected.Columns {
		expectedColumns[column.Name] = column
	}
	actualColumns := map[string]Column{}
	for _, column := range actual.Columns {
		actualColumns[column.Name] = column
	}
	for i, column := range expected.Columns {
		other, ok := actualColumns[column.Name]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("%s: coluna %q falta em %s", where, column.Name, actualLabel))
			continue
		}
		if column.Type != other.Type {
			diffs = append(diffs, fmt.Sprintf("%s: coluna %q é %q em %s e %q em %s",
				where, column.Name, column.Type, expectedLabel, other.Type, actualLabel))
		}
		if column.NotNull != other.NotNull {
			diffs = append(diffs, fmt.Sprintf("%s: nulidade de %q difere (%s: notnull=%t, %s: notnull=%t)",
				where, column.Name, expectedLabel, column.NotNull, actualLabel, other.NotNull))
		}
		if column.Primary != other.Primary {
			diffs = append(diffs, fmt.Sprintf("%s: chave primária de %q difere (%s: %t, %s: %t)",
				where, column.Name, expectedLabel, column.Primary, actualLabel, other.Primary))
		}
		if i < len(actual.Columns) && actual.Columns[i].Name != column.Name {
			diffs = append(diffs, fmt.Sprintf("%s: ordem das colunas difere (posição %d é %q em %s e %q em %s)",
				where, i+1, column.Name, expectedLabel, actual.Columns[i].Name, actualLabel))
		}
	}
	for _, column := range actual.Columns {
		if _, ok := expectedColumns[column.Name]; !ok {
			diffs = append(diffs, fmt.Sprintf("%s: coluna %q sobra em %s", where, column.Name, actualLabel))
		}
	}

	expectedFKs := map[string]ForeignKey{}
	for _, fk := range expected.ForeignKeys {
		expectedFKs[fk.Column] = fk
	}
	actualFKs := map[string]ForeignKey{}
	for _, fk := range actual.ForeignKeys {
		actualFKs[fk.Column] = fk
	}
	for _, column := range columnNames(expected.ForeignKeys) {
		fk := expectedFKs[column]
		other, ok := actualFKs[column]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("%s: chave estrangeira de %q falta em %s", where, column, actualLabel))
			continue
		}
		if fk.Table != other.Table {
			diffs = append(diffs, fmt.Sprintf("%s: chave estrangeira de %q aponta para %q em %s e %q em %s",
				where, column, fk.Table, expectedLabel, other.Table, actualLabel))
		}
		if fk.OnDelete != other.OnDelete {
			diffs = append(diffs, fmt.Sprintf("%s: ON DELETE de %q é %q em %s e %q em %s",
				where, column, fk.OnDelete, expectedLabel, other.OnDelete, actualLabel))
		}
	}
	for _, fk := range actual.ForeignKeys {
		if _, ok := expectedFKs[fk.Column]; !ok {
			diffs = append(diffs, fmt.Sprintf("%s: chave estrangeira de %q sobra em %s", where, fk.Column, actualLabel))
		}
	}

	return diffs
}

func diffIndex(expected, actual Index, expectedLabel, actualLabel string) []string {
	var diffs []string
	where := fmt.Sprintf("índice %q", expected.Name)

	if expected.Table != actual.Table {
		diffs = append(diffs, fmt.Sprintf("%s: tabela é %q em %s e %q em %s",
			where, expected.Table, expectedLabel, actual.Table, actualLabel))
	}
	if expected.Unique != actual.Unique {
		diffs = append(diffs, fmt.Sprintf("%s: unicidade difere (%s: %t, %s: %t)",
			where, expectedLabel, expected.Unique, actualLabel, actual.Unique))
	}
	if strings.Join(expected.Columns, ",") != strings.Join(actual.Columns, ",") {
		diffs = append(diffs, fmt.Sprintf("%s: colunas são (%s) em %s e (%s) em %s",
			where, strings.Join(expected.Columns, ", "), expectedLabel,
			strings.Join(actual.Columns, ", "), actualLabel))
	}
	return diffs
}

// ── Parser ─────────────────────────────────────────────────────────

var (
	createTablePattern = regexp.MustCompile(`(?is)^create\s+table\b`)
	createIndexPattern = regexp.MustCompile(`(?is)^create\s+(unique\s+)?index\b`)
	tableHeaderPattern = regexp.MustCompile(`(?is)^create\s+table\s+(if\s+not\s+exists\s+)?("[^"]+"|` + "`[^`]+`" + `|\w+)\s*\(`)
	indexPattern       = regexp.MustCompile(`(?is)^create\s+(unique\s+)?index\s+(if\s+not\s+exists\s+)?("[^"]+"|` + "`[^`]+`" + `|\w+)\s+on\s+("[^"]+"|` + "`[^`]+`" + `|\w+)\s*\(([^)]*)\)`)
	foreignKeyPattern  = regexp.MustCompile(`(?is)^foreign\s+key\s*\(([^)]*)\)\s*references\s+("[^"]+"|` + "`[^`]+`" + `|\w+)\s*\(([^)]*)\)(.*)$`)
	onDeletePattern    = regexp.MustCompile(`(?is)on\s+delete\s+(no\s+action|set\s+null|set\s+default|cascade|restrict)`)
	primaryKeyPattern  = regexp.MustCompile(`(?is)^primary\s+key\s*\(([^)]*)\)`)
	// ALTER TABLE ... ADD COLUMN é como as migrações acrescentam coluna a tabela
	// existente; o parâmetro [^;]* casa a definição da coluna até o fim da
	// instrução (a coluna é a última da tabela, como o banco a guarda).
	alterAddColumnPattern = regexp.MustCompile(`(?is)^alter\s+table\s+("[^"]+"|` + "`[^`]+`" + `|\w+)\s+add\s+(column\s+)?([^;]*)$`)
	gooseDownPattern      = regexp.MustCompile(`(?i)^\s*--\s*\+goose\s+down\b`)
	gooseUpPattern        = regexp.MustCompile(`(?i)^\s*--\s*\+goose\s+up\b`)
)

// splitStatements quebra o texto em instruções, descartando comentários de
// linha e instruções vazias.
func splitStatements(source string) []string {
	// Só o caminho de subida descreve o schema final: o Down desfaz o que o Up
	// fez e, se fosse lido, cancelaria as colunas recém-acrescentadas. Os
	// marcadores são tratados por linha, porque vários arquivos de migração são
	// concatenados antes da leitura (o Down de um não pode esconder o Up do
	// seguinte).
	var withoutComments strings.Builder
	skipping := false
	for _, line := range strings.Split(source, "\n") {
		if gooseDownPattern.MatchString(line) {
			skipping = true
			continue
		}
		if gooseUpPattern.MatchString(line) {
			skipping = false
			continue
		}
		if skipping {
			continue
		}
		if index := strings.Index(line, "--"); index >= 0 {
			line = line[:index]
		}
		withoutComments.WriteString(line)
		withoutComments.WriteString("\n")
	}

	var statements []string
	for _, statement := range strings.Split(withoutComments.String(), ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}

func parseCreateTable(statement string) (Table, error) {
	header := tableHeaderPattern.FindStringSubmatch(statement)
	if header == nil {
		return Table{}, fmt.Errorf("não entendi o CREATE TABLE: %.60s", statement)
	}
	table := Table{Name: normalizeIdentifier(header[2])}

	body, err := tableBody(statement, header[0])
	if err != nil {
		return Table{}, fmt.Errorf("tabela %q: %w", table.Name, err)
	}

	for _, item := range splitTopLevel(body) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		switch {
		case foreignKeyPattern.MatchString(item):
			fk, err := parseForeignKey(item)
			if err != nil {
				return Table{}, fmt.Errorf("tabela %q: %w", table.Name, err)
			}
			table.ForeignKeys = append(table.ForeignKeys, fk)
		case primaryKeyPattern.MatchString(item):
			// chave primária composta: marcada nas colunas depois
			continue
		default:
			column, err := parseColumn(item)
			if err != nil {
				return Table{}, fmt.Errorf("tabela %q: %w", table.Name, err)
			}
			table.Columns = append(table.Columns, column)
		}
	}
	return table, nil
}

// tableBody devolve o conteúdo entre os parênteses do CREATE TABLE.
func tableBody(statement, header string) (string, error) {
	open := strings.Index(statement[len(header)-1:], "(")
	if open < 0 {
		return "", fmt.Errorf("parêntese de abertura ausente")
	}
	start := len(header) - 1 + open
	end := strings.LastIndex(statement, ")")
	if end <= start {
		return "", fmt.Errorf("parêntese de fechamento ausente")
	}
	return statement[start+1 : end], nil
}

func parseColumn(item string) (Column, error) {
	fields := strings.Fields(item)
	if len(fields) < 2 {
		return Column{}, fmt.Errorf("não entendi a coluna: %q", item)
	}
	column := Column{Name: normalizeIdentifier(fields[0])}

	typeName := strings.ToLower(fields[1])
	if len(fields) > 2 {
		if twoWords := typeName + " " + strings.ToLower(fields[2]); canonicalTypes[twoWords] != "" {
			typeName = twoWords
		}
	}
	canonical, ok := canonicalTypes[typeName]
	if !ok {
		return Column{}, fmt.Errorf("tipo desconhecido %q na coluna %q (mapa aceito: text, integer, real, double precision)",
			typeName, column.Name)
	}
	column.Type = canonical

	rest := strings.ToUpper(strings.Join(fields[2:], " "))
	if strings.Contains(rest, "PRIMARY KEY") {
		column.Primary = true
	}
	column.NotNull = strings.Contains(rest, "NOT NULL")
	return column, nil
}

func parseForeignKey(item string) (ForeignKey, error) {
	matches := foreignKeyPattern.FindStringSubmatch(item)
	if matches == nil {
		return ForeignKey{}, fmt.Errorf("não entendi a chave estrangeira: %q", item)
	}
	fk := ForeignKey{
		Column:   normalizeIdentifier(matches[1]),
		Table:    normalizeIdentifier(matches[2]),
		OnDelete: "NO ACTION",
	}
	if action := onDeletePattern.FindStringSubmatch(matches[4]); action != nil {
		fk.OnDelete = strings.ToUpper(strings.Join(strings.Fields(action[1]), " "))
	}
	return fk, nil
}

func parseCreateIndex(statement string) (Index, error) {
	matches := indexPattern.FindStringSubmatch(statement)
	if matches == nil {
		return Index{}, fmt.Errorf("não entendi o CREATE INDEX: %.60s", statement)
	}
	index := Index{
		Name:   normalizeIdentifier(matches[3]),
		Table:  normalizeIdentifier(matches[4]),
		Unique: strings.TrimSpace(matches[1]) != "",
	}
	for _, column := range strings.Split(matches[5], ",") {
		if column = strings.TrimSpace(column); column != "" {
			index.Columns = append(index.Columns, normalizeIdentifier(column))
		}
	}
	return index, nil
}

// splitTopLevel quebra a lista pelo separador de vírgula, respeitando
// parênteses (chaves estrangeiras compostas).
func splitTopLevel(body string) []string {
	var items []string
	depth, start := 0, 0
	for i, r := range body {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				items = append(items, body[start:i])
				start = i + 1
			}
		}
	}
	items = append(items, body[start:])
	return items
}

// normalizeIdentifier remove aspas e normaliza para minúsculas.
func normalizeIdentifier(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"`")
	return strings.ToLower(value)
}

func sortSchema(schema *Schema) {
	sort.Slice(schema.Tables, func(i, j int) bool { return schema.Tables[i].Name < schema.Tables[j].Name })
	sort.Slice(schema.Indexes, func(i, j int) bool { return schema.Indexes[i].Name < schema.Indexes[j].Name })
	for i := range schema.Tables {
		sort.Slice(schema.Tables[i].ForeignKeys, func(a, b int) bool {
			return schema.Tables[i].ForeignKeys[a].Column < schema.Tables[i].ForeignKeys[b].Column
		})
	}
}

func tableNames(tables []Table) []string {
	names := make([]string, 0, len(tables))
	for _, table := range tables {
		names = append(names, table.Name)
	}
	return names
}

func indexNames(indexes []Index) []string {
	names := make([]string, 0, len(indexes))
	for _, index := range indexes {
		names = append(names, index.Name)
	}
	return names
}

func columnNames(fks []ForeignKey) []string {
	names := make([]string, 0, len(fks))
	for _, fk := range fks {
		names = append(names, fk.Column)
	}
	return names
}
