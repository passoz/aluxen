package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/domain"
)

// ── Dublês ─────────────────────────────────────────────────────────

type leaseClock struct{ now time.Time }

func (c *leaseClock) Now() time.Time { return c.now }

type leaseIDs struct{ next int }

func (g *leaseIDs) New() string {
	g.next++
	return fmt.Sprintf("lease-id-%d", g.next)
}

type leaseAuditRepo struct{ entries []domain.AuditLog }

func (r *leaseAuditRepo) Insert(_ context.Context, entry *domain.AuditLog) error {
	r.entries = append(r.entries, *entry)
	return nil
}

func (r *leaseAuditRepo) List(context.Context, domain.AuditFilter) ([]domain.AuditLog, error) {
	return r.entries, nil
}

func (r *leaseAuditRepo) Count(context.Context, domain.AuditFilter) (int, error) {
	return len(r.entries), nil
}

func (r *leaseAuditRepo) Actors(context.Context) ([]string, error) { return nil, nil }

func (r *leaseAuditRepo) actions() []string {
	actions := make([]string, 0, len(r.entries))
	for _, entry := range r.entries {
		actions = append(actions, entry.Action)
	}
	return actions
}

// leaseFakeLeases implementa a porta de contratos em memória, reproduzindo a
// checagem de sobreposição do SQL (somente ativo/temporada ocupam o imóvel).
type leaseFakeLeases struct {
	items         map[string]*domain.Lease
	order         []string
	tenantNames   map[string]string
	propertyNames map[string]string
}

func newLeaseFakeLeases() *leaseFakeLeases {
	return &leaseFakeLeases{
		items:         map[string]*domain.Lease{},
		tenantNames:   map[string]string{},
		propertyNames: map[string]string{},
	}
}

func (r *leaseFakeLeases) withNames(tenantID, tenantName, propertyID, propertyName string) {
	r.tenantNames[tenantID] = tenantName
	r.propertyNames[propertyID] = propertyName
}

func (r *leaseFakeLeases) named(lease *domain.Lease) domain.Lease {
	out := *lease
	out.TenantName = r.tenantNames[lease.TenantID]
	out.PropertyName = r.propertyNames[lease.PropertyID]
	return out
}

func (r *leaseFakeLeases) List(context.Context) ([]domain.Lease, error) {
	out := make([]domain.Lease, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.named(r.items[id]))
	}
	return out, nil
}

func (r *leaseFakeLeases) ByID(_ context.Context, id string) (*domain.Lease, error) {
	lease, ok := r.items[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	out := r.named(lease)
	return &out, nil
}

func (r *leaseFakeLeases) ByTenant(_ context.Context, tenantID string) ([]domain.Lease, error) {
	out := []domain.Lease{}
	for _, id := range r.order {
		if lease := r.items[id]; lease.TenantID == tenantID {
			out = append(out, r.named(lease))
		}
	}
	return out, nil
}

func (r *leaseFakeLeases) ByProperty(_ context.Context, propertyID string) ([]domain.Lease, error) {
	out := []domain.Lease{}
	for _, id := range r.order {
		if lease := r.items[id]; lease.PropertyID == propertyID {
			out = append(out, r.named(lease))
		}
	}
	return out, nil
}

func (r *leaseFakeLeases) Overlapping(_ context.Context, propertyID, excludeID, startDate, endDate string) ([]domain.Lease, error) {
	out := []domain.Lease{}
	for _, id := range r.order {
		lease := r.items[id]
		if lease.PropertyID != propertyID || lease.ID == excludeID || !lease.Status.BlocksOverlap() {
			continue
		}
		if domain.Overlaps(lease.StartDate, lease.EndDate, startDate, endDate) {
			out = append(out, r.named(lease))
		}
	}
	return out, nil
}

func (r *leaseFakeLeases) ActiveInRange(_ context.Context, startDate, endDate string) ([]domain.Lease, error) {
	out := []domain.Lease{}
	for _, id := range r.order {
		lease := r.items[id]
		if lease.Status != domain.LeaseStatusActive {
			continue
		}
		if domain.Overlaps(lease.StartDate, lease.EndDate, startDate, endDate) {
			out = append(out, r.named(lease))
		}
	}
	return out, nil
}

func (r *leaseFakeLeases) Create(_ context.Context, lease *domain.Lease) error {
	stored := *lease
	r.items[lease.ID] = &stored
	r.order = append(r.order, lease.ID)
	return nil
}

func (r *leaseFakeLeases) Update(_ context.Context, lease *domain.Lease) error {
	if _, ok := r.items[lease.ID]; !ok {
		return domain.ErrNotFound
	}
	stored := *lease
	r.items[lease.ID] = &stored
	return nil
}

func (r *leaseFakeLeases) UpdateStatus(_ context.Context, id string, status domain.LeaseStatus, updatedAt string) error {
	lease, ok := r.items[id]
	if !ok {
		return domain.ErrNotFound
	}
	lease.Status = status
	lease.UpdatedAt = updatedAt
	return nil
}

func (r *leaseFakeLeases) Delete(_ context.Context, id string) error {
	if _, ok := r.items[id]; !ok {
		return domain.ErrNotFound
	}
	delete(r.items, id)
	for index, candidate := range r.order {
		if candidate == id {
			r.order = append(r.order[:index], r.order[index+1:]...)
			break
		}
	}
	return nil
}

func (r *leaseFakeLeases) Count(context.Context) (int, error) { return len(r.items), nil }

func (r *leaseFakeLeases) CountByStatus(_ context.Context, status domain.LeaseStatus) (int, error) {
	total := 0
	for _, lease := range r.items {
		if lease.Status == status {
			total++
		}
	}
	return total, nil
}

// ── Fixture ────────────────────────────────────────────────────────

func newLeaseDeps() (Deps, *leaseFakeLeases, *leaseAuditRepo) {
	leases := newLeaseFakeLeases()
	leases.withNames("t1", "Ana", "p1", "Casa")
	leases.withNames("t2", "Bruno", "p2", "Apto")
	audit := &leaseAuditRepo{}
	deps := Deps{
		Repos: Repositories{Leases: leases, Audit: audit},
		Clock: &leaseClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)},
		IDs:   &leaseIDs{},
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return deps, leases, audit
}

