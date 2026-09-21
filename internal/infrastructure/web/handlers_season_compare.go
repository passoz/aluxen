package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/authz"
	"github.com/passoz/aluxen/internal/infrastructure/pdf"
)

// Vistoria por imagem (tier 2): comparação das fotos de entrada e de saída e
// laudo em PDF.
//
//	POST /api/inspections/compare        — compara as vistorias do contrato (JSON)
//	GET  /comparison-pdf?lease_id=&token= — laudo de comparação em PDF
//
// A comparação é sugestão: quem retém caução continua sendo o locador, no
// painel. O acesso ao laudo vale a mesma regra dos demais documentos
// (administrador logado, chave de autenticidade válida ou o locatário titular)
// e a chave é a do auto de vistoria de ENTRADA do contrato: o laudo não é
// entidade própria e a página pública de validação é por vistoria
// (`/validar/vistoria/{id}`), então o selo impresso aponta para o auto de
// entrada e o QR Code resolves numa página real.

const (
	// maxComparisonBodyBytes limita o corpo do pedido de comparação.
	maxComparisonBodyBytes = 8 << 10
	// comparisonReadError é a mensagem única de corpo ilegível.
	comparisonReadError = "Não foi possível ler os dados enviados."
	// comparisonUnavailableMessage é a resposta quando a instância não tem
	// provedor de IA configurado.
	comparisonUnavailableMessage = "Comparação por imagem não configurada nesta instância."
	// comparisonInvalidMessage é a resposta quando o provedor devolveu severidade
	// fora do vocabulário: falha de leitura, nunca dano presumido.
	comparisonInvalidMessage = "A comparação devolveu um resultado fora do formato esperado. Tente novamente."
)

// InspectionCompare compara as fotos das vistorias do contrato e devolve a
// visão da comparação em JSON. Só o administrador pede a comparação.
func (h *Handlers) InspectionCompare(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	leaseID, err := comparisonLeaseID(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": comparisonReadError, "requestId": requestIDFrom(r)})
		return
	}

	view, err := application.CompareLeaseInspections(r.Context(), h.Deps, h.comparisonAuditContext(r), leaseID)
	if err != nil {
		// As duas falhas próprias da comparação (provedor ausente e resposta fora
		// do formato) têm código próprio; o resto é erro de domínio comum —
		// validação sem vistoria, por exemplo. Nunca responder ok sem comparação.
		if !h.failComparison(w, r, err) {
			h.mapError(w, r, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "comparison": view})
}

// ComparisonPDF emite o laudo de comparação de vistorias em PDF.
func (h *Handlers) ComparisonPDF(w http.ResponseWriter, r *http.Request) {
	leaseID := strings.TrimSpace(r.URL.Query().Get("lease_id"))
	if leaseID == "" {
		http.Error(w, "Contrato inválido", http.StatusBadRequest)
		return
	}
	token := r.URL.Query().Get("token")

	// A chave de acesso é a do auto de vistoria de entrada do contrato (ver o
	// comentário do topo): sem ele não existe laudo.
	checkIn, err := application.InspectionForLease(r.Context(), h.Deps, leaseID, domain.InspectionKindCheckIn)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		// Visitante sem vistoria de entrada vai para o login, como nos demais
		// documentos; quem está logado recebe 404.
		if h.Session.AdminID(r) == "" && h.Session.ClientID(r) == "" {
			h.unauthorized(w, r)
			return
		}
		http.Error(w, "Vistoria de entrada não encontrada", http.StatusNotFound)
		return
	case err != nil:
		h.mapError(w, r, err)
		return
	}

	if !h.documentAccessGranted(w, r, authz.KindInspection, checkIn.ID, token, func() (bool, error) {
		return application.CanAccessInspection(r.Context(), h.Deps, checkIn.ID, h.Session.AdminID(r), h.Session.ClientID(r))
	}) {
		return
	}

	view, err := application.CompareLeaseInspections(r.Context(), h.Deps, h.comparisonAuditContext(r), leaseID)
	if errors.Is(err, domain.ErrNotFound) {
		http.Error(w, "Vistoria de entrada não encontrada", http.StatusNotFound)
		return
	}
	if err != nil {
		// As mesmas falhas próprias do POST têm código próprio; o resto é erro de
		// domínio comum. O laudo nunca é montado sem comparação.
		if !h.failComparison(w, r, err) {
			h.mapError(w, r, err)
		}
		return
	}

	document, err := pdf.BuildInspectionComparison(comparisonDocumentInput(view, h.Deps.Clock.Now(), logoImage()))
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	rendered, err := pdf.Render(document)
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	h.auditDocument(r, domain.ActionDocumentInspection, view.LeaseID, view.TenantName+" · "+view.PropertyName,
		"Laudo de comparação de vistorias de "+view.PropertyName+" emitido",
		map[string]any{"kind": "Laudo de comparação de vistorias", "count": view.Summary.Photos, "confidence": view.Summary.Confidence})

	writePDF(w, fmt.Sprintf("laudo-%s.pdf", view.LeaseID), rendered)
}

