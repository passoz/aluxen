package web

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/authz"
)

// Comparação de vistorias por imagem: as duas rotas são exercitadas pelo router
// completo (CSRF, sessão e autorização incluídos) e a porta de IA é um dublê.

const (
	cmpLeaseID    = "lease-cmp"
	cmpCheckInID  = "insp-cmp-in"
	cmpCheckOutID = "insp-cmp-out"
	cmpTenantID   = "tenant-cmp"
	cmpInPhoto    = "inspections/aaa-fachada.jpg"
	cmpOutPhoto   = "inspections/bbb-sala.jpg"
)

// cmpPort é o dublê da porta de comparação por imagem.
type cmpPort struct {
	result application.PhotoComparison
	raw    string
	err    error
	calls  int
}

func (p *cmpPort) CompareInspectionPhotos(context.Context, application.PhotoComparisonInput) (application.PhotoComparison, string, error) {
	p.calls++
	return p.result, p.raw, p.err
}

// cmpComparison é a resposta padrão do dublê: um achado de atenção e um de dano,
// ambos citando fotos que existem no armazenamento.
func cmpComparison() application.PhotoComparison {
	return application.PhotoComparison{
		Summary:    "O sofá ganhou uma mancha; o rodapé trincou.",
		Confidence: 0.7,
		Findings: []application.ComparisonFinding{
			{Area: "Sala", Item: "Sofá", State: "Mancha", Severity: application.FindingAttention, Note: "mancha escura", CheckInPhoto: cmpInPhoto, CheckOutPhoto: cmpOutPhoto},
			{Area: "Sala", Item: "Rodapé", State: "Trinca", Severity: application.FindingDamage, CheckInPhoto: cmpOutPhoto},
		},
	}
}

// cmpPanel monta o painel com um contrato de temporada, as duas vistorias com
// fotos e o dublê da comparação ligado.
func cmpPanel(t *testing.T) (*covPanel, *cmpPort) {
	t.Helper()

	panel := newCovPanel(t)
	panel.tenants.items = []domain.Tenant{{ID: cmpTenantID, Name: "Maria Silva", CPF: covString("123.456.789-00")}}
	panel.properties.items = []domain.Property{{
		ID: cmpLeaseID + "-property", Name: "Casa da Praia",
		Address: "Rua das Flores, 123", City: "Cabo Frio", State: "RJ",
	}}
	panel.leases.items = []domain.Lease{{
		ID:            cmpLeaseID,
		TenantID:      cmpTenantID,
		PropertyID:    cmpLeaseID + "-property",
		TenantName:    "Maria Silva",
		PropertyName:  "Casa da Praia",
		StartDate:     "2026-03-01",
		EndDate:       "2026-09-30",
		DueDay:        10,
		RentAmount:    1200,
		Status:        domain.LeaseStatusSeason,
		DepositAmount: covFloat(1200),
		DepositStatus: domain.DepositStatusHeld,
	}}
	panel.settings.values[domain.SettingLandlordName] = "João Souza"
	panel.settings.values[domain.SettingLandlordCPF] = "987.654.321-00"
	panel.inspections.items = []domain.Inspection{
		{
			ID:          cmpCheckInID,
			LeaseID:     cmpLeaseID,
			Kind:        domain.InspectionKindCheckIn,
			PerformedAt: "2026-03-01T10:00",
			Condition:   domain.InspectionConditionGood,
			Items:       []domain.InspectionItem{{Area: "Sala", Item: "Sofá", State: "Bom"}},
			Photos:      []domain.InspectionPhoto{{Name: cmpInPhoto, Label: "fachada.jpg"}},
			TenantName:  "Maria Silva",
		},
		{
			ID:          cmpCheckOutID,
			LeaseID:     cmpLeaseID,
			Kind:        domain.InspectionKindCheckOut,
			PerformedAt: "2026-09-20T09:00",
			Condition:   domain.InspectionConditionFair,
			Photos:      []domain.InspectionPhoto{{Name: cmpOutPhoto, Label: "sala.jpg"}},
			TenantName:  "Maria Silva",
		},
	}
	panel.attachments.files[cmpInPhoto] = []byte("foto de entrada")
	panel.attachments.files[cmpOutPhoto] = []byte("foto de saída")

	// A instância injeta o gerador real de tokens; usar o mesmo gerador que a
	// borda HTTP confere é o que prova que a chave impressa no laudo abre o
	// próprio laudo.
	panel.common.Deps.Signer = authz.NewTokenGenerator(panel.common.Cfg.SessionSecret)

	port := &cmpPort{result: cmpComparison(), raw: `{"findings":[]}`}
	panel.common.Deps.Photos = port
	return panel, port
}

