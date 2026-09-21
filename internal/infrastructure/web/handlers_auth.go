package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Autenticação do administrador e informações do sistema.

// Root redireciona para o painel ou para o login.
func (h *Handlers) Root(w http.ResponseWriter, r *http.Request) {
	if h.Session.AdminID(r) != "" {
		http.Redirect(w, r, "/dashboard?tab=dashboard", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}

// LoginPage renderiza o formulário de acesso.
func (h *Handlers) LoginPage(w http.ResponseWriter, r *http.Request) {
	if h.Session.AdminID(r) != "" {
		http.Redirect(w, r, "/dashboard?tab=dashboard", http.StatusFound)
		return
	}
	h.renderLogin(w, r, http.StatusOK, "")
}

// Login autentica o administrador aplicando o gate de tentativas.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderLogin(w, r, http.StatusBadRequest, "Não foi possível ler os dados enviados.")
		return
	}
	ip := clientIP(r)

	if decision := h.LoginGate.Check(ip); !decision.Allowed {
		w.Header().Set("Retry-After", strconv.Itoa(decision.RetryAfterSeconds))
		h.auditAuth(r, domain.ActionLoginFailed, nil, "", map[string]any{"reason": decision.Reason, "username": r.FormValue("username")})
		h.renderLogin(w, r, http.StatusTooManyRequests, decision.Message())
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || password == "" {
		h.renderLogin(w, r, http.StatusBadRequest, "Informe usuário e senha.")
		return
	}

	user, err := application.AdminLogin(r.Context(), h.Deps, username, password)
	if err != nil {
		h.LoginGate.RegisterFailure(ip)
		h.auditAuth(r, domain.ActionLoginFailed, nil, username, nil)
		h.renderLogin(w, r, http.StatusUnauthorized, "Usuário ou senha inválidos.")
		return
	}

	h.LoginGate.RegisterSuccess(ip)
	h.Session.SetAdmin(w, user.ID)
	h.auditAuth(r, domain.ActionLogin, &user.ID, user.Username, nil)

	if user.MustChangePassword() {
		http.Redirect(w, r, "/settings?forced=1", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/dashboard?tab=dashboard", http.StatusFound)
}

// Logout encerra a sessão do administrador.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	if userID := h.Session.AdminID(r); userID != "" {
		h.auditAuth(r, domain.ActionLogout, &userID, "", nil)
	}
	h.Session.ClearAdmin(w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// SystemInfo devolve o payload de identificação da instância.
func (h *Handlers) SystemInfo(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	counts, err := application.SystemCounts(r.Context(), h.Deps)
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	appURL, err := application.SettingValue(r.Context(), h.Deps, domain.SettingAppURL)
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	sizeBytes := int64(0)
	if h.DBSize != nil {
		sizeBytes = h.DBSize()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"app":             "Aluxen",
		"version":         h.Build.Version,
		"packageVersion":  h.Build.PackageVersion,
		"buildNumber":     h.Build.BuildNumber,
		"runtime":         "Go",
		"nodeEnv":         h.Cfg.AppEnv,
		"timezone":        timezoneName(),
		"startedAt":       h.Started.UTC().Format("2006-01-02T15:04:05Z07:00"),
		"uptimeSeconds":   int64(h.Build.Uptime().Seconds()),
		"database":        map[string]any{"dialect": h.Dialect, "path": h.DBPath, "sizeBytes": sizeBytes},
		"counts":          map[string]int{"tenants": counts.Tenants, "properties": counts.Properties, "leases": counts.Leases, "receipts": counts.Receipts},
		"appUrl":          appURL,
		"user":            user.Username,
		"authenticityUrl": application.ResolvePublicBaseURL(r.Context(), h.Deps, r.Host),
	})
}

func (h *Handlers) renderLogin(w http.ResponseWriter, r *http.Request, status int, message string) {
	data := &PageData{
		Title:     "Aluxen | Entrar",
		Page:      "login",
		CSRFToken: h.CSRF.Token(w, r),
		Build:     h.Build,
		AppURL:    h.Cfg.AppURL,
		Data:      map[string]any{"Error": message},
	}
	if err := h.Render.Render(w, status, "login.html", data); err != nil {
		h.Log.Error("falha ao renderizar login", "scope", "http", "err", err)
		http.Error(w, message, status)
	}
}

// auditAuth registra eventos de acesso. Falha de auditoria nunca derruba o login.
func (h *Handlers) auditAuth(r *http.Request, action string, actorID *string, username string, metadata map[string]any) {
	actx := h.auditContext(r, domain.AuditOriginWeb)
	if username != "" {
		actx.ActorName = &username
	}
	if actorID != nil {
		if user, err := application.AdminByID(r.Context(), h.Deps, *actorID); err == nil {
			actx.ActorName = &user.Username
		}
	}
	summary := domain.AuditActionMetaFor(action).Label
	if username != "" {
		summary = summary + " · " + username
	}
	if err := application.Record(r.Context(), h.Deps, actx, application.AuditEntry{
		Action:   action,
		Entity:   domain.EntityUser,
		EntityID: domain.Str(actorID),
		Summary:  summary,
		Metadata: metadata,
	}); err != nil {
		h.Log.Warn("auditoria de acesso não gravada", "scope", "http", "action", action, "err", err)
	}
}

// auditContext monta o contexto de auditoria a partir da requisição.
func (h *Handlers) auditContext(r *http.Request, origin domain.AuditOrigin) application.AuditContext {
	actx := application.AuditContext{
		Origin:    origin,
		IP:        clientIP(r),
		UserAgent: truncate(r.UserAgent(), 300),
		RequestID: requestIDFrom(r),
	}
	if userID := h.Session.AdminID(r); userID != "" {
		actx.ActorID = &userID
		if user, err := application.AdminByID(r.Context(), h.Deps, userID); err == nil {
			actx.ActorName = &user.Username
		}
	}
	return actx
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func timezoneName() string {
	return time.Local.String()
}
