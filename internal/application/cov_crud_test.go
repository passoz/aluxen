package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/stretchr/testify/require"
)

// Cadastros do painel: locatários, imóveis, contratos e lançamentos — regras de
// validação, trilha de auditoria e erros de repositório.

// ── tenants.go ────────────────────────────────────────────────────

func TestCovTenantCreateAppliesMasksAndAudits(t *testing.T) {
	env := newCovEnv(t)

	tenant, err := TenantCreate(context.Background(), env.deps, covCtx(), TenantInput{
		ID: "t-fixo", Name: " Ana Lima ", Email: " ana@example.com ",
		Phone: "21999998888", CPF: "12345678900",
	})
	require.NoError(t, err)
	require.Equal(t, "t-fixo", tenant.ID, "o id do formulário é preservado (fila offline)")
	require.Equal(t, "Ana Lima", tenant.Name)
	require.Equal(t, "(21) 99999-8888", domain.Str(tenant.Phone))
	require.Equal(t, "123.456.789-00", domain.Str(tenant.CPF))
	require.Equal(t, tenant.CreatedAt, tenant.UpdatedAt)
	require.Len(t, env.tenants.items, 1)
	require.Equal(t, []string{domain.ActionTenantCreate}, covAuditActions(env.audit.entries))
}

func TestCovTenantCreateWithoutOptionalFieldsAndGeneratedID(t *testing.T) {
	env := newCovEnv(t)

	tenant, err := TenantCreate(context.Background(), env.deps, covCtx(), TenantInput{Name: "Ana", Email: "ana@example.com"})
	require.NoError(t, err)
	require.Equal(t, "cov-001", tenant.ID)
	require.Nil(t, tenant.Phone, "telefone em branco não vira string vazia")
	require.Nil(t, tenant.CPF)
}

func TestCovTenantCreateValidatesInput(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()

	_, err := TenantCreate(ctx, env.deps, covCtx(), TenantInput{Email: "ana@example.com"})
	require.EqualError(t, err, "Nome e email são obrigatórios.")
	_, err = TenantCreate(ctx, env.deps, covCtx(), TenantInput{Name: "Ana"})
	require.EqualError(t, err, "Nome e email são obrigatórios.")
	_, err = TenantCreate(ctx, env.deps, covCtx(), TenantInput{Name: "Ana", Email: "invalido"})
	require.EqualError(t, err, "Informe um e-mail válido.")
	require.Empty(t, env.tenants.items)
	require.Empty(t, env.audit.entries)
}

func TestCovTenantCreatePropagatesFailures(t *testing.T) {
	env := newCovEnv(t)
	env.tenants.createErr = errors.New("cadastro recusado")
	_, err := TenantCreate(context.Background(), env.deps, covCtx(), TenantInput{Name: "Ana", Email: "ana@example.com"})
	require.ErrorIs(t, err, env.tenants.createErr)

	env = newCovEnv(t)
	env.audit.insertErr = errors.New("trilha fora")
	_, err = TenantCreate(context.Background(), env.deps, covCtx(), TenantInput{Name: "Ana", Email: "ana@example.com"})
	require.ErrorIs(t, err, env.audit.insertErr)
	require.Len(t, env.tenants.items, 1, "a falha da trilha não desfaz o cadastro")
}

func TestCovTenantUpdate(t *testing.T) {
	env := newCovEnv(t)
	before := covTenantOf("t1", "Ana")
	before.Phone = domain.StrPtr("(21) 99999-8888")
	env.tenants.items = []domain.Tenant{before}

	updated, err := TenantUpdate(context.Background(), env.deps, covCtx(), TenantInput{
		ID: "t1", Name: "Ana Lima", Email: "ana.lima@example.com", CPF: "12345678900",
	})
	require.NoError(t, err)
	require.Equal(t, "Ana Lima", updated.Name)
	require.Equal(t, "2026-09-21 12:00:00", updated.UpdatedAt)
	require.Equal(t, "123.456.789-00", domain.Str(updated.CPF))
	require.Equal(t, "Ana Lima", env.tenants.items[0].Name)
	require.Equal(t, []string{domain.ActionTenantUpdate}, covAuditActions(env.audit.entries))
	require.Contains(t, *env.audit.entries[0].Changes, "ana.lima@example.com")
}

func TestCovTenantUpdateValidation(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()

	_, err := TenantUpdate(ctx, env.deps, covCtx(), TenantInput{Name: "Ana", Email: "ana@example.com"})
	require.EqualError(t, err, "Nome e email são obrigatórios.")
	_, err = TenantUpdate(ctx, env.deps, covCtx(), TenantInput{ID: "t1", Name: "Ana", Email: "invalido"})
	require.EqualError(t, err, "Informe um e-mail válido.")
}

func TestCovTenantUpdatePropagatesFailures(t *testing.T) {
	input := TenantInput{ID: "t1", Name: "Ana", Email: "ana@example.com"}
	ctx := context.Background()

	env := newCovEnv(t)
	_, err := TenantUpdate(ctx, env.deps, covCtx(), input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "buscar locatário")

	env = newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t1", "Ana")}
	env.tenants.updateErr = errors.New("alteração recusada")
	_, err = TenantUpdate(ctx, env.deps, covCtx(), input)
	require.ErrorIs(t, err, env.tenants.updateErr)

	env = newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t1", "Ana")}
	env.audit.insertErr = errors.New("trilha fora")
	_, err = TenantUpdate(ctx, env.deps, covCtx(), input)
	require.ErrorIs(t, err, env.audit.insertErr)
}

func TestCovTenantDeleteBlocksActiveLease(t *testing.T) {
	env := newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t1", "Ana")}
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 1000)}

	err := TenantDelete(context.Background(), env.deps, covCtx(), "t1")
	require.EqualError(t, err, "Não é possível excluir inquilino com contrato ativo. Encerre o contrato primeiro.")
	require.Empty(t, env.tenants.deleted)
}

func TestCovTenantDelete(t *testing.T) {
	env := newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t1", "Ana")}
	// Contrato encerrado não bloqueia a exclusão.
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusTerminated, "2025-01-01", "2025-12-31", 1000)}

	require.NoError(t, TenantDelete(context.Background(), env.deps, covCtx(), " t1 "))
	require.Equal(t, []string{"t1"}, env.tenants.deleted)
	require.Empty(t, env.tenants.items)
	require.Equal(t, []string{domain.ActionTenantDelete}, covAuditActions(env.audit.entries))
	require.Contains(t, *env.audit.entries[0].Changes, "Ana")
}

