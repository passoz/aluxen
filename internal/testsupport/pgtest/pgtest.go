// Package pgtest dá a cada caso de teste o seu próprio schema na base Postgres
// apontada por TEST_DATABASE_URL.
//
// A suíte roda os pacotes em paralelo e todos compartilham a mesma base de
// teste. Sem isolamento, um caso enxerga o que o outro escreveu: o destino da
// migração SQLite → Postgres, que precisa estar vazio, encontrava as tabelas
// deixadas pelos testes do pacote de banco. Um schema por caso elimina a
// disputa — cada teste nasce com as tabelas que ele mesmo criou.
//
// O pacote é importado apenas por testes; a aplicação não o conhece.
package pgtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Schema é um schema isolado da base de teste.
type Schema struct {
	// DSN abre a base com o search_path apontando para o schema isolado: todo
	// SQL sem qualificação — inclusive as migrações e a tabela do goose — nasce
	// e morre dentro dele.
	DSN string

	name     string
	adminDSN string
}

// Isolate cria um schema exclusivo na base de dsn. Quem chama precisa apagá-lo
// com Drop quando o caso terminar.
func Isolate(ctx context.Context, dsn string) (*Schema, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return nil, fmt.Errorf("sortear nome do schema: %w", err)
	}
	name := "test_" + hex.EncodeToString(random)

	isolated, err := withSearchPath(dsn, name)
	if err != nil {
		return nil, err
	}

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("conectar ao Postgres de teste: %w", err)
	}
	defer func() { _ = admin.Close() }()

	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA "`+name+`"`); err != nil {
		return nil, fmt.Errorf("criar schema %s: %w", name, err)
	}

	return &Schema{DSN: isolated, name: name, adminDSN: dsn}, nil
}

// Name devolve o nome do schema criado.
func (s *Schema) Name() string { return s.name }

// Drop apaga o schema e tudo que foi criado dentro dele.
func (s *Schema) Drop(ctx context.Context) error {
	admin, err := sql.Open("pgx", s.adminDSN)
	if err != nil {
		return fmt.Errorf("conectar ao Postgres de teste: %w", err)
	}
	defer func() { _ = admin.Close() }()

	if _, err := admin.ExecContext(ctx, `DROP SCHEMA IF EXISTS "`+s.name+`" CASCADE`); err != nil {
		return fmt.Errorf("apagar schema %s: %w", s.name, err)
	}
	return nil
}

// withSearchPath acrescenta o search_path ao DSN nas duas formas aceitas pelo
// libpq: URL (`postgres://...?search_path=...`) e pares `chave=valor`.
func withSearchPath(dsn, schema string) (string, error) {
	if strings.Contains(dsn, "search_path") {
		return "", fmt.Errorf("TEST_DATABASE_URL já define search_path: o isolamento não pode ser aplicado")
	}

	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("ler TEST_DATABASE_URL: %w", err)
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String(), nil
	}

	return dsn + " search_path=" + schema, nil
}
