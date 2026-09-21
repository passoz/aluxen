package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── Provedor de IA falso ───────────────────────────────────────────

// recordedRequest é uma chamada recebida pelo provedor falso.
type recordedRequest struct {
	method string
	path   string
	header http.Header
	body   string
}

// fakeProvider serve a API compatível com OpenAI em memória e guarda o que
// recebeu. Nenhum teste deste pacote toca a rede real.
type fakeProvider struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
}

func newFakeProvider(t *testing.T, handler http.HandlerFunc) *fakeProvider {
	t.Helper()
	fake := &fakeProvider{t: t}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fake.mu.Lock()
		fake.requests = append(fake.requests, recordedRequest{
			method: r.Method,
			path:   r.URL.Path,
			header: r.Header.Clone(),
			body:   string(body),
		})
		fake.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeProvider) client(config Config) *Client {
	f.t.Helper()
	config.BaseURL = f.server.URL
	if config.APIKey == "" {
		config.APIKey = "segredo"
	}
	return NewClient(config)
}

func (f *fakeProvider) last() recordedRequest {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		f.t.Fatal("nenhuma chamada chegou ao provedor falso")
	}
	return f.requests[len(f.requests)-1]
}

func (f *fakeProvider) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// respondContent devolve a resposta compatível com OpenAI com o texto pedido no
// campo `content`.
func respondContent(t *testing.T, content string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("montagem da resposta falsa: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
}

// document é o documento de teste: um "JPEG" pequeno e válido para o cliente.
func document() Document {
	return Document{Bytes: []byte("conteudo-do-arquivo"), Mime: "image/jpeg", FileName: "comprovante.jpg"}
}

// ── Configuração ───────────────────────────────────────────────────

func TestNewClientDefaults(t *testing.T) {
	client := NewClient(Config{})
	if client.Model() != DefaultModel {
		t.Errorf("modelo = %q, esperado %q", client.Model(), DefaultModel)
	}
	if client.MaxBytes() != DefaultMaxBytes {
		t.Errorf("limite = %d, esperado %d", client.MaxBytes(), DefaultMaxBytes)
	}
	if client.http.Timeout != DefaultTimeout {
		t.Errorf("timeout = %v, esperado %v", client.http.Timeout, DefaultTimeout)
	}
	if client.Configured() {
		t.Error("cliente sem URL e sem chave não deveria estar configurado")
	}
}

func TestNewClientUsesConfig(t *testing.T) {
	httpClient := &http.Client{Timeout: 3 * time.Second}
	client := NewClient(Config{
		BaseURL:    "https://ia.exemplo/v1/",
		APIKey:     "chave",
		Model:      "modelo-de-visao",
		HTTPClient: httpClient,
		Timeout:    9 * time.Second,
		MaxBytes:   1024,
	})
	if client.Model() != "modelo-de-visao" {
		t.Errorf("modelo = %q", client.Model())
	}
	if client.MaxBytes() != 1024 {
		t.Errorf("limite = %d, esperado 1024", client.MaxBytes())
	}
	if client.http != httpClient {
		t.Error("cliente HTTP injetado não foi usado")
	}
	if client.baseURL != "https://ia.exemplo/v1" {
		t.Errorf("base = %q, esperado sem barra final", client.baseURL)
	}
	if !client.Configured() {
		t.Error("cliente com URL e chave deveria estar configurado")
	}
}

func TestNotConfiguredSkipsProvider(t *testing.T) {
	fake := newFakeProvider(t, respondContent(t, `{}`))
	cases := []struct {
		name   string
		config Config
	}{
		{"sem URL", Config{APIKey: "chave"}},
		{"sem chave", Config{BaseURL: fake.server.URL}},
		{"sem nada", Config{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(tc.config)
			ctx := context.Background()
			if _, _, err := client.ExtractPaymentProof(ctx, document()); !errors.Is(err, ErrNotConfigured) {
				t.Errorf("comprovante: erro = %v, esperado ErrNotConfigured", err)
			}
			if _, _, err := client.ExtractLeaseDocument(ctx, document()); !errors.Is(err, ErrNotConfigured) {
				t.Errorf("contrato: erro = %v, esperado ErrNotConfigured", err)
			}
			if _, _, err := client.ExtractPersonDocument(ctx, document()); !errors.Is(err, ErrNotConfigured) {
				t.Errorf("documento pessoal: erro = %v, esperado ErrNotConfigured", err)
			}
			if fake.calls() != 0 {
				t.Errorf("instância sem IA não deveria chamar o provedor (%d chamadas)", fake.calls())
			}
		})
	}
}

// ── Validação do documento ─────────────────────────────────────────

func TestDocumentValidation(t *testing.T) {
	fake := newFakeProvider(t, respondContent(t, `{"amount": 10, "date": "2026-09-01", "confidence": 0.9}`))
	cases := []struct {
		name    string
		doc     Document
		config  Config
		wantErr string
	}{
		{"arquivo vazio", Document{Bytes: nil, Mime: "image/png", FileName: "print.png"}, Config{}, "está vazio"},
		{"acima do limite", Document{Bytes: []byte(strings.Repeat("x", 2048)), Mime: "image/png", FileName: "print.png"}, Config{MaxBytes: 1024}, "acima do limite"},
		{"limite padrão de 12 MiB", Document{Bytes: make([]byte, DefaultMaxBytes+1), Mime: "application/pdf", FileName: "contrato.pdf"}, Config{}, "acima do limite"},
		{"mime não suportado", Document{Bytes: []byte("x"), Mime: "text/plain", FileName: "nota.txt"}, Config{}, "mime"},
		{"sem mime", Document{Bytes: []byte("x"), FileName: "arquivo"}, Config{}, "sem tipo de arquivo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.client(tc.config)
			_, _, err := client.ExtractPaymentProof(context.Background(), tc.doc)
			if !errors.Is(err, ErrInvalidDocument) {
				t.Fatalf("erro = %v, esperado ErrInvalidDocument", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("mensagem = %q, esperada contendo %q", err.Error(), tc.wantErr)
			}
		})
	}
	if fake.calls() != 0 {
		t.Errorf("documento inválido não deveria chegar ao provedor (%d chamadas)", fake.calls())
	}
}

func TestAcceptedMimes(t *testing.T) {
	cases := []struct {
		mime string
		want string
	}{
		{"image/jpeg", "image/jpeg"},
		{"IMAGE/JPG", "image/jpeg"},
		{" image/png ", "image/png"},
		{"image/webp", "image/webp"},
		{"application/pdf; charset=binary", "application/pdf"},
		{"image/gif", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := normalizeMime(tc.mime); got != tc.want {
			t.Errorf("normalizeMime(%q) = %q, esperado %q", tc.mime, got, tc.want)
		}
	}
}

// ── Requisição enviada ─────────────────────────────────────────────

func TestRequestPayload(t *testing.T) {
	fake := newFakeProvider(t, respondContent(t, `{"amount": 1500.5, "date": "2026-09-10", "confidence": 0.97}`))
	client := fake.client(Config{Model: "modelo-de-visao"})

	doc := document()
	if _, _, err := client.ExtractPaymentProof(context.Background(), doc); err != nil {
		t.Fatalf("extração falhou: %v", err)
	}

	request := fake.last()
	if request.method != http.MethodPost {
		t.Errorf("método = %s, esperado POST", request.method)
	}
	if request.path != chatPath {
		t.Errorf("caminho = %s, esperado %s", request.path, chatPath)
	}
	if got := request.header.Get("Authorization"); got != "Bearer segredo" {
		t.Errorf("Authorization = %q", got)
	}
	if got := request.header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}

	var payload struct {
		Model          string `json:"model"`
		Temperature    float64
		ResponseFormat struct {
			Type string `json:"type"`
		} `json:"response_format"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(request.body), &payload); err != nil {
		t.Fatalf("corpo enviado não é JSON: %v", err)
	}
	if payload.Model != "modelo-de-visao" {
		t.Errorf("modelo enviado = %q", payload.Model)
	}
	if payload.Temperature != 0 {
		t.Errorf("temperatura = %v, esperada 0", payload.Temperature)
	}
	if payload.ResponseFormat.Type != "json_object" {
		t.Errorf("response_format = %q, esperado json_object", payload.ResponseFormat.Type)
	}
	if len(payload.Messages) != 2 {
		t.Fatalf("mensagens = %d, esperadas 2", len(payload.Messages))
	}
	if payload.Messages[0].Role != "system" {
		t.Errorf("papel da primeira mensagem = %q", payload.Messages[0].Role)
	}
	var prompt string
	if err := json.Unmarshal(payload.Messages[0].Content, &prompt); err != nil {
		t.Fatalf("instrução de sistema não é texto: %v", err)
	}
	if !strings.Contains(prompt, "JSON") || !strings.Contains(prompt, "confidence") {
		t.Errorf("instrução de sistema inesperada: %q", prompt)
	}
	if payload.Messages[1].Role != "user" {
		t.Errorf("papel da segunda mensagem = %q", payload.Messages[1].Role)
	}
	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(payload.Messages[1].Content, &parts); err != nil {
		t.Fatalf("conteúdo da mensagem do usuário não é lista de partes: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("partes = %d, esperadas 2", len(parts))
	}
	if parts[0].Type != "text" || !strings.Contains(parts[0].Text, "JSON") {
		t.Errorf("instrução inesperada: %+v", parts[0])
	}
	wantURL := dataURL("image/jpeg", doc.Bytes)
	if parts[1].Type != "image_url" || parts[1].ImageURL.URL != wantURL {
		t.Errorf("documento enviado = %q, esperado %q", parts[1].ImageURL.URL, wantURL)
	}
	if !strings.HasPrefix(wantURL, "data:image/jpeg;base64,") {
		t.Fatalf("data URL sem base64: %q", wantURL)
	}
}

func TestPDFGoesAsPDFDataURL(t *testing.T) {
	fake := newFakeProvider(t, respondContent(t, `{"tenantName": "Maria", "confidence": 0.8}`))
	client := fake.client(Config{})
	doc := Document{Bytes: []byte("%PDF-1.4 contrato"), Mime: "application/pdf", FileName: "contrato.pdf"}
	if _, _, err := client.ExtractLeaseDocument(context.Background(), doc); err != nil {
		t.Fatalf("extração falhou: %v", err)
	}
	if body := fake.last().body; !strings.Contains(body, "data:application/pdf;base64,") {
		t.Error("o PDF não foi enviado como data URL base64")
	}
}

// ── Falhas do provedor ─────────────────────────────────────────────

func TestProviderHTTPError(t *testing.T) {
	fake := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "modelo indisponível", http.StatusInternalServerError)
	})
	client := fake.client(Config{})
	extraction, _, err := client.ExtractPaymentProof(context.Background(), document())
	if err == nil {
		t.Fatal("erro HTTP deveria falhar")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("mensagem = %q, esperada com o status", err.Error())
	}
	if errors.Is(err, ErrNotConfigured) || errors.Is(err, ErrInvalidDocument) {
		t.Errorf("erro HTTP classificado errado: %v", err)
	}
	if extraction.Amount != 0 {
		t.Errorf("extração = %+v, esperada vazia", extraction)
	}
}

func TestTransportError(t *testing.T) {
	fake := newFakeProvider(t, respondContent(t, `{}`))
	client := fake.client(Config{})
	fake.server.Close()
	if _, _, err := client.ExtractPaymentProof(context.Background(), document()); err == nil {
		t.Fatal("servidor fora do ar deveria falhar")
	}
}

func TestResponseTooLarge(t *testing.T) {
	fake := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, io.LimitReader(strings.NewReader(strings.Repeat("x", maxResponseBytes*2)), maxResponseBytes*2))
	})
	client := fake.client(Config{})
	_, _, err := client.ExtractPaymentProof(context.Background(), document())
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("erro = %v, esperado ErrInvalidResponse", err)
	}
}

func TestEmptyAndMalformedEnvelope(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"envelope inválido", `não é json`, "envelope"},
		{"sem escolhas", `{"choices": []}`, "sem escolhas"},
		{"conteúdo vazio", `{"choices": [{"message": {"content": "  "}}]}`, "vazia"},
		{"conteúdo nulo", `{"choices": [{"message": {"content": null}}]}`, "vazia"},
		{"conteúdo não textual", `{"choices": [{"message": {"content": 42}}]}`, "não é texto"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			})
			client := fake.client(Config{})
			_, _, err := client.ExtractPaymentProof(context.Background(), document())
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("erro = %v, esperado ErrInvalidResponse", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("mensagem = %q, esperada contendo %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestContentAsParts(t *testing.T) {
	fake := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":[{"type":"text","text":"{\"amount\": 100.0, \"date\": \"2026-09-01\", \"confidence\": 1}"}]}}]}`)
	})
	client := fake.client(Config{})
	extraction, _, err := client.ExtractPaymentProof(context.Background(), document())
	if err != nil {
		t.Fatalf("extração falhou: %v", err)
	}
	if extraction.Amount != 100 || extraction.Confidence != 1 {
		t.Errorf("extração = %+v", extraction)
	}
}
