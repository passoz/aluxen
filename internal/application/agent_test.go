package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/domain"
)

// Atendimento automático e régua de cobrança são exercitados com dublês do
// provedor de IA, do canal e da trilha: nenhum teste fala com o WAHA nem com o
// provedor, e o dado que o teste confere vem do recibo de mentira.
//
// Os dublês levam o prefixo `ag` (atendimento) e o motivo é o de sempre: o
// pacote já tem os dublês `wa` do canal do WhatsApp e os `rcpt` do painel.

const (
	agTenantID      = "tenant-1"
	agOtherTenantID = "tenant-2"
	agReceiptID     = "receipt-1"
	agOtherReceipt  = "receipt-2"
	agStamp         = "2026-09-21 12:00:00"

	agPhone      = "(11) 99999-9999"
	agOtherPhone = "(21) 98888-7777"
)

// agToday é o "hoje" dos testes: os atrasos são contados a partir dele.
func agToday() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }

// ── Dublês ─────────────────────────────────────────────────────────

// agReceipts guarda os lançamentos do teste.
type agReceipts struct {
	domain.ReceiptRepository
	receipts []domain.Receipt
}

func (r *agReceipts) List(context.Context) ([]domain.Receipt, error) { return r.receipts, nil }

func (r *agReceipts) ByID(_ context.Context, id string) (*domain.Receipt, error) {
	for index := range r.receipts {
		if r.receipts[index].ID == id {
			return &r.receipts[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *agReceipts) ByTenant(_ context.Context, tenantID string) ([]domain.Receipt, error) {
	found := make([]domain.Receipt, 0, len(r.receipts))
	for index := range r.receipts {
		if domain.Str(r.receipts[index].TenantID) == tenantID {
			found = append(found, r.receipts[index])
		}
	}
	return found, nil
}

// agLeases guarda os contratos do teste.
type agLeases struct {
	domain.LeaseRepository
	leases []domain.Lease
}

func (l *agLeases) ByID(_ context.Context, id string) (*domain.Lease, error) {
	for index := range l.leases {
		if l.leases[index].ID == id {
			return &l.leases[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (l *agLeases) ByTenant(_ context.Context, tenantID string) ([]domain.Lease, error) {
	found := make([]domain.Lease, 0, len(l.leases))
	for index := range l.leases {
		if l.leases[index].TenantID == tenantID {
			found = append(found, l.leases[index])
		}
	}
	return found, nil
}

// agMessages guarda as mensagens do canal e devolve as do titular da mais
// recente para a mais antiga — a ordem que a porta promete.
type agMessages struct {
	mu       sync.Mutex
	messages []domain.WhatsAppMessage
}

func (s *agMessages) InsertIfAbsent(_ context.Context, message *domain.WhatsAppMessage) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, *message)
	return 1, nil
}

func (s *agMessages) ByTenant(_ context.Context, tenantID string, limit int) ([]domain.WhatsAppMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := make([]domain.WhatsAppMessage, 0, len(s.messages))
	for _, message := range s.messages {
		if domain.Str(message.TenantID) == tenantID {
			found = append(found, message)
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].CreatedAt > found[j].CreatedAt })
	if limit > 0 && len(found) > limit {
		found = found[:limit]
	}
	return found, nil
}

func (s *agMessages) stored() []domain.WhatsAppMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.WhatsAppMessage(nil), s.messages...)
}

// agChat é o dublê do provedor de IA: devolve as respostas combinadas, na ordem,
// e guarda cada pedido — é assim que o teste confere o que o modelo recebeu.
type agChat struct {
	mu        sync.Mutex
	responses []ChatResponse
	requests  []ChatRequest
}

func (c *agChat) Complete(_ context.Context, request ChatRequest) (ChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, request)
	if len(c.responses) == 0 {
		return ChatResponse{}, errors.New("dublê do provedor sem resposta combinada")
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

func (c *agChat) calls() []ChatRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ChatRequest(nil), c.requests...)
}

// agChannel é o canal de envio: guarda o que saiu e falha sob comando.
type agChannel struct {
	WhatsAppChannel
	mu   sync.Mutex
	sent []agSentMessage
	err  error
}

type agSentMessage struct {
	phone string
	text  string
}

func (c *agChannel) SendText(_ context.Context, phone, text string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return "", c.err
	}
	c.sent = append(c.sent, agSentMessage{phone: phone, text: text})
	return "wamid-1", nil
}

