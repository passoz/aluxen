package web

import "net/http"

// Painel administrativo: tela e mutações. Substituir o stub pela implementação
// real (application + templates) mantendo as assinaturas.
//
//	GET  /dashboard   — abas dashboard, tenants, properties, leases, receipts, history; `json=true` devolve JSON
//	POST /dashboard   — intents de mutação, resposta {"ok":true} ou 400 {"error"}

func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "dashboard.get")
}

func (h *Handlers) DashboardMutation(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "dashboard.post")
}
