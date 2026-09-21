// Package web contém os adaptadores HTTP: router, middlewares, sessão, CSRF,
// renderização de templates e os handlers. Handlers são finos: decodificam a
// requisição, chamam o caso de uso em internal/application e codificam a resposta.
package web

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/infrastructure/buildinfo"
	"github.com/passoz/aluxen/internal/infrastructure/config"
)

// Common reúne tudo que todo handler precisa. É construído uma vez no
// composition root e embutido em Handlers.
type Common struct {
	Cfg     *config.Config
	Log     *slog.Logger
	Render  *Renderer
	Session *SessionManager
	CSRF    *CSRF

	// Deps são as dependências dos casos de uso (repositórios, clock, ids, hasher).
	Deps application.Deps

	Build   buildinfo.Info
	Started time.Time

	// Dialect, DBPath e DBSize alimentam /api/health e /api/info.
	Dialect string
	DBPath  string
	DBSize  func() int64
	Ping    func(ctx context.Context) error

	// MustChangePasswordFunc informa se o administrador ainda usa a senha
	// semeada (trava de senha padrão). Injetado pelo composition root.
	MustChangePasswordFunc func(ctx context.Context, userID string) (bool, error)

	// LoginGate aplica o rate limit e o bloqueio progressivo do login.
	LoginGate *application.LoginGate
}

// Handlers expõe os métodos HTTP. Cada área tem seu arquivo (handlers_*.go).
type Handlers struct {
	*Common
}

// NewHandlers cria o conjunto de handlers sobre as dependências comuns.
func NewHandlers(common *Common) *Handlers {
	return &Handlers{Common: common}
}

// clientIP reproduz a resolução de IP do app atual: primeiro item de
// X-Forwarded-For, senão X-Real-IP, senão "desconhecido".
func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		return strings.TrimSpace(first)
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return strings.TrimSpace(realIP)
	}
	return "desconhecido"
}
