package format

import (
	"strings"
	"testing"
	"time"
)

func TestCovFmtNormalizeDigits(t *testing.T) {
	cases := map[string]string{
		"(22) 99888-1122": "22998881122",
		"111.222.333-44":  "11122233344",
		"sem dígitos":     "",
		"":                "",
		"a1b2c3":          "123",
	}
	for input, want := range cases {
		if got := NormalizeDigits(input); got != want {
			t.Fatalf("NormalizeDigits(%q) = %q, quer %q", input, got, want)
		}
	}
}

func TestCovFmtFormatDateBR(t *testing.T) {
	cases := map[string]string{
		"":                    "-",
		"2026-09-10":          "10/09/2026",
		"2026-09-10T14:30":    "10/09/2026",
		"2026-09-10 14:30:00": "10/09/2026",
		"2026-9-10":           "10/09/2026",
		"10/09/2026":          "10/09/2026", // formato inesperado volta cru
		"2026-09":             "2026-09",
		"2026-xx-10":          "2026-xx-10", // parte não numérica volta cru
	}
	for input, want := range cases {
		if got := FormatDateBR(input); got != want {
			t.Fatalf("FormatDateBR(%q) = %q, quer %q", input, got, want)
		}
	}
}

func TestCovFmtFormatDateTimeBR(t *testing.T) {
	cases := map[string]string{
		"":                     "-",
		"2026-09-10":           "10/09/2026",
		"2026-09-10T14:30":     "10/09/2026 14:30",
		"2026-09-10 08:05:59":  "10/09/2026 08:05",
		"2026-09-10T9:07":      "10/09/2026 09:07",
		"2026-09-10T14:30:00Z": "10/09/2026 14:30",
		"sem-formato":          "sem-formato",
		"2026-09-10T":          "10/09/2026",
	}
	for input, want := range cases {
		if got := FormatDateTimeBR(input); got != want {
			t.Fatalf("FormatDateTimeBR(%q) = %q, quer %q", input, got, want)
		}
	}
}

func TestCovFmtFormatCurrency(t *testing.T) {
	cases := map[float64]string{
		0:          "R$ 0,00",
		999:        "R$ 999,00",
		1000:       "R$ 1.000,00",
		2500:       "R$ 2.500,00",
		1234567.89: "R$ 1.234.567,89",
		-1234.5:    "-R$ 1.234,50",
		0.999:      "R$ 1,00", // centavos arredondam para o real seguinte
		0.01:       "R$ 0,01",
		1000000:    "R$ 1.000.000,00",
	}
	for value, want := range cases {
		if got := FormatCurrency(value); got != want {
			t.Fatalf("FormatCurrency(%v) = %q, quer %q", value, got, want)
		}
	}
}

func TestCovFmtFormatCurrencyFromString(t *testing.T) {
	cases := map[string]string{
		"2500.5": "R$ 2.500,50",
		" 100 ":  "R$ 100,00",
		"abc":    "R$ 0,00",
		"":       "R$ 0,00",
		"-12.5":  "-R$ 12,50",
	}
	for input, want := range cases {
		if got := FormatCurrencyFromString(input); got != want {
			t.Fatalf("FormatCurrencyFromString(%q) = %q, quer %q", input, got, want)
		}
	}
}

func TestCovFmtNormalizeMoneyInput(t *testing.T) {
	cases := map[string]string{
		"":                       "",
		"1":                      "0.01",
		"12":                     "0.12",
		"12345":                  "123.45",
		"R$ 1.234,56":            "1234.56",
		"abc":                    "",
		strings.Repeat("9", 400): "", // estoura o float e é descartado
	}
	for input, want := range cases {
		if got := NormalizeMoneyInput(input); got != want {
			t.Fatalf("NormalizeMoneyInput(%q) = %q, quer %q", input, got, want)
		}
	}
}

