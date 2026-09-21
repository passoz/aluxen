package web

import (
	"net/http"

	"github.com/passoz/aluxen/internal/domain"
)

// Lançamentos financeiros. Este arquivo é do dono da área "recibos" — substitua
// os stubs pela implementação real (casos de uso em
// internal/application/receipts.go + template tab_receipts.html) mantendo a
// assinatura.
//
// Intents atendidos: receipt.create, receipt.update, receipt.pay,
// receipt.delete, receipt.batch-pay, receipt.batch-delete,
// receipt.generate-monthly.

// receiptIntent executa o intent de lançamento e devolve o erro de domínio
// correspondente (nil em caso de sucesso).
func (h *Handlers) receiptIntent(r *http.Request, user *domain.User, intent string) error {
	_ = r
	_ = user
	return domain.Invalid("Ação inválida.")
}
