package parity_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/infrastructure/db"
	"github.com/passoz/aluxen/internal/infrastructure/db/parity"
)

// Estes testes são a trava de paridade do banco: o mesmo arquivo de queries e o
// mesmo comportamento precisam valer para SQLite e Postgres. Qualquer
// divergência de tabela, coluna, nulidade, chave, índice, código gerado ou
// migração quebra aqui, com a lista do que difere.

const (
	sqliteLabel   = "schema/sqlite.sql"
	postgresLabel = "schema/postgres.sql"
)

// TestSchemaParity compara o DDL canônico dos dois dialetos.
func TestSchemaParity(t *testing.T) {
	root := repoRoot(t)
	sqliteSchema := parseFile(t, filepath.Join(root, "schema", "sqlite.sql"))
	postgresSchema := parseFile(t, filepath.Join(root, "schema", "postgres.sql"))

	assertNoDiff(t, parity.Diff(sqliteSchema, postgresSchema, sqliteLabel, postgresLabel))
}

// TestGeneratedCodeParity compara o código gerado pelo sqlc nos dois dialetos,
// normalizando as duas diferenças legítimas: largura dos inteiros (int64 no
// SQLite, int32 no Postgres), marcador de parâmetro (?n e $n) e aspas de
// identificador (o SQLite cita palavras reservadas).
func TestGeneratedCodeParity(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{"models.go", "queries.sql.go"} {
		sqliteFile := filepath.Join(root, "internal", "infrastructure", "db", "sqlite", "sqlcgen", name)
		postgresFile := filepath.Join(root, "internal", "infrastructure", "db", "postgres", "sqlcgen", name)

		got := normalizeGenerated(readFile(t, sqliteFile))
		want := normalizeGenerated(readFile(t, postgresFile))
		if got == want {
			continue
		}
		t.Errorf("%s: sqlcgen do SQLite e do Postgres divergem após normalização:\n%s",
			name, strings.Join(diffLines(want, got), "\n"))
	}
}

// TestSQLiteMigrationsMatchCanonicalSchema aplica as migrações num banco vazio e
// compara o schema resultante com o DDL canônico: a migração e o schema usado
// pelo sqlc não podem divergir.
func TestSQLiteMigrationsMatchCanonicalSchema(t *testing.T) {
	root := repoRoot(t)
	ctx := context.Background()

	store, err := db.Open(ctx, "file:"+filepath.Join(t.TempDir(), "parity.db"), "")
	if err != nil {
		t.Fatalf("abrir banco de paridade: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Errorf("fechar banco de paridade: %v", err)
		}
	}()

	actual, err := parity.LoadSQLite(ctx, store.DB(), map[string]bool{"goose_db_version": true})
	if err != nil {
		t.Fatalf("ler schema migrado: %v", err)
	}
	expected := parseFile(t, filepath.Join(root, "schema", "sqlite.sql"))

	assertNoDiff(t, parity.Diff(expected, actual, sqliteLabel, "migrações aplicadas"))
}

// TestPostgresMigrationsMatchCanonicalSchema faz a mesma conferência do lado
// Postgres, sem servidor: compara o SQL das migrações com o DDL canônico.
func TestPostgresMigrationsMatchCanonicalSchema(t *testing.T) {
	root := repoRoot(t)
	expected := parseFile(t, filepath.Join(root, "schema", "postgres.sql"))
	actual := parseFiles(t, filepath.Join(root, "migrations", "postgres"), ".sql")

	assertNoDiff(t, parity.Diff(expected, actual, postgresLabel, "migrations/postgres"))
}

// ── Apoio ──────────────────────────────────────────────────────────

func assertNoDiff(t *testing.T, diffs []string) {
	t.Helper()
	if len(diffs) == 0 {
		return
	}
	sort.Strings(diffs)
	t.Fatalf("paridade de schema quebrada:\n  %s", strings.Join(diffs, "\n  "))
}

func parseFile(t *testing.T, path string) parity.Schema {
	t.Helper()
	schema, err := parity.ParseSQL(readFile(t, path))
	if err != nil {
		t.Fatalf("ler %s: %v", path, err)
	}
	return schema
}

func parseFiles(t *testing.T, dir, suffix string) parity.Schema {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("listar %s: %v", dir, err)
	}
	var joined strings.Builder
	found := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), suffix) {
			continue
		}
		joined.WriteString(readFile(t, filepath.Join(dir, entry.Name())))
		joined.WriteString("\n")
		found++
	}
	if found == 0 {
		t.Fatalf("nenhuma migração em %s", dir)
	}
	schema, err := parity.ParseSQL(joined.String())
	if err != nil {
		t.Fatalf("ler migrações de %s: %v", dir, err)
	}
	return schema
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ler %s: %v", path, err)
	}
	return string(content)
}

var (
	intWidthPattern    = regexp.MustCompile(`\bint32\b`)
	placeholderPattern = regexp.MustCompile(`\$(\d+)`)
	quotedIdentPattern = regexp.MustCompile(`"([A-Za-z_][A-Za-z0-9_]*)"`)
)

// normalizeGenerated iguala as diferenças esperadas entre os dialetos.
func normalizeGenerated(source string) string {
	source = intWidthPattern.ReplaceAllString(source, "int64")
	source = placeholderPattern.ReplaceAllString(source, "?$1")
	return quotedIdentPattern.ReplaceAllString(source, "$1")
}

// diffLines aponta as primeiras linhas divergentes de dois textos.
func diffLines(want, got string) []string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	var diffs []string
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		expected, actual := "", ""
		if i < len(wantLines) {
			expected = wantLines[i]
		}
		if i < len(gotLines) {
			actual = gotLines[i]
		}
		if expected != actual {
			diffs = append(diffs, "    linha "+strconv.Itoa(i+1)+": postgres: "+strings.TrimSpace(expected)+
				" | sqlite: "+strings.TrimSpace(actual))
			if len(diffs) == 10 {
				diffs = append(diffs, "    ...")
				break
			}
		}
	}
	return diffs
}

// repoRoot sobe até o diretório do go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("descobrir diretório do teste: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod não encontrado a partir do diretório do teste")
		}
		dir = parent
	}
}
