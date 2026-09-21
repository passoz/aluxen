package platform

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/passoz/aluxen/internal/domain"
)

// bcryptCost é o custo do hash. O app Bun semeia e troca senhas com custo 10
// (`bcryptjs.hash(..., 10)`), e os hashes existentes em produção precisam
// continuar válidos: mudar o custo exigiria re-hash no primeiro login.
const bcryptCost = 10

// BcryptHasher deriva e confere senhas com bcrypt.
type BcryptHasher struct{}

// Hash devolve o hash bcrypt da senha (custo 10, prefixo `$2a$`).
func (BcryptHasher) Hash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("platform: gerar hash de senha: %w", err)
	}
	return string(hash), nil
}

// Compare confere se a senha corresponde ao hash. Quando não corresponde — ou
// quando o hash é inválido — devolve um erro que embrulha
// `domain.ErrUnauthorized`, nunca `nil, nil`. Hashes `$2a$` e `$2b$` do app
// atual são aceitos.
func (BcryptHasher) Compare(hash, password string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return fmt.Errorf("platform: senha não confere: %w", domain.ErrUnauthorized)
	}
	return nil
}

var _ domain.PasswordHasher = BcryptHasher{}
