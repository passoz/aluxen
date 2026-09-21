package platform

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestUUIDv7IsVersionedAndUnique(t *testing.T) {
	generator := UUIDv7{}
	seen := make(map[string]struct{}, 1000)

	for i := 0; i < 1000; i++ {
		id := generator.New()

		parsed, err := uuid.Parse(id)
		if err != nil {
			t.Fatalf("id %q não é UUID válido: %v", id, err)
		}
		if got := parsed.Version(); got != 7 {
			t.Fatalf("versão de %q = %d, esperado 7", id, got)
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("id repetido: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestUUIDv7CarriesCurrentTime(t *testing.T) {
	generator := UUIDv7{}
	parsed, err := uuid.Parse(generator.New())
	if err != nil {
		t.Fatalf("UUID inválido: %v", err)
	}

	milliseconds := int64(parsed[0])<<40 |
		int64(parsed[1])<<32 |
		int64(parsed[2])<<24 |
		int64(parsed[3])<<16 |
		int64(parsed[4])<<8 |
		int64(parsed[5])
	generatedAt := time.UnixMilli(milliseconds)

	if delta := time.Since(generatedAt); delta < -time.Minute || delta > time.Minute {
		t.Errorf("timestamp do UUID a %v do relógio (uuid=%v)", delta, generatedAt)
	}
}
