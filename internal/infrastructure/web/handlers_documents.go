package web

import "net/http"

// Documentos com prova de autenticidade.
//
//	GET /pdf                — PDF do recibo (id, token, assinatura); admin, token HMAC ou locatário titular
//	GET /contract-pdf       — PDF do contrato (id, token)
//	GET /validar/recibo/{id}   — página pública de validação do recibo
//	GET /validar/contrato/{id} — página pública de validação do contrato

func (h *Handlers) ReceiptPDF(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "pdf.receipt")
}

func (h *Handlers) ContractPDF(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "pdf.contract")
}

func (h *Handlers) VerifyReceipt(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "verify.receipt")
}

func (h *Handlers) VerifyContract(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "verify.contract")
}
