package pdf

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"image/png"
	"strings"

	"github.com/go-pdf/fpdf"

	"github.com/passoz/aluxen/internal/infrastructure/authz"
)

// Desenho do documento em PDF A4.
//
// O renderizador percorre os blocos em fluxo: mantém o cursor vertical, quebra
// página quando o conteúdo não cabe e desenha cada tipo de bloco com as medidas
// e as cores do app atual. As fontes são as core fonts Helvetica (normal e
// negrito), então os acentos vão traduzidos para cp1252.

// Medidas da página, em pontos.
const (
	pageMargin   = 48.0
	topMargin    = 48.0
	bottomMargin = 52.0
	fontFamily   = "Helvetica"
	// lineSpacing é o avanço entre linhas de um parágrafo (1,5 do corpo).
	lineSpacing = 1.5
)

// rgb é uma cor da paleta do documento.
type rgb struct{ r, g, b int }

// Paleta do app atual (convertida de 0–1 para 0–255).
var (
	pageColour   = rgb{251, 251, 250}
	inkColour    = rgb{26, 31, 36}
	mutedColour  = rgb{99, 110, 117}
	accentColour = rgb{54, 140, 122}
	dangerColour = rgb{217, 89, 51}
	borderColour = rgb{212, 224, 219}
	sealColour   = rgb{247, 251, 250}
	sealAccent   = rgb{33, 115, 97}
)

// signatureRowState é a linha de assinatura já aberta pela metade esquerda.
type signatureRowState struct {
	top    float64
	height float64
}

// renderer desenha um Document na página.
type renderer struct {
	doc       *fpdf.Fpdf
	translate func(string) string
	width     float64
	height    float64
	cursorY   float64
	// signatureRow é a linha de assinatura já aberta pela metade esquerda, para
	// que a metade direita saia na mesma linha da página.
	signatureRow *signatureRowState
}

// Render desenha o documento em PDF A4 e devolve os bytes.
func Render(document Document) ([]byte, error) {
	engine := fpdf.New("P", "pt", "A4", "")
	engine.SetMargins(pageMargin, topMargin, pageMargin)
	engine.SetAutoPageBreak(false, 0)
	engine.SetTitle("Aluxen", true)
	engine.SetCreator("Aluxen", true)
	engine.SetProducer("Aluxen", true)
	if err := engine.Error(); err != nil {
		return nil, fmt.Errorf("iniciar o PDF: %w", err)
	}

	draw := &renderer{doc: engine, translate: engine.UnicodeTranslatorFromDescriptor("")}
	draw.width, draw.height = engine.GetPageSize()

	pages := document.Pages
	if len(pages) == 0 {
		pages = []Page{{}}
	}
	for _, page := range pages {
		if err := draw.startPage(); err != nil {
			return nil, err
		}
		for _, block := range page.Blocks {
			if err := draw.block(block); err != nil {
				return nil, err
			}
		}
		draw.flushSignatureRow()
	}

	var output bytes.Buffer
	if err := engine.Output(&output); err != nil {
		return nil, fmt.Errorf("gerar o PDF: %w", err)
	}
	return output.Bytes(), nil
}

// block desenha um bloco do modelo.
func (r *renderer) block(block Block) error {
	// Um bloco que não é de assinatura fecha a linha de assinatura aberta.
	if block.Kind != BlockSignature && block.Kind != BlockSignatureNote {
		r.flushSignatureRow()
	}

	switch block.Kind {
	case BlockBrand:
		return r.brand(block)
	case BlockTitle:
		return r.heading(block, 20, ToneInk)
	case BlockName:
		return r.heading(block, 22, ToneInk)
	case BlockSubtitle:
		return r.heading(block, 11, ToneMuted)
	case BlockSection:
		return r.section(block)
	case BlockText:
		return r.paragraph(block, false)
	case BlockIndent:
		return r.paragraph(block, true)
	case BlockLabel:
		return r.label(block)
	case BlockMetric:
		return r.metric(block)
	case BlockRule:
		return r.rule(block)
	case BlockGap:
		r.cursorY += defaultFloat(block.Size, 12)
		return nil
	case BlockImage:
		return r.image(block)
	case BlockSignature, BlockSignatureNote:
		return r.signature(block)
	case BlockAuth:
		return r.auth(block)
	case BlockQR:
		return r.standaloneQR(block)
	default:
		return fmt.Errorf("bloco %q sem desenho definido", block.Kind)
	}
}

