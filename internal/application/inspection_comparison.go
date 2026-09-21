package application

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Vistoria por imagem (tier 2): compara as fotos do auto de entrada com as do
// auto de saída do contrato e monta o laudo que o locador revisa.
//
// O contrato da porta (vocabulário de severidade, tipos de achado e o
// comparador) vive em inspection_compare.go; aqui fica o caso de uso. A divisão
// de trabalho é a de sempre: a IA lê as imagens e SUGERE achados, enquanto o
// código valida a severidade, exige que todo achado cite foto existente, conta
// as fotos, calcula o resumo quantitativo e redige o laudo. O laudo é subsídio
// do locador: não retém caução, não define valor de dano e não substitui a
// vistoria presencial.

// ErrComparisonUnavailable indica que a comparação por imagem não está
// configurada nesta instância (sem provedor de IA). O handler traduz em 503 —
// como a consulta em linguagem natural.
var ErrComparisonUnavailable = errors.New("comparação por imagem não configurada nesta instância")

// ErrInvalidComparisonResponse indica que a comparação devolvida pela porta
// saiu do formato combinado: severidade fora do vocabulário fechado ou
// confiança fora de 0..1. É falha de leitura, nunca dado — o laudo só imprime o
// vocabulário validado.
var ErrInvalidComparisonResponse = errors.New("resposta inválida da comparação por imagem")

// ComparisonSide é uma das vistorias comparadas, pronta para a tela e o laudo.
type ComparisonSide struct {
	InspectionID   string `json:"inspectionId"`
	Kind           string `json:"kind"`
	KindLabel      string `json:"kindLabel"`
	PerformedAt    string `json:"performedAt"`
	PerformedLabel string `json:"performedLabel"`
	Condition      string `json:"condition"`
	ConditionLabel string `json:"conditionLabel"`
	Items          int    `json:"items"`
	Photos         int    `json:"photos"`
}

// ComparisonPhotoRow é uma foto comparada, na relação que o laudo imprime.
type ComparisonPhotoRow struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Side     string `json:"side"`
	SideName string `json:"sideName"`
}

// ComparisonFindingRow é um achado validado, pronto para a tela e a tabela do
// laudo. `Severity` já saiu do vocabulário fechado validado pelo caso de uso.
type ComparisonFindingRow struct {
	Area          string          `json:"area"`
	Item          string          `json:"item"`
	State         string          `json:"state"`
	Severity      FindingSeverity `json:"severity"`
	SeverityLabel string          `json:"severityLabel"`
	Note          string          `json:"note"`
	CheckInPhoto  string          `json:"checkInPhoto"`
	CheckOutPhoto string          `json:"checkOutPhoto"`
	CheckInLabel  string          `json:"checkInLabel"`
	CheckOutLabel string          `json:"checkOutLabel"`
}

// ComparisonSummary é o resumo quantitativo do laudo, calculado pelo código a
// partir dos achados validados.
type ComparisonSummary struct {
	Findings   int     `json:"findings"`
	Ok         int     `json:"ok"`
	Attention  int     `json:"attention"`
	Damaged    int     `json:"damaged"`
	Photos     int     `json:"photos"`
	Discarded  int     `json:"discarded"`
	Confidence float64 `json:"confidence"`
}

