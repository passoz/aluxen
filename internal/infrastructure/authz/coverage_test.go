package authz_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/infrastructure/authz"
)

// Segredo e documento deste arquivo: os vetores ficam aqui para que a bateria
// não dependa das constantes do outro arquivo de teste do pacote.
const (
	covSecret     = "segredo-de-teste-da-cobertura"
	covDocumentID = "018f2c1e-6b3d-7c4a-9e21-5f6a7b8c9d0f"
	// covLegacySecret é o segredo padrão embutido do app Bun, removido do port:
	// nenhuma instalação pode assinar com ele por falta de configuração.
	covLegacySecret = "aluxen-authenticity-secret-key-2026"
)

// A chave legível do documento é recortada de quatro em quatro caracteres.
// Um token com tamanho inesperado (cópia manual incompleta, leitura parcial do
// QR Code) não pode sair com grupos inventados nem entrar em pânico: só os
// caracteres disponíveis viram grupos.
func TestCovFormatCodeHandlesUnexpectedTokenLengths(t *testing.T) {
	generator := authz.NewTokenGenerator(covSecret)

	cases := []struct {
		name  string
		token string
		want  string
	}{
		{"token vazio", "", "AUTH----"},
		{"menos de um grupo", "AB", "AUTH-AB---"},
		{"menos de dois grupos", "ABCDEF", "AUTH-ABCD-EF--"},
		{"exatamente três grupos", "ABCDEFGHIJKL", "AUTH-ABCD-EFGH-IJKL-"},
		{"sem caractere alfanumérico", "---- ::::", "AUTH----"},
		{"quatro grupos completos", "abcdef0123456789", "AUTH-ABCD-EF01-2345-6789"},
	}
	for _, testCase := range cases {
		if got := generator.FormatCode(testCase.token); got != testCase.want {
			t.Errorf("%s: FormatCode(%q) = %q, esperado %q", testCase.name, testCase.token, got, testCase.want)
		}
	}
}

// A chave impressa usa exatamente os 16 primeiros caracteres alfanuméricos do
// token: um token colado duas vezes (ou com pontuação) imprime a mesma chave.
func TestCovFormatCodeUsesOnlyTheFirstFourGroups(t *testing.T) {
	generator := authz.NewTokenGenerator(covSecret)
	token := generator.Generate(authz.KindReceipt, covDocumentID)
	if len(token) != authz.TokenLength {
		t.Fatalf("token = %q, esperado %d caracteres", token, authz.TokenLength)
	}

	upper := strings.ToUpper(token)
	want := "AUTH-" + upper[0:4] + "-" + upper[4:8] + "-" + upper[8:12] + "-" + upper[12:16]
	if got := generator.FormatCode(token); got != want {
		t.Errorf("FormatCode(token) = %q, esperado %q", got, want)
	}
	if got := generator.FormatCode(token + token); got != want {
		t.Errorf("FormatCode(token repetido) = %q, esperado %q", got, want)
	}
	if got := generator.FormatCode(strings.ToUpper(token)); got != want {
		t.Errorf("FormatCode(token em maiúsculas) = %q, esperado %q", got, want)
	}
}

// Sem segredo configurado não existe chave padrão: o gerador assina com a chave
// vazia em vez de cair no segredo embutido do app antigo, que era compartilhado
// por todas as instalações (permitia forjar a chave AUTH de qualquer documento).
func TestCovGeneratorWithoutSecretDoesNotFallBackToTheLegacyKey(t *testing.T) {
	empty := authz.NewTokenGenerator("")
	legacy := authz.NewTokenGenerator(covLegacySecret)

	token := empty.Generate(authz.KindReceipt, covDocumentID)
	if token != empty.Generate(authz.KindReceipt, covDocumentID) {
		t.Fatalf("geração sem segredo não é determinística: %q", token)
	}
	if len(token) != authz.TokenLength {
		t.Errorf("token sem segredo tem %d caracteres, esperado %d", len(token), authz.TokenLength)
	}
	if token == legacy.Generate(authz.KindReceipt, covDocumentID) {
		t.Error("gerador sem segredo assinou com a chave padrão do app antigo")
	}
	if !empty.Verify(authz.KindReceipt, covDocumentID, token) {
		t.Error("token gerado sem segredo foi recusado pelo próprio gerador")
	}
	if empty.Verify(authz.KindReceipt, covDocumentID, legacy.Generate(authz.KindReceipt, covDocumentID)) {
		t.Error("token do segredo antigo foi aceito por um gerador sem segredo")
	}
}

