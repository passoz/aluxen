package whatsapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/application"
)

// Webhook do WAHA: por aqui entram as mensagens que o locatário manda. Duas
// responsabilidades separadas — autenticar o remetente (HMAC do corpo) e
// entender o evento (envelope do WAHA).

// Eventos do WAHA que são mensagem. Os demais (session.status, ack, presença)
// não interessam ao Aluxen e são ignorados sem erro de protocolo.
const (
	EventMessage    = "message"
	EventMessageAny = "message.any"
)

// ErrIgnoredEvent indica um webhook legítimo que não é mensagem a processar.
var ErrIgnoredEvent = errors.New("evento de webhook ignorado")

// VerifySignature confere a assinatura HMAC-SHA256 do corpo cru do webhook,
// comparando em tempo constante. O WAHA manda o hex do digest no header
// (`X-Webhook-Hmac`); também aceitamos base64 e o prefixo `sha256=`.
//
// Sem segredo configurado a resposta é `false` — fail-closed: um webhook sem
// autenticação é recusado por padrão. A única exceção é o modo demo sem segredo,
// tratado em WebhookPolicy.Allowed.
func VerifySignature(secret string, body []byte, header string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return false
	}
	provided := strings.TrimSpace(header)
	if provided == "" {
		return false
	}
	lower := strings.ToLower(provided)
	for _, prefix := range []string{"sha256=", "hmac-sha256="} {
		if strings.HasPrefix(lower, prefix) {
			provided = strings.TrimSpace(provided[len(prefix):])
			break
		}
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	if decoded, err := hex.DecodeString(provided); err == nil && hmac.Equal(decoded, expected) {
		return true
	}
	if decoded, err := base64.StdEncoding.DecodeString(provided); err == nil && hmac.Equal(decoded, expected) {
		return true
	}
	return false
}

// WebhookPolicy é a decisão de configuração sobre autenticar o webhook.
type WebhookPolicy struct {
	// Secret é o segredo combinado com o WAHA (`WAHA_WEBHOOK_SECRET`).
	Secret string
	// Demo indica o modo demonstração (`WHATSAPP_MODE=demo`).
	Demo bool
}

// Allowed diz se o webhook pode ser processado.
//
// Decisão registrada: sem assinatura válida não passa (fail-closed), com uma
// única exceção explícita — modo demo **e** segredo vazio. A demonstração sobe
// sem `WAHA_WEBHOOK_SECRET` e precisa receber mensagem para o canal
// bidirecional funcionar; em `client`, ou com segredo configurado, assinatura
// inválida é recusa. O modo demo já é declarado como canal não-oficial, e o
// handler registra um aviso quando aceita um webhook aberto.
func (p WebhookPolicy) Allowed(body []byte, header string) bool {
	if p.Open() {
		return true
	}
	return VerifySignature(p.Secret, body, header)
}

// Open informa se o webhook está aceitando qualquer remetente (modo demo sem
// segredo). Quem chama registra o aviso no log.
func (p WebhookPolicy) Open() bool {
	return strings.TrimSpace(p.Secret) == "" && p.Demo
}

// webhookEnvelope é o envelope do WAHA: evento, sessão e o payload da mensagem.
type webhookEnvelope struct {
	Event   string `json:"event"`
	Session string `json:"session"`
	Payload struct {
		ID          json.RawMessage `json:"id"`
		From        string          `json:"from"`
		To          string          `json:"to"`
		Participant string          `json:"participant"`
		FromMe      bool            `json:"fromMe"`
		Body        string          `json:"body"`
		Timestamp   int64           `json:"timestamp"`
		PushName    string          `json:"pushName"`
		NotifyName  string          `json:"notifyName"`
		HasMedia    bool            `json:"hasMedia"`
	} `json:"payload"`
}

// ParseWebhook entende o envelope do WAHA e extrai remetente, texto, id e
// instante da mensagem. Evento que não seja mensagem devolve ErrIgnoredEvent:
// o WAHA entrega também avisos de sessão e confirmações de leitura.
//
// O tipo devolvido é o do caso de uso (application.IncomingMessage): a
// infraestrutura preenche o contrato, e a aplicação persiste e audita sem
// espelhar o mesmo struct em duas camadas.
func ParseWebhook(body []byte) (application.IncomingMessage, error) {
	var envelope webhookEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return application.IncomingMessage{}, fmt.Errorf("webhook do WhatsApp ilegível: %w", err)
	}
	if envelope.Event != EventMessage && envelope.Event != EventMessageAny {
		return application.IncomingMessage{}, fmt.Errorf("%w: %s", ErrIgnoredEvent, envelope.Event)
	}

	chatID := envelope.Payload.From
	if envelope.Payload.FromMe {
		chatID = envelope.Payload.To
	}
	// Em conversa de grupo o `from` é o grupo; quem escreveu é o `participant`.
	senderID := envelope.Payload.Participant
	if senderID == "" {
		senderID = chatID
	}

	message := application.IncomingMessage{
		Event:       envelope.Event,
		Session:     envelope.Session,
		ID:          messageID(envelope.Payload.ID),
		ChatID:      chatID,
		Phone:       chatPhone(senderID),
		ContactName: firstNonEmpty(envelope.Payload.PushName, envelope.Payload.NotifyName),
		Body:        envelope.Payload.Body,
		FromMe:      envelope.Payload.FromMe,
		HasMedia:    envelope.Payload.HasMedia,
	}
	if envelope.Payload.Timestamp > 0 {
		message.Timestamp = time.Unix(envelope.Payload.Timestamp, 0).UTC()
	}
	return message, nil
}

// chatPhone devolve só os dígitos de um identificador de conversa do WAHA
// (`5511999999999@c.us` → `5511999999999`).
func chatPhone(chatID string) string {
	if at := strings.IndexByte(chatID, '@'); at >= 0 {
		chatID = chatID[:at]
	}
	return digits(chatID)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
