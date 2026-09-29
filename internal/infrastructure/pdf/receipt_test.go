package pdf_test

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/infrastructure/pdf"
)

// Os testes deste pacote olham o modelo (blocos), não o arquivo: o conteúdo
// jurídico — valor por extenso, cláusulas, chave de autenticidade — é conferido
// sem abrir PDF. O desenho é conferido nos testes de `Render`.

const (
	testReceiptID = "018f2c1e-6b3d-7c4a-9e21-5f6a7b8c9d0e"
	testAuthCode  = "AUTH-26E2-BA16-FD41-6893"
)

var testValidationURL = "https://aluxen.evolucsia.com/validar/recibo/" + testReceiptID + "?token=26e2ba16fd4168935f107b30810a1e3b"

var testIssuedAt = time.Date(2026, time.September, 21, 15, 4, 5, 0, time.Local)

// blocksOf devolve os blocos do documento de todas as páginas.
func blocksOf(document pdf.Document) []pdf.Block {
	blocks := make([]pdf.Block, 0, 64)
	for _, page := range document.Pages {
		blocks = append(blocks, page.Blocks...)
	}
	return blocks
}

// blocksOfKind devolve os blocos do tipo pedido, em ordem.
func blocksOfKind(document pdf.Document, kind pdf.BlockKind) []pdf.Block {
	blocks := make([]pdf.Block, 0, 8)
	for _, block := range blocksOf(document) {
		if block.Kind == kind {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// joinText concatena o texto e os valores de todos os blocos.
func joinText(document pdf.Document) string {
	var builder strings.Builder
	for _, block := range blocksOf(document) {
		builder.WriteString(block.Text)
		builder.WriteString(" ")
		builder.WriteString(block.Value)
		builder.WriteString(" ")
		builder.WriteString(block.Code)
		builder.WriteString(" ")
		builder.WriteString(block.QR)
		builder.WriteString("\n")
	}
	return builder.String()
}

// findBlock devolve o primeiro bloco com o texto pedido.
func findBlock(document pdf.Document, text string) (pdf.Block, bool) {
	for _, block := range blocksOf(document) {
		if strings.Contains(block.Text, text) || strings.Contains(block.Value, text) {
			return block, true
		}
	}
	return pdf.Block{}, false
}

// signaturePNG lê o asset da assinatura do locador embutido no pacote web.
func signaturePNG(t *testing.T) []byte {
	t.Helper()
	image, err := os.ReadFile("../web/static/assets/assinatura01.png")
	if err != nil {
		t.Fatalf("ler a assinatura: %v", err)
	}
	return image
}

func paidRentReceipt() pdf.ReceiptInput {
	return pdf.ReceiptInput{
		ReceiptID:       testReceiptID,
		TenantName:      "Maria Silva",
		TenantCPF:       "123.456.789-00",
		PropertyName:    "Casa da Praia",
		PropertyAddress: "Rua das Flores, 123, Cabo Frio, RJ",
		Category:        "rent",
		DueDate:         "2026-09-10",
		PaidAt:          "2026-09-09T14:30",
		Amount:          1200.50,
		LandlordName:    "João Souza",
		LandlordCPF:     "987.654.321-00",
		AuthCode:        testAuthCode,
		ValidationURL:   testValidationURL,
		IssuedAt:        testIssuedAt,
	}
}

func TestBuildReceiptKeepsTheTextsAndTheMoneyInWords(t *testing.T) {
	receipt, err := pdf.BuildReceipt(paidRentReceipt())
	if err != nil {
		t.Fatalf("BuildReceipt devolveu erro: %v", err)
	}

	text := joinText(receipt)
	for _, expected := range []string{
		"Recibo de aluguel",
		"Recibo 018F2C1E",
		"Recebido de",
		"Maria Silva",
		"CPF: 123.456.789-00",
		"Imóvel: Casa da Praia",
		"Resumo do recebimento",
		"R$ 1.200,50",
		"10/09/2026",
		"09/09/2026",
		"Declaração",
		"Declaro, para os devidos fins, o recebimento do valor de R$ 1.200,50 (mil e duzentos reais e cinquenta centavos) referente ao aluguel do imóvel Casa da Praia na Rua das Flores, 123, Cabo Frio, RJ, vinculado ao locatário Maria Silva.",
		"Cabo Frio, 21 de setembro de 2026",
		"João Souza",
		"CPF 987.654.321-00",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("recibo sem %q\n---\n%s", expected, text)
		}
	}
	if strings.Contains(text, "Pagamento Parcial") {
		t.Fatal("recibo integral anunciou pagamento parcial")
	}
}
func TestBuildReceiptHasNoBrandLogo(t *testing.T) {
	input := paidRentReceipt()
	input.LogoPNG = logoPNG(t)
	receipt, err := pdf.BuildReceipt(input)
	if err != nil {
		t.Fatalf("BuildReceipt devolveu erro: %v", err)
	}
	brands := blocksOfKind(receipt, pdf.BlockBrand)
	if len(brands) != 0 {
		t.Fatalf("recibo não deve conter bloco de marca/logo, encontrou %d", len(brands))
	}
}

func TestBuildReceiptCarriesTheAuthenticitySeal(t *testing.T) {
	receipt, err := pdf.BuildReceipt(paidRentReceipt())
	if err != nil {
		t.Fatalf("BuildReceipt devolveu erro: %v", err)
	}

	seals := blocksOfKind(receipt, pdf.BlockAuth)
	if len(seals) != 1 {
		t.Fatalf("blocos de autenticidade = %d, esperado 1", len(seals))
	}
	seal := seals[0]
	if seal.QR != testValidationURL {
		t.Fatalf("URL do QR = %q, esperada %q", seal.QR, testValidationURL)
	}
	if seal.Code != testAuthCode {
		t.Fatalf("chave AUTH = %q, esperada %q", seal.Code, testAuthCode)
	}
	if seal.Text != "Validação de Autenticidade Digital" {
		t.Fatalf("título do selo = %q", seal.Text)
	}
	if !strings.Contains(seal.Value, "Aponte a câmera do celular") {
		t.Fatalf("legenda do selo = %q", seal.Value)
	}
}

func TestBuildReceiptShowsTheSignatureOnlyWhenAsked(t *testing.T) {
	withoutSignature, err := pdf.BuildReceipt(paidRentReceipt())
	if err != nil {
		t.Fatalf("BuildReceipt devolveu erro: %v", err)
	}
	if images := blocksOfKind(withoutSignature, pdf.BlockImage); len(images) != 0 {
		t.Fatalf("recibo sem assinatura tem %d imagens", len(images))
	}

	input := paidRentReceipt()
	input.Signature = true
	input.SignaturePNG = signaturePNG(t)
	withSignature, err := pdf.BuildReceipt(input)
	if err != nil {
		t.Fatalf("BuildReceipt devolveu erro: %v", err)
	}
	images := blocksOfKind(withSignature, pdf.BlockImage)
	if len(images) != 1 {
		t.Fatalf("recibo com assinatura tem %d imagens, esperado 1", len(images))
	}
	image := images[0]
	if !bytes.Equal(image.ImagePNG, input.SignaturePNG) {
		t.Fatal("imagem da assinatura não é a informada")
	}
	if image.Align != pdf.AlignCenter {
		t.Fatalf("alinhamento da assinatura = %q", image.Align)
	}
	// O asset tem 826x302: na largura máxima de 63,5 mm a altura fica em 23,2 mm.
	if image.WidthMM < 63 || image.WidthMM > 64 {
		t.Fatalf("largura da assinatura = %.1f mm", image.WidthMM)
	}
	if image.HeightMM < 23 || image.HeightMM > 24 {
		t.Fatalf("altura da assinatura = %.1f mm", image.HeightMM)
	}

	// Assinatura pedida sem imagem não inventa bloco de imagem.
	input.SignaturePNG = nil
	withoutImage, err := pdf.BuildReceipt(input)
	if err != nil {
		t.Fatalf("BuildReceipt devolveu erro: %v", err)
	}
	if images := blocksOfKind(withoutImage, pdf.BlockImage); len(images) != 0 {
		t.Fatalf("recibo com assinatura pedida e sem imagem tem %d imagens", len(images))
	}
}

func TestBuildReceiptDescribesPartialPayment(t *testing.T) {
	input := paidRentReceipt()
	input.PaidAmount = 500
	input.HasPaidAmount = true

	receipt, err := pdf.BuildReceipt(input)
	if err != nil {
		t.Fatalf("BuildReceipt devolveu erro: %v", err)
	}
	text := joinText(receipt)

	for _, expected := range []string{
		"Recibo 018F2C1E • Pagamento Parcial",
		"Valor Total",
		"R$ 1.200,50",
		"Valor Pago",
		"R$ 500,00",
		"Saldo Restante",
		"R$ 700,50",
		"Pago em: 09/09/2026",
		"Declaro, para os devidos fins, o recebimento parcial no valor de R$ 500,00 (quinhentos reais) de um total de R$ 1.200,50 referente ao aluguel do imóvel Casa da Praia na Rua das Flores, 123, Cabo Frio, RJ, vinculado ao locatário Maria Silva, restando o saldo devedor de R$ 700,50.",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("recibo parcial sem %q\n---\n%s", expected, text)
		}
	}
}

func TestBuildReceiptDescribesExtraCharge(t *testing.T) {
	input := paidRentReceipt()
	input.Category = "extra"
	input.Description = "Limpeza"
	input.Amount = 250
	// Lançamento avulso vinculado só ao nome do imóvel: a referência sai com o
	// rótulo "Referência/Imóvel" e a declaração abre parênteses com o nome.
	input.PropertyName = "Casa da Praia"
	input.PropertyAddress = ""

	receipt, err := pdf.BuildReceipt(input)
	if err != nil {
		t.Fatalf("BuildReceipt devolveu erro: %v", err)
	}
	text := joinText(receipt)

	if !strings.Contains(text, "Recibo - Limpeza") {
		t.Fatalf("título do lançamento avulso ausente\n---\n%s", text)
	}
	if !strings.Contains(text, "Declaro, para os devidos fins, o recebimento do valor de R$ 250,00 (duzentos e cinquenta reais) referente a Limpeza, vinculado a Maria Silva (Casa da Praia).") {
		t.Fatalf("declaração do lançamento avulso ausente\n---\n%s", text)
	}
	if !strings.Contains(text, "Referência/Imóvel: ") {
		t.Fatalf("linha de referência do imóvel ausente\n---\n%s", text)
	}
}

func TestBuildReceiptWithoutDescriptionUsesTheGenericTitle(t *testing.T) {
	input := paidRentReceipt()
	input.Category = "extra"
	input.Description = ""
	input.PropertyName = ""
	input.PropertyAddress = ""

	receipt, err := pdf.BuildReceipt(input)
	if err != nil {
		t.Fatalf("BuildReceipt devolveu erro: %v", err)
	}
	if block, ok := findBlock(receipt, "Recibo de Pagamento"); !ok || block.Kind != pdf.BlockTitle {
		t.Fatalf("título genérico do lançamento avulso ausente: %+v", block)
	}
}

func TestRenderReceiptProducesAPDF(t *testing.T) {
	input := paidRentReceipt()
	input.Signature = true
	input.SignaturePNG = signaturePNG(t)
	input.LogoPNG = logoPNG(t)

	receipt, err := pdf.BuildReceipt(input)
	if err != nil {
		t.Fatalf("BuildReceipt devolveu erro: %v", err)
	}
	rendered, err := pdf.Render(receipt)
	if err != nil {
		t.Fatalf("Render devolveu erro: %v", err)
	}
	assertPDF(t, rendered)
}

// logoPNG lê a logomarca embutida no pacote web (usada no cabeçalho).
func logoPNG(t *testing.T) []byte {
	t.Helper()
	image, err := os.ReadFile("../web/static/logomarca.png")
	if err != nil {
		t.Fatalf("ler a logomarca: %v", err)
	}
	return image
}

// assertPDF confere que os bytes são um PDF fechado.
func assertPDF(t *testing.T, rendered []byte) {
	t.Helper()
	if !bytes.HasPrefix(rendered, []byte("%PDF-")) {
		t.Fatalf("documento não começa com %%PDF-: %q", rendered[:min(16, len(rendered))])
	}
	if !bytes.Contains(rendered, []byte("%%EOF")) {
		t.Fatalf("documento sem marcador %s", "%%EOF")
	}
	if !bytes.Contains(rendered, []byte("/Helvetica")) {
		t.Fatal("documento não usa a fonte Helvetica")
	}
}

func TestRenderRejectsUnknownBlock(t *testing.T) {
	document := pdf.Document{Pages: []pdf.Page{{Blocks: []pdf.Block{{Kind: pdf.BlockKind("inventado"), Text: "x"}}}}}
	if _, err := pdf.Render(document); err == nil {
		t.Fatal("bloco desconhecido foi aceito")
	}
}
