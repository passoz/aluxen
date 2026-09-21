package pdf_test

import (
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/infrastructure/pdf"
)

// O auto de vistoria é conferido no modelo (blocos): título por tipo, partes,
// tabela de itens na ordem conferida, relação das fotos e selo de autenticidade.

const testInspectionID = "018f2c1e-6b3d-7c4a-9e21-5f6a7b8c9d0e"

var testInspectionURL = "https://aluxen.evolucsia.com/validar/vistoria/" + testInspectionID + "?token=26e2ba16fd4168935f107b30810a1e3b"

func inspectionInput() pdf.InspectionInput {
	return pdf.InspectionInput{
		InspectionID:    testInspectionID,
		Kind:            "checkin",
		LeaseID:         "019a3b4c-5d6e-7f80-91a2-b3c4d5e6f708",
		TenantName:      "Maria Silva",
		TenantCPF:       "123.456.789-00",
		PropertyName:    "Casa da Praia",
		PropertyAddress: "Rua das Flores, 123, Cabo Frio, RJ",
		StartDate:       "2026-03-01",
		EndDate:         "2027-02-28",
		PerformedAt:     "2026-09-21T09:30",
		Condition:       "boa",
		Notes:           "Chaves entregues em duas vias.",
		Items: []pdf.InspectionItemInput{
			{Area: "Sala", Item: "Sofá", State: "Bom", Notes: "Pequeno desgaste no braço"},
			{Area: "Cozinha", Item: "Geladeira", State: "Ótimo"},
			{Area: "Quarto", Item: "Guarda-roupa", State: "Regular", Notes: "Porta desalinhada"},
		},
		Photos: []pdf.InspectionPhotoInput{
			{Name: "018f-fachada.jpg", Label: "Fachada", Caption: "Vista da rua"},
			{Name: "018f-sala.jpg"},
		},
		LandlordName:  "João Souza",
		LandlordCPF:   "987.654.321-00",
		AuthCode:      testAuthCode,
		ValidationURL: testInspectionURL,
		IssuedAt:      testIssuedAt,
	}
}

func TestBuildInspectionTitlesByKind(t *testing.T) {
	cases := map[string]string{
		"checkin":  "AUTO DE VISTORIA DE ENTRADA",
		"checkout": "AUTO DE VISTORIA DE SAÍDA",
	}
	for kind, expected := range cases {
		input := inspectionInput()
		input.Kind = kind

		document, err := pdf.BuildInspection(input)
		if err != nil {
			t.Fatalf("%s: BuildInspection devolveu erro: %v", kind, err)
		}
		titles := blocksOfKind(document, pdf.BlockTitle)
		if len(titles) != 1 || titles[0].Text != expected {
			t.Fatalf("%s: título = %+v, esperado %q", kind, titles, expected)
		}
	}
}

func TestBuildInspectionIdentifiesThePartsThePropertyAndTheVisit(t *testing.T) {
	document, err := pdf.BuildInspection(inspectionInput())
	if err != nil {
		t.Fatalf("BuildInspection devolveu erro: %v", err)
	}
	text := joinText(document)

	for _, expected := range []string{
		"Auto 018F2C1E",
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
		"019a3b4c-5d6e-7f80-91a2-b3c4d5e6f708",
		"Período do contrato:",
		"01/03/2026 a 28/02/2027",
		"Data e hora:",
		"21/09/2026 09:30",
		"Estado geral:",
		"Boa",
		"Observações gerais",
		"Chaves entregues em duas vias.",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("auto sem %q\n---\n%s", expected, text)
		}
	}
}

