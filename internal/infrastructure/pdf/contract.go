package pdf

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Contrato de locação.
//
// O documento reproduz o contrato do app atual (`src/utils/contract-pdf.ts`): o
// título muda com a situação do contrato, seguem-se a qualificação das partes, o
// objeto, e as 25 cláusulas transcritas literalmente — sem reescrever, resumir ou
// renumerar nada —, o fecho com as assinaturas e o selo de autenticidade com QR
// Code e chave AUTH.

// ContractInput reúne tudo que o contrato imprime.
type ContractInput struct {
	LeaseID         string
	TenantName      string
	TenantPhone     string
	TenantCPF       string
	PropertyName    string
	PropertyAddress string
	PropertyCity    string
	PropertyState   string
	PropertyZip     string
	StartDate       string
	EndDate         string
	DueDay          int
	RentAmount      float64
	// Status é a situação do contrato (`active`, `suspended`, `season` ou
	// `terminated`) e decide o título e o texto das cláusulas 2ª, 9ª e 21ª.
	Status          string
	LandlordName    string
	LandlordCPF     string
	LandlordAddress string
	AuthCode        string
	ValidationURL   string
	IssuedAt        time.Time
}

// contractQRSize é o lado do QR Code do selo, em pontos.
const contractQRSize = 50.0

// contractAuthCaption é a legenda do selo no contrato.
const contractAuthCaption = "Este contrato possui registro eletrônico oficial. Aponte a câmera para consultar a autenticidade e vigência."

// contractSignatureLineWidthMM é a largura da linha de assinatura do contrato.
const contractSignatureLineWidthMM = 67

