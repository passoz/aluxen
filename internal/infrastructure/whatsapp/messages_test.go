package whatsapp

import (
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/application"
)

func TestSanitizePhone(t *testing.T) {
	cases := map[string]string{
		"(11) 99999-9999":   "5511999999999",
		"11999999999":       "5511999999999",
		"5511999999999":     "5511999999999",
		"+55 11 99999-9999": "5511999999999",
		"5511 9999-9999":    "551199999999",
		"":                  "",
		"sem número":        "",
	}
	for input, expected := range cases {
		if got := SanitizePhone(input); got != expected {
			t.Fatalf("SanitizePhone(%q) = %q, esperado %q", input, got, expected)
		}
	}
}

func TestDirectURL(t *testing.T) {
	if got := DirectURL("(11) 99999-9999"); got != "https://wa.me/5511999999999" {
		t.Fatalf("DirectURL = %q", got)
	}
	if got := DirectURL(""); got != "" {
		t.Fatalf("DirectURL sem dígitos = %q, esperado vazio", got)
	}
}

func TestDirectURLWithMessage(t *testing.T) {
	url := DirectURLWithMessage("11999999999", "Olá, tudo bem?\nSegue o recibo & o link")
	expected := "https://wa.me/5511999999999?text=" +
		"Ol%C3%A1%2C%20tudo%20bem%3F%0ASegue%20o%20recibo%20%26%20o%20link"
	if url != expected {
		t.Fatalf("DirectURLWithMessage = %q, esperado %q", url, expected)
	}
	if got := DirectURLWithMessage("", "oi"); got != "" {
		t.Fatalf("link sem telefone = %q, esperado vazio", got)
	}

	// O `encodeURIComponent` do app preserva estes caracteres: a mensagem do
	// recibo usa `*negrito*` e parênteses, que precisam sair literais no link.
	withMarkers := DirectURLWithMessage("11999999999", "*Quitado* (R$ 1.200,00)! ~ok~")
	if !strings.HasSuffix(withMarkers, "?text=*Quitado*%20(R%24%201.200%2C00)!%20~ok~") {
		t.Fatalf("marcadores da mensagem foram codificados: %q", withMarkers)
	}
}

func TestChatID(t *testing.T) {
	if got := ChatID("(11) 99999-9999"); got != "5511999999999@c.us" {
		t.Fatalf("ChatID = %q", got)
	}
	if got := ChatID(""); got != "" {
		t.Fatalf("ChatID sem dígitos = %q, esperado vazio", got)
	}
}

func TestReceiptMessagePending(t *testing.T) {
	message := ReceiptMessage(application.WhatsAppReceiptMessage{
		TenantName:    "Maria Silva",
		PropertyName:  "Casa Centro",
		DueDate:       "10/10/2026",
		Amount:        "R$ 1.200,00",
		AmountRaw:     1200,
		LandlordName:  "João Locador",
		PDFURL:        "https://aluxen.test/pdf?id=r1&token=abc",
		ValidationURL: "https://aluxen.test/validar/recibo/r1?token=abc",
	})

	expected := `Olá, *Maria Silva*! Tudo bem?

Segue o comprovante do seu aluguel referente ao imóvel *Casa Centro*:

📄 *Recibo de Aluguel*
💰 *Valor*: R$ 1.200,00
📅 *Vencimento*: 10/10/2026
⏳ *Status*: Aguardando pagamento

🔗 *Visualizar / Baixar Recibo (PDF)*:
https://aluxen.test/pdf?id=r1&token=abc

🔒 *Verificação de Autenticidade Digital*:
https://aluxen.test/validar/recibo/r1?token=abc

Qualquer dúvida, estou à disposição!
Atenciosamente,
*João Locador*`

	if message != expected {
		t.Fatalf("mensagem do recibo pendente:\n%q\nesperado:\n%q", message, expected)
	}
}

func TestReceiptMessagePaid(t *testing.T) {
	paid := 1200.0
	message := ReceiptMessage(application.WhatsAppReceiptMessage{
		TenantName:    "Maria Silva",
		PropertyName:  "Casa Centro",
		DueDate:       "10/10/2026",
		Amount:        "R$ 1.200,00",
		AmountRaw:     1200,
		PaidAmount:    "R$ 1.200,00",
		PaidAmountRaw: &paid,
		PaidAt:        "05/10/2026 14:30",
	})

	if !strings.Contains(message, "✅ *Status*: Quitado em 05/10/2026 14:30") {
		t.Fatalf("mensagem sem o status de quitado:\n%s", message)
	}
	if strings.Contains(message, "💵 *Valor Pago*") {
		t.Fatalf("recibo quitado não deve listar valor pago:\n%s", message)
	}
	if !strings.Contains(message, "*Locador*") {
		t.Fatalf("mensagem sem o locador padrão:\n%s", message)
	}
}

