package pdf_test

import (
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/infrastructure/pdf"
)

// As 25 cláusulas do contrato, na ordem em que o documento as imprime. São os
// títulos literais do app atual (`src/utils/contract-pdf.ts`) — conferir a lista
// completa garante que nenhuma cláusula foi omitida, resumida ou renumerada.
var contractClauseTitles = []string{
	"Cláusula 1ª - DO OBJETO",
	"Cláusula 2ª - DA DESTINAÇÃO",
	"Cláusula 3ª - DO ESTADO DO IMÓVEL",
	"Cláusula 4ª - DAS BENFEITORIAS",
	"Cláusula 5ª - DA RESTITUIÇÃO",
	"Cláusula 6ª - DA VENDA DO IMÓVEL",
	"Cláusula 7ª - DAS VISTORIAS",
	"Cláusula 8ª - DOS ACIDENTES",
	"Cláusula 9ª - DO ALUGUEL",
	"Cláusula 10ª - DO RECIBO",
	"Cláusula 11ª - DO REAJUSTE",
	"Cláusula 12ª - DA COBRANÇA",
	"Cláusula 13ª - DAS DESPESAS",
	"Cláusula 14ª - DA MORA",
	"Cláusula 15ª - DO ATRASO",
	"Cláusula 16ª - DA TOLERÂNCIA",
	"Cláusula 17ª - DA MULTA CONTRATUAL",
	"Cláusula 18ª - DA RESCISÃO",
	"Cláusula 19ª - DO USO DIVERSO",
	"Cláusula 20ª - DA RESCISÃO PELO LOCATÁRIO",
	"Cláusula 21ª - DO PRAZO",
	"Cláusula 22ª - DA PRORROGAÇÃO",
	"Cláusula 23ª - DO REGISTRO",
	"Cláusula 24ª - DA CAUÇÃO",
	"Cláusula 25ª - DO FORO",
}

var testContractValidationURL = "https://aluxen.evolucsia.com/validar/contrato/" + testReceiptID + "?token=f5056b3371be8a715796f2822ad83c54"

func residentialContract() pdf.ContractInput {
	return pdf.ContractInput{
		LeaseID:         testReceiptID,
		TenantName:      "Maria Silva",
		TenantPhone:     "(22) 99999-0000",
		TenantCPF:       "123.456.789-00",
		PropertyName:    "Casa da Praia",
		PropertyAddress: "Rua das Flores, 123",
		PropertyCity:    "cabo frio",
		PropertyState:   "rj",
		PropertyZip:     "28900-000",
		StartDate:       "2026-03-01",
		EndDate:         "2027-02-28",
		DueDay:          10,
		RentAmount:      1200.50,
		Status:          "active",
		LandlordName:    "João Souza",
		LandlordCPF:     "987.654.321-00",
		LandlordAddress: "Rua do Sol, 45, Cabo Frio - RJ",
		AuthCode:        testAuthCode,
		ValidationURL:   testContractValidationURL,
		IssuedAt:        testIssuedAt,
	}
}

func TestBuildContractHasTheTwentyFiveClausesInOrder(t *testing.T) {
	contract, err := pdf.BuildContract(residentialContract())
	if err != nil {
		t.Fatalf("BuildContract devolveu erro: %v", err)
	}

	sections := make([]string, 0, len(contractClauseTitles))
	for _, block := range blocksOfKind(contract, pdf.BlockSection) {
		if strings.HasPrefix(block.Text, "Cláusula ") {
			sections = append(sections, block.Text)
		}
	}
	if len(sections) != 25 {
		t.Fatalf("cláusulas = %d, esperado 25: %v", len(sections), sections)
	}
	for index, expected := range contractClauseTitles {
		if sections[index] != expected {
			t.Fatalf("cláusula %d = %q, esperada %q", index+1, sections[index], expected)
		}
	}

	// Toda cláusula tem texto: nenhuma ficou só com o título.
	blocks := blocksOf(contract)
	for index, block := range blocks {
		if block.Kind != pdf.BlockSection || !strings.HasPrefix(block.Text, "Cláusula ") {
			continue
		}
		if index+1 >= len(blocks) || blocks[index+1].Kind != pdf.BlockText || strings.TrimSpace(blocks[index+1].Text) == "" {
			t.Fatalf("%s sem texto depois do título", block.Text)
		}
	}
}

