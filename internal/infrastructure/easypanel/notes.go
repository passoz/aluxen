package easypanel

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// expiryKey é a chave da etiqueta de expiração guardada na anotação do serviço
// (ARQUITETURA-DEPLOY.md §9). A anotação é o inventário da demo: fica no painel,
// não numa planilha paralela.
const expiryKey = "expires_at"

// dateLayout é o formato da etiqueta de expiração (data, sem hora: a demo vence
// no dia).
const dateLayout = "2006-01-02"

// EncodeNotes monta a anotação do serviço com a data de expiração.
func EncodeNotes(expiresAt time.Time) string {
	return expiryKey + "=" + ExpiryDate(expiresAt)
}

// ParseNotes lê a data de expiração da anotação. Devolve false quando a
// anotação não tem etiqueta — serviço sem prazo não expira sozinho.
func ParseNotes(notes string) (time.Time, bool) {
	for _, line := range strings.Split(notes, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found || !strings.EqualFold(strings.TrimSpace(key), expiryKey) {
			continue
		}
		expiresAt, err := parseDate(strings.TrimSpace(value))
		if err != nil {
			return time.Time{}, false
		}
		return expiresAt, true
	}
	return time.Time{}, false
}

// Expired informa se a demo venceu considerando uma janela de antecedência: com
// `horizon` zero só vence o que já passou; com 7 dias, o que vence dentro da
// semana entra na limpeza.
func Expired(notes string, now time.Time, horizon time.Duration) bool {
	expiresAt, ok := ParseNotes(notes)
	if !ok {
		return false
	}
	return !expiresAt.After(now.UTC().Add(horizon))
}

// ExpiryDate devolve a data de expiração no formato da etiqueta.
func ExpiryDate(t time.Time) string {
	return t.UTC().Format(dateLayout)
}

// ParseExpiry lê uma data de expiração informada pelo operador: `AAAA-MM-DD` ou
// a forma relativa `+Nd` (N dias a partir de `now`).
func ParseExpiry(value string, now time.Time) (time.Time, error) {
	if strings.HasPrefix(value, "+") && strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(value, "+"), "d"))
		if err != nil || days < 0 {
			return time.Time{}, fmt.Errorf("prazo relativo inválido %q: use +Nd", value)
		}
		return now.UTC().AddDate(0, 0, days), nil
	}
	expiresAt, err := parseDate(value)
	if err != nil {
		return time.Time{}, fmt.Errorf("data de expiração inválida %q: use AAAA-MM-DD ou +Nd", value)
	}
	return expiresAt, nil
}

// EnvLines monta o `.env` do Easypanel a partir das variáveis da instância, em
// ordem de chave para o arquivo ser estável entre execuções.
func EnvLines(vars map[string]string) string {
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(vars[key])
		builder.WriteString("\n")
	}
	return builder.String()
}

// parseDate lê a data absoluta da etiqueta.
func parseDate(value string) (time.Time, error) {
	expiresAt, err := time.Parse(dateLayout, value)
	if err != nil {
		return time.Time{}, err
	}
	return expiresAt.UTC(), nil
}
