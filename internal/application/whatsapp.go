package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Canal de WhatsApp (WAHA). Os casos de uso resolvem quem recebe a mensagem, o
// que ela diz e o que fica registrado; a conversa com o WAHA e o estado do
// pareamento são infraestrutura (internal/infrastructure/whatsapp), que
// implementa as portas declaradas aqui.
//
// Esta é a superfície de texto do canal: normalização de telefone, link direto
// e as mensagens do recibo e do contrato. As portas existem para que o caso de
// uso monte a mensagem sem depender da infraestrutura (a regra de dependência
// do port proíbe application → infrastructure); os textos literais vivem em
// internal/infrastructure/whatsapp/messages.go.
type WhatsAppMessages interface {
	SanitizePhone(raw string) string
	DirectURLWithMessage(phone, message string) string
	Receipt(message WhatsAppReceiptMessage) string
	Contract(message WhatsAppContractMessage) string
}

// WhatsAppReceiptMessage reúne o que a mensagem do recibo mostra. Os valores já
// vêm formatados para exibição: datas em dd/mm/aaaa e dinheiro em Real.
type WhatsAppReceiptMessage struct {
	TenantName   string
	PropertyName string
	DueDate      string
	Amount       string
	PaidAmount   string
	// PaidAmountRaw alimenta a decisão de pagamento parcial; nil sem baixa.
	PaidAmountRaw *float64
	AmountRaw     float64
	Category      string
	Description   string
	// PaidAt é a data da baixa já formatada (dd/mm/aaaa HH:mm).
	PaidAt        string
	PDFURL        string
	ValidationURL string
	LandlordName  string
}

// WhatsAppContractMessage reúne o que a mensagem do contrato mostra.
type WhatsAppContractMessage struct {
	TenantName    string
	PropertyName  string
	StartDate     string
	EndDate       string
	DueDay        int
	RentAmount    string
	PDFURL        string
	ValidationURL string
	LandlordName  string
}

// Tipos de documento aceitos em /api/whatsapp/preview.
const (
	WhatsAppKindReceipt = "receipt"
	WhatsAppKindLease   = "lease"
)

// Etapas observáveis do envio, com os mesmos valores que o painel já conhece.
type WhatsAppStage string

const (
	StagePreparing   WhatsAppStage = "preparing"
	StageConnecting  WhatsAppStage = "connecting"
	StageOpeningChat WhatsAppStage = "opening_chat"
	StageSending     WhatsAppStage = "sending"
	StageConfirming  WhatsAppStage = "confirming"
	StageDone        WhatsAppStage = "done"
)

// Desfechos do job de envio.
type WhatsAppJobState string

const (
	JobRunning WhatsAppJobState = "running"
	JobDone    WhatsAppJobState = "done"
	JobError   WhatsAppJobState = "error"
)

