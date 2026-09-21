package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Fila de revisão humana dos comprovantes.
//
// A leitura automática só dá baixa quando todas as regras fecham. Tudo o que
// ficou em dúvida — valor diferente, destinatário que não é o locador, data fora
// da janela, confiança baixa, comprovante ilegível — aparece aqui com o que foi
// lido, a confiança e o motivo, para o locador decidir com o documento à vista.
// Nada é gravado a partir do documento sem esta confirmação.

// PaymentProofRow é uma linha da fila: o comprovante, o que a leitura achou e
// contra qual lançamento ele foi conferido.
type PaymentProofRow struct {
	ID                  string  `json:"id"`
	CreatedAt           string  `json:"createdAt"`
	Source              string  `json:"source"`
	SourceLabel         string  `json:"sourceLabel"`
	Phone               string  `json:"phone"`
	TenantID            string  `json:"tenantId"`
	TenantName          string  `json:"tenantName"`
	ReceiptID           string  `json:"receiptId"`
	ReceiptLabel        string  `json:"receiptLabel"`
	ReceiptAmount       float64 `json:"receiptAmount"`
	ReceiptDueDate      string  `json:"receiptDueDate"`
	ReceiptPaid         bool    `json:"receiptPaid"`
	ExtractedAmount     float64 `json:"extractedAmount"`
	ExtractedDate       string  `json:"extractedDate"`
	ExtractedPayer      string  `json:"extractedPayer"`
	ExtractedPayee      string  `json:"extractedPayee"`
	ExtractedIdentifier string  `json:"extractedIdentifier"`
	Confidence          float64 `json:"confidence"`
	ConfidenceLabel     string  `json:"confidenceLabel"`
	Status              string  `json:"status"`
	StatusLabel         string  `json:"statusLabel"`
	Decision            string  `json:"decision"`
	DecisionLabel       string  `json:"decisionLabel"`
	Reason              string  `json:"reason"`
	FileName            string  `json:"fileName"`
	DocumentLabel       string  `json:"documentLabel"`
	FileURL             string  `json:"fileUrl"`
	DecidedBy           string  `json:"decidedBy"`
	DecidedAt           string  `json:"decidedAt"`
}

// DecideInput é a decisão humana sobre um comprovante ambíguo.
type DecideInput struct {
	ProofID   string
	Approve   bool
	ReceiptID string
	Reason    string
	DecidedBy string
}

