package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Atendimento automático do WhatsApp (CAP-302) e leitura das conversas
// (CAP-301).
//
// Guardrails (BR-302/BR-303): o modelo nunca toca o banco. Toda resposta sai de
// ferramenta tipada limitada ao titular da conversa (agent_tools.go); sem
// ferramenta aplicável, com pergunta fora de escopo, pergunta jurídica ou com o
// agente desligado, a conversa escala para o locador e o locatário recebe o
// encaminhamento. Nenhum número é inventado: o que a ferramenta não achou, a
// resposta diz que não achou.
//
// `ListConversations` alimenta a aba de conversas do painel: por titular, com a
// última mensagem, o que ficou sem resposta e o lançamento em atraso.

const (
	// DefaultConversationLimit é a janela de mensagens lida por conversa.
	DefaultConversationLimit = 50
	// MaxConversationLimit limita o pedido do painel.
	MaxConversationLimit = 200
	// conversationPreview é o quanto da mensagem aparece na lista de conversas.
	conversationPreview = 160
	// agentMaxRounds é o limite de rodadas do laço de ferramentas: o atendimento
	// não fica preso conversando com o provedor.
	agentMaxRounds = 4
)

// Motivos de escalonamento. São eles que a trilha registra e o log cita — nunca
// o texto da mensagem.
const (
	agentReasonNoTenant   = "mensagem sem titular"
	agentReasonDisabled   = "atendimento automático desligado"
	agentReasonLegal      = "pergunta jurídica"
	agentReasonEmpty      = "mensagem sem texto"
	agentReasonProvider   = "falha do provedor de IA"
	agentReasonRounds     = "limite de rodadas do atendimento alcançado"
	agentReasonUngrounded = "nenhuma ferramenta aplicável"
)

// Tipos de evento do atendimento registrados na trilha.
const (
	agentKindAnswer     = "agent-answer"
	agentKindEscalation = "agent-escalation"
	// agentKindManualEscalation é o escalonamento feito pelo próprio locador no
	// painel, não pelo atendimento automático.
	agentKindManualEscalation = "manual-escalation"
)

// ConversationRow é uma conversa do painel: o titular, o que ele falou por
// último, o que ficou sem resposta e o lançamento em atraso quando houver.
type ConversationRow struct {
	TenantID           string               `json:"tenantId"`
	TenantName         string               `json:"tenantName"`
	Phone              string               `json:"phone"`
	LastMessage        string               `json:"lastMessage"`
	LastDirection      string               `json:"lastDirection"`
	LastDirectionLabel string               `json:"lastDirectionLabel"`
	LastAt             string               `json:"lastAt"`
	MessageCount       int                  `json:"messageCount"`
	AwaitingReply      bool                 `json:"awaitingReply"`
	OverdueCount       int                  `json:"overdueCount"`
	OverdueTotal       float64              `json:"overdueTotal"`
	Overdue            *ConversationOverdue `json:"overdue,omitempty"`
}

// ConversationOverdue é o lançamento em atraso de uma conversa, com o estágio
// que a régua sustenta para ele.
type ConversationOverdue struct {
	ReceiptID      string                 `json:"receiptId"`
	DueDate        string                 `json:"dueDate"`
	Amount         float64                `json:"amount"`
	DaysLate       int                    `json:"daysLate"`
	Stage          domain.CollectionStage `json:"stage"`
	StageLabel     string                 `json:"stageLabel"`
	StageSent      domain.CollectionStage `json:"stageSent"`
	StageSentLabel string                 `json:"stageSentLabel,omitempty"`
	StagePending   bool                   `json:"stagePending"`
}

// AgentReply é o desfecho do atendimento de uma mensagem recebida.
type AgentReply struct {
	MessageID string   `json:"messageId"`
	TenantID  string   `json:"tenantId"`
	Reply     string   `json:"reply"`
	Tools     []string `json:"tools"`
	Escalated bool     `json:"escalated"`
	Reason    string   `json:"reason,omitempty"`
	Rounds    int      `json:"rounds"`
}