// cmpEntryKey é a chave de autenticidade do auto de vistoria de entrada, que é
// a credencial do laudo.
func cmpEntryKey(panel *covPanel) string {
	return panel.common.Deps.Signer.Generate("inspection", cmpCheckInID)
}

// cmpEntryCode é a chave legível impressa no selo do laudo.
func cmpEntryCode(panel *covPanel) string {
	return panel.common.Deps.Signer.FormatCode(cmpEntryKey(panel))
}

// ── POST /api/inspections/compare ──────────────────────────────────

func TestInspectionCompareServesTheComparisonToTheAdmin(t *testing.T) {
	panel, port := cmpPanel(t)

	recorder := panel.csrfPost("/api/inspections/compare", url.Values{"lease_id": {cmpLeaseID}})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	if port.calls != 1 {
		t.Fatalf("chamadas da porta = %d, esperado 1", port.calls)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}

	payload := covDecodeJSON(t, recorder)
	if payload["ok"] != true {
		t.Fatalf("payload = %v", payload)
	}
	comparison, _ := payload["comparison"].(map[string]any)
	if comparison == nil {
		t.Fatalf("payload sem a comparação: %v", payload)
	}
	if comparison["leaseId"] != cmpLeaseID || comparison["tenantName"] != "Maria Silva" {
		t.Fatalf("comparação = %v", comparison)
	}
	summary, _ := comparison["summary"].(map[string]any)
	if summary["findings"] != float64(2) || summary["attention"] != float64(1) || summary["damaged"] != float64(1) || summary["photos"] != float64(2) {
		t.Fatalf("resumo = %v", summary)
	}
	findings, _ := comparison["findings"].([]any)
	if len(findings) != 2 {
		t.Fatalf("achados = %v", comparison["findings"])
	}
	damage, _ := findings[1].(map[string]any)
	if damage["item"] != "Rodapé" || damage["severity"] != "dano" || damage["severityLabel"] != "Dano" {
		t.Fatalf("achado = %v", damage)
	}
	laudo, _ := comparison["laudo"].(string)
	for _, expected := range []string{"LAUDO DE COMPARAÇÃO DE VISTORIAS", "Achados: 0 sem alteração, 1 de atenção, 1 de dano.", "subsídio do locador"} {
		if !strings.Contains(laudo, expected) {
			t.Fatalf("laudo sem %q:\n%s", expected, laudo)
		}
	}
	if comparison["authCode"] != cmpEntryCode(panel) {
		t.Fatalf("chave do laudo = %v, esperada %q", comparison["authCode"], cmpEntryCode(panel))
	}
	if url, _ := comparison["validationUrl"].(string); !strings.Contains(url, "/validar/vistoria/"+cmpCheckInID) {
		t.Fatalf("URL de validação = %v", comparison["validationUrl"])
	}

	if actions := panel.audit.actions(); len(actions) != 1 || actions[0] != domain.ActionInspectionCompare {
		t.Fatalf("auditoria = %v", actions)
	}
}

