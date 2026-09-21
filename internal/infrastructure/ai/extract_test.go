package ai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
)

// extrairComprovante roda a extração contra o provedor falso com o conteúdo
// devolvido pelo "modelo" — é o caminho que o consumidor enxerga.
func extrairComprovante(t *testing.T, content string) (domain.PaymentExtraction, string, error) {
	t.Helper()
	fake := newFakeProvider(t, respondContent(t, content))
	return fake.client(Config{}).ExtractPaymentProof(context.Background(), document())
}

func extrairContrato(t *testing.T, content string) (domain.LeaseIntakeFields, string, error) {
	t.Helper()
	fake := newFakeProvider(t, respondContent(t, content))
	return fake.client(Config{}).ExtractLeaseDocument(context.Background(), document())
}

func extrairPessoa(t *testing.T, content string) (domain.PersonIntakeFields, string, error) {
	t.Helper()
	fake := newFakeProvider(t, respondContent(t, content))
	return fake.client(Config{}).ExtractPersonDocument(context.Background(), document())
}

// ── Comprovante de pagamento ───────────────────────────────────────

func TestExtractPaymentProofValid(t *testing.T) {
	content := `{"amount": 1500.5, "date": "2026-09-10", "payer": "Ana Souza", "payee": "Carlos Lima", "identifier": "E123456789", "confidence": 0.97}`
	extraction, raw, err := extrairComprovante(t, content)
	if err != nil {
		t.Fatalf("extração falhou: %v", err)
	}
	want := domain.PaymentExtraction{
		Amount:     1500.5,
		Date:       "2026-09-10",
		Payer:      "Ana Souza",
		Payee:      "Carlos Lima",
		Identifier: "E123456789",
		Confidence: 0.97,
	}
	if extraction != want {
		t.Errorf("extração = %+v, esperada %+v", extraction, want)
	}
	if raw != content {
		t.Errorf("resposta crua = %q, esperada a do provedor", raw)
	}
}

func TestExtractPaymentProofTolerantContent(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"cerca de código", "```json\n{\"amount\": 10, \"date\": \"2026-09-01\", \"confidence\": 0.5}\n```"},
		{"cerca sem linguagem", "```\n{\"amount\": 10, \"date\": \"2026-09-01\", \"confidence\": 0.5}\n```"},
		{"texto em volta", "Claro! Aqui está:\n{\"amount\": 10, \"date\": \"2026-09-01\", \"confidence\": 0.5}\nEspero ter ajudado."},
		{"campos extras", "{\"amount\": 10, \"date\": \"2026-09-01\", \"confidence\": 0.5, \"banco\": \"Nubank\"}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			extraction, _, err := extrairComprovante(t, tc.content)
			if err != nil {
				t.Fatalf("extração falhou: %v", err)
			}
			if extraction.Amount != 10 || extraction.Date != "2026-09-01" || extraction.Confidence != 0.5 {
				t.Errorf("extração = %+v", extraction)
			}
		})
	}
}

func TestExtractPaymentProofAmountAsText(t *testing.T) {
	cases := []struct {
		amount string
		want   float64
	}{
		{`"R$ 1.500,50"`, 1500.5},
		{`"1500,50"`, 1500.5},
		{`"1500.50"`, 1500.5},
	}
	for _, tc := range cases {
		t.Run(tc.amount, func(t *testing.T) {
			content := `{"amount": ` + tc.amount + `, "date": "2026-09-01", "confidence": 0.9}`
			extraction, _, err := extrairComprovante(t, content)
			if err != nil {
				t.Fatalf("extração falhou: %v", err)
			}
			if extraction.Amount != tc.want {
				t.Errorf("valor = %v, esperado %v", extraction.Amount, tc.want)
			}
		})
	}
}

