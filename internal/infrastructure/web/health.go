package web

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Healthz é liveness: não toca dependência externa — banco lento não pode
// reiniciar um processo saudável.
func (h *Handlers) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// Readyz é readiness: só passa com ping real no banco.
func (h *Handlers) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if h.Ping == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
		return
	}
	if err := h.Ping(ctx); err != nil {
		h.Log.Error("banco indisponível na readiness", "scope", "http", "err", err)
		http.Error(w, "banco indisponível", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

// Health expõe o mesmo payload de /api/health do app atual, com os nomes da
// stack nova.
func (h *Handlers) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":    "ok",
		"runtime":   "go",
		"framework": "net/http",
		"database":  h.databaseDescription(),
		"orm":       "sqlc",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *Handlers) databaseDescription() string {
	switch h.Dialect {
	case "postgres":
		return "postgres (pgx)"
	case "sqlite":
		return "sqlite (modernc)"
	default:
		return fmt.Sprintf("desconhecido (%s)", h.Dialect)
	}
}