// InspectionComparisonView é a comparação pronta para a tela, o laudo e o PDF.
type InspectionComparisonView struct {
	LeaseID         string                 `json:"leaseId"`
	TenantName      string                 `json:"tenantName"`
	PropertyName    string                 `json:"propertyName"`
	PropertyAddress string                 `json:"propertyAddress"`
	StartDate       string                 `json:"startDate"`
	EndDate         string                 `json:"endDate"`
	PeriodLabel     string                 `json:"periodLabel"`
	LandlordName    string                 `json:"landlordName"`
	LandlordCPF     string                 `json:"landlordCpf"`
	TenantCPF       string                 `json:"tenantCpf"`
	CheckIn         ComparisonSide         `json:"checkIn"`
	CheckOut        ComparisonSide         `json:"checkOut"`
	Photos          []ComparisonPhotoRow   `json:"photos"`
	Findings        []ComparisonFindingRow `json:"findings"`
	Summary         ComparisonSummary      `json:"summary"`
	// ProviderSummary é o resumo em texto do provedor: sugestão para o locador
	// revisar. Não entra no laudo formal, que é determinístico.
	ProviderSummary string `json:"providerSummary"`
	// Laudo é o texto determinístico montado a partir dos achados validados.
	Laudo string `json:"laudo"`
	// AuthCode e ValidationURL são o selo do laudo. O laudo não é entidade
	// própria: o selo é o do auto de vistoria de ENTRADA do contrato, que é o
	// documento-âncora (a página pública de validação é por vistoria).
	AuthCode      string `json:"authCode"`
	ValidationURL string `json:"validationUrl"`
}

// CompareLeaseInspections compara as fotos da vistoria de entrada com as da
// saída do contrato e devolve a visão com os achados, o resumo e o laudo.
//
// Sem as duas vistorias não há comparação: falta de vistoria é validação com
// mensagem clara, contrato inexistente é `domain.ErrNotFound`. Foto referenciada
// que não está no armazenamento também é validação — nunca pânico.
func CompareLeaseInspections(ctx context.Context, deps Deps, actx AuditContext, leaseID string) (*InspectionComparisonView, error) {
	if strings.TrimSpace(leaseID) == "" {
		return nil, domain.Invalid("Informe o contrato da comparação.")
	}
	if deps.Photos == nil {
		return nil, ErrComparisonUnavailable
	}

	lease, err := deps.Repos.Leases.ByID(ctx, leaseID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("buscar contrato da comparação: %w", err)
	}

	checkIn, checkOut, err := comparisonInspections(ctx, deps, lease.ID)
	if err != nil {
		return nil, err
	}

	checkInPhotos, err := comparisonPhotosOf(deps, lease.ID, checkIn)
	if err != nil {
		return nil, err
	}
	checkOutPhotos, err := comparisonPhotosOf(deps, lease.ID, checkOut)
	if err != nil {
		return nil, err
	}
	if len(checkInPhotos)+len(checkOutPhotos) == 0 {
		return nil, domain.Invalid("As vistorias deste contrato não têm fotos anexadas para comparar.")
	}

	comparison, _, err := deps.Photos.CompareInspectionPhotos(ctx, PhotoComparisonInput{
		LeaseID:      lease.ID,
		TenantName:   firstNonEmpty(lease.TenantName, checkIn.TenantName, checkOut.TenantName),
		PropertyName: firstNonEmpty(lease.PropertyName, checkIn.PropertyName, checkOut.PropertyName),
		CheckIn:      checkInPhotos,
		CheckOut:     checkOutPhotos,
		Items:        checkIn.Items,
	})
	if err != nil {
		return nil, fmt.Errorf("comparar as fotos das vistorias: %w", err)
	}

	view, err := buildComparisonView(ctx, deps, lease, checkIn, checkOut, checkInPhotos, checkOutPhotos, comparison)
	if err != nil {
		return nil, err
	}

	// A trilha guarda só id, contagem de fotos, achados e confiança: nem o
	// conteúdo das imagens nem o texto do provedor entram aqui.
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionInspectionCompare,
		Entity:      domain.EntityInspection,
		EntityID:    lease.ID,
		EntityLabel: DescribeLease(lease),
		Summary:     "Vistorias comparadas · " + view.PropertyName,
		Metadata: map[string]any{
			"photos":     view.Summary.Photos,
			"findings":   view.Summary.Findings,
			"damaged":    view.Summary.Damaged,
			"confidence": view.Summary.Confidence,
		},
	}); err != nil {
		return nil, err
	}

	deps.Log.Info("vistorias comparadas",
		"scope", "season", "lease_id", lease.ID, "photos", view.Summary.Photos,
		"findings", view.Summary.Findings, "damaged", view.Summary.Damaged,
		"confidence", view.Summary.Confidence)

	return view, nil
}

