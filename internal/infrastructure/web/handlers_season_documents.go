package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
	"github.com/passoz/aluxen/internal/infrastructure/authz"
	"github.com/passoz/aluxen/internal/infrastructure/pdf"
)

// Documentos da temporada: auto de vistoria e recibo de devolução de caução.
//
//	GET /inspection-pdf?id=&token=       — PDF do auto de vistoria
//	GET /deposit-pdf?id=&token=          — PDF do recibo de devolução de caução
//	GET /validar/vistoria/{id}?token=    — página pública de validação da vistoria
//	GET /validar/caucao/{id}?token=      — página pública de validação da caução
//
// O acesso ao PDF vale a mesma regra dos demais documentos (administrador
// logado, chave de autenticidade válida ou o locatário titular do contrato) e a
// página pública exige a chave do próprio documento: o link é a credencial.

// InspectionPDF emite o auto de vistoria em PDF.
func (h *Handlers) InspectionPDF(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}
	token := r.URL.Query().Get("token")

	if !h.documentAccessGranted(w, r, authz.KindInspection, id, token, func() (bool, error) {
		return application.CanAccessInspection(r.Context(), h.Deps, id, h.Session.AdminID(r), h.Session.ClientID(r))
	}) {
		return
	}

	data, err := application.InspectionDocumentInput(r.Context(), h.Deps, id)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		http.Error(w, "Vistoria não encontrada", http.StatusNotFound)
		return
	case err != nil:
		h.mapError(w, r, err)
		return
	}

	document, err := pdf.BuildInspection(pdf.InspectionInput{
		InspectionID:    data.InspectionID,
		Kind:            string(data.Kind),
		LeaseID:         data.LeaseID,
		TenantName:      data.TenantName,
		TenantCPF:       data.TenantCPF,
		PropertyName:    data.PropertyName,
		PropertyAddress: data.PropertyAddress,
		StartDate:       data.StartDate,
		EndDate:         data.EndDate,
		PerformedAt:     data.PerformedAt,
		Condition:       string(data.Condition),
		Notes:           data.Notes,
		Items:           inspectionItemInputs(data.Items),
		Photos:          inspectionPhotoInputs(data.Photos),
		LandlordName:    data.LandlordName,
		LandlordCPF:     data.LandlordCPF,
		AuthCode:        data.AuthCode,
		ValidationURL:   data.ValidationURL,
		IssuedAt:        data.IssuedAt,
		LogoPNG:         logoImage(),
	})
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	rendered, err := pdf.Render(document)
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	h.auditDocument(r, domain.ActionDocumentInspection, data.InspectionID, data.Label,
		data.KindLabel+" em "+data.PropertyName+" emitida",
		map[string]any{"kind": data.KindLabel, "count": len(data.Items)})

	writePDF(w, fmt.Sprintf("vistoria-%s.pdf", data.InspectionID), rendered)
}

// DepositReceiptPDF emite o recibo de devolução de caução em PDF.
func (h *Handlers) DepositReceiptPDF(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}
	token := r.URL.Query().Get("token")

	if !h.documentAccessGranted(w, r, authz.KindDeposit, id, token, func() (bool, error) {
		return application.CanAccessDeposit(r.Context(), h.Deps, id, h.Session.AdminID(r), h.Session.ClientID(r))
	}) {
		return
	}

	data, err := application.DepositDocumentInput(r.Context(), h.Deps, id)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		http.Error(w, "Caução não encontrada", http.StatusNotFound)
		return
	case err != nil:
		h.mapError(w, r, err)
		return
	}
	// Contrato sem caução registrada não gera recibo de devolução.
	if !data.HasDeposit {
		http.Error(w, "Este contrato não tem caução registrada.", http.StatusForbidden)
		return
	}

	document, err := pdf.BuildDepositReceipt(pdf.DepositReceiptInput{
		LeaseID:         data.LeaseID,
		TenantName:      data.TenantName,
		TenantCPF:       data.TenantCPF,
		PropertyName:    data.PropertyName,
		PropertyAddress: data.PropertyAddress,
		LandlordName:    data.LandlordName,
		LandlordCPF:     data.LandlordCPF,
		Amount:          data.Amount,
		Refunded:        data.Refunded,
		Retained:        data.Retained,
		Status:          string(data.Status),
		ReceivedAt:      data.ReceivedAt,
		RefundedAt:      data.RefundedAt,
		Notes:           data.Notes,
		AuthCode:        data.AuthCode,
		ValidationURL:   data.ValidationURL,
		IssuedAt:        data.IssuedAt,
		LogoPNG:         logoImage(),
	})
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	rendered, err := pdf.Render(document)
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	h.auditDocument(r, domain.ActionDocumentDeposit, data.LeaseID, data.Label,
		"Recibo de devolução de caução em "+data.PropertyName+" emitido",
		map[string]any{"kind": "Recibo de devolução de caução", "amount": domain.FormatNumber(data.Amount)})

	writePDF(w, fmt.Sprintf("caucao-%s.pdf", data.LeaseID), rendered)
}

