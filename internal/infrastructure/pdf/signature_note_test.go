package pdf_test

import (
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/infrastructure/pdf"
)

// A promessa do produto é "assinatura visualizada" (FR-304): o recibo e o
// contrato precisam declarar isso no rodapé do bloco de assinatura, e nenhum
// bloco pode prometer "assinatura digital" — que soaria como assinatura
// eletrônica com validade jurídica nos termos da ICP-Brasil.

// assertNoDigitalSignaturePromise reprova qualquer bloco que prometa
// "assinatura digital" (a busca ignora maiúsculas/minúsculas).
func assertNoDigitalSignaturePromise(t *testing.T, document pdf.Document) {
	t.Helper()
	for _, block := range blocksOf(document) {
		text := strings.ToLower(block.Text + " " + block.Value + " " + block.Code)
		if strings.Contains(text, "assinatura digital") {
			t.Fatalf("bloco promete assinatura digital: %q", block.Text)
		}
	}
}

// lastIndex devolve a posição do último bloco do tipo pedido (-1 se não houver).
func lastIndex(document pdf.Document, kind pdf.BlockKind) int {
	position := -1
	for index, block := range blocksOf(document) {
		if block.Kind == kind {
			position = index
		}
	}
	return position
}

// indexOfNote devolve a posição da nota de assinatura visualizada.
func indexOfNote(t *testing.T, document pdf.Document) (int, string) {
	t.Helper()
	for index, block := range blocksOf(document) {
		if strings.Contains(block.Text, "Assinatura visualizada") {
			return index, block.Text
		}
	}
	t.Fatalf("documento sem a nota de assinatura visualizada")
	return -1, ""
}

func TestReceiptCarriesTheVisualizedSignatureNote(t *testing.T) {
	for name, signature := range map[string]bool{"com imagem": true, "sem imagem": false} {
		input := paidRentReceipt()
		if signature {
			input.Signature = true
			input.SignaturePNG = signaturePNG(t)
		}
		receipt, err := pdf.BuildReceipt(input)
		if err != nil {
			t.Fatalf("%s: BuildReceipt devolveu erro: %v", name, err)
		}

		noteIndex, note := indexOfNote(t, receipt)
		for _, expected := range []string{"Assinatura visualizada", "imagem impressa", "ICP-Brasil"} {
			if !strings.Contains(note, expected) {
				t.Fatalf("%s: nota de assinatura sem %q: %q", name, expected, note)
			}
		}
		if signatureIndex := lastIndex(receipt, pdf.BlockSignature); noteIndex < signatureIndex {
			t.Fatalf("%s: nota de assinatura (%d) antes da linha de assinatura (%d)", name, noteIndex, signatureIndex)
		}
		assertNoDigitalSignaturePromise(t, receipt)
	}
}

func TestContractCarriesTheVisualizedSignatureNote(t *testing.T) {
	contract, err := pdf.BuildContract(residentialContract())
	if err != nil {
		t.Fatalf("BuildContract devolveu erro: %v", err)
	}

	noteIndex, note := indexOfNote(t, contract)
	for _, expected := range []string{"Assinatura visualizada", "manuscritas", "ICP-Brasil"} {
		if !strings.Contains(note, expected) {
			t.Fatalf("nota de assinatura sem %q: %q", expected, note)
		}
	}
	if signatureIndex := lastIndex(contract, pdf.BlockSignature); noteIndex < signatureIndex {
		t.Fatalf("nota de assinatura (%d) antes da última linha de assinatura (%d)", noteIndex, signatureIndex)
	}
	assertNoDigitalSignaturePromise(t, contract)
}