func (c *agChannel) sentMessages() []agSentMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]agSentMessage(nil), c.sent...)
}

// ── Montagem ──────────────────────────────────────────────────────

type agFixture struct {
	deps     Deps
	audit    *waAudit
	messages *agMessages
	receipts *agReceipts
	leases   *agLeases
	tenants  *waTenants
	chat     *agChat
}

func newAgentFixture(t *testing.T) *agFixture {
	t.Helper()

	audit := &waAudit{}
	messages := &agMessages{}
	receipts := &agReceipts{}
	leases := &agLeases{}
	tenants := &waTenants{}
	chat := &agChat{}

	deps := Deps{
		Repos: Repositories{
			Tenants:  tenants,
			Receipts: receipts,
			Leases:   leases,
			Audit:    audit,
			WhatsApp: messages,
			Settings: &waSettings{values: map[string]string{domain.SettingLandlordName: "João Locador"}},
		},
		Clock: waClock{now: agToday()},
		IDs:   &waIDs{},
		Chat:  chat,
		Log:   slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})),
	}
	return &agFixture{deps: deps, audit: audit, messages: messages, receipts: receipts, leases: leases, tenants: tenants, chat: chat}
}

func (f *agFixture) addTenant(tenant domain.Tenant) {
	f.tenants.tenants = append(f.tenants.tenants, tenant)
}

func (f *agFixture) addReceipt(receipt domain.Receipt) {
	f.receipts.receipts = append(f.receipts.receipts, receipt)
}

func (f *agFixture) addLease(lease domain.Lease) {
	f.leases.leases = append(f.leases.leases, lease)
}

func (f *agFixture) addMessage(message domain.WhatsAppMessage) {
	f.messages.messages = append(f.messages.messages, message)
}

// addCollectionSent grava na trilha um envio da régua, como o caso de uso grava.
func (f *agFixture) addCollectionSent(t *testing.T, receiptID string, stage domain.CollectionStage) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"stage":     int(stage),
		"channel":   CollectionChannelWhatsApp,
		"receiptId": receiptID,
	})
	require.NoError(t, err)
	metadata := string(encoded)
	require.NoError(t, f.audit.Insert(context.Background(), &domain.AuditLog{
		ID:          "audit-" + receiptID,
		CreatedAt:   agStamp,
		Action:      domain.ActionCollectionSent,
		Entity:      domain.EntityCollection,
		EntityID:    domain.StrPtr(receiptID),
		Summary:     "Cobrança enviada",
		Metadata:    &metadata,
		Origin:      domain.AuditOriginWeb,
		EntityLabel: domain.StrPtr("Cobrança"),
	}))
}

// auditOf devolve o primeiro evento da ação informada.
func (f *agFixture) auditOf(t *testing.T, action string) domain.AuditLog {
	t.Helper()
	f.audit.mu.Lock()
	entries := append([]domain.AuditLog(nil), f.audit.entries...)
	f.audit.mu.Unlock()
	for _, entry := range entries {
		if entry.Action == action {
			return entry
		}
	}
	t.Fatalf("evento %q não registrado na trilha; ações gravadas: %v", action, auditActionsOf(entries))
	return domain.AuditLog{}
}

func (f *agFixture) auditActions() []string {
	f.audit.mu.Lock()
	defer f.audit.mu.Unlock()
	actions := make([]string, 0, len(f.audit.entries))
	for _, entry := range f.audit.entries {
		actions = append(actions, entry.Action)
	}
	return actions
}

func (f *agFixture) tenant(t *testing.T, id string) *domain.Tenant {
	t.Helper()
	for index := range f.tenants.tenants {
		if f.tenants.tenants[index].ID == id {
			return &f.tenants.tenants[index]
		}
	}
	t.Fatalf("titular %q não está no dublê", id)
	return nil
}

// ── Auxiliares ────────────────────────────────────────────────────

func agTenant(id, name, phone string) domain.Tenant {
	tenant := domain.Tenant{
		ID: id, Name: name, Email: id + "@example.com",
		CreatedAt: agStamp, UpdatedAt: agStamp,
	}
	if phone != "" {
		tenant.Phone = domain.StrPtr(phone)
	}
	return tenant
}