func TestBuildInspectionListsTheItemsInOrder(t *testing.T) {
	document, err := pdf.BuildInspection(inspectionInput())
	if err != nil {
		t.Fatalf("BuildInspection devolveu erro: %v", err)
	}

	tables := blocksOfKind(document, pdf.BlockTable)
	if len(tables) != 1 {
		t.Fatalf("tabelas = %d, esperado 1", len(tables))
	}
	table := tables[0]
	expectedColumns := []string{"Ambiente", "Item", "Estado", "Observação"}
	if len(table.Columns) != len(expectedColumns) {
		t.Fatalf("colunas = %v, esperado %v", table.Columns, expectedColumns)
	}
	for index, column := range expectedColumns {
		if table.Columns[index] != column {
			t.Fatalf("coluna %d = %q, esperada %q", index, table.Columns[index], column)
		}
	}

	expectedRows := [][]string{
		{"Sala", "Sofá", "Bom", "Pequeno desgaste no braço"},
		{"Cozinha", "Geladeira", "Ótimo", ""},
		{"Quarto", "Guarda-roupa", "Regular", "Porta desalinhada"},
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

	// A tabela ainda carrega o cabeçalho e as frações de largura das colunas.
	if len(table.ColumnWidths) != len(expectedColumns) {
		t.Fatalf("larguras = %v", table.ColumnWidths)
	}
	total := 0.0
	for _, width := range table.ColumnWidths {
		total += width
	}
	if total < 0.99 || total > 1.01 {
		t.Fatalf("larguras não somam 1: %v", table.ColumnWidths)
	}
}

func TestBuildInspectionListsTheAttachedPhotos(t *testing.T) {
	document, err := pdf.BuildInspection(inspectionInput())
	if err != nil {
		t.Fatalf("BuildInspection devolveu erro: %v", err)
	}
	text := joinText(document)

	if !strings.Contains(text, "Fotos anexadas") {
		t.Fatalf("auto sem a seção das fotos\n---\n%s", text)
	}
	if !strings.Contains(text, "Foto 1:") || !strings.Contains(text, "Fachada · Vista da rua · arquivo: 018f-fachada.jpg") {
		t.Fatalf("primeira foto não foi descrita\n---\n%s", text)
	}
	// Foto sem rótulo nem legenda: o PDF cita apenas o arquivo guardado.
	if !strings.Contains(text, "Foto 2:") || !strings.Contains(text, "arquivo: 018f-sala.jpg") {
		t.Fatalf("segunda foto não foi descrita\n---\n%s", text)
	}
}

func TestBuildInspectionDescribesTheEmptyCase(t *testing.T) {
	input := inspectionInput()
	input.Items = nil
	input.Photos = nil
	input.Notes = "   "

	document, err := pdf.BuildInspection(input)
	if err != nil {
		t.Fatalf("BuildInspection devolveu erro: %v", err)
	}
	if tables := blocksOfKind(document, pdf.BlockTable); len(tables) != 0 {
		t.Fatalf("vistoria sem itens montou %d tabelas", len(tables))
	}
	text := joinText(document)
	for _, expected := range []string{
		"Não há itens conferidos registrados nesta vistoria.",
		"Não há fotos anexadas a esta vistoria.",
		"Sem observações registradas.",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("auto sem %q\n---\n%s", expected, text)
		}
	}
}

func TestBuildInspectionCarriesTheAuthenticitySeal(t *testing.T) {
	document, err := pdf.BuildInspection(inspectionInput())
	if err != nil {
		t.Fatalf("BuildInspection devolveu erro: %v", err)
	}

	seals := blocksOfKind(document, pdf.BlockAuth)
	if len(seals) != 1 {
		t.Fatalf("blocos de autenticidade = %d, esperado 1", len(seals))
	}
	seal := seals[0]
	if seal.QR != testInspectionURL {
		t.Fatalf("URL do QR = %q, esperada %q", seal.QR, testInspectionURL)
	}
	if seal.Code != testAuthCode {
		t.Fatalf("chave AUTH = %q, esperada %q", seal.Code, testAuthCode)
	}
	if seal.Text != "Validação de Autenticidade Digital" {
		t.Fatalf("título do selo = %q", seal.Text)
	}
}

func TestBuildInspectionSignsTheLandlordAndTheTenant(t *testing.T) {
	document, err := pdf.BuildInspection(inspectionInput())
	if err != nil {
		t.Fatalf("BuildInspection devolveu erro: %v", err)
	}

	signatures := blocksOfKind(document, pdf.BlockSignature)
	if len(signatures) != 2 {
		t.Fatalf("assinaturas = %d, esperado 2", len(signatures))
	}
	if signatures[0].Text != "João Souza" || signatures[0].Align != pdf.AlignLeft {
		t.Fatalf("assinatura do locador = %+v", signatures[0])
	}
	if signatures[1].Text != "Maria Silva" || signatures[1].Align != pdf.AlignRight {
		t.Fatalf("assinatura do locatário = %+v", signatures[1])
	}

	notes := blocksOfKind(document, pdf.BlockSignatureNote)
	if len(notes) != 2 || notes[0].Text != "Locador" || notes[1].Text != "Locatário" {
		t.Fatalf("rótulos das assinaturas = %+v", notes)
	}
}

func TestRenderInspectionProducesAPDF(t *testing.T) {
	input := inspectionInput()
	input.LogoPNG = logoPNG(t)

	document, err := pdf.BuildInspection(input)
	if err != nil {
		t.Fatalf("BuildInspection devolveu erro: %v", err)
	}
	rendered, err := pdf.Render(document)
	if err != nil {
		t.Fatalf("Render devolveu erro: %v", err)
	}
	assertPDF(t, rendered)
}