func TestInspectionCompareRejectsAnonymousAndTenant(t *testing.T) {
	panel, port := cmpPanel(t)
	target := "/api/inspections/compare"

	anonymous := panel.csrfPostAs(target, url.Values{"lease_id": {cmpLeaseID}}, "", false)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("visitante: status = %d, esperado 401", anonymous.Code)
	}
	tenant := panel.csrfPostAs(target, url.Values{"lease_id": {cmpLeaseID}}, cmpTenantID, false)
	if tenant.Code != http.StatusUnauthorized {
		t.Fatalf("locatário: status = %d, esperado 401", tenant.Code)
	}
	if !strings.Contains(anonymous.Body.String(), "Não autorizado.") {
		t.Fatalf("corpo = %q", anonymous.Body.String())
	}
	if port.calls != 0 {
		t.Fatal("pedido sem administrador chegou ao provedor de IA")
	}
}

func TestInspectionCompareAcceptsAJSONBody(t *testing.T) {
	panel, port := cmpPanel(t)

	// O painel manda o formulário com o CSRF no corpo; um cliente de API manda
	// JSON e leva o mesmo token na query, como nas demais rotas do painel.
	page := httptest.NewRecorder()
	panel.handler.Router().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/login", nil))
	token := covCookieValue(page.Result().Cookies(), csrfCookieName)
	if token == "" {
		t.Fatal("cookie de CSRF não emitido")
	}

	request := httptest.NewRequest(http.MethodPost, "/api/inspections/compare?csrf_token="+url.QueryEscape(token),
		strings.NewReader(`{"lease_id":"`+cmpLeaseID+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.AddCookie(panel.sessionCookie(panel.admin.ID, true))
	for _, cookie := range page.Result().Cookies() {
		request.AddCookie(cookie)
	}

	recorder := panel.do(request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	if payload := covDecodeJSON(t, recorder); payload["ok"] != true {
		t.Fatalf("payload = %v", payload)
	}
	if port.calls != 1 {
		t.Fatalf("chamadas da porta = %d, esperado 1", port.calls)
	}

	broken := httptest.NewRequest(http.MethodPost, "/api/inspections/compare?csrf_token="+url.QueryEscape(token),
		strings.NewReader(`{"lease_id":`))
	broken.Header.Set("Content-Type", "application/json")
	broken.AddCookie(panel.sessionCookie(panel.admin.ID, true))
	for _, cookie := range page.Result().Cookies() {
		broken.AddCookie(cookie)
	}
	refused := panel.do(broken)
	if refused.Code != http.StatusBadRequest {
		t.Fatalf("corpo ilegível: status = %d, esperado 400", refused.Code)
	}
	if !strings.Contains(refused.Body.String(), comparisonReadError) {
		t.Fatalf("corpo = %q", refused.Body.String())
	}
	if port.calls != 1 {
		t.Fatal("corpo ilegível chegou ao provedor de IA")
	}
}

func TestComparisonRoutesReportProviderFailure(t *testing.T) {
	panel, port := cmpPanel(t)
	port.err = errors.New("provedor fora do ar")

	posted := panel.csrfPost("/api/inspections/compare", url.Values{"lease_id": {cmpLeaseID}})
	if posted.Code != http.StatusInternalServerError {
		t.Fatalf("POST: status = %d, esperado 500: %s", posted.Code, posted.Body.String())
	}
	if strings.Contains(posted.Body.String(), `"ok":true`) {
		t.Fatalf("POST respondeu ok sem comparação: %s", posted.Body.String())
	}

	// A mesma falha no laudo não pode virar PDF vazio nem pânico.
	pdf := panel.as(http.MethodGet, "/comparison-pdf?lease_id="+cmpLeaseID, nil, panel.admin.ID)
	if pdf.Code != http.StatusInternalServerError {
		t.Fatalf("PDF: status = %d, esperado 500: %s", pdf.Code, pdf.Body.String())
	}
	if actions := panel.audit.actions(); len(actions) != 0 {
		t.Fatalf("falha de leitura foi auditada: %v", actions)
	}
}

func TestInspectionCompareWithoutProviderIsUnavailable(t *testing.T) {
	panel, _ := cmpPanel(t)
	panel.common.Deps.Photos = nil

	recorder := panel.csrfPost("/api/inspections/compare", url.Values{"lease_id": {cmpLeaseID}})
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, esperado 503: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "Comparação por imagem não configurada nesta instância.") {
		t.Fatalf("corpo = %q", recorder.Body.String())
	}
}

func TestInspectionCompareWithoutBothInspectionsIsRefused(t *testing.T) {
	panel, port := cmpPanel(t)
	panel.inspections.items = panel.inspections.items[:1]

	recorder := panel.csrfPost("/api/inspections/compare", url.Values{"lease_id": {cmpLeaseID}})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "Registre a vistoria de saída do contrato antes de comparar.") {
		t.Fatalf("corpo = %q", recorder.Body.String())
	}
	if port.calls != 0 {
		t.Fatal("comparação sem as duas vistorias chegou ao provedor")
	}
}

func TestInspectionCompareReportsMissingContract(t *testing.T) {
	panel, _ := cmpPanel(t)

	recorder := panel.csrfPost("/api/inspections/compare", url.Values{"lease_id": {"inexistente"}})
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404: %s", recorder.Code, recorder.Body.String())
	}
}

