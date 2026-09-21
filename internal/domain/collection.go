package domain

// Régua de cobrança e atendimento automático (Work 0004). Este arquivo reúne o
// vocabulário que as duas áreas novas acrescentam ao domínio: os estágios da
// régua — decididos pelo atraso do lançamento, nunca pelo modelo de linguagem —
// e os eventos que registram a atuação do atendimento.
//
// Os estágios são um valor inteiro porque é ele que vai para o metadado `stage`
// da trilha; o rótulo é a única forma de apresentação.

// CollectionStage é o estágio da régua de cobrança.
type CollectionStage int

const (
	// CollectionStageFriendly é o lembrete amigável do primeiro atraso.
	CollectionStageFriendly CollectionStage = 1
	// CollectionStageFirm é a cobrança de tom firme.
	CollectionStageFirm CollectionStage = 2
	// CollectionStageInstallment é a proposta de parcelamento.
	CollectionStageInstallment CollectionStage = 3
)

// Prazos, em dias de atraso, a partir dos quais cada estágio é sustentado pelo
// dado do banco. Um lançamento vencido ontem recebe o lembrete amigável; a
// cobrança firme começa na segunda semana e a proposta de parcelamento, na
// terceira — o inquilino que atrasa um dia não recebe cobrança dura.
const (
	CollectionFriendlyDays    = 1
	CollectionFirmDays        = 7
	CollectionInstallmentDays = 15
)

// CollectionStageLabel devolve o rótulo em português do estágio.
func CollectionStageLabel(stage CollectionStage) string {
	switch stage {
	case CollectionStageFriendly:
		return "Lembrete amigável"
	case CollectionStageFirm:
		return "Cobrança firme"
	case CollectionStageInstallment:
		return "Proposta de parcelamento"
	default:
		return ""
	}
}

// Label devolve o rótulo em português do estágio.
func (s CollectionStage) Label() string { return CollectionStageLabel(s) }

// Valid informa se o valor é um estágio da régua.
func (s CollectionStage) Valid() bool { return CollectionStageLabel(s) != "" }

// CollectionStageByDelay devolve o estágio que o atraso (em dias) sustenta.
// Atraso menor que o primeiro prazo não sustenta estágio nenhum: a régua não
// cobra lançamento que ainda não venceu.
func CollectionStageByDelay(daysLate int) (CollectionStage, bool) {
	switch {
	case daysLate >= CollectionInstallmentDays:
		return CollectionStageInstallment, true
	case daysLate >= CollectionFirmDays:
		return CollectionStageFirm, true
	case daysLate >= CollectionFriendlyDays:
		return CollectionStageFriendly, true
	default:
		return 0, false
	}
}

// CollectionStageFromValue lê o estágio gravado na trilha (metadado `stage`).
// Valor desconhecido é descartado, nunca convertido em estágio.
func CollectionStageFromValue(value int) (CollectionStage, bool) {
	stage := CollectionStage(value)
	if !stage.Valid() {
		return 0, false
	}
	return stage, true
}

// CollectionStageOptions é um estágio como o painel o apresenta: valor, rótulo e
// o atraso que o abre.
type CollectionStageOption struct {
	Value    int    `json:"value"`
	Label    string `json:"label"`
	FromDays int    `json:"fromDays"`
}

// CollectionStageOptions devolve os estágios na ordem de envio, para o painel
// explicar a régua sem repetir os prazos.
func CollectionStageOptions() []CollectionStageOption {
	return []CollectionStageOption{
		{int(CollectionStageFriendly), CollectionStageLabel(CollectionStageFriendly), CollectionFriendlyDays},
		{int(CollectionStageFirm), CollectionStageLabel(CollectionStageFirm), CollectionFirmDays},
		{int(CollectionStageInstallment), CollectionStageLabel(CollectionStageInstallment), CollectionInstallmentDays},
	}
}

// ── Vocabulário da trilha ──────────────────────────────────────────

// Ações do atendimento automático. O catálogo da trilha vive em
// audit_catalog.go, que outra área do port mantém; o vocabulário continua
// fechado porque as ações novas entram no mesmo mapa, registradas abaixo.
const (
	// ActionAgentAnswered registra a resposta que o atendimento montou com
	// dado real do banco (CAP-302: cada resposta é registrada).
	ActionAgentAnswered = "agent.answered"
	// ActionAgentEscalated registra a conversa encaminhada ao locador.
	ActionAgentEscalated = "agent.escalated"
	// ActionMaintenanceOpened registra a solicitação de manutenção aberta pelo
	// locatário no atendimento.
	ActionMaintenanceOpened = "maintenance.opened"
)

// collectionAuditActions registra as ações novas no catálogo. Sem isso, a aba
// Histórico mostraria o identificador cru em vez do rótulo em português.
func init() {
	auditActions[ActionAgentAnswered] = AuditActionMeta{
		Action: ActionAgentAnswered,
		Label:  "Resposta do atendimento automático",
		Entity: EntityWhatsApp,
		Tone:   ToneAccess,
	}
	auditActions[ActionAgentEscalated] = AuditActionMeta{
		Action: ActionAgentEscalated,
		Label:  "Conversa encaminhada ao locador",
		Entity: EntityWhatsApp,
		Tone:   ToneUpdate,
	}
	auditActions[ActionMaintenanceOpened] = AuditActionMeta{
		Action: ActionMaintenanceOpened,
		Label:  "Solicitação de manutenção registrada",
		Entity: EntityWhatsApp,
		Tone:   ToneCreate,
	}
}

// Situação da mensagem registrada por nós que ainda não foi entregue ao canal.
// A tabela `whatsapp_messages` não restringe os valores de `status`.
const WhatsAppStatusPending = "pending"
