package domain

import (
	"math"
	"strconv"
)

// Valor por extenso em português — mesma regra do app atual (usado no recibo e
// no contrato em PDF, onde o valor precisa aparecer escrito).

var (
	unitsInWords = []string{"zero", "um", "dois", "três", "quatro", "cinco", "seis", "sete", "oito", "nove"}
	teensInWords = []string{"dez", "onze", "doze", "treze", "quatorze", "quinze", "dezesseis", "dezessete", "dezoito", "dezenove"}
	tensInWords  = []string{"", "", "vinte", "trinta", "quarenta", "cinquenta", "sessenta", "setenta", "oitenta", "noventa"}
	hundInWords  = []string{"", "cento", "duzentos", "trezentos", "quatrocentos", "quinhentos", "seiscentos", "setecentos", "oitocentos", "novecentos"}
)

func underThousandInWords(n int) string {
	switch {
	case n == 0:
		return ""
	case n < 10:
		return unitsInWords[n]
	case n < 20:
		return teensInWords[n-10]
	case n < 100:
		ten := n / 10
		rest := n % 10
		if rest != 0 {
			return tensInWords[ten] + " e " + underThousandInWords(rest)
		}
		return tensInWords[ten]
	case n == 100:
		return "cem"
	default:
		hundred := n / 100
		rest := n % 100
		if rest != 0 {
			return hundInWords[hundred] + " e " + underThousandInWords(rest)
		}
		return hundInWords[hundred]
	}
}

// NumberInWords escreve um inteiro não negativo por extenso em português.
func NumberInWords(value int) string {
	if value == 0 {
		return "zero"
	}
	if value < 0 {
		return "menos " + NumberInWords(-value)
	}

	billions := value / 1_000_000_000
	millions := (value % 1_000_000_000) / 1_000_000
	thousands := (value % 1_000_000) / 1_000
	remainder := value % 1_000

	parts := make([]string, 0, 4)
	if billions > 0 {
		if billions == 1 {
			parts = append(parts, "um bilhão")
		} else {
			parts = append(parts, NumberInWords(billions)+" bilhões")
		}
	}
	if millions > 0 {
		if millions == 1 {
			parts = append(parts, "um milhão")
		} else {
			parts = append(parts, NumberInWords(millions)+" milhões")
		}
	}
	if thousands > 0 {
		if thousands == 1 {
			parts = append(parts, "mil")
		} else {
			parts = append(parts, NumberInWords(thousands)+" mil")
		}
	}
	if remainder > 0 {
		parts = append(parts, underThousandInWords(remainder))
	}

	if len(parts) == 1 {
		return parts[0]
	}
	last := parts[len(parts)-1]
	return joinWithCommaAnd(parts[:len(parts)-1]) + " e " + last
}

func joinWithCommaAnd(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += ", "
		}
		out += part
	}
	return out
}

// MoneyInWords escreve um valor monetário por extenso em reais e centavos.
func MoneyInWords(value float64) string {
	whole := int(math.Floor(value))
	cents := int(math.Round((value - float64(whole)) * 100))
	if cents == 100 {
		whole++
		cents = 0
	}

	wholeText := NumberInWords(whole) + " reais"
	if whole == 1 {
		wholeText = NumberInWords(whole) + " real"
	}
	if cents == 0 {
		return wholeText
	}
	centsText := NumberInWords(cents) + " centavos"
	if cents == 1 {
		centsText = NumberInWords(cents) + " centavo"
	}
	return wholeText + " e " + centsText
}

// FormatFloat devolve o valor com duas casas, sem separador de milhar.
func FormatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}
