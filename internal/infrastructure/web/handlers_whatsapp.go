package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
	"github.com/passoz/aluxen/internal/infrastructure/whatsapp"
)

// WhatsApp via WAHA (substitui o robô headless com Chromium).
//
//	GET  /api/whatsapp/status            — {isLoggedIn,hasQRCode,chromiumAvailable,profileDir}
//	POST /api/whatsapp/login/start       — inicia sessão/pareamento
//	GET  /api/whatsapp/login/status      — estado do pareamento (qrCode, message, error)
//	POST /api/whatsapp/login/cancel      — cancela o pareamento
//	POST /api/whatsapp/logout            — encerra a sessão
//	POST /api/whatsapp/preview           — monta a mensagem (type=receipt|lease, id)
//	POST /api/whatsapp/send              — cria job assíncrono, devolve {jobId}
//	GET  /api/whatsapp/send/status       — estado do job (jobId)
//	POST /api/whatsapp/webhook           — mensagens recebidas (WAHA); isento de CSRF, valida HMAC
//
// O contrato JSON é o do app atual. O campo `chromiumAvailable` continua com o
// mesmo nome por paridade, mas agora responde "o canal está configurado": é
// `true` quando WAHA_API_URL está definida. `profileDir` fica vazio porque não
// há mais perfil de navegador na instância — a sessão vive no WAHA.

// whatsAppSignatureHeader é o header em que o WAHA entrega o HMAC do webhook.
// A instância do WAHA precisa ser configurada com HMAC SHA-256: é o algoritmo
// que VerifySignature confere (assinatura de outro algoritmo é recusada).
const whatsAppSignatureHeader = "X-Webhook-Hmac"

// maxWhatsAppWebhookBody limita o corpo aceito no webhook (mensagem de WhatsApp
// é pequena; anexo não é lido).
const maxWhatsAppWebhookBody = 1 << 20

// whatsAppChannel devolve o canal montado pelo composition root. Instância sem
// canal (teste, ou WAHA ausente do boot) responde 503: nada de criar estado
// paralelo por requisição.
func (h *Handlers) whatsAppChannel(w http.ResponseWriter) (*whatsapp.Runtime, bool) {
	if h.WhatsApp == nil {
		h.Log.Error("canal do WhatsApp não montado nesta instância", "scope", "whatsapp")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Canal de WhatsApp indisponível."})
		return nil, false
	}
	return h.WhatsApp, true
}

// WhatsAppStatus devolve a situação do canal.
func (h *Handlers) WhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	channel, ok := h.whatsAppChannel(w)
	if !ok {
		return
	}
	state := whatsapp.SessionStateUnknown
	if channel.Client.Configured() {
		current, err := channel.Client.Status(r.Context())
		if err != nil {
			h.Log.Warn("não foi possível consultar a sessão do WhatsApp", "scope", "whatsapp", "err", err)
		} else {
			state = current
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"isLoggedIn":        state == whatsapp.SessionStateWorking,
		"hasQRCode":         state == whatsapp.SessionStateScanQR,
		"chromiumAvailable": channel.Client.Configured(),
		"profileDir":        "",
	})
}

// WhatsAppLoginStart inicia o pareamento e devolve o estado inicial.
func (h *Handlers) WhatsAppLoginStart(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	channel, ok := h.whatsAppChannel(w)
	if !ok {
		return
	}

	state := channel.Login.Start(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": state})
}

// WhatsAppLoginStatus devolve o estado do pareamento em curso.
func (h *Handlers) WhatsAppLoginStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	channel, ok := h.whatsAppChannel(w)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": channel.Login.State()})
}

// WhatsAppLoginCancel cancela o pareamento em curso.
func (h *Handlers) WhatsAppLoginCancel(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	channel, ok := h.whatsAppChannel(w)
	if !ok {
		return
	}

	state := channel.Login.Cancel(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": state})
}

// WhatsAppLogout encerra a sessão pareada e registra o evento na trilha.
func (h *Handlers) WhatsAppLogout(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	channel, ok := h.whatsAppChannel(w)
	if !ok {
		return
	}

	actx := h.auditContext(r, domain.AuditOriginWeb)
	if _, err := channel.Login.Logout(r.Context()); err != nil {
		h.Log.Warn("falha ao desconectar o WhatsApp", "scope", "whatsapp", "err", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Não foi possível desconectar o WhatsApp agora. Tente novamente.",
		})
		return
	}

	if err := application.Record(r.Context(), h.Deps, actx, application.AuditEntry{
		Action:      domain.ActionWhatsAppLogout,
		Entity:      domain.EntityWhatsApp,
		EntityLabel: "Sessão do WhatsApp",
		Summary:     "Sessão do WhatsApp desconectada",
	}); err != nil {
		h.Log.Warn("auditoria do logout do WhatsApp não gravada", "scope", "whatsapp", "err", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "WhatsApp desconectado com sucesso."})
}

// WhatsAppPreview monta a mensagem e devolve o destinatário e o link direto.
func (h *Handlers) WhatsAppPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.mapError(w, r, domain.Invalid("Parâmetros inválidos."))
		return
	}

	preview, err := application.WhatsAppPreview(
		r.Context(),
		h.Deps,
		strings.TrimSpace(r.PostFormValue("type")),
		strings.TrimSpace(r.PostFormValue("id")),
	)
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"type":           preview.Type,
		"id":             preview.ID,
		"phone":          preview.Phone,
		"formattedPhone": preview.FormattedPhone,
		"tenantName":     preview.TenantName,
		"propertyName":   preview.PropertyName,
		"message":        preview.Message,
		"directUrl":      preview.DirectURL,
	})
}

