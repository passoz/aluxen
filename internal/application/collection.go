package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Régua de cobrança: decide, pelo atraso do lançamento e pelo histórico de
// envios já registrados na trilha, qual estágio cobrar hoje, e envia o texto do
// estágio pelo canal do WhatsApp.
//
// Todas as decisões são determinísticas e vêm do recibo: o estágio sai dos dias
// de atraso, o valor e o vencimento saem do lançamento, e o texto de cada
// estágio é fixo. O modelo de linguagem não participa da cobrança — a redação
// assistida é opcional e nunca substitui o número do banco.
//
// `PlanCollection` só lê; `RunCollection` envia e registra. Recibo pago (ou
// ainda a vencer) nunca entra na régua: o pagamento interrompe a cobrança.

const (
	// CollectionChannelWhatsApp é o canal do envio entregue ao WhatsApp.
	CollectionChannelWhatsApp = "whatsapp"
	// CollectionChannelPending é o canal do envio apenas registrado: a instância
	// não tem canal montado, então a mensagem fica pendente no histórico da
	// conversa em vez de sair.
	CollectionChannelPending = "pending"

	// collectionHistoryLimit é o teto de eventos `collection.sent` lidos da
	// trilha para derivar o último estágio enviado de cada recibo.
	collectionHistoryLimit = 500

	// maintenanceDescriptionLimit é o teto da descrição guardada na trilha.
	maintenanceDescriptionLimit = 300
)

// CollectionCandidate é um lançamento que a régua cobra hoje, já com o estágio
// decidido e o texto montado (é o que o painel mostra na simulação).
type CollectionCandidate struct {
	ReceiptID      string                 `json:"receiptId"`
	TenantID       string                 `json:"tenantId"`
	TenantName     string                 `json:"tenantName"`
	PropertyName   string                 `json:"propertyName"`
	Phone          string                 `json:"phone"`
	DueDate        string                 `json:"dueDate"`
	Amount         float64                `json:"amount"`
	DaysLate       int                    `json:"daysLate"`
	Stage          domain.CollectionStage `json:"stage"`
	StageLabel     string                 `json:"stageLabel"`
	LastStage      domain.CollectionStage `json:"lastStage"`
	LastStageLabel string                 `json:"lastStageLabel,omitempty"`
	Message        string                 `json:"message"`
	// LandlordName assina a mensagem; não vai para o JSON porque o painel já o
	// mostra no texto.
	LandlordName string `json:"-"`
}

// CollectionOutcome é o desfecho do estágio de um lançamento na rodada.
type CollectionOutcome struct {
	ReceiptID  string                 `json:"receiptId"`
	TenantID   string                 `json:"tenantId"`
	TenantName string                 `json:"tenantName"`
	Stage      domain.CollectionStage `json:"stage"`
	StageLabel string                 `json:"stageLabel"`
	Channel    string                 `json:"channel,omitempty"`
	Delivered  bool                   `json:"delivered"`
	DryRun     bool                   `json:"dryRun"`
	Reason     string                 `json:"reason,omitempty"`
	Message    string                 `json:"message"`
	Amount     float64                `json:"amount"`
	DueDate    string                 `json:"dueDate"`
}

// PlanCollection devolve o que a régua cobraria hoje: um candidato por
// lançamento vencido e ainda não pago cujo estágio sustentado pelo atraso seja
// maior que o último estágio já enviado para ele.
func PlanCollection(ctx context.Context, deps Deps, today time.Time) ([]CollectionCandidate, error) {
	receipts, err := deps.Repos.Receipts.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listar lançamentos da régua: %w", err)
	}
	history, err := collectionHistory(ctx, deps)
	if err != nil {
		return nil, err
	}

	candidates := make([]CollectionCandidate, 0, len(receipts))
	for index := range receipts {
		receipt := &receipts[index]
		if domain.ReceiptStatusOf(*receipt, today) != domain.ReceiptStatusOverdue {
			continue
		}
		stage, ok := domain.CollectionStageByDelay(lateDays(receipt.DueDate, today))
		if !ok {
			continue
		}
		last := history[receipt.ID]
		if stage <= last {
			// O estágio que o atraso sustenta já foi enviado: a régua não repete o
			// mesmo texto e o inquilino não recebe duas cobranças iguais.
			continue
		}
		candidate, err := collectionCandidate(ctx, deps, receipt, stage, last, today)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}

	// Lançamento mais antigo primeiro, como a fila da cobrança.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].DueDate != candidates[j].DueDate {
			return candidates[i].DueDate < candidates[j].DueDate
		}
		return candidates[i].ReceiptID < candidates[j].ReceiptID
	})
	return candidates, nil
}

