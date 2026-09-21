package web

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Comprovantes de pagamento com leitura assistida por IA e onboarding por
// documento (funcionalidades novas).
//
//	POST /api/payments/proof           — upload de comprovante (imagem/PDF) → extração + validação determinística
//	GET  /api/payments/proof/{id}/file — arquivo do comprovante (revisão à vista)
//	GET  /api/payments/review          — fila de revisão humana
//	POST /api/payments/review          — decisão humana (aceitar/recusar) sobre um comprovante ambíguo
//
// O onboarding por documento (intake) vive em handlers_intake.go.
//
// O handler é adaptador fino: lê o multipart/formulário, chama o caso de uso e
// devolve JSON. Nenhuma regra de decisão vive aqui — quem decide a baixa é
// internal/application (e, por baixo, internal/domain).
//
// O CSRF da instância é o double-submit do painel: as mutações exigem o campo
// `csrf_token` no corpo (formulários) ou na query (clientes que mandam JSON).

const (
	// maxProofBytes é o tamanho máximo aceito para um comprovante.
	maxProofBytes = 10 << 20 // 10 MiB
	// maxProofRequestBytes é o limite do corpo do upload (arquivo + campos).
	maxProofRequestBytes = maxProofBytes + (1 << 20)
	// maxReviewBodyBytes limita o corpo da decisão de revisão.
	maxReviewBodyBytes = 64 << 10
)

// PaymentProofUpload recebe o comprovante (multipart, campo `file`), guarda o
// arquivo e enfileira a leitura. A resposta não espera o provedor de IA.
func (h *Handlers) PaymentProofUpload(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	if !strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Envie o arquivo do comprovante."})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxProofRequestBytes)
	if err := r.ParseMultipartForm(maxProofBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Não foi possível ler os dados enviados."})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Envie o arquivo do comprovante."})
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			h.Log.Warn("arquivo do comprovante não fechado corretamente", "scope", "http", "err", err)
		}
	}()
	if header.Size > maxProofBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "O comprovante pode ter no máximo 10 MB."})
		return
	}
	content, err := io.ReadAll(io.LimitReader(file, maxProofBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Não foi possível ler o arquivo enviado."})
		return
	}

	outcome, err := application.SubmitPaymentProof(r.Context(), h.Deps, h.auditContext(r, domain.AuditOriginWeb), application.PaymentProofInput{
		Source:   domain.PaymentProofSource(strings.TrimSpace(r.FormValue("source"))),
		Phone:    r.FormValue("phone"),
		TenantID: r.FormValue("tenant_id"),
		FileName: header.Filename,
		Mime:     header.Header.Get("Content-Type"),
		Bytes:    content,
	})
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"proofId": outcome.ProofID,
		"jobId":   outcome.JobID,
		"status":  string(outcome.Status),
	})
}

// PaymentProofReviewList devolve a fila de revisão humana em JSON.
func (h *Handlers) PaymentProofReviewList(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	limit := application.PaymentProofReviewLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	rows, err := application.ListPaymentProofReview(r.Context(), h.Deps, limit)
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "proofs": rows})
}