// WhatsAppSend abre o job do envio e devolve o `jobId` para acompanhamento.
func (h *Handlers) WhatsAppSend(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.mapError(w, r, domain.Invalid("Parâmetros inválidos."))
		return
	}

	channel, ok := h.whatsAppChannel(w)
	if !ok {
		return
	}

	jobID, err := application.WhatsAppSendJob(r.Context(), h.Deps, channel, application.WhatsAppSendRequest{
		UserID:  user.ID,
		To:      r.PostFormValue("to"),
		Message: r.PostFormValue("message"),
		Actor:   h.auditContext(r, domain.AuditOriginWeb),
	})
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "jobId": jobID})
}

// WhatsAppSendStatus devolve o andamento do envio (só para quem o criou).
func (h *Handlers) WhatsAppSendStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	channel, ok := h.whatsAppChannel(w)
	if !ok {
		return
	}

	job, err := application.WhatsAppJobStatus(r.Context(), h.Deps, channel, r.URL.Query().Get("jobId"), user.ID)
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "job": job})
}

// WhatsAppWebhook recebe as mensagens do WAHA. Não exige sessão nem CSRF — a
// autenticação é a assinatura HMAC do corpo —, e responde 200 também para os
// eventos que não são mensagem, para o WAHA não reentregá-los.
// deliverAgentReply entrega a resposta do atendimento pelo canal. O webhook não
// espera o envio: a resposta vira job e o WAHA confirma em segundo plano — do
// contrário o provedor lento seguraria a entrega do evento.
const agentReplyTimeout = 30 * time.Second

func (h *Handlers) deliverAgentReply(message application.IncomingMessage, reply *application.AgentReply) {
	if h.WhatsApp == nil {
		h.Log.Warn("resposta do atendimento não enviada: canal indisponível",
			"scope", "whatsapp", "tenant_id", reply.TenantID)
		return
	}

	job := h.WhatsApp.Create("agent")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), agentReplyTimeout)
		defer cancel()

		h.WhatsApp.Stage(job.ID, application.StagePreparing)
		h.WhatsApp.Stage(job.ID, application.StageSending)
		messageID, err := h.WhatsApp.SendText(ctx, message.Phone, reply.Reply)
		if err != nil {
			h.WhatsApp.Fail(job.ID, "falha ao enviar a resposta do atendimento")
			h.Log.Warn("falha ao enviar a resposta do atendimento", "scope", "whatsapp",
				"tenant_id", reply.TenantID, "job_id", job.ID, "err", err)
			return
		}
		h.WhatsApp.Complete(job.ID, application.WhatsAppSendResult{
			Success:   true,
			To:        message.Phone,
			Message:   messageID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
		h.Log.Info("resposta do atendimento enviada", "scope", "whatsapp",
			"tenant_id", reply.TenantID, "job_id", job.ID, "escalated", reply.Escalated)
	}()
}

func (h *Handlers) WhatsAppWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWhatsAppWebhookBody))
	if err != nil {
		h.Log.Warn("webhook do WhatsApp ilegível", "scope", "whatsapp", "err", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Corpo inválido."})
		return
	}

	policy := whatsapp.WebhookPolicy{
		Secret: h.Cfg.WahaWebhookSecret,
		Demo:   strings.EqualFold(strings.TrimSpace(h.Cfg.WhatsAppMode), "demo"),
	}
	if !policy.Allowed(body, r.Header.Get(whatsAppSignatureHeader)) {
		h.Log.Warn("webhook do WhatsApp recusado", "scope", "whatsapp", "reason", "assinatura ausente ou inválida")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Não autorizado."})
		return
	}
	if policy.Open() {
		h.Log.Warn("webhook do WhatsApp aceito sem assinatura", "scope", "whatsapp", "reason", "modo demo sem WAHA_WEBHOOK_SECRET")
	}

	message, err := whatsapp.ParseWebhook(body)
	if err != nil {
		if errors.Is(err, whatsapp.ErrIgnoredEvent) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ignored": true})
			return
		}
		h.Log.Warn("webhook do WhatsApp inválido", "scope", "whatsapp", "err", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Corpo inválido."})
		return
	}

	if err := application.RegisterIncomingMessage(r.Context(), h.Deps, message); err != nil {
		h.Log.Error("falha ao registrar mensagem recebida no WhatsApp", "scope", "whatsapp", "err", err)
		h.mapError(w, r, err)
		return
	}

	// Atendimento: o agente responde com dado do banco ou escala para o locador.
	// A resposta sai pelo canal e o webhook não espera o envio terminar.
	// O atendimento consome a mensagem no formato do domínio; o telefone é o
	// vínculo (o webhook já tentou vincular pelo número ao registrar).
	incoming := domain.WhatsAppMessage{
		ID:         message.ID,
		CreatedAt:  format.FormatLocalDateTimeMillis(time.Now()),
		Direction:  domain.WhatsAppDirectionIn,
		ChatID:     message.ChatID,
		Phone:      message.Phone,
		Body:       message.Body,
		Status:     domain.WhatsAppStatusReceived,
		ExternalID: domain.StrPtr(message.ID),
	}
	reply, err := application.HandleIncomingMessage(r.Context(), h.Deps, application.AuditContext{Origin: domain.AuditOriginPublic}, incoming)
	if err != nil {
		h.Log.Error("falha no atendimento da mensagem recebida", "scope", "whatsapp", "err", err)
	} else if reply != nil && reply.Reply != "" {
		h.deliverAgentReply(message, reply)
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
