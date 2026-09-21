// Package application contém os casos de uso do Aluxen. Cada caso de uso é uma
// função sem estado que recebe as dependências explicitamente (`Deps`), usa
// apenas as portas do domínio e devolve entidades ou erros de domínio.
//
// Nenhuma regra de negócio vive em handler HTTP; nenhum SQL vive aqui.
package application

import (
	"context"
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
	// WhatsApp guarda as mensagens trocadas pelo canal (webhook do WAHA).
	WhatsApp domain.WhatsAppMessageRepository
	// Inspections guarda os autos de vistoria de entrada e saída (temporada).
	Inspections domain.InspectionRepository
	// Jobs é a fila de trabalho assíncrono (leitura de documento, envio, cobrança).
	Jobs domain.JobRepository
	// PaymentProofs guarda comprovantes lidos e a decisão sobre eles.
	PaymentProofs domain.PaymentProofRepository
	// Intakes guarda cadastros propostos a partir de documento.
	Intakes domain.DocumentIntakeRepository
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
	// AIConfidenceThreshold separa a baixa automática da revisão humana.
	AIConfidenceThreshold float64
	// AIEnabled informa se o provedor de IA está configurado nesta instância.
	AIEnabled bool
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

// DocumentFile é o arquivo enviado para leitura assistida.
type DocumentFile struct {
	Bytes    []byte
	Mime     string
	FileName string
}

// ProofExtractor lê comprovantes de pagamento. A implementação concreta fala com
// o provedor de IA; o caso de uso só conhece este contrato.
type ProofExtractor interface {
	ExtractPaymentProof(ctx context.Context, doc DocumentFile) (domain.PaymentExtraction, string, error)
}

// IntakeExtractor lê contratos antigos e documentos pessoais.
type IntakeExtractor interface {
	ExtractLeaseDocument(ctx context.Context, doc DocumentFile) (domain.LeaseIntakeFields, string, error)
	ExtractPersonDocument(ctx context.Context, doc DocumentFile) (domain.PersonIntakeFields, string, error)
}

// Deps são as dependências injetadas em todos os casos de uso.
type Deps struct {
	Repos  Repositories
	Clock  domain.Clock
	IDs    domain.IDGenerator
	Hasher domain.PasswordHasher
	Signer TokenSigner
	// Messages monta os textos do canal do WhatsApp (textos literais do app
	// atual). É um valor sem estado; a implementação vive em
	// internal/infrastructure/whatsapp.
	Messages WhatsAppMessages
	// Attachments guarda fotos de vistoria e documentos enviados.
	Attachments AttachmentStore
	// Proofs lê comprovantes de pagamento (IA). Nulo desliga a leitura.
	Proofs ProofExtractor
	// Intakes lê contratos antigos e documentos pessoais (IA).
	Intakes IntakeExtractor
	// Chat conversa com o provedor usando ferramentas tipadas (agente de
	// atendimento e cobrança). Nulo desliga o agente.
	Chat ChatClient
	Log     *slog.Logger
	Opts    Options
}
