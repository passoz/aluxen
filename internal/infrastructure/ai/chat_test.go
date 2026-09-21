package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/application"
)

// A asserção de compilação é a prova de que o cliente serve de implementação
// concreta da porta: sem ela o pacote não compila.
var _ application.ChatClient = (*Client)(nil)

// ferramenta é a declaração de teste de uma ferramenta tipada.
func ferramenta() application.ChatTool {
	return application.ChatTool{
		Name:        "consultar_recibo",
		Description: "Consulta o recibo aberto do titular.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"mes": map[string]any{"type": "string"},
			},
			"required": []any{"mes"},
		},
	}
}

// conversa é um pedido de teste com histórico.
func conversa() application.ChatRequest {
	return application.ChatRequest{
		System: "Você atende inquilinos do Aluxen.",
		Messages: []application.ChatMessage{
			{Role: "user", Content: "Quanto é o aluguel deste mês?"},
			{Role: "assistant", Content: "Vou consultar."},
			{Role: "user", Content: "Obrigado."},
		},
		Temperature: 0.2,
	}
}

// enviado devolve o corpo da última requisição já decodificado.
func enviado(t *testing.T, fake *fakeProvider) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(fake.last().body), &payload); err != nil {
		t.Fatalf("corpo enviado não é JSON: %v", err)
	}
	return payload
}

// mensagens devolve a lista de mensagens enviadas.
func mensagens(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	raw, ok := payload["messages"].([]any)
	if !ok {
		t.Fatalf("campo messages ausente ou de tipo errado: %v", payload["messages"])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		message, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("mensagem de tipo errado: %v", item)
		}
		out = append(out, message)
	}
	return out
}

// ── Pedido enviado ─────────────────────────────────────────────────

func TestCompleteWithoutTools(t *testing.T) {
	fake := newFakeProvider(t, respondContent(t, "Claro, o aluguel vence dia 5."))
	client := fake.client(Config{Model: "modelo-de-conversa"})

	response, err := client.Complete(context.Background(), conversa())
	if err != nil {
		t.Fatalf("conversa falhou: %v", err)
	}
	if response.Message != "Claro, o aluguel vence dia 5." {
		t.Errorf("mensagem = %q", response.Message)
	}
	if len(response.ToolCalls) != 0 {
		t.Errorf("chamadas = %+v, esperadas nenhuma", response.ToolCalls)
	}
	if !strings.Contains(response.Raw, "Claro, o aluguel vence dia 5.") {
		t.Errorf("corpo cru = %q, esperado o do provedor", response.Raw)
	}

	request := fake.last()
	if request.path != chatPath || request.method != http.MethodPost {
		t.Errorf("chamada = %s %s", request.method, request.path)
	}
	if got := request.header.Get("Authorization"); got != "Bearer segredo" {
		t.Errorf("Authorization = %q", got)
	}

	payload := enviado(t, fake)
	if payload["model"] != "modelo-de-conversa" {
		t.Errorf("modelo = %v", payload["model"])
	}
	if payload["temperature"] != 0.2 {
		t.Errorf("temperature = %v, esperada 0.2", payload["temperature"])
	}
	if _, ok := payload["tools"]; ok {
		t.Errorf("pedido sem ferramentas não deveria enviar o campo tools: %v", payload["tools"])
	}
	if _, ok := payload["response_format"]; ok {
		t.Error("conversa não usa modo JSON")
	}
	messages := mensagens(t, payload)
	if len(messages) != 4 {
		t.Fatalf("mensagens = %d, esperadas 4", len(messages))
	}
	if messages[0]["role"] != "system" || messages[0]["content"] != "Você atende inquilinos do Aluxen." {
		t.Errorf("mensagem de sistema inesperada: %v", messages[0])
	}
	if messages[1]["role"] != "user" || messages[2]["role"] != "assistant" || messages[3]["content"] != "Obrigado." {
		t.Errorf("histórico fora de ordem: %v", messages[1:])
	}
}