func leaseFixture(id, tenantID, propertyID, startDate, endDate string, dueDay int, rent float64, status domain.LeaseStatus) LeaseInput {
	return LeaseInput{
		ID:         id,
		TenantID:   tenantID,
		PropertyID: propertyID,
		StartDate:  startDate,
		EndDate:    endDate,
		DueDay:     dueDay,
		RentAmount: rent,
		Status:     status,
	}
}

func requireLeaseValidation(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("esperava erro %q, veio nil", want)
	}
	if !domain.IsValidation(err) {
		t.Fatalf("erro não é de validação: %v", err)
	}
	if got := domain.ValidationMessage(err); got != want {
		t.Fatalf("mensagem = %q, esperada %q", got, want)
	}
}

func requireNoLeaseError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
}

func leaseAuditEntry(t *testing.T, repo *leaseAuditRepo, action string) domain.AuditLog {
	t.Helper()
	for _, entry := range repo.entries {
		if entry.Action == action {
			return entry
		}
	}
	t.Fatalf("trilha sem %s: %v", action, repo.actions())
	return domain.AuditLog{}
}

func leaseAuditChanges(t *testing.T, entry domain.AuditLog) []domain.AuditChange {
	t.Helper()
	if entry.Changes == nil {
		return nil
	}
	var changes []domain.AuditChange
	if err := json.Unmarshal([]byte(*entry.Changes), &changes); err != nil {
		t.Fatalf("alterações inválidas: %v", err)
	}
	return changes
}

func leaseChangeFor(t *testing.T, changes []domain.AuditChange, field string) domain.AuditChange {
	t.Helper()
	for _, change := range changes {
		if change.Field == field {
			return change
		}
	}
	t.Fatalf("diff sem o campo %q: %+v", field, changes)
	return domain.AuditChange{}
}

func leaseStatusIn(t *testing.T, repo *leaseFakeLeases, id string) domain.LeaseStatus {
	t.Helper()
	lease, ok := repo.items[id]
	if !ok {
		t.Fatalf("contrato %q não encontrado", id)
	}
	return lease.Status
}

// ── Sobreposição ───────────────────────────────────────────────────

func TestLeaseCreateRejectsActiveOverlap(t *testing.T) {
	deps, _, _ := newLeaseDeps()
	ctx := context.Background()

	_, err := LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive))
	requireNoLeaseError(t, err)

	_, err = LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("", "t2", "p1", "2026-06-01", "2026-06-30", 10, 1800, domain.LeaseStatusActive))
	requireLeaseValidation(t, err, "Este imóvel já possui contrato vigente no período.")
}

