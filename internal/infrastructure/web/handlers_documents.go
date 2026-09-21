package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
	"github.com/passoz/aluxen/internal/infrastructure/authz"
	"github.com/passoz/aluxen/internal/infrastructure/pdf"
)

// Documentos com prova de autenticidade.
//
//	GET /pdf                    — PDF do recibo (id, token, assinatura); admin, token HMAC ou locatário titular
//	GET /contract-pdf           — PDF do contrato (id, token)
//	GET /validar/recibo/{id}    — página pública de validação do recibo
//	GET /validar/contrato/{id}  — página pública de validação do contrato
//
// O selo de autenticidade é o mesmo do app atual: token HMAC do documento no QR
// Code e na chave AUTH impressa no rodapé.

// Imagens embutidas de `static/` usadas nos documentos, lidas uma única vez.
var (
	signatureImage = sync.OnceValue(func() []byte {
		image, err := staticFS.ReadFile("static/assets/assinatura01.png")
		if err != nil {
			return nil
		}
		return image
	})
	logoImage = sync.OnceValue(func() []byte {
		image, err := staticFS.ReadFile("static/logomarca.png")
		if err != nil {
			return nil
		}
		return image
	})
)

// ReceiptPDF emite o recibo em PDF.
func (h *Handlers) ReceiptPDF(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}
	token := r.URL.Query().Get("token")
	showSignature := r.URL.Query().Get("assinatura") != "false"

	if !h.documentAccessGranted(w, r, authz.KindReceipt, id, token, func() (bool, error) {
		return application.CanAccessReceipt(r.Context(), h.Deps, id, h.Session.AdminID(r), h.Session.ClientID(r))
	}) {
		return
	}

	data, err := application.ReceiptDocumentInput(r.Context(), h.Deps, id)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		http.Error(w, "Recibo não encontrado", http.StatusNotFound)
		return
	case err != nil:
		h.mapError(w, r, err)
		return
	}
	// Recibo pendente/vencido não gera documento: só o pagamento (integral ou
	// parcial) libera a emissão.
	if !data.IsPaid {
		http.Error(w, "Recibo não pago. O recibo só é liberado após o pagamento.", http.StatusForbidden)
		return
	}

	document, err := pdf.BuildReceipt(pdf.ReceiptInput{
		ReceiptID:       data.ReceiptID,
		TenantName:      data.TenantName,
		TenantCPF:       data.TenantCPF,
		PropertyName:    data.PropertyName,
		PropertyAddress: data.PropertyAddress,
		Category:        string(data.Category),
		Description:     data.Description,
		DueDate:         data.DueDate,
		PaidAt:          data.PaidAt,
		Amount:          data.Amount,
		PaidAmount:      data.PaidAmount,
		HasPaidAmount:   data.HasPaidAmount,
		LandlordName:    data.LandlordName,
		LandlordCPF:     data.LandlordCPF,
		Signature:       showSignature,
		SignaturePNG:    signatureImage(),
		LogoPNG:         logoImage(),
		AuthCode:        data.AuthCode,
		ValidationURL:   data.ValidationURL,
		IssuedAt:        data.IssuedAt,
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

	h.auditDocument(r, domain.ActionDocumentReceipt, data.ReceiptID, data.Label,
		"Recibo de "+data.Label+" emitido",
		map[string]any{"kind": "Recibo", "signature": showSignature})

	writePDF(w, fmt.Sprintf(`recibo-%s.pdf`, data.ReceiptID), rendered)
}

// ContractPDF emite o contrato de locação em PDF.
func (h *Handlers) ContractPDF(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}
	token := r.URL.Query().Get("token")

	if !h.documentAccessGranted(w, r, authz.KindLease, id, token, func() (bool, error) {
		return application.CanAccessLease(r.Context(), h.Deps, id, h.Session.AdminID(r), h.Session.ClientID(r))
	}) {
		return
	}

	data, err := application.LeaseDocumentInput(r.Context(), h.Deps, id)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		http.Error(w, "Contrato não encontrado", http.StatusNotFound)
		return
	case err != nil:
		h.mapError(w, r, err)
		return
	}

	document, err := pdf.BuildContract(pdf.ContractInput{
		LeaseID:         data.LeaseID,
		TenantName:      data.TenantName,
		TenantPhone:     data.TenantPhone,
		TenantCPF:       data.TenantCPF,
		PropertyName:    data.PropertyName,
		PropertyAddress: data.PropertyStreet,
		PropertyCity:    data.PropertyCity,
		PropertyState:   data.PropertyState,
		PropertyZip:     data.PropertyZip,
		StartDate:       data.StartDate,
		EndDate:         data.EndDate,
		DueDay:          data.DueDay,
		RentAmount:      data.RentAmount,
		Status:          string(data.Status),
		LandlordName:    data.LandlordName,
		LandlordCPF:     data.LandlordCPF,
		LandlordAddress: data.LandlordAddress,
		AuthCode:        data.AuthCode,
		ValidationURL:   data.ValidationURL,
		IssuedAt:        data.IssuedAt,
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

	h.auditDocument(r, domain.ActionDocumentContract, data.LeaseID, data.Label,
		"Contrato de "+data.Label+" emitido",
		map[string]any{"kind": "Contrato", "signature": true})

	writePDF(w, fmt.Sprintf(`contrato-%s.pdf`, data.LeaseID), rendered)
}

// VerifyReceipt renderiza a página pública de validação do recibo.
func (h *Handlers) VerifyReceipt(w http.ResponseWriter, r *http.Request) {
	data, err := application.VerifyReceiptToken(r.Context(), h.Deps, r.PathValue("id"), r.URL.Query().Get("token"))
	if err != nil {
		h.renderVerifyFailure(w, r, "Aluxen | Autenticidade do Recibo", err, map[string]string{
			"notFound": "Recibo não localizado na base oficial de registros.",
		})
		return
	}
	// Recibo ainda não pago não é liberado para validação pública.
	if !data.IsPaid {
		h.renderVerify(w, r, http.StatusForbidden, "Aluxen | Autenticidade do Recibo", verifyView{
			Error: "Recibo ainda não pago. O documento só é liberado após o pagamento.",
		})
		return
	}

	view := verifyView{
		Valid:           true,
		Type:            "receipt",
		TypeLabel:       "Recibo de Pagamento de Aluguel",
		ID:              data.ReceiptID,
		Token:           data.Token,
		AuthCode:        data.AuthCode,
		TenantName:      data.TenantName,
		TenantCPF:       data.TenantCPF,
		PropertyName:    data.PropertyName,
		PropertyAddress: data.PropertyAddress,
		LandlordName:    data.LandlordName,
		Amount:          format.FormatCurrency(data.Amount),
		AmountRaw:       data.Amount,
		Category:        domain.ReceiptCategoryLabel(data.Category),
		Description:     data.Description,
		DueDate:         format.FormatDateBR(data.DueDate),
		PaidAt:          format.FormatDateTimeBR(data.PaidAt),
		IsPaid:          data.IsPaid,
		IsPartial:       data.IsPartial,
		DownloadURL:     "/pdf?id=" + data.ReceiptID + "&token=" + data.Token,
	}
	if data.HasPaidAmount {
		view.PaidAmount = format.FormatCurrency(data.PaidAmount)
		view.PaidAmountRaw = data.PaidAmount
		if data.IsPartial {
			view.Balance = format.FormatCurrency(data.Amount - data.PaidAmount)
		}
	}
	h.renderVerify(w, r, http.StatusOK, "Aluxen | Autenticidade do Recibo", view)
}

// VerifyContract renderiza a página pública de validação do contrato.
func (h *Handlers) VerifyContract(w http.ResponseWriter, r *http.Request) {
	data, err := application.VerifyLeaseToken(r.Context(), h.Deps, r.PathValue("id"), r.URL.Query().Get("token"))
	if err != nil {
		h.renderVerifyFailure(w, r, "Aluxen | Autenticidade do Contrato", err, map[string]string{
			"notFound": "Contrato não localizado na base oficial de registros.",
		})
		return
	}

	h.renderVerify(w, r, http.StatusOK, "Aluxen | Autenticidade do Contrato", verifyView{
		Valid:           true,
		Type:            "lease",
		TypeLabel:       "Contrato de Locação Residencial",
		ID:              data.LeaseID,
		Token:           data.Token,
		AuthCode:        data.AuthCode,
		TenantName:      data.TenantName,
		TenantCPF:       data.TenantCPF,
		PropertyName:    data.PropertyName,
		PropertyAddress: data.PropertyAddress,
		LandlordName:    data.LandlordName,
		Amount:          format.FormatCurrency(data.RentAmount),
		AmountRaw:       data.RentAmount,
		StartDate:       format.FormatDateBR(data.StartDate),
		EndDate:         format.FormatDateBR(data.EndDate),
		DueDay:          data.DueDay,
		Status:          string(data.Status),
		StatusLabel:     domain.LeaseStatusLabel(data.Status),
		DownloadURL:     "/contract-pdf?id=" + data.LeaseID + "&token=" + data.Token,
	})
}

// verifyView é o contexto da página pública de validação (`verify.html`).
type verifyView struct {
	// Valid marca o selo (verde quando o documento é autêntico, vermelho quando
	// a chave não confere).
	Valid     bool
	Type      string
	TypeLabel string
	Error     string

	ID       string
	Token    string
	AuthCode string

	TenantName      string
	TenantCPF       string
	PropertyName    string
	PropertyAddress string
	LandlordName    string

	Amount    string
	AmountRaw float64
	// PaidAmount/PaidAmountRaw só existem quando o lançamento tem valor pago
	// registrado (mesmo que seja o valor cheio).
	PaidAmount    string
	PaidAmountRaw float64
	IsPaid        bool
	IsPartial     bool
	Balance       string

	Category    string
	Description string
	DueDate     string
	PaidAt      string

	StartDate   string
	EndDate     string
	DueDay      int
	Status      string
	StatusLabel string

	// Auto de vistoria: rótulo do tipo, data e hora da realização, estado geral e
	// os totais conferidos (o período do contrato usa StartDate/EndDate).
	KindLabel   string
	PerformedAt string
	Condition   string
	ItemsCount  int
	PhotosCount int

	// Recibo de devolução de caução: valor devolvido e valor retido (o valor da
	// caução usa Amount).
	RefundedAmount    string
	RefundedAmountRaw float64
	RetainedAmount    string
	RetainedAmountRaw float64
	RefundedAt        string

	VerifiedAt  string
	DownloadURL string
}

// renderVerifyFailure traduz o erro do caso de uso na página pública: chave
// inválida vira 400, documento inexistente vira 404 e o resto segue o mapa
// padrão de erro.
func (h *Handlers) renderVerifyFailure(w http.ResponseWriter, r *http.Request, title string, err error, messages map[string]string) {
	switch {
	case domain.IsValidation(err):
		h.renderVerify(w, r, http.StatusBadRequest, title, verifyView{Error: domain.ValidationMessage(err)})
	case errors.Is(err, domain.ErrNotFound):
		h.renderVerify(w, r, http.StatusNotFound, title, verifyView{Error: messages["notFound"]})
	default:
		h.mapError(w, r, err)
	}
}

// renderVerify renderiza a página pública com o status indicado.
func (h *Handlers) renderVerify(w http.ResponseWriter, r *http.Request, status int, title string, view verifyView) {
	// A consulta é datada em dd/mm/aaaa HH:mm, como no app atual.
	view.VerifiedAt = format.FormatDateTimeBR(format.FormatLocalDateTime(h.Deps.Clock.Now()))

	data := &PageData{
		Title:     title,
		Page:      "verify",
		CSRFToken: h.CSRF.Token(w, r),
		Build:     h.Build,
		AppURL:    h.Cfg.AppURL,
		Data:      view,
	}
	if err := h.Render.Render(w, status, "verify.html", data); err != nil {
		h.Log.Error("falha ao renderizar a validação pública", "scope", "http", "err", err)
		http.Error(w, view.Error, status)
	}
}

// documentAccessGranted decide o acesso ao documento: administrador logado,
// chave de autenticidade válida ou locatário titular (resolvido por `owner`).
//
// Sem nenhuma das três, o locatário logado recebe 403 (o documento não é dele) e
// quem não está logado vai para o login — como no app atual.
func (h *Handlers) documentAccessGranted(w http.ResponseWriter, r *http.Request, kind, id, token string, owner func() (bool, error)) bool {
	if h.Session.AdminID(r) != "" {
		return true
	}
	if token != "" && h.verifier().Verify(kind, id, token) {
		return true
	}
	owns, err := owner()
	if err != nil {
		h.mapError(w, r, err)
		return false
	}
	if owns {
		return true
	}
	if h.Session.ClientID(r) != "" {
		http.Error(w, "Este documento não pertence ao seu cadastro.", http.StatusForbidden)
		return false
	}
	h.unauthorized(w, r)
	return false
}

// verifier devolve o gerador dos tokens de autenticidade da instância. O
// composition root injeta o gerador em `Deps.Signer` (é a mesma implementação de
// `internal/infrastructure/authz`); quando a composição não o injetou, o segredo
// da configuração (`SESSION_SECRET`) constrói um gerador equivalente. Não existe
// segredo embutido no código.
func (h *Handlers) verifier() authz.Verifier {
	if verifier, ok := h.Deps.Signer.(authz.Verifier); ok {
		return verifier
	}
	secret := ""
	if h.Cfg != nil {
		secret = h.Cfg.SessionSecret
	}
	return authz.NewTokenGenerator(secret)
}

// auditDocument grava a emissão do documento na trilha. A origem é o painel
// quando quem pediu está logado e o link público quando o pedido veio do token.
func (h *Handlers) auditDocument(r *http.Request, action, entityID, label, summary string, metadata map[string]any) {
	origin := domain.AuditOriginPublic
	if h.Session.AdminID(r) != "" {
		origin = domain.AuditOriginWeb
	}
	if err := application.Record(r.Context(), h.Deps, h.auditContext(r, origin), application.AuditEntry{
		Action:      action,
		Entity:      domain.EntityDocument,
		EntityID:    entityID,
		EntityLabel: label,
		Summary:     summary,
		Metadata:    metadata,
	}); err != nil {
		h.Log.Warn("auditoria de documento não gravada", "scope", "http", "action", action, "err", err)
	}
}

// writePDF devolve o PDF para exibição no navegador.
func writePDF(w http.ResponseWriter, filename string, document []byte) {
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(document)
}
