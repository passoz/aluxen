package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Área do Cliente (portal do locatário), somente leitura, isolada por titular.
//
//	GET  /portal/login      — página de acesso
//	POST /portal/login      — login por CPF; 302 /portal/password ou /portal
//	GET|POST /portal/logout — 302 /portal/login
//	GET  /portal            — contratos, recibos pagos e documentos do titular
//	GET  /portal/password   — troca obrigatória no primeiro acesso
//	POST /portal/password   — 302 /portal?ok=senha
//
// O acesso usa sessão própria (`__aluxen_client_session`) e o mesmo gate de
// tentativas do login do painel; a auditoria sai com origem `client`.

// Títulos das páginas da Área do Cliente (mesmos do app atual).
const (
	portalLoginTitle    = "Aluxen | Área do Cliente"
	portalHomeTitle     = "Aluxen | Área do Cliente"
	portalPasswordTitle = "Aluxen | Trocar senha"
)

// portalPageData é o contexto dos templates da Área do Cliente.
type portalPageData struct {
	// Error é a mensagem de recusa exibida no formulário.
	Error string
	// CPF devolve o CPF digitado quando o login falha, como no app atual.
	CPF string
	// Tenant é o locatário autenticado (página do portal).
	Tenant *domain.Tenant
	// Area são os contratos, recibos quitados e documentos do titular.
	Area *application.ClientArea
	// Message é a confirmação de sucesso (`?ok=senha`).
	Message string
	// FirstAccess marca a tela de senha do primeiro acesso (senha = CPF).
	FirstAccess bool
}

// PortalLoginPage renderiza o formulário de acesso da Área do Cliente.
func (h *Handlers) PortalLoginPage(w http.ResponseWriter, r *http.Request) {
	if h.Session.ClientID(r) != "" {
		http.Redirect(w, r, "/portal", http.StatusFound)
		return
	}
	h.renderPortalLogin(w, r, http.StatusOK, "", "")
}