func TestCovTenantDeleteValidation(t *testing.T) {
	env := newCovEnv(t)
	require.EqualError(t, TenantDelete(context.Background(), env.deps, covCtx(), "   "), "Registro não informado.")
}

func TestCovTenantDeletePropagatesFailures(t *testing.T) {
	ctx := context.Background()

	env := newCovEnv(t)
	env.leases.byTenantErr = errors.New("contratos fora")
	require.ErrorContains(t, TenantDelete(ctx, env.deps, covCtx(), "t1"), "listar contratos do locatário")

	env = newCovEnv(t)
	require.ErrorContains(t, TenantDelete(ctx, env.deps, covCtx(), "t1"), "buscar locatário")

	env = newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t1", "Ana")}
	env.tenants.delErr = errors.New("exclusão recusada")
	require.ErrorIs(t, TenantDelete(ctx, env.deps, covCtx(), "t1"), env.tenants.delErr)

	env = newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t1", "Ana")}
	env.audit.insertErr = errors.New("trilha fora")
	require.ErrorIs(t, TenantDelete(ctx, env.deps, covCtx(), "t1"), env.audit.insertErr)
}

// ── properties.go ─────────────────────────────────────────────────

func TestCovPropertyCreate(t *testing.T) {
	env := newCovEnv(t)

	property, err := PropertyCreate(context.Background(), env.deps, covCtx(), PropertyInput{
		ID: "p-fixo", Name: " Casa ", Address: " Rua A, 1 ", City: " Niterói ", State: " RJ ", Zip: "24000000",
	})
	require.NoError(t, err)
	require.Equal(t, "p-fixo", property.ID)
	require.Equal(t, "Casa", property.Name)
	require.Equal(t, "24000-000", domain.Str(property.Zip))
	require.Equal(t, []string{domain.ActionPropertyCreate}, covAuditActions(env.audit.entries))

	generated, err := PropertyCreate(context.Background(), env.deps, covCtx(), PropertyInput{Name: "Loja", Address: "Rua B", City: "Niterói", State: "RJ"})
	require.NoError(t, err)
	require.Equal(t, "cov-002", generated.ID)
	require.Nil(t, generated.Zip)
}

func TestCovPropertyCreateValidatesInput(t *testing.T) {
	env := newCovEnv(t)
	_, err := PropertyCreate(context.Background(), env.deps, covCtx(), PropertyInput{Name: "Casa", Address: "Rua A", City: "Niterói"})
	require.EqualError(t, err, "Preencha nome, endereço, cidade e estado.")
	require.Empty(t, env.props.items)
}

func TestCovPropertyCreatePropagatesFailures(t *testing.T) {
	input := PropertyInput{Name: "Casa", Address: "Rua A", City: "Niterói", State: "RJ"}
	ctx := context.Background()

	env := newCovEnv(t)
	env.props.createErr = errors.New("cadastro recusado")
	_, err := PropertyCreate(ctx, env.deps, covCtx(), input)
	require.ErrorIs(t, err, env.props.createErr)

	env = newCovEnv(t)
	env.audit.insertErr = errors.New("trilha fora")
	_, err = PropertyCreate(ctx, env.deps, covCtx(), input)
	require.ErrorIs(t, err, env.audit.insertErr)
}

func TestCovPropertyUpdate(t *testing.T) {
	env := newCovEnv(t)
	env.props.items = []domain.Property{covPropertyOf("p1", "Casa")}

	updated, err := PropertyUpdate(context.Background(), env.deps, covCtx(), PropertyInput{
		ID: "p1", Name: "Casa da Praia", Address: "Rua A, 1", City: "Niterói", State: "RJ", Zip: "24000000",
	})
	require.NoError(t, err)
	require.Equal(t, "Casa da Praia", updated.Name)
	require.Equal(t, "24000-000", domain.Str(updated.Zip))
	require.Equal(t, "2026-09-21 12:00:00", updated.UpdatedAt)
	require.Equal(t, []string{domain.ActionPropertyUpdate}, covAuditActions(env.audit.entries))
}

func TestCovPropertyUpdateValidation(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()

	_, err := PropertyUpdate(ctx, env.deps, covCtx(), PropertyInput{Name: "Casa", Address: "Rua", City: "Niterói", State: "RJ"})
	require.EqualError(t, err, "Preencha nome, endereço, cidade e estado.")
	_, err = PropertyUpdate(ctx, env.deps, covCtx(), PropertyInput{ID: "p1", Name: "Casa"})
	require.EqualError(t, err, "Preencha nome, endereço, cidade e estado.")
}

func TestCovPropertyUpdatePropagatesFailures(t *testing.T) {
	input := PropertyInput{ID: "p1", Name: "Casa", Address: "Rua A", City: "Niterói", State: "RJ"}
	ctx := context.Background()

	env := newCovEnv(t)
	_, err := PropertyUpdate(ctx, env.deps, covCtx(), input)
	require.ErrorContains(t, err, "buscar imóvel")

	env = newCovEnv(t)
	env.props.items = []domain.Property{covPropertyOf("p1", "Casa")}
	env.props.updateErr = errors.New("alteração recusada")
	_, err = PropertyUpdate(ctx, env.deps, covCtx(), input)
	require.ErrorIs(t, err, env.props.updateErr)

	env = newCovEnv(t)
	env.props.items = []domain.Property{covPropertyOf("p1", "Casa")}
	env.audit.insertErr = errors.New("trilha fora")
	_, err = PropertyUpdate(ctx, env.deps, covCtx(), input)
	require.ErrorIs(t, err, env.audit.insertErr)
}

func TestCovPropertyDelete(t *testing.T) {
	env := newCovEnv(t)

	require.EqualError(t, PropertyDelete(context.Background(), env.deps, covCtx(), " "), "Registro não informado.")

	env.props.items = []domain.Property{covPropertyOf("p1", "Casa")}
	require.NoError(t, PropertyDelete(context.Background(), env.deps, covCtx(), "p1"))
	require.Equal(t, []string{"p1"}, env.props.deleted)
	require.Equal(t, []string{domain.ActionPropertyDelete}, covAuditActions(env.audit.entries))
}

