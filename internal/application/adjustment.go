package application

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Reajuste anual assistido (tier 2).
//
// O cálculo é determinístico e acontece aqui: novo valor = valor atual ×
// (1 + índice/100), arredondado a duas casas. O modelo de IA, quando existe,
// apenas redige a notificação a partir do valor JÁ calculado — ele nunca
// calcula, nunca lê o banco e nunca escolhe a vigência. A aplicação grava o
// novo aluguel no contrato e deixa a linha correspondente na trilha.

// AdjustmentIndex é o índice oficial aplicado ao reajuste do aluguel.
type AdjustmentIndex string

const (
	AdjustmentIndexIGPM AdjustmentIndex = "igpm"
	AdjustmentIndexIPCA AdjustmentIndex = "ipca"
)

// adjustmentIndices são os índices aceitos, na ordem exibida no painel.
var adjustmentIndices = []AdjustmentIndex{AdjustmentIndexIGPM, AdjustmentIndexIPCA}

// AdjustmentIndexLabel devolve o rótulo em português do índice.
func AdjustmentIndexLabel(index AdjustmentIndex) string {
	switch index {
	case AdjustmentIndexIGPM:
		return "IGP-M"
	case AdjustmentIndexIPCA:
		return "IPCA"
	default:
		return string(index)
	}
}

// ParseAdjustmentIndex normaliza o índice informado no painel. Valor fora do
// vocabulário fechado é recusado — não existe "índice livre" no reajuste.
func ParseAdjustmentIndex(raw string) (AdjustmentIndex, error) {
	normalized := AdjustmentIndex(strings.ToLower(strings.TrimSpace(raw)))
	for _, index := range adjustmentIndices {
		if normalized == index {
			return index, nil
		}
	}
	return "", domain.Invalid("Escolha o índice do reajuste (IGP-M ou IPCA).")
}

// AdjustmentIndexOption é uma opção do seletor de índice da aba de reajuste.
type AdjustmentIndexOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// AdjustmentIndexOptions devolve os índices aceitos para o formulário.
func AdjustmentIndexOptions() []AdjustmentIndexOption {
	options := make([]AdjustmentIndexOption, 0, len(adjustmentIndices))
	for _, index := range adjustmentIndices {
		options = append(options, AdjustmentIndexOption{Value: string(index), Label: AdjustmentIndexLabel(index)})
	}
	return options
}

// AdjustmentPreviewPayload é a prévia do reajuste de um contrato: o percentual
// aplicado, o valor anterior, o valor novo e a vigência.
//
// O nome difere do caso de uso (`AdjustmentPreview`) porque Go não permite tipo
// e função com o mesmo nome no pacote.
type AdjustmentPreviewPayload struct {
	LeaseID      string  `json:"leaseId"`
	TenantName   string  `json:"tenantName"`
	PropertyName string  `json:"propertyName"`
	Index        string  `json:"index"`
	IndexLabel   string  `json:"indexLabel"`
	Rate         float64 `json:"rate"`
	// RateLabel é o percentual já formatado para leitura (4,5%).
	RateLabel           string  `json:"rateLabel"`
	PreviousAmount      float64 `json:"previousAmount"`
	PreviousAmountLabel string  `json:"previousAmountLabel"`
	NewAmount           float64 `json:"newAmount"`
	NewAmountLabel      string  `json:"newAmountLabel"`
	Difference          float64 `json:"difference"`
	DifferenceLabel     string  `json:"differenceLabel"`
	EffectiveDate       string  `json:"effectiveDate"`
	EffectiveDateLabel  string  `json:"effectiveDateLabel"`
}

// AdjustmentPreview calcula o reajuste do contrato sem gravar nada. Recusa
// contrato encerrado, percentual zerado ou negativo, índice desconhecido e
// vigência inválida.
func AdjustmentPreview(ctx context.Context, deps Deps, leaseID string, index AdjustmentIndex, rate float64, effectiveDate string) (*AdjustmentPreviewPayload, error) {
	lease, normalized, effective, err := adjustmentTarget(ctx, deps, leaseID, index, rate, effectiveDate)
	if err != nil {
		return nil, err
	}
	preview := adjustmentPreviewOf(lease, normalized, rate, effective)
	return &preview, nil
}

// AdjustmentApplyInput são os campos da aplicação do reajuste. Confirm precisa
// vir marcado: sem a confirmação explícita do painel nada é gravado.
type AdjustmentApplyInput struct {
	LeaseID       string
	Index         AdjustmentIndex
	Rate          float64
	EffectiveDate string
	Confirm       bool
}

