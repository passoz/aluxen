package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/stretchr/testify/require"
)

// Cobertura dos casos de uso de base: configurações da instância, contagens,
// URL pública, autenticação e trilha de auditoria.

// ── settings.go ───────────────────────────────────────────────────

func TestCovSaveLandlordDataValidatesAndAudits(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()

	err := SaveLandlordData(ctx, env.deps, covCtx(), LandlordInput{Name: "   ", Email: "a@b.com"})
	require.Error(t, err)
	require.EqualError(t, err, "Informe o nome do locador.")

	err = SaveLandlordData(ctx, env.deps, covCtx(), LandlordInput{Name: "Maria", Email: "invalido"})
	require.Error(t, err)
	require.EqualError(t, err, "Informe um e-mail válido.")
	require.Empty(t, env.audit.entries, "validação recusada não pode deixar rastro na trilha")

	err = SaveLandlordData(ctx, env.deps, covCtx(), LandlordInput{Name: "  Maria Silva ", CPF: " 123.456.789-00 ", Email: " maria@example.com "})
	require.NoError(t, err)
	require.Equal(t, "Maria Silva", env.settings.values[domain.SettingLandlordName])
	require.Equal(t, "123.456.789-00", env.settings.values[domain.SettingLandlordCPF])
	require.Equal(t, "maria@example.com", env.settings.values[domain.SettingLandlordEmail])

	require.Len(t, env.audit.entries, 1)
	entry := env.audit.entries[0]
	require.Equal(t, domain.ActionSettingLandlord, entry.Action)
	require.Equal(t, domain.EntitySetting, entry.Entity)
	require.Equal(t, "landlord", domain.Str(entry.EntityID))
	require.NotNil(t, entry.Changes, "o diff dos dados do locador é registrado")
	require.Contains(t, *entry.Changes, "maria@example.com")
}

func TestCovSaveLandlordDataSkipsAuditWithoutChange(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	input := LandlordInput{Name: "Maria", CPF: "123", Email: "maria@example.com"}

	require.NoError(t, SaveLandlordData(ctx, env.deps, covCtx(), input))
	require.NoError(t, SaveLandlordData(ctx, env.deps, covCtx(), input))

	require.Len(t, env.audit.entries, 1, "salvar sem mudança não gera linha na trilha")
}