func agReceipt(id, tenantID, dueDate string, amount float64) domain.Receipt {
	return domain.Receipt{
		ID: id, TenantID: domain.StrPtr(tenantID), Category: domain.ReceiptCategoryRent,
		DueDate: dueDate, Amount: amount, CreatedAt: agStamp,
	}
}

func agPaidReceipt(id, tenantID, dueDate string, amount float64) domain.Receipt {
	receipt := agReceipt(id, tenantID, dueDate, amount)
	receipt.PaidAt = domain.StrPtr("2026-08-09T10:00")
	receipt.PaidAmount = domain.FloatPtr(amount)
	return receipt
}

func agIncoming(tenantID *string, phone, body string) domain.WhatsAppMessage {
	message := domain.WhatsAppMessage{
		ID:        "message-1",
		CreatedAt: agStamp,
		Direction: domain.WhatsAppDirectionIn,
		ChatID:    "5511999999999@c.us",
		Phone:     phone,
		Body:      body,
		Status:    domain.WhatsAppStatusReceived,
	}
	if tenantID != nil {
		message.TenantID = tenantID
	}
	return message
}

// agAuditContext é o contexto de auditoria de uma mensagem que chegou pelo
// canal (sem administrador: quem escreveu foi o locatário).
func agAuditContext() AuditContext {
	return AuditContext{Origin: domain.AuditOriginPublic}
}

func agMetadata(t *testing.T, entry domain.AuditLog) map[string]any {
	t.Helper()
	require.NotNil(t, entry.Metadata, "evento sem metadados")
	var fields map[string]any
	require.NoError(t, json.Unmarshal([]byte(*entry.Metadata), &fields))
	return fields
}

func auditActionsOf(entries []domain.AuditLog) []string {
	actions := make([]string, 0, len(entries))
	for _, entry := range entries {
		actions = append(actions, entry.Action)
	}
	return actions
}

func agCandidateIDs(candidates []CollectionCandidate) []string {
	ids := make([]string, 0, len(candidates))
	for index := range candidates {
		ids = append(ids, candidates[index].ReceiptID)
	}
	return ids
}

// ── Ferramentas tipadas ───────────────────────────────────────────

func TestAgentAnswersWithReceiptDataFromTool(t *testing.T) {
	fixture := newAgentFixture(t)
	tenant := agTenant(agTenantID, "Maria Souza", agPhone)
	fixture.addTenant(tenant)
	fixture.addReceipt(agReceipt(agReceiptID, agTenantID, "2026-09-10", 2500))
	fixture.chat.responses = []ChatResponse{
		{ToolCalls: []ChatToolCall{{Name: agentToolOpenReceipts, Arguments: map[string]any{}}}},
		{Message: "Você tem um lançamento em aberto: R$ 2.500,00 com vencimento em 10/09/2026."},
	}

	reply, err := HandleIncomingMessage(context.Background(), fixture.deps, agAuditContext(),
		agIncoming(domain.StrPtr(agTenantID), agPhone, "Quanto eu devo?"))
	require.NoError(t, err)

	assert.False(t, reply.Escalated)
	assert.Equal(t, []string{agentToolOpenReceipts}, reply.Tools)
	assert.Equal(t, 2, reply.Rounds)
	assert.Equal(t, "Você tem um lançamento em aberto: R$ 2.500,00 com vencimento em 10/09/2026.", reply.Reply)

	// O modelo só pôde responder o que a ferramenta devolveu: o segundo pedido
	// leva o valor e o vencimento que estão no banco.
	requests := fixture.chat.calls()
	require.Len(t, requests, 2)
	require.NotEmpty(t, requests[1].Messages)
	grounding := requests[1].Messages[len(requests[1].Messages)-1]
	assert.Contains(t, grounding.Content, agentToolOpenReceipts)
	assert.Contains(t, grounding.Content, "R$ 2.500,00")
	assert.Contains(t, grounding.Content, "10/09/2026")
	assert.Contains(t, requests[0].System, "Maria Souza")

	// A resposta fica registrada na trilha, com o tipo do evento e o telefone
	// mascarado.
	entry := fixture.auditOf(t, domain.ActionAgentAnswered)
	assert.Equal(t, "Maria Souza", domain.Str(entry.EntityLabel))
	metadata := agMetadata(t, entry)
	assert.Equal(t, agentKindAnswer, metadata["kind"])
	assert.Equal(t, "****9999", metadata["phone"])
}

