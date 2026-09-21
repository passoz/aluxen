package platform

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/passoz/aluxen/internal/domain"
)

// UUIDv7 gera identificadores UUID versão 7 (ordenados por tempo), como o
// `createId` do app Bun.
type UUIDv7 struct{}

// New devolve um UUIDv7 em forma canônica. Sem entropia do sistema não há como
// gerar um id seguro, então a falha interrompe o processo em vez de devolver um
// valor repetido.
func (UUIDv7) New() string {
	id, err := uuid.NewV7()
	if err != nil {
		panic(fmt.Sprintf("platform: gerar UUIDv7: %v", err))
	}
	return id.String()
}

var _ domain.IDGenerator = UUIDv7{}
