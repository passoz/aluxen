package pdf_test

import (
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/infrastructure/pdf"
)

// O recibo de devolução de caução é conferido no modelo (blocos): título,
// partes, valores (devolvido por extenso e retido quando houver), situação,
// declaração e selo de autenticidade.

var testDepositURL = "https://aluxen.evolucsia.com/validar/caucao/" + testReceiptID + "?token=26e2ba16fd4168935f107b30810a1e3b"

func depositInput() pdf.DepositReceiptInput {
	return pdf.DepositReceiptInput{
		LeaseID:         testReceiptID,
		TenantName:      "Maria Silva",
		TenantCPF:       "123.456.789-00",
		PropertyName:    "Casa da Praia",
		PropertyAddress: "Rua das Flores, 123, Cabo Frio, RJ",
		LandlordName:    "João Souza",
		LandlordCPF:     "987.654.321-00",
		Amount:          1200,
		Refunded:        1200,
		Retained:        0,
		Status:          "refunded",
		ReceivedAt:      "2026-03-01T10:00",
		RefundedAt:      "2026-09-21T15:04",
		AuthCode:        testAuthCode,
		ValidationURL:   testDepositURL,
		IssuedAt:        testIssuedAt,
	}
}

func TestBuildDepositReceiptIdentifiesTheRefund(t *testing.T) {
	document, err := pdf.BuildDepositReceipt(depositInput())
	if err != nil {
		t.Fatalf("BuildDepositReceipt devolveu erro: %v", err)
	}

	titles := blocksOfKind(document, pdf.BlockTitle)
	if len(titles) != 1 || titles[0].Text != "RECIBO DE DEVOLUÇÃO DE CAUÇÃO" {
		t.Fatalf("título = %+v", titles)
	}

	text := joinText(document)
	for _, expected := range []string{
		"Recibo 018F2C1E",
		"Locador",
		"João Souza",
		"987.654.321-00",
		"Locatário",
		"Maria Silva",
		"123.456.789-00",
		"Casa da Praia",
		"Rua das Flores, 123, Cabo Frio, RJ",
		"Contrato:",
		testReceiptID,
		"Valor da Caução",
		"R$ 1.200,00",
		"Valor Devolvido",
		"Situação",
		"Devolvida",
		"Recebido em:",
		"01/03/2026 10:00",
		"Devolvido em:",
		"21/09/2026 15:04",
		"Cabo Frio, 21 de setembro de 2026",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("recibo sem %q\n---\n%s", expected, text)
		}
	}
	// Devolução integral não imprime valor retido.
	if strings.Contains(text, "Valor Retido") {
		t.Fatalf("devolução integral anunciou valor retido\n---\n%s", text)
	}
}

func TestBuildDepositReceiptWritesTheRefundInWords(t *testing.T) {
	document, err := pdf.BuildDepositReceipt(depositInput())
	if err != nil {
		t.Fatalf("BuildDepositReceipt devolveu erro: %v", err)
	}

	declaration := "Declaro, para os devidos fins, a devolução integral da caução no valor de R$ 1.200,00 (mil e duzentos reais) referente ao imóvel Casa da Praia na Rua das Flores, 123, Cabo Frio, RJ, vinculado ao locatário Maria Silva, nada restando retido."
	if text := joinText(document); !strings.Contains(text, declaration) {
		t.Fatalf("declaração da devolução integral ausente\n---\n%s", text)
	}
}

func TestBuildDepositReceiptDescribesThePartialRefund(t *testing.T) {
	input := depositInput()
	input.Refunded = 500
	input.Retained = 700
	input.Status = "partially_refunded"

	document, err := pdf.BuildDepositReceipt(input)
	if err != nil {
		t.Fatalf("BuildDepositReceipt devolveu erro: %v", err)
	}
	text := joinText(document)

	for _, expected := range []string{
		"Valor Devolvido",
		"R$ 500,00",
		"Valor Retido",
		"R$ 700,00",
		"Devolvida parcialmente",
		"Declaro, para os devidos fins, a devolução parcial da caução no valor de R$ 500,00 (quinhentos reais) de um total de R$ 1.200,00 (mil e duzentos reais) referente ao imóvel Casa da Praia na Rua das Flores, 123, Cabo Frio, RJ, vinculado ao locatário Maria Silva, ficando retido o valor de R$ 700,00 (setecentos reais).",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("recibo parcial sem %q\n---\n%s", expected, text)
		}
	}
}

