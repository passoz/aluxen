package application_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db"
)

// ── Apoio ──────────────────────────────────────────────────────────

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

// seqIDs gera identificadores determinísticos para as importações.
type seqIDs struct{ next int }

func (g *seqIDs) New() string {
	g.next++
	return fmt.Sprintf("novo-%d", g.next)
}

// newDeps abre um SQLite em memória com as migrações aplicadas e devolve as
// dependências dos casos de uso sobre ele.
func newDeps(t *testing.T) application.Deps {
	t.Helper()

	store, err := db.Open(context.Background(), ":memory:", "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	return application.Deps{
		Repos: application.Repositories{
			Users:      store.Users,
			Tenants:    store.Tenants,
			Properties: store.Properties,
			Leases:     store.Leases,
			Receipts:   store.Receipts,
			Settings:   store.Settings,
			Audit:      store.Audit,
		},
		Clock: fixedClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)},
		IDs:   &seqIDs{},
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func seedAudit(t *testing.T, deps application.Deps, entries ...*domain.AuditLog) {
	t.Helper()
	for _, entry := range entries {
		require.NoError(t, deps.Repos.Audit.Insert(context.Background(), entry))
	}
}

func strPtr(value string) *string { return &value }

// ── Leitura da query ───────────────────────────────────────────────

func TestReadAuditQueryDefaults(t *testing.T) {
	filter := application.ReadAuditQuery(url.Values{})
	assert.Equal(t, 1, filter.Page)
	assert.Equal(t, application.DefaultAuditPageSize, filter.PageSize)
	assert.True(t, filter.Descending)
	assert.Empty(t, filter.Entity)
	assert.Empty(t, filter.From)
}

func TestReadAuditQueryClampsPageAndPageSize(t *testing.T) {
	cases := []struct {
		name         string
		values       url.Values
		wantPage     int
		wantPageSize int
	}{
		{"página inválida", url.Values{"page": {"abc"}}, 1, 25},
		{"página zero", url.Values{"page": {"0"}}, 1, 25},
		{"página negativa", url.Values{"page": {"-4"}}, 1, 25},
		{"página válida", url.Values{"page": {"7"}}, 7, 25},
		{"tamanho abaixo do mínimo", url.Values{"pageSize": {"1"}}, 1, appMinPageSize},
		{"tamanho acima do máximo", url.Values{"pageSize": {"1000"}}, 1, appMaxPageSize},
		{"tamanho inválido", url.Values{"pageSize": {"x"}}, 1, 25},
		{"tamanho válido", url.Values{"pageSize": {"50"}}, 1, 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filter := application.ReadAuditQuery(tc.values)
			assert.Equal(t, tc.wantPage, filter.Page)
			assert.Equal(t, tc.wantPageSize, filter.PageSize)
		})
	}
}

const (
	appMinPageSize = application.MinAuditPageSize
	appMaxPageSize = application.MaxAuditPageSize
)

func TestReadAuditQueryRejectsMalformedDates(t *testing.T) {
	valid := application.ReadAuditQuery(url.Values{"from": {"2026-03-05"}, "to": {"2026-12-31"}})
	assert.Equal(t, "2026-03-05", valid.From)
	assert.Equal(t, "2026-12-31", valid.To)

	for _, malformed := range []string{"2026-3-5", "05/03/2026", "2026-03-05T00:00", "2026-03-0a", "20260305", "2026-03-5"} {
		filter := application.ReadAuditQuery(url.Values{"from": {malformed}, "to": {malformed}})
		assert.Empty(t, filter.From, "from deveria ser descartado: %s", malformed)
		assert.Empty(t, filter.To, "to deveria ser descartado: %s", malformed)
	}
}

func TestReadAuditQueryOrderAndTrim(t *testing.T) {
	asc := application.ReadAuditQuery(url.Values{"order": {"asc"}, "entity": {" tenant "}, "search": {" carlos "}})
	assert.False(t, asc.Descending)
	assert.Equal(t, "tenant", asc.Entity)
	assert.Equal(t, "carlos", asc.Search)

	desc := application.ReadAuditQuery(url.Values{"order": {"desc"}})
	assert.True(t, desc.Descending)

	other := application.ReadAuditQuery(url.Values{"order": {"qualquer"}})
	assert.True(t, other.Descending, "ordem desconhecida cai no padrão (mais recente primeiro)")
}

