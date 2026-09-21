package whatsapp

import (
	"fmt"
	"strings"

	"github.com/passoz/aluxen/internal/application"
)

// Textos das mensagens do WhatsApp. São os mesmos que o app atual envia: o
// locatário recebe exatamente o mesmo conteúdo, com os mesmos emojis e a mesma
// formatação (`*negrito*`).
//
// Toda montagem é função pura de strings já formatadas (data em dd/mm/aaaa,
// valor em Real) — quem formata é internal/domain/format, no caso de uso.

// SanitizePhone devolve o número no formato que o WhatsApp espera: só dígitos,
// com o código do Brasil à frente quando ele não vier explícito. É o
// `formatWhatsAppNumber` do app atual.
func SanitizePhone(raw string) string {
	clean := digits(raw)
	if clean == "" {
		return ""
	}
	if strings.HasPrefix(clean, "55") && len(clean) >= 12 {
		return clean
	}
	return "55" + clean
}

// digits devolve apenas os dígitos de um texto.
func digits(value string) string {
	var out strings.Builder
	for _, char := range value {
		if char >= '0' && char <= '9' {
			out.WriteRune(char)
		}
	}
	return out.String()
}

// DirectURL devolve o link direto da conversa (`https://wa.me/<dígitos>`), ou
// string vazia quando o telefone não tem dígito nenhum.
func DirectURL(phone string) string {
	clean := SanitizePhone(phone)
	if clean == "" {
		return ""
	}
	return "https://wa.me/" + clean
}

// DirectURLWithMessage devolve o link direto com a mensagem pré-preenchida — é o
// `directUrl` que /api/whatsapp/preview devolve. A codificação é a mesma do
// `encodeURIComponent` do app atual, byte a byte.
func DirectURLWithMessage(phone, message string) string {
	base := DirectURL(phone)
	if base == "" {
		return ""
	}
	return base + "?text=" + encodeURIComponent(message)
}

// encodeURIComponent codifica um valor como componente de URL com o mesmo
// conjunto de caracteres preservados pelo `encodeURIComponent` do JavaScript
// (`A-Z a-z 0-9 - _ . ! ~ * ' ( )`), e não com o `+` do QueryEscape. Os links
// `wa.me` do app atual saem exatamente assim.
func encodeURIComponent(value string) string {
	const preserved = "-_.!~*'()"
	var out strings.Builder
	for index := range len(value) {
		char := value[index]
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
			out.WriteByte(char)
		case strings.IndexByte(preserved, char) >= 0:
			out.WriteByte(char)
		default:
			out.WriteString(fmt.Sprintf("%%%02X", char))
		}
	}
	return out.String()
}

// ChatID devolve o identificador de conversa que o WAHA usa no envio.
func ChatID(phone string) string {
	clean := SanitizePhone(phone)
	if clean == "" {
		return ""
	}
	return clean + "@c.us"
}

// ReceiptMessage monta a mensagem do recibo — o `buildReceiptWhatsAppMessage`
// do app atual, linha por linha.
func ReceiptMessage(in application.WhatsAppReceiptMessage) string {
	isExtra := in.Category == "extra" || in.Description != ""
	isPartial := in.PaidAmountRaw != nil && *in.PaidAmountRaw > 0 && *in.PaidAmountRaw < in.AmountRaw
	isPaid := in.PaidAt != "" && !isPartial

	statusLine := "⏳ *Status*: Aguardando pagamento"
	switch {
	case isPartial:
		statusLine = fmt.Sprintf("🟡 *Status*: Pagamento parcial (%s de %s)", in.PaidAmount, in.Amount)
		if in.PaidAt != "" {
			statusLine += " registrado em " + in.PaidAt
		}
	case isPaid:
		statusLine = "✅ *Status*: Quitado em " + in.PaidAt
	}

	docTitle := "Recibo de Aluguel"
	subjectIntro := fmt.Sprintf("Segue o comprovante do seu aluguel referente ao imóvel *%s*:", in.PropertyName)
	if isExtra {
		docTitle = "Recibo de Pagamento"
		if in.Description != "" {
			docTitle = "Recibo - " + in.Description
		}
		subject := "*cobrança avulsa*"
		if in.Description != "" {
			subject = "*" + in.Description + "*"
		}
		if in.PropertyName != "" {
			subject += fmt.Sprintf(" (Imóvel: *%s*)", in.PropertyName)
		}
		subjectIntro = "Segue o comprovante referente a " + subject + ":"
	}

	partialLine := ""
	if isPartial {
		partialLine = "\n💵 *Valor Pago*: " + in.PaidAmount
	}

	landlordName := in.LandlordName
	if landlordName == "" {
		landlordName = "Locador"
	}

	return fmt.Sprintf(`Olá, *%s*! Tudo bem?

%s

📄 *%s*
💰 *Valor*: %s%s
📅 *Vencimento*: %s
%s

🔗 *Visualizar / Baixar Recibo (PDF)*:
%s

🔒 *Verificação de Autenticidade Digital*:
%s

Qualquer dúvida, estou à disposição!
Atenciosamente,
*%s*`,
		in.TenantName, subjectIntro, docTitle, in.Amount, partialLine, in.DueDate, statusLine,
		in.PDFURL, in.ValidationURL, landlordName)
}

// ContractMessage monta a mensagem do contrato — o
// `buildContractWhatsAppMessage` do app atual, linha por linha.
func ContractMessage(in application.WhatsAppContractMessage) string {
	landlordName := in.LandlordName
	if landlordName == "" {
		landlordName = "Locador"
	}

	return fmt.Sprintf(`Olá, *%s*! Tudo bem?

Segue o seu contrato de locação residencial referente ao imóvel *%s*:

📄 *Contrato de Locação Residencial*
💰 *Valor do Aluguel*: %s
📅 *Vencimento Mensal*: Todo dia %d
🗓️ *Vigência*: %s a %s

🔗 *Visualizar / Baixar Contrato (PDF)*:
%s

🔒 *Verificação de Autenticidade Digital*:
%s

Qualquer dúvida, estou à disposição!
Atenciosamente,
*%s*`,
		in.TenantName, in.PropertyName, in.RentAmount, in.DueDay, in.StartDate, in.EndDate,
		in.PDFURL, in.ValidationURL, landlordName)
}

// Messages implementa application.WhatsAppMessages sobre as funções puras deste
// pacote: é o valor sem estado que o composition root injeta em `Deps.Messages`,
// permitindo ao caso de uso montar a mensagem sem depender da infraestrutura.
type Messages struct{}

var _ application.WhatsAppMessages = Messages{}

// SanitizePhone devolve o número no formato do WhatsApp.
func (Messages) SanitizePhone(raw string) string { return SanitizePhone(raw) }

// DirectURLWithMessage devolve o link direto com a mensagem pré-preenchida.
func (Messages) DirectURLWithMessage(phone, message string) string {
	return DirectURLWithMessage(phone, message)
}

// Receipt monta a mensagem do recibo.
func (Messages) Receipt(message application.WhatsAppReceiptMessage) string {
	return ReceiptMessage(message)
}

// Contract monta a mensagem do contrato.
func (Messages) Contract(message application.WhatsAppContractMessage) string {
	return ContractMessage(message)
}