// startPage abre uma página com o fundo do documento e o cursor no topo.
func (r *renderer) startPage() error {
	r.doc.AddPage()
	if err := r.doc.Error(); err != nil {
		return fmt.Errorf("abrir página: %w", err)
	}
	r.doc.SetFillColor(pageColour.r, pageColour.g, pageColour.b)
	r.doc.Rect(0, 0, r.width, r.height, "F")
	r.cursorY = topMargin
	r.signatureRow = nil
	return nil
}

// ensure garante o espaço pedido: sem ele, abre nova página.
func (r *renderer) ensure(needed float64) error {
	if r.cursorY+needed <= r.height-bottomMargin {
		return nil
	}
	return r.startPage()
}

// contentWidth é a largura útil da página.
func (r *renderer) contentWidth() float64 { return r.width - 2*pageMargin }

// font seleciona a fonte do desenho seguinte.
func (r *renderer) font(bold bool, size float64) {
	style := ""
	if bold {
		style = "B"
	}
	r.doc.SetFont(fontFamily, style, size)
}

// tone seleciona a cor do texto do tom pedido.
func (r *renderer) tone(tone string) rgb {
	switch tone {
	case ToneMuted:
		return mutedColour
	case ToneAccent:
		return accentColour
	case ToneDanger:
		return dangerColour
	default:
		return inkColour
	}
}

// text escreve uma linha com a linha de base em `y`.
func (r *renderer) text(x, y float64, value string, colour rgb) {
	r.doc.SetTextColor(colour.r, colour.g, colour.b)
	r.doc.Text(x, y, r.translate(value))
}

// alignedX devolve a abcissa inicial do texto para o alinhamento pedido.
func (r *renderer) alignedX(align string, textWidth float64) float64 {
	switch align {
	case AlignCenter:
		return pageMargin + (r.contentWidth()-textWidth)/2
	case AlignRight:
		return pageMargin + r.contentWidth() - textWidth
	default:
		return pageMargin
	}
}

// lineWidth mede o texto na fonte corrente.
func (r *renderer) lineWidth(value string) float64 {
	return r.doc.GetStringWidth(r.translate(value))
}

// brand desenha a marca: a imagem da logomarca quando existe e o wordmark ao
// lado (ou sozinho, quando não há imagem).
func (r *renderer) brand(block Block) error {
	size := defaultFloat(block.Size, 12)
	colour := r.tone(defaultString(block.Tone, ToneAccent))
	wordmark := strings.TrimSpace(block.Text)

	if len(block.ImagePNG) == 0 {
		if wordmark == "" {
			return nil
		}
		if err := r.ensure(size * 1.8); err != nil {
			return err
		}
		r.font(block.Bold, size)
		r.text(pageMargin, r.cursorY+size, wordmark, colour)
		r.cursorY += size * 1.8
		return nil
	}

	width, height, err := r.imageExtent(block.ImagePNG, defaultFloat(block.WidthMM, 38), block.HeightMM)
	if err != nil {
		return err
	}
	if err := r.ensure(height + 12); err != nil {
		return err
	}
	if err := r.drawImage(block.ImagePNG, pageMargin, r.cursorY, width, height); err != nil {
		return err
	}
	if wordmark != "" {
		r.font(block.Bold, size)
		baseline := r.cursorY + height/2 + size*0.35
		r.text(pageMargin+width+10, baseline, wordmark, colour)
	}
	r.cursorY += height + 12
	return nil
}

// heading desenha título, subtítulo e nome: uma linha, com alinhamento e tom.
func (r *renderer) heading(block Block, defaultSize float64, defaultTone string) error {
	size := defaultFloat(block.Size, defaultSize)
	if err := r.ensure(size * 1.6); err != nil {
		return err
	}
	r.font(block.Bold, size)
	r.text(r.alignedX(block.Align, r.lineWidth(block.Text)), r.cursorY+size, block.Text, r.tone(defaultString(block.Tone, defaultTone)))
	r.cursorY += size * 1.6
	return nil
}