func TestAgentToolContractIsClosedAndTenantScoped(t *testing.T) {
	declarations := agentToolDeclarations()
	names := make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		names = append(names, declaration.Name)
		assert.NotEmpty(t, declaration.Description)
		assert.Equal(t, false, declaration.Parameters["additionalProperties"])
		properties, ok := declaration.Parameters["properties"].(map[string]any)
		require.True(t, ok, "ferramenta %s sem propriedades declaradas", declaration.Name)
		// A barreira do titular: o modelo não escolhe de quem é o dado.
		assert.NotContains(t, properties, "tenantId")
		assert.NotContains(t, properties, "tenantID")
	}
	assert.ElementsMatch(t,
		[]string{agentToolOpenReceipts, agentToolContract, agentToolReceiptStatus, agentToolMaintenance, agentToolEscalate},
		names)
}

func TestAgentRefusesAnotherTenantsReceipt(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.addTenant(agTenant(agOtherTenantID, "Pedro Lima", agOtherPhone))
	fixture.addReceipt(agReceipt(agOtherReceipt, agOtherTenantID, "2026-09-01", 999))
	fixture.chat.responses = []ChatResponse{
		{ToolCalls: []ChatToolCall{{
			Name:      agentToolReceiptStatus,
			Arguments: map[string]any{"receiptId": agOtherReceipt},
		}}},
		{Message: "O lançamento de R$ 999,00 do Pedro está vencido."},
	}

	reply, err := HandleIncomingMessage(context.Background(), fixture.deps, agAuditContext(),
		agIncoming(domain.StrPtr(agTenantID), agPhone, "E o lançamento do Pedro, quanto é?"))
	require.NoError(t, err)

	// Nenhuma ferramenta devolveu dado: a resposta não pode sair do modelo.
	assert.True(t, reply.Escalated)
	assert.Empty(t, reply.Tools)
	assert.NotContains(t, reply.Reply, "999")
	assert.NotContains(t, reply.Reply, "Pedro")

	// O modelo recebeu a recusa, não o dado do outro titular.
	requests := fixture.chat.calls()
	require.Len(t, requests, 2)
	refusal := requests[1].Messages[len(requests[1].Messages)-1]
	assert.Contains(t, refusal.Content, "erro:")
	assert.NotContains(t, refusal.Content, "999")

	// E a ferramenta recusa antes de ler qualquer coisa do lançamento.
	result, err := receiptStatusOf(context.Background(), fixture.deps, fixture.tenant(t, agTenantID), agOtherReceipt)
	require.Error(t, err)
	assert.Empty(t, result.Data)
}

func TestAgentRefusesForeignTenantArgument(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.addTenant(agTenant(agOtherTenantID, "Pedro Lima", agOtherPhone))
	fixture.addReceipt(agReceipt(agReceiptID, agTenantID, "2026-09-01", 2500))

	result, err := runAgentTool(context.Background(), fixture.deps, agAuditContext(), fixture.tenant(t, agTenantID),
		ChatToolCall{Name: agentToolReceiptStatus, Arguments: map[string]any{
			"receiptId": agReceiptID,
			"tenantId":  agOtherTenantID,
		}})
	require.Error(t, err)
	assert.Empty(t, result.Data)
	assert.Contains(t, err.Error(), "tenantId")
}

func TestAgentEscalatesQuestionWithoutApplicableTool(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.chat.responses = []ChatResponse{{Message: "Acho que o valor é em torno de R$ 2.000,00."}}

	reply, err := HandleIncomingMessage(context.Background(), fixture.deps, agAuditContext(),
		agIncoming(domain.StrPtr(agTenantID), agPhone, "Qual a cor da parede da sala?"))
	require.NoError(t, err)

	assert.True(t, reply.Escalated)
	assert.Equal(t, agentReasonUngrounded, reply.Reason)
	assert.Empty(t, reply.Tools)
	assert.NotContains(t, reply.Reply, "2.000") // resposta sem lastro não é publicada
	assert.Contains(t, reply.Reply, "locador")

	entry := fixture.auditOf(t, domain.ActionAgentEscalated)
	metadata := agMetadata(t, entry)
	assert.Equal(t, agentKindEscalation, metadata["kind"])
	assert.Equal(t, agentReasonUngrounded, metadata["reason"])
}

