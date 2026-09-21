// Package domain contém as entidades, as portas (interfaces) e as regras puras
// da locação. Nenhum arquivo deste pacote importa application ou infrastructure.
package domain

import (
	"errors"
	"fmt"
)

// Erros sentinela do domínio. As camadas externas traduzem para HTTP com
// errors.Is/errors.As, nunca por comparação de texto.
var (
	ErrNotFound     = errors.New("registro não encontrado")
	ErrConflict     = errors.New("conflito de estado")
	ErrUnauthorized = errors.New("não autorizado")
)

// ValidationError carrega a mensagem exata que o usuário lê. O app atual devolve
// essas mensagens em português no corpo da resposta; o port preserva o texto.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// Invalid devolve um erro de validação com a mensagem dada ao usuário.
func Invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// IsValidation informa se err é (ou embrulha) um erro de validação.
func IsValidation(err error) bool {
	var target *ValidationError
	return errors.As(err, &target)
}

// ValidationMessage devolve a mensagem de validação de err, ou a string vazia.
func ValidationMessage(err error) string {
	var target *ValidationError
	if errors.As(err, &target) {
		return target.Message
	}
	return ""
}