// ListConversations devolve as conversas do painel, da mais recente para a mais
// antiga. Só titular com mensagem registrada é conversa; `limit` é a janela de
// mensagens lida de cada titular (contagem e última mensagem).
func ListConversations(ctx context.Context, deps Deps, limit int) ([]ConversationRow, error) {
	window := limit
	if window <= 0 {
		window = DefaultConversationLimit
	}
	if window > MaxConversationLimit {
		window = MaxConversationLimit
	}

	if deps.Repos.WhatsApp == nil {
		// Instância sem o repositório de mensagens: a aba renderiza vazia, como o
		// resto do painel — ausência de dado não derruba a página.
		return []ConversationRow{}, nil
	}

	tenants, err := deps.Repos.Tenants.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listar titulares das conversas: %w", err)
	}
	receipts, err := deps.Repos.Receipts.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listar lançamentos das conversas: %w", err)
	}
	history, err := collectionHistory(ctx, deps)
	if err != nil {
		return nil, err
	}

	today := deps.Clock.Now()
	overdueByTenant := map[string][]domain.Receipt{}
	for index := range receipts {
		receipt := &receipts[index]
		if domain.ReceiptStatusOf(*receipt, today) != domain.ReceiptStatusOverdue {
			continue
		}
		tenantID := domain.Str(receipt.TenantID)
		if tenantID == "" {
			continue
		}
		overdueByTenant[tenantID] = append(overdueByTenant[tenantID], *receipt)
	}

	rows := make([]ConversationRow, 0, len(tenants))
	for index := range tenants {
		tenant := &tenants[index]
		messages, err := deps.Repos.WhatsApp.ByTenant(ctx, tenant.ID, window)
		if err != nil {
			return nil, fmt.Errorf("listar mensagens do titular: %w", err)
		}
		if len(messages) == 0 {
			continue
		}
		rows = append(rows, conversationRow(tenant, messages, overdueByTenant[tenant.ID], history, today))
	}

	// Conversa mais recente primeiro; sem mensagem datada, o nome desempata.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].LastAt != rows[j].LastAt {
			return rows[i].LastAt > rows[j].LastAt
		}
		return rows[i].TenantName < rows[j].TenantName
	})
	return rows, nil
}

// HandleIncomingMessage atende a mensagem recebida: monta o pedido com o system
// prompt, executa o laço de ferramentas tipadas e devolve a resposta do
// atendimento. Não envia nada — quem entrega é o canal, na camada que já o
// conhece (mesma divisão de `RegisterIncomingMessage`); escalonamento e resposta
// ficam registrados na trilha.
func HandleIncomingMessage(ctx context.Context, deps Deps, actx AuditContext, msg domain.WhatsAppMessage) (*AgentReply, error) {
	tenant, err := conversationTenant(ctx, deps, msg)
	if err != nil {
		return nil, err
	}
	if tenant == nil {
		return escalateToHuman(ctx, deps, actx, msg, nil, agentReasonNoTenant)
	}
	if deps.Chat == nil {
		// Agente desligado (instância sem provedor de IA): a conversa vai direto
		// para o locador, sem passar pelo modelo.
		return escalateToHuman(ctx, deps, actx, msg, tenant, agentReasonDisabled)
	}

	body := strings.TrimSpace(msg.Body)
	if reason, legal := legalQuestion(body); legal {
		// BR-303: dúvida jurídica é recusada antes de qualquer consulta — o
		// atendimento não interpreta contrato nem orienta juridicamente.
		return escalateToHuman(ctx, deps, actx, msg, tenant, reason)
	}
	if body == "" {
		return escalateToHuman(ctx, deps, actx, msg, tenant, agentReasonEmpty)
	}

	reply := &AgentReply{MessageID: msg.ID, TenantID: tenant.ID}
	request := ChatRequest{
		System:      agentSystemPrompt(ctx, deps, tenant),
		Messages:    []ChatMessage{{Role: "user", Content: body}},
		Tools:       agentToolDeclarations(),
		Temperature: 0,
	}
	// grounded diz se alguma ferramenta já devolveu dado real nesta conversa: sem
	// isso, a resposta do modelo não tem lastro e não é publicada.
	grounded := false
	usedTools := make([]string, 0, agentMaxRounds)

	for round := 1; round <= agentMaxRounds; round++ {
		response, err := deps.Chat.Complete(ctx, request)
		if err != nil {
			deps.Log.Warn("atendimento sem resposta do provedor de IA",
				"scope", "agent", "tenant", tenant.ID, "round", round, "err", err)
			return escalateToHuman(ctx, deps, actx, msg, tenant, agentReasonProvider)
		}

		if len(response.ToolCalls) == 0 {
			text := strings.TrimSpace(response.Message)
			if text == "" || !grounded {
				return escalateToHuman(ctx, deps, actx, msg, tenant, agentReasonUngrounded)
			}
			reply.Reply = text
			reply.Tools = usedTools
			reply.Rounds = round
			if err := recordAgentEvent(ctx, deps, actx, msg, tenant, domain.ActionAgentAnswered,
				fmt.Sprintf("Resposta do atendimento automático para %s", tenant.Name),
				map[string]any{"phone": maskedPhone(msg.Phone), "kind": agentKindAnswer}); err != nil {
				return nil, err
			}
			return reply, nil
		}

		for _, call := range response.ToolCalls {
			result, toolErr := runAgentTool(ctx, deps, actx, tenant, call)
			if result.Escalate {
				// O próprio modelo reconheceu o limite do atendimento.
				return escalateToHuman(ctx, deps, actx, msg, tenant, result.Reason)
			}
			if toolErr != nil {
				deps.Log.Info("ferramenta do atendimento recusada",
					"scope", "agent", "tenant", tenant.ID, "tool", call.Name, "round", round)
				request.Messages = append(request.Messages, toolResultTurn(call.Name, "erro: "+toolErr.Error()))
				continue
			}
			grounded = true
			usedTools = appendUnique(usedTools, call.Name)
			request.Messages = append(request.Messages, toolResultTurn(call.Name, result.Data))
		}
	}

	deps.Log.Warn("atendimento atingiu o limite de rodadas",
		"scope", "agent", "tenant", tenant.ID, "rounds", agentMaxRounds)
	return escalateToHuman(ctx, deps, actx, msg, tenant, agentReasonRounds)
}

