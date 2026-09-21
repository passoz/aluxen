package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Consulta em linguagem natural sobre os números do sistema (tier 2).
//
// O modelo NÃO escreve SQL e NÃO calcula nada: ele escolhe uma das ferramentas
// tipadas declaradas aqui, o caso de uso executa a leitura determinística (as
// mesmas funções do relatório e das listagens) e o modelo apenas redige a
// resposta a partir do resultado devolvido. Pergunta sem ferramenta aplicável é
// recusada com clareza, e sem provedor configurado a consulta nem começa.

// ErrAskUnavailable indica que o provedor de IA não está configurado nesta
// instância. O handler traduz em 503 com mensagem clara.
var ErrAskUnavailable = errors.New("consulta em linguagem natural não está configurada nesta instância")

const (
	// askMaxRounds é o teto de rodadas do laço de ferramentas — o mesmo padrão
	// do agente de atendimento: ferramenta executada alimenta a próxima rodada,
	// e o laço não gira para sempre.
	askMaxRounds = 4
	// AskRefusalMessage é a recusa devolvida quando nenhuma ferramenta responde
	// à pergunta. Nunca devolvemos texto livre do modelo sem dado por trás.
	AskRefusalMessage = "Não consigo responder isso com os dados do sistema."
)

// askSystemPrompt é a instrução fixa da consulta.
const askSystemPrompt = "Você responde perguntas sobre os números de uma carteira de locação " +
	"usando somente as ferramentas declaradas. Nunca invente números, nunca escreva SQL e nunca " +
	"responda de memória: se nenhuma ferramenta responde à pergunta, diga que não consegue responder " +
	"com os dados do sistema. Responda em português do Brasil, em poucas frases, citando os números " +
	"devolvidos pela ferramenta."

// AskAnswer é a resposta da consulta em linguagem natural.
type AskAnswer struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	// Refused marca a pergunta que o sistema não consegue responder.
	Refused bool `json:"refused"`
	// Tools são as ferramentas executadas, na ordem.
	Tools []string `json:"tools"`
	// summaries são os resumos determinísticos das ferramentas executadas; valem
	// como resposta quando o modelo não devolve texto.
	summaries []string
}

// AskReport responde à pergunta com ferramentas tipadas.
//
// O laço é limitado: cada rodada pode pedir ferramentas, cujo resultado volta ao
// modelo como mensagem de usuário (mesma convenção do agente de atendimento).
// Quando o modelo responde sem pedir ferramenta e nenhuma foi executada, a
// consulta é recusada — não existe resposta inventada.
func AskReport(ctx context.Context, deps Deps, question string) (*AskAnswer, error) {
	trimmed := strings.TrimSpace(question)
	if trimmed == "" {
		return nil, domain.Invalid("Escreva a pergunta.")
	}
	if deps.Chat == nil {
		return nil, ErrAskUnavailable
	}

	toolbox := askToolboxOf()
	declared := make([]ChatTool, 0, len(toolbox.ordered))
	for _, tool := range toolbox.ordered {
		declared = append(declared, tool.declaration())
	}

	answer := &AskAnswer{Question: trimmed, Tools: []string{}}
	messages := []ChatMessage{{Role: "user", Content: trimmed}}

	for round := 0; round < askMaxRounds; round++ {
		response, err := deps.Chat.Complete(ctx, ChatRequest{
			System:      askSystemPrompt,
			Messages:    messages,
			Tools:       declared,
			Temperature: 0,
		})
		if err != nil {
			return nil, fmt.Errorf("consultar o provedor de IA: %w", err)
		}
		if len(response.ToolCalls) == 0 {
			return askAnswerFrom(response, answer), nil
		}

		if text := strings.TrimSpace(response.Message); text != "" {
			messages = append(messages, ChatMessage{Role: "assistant", Content: text})
		}
		for _, call := range response.ToolCalls {
			tool, ok := toolbox.byName[strings.TrimSpace(call.Name)]
			if !ok {
				// Ferramenta não declarada não é executada: o modelo saiu do
				// vocabulário e a consulta é recusada em vez de inventar dado.
				return &AskAnswer{Question: trimmed, Answer: AskRefusalMessage, Refused: true, Tools: answer.Tools}, nil
			}

			result, summary, err := tool.run(ctx, deps, call.Arguments)
			if err != nil {
				return nil, err
			}
			answer.Tools = append(answer.Tools, tool.name)
			if summary != "" {
				answer.summaries = append(answer.summaries, summary)
			}

			encoded, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("serializar o resultado da ferramenta %s: %w", tool.name, err)
			}
			messages = append(messages, askToolResultTurn(tool.name, string(encoded)))
		}
	}

	// Teto de rodadas: devolve o resumo determinístico do que foi consultado.
	return askAnswerFrom(ChatResponse{}, answer), nil
}

