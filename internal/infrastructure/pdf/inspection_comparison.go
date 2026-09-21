package pdf

import (
	"strconv"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain/format"
)

// Laudo de comparação de vistorias (temporada).
//
// O documento é montado sobre o resultado já validado pelo caso de uso: o que a
// IA sugeriu está aqui, o que o código recusou não. Ele identifica as partes, o
// período e as duas vistorias, traz o resumo quantitativo, a tabela de achados
// (ambiente, item, foto de entrada, foto de saída, severidade e observação), a
// relação das fotos comparadas, a ressalva de que o laudo é subsídio do locador
// e não decide cobrança, as assinaturas e o mesmo selo de autenticidade dos
// demais documentos (QR Code da página pública e chave AUTH).

// ComparisonSideInput descreve uma das vistorias comparadas.
type ComparisonSideInput struct {
	// Label é o nome da vistoria ("Vistoria de entrada").
	Label string
	// PerformedAt é a data e a hora da vistoria (`YYYY-MM-DDTHH:mm`).
	PerformedAt string
	// Condition é o estado geral já com o rótulo em português.
	Condition string
	// Photos é quantas fotos aquela vistoria levou para a comparação.
	Photos int
}

// ComparisonSummaryInput é o resumo quantitativo do laudo.
type ComparisonSummaryInput struct {
	Ok         int
	Attention  int
	Damaged    int
	Photos     int
	Confidence float64
}

// ComparisonFindingInput é uma linha da tabela de achados. `Severity` já vem do
// vocabulário fechado validado; `CheckInLabel`/`CheckOutLabel` são os rótulos
// legíveis das fotos citadas (o nome do arquivo entra quando não há rótulo).
type ComparisonFindingInput struct {
	Area          string
	Item          string
	State         string
	Severity      string
	Note          string
	CheckInLabel  string
	CheckOutLabel string
}

// ComparisonPhotoInput é uma foto comparada: o rótulo, o arquivo guardado e a
// vistoria de onde ela veio.
type ComparisonPhotoInput struct {
	Name  string
	Label string
	// Side é o nome da vistoria ("Entrada" ou "Saída").
	Side string
}

// ComparisonDocumentInput reúne tudo que o laudo imprime.
type ComparisonDocumentInput struct {
	LeaseID      string
	TenantName   string
	TenantCPF    string
	PropertyName string
	// PropertyAddress é o endereço completo do imóvel.
	PropertyAddress string
	// StartDate e EndDate delimitam o período do contrato (`YYYY-MM-DD`).
	StartDate string
	EndDate   string
	CheckIn   ComparisonSideInput
	CheckOut  ComparisonSideInput
	Summary   ComparisonSummaryInput
	Findings  []ComparisonFindingInput
	Photos    []ComparisonPhotoInput
	// Disclaimer é a ressalva de uma linha do laudo, impressa como veio de quem
	// montou a comparação; vazia usa o texto padrão do documento.
	Disclaimer    string
	LandlordName  string
	LandlordCPF   string
	AuthCode      string
	ValidationURL string
	// IssuedAt data o documento ("Cabo Frio, 21 de setembro de 2026").
	IssuedAt time.Time
	// LogoPNG é a marca do emissor impressa no cabeçalho (opcional).
	LogoPNG []byte
}

// Medidas do laudo de comparação, em pontos.
const (
	// comparisonQRSize é o lado do QR Code do selo.
	comparisonQRSize = 50.0
	// comparisonSignatureLineWidthMM é a largura das linhas de assinatura.
	comparisonSignatureLineWidthMM = 67
	// comparisonTableSize é o corpo da tabela de achados.
	comparisonTableSize = 9.5
)

// comparisonAuthCaption é a legenda do selo do laudo. O laudo não é entidade
// própria: o selo é o do auto de vistoria de entrada do contrato, que é o
// documento-âncora.
const comparisonAuthCaption = "Aponte a câmera do celular para conferir a autenticidade do auto de vistoria de entrada deste contrato no servidor."

// comparisonDefaultDisclaimer é a ressalva impressa quando quem monta a
// comparação não informa o texto.
const comparisonDefaultDisclaimer = "Ressalva: este laudo é subsídio do locador para revisar a saída da temporada; " +
	"foi gerado a partir das fotos anexadas aos autos de vistoria, não substitui a vistoria presencial " +
	"e não define valor de dano nem retenção de caução."

