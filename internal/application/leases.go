package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Contratos de locação. As regras são as mesmas do app atual: o imóvel não pode
// ter dois contratos vigentes no mesmo período — temporada incluída —, enquanto
// suspenso e encerrado não ocupam o imóvel. Cada operação deixa a sua linha na
// trilha de auditoria.

// LeaseInput são os campos do contrato informados no painel.
type LeaseInput struct {
	// ID é o identificador opcional enviado pelo formulário (a fila offline
	// preserva o id gerado no cliente). Vazio gera um id novo.
	ID         string
	TenantID   string
	PropertyID string
	StartDate  string
	EndDate    string
	DueDay     int
	RentAmount float64
	// Status vazio na criação vira `active`; na edição mantém a situação atual.
	Status domain.LeaseStatus
	// DepositAmount é a caução opcional do formulário: nil preserva a caução
	// já registrada (na edição) ou segue sem caução (na criação). A caução
	// não olha o status do contrato: vale para fixo e para temporada.
	DepositAmount *float64
	// DepositNotes são as observações da caução, usadas quando DepositAmount
	// vem preenchida.
	DepositNotes string
	// InspectionCheckIn pede a vistoria de entrada na criação do contrato. Na
	// edição o pedido é ignorado: vistoria se gerencia pela bancada.
	InspectionCheckIn     bool
	InspectionCondition   domain.InspectionCondition
	InspectionPerformedAt string
	InspectionNotes       string
}

// LeaseCreate cadastra o contrato e registra a criação na trilha.
func LeaseCreate(ctx context.Context, deps Deps, actx AuditContext, input LeaseInput) (*domain.Lease, error) {
	status := input.Status
	if status == "" {
		status = domain.LeaseStatusActive
	}
	if !status.SelectableOnCreate() {
		return nil, domain.Invalid("Status de contrato inválido.")
	}
	if err := validateLeaseFields(input); err != nil {
		return nil, err
	}
	if err := validateLeaseDeposit(input); err != nil {
		return nil, err
	}
	if status.BlocksOverlap() {
		if err := ensureLeaseSlot(ctx, deps, input.PropertyID, "", input.StartDate, input.EndDate, status); err != nil {
			return nil, err
		}
	}

	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = deps.IDs.New()
	}
	stamp := format.FormatUTCStamp(deps.Clock.Now())
	lease := &domain.Lease{
		ID:         id,
		TenantID:   input.TenantID,
		PropertyID: input.PropertyID,
		StartDate:  input.StartDate,
		EndDate:    input.EndDate,
		DueDay:     input.DueDay,
		RentAmount: input.RentAmount,
		Status:     status,
		CreatedAt:  stamp,
		UpdatedAt:  stamp,
	}
	if err := deps.Repos.Leases.Create(ctx, lease); err != nil {
		return nil, fmt.Errorf("cadastrar contrato: %w", err)
	}

	created, err := leaseByID(ctx, deps, id)
	if err != nil {
		return nil, err
	}
	label := DescribeLease(created)
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionLeaseCreate,
		Entity:      domain.EntityLease,
		EntityID:    created.ID,
		EntityLabel: label,
		Summary:     fmt.Sprintf("Contrato de %s criado", label),
		Changes:     domain.BuildAuditChanges(domain.LeaseAuditFields, nil, LeaseSnapshot(created)),
	}); err != nil {
		return nil, err
	}

	// Caução e vistoria nascem pelo mesmo fluxo da bancada: mesma validação,
	// mesma trilha, mesmo recibo. Nada aqui olha o status do contrato.
	if input.DepositAmount != nil {
		created, err = SetLeaseDeposit(ctx, deps, actx, DepositInput{
			LeaseID: id,
			Amount:  *input.DepositAmount,
			Notes:   input.DepositNotes,
		})
		if err != nil {
			return nil, err
		}
	}
	if input.InspectionCheckIn {
		if _, err := InspectionCreate(ctx, deps, actx, checkInInput(id, input)); err != nil {
			return nil, err
		}
	}
	return created, nil
}

