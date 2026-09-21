package domain

import (
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain/format"
)

// Fila de trabalho assíncrono (tabela `jobs`). Existe para que leitura de
// documento, envio e cobrança rodem fora do caminho da requisição HTTP.

// JobKind é o tipo de trabalho enfileirado.
type JobKind string

const (
	JobKindPaymentProof   JobKind = "payment-proof"
	JobKindDocumentIntake JobKind = "document-intake"
	JobKindCollection     JobKind = "collection"
	JobKindWhatsAppSend   JobKind = "whatsapp-send"
)

// JobStatus é o estado do trabalho na fila.
type JobStatus string

const (
	JobStatusQueued  JobStatus = "queued"
	JobStatusRunning JobStatus = "running"
	JobStatusDone    JobStatus = "done"
	JobStatusFailed  JobStatus = "failed"
)

// JobStatusLabel devolve o rótulo em português do estado do trabalho.
func JobStatusLabel(status JobStatus) string {
	switch status {
	case JobStatusQueued:
		return "Na fila"
	case JobStatusRunning:
		return "Em execução"
	case JobStatusDone:
		return "Concluído"
	case JobStatusFailed:
		return "Falhou"
	default:
		return string(status)
	}
}

// Job é uma unidade de trabalho persistida.
type Job struct {
	ID          string
	CreatedAt   string
	UpdatedAt   string
	Kind        JobKind
	Status      JobStatus
	Attempts    int
	MaxAttempts int
	Payload     *string
	Result      *string
	Error       *string
	ScheduledAt string
	StartedAt   *string
	FinishedAt  *string
}

// CanRetry informa se o trabalho ainda pode ser tentado de novo.
func (j Job) CanRetry() bool {
	return j.Attempts < j.MaxAttempts
}

// PaymentProofSource é de onde o comprovante veio.
type PaymentProofSource string

const (
	PaymentProofSourceWhatsApp PaymentProofSource = "whatsapp"
	PaymentProofSourcePortal   PaymentProofSource = "portal"
	PaymentProofSourcePanel    PaymentProofSource = "panel"
)

// PaymentProofStatus é a situação do comprovante na esteira de decisão.
type PaymentProofStatus string

const (
	PaymentProofStatusPending  PaymentProofStatus = "pending"
	PaymentProofStatusAccepted PaymentProofStatus = "accepted"
	PaymentProofStatusReview   PaymentProofStatus = "review"
	PaymentProofStatusRejected PaymentProofStatus = "rejected"
)

// PaymentProofStatusLabel devolve o rótulo em português da situação.
func PaymentProofStatusLabel(status PaymentProofStatus) string {
	switch status {
	case PaymentProofStatusPending:
		return "Lendo"
	case PaymentProofStatusAccepted:
		return "Aceito"
	case PaymentProofStatusReview:
		return "Em revisão"
	case PaymentProofStatusRejected:
		return "Recusado"
	default:
		return string(status)
	}
}

// PaymentDecision é quem decidiu a baixa.
type PaymentDecision string

const (
	PaymentDecisionPending PaymentDecision = "pending"
	PaymentDecisionAuto    PaymentDecision = "auto"
	PaymentDecisionManual  PaymentDecision = "manual"
)

// PaymentDecisionLabel devolve o rótulo em português da origem da decisão.
func PaymentDecisionLabel(decision PaymentDecision) string {
	switch decision {
	case PaymentDecisionAuto:
		return "Automática"
	case PaymentDecisionManual:
		return "Manual"
	default:
		return "Pendente"
	}
}

// PaymentExtraction é o que a leitura do documento devolveu.
type PaymentExtraction struct {
	Amount     float64
	Date       string
	Payer      string
	Payee      string
	Identifier string
	Confidence float64
}

// PaymentProof é um comprovante recebido, com o que foi lido e o que foi decidido.
type PaymentProof struct {
	ID                  string
	CreatedAt           string
	UpdatedAt           string
	Source              PaymentProofSource
	Phone               *string
	TenantID            *string
	ReceiptID           *string
	FileName            *string
	FilePath            *string
	Mime                *string
	ExtractedAmount     *float64
	ExtractedDate       *string
	ExtractedPayer      *string
	ExtractedPayee      *string
	ExtractedIdentifier *string
	Confidence          *float64
	Status              PaymentProofStatus
	Decision            PaymentDecision
	DecisionReason      *string
	DecidedBy           *string
	DecidedAt           *string
	RawResponse         *string
}