func TestInspectionCompareRefusesSeverityOutsideTheVocabulary(t *testing.T) {
	panel, port := cmpPanel(t)
	port.result.Findings[0].Severity = application.FindingSeverity("grave")

	recorder := panel.csrfPost("/api/inspections/compare", url.Values{"lease_id": {cmpLeaseID}})
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, esperado 502: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "fora do formato esperado") {
		t.Fatalf("corpo = %q", recorder.Body.String())
	}
	if actions := panel.audit.actions(); len(actions) != 0 {
		t.Fatalf("resposta inválida foi auditada: %v", actions)
	}
}

// ── GET /comparison-pdf ────────────────────────────────────────────

func TestComparisonPDFServesTheLaudoWithTheEntryAutoKey(t *testing.T) {
	panel, _ := cmpPanel(t)
	target := "/comparison-pdf?lease_id=" + cmpLeaseID + "&token=" + cmpEntryKey(panel)

	recorder := panel.as(http.MethodGet, target, nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/pdf" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	expectedDisposition := `inline; filename="laudo-` + cmpLeaseID + `.pdf"`
	if disposition := recorder.Header().Get("Content-Disposition"); disposition != expectedDisposition {
		t.Fatalf("Content-Disposition = %q, esperado %q", disposition, expectedDisposition)
	}
	if !bytes.HasPrefix(recorder.Body.Bytes(), []byte("%PDF-")) {
		t.Fatal("resposta não é um PDF")
	}

	actions := panel.audit.actions()
	if len(actions) != 2 || actions[0] != domain.ActionInspectionCompare || actions[1] != domain.ActionDocumentInspection {
		t.Fatalf("auditoria = %v", actions)
	}
	if origin := panel.audit.entries[0].Origin; origin != domain.AuditOriginPublic {
		t.Fatalf("origem da comparação = %q, esperado link público", origin)
	}
	if origin := panel.audit.entries[1].Origin; origin != domain.AuditOriginPublic {
		t.Fatalf("origem do documento = %q, esperado link público", origin)
	}
	if summary := panel.audit.entries[1].Summary; !strings.Contains(summary, "Laudo de comparação de vistorias") {
		t.Fatalf("resumo do documento = %q", summary)
	}
}

func TestComparisonPDFAsAdminAndAsOwnerTenant(t *testing.T) {
	panel, _ := cmpPanel(t)
	target := "/comparison-pdf?lease_id=" + cmpLeaseID

	admin := panel.as(http.MethodGet, target, nil, panel.admin.ID)
	if admin.Code != http.StatusOK {
		t.Fatalf("administrador: status = %d: %s", admin.Code, admin.Body.String())
	}
	if origin := panel.audit.entries[0].Origin; origin != domain.AuditOriginWeb {
		t.Fatalf("origem da auditoria do administrador = %q", origin)
	}

	owner := panel.asClient(http.MethodGet, target, cmpTenantID)
	if owner.Code != http.StatusOK {
		t.Fatalf("titular: status = %d: %s", owner.Code, owner.Body.String())
	}
}

func TestComparisonPDFRefusesAnotherTenantAndAnonymousVisitor(t *testing.T) {
	panel, _ := cmpPanel(t)
	target := "/comparison-pdf?lease_id=" + cmpLeaseID

	other := panel.asClient(http.MethodGet, target, "tenant-outro")
	if other.Code != http.StatusForbidden {
		t.Fatalf("outro locatário: status = %d, esperado 403", other.Code)
	}
	if !strings.Contains(other.Body.String(), "Este documento não pertence ao seu cadastro.") {
		t.Fatalf("corpo = %q", other.Body.String())
	}

	anonymous := panel.as(http.MethodGet, target, nil, "")
	if anonymous.Code != http.StatusFound || anonymous.Header().Get("Location") != "/login" {
		t.Fatalf("visitante: status = %d, destino = %q", anonymous.Code, anonymous.Header().Get("Location"))
	}
}

func TestComparisonPDFRejectsRequestWithoutContract(t *testing.T) {
	panel, _ := cmpPanel(t)

	recorder := panel.as(http.MethodGet, "/comparison-pdf", nil, panel.admin.ID)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "Contrato inválido") {
		t.Fatalf("corpo = %q", body)
	}
}

