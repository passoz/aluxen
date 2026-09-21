package web

import (
	"net/http"

	"github.com/passoz/aluxen/internal/domain"
)

// Cadastros: locatários e imóveis. Este arquivo é do dono da área "cadastros" —
// substitua os stubs pela implementação real (casos de uso em
// internal/application/tenants.go e properties.go + template tab_tenants.html e
// tab_properties.html) mantendo as assinaturas.
//
// Intents atendidos: tenant.create, tenant.update, tenant.delete,
// property.create, property.update, property.delete.

// catalogIntent executa o intent de cadastro e devolve o erro de domínio
// correspondente (nil em caso de sucesso).
func (h *Handlers) catalogIntent(r *http.Request, user *domain.User, intent string) error {
	_ = r
	_ = user
	return domain.Invalid("Ação inválida.")
}
