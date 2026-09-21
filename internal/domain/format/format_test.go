package format

import "testing"

func TestFormatDateBRAcceptsBothTimestampShapes(t *testing.T) {
	cases := map[string]string{
		"2026-09-10":          "10/09/2026",
		"2026-09-10T14:30":    "10/09/2026",
		"2026-09-10 14:30:00": "10/09/2026",
		"":                    "-",
	}

	for value, want := range cases {
		if got := FormatDateBR(value); got != want {
			t.Errorf("FormatDateBR(%q) = %q, esperado %q", value, got, want)
		}
	}
}

func TestFormatDateTimeBR(t *testing.T) {
	cases := map[string]string{
		"2026-09-10T14:30":        "10/09/2026 14:30",
		"2026-09-10 14:30:00":     "10/09/2026 14:30",
		"2026-09-10":              "10/09/2026",
		"2026-09-10T09:05:12.123": "10/09/2026 09:05",
		"":                        "-",
	}

	for value, want := range cases {
		if got := FormatDateTimeBR(value); got != want {
			t.Errorf("FormatDateTimeBR(%q) = %q, esperado %q", value, got, want)
		}
	}
}

func TestFormatCurrency(t *testing.T) {
	cases := map[float64]string{
		0:          "R$ 0,00",
		0.5:        "R$ 0,50",
		2500:       "R$ 2.500,00",
		2500.5:     "R$ 2.500,50",
		1234567.89: "R$ 1.234.567,89",
	}

	for value, want := range cases {
		if got := FormatCurrency(value); got != want {
			t.Errorf("FormatCurrency(%v) = %q, esperado %q", value, got, want)
		}
	}
}

func TestInputMasks(t *testing.T) {
	if got := FormatCPFInput("11122233344"); got != "111.222.333-44" {
		t.Errorf("máscara de CPF = %q", got)
	}
	if got := FormatCPFInput("111.222.333-445"); got != "111.222.333-44" {
		t.Errorf("máscara de CPF deve limitar a 11 dígitos, veio %q", got)
	}
	if got := FormatCEPInput("28907250"); got != "28907-250" {
		t.Errorf("máscara de CEP = %q", got)
	}
	if got := FormatPhoneInput("22998881122"); got != "(22) 99888-1122" {
		t.Errorf("máscara de telefone = %q", got)
	}
	if got := FormatPhoneInput("2233334444"); got != "(22) 3333-4444" {
		t.Errorf("máscara de telefone fixo = %q", got)
	}
	if got := NormalizeMoneyInput("R$ 2.500,00"); got != "2500.00" {
		t.Errorf("normalização de valor = %q", got)
	}
}

func TestLocalDateTimeRoundTrip(t *testing.T) {
	parsed := ParseLocalDateTime("2026-09-10T14:30")
	if got := FormatLocalDateTime(parsed); got != "2026-09-10T14:30" {
		t.Fatalf("ida e volta = %q", got)
	}
	parsed = ParseLocalDateTime("2026-09-10 08:05:59")
	if got := FormatLocalDateTime(parsed); got != "2026-09-10T08:05" {
		t.Fatalf("ida e volta com formato do SQLite = %q", got)
	}
	if got := FormatLocalDate(ParseLocalDate("2026-09-10")); got != "2026-09-10" {
		t.Fatalf("data local = %q", got)
	}
}
