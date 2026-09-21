package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Cadastro de locatários: criação, alteração e exclusão. As mensagens de erro
// são as mesmas do app atual e cada operação deixa a sua linha na trilha.

// TenantInput são os campos do locatário informados no painel.
type TenantInput struct {
	// ID é o identificador opcional enviado pelo formulário (a fila offline
	// preserva o id gerado no cliente). Vazio gera um id novo.
	ID    string
	Name  string
	Email string
	Phone string
	CPF   string
}

// TenantCreate cadastra o locatário e registra a criação na trilha.
func TenantCreate(ctx context.Context, deps Deps, actx AuditContext, input TenantInput) (*domain.Tenant, error) {
	tenant, err := buildTenant(deps, input)
	if err != nil {
		return nil, err
	}
	if err := deps.Repos.Tenants.Create(ctx, tenant); err != nil {
		return nil, fmt.Errorf("cadastrar locatário: %w", err)
	}
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionTenantCreate,
		Entity:      domain.EntityTenant,
		EntityID:    tenant.ID,
		EntityLabel: tenant.Name,
		Summary:     fmt.Sprintf("Inquilino \"%s\" cadastrado", tenant.Name),
		Changes:     domain.BuildAuditChanges(domain.TenantAuditFields, nil, TenantSnapshot(tenant)),
	}); err != nil {
		return nil, err
	}
	return tenant, nil
}

// TenantUpdate altera o locatário e registra o diff na trilha.
func TenantUpdate(ctx context.Context, deps Deps, actx AuditContext, input TenantInput) (*domain.Tenant, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return nil, domain.Invalid("Nome e email são obrigatórios.")
	}
	fields, err := tenantFieldsFrom(input)
	if err != nil {
		return nil, err
	}

	before, err := deps.Repos.Tenants.ByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("buscar locatário: %w", err)
	}

	after := *before
	after.Name = fields.name
	after.Email = fields.email
	after.Phone = fields.phone
	after.CPF = fields.cpf
	after.UpdatedAt = format.FormatUTCStamp(deps.Clock.Now())

	if err := deps.Repos.Tenants.Update(ctx, &after); err != nil {
		return nil, fmt.Errorf("alterar locatário: %w", err)
	}
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionTenantUpdate,
		Entity:      domain.EntityTenant,
		EntityID:    after.ID,
		EntityLabel: after.Name,
		Summary:     fmt.Sprintf("Inquilino \"%s\" alterado", after.Name),
		Changes:     domain.BuildAuditChanges(domain.TenantAuditFields, TenantSnapshot(before), TenantSnapshot(&after)),
	}); err != nil {
		return nil, err
	}
	return &after, nil
}

// TenantDelete exclui o locatário. Contrato ativo bloqueia a exclusão, como no
// app atual — é preciso encerrar o contrato primeiro.
func TenantDelete(ctx context.Context, deps Deps, actx AuditContext, id string) error {
	tenantID := strings.TrimSpace(id)
	if tenantID == "" {
		return domain.Invalid("Registro não informado.")
	}

	leases, err := deps.Repos.Leases.ByTenant(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("listar contratos do locatário: %w", err)
	}
	for _, lease := range leases {
		if lease.Status == domain.LeaseStatusActive {
			return domain.Invalid("Não é possível excluir inquilino com contrato ativo. Encerre o contrato primeiro.")
		}
	}

	before, err := deps.Repos.Tenants.ByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("buscar locatário: %w", err)
	}
	if err := deps.Repos.Tenants.Delete(ctx, tenantID); err != nil {
		return fmt.Errorf("excluir locatário: %w", err)
	}
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionTenantDelete,
		Entity:      domain.EntityTenant,
		EntityID:    before.ID,
		EntityLabel: before.Name,
		Summary:     fmt.Sprintf("Inquilino \"%s\" excluído", before.Name),
		Changes:     domain.BuildAuditChanges(domain.TenantAuditFields, TenantSnapshot(before), nil),
	}); err != nil {
		return err
	}
	return nil
}

// tenantFields valida e normaliza os campos comuns à criação e à alteração.
type tenantFields struct {
	name  string
	email string
	phone *string
	cpf   *string
}

func tenantFieldsFrom(input TenantInput) (tenantFields, error) {
	name := strings.TrimSpace(input.Name)
	email := strings.TrimSpace(input.Email)
	if name == "" || email == "" {
		return tenantFields{}, domain.Invalid("Nome e email são obrigatórios.")
	}
	if err := domain.ValidateEmail(email); err != nil {
		return tenantFields{}, err
	}
	return tenantFields{
		name:  name,
		email: email,
		phone: maskedOptional(input.Phone, format.FormatPhoneInput),
		cpf:   maskedOptional(input.CPF, format.FormatCPFInput),
	}, nil
}

// buildTenant monta a entidade do locatário a partir do formulário.
func buildTenant(deps Deps, input TenantInput) (*domain.Tenant, error) {
	fields, err := tenantFieldsFrom(input)
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = deps.IDs.New()
	}
	stamp := format.FormatUTCStamp(deps.Clock.Now())
	return &domain.Tenant{
		ID:        id,
		Name:      fields.name,
		Email:     fields.email,
		Phone:     fields.phone,
		CPF:       fields.cpf,
		CreatedAt: stamp,
		UpdatedAt: stamp,
	}, nil
}

// maskedOptional aplica a máscara ao valor informado e devolve nulo quando nada
// foi digitado — os campos de CPF, telefone e CEP são opcionais.
func maskedOptional(value string, mask func(string) string) *string {
	masked := mask(value)
	if masked == "" {
		return nil
	}
	return domain.StrPtr(masked)
}
