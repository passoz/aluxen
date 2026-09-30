package web

import (
	"net/http"
	"strings"

	"github.com/passoz/aluxen/internal/application"
)

// Detalhamento filtrado do painel: o handler só lê a querystring e devolve o
// recorte calculado pela camada application. Sem regra de negócio aqui.

// Drilldown devolve os lançamentos do recorte com os totais.
func (h *Handlers) Drilldown(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	query := r.URL.Query()
	result, err := application.DrilldownReceipts(r.Context(), h.Deps, application.DrilldownFilter{
		From:       strings.TrimSpace(query.Get("from")),
		To:         strings.TrimSpace(query.Get("to")),
		Status:     strings.TrimSpace(query.Get("status")),
		Category:   strings.TrimSpace(query.Get("category")),
		TenantID:   strings.TrimSpace(query.Get("tenant_id")),
		PropertyID: strings.TrimSpace(query.Get("property_id")),
		Query:      query.Get("q"),
	})
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "drilldown": result})
}
