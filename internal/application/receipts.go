package application

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Lançamentos financeiros: aluguel mensal, cobrança avulsa, baixa (integral ou
// parcial), geração mensal idempotente e exclusão — em lote quando o painel
// envia vários ids. As mensagens de erro são as mesmas do app atual e cada
// operação deixa a sua linha na trilha.

// ReceiptInput são os campos do lançamento informados no painel. Os valores
// chegam como texto (o formulário é a origem) e são interpretados aqui.
type ReceiptInput struct {
	// ID é o identificador opcional enviado pelo formulário (a fila offline
	// preserva o id gerado no cliente). Vazio gera um id novo.
	ID          string
	LeaseID     string
	TenantID    string
	PropertyID  string
	Category    string
	Description string
	DueDate     string
	PaidAt      string
	Amount      string
	PaidAmount  string
}

// ReceiptPayInput são os campos da baixa de um lançamento. PayAmount vazio
// significa baixa pelo valor total; PaidAt vazio significa agora.
type ReceiptPayInput struct {
	ID        string
	PayAmount string
	PaidAt    string
}

// ReceiptCreate lança o pagamento e registra a criação na trilha.
func ReceiptCreate(ctx context.Context, deps Deps, actx AuditContext, input ReceiptInput) (*domain.Receipt, error) {
	fields, err := receiptFieldsFrom(ctx, deps, input)
	if err != nil {
		return nil, err
	}

	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = deps.IDs.New()
	}
	receipt := fields.apply(&domain.Receipt{
		ID:        id,
		CreatedAt: format.FormatUTCStamp(deps.Clock.Now()),
	})

	if err := deps.Repos.Receipts.Create(ctx, receipt); err != nil {
		return nil, fmt.Errorf("lançar pagamento: %w", err)
	}

	created, err := deps.Repos.Receipts.ByID(ctx, receipt.ID)
	if err != nil {
		return nil, fmt.Errorf("buscar lançamento criado: %w", err)
	}
	label := DescribeReceipt(created)
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionReceiptCreate,
		Entity:      domain.EntityReceipt,
		EntityID:    created.ID,
		EntityLabel: label,
		Summary:     "Pagamento lançado para " + label,
		Changes:     domain.BuildAuditChanges(domain.ReceiptAuditFields, nil, ReceiptSnapshot(created)),
	}); err != nil {
		return nil, err
	}
	return created, nil
}

// ReceiptUpdate altera o lançamento e registra o diff na trilha.
func ReceiptUpdate(ctx context.Context, deps Deps, actx AuditContext, input ReceiptInput) (*domain.Receipt, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return nil, domain.Invalid("Registro não informado.")
	}
	fields, err := receiptFieldsFrom(ctx, deps, input)
	if err != nil {
		return nil, err
	}

	before, err := deps.Repos.Receipts.ByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("buscar lançamento: %w", err)
	}

	after := *before
	fields.apply(&after)
	after.UpdatedAt = domain.StrPtr(format.FormatUTCStamp(deps.Clock.Now()))

	if err := deps.Repos.Receipts.Update(ctx, &after); err != nil {
		return nil, fmt.Errorf("alterar lançamento: %w", err)
	}

	updated, err := deps.Repos.Receipts.ByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("buscar lançamento alterado: %w", err)
	}
	label := DescribeReceipt(updated)
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionReceiptUpdate,
		Entity:      domain.EntityReceipt,
		EntityID:    updated.ID,
		EntityLabel: label,
		Summary:     "Pagamento de " + label + " alterado",
		Changes:     domain.BuildAuditChanges(domain.ReceiptAuditFields, ReceiptSnapshot(before), ReceiptSnapshot(updated)),
	}); err != nil {
		return nil, err
	}
	return updated, nil
}

