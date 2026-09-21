package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Ferramentas tipadas do atendimento automático (BR-302).
//
// O modelo não tem acesso ao banco: ele escolhe uma ferramenta da lista fechada
// abaixo, os argumentos são validados antes de qualquer leitura, e a execução
// recebe **o titular da conversa** resolvido pelo caso de uso — nunca um
// identificador vindo do modelo. Argumento desconhecido é recusado, o que
// inclui qualquer tentativa de apontar outro titular.
//
// O que a ferramenta devolve é o único material que o modelo tem para
// responder: valor, vencimento, situação e datas saem do banco.

const (
	// agentReceiptLimit é quantos lançamentos uma consulta devolve.
	agentReceiptLimit = 10
	// agentTextArgLimit é o teto de um argumento de texto do modelo.
	agentTextArgLimit = 1000
	// agentEscalationReasonLimit é o teto do motivo de escalonamento.
	agentEscalationReasonLimit = 120

	// Nomes das ferramentas. São o contrato com o provedor.
	agentToolOpenReceipts    = "openReceiptsOf"
	agentToolContract        = "contractOf"
	agentToolReceiptStatus   = "receiptStatusOf"
	agentToolMaintenance     = "openMaintenanceRequest"
	agentToolEscalate        = "escalateToLandlord"
	agentKindMaintenance     = "maintenance"
	agentMaintenanceMinChars = 3
)

// agentToolResult é o que a ferramenta devolveu.
type agentToolResult struct {
	// Data é o dado real do banco, já formatado para leitura.
	Data string
	// Escalate pede o encaminhamento ao locador: o modelo reconheceu que o
	// assunto não é do atendimento.
	Escalate bool
	// Reason é o motivo do encaminhamento.
	Reason string
}

// agentTool é uma ferramenta do atendimento: o que o modelo vê (nome, descrição
// e JSON Schema) e o que roda no banco.
type agentTool struct {
	Name        string
	Description string
	// Parameters é o JSON Schema dos argumentos aceitos.
	Parameters map[string]any
	// Run executa a ferramenta para o titular informado. O contexto de auditoria
	// vem de quem recebeu a mensagem.
	Run func(ctx context.Context, deps Deps, actx AuditContext, tenant *domain.Tenant, args map[string]any) (agentToolResult, error)
}

