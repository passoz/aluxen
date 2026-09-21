// Package ai implementa a leitura assistida por IA dos documentos do Aluxen
// sobre uma API compatível com OpenAI (`POST {base}/chat/completions`, com a
// imagem ou o PDF anexado como data URL base64). O provedor é o mesmo já usado
// no resto do stack (DeepSeek v4.1 Flash, que lê imagem), então não entra
// dependência, chave nem contrato novos.
//
// Este pacote é infraestrutura pura: monta a requisição, exige JSON estrito e
// devolve valores tipados do domínio. Nenhuma regra de negócio mora aqui — quem
// decide a baixa do comprovante é domain.EvaluatePaymentMatch, aplicada por
// internal/application; quem decide se um cadastro proposto é gravado é a
// confirmação humana; quem executa uma ferramenta pedida pelo modelo é o caso
// de uso do agente, não este cliente. Saída fora de formato é falha, nunca dado
// (BR-201).
//
// O documento enviado é dado pessoal: este pacote não registra log nenhum e as
// mensagens de erro citam apenas o campo e o motivo — nunca o valor lido nem o
// arquivo (BR-202). A única exceção é o trecho truncado do corpo de erro que o
// próprio provedor devolve num status fora de 2xx.
package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/application"
)

// ErrNotConfigured indica que a instância não tem provedor de IA configurado
// (`AI_API_URL` ou `AI_API_KEY` vazio). Sem ele a instância sobe normalmente e a
// fila registra o motivo — nunca é erro de servidor para o usuário.
var ErrNotConfigured = errors.New("IA não configurada: defina AI_API_URL e AI_API_KEY")

// ErrInvalidDocument indica documento que não pode ser enviado ao provedor:
// vazio, acima do limite de tamanho ou com mime não suportado.
var ErrInvalidDocument = errors.New("documento inválido para leitura por IA")

// ErrInvalidResponse indica que o provedor não devolveu o schema combinado:
// JSON inválido, campo ausente ou valor fora de faixa. É falha de leitura,
// nunca dado (BR-201).
var ErrInvalidResponse = errors.New("resposta inválida do provedor de IA")

const (
	// DefaultTimeout é o tempo máximo de uma leitura por IA. Visão com imagem é
	// mais lenta que o resto do stack, então o padrão é folgado.
	DefaultTimeout = 60 * time.Second
	// DefaultMaxBytes é o limite padrão do documento enviado: 12 MiB.
	DefaultMaxBytes int64 = 12 << 20
	// DefaultModel é o modelo usado quando a instância não informa `AI_MODEL` —
	// o mesmo do resto do stack, que lê imagem.
	DefaultModel = "deepseek-v4.1-flash"
	// chatPath é o caminho da API compatível com OpenAI.
	chatPath = "/chat/completions"
	// maxResponseBytes limita o corpo lido do provedor, para que uma resposta
	// patológica não consuma memória sem limite.
	maxResponseBytes = 1 << 20
	// maxErrorBody limita quanto do corpo de erro entra na mensagem.
	maxErrorBody = 300
)

// supportedMimes são os formatos que o provedor lê: foto/print de comprovante e
// PDF de contrato ou documento pessoal.
var supportedMimes = []string{"image/jpeg", "image/png", "image/webp", "application/pdf"}

// Document é o arquivo a ler: bytes, mime e nome. É um alias de
// application.DocumentFile para que Client satisfaça application.ProofExtractor
// e application.IntakeExtractor sem adaptador.
type Document = application.DocumentFile

// O mesmo cliente é a implementação concreta das três portas publicadas por
// internal/application: leitura de comprovante, leitura de documento de
// cadastro e conversa com ferramentas. A composição injeta o mesmo valor nas
// três — a asserção é de compilação, não de teste.
var (
	_ application.ProofExtractor  = (*Client)(nil)
	_ application.IntakeExtractor = (*Client)(nil)
	_ application.ChatClient      = (*Client)(nil)
	_ application.PhotoComparator = (*Client)(nil)
)

// Config configura o cliente do provedor de IA.
type Config struct {
	// BaseURL é a base da API compatível com OpenAI, incluindo a versão
	// (ex.: https://api.deepseek.com/v1). Vazia desliga a leitura por IA.
	BaseURL string
	// APIKey autentica as chamadas (header Authorization: Bearer).
	APIKey string
	// Model é o modelo de visão usado nas extrações (padrão DefaultModel).
	Model string
	// HTTPClient permite injetar o cliente HTTP; os testes usam httptest.Server.
	HTTPClient *http.Client
	// Timeout é o tempo máximo de uma leitura. Só vale quando HTTPClient é nil.
	Timeout time.Duration
	// MaxBytes é o limite do documento enviado (padrão DefaultMaxBytes).
	MaxBytes int64
}