// ListPaymentProofReview devolve a fila de revisão, do comprovante mais recente
// para o mais antigo. `limit` não positivo cai no padrão.
func ListPaymentProofReview(ctx context.Context, deps Deps, limit int) ([]PaymentProofRow, error) {
	if limit <= 0 {
		limit = PaymentProofReviewLimit
	}
	proofs, err := deps.Repos.PaymentProofs.ListByStatus(ctx, domain.PaymentProofStatusReview, limit)
	if err != nil {
		return nil, fmt.Errorf("listar comprovantes em revisão: %w", err)
	}

	rows := make([]PaymentProofRow, 0, len(proofs))
	for index := range proofs {
		row, err := paymentProofRow(ctx, deps, &proofs[index])
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// DecidePaymentProof registra a decisão humana: o aceite dá baixa no lançamento
// escolhido (ou no lançamento em aberto do titular) e a recusa guarda o motivo.
// Nos dois casos a decisão fica marcada como manual na trilha.
func DecidePaymentProof(ctx context.Context, deps Deps, actx AuditContext, input DecideInput) error {
	proofID := strings.TrimSpace(input.ProofID)
	if proofID == "" {
		return domain.Invalid("Comprovante não informado.")
	}
	proof, err := deps.Repos.PaymentProofs.ByID(ctx, proofID)
	if err != nil {
		return fmt.Errorf("buscar comprovante: %w", err)
	}
	switch proof.Status {
	case domain.PaymentProofStatusAccepted, domain.PaymentProofStatusRejected:
		return domain.Invalid("Este comprovante já foi decidido.")
	}

	if input.Approve {
		return approvePaymentProof(ctx, deps, actx, proof, input)
	}
	return rejectPaymentProof(ctx, deps, actx, proof, input)
}

// approvePaymentProof dá baixa no lançamento e fecha o comprovante como aceito.
func approvePaymentProof(ctx context.Context, deps Deps, actx AuditContext, proof *domain.PaymentProof, input DecideInput) error {
	receiptID := strings.TrimSpace(input.ReceiptID)
	if receiptID == "" {
		// Sem escolha explícita vale o lançamento conferido na leitura; sem ele,
		// o lançamento em aberto do titular.
		receiptID = domain.Str(proof.ReceiptID)
	}
	if receiptID == "" {
		receipt, err := openReceiptForTenant(ctx, deps, domain.Str(proof.TenantID))
		if err != nil {
			return err
		}
		if receipt == nil {
			return domain.Invalid("Nenhum lançamento em aberto para dar baixa.")
		}
		receiptID = receipt.ID
	}

	paidAt := paymentProofPaidAt(domain.Str(proof.ExtractedDate), deps.Clock.Now())
	receipt, err := ReceiptPay(ctx, deps, actx, ReceiptPayInput{ID: receiptID, PaidAt: paidAt})
	if err != nil {
		return err
	}

	now := deps.Clock.Now()
	proof.Status = domain.PaymentProofStatusAccepted
	proof.Decision = domain.PaymentDecisionManual
	proof.ReceiptID = domain.StrPtr(receipt.ID)
	proof.DecisionReason = domain.StrPtr(paymentProofDecisionReason(input.Reason, "aceito na revisão"))
	proof.DecidedBy = optionalString(strings.TrimSpace(input.DecidedBy))
	proof.DecidedAt = domain.StrPtr(format.FormatUTCStamp(now))
	proof.UpdatedAt = format.FormatUTCStamp(now)
	if err := deps.Repos.PaymentProofs.UpdateDecision(ctx, proof); err != nil {
		return fmt.Errorf("gravar o aceite do comprovante: %w", err)
	}

	// O aceite humano também confirma a quitação pelo WhatsApp, pela mesma fila.
	if tenant, err := tenantByID(ctx, deps, domain.Str(receipt.TenantID)); err != nil {
		deps.Log.Warn("titular do recibo não resolvido para a confirmação", "scope", "payments", "err", err)
	} else if err := EnqueuePaymentConfirmation(ctx, deps, proof, receipt, tenant); err != nil {
		deps.Log.Warn("confirmação de quitação não enfileirada", "scope", "payments", "err", err)
	}

	return Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionPaymentProofAccepted,
		Entity:      domain.EntityPayment,
		EntityID:    proof.ID,
		EntityLabel: paymentProofLabel(proof),
		Summary:     "Comprovante aceito na revisão em " + DescribeReceipt(receipt),
		Metadata: map[string]any{
			"receiptId": receipt.ID,
			"decision":  string(domain.PaymentDecisionManual),
			"decidedBy": domain.Str(proof.DecidedBy),
			"reason":    domain.Str(proof.DecisionReason),
		},
	})
}

// rejectPaymentProof fecha o comprovante como recusado, com o motivo informado.
func rejectPaymentProof(ctx context.Context, deps Deps, actx AuditContext, proof *domain.PaymentProof, input DecideInput) error {
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return domain.Invalid("Informe o motivo da recusa.")
	}

	now := deps.Clock.Now()
	proof.Status = domain.PaymentProofStatusRejected
	proof.Decision = domain.PaymentDecisionManual
	proof.DecisionReason = domain.StrPtr(reason)
	proof.DecidedBy = optionalString(strings.TrimSpace(input.DecidedBy))
	proof.DecidedAt = domain.StrPtr(format.FormatUTCStamp(now))
	proof.UpdatedAt = format.FormatUTCStamp(now)
	if err := deps.Repos.PaymentProofs.UpdateDecision(ctx, proof); err != nil {
		return fmt.Errorf("gravar a recusa do comprovante: %w", err)
	}
	return Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionPaymentProofRejected,
		Entity:      domain.EntityPayment,
		EntityID:    proof.ID,
		EntityLabel: paymentProofLabel(proof),
		Summary:     "Comprovante recusado na revisão: " + reason,
		Metadata: map[string]any{
			"decision":  string(domain.PaymentDecisionManual),
			"decidedBy": domain.Str(proof.DecidedBy),
			"reason":    reason,
		},
	})
}

