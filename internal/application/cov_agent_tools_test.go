package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cobertura das ferramentas tipadas do atendimento (`agent_tools.go`) e da
// leitura das conversas (`agent.go`). Todo símbolo auxiliar leva o prefixo
// `cagt` para não colidir com os dublês do harness (`cov`) nem com os testes
// já existentes (`ag`).
//
// O que se prova aqui é o comportamento observável: o que a ferramenta devolve
// ao modelo, o que ficou gravado na trilha, o que o titular de outra conversa
// nunca vê e como a janela de mensagens do painel é limitada.

// ── Dublês locais do lote `cagt` ───────────────────────────────────

// cagtChat é o provedor de IA roteirizado: devolve as respostas combinadas, na
// ordem, e guarda cada pedido — é assim que o teste confere o que o modelo
// recebeu de volta da ferramenta.
type cagtChat struct {
	responses []ChatResponse
	requests  []ChatRequest
}

func (c *cagtChat) Complete(_ context.Context, request ChatRequest) (ChatResponse, error) {
	c.requests = append(c.requests, request)
	if len(c.responses) == 0 {
		return ChatResponse{}, errors.New("dublê do provedor sem resposta combinada")
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

// cagtAuditFalha é a trilha que falha ao listar. O harness só injeta falha de
// gravação; a leitura das conversas depende do histórico da régua.
type cagtAuditFalha struct {
	*covAudit
	listErr error
}

func (a *cagtAuditFalha) List(ctx context.Context, filter domain.AuditFilter) ([]domain.AuditLog, error) {
	if a.listErr != nil {
		return nil, a.listErr
	}
	return a.covAudit.List(ctx, filter)
}

// ── Auxiliares do lote `cagt` ──────────────────────────────────────

// cagtTenant devolve o titular padrão das ferramentas, com telefone cadastrado.
func cagtTenant(name string) domain.Tenant {
	return domain.Tenant{ID: "cagt-tenant", Name: name, Phone: domain.StrPtr("(21) 99999-9999")}
}

// cagtMessage monta uma mensagem recebida do titular.
func cagtMessage(tenantID, id, createdAt string) domain.WhatsAppMessage {
	return domain.WhatsAppMessage{
		ID:        id,
		CreatedAt: createdAt,
		Direction: domain.WhatsAppDirectionIn,
		Phone:     "5511999999999",
		TenantID:  domain.StrPtr(tenantID),
		Body:      "Olá",
		Status:    domain.WhatsAppStatusReceived,
	}
}

func cagtAddTenant(t *testing.T, env *covEnv, tenant domain.Tenant) {
	t.Helper()
	require.NoError(t, env.tenants.Create(context.Background(), &tenant))
}

func cagtAddLease(t *testing.T, env *covEnv, lease domain.Lease) {
	t.Helper()
	require.NoError(t, env.leases.Create(context.Background(), &lease))
}

func cagtAddReceipt(t *testing.T, env *covEnv, receipt domain.Receipt) {
	t.Helper()
	require.NoError(t, env.receipts.Create(context.Background(), &receipt))
}

func cagtAddMessage(t *testing.T, env *covEnv, message domain.WhatsAppMessage) {
	t.Helper()
	_, err := env.messages.InsertIfAbsent(context.Background(), &message)
	require.NoError(t, err)
}

// cagtMetadata decodifica os metadados gravados na trilha (a coluna é JSON).
func cagtMetadata(t *testing.T, entry domain.AuditLog) map[string]any {
	t.Helper()
	require.NotNil(t, entry.Metadata, "evento sem metadados")
	var fields map[string]any
	require.NoError(t, json.Unmarshal([]byte(*entry.Metadata), &fields))
	return fields
}

// cagtCall despacha uma chamada do modelo pelo caminho real do atendimento.
func cagtCall(t *testing.T, env *covEnv, tenant *domain.Tenant, call ChatToolCall) (agentToolResult, error) {
	t.Helper()
	return runAgentTool(context.Background(), env.deps, covCtx(), tenant, call)
}

// ── Lista fechada e despacho ───────────────────────────────────────

// A lista de ferramentas é fechada: nome fora dela não resolve, e a resolução é
// exata — nunca por aproximação.
func TestCagtAgentToolByNameResolvesOnlyTheDeclaredNames(t *testing.T) {
	for _, tool := range agentToolSet {
		resolved, ok := agentToolByName(tool.Name)
		require.True(t, ok, "ferramenta declarada %s deve resolver", tool.Name)
		assert.Equal(t, tool.Name, resolved.Name)
	}

	for _, name := range []string{"", "deleteTenant", "openReceipts", "OpenReceiptsOf", "receiptStatusOf "} {
		if _, ok := agentToolByName(name); ok {
			t.Fatalf("nome %q não pertence à lista fechada e não pode resolver", name)
		}
	}
}

// Ferramenta fora da lista é recusada antes de qualquer leitura: nenhum dado
// sai do banco e nada é gravado na trilha.
func TestCagtRunAgentToolRefusesToolOutsideTheClosedList(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")

	_, err := cagtCall(t, env, &tenant, ChatToolCall{
		Name:      "  deleteTenant  ",
		Arguments: map[string]any{"id": tenant.ID},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deleteTenant")
	assert.Contains(t, err.Error(), "não faz parte do atendimento")
	assert.Empty(t, env.audit.entries, "ferramenta recusada não pode gravar trilha")
}

// O atendimento sempre recebe o titular resolvido pelo caso de uso; sem ele a
// ferramenta não roda.
func TestCagtRunAgentToolRequiresTheTenantOfTheConversation(t *testing.T) {
	env := newCovEnv(t)

	_, err := cagtCall(t, env, nil, ChatToolCall{Name: agentToolOpenReceipts})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "não tem titular")
}

// Chamada sem argumentos é aceita pelas ferramentas que não declaram nenhum, e
// o resultado é o dado real do banco.
func TestCagtRunAgentToolRunsKnownToolWithoutArguments(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")
	cagtAddReceipt(t, env, covReceiptOf("r1", tenant.ID, "l1", "2026-09-01", 1500))

	result, err := cagtCall(t, env, &tenant, ChatToolCall{Name: agentToolOpenReceipts})
	require.NoError(t, err)
	assert.Contains(t, result.Data, "id r1")
	assert.Contains(t, result.Data, format.FormatCurrency(1500))
}

// O contrato sai da ferramenta tipada: o modelo recebe o dado do titular, sem
// escolher contrato por identificador.
func TestCagtRunAgentToolReturnsTheContractOfTheTenant(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")
	lease := covLeaseOf("l1", tenant.ID, "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 1800)
	lease.PropertyName = "Casa do Centro"
	cagtAddLease(t, env, lease)

	result, err := cagtCall(t, env, &tenant, ChatToolCall{Name: agentToolContract})
	require.NoError(t, err)
	assert.Contains(t, result.Data, "Casa do Centro")
	assert.Contains(t, result.Data, format.FormatCurrency(1800))
	assert.Contains(t, result.Data, domain.LeaseStatusLabel(domain.LeaseStatusActive))
}

// Argumento que a ferramenta não declara é recusado — é essa barreira que
// impede o modelo de apontar outro titular.
func TestCagtRunAgentToolValidatesArgumentsBeforeAnyRead(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")

	cases := []struct {
		name string
		call ChatToolCall
		want string
	}{
		{
			"lançamentos com titular escolhido pelo modelo",
			ChatToolCall{Name: agentToolOpenReceipts, Arguments: map[string]any{"tenantId": "cagt-outro"}},
			"não aceita o argumento",
		},
		{
			"contrato com identificador escolhido pelo modelo",
			ChatToolCall{Name: agentToolContract, Arguments: map[string]any{"leaseId": "l1"}},
			"não aceita o argumento",
		},
		{
			"situação sem identificador",
			ChatToolCall{Name: agentToolReceiptStatus, Arguments: map[string]any{}},
			"exige o argumento",
		},
		{
			"situação com identificador não textual",
			ChatToolCall{Name: agentToolReceiptStatus, Arguments: map[string]any{"receiptId": 7}},
			"deve ser texto",
		},
		{
			"manutenção sem descrição",
			ChatToolCall{Name: agentToolMaintenance, Arguments: map[string]any{}},
			"exige o argumento",
		},
		{
			"manutenção com descrição vazia",
			ChatToolCall{Name: agentToolMaintenance, Arguments: map[string]any{"description": "   "}},
			"está vazio",
		},
		{
			"manutenção com descrição curta demais",
			ChatToolCall{Name: agentToolMaintenance, Arguments: map[string]any{"description": "ok"}},
			"está vazio",
		},
		{
			"escalonamento com argumento extra",
			ChatToolCall{Name: agentToolEscalate, Arguments: map[string]any{"reason": "fora do escopo", "tenantId": "cagt-outro"}},
			"não aceita o argumento",
		},
		{
			"escalonamento sem motivo",
			ChatToolCall{Name: agentToolEscalate, Arguments: map[string]any{}},
			"exige o argumento",
		},
		{
			"escalonamento com motivo em branco",
			ChatToolCall{Name: agentToolEscalate, Arguments: map[string]any{"reason": "   "}},
			"está vazio",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cagtCall(t, env, &tenant, tc.call)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
	assert.Empty(t, env.audit.entries, "chamada recusada não pode gravar trilha")
}

// O escalonamento pedido pelo modelo carrega o motivo normalizado.
func TestCagtRunAgentToolEscalatesThroughTheTypedTool(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")

	result, err := cagtCall(t, env, &tenant, ChatToolCall{
		Name:      agentToolEscalate,
		Arguments: map[string]any{"reason": "  assunto fora do escopo  "},
	})
	require.NoError(t, err)
	assert.True(t, result.Escalate)
	assert.Equal(t, "assunto fora do escopo", result.Reason)
}

// ── Lançamentos em aberto ──────────────────────────────────────────

// Pago não aparece; parcial aparece, porque ainda há saldo. A ordem é a do
// vencimento e o dado sai formatado para leitura.
func TestCagtOpenReceiptsOfListsOnlyWhatIsStillOpen(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")

	paid := covPaidReceipt(covReceiptOf("r-pago", tenant.ID, "l1", "2026-08-10", 900), "2026-08-09", 900)
	partial := covPaidReceipt(covReceiptOf("r-parcial", tenant.ID, "l1", "2026-09-15", 900), "2026-09-14", 400)
	partial.Description = domain.StrPtr("Aluguel de setembro")
	cagtAddReceipt(t, env, paid)
	cagtAddReceipt(t, env, partial)
	cagtAddReceipt(t, env, covReceiptOf("r-atrasado", tenant.ID, "l1", "2026-09-01", 900))
	cagtAddReceipt(t, env, covReceiptOf("r-futuro", tenant.ID, "l1", "2026-10-10", 900))

	result, err := openReceiptsOf(context.Background(), env.deps, &tenant)
	require.NoError(t, err)
	assert.NotContains(t, result.Data, "r-pago", "lançamento quitado não é pendência")

	lines := strings.Split(strings.TrimPrefix(result.Data, "Lançamentos em aberto:\n"), "\n")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "id r-atrasado")
	assert.Contains(t, lines[0], domain.ReceiptStatusLabel(domain.ReceiptStatusOverdue))
	assert.Contains(t, lines[0], format.FormatDateBR("2026-09-01"))
	assert.Contains(t, lines[1], "id r-parcial")
	assert.Contains(t, lines[1], "Aluguel de setembro")
	assert.Contains(t, lines[1], domain.ReceiptStatusLabel(domain.ReceiptStatusPartial))
	assert.Contains(t, lines[1], "valor já pago "+format.FormatCurrency(400))
	assert.Contains(t, lines[2], "id r-futuro")
	assert.Contains(t, lines[2], domain.ReceiptStatusLabel(domain.ReceiptStatusDue))
	assert.Contains(t, lines[2], format.FormatCurrency(900))
}

// Sem pendência, a ferramenta diz que não achou — nunca estima.
func TestCagtOpenReceiptsOfWithoutOpenReceipts(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")
	cagtAddReceipt(t, env, covPaidReceipt(covReceiptOf("r1", tenant.ID, "l1", "2026-09-01", 900), "2026-09-01", 900))

	result, err := openReceiptsOf(context.Background(), env.deps, &tenant)
	require.NoError(t, err)
	assert.Equal(t, "Nenhum lançamento em aberto para este titular.", result.Data)
}

// Vencimento empatado desempata pelo identificador: a lista é estável.
func TestCagtOpenReceiptsOfOrdersByIDWhenDueDatesTie(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")
	cagtAddReceipt(t, env, covReceiptOf("r-b", tenant.ID, "l1", "2026-10-10", 100))
	cagtAddReceipt(t, env, covReceiptOf("r-a", tenant.ID, "l1", "2026-10-10", 100))

	result, err := openReceiptsOf(context.Background(), env.deps, &tenant)
	require.NoError(t, err)
	if strings.Index(result.Data, "id r-a") > strings.Index(result.Data, "id r-b") {
		t.Fatalf("ordem por identificador esperada, veio %q", result.Data)
	}
}

// A consulta tem teto: o que passa dele é resumido, não omitido em silêncio.
func TestCagtOpenReceiptsOfCapsTheListing(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")
	for index := 0; index < agentReceiptLimit+2; index++ {
		cagtAddReceipt(t, env, covReceiptOf(fmt.Sprintf("r-%02d", index), tenant.ID, "l1", "2026-10-10", 100))
	}

	result, err := openReceiptsOf(context.Background(), env.deps, &tenant)
	require.NoError(t, err)
	assert.Equal(t, agentReceiptLimit, strings.Count(result.Data, "id r-"))
	assert.Contains(t, result.Data, "... e mais 2 lançamento(s) em aberto.")
}

func TestCagtOpenReceiptsOfReportsRepositoryFailure(t *testing.T) {
	env := newCovEnv(t)
	env.receipts.byTenantErr = errors.New("banco fora")
	tenant := cagtTenant("Maria")

	_, err := openReceiptsOf(context.Background(), env.deps, &tenant)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listar lançamentos do titular")
}

// ── Contrato ───────────────────────────────────────────────────────

// O contrato ativo manda, mesmo com contrato encerrado na mesma lista.
func TestCagtContractOfPrefersTheActiveLease(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")
	ended := covLeaseOf("l-antigo", tenant.ID, "p1", domain.LeaseStatusTerminated, "2024-01-01", "2024-12-31", 700)
	ended.PropertyName = "Casa Antiga"
	active := covLeaseOf("l-atual", tenant.ID, "p2", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 1800)
	active.PropertyName = "Casa Atual"
	cagtAddLease(t, env, ended)
	cagtAddLease(t, env, active)

	result, err := contractOf(context.Background(), env.deps, &tenant)
	require.NoError(t, err)
	assert.Contains(t, result.Data, "Casa Atual")
	assert.NotContains(t, result.Data, "Casa Antiga")
	assert.Contains(t, result.Data, format.FormatCurrency(1800))
	assert.Contains(t, result.Data, format.FormatDateBR("2026-01-01"))
	assert.Contains(t, result.Data, format.FormatDateBR("2026-12-31"))
	assert.Contains(t, result.Data, "vencimento todo dia 10")
	assert.Contains(t, result.Data, domain.LeaseStatusLabel(domain.LeaseStatusActive))
}

// Encerrado só aparece quando é o único contrato; suspenso é o vigente quando
// não há ativo.
func TestCagtContractOfUsesTheVigentLeaseWhenThereIsNoActiveOne(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")
	ended := covLeaseOf("l-antigo", tenant.ID, "p1", domain.LeaseStatusTerminated, "2024-01-01", "2024-12-31", 700)
	ended.PropertyName = "Casa Antiga"
	suspended := covLeaseOf("l-suspenso", tenant.ID, "p2", domain.LeaseStatusSuspended, "2026-01-01", "2026-12-31", 900)
	suspended.PropertyName = "Casa Suspensa"
	cagtAddLease(t, env, ended)
	cagtAddLease(t, env, suspended)

	result, err := contractOf(context.Background(), env.deps, &tenant)
	require.NoError(t, err)
	assert.Contains(t, result.Data, "Casa Suspensa")
	assert.NotContains(t, result.Data, "Casa Antiga")
	assert.Contains(t, result.Data, domain.LeaseStatusLabel(domain.LeaseStatusSuspended))
}

func TestCagtContractOfWithoutContract(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")
	ended := covLeaseOf("l-antigo", tenant.ID, "p1", domain.LeaseStatusTerminated, "2024-01-01", "2024-12-31", 700)
	cagtAddLease(t, env, ended)

	result, err := contractOf(context.Background(), env.deps, &tenant)
	require.NoError(t, err)
	assert.Equal(t, "Nenhum contrato encontrado para este titular.", result.Data)
}

func TestCagtContractOfReportsRepositoryFailure(t *testing.T) {
	env := newCovEnv(t)
	env.leases.byTenantErr = errors.New("banco fora")
	tenant := cagtTenant("Maria")

	_, err := contractOf(context.Background(), env.deps, &tenant)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listar contratos do titular")
}

// ── Situação de um lançamento ──────────────────────────────────────

// O lançamento do próprio titular volta com valor, vencimento e situação.
func TestCagtReceiptStatusOfReturnsTheReceiptOfTheTenant(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")
	cagtAddTenant(t, env, tenant)
	receipt := covReceiptOf("r1", tenant.ID, "", "2026-09-01", 900)
	receipt.LeaseID = nil
	cagtAddReceipt(t, env, receipt)

	result, err := receiptStatusOf(context.Background(), env.deps, &tenant, "r1")
	require.NoError(t, err)
	assert.Contains(t, result.Data, "id r1")
	assert.Contains(t, result.Data, format.FormatCurrency(900))
	assert.Contains(t, result.Data, format.FormatDateBR("2026-09-01"))
	assert.Contains(t, result.Data, domain.ReceiptStatusLabel(domain.ReceiptStatusOverdue))
}

// Lançamento de outro titular é recusado com a mesma resposta do inexistente:
// o atendimento não confirma dado de outra pessoa.
func TestCagtReceiptStatusOfRefusesAnotherTenantsReceipt(t *testing.T) {
	env := newCovEnv(t)
	holder := cagtTenant("Maria")
	other := covTenantOf("cagt-outro", "Pedro")
	cagtAddTenant(t, env, other)

	lease := covLeaseOf("l-pedro", other.ID, "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
	cagtAddLease(t, env, lease)
	// O titular do lançamento sai do contrato: é o mesmo coalesce do app.
	cagtAddReceipt(t, env, covReceiptOf("r-pedro", "", "l-pedro", "2026-09-01", 900))

	_, foreignErr := receiptStatusOf(context.Background(), env.deps, &holder, "r-pedro")
	require.Error(t, foreignErr)

	_, missingErr := receiptStatusOf(context.Background(), env.deps, &holder, "r-inexistente")
	require.Error(t, missingErr)

	assert.Equal(t, missingErr.Error(), foreignErr.Error(),
		"recusar dado de outra pessoa não pode denunciar que o lançamento existe")
	assert.NotContains(t, foreignErr.Error(), "Pedro")
	assert.Contains(t, foreignErr.Error(), "não encontrado")
}

// Lançamento sem titular resolvido também é recusado: sem dono não é dado do
// titular desta conversa.
func TestCagtReceiptStatusOfRefusesOrphanReceipt(t *testing.T) {
	env := newCovEnv(t)
	holder := cagtTenant("Maria")
	orphan := covReceiptOf("r-orfao", "", "", "2026-09-01", 900)
	orphan.TenantID = nil
	orphan.LeaseID = nil
	cagtAddReceipt(t, env, orphan)

	_, err := receiptStatusOf(context.Background(), env.deps, &holder, "r-orfao")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "não encontrado")
}

func TestCagtReceiptStatusOfReportsLookupFailures(t *testing.T) {
	t.Run("lançamento ilegível", func(t *testing.T) {
		env := newCovEnv(t)
		env.receipts.byIDErr = errors.New("banco fora")
		tenant := cagtTenant("Maria")

		_, err := receiptStatusOf(context.Background(), env.deps, &tenant, "r1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "buscar lançamento do titular")
	})

	t.Run("contrato do lançamento ilegível", func(t *testing.T) {
		env := newCovEnv(t)
		tenant := cagtTenant("Maria")
		cagtAddReceipt(t, env, covReceiptOf("r1", tenant.ID, "l1", "2026-09-01", 900))
		env.leases.byIDErr = errors.New("banco fora")

		_, err := receiptStatusOf(context.Background(), env.deps, &tenant, "r1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "banco fora")
	})
}

// ── Manutenção e escalonamento ─────────────────────────────────────

// A solicitação fica na trilha com o problema nas palavras do locatário, o
// titular e o telefone mascarado — é o que o locador lê depois.
func TestCagtOpenMaintenanceRequestRecordsTheTrail(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")
	tenant.Phone = domain.StrPtr("(21) 98888-7777")

	result, err := cagtCall(t, env, &tenant, ChatToolCall{
		Name:      agentToolMaintenance,
		Arguments: map[string]any{"description": "  A torneira da cozinha está vazando.  "},
	})
	require.NoError(t, err)
	assert.Contains(t, result.Data, "A torneira da cozinha está vazando.")

	require.Len(t, env.audit.entries, 1)
	entry := env.audit.entries[0]
	assert.Equal(t, domain.ActionMaintenanceOpened, entry.Action)
	assert.Equal(t, domain.EntityWhatsApp, entry.Entity)
	assert.Equal(t, tenant.ID, domain.Str(entry.EntityID))
	assert.Equal(t, tenant.Name, domain.Str(entry.EntityLabel))
	metadata := cagtMetadata(t, entry)
	assert.Equal(t, "****7777", metadata["phone"], "telefone do locatário não vai inteiro para a trilha")
	assert.Equal(t, agentKindMaintenance, metadata["kind"])
	assert.Equal(t, "A torneira da cozinha está vazando.", metadata["reason"])
}

// Descrição longa é cortada na trilha, nunca motivo de recusa.
func TestCagtOpenMaintenanceRequestTruncatesTheDescription(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")
	description := strings.Repeat("vazamento ", 60)

	result, err := openMaintenanceRequest(context.Background(), env.deps, covCtx(), &tenant, description)
	require.NoError(t, err)

	require.Len(t, env.audit.entries, 1)
	assert.Equal(t, description[:maintenanceDescriptionLimit], cagtMetadata(t, env.audit.entries[0])["reason"])
	assert.Contains(t, result.Data, description[:maintenanceDescriptionLimit])
}

func TestCagtOpenMaintenanceRequestReportsAuditFailure(t *testing.T) {
	env := newCovEnv(t)
	env.audit.insertErr = errors.New("trilha fora")
	tenant := cagtTenant("Maria")

	_, err := openMaintenanceRequest(context.Background(), env.deps, covCtx(), &tenant, "torneira vazando")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "registrar solicitação de manutenção")
}

// Motivo em branco não deixa o encaminhamento sem razão registrada; motivo
// longo é cortado no teto.
func TestCagtEscalateToLandlordKeepsTheReasonBounded(t *testing.T) {
	blank := escalateToLandlord("   ")
	assert.True(t, blank.Escalate)
	assert.Equal(t, agentReasonUngrounded, blank.Reason)

	trimmed := escalateToLandlord("  assunto fora do escopo  ")
	assert.True(t, trimmed.Escalate)
	assert.Equal(t, "assunto fora do escopo", trimmed.Reason)

	long := escalateToLandlord(strings.Repeat("x", agentEscalationReasonLimit+50))
	assert.Equal(t, strings.Repeat("x", agentEscalationReasonLimit), long.Reason)
}

// ── Auxiliares das ferramentas ─────────────────────────────────────

func TestCagtTextArgValidatesAndTrims(t *testing.T) {
	_, err := textArg("cagt-tool", map[string]any{}, "k", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exige o argumento")

	_, err = textArg("cagt-tool", map[string]any{"k": 3}, "k", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deve ser texto")

	_, err = textArg("cagt-tool", map[string]any{"k": "   "}, "k", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "está vazio")

	_, err = textArg("cagt-tool", map[string]any{"k": "ab"}, "k", 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "está vazio")

	trimmed, err := textArg("cagt-tool", map[string]any{"k": "  abc  "}, "k", 3)
	require.NoError(t, err)
	assert.Equal(t, "abc", trimmed)

	long, err := textArg("cagt-tool", map[string]any{"k": strings.Repeat("a", agentTextArgLimit+100)}, "k", 1)
	require.NoError(t, err)
	assert.Len(t, long, agentTextArgLimit)
}

// O nome do lançamento é a descrição quando existe, senão a categoria.
func TestCagtReceiptLabelPrefersDescriptionOverCategory(t *testing.T) {
	described := covReceiptOf("r1", "t1", "l1", "2026-09-01", 900)
	described.Description = domain.StrPtr("  Aluguel de setembro  ")
	assert.Equal(t, "Aluguel de setembro", receiptLabel(&described))

	without := covReceiptOf("r2", "t1", "l1", "2026-09-01", 900)
	assert.Equal(t, domain.ReceiptCategoryLabel(domain.ReceiptCategoryRent), receiptLabel(&without))

	blank := covReceiptOf("r3", "t1", "l1", "2026-09-01", 900)
	blank.Description = domain.StrPtr("   ")
	assert.Equal(t, domain.ReceiptCategoryLabel(domain.ReceiptCategoryRent), receiptLabel(&blank))
}

// A linha do lançamento é o material que o modelo tem para responder: valor,
// vencimento, situação e, quando houver, o que já foi pago.
func TestCagtReceiptLineDescribesTheReceiptForTheModel(t *testing.T) {
	receipt := covReceiptOf("r1", "t1", "l1", "2026-09-01", 900)
	receipt.Description = domain.StrPtr("Aluguel de setembro")

	line := receiptLine(&receipt, covToday())
	assert.Contains(t, line, "id r1")
	assert.Contains(t, line, "Aluguel de setembro")
	assert.Contains(t, line, format.FormatCurrency(900))
	assert.Contains(t, line, format.FormatDateBR("2026-09-01"))
	assert.Contains(t, line, domain.ReceiptStatusLabel(domain.ReceiptStatusOverdue))
	assert.NotContains(t, line, "valor já pago")

	partial := covPaidReceipt(receipt, "2026-09-14", 400)
	assert.Contains(t, receiptLine(&partial, covToday()), "valor já pago "+format.FormatCurrency(400))
}

// ── Conversas do painel ────────────────────────────────────────────

// A janela de mensagens tem padrão e teto: o painel não lê a conversa inteira
// nem aceita pedido sem limite.
func TestCagtListConversationsBoundsTheMessageWindow(t *testing.T) {
	t.Run("sem limite usa o padrão", func(t *testing.T) {
		env := newCovEnv(t)
		tenant := cagtTenant("Maria")
		cagtAddTenant(t, env, tenant)
		for index := 0; index < DefaultConversationLimit+10; index++ {
			cagtAddMessage(t, env, cagtMessage(tenant.ID, fmt.Sprintf("m-%03d", index), "2026-09-21 09:00:00"))
		}

		rows, err := ListConversations(context.Background(), env.deps, 0)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, DefaultConversationLimit, rows[0].MessageCount)
	})

	t.Run("limite acima do teto é cortado", func(t *testing.T) {
		env := newCovEnv(t)
		tenant := cagtTenant("Maria")
		cagtAddTenant(t, env, tenant)
		for index := 0; index < MaxConversationLimit+5; index++ {
			cagtAddMessage(t, env, cagtMessage(tenant.ID, fmt.Sprintf("m-%03d", index), "2026-09-21 09:00:00"))
		}

		rows, err := ListConversations(context.Background(), env.deps, MaxConversationLimit+50)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, MaxConversationLimit, rows[0].MessageCount)
	})
}

// Titular sem mensagem não é conversa, e lançamento em atraso sem titular não
// pertence a conversa nenhuma.
func TestCagtListConversationsSkipsTenantsWithoutMessages(t *testing.T) {
	env := newCovEnv(t)
	talker := cagtTenant("Maria")
	quiet := covTenantOf("cagt-quiet", "Sem Conversa")
	cagtAddTenant(t, env, talker)
	cagtAddTenant(t, env, quiet)
	cagtAddMessage(t, env, cagtMessage(talker.ID, "m1", "2026-09-21 09:00:00"))

	orphan := covReceiptOf("r-orfao", "", "", "2026-09-01", 900)
	orphan.TenantID = nil
	cagtAddReceipt(t, env, orphan)

	rows, err := ListConversations(context.Background(), env.deps, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, talker.ID, rows[0].TenantID)
	assert.Zero(t, rows[0].OverdueCount)
}

// A conversa mais recente vem primeiro; sem mensagem datada que desempate, o
// nome decide — a lista é estável entre leituras.
func TestCagtListConversationsOrdersByRecencyThenName(t *testing.T) {
	env := newCovEnv(t)
	old := covTenantOf("cagt-old", "Zeta")
	ana := covTenantOf("cagt-ana", "Ana")
	bruno := covTenantOf("cagt-bruno", "Bruno")
	cagtAddTenant(t, env, old)
	cagtAddTenant(t, env, ana)
	cagtAddTenant(t, env, bruno)
	cagtAddMessage(t, env, cagtMessage(old.ID, "m-old", "2026-09-19 08:00:00"))
	cagtAddMessage(t, env, cagtMessage(ana.ID, "m-ana", "2026-09-21 09:00:00"))
	cagtAddMessage(t, env, cagtMessage(bruno.ID, "m-bruno", "2026-09-21 09:00:00"))

	rows, err := ListConversations(context.Background(), env.deps, 10)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, []string{"Ana", "Bruno", "Zeta"},
		[]string{rows[0].TenantName, rows[1].TenantName, rows[2].TenantName})
}

// O atraso do titular entra na conversa: contagem, total e o estágio que a
// régua sustenta para o lançamento mais atrasado.
func TestCagtListConversationsSummarizesTheOverdueOfTheConversation(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")
	cagtAddTenant(t, env, tenant)
	cagtAddMessage(t, env, cagtMessage(tenant.ID, "m1", "2026-09-21 09:00:00"))
	cagtAddReceipt(t, env, covReceiptOf("r-atraso", tenant.ID, "l1", "2026-09-01", 900))
	cagtAddReceipt(t, env, covPaidReceipt(covReceiptOf("r-pago", tenant.ID, "l1", "2026-08-01", 900), "2026-08-01", 900))
	cagtAddReceipt(t, env, covReceiptOf("r-futuro", tenant.ID, "l1", "2026-10-10", 900))

	rows, err := ListConversations(context.Background(), env.deps, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].OverdueCount, "só o lançamento vencido conta")
	assert.Equal(t, 900.0, rows[0].OverdueTotal)

	require.NotNil(t, rows[0].Overdue)
	assert.Equal(t, "r-atraso", rows[0].Overdue.ReceiptID)
	assert.Equal(t, 20, rows[0].Overdue.DaysLate)
	assert.Equal(t, domain.CollectionStageInstallment, rows[0].Overdue.Stage)
	assert.NotEmpty(t, rows[0].Overdue.StageLabel)
	assert.Empty(t, rows[0].Overdue.StageSentLabel, "nada foi cobrado ainda")
	assert.True(t, rows[0].Overdue.StagePending, "o estágio sustentado pelo atraso ainda falta cobrar")
}

func TestCagtListConversationsReportsRepositoryFailures(t *testing.T) {
	t.Run("titulares ilegíveis", func(t *testing.T) {
		env := newCovEnv(t)
		env.tenants.listErr = errors.New("banco fora")

		_, err := ListConversations(context.Background(), env.deps, 10)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "listar titulares das conversas")
	})

	t.Run("lançamentos ilegíveis", func(t *testing.T) {
		env := newCovEnv(t)
		env.receipts.listErr = errors.New("banco fora")

		_, err := ListConversations(context.Background(), env.deps, 10)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "listar lançamentos das conversas")
	})

	t.Run("histórico da régua ilegível", func(t *testing.T) {
		env := newCovEnv(t)
		env.deps.Repos.Audit = &cagtAuditFalha{covAudit: env.audit, listErr: errors.New("trilha fora")}

		_, err := ListConversations(context.Background(), env.deps, 10)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "listar cobranças enviadas")
	})

	t.Run("mensagens do titular ilegíveis", func(t *testing.T) {
		env := newCovEnv(t)
		cagtAddTenant(t, env, cagtTenant("Maria"))
		env.messages.byTenantErr = errors.New("banco fora")

		_, err := ListConversations(context.Background(), env.deps, 10)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "listar mensagens do titular")
	})
}