// Origens do texto da notificação devolvida na aplicação.
const (
	// AdjustmentNoticeDeterministic é a carta montada pelo sistema.
	AdjustmentNoticeDeterministic = "deterministico"
	// AdjustmentNoticeAI é a carta redigida pelo provedor de IA a partir dos
	// números já calculados.
	AdjustmentNoticeAI = "ia"
)

// AdjustmentResult é o desfecho da aplicação: a prévia calculada e o texto da
// notificação formal, pronto para copiar ou enviar.
type AdjustmentResult struct {
	Preview      AdjustmentPreviewPayload `json:"preview"`
	Notice       string                   `json:"notice"`
	NoticeSource string                   `json:"noticeSource"`
}

// ApplyAdjustment grava o novo aluguel do contrato, registra o reajuste na
// trilha e devolve a notificação formal já redigida.
//
// O valor gravado vem sempre do cálculo determinístico desta função; o provedor
// de IA, quando configurado, só redige o texto.
func ApplyAdjustment(ctx context.Context, deps Deps, actx AuditContext, input AdjustmentApplyInput) (*AdjustmentResult, error) {
	if !input.Confirm {
		return nil, domain.Invalid("Confirme o reajuste para aplicar.")
	}

	lease, index, effective, err := adjustmentTarget(ctx, deps, input.LeaseID, input.Index, input.Rate, input.EffectiveDate)
	if err != nil {
		return nil, err
	}
	preview := adjustmentPreviewOf(lease, index, input.Rate, effective)

	after := *lease
	after.RentAmount = preview.NewAmount
	after.UpdatedAt = format.FormatUTCStamp(deps.Clock.Now())
	if err := deps.Repos.Leases.Update(ctx, &after); err != nil {
		return nil, fmt.Errorf("aplicar reajuste no contrato: %w", err)
	}

	updated, err := deps.Repos.Leases.ByID(ctx, lease.ID)
	if err != nil {
		return nil, fmt.Errorf("buscar contrato reajustado: %w", err)
	}
	label := DescribeLease(updated)
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionAdjustmentApplied,
		Entity:      domain.EntityLease,
		EntityID:    updated.ID,
		EntityLabel: label,
		Summary: fmt.Sprintf("Aluguel de %s reajustado pelo %s (%s), de %s para %s, a partir de %s",
			label, preview.IndexLabel, preview.RateLabel, preview.PreviousAmountLabel, preview.NewAmountLabel, preview.EffectiveDateLabel),
		Changes: domain.BuildAuditChanges(domain.LeaseAuditFields, LeaseSnapshot(lease), LeaseSnapshot(updated)),
		Metadata: map[string]any{
			"index":         preview.Index,
			"rate":          preview.Rate,
			"effectiveDate": preview.EffectiveDate,
			"amount":        preview.NewAmount,
		},
	}); err != nil {
		return nil, err
	}

	result := &AdjustmentResult{
		Preview:      preview,
		Notice:       adjustmentNotice(preview, landlordName(ctx, deps)),
		NoticeSource: AdjustmentNoticeDeterministic,
	}
	if written, ok := adjustmentAINotice(ctx, deps, preview); ok {
		result.Notice = written
		result.NoticeSource = AdjustmentNoticeAI
	}
	return result, nil
}

// adjustmentTarget valida contrato, índice, percentual e vigência e devolve o
// contrato carregado e a vigência normalizada.
func adjustmentTarget(ctx context.Context, deps Deps, leaseID string, index AdjustmentIndex, rate float64, effectiveDate string) (*domain.Lease, AdjustmentIndex, string, error) {
	id := strings.TrimSpace(leaseID)
	if id == "" {
		return nil, "", "", domain.Invalid("Selecione o contrato do reajuste.")
	}
	normalized, err := ParseAdjustmentIndex(string(index))
	if err != nil {
		return nil, "", "", err
	}
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate <= 0 {
		return nil, "", "", domain.Invalid("Informe um percentual de reajuste maior que zero.")
	}
	effective, err := adjustmentDate(effectiveDate)
	if err != nil {
		return nil, "", "", err
	}

	lease, err := deps.Repos.Leases.ByID(ctx, id)
	if err != nil {
		return nil, "", "", err
	}
	if lease.Status == domain.LeaseStatusTerminated {
		return nil, "", "", domain.Invalid("Contrato encerrado não pode ser reajustado.")
	}
	if lease.RentAmount <= 0 {
		return nil, "", "", domain.Invalid("O contrato não tem valor de aluguel para reajustar.")
	}
	return lease, normalized, effective, nil
}

