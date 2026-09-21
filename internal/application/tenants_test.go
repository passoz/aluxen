package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
	"github.com/stretchr/testify/require"
)

// ── Dublês das portas usadas pelos cadastros ───────────────────────
//
// Os dublês guardam cópias das entidades: o caso de uso altera uma cópia e o
// dublê só troca o valor quando o caso de uso pede, como o banco faria.

type catalogClock struct{ now time.Time }

func (c *catalogClock) Now() time.Time { return c.now }

type catalogIDs struct{ next int }

func (g *catalogIDs) New() string {
	g.next++
	return fmt.Sprintf("gen-%d", g.next)
}

type catalogTenantRepo struct {
	items map[string]domain.Tenant
}

func newCatalogTenantRepo(tenants ...domain.Tenant) *catalogTenantRepo {
	repo := &catalogTenantRepo{items: map[string]domain.Tenant{}}
	for _, tenant := range tenants {
		repo.items[tenant.ID] = tenant
	}
	return repo
}

func (r *catalogTenantRepo) List(context.Context) ([]domain.Tenant, error) {
	tenants := make([]domain.Tenant, 0, len(r.items))
	for _, tenant := range r.items {
		tenants = append(tenants, tenant)
	}
	sort.Slice(tenants, func(i, j int) bool { return tenants[i].Name < tenants[j].Name })
	return tenants, nil
}

func (r *catalogTenantRepo) ByID(_ context.Context, id string) (*domain.Tenant, error) {
	tenant, ok := r.items[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &tenant, nil
}

func (r *catalogTenantRepo) ByCPF(_ context.Context, digits string) (*domain.Tenant, error) {
	for _, tenant := range r.items {
		if format.NormalizeDigits(domain.Str(tenant.CPF)) == digits {
			found := tenant
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *catalogTenantRepo) Create(_ context.Context, tenant *domain.Tenant) error {
	if _, exists := r.items[tenant.ID]; exists {
		return domain.ErrConflict
	}
	r.items[tenant.ID] = *tenant
	return nil
}

func (r *catalogTenantRepo) Update(_ context.Context, tenant *domain.Tenant) error {
	if _, exists := r.items[tenant.ID]; !exists {
		return domain.ErrNotFound
	}
	r.items[tenant.ID] = *tenant
	return nil
}

func (r *catalogTenantRepo) UpdatePassword(_ context.Context, id, passwordHash, changedAt, updatedAt string) error {
	tenant, exists := r.items[id]
	if !exists {
		return domain.ErrNotFound
	}
	tenant.PasswordHash = &passwordHash
	tenant.PasswordChangedAt = &changedAt
	tenant.UpdatedAt = updatedAt
	r.items[id] = tenant
	return nil
}

func (r *catalogTenantRepo) Delete(_ context.Context, id string) error {
	if _, exists := r.items[id]; !exists {
		return domain.ErrNotFound
	}
	delete(r.items, id)
	return nil
}

func (r *catalogTenantRepo) Count(context.Context) (int, error) { return len(r.items), nil }

type catalogPropertyRepo struct {
	items map[string]domain.Property
}

func newCatalogPropertyRepo(properties ...domain.Property) *catalogPropertyRepo {
	repo := &catalogPropertyRepo{items: map[string]domain.Property{}}
	for _, property := range properties {
		repo.items[property.ID] = property
	}
	return repo
}

func (r *catalogPropertyRepo) List(context.Context) ([]domain.Property, error) {
	properties := make([]domain.Property, 0, len(r.items))
	for _, property := range r.items {
		properties = append(properties, property)
	}
	sort.Slice(properties, func(i, j int) bool { return properties[i].Name < properties[j].Name })
	return properties, nil
}

func (r *catalogPropertyRepo) ByID(_ context.Context, id string) (*domain.Property, error) {
	property, ok := r.items[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &property, nil
}

func (r *catalogPropertyRepo) Create(_ context.Context, property *domain.Property) error {
	if _, exists := r.items[property.ID]; exists {
		return domain.ErrConflict
	}
	r.items[property.ID] = *property
	return nil
}

func (r *catalogPropertyRepo) Update(_ context.Context, property *domain.Property) error {
	if _, exists := r.items[property.ID]; !exists {
		return domain.ErrNotFound
	}
	r.items[property.ID] = *property
	return nil
}

func (r *catalogPropertyRepo) Delete(_ context.Context, id string) error {
	if _, exists := r.items[id]; !exists {
		return domain.ErrNotFound
	}
	delete(r.items, id)
	return nil
}

func (r *catalogPropertyRepo) Count(context.Context) (int, error) { return len(r.items), nil }

// catalogLeaseRepo implementa apenas a consulta usada pelos cadastros; os
// demais métodos do port ficam no embed (nunca chamados por estes casos de uso).
type catalogLeaseRepo struct {
	domain.LeaseRepository
	items []domain.Lease
}

func (r *catalogLeaseRepo) ByTenant(_ context.Context, tenantID string) ([]domain.Lease, error) {
	leases := make([]domain.Lease, 0, len(r.items))
	for _, lease := range r.items {
		if lease.TenantID == tenantID {
			leases = append(leases, lease)
		}
	}
	return leases, nil
}

type catalogAuditRepo struct{ entries []domain.AuditLog }

func (r *catalogAuditRepo) Insert(_ context.Context, entry *domain.AuditLog) error {
	r.entries = append(r.entries, *entry)
	return nil
}

func (r *catalogAuditRepo) List(context.Context, domain.AuditFilter) ([]domain.AuditLog, error) {
	return r.entries, nil
}

func (r *catalogAuditRepo) Count(context.Context, domain.AuditFilter) (int, error) {
	return len(r.entries), nil
}

func (r *catalogAuditRepo) Actors(context.Context) ([]string, error) { return nil, nil }

// catalogFixture reúne as dependências e os dublês de um cenário de teste.
type catalogFixture struct {
	deps       Deps
	tenants    *catalogTenantRepo
	properties *catalogPropertyRepo
	leases     *catalogLeaseRepo
	audit      *catalogAuditRepo
	clock      *catalogClock
}

func newCatalogFixture() *catalogFixture {
	tenants := newCatalogTenantRepo()
	properties := newCatalogPropertyRepo()
	leases := &catalogLeaseRepo{}
	audit := &catalogAuditRepo{}
	clock := &catalogClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}

	return &catalogFixture{
		deps: Deps{
			Repos: Repositories{
				Tenants:    tenants,
				Properties: properties,
				Leases:     leases,
				Audit:      audit,
			},
			Clock: clock,
			IDs:   &catalogIDs{},
			Log:   slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})),
		},
		tenants:    tenants,
		properties: properties,
		leases:     leases,
		audit:      audit,
		clock:      clock,
	}
}

