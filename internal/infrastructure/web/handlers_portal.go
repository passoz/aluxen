package web

import "net/http"

// Área do Cliente (portal do locatário), somente leitura, isolada por titular.
//
//	GET  /portal/login      — página de acesso
//	POST /portal/login      — login por CPF; 302 /portal/password ou /portal
//	GET|POST /portal/logout — 302 /portal/login
//	GET  /portal            — contratos, recibos pagos e documentos do titular
//	GET  /portal/password   — troca obrigatória no primeiro acesso
//	POST /portal/password   — 302 /portal?ok=senha

func (h *Handlers) PortalLoginPage(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "portal.login.page")
}

func (h *Handlers) PortalLogin(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "portal.login")
}

func (h *Handlers) PortalLogout(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "portal.logout")
}

func (h *Handlers) PortalHome(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "portal.home")
}

func (h *Handlers) PortalPasswordPage(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "portal.password.page")
}

func (h *Handlers) PortalPassword(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "portal.password")
}