// BuildContract monta o modelo do contrato de locação.
func BuildContract(input ContractInput) (Document, error) {
	isSeason := input.Status == string(domain.LeaseStatusSeason)
	isSuspended := input.Status == string(domain.LeaseStatusSuspended)
	issuedAt := input.IssuedAt
	if issuedAt.IsZero() {
		issuedAt = time.Now()
	}

	city := capitalize(input.PropertyCity)
	state := strings.ToUpper(strings.TrimSpace(input.PropertyState))
	rentAmount := format.FormatCurrency(input.RentAmount)
	rentInWords := domain.MoneyInWords(input.RentAmount)
	dueDayInWords := dueDayInWords(input.DueDay)

	blocks := make([]Block, 0, 96)
	blocks = append(blocks, Block{
		Kind:  BlockTitle,
		Text:  contractTitle(isSeason, isSuspended),
		Align: AlignCenter,
		Size:  15,
		Bold:  true,
	})
	blocks = append(blocks, Block{Kind: BlockRule})
	blocks = append(blocks, Block{Kind: BlockGap, Size: 6})

	blocks = append(blocks, section("IDENTIFICAÇÃO DAS PARTES CONTRATANTES"))
	blocks = append(blocks, paragraph("LOCADOR:"))
	blocks = append(blocks, label("Nome:", input.LandlordName))
	if strings.TrimSpace(input.LandlordCPF) != "" {
		blocks = append(blocks, label("CPF:", input.LandlordCPF))
	}
	if strings.TrimSpace(input.LandlordAddress) != "" {
		blocks = append(blocks, paragraph("Endereço: "+input.LandlordAddress))
	}
	blocks = append(blocks, Block{Kind: BlockGap, Size: 6})
	blocks = append(blocks, paragraph("LOCATÁRIO:"))
	blocks = append(blocks, label("Nome:", input.TenantName))
	blocks = append(blocks, label("CPF:", input.TenantCPF))
	blocks = append(blocks, label("Telefone:", input.TenantPhone))
	blocks = append(blocks, Block{Kind: BlockGap, Size: 6})
	blocks = append(blocks, paragraph(
		"As partes acima identificadas têm, entre si, justo e acertado o presente Contrato de Locação Residencial, que se regerá pelas cláusulas seguintes e pelas condições de preço, forma e termo de pagamento descritas no presente.",
	))

	// ── Cláusula 1ª - DO OBJETO ───────────────────────────────────────
	blocks = append(blocks, section("Cláusula 1ª - DO OBJETO"))
	blocks = append(blocks, paragraph(contractObject(input, city, state)))
	blocks = append(blocks, paragraph(
		"Parágrafo único: O imóvel entregue na data da assinatura deste contrato, pelo LOCADOR ao LOCATÁRIO, possui as características contidas no auto de vistoria anexo, que desde já aceitam expressamente.",
	))

	// ── Cláusula 2ª - DA DESTINAÇÃO ───────────────────────────────────
	blocks = append(blocks, section("Cláusula 2ª - DA DESTINAÇÃO"))
	if isSeason {
		blocks = append(blocks, paragraph(
			"A presente LOCAÇÃO destina-se ao uso do imóvel para fins de TEMPORADA (veraneio e lazer), pelo período pactuado na Cláusula 21ª, restando proibido ao LOCATÁRIO sublocá-lo ou usá-lo de forma diferente do previsto, salvo autorização expressa do LOCADOR.",
		))
	} else {
		blocks = append(blocks, paragraph(
			"A presente LOCAÇÃO destina-se restritivamente ao uso do imóvel para fins residenciais, restando proibido ao LOCATÁRIO, sublocá-lo ou usá-lo de forma diferente do previsto, salvo autorização expressa do LOCADOR.",
		))
	}

	// ── Cláusula 3ª - DO ESTADO DO IMÓVEL ─────────────────────────────
	blocks = append(blocks, section("Cláusula 3ª - DO ESTADO DO IMÓVEL"))
	blocks = append(blocks, paragraph(
		"O imóvel objeto deste contrato será entregue nas condições descritas no auto de vistoria, ou seja, com instalações elétricas e hidráulicas em perfeito funcionamento, com todos os cômodos e paredes pintados, sendo que portas, portões e acessórios se encontram também em funcionamento correto, devendo o LOCATÁRIO mantê-lo desta forma. Fica também acordado, que o imóvel será devolvido nas mesmas condições previstas no auto de vistoria, além de, no ato da entrega das chaves, com todas as despesas pagas.",
	))

	// ── Cláusula 4ª - DAS BENFEITORIAS ────────────────────────────────
	blocks = append(blocks, section("Cláusula 4ª - DAS BENFEITORIAS"))
	blocks = append(blocks, paragraph(
		"Qualquer benfeitoria ou construção que seja destinada ao imóvel objeto deste, deverá de imediato, ser submetida a autorização expressa do LOCADOR. Vindo a ser feita a benfeitoria, faculta ao LOCADOR aceitá-la ou não, restando ao LOCATÁRIO em caso do LOCADOR não aceitá-la, modificar o imóvel da maneira que lhe foi entregue. As benfeitorias, consertos ou reparos farão parte integrante do imóvel, não assistindo ao LOCATÁRIO o direito de retenção ou indenização sobre a mesma, com exceção das benfeitorias necessárias, na forma da lei.",
	))

	// ── Cláusula 5ª - DA RESTITUIÇÃO ──────────────────────────────────
	blocks = append(blocks, section("Cláusula 5ª - DA RESTITUIÇÃO"))
	blocks = append(blocks, paragraph(
		"O LOCATÁRIO restituirá o imóvel locado nas mesmas condições as quais o recebeu, quais sejam, pintado com tinta na cor contida no auto de vistoria, sendo que as instalações elétricas, hidráulicas e acessórios deverão também, estar em perfeitas condições de funcionamento, salvo as deterioração decorrentes do uso normal e habitual do imóvel.",
	))
	blocks = append(blocks, paragraph(
		"Parágrafo único: Os autos de vistoria inicial e final, que farão parte deste contrato conterão assinatura de duas testemunhas e dos contratantes.",
	))

	// ── Cláusula 6ª - DA VENDA DO IMÓVEL ──────────────────────────────
	blocks = append(blocks, section("Cláusula 6ª - DA VENDA DO IMÓVEL"))
	blocks = append(blocks, paragraph(
		"Caso o LOCADOR manifeste vontade de vender o imóvel objeto do presente, deverá propor por escrito ao LOCATÁRIO, que se obrigará a emitir a resposta em 30 (trinta) dias, a partir da comunicação inicial, sendo garantido ao LOCATÁRIO as mesmas condições de compra que serão ofertadas aos outros possíveis compradores.",
	))

	// ── Cláusula 7ª - DAS VISTORIAS ───────────────────────────────────
	blocks = append(blocks, section("Cláusula 7ª - DAS VISTORIAS"))
	blocks = append(blocks, paragraph(
		"O LOCATÁRIO permitirá ao LOCADOR, realizar vistorias no imóvel em dia e hora a serem combinados, podendo este último averiguar o funcionamento de todas as instalações e acessórios. Constatando-se algum vício que possa afetar a estrutura física do imóvel, desde que não seja a parte da estrutura em concreto, e desde que este vício seja por falta de manutenção a que está obrigado por lei o LOCATÁRIO, este ficará compelido a realizar o conserto, no prazo de 15 dias, observado o direito de retenção e/ou indenização das benfeitorias necessárias, na forma da lei. Não ocorrendo o conserto, o LOCADOR ficará facultado a RESCINDIR O CONTRATO no prazo de 30 (trinta) dias, sem prejuízo dos numerários previstos neste.",
	))

	// ── Cláusula 8ª - DOS ACIDENTES ───────────────────────────────────
	blocks = append(blocks, section("Cláusula 8ª - DOS ACIDENTES"))
	blocks = append(blocks, paragraph(
		"Qualquer acidente que porventura venha a ocorrer no imóvel por culpa ou dolo do LOCATÁRIO, o mesmo ficará obrigado a pagar, além da multa prevista na Cláusula 15ª, todas as despesas por danos causados ao imóvel, devendo restituí-lo no estado que o encontrou e que, sobretudo, teve conhecimento no auto de vistoria.",
	))

	// ── Cláusula 9ª - DO ALUGUEL ──────────────────────────────────────
	blocks = append(blocks, section("Cláusula 9ª - DO ALUGUEL"))
	switch {
	case isSeason:
		blocks = append(blocks, paragraph(fmt.Sprintf(
			"Pela utilização do imóvel durante a temporada, o LOCATÁRIO pagará ao LOCADOR o valor de %s (%s), a ser pago conforme pactuado entre as partes, sem que isso configure locação por prazo indeterminado ou mensalidade recorrente, sob pena de multa, correções e despesas previstas nas Cláusulas 14ª e 15ª.",
			rentAmount, rentInWords,
		)))
	case isSuspended:
		blocks = append(blocks, paragraph(fmt.Sprintf(
			"Este contrato é mantido para fins de registro de posse/uso do imóvel, sem cobrança automática de aluguel. Eventual valor acordado entre as partes (%s) poderá ser quitado mediante recibo emitido manualmente.",
			rentAmount,
		)))
	default:
		blocks = append(blocks, paragraph(fmt.Sprintf(
			"Como aluguel mensal, o LOCATÁRIO se obrigará a pagar o valor de %s (%s), a ser efetuado diretamente ao LOCADOR, devendo fazê-lo até o dia %s do mês vincendo, sob pena de multa, correções e despesas previstas nas Cláusulas 14ª e 15ª.",
			rentAmount, rentInWords, dueDayInWords,
		)))
	}

	// ── Cláusula 10ª - DO RECIBO ──────────────────────────────────────
	blocks = append(blocks, section("Cláusula 10ª - DO RECIBO"))
	blocks = append(blocks, paragraph(
		"Fica obrigado o LOCADOR ou seu procurador, a emitir recibo da quantia paga, no ato do pagamento, relacionando pormenorizadamente todos os valores oriundos de juros, ou outra despesa. Caso o LOCATÁRIO venha a efetuar o pagamento do aluguel através de cheque, o LOCADOR emitirá o recibo de pagamento mencionado o número do cheque, número do banco e o emitente do mesmo, que será dado quitação somente após compensação do mesmo.",
	))

	// ── Cláusula 11ª - DO REAJUSTE ────────────────────────────────────
	blocks = append(blocks, section("Cláusula 11ª - DO REAJUSTE"))
	blocks = append(blocks, paragraph(
		"O valor do aluguel será reajustado anualmente, tendo como base, os índices previstos e acumulados no período anual do IGP-M, em caso de falta deste índice, o reajustamento do aluguel terá por base a média da variação dos índices inflacionários do ano corrente ao da execução do aluguel, até o primeiro dia anterior ao pagamento de todos os valores devidos.",
	))

	// ── Cláusula 12ª - DA COBRANÇA ────────────────────────────────────
	blocks = append(blocks, section("Cláusula 12ª - DA COBRANÇA"))
	blocks = append(blocks, paragraph(
		"Faculta ao LOCADOR ou ao seu procurador, cobrar do LOCATÁRIO, o(s) aluguel(éis) e despesa(s) vencido(s), oriundo(s) deste contrato, utilizando-se de todos os meios legais admitidos.",
	))
	blocks = append(blocks, paragraph(
		"Parágrafo único: O(s) cheque(s) utilizado(s) em pagamento, se não compensado(s) até o quinto dia útil contados a partir do vencimento do aluguel, ocasionará(ão) mora do LOCATÁRIO, facultando ao LOCADOR a aplicação do disposto na Cláusula 15ª.",
	))

	// ── Cláusula 13ª - DAS DESPESAS ───────────────────────────────────
	blocks = append(blocks, section("Cláusula 13ª - DAS DESPESAS"))
	blocks = append(blocks, paragraph(
		"As despesas diretamente ligadas à conservação do imóvel, quais sejam: água, ressalvado o disposto no parágrafo único da cláusula nona, para que não haja duplo pagamento; luz, gás e telefone e o valor único anual estipulado na cláusula nona parágrafo segundo, que estejam relacionadas ao uso do mesmo, ficarão sob a responsabilidade do LOCATÁRIO, ressalvando-se quanto à contribuição de melhoria, que é de responsabilidade do LOCADOR. Os demais tributos e taxas incidentes sobre o imóvel são de responsabilidade do LOCADOR.",
	))

	// ── Cláusula 14ª - DA MORA ────────────────────────────────────────
	blocks = append(blocks, section("Cláusula 14ª - DA MORA"))
	blocks = append(blocks, paragraph(
		"O LOCATÁRIO, não vindo a efetuar o pagamento do aluguel até a data estipulada na Cláusula 10ª, fica obrigado a pagar multa de 2% (dois por cento) sobre o valor do aluguel estipulado neste contrato, bem como juros de mora de 1% (um por cento) ao mês, mais correção monetária.",
	))

	// ── Cláusula 15ª - DO ATRASO ──────────────────────────────────────
	blocks = append(blocks, section("Cláusula 15ª - DO ATRASO"))
	blocks = append(blocks, paragraph(
		"Em caso de atraso no pagamento dos aluguéis e não compensando o cheque destinado para tal fim, restará em mora o LOCATÁRIO, ficando responsabilizado por todos os pagamentos previstos neste atraso, sem prejuízo do pagamento da multa, juros de mora e correção monetária. Não configurarão novação ou adição às cláusulas contidas no presente instrumento, os atos de mera tolerância referentes ao atraso no pagamento do aluguel.",
	))

	// ── Cláusula 16ª - DA TOLERÂNCIA ──────────────────────────────────
	blocks = append(blocks, section("Cláusula 16ª - DA TOLERÂNCIA"))
	blocks = append(blocks, paragraph(
		"O LOCATÁRIO terá um prazo de tolerância para efetuar o pagamento do aluguel até o 5º (quinto) dia útil após o vencimento, caso não seja dia útil, ficará obrigado desde já a efetuar o pagamento no primeiro dia útil subsequente a esta data.",
	))

	// ── Cláusula 17ª - DA MULTA CONTRATUAL ────────────────────────────
	blocks = append(blocks, section("Cláusula 17ª - DA MULTA CONTRATUAL"))
	blocks = append(blocks, paragraph(
		"As partes estipulam o pagamento da multa no valor de 3 (três) aluguéis vigentes a época da ocorrência do fato, a ser aplicada àquele que venha a infringir quaisquer das cláusulas contidas neste contrato exceto quando da ocorrência das hipóteses previstas nas Cláusulas 15ª e 18ª, sendo dispensado do pagamento da multa no caso do contrato tornar-se por prazo indeterminado.",
	))

	// ── Cláusula 18ª - DA RESCISÃO ────────────────────────────────────
	blocks = append(blocks, section("Cláusula 18ª - DA RESCISÃO"))
	blocks = append(blocks, paragraph(
		"Ocorrerá a rescisão do presente contrato, independente de qualquer comunicação prévia ou indenização por parte do LOCADOR, garantindo-se ao LOCATÁRIO o prazo de trinta dias para desocupar o imóvel, quando:",
	))
	blocks = append(blocks, paragraph(
		"a) Ocorrendo qualquer sinistro, incêndio ou algo que venha a impossibilitar a posse do imóvel, independente de dolo ou culpa do LOCATÁRIO; bem como quaisquer outras hipóteses que maculem o imóvel de vício e impossibilite sua posse;",
	))
	blocks = append(blocks, paragraph(
		"b) Em hipótese de desapropriação do imóvel alugado ou manifestação do poder público.",
	))

	// ── Cláusula 19ª - DO USO DIVERSO ─────────────────────────────────
	blocks = append(blocks, section("Cláusula 19ª - DO USO DIVERSO"))
	blocks = append(blocks, paragraph(
		"Caso o imóvel seja utilizado de forma diversa da locação residencial, restará facultado ao LOCADOR, rescindir o presente contrato de plano, sem gerar direito a indenização ou qualquer ônus por parte deste último. Sem prejuízo da obrigação do LOCATÁRIO de efetuar o pagamento das multas e despesas previstas na Cláusula 15ª. Salvo autorização expressa do LOCADOR.",
	))

	// ── Cláusula 20ª - DA RESCISÃO PELO LOCATÁRIO ─────────────────────
	blocks = append(blocks, section("Cláusula 20ª - DA RESCISÃO PELO LOCATÁRIO"))
	blocks = append(blocks, paragraph(
		"Caso o LOCATÁRIO deseje rescindir o presente contrato, deverá notificar ao LOCADOR, com antecedência mínima de 30 (trinta) dias, garantindo-se a incidência da cláusula 17ª.",
	))

	// ── Cláusula 21ª - DO PRAZO ───────────────────────────────────────
	blocks = append(blocks, section("Cláusula 21ª - DO PRAZO"))
	blocks = append(blocks, paragraph(contractTerm(input, isSeason || isSuspended)))

	// ── Cláusula 22ª - DA PRORROGAÇÃO ─────────────────────────────────
	blocks = append(blocks, section("Cláusula 22ª - DA PRORROGAÇÃO"))
	blocks = append(blocks, paragraph(
		"Ultrapassando o contrato a data prevista, ou seja, tornando-se contrato por tempo indeterminado, poderá o LOCADOR rescindi-lo a qualquer tempo, desde que ocorra notificação por escrito ao LOCATÁRIO, que ficará compelido a sair do imóvel dentro do prazo de 30 (trinta) dias, a contar do recebimento da notificação, facultando-se ao LOCATÁRIO o mesmo direito. Ocorrendo prorrogação, o LOCATÁRIO e o LOCADOR ficarão obrigados por todo o teor deste contrato.",
	))

	// ── Cláusula 23ª - DO REGISTRO ────────────────────────────────────
	blocks = append(blocks, section("Cláusula 23ª - DO REGISTRO"))
	blocks = append(blocks, paragraph(
		"Este contrato deve ser registrado no Cartório de Registro Imobiliário, ficando sob a responsabilidade do LOCADOR a efetivação desta medida.",
	))

	// ── Cláusula 24ª - DA CAUÇÃO ──────────────────────────────────────
	blocks = append(blocks, section("Cláusula 24ª - DA CAUÇÃO"))
	blocks = append(blocks, paragraph(
		"As partes mutuamente dispensam quaisquer tipos de caução, tais como fiador e avalistas.",
	))

	// ── Cláusula 25ª - DO FORO ────────────────────────────────────────
	blocks = append(blocks, section("Cláusula 25ª - DO FORO"))
	blocks = append(blocks, paragraph(fmt.Sprintf(
		"O presente contrato passa a vigorar entre as partes a partir da assinatura do mesmo, as quais elegem o foro da cidade de %s, onde se situa o imóvel, para dirimirem quaisquer dúvidas provenientes da execução e cumprimento do mesmo.",
		city,
	)))

	blocks = append(blocks, Block{Kind: BlockGap, Size: 18})
	blocks = append(blocks, paragraph(
		"Por estarem, assim justos e contratados, firmam o presente instrumento, em duas vias de igual teor, juntamente com 2 (duas) testemunhas.",
	))
	blocks = append(blocks, paragraph(city+", "+formatLongDate(issuedAt)+"."))
	blocks = append(blocks, Block{Kind: BlockGap, Size: 18})

	for _, row := range [][2]string{
		{"Locador (a)", "Cônjuge"},
		{"Locatário (a)", "Cônjuge"},
		{"Testemunha 1", "Testemunha 2"},
	} {
		blocks = append(blocks,
			Block{Kind: BlockSignature, Text: row[0], Align: AlignLeft, Size: 11, Bold: true, WidthMM: contractSignatureLineWidthMM},
			Block{Kind: BlockSignature, Text: row[1], Align: AlignRight, Size: 11, Bold: true, WidthMM: contractSignatureLineWidthMM},
		)
	}
	blocks = append(blocks, Block{Kind: BlockGap, Size: 4})
	blocks = append(blocks,
		Block{Kind: BlockSignatureNote, Text: "RG:", Align: AlignLeft, Size: 10, Tone: ToneMuted},
		Block{Kind: BlockSignatureNote, Text: "RG:", Align: AlignRight, Size: 10, Tone: ToneMuted},
	)

	blocks = append(blocks, Block{Kind: BlockGap, Size: 22})
	blocks = append(blocks, authBlock(input.ValidationURL, input.AuthCode, contractAuthCaption, contractQRSize))

	return newDocument(blocks), nil
}