func TestLeaseCreateRejectsSeasonOverActive(t *testing.T) {
	deps, _, _ := newLeaseDeps()
	ctx := context.Background()

	_, err := LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive))
	requireNoLeaseError(t, err)

	_, err = LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("", "t2", "p1", "2026-06-01", "2026-06-20", 5, 900, domain.LeaseStatusSeason))
	requireLeaseValidation(t, err, "Este imóvel já possui contrato vigente no período. Temporada só pode ser criada em imóvel sem contrato ativo ou com contrato suspenso.")
}

func TestLeaseCreateSuspendedDoesNotBlockProperty(t *testing.T) {
	deps, _, _ := newLeaseDeps()
	ctx := context.Background()

	_, err := LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusSuspended))
	requireNoLeaseError(t, err)

	// Ativo sobre suspenso é permitido (o suspenso não ocupa o imóvel).
	_, err = LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("", "t2", "p1", "2026-03-01", "2026-09-30", 5, 1800, domain.LeaseStatusActive))
	requireNoLeaseError(t, err)

	// Temporada sobre suspenso também é permitida, como no app atual.
	_, err = LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("", "t2", "p1", "2026-06-01", "2026-06-20", 5, 900, domain.LeaseStatusSeason))
	requireLeaseValidation(t, err, "Este imóvel já possui contrato vigente no período. Temporada só pode ser criada em imóvel sem contrato ativo ou com contrato suspenso.")
}

func TestLeaseCreateSeasonOnFreeProperty(t *testing.T) {
	deps, _, _ := newLeaseDeps()
	ctx := context.Background()

	created, err := LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("", "t1", "p1", "2026-06-01", "2026-06-20", 5, 900, domain.LeaseStatusSeason))
	requireNoLeaseError(t, err)
	if created.Status != domain.LeaseStatusSeason {
		t.Fatalf("status = %q, esperado season", created.Status)
	}
	if created.TenantName != "Ana" || created.PropertyName != "Casa" {
		t.Fatalf("nomes do join inesperados: %q / %q", created.TenantName, created.PropertyName)
	}
}

func TestLeaseCreateNonOverlappingPeriodIsAllowed(t *testing.T) {
	deps, _, _ := newLeaseDeps()
	ctx := context.Background()

	_, err := LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("", "t1", "p1", "2026-01-01", "2026-06-30", 10, 2500, domain.LeaseStatusActive))
	requireNoLeaseError(t, err)

	_, err = LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("", "t2", "p1", "2026-07-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive))
	requireNoLeaseError(t, err)
}

func TestLeaseUpdateIgnoresItself(t *testing.T) {
	deps, _, _ := newLeaseDeps()
	ctx := context.Background()

	created, err := LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive))
	requireNoLeaseError(t, err)

	updated, err := LeaseUpdate(ctx, deps, AuditContext{}, leaseFixture(created.ID, "t1", "p1", "2026-02-01", "2026-11-30", 12, 2700, domain.LeaseStatusActive))
	requireNoLeaseError(t, err)
	if updated.DueDay != 12 || updated.RentAmount != 2700 {
		t.Fatalf("contrato não atualizado: %+v", updated)
	}
}

func TestLeaseUpdateRejectsOverlapWithOtherLease(t *testing.T) {
	deps, _, _ := newLeaseDeps()
	ctx := context.Background()

	_, err := LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive))
	requireNoLeaseError(t, err)
	_, err = LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("l2", "t2", "p2", "2026-01-01", "2026-12-31", 10, 3200, domain.LeaseStatusActive))
	requireNoLeaseError(t, err)

	_, err = LeaseUpdate(ctx, deps, AuditContext{}, leaseFixture("l1", "t1", "p2", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive))
	requireLeaseValidation(t, err, "Este imóvel já possui contrato vigente no período.")
}

func TestLeaseUpdateKeepsStatusWhenOmitted(t *testing.T) {
	deps, leases, _ := newLeaseDeps()
	ctx := context.Background()

	_, err := LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusSuspended))
	requireNoLeaseError(t, err)

	input := leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 15, 2600, "")
	updated, err := LeaseUpdate(ctx, deps, AuditContext{}, input)
	requireNoLeaseError(t, err)
	if updated.Status != domain.LeaseStatusSuspended {
		t.Fatalf("status = %q, esperado manter suspended", updated.Status)
	}
	if leaseStatusIn(t, leases, "l1") != domain.LeaseStatusSuspended {
		t.Fatal("status não preservado no repositório")
	}
}

