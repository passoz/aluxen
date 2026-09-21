package pdf

import (
	"fmt"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Recibo de devolução de caução (temporada).
//
// O documento registra as partes e o imóvel, o valor da caução, o valor
// devolvido (por extenso), o que ficou retido quando houver, a situação da
// caução e a data da devolução, seguido da declaração de devolução, da linha de
// assinatura do locador e do selo de autenticidade (QR Code da página pública e
// chave AUTH).

// DepositReceiptInput reúne tudo que o recibo de devolução de caução imprime.
type DepositReceiptInput struct {
	LeaseID      string
	TenantName   string
	TenantCPF    string
	PropertyName string
	// PropertyAddress é o endereço completo do imóvel.
	PropertyAddress string
	LandlordName    string
	LandlordCPF     string

	// Amount é a caução registrada; Refunded é o valor devolvido ao locatário e
	// Retained é o que ficou com o locador.
	Amount   float64
	Refunded float64
	Retained float64

	// Status é a situação da caução (`held`, `refunded`, `partially_refunded`,
	// `forfeited`) e decide a declaração impressa.
	Status     string
	ReceivedAt string
	// RefundedAt é a data da devolução (`YYYY-MM-DDTHH:mm`), vazia enquanto a
	// caução não foi devolvida.
	RefundedAt string
	Notes      string

	// AuthCode é a chave legível (`AUTH-XXXX-XXXX-XXXX-XXXX`) e ValidationURL é
	// o endereço público de validação, ambos responsabilidade da camada de cima.
	AuthCode      string
	ValidationURL string
	// IssuedAt data o documento ("Cabo Frio, 21 de setembro de 2026").
	IssuedAt time.Time
	// LogoPNG é a marca do emissor impressa no cabeçalho (opcional).
	LogoPNG []byte
}

// Medidas do recibo de devolução, em pontos.
const (
	// depositQRSize é o lado do QR Code do selo.
	depositQRSize = 50.0
	// depositSignatureLineWidth é a largura da linha de assinatura.
	depositSignatureLineWidth = 220.0
)

// depositAuthCaption é a legenda do selo no recibo de devolução.
const depositAuthCaption = "Aponte a câmera do celular para verificar o registro oficial deste recibo de devolução de caução no servidor."

// BuildDepositReceipt monta o modelo do recibo de devolução de caução.
func BuildDepositReceipt(input DepositReceiptInput) (Document, error) {
	issuedAt := input.IssuedAt
	if issuedAt.IsZero() {
		issuedAt = time.Now()
	}
	status := domain.DepositStatus(input.Status)

	blocks := make([]Block, 0, 28)
	blocks = append(blocks, Block{
		Kind:     BlockBrand,
		Text:     "Aluxen",
		ImagePNG: input.LogoPNG,
		WidthMM:  38,
		Tone:     ToneAccent,
		Bold:     true,
		Size:     13,
	})
	blocks = append(blocks, Block{
		Kind: BlockTitle,
		Text: depositReceiptTitle,
		Size: 18,
		Bold: true,
	})
	blocks = append(blocks, Block{
		Kind: BlockSubtitle,
		Text: depositReceiptSubtitle(input.LeaseID),
		Size: 11,
		Tone: ToneMuted,
	})
	blocks = append(blocks, Block{Kind: BlockRule, Tone: ToneAccent})

	blocks = append(blocks, Block{Kind: BlockSection, Text: "Locador", Size: 11, Tone: ToneAccent})
	blocks = append(blocks, label("Nome:", input.LandlordName))
	if cpf := strings.TrimSpace(input.LandlordCPF); cpf != "" {
		blocks = append(blocks, label("CPF:", cpf))
	}

	blocks = append(blocks, Block{Kind: BlockSection, Text: "Locatário", Size: 11, Tone: ToneAccent})
	blocks = append(blocks, label("Nome:", input.TenantName))
	if cpf := strings.TrimSpace(input.TenantCPF); cpf != "" {
		blocks = append(blocks, label("CPF:", cpf))
	}

	blocks = append(blocks, Block{Kind: BlockSection, Text: "Imóvel", Size: 11, Tone: ToneAccent})
	blocks = append(blocks, label("Imóvel:", input.PropertyName))
	if address := strings.TrimSpace(input.PropertyAddress); address != "" {
		blocks = append(blocks, label("Endereço:", address))
	}
	if leaseID := strings.TrimSpace(input.LeaseID); leaseID != "" {
		blocks = append(blocks, label("Contrato:", leaseID))
	}

	blocks = append(blocks, Block{Kind: BlockGap, Size: 6})
	blocks = append(blocks, Block{Kind: BlockSection, Text: "Resumo da caução", Size: 12, Tone: ToneAccent})
	blocks = append(blocks, metric("Valor da Caução", format.FormatCurrency(input.Amount), 15, ToneInk))
	if input.Refunded > 0 {
		blocks = append(blocks, metric("Valor Devolvido", format.FormatCurrency(input.Refunded), 17, ToneAccent))
	}
	if input.Retained > 0 {
		blocks = append(blocks, metric("Valor Retido", format.FormatCurrency(input.Retained), 15, ToneDanger))
	}
	blocks = append(blocks, metric("Situação", domain.DepositStatusLabel(status), 12, ToneInk))

	if receivedAt := strings.TrimSpace(input.ReceivedAt); receivedAt != "" {
		blocks = append(blocks, label("Recebido em:", format.FormatDateTimeBR(receivedAt)))
	}
	if refundedAt := strings.TrimSpace(input.RefundedAt); refundedAt != "" {
		blocks = append(blocks, label("Devolvido em:", format.FormatDateTimeBR(refundedAt)))
	}

	blocks = append(blocks, Block{Kind: BlockGap, Size: 12})
	blocks = append(blocks, Block{Kind: BlockSection, Text: "Declaração", Size: 14, Tone: ToneInk})
	blocks = append(blocks, paragraph(depositDeclaration(input, status)))
	if notes := strings.TrimSpace(input.Notes); notes != "" {
		blocks = append(blocks, paragraph("Observações: "+notes))
	}

	blocks = append(blocks, Block{Kind: BlockGap, Size: 18})
	blocks = append(blocks, Block{Kind: BlockText, Text: "Cabo Frio, " + formatLongDate(issuedAt), Size: 12})

	blocks = append(blocks, Block{
		Kind:    BlockSignature,
		Text:    signatureName(input.LandlordName, "Locador"),
		Align:   AlignCenter,
		Size:    11,
		Bold:    true,
		WidthMM: pointsToMM(depositSignatureLineWidth),
	})
	if cpf := strings.TrimSpace(input.LandlordCPF); cpf != "" {
		blocks = append(blocks, Block{Kind: BlockSubtitle, Text: "CPF " + cpf, Align: AlignCenter, Size: 10, Tone: ToneMuted})
	}

	blocks = append(blocks, Block{Kind: BlockGap, Size: 14})
	blocks = append(blocks, authBlock(input.ValidationURL, input.AuthCode, depositAuthCaption, depositQRSize))

	return newDocument(blocks), nil
}

// depositReceiptTitle é o título fixo do recibo de devolução de caução.
const depositReceiptTitle = "RECIBO DE DEVOLUÇÃO DE CAUÇÃO"

// depositReceiptSubtitle monta a linha de identificação: `Recibo <8 primeiros
// caracteres do contrato em maiúsculas>`.
func depositReceiptSubtitle(leaseID string) string {
	return "Recibo " + strings.ToUpper(truncate(leaseID, 8))
}

// depositDeclaration monta a declaração de devolução conforme a situação da
// caução: retida (nada devolvido ainda), devolvida integralmente, devolvida em
// parte (com o valor retido) ou retida integralmente.
func depositDeclaration(input DepositReceiptInput, status domain.DepositStatus) string {
	property := fallback(receiptPropertyDescription(input.PropertyName, input.PropertyAddress), "objeto do contrato")
	tenant := fallback(strings.TrimSpace(input.TenantName), "locatário")

	switch status {
	case domain.DepositStatusHeld:
		return fmt.Sprintf(
			"Declaro, para os devidos fins, que o valor de %s (%s) referente à caução do imóvel %s, vinculado ao locatário %s, permanece retido pelo LOCADOR, não havendo, até a presente data, devolução a registrar.",
			format.FormatCurrency(input.Amount), domain.MoneyInWords(input.Amount), property, tenant,
		)
	case domain.DepositStatusPartiallyRefunded:
		return fmt.Sprintf(
			"Declaro, para os devidos fins, a devolução parcial da caução no valor de %s (%s) de um total de %s (%s) referente ao imóvel %s, vinculado ao locatário %s, ficando retido o valor de %s (%s).",
			format.FormatCurrency(input.Refunded), domain.MoneyInWords(input.Refunded),
			format.FormatCurrency(input.Amount), domain.MoneyInWords(input.Amount),
			property, tenant,
			format.FormatCurrency(input.Retained), domain.MoneyInWords(input.Retained),
		)
	case domain.DepositStatusForfeited:
		return fmt.Sprintf(
			"Declaro, para os devidos fins, a retenção integral da caução no valor de %s (%s) referente ao imóvel %s, vinculado ao locatário %s, nada sendo devolvido ao LOCATÁRIO.",
			format.FormatCurrency(input.Amount), domain.MoneyInWords(input.Amount), property, tenant,
		)
	case domain.DepositStatusRefunded:
		return fmt.Sprintf(
			"Declaro, para os devidos fins, a devolução integral da caução no valor de %s (%s) referente ao imóvel %s, vinculado ao locatário %s, nada restando retido.",
			format.FormatCurrency(input.Refunded), domain.MoneyInWords(input.Refunded), property, tenant,
		)
	default:
		return fmt.Sprintf(
			"Declaro, para os devidos fins, o registro da caução no valor de %s (%s) referente ao imóvel %s, vinculado ao locatário %s.",
			format.FormatCurrency(input.Amount), domain.MoneyInWords(input.Amount), property, tenant,
		)
	}
}
