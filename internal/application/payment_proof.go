package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Baixa automática por comprovante.
//
// A esteira tem duas etapas de propósito: o upload só guarda o arquivo, cria o
// registro `pending` e enfileira o trabalho — a resposta HTTP não espera o
// provedor de leitura; quem lê o documento é o worker (`ProcessPaymentProof`).
//
// A IA só lê. Quem decide a baixa é `domain.EvaluatePaymentMatch`, e a baixa em
// si é a mesma do painel (`ReceiptPay`). Saída fora de formato do provedor é
// falha, nunca dado: sem extração confiável o comprovante vai para a fila de
// revisão humana e nenhum lançamento é quitado.

const (
	// paymentProofWindowDays é a janela de datas em torno do vencimento aceita
	// na conferência determinística do comprovante.
	paymentProofWindowDays = 10
	// paymentProofStorePrefix é o diretório dos comprovantes no armazenamento.
	paymentProofStorePrefix = "payment-proofs"
	// PaymentProofReviewLimit é o tamanho padrão da fila de revisão.
	PaymentProofReviewLimit = 50
)

// Motivos de revisão que nascem da própria leitura (os motivos de recusa
// automática vêm de `domain.EvaluatePaymentMatch`).
const (
	reasonProofUnreadable  = "não foi possível ler o comprovante"
	reasonProofNoExtractor = "provedor de leitura não configurado"
	reasonProofNoFile      = "arquivo do comprovante indisponível"
	reasonProofNotQueued   = "não foi possível enfileirar a leitura"
)

// Erros internos da leitura: dizem ao caso de uso se vale tentar de novo.
var (
	errProofExtractorUnavailable = errors.New("provedor de leitura não configurado")
	errProofFileUnavailable      = errors.New("arquivo do comprovante indisponível")
)

// PaymentProofInput é o comprovante recebido, ainda como arquivo.
type PaymentProofInput struct {
	Source   domain.PaymentProofSource
	Phone    string
	TenantID string
	FileName string
	Mime     string
	Bytes    []byte
}

// PaymentProofOutcome é o desfecho do envio e da leitura.
type PaymentProofOutcome struct {
	ProofID    string
	JobID      string
	Status     domain.PaymentProofStatus
	Decision   domain.PaymentDecision
	ReceiptID  string
	Reason     string
	Confidence float64
	Extracted  domain.PaymentExtraction
}

// PaymentProofJobPayload é o conteúdo do trabalho `payment-proof` na fila.
type PaymentProofJobPayload struct {
	ProofID string `json:"proofId"`
}

// SubmitPaymentProof guarda o comprovante, cria o registro `pending` e enfileira
// a leitura. Nenhuma chamada ao provedor de IA acontece aqui.
func SubmitPaymentProof(ctx context.Context, deps Deps, actx AuditContext, input PaymentProofInput) (*PaymentProofOutcome, error) {
	if deps.Attachments == nil {
		return nil, errors.New("armazenamento de anexos indisponível")
	}
	if len(input.Bytes) == 0 {
		return nil, domain.Invalid("Envie o arquivo do comprovante.")
	}
	if !supportedPaymentProofFile(input.Mime, input.FileName) {
		return nil, domain.Invalid("Formato de comprovante não suportado. Envie uma imagem ou um PDF.")
	}

	source := input.Source
	if source == "" {
		source = domain.PaymentProofSourcePanel
	}
	tenantID := strings.TrimSpace(input.TenantID)
	if tenantID != "" {
		if _, err := deps.Repos.Tenants.ByID(ctx, tenantID); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, domain.Invalid("Inquilino não encontrado.")
			}
			return nil, fmt.Errorf("buscar inquilino do comprovante: %w", err)
		}
	}

	stored, err := deps.Attachments.Save(paymentProofStorePrefix, input.FileName, input.Bytes)
	if err != nil {
		return nil, fmt.Errorf("guardar comprovante: %w", err)
	}

	now := deps.Clock.Now()
	proof := &domain.PaymentProof{
		ID:        deps.IDs.New(),
		CreatedAt: format.FormatUTCStamp(now),
		UpdatedAt: format.FormatUTCStamp(now),
		Source:    source,
		Phone:     optionalString(strings.TrimSpace(input.Phone)),
		TenantID:  optionalString(tenantID),
		FileName:  optionalString(strings.TrimSpace(input.FileName)),
		FilePath:  &stored,
		Mime:      optionalString(strings.TrimSpace(input.Mime)),
		Status:    domain.PaymentProofStatusPending,
		Decision:  domain.PaymentDecisionPending,
	}
	if err := deps.Repos.PaymentProofs.Create(ctx, proof); err != nil {
		return nil, fmt.Errorf("registrar comprovante: %w", err)
	}

	job, err := Enqueue(ctx, deps, domain.JobKindPaymentProof, PaymentProofJobPayload{ProofID: proof.ID}, 0)
	if err != nil {
		// Fila fora do ar não pode engolir o comprovante: ele vai para a revisão
		// humana com o motivo, em vez de sumir atrás de um 500.
		if _, reviewErr := reviewPaymentProof(ctx, deps, actx, proof, reasonProofNotQueued); reviewErr != nil {
			return nil, reviewErr
		}
		return outcomeFromProof(proof), nil
	}

	outcome := outcomeFromProof(proof)
	outcome.JobID = job.ID
	return outcome, nil
}

