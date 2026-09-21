package application

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Reajuste anual (tier 2) sobre os dublês do lote `cov`. O cálculo, a vigência
// aceita e o que fica gravado no contrato e na trilha são o que precisa ser
// provado; a carta determinística é provada pelo texto gerado.

// ── Apoio local do reajuste (`cadj`) ──────────────────────────────

// cadjChat é o dublê da porta de conversa: devolve o roteiro combinado e guarda
// os pedidos recebidos, para conferir o que o modelo recebeu.
type cadjChat struct {
	replies  []ChatResponse
	err      error
	requests []ChatRequest
}

func (c *cadjChat) Complete(_ context.Context, request ChatRequest) (ChatResponse, error) {
	c.requests = append(c.requests, request)
	if c.err != nil {
		return ChatResponse{}, c.err
	}
	if len(c.replies) == 0 {
		return ChatResponse{}, nil
	}
	reply := c.replies[0]
	c.replies = c.replies[1:]
	return reply, nil
}

// cadjLeases lê o contrato devolvendo cópia — a caixa em memória devolve o
// próprio elemento da fatia, e sem a cópia o snapshot "antes" mudaria junto com
// a gravação, escondendo o diff do reajuste. Também permite falhar a releitura
// do contrato a partir da enésima chamada, provando que a aplicação aborta
// quando a releitura do contrato reajustado falha (a primeira leitura, a da
// validação, continua funcionando).
type cadjLeases struct {
	*covLeases
	byIDFailAfter int
	byIDErr       error
	calls         int
}

func (r *cadjLeases) ByID(ctx context.Context, id string) (*domain.Lease, error) {
	r.calls++
	if r.byIDFailAfter > 0 && r.calls > r.byIDFailAfter {
		return nil, r.byIDErr
	}
	lease, err := r.covLeases.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	copied := *lease
	return &copied, nil
}

// cadjEnv monta o ambiente do reajuste com o repositório de contratos lendo
// cópias (como o banco faz).
func cadjEnv(t *testing.T) *covEnv {
	t.Helper()
	env := newCovEnv(t)
	env.deps.Repos.Leases = &cadjLeases{covLeases: env.leases}
	return env
}

// cadjAudit é a trilha em memória com falha de leitura injetável — o dublê do
// lote só injeta falha de gravação.
type cadjAudit struct {
	*covAudit
	listErr error
}

func (r *cadjAudit) List(ctx context.Context, filter domain.AuditFilter) ([]domain.AuditLog, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.covAudit.List(ctx, filter)
}

// cadjSeedLease insere o contrato no repositório em memória já com os nomes
// resolvidos, como a consulta do painel entrega.
func cadjSeedLease(env *covEnv, lease domain.Lease) domain.Lease {
	if lease.TenantName == "" {
		lease.TenantName = "Maria Souza"
	}
	if lease.PropertyName == "" {
		lease.PropertyName = "Apto 12"
	}
	env.leases.items = append(env.leases.items, lease)
	return lease
}

// cadjLeaseNamed monta o contrato com os nomes já resolvidos.
func cadjLeaseNamed(id, tenantID, propertyID, tenantName, propertyName string, status domain.LeaseStatus, rent float64) domain.Lease {
	lease := covLeaseOf(id, tenantID, propertyID, status, "2025-01-01", "2027-12-31", rent)
	lease.TenantName = tenantName
	lease.PropertyName = propertyName
	return lease
}

// cadjSeedTenant insere o locatário com o telefone informado (vazio = sem
// telefone cadastrado).
func cadjSeedTenant(env *covEnv, id, name, phone string) {
	tenant := covTenantOf(id, name)
	if phone != "" {
		tenant.Phone = domain.StrPtr(phone)
	}
	env.tenants.items = append(env.tenants.items, tenant)
}

// cadjApply aplica o reajuste combinado dos testes: IGP-M, 4,5%, vigência
// 01/10/2026, já confirmado no painel.
func cadjApply(env *covEnv, leaseID string) (*AdjustmentResult, error) {
	return ApplyAdjustment(context.Background(), env.deps, covCtx(), AdjustmentApplyInput{
		LeaseID:       leaseID,
		Index:         AdjustmentIndexIGPM,
		Rate:          4.5,
		EffectiveDate: "2026-10-01",
		Confirm:       true,
	})
}

// cadjTrailEntry devolve a linha da trilha de um reajuste já aplicado, no mesmo
// formato gravado por ApplyAdjustment (changes e metadata são os JSON crus).
func cadjTrailEntry(id, leaseID, createdAt, changes, metadata string) domain.AuditLog {
	entry := domain.AuditLog{
		ID:        id,
		CreatedAt: createdAt,
		Action:    domain.ActionAdjustmentApplied,
		Entity:    domain.EntityLease,
		EntityID:  domain.StrPtr(leaseID),
		Summary:   "Aluguel reajustado",
	}
	if changes != "" {
		entry.Changes = domain.StrPtr(changes)
	}
	if metadata != "" {
		entry.Metadata = domain.StrPtr(metadata)
	}
	return entry
}

// cadjRentChange é o diff de aluguel de um reajuste já aplicado.
const cadjRentChange = `[{"field":"rentAmount","label":"Aluguel","from":"R$ 2.000,00","to":"R$ 2.100,00"}]`

