package web

import (
	"context"
	"net/http"
)

// Conversas do WhatsApp e régua de cobrança (Work 0004).
//
//	GET  /api/conversations            — conversas por inquilino (JSON)
//	POST /dashboard/conversation       — resposta manual e escalonamento
//	POST /api/collection/run           — avança a régua dos lançamentos vencidos
//
// Este arquivo pertence ao dono da área de mensageria; substitua os stubs pela
// implementação real mantendo as assinaturas e implemente
// `conversationsTabData`, que o painel usa para renderizar a aba `conversations`.

func (h *Handlers) ConversationList(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "conversations.list")
}

func (h *Handlers) ConversationMutation(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "conversations.mutation")
}

func (h *Handlers) CollectionRun(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "collection.run")
}

// conversationsTabData monta o contexto da aba de conversas.
func (h *Handlers) conversationsTabData(context.Context) (any, error) {
	return nil, nil
}