func TestReceiptMessagePartial(t *testing.T) {
	partial := 600.0
	message := ReceiptMessage(application.WhatsAppReceiptMessage{
		TenantName:    "Maria Silva",
		PropertyName:  "Casa Centro",
		DueDate:       "10/10/2026",
		Amount:        "R$ 1.200,00",
		AmountRaw:     1200,
		PaidAmount:    "R$ 600,00",
		PaidAmountRaw: &partial,
		PaidAt:        "05/10/2026 14:30",
	})

	if !strings.Contains(message, "🟡 *Status*: Pagamento parcial (R$ 600,00 de R$ 1.200,00) registrado em 05/10/2026 14:30") {
		t.Fatalf("mensagem sem o status parcial:\n%s", message)
	}
	if !strings.Contains(message, "💵 *Valor Pago*: R$ 600,00") {
		t.Fatalf("mensagem sem o valor pago:\n%s", message)
	}
}

func TestReceiptMessageExtraWithoutPaymentDate(t *testing.T) {
	partial := 100.0
	message := ReceiptMessage(application.WhatsAppReceiptMessage{
		TenantName:    "Maria Silva",
		PropertyName:  "Casa Centro",
		DueDate:       "10/10/2026",
		Amount:        "R$ 200,00",
		AmountRaw:     200,
		PaidAmount:    "R$ 100,00",
		PaidAmountRaw: &partial,
		Category:      "extra",
		Description:   "Taxa de limpeza",
	})

	if !strings.Contains(message, "🟡 *Status*: Pagamento parcial (R$ 100,00 de R$ 200,00)\n") {
		t.Fatalf("pagamento parcial sem data deve omitir o registro:\n%s", message)
	}
	if !strings.Contains(message, "Segue o comprovante referente a *Taxa de limpeza* (Imóvel: *Casa Centro*):") {
		t.Fatalf("assunto do lançamento avulso inesperado:\n%s", message)
	}
	if !strings.Contains(message, "📄 *Recibo - Taxa de limpeza*") {
		t.Fatalf("título do lançamento avulso inesperado:\n%s", message)
	}
}

func TestReceiptMessageExtraWithoutDescription(t *testing.T) {
	message := ReceiptMessage(application.WhatsAppReceiptMessage{
		TenantName:   "Maria Silva",
		PropertyName: "Casa Centro",
		DueDate:      "10/10/2026",
		Amount:       "R$ 200,00",
		Category:     "extra",
	})

	if !strings.Contains(message, "Segue o comprovante referente a *cobrança avulsa* (Imóvel: *Casa Centro*):") {
		t.Fatalf("assunto sem descrição inesperado:\n%s", message)
	}
	if !strings.Contains(message, "📄 *Recibo de Pagamento*") {
		t.Fatalf("título sem descrição inesperado:\n%s", message)
	}
}

func TestReceiptMessageExtraWithoutProperty(t *testing.T) {
	message := ReceiptMessage(application.WhatsAppReceiptMessage{
		TenantName:  "Maria Silva",
		DueDate:     "10/10/2026",
		Amount:      "R$ 200,00",
		Description: "Conserto do chuveiro",
	})

	if !strings.Contains(message, "Segue o comprovante referente a *Conserto do chuveiro*:\n") {
		t.Fatalf("assunto sem imóvel inesperado:\n%s", message)
	}
}

func TestContractMessage(t *testing.T) {
	message := ContractMessage(application.WhatsAppContractMessage{
		TenantName:    "Maria Silva",
		PropertyName:  "Casa Centro",
		StartDate:     "01/01/2026",
		EndDate:       "31/12/2026",
		DueDay:        5,
		RentAmount:    "R$ 1.500,00",
		LandlordName:  "João Locador",
		PDFURL:        "https://aluxen.test/contract-pdf?id=l1&token=abc",
		ValidationURL: "https://aluxen.test/validar/contrato/l1?token=abc",
	})

	expected := `Olá, *Maria Silva*! Tudo bem?

Segue o seu contrato de locação residencial referente ao imóvel *Casa Centro*:

📄 *Contrato de Locação Residencial*
💰 *Valor do Aluguel*: R$ 1.500,00
📅 *Vencimento Mensal*: Todo dia 5
🗓️ *Vigência*: 01/01/2026 a 31/12/2026

🔗 *Visualizar / Baixar Contrato (PDF)*:
https://aluxen.test/contract-pdf?id=l1&token=abc

🔒 *Verificação de Autenticidade Digital*:
https://aluxen.test/validar/contrato/l1?token=abc

Qualquer dúvida, estou à disposição!
Atenciosamente,
*João Locador*`

	if message != expected {
		t.Fatalf("mensagem do contrato:\n%q\nesperado:\n%q", message, expected)
	}
}
