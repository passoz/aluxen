package web

import (
	"net/http"
	"strings"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Configurações da instância: dados do locador, URL pública, WhatsApp e troca
// de senha. A tela é isenta da trava de senha, porque é justamente onde a troca
// obrigatória acontece.

// SettingsPage renderiza a tela de configurações.
func (h *Handlers) SettingsPage(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	h.renderSettings(w, r, user, http.StatusOK, "", false)
}

// SettingsMutation executa os intents da tela de configurações.
func (h *Handlers) SettingsMutation(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderSettings(w, r, user, http.StatusBadRequest, "Não foi possível ler os dados enviados.", false)
		return
	}

	intent := strings.TrimSpace(r.FormValue("intent"))
	actx := h.auditContext(r, domain.AuditOriginWeb)

	switch intent {
	case "save-landlord":
		err := application.SaveLandlordData(r.Context(), h.Deps, actx, application.LandlordInput{
			Name:  r.FormValue("name"),
			CPF:   r.FormValue("cpf"),
			Email: r.FormValue("email"),
		})
		if err != nil {
			h.renderSettingsError(w, r, user, err)
			return
		}
		h.renderSettings(w, r, user, http.StatusOK, "Dados salvos com sucesso.", false)

	case "save-app-url":
		if err := application.SaveAppURL(r.Context(), h.Deps, actx, r.FormValue("app_url")); err != nil {
			h.renderSettingsError(w, r, user, err)
			return
		}
		h.renderSettings(w, r, user, http.StatusOK, "URL pública do sistema salva com sucesso.", false)

	case "change-password":
		mustChange, err := application.AdminMustChangePassword(r.Context(), h.Deps, user.ID)
		if err != nil {
			h.mapError(w, r, err)
			return
		}
		err = application.AdminChangePassword(r.Context(), h.Deps, user.ID,
			r.FormValue("current_password"), r.FormValue("new_password"), r.FormValue("confirm_password"))
		if err != nil {
			h.renderSettingsError(w, r, user, err)
			return
		}
		if err := application.Record(r.Context(), h.Deps, actx, application.AuditEntry{
			Action:      domain.ActionUserPassword,
			Entity:      domain.EntityUser,
			EntityID:    user.ID,
			EntityLabel: user.Username,
			Summary:     "Senha alterada · " + user.Username,
		}); err != nil {
			h.Log.Warn("auditoria da troca de senha não gravada", "scope", "http", "err", err)
		}
		if mustChange {
			http.Redirect(w, r, "/dashboard?tab=dashboard", http.StatusFound)
			return
		}
		h.renderSettings(w, r, user, http.StatusOK, "Senha alterada com sucesso.", false)

	default:
		h.renderSettings(w, r, user, http.StatusBadRequest, "Ação inválida.", false)
	}
}

func (h *Handlers) renderSettingsError(w http.ResponseWriter, r *http.Request, user *domain.User, err error) {
	if domain.IsValidation(err) {
		h.renderSettings(w, r, user, http.StatusBadRequest, domain.ValidationMessage(err), false)
		return
	}
	h.mapError(w, r, err)
}

func (h *Handlers) renderSettings(w http.ResponseWriter, r *http.Request, user *domain.User, status int, message string, forced bool) {
	view, err := application.LoadSettingsView(r.Context(), h.Deps, r.Host)
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	mustChange, err := application.AdminMustChangePassword(r.Context(), h.Deps, user.ID)
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	forced = forced || mustChange || r.URL.Query().Get("forced") == "1"
	view.MustChange = mustChange

	data := &PageData{
		Title:     "Aluxen | Configurações",
		Page:      "settings",
		CSRFToken: h.CSRF.Token(w, r),
		Build:     h.Build,
		AppURL:    h.Cfg.AppURL,
		Data: map[string]any{
			"User":    user,
			"View":    view,
			"Message": message,
			"Forced":  forced,
		},
	}
	if err := h.Render.Render(w, status, "settings.html", data); err != nil {
		h.Log.Error("falha ao renderizar configurações", "scope", "http", "err", err)
		http.Error(w, message, status)
	}
}