// ── Consulta da trilha ─────────────────────────────────────────────

func sampleAuditLogs() []*domain.AuditLog {
	return []*domain.AuditLog{
		{
			ID: "audit-1", CreatedAt: "2026-03-05T08:00:00.000", Action: domain.ActionTenantCreate,
			Entity: domain.EntityTenant, EntityID: strPtr("tenant-1"), EntityLabel: strPtr("Carlos Drummond"),
			ActorID: strPtr("admin-1"), ActorName: strPtr("passoz"), Summary: "Inquilino cadastrado",
			Changes: strPtr(`[{"field":"name","label":"Nome","from":null,"to":"Carlos Drummond"}]`),
			Origin:  domain.AuditOriginWeb, IP: strPtr("127.0.0.1"), RequestID: strPtr("abcd1234"),
			Metadata: strPtr(`{"count":2}`),
		},
		{
			ID: "audit-2", CreatedAt: "2026-03-05T09:00:00.000", Action: domain.ActionReceiptPay,
			Entity: domain.EntityReceipt, EntityID: strPtr("receipt-1"), EntityLabel: strPtr("APTO 101"),
			ActorName: strPtr("passoz"), Summary: "Baixa registrada", Origin: domain.AuditOriginClient,
		},
		{
			ID: "audit-3", CreatedAt: "2026-03-06T07:30:00.000", Action: domain.ActionDocumentContract,
			Entity: domain.EntityDocument, EntityLabel: strPtr("Contrato de Carlos Drummond"),
			ActorName: strPtr("Maria"), Summary: "Contrato emitido", Origin: domain.AuditOriginPublic,
		},
	}
}

func TestListAuditPaginatesAndOrders(t *testing.T) {
	ctx := context.Background()
	deps := newDeps(t)
	seedAudit(t, deps, sampleAuditLogs()...)

	page, err := application.ListAudit(ctx, deps, application.AuditFilter{Descending: true})
	require.NoError(t, err)
	assert.Equal(t, 3, page.Total)
	assert.Equal(t, 1, page.Page)
	assert.Equal(t, application.DefaultAuditPageSize, page.PageSize)
	require.Len(t, page.Items, 3)
	assert.Equal(t, []string{"audit-3", "audit-2", "audit-1"}, []string{page.Items[0].ID, page.Items[1].ID, page.Items[2].ID})
	assert.Equal(t, []string{"Maria", "passoz"}, page.Actors)

	// A segunda página traz o restante.
	second, err := application.ListAudit(ctx, deps, application.AuditFilter{Page: 2, PageSize: 5, Descending: true})
	require.NoError(t, err)
	require.Len(t, second.Items, 0)
	assert.Equal(t, 2, second.Page)

	// Ascendente inverte a ordem.
	ascending, err := application.ListAudit(ctx, deps, application.AuditFilter{Descending: false})
	require.NoError(t, err)
	assert.Equal(t, "audit-1", ascending.Items[0].ID)

	// Paginação de verdade: 5 itens, página 2 com 5 por página.
	for i := 4; i <= 7; i++ {
		seedAudit(t, deps, &domain.AuditLog{
			ID: fmt.Sprintf("audit-%d", i), CreatedAt: fmt.Sprintf("2026-03-07T0%d:00:00.000", i),
			Action: domain.ActionLogin, Entity: domain.EntityUser, Summary: "Acesso ao sistema",
			ActorName: strPtr("passoz"), Origin: domain.AuditOriginWeb,
		})
	}
	paged, err := application.ListAudit(ctx, deps, application.AuditFilter{Page: 2, PageSize: 5, Descending: true})
	require.NoError(t, err)
	assert.Equal(t, 7, paged.Total)
	require.Len(t, paged.Items, 2)
}