func TestCovPropertyDeletePropagatesFailures(t *testing.T) {
	ctx := context.Background()

	env := newCovEnv(t)
	require.ErrorContains(t, PropertyDelete(ctx, env.deps, covCtx(), "p1"), "buscar imóvel")

	env = newCovEnv(t)
	env.props.items = []domain.Property{covPropertyOf("p1", "Casa")}
	env.props.delErr = errors.New("exclusão recusada")
	require.ErrorIs(t, PropertyDelete(ctx, env.deps, covCtx(), "p1"), env.props.delErr)

	env = newCovEnv(t)
	env.props.items = []domain.Property{covPropertyOf("p1", "Casa")}
	env.audit.insertErr = errors.New("trilha fora")
	require.ErrorIs(t, PropertyDelete(ctx, env.deps, covCtx(), "p1"), env.audit.insertErr)
}

// ── leases.go ─────────────────────────────────────────────────────

func TestCovLeaseCreate(t *testing.T) {
	env := newCovEnv(t)

	lease, err := LeaseCreate(context.Background(), env.deps, covCtx(), LeaseInput{
		ID: "l-fixo", TenantID: "t1", PropertyID: "p1",
		StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 1500,
	})
	require.NoError(t, err)
	require.Equal(t, "l-fixo", lease.ID)
	require.Equal(t, domain.LeaseStatusActive, lease.Status, "status vazio na criação vira ativo")
	require.Equal(t, []string{domain.ActionLeaseCreate}, covAuditActions(env.audit.entries))
	require.Len(t, env.leases.items, 1)
}

func TestCovLeaseCreateRejectsOverlap(t *testing.T) {
	env := newCovEnv(t)
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-06-30", 1000)}
	ctx := context.Background()

	_, err := LeaseCreate(ctx, env.deps, covCtx(), LeaseInput{
		TenantID: "t2", PropertyID: "p1", StartDate: "2026-05-01", EndDate: "2026-08-31", DueDay: 5, RentAmount: 900,
	})
	require.EqualError(t, err, "Este imóvel já possui contrato vigente no período.")

	_, err = LeaseCreate(ctx, env.deps, covCtx(), LeaseInput{
		TenantID: "t2", PropertyID: "p1", StartDate: "2026-05-01", EndDate: "2026-08-31", DueDay: 5, RentAmount: 900,
		Status: domain.LeaseStatusSeason,
	})
	require.EqualError(t, err, "Este imóvel já possui contrato vigente no período. Temporada só pode ser criada em imóvel sem contrato ativo ou com contrato suspenso.")

	// Período fora da vigência existente entra normalmente.
	_, err = LeaseCreate(ctx, env.deps, covCtx(), LeaseInput{
		TenantID: "t2", PropertyID: "p1", StartDate: "2026-07-01", EndDate: "2026-12-31", DueDay: 5, RentAmount: 900,
	})
	require.NoError(t, err)
}

func TestCovLeaseCreateValidation(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()

	_, err := LeaseCreate(ctx, env.deps, covCtx(), LeaseInput{
		TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 1000,
		Status: domain.LeaseStatusTerminated,
	})
	require.EqualError(t, err, "Status de contrato inválido.")

	_, err = LeaseCreate(ctx, env.deps, covCtx(), LeaseInput{PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 1000})
	require.EqualError(t, err, "Preencha todos os campos do contrato.")

	_, err = LeaseCreate(ctx, env.deps, covCtx(), LeaseInput{TenantID: "t1", PropertyID: "p1", DueDay: 10, RentAmount: 1000})
	require.EqualError(t, err, "Informe o período do contrato.")

	_, err = LeaseCreate(ctx, env.deps, covCtx(), LeaseInput{TenantID: "t1", PropertyID: "p1", StartDate: "2026-12-31", EndDate: "2026-01-01", DueDay: 10, RentAmount: 1000})
	require.EqualError(t, err, "A data de término deve ser igual ou posterior à data de início.")

	_, err = LeaseCreate(ctx, env.deps, covCtx(), LeaseInput{TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 40, RentAmount: 1000})
	require.EqualError(t, err, "Informe um dia de vencimento entre 1 e 31.")

	_, err = LeaseCreate(ctx, env.deps, covCtx(), LeaseInput{TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10})
	require.EqualError(t, err, "Informe o valor do aluguel.")
	require.Empty(t, env.leases.items)
}

// covLeasesOverlapErr injeta falha na checagem de sobreposição.
type covLeasesOverlapErr struct {
	*covLeases
	err error
}

func (r covLeasesOverlapErr) Overlapping(context.Context, string, string, string, string) ([]domain.Lease, error) {
	return nil, r.err
}

func TestCovLeaseCreatePropagatesFailures(t *testing.T) {
	input := LeaseInput{TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 1000}
	ctx := context.Background()

	env := newCovEnv(t)
	env.deps.Repos.Leases = covLeasesOverlapErr{covLeases: env.leases, err: errors.New("consulta fora")}
	_, err := LeaseCreate(ctx, env.deps, covCtx(), input)
	require.ErrorContains(t, err, "verificar sobreposição de contrato")

	env = newCovEnv(t)
	env.leases.createErr = errors.New("cadastro recusado")
	_, err = LeaseCreate(ctx, env.deps, covCtx(), input)
	require.ErrorIs(t, err, env.leases.createErr)

	env = newCovEnv(t)
	env.leases.byIDErr = errors.New("leitura fora")
	_, err = LeaseCreate(ctx, env.deps, covCtx(), input)
	require.ErrorContains(t, err, "buscar contrato")

	env = newCovEnv(t)
	env.audit.insertErr = errors.New("trilha fora")
	_, err = LeaseCreate(ctx, env.deps, covCtx(), input)
	require.ErrorIs(t, err, env.audit.insertErr)
}

func TestCovLeaseUpdate(t *testing.T) {
	env := newCovEnv(t)
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-06-30", 1000)}
	ctx := context.Background()

	updated, err := LeaseUpdate(ctx, env.deps, covCtx(), LeaseInput{
		ID: "l1", TenantID: "t2", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 15, RentAmount: 1800,
	})
	require.NoError(t, err)
	require.Equal(t, "t2", updated.TenantID)
	require.Equal(t, 1800.0, updated.RentAmount)
	require.Equal(t, domain.LeaseStatusActive, updated.Status, "status em branco preserva a situação atual")

	// O próprio contrato é ignorado na checagem de sobreposição.
	require.Equal(t, []string{domain.ActionLeaseUpdate}, covAuditActions(env.audit.entries))
}

