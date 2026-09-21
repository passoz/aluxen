package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// O reajuste é exercitado sobre o SQLite em memória (as mesmas queries do
// sistema) porque o valor gravado e a linha da trilha são o que precisa ser
// provado — dublê de repositório não mostraria nem um nem outro.

// adjustmentToday é o instante de referência dos testes de reajuste.
var adjustmentToday = time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)

// adjustmentChat é o dublê da porta de conversa: devolve o roteiro combinado e
// guarda o pedido recebido.
type adjustmentChat struct {
	responses []application.ChatResponse
	requests  []application.ChatRequest
	err       error
}

func (c *adjustmentChat) Complete(_ context.Context, request application.ChatRequest) (application.ChatResponse, error) {
	c.requests = append(c.requests, request)
	if c.err != nil {
		return application.ChatResponse{}, c.err
	}
	if len(c.responses) == 0 {
		return application.ChatResponse{}, nil
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

// newAdjustmentFixture monta o ambiente do reajuste (locatário, imóvel e
// relógio fixo) reusando o apoio dos testes de lançamento.
func newAdjustmentFixture(t *testing.T) *receiptsFixture {
	t.Helper()
	return newReceiptsFixture(t, adjustmentToday)
}

func TestAdjustmentPreviewComputesDeterministicAmount(t *testing.T) {
	fixture := newAdjustmentFixture(t)

	cases := []struct {
		name string
		rent float64
		rate float64
		want float64
	}{
		{"percentual cheio", 2500, 4.5, 2612.50},
		{"arredonda para cima no terceiro decimal", 777.77, 3.7, 806.55},
		{"arredonda para baixo no terceiro decimal", 1500.55, 3.7, 1556.07},
		{"percentual com centésimos", 987.65, 4.19, 1029.03},
	}

	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			lease := fixture.seedLease(t, domain.Lease{
				ID:         "lease-reajuste-" + string(rune('a'+index)),
				RentAmount: testCase.rent,
			})

			preview, err := application.AdjustmentPreview(context.Background(), fixture.deps, lease.ID,
				application.AdjustmentIndexIGPM, testCase.rate, "2026-10-01")
			require.NoError(t, err)

			assert.Equal(t, testCase.want, preview.NewAmount)
			assert.Equal(t, testCase.rent, preview.PreviousAmount)
			assert.InDelta(t, testCase.want-testCase.rent, preview.Difference, 0.001)
			assert.Equal(t, "igpm", preview.Index)
			assert.Equal(t, "IGP-M", preview.IndexLabel)
			assert.Equal(t, "2026-10-01", preview.EffectiveDate)
			assert.Equal(t, "01/10/2026", preview.EffectiveDateLabel)
			assert.Equal(t, format.FormatCurrency(testCase.want), preview.NewAmountLabel)
			assert.Equal(t, "Maria Souza", preview.TenantName)
			assert.Equal(t, "Apto 12", preview.PropertyName)
		})
	}
}

func TestAdjustmentPreviewFormatsRateForReading(t *testing.T) {
	fixture := newAdjustmentFixture(t)
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-rate"})

	preview, err := application.AdjustmentPreview(context.Background(), fixture.deps, lease.ID,
		application.AdjustmentIndexIPCA, 4.5, "2026-10-01")
	require.NoError(t, err)
	assert.Equal(t, "4,5%", preview.RateLabel)
	assert.Equal(t, "IPCA", preview.IndexLabel)

	preview, err = application.AdjustmentPreview(context.Background(), fixture.deps, lease.ID,
		application.AdjustmentIndexIPCA, 4, "2026-10-01")
	require.NoError(t, err)
	assert.Equal(t, "4%", preview.RateLabel)
}