// comparisonInspections carrega as duas vistorias do contrato. As duas são
// obrigatórias: comparar exige a linha de base e o resultado.
func comparisonInspections(ctx context.Context, deps Deps, leaseID string) (*domain.Inspection, *domain.Inspection, error) {
	inspections, err := deps.Repos.Inspections.ByLease(ctx, leaseID)
	if err != nil {
		return nil, nil, fmt.Errorf("listar vistorias do contrato: %w", err)
	}
	var checkIn, checkOut *domain.Inspection
	for index := range inspections {
		switch inspections[index].Kind {
		case domain.InspectionKindCheckIn:
			if checkIn == nil {
				checkIn = &inspections[index]
			}
		case domain.InspectionKindCheckOut:
			if checkOut == nil {
				checkOut = &inspections[index]
			}
		}
	}
	switch {
	case checkIn == nil && checkOut == nil:
		return nil, nil, domain.Invalid("Registre as vistorias de entrada e de saída do contrato antes de comparar.")
	case checkIn == nil:
		return nil, nil, domain.Invalid("Registre a vistoria de entrada do contrato antes de comparar.")
	case checkOut == nil:
		return nil, nil, domain.Invalid("Registre a vistoria de saída do contrato antes de comparar.")
	}
	return checkIn, checkOut, nil
}

// comparisonPhotosOf lê as fotos da vistoria no armazenamento da instância e
// resolve o mime pelo nome do arquivo. Foto ausente ou em formato que o
// provedor não lê é validação com o nome do arquivo — nunca pânico.
func comparisonPhotosOf(deps Deps, leaseID string, inspection *domain.Inspection) ([]ComparisonPhoto, error) {
	if len(inspection.Photos) == 0 {
		return nil, nil
	}
	if deps.Attachments == nil {
		return nil, domain.Invalid("O armazenamento de fotos não está configurado nesta instância.")
	}

	photos := make([]ComparisonPhoto, 0, len(inspection.Photos))
	for _, photo := range inspection.Photos {
		name := strings.TrimSpace(photo.Name)
		if name == "" {
			// Foto sem arquivo guardado não tem o que comparar.
			continue
		}
		mime, ok := comparisonPhotoMime(name)
		if !ok {
			return nil, domain.Invalid("A foto %s está em formato que a comparação não lê (use JPG, PNG ou WEBP).", name)
		}
		content, err := deps.Attachments.Open(name)
		if err != nil {
			deps.Log.Warn("foto da vistoria não lida para a comparação",
				"scope", "season", "lease_id", leaseID, "photo", name, "err", err)
			return nil, domain.Invalid("O arquivo da foto %s não está no armazenamento da instância.", name)
		}
		photos = append(photos, ComparisonPhoto{
			Name:  name,
			Bytes: content,
			Mime:  mime,
			Label: firstNonEmpty(photo.Label, photo.Caption, name),
		})
	}
	return photos, nil
}

// comparisonPhotoMime resolve o mime da foto pelo nome guardado. O upload aceita
// HEIC/HEIF do celular, mas o provedor não lê esse formato: o que não for JPG,
// PNG ou WEBP é recusado com o nome do arquivo em vez de virar imagem ilegível.
func comparisonPhotoMime(name string) (string, bool) {
	switch strings.ToLower(path.Ext(strings.TrimSpace(name))) {
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".png":
		return "image/png", true
	case ".webp":
		return "image/webp", true
	default:
		return "", false
	}
}

