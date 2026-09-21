package db

import (
	"context"
	"database/sql"
	"os"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db/postgres"
	"github.com/passoz/aluxen/internal/infrastructure/db/sqlite"
)

// Store é o contêiner do banco: o dialeto em uso, a conexão aberta e os
// repositórios de domínio implementados por ele. Quem consome o banco depende
// apenas das portas — o composition root monta application.Repositories a partir
// daqui.
type Store struct {
	Dialect Dialect

	Users         domain.UserRepository
	Tenants       domain.TenantRepository
	Properties    domain.PropertyRepository
	Leases        domain.LeaseRepository
	Receipts      domain.ReceiptRepository
	Settings      domain.SettingRepository
	Audit         domain.AuditRepository
	WhatsApp      domain.WhatsAppMessageRepository
	Inspections   domain.InspectionRepository
	Jobs          domain.JobRepository
	PaymentProofs domain.PaymentProofRepository
	Intakes       domain.DocumentIntakeRepository

	db   *sql.DB
	path string
}

// newStore monta os repositórios do dialeto sobre a conexão aberta.
func newStore(dialect Dialect, sqlDB *sql.DB, target string) *Store {
	store := &Store{Dialect: dialect, db: sqlDB}
	if dialect == DialectPostgres {
		store.path = "" // não há arquivo no Postgres
		store.Users = postgres.NewUsers(sqlDB)
		store.Tenants = postgres.NewTenants(sqlDB)
		store.Properties = postgres.NewProperties(sqlDB)
		store.Leases = postgres.NewLeases(sqlDB)
		store.Receipts = postgres.NewReceipts(sqlDB)
		store.Settings = postgres.NewSettings(sqlDB)
		store.Audit = postgres.NewAudit(sqlDB)
		store.WhatsApp = postgres.NewWhatsAppMessages(sqlDB)
		store.Inspections = postgres.NewInspections(sqlDB)
		store.Jobs = postgres.NewJobs(sqlDB)
		store.PaymentProofs = postgres.NewPaymentProofs(sqlDB)
		store.Intakes = postgres.NewDocumentIntakes(sqlDB)
		return store
	}

	store.path = target
	store.Users = sqlite.NewUsers(sqlDB)
	store.Tenants = sqlite.NewTenants(sqlDB)
	store.Properties = sqlite.NewProperties(sqlDB)
	store.Leases = sqlite.NewLeases(sqlDB)
	store.Receipts = sqlite.NewReceipts(sqlDB)
	store.Settings = sqlite.NewSettings(sqlDB)
	store.Audit = sqlite.NewAudit(sqlDB)
	store.WhatsApp = sqlite.NewWhatsAppMessages(sqlDB)
	store.Inspections = sqlite.NewInspections(sqlDB)
	store.Jobs = sqlite.NewJobs(sqlDB)
	store.PaymentProofs = sqlite.NewPaymentProofs(sqlDB)
	store.Intakes = sqlite.NewDocumentIntakes(sqlDB)
	return store
}

// Migrate aplica as migrações pendentes do dialeto da conexão.
func (s *Store) Migrate(ctx context.Context) error {
	return runMigrations(ctx, s.db, s.Dialect)
}

// Ping confirma que o banco responde.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Close fecha a conexão.
func (s *Store) Close() error {
	return s.db.Close()
}

// Path devolve o caminho do arquivo SQLite (vazio no Postgres).
func (s *Store) Path() string {
	return s.path
}

// SizeBytes devolve o tamanho do arquivo SQLite em bytes (zero no Postgres).
// É o mesmo número que o app atual mostra em /api/info.
func (s *Store) SizeBytes() int64 {
	if s.path == "" {
		return 0
	}
	info, err := os.Stat(s.path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// DB devolve a conexão crua para as rotinas de infraestrutura que precisam dela
// (testes de paridade, CLI de migração SQLite → Postgres). Os casos de uso nunca
// devem usar isto: eles falam com as portas de domínio.
func (s *Store) DB() *sql.DB {
	return s.db
}