// contractTitle devolve o título do contrato conforme a situação: temporada,
// residencial com cobrança suspensa ou residencial padrão.
func contractTitle(isSeason, isSuspended bool) string {
	switch {
	case isSeason:
		return "CONTRATO DE LOCAÇÃO POR TEMPORADA"
	case isSuspended:
		return "CONTRATO DE LOCAÇÃO RESIDENCIAL (SEM COBRANÇA AUTOMÁTICA)"
	default:
		return "CONTRATO DE LOCAÇÃO RESIDENCIAL"
	}
}

// contractObject monta a cláusula 1ª com o nome, o endereço, a cidade, o CEP e o
// estado do imóvel.
func contractObject(input ContractInput, city, state string) string {
	propertyName := strings.TrimSpace(input.PropertyName)
	address := strings.TrimSpace(input.PropertyAddress)
	location := strings.TrimPrefix(address, prepositionPrefix(address))

	namePart := ""
	if propertyName != "" {
		namePart = " " + propertyName
	}
	situatedPart := ""
	if location != "" {
		situatedPart = ", situado na " + location
	}
	zipPart := ""
	if zip := strings.TrimSpace(input.PropertyZip); zip != "" {
		zipPart = ", CEP " + zip
	}
	statePart := ""
	if state != "" {
		if state == "RJ" {
			statePart = ", no Estado do Rio de Janeiro"
		} else {
			statePart = ", no Estado de " + state
		}
	}
	return fmt.Sprintf(
		"O presente contrato tem como OBJETO o imóvel%s de propriedade do LOCADOR%s, cidade de %s%s%s.",
		namePart, situatedPart, city, zipPart, statePart,
	)
}