// buildComparisonView valida a resposta da porta e monta a visão. É aqui que o
// código decide: severidade fora do vocabulário recusa a resposta inteira,
// achado que cita foto desconhecida (ou nenhuma foto) é descartado, e o resumo
// quantitativo e o texto do laudo saem dos achados validados.
func buildComparisonView(
	ctx context.Context,
	deps Deps,
	lease *domain.Lease,
	checkIn, checkOut *domain.Inspection,
	checkInPhotos, checkOutPhotos []ComparisonPhoto,
	comparison PhotoComparison,
) (*InspectionComparisonView, error) {
	if comparison.Confidence < 0 || comparison.Confidence > 1 {
		return nil, fmt.Errorf("%w: confiança fora de 0..1", ErrInvalidComparisonResponse)
	}

	known := make(map[string]struct{}, len(checkInPhotos)+len(checkOutPhotos))
	for _, photo := range checkInPhotos {
		known[photo.Name] = struct{}{}
	}
	for _, photo := range checkOutPhotos {
		known[photo.Name] = struct{}{}
	}

	findings := make([]ComparisonFindingRow, 0, len(comparison.Findings))
	kept := PhotoComparison{Confidence: comparison.Confidence}
	summary := ComparisonSummary{Photos: len(known), Confidence: comparison.Confidence}
	for _, finding := range comparison.Findings {
		severity, err := ParseFindingSeverity(string(finding.Severity))
		if err != nil {
			return nil, fmt.Errorf("%w: severidade fora do vocabulário", ErrInvalidComparisonResponse)
		}
		// O laudo cita arquivo por arquivo: achado que não aponta para foto
		// existente não é verificável pelo locador e sai do laudo.
		if !comparisonFindingPhotosKnown(finding, known) {
			summary.Discarded++
			continue
		}
		validated := ComparisonFinding{
			Area:          strings.TrimSpace(finding.Area),
			Item:          strings.TrimSpace(finding.Item),
			State:         strings.TrimSpace(finding.State),
			Severity:      severity,
			Note:          strings.TrimSpace(finding.Note),
			CheckInPhoto:  strings.TrimSpace(finding.CheckInPhoto),
			CheckOutPhoto: strings.TrimSpace(finding.CheckOutPhoto),
		}
		kept.Findings = append(kept.Findings, validated)
		findings = append(findings, ComparisonFindingRow{
			Area:          validated.Area,
			Item:          validated.Item,
			State:         validated.State,
			Severity:      validated.Severity,
			SeverityLabel: FindingSeverityLabel(validated.Severity),
			Note:          validated.Note,
			CheckInPhoto:  validated.CheckInPhoto,
			CheckOutPhoto: validated.CheckOutPhoto,
			CheckInLabel:  comparisonPhotoLabel(checkInPhotos, validated.CheckInPhoto),
			CheckOutLabel: comparisonPhotoLabel(checkOutPhotos, validated.CheckOutPhoto),
		})
	}
	// O resumo quantitativo conta só o que ficou no laudo (o descartado não
	// entra em nenhuma das três contagens).
	summary.Ok, summary.Attention, summary.Damaged = kept.Counts()
	summary.Findings = len(findings)

	tenant, err := optionalTenant(ctx, deps, leaseTenantID(lease))
	if err != nil {
		return nil, err
	}
	property, err := optionalProperty(ctx, deps, leasePropertyID(lease))
	if err != nil {
		return nil, err
	}
	landlordName, landlordCPF, err := landlordSettings(ctx, deps)
	if err != nil {
		return nil, err
	}
	// O laudo usa o selo do auto de entrada: mesma chave, mesmo `kind` e a
	// mesma página pública de validação dos demais documentos.
	_, code, validationURL, err := documentAuth(ctx, deps, documentKindInspection, checkIn.ID)
	if err != nil {
		return nil, err
	}

	view := &InspectionComparisonView{
		LeaseID:         lease.ID,
		TenantName:      firstNonEmpty(lease.TenantName, checkIn.TenantName, checkOut.TenantName, tenantName(tenant), receiptTenantFallback),
		PropertyName:    firstNonEmpty(lease.PropertyName, checkIn.PropertyName, checkOut.PropertyName, propertyName(property)),
		PropertyAddress: joinAddress(property),
		StartDate:       leaseStartDate(lease),
		EndDate:         leaseEndDate(lease),
		LandlordName:    landlordName,
		LandlordCPF:     landlordCPF,
		TenantCPF:       domain.Str(tenantCpf(tenant)),
		CheckIn:         comparisonSideOf(checkIn, checkInPhotos),
		CheckOut:        comparisonSideOf(checkOut, checkOutPhotos),
		Photos:          comparisonPhotoRows(checkIn, checkOut, checkInPhotos, checkOutPhotos),
		Findings:        findings,
		Summary:         summary,
		ProviderSummary: strings.TrimSpace(comparison.Summary),
		AuthCode:        code,
		ValidationURL:   validationURL,
	}
	view.PeriodLabel = format.FormatDateBR(view.StartDate) + " a " + format.FormatDateBR(view.EndDate)
	view.Laudo = BuildComparisonReport(view)
	return view, nil
}