func TestAdjustmentPreviewRejectsInvalidInput(t *testing.T) {
	fixture := newAdjustmentFixture(t)
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-ativo"})
	terminated := fixture.seedLease(t, domain.Lease{
		ID: "lease-encerrado", Status: domain.LeaseStatusTerminated, PropertyID: fixture.propID,
		StartDate: "2024-01-01", EndDate: "2025-12-31",
	})

	cases := []struct {
		name          string
		leaseID       string
		index         application.AdjustmentIndex
		rate          float64
		effectiveDate string
		message       string
	}{
		{"sem contrato", "", application.AdjustmentIndexIGPM, 4.5, "2026-10-01", "Selecione o contrato do reajuste."},
		{"índice desconhecido", lease.ID, application.AdjustmentIndex("inpc"), 4.5, "2026-10-01", "Escolha o índice do reajuste (IGP-M ou IPCA)."},
		{"índice vazio", lease.ID, "", 4.5, "2026-10-01", "Escolha o índice do reajuste (IGP-M ou IPCA)."},
		{"percentual zero", lease.ID, application.AdjustmentIndexIGPM, 0, "2026-10-01", "Informe um percentual de reajuste maior que zero."},
		{"percentual negativo", lease.ID, application.AdjustmentIndexIGPM, -3, "2026-10-01", "Informe um percentual de reajuste maior que zero."},
		{"vigência vazia", lease.ID, application.AdjustmentIndexIGPM, 4.5, "", "Informe a data de vigência do reajuste."},
		{"vigência inválida", lease.ID, application.AdjustmentIndexIGPM, 4.5, "31/02/2026", "Informe uma data de vigência válida."},
		{"vigência ilegível", lease.ID, application.AdjustmentIndexIGPM, 4.5, "outubro", "Informe uma data de vigência válida."},
		{"contrato encerrado", terminated.ID, application.AdjustmentIndexIGPM, 4.5, "2026-10-01", "Contrato encerrado não pode ser reajustado."},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := application.AdjustmentPreview(context.Background(), fixture.deps, testCase.leaseID,
				testCase.index, testCase.rate, testCase.effectiveDate)
			requireAdjustmentInvalid(t, err, testCase.message)
		})
	}
}

func TestAdjustmentPreviewRejectsUnknownLease(t *testing.T) {
	fixture := newAdjustmentFixture(t)

	_, err := application.AdjustmentPreview(context.Background(), fixture.deps, "lease-inexistente",
		application.AdjustmentIndexIGPM, 4.5, "2026-10-01")
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrNotFound), "erro deveria ser de registro ausente: %v", err)
}

func TestApplyAdjustmentWritesRentAndAudits(t *testing.T) {
	fixture := newAdjustmentFixture(t)
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1", RentAmount: 2500})

	result, err := application.ApplyAdjustment(context.Background(), fixture.deps, receiptsAuditContext(),
		application.AdjustmentApplyInput{
			LeaseID:       lease.ID,
			Index:         application.AdjustmentIndexIGPM,
			Rate:          4.5,
			EffectiveDate: "2026-10-01",
			Confirm:       true,
		})
	require.NoError(t, err)

	assert.Equal(t, 2612.50, result.Preview.NewAmount)
	assert.Equal(t, application.AdjustmentNoticeDeterministic, result.NoticeSource)
	assert.Contains(t, result.Notice, "R$ 2.612,50", "notificação deve citar o valor novo")
	assert.Contains(t, result.Notice, "01/10/2026", "notificação deve citar a vigência")
	assert.Contains(t, result.Notice, "IGP-M")
	assert.Contains(t, result.Notice, "4,5%")
	assert.Contains(t, result.Notice, "R$ 2.500,00", "notificação deve citar o valor anterior")
	assert.Contains(t, result.Notice, "Maria Souza", "notificação deve qualificar o inquilino")
	assert.Contains(t, result.Notice, "Apto 12")

	stored, err := fixture.deps.Repos.Leases.ByID(context.Background(), lease.ID)
	require.NoError(t, err)
	assert.Equal(t, 2612.50, stored.RentAmount, "o valor gravado é o valor calculado")

	entries := fixture.auditFor(t, domain.ActionAdjustmentApplied)
	require.Len(t, entries, 1)
	entry := entries[0]
	assert.Equal(t, domain.EntityLease, entry.Entity)
	assert.Equal(t, lease.ID, domain.Str(entry.EntityID))
	assert.Contains(t, entry.Summary, "R$ 2.500,00")
	assert.Contains(t, entry.Summary, "R$ 2.612,50")

	change := receiptsChangeFor(t, receiptsChanges(t, entry), "rentAmount")
	require.NotNil(t, change.From)
	require.NotNil(t, change.To)
	assert.Equal(t, "R$ 2.500,00", *change.From)
	assert.Equal(t, "R$ 2.612,50", *change.To)

	metadata := receiptsMetadata(t, entry)
	assert.Equal(t, "igpm", metadata["index"])
	assert.Equal(t, 4.5, metadata["rate"])
	assert.Equal(t, "2026-10-01", metadata["effectiveDate"])
	assert.Equal(t, 2612.5, metadata["amount"])
}

