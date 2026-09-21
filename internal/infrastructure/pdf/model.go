// Package pdf monta e desenha os documentos oficiais do Aluxen em PDF A4.
//
// A montagem é separada do desenho: `BuildReceipt` e `BuildContract` devolvem um
// `Document` — páginas com blocos tipados, sem nenhum traço do motor de PDF — e
// `Render` traduz esse modelo em bytes de PDF. Assim o conteúdo jurídico (valor
// por extenso, cláusulas, chave de autenticidade) é verificável em teste sem
// abrir o arquivo gerado.
package pdf

// Document é o documento montado: uma sequência de páginas com blocos.
type Document struct {
	Pages []Page
}

// Page é uma página do documento. O construtor normalmente devolve uma única
// página com todo o fluxo; o renderizador quebra página sozinho quando o
// conteúdo ultrapassa a área útil (o modelo não precisa saber medir texto).
type Page struct {
	Blocks []Block
}

// BlockKind identifica o desenho de um bloco.
type BlockKind string

const (
	// BlockBrand é a marca do emissor: imagem (quando houver) e/ou wordmark.
	BlockBrand BlockKind = "brand"
	// BlockTitle é o título do documento.
	BlockTitle BlockKind = "title"
	// BlockSubtitle é a linha de identificação abaixo do título.
	BlockSubtitle BlockKind = "subtitle"
	// BlockSection é o título de seção/cláusula.
	BlockSection BlockKind = "section"
	// BlockName destaca um nome (locatário, por exemplo) em corpo grande.
	BlockName BlockKind = "name"
	// BlockText é um parágrafo corrido, quebrado em linhas.
	BlockText BlockKind = "text"
	// BlockIndent é um parágrafo com a primeira linha recuada.
	BlockIndent BlockKind = "indent"
	// BlockLabel é um rótulo em negrito seguido do valor na mesma linha.
	BlockLabel BlockKind = "label"
	// BlockMetric é uma métrica do resumo: rótulo pequeno sobre o valor.
	BlockMetric BlockKind = "metric"
	// BlockRule é uma linha horizontal de separação.
	BlockRule BlockKind = "rule"
	// BlockGap é um espaço vertical (`Size` em pontos).
	BlockGap BlockKind = "gap"
	// BlockTable é uma tabela de colunas fixas: o cabeçalho em negrito e uma
	// linha por registro (`Columns`, `Rows` e `ColumnWidths`).
	BlockTable BlockKind = "table"
	// BlockImage é uma imagem embutida (`ImagePNG`), medida em milímetros.
	BlockImage BlockKind = "image"
	// BlockSignature é uma linha de assinatura com o rótulo do signatário. Duas
	// linhas consecutivas (esquerda e direita) dividem a mesma linha da página.
	BlockSignature BlockKind = "signature"
	// BlockSignatureNote é um rótulo de assinatura sem linha (`RG:`, por
	// exemplo), que também ocupa meia linha da página.
	BlockSignatureNote BlockKind = "signature-note"
	// BlockAuth é o bloco de autenticidade: QR Code, chave AUTH e URL.
	BlockAuth BlockKind = "auth"
	// BlockQR é um QR Code isolado, centralizado (`QR` é o conteúdo).
	BlockQR BlockKind = "qr"
)

// Tons de texto. O tom escolhe a cor semântica do bloco sem que o modelo
// carregue valores de cor.
const (
	ToneInk    = "ink"
	ToneMuted  = "muted"
	ToneAccent = "accent"
	ToneDanger = "danger"
)

// Alinhamentos aceitos pelos blocos.
const (
	AlignLeft   = "left"
	AlignCenter = "center"
	AlignRight  = "right"
)

// Block é uma peça do documento. O renderizador percorre os blocos na ordem em
// que aparecem, quebrando página quando o espaço acaba.
//
// `Size` é o corpo da fonte em pontos. `Value` é o valor de um bloco `label` ou
// `metric`, `Code` é a chave AUTH impressa no bloco `auth`, `QR` é o conteúdo do
// QR Code (a URL pública de validação) e `WidthMM`/`HeightMM` medem imagens em
// milímetros. Campo vazio usa o padrão do tipo de bloco.
type Block struct {
	Kind     BlockKind
	Text     string
	Value    string
	Code     string
	Size     float64
	Bold     bool
	Align    string
	Tone     string
	ImagePNG []byte
	WidthMM  float64
	HeightMM float64
	QR       string
	// Columns e Rows formam a tabela do bloco `table`: o cabeçalho e as linhas
	// de dados, célula a célula. Cada célula é quebrada em linhas dentro da
	// largura da sua coluna.
	Columns []string
	Rows    [][]string
	// ColumnWidths são as frações de largura das colunas (somam 1). Sem elas, as
	// colunas saem com a mesma largura.
	ColumnWidths []float64
}

// newDocument monta um documento de uma página a partir dos blocos.
func newDocument(blocks []Block) Document {
	return Document{Pages: []Page{{Blocks: blocks}}}
}

// label monta um bloco de rótulo e valor (`Nome: Fulano`).
func label(text, value string) Block {
	return Block{Kind: BlockLabel, Text: text, Value: value}
}

// paragraph monta um parágrafo corrido.
func paragraph(text string) Block {
	return Block{Kind: BlockText, Text: text}
}

// section monta o título de uma seção/cláusula.
func section(text string) Block {
	return Block{Kind: BlockSection, Text: text}
}

// metric monta uma célula do resumo (rótulo sobre o valor).
func metric(text, value string, size float64, tone string) Block {
	return Block{Kind: BlockMetric, Text: text, Value: value, Size: size, Tone: tone}
}

// table monta uma tabela: o cabeçalho, as linhas de dados e as frações de
// largura das colunas.
func table(columns []string, rows [][]string, widths []float64, size float64) Block {
	return Block{Kind: BlockTable, Columns: columns, Rows: rows, ColumnWidths: widths, Size: size}
}

// authBlock monta o selo de autenticidade do documento.
func authBlock(validationURL, authCode, caption string, size float64) Block {
	return Block{
		Kind:  BlockAuth,
		Text:  authTitle,
		Value: caption,
		Code:  authCode,
		QR:    validationURL,
		Size:  size,
	}
}

// Textos do selo de autenticidade. O app atual imprime esta mesma chamada nos
// dois documentos; a legenda muda porque o texto de cada um é diferente.
const authTitle = "Validação de Autenticidade Digital"