func TestAgentEscalatesLegalQuestionWithoutCallingProvider(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))

	reply, err := HandleIncomingMessage(context.Background(), fixture.deps, agAuditContext(),
		agIncoming(domain.StrPtr(agTenantID), agPhone, "Vou falar com meu advogado: o contrato permite isso?"))
	require.NoError(t, err)

	assert.True(t, reply.Escalated)
	assert.Equal(t, agentReasonLegal, reply.Reason)
	assert.Contains(t, reply.Reply, "locador")
	assert.Empty(t, fixture.chat.calls(), "pergunta jurídica não vai ao provedor")
	assert.Contains(t, fixture.auditActions(), domain.ActionAgentEscalated)
}

func TestAgentEscalatesMessageWithoutTenant(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))

	reply, err := HandleIncomingMessage(context.Background(), fixture.deps, agAuditContext(),
		agIncoming(nil, "5511977776666", "Oi, tudo bem?"))
	require.NoError(t, err)

	assert.True(t, reply.Escalated)
	assert.Equal(t, agentReasonNoTenant, reply.Reason)
	assert.Empty(t, reply.TenantID)
	assert.Empty(t, fixture.chat.calls())
}

func TestAgentEscalatesBlankMessage(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))

	reply, err := HandleIncomingMessage(context.Background(), fixture.deps, agAuditContext(),
		agIncoming(domain.StrPtr(agTenantID), agPhone, "   "))
	require.NoError(t, err)

	assert.True(t, reply.Escalated)
	assert.Equal(t, agentReasonEmpty, reply.Reason)
	assert.Empty(t, fixture.chat.calls())
}

func TestAgentEscalatesWhenProviderIsDisabled(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.deps.Chat = nil

	reply, err := HandleIncomingMessage(context.Background(), fixture.deps, agAuditContext(),
		agIncoming(domain.StrPtr(agTenantID), agPhone, "Quanto eu devo?"))
	require.NoError(t, err)

	assert.True(t, reply.Escalated)
	assert.Equal(t, agentReasonDisabled, reply.Reason)
	assert.Contains(t, reply.Reply, "locador")
}

func TestAgentEscalatesAfterRoundLimit(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.addReceipt(agReceipt(agReceiptID, agTenantID, "2026-09-01", 2500))
	for round := 0; round <= agentMaxRounds; round++ {
		// O dublê sempre pede a mesma ferramenta: o laço não pode ser infinito.
		fixture.chat.responses = append(fixture.chat.responses,
			ChatResponse{ToolCalls: []ChatToolCall{{Name: agentToolOpenReceipts, Arguments: map[string]any{}}}})
	}

	reply, err := HandleIncomingMessage(context.Background(), fixture.deps, agAuditContext(),
		agIncoming(domain.StrPtr(agTenantID), agPhone, "Quanto eu devo?"))
	require.NoError(t, err)

	assert.True(t, reply.Escalated)
	assert.Equal(t, agentReasonRounds, reply.Reason)
	assert.Len(t, fixture.chat.calls(), agentMaxRounds)
}

// ── Manutenção ────────────────────────────────────────────────────

func TestAgentRegistersMaintenanceRequest(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.chat.responses = []ChatResponse{
		{ToolCalls: []ChatToolCall{{
			Name:      agentToolMaintenance,
			Arguments: map[string]any{"description": "O chuveiro do banheiro está vazando."},
		}}},
		{Message: "Registrei sua solicitação de manutenção do chuveiro."},
	}

	reply, err := HandleIncomingMessage(context.Background(), fixture.deps, agAuditContext(),
		agIncoming(domain.StrPtr(agTenantID), agPhone, "O chuveiro quebrou, e agora?"))
	require.NoError(t, err)

	assert.False(t, reply.Escalated)
	assert.Equal(t, []string{agentToolMaintenance}, reply.Tools)

	entry := fixture.auditOf(t, domain.ActionMaintenanceOpened)
	assert.Equal(t, "Maria Souza", domain.Str(entry.EntityLabel))
	metadata := agMetadata(t, entry)
	assert.Equal(t, agentKindMaintenance, metadata["kind"])
	assert.Equal(t, "****9999", metadata["phone"])
	assert.Contains(t, metadata["reason"], "chuveiro")
}

