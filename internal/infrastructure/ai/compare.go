package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/passoz/aluxen/internal/application"
)

// Este arquivo implementa a comparação de fotos de vistoria (Tier 2). A IA
// olha as fotos de entrada e de saída e SUGERE achados; quem valida o
// vocabulário de severidade e confere os nomes das fotos é o parse abaixo, e
// quem decide o que fazer com o laudo é o caso de uso. O laudo é subsídio do
// locador: não substitui a vistoria presencial e não define valor de dano nem
// retenção de caução.

// comparePrompt descreve a comparação. O vocabulário de severidade é fechado —
// resposta fora dele é inválida, nunca interpretada como dano provável.
const comparePrompt = `Você compara fotos de vistoria de imóvel alugado (entrada x saída) e responde apenas com JSON.
Formato exato, sem texto em volta:
{"summary":"","confidence":0.0,"findings":[{"area":"","item":"","state":"","severity":"ok|atencao|dano","note":"","checkInPhoto":"","checkOutPhoto":""}]}
Regras:
- summary: resumo curto e objetivo do que mudou entre a entrada e a saída.
- confidence: sua confiança na comparação, de 0 a 1.
- findings: um achado por item observado; use lista vazia quando nada tiver mudado.
- area e item: ambiente e item vistoriado (use o checklist enviado quando existir).
- state: o estado observado na saída (ex.: "íntegro", "riscado", "quebrado").
- severity: exatamente "ok", "atencao" ou "dano" — nada fora disso.
- note: observação curta sobre o que mudou.
- checkInPhoto e checkOutPhoto: o nome EXATO do arquivo da foto correspondente, como aparece no texto que acompanha cada imagem; deixe vazio quando não houver foto daquele lado.
Nunca invente: você só compara o que aparece nas fotos.`

// CompareInspectionPhotos compara as fotos das vistorias de entrada e de saída
// e devolve os achados sugeridos, com a resposta crua do provedor para a
// trilha de auditoria.
//
// Nada é gravado nem cobrado aqui: o laudo é sugestão para o locador conferir.
// Resposta fora do schema (severidade fora do vocabulário, foto citada que não
// foi enviada, campo ausente) é falha, nunca dado.
func (c *Client) CompareInspectionPhotos(ctx context.Context, input application.PhotoComparisonInput) (application.PhotoComparison, string, error) {
	if !c.Configured() {
		return application.PhotoComparison{}, "", ErrNotConfigured
	}
	parts, err := c.photoParts(input)
	if err != nil {
		return application.PhotoComparison{}, "", err
	}
	raw, err := c.completeImages(ctx, comparePrompt, compareInstruction(input), parts)
	if err != nil {
		return application.PhotoComparison{}, raw, err
	}
	comparison, err := parseComparison(raw, input)
	if err != nil {
		return application.PhotoComparison{}, raw, err
	}
	return comparison, raw, nil
}

// photoParts monta as partes da mensagem: cada foto precedida do texto que diz
// a série (ENTRADA/SAÍDA) e o nome do arquivo — é assim que o modelo sabe qual
// nome usar em checkInPhoto/checkOutPhoto. Cada foto passa pela mesma validação
// de tamanho e mime da leitura de documento.
func (c *Client) photoParts(input application.PhotoComparisonInput) ([]chatPart, error) {
	total := len(input.CheckIn) + len(input.CheckOut)
	if total == 0 {
		return nil, fmt.Errorf("%w: nenhuma foto para comparar", ErrInvalidDocument)
	}

	parts := make([]chatPart, 0, 2*total)
	groups := []struct {
		series string
		photos []application.ComparisonPhoto
	}{
		{"ENTRADA", input.CheckIn},
		{"SAÍDA", input.CheckOut},
	}
	for _, group := range groups {
		for _, photo := range group.photos {
			mime, err := c.validateFile(photo.Bytes, photo.Mime, photo.Name)
			if err != nil {
				return nil, err
			}
			parts = append(parts, chatPart{Type: "text", Text: photoLabel(group.series, photo)})
			parts = append(parts, chatPart{Type: "image_url", ImageURL: &imageURL{URL: dataURL(mime, photo.Bytes)}})
		}
	}
	return parts, nil
}

// photoLabel é o texto que precede a imagem e amarra o nome do arquivo à foto.
func photoLabel(series string, photo application.ComparisonPhoto) string {
	label := series + ` — arquivo: "` + strings.TrimSpace(photo.Name) + `"`
	if original := strings.TrimSpace(photo.Label); original != "" {
		label += " (nome original: " + original + ")"
	}
	return label
}