// askAnswerFrom decide a resposta da rodada. Com ferramenta executada vale o
// texto do modelo (a base numérica veio das ferramentas) e, na ausência dele, o
// resumo determinístico. Sem ferramenta nenhuma a consulta é recusada.
func askAnswerFrom(response ChatResponse, answer *AskAnswer) *AskAnswer {
	if len(answer.Tools) == 0 {
		answer.Answer = AskRefusalMessage
		answer.Refused = true
		return answer
	}
	text := strings.TrimSpace(response.Message)
	if text == "" {
		text = strings.Join(answer.summaries, "\n")
	}
	if text == "" {
		answer.Answer = AskRefusalMessage
		answer.Refused = true
		return answer
	}
	answer.Answer = text
	return answer
}

// askToolResultTurn devolve o resultado da ferramenta ao modelo. O contrato de
// conversa carrega apenas papel e conteúdo, e a convenção combinada com o agente
// de atendimento é a mensagem de usuário com o resultado.
func askToolResultTurn(tool, result string) ChatMessage {
	return ChatMessage{Role: "user", Content: "Resultado da ferramenta " + tool + ": " + result}
}

// ── Ferramentas tipadas ────────────────────────────────────────────

// askTool é uma ferramenta da consulta: a declaração entregue ao modelo e a
// leitura determinística que responde do banco.
type askTool struct {
	name        string
	description string
	parameters  map[string]any
	run         func(ctx context.Context, deps Deps, arguments map[string]any) (any, string, error)
}

// declaration monta a ferramenta no formato aceito pela porta de conversa.
func (t askTool) declaration() ChatTool {
	return ChatTool{Name: t.name, Description: t.description, Parameters: t.parameters}
}

// askToolbox é o conjunto de ferramentas da consulta, na ordem em que é
// declarado ao modelo.
type askToolbox struct {
	ordered []askTool
	byName  map[string]askTool
}

// askToolboxOf monta o conjunto de ferramentas da consulta.
func askToolboxOf() askToolbox {
	ordered := []askTool{
		{
			name:        "resumo_mes",
			description: "Totais de um mês de referência: esperado, recebido, em aberto, vencido e a contagem por situação.",
			parameters:  askMonthParameters(),
			run:         askResumoMes,
		},
		{
			name:        "recebidos_mes",
			description: "Lançamentos já pagos (total ou parcialmente) em um mês de referência, com titular, imóvel, vencimento e valor.",
			parameters:  askMonthParameters(),
			run:         askRecebidosMes,
		},
		{
			name:        "abertos",
			description: "Lançamentos ainda em aberto (a vencer) em um mês de referência, com titular, imóvel, vencimento e valor.",
			parameters:  askMonthParameters(),
			run:         askAbertos,
		},
		{
			name:        "inadimplentes",
			description: "Inquilinos com lançamentos vencidos e sem baixa, com quantidade, valor total e dias de atraso.",
			parameters:  askNoParameters(),
			run:         askInadimplentes,
		},
		{
			name:        "ocupacao",
			description: "Ocupação dos imóveis: contratos vigentes hoje, imóveis vagos e receita mensal esperada.",
			parameters:  askNoParameters(),
			run:         askOcupacao,
		},
	}
	byName := make(map[string]askTool, len(ordered))
	for _, tool := range ordered {
		byName[tool.name] = tool
	}
	return askToolbox{ordered: ordered, byName: byName}
}

// askMonthParameters é o JSON Schema das ferramentas de competência.
func askMonthParameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"year":  map[string]any{"type": "integer", "description": "Ano de referência (ex.: 2026). Omitido usa o mês corrente."},
			"month": map[string]any{"type": "integer", "description": "Mês de referência, de 1 a 12. Omitido usa o mês corrente."},
		},
	}
}

// askNoParameters é o JSON Schema das ferramentas sem argumento.
func askNoParameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// askResumoMes devolve os totais da competência.
func askResumoMes(ctx context.Context, deps Deps, arguments map[string]any) (any, string, error) {
	year, month := askYearMonth(deps, arguments)
	report, err := MonthlyReport(ctx, deps, year, month)
	if err != nil {
		return nil, "", err
	}
	summary := fmt.Sprintf("Em %s: esperado %s, recebido %s, em aberto %s e vencido %s, em %d lançamento(s).",
		report.Label, format.FormatCurrency(report.Expected), format.FormatCurrency(report.Received),
		format.FormatCurrency(report.Open), format.FormatCurrency(report.Overdue), report.Receipts)
	return report, summary, nil
}

