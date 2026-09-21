package domain

import (
	"context"
	"time"
)

// ── Portas de repositório ──────────────────────────────────────────
//
// As interfaces são implementadas duas vezes (SQLite e Postgres) em
// internal/infrastructure/db. A aplicação depende apenas destas portas.

// UserRepository lê e escreve administradores.
type UserRepository interface {
	ByID(ctx context.Context, id string) (*User, error)
	ByUsername(ctx context.Context, username string) (*User, error)
	Create(ctx context.Context, user *User) error
	UpdateUsername(ctx context.Context, id, username string) error
	UpdatePassword(ctx context.Context, id, passwordHash, changedAt string) error
}

// TenantRepository lê e escreve locatários.
type TenantRepository interface {
	List(ctx context.Context) ([]Tenant, error)
	ByID(ctx context.Context, id string) (*Tenant, error)
	ByCPF(ctx context.Context, digits string) (*Tenant, error)
	Create(ctx context.Context, tenant *Tenant) error
	Update(ctx context.Context, tenant *Tenant) error
	UpdatePassword(ctx context.Context, id, passwordHash, changedAt, updatedAt string) error
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int, error)
}

// PropertyRepository lê e escreve imóveis.
type PropertyRepository interface {
	List(ctx context.Context) ([]Property, error)
	ByID(ctx context.Context, id string) (*Property, error)
	Create(ctx context.Context, property *Property) error
	Update(ctx context.Context, property *Property) error
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int, error)
}

// LeaseRepository lê e escreve contratos.
type LeaseRepository interface {
	List(ctx context.Context) ([]Lease, error)
	ByID(ctx context.Context, id string) (*Lease, error)
	ByTenant(ctx context.Context, tenantID string) ([]Lease, error)
	ByProperty(ctx context.Context, propertyID string) ([]Lease, error)
	// Overlapping devolve contratos ativos ou de temporada do imóvel que
	// intersectam o período informado, ignorando excludeID (vazio = nenhum).
	Overlapping(ctx context.Context, propertyID, excludeID, startDate, endDate string) ([]Lease, error)
	// ActiveInRange devolve contratos ativos vigentes em algum ponto do período.
	ActiveInRange(ctx context.Context, startDate, endDate string) ([]Lease, error)
	Create(ctx context.Context, lease *Lease) error
	Update(ctx context.Context, lease *Lease) error
	UpdateStatus(ctx context.Context, id string, status LeaseStatus, updatedAt string) error
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int, error)
	CountByStatus(ctx context.Context, status LeaseStatus) (int, error)
}

// ReceiptRepository lê e escreve lançamentos.
type ReceiptRepository interface {
	List(ctx context.Context) ([]Receipt, error)
	ByID(ctx context.Context, id string) (*Receipt, error)
	ByTenant(ctx context.Context, tenantID string) ([]Receipt, error)
	ByLease(ctx context.Context, leaseID string) ([]Receipt, error)
	Create(ctx context.Context, receipt *Receipt) error
	// CreateIfAbsent insere ignorando conflito na chave de geração mensal e
	// devolve quantas linhas foram inseridas (0 = já existia).
	CreateIfAbsent(ctx context.Context, receipt *Receipt) (int64, error)
	Update(ctx context.Context, receipt *Receipt) error
	UpdatePayment(ctx context.Context, id string, paidAt string, paidAmount float64, updatedAt string) error
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int, error)
}

// InspectionRepository lê e escreve os autos de vistoria.
type InspectionRepository interface {
	List(ctx context.Context) ([]Inspection, error)
	ByID(ctx context.Context, id string) (*Inspection, error)
	ByLease(ctx context.Context, leaseID string) ([]Inspection, error)
	Create(ctx context.Context, inspection *Inspection) error
	Update(ctx context.Context, inspection *Inspection) error
	Delete(ctx context.Context, id string) error
}

// SettingRepository lê e escreve configurações.
type SettingRepository interface {
	Get(ctx context.Context, key string) (string, error)
	List(ctx context.Context) (map[string]string, error)
	Set(ctx context.Context, key, value string) error
}

// AuditFilter são os filtros aceitos na consulta da trilha.
type AuditFilter struct {
	Entity string
	// EntityID recorta a trilha de um registro específico (usado pela régua de
	// cobrança para saber o último estágio enviado de um lançamento).
	EntityID   string
	Action     string
	Actor      string
	From       string
	To         string
	Search     string
	Descending bool
	Limit      int
	Offset     int
}

// AuditRepository lê e escreve a trilha de auditoria (somente-anexada).
type AuditRepository interface {
	Insert(ctx context.Context, entry *AuditLog) error
	List(ctx context.Context, filter AuditFilter) ([]AuditLog, error)
	Count(ctx context.Context, filter AuditFilter) (int, error)
	Actors(ctx context.Context) ([]string, error)
}

// WhatsAppMessageRepository lê e escreve as mensagens trocadas pelo WhatsApp
// (tabela `whatsapp_messages`).
type WhatsAppMessageRepository interface {
	// InsertIfAbsent grava a mensagem e devolve quantas linhas entraram: zero
	// quando o `external_id` já existe, o que torna idempotente a reentrega do
	// webhook pelo WAHA.
	InsertIfAbsent(ctx context.Context, message *WhatsAppMessage) (int64, error)
	// ByTenant devolve as mensagens do locatário, da mais recente para a mais antiga.
	ByTenant(ctx context.Context, tenantID string, limit int) ([]WhatsAppMessage, error)
}

// JobRepository é a fila de trabalho assíncrono persistida.
type JobRepository interface {
	Create(ctx context.Context, job *Job) error
	ByID(ctx context.Context, id string) (*Job, error)
	ListByStatus(ctx context.Context, status JobStatus, limit int) ([]Job, error)
	List(ctx context.Context, limit int) ([]Job, error)
	Update(ctx context.Context, job *Job) error
}

// PaymentProofRepository guarda os comprovantes lidos e a decisão tomada.
type PaymentProofRepository interface {
	Create(ctx context.Context, proof *PaymentProof) error
	ByID(ctx context.Context, id string) (*PaymentProof, error)
	ListByStatus(ctx context.Context, status PaymentProofStatus, limit int) ([]PaymentProof, error)
	List(ctx context.Context, limit int) ([]PaymentProof, error)
	ByReceipt(ctx context.Context, receiptID string) ([]PaymentProof, error)
	UpdateDecision(ctx context.Context, proof *PaymentProof) error
}

// DocumentIntakeRepository guarda os cadastros propostos a partir de documento.
type DocumentIntakeRepository interface {
	Create(ctx context.Context, intake *DocumentIntake) error
	ByID(ctx context.Context, id string) (*DocumentIntake, error)
	List(ctx context.Context, limit int) ([]DocumentIntake, error)
	ListByStatus(ctx context.Context, status IntakeStatus, limit int) ([]DocumentIntake, error)
	Update(ctx context.Context, intake *DocumentIntake) error
}

// ── Portas de serviço ──────────────────────────────────────────────

// Clock permite injetar o tempo em testes.
type Clock interface {
	Now() time.Time
}

// IDGenerator gera identificadores únicos (UUIDv7).
type IDGenerator interface {
	New() string
}

// PasswordHasher deriva e compara hashes de senha.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Compare(hash, password string) error
}
