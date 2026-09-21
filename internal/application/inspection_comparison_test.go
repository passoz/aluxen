package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
)

// Comparação de vistorias por imagem: o comportamento observável é o laudo que
// sai do caso de uso (achados, resumo quantitativo e texto), o que foi enviado
// ao provedor e o que ficou na trilha. A porta é um dublê: nenhum teste fala com
// IA.

const (
	compLeaseID    = "lease-comp-1"
	compTenantID   = "tenant-comp-1"
	compPropertyID = "property-comp-1"
	compCheckInID  = "insp-comp-in"
	compCheckOutID = "insp-comp-out"
	compInPhoto    = "inspections/aaa-fachada.jpg"
	compOutPhoto   = "inspections/bbb-sala.png"
)

// compPhotos é o dublê da porta de comparação: devolve a resposta preparada e
// guarda o que o caso de uso enviou.
type compPhotos struct {
	result PhotoComparison
	raw    string
	err    error
	calls  int
	last   PhotoComparisonInput
}

func (c *compPhotos) CompareInspectionPhotos(_ context.Context, input PhotoComparisonInput) (PhotoComparison, string, error) {
	c.calls++
	c.last = input
	return c.result, c.raw, c.err
}

// newCompEnv monta o contrato de temporada com as duas vistorias, as fotos no
// armazenamento e o dublê da porta ligado.
func newCompEnv(t *testing.T) (*covEnv, *compPhotos) {
	t.Helper()

	env := newCovEnv(t)
	lease := covLeaseOf(compLeaseID, compTenantID, compPropertyID, domain.LeaseStatusSeason, "2026-03-01", "2026-09-30", 1200)
	// O repositório real já traz o contrato com os nomes resolvidos.
	lease.TenantName = "Maria Silva"
	lease.PropertyName = "Casa da Praia"
	env.leases.items = []domain.Lease{lease}
	env.tenants.items = []domain.Tenant{{ID: compTenantID, Name: "Maria Silva", CPF: domain.StrPtr("123.456.789-00")}}
	env.props.items = []domain.Property{covPropertyOf(compPropertyID, "Casa da Praia")}
	env.settings.values[domain.SettingLandlordName] = "João Souza"
	env.settings.values[domain.SettingLandlordCPF] = "987.654.321-00"
	env.settings.values[domain.SettingAppURL] = "https://aluxen.example"
	env.inspections.items = []domain.Inspection{
		{
			ID:          compCheckInID,
			LeaseID:     compLeaseID,
			Kind:        domain.InspectionKindCheckIn,
			PerformedAt: "2026-03-01T10:00",
			Condition:   domain.InspectionConditionGood,
			Items: []domain.InspectionItem{
				{Area: "Sala", Item: "Sofá", State: "Bom"},
			},
			Photos:       []domain.InspectionPhoto{{Name: compInPhoto, Label: "fachada.jpg"}},
			TenantName:   "Maria Silva",
			PropertyName: "Casa da Praia",
		},
		{
			ID:           compCheckOutID,
			LeaseID:      compLeaseID,
			Kind:         domain.InspectionKindCheckOut,
			PerformedAt:  "2026-09-20T09:00",
			Condition:    domain.InspectionConditionFair,
			Photos:       []domain.InspectionPhoto{{Name: compOutPhoto, Label: "sala.jpg"}},
			TenantName:   "Maria Silva",
			PropertyName: "Casa da Praia",
		},
	}
	env.attachments.files[compInPhoto] = []byte("foto de entrada")
	env.attachments.files[compOutPhoto] = []byte("foto de saída")

	photos := &compPhotos{result: PhotoComparison{
		Summary:    "A fachada permanece igual; o sofá ganhou uma mancha.",
		Confidence: 0.82,
		Findings: []ComparisonFinding{
			{Area: "Fachada", Item: "Pintura", State: "Sem alteração", Severity: FindingOK, CheckInPhoto: compInPhoto, CheckOutPhoto: compOutPhoto},
			{Area: "Sala", Item: "Sofá", State: "Mancha no braço", Severity: FindingAttention, Note: "mancha escura", CheckInPhoto: compInPhoto, CheckOutPhoto: compOutPhoto},
			{Area: "Sala", Item: "Rodapé", State: "Trinca", Severity: FindingDamage, Note: "trinca de 20 cm", CheckOutPhoto: compOutPhoto},
		},
	}, raw: `{"findings":[]}`}
	env.deps.Photos = photos
	return env, photos
}