func TestExtractPaymentProofRejections(t *testing.T) {
	cases := []struct {
		name    string
		content string
		campo   string
	}{
		{"objeto vazio", `{}`, "amount"},
		{"valor ausente", `{"date": "2026-09-01", "confidence": 0.9}`, "amount"},
		{"valor nulo", `{"amount": null, "date": "2026-09-01", "confidence": 0.9}`, "amount"},
		{"valor zero", `{"amount": 0, "date": "2026-09-01", "confidence": 0.9}`, "amount"},
		{"valor negativo", `{"amount": -10, "date": "2026-09-01", "confidence": 0.9}`, "amount"},
		{"valor não numérico", `{"amount": "não sei", "date": "2026-09-01", "confidence": 0.9}`, "amount"},
		{"data ausente", `{"amount": 10, "confidence": 0.9}`, "date"},
		{"data fora do formato", `{"amount": 10, "date": "10/09/2026", "confidence": 0.9}`, "date"},
		{"data inexistente", `{"amount": 10, "date": "2026-02-31", "confidence": 0.9}`, "date"},
		{"confiança ausente", `{"amount": 10, "date": "2026-09-01"}`, "confidence"},
		{"confiança acima de 1", `{"amount": 10, "date": "2026-09-01", "confidence": 1.2}`, "confidence"},
		{"confiança negativa", `{"amount": 10, "date": "2026-09-01", "confidence": -0.1}`, "confidence"},
		{"confiança não numérica", `{"amount": 10, "date": "2026-09-01", "confidence": "alta"}`, "confidence"},
		{"campo de texto inválido", `{"amount": 10, "date": "2026-09-01", "payer": ["Ana"], "confidence": 0.9}`, "payer"},
		{"JSON inválido", `o documento é um comprovante de aluguel`, "JSON"},
		{"JSON não é objeto", `[1, 2, 3]`, "JSON"},
		{"JSON truncado", `{"amount": 10, "date": "2026-09-01"`, "JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			extraction, raw, err := extrairComprovante(t, tc.content)
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("erro = %v, esperado ErrInvalidResponse", err)
			}
			if !strings.Contains(err.Error(), tc.campo) {
				t.Errorf("mensagem = %q, esperada citando %q", err.Error(), tc.campo)
			}
			if extraction != (domain.PaymentExtraction{}) {
				t.Errorf("extração = %+v, esperada vazia", extraction)
			}
			if raw != tc.content {
				t.Errorf("resposta crua = %q, esperada a do provedor para a trilha", raw)
			}
		})
	}
}

func TestExtractPaymentProofOptionalFieldsStayEmpty(t *testing.T) {
	extraction, _, err := extrairComprovante(t, `{"amount": 900, "date": "2026-09-05", "confidence": 0.6}`)
	if err != nil {
		t.Fatalf("extração falhou: %v", err)
	}
	if extraction.Payer != "" || extraction.Payee != "" || extraction.Identifier != "" {
		t.Errorf("campos não lidos deveriam ficar vazios: %+v", extraction)
	}
}