// adjustmentPreviewOf monta a prévia a partir do contrato e dos parâmetros já
// validados.
func adjustmentPreviewOf(lease *domain.Lease, index AdjustmentIndex, rate float64, effectiveDate string) AdjustmentPreviewPayload {
	newAmount := adjustmentAmount(lease.RentAmount, rate)
	return AdjustmentPreviewPayload{
		LeaseID:             lease.ID,
		TenantName:          lease.TenantName,
		PropertyName:        lease.PropertyName,
		Index:               string(index),
		IndexLabel:          AdjustmentIndexLabel(index),
		Rate:                rate,
		RateLabel:           adjustmentRateLabel(rate),
		PreviousAmount:      lease.RentAmount,
		PreviousAmountLabel: format.FormatCurrency(lease.RentAmount),
		NewAmount:           newAmount,
		NewAmountLabel:      format.FormatCurrency(newAmount),
		Difference:          adjustmentRound(newAmount - lease.RentAmount),
		DifferenceLabel:     format.FormatCurrency(adjustmentRound(newAmount - lease.RentAmount)),
		EffectiveDate:       effectiveDate,
		EffectiveDateLabel:  format.FormatDateBR(effectiveDate),
	}
}

// adjustmentAmount aplica o percentual ao valor atual e arredonda a duas casas.
// O arredondamento é o comercial (meio para cima) e acontece em centavos, para
// que o valor gravado seja exatamente o valor apresentado.
func adjustmentAmount(current, rate float64) float64 {
	return adjustmentRound(current * (1 + rate/100))
}

// adjustmentRound arredonda um valor em reais para duas casas decimais.
func adjustmentRound(value float64) float64 {
	return math.Round(value*100) / 100
}

// adjustmentRateLabel formata o percentual para leitura (4,5%, 4%, 0,4125%).
func adjustmentRateLabel(rate float64) string {
	text := strconv.FormatFloat(rate, 'f', -1, 64)
	return strings.ReplaceAll(text, ".", ",") + "%"
}

// adjustmentDate normaliza a vigência para YYYY-MM-DD. Aceita o formato ISO do
// formulário e o formato brasileiro digitado à mão.
func adjustmentDate(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", domain.Invalid("Informe a data de vigência do reajuste.")
	}
	if len(trimmed) >= len("2006-01-02") {
		candidate := trimmed[:len("2006-01-02")]
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			return candidate, nil
		}
	}
	if parts := strings.Split(trimmed, "/"); len(parts) == 3 {
		day, dayErr := strconv.Atoi(parts[0])
		month, monthErr := strconv.Atoi(parts[1])
		year, yearErr := strconv.Atoi(parts[2])
		if dayErr == nil && monthErr == nil && yearErr == nil {
			candidate := fmt.Sprintf("%04d-%02d-%02d", year, month, day)
			if _, err := time.Parse("2006-01-02", candidate); err == nil {
				return candidate, nil
			}
		}
	}
	return "", domain.Invalid("Informe uma data de vigência válida.")
}

// ── Notificação formal ─────────────────────────────────────────────

// adjustmentNotice monta a carta determinística do reajuste: qualificação das
// partes, índice, percentual, valor novo e vigência.
func adjustmentNotice(preview AdjustmentPreviewPayload, landlord string) string {
	var builder strings.Builder
	builder.WriteString("Notificação de reajuste de aluguel\n\n")

	tenant := strings.TrimSpace(preview.TenantName)
	if tenant == "" {
		builder.WriteString("Prezado(a) locatário(a),\n\n")
	} else {
		builder.WriteString("Prezado(a) " + tenant + ",\n\n")
	}

	property := strings.TrimSpace(preview.PropertyName)
	builder.WriteString("Na qualidade de locador(a)")
	if property != "" {
		builder.WriteString(" do imóvel " + property)
	}
	builder.WriteString(", notificamos o reajuste do aluguel pelo índice " + preview.IndexLabel +
		", correspondente a " + preview.RateLabel + ".\n\n")
	builder.WriteString("O valor mensal passa de " + preview.PreviousAmountLabel + " para " + preview.NewAmountLabel +
		", a partir de " + preview.EffectiveDateLabel + ".\n\n")
	builder.WriteString("O novo valor será cobrado nos vencimentos a partir da vigência indicada.\n\n")
	builder.WriteString("Atenciosamente,\n")
	if name := strings.TrimSpace(landlord); name != "" {
		builder.WriteString(name + "\n")
	}
	return builder.String()
}

