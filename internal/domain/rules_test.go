package domain

import (
	"testing"
	"time"
)

func TestOverlaps(t *testing.T) {
	cases := []struct {
		name         string
		startA, endA string
		startB, endB string
		want         bool
	}{
		{"períodos idênticos", "2026-01-01", "2026-12-31", "2026-01-01", "2026-12-31", true},
		{"sobreposição parcial no fim", "2026-01-01", "2026-06-30", "2026-06-01", "2026-12-31", true},
		{"sobreposição parcial no início", "2026-06-01", "2026-12-31", "2026-01-01", "2026-06-30", true},
		{"contido", "2026-03-01", "2026-04-01", "2026-01-01", "2026-12-31", true},
		{"encostado no fim (término = início)", "2026-01-01", "2026-06-01", "2026-06-01", "2026-07-01", true},
		{"posterior sem toque", "2026-07-01", "2026-08-01", "2026-01-01", "2026-06-30", false},
		{"anterior sem toque", "2025-01-01", "2025-06-30", "2026-01-01", "2026-06-30", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := Overlaps(testCase.startA, testCase.endA, testCase.startB, testCase.endB)
			if got != testCase.want {
				t.Fatalf("Overlaps(%s,%s,%s,%s) = %v, esperado %v",
					testCase.startA, testCase.endA, testCase.startB, testCase.endB, got, testCase.want)
			}
		})
	}
}

func TestDueDateForMonthClampsToLastDay(t *testing.T) {
	cases := []struct {
		year, month, dueDay int
		want                string
	}{
		{2026, 2, 31, "2026-02-28"},
		{2028, 2, 31, "2028-02-29"},
		{2026, 4, 31, "2026-04-30"},
		{2026, 9, 10, "2026-09-10"},
		{2026, 9, 0, "2026-09-01"},
	}

	for _, testCase := range cases {
		got := DueDateForMonth(testCase.year, testCase.month, testCase.dueDay)
		if got != testCase.want {
			t.Errorf("DueDateForMonth(%d,%d,%d) = %s, esperado %s",
				testCase.year, testCase.month, testCase.dueDay, got, testCase.want)
		}
	}
}

func TestReceiptStatusOf(t *testing.T) {
	today := time.Date(2026, 9, 21, 15, 0, 0, 0, time.Local)
	paid := "2026-09-10T10:00"
	amount := func(value float64) *float64 { return &value }

	cases := []struct {
		name    string
		receipt Receipt
		want    ReceiptStatus
	}{
		{
			name:    "sem pagamento e vencimento futuro",
			receipt: Receipt{DueDate: "2026-09-30", Amount: 2500},
			want:    ReceiptStatusDue,
		},
		{
			name:    "sem pagamento e vencimento passado",
			receipt: Receipt{DueDate: "2026-09-10", Amount: 2500},
			want:    ReceiptStatusOverdue,
		},
		{
			name:    "vencimento hoje ainda não venceu",
			receipt: Receipt{DueDate: "2026-09-21", Amount: 2500},
			want:    ReceiptStatusDue,
		},
		{
			name:    "pago pelo registro de pagamento",
			receipt: Receipt{DueDate: "2026-09-10", Amount: 2500, PaidAt: &paid},
			want:    ReceiptStatusPaid,
		},
		{
			name:    "pagamento parcial explícito",
			receipt: Receipt{DueDate: "2026-09-10", Amount: 2500, PaidAt: &paid, PaidAmount: amount(1200)},
			want:    ReceiptStatusPartial,
		},
		{
			name:    "pagamento integral explícito",
			receipt: Receipt{DueDate: "2026-09-10", Amount: 2500, PaidAt: &paid, PaidAmount: amount(2500)},
			want:    ReceiptStatusPaid,
		},
		{
			name:    "valor pago maior que o total continua pago",
			receipt: Receipt{DueDate: "2026-09-10", Amount: 2500, PaidAmount: amount(2600)},
			want:    ReceiptStatusPaid,
		},
		{
			name:    "valor pago zerado cai na regra de vencimento",
			receipt: Receipt{DueDate: "2026-09-10", Amount: 2500, PaidAmount: amount(0)},
			want:    ReceiptStatusOverdue,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := ReceiptStatusOf(testCase.receipt, today)
			if got != testCase.want {
				t.Fatalf("ReceiptStatusOf = %s, esperado %s", got, testCase.want)
			}
		})
	}
}

func TestValidateEmail(t *testing.T) {
	valid := []string{"ana@exemplo.com", "ana.silva+locacao@exemplo.com.br"}
	for _, email := range valid {
		if err := ValidateEmail(email); err != nil {
			t.Errorf("ValidateEmail(%q) devolveu erro: %v", email, err)
		}
	}

	invalid := []string{"", "sem-arroba", "sem@tld", "com espaço@exemplo.com", "@exemplo.com"}
	for _, email := range invalid {
		err := ValidateEmail(email)
		if err == nil {
			t.Errorf("ValidateEmail(%q) deveria falhar", email)
			continue
		}
		if ValidationMessage(err) != "Informe um e-mail válido." {
			t.Errorf("mensagem inesperada para %q: %s", email, ValidationMessage(err))
		}
	}
}

func TestGeneratedKeyForMonth(t *testing.T) {
	if got := GeneratedKeyForMonth("lease-1", 2026, 8); got != "lease-1:2026-08" {
		t.Fatalf("chave de geração = %s", got)
	}
}

func TestLeaseStatusRules(t *testing.T) {
	if !LeaseStatusActive.BlocksOverlap() || !LeaseStatusSeason.BlocksOverlap() {
		t.Fatal("ativo e temporada devem bloquear sobreposição")
	}
	if LeaseStatusSuspended.BlocksOverlap() || LeaseStatusTerminated.BlocksOverlap() {
		t.Fatal("suspenso e encerrado não podem bloquear sobreposição")
	}
	if LeaseStatusTerminated.SelectableOnCreate() {
		t.Fatal("encerrado não pode ser escolhido na criação")
	}
	if !LeaseStatusTerminated.SelectableOnUpdate() {
		t.Fatal("encerrado deve ser escolhido na edição")
	}
	if LeaseStatusLabel(LeaseStatusSeason) != "Temporada" {
		t.Fatalf("rótulo de temporada = %s", LeaseStatusLabel(LeaseStatusSeason))
	}
}
