// Package authz emite e confere a chave de autenticidade dos documentos do
// Aluxen e desenha o QR Code que aponta para a página pública de validação.
//
// Um documento é autenticado pelo token `HMAC-SHA256(segredo, "<kind>:<id>")`
// em hexadecimal truncado nos 32 primeiros caracteres — exatamente o cálculo do
// app Bun, de modo que recibos e contratos já impressos continuam válidos. Os
// documentos da temporada (auto de vistoria e recibo de devolução de caução)
// usam o mesmo cálculo, com o seu próprio `kind`.
//
// O segredo é sempre injetado pelo composition root (config `SESSION_SECRET`),
// a mesma chave usada nas sessões e no CSRF. O app anterior tinha um segredo
// padrão embutido no código (`aluxen-authenticity-secret-key-2026`), usado
// quando a variável de ambiente não existia: toda instalação compartilhava a
// mesma chave, o que permitia forjar a chave AUTH de qualquer documento. Aqui
// não há fallback — sem segredo não há token.
package authz

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/passoz/aluxen/internal/application"
)

// Kind identifica o tipo do documento assinado.
//
// É um alias de `string` de propósito: a porta `application.TokenSigner`
// declara `Generate(kind, id string)` e o alias faz os métodos do gerador
// satisfazerem essa porta sem conversão em nenhum ponto do caminho.
type Kind = string

const (
	// KindReceipt assina recibos de pagamento.
	KindReceipt Kind = "receipt"
	// KindLease assina contratos de locação.
	KindLease Kind = "lease"
	// KindInspection assina os autos de vistoria de entrada e de saída.
	KindInspection Kind = "inspection"
	// KindDeposit assina os recibos de devolução de caução da temporada.
	KindDeposit Kind = "deposit"
)

// validationPaths é o caminho público de validação de cada tipo de documento:
// `<base><caminho><id>?token=<token>`. Tipo desconhecido cai no recibo, o
// documento padrão do app.
var validationPaths = map[Kind]string{
	KindReceipt:    "/validar/recibo/",
	KindLease:      "/validar/contrato/",
	KindInspection: "/validar/vistoria/",
	KindDeposit:    "/validar/caucao/",
}

// TokenLength é o comprimento do token: os 32 primeiros caracteres
// hexadecimais do HMAC-SHA256 (128 bits), como no app atual.
const TokenLength = 32

// QRSize é o lado do QR Code em pixels (mesmo valor do app atual).
const QRSize = 140

// TokenGenerator emite e confere as chaves de autenticidade da instância.
type TokenGenerator struct {
	secret []byte
}

// Verifier é a parte do gerador que a borda HTTP usa além da porta de emissão
// (`application.TokenSigner`): conferir um token recebido e desenhar o QR Code.
type Verifier interface {
	Verify(kind, id, token string) bool
	QRCode(content string) ([]byte, error)
}

// O gerador é a implementação concreta da porta de emissão dos casos de uso.
var _ application.TokenSigner = (*TokenGenerator)(nil)

// O gerador também atende à borda HTTP (verificação e QR).
var _ Verifier = (*TokenGenerator)(nil)

// NewTokenGenerator cria o gerador com o segredo da instância
// (`SESSION_SECRET`). Não existe valor padrão: um segredo vazio produziria
// tokens previsíveis, então o composition root sempre passa a chave real.
func NewTokenGenerator(secret string) *TokenGenerator {
	return &TokenGenerator{secret: []byte(secret)}
}

// Generate devolve o token de autenticidade do documento `<kind>:<id>`.
func (g *TokenGenerator) Generate(kind Kind, id string) string {
	mac := hmac.New(sha256.New, g.secret)
	mac.Write([]byte(kind + ":" + id))
	sum := mac.Sum(nil)

	// Equivalente a `hex.EncodeToString(sum)[:32]` — codifica apenas os 16 bytes
	// necessários, sem materializar os 64 caracteres intermediários.
	encoded := make([]byte, TokenLength)
	hex.Encode(encoded, sum[:TokenLength/2])
	return string(encoded)
}

// Verify confere o token em tempo constante. Token de tamanho diferente do
// esperado é recusado de imediato (o compare de tempo constante exigiria o
// mesmo comprimento) e token de outro tipo ou de outro documento não confere.
func (g *TokenGenerator) Verify(kind Kind, id, token string) bool {
	expected := g.Generate(kind, id)
	if len(token) != len(expected) {
		return false
	}
	return hmac.Equal([]byte(token), []byte(expected))
}

// FormatCode devolve a chave legível impressa no documento
// (`AUTH-XXXX-XXXX-XXXX-XXXX`), formada pelos 16 primeiros caracteres
// alfanuméricos do token em maiúsculas.
func (g *TokenGenerator) FormatCode(token string) string {
	clean := alphanumeric(token)
	part := func(from, to int) string {
		if from >= len(clean) {
			return ""
		}
		if to > len(clean) {
			to = len(clean)
		}
		return clean[from:to]
	}
	return "AUTH-" + part(0, 4) + "-" + part(4, 8) + "-" + part(8, 12) + "-" + part(12, 16)
}

// ValidationURL monta o endereço público de validação do documento:
// `<base>/validar/recibo/<id>?token=<token>` (ou `contrato`, `vistoria`,
// `caucao`, conforme o tipo).
func (g *TokenGenerator) ValidationURL(base string, kind Kind, id, token string) string {
	return strings.TrimSuffix(base, "/") + validationPath(kind) + id + "?token=" + token
}

// QRCode desenha o conteúdo em PNG com correção de erro média — os mesmos
// parâmetros de imagem do app atual (140 pixels de lado). O codificador aplica
// a zona de silêncio padrão de 4 módulos (o app usava 1 módulo), o que melhora
// a leitura na impressão.
func (g *TokenGenerator) QRCode(content string) ([]byte, error) {
	return QRCode(content)
}

// QRCode desenha `content` em PNG com correção de erro média. É a única
// implementação de QR do port: o renderizador de PDF e a borda HTTP usam esta
// função, para que o padrão do selo não divirja entre documentos.
func QRCode(content string) ([]byte, error) {
	code, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return nil, fmt.Errorf("montar QR Code: %w", err)
	}
	png, err := code.PNG(QRSize)
	if err != nil {
		return nil, fmt.Errorf("gerar PNG do QR Code: %w", err)
	}
	return png, nil
}

// validationPath devolve o caminho público de validação do tipo de documento.
func validationPath(kind Kind) string {
	if path, ok := validationPaths[kind]; ok {
		return path
	}
	return validationPaths[KindReceipt]
}

// alphanumeric devolve o token em maiúsculas sem pontuação, como o app atual
// (`token.toUpperCase().replace(/[^A-Z0-9]/g, ”)`).
func alphanumeric(token string) string {
	var b strings.Builder
	b.Grow(len(token))
	for _, r := range strings.ToUpper(token) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
