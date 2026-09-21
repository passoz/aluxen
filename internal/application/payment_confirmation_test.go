package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
)

// channelFake registra as mensagens entregues pelo canal.
type channelFake struct {
	sent []string
	to   []string
	fail error
}

func (c *channelFake) SendText(_ context.Context, phone, text string) (string, error) {
	if c.fail != nil {
		return "", c.fail
	}
	c.to = append(c.to, phone)
	c.sent = append(c.sent, text)
	return "msg-1", nil
}

func (c *channelFake) Create(string) WhatsAppJob              { return WhatsAppJob{} }
func (c *channelFake) Stage(string, WhatsAppStage)            {}
func (c *channelFake) Complete(string, WhatsAppSendResult)    {}
func (c *channelFake) Fail(string, string)                    {}
func (c *channelFake) Get(string, string) (WhatsAppJob, bool) { return WhatsAppJob{}, false }

func TestPaymentConfirmationIsQueuedAndDelivered(t *testing.T) {
	env := newCovEnv(t)
	channel := &channelFake{}
	env.deps.Channel = channel
	env.deps.Messages = covMessages{}

	tenant := &domain.Tenant{ID: "tenant-1", Name: "Carlos Drummond", Phone: domain.StrPtr("(22) 99888-1122")}
	receipt := &domain.Receipt{
		ID: "receipt-1", TenantID: domain.StrPtr("tenant-1"), PropertyName: "Apto 101",
		DueDate: "2026-09-10", Amount: 2500, PaidAmount: domain.FloatPtr(2500),
	}
	proof := &domain.PaymentProof{ID: "proof-1", ExtractedDate: domain.StrPtr("2026-09-09")}

	if err := EnqueuePaymentConfirmation(context.Background(), env.deps, proof, receipt, tenant); err != nil {
		t.Fatalf("enfileirar confirmação: %v", err)
	}

	queued, err := env.jobs.ListByStatus(context.Background(), domain.JobStatusQueued, 10)
	if err != nil {
		t.Fatalf("listar fila: %v", err)
	}
	if len(queued) != 1 || queued[0].Kind != domain.JobKindWhatsAppSend {
		t.Fatalf("fila inesperada: %+v", queued)
	}

	if err := ProcessWhatsAppSendJob(context.Background(), env.deps, queued[0]); err != nil {
		t.Fatalf("entregar confirmação: %v", err)
	}
	if len(channel.sent) != 1 {
		t.Fatalf("mensagens entregues = %d, esperada 1", len(channel.sent))
	}
	if channel.to[0] != "22998881122" {
		t.Fatalf("destino = %q, esperado só dígitos", channel.to[0])
	}
	if !strings.Contains(channel.sent[0], "Carlos Drummond") {
		t.Fatalf("mensagem sem o titular: %q", channel.sent[0])
	}
}

func TestPaymentConfirmationSkipsWithoutPhone(t *testing.T) {
	env := newCovEnv(t)
	env.deps.Channel = &channelFake{}
	env.deps.Messages = covMessages{}

	tenant := &domain.Tenant{ID: "tenant-1", Name: "Sem Telefone"}
	receipt := &domain.Receipt{ID: "receipt-1", Amount: 100, DueDate: "2026-09-10"}

	if err := EnqueuePaymentConfirmation(context.Background(), env.deps, &domain.PaymentProof{ID: "p"}, receipt, tenant); err != nil {
		t.Fatalf("sem telefone não é erro: %v", err)
	}
	queued, err := env.jobs.ListByStatus(context.Background(), domain.JobStatusQueued, 10)
	if err != nil {
		t.Fatalf("listar fila: %v", err)
	}
	if len(queued) != 0 {
		t.Fatalf("nada deveria ser enfileirado sem telefone: %+v", queued)
	}
}

func TestProcessSendJobRejectsBrokenPayloadAndChannelFailure(t *testing.T) {
	env := newCovEnv(t)

	// Sem canal configurado o trabalho falha com motivo claro (o worker decide a
	// retentativa).
	if err := ProcessWhatsAppSendJob(context.Background(), env.deps, domain.Job{ID: "j1"}); err == nil {
		t.Fatal("trabalho sem canal deveria falhar")
	}

	env.deps.Channel = &channelFake{}
	payload := "{isso não é json"
	if err := ProcessWhatsAppSendJob(context.Background(), env.deps, domain.Job{ID: "j2", Payload: &payload}); err == nil {
		t.Fatal("payload inválido deveria falhar")
	}

	broken := `{"phone":"","message":""}`
	if err := ProcessWhatsAppSendJob(context.Background(), env.deps, domain.Job{ID: "j3", Payload: &broken}); err == nil {
		t.Fatal("mensagem sem destino deveria falhar")
	}

	valid, err := json.Marshal(paymentConfirmation{Phone: "22998881122", Message: "oi"})
	if err != nil {
		t.Fatalf("montar payload: %v", err)
	}
	text := string(valid)
	env.deps.Channel = &channelFake{fail: errors.New("WAHA fora do ar")}
	if err := ProcessWhatsAppSendJob(context.Background(), env.deps, domain.Job{ID: "j4", Payload: &text}); err == nil {
		t.Fatal("falha do canal deveria virar erro do trabalho")
	}
}