// ReceiptPay dá baixa no lançamento (integral ou parcial) e registra na trilha.
func ReceiptPay(ctx context.Context, deps Deps, actx AuditContext, input ReceiptPayInput) (*domain.Receipt, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return nil, domain.Invalid("Registro não informado.")
	}
	receipt, err := deps.Repos.Receipts.ByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("buscar lançamento: %w", err)
	}

	// Sem valor informado, a baixa é pelo valor total do lançamento.
	payAmount := receipt.Amount
	if raw := strings.TrimSpace(input.PayAmount); raw != "" {
		parsed, ok := receiptMoney(raw)
		if !ok {
			return nil, domain.Invalid("Informe o valor pago.")
		}
		payAmount = parsed
	}
	paidAt := strings.TrimSpace(input.PaidAt)
	if paidAt == "" {
		paidAt = format.FormatLocalDateTime(deps.Clock.Now())
	}

	if err := deps.Repos.Receipts.UpdatePayment(ctx, receipt.ID, paidAt, payAmount, format.FormatUTCStamp(deps.Clock.Now())); err != nil {
		return nil, fmt.Errorf("dar baixa no lançamento: %w", err)
	}

	updated, err := deps.Repos.Receipts.ByID(ctx, receipt.ID)
	if err != nil {
		return nil, fmt.Errorf("buscar lançamento baixado: %w", err)
	}
	label := DescribeReceipt(updated)
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionReceiptPay,
		Entity:      domain.EntityReceipt,
		EntityID:    updated.ID,
		EntityLabel: label,
		Summary:     "Baixa de " + label + " registrada",
		Changes:     domain.BuildAuditChanges(domain.ReceiptAuditFields, ReceiptSnapshot(receipt), ReceiptSnapshot(updated)),
	}); err != nil {
		return nil, err
	}
	return updated, nil
}

// ReceiptBatchPay dá baixa em vários lançamentos pelo valor total de cada um,
// com o mesmo instante de pagamento — uma linha de trilha por lançamento.
func ReceiptBatchPay(ctx context.Context, deps Deps, actx AuditContext, ids []string) error {
	selected, err := receiptIDs(ids)
	if err != nil {
		return err
	}

	now := deps.Clock.Now()
	paidAt := format.FormatLocalDateTime(now)
	stamp := format.FormatUTCStamp(now)

	// Os lançamentos são lidos antes de qualquer escrita: id desconhecido
	// interrompe o lote sem deixar baixa pela metade.
	before := make([]*domain.Receipt, 0, len(selected))
	for _, id := range selected {
		receipt, err := deps.Repos.Receipts.ByID(ctx, id)
		if err != nil {
			return fmt.Errorf("buscar lançamento do lote: %w", err)
		}
		before = append(before, receipt)
	}
	for _, receipt := range before {
		if err := deps.Repos.Receipts.UpdatePayment(ctx, receipt.ID, paidAt, receipt.Amount, stamp); err != nil {
			return fmt.Errorf("dar baixa no lote: %w", err)
		}
	}
	for _, receipt := range before {
		after, err := deps.Repos.Receipts.ByID(ctx, receipt.ID)
		if err != nil {
			return fmt.Errorf("buscar lançamento do lote: %w", err)
		}
		label := DescribeReceipt(after)
		if err := Record(ctx, deps, actx, AuditEntry{
			Action:      domain.ActionReceiptBatchPay,
			Entity:      domain.EntityReceipt,
			EntityID:    after.ID,
			EntityLabel: label,
			Summary:     "Baixa em lote de " + label + " registrada",
			Changes:     domain.BuildAuditChanges(domain.ReceiptAuditFields, ReceiptSnapshot(receipt), ReceiptSnapshot(after)),
			Metadata:    map[string]any{"batch": len(selected)},
		}); err != nil {
			return err
		}
	}
	return nil
}

// ReceiptGenerateMonthly gera os aluguéis do mês corrente para os contratos
// ativos vigentes no mês. A chave `generated_key` (`<contrato>:<AAAA-MM>`) torna
// a geração idempotente: repetir a chamada não duplica lançamento nenhum.
// Devolve quantos lançamentos foram realmente criados.
func ReceiptGenerateMonthly(ctx context.Context, deps Deps, actx AuditContext) (int, error) {
	now := deps.Clock.Now()
	year, month := now.Year(), int(now.Month())
	monthKey := domain.MonthKey(year, month)

	leases, err := deps.Repos.Leases.ActiveInRange(ctx, monthKey+"-01", monthEndKey(year, month))
	if err != nil {
		return 0, fmt.Errorf("listar contratos vigentes no mês: %w", err)
	}

	stamp := format.FormatUTCStamp(now)
	inserted := 0
	for _, lease := range leases {
		receipt := &domain.Receipt{
			ID:           deps.IDs.New(),
			LeaseID:      domain.StrPtr(lease.ID),
			TenantID:     domain.StrPtr(lease.TenantID),
			PropertyID:   domain.StrPtr(lease.PropertyID),
			Category:     domain.ReceiptCategoryRent,
			DueDate:      domain.DueDateForMonth(year, month, lease.DueDay),
			Amount:       lease.RentAmount,
			GeneratedKey: domain.StrPtr(domain.GeneratedKeyForMonth(lease.ID, year, month)),
			CreatedAt:    stamp,
		}
		rows, err := deps.Repos.Receipts.CreateIfAbsent(ctx, receipt)
		if err != nil {
			return inserted, fmt.Errorf("gerar pagamento do mês: %w", err)
		}
		inserted += int(rows)
	}

	competence := fmt.Sprintf("%02d/%04d", month, year)
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionReceiptGenerateMonth,
		Entity:      domain.EntityReceipt,
		EntityID:    monthKey,
		EntityLabel: "Competência " + competence,
		Summary:     fmt.Sprintf("Geração automática de %d pagamento(s) para %s", inserted, competence),
		Metadata:    map[string]any{"month": monthKey, "count": inserted},
	}); err != nil {
		return inserted, err
	}
	return inserted, nil
}

