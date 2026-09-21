package domain

// Entidades do domínio. Campos anuláveis no banco são ponteiros aqui, para que a
// ausência (NULL) seja distinguível da string vazia.

// User é o administrador do sistema (tabela `users`).
type User struct {
	ID                string
	Username          string
	PasswordHash      string
	PasswordChangedAt *string
	CreatedAt         string
}

// MustChangePassword informa se o administrador ainda usa a senha semeada e
// portanto está travado até trocá-la.
func (u User) MustChangePassword() bool {
	return u.PasswordChangedAt == nil || *u.PasswordChangedAt == ""
}

// Tenant é o locatário (tabela `tenants`). PasswordHash nulo significa que ele
// ainda usa a senha padrão (o próprio CPF, sem pontuação).
type Tenant struct {
	ID                string
	Name              string
	Email             string
	Phone             *string
	CPF               *string
	PasswordHash      *string
	PasswordChangedAt *string
	CreatedAt         string
	UpdatedAt         string
}

// MustChangePassword informa se o locatário ainda usa a senha padrão.
func (t Tenant) MustChangePassword() bool {
	return t.PasswordChangedAt == nil || *t.PasswordChangedAt == ""
}

// Property é o imóvel (tabela `properties`).
type Property struct {
	ID        string
	Name      string
	Address   string
	City      string
	State     string
	Zip       *string
	CreatedAt string
	UpdatedAt string
}

// Lease é o contrato de locação (tabela `leases`). TenantName e PropertyName
// vêm do join e são usados em listagens, PDFs e auditoria.
type Lease struct {
	ID           string
	TenantID     string
	PropertyID   string
	StartDate    string
	EndDate      string
	DueDay       int
	RentAmount   float64
	Status       LeaseStatus
	AuthToken    *string
	CreatedAt    string
	UpdatedAt    string
	TenantName   string
	PropertyName string
}

// Receipt é o lançamento financeiro (tabela `receipts`). Recibo de aluguel tem
// LeaseID; lançamento avulso pode não ter contrato.
type Receipt struct {
	ID           string
	LeaseID      *string
	TenantID     *string
	PropertyID   *string
	Category     ReceiptCategory
	Description  *string
	DueDate      string
	PaidAt       *string
	Amount       float64
	PaidAmount   *float64
	PdfPath      *string
	GeneratedKey *string
	AuthToken    *string
	CreatedAt    string
	UpdatedAt    *string
	TenantName   string
	PropertyName string
}

// Setting é a configuração chave-valor (tabela `settings`).
type Setting struct {
	Key   string
	Value string
}

// Chaves de configuração usadas pelo sistema.
const (
	SettingLandlordName    = "landlord_name"
	SettingLandlordCPF     = "landlord_cpf"
	SettingLandlordEmail   = "landlord_email"
	SettingLandlordAddress = "landlord_address"
	SettingAppURL          = "app_url"
)

// AuditLog é uma linha da trilha de auditoria (tabela `audit_logs`).
type AuditLog struct {
	ID          string
	CreatedAt   string
	ActorID     *string
	ActorName   *string
	Action      string
	Entity      string
	EntityID    *string
	EntityLabel *string
	Summary     string
	Changes     *string
	Metadata    *string
	Origin      AuditOrigin
	IP          *string
	UserAgent   *string
	RequestID   *string
}

// StrPtr devolve um ponteiro para s (auxiliar de montagem de entidades).
func StrPtr(s string) *string { return &s }

// FloatPtr devolve um ponteiro para f.
func FloatPtr(f float64) *float64 { return &f }

// Str devolve o valor apontado por p, ou a string vazia se p for nulo.
func Str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Float devolve o valor apontado por p, ou zero se p for nulo.
func Float(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
