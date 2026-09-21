package application_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// ── Fila de revisão ────────────────────────────────────────────────

func TestListPaymentProofReviewShowsWhatWasRead(t *testing.T) {
	fixture := newProofFixture(t)
	fixture.extractor.extraction.Amount = 2000
	outcome := fixture.submit(t)
	_, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.NoError(t, err)

	rows, err := application.ListPaymentProofReview(context.Background(), fixture.deps, 0)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	row := rows[0]
	assert.Equal(t, outcome.ProofID, row.ID)
	assert.Equal(t, "Maria Souza", row.TenantName)
	assert.InDelta(t, 2000.0, row.ExtractedAmount, 0.0001)
	assert.Equal(t, "2026-09-09", row.ExtractedDate)
	assert.Equal(t, "Carlos Drummond", row.ExtractedPayee)
	assert.InDelta(t, 0.95, row.Confidence, 0.0001)
	assert.Equal(t, "95%", row.ConfidenceLabel)
	assert.Equal(t, "valor do comprovante diferente do lançamento", row.Reason)
	assert.Equal(t, "Em revisão", row.StatusLabel)
	assert.Equal(t, "Pendente", row.DecisionLabel)
	assert.InDelta(t, 2500.0, row.ReceiptAmount, 0.0001)
	assert.Equal(t, "2026-09-10", row.ReceiptDueDate)
	assert.False(t, row.ReceiptPaid)
	assert.Equal(t, "/api/payments/proof/"+outcome.ProofID+"/file", row.FileURL)
	assert.Equal(t, "comprovante.jpg", row.FileName)
	assert.Equal(t, "comprovante.jpg · Maria Souza", row.DocumentLabel)
}

func TestListPaymentProofReviewLeavesDecidedProofsOut(t *testing.T) {
	fixture := newProofFixture(t)
	outcome := fixture.submit(t)
	_, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.NoError(t, err)

	rows, err := application.ListPaymentProofReview(context.Background(), fixture.deps, 10)
	require.NoError(t, err)
	assert.Empty(t, rows, "comprovante aceito não fica na fila")
}

// ── Decisão humana ─────────────────────────────────────────────────

func TestDecidePaymentProofApprovesAndPaysTheReceipt(t *testing.T) {
	fixture := newProofFixture(t)
	fixture.extractor.extraction.Amount = 2000
	outcome := fixture.submit(t)
	_, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.NoError(t, err)

	require.NoError(t, application.DecidePaymentProof(context.Background(), fixture.deps,
		application.AuditContext{Origin: domain.AuditOriginWeb, IP: "127.0.0.1"},
		application.DecideInput{
			ProofID:   outcome.ProofID,
			Approve:   true,
			ReceiptID: proofReceiptID,
			Reason:    "valor confere no extrato do banco",
			DecidedBy: "passoz",
		}))

	proof := fixture.proof(t, outcome.ProofID)
	assert.Equal(t, domain.PaymentProofStatusAccepted, proof.Status)
	assert.Equal(t, domain.PaymentDecisionManual, proof.Decision)
	assert.Equal(t, proofReceiptID, domain.Str(proof.ReceiptID))
	assert.Equal(t, "aceito na revisão: valor confere no extrato do banco", domain.Str(proof.DecisionReason))
	assert.Equal(t, "passoz", domain.Str(proof.DecidedBy))
	require.NotNil(t, proof.DecidedAt)

	receipt := fixture.receipt(t, proofReceiptID)
	require.NotNil(t, receipt.PaidAt)
	assert.Equal(t, "2026-09-09T00:00", *receipt.PaidAt, "a baixa usa a data do comprovante")
	require.NotNil(t, receipt.PaidAmount)
	assert.InDelta(t, 2500.0, *receipt.PaidAmount, 0.0001)

	assert.Equal(t, []domain.AuditOrigin{domain.AuditOriginWeb},
		fixture.auditOrigins(t, domain.ActionPaymentProofAccepted))
}