// askRecebidosMes devolve os lançamentos já pagos da competência.
func askRecebidosMes(ctx context.Context, deps Deps, arguments map[string]any) (any, string, error) {
	return askMonthRows(ctx, deps, arguments, func(status domain.ReceiptStatus) bool {
		return status == domain.ReceiptStatusPaid || status == domain.ReceiptStatusPartial
	}, "recebido")
}

// askAbertos devolve os lançamentos a vencer da competência.
func askAbertos(ctx context.Context, deps Deps, arguments map[string]any) (any, string, error) {
	return askMonthRows(ctx, deps, arguments, func(status domain.ReceiptStatus) bool {
		return status == domain.ReceiptStatusDue
	}, "em aberto")
}

// askMonthRows é o corpo comum das ferramentas de lista da competência.
func askMonthRows(ctx context.Context, deps Deps, arguments map[string]any, want func(domain.ReceiptStatus) bool, label string) (any, string, error) {
	year, month := askYearMonth(deps, arguments)
	selection, err := reportMonthOf(ctx, deps, year, month)
	if err != nil {
		return nil, "", err
	}
	rows := selection.rows(want)
	total := 0.0
	for _, row := range rows {
		total += row.Amount
	}
	summary := fmt.Sprintf("Em %s: %d lançamento(s) %s, total %s.",
		selection.label, len(rows), label, format.FormatCurrency(total))
	return map[string]any{
		"month":    selection.key,
		"label":    selection.label,
		"count":    len(rows),
		"total":    total,
		"receipts": rows,
	}, summary, nil
}

// reportDelinquentRow é um inquilino com lançamentos vencidos.
type reportDelinquentRow struct {
	TenantID           string  `json:"tenantId"`
	TenantName         string  `json:"tenantName"`
	Receipts           int     `json:"receipts"`
	Amount             float64 `json:"amount"`
	AmountLabel        string  `json:"amountLabel"`
	OldestDueDate      string  `json:"oldestDueDate"`
	OldestDueDateLabel string  `json:"oldestDueDateLabel"`
	DaysLate           int     `json:"daysLate"`
}

// askInadimplentes devolve os inquilinos com lançamentos vencidos e sem baixa.
func askInadimplentes(ctx context.Context, deps Deps, arguments map[string]any) (any, string, error) {
	receipts, err := ListReceipts(ctx, deps)
	if err != nil {
		return nil, "", err
	}
	tenants, err := ListTenants(ctx, deps)
	if err != nil {
		return nil, "", err
	}

	tenantNames := make(map[string]string, len(tenants))
	for _, tenant := range tenants {
		tenantNames[tenant.ID] = tenant.Name
	}

	today := deps.Clock.Now()
	midnight := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	grouped := map[string]*reportDelinquentRow{}
	for _, receipt := range receipts {
		if domain.ReceiptStatusOf(receipt, today) != domain.ReceiptStatusOverdue {
			continue
		}
		tenantID := domain.Str(receipt.TenantID)
		row, ok := grouped[tenantID]
		if !ok {
			name := receipt.TenantName
			if name == "" {
				name = tenantNames[tenantID]
			}
			if name == "" {
				name = "Sem titular"
			}
			row = &reportDelinquentRow{TenantID: tenantID, TenantName: name}
			grouped[tenantID] = row
		}
		row.Receipts++
		row.Amount += receipt.Amount
		if row.OldestDueDate == "" || receipt.DueDate < row.OldestDueDate {
			row.OldestDueDate = receipt.DueDate
			row.DaysLate = reportDaysLate(receipt.DueDate, midnight)
		}
	}

	rows := make([]reportDelinquentRow, 0, len(grouped))
	total := 0.0
	for _, row := range grouped {
		row.AmountLabel = format.FormatCurrency(row.Amount)
		row.OldestDueDateLabel = format.FormatDateBR(row.OldestDueDate)
		total += row.Amount
		rows = append(rows, *row)
	}
	sort.SliceStable(rows, func(a, b int) bool {
		if rows[a].Amount != rows[b].Amount {
			return rows[a].Amount > rows[b].Amount
		}
		return rows[a].TenantName < rows[b].TenantName
	})

	summary := fmt.Sprintf("%d inquilino(s) com lançamento vencido, total %s.", len(rows), format.FormatCurrency(total))
	if len(rows) > 0 {
		summary = fmt.Sprintf("%s O mais antigo venceu em %s (%d dia(s) de atraso).",
			summary, rows[0].OldestDueDateLabel, rows[0].DaysLate)
	}
	return map[string]any{
		"count":        len(rows),
		"total":        total,
		"delinquents":  rows,
		"referenceDay": format.FormatLocalDate(midnight),
	}, summary, nil
}