// compFindings devolve os achados do laudo indexados por item.
func compFindings(view *InspectionComparisonView) map[string]ComparisonFindingRow {
	findings := make(map[string]ComparisonFindingRow, len(view.Findings))
	for _, finding := range view.Findings {
		findings[finding.Item] = finding
	}
	return findings
}

func TestCompareLeaseInspectionsBuildsTheLaudoFromValidatedFindings(t *testing.T) {
	env, photos := newCompEnv(t)
	photos.result.Findings = append(photos.result.Findings,
		// Achado que cita foto que não foi comparada: sai do laudo.
		ComparisonFinding{Area: "Quarto", Item: "Cama", Severity: FindingDamage, CheckInPhoto: "inspections/inexistente.jpg"},
		// Achado sem foto nenhuma: também não é verificável pelo locador.
		ComparisonFinding{Area: "Cozinha", Item: "Pia", Severity: FindingAttention},
	)

	view, err := CompareLeaseInspections(context.Background(), env.deps, covCtx(), compLeaseID)
	if err != nil {
		t.Fatalf("CompareLeaseInspections devolveu erro: %v", err)
	}

	// Achados validados, na ordem em que a comparação devolveu.
	if len(view.Findings) != 3 {
		t.Fatalf("achados = %d, esperado 3 (os dois sem foto saem do laudo)", len(view.Findings))
	}
	findings := compFindings(view)
	damage, ok := findings["Rodapé"]
	if !ok {
		t.Fatalf("laudo sem o achado de dano: %+v", view.Findings)
	}
	if damage.Severity != FindingDamage || damage.SeverityLabel != "Dano" {
		t.Fatalf("severidade do achado = %q (%q)", damage.Severity, damage.SeverityLabel)
	}
	if damage.Note != "trinca de 20 cm" || damage.State != "Trinca" {
		t.Fatalf("observação do achado = %+v", damage)
	}
	if damage.CheckOutLabel != "sala.jpg" || damage.CheckInLabel != "" {
		t.Fatalf("fotos do achado = %+v", damage)
	}

	// Resumo quantitativo: conta só o que ficou no laudo.
	summary := view.Summary
	if summary.Findings != 3 || summary.Ok != 1 || summary.Attention != 1 || summary.Damaged != 1 {
		t.Fatalf("resumo = %+v", summary)
	}
	if summary.Photos != 2 || summary.Discarded != 2 {
		t.Fatalf("resumo = %+v", summary)
	}
	if summary.Confidence != 0.82 {
		t.Fatalf("confiança = %v", summary.Confidence)
	}

	// Texto do laudo: determinístico a partir dos achados validados.
	for _, expected := range []string{
		"LAUDO DE COMPARAÇÃO DE VISTORIAS",
		"Contrato " + compLeaseID + " · Maria Silva · Casa da Praia",
		"Período do contrato: 01/03/2026 a 30/09/2026",
		"Vistoria de entrada: 01/03/2026 10:00 · Boa · 1 foto(s).",
		"Vistoria de saída: 20/09/2026 09:00 · Regular · 1 foto(s).",
		"Fotos comparadas: 2.",
		"Achados: 1 sem alteração, 1 de atenção, 1 de dano.",
		"- Sala · Rodapé (dano): Trinca — trinca de 20 cm [fotos — saída: " + compOutPhoto + "]",
		"2 achado(s) foram descartados por citarem foto que não está entre as comparadas.",
		"não define valor de dano nem retenção de caução",
	} {
		if !strings.Contains(view.Laudo, expected) {
			t.Fatalf("laudo sem %q\n---\n%s", expected, view.Laudo)
		}
	}
	if strings.Contains(view.Laudo, "inexistente.jpg") || strings.Contains(view.Laudo, "Cama") {
		t.Fatalf("laudo citou achado descartado\n---\n%s", view.Laudo)
	}

	// Identificação das partes e das vistorias.
	if view.TenantName != "Maria Silva" || view.PropertyName != "Casa da Praia" || view.TenantCPF != "123.456.789-00" {
		t.Fatalf("identificação = %+v", view)
	}
	if view.LandlordName != "João Souza" || view.LandlordCPF != "987.654.321-00" {
		t.Fatalf("locador = %+v", view)
	}
	if view.CheckIn.InspectionID != compCheckInID || view.CheckIn.KindLabel != "Vistoria de entrada" || view.CheckIn.Items != 1 {
		t.Fatalf("vistoria de entrada = %+v", view.CheckIn)
	}
	if view.CheckOut.InspectionID != compCheckOutID || view.CheckOut.ConditionLabel != "Regular" {
		t.Fatalf("vistoria de saída = %+v", view.CheckOut)
	}
	if view.ProviderSummary == "" {
		t.Fatal("resumo sugerido pela IA não foi preservado na visão")
	}

	// Relação das fotos comparadas: entrada primeiro.
	if len(view.Photos) != 2 || view.Photos[0].Side != "checkin" || view.Photos[0].SideName != "Entrada" || view.Photos[1].SideName != "Saída" {
		t.Fatalf("relação de fotos = %+v", view.Photos)
	}

	// A porta recebeu as fotos com o mime resolvido, o conteúdo lido e os itens
	// conferidos na entrada.
	if photos.calls != 1 {
		t.Fatalf("chamadas da porta = %d", photos.calls)
	}
	if len(photos.last.CheckIn) != 1 || len(photos.last.CheckOut) != 1 {
		t.Fatalf("fotos enviadas = %+v", photos.last)
	}
	if photos.last.CheckIn[0].Mime != "image/jpeg" || photos.last.CheckOut[0].Mime != "image/png" {
		t.Fatalf("mime das fotos = %+v", photos.last)
	}
	if string(photos.last.CheckIn[0].Bytes) != "foto de entrada" || photos.last.CheckOut[0].Label != "sala.jpg" {
		t.Fatalf("conteúdo enviado = %+v", photos.last)
	}
	if photos.last.LeaseID != compLeaseID || photos.last.TenantName != "Maria Silva" || len(photos.last.Items) != 1 {
		t.Fatalf("entrada da comparação = %+v", photos.last)
	}

	// O selo do laudo é o do auto de vistoria de entrada: mesma chave (o id do
	// auto de entrada dentro do token) e mesma página pública de validação.
	entryToken := "inspection-" + compCheckInID + "-tok"
	if view.AuthCode != "AUTH-"+strings.ToUpper(entryToken) {
		t.Fatalf("chave legível do laudo = %q", view.AuthCode)
	}
	expectedURL := "https://aluxen.example/validar/inspection/" + compCheckInID + "?t=" + entryToken
	if view.ValidationURL != expectedURL {
		t.Fatalf("URL de validação = %q, esperada %q", view.ValidationURL, expectedURL)
	}
}