// ── Vocabulário do índice ─────────────────────────────────────────

func TestCadjIndexVocabulary(t *testing.T) {
	for _, testCase := range []struct {
		index AdjustmentIndex
		want  string
	}{
		{AdjustmentIndexIGPM, "IGP-M"},
		{AdjustmentIndexIPCA, "IPCA"},
		{AdjustmentIndex("incc"), "incc"},
	} {
		assert.Equal(t, testCase.want, AdjustmentIndexLabel(testCase.index))
	}

	for _, testCase := range []struct {
		raw  string
		want AdjustmentIndex
	}{
		{"igpm", AdjustmentIndexIGPM},
		{" IPCA ", AdjustmentIndexIPCA},
		{"IgPm", AdjustmentIndexIGPM},
	} {
		got, err := ParseAdjustmentIndex(testCase.raw)
		require.NoError(t, err, "entrada %q", testCase.raw)
		assert.Equal(t, testCase.want, got)
	}

	for _, raw := range []string{"", "   ", "inpc", "ivar", "igp-m"} {
		_, err := ParseAdjustmentIndex(raw)
		require.Error(t, err, "entrada %q", raw)
		assert.True(t, domain.IsValidation(err), "erro deveria ser de validação: %v", err)
		assert.Equal(t, "Escolha o índice do reajuste (IGP-M ou IPCA).", domain.ValidationMessage(err))
	}

	options := AdjustmentIndexOptions()
	require.Len(t, options, 2)
	assert.Equal(t, AdjustmentIndexOption{Value: "igpm", Label: "IGP-M"}, options[0])
	assert.Equal(t, AdjustmentIndexOption{Value: "ipca", Label: "IPCA"}, options[1])
}

// ── Vigência e cálculo ────────────────────────────────────────────

func TestCadjDateNormalization(t *testing.T) {
	for _, testCase := range []struct{ raw, want string }{
		{"2026-10-01", "2026-10-01"},
		{"  2026-10-01  ", "2026-10-01"},
		{"2026-10-01T00:00:00", "2026-10-01"},
		{"2026-10-01 12:30:00", "2026-10-01"},
		{"01/10/2026", "2026-10-01"},
		{"5/3/2026", "2026-03-05"},
	} {
		got, err := adjustmentDate(testCase.raw)
		require.NoError(t, err, "entrada %q", testCase.raw)
		assert.Equal(t, testCase.want, got)
	}

	for _, testCase := range []struct{ raw, message string }{
		{"", "Informe a data de vigência do reajuste."},
		{"   ", "Informe a data de vigência do reajuste."},
		{"2026-13-01", "Informe uma data de vigência válida."},
		{"31/02/2026", "Informe uma data de vigência válida."},
		{"01/10", "Informe uma data de vigência válida."},
		{"a/b/c", "Informe uma data de vigência válida."},
		{"outubro", "Informe uma data de vigência válida."},
	} {
		_, err := adjustmentDate(testCase.raw)
		require.Error(t, err, "entrada %q", testCase.raw)
		assert.Equal(t, testCase.message, domain.ValidationMessage(err))
	}
}

func TestCadjAmountRounding(t *testing.T) {
	for _, testCase := range []struct{ current, rate, want float64 }{
		{1000, 10, 1100},
		{2500, 4.5, 2612.50},
		{777.77, 3.7, 806.55},
		{987.65, 4.19, 1029.03},
		{1000, 0.0001, 1000}, // o arredondamento devolve o valor original
	} {
		assert.Equal(t, testCase.want, adjustmentAmount(testCase.current, testCase.rate),
			"%.2f com %.3f%%", testCase.current, testCase.rate)
	}

	for _, testCase := range []struct{ value, want float64 }{
		{10.124, 10.12},
		{10.125, 10.13},
		{-3.456, -3.46},
		{0, 0},
	} {
		assert.Equal(t, testCase.want, adjustmentRound(testCase.value))
	}

	for _, testCase := range []struct {
		rate float64
		want string
	}{
		{4.5, "4,5%"},
		{4, "4%"},
		{0.4125, "0,4125%"},
		{10, "10%"},
	} {
		assert.Equal(t, testCase.want, adjustmentRateLabel(testCase.rate))
	}
}

// ── Prévia ────────────────────────────────────────────────────────