// PortalLogin autentica o locatário aplicando o gate de tentativas.
func (h *Handlers) PortalLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderPortalLogin(w, r, http.StatusBadRequest, "Não foi possível ler os dados enviados.", "")
		return
	}
	ip := clientIP(r)
	cpf := strings.TrimSpace(r.FormValue("cpf"))

	if decision := h.LoginGate.Check(ip); !decision.Allowed {
		reason := clientGateReasonLabel(decision.Reason)
		w.Header().Set("Retry-After", strconv.Itoa(decision.RetryAfterSeconds))
		h.auditClientEvent(r, domain.ActionClientLoginFailed, nil, cpf,
			"Área do Cliente: "+strings.ToLower(reason),
			map[string]any{"reason": reason, "cpf": cpf, "retryAfterSeconds": decision.RetryAfterSeconds},
		)
		h.renderPortalLogin(w, r, http.StatusTooManyRequests, decision.Message(), cpf)
		return
	}

	password := r.FormValue("password")
	if cpf == "" || password == "" {
		h.LoginGate.RegisterFailure(ip)
		h.renderPortalLogin(w, r, http.StatusBadRequest, "Informe CPF e senha.", cpf)
		return
	}

	tenant, err := application.ClientLogin(r.Context(), h.Deps, cpf, password)
	if err != nil {
		h.LoginGate.RegisterFailure(ip)
		h.auditClientEvent(r, domain.ActionClientLoginFailed, nil, cpf,
			"Tentativa de acesso negada na Área do Cliente para o CPF informado",
			map[string]any{"reason": "Credenciais inválidas"},
		)
		h.renderPortalLogin(w, r, http.StatusUnauthorized, "CPF ou senha inválidos.", cpf)
		return
	}

	h.LoginGate.RegisterSuccess(ip)
	h.Session.SetClient(w, tenant.ID)
	h.auditClientEvent(r, domain.ActionClientLogin, tenant, tenant.Name,
		"Acesso à Área do Cliente por \""+tenant.Name+"\"", nil)

	// Senha padrão (CPF) ainda vigente: a troca vem antes de qualquer leitura.
	if tenant.MustChangePassword() {
		http.Redirect(w, r, "/portal/password", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/portal", http.StatusFound)
}

// PortalLogout encerra a sessão do locatário.
func (h *Handlers) PortalLogout(w http.ResponseWriter, r *http.Request) {
	tenant, _ := h.clientTenant(r)
	h.Session.ClearClient(w)

	label := ""
	summary := "Saída da Área do Cliente"
	if tenant != nil {
		label = tenant.Name
		summary = "Saída da Área do Cliente de \"" + tenant.Name + "\""
	}
	h.auditClientEvent(r, domain.ActionClientLogout, tenant, label, summary, nil)
	http.Redirect(w, r, "/portal/login", http.StatusFound)
}

// PortalHome mostra os contratos, recibos quitados e documentos do titular.
func (h *Handlers) PortalHome(w http.ResponseWriter, r *http.Request) {
	tenant, ok := h.requireClient(w, r)
	if !ok {
		return
	}
	if tenant.MustChangePassword() {
		http.Redirect(w, r, "/portal/password", http.StatusFound)
		return
	}

	area, err := application.LoadClientArea(r.Context(), h.Deps, tenant.ID)
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	message := ""
	// O app atual redireciona `ok=password` e nunca exibe a mensagem; aqui o
	// aviso de sucesso chega pelo parâmetro que a tela de senha devolve.
	if r.URL.Query().Get("ok") == "senha" {
		message = "Senha alterada com sucesso."
	}
	h.renderPortalHome(w, r, area, message)
}

// PortalPasswordPage renderiza a troca de senha do locatário.
func (h *Handlers) PortalPasswordPage(w http.ResponseWriter, r *http.Request) {
	tenant, ok := h.requireClient(w, r)
	if !ok {
		return
	}
	h.renderPortalPassword(w, r, http.StatusOK, tenant.MustChangePassword(), "")
}

// PortalPassword troca a senha do locatário e devolve a Área do Cliente com a
// confirmação de sucesso.
func (h *Handlers) PortalPassword(w http.ResponseWriter, r *http.Request) {
	tenant, ok := h.requireClient(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderPortalPassword(w, r, http.StatusBadRequest, tenant.MustChangePassword(), "Não foi possível ler os dados enviados.")
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmation := r.FormValue("confirm_password")

	if newPassword != confirmation {
		h.renderPortalPassword(w, r, http.StatusBadRequest, tenant.MustChangePassword(), "A confirmação não corresponde à nova senha.")
		return
	}

	if err := application.ClientChangePassword(r.Context(), h.Deps, tenant.ID, currentPassword, newPassword, confirmation); err != nil {
		if domain.IsValidation(err) {
			h.renderPortalPassword(w, r, http.StatusBadRequest, tenant.MustChangePassword(), domain.ValidationMessage(err))
			return
		}
		h.mapError(w, r, err)
		return
	}

	h.auditClientEvent(r, domain.ActionClientPassword, tenant, tenant.Name,
		"Senha da Área do Cliente alterada por \""+tenant.Name+"\"", nil)
	http.Redirect(w, r, "/portal?ok=senha", http.StatusFound)
}

// requireClient devolve o locatário autenticado ou escreve a recusa da Área do
// Cliente (302 para o acesso) e devolve false.
func (h *Handlers) requireClient(w http.ResponseWriter, r *http.Request) (*domain.Tenant, bool) {
	tenant, err := h.clientTenant(r)
	if err != nil {
		h.Log.Error("falha ao carregar o locatário da sessão", "scope", "http", "err", err)
	}
	if tenant == nil {
		h.Session.ClearClient(w)
		http.Redirect(w, r, "/portal/login", http.StatusFound)
		return nil, false
	}
	return tenant, true
}

// clientTenant resolve o locatário da sessão da Área do Cliente. Sessão ausente
// ou cadastro removido devolve nil (a sessão é encerrada por quem chama).
func (h *Handlers) clientTenant(r *http.Request) (*domain.Tenant, error) {
	tenantID := h.Session.ClientID(r)
	if tenantID == "" {
		return nil, nil
	}
	tenant, err := application.ClientByID(r.Context(), h.Deps, tenantID)
	if err != nil {
		if errors.Is(err, domain.ErrUnauthorized) {
			return nil, nil
		}
		return nil, err
	}
	return tenant, nil
}

// auditClientEvent registra um evento da Área do Cliente. O titular identificado
// é o ator; recusa de acesso não tem ator, só o CPF informado no rótulo.
func (h *Handlers) auditClientEvent(r *http.Request, action string, tenant *domain.Tenant, entityLabel, summary string, metadata map[string]any) {
	actx := application.AuditContext{
		Origin:    domain.AuditOriginClient,
		IP:        clientIP(r),
		UserAgent: truncate(r.UserAgent(), 300),
		RequestID: requestIDFrom(r),
	}
	entityID := ""
	if tenant != nil {
		entityID = tenant.ID
		actx.ActorID = &tenant.ID
		actx.ActorName = &tenant.Name
	}
	if err := application.Record(r.Context(), h.Deps, actx, application.AuditEntry{
		Action:      action,
		Entity:      domain.EntityTenant,
		EntityID:    entityID,
		EntityLabel: entityLabel,
		Summary:     summary,
		Metadata:    metadata,
	}); err != nil {
		h.Log.Warn("auditoria da Área do Cliente não gravada", "scope", "http", "action", action, "err", err)
	}
}

// clientGateReasonLabel descreve o motivo do gate de tentativas, como no app
// atual — o texto aparece na trilha de auditoria.
func clientGateReasonLabel(reason string) string {
	switch reason {
	case "locked":
		return "IP bloqueado temporariamente por tentativas repetidas"
	case "ip":
		return "Limite de tentativas por minuto excedido"
	case "global":
		return "Limite global de tentativas por minuto excedido"
	default:
		return "Tentativa recusada pelo limitador de acesso"
	}
}

func (h *Handlers) renderPortalLogin(w http.ResponseWriter, r *http.Request, status int, message, cpf string) {
	h.renderPortal(w, r, status, "portal_login", portalLoginTitle, portalPageData{Error: message, CPF: cpf}, message)
}

func (h *Handlers) renderPortalHome(w http.ResponseWriter, r *http.Request, area *application.ClientArea, message string) {
	h.renderPortal(w, r, http.StatusOK, "portal", portalHomeTitle, portalPageData{
		Tenant:  area.Tenant,
		Area:    area,
		Message: message,
	}, "Erro ao carregar a Área do Cliente.")
}

func (h *Handlers) renderPortalPassword(w http.ResponseWriter, r *http.Request, status int, firstAccess bool, message string) {
	h.renderPortal(w, r, status, "portal_password", portalPasswordTitle, portalPageData{
		Error:       message,
		FirstAccess: firstAccess,
	}, message)
}

// renderPortal renderiza uma página da Área do Cliente. A falha de renderização
// devolve o texto simples da mensagem: a página não tem como se recuperar dela.
func (h *Handlers) renderPortal(w http.ResponseWriter, r *http.Request, status int, name, title string, data portalPageData, fallback string) {
	pageData := &PageData{
		Title:     title,
		Page:      name,
		CSRFToken: h.CSRF.Token(w, r),
		Build:     h.Build,
		AppURL:    h.Cfg.AppURL,
		Data:      data,
	}
	if err := h.Render.Render(w, status, name+".html", pageData); err != nil {
		h.Log.Error("falha ao renderizar a Área do Cliente", "scope", "http", "page", name, "err", err)
		http.Error(w, fallback, status)
	}
}
