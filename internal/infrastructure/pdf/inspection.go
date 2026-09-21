package pdf

import (
	"strconv"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Auto de vistoria de entrada e de saída (temporada).
//
// O documento registra as partes, o período do contrato, a data e o estado geral
// da vistoria, a tabela de itens conferidos, as observações gerais e a relação
// das fotos anexadas — as fotos ficam no armazenamento da instância e o PDF
// apenas as referencia pelo nome. Fecha com as assinaturas do locador e do
// locatário e com o mesmo selo de autenticidade dos demais documentos (QR Code
// da página pública e chave AUTH).

// InspectionItemInput é uma linha da tabela de itens conferidos.
type InspectionItemInput struct {
	Area  string
	Item  string
	State string
	Notes string
}

// InspectionPhotoInput é uma foto anexada: `Name` é o nome do arquivo no
// armazenamento da instância e `Label`/`Caption` são os textos que a identificam
// na relação impressa.
type InspectionPhotoInput struct {
	Name    string
	Label   string
	Caption string
}

// InspectionInput reúne tudo que o auto de vistoria imprime.
type InspectionInput struct {
	InspectionID string
	// Kind é o tipo da vistoria (`checkin` ou `checkout`) e decide o título.
	Kind         string
	LeaseID      string
	TenantName   string
	TenantCPF    string
	PropertyName string
	// PropertyAddress é o endereço completo do imóvel.
	PropertyAddress string
	// StartDate e EndDate delimitam o período do contrato (`YYYY-MM-DD`).
	StartDate string
	EndDate   string
	// PerformedAt é a data e a hora da vistoria (`YYYY-MM-DDTHH:mm`).
	PerformedAt string
	// Condition é o estado geral do imóvel (`otima`, `boa`, `regular`, `ruim`).
	Condition    string
	Notes        string
	Items        []InspectionItemInput
	Photos       []InspectionPhotoInput
	LandlordName string
	LandlordCPF  string
	// AuthCode é a chave legível (`AUTH-XXXX-XXXX-XXXX-XXXX`) e ValidationURL é
	// o endereço público de validação, ambos responsabilidade da camada de cima.
	AuthCode      string
	ValidationURL string
	// IssuedAt data o documento ("Cabo Frio, 21 de setembro de 2026").
	IssuedAt time.Time
	// LogoPNG é a marca do emissor impressa no cabeçalho (opcional).
	LogoPNG []byte
}

// Medidas do auto de vistoria, em pontos.
const (
	// inspectionQRSize é o lado do QR Code do selo.
	inspectionQRSize = 50.0
	// inspectionSignatureLineWidthMM é a largura das linhas de assinatura.
	inspectionSignatureLineWidthMM = 67
	// inspectionTableSize é o corpo da tabela de itens.
	inspectionTableSize = 9.5
)

// inspectionAuthCaption é a legenda do selo no auto de vistoria.
const inspectionAuthCaption = "Aponte a câmera do celular para verificar a autenticidade deste auto de vistoria no servidor."

// inspectionColumns são as colunas da tabela de itens e as frações de largura de
// cada uma (a observação fica com a maior parte da linha).
var (
	inspectionColumns      = []string{"Ambiente", "Item", "Estado", "Observação"}
	inspectionColumnWidths = []float64{0.22, 0.26, 0.16, 0.36}
)

// BuildInspection monta o modelo do auto de vistoria.
func BuildInspection(input InspectionInput) (Document, error) {
	issuedAt := input.IssuedAt
	if issuedAt.IsZero() {
		issuedAt = time.Now()
	}

	blocks := make([]Block, 0, 32)
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
		Text: inspectionTitle(input.Kind),
		Size: 18,
		Bold: true,
	})
	blocks = append(blocks, Block{
		Kind: BlockSubtitle,
		Text: inspectionSubtitle(input.InspectionID),
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
	blocks = append(blocks, label("Período do contrato:", inspectionPeriod(input.StartDate, input.EndDate)))

	blocks = append(blocks, Block{Kind: BlockSection, Text: "Vistoria", Size: 11, Tone: ToneAccent})
	blocks = append(blocks, label("Data e hora:", format.FormatDateTimeBR(input.PerformedAt)))
	blocks = append(blocks, label("Estado geral:", domain.InspectionConditionLabel(domain.InspectionCondition(input.Condition))))

	blocks = append(blocks, Block{Kind: BlockSection, Text: "Itens conferidos", Size: 11, Tone: ToneAccent})
	if len(input.Items) == 0 {
		blocks = append(blocks, paragraph("Não há itens conferidos registrados nesta vistoria."))
	} else {
		rows := make([][]string, 0, len(input.Items))
		for _, item := range input.Items {
			// A ordem das linhas é a ordem em que os itens foram conferidos.
			rows = append(rows, []string{
				strings.TrimSpace(item.Area),
				strings.TrimSpace(item.Item),
				strings.TrimSpace(item.State),
				strings.TrimSpace(item.Notes),
			})
		}
		blocks = append(blocks, table(inspectionColumns, rows, inspectionColumnWidths, inspectionTableSize))
	}

	blocks = append(blocks, Block{Kind: BlockSection, Text: "Observações gerais", Size: 11, Tone: ToneAccent})
	blocks = append(blocks, paragraph(inspectionNotes(input.Notes)))

	blocks = append(blocks, Block{Kind: BlockSection, Text: "Fotos anexadas", Size: 11, Tone: ToneAccent})
	blocks = append(blocks, paragraph(
		"As fotos desta vistoria ficam armazenadas na instância do sistema; os arquivos abaixo podem ser conferidos com o locador.",
	))
	if len(input.Photos) == 0 {
		blocks = append(blocks, paragraph("Não há fotos anexadas a esta vistoria."))
	} else {
		for index, photo := range input.Photos {
			blocks = append(blocks, label("Foto "+strconv.Itoa(index+1)+":", inspectionPhotoDescription(photo)))
		}
	}

	blocks = append(blocks, Block{Kind: BlockGap, Size: 16})
	blocks = append(blocks, paragraph("Cabo Frio, "+formatLongDate(issuedAt)+"."))

	blocks = append(blocks, Block{Kind: BlockGap, Size: 18})
	blocks = append(blocks,
		Block{
			Kind:    BlockSignature,
			Text:    signatureName(input.LandlordName, "Locador"),
			Align:   AlignLeft,
			Size:    11,
			Bold:    true,
			WidthMM: inspectionSignatureLineWidthMM,
		},
		Block{
			Kind:    BlockSignature,
			Text:    signatureName(input.TenantName, "Locatário"),
			Align:   AlignRight,
			Size:    11,
			Bold:    true,
			WidthMM: inspectionSignatureLineWidthMM,
		},
	)
	blocks = append(blocks, Block{Kind: BlockGap, Size: 4})
	blocks = append(blocks,
		Block{Kind: BlockSignatureNote, Text: "Locador", Align: AlignLeft, Size: 10, Tone: ToneMuted},
		Block{Kind: BlockSignatureNote, Text: "Locatário", Align: AlignRight, Size: 10, Tone: ToneMuted},
	)

	blocks = append(blocks, Block{Kind: BlockGap, Size: 14})
	blocks = append(blocks, authBlock(input.ValidationURL, input.AuthCode, inspectionAuthCaption, inspectionQRSize))

	return newDocument(blocks), nil
}

// inspectionTitle devolve o título do auto conforme o tipo da vistoria.
func inspectionTitle(kind string) string {
	if domain.InspectionKind(kind) == domain.InspectionKindCheckOut {
		return "AUTO DE VISTORIA DE SAÍDA"
	}
	return "AUTO DE VISTORIA DE ENTRADA"
}

// inspectionSubtitle monta a linha de identificação: `Auto <8 primeiros
// caracteres do id em maiúsculas>`, como no recibo.
func inspectionSubtitle(inspectionID string) string {
	return "Auto " + strings.ToUpper(truncate(inspectionID, 8))
}

// inspectionPeriod escreve o período do contrato ("01/03/2026 a 28/02/2027").
func inspectionPeriod(startDate, endDate string) string {
	return format.FormatDateBR(startDate) + " a " + format.FormatDateBR(endDate)
}

// inspectionNotes devolve as observações gerais, com o texto padrão quando não
// houve nenhuma.
func inspectionNotes(notes string) string {
	if trimmed := strings.TrimSpace(notes); trimmed != "" {
		return trimmed
	}
	return "Sem observações registradas."
}

// inspectionPhotoDescription descreve a foto anexada: o rótulo, a legenda e o
// nome do arquivo no armazenamento da instância.
func inspectionPhotoDescription(photo InspectionPhotoInput) string {
	parts := make([]string, 0, 3)
	for _, part := range []string{strings.TrimSpace(photo.Label), strings.TrimSpace(photo.Caption)} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if name := strings.TrimSpace(photo.Name); name != "" {
		parts = append(parts, "arquivo: "+name)
	}
	if len(parts) == 0 {
		return "sem arquivo associado"
	}
	return strings.Join(parts, " · ")
}

// signatureName devolve o nome impresso sobre a linha de assinatura, com o
// rótulo do papel como padrão quando o cadastro está vazio.
func signatureName(name, role string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	return role
}
