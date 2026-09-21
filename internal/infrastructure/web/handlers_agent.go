package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Conversas do WhatsApp e régua de cobrança (Work 0004).
//
//	GET  /api/conversations       — conversas por inquilino (JSON)
//	POST /dashboard/conversation  — resposta manual (`conversation.reply`) e
//	                               escalonamento (`conversation.escalate`)
//	POST /api/collection/run      — avança a régua dos lançamentos vencidos
//
// O handler é fino: exige a sessão do administrador, chama o caso de uso em
// internal/application e devolve JSON. Nenhuma decisão de cobrança e nenhuma
// consulta de conversa mora aqui.

const (
	// conversationIntentReply entrega a resposta escrita pelo locador.
	conversationIntentReply = "conversation.reply"
	// conversationIntentEscalate registra que o locador assumiu a conversa.
	conversationIntentEscalate = "conversation.escalate"

	// maxCollectionRunBody limita o corpo JSON de /api/collection/run.
	maxCollectionRunBody = 64 << 10
)

// conversationTabContext é o contexto da aba de conversas: as conversas, o que
// a régua cobraria hoje e os estágios como o painel os apresenta.
type conversationTabContext struct {
	Conversations []application.ConversationRow     `json:"conversations"`
	Candidates    []application.CollectionCandidate `json:"candidates"`
	Stages        []domain.CollectionStageOption    `json:"stages"`
}

// conversationsTabData carrega o contexto da aba de conversas (cada aba carrega
// o seu contexto: o painel não paga I/O de tela que ninguém abriu).
func (h *Handlers) conversationsTabData(ctx context.Context) (any, error) {
	conversations, err := application.ListConversations(ctx, h.Deps, 0)
	if err != nil {
		return nil, err
	}
	candidates, err := application.PlanCollection(ctx, h.Deps, h.Deps.Clock.Now())
	if err != nil {
		return nil, err
	}
	return &conversationTabContext{
		Conversations: conversations,
		Candidates:    candidates,
		Stages:        domain.CollectionStageOptions(),
	}, nil
}

// ConversationList devolve as conversas do painel, da mais recente para a mais
// antiga. `limit` é a janela de mensagens lida por conversa.
func (h *Handlers) ConversationList(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			h.mapError(w, r, domain.Invalid("Limite inválido."))
			return
		}
		limit = parsed
	}

	conversations, err := application.ListConversations(r.Context(), h.Deps, limit)
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "conversations": conversations})
}

// ConversationMutation despacha os intents da aba de conversas.
func (h *Handlers) ConversationMutation(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Não foi possível ler os dados enviados."})
		return
	}

	switch strings.TrimSpace(r.FormValue("intent")) {
	case conversationIntentReply:
		h.conversationReply(w, r, user)
	case conversationIntentEscalate:
		h.conversationEscalate(w, r)
	default:
		h.mapError(w, r, domain.Invalid("Ação inválida."))
	}
}

// conversationReply entrega a resposta escrita pelo locador ao titular da
// conversa (o destinatário é o cadastro do titular, nunca o formulário).
func (h *Handlers) conversationReply(w http.ResponseWriter, r *http.Request, user *domain.User) {
	if _, ok := h.whatsAppChannel(w); !ok {
		return
	}

	jobID, err := application.ConversationReply(r.Context(), h.Deps, h.WhatsApp, application.ManualReplyInput{
		UserID:   user.ID,
		TenantID: r.FormValue("tenant_id"),
		Message:  r.FormValue("message"),
		Actor:    h.auditContext(r, domain.AuditOriginWeb),
	})
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "jobId": jobID})
}

// conversationEscalate registra que o locador assumiu a conversa.
func (h *Handlers) conversationEscalate(w http.ResponseWriter, r *http.Request) {
	err := application.EscalateConversation(
		r.Context(),
		h.Deps,
		h.auditContext(r, domain.AuditOriginWeb),
		r.FormValue("tenant_id"),
		r.FormValue("reason"),
	)
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// collectionRunRequest é o pedido de /api/collection/run. O painel manda
// formulário; a API pode mandar JSON.
type collectionRunRequest struct {
	DryRun bool `json:"dry_run"`
}

// CollectionRun avança a régua dos lançamentos vencidos. Com `dry_run`, nada é
// enviado e nada é registrado: a resposta é a simulação do que sairia.
func (h *Handlers) CollectionRun(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	request, err := collectionRunInput(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Não foi possível ler os dados enviados."})
		return
	}

	actx := h.auditContext(r, domain.AuditOriginWeb)
	var outcomes []application.CollectionOutcome
	if h.WhatsApp != nil {
		outcomes, err = application.RunCollectionWithChannel(r.Context(), h.Deps, h.WhatsApp, actx, request.DryRun)
	} else {
		// Instância sem canal montado: o envio fica registrado como pendente.
		outcomes, err = application.RunCollection(r.Context(), h.Deps, actx, request.DryRun)
	}
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	if outcomes == nil {
		outcomes = []application.CollectionOutcome{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"dryRun":   request.DryRun,
		"outcomes": outcomes,
	})
}

// collectionRunInput lê o pedido: JSON quando o cliente manda JSON, senão o
// formulário do painel.
func collectionRunInput(r *http.Request) (collectionRunRequest, error) {
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		var payload collectionRunRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, maxCollectionRunBody)).Decode(&payload); err != nil {
			return collectionRunRequest{}, err
		}
		return payload, nil
	}
	if err := r.ParseForm(); err != nil {
		return collectionRunRequest{}, err
	}
	return collectionRunRequest{DryRun: formDryRun(r.FormValue("dry_run"))}, nil
}

// formDryRun interpreta o campo `dry_run` do formulário da régua; o vocabulário
// de aceite é o mesmo dos outros formulários do painel.
func formDryRun(value string) bool { return formApprove(value) }