// ── Atendimento manual do locador ──────────────────────────────────

// ManualReplyInput é a resposta que o locador escreve no painel.
type ManualReplyInput struct {
	// UserID é o administrador dono do envio (a fila do canal é por usuário).
	UserID string
	// TenantID é o titular da conversa: o telefone sai do cadastro dele.
	TenantID string
	Message  string
	Actor    AuditContext
}

// ConversationReply entrega a resposta manual do locador ao titular da
// conversa. É o mesmo envio avulso do canal (job + trilha); o que o painel não
// escolhe é o destinatário — ele vem do titular.
func ConversationReply(ctx context.Context, deps Deps, channel WhatsAppChannel, input ManualReplyInput) (string, error) {
	tenant, err := conversationTenantByID(ctx, deps, input.TenantID)
	if err != nil {
		return "", err
	}
	phone := strings.TrimSpace(domain.Str(tenant.Phone))
	if phone == "" {
		return "", domain.Invalid("O inquilino %s não tem telefone cadastrado.", tenant.Name)
	}

	return WhatsAppSendJob(ctx, deps, channel, WhatsAppSendRequest{
		UserID:  input.UserID,
		To:      phone,
		Message: input.Message,
		Actor:   input.Actor,
	})
}

// EscalateConversation registra que o locador assumiu a conversa — o mesmo
// evento do encaminhamento automático, agora com o locador como autor.
func EscalateConversation(ctx context.Context, deps Deps, actx AuditContext, tenantID, reason string) error {
	tenant, err := conversationTenantByID(ctx, deps, tenantID)
	if err != nil {
		return err
	}

	reason = truncate(strings.TrimSpace(reason), agentEscalationReasonLimit)
	if reason == "" {
		reason = "escalonamento manual do locador"
	}
	return Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionAgentEscalated,
		Entity:      domain.EntityWhatsApp,
		EntityID:    tenant.ID,
		EntityLabel: tenant.Name,
		Summary:     fmt.Sprintf("Conversa de %s assumida pelo locador", tenant.Name),
		Metadata: map[string]any{
			"phone":  maskedPhone(domain.Str(tenant.Phone)),
			"kind":   agentKindManualEscalation,
			"reason": reason,
		},
	})
}