func TestCovSaveLandlordDataPropagatesRepositoryFailure(t *testing.T) {
	env := newCovEnv(t)
	env.settings.getErr = errors.New("banco fora")

	err := SaveLandlordData(context.Background(), env.deps, covCtx(), LandlordInput{Name: "Maria", Email: "maria@example.com"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "banco fora")
}

func TestCovSaveAppURLValidatesStoresAndAudits(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()

	err := SaveAppURL(ctx, env.deps, covCtx(), "painel.local")
	require.Error(t, err)
	require.EqualError(t, err, "Informe uma URL começando por http:// ou https://.")

	require.NoError(t, SaveAppURL(ctx, env.deps, covCtx(), " https://painel.example.com/ "))
	require.Equal(t, "https://painel.example.com/", env.settings.values[domain.SettingAppURL])
	require.Len(t, env.audit.entries, 1)
	require.Equal(t, domain.ActionSettingAppURL, env.audit.entries[0].Action)
	require.Contains(t, env.audit.entries[0].Summary, "alterada")

	require.NoError(t, SaveAppURL(ctx, env.deps, covCtx(), "   "))
	require.Equal(t, "", env.settings.values[domain.SettingAppURL])
	require.Len(t, env.audit.entries, 2)
	require.Contains(t, env.audit.entries[1].Summary, "removida")
}

func TestCovSaveAppURLWithoutChangeDoesNotAudit(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()

	require.NoError(t, SaveAppURL(ctx, env.deps, covCtx(), ""))
	require.Empty(t, env.audit.entries, "remover o que já estava vazio não é alteração")
}

func TestCovSaveAppURLPropagatesFailure(t *testing.T) {
	env := newCovEnv(t)
	env.settings.getErr = errors.New("leitura falhou")
	require.Error(t, SaveAppURL(context.Background(), env.deps, covCtx(), "https://x.com"))

	env = newCovEnv(t)
	env.settings.setErr = errors.New("escrita falhou")
	require.Error(t, SaveAppURL(context.Background(), env.deps, covCtx(), "https://x.com"))
}

func TestCovLoadSettingsView(t *testing.T) {
	env := newCovEnv(t)
	env.settings.values[domain.SettingLandlordName] = "Maria"
	env.settings.values[domain.SettingLandlordCPF] = "123.456.789-00"
	env.settings.values[domain.SettingLandlordEmail] = "maria@example.com"
	env.settings.values[domain.SettingAppURL] = "https://painel.example.com/"

	view, err := LoadSettingsView(context.Background(), env.deps, "interno.local")
	require.NoError(t, err)
	require.Equal(t, "Maria", view.LandlordName)
	require.Equal(t, "123.456.789-00", view.LandlordCPF)
	require.Equal(t, "maria@example.com", view.LandlordEmail)
	require.Equal(t, "https://painel.example.com/", view.AppURL)
	require.Equal(t, "https://painel.example.com", view.ResolvedURL, "a barra final não entra nos links gerados")
}

func TestCovLoadSettingsViewPropagatesFailure(t *testing.T) {
	env := newCovEnv(t)
	env.settings.listErr = errors.New("indisponível")

	view, err := LoadSettingsView(context.Background(), env.deps, "")
	require.Error(t, err)
	require.Nil(t, view)
	require.Contains(t, err.Error(), "carregar configurações")
}

// ── system.go ─────────────────────────────────────────────────────

func TestCovSystemCounts(t *testing.T) {
	env := newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t1", "Ana"), covTenantOf("t2", "Bia")}
	env.props.items = []domain.Property{covPropertyOf("p1", "Casa")}
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 1000)}
	env.receipts.items = []domain.Receipt{covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)}

	counts, err := SystemCounts(context.Background(), env.deps)
	require.NoError(t, err)
	require.Equal(t, Counts{Tenants: 2, Properties: 1, Leases: 1, Receipts: 1}, counts)
}

func TestCovSystemCountsPropagatesEachFailure(t *testing.T) {
	failure := errors.New("contagem falhou")
	cases := map[string]func(*covEnv){
		"locatários": func(env *covEnv) { env.tenants.countErr = failure },
		"imóveis":    func(env *covEnv) { env.props.countErr = failure },
		"contratos":  func(env *covEnv) { env.leases.countErr = failure },
		"pagamentos": func(env *covEnv) { env.receipts.countErr = failure },
	}
	for label, breakIt := range cases {
		t.Run(label, func(t *testing.T) {
			env := newCovEnv(t)
			breakIt(env)

			_, err := SystemCounts(context.Background(), env.deps)
			require.ErrorIs(t, err, failure)
		})
	}
}

func TestCovSettingValueAndSettings(t *testing.T) {
	env := newCovEnv(t)
	env.settings.values["app_url"] = "https://painel.example.com"

	value, err := SettingValue(context.Background(), env.deps, "app_url")
	require.NoError(t, err)
	require.Equal(t, "https://painel.example.com", value)

	missing, err := SettingValue(context.Background(), env.deps, "inexistente")
	require.NoError(t, err)
	require.Equal(t, "", missing, "configuração ausente devolve vazio, não erro")

	all, err := Settings(context.Background(), env.deps)
	require.NoError(t, err)
	require.Equal(t, "https://painel.example.com", all["app_url"])
}

func TestCovSettingValueAndSettingsPropagateFailure(t *testing.T) {
	env := newCovEnv(t)
	env.settings.getErr = errors.New("leitura quebrada")
	_, err := SettingValue(context.Background(), env.deps, "app_url")
	require.ErrorIs(t, err, env.settings.getErr)
	require.Contains(t, err.Error(), "app_url")

	env = newCovEnv(t)
	env.settings.listErr = errors.New("listagem quebrada")
	all, err := Settings(context.Background(), env.deps)
	require.Error(t, err)
	require.Nil(t, all)
}

