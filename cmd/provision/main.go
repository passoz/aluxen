// Command provision cria, lista e expira as instâncias do Aluxen no Easypanel —
// uma instância por cliente ou demo (ARQUITETURA-DEPLOY.md §8 e §9).
//
// Os segredos vêm do ambiente da operação, nunca do código:
//
//	EASYPANEL_URL      base da API do painel
//	EASYPANEL_API_KEY  chave da API do painel
//	EASYPANEL_PROJECT  projeto do painel (padrão: aluxen)
//
// Uso:
//
//	provision create --name aluxen-demo-buzios --domain buzios.aluxen.evolucsia.com \
//	    --image ghcr.io/passoz/aluxen:sha-abc123 --db-dir /data --seed-demo
//	provision list
//	provision expire --days 0 [--dry-run]
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/passoz/aluxen/internal/infrastructure/easypanel"
)

// defaultProject é o projeto do painel onde todas as instâncias convivem: o
// limite do plano é de projetos, não de apps (ARQUITETURA-DEPLOY.md §2).
const defaultProject = "aluxen"

// defaultPort é a porta do binário Go dentro do container (Dockerfile).
const defaultPort = 3000

// volumeSuffix nomeia o volume persistente a partir do nome do serviço.
const volumeSuffix = "-data"

// passthroughEnv são as variáveis repassadas do ambiente do operador para a
// instância quando definidas. O provisionamento nunca inventa segredo: o que não
// estiver no ambiente simplesmente não vai.
var passthroughEnv = []string{
	"AI_API_URL", "AI_API_KEY", "AI_MODEL", "AI_CONFIDENCE_THRESHOLD",
	"WAHA_API_URL", "WAHA_API_KEY", "WAHA_SESSION", "WAHA_WEBHOOK_SECRET",
	"WHATSAPP_MODE", "LOG_LEVEL", "STORAGE_DIR",
	"DEFAULT_LANDLORD_NAME", "DEFAULT_LANDLORD_CPF",
}

