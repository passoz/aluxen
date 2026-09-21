package application

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrationSpecMatchesSchema é a trava contra perda silenciosa de dado: a
// especificação da migração precisa listar exatamente as tabelas e as colunas do
// schema canônico, na mesma ordem. Uma coluna acrescentada ao schema sem entrar
// aqui seria copiada — e ficaria para trás sem ninguém perceber.
func TestMigrationSpecMatchesSchema(t *testing.T) {
	schema := readSchemaTables(t, "schema/sqlite.sql")

	spec := make(map[string]migrationTable, len(migrationTables))
	for _, table := range migrationTables {
		spec[table.name] = table
	}

	assert.Len(t, spec, len(schema), "a especificação precisa cobrir todas as tabelas do schema")
	for name, expected := range schema {
		table, ok := spec[name]
		if !assert.True(t, ok, "tabela %s do schema está fora da especificação da migração", name) {
			continue
		}
		assert.Equal(t, expected.columns, table.columns,
			"as colunas de %s precisam ser as do schema, na mesma ordem", name)
		assert.NotEmpty(t, table.key, "a tabela %s precisa de chave para ordenar o checksum", name)
	}
}

// TestMigrationOrderRespectsForeignKeys garante que nenhuma tabela é copiada
// antes do que ela referencia: a cópia não desliga as chaves estrangeiras.
func TestMigrationOrderRespectsForeignKeys(t *testing.T) {
	schema := readSchemaTables(t, "schema/sqlite.sql")

	position := make(map[string]int, len(migrationTables))
	for index, table := range migrationTables {
		position[table.name] = index
	}

	for _, table := range migrationTables {
		for _, reference := range schema[table.name].refs {
			parent, ok := position[reference]
			require.True(t, ok, "%s referencia %s, que não está na ordem de cópia", table.name, reference)
			assert.Less(t, parent, position[table.name],
				"%s precisa ser copiada depois de %s", table.name, reference)
		}
	}
}

// schemaTable é uma tabela do schema canônico.
type schemaTable struct {
	columns []string
	refs    []string
}

var (
	createTablePattern = regexp.MustCompile(`(?s)CREATE TABLE (\w+) \((.*?)\n\);`)
	foreignKeyPattern  = regexp.MustCompile(`FOREIGN KEY \(\w+\) REFERENCES (\w+)`)
	columnPattern      = regexp.MustCompile(`^\s*(\w+)\s`)
)

// readSchemaTables lê o DDL canônico e devolve as colunas e as referências de
// cada tabela.
func readSchemaTables(t *testing.T, relative string) map[string]schemaTable {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), relative))
	require.NoError(t, err)

	tables := map[string]schemaTable{}
	for _, match := range createTablePattern.FindAllStringSubmatch(string(raw), -1) {
		table := schemaTable{}
		for _, line := range strings.Split(match[2], "\n") {
			trimmed := strings.TrimSpace(line)
			switch {
			case trimmed == "", strings.HasPrefix(trimmed, "FOREIGN KEY"),
				strings.HasPrefix(trimmed, "PRIMARY KEY"), strings.HasPrefix(trimmed, "UNIQUE"),
				strings.HasPrefix(trimmed, "CHECK"), strings.HasPrefix(trimmed, "CONSTRAINT"):
				continue
			}
			if column := columnPattern.FindStringSubmatch(line); column != nil {
				table.columns = append(table.columns, column[1])
			}
		}
		for _, reference := range foreignKeyPattern.FindAllStringSubmatch(match[2], -1) {
			table.refs = append(table.refs, reference[1])
		}
		tables[match[1]] = table
	}
	require.NotEmpty(t, tables, "nenhuma tabela lida de %s", relative)
	return tables
}

// repoRoot sobe até a raiz do módulo.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "raiz do módulo não encontrada a partir de %s", dir)
		dir = parent
	}
}
