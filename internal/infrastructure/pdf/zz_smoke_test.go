package pdf_test

import (
	"os"
	"testing"

	"github.com/passoz/aluxen/internal/infrastructure/pdf"
)

// Rascunho de verificação: despeja os dois documentos da temporada em /tmp para
// conferência do texto extraído pelo pdftotext. Não faz parte da bateria.
func TestSmokeWriteSeasonDocuments(t *testing.T) {
	for name, document := range map[string]func() (pdf.Document, error){
		"/tmp/vistoria.pdf": func() (pdf.Document, error) {
			input := inspectionInput()
			input.LogoPNG = logoPNG(t)
			return pdf.BuildInspection(input)
		},
		"/tmp/caucao.pdf": func() (pdf.Document, error) {
			input := depositInput()
			input.Refunded = 500
			input.Retained = 700
			input.Status = "partially_refunded"
			input.LogoPNG = logoPNG(t)
			return pdf.BuildDepositReceipt(input)
		},
	} {
		document, err := document()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		rendered, err := pdf.Render(document)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := os.WriteFile(name, rendered, 0o600); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