// comparisonColumns são as colunas da tabela de achados e as frações de largura
// de cada uma (a observação fica com a maior parte da linha).
var (
	comparisonColumns      = []string{"Ambiente", "Item", "Entrada", "Saída", "Severidade", "Observação"}
	comparisonColumnWidths = []float64{0.15, 0.17, 0.13, 0.13, 0.14, 0.28}
)

// BuildInspectionComparison monta o modelo do laudo de comparação de vistorias.
func BuildInspectionComparison(input ComparisonDocumentInput) (Document, error) {
	issuedAt := input.IssuedAt
	if issuedAt.IsZero() {
		issuedAt = time.Now()
	}

	blocks := make([]Block, 0, 48)
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
		Text: "LAUDO DE COMPARAÇÃO DE VISTORIAS",
		Size: 17,
		Bold: true,
	})
	blocks = append(blocks, Block{
		Kind: BlockSubtitle,
		Text: comparisonSubtitle(input.LeaseID),
		Size: 11,
		Tone: ToneMuted,
	})
	blocks = append(blocks, Block{Kind: BlockRule, Tone: ToneAccent})

	blocks = append(blocks, Block{Kind: BlockSection, Text: "Partes e imóvel", Size: 11, Tone: ToneAccent})
	blocks = append(blocks, label("Locador:", input.LandlordName))
	if cpf := strings.TrimSpace(input.LandlordCPF); cpf != "" {
		blocks = append(blocks, label("CPF do locador:", cpf))
	}
	blocks = append(blocks, label("Locatário:", input.TenantName))
	if cpf := strings.TrimSpace(input.TenantCPF); cpf != "" {
		blocks = append(blocks, label("CPF do locatário:", cpf))
	}
	blocks = append(blocks, label("Imóvel:", input.PropertyName))
	if address := strings.TrimSpace(input.PropertyAddress); address != "" {
		blocks = append(blocks, label("Endereço:", address))
	}
	if leaseID := strings.TrimSpace(input.LeaseID); leaseID != "" {
		blocks = append(blocks, label("Contrato:", leaseID))
	}

	blocks = append(blocks, Block{Kind: BlockSection, Text: "Período e vistorias", Size: 11, Tone: ToneAccent})
	blocks = append(blocks, label("Período do contrato:", inspectionPeriod(input.StartDate, input.EndDate)))
	blocks = append(blocks, label(comparisonSideLabel(input.CheckIn, "Vistoria de entrada:"), comparisonSideDescription(input.CheckIn)))
	blocks = append(blocks, label(comparisonSideLabel(input.CheckOut, "Vistoria de saída:"), comparisonSideDescription(input.CheckOut)))

	blocks = append(blocks, Block{Kind: BlockSection, Text: "Resumo quantitativo", Size: 11, Tone: ToneAccent})
	blocks = append(blocks,
		metric("Sem alteração", strconv.Itoa(input.Summary.Ok), 13, ToneInk),
		metric("Atenção", strconv.Itoa(input.Summary.Attention), 13, ToneAccent),
		metric("Dano", strconv.Itoa(input.Summary.Damaged), 14, ToneDanger),
		metric("Fotos comparadas", strconv.Itoa(input.Summary.Photos), 11, ToneInk),
		metric("Confiança da leitura", comparisonConfidenceLabel(input.Summary.Confidence), 11, ToneMuted),
	)

	blocks = append(blocks, Block{Kind: BlockSection, Text: "Achados da comparação", Size: 11, Tone: ToneAccent})
	if len(input.Findings) == 0 {
		blocks = append(blocks, paragraph("A comparação não apontou nenhum achado entre as fotos analisadas."))
	} else {
		rows := make([][]string, 0, len(input.Findings))
		for _, finding := range input.Findings {
			rows = append(rows, []string{
				strings.TrimSpace(finding.Area),
				strings.TrimSpace(finding.Item),
				strings.TrimSpace(finding.CheckInLabel),
				strings.TrimSpace(finding.CheckOutLabel),
				strings.TrimSpace(finding.Severity),
				comparisonFindingNote(finding),
			})
		}
		blocks = append(blocks, table(comparisonColumns, rows, comparisonColumnWidths, comparisonTableSize))
	}

	blocks = append(blocks, Block{Kind: BlockSection, Text: "Fotos comparadas", Size: 11, Tone: ToneAccent})
	blocks = append(blocks, paragraph(
		"As fotos abaixo foram comparadas entre si e continuam disponíveis nos autos de vistoria da instância.",
	))
	if len(input.Photos) == 0 {
		blocks = append(blocks, paragraph("Não há fotos comparadas neste contrato."))
	} else {
		for index, photo := range input.Photos {
			blocks = append(blocks, label("Foto "+strconv.Itoa(index+1)+":", comparisonPhotoDescription(photo)))
		}
	}

	blocks = append(blocks, Block{Kind: BlockGap, Size: 12})
	blocks = append(blocks, paragraph(firstNonEmptyText(input.Disclaimer, comparisonDefaultDisclaimer)))

	blocks = append(blocks, Block{Kind: BlockGap, Size: 14})
	blocks = append(blocks, paragraph("Cabo Frio, "+formatLongDate(issuedAt)+"."))

	blocks = append(blocks, Block{Kind: BlockGap, Size: 18})
	blocks = append(blocks,
		Block{
			Kind:    BlockSignature,
			Text:    signatureName(input.LandlordName, "Locador"),
			Align:   AlignLeft,
			Size:    11,
			Bold:    true,
			WidthMM: comparisonSignatureLineWidthMM,
		},
		Block{
			Kind:    BlockSignature,
			Text:    signatureName(input.TenantName, "Locatário"),
			Align:   AlignRight,
			Size:    11,
			Bold:    true,
			WidthMM: comparisonSignatureLineWidthMM,
		},
	)
	blocks = append(blocks, Block{Kind: BlockGap, Size: 4})
	blocks = append(blocks,
		Block{Kind: BlockSignatureNote, Text: "Locador", Align: AlignLeft, Size: 10, Tone: ToneMuted},
		Block{Kind: BlockSignatureNote, Text: "Locatário", Align: AlignRight, Size: 10, Tone: ToneMuted},
	)

	blocks = append(blocks, Block{Kind: BlockGap, Size: 14})
	blocks = append(blocks, authBlock(input.ValidationURL, input.AuthCode, comparisonAuthCaption, comparisonQRSize))

	return newDocument(blocks), nil
}

