package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
)

// Configurações da instância: dados do locador (usados nos documentos) e URL
// pública (usada nos links de validação e nos QR codes).

// LandlordInput são os dados do locador informados na tela de configurações.
type LandlordInput struct {
	Name  string
	CPF   string
	Email string
}

// SaveLandlordData grava os dados do locador e registra a alteração na trilha.
func SaveLandlordData(ctx context.Context, deps Deps, actx AuditContext, input LandlordInput) error {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.Invalid("Informe o nome do locador.")
	}
	if err := domain.ValidateEmail(input.Email); err != nil {
		return err
	}

	before, err := landlordSnapshot(ctx, deps)
	if err != nil {
		return err
	}

	values := map[string]string{
		domain.SettingLandlordName:  name,
		domain.SettingLandlordCPF:   strings.TrimSpace(input.CPF),
		domain.SettingLandlordEmail: strings.TrimSpace(input.Email),
	}
	for key, value := range values {
		if err := SaveSetting(ctx, deps, key, value); err != nil {
			return err
		}
	}

	after, err := landlordSnapshot(ctx, deps)
	if err != nil {
		return err
	}
	changes := domain.BuildAuditChanges(domain.LandlordAuditFields, before, after)
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionSettingLandlord,
		Entity:      domain.EntitySetting,
		EntityID:    "landlord",
		EntityLabel: "Dados do locador",
		Summary:     domain.AuditActionMetaFor(domain.ActionSettingLandlord).Label,
		Changes:     changes,
	}); err != nil {
		return err
	}
	return nil
}

func landlordSnapshot(ctx context.Context, deps Deps) (map[string]string, error) {
	name, err := SettingValue(ctx, deps, domain.SettingLandlordName)
	if err != nil {
		return nil, err
	}
	cpf, err := SettingValue(ctx, deps, domain.SettingLandlordCPF)
	if err != nil {
		return nil, err
	}
	email, err := SettingValue(ctx, deps, domain.SettingLandlordEmail)
	if err != nil {
		return nil, err
	}
	return map[string]string{"name": name, "cpf": cpf, "email": email}, nil
}

// SaveAppURL grava a URL pública do sistema (vazio remove a configuração).
func SaveAppURL(ctx context.Context, deps Deps, actx AuditContext, rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed != "" && !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		return domain.Invalid("Informe uma URL começando por http:// ou https://.")
	}

	before, err := SettingValue(ctx, deps, domain.SettingAppURL)
	if err != nil {
		return err
	}
	if err := SaveSetting(ctx, deps, domain.SettingAppURL, trimmed); err != nil {
		return err
	}

	summary := "URL pública do sistema alterada"
	if trimmed == "" {
		summary = "URL pública do sistema removida"
	}
	changes := domain.BuildAuditChanges(domain.AppURLAuditFields,
		map[string]string{"url": before},
		map[string]string{"url": trimmed},
	)
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionSettingAppURL,
		Entity:      domain.EntitySetting,
		EntityID:    "app_url",
		EntityLabel: "URL pública",
		Summary:     summary,
		Changes:     changes,
	}); err != nil {
		return err
	}
	return nil
}

// SettingsView é o que a tela de configurações precisa exibir.
type SettingsView struct {
	LandlordName  string
	LandlordCPF   string
	LandlordEmail string
	AppURL        string
	ResolvedURL   string
	MustChange    bool
}

// LoadSettingsView carrega a visão da tela de configurações.
func LoadSettingsView(ctx context.Context, deps Deps, requestHost string) (*SettingsView, error) {
	values, err := Settings(ctx, deps)
	if err != nil {
		return nil, fmt.Errorf("carregar configurações: %w", err)
	}
	return &SettingsView{
		LandlordName:  values[domain.SettingLandlordName],
		LandlordCPF:   values[domain.SettingLandlordCPF],
		LandlordEmail: values[domain.SettingLandlordEmail],
		AppURL:        values[domain.SettingAppURL],
		ResolvedURL:   ResolvePublicBaseURL(ctx, deps, requestHost),
	}, nil
}