// agentToolSet são as ferramentas do atendimento. A lista é fechada: nome fora
// dela é recusado, nunca resolvido por aproximação.
var agentToolSet = []agentTool{
	{
		Name:        agentToolOpenReceipts,
		Description: "Lista os lançamentos (aluguéis e cobranças) em aberto do titular desta conversa, com valor, vencimento e situação.",
		Parameters:  objectSchema(map[string]any{}),
		Run: func(ctx context.Context, deps Deps, _ AuditContext, tenant *domain.Tenant, args map[string]any) (agentToolResult, error) {
			if err := onlyArgs(agentToolOpenReceipts, args); err != nil {
				return agentToolResult{}, err
			}
			return openReceiptsOf(ctx, deps, tenant)
		},
	},
	{
		Name:        agentToolContract,
		Description: "Devolve o contrato do titular desta conversa: imóvel, período, valor do aluguel, dia de vencimento e situação.",
		Parameters:  objectSchema(map[string]any{}),
		Run: func(ctx context.Context, deps Deps, _ AuditContext, tenant *domain.Tenant, args map[string]any) (agentToolResult, error) {
			if err := onlyArgs(agentToolContract, args); err != nil {
				return agentToolResult{}, err
			}
			return contractOf(ctx, deps, tenant)
		},
	},
	{
		Name:        agentToolReceiptStatus,
		Description: "Consulta um lançamento específico do titular desta conversa pelo identificador devolvido por openReceiptsOf.",
		Parameters: objectSchema(map[string]any{
			"receiptId": map[string]any{
				"type":        "string",
				"description": "Identificador do lançamento devolvido por openReceiptsOf.",
			},
		}, "receiptId"),
		Run: func(ctx context.Context, deps Deps, _ AuditContext, tenant *domain.Tenant, args map[string]any) (agentToolResult, error) {
			if err := onlyArgs(agentToolReceiptStatus, args, "receiptId"); err != nil {
				return agentToolResult{}, err
			}
			receiptID, err := textArg(agentToolReceiptStatus, args, "receiptId", 1)
			if err != nil {
				return agentToolResult{}, err
			}
			return receiptStatusOf(ctx, deps, tenant, receiptID)
		},
	},
	{
		Name:        agentToolMaintenance,
		Description: "Abre a solicitação de manutenção do imóvel do titular desta conversa, com a descrição do problema nas palavras dele.",
		Parameters: objectSchema(map[string]any{
			"description": map[string]any{
				"type":        "string",
				"description": "O que o locatário relatou, com o imóvel e o problema.",
			},
		}, "description"),
		Run: func(ctx context.Context, deps Deps, actx AuditContext, tenant *domain.Tenant, args map[string]any) (agentToolResult, error) {
			if err := onlyArgs(agentToolMaintenance, args, "description"); err != nil {
				return agentToolResult{}, err
			}
			description, err := textArg(agentToolMaintenance, args, "description", agentMaintenanceMinChars)
			if err != nil {
				return agentToolResult{}, err
			}
			return openMaintenanceRequest(ctx, deps, actx, tenant, description)
		},
	},
	{
		Name:        agentToolEscalate,
		Description: "Encaminha a conversa ao locador. Use quando a pergunta estiver fora do escopo, for jurídica ou quando o locatário pedir algo que o atendimento não resolve.",
		Parameters: objectSchema(map[string]any{
			"reason": map[string]any{
				"type":        "string",
				"description": "Motivo curto do encaminhamento.",
			},
		}, "reason"),
		Run: func(_ context.Context, _ Deps, _ AuditContext, _ *domain.Tenant, args map[string]any) (agentToolResult, error) {
			if err := onlyArgs(agentToolEscalate, args, "reason"); err != nil {
				return agentToolResult{}, err
			}
			reason, err := textArg(agentToolEscalate, args, "reason", 1)
			if err != nil {
				return agentToolResult{}, err
			}
			return escalateToLandlord(reason), nil
		},
	},
}

// agentDeclarations é a declaração das ferramentas para o provedor, montada uma
// vez (o mesmo JSON Schema vale para toda conversa).
var agentDeclarations = declarationsOf(agentToolSet)

// agentToolDeclarations devolve as ferramentas como o provedor as recebe.
func agentToolDeclarations() []ChatTool { return agentDeclarations }

// runAgentTool despacha a chamada do modelo. Ferramenta fora da lista fechada é
// recusada — nunca executada por aproximação.
func runAgentTool(ctx context.Context, deps Deps, actx AuditContext, tenant *domain.Tenant, call ChatToolCall) (agentToolResult, error) {
	name := strings.TrimSpace(call.Name)
	tool, ok := agentToolByName(name)
	if !ok {
		return agentToolResult{}, domain.Invalid("Ferramenta %q não faz parte do atendimento.", name)
	}
	if tenant == nil {
		return agentToolResult{}, domain.Invalid("O atendimento não tem titular nesta conversa.")
	}
	args := call.Arguments
	if args == nil {
		args = map[string]any{}
	}
	return tool.Run(ctx, deps, actx, tenant, args)
}

// toolResultTurn é o turno que leva o resultado da ferramenta de volta ao
// modelo. O contrato da conversa (`ChatMessage`) carrega só papel e texto, sem o
// identificador da chamada; por isso o resultado volta como turno do usuário
// rotulado com o nome da ferramenta — formato que qualquer provedor compatível
// aceita.
func toolResultTurn(tool, result string) ChatMessage {
	return ChatMessage{Role: "user", Content: "Resultado da ferramenta " + tool + ": " + result}
}

// ── Ferramentas ────────────────────────────────────────────────────