func TestCompareLeaseInspectionsAuditsWithoutImageContent(t *testing.T) {
	env, _ := newCompEnv(t)

	if _, err := CompareLeaseInspections(context.Background(), env.deps, covCtx(), compLeaseID); err != nil {
		t.Fatalf("CompareLeaseInspections devolveu erro: %v", err)
	}

	if len(env.audit.entries) != 1 {
		t.Fatalf("eventos de auditoria = %d, esperado 1", len(env.audit.entries))
	}
	entry := env.audit.entries[0]
	if entry.Action != domain.ActionInspectionCompare || entry.Entity != domain.EntityInspection {
		t.Fatalf("evento = %+v", entry)
	}
	if domain.Str(entry.EntityID) != compLeaseID || domain.Str(entry.EntityLabel) != "Maria Silva · Casa da Praia" {
		t.Fatalf("evento = %+v", entry)
	}
	metadata := string(*entry.Metadata)
	for _, expected := range []string{`"photos":2`, `"findings":3`, `"damaged":1`, `"confidence":0.82`} {
		if !strings.Contains(metadata, expected) {
			t.Fatalf("metadados = %s, esperado conter %s", metadata, expected)
		}
	}
	for _, forbidden := range []string{"foto de entrada", "foto de saída", compInPhoto, compOutPhoto, "fachada.jpg"} {
		if strings.Contains(metadata, forbidden) {
			t.Fatalf("metadados com conteúdo de imagem: %s", metadata)
		}
	}
}

