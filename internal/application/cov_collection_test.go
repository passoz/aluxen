package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Cobertura da régua de cobrança (`collection.go`) e da superfície de texto do
// canal (`whatsapp.go`, `chat.go`). Os dublês em memória vêm do harness `cov`;
// aqui só entram o canal de envio e a trilha que precisa falhar ao listar —
// comportamentos que o harness não oferece.

// ── Dublês locais (`ccol`) ────────────────────────────────────────

// ccolSent é um envio registrado pelo canal.
type ccolSent struct{ Phone, Text string }

// ccolChannel é o canal de WhatsApp do teste: registra o que saiu, falha sob
// comando e guarda os jobs por dono.
type ccolChannel struct {
	sends   []ccolSent
	sendErr error
	jobs    map[string]WhatsAppJob
	owners  map[string]string
	next    int
}

func newCcolChannel() *ccolChannel {
	return &ccolChannel{jobs: map[string]WhatsAppJob{}, owners: map[string]string{}}
}

func (c *ccolChannel) SendText(_ context.Context, phone, text string) (string, error) {
	if c.sendErr != nil {
		return "", c.sendErr
	}
	c.sends = append(c.sends, ccolSent{Phone: phone, Text: text})
	return "wamid-canal", nil
}

func (c *ccolChannel) Create(userID string) WhatsAppJob {
	c.next++
	job := WhatsAppJob{
		ID:        fmt.Sprintf("job-%d", c.next),
		UserID:    userID,
		Status:    JobRunning,
		Stage:     StagePreparing,
		CreatedAt: 1,
		UpdatedAt: 1,
	}
	c.jobs[job.ID] = job
	c.owners[job.ID] = userID
	return job
}

func (c *ccolChannel) Stage(jobID string, stage WhatsAppStage) {
	job := c.jobs[jobID]
	job.Stage = stage
	c.jobs[jobID] = job
}

func (c *ccolChannel) Complete(jobID string, result WhatsAppSendResult) {
	job := c.jobs[jobID]
	job.Status = JobDone
	job.Stage = StageDone
	job.Result = &result
	c.jobs[jobID] = job
}

func (c *ccolChannel) Fail(jobID, reason string) {
	job := c.jobs[jobID]
	job.Status = JobError
	job.Error = reason
	c.jobs[jobID] = job
}

// Get devolve o job sem normalizar o id: quem consulta é que precisa entregar o
// id limpo.
func (c *ccolChannel) Get(jobID, userID string) (WhatsAppJob, bool) {
	job, ok := c.jobs[jobID]
	if !ok || c.owners[job.ID] != userID {
		return WhatsAppJob{}, false
	}
	return job, true
}

// ccolAuditFalha é a trilha que não consegue listar: o histórico da régua não
// pode ser derivado e a rodada precisa parar em vez de cobrar às cegas.
type ccolAuditFalha struct {
	*covAudit
	listErr error
}

func (r *ccolAuditFalha) List(context.Context, domain.AuditFilter) ([]domain.AuditLog, error) {
	return nil, r.listErr
}

// ccolMessages expõe na mensagem os campos que a prévia decide, para que o
// teste prove o que a prévia entregou ao canal de texto — o dublê do harness só
// mostra parte deles.
type ccolMessages struct{ covMessages }

func (ccolMessages) Receipt(message WhatsAppReceiptMessage) string {
	paidRaw := "nil"
	if message.PaidAmountRaw != nil {
		paidRaw = format.FormatCurrency(*message.PaidAmountRaw)
	}
	return strings.Join([]string{
		"recibo", message.TenantName, message.PropertyName, message.DueDate,
		message.Amount, message.PaidAmount, message.PaidAt, message.Category,
		message.Description, message.PDFURL, message.ValidationURL,
		message.LandlordName, paidRaw,
	}, "|")
}

func (ccolMessages) Contract(message WhatsAppContractMessage) string {
	return strings.Join([]string{
		"contrato", message.TenantName, message.PropertyName, message.StartDate,
		message.EndDate, fmt.Sprintf("%d", message.DueDay), message.RentAmount,
		message.PDFURL, message.ValidationURL, message.LandlordName,
	}, "|")
}

// ── Auxiliares (`ccol`) ───────────────────────────────────────────

// ccolEnv monta o ambiente com a base pública fixa: os links dos documentos não
// podem depender da rede da máquina que roda o teste.
func ccolEnv(t *testing.T) *covEnv {
	t.Helper()
	env := newCovEnv(t)
	env.settings.values[domain.SettingAppURL] = "https://aluxen.test"
	return env
}

// ccolReceipt monta um lançamento sem contrato (avulso), como os que a régua
// cobra pelo titular do próprio lançamento.
func ccolReceipt(id, tenantID, dueDate string, amount float64) domain.Receipt {
	return domain.Receipt{
		ID:       id,
		TenantID: domain.StrPtr(tenantID),
		Category: domain.ReceiptCategoryRent,
		DueDate:  dueDate,
		Amount:   amount,
	}
}

// ccolMetadata decodifica o metadado JSON do evento da trilha.
func ccolMetadata(t *testing.T, entry domain.AuditLog) map[string]any {
	t.Helper()
	require.NotNil(t, entry.Metadata, "evento sem metadado")
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(*entry.Metadata), &out))
	return out
}

// ccolEntries filtra os eventos da ação informada.
func ccolEntries(env *covEnv, action string) []domain.AuditLog {
	var out []domain.AuditLog
	for _, entry := range env.audit.entries {
		if entry.Action == action {
			out = append(out, entry)
		}
	}
	return out
}

// ccolOnly devolve o único evento da ação informada.
func ccolOnly(t *testing.T, env *covEnv, action string) domain.AuditLog {
	t.Helper()
	entries := ccolEntries(env, action)
	require.Len(t, entries, 1, "eventos %s na trilha", action)
	return entries[0]
}

// ccolCandidateIDs devolve os ids dos candidatos na ordem.
func ccolCandidateIDs(candidates []CollectionCandidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ReceiptID)
	}
	return ids
}

// ccolOverdue monta o cenário padrão: um locatário com telefone e um lançamento
// vencido em 2026-09-01 (20 dias de atraso, estágio de parcelamento).
func ccolOverdue(t *testing.T) (*covEnv, string) {
	t.Helper()
	env := ccolEnv(t)
	tenant := covTenantOf("tenant-1", "Maria Souza")
	tenant.Phone = domain.StrPtr("(11) 99999-9999")
	env.tenants.items = []domain.Tenant{tenant}
	env.receipts.items = []domain.Receipt{ccolReceipt("receipt-1", "tenant-1", "2026-09-01", 2000)}
	return env, "receipt-1"
}

// ── PlanCollection ────────────────────────────────────────────────