const usageText = `provision — instâncias do Aluxen no Easypanel

Subcomandos:
  create --name <serviço> --domain <host> --image <imagem> [opções]
      Cria (ou ajusta) a instância: app, volume, variáveis, domínio https e deploy.
      --project <nome>        projeto no painel (padrão: $EASYPANEL_PROJECT ou "aluxen")
      --db-dir <caminho>      diretório do banco dentro do volume (padrão: /data)
      --port <porta>          porta do app (padrão: 3000)
      --seed-demo             sobe a instância já com os dados de demonstração
      --seed-username <user>  administrador semeado (padrão: $SEED_USERNAME ou "passoz")
      --seed-password <senha> senha semeada (ou $SEED_PASSWORD; obrigatória)
      --session-secret <s>    segredo de sessão (padrão: $SESSION_SECRET ou gerado)
      --expires-at <data>     etiqueta de expiração: AAAA-MM-DD ou +Nd

  list [--project <nome>]
      Lista as instâncias do projeto com a etiqueta de expiração quando houver.

  expire [--days N | --expires-at <data>] [--dry-run] [--project <nome>]
      Remove as demos vencidas (app e volume) e relata. --days 0 remove só o que
      já venceu; --days 7 antecipa a limpeza para quem vence na semana.

Ambiente: EASYPANEL_URL, EASYPANEL_API_KEY, EASYPANEL_PROJECT.
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

// run despacha os subcomandos.
func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(strings.TrimSpace(usageText))
	}
	// Ctrl-C interrompe o provisionamento entre chamadas da API.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch args[0] {
	case "create":
		return runCreate(ctx, args[1:], out)
	case "list":
		return runList(ctx, args[1:], out)
	case "expire":
		return runExpire(ctx, args[1:], out)
	case "help", "-h", "--help":
		fmt.Fprint(out, usageText)
		return nil
	default:
		return fmt.Errorf("subcomando desconhecido %q\n\n%s", args[0], usageText)
	}
}

// newClient monta o cliente da API a partir do ambiente da operação.
func newClient() (*easypanel.Client, error) {
	client := easypanel.NewClient(easypanel.Config{
		BaseURL: os.Getenv("EASYPANEL_URL"),
		APIKey:  os.Getenv("EASYPANEL_API_KEY"),
	})
	if !client.Configured() {
		return nil, easypanel.ErrNotConfigured
	}
	return client, nil
}

// projectName resolve o projeto do painel.
func projectName(flagValue string) string {
	return firstNonEmpty(flagValue, os.Getenv("EASYPANEL_PROJECT"), defaultProject)
}

// ── create ─────────────────────────────────────────────────────────

// createOptions são os parâmetros do subcomando create.
type createOptions struct {
	project       string
	name          string
	domain        string
	image         string
	dbDir         string
	port          int
	seedDemo      bool
	seedUsername  string
	seedPassword  string
	sessionSecret string
	expiresAt     string
}

// runCreate provisiona (ou converge) uma instância.
func runCreate(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("create", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	options := createOptions{}
	flags.StringVar(&options.project, "project", "", "projeto no painel")
	flags.StringVar(&options.name, "name", "", "nome do serviço")
	flags.StringVar(&options.domain, "domain", "", "subdomínio da instância")
	flags.StringVar(&options.image, "image", "", "imagem publicada no GHCR")
	flags.StringVar(&options.dbDir, "db-dir", "/data", "diretório do banco no volume")
	flags.IntVar(&options.port, "port", defaultPort, "porta do app")
	flags.BoolVar(&options.seedDemo, "seed-demo", false, "popular a instância com dados de demonstração")
	flags.StringVar(&options.seedUsername, "seed-username", "", "administrador semeado")
	flags.StringVar(&options.seedPassword, "seed-password", "", "senha semeada")
	flags.StringVar(&options.sessionSecret, "session-secret", "", "segredo de sessão")
	flags.StringVar(&options.expiresAt, "expires-at", "", "etiqueta de expiração (AAAA-MM-DD ou +Nd)")

	if err := flags.Parse(args); err != nil {
		return err
	}
	options.project = projectName(options.project)
	options.seedUsername = firstNonEmpty(options.seedUsername, os.Getenv("SEED_USERNAME"), "passoz")
	options.seedPassword = firstNonEmpty(options.seedPassword, os.Getenv("SEED_PASSWORD"))
	options.sessionSecret = firstNonEmpty(options.sessionSecret, os.Getenv("SESSION_SECRET"))

	if options.name == "" || options.domain == "" || options.image == "" {
		return errors.New("create exige --name, --domain e --image")
	}
	if options.dbDir == "" {
		return errors.New("--db-dir não pode ser vazio")
	}
	if options.seedPassword == "" {
		return errors.New("create exige --seed-password ou SEED_PASSWORD")
	}
	if options.sessionSecret == "" {
		generated, err := newSecret()
		if err != nil {
			return err
		}
		options.sessionSecret = generated
	}

	var expiresAt time.Time
	if options.expiresAt != "" {
		parsed, err := easypanel.ParseExpiry(options.expiresAt, time.Now())
		if err != nil {
			return err
		}
		expiresAt = parsed
	}

	client, err := newClient()
	if err != nil {
		return err
	}

	report := provisionInstance(ctx, client, options, expiresAt)
	printCreateReport(out, options, expiresAt, report)
	return report.err
}

// step é o resultado de uma etapa do provisionamento.
type step struct {
	Label  string
	Status string
	Err    error
}

// provisionReport é o relatório da sequência de provisionamento.
type provisionReport struct {
	Steps []step
	err   error
}

// add registra a etapa e guarda a primeira falha.
func (r *provisionReport) add(label, status string, err error) {
	r.Steps = append(r.Steps, step{Label: label, Status: status, Err: err})
	if err != nil && r.err == nil {
		r.err = err
	}
}

// provisionInstance executa a sequência do desenho de forma idempotente: o que
// já existe não é recriado, o que falta é ajustado. Uma falha interrompe a
// sequência e é relatada com a etapa onde parou.
func provisionInstance(ctx context.Context, client *easypanel.Client, options createOptions, expiresAt time.Time) *provisionReport {
	report := &provisionReport{}
	volume := options.name + volumeSuffix

	// 1. App com a imagem publicada — nada é construído na VPS (BR-401).
	services, err := client.ListAppServices(ctx)
	if err != nil {
		report.add("listar serviços do painel", "falhou", err)
		return report
	}
	if appServiceExists(services, options.project, options.name) {
		// O serviço já existe: a imagem é convergida para a declarada. É o
		// caminho da atualização canário — mesma sequência, um cliente por vez.
		err := client.UpdateAppSourceImage(ctx, options.project, options.name, options.image)
		report.add("imagem "+options.image, "ajustada", err)
		if err != nil {
			return report
		}
	} else {
		err := client.CreateAppService(ctx, options.project, options.name, options.image)
		report.add("serviço de aplicação "+options.name, "criado", err)
		if err != nil {
			return report
		}
	}

	// 2. Volume persistente onde vive o banco.
	mounts, err := client.ListMounts(ctx, options.project, options.name)
	if err != nil {
		report.add("listar volumes", "falhou", err)
		return report
	}
	if mountExists(mounts, options.dbDir) {
		report.add("volume "+volume+" em "+options.dbDir, "já existia", nil)
	} else {
		err := client.CreateServiceMount(ctx, options.project, options.name,
			easypanel.Mount{Type: "volume", Name: volume, MountPath: options.dbDir})
		report.add("volume "+volume+" em "+options.dbDir, "criado", err)
		if err != nil {
			return report
		}
	}

	// 3. Variáveis da instância. A convergência é declarativa: a instância é do
	// provisionamento e o ambiente é reescrito com o estado declarado.
	if err := client.UpdateAppEnvironment(ctx, options.project, options.name, instanceEnv(options)); err != nil {
		report.add("variáveis da instância", "falhou", err)
		return report
	}
	report.add("variáveis da instância", "atualizadas", nil)

	// 4. Domínio https com certificado do Let's Encrypt (o DNS é manual).
	domains, err := client.ListDomains(ctx, options.project, options.name)
	if err != nil {
		report.add("listar domínios", "falhou", err)
		return report
	}
	if domainExists(domains, options.domain) {
		report.add("domínio "+options.domain, "já existia", nil)
	} else {
		err := client.CreateDomain(ctx, options.project, options.name, options.domain, options.port)
		report.add("domínio "+options.domain, "criado", err)
		if err != nil {
			return report
		}
	}

	// 5. Deploy.
	err = client.DeployAppService(ctx, options.project, options.name)
	report.add("deploy", "disparado", err)
	if err != nil {
		return report
	}

	// 6. Etiqueta de expiração da demo (ARQUITETURA-DEPLOY.md §9).
	if !expiresAt.IsZero() {
		err := client.SetServiceNotes(ctx, options.project, options.name, easypanel.EncodeNotes(expiresAt))
		report.add("etiqueta de expiração "+easypanel.ExpiryDate(expiresAt), "gravada", err)
	}

	return report
}

// instanceEnv monta as variáveis da instância. O banco é SQLite no volume
// (DATABASE_URL vazia): a instância só troca de dialeto por decisão explícita.
func instanceEnv(options createOptions) string {
	vars := map[string]string{
		"APP_ENV":        "production",
		"HOST":           "0.0.0.0",
		"PORT":           fmt.Sprint(options.port),
		"TZ":             "America/Sao_Paulo",
		"LOG_FORMAT":     "json",
		"DB_DIR":         options.dbDir,
		"SESSION_SECRET": options.sessionSecret,
		"SEED_USERNAME":  options.seedUsername,
		"SEED_PASSWORD":  options.seedPassword,
		"SEED_DEMO":      fmt.Sprint(options.seedDemo),
		"APP_URL":        "https://" + options.domain,
	}
	for _, key := range passthroughEnv {
		if value := os.Getenv(key); value != "" {
			vars[key] = value
		}
	}
	return easypanel.EnvLines(vars)
}

// appServiceExists informa se o serviço já está no projeto.
func appServiceExists(services []easypanel.AppServiceRef, project, name string) bool {
	for _, service := range services {
		if service.Project == project && service.Service == name {
			return true
		}
	}
	return false
}

// mountExists informa se já há um ponto de montagem no caminho.
func mountExists(mounts []easypanel.Mount, path string) bool {
	for _, mount := range mounts {
		if mount.MountPath == path {
			return true
		}
	}
	return false
}

// domainExists informa se o host já está publicado.
func domainExists(domains []easypanel.Domain, host string) bool {
	for _, domain := range domains {
		if domain.Host == host {
			return true
		}
	}
	return false
}

// printCreateReport descreve o que foi criado, o que já existia e o próximo
// passo manual (o DNS).
func printCreateReport(out io.Writer, options createOptions, expiresAt time.Time, report *provisionReport) {
	fmt.Fprintf(out, "projeto:   %s\n", options.project)
	fmt.Fprintf(out, "instância: %s\n", options.name)
	fmt.Fprintf(out, "imagem:    %s\n", options.image)
	fmt.Fprintf(out, "volume:    %s%s em %s\n", options.name, volumeSuffix, options.dbDir)
	fmt.Fprintf(out, "domínio:   https://%s\n", options.domain)
	fmt.Fprintf(out, "demo:      %s\n", yesNo(options.seedDemo))
	if !expiresAt.IsZero() {
		fmt.Fprintf(out, "expira em: %s\n", easypanel.ExpiryDate(expiresAt))
	}
	fmt.Fprintln(out)

	for _, step := range report.Steps {
		if step.Err != nil {
			fmt.Fprintf(out, "  [%-10s] %s: %v\n", "falhou", step.Label, step.Err)
			continue
		}
		fmt.Fprintf(out, "  [%-10s] %s\n", step.Status, step.Label)
	}

	if report.err != nil {
		fmt.Fprintf(out, "\nprovisionamento interrompido: %v\n", report.err)
		return
	}
	fmt.Fprintf(out, "\npróximo passo: aponte o DNS de %s para a VPS (registro manual).\n", options.domain)
}

// ── list ───────────────────────────────────────────────────────────

// runList lista as instâncias do projeto com a etiqueta de expiração.
func runList(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	project := flags.String("project", "", "projeto no painel")
	if err := flags.Parse(args); err != nil {
		return err
	}

	client, err := newClient()
	if err != nil {
		return err
	}

	instances, err := listInstances(ctx, client, projectName(*project))
	if err != nil {
		return err
	}

	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "SERVIÇO\tEXPIRA\tSITUAÇÃO")
	now := time.Now()
	for _, instance := range instances {
		expiry, situation := "-", "sem prazo"
		if !instance.expiresAt.IsZero() {
			expiry = easypanel.ExpiryDate(instance.expiresAt)
			situation = "vigente"
			if easypanel.Expired(instance.notes, now, 0) {
				situation = "vencida"
			}
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\n", instance.service, expiry, situation)
	}
	return writer.Flush()
}

// instance é uma instância do projeto com a etiqueta já lida.
type instance struct {
	service   string
	notes     string
	expiresAt time.Time
}

// listInstances lê os serviços de aplicação do projeto e a etiqueta de cada um.
func listInstances(ctx context.Context, client *easypanel.Client, project string) ([]instance, error) {
	services, err := client.ListAppServices(ctx)
	if err != nil {
		return nil, err
	}

	instances := make([]instance, 0, len(services))
	for _, service := range services {
		if service.Project != project || (service.Type != "" && service.Type != "app") {
			continue
		}
		notes, err := client.GetServiceNotes(ctx, project, service.Service)
		if err != nil {
			return nil, fmt.Errorf("ler etiqueta de %s: %w", service.Service, err)
		}
		expiresAt, _ := easypanel.ParseNotes(notes)
		instances = append(instances, instance{service: service.Service, notes: notes, expiresAt: expiresAt})
	}
	return instances, nil
}

// ── expire ─────────────────────────────────────────────────────────

// runExpire remove as demos vencidas e relata o que removeu.
func runExpire(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("expire", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	project := flags.String("project", "", "projeto no painel")
	days := flags.Int("days", 0, "antecipa a limpeza para quem vence em N dias")
	expiresAt := flags.String("expires-at", "", "remove o que vence até esta data (AAAA-MM-DD)")
	dryRun := flags.Bool("dry-run", false, "lista o que seria removido, sem remover")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *days < 0 {
		return errors.New("--days não pode ser negativo")
	}
	if *expiresAt != "" && *days != 0 {
		return errors.New("use --days ou --expires-at, não os dois")
	}

	horizon := time.Duration(*days) * 24 * time.Hour
	if *expiresAt != "" {
		deadline, err := easypanel.ParseExpiry(*expiresAt, time.Now())
		if err != nil {
			return err
		}
		// A data informada vale até o fim do dia: uma demo que vence hoje entra
		// na limpeza pedida para hoje.
		horizon = deadline.AddDate(0, 0, 1).Sub(time.Now().UTC()) - time.Second
	}

	client, err := newClient()
	if err != nil {
		return err
	}

	instances, err := listInstances(ctx, client, projectName(*project))
	if err != nil {
		return err
	}

	now := time.Now()
	removed, due := 0, 0
	var failures []error
	for _, instance := range instances {
		if !easypanel.Expired(instance.notes, now, horizon) {
			continue
		}
		due++
		if *dryRun {
			fmt.Fprintf(out, "removeria %s (expira em %s)\n", instance.service, easypanel.ExpiryDate(instance.expiresAt))
			continue
		}
		if err := client.DeleteAppService(ctx, projectName(*project), instance.service); err != nil {
			failures = append(failures, fmt.Errorf("remover %s: %w", instance.service, err))
			fmt.Fprintf(out, "falhou ao remover %s: %v\n", instance.service, err)
			continue
		}
		removed++
		fmt.Fprintf(out, "removido %s (app e volume)\n", instance.service)
	}

	if *dryRun {
		fmt.Fprintf(out, "\n%d instância(s) vencida(s); nada foi removido (--dry-run).\n", due)
		return nil
	}
	fmt.Fprintf(out, "\n%d instância(s) removida(s).\n", removed)
	return errors.Join(failures...)
}

// ── Apoio ──────────────────────────────────────────────────────────

// newSecret gera o segredo de sessão das instâncias criadas sem um informado.
func newSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("gerar segredo de sessão: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// firstNonEmpty devolve o primeiro valor não vazio.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// yesNo formata um booleano para o relatório.
func yesNo(value bool) string {
	if value {
		return "sim"
	}
	return "não"
}