func TestDecidePaymentProofApprovesTheOpenReceiptOfTheTenant(t *testing.T) {
	fixture := newProofFixture(t)
	fixture.extractor.extraction.Amount = 2000
	outcome := fixture.submit(t)
	_, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.NoError(t, err)

	// Sem escolha explícita de lançamento, vale o que ficou em aberto no titular.
	require.NoError(t, application.DecidePaymentProof(context.Background(), fixture.deps,
		application.AuditContext{Origin: domain.AuditOriginWeb},
		application.DecideInput{ProofID: outcome.ProofID, Approve: true, DecidedBy: "passoz"}))

	assert.NotNil(t, fixture.receipt(t, proofReceiptID).PaidAt)
	assert.Equal(t, proofReceiptID, domain.Str(fixture.proof(t, outcome.ProofID).ReceiptID))
}

func TestDecidePaymentProofRejectRecordsTheReason(t *testing.T) {
	fixture := newProofFixture(t)
	fixture.extractor.extraction.Amount = 2000
	outcome := fixture.submit(t)
	_, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.NoError(t, err)

	require.NoError(t, application.DecidePaymentProof(context.Background(), fixture.deps,
		application.AuditContext{Origin: domain.AuditOriginWeb},
		application.DecideInput{
			ProofID:   outcome.ProofID,
			Reason:    "comprovante de outro mês",
			DecidedBy: "passoz",
		}))

	proof := fixture.proof(t, outcome.ProofID)
	assert.Equal(t, domain.PaymentProofStatusRejected, proof.Status)
	assert.Equal(t, domain.PaymentDecisionManual, proof.Decision)
	assert.Equal(t, "comprovante de outro mês", domain.Str(proof.DecisionReason))
	assert.Equal(t, "passoz", domain.Str(proof.DecidedBy))
	assert.Nil(t, fixture.receipt(t, proofReceiptID).PaidAt, "recusa não dá baixa")
	assert.Equal(t, []domain.AuditOrigin{domain.AuditOriginWeb},
		fixture.auditOrigins(t, domain.ActionPaymentProofRejected))
	assert.Contains(t, fixture.auditActions(t), domain.ActionPaymentProofRejected)
}

func TestDecidePaymentProofRequiresReasonToReject(t *testing.T) {
	fixture := newProofFixture(t)
	fixture.extractor.extraction.Amount = 2000
	outcome := fixture.submit(t)
	_, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.NoError(t, err)

	err = application.DecidePaymentProof(context.Background(), fixture.deps,
		application.AuditContext{Origin: domain.AuditOriginWeb},
		application.DecideInput{ProofID: outcome.ProofID})
	require.Error(t, err)
	assert.Equal(t, "Informe o motivo da recusa.", domain.ValidationMessage(err))
	assert.Equal(t, domain.PaymentProofStatusReview, fixture.proof(t, outcome.ProofID).Status)
}

func TestDecidePaymentProofDoesNotDecideTwice(t *testing.T) {
	fixture := newProofFixture(t)
	fixture.extractor.extraction.Amount = 2000
	outcome := fixture.submit(t)
	_, err := application.ProcessPaymentProof(context.Background(), fixture.deps, outcome.ProofID)
	require.NoError(t, err)
	require.NoError(t, application.DecidePaymentProof(context.Background(), fixture.deps,
		application.AuditContext{Origin: domain.AuditOriginWeb},
		application.DecideInput{ProofID: outcome.ProofID, Reason: "não confere", DecidedBy: "passoz"}))

	err = application.DecidePaymentProof(context.Background(), fixture.deps,
		application.AuditContext{Origin: domain.AuditOriginWeb},
		application.DecideInput{ProofID: outcome.ProofID, Approve: true, DecidedBy: "passoz"})
	require.Error(t, err)
	assert.Equal(t, "Este comprovante já foi decidido.", domain.ValidationMessage(err))
	assert.Equal(t, domain.PaymentProofStatusRejected, fixture.proof(t, outcome.ProofID).Status)
	assert.Nil(t, fixture.receipt(t, proofReceiptID).PaidAt)
}
