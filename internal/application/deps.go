// Package application contém os casos de uso do Aluxen. Cada caso de uso é uma
// função sem estado que recebe as dependências explicitamente (`Deps`), usa
// apenas as portas do domínio e devolve entidades ou erros de domínio.
//
// Nenhuma regra de negócio vive em handler HTTP; nenhum SQL vive aqui.
package application

import (
	"log/slog"

	"github.com/passoz/aluxen/internal/domain"
)

// Repositories agrupa as portas de persistência usadas pelos casos de uso.
type Repositories struct {
	Users      domain.UserRepository
	Tenants    domain.TenantRepository
	Properties domain.PropertyRepository
	Leases     domain.LeaseRepository
	Receipts   domain.ReceiptRepository
	Settings   domain.SettingRepository
	Audit      domain.AuditRepository
}

// Options são as configurações de produto que os casos de uso precisam conhecer.
type Options struct {
	// PublicBaseURL é a URL pública configurada na instância (`APP_URL`/`BASE_URL`).
	PublicBaseURL string
	// SeedUsername é o usuário administrador semeado (usado no login).
	SeedUsername string
	// RateLimitMax, RateLimitGlobalMax e LoginLockThreshold reproduzem os limites
	// do app atual para o gate de login.
	RateLimitMax        int
	RateLimitGlobalMax  int
	LoginLockThreshold  int
	MaxBackupImportSize int64
}

// TokenSigner emite os tokens de autenticidade dos documentos. O port fica aqui
// (e não no pacote de infraestrutura) para que casos de uso de áreas diferentes
// — documentos e WhatsApp, por exemplo — dependam do mesmo contrato.
//
// `kind` é `receipt` ou `lease`, os mesmos valores usados no HMAC do app atual.
type TokenSigner interface {
	Generate(kind, id string) string
	FormatCode(token string) string
	ValidationURL(base, kind, id, token string) string
}

// Deps são as dependências injetadas em todos os casos de uso.
type Deps struct {
	Repos  Repositories
	Clock  domain.Clock
	IDs    domain.IDGenerator
	Hasher domain.PasswordHasher
	Signer TokenSigner
	Log    *slog.Logger
	Opts   Options
}