// ── Transições de situação ─────────────────────────────────────────

func TestLeaseSetStatusTransitions(t *testing.T) {
	deps, leases, audit := newLeaseDeps()
	ctx := context.Background()

	_, err := LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive))
	requireNoLeaseError(t, err)

	steps := []struct {
		intent string
		status domain.LeaseStatus
	}{
		{domain.ActionLeaseTerminate, domain.LeaseStatusTerminated},
		{domain.ActionLeaseReactivate, domain.LeaseStatusActive},
		{domain.ActionLeaseSuspend, domain.LeaseStatusSuspended},
		{domain.ActionLeaseReactivate, domain.LeaseStatusActive},
	}
	for _, step := range steps {
		requireNoLeaseError(t, LeaseSetStatus(ctx, deps, AuditContext{}, step.intent, "l1"))
		if got := leaseStatusIn(t, leases, "l1"); got != step.status {
			t.Fatalf("%s: status = %q, esperado %q", step.intent, got, step.status)
		}
		entry := leaseAuditEntry(t, audit, step.intent)
		if entry.Entity != domain.EntityLease || domain.Str(entry.EntityID) != "l1" {
			t.Fatalf("%s: evento inesperado %+v", step.intent, entry)
		}
	}

	requireNoLeaseError(t, LeaseSetStatus(ctx, deps, AuditContext{}, domain.ActionLeaseTerminate, "l1"))
	if leaseStatusIn(t, leases, "l1") != domain.LeaseStatusTerminated {
		t.Fatal("encerramento final não aplicado")
	}
	entry := leaseAuditEntry(t, audit, domain.ActionLeaseTerminate)
	changes := leaseAuditChanges(t, entry)
	status := leaseChangeFor(t, changes, "status")
	if status.To == nil || *status.To != "Encerrado" {
		t.Fatalf("diff de situação inesperado: %+v", status)
	}
}

func TestLeaseSetStatusRejectsUnknownIntent(t *testing.T) {
	deps, _, _ := newLeaseDeps()

	err := LeaseSetStatus(context.Background(), deps, AuditContext{}, "lease.promote", "l1")
	requireLeaseValidation(t, err, "Ação inválida.")
}

// ── Exclusão e auditoria ───────────────────────────────────────────

func TestLeaseDeleteRemovesLeaseAndAudits(t *testing.T) {
	deps, leases, audit := newLeaseDeps()
	ctx := context.Background()

	_, err := LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive))
	requireNoLeaseError(t, err)

	requireNoLeaseError(t, LeaseDelete(ctx, deps, AuditContext{}, "l1"))
	if _, ok := leases.items["l1"]; ok {
		t.Fatal("contrato não foi excluído")
	}

	entry := leaseAuditEntry(t, audit, domain.ActionLeaseDelete)
	if domain.Str(entry.EntityLabel) != "Ana · Casa" {
		t.Fatalf("rótulo = %q, esperado \"Ana · Casa\"", domain.Str(entry.EntityLabel))
	}
	changes := leaseAuditChanges(t, entry)
	tenant := leaseChangeFor(t, changes, "tenant_name")
	if tenant.From == nil || *tenant.From != "Ana" || tenant.To != nil {
		t.Fatalf("diff de exclusão inesperado: %+v", tenant)
	}
}

func TestLeaseCreateAuditSnapshot(t *testing.T) {
	deps, _, audit := newLeaseDeps()

	_, err := LeaseCreate(context.Background(), deps, AuditContext{}, leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive))
	requireNoLeaseError(t, err)

	entry := leaseAuditEntry(t, audit, domain.ActionLeaseCreate)
	if entry.Entity != domain.EntityLease || domain.Str(entry.EntityID) != "l1" {
		t.Fatalf("evento inesperado: %+v", entry)
	}
	if domain.Str(entry.EntityLabel) != "Ana · Casa" || entry.Summary != "Contrato de Ana · Casa criado" {
		t.Fatalf("rótulo/resumo inesperados: %q / %q", domain.Str(entry.EntityLabel), entry.Summary)
	}
	changes := leaseAuditChanges(t, entry)
	if got := leaseChangeFor(t, changes, "status"); got.To == nil || *got.To != "Ativo" {
		t.Fatalf("situação inicial inesperada: %+v", got)
	}
	if got := leaseChangeFor(t, changes, "rentAmount"); got.To == nil || *got.To != "R$ 2.500,00" {
		t.Fatalf("aluguel inicial inesperado: %+v", got)
	}
}