func TestBuildDepositReceiptDescribesTheFullRetention(t *testing.T) {
	input := depositInput()
	input.Refunded = 0
	input.Retained = 1200
	input.Status = "forfeited"

	document, err := pdf.BuildDepositReceipt(input)
	if err != nil {
		t.Fatalf("BuildDepositReceipt devolveu erro: %v", err)
	}
	text := joinText(document)

	for _, expected := range []string{
		"Valor Retido",
		"R$ 1.200,00",
		"Retida integralmente",
		"Declaro, para os devidos fins, a retenção integral da caução no valor de R$ 1.200,00 (mil e duzentos reais) referente ao imóvel Casa da Praia na Rua das Flores, 123, Cabo Frio, RJ, vinculado ao locatário Maria Silva, nada sendo devolvido ao LOCATÁRIO.",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("recibo com retenção total sem %q\n---\n%s", expected, text)
		}
	}
	if strings.Contains(text, "Valor Devolvido") {
		t.Fatalf("retenção integral anunciou valor devolvido\n---\n%s", text)
	}
}

func TestBuildDepositReceiptDescribesTheDepositStillHeld(t *testing.T) {
	input := depositInput()
	input.Refunded = 0
	input.Retained = 1200
	input.Status = "held"
	input.RefundedAt = ""

	document, err := pdf.BuildDepositReceipt(input)
	if err != nil {
		t.Fatalf("BuildDepositReceipt devolveu erro: %v", err)
	}
	text := joinText(document)

	for _, expected := range []string{
		"Retida",
		"Declaro, para os devidos fins, que o valor de R$ 1.200,00 (mil e duzentos reais) referente à caução do imóvel Casa da Praia na Rua das Flores, 123, Cabo Frio, RJ, vinculado ao locatário Maria Silva, permanece retido pelo LOCADOR, não havendo, até a presente data, devolução a registrar.",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("recibo sem %q\n---\n%s", expected, text)
		}
	}
	// Sem devolução registrada não há data de devolução a imprimir.
	if strings.Contains(text, "Devolvido em:") {
		t.Fatalf("recibo sem devolução imprimiu a data\n---\n%s", text)
	}
}

func TestBuildDepositReceiptCarriesTheAuthenticitySeal(t *testing.T) {
	document, err := pdf.BuildDepositReceipt(depositInput())
	if err != nil {
		t.Fatalf("BuildDepositReceipt devolveu erro: %v", err)
	}

	seals := blocksOfKind(document, pdf.BlockAuth)
	if len(seals) != 1 {
		t.Fatalf("blocos de autenticidade = %d, esperado 1", len(seals))
	}
	seal := seals[0]
	if seal.QR != testDepositURL {
		t.Fatalf("URL do QR = %q, esperada %q", seal.QR, testDepositURL)
	}
	if seal.Code != testAuthCode {
		t.Fatalf("chave AUTH = %q, esperada %q", seal.Code, testAuthCode)
	}
	if !strings.Contains(seal.Value, "Aponte a câmera do celular") {
		t.Fatalf("legenda do selo = %q", seal.Value)
	}

	signatures := blocksOfKind(document, pdf.BlockSignature)
	if len(signatures) != 1 || signatures[0].Text != "João Souza" || signatures[0].Align != pdf.AlignCenter {
		t.Fatalf("assinatura do locador = %+v", signatures)
	}
}

func TestRenderDepositReceiptProducesAPDF(t *testing.T) {
	document, err := pdf.BuildDepositReceipt(depositInput())
	if err != nil {
		t.Fatalf("BuildDepositReceipt devolveu erro: %v", err)
	}
	rendered, err := pdf.Render(document)
	if err != nil {
		t.Fatalf("Render devolveu erro: %v", err)
	}
	assertPDF(t, rendered)
}

// O renderizador recusa uma tabela malformada em vez de desenhar lixo.
func TestRenderRejectsMalformedTable(t *testing.T) {
	document := pdf.Document{Pages: []pdf.Page{{Blocks: []pdf.Block{
		{Kind: pdf.BlockTable, Columns: []string{"A", "B"}, Rows: [][]string{{"só uma célula"}}},
	}}}}
	if _, err := pdf.Render(document); err == nil {
		t.Fatal("tabela com linha incompleta foi aceita")
	}

	document = pdf.Document{Pages: []pdf.Page{{Blocks: []pdf.Block{{Kind: pdf.BlockTable}}}}}
	if _, err := pdf.Render(document); err == nil {
		t.Fatal("tabela sem colunas foi aceita")
	}
}