func TestListAuditAppliesFiltersAndSearch(t *testing.T) {
	ctx := context.Background()
	deps := newDeps(t)
	seedAudit(t, deps, sampleAuditLogs()...)

	byEntity, err := application.ListAudit(ctx, deps, application.AuditFilter{Entity: "receipt"})
	require.NoError(t, err)
	assert.Equal(t, 1, byEntity.Total)
	assert.Equal(t, "audit-2", byEntity.Items[0].ID)

	byAction, err := application.ListAudit(ctx, deps, application.AuditFilter{Action: domain.ActionTenantCreate})
	require.NoError(t, err)
	assert.Equal(t, 1, byAction.Total)

	byActor, err := application.ListAudit(ctx, deps, application.AuditFilter{Actor: "Maria"})
	require.NoError(t, err)
	assert.Equal(t, 1, byActor.Total)
	assert.Equal(t, "audit-3", byActor.Items[0].ID)

	// Busca sem diferenciar maiúsculas, no resumo, no rótulo e no id.
	for term, wantID := range map[string]string{
		"contrato":  "audit-3",
		"APTO 101":  "audit-2",
		"receipt-1": "audit-2",
	} {
		page, err := application.ListAudit(ctx, deps, application.AuditFilter{Search: term})
		require.NoError(t, err)
		require.Equalf(t, 1, page.Total, "busca %q", term)
		assert.Equal(t, wantID, page.Items[0].ID, "busca %q", term)
	}

	// Busca pelo nome do usuário alcança todos os eventos que ele assinou.
	byName, err := application.ListAudit(ctx, deps, application.AuditFilter{Search: "passoz"})
	require.NoError(t, err)
	assert.Equal(t, 2, byName.Total)

	// Limite superior só com data alcança os eventos do próprio dia.
	toDay, err := application.ListAudit(ctx, deps, application.AuditFilter{To: "2026-03-05"})
	require.NoError(t, err)
	assert.Equal(t, 2, toDay.Total)

	fromDay, err := application.ListAudit(ctx, deps, application.AuditFilter{From: "2026-03-06"})
	require.NoError(t, err)
	assert.Equal(t, 1, fromDay.Total)
	assert.Equal(t, "audit-3", fromDay.Items[0].ID)
}

func TestListAuditDecodesChangesAndMetadata(t *testing.T) {
	ctx := context.Background()
	deps := newDeps(t)
	seedAudit(t, deps, sampleAuditLogs()...)

	page, err := application.ListAudit(ctx, deps, application.AuditFilter{Action: domain.ActionTenantCreate})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)

	entry := page.Items[0]
	require.Len(t, entry.Changes, 1)
	assert.Equal(t, "name", entry.Changes[0].Field)
	assert.Nil(t, entry.Changes[0].From)
	assert.Equal(t, "Carlos Drummond", domain.Str(entry.Changes[0].To))
	assert.Equal(t, float64(2), entry.Metadata["count"])
	assert.Equal(t, "web", entry.Origin)
	assert.Equal(t, "127.0.0.1", domain.Str(entry.IP))
}

func TestListAuditForExportIgnoresPagination(t *testing.T) {
	ctx := context.Background()
	deps := newDeps(t)
	seedAudit(t, deps, sampleAuditLogs()...)

	entries, err := application.ListAuditForExport(ctx, deps, application.AuditFilter{Page: 3, PageSize: application.MinAuditPageSize, Entity: "tenant"})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "audit-1", entries[0].ID)
}

// ── CSV ────────────────────────────────────────────────────────────