func TestCovLeaseUpdateValidation(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()

	_, err := LeaseUpdate(ctx, env.deps, covCtx(), LeaseInput{TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 1000})
	require.EqualError(t, err, "Preencha todos os campos do contrato.")

	_, err = LeaseUpdate(ctx, env.deps, covCtx(), LeaseInput{ID: "l1", TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 1000, Status: "invalido"})
	require.EqualError(t, err, "Status de contrato inválido.")

	_, err = LeaseUpdate(ctx, env.deps, covCtx(), LeaseInput{ID: "l1", DueDay: 10, RentAmount: 1000})
	require.EqualError(t, err, "Preencha todos os campos do contrato.")
}

func TestCovLeaseUpdatePropagatesFailures(t *testing.T) {
	input := LeaseInput{ID: "l1", TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 1000}
	ctx := context.Background()

	env := newCovEnv(t)
	_, err := LeaseUpdate(ctx, env.deps, covCtx(), input)
	require.ErrorContains(t, err, "buscar contrato")

	env = newCovEnv(t)
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-06-30", 1000)}
	env.leases.updateErr = errors.New("alteração recusada")
	_, err = LeaseUpdate(ctx, env.deps, covCtx(), input)
	require.ErrorIs(t, err, env.leases.updateErr)

	env = newCovEnv(t)
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-06-30", 1000)}
	env.deps.Repos.Leases = covLeasesOverlapErr{covLeases: env.leases, err: errors.New("consulta fora")}
	_, err = LeaseUpdate(ctx, env.deps, covCtx(), input)
	require.ErrorContains(t, err, "verificar sobreposição de contrato")

	env = newCovEnv(t)
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-06-30", 1000)}
	env.audit.insertErr = errors.New("trilha fora")
	_, err = LeaseUpdate(ctx, env.deps, covCtx(), input)
	require.ErrorIs(t, err, env.audit.insertErr)
}

func TestCovLeaseSetStatusTransitions(t *testing.T) {
	cases := map[string]struct {
		intent string
		from   domain.LeaseStatus
		want   domain.LeaseStatus
		label  string
	}{
		"encerrar":  {domain.ActionLeaseTerminate, domain.LeaseStatusActive, domain.LeaseStatusTerminated, "encerrado"},
		"suspender": {domain.ActionLeaseSuspend, domain.LeaseStatusActive, domain.LeaseStatusSuspended, "suspenso"},
		"reativar":  {domain.ActionLeaseReactivate, domain.LeaseStatusSuspended, domain.LeaseStatusActive, "reativado"},
	}
	for _, scenario := range cases {
		intent := scenario.intent
		t.Run(scenario.label, func(t *testing.T) {
			env := newCovEnv(t)
			env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", scenario.from, "2026-01-01", "2026-12-31", 1000)}

			require.NoError(t, LeaseSetStatus(context.Background(), env.deps, covCtx(), intent, "l1"))
			require.Equal(t, scenario.want, env.leases.items[0].Status)
			require.Equal(t, string(scenario.want), env.leases.statusUpdates[0].Value)
			require.Equal(t, []string{intent}, covAuditActions(env.audit.entries))
			require.Contains(t, env.audit.entries[0].Summary, scenario.label)
		})
	}
}

func TestCovLeaseSetStatusWithoutChangeDoesNotAudit(t *testing.T) {
	env := newCovEnv(t)
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 1000)}

	// Reativar um contrato já ativo não muda nada: nenhuma linha na trilha.
	require.NoError(t, LeaseSetStatus(context.Background(), env.deps, covCtx(), domain.ActionLeaseReactivate, "l1"))
	require.Empty(t, env.audit.entries)
}

func TestCovLeaseSetStatusValidation(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()

	require.EqualError(t, LeaseSetStatus(ctx, env.deps, covCtx(), "lease.apagar", "l1"), "Ação inválida.")
	require.EqualError(t, LeaseSetStatus(ctx, env.deps, covCtx(), domain.ActionLeaseTerminate, "  "), "Contrato não informado.")
	require.ErrorContains(t, LeaseSetStatus(ctx, env.deps, covCtx(), domain.ActionLeaseTerminate, "l1"), "buscar contrato")
}

func TestCovLeaseSetStatusPropagatesFailures(t *testing.T) {
	ctx := context.Background()

	env := newCovEnv(t)
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 1000)}
	env.leases.statusErr = errors.New("transição recusada")
	require.ErrorIs(t, LeaseSetStatus(ctx, env.deps, covCtx(), domain.ActionLeaseSuspend, "l1"), env.leases.statusErr)

	env = newCovEnv(t)
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 1000)}
	env.audit.insertErr = errors.New("trilha fora")
	require.ErrorIs(t, LeaseSetStatus(ctx, env.deps, covCtx(), domain.ActionLeaseTerminate, "l1"), env.audit.insertErr)
}

func TestCovLeaseDelete(t *testing.T) {
	env := newCovEnv(t)
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 1000)}
	ctx := context.Background()

	require.EqualError(t, LeaseDelete(ctx, env.deps, covCtx(), ""), "Contrato não informado.")
	require.ErrorContains(t, LeaseDelete(ctx, env.deps, covCtx(), "l2"), "buscar contrato")

	require.NoError(t, LeaseDelete(ctx, env.deps, covCtx(), "l1"))
	require.Equal(t, []string{"l1"}, env.leases.deleted)
	require.Equal(t, []string{domain.ActionLeaseDelete}, covAuditActions(env.audit.entries))
	require.Contains(t, env.audit.entries[0].Summary, "excluído")
}

func TestCovLeaseDeletePropagatesFailures(t *testing.T) {
	ctx := context.Background()

	env := newCovEnv(t)
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 1000)}
	env.leases.delErr = errors.New("exclusão recusada")
	require.ErrorIs(t, LeaseDelete(ctx, env.deps, covCtx(), "l1"), env.leases.delErr)

	env = newCovEnv(t)
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 1000)}
	env.audit.insertErr = errors.New("trilha fora")
	require.ErrorIs(t, LeaseDelete(ctx, env.deps, covCtx(), "l1"), env.audit.insertErr)
}

// ── receipts.go ───────────────────────────────────────────────────

func covReceiptEnv(t *testing.T) *covEnv {
	t.Helper()
	env := newCovEnv(t)
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 1000)}
	return env
}