// comparisonSubtitle monta a linha de identificação: `Laudo <8 primeiros
// caracteres do contrato em maiúsculas>`, como no auto de vistoria.
func comparisonSubtitle(leaseID string) string {
	return "Laudo " + strings.ToUpper(truncate(leaseID, 8))
}

// comparisonSideLabel devolve o rótulo da vistoria no laudo.
func comparisonSideLabel(side ComparisonSideInput, fallback string) string {
	if label := strings.TrimSpace(side.Label); label != "" {
		return label + ":"
	}
	return fallback
}

// comparisonSideDescription descreve a vistoria: data, estado geral e quantas
// fotos entraram na comparação.
func comparisonSideDescription(side ComparisonSideInput) string {
	parts := make([]string, 0, 3)
	if performed := strings.TrimSpace(side.PerformedAt); performed != "" {
		parts = append(parts, format.FormatDateTimeBR(performed))
	}
	if condition := strings.TrimSpace(side.Condition); condition != "" {
		parts = append(parts, condition)
	}
	parts = append(parts, strconv.Itoa(side.Photos)+" foto(s) comparada(s)")
	return strings.Join(parts, " · ")
}

// comparisonConfidenceLabel escreve a confiança da leitura em porcento.
func comparisonConfidenceLabel(confidence float64) string {
	return strconv.Itoa(int(confidence*100+0.5)) + "%"
}

// comparisonFindingNote é a observação da linha: o estado lido e a nota.
func comparisonFindingNote(finding ComparisonFindingInput) string {
	parts := make([]string, 0, 2)
	if state := strings.TrimSpace(finding.State); state != "" {
		parts = append(parts, state)
	}
	if note := strings.TrimSpace(finding.Note); note != "" {
		parts = append(parts, note)
	}
	return strings.Join(parts, " · ")
}

// comparisonPhotoDescription descreve a foto comparada: o rótulo, o arquivo
// guardado na instância e a vistoria de onde ela veio.
func comparisonPhotoDescription(photo ComparisonPhotoInput) string {
	parts := make([]string, 0, 3)
	if label := strings.TrimSpace(photo.Label); label != "" {
		parts = append(parts, label)
	}
	if name := strings.TrimSpace(photo.Name); name != "" {
		parts = append(parts, "arquivo: "+name)
	}
	if side := strings.TrimSpace(photo.Side); side != "" {
		parts = append(parts, "vistoria de "+strings.ToLower(side))
	}
	if len(parts) == 0 {
		return "sem arquivo associado"
	}
	return strings.Join(parts, " · ")
}

// firstNonEmptyText devolve o texto aparado ou o padrão quando vazio.
func firstNonEmptyText(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}