// PaymentProofReviewDecision registra a decisão humana sobre um comprovante
// ambíguo. Aceita o formulário do painel (urlencoded) e JSON.
func (h *Handlers) PaymentProofReviewDecision(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	input, err := reviewDecisionInput(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Não foi possível ler os dados enviados."})
		return
	}
	input.DecidedBy = user.Username

	if err := application.DecidePaymentProof(r.Context(), h.Deps, h.auditContext(r, domain.AuditOriginWeb), input); err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// PaymentProofFile entrega o arquivo do comprovante para a revisão humana
// (rota registrada em router.go: GET /api/payments/proof/{id}/file).
//
// O comprovante é dado pessoal: só o administrador autenticado recebe o arquivo,
// e apenas quando ele é imagem ou PDF. Nada de cache intermediário.
func (h *Handlers) PaymentProofFile(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	if h.Deps.Attachments == nil {
		h.failJSONOrPage(w, r, http.StatusNotFound, "Arquivo do comprovante não encontrado.")
		return
	}

	proof, err := h.Deps.Repos.PaymentProofs.ByID(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	contentType, ok := proofContentType(proof)
	if !ok {
		h.failJSONOrPage(w, r, http.StatusNotFound, "Arquivo do comprovante não encontrado.")
		return
	}
	content, err := h.Deps.Attachments.Open(domain.Str(proof.FilePath))
	if err != nil {
		h.Log.Warn("arquivo do comprovante indisponível no armazenamento",
			"scope", "http", "proof_id", proof.ID, "err", err)
		h.failJSONOrPage(w, r, http.StatusNotFound, "Arquivo do comprovante não encontrado.")
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{
		"filename": paymentProofDownloadName(proof),
	}))
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(content); err != nil {
		h.Log.Warn("falha ao enviar o arquivo do comprovante", "scope", "http", "proof_id", proof.ID, "err", err)
	}
}

// paymentReviewRequest é o corpo aceito na decisão de revisão.
type paymentReviewRequest struct {
	ProofID   string `json:"proofId"`
	Approve   bool   `json:"approve"`
	ReceiptID string `json:"receiptId"`
	Reason    string `json:"reason"`
}

// reviewDecisionInput lê a decisão do formulário do painel ou de um corpo JSON.
func reviewDecisionInput(r *http.Request) (application.DecideInput, error) {
	var payload paymentReviewRequest
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		if err := json.NewDecoder(io.LimitReader(r.Body, maxReviewBodyBytes)).Decode(&payload); err != nil {
			return application.DecideInput{}, err
		}
	} else {
		if err := r.ParseForm(); err != nil {
			return application.DecideInput{}, err
		}
		payload = paymentReviewRequest{
			ProofID:   r.FormValue("proof_id"),
			Approve:   formApprove(r.FormValue("approve")),
			ReceiptID: r.FormValue("receipt_id"),
			Reason:    r.FormValue("reason"),
		}
	}
	return application.DecideInput{
		ProofID:   payload.ProofID,
		Approve:   payload.Approve,
		ReceiptID: payload.ReceiptID,
		Reason:    payload.Reason,
	}, nil
}

// formApprove interpreta o campo de aceite do formulário.
func formApprove(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on", "yes", "sim", "accept", "aceitar":
		return true
	default:
		return false
	}
}

// proofContentType resolve o tipo do arquivo do comprovante. Vale o `mime`
// gravado no registro; quando ele não é imagem nem PDF (cliente que manda
// `application/octet-stream`, comum em upload de celular) o tipo sai da extensão
// do arquivo. Sem imagem nem PDF o arquivo não é servido: esta rota nunca
// devolve um tipo de conteúdo arbitrário.
func proofContentType(proof *domain.PaymentProof) (string, bool) {
	contentType := strings.ToLower(strings.TrimSpace(domain.Str(proof.Mime)))
	if !servedProofType(contentType) {
		contentType = strings.ToLower(mime.TypeByExtension(strings.ToLower(filepath.Ext(domain.Str(proof.FileName)))))
	}
	if !servedProofType(contentType) {
		return "", false
	}
	return contentType, true
}

// servedProofType informa se o tipo é seguro para servir inline.
func servedProofType(contentType string) bool {
	switch contentType {
	case "image/jpeg", "image/png", "image/webp", "image/heic", "image/heif", "application/pdf":
		return true
	default:
		return false
	}
}

// paymentProofDownloadName é o nome sugerido ao abrir o arquivo do comprovante.
func paymentProofDownloadName(proof *domain.PaymentProof) string {
	if name := strings.TrimSpace(domain.Str(proof.FileName)); name != "" {
		return filepath.Base(name)
	}
	return "comprovante"
}