func TestBuildContractTitleFollowsTheStatus(t *testing.T) {
	cases := map[string]string{
		"active":    "CONTRATO DE LOCAÇÃO RESIDENCIAL",
		"season":    "CONTRATO DE LOCAÇÃO POR TEMPORADA",
		"suspended": "CONTRATO DE LOCAÇÃO RESIDENCIAL (SEM COBRANÇA AUTOMÁTICA)",
	}
	for status, expected := range cases {
		input := residentialContract()
		input.Status = status
		contract, err := pdf.BuildContract(input)
		if err != nil {
			t.Fatalf("BuildContract (%s) devolveu erro: %v", status, err)
		}
		title, ok := findBlock(contract, expected)
		if !ok || title.Kind != pdf.BlockTitle || title.Text != expected {
			t.Fatalf("status %s: título = %+v, esperado %q", status, title, expected)
		}
		if title.Align != pdf.AlignCenter {
			t.Fatalf("status %s: alinhamento do título = %q", status, title.Align)
		}
	}
}

func TestBuildContractQualifiesThePartiesAndTheProperty(t *testing.T) {
	contract, err := pdf.BuildContract(residentialContract())
	if err != nil {
		t.Fatalf("BuildContract devolveu erro: %v", err)
	}
	text := joinText(contract)

	for _, expected := range []string{
		"IDENTIFICAÇÃO DAS PARTES CONTRATANTES",
		"LOCADOR:",
		"Nome: João Souza",
		"CPF: 987.654.321-00",
		"Endereço: Rua do Sol, 45, Cabo Frio - RJ",
		"LOCATÁRIO:",
		"Nome: Maria Silva",
		"CPF: 123.456.789-00",
		"Telefone: (22) 99999-0000",
		"O presente contrato tem como OBJETO o imóvel Casa da Praia de propriedade do LOCADOR, situado na Rua das Flores, 123, cidade de Cabo Frio, CEP 28900-000, no Estado do Rio de Janeiro.",
		"As partes acima identificadas têm, entre si, justo e acertado o presente Contrato de Locação Residencial",
		"Parágrafo único: O imóvel entregue na data da assinatura deste contrato",
		"Por estarem, assim justos e contratados, firmam o presente instrumento, em duas vias de igual teor, juntamente com 2 (duas) testemunhas.",
		"Cabo Frio, 21 de setembro de 2026.",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("contrato sem %q\n---\n%s", expected, text)
		}
	}
}

func TestBuildContractWritesTheRentInWordsAndTheDueDay(t *testing.T) {
	contract, err := pdf.BuildContract(residentialContract())
	if err != nil {
		t.Fatalf("BuildContract devolveu erro: %v", err)
	}
	text := joinText(contract)

	expected := "Como aluguel mensal, o LOCATÁRIO se obrigará a pagar o valor de R$ 1.200,50 (mil e duzentos reais e cinquenta centavos), a ser efetuado diretamente ao LOCADOR, devendo fazê-lo até o dia dez do mês vincendo, sob pena de multa, correções e despesas previstas nas Cláusulas 14ª e 15ª."
	if !strings.Contains(text, expected) {
		t.Fatalf("cláusula 9ª do contrato residencial ausente\n---\n%s", text)
	}
}