func TestCompleteWithTools(t *testing.T) {
	fake := newFakeProvider(t, respondContent(t, "Consultando."))
	client := fake.client(Config{})

	request := conversa()
	request.Tools = []application.ChatTool{ferramenta()}
	if _, err := client.Complete(context.Background(), request); err != nil {
		t.Fatalf("conversa falhou: %v", err)
	}

	payload := enviado(t, fake)
	raw, ok := payload["tools"].([]any)
	if !ok || len(raw) != 1 {
		t.Fatalf("tools = %v, esperada uma ferramenta", payload["tools"])
	}
	tool := raw[0].(map[string]any)
	if tool["type"] != "function" {
		t.Errorf("tipo da ferramenta = %v, esperado function", tool["type"])
	}
	function := tool["function"].(map[string]any)
	if function["name"] != "consultar_recibo" {
		t.Errorf("nome = %v", function["name"])
	}
	if function["description"] != "Consulta o recibo aberto do titular." {
		t.Errorf("descrição = %v", function["description"])
	}
	schema := function["parameters"].(map[string]any)
	if schema["type"] != "object" {
		t.Errorf("schema = %v", schema)
	}
	if _, ok := schema["properties"].(map[string]any)["mes"]; !ok {
		t.Errorf("schema sem a propriedade esperada: %v", schema)
	}
}

func TestCompleteWithoutSystemMessage(t *testing.T) {
	fake := newFakeProvider(t, respondContent(t, "Oi."))
	client := fake.client(Config{})
	request := application.ChatRequest{Messages: []application.ChatMessage{{Role: "user", Content: "Oi"}}}
	if _, err := client.Complete(context.Background(), request); err != nil {
		t.Fatalf("conversa falhou: %v", err)
	}
	messages := mensagens(t, enviado(t, fake))
	if len(messages) != 1 || messages[0]["role"] != "user" {
		t.Errorf("mensagens = %v, esperado só o histórico", messages)
	}
	if enviado(t, fake)["temperature"] != 0.0 {
		t.Errorf("temperature = %v, esperado 0", enviado(t, fake)["temperature"])
	}
}

// ── Resposta recebida ──────────────────────────────────────────────

func TestCompleteToolCalls(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantText string
		want     []application.ChatToolCall
	}{
		{
			name:     "só texto",
			body:     `{"choices":[{"message":{"content":"Vence dia 5."}}]}`,
			wantText: "Vence dia 5.",
		},
		{
			name: "só chamada de ferramenta",
			body: `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"consultar_recibo","arguments":"{\"mes\":\"2026-09\"}"}}]}}]}`,
			want: []application.ChatToolCall{
				{Name: "consultar_recibo", Arguments: map[string]any{"mes": "2026-09"}},
			},
		},
		{
			name:     "texto e duas chamadas",
			body:     `{"choices":[{"message":{"content":"Vou consultar e emitir.","tool_calls":[{"function":{"name":"consultar_recibo","arguments":"{\"mes\":\"2026-09\"}"}},{"function":{"name":"emitir_recibo","arguments":"{\"mes\":\"2026-09\",\"parcial\":false}"}}]}}]}`,
			wantText: "Vou consultar e emitir.",
			want: []application.ChatToolCall{
				{Name: "consultar_recibo", Arguments: map[string]any{"mes": "2026-09"}},
				{Name: "emitir_recibo", Arguments: map[string]any{"mes": "2026-09", "parcial": false}},
			},
		},
		{
			name: "argumentos como objeto e sem argumento",
			body: `{"choices":[{"message":{"content":"","tool_calls":[{"function":{"name":"consultar_recibo","arguments":{"mes":"2026-09"}}},{"function":{"name":"listar_imoveis"}}]}}]}`,
			want: []application.ChatToolCall{
				{Name: "consultar_recibo", Arguments: map[string]any{"mes": "2026-09"}},
				{Name: "listar_imoveis"},
			},
		},
		{
			name:     "conteúdo em partes",
			body:     `{"choices":[{"message":{"content":[{"type":"text","text":"Pronto."}]}}]}`,
			wantText: "Pronto.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			})
			client := fake.client(Config{})
			response, err := client.Complete(context.Background(), conversa())
			if err != nil {
				t.Fatalf("conversa falhou: %v", err)
			}
			if response.Message != tc.wantText {
				t.Errorf("mensagem = %q, esperada %q", response.Message, tc.wantText)
			}
			if !reflect.DeepEqual(response.ToolCalls, tc.want) {
				t.Errorf("chamadas = %+v, esperadas %+v", response.ToolCalls, tc.want)
			}
			if response.Raw != tc.body {
				t.Errorf("corpo cru = %q, esperado o do provedor", response.Raw)
			}
		})
	}
}

