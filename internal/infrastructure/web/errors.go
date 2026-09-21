package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
)

// Tradução de erro de domínio → resposta HTTP. A mensagem de validação vai
// literal para o usuário (é o texto que o app atual devolve); erro inesperado
// nunca vaza detalhe: vira 500 com o requestId para correlação no log.

func jsonMarshal(payload any) ([]byte, error) {
	return json.Marshal(payload)
}

// mapError escreve a resposta adequada para err.
func (h *Handlers) mapError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.failJSONOrPage(w, r, http.StatusNotFound, "Registro não encontrado.")
	case errors.Is(err, domain.ErrConflict):
		h.failJSONOrPage(w, r, http.StatusConflict, "Operação não permitida pelo estado atual do registro.")
	case errors.Is(err, domain.ErrUnauthorized):
		h.failJSONOrPage(w, r, http.StatusUnauthorized, "Não autorizado.")
	case domain.IsValidation(err):
		h.failJSONOrPage(w, r, http.StatusBadRequest, domain.ValidationMessage(err))
	default:
		h.Log.Error("erro não tratado no handler", "scope", "http", "err", err, "path", r.URL.Path)
		h.failJSONOrPage(w, r, http.StatusInternalServerError, "Erro interno do servidor.")
	}
}

func (h *Handlers) failJSONOrPage(w http.ResponseWriter, r *http.Request, status int, message string) {
	if strings.HasPrefix(r.URL.Path, "/api/") || wantsJSON(r) {
		writeJSON(w, status, map[string]string{"error": message, "requestId": requestIDFrom(r)})
		return
	}
	h.renderError(w, r, status, message)
}

// renderError devolve a página de erro HTML.
func (h *Handlers) renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	data := &PageData{
		Title:  "Aluxen | Erro",
		Page:   "error",
		AppURL: h.Cfg.AppURL,
		Build:  h.Build,
		Data: map[string]any{
			"Status":  status,
			"Message": message,
		},
	}
	if err := h.Render.Render(w, status, "error.html", data); err != nil {
		http.Error(w, message, status)
	}
}

// notFound é o handler padrão de rota inexistente.
func (h *Handlers) notFound(w http.ResponseWriter, r *http.Request) {
	h.Log.Warn("rota não encontrada", "scope", "http", "path", r.URL.Path)
	if strings.HasPrefix(r.URL.Path, "/api/") || wantsJSON(r) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Rota não encontrada.", "requestId": requestIDFrom(r)})
		return
	}
	h.renderError(w, r, http.StatusNotFound, "Página não encontrada.")
}

func requestIDFrom(r *http.Request) string {
	if value, ok := r.Context().Value(requestIDKey).(string); ok {
		return value
	}
	return ""
}