// paymentProofRow monta a linha da fila, juntando o titular e o lançamento
// conferido. Registro apagado no meio do caminho não derruba a fila: a linha
// sai sem o dado que sumiu.
func paymentProofRow(ctx context.Context, deps Deps, proof *domain.PaymentProof) (PaymentProofRow, error) {
	row := PaymentProofRow{
		ID:                  proof.ID,
		CreatedAt:           proof.CreatedAt,
		Source:              string(proof.Source),
		SourceLabel:         paymentProofSourceLabel(proof.Source),
		Phone:               domain.Str(proof.Phone),
		TenantID:            domain.Str(proof.TenantID),
		ReceiptID:           domain.Str(proof.ReceiptID),
		ExtractedAmount:     domain.Float(proof.ExtractedAmount),
		ExtractedDate:       domain.Str(proof.ExtractedDate),
		ExtractedPayer:      domain.Str(proof.ExtractedPayer),
		ExtractedPayee:      domain.Str(proof.ExtractedPayee),
		ExtractedIdentifier: domain.Str(proof.ExtractedIdentifier),
		Confidence:          domain.Float(proof.Confidence),
		ConfidenceLabel:     paymentProofConfidenceLabel(domain.Float(proof.Confidence)),
		Status:              string(proof.Status),
		StatusLabel:         domain.PaymentProofStatusLabel(proof.Status),
		Decision:            string(proof.Decision),
		DecisionLabel:       domain.PaymentDecisionLabel(proof.Decision),
		Reason:              domain.Str(proof.DecisionReason),
		FileName:            domain.Str(proof.FileName),
		FileURL:             paymentProofFileURL(proof),
		DecidedBy:           domain.Str(proof.DecidedBy),
		DecidedAt:           domain.Str(proof.DecidedAt),
	}

	if tenantID := domain.Str(proof.TenantID); tenantID != "" {
		tenant, err := deps.Repos.Tenants.ByID(ctx, tenantID)
		switch {
		case err == nil:
			row.TenantName = tenant.Name
		case !errors.Is(err, domain.ErrNotFound):
			return row, fmt.Errorf("buscar titular do comprovante: %w", err)
		}
	}

	if receiptID := domain.Str(proof.ReceiptID); receiptID != "" {
		receipt, err := deps.Repos.Receipts.ByID(ctx, receiptID)
		switch {
		case err == nil:
			row.ReceiptAmount = receipt.Amount
			row.ReceiptDueDate = receipt.DueDate
			row.ReceiptLabel = DescribeReceipt(receipt)
			row.ReceiptPaid = receipt.PaidAt != nil && *receipt.PaidAt != ""
		case !errors.Is(err, domain.ErrNotFound):
			return row, fmt.Errorf("buscar lançamento do comprovante: %w", err)
		}
	}

	row.DocumentLabel = paymentProofDocumentLabel(proof, row.TenantName)
	return row, nil
}

// paymentProofDocumentLabel identifica o comprovante na revisão: o arquivo e,
// quando há, o titular.
func paymentProofDocumentLabel(proof *domain.PaymentProof, tenantName string) string {
	label := domain.Str(proof.FileName)
	if label == "" {
		label = "Comprovante " + proof.ID
	}
	if tenantName != "" {
		return label + " · " + tenantName
	}
	return label
}

// paymentProofFileURL é o endereço do arquivo do comprovante na revisão.
func paymentProofFileURL(proof *domain.PaymentProof) string {
	if domain.Str(proof.FilePath) == "" {
		return ""
	}
	return "/api/payments/proof/" + proof.ID + "/file"
}

// paymentProofDecisionReason junta o motivo do humano ao desfecho padrão.
func paymentProofDecisionReason(reason, fallback string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fallback
	}
	return fallback + ": " + reason
}

// paymentProofConfidenceLabel mostra a confiança da leitura em porcentagem.
func paymentProofConfidenceLabel(confidence float64) string {
	if confidence <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", confidence*100)
}

// paymentProofSourceLabel é o rótulo em português da origem do comprovante.
func paymentProofSourceLabel(source domain.PaymentProofSource) string {
	switch source {
	case domain.PaymentProofSourceWhatsApp:
		return "WhatsApp"
	case domain.PaymentProofSourcePortal:
		return "Área do cliente"
	case domain.PaymentProofSourcePanel:
		return "Painel"
	default:
		return string(source)
	}
}