// Um token do mesmo comprimento com um caractere trocado não confere: é o
// caminho que pega a adulteração de um documento impresso.
func TestCovVerifyRejectsTokensTamperedAtTheSameLength(t *testing.T) {
	generator := authz.NewTokenGenerator(covSecret)
	token := generator.Generate(authz.KindReceipt, covDocumentID)

	tampered := []byte(token)
	if tampered[len(tampered)-1] == '0' {
		tampered[len(tampered)-1] = '1'
	} else {
		tampered[len(tampered)-1] = '0'
	}
	if generator.Verify(authz.KindReceipt, covDocumentID, string(tampered)) {
		t.Errorf("token adulterado %q foi aceito", tampered)
	}

	// Um único bit trocado no primeiro caractere também muda a chave.
	flipped := []byte(token)
	flipped[0] = '0'
	if flipped[0] == token[0] {
		flipped[0] = '1'
	}
	if generator.Verify(authz.KindReceipt, covDocumentID, string(flipped)) {
		t.Errorf("token adulterado %q foi aceito", flipped)
	}
}

// Os quatro documentos da temporada têm o seu caminho público de validação, e
// um tipo desconhecido cai no recibo — o documento padrão do app.
func TestCovValidationURLForEveryKindAndUnknownFallback(t *testing.T) {
	generator := authz.NewTokenGenerator(covSecret)

	cases := map[authz.Kind]string{
		authz.KindReceipt:    "/validar/recibo/",
		authz.KindLease:      "/validar/contrato/",
		authz.KindInspection: "/validar/vistoria/",
		authz.KindDeposit:    "/validar/caucao/",
	}
	for kind, path := range cases {
		got := generator.ValidationURL("https://aluxen.evolucsia.com", kind, covDocumentID, "token")
		want := "https://aluxen.evolucsia.com" + path + covDocumentID + "?token=token"
		if got != want {
			t.Errorf("URL de %s = %q, esperado %q", kind, got, want)
		}
	}

	receipt := generator.ValidationURL("https://aluxen.evolucsia.com", authz.KindReceipt, covDocumentID, "token")
	for _, kind := range []authz.Kind{"", "outro", "RECEIPT", "recibo", "contrato"} {
		if got := generator.ValidationURL("https://aluxen.evolucsia.com", kind, covDocumentID, "token"); got != receipt {
			t.Errorf("tipo desconhecido %q: URL = %q, esperado o caminho do recibo %q", kind, got, receipt)
		}
	}
}

// O QR Code recusa conteúdo que não cabe no padrão — e também o conteúdo vazio,
// que sairia como um selo sem dado nenhum — em vez de devolver uma imagem
// ilegível. O conteúdo curto continua desenhando um PNG válido.
func TestCovQRCodeRejectsEmptyAndOversizedContent(t *testing.T) {
	generator := authz.NewTokenGenerator(covSecret)

	for _, content := range []string{"", strings.Repeat("A", 8000)} {
		if _, err := generator.QRCode(content); err == nil {
			t.Errorf("QRCode(%d caracteres) deveria devolver erro", len(content))
		}
	}

	png, err := generator.QRCode("a")
	if err != nil {
		t.Fatalf("QRCode do conteúdo curto devolveu erro: %v", err)
	}
	if !bytes.HasPrefix(png, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}) {
		t.Error("QR Code do conteúdo curto não é um PNG")
	}
}
