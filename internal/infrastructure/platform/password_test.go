package platform

import (
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/passoz/aluxen/internal/domain"
)

// legacyHash é um hash de teste no formato gravado pelo app Bun (bcrypt, custo 10).
// O port precisa continuar validando hashes assim.
const legacyHash = "$2b$10$mv411FwL8LPi14KG58PiTOsDyo7XCK8VEDFQdeWDE..15tb3kAQGi"

func TestBcryptHasherHashAndCompare(t *testing.T) {
	hasher := BcryptHasher{}

	hash, err := hasher.Hash("senha-de-teste")
	if err != nil {
		t.Fatalf("Hash() devolveu erro: %v", err)
	}
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("hash gerado inválido: %v", err)
	}
	if cost != 10 {
		t.Errorf("custo do hash = %d, esperado 10", cost)
	}
	if err := hasher.Compare(hash, "senha-de-teste"); err != nil {
		t.Errorf("Compare() na senha correta devolveu erro: %v", err)
	}

	other, err := hasher.Hash("senha-de-teste")
	if err != nil {
		t.Fatalf("Hash() devolveu erro: %v", err)
	}
	if other == hash {
		t.Error("hashes da mesma senha deveriam diferir pelo sal")
	}
}

func TestBcryptHasherRejectsWrongPassword(t *testing.T) {
	hasher := BcryptHasher{}
	hash, err := hasher.Hash("senha-de-teste")
	if err != nil {
		t.Fatalf("Hash() devolveu erro: %v", err)
	}

	err = hasher.Compare(hash, "outra-senha")
	if err == nil {
		t.Fatal("Compare() deveria devolver erro quando a senha não confere")
	}
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("erro = %v, esperado embrulhar domain.ErrUnauthorized", err)
	}
}

func TestBcryptHasherAcceptsLegacyBcryptHashes(t *testing.T) {
	hasher := BcryptHasher{}

	if err := hasher.Compare(legacyHash, "senha-de-teste"); err != nil {
		t.Errorf("hash $2b$ de produção recusado: %v", err)
	}
	if err := hasher.Compare(legacyHash, "senha-errada"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("erro = %v, esperado embrulhar domain.ErrUnauthorized", err)
	}
}

func TestBcryptHasherFailsClosedOnInvalidHash(t *testing.T) {
	hasher := BcryptHasher{}

	for _, hash := range []string{"", "não-é-hash", "$2a$10$curto"} {
		if err := hasher.Compare(hash, "senha-de-teste"); err == nil {
			t.Errorf("Compare(%q) deveria falhar", hash)
		} else if !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("Compare(%q) = %v, esperado embrulhar domain.ErrUnauthorized", hash, err)
		}
	}
}
