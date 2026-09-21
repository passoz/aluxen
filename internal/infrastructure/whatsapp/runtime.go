package whatsapp

import (
	"context"
	"log/slog"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Runtime é o canal de WhatsApp desta instância: o cliente do WAHA, o
// pareamento em curso e a fila de envio. O pareamento e os jobs vivem em
// memória, então o canal é um objeto único do processo — como no app atual, que
// tinha um módulo para cada um.
type Runtime struct {
	Client *Client
	Login  *LoginManager
	// *JobStore é embutido de propósito: as operações da fila (Create, Stage,
	// Complete, Fail, Get) são promovidas para o canal, que é o que o caso de uso
	// consome — sem métodos de repasse.
	*JobStore
}

// NewRuntime monta o canal sobre a configuração do WAHA.
func NewRuntime(cfg ClientConfig, clock domain.Clock, ids domain.IDGenerator, log *slog.Logger, login LoginConfig) *Runtime {
	client := NewClient(cfg)
	return &Runtime{
		Client:   client,
		Login:    NewLoginManager(client, log, login),
		JobStore: NewJobStore(clock, ids),
	}
}

// SendText implementa application.WhatsAppChannel: o telefone vira o
// identificador de conversa do WAHA (`<dígitos>@c.us`).
func (r *Runtime) SendText(ctx context.Context, phone, text string) (string, error) {
	return r.Client.SendText(ctx, ChatID(phone), text)
}

// As portas usadas pelos casos de uso são satisfeitas pelo canal.
var (
	_ application.WhatsAppChannel = (*Runtime)(nil)
	_ SessionClient               = (*Client)(nil)
	_ Availability                = (*Client)(nil)
)
