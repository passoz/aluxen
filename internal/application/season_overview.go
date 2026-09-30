package application

import (
	"context"
	"fmt"
	"time"

	"github.com/passoz/aluxen/internal/domain"
)

// Painel de temporada: junta, por contrato, a caução e as vistorias. É leitura
// pura sobre o que já existe — serve à aba que o administrador de temporada usa
// como bancada de trabalho.

// SeasonLeaseRow é uma linha da bancada de temporada.
type SeasonLeaseRow struct {
	Lease       *domain.Lease
	Deposit     *DepositView
	Inspections []domain.Inspection
	CheckIn     *domain.Inspection
	CheckOut    *domain.Inspection
	IsSeason    bool
}

// SeasonOverview é o conteúdo da aba de temporada.
type SeasonOverview struct {
	Rows             []SeasonLeaseRow
	SeasonCount      int
	WithDeposit      int
	DepositsHeld     float64
	DepositsReturned float64
	PendingCheckOut  int
}

// BuildSeasonOverview monta a bancada de temporada.
func BuildSeasonOverview(ctx context.Context, deps Deps) (*SeasonOverview, error) {
	leases, err := ListLeases(ctx, deps)
	if err != nil {
		return nil, err
	}
	inspections, err := ListInspections(ctx, deps)
	if err != nil {
		return nil, err
	}

	byLease := map[string][]domain.Inspection{}
	for _, inspection := range inspections {
		byLease[inspection.LeaseID] = append(byLease[inspection.LeaseID], inspection)
	}

	today := deps.Clock.Now().Format("2006-01-02")
	overview := &SeasonOverview{Rows: make([]SeasonLeaseRow, 0, len(leases))}
	for index := range leases {
		lease := leases[index]
		if lease.Status == domain.LeaseStatusTerminated {
			continue
		}
		row := SeasonLeaseRow{
			Lease:       &lease,
			Deposit:     DepositViewOf(&lease),
			Inspections: byLease[lease.ID],
			IsSeason:    lease.Status == domain.LeaseStatusSeason,
		}
		for inspectionIndex := range row.Inspections {
			inspection := &row.Inspections[inspectionIndex]
			switch inspection.Kind {
			case domain.InspectionKindCheckIn:
				row.CheckIn = inspection
			case domain.InspectionKindCheckOut:
				row.CheckOut = inspection
			}
		}

		if row.IsSeason {
			overview.SeasonCount++
			if row.CheckOut == nil {
				overview.PendingCheckOut++
			}
		} else if row.CheckOut == nil && leaseEnded(lease.EndDate, today) {
			// Fixo com prazo encerrado e sem vistoria de saída: a saída está
			// pendente como na temporada. Fixo ainda vigente não conta.
			overview.PendingCheckOut++
		}
		if domain.HasDeposit(lease.DepositAmount, lease.DepositStatus) {
			overview.WithDeposit++
			overview.DepositsHeld += row.Deposit.Outstanding
			overview.DepositsReturned += row.Deposit.Refunded
		}
		overview.Rows = append(overview.Rows, row)
	}
	return overview, nil
}

// leaseEnded informa se o prazo do contrato já passou na data informada
// (aaaa-mm-dd). Data fora do formato não conta como encerrada.
func leaseEnded(endDate, today string) bool {
	if len(endDate) < 10 || len(today) < 10 {
		return false
	}
	end := endDate[:10]
	if _, err := time.Parse("2006-01-02", end); err != nil {
		return false
	}
	return end < today[:10]
}

// InspectionForLease devolve a vistoria de um contrato, por tipo.
func InspectionForLease(ctx context.Context, deps Deps, leaseID string, kind domain.InspectionKind) (*domain.Inspection, error) {
	inspections, err := deps.Repos.Inspections.ByLease(ctx, leaseID)
	if err != nil {
		return nil, fmt.Errorf("listar vistorias do contrato: %w", err)
	}
	for index := range inspections {
		if inspections[index].Kind == kind {
			return &inspections[index], nil
		}
	}
	return nil, domain.ErrNotFound
}