// ProcessPaymentProofJob é o handler do trabalho `payment-proof` registrado no
// worker (mesma assinatura de `JobHandler`). Devolve erro só quando a leitura
// pode ser tentada de novo; o comprovante nunca se perde — ele fica em revisão.
func ProcessPaymentProofJob(ctx context.Context, deps Deps, job domain.Job) error {
	var payload PaymentProofJobPayload
	if raw := strings.TrimSpace(domain.Str(job.Payload)); raw != "" {
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return fmt.Errorf("ler o conteúdo do trabalho %s: %w", job.ID, err)
		}
	}
	if strings.TrimSpace(payload.ProofID) == "" {
		return fmt.Errorf("trabalho %s sem comprovante", job.ID)
	}
	_, err := ProcessPaymentProof(ctx, deps, payload.ProofID)
	return err
}

// ProcessPaymentProof lê o comprovante (chamado pelo worker), confere contra o
// recibo aberto do titular e decide: aprovado dá baixa automática; qualquer
// dúvida vai para a revisão humana.
func ProcessPaymentProof(ctx context.Context, deps Deps, proofID string) (*PaymentProofOutcome, error) {
	id := strings.TrimSpace(proofID)
	if id == "" {
		return nil, domain.Invalid("Comprovante não informado.")
	}
	proof, err := deps.Repos.PaymentProofs.ByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("buscar comprovante: %w", err)
	}
	// O trabalho roda em segundo plano: a trilha registra a origem `job`.
	actx := AuditContext{Origin: domain.AuditOriginJob}

	// Decisão já tomada não é refeita — o trabalho pode ser reexecutado.
	if proof.Status == domain.PaymentProofStatusAccepted || proof.Status == domain.PaymentProofStatusRejected {
		return outcomeFromProof(proof), nil
	}

	extraction, raw, readErr := readPaymentProof(ctx, deps, proof)
	if readErr != nil {
		reason := reasonProofUnreadable
		switch {
		case errors.Is(readErr, errProofExtractorUnavailable):
			reason = reasonProofNoExtractor
		case errors.Is(readErr, errProofFileUnavailable):
			reason = reasonProofNoFile
		}
		if _, err := reviewPaymentProof(ctx, deps, actx, proof, reason); err != nil {
			return nil, err
		}
		logPaymentProof(deps, proof, reason)
		if errors.Is(readErr, errProofExtractorUnavailable) {
			// Configuração ausente não melhora na retentativa: o trabalho termina.
			return outcomeFromProof(proof), nil
		}
		// Provedor fora do ar ou arquivo ilegível ainda podem passar: o trabalho
		// falha e o worker tenta de novo.
		return outcomeFromProof(proof), fmt.Errorf("ler comprovante: %w", readErr)
	}

	applyPaymentExtraction(proof, extraction, raw, deps.Clock.Now())

	receipt, err := openReceiptForTenant(ctx, deps, domain.Str(proof.TenantID))
	if err != nil {
		return nil, err
	}
	landlordName, err := SettingValue(ctx, deps, domain.SettingLandlordName)
	if err != nil {
		return nil, err
	}
	landlordCPF, err := SettingValue(ctx, deps, domain.SettingLandlordCPF)
	if err != nil {
		return nil, err
	}

	result := domain.EvaluatePaymentMatch(domain.PaymentMatchInput{
		Extracted:     extraction,
		Receipt:       receipt,
		LandlordName:  landlordName,
		LandlordCPF:   landlordCPF,
		Today:         deps.Clock.Now(),
		MinConfidence: deps.Opts.AIConfidenceThreshold,
		WindowDays:    paymentProofWindowDays,
	})
	if !result.Approved {
		// O lançamento conferido fica gravado: é ele que a revisão mostra ao
		// lado do valor lido, e é nele que o aceite humano dá baixa.
		if receipt != nil {
			proof.ReceiptID = domain.StrPtr(receipt.ID)
		}
		if _, err := reviewPaymentProof(ctx, deps, actx, proof, result.Reason); err != nil {
			return nil, err
		}
		logPaymentProof(deps, proof, result.Reason)
		return outcomeFromProof(proof), nil
	}

	return acceptPaymentProof(ctx, deps, actx, proof, receipt, result.Reason)
}

