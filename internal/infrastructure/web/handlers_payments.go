package web

import "net/http"

// Comprovantes de pagamento com leitura assistida por IA e onboarding por
// documento (funcionalidades novas).
//
//	POST /api/payments/proof      — upload de comprovante (imagem/PDF) → extração + validação determinística
//	GET  /api/payments/review     — fila de revisão humana
//	POST /api/payments/review     — decisão humana (aceitar/recusar) sobre um comprovante ambíguo
//	POST /api/intake/document     — upload de contrato antigo/RG/CNH → proposta de cadastro
//	POST /api/intake/confirm      — confirmação humana antes de gravar o cadastro

func (h *Handlers) PaymentProofUpload(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "payments.proof.upload")
}

func (h *Handlers) PaymentProofReviewList(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "payments.review.list")
}

func (h *Handlers) PaymentProofReviewDecision(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "payments.review.decision")
}

func (h *Handlers) DocumentIntake(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "intake.document")
}

func (h *Handlers) DocumentIntakeConfirm(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "intake.confirm")
}
