// Package platform concentra os serviços básicos de infraestrutura que o
// domínio expõe como portas (`domain.Clock`, `domain.IDGenerator`,
// `domain.PasswordHasher`): relógio, gerador de id e hash de senha.
package platform

import (
	"time"

	"github.com/passoz/aluxen/internal/domain"
)

// SystemClock usa o relógio do sistema.
type SystemClock struct{}

// Now devolve o instante atual no fuso local, como o app Bun faz.
func (SystemClock) Now() time.Time { return time.Now() }

// FixedClock devolve sempre o mesmo instante; serve aos testes e é exportado
// para que outros pacotes também o usem em seus dublês.
type FixedClock struct {
	T time.Time
}

// Now devolve o instante fixado.
func (c FixedClock) Now() time.Time { return c.T }

// NewFixedClock fixa o relógio no instante informado.
func NewFixedClock(at time.Time) FixedClock { return FixedClock{T: at} }

var (
	_ domain.Clock = SystemClock{}
	_ domain.Clock = FixedClock{}
)