// Extraction devolve a extração como valor.
func (p PaymentProof) Extraction() PaymentExtraction {
	return PaymentExtraction{
		Amount:     Float(p.ExtractedAmount),
		Date:       Str(p.ExtractedDate),
		Payer:      Str(p.ExtractedPayer),
		Payee:      Str(p.ExtractedPayee),
		Identifier: Str(p.ExtractedIdentifier),
		Confidence: Float(p.Confidence),
	}
}

// IntakeKind é o tipo de documento aceito no onboarding.
type IntakeKind string

const (
	IntakeKindLease        IntakeKind = "lease"
	IntakeKindIDDocument   IntakeKind = "id-document"
	IntakeKindAddressProof IntakeKind = "address-proof"
)

// IntakeKindLabel devolve o rótulo em português do tipo de documento.
func IntakeKindLabel(kind IntakeKind) string {
	switch kind {
	case IntakeKindLease:
		return "Contrato antigo"
	case IntakeKindIDDocument:
		return "Documento de identidade"
	case IntakeKindAddressProof:
		return "Comprovante de residência"
	default:
		return string(kind)
	}
}

// Valid informa se o tipo de documento é aceito.
func (k IntakeKind) Valid() bool {
	switch k {
	case IntakeKindLease, IntakeKindIDDocument, IntakeKindAddressProof:
		return true
	default:
		return false
	}
}

// IntakeStatus é a situação do cadastro proposto.
type IntakeStatus string

const (
	IntakeStatusPending   IntakeStatus = "pending"
	IntakeStatusConfirmed IntakeStatus = "confirmed"
	IntakeStatusDiscarded IntakeStatus = "discarded"
)

// IntakeStatusLabel devolve o rótulo em português da situação.
func IntakeStatusLabel(status IntakeStatus) string {
	switch status {
	case IntakeStatusPending:
		return "Aguardando conferência"
	case IntakeStatusConfirmed:
		return "Confirmado"
	case IntakeStatusDiscarded:
		return "Descartado"
	default:
		return string(status)
	}
}

// LeaseIntakeFields são os campos de contrato lidos de um documento.
type LeaseIntakeFields struct {
	TenantName    string  `json:"tenantName"`
	TenantCPF     string  `json:"tenantCpf"`
	TenantEmail   string  `json:"tenantEmail"`
	TenantPhone   string  `json:"tenantPhone"`
	PropertyName  string  `json:"propertyName"`
	PropertyCity  string  `json:"propertyCity"`
	PropertyState string  `json:"propertyState"`
	StartDate     string  `json:"startDate"`
	EndDate       string  `json:"endDate"`
	DueDay        int     `json:"dueDay"`
	RentAmount    float64 `json:"rentAmount"`
	AdjustIndex   string  `json:"adjustIndex"`
	Guarantee     string  `json:"guarantee"`
	Confidence    float64 `json:"confidence"`
}

// PersonIntakeFields são os campos de pessoa lidos de um documento.
type PersonIntakeFields struct {
	Name       string  `json:"name"`
	CPF        string  `json:"cpf"`
	Email      string  `json:"email"`
	Phone      string  `json:"phone"`
	Address    string  `json:"address"`
	City       string  `json:"city"`
	State      string  `json:"state"`
	Zip        string  `json:"zip"`
	Confidence float64 `json:"confidence"`
}

// DocumentIntake é um cadastro proposto a partir de documento, sempre sujeito a
// conferência humana antes de virar registro.
type DocumentIntake struct {
	ID              string
	CreatedAt       string
	UpdatedAt       string
	Kind            IntakeKind
	FileName        *string
	FilePath        *string
	Mime            *string
	Status          IntakeStatus
	Extracted       *string
	Confidence      *float64
	CreatedEntity   *string
	CreatedEntityID *string
	RawResponse     *string
	Notes           *string
}

// PaymentMatchInput reúne tudo que a decisão determinística precisa olhar.
type PaymentMatchInput struct {
	Extracted     PaymentExtraction
	Receipt       *Receipt
	LandlordName  string
	LandlordCPF   string
	Today         time.Time
	MinConfidence float64
	WindowDays    int
}