// prepositionPrefix devolve a preposição inicial do endereço (`na `, `no ` ou
// `em `), quando existe, para não repeti-la em "situado na".
func prepositionPrefix(address string) string {
	lowered := strings.ToLower(address)
	for _, prefix := range []string{"na ", "no ", "em "} {
		if strings.HasPrefix(lowered, prefix) {
			return address[:len(prefix)]
		}
	}
	return ""
}

// contractTerm monta a cláusula 21ª (prazo). Contratos de temporada e suspensos
// usam o texto com as datas compreendidas; o residencial padrão usa o texto de
// 12 meses.
func contractTerm(input ContractInput, usesExplicitRange bool) string {
	startYear, startMonth, startDay := contractDateParts(input.StartDate, 2026)
	endYear, endMonth, endDay := contractDateParts(input.EndDate, 2027)
	start := fmt.Sprintf("%d, do mês %s no ano de %d", startDay, monthNamesInWords[startMonth-1], startYear)
	end := fmt.Sprintf("%d do mês %s no ano de %d", endDay, monthNamesInWords[endMonth-1], endYear)
	closing := ", data a qual o imóvel deverá ser devolvido nas condições previstas na Cláusula 5ª, efetivando-se com a entrega das chaves, independentemente de aviso ou qualquer outra medida judicial ou extrajudicial."
	if usesExplicitRange {
		return "A presente locação terá o lapso temporal de validade compreendido entre o dia " + start + " e o dia " + end + closing
	}
	return "A presente locação terá o lapso temporal de validade de 12 meses, a iniciar-se no dia " +
		start + " e findar-se no dia " + end + closing
}