func TestCadjPreviewComputesPayload(t *testing.T) {
	env := cadjEnv(t)
	lease := cadjSeedLease(env, covLeaseOf("lease-1", "tenant-1", "prop-1", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 2500))

	preview, err := AdjustmentPreview(context.Background(), env.deps, "  lease-1  ", AdjustmentIndexIPCA, 4.5, "01/10/2026")
	require.NoError(t, err)
	require.NotNil(t, preview)

	assert.Equal(t, "lease-1", preview.LeaseID)
	assert.Equal(t, "Maria Souza", preview.TenantName)
	assert.Equal(t, "Apto 12", preview.PropertyName)
	assert.Equal(t, "ipca", preview.Index)
	assert.Equal(t, "IPCA", preview.IndexLabel)
	assert.Equal(t, 4.5, preview.Rate)
	assert.Equal(t, "4,5%", preview.RateLabel)
	assert.Equal(t, 2500.0, preview.PreviousAmount)
	assert.Equal(t, "R$ 2.500,00", preview.PreviousAmountLabel)
	assert.Equal(t, 2612.5, preview.NewAmount)
	assert.Equal(t, "R$ 2.612,50", preview.NewAmountLabel)
	assert.Equal(t, 112.5, preview.Difference)
	assert.Equal(t, "R$ 112,50", preview.DifferenceLabel)
	assert.Equal(t, "2026-10-01", preview.EffectiveDate)
	assert.Equal(t, "01/10/2026", preview.EffectiveDateLabel)

	assert.Empty(t, env.audit.entries, "a prévia não grava nada")
	assert.Empty(t, env.leases.items[0].UpdatedAt, "a prévia não altera o contrato")
	assert.Equal(t, 2500.0, env.leases.items[0].RentAmount)

	// O painel manda o índice em maiúsculas; a prévia normaliza.
	uppercase, err := AdjustmentPreview(context.Background(), env.deps, lease.ID, AdjustmentIndex("IPCA"), 4.5, "2026-10-01")
	require.NoError(t, err)
	assert.Equal(t, "ipca", uppercase.Index)
}

func TestCadjPreviewRejectsInvalidInput(t *testing.T) {
	env := cadjEnv(t)
	active := cadjSeedLease(env, covLeaseOf("lease-1", "tenant-1", "prop-1", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 2500))
	terminated := cadjSeedLease(env, covLeaseOf("lease-2", "tenant-2", "prop-2", domain.LeaseStatusTerminated, "2023-01-01", "2024-12-31", 1800))
	zeroRent := cadjSeedLease(env, covLeaseOf("lease-3", "tenant-3", "prop-3", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 0))

	for _, testCase := range []struct {
		name    string
		leaseID string
		index   AdjustmentIndex
		rate    float64
		date    string
		message string
	}{
		{"sem contrato", "", AdjustmentIndexIGPM, 4.5, "2026-10-01", "Selecione o contrato do reajuste."},
		{"contrato em branco", "   ", AdjustmentIndexIGPM, 4.5, "2026-10-01", "Selecione o contrato do reajuste."},
		{"índice desconhecido", active.ID, AdjustmentIndex("inpc"), 4.5, "2026-10-01", "Escolha o índice do reajuste (IGP-M ou IPCA)."},
		{"índice vazio", active.ID, "", 4.5, "2026-10-01", "Escolha o índice do reajuste (IGP-M ou IPCA)."},
		{"percentual zero", active.ID, AdjustmentIndexIPCA, 0, "2026-10-01", "Informe um percentual de reajuste maior que zero."},
		{"percentual negativo", active.ID, AdjustmentIndexIPCA, -3, "2026-10-01", "Informe um percentual de reajuste maior que zero."},
		{"percentual não numérico", active.ID, AdjustmentIndexIPCA, math.NaN(), "2026-10-01", "Informe um percentual de reajuste maior que zero."},
		{"percentual infinito", active.ID, AdjustmentIndexIPCA, math.Inf(1), "2026-10-01", "Informe um percentual de reajuste maior que zero."},
		{"vigência vazia", active.ID, AdjustmentIndexIPCA, 4.5, "", "Informe a data de vigência do reajuste."},
		{"vigência inválida", active.ID, AdjustmentIndexIPCA, 4.5, "31/02/2026", "Informe uma data de vigência válida."},
		{"contrato encerrado", terminated.ID, AdjustmentIndexIGPM, 4.5, "2026-10-01", "Contrato encerrado não pode ser reajustado."},
		{"contrato sem aluguel", zeroRent.ID, AdjustmentIndexIGPM, 4.5, "2026-10-01", "O contrato não tem valor de aluguel para reajustar."},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			preview, err := AdjustmentPreview(context.Background(), env.deps, testCase.leaseID, testCase.index, testCase.rate, testCase.date)
			require.Error(t, err)
			assert.Nil(t, preview)
			assert.True(t, domain.IsValidation(err), "erro deveria ser de validação: %v", err)
			assert.Equal(t, testCase.message, domain.ValidationMessage(err))
		})
	}

	assert.Empty(t, env.audit.entries, "prévia recusada não deixa rastro na trilha")
}

func TestCadjPreviewAllowsNonTerminatedStatuses(t *testing.T) {
	env := cadjEnv(t)
	for _, status := range []domain.LeaseStatus{domain.LeaseStatusActive, domain.LeaseStatusSuspended, domain.LeaseStatusSeason} {
		lease := cadjSeedLease(env, covLeaseOf("lease-"+string(status), "tenant-1", "prop-1", status, "2025-01-01", "2027-12-31", 1000))

		preview, err := AdjustmentPreview(context.Background(), env.deps, lease.ID, AdjustmentIndexIGPM, 10, "2026-10-01")
		require.NoError(t, err, "situação %s", status)
		assert.Equal(t, 1100.0, preview.NewAmount)
	}
}

func TestCadjPreviewPropagatesRepositoryFailure(t *testing.T) {
	env := cadjEnv(t)
	sentinel := errors.New("banco fora do ar")
	env.leases.byIDErr = sentinel

	preview, err := AdjustmentPreview(context.Background(), env.deps, "lease-1", AdjustmentIndexIGPM, 4.5, "2026-10-01")
	require.Error(t, err)
	assert.Nil(t, preview)
	assert.True(t, errors.Is(err, sentinel))
}