// PaymentMatchResult é o veredito da decisão determinística.
type PaymentMatchResult struct {
	Approved bool
	Reason   string
}

// EvaluatePaymentMatch decide se o comprovante pode dar baixa automática.
//
// A regra é deliberadamente conservadora: qualquer dúvida (confiança baixa,
// valor diferente, destinatário que não é o locador, data fora da janela ou
// ausência de recibo aberto) manda para revisão humana. A IA só lê o documento;
// quem decide é este código.
func EvaluatePaymentMatch(input PaymentMatchInput) PaymentMatchResult {
	if input.MinConfidence > 0 && input.Extracted.Confidence < input.MinConfidence {
		return PaymentMatchResult{Reason: "confiança da leitura abaixo do limite"}
	}
	if input.Receipt == nil {
		return PaymentMatchResult{Reason: "nenhum lançamento em aberto para este titular"}
	}
	if input.Receipt.PaidAt != nil && *input.Receipt.PaidAt != "" {
		return PaymentMatchResult{Reason: "lançamento já estava quitado"}
	}
	if input.Extracted.Amount <= 0 {
		return PaymentMatchResult{Reason: "valor não identificado no comprovante"}
	}
	if !amountsMatch(input.Extracted.Amount, input.Receipt.Amount) {
		return PaymentMatchResult{Reason: "valor do comprovante diferente do lançamento"}
	}
	if !payeeMatchesLandlord(input.Extracted.Payee, input.LandlordName, input.LandlordCPF) {
		return PaymentMatchResult{Reason: "destinatário do comprovante não é o locador"}
	}
	if !dateInsideWindow(input.Extracted.Date, input.Receipt.DueDate, input.Today, input.WindowDays) {
		return PaymentMatchResult{Reason: "data do comprovante fora da janela do lançamento"}
	}
	return PaymentMatchResult{Approved: true, Reason: "valor, destinatário e data conferem"}
}

func amountsMatch(left, right float64) bool {
	difference := left - right
	if difference < 0 {
		difference = -difference
	}
	return difference < 0.01
}

// payeeMatchesLandlord aceita o nome do locador (comparação sem caixa, sem
// acento e por partes) ou o CPF em dígitos.
func payeeMatchesLandlord(payee, landlordName, landlordCPF string) bool {
	payee = strings.TrimSpace(payee)
	if payee == "" {
		return false
	}
	if digits := format.NormalizeDigits(payee); digits != "" && landlordCPF != "" && digits == format.NormalizeDigits(landlordCPF) {
		return true
	}
	normalizedPayee := normalizeWords(payee)
	normalizedLandlord := normalizeWords(landlordName)
	if normalizedLandlord == "" {
		return false
	}
	if strings.Contains(normalizedPayee, normalizedLandlord) || strings.Contains(normalizedLandlord, normalizedPayee) {
		return true
	}
	// Nome composto: aceita quando o primeiro e o último nome do locador aparecem.
	parts := strings.Fields(normalizedLandlord)
	if len(parts) >= 2 {
		return strings.Contains(normalizedPayee, parts[0]) && strings.Contains(normalizedPayee, parts[len(parts)-1])
	}
	return false
}

func normalizeWords(value string) string {
	replacer := strings.NewReplacer(
		"á", "a", "à", "a", "ã", "a", "â", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "õ", "o", "ô", "o", "ö", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u",
		"ç", "c", "ñ", "n",
	)
	return strings.ToLower(replacer.Replace(strings.TrimSpace(value)))
}

// dateInsideWindow aceita a data do comprovante dentro de uma janela em torno do
// vencimento e nunca no futuro distante.
func dateInsideWindow(date, dueDate string, today time.Time, windowDays int) bool {
	parsed, err := time.ParseInLocation("2006-01-02", firstDatePart(strings.TrimSpace(date)), today.Location())
	if err != nil {
		return false
	}
	if windowDays <= 0 {
		windowDays = 10
	}
	if parsed.After(today.AddDate(0, 0, 1)) {
		return false
	}
	due, err := time.ParseInLocation("2006-01-02", firstDatePart(strings.TrimSpace(dueDate)), today.Location())
	if err != nil {
		return true
	}
	earliest := due.AddDate(0, 0, -windowDays)
	latest := due.AddDate(0, 0, windowDays)
	return !parsed.Before(earliest) && !parsed.After(latest)
}
