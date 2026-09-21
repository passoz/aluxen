package application

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/domain"
)

// Régua de cobrança: estágio por atraso, histórico que impede repetir o mesmo
// estágio, envio pelo canal e registro na trilha. Os dublês são os de
// `agent_test.go` (`newAgentFixture`), compartilhados pelas duas áreas.

func agCollectionContext() AuditContext {
	return AuditContext{Origin: domain.AuditOriginJob}
}

func agStageOf(t *testing.T, candidates []CollectionCandidate, receiptID string) domain.CollectionStage {
	t.Helper()
	for index := range candidates {
		if candidates[index].ReceiptID == receiptID {
			return candidates[index].Stage
		}
	}
	t.Fatalf("lançamento %q fora dos candidatos: %v", receiptID, agCandidateIDs(candidates))
	return 0
}

// ── Decisão do estágio ────────────────────────────────────────────

func TestPlanCollectionStagesByDelay(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.addReceipt(agReceipt("receipt-friendly", agTenantID, "2026-09-18", 900))     // 3 dias
	fixture.addReceipt(agReceipt("receipt-firm", agTenantID, "2026-09-14", 1200))        // 7 dias
	fixture.addReceipt(agReceipt("receipt-installment", agTenantID, "2026-09-01", 2000)) // 20 dias
	fixture.addReceipt(agPaidReceipt("receipt-paid", agTenantID, "2026-08-10", 800))

	candidates, err := PlanCollection(context.Background(), fixture.deps, agToday())
	require.NoError(t, err)

	// Vencimento mais antigo primeiro; o recibo pago não entra na régua.
	require.Equal(t,
		[]string{"receipt-installment", "receipt-firm", "receipt-friendly"},
		agCandidateIDs(candidates))

	assert.Equal(t, domain.CollectionStageInstallment, agStageOf(t, candidates, "receipt-installment"))
	assert.Equal(t, domain.CollectionStageFirm, agStageOf(t, candidates, "receipt-firm"))
	assert.Equal(t, domain.CollectionStageFriendly, agStageOf(t, candidates, "receipt-friendly"))

	installment := candidates[0]
	assert.Equal(t, 20, installment.DaysLate)
	assert.Equal(t, "Proposta de parcelamento", installment.StageLabel)
	assert.Equal(t, domain.CollectionStage(0), installment.LastStage)
	assert.Contains(t, installment.Message, "R$ 2.000,00")
	assert.Contains(t, installment.Message, "01/09/2026")
	assert.Contains(t, installment.Message, "20 dias de atraso")
}

func TestPlanCollectionSkipsStageAlreadySent(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.addReceipt(agReceipt("receipt-sent", agTenantID, "2026-09-18", 900)) // 3 dias, estágio 1 já enviado
	fixture.addReceipt(agReceipt("receipt-next", agTenantID, "2026-09-14", 1200))
	fixture.addCollectionSent(t, "receipt-sent", domain.CollectionStageFriendly)
	fixture.addCollectionSent(t, "receipt-next", domain.CollectionStageFriendly)

	candidates, err := PlanCollection(context.Background(), fixture.deps, agToday())
	require.NoError(t, err)

	// O lembrete amigável já foi: o de 3 dias sai da régua; o de 7 dias avança
	// para a cobrança firme.
	require.Equal(t, []string{"receipt-next"}, agCandidateIDs(candidates))
	assert.Equal(t, domain.CollectionStageFirm, candidates[0].Stage)
	assert.Equal(t, domain.CollectionStageFriendly, candidates[0].LastStage)
	assert.Equal(t, "Lembrete amigável", candidates[0].LastStageLabel)
}

func TestPlanCollectionIgnoresUnreadableHistory(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.addReceipt(agReceipt(agReceiptID, agTenantID, "2026-09-18", 900))

	broken := "não é json"
	require.NoError(t, fixture.audit.Insert(context.Background(), &domain.AuditLog{
		ID: "audit-broken", CreatedAt: agStamp, Action: domain.ActionCollectionSent,
		Entity: domain.EntityCollection, EntityID: domain.StrPtr(agReceiptID),
		Summary: "Cobrança enviada", Metadata: &broken, Origin: domain.AuditOriginWeb,
	}))

	candidates, err := PlanCollection(context.Background(), fixture.deps, agToday())
	require.NoError(t, err)

	// Metadado ilegível não vira estágio por adivinhação nem trava a régua.
	require.Len(t, candidates, 1)
	assert.Equal(t, domain.CollectionStageFriendly, candidates[0].Stage)
}