// ── Aplicação ─────────────────────────────────────────────────────

func TestCadjApplyAdjustmentWritesRentAndTrail(t *testing.T) {
	env := cadjEnv(t)
	env.settings.values[domain.SettingLandlordName] = "João Locador"
	lease := cadjSeedLease(env, covLeaseOf("lease-1", "tenant-1", "prop-1", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 2500))

	result, err := cadjApply(env, lease.ID)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 2612.5, result.Preview.NewAmount)
	assert.Equal(t, AdjustmentNoticeDeterministic, result.NoticeSource)
	for _, expected := range []string{
		"Notificação de reajuste de aluguel",
		"Prezado(a) Maria Souza,",
		"do imóvel Apto 12",
		"IGP-M",
		"4,5%",
		"de R$ 2.500,00 para R$ 2.612,50",
		"a partir de 01/10/2026",
		"João Locador",
	} {
		assert.Contains(t, result.Notice, expected)
	}

	stored := env.leases.items[0]
	assert.Equal(t, 2612.5, stored.RentAmount, "o valor gravado é o valor calculado")
	assert.Equal(t, format.FormatUTCStamp(covToday()), stored.UpdatedAt)

	require.Len(t, env.audit.entries, 1)
	row := auditRowFrom(&env.audit.entries[0])
	assert.Equal(t, domain.ActionAdjustmentApplied, row.Action)
	assert.Equal(t, domain.EntityLease, row.Entity)
	assert.Equal(t, "web", row.Origin)
	require.NotNil(t, row.EntityID)
	assert.Equal(t, "lease-1", *row.EntityID)
	require.NotNil(t, row.EntityLabel)
	assert.Equal(t, "Maria Souza · Apto 12", *row.EntityLabel)
	assert.Contains(t, row.Summary, "Aluguel de Maria Souza · Apto 12 reajustado pelo IGP-M (4,5%)")
	assert.Contains(t, row.Summary, "de R$ 2.500,00 para R$ 2.612,50, a partir de 01/10/2026")

	assert.Equal(t, "igpm", row.Metadata["index"])
	assert.Equal(t, 4.5, row.Metadata["rate"])
	assert.Equal(t, "2026-10-01", row.Metadata["effectiveDate"])
	assert.Equal(t, 2612.5, row.Metadata["amount"])

	require.Len(t, row.Changes, 1)
	change := row.Changes[0]
	assert.Equal(t, "rentAmount", change.Field)
	assert.Equal(t, "Aluguel", change.Label)
	require.NotNil(t, change.From)
	require.NotNil(t, change.To)
	assert.Equal(t, "R$ 2.500,00", *change.From)
	assert.Equal(t, "R$ 2.612,50", *change.To)
}

func TestCadjApplyAdjustmentRequiresConfirmation(t *testing.T) {
	env := cadjEnv(t)
	cadjSeedLease(env, covLeaseOf("lease-1", "tenant-1", "prop-1", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 2500))
	// O repositório falharia, mas a confirmação é checada antes de qualquer leitura.
	env.leases.byIDErr = errors.New("não deveria ler o contrato")

	result, err := ApplyAdjustment(context.Background(), env.deps, covCtx(), AdjustmentApplyInput{
		LeaseID:       "lease-1",
		Index:         AdjustmentIndexIGPM,
		Rate:          4.5,
		EffectiveDate: "2026-10-01",
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, domain.IsValidation(err))
	assert.Equal(t, "Confirme o reajuste para aplicar.", domain.ValidationMessage(err))
	assert.Equal(t, 2500.0, env.leases.items[0].RentAmount)
	assert.Empty(t, env.audit.entries)
}

func TestCadjApplyAdjustmentPropagatesValidation(t *testing.T) {
	env := cadjEnv(t)
	lease := cadjSeedLease(env, covLeaseOf("lease-1", "tenant-1", "prop-1", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 2500))

	for _, testCase := range []struct {
		name  string
		input AdjustmentApplyInput
	}{
		{"índice desconhecido", AdjustmentApplyInput{LeaseID: lease.ID, Index: AdjustmentIndex("inpc"), Rate: 4.5, EffectiveDate: "2026-10-01", Confirm: true}},
		{"percentual zero", AdjustmentApplyInput{LeaseID: lease.ID, Index: AdjustmentIndexIGPM, Rate: 0, EffectiveDate: "2026-10-01", Confirm: true}},
		{"vigência inválida", AdjustmentApplyInput{LeaseID: lease.ID, Index: AdjustmentIndexIGPM, Rate: 4.5, EffectiveDate: "31/02/2026", Confirm: true}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := ApplyAdjustment(context.Background(), env.deps, covCtx(), testCase.input)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.True(t, domain.IsValidation(err), "erro deveria ser de validação: %v", err)
		})
	}

	// Contrato inexistente é erro de registro ausente, não de validação.
	_, err := ApplyAdjustment(context.Background(), env.deps, covCtx(), AdjustmentApplyInput{
		LeaseID: "lease-inexistente", Index: AdjustmentIndexIGPM, Rate: 4.5, EffectiveDate: "2026-10-01", Confirm: true,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrNotFound))

	assert.Equal(t, 2500.0, env.leases.items[0].RentAmount, "aplicação recusada não altera o contrato")
	assert.Empty(t, env.audit.entries)
}

