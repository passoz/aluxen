package platform

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/passoz/aluxen/internal/domain"
)

// O bcrypt só usa os primeiros 72 bytes da senha. O port precisa recusar a
// senha maior do que isso em vez de aceitá-la truncada — duas senhas que
// compartilham os 72 primeiros bytes são a mesma senha para o algoritmo.
func TestCovBcryptHasherRejectsPasswordBeyondTheBcryptLimit(t *testing.T) {
	hasher := BcryptHasher{}
	atLimit := strings.Repeat("a", 72)
	long := atLimit + "b"

	hash, err := hasher.Hash(atLimit)
	if err != nil {
		t.Fatalf("Hash() da senha no limite devolveu erro: %v", err)
	}
	if err := hasher.Compare(hash, atLimit); err != nil {
		t.Errorf("Compare() da senha no limite devolveu erro: %v", err)
	}

	if produced, err := hasher.Hash(long); err == nil {
		t.Error("Hash() deveria recusar senha acima do limite do bcrypt em vez de truncá-la")
	} else if produced != "" {
		t.Errorf("Hash() devolveu %q junto do erro, esperado vazio", produced)
	}

	err = hasher.Compare(hash, strings.Repeat("z", 73))
	if err == nil {
		t.Fatal("Compare() aceitou senha acima do limite do bcrypt que não corresponde ao hash")
	}
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("erro = %v, esperado embrulhar domain.ErrUnauthorized", err)
	}
}

// covUUIDMillis extrai o instante de um UUIDv7 (os 48 bits de tempo iniciais).
func covUUIDMillis(id uuid.UUID) int64 {
	return int64(id[0])<<40 | int64(id[1])<<32 | int64(id[2])<<24 |
		int64(id[3])<<16 | int64(id[4])<<8 | int64(id[5])
}

// Os ids de uma instância precisam ser únicos, da variante RFC 4122 e ordenados
// pelo instante da criação: é a ordenação que o app usa para paginar recibos.
func TestCovUUIDv7IsOrderedByTimeAndUsesTheRFCVariant(t *testing.T) {
	generator := UUIDv7{}

	const batch = 200
	seen := make(map[uuid.UUID]struct{}, batch)
	previous := uuid.Nil
	var previousMillis int64 = -1

	for i := 0; i < batch; i++ {
		parsed, err := uuid.Parse(generator.New())
		if err != nil {
			t.Fatalf("id %d não é UUID válido: %v", i, err)
		}
		if parsed.Variant() != uuid.RFC4122 {
			t.Fatalf("variante de %v = %v, esperado RFC4122", parsed, parsed.Variant())
		}
		if _, repeated := seen[parsed]; repeated {
			t.Fatalf("id repetido no lote: %v", parsed)
		}
		seen[parsed] = struct{}{}

		millis := covUUIDMillis(parsed)
		if millis < previousMillis {
			t.Fatalf("instante retrocedeu no lote: %d após %d", millis, previousMillis)
		}
		previousMillis = millis

		if i > 0 && bytes.Compare(previous[:], parsed[:]) >= 0 {
			t.Fatalf("id %v não é posterior a %v", parsed, previous)
		}
		previous = parsed
	}
}

// O relógio fixo é um valor: o zero vale como instante zero e dois relógios
// apontam para instantes independentes, o que deixa o teste escolher o momento.
func TestCovFixedClockZeroValueAndAdvance(t *testing.T) {
	var zero FixedClock
	if got := zero.Now(); !got.IsZero() {
		t.Errorf("FixedClock{} aponta para %v, esperado o instante zero", got)
	}

	start := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	clock := NewFixedClock(start)
	advanced := FixedClock{T: start.Add(48 * time.Hour)}

	if got := clock.Now(); !got.Equal(start) {
		t.Errorf("Now() = %v, esperado %v", got, start)
	}
	if !clock.Now().Before(advanced.Now()) {
		t.Errorf("%v não é anterior a %v", clock.Now(), advanced.Now())
	}
	if delta := advanced.Now().Sub(clock.Now()); delta != 48*time.Hour {
		t.Errorf("intervalo entre os relógios = %v, esperado 48h", delta)
	}
}