// conversationTenantByID resolve o titular da conversa pelo id informado no
// painel. Titular inexistente é `ErrNotFound` (o mapa de erro vira 404).
func conversationTenantByID(ctx context.Context, deps Deps, tenantID string) (*domain.Tenant, error) {
	trimmed := strings.TrimSpace(tenantID)
	if trimmed == "" {
		return nil, domain.ErrNotFound
	}
	tenant, err := tenantByID(ctx, deps, trimmed)
	if err != nil {
		return nil, err
	}
	if tenant == nil {
		return nil, domain.ErrNotFound
	}
	return tenant, nil
}

// ── Auxiliares do atendimento ──────────────────────────────────────

// conversationTenant resolve o titular da conversa. O webhook já vincula a
// mensagem pelo telefone; quando o vínculo não veio (mensagem gravada antes do
// cadastro), o telefone é consultado de novo — número desconhecido continua sem
// titular e a conversa escala.
func conversationTenant(ctx context.Context, deps Deps, msg domain.WhatsAppMessage) (*domain.Tenant, error) {
	tenant, err := tenantByID(ctx, deps, domain.Str(msg.TenantID))
	if err != nil {
		return nil, err
	}
	if tenant != nil {
		return tenant, nil
	}
	phone := format.NormalizeDigits(msg.Phone)
	if phone == "" {
		return nil, nil
	}
	return tenantByPhone(ctx, deps, phone)
}

// escalateToHuman encaminha a conversa ao locador: registra o escalonamento na
// trilha e devolve o encaminhamento que o locatário recebe. Nada é inventado —
// a resposta diz que a mensagem foi encaminhada, não o que o locador dirá.
func escalateToHuman(ctx context.Context, deps Deps, actx AuditContext, msg domain.WhatsAppMessage, tenant *domain.Tenant, reason string) (*AgentReply, error) {
	reply := &AgentReply{
		MessageID: msg.ID,
		Reply:     agentEscalationReply(reason),
		Tools:     []string{},
		Escalated: true,
		Reason:    reason,
	}
	label := "Conversa sem titular"
	if tenant != nil {
		reply.TenantID = tenant.ID
		label = fmt.Sprintf("Conversa de %s encaminhada ao locador", tenant.Name)
	}

	if err := recordAgentEvent(ctx, deps, actx, msg, tenant, domain.ActionAgentEscalated, label,
		map[string]any{
			"phone":  maskedPhone(msg.Phone),
			"kind":   agentKindEscalation,
			"reason": reason,
		}); err != nil {
		return nil, err
	}
	deps.Log.Info("conversa escalonada para o locador",
		"scope", "agent", "tenant", domain.Str(msg.TenantID), "reason", reason)
	return reply, nil
}

// agentEscalationReply é o texto que o locatário recebe quando o atendimento
// encaminha a conversa. É determinístico: não promete prazo nem resposta.
func agentEscalationReply(reason string) string {
	if reason == agentReasonLegal {
		return "Não posso tratar de questões jurídicas por aqui. Encaminhei sua mensagem ao locador, que responde diretamente."
	}
	return "Recebi sua mensagem e encaminhei ao locador, que responde por aqui o mais breve possível."
}

// recordAgentEvent grava o evento do atendimento na trilha. O registro é o
// mesmo para os dois desfechos: quem respondeu ou quem encaminhou, sobre qual
// mensagem e por quê.
func recordAgentEvent(ctx context.Context, deps Deps, actx AuditContext, msg domain.WhatsAppMessage, tenant *domain.Tenant, action, summary string, metadata map[string]any) error {
	entityLabel := msg.Phone
	if tenant != nil {
		entityLabel = tenant.Name
	}
	return Record(ctx, deps, actx, AuditEntry{
		Action:      action,
		Entity:      domain.EntityWhatsApp,
		EntityID:    msg.ID,
		EntityLabel: entityLabel,
		Summary:     summary,
		Metadata:    metadata,
	})
}