// RunCollection avança a régua sem canal de envio: o que a régua decidir fica
// registrado como envio pendente. É o caminho da instância sem WhatsApp
// configurado — e o que os testes usam para exercitar a decisão sem rede.
func RunCollection(ctx context.Context, deps Deps, actx AuditContext, dryRun bool) ([]CollectionOutcome, error) {
	return RunCollectionWithChannel(ctx, deps, nil, actx, dryRun)
}

// RunCollectionWithChannel avança a régua entregando cada estágio pelo canal
// informado (nulo = sem canal). Com `dryRun`, nada é enviado e nada é
// registrado: o retorno é a simulação do que sairia.
func RunCollectionWithChannel(ctx context.Context, deps Deps, channel WhatsAppChannel, actx AuditContext, dryRun bool) ([]CollectionOutcome, error) {
	candidates, err := PlanCollection(ctx, deps, deps.Clock.Now())
	if err != nil {
		return nil, err
	}

	outcomes := make([]CollectionOutcome, 0, len(candidates))
	for _, candidate := range candidates {
		outcomes = append(outcomes, deliverCollection(ctx, deps, channel, actx, candidate, dryRun))
	}
	return outcomes, nil
}

// deliverCollection entrega (ou registra) o estágio de um candidato. Falha de
// canal não derruba a rodada: o desfecho volta com o motivo e o lançamento
// continua na régua para a próxima execução.
func deliverCollection(ctx context.Context, deps Deps, channel WhatsAppChannel, actx AuditContext, candidate CollectionCandidate, dryRun bool) CollectionOutcome {
	outcome := CollectionOutcome{
		ReceiptID:  candidate.ReceiptID,
		TenantID:   candidate.TenantID,
		TenantName: candidate.TenantName,
		Stage:      candidate.Stage,
		StageLabel: candidate.StageLabel,
		Message:    candidate.Message,
		Amount:     candidate.Amount,
		DueDate:    candidate.DueDate,
		DryRun:     dryRun,
	}

	if dryRun {
		outcome.Reason = "Simulação: nenhuma mensagem enviada."
		return outcome
	}
	if format.NormalizeDigits(candidate.Phone) == "" {
		outcome.Reason = "Titular sem telefone cadastrado."
		deps.Log.Warn("cobrança sem destinatário",
			"scope", "collection", "receipt", candidate.ReceiptID,
			"tenant", candidate.TenantID, "stage", int(candidate.Stage))
		return outcome
	}

	switch {
	case channel == nil:
		if err := registerPendingCollection(ctx, deps, candidate); err != nil {
			deps.Log.Warn("envio pendente da cobrança não registrado",
				"scope", "collection", "receipt", candidate.ReceiptID, "err", err)
			outcome.Reason = "Não foi possível registrar o envio pendente."
			return outcome
		}
		outcome.Channel = CollectionChannelPending
		outcome.Reason = "Canal do WhatsApp indisponível: envio registrado como pendente."
	default:
		if _, err := channel.SendText(ctx, candidate.Phone, candidate.Message); err != nil {
			recordCollectionFailure(ctx, deps, actx, candidate, err)
			outcome.Reason = "Falha no envio pelo WhatsApp."
			return outcome
		}
		outcome.Channel = CollectionChannelWhatsApp
		outcome.Delivered = true
	}

	recordCollectionSent(ctx, deps, actx, candidate, outcome.Channel)
	return outcome
}

// CollectionMessage monta o texto do estágio: determinístico, com valor e
// vencimento vindos do recibo. Nenhum número nasce fora do lançamento.
func CollectionMessage(candidate CollectionCandidate) string {
	dress := fmt.Sprintf("Olá%s", greetingName(candidate.TenantName))
	amount := format.FormatCurrency(candidate.Amount)
	dueDate := format.FormatDateBR(candidate.DueDate)
	late := lateDaysLabel(candidate.DaysLate)

	var text string
	switch candidate.Stage {
	case domain.CollectionStageFirm:
		text = fmt.Sprintf(
			"%s, o aluguel com vencimento em %s, no valor de %s, segue em aberto (%s) e ainda não identificamos o pagamento. Pedimos a regularização. Se já pagou, envie o comprovante por aqui.",
			dress, dueDate, amount, late)
	case domain.CollectionStageInstallment:
		text = fmt.Sprintf(
			"%s, o aluguel com vencimento em %s, no valor de %s, está em aberto (%s). Podemos combinar o pagamento em partes para regularizar: responda esta mensagem com a data que funciona para você.",
			dress, dueDate, amount, late)
	default:
		text = fmt.Sprintf(
			"%s, passando para lembrar do aluguel com vencimento em %s, no valor de %s, ainda em aberto (%s). Se o pagamento já foi feito, por favor desconsidere esta mensagem.",
			dress, dueDate, amount, late)
	}

	if candidate.LandlordName != "" {
		text += "\n\n" + candidate.LandlordName
	}
	return text
}