// LeaseUpdate altera o contrato e registra o diff na trilha. Status vazio
// preserva a situação atual; o próprio contrato é ignorado na checagem de
// sobreposição.
func LeaseUpdate(ctx context.Context, deps Deps, actx AuditContext, input LeaseInput) (*domain.Lease, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return nil, domain.Invalid("Preencha todos os campos do contrato.")
	}
	if input.Status != "" && !input.Status.SelectableOnUpdate() {
		return nil, domain.Invalid("Status de contrato inválido.")
	}
	if err := validateLeaseFields(input); err != nil {
		return nil, err
	}
	if err := validateLeaseDeposit(input); err != nil {
		return nil, err
	}

	before, err := leaseByID(ctx, deps, id)
	if err != nil {
		return nil, err
	}

	status := before.Status
	if input.Status != "" {
		status = input.Status
	}
	if status.BlocksOverlap() {
		if err := ensureLeaseSlot(ctx, deps, input.PropertyID, id, input.StartDate, input.EndDate, status); err != nil {
			return nil, err
		}
	}

	after := *before
	after.TenantID = input.TenantID
	after.PropertyID = input.PropertyID
	after.StartDate = input.StartDate
	after.EndDate = input.EndDate
	after.DueDay = input.DueDay
	after.RentAmount = input.RentAmount
	after.Status = status
	after.UpdatedAt = format.FormatUTCStamp(deps.Clock.Now())

	if err := deps.Repos.Leases.Update(ctx, &after); err != nil {
		return nil, fmt.Errorf("alterar contrato: %w", err)
	}

	updated, err := leaseByID(ctx, deps, id)
	if err != nil {
		return nil, err
	}
	label := DescribeLease(updated)
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionLeaseUpdate,
		Entity:      domain.EntityLease,
		EntityID:    updated.ID,
		EntityLabel: label,
		Summary:     fmt.Sprintf("Contrato de %s alterado", label),
		Changes:     domain.BuildAuditChanges(domain.LeaseAuditFields, LeaseSnapshot(before), LeaseSnapshot(updated)),
	}); err != nil {
		return nil, err
	}

	// Campo de caução em branco preserva a caução já registrada: a escrita de
	// contrato é de linha inteira e o `after` acima já carregou os campos de
	// caução do `before`. Preenchido, registra pelo fluxo da bancada.
	if input.DepositAmount != nil {
		updated, err = SetLeaseDeposit(ctx, deps, actx, DepositInput{
			LeaseID: id,
			Amount:  *input.DepositAmount,
			Notes:   input.DepositNotes,
		})
		if err != nil {
			return nil, err
		}
	}
	return updated, nil
}

// LeaseSetStatus aplica uma transição de situação — `lease.terminate`,
// `lease.suspend` ou `lease.reactivate` — alterando apenas `status` e
// `updated_at`, como no app atual.
func LeaseSetStatus(ctx context.Context, deps Deps, actx AuditContext, intent, id string) error {
	status, actionLabel, ok := leaseStatusTransition(intent)
	if !ok {
		return domain.Invalid("Ação inválida.")
	}
	leaseID := strings.TrimSpace(id)
	if leaseID == "" {
		return domain.Invalid("Contrato não informado.")
	}

	before, err := leaseByID(ctx, deps, leaseID)
	if err != nil {
		return err
	}
	if err := deps.Repos.Leases.UpdateStatus(ctx, leaseID, status, format.FormatUTCStamp(deps.Clock.Now())); err != nil {
		return fmt.Errorf("alterar situação do contrato: %w", err)
	}

	after, err := leaseByID(ctx, deps, leaseID)
	if err != nil {
		return err
	}
	label := DescribeLease(after)
	return Record(ctx, deps, actx, AuditEntry{
		Action:      intent,
		Entity:      domain.EntityLease,
		EntityID:    after.ID,
		EntityLabel: label,
		Summary:     fmt.Sprintf("Contrato de %s %s", label, actionLabel),
		Changes:     domain.BuildAuditChanges(domain.LeaseAuditFields, LeaseSnapshot(before), LeaseSnapshot(after)),
	})
}