func TestBuildContractTermFollowsTheStatus(t *testing.T) {
	season := residentialContract()
	season.Status = "season"
	contract, err := pdf.BuildContract(season)
	if err != nil {
		t.Fatalf("BuildContract devolveu erro: %v", err)
	}
	text := joinText(contract)

	for _, expected := range []string{
		"A presente locação terá o lapso temporal de validade compreendido entre o dia 1, do mês março no ano de 2026 e o dia 28 do mês fevereiro no ano de 2027, data a qual o imóvel deverá ser devolvido nas condições previstas na Cláusula 5ª",
		"A presente LOCAÇÃO destina-se ao uso do imóvel para fins de TEMPORADA (veraneio e lazer), pelo período pactuado na Cláusula 21ª",
		"Pela utilização do imóvel durante a temporada, o LOCATÁRIO pagará ao LOCADOR o valor de R$ 1.200,50 (mil e duzentos reais e cinquenta centavos)",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("contrato de temporada sem %q\n---\n%s", expected, text)
		}
	}

	active := residentialContract()
	activeContract, err := pdf.BuildContract(active)
	if err != nil {
		t.Fatalf("BuildContract devolveu erro: %v", err)
	}
	if !strings.Contains(joinText(activeContract), "A presente locação terá o lapso temporal de validade de 12 meses, a iniciar-se no dia 1, do mês março no ano de 2026 e findar-se no dia 28 do mês fevereiro no ano de 2027") {
		t.Fatal("prazo do contrato residencial não tem os 12 meses")
	}

	suspended := residentialContract()
	suspended.Status = "suspended"
	suspendedContract, err := pdf.BuildContract(suspended)
	if err != nil {
		t.Fatalf("BuildContract devolveu erro: %v", err)
	}
	if !strings.Contains(joinText(suspendedContract), "Este contrato é mantido para fins de registro de posse/uso do imóvel, sem cobrança automática de aluguel. Eventual valor acordado entre as partes (R$ 1.200,50) poderá ser quitado mediante recibo emitido manualmente.") {
		t.Fatal("cláusula 9ª do contrato suspenso ausente")
	}
}

func TestBuildContractSignaturesAndSeal(t *testing.T) {
	contract, err := pdf.BuildContract(residentialContract())
	if err != nil {
		t.Fatalf("BuildContract devolveu erro: %v", err)
	}

	signatures := blocksOfKind(contract, pdf.BlockSignature)
	expected := []struct {
		label string
		align string
	}{
		{"Locador (a)", pdf.AlignLeft},
		{"Cônjuge", pdf.AlignRight},
		{"Locatário (a)", pdf.AlignLeft},
		{"Cônjuge", pdf.AlignRight},
		{"Testemunha 1", pdf.AlignLeft},
		{"Testemunha 2", pdf.AlignRight},
	}
	if len(signatures) != len(expected) {
		t.Fatalf("linhas de assinatura = %d, esperado %d", len(signatures), len(expected))
	}
	for index, want := range expected {
		if signatures[index].Text != want.label || signatures[index].Align != want.align {
			t.Fatalf("assinatura %d = %q/%q, esperada %q/%q", index, signatures[index].Text, signatures[index].Align, want.label, want.align)
		}
	}
	if notes := blocksOfKind(contract, pdf.BlockSignatureNote); len(notes) != 2 {
		t.Fatalf("rótulos de RG = %d, esperado 2", len(notes))
	}

	seals := blocksOfKind(contract, pdf.BlockAuth)
	if len(seals) != 1 {
		t.Fatalf("blocos de autenticidade = %d, esperado 1", len(seals))
	}
	if seals[0].QR != testContractValidationURL {
		t.Fatalf("URL do QR = %q, esperada %q", seals[0].QR, testContractValidationURL)
	}
	if seals[0].Code != testAuthCode {
		t.Fatalf("chave AUTH = %q, esperada %q", seals[0].Code, testAuthCode)
	}
}

func TestRenderContractProducesAPDF(t *testing.T) {
	for _, status := range []string{"active", "season", "suspended"} {
		input := residentialContract()
		input.Status = status
		contract, err := pdf.BuildContract(input)
		if err != nil {
			t.Fatalf("BuildContract (%s) devolveu erro: %v", status, err)
		}
		rendered, err := pdf.Render(contract)
		if err != nil {
			t.Fatalf("Render (%s) devolveu erro: %v", status, err)
		}
		assertPDF(t, rendered)
	}
}