func TestPlanCollectionWithoutTrailStillWorks(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.addReceipt(agReceipt(agReceiptID, agTenantID, "2026-09-01", 2000))
	fixture.deps.Repos.Audit = nil

	candidates, err := PlanCollection(context.Background(), fixture.deps, agToday())
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, domain.CollectionStageInstallment, candidates[0].Stage)
}

func TestCollectionMessageIsDeterministicAndQuotesReceipt(t *testing.T) {
	candidate := CollectionCandidate{
		ReceiptID:    agReceiptID,
		TenantName:   "Maria Souza",
		DueDate:      "2026-09-10",
		Amount:       2500,
		DaysLate:     11,
		LandlordName: "João Locador",
	}

	for _, stage := range []domain.CollectionStage{
		domain.CollectionStageFriendly,
		domain.CollectionStageFirm,
		domain.CollectionStageInstallment,
	} {
		candidate.Stage = stage
		message := CollectionMessage(candidate)
		assert.Equal(t, message, CollectionMessage(candidate), "o texto do estágio não varia")
		assert.Contains(t, message, "R$ 2.500,00", "estágio %d", stage)
		assert.Contains(t, message, "10/09/2026", "estágio %d", stage)
		assert.Contains(t, message, "11 dias de atraso", "estágio %d", stage)
		assert.Contains(t, message, "João Locador", "estágio %d", stage)
		assert.Contains(t, message, "Maria", "estágio %d", stage)
	}
}

// ── Envio ─────────────────────────────────────────────────────────

func TestRunCollectionSendsStageAndAudits(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.addReceipt(agReceipt(agReceiptID, agTenantID, "2026-09-01", 2000))
	channel := &agChannel{}

	outcomes, err := RunCollectionWithChannel(context.Background(), fixture.deps, channel, agCollectionContext(), false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)

	outcome := outcomes[0]
	assert.True(t, outcome.Delivered)
	assert.Equal(t, CollectionChannelWhatsApp, outcome.Channel)
	assert.Equal(t, domain.CollectionStageInstallment, outcome.Stage)
	assert.Equal(t, "Proposta de parcelamento", outcome.StageLabel)
	assert.Empty(t, outcome.Reason)

	sent := channel.sentMessages()
	require.Len(t, sent, 1)
	assert.Equal(t, agPhone, sent[0].phone)
	assert.Equal(t, outcome.Message, sent[0].text)
	assert.Contains(t, sent[0].text, "R$ 2.000,00")

	entry := fixture.auditOf(t, domain.ActionCollectionSent)
	assert.Equal(t, agReceiptID, domain.Str(entry.EntityID))
	metadata := agMetadata(t, entry)
	assert.Equal(t, float64(domain.CollectionStageInstallment), metadata["stage"])
	assert.Equal(t, CollectionChannelWhatsApp, metadata["channel"])
	assert.Equal(t, agReceiptID, metadata["receiptId"])
	assert.Equal(t, 2000.0, metadata["amount"])
	assert.Equal(t, "****9999", metadata["phone"])

	// Quem registra a mensagem enviada é o eco do canal; a régua não duplica o
	// histórico da conversa.
	assert.Empty(t, fixture.messages.stored())
}

func TestRunCollectionDryRunSendsNothing(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.addReceipt(agReceipt(agReceiptID, agTenantID, "2026-09-14", 1200))
	channel := &agChannel{}

	outcomes, err := RunCollectionWithChannel(context.Background(), fixture.deps, channel, agCollectionContext(), true)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)

	assert.True(t, outcomes[0].DryRun)
	assert.False(t, outcomes[0].Delivered)
	assert.Empty(t, outcomes[0].Channel)
	assert.NotEmpty(t, outcomes[0].Message, "a simulação mostra o que sairia")

	assert.Empty(t, channel.sentMessages())
	assert.Empty(t, fixture.messages.stored())
	assert.Empty(t, fixture.auditActions())
}

