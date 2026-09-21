package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/passoz/aluxen/internal/application"
)

// Este arquivo implementa a conversa com ferramentas tipadas do agente de
// atendimento. O modelo só age por ferramenta declarada aqui pelo chamador;
// nenhuma ferramenta nasce no pacote e nenhuma ação é executada — o cliente
// devolve o pedido e quem executa é o caso de uso.

// chatPayload é o corpo de uma conversa no formato OpenAI. Diferente da leitura
// de documento, aqui não há `response_format`: a resposta é conversa e/ou
// chamadas de ferramenta.
type chatPayload struct {
	Model       string     `json:"model"`
	Messages    []chatTurn `json:"messages"`
	Tools       []chatTool `json:"tools,omitempty"`
	Temperature float64    `json:"temperature"`
}

// chatTurn é uma mensagem enviada na conversa.
type chatTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatTool declara uma ferramenta no formato OpenAI.
type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

// chatFunction é a descrição da ferramenta: nome, texto de apoio e JSON Schema
// dos argumentos.
type chatFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// Complete conversa com o provedor e devolve o texto e/ou as chamadas de
// ferramenta pedidas pelo modelo, com o corpo cru da resposta para a trilha.
//
// Quando a resposta traz uma chamada de ferramenta inválida (sem nome ou com
// argumentos que não são objeto JSON), o erro é ErrInvalidResponse e o texto já
// lido volta junto — a trilha registra o que o modelo respondeu, mas nada é
// executado.
func (c *Client) Complete(ctx context.Context, request application.ChatRequest) (application.ChatResponse, error) {
	raw, err := c.post(ctx, c.chatPayload(request))
	if err != nil {
		return application.ChatResponse{}, err
	}
	// O corpo cru volta mesmo quando o parse falha: a trilha registra o que o
	// modelo respondeu, mas nada é executado.
	response := application.ChatResponse{Raw: string(raw)}

	var decoded chatResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		// A mensagem não repete o trecho devolvido: a conversa é dado pessoal.
		return response, fmt.Errorf("%w: envelope do provedor inválido", ErrInvalidResponse)
	}
	message, err := firstMessage(decoded)
	if err != nil {
		return response, err
	}
	text, err := parseContent(message.Content, false)
	response.Message = text
	if err != nil {
		return response, err
	}
	calls, err := parseToolCalls(message.ToolCalls)
	response.ToolCalls = calls
	if err != nil {
		return response, err
	}
	return response, nil
}

// chatPayload monta o corpo do pedido: a instrução de sistema (quando houver),
// o histórico na ordem enviada e as ferramentas declaradas.
func (c *Client) chatPayload(request application.ChatRequest) chatPayload {
	messages := make([]chatTurn, 0, len(request.Messages)+1)
	if strings.TrimSpace(request.System) != "" {
		messages = append(messages, chatTurn{Role: "system", Content: request.System})
	}
	for _, message := range request.Messages {
		messages = append(messages, chatTurn{Role: message.Role, Content: message.Content})
	}

	tools := make([]chatTool, 0, len(request.Tools))
	for _, tool := range request.Tools {
		tools = append(tools, chatTool{
			Type: "function",
			Function: chatFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}

	return chatPayload{
		Model:       c.model,
		Messages:    messages,
		Tools:       tools,
		Temperature: request.Temperature,
	}
}

// parseToolCalls converte as chamadas de ferramenta da resposta. Chamada sem
// nome não tem como ser despachada e é falha, nunca dado.
func parseToolCalls(raw []toolCallWire) ([]application.ChatToolCall, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	calls := make([]application.ChatToolCall, 0, len(raw))
	for _, call := range raw {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: chamada de ferramenta sem nome", ErrInvalidResponse)
		}
		arguments, err := parseArguments(name, call.Function.Arguments)
		if err != nil {
			return nil, err
		}
		calls = append(calls, application.ChatToolCall{Name: name, Arguments: arguments})
	}
	return calls, nil
}

// parseArguments decodifica os argumentos da ferramenta. No formato OpenAI eles
// vêm como string JSON; aceitamos também o objeto que alguns provedores
// devolvem. Ferramenta sem argumento é válida (schema vazio); o resto é falha —
// a mensagem cita a ferramenta e o motivo, nunca os valores lidos.
func parseArguments(name string, raw json.RawMessage) (map[string]any, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return nil, nil
	}
	var quoted string
	if err := json.Unmarshal(raw, &quoted); err == nil {
		text = strings.TrimSpace(quoted)
		if text == "" || text == "null" {
			return nil, nil
		}
	}

	var arguments map[string]any
	if err := json.Unmarshal([]byte(text), &arguments); err != nil {
		return nil, fmt.Errorf("%w: argumentos da ferramenta %q não são um objeto JSON", ErrInvalidResponse, name)
	}
	if arguments == nil {
		return nil, fmt.Errorf("%w: argumentos da ferramenta %q não são um objeto JSON", ErrInvalidResponse, name)
	}
	return arguments, nil
}
