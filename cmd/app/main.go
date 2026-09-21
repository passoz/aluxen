// Command app é o composition root do Aluxen: o único lugar que conhece as
// implementações concretas (banco, relógio, hasher, HTTP).
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/ai"
	"github.com/passoz/aluxen/internal/infrastructure/authz"
	"github.com/passoz/aluxen/internal/infrastructure/buildinfo"
	"github.com/passoz/aluxen/internal/infrastructure/config"
	"github.com/passoz/aluxen/internal/infrastructure/db"
	"github.com/passoz/aluxen/internal/infrastructure/logging"
	"github.com/passoz/aluxen/internal/infrastructure/platform"
	"github.com/passoz/aluxen/internal/infrastructure/storage"
	"github.com/passoz/aluxen/internal/infrastructure/web"
	"github.com/passoz/aluxen/internal/infrastructure/whatsapp"
)

func main() {
	// Modo de verificação de saúde: a imagem é distroless (sem shell nem curl),
	// então o próprio binário consulta /healthz quando chamado com --healthcheck.
	if len(os.Args) > 1 && os.Args[1] == "--healthcheck" {
		if err := healthcheck(); err != nil {
			os.Stderr.WriteString("healthcheck: " + err.Error() + "\n")
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		os.Stderr.WriteString("falha ao iniciar: " + err.Error() + "\n")
		os.Exit(1)
	}
}

// healthcheck consulta a própria instância e devolve erro quando não responde.
func healthcheck() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port)) + "/healthz")
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", response.StatusCode)
	}
	return nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := logging.New(cfg)
	build := buildinfo.Get()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := db.Open(ctx, cfg.DatabaseURL, cfg.DBDir)
	if err != nil {
		return fmt.Errorf("abrir banco: %w", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Error("falha ao fechar banco", "err", err)
		}
	}()

	if err := store.EnsureAdmin(ctx, cfg.SeedUsername, cfg.SeedPassword, cfg.DefaultLandlordName, cfg.DefaultLandlordCPF); err != nil {
		return fmt.Errorf("semear administrador: %w", err)
	}
	if cfg.SeedDemo {
		if err := db.SeedDemo(ctx, store); err != nil {
			return fmt.Errorf("semear demonstração: %w", err)
		}
	}

	deps := application.Deps{
		Repos: application.Repositories{
			Users:         store.Users,
			Tenants:       store.Tenants,
			Properties:    store.Properties,
			Leases:        store.Leases,
			Receipts:      store.Receipts,
			Settings:      store.Settings,
			Audit:         store.Audit,
			WhatsApp:      store.WhatsApp,
			Inspections:   store.Inspections,
			Jobs:          store.Jobs,
			PaymentProofs: store.PaymentProofs,
			Intakes:       store.Intakes,
		},
		Clock:  platform.SystemClock{},
		IDs:    platform.UUIDv7{},
		Hasher: platform.BcryptHasher{},
		Signer: authz.NewTokenGenerator(cfg.SessionSecret),
		Log:    logger,
		Opts: application.Options{
			PublicBaseURL:      firstNonEmpty(cfg.AppURL, cfg.BaseURL),
			SeedUsername:       cfg.SeedUsername,
			RateLimitMax:       cfg.RateLimitMax,
			RateLimitGlobalMax: cfg.RateLimitGlobalMax,
			LoginLockThreshold: cfg.LoginLockThreshold,
		},
	}

	renderer, err := web.NewRenderer()
	if err != nil {
		return err
	}

	attachments, err := storage.NewFileSystem(resolveStorageDir(cfg))
	if err != nil {
		return fmt.Errorf("preparar armazenamento de anexos: %w", err)
	}
	deps.Attachments = attachments

	// Leitura assistida por IA: uma instância sem provedor configurado continua
	// subindo; o que depende de IA cai na revisão humana com motivo.
	if cfg.AIAPIURL != "" && cfg.AIAPIKey != "" {
		reader := ai.NewClient(ai.Config{
			BaseURL: cfg.AIAPIURL,
			APIKey:  cfg.AIAPIKey,
			Model:   cfg.AIModel,
		})
		deps.Proofs = reader
		deps.Intakes = reader
		deps.Chat = reader
		deps.Photos = reader
		deps.Opts.AIEnabled = true
	}
	deps.Opts.AIConfidenceThreshold = cfg.AIConfidenceThreshold

	worker := application.NewWorker(deps, application.WorkerOptions{})
	worker.Register(domain.JobKindPaymentProof, application.ProcessPaymentProofJob)
	worker.Register(domain.JobKindDocumentIntake, application.ProcessDocumentIntakeJob)
	worker.Register(domain.JobKindWhatsAppSend, application.ProcessWhatsAppSendJob)

	whatsappRuntime := whatsapp.NewRuntime(
		whatsapp.ClientConfig{BaseURL: cfg.WahaAPIURL, APIKey: cfg.WahaAPIKey, Session: cfg.WahaSession},
		deps.Clock, deps.IDs, logger, whatsapp.LoginConfig{},
	)
	deps.Messages = whatsapp.Messages{}
	// O canal de saída também serve aos trabalhos enfileirados (confirmação de
	// quitação), não só ao envio disparado pelo painel.
	deps.Channel = whatsappRuntime

	started := time.Now()
	gate := application.NewLoginGate(deps.Clock, cfg.RateLimitMax, cfg.RateLimitGlobalMax, cfg.LoginLockThreshold)
	common := &web.Common{
		Cfg:     cfg,
		Log:     logger,
		Render:  renderer,
		Session: web.NewSessionManager(cfg.SessionSecret, cfg.SecureCookies()),
		CSRF:    web.NewCSRF(cfg.SessionSecret, cfg.SecureCookies()),
		Deps:    deps,
		Build:   build,
		Started: started,
		Dialect: string(store.Dialect),
		DBPath:  store.Path(),
		DBSize:  store.SizeBytes,
		Ping:    store.Ping,
		MustChangePasswordFunc: func(ctx context.Context, userID string) (bool, error) {
			return application.AdminMustChangePassword(ctx, deps, userID)
		},
		LoginGate: gate,
		WhatsApp:  whatsappRuntime,
	}

	server := &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Handler:           web.NewHandlers(common).Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	if recovered, err := worker.RecoverOrphans(ctx); err != nil {
		logger.Warn("não foi possível recuperar jobs órfãos", "err", err)
	} else if recovered > 0 {
		logger.Info("jobs órfãos devolvidos para a fila", "count", recovered)
	}
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		worker.Run(ctx)
	}()

	logger.Info("servidor iniciado",
		"addr", server.Addr, "dialect", store.Dialect, "database", store.Path(),
		"version", build.Version, "env", cfg.AppEnv)

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("falha ao escutar", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("encerramento não completou", "err", err)
		_ = server.Close()
	}
	// O worker drena o job em execução antes de sair (o cancelamento do contexto
	// já aconteceu em stop()).
	select {
	case <-workerDone:
	case <-time.After(20 * time.Second):
		logger.Warn("worker não encerrou no tempo esperado")
	}
	logger.Info("servidor encerrado")
	return nil
}

// resolveStorageDir decide onde ficam os anexos: STORAGE_DIR quando informado,
// senão <DB_DIR>/storage (volume da instância), senão ./storage.
func resolveStorageDir(cfg *config.Config) string {
	if cfg.StorageDir != "" {
		return cfg.StorageDir
	}
	if cfg.DBDir != "" {
		return filepath.Join(cfg.DBDir, "storage")
	}
	return "storage"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
