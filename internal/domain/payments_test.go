package domain

import (
	"testing"
	"time"
)

func TestEvaluatePaymentMatchApprovesOnlyWhenEverythingCloses(t *testing.T) {
	today := time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)
	paidAt := "2026-09-10T10:00"
	openReceipt := &Receipt{
		ID: "receipt-1", DueDate: "2026-09-10", Amount: 2500, TenantName: "Carlos", PropertyName: "Apto 101",
	}
	closedReceipt := &Receipt{
		ID: "receipt-2", DueDate: "2026-09-10", Amount: 2500, PaidAt: &paidAt,
	}

	base := func() PaymentMatchInput {
		return PaymentMatchInput{
			Extracted: PaymentExtraction{
				Amount: 2500, Date: "2026-09-09", Payer: "Carlos Drummond",
				Payee: "Locador Teste", Identifier: "E12345678", Confidence: 0.97,
			},
			Receipt:       openReceipt,
			LandlordName:  "Locador Teste",
			LandlordCPF:   "111.222.333-44",
			Today:         today,
			MinConfidence: 0.9,
			WindowDays:    10,
		}
	}

	if result := EvaluatePaymentMatch(base()); !result.Approved {
		t.Fatalf("comprovante completo deveria ser aprovado: %s", result.Reason)
	}

	cases := []struct {
		name   string
		mutate func(*PaymentMatchInput)
	}{
		{"confiança baixa", func(in *PaymentMatchInput) { in.Extracted.Confidence = 0.4 }},
		{"sem recibo aberto", func(in *PaymentMatchInput) { in.Receipt = nil }},
		{"recibo já quitado", func(in *PaymentMatchInput) { in.Receipt = closedReceipt }},
		{"valor zerado no comprovante", func(in *PaymentMatchInput) { in.Extracted.Amount = 0 }},
		{"valor divergente", func(in *PaymentMatchInput) { in.Extracted.Amount = 2400 }},
		{"destinatário errado", func(in *PaymentMatchInput) { in.Extracted.Payee = "Outra Pessoa" }},
		{"destinatário vazio", func(in *PaymentMatchInput) { in.Extracted.Payee = "" }},
		{"data fora da janela", func(in *PaymentMatchInput) { in.Extracted.Date = "2026-07-01" }},
		{"data no futuro distante", func(in *PaymentMatchInput) { in.Extracted.Date = "2026-12-01" }},
		{"data ilegível", func(in *PaymentMatchInput) { in.Extracted.Date = "ontem" }},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			input := base()
			testCase.mutate(&input)
			result := EvaluatePaymentMatch(input)
			if result.Approved {
				t.Fatalf("não deveria aprovar (%s)", testCase.name)
			}
			if result.Reason == "" {
				t.Fatal("recusa precisa de motivo")
			}
		})
	}
}

func TestEvaluatePaymentMatchAcceptsLandlordCPFAndPartialName(t *testing.T) {
	today := time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)
	receipt := &Receipt{ID: "r", DueDate: "2026-09-10", Amount: 1500}

	cases := map[string]string{
		"CPF formatado":          "111.222.333-44",
		"CPF em dígitos":         "11122233344",
		"nome completo":          "Locador Teste",
		"nome em maiúscula":      "LOCADOR TESTE",
		"primeiro e último nome": "Locador da Silva Teste",
	}

	for name, payee := range cases {
		t.Run(name, func(t *testing.T) {
			result := EvaluatePaymentMatch(PaymentMatchInput{
				Extracted:    PaymentExtraction{Amount: 1500, Date: "2026-09-10", Payee: payee, Confidence: 1},
				Receipt:      receipt,
				LandlordName: "Locador Teste",
				LandlordCPF:  "111.222.333-44",
				Today:        today,
				WindowDays:   10,
			})
			if !result.Approved {
				t.Fatalf("destinatário %q deveria ser aceito: %s", payee, result.Reason)
			}
		})
	}
}

func TestEvaluatePaymentMatchIgnoresConfidenceWhenLimitIsZero(t *testing.T) {
	today := time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)
	result := EvaluatePaymentMatch(PaymentMatchInput{
		Extracted:     PaymentExtraction{Amount: 1000, Date: "2026-09-10", Payee: "Locador", Confidence: 0.1},
		Receipt:       &Receipt{ID: "r", DueDate: "2026-09-10", Amount: 1000},
		LandlordName:  "Locador",
		Today:         today,
		MinConfidence: 0,
		WindowDays:    10,
	})
	if !result.Approved {
		t.Fatalf("sem limite de confiança a decisão é pelas demais regras: %s", result.Reason)
	}
}

func TestJobLifecycleHelpers(t *testing.T) {
	job := Job{Kind: JobKindPaymentProof, Status: JobStatusQueued, Attempts: 0, MaxAttempts: 3}
	if !job.CanRetry() {
		t.Fatal("job sem tentativas pode ser retentado")
	}
	job.Attempts = 3
	if job.CanRetry() {
		t.Fatal("job com tentativas esgotadas não pode ser retentado")
	}
	if JobStatusLabel(JobStatusFailed) != "Falhou" || JobStatusLabel(JobStatus("outro")) != "outro" {
		t.Fatal("rótulos de estado do job inesperados")
	}
}

func TestPaymentAndIntakeLabels(t *testing.T) {
	if PaymentProofStatusLabel(PaymentProofStatusReview) != "Em revisão" {
		t.Fatal("rótulo de comprovante em revisão inesperado")
	}
	if PaymentDecisionLabel(PaymentDecisionAuto) != "Automática" || PaymentDecisionLabel(PaymentDecision("x")) != "Pendente" {
		t.Fatal("rótulos de decisão inesperados")
	}
	if !IntakeKindLease.Valid() || IntakeKind("outro").Valid() {
		t.Fatal("validação de tipo de documento inesperada")
	}
	if IntakeKindLabel(IntakeKindIDDocument) != "Documento de identidade" {
		t.Fatal("rótulo de documento pessoal inesperado")
	}
	if IntakeStatusLabel(IntakeStatusConfirmed) != "Confirmado" {
		t.Fatal("rótulo de situação do intake inesperado")
	}
}

func TestPaymentProofExtractionConvertsPointers(t *testing.T) {
	proof := PaymentProof{
		ExtractedAmount: FloatPtr(2500.5),
		ExtractedDate:   StrPtr("2026-09-10"),
		ExtractedPayer:  StrPtr("Carlos"),
		Confidence:      FloatPtr(0.91),
	}
	extraction := proof.Extraction()
	if extraction.Amount != 2500.5 || extraction.Date != "2026-09-10" || extraction.Payer != "Carlos" || extraction.Confidence != 0.91 {
		t.Fatalf("extração convertida errada: %+v", extraction)
	}
	if extraction.Payee != "" || extraction.Identifier != "" {
		t.Fatalf("campos ausentes deveriam vir vazios: %+v", extraction)
	}
}