// ── Auxiliares ─────────────────────────────────────────────────────

// collectionCandidate reúne o candidato: o titular (para o telefone), os valores
// do recibo e o texto do estágio.
func collectionCandidate(ctx context.Context, deps Deps, receipt *domain.Receipt, stage, last domain.CollectionStage, today time.Time) (CollectionCandidate, error) {
	tenant, err := receiptTenant(ctx, deps, receipt)
	if err != nil {
		return CollectionCandidate{}, err
	}

	candidate := CollectionCandidate{
		ReceiptID:    receipt.ID,
		TenantID:     domain.Str(receipt.TenantID),
		TenantName:   receipt.TenantName,
		PropertyName: receipt.PropertyName,
		DueDate:      receipt.DueDate,
		Amount:       receipt.Amount,
		DaysLate:     lateDays(receipt.DueDate, today),
		Stage:        stage,
		StageLabel:   stage.Label(),
		LastStage:    last,
		LandlordName: landlordName(ctx, deps),
	}
	if tenant != nil {
		candidate.TenantID = tenant.ID
		candidate.Phone = domain.Str(tenant.Phone)
		if candidate.TenantName == "" {
			candidate.TenantName = tenant.Name
		}
	}
	if last.Valid() {
		candidate.LastStageLabel = last.Label()
	}
	candidate.Message = CollectionMessage(candidate)
	return candidate, nil
}

// collectionHistory deriva o último estágio já enviado de cada lançamento. A
// trilha é a memória da régua: o `entity_id` do evento `collection.sent` é o id
// do recibo e o metadado `stage` diz até onde a cobrança chegou.
func collectionHistory(ctx context.Context, deps Deps) (map[string]domain.CollectionStage, error) {
	history := map[string]domain.CollectionStage{}
	if deps.Repos.Audit == nil {
		return history, nil
	}
	entries, err := deps.Repos.Audit.List(ctx, domain.AuditFilter{
		Action:     domain.ActionCollectionSent,
		Descending: true,
		Limit:      collectionHistoryLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("listar cobranças enviadas: %w", err)
	}
	for index := range entries {
		entry := &entries[index]
		receiptID := domain.Str(entry.EntityID)
		if receiptID == "" {
			continue
		}
		stage, ok := collectionEntryStage(entry.Metadata)
		if !ok {
			continue
		}
		if current, seen := history[receiptID]; !seen || stage > current {
			history[receiptID] = stage
		}
	}
	return history, nil
}

// collectionEntryStage lê o estágio do metadado do evento. Metadado ilegível é
// descartado: nunca vira estágio por adivinhação.
func collectionEntryStage(metadata *string) (domain.CollectionStage, bool) {
	if metadata == nil || strings.TrimSpace(*metadata) == "" {
		return 0, false
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(*metadata), &fields); err != nil {
		return 0, false
	}
	value, ok := fields["stage"].(float64)
	if !ok {
		return 0, false
	}
	return domain.CollectionStageFromValue(int(value))
}

// registerPendingCollection grava o envio que não pôde sair como mensagem
// pendente da conversa: o locador vê o que a régua tentou dizer e a régua não
// repete o estágio.
func registerPendingCollection(ctx context.Context, deps Deps, candidate CollectionCandidate) error {
	if deps.Repos.WhatsApp == nil {
		return errors.New("persistência das mensagens do canal não configurada")
	}
	metadata, err := json.Marshal(map[string]any{
		"stage":     int(candidate.Stage),
		"channel":   CollectionChannelPending,
		"receiptId": candidate.ReceiptID,
		"amount":    candidate.Amount,
	})
	if err != nil {
		return err
	}
	encoded := string(metadata)

	message := &domain.WhatsAppMessage{
		ID:        deps.IDs.New(),
		CreatedAt: format.FormatUTCStamp(deps.Clock.Now()),
		Direction: domain.WhatsAppDirectionOut,
		ChatID:    whatsAppChatID(candidate.Phone),
		Phone:     format.NormalizeDigits(candidate.Phone),
		Body:      candidate.Message,
		Status:    domain.WhatsAppStatusPending,
		Metadata:  &encoded,
	}
	if candidate.TenantID != "" {
		message.TenantID = domain.StrPtr(candidate.TenantID)
	}
	if _, err := deps.Repos.WhatsApp.InsertIfAbsent(ctx, message); err != nil {
		return fmt.Errorf("registrar envio pendente da cobrança: %w", err)
	}
	return nil
}

// recordCollectionSent registra o estágio na trilha. O evento é o que permite à
// régua saber até onde cobrou — por isso ele é gravado também quando o envio
// ficou pendente, com o canal dizendo a verdade.
func recordCollectionSent(ctx context.Context, deps Deps, actx AuditContext, candidate CollectionCandidate, channel string) {
	summary := fmt.Sprintf("Cobrança (%s) enviada para %s", strings.ToLower(candidate.StageLabel), candidate.TenantName)
	if channel == CollectionChannelPending {
		summary = fmt.Sprintf("Cobrança (%s) registrada como pendente para %s", strings.ToLower(candidate.StageLabel), candidate.TenantName)
	}
	if candidate.TenantName == "" {
		summary = fmt.Sprintf("Cobrança (%s) enviada", strings.ToLower(candidate.StageLabel))
	}

	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionCollectionSent,
		Entity:      domain.EntityCollection,
		EntityID:    candidate.ReceiptID,
		EntityLabel: collectionEntityLabel(candidate),
		Summary:     summary,
		Metadata: map[string]any{
			"stage":     int(candidate.Stage),
			"channel":   channel,
			"receiptId": candidate.ReceiptID,
			"amount":    candidate.Amount,
			"phone":     maskedPhone(candidate.Phone),
		},
	}); err != nil {
		deps.Log.Warn("auditoria da cobrança não gravada",
			"scope", "collection", "receipt", candidate.ReceiptID,
			"stage", int(candidate.Stage), "channel", channel, "err", err)
	}
}

