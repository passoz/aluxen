package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Confirmação de quitação no WhatsApp.
//
// A baixa e a resposta são independentes de propósito: a baixa já está gravada
// quando a confirmação entra na fila, e uma falha de envio não desfaz o
// pagamento — o locatário pode receber a confirmação depois, e o locador vê o
// trabalho na fila.

// paymentConfirmationKey é a chave do payload do trabalho de confirmação.
type paymentConfirmation struct {
	ReceiptID string `json:"receiptId"`
	TenantID  string `json:"tenantId"`
	Phone     string `json:"phone"`
	Message   string `json:"message"`
}

// EnqueuePaymentConfirmation enfileira a mensagem que confirma a quitação.
// Sem telefone do titular não há para quem enviar: a fila não recebe nada.
func EnqueuePaymentConfirmation(ctx context.Context, deps Deps, proof *domain.PaymentProof, receipt *domain.Receipt, tenant *domain.Tenant) error {
	if tenant == nil {
		return nil
	}
	phone := format.NormalizeDigits(domain.Str(tenant.Phone))
	if phone == "" {
		deps.Log.Info("confirmação de quitação sem telefone do titular",
			"scope", "payments", "receipt", receipt.ID, "tenant", tenant.ID)
		return nil
	}
	if deps.Messages == nil {
		return nil
	}

	// A mensagem é a mesma que o locador enviaria à mão: recibo quitado, com o
	// valor pago e a data da baixa já formatados.
	message := deps.Messages.Receipt(WhatsAppReceiptMessage{
		TenantName:    tenant.Name,
		PropertyName:  receipt.PropertyName,
		DueDate:       format.FormatDateBR(receipt.DueDate),
		Amount:        format.FormatCurrency(receipt.Amount),
		PaidAmount:    format.FormatCurrency(domain.Float(receipt.PaidAmount)),
		PaidAmountRaw: receipt.PaidAmount,
		AmountRaw:     receipt.Amount,
		Category:      string(receipt.Category),
		Description:   domain.Str(receipt.Description),
		PaidAt:        format.FormatDateTimeBR(paymentProofPaidAt(domain.Str(proof.ExtractedDate), deps.Clock.Now())),
	})

	payload, err := json.Marshal(paymentConfirmation{
		ReceiptID: receipt.ID,
		TenantID:  tenant.ID,
		Phone:     phone,
		Message:   message,
	})
	if err != nil {
		return fmt.Errorf("montar a confirmação de quitação: %w", err)
	}

	if _, err := Enqueue(ctx, deps, domain.JobKindWhatsAppSend, json.RawMessage(payload), 0); err != nil {
		return fmt.Errorf("enfileirar a confirmação de quitação: %w", err)
	}
	return nil
}

// ProcessWhatsAppSendJob entrega uma mensagem enfileirada (confirmação de
// quitação ou qualquer aviso do produto) pelo canal.
func ProcessWhatsAppSendJob(ctx context.Context, deps Deps, job domain.Job) error {
	if deps.Channel == nil {
		return errors.New("canal de WhatsApp não configurado")
	}
	if job.Payload == nil || *job.Payload == "" {
		return domain.Invalid("Mensagem sem destinatário.")
	}

	var payload paymentConfirmation
	if err := json.Unmarshal([]byte(*job.Payload), &payload); err != nil {
		return fmt.Errorf("ler o payload da mensagem: %w", err)
	}
	if payload.Phone == "" || payload.Message == "" {
		return domain.Invalid("Mensagem sem destinatário ou sem texto.")
	}

	messageID, err := deps.Channel.SendText(ctx, payload.Phone, payload.Message)
	if err != nil {
		return fmt.Errorf("enviar a mensagem pelo canal: %w", err)
	}

	if err := Record(ctx, deps, AuditContext{Origin: domain.AuditOriginJob}, AuditEntry{
		Action:      domain.ActionWhatsAppSend,
		Entity:      domain.EntityWhatsApp,
		EntityID:    payload.ReceiptID,
		EntityLabel: payload.TenantID,
		Summary:     "Confirmação de quitação enviada pelo WhatsApp",
		Metadata:    map[string]any{"phone": maskedPhone(payload.Phone), "messageId": messageID},
	}); err != nil {
		return err
	}
	return nil
}