func TestCovSaveSetting(t *testing.T) {
	env := newCovEnv(t)
	require.NoError(t, SaveSetting(context.Background(), env.deps, "landlord_name", "Maria"))
	require.Equal(t, "Maria", env.settings.values["landlord_name"])

	env.settings.setErr = errors.New("disco cheio")
	err := SaveSetting(context.Background(), env.deps, "landlord_name", "Outra")
	require.ErrorIs(t, err, env.settings.setErr)
	require.Contains(t, err.Error(), "landlord_name")
}

func TestCovResolvePublicBaseURLPriority(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()

	// 1. configuração `app_url` vence tudo, inclusive a URL da instância.
	env.settings.values[domain.SettingAppURL] = " https://do-painel.example.com/ "
	env.deps.Opts.PublicBaseURL = "https://do-ambiente.example.com"
	require.Equal(t, "https://do-painel.example.com", ResolvePublicBaseURL(ctx, env.deps, "painel.exemplo.com"))

	// 2. sem configuração, vale a URL da instância (`APP_URL`/`BASE_URL`).
	delete(env.settings.values, domain.SettingAppURL)
	require.Equal(t, "https://do-ambiente.example.com", ResolvePublicBaseURL(ctx, env.deps, "painel.exemplo.com"))

	// 3. sem nenhuma das duas, vale o host da requisição (https só no domínio público).
	env.deps.Opts.PublicBaseURL = ""
	require.Equal(t, "http://painel.exemplo.com", ResolvePublicBaseURL(ctx, env.deps, " painel.exemplo.com "))
	require.Equal(t, "https://aluxen.evolucsia.com", ResolvePublicBaseURL(ctx, env.deps, "aluxen.evolucsia.com"))

	// 4. host local é descartado: cai no endereço da rede local da máquina ou no
	// padrão de produção — nunca no host que chegou na requisição.
	local := ResolvePublicBaseURL(ctx, env.deps, "localhost:8080")
	require.True(t, local == DefaultProductionURL || strings.HasPrefix(local, "http://"), "veio %q", local)
	require.NotContains(t, local, "localhost")

	// 5. sem host nenhum, o mesmo vale.
	require.NotEmpty(t, ResolvePublicBaseURL(ctx, env.deps, ""))
	require.NotContains(t, ResolvePublicBaseURL(ctx, env.deps, ""), "localhost")
}

func TestCovResolvePublicBaseURLIgnoresBrokenSetting(t *testing.T) {
	env := newCovEnv(t)
	env.settings.getErr = errors.New("leitura quebrada")
	env.deps.Opts.PublicBaseURL = "https://do-ambiente.example.com"

	// Configuração ilegível não derruba a resolução: cai para a próxima prioridade.
	require.Equal(t, "https://do-ambiente.example.com", ResolvePublicBaseURL(context.Background(), env.deps, ""))
}

func TestCovIsLocalHost(t *testing.T) {
	for _, host := range []string{"localhost", "localhost:3000", "127.0.0.1", "127.0.0.1:8080", "0.0.0.0"} {
		require.True(t, isLocalHost(host), "%q deveria ser host local", host)
	}
	for _, host := range []string{"painel.example.com", "10.0.0.5:8080", "aluxen.evolucsia.com", ""} {
		require.False(t, isLocalHost(host), "%q não deveria ser host local", host)
	}

	// O caso `::1` da lista é inalcançável: a porta é removida antes da
	// comparação, e "::1" perde tudo depois do primeiro ":". Documentado aqui
	// como o comportamento observável atual (relatado como defeito).
	require.False(t, isLocalHost("::1"))
	require.False(t, isLocalHost("[::1]:8080"), "o host IPv6 entre colchetes também não casa")
}

func TestCovHasContainerPrefix(t *testing.T) {
	for _, name := range []string{"docker0", "br-1a2b3c", "veth9f", "virbr0", "lxcbr0"} {
		require.True(t, hasContainerPrefix(name), "%q é interface de container", name)
	}
	for _, name := range []string{"eth0", "wlan0", "enp0s3", "lo"} {
		require.False(t, hasContainerPrefix(name), "%q não é interface de container", name)
	}
}