func TestBuildAuditCSVLayoutAndBOM(t *testing.T) {
	ctx := context.Background()
	deps := newDeps(t)
	seedAudit(t, deps, sampleAuditLogs()...)

	entries, err := application.ListAuditForExport(ctx, deps, application.AuditFilter{Descending: false, Entity: "tenant"})
	require.NoError(t, err)

	csv := application.BuildAuditCSV(entries)

	assert.True(t, strings.HasPrefix(csv, "\uFEFF"), "CSV deve começar com BOM")
	assert.True(t, strings.HasSuffix(csv, "\r\n"), "CSV deve terminar com CRLF")
	assert.Contains(t, csv, `"Data/Hora";"Usuário";"Ação";"Área";"Registro";"ID do registro";"Resumo";"Alterações";"Metadados";"Origem";"IP";"User-Agent";"Request"`)

	lines := strings.Split(strings.TrimSuffix(strings.TrimPrefix(csv, "\uFEFF"), "\r\n"), "\r\n")
	require.Len(t, lines, 2)
	assert.Equal(t, 13, len(strings.Split(lines[0], ";")), "cabeçalho com 13 colunas")

	row := lines[1]
	assert.Equal(t, 13, len(strings.Split(row, ";")), "cada evento tem 13 colunas")
	assert.Contains(t, row, `"05/03/2026 08:00"`)
	assert.Contains(t, row, `"passoz"`)
	assert.Contains(t, row, `"Inquilino"`)
	assert.Contains(t, row, `"Nome: — → Carlos Drummond"`, "valor nulo vira travessão no resumo das alterações")
	assert.Contains(t, row, `"Quantidade: 2"`)
	assert.Contains(t, row, `"Painel"`)
	assert.Contains(t, row, `"127.0.0.1"`)
}

func TestBuildAuditCSVQuotesDelimitersAndEmptyEntries(t *testing.T) {
	assert.Equal(t,
		"\uFEFF\"Data/Hora\";\"Usuário\";\"Ação\";\"Área\";\"Registro\";\"ID do registro\";\"Resumo\";\"Alterações\";\"Metadados\";\"Origem\";\"IP\";\"User-Agent\";\"Request\"\r\n",
		application.BuildAuditCSV(nil))

	entry := application.AuditRow{
		ID: "x", CreatedAt: "2026-03-05T08:00:00.000", Action: domain.ActionTenantCreate,
		Entity: domain.EntityTenant, Summary: `Disse "oi"; e saiu`,
		Changes: []domain.AuditChange{}, Metadata: map[string]any{},
	}
	csv := application.BuildAuditCSV([]application.AuditRow{entry})
	assert.Contains(t, csv, `"Disse ""oi""; e saiu"`, "aspas dobradas e ponto-e-vírgula preservado dentro das aspas")
	assert.Contains(t, csv, `"Sistema"`, "sem usuário o evento é do sistema")
}

func TestBuildAuditCSVLimitsRows(t *testing.T) {
	entries := make([]application.AuditRow, application.AuditExportLimit+10)
	for i := range entries {
		entries[i] = application.AuditRow{ID: fmt.Sprintf("audit-%d", i), Action: domain.ActionLogin, Entity: domain.EntityUser, CreatedAt: "2026-03-05T08:00:00.000"}
	}
	csv := application.BuildAuditCSV(entries)
	assert.Equal(t, application.AuditExportLimit+1, strings.Count(csv, "\r\n"), "cabeçalho + limite de eventos")
}

// ── Backup ─────────────────────────────────────────────────────────

func seedCatalog(t *testing.T, deps application.Deps) (domain.Tenant, domain.Property, domain.Lease, domain.Receipt) {
	t.Helper()
	ctx := context.Background()

	tenant := domain.Tenant{
		ID: "tenant-1", Name: "Carlos Drummond", Email: "carlos@aluxen.test",
		Phone: strPtr("(22) 90000-0000"), CPF: strPtr("123.456.789-00"),
		CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-02-01 10:00:00",
	}
	require.NoError(t, deps.Repos.Tenants.Create(ctx, &tenant))

	property := domain.Property{
		ID: "property-1", Name: "Apto 101", Address: "Rua das Flores, 10", City: "Cabo Frio", State: "RJ",
		Zip:       strPtr("28900-000"),
		CreatedAt: "2026-01-02 10:00:00", UpdatedAt: "2026-02-02 10:00:00",
	}
	require.NoError(t, deps.Repos.Properties.Create(ctx, &property))

	lease := domain.Lease{
		ID: "lease-1", TenantID: tenant.ID, PropertyID: property.ID,
		StartDate: "2026-01-10", EndDate: "2026-12-09", DueDay: 10, RentAmount: 1500,
		Status: domain.LeaseStatusActive, CreatedAt: "2026-01-03 10:00:00", UpdatedAt: "2026-02-03 10:00:00",
	}
	require.NoError(t, deps.Repos.Leases.Create(ctx, &lease))

	receipt := domain.Receipt{
		ID: "receipt-1", LeaseID: strPtr(lease.ID), Category: domain.ReceiptCategoryRent,
		DueDate: "2026-02-10", PaidAt: strPtr("2026-02-10T09:30"), Amount: 1500,
		CreatedAt: "2026-01-05 10:00:00",
	}
	require.NoError(t, deps.Repos.Receipts.Create(ctx, &receipt))

	return tenant, property, lease, receipt
}

