package pdf

import (
	"bytes"
	"fmt"
	"image/png"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Recibo de pagamento.
//
// O documento reproduz o recibo do app atual (`src/utils/pdf.ts`): cabeçalho com
// marca, título e identificação, dados do locatário e do imóvel, resumo do
// recebimento (integral ou parcial), declaração de quitação com o valor por
// extenso, linha de assinatura do locador e rodapé com o QR Code de validação e
// a chave AUTH. Os textos são os mesmos, palavra por palavra.

// ReceiptInput reúne tudo que o recibo imprime.
type ReceiptInput struct {
	ReceiptID       string
	TenantName      string
	TenantCPF       string
	PropertyName    string
	PropertyAddress string
	// Category é a categoria do lançamento (`rent` ou `extra`) e Description é a
	// referência livre do lançamento avulso.
	Category    string
	Description string
	DueDate     string
	PaidAt      string
	Amount      float64
	// PaidAmount só é considerado quando HasPaidAmount é verdadeiro (no banco,
	// `paid_amount` nulo significa "pagou o valor cheio").
	PaidAmount    float64
	HasPaidAmount bool
	LandlordName  string
	LandlordCPF   string
	// Signature pede a imagem da assinatura do locador no documento;
	// SignaturePNG são os bytes dessa imagem (asset `assinatura01.png`).
	Signature    bool
	SignaturePNG []byte
	// LogoPNG é a marca do emissor impressa no cabeçalho (opcional).
	LogoPNG []byte
	// AuthCode é a chave legível (`AUTH-XXXX-XXXX-XXXX-XXXX`) e ValidationURL é o
	// endereço público de validação, ambos responsabilidade da camada de cima.
	AuthCode      string
	ValidationURL string
	// IssuedAt data o documento ("Cabo Frio, 21 de setembro de 2026").
	IssuedAt time.Time
}

// Larguras e corpos do recibo, em pontos (as mesmas medidas do app atual).
const (
	// receiptSignatureMaxWidth é a largura máxima da imagem da assinatura.
	receiptSignatureMaxWidthMM = 63.5
	// receiptSignatureLineWidth é a largura da linha de assinatura.
	receiptSignatureLineWidth = 220.0
	// receiptQRSize é o lado do QR Code do rodapé.
	receiptQRSize = 58.0
	// receiptSignatureNote acompanha o bloco de assinatura e explica o que a
	// assinatura impressa significa: é a imagem da assinatura do locador, não
	// uma assinatura eletrônica com validade jurídica nos termos da ICP-Brasil.
	receiptSignatureNote = "Assinatura visualizada: a assinatura do locador é a imagem impressa neste documento; não é assinatura eletrônica com validade jurídica nos termos da ICP-Brasil."
)

// BuildReceipt monta o modelo do recibo de pagamento.
func BuildReceipt(input ReceiptInput) (Document, error) {
	isExtra := input.Category == string(domain.ReceiptCategoryExtra) || input.Description != ""
	isPartial := input.HasPaidAmount && input.PaidAmount > 0 && input.PaidAmount < input.Amount
	issuedAt := input.IssuedAt
	if issuedAt.IsZero() {
		issuedAt = time.Now()
	}

	blocks := make([]Block, 0, 24)
	blocks = append(blocks, Block{
		Kind: BlockTitle,
		Text: truncate(receiptTitle(input, isExtra), 36),
		Size: 22,
		Bold: true,
	})
	blocks = append(blocks, Block{
		Kind: BlockSubtitle,
		Text: receiptSubtitle(input.ReceiptID, isPartial),
		Size: 11,
		Tone: ToneMuted,
	})
	blocks = append(blocks, Block{Kind: BlockRule, Tone: ToneAccent})

	blocks = append(blocks, Block{Kind: BlockSection, Text: "Recebido de", Size: 11, Tone: ToneAccent})
	blocks = append(blocks, Block{Kind: BlockName, Text: input.TenantName, Size: 22, Bold: true})
	if cpf := strings.TrimSpace(input.TenantCPF); cpf != "" {
		blocks = append(blocks, Block{Kind: BlockSubtitle, Text: "CPF: " + cpf, Size: 12, Tone: ToneMuted})
	}
	if property := strings.TrimSpace(input.PropertyName); property != "" {
		prefix := "Imóvel: "
		if isExtra {
			prefix = "Referência/Imóvel: "
		}
		blocks = append(blocks, Block{Kind: BlockSubtitle, Text: prefix + property, Size: 12, Tone: ToneMuted})
	}

	blocks = append(blocks, Block{Kind: BlockGap, Size: 10})
	blocks = append(blocks, Block{Kind: BlockSection, Text: "Resumo do recebimento", Size: 12, Tone: ToneAccent})
	if isPartial {
		blocks = append(blocks,
			metric("Valor Total", format.FormatCurrency(input.Amount), 14, ToneInk),
			metric("Valor Pago", format.FormatCurrency(input.PaidAmount), 16, ToneAccent),
			metric("Saldo Restante", format.FormatCurrency(remainingBalance(input)), 14, ToneDanger),
			metric("Vencimento", format.FormatDateBR(input.DueDate), 11, ToneInk),
		)
		if input.PaidAt != "" {
			blocks = append(blocks, Block{Kind: BlockText, Text: "Pago em: " + format.FormatDateBR(input.PaidAt), Size: 10, Tone: ToneMuted})
		}
	} else {
		blocks = append(blocks,
			metric("Valor", format.FormatCurrency(input.Amount), 20, ToneAccent),
			metric("Vencimento", format.FormatDateBR(input.DueDate), 12, ToneInk),
			metric("Pago em", format.FormatDateBR(input.PaidAt), 12, ToneInk),
		)
	}

	blocks = append(blocks, Block{Kind: BlockGap, Size: 12})
	blocks = append(blocks, Block{Kind: BlockSection, Text: "Declaração", Size: 14, Tone: ToneInk})
	blocks = append(blocks, paragraph(receiptDeclaration(input, isExtra, isPartial)))

	blocks = append(blocks, Block{Kind: BlockGap, Size: 18})
	blocks = append(blocks, Block{Kind: BlockText, Text: "Cabo Frio, " + formatLongDate(issuedAt), Size: 12})

	if input.Signature && len(input.SignaturePNG) > 0 {
		widthMM, heightMM, err := measureImageMM(input.SignaturePNG, receiptSignatureMaxWidthMM)
		if err != nil {
			return Document{}, fmt.Errorf("medir a assinatura do locador: %w", err)
		}
		blocks = append(blocks, Block{Kind: BlockGap, Size: 12})
		blocks = append(blocks, Block{
			Kind:     BlockImage,
			ImagePNG: input.SignaturePNG,
			WidthMM:  widthMM,
			HeightMM: heightMM,
			Align:    AlignCenter,
		})
	}

	blocks = append(blocks, Block{
		Kind:    BlockSignature,
		Text:    input.LandlordName,
		Align:   AlignCenter,
		Size:    11,
		Bold:    true,
		WidthMM: pointsToMM(receiptSignatureLineWidth),
	})
	if cpf := strings.TrimSpace(input.LandlordCPF); cpf != "" {
		blocks = append(blocks, Block{Kind: BlockSubtitle, Text: "CPF " + cpf, Align: AlignCenter, Size: 10, Tone: ToneMuted})
	}
	blocks = append(blocks, Block{Kind: BlockText, Text: receiptSignatureNote, Size: 8, Tone: ToneMuted})

	blocks = append(blocks, Block{Kind: BlockGap, Size: 14})
	blocks = append(blocks, authBlock(input.ValidationURL, input.AuthCode, receiptAuthCaption, receiptQRSize))

	return newDocument(blocks), nil
}

// receiptAuthCaption é a legenda do selo no recibo.
const receiptAuthCaption = "Aponte a câmera do celular para verificar o registro oficial deste recibo no servidor."

// receiptTitle escolhe o título do recibo: lançamento avulso usa a descrição,
// aluguel mensal usa o título fixo do app.
func receiptTitle(input ReceiptInput, isExtra bool) string {
	if !isExtra {
		return "Recibo de aluguel"
	}
	if description := strings.TrimSpace(input.Description); description != "" {
		return "Recibo - " + description
	}
	return "Recibo de Pagamento"
}

// receiptSubtitle monta a linha de identificação: `Recibo <8 primeiros
// caracteres do id>`, seguida do aviso de pagamento parcial.
func receiptSubtitle(receiptID string, isPartial bool) string {
	identifier := strings.ToUpper(truncate(receiptID, 8))
	subtitle := "Recibo " + identifier
	if isPartial {
		subtitle += " • Pagamento Parcial"
	}
	return subtitle
}

// receiptDeclaration monta a declaração de quitação com o valor em número e por
// extenso, distinguindo aluguel de lançamento avulso e pagamento integral de
// parcial — os quatro textos do app atual.
func receiptDeclaration(input ReceiptInput, isExtra, isPartial bool) string {
	property := receiptPropertyDescription(input.PropertyName, input.PropertyAddress)
	paid := input.Amount
	if isPartial {
		paid = input.PaidAmount
	}

	if isPartial {
		remaining := remainingBalance(input)
		if isExtra {
			return fmt.Sprintf(
				"Declaro, para os devidos fins, o recebimento parcial no valor de %s (%s) de um total de %s referente a %s, vinculado a %s%s, restando o saldo devedor de %s.",
				format.FormatCurrency(paid), domain.MoneyInWords(paid), format.FormatCurrency(input.Amount),
				fallback(strings.TrimSpace(input.Description), "taxa avulsa"),
				fallback(strings.TrimSpace(input.TenantName), "locatário"),
				optionalParenthesis(property),
				format.FormatCurrency(remaining),
			)
		}
		return fmt.Sprintf(
			"Declaro, para os devidos fins, o recebimento parcial no valor de %s (%s) de um total de %s referente ao aluguel do imóvel %s, vinculado ao locatário %s, restando o saldo devedor de %s.",
			format.FormatCurrency(paid), domain.MoneyInWords(paid), format.FormatCurrency(input.Amount),
			property, input.TenantName, format.FormatCurrency(remaining),
		)
	}

	if isExtra {
		return fmt.Sprintf(
			"Declaro, para os devidos fins, o recebimento do valor de %s (%s) referente a %s, vinculado a %s%s.",
			format.FormatCurrency(input.Amount), domain.MoneyInWords(input.Amount),
			fallback(strings.TrimSpace(input.Description), "taxa avulsa"),
			fallback(strings.TrimSpace(input.TenantName), "locatário"),
			optionalParenthesis(property),
		)
	}
	return fmt.Sprintf(
		"Declaro, para os devidos fins, o recebimento do valor de %s (%s) referente ao aluguel do imóvel %s, vinculado ao locatário %s.",
		format.FormatCurrency(input.Amount), domain.MoneyInWords(input.Amount), property, input.TenantName,
	)
}

// receiptPropertyDescription descreve o imóvel como no app: nome e endereço
// unidos, sem repetir a preposição quando o endereço já começa com ela.
func receiptPropertyDescription(name, address string) string {
	name = strings.TrimSpace(name)
	address = strings.TrimSpace(address)
	prefix := ""
	if address != "" && !startsWithPreposition(address) {
		prefix = "na "
	}
	switch {
	case name != "" && address != "":
		return name + " " + prefix + address
	case name != "":
		return name
	case address != "":
		return prefix + address
	default:
		return ""
	}
}

// startsWithPreposition informa se o texto já começa com `na`, `no` ou `em`.
func startsWithPreposition(value string) bool {
	lowered := strings.ToLower(value)
	for _, prefix := range []string{"na ", "no ", "em "} {
		if strings.HasPrefix(lowered, prefix) {
			return true
		}
	}
	return false
}

// remainingBalance devolve o saldo devedor de um pagamento parcial, nunca
// negativo.
func remainingBalance(input ReceiptInput) float64 {
	remaining := input.Amount - input.PaidAmount
	if remaining < 0 {
		return 0
	}
	return remaining
}

// optionalParenthesis embrulha o texto em parênteses quando ele existe.
func optionalParenthesis(value string) string {
	if value == "" {
		return ""
	}
	return " (" + value + ")"
}

// fallback devolve value ou o padrão quando value está vazio.
func fallback(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

// measureImageMM mede a imagem preservando a proporção: a largura pedida é o
// limite e a altura sai do formato do arquivo. A largura devolvida nunca passa
// do limite (imagem mais estreita que o limite mantém o tamanho original em
// pixels convertido para milímetros a 72 dpi).
func measureImageMM(image []byte, maxWidthMM float64) (float64, float64, error) {
	config, err := png.DecodeConfig(bytes.NewReader(image))
	if err != nil {
		return 0, 0, err
	}
	if config.Width <= 0 || config.Height <= 0 {
		return 0, 0, fmt.Errorf("imagem %dx%d sem dimensões", config.Width, config.Height)
	}
	widthMM := pointsToMM(float64(config.Width))
	if widthMM > maxWidthMM {
		widthMM = maxWidthMM
	}
	heightMM := widthMM * float64(config.Height) / float64(config.Width)
	return widthMM, heightMM, nil
}

// mmToPoints e pointsToMM convertem entre milímetros e pontos tipográficos
// (72 pontos por polegada).
func mmToPoints(millimetres float64) float64 { return millimetres * 72 / 25.4 }

func pointsToMM(points float64) float64 { return points * 25.4 / 72 }

// truncate corta o texto em `limit` caracteres (nunca parte um caractere
// multibyte no meio).
func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// formatLongDate escreve a data por extenso em português ("21 de setembro de
// 2026"), como o app atual faz na data de emissão e no fecho do contrato.
func formatLongDate(value time.Time) string {
	return fmt.Sprintf("%d de %s de %d", value.Day(), monthNamesInWords[value.Month()-1], value.Year())
}

// monthNamesInWords são os meses por extenso usados no contrato e na data de
// emissão do recibo.
var monthNamesInWords = [...]string{
	"janeiro", "fevereiro", "março", "abril", "maio", "junho",
	"julho", "agosto", "setembro", "outubro", "novembro", "dezembro",
}