// recordCollectionFailure registra a falha de envio na trilha. A régua não
// avança: sem evento `collection.sent`, o mesmo estágio é tentado de novo.
func recordCollectionFailure(ctx context.Context, deps Deps, actx AuditContext, candidate CollectionCandidate, err error) {
	deps.Log.Warn("falha no envio da cobrança",
		"scope", "collection", "receipt", candidate.ReceiptID,
		"tenant", candidate.TenantID, "stage", int(candidate.Stage), "err", err)

	if auditErr := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionWhatsAppSendFailed,
		Entity:      domain.EntityWhatsApp,
		EntityID:    candidate.ReceiptID,
		EntityLabel: collectionEntityLabel(candidate),
		Summary:     fmt.Sprintf("Falha ao cobrar %s pelo WhatsApp", candidate.TenantName),
		Metadata: map[string]any{
			"phone":     maskedPhone(candidate.Phone),
			"reason":    truncate(err.Error(), 300),
			"kind":      "collection",
			"receiptId": candidate.ReceiptID,
		},
	}); auditErr != nil {
		deps.Log.Warn("auditoria da falha de cobrança não gravada",
			"scope", "collection", "receipt", candidate.ReceiptID, "err", auditErr)
	}
}

// collectionEntityLabel monta o rótulo do evento na trilha: titular e
// competência, como o rótulo do lançamento.
func collectionEntityLabel(candidate CollectionCandidate) string {
	month := ""
	if len(candidate.DueDate) >= 7 {
		month = candidate.DueDate[5:7] + "/" + candidate.DueDate[0:4]
	}
	if candidate.TenantName == "" {
		return "Competência " + month
	}
	return candidate.TenantName + " · " + month
}

// lateDays conta os dias de atraso entre o vencimento e hoje. As duas datas são
// comparadas em UTC para que a virada de horário de verão não mude a conta.
func lateDays(dueDate string, today time.Time) int {
	due := format.ParseLocalDate(dueDate)
	from := time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, time.UTC)
	to := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	return int(to.Sub(from) / (24 * time.Hour))
}

// lateDaysLabel escreve o atraso em dias.
func lateDaysLabel(days int) string {
	if days == 1 {
		return "1 dia de atraso"
	}
	return fmt.Sprintf("%d dias de atraso", days)
}

// greetingName monta o tratamento do titular: "Olá, Maria" ou só "Olá" quando o
// nome não está preenchido.
func greetingName(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return ""
	}
	return ", " + fields[0]
}

// maskedPhone esconde o telefone no log e nos metadados da trilha: só os quatro
// últimos dígitos aparecem. O número do locatário é dado pessoal e não pertence
// ao log.
func maskedPhone(phone string) string {
	digits := format.NormalizeDigits(phone)
	if len(digits) < 4 {
		return "****"
	}
	return "****" + digits[len(digits)-4:]
}

// whatsAppChatID reproduz o identificador de conversa que o WAHA usa
// (`<dígitos com DDI>@c.us`, o mesmo de `whatsapp.ChatID`). Está duplicado aqui
// porque a regra de dependência do port proíbe application importar
// infrastructure; é o único ponto que precisa do formato.
func whatsAppChatID(phone string) string {
	digits := format.NormalizeDigits(phone)
	if digits == "" {
		return ""
	}
	if !strings.HasPrefix(digits, "55") || len(digits) < 12 {
		digits = "55" + digits
	}
	return digits + "@c.us"
}
