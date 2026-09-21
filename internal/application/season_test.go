package application

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/domain"
)

// ── Dublês de temporada ────────────────────────────────────────────

type seasonClock struct{ now time.Time }

func (c *seasonClock) Now() time.Time { return c.now }

type seasonIDs struct{ next int }

func (g *seasonIDs) New() string {
	g.next++
	return "season-id-" + string(rune('0'+g.next))
}

type seasonLeases struct {
	items   []domain.Lease
	updates int
}

func (r *seasonLeases) List(context.Context) ([]domain.Lease, error) { return r.items, nil }

func (r *seasonLeases) ByID(_ context.Context, id string) (*domain.Lease, error) {
	for index := range r.items {
		if r.items[index].ID == id {
			return &r.items[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *seasonLeases) ByTenant(_ context.Context, tenantID string) ([]domain.Lease, error) {
	filtered := make([]domain.Lease, 0, len(r.items))
	for _, lease := range r.items {
		if lease.TenantID == tenantID {
			filtered = append(filtered, lease)
		}
	}
	return filtered, nil
}

func (r *seasonLeases) ByProperty(_ context.Context, propertyID string) ([]domain.Lease, error) {
	filtered := make([]domain.Lease, 0, len(r.items))
	for _, lease := range r.items {
		if lease.PropertyID == propertyID {
			filtered = append(filtered, lease)
		}
	}
	return filtered, nil
}

func (r *seasonLeases) Overlapping(context.Context, string, string, string, string) ([]domain.Lease, error) {
	return nil, nil
}

func (r *seasonLeases) ActiveInRange(context.Context, string, string) ([]domain.Lease, error) {
	return nil, nil
}

func (r *seasonLeases) Create(_ context.Context, lease *domain.Lease) error {
	r.items = append(r.items, *lease)
	return nil
}

func (r *seasonLeases) Update(_ context.Context, lease *domain.Lease) error {
	for index := range r.items {
		if r.items[index].ID == lease.ID {
			r.items[index] = *lease
			r.updates++
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *seasonLeases) UpdateStatus(_ context.Context, id string, status domain.LeaseStatus, updatedAt string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items[index].Status = status
			r.items[index].UpdatedAt = updatedAt
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *seasonLeases) Delete(_ context.Context, id string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *seasonLeases) Count(context.Context) (int, error) { return len(r.items), nil }

func (r *seasonLeases) CountByStatus(_ context.Context, status domain.LeaseStatus) (int, error) {
	total := 0
	for _, lease := range r.items {
		if lease.Status == status {
			total++
		}
	}
	return total, nil
}

type seasonReceipts struct{ items []domain.Receipt }

func (r *seasonReceipts) List(context.Context) ([]domain.Receipt, error) { return r.items, nil }

func (r *seasonReceipts) ByID(_ context.Context, id string) (*domain.Receipt, error) {
	for index := range r.items {
		if r.items[index].ID == id {
			return &r.items[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *seasonReceipts) ByTenant(_ context.Context, tenantID string) ([]domain.Receipt, error) {
	filtered := make([]domain.Receipt, 0, len(r.items))
	for _, receipt := range r.items {
		if receipt.TenantID != nil && *receipt.TenantID == tenantID {
			filtered = append(filtered, receipt)
		}
	}
	return filtered, nil
}

func (r *seasonReceipts) ByLease(_ context.Context, leaseID string) ([]domain.Receipt, error) {
	filtered := make([]domain.Receipt, 0, len(r.items))
	for _, receipt := range r.items {
		if receipt.LeaseID != nil && *receipt.LeaseID == leaseID {
			filtered = append(filtered, receipt)
		}
	}
	return filtered, nil
}

func (r *seasonReceipts) Create(_ context.Context, receipt *domain.Receipt) error {
	r.items = append(r.items, *receipt)
	return nil
}

func (r *seasonReceipts) CreateIfAbsent(_ context.Context, receipt *domain.Receipt) (int64, error) {
	r.items = append(r.items, *receipt)
	return 1, nil
}

func (r *seasonReceipts) Update(_ context.Context, receipt *domain.Receipt) error {
	for index := range r.items {
		if r.items[index].ID == receipt.ID {
			r.items[index] = *receipt
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *seasonReceipts) UpdatePayment(context.Context, string, string, float64, string) error {
	return nil
}

func (r *seasonReceipts) Delete(_ context.Context, id string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *seasonReceipts) Count(context.Context) (int, error) { return len(r.items), nil }

type seasonInspections struct {
	items []domain.Inspection
}

func (r *seasonInspections) List(context.Context) ([]domain.Inspection, error) { return r.items, nil }

func (r *seasonInspections) ByID(_ context.Context, id string) (*domain.Inspection, error) {
	for index := range r.items {
		if r.items[index].ID == id {
			return &r.items[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *seasonInspections) ByLease(_ context.Context, leaseID string) ([]domain.Inspection, error) {
	filtered := make([]domain.Inspection, 0, len(r.items))
	for _, inspection := range r.items {
		if inspection.LeaseID == leaseID {
			filtered = append(filtered, inspection)
		}
	}
	return filtered, nil
}

func (r *seasonInspections) Create(_ context.Context, inspection *domain.Inspection) error {
	r.items = append(r.items, *inspection)
	return nil
}

func (r *seasonInspections) Update(_ context.Context, inspection *domain.Inspection) error {
	for index := range r.items {
		if r.items[index].ID == inspection.ID {
			r.items[index] = *inspection
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *seasonInspections) Delete(_ context.Context, id string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

type seasonProperties struct{ items []domain.Property }

func (r *seasonProperties) List(context.Context) ([]domain.Property, error) { return r.items, nil }

func (r *seasonProperties) ByID(_ context.Context, id string) (*domain.Property, error) {
	for index := range r.items {
		if r.items[index].ID == id {
			return &r.items[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *seasonProperties) Create(_ context.Context, property *domain.Property) error {
	r.items = append(r.items, *property)
	return nil
}

func (r *seasonProperties) Update(_ context.Context, property *domain.Property) error {
	for index := range r.items {
		if r.items[index].ID == property.ID {
			r.items[index] = *property
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *seasonProperties) Delete(_ context.Context, id string) error { return nil }

func (r *seasonProperties) Count(context.Context) (int, error) { return len(r.items), nil }

type seasonAudit struct{ entries []domain.AuditLog }

func (r *seasonAudit) Insert(_ context.Context, entry *domain.AuditLog) error {
	r.entries = append(r.entries, *entry)
	return nil
}

func (r *seasonAudit) List(context.Context, domain.AuditFilter) ([]domain.AuditLog, error) {
	return r.entries, nil
}

func (r *seasonAudit) Count(context.Context, domain.AuditFilter) (int, error) {
	return len(r.entries), nil
}

func (r *seasonAudit) Actors(context.Context) ([]string, error) { return nil, nil }

type seasonAttachments struct {
	saved   map[string][]byte
	deleted []string
}

func newSeasonAttachments() *seasonAttachments {
	return &seasonAttachments{saved: map[string][]byte{}}
}

func (s *seasonAttachments) Save(prefix, filename string, data []byte) (string, error) {
	name := prefix + "/" + filename
	s.saved[name] = data
	return name, nil
}

func (s *seasonAttachments) Open(name string) ([]byte, error) {
	data, ok := s.saved[name]
	if !ok {
		return nil, errors.New("anexo ausente")
	}
	return data, nil
}

func (s *seasonAttachments) Delete(name string) error {
	s.deleted = append(s.deleted, name)
	delete(s.saved, name)
	return nil
}

type seasonFixture struct {
	deps        Deps
	leases      *seasonLeases
	receipts    *seasonReceipts
	inspections *seasonInspections
	properties  *seasonProperties
	audit       *seasonAudit
	attachments *seasonAttachments
	clock       *seasonClock
}

func newSeasonFixture(t *testing.T) *seasonFixture {
	t.Helper()

	clock := &seasonClock{now: time.Date(2026, 9, 21, 15, 30, 0, 0, time.Local)}
	leases := &seasonLeases{items: []domain.Lease{
		{
			ID: "lease-season", TenantID: "tenant-1", PropertyID: "property-1",
			StartDate: "2026-09-01", EndDate: "2026-09-30", DueDay: 5, RentAmount: 2500,
			Status: domain.LeaseStatusSeason, TenantName: "Carlos Drummond", PropertyName: "Apto 101",
		},
		{
			ID: "lease-active", TenantID: "tenant-2", PropertyID: "property-2",
			StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 3200,
			Status: domain.LeaseStatusActive, TenantName: "Clarice Lispector", PropertyName: "Casa das Dunas",
		},
		{
			ID: "lease-terminated", TenantID: "tenant-3", PropertyID: "property-3",
			StartDate: "2025-01-01", EndDate: "2025-12-31", DueDay: 1, RentAmount: 1800,
			Status: domain.LeaseStatusTerminated, TenantName: "Machado de Assis", PropertyName: "Cobertura Caiçara",
		},
	}}
	properties := &seasonProperties{items: []domain.Property{
		{ID: "property-1", Name: "Apto 101", City: "Cabo Frio", State: "RJ"},
		{ID: "property-2", Name: "Casa das Dunas", City: "Cabo Frio", State: "RJ"},
		{ID: "property-3", Name: "Cobertura Caiçara", City: "Búzios", State: "RJ"},
	}}

	receipts := &seasonReceipts{}
	inspections := &seasonInspections{}
	audit := &seasonAudit{}
	attachments := newSeasonAttachments()

	deps := Deps{
		Repos: Repositories{
			Leases:      leases,
			Receipts:    receipts,
			Properties:  properties,
			Inspections: inspections,
			Audit:       audit,
		},
		Clock:       clock,
		IDs:         &seasonIDs{},
		Attachments: attachments,
		Log:         slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})),
	}
	return &seasonFixture{
		deps: deps, leases: leases, receipts: receipts, inspections: inspections,
		properties: properties, audit: audit, attachments: attachments, clock: clock,
	}
}

func (f *seasonFixture) context() (context.Context, AuditContext) {
	actor := "passoz"
	return context.Background(), AuditContext{Origin: domain.AuditOriginWeb, ActorName: &actor}
}

// ── Caução ─────────────────────────────────────────────────────────

func TestSetLeaseDepositHoldsAmount(t *testing.T) {
	fixture := newSeasonFixture(t)
	ctx, actx := fixture.context()

	lease, err := SetLeaseDeposit(ctx, fixture.deps, actx, DepositInput{LeaseID: "lease-season", Amount: 1200, Notes: "caução de temporada"})
	if err != nil {
		t.Fatalf("registrar caução: %v", err)
	}
	if lease.DepositAmount == nil || *lease.DepositAmount != 1200 {
		t.Fatalf("valor da caução = %v", lease.DepositAmount)
	}
	if lease.DepositStatus != domain.DepositStatusHeld {
		t.Fatalf("situação = %q, esperada held", lease.DepositStatus)
	}
	if lease.DepositReceivedAt == nil || *lease.DepositReceivedAt == "" {
		t.Fatal("data de recebimento não registrada")
	}
	if len(fixture.audit.entries) != 1 || fixture.audit.entries[0].Action != domain.ActionLeaseUpdate {
		t.Fatalf("trilha inesperada: %+v", fixture.audit.entries)
	}
}

func TestSetLeaseDepositRejectsInvalidInput(t *testing.T) {
	fixture := newSeasonFixture(t)
	ctx, actx := fixture.context()

	if _, err := SetLeaseDeposit(ctx, fixture.deps, actx, DepositInput{LeaseID: "lease-season"}); !domain.IsValidation(err) {
		t.Fatalf("caução sem valor deveria ser recusada, veio %v", err)
	}
	if _, err := SetLeaseDeposit(ctx, fixture.deps, actx, DepositInput{LeaseID: "inexistente", Amount: 100}); !domain.IsValidation(err) {
		t.Fatalf("contrato inexistente deveria ser recusado, veio %v", err)
	}
}

func TestRefundDepositFullPartialAndForfeited(t *testing.T) {
	cases := []struct {
		name       string
		requested  float64
		wantStatus domain.DepositStatus
		wantAmount float64
		wantLedger bool
	}{
		{"devolução integral", 1200, domain.DepositStatusRefunded, 1200, true},
		{"devolução parcial", 800, domain.DepositStatusPartiallyRefunded, 800, true},
		{"retenção total", 0, domain.DepositStatusForfeited, 0, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newSeasonFixture(t)
			ctx, actx := fixture.context()
			if _, err := SetLeaseDeposit(ctx, fixture.deps, actx, DepositInput{LeaseID: "lease-season", Amount: 1200}); err != nil {
				t.Fatalf("preparar caução: %v", err)
			}

			view, err := RefundDeposit(ctx, fixture.deps, actx, "lease-season", testCase.requested, "saída sem dano")
			if err != nil {
				t.Fatalf("devolver caução: %v", err)
			}
			if view.Status != testCase.wantStatus {
				t.Fatalf("situação = %q, esperada %q", view.Status, testCase.wantStatus)
			}
			if view.Refunded != testCase.wantAmount {
				t.Fatalf("valor devolvido = %v, esperado %v", view.Refunded, testCase.wantAmount)
			}

			ledger := 0
			for _, receipt := range fixture.receipts.items {
				if receipt.Category == domain.ReceiptCategoryDepositRefund {
					ledger++
					if receipt.Amount != testCase.wantAmount {
						t.Fatalf("lançamento com valor %v, esperado %v", receipt.Amount, testCase.wantAmount)
					}
					if receipt.PaidAt == nil || *receipt.PaidAt == "" {
						t.Fatal("lançamento de devolução sem data de pagamento")
					}
				}
			}
			if testCase.wantLedger && ledger != 1 {
				t.Fatalf("lançamentos de devolução = %d, esperado 1", ledger)
			}
			if !testCase.wantLedger && ledger != 0 {
				t.Fatalf("retenção total não pode gerar lançamento, veio %d", ledger)
			}

			found := false
			for _, entry := range fixture.audit.entries {
				if entry.Action == domain.ActionDepositRefund {
					found = true
				}
			}
			if !found {
				t.Fatal("devolução não registrada na trilha")
			}
		})
	}
}

func TestRefundDepositGuardsAgainstOverRefund(t *testing.T) {
	fixture := newSeasonFixture(t)
	ctx, actx := fixture.context()

	if _, err := RefundDeposit(ctx, fixture.deps, actx, "lease-season", 100, ""); !domain.IsValidation(err) {
		t.Fatalf("devolver sem caução retida deveria falhar, veio %v", err)
	}

	if _, err := SetLeaseDeposit(ctx, fixture.deps, actx, DepositInput{LeaseID: "lease-season", Amount: 1200}); err != nil {
		t.Fatalf("preparar caução: %v", err)
	}
	if _, err := RefundDeposit(ctx, fixture.deps, actx, "lease-season", 1300, ""); !domain.IsValidation(err) {
		t.Fatalf("devolver acima do retido deveria falhar, veio %v", err)
	}

	if _, err := RefundDeposit(ctx, fixture.deps, actx, "lease-season", 1200, ""); err != nil {
		t.Fatalf("devolução integral: %v", err)
	}
	if _, err := RefundDeposit(ctx, fixture.deps, actx, "lease-season", 1200, ""); !domain.IsValidation(err) {
		t.Fatalf("segunda devolução deveria falhar, veio %v", err)
	}
}

// ── Vistoria ───────────────────────────────────────────────────────

func TestInspectionCreatePersistsItemsAndAudits(t *testing.T) {
	fixture := newSeasonFixture(t)
	ctx, actx := fixture.context()

	inspection, err := InspectionCreate(ctx, fixture.deps, actx, InspectionInput{
		LeaseID:     "lease-season",
		Kind:        domain.InspectionKindCheckIn,
		PerformedAt: "2026-09-01T14:00",
		Condition:   domain.InspectionConditionGood,
		Notes:       "chuveiro com marca de uso",
		Items: []domain.InspectionItem{
			{Area: "Sala", Item: "Sofá", State: "bom"},
			{Area: "Cozinha", Item: "Geladeira", State: "bom", Notes: "risco na porta"},
		},
		Photos: []domain.InspectionPhoto{{Name: "inspections/foto-1.jpg", Label: "sala.jpg"}},
	})
	if err != nil {
		t.Fatalf("criar vistoria: %v", err)
	}
	if len(inspection.Items) != 2 {
		t.Fatalf("itens = %d, esperado 2", len(inspection.Items))
	}
	if inspection.TenantName != "Carlos Drummond" || inspection.PropertyName != "Apto 101" {
		t.Fatalf("vistoria sem nomes do contrato: %+v", inspection)
	}
	if len(fixture.inspections.items) != 1 {
		t.Fatal("vistoria não gravada")
	}
	if len(fixture.audit.entries) != 1 || fixture.audit.entries[0].Action != domain.ActionInspectionCreate {
		t.Fatalf("trilha inesperada: %+v", fixture.audit.entries)
	}
}

func TestInspectionCreateValidatesFields(t *testing.T) {
	fixture := newSeasonFixture(t)
	ctx, actx := fixture.context()

	cases := map[string]InspectionInput{
		"tipo inválido":    {LeaseID: "lease-season", Kind: "qualquer", Condition: domain.InspectionConditionGood, PerformedAt: "2026-09-01"},
		"estado inválido":  {LeaseID: "lease-season", Kind: domain.InspectionKindCheckIn, Condition: "perfeito", PerformedAt: "2026-09-01"},
		"data ausente":     {LeaseID: "lease-season", Kind: domain.InspectionKindCheckIn, Condition: domain.InspectionConditionGood},
		"contrato ausente": {LeaseID: "inexistente", Kind: domain.InspectionKindCheckIn, Condition: domain.InspectionConditionGood, PerformedAt: "2026-09-01"},
	}
	for name, input := range cases {
		if _, err := InspectionCreate(ctx, fixture.deps, actx, input); !domain.IsValidation(err) {
			t.Errorf("%s: esperava erro de validação, veio %v", name, err)
		}
	}
}

func TestInspectionUpdateAndDelete(t *testing.T) {
	fixture := newSeasonFixture(t)
	ctx, actx := fixture.context()

	created, err := InspectionCreate(ctx, fixture.deps, actx, InspectionInput{
		LeaseID: "lease-season", Kind: domain.InspectionKindCheckOut,
		PerformedAt: "2026-09-30T11:00", Condition: domain.InspectionConditionFair,
		Photos: []domain.InspectionPhoto{{Name: "inspections/foto-2.jpg"}},
	})
	if err != nil {
		t.Fatalf("criar vistoria: %v", err)
	}

	updated, err := InspectionUpdate(ctx, fixture.deps, actx, InspectionInput{
		ID: created.ID, LeaseID: created.LeaseID, Kind: domain.InspectionKindCheckOut,
		PerformedAt: "2026-09-30T12:00", Condition: domain.InspectionConditionBad,
		Notes: "porta da cozinha danificada",
	})
	if err != nil {
		t.Fatalf("alterar vistoria: %v", err)
	}
	if updated.Condition != domain.InspectionConditionBad {
		t.Fatalf("estado geral = %q", updated.Condition)
	}
	if len(updated.Photos) != 1 || updated.Photos[0].Name != "inspections/foto-2.jpg" {
		t.Fatalf("edição sem upload novo deve preservar as fotos: %+v", updated.Photos)
	}

	var updateEntry *domain.AuditLog
	for index := range fixture.audit.entries {
		if fixture.audit.entries[index].Action == domain.ActionInspectionUpdate {
			updateEntry = &fixture.audit.entries[index]
		}
	}
	if updateEntry == nil {
		t.Fatal("alteração não registrada na trilha")
	}
	if updateEntry.Changes == nil || *updateEntry.Changes == "" {
		t.Fatal("alteração sem diff de campos")
	}

	if err := InspectionDelete(ctx, fixture.deps, actx, created.ID); err != nil {
		t.Fatalf("excluir vistoria: %v", err)
	}
	if len(fixture.inspections.items) != 0 {
		t.Fatal("vistoria não excluída")
	}
	if len(fixture.attachments.deleted) != 1 || fixture.attachments.deleted[0] != "inspections/foto-2.jpg" {
		t.Fatalf("fotos não removidas do armazenamento: %v", fixture.attachments.deleted)
	}
	if err := InspectionDelete(ctx, fixture.deps, actx, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("excluir duas vezes deveria devolver ErrNotFound, veio %v", err)
	}
}

// ── Calendário e bancada ───────────────────────────────────────────

func TestBuildCalendarMarksOccupancy(t *testing.T) {
	fixture := newSeasonFixture(t)
	ctx, _ := fixture.context()

	calendar, err := BuildCalendar(ctx, fixture.deps, 2026, 9)
	if err != nil {
		t.Fatalf("montar calendário: %v", err)
	}
	if calendar.Month.Label != "setembro de 2026" || calendar.Month.Days != 30 {
		t.Fatalf("mês inesperado: %+v", calendar.Month)
	}
	if calendar.Month.Previous != "/dashboard?tab=calendar&year=2026&month=8" ||
		calendar.Month.Next != "/dashboard?tab=calendar&year=2026&month=10" {
		t.Fatalf("navegação inesperada: %q / %q", calendar.Month.Previous, calendar.Month.Next)
	}

	byID := map[string]CalendarProperty{}
	for _, property := range calendar.Properties {
		byID[property.PropertyID] = property
	}

	season := byID["property-1"]
	if len(season.Days) != 30 || season.OccupiedDays != 30 {
		t.Fatalf("temporada: dias=%d ocupados=%d", len(season.Days), season.OccupiedDays)
	}
	if season.Days[0].Status != domain.LeaseStatusSeason || !season.Days[0].Occupied() {
		t.Fatalf("primeiro dia deveria estar ocupado por temporada: %+v", season.Days[0])
	}
	if season.Days[0].TenantName != "Carlos Drummond" {
		t.Fatalf("dia sem inquilino: %+v", season.Days[0])
	}

	terminated := byID["property-3"]
	if terminated.OccupiedDays != 0 {
		t.Fatalf("contrato encerrado não pode ocupar o mês: %d", terminated.OccupiedDays)
	}
	for _, day := range terminated.Days {
		if day.Occupied() {
			t.Fatalf("dia %d marcado como ocupado por contrato encerrado", day.Day)
		}
	}
}

func TestBuildCalendarFallsBackToCurrentMonth(t *testing.T) {
	fixture := newSeasonFixture(t)
	ctx, _ := fixture.context()

	calendar, err := BuildCalendar(ctx, fixture.deps, 0, 99)
	if err != nil {
		t.Fatalf("montar calendário: %v", err)
	}
	if calendar.Month.Year != 2026 || calendar.Month.Month != 9 {
		t.Fatalf("esperava setembro de 2026, veio %+v", calendar.Month)
	}
}

func TestBuildSeasonOverviewCountsDepositsAndCheckouts(t *testing.T) {
	fixture := newSeasonFixture(t)
	ctx, actx := fixture.context()

	if _, err := SetLeaseDeposit(ctx, fixture.deps, actx, DepositInput{LeaseID: "lease-season", Amount: 1200}); err != nil {
		t.Fatalf("preparar caução: %v", err)
	}
	if _, err := InspectionCreate(ctx, fixture.deps, actx, InspectionInput{
		LeaseID: "lease-season", Kind: domain.InspectionKindCheckIn,
		PerformedAt: "2026-09-01T14:00", Condition: domain.InspectionConditionGood,
	}); err != nil {
		t.Fatalf("preparar vistoria: %v", err)
	}

	overview, err := BuildSeasonOverview(ctx, fixture.deps)
	if err != nil {
		t.Fatalf("montar bancada: %v", err)
	}
	if overview.SeasonCount != 1 {
		t.Fatalf("contratos de temporada = %d", overview.SeasonCount)
	}
	if overview.WithDeposit != 1 || overview.DepositsHeld != 1200 {
		t.Fatalf("cauções: %d retidas %v", overview.WithDeposit, overview.DepositsHeld)
	}
	if overview.PendingCheckOut != 1 {
		t.Fatalf("saídas pendentes = %d, esperada 1", overview.PendingCheckOut)
	}
	if len(overview.Rows) != 2 {
		t.Fatalf("linhas = %d, esperado 2 (encerrado fora)", len(overview.Rows))
	}
	for _, row := range overview.Rows {
		if row.Lease.ID == "lease-terminated" {
			t.Fatal("contrato encerrado não deveria aparecer na bancada")
		}
	}
}

func TestInspectionForLease(t *testing.T) {
	fixture := newSeasonFixture(t)
	ctx, actx := fixture.context()

	if _, err := InspectionCreate(ctx, fixture.deps, actx, InspectionInput{
		LeaseID: "lease-season", Kind: domain.InspectionKindCheckIn,
		PerformedAt: "2026-09-01T14:00", Condition: domain.InspectionConditionGood,
	}); err != nil {
		t.Fatalf("criar vistoria: %v", err)
	}

	found, err := InspectionForLease(ctx, fixture.deps, "lease-season", domain.InspectionKindCheckIn)
	if err != nil {
		t.Fatalf("buscar vistoria: %v", err)
	}
	if found.Kind != domain.InspectionKindCheckIn {
		t.Fatalf("tipo = %q", found.Kind)
	}
	if _, err := InspectionForLease(ctx, fixture.deps, "lease-season", domain.InspectionKindCheckOut); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("vistoria de saída inexistente deveria devolver ErrNotFound, veio %v", err)
	}
}
