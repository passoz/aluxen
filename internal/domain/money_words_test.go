package domain

import "testing"

func TestNumberInWords(t *testing.T) {
	cases := map[int]string{
		0:       "zero",
		1:       "um",
		14:      "quatorze",
		21:      "vinte e um",
		100:     "cem",
		101:     "cento e um",
		500:     "quinhentos",
		999:     "novecentos e noventa e nove",
		1000:    "mil",
		1800:    "mil e oitocentos",
		2500:    "dois mil e quinhentos",
		100000:  "cem mil",
		1000000: "um milhão",
		2000000: "dois milhões",
	}

	for value, want := range cases {
		if got := NumberInWords(value); got != want {
			t.Errorf("NumberInWords(%d) = %q, esperado %q", value, got, want)
		}
	}
}

func TestMoneyInWords(t *testing.T) {
	cases := map[float64]string{
		0:       "zero reais",
		1:       "um real",
		1.01:    "um real e um centavo",
		1.99:    "um real e noventa e nove centavos",
		100:     "cem reais",
		2500:    "dois mil e quinhentos reais",
		2500.5:  "dois mil e quinhentos reais e cinquenta centavos",
		1234.56: "mil e duzentos e trinta e quatro reais e cinquenta e seis centavos",
		999999:  "novecentos e noventa e nove mil e novecentos e noventa e nove reais",
	}

	for value, want := range cases {
		if got := MoneyInWords(value); got != want {
			t.Errorf("MoneyInWords(%v) = %q, esperado %q", value, got, want)
		}
	}
}