func TestCadjApplyAdjustmentPropagatesRepositoryFailure(t *testing.T) {
	sentinel := errors.New("banco fora do ar")

	t.Run("falha ao gravar o contrato", func(t *testing.T) {
		env := cadjEnv(t)
		cadjSeedLease(env, covLeaseOf("lease-1", "tenant-1", "prop-1", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 2500))
		env.leases.updateErr = sentinel

		result, err := cadjApply(env, "lease-1")
		require.Error(t, err)
		assert.Nil(t, result)
		assert.True(t, errors.Is(err, sentinel))
		assert.Contains(t, err.Error(), "aplicar reajuste no contrato")
		assert.Equal(t, 2500.0, env.leases.items[0].RentAmount, "sem gravação o contrato fica como estava")
		assert.Empty(t, env.audit.entries, "sem gravação não há linha na trilha")
	})

	t.Run("falha ao reler o contrato reajustado", func(t *testing.T) {
		env := cadjEnv(t)
		cadjSeedLease(env, covLeaseOf("lease-1", "tenant-1", "prop-1", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 2500))
		env.deps.Repos.Leases = &cadjLeases{covLeases: env.leases, byIDFailAfter: 1, byIDErr: sentinel}

		result, err := cadjApply(env, "lease-1")
		require.Error(t, err)
		assert.Nil(t, result)
		assert.True(t, errors.Is(err, sentinel))
		assert.Contains(t, err.Error(), "buscar contrato reajustado")
		assert.Equal(t, 2612.5, env.leases.items[0].RentAmount, "o valor já tinha sido gravado antes da releitura")
		assert.Empty(t, env.audit.entries)
	})

	t.Run("falha ao gravar a trilha", func(t *testing.T) {
		env := cadjEnv(t)
		cadjSeedLease(env, covLeaseOf("lease-1", "tenant-1", "prop-1", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 2500))
		env.audit.insertErr = sentinel

		result, err := cadjApply(env, "lease-1")
		require.Error(t, err)
		assert.Nil(t, result)
		assert.True(t, errors.Is(err, sentinel))
		assert.Contains(t, err.Error(), "gravar auditoria")
		assert.Equal(t, 2612.5, env.leases.items[0].RentAmount, "a falha de trilha não desfaz o reajuste")
	})
}

func TestCadjApplyAdjustmentWithoutValueChange(t *testing.T) {
	env := cadjEnv(t)
	cadjSeedLease(env, covLeaseOf("lease-1", "tenant-1", "prop-1", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 1000))

	result, err := ApplyAdjustment(context.Background(), env.deps, covCtx(), AdjustmentApplyInput{
		LeaseID:       "lease-1",
		Index:         AdjustmentIndexIPCA,
		Rate:          0.0001,
		EffectiveDate: "2026-10-01",
		Confirm:       true,
	})
	require.NoError(t, err)
	assert.Equal(t, 1000.0, result.Preview.NewAmount, "o arredondamento pode devolver o valor atual")
	assert.Equal(t, 1000.0, env.leases.items[0].RentAmount)
	assert.Empty(t, env.audit.entries, "reajuste que não muda o valor não gera linha na trilha")
}

// ── Notificação formal ────────────────────────────────────────────

func TestCadjNoticeText(t *testing.T) {
	env := cadjEnv(t)
	lease := cadjSeedLease(env, covLeaseOf("lease-1", "tenant-1", "prop-1", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 2500))
	preview, err := AdjustmentPreview(context.Background(), env.deps, lease.ID, AdjustmentIndexIPCA, 4.5, "2026-10-01")
	require.NoError(t, err)

	notice := adjustmentNotice(*preview, "João Locador")
	assert.Contains(t, notice, "Prezado(a) Maria Souza,")
	assert.Contains(t, notice, "Na qualidade de locador(a) do imóvel Apto 12,")
	assert.Contains(t, notice, "pelo índice IPCA, correspondente a 4,5%.")
	assert.Contains(t, notice, "O valor mensal passa de R$ 2.500,00 para R$ 2.612,50, a partir de 01/10/2026.")
	assert.Contains(t, notice, "O novo valor será cobrado nos vencimentos a partir da vigência indicada.")
	assert.True(t, strings.HasSuffix(notice, "Atenciosamente,\nJoão Locador\n"))

	anonymous := *preview
	anonymous.TenantName = "   "
	anonymous.PropertyName = ""
	noLandlord := adjustmentNotice(anonymous, "  ")
	assert.Contains(t, noLandlord, "Prezado(a) locatário(a),")
	assert.NotContains(t, noLandlord, " do imóvel")
	assert.True(t, strings.HasSuffix(noLandlord, "Atenciosamente,\n"))
}