func TestLeaseUpdateAuditDiff(t *testing.T) {
	deps, _, audit := newLeaseDeps()
	ctx := context.Background()

	_, err := LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive))
	requireNoLeaseError(t, err)
	_, err = LeaseUpdate(ctx, deps, AuditContext{}, leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 15, 2800, domain.LeaseStatusActive))
	requireNoLeaseError(t, err)

	entry := leaseAuditEntry(t, audit, domain.ActionLeaseUpdate)
	changes := leaseAuditChanges(t, entry)
	if len(changes) != 2 {
		t.Fatalf("diff = %+v, esperava 2 alterações", changes)
	}
	rent := leaseChangeFor(t, changes, "rentAmount")
	if rent.From == nil || *rent.From != "R$ 2.500,00" || rent.To == nil || *rent.To != "R$ 2.800,00" {
		t.Fatalf("diff de aluguel inesperado: %+v", rent)
	}
}

// ── Validações ─────────────────────────────────────────────────────

func TestLeaseCreateValidations(t *testing.T) {
	cases := []struct {
		name  string
		input LeaseInput
		want  string
	}{
		{
			name:  "status inválido",
			input: leaseFixture("", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatus("invalid-status")),
			want:  "Status de contrato inválido.",
		},
		{
			name:  "inquilino ausente",
			input: leaseFixture("", "", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive),
			want:  "Preencha todos os campos do contrato.",
		},
		{
			name:  "imóvel ausente",
			input: leaseFixture("", "t1", "", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive),
			want:  "Preencha todos os campos do contrato.",
		},
		{
			name:  "período ausente",
			input: leaseFixture("", "t1", "p1", "", "", 10, 2500, domain.LeaseStatusActive),
			want:  "Informe o período do contrato.",
		},
		{
			name:  "término antes do início",
			input: leaseFixture("", "t1", "p1", "2026-12-31", "2026-01-01", 10, 2500, domain.LeaseStatusActive),
			want:  "A data de término deve ser igual ou posterior à data de início.",
		},
		{
			name:  "dia de vencimento zero",
			input: leaseFixture("", "t1", "p1", "2026-01-01", "2026-12-31", 0, 2500, domain.LeaseStatusActive),
			want:  "Informe um dia de vencimento entre 1 e 31.",
		},
		{
			name:  "dia de vencimento acima de 31",
			input: leaseFixture("", "t1", "p1", "2026-01-01", "2026-12-31", 32, 2500, domain.LeaseStatusActive),
			want:  "Informe um dia de vencimento entre 1 e 31.",
		},
		{
			name:  "aluguel zero",
			input: leaseFixture("", "t1", "p1", "2026-01-01", "2026-12-31", 10, 0, domain.LeaseStatusActive),
			want:  "Informe o valor do aluguel.",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			deps, _, _ := newLeaseDeps()
			_, err := LeaseCreate(context.Background(), deps, AuditContext{}, test.input)
			requireLeaseValidation(t, err, test.want)
		})
	}
}

func TestLeaseUpdateValidations(t *testing.T) {
	deps, _, _ := newLeaseDeps()
	ctx := context.Background()

	_, err := LeaseCreate(ctx, deps, AuditContext{}, leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive))
	requireNoLeaseError(t, err)

	_, err = LeaseUpdate(ctx, deps, AuditContext{}, leaseFixture("", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive))
	requireLeaseValidation(t, err, "Preencha todos os campos do contrato.")

	_, err = LeaseUpdate(ctx, deps, AuditContext{}, leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatus("invalid-status")))
	requireLeaseValidation(t, err, "Status de contrato inválido.")

	_, err = LeaseUpdate(ctx, deps, AuditContext{}, leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 40, 2500, domain.LeaseStatusActive))
	requireLeaseValidation(t, err, "Informe um dia de vencimento entre 1 e 31.")
}