// section desenha o título de uma seção ou cláusula, com respiro antes.
func (r *renderer) section(block Block) error {
	size := defaultFloat(block.Size, 11.5)
	if err := r.ensure(size*1.8 + 8); err != nil {
		return err
	}
	r.cursorY += 8
	r.font(true, size)
	r.text(pageMargin, r.cursorY+size, block.Text, r.tone(defaultString(block.Tone, ToneAccent)))
	r.cursorY += size * 1.8
	return nil
}

// paragraph desenha um parágrafo quebrado em linhas (`indent` recua a
// primeira linha, como os parágrafos únicos do contrato).
func (r *renderer) paragraph(block Block, indent bool) error {
	size := defaultFloat(block.Size, 11)
	indentWidth := 0.0
	if indent {
		indentWidth = 20
	}
	lines := r.wrap(block.Text, size, block.Bold, r.contentWidth()-indentWidth)
	lineHeight := size * lineSpacing
	if err := r.ensure(float64(len(lines))*lineHeight + 10); err != nil {
		return err
	}
	colour := r.tone(defaultString(block.Tone, ToneInk))
	r.font(block.Bold, size)
	for index, line := range lines {
		offset := 0.0
		if indent && index == 0 {
			offset = indentWidth
		}
		r.text(pageMargin+offset, r.cursorY+size+float64(index)*lineHeight, line, colour)
	}
	r.cursorY += float64(len(lines))*lineHeight + 10
	return nil
}

// label desenha o rótulo em negrito seguido do valor na mesma linha.
func (r *renderer) label(block Block) error {
	size := defaultFloat(block.Size, 10.5)
	if err := r.ensure(size * 1.8); err != nil {
		return err
	}
	colour := r.tone(defaultString(block.Tone, ToneInk))
	r.font(true, size)
	baseline := r.cursorY + size
	r.text(pageMargin, baseline, block.Text, colour)
	if value := strings.TrimSpace(block.Value); value != "" {
		r.font(false, size)
		r.text(pageMargin+r.lineWidth(block.Text)+4, baseline, value, colour)
	}
	r.cursorY += size * 1.8
	return nil
}

// metric desenha uma célula do resumo: rótulo pequeno sobre o valor.
func (r *renderer) metric(block Block) error {
	labelSize := 10.0
	valueSize := defaultFloat(block.Size, 14)
	height := labelSize*1.5 + valueSize*1.4 + 6
	if err := r.ensure(height); err != nil {
		return err
	}
	r.font(true, labelSize)
	r.text(pageMargin, r.cursorY+labelSize, block.Text, mutedColour)
	r.cursorY += labelSize * 1.5
	r.font(true, valueSize)
	r.text(pageMargin, r.cursorY+valueSize, block.Value, r.tone(defaultString(block.Tone, ToneInk)))
	r.cursorY += valueSize*1.4 + 6
	return nil
}

// rule desenha a linha horizontal de separação.
func (r *renderer) rule(block Block) error {
	if err := r.ensure(12); err != nil {
		return err
	}
	colour := borderColour
	if block.Tone == ToneAccent {
		colour = accentColour
	}
	r.doc.SetDrawColor(colour.r, colour.g, colour.b)
	r.doc.SetLineWidth(1)
	r.doc.Line(pageMargin, r.cursorY, pageMargin+r.contentWidth(), r.cursorY)
	r.cursorY += 12
	return nil
}

// image desenha uma imagem medida em milímetros, respeitando a proporção quando
// o modelo não informa a altura.
func (r *renderer) image(block Block) error {
	if len(block.ImagePNG) == 0 {
		return fmt.Errorf("bloco de imagem sem conteúdo")
	}
	width, height, err := r.imageExtent(block.ImagePNG, defaultFloat(block.WidthMM, 40), block.HeightMM)
	if err != nil {
		return err
	}
	if err := r.ensure(height + 10); err != nil {
		return err
	}
	x := pageMargin
	if block.Align == AlignCenter {
		x = pageMargin + (r.contentWidth()-width)/2
	}
	if err := r.drawImage(block.ImagePNG, x, r.cursorY, width, height); err != nil {
		return err
	}
	r.cursorY += height + 10
	return nil
}