func TestCadjApplyAdjustmentUsesAINotice(t *testing.T) {
	t.Run("provedor desligado mantém a carta determinística", func(t *testing.T) {
		env := cadjEnv(t)
		env.deps.Opts.AIEnabled = false
		cadjSeedLease(env, covLeaseOf("lease-1", "tenant-1", "prop-1", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 2500))

		result, err := cadjApply(env, "lease-1")
		require.NoError(t, err)
		assert.Equal(t, AdjustmentNoticeDeterministic, result.NoticeSource)
		assert.Contains(t, result.Notice, "R$ 2.612,50")
	})

	t.Run("carta redigida cita os números calculados", func(t *testing.T) {
		env := cadjEnv(t)
		env.deps.Opts.AIEnabled = true
		cadjSeedLease(env, covLeaseOf("lease-1", "tenant-1", "prop-1", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 2500))
		written := "Prezado(a) Maria Souza, o aluguel do Apto 12 passa a R$ 2.612,50 a partir de 01/10/2026."
		chat := &cadjChat{replies: []ChatResponse{{Message: "\n  " + written + "  \n"}}}
		env.deps.Chat = chat

		result, err := cadjApply(env, "lease-1")
		require.NoError(t, err)
		assert.Equal(t, AdjustmentNoticeAI, result.NoticeSource)
		assert.Equal(t, written, result.Notice, "o texto redigido volta sem os espaços das bordas")

		require.Len(t, chat.requests, 1)
		request := chat.requests[0]
		assert.Equal(t, adjustmentNoticeSystem, request.System)
		assert.Equal(t, 0.2, request.Temperature)
		assert.Empty(t, request.Tools, "a redação não declara ferramenta: o modelo não consulta nem calcula")
		require.Len(t, request.Messages, 1)
		assert.Equal(t, "user", request.Messages[0].Role)
		for _, expected := range []string{"IGP-M", "4,5%", "R$ 2.500,00", "R$ 2.612,50", "01/10/2026", "Modelo de referência"} {
			assert.Contains(t, request.Messages[0].Content, expected)
		}

		// O texto do modelo não muda o que foi gravado.
		assert.Equal(t, 2612.5, env.leases.items[0].RentAmount)
		require.Len(t, env.audit.entries, 1)
		assert.Equal(t, 2612.5, auditRowFrom(&env.audit.entries[0]).Metadata["amount"])
	})

	t.Run("carta sem os números calculados cai na determinística", func(t *testing.T) {
		env := cadjEnv(t)
		cadjSeedLease(env, covLeaseOf("lease-1", "tenant-1", "prop-1", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 2500))
		chat := &cadjChat{replies: []ChatResponse{{Message: "Comunicamos o reajuste do seu aluguel."}}}
		env.deps.Chat = chat

		result, err := cadjApply(env, "lease-1")
		require.NoError(t, err)
		assert.Equal(t, AdjustmentNoticeDeterministic, result.NoticeSource)
		assert.Contains(t, result.Notice, "R$ 2.612,50")
		assert.Contains(t, result.Notice, "01/10/2026")
		assert.Len(t, chat.requests, 1)
	})

	t.Run("carta em branco cai na determinística", func(t *testing.T) {
		env := cadjEnv(t)
		cadjSeedLease(env, covLeaseOf("lease-1", "tenant-1", "prop-1", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 2500))
		env.deps.Chat = &cadjChat{replies: []ChatResponse{{Message: "   \n  "}}}

		result, err := cadjApply(env, "lease-1")
		require.NoError(t, err)
		assert.Equal(t, AdjustmentNoticeDeterministic, result.NoticeSource)
		assert.Contains(t, result.Notice, "R$ 2.612,50")
	})

	t.Run("falha do provedor cai na determinística", func(t *testing.T) {
		env := cadjEnv(t)
		cadjSeedLease(env, covLeaseOf("lease-1", "tenant-1", "prop-1", domain.LeaseStatusActive, "2025-01-01", "2027-12-31", 2500))
		env.deps.Chat = &cadjChat{err: errors.New("provedor fora do ar")}

		result, err := cadjApply(env, "lease-1")
		require.NoError(t, err, "a falha de redação não derruba o reajuste")
		assert.Equal(t, AdjustmentNoticeDeterministic, result.NoticeSource)
		assert.Contains(t, result.Notice, "R$ 2.612,50")
		assert.Contains(t, result.Notice, "01/10/2026")
	})
}

// ── Histórico lido da trilha ──────────────────────────────────────

func TestCadjHistoryMetadataReading(t *testing.T) {
	metadata := map[string]any{
		"index":         "ipca",
		"effectiveDate": "2026-11-01",
		"rate":          4.5,
		"note":          "reajuste anual",
		"count":         3,
		"nothing":       nil,
	}
	assert.Equal(t, "ipca", adjustmentMetadataString(metadata, "index"))
	assert.Equal(t, "reajuste anual", adjustmentMetadataString(metadata, "note"))
	assert.Equal(t, "", adjustmentMetadataString(metadata, "missing"))
	assert.Equal(t, "", adjustmentMetadataString(metadata, "rate"), "metadado numérico não é texto")
	assert.Equal(t, "", adjustmentMetadataString(metadata, "nothing"))

	assert.Equal(t, "4,5%", adjustmentMetadataRate(metadata))
	assert.Equal(t, "4,5%", adjustmentMetadataRate(map[string]any{"rate": "4.5"}))
	assert.Equal(t, "", adjustmentMetadataRate(map[string]any{"rate": "abc"}))
	assert.Equal(t, "", adjustmentMetadataRate(map[string]any{"rate": 3}))
	assert.Equal(t, "", adjustmentMetadataRate(map[string]any{"rate": nil}))
	assert.Equal(t, "", adjustmentMetadataRate(map[string]any{}))
}