// readPaymentProof abre o arquivo guardado e pede a leitura ao provedor.
func readPaymentProof(ctx context.Context, deps Deps, proof *domain.PaymentProof) (domain.PaymentExtraction, string, error) {
	if deps.Proofs == nil {
		return domain.PaymentExtraction{}, "", errProofExtractorUnavailable
	}
	if deps.Attachments == nil {
		return domain.PaymentExtraction{}, "", errProofFileUnavailable
	}
	content, err := deps.Attachments.Open(domain.Str(proof.FilePath))
	if err != nil {
		return domain.PaymentExtraction{}, "", fmt.Errorf("%w: %w", errProofFileUnavailable, err)
	}
	return deps.Proofs.ExtractPaymentProof(ctx, DocumentFile{
		Bytes:    content,
		Mime:     domain.Str(proof.Mime),
		FileName: domain.Str(proof.FileName),
	})
}

// applyPaymentExtraction copia para o registro o que a leitura devolveu.
func applyPaymentExtraction(proof *domain.PaymentProof, extraction domain.PaymentExtraction, raw string, now time.Time) {
	proof.ExtractedAmount = domain.FloatPtr(extraction.Amount)
	proof.ExtractedDate = optionalString(strings.TrimSpace(extraction.Date))
	proof.ExtractedPayer = optionalString(strings.TrimSpace(extraction.Payer))
	proof.ExtractedPayee = optionalString(strings.TrimSpace(extraction.Payee))
	proof.ExtractedIdentifier = optionalString(strings.TrimSpace(extraction.Identifier))
	proof.Confidence = domain.FloatPtr(extraction.Confidence)
	proof.RawResponse = optionalString(strings.TrimSpace(raw))
	proof.UpdatedAt = format.FormatUTCStamp(now)
}

// acceptPaymentProof dá baixa no lançamento e grava o veredito automático.
func acceptPaymentProof(ctx context.Context, deps Deps, actx AuditContext, proof *domain.PaymentProof, receipt *domain.Receipt, reason string) (*PaymentProofOutcome, error) {
	paidAt := paymentProofPaidAt(domain.Str(proof.ExtractedDate), deps.Clock.Now())
	if _, err := ReceiptPay(ctx, deps, actx, ReceiptPayInput{ID: receipt.ID, PaidAt: paidAt}); err != nil {
		return nil, fmt.Errorf("dar baixa pelo comprovante: %w", err)
	}

	now := deps.Clock.Now()
	proof.Status = domain.PaymentProofStatusAccepted
	proof.Decision = domain.PaymentDecisionAuto
	proof.ReceiptID = domain.StrPtr(receipt.ID)
	proof.DecisionReason = domain.StrPtr(reason)
	proof.DecidedBy = nil
	proof.DecidedAt = domain.StrPtr(format.FormatUTCStamp(now))
	proof.UpdatedAt = format.FormatUTCStamp(now)
	if err := deps.Repos.PaymentProofs.UpdateDecision(ctx, proof); err != nil {
		return nil, fmt.Errorf("gravar a decisão do comprovante: %w", err)
	}

	// Confirmação no WhatsApp: entra na fila depois da baixa gravada, para que o
	// locatário saiba da quitação sem que o envio segure a decisão.
	if tenant, err := tenantByID(ctx, deps, domain.Str(receipt.TenantID)); err != nil {
		deps.Log.Warn("titular do recibo não resolvido para a confirmação", "scope", "payments", "err", err)
	} else if err := EnqueuePaymentConfirmation(ctx, deps, proof, receipt, tenant); err != nil {
		deps.Log.Warn("confirmação de quitação não enfileirada", "scope", "payments", "err", err)
	}

	label := DescribeReceipt(receipt)
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionPaymentProofAccepted,
		Entity:      domain.EntityPayment,
		EntityID:    proof.ID,
		EntityLabel: paymentProofLabel(proof),
		Summary:     "Baixa automática por comprovante em " + label,
		Metadata: map[string]any{
			"receiptId":  receipt.ID,
			"decision":   string(domain.PaymentDecisionAuto),
			"confidence": domain.Float(proof.Confidence),
			"reason":     reason,
		},
	}); err != nil {
		return nil, err
	}
	logPaymentProof(deps, proof, reason)
	return outcomeFromProof(proof), nil
}

