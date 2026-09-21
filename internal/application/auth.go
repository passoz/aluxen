package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Autenticação do administrador e do locatário. As mensagens de erro são as
// mesmas do app atual — o usuário lê exatamente o texto de antes.

// AdminByID devolve o administrador autenticado.
func AdminByID(ctx context.Context, deps Deps, id string) (*domain.User, error) {
	user, err := deps.Repos.Users.ByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrUnauthorized
		}
		return nil, fmt.Errorf("buscar administrador: %w", err)
	}
	return user, nil
}

// AdminLogin valida usuário e senha e devolve o administrador autenticado.
// Devolve domain.ErrUnauthorized quando as credenciais não conferem.
func AdminLogin(ctx context.Context, deps Deps, username, password string) (*domain.User, error) {
	user, err := deps.Repos.Users.ByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrUnauthorized
		}
		return nil, fmt.Errorf("buscar administrador por usuário: %w", err)
	}
	if err := deps.Hasher.Compare(user.PasswordHash, password); err != nil {
		return nil, domain.ErrUnauthorized
	}
	return user, nil
}

// AdminMustChangePassword informa se o administrador ainda usa a senha semeada.
func AdminMustChangePassword(ctx context.Context, deps Deps, userID string) (bool, error) {
	user, err := deps.Repos.Users.ByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("verificar troca de senha: %w", err)
	}
	return user.MustChangePassword(), nil
}

// AdminChangePassword troca a senha do administrador validando a atual.
func AdminChangePassword(ctx context.Context, deps Deps, userID, currentPassword, newPassword, confirmPassword string) error {
	user, err := deps.Repos.Users.ByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Invalid("Usuário não encontrado.")
		}
		return fmt.Errorf("buscar administrador: %w", err)
	}
	if err := deps.Hasher.Compare(user.PasswordHash, currentPassword); err != nil {
		return domain.Invalid("Senha atual incorreta.")
	}
	if newPassword != confirmPassword {
		return domain.Invalid("A confirmação não corresponde à nova senha.")
	}
	if len(newPassword) < 6 {
		return domain.Invalid("Nova senha deve ter no mínimo 6 caracteres.")
	}

	hash, err := deps.Hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("gerar hash da nova senha: %w", err)
	}
	changedAt := format.FormatUTCStamp(deps.Clock.Now())
	if err := deps.Repos.Users.UpdatePassword(ctx, userID, hash, changedAt); err != nil {
		return fmt.Errorf("gravar nova senha: %w", err)
	}
	return nil
}

// ClientByID devolve o locatário da sessão da Área do Cliente.
func ClientByID(ctx context.Context, deps Deps, tenantID string) (*domain.Tenant, error) {
	tenant, err := deps.Repos.Tenants.ByID(ctx, tenantID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrUnauthorized
		}
		return nil, fmt.Errorf("buscar locatário: %w", err)
	}
	return tenant, nil
}

// ClientLogin autentica o locatário pelo CPF (com ou sem pontuação). Sem senha
// própria cadastrada, a senha válida é o próprio CPF.
func ClientLogin(ctx context.Context, deps Deps, identifier, password string) (*domain.Tenant, error) {
	cpf := format.NormalizeDigits(identifier)
	if len(cpf) != 11 || password == "" {
		return nil, domain.ErrUnauthorized
	}
	tenant, err := deps.Repos.Tenants.ByCPF(ctx, cpf)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrUnauthorized
		}
		return nil, fmt.Errorf("buscar locatário por CPF: %w", err)
	}
	if !clientPasswordMatches(deps, tenant, password) {
		return nil, domain.ErrUnauthorized
	}
	return tenant, nil
}

func clientPasswordMatches(deps Deps, tenant *domain.Tenant, password string) bool {
	if tenant.PasswordHash != nil && *tenant.PasswordHash != "" {
		return deps.Hasher.Compare(*tenant.PasswordHash, password) == nil
	}
	cpf := format.NormalizeDigits(domain.Str(tenant.CPF))
	return cpf != "" && format.NormalizeDigits(password) == cpf
}

// ClientMustChangePassword informa se o locatário ainda usa a senha padrão.
func ClientMustChangePassword(ctx context.Context, deps Deps, tenantID string) (bool, error) {
	tenant, err := deps.Repos.Tenants.ByID(ctx, tenantID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return false, domain.ErrUnauthorized
		}
		return false, fmt.Errorf("verificar troca de senha do locatário: %w", err)
	}
	return tenant.MustChangePassword(), nil
}

// ClientChangePassword troca a senha do locatário (CPF vale como senha atual no
// primeiro acesso) e proíbe que a nova senha seja o próprio CPF.
func ClientChangePassword(ctx context.Context, deps Deps, tenantID, currentPassword, newPassword, confirmPassword string) error {
	tenant, err := deps.Repos.Tenants.ByID(ctx, tenantID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Invalid("Cadastro de locatário não encontrado.")
		}
		return fmt.Errorf("buscar locatário: %w", err)
	}
	if !clientPasswordMatches(deps, tenant, currentPassword) {
		return domain.Invalid("Senha atual incorreta.")
	}
	if newPassword != confirmPassword {
		return domain.Invalid("A confirmação não corresponde à nova senha.")
	}
	password := trimSpace(newPassword)
	if len(password) < 6 {
		return domain.Invalid("A nova senha deve ter no mínimo 6 caracteres.")
	}
	cpf := format.NormalizeDigits(domain.Str(tenant.CPF))
	if cpf != "" && format.NormalizeDigits(password) == cpf {
		return domain.Invalid("A nova senha não pode ser o seu CPF.")
	}

	hash, err := deps.Hasher.Hash(password)
	if err != nil {
		return fmt.Errorf("gerar hash da nova senha do locatário: %w", err)
	}
	now := format.FormatUTCStamp(deps.Clock.Now())
	if err := deps.Repos.Tenants.UpdatePassword(ctx, tenantID, hash, now, now); err != nil {
		return fmt.Errorf("gravar nova senha do locatário: %w", err)
	}
	return nil
}

func trimSpace(value string) string {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\t' || value[start] == '\n' || value[start] == '\r') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\t' || value[end-1] == '\n' || value[end-1] == '\r') {
		end--
	}
	return value[start:end]
}