// comparisonSideOf descreve uma das vistorias comparadas.
func comparisonSideOf(inspection *domain.Inspection, photos []ComparisonPhoto) ComparisonSide {
	return ComparisonSide{
		InspectionID:   inspection.ID,
		Kind:           string(inspection.Kind),
		KindLabel:      domain.InspectionKindLabel(inspection.Kind),
		PerformedAt:    inspection.PerformedAt,
		PerformedLabel: format.FormatDateTimeBR(inspection.PerformedAt),
		Condition:      string(inspection.Condition),
		ConditionLabel: domain.InspectionConditionLabel(inspection.Condition),
		Items:          len(inspection.Items),
		Photos:         len(photos),
	}
}

// comparisonPhotoRows monta a relação das fotos comparadas, entrada primeiro.
func comparisonPhotoRows(checkIn, checkOut *domain.Inspection, checkInPhotos, checkOutPhotos []ComparisonPhoto) []ComparisonPhotoRow {
	rows := make([]ComparisonPhotoRow, 0, len(checkInPhotos)+len(checkOutPhotos))
	for _, group := range []struct {
		inspection *domain.Inspection
		photos     []ComparisonPhoto
	}{
		{checkIn, checkInPhotos},
		{checkOut, checkOutPhotos},
	} {
		for _, photo := range group.photos {
			rows = append(rows, ComparisonPhotoRow{
				Name:     photo.Name,
				Label:    photo.Label,
				Side:     string(group.inspection.Kind),
				SideName: comparisonPhotoSide(group.inspection.Kind),
			})
		}
	}
	return rows
}

// comparisonPhotoSide devolve o nome da vistoria na relação de fotos.
func comparisonPhotoSide(kind domain.InspectionKind) string {
	if kind == domain.InspectionKindCheckOut {
		return "Saída"
	}
	return "Entrada"
}

// comparisonPhotoLabel devolve o rótulo da foto citada pelo achado, ou o
// próprio nome do arquivo quando a foto não tem legenda.
func comparisonPhotoLabel(photos []ComparisonPhoto, name string) string {
	if name == "" {
		return ""
	}
	for _, photo := range photos {
		if photo.Name == name {
			return firstNonEmpty(photo.Label, name)
		}
	}
	return name
}

// comparisonFindingPhotosKnown informa se o achado cita ao menos uma foto e se
// todas as fotos citadas existem entre as comparadas.
func comparisonFindingPhotosKnown(finding ComparisonFinding, known map[string]struct{}) bool {
	cited := make([]string, 0, 2)
	for _, name := range []string{finding.CheckInPhoto, finding.CheckOutPhoto} {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			cited = append(cited, trimmed)
		}
	}
	if len(cited) == 0 {
		return false
	}
	for _, name := range cited {
		if _, ok := known[name]; !ok {
			return false
		}
	}
	return true
}