// reviewPaymentProof manda o comprovante para a fila de revisão humana.
func reviewPaymentProof(ctx context.Context, deps Deps, actx AuditContext, proof *domain.PaymentProof, reason string) (*domain.PaymentProof, error) {
	now := deps.Clock.Now()
	proof.Status = domain.PaymentProofStatusReview
	proof.Decision = domain.PaymentDecisionPending
	proof.DecisionReason = domain.StrPtr(reason)
	proof.UpdatedAt = format.FormatUTCStamp(now)
	if err := deps.Repos.PaymentProofs.UpdateDecision(ctx, proof); err != nil {
		return nil, fmt.Errorf("gravar comprovante em revisão: %w", err)
	}
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionPaymentProofReview,
		Entity:      domain.EntityPayment,
		EntityID:    proof.ID,
		EntityLabel: paymentProofLabel(proof),
		Summary:     "Comprovante em revisão: " + reason,
		Metadata: map[string]any{
			"reason":     reason,
			"confidence": domain.Float(proof.Confidence),
		},
	}); err != nil {
		return nil, err
	}
	return proof, nil
}

// openReceiptForTenant devolve o lançamento em aberto mais antigo por vencimento
// do titular. Sem titular informado não há lançamento para casar.
func openReceiptForTenant(ctx context.Context, deps Deps, tenantID string) (*domain.Receipt, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, nil
	}
	receipts, err := deps.Repos.Receipts.ByTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("buscar lançamentos do titular: %w", err)
	}
	oldest := -1
	for index := range receipts {
		if paid := receipts[index].PaidAt; paid != nil && *paid != "" {
			continue
		}
		if oldest < 0 || receipts[index].DueDate < receipts[oldest].DueDate {
			oldest = index
		}
	}
	if oldest < 0 {
		return nil, nil
	}
	return &receipts[oldest], nil
}

// paymentProofPaidAt usa a data do comprovante como data da baixa. Data ausente
// ou ilegível devolve vazio, que em `ReceiptPay` significa "agora".
func paymentProofPaidAt(date string, now time.Time) string {
	trimmed := strings.TrimSpace(date)
	if len(trimmed) > 10 {
		trimmed = trimmed[:10]
	}
	parsed, err := time.ParseInLocation("2006-01-02", trimmed, now.Location())
	if err != nil {
		return ""
	}
	return format.FormatLocalDateTime(parsed)
}

// outcomeFromProof traduz o registro no desfecho devolvido ao chamador.
func outcomeFromProof(proof *domain.PaymentProof) *PaymentProofOutcome {
	return &PaymentProofOutcome{
		ProofID:    proof.ID,
		Status:     proof.Status,
		Decision:   proof.Decision,
		ReceiptID:  domain.Str(proof.ReceiptID),
		Reason:     domain.Str(proof.DecisionReason),
		Confidence: domain.Float(proof.Confidence),
		Extracted:  proof.Extraction(),
	}
}

// paymentProofLabel é o rótulo do comprovante na trilha: o nome do arquivo, que
// é o que o humano reconhece. O conteúdo do documento nunca entra na trilha.
func paymentProofLabel(proof *domain.PaymentProof) string {
	if name := domain.Str(proof.FileName); name != "" {
		return name
	}
	return "Comprovante " + proof.ID
}

// logPaymentProof registra só o que não é dado pessoal: id, situação, motivo e
// confiança. O conteúdo lido e o arquivo nunca vão para o log.
func logPaymentProof(deps Deps, proof *domain.PaymentProof, reason string) {
	if deps.Log == nil {
		return
	}
	deps.Log.Info("comprovante processado",
		"scope", "payment-proof",
		"proof_id", proof.ID,
		"status", string(proof.Status),
		"decision", string(proof.Decision),
		"reason", reason,
		"confidence", domain.Float(proof.Confidence),
	)
}

// supportedPaymentProofFile aceita as imagens que o celular envia e o PDF do
// comprovante bancário.
func supportedPaymentProofFile(mime, filename string) bool {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg", "image/png", "image/webp", "image/heic", "image/heif", "application/pdf", "":
	default:
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(filename))
	for _, extension := range []string{".jpg", ".jpeg", ".png", ".webp", ".heic", ".heif", ".pdf"} {
		if strings.HasSuffix(lower, extension) {
			return true
		}
	}
	return strings.TrimSpace(mime) != ""
}