func TestCcolPlanCollectionPropagatesRepositoryErrors(t *testing.T) {
	ctx := context.Background()

	// Falha ao listar os lançamentos interrompe o planejamento.
	env := ccolEnv(t)
	env.receipts.listErr = errors.New("banco fora")
	_, err := PlanCollection(ctx, env.deps, covToday())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listar lançamentos da régua")

	// Falha ao ler a trilha interrompe antes de escolher estágio: sem histórico
	// a régua repetiria cobrança já enviada.
	env = ccolEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("tenant-1", "Maria")}
	env.receipts.items = []domain.Receipt{ccolReceipt("receipt-1", "tenant-1", "2026-09-01", 2000)}
	env.deps.Repos.Audit = &ccolAuditFalha{covAudit: env.audit, listErr: errors.New("trilha fora")}
	_, err = PlanCollection(ctx, env.deps, covToday())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listar cobranças enviadas")

	// Falha ao resolver o titular do lançamento interrompe a régua inteira.
	env = ccolEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("tenant-1", "Maria")}
	env.tenants.byIDErr = errors.New("locatário indisponível")
	env.receipts.items = []domain.Receipt{ccolReceipt("receipt-1", "tenant-1", "2026-09-01", 2000)}
	_, err = PlanCollection(ctx, env.deps, covToday())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "locatário indisponível")

	// A rodada para antes de entregar qualquer coisa quando o planejamento falha.
	env = ccolEnv(t)
	env.receipts.listErr = errors.New("banco fora")
	_, err = RunCollectionWithChannel(ctx, env.deps, newCcolChannel(), covCtx(), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listar lançamentos da régua")
}

func TestCcolPlanCollectionSkipsNotOverdueAndRanksTies(t *testing.T) {
	env := ccolEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("tenant-1", "Maria")}

	paid := ccolReceipt("receipt-paid", "tenant-1", "2026-08-10", 800)
	paid = covPaidReceipt(paid, "2026-08-09", 800)
	env.receipts.items = []domain.Receipt{
		ccolReceipt("receipt-future", "tenant-1", "2026-10-10", 900),
		paid,
		ccolReceipt("receipt-today", "tenant-1", "2026-09-21", 900),
		ccolReceipt("receipt-b", "tenant-1", "2026-09-14", 1200),
		ccolReceipt("receipt-a", "tenant-1", "2026-09-14", 1100),
		ccolReceipt("receipt-old", "tenant-1", "2026-09-01", 2000),
	}

	candidates, err := PlanCollection(context.Background(), env.deps, covToday())
	require.NoError(t, err)

	// Vencido ontem e a vencer ficam fora; empate de vencimento sai pelo id.
	assert.Equal(t, []string{"receipt-old", "receipt-a", "receipt-b"}, ccolCandidateIDs(candidates))
	assert.Equal(t, 20, candidates[0].DaysLate)
	assert.Equal(t, domain.CollectionStageFirm, candidates[1].Stage)
	assert.Equal(t, domain.CollectionStageFirm, candidates[2].Stage)
	assert.Equal(t, domain.CollectionStage(0), candidates[1].LastStage)
	assert.Empty(t, candidates[1].LastStageLabel)
}

func TestCcolPlanCollectionIgnoresReceiptWithoutTrailEntryID(t *testing.T) {
	env := ccolEnv(t)
	env.receipts.items = []domain.Receipt{ccolReceipt("receipt-1", "", "2026-09-01", 2000)}

	// Evento sem entity_id não pertence a lançamento nenhum e é ignorado.
	blank := `{"stage":3}`
	require.NoError(t, env.audit.Insert(context.Background(), &domain.AuditLog{
		ID: "audit-1", Action: domain.ActionCollectionSent, Entity: domain.EntityCollection,
		EntityID: domain.StrPtr(""), Metadata: &blank,
	}))

	candidates, err := PlanCollection(context.Background(), env.deps, covToday())
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, domain.CollectionStageInstallment, candidates[0].Stage)
	assert.Equal(t, domain.CollectionStage(0), candidates[0].LastStage)
}

// ── Texto da cobrança ─────────────────────────────────────────────

func TestCcolCollectionMessageExactTexts(t *testing.T) {
	base := CollectionCandidate{
		TenantName:   "Maria Souza",
		DueDate:      "2026-09-10",
		Amount:       2500,
		DaysLate:     11,
		LandlordName: "João Locador",
	}
	signature := "\n\nJoão Locador"

	cases := map[domain.CollectionStage]string{
		domain.CollectionStageFriendly: "Olá, Maria, passando para lembrar do aluguel com vencimento em 10/09/2026, " +
			"no valor de R$ 2.500,00, ainda em aberto (11 dias de atraso). Se o pagamento já foi feito, " +
			"por favor desconsidere esta mensagem." + signature,
		domain.CollectionStageFirm: "Olá, Maria, o aluguel com vencimento em 10/09/2026, no valor de R$ 2.500,00, " +
			"segue em aberto (11 dias de atraso) e ainda não identificamos o pagamento. Pedimos a regularização. " +
			"Se já pagou, envie o comprovante por aqui." + signature,
		domain.CollectionStageInstallment: "Olá, Maria, o aluguel com vencimento em 10/09/2026, no valor de R$ 2.500,00, " +
			"está em aberto (11 dias de atraso). Podemos combinar o pagamento em partes para regularizar: " +
			"responda esta mensagem com a data que funciona para você." + signature,
	}
	for stage, want := range cases {
		candidate := base
		candidate.Stage = stage
		assert.Equal(t, want, CollectionMessage(candidate), "estágio %s", stage.Label())
	}

	// Titular sem nome e sem assinatura: o texto continua legível e sem número
	// vindo de fora do lançamento.
	anonymous := CollectionCandidate{Stage: domain.CollectionStageFriendly, DueDate: "2026-09-20", Amount: 900, DaysLate: 1}
	assert.Equal(t,
		"Olá, passando para lembrar do aluguel com vencimento em 20/09/2026, no valor de R$ 900,00, "+
			"ainda em aberto (1 dia de atraso). Se o pagamento já foi feito, por favor desconsidere esta mensagem.",
		CollectionMessage(anonymous))

	// Estágio desconhecido cai no lembrete — o mesmo do estágio amigável.
	unknown := anonymous
	unknown.Stage = domain.CollectionStage(9)
	assert.Equal(t, CollectionMessage(anonymous), CollectionMessage(unknown))
}