func TestCovFmtFormatPhoneInput(t *testing.T) {
	cases := map[string]string{
		"":                "",
		"1":               "(1",
		"22":              "(22",
		"229":             "(22) 9",
		"229888":          "(22) 9888",
		"2298887":         "(22) 9888-7",
		"22988871122":     "(22) 98887-1122",
		"(22) 99888-1122": "(22) 99888-1122",
		"229888711223344": "(22) 98887-1122", // trunca em 11 dígitos
	}
	for input, want := range cases {
		if got := FormatPhoneInput(input); got != want {
			t.Fatalf("FormatPhoneInput(%q) = %q, quer %q", input, got, want)
		}
	}
}

func TestCovFmtFormatCEPInput(t *testing.T) {
	cases := map[string]string{
		"":           "",
		"123":        "123",
		"12345":      "12345",
		"123456":     "12345-6",
		"12345678":   "12345-678",
		"28.907-250": "28907-250",
		"1234567890": "12345-678", // trunca em 8 dígitos
	}
	for input, want := range cases {
		if got := FormatCEPInput(input); got != want {
			t.Fatalf("FormatCEPInput(%q) = %q, quer %q", input, got, want)
		}
	}
}

func TestCovFmtFormatCPFInput(t *testing.T) {
	cases := map[string]string{
		"":               "",
		"123":            "123",
		"1234":           "123.4",
		"123456":         "123.456",
		"1234567":        "123.456.7",
		"123456789":      "123.456.789",
		"12345678901":    "123.456.789-01",
		"111.222.333-44": "111.222.333-44",
		"1234567890123":  "123.456.789-01", // trunca em 11 dígitos
	}
	for input, want := range cases {
		if got := FormatCPFInput(input); got != want {
			t.Fatalf("FormatCPFInput(%q) = %q, quer %q", input, got, want)
		}
	}
}

func TestCovFmtParseLocalDate(t *testing.T) {
	got := ParseLocalDate("2026-09-10")
	want := time.Date(2026, 9, 10, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("ParseLocalDate = %v, quer %v", got, want)
	}
	// entrada inválida cai no marco zero do app
	got = ParseLocalDate("sem-data")
	if !got.Equal(time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local)) {
		t.Fatalf("ParseLocalDate inválido = %v", got)
	}
}

func TestCovFmtParseLocalDateTime(t *testing.T) {
	cases := map[string]time.Time{
		"2026-09-10T14:30": time.Date(2026, 9, 10, 14, 30, 0, 0, time.Local),
		"2026-09-10 08:05": time.Date(2026, 9, 10, 8, 5, 0, 0, time.Local),
		"2026-09-10":       time.Date(2026, 9, 10, 0, 0, 0, 0, time.Local),
		"invalido":         time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local),
	}
	for input, want := range cases {
		if got := ParseLocalDateTime(input); !got.Equal(want) {
			t.Fatalf("ParseLocalDateTime(%q) = %v, quer %v", input, got, want)
		}
	}
}

func TestCovFmtFormatLocalStamps(t *testing.T) {
	moment := time.Date(2026, 9, 10, 14, 30, 45, 123456789, time.Local)

	if got := FormatLocalDate(moment); got != "2026-09-10" {
		t.Fatalf("FormatLocalDate = %q", got)
	}
	if got := FormatLocalDateTime(moment); got != "2026-09-10T14:30" {
		t.Fatalf("FormatLocalDateTime = %q", got)
	}
	if got := FormatLocalDateTimeMillis(moment); got != "2026-09-10T14:30:45.123" {
		t.Fatalf("FormatLocalDateTimeMillis = %q", got)
	}

	zone := time.FixedZone("BRT", -3*60*60)
	local := time.Date(2026, 9, 10, 14, 30, 45, 0, zone)
	if got := FormatUTCStamp(local); got != "2026-09-10 17:30:45" {
		t.Fatalf("FormatUTCStamp = %q, quer %q", got, "2026-09-10 17:30:45")
	}
}
