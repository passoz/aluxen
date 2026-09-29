package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Regras puras da locação. Sem dependência de banco, HTTP ou framework.

// Overlaps informa se dois períodos de datas ISO (YYYY-MM-DD) se intersectam.
// A comparação lexicográfica é válida para esse formato.
func Overlaps(startA, endA, startB, endB string) bool {
	return startA <= endB && endA >= startB
}

// DaysInMonth devolve o número de dias do mês (month de 1 a 12).
func DaysInMonth(year, month int) int {
	return time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// DueDateForMonth devolve o vencimento no formato YYYY-MM-DD, limitado ao último
// dia do mês quando o dia contratado não existe (ex.: dia 31 em fevereiro).
func DueDateForMonth(year, month, dueDay int) string {
	day := dueDay
	if last := DaysInMonth(year, month); day > last {
		day = last
	}
	if day < 1 {
		day = 1
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

// MonthKey devolve a competência YYYY-MM usada na chave de geração mensal.
func MonthKey(year, month int) string {
	return fmt.Sprintf("%04d-%02d", year, month)
}

// GeneratedKeyForMonth monta a chave única de deduplicação da geração mensal.
func GeneratedKeyForMonth(leaseID string, year, month int) string {
	return leaseID + ":" + MonthKey(year, month)
}

// ReceiptStatusOf deriva o status do lançamento exatamente como o app atual:
// valor pago explícito decide entre parcial e pago; sem valor explícito, a
// presença de pagamento decide; sem pagamento, compara o vencimento com hoje.
func ReceiptStatusOf(receipt Receipt, today time.Time) ReceiptStatus {
	totalAmount := receipt.Amount
	hasExplicitPaidAmount := receipt.PaidAmount != nil
	paidAmount := 0.0
	if hasExplicitPaidAmount {
		paidAmount = *receipt.PaidAmount
	} else if receipt.PaidAt != nil && *receipt.PaidAt != "" {
		paidAmount = totalAmount
	}

	if hasExplicitPaidAmount && paidAmount > 0 && totalAmount > 0 {
		if paidAmount < totalAmount {
			return ReceiptStatusPartial
		}
		return ReceiptStatusPaid
	}

	if receipt.PaidAt != nil && *receipt.PaidAt != "" {
		return ReceiptStatusPaid
	}

	due, err := time.ParseInLocation("2006-01-02", firstDatePart(receipt.DueDate), today.Location())
	if err != nil {
		return ReceiptStatusDue
	}
	midnight := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	if due.Before(midnight) {
		return ReceiptStatusOverdue
	}
	return ReceiptStatusDue
}

func firstDatePart(value string) string {
	if index := strings.IndexAny(value, "T "); index >= 0 {
		return value[:index]
	}
	return value
}

// O TLD exige duas letras ou mais, como no app anterior: `ana@exemplo.c` não é
// um endereço válido e não pode entrar no cadastro.
var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[A-Za-z]{2,}$`)

// ValidateEmail aceita o endereço ou devolve o erro com a mensagem do app atual.
func ValidateEmail(email string) error {
	trimmed := strings.TrimSpace(email)
	if !emailPattern.MatchString(trimmed) {
		return Invalid("Informe um e-mail válido.")
	}
	return nil
}

// ValidateDueDay aceita dia de vencimento entre 1 e 31.
func ValidateDueDay(day int) error {
	if day < 1 || day > 31 {
		return Invalid("Informe um dia de vencimento entre 1 e 31.")
	}
	return nil
}

// ValidateRentAmount exige valor de aluguel maior que zero.
func ValidateRentAmount(amount float64) error {
	if amount <= 0 {
		return Invalid("Informe o valor do aluguel.")
	}
	return nil
}

// ValidatePeriod exige data de término igual ou posterior ao início.
func ValidatePeriod(startDate, endDate string) error {
	if startDate == "" || endDate == "" {
		return Invalid("Informe o período do contrato.")
	}
	if endDate < startDate {
		return Invalid("A data de término deve ser igual ou posterior à data de início.")
	}
	return nil
}
