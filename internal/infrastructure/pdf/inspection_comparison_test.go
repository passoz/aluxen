package pdf_test

import (
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/infrastructure/pdf"
)

// O laudo de comparação é conferido no modelo (blocos): identificação, partes e
// período, resumo quantitativo, tabela de achados, relação das fotos comparadas,
// ressalva, assinaturas e selo de autenticidade.

const testComparisonLeaseID = "019b1c2d-3e4f-7081-92a3-b4c5d6e7f809"

var testComparisonURL = "https://aluxen.evolucsia.com/validar/vistoria/" + testInspectionID + "?token=26e2ba16fd4168935f107b30810a1e3b"

func comparisonInput() pdf.ComparisonDocumentInput {
	return pdf.ComparisonDocumentInput{
		LeaseID:         testComparisonLeaseID,
		TenantName:      "Maria Silva",
		TenantCPF:       "123.456.789-00",
		PropertyName:    "Casa da Praia",
		PropertyAddress: "Rua das Flores, 123, Cabo Frio, RJ",
		StartDate:       "2026-03-01",
		EndDate:         "2026-09-30",
		CheckIn: pdf.ComparisonSideInput{
			Label:       "Vistoria de entrada",
			PerformedAt: "2026-03-01T10:00",
			Condition:   "Boa",
			Photos:      2,
		},
		CheckOut: pdf.ComparisonSideInput{
			Label:       "Vistoria de saída",
			PerformedAt: "2026-09-20T09:00",
			Condition:   "Regular",
			Photos:      1,
		},
		Summary: pdf.ComparisonSummaryInput{Ok: 4, Attention: 2, Damaged: 1, Photos: 3, Confidence: 0.82},
		Findings: []pdf.ComparisonFindingInput{
			{Area: "Fachada", Item: "Pintura", Severity: "Sem alteração", CheckInLabel: "fachada.jpg", CheckOutLabel: "sala.jpg"},
			{Area: "Sala", Item: "Sofá", State: "Mancha no braço", Severity: "Atenção", Note: "mancha escura", CheckInLabel: "fachada.jpg", CheckOutLabel: "sala.jpg"},
			{Area: "Sala", Item: "Rodapé", Severity: "Dano", CheckOutLabel: "sala.jpg"},
		},
		Photos: []pdf.ComparisonPhotoInput{
			{Name: "inspections/aaa-fachada.jpg", Label: "fachada.jpg", Side: "Entrada"},
			{Name: "inspections/bbb-sala.jpg", Label: "sala.jpg", Side: "Entrada"},
			{Name: "inspections/ccc-sala.jpg", Side: "Saída"},
		},
		Disclaimer:    "Ressalva de teste: laudo é subsídio do locador.",
		LandlordName:  "João Souza",
		LandlordCPF:   "987.654.321-00",
		AuthCode:      testAuthCode,
		ValidationURL: testComparisonURL,
		IssuedAt:      testIssuedAt,
	}
}

func TestBuildInspectionComparisonIdentifiesTheDocumentThePartsAndThePeriod(t *testing.T) {
	document, err := pdf.BuildInspectionComparison(comparisonInput())
	if err != nil {
		t.Fatalf("BuildInspectionComparison devolveu erro: %v", err)
	}

	titles := blocksOfKind(document, pdf.BlockTitle)
	if len(titles) != 1 || titles[0].Text != "LAUDO DE COMPARAÇÃO DE VISTORIAS" {
		t.Fatalf("título = %+v", titles)
	}

	text := joinText(document)
	for _, expected := range []string{
		"Laudo 019B1C2D",
		"Locador:",
		"João Souza",
		"CPF do locador:",
		"987.654.321-00",
		"Locatário:",
		"Maria Silva",
		"CPF do locatário:",
		"123.456.789-00",
		"Imóvel:",
		"Casa da Praia",
		"Endereço:",
		"Rua das Flores, 123, Cabo Frio, RJ",
		"Contrato:",
		testComparisonLeaseID,
		"Período do contrato:",
		"01/03/2026 a 30/09/2026",
		"Vistoria de entrada:",
		"01/03/2026 10:00 · Boa · 2 foto(s) comparada(s)",
		"Vistoria de saída:",
		"20/09/2026 09:00 · Regular · 1 foto(s) comparada(s)",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("laudo sem %q\n---\n%s", expected, text)
		}
	}
}

func TestBuildInspectionComparisonShowsTheQuantitativeSummary(t *testing.T) {
	document, err := pdf.BuildInspectionComparison(comparisonInput())
	if err != nil {
		t.Fatalf("BuildInspectionComparison devolveu erro: %v", err)
	}

	metrics := map[string]string{}
	for _, block := range blocksOfKind(document, pdf.BlockMetric) {
		metrics[block.Text] = block.Value
	}
	expected := map[string]string{
		"Sem alteração":        "4",
		"Atenção":              "2",
		"Dano":                 "1",
		"Fotos comparadas":     "3",
		"Confiança da leitura": "82%",
	}
	for label, value := range expected {
		if metrics[label] != value {
			t.Fatalf("métrica %q = %q, esperada %q (métricas: %v)", label, metrics[label], value, metrics)
		}
	}
}

