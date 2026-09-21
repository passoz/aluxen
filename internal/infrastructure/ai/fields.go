package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Este arquivo concentra o parse da saída do provedor. A regra é uma só: o
// schema é estrito (BR-201) e nenhuma mensagem de erro cita o conteúdo lido —
// só o nome do campo e o motivo.

// fields decodifica o objeto JSON devolvido pelo provedor. Toleramos cercas de
// código (```json ... ```) e texto em volta, porque é o que os provedores de
// visão devolvem na prática; fora isso, o conteúdo tem de ser um objeto JSON.
func fields(content string) (map[string]json.RawMessage, error) {
	text := strings.TrimSpace(content)
	if strings.HasPrefix(text, "```") {
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		}
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), "```"))
	}
	// Texto em volta (explicação antes ou depois do objeto): recortamos o
	// objeto. Não inventamos nada — o que não for objeto continua sendo erro.
	if start := strings.IndexByte(text, '{'); start > 0 {
		if end := strings.LastIndexByte(text, '}'); end > start {
			text = text[start : end+1]
		}
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &object); err != nil {
		// A mensagem cita a posição, nunca o trecho: o texto do provedor pode
		// conter dado pessoal lido do documento (BR-202).
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			return nil, fmt.Errorf("%w: JSON inválido na posição %d", ErrInvalidResponse, syntax.Offset)
		}
		return nil, fmt.Errorf("%w: JSON inválido", ErrInvalidResponse)
	}
	if object == nil {
		return nil, fmt.Errorf("%w: JSON não é um objeto", ErrInvalidResponse)
	}
	return object, nil
}

// textField devolve o campo como texto já aparado; ausente, nulo ou vazio
// devolve "". Número devolvido onde se esperava texto (CPF, CEP) é aceito como
// literal, porque é assim que o provedor devolve número grande.
func textField(object map[string]json.RawMessage, key string) (string, error) {
	raw, ok := object[key]
	if !ok {
		return "", nil
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return strings.TrimSpace(value), nil
	}
	if isNumberLiteral(text) {
		return text, nil
	}
	return "", fmt.Errorf("%w: campo %q não é texto", ErrInvalidResponse, key)
}

// requiredText exige o campo-âncora do schema: sem ele não há leitura útil.
func requiredText(object map[string]json.RawMessage, key string) (string, error) {
	value, err := textField(object, key)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("%w: campo %q ausente", ErrInvalidResponse, key)
	}
	return value, nil
}

// numberField devolve o campo como número e se ele veio preenchido. O provedor
// às vezes devolve valor como texto ("R$ 1.500,00"); aceitamos esse formato e
// recusamos o resto — nunca inventamos valor.
func numberField(object map[string]json.RawMessage, key string) (float64, bool, error) {
	raw, ok := object[key]
	if !ok {
		return 0, false, nil
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return 0, false, nil
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, true, nil
	}
	var quoted string
	if err := json.Unmarshal(raw, &quoted); err == nil {
		if parsed, ok := parseAmount(quoted); ok {
			return parsed, true, nil
		}
	}
	return 0, false, fmt.Errorf("%w: campo %q não é número", ErrInvalidResponse, key)
}

// requiredAmount exige valor maior que zero — a âncora do comprovante.
func requiredAmount(object map[string]json.RawMessage, key string) (float64, error) {
	value, present, err := numberField(object, key)
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, fmt.Errorf("%w: campo %q ausente", ErrInvalidResponse, key)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%w: campo %q deve ser maior que zero", ErrInvalidResponse, key)
	}
	return value, nil
}

// amountField aceita valor ausente (devolve zero) e recusa valor inválido.
func amountField(object map[string]json.RawMessage, key string) (float64, error) {
	value, _, err := numberField(object, key)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, fmt.Errorf("%w: campo %q não pode ser negativo", ErrInvalidResponse, key)
	}
	return value, nil
}

// dateField exige data no formato YYYY-MM-DD, o mesmo que o domínio compara.
func dateField(object map[string]json.RawMessage, key string, required bool) (string, error) {
	value, err := textField(object, key)
	if err != nil {
		return "", err
	}
	if value == "" {
		if required {
			return "", fmt.Errorf("%w: campo %q ausente", ErrInvalidResponse, key)
		}
		return "", nil
	}
	if _, err := time.Parse(time.DateOnly, value); err != nil {
		return "", fmt.Errorf("%w: campo %q não está em YYYY-MM-DD", ErrInvalidResponse, key)
	}
	return value, nil
}

// confidenceField exige a confiança declarada pelo provedor dentro de 0..1. É o
// que separa a baixa automática da revisão humana.
func confidenceField(object map[string]json.RawMessage) (float64, error) {
	value, present, err := numberField(object, "confidence")
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, fmt.Errorf("%w: campo \"confidence\" ausente", ErrInvalidResponse)
	}
	if value < 0 || value > 1 {
		return 0, fmt.Errorf("%w: campo \"confidence\" fora de 0..1", ErrInvalidResponse)
	}
	return value, nil
}

// arrayField devolve o campo como lista de objetos. Lista vazia é válida
// (nada a relatar); ausente, nula ou de outro tipo é falha de schema.
func arrayField(object map[string]json.RawMessage, key string) ([]map[string]json.RawMessage, error) {
	raw, ok := object[key]
	if !ok {
		return nil, fmt.Errorf("%w: campo %q ausente", ErrInvalidResponse, key)
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return nil, fmt.Errorf("%w: campo %q ausente", ErrInvalidResponse, key)
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		// Sem repetir o trecho devolvido: pode conter dado pessoal.
		return nil, fmt.Errorf("%w: campo %q não é uma lista de objetos", ErrInvalidResponse, key)
	}
	return items, nil
}

// dayField devolve o dia de vencimento opcional, recusando dia inexistente.
func dayField(object map[string]json.RawMessage, key string) (int, error) {
	value, present, err := numberField(object, key)
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, nil
	}
	day := int(value)
	if day < 1 || day > 31 || value != float64(day) {
		return 0, fmt.Errorf("%w: campo %q não é dia de 1 a 31", ErrInvalidResponse, key)
	}
	return day, nil
}

// parseAmount aceita "1500.5" (número puro, o que o prompt pede), "R$ 1.500,50"
// e "1500,50" (formatação brasileira), que é o que o provedor devolve quando
// decide formatar o valor. Vírgula presente implica separador decimal
// brasileiro (pontos são de milhar); sem vírgula o ponto é decimal. Um
// "1.500" solto, portanto, é lido como 1.5 — e a divergência de valor manda o
// comprovante para revisão humana em vez de dar baixa errada.
func parseAmount(raw string) (float64, bool) {
	value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "R$"))
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if strings.Contains(value, ",") {
		value = strings.ReplaceAll(value, ".", "")
		value = strings.Replace(value, ",", ".", 1)
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// isNumberLiteral informa se o texto é um literal numérico JSON.
func isNumberLiteral(text string) bool {
	if text == "" {
		return false
	}
	if _, err := strconv.ParseFloat(text, 64); err != nil {
		return false
	}
	return true
}