func TestCadjHistoryOfReadsTrailRow(t *testing.T) {
	entry := auditRowFrom(&domain.AuditLog{
		ID:        "audit-1",
		CreatedAt: "2026-09-21T09:30:00.000",
		Action:    domain.ActionAdjustmentApplied,
		Entity:    domain.EntityLease,
		EntityID:  domain.StrPtr("lease-1"),
		Summary:   "Aluguel reajustado pelo IPCA (4,5%)",
		Changes:   domain.StrPtr(`[{"field":"tenant_name","label":"Inquilino","from":"Maria","to":"Maria"},{"field":"rentAmount","label":"Aluguel","from":"R$ 2.500,00","to":"R$ 2.612,50"}]`),
		Metadata:  domain.StrPtr(`{"index":"ipca","rate":4.5,"effectiveDate":"2026-11-01"}`),
	})

	history := adjustmentHistoryOf(entry)
	assert.Equal(t, "audit-1", history.ID)
	assert.Equal(t, "2026-09-21T09:30:00.000", history.AppliedAt)
	assert.Equal(t, "21/09/2026 09:30", history.AppliedAtLabel)
	assert.Equal(t, "Aluguel reajustado pelo IPCA (4,5%)", history.Summary)
	assert.Equal(t, "IPCA", history.IndexLabel)
	assert.Equal(t, "4,5%", history.RateLabel)
	assert.Equal(t, "01/11/2026", history.EffectiveDateLabel)
	assert.Equal(t, "R$ 2.500,00", history.PreviousAmountLabel)
	assert.Equal(t, "R$ 2.612,50", history.NewAmountLabel)
}

func TestCadjHistoryOfWithoutNumbers(t *testing.T) {
	entry := auditRowFrom(&domain.AuditLog{
		ID:        "audit-2",
		CreatedAt: "2026-09-21T09:30:00.000",
		Metadata:  domain.StrPtr(`{"index":7,"rate":"x"}`),
	})

	history := adjustmentHistoryOf(entry)
	assert.Equal(t, "", history.IndexLabel, "índice sem texto na trilha fica sem rótulo")
	assert.Equal(t, "", history.RateLabel)
	assert.Equal(t, "", history.EffectiveDateLabel, "evento sem vigência nos metadados não inventa data")
	assert.Equal(t, "", history.PreviousAmountLabel)
	assert.Equal(t, "", history.NewAmountLabel)
}

func TestCadjHistoryGroupsByLease(t *testing.T) {
	env := cadjEnv(t)
	env.audit.entries = []domain.AuditLog{
		cadjTrailEntry("audit-1", "lease-1", "2026-09-20T10:00:00.000", cadjRentChange, `{"index":"igpm","rate":5,"effectiveDate":"2026-10-01"}`),
		cadjTrailEntry("audit-2", "lease-2", "2026-09-19T10:00:00.000", cadjRentChange, `{"index":"ipca","rate":4.5,"effectiveDate":"2026-11-01"}`),
		cadjTrailEntry("audit-3", "lease-1", "2026-09-18T10:00:00.000", cadjRentChange, `{"index":"ipca","rate":4.19,"effectiveDate":"2026-12-01"}`),
		cadjTrailEntry("audit-4", "", "2026-09-17T10:00:00.000", "", ""),
	}

	grouped, err := adjustmentHistory(context.Background(), env.deps)
	require.NoError(t, err)
	require.Len(t, grouped, 2, "evento sem contrato não entra no histórico")

	require.Len(t, grouped["lease-1"], 2)
	assert.Equal(t, "audit-1", grouped["lease-1"][0].ID)
	assert.Equal(t, "audit-3", grouped["lease-1"][1].ID)
	assert.Equal(t, "5%", grouped["lease-1"][0].RateLabel)
	assert.Equal(t, "IGP-M", grouped["lease-1"][0].IndexLabel)
	assert.Equal(t, "4,19%", grouped["lease-1"][1].RateLabel)
	assert.Equal(t, "R$ 2.000,00", grouped["lease-1"][0].PreviousAmountLabel)
	assert.Equal(t, "R$ 2.100,00", grouped["lease-1"][0].NewAmountLabel)

	require.Len(t, grouped["lease-2"], 1)
	assert.Equal(t, "IPCA", grouped["lease-2"][0].IndexLabel)
	assert.Equal(t, "01/11/2026", grouped["lease-2"][0].EffectiveDateLabel)
	assert.NotContains(t, grouped, "")
}

func TestCadjHistoryPropagatesTrailFailure(t *testing.T) {
	env := cadjEnv(t)
	sentinel := errors.New("trilha indisponível")
	env.deps.Repos.Audit = &cadjAudit{covAudit: env.audit, listErr: sentinel}

	grouped, err := adjustmentHistory(context.Background(), env.deps)
	require.Error(t, err)
	assert.Nil(t, grouped)
	assert.True(t, errors.Is(err, sentinel))
	assert.Contains(t, err.Error(), "listar reajustes da trilha")
}

