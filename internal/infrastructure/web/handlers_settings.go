package web

import "net/http"

// Configurações: dados do locador, URL pública e troca de senha.
//
//	GET  /settings — página (isenta da trava de senha); `?forced=1` mostra o modo obrigatório
//	POST /settings — intents save-landlord, save-app-url, change-password

func (h *Handlers) SettingsPage(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "settings.get")
}

func (h *Handlers) SettingsMutation(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "settings.post")
}