// VerifyInspection renderiza a página pública de validação do auto de vistoria.
func (h *Handlers) VerifyInspection(w http.ResponseWriter, r *http.Request) {
	data, err := application.VerifyInspectionToken(r.Context(), h.Deps, r.PathValue("id"), r.URL.Query().Get("token"))
	if err != nil {
		h.renderVerifyFailure(w, r, "Aluxen | Autenticidade do Auto de Vistoria", err, map[string]string{
			"notFound": "Vistoria não localizada na base oficial de registros.",
		})
		return
	}

	h.renderVerify(w, r, http.StatusOK, "Aluxen | Autenticidade do Auto de Vistoria", verifyView{
		Valid:           true,
		Type:            "inspection",
		TypeLabel:       inspectionVerifyLabel(data.Kind),
		ID:              data.InspectionID,
		Token:           data.Token,
		AuthCode:        data.AuthCode,
		TenantName:      data.TenantName,
		TenantCPF:       data.TenantCPF,
		PropertyName:    data.PropertyName,
		PropertyAddress: data.PropertyAddress,
		LandlordName:    data.LandlordName,
		KindLabel:       data.KindLabel,
		PerformedAt:     format.FormatDateTimeBR(data.PerformedAt),
		Condition:       data.ConditionLabel,
		ItemsCount:      len(data.Items),
		PhotosCount:     len(data.Photos),
		StartDate:       format.FormatDateBR(data.StartDate),
		EndDate:         format.FormatDateBR(data.EndDate),
		DownloadURL:     "/inspection-pdf?id=" + data.InspectionID + "&token=" + data.Token,
	})
}

// VerifyDeposit renderiza a página pública de validação do recibo de devolução
// de caução.
func (h *Handlers) VerifyDeposit(w http.ResponseWriter, r *http.Request) {
	data, err := application.VerifyDepositToken(r.Context(), h.Deps, r.PathValue("id"), r.URL.Query().Get("token"))
	if err != nil {
		h.renderVerifyFailure(w, r, "Aluxen | Autenticidade do Recibo de Caução", err, map[string]string{
			"notFound": "Caução não localizada na base oficial de registros.",
		})
		return
	}

	view := verifyView{
		Valid:           true,
		Type:            "deposit",
		TypeLabel:       "Recibo de Devolução de Caução",
		ID:              data.LeaseID,
		Token:           data.Token,
		AuthCode:        data.AuthCode,
		TenantName:      data.TenantName,
		TenantCPF:       data.TenantCPF,
		PropertyName:    data.PropertyName,
		PropertyAddress: data.PropertyAddress,
		LandlordName:    data.LandlordName,
		Amount:          format.FormatCurrency(data.Amount),
		AmountRaw:       data.Amount,
		Status:          string(data.Status),
		StatusLabel:     data.StatusLabel,
		DownloadURL:     "/deposit-pdf?id=" + data.LeaseID + "&token=" + data.Token,
	}
	if data.Refunded > 0 {
		view.RefundedAmount = format.FormatCurrency(data.Refunded)
		view.RefundedAmountRaw = data.Refunded
	}
	if data.Retained > 0 {
		view.RetainedAmount = format.FormatCurrency(data.Retained)
		view.RetainedAmountRaw = data.Retained
	}
	if data.RefundedAt != "" {
		view.RefundedAt = format.FormatDateTimeBR(data.RefundedAt)
	}
	h.renderVerify(w, r, http.StatusOK, "Aluxen | Autenticidade do Recibo de Caução", view)
}

// inspectionVerifyLabel é o rótulo do tipo de vistoria exibido na página
// pública, no mesmo tom do título do PDF.
func inspectionVerifyLabel(kind domain.InspectionKind) string {
	if kind == domain.InspectionKindCheckOut {
		return "Auto de Vistoria de Saída"
	}
	return "Auto de Vistoria de Entrada"
}

// inspectionItemInputs converte os itens conferidos para o modelo do PDF.
func inspectionItemInputs(items []domain.InspectionItem) []pdf.InspectionItemInput {
	if len(items) == 0 {
		return nil
	}
	converted := make([]pdf.InspectionItemInput, 0, len(items))
	for _, item := range items {
		converted = append(converted, pdf.InspectionItemInput{
			Area:  item.Area,
			Item:  item.Item,
			State: item.State,
			Notes: item.Notes,
		})
	}
	return converted
}

// inspectionPhotoInputs converte as fotos anexadas para o modelo do PDF: o
// arquivo continua no armazenamento da instância e o documento só o referencia.
func inspectionPhotoInputs(photos []domain.InspectionPhoto) []pdf.InspectionPhotoInput {
	if len(photos) == 0 {
		return nil
	}
	converted := make([]pdf.InspectionPhotoInput, 0, len(photos))
	for _, photo := range photos {
		converted = append(converted, pdf.InspectionPhotoInput{
			Name:    photo.Name,
			Label:   photo.Label,
			Caption: photo.Caption,
		})
	}
	return converted
}
