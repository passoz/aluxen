package web

import "net/http"

// Comprovantes de pagamento com leitura assistida por IA e onboarding por
// documento (funcionalidades novas).
//
//	POST /api/payments/proof      — upload de comprovante (imagem/PDF) → extração + validação determinística
//	GET  /api/payments/review     — fila de revisão humana
//	POST /api/payments/review     — decisão humana (aceitar/recusar) sobre um comprovante ambíguo
//
// O onboarding por documento (intake) vive em handlers_intake.go.

func (h *Handlers) PaymentProofUpload(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "payments.proof.upload")
}

func (h *Handlers) PaymentProofReviewList(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "payments.review.list")
}

func (h *Handlers) PaymentProofReviewDecision(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "payments.review.decision")
}
