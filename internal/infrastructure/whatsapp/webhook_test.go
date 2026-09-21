package whatsapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

// sign devolve a assinatura hex do corpo, no formato que o WAHA envia.
func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// messagePayload monta o envelope de uma mensagem recebida.
func messagePayload(event string) []byte {
	return []byte(`{
		"event": "` + event + `",
		"session": "aluxen",
		"payload": {
			"id": {"fromMe": false, "_serialized": "false_5511999999999@c.us_ABC"},
			"from": "5511999999999@c.us",
			"to": "5521988887777@c.us",
			"body": "Já paguei o aluguel",
			"timestamp": 1790000000,
			"fromMe": false,
			"hasMedia": false,
			"pushName": "Maria Silva"
		}
	}`)
}

func TestVerifySignatureAcceptsHexDigest(t *testing.T) {
	body := messagePayload(EventMessage)

	if !VerifySignature("s3gredo", body, sign("s3gredo", body)) {
		t.Fatal("assinatura hex válida foi recusada")
	}
}

func TestVerifySignatureAcceptsPrefixedAndBase64Digest(t *testing.T) {
	body := messagePayload(EventMessage)
	mac := hmac.New(sha256.New, []byte("s3gredo"))
	mac.Write(body)
	base64Digest := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if !VerifySignature("s3gredo", body, base64Digest) {
		t.Fatal("assinatura em base64 foi recusada")
	}
	if !VerifySignature("s3gredo", body, "sha256="+base64Digest) {
		t.Fatal("assinatura com prefixo sha256= foi recusada")
	}
}

func TestVerifySignatureRejectsWrongSignature(t *testing.T) {
	body := messagePayload(EventMessage)

	cases := map[string]string{
		"assinatura de outro segredo": sign("outro", body),
		"assinatura de outro corpo":   sign("s3gredo", []byte(`{"event":"message"}`)),
		"header vazio":                "",
		"lixo":                        "nem-hex-nem-base64!",
		"corpo alterado depois":       sign("s3gredo", []byte("outro corpo")),
	}
	for name, header := range cases {
		if VerifySignature("s3gredo", body, header) {
			t.Fatalf("%s: assinatura inválida foi aceita", name)
		}
	}
}

func TestVerifySignatureWithoutSecretIsFailClosed(t *testing.T) {
	body := messagePayload(EventMessage)

	if VerifySignature("", body, sign("", body)) {
		t.Fatal("webhook sem segredo configurado não pode ser aceito (fail-closed)")
	}
	if VerifySignature("   ", body, sign("   ", body)) {
		t.Fatal("segredo em branco equivale a sem segredo")
	}
}

func TestWebhookPolicyDemoWithoutSecretIsOpen(t *testing.T) {
	body := messagePayload(EventMessage)

	demo := WebhookPolicy{Secret: "", Demo: true}
	if !demo.Open() || !demo.Allowed(body, "") {
		t.Fatal("modo demo sem segredo precisa aceitar o webhook para a demonstração funcionar")
	}

	client := WebhookPolicy{Secret: "", Demo: false}
	if client.Open() || client.Allowed(body, "") {
		t.Fatal("modo client sem segredo continua fail-closed")
	}
}

func TestWebhookPolicyWithSecretAlwaysRequiresSignature(t *testing.T) {
	body := messagePayload(EventMessage)

	policy := WebhookPolicy{Secret: "s3gredo", Demo: true}
	if policy.Open() {
		t.Fatal("com segredo configurado o webhook não está aberto")
	}
	if !policy.Allowed(body, sign("s3gredo", body)) {
		t.Fatal("assinatura válida foi recusada")
	}
	if policy.Allowed(body, sign("outro", body)) || policy.Allowed(body, "") {
		t.Fatal("assinatura inválida foi aceita mesmo em modo demo")
	}
}