// signature desenha uma linha de assinatura com o rótulo do signatário. As
// metades esquerda e direita consecutivas dividem a mesma linha da página; a
// metade central (`AlignCenter`) desenha uma linha única centralizada. Um bloco
// `signature-note` desenha só o rótulo, na coluna correspondente.
func (r *renderer) signature(block Block) error {
	size := defaultFloat(block.Size, 11)
	if r.signatureRow == nil {
		rowHeight := signatureRowHeight(size)
		if block.Kind == BlockSignatureNote {
			rowHeight = noteRowHeight
		}
		if err := r.ensure(rowHeight + 8); err != nil {
			return err
		}
		r.signatureRow = &signatureRowState{top: r.cursorY, height: rowHeight}
	}
	row := r.signatureRow
	rowY := row.top

	if block.Kind == BlockSignatureNote {
		r.font(block.Bold, size)
		r.text(r.columnX(block.Align), rowY+size+2, block.Text, r.tone(defaultString(block.Tone, ToneMuted)))
	} else {
		lineWidth := mmToPoints(defaultFloat(block.WidthMM, contractSignatureLineWidthMM))
		x := pageMargin + 10
		switch block.Align {
		case AlignCenter:
			x = pageMargin + (r.contentWidth()-lineWidth)/2
		case AlignRight:
			x = r.columnX(AlignRight)
		}
		r.doc.SetDrawColor(mutedColour.r, mutedColour.g, mutedColour.b)
		r.doc.SetLineWidth(1)
		r.doc.Line(x, rowY, x+lineWidth, rowY)
		r.font(block.Bold, size)
		r.text(x+(lineWidth-r.lineWidth(block.Text))/2, rowY+size+6, block.Text, inkColour)
	}

	// Só a metade esquerda deixa a linha aberta para a coluna da direita.
	if block.Align == AlignLeft {
		return nil
	}
	r.cursorY = rowY + row.height
	r.signatureRow = nil
	return nil
}

// flushSignatureRow fecha uma linha de assinatura que ficou só com a metade
// esquerda antes de outro bloco.
func (r *renderer) flushSignatureRow() {
	if r.signatureRow == nil {
		return
	}
	r.cursorY = r.signatureRow.top + r.signatureRow.height
	r.signatureRow = nil
}

// columnX devolve a abcissa da coluna de assinatura.
func (r *renderer) columnX(align string) float64 {
	if align == AlignRight {
		return r.width/2 + 10
	}
	return pageMargin + 10
}

// auth desenha o selo de autenticidade: QR Code, título, legenda, chave AUTH e
// URL pública de validação.
func (r *renderer) auth(block Block) error {
	size := defaultFloat(block.Size, contractQRSize)
	image, err := authz.QRCode(block.QR)
	if err != nil {
		return err
	}
	boxHeight := size + 16
	if err := r.ensure(boxHeight + 20); err != nil {
		return err
	}
	top := r.cursorY
	r.doc.SetFillColor(sealColour.r, sealColour.g, sealColour.b)
	r.doc.SetDrawColor(borderColour.r, borderColour.g, borderColour.b)
	r.doc.SetLineWidth(1)
	r.doc.Rect(pageMargin, top, r.contentWidth(), boxHeight, "DF")

	if err := r.drawImage(image, pageMargin+8, top+8, size, size); err != nil {
		return err
	}

	textX := pageMargin + size + 24
	colour := sealAccent
	if block.Text != "" {
		r.font(true, 9.5)
		r.text(textX, top+20, block.Text, colour)
	}
	if block.Value != "" {
		r.font(false, 8)
		r.text(textX, top+34, block.Value, inkColour)
	}
	if block.Code != "" {
		r.font(true, 8)
		r.text(textX, top+47, "Chave: "+block.Code, inkColour)
	}
	if block.QR != "" {
		r.font(false, 7)
		r.text(textX, top+59, block.QR, mutedColour)
	}

	r.cursorY = top + boxHeight + 14
	return nil
}