// ReceiptDelete exclui o lançamento e registra a exclusão na trilha.
func ReceiptDelete(ctx context.Context, deps Deps, actx AuditContext, id string) error {
	receiptID := strings.TrimSpace(id)
	if receiptID == "" {
		return domain.Invalid("Registro não informado.")
	}

	before, err := deps.Repos.Receipts.ByID(ctx, receiptID)
	if err != nil {
		return fmt.Errorf("buscar lançamento: %w", err)
	}
	if err := deps.Repos.Receipts.Delete(ctx, receiptID); err != nil {
		return fmt.Errorf("excluir lançamento: %w", err)
	}

	label := DescribeReceipt(before)
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionReceiptDelete,
		Entity:      domain.EntityReceipt,
		EntityID:    before.ID,
		EntityLabel: label,
		Summary:     "Pagamento de " + label + " excluído",
		Changes:     domain.BuildAuditChanges(domain.ReceiptAuditFields, ReceiptSnapshot(before), nil),
	}); err != nil {
		return err
	}
	return nil
}

// ReceiptBatchDelete exclui vários lançamentos, um a um (nada de `IN (...)`
// dinâmico), com uma linha de trilha por lançamento.
func ReceiptBatchDelete(ctx context.Context, deps Deps, actx AuditContext, ids []string) error {
	selected, err := receiptIDs(ids)
	if err != nil {
		return err
	}

	before := make([]*domain.Receipt, 0, len(selected))
	for _, id := range selected {
		receipt, err := deps.Repos.Receipts.ByID(ctx, id)
		if err != nil {
			return fmt.Errorf("buscar lançamento do lote: %w", err)
		}
		before = append(before, receipt)
	}
	for _, receipt := range before {
		if err := deps.Repos.Receipts.Delete(ctx, receipt.ID); err != nil {
			return fmt.Errorf("excluir lançamento do lote: %w", err)
		}
	}
	for _, receipt := range before {
		label := DescribeReceipt(receipt)
		if err := Record(ctx, deps, actx, AuditEntry{
			Action:      domain.ActionReceiptBatchDelete,
			Entity:      domain.EntityReceipt,
			EntityID:    receipt.ID,
			EntityLabel: label,
			Summary:     "Pagamento de " + label + " excluído em lote",
			Changes:     domain.BuildAuditChanges(domain.ReceiptAuditFields, ReceiptSnapshot(receipt), nil),
			Metadata:    map[string]any{"batch": len(selected)},
		}); err != nil {
			return err
		}
	}
	return nil
}

// receiptFields são os campos do lançamento já validados e normalizados.
type receiptFields struct {
	leaseID     *string
	tenantID    *string
	propertyID  *string
	category    domain.ReceiptCategory
	description *string
	dueDate     string
	paidAt      *string
	amount      float64
	paidAmount  *float64
}

// apply copia os campos para o lançamento, preservando o que não é do
// formulário (id, documento emitido, chave de geração e criação).
func (f receiptFields) apply(receipt *domain.Receipt) *domain.Receipt {
	receipt.LeaseID = f.leaseID
	receipt.TenantID = f.tenantID
	receipt.PropertyID = f.propertyID
	receipt.Category = f.category
	receipt.Description = f.description
	receipt.DueDate = f.dueDate
	receipt.PaidAt = f.paidAt
	receipt.Amount = f.amount
	receipt.PaidAmount = f.paidAmount
	return receipt
}