func TestExportBackupReadsCatalog(t *testing.T) {
	ctx := context.Background()
	deps := newDeps(t)
	seedCatalog(t, deps)

	backup, err := application.ExportBackup(ctx, deps)
	require.NoError(t, err)

	assert.Equal(t, "2026-09-21T12:00:00.000Z", backup.ExportedAt)
	require.Len(t, backup.Tenants, 1)
	assert.Equal(t, "Carlos Drummond", backup.Tenants[0].Name)
	assert.Equal(t, "2026-01-01 10:00:00", backup.Tenants[0].CreatedAt)
	require.Len(t, backup.Properties, 1)
	require.Len(t, backup.Leases, 1)
	assert.Equal(t, "tenant-1", backup.Leases[0].TenantID)
	require.Len(t, backup.Receipts, 1)
	assert.Equal(t, "lease-1", domain.Str(backup.Receipts[0].LeaseID))
	assert.Equal(t, "2026-01-05 10:00:00", backup.Receipts[0].CreatedAt)

	encoded, err := json.Marshal(backup)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "password", "o backup não carrega credenciais")
	assert.NotContains(t, string(encoded), "authToken", "o backup não carrega tokens de autenticidade")
}

func TestImportBackupRegeneratesIdentifiersAndRemapsForeignKeys(t *testing.T) {
	ctx := context.Background()
	source := newDeps(t)
	seedCatalog(t, source)

	backup, err := application.ExportBackup(ctx, source)
	require.NoError(t, err)
	encoded, err := json.Marshal(backup)
	require.NoError(t, err)

	target := newDeps(t)
	result, err := application.ImportBackup(ctx, target, application.AuditContext{Origin: domain.AuditOriginWeb}, encoded)
	require.NoError(t, err)
	assert.Equal(t, application.ImportResult{Tenants: 1, Properties: 1, Leases: 1, Receipts: 1}, *result)

	tenants, err := target.Repos.Tenants.List(ctx)
	require.NoError(t, err)
	require.Len(t, tenants, 1)
	assert.NotEqual(t, "tenant-1", tenants[0].ID, "locatário recebe id novo")
	assert.Equal(t, "Carlos Drummond", tenants[0].Name)
	assert.Equal(t, "123.456.789-00", domain.Str(tenants[0].CPF))
	assert.Equal(t, "2026-01-01 10:00:00", tenants[0].CreatedAt, "createdAt original preservado")
	assert.Equal(t, "2026-02-01 10:00:00", tenants[0].UpdatedAt, "updatedAt original preservado")

	properties, err := target.Repos.Properties.List(ctx)
	require.NoError(t, err)
	require.Len(t, properties, 1)
	assert.NotEqual(t, "property-1", properties[0].ID)

	leases, err := target.Repos.Leases.List(ctx)
	require.NoError(t, err)
	require.Len(t, leases, 1)
	assert.NotEqual(t, "lease-1", leases[0].ID)
	assert.Equal(t, tenants[0].ID, leases[0].TenantID, "FK do locatário remapeada")
	assert.Equal(t, properties[0].ID, leases[0].PropertyID, "FK do imóvel remapeada")
	assert.Equal(t, domain.LeaseStatusActive, leases[0].Status)
	assert.Equal(t, 10, leases[0].DueDay)
	assert.Equal(t, 1500.0, leases[0].RentAmount)

	receipts, err := target.Repos.Receipts.List(ctx)
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	require.NotNil(t, receipts[0].LeaseID)
	assert.Equal(t, leases[0].ID, *receipts[0].LeaseID, "FK do contrato remapeada")
	assert.Equal(t, domain.ReceiptCategoryRent, receipts[0].Category)
	assert.Nil(t, receipts[0].GeneratedKey)
	assert.Nil(t, receipts[0].AuthToken)
	assert.Equal(t, "2026-01-05 10:00:00", receipts[0].CreatedAt)
}