func TestCovReceiptCreateWithLease(t *testing.T) {
	env := covReceiptEnv(t)
	ctx := context.Background()

	receipt, err := ReceiptCreate(ctx, env.deps, covCtx(), ReceiptInput{
		ID: "r-fixo", LeaseID: "l1", DueDate: "2026-09-10", Amount: "1000",
	})
	require.NoError(t, err)
	require.Equal(t, "r-fixo", receipt.ID)
	require.Equal(t, domain.ReceiptCategoryRent, receipt.Category, "categoria ausente com contrato vira aluguel")
	require.Equal(t, "t1", domain.Str(receipt.TenantID), "o contrato manda no inquilino")
	require.Equal(t, "p1", domain.Str(receipt.PropertyID))
	require.Equal(t, "2026-09-21 12:00:00", receipt.CreatedAt)
	require.Equal(t, []string{domain.ActionReceiptCreate}, covAuditActions(env.audit.entries))
}

func TestCovReceiptCreateExtraAndPaid(t *testing.T) {
	env := covReceiptEnv(t)
	ctx := context.Background()

	extra, err := ReceiptCreate(ctx, env.deps, covCtx(), ReceiptInput{
		TenantID: "t9", Category: "extra", Description: " Taxa de limpeza ", DueDate: "2026-09-10", Amount: "250,50",
	})
	require.NoError(t, err)
	require.Equal(t, domain.ReceiptCategoryExtra, extra.Category)
	require.Equal(t, "Taxa de limpeza", domain.Str(extra.Description))
	require.Equal(t, 250.5, extra.Amount)

	// Valor pago sem data preenche a data de agora; data de pagamento sem valor
	// assume o total.
	paid, err := ReceiptCreate(ctx, env.deps, covCtx(), ReceiptInput{
		LeaseID: "l1", DueDate: "2026-09-10", Amount: "1000", PaidAmount: "400",
	})
	require.NoError(t, err)
	require.Equal(t, "2026-09-21T12:00", domain.Str(paid.PaidAt))
	require.Equal(t, 400.0, domain.Float(paid.PaidAmount))

	quitado, err := ReceiptCreate(ctx, env.deps, covCtx(), ReceiptInput{
		LeaseID: "l1", DueDate: "2026-09-10", Amount: "1000", PaidAt: "2026-09-09T10:00",
	})
	require.NoError(t, err)
	require.Equal(t, 1000.0, domain.Float(quitado.PaidAmount))
}

func TestCovReceiptCreateValidation(t *testing.T) {
	env := covReceiptEnv(t)
	ctx := context.Background()

	_, err := ReceiptCreate(ctx, env.deps, covCtx(), ReceiptInput{LeaseID: "l1", Amount: "1000"})
	require.EqualError(t, err, "Preencha vencimento e valor.")
	_, err = ReceiptCreate(ctx, env.deps, covCtx(), ReceiptInput{LeaseID: "l1", DueDate: "2026-09-10", Amount: "abc"})
	require.EqualError(t, err, "Preencha vencimento e valor.")
	_, err = ReceiptCreate(ctx, env.deps, covCtx(), ReceiptInput{Category: "rent", DueDate: "2026-09-10", Amount: "1000"})
	require.EqualError(t, err, "Selecione um contrato para recebimento de aluguel.")
	_, err = ReceiptCreate(ctx, env.deps, covCtx(), ReceiptInput{Category: "extra", DueDate: "2026-09-10", Amount: "1000"})
	require.EqualError(t, err, "Informe a descrição para cobrança avulsa.")
	require.Empty(t, env.receipts.items)
}

func TestCovReceiptCreatePropagatesFailures(t *testing.T) {
	input := ReceiptInput{LeaseID: "l1", DueDate: "2026-09-10", Amount: "1000"}
	ctx := context.Background()

	env := covReceiptEnv(t)
	input.LeaseID = "l9"
	_, err := ReceiptCreate(ctx, env.deps, covCtx(), input)
	require.ErrorContains(t, err, "buscar contrato do lançamento")

	env = covReceiptEnv(t)
	input.LeaseID = "l1"
	env.receipts.createErr = errors.New("lançamento recusado")
	_, err = ReceiptCreate(ctx, env.deps, covCtx(), input)
	require.ErrorIs(t, err, env.receipts.createErr)

	env = covReceiptEnv(t)
	env.receipts.byIDErr = errors.New("leitura fora")
	_, err = ReceiptCreate(ctx, env.deps, covCtx(), input)
	require.ErrorContains(t, err, "buscar lançamento criado")

	env = covReceiptEnv(t)
	env.audit.insertErr = errors.New("trilha fora")
	_, err = ReceiptCreate(ctx, env.deps, covCtx(), input)
	require.ErrorIs(t, err, env.audit.insertErr)
}

func TestCovReceiptUpdate(t *testing.T) {
	env := covReceiptEnv(t)
	env.receipts.items = []domain.Receipt{covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)}
	ctx := context.Background()

	updated, err := ReceiptUpdate(ctx, env.deps, covCtx(), ReceiptInput{
		ID: "r1", LeaseID: "l1", DueDate: "2026-09-15", Amount: "1200", Description: "Aluguel de setembro",
	})
	require.NoError(t, err)
	require.Equal(t, 1200.0, updated.Amount)
	require.Equal(t, "2026-09-15", updated.DueDate)
	require.Equal(t, "2026-09-21 12:00:00", domain.Str(updated.UpdatedAt))
	require.Equal(t, []string{domain.ActionReceiptUpdate}, covAuditActions(env.audit.entries))

	_, err = ReceiptUpdate(ctx, env.deps, covCtx(), ReceiptInput{DueDate: "2026-09-15", Amount: "1200"})
	require.EqualError(t, err, "Registro não informado.")
}

func TestCovReceiptUpdatePropagatesFailures(t *testing.T) {
	input := ReceiptInput{ID: "r1", LeaseID: "l1", DueDate: "2026-09-15", Amount: "1200"}
	ctx := context.Background()

	env := covReceiptEnv(t)
	input.LeaseID = "l9"
	_, err := ReceiptUpdate(ctx, env.deps, covCtx(), input)
	require.ErrorContains(t, err, "buscar contrato do lançamento")

	env = covReceiptEnv(t)
	input.LeaseID = "l1"
	env.receipts.byIDErr = errors.New("leitura fora")
	_, err = ReceiptUpdate(ctx, env.deps, covCtx(), input)
	require.ErrorContains(t, err, "buscar lançamento")

	env = covReceiptEnv(t)
	env.receipts.items = []domain.Receipt{covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)}
	env.receipts.updateErr = errors.New("alteração recusada")
	_, err = ReceiptUpdate(ctx, env.deps, covCtx(), input)
	require.ErrorIs(t, err, env.receipts.updateErr)

	env = covReceiptEnv(t)
	env.receipts.items = []domain.Receipt{covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)}
	env.audit.insertErr = errors.New("trilha fora")
	_, err = ReceiptUpdate(ctx, env.deps, covCtx(), input)
	require.ErrorIs(t, err, env.audit.insertErr)
}