func TestCcolCollectionAuxLabels(t *testing.T) {
	assert.Equal(t, "1 dia de atraso", lateDaysLabel(1))
	assert.Equal(t, "0 dias de atraso", lateDaysLabel(0))
	assert.Equal(t, "20 dias de atraso", lateDaysLabel(20))

	assert.Equal(t, ", Maria", greetingName("Maria Souza"))
	assert.Equal(t, ", Maria", greetingName("  Maria  "))
	assert.Equal(t, "", greetingName(""))
	assert.Equal(t, "", greetingName("   "))

	assert.Equal(t, "****9999", maskedPhone("(11) 99999-9999"))
	assert.Equal(t, "****", maskedPhone("119"))
	assert.Equal(t, "****", maskedPhone(""))
	assert.Equal(t, "****1234", maskedPhone("1234"))

	assert.Equal(t, "", whatsAppChatID(""))
	assert.Equal(t, "5511999999999@c.us", whatsAppChatID("(11) 99999-9999"))
	assert.Equal(t, "5511999999999@c.us", whatsAppChatID("11999999999"))
	assert.Equal(t, "5511999999999@c.us", whatsAppChatID("5511999999999"))
	assert.Equal(t, "555511999999@c.us", whatsAppChatID("5511999999"), "DDI duplicado é o que o app atual faz")

	assert.Equal(t,
		"Maria Souza · 09/2026",
		collectionEntityLabel(CollectionCandidate{TenantName: "Maria Souza", DueDate: "2026-09-10"}))
	assert.Equal(t,
		"Competência 09/2026",
		collectionEntityLabel(CollectionCandidate{DueDate: "2026-09-10"}))
	assert.Equal(t,
		"Maria Souza · ",
		collectionEntityLabel(CollectionCandidate{TenantName: "Maria Souza", DueDate: "2026"}))

	// Atraso é contado em dias cheios, ignorando a hora do dia.
	assert.Equal(t, 1, lateDays("2026-09-20", time.Date(2026, 9, 21, 23, 59, 0, 0, time.UTC)))
	assert.Equal(t, 0, lateDays("2026-09-21", time.Date(2026, 9, 21, 0, 1, 0, 0, time.UTC)))
	assert.Equal(t, -3, lateDays("2026-09-24", covToday()))
}

func TestCcolCollectionCandidateTenantFallbacks(t *testing.T) {
	// O nome do lançamento manda; o do cadastro só preenche o buraco.
	env := ccolEnv(t)
	tenant := covTenantOf("tenant-1", "Maria do Cadastro")
	tenant.Phone = domain.StrPtr("11999999999")
	env.tenants.items = []domain.Tenant{tenant}
	receipt := ccolReceipt("receipt-1", "tenant-1", "2026-09-01", 2000)
	receipt.TenantName = "Maria do Lançamento"
	env.receipts.items = []domain.Receipt{receipt}
	sent := `{"stage":1}`
	require.NoError(t, env.audit.Insert(context.Background(), &domain.AuditLog{
		ID: "audit-1", Action: domain.ActionCollectionSent, Entity: domain.EntityCollection,
		EntityID: domain.StrPtr("receipt-1"), Metadata: &sent,
	}))

	candidates, err := PlanCollection(context.Background(), env.deps, covToday())
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	candidate := candidates[0]
	assert.Equal(t, "tenant-1", candidate.TenantID)
	assert.Equal(t, "Maria do Lançamento", candidate.TenantName)
	assert.Equal(t, "11999999999", candidate.Phone)
	assert.Equal(t, domain.CollectionStageInstallment, candidate.Stage)
	assert.Equal(t, "Proposta de parcelamento", candidate.StageLabel)
	assert.Equal(t, domain.CollectionStageFriendly, candidate.LastStage)
	assert.Equal(t, "Lembrete amigável", candidate.LastStageLabel)

	// Lançamento sem titular conhecido: a régua mantém o candidato, apenas sem
	// destinatário e sem saudação.
	env = ccolEnv(t)
	env.receipts.items = []domain.Receipt{ccolReceipt("receipt-solto", "", "2026-09-01", 2000)}
	candidates, err = PlanCollection(context.Background(), env.deps, covToday())
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, "", candidates[0].TenantID)
	assert.Equal(t, "", candidates[0].Phone)
	assert.True(t, strings.HasPrefix(candidates[0].Message, "Olá, o aluguel"), candidates[0].Message)

	// Titular removido do cadastro é o mesmo que titular ausente.
	env = ccolEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("tenant-outro", "Outro")}
	env.receipts.items = []domain.Receipt{ccolReceipt("receipt-removido", "tenant-1", "2026-09-01", 2000)}
	candidates, err = PlanCollection(context.Background(), env.deps, covToday())
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, "", candidates[0].Phone)
}

// ── Histórico da trilha ───────────────────────────────────────────

func TestCcolCollectionHistoryAndEntryStage(t *testing.T) {
	ctx := context.Background()

	// Sem trilha configurada a régua não tem memória, mas continua funcionando.
	env := ccolEnv(t)
	env.deps.Repos.Audit = nil
	history, err := collectionHistory(ctx, env.deps)
	require.NoError(t, err)
	assert.Empty(t, history)

	env = ccolEnv(t)
	entries := []*domain.AuditLog{
		{ID: "1", Action: domain.ActionCollectionSent, EntityID: nil},
		{ID: "2", Action: domain.ActionCollectionSent, EntityID: domain.StrPtr(""), Metadata: ptrString(`{"stage":3}`)},
		{ID: "3", Action: domain.ActionCollectionSent, EntityID: domain.StrPtr("receipt-x"), Metadata: nil},
		{ID: "4", Action: domain.ActionCollectionSent, EntityID: domain.StrPtr("receipt-x"), Metadata: ptrString("   ")},
		{ID: "5", Action: domain.ActionCollectionSent, EntityID: domain.StrPtr("receipt-x"), Metadata: ptrString("não é json")},
		{ID: "6", Action: domain.ActionCollectionSent, EntityID: domain.StrPtr("receipt-x"), Metadata: ptrString(`{"channel":"pending"}`)},
		{ID: "7", Action: domain.ActionCollectionSent, EntityID: domain.StrPtr("receipt-x"), Metadata: ptrString(`{"stage":"2"}`)},
		{ID: "8", Action: domain.ActionCollectionSent, EntityID: domain.StrPtr("receipt-x"), Metadata: ptrString(`{"stage":9}`)},
		{ID: "9", Action: domain.ActionCollectionSent, EntityID: domain.StrPtr("receipt-x"), Metadata: ptrString(`{"stage":1}`)},
		{ID: "10", Action: domain.ActionCollectionSent, EntityID: domain.StrPtr("receipt-x"), Metadata: ptrString(`{"stage":3}`)},
		{ID: "11", Action: domain.ActionCollectionSent, EntityID: domain.StrPtr("receipt-y"), Metadata: ptrString(`{"stage":3}`)},
		{ID: "12", Action: domain.ActionCollectionSent, EntityID: domain.StrPtr("receipt-y"), Metadata: ptrString(`{"stage":1}`)},
	}
	for _, entry := range entries {
		require.NoError(t, env.audit.Insert(ctx, entry))
	}

	history, err = collectionHistory(ctx, env.deps)
	require.NoError(t, err)
	require.Len(t, history, 2, "metadado ilegível não cria entrada de histórico")
	assert.Equal(t, domain.CollectionStageInstallment, history["receipt-x"])
	assert.Equal(t, domain.CollectionStageInstallment, history["receipt-y"], "o maior estágio enviado é o que vale")

	for _, testCase := range []struct {
		name     string
		metadata *string
		want     domain.CollectionStage
		ok       bool
	}{
		{"sem metadado", nil, 0, false},
		{"metadado vazio", ptrString("   "), 0, false},
		{"json inválido", ptrString("{"), 0, false},
		{"sem stage", ptrString(`{"channel":"pending"}`), 0, false},
		{"stage textual", ptrString(`{"stage":"1"}`), 0, false},
		{"stage fora da régua", ptrString(`{"stage":0}`), 0, false},
		{"stage válido", ptrString(`{"stage":2}`), domain.CollectionStageFirm, true},
	} {
		stage, ok := collectionEntryStage(testCase.metadata)
		assert.Equal(t, testCase.ok, ok, testCase.name)
		assert.Equal(t, testCase.want, stage, testCase.name)
	}
}

