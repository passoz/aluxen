package web

import "net/http"

// Temporada: caução, vistoria e calendário de ocupação (funcionalidades novas).
//
//	POST /dashboard/deposit      — lançar/devolver caução do contrato
//	POST /dashboard/inspection   — registrar vistoria de entrada/saída com itens e fotos
//	GET  /inspection-pdf         — PDF do auto de vistoria (token HMAC ou admin)
//	GET  /deposit-pdf            — recibo de devolução de caução (token HMAC ou admin)

func (h *Handlers) DepositMutation(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "season.deposit")
}

func (h *Handlers) InspectionMutation(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "season.inspection")
}

func (h *Handlers) InspectionPDF(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "season.inspection.pdf")
}

func (h *Handlers) DepositReceiptPDF(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "season.deposit.pdf")
}