func (f *catalogFixture) actx() AuditContext {
	return AuditContext{Origin: domain.AuditOriginWeb, IP: "10.0.0.1", UserAgent: "teste"}
}

func (f *catalogFixture) requireAudit(t *testing.T) domain.AuditLog {
	t.Helper()
	require.Len(t, f.audit.entries, 1, "esperava exatamente um evento de auditoria")
	return f.audit.entries[0]
}

func auditChanges(t *testing.T, entry domain.AuditLog) []domain.AuditChange {
	t.Helper()
	if entry.Changes == nil {
		return nil
	}
	var changes []domain.AuditChange
	require.NoError(t, json.Unmarshal([]byte(*entry.Changes), &changes), "changes da auditoria não são JSON de diff")
	return changes
}

func changeFor(t *testing.T, changes []domain.AuditChange, field string) domain.AuditChange {
	t.Helper()
	for _, change := range changes {
		if change.Field == field {
			return change
		}
	}
	t.Fatalf("diff de auditoria sem o campo %q: %+v", field, changes)
	return domain.AuditChange{}
}

const catalogStamp = "2026-09-21 12:00:00"

// ── Locatários ─────────────────────────────────────────────────────

func TestTenantCreateMasksOptionalFieldsAndRecordsAudit(t *testing.T) {
	fixture := newCatalogFixture()

	tenant, err := TenantCreate(context.Background(), fixture.deps, fixture.actx(), TenantInput{
		Name:  "  Carlos Drummond  ",
		Email: " carlos@poesia.com.br ",
		Phone: "22998887766",
		CPF:   "11122233344",
	})
	require.NoError(t, err)

	require.Equal(t, "gen-1", tenant.ID)
	require.Equal(t, "Carlos Drummond", tenant.Name)
	require.Equal(t, "carlos@poesia.com.br", tenant.Email)
	require.Equal(t, "(22) 99888-7766", domain.Str(tenant.Phone))
	require.Equal(t, "111.222.333-44", domain.Str(tenant.CPF))
	require.Equal(t, catalogStamp, tenant.CreatedAt)
	require.Equal(t, catalogStamp, tenant.UpdatedAt)

	stored, err := fixture.tenants.ByID(context.Background(), tenant.ID)
	require.NoError(t, err)
	require.Equal(t, *tenant, *stored)

	entry := fixture.requireAudit(t)
	require.Equal(t, domain.ActionTenantCreate, entry.Action)
	require.Equal(t, domain.EntityTenant, entry.Entity)
	require.Equal(t, "gen-1", domain.Str(entry.EntityID))
	require.Equal(t, "Carlos Drummond", domain.Str(entry.EntityLabel))
	require.Equal(t, `Inquilino "Carlos Drummond" cadastrado`, entry.Summary)
	require.Equal(t, domain.AuditOriginWeb, entry.Origin)

	changes := auditChanges(t, entry)
	require.Len(t, changes, 4)
	name := changeFor(t, changes, "name")
	require.Nil(t, name.From)
	require.Equal(t, "Carlos Drummond", domain.Str(name.To))
	require.Equal(t, "CPF", changeFor(t, changes, "cpf").Label)
	require.Equal(t, "111.222.333-44", domain.Str(changeFor(t, changes, "cpf").To))
	require.Equal(t, "(22) 99888-7766", domain.Str(changeFor(t, changes, "phone").To))
}