// openReceiptsOf lista os lançamentos em aberto do titular. Lançamento pago não
// aparece; parcial aparece, porque ainda há saldo.
func openReceiptsOf(ctx context.Context, deps Deps, tenant *domain.Tenant) (agentToolResult, error) {
	receipts, err := deps.Repos.Receipts.ByTenant(ctx, tenant.ID)
	if err != nil {
		return agentToolResult{}, fmt.Errorf("listar lançamentos do titular: %w", err)
	}

	today := deps.Clock.Now()
	open := make([]domain.Receipt, 0, len(receipts))
	for index := range receipts {
		if domain.ReceiptStatusOf(receipts[index], today) == domain.ReceiptStatusPaid {
			continue
		}
		open = append(open, receipts[index])
	}
	if len(open) == 0 {
		return agentToolResult{Data: "Nenhum lançamento em aberto para este titular."}, nil
	}
	sort.SliceStable(open, func(i, j int) bool {
		if open[i].DueDate != open[j].DueDate {
			return open[i].DueDate < open[j].DueDate
		}
		return open[i].ID < open[j].ID
	})

	lines := make([]string, 0, agentReceiptLimit+1)
	for index := range open {
		if index == agentReceiptLimit {
			lines = append(lines, fmt.Sprintf("... e mais %d lançamento(s) em aberto.", len(open)-index))
			break
		}
		lines = append(lines, receiptLine(&open[index], today))
	}
	return agentToolResult{Data: "Lançamentos em aberto:\n" + strings.Join(lines, "\n")}, nil
}

// contractOf descreve o contrato vigente do titular (o ativo manda; encerrado só
// aparece quando é o único que existe).
func contractOf(ctx context.Context, deps Deps, tenant *domain.Tenant) (agentToolResult, error) {
	leases, err := deps.Repos.Leases.ByTenant(ctx, tenant.ID)
	if err != nil {
		return agentToolResult{}, fmt.Errorf("listar contratos do titular: %w", err)
	}

	var lease *domain.Lease
	for index := range leases {
		if leases[index].Status == domain.LeaseStatusActive {
			lease = &leases[index]
			break
		}
		if lease == nil && leases[index].Status != domain.LeaseStatusTerminated {
			lease = &leases[index]
		}
	}
	if lease == nil {
		return agentToolResult{Data: "Nenhum contrato encontrado para este titular."}, nil
	}

	return agentToolResult{Data: fmt.Sprintf(
		"Contrato do imóvel %s: período de %s a %s, aluguel de %s com vencimento todo dia %d, situação %s.",
		lease.PropertyName,
		format.FormatDateBR(lease.StartDate),
		format.FormatDateBR(lease.EndDate),
		format.FormatCurrency(lease.RentAmount),
		lease.DueDay,
		domain.LeaseStatusLabel(lease.Status),
	)}, nil
}

// receiptStatusOf consulta um lançamento do titular. Lançamento de outro titular
// é recusado antes de qualquer leitura: a resposta é a mesma do lançamento
// inexistente, para o atendimento não confirmar dado de outra pessoa.
func receiptStatusOf(ctx context.Context, deps Deps, tenant *domain.Tenant, receiptID string) (agentToolResult, error) {
	receipt, err := deps.Repos.Receipts.ByID(ctx, receiptID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return agentToolResult{}, domain.Invalid("Lançamento não encontrado para este titular.")
		}
		return agentToolResult{}, fmt.Errorf("buscar lançamento do titular: %w", err)
	}

	owner, err := receiptTenant(ctx, deps, receipt)
	if err != nil {
		return agentToolResult{}, err
	}
	if owner == nil || owner.ID != tenant.ID {
		return agentToolResult{}, domain.Invalid("Lançamento não encontrado para este titular.")
	}

	return agentToolResult{Data: receiptLine(receipt, deps.Clock.Now())}, nil
}

