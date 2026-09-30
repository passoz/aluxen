package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
	"github.com/passoz/aluxen/internal/infrastructure/buildinfo"
)

//go:embed templates/*.html templates/partials/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Renderer renderiza as páginas server-side. Os templates são parseados uma
// única vez no boot e ficam imutáveis.
type Renderer struct {
	templates *template.Template
}

// PageData é o contexto base de toda página renderizada.
type PageData struct {
	Title     string
	Page      string
	CSRFToken string
	Build     buildinfo.Info
	AppURL    string
	Theme     string

	// Content é o HTML já renderizado do conteúdo principal (as abas do painel
	// são renderizadas separadamente e injetadas aqui).
	Content template.HTML

	// Data é o contexto específico da página (tipado por template).
	Data any
}

// NewRenderer parseia os templates embutidos.
func NewRenderer() (*Renderer, error) {
	funcs := template.FuncMap{
		"money":        format.FormatCurrency,
		"moneyText":    format.FormatCurrencyFromString,
		"moneyValue":   domain.FormatFloat,
		"date":         format.FormatDateBR,
		"datetime":     format.FormatDateTimeBR,
		"leaseStatus":  labelOf[domain.LeaseStatus](domain.LeaseStatusLabel),
		"category":     labelOf[domain.ReceiptCategory](domain.ReceiptCategoryLabel),
		"receiptState": labelOf[domain.ReceiptStatus](domain.ReceiptStatusLabel),
		"receiptStatus": func(receipt domain.Receipt) string {
			return domain.ReceiptStatusLabel(domain.ReceiptStatusOf(receipt, time.Now()))
		},
		"receiptStatusCode": func(receipt domain.Receipt) string {
			return string(domain.ReceiptStatusOf(receipt, time.Now()))
		},
		"depositStatus": labelOf[domain.DepositStatus](domain.DepositStatusLabel),
		"hasDeposit": func(lease domain.Lease) bool {
			return domain.HasDeposit(lease.DepositAmount, lease.DepositStatus)
		},
		"entityLabel": domain.EntityLabel,
		"actionLabel": func(action string) string { return domain.AuditActionMetaFor(action).Label },
		"originLabel": func(origin string) string { return domain.AuditOriginLabel(domain.AuditOrigin(origin)) },
		"metadataLabel": func(key string) string {
			return domain.MetadataLabel(key)
		},
		"deref": func(value *string) string { return domain.Str(value) },
		"dict": func(values ...any) map[string]any {
			dict := make(map[string]any, len(values)/2)
			for index := 0; index+1 < len(values); index += 2 {
				key, _ := values[index].(string)
				dict[key] = values[index+1]
			}
			return dict
		},
		"json": func(value any) template.JS {
			encoded, err := json.Marshal(value)
			if err != nil {
				return template.JS("null")
			}
			return template.JS(encoded)
		},
		"percent": func(value, total float64) int {
			if total <= 0 {
				return 0
			}
			percentage := int(math.Round(value / total * 100))
			if percentage < 0 {
				return 0
			}
			if percentage > 100 {
				return 100
			}
			return percentage
		},
		"statusColor": func(status string) string {
			switch status {
			case string(domain.ReceiptStatusPaid):
				return "#10b981"
			case string(domain.ReceiptStatusPartial):
				return "#3b82f6"
			case string(domain.ReceiptStatusDue):
				return "#f59e0b"
			case string(domain.ReceiptStatusOverdue):
				return "#ef4444"
			default:
				return "#999999"
			}
		},
		"weekdayName": func(weekday int) string {
			names := []string{"domingo", "segunda-feira", "terça-feira", "quarta-feira", "quinta-feira", "sexta-feira", "sábado"}
			if weekday < 0 || weekday >= len(names) {
				return ""
			}
			return names[weekday]
		},
		"hasPrefix": strings.HasPrefix,
		"upper":     strings.ToUpper,
		"add":       func(a, b int) int { return a + b },
		"seq": func(from, to int) []int {
			values := make([]int, 0, to-from+1)
			for value := from; value <= to; value++ {
				values = append(values, value)
			}
			return values
		},
		// As funções abaixo desenham os gráficos da aba de indicadores em SVG.
		// A aritmética de escala fica fora do template para que ele apenas
		// posicione os elementos.
		"svgNum":            svgNum,
		"metricValueFont":   metricValueFont,
		"categoryColor":     categoryColor,
		"donutSlices":       donutSlices,
		"monthlyChart":      monthlyChart,
		"categoryBars":      categoryBars,
		"occupancyBars":     occupancyBars,
		"occupancyTip":      occupancyTip,
		"dashboardCardTips": dashboardCardTips,
		// leaseStatusSummary reproduz o texto do badge de contratos do app
		// original: os totais por status que existem, na ordem ativo, suspenso e
		// temporada; sem nenhum deles, o total de contratos.
		"leaseStatusSummary": func(leases []domain.Lease) string {
			counts := map[domain.LeaseStatus]int{}
			for _, lease := range leases {
				counts[lease.Status]++
			}
			parts := make([]string, 0, 3)
			for _, entry := range []struct {
				status domain.LeaseStatus
				format string
			}{
				{domain.LeaseStatusActive, "%d ativo(s)"},
				{domain.LeaseStatusSuspended, "%d suspenso(s)"},
				{domain.LeaseStatusSeason, "%d temporada(s)"},
			} {
				if total := counts[entry.status]; total > 0 {
					parts = append(parts, fmt.Sprintf(entry.format, total))
				}
			}
			if len(parts) == 0 {
				return fmt.Sprintf("%d contrato(s)", len(leases))
			}
			return strings.Join(parts, " • ")
		},
		// moneyPtr formata um valor opcional como moeda, tratando o nulo como
		// zero (o app original faz `valor || 0`).
		"moneyPtr": func(value *float64) string {
			if value == nil {
				return format.FormatCurrency(0)
			}
			return format.FormatCurrency(*value)
		},
		// receiptRemaining é o que falta pagar de um lançamento parcial; nunca
		// negativo (pagamento a mais não vira crédito na tela).
		"receiptRemaining": func(receipt domain.Receipt) float64 {
			if receipt.PaidAmount == nil {
				return receipt.Amount
			}
			remaining := receipt.Amount - *receipt.PaidAmount
			if remaining < 0 {
				return 0
			}
			return remaining
		},
		// moneyValuePtr formata um valor opcional no formato decimal do
		// formulário (`2500.00`), deixando o campo vazio quando não há valor.
		"moneyValuePtr": func(value *float64) string {
			if value == nil {
				return ""
			}
			return domain.FormatFloat(*value)
		},
		// recentYears é a janela de anos do filtro de lançamentos: o ano
		// corrente e os dois de cada lado, como no app original.
		"recentYears": func() []int {
			current := time.Now().Year()
			years := make([]int, 0, 5)
			for year := current - 2; year <= current+2; year++ {
				years = append(years, year)
			}
			return years
		},
	}

	templates, err := template.New("aluxen").Funcs(funcs).ParseFS(templateFS, "templates/*.html", "templates/partials/*.html")
	if err != nil {
		return nil, fmt.Errorf("parsear templates: %w", err)
	}
	return &Renderer{templates: templates}, nil
}