// failComparison escreve a resposta das falhas próprias da comparação
// (provedor ausente e resposta fora do formato) e diz se já respondeu.
func (h *Handlers) failComparison(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, application.ErrComparisonUnavailable):
		h.failJSONOrPage(w, r, http.StatusServiceUnavailable, comparisonUnavailableMessage)
	case errors.Is(err, application.ErrInvalidComparisonResponse):
		h.failJSONOrPage(w, r, http.StatusBadGateway, comparisonInvalidMessage)
	default:
		return false
	}
	return true
}

// comparisonAuditContext escolhe a origem da trilha: painel quando quem pediu
// está logado, link público quando o pedido veio da chave de autenticidade.
func (h *Handlers) comparisonAuditContext(r *http.Request) application.AuditContext {
	origin := domain.AuditOriginPublic
	if h.Session.AdminID(r) != "" {
		origin = domain.AuditOriginWeb
	}
	return h.auditContext(r, origin)
}

// comparisonLeaseID lê o contrato do pedido: corpo JSON (`{"lease_id": "..."}`)
// ou formulário do painel, com o mesmo nome de campo.
func comparisonLeaseID(r *http.Request) (string, error) {
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		var payload struct {
			LeaseID string `json:"lease_id"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxComparisonBodyBytes)).Decode(&payload); err != nil {
			return "", err
		}
		return strings.TrimSpace(payload.LeaseID), nil
	}
	if err := r.ParseForm(); err != nil {
		return "", err
	}
	return strings.TrimSpace(r.FormValue("lease_id")), nil
}

// comparisonDocumentInput traduz a visão da comparação no modelo do PDF. A
// visão já vem validada pelo caso de uso: aqui só há cópia de campo.
func comparisonDocumentInput(view *application.InspectionComparisonView, issuedAt time.Time, logoPNG []byte) pdf.ComparisonDocumentInput {
	findings := make([]pdf.ComparisonFindingInput, 0, len(view.Findings))
	for _, finding := range view.Findings {
		findings = append(findings, pdf.ComparisonFindingInput{
			Area:          finding.Area,
			Item:          finding.Item,
			State:         finding.State,
			Severity:      finding.SeverityLabel,
			Note:          finding.Note,
			CheckInLabel:  finding.CheckInLabel,
			CheckOutLabel: finding.CheckOutLabel,
		})
	}
	photos := make([]pdf.ComparisonPhotoInput, 0, len(view.Photos))
	for _, photo := range view.Photos {
		photos = append(photos, pdf.ComparisonPhotoInput{Name: photo.Name, Label: photo.Label, Side: photo.SideName})
	}

	return pdf.ComparisonDocumentInput{
		LeaseID:         view.LeaseID,
		TenantName:      view.TenantName,
		TenantCPF:       view.TenantCPF,
		PropertyName:    view.PropertyName,
		PropertyAddress: view.PropertyAddress,
		StartDate:       view.StartDate,
		EndDate:         view.EndDate,
		CheckIn: pdf.ComparisonSideInput{
			Label:       view.CheckIn.KindLabel,
			PerformedAt: view.CheckIn.PerformedAt,
			Condition:   view.CheckIn.ConditionLabel,
			Photos:      view.CheckIn.Photos,
		},
		CheckOut: pdf.ComparisonSideInput{
			Label:       view.CheckOut.KindLabel,
			PerformedAt: view.CheckOut.PerformedAt,
			Condition:   view.CheckOut.ConditionLabel,
			Photos:      view.CheckOut.Photos,
		},
		Summary: pdf.ComparisonSummaryInput{
			Ok:         view.Summary.Ok,
			Attention:  view.Summary.Attention,
			Damaged:    view.Summary.Damaged,
			Photos:     view.Summary.Photos,
			Confidence: view.Summary.Confidence,
		},
		Findings:      findings,
		Photos:        photos,
		Disclaimer:    application.ComparisonDisclaimer,
		LandlordName:  view.LandlordName,
		LandlordCPF:   view.LandlordCPF,
		AuthCode:      view.AuthCode,
		ValidationURL: view.ValidationURL,
		IssuedAt:      issuedAt,
		LogoPNG:       logoPNG,
	}
}
