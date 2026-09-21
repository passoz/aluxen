package application

import "context"

// Conversa com o provedor de IA com ferramentas tipadas. O port vive aqui para
// que o agente de atendimento dependa de um contrato, e não do cliente HTTP.
//
// Regra do produto: o agente só age por ferramenta tipada e limitada ao titular
// da conversa; sem ferramenta aplicável, a conversa escala para o locador.

// ChatMessage é uma mensagem da conversa enviada ao provedor.
type ChatMessage struct {
	Role    string
	Content string
}

// ChatTool descreve uma ferramenta disponível ao modelo.
type ChatTool struct {
	Name        string
	Description string
	// Parameters é o JSON Schema dos argumentos aceitos.
	Parameters map[string]any
}

// ChatToolCall é a chamada de ferramenta pedida pelo modelo.
type ChatToolCall struct {
	Name      string
	Arguments map[string]any
}

// ChatRequest é o pedido de resposta ao provedor.
type ChatRequest struct {
	System   string
	Messages []ChatMessage
	Tools    []ChatTool
	// Temperature permite respostas mais determinísticas (padrão 0).
	Temperature float64
}

// ChatResponse é a resposta do provedor: texto e/ou chamadas de ferramenta.
type ChatResponse struct {
	Message   string
	ToolCalls []ChatToolCall
	Raw       string
}

// ChatClient conversa com o provedor. Implementação concreta em
// internal/infrastructure/ai; nula desliga o agente (escalonamento direto).
type ChatClient interface {
	Complete(ctx context.Context, request ChatRequest) (ChatResponse, error)
}
