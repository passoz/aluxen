// Command migrate troca uma instância de SQLite para Postgres
// (ARQUITETURA-DEPLOY.md §10): pré-checagem, dry-run, cópia em transação,
// verificação por contagem e checksum e rollback.
//
// Uso:
//
//	go run ./cmd/migrate --from file:/data/aluxen.db --to postgres://user:pass@host:5432/aluxen
//	go run ./cmd/migrate --from file:/data/aluxen.db --to postgres://... --dry-run
//
// O destino precisa estar migrado e vazio — este comando aplica as migrações
// pendentes nele antes de copiar. A origem nunca é apagada: o volume fica como
// backup e o rollback é voltar a `DATABASE_URL` para o arquivo SQLite.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/infrastructure/db"
)

const usageText = `migrate — cópia verificada de SQLite para Postgres

  --from <origem>   arquivo SQLite da instância (aceita file:/data/aluxen.db)
  --to <destino>    DSN do Postgres (postgres://user:pass@host:5432/aluxen)
  --dry-run         conta as linhas por tabela sem escrever nada no destino

A origem é aberta em modo somente consulta e nunca é apagada.
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

// run executa a migração e imprime o relatório.
func run(args []string, out io.Writer) (err error) {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	from := flags.String("from", "", "arquivo SQLite de origem")
	to := flags.String("to", "", "DSN do destino")
	dryRun := flags.Bool("dry-run", false, "contar as linhas sem escrever")

	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*from) == "" || strings.TrimSpace(*to) == "" {
		return fmt.Errorf("--from e --to são obrigatórios\n\n%s", usageText)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// O destino é aberto pelo mesmo caminho do app: resolve o dialeto e aplica
	// as migrações pendentes. Assim ele chega "migrado e vazio" à cópia.
	store, err := db.Open(ctx, *to, "")
	if err != nil {
		return fmt.Errorf("abrir destino: %w", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	report, migrateErr := application.MigrateSQLiteToPostgres(ctx, application.MigrateOptions{
		Source:             *from,
		Destination:        store.DB(),
		DestinationDialect: application.Dialect(store.Dialect),
		DryRun:             *dryRun,
	})
	fmt.Fprint(out, report.String())

	if migrateErr != nil {
		if errors.Is(migrateErr, application.ErrMigrateDestinationNotEmpty) {
			fmt.Fprintln(out, "\no destino precisa estar vazio: aponte para um banco novo, ainda sem dados.")
		}
		return migrateErr
	}

	if *dryRun {
		fmt.Fprintln(out, "\nnada foi escrito. Rode sem --dry-run para copiar.")
		return nil
	}

	fmt.Fprintf(out, "\ncópia concluída em %s. Próximo passo: troque DATABASE_URL da instância para o DSN do Postgres\n"+
		"e faça o redeploy. O SQLite de origem continua no volume, como backup.\n", report.Duration.Round(time.Millisecond))
	return nil
}