// LeaseDelete exclui o contrato e registra a exclusão na trilha. Os recibos
// ligados a ele caem junto, pela cascata da chave estrangeira.
func LeaseDelete(ctx context.Context, deps Deps, actx AuditContext, id string) error {
	leaseID := strings.TrimSpace(id)
	if leaseID == "" {
		return domain.Invalid("Contrato não informado.")
	}

	before, err := leaseByID(ctx, deps, leaseID)
	if err != nil {
		return err
	}
	if err := deps.Repos.Leases.Delete(ctx, leaseID); err != nil {
		return fmt.Errorf("excluir contrato: %w", err)
	}
	label := DescribeLease(before)
	return Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionLeaseDelete,
		Entity:      domain.EntityLease,
		EntityID:    before.ID,
		EntityLabel: label,
		Summary:     fmt.Sprintf("Contrato de %s excluído", label),
		Changes:     domain.BuildAuditChanges(domain.LeaseAuditFields, LeaseSnapshot(before), nil),
	})
}

// validateLeaseDeposit valida a caução e a vistoria de entrada opcionais do
// formulário, antes de gravar qualquer coisa: valor precisa ser positivo, e a
// vistoria precisa de tipo, estado e data válidos.
func validateLeaseDeposit(input LeaseInput) error {
	if input.DepositAmount != nil && *input.DepositAmount <= 0 {
		return domain.Invalid("Informe o valor da caução.")
	}
	if input.InspectionCheckIn {
		resolved := checkInInput("", input)
		if err := domain.ValidateInspection(resolved.Kind, resolved.Condition, resolved.PerformedAt); err != nil {
			return err
		}
	}
	return nil
}

// checkInInput resolve o pedido de vistoria de entrada do formulário: estado
// padrão é boa e a data padrão é o início do contrato.
func checkInInput(leaseID string, input LeaseInput) InspectionInput {
	condition := input.InspectionCondition
	if condition == "" {
		condition = domain.InspectionConditionGood
	}
	performedAt := strings.TrimSpace(input.InspectionPerformedAt)
	if performedAt == "" {
		performedAt = input.StartDate
	}
	return InspectionInput{
		LeaseID:     leaseID,
		Kind:        domain.InspectionKindCheckIn,
		PerformedAt: performedAt,
		Condition:   condition,
		Notes:       input.InspectionNotes,
	}
}

// validateLeaseFields valida os campos comuns à criação e à alteração com as
// mensagens do app atual.
func validateLeaseFields(input LeaseInput) error {
	if input.TenantID == "" || input.PropertyID == "" {
		return domain.Invalid("Preencha todos os campos do contrato.")
	}
	if err := domain.ValidatePeriod(input.StartDate, input.EndDate); err != nil {
		return err
	}
	if err := domain.ValidateDueDay(input.DueDay); err != nil {
		return err
	}
	return domain.ValidateRentAmount(input.RentAmount)
}

// ensureLeaseSlot recusa a gravação quando já existe contrato vigente no imóvel
// dentro do período. A mensagem de temporada é própria, como no app atual.
func ensureLeaseSlot(ctx context.Context, deps Deps, propertyID, excludeID, startDate, endDate string, status domain.LeaseStatus) error {
	overlaps, err := deps.Repos.Leases.Overlapping(ctx, propertyID, excludeID, startDate, endDate)
	if err != nil {
		return fmt.Errorf("verificar sobreposição de contrato: %w", err)
	}
	if len(overlaps) == 0 {
		return nil
	}
	if status == domain.LeaseStatusSeason {
		return domain.Invalid("Este imóvel já possui contrato vigente no período. Temporada só pode ser criada em imóvel sem contrato ativo ou com contrato suspenso.")
	}
	return domain.Invalid("Este imóvel já possui contrato vigente no período.")
}

// leaseStatusTransition traduz o intent de situação para o status gravado e o
// rótulo usado no resumo da trilha.
func leaseStatusTransition(intent string) (domain.LeaseStatus, string, bool) {
	switch intent {
	case domain.ActionLeaseTerminate:
		return domain.LeaseStatusTerminated, "encerrado", true
	case domain.ActionLeaseSuspend:
		return domain.LeaseStatusSuspended, "suspenso", true
	case domain.ActionLeaseReactivate:
		return domain.LeaseStatusActive, "reativado", true
	default:
		return "", "", false
	}
}

// leaseByID carrega o contrato com os nomes de locatário e imóvel do join.
func leaseByID(ctx context.Context, deps Deps, id string) (*domain.Lease, error) {
	lease, err := deps.Repos.Leases.ByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("buscar contrato: %w", err)
	}
	return lease, nil
}
