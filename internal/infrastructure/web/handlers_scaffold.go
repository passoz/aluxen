package web

import "net/http"

// ── ANDAIME TEMPORÁRIO DO PORT ─────────────────────────────────────
//
// Este arquivo e os métodos `notImplemented` existem apenas enquanto as áreas
// do port não estão implementadas: cada área tem o seu arquivo `handlers_*.go`,
// que substitui o stub correspondente. Nenhum stub pode sobreviver ao fim do
// Work — a verificação final é `grep -rn "não implementado" internal/`.

// notImplemented responde 501 enquanto a área não é implementada.
func (h *Handlers) notImplemented(w http.ResponseWriter, r *http.Request, area string) {
	h.Log.Warn("área ainda não implementada", "scope", "http", "area", area, "path", r.URL.Path)
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error": "não implementado: " + area,
	})
}