// reportDaysLate conta os dias de atraso entre o vencimento e a referência.
func reportDaysLate(dueDate string, midnight time.Time) int {
	due, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dueDate), midnight.Location())
	if err != nil {
		return 0
	}
	days := int(midnight.Sub(due).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// reportOccupancyRow é a ocupação de um imóvel.
type reportOccupancyRow struct {
	PropertyID      string  `json:"propertyId"`
	PropertyName    string  `json:"propertyName"`
	ActiveLeases    int     `json:"activeLeases"`
	TenantName      string  `json:"tenantName"`
	RentAmount      float64 `json:"rentAmount"`
	RentAmountLabel string  `json:"rentAmountLabel"`
	EndDate         string  `json:"endDate"`
	EndDateLabel    string  `json:"endDateLabel"`
	Vacant          bool    `json:"vacant"`
}

// askOcupacao devolve a ocupação dos imóveis e a receita mensal esperada.
func askOcupacao(ctx context.Context, deps Deps, arguments map[string]any) (any, string, error) {
	leases, err := ListLeases(ctx, deps)
	if err != nil {
		return nil, "", err
	}
	properties, err := ListProperties(ctx, deps)
	if err != nil {
		return nil, "", err
	}

	today := format.FormatLocalDate(deps.Clock.Now())
	byProperty := map[string][]domain.Lease{}
	active := 0
	expected := 0.0
	for _, lease := range leases {
		if lease.Status != domain.LeaseStatusActive {
			continue
		}
		if today < lease.StartDate || today > lease.EndDate {
			continue
		}
		byProperty[lease.PropertyID] = append(byProperty[lease.PropertyID], lease)
		active++
		expected += lease.RentAmount
	}

	rows := make([]reportOccupancyRow, 0, len(properties))
	vacant := 0
	for _, property := range properties {
		row := reportOccupancyRow{
			PropertyID:   property.ID,
			PropertyName: property.Name,
			ActiveLeases: len(byProperty[property.ID]),
			Vacant:       len(byProperty[property.ID]) == 0,
		}
		if row.Vacant {
			vacant++
		} else {
			current := byProperty[property.ID][0]
			row.TenantName = current.TenantName
			row.RentAmount = current.RentAmount
			row.RentAmountLabel = format.FormatCurrency(current.RentAmount)
			row.EndDate = current.EndDate
			row.EndDateLabel = format.FormatDateBR(current.EndDate)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(a, b int) bool {
		if rows[a].Vacant != rows[b].Vacant {
			return !rows[a].Vacant
		}
		return rows[a].PropertyName < rows[b].PropertyName
	})

	summary := fmt.Sprintf("%d contrato(s) ativo(s) hoje, %d de %d imóvel(is) vago(s); receita mensal esperada %s.",
		active, vacant, len(properties), format.FormatCurrency(expected))
	return map[string]any{
		"activeLeases":      active,
		"vacantProperties":  vacant,
		"properties":        len(properties),
		"expectedMonthly":   expected,
		"expectedLabel":     format.FormatCurrency(expected),
		"occupancy":         rows,
		"referenceDay":      today,
	}, summary, nil
}

// askYearMonth lê a competência pedida pela ferramenta, caindo para o mês
// corrente quando o argumento vem ausente ou fora de faixa — argumento de
// modelo nunca vira erro de servidor.
func askYearMonth(deps Deps, arguments map[string]any) (int, int) {
	now := deps.Clock.Now()
	year, month := now.Year(), int(now.Month())
	if parsed, ok := askArgumentInt(arguments, "year", "ano"); ok && parsed >= 1970 && parsed <= 9999 {
		year = parsed
	}
	if parsed, ok := askArgumentInt(arguments, "month", "mes", "mês"); ok && parsed >= 1 && parsed <= 12 {
		month = parsed
	}
	return year, month
}

// askArgumentInt lê um argumento inteiro, aceitando número e texto.
func askArgumentInt(arguments map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		value, ok := arguments[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return int(typed), true
		case int:
			return typed, true
		case string:
			parsed, err := strconv.Atoi(strings.TrimSpace(typed))
			if err == nil {
				return parsed, true
			}
		}
	}
	return 0, false
}