func TestCovLocalNetworkAddressIsUsableOrEmpty(t *testing.T) {
	// A função lê as interfaces da máquina: o contrato observável é devolver um
	// IPv4 privado utilizável em uma URL ou a string vazia — nunca um loopback.
	address := localNetworkAddress()
	require.NotContains(t, address, "127.0.0.1")
	if address != "" {
		require.Regexp(t, `^\d+\.\d+\.\d+\.\d+$`, address)
	}
}

// ── audit.go ──────────────────────────────────────────────────────

func TestCovRecordWritesEventWithChangesAndMetadata(t *testing.T) {
	env := newCovEnv(t)
	actx := covCtx()
	actx.ActorID = nil
	actx.ActorName = nil

	err := Record(context.Background(), env.deps, actx, AuditEntry{
		Action:      domain.ActionTenantCreate,
		Entity:      domain.EntityTenant,
		EntityID:    "t1",
		EntityLabel: "Ana",
		Summary:     "Inquilino cadastrado",
		Changes:     []domain.AuditChange{{Field: "name", Label: "Nome", From: nil, To: domain.StrPtr("Ana")}},
		Metadata:    map[string]any{"batch": 2},
	})
	require.NoError(t, err)
	require.Len(t, env.audit.entries, 1)

	entry := env.audit.entries[0]
	require.Equal(t, domain.AuditOriginWeb, entry.Origin, "origem vazia vira painel")
	require.Nil(t, entry.ActorID)
	require.Equal(t, "cov-001", entry.ID)
	require.Equal(t, "10.0.0.1", domain.Str(entry.IP))
	require.Equal(t, "cov-test", domain.Str(entry.UserAgent))
	require.Equal(t, "req-1", domain.Str(entry.RequestID))
	require.NotNil(t, entry.Changes)
	require.NotNil(t, entry.Metadata)
	require.Contains(t, *entry.Metadata, `"batch":2`)
	require.Contains(t, entry.CreatedAt, "2026-09-21T12:00:00.000")
}

func TestCovRecordSkipsEmptyChangeListButKeepsDiffLessEvent(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()

	require.NoError(t, Record(ctx, env.deps, covCtx(), AuditEntry{
		Action: domain.ActionTenantUpdate, Entity: domain.EntityTenant, Changes: []domain.AuditChange{},
	}))
	require.Empty(t, env.audit.entries, "lista de alterações vazia não é evento")

	require.NoError(t, Record(ctx, env.deps, covCtx(), AuditEntry{
		Action: domain.ActionLogin, Entity: domain.EntityUser,
	}))
	require.Len(t, env.audit.entries, 1, "evento sem diff (acesso) é registrado")
	entry := env.audit.entries[0]
	require.Nil(t, entry.Changes)
	require.Nil(t, entry.Metadata)
	require.Nil(t, entry.EntityID)
	require.Equal(t, "10.0.0.1", domain.Str(entry.IP), "IP informado é registrado")
}

func TestCovRecordPropagatesRepositoryFailure(t *testing.T) {
	env := newCovEnv(t)
	env.audit.insertErr = errors.New("trilha somente leitura")

	err := Record(context.Background(), env.deps, covCtxOf(domain.AuditOriginJob), AuditEntry{
		Action: domain.ActionTenantCreate, Entity: domain.EntityTenant,
	})
	require.ErrorIs(t, err, env.audit.insertErr)
	require.Contains(t, err.Error(), "gravar auditoria")
}