func TestLeaseStatusChangeRejectsMissingLease(t *testing.T) {
	deps, _, _ := newLeaseDeps()

	requireLeaseValidation(t, LeaseSetStatus(context.Background(), deps, AuditContext{}, domain.ActionLeaseTerminate, ""), "Contrato não informado.")
	requireLeaseValidation(t, LeaseDelete(context.Background(), deps, AuditContext{}, ""), "Contrato não informado.")
}

// ── Caução e vistoria opcionais no formulário (Work 0007, task 7.5) ──

// leaseDepositDeps estende newLeaseDeps com o repositório de vistorias, para
// os testes de caução e de vistoria de entrada no formulário de contrato.
func leaseDepositDeps() (Deps, *leaseFakeLeases, *leaseAuditRepo, *covInspections) {
	deps, leases, audit := newLeaseDeps()
	inspections := &covInspections{}
	deps.Repos.Inspections = inspections
	return deps, leases, audit, inspections
}

func leaseDepositSummary(entries []domain.AuditLog) []string {
	summaries := make([]string, 0, len(entries))
	for _, entry := range entries {
		summaries = append(summaries, entry.Summary)
	}
	return summaries
}

func TestLeaseCreateWithDeposit(t *testing.T) {
	deps, _, audit, _ := leaseDepositDeps()
	ctx := context.Background()

	input := leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive)
	input.DepositAmount = domain.FloatPtr(900)
	input.DepositNotes = "caução do fixo"
	lease, err := LeaseCreate(ctx, deps, covCtx(), input)
	if err != nil {
		t.Fatalf("criar com caução: %v", err)
	}
	if lease.DepositAmount == nil || *lease.DepositAmount != 900 {
		t.Fatalf("caução = %v, esperada 900", lease.DepositAmount)
	}
	if lease.DepositStatus != domain.DepositStatusHeld {
		t.Fatalf("situação = %q, esperada held", lease.DepositStatus)
	}
	if lease.DepositReceivedAt == nil || *lease.DepositReceivedAt == "" {
		t.Fatal("caução sem data de recebimento")
	}
	summaries := leaseDepositSummary(audit.entries)
	foundCreate, foundDeposit := false, false
	for _, summary := range summaries {
		if summary == "Contrato de Ana · Casa criado" {
			foundCreate = true
		}
		if summary == "Caução registrada" {
			foundDeposit = true
		}
	}
	if !foundCreate || !foundDeposit {
		t.Fatalf("trilha sem criação e caução: %v", summaries)
	}
}

func TestLeaseCreateWithDepositOnSeason(t *testing.T) {
	deps, _, _, _ := leaseDepositDeps()
	ctx := context.Background()

	// A caução não olha o status do contrato: temporada aceita como o fixo.
	input := leaseFixture("l1", "t1", "p1", "2026-09-01", "2026-09-30", 5, 2500, domain.LeaseStatusSeason)
	input.DepositAmount = domain.FloatPtr(1200)
	lease, err := LeaseCreate(ctx, deps, covCtx(), input)
	if err != nil {
		t.Fatalf("criar temporada com caução: %v", err)
	}
	if lease.DepositStatus != domain.DepositStatusHeld {
		t.Fatalf("situação = %q, esperada held", lease.DepositStatus)
	}
}

func TestLeaseCreateWithoutDeposit(t *testing.T) {
	deps, leases, _, _ := leaseDepositDeps()
	ctx := context.Background()

	lease, err := LeaseCreate(ctx, deps, covCtx(), leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive))
	if err != nil {
		t.Fatalf("criar sem caução: %v", err)
	}
	if lease.DepositAmount != nil || lease.DepositStatus != "" {
		t.Fatalf("contrato sem caução ganhou caução: %+v", lease)
	}
	stored, err := leases.ByID(ctx, "l1")
	if err != nil {
		t.Fatalf("buscar contrato: %v", err)
	}
	if stored.DepositAmount != nil {
		t.Fatalf("caução gravada sem pedido: %v", stored.DepositAmount)
	}
}

func TestLeaseCreateRejectsInvalidDeposit(t *testing.T) {
	deps, leases, _, _ := leaseDepositDeps()
	ctx := context.Background()

	input := leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive)
	input.DepositAmount = domain.FloatPtr(0)
	_, err := LeaseCreate(ctx, deps, covCtx(), input)
	requireLeaseValidation(t, err, "Informe o valor da caução.")
	if _, err := leases.ByID(ctx, "l1"); err == nil {
		t.Fatal("contrato gravado com caução inválida")
	}
}