// compareInstruction é o texto da instrução: contexto do imóvel, a regra de
// nomenclatura das fotos e o checklist da vistoria.
func compareInstruction(input application.PhotoComparisonInput) string {
	var builder strings.Builder
	builder.WriteString("Compare as fotos da vistoria de ENTRADA com as da vistoria de SAÍDA e devolva o JSON do formato pedido.\n")
	if name := strings.TrimSpace(input.PropertyName); name != "" {
		builder.WriteString("Imóvel: " + name + "\n")
	}
	if name := strings.TrimSpace(input.TenantName); name != "" {
		builder.WriteString("Locatário: " + name + "\n")
	}
	builder.WriteString("Cada imagem vem logo depois de um texto que diz a série (ENTRADA ou SAÍDA) e o nome do arquivo dela: use exatamente esse nome em checkInPhoto/checkOutPhoto.\n")

	if len(input.Items) == 0 {
		builder.WriteString("Esta vistoria não tem checklist de itens: descreva area e item você mesmo.\n")
		return builder.String()
	}
	builder.WriteString("Itens da vistoria (use area e item daqui quando o achado corresponder):\n")
	for _, item := range input.Items {
		line := "- " + strings.TrimSpace(item.Area) + " — " + strings.TrimSpace(item.Item)
		if state := strings.TrimSpace(item.State); state != "" {
			line += " (estado na vistoria: " + state + ")"
		}
		if notes := strings.TrimSpace(item.Notes); notes != "" {
			line += " — " + notes
		}
		builder.WriteString(line + "\n")
	}
	return builder.String()
}

// parseComparison valida a resposta da comparação contra o schema combinado.
func parseComparison(raw string, input application.PhotoComparisonInput) (application.PhotoComparison, error) {
	object, err := fields(raw)
	if err != nil {
		return application.PhotoComparison{}, err
	}
	summary, err := requiredText(object, "summary")
	if err != nil {
		return application.PhotoComparison{}, err
	}
	confidence, err := confidenceField(object)
	if err != nil {
		return application.PhotoComparison{}, err
	}
	rawFindings, err := arrayField(object, "findings")
	if err != nil {
		return application.PhotoComparison{}, err
	}

	photos := map[string]map[string]bool{
		"checkInPhoto":  photoNames(input.CheckIn),
		"checkOutPhoto": photoNames(input.CheckOut),
	}
	findings := make([]application.ComparisonFinding, 0, len(rawFindings))
	for index, item := range rawFindings {
		finding, err := parseFinding(item, index, photos)
		if err != nil {
			return application.PhotoComparison{}, err
		}
		findings = append(findings, finding)
	}
	return application.PhotoComparison{Summary: summary, Confidence: confidence, Findings: findings}, nil
}

// parseFinding valida um achado. `item` e `severity` são a âncora: sem eles o
// achado não tem como ser conferido pelo locador.
func parseFinding(object map[string]json.RawMessage, index int, photos map[string]map[string]bool) (application.ComparisonFinding, error) {
	at := func(err error) error {
		return fmt.Errorf("%w (achado %d)", err, index+1)
	}

	item, err := requiredText(object, "item")
	if err != nil {
		return application.ComparisonFinding{}, at(err)
	}
	rawSeverity, err := requiredText(object, "severity")
	if err != nil {
		return application.ComparisonFinding{}, at(err)
	}
	severity, parseErr := application.ParseFindingSeverity(rawSeverity)
	if parseErr != nil {
		// A mensagem não repete o valor devolvido: pode conter dado da conversa.
		return application.ComparisonFinding{}, fmt.Errorf(
			"%w: campo \"severity\" fora do vocabulário ok|atencao|dano (achado %d)", ErrInvalidResponse, index+1)
	}

	var finding application.ComparisonFinding
	finding.Item = item
	finding.Severity = severity
	for _, field := range []struct {
		key    string
		target *string
	}{
		{"area", &finding.Area},
		{"state", &finding.State},
		{"note", &finding.Note},
	} {
		value, err := textField(object, field.key)
		if err != nil {
			return application.ComparisonFinding{}, at(err)
		}
		*field.target = value
	}
	for key, names := range photos {
		value, err := photoField(object, key, names)
		if err != nil {
			return application.ComparisonFinding{}, at(err)
		}
		if key == "checkInPhoto" {
			finding.CheckInPhoto = value
		} else {
			finding.CheckOutPhoto = value
		}
	}
	return finding, nil
}

// photoField lê o nome da foto citada e exige que seja o nome EXATO de uma das
// fotos enviadas: foto desconhecida é resposta inválida, nunca um achado
// apontando para lugar nenhum.
func photoField(object map[string]json.RawMessage, key string, names map[string]bool) (string, error) {
	value, err := textField(object, key)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", nil
	}
	if !names[value] {
		return "", fmt.Errorf("%w: campo %q cita uma foto que não está entre as fotos enviadas", ErrInvalidResponse, key)
	}
	return value, nil
}

// photoNames reúne os nomes de arquivo das fotos de uma série.
func photoNames(photos []application.ComparisonPhoto) map[string]bool {
	names := make(map[string]bool, len(photos))
	for _, photo := range photos {
		if name := strings.TrimSpace(photo.Name); name != "" {
			names[name] = true
		}
	}
	return names
}