// adjustmentNoticeSystem é a instrução do provedor de IA na redação da carta.
const adjustmentNoticeSystem = "Você redige notificações formais de reajuste de aluguel em português do Brasil. " +
	"Você não calcula nada: use exclusivamente os números, o índice e a data informados. " +
	"Responda apenas com o texto da notificação, sem comentários."

// adjustmentAINotice pede ao provedor para redigir a notificação a partir do
// valor já calculado. Sem provedor — ou se a redação falhar ou não citar os
// números calculados — a carta determinística continua valendo.
func adjustmentAINotice(ctx context.Context, deps Deps, preview AdjustmentPreviewPayload) (string, bool) {
	if deps.Chat == nil {
		return "", false
	}

	base := adjustmentNotice(preview, landlordName(ctx, deps))
	response, err := deps.Chat.Complete(ctx, ChatRequest{
		System: adjustmentNoticeSystem,
		Messages: []ChatMessage{{
			Role: "user",
			Content: "Dados já calculados do reajuste (não altere nenhum número):\n" +
				"- Inquilino: " + preview.TenantName + "\n" +
				"- Imóvel: " + preview.PropertyName + "\n" +
				"- Índice: " + preview.IndexLabel + "\n" +
				"- Percentual: " + preview.RateLabel + "\n" +
				"- Valor anterior: " + preview.PreviousAmountLabel + "\n" +
				"- Valor novo: " + preview.NewAmountLabel + "\n" +
				"- Vigência: " + preview.EffectiveDateLabel + "\n\n" +
				"Modelo de referência:\n" + base,
		}},
		Temperature: 0.2,
	})
	if err != nil {
		deps.Log.Warn("falha ao redigir a notificação do reajuste", "scope", "adjustment", "lease_id", preview.LeaseID, "err", err)
		return "", false
	}

	written := strings.TrimSpace(response.Message)
	if written == "" || !strings.Contains(written, preview.NewAmountLabel) || !strings.Contains(written, preview.EffectiveDateLabel) {
		deps.Log.Warn("notificação redigida não cita os números calculados; mantendo o texto determinístico",
			"scope", "adjustment", "lease_id", preview.LeaseID)
		return "", false
	}
	return written, true
}

// ── Aba de reajuste ────────────────────────────────────────────────

// adjustmentHistoryLimit limita quantos eventos de reajuste a aba carrega.
const adjustmentHistoryLimit = 200

// AdjustmentHistory é um reajuste já aplicado, lido da trilha.
type AdjustmentHistory struct {
	ID                  string `json:"id"`
	AppliedAt           string `json:"appliedAt"`
	AppliedAtLabel      string `json:"appliedAtLabel"`
	Summary             string `json:"summary"`
	IndexLabel          string `json:"indexLabel"`
	RateLabel           string `json:"rateLabel"`
	EffectiveDateLabel  string `json:"effectiveDateLabel"`
	PreviousAmountLabel string `json:"previousAmountLabel"`
	NewAmountLabel      string `json:"newAmountLabel"`
}

// AdjustmentLeaseRow é um contrato na aba de reajuste, com o valor atual e o
// histórico de reajustes.
type AdjustmentLeaseRow struct {
	ID              string              `json:"id"`
	TenantName      string              `json:"tenantName"`
	PropertyName    string              `json:"propertyName"`
	TenantPhone     string              `json:"tenantPhone"`
	Status          domain.LeaseStatus  `json:"status"`
	StatusLabel     string              `json:"statusLabel"`
	RentAmount      float64             `json:"rentAmount"`
	RentAmountLabel string              `json:"rentAmountLabel"`
	StartDate       string              `json:"startDate"`
	EndDate         string              `json:"endDate"`
	Adjustable      bool                `json:"adjustable"`
	History         []AdjustmentHistory `json:"history"`
}

// AdjustmentView é o contexto da aba de reajuste.
type AdjustmentView struct {
	Today    string                  `json:"today"`
	MonthKey string                  `json:"monthKey"`
	Indices  []AdjustmentIndexOption `json:"indices"`
	Leases   []AdjustmentLeaseRow    `json:"leases"`
}