func ptrString(value string) *string { return &value }

// ── Registro do envio pendente ────────────────────────────────────

func TestCcolRegisterPendingCollection(t *testing.T) {
	ctx := context.Background()
	candidate := CollectionCandidate{
		ReceiptID: "receipt-1", TenantID: "tenant-1", TenantName: "Maria",
		Phone: "(11) 99999-9999", Stage: domain.CollectionStageFirm,
		Message: "texto da cobrança", Amount: 1200,
	}

	// Sem persistência do canal não há onde registrar o pendente.
	env := ccolEnv(t)
	env.deps.Repos.WhatsApp = nil
	require.Error(t, registerPendingCollection(ctx, env.deps, candidate))

	// Falha da caixa é propagada com o contexto do que se tentou gravar.
	env = ccolEnv(t)
	env.messages.insertErr = errors.New("disco cheio")
	err := registerPendingCollection(ctx, env.deps, candidate)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "registrar envio pendente da cobrança")
	assert.Empty(t, env.messages.items)

	env = ccolEnv(t)
	require.NoError(t, registerPendingCollection(ctx, env.deps, candidate))
	require.Len(t, env.messages.items, 1)
	stored := env.messages.items[0]
	assert.Equal(t, format.FormatUTCStamp(covToday()), stored.CreatedAt)
	assert.Equal(t, domain.WhatsAppDirectionOut, stored.Direction)
	assert.Equal(t, "5511999999999@c.us", stored.ChatID)
	assert.Equal(t, "11999999999", stored.Phone)
	assert.Equal(t, candidate.Message, stored.Body)
	assert.Equal(t, domain.WhatsAppStatusPending, stored.Status)
	assert.Equal(t, "tenant-1", domain.Str(stored.TenantID))
	require.NotNil(t, stored.Metadata)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal([]byte(*stored.Metadata), &metadata))
	assert.Equal(t, float64(domain.CollectionStageFirm), metadata["stage"])
	assert.Equal(t, CollectionChannelPending, metadata["channel"])
	assert.Equal(t, "receipt-1", metadata["receiptId"])
	assert.Equal(t, 1200.0, metadata["amount"])

	// Lançamento sem titular conhecido não amarra a mensagem a um locatário.
	env = ccolEnv(t)
	candidate.TenantID = ""
	require.NoError(t, registerPendingCollection(ctx, env.deps, candidate))
	require.Len(t, env.messages.items, 1)
	assert.Nil(t, env.messages.items[0].TenantID)
}

// ── Rodada (RunCollection) ────────────────────────────────────────

func TestCcolRunCollectionDeliversAndAudits(t *testing.T) {
	env, receiptID := ccolOverdue(t)
	channel := newCcolChannel()

	outcomes, err := RunCollectionWithChannel(context.Background(), env.deps, channel, covCtxOf(domain.AuditOriginJob), false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)

	outcome := outcomes[0]
	assert.Equal(t, receiptID, outcome.ReceiptID)
	assert.Equal(t, "tenant-1", outcome.TenantID)
	assert.Equal(t, domain.CollectionStageInstallment, outcome.Stage)
	assert.Equal(t, "Proposta de parcelamento", outcome.StageLabel)
	assert.Equal(t, 2000.0, outcome.Amount)
	assert.Equal(t, "2026-09-01", outcome.DueDate)
	assert.True(t, outcome.Delivered)
	assert.False(t, outcome.DryRun)
	assert.Equal(t, CollectionChannelWhatsApp, outcome.Channel)
	assert.Empty(t, outcome.Reason)

	require.Len(t, channel.sends, 1)
	assert.Equal(t, "(11) 99999-9999", channel.sends[0].Phone)
	assert.Equal(t, outcome.Message, channel.sends[0].Text)
	assert.Contains(t, channel.sends[0].Text, "R$ 2.000,00")

	entry := ccolOnly(t, env, domain.ActionCollectionSent)
	assert.Equal(t, domain.EntityCollection, entry.Entity)
	assert.Equal(t, receiptID, domain.Str(entry.EntityID))
	assert.Equal(t, "Maria Souza · 09/2026", domain.Str(entry.EntityLabel))
	assert.Equal(t, "Cobrança (proposta de parcelamento) enviada para Maria Souza", entry.Summary)
	assert.Equal(t, domain.AuditOriginJob, entry.Origin)
	metadata := ccolMetadata(t, entry)
	assert.Equal(t, float64(domain.CollectionStageInstallment), metadata["stage"])
	assert.Equal(t, CollectionChannelWhatsApp, metadata["channel"])
	assert.Equal(t, receiptID, metadata["receiptId"])
	assert.Equal(t, 2000.0, metadata["amount"])
	assert.Equal(t, "****9999", metadata["phone"], "o telefone do locatário não vai inteiro para a trilha")

	// O eco do canal é quem grava a mensagem enviada; a régua não duplica.
	assert.Empty(t, env.messages.items)
}

