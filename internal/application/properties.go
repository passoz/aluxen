package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Cadastro de imóveis: criação, alteração e exclusão. Diferente do locatário, a
// exclusão de imóvel não tem trava de contrato — é o comportamento do app atual.

// PropertyInput são os campos do imóvel informados no painel.
type PropertyInput struct {
	// ID é o identificador opcional enviado pelo formulário (a fila offline
	// preserva o id gerado no cliente). Vazio gera um id novo.
	ID      string
	Name    string
	Address string
	City    string
	State   string
	Zip     string
}

// PropertyCreate cadastra o imóvel e registra a criação na trilha.
func PropertyCreate(ctx context.Context, deps Deps, actx AuditContext, input PropertyInput) (*domain.Property, error) {
	property, err := buildProperty(deps, input)
	if err != nil {
		return nil, err
	}
	if err := deps.Repos.Properties.Create(ctx, property); err != nil {
		return nil, fmt.Errorf("cadastrar imóvel: %w", err)
	}
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionPropertyCreate,
		Entity:      domain.EntityProperty,
		EntityID:    property.ID,
		EntityLabel: property.Name,
		Summary:     fmt.Sprintf("Imóvel \"%s\" cadastrado", property.Name),
		Changes:     domain.BuildAuditChanges(domain.PropertyAuditFields, nil, PropertySnapshot(property)),
	}); err != nil {
		return nil, err
	}
	return property, nil
}

// PropertyUpdate altera o imóvel e registra o diff na trilha.
func PropertyUpdate(ctx context.Context, deps Deps, actx AuditContext, input PropertyInput) (*domain.Property, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return nil, domain.Invalid("Preencha nome, endereço, cidade e estado.")
	}
	fields, err := propertyFieldsFrom(input)
	if err != nil {
		return nil, err
	}

	before, err := deps.Repos.Properties.ByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("buscar imóvel: %w", err)
	}

	after := *before
	after.Name = fields.name
	after.Address = fields.address
	after.City = fields.city
	after.State = fields.state
	after.Zip = fields.zip
	after.UpdatedAt = format.FormatUTCStamp(deps.Clock.Now())

	if err := deps.Repos.Properties.Update(ctx, &after); err != nil {
		return nil, fmt.Errorf("alterar imóvel: %w", err)
	}
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionPropertyUpdate,
		Entity:      domain.EntityProperty,
		EntityID:    after.ID,
		EntityLabel: after.Name,
		Summary:     fmt.Sprintf("Imóvel \"%s\" alterado", after.Name),
		Changes:     domain.BuildAuditChanges(domain.PropertyAuditFields, PropertySnapshot(before), PropertySnapshot(&after)),
	}); err != nil {
		return nil, err
	}
	return &after, nil
}

// PropertyDelete exclui o imóvel e registra a exclusão na trilha.
func PropertyDelete(ctx context.Context, deps Deps, actx AuditContext, id string) error {
	propertyID := strings.TrimSpace(id)
	if propertyID == "" {
		return domain.Invalid("Registro não informado.")
	}

	before, err := deps.Repos.Properties.ByID(ctx, propertyID)
	if err != nil {
		return fmt.Errorf("buscar imóvel: %w", err)
	}
	if err := deps.Repos.Properties.Delete(ctx, propertyID); err != nil {
		return fmt.Errorf("excluir imóvel: %w", err)
	}
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionPropertyDelete,
		Entity:      domain.EntityProperty,
		EntityID:    before.ID,
		EntityLabel: before.Name,
		Summary:     fmt.Sprintf("Imóvel \"%s\" excluído", before.Name),
		Changes:     domain.BuildAuditChanges(domain.PropertyAuditFields, PropertySnapshot(before), nil),
	}); err != nil {
		return err
	}
	return nil
}

// propertyFields são os campos do imóvel já validados e normalizados.
type propertyFields struct {
	name    string
	address string
	city    string
	state   string
	zip     *string
}

func propertyFieldsFrom(input PropertyInput) (propertyFields, error) {
	name := strings.TrimSpace(input.Name)
	address := strings.TrimSpace(input.Address)
	city := strings.TrimSpace(input.City)
	state := strings.TrimSpace(input.State)
	if name == "" || address == "" || city == "" || state == "" {
		return propertyFields{}, domain.Invalid("Preencha nome, endereço, cidade e estado.")
	}
	return propertyFields{
		name:    name,
		address: address,
		city:    city,
		state:   state,
		zip:     maskedOptional(input.Zip, format.FormatCEPInput),
	}, nil
}

// buildProperty monta a entidade do imóvel a partir do formulário.
func buildProperty(deps Deps, input PropertyInput) (*domain.Property, error) {
	fields, err := propertyFieldsFrom(input)
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = deps.IDs.New()
	}
	stamp := format.FormatUTCStamp(deps.Clock.Now())
	return &domain.Property{
		ID:        id,
		Name:      fields.name,
		Address:   fields.address,
		City:      fields.city,
		State:     fields.state,
		Zip:       fields.zip,
		CreatedAt: stamp,
		UpdatedAt: stamp,
	}, nil
}