func TestTenantCreateKeepsProvidedIDAndLeavesOptionalFieldsEmpty(t *testing.T) {
	fixture := newCatalogFixture()

	tenant, err := TenantCreate(context.Background(), fixture.deps, fixture.actx(), TenantInput{
		ID:    "tenant-fixo",
		Name:  "Ana Cristina",
		Email: "ana@exemplo.com.br",
	})
	require.NoError(t, err)
	require.Equal(t, "tenant-fixo", tenant.ID)
	require.Nil(t, tenant.Phone)
	require.Nil(t, tenant.CPF)

	changes := auditChanges(t, fixture.requireAudit(t))
	require.Len(t, changes, 2, "telefone e CPF vazios não entram no diff")
	for _, change := range changes {
		require.Contains(t, []string{"name", "email"}, change.Field)
	}
}

func TestTenantCreateRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name    string
		input   TenantInput
		message string
	}{
		{"sem nome", TenantInput{Email: "carlos@poesia.com.br"}, "Nome e email são obrigatórios."},
		{"sem email", TenantInput{Name: "Carlos"}, "Nome e email são obrigatórios."},
		{"email inválido", TenantInput{Name: "Carlos", Email: "sem-arroba.teste"}, "Informe um e-mail válido."},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newCatalogFixture()

			_, err := TenantCreate(context.Background(), fixture.deps, fixture.actx(), testCase.input)
			require.Error(t, err)
			require.True(t, domain.IsValidation(err), "esperava erro de validação, veio %v", err)
			require.Equal(t, testCase.message, domain.ValidationMessage(err))
			require.Empty(t, fixture.tenants.items, "cadastro inválido não pode ser gravado")
			require.Empty(t, fixture.audit.entries, "cadastro inválido não pode gerar auditoria")
		})
	}
}

func TestTenantUpdateRecordsOnlyChangedFields(t *testing.T) {
	fixture := newCatalogFixture()
	fixture.tenants.items["t1"] = domain.Tenant{
		ID: "t1", Name: "Carlos Drummond", Email: "carlos@poesia.com.br",
		Phone: domain.StrPtr("(22) 99888-7766"), CPF: domain.StrPtr("111.222.333-44"),
		CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
	}

	tenant, err := TenantUpdate(context.Background(), fixture.deps, fixture.actx(), TenantInput{
		ID:    "t1",
		Name:  "Carlos Drummond de Andrade",
		Email: "carlos@poesia.com.br",
		Phone: "(22) 99888-7766",
		CPF:   "",
	})
	require.NoError(t, err)
	require.Equal(t, "Carlos Drummond de Andrade", tenant.Name)
	require.Nil(t, tenant.CPF)
	require.Equal(t, "2026-01-01 10:00:00", tenant.CreatedAt)
	require.Equal(t, catalogStamp, tenant.UpdatedAt)

	entry := fixture.requireAudit(t)
	require.Equal(t, domain.ActionTenantUpdate, entry.Action)
	require.Equal(t, "t1", domain.Str(entry.EntityID))
	require.Equal(t, "Carlos Drummond de Andrade", domain.Str(entry.EntityLabel))
	require.Equal(t, `Inquilino "Carlos Drummond de Andrade" alterado`, entry.Summary)

	changes := auditChanges(t, entry)
	require.Len(t, changes, 2, "e-mail e telefone iguais ficam fora do diff")
	require.Equal(t, "Carlos Drummond", domain.Str(changeFor(t, changes, "name").From))
	require.Equal(t, "Carlos Drummond de Andrade", domain.Str(changeFor(t, changes, "name").To))
	cpf := changeFor(t, changes, "cpf")
	require.Equal(t, "111.222.333-44", domain.Str(cpf.From))
	require.Nil(t, cpf.To)

	stored, err := fixture.tenants.ByID(context.Background(), "t1")
	require.NoError(t, err)
	require.Equal(t, "Carlos Drummond de Andrade", stored.Name)
	require.Nil(t, stored.CPF)
}

