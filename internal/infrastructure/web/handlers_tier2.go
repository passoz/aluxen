package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Tier 2 assistido: reajuste anual e leitura dos números (Work 0005).
//
//	GET  /api/report/monthly      — relatório do mês (JSON)
//	POST /api/report/ask          — pergunta em linguagem natural (tool-calling tipado)
//	POST /api/adjustment/preview  — calcula o reajuste de um contrato
//	POST /api/adjustment/apply    — aplica o reajuste e registra na trilha
//
// O handler é adaptador fino: lê a requisição, chama o caso de uso e devolve
// JSON. Nenhum cálculo de reajuste, nenhuma agregação de relatório e nenhum
// acesso a banco vivem aqui — tudo isso está em internal/application.
//
// As mutações aceitam o formulário do painel (urlencoded, CSRF no corpo) e um
// corpo JSON com os mesmos nomes de campo (CSRF na query, como nas demais rotas
// do painel).

// maxTier2BodyBytes limita o corpo das requisições de tier 2.
const maxTier2BodyBytes = 64 << 10

// tier2ReadError é a mensagem única de corpo ilegível.
const tier2ReadError = "Não foi possível ler os dados enviados."

// ReportMonthly devolve o relatório da competência pedida. Sem `year`/`month` na
// query, a competência é o mês corrente.
func (h *Handlers) ReportMonthly(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	year, month, err := tier2YearMonth(r.URL.Query().Get("year"), r.URL.Query().Get("month"), h.Deps.Clock.Now())
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	report, err := application.MonthlyReport(r.Context(), h.Deps, year, month)
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "report": report})
}

// ReportAsk responde à pergunta em linguagem natural pelas ferramentas tipadas.
// Sem provedor de IA configurado a resposta é 503 com mensagem clara.
func (h *Handlers) ReportAsk(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	fields, err := tier2Fields(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": tier2ReadError})
		return
	}

	answer, err := application.AskReport(r.Context(), h.Deps, fields.Question)
	if errors.Is(err, application.ErrAskUnavailable) {
		h.failJSONOrPage(w, r, http.StatusServiceUnavailable, "A consulta em linguagem natural não está configurada nesta instância.")
		return
	}
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "answer": answer})
}

// AdjustmentPreview calcula o reajuste sem gravar nada.
func (h *Handlers) AdjustmentPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	fields, err := tier2Fields(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": tier2ReadError})
		return
	}
	input, err := tier2AdjustmentInput(fields)
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	preview, err := application.AdjustmentPreview(r.Context(), h.Deps, input.LeaseID, input.Index, input.Rate, input.EffectiveDate)
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "preview": preview})
}

// AdjustmentApply aplica o reajuste e devolve a notificação formal. A aplicação
// exige a confirmação explícita do painel (`confirm`).
func (h *Handlers) AdjustmentApply(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	fields, err := tier2Fields(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": tier2ReadError})
		return
	}
	input, err := tier2AdjustmentInput(fields)
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	result, err := application.ApplyAdjustment(r.Context(), h.Deps, h.auditContext(r, domain.AuditOriginWeb), input)
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

// adjustmentTabData monta o contexto da aba de reajuste: os contratos com o
// valor atual e o histórico de reajustes lido da trilha.
func (h *Handlers) adjustmentTabData(ctx context.Context) (any, error) {
	return application.BuildAdjustmentView(ctx, h.Deps)
}

// tier2Request reúne os campos aceitos nas rotas de tier 2, do formulário do
// painel ou de um corpo JSON com os mesmos nomes.
type tier2Request struct {
	Question      string       `json:"question"`
	LeaseID       string       `json:"lease_id"`
	Index         string       `json:"index"`
	Rate          string       `json:"rate"`
	EffectiveDate string       `json:"effective_date"`
	Confirm       tier2Confirm `json:"confirm"`
}

// tier2Confirm aceita a confirmação como booleano (JSON) ou como texto (o
// formulário envia "1").
type tier2Confirm bool

func (c *tier2Confirm) UnmarshalJSON(raw []byte) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*c = tier2Confirm(tier2Truthy(value))
	return nil
}

// tier2Truthy interpreta a confirmação em qualquer das formas aceitas.
func tier2Truthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	case string:
		return formApprove(typed)
	default:
		return false
	}
}

// tier2Fields lê os campos do formulário do painel ou de um corpo JSON.
func tier2Fields(r *http.Request) (tier2Request, error) {
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		var payload tier2Request
		if err := json.NewDecoder(io.LimitReader(r.Body, maxTier2BodyBytes)).Decode(&payload); err != nil {
			return tier2Request{}, err
		}
		return payload, nil
	}
	if err := r.ParseForm(); err != nil {
		return tier2Request{}, err
	}
	return tier2Request{
		Question:      r.FormValue("question"),
		LeaseID:       r.FormValue("lease_id"),
		Index:         r.FormValue("index"),
		Rate:          r.FormValue("rate"),
		EffectiveDate: r.FormValue("effective_date"),
		Confirm:       tier2Confirm(formApprove(r.FormValue("confirm"))),
	}, nil
}

// tier2AdjustmentInput traduz os campos do painel no caso de uso do reajuste.
func tier2AdjustmentInput(fields tier2Request) (application.AdjustmentApplyInput, error) {
	index, err := application.ParseAdjustmentIndex(fields.Index)
	if err != nil {
		return application.AdjustmentApplyInput{}, err
	}
	rate, err := tier2Rate(fields.Rate)
	if err != nil {
		return application.AdjustmentApplyInput{}, err
	}
	return application.AdjustmentApplyInput{
		LeaseID:       fields.LeaseID,
		Index:         index,
		Rate:          rate,
		EffectiveDate: fields.EffectiveDate,
		Confirm:       bool(fields.Confirm),
	}, nil
}

// tier2Rate interpreta o percentual do reajuste. O painel envia o valor já
// normalizado; a máscara pt-BR e o sinal de porcento também são aceitos.
func tier2Rate(raw string) (float64, error) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(raw, "%", ""))
	if trimmed == "" {
		return 0, domain.Invalid("Informe o percentual do reajuste.")
	}
	if strings.Contains(trimmed, ",") {
		trimmed = strings.ReplaceAll(trimmed, ".", "")
		trimmed = strings.ReplaceAll(trimmed, ",", ".")
	}
	rate, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, domain.Invalid("Informe um percentual de reajuste válido.")
	}
	return rate, nil
}

// tier2YearMonth lê a competência da query. Campo ausente cai para o mês
// corrente; campo presente e ilegível é erro — relatório pedido nunca devolve
// silenciosamente outro mês.
func tier2YearMonth(rawYear, rawMonth string, now time.Time) (int, int, error) {
	year, month := now.Year(), int(now.Month())
	if trimmed := strings.TrimSpace(rawYear); trimmed != "" {
		parsed, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, 0, domain.Invalid("Informe um ano e um mês válidos.")
		}
		year = parsed
	}
	if trimmed := strings.TrimSpace(rawMonth); trimmed != "" {
		parsed, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, 0, domain.Invalid("Informe um ano e um mês válidos.")
		}
		month = parsed
	}
	return year, month, nil
}
