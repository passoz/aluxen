// Package whatsapp implementa o canal de WhatsApp do Aluxen sobre o WAHA
// (WhatsApp HTTP API), substituindo o robô headless com Puppeteer/Chromium do
// app Bun. Nada de navegador: o pareamento, o estado da sessão e o envio são
// chamadas HTTP, e as mensagens recebidas entram pelo webhook.
//
// Este pacote é infraestrutura pura: conhece o protocolo do WAHA, guarda
// estado em memória (pareamento e fila de envio) e não decide regra de negócio
// — quem decide é internal/application.
package whatsapp

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
)

// ErrNotConfigured indica que a instância não tem o WAHA configurado
// (`WAHA_API_URL` vazio). Sem ele não há sessão, pareamento nem envio.
var ErrNotConfigured = errors.New("WAHA não configurado: defina WAHA_API_URL")

// QRCodeMessage é o texto que acompanha o QR Code no pareamento — o mesmo do
// app atual, para que a tela não mude.
const QRCodeMessage = "Abra o WhatsApp no celular > Aparelhos Conectados > Conectar e aponte a câmera."

// maxErrorBody limita quanto do corpo de erro entra na mensagem de erro.
const maxErrorBody = 300

// SessionState é o estado da sessão informado pelo WAHA.
type SessionState string

const (
	SessionStateUnknown  SessionState = "UNKNOWN"
	SessionStateStarting SessionState = "STARTING"
	SessionStateScanQR   SessionState = "SCAN_QR_CODE"
	SessionStateWorking  SessionState = "WORKING"
	SessionStateFailed   SessionState = "FAILED"
	SessionStateStopped  SessionState = "STOPPED"
)

// ClientConfig configura o cliente do WAHA.
type ClientConfig struct {
	// BaseURL é a base da API do WAHA (ex.: http://waha:3000).
	BaseURL string
	// APIKey habilita o header X-Api-Key quando informada.
	APIKey string
	// Session é a sessão do WAHA usada pela instância (padrão `default`).
	Session string
	// HTTPClient permite injetar o cliente HTTP (testes usam httptest.Server).
	HTTPClient *http.Client
}

// Client fala com a API do WAHA.
type Client struct {
	baseURL string
	apiKey  string
	session string
	http    *http.Client
}

// NewClient cria o cliente do WAHA. Sem BaseURL o cliente continua válido, mas
// todo método devolve ErrNotConfigured — é o que permite subir a instância sem
// WhatsApp configurado.
func NewClient(cfg ClientConfig) *Client {
	session := strings.TrimSpace(cfg.Session)
	if session == "" {
		session = "default"
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:  strings.TrimSpace(cfg.APIKey),
		session: session,
		http:    httpClient,
	}
}

// Configured informa se a instância tem WAHA configurado. É o valor devolvido
// no campo `chromiumAvailable` de /api/whatsapp/status: o nome do campo ficou
// congelado por paridade com o app atual, mas agora ele diz "o canal está
// disponível" (WAHA no lugar do Chromium).
func (c *Client) Configured() bool { return c.baseURL != "" }

// SessionName devolve a sessão do WAHA usada pela instância.
func (c *Client) SessionName() string { return c.session }

// Status consulta o estado da sessão (`GET /api/sessions/{session}`).
func (c *Client) Status(ctx context.Context) (SessionState, error) {
	res, err := c.call(ctx, http.MethodGet, c.sessionPath(), nil, "application/json")
	if err != nil {
		return "", err
	}
	var payload sessionPayload
	if err := expectJSON(res, http.MethodGet, c.sessionPath(), &payload); err != nil {
		return "", err
	}
	return sessionState(payload.Status), nil
}

// StartLogin pede ao WAHA que inicie a sessão (`POST /api/sessions/{session}/start`),
// o que faz o WhatsApp gerar o QR Code do pareamento.
func (c *Client) StartLogin(ctx context.Context) error {
	res, err := c.call(ctx, http.MethodPost, c.sessionPath()+"/start", struct{}{}, "application/json")
	if err != nil {
		return err
	}
	return expectJSON(res, http.MethodPost, c.sessionPath()+"/start", nil)
}

// LoginState devolve o estado do pareamento já traduzido para o vocabulário da
// interface, com o QR Code em base64 quando a sessão está esperando leitura.
func (c *Client) LoginState(ctx context.Context) (LoginState, error) {
	state, err := c.Status(ctx)
	if err != nil {
		return LoginState{}, err
	}

	switch state {
	case SessionStateWorking:
		return LoginState{Status: LoginStateConnected, Message: "WhatsApp conectado com sucesso ao Aluxen!"}, nil
	case SessionStateScanQR:
		qrCode, err := c.qrCode(ctx)
		if err != nil {
			return LoginState{}, err
		}
		return LoginState{Status: LoginStateScanQR, QRCode: qrCode, Message: QRCodeMessage}, nil
	case SessionStateStarting:
		return LoginState{Status: LoginStateStarting, Message: "Iniciando sessão do WhatsApp..."}, nil
	case SessionStateFailed:
		return LoginState{Status: LoginStateError, Error: "O WAHA não conseguiu iniciar a sessão do WhatsApp."}, nil
	default:
		return LoginState{Status: LoginStateIdle}, nil
	}
}

// CancelLogin interrompe a sessão em pareamento (`POST /api/sessions/{session}/stop`).
func (c *Client) CancelLogin(ctx context.Context) error {
	path := c.sessionPath() + "/stop"
	res, err := c.call(ctx, http.MethodPost, path, struct{}{}, "application/json")
	if err != nil {
		return err
	}
	return expectJSON(res, http.MethodPost, path, nil)
}

