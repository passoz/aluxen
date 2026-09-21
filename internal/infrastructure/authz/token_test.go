package authz_test

import (
	"bytes"
	"image/png"
	"regexp"
	"testing"

	"github.com/passoz/aluxen/internal/infrastructure/authz"
)

// ID de teste (UUIDv7) usado em todos os vetores deste arquivo.
const testReceiptID = "018f2c1e-6b3d-7c4a-9e21-5f6a7b8c9d0e"

// Vetores calculados fora do código (openssl dgst -sha256 -hmac) para fixar o
// formato do token `HMAC-SHA256(segredo, "<kind>:<id>")[:32]`. O primeiro vetor
// usa o segredo padrão do app Bun: é ele que garante que os documentos já
// impressos continuam válidos quando a instância tem o mesmo SESSION_SECRET.
const (
	legacySecret       = "aluxen-authenticity-secret-key-2026"
	legacyReceiptToken = "26e2ba16fd4168935f107b30810a1e3b"
	legacyLeaseToken   = "f5056b3371be8a715796f2822ad83c54"
)

func TestGenerateIsDeterministicAndMatchesLegacyApp(t *testing.T) {
	generator := authz.NewTokenGenerator(legacySecret)

	token := generator.Generate(authz.KindReceipt, testReceiptID)
	if token != legacyReceiptToken {
		t.Fatalf("token do recibo = %q, esperado %q", token, legacyReceiptToken)
	}
	if again := generator.Generate(authz.KindReceipt, testReceiptID); again != token {
		t.Fatalf("geração repetida devolveu %q, esperado %q", again, token)
	}
	if lease := generator.Generate(authz.KindLease, testReceiptID); lease != legacyLeaseToken {
		t.Fatalf("token do contrato = %q, esperado %q", lease, legacyLeaseToken)
	}
}

