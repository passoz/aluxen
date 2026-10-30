package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

const (
	// seedBcryptCost é o custo dos hashes existentes (compatível com os do app
	// atual, que usa bcryptjs com custo 10).
	seedBcryptCost = 10
	// legacySeedUsername é o usuário administrador das instalações antigas.
	legacySeedUsername = "admin"

	defaultSeedUsername = "passoz"
	defaultLandlordName = "Locador"
)

// EnsureAdmin garante que existe um administrador utilizável e as configurações
// padrão do locador. Mesma semântica do app atual:
//
//   - já existe usuário com o nome semeado → nada é feito (a senha nunca é
//     redefinida em cima de uma senha trocada pelo usuário);
//   - existe o usuário legado `admin` → ele é renomeado (mantendo a senha);
//   - caso contrário, cria o usuário com a senha semeada e SEM
//     `password_changed_at`, o que obriga a troca no primeiro acesso;
//   - `landlord_name`/`landlord_cpf` só são gravados quando ainda não existem.
//
// Valores vazios caem para a variável de ambiente correspondente. A senha não
// tem padrão: sem `SEED_PASSWORD` no primeiro boot, a criação falha.
func (s *Store) EnsureAdmin(ctx context.Context, username, password, landlordName, landlordCPF string) error {
	username = firstNonEmpty(username, os.Getenv("SEED_USERNAME"), defaultSeedUsername)
	password = firstNonEmpty(password, os.Getenv("SEED_PASSWORD"))
	landlordName = firstNonEmpty(landlordName, os.Getenv("DEFAULT_LANDLORD_NAME"), defaultLandlordName)
	landlordCPF = firstNonEmpty(landlordCPF, os.Getenv("DEFAULT_LANDLORD_CPF"))

	_, err := s.Users.ByUsername(ctx, username)
	switch {
	case err == nil:
		// administrador já existe: a senha é dele, não mexemos
	case errors.Is(err, domain.ErrNotFound):
		if err := s.createOrAdoptAdmin(ctx, username, password); err != nil {
			return err
		}
	default:
		return fmt.Errorf("procurar administrador %q: %w", username, err)
	}

	return s.ensureDefaultSettings(ctx, landlordName, landlordCPF)
}

// createOrAdoptAdmin cria o administrador semeado ou adota o usuário legado.
func (s *Store) createOrAdoptAdmin(ctx context.Context, username, password string) error {
	if username != defaultSeedUsername {
		defaultAdmin, err := s.Users.ByUsername(ctx, defaultSeedUsername)
		switch {
		case err == nil:
			if err := s.Users.UpdateUsername(ctx, defaultAdmin.ID, username); err != nil {
				return fmt.Errorf("renomear administrador %q: %w", defaultSeedUsername, err)
			}
			return nil
		case !errors.Is(err, domain.ErrNotFound):
			return fmt.Errorf("procurar administrador %q: %w", defaultSeedUsername, err)
		}
	}

	legacy, err := s.Users.ByUsername(ctx, legacySeedUsername)
	switch {
	case err == nil:
		if err := s.Users.UpdateUsername(ctx, legacy.ID, username); err != nil {
			return fmt.Errorf("renomear administrador legado %q: %w", legacySeedUsername, err)
		}
		return nil
	case !errors.Is(err, domain.ErrNotFound):
		return fmt.Errorf("procurar administrador legado %q: %w", legacySeedUsername, err)
	}

	if password == "" {
		return errors.New("SEED_PASSWORD é obrigatório no primeiro boot")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), seedBcryptCost)
	if err != nil {
		return fmt.Errorf("gerar hash da senha semeada: %w", err)
	}
	user := &domain.User{
		ID:           newID(),
		Username:     username,
		PasswordHash: string(hash),
		CreatedAt:    format.FormatUTCStamp(time.Now()),
	}
	if err := s.Users.Create(ctx, user); err != nil {
		return fmt.Errorf("criar administrador semeado %q: %w", username, err)
	}
	return nil
}

// ensureDefaultSettings grava as configurações padrão que ainda não existem.
func (s *Store) ensureDefaultSettings(ctx context.Context, landlordName, landlordCPF string) error {
	defaults := []struct{ key, value string }{
		{domain.SettingLandlordName, landlordName},
		{domain.SettingLandlordCPF, landlordCPF},
	}
	for _, setting := range defaults {
		_, err := s.Settings.Get(ctx, setting.key)
		switch {
		case err == nil:
			continue
		case !errors.Is(err, domain.ErrNotFound):
			return fmt.Errorf("ler configuração %q: %w", setting.key, err)
		}
		if err := s.Settings.Set(ctx, setting.key, setting.value); err != nil {
			return fmt.Errorf("gravar configuração padrão %q: %w", setting.key, err)
		}
	}
	return nil
}

// firstNonEmpty devolve o primeiro valor não vazio.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// newID devolve um identificador novo no mesmo formato do app atual (UUIDv7).
func newID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}