// agentSystemPrompt descreve o escopo do atendimento, as ferramentas e as
// proibições. É fixo (fora o nome do locador e do titular): o comportamento não
// muda com o texto que chega na conversa.
func agentSystemPrompt(ctx context.Context, deps Deps, tenant *domain.Tenant) string {
	landlord := landlordName(ctx, deps)
	if landlord == "" {
		landlord = "o locador"
	}
	return fmt.Sprintf(`Você atende os locatários de um sistema de locação pelo WhatsApp, em nome de %s.

Locatário desta conversa: %s.

Escopo: dúvidas sobre os lançamentos (aluguéis do titular), o contrato do titular e o registro de pedidos de manutenção do imóvel dele.

Regras:
- Os dados vêm sempre das ferramentas. Você não tem acesso ao banco e nunca inventa valor, vencimento, situação ou data.
- Use openReceiptsOf para os lançamentos em aberto, contractOf para o contrato e receiptStatusOf para um lançamento específico que já apareceu na conversa.
- Se a ferramenta não encontrar, diga que não encontrou. Nunca estime nem complete o dado.
- Trate apenas os dados deste locatário; dado de outra pessoa não existe para você.
- Não dê orientação jurídica, não interprete cláusula de contrato e não prometa prazo, desconto, acordo ou obra. Pergunta jurídica ou assunto fora desse escopo: chame escalateToLandlord.
- Pedido de manutenção: chame openMaintenanceRequest com a descrição do problema nas palavras do locatário.
- Responda em português do Brasil, em no máximo quatro frases, sem citar identificadores internos.`, landlord, tenant.Name)
}

// legalQuestion reconhece a dúvida jurídica antes de consultar o modelo: a
// recusa é determinística e não depende de o provedor obedecer ao prompt.
func legalQuestion(body string) (string, bool) {
	if strings.TrimSpace(body) == "" {
		return "", false
	}
	folded := foldAgentText(body)
	for _, term := range legalTerms {
		if strings.Contains(folded, term) {
			return agentReasonLegal, true
		}
	}
	return "", false
}

// legalTerms são os termos que caracterizam dúvida jurídica. A lista é curta de
// propósito: escalonar é seguro, responder o que não pode é que não é.
var legalTerms = []string{
	"advogado", "advogada", "juridic", "justica", "judicial", "processo",
	"despejo", "indenizacao", "usucapiao", "liminar", "acao de execucao",
	"lei do inquinilato", "lei do inquilinato", "acao judicial",
}

// agentAccents reduz acentos para a comparação de termos jurídicos.
var agentAccents = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// foldAgentText reduz o texto a minúsculas sem acento, para a comparação não
// depender de como o locatário escreveu.
func foldAgentText(value string) string {
	return agentAccents.Replace(strings.ToLower(value))
}

// conversationRow monta a linha da conversa a partir da janela de mensagens do
// titular (a mais recente primeiro, como a porta devolve).
func conversationRow(tenant *domain.Tenant, messages []domain.WhatsAppMessage, overdue []domain.Receipt, history map[string]domain.CollectionStage, today time.Time) ConversationRow {
	last := messages[0]
	row := ConversationRow{
		TenantID:           tenant.ID,
		TenantName:         tenant.Name,
		Phone:              domain.Str(tenant.Phone),
		LastMessage:        truncate(last.Body, conversationPreview),
		LastDirection:      string(last.Direction),
		LastDirectionLabel: domain.WhatsAppDirectionLabel(last.Direction),
		LastAt:             last.CreatedAt,
		MessageCount:       len(messages),
		AwaitingReply:      last.Direction == domain.WhatsAppDirectionIn,
	}

	for index := range overdue {
		receipt := &overdue[index]
		row.OverdueCount++
		row.OverdueTotal += receipt.Amount
		candidate := overdueCandidate(receipt, history[receipt.ID], today)
		if row.Overdue == nil || candidate.DaysLate > row.Overdue.DaysLate {
			row.Overdue = candidate
		}
	}
	return row
}

// overdueCandidate descreve o lançamento em atraso para o painel: o estágio que
// o atraso sustenta, o que já foi cobrado e se ainda falta cobrar.
func overdueCandidate(receipt *domain.Receipt, last domain.CollectionStage, today time.Time) *ConversationOverdue {
	days := lateDays(receipt.DueDate, today)
	stage, _ := domain.CollectionStageByDelay(days)
	overdue := &ConversationOverdue{
		ReceiptID:    receipt.ID,
		DueDate:      receipt.DueDate,
		Amount:       receipt.Amount,
		DaysLate:     days,
		Stage:        stage,
		StageLabel:   stage.Label(),
		StageSent:    last,
		StagePending: stage.Valid() && stage > last,
	}
	if last.Valid() {
		overdue.StageSentLabel = last.Label()
	}
	return overdue
}

// appendUnique acrescenta o nome da ferramenta sem repetir: o painel mostra o
// que foi consultado, não quantas vezes.
func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