func TestCovRecordRejectsUnserializablePayload(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()

	// Metadados com valor que o JSON recusa: o evento não pode ser gravado pela
	// metade — a falha acontece antes de qualquer escrita.
	err := Record(ctx, env.deps, covCtx(), AuditEntry{
		Action: domain.ActionTenantCreate, Entity: domain.EntityTenant,
		Changes:  []domain.AuditChange{{Field: "name", Label: "Nome", To: domain.StrPtr("Ana")}},
		Metadata: map[string]any{"canal": make(chan int)},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "metadados da auditoria")
	require.Empty(t, env.audit.entries)
}

func TestCovSnapshotsAndDescriptions(t *testing.T) {
	require.Nil(t, TenantSnapshot(nil))
	require.Nil(t, PropertySnapshot(nil))
	require.Nil(t, LeaseSnapshot(nil))
	require.Nil(t, ReceiptSnapshot(nil))

	phone := "21999998888"
	tenant := &domain.Tenant{Name: "Ana", Email: "ana@example.com", Phone: &phone}
	require.Equal(t, map[string]string{"name": "Ana", "email": "ana@example.com", "phone": phone, "cpf": ""}, TenantSnapshot(tenant))

	lease := &domain.Lease{TenantName: "Ana", PropertyName: "Casa", StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 1500, Status: domain.LeaseStatusActive}
	snapshot := LeaseSnapshot(lease)
	require.Equal(t, "10", snapshot["dueDay"])
	require.Equal(t, "active", snapshot["status"])
	require.Equal(t, "Ana · Casa", DescribeLease(lease))
	require.Equal(t, "", DescribeLease(nil))

	semBaixa := covReceiptOf("r1", "t1", "l1", "2026-09-10", 1000)
	semBaixa.TenantName = "Ana"
	semBaixa.PropertyName = "Casa"
	require.Equal(t, "Ana · Casa · 09/2026", DescribeReceipt(&semBaixa))
	require.Equal(t, "", ReceiptSnapshot(&semBaixa)["paidAmount"], "sem baixa o valor pago é vazio")

	comBaixa := covPaidReceipt(semBaixa, "2026-09-09T10:00", 800)
	require.Equal(t, "800", ReceiptSnapshot(&comBaixa)["paidAmount"])
	require.Equal(t, "", DescribeReceipt(nil))

	// Sem titular e sem imóvel, o rótulo cai para a descrição; sem descrição,
	// para a competência.
	avulso := domain.Receipt{ID: "r2", DueDate: "2026-09-10", Amount: 50, Category: domain.ReceiptCategoryExtra}
	require.Equal(t, "Competência 09/2026", DescribeReceipt(&avulso))
	avulso.Description = domain.StrPtr("Taxa de limpeza")
	require.Equal(t, "Taxa de limpeza · 09/2026", DescribeReceipt(&avulso))
	require.Equal(t, "Taxa de limpeza · ", DescribeReceipt(&domain.Receipt{Description: domain.StrPtr("Taxa de limpeza")}))
}

// ── auth.go ───────────────────────────────────────────────────────

func covUserWithPassword(id, username, password string) domain.User {
	return domain.User{ID: id, Username: username, PasswordHash: "cov$" + password}
}

func TestCovAdminByID(t *testing.T) {
	env := newCovEnv(t)
	user := covUserWithPassword("u1", "admin", "segredo")
	env.users.items = []domain.User{user}

	found, err := AdminByID(context.Background(), env.deps, "u1")
	require.NoError(t, err)
	require.Equal(t, "admin", found.Username)

	_, err = AdminByID(context.Background(), env.deps, "nope")
	require.ErrorIs(t, err, domain.ErrUnauthorized)

	env.users.byIDErr = errors.New("banco fora")
	_, err = AdminByID(context.Background(), env.deps, "u1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "buscar administrador")
}

func TestCovAdminLogin(t *testing.T) {
	env := newCovEnv(t)
	env.users.items = []domain.User{covUserWithPassword("u1", "admin", "segredo")}
	ctx := context.Background()

	user, err := AdminLogin(ctx, env.deps, "admin", "segredo")
	require.NoError(t, err)
	require.Equal(t, "u1", user.ID)

	_, err = AdminLogin(ctx, env.deps, "admin", "errada")
	require.ErrorIs(t, err, domain.ErrUnauthorized)
	_, err = AdminLogin(ctx, env.deps, "ninguem", "segredo")
	require.ErrorIs(t, err, domain.ErrUnauthorized)

	env.users.byUsernameErr = errors.New("banco fora")
	_, err = AdminLogin(ctx, env.deps, "admin", "segredo")
	require.Error(t, err)
	require.Contains(t, err.Error(), "buscar administrador por usuário")
}

func TestCovAdminMustChangePassword(t *testing.T) {
	env := newCovEnv(t)
	env.users.items = []domain.User{{ID: "u1", Username: "admin", PasswordHash: "cov$seed"}}
	ctx := context.Background()

	pending, err := AdminMustChangePassword(ctx, env.deps, "u1")
	require.NoError(t, err)
	require.True(t, pending, "administrador semeado é travado até trocar a senha")

	changed := covUserWithPassword("u2", "outro", "segredo")
	changed.PasswordChangedAt = domain.StrPtr("2026-09-01 10:00:00")
	env.users.items = append(env.users.items, changed)
	pending, err = AdminMustChangePassword(ctx, env.deps, "u2")
	require.NoError(t, err)
	require.False(t, pending)

	pending, err = AdminMustChangePassword(ctx, env.deps, "ausente")
	require.NoError(t, err)
	require.False(t, pending, "usuário inexistente não é tratado como pendente")

	env.users.byIDErr = errors.New("banco fora")
	_, err = AdminMustChangePassword(ctx, env.deps, "u1")
	require.Error(t, err)
}

func TestCovAdminChangePassword(t *testing.T) {
	ctx := context.Background()

	env := newCovEnv(t)
	env.users.items = []domain.User{covUserWithPassword("u1", "admin", "segredo")}
	require.NoError(t, AdminChangePassword(ctx, env.deps, "u1", "segredo", "novasenha", "novasenha"))
	require.Equal(t, "cov$novasenha", env.users.items[0].PasswordHash)
	require.Equal(t, "2026-09-21 12:00:00", domain.Str(env.users.items[0].PasswordChangedAt))

	env = newCovEnv(t)
	env.users.items = []domain.User{covUserWithPassword("u1", "admin", "segredo")}
	require.EqualError(t, AdminChangePassword(ctx, env.deps, "u1", "errada", "novasenha", "novasenha"), "Senha atual incorreta.")
	require.EqualError(t, AdminChangePassword(ctx, env.deps, "u1", "segredo", "novasenha", "outra"), "A confirmação não corresponde à nova senha.")
	require.EqualError(t, AdminChangePassword(ctx, env.deps, "u1", "segredo", "curta", "curta"), "Nova senha deve ter no mínimo 6 caracteres.")
	require.EqualError(t, AdminChangePassword(ctx, env.deps, "ausente", "segredo", "novasenha", "novasenha"), "Usuário não encontrado.")

	env.users.byIDErr = errors.New("banco fora")
	require.Error(t, AdminChangePassword(ctx, env.deps, "u1", "segredo", "novasenha", "novasenha"))

	env = newCovEnv(t)
	env.users.items = []domain.User{covUserWithPassword("u1", "admin", "segredo")}
	env.users.updatePassErr = errors.New("gravação recusada")
	err := AdminChangePassword(ctx, env.deps, "u1", "segredo", "novasenha", "novasenha")
	require.Error(t, err)
	require.Contains(t, err.Error(), "gravar nova senha")
}

func TestCovClientByIDAndMustChangePassword(t *testing.T) {
	env := newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t1", "Ana"), covTenantOf("t2", "Bia")}
	env.tenants.items[1].PasswordChangedAt = domain.StrPtr("2026-01-01 00:00:00")
	ctx := context.Background()

	tenant, err := ClientByID(ctx, env.deps, "t1")
	require.NoError(t, err)
	require.Equal(t, "Ana", tenant.Name)

	_, err = ClientByID(ctx, env.deps, "ausente")
	require.ErrorIs(t, err, domain.ErrUnauthorized)

	env.tenants.byIDErr = errors.New("banco fora")
	_, err = ClientByID(ctx, env.deps, "t1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "buscar locatário")

	env = newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantOf("t1", "Ana")}
	pending, err := ClientMustChangePassword(ctx, env.deps, "t1")
	require.NoError(t, err)
	require.True(t, pending)

	_, err = ClientMustChangePassword(ctx, env.deps, "ausente")
	require.ErrorIs(t, err, domain.ErrUnauthorized)

	env.tenants.byIDErr = errors.New("banco fora")
	_, err = ClientMustChangePassword(ctx, env.deps, "t1")
	require.Error(t, err)
}

func covTenantWithCPF(id, cpf string) domain.Tenant {
	tenant := covTenantOf(id, "Ana")
	tenant.CPF = domain.StrPtr(cpf)
	return tenant
}

func TestCovClientLoginWithDefaultCPFPassword(t *testing.T) {
	env := newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantWithCPF("t1", "123.456.789-00")}
	ctx := context.Background()

	// Sem senha própria, o CPF (com ou sem pontuação) é a senha válida.
	tenant, err := ClientLogin(ctx, env.deps, "123.456.789-00", "12345678900")
	require.NoError(t, err)
	require.Equal(t, "t1", tenant.ID)

	_, err = ClientLogin(ctx, env.deps, "123.456.789-00", "99999999999")
	require.ErrorIs(t, err, domain.ErrUnauthorized)

	_, err = ClientLogin(ctx, env.deps, "123", "12345678900")
	require.ErrorIs(t, err, domain.ErrUnauthorized)
	_, err = ClientLogin(ctx, env.deps, "123.456.789-00", "")
	require.ErrorIs(t, err, domain.ErrUnauthorized)
	_, err = ClientLogin(ctx, env.deps, "999.888.777-66", "99988877766")
	require.ErrorIs(t, err, domain.ErrUnauthorized)

	env.tenants.byCPFErr = errors.New("banco fora")
	_, err = ClientLogin(ctx, env.deps, "123.456.789-00", "12345678900")
	require.Error(t, err)
	require.Contains(t, err.Error(), "buscar locatário por CPF")
}

func TestCovClientLoginWithOwnPassword(t *testing.T) {
	env := newCovEnv(t)
	tenant := covTenantWithCPF("t1", "123.456.789-00")
	tenant.PasswordHash = domain.StrPtr("cov$escolhida")
	env.tenants.items = []domain.Tenant{tenant}

	logged, err := ClientLogin(context.Background(), env.deps, "123.456.789-00", "escolhida")
	require.NoError(t, err)
	require.Equal(t, "t1", logged.ID)

	_, err = ClientLogin(context.Background(), env.deps, "123.456.789-00", "12345678900")
	require.ErrorIs(t, err, domain.ErrUnauthorized, "depois de trocar a senha, o CPF deixa de valer")
}

func TestCovClientChangePassword(t *testing.T) {
	ctx := context.Background()

	env := newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantWithCPF("t1", "123.456.789-00")}
	require.NoError(t, ClientChangePassword(ctx, env.deps, "t1", "12345678900", "  novasenha  ", "  novasenha  "))
	require.Equal(t, "cov$novasenha", domain.Str(env.tenants.items[0].PasswordHash))
	require.Equal(t, "2026-09-21 12:00:00", domain.Str(env.tenants.items[0].PasswordChangedAt))

	env = newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantWithCPF("t1", "123.456.789-00")}
	require.EqualError(t, ClientChangePassword(ctx, env.deps, "t1", "000", "novasenha", "novasenha"), "Senha atual incorreta.")
	require.EqualError(t, ClientChangePassword(ctx, env.deps, "t1", "12345678900", "novasenha", "outra"), "A confirmação não corresponde à nova senha.")
	require.EqualError(t, ClientChangePassword(ctx, env.deps, "t1", "12345678900", "curta", "curta"), "A nova senha deve ter no mínimo 6 caracteres.")
	require.EqualError(t, ClientChangePassword(ctx, env.deps, "t1", "12345678900", "123.456.789-00", "123.456.789-00"), "A nova senha não pode ser o seu CPF.")
	require.EqualError(t, ClientChangePassword(ctx, env.deps, "ausente", "12345678900", "novasenha", "novasenha"), "Cadastro de locatário não encontrado.")

	env.tenants.byIDErr = errors.New("banco fora")
	require.Error(t, ClientChangePassword(ctx, env.deps, "t1", "12345678900", "novasenha", "novasenha"))

	env = newCovEnv(t)
	env.tenants.items = []domain.Tenant{covTenantWithCPF("t1", "123.456.789-00")}
	env.tenants.updateErr = errors.New("gravação recusada")
	err := ClientChangePassword(ctx, env.deps, "t1", "12345678900", "novasenha", "novasenha")
	require.Error(t, err)
	require.Contains(t, err.Error(), "gravar nova senha do locatário")
}