// BuildComparisonReport redige o texto do laudo a partir dos achados validados.
// O texto é determinístico: mesma visão, mesmo laudo — nada de texto livre do
// provedor aqui.
func BuildComparisonReport(view *InspectionComparisonView) string {
	if view == nil {
		return ""
	}
	lines := make([]string, 0, len(view.Findings)+8)
	lines = append(lines, "LAUDO DE COMPARAÇÃO DE VISTORIAS")
	lines = append(lines, "Contrato "+view.LeaseID+" · "+view.TenantName+" · "+view.PropertyName)
	if view.PeriodLabel != "" {
		lines = append(lines, "Período do contrato: "+view.PeriodLabel)
	}
	lines = append(lines, comparisonSideReport(view.CheckIn))
	lines = append(lines, comparisonSideReport(view.CheckOut))
	lines = append(lines, fmt.Sprintf("Fotos comparadas: %d.", view.Summary.Photos))
	lines = append(lines, "Achados: "+comparisonCountsText(view.Summary)+".")
	for _, finding := range view.Findings {
		lines = append(lines, "- "+comparisonFindingText(finding))
	}
	if view.Summary.Discarded > 0 {
		lines = append(lines, fmt.Sprintf(
			"%d achado(s) foram descartados por citarem foto que não está entre as comparadas.",
			view.Summary.Discarded))
	}
	lines = append(lines, ComparisonDisclaimer)
	return strings.Join(lines, "\n")
}

// comparisonSideReport descreve uma vistoria dentro do laudo.
func comparisonSideReport(side ComparisonSide) string {
	return fmt.Sprintf("%s: %s · %s · %d foto(s).", side.KindLabel, side.PerformedLabel, side.ConditionLabel, side.Photos)
}

// comparisonCountsText é a frase do resumo quantitativo.
func comparisonCountsText(summary ComparisonSummary) string {
	return fmt.Sprintf("%d sem alteração, %d de atenção, %d de dano",
		summary.Ok, summary.Attention, summary.Damaged)
}

// comparisonFindingText é a linha de um achado no texto do laudo.
func comparisonFindingText(finding ComparisonFindingRow) string {
	subject := strings.TrimSpace(finding.Area)
	if item := strings.TrimSpace(finding.Item); item != "" {
		subject = strings.TrimSpace(subject + " · " + item)
	}
	if subject == "" {
		subject = "Item não identificado"
	}
	text := subject + " (" + strings.ToLower(finding.SeverityLabel) + ")"
	if state := strings.TrimSpace(finding.State); state != "" {
		text += ": " + state
	}
	if note := strings.TrimSpace(finding.Note); note != "" {
		text += " — " + note
	}
	if photos := comparisonFindingPhotosText(finding); photos != "" {
		text += " " + photos
	}
	return text
}

// comparisonFindingPhotosText cita as fotos do achado no texto do laudo.
func comparisonFindingPhotosText(finding ComparisonFindingRow) string {
	parts := make([]string, 0, 2)
	if finding.CheckInPhoto != "" {
		parts = append(parts, "entrada: "+finding.CheckInPhoto)
	}
	if finding.CheckOutPhoto != "" {
		parts = append(parts, "saída: "+finding.CheckOutPhoto)
	}
	if len(parts) == 0 {
		return ""
	}
	return "[fotos — " + strings.Join(parts, "; ") + "]"
}

// ComparisonDisclaimer é a ressalva do laudo, uma linha só: o laudo é subsídio
// do locador e não decide cobrança.
const ComparisonDisclaimer = "Ressalva: este laudo é subsídio do locador para revisar a saída da temporada; " +
	"foi gerado a partir das fotos anexadas aos autos de vistoria, não substitui a vistoria presencial " +
	"e não define valor de dano nem retenção de caução."