func TestCompareLeaseInspectionsRequiresBothInspections(t *testing.T) {
	cases := map[string]struct {
		keep    []string
		message string
	}{
		"sem nenhuma vistoria": {nil, "Registre as vistorias de entrada e de saída do contrato antes de comparar."},
		"sem a entrada":        {[]string{compCheckOutID}, "Registre a vistoria de entrada do contrato antes de comparar."},
		"sem a saída":          {[]string{compCheckInID}, "Registre a vistoria de saída do contrato antes de comparar."},
	}
	for name, testCase := range cases {
		env, photos := newCompEnv(t)
		inspections := make([]domain.Inspection, 0, len(testCase.keep))
		for _, keep := range testCase.keep {
			for _, inspection := range env.inspections.items {
				if inspection.ID == keep {
					inspections = append(inspections, inspection)
				}
			}
		}
		env.inspections.items = inspections

		_, err := CompareLeaseInspections(context.Background(), env.deps, covCtx(), compLeaseID)
		if !domain.IsValidation(err) || domain.ValidationMessage(err) != testCase.message {
			t.Fatalf("%s: erro = %v (%q)", name, err, domain.ValidationMessage(err))
		}
		if photos.calls != 0 {
			t.Fatalf("%s: a porta foi chamada sem as duas vistorias", name)
		}
	}
}

func TestCompareLeaseInspectionsFailsWhenAPhotoIsMissingFromStorage(t *testing.T) {
	env, photos := newCompEnv(t)
	delete(env.attachments.files, compOutPhoto)

	_, err := CompareLeaseInspections(context.Background(), env.deps, covCtx(), compLeaseID)
	if !domain.IsValidation(err) {
		t.Fatalf("erro = %v, esperado validação", err)
	}
	if message := domain.ValidationMessage(err); !strings.Contains(message, compOutPhoto) {
		t.Fatalf("mensagem sem o nome do arquivo: %q", message)
	}
	if photos.calls != 0 {
		t.Fatal("a comparação foi enviada mesmo com foto ausente")
	}
}

func TestCompareLeaseInspectionsRefusesFormatTheProviderDoesNotRead(t *testing.T) {
	env, photos := newCompEnv(t)
	env.inspections.items[1].Photos = []domain.InspectionPhoto{{Name: "inspections/bbb-sala.heic"}}

	_, err := CompareLeaseInspections(context.Background(), env.deps, covCtx(), compLeaseID)
	if !domain.IsValidation(err) {
		t.Fatalf("erro = %v, esperado validação", err)
	}
	if message := domain.ValidationMessage(err); !strings.Contains(message, "bbb-sala.heic") {
		t.Fatalf("mensagem sem o nome do arquivo: %q", message)
	}
	if photos.calls != 0 {
		t.Fatal("formato não suportado chegou ao provedor")
	}
}

func TestCompareLeaseInspectionsRequiresPhotos(t *testing.T) {
	env, photos := newCompEnv(t)
	for index := range env.inspections.items {
		env.inspections.items[index].Photos = nil
	}

	_, err := CompareLeaseInspections(context.Background(), env.deps, covCtx(), compLeaseID)
	if !domain.IsValidation(err) || domain.ValidationMessage(err) != "As vistorias deste contrato não têm fotos anexadas para comparar." {
		t.Fatalf("erro = %v (%q)", err, domain.ValidationMessage(err))
	}
	if photos.calls != 0 {
		t.Fatal("comparação sem fotos chegou ao provedor")
	}
}

func TestCompareLeaseInspectionsRejectsSeverityOutsideTheVocabulary(t *testing.T) {
	env, photos := newCompEnv(t)
	photos.result.Findings[0].Severity = FindingSeverity("grave")

	_, err := CompareLeaseInspections(context.Background(), env.deps, covCtx(), compLeaseID)
	if !errors.Is(err, ErrInvalidComparisonResponse) {
		t.Fatalf("erro = %v, esperado ErrInvalidComparisonResponse", err)
	}
	if len(env.audit.entries) != 0 {
		t.Fatal("resposta inválida foi auditada como comparação concluída")
	}
}

func TestCompareLeaseInspectionsRejectsConfidenceOutsideTheRange(t *testing.T) {
	env, photos := newCompEnv(t)
	photos.result.Confidence = 1.4

	_, err := CompareLeaseInspections(context.Background(), env.deps, covCtx(), compLeaseID)
	if !errors.Is(err, ErrInvalidComparisonResponse) {
		t.Fatalf("erro = %v, esperado ErrInvalidComparisonResponse", err)
	}
}