// Client fala com o provedor de IA.
type Client struct {
	baseURL  string
	apiKey   string
	model    string
	maxBytes int64
	http     *http.Client
}

// NewClient cria o cliente do provedor. Sem BaseURL ou APIKey o cliente
// continua válido, mas todo método devolve ErrNotConfigured — é o que permite
// subir a instância sem IA configurada.
func NewClient(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		baseURL:  strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:   strings.TrimSpace(cfg.APIKey),
		model:    model,
		maxBytes: maxBytes,
		http:     httpClient,
	}
}

// Configured informa se a instância tem provedor de IA configurado. É o valor
// que a composição publica em Options.AIEnabled.
func (c *Client) Configured() bool { return c.baseURL != "" && c.apiKey != "" }

// Model devolve o modelo de visão usado nas extrações.
func (c *Client) Model() string { return c.model }

// MaxBytes devolve o limite de tamanho do documento enviado.
func (c *Client) MaxBytes() int64 { return c.maxBytes }

// ── Protocolo compatível com OpenAI ────────────────────────────────

// chatRequest é o corpo enviado ao provedor.
type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Temperature    float64       `json:"temperature"`
	ResponseFormat responseMode  `json:"response_format"`
}

// responseMode pede ao provedor JSON puro, sem texto em volta.
type responseMode struct {
	Type string `json:"type"`
}

// chatMessage é uma mensagem da conversa. O conteúdo é texto (instrução) ou uma
// lista de partes (instrução + documento).
type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// chatPart é uma parte do conteúdo: texto ou imagem.
type chatPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

// chatResponse é a resposta do provedor; só o que usamos dela.
type chatResponse struct {
	Choices []struct {
		Message messageWire `json:"message"`
	} `json:"choices"`
}

// messageWire é a mensagem devolvida pelo provedor.
type messageWire struct {
	Content   json.RawMessage `json:"content"`
	ToolCalls []toolCallWire  `json:"tool_calls"`
}

// toolCallWire é uma chamada de ferramenta pedida pelo modelo. Os argumentos
// vêm como string JSON no formato OpenAI; guardamos cru para aceitar também o
// objeto que alguns provedores devolvem.
type toolCallWire struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

// complete envia o documento com a instrução e devolve o texto cru do provedor.
// O documento vai como data URL base64 do próprio mime, no campo `image_url`.
func (c *Client) complete(ctx context.Context, prompt, instruction string, doc Document) (string, error) {
	if !c.Configured() {
		return "", ErrNotConfigured
	}
	mime, err := c.validateFile(doc.Bytes, doc.Mime, doc.FileName)
	if err != nil {
		return "", err
	}
	return c.completeImages(ctx, prompt, instruction, []chatPart{
		{Type: "image_url", ImageURL: &imageURL{URL: dataURL(mime, doc.Bytes)}},
	})
}

// completeImages envia a instrução com um ou mais conteúdos (imagem como data
// URL base64 do próprio mime, e o texto que amarra o nome do arquivo à foto) e
// devolve o texto cru do provedor. É o caminho compartilhado pela leitura de um
// documento e pela comparação de fotos.
func (c *Client) completeImages(ctx context.Context, prompt, instruction string, content []chatPart) (string, error) {
	if !c.Configured() {
		return "", ErrNotConfigured
	}
	parts := make([]chatPart, 0, len(content)+1)
	parts = append(parts, chatPart{Type: "text", Text: instruction})
	parts = append(parts, content...)

	raw, err := c.post(ctx, chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: prompt},
			{Role: "user", Content: parts},
		},
		Temperature:    0,
		ResponseFormat: responseMode{Type: "json_object"},
	})
	if err != nil {
		return "", err
	}

	var decoded chatResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		// A mensagem não repete o trecho devolvido: pode conter dado pessoal.
		return "", fmt.Errorf("%w: envelope do provedor inválido", ErrInvalidResponse)
	}
	message, err := firstMessage(decoded)
	if err != nil {
		return "", err
	}
	return parseContent(message.Content, true)
}