func TestCovReceiptPay(t *testing.T) {
	env := covReceiptEnv(t)
	env.receipts.items = []domain.Receipt{covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)}
	ctx := context.Background()

	paid, err := ReceiptPay(ctx, env.deps, covCtx(), ReceiptPayInput{ID: "r1"})
	require.NoError(t, err)
	require.Equal(t, 1000.0, domain.Float(paid.PaidAmount), "sem valor informado a baixa é pelo total")
	require.Equal(t, "2026-09-21T12:00", domain.Str(paid.PaidAt))
	require.Equal(t, []string{domain.ActionReceiptPay}, covAuditActions(env.audit.entries))

	// Baixa parcial com valor e data informados.
	env.receipts.items = []domain.Receipt{covReceiptOf("r2", "t1", "l1", "2026-09-10", 1000)}
	partial, err := ReceiptPay(ctx, env.deps, covCtx(), ReceiptPayInput{ID: "r2", PayAmount: "300", PaidAt: "2026-09-09T08:00"})
	require.NoError(t, err)
	require.Equal(t, 300.0, domain.Float(partial.PaidAmount))
	require.Equal(t, "2026-09-09T08:00", domain.Str(partial.PaidAt))

	_, err = ReceiptPay(ctx, env.deps, covCtx(), ReceiptPayInput{})
	require.EqualError(t, err, "Registro não informado.")
	_, err = ReceiptPay(ctx, env.deps, covCtx(), ReceiptPayInput{ID: "r2", PayAmount: "abc"})
	require.EqualError(t, err, "Informe o valor pago.")
}

func TestCovReceiptPayPropagatesFailures(t *testing.T) {
	ctx := context.Background()

	env := covReceiptEnv(t)
	_, err := ReceiptPay(ctx, env.deps, covCtx(), ReceiptPayInput{ID: "r1"})
	require.ErrorContains(t, err, "buscar lançamento")

	env = covReceiptEnv(t)
	env.receipts.items = []domain.Receipt{covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)}
	env.receipts.payErr = errors.New("baixa recusada")
	_, err = ReceiptPay(ctx, env.deps, covCtx(), ReceiptPayInput{ID: "r1"})
	require.ErrorIs(t, err, env.receipts.payErr)

	env = covReceiptEnv(t)
	env.receipts.items = []domain.Receipt{covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)}
	env.audit.insertErr = errors.New("trilha fora")
	_, err = ReceiptPay(ctx, env.deps, covCtx(), ReceiptPayInput{ID: "r1"})
	require.ErrorIs(t, err, env.audit.insertErr)
}

func TestCovReceiptBatchPay(t *testing.T) {
	env := covReceiptEnv(t)
	env.receipts.items = []domain.Receipt{
		covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000),
		covReceiptOf("r2", "t1", "l1", "2026-10-10", 1000),
	}
	ctx := context.Background()

	require.EqualError(t, ReceiptBatchPay(ctx, env.deps, covCtx(), []string{"  ", ""}), "Nenhum recibo selecionado.")

	// Id desconhecido interrompe o lote sem deixar baixa pela metade.
	require.ErrorContains(t, ReceiptBatchPay(ctx, env.deps, covCtx(), []string{"r1", "r9"}), "buscar lançamento do lote")
	require.Empty(t, env.receipts.payments)
	require.Empty(t, env.audit.entries)

	require.NoError(t, ReceiptBatchPay(ctx, env.deps, covCtx(), []string{"r1", " r2 "}))
	require.Len(t, env.receipts.payments, 2)
	require.Len(t, env.audit.entries, 2)
	require.Contains(t, *env.audit.entries[0].Metadata, `"batch":2`)
	require.Equal(t, 1000.0, domain.Float(env.receipts.items[1].PaidAmount))
}

func TestCovReceiptBatchPayPropagatesFailures(t *testing.T) {
	ctx := context.Background()

	env := covReceiptEnv(t)
	env.receipts.items = []domain.Receipt{covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)}
	env.receipts.payErr = errors.New("baixa recusada")
	require.ErrorContains(t, ReceiptBatchPay(ctx, env.deps, covCtx(), []string{"r1"}), "dar baixa no lote")

	env = covReceiptEnv(t)
	env.receipts.items = []domain.Receipt{covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)}
	env.audit.insertErr = errors.New("trilha fora")
	require.ErrorIs(t, ReceiptBatchPay(ctx, env.deps, covCtx(), []string{"r1"}), env.audit.insertErr)
}

func TestCovReceiptGenerateMonthly(t *testing.T) {
	env := covReceiptEnv(t)
	env.leases.items = append(env.leases.items,
		covLeaseOf("l2", "t2", "p2", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 2000),
		covLeaseOf("l3", "t3", "p3", domain.LeaseStatusTerminated, "2026-01-01", "2026-12-31", 500),
	)
	ctx := context.Background()

	created, err := ReceiptGenerateMonthly(ctx, env.deps, covCtx())
	require.NoError(t, err)
	require.Equal(t, 2, created, "só contratos ativos vigentes no mês geram aluguel")
	require.Len(t, env.receipts.items, 2)
	require.Equal(t, "2026-09-10", env.receipts.items[0].DueDate)
	require.Equal(t, "l1:2026-09", domain.Str(env.receipts.items[0].GeneratedKey))

	// Repetir não duplica: a chave de geração torna a rotina idempotente.
	created, err = ReceiptGenerateMonthly(ctx, env.deps, covCtx())
	require.NoError(t, err)
	require.Equal(t, 0, created)
	require.Len(t, env.receipts.items, 2)
	require.Equal(t, []string{domain.ActionReceiptGenerateMonth, domain.ActionReceiptGenerateMonth}, covAuditActions(env.audit.entries))
	require.Contains(t, env.audit.entries[1].Summary, "0 pagamento")
}