func TestComparisonPDFWithoutTheEntryInspection(t *testing.T) {
	panel, _ := cmpPanel(t)
	panel.inspections.items = panel.inspections.items[1:]

	missing := panel.as(http.MethodGet, "/comparison-pdf?lease_id="+cmpLeaseID, nil, panel.admin.ID)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("contrato sem vistoria de entrada: status = %d, esperado 404", missing.Code)
	}
	if body := missing.Body.String(); !strings.Contains(body, "Vistoria de entrada não encontrada") {
		t.Fatalf("corpo = %q", body)
	}

	unknown := panel.as(http.MethodGet, "/comparison-pdf?lease_id=inexistente", nil, panel.admin.ID)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("contrato inexistente: status = %d, esperado 404", unknown.Code)
	}
	if actions := panel.audit.actions(); len(actions) != 0 {
		t.Fatalf("laudo sem vistorias foi auditado: %v", actions)
	}
}

func TestComparisonPDFWithoutProviderIsUnavailable(t *testing.T) {
	panel, _ := cmpPanel(t)
	panel.common.Deps.Photos = nil

	recorder := panel.as(http.MethodGet, "/comparison-pdf?lease_id="+cmpLeaseID, nil, panel.admin.ID)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, esperado 503: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "Comparação por imagem não configurada nesta instância.") {
		t.Fatalf("corpo = %q", recorder.Body.String())
	}
}

// ── Aba de temporada ───────────────────────────────────────────────

func TestSeasonTabOffersTheComparisonWhenBothInspectionsExist(t *testing.T) {
	panel, _ := cmpPanel(t)

	recorder := panel.as(http.MethodGet, "/dashboard?tab=season", nil, panel.admin.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`id="modal-comparison"`,
		`data-comparison-run="` + cmpLeaseID + `"`,
		`data-modal-fill='{"lease_id":"` + cmpLeaseID + `"}'`,
		"Comparar vistorias",
		"/api/inspections/compare",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("aba de temporada sem %q", expected)
		}
	}
}

func TestSeasonTabHidesTheComparisonWithoutTheCheckOut(t *testing.T) {
	panel, _ := cmpPanel(t)
	panel.inspections.items = panel.inspections.items[:1]

	recorder := panel.as(http.MethodGet, "/dashboard?tab=season", nil, panel.admin.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	if body := recorder.Body.String(); strings.Contains(body, `data-comparison-run="`+cmpLeaseID+`"`) {
		t.Fatal("aba ofereceu a comparação sem a vistoria de saída")
	}
}
