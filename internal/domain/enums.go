package domain

// Enums persistidos como texto. Os valores literais são os mesmos do app atual
// (colunas `leases.status` e `receipts.category`), sem renomear nada.

// LeaseStatus é a situação do contrato (`leases.status`).
type LeaseStatus string

const (
	LeaseStatusActive     LeaseStatus = "active"
	LeaseStatusSuspended  LeaseStatus = "suspended"
	LeaseStatusSeason     LeaseStatus = "season"
	LeaseStatusTerminated LeaseStatus = "terminated"
)

// LeaseStatusLabel devolve o rótulo em português usado na UI e na auditoria.
func LeaseStatusLabel(status LeaseStatus) string {
	switch status {
	case LeaseStatusActive:
		return "Ativo"
	case LeaseStatusSuspended:
		return "Suspenso"
	case LeaseStatusSeason:
		return "Temporada"
	case LeaseStatusTerminated:
		return "Encerrado"
	default:
		return string(status)
	}
}

// BlocksOverlap informa se o status ocupa o imóvel (bloqueia sobreposição de
// período). Suspenso e encerrado não bloqueiam.
func (s LeaseStatus) BlocksOverlap() bool {
	return s == LeaseStatusActive || s == LeaseStatusSeason
}

// SelectableOnCreate informa se o status pode ser escolhido na criação.
func (s LeaseStatus) SelectableOnCreate() bool {
	return s == LeaseStatusActive || s == LeaseStatusSuspended || s == LeaseStatusSeason
}

// SelectableOnUpdate informa se o status pode ser escolhido na edição.
func (s LeaseStatus) SelectableOnUpdate() bool {
	return s.SelectableOnCreate() || s == LeaseStatusTerminated
}

// ReceiptCategory é a categoria do lançamento (`receipts.category`).
type ReceiptCategory string

const (
	ReceiptCategoryRent ReceiptCategory = "rent"
	// ReceiptCategoryExtra é a cobrança avulsa genérica.
	ReceiptCategoryExtra ReceiptCategory = "extra"
	// ReceiptCategoryCleaning é a taxa de limpeza da temporada.
	ReceiptCategoryCleaning ReceiptCategory = "cleaning"
	// ReceiptCategoryConsumables são os consumíveis da temporada.
	ReceiptCategoryConsumables ReceiptCategory = "consumables"
	// ReceiptCategoryDepositRefund registra a devolução de caução na trilha
	// financeira (o recibo em PDF é emitido a partir dela).
	ReceiptCategoryDepositRefund ReceiptCategory = "deposit-refund"
)

// ReceiptCategoryLabel devolve o rótulo em português da categoria.
func ReceiptCategoryLabel(category ReceiptCategory) string {
	switch category {
	case ReceiptCategoryRent:
		return "Aluguel"
	case ReceiptCategoryExtra:
		return "Avulsa"
	case ReceiptCategoryCleaning:
		return "Taxa de limpeza"
	case ReceiptCategoryConsumables:
		return "Consumíveis"
	case ReceiptCategoryDepositRefund:
		return "Devolução de caução"
	default:
		return string(category)
	}
}

// ReceiptStatus é o status derivado (nunca persistido) do lançamento.
type ReceiptStatus string

const (
	ReceiptStatusPaid    ReceiptStatus = "paid"
	ReceiptStatusPartial ReceiptStatus = "partial"
	ReceiptStatusDue     ReceiptStatus = "due"
	ReceiptStatusOverdue ReceiptStatus = "overdue"
)

// ReceiptStatusLabel devolve o rótulo em português do status derivado.
func ReceiptStatusLabel(status ReceiptStatus) string {
	switch status {
	case ReceiptStatusPaid:
		return "Pago"
	case ReceiptStatusPartial:
		return "Parcial"
	case ReceiptStatusOverdue:
		return "Vencido"
	default:
		return "A vencer"
	}
}

// AuditOrigin é a origem do evento de auditoria (`audit_logs.origin`).
type AuditOrigin string

const (
	AuditOriginWeb    AuditOrigin = "web"
	AuditOriginPublic AuditOrigin = "public"
	AuditOriginClient AuditOrigin = "client"
	AuditOriginJob    AuditOrigin = "job"
)

// WhatsAppDirection é o sentido da mensagem (`whatsapp_messages.direction`).
type WhatsAppDirection string

const (
	// WhatsAppDirectionIn é a mensagem que o locatário mandou para nós.
	WhatsAppDirectionIn WhatsAppDirection = "in"
	// WhatsAppDirectionOut é a mensagem que nós mandamos (eco do nosso envio).
	WhatsAppDirectionOut WhatsAppDirection = "out"
)

// Situações gravadas em `whatsapp_messages.status`.
const (
	WhatsAppStatusReceived = "received"
	WhatsAppStatusSent     = "sent"
)

// WhatsAppDirectionLabel devolve o rótulo em português do sentido da mensagem.
func WhatsAppDirectionLabel(direction WhatsAppDirection) string {
	switch direction {
	case WhatsAppDirectionIn:
		return "Recebida"
	case WhatsAppDirectionOut:
		return "Enviada"
	default:
		return string(direction)
	}
}

// AuditOriginLabel devolve o rótulo em português da origem.
func AuditOriginLabel(origin AuditOrigin) string {
	switch origin {
	case AuditOriginWeb:
		return "Painel"
	case AuditOriginPublic:
		return "Link público"
	case AuditOriginClient:
		return "Área do Cliente"
	case AuditOriginJob:
		return "Rotina automática"
	default:
		return string(origin)
	}
}