func TestCompleteRejections(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"sem escolhas", `{"choices":[]}`, "sem escolhas"},
		{"envelope inválido", `não é json`, "envelope"},
		{"argumentos malformados", `{"choices":[{"message":{"content":"","tool_calls":[{"function":{"name":"consultar_recibo","arguments":"{mes: 2026-09"}}]}}]}`, "consultar_recibo"},
		{"argumentos não são objeto", `{"choices":[{"message":{"content":"","tool_calls":[{"function":{"name":"consultar_recibo","arguments":"[1,2]"}}]}}]}`, "objeto JSON"},
		{"argumentos nulos literais", `{"choices":[{"message":{"content":"","tool_calls":[{"function":{"name":"consultar_recibo","arguments":"null"}}]}}]}`, ""},
		{"chamada sem nome", `{"choices":[{"message":{"content":"","tool_calls":[{"function":{"arguments":"{}"}}]}}]}`, "sem nome"},
		{"conteúdo não textual", `{"choices":[{"message":{"content":42}}]}`, "não é texto"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			})
			client := fake.client(Config{})
			response, err := client.Complete(context.Background(), conversa())
			if tc.wantErr == "" {
				// "null" como argumentos é ferramenta sem argumento: válido.
				if err != nil {
					t.Fatalf("conversa falhou: %v", err)
				}
				if len(response.ToolCalls) != 1 || response.ToolCalls[0].Arguments != nil {
					t.Errorf("chamadas = %+v, esperada uma sem argumentos", response.ToolCalls)
				}
				return
			}
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("erro = %v, esperado ErrInvalidResponse", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("mensagem = %q, esperada citando %q", err.Error(), tc.wantErr)
			}
			if response.Raw != tc.body {
				t.Errorf("corpo cru = %q, esperado o do provedor para a trilha", response.Raw)
			}
		})
	}
}

func TestCompleteProviderFailures(t *testing.T) {
	t.Run("HTTP 500", func(t *testing.T) {
		fake := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "modelo indisponível", http.StatusInternalServerError)
		})
		client := fake.client(Config{})
		if _, err := client.Complete(context.Background(), conversa()); err == nil || !strings.Contains(err.Error(), "500") {
			t.Fatalf("erro = %v, esperado com o status", err)
		}
	})

	t.Run("não configurado", func(t *testing.T) {
		fake := newFakeProvider(t, respondContent(t, "Oi."))
		client := NewClient(Config{})
		if _, err := client.Complete(context.Background(), conversa()); !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("erro = %v, esperado ErrNotConfigured", err)
		}
		if fake.calls() != 0 {
			t.Errorf("instância sem IA não deveria chamar o provedor (%d chamadas)", fake.calls())
		}
	})

	t.Run("transporte fora do ar", func(t *testing.T) {
		fake := newFakeProvider(t, respondContent(t, "Oi."))
		client := fake.client(Config{})
		fake.server.Close()
		if _, err := client.Complete(context.Background(), conversa()); err == nil {
			t.Fatal("servidor fora do ar deveria falhar")
		}
	})
}