func TestBuildInspectionComparisonListsTheFindingsInTheTable(t *testing.T) {
	document, err := pdf.BuildInspectionComparison(comparisonInput())
	if err != nil {
		t.Fatalf("BuildInspectionComparison devolveu erro: %v", err)
	}

	tables := blocksOfKind(document, pdf.BlockTable)
	if len(tables) != 1 {
		t.Fatalf("tabelas = %d, esperado 1", len(tables))
	}
	table := tables[0]
	expectedColumns := []string{"Ambiente", "Item", "Entrada", "Saída", "Severidade", "Observação"}
	for index, column := range expectedColumns {
		if index >= len(table.Columns) || table.Columns[index] != column {
			t.Fatalf("colunas = %v, esperado %v", table.Columns, expectedColumns)
		}
	}
	total := 0.0
	for _, width := range table.ColumnWidths {
		total += width
	}
	if total < 0.99 || total > 1.01 {
		t.Fatalf("larguras não somam 1: %v", table.ColumnWidths)
	}

	expectedRows := [][]string{
		{"Fachada", "Pintura", "fachada.jpg", "sala.jpg", "Sem alteração", ""},
		{"Sala", "Sofá", "fachada.jpg", "sala.jpg", "Atenção", "Mancha no braço · mancha escura"},
		{"Sala", "Rodapé", "", "sala.jpg", "Dano", ""},
	}
	if len(table.Rows) != len(expectedRows) {
		t.Fatalf("linhas = %d, esperado %d", len(table.Rows), len(expectedRows))
	}
	for index, expected := range expectedRows {
		row := table.Rows[index]
		if len(row) != len(expected) {
			t.Fatalf("linha %d = %v, esperada %v", index, row, expected)
		}
		for cell := range expected {
			if row[cell] != expected[cell] {
				t.Fatalf("linha %d, célula %d = %q, esperada %q", index, cell, row[cell], expected[cell])
			}
		}
	}
}

func TestBuildInspectionComparisonListsTheComparedPhotos(t *testing.T) {
	document, err := pdf.BuildInspectionComparison(comparisonInput())
	if err != nil {
		t.Fatalf("BuildInspectionComparison devolveu erro: %v", err)
	}
	text := joinText(document)

	if !strings.Contains(text, "Fotos comparadas") {
		t.Fatalf("laudo sem a seção das fotos\n---\n%s", text)
	}
	if !strings.Contains(text, "Foto 1:") || !strings.Contains(text, "fachada.jpg · arquivo: inspections/aaa-fachada.jpg · vistoria de entrada") {
		t.Fatalf("primeira foto não foi descrita\n---\n%s", text)
	}
	// Foto sem rótulo: o laudo cita o arquivo guardado e a vistoria.
	if !strings.Contains(text, "Foto 3:") || !strings.Contains(text, "arquivo: inspections/ccc-sala.jpg · vistoria de saída") {
		t.Fatalf("terceira foto não foi descrita\n---\n%s", text)
	}
}

func TestBuildInspectionComparisonCarriesTheDisclaimerTheSignaturesAndTheSeal(t *testing.T) {
	document, err := pdf.BuildInspectionComparison(comparisonInput())
	if err != nil {
		t.Fatalf("BuildInspectionComparison devolveu erro: %v", err)
	}

	text := joinText(document)
	if !strings.Contains(text, "Ressalva de teste: laudo é subsídio do locador.") {
		t.Fatalf("laudo sem a ressalva informada\n---\n%s", text)
	}
	// A chamada padrão continua impressa no fecho do documento.
	if !strings.Contains(text, "Cabo Frio, 21 de setembro de 2026") {
		t.Fatalf("laudo sem a data de emissão\n---\n%s", text)
	}

	signatures := blocksOfKind(document, pdf.BlockSignature)
	if len(signatures) != 2 || signatures[0].Text != "João Souza" || signatures[1].Text != "Maria Silva" {
		t.Fatalf("assinaturas = %+v", signatures)
	}
	notes := blocksOfKind(document, pdf.BlockSignatureNote)
	if len(notes) != 2 || notes[0].Text != "Locador" || notes[1].Text != "Locatário" {
		t.Fatalf("rótulos das assinaturas = %+v", notes)
	}

	seals := blocksOfKind(document, pdf.BlockAuth)
	if len(seals) != 1 {
		t.Fatalf("blocos de autenticidade = %d, esperado 1", len(seals))
	}
	if seals[0].QR != testComparisonURL || seals[0].Code != testAuthCode {
		t.Fatalf("selo = %+v", seals[0])
	}
	if seals[0].Text != "Validação de Autenticidade Digital" {
		t.Fatalf("título do selo = %q", seals[0].Text)
	}
}

func TestBuildInspectionComparisonDefaultsTheDisclaimerAndTheEmptyCase(t *testing.T) {
	input := comparisonInput()
	input.Disclaimer = "   "
	input.Findings = nil
	input.Photos = nil
	input.CheckIn.Photos = 0
	input.CheckOut.Photos = 0

	document, err := pdf.BuildInspectionComparison(input)
	if err != nil {
		t.Fatalf("BuildInspectionComparison devolveu erro: %v", err)
	}
	if tables := blocksOfKind(document, pdf.BlockTable); len(tables) != 0 {
		t.Fatalf("laudo sem achados montou %d tabelas", len(tables))
	}
	text := joinText(document)
	for _, expected := range []string{
		"A comparação não apontou nenhum achado entre as fotos analisadas.",
		"Não há fotos comparadas neste contrato.",
		"subsídio do locador",
		"não define valor de dano nem retenção de caução",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("laudo sem %q\n---\n%s", expected, text)
		}
	}
}

func TestRenderInspectionComparisonProducesAPDF(t *testing.T) {
	input := comparisonInput()
	input.LogoPNG = logoPNG(t)

	document, err := pdf.BuildInspectionComparison(input)
	if err != nil {
		t.Fatalf("BuildInspectionComparison devolveu erro: %v", err)
	}
	rendered, err := pdf.Render(document)
	if err != nil {
		t.Fatalf("Render devolveu erro: %v", err)
	}
	assertPDF(t, rendered)
}