func TestCcolRunCollectionAuditsAnonymousTenant(t *testing.T) {
	env := ccolEnv(t)
	anonymous := covTenantOf("tenant-1", "")
	anonymous.Phone = domain.StrPtr("11999999999")
	env.tenants.items = []domain.Tenant{anonymous}
	env.receipts.items = []domain.Receipt{ccolReceipt("receipt-1", "tenant-1", "2026-09-20", 900)}
	channel := newCcolChannel()

	outcomes, err := RunCollectionWithChannel(context.Background(), env.deps, channel, covCtx(), false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.True(t, outcomes[0].Delivered)
	assert.Equal(t, "", outcomes[0].TenantName)
	assert.Contains(t, channel.sends[0].Text, "Olá, passando para lembrar")

	entry := ccolOnly(t, env, domain.ActionCollectionSent)
	assert.Equal(t, "Cobrança (lembrete amigável) enviada", entry.Summary)
	assert.Equal(t, "Competência 09/2026", domain.Str(entry.EntityLabel))
}

func TestCcolRunCollectionWithoutChannelRegistersPending(t *testing.T) {
	env, receiptID := ccolOverdue(t)

	outcomes, err := RunCollection(context.Background(), env.deps, covCtx(), false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.False(t, outcomes[0].Delivered)
	assert.Equal(t, CollectionChannelPending, outcomes[0].Channel)
	assert.Contains(t, outcomes[0].Reason, "pendente")

	require.Len(t, env.messages.items, 1)
	assert.Equal(t, domain.WhatsAppStatusPending, env.messages.items[0].Status)
	assert.Equal(t, outcomes[0].Message, env.messages.items[0].Body)

	entry := ccolOnly(t, env, domain.ActionCollectionSent)
	assert.Equal(t, "Cobrança (proposta de parcelamento) registrada como pendente para Maria Souza", entry.Summary)
	assert.Equal(t, CollectionChannelPending, ccolMetadata(t, entry)["channel"])
	assert.Equal(t, receiptID, domain.Str(entry.EntityID))

	// O estágio registrado como pendente não é repetido na rodada seguinte.
	again, err := PlanCollection(context.Background(), env.deps, covToday())
	require.NoError(t, err)
	assert.Empty(t, again)
}

func TestCcolRunCollectionChannelFailureDoesNotAdvance(t *testing.T) {
	env, receiptID := ccolOverdue(t)
	channel := newCcolChannel()
	channel.sendErr = errors.New("WAHA indisponível")

	outcomes, err := RunCollectionWithChannel(context.Background(), env.deps, channel, covCtxOf(domain.AuditOriginJob), false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.False(t, outcomes[0].Delivered)
	assert.Equal(t, "Falha no envio pelo WhatsApp.", outcomes[0].Reason)
	assert.Empty(t, outcomes[0].Channel)
	assert.Empty(t, channel.sends)
	assert.Empty(t, env.messages.items, "falha de envio não vira mensagem pendente")

	assert.Empty(t, ccolEntries(env, domain.ActionCollectionSent), "sem envio não há avanço da régua")
	entry := ccolOnly(t, env, domain.ActionWhatsAppSendFailed)
	assert.Equal(t, domain.EntityWhatsApp, entry.Entity)
	assert.Equal(t, receiptID, domain.Str(entry.EntityID))
	assert.Equal(t, "Falha ao cobrar Maria Souza pelo WhatsApp", entry.Summary)
	metadata := ccolMetadata(t, entry)
	assert.Equal(t, "****9999", metadata["phone"])
	assert.Equal(t, "WAHA indisponível", metadata["reason"])
	assert.Equal(t, "collection", metadata["kind"])
	assert.Equal(t, receiptID, metadata["receiptId"])

	// A régua tenta o mesmo estágio de novo na próxima rodada.
	planned, err := PlanCollection(context.Background(), env.deps, covToday())
	require.NoError(t, err)
	require.Len(t, planned, 1)
	assert.Equal(t, domain.CollectionStageInstallment, planned[0].Stage)
}

func TestCcolRunCollectionTruncatesFailureReason(t *testing.T) {
	env, _ := ccolOverdue(t)
	channel := newCcolChannel()
	channel.sendErr = errors.New(strings.Repeat("x", 400))

	_, err := RunCollectionWithChannel(context.Background(), env.deps, channel, covCtx(), false)
	require.NoError(t, err)

	metadata := ccolMetadata(t, ccolOnly(t, env, domain.ActionWhatsAppSendFailed))
	reason, ok := metadata["reason"].(string)
	require.True(t, ok)
	assert.Len(t, reason, maintenanceDescriptionLimit, "a razão da falha é cortada antes de ir para a trilha")
}

func TestCcolRunCollectionAuditFailuresAreBestEffort(t *testing.T) {
	// Falha ao auditar não derruba a entrega já feita.
	env, _ := ccolOverdue(t)
	env.audit.insertErr = errors.New("trilha fora")
	channel := newCcolChannel()

	outcomes, err := RunCollectionWithChannel(context.Background(), env.deps, channel, covCtx(), false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.True(t, outcomes[0].Delivered)
	assert.Len(t, channel.sends, 1)
	assert.Empty(t, env.audit.entries)

	// Falha ao auditar a própria falha também não derruba a rodada.
	env, _ = ccolOverdue(t)
	env.audit.insertErr = errors.New("trilha fora")
	channel = newCcolChannel()
	channel.sendErr = errors.New("WAHA indisponível")

	outcomes, err = RunCollectionWithChannel(context.Background(), env.deps, channel, covCtx(), false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.False(t, outcomes[0].Delivered)
	assert.Empty(t, env.audit.entries)
}

func TestCcolRunCollectionPendingRegistrationFailure(t *testing.T) {
	env, _ := ccolOverdue(t)
	env.messages.insertErr = errors.New("disco cheio")

	outcomes, err := RunCollection(context.Background(), env.deps, covCtx(), false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.False(t, outcomes[0].Delivered)
	assert.Equal(t, "Não foi possível registrar o envio pendente.", outcomes[0].Reason)
	assert.Empty(t, outcomes[0].Channel)
	// Sem mensagem nem evento, a régua não avança em silêncio.
	assert.Empty(t, env.audit.entries)
}

func TestCcolRunCollectionDryRunAndMissingPhone(t *testing.T) {
	env, _ := ccolOverdue(t)
	channel := newCcolChannel()

	outcomes, err := RunCollectionWithChannel(context.Background(), env.deps, channel, covCtx(), true)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.True(t, outcomes[0].DryRun)
	assert.False(t, outcomes[0].Delivered)
	assert.Equal(t, "Simulação: nenhuma mensagem enviada.", outcomes[0].Reason)
	assert.Contains(t, outcomes[0].Message, "R$ 2.000,00")
	assert.Empty(t, channel.sends)
	assert.Empty(t, env.messages.items)
	assert.Empty(t, env.audit.entries)

	// Titular sem telefone: nada sai e nada é registrado.
	env = ccolEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("tenant-1", "Maria Souza")}
	env.receipts.items = []domain.Receipt{ccolReceipt("receipt-1", "tenant-1", "2026-09-01", 2000)}
	channel = newCcolChannel()

	outcomes, err = RunCollectionWithChannel(context.Background(), env.deps, channel, covCtx(), false)
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	assert.False(t, outcomes[0].Delivered)
	assert.Equal(t, "Titular sem telefone cadastrado.", outcomes[0].Reason)
	assert.Empty(t, channel.sends)
	assert.Empty(t, env.messages.items)
	assert.Empty(t, env.audit.entries)
}

// ── Prévia do WhatsApp ────────────────────────────────────────────

func TestCcolWhatsAppPreviewValidation(t *testing.T) {
	ctx := context.Background()
	env := ccolEnv(t)

	_, err := WhatsAppPreview(ctx, env.deps, WhatsAppKindReceipt, "   ")
	assert.True(t, domain.IsValidation(err))
	assert.Equal(t, "Parâmetros inválidos.", domain.ValidationMessage(err))

	_, err = WhatsAppPreview(ctx, env.deps, "outro", "receipt-1")
	assert.True(t, domain.IsValidation(err))

	env.deps.Messages = nil
	_, err = WhatsAppPreview(ctx, env.deps, WhatsAppKindReceipt, "receipt-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "superfície de texto do WhatsApp não configurada")
}

func TestCcolWhatsAppPreviewReceiptPayload(t *testing.T) {
	ctx := context.Background()
	env := ccolEnv(t)
	env.deps.Messages = ccolMessages{}
	env.settings.values[domain.SettingLandlordName] = "João Locador"

	tenant := covTenantOf("tenant-1", "Maria Cadastro")
	tenant.Phone = domain.StrPtr("(11) 99999-9999")
	env.tenants.items = []domain.Tenant{tenant}

	receipt := domain.Receipt{
		ID: "receipt-1", TenantID: domain.StrPtr("tenant-1"), TenantName: "Maria Souza",
		PropertyName: "Apto 1", Category: domain.ReceiptCategoryRent, Description: domain.StrPtr("Aluguel de setembro"),
		DueDate: "2026-09-10", Amount: 1000, PaidAt: domain.StrPtr("2026-09-20T10:00:00Z"),
	}
	env.receipts.items = []domain.Receipt{receipt}

	// Recibo quitado sem valor explícito: a prévia não inventa valor pago.
	payload, err := WhatsAppPreview(ctx, env.deps, WhatsAppKindReceipt, "receipt-1")
	require.NoError(t, err)
	assert.Equal(t, WhatsAppKindReceipt, payload.Type)
	assert.Equal(t, "receipt-1", payload.ID)
	assert.Equal(t, "(11) 99999-9999", payload.Phone)
	assert.Equal(t, "11999999999", payload.FormattedPhone)
	assert.Equal(t, "Maria Souza", payload.TenantName)
	assert.Equal(t, "Apto 1", payload.PropertyName)
	assert.Equal(t, strings.Join([]string{
		"recibo", "Maria Souza", "Apto 1", "10/09/2026", "R$ 1.000,00", "",
		"20/09/2026 10:00", "rent", "Aluguel de setembro",
		"https://aluxen.test/pdf?id=receipt-1&token=receipt-receipt-1-tok",
		"https://aluxen.test/validar/receipt/receipt-1?t=receipt-receipt-1-tok",
		"João Locador", "nil",
	}, "|"), payload.Message)
	assert.Equal(t, "https://wa.me/"+payload.Phone+"?text="+payload.Message, payload.DirectURL)

	// Recibo parcial: o valor pago entra na mensagem e o bruto fica explícito.
	receipt.PaidAmount = domain.FloatPtr(400)
	env.receipts.items = []domain.Receipt{receipt}
	payload, err = WhatsAppPreview(ctx, env.deps, WhatsAppKindReceipt, "receipt-1")
	require.NoError(t, err)
	assert.Contains(t, payload.Message, "|R$ 400,00|")
	assert.True(t, strings.HasSuffix(payload.Message, "|R$ 400,00"), payload.Message)

	// Lançamento sem nome próprio usa o rótulo genérico no texto, mas o painel
	// continua mostrando o campo vazio do lançamento.
	receipt.TenantName = ""
	env.receipts.items = []domain.Receipt{receipt}
	payload, err = WhatsAppPreview(ctx, env.deps, WhatsAppKindReceipt, "receipt-1")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(payload.Message, "recibo|Locatário|"), payload.Message)
	assert.Equal(t, "", payload.TenantName)
}

func TestCcolWhatsAppPreviewLeasePayload(t *testing.T) {
	ctx := context.Background()
	env := ccolEnv(t)
	env.deps.Messages = ccolMessages{}
	env.settings.values[domain.SettingLandlordName] = "João Locador"

	tenant := covTenantOf("tenant-1", "Maria Souza")
	tenant.Phone = domain.StrPtr("21988887777")
	env.tenants.items = []domain.Tenant{tenant}
	lease := covLeaseOf("lease-1", "tenant-1", "property-1", domain.LeaseStatusActive, "2026-01-10", "2026-12-10", 1500)
	lease.TenantName = "Maria Souza"
	lease.PropertyName = "Apto 2"
	env.leases.items = []domain.Lease{lease}

	payload, err := WhatsAppPreview(ctx, env.deps, WhatsAppKindLease, "lease-1")
	require.NoError(t, err)
	assert.Equal(t, WhatsAppKindLease, payload.Type)
	assert.Equal(t, "lease-1", payload.ID)
	assert.Equal(t, "21988887777", payload.Phone)
	assert.Equal(t, strings.Join([]string{
		"contrato", "Maria Souza", "Apto 2", "10/01/2026", "10/12/2026", "10", "R$ 1.500,00",
		"https://aluxen.test/contract-pdf?id=lease-1&token=lease-lease-1-tok",
		"https://aluxen.test/validar/lease/lease-1?t=lease-lease-1-tok",
		"João Locador",
	}, "|"), payload.Message)
	assert.Equal(t, "https://wa.me/"+payload.Phone+"?text="+payload.Message, payload.DirectURL)
}

func TestCcolWhatsAppPreviewRepositoryErrors(t *testing.T) {
	ctx := context.Background()

	// Lançamento inexistente.
	env := ccolEnv(t)
	_, err := WhatsAppPreview(ctx, env.deps, WhatsAppKindReceipt, "receipt-1")
	assert.ErrorIs(t, err, domain.ErrNotFound)

	// Falha ao ler o lançamento.
	env = ccolEnv(t)
	env.receipts.byIDErr = errors.New("banco fora")
	_, err = WhatsAppPreview(ctx, env.deps, WhatsAppKindReceipt, "receipt-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "banco fora")

	// Recibo ainda não pago não é liberado.
	env = ccolEnv(t)
	env.receipts.items = []domain.Receipt{ccolReceipt("receipt-1", "tenant-1", "2026-09-10", 1000)}
	_, err = WhatsAppPreview(ctx, env.deps, WhatsAppKindReceipt, "receipt-1")
	assert.True(t, domain.IsValidation(err))
	assert.Equal(t, "Recibo ainda não pago.", domain.ValidationMessage(err))

	// Falha ao resolver o titular do recibo.
	env = ccolEnv(t)
	env.deps.Messages = ccolMessages{}
	paid := covPaidReceipt(ccolReceipt("receipt-1", "tenant-1", "2026-09-10", 1000), "2026-09-09", 1000)
	env.receipts.items = []domain.Receipt{paid}
	env.tenants.byIDErr = errors.New("locatário indisponível")
	_, err = WhatsAppPreview(ctx, env.deps, WhatsAppKindReceipt, "receipt-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "locatário indisponível")

	// O contrato do recibo manda no titular; recibo sem contrato cai no próprio.
	env = ccolEnv(t)
	env.deps.Messages = ccolMessages{}
	leaseTenant := covTenantOf("tenant-lease", "Maria do Contrato")
	leaseTenant.Phone = domain.StrPtr("21999990000")
	receiptTenant := covTenantOf("tenant-receipt", "Maria do Recibo")
	receiptTenant.Phone = domain.StrPtr("11999991111")
	env.tenants.items = []domain.Tenant{leaseTenant, receiptTenant}
	withLease := paid
	withLease.TenantID = domain.StrPtr("tenant-receipt")
	withLease.LeaseID = domain.StrPtr("lease-1")
	env.leases.items = []domain.Lease{covLeaseOf("lease-1", "tenant-lease", "property-1", domain.LeaseStatusActive, "2026-01-10", "2026-12-10", 1500)}
	env.receipts.items = []domain.Receipt{withLease}

	payload, err := WhatsAppPreview(ctx, env.deps, WhatsAppKindReceipt, "receipt-1")
	require.NoError(t, err)
	assert.Equal(t, "21999990000", payload.Phone)

	// Contrato do recibo já removido: a régua cai no titular do lançamento.
	withLease.LeaseID = domain.StrPtr("lease-removido")
	env.receipts.items = []domain.Receipt{withLease}
	payload, err = WhatsAppPreview(ctx, env.deps, WhatsAppKindReceipt, "receipt-1")
	require.NoError(t, err)
	assert.Equal(t, "11999991111", payload.Phone)

	// Falha real ao ler o contrato é propagada, não confundida com ausência.
	env.leases.byIDErr = errors.New("contratos fora")
	_, err = WhatsAppPreview(ctx, env.deps, WhatsAppKindReceipt, "receipt-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contratos fora")

	// Contrato inexistente.
	env = ccolEnv(t)
	_, err = WhatsAppPreview(ctx, env.deps, WhatsAppKindLease, "lease-1")
	assert.ErrorIs(t, err, domain.ErrNotFound)

	// Falha ao ler o contrato.
	env = ccolEnv(t)
	env.leases.byIDErr = errors.New("contratos fora")
	_, err = WhatsAppPreview(ctx, env.deps, WhatsAppKindLease, "lease-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contratos fora")

	// Falha ao resolver o titular do contrato.
	env = ccolEnv(t)
	env.deps.Messages = ccolMessages{}
	env.leases.items = []domain.Lease{
		covLeaseOf("lease-1", "tenant-1", "property-1", domain.LeaseStatusActive, "2026-01-10", "2026-12-10", 1500),
	}
	env.tenants.byIDErr = errors.New("locatário indisponível")
	_, err = WhatsAppPreview(ctx, env.deps, WhatsAppKindLease, "lease-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "locatário indisponível")

	// Contrato sem assinador configurado também não gera link de autenticidade.
	env = ccolEnv(t)
	env.deps.Messages = ccolMessages{}
	env.deps.Signer = nil
	env.tenants.items = []domain.Tenant{covTenantOf("tenant-1", "Maria Souza")}
	env.leases.items = []domain.Lease{
		covLeaseOf("lease-1", "tenant-1", "property-1", domain.LeaseStatusActive, "2026-01-10", "2026-12-10", 1500),
	}
	_, err = WhatsAppPreview(ctx, env.deps, WhatsAppKindLease, "lease-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "assinador dos tokens")

	// Sem assinador não há link de autenticidade a montar.
	env = ccolEnv(t)
	env.deps.Messages = ccolMessages{}
	env.deps.Signer = nil
	env.tenants.items = []domain.Tenant{covTenantOf("tenant-1", "Maria Souza")}
	env.receipts.items = []domain.Receipt{paid}
	_, err = WhatsAppPreview(ctx, env.deps, WhatsAppKindReceipt, "receipt-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "assinador dos tokens")
}

// ── Job e telefone ────────────────────────────────────────────────

func TestCcolWhatsAppJobStatus(t *testing.T) {
	ctx := context.Background()
	env := ccolEnv(t)
	channel := newCcolChannel()

	_, err := WhatsAppJobStatus(ctx, env.deps, nil, "job-1", "user-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "canal do WhatsApp não configurado")

	job := channel.Create("user-1")
	got, err := WhatsAppJobStatus(ctx, env.deps, channel, "  "+job.ID+"  ", "user-1")
	require.NoError(t, err)
	assert.Equal(t, job.ID, got.ID, "o id é consultado sem os espaços do painel")

	// Job de outro dono é indistinguível de inexistente.
	_, err = WhatsAppJobStatus(ctx, env.deps, channel, job.ID, "user-2")
	assert.ErrorIs(t, err, domain.ErrNotFound)
	_, err = WhatsAppJobStatus(ctx, env.deps, channel, "job-inexistente", "user-1")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestCcolTenantByPhoneAndPhoneMatches(t *testing.T) {
	ctx := context.Background()
	env := ccolEnv(t)
	masked := covTenantOf("tenant-1", "Maria Souza")
	masked.Phone = domain.StrPtr("(11) 99999-9999")
	masked.CPF = domain.StrPtr("123.456.789-09")
	plain := covTenantOf("tenant-2", "João Lima")
	plain.Phone = domain.StrPtr("21988887777")
	env.tenants.items = []domain.Tenant{masked, plain}

	// Telefone curto não consulta o cadastro: não há como casar com DDD.
	env.tenants.listErr = errors.New("banco fora")
	found, err := tenantByPhone(ctx, env.deps, "1199")
	require.NoError(t, err)
	assert.Nil(t, found)
	env.tenants.listErr = nil

	// Cadastro com máscara casa com o dígitos do WhatsApp, com e sem DDI.
	found, err = tenantByPhone(ctx, env.deps, "11999999999")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "tenant-1", found.ID)

	found, err = tenantByPhone(ctx, env.deps, "5511999999999")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "tenant-1", found.ID)

	found, err = tenantByPhone(ctx, env.deps, "5521988887777")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "tenant-2", found.ID)

	// Número de outra pessoa não casa por coincidência de sufixo.
	found, err = tenantByPhone(ctx, env.deps, "11912345678")
	require.NoError(t, err)
	assert.Nil(t, found)

	// Falha da caixa é propagada.
	env.tenants.listErr = errors.New("banco fora")
	_, err = tenantByPhone(ctx, env.deps, "11999999999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "banco fora")

	for _, testCase := range []struct {
		name      string
		candidate string
		phone     string
		want      bool
	}{
		{"cadastro sem telefone", "", "11999999999", false},
		{"iguais", "11999999999", "11999999999", true},
		{"iguais e curtos", "1199", "1199", true},
		{"cadastro curto demais", "1199", "11999999999", false},
		{"entrada curta demais", "11999999999", "1199", false},
		{"entrada com DDI", "11999999999", "5511999999999", true},
		{"cadastro com DDI", "5511999999999", "11999999999", true},
		{"números diferentes", "11999999999", "21999999999", false},
	} {
		assert.Equal(t, testCase.want, phoneMatches(testCase.candidate, testCase.phone), testCase.name)
	}
}

// ── Metadados, trilha e corte de texto ────────────────────────────

func TestCcolWhatsAppMessageMetadata(t *testing.T) {
	stamp := time.Date(2026, 9, 20, 15, 4, 5, 0, time.UTC)
	metadata := whatsAppMessageMetadata(IncomingMessage{
		Event: "message", Session: "default", ContactName: "Maria",
		FromMe: true, HasMedia: true, Timestamp: stamp,
	})
	require.NotNil(t, metadata)
	var fields map[string]any
	require.NoError(t, json.Unmarshal([]byte(*metadata), &fields))
	assert.Equal(t, "message", fields["event"])
	assert.Equal(t, "default", fields["session"])
	assert.Equal(t, "Maria", fields["contactName"])
	assert.Equal(t, true, fields["fromMe"])
	assert.Equal(t, true, fields["hasMedia"])
	assert.Equal(t, "2026-09-20T15:04:05.000Z", fields["whatsappAt"])

	// Webhook sem instante não inventa data.
	metadata = whatsAppMessageMetadata(IncomingMessage{Event: "message"})
	require.NotNil(t, metadata)
	require.NoError(t, json.Unmarshal([]byte(*metadata), &fields))
	assert.Equal(t, "", fields["whatsappAt"])

	assert.Equal(t, "", timeLabel(time.Time{}))
	assert.Equal(t, "2026-09-20T15:04:05.000Z", timeLabel(stamp))
}

func TestCcolRecordWhatsAppSendAudits(t *testing.T) {
	ctx := context.Background()
	request := WhatsAppSendRequest{UserID: "user-1", To: "11999999999", Message: "oi", Actor: covCtx()}

	env := ccolEnv(t)
	recordWhatsAppSend(ctx, env.deps, request, "job-1", "11999999999")
	entry := ccolOnly(t, env, domain.ActionWhatsAppSend)
	assert.Equal(t, domain.EntityWhatsApp, entry.Entity)
	assert.Equal(t, "job-1", domain.Str(entry.EntityID))
	assert.Equal(t, "11999999999", domain.Str(entry.EntityLabel))
	assert.Equal(t, "Mensagem enviada para 11999999999 pelo WhatsApp", entry.Summary)
	metadata := ccolMetadata(t, entry)
	assert.Equal(t, "11999999999", metadata["phone"])
	assert.Equal(t, "job-1", metadata["jobId"])
	assert.Equal(t, domain.AuditOriginWeb, entry.Origin)

	env = ccolEnv(t)
	recordWhatsAppSendFailure(ctx, env.deps, request, "job-2", "11999999999", strings.Repeat("x", 400))
	entry = ccolOnly(t, env, domain.ActionWhatsAppSendFailed)
	assert.Equal(t, "job-2", domain.Str(entry.EntityID))
	assert.Equal(t, "Falha ao enviar mensagem para 11999999999 pelo WhatsApp", entry.Summary)
	metadata = ccolMetadata(t, entry)
	assert.Equal(t, "job-2", metadata["jobId"])
	reason, ok := metadata["reason"].(string)
	require.True(t, ok)
	assert.Len(t, reason, maintenanceDescriptionLimit)

	// Falha ao auditar é registrada no log e não vira erro para quem chamou.
	env = ccolEnv(t)
	env.audit.insertErr = errors.New("trilha fora")
	recordWhatsAppSend(ctx, env.deps, request, "job-3", "11999999999")
	recordWhatsAppSendFailure(ctx, env.deps, request, "job-3", "11999999999", "WAHA indisponível")
	assert.Empty(t, env.audit.entries)
}

func TestCcolTruncate(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		value string
		limit int
		want  string
	}{
		{"abaixo do limite", "abc", 5, "abc"},
		{"exatamente no limite", "abcdef", 6, "abcdef"},
		{"corta no limite", "abcdef", 3, "abc"},
		{"vazio", "", 0, ""},
		{"tudo", strings.Repeat("a", 300), 300, strings.Repeat("a", 300)},
		{"um a menos", strings.Repeat("a", 300), 299, strings.Repeat("a", 299)},
	} {
		assert.Equal(t, testCase.want, truncate(testCase.value, testCase.limit), testCase.name)
	}
}

func TestCcolLandlordNameAndPaidAmountLabel(t *testing.T) {
	ctx := context.Background()

	env := ccolEnv(t)
	env.settings.values[domain.SettingLandlordName] = "João Locador"
	assert.Equal(t, "João Locador", landlordName(ctx, env.deps))

	env = ccolEnv(t)
	assert.Equal(t, "", landlordName(ctx, env.deps), "locador não configurado")

	env = ccolEnv(t)
	env.settings.getErr = errors.New("configurações fora")
	assert.Equal(t, "", landlordName(ctx, env.deps), "falha ao ler não derruba a mensagem")

	assert.Equal(t, "", paidAmountLabel(&domain.Receipt{}))
	assert.Equal(t, "R$ 0,00", paidAmountLabel(&domain.Receipt{PaidAmount: domain.FloatPtr(0)}))
	assert.Equal(t, "R$ 1.234,50", paidAmountLabel(&domain.Receipt{PaidAmount: domain.FloatPtr(1234.5)}))
	assert.Equal(t, "-R$ 10,00", paidAmountLabel(&domain.Receipt{PaidAmount: domain.FloatPtr(-10)}))
}