// post envia o corpo já montado e devolve a resposta crua de um 2xx. Erro de
// transporte vira erro embrulhado; status fora de 2xx vira erro com o trecho do
// corpo devolvido pelo provedor. Nada aqui é registrado em log.
func (c *Client) post(ctx context.Context, payload any) ([]byte, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("IA POST %s: corpo inválido: %w", chatPath, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+chatPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("IA POST %s: requisição inválida: %w", chatPath, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("IA POST %s: %w", chatPath, err)
	}
	defer res.Body.Close()

	raw, truncated, err := readLimited(res.Body, maxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("IA POST %s: leitura da resposta: %w", chatPath, err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("IA POST %s: status %d: %s", chatPath, res.StatusCode, snippet(raw))
	}
	if truncated {
		return nil, fmt.Errorf("%w: resposta do provedor acima de %s", ErrInvalidResponse, sizeLabel(maxResponseBytes))
	}
	return raw, nil
}

// firstMessage devolve a primeira mensagem da resposta.
func firstMessage(res chatResponse) (messageWire, error) {
	if len(res.Choices) == 0 {
		return messageWire{}, fmt.Errorf("%w: resposta sem escolhas", ErrInvalidResponse)
	}
	return res.Choices[0].Message, nil
}

// parseContent extrai o texto da mensagem. O conteúdo vem como string na
// maioria dos provedores e como lista de partes em alguns. Quando o texto é
// obrigatório (leitura de documento), vazio é falha; numa conversa, vazio é
// resposta legítima de quem só pediu ferramenta.
func parseContent(raw json.RawMessage, required bool) (string, error) {
	empty := func() error {
		if required {
			return fmt.Errorf("%w: resposta vazia do provedor", ErrInvalidResponse)
		}
		return nil
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return "", empty()
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil {
		if strings.TrimSpace(text) == "" {
			return "", empty()
		}
		return text, nil
	}
	var parts []chatPart
	if err := json.Unmarshal(trimmed, &parts); err == nil {
		var builder strings.Builder
		for _, part := range parts {
			if part.Type == "text" {
				builder.WriteString(part.Text)
			}
		}
		if strings.TrimSpace(builder.String()) == "" {
			return "", empty()
		}
		return builder.String(), nil
	}
	return "", fmt.Errorf("%w: resposta do provedor não é texto", ErrInvalidResponse)
}

// validate recusa o documento que não pode ir ao provedor.
func (c *Client) validate(doc Document) (string, error) {
	return c.validateFile(doc.Bytes, doc.Mime, doc.FileName)
}

// validateFile recusa o arquivo que não pode ir ao provedor. As mensagens são
// de validação, em português, e citam apenas nome, mime e tamanho — nunca o
// conteúdo.
func (c *Client) validateFile(content []byte, mime, fileName string) (string, error) {
	name := strings.TrimSpace(fileName)
	if name == "" {
		name = "documento sem nome"
	}
	if len(content) == 0 {
		return "", fmt.Errorf("%w: %s está vazio", ErrInvalidDocument, name)
	}
	if int64(len(content)) > c.maxBytes {
		return "", fmt.Errorf("%w: %s tem %s, acima do limite de %s",
			ErrInvalidDocument, name, sizeLabel(int64(len(content))), sizeLabel(c.maxBytes))
	}
	normalized := normalizeMime(mime)
	if normalized == "" {
		if raw := strings.TrimSpace(mime); raw != "" {
			return "", fmt.Errorf("%w: mime %q não suportado por %s (aceitos: %s)",
				ErrInvalidDocument, raw, name, strings.Join(supportedMimes, ", "))
		}
		return "", fmt.Errorf("%w: %s sem tipo de arquivo (aceitos: %s)",
			ErrInvalidDocument, name, strings.Join(supportedMimes, ", "))
	}
	return normalized, nil
}

// normalizeMime reduz o mime ao formato aceito e devolve vazio quando não é
// suportado. `image/jpg` é o apelido de jpeg que aparece em arquivo de celular.
func normalizeMime(raw string) string {
	mime := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	if mime == "image/jpg" {
		return "image/jpeg"
	}
	for _, supported := range supportedMimes {
		if mime == supported {
			return mime
		}
	}
	return ""
}

// dataURL monta o data URL base64 do documento, no formato que o provedor lê.
func dataURL(mime string, content []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(content)
}

// readLimited lê o corpo até o limite e informa se havia mais bytes.
func readLimited(body io.Reader, limit int64) ([]byte, bool, error) {
	raw, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(raw)) > limit {
		return raw[:limit], true, nil
	}
	return raw, false, nil
}

// snippet devolve um trecho de uma linha do corpo, para a mensagem de erro.
func snippet(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	if len(text) > maxErrorBody {
		return text[:maxErrorBody] + "..."
	}
	return text
}

// sizeLabel descreve um tamanho em MiB, para as mensagens de validação.
func sizeLabel(bytes int64) string {
	return fmt.Sprintf("%.1f MiB", float64(bytes)/(1<<20))
}