// ── Aba de reajuste ───────────────────────────────────────────────

func TestCadjBuildAdjustmentView(t *testing.T) {
	env := cadjEnv(t)
	cadjSeedTenant(env, "tenant-1", "Ana", "21999990000")
	cadjSeedTenant(env, "tenant-2", "Bruno", "")
	cadjSeedLease(env, cadjLeaseNamed("lease-bruno", "tenant-2", "prop-2", "Bruno", "Casa 2", domain.LeaseStatusActive, 1200))
	anaCasa := cadjSeedLease(env, cadjLeaseNamed("lease-ana-casa", "tenant-1", "prop-1", "Ana", "Casa 1", domain.LeaseStatusActive, 2000))
	cadjSeedLease(env, cadjLeaseNamed("lease-ana-apto", "tenant-1", "prop-3", "Ana", "Apto 3", domain.LeaseStatusActive, 1800))
	cadjSeedLease(env, cadjLeaseNamed("lease-carla", "tenant-9", "prop-4", "Carla", "Sítio", domain.LeaseStatusTerminated, 900))
	cadjSeedLease(env, cadjLeaseNamed("lease-zulu", "tenant-3", "prop-5", "Zulu", "Kitnet", domain.LeaseStatusActive, 0))
	env.audit.entries = []domain.AuditLog{
		cadjTrailEntry("audit-1", anaCasa.ID, "2026-09-20T10:00:00.000", cadjRentChange, `{"index":"igpm","rate":4.5,"effectiveDate":"2026-10-01"}`),
	}

	view, err := BuildAdjustmentView(context.Background(), env.deps)
	require.NoError(t, err)
	require.NotNil(t, view)

	assert.Equal(t, "2026-09-21", view.Today)
	assert.Equal(t, "2026-09", view.MonthKey)
	assert.Equal(t, AdjustmentIndexOptions(), view.Indices)

	require.Len(t, view.Leases, 5)
	order := make([]string, 0, len(view.Leases))
	for _, row := range view.Leases {
		order = append(order, row.TenantName+"/"+row.PropertyName)
	}
	assert.Equal(t, []string{"Ana/Apto 3", "Ana/Casa 1", "Bruno/Casa 2", "Zulu/Kitnet", "Carla/Sítio"}, order,
		"contrato encerrado vai por último; o resto por locatário e imóvel")

	ana := view.Leases[1]
	assert.Equal(t, "21999990000", ana.TenantPhone, "o telefone vem do locatário titular")
	assert.Equal(t, "Ativo", ana.StatusLabel)
	assert.True(t, ana.Adjustable)
	assert.Equal(t, 2000.0, ana.RentAmount)
	assert.Equal(t, "R$ 2.000,00", ana.RentAmountLabel)
	assert.Equal(t, "2025-01-01", ana.StartDate)
	assert.Equal(t, "2027-12-31", ana.EndDate)
	require.Len(t, ana.History, 1)
	assert.Equal(t, "IGP-M", ana.History[0].IndexLabel)
	assert.Equal(t, "4,5%", ana.History[0].RateLabel)
	assert.Equal(t, "01/10/2026", ana.History[0].EffectiveDateLabel)
	assert.Equal(t, "R$ 2.000,00", ana.History[0].PreviousAmountLabel)

	bruno := view.Leases[2]
	assert.Equal(t, "", bruno.TenantPhone, "locatário sem telefone não inventa número")
	assert.NotNil(t, bruno.History, "contrato sem reajuste entrega lista vazia, não nula")
	assert.Empty(t, bruno.History)

	zulu := view.Leases[3]
	assert.False(t, zulu.Adjustable, "contrato sem valor de aluguel não é reajustável")

	terminated := view.Leases[4]
	assert.False(t, terminated.Adjustable, "contrato encerrado não é reajustável")
	assert.Equal(t, "Encerrado", terminated.StatusLabel)
}

func TestCadjBuildAdjustmentViewPropagatesFailures(t *testing.T) {
	sentinel := errors.New("banco fora do ar")

	t.Run("falha ao listar contratos", func(t *testing.T) {
		env := cadjEnv(t)
		env.leases.listErr = sentinel

		view, err := BuildAdjustmentView(context.Background(), env.deps)
		require.Error(t, err)
		assert.Nil(t, view)
		assert.True(t, errors.Is(err, sentinel))
		assert.Contains(t, err.Error(), "listar contratos")
	})

	t.Run("falha ao listar locatários", func(t *testing.T) {
		env := cadjEnv(t)
		env.tenants.listErr = sentinel

		view, err := BuildAdjustmentView(context.Background(), env.deps)
		require.Error(t, err)
		assert.Nil(t, view)
		assert.True(t, errors.Is(err, sentinel))
		assert.Contains(t, err.Error(), "listar locatários")
	})

	t.Run("falha ao ler a trilha", func(t *testing.T) {
		env := cadjEnv(t)
		env.deps.Repos.Audit = &cadjAudit{covAudit: env.audit, listErr: sentinel}

		view, err := BuildAdjustmentView(context.Background(), env.deps)
		require.Error(t, err)
		assert.Nil(t, view)
		assert.True(t, errors.Is(err, sentinel))
		assert.Contains(t, err.Error(), "listar reajustes da trilha")
	})
}