// receiptFieldsFrom valida e normaliza o formulário do lançamento. As regras são
// as do app atual: aluguel exige contrato, cobrança avulsa exige descrição, o
// contrato manda no inquilino/imóvel, valor pago sem data preenche a data de
// agora e data de pagamento sem valor assume o valor total.
func receiptFieldsFrom(ctx context.Context, deps Deps, input ReceiptInput) (receiptFields, error) {
	dueDate := strings.TrimSpace(input.DueDate)
	amount, ok := receiptMoney(input.Amount)
	if dueDate == "" || !ok {
		return receiptFields{}, domain.Invalid("Preencha vencimento e valor.")
	}

	leaseID := strings.TrimSpace(input.LeaseID)
	category := receiptCategory(input.Category, leaseID)
	description := strings.TrimSpace(input.Description)
	if category == domain.ReceiptCategoryRent && leaseID == "" {
		return receiptFields{}, domain.Invalid("Selecione um contrato para recebimento de aluguel.")
	}
	if category == domain.ReceiptCategoryExtra && description == "" {
		return receiptFields{}, domain.Invalid("Informe a descrição para cobrança avulsa.")
	}

	tenantID := strings.TrimSpace(input.TenantID)
	propertyID := strings.TrimSpace(input.PropertyID)
	if leaseID != "" {
		lease, err := deps.Repos.Leases.ByID(ctx, leaseID)
		if err != nil {
			return receiptFields{}, fmt.Errorf("buscar contrato do lançamento: %w", err)
		}
		tenantID = lease.TenantID
		propertyID = lease.PropertyID
	}

	paidAt := strings.TrimSpace(input.PaidAt)
	paidAmount := receiptOptionalMoney(input.PaidAmount)
	switch {
	case paidAmount != nil && *paidAmount > 0 && paidAt == "":
		paidAt = format.FormatLocalDateTime(deps.Clock.Now())
	case paidAt != "" && (paidAmount == nil || *paidAmount == 0):
		paidAmount = domain.FloatPtr(amount)
	}

	return receiptFields{
		leaseID:     optionalString(leaseID),
		tenantID:    optionalString(tenantID),
		propertyID:  optionalString(propertyID),
		category:    category,
		description: optionalString(description),
		dueDate:     dueDate,
		paidAt:      optionalString(paidAt),
		amount:      amount,
		paidAmount:  paidAmount,
	}, nil
}

// receiptCategory resolve a categoria do lançamento. Sem categoria explícita
// (ou com valor desconhecido) ela é derivada da presença de contrato.
func receiptCategory(raw, leaseID string) domain.ReceiptCategory {
	switch domain.ReceiptCategory(strings.TrimSpace(raw)) {
	case domain.ReceiptCategoryRent:
		return domain.ReceiptCategoryRent
	case domain.ReceiptCategoryExtra:
		return domain.ReceiptCategoryExtra
	}
	if leaseID != "" {
		return domain.ReceiptCategoryRent
	}
	return domain.ReceiptCategoryExtra
}

// receiptIDs limpa a lista de ids do lote e recusa lote vazio.
func receiptIDs(ids []string) ([]string, error) {
	cleaned := make([]string, 0, len(ids))
	for _, id := range ids {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	if len(cleaned) == 0 {
		return nil, domain.Invalid("Nenhum recibo selecionado.")
	}
	return cleaned, nil
}

// monthEndKey devolve o último dia do mês no formato YYYY-MM-DD.
func monthEndKey(year, month int) string {
	return fmt.Sprintf("%s-%02d", domain.MonthKey(year, month), domain.DaysInMonth(year, month))
}

// receiptMoney interpreta um valor informado no formulário. O painel envia o
// valor já normalizado (ponto decimal), mas o formato pt-BR com vírgula também é
// aceito — o campo é digitado por quem usa o sistema.
func receiptMoney(raw string) (float64, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		// Mesma leitura do app atual: campo vazio vale zero.
		return 0, true
	}
	cleaned := make([]byte, 0, len(trimmed))
	for index := 0; index < len(trimmed); index++ {
		character := trimmed[index]
		switch {
		case character >= '0' && character <= '9', character == '.', character == ',', character == '-':
			cleaned = append(cleaned, character)
		}
	}
	normalized := string(cleaned)
	if strings.Contains(normalized, ",") {
		normalized = strings.ReplaceAll(normalized, ".", "")
		normalized = strings.ReplaceAll(normalized, ",", ".")
	}
	value, err := strconv.ParseFloat(normalized, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

// receiptOptionalMoney interpreta um valor opcional: campo vazio (ou ilegível)
// vira nulo, que é "sem valor pago registrado".
func receiptOptionalMoney(raw string) *float64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	value, ok := receiptMoney(raw)
	if !ok {
		return nil
	}
	return domain.FloatPtr(value)
}