// openMaintenanceRequest registra o pedido de manutenção na trilha. É o que o
// locador lê depois: o problema nas palavras do locatário, com o titular e o
// telefone (mascarado) no evento.
func openMaintenanceRequest(ctx context.Context, deps Deps, actx AuditContext, tenant *domain.Tenant, description string) (agentToolResult, error) {
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionMaintenanceOpened,
		Entity:      domain.EntityWhatsApp,
		EntityID:    tenant.ID,
		EntityLabel: tenant.Name,
		Summary:     fmt.Sprintf("Solicitação de manutenção registrada para %s", tenant.Name),
		Metadata: map[string]any{
			"phone":  maskedPhone(domain.Str(tenant.Phone)),
			"kind":   agentKindMaintenance,
			"reason": truncate(description, maintenanceDescriptionLimit),
		},
	}); err != nil {
		return agentToolResult{}, fmt.Errorf("registrar solicitação de manutenção: %w", err)
	}

	return agentToolResult{Data: fmt.Sprintf(
		"Solicitação de manutenção registrada para %s: %s",
		tenant.Name, truncate(description, maintenanceDescriptionLimit),
	)}, nil
}

// escalateToLandlord pede o encaminhamento da conversa ao locador.
func escalateToLandlord(reason string) agentToolResult {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = agentReasonUngrounded
	}
	return agentToolResult{Escalate: true, Reason: truncate(reason, agentEscalationReasonLimit)}
}

// ── Auxiliares ─────────────────────────────────────────────────────

// agentToolByName busca a ferramenta na lista fechada.
func agentToolByName(name string) (agentTool, bool) {
	for index := range agentToolSet {
		if agentToolSet[index].Name == name {
			return agentToolSet[index], true
		}
	}
	return agentTool{}, false
}

// declarationsOf monta as declarações do provedor a partir das ferramentas.
func declarationsOf(tools []agentTool) []ChatTool {
	declarations := make([]ChatTool, 0, len(tools))
	for index := range tools {
		declarations = append(declarations, ChatTool{
			Name:        tools[index].Name,
			Description: tools[index].Description,
			Parameters:  tools[index].Parameters,
		})
	}
	return declarations
}

// objectSchema monta o JSON Schema de um objeto fechado: argumento não declarado
// é recusado pelo provedor e, de novo, pela validação da ferramenta.
func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// onlyArgs recusa argumento que a ferramenta não declara. É a barreira que
// impede o modelo de escolher o titular: `tenantId` (ou qualquer outro campo)
// não passa.
func onlyArgs(toolName string, args map[string]any, allowed ...string) error {
	unknown := make([]string, 0, len(args))
	for key := range args {
		if !slices.Contains(allowed, key) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return domain.Invalid("A ferramenta %s não aceita o argumento %q.", toolName, unknown[0])
}

// textArg lê um argumento de texto obrigatório, com o mínimo de caracteres
// exigido. Texto longo é cortado, nunca motivo de recusa.
func textArg(toolName string, args map[string]any, key string, minLength int) (string, error) {
	value, ok := args[key]
	if !ok {
		return "", domain.Invalid("A ferramenta %s exige o argumento %q.", toolName, key)
	}
	text, ok := value.(string)
	if !ok {
		return "", domain.Invalid("O argumento %q da ferramenta %s deve ser texto.", key, toolName)
	}
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < minLength {
		return "", domain.Invalid("O argumento %q da ferramenta %s está vazio.", key, toolName)
	}
	return truncate(trimmed, agentTextArgLimit), nil
}

// receiptLine descreve um lançamento para o modelo: identificador (para a
// consulta seguinte), valor, vencimento e situação derivada do banco.
func receiptLine(receipt *domain.Receipt, today time.Time) string {
	line := fmt.Sprintf("id %s: %s, valor %s, vencimento %s, situação %s",
		receipt.ID,
		receiptLabel(receipt),
		format.FormatCurrency(receipt.Amount),
		format.FormatDateBR(receipt.DueDate),
		domain.ReceiptStatusLabel(domain.ReceiptStatusOf(*receipt, today)),
	)
	if receipt.PaidAmount != nil && *receipt.PaidAmount > 0 {
		line += fmt.Sprintf(", valor já pago %s", format.FormatCurrency(*receipt.PaidAmount))
	}
	return line
}

// receiptLabel é o nome do lançamento: a descrição quando existe, senão a
// categoria.
func receiptLabel(receipt *domain.Receipt) string {
	if description := strings.TrimSpace(domain.Str(receipt.Description)); description != "" {
		return description
	}
	return domain.ReceiptCategoryLabel(receipt.Category)
}
