package web

import (
	"net/http"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Cadastros: locatários e imóveis. O handler é fino — lê o formulário, chama o
// caso de uso em internal/application e devolve o erro de domínio (nil no
// sucesso); a resposta HTTP fica com o despachante do painel.
//
// Intents atendidos: tenant.create, tenant.update, tenant.delete,
// property.create, property.update, property.delete.

// catalogIntent executa o intent de cadastro e devolve o erro de domínio
// correspondente (nil em caso de sucesso).
func (h *Handlers) catalogIntent(r *http.Request, user *domain.User, intent string) error {
	actx := h.auditContext(r, domain.AuditOriginWeb)
	if user != nil {
		actx.ActorID = &user.ID
		actx.ActorName = &user.Username
	}

	switch intent {
	case "tenant.create":
		_, err := application.TenantCreate(r.Context(), h.Deps, actx, tenantInput(r))
		return err
	case "tenant.update":
		_, err := application.TenantUpdate(r.Context(), h.Deps, actx, tenantInput(r))
		return err
	case "tenant.delete":
		return application.TenantDelete(r.Context(), h.Deps, actx, r.FormValue("id"))
	case "property.create":
		_, err := application.PropertyCreate(r.Context(), h.Deps, actx, propertyInput(r))
		return err
	case "property.update":
		_, err := application.PropertyUpdate(r.Context(), h.Deps, actx, propertyInput(r))
		return err
	case "property.delete":
		return application.PropertyDelete(r.Context(), h.Deps, actx, r.FormValue("id"))
	default:
		return domain.Invalid("Ação inválida.")
	}
}

// tenantInput lê os campos do locatário do formulário do painel.
func tenantInput(r *http.Request) application.TenantInput {
	return application.TenantInput{
		ID:    r.FormValue("id"),
		Name:  r.FormValue("name"),
		Email: r.FormValue("email"),
		Phone: r.FormValue("phone"),
		CPF:   r.FormValue("cpf"),
	}
}

// propertyInput lê os campos do imóvel do formulário do painel.
func propertyInput(r *http.Request) application.PropertyInput {
	return application.PropertyInput{
		ID:      r.FormValue("id"),
		Name:    r.FormValue("name"),
		Address: r.FormValue("address"),
		City:    r.FormValue("city"),
		State:   r.FormValue("state"),
		Zip:     r.FormValue("zip"),
	}
}

// ── Apoio das abas de cadastro ─────────────────────────────────────
//
// As tabelas de inquilinos e imóveis mostram quantos contratos cada registro
// tem; o painel já carrega os contratos no snapshot, então a contagem é feita
// aqui, sem consulta nova.

// Data devolve o próprio contexto da aba. As abas são executadas com o
// `*tabData` na raiz, mas os templates de cadastro leem o snapshot por
// `$.Data.Snapshot` — a forma usada na casca do painel (`dashboard.html`, onde
// `.Data` é o `PageData.Data`). O método sem argumento faz as duas formas
// resolverem o mesmo contexto, sem depender de como a aba é renderizada.
func (d *tabData) Data() *tabData { return d }

func (d *tabData) snapshotLeases() []domain.Lease {
	if d == nil || d.Snapshot == nil {
		return nil
	}
	return d.Snapshot.Leases
}

// TenantLeaseCount devolve quantos contratos o locatário possui.
func (d *tabData) TenantLeaseCount(tenantID string) int {
	count := 0
	for _, lease := range d.snapshotLeases() {
		if lease.TenantID == tenantID {
			count++
		}
	}
	return count
}

// TenantHasActiveLease informa se o locatário tem contrato ativo — o que
// impede a exclusão do cadastro.
func (d *tabData) TenantHasActiveLease(tenantID string) bool {
	for _, lease := range d.snapshotLeases() {
		if lease.TenantID == tenantID && lease.Status == domain.LeaseStatusActive {
			return true
		}
	}
	return false
}

// PropertyLeaseCount devolve quantos contratos o imóvel possui.
func (d *tabData) PropertyLeaseCount(propertyID string) int {
	count := 0
	for _, lease := range d.snapshotLeases() {
		if lease.PropertyID == propertyID {
			count++
		}
	}
	return count
}