func TestCovClientChangePasswordWithoutRegisteredCPF(t *testing.T) {
	env := newCovEnv(t)
	tenant := covTenantOf("t1", "Ana")
	tenant.CPF = domain.StrPtr("")
	tenant.PasswordHash = domain.StrPtr("cov$atual")
	env.tenants.items = []domain.Tenant{tenant}

	// Sem CPF cadastrado não existe a trava do "senha igual ao CPF": a senha
	// atual confere pelo hash e a nova entra normalmente.
	require.NoError(t, ClientChangePassword(context.Background(), env.deps, "t1", "atual", "abcdef", "abcdef"))
	require.Equal(t, "cov$abcdef", domain.Str(env.tenants.items[0].PasswordHash))
}

func TestCovJSONMetadataRoundTrip(t *testing.T) {
	// Garante que a serialização usada pela trilha continua legível por JSON.
	encoded, err := marshalOptional(map[string]any{"month": "2026-09", "count": 3})
	require.NoError(t, err)
	require.NotNil(t, encoded)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(*encoded), &decoded))
	require.Equal(t, "2026-09", decoded["month"])

	empty, err := marshalOptional([]domain.AuditChange{})
	require.NoError(t, err)
	require.Nil(t, empty)
}

// covSettingsFailKey falha a leitura apenas da chave informada — permite
// exercitar cada ponto de falha do snapshot do locador.
type covSettingsFailKey struct {
	*covSettings
	failKey string
}