// ── Titular da conversa ────────────────────────────────────────────

func TestCagtConversationTenantUsesTheLinkedTenant(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")
	cagtAddTenant(t, env, tenant)

	got, err := conversationTenant(context.Background(), env.deps, domain.WhatsAppMessage{
		ID: "m1", TenantID: domain.StrPtr(tenant.ID), Phone: "5511999999999",
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, tenant.ID, got.ID)
}

// Mensagem gravada antes do cadastro ainda acha o titular pelo telefone;
// número desconhecido continua sem titular.
func TestCagtConversationTenantFallsBackToThePhone(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")
	tenant.Phone = domain.StrPtr("(21) 98888-7777")
	cagtAddTenant(t, env, tenant)

	got, err := conversationTenant(context.Background(), env.deps, domain.WhatsAppMessage{ID: "m1", Phone: "5521988887777"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, tenant.ID, got.ID)

	unknown, err := conversationTenant(context.Background(), env.deps, domain.WhatsAppMessage{ID: "m2", Phone: "551133334444"})
	require.NoError(t, err)
	assert.Nil(t, unknown)
}

func TestCagtConversationTenantWithoutPhoneStaysWithoutTenant(t *testing.T) {
	env := newCovEnv(t)
	cagtAddTenant(t, env, cagtTenant("Maria"))

	got, err := conversationTenant(context.Background(), env.deps, domain.WhatsAppMessage{ID: "m1", Phone: "sem-número"})
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestCagtConversationTenantReportsLookupFailures(t *testing.T) {
	t.Run("vínculo ilegível", func(t *testing.T) {
		env := newCovEnv(t)
		env.tenants.byIDErr = errors.New("banco fora")

		_, err := conversationTenant(context.Background(), env.deps, domain.WhatsAppMessage{
			ID: "m1", TenantID: domain.StrPtr("cagt-tenant"),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "banco fora")
	})

	t.Run("telefone ilegível", func(t *testing.T) {
		env := newCovEnv(t)
		env.tenants.listErr = errors.New("banco fora")

		_, err := conversationTenant(context.Background(), env.deps, domain.WhatsAppMessage{ID: "m1", Phone: "5521988887777"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "banco fora")
	})
}

// O painel resolve a conversa pelo identificador informado; titular
// inexistente (ou identificador em branco) é `ErrNotFound`.
func TestCagtConversationTenantByIDRequiresKnownTenant(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria")
	cagtAddTenant(t, env, tenant)

	got, err := conversationTenantByID(context.Background(), env.deps, "  "+tenant.ID+"  ")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, tenant.ID, got.ID)

	_, err = conversationTenantByID(context.Background(), env.deps, "   ")
	assert.ErrorIs(t, err, domain.ErrNotFound)

	_, err = conversationTenantByID(context.Background(), env.deps, "cagt-inexistente")
	assert.ErrorIs(t, err, domain.ErrNotFound)

	env.tenants.byIDErr = errors.New("banco fora")
	_, err = conversationTenantByID(context.Background(), env.deps, tenant.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "banco fora")
}

// ── Prompt do atendimento ──────────────────────────────────────────

// O prompt reflete o estado da instância: o nome do locador configurado e o
// titular da conversa, além das ferramentas e das proibições.
func TestCagtAgentSystemPromptNamesLandlordAndTenant(t *testing.T) {
	env := newCovEnv(t)
	env.settings.values[domain.SettingLandlordName] = "Imobiliária Central"
	tenant := cagtTenant("Maria Souza")

	prompt := agentSystemPrompt(context.Background(), env.deps, &tenant)
	assert.Contains(t, prompt, "Imobiliária Central")
	assert.Contains(t, prompt, "Locatário desta conversa: Maria Souza.")
	assert.Contains(t, prompt, agentToolOpenReceipts)
	assert.Contains(t, prompt, agentToolContract)
	assert.Contains(t, prompt, agentToolReceiptStatus)
	assert.Contains(t, prompt, agentToolEscalate)
}

// Instância sem locador configurado não deixa o prompt sem autor.
func TestCagtAgentSystemPromptWithoutConfiguredLandlord(t *testing.T) {
	env := newCovEnv(t)
	tenant := cagtTenant("Maria Souza")

	prompt := agentSystemPrompt(context.Background(), env.deps, &tenant)
	assert.Contains(t, prompt, "em nome de o locador.")
	assert.Contains(t, prompt, "Maria Souza")
}

// ── Guardrail de ponta a ponta ─────────────────────────────────────

// A resposta só é publicada com lastro: sem ferramenta que devolva dado real,
// o atendimento encaminha ao locador em vez de repetir o modelo.
func TestCagtAgentPublishesAnAnswerOnlyAfterATypedTool(t *testing.T) {
	env := newCovEnv(t)
	holder := cagtTenant("Maria")
	cagtAddTenant(t, env, holder)
	cagtAddReceipt(t, env, covReceiptOf("r1", holder.ID, "", "2026-09-01", 900))

	chat := &cagtChat{responses: []ChatResponse{
		{ToolCalls: []ChatToolCall{{Name: agentToolOpenReceipts}}},
		{Message: "Você tem um aluguel vencido em 01/09/2026."},
	}}
	env.deps.Chat = chat

	reply, err := HandleIncomingMessage(context.Background(), env.deps, covCtx(), domain.WhatsAppMessage{
		ID: "m1", TenantID: domain.StrPtr(holder.ID), Phone: "5511999999999", Body: "Quanto eu devo?",
	})
	require.NoError(t, err)
	require.NotNil(t, reply)
	assert.False(t, reply.Escalated)
	assert.Equal(t, "Você tem um aluguel vencido em 01/09/2026.", reply.Reply)
	assert.Equal(t, []string{agentToolOpenReceipts}, reply.Tools)
	assert.Equal(t, 2, reply.Rounds)

	require.Len(t, chat.requests, 2)
	require.Len(t, chat.requests[1].Messages, 2)
	assert.Contains(t, chat.requests[1].Messages[1].Content, "id r1")
	assert.Contains(t, chat.requests[1].Messages[1].Content, format.FormatCurrency(900))
}

// Ferramenta limitada ao titular: o modelo pede o lançamento de outra conversa
// e recebe o erro da ferramenta — nunca o dado de outra pessoa. Sem lastro, a
// resposta não é publicada e a conversa é encaminhada.
func TestCagtAgentRefusesAnotherTenantsReceiptInsideTheLoop(t *testing.T) {
	env := newCovEnv(t)
	holder := cagtTenant("Maria")
	cagtAddTenant(t, env, holder)
	other := covTenantOf("cagt-outro", "Pedro")
	cagtAddTenant(t, env, other)
	cagtAddLease(t, env, covLeaseOf("l-pedro", other.ID, "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900))
	cagtAddReceipt(t, env, covReceiptOf("r-pedro", "", "l-pedro", "2026-09-01", 900))

	chat := &cagtChat{responses: []ChatResponse{
		{ToolCalls: []ChatToolCall{{Name: agentToolReceiptStatus, Arguments: map[string]any{"receiptId": "r-pedro"}}}},
		{Message: "O lançamento de Pedro venceu em 01/09/2026."},
	}}
	env.deps.Chat = chat

	reply, err := HandleIncomingMessage(context.Background(), env.deps, covCtx(), domain.WhatsAppMessage{
		ID: "m1", TenantID: domain.StrPtr(holder.ID), Phone: "5511999999999", Body: "Quanto é o lançamento r-pedro?",
	})
	require.NoError(t, err)
	require.NotNil(t, reply)
	assert.True(t, reply.Escalated)
	assert.Equal(t, agentReasonUngrounded, reply.Reason)
	assert.NotContains(t, reply.Reply, "Pedro", "resposta do modelo sem lastro não é publicada")
	assert.Empty(t, reply.Tools)

	require.Len(t, chat.requests, 2)
	require.Len(t, chat.requests[1].Messages, 2)
	toolTurn := chat.requests[1].Messages[1].Content
	assert.Contains(t, toolTurn, "erro:")
	assert.Contains(t, toolTurn, "não encontrado")
	assert.NotContains(t, toolTurn, format.FormatCurrency(900), "o dado do outro titular não volta para o modelo")
}
