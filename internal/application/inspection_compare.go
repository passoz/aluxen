package application

import (
	"context"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
)

// Comparação de vistorias por imagem (Tier 2): a IA olha as fotos de entrada e
// de saída e SUGERE achados por item; o código valida o vocabulário, descarta
// achado que cite foto desconhecida e monta o resumo quantitativo.
//
// O laudo é subsídio do locador: não substitui a vistoria presencial e não
// define valor de dano nem retenção de caução.

// ComparisonPhoto é uma foto enviada para comparação, já com o conteúdo lido do
// armazenamento da instância.
type ComparisonPhoto struct {
	// Name é o nome armazenado (o mesmo que a vistoria guarda).
	Name string
	// Bytes é o conteúdo do arquivo.
	Bytes []byte
	// Mime é o tipo do arquivo, usado no data URL enviado ao provedor.
	Mime string
	// Label é o rótulo legível (nome original do arquivo).
	Label string
}

// PhotoComparisonInput reúne tudo que a comparação precisa.
type PhotoComparisonInput struct {
	LeaseID      string
	TenantName   string
	PropertyName string
	CheckIn      []ComparisonPhoto
	CheckOut     []ComparisonPhoto
	Items        []domain.InspectionItem
}

// FindingSeverity é o vocabulário fechado da severidade de um achado.
type FindingSeverity string

const (
	// FindingOK é o item sem alteração entre entrada e saída.
	FindingOK FindingSeverity = "ok"
	// FindingAttention é a alteração que merece registro, sem dano.
	FindingAttention FindingSeverity = "atencao"
	// FindingDamage é o dano identificado na saída.
	FindingDamage FindingSeverity = "dano"
)

// FindingSeverityLabel devolve o rótulo em português da severidade.
func FindingSeverityLabel(severity FindingSeverity) string {
	switch severity {
	case FindingOK:
		return "Sem alteração"
	case FindingAttention:
		return "Atenção"
	case FindingDamage:
		return "Dano"
	default:
		return string(severity)
	}
}

// ParseFindingSeverity aceita apenas o vocabulário fechado. Resposta do provedor
// fora dele é inválida — nunca é interpretada como dano "provável".
func ParseFindingSeverity(raw string) (FindingSeverity, error) {
	switch FindingSeverity(strings.ToLower(strings.TrimSpace(raw))) {
	case FindingOK:
		return FindingOK, nil
	case FindingAttention:
		return FindingAttention, nil
	case FindingDamage:
		return FindingDamage, nil
	default:
		return "", domain.Invalid("Severidade %q fora do vocabulário aceito.", raw)
	}
}

// ComparisonFinding é um achado sugerido pela comparação.
type ComparisonFinding struct {
	Area          string
	Item          string
	State         string
	Severity      FindingSeverity
	Note          string
	CheckInPhoto  string
	CheckOutPhoto string
}

// PhotoComparison é a resposta validada da comparação.
type PhotoComparison struct {
	Findings   []ComparisonFinding
	Confidence float64
	Summary    string
}

// Counts devolve o resumo quantitativo por severidade.
func (c PhotoComparison) Counts() (untouched, attention, damage int) {
	for _, finding := range c.Findings {
		switch finding.Severity {
		case FindingOK:
			untouched++
		case FindingAttention:
			attention++
		case FindingDamage:
			damage++
		}
	}
	return untouched, attention, damage
}

// PhotoComparator compara as fotos das vistorias. A implementação concreta fala
// com o provedor de IA; nula desliga o recurso.
type PhotoComparator interface {
	CompareInspectionPhotos(ctx context.Context, input PhotoComparisonInput) (PhotoComparison, string, error)
}
