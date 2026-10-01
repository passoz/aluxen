package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Contratos. O handler é fino — lê o formulário, chama o caso de uso em
// internal/application e devolve o erro de domínio (nil no sucesso); a resposta
// HTTP fica com o despachante do painel.
//
// Intents atendidos: lease.create, lease.update, lease.terminate,
// lease.suspend, lease.reactivate, lease.delete.

// leaseIntent executa o intent de contrato e devolve o erro de domínio
// correspondente (nil em caso de sucesso).
func (h *Handlers) leaseIntent(r *http.Request, user *domain.User, intent string) error {
	actx := h.auditContext(r, domain.AuditOriginWeb)
	if user != nil {
		actx.ActorID = &user.ID
		actx.ActorName = &user.Username
	}

	switch intent {
	case "lease.create":
		input, err := leaseInput(r, "")
		if err != nil {
			return err
		}
		_, err = application.LeaseCreate(r.Context(), h.Deps, actx, input)
		return err
	case "lease.update":
		input, err := leaseInput(r, r.FormValue("id"))
		if err != nil {
			return err
		}
		_, err = application.LeaseUpdate(r.Context(), h.Deps, actx, input)
		return err
	case "lease.terminate", "lease.suspend", "lease.reactivate":
		return application.LeaseSetStatus(r.Context(), h.Deps, actx, intent, r.FormValue("id"))
	case "lease.delete":
		return application.LeaseDelete(r.Context(), h.Deps, actx, r.FormValue("id"))
	default:
		return domain.Invalid("Ação inválida.")
	}
}

// leaseInput lê os campos do contrato do formulário do painel. `id` vazio deixa
// o caso de uso gerar o identificador (criação). A caução é opcional: campo em
// branco preserva a caução já registrada; valor inválido recusa o envio.
func leaseInput(r *http.Request, id string) (application.LeaseInput, error) {
	dueDay, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("due_day")))
	input := application.LeaseInput{
		ID:         strings.TrimSpace(id),
		TenantID:   strings.TrimSpace(r.FormValue("tenant_id")),
		PropertyID: strings.TrimSpace(r.FormValue("property_id")),
		StartDate:  strings.TrimSpace(r.FormValue("start_date")),
		EndDate:    strings.TrimSpace(r.FormValue("end_date")),
		DueDay:     dueDay,
		RentAmount: leaseRentAmount(r.FormValue("rent_amount")),
		Status:     domain.LeaseStatus(strings.TrimSpace(r.FormValue("status"))),
	}
	if raw := strings.TrimSpace(r.FormValue("deposit_amount")); raw != "" {
		amount, err := parseMoneyField(raw)
		if err != nil {
			return input, err
		}
		input.DepositAmount = &amount
	}
	input.DepositNotes = r.FormValue("deposit_notes")
	input.InspectionCheckIn = strings.TrimSpace(r.FormValue("inspection_checkin")) != ""
	input.InspectionCondition = domain.InspectionCondition(strings.TrimSpace(r.FormValue("inspection_condition")))
	input.InspectionPerformedAt = strings.TrimSpace(r.FormValue("inspection_performed_at"))
	input.InspectionNotes = r.FormValue("inspection_notes")
	return input, nil
}

// ── Apoio da aba de contratos ──────────────────────────────────────
//
// A tabela de contratos filtra por situação pelo parâmetro `status` da URL (a
// aba já recebe esse valor); o selo de situação é resolvido aqui para o
// template não repetir a regra de cor em cada linha.

// leaseRow é a linha da tabela de contratos com a cor do selo resolvida.
type leaseRow struct {
	domain.Lease
	Tone string
	// CheckInID e CheckOutID são os autos de vistoria do contrato (vazio quando
	// não há): a linha oferece os PDFs e a comparação a partir deles.
	CheckInID  string
	CheckOutID string
}

// LeaseRows devolve os contratos do snapshot filtrados pela situação escolhida
// no filtro da aba (`status` da URL; vazio ou `all` mostra todos).
func (d *tabData) LeaseRows() []leaseRow {
	if d.Snapshot == nil {
		return nil
	}
	rows := make([]leaseRow, 0, len(d.Snapshot.Leases))
	for _, lease := range d.Snapshot.Leases {
		if d.Status != "" && d.Status != "all" && d.Status != string(lease.Status) {
			continue
		}
		row := leaseRow{Lease: lease, Tone: leaseStatusTone(lease.Status)}
		if pair, ok := d.InspectionsByLease[lease.ID]; ok {
			row.CheckInID, row.CheckOutID = pair[0], pair[1]
		}
		rows = append(rows, row)
	}
	return rows
}

// leaseStatusTone mapeia a situação para a cor do selo do design system.
func leaseStatusTone(status domain.LeaseStatus) string {
	switch status {
	case domain.LeaseStatusActive:
		return "paid"
	case domain.LeaseStatusSuspended:
		return "due"
	case domain.LeaseStatusSeason:
		return "partial"
	default:
		return "overdue"
	}
}

// leaseRentAmount aceita tanto o decimal cru (`2500`, `2500.5`) quanto o valor
// já mascarado em pt-BR (`2.500,00`) que o campo de aluguel pode enviar.
func leaseRentAmount(raw string) float64 {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0
	}
	if strings.Contains(value, ",") {
		value = strings.ReplaceAll(value, ".", "")
		value = strings.ReplaceAll(value, ",", ".")
	}
	amount, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return amount
}