func (r covSettingsFailKey) Get(ctx context.Context, key string) (string, error) {
	if key == r.failKey {
		return "", errors.New("chave indisponível: " + key)
	}
	return r.covSettings.Get(ctx, key)
}

func TestCovLandlordSnapshotFailsOnEachKey(t *testing.T) {
	for _, key := range []string{domain.SettingLandlordCPF, domain.SettingLandlordEmail} {
		t.Run(key, func(t *testing.T) {
			env := newCovEnv(t)
			env.deps.Repos.Settings = covSettingsFailKey{covSettings: env.settings, failKey: key}

			err := SaveLandlordData(context.Background(), env.deps, covCtx(), LandlordInput{Name: "Maria", Email: "maria@example.com"})
			require.Error(t, err)
			require.Contains(t, err.Error(), key)
			require.Empty(t, env.settings.sets, "nada é gravado quando o snapshot falha")
		})
	}
}

func TestCovSaveLandlordDataPropagatesWriteFailure(t *testing.T) {
	env := newCovEnv(t)
	env.settings.setErr = errors.New("escrita recusada")

	err := SaveLandlordData(context.Background(), env.deps, covCtx(), LandlordInput{Name: "Maria", Email: "maria@example.com"})
	require.ErrorIs(t, err, env.settings.setErr)
	require.Empty(t, env.audit.entries)
}

func TestCovPropertySnapshot(t *testing.T) {
	zip := "24000-000"
	property := &domain.Property{Name: "Casa", Address: "Rua A, 1", City: "Niterói", State: "RJ", Zip: &zip}
	require.Equal(t, map[string]string{
		"name": "Casa", "address": "Rua A, 1", "city": "Niterói", "state": "RJ", "zip": zip,
	}, PropertySnapshot(property))
}
