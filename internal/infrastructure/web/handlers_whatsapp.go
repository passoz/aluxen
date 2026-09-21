package web

import "net/http"

// WhatsApp via WAHA (substitui o robô headless com Chromium).
//
//	GET  /api/whatsapp/status            — {isLoggedIn,hasQRCode,chromiumAvailable,profileDir}
//	POST /api/whatsapp/login/start       — inicia sessão/pareamento
//	GET  /api/whatsapp/login/status      — estado do pareamento (qrCode, message, error)
//	POST /api/whatsapp/login/cancel      — cancela o pareamento
//	POST /api/whatsapp/logout            — encerra a sessão
//	POST /api/whatsapp/preview           — monta a mensagem (type=receipt|lease, id)
//	POST /api/whatsapp/send              — cria job assíncrono, devolve {jobId}
//	GET  /api/whatsapp/send/status       — estado do job (jobId)
//	POST /api/whatsapp/webhook           — mensagens recebidas (WAHA); isento de CSRF, valida HMAC

func (h *Handlers) WhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "whatsapp.status")
}

func (h *Handlers) WhatsAppLoginStart(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "whatsapp.login.start")
}

func (h *Handlers) WhatsAppLoginStatus(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "whatsapp.login.status")
}

func (h *Handlers) WhatsAppLoginCancel(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "whatsapp.login.cancel")
}

func (h *Handlers) WhatsAppLogout(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "whatsapp.logout")
}

func (h *Handlers) WhatsAppPreview(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "whatsapp.preview")
}

func (h *Handlers) WhatsAppSend(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "whatsapp.send")
}

func (h *Handlers) WhatsAppSendStatus(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "whatsapp.send.status")
}

func (h *Handlers) WhatsAppWebhook(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "whatsapp.webhook")
}