// standaloneQR desenha um QR Code isolado e centralizado.
func (r *renderer) standaloneQR(block Block) error {
	content := strings.TrimSpace(block.QR)
	if content == "" {
		content = strings.TrimSpace(block.Text)
	}
	image, err := authz.QRCode(content)
	if err != nil {
		return err
	}
	size := defaultFloat(block.Size, 74)
	if err := r.ensure(size + 12); err != nil {
		return err
	}
	if err := r.drawImage(image, pageMargin+(r.contentWidth()-size)/2, r.cursorY, size, size); err != nil {
		return err
	}
	r.cursorY += size + 12
	return nil
}

// drawImage registra e posiciona a imagem. O nome é o resumo do conteúdo, então
// a mesma imagem referenciada duas vezes é embutida uma única vez no arquivo.
func (r *renderer) drawImage(image []byte, x, y, width, height float64) error {
	name := fmt.Sprintf("img-%x", sha1.Sum(image))
	options := fpdf.ImageOptions{ImageType: "PNG"}
	r.doc.RegisterImageOptionsReader(name, options, bytes.NewReader(image))
	if err := r.doc.Error(); err != nil {
		return fmt.Errorf("registrar imagem: %w", err)
	}
	r.doc.ImageOptions(name, x, y, width, height, false, options, 0, "")
	if err := r.doc.Error(); err != nil {
		return fmt.Errorf("desenhar imagem: %w", err)
	}
	return nil
}

// imageExtent devolve a largura e a altura em pontos. As medidas do modelo
// mandam; quando falta uma delas, a proporção do arquivo completa a outra.
func (r *renderer) imageExtent(image []byte, widthMM, heightMM float64) (float64, float64, error) {
	width := mmToPoints(widthMM)
	height := mmToPoints(heightMM)
	if width > 0 && height > 0 {
		return width, height, nil
	}
	if width <= 0 && height <= 0 {
		return 0, 0, fmt.Errorf("bloco de imagem sem largura nem altura")
	}
	config, err := png.DecodeConfig(bytes.NewReader(image))
	if err != nil {
		return 0, 0, fmt.Errorf("medir a imagem: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 {
		return 0, 0, fmt.Errorf("imagem %dx%d sem dimensões", config.Width, config.Height)
	}
	ratio := float64(config.Height) / float64(config.Width)
	if width <= 0 {
		width = height / ratio
	} else {
		height = width * ratio
	}
	return width, height, nil
}

// wrap quebra o texto nas linhas que cabem na largura pedida, palavra por
// palavra (e, para palavra maior que a linha, caractere a caractere) — a mesma
// regra do app atual.
func (r *renderer) wrap(text string, size float64, bold bool, maxWidth float64) []string {
	r.font(bold, size)
	lines := make([]string, 0, 8)
	current := ""
	for _, word := range strings.Fields(text) {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if r.lineWidth(candidate) <= maxWidth {
			current = candidate
			continue
		}
		if current != "" {
			lines = append(lines, current)
			current = word
			continue
		}
		slice := ""
		for _, char := range word {
			next := slice + string(char)
			if slice == "" || r.lineWidth(next) <= maxWidth {
				slice = next
				continue
			}
			lines = append(lines, slice)
			slice = string(char)
		}
		current = slice
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		lines = append(lines, "")
	}
	return lines
}

// signatureRowHeight é o avanço vertical de uma linha de assinatura: a linha
// desenhada, o rótulo abaixo dela e a folga até a linha seguinte.
func signatureRowHeight(size float64) float64 { return size*1.6 + 34 }

// noteRowHeight é o avanço de um rótulo sem linha de assinatura.
const noteRowHeight = 20

// defaultFloat devolve value ou o padrão quando value não foi informado.
func defaultFloat(value, defaultValue float64) float64 {
	if value == 0 {
		return defaultValue
	}
	return value
}

// defaultString devolve value ou o padrão quando value está vazio.
func defaultString(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}