func TestApplyAdjustmentRequiresConfirmation(t *testing.T) {
	fixture := newAdjustmentFixture(t)
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1", RentAmount: 2500})

	_, err := application.ApplyAdjustment(context.Background(), fixture.deps, receiptsAuditContext(),
		application.AdjustmentApplyInput{
			LeaseID:       lease.ID,
			Index:         application.AdjustmentIndexIGPM,
			Rate:          4.5,
			EffectiveDate: "2026-10-01",
		})
	requireAdjustmentInvalid(t, err, "Confirme o reajuste para aplicar.")

	stored, err := fixture.deps.Repos.Leases.ByID(context.Background(), lease.ID)
	require.NoError(t, err)
	assert.Equal(t, 2500.0, stored.RentAmount, "sem confirmação nada é gravado")
	assert.Empty(t, fixture.auditFor(t, domain.ActionAdjustmentApplied))
}

func TestApplyAdjustmentUsesRedactedNoticeFromAI(t *testing.T) {
	fixture := newAdjustmentFixture(t)
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1", RentAmount: 2500})

	chat := &adjustmentChat{responses: []application.ChatResponse{{
		Message: "Prezado(a) Maria Souza, o aluguel do Apto 12 passa a R$ 2.612,50 a partir de 01/10/2026.",
	}}}
	fixture.deps.Chat = chat
	redacted := "Prezado(a) Maria Souza, o aluguel do Apto 12 passa a R$ 2.612,50 a partir de 01/10/2026."

	result, err := application.ApplyAdjustment(context.Background(), fixture.deps, receiptsAuditContext(),
		application.AdjustmentApplyInput{
			LeaseID:       lease.ID,
			Index:         application.AdjustmentIndexIGPM,
			Rate:          4.5,
			EffectiveDate: "2026-10-01",
			Confirm:       true,
		})
	require.NoError(t, err)

	assert.Equal(t, application.AdjustmentNoticeAI, result.NoticeSource)
	assert.Equal(t, redacted, result.Notice)
	require.Len(t, chat.requests, 1)
	assert.Empty(t, chat.requests[0].Tools, "a redação não declara ferramenta: o modelo não consulta nem calcula")
	assert.Contains(t, chat.requests[0].Messages[0].Content, "R$ 2.612,50", "o modelo recebe o valor já calculado")
	assert.Contains(t, chat.requests[0].Messages[0].Content, "01/10/2026")

	// O valor gravado continua sendo o do cálculo determinístico.
	stored, err := fixture.deps.Repos.Leases.ByID(context.Background(), lease.ID)
	require.NoError(t, err)
	assert.Equal(t, 2612.50, stored.RentAmount)
}

func TestApplyAdjustmentFallsBackWhenRedactionOmitsNumbers(t *testing.T) {
	fixture := newAdjustmentFixture(t)
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1", RentAmount: 2500})

	fixture.deps.Chat = &adjustmentChat{responses: []application.ChatResponse{{
		Message: "Comunicamos o reajuste do seu aluguel. Qualquer dúvida, estamos à disposição.",
	}}}

	result, err := application.ApplyAdjustment(context.Background(), fixture.deps, receiptsAuditContext(),
		application.AdjustmentApplyInput{
			LeaseID:       lease.ID,
			Index:         application.AdjustmentIndexIPCA,
			Rate:          4.5,
			EffectiveDate: "2026-10-01",
			Confirm:       true,
		})
	require.NoError(t, err)

	assert.Equal(t, application.AdjustmentNoticeDeterministic, result.NoticeSource)
	assert.Contains(t, result.Notice, "R$ 2.612,50")
	assert.Contains(t, result.Notice, "01/10/2026")
}

func TestApplyAdjustmentFallsBackWhenProviderFails(t *testing.T) {
	fixture := newAdjustmentFixture(t)
	lease := fixture.seedLease(t, domain.Lease{ID: "lease-1", RentAmount: 2500})

	fixture.deps.Chat = &adjustmentChat{err: errors.New("provedor fora do ar")}

	result, err := application.ApplyAdjustment(context.Background(), fixture.deps, receiptsAuditContext(),
		application.AdjustmentApplyInput{
			LeaseID:       lease.ID,
			Index:         application.AdjustmentIndexIGPM,
			Rate:          4.5,
			EffectiveDate: "2026-10-01",
			Confirm:       true,
		})
	require.NoError(t, err)
	assert.Equal(t, application.AdjustmentNoticeDeterministic, result.NoticeSource)
	assert.Contains(t, result.Notice, "R$ 2.612,50")
}