// Logout encerra a sessão e descarta o pareamento no WAHA
// (`POST /api/sessions/{session}/logout`).
func (c *Client) Logout(ctx context.Context) error {
	path := c.sessionPath() + "/logout"
	res, err := c.call(ctx, http.MethodPost, path, struct{}{}, "application/json")
	if err != nil {
		return err
	}
	return expectJSON(res, http.MethodPost, path, nil)
}

// SendText envia um texto (`POST /api/sendText`) e devolve o id da mensagem no
// WhatsApp, quando o WAHA o informa.
func (c *Client) SendText(ctx context.Context, chatID, text string) (string, error) {
	payload := map[string]string{"session": c.session, "chatId": chatID, "text": text}
	res, err := c.call(ctx, http.MethodPost, "/api/sendText", payload, "application/json")
	if err != nil {
		return "", err
	}
	var body struct {
		ID json.RawMessage `json:"id"`
	}
	if err := expectJSON(res, http.MethodPost, "/api/sendText", &body); err != nil {
		return "", err
	}
	return messageID(body.ID), nil
}

// ── Protocolo ──────────────────────────────────────────────────────

// sessionPayload é o subconjunto da sessão do WAHA que usamos.
type sessionPayload struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// sessionState normaliza o estado devolvido pelo WAHA.
func sessionState(raw string) SessionState {
	switch SessionState(strings.ToUpper(strings.TrimSpace(raw))) {
	case SessionStateStarting:
		return SessionStateStarting
	case SessionStateScanQR:
		return SessionStateScanQR
	case SessionStateWorking:
		return SessionStateWorking
	case SessionStateFailed:
		return SessionStateFailed
	case SessionStateStopped:
		return SessionStateStopped
	default:
		return SessionStateUnknown
	}
}

func (c *Client) sessionPath() string {
	return "/api/sessions/" + c.session
}

// response é a resposta crua de uma chamada ao WAHA.
type response struct {
	status int
	header http.Header
	body   []byte
}

// call executa a chamada HTTP. Erro de transporte vira erro embrulhado; status
// fora de 2xx vira erro com o trecho do corpo devolvido pelo WAHA.
func (c *Client) call(ctx context.Context, method, path string, payload any, accept string) (response, error) {
	if !c.Configured() {
		return response{}, ErrNotConfigured
	}

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return response{}, fmt.Errorf("WAHA %s %s: corpo inválido: %w", method, path, err)
		}
		body = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return response{}, fmt.Errorf("WAHA %s %s: requisição inválida: %w", method, path, err)
	}
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		request.Header.Set("X-Api-Key", c.apiKey)
	}

	httpResponse, err := c.http.Do(request)
	if err != nil {
		return response{}, fmt.Errorf("WAHA %s %s: %w", method, path, err)
	}
	defer func() { _ = httpResponse.Body.Close() }()

	raw, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return response{}, fmt.Errorf("WAHA %s %s: leitura da resposta: %w", method, path, err)
	}

	res := response{status: httpResponse.StatusCode, header: httpResponse.Header, body: raw}
	if res.status < 200 || res.status > 299 {
		return res, fmt.Errorf("WAHA %s %s: status %d: %s", method, path, res.status, snippet(raw))
	}
	return res, nil
}

// expectJSON valida a resposta (já 2xx) e decodifica o corpo em out, quando pedido.
func expectJSON(res response, method, path string, out any) error {
	if out == nil {
		return nil
	}
	if len(bytes.TrimSpace(res.body)) == 0 {
		return fmt.Errorf("WAHA %s %s: resposta vazia", method, path)
	}
	if err := json.Unmarshal(res.body, out); err != nil {
		return fmt.Errorf("WAHA %s %s: corpo inválido: %w", method, path, err)
	}
	return nil
}

// snippet devolve um trecho de uma linha do corpo, para a mensagem de erro.
func snippet(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	if len(text) > maxErrorBody {
		return text[:maxErrorBody] + "..."
	}
	return text
}

// messageID extrai o id da mensagem da resposta do WAHA, que varia conforme a
// versão: string (`"ABC"`) ou objeto (`{"_serialized": "ABC"}` / `{"id": "ABC"}`).
func messageID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var object struct {
		Serialized string `json:"_serialized"`
		ID         string `json:"id"`
	}
	if err := json.Unmarshal(raw, &object); err == nil {
		if object.Serialized != "" {
			return object.Serialized
		}
		return object.ID
	}
	return ""
}

// qrCode lê o QR Code do pareamento (`GET /api/{session}/auth/qr?format=image`)
// e devolve um data URL PNG, do mesmo jeito que o app atual entrega ao painel.
func (c *Client) qrCode(ctx context.Context) (string, error) {
	path := "/api/" + c.session + "/auth/qr?format=image"
	res, err := c.call(ctx, http.MethodGet, path, nil, "image/png")
	if err != nil {
		return "", err
	}

	if strings.HasPrefix(res.header.Get("Content-Type"), "image/") {
		if len(res.body) == 0 {
			return "", fmt.Errorf("WAHA GET %s: imagem vazia", path)
		}
		return pngDataURL(res.body), nil
	}

	// Algumas versões devolvem JSON (`{"value": "..."}`) mesmo com format=image:
	// o valor pode ser o data URL pronto ou o texto do QR Code.
	var payload struct {
		Value string `json:"value"`
	}
	if err := expectJSON(res, http.MethodGet, path, &payload); err != nil {
		return "", err
	}
	if strings.HasPrefix(payload.Value, "data:image/") {
		return payload.Value, nil
	}
	if payload.Value == "" {
		return "", fmt.Errorf("WAHA GET %s: QR Code ausente na resposta", path)
	}
	return encodePNGDataURL(payload.Value)
}

func pngDataURL(image []byte) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(image)
}