func TestImportBackupDefaultsDueDayAndIgnoresReceiptExtras(t *testing.T) {
	ctx := context.Background()
	deps := newDeps(t)

	raw := `{
		"exportedAt": "2026-09-21T12:00:00.000Z",
		"tenants": [{"id": "t1", "name": "Locatário A", "email": "a@aluxen.test", "createdAt": "2026-01-01 00:00:00", "updatedAt": "2026-01-02 00:00:00"}],
		"properties": [{"id": "p1", "name": "Imóvel A", "address": "Rua 1", "city": "Cabo Frio", "state": "RJ", "createdAt": "2026-01-01 00:00:00", "updatedAt": "2026-01-02 00:00:00"}],
		"leases": [{"id": "l1", "tenantId": "t1", "propertyId": "p1", "startDate": "2026-01-01", "endDate": "2026-12-31", "dueDay": 0, "rentAmount": 900, "status": "suspended", "createdAt": "2026-01-01 00:00:00", "updatedAt": "2026-01-02 00:00:00"}],
		"receipts": [{"id": "r1", "leaseId": "l1", "dueDate": "2026-02-10", "paidAt": "2026-02-10T10:00", "amount": 900, "createdAt": "2026-01-05 00:00:00", "category": "extra", "description": "taxa", "paidAmount": 300, "generatedKey": "2026-02", "authToken": "abc"}]
	}`

	result, err := application.ImportBackup(ctx, deps, application.AuditContext{}, []byte(raw))
	require.NoError(t, err)
	assert.Equal(t, application.ImportResult{Tenants: 1, Properties: 1, Leases: 1, Receipts: 1}, *result)

	leases, err := deps.Repos.Leases.List(ctx)
	require.NoError(t, err)
	require.Len(t, leases, 1)
	assert.Equal(t, 1, leases[0].DueDay, "dueDay ausente cai para 1")
	assert.Equal(t, domain.LeaseStatusSuspended, leases[0].Status, "situação do contrato preservada")

	receipts, err := deps.Repos.Receipts.List(ctx)
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	assert.Equal(t, domain.ReceiptCategoryRent, receipts[0].Category, "categoria ignorada, assume Aluguel")
	assert.Nil(t, receipts[0].Description)
	assert.Nil(t, receipts[0].PaidAmount)
	assert.Nil(t, receipts[0].GeneratedKey)
	assert.Nil(t, receipts[0].AuthToken)
}

func TestImportBackupRejectsMalformedPayloads(t *testing.T) {
	ctx := context.Background()
	deps := newDeps(t)

	_, err := application.ImportBackup(ctx, deps, application.AuditContext{}, []byte("não é json"))
	require.Error(t, err)
	assert.Equal(t, "Arquivo JSON inválido.", domain.ValidationMessage(err))

	_, err = application.ImportBackup(ctx, deps, application.AuditContext{}, []byte(`{"exportedAt":"x"}`))
	require.Error(t, err)
	assert.Equal(t, "Formato de backup inválido.", domain.ValidationMessage(err))

	partial := `{"tenants":[],"properties":[],"leases":[]}`
	_, err = application.ImportBackup(ctx, deps, application.AuditContext{}, []byte(partial))
	require.Error(t, err)
	assert.Equal(t, "Formato de backup inválido.", domain.ValidationMessage(err))

	// Lista presente mas nula também invalida.
	_, err = application.ImportBackup(ctx, deps, application.AuditContext{}, []byte(`{"tenants":[],"properties":[],"leases":[],"receipts":null}`))
	require.Error(t, err)
	assert.Equal(t, "Formato de backup inválido.", domain.ValidationMessage(err))
}