func TestAdjustmentViewListsLeasesWithHistoryFromTrail(t *testing.T) {
	fixture := newAdjustmentFixture(t)
	active := fixture.seedLease(t, domain.Lease{ID: "lease-1", RentAmount: 2500})
	terminated := fixture.seedLease(t, domain.Lease{
		ID: "lease-2", Status: domain.LeaseStatusTerminated, PropertyID: fixture.propID,
		StartDate: "2024-01-01", EndDate: "2025-12-31", RentAmount: 1800,
	})

	_, err := application.ApplyAdjustment(context.Background(), fixture.deps, receiptsAuditContext(),
		application.AdjustmentApplyInput{
			LeaseID:       active.ID,
			Index:         application.AdjustmentIndexIGPM,
			Rate:          4.5,
			EffectiveDate: "2026-10-01",
			Confirm:       true,
		})
	require.NoError(t, err)

	view, err := application.BuildAdjustmentView(context.Background(), fixture.deps)
	require.NoError(t, err)

	assert.Equal(t, "2026-09-21", view.Today)
	assert.Equal(t, "2026-09", view.MonthKey)
	require.Len(t, view.Indices, 2)
	assert.Equal(t, application.AdjustmentIndexOption{Value: "igpm", Label: "IGP-M"}, view.Indices[0])
	assert.Equal(t, application.AdjustmentIndexOption{Value: "ipca", Label: "IPCA"}, view.Indices[1])

	require.Len(t, view.Leases, 2)
	// O contrato encerrado vai para o fim da lista.
	assert.Equal(t, active.ID, view.Leases[0].ID)
	assert.Equal(t, terminated.ID, view.Leases[1].ID)
	assert.False(t, view.Leases[1].Adjustable, "contrato encerrado não é reajustável")
	assert.True(t, view.Leases[0].Adjustable)

	row := view.Leases[0]
	assert.Equal(t, 2612.50, row.RentAmount, "a aba mostra o valor atual do contrato")
	assert.Equal(t, "R$ 2.612,50", row.RentAmountLabel)
	assert.Equal(t, "Ativo", row.StatusLabel)
	require.Len(t, row.History, 1)
	history := row.History[0]
	assert.Equal(t, "IGP-M", history.IndexLabel)
	assert.Equal(t, "4,5%", history.RateLabel)
	assert.Equal(t, "R$ 2.500,00", history.PreviousAmountLabel)
	assert.Equal(t, "R$ 2.612,50", history.NewAmountLabel)
	assert.Equal(t, "01/10/2026", history.EffectiveDateLabel)
	assert.NotEmpty(t, history.AppliedAtLabel)
	assert.Contains(t, history.Summary, "reajustado")

	assert.Empty(t, view.Leases[1].History, "o contrato sem reajuste não tem histórico")
}

func TestAdjustmentViewKeepsHistoryOfTheContractOnly(t *testing.T) {
	fixture := newAdjustmentFixture(t)
	first := fixture.seedLease(t, domain.Lease{ID: "lease-1", RentAmount: 2500})
	second := fixture.seedLease(t, domain.Lease{
		ID: "lease-2", PropertyID: fixture.propID, RentAmount: 1200,
		StartDate: "2027-01-01", EndDate: "2027-12-31",
	})

	for _, lease := range []domain.Lease{first, second} {
		_, err := application.ApplyAdjustment(context.Background(), fixture.deps, receiptsAuditContext(),
			application.AdjustmentApplyInput{
				LeaseID:       lease.ID,
				Index:         application.AdjustmentIndexIPCA,
				Rate:          10,
				EffectiveDate: "2026-11-01",
				Confirm:       true,
			})
		require.NoError(t, err)
	}

	view, err := application.BuildAdjustmentView(context.Background(), fixture.deps)
	require.NoError(t, err)
	require.Len(t, view.Leases, 2)
	for _, row := range view.Leases {
		require.Len(t, row.History, 1, "cada contrato carrega apenas o seu reajuste")
		assert.Equal(t, "IPCA", row.History[0].IndexLabel)
		assert.Equal(t, "10%", row.History[0].RateLabel)
	}
}

func requireAdjustmentInvalid(t *testing.T, err error, message string) {
	t.Helper()
	require.Error(t, err)
	assert.True(t, domain.IsValidation(err), "erro deveria ser de validação: %v", err)
	assert.Equal(t, message, domain.ValidationMessage(err))
}
