package web

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Painel administrativo: casca das abas, modo JSON e roteamento dos intents de
// mutação. Cada área implementa o seu intent em handlers_*.go; aqui só há
// despacho, carregamento de dados e renderização da casca.

// dashboardTabs são as abas válidas do painel.
var dashboardTabs = []string{"dashboard", "tenants", "properties", "leases", "receipts", "season", "calendar", "review", "intake", "conversations", "adjustment", "history", "metrics"}

// tabTemplate é o template de conteúdo de cada aba. Áreas que ainda não
// publicaram o template apenas não renderizam conteúdo (o painel não quebra).
var tabTemplate = map[string]string{
	"dashboard":     "tab_dashboard",
	"tenants":       "tab_tenants",
	"properties":    "tab_properties",
	"leases":        "tab_leases",
	"receipts":      "tab_receipts",
	"season":        "tab_season",
	"calendar":      "tab_calendar",
	"review":        "tab_review",
	"intake":        "tab_intake",
	"conversations": "tab_conversations",
	"adjustment":    "tab_adjustment",
	"history":       "tab_history",
	"metrics":       "tab_metrics",
}

// Dashboard renderiza o painel na aba pedida (ou devolve JSON com json=true).
func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}

	snapshot, err := application.DashboardSnapshot(r.Context(), h.Deps)
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"user":       map[string]any{"id": user.ID, "username": user.Username},
			"tenants":    tenantsJSON(snapshot.Tenants),
			"properties": propertiesJSON(snapshot.Properties),
			"leases":     leasesJSON(snapshot.Leases),
			"receipts":   receiptsJSON(snapshot.Receipts),
			"metrics":    snapshot.Metrics,
		})
		return
	}

	tab := normalizeTab(r.URL.Query().Get("tab"))
	tabContext := &tabData{
		User:       user,
		Snapshot:   snapshot,
		Tab:        tab,
		Month:      r.URL.Query().Get("month"),
		Year:       r.URL.Query().Get("year"),
		Status:     r.URL.Query().Get("status"),
		Toast:      r.URL.Query().Get("toast"),
		CSRFToken:  h.CSRF.Token(w, r),
		RequestURL: r.URL.String(),
	}
	// Cada área carrega o contexto da SUA aba: o painel não paga I/O de tela que
	// o locador não abriu, e um recurso não configurado não derruba as demais.
	if err := h.loadTabContext(r, tabContext); err != nil {
		h.mapError(w, r, err)
		return
	}

	content, err := h.renderTab(tab, tabContext)
	if err != nil {
		h.Log.Error("falha ao renderizar aba do painel", "scope", "http", "tab", tab, "err", err)
		h.renderError(w, r, http.StatusInternalServerError, "Erro interno do servidor.")
		return
	}

	data := &PageData{
		Title:     "Aluxen | Painel",
		Page:      tab,
		CSRFToken: content.CSRFToken,
		Build:     h.Build,
		AppURL:    h.Cfg.AppURL,
		Content:   content.HTML,
		Data:      content,
	}
	if err := h.Render.Render(w, http.StatusOK, "dashboard.html", data); err != nil {
		h.Log.Error("falha ao renderizar painel", "scope", "http", "err", err)
		h.renderError(w, r, http.StatusInternalServerError, "Erro interno do servidor.")
	}
}

// loadTabContext carrega o contexto específico da aba aberta.
func (h *Handlers) loadTabContext(r *http.Request, data *tabData) error {
	ctx := r.Context()
	var err error

	switch data.Tab {
	case "season":
		data.Season, err = application.BuildSeasonOverview(ctx, h.Deps)
	case "calendar":
		year, month := parseCalendarQuery(r.URL.Query().Get("year"), r.URL.Query().Get("month"), h.Deps.Clock.Now())
		data.Calendar, err = application.BuildCalendar(ctx, h.Deps, year, month)
	case "review":
		data.Review, err = application.ListPaymentProofReview(ctx, h.Deps, 50)
	case "intake":
		data.Intake, err = h.intakeTabData(ctx)
	case "conversations":
		data.Conversations, err = h.conversationsTabData(ctx)
	case "adjustment":
		data.Adjustments, err = h.adjustmentTabData(ctx)
	}
	if err != nil {
		return err
	}
	return nil
}

// tabData é o contexto entregue ao template da aba.
type tabData struct {
	User     *domain.User
	Snapshot *application.DashboardData
	Season   *application.SeasonOverview
	Calendar *application.CalendarView
	Tab      string
	// Review é a fila de comprovantes em revisão humana.
	Review []application.PaymentProofRow
	// Intake é a lista de cadastros propostos a partir de documento.
	Intake any
	// Conversations e Adjustments são os contextos das abas de mensageria e de
	// reajuste, montados por quem é dono de cada área.
	Conversations any
	Adjustments   any
	Month         string
	Year          string
	Status        string
	Toast         string
	CSRFToken     string
	RequestURL    string
	HTML          template.HTML
}

// renderTab executa o template da aba, quando ele existe.
func (h *Handlers) renderTab(tab string, data *tabData) (*tabData, error) {
	name, ok := tabTemplate[tab]
	if !ok || !h.Render.Has(name) {
		data.HTML = ""
		return data, nil
	}
	html, err := h.Render.Execute(name, data)
	if err != nil {
		return nil, err
	}
	data.HTML = html
	return data, nil
}

// parseCalendarQuery lê ano e mês do calendário, caindo para o mês corrente.
func parseCalendarQuery(rawYear, rawMonth string, now time.Time) (int, int) {
	year, err := strconv.Atoi(strings.TrimSpace(rawYear))
	if err != nil || year < 1970 || year > 9999 {
		year = now.Year()
	}
	month, err := strconv.Atoi(strings.TrimSpace(rawMonth))
	if err != nil || month < 1 || month > 12 {
		month = int(now.Month())
	}
	return year, month
}

func normalizeTab(tab string) string {
	for _, candidate := range dashboardTabs {
		if tab == candidate {
			return candidate
		}
	}
	return "dashboard"
}

// DashboardMutation despacha os intents de mutação do painel.
func (h *Handlers) DashboardMutation(w http.ResponseWriter, r *http.Request) {
	user, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Não foi possível ler os dados enviados."})
		return
	}

	intent := strings.TrimSpace(r.FormValue("intent"))
	var err error
	switch {
	case strings.HasPrefix(intent, "tenant."), strings.HasPrefix(intent, "property."):
		err = h.catalogIntent(r, user, intent)
	case strings.HasPrefix(intent, "lease."):
		err = h.leaseIntent(r, user, intent)
	case strings.HasPrefix(intent, "receipt."):
		err = h.receiptIntent(r, user, intent)
	default:
		err = domain.Invalid("Ação inválida.")
	}

	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Metrics devolve os indicadores agregados.
func (h *Handlers) Metrics(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	payload, err := application.ComputeMetrics(r.Context(), h.Deps)
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}