func TestRunCollectionWithoutChannelRegistersPending(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.addReceipt(agReceipt(agReceiptID, agTenantID, "2026-09-14", 1200))

	outcomes, err := RunCollection(context.Background(), fixture.deps, agCollectionContext(), false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)

	assert.False(t, outcomes[0].Delivered)
	assert.Equal(t, CollectionChannelPending, outcomes[0].Channel)
	assert.Contains(t, outcomes[0].Reason, "pendente")

	stored := fixture.messages.stored()
	require.Len(t, stored, 1)
	assert.Equal(t, domain.WhatsAppStatusPending, stored[0].Status)
	assert.Equal(t, domain.WhatsAppDirectionOut, stored[0].Direction)
	assert.Equal(t, agTenantID, domain.Str(stored[0].TenantID))
	assert.Equal(t, "5511999999999@c.us", stored[0].ChatID)
	assert.Equal(t, outcomes[0].Message, stored[0].Body)

	metadata := agMetadata(t, fixture.auditOf(t, domain.ActionCollectionSent))
	assert.Equal(t, CollectionChannelPending, metadata["channel"])
}

func TestRunCollectionStopsAtPaidReceipt(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.addReceipt(agPaidReceipt(agReceiptID, agTenantID, "2026-09-01", 2000))
	channel := &agChannel{}

	outcomes, err := RunCollectionWithChannel(context.Background(), fixture.deps, channel, agCollectionContext(), false)
	require.NoError(t, err)

	assert.Empty(t, outcomes, "recibo pago interrompe a régua")
	assert.Empty(t, channel.sentMessages())
	assert.Empty(t, fixture.auditActions())
	assert.Empty(t, fixture.messages.stored())
}

func TestRunCollectionSkipsTenantWithoutPhone(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", ""))
	fixture.addReceipt(agReceipt(agReceiptID, agTenantID, "2026-09-01", 2000))
	channel := &agChannel{}

	outcomes, err := RunCollectionWithChannel(context.Background(), fixture.deps, channel, agCollectionContext(), false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)

	assert.False(t, outcomes[0].Delivered)
	assert.Contains(t, outcomes[0].Reason, "telefone")
	assert.Empty(t, channel.sentMessages())
	assert.Empty(t, fixture.auditActions(), "sem destinatário não há envio a registrar")
	assert.Empty(t, fixture.messages.stored())
}

func TestRunCollectionDoesNotAdvanceOnChannelFailure(t *testing.T) {
	fixture := newAgentFixture(t)
	fixture.addTenant(agTenant(agTenantID, "Maria Souza", agPhone))
	fixture.addReceipt(agReceipt(agReceiptID, agTenantID, "2026-09-01", 2000))
	channel := &agChannel{err: errors.New("WAHA indisponível")}

	outcomes, err := RunCollectionWithChannel(context.Background(), fixture.deps, channel, agCollectionContext(), false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)

	assert.False(t, outcomes[0].Delivered)
	assert.Contains(t, outcomes[0].Reason, "Falha")

	// Sem evento `collection.sent`, o estágio é tentado de novo na próxima
	// rodada — a falha de canal não avança a régua em silêncio.
	actions := fixture.auditActions()
	assert.NotContains(t, actions, domain.ActionCollectionSent)
	assert.Contains(t, actions, domain.ActionWhatsAppSendFailed)

	planned, err := PlanCollection(context.Background(), fixture.deps, agToday())
	require.NoError(t, err)
	assert.Len(t, planned, 1)
}

// ── Estágios (domínio) ────────────────────────────────────────────

func TestCollectionStageByDelay(t *testing.T) {
	cases := []struct {
		days int
		want domain.CollectionStage
		ok   bool
	}{
		{0, 0, false},
		{1, domain.CollectionStageFriendly, true},
		{6, domain.CollectionStageFriendly, true},
		{7, domain.CollectionStageFirm, true},
		{14, domain.CollectionStageFirm, true},
		{15, domain.CollectionStageInstallment, true},
		{90, domain.CollectionStageInstallment, true},
	}
	for _, testCase := range cases {
		stage, ok := domain.CollectionStageByDelay(testCase.days)
		assert.Equal(t, testCase.ok, ok, "%d dias", testCase.days)
		assert.Equal(t, testCase.want, stage, "%d dias", testCase.days)
	}

	value, ok := domain.CollectionStageFromValue(9)
	assert.False(t, ok)
	assert.Zero(t, value)

	options := domain.CollectionStageOptions()
	require.Len(t, options, 3)
	assert.Equal(t, 1, options[0].Value)
	assert.Equal(t, "Lembrete amigável", options[0].Label)
	assert.Equal(t, domain.CollectionFirmDays, options[1].FromDays)
	assert.Equal(t, "Proposta de parcelamento", options[2].Label)
}