func TestLeaseUpdateBlankDepositPreserves(t *testing.T) {
	deps, _, audit, _ := leaseDepositDeps()
	ctx := context.Background()

	if _, err := LeaseCreate(ctx, deps, covCtx(), leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive)); err != nil {
		t.Fatalf("criar: %v", err)
	}
	setInput := leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive)
	setInput.DepositAmount = domain.FloatPtr(500)
	if _, err := LeaseUpdate(ctx, deps, covCtx(), setInput); err != nil {
		t.Fatalf("registrar caução: %v", err)
	}

	// Campo em branco na edição preserva caução e não registra de novo.
	before := len(audit.entries)
	updated, err := LeaseUpdate(ctx, deps, covCtx(), leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2600, domain.LeaseStatusActive))
	if err != nil {
		t.Fatalf("editar: %v", err)
	}
	if updated.DepositAmount == nil || *updated.DepositAmount != 500 || updated.DepositStatus != domain.DepositStatusHeld {
		t.Fatalf("caução apagada na edição: %+v", updated)
	}
	for _, entry := range audit.entries[before:] {
		if entry.Summary == "Caução registrada" {
			t.Fatalf("caução registrada de novo sem pedido: %+v", entry)
		}
	}
}

func TestLeaseUpdateWithDepositSets(t *testing.T) {
	deps, _, audit, _ := leaseDepositDeps()
	ctx := context.Background()

	if _, err := LeaseCreate(ctx, deps, covCtx(), leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive)); err != nil {
		t.Fatalf("criar: %v", err)
	}
	input := leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive)
	input.DepositAmount = domain.FloatPtr(700)
	updated, err := LeaseUpdate(ctx, deps, covCtx(), input)
	if err != nil {
		t.Fatalf("editar com caução: %v", err)
	}
	if updated.DepositAmount == nil || *updated.DepositAmount != 700 || updated.DepositStatus != domain.DepositStatusHeld {
		t.Fatalf("caução = %+v, esperada 700 held", updated)
	}
	found := false
	for _, entry := range audit.entries {
		if entry.Summary == "Caução registrada" {
			found = true
		}
	}
	if !found {
		t.Fatalf("trilha sem caução: %v", leaseDepositSummary(audit.entries))
	}
}

func TestLeaseCreateWithCheckIn(t *testing.T) {
	deps, _, _, inspections := leaseDepositDeps()
	ctx := context.Background()

	input := leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive)
	input.InspectionCheckIn = true
	input.InspectionCondition = domain.InspectionConditionGood
	input.InspectionPerformedAt = "2026-01-01"
	input.InspectionNotes = "entrada tranquila"
	if _, err := LeaseCreate(ctx, deps, covCtx(), input); err != nil {
		t.Fatalf("criar com vistoria: %v", err)
	}
	found, err := inspections.ByLease(ctx, "l1")
	if err != nil {
		t.Fatalf("listar vistorias: %v", err)
	}
	if len(found) != 1 || found[0].Kind != domain.InspectionKindCheckIn {
		t.Fatalf("vistorias = %+v, esperada 1 de entrada", found)
	}
	if found[0].Condition != domain.InspectionConditionGood || found[0].PerformedAt != "2026-01-01" {
		t.Fatalf("vistoria = %+v", found[0])
	}
}

func TestLeaseCreateCheckInDefaults(t *testing.T) {
	deps, _, _, inspections := leaseDepositDeps()
	ctx := context.Background()

	// Só o pedido: estado padrão é boa e a data é o início do contrato.
	input := leaseFixture("l1", "t1", "p1", "2026-01-01", "2026-12-31", 10, 2500, domain.LeaseStatusActive)
	input.InspectionCheckIn = true
	if _, err := LeaseCreate(ctx, deps, covCtx(), input); err != nil {
		t.Fatalf("criar com vistoria padrão: %v", err)
	}
	found, err := inspections.ByLease(ctx, "l1")
	if err != nil {
		t.Fatalf("listar vistorias: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("vistorias = %+v, esperada 1", found)
	}
	if found[0].Condition != domain.InspectionConditionGood || found[0].PerformedAt != "2026-01-01" {
		t.Fatalf("padrões = %+v", found[0])
	}
}