func TestCompareLeaseInspectionsKeepsTheLaudoWhenNoFindingSurvives(t *testing.T) {
	env, _ := newCompEnv(t)
	// Nenhum achado cita foto comparada: o laudo sai com o resumo zerado, sem
	// erro, e o descarte aparece contado para o locador revisar.
	env.deps.Photos.(*compPhotos).result.Findings = []ComparisonFinding{
		{Area: "Quarto", Item: "Cama", Severity: FindingDamage, CheckInPhoto: "inspections/sumiu.jpg"},
		{Area: "Cozinha", Item: "Pia", Severity: FindingAttention},
	}

	view, err := CompareLeaseInspections(context.Background(), env.deps, covCtx(), compLeaseID)
	if err != nil {
		t.Fatalf("CompareLeaseInspections devolveu erro: %v", err)
	}
	if len(view.Findings) != 0 {
		t.Fatalf("achados = %+v, esperado nenhum", view.Findings)
	}
	summary := view.Summary
	if summary.Findings != 0 || summary.Ok != 0 || summary.Attention != 0 || summary.Damaged != 0 || summary.Discarded != 2 {
		t.Fatalf("resumo = %+v", summary)
	}
	if summary.Photos != 2 {
		t.Fatalf("fotos comparadas = %d, esperado 2", summary.Photos)
	}
	if !strings.Contains(view.Laudo, "Achados: 0 sem alteração, 0 de atenção, 0 de dano.") {
		t.Fatalf("laudo sem o resumo zerado\n---\n%s", view.Laudo)
	}
	if !strings.Contains(view.Laudo, "2 achado(s) foram descartados") {
		t.Fatalf("laudo sem o descarte contado\n---\n%s", view.Laudo)
	}
	if actions := covAuditActions(env.audit.entries); len(actions) != 1 || actions[0] != domain.ActionInspectionCompare {
		t.Fatalf("auditoria = %v", actions)
	}
}

func TestCompareLeaseInspectionsWithoutProvider(t *testing.T) {
	env, photos := newCompEnv(t)
	env.deps.Photos = nil

	_, err := CompareLeaseInspections(context.Background(), env.deps, covCtx(), compLeaseID)
	if !errors.Is(err, ErrComparisonUnavailable) {
		t.Fatalf("erro = %v, esperado ErrComparisonUnavailable", err)
	}
	if photos.calls != 0 {
		t.Fatal("a porta foi chamada sem estar ligada")
	}
}

func TestCompareLeaseInspectionsValidatesTheContract(t *testing.T) {
	env, photos := newCompEnv(t)

	if _, err := CompareLeaseInspections(context.Background(), env.deps, covCtx(), "  "); !domain.IsValidation(err) {
		t.Fatalf("contrato vazio: erro = %v, esperado validação", err)
	}
	if _, err := CompareLeaseInspections(context.Background(), env.deps, covCtx(), "inexistente"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("contrato inexistente: erro = %v, esperado ErrNotFound", err)
	}
	if photos.calls != 0 {
		t.Fatal("contrato inválido chegou ao provedor")
	}
}

func TestCompareLeaseInspectionsPropagatesTheProviderFailure(t *testing.T) {
	env, photos := newCompEnv(t)
	photos.err = errors.New("provedor fora do ar")

	_, err := CompareLeaseInspections(context.Background(), env.deps, covCtx(), compLeaseID)
	if !errors.Is(err, photos.err) {
		t.Fatalf("erro = %v, esperado embrulhar a falha do provedor", err)
	}
	if len(env.audit.entries) != 0 {
		t.Fatal("falha de leitura foi auditada como comparação concluída")
	}
}

func TestCompareLeaseInspectionsWithoutAttachmentStore(t *testing.T) {
	env, _ := newCompEnv(t)
	env.deps.Attachments = nil

	_, err := CompareLeaseInspections(context.Background(), env.deps, covCtx(), compLeaseID)
	if !domain.IsValidation(err) || !strings.Contains(domain.ValidationMessage(err), "armazenamento") {
		t.Fatalf("erro = %v (%q)", err, domain.ValidationMessage(err))
	}
}

func TestBuildComparisonReportOfNothing(t *testing.T) {
	if report := BuildComparisonReport(nil); report != "" {
		t.Fatalf("laudo sem visão = %q", report)
	}
}
