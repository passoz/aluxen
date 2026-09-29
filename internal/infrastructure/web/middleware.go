package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Cadeia de middlewares. A ordem reproduz a do app atual (log → estáticos →
// trava de senha) e acrescenta recuperação de pânico, headers de segurança,
// redirecionamento HTTPS em produção e CSRF nas mutações.

type contextKey string

const (
	requestIDKey contextKey = "requestId"
	adminUserKey contextKey = "adminUser"
	clientIDKey  contextKey = "clientId"
)

// Router devolve o handler HTTP completo da aplicação.
func (h *Handlers) Router() http.Handler {
	mux := h.routes()
	handler := http.Handler(mux)
	handler = h.passwordGate(handler)
	handler = h.CSRF.Middleware(handler)
	handler = h.forceHTTPS(handler)
	handler = h.securityHeaders(handler)
	handler = h.serveStatic(handler)
	handler = h.requestLogger(handler)
	handler = h.recoverPanic(handler)
	return handler
}

func (h *Handlers) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				h.Log.Error("pânico no handler", "scope", "http", "err", recovered, "path", r.URL.Path)
				http.Error(w, "Erro interno do servidor.", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (h *Handlers) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := newRequestID()
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r.WithContext(ctx))

		duration := time.Since(started)
		level := h.Log.Info
		if recorder.status >= http.StatusInternalServerError {
			level = h.Log.Error
		} else if recorder.status >= http.StatusBadRequest {
			level = h.Log.Warn
		}
		attributes := []any{
			"scope", "http", "request_id", requestID, "method", r.Method,
			"path", r.URL.Path, "status", recorder.status,
			"duration_ms", duration.Milliseconds(), "ip", clientIP(r),
		}
		if userID := h.Session.AdminID(r); userID != "" {
			attributes = append(attributes, "user_id", userID)
		}
		level("requisição", attributes...)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func newRequestID() string {
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(buffer)
}

func (h *Handlers) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func (h *Handlers) forceHTTPS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.Cfg.IsProduction() {
			next.ServeHTTP(w, r)
			return
		}
		if isProbePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
			target := "https://" + r.Host + r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isProbePath diz se o caminho é consultado por sonda de saúde.
//
// As sondas (k8s liveness/readiness) falam HTTP direto com o pod e não passam
// pelo proxy que injeta o `X-Forwarded-Proto`. Redirecioná-las para HTTPS
// deixaria o pod permanentemente unhealthy — e, no k8s, preso em CrashLoop.
func isProbePath(path string) bool {
	return path == "/healthz" || path == "/readyz"
}

// serveStatic entrega os arquivos embutidos antes da trava de senha, para que
// CSS, JS e ícones carreguem em qualquer tela.
func (h *Handlers) serveStatic(next http.Handler) http.Handler {
	static := StaticHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		if !looksLikeStatic(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		static.ServeHTTP(w, r)
	})
}

func looksLikeStatic(path string) bool {
	if path == "/" {
		return false
	}
	if strings.HasPrefix(path, "/styles/") || strings.HasPrefix(path, "/assets/") {
		return true
	}
	last := path[strings.LastIndex(path, "/")+1:]
	return strings.Contains(last, ".")
}

// passwordGate trava o administrador que ainda usa a senha semeada. As rotas
// liberadas são as mesmas do app atual.
func (h *Handlers) passwordGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := h.Session.AdminID(r)
		if userID == "" || gateExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		mustChange, err := h.MustChangePassword(r.Context(), userID)
		if err != nil || !mustChange {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || wantsJSON(r) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Troca de senha obrigatória."})
			return
		}
		http.Redirect(w, r, "/settings?forced=1", http.StatusFound)
	})
}

func gateExempt(path string) bool {
	switch path {
	case "/login", "/logout", "/settings", "/api/health":
		return true
	}
	if strings.HasPrefix(path, "/portal") || strings.HasPrefix(path, "/validar") {
		return true
	}
	if looksLikeStatic(path) {
		return true
	}
	return false
}

// MustChangePassword é injetado pelo composition root (caso de uso do admin).
func (h *Handlers) MustChangePassword(ctx context.Context, userID string) (bool, error) {
	if h.MustChangePasswordFunc == nil {
		return false, nil
	}
	return h.MustChangePasswordFunc(ctx, userID)
}

// requireAdmin devolve o administrador autenticado ou escreve a resposta de
// recusa (401 em JSON, 302 para /login em HTML) e devolve false.
func (h *Handlers) requireAdmin(w http.ResponseWriter, r *http.Request) (*domain.User, bool) {
	userID := h.Session.AdminID(r)
	if userID == "" {
		h.unauthorized(w, r)
		return nil, false
	}
	user, err := application.AdminByID(r.Context(), h.Deps, userID)
	if err != nil {
		h.Session.ClearAdmin(w)
		h.unauthorized(w, r)
		return nil, false
	}
	return user, true
}

func (h *Handlers) unauthorized(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || wantsJSON(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Não autorizado."})
		return
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}

func wantsJSON(r *http.Request) bool {
	if r.URL.Query().Get("json") == "true" {
		return true
	}
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	writeJSONWith(w, status, payload, nil)
}

func writeJSONWith(w http.ResponseWriter, status int, payload any, headers map[string]string) {
	encoded, err := jsonMarshal(payload)
	if err != nil {
		http.Error(w, "Erro interno do servidor.", http.StatusInternalServerError)
		return
	}
	for key, value := range headers {
		w.Header().Set(key, value)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}