// Render executa o template da página com o status informado.
func (r *Renderer) Render(w http.ResponseWriter, status int, page string, data *PageData) error {
	var buffer bytes.Buffer
	if err := r.templates.ExecuteTemplate(&buffer, page, data); err != nil {
		return fmt.Errorf("renderizar %s: %w", page, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(buffer.Bytes()); err != nil {
		return fmt.Errorf("escrever resposta de %s: %w", page, err)
	}
	return nil
}

// labelOf converte um valor de template (o tipo do domínio ou uma string) no
// rótulo em português. Aceita os dois porque o mesmo rótulo é usado a partir de
// entidades e de campos já convertidos para texto.
func labelOf[T ~string](label func(T) string) func(any) string {
	return func(value any) string {
		switch typed := value.(type) {
		case T:
			return label(typed)
		case string:
			return label(T(typed))
		default:
			return fmt.Sprint(value)
		}
	}
}

// ── Gráficos de indicadores ──────────────────────────────────────────────
// A aba de indicadores desenha os gráficos em SVG no template. Para que o
// template apenas posicione os elementos, a escala das séries e os atributos
// de traço ficam calculados nestas funções.

// svgNum formata um número para uso em atributos de SVG: menor representação
// decimal possível e sem zero negativo (o JavaScript também imprime "0").
func svgNum(value float64) string {
	if value == 0 {
		return "0"
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// truncateText encurta o texto ao limite de runas informado, marcando o corte
// com reticências. A contagem é em runas para não partir acentos.
func truncateText(value string, limit int) string {
	if limit < 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

// barWidth devolve a largura percentual de um valor dentro de uma série
// escalada pelo maior valor dela: a maior barra ocupa a barra inteira. Uma
// série zerada não divide por zero.
func barWidth(value, max float64) string {
	if max <= 0 {
		max = 1
	}
	return svgNum(value / max * 100)
}

// metricValueFont devolve o tamanho de fonte do valor de um cartão de
// indicador: valores em moeda ou longos precisam de fonte menor para caber.
func metricValueFont(value string) string {
	if strings.HasPrefix(value, "R$") || utf8.RuneCountInString(value) > 5 {
		return "1.35rem"
	}
	return "1.75rem"
}

// categoryColor devolve a cor da categoria de recibo nas barras por categoria.
func categoryColor(category string) string {
	switch category {
	case string(domain.ReceiptCategoryExtra):
		return "#14b8a6"
	default:
		return "#6366f1"
	}
}

// categoryBar é uma linha do gráfico de recibos por categoria. Tip é o texto
// do tooltip da barra.
type categoryBar struct {
	Category string
	Label    string
	Count    int
	Total    string
	Width    string
	Tip      string
}

// categoryBars monta as barras por categoria, escaladas pelo maior total do
// conjunto: a categoria de maior valor ocupa a barra inteira.
func categoryBars(categories []application.MetricsCategory) []categoryBar {
	max := 1.0
	grand := 0.0
	for _, category := range categories {
		if category.Total > max {
			max = category.Total
		}
		grand += category.Total
	}
	bars := make([]categoryBar, 0, len(categories))
	for _, category := range categories {
		total := format.FormatCurrency(category.Total)
		bars = append(bars, categoryBar{
			Category: category.Category,
			Label:    category.Label,
			Count:    category.Count,
			Total:    total,
			Width:    barWidth(category.Total, max),
			Tip: fmt.Sprintf("%s · %d recibo(s) · %s (%d%% do total)",
				category.Label, category.Count, total, tipShare(category.Total, grand)),
		})
	}
	return bars
}

// occupancyBar é uma linha do gráfico de ocupação por imóvel.
type occupancyBar struct {
	ID     string
	Name   string
	Title  string
	Width  string
	Leases string
}

// occupancyBars monta as barras de ocupação: mantém os primeiros imóveis do
// recorte, encurta o nome para caber na coluna e escala a barra pelo imóvel
// mais ocupado do recorte.
func occupancyBars(properties []application.MetricsProperty, limit int) []occupancyBar {
	if limit <= 0 || limit > len(properties) {
		limit = len(properties)
	}
	properties = properties[:limit]

	max := 1.0
	for _, property := range properties {
		if leases := float64(property.ActiveLeases); leases > max {
			max = leases
		}
	}
	bars := make([]occupancyBar, 0, len(properties))
	for _, property := range properties {
		bars = append(bars, occupancyBar{
			ID:     property.ID,
			Name:   truncateText(property.Name, 30),
			Title:  property.Name,
			Width:  barWidth(float64(property.ActiveLeases), max),
			Leases: fmt.Sprintf("%d contrato(s)", property.ActiveLeases),
		})
	}
	return bars
}

// donutSlice é uma fatia do gráfico de rosca com o traço já calculado. Tip é
// o texto do tooltip da fatia.
type donutSlice struct {
	DashArray  string
	DashOffset float64
	Tip        string
}

// donutView é a rosca pronta para o template: o total das contagens e as
// fatias na ordem de desenho.
type donutView struct {
	Total  int
	Slices []donutSlice
}

// donutSlices distribui as contagens sobre a circunferência de uma rosca do
// raio informado. Uma série sem contagens devolve total zero e nenhuma fatia.
func donutSlices(status []application.MetricsStatus, radius float64) donutView {
	total := 0
	for _, entry := range status {
		total += entry.Count
	}
	if total == 0 {
		return donutView{}
	}

	circumference := 2 * math.Pi * radius
	slices := make([]donutSlice, 0, len(status))
	offset := 0.0
	for _, entry := range status {
		length := float64(entry.Count) / float64(total) * circumference
		slices = append(slices, donutSlice{
			DashArray:  svgNum(length) + " " + svgNum(circumference-length),
			DashOffset: offset,
			Tip: fmt.Sprintf("%s · %d recibo(s) (%d%%)",
				entry.Label, entry.Count, tipShare(float64(entry.Count), float64(total))),
		})
		offset -= length
	}
	return donutView{Total: total, Slices: slices}
}

// chartLine é uma linha dos eixos do gráfico, em unidades do viewBox.
type chartLine struct {
	X1, Y1, X2, Y2 float64
}

// chartGuide é uma linha de grade do gráfico com o rótulo de valor.
type chartGuide struct {
	X1, X2 float64
	Y      float64
	LabelX float64
	LabelY float64
	Label  string
}

// chartColumn é uma coluna do gráfico de barras verticais: o esperado fica
// atrás e o recebido à frente. Tip é o texto do tooltip da coluna; From e To
// são os limites do mês para o recorte do detalhamento.
type chartColumn struct {
	Label     string
	X         float64
	Width     float64
	ExpectedY float64
	ExpectedH float64
	ReceivedY float64
	ReceivedH float64
	LabelX    float64
	LabelY    float64
	Tip       string
	From      string
	To        string
}

// chartView é a geometria de um gráfico de barras verticais.
type chartView struct {
	Width   float64
	Height  float64
	Axis    []chartLine
	Guides  []chartGuide
	Columns []chartColumn
}

// monthlyChart posiciona eixos, linhas de grade e colunas do gráfico de
// recebimentos mensais no viewBox usado pela tela. A escala usa o maior valor
// recebido ou esperado do período.
func monthlyChart(months []application.MetricsMonth) chartView {
	const (
		width     = 600.0
		height    = 200.0
		padTop    = 16.0
		padRight  = 16.0
		padBottom = 28.0
		padLeft   = 42.0
	)

	baseline := height - padBottom
	view := chartView{
		Width:  width,
		Height: height,
		Axis: []chartLine{
			{X1: padLeft, Y1: baseline, X2: width - padRight, Y2: baseline},
			{X1: padLeft, Y1: padTop, X2: padLeft, Y2: baseline},
		},
	}

	chartW := width - padLeft - padRight
	chartH := height - padTop - padBottom

	max := 1.0
	for _, month := range months {
		if value := math.Max(month.Received, month.Expected); value > max {
			max = value
		}
	}
	slot := chartW / math.Max(float64(len(months)), 1)
	columnWidth := math.Max(12, slot-16)

	view.Columns = make([]chartColumn, 0, len(months))
	for index, month := range months {
		x := padLeft + slot*float64(index) + (slot-columnWidth)/2
		expected := month.Expected / max * chartH
		received := month.Received / max * chartH
		from, to := monthBounds(month.Key)
		view.Columns = append(view.Columns, chartColumn{
			Label:     month.Label,
			X:         x,
			Width:     columnWidth,
			ExpectedY: baseline - expected,
			ExpectedH: expected,
			ReceivedY: baseline - received,
			ReceivedH: received,
			LabelX:    x + columnWidth/2,
			LabelY:    height - 8,
			Tip: fmt.Sprintf("%s · recebido %s de %s esperados · %s em aberto",
				month.Label, format.FormatCurrency(month.Received),
				format.FormatCurrency(month.Expected), format.FormatCurrency(month.Open)),
			From: from,
			To:   to,
		})
	}

	for _, ratio := range []float64{0, 0.5, 1} {
		y := baseline - chartH*ratio
		view.Guides = append(view.Guides, chartGuide{
			X1:     padLeft,
			X2:     width - padRight,
			Y:      y,
			LabelX: padLeft - 4,
			LabelY: y + 4,
			Label:  format.FormatCurrency(max * ratio),
		})
	}
	return view
}

// ── Tooltips do painel ───────────────────────────────────────────────────
// Todo texto de tooltip é montado aqui, no servidor, a partir do instantâneo
// que o template já recebe. O JS só posiciona e mostra o elemento: nenhum
// número é montado no navegador (Work 0007, BR-703). A composição acrescenta
// informação em vez de repetir o valor impresso: todo tooltip junta as partes
// com " · " ou declara a ausência com "nenhum".

// cardTips são os oito textos dos cartões de indicador do painel.
type cardTips struct {
	Tenants    string
	Properties string
	Receipts   string
	Received   string
	Open       string
	Overdue    string
	Expected   string
	Total      string
}

// dashboardCardTips monta os textos dos cartões a partir do instantâneo. A
// situação do lançamento usa a mesma regra do resto do painel
// (domain.ReceiptStatusOf, que normaliza a referência para a meia-noite).
func dashboardCardTips(snapshot *application.DashboardData) cardTips {
	tips := cardTips{
		Tenants:    "nenhum inquilino cadastrado",
		Properties: "nenhum imóvel cadastrado",
		Receipts:   "nenhum lançamento",
		Received:   "nenhum lançamento pago ainda",
		Open:       "nenhum lançamento a vencer",
		Overdue:    "nenhum lançamento vencido",
		Expected:   "nenhum contrato ativo",
		Total:      "nenhum lançamento",
	}
	if snapshot == nil {
		return tips
	}

	now := time.Now()
	activeTenants := map[string]bool{}
	occupiedProperties := map[string]bool{}
	activeLeases := 0
	for _, lease := range snapshot.Leases {
		if lease.Status != domain.LeaseStatusActive {
			continue
		}
		activeLeases++
		activeTenants[lease.TenantID] = true
		occupiedProperties[lease.PropertyID] = true
	}

	if len(snapshot.Tenants) > 0 {
		tips.Tenants = fmt.Sprintf("%d cadastrado(s) · %d com contrato ativo", len(snapshot.Tenants), len(activeTenants))
	}
	if len(snapshot.Properties) > 0 {
		free := len(snapshot.Properties) - len(occupiedProperties)
		tips.Properties = fmt.Sprintf("%d imóvel(is) · %d ocupado(s) · %d livre(s)", len(snapshot.Properties), len(occupiedProperties), free)
	}

	paidCount, partialCount, dueCount, overdueCount := 0, 0, 0, 0
	paidSum := 0.0
	earliestDue := ""
	oldestOverdue := ""
	for _, receipt := range snapshot.Receipts {
		switch domain.ReceiptStatusOf(receipt, now) {
		case domain.ReceiptStatusPaid:
			paidCount++
			paidSum += tipPaidAmount(receipt)
		case domain.ReceiptStatusPartial:
			partialCount++
			paidSum += tipPaidAmount(receipt)
		case domain.ReceiptStatusDue:
			dueCount++
			if earliestDue == "" || receipt.DueDate < earliestDue {
				earliestDue = receipt.DueDate
			}
		case domain.ReceiptStatusOverdue:
			overdueCount++
			if oldestOverdue == "" || receipt.DueDate < oldestOverdue {
				oldestOverdue = receipt.DueDate
			}
		}
	}

	if len(snapshot.Receipts) > 0 {
		parts := []string{}
		if paidCount > 0 {
			parts = append(parts, fmt.Sprintf("%d pago(s)", paidCount))
		}
		if partialCount > 0 {
			parts = append(parts, fmt.Sprintf("%d parcial(is)", partialCount))
		}
		if dueCount > 0 {
			parts = append(parts, fmt.Sprintf("%d a vencer", dueCount))
		}
		if overdueCount > 0 {
			parts = append(parts, fmt.Sprintf("%d vencido(s)", overdueCount))
		}
		tips.Receipts = fmt.Sprintf("%d lançamento(s) · %s", len(snapshot.Receipts), strings.Join(parts, " · "))
		tips.Total = fmt.Sprintf("%d lançamento(s) · %s pago(s) (%d%% do total)",
			len(snapshot.Receipts), format.FormatCurrency(paidSum), tipShare(paidSum, snapshot.Metrics.Totals.TotalAmount))
	}
	if paidCount+partialCount > 0 {
		tips.Received = fmt.Sprintf("%s · recebido de %d recibo(s) pago(s)", format.FormatCurrency(paidSum), paidCount+partialCount)
	}
	if dueCount > 0 {
		tips.Open = fmt.Sprintf("%d a vencer · primeiro vence em %s", dueCount, format.FormatDateBR(earliestDue))
	}
	if overdueCount > 0 {
		tips.Overdue = fmt.Sprintf("%d vencido(s) · mais antigo desde %s (%d dias)",
			overdueCount, format.FormatDateBR(oldestOverdue), tipDaysOverdue(oldestOverdue, now))
	}
	if activeLeases > 0 {
		average := 0.0
		if snapshot.Metrics.Totals.ExpectedMonthly > 0 {
			average = snapshot.Metrics.Totals.ExpectedMonthly / float64(activeLeases)
		}
		tips.Expected = fmt.Sprintf("%d contrato(s) ativo(s) · ticket médio %s", activeLeases, format.FormatCurrency(average))
	}
	return tips
}

// tipPaidAmount devolve o valor efetivamente pago do lançamento, com a mesma
// convenção da agregação de indicadores: valor explícito quando há, total
// quando a baixa não informa valor.
func tipPaidAmount(receipt domain.Receipt) float64 {
	if receipt.PaidAmount != nil {
		return *receipt.PaidAmount
	}
	if receipt.PaidAt != nil && *receipt.PaidAt != "" {
		return receipt.Amount
	}
	return 0
}

// tipShare devolve o percentual inteiro de value sobre total, limitado a 0-100.
func tipShare(value, total float64) int {
	if total <= 0 {
		return 0
	}
	share := int(math.Round(value / total * 100))
	if share < 0 {
		return 0
	}
	if share > 100 {
		return 100
	}
	return share
}

// tipDaysOverdue devolve há quantos dias o vencimento passou, contando da
// meia-noite de hoje até a meia-noite do vencimento.
func tipDaysOverdue(dueDate string, now time.Time) int {
	due, err := time.ParseInLocation("2006-01-02", tipDatePart(dueDate), now.Location())
	if err != nil {
		return 0
	}
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	days := int(midnight.Sub(due).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// tipDatePart extrai a parte de data (aaaa-mm-dd) do vencimento.
func tipDatePart(value string) string {
	if len(value) >= 10 {
		return value[:10]
	}
	return value
}

// occupancyTip monta o texto da barra de ocupação do imóvel: o nome inteiro,
// quantos contratos ativos ele tem e quem ocupa. Imóvel sem contrato ativo
// declara a ausência em vez de repetir o zero impresso na linha.
func occupancyTip(bar occupancyBar, leases []domain.Lease) string {
	names := []string{}
	active := 0
	for _, lease := range leases {
		if lease.PropertyID != bar.ID || lease.Status != domain.LeaseStatusActive {
			continue
		}
		active++
		if lease.TenantName != "" {
			names = append(names, lease.TenantName)
		}
	}
	if active == 0 {
		return bar.Title + " · nenhum contrato ativo"
	}
	return fmt.Sprintf("%s · %d contrato(s) ativo(s) · %s", bar.Title, active, strings.Join(names, ", "))
}

// monthBounds devolve o primeiro e o último dia do mês da competência
// (aaaa-mm), para o recorte de período do detalhamento. Chave fora do formato
// devolve limites vazios, e o recorte os ignora.
func monthBounds(key string) (string, string) {
	first, err := time.Parse("2006-01", key)
	if err != nil {
		return "", ""
	}
	last := first.AddDate(0, 1, -1)
	return first.Format("2006-01-02"), last.Format("2006-01-02")
}

// Has informa se o template existe. Permite que áreas independentes publiquem
// suas abas sem quebrar a renderização das demais.
func (r *Renderer) Has(name string) bool {
	return r.templates.Lookup(name) != nil
}

// Execute renderiza um template (aba, modal, linha de tabela) e devolve o HTML.
func (r *Renderer) Execute(name string, data any) (template.HTML, error) {
	var buffer bytes.Buffer
	if err := r.templates.ExecuteTemplate(&buffer, name, data); err != nil {
		return "", fmt.Errorf("renderizar %s: %w", name, err)
	}
	return template.HTML(buffer.String()), nil
}

// Os tipos que a stdlib não conhece precisam ser registrados: sem isso o
// manifest do PWA sai como text/plain e o navegador reclama.
func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	_ = mime.AddExtensionType(".webp", "image/webp")
}

// StaticHandler serve os arquivos embutidos de static/.
func StaticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(fmt.Sprintf("static embutido inválido: %v", err))
	}
	return http.FileServer(http.FS(sub))
}