// contractDateParts separa uma data ISO em ano, mês e dia, caindo para o padrão
// do app atual (2026 e 2027) quando a data está incompleta.
func contractDateParts(value string, defaultYear int) (int, int, int) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	year, month, day := defaultYear, 1, 1
	if len(parts) > 0 {
		if parsed, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
			year = parsed
		}
	}
	if len(parts) > 1 {
		if parsed, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil && parsed >= 1 && parsed <= 12 {
			month = parsed
		}
	}
	if len(parts) > 2 {
		if parsed, err := strconv.Atoi(strings.TrimSpace(parts[2])); err == nil && parsed >= 1 && parsed <= 31 {
			day = parsed
		}
	}
	return year, month, day
}

// dueDayInWords escreve o dia de vencimento por extenso ("primeiro" a "trinta e
// um"), como o app atual imprime na cláusula 9ª.
func dueDayInWords(dueDay int) string {
	days := [...]string{
		"primeiro", "dois", "três", "quatro", "cinco", "seis", "sete", "oito", "nove", "dez",
		"onze", "doze", "treze", "quatorze", "quinze", "dezesseis", "dezessete", "dezoito", "dezenove",
		"vinte", "vinte e um", "vinte e dois", "vinte e três", "vinte e quatro", "vinte e cinco",
		"vinte e seis", "vinte e sete", "vinte e oito", "vinte e nove", "trinta", "trinta e um",
	}
	if dueDay < 1 || dueDay > len(days) {
		return strconv.Itoa(dueDay)
	}
	return days[dueDay-1]
}

// capitalize devolve o texto com a primeira letra de cada palavra em maiúscula.
func capitalize(value string) string {
	words := strings.Fields(value)
	for index, word := range words {
		runes := []rune(strings.ToLower(word))
		if len(runes) == 0 {
			continue
		}
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		words[index] = string(runes)
	}
	return strings.Join(words, " ")
}
