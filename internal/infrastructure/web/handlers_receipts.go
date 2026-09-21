package web

import (
	"net/http"
	"strings"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Lançamentos financeiros. O handler é adaptador fino: decodifica o formulário
// e chama o caso de uso de internal/application/receipts.go — nenhuma regra de
// negócio e nenhum SQL aqui.
//
// Intents atendidos: receipt.create, receipt.update, receipt.pay,
// receipt.delete, receipt.batch-pay, receipt.batch-delete,
// receipt.generate-monthly.

// receiptIntent executa o intent de lançamento e devolve o erro de domínio
// correspondente (nil em caso de sucesso).
func (h *Handlers) receiptIntent(r *http.Request, user *domain.User, intent string) error {
	ctx := r.Context()
	// O ator da trilha é resolvido pelo próprio contexto (sessão + IP + agente);
	// `user` já está autenticado pelo despachante do painel.
	actx := h.auditContext(r, domain.AuditOriginWeb)

	switch intent {
	case "receipt.create":
		_, err := application.ReceiptCreate(ctx, h.Deps, actx, receiptInputFrom(r))
		return err
	case "receipt.update":
		_, err := application.ReceiptUpdate(ctx, h.Deps, actx, receiptInputFrom(r))
		return err
	case "receipt.pay":
		_, err := application.ReceiptPay(ctx, h.Deps, actx, application.ReceiptPayInput{
			ID:        r.FormValue("id"),
			PayAmount: r.FormValue("pay_amount"),
			PaidAt:    r.FormValue("paid_at"),
		})
		return err
	case "receipt.delete":
		return application.ReceiptDelete(ctx, h.Deps, actx, r.FormValue("id"))
	case "receipt.batch-pay":
		return application.ReceiptBatchPay(ctx, h.Deps, actx, receiptIDsFromForm(r.FormValue("ids")))
	case "receipt.batch-delete":
		return application.ReceiptBatchDelete(ctx, h.Deps, actx, receiptIDsFromForm(r.FormValue("ids")))
	case "receipt.generate-monthly":
		_, err := application.ReceiptGenerateMonthly(ctx, h.Deps, actx)
		return err
	default:
		return domain.Invalid("Ação inválida.")
	}
}

// receiptInputFrom decodifica o formulário do lançamento. Os valores continuam
// como texto: a interpretação (valor, categoria, derivação do contrato) é do
// caso de uso.
func receiptInputFrom(r *http.Request) application.ReceiptInput {
	return application.ReceiptInput{
		ID:          r.FormValue("id"),
		LeaseID:     r.FormValue("lease_id"),
		TenantID:    r.FormValue("tenant_id"),
		PropertyID:  r.FormValue("property_id"),
		Category:    r.FormValue("category"),
		Description: r.FormValue("description"),
		DueDate:     r.FormValue("due_date"),
		PaidAt:      r.FormValue("paid_at"),
		Amount:      r.FormValue("amount"),
		PaidAmount:  r.FormValue("paid_amount"),
	}
}

// receiptIDsFromForm lê a lista de ids do lote (CSV enviado pelo painel).
func receiptIDsFromForm(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, ",")
}