func TestGenerateProducesThirtyTwoHexCharacters(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{32}$`)
	generator := authz.NewTokenGenerator("segredo-de-teste")

	for _, kind := range []authz.Kind{authz.KindReceipt, authz.KindLease} {
		token := generator.Generate(kind, testReceiptID)
		if !pattern.MatchString(token) {
			t.Fatalf("token de %s = %q: esperado 32 caracteres hexadecimais minúsculos", kind, token)
		}
		if len(token) != authz.TokenLength {
			t.Fatalf("comprimento do token de %s = %d, esperado %d", kind, len(token), authz.TokenLength)
		}
	}
}

func TestGenerateDependsOnTheSecret(t *testing.T) {
	token := authz.NewTokenGenerator("segredo-de-teste").Generate(authz.KindReceipt, testReceiptID)
	if token != "2a2ad95ad92a133cd1680b92c4c17066" {
		t.Fatalf("token = %q, esperado %q", token, "2a2ad95ad92a133cd1680b92c4c17066")
	}
	if other := authz.NewTokenGenerator("outro-segredo").Generate(authz.KindReceipt, testReceiptID); other == token {
		t.Fatal("segredos diferentes geraram o mesmo token")
	}
}

func TestVerifyAcceptsOnlyTheMatchingDocument(t *testing.T) {
	generator := authz.NewTokenGenerator(legacySecret)
	valid := generator.Generate(authz.KindReceipt, testReceiptID)

	if !generator.Verify(authz.KindReceipt, testReceiptID, valid) {
		t.Fatal("token válido foi recusado")
	}

	cases := map[string]struct {
		kind  authz.Kind
		id    string
		token string
	}{
		"outro tipo de documento": {authz.KindLease, testReceiptID, valid},
		"outro documento":         {authz.KindReceipt, "outro-id", valid},
		"token vazio":             {authz.KindReceipt, testReceiptID, ""},
		"token curto":             {authz.KindReceipt, testReceiptID, valid[:16]},
		"token longo":             {authz.KindReceipt, testReceiptID, valid + "00"},
		"token em maiúsculas":     {authz.KindReceipt, testReceiptID, "26E2BA16FD4168935F107B30810A1E3B"},
		"outro segredo":           {authz.KindReceipt, testReceiptID, authz.NewTokenGenerator("outro-segredo").Generate(authz.KindReceipt, testReceiptID)},
	}
	for name, testCase := range cases {
		if generator.Verify(testCase.kind, testCase.id, testCase.token) {
			t.Fatalf("%s: token aceito indevidamente", name)
		}
	}
}

func TestFormatCode(t *testing.T) {
	generator := authz.NewTokenGenerator(legacySecret)
	code := generator.FormatCode(legacyReceiptToken)
	if code != "AUTH-26E2-BA16-FD41-6893" {
		t.Fatalf("chave = %q, esperado %q", code, "AUTH-26E2-BA16-FD41-6893")
	}
	if withPunctuation := generator.FormatCode("26e2-ba16 fd41:6893"); withPunctuation != code {
		t.Fatalf("chave com pontuação = %q, esperado %q", withPunctuation, code)
	}
}

func TestValidationURL(t *testing.T) {
	generator := authz.NewTokenGenerator(legacySecret)

	receipt := generator.ValidationURL("https://aluxen.evolucsia.com/", authz.KindReceipt, testReceiptID, legacyReceiptToken)
	expectedReceipt := "https://aluxen.evolucsia.com/validar/recibo/" + testReceiptID + "?token=" + legacyReceiptToken
	if receipt != expectedReceipt {
		t.Fatalf("URL do recibo = %q, esperado %q", receipt, expectedReceipt)
	}

	lease := generator.ValidationURL("http://192.168.0.10:3000", authz.KindLease, testReceiptID, legacyLeaseToken)
	expectedLease := "http://192.168.0.10:3000/validar/contrato/" + testReceiptID + "?token=" + legacyLeaseToken
	if lease != expectedLease {
		t.Fatalf("URL do contrato = %q, esperado %q", lease, expectedLease)
	}
}

func TestQRCodeGeneratesValidPNG(t *testing.T) {
	generator := authz.NewTokenGenerator(legacySecret)
	url := generator.ValidationURL("https://aluxen.evolucsia.com", authz.KindReceipt, testReceiptID, legacyReceiptToken)

	image, err := generator.QRCode(url)
	if err != nil {
		t.Fatalf("QRCode devolveu erro: %v", err)
	}
	if !bytes.HasPrefix(image, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}) {
		t.Fatal("QR Code não é um PNG: assinatura ausente")
	}

	decoded, err := png.DecodeConfig(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("PNG do QR Code ilegível: %v", err)
	}
	if decoded.Width != authz.QRSize || decoded.Height != authz.QRSize {
		t.Fatalf("QR Code %dx%d, esperado %dx%d", decoded.Width, decoded.Height, authz.QRSize, authz.QRSize)
	}

	// O mesmo conteúdo produz o mesmo PNG (determinístico) e conteúdos
	// diferentes produzem imagens diferentes.
	repeat, err := authz.QRCode(url)
	if err != nil {
		t.Fatalf("QRCode devolveu erro na repetição: %v", err)
	}
	if !bytes.Equal(image, repeat) {
		t.Fatal("QR Code do mesmo conteúdo saiu diferente")
	}
	other, err := authz.QRCode("https://aluxen.evolucsia.com/validar/recibo/outro?token=" + legacyReceiptToken)
	if err != nil {
		t.Fatalf("QRCode devolveu erro: %v", err)
	}
	if bytes.Equal(image, other) {
		t.Fatal("QR Code de conteúdos diferentes saiu igual")
	}
}

// A porta é declarada em application; o alias `Kind` precisa continuar
// satisfazendo-a com a assinatura `Generate(kind, id string)`.
var _ interface {
	Generate(string, string) string
} = authz.NewTokenGenerator("")