func TestCovReceiptGenerateMonthlyPropagatesFailures(t *testing.T) {
	ctx := context.Background()

	env := covReceiptEnv(t)
	env.leases.listErr = errors.New("contratos fora")
	_, err := ReceiptGenerateMonthly(ctx, env.deps, covCtx())
	require.ErrorContains(t, err, "listar contratos vigentes no mês")

	env = covReceiptEnv(t)
	env.receipts.createErr = errors.New("lançamento recusado")
	_, err = ReceiptGenerateMonthly(ctx, env.deps, covCtx())
	require.ErrorContains(t, err, "gerar pagamento do mês")

	env = covReceiptEnv(t)
	env.audit.insertErr = errors.New("trilha fora")
	created, err := ReceiptGenerateMonthly(ctx, env.deps, covCtx())
	require.ErrorIs(t, err, env.audit.insertErr)
	require.Equal(t, 1, created, "os lançamentos criados antes da falha da trilha são informados")
}

func TestCovReceiptDelete(t *testing.T) {
	env := covReceiptEnv(t)
	env.receipts.items = []domain.Receipt{covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)}
	ctx := context.Background()

	require.EqualError(t, ReceiptDelete(ctx, env.deps, covCtx(), ""), "Registro não informado.")

	require.NoError(t, ReceiptDelete(ctx, env.deps, covCtx(), "r1"))
	require.Equal(t, []string{"r1"}, env.receipts.deleted)
	require.Equal(t, []string{domain.ActionReceiptDelete}, covAuditActions(env.audit.entries))
}

func TestCovReceiptDeletePropagatesFailures(t *testing.T) {
	ctx := context.Background()

	env := covReceiptEnv(t)
	require.ErrorContains(t, ReceiptDelete(ctx, env.deps, covCtx(), "r1"), "buscar lançamento")

	env = covReceiptEnv(t)
	env.receipts.items = []domain.Receipt{covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)}
	env.receipts.delErr = errors.New("exclusão recusada")
	require.ErrorIs(t, ReceiptDelete(ctx, env.deps, covCtx(), "r1"), env.receipts.delErr)

	env = covReceiptEnv(t)
	env.receipts.items = []domain.Receipt{covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)}
	env.audit.insertErr = errors.New("trilha fora")
	require.ErrorIs(t, ReceiptDelete(ctx, env.deps, covCtx(), "r1"), env.audit.insertErr)
}

func TestCovReceiptBatchDelete(t *testing.T) {
	env := covReceiptEnv(t)
	env.receipts.items = []domain.Receipt{
		covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000),
		covReceiptOf("r2", "t1", "l1", "2026-10-10", 1000),
	}
	ctx := context.Background()

	require.EqualError(t, ReceiptBatchDelete(ctx, env.deps, covCtx(), nil), "Nenhum recibo selecionado.")
	require.ErrorContains(t, ReceiptBatchDelete(ctx, env.deps, covCtx(), []string{"r9"}), "buscar lançamento do lote")

	require.NoError(t, ReceiptBatchDelete(ctx, env.deps, covCtx(), []string{"r1", "r2"}))
	require.Empty(t, env.receipts.items)
	require.Len(t, env.audit.entries, 2)
	require.Contains(t, env.audit.entries[0].Summary, "excluído em lote")
}

func TestCovReceiptBatchDeletePropagatesFailures(t *testing.T) {
	ctx := context.Background()

	env := covReceiptEnv(t)
	env.receipts.items = []domain.Receipt{covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)}
	env.receipts.delErr = errors.New("exclusão recusada")
	require.ErrorContains(t, ReceiptBatchDelete(ctx, env.deps, covCtx(), []string{"r1"}), "excluir lançamento do lote")

	env = covReceiptEnv(t)
	env.receipts.items = []domain.Receipt{covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)}
	env.audit.insertErr = errors.New("trilha fora")
	require.ErrorIs(t, ReceiptBatchDelete(ctx, env.deps, covCtx(), []string{"r1"}), env.audit.insertErr)
}

func TestCovReceiptCategoryAndMoneyParsing(t *testing.T) {
	require.Equal(t, domain.ReceiptCategoryRent, receiptCategory("rent", ""))
	require.Equal(t, domain.ReceiptCategoryExtra, receiptCategory("extra", "l1"))
	// Categoria de temporada informada explicitamente é rebaixada para aluguel
	// quando há contrato: o mapa só conhece aluguel e avulsa (relatado).
	require.Equal(t, domain.ReceiptCategoryRent, receiptCategory(" cleaning ", "l1"))
	require.Equal(t, domain.ReceiptCategoryRent, receiptCategory("", "l1"), "sem categoria, o contrato decide")
	require.Equal(t, domain.ReceiptCategoryExtra, receiptCategory("desconhecida", ""), "categoria desconhecida sem contrato vira avulsa")

	value, ok := receiptMoney("")
	require.True(t, ok)
	require.Equal(t, 0.0, value, "campo vazio vale zero")
	value, ok = receiptMoney("1.234,56")
	require.True(t, ok)
	require.Equal(t, 1234.56, value)
	value, ok = receiptMoney("R$ 2.500,00")
	require.True(t, ok)
	require.Equal(t, 2500.0, value)
	_, ok = receiptMoney("abc")
	require.False(t, ok)
	_, ok = receiptMoney(strings.Repeat("9", 400))
	require.False(t, ok, "valor fora da faixa de float é recusado")

	require.Nil(t, receiptOptionalMoney("  "))
	require.Nil(t, receiptOptionalMoney("ilegível"))
	require.Equal(t, 350.0, domain.Float(receiptOptionalMoney("350")))

	require.Equal(t, "2026-02-28", monthEndKey(2026, 2))
	require.Equal(t, "2028-02-29", monthEndKey(2028, 2))
	require.Equal(t, "2026-04-30", monthEndKey(2026, 4))
}

// ── client.go ─────────────────────────────────────────────────────