// BuildAdjustmentView monta a aba de reajuste: os contratos com o valor atual,
// o telefone do titular (para o envio da notificação) e o histórico de reajustes
// lido da trilha de auditoria.
func BuildAdjustmentView(ctx context.Context, deps Deps) (*AdjustmentView, error) {
	leases, err := ListLeases(ctx, deps)
	if err != nil {
		return nil, err
	}
	tenants, err := ListTenants(ctx, deps)
	if err != nil {
		return nil, err
	}

	history, err := adjustmentHistory(ctx, deps)
	if err != nil {
		return nil, err
	}

	phones := make(map[string]string, len(tenants))
	for _, tenant := range tenants {
		phones[tenant.ID] = domain.Str(tenant.Phone)
	}

	rows := make([]AdjustmentLeaseRow, 0, len(leases))
	for i := range leases {
		lease := leases[i]
		row := AdjustmentLeaseRow{
			ID:              lease.ID,
			TenantName:      lease.TenantName,
			PropertyName:    lease.PropertyName,
			TenantPhone:     phones[lease.TenantID],
			Status:          lease.Status,
			StatusLabel:     domain.LeaseStatusLabel(lease.Status),
			RentAmount:      lease.RentAmount,
			RentAmountLabel: format.FormatCurrency(lease.RentAmount),
			StartDate:       lease.StartDate,
			EndDate:         lease.EndDate,
			Adjustable:      lease.Status != domain.LeaseStatusTerminated && lease.RentAmount > 0,
			History:         history[lease.ID],
		}
		if row.History == nil {
			row.History = []AdjustmentHistory{}
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(a, b int) bool {
		if rows[a].Status == domain.LeaseStatusTerminated != (rows[b].Status == domain.LeaseStatusTerminated) {
			return rows[b].Status == domain.LeaseStatusTerminated
		}
		if rows[a].TenantName != rows[b].TenantName {
			return rows[a].TenantName < rows[b].TenantName
		}
		return rows[a].PropertyName < rows[b].PropertyName
	})

	return &AdjustmentView{
		Today:    format.FormatLocalDate(deps.Clock.Now()),
		MonthKey: deps.Clock.Now().Format("2006-01"),
		Indices:  AdjustmentIndexOptions(),
		Leases:   rows,
	}, nil
}

// adjustmentHistory agrupa os reajustes já aplicados por contrato, do mais
// recente para o mais antigo.
func adjustmentHistory(ctx context.Context, deps Deps) (map[string][]AdjustmentHistory, error) {
	entries, err := deps.Repos.Audit.List(ctx, domain.AuditFilter{
		Entity:     domain.EntityLease,
		Action:     domain.ActionAdjustmentApplied,
		Descending: true,
		Limit:      adjustmentHistoryLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("listar reajustes da trilha: %w", err)
	}

	grouped := make(map[string][]AdjustmentHistory, len(entries))
	for i := range entries {
		entry := auditRowFrom(&entries[i])
		leaseID := domain.Str(entry.EntityID)
		if leaseID == "" {
			continue
		}
		grouped[leaseID] = append(grouped[leaseID], adjustmentHistoryOf(entry))
	}
	return grouped, nil
}

// adjustmentHistoryOf traduz uma linha da trilha no reajuste exibido na aba. Os
// números vêm do diff já formatado e dos metadados do evento.
func adjustmentHistoryOf(entry AuditRow) AdjustmentHistory {
	history := AdjustmentHistory{
		ID:             entry.ID,
		AppliedAt:      entry.CreatedAt,
		AppliedAtLabel: format.FormatDateTimeBR(entry.CreatedAt),
		Summary:        entry.Summary,
		IndexLabel:     AdjustmentIndexLabel(AdjustmentIndex(adjustmentMetadataString(entry.Metadata, "index"))),
		RateLabel:      adjustmentMetadataRate(entry.Metadata),
	}
	if effective := adjustmentMetadataString(entry.Metadata, "effectiveDate"); effective != "" {
		history.EffectiveDateLabel = format.FormatDateBR(effective)
	}
	for _, change := range entry.Changes {
		switch change.Field {
		case "rentAmount":
			history.PreviousAmountLabel = domain.Str(change.From)
			history.NewAmountLabel = domain.Str(change.To)
		}
	}
	return history
}

// adjustmentMetadataString lê um metadado de texto do evento da trilha.
func adjustmentMetadataString(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return text
}

// adjustmentMetadataRate formata o percentual gravado nos metadados.
func adjustmentMetadataRate(metadata map[string]any) string {
	switch typed := metadata["rate"].(type) {
	case float64:
		return adjustmentRateLabel(typed)
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return ""
		}
		return adjustmentRateLabel(parsed)
	default:
		return ""
	}
}
