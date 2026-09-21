package web

import (
	"net/http"

	"github.com/passoz/aluxen/internal/domain"
)

// Contratos. Este arquivo é do dono da área "contratos" — substitua os stubs
// pela implementação real (casos de uso em internal/application/leases.go +
// template tab_leases.html) mantendo a assinatura.
//
// Intents atendidos: lease.create, lease.update, lease.terminate,
// lease.suspend, lease.reactivate, lease.delete.

// leaseIntent executa o intent de contrato e devolve o erro de domínio
// correspondente (nil em caso de sucesso).
func (h *Handlers) leaseIntent(r *http.Request, user *domain.User, intent string) error {
	_ = r
	_ = user
	return domain.Invalid("Ação inválida.")
}