// A mensagem de erro vai para o log da fila: não pode carregar o que a IA leu
// (dado pessoal, BR-202).
func TestErrorMessagesDoNotLeakDocumentData(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"data inválida com pagador legível", `{"amount": 1500.5, "date": "10/09/2026", "payer": "Ana Souza Lima", "confidence": 0.9}`},
		{"confiança inválida com destinatário legível", `{"amount": 1500.5, "date": "2026-09-10", "payee": "Carlos Pereira", "confidence": 1.4}`},
		{"JSON inválido com nome legível", `{"payer": "Ana Souza Lima", "cpf": "123.456.789-01"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := extrairComprovante(t, tc.content)
			if err == nil {
				t.Fatal("resposta fora do schema deveria falhar")
			}
			for _, leaked := range []string{"Ana Souza Lima", "Carlos Pereira", "123.456.789-01", "1500.5"} {
				if strings.Contains(err.Error(), leaked) {
					t.Errorf("mensagem %q vazou dado do documento (%q)", err.Error(), leaked)
				}
			}
		})
	}
}

// ── Contrato de locação ────────────────────────────────────────────

func TestExtractLeaseDocumentValid(t *testing.T) {
	content := `{"tenantName": "Maria Silva", "tenantCpf": 12345678901, "tenantEmail": "maria@exemplo.com", "tenantPhone": "11999998888", "propertyName": "Rua A, 10", "propertyCity": "São Paulo", "propertyState": "SP", "startDate": "2024-01-01", "endDate": "2026-01-01", "dueDay": 5, "rentAmount": "R$ 2.500,00", "adjustIndex": "IGP-M", "guarantee": "fiador", "confidence": 0.82}`
	fields, raw, err := extrairContrato(t, content)
	if err != nil {
		t.Fatalf("extração falhou: %v", err)
	}
	want := domain.LeaseIntakeFields{
		TenantName:    "Maria Silva",
		TenantCPF:     "12345678901",
		TenantEmail:   "maria@exemplo.com",
		TenantPhone:   "11999998888",
		PropertyName:  "Rua A, 10",
		PropertyCity:  "São Paulo",
		PropertyState: "SP",
		StartDate:     "2024-01-01",
		EndDate:       "2026-01-01",
		DueDay:        5,
		RentAmount:    2500,
		AdjustIndex:   "IGP-M",
		Guarantee:     "fiador",
		Confidence:    0.82,
	}
	if fields != want {
		t.Errorf("campos = %+v, esperados %+v", fields, want)
	}
	if raw != content {
		t.Errorf("resposta crua = %q", raw)
	}
}

func TestExtractLeaseDocumentRejections(t *testing.T) {
	cases := []struct {
		name    string
		content string
		campo   string
	}{
		{"locatário ausente", `{"confidence": 0.9}`, "tenantName"},
		{"locatário vazio", `{"tenantName": "  ", "confidence": 0.9}`, "tenantName"},
		{"data de início inválida", `{"tenantName": "Maria", "startDate": "01/01/2024", "confidence": 0.9}`, "startDate"},
		{"data de fim inválida", `{"tenantName": "Maria", "endDate": "2024", "confidence": 0.9}`, "endDate"},
		{"dia de vencimento fora da faixa", `{"tenantName": "Maria", "dueDay": 40, "confidence": 0.9}`, "dueDay"},
		{"dia de vencimento fracionário", `{"tenantName": "Maria", "dueDay": 5.5, "confidence": 0.9}`, "dueDay"},
		{"aluguel inválido", `{"tenantName": "Maria", "rentAmount": "muito", "confidence": 0.9}`, "rentAmount"},
		{"confiança ausente", `{"tenantName": "Maria"}`, "confidence"},
		{"JSON inválido", `não consegui ler`, "JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields, raw, err := extrairContrato(t, tc.content)
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("erro = %v, esperado ErrInvalidResponse", err)
			}
			if !strings.Contains(err.Error(), tc.campo) {
				t.Errorf("mensagem = %q, esperada citando %q", err.Error(), tc.campo)
			}
			if fields != (domain.LeaseIntakeFields{}) {
				t.Errorf("campos = %+v, esperados vazios", fields)
			}
			if raw != tc.content {
				t.Errorf("resposta crua = %q", raw)
			}
		})
	}
}

func TestExtractLeaseDocumentOptionalFieldsStayEmpty(t *testing.T) {
	fields, _, err := extrairContrato(t, `{"tenantName": "Maria Silva", "confidence": 0.5}`)
	if err != nil {
		t.Fatalf("extração falhou: %v", err)
	}
	if fields.StartDate != "" || fields.EndDate != "" || fields.DueDay != 0 || fields.RentAmount != 0 {
		t.Errorf("campos não lidos deveriam ficar vazios: %+v", fields)
	}
}

// ── Documento pessoal ──────────────────────────────────────────────

func TestExtractPersonDocumentValid(t *testing.T) {
	content := `{"name": "João Pereira", "cpf": "123.456.789-01", "email": "joao@exemplo.com", "phone": "(11) 98888-7777", "address": "Rua B, 20, ap 3", "city": "Campinas", "state": "SP", "zip": "13000-000", "confidence": 0.75}`
	fields, raw, err := extrairPessoa(t, content)
	if err != nil {
		t.Fatalf("extração falhou: %v", err)
	}
	want := domain.PersonIntakeFields{
		Name:       "João Pereira",
		CPF:        "123.456.789-01",
		Email:      "joao@exemplo.com",
		Phone:      "(11) 98888-7777",
		Address:    "Rua B, 20, ap 3",
		City:       "Campinas",
		State:      "SP",
		Zip:        "13000-000",
		Confidence: 0.75,
	}
	if fields != want {
		t.Errorf("campos = %+v, esperados %+v", fields, want)
	}
	if raw != content {
		t.Errorf("resposta crua = %q", raw)
	}
}

func TestExtractPersonDocumentRejections(t *testing.T) {
	cases := []struct {
		name    string
		content string
		campo   string
	}{
		{"nome ausente", `{"cpf": "123", "confidence": 0.9}`, "name"},
		{"nome vazio", `{"name": "", "confidence": 0.9}`, "name"},
		{"CPF como lista", `{"name": "João", "cpf": ["1"], "confidence": 0.9}`, "cpf"},
		{"confiança fora da faixa", `{"name": "João", "confidence": 9}`, "confidence"},
		{"JSON inválido", `documento ilegível`, "JSON"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields, raw, err := extrairPessoa(t, tc.content)
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("erro = %v, esperado ErrInvalidResponse", err)
			}
			if !strings.Contains(err.Error(), tc.campo) {
				t.Errorf("mensagem = %q, esperada citando %q", err.Error(), tc.campo)
			}
			if fields != (domain.PersonIntakeFields{}) {
				t.Errorf("campos = %+v, esperados vazios", fields)
			}
			if raw != tc.content {
				t.Errorf("resposta crua = %q", raw)
			}
		})
	}
}