// WhatsAppSendResult é o resultado do envio concluído.
type WhatsAppSendResult struct {
	Success   bool   `json:"success"`
	To        string `json:"to"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Error     string `json:"error,omitempty"`
}

// WhatsAppJob é o acompanhamento de um envio — o JSON do app atual, campo a campo.
type WhatsAppJob struct {
	ID        string              `json:"id"`
	UserID    string              `json:"userId"`
	Status    WhatsAppJobState    `json:"status"`
	Stage     WhatsAppStage       `json:"stage"`
	Result    *WhatsAppSendResult `json:"result,omitempty"`
	Error     string              `json:"error,omitempty"`
	CreatedAt int64               `json:"createdAt"`
	UpdatedAt int64               `json:"updatedAt"`
}

// WhatsAppChannel é o canal de envio: entrega a mensagem e mantém o registro dos
// envios em andamento (memória, TTL de 5 minutos). Implementado por
// internal/infrastructure/whatsapp.
type WhatsAppChannel interface {
	// SendText envia o texto ao telefone informado e devolve o id da mensagem no
	// WhatsApp. O identificador de conversa é resolvido pelo canal.
	SendText(ctx context.Context, phone, text string) (string, error)
	// Create abre o job do envio do administrador informado.
	Create(userID string) WhatsAppJob
	// Stage publica a etapa atual do envio.
	Stage(jobID string, stage WhatsAppStage)
	// Complete encerra o job com o resultado.
	Complete(jobID string, result WhatsAppSendResult)
	// Fail encerra o job com a razão da falha.
	Fail(jobID, reason string)
	// Get devolve o job do administrador informado (job de outro dono não é
	// consultável).
	Get(jobID, userID string) (WhatsAppJob, bool)
}

// IncomingMessage é uma mensagem que chegou pelo canal, já interpretada pelo
// webhook do WAHA.
type IncomingMessage struct {
	// Event é o evento do WAHA que trouxe a mensagem (`message`/`message.any`).
	Event string
	// Session é a sessão do WAHA que recebeu a mensagem.
	Session string
	// ID é o id da mensagem no WhatsApp (idempotência do webhook).
	ID string
	// ChatID é a conversa no formato do WAHA (`5511999999999@c.us`).
	ChatID string
	// Phone é o telefone do outro lado, só dígitos.
	Phone string
	// ContactName é o nome exibido do contato, quando informado.
	ContactName string
	// Body é o texto da mensagem.
	Body string
	// Timestamp é o instante registrado pelo WhatsApp (zero quando ausente).
	Timestamp time.Time
	// FromMe é true no eco de uma mensagem que nós mesmos enviamos.
	FromMe bool
	// HasMedia informa se a mensagem traz anexo.
	HasMedia bool
}

// WhatsAppPreviewPayload é a prévia do envio: o mesmo payload de
// /api/whatsapp/preview no app atual. O nome difere do caso de uso
// (`WhatsAppPreview`) porque Go não permite tipo e função com o mesmo nome.
type WhatsAppPreviewPayload struct {
	Type           string `json:"type"`
	ID             string `json:"id"`
	Phone          string `json:"phone"`
	FormattedPhone string `json:"formattedPhone"`
	TenantName     string `json:"tenantName"`
	PropertyName   string `json:"propertyName"`
	Message        string `json:"message"`
	DirectURL      string `json:"directUrl"`
}

// WhatsAppPreview resolve o destinatário, monta a mensagem e devolve os links do
// documento (PDF e validação de autenticidade). Recibo ainda não pago não é
// liberado: o documento só existe depois da baixa.
func WhatsAppPreview(ctx context.Context, deps Deps, kind, id string) (*WhatsAppPreviewPayload, error) {
	if strings.TrimSpace(id) == "" || (kind != WhatsAppKindReceipt && kind != WhatsAppKindLease) {
		return nil, domain.Invalid("Parâmetros inválidos.")
	}
	if deps.Messages == nil {
		return nil, errors.New("superfície de texto do WhatsApp não configurada")
	}

	if kind == WhatsAppKindReceipt {
		return receiptPreview(ctx, deps, id)
	}
	return leasePreview(ctx, deps, id)
}

func receiptPreview(ctx context.Context, deps Deps, id string) (*WhatsAppPreviewPayload, error) {
	receipt, err := deps.Repos.Receipts.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if domain.Str(receipt.PaidAt) == "" {
		return nil, domain.Invalid("Recibo ainda não pago.")
	}

	tenant, err := receiptTenant(ctx, deps, receipt)
	if err != nil {
		return nil, err
	}
	phone := domain.Str(tenant.Phone)

	base := ResolvePublicBaseURL(ctx, deps, "")
	pdfURL, validationURL, err := whatsAppDocumentLinks(deps, base, WhatsAppKindReceipt, receipt.ID)
	if err != nil {
		return nil, err
	}

	tenantName := receipt.TenantName
	if tenantName == "" {
		tenantName = "Locatário"
	}
	message := deps.Messages.Receipt(WhatsAppReceiptMessage{
		TenantName:    tenantName,
		PropertyName:  receipt.PropertyName,
		DueDate:       format.FormatDateBR(receipt.DueDate),
		Amount:        format.FormatCurrency(receipt.Amount),
		PaidAmount:    paidAmountLabel(receipt),
		PaidAmountRaw: receipt.PaidAmount,
		AmountRaw:     receipt.Amount,
		Category:      string(receipt.Category),
		Description:   domain.Str(receipt.Description),
		PaidAt:        format.FormatDateTimeBR(domain.Str(receipt.PaidAt)),
		PDFURL:        pdfURL,
		ValidationURL: validationURL,
		LandlordName:  landlordName(ctx, deps),
	})

	return &WhatsAppPreviewPayload{
		Type:           WhatsAppKindReceipt,
		ID:             receipt.ID,
		Phone:          phone,
		FormattedPhone: deps.Messages.SanitizePhone(phone),
		TenantName:     receipt.TenantName,
		PropertyName:   receipt.PropertyName,
		Message:        message,
		DirectURL:      deps.Messages.DirectURLWithMessage(phone, message),
	}, nil
}

func leasePreview(ctx context.Context, deps Deps, id string) (*WhatsAppPreviewPayload, error) {
	lease, err := deps.Repos.Leases.ByID(ctx, id)
	if err != nil {
		return nil, err
	}

	tenant, err := tenantByID(ctx, deps, lease.TenantID)
	if err != nil {
		return nil, err
	}
	phone := domain.Str(tenant.Phone)

	base := ResolvePublicBaseURL(ctx, deps, "")
	pdfURL, validationURL, err := whatsAppDocumentLinks(deps, base, WhatsAppKindLease, lease.ID)
	if err != nil {
		return nil, err
	}

	message := deps.Messages.Contract(WhatsAppContractMessage{
		TenantName:    lease.TenantName,
		PropertyName:  lease.PropertyName,
		StartDate:     format.FormatDateBR(lease.StartDate),
		EndDate:       format.FormatDateBR(lease.EndDate),
		DueDay:        lease.DueDay,
		RentAmount:    format.FormatCurrency(lease.RentAmount),
		PDFURL:        pdfURL,
		ValidationURL: validationURL,
		LandlordName:  landlordName(ctx, deps),
	})

	return &WhatsAppPreviewPayload{
		Type:           WhatsAppKindLease,
		ID:             lease.ID,
		Phone:          phone,
		FormattedPhone: deps.Messages.SanitizePhone(phone),
		TenantName:     lease.TenantName,
		PropertyName:   lease.PropertyName,
		Message:        message,
		DirectURL:      deps.Messages.DirectURLWithMessage(phone, message),
	}, nil
}

// WhatsAppSendRequest é o pedido de envio vindo do painel.
type WhatsAppSendRequest struct {
	// UserID é o administrador dono do job.
	UserID string
	// To é o telefone como o painel o informa.
	To string
	// Message é o texto a enviar.
	Message string
	// Actor é o contexto de auditoria da requisição.
	Actor AuditContext
}

// WhatsAppSendJob abre o job do envio, dispara o envio em segundo plano e
// devolve o `jobId` que o painel acompanha por /api/whatsapp/send/status.
//
// A validação do destinatário acontece antes de abrir o job: erro de número não
// vira job falho, vira 400 na hora — como no app atual.
func WhatsAppSendJob(ctx context.Context, deps Deps, channel WhatsAppChannel, request WhatsAppSendRequest) (string, error) {
	if deps.Messages == nil || channel == nil {
		return "", errors.New("canal do WhatsApp não configurado")
	}

	to := strings.TrimSpace(request.To)
	message := strings.TrimSpace(request.Message)
	if to == "" || message == "" {
		return "", domain.Invalid("Destinatário e mensagem são obrigatórios.")
	}
	phone := deps.Messages.SanitizePhone(to)
	if len(phone) < 12 {
		return "", domain.Invalid("Número de telefone \"%s\" inválido para envio via WhatsApp.", to)
	}

	job := channel.Create(request.UserID)
	go deliverWhatsApp(ctx, deps, channel, request, job.ID, phone, message)
	return job.ID, nil
}

// deliverWhatsApp executa o envio e publica o desfecho no job e na trilha. O
// contexto é próprio: a resposta HTTP já foi enviada quando o WAHA responde.
func deliverWhatsApp(ctx context.Context, deps Deps, channel WhatsAppChannel, request WhatsAppSendRequest, jobID, phone, message string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()

	// O envio pelo WAHA é uma chamada HTTP: não há etapa de "conectar" separada
	// da de transmitir, como havia no robô headless.
	channel.Stage(jobID, StageSending)
	messageID, err := channel.SendText(ctx, phone, message)
	if err != nil {
		reason := err.Error()
		channel.Fail(jobID, reason)
		recordWhatsAppSendFailure(ctx, deps, request, jobID, phone, reason)
		return
	}
	if messageID != "" {
		channel.Stage(jobID, StageConfirming)
	}

	channel.Complete(jobID, WhatsAppSendResult{
		Success:   true,
		To:        phone,
		Message:   message,
		Timestamp: deps.Clock.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
	})
	recordWhatsAppSend(ctx, deps, request, jobID, phone)
}

// WhatsAppJobStatus devolve o job do administrador informado. Job de outro dono
// é indistinguível de job inexistente.
func WhatsAppJobStatus(ctx context.Context, deps Deps, channel WhatsAppChannel, jobID, userID string) (WhatsAppJob, error) {
	if channel == nil {
		return WhatsAppJob{}, errors.New("canal do WhatsApp não configurado")
	}
	job, ok := channel.Get(strings.TrimSpace(jobID), userID)
	if !ok {
		return WhatsAppJob{}, domain.ErrNotFound
	}
	return job, nil
}

// RegisterIncomingMessage persiste a mensagem que chegou pelo webhook e a
// vincula ao locatário pelo telefone (só dígitos). Reentrega do mesmo webhook
// não duplica nada: o id da mensagem no WhatsApp é único.
func RegisterIncomingMessage(ctx context.Context, deps Deps, message IncomingMessage) error {
	if deps.Repos.WhatsApp == nil {
		return errors.New("persistência das mensagens do WhatsApp não configurada")
	}

	phone := format.NormalizeDigits(message.Phone)
	tenant, err := tenantByPhone(ctx, deps, phone)
	if err != nil {
		return err
	}

	direction := domain.WhatsAppDirectionIn
	status := domain.WhatsAppStatusReceived
	if message.FromMe {
		direction = domain.WhatsAppDirectionOut
		status = domain.WhatsAppStatusSent
	}

	stored := &domain.WhatsAppMessage{
		ID:         deps.IDs.New(),
		CreatedAt:  format.FormatUTCStamp(deps.Clock.Now()),
		Direction:  direction,
		ChatID:     message.ChatID,
		Phone:      phone,
		Body:       message.Body,
		Status:     status,
		ExternalID: optionalString(message.ID),
		Metadata:   whatsAppMessageMetadata(message),
	}
	if tenant != nil {
		stored.TenantID = &tenant.ID
	}

	inserted, err := deps.Repos.WhatsApp.InsertIfAbsent(ctx, stored)
	if err != nil {
		return err
	}
	if inserted == 0 {
		// Webhook reentregue pelo WAHA: já está gravado, e auditar de novo
		// duplicaria a linha da trilha.
		return nil
	}

	if direction == domain.WhatsAppDirectionOut {
		// Eco do nosso próprio envio: ele já foi auditado no job de envio.
		return nil
	}

	entityLabel := phone
	if tenant != nil {
		entityLabel = tenant.Name
	}
	return Record(ctx, deps, AuditContext{Origin: domain.AuditOriginPublic}, AuditEntry{
		Action:      domain.ActionWhatsAppMessageReceived,
		Entity:      domain.EntityWhatsApp,
		EntityID:    stored.ID,
		EntityLabel: entityLabel,
		Summary:     fmt.Sprintf("Mensagem recebida pelo WhatsApp de %s", entityLabel),
		Metadata: map[string]any{
			"phone":       phone,
			"messageId":   message.ID,
			"hasMedia":    message.HasMedia,
			"tenantId":    domain.Str(stored.TenantID),
			"whatsappAt":  timeLabel(message.Timestamp),
			"bodyPreview": truncate(message.Body, 160),
		},
	})
}

// ── Auxiliares ─────────────────────────────────────────────────────

// whatsAppDocumentLinks monta o link do PDF e o link de validação de
// autenticidade do documento, com o token HMAC da instância.
func whatsAppDocumentLinks(deps Deps, base, kind, id string) (pdfURL, validationURL string, err error) {
	if deps.Signer == nil {
		return "", "", errors.New("assinador dos tokens de autenticidade não configurado")
	}
	token := deps.Signer.Generate(kind, id)
	validationURL = deps.Signer.ValidationURL(base, kind, id, token)
	resource := "/pdf"
	if kind == WhatsAppKindLease {
		resource = "/contract-pdf"
	}
	pdfURL = fmt.Sprintf("%s%s?id=%s&token=%s", base, resource, id, token)
	return pdfURL, validationURL, nil
}

// landlordName devolve o nome do locador configurado na instância.
func landlordName(ctx context.Context, deps Deps) string {
	value, err := SettingValue(ctx, deps, domain.SettingLandlordName)
	if err != nil {
		deps.Log.Warn("não foi possível ler o nome do locador", "scope", "whatsapp", "err", err)
		return ""
	}
	return value
}

// paidAmountLabel formata o valor pago, ou devolve string vazia sem baixa.
func paidAmountLabel(receipt *domain.Receipt) string {
	if receipt.PaidAmount == nil {
		return ""
	}
	return format.FormatCurrency(*receipt.PaidAmount)
}

// receiptTenant resolve o titular do recibo: o contrato manda, e o próprio
// lançamento é a alternativa dos avulsos — o mesmo coalesce da consulta do app.
func receiptTenant(ctx context.Context, deps Deps, receipt *domain.Receipt) (*domain.Tenant, error) {
	tenantID := domain.Str(receipt.TenantID)
	if receipt.LeaseID != nil {
		lease, err := deps.Repos.Leases.ByID(ctx, *receipt.LeaseID)
		switch {
		case err == nil:
			tenantID = lease.TenantID
		case !errors.Is(err, domain.ErrNotFound):
			return nil, err
		}
	}
	return tenantByID(ctx, deps, tenantID)
}

// tenantByID busca o locatário; id vazio (ou já removido) devolve nil, porque o
// lançamento pode ter perdido o titular.
func tenantByID(ctx context.Context, deps Deps, id string) (*domain.Tenant, error) {
	if id == "" {
		return nil, nil
	}
	tenant, err := deps.Repos.Tenants.ByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return tenant, nil
}

// tenantByPhone acha o locatário dono do telefone, comparando só os dígitos: o
// WhatsApp manda o número com DDI e o cadastro costuma ter só DDD + número.
func tenantByPhone(ctx context.Context, deps Deps, phone string) (*domain.Tenant, error) {
	if len(phone) < 10 {
		return nil, nil
	}
	tenants, err := deps.Repos.Tenants.List(ctx)
	if err != nil {
		return nil, err
	}
	for index := range tenants {
		if phoneMatches(format.NormalizeDigits(domain.Str(tenants[index].Phone)), phone) {
			return &tenants[index], nil
		}
	}
	return nil, nil
}

// phoneMatches compara dois telefones já reduzidos a dígitos, aceitando a
// presença do DDI de um lado só. Exige tamanho mínimo para não casar por acaso.
func phoneMatches(candidate, phone string) bool {
	if candidate == "" {
		return false
	}
	if candidate == phone {
		return true
	}
	if len(candidate) < 10 || len(phone) < 10 {
		return false
	}
	return strings.HasSuffix(phone, candidate) || strings.HasSuffix(candidate, phone)
}

// whatsAppMessageMetadata monta o JSON gravado em `whatsapp_messages.metadata`.
func whatsAppMessageMetadata(message IncomingMessage) *string {
	payload, err := json.Marshal(map[string]any{
		"event":       message.Event,
		"session":     message.Session,
		"contactName": message.ContactName,
		"fromMe":      message.FromMe,
		"hasMedia":    message.HasMedia,
		"whatsappAt":  timeLabel(message.Timestamp),
	})
	if err != nil {
		return nil
	}
	encoded := string(payload)
	return &encoded
}

// timeLabel formata o instante do WhatsApp, ou string vazia quando ausente.
func timeLabel(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}

func recordWhatsAppSend(ctx context.Context, deps Deps, request WhatsAppSendRequest, jobID, phone string) {
	if err := Record(ctx, deps, request.Actor, AuditEntry{
		Action:      domain.ActionWhatsAppSend,
		Entity:      domain.EntityWhatsApp,
		EntityID:    jobID,
		EntityLabel: phone,
		Summary:     fmt.Sprintf("Mensagem enviada para %s pelo WhatsApp", phone),
		Metadata:    map[string]any{"phone": phone, "jobId": jobID},
	}); err != nil {
		deps.Log.Warn("auditoria do envio não gravada", "scope", "whatsapp", "job", jobID, "err", err)
	}
}

func recordWhatsAppSendFailure(ctx context.Context, deps Deps, request WhatsAppSendRequest, jobID, phone, reason string) {
	if err := Record(ctx, deps, request.Actor, AuditEntry{
		Action:      domain.ActionWhatsAppSendFailed,
		Entity:      domain.EntityWhatsApp,
		EntityID:    jobID,
		EntityLabel: phone,
		Summary:     fmt.Sprintf("Falha ao enviar mensagem para %s pelo WhatsApp", phone),
		Metadata: map[string]any{
			"phone":  phone,
			"jobId":  jobID,
			"reason": truncate(reason, 300),
		},
	}); err != nil {
		deps.Log.Warn("auditoria da falha de envio não gravada", "scope", "whatsapp", "job", jobID, "err", err)
	}
}

// truncate corta o texto no limite informado (o app cortava a razão da falha
// em 300 caracteres antes de auditá-la).
func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