func TestParseWebhookExtractsMessage(t *testing.T) {
	message, err := ParseWebhook(messagePayload(EventMessage))
	if err != nil {
		t.Fatalf("ParseWebhook devolveu erro: %v", err)
	}

	if message.Event != EventMessage || message.Session != "aluxen" {
		t.Fatalf("envelope = %+v", message)
	}
	if message.ID != "false_5511999999999@c.us_ABC" {
		t.Fatalf("id = %q", message.ID)
	}
	if message.ChatID != "5511999999999@c.us" || message.Phone != "5511999999999" {
		t.Fatalf("remetente = %q / %q", message.ChatID, message.Phone)
	}
	if message.Text != "Já paguei o aluguel" {
		t.Fatalf("texto = %q", message.Text)
	}
	if message.FromMe || message.HasMedia {
		t.Fatalf("sinalizadores inesperados: %+v", message)
	}
	if message.ContactName != "Maria Silva" {
		t.Fatalf("nome do contato = %q", message.ContactName)
	}
	expected := time.Unix(1790000000, 0).UTC()
	if !message.Timestamp.Equal(expected) {
		t.Fatalf("timestamp = %v, esperado %v", message.Timestamp, expected)
	}
}

func TestParseWebhookAcceptsMessageAny(t *testing.T) {
	message, err := ParseWebhook(messagePayload(EventMessageAny))
	if err != nil {
		t.Fatalf("ParseWebhook devolveu erro: %v", err)
	}
	if message.Event != EventMessageAny {
		t.Fatalf("evento = %q", message.Event)
	}
}

func TestParseWebhookUsesDestinationOnOurOwnEcho(t *testing.T) {
	body := []byte(`{
		"event": "message.any",
		"session": "aluxen",
		"payload": {
			"id": "true_5511999999999@c.us_ABC",
			"from": "5521988887777@c.us",
			"to": "5511999999999@c.us",
			"fromMe": true,
			"body": "Segue o recibo"
		}
	}`)

	message, err := ParseWebhook(body)
	if err != nil {
		t.Fatalf("ParseWebhook devolveu erro: %v", err)
	}
	if !message.FromMe {
		t.Fatal("eco do nosso envio precisa vir marcado como FromMe")
	}
	if message.ChatID != "5511999999999@c.us" || message.Phone != "5511999999999" {
		t.Fatalf("destinatário = %q / %q", message.ChatID, message.Phone)
	}
}

func TestParseWebhookUsesParticipantOfGroupChat(t *testing.T) {
	body := []byte(`{
		"event": "message",
		"session": "aluxen",
		"payload": {
			"id": "false_123456789-987654@g.us_ABC",
			"from": "123456789-987654@g.us",
			"participant": "5511999999999@c.us",
			"body": "bom dia"
		}
	}`)

	message, err := ParseWebhook(body)
	if err != nil {
		t.Fatalf("ParseWebhook devolveu erro: %v", err)
	}
	if message.ChatID != "123456789-987654@g.us" {
		t.Fatalf("conversa = %q", message.ChatID)
	}
	if message.Phone != "5511999999999" {
		t.Fatalf("telefone do participante = %q", message.Phone)
	}
}

func TestParseWebhookIgnoresOtherEvents(t *testing.T) {
	body := []byte(`{"event":"session.status","session":"aluxen","payload":{"status":"WORKING"}}`)

	_, err := ParseWebhook(body)
	if !errors.Is(err, ErrIgnoredEvent) {
		t.Fatalf("erro = %v, esperado ErrIgnoredEvent", err)
	}
}

func TestParseWebhookRejectsMalformedBody(t *testing.T) {
	if _, err := ParseWebhook([]byte(`{"event":`)); err == nil {
		t.Fatal("esperava erro para corpo ilegível")
	}
}

func TestParseWebhookWithoutTimestamp(t *testing.T) {
	body := []byte(`{"event":"message","payload":{"id":"ABC","from":"5511999999999@c.us","body":"oi"}}`)

	message, err := ParseWebhook(body)
	if err != nil {
		t.Fatalf("ParseWebhook devolveu erro: %v", err)
	}
	if !message.Timestamp.IsZero() {
		t.Fatalf("timestamp = %v, esperado zero", message.Timestamp)
	}
	if message.ID != "ABC" {
		t.Fatalf("id = %q", message.ID)
	}
}