func TestMaintenanceRequestRejectsEmptyDescription(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))

	result, err := runAgentTool(context.Background(), fixture.deps, agAuditContext(), fixture.tenant(t, agTenantID),
		ChatToolCall{Name: agentToolMaintenance, Arguments: map[string]any{"description": "  "}})
	require.Error(t, err)
	assert.Empty(t, result.Data)
	assert.Empty(t, fixture.auditActions(), "argumento inválido não registra nada")
}

// ── Conversas ─────────────────────────────────────────────────────

func TestListConversationsSummarizesMessagesAndOverdue(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.addTenant(agTenant(agOtherTenantID, "Pedro Lima", agOtherPhone))
	fixture.addTenant(agTenant("tenant-3", "Sem Conversa", "(31) 97777-6666"))

	fixture.addReceipt(agReceipt(agReceiptID, agTenantID, "2026-09-01", 2000))
	fixture.addReceipt(agPaidReceipt("receipt-paid", agTenantID, "2026-08-10", 800))
	fixture.addCollectionSent(t, agReceiptID, domain.CollectionStageFriendly)

	fixture.addMessage(domain.WhatsAppMessage{
		ID: "m1", CreatedAt: "2026-09-20 10:00:00", Direction: domain.WhatsAppDirectionIn,
		Phone: "5511999999999", TenantID: domain.StrPtr(agTenantID), Body: "Já paguei, viu?", Status: domain.WhatsAppStatusReceived,
	})
	fixture.addMessage(domain.WhatsAppMessage{
		ID: "m2", CreatedAt: "2026-09-21 09:00:00", Direction: domain.WhatsAppDirectionOut,
		Phone: "5511999999999", TenantID: domain.StrPtr(agTenantID), Body: "Recebi, obrigado.", Status: domain.WhatsAppStatusSent,
	})
	fixture.addMessage(domain.WhatsAppMessage{
		ID: "m3", CreatedAt: "2026-09-21 11:00:00", Direction: domain.WhatsAppDirectionIn,
		Phone: "5521988887777", TenantID: domain.StrPtr(agOtherTenantID), Body: "O chuveiro quebrou.", Status: domain.WhatsAppStatusReceived,
	})

	rows, err := ListConversations(context.Background(), fixture.deps, 0)
	require.NoError(t, err)
	require.Len(t, rows, 2, "titular sem conversa não é conversa")

	// A conversa mais recente vem primeiro.
	assert.Equal(t, agOtherTenantID, rows[0].TenantID)
	assert.True(t, rows[0].AwaitingReply)
	assert.Equal(t, 1, rows[0].MessageCount)
	assert.Equal(t, "O chuveiro quebrou.", rows[0].LastMessage)
	assert.Equal(t, string(domain.WhatsAppDirectionIn), rows[0].LastDirection)
	assert.Equal(t, "Recebida", rows[0].LastDirectionLabel)
	assert.Zero(t, rows[0].OverdueCount)
	assert.Nil(t, rows[0].Overdue)

	row := rows[1]
	assert.Equal(t, agTenantID, row.TenantID)
	assert.Equal(t, "Maria Souza", row.TenantName)
	assert.Equal(t, agPhone, row.Phone)
	assert.False(t, row.AwaitingReply, "a última mensagem foi nossa")
	assert.Equal(t, 2, row.MessageCount)
	assert.Equal(t, "Recebi, obrigado.", row.LastMessage)
	assert.Equal(t, "2026-09-21 09:00:00", row.LastAt)

	require.NotNil(t, row.Overdue)
	assert.Equal(t, agReceiptID, row.Overdue.ReceiptID)
	assert.Equal(t, 20, row.Overdue.DaysLate)
	assert.Equal(t, domain.CollectionStageInstallment, row.Overdue.Stage)
	assert.Equal(t, domain.CollectionStageFriendly, row.Overdue.StageSent)
	assert.True(t, row.Overdue.StagePending)
	assert.Equal(t, 1, row.OverdueCount)
	assert.Equal(t, 2000.0, row.OverdueTotal)
}

func TestListConversationsWithoutMessageRepository(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.deps.Repos.WhatsApp = nil

	rows, err := ListConversations(context.Background(), fixture.deps, 0)
	require.NoError(t, err)
	assert.Empty(t, rows)
}
