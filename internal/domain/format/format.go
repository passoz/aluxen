// Package format contém formatação de apresentação (datas, moeda, máscaras)
// usada pelo painel, pelos PDFs e pela trilha de auditoria. São funções puras,
// sem dependência de infraestrutura.
package format

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// NormalizeDigits devolve apenas os dígitos de s (CPF, CEP, telefone).
func NormalizeDigits(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FormatDateBR formata YYYY-MM-DD (ou YYYY-MM-DDTHH:mm / YYYY-MM-DD HH:mm:ss)
// como dd/mm/aaaa. Valor vazio devolve "-".
func FormatDateBR(value string) string {
	if value == "" {
		return "-"
	}
	datePart := splitDatePart(value)
	year, month, day, ok := parseDateParts(datePart)
	if !ok {
		return value
	}
	return fmt.Sprintf("%02d/%02d/%04d", day, month, year)
}

// FormatDateTimeBR formata dd/mm/aaaa HH:mm, omitindo a hora quando ausente.
func FormatDateTimeBR(value string) string {
	if value == "" {
		return "-"
	}
	datePart, timePart := splitDateTimeParts(value)
	year, month, day, ok := parseDateParts(datePart)
	if !ok {
		return value
	}
	if timePart == "" {
		return fmt.Sprintf("%02d/%02d/%04d", day, month, year)
	}
	hour, minute := splitHourMinute(timePart)
	return fmt.Sprintf("%02d/%02d/%04d %s:%s", day, month, year, hour, minute)
}

// FormatCurrency formata um valor em reais no padrão pt-BR (R$ 1.234,56).
func FormatCurrency(value float64) string {
	negative := value < 0
	absolute := math.Abs(value)
	whole := int64(absolute)
	cents := int(math.Round((absolute - float64(whole)) * 100))
	if cents == 100 {
		whole++
		cents = 0
	}
	grouped := groupThousands(whole)
	sign := ""
	if negative {
		sign = "-"
	}
	return fmt.Sprintf("%sR$ %s,%02d", sign, grouped, cents)
}

// FormatCurrencyFromString aceita string (o painel envia valores como texto).
func FormatCurrencyFromString(value string) string {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return FormatCurrency(0)
	}
	return FormatCurrency(parsed)
}

func groupThousands(value int64) string {
	digits := strconv.FormatInt(value, 10)
	if len(digits) <= 3 {
		return digits
	}
	var parts []string
	for len(digits) > 3 {
		parts = append([]string{digits[len(digits)-3:]}, parts...)
		digits = digits[:len(digits)-3]
	}
	parts = append([]string{digits}, parts...)
	return strings.Join(parts, ".")
}

// NormalizeMoneyInput converte dígitos digitados em valor decimal com duas casas.
func NormalizeMoneyInput(value string) string {
	digits := NormalizeDigits(value)
	if digits == "" {
		return ""
	}
	number, err := strconv.ParseFloat(digits, 64)
	if err != nil {
		return ""
	}
	return strconv.FormatFloat(number/100, 'f', 2, 64)
}

// FormatPhoneInput aplica a máscara (00) 00000-0000.
func FormatPhoneInput(value string) string {
	digits := truncateDigits(value, 11)
	switch {
	case digits == "":
		return ""
	case len(digits) <= 2:
		return "(" + digits
	case len(digits) <= 6:
		return "(" + digits[:2] + ") " + digits[2:]
	case len(digits) <= 10:
		return "(" + digits[:2] + ") " + digits[2:6] + "-" + digits[6:]
	default:
		return "(" + digits[:2] + ") " + digits[2:7] + "-" + digits[7:]
	}
}

// FormatCEPInput aplica a máscara 00000-000.
func FormatCEPInput(value string) string {
	digits := truncateDigits(value, 8)
	if len(digits) <= 5 {
		return digits
	}
	return digits[:5] + "-" + digits[5:]
}

// FormatCPFInput aplica a máscara 000.000.000-00.
func FormatCPFInput(value string) string {
	digits := truncateDigits(value, 11)
	switch {
	case digits == "":
		return ""
	case len(digits) <= 3:
		return digits
	case len(digits) <= 6:
		return digits[:3] + "." + digits[3:]
	case len(digits) <= 9:
		return digits[:3] + "." + digits[3:6] + "." + digits[6:]
	default:
		return digits[:3] + "." + digits[3:6] + "." + digits[6:9] + "-" + digits[9:]
	}
}

func truncateDigits(value string, limit int) string {
	digits := NormalizeDigits(value)
	if len(digits) > limit {
		return digits[:limit]
	}
	return digits
}

// ParseLocalDate interpreta YYYY-MM-DD no fuso local.
func ParseLocalDate(value string) time.Time {
	year, month, day, ok := parseDateParts(splitDatePart(value))
	if !ok {
		return time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local)
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
}

// ParseLocalDateTime interpreta YYYY-MM-DDTHH:mm ou YYYY-MM-DD HH:mm.
func ParseLocalDateTime(value string) time.Time {
	datePart, timePart := splitDateTimeParts(value)
	year, month, day, ok := parseDateParts(datePart)
	if !ok {
		year, month, day = 2000, 1, 1
	}
	hourText, minuteText := splitHourMinute(timePart)
	hour, _ := strconv.Atoi(hourText)
	minute, _ := strconv.Atoi(minuteText)
	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.Local)
}

// FormatLocalDate formata uma data como YYYY-MM-DD no fuso local.
func FormatLocalDate(value time.Time) string {
	return fmt.Sprintf("%04d-%02d-%02d", value.Year(), int(value.Month()), value.Day())
}

// FormatLocalDateTime formata YYYY-MM-DDTHH:mm no fuso local.
func FormatLocalDateTime(value time.Time) string {
	return fmt.Sprintf("%sT%02d:%02d", FormatLocalDate(value), value.Hour(), value.Minute())
}

// FormatLocalDateTimeMillis formata YYYY-MM-DDTHH:mm:ss.SSS no fuso local —
// formato usado em `audit_logs.created_at`.
func FormatLocalDateTimeMillis(value time.Time) string {
	return fmt.Sprintf("%s.%03d", value.Format("2006-01-02T15:04:05"), value.Nanosecond()/1_000_000)
}

// FormatUTCStamp formata o horário UTC no formato gravado pelo SQLite
// (`datetime('now')` → YYYY-MM-DD HH:MM:SS).
func FormatUTCStamp(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05")
}

func splitDatePart(value string) string {
	datePart, _ := splitDateTimeParts(value)
	return datePart
}

func splitDateTimeParts(value string) (string, string) {
	if index := strings.IndexAny(value, "T "); index >= 0 {
		return value[:index], strings.TrimSpace(value[index+1:])
	}
	return value, ""
}

func parseDateParts(datePart string) (int, int, int, bool) {
	parts := strings.Split(datePart, "-")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	year, errYear := strconv.Atoi(parts[0])
	month, errMonth := strconv.Atoi(parts[1])
	day, errDay := strconv.Atoi(parts[2])
	if errYear != nil || errMonth != nil || errDay != nil {
		return 0, 0, 0, false
	}
	return year, month, day, true
}

func splitHourMinute(timePart string) (string, string) {
	hour, minute := "00", "00"
	parts := strings.Split(timePart, ":")
	if len(parts) > 0 && parts[0] != "" {
		hour = padTwo(parts[0])
	}
	if len(parts) > 1 && parts[1] != "" {
		minute = padTwo(parts[1])
	}
	return hour, minute
}

func padTwo(value string) string {
	if len(value) >= 2 {
		return value[:2]
	}
	return "0" + value
}