func TestCovLoadClientArea(t *testing.T) {
	env := newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t1", "Ana")}
	env.props.items = []domain.Property{covPropertyOf("p1", "Casa")}
	first := covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-03-01", "2027-02-28", 1500)
	first.TenantName = "Ana"
	first.PropertyName = "Casa"
	season := covLeaseOf("l2", "t1", "p9", domain.LeaseStatusSeason, "2026-07-01", "2026-07-31", 300)
	season.TenantName = "Ana"
	env.leases.items = []domain.Lease{first, season}
	quitado := covPaidReceipt(covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000), "2026-09-09T10:30", 1000)
	partial := covPaidReceipt(covReceiptOf("r2", "t1", "l1", "2026-10-10", 1000), "2026-10-09T10:30", 400)
	avulsa := covReceiptOf("r3", "t1", "", "2026-08-10", 80)
	avulsa.Category = domain.ReceiptCategoryExtra
	avulsa.Description = domain.StrPtr("Taxa de limpeza")
	avulsa.PaidAt = domain.StrPtr("2026-08-09T10:30")
	aberta := covReceiptOf("r4", "t1", "l1", "2026-11-10", 1000)
	env.receipts.items = []domain.Receipt{quitado, partial, avulsa, aberta}

	area, err := LoadClientArea(context.Background(), env.deps, "t1")
	require.NoError(t, err)
	require.Equal(t, "Ana", area.Tenant.Name)

	require.Len(t, area.Leases, 2)
	lease := area.Leases[0]
	require.Equal(t, "Casa", lease.PropertyName)
	require.Equal(t, "Rua A, 1, Niterói, RJ", lease.PropertyAddress)
	require.Equal(t, "01/03/2026 a 28/02/2027", lease.Period)
	require.Equal(t, "R$ 1.500,00", lease.RentAmount)
	require.Equal(t, "Ativo", lease.StatusLabel)
	require.Equal(t, "paid", lease.StatusClass)
	require.Equal(t, "/contract-pdf?id=l1", lease.PDFURL)
	require.Contains(t, lease.ValidationURL, "l1")

	require.Equal(t, "partial", area.Leases[1].StatusClass, "temporada usa a classe parcial")
	require.Equal(t, "", area.Leases[1].PropertyAddress, "imóvel sumido não quebra o portal")

	require.Len(t, area.Receipts, 3, "lançamento sem baixa não aparece no portal")
	require.Equal(t, "Aluguel 09/2026", area.Receipts[0].Reference)
	require.Equal(t, "10/09/2026", area.Receipts[0].DueDate)
	require.Equal(t, "09/09/2026 10:30", area.Receipts[0].PaidAt)
	require.Equal(t, "R$ 1.000,00", area.Receipts[0].AmountPaid)
	require.False(t, area.Receipts[0].IsPartial)
	require.True(t, area.Receipts[1].IsPartial)
	require.Equal(t, "R$ 400,00", area.Receipts[1].AmountPaid)
	require.Equal(t, "Taxa de limpeza", area.Receipts[2].Reference, "cobrança avulsa usa a descrição")
	require.Equal(t, "/pdf?id=r1", area.Receipts[0].PDFURL)
}

func TestCovLoadClientAreaUnauthorizedWhenTenantIsGone(t *testing.T) {
	env := newCovEnv(t)

	area, err := LoadClientArea(context.Background(), env.deps, "t1")
	require.ErrorIs(t, err, domain.ErrUnauthorized)
	require.Nil(t, area)
}

func TestCovLoadClientAreaPropagatesFailures(t *testing.T) {
	ctx := context.Background()

	env := newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t1", "Ana")}
	env.leases.byTenantErr = errors.New("contratos fora")
	_, err := LoadClientArea(ctx, env.deps, "t1")
	require.ErrorContains(t, err, "listar contratos do locatário")

	env = newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t1", "Ana")}
	env.receipts.byTenantErr = errors.New("recibos fora")
	_, err = LoadClientArea(ctx, env.deps, "t1")
	require.ErrorContains(t, err, "listar recibos do locatário")

	// Sem emissor de tokens o portal não consegue montar o link de validação.
	env = newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t1", "Ana")}
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 1000)}
	env.deps.Signer = nil
	_, err = LoadClientArea(ctx, env.deps, "t1")
	require.ErrorContains(t, err, "montar o link de validação do documento l1")
}

func TestCovClientLeaseStatusClass(t *testing.T) {
	require.Equal(t, "paid", clientLeaseStatusClass(domain.LeaseStatusActive))
	require.Equal(t, "partial", clientLeaseStatusClass(domain.LeaseStatusSeason))
	require.Equal(t, "terminated", clientLeaseStatusClass(domain.LeaseStatusTerminated))
	require.Equal(t, "due", clientLeaseStatusClass(domain.LeaseStatusSuspended))
	require.Equal(t, "due", clientLeaseStatusClass("desconhecido"))
}

func TestCovClientReceiptReference(t *testing.T) {
	extra := covReceiptOf("r1", "t1", "", "2026-09-10", 80)
	extra.Category = domain.ReceiptCategoryExtra
	require.Equal(t, "Cobrança avulsa", clientReceiptReference(extra), "avulsa sem descrição tem rótulo próprio")
	extra.Description = domain.StrPtr("Taxa de limpeza")
	require.Equal(t, "Taxa de limpeza", clientReceiptReference(extra))

	cleaning := covReceiptOf("r2", "t1", "", "2026-09-10", 80)
	cleaning.Category = domain.ReceiptCategoryCleaning
	require.Equal(t, "Taxa de limpeza 09/2026", clientReceiptReference(cleaning))

	semData := covReceiptOf("r3", "t1", "", "", 80)
	require.Equal(t, "Aluguel ", clientReceiptReference(semData))
}

func TestCovClientPropertyAddressCachesTheLookup(t *testing.T) {
	env := newCovEnv(t)
	env.props.items = []domain.Property{covPropertyOf("p1", "Casa")}
	cache := map[string]string{}

	first, err := clientPropertyAddress(context.Background(), env.deps, "p1", cache)
	require.NoError(t, err)
	require.Equal(t, "Rua A, 1, Niterói, RJ", first)

	// O imóvel é removido, mas o endereço já resolvido segue no cache.
	env.props.items = nil
	second, err := clientPropertyAddress(context.Background(), env.deps, "p1", cache)
	require.NoError(t, err)
	require.Equal(t, first, second)

	// Id vazio não consulta o repositório e devolve endereço vazio.
	empty, err := clientPropertyAddress(context.Background(), env.deps, "", cache)
	require.NoError(t, err)
	require.Equal(t, "", empty)

	// Falha real do repositório é propagada.
	env.props.byIDErr = errors.New("imóveis fora")
	_, err = clientPropertyAddress(context.Background(), env.deps, "p2", cache)
	require.ErrorIs(t, err, env.props.byIDErr)
}

func TestCovClientValidationURL(t *testing.T) {
	env := newCovEnv(t)

	url, err := clientValidationURL(context.Background(), env.deps, documentKindReceipt, "r1")
	require.NoError(t, err)
	require.Contains(t, url, "/validar/receipt/r1")

	env.deps.Signer = nil
	_, err = clientValidationURL(context.Background(), env.deps, documentKindReceipt, "r1")
	require.ErrorContains(t, err, "montar o link de validação do documento r1")
}
