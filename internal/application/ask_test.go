package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// A consulta em linguagem natural é exercitada com um dublê da porta de conversa
// sobre o SQLite em memória: o que precisa ser provado é que a resposta carrega
// os números vindos das ferramentas — e que sem ferramenta não há resposta.

// tier2AskChat é o dublê da porta de conversa: devolve o roteiro combinado e
// guarda os pedidos recebidos.
type tier2AskChat struct {
	responses []application.ChatResponse
	requests  []application.ChatRequest
}

func (c *tier2AskChat) Complete(_ context.Context, request application.ChatRequest) (application.ChatResponse, error) {
	c.requests = append(c.requests, request)
	if len(c.responses) == 0 {
		return application.ChatResponse{}, nil
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

// tier2ToolCall monta a resposta do modelo pedindo uma ferramenta.
func tier2ToolCall(name string, arguments map[string]any) application.ChatResponse {
	return application.ChatResponse{ToolCalls: []application.ChatToolCall{{Name: name, Arguments: arguments}}}
}

// newTier2AskFixture monta a carteira da consulta (competência com pago,
// parcial, a vencer e vencido).
func newTier2AskFixture(t *testing.T) *receiptsFixture {
	t.Helper()
	fixture := newReceiptsFixture(t, adjustmentToday)
	seedReportReceipts(t, fixture)
	return fixture
}

// lastTurn devolve a última mensagem enviada ao provedor na rodada informada.
func lastTurn(t *testing.T, request application.ChatRequest) application.ChatMessage {
	t.Helper()
	require.NotEmpty(t, request.Messages)
	return request.Messages[len(request.Messages)-1]
}

func TestAskReportAnswersWithTypedTools(t *testing.T) {
	fixture := newTier2AskFixture(t)
	chat := &tier2AskChat{responses: []application.ChatResponse{
		tier2ToolCall("resumo_mes", map[string]any{"year": float64(2026), "month": float64(9)}),
		// Segunda rodada sem texto: a resposta cai no resumo determinístico da
		// ferramenta, que é o que garante que o número veio do caso de uso.
		{},
	}}
	fixture.deps.Chat = chat

	answer, err := application.AskReport(context.Background(), fixture.deps, "Quanto recebi em setembro de 2026?")
	require.NoError(t, err)

	assert.False(t, answer.Refused)
	assert.Equal(t, []string{"resumo_mes"}, answer.Tools)
	assert.Contains(t, answer.Answer, "R$ 2.900,00", "a resposta cita o recebido do mês")
	assert.Contains(t, answer.Answer, "setembro/2026")
	assert.Contains(t, answer.Answer, "R$ 300,00", "a resposta cita o vencido do mês")

	require.Len(t, chat.requests, 2)
	assert.Equal(t, []string{"resumo_mes", "recebidos_mes", "abertos", "inadimplentes", "ocupacao"},
		declaredTools(chat.requests[0]), "o modelo só pode escolher ferramentas declaradas")
	assert.Equal(t, "Quanto recebi em setembro de 2026?", lastTurn(t, chat.requests[0]).Content)

	toolResult := lastTurn(t, chat.requests[1])
	assert.Equal(t, "user", toolResult.Role)
	assert.Contains(t, toolResult.Content, "Resultado da ferramenta resumo_mes")
	assert.Contains(t, toolResult.Content, `"received":2900`, "o resultado da ferramenta é o dado do caso de uso")
	assert.Contains(t, toolResult.Content, `"overdue":300`)
}

func TestAskReportKeepsModelTextWhenToolRan(t *testing.T) {
	fixture := newTier2AskFixture(t)
	chat := &tier2AskChat{responses: []application.ChatResponse{
		tier2ToolCall("recebidos_mes", map[string]any{"month": float64(9)}),
		{Message: "Em setembro/2026 entraram R$ 2.900,00, com um lançamento parcial de R$ 400,00."},
	}}
	fixture.deps.Chat = chat

	answer, err := application.AskReport(context.Background(), fixture.deps, "Quanto entrou em setembro?")
	require.NoError(t, err)

	assert.False(t, answer.Refused)
	assert.Equal(t, "Em setembro/2026 entraram R$ 2.900,00, com um lançamento parcial de R$ 400,00.", answer.Answer)
	assert.Equal(t, []string{"recebidos_mes"}, answer.Tools)
	assert.Contains(t, lastTurn(t, chat.requests[1]).Content, `"receipts":`)
}

func TestAskReportRefusesQuestionWithoutTool(t *testing.T) {
	fixture := newTier2AskFixture(t)
	chat := &tier2AskChat{responses: []application.ChatResponse{
		{Message: "Posso responder sobre outros assuntos."},
	}}
	fixture.deps.Chat = chat

	answer, err := application.AskReport(context.Background(), fixture.deps, "Qual a cor do céu?")
	require.NoError(t, err)

	assert.True(t, answer.Refused)
	assert.Equal(t, application.AskRefusalMessage, answer.Answer)
	assert.Empty(t, answer.Tools)
	require.Len(t, chat.requests, 1, "sem ferramenta o laço não continua")
}

func TestAskReportRefusesToolOutsideVocabulary(t *testing.T) {
	fixture := newTier2AskFixture(t)
	chat := &tier2AskChat{responses: []application.ChatResponse{
		tier2ToolCall("executar_sql", map[string]any{"sql": "select * from receipts"}),
	}}
	fixture.deps.Chat = chat

	answer, err := application.AskReport(context.Background(), fixture.deps, "Me dá todos os recibos crus.")
	require.NoError(t, err)

	assert.True(t, answer.Refused)
	assert.Equal(t, application.AskRefusalMessage, answer.Answer)
	assert.Empty(t, answer.Tools, "ferramenta não declarada não é executada")
	require.Len(t, chat.requests, 1)
}

func TestAskReportRefusesAfterRoundLimit(t *testing.T) {
	fixture := newTier2AskFixture(t)
	chat := &tier2AskChat{responses: []application.ChatResponse{
		tier2ToolCall("abertos", nil),
		tier2ToolCall("abertos", nil),
		tier2ToolCall("abertos", nil),
		tier2ToolCall("abertos", nil),
		tier2ToolCall("abertos", nil),
	}}
	fixture.deps.Chat = chat

	answer, err := application.AskReport(context.Background(), fixture.deps, "E os abertos?")
	require.NoError(t, err)

	assert.False(t, answer.Refused, "com ferramenta executada a resposta é o resumo do que foi consultado")
	assert.Len(t, chat.requests, 4, "o laço para no teto de rodadas")
	assert.Contains(t, answer.Answer, "em aberto")
	assert.Contains(t, answer.Answer, "R$ 680,00")
}

func TestAskReportResolvesDelinquentsAndOccupancy(t *testing.T) {
	fixture := newTier2AskFixture(t)
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1", RentAmount: 2500})

	cases := []struct {
		name string
		tool string
		want []string
	}{
		{"inadimplentes", "inadimplentes", []string{"Maria Souza", "R$ 1.299,00", "10/08/2026"}},
		{"ocupacao", "ocupacao", []string{"Apto 12", "R$ 2.500,00"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			chat := &tier2AskChat{responses: []application.ChatResponse{tier2ToolCall(testCase.tool, nil), {}}}
			fixture.deps.Chat = chat

			answer, err := application.AskReport(context.Background(), fixture.deps, "Pergunta "+testCase.tool)
			require.NoError(t, err)
			assert.Equal(t, []string{testCase.tool}, answer.Tools)
			for _, want := range testCase.want {
				assert.Contains(t, lastTurn(t, chat.requests[1]).Content, want)
			}
		})
	}
	require.NotEmpty(t, lease.ID)
}

func TestAskReportWithoutProvider(t *testing.T) {
	fixture := newTier2AskFixture(t)

	_, err := application.AskReport(context.Background(), fixture.deps, "Quanto recebi?")
	require.Error(t, err)
	assert.True(t, errors.Is(err, application.ErrAskUnavailable), "erro deveria indicar provedor ausente: %v", err)
}

func TestAskReportRejectsEmptyQuestion(t *testing.T) {
	fixture := newTier2AskFixture(t)
	fixture.deps.Chat = &tier2AskChat{}

	_, err := application.AskReport(context.Background(), fixture.deps, "   ")
	requireAdjustmentInvalid(t, err, "Escreva a pergunta.")
}

// declaredTools devolve os nomes das ferramentas declaradas no pedido.
func declaredTools(request application.ChatRequest) []string {
	names := make([]string, 0, len(request.Tools))
	for _, tool := range request.Tools {
		names = append(names, tool.Name)
	}
	return names
}