func TestTenantUpdateRequiresIDAndExistingRecord(t *testing.T) {
	fixture := newCatalogFixture()

	_, err := TenantUpdate(context.Background(), fixture.deps, fixture.actx(), TenantInput{Name: "Carlos", Email: "carlos@poesia.com.br"})
	require.Equal(t, "Nome e email são obrigatórios.", domain.ValidationMessage(err))

	_, err = TenantUpdate(context.Background(), fixture.deps, fixture.actx(), TenantInput{ID: "inexistente", Name: "Carlos", Email: "carlos@poesia.com.br"})
	require.ErrorIs(t, err, domain.ErrNotFound)
	require.Empty(t, fixture.audit.entries)
}

func TestTenantDeleteBlocksActiveLease(t *testing.T) {
	fixture := newCatalogFixture()
	fixture.tenants.items["t1"] = domain.Tenant{ID: "t1", Name: "Carlos Drummond", Email: "carlos@poesia.com.br"}
	fixture.leases.items = []domain.Lease{
		{ID: "l1", TenantID: "t1", PropertyID: "p1", Status: domain.LeaseStatusActive},
		{ID: "l2", TenantID: "t1", PropertyID: "p2", Status: domain.LeaseStatusTerminated},
	}

	err := TenantDelete(context.Background(), fixture.deps, fixture.actx(), "t1")
	require.Equal(t, "Não é possível excluir inquilino com contrato ativo. Encerre o contrato primeiro.", domain.ValidationMessage(err))
	require.Contains(t, fixture.tenants.items, "t1", "locatário com contrato ativo não pode ser excluído")
	require.Empty(t, fixture.audit.entries)
}

func TestTenantDeleteRemovesTenantAndRecordsAudit(t *testing.T) {
	fixture := newCatalogFixture()
	fixture.tenants.items["t1"] = domain.Tenant{
		ID: "t1", Name: "Carlos Drummond", Email: "carlos@poesia.com.br",
		Phone: domain.StrPtr("(22) 99888-7766"), CPF: domain.StrPtr("111.222.333-44"),
	}
	fixture.leases.items = []domain.Lease{{ID: "l1", TenantID: "t1", PropertyID: "p1", Status: domain.LeaseStatusTerminated}}

	require.NoError(t, TenantDelete(context.Background(), fixture.deps, fixture.actx(), "t1"))
	require.NotContains(t, fixture.tenants.items, "t1")

	entry := fixture.requireAudit(t)
	require.Equal(t, domain.ActionTenantDelete, entry.Action)
	require.Equal(t, domain.EntityTenant, entry.Entity)
	require.Equal(t, "Carlos Drummond", domain.Str(entry.EntityLabel))
	require.Equal(t, `Inquilino "Carlos Drummond" excluído`, entry.Summary)

	changes := auditChanges(t, entry)
	require.Len(t, changes, 4)
	for _, change := range changes {
		require.NotNil(t, change.From)
		require.Nil(t, change.To, "exclusão apaga todos os campos no diff")
	}
}

func TestTenantDeleteRequiresIDAndExistingRecord(t *testing.T) {
	fixture := newCatalogFixture()

	require.Equal(t, "Registro não informado.", domain.ValidationMessage(
		TenantDelete(context.Background(), fixture.deps, fixture.actx(), "  ")))
	require.ErrorIs(t, TenantDelete(context.Background(), fixture.deps, fixture.actx(), "inexistente"), domain.ErrNotFound)
}
