package db_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// A mesma bateria roda nos dois dialetos: SQLite sempre (em memória) e Postgres
// quando TEST_DATABASE_URL aponta para um servidor de teste. O objetivo é que a
// aplicação não precise saber em qual banco está rodando.
type target struct {
	name string
	dsn  string
}

func targets(t *testing.T) []target {
	t.Helper()
	list := []target{{name: "sqlite", dsn: ":memory:"}}
	if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
		list = append(list, target{name: "postgres", dsn: dsn})
	}
	return list
}

func openStore(t *testing.T, tgt target) *db.Store {
	t.Helper()
	ctx := context.Background()
	if tgt.name == "postgres" {
		resetPostgres(t, tgt.dsn)
	}
	store, err := db.Open(ctx, tgt.dsn, "")
	require.NoError(t, err, "abrir banco %s", tgt.name)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

// resetPostgres limpa o banco de teste para cada caso começar do zero. As
// tabelas são descobertas no catálogo (e não numa lista fixa) para que uma
// tabela nova nunca vaze de um caso para o outro.
func resetPostgres(t *testing.T, dsn string) {
	t.Helper()
	ctx := context.Background()
	handle, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, handle.Close()) }()

	rows, err := handle.QueryContext(ctx, "SELECT tablename FROM pg_tables WHERE schemaname = current_schema()")
	require.NoError(t, err)
	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())

	for _, table := range tables {
		_, err := handle.ExecContext(ctx, `DROP TABLE IF EXISTS "`+table+`" CASCADE`)
		require.NoError(t, err, "limpar %s", table)
	}
}

// ── Usuários ───────────────────────────────────────────────────────

func TestUsersRepository(t *testing.T) {
	for _, tgt := range targets(t) {
		t.Run(tgt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t, tgt)

			user := &domain.User{
				ID:           "user-1",
				Username:     "passoz",
				PasswordHash: "hash-1",
				CreatedAt:    "2026-01-01 10:00:00",
			}
			require.NoError(t, store.Users.Create(ctx, user))

			byID, err := store.Users.ByID(ctx, user.ID)
			require.NoError(t, err)
			assert.Equal(t, *user, *byID)
			assert.Nil(t, byID.PasswordChangedAt)
			assert.True(t, byID.MustChangePassword())

			byUsername, err := store.Users.ByUsername(ctx, "passoz")
			require.NoError(t, err)
			assert.Equal(t, user.ID, byUsername.ID)

			_, err = store.Users.ByID(ctx, "inexistente")
			assert.ErrorIs(t, err, domain.ErrNotFound)
			_, err = store.Users.ByUsername(ctx, "inexistente")
			assert.ErrorIs(t, err, domain.ErrNotFound)

			require.NoError(t, store.Users.UpdateUsername(ctx, user.ID, "admin"))
			_, err = store.Users.ByUsername(ctx, "admin")
			require.NoError(t, err)
			_, err = store.Users.ByUsername(ctx, "passoz")
			assert.ErrorIs(t, err, domain.ErrNotFound)

			require.NoError(t, store.Users.UpdatePassword(ctx, user.ID, "hash-2", "2026-02-01 09:00:00"))
			updated, err := store.Users.ByID(ctx, user.ID)
			require.NoError(t, err)
			assert.Equal(t, "hash-2", updated.PasswordHash)
			require.NotNil(t, updated.PasswordChangedAt)
			assert.Equal(t, "2026-02-01 09:00:00", *updated.PasswordChangedAt)
			assert.False(t, updated.MustChangePassword())

			// o nome de usuário é único
			require.NoError(t, store.Users.Create(ctx, &domain.User{
				ID: "user-2", Username: "outro", PasswordHash: "hash", CreatedAt: "2026-01-02 10:00:00",
			}))
			err = store.Users.Create(ctx, &domain.User{
				ID: "user-3", Username: "outro", PasswordHash: "hash", CreatedAt: "2026-01-03 10:00:00",
			})
			assert.ErrorIs(t, err, domain.ErrConflict)
		})
	}
}

// ── Locatários ─────────────────────────────────────────────────────

func TestTenantsRepository(t *testing.T) {
	for _, tgt := range targets(t) {
		t.Run(tgt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t, tgt)

			complete := &domain.Tenant{
				ID: "tenant-1", Name: "Carlos Drummond", Email: "carlos@exemplo.com",
				Phone: domain.StrPtr("(22) 99888-1122"), CPF: domain.StrPtr("111.222.333-44"),
				CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
			}
			sparse := &domain.Tenant{
				ID: "tenant-2", Name: "Clarice Lispector", Email: "clarice@exemplo.com",
				CreatedAt: "2026-01-02 10:00:00", UpdatedAt: "2026-01-02 10:00:00",
			}
			require.NoError(t, store.Tenants.Create(ctx, complete))
			require.NoError(t, store.Tenants.Create(ctx, sparse))

			list, err := store.Tenants.List(ctx)
			require.NoError(t, err)
			require.Len(t, list, 2)
			assert.Equal(t, []string{"Carlos Drummond", "Clarice Lispector"}, []string{list[0].Name, list[1].Name})

			byID, err := store.Tenants.ByID(ctx, complete.ID)
			require.NoError(t, err)
			assert.Equal(t, *complete, *byID)

			byID, err = store.Tenants.ByID(ctx, sparse.ID)
			require.NoError(t, err)
			assert.Nil(t, byID.Phone)
			assert.Nil(t, byID.CPF)
			assert.Nil(t, byID.PasswordHash)
			assert.True(t, byID.MustChangePassword())

			_, err = store.Tenants.ByID(ctx, "inexistente")
			assert.ErrorIs(t, err, domain.ErrNotFound)

			// o CPF é procurado por dígitos, mesmo gravado com pontuação
			byCPF, err := store.Tenants.ByCPF(ctx, "11122233344")
			require.NoError(t, err)
			assert.Equal(t, complete.ID, byCPF.ID)
			_, err = store.Tenants.ByCPF(ctx, "99999999999")
			assert.ErrorIs(t, err, domain.ErrNotFound)

			complete.Name = "Carlos D. de Andrade"
			complete.Email = "drummond@exemplo.com"
			complete.Phone = domain.StrPtr("(22) 90000-0000")
			complete.CPF = nil
			complete.UpdatedAt = "2026-03-01 12:00:00"
			require.NoError(t, store.Tenants.Update(ctx, complete))
			updated, err := store.Tenants.ByID(ctx, complete.ID)
			require.NoError(t, err)
			assert.Equal(t, "Carlos D. de Andrade", updated.Name)
			assert.Nil(t, updated.CPF)
			assert.Equal(t, "(22) 90000-0000", domain.Str(updated.Phone))
			assert.Equal(t, "2026-03-01 12:00:00", updated.UpdatedAt)

			require.NoError(t, store.Tenants.UpdatePassword(ctx, sparse.ID, "hash-cliente", "2026-03-02 08:00:00", "2026-03-02 08:00:00"))
			updated, err = store.Tenants.ByID(ctx, sparse.ID)
			require.NoError(t, err)
			require.NotNil(t, updated.PasswordHash)
			assert.Equal(t, "hash-cliente", *updated.PasswordHash)
			require.NotNil(t, updated.PasswordChangedAt)
			assert.Equal(t, "2026-03-02 08:00:00", *updated.PasswordChangedAt)

			count, err := store.Tenants.Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, 2, count)

			require.NoError(t, store.Tenants.Delete(ctx, sparse.ID))
			_, err = store.Tenants.ByID(ctx, sparse.ID)
			assert.ErrorIs(t, err, domain.ErrNotFound)
			count, err = store.Tenants.Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, 1, count)
		})
	}
}

// ── Imóveis ────────────────────────────────────────────────────────

func TestPropertiesRepository(t *testing.T) {
	for _, tgt := range targets(t) {
		t.Run(tgt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t, tgt)

			first := &domain.Property{
				ID: "property-1", Name: "Apto 101", Address: "Av. do Contorno, 250",
				City: "Cabo Frio", State: "RJ", Zip: domain.StrPtr("28907-250"),
				CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
			}
			second := &domain.Property{
				ID: "property-2", Name: "Casa das Dunas", Address: "Rua dos Diamantes, 14",
				City: "Cabo Frio", State: "RJ",
				CreatedAt: "2026-01-02 10:00:00", UpdatedAt: "2026-01-02 10:00:00",
			}
			require.NoError(t, store.Properties.Create(ctx, first))
			require.NoError(t, store.Properties.Create(ctx, second))

			list, err := store.Properties.List(ctx)
			require.NoError(t, err)
			require.Len(t, list, 2)
			assert.Equal(t, []string{"Apto 101", "Casa das Dunas"}, []string{list[0].Name, list[1].Name})

			byID, err := store.Properties.ByID(ctx, first.ID)
			require.NoError(t, err)
			assert.Equal(t, *first, *byID)
			byID, err = store.Properties.ByID(ctx, second.ID)
			require.NoError(t, err)
			assert.Nil(t, byID.Zip)

			_, err = store.Properties.ByID(ctx, "inexistente")
			assert.ErrorIs(t, err, domain.ErrNotFound)

			first.Name = "Apto 101 - Praia do Forte"
			first.Zip = nil
			first.UpdatedAt = "2026-02-01 10:00:00"
			require.NoError(t, store.Properties.Update(ctx, first))
			updated, err := store.Properties.ByID(ctx, first.ID)
			require.NoError(t, err)
			assert.Equal(t, "Apto 101 - Praia do Forte", updated.Name)
			assert.Nil(t, updated.Zip)

			count, err := store.Properties.Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, 2, count)

			require.NoError(t, store.Properties.Delete(ctx, second.ID))
			_, err = store.Properties.ByID(ctx, second.ID)
			assert.ErrorIs(t, err, domain.ErrNotFound)
		})
	}
}

// ── Contratos ──────────────────────────────────────────────────────

func TestLeasesRepository(t *testing.T) {
	for _, tgt := range targets(t) {
		t.Run(tgt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t, tgt)
			fixtures := seedPeople(t, store)

			active := &domain.Lease{
				ID: "lease-1", TenantID: fixtures.tenantA, PropertyID: fixtures.propertyA,
				StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 2500,
				Status: domain.LeaseStatusActive, CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
			}
			terminated := &domain.Lease{
				ID: "lease-2", TenantID: fixtures.tenantB, PropertyID: fixtures.propertyB,
				StartDate: "2025-01-01", EndDate: "2025-12-31", DueDay: 5, RentAmount: 1800,
				Status: domain.LeaseStatusTerminated, CreatedAt: "2025-01-01 10:00:00", UpdatedAt: "2025-01-01 10:00:00",
			}
			overlapping := &domain.Lease{
				ID: "lease-3", TenantID: fixtures.tenantB, PropertyID: fixtures.propertyA,
				StartDate: "2026-06-01", EndDate: "2026-08-31", DueDay: 1, RentAmount: 1900,
				Status: domain.LeaseStatusSeason, CreatedAt: "2026-05-01 10:00:00", UpdatedAt: "2026-05-01 10:00:00",
			}
			for _, lease := range []*domain.Lease{active, terminated, overlapping} {
				require.NoError(t, store.Leases.Create(ctx, lease))
			}

			list, err := store.Leases.List(ctx)
			require.NoError(t, err)
			require.Len(t, list, 3)
			assert.Equal(t, []string{"lease-3", "lease-1", "lease-2"}, []string{list[0].ID, list[1].ID, list[2].ID})
			assert.Equal(t, "Carlos Drummond", list[1].TenantName)
			assert.Equal(t, "Apto 101", list[1].PropertyName)

			byID, err := store.Leases.ByID(ctx, active.ID)
			require.NoError(t, err)
			assert.Equal(t, 10, byID.DueDay)
			assert.Equal(t, 2500.0, byID.RentAmount)
			assert.Equal(t, domain.LeaseStatusActive, byID.Status)
			assert.Nil(t, byID.AuthToken)
			assert.Equal(t, "Carlos Drummond", byID.TenantName)
			assert.Equal(t, "Apto 101", byID.PropertyName)

			_, err = store.Leases.ByID(ctx, "inexistente")
			assert.ErrorIs(t, err, domain.ErrNotFound)

			byTenant, err := store.Leases.ByTenant(ctx, fixtures.tenantB)
			require.NoError(t, err)
			require.Len(t, byTenant, 2)
			assert.Equal(t, []string{"lease-3", "lease-2"}, []string{byTenant[0].ID, byTenant[1].ID})

			byProperty, err := store.Leases.ByProperty(ctx, fixtures.propertyA)
			require.NoError(t, err)
			require.Len(t, byProperty, 2)

			// sobreposição ignora encerrado e o contrato excluído
			overlaps, err := store.Leases.Overlapping(ctx, fixtures.propertyA, "", "2026-07-01", "2026-07-31")
			require.NoError(t, err)
			require.Len(t, overlaps, 2)
			overlaps, err = store.Leases.Overlapping(ctx, fixtures.propertyA, overlapping.ID, "2026-07-01", "2026-07-31")
			require.NoError(t, err)
			require.Len(t, overlaps, 1)
			assert.Equal(t, active.ID, overlaps[0].ID)
			overlaps, err = store.Leases.Overlapping(ctx, fixtures.propertyB, "", "2026-07-01", "2026-07-31")
			require.NoError(t, err)
			assert.Empty(t, overlaps)

			inRange, err := store.Leases.ActiveInRange(ctx, "2026-01-01", "2026-12-31")
			require.NoError(t, err)
			require.Len(t, inRange, 1)
			assert.Equal(t, active.ID, inRange[0].ID)

			require.NoError(t, store.Leases.UpdateStatus(ctx, active.ID, domain.LeaseStatusSuspended, "2026-07-01 09:00:00"))
			updated, err := store.Leases.ByID(ctx, active.ID)
			require.NoError(t, err)
			assert.Equal(t, domain.LeaseStatusSuspended, updated.Status)
			assert.Equal(t, "2026-07-01 09:00:00", updated.UpdatedAt)

			terminated.EndDate = "2026-01-31"
			terminated.DueDay = 7
			terminated.RentAmount = 1950
			terminated.Status = domain.LeaseStatusActive
			terminated.UpdatedAt = "2026-02-01 09:00:00"
			require.NoError(t, store.Leases.Update(ctx, terminated))
			updated, err = store.Leases.ByID(ctx, terminated.ID)
			require.NoError(t, err)
			assert.Equal(t, "2026-01-31", updated.EndDate)
			assert.Equal(t, 7, updated.DueDay)
			assert.Equal(t, 1950.0, updated.RentAmount)
			assert.Equal(t, domain.LeaseStatusActive, updated.Status)

			count, err := store.Leases.Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, 3, count)
			count, err = store.Leases.CountByStatus(ctx, domain.LeaseStatusSeason)
			require.NoError(t, err)
			assert.Equal(t, 1, count)

			require.NoError(t, store.Leases.Delete(ctx, overlapping.ID))
			_, err = store.Leases.ByID(ctx, overlapping.ID)
			assert.ErrorIs(t, err, domain.ErrNotFound)
		})
	}
}

// ── Lançamentos ────────────────────────────────────────────────────

func TestReceiptsRepository(t *testing.T) {
	for _, tgt := range targets(t) {
		t.Run(tgt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t, tgt)
			fixtures := seedPeople(t, store)

			lease := &domain.Lease{
				ID: "lease-1", TenantID: fixtures.tenantA, PropertyID: fixtures.propertyA,
				StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 2500,
				Status: domain.LeaseStatusActive, CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
			}
			require.NoError(t, store.Leases.Create(ctx, lease))

			rent := &domain.Receipt{
				ID: "receipt-1", LeaseID: domain.StrPtr(lease.ID), Category: domain.ReceiptCategoryRent,
				DueDate: "2026-07-10", PaidAt: domain.StrPtr("2026-07-09T14:30"), Amount: 2500,
				GeneratedKey: domain.StrPtr(lease.ID + ":2026-07"), CreatedAt: "2026-07-01 10:00:00",
			}
			extra := &domain.Receipt{
				ID: "receipt-2", TenantID: domain.StrPtr(fixtures.tenantB), PropertyID: domain.StrPtr(fixtures.propertyB),
				Category: domain.ReceiptCategoryExtra, Description: domain.StrPtr("Taxa de limpeza"),
				DueDate: "2026-08-15", Amount: 350, CreatedAt: "2026-08-01 10:00:00",
			}
			require.NoError(t, store.Receipts.Create(ctx, rent))
			require.NoError(t, store.Receipts.Create(ctx, extra))

			byID, err := store.Receipts.ByID(ctx, rent.ID)
			require.NoError(t, err)
			assert.Equal(t, "Carlos Drummond", byID.TenantName)
			assert.Equal(t, "Apto 101", byID.PropertyName)
			rent.TenantName, rent.PropertyName = byID.TenantName, byID.PropertyName
			assert.Equal(t, *rent, *byID)

			byID, err = store.Receipts.ByID(ctx, extra.ID)
			require.NoError(t, err)
			assert.Nil(t, byID.LeaseID)
			assert.Equal(t, domain.ReceiptCategoryExtra, byID.Category)
			assert.Equal(t, "Taxa de limpeza", domain.Str(byID.Description))
			assert.Nil(t, byID.PaidAmount)
			assert.Nil(t, byID.UpdatedAt)
			assert.Equal(t, "Clarice Lispector", byID.TenantName)
			assert.Equal(t, "Casa das Dunas", byID.PropertyName)

			_, err = store.Receipts.ByID(ctx, "inexistente")
			assert.ErrorIs(t, err, domain.ErrNotFound)

			list, err := store.Receipts.List(ctx)
			require.NoError(t, err)
			require.Len(t, list, 2)
			assert.Equal(t, []string{"receipt-2", "receipt-1"}, []string{list[0].ID, list[1].ID})

			byLease, err := store.Receipts.ByLease(ctx, lease.ID)
			require.NoError(t, err)
			require.Len(t, byLease, 1)
			assert.Equal(t, rent.ID, byLease[0].ID)

			// o locatário vê tanto o aluguel (pelo contrato) quanto a avulsa
			byTenant, err := store.Receipts.ByTenant(ctx, fixtures.tenantA)
			require.NoError(t, err)
			require.Len(t, byTenant, 1)
			byTenant, err = store.Receipts.ByTenant(ctx, fixtures.tenantB)
			require.NoError(t, err)
			require.Len(t, byTenant, 1)
			assert.Equal(t, extra.ID, byTenant[0].ID)

			// a geração mensal é idempotente pela chave
			duplicate := &domain.Receipt{
				ID: "receipt-3", LeaseID: domain.StrPtr(lease.ID), Category: domain.ReceiptCategoryRent,
				DueDate: "2026-07-10", Amount: 2500,
				GeneratedKey: domain.StrPtr(lease.ID + ":2026-07"), CreatedAt: "2026-07-02 10:00:00",
			}
			inserted, err := store.Receipts.CreateIfAbsent(ctx, duplicate)
			require.NoError(t, err)
			assert.Equal(t, int64(0), inserted)
			inserted, err = store.Receipts.CreateIfAbsent(ctx, &domain.Receipt{
				ID: "receipt-4", LeaseID: domain.StrPtr(lease.ID), Category: domain.ReceiptCategoryRent,
				DueDate: "2026-09-10", Amount: 2500,
				GeneratedKey: domain.StrPtr(lease.ID + ":2026-09"), CreatedAt: "2026-09-01 10:00:00",
			})
			require.NoError(t, err)
			assert.Equal(t, int64(1), inserted)
			count, err := store.Receipts.Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, 3, count)

			rent.Amount = 2600
			rent.DueDate = "2026-07-11"
			rent.Description = domain.StrPtr("Aluguel reajustado")
			rent.UpdatedAt = domain.StrPtr("2026-07-05 08:00:00")
			require.NoError(t, store.Receipts.Update(ctx, rent))
			updated, err := store.Receipts.ByID(ctx, rent.ID)
			require.NoError(t, err)
			assert.Equal(t, 2600.0, updated.Amount)
			assert.Equal(t, "2026-07-11", updated.DueDate)
			assert.Equal(t, "Aluguel reajustado", domain.Str(updated.Description))
			assert.Equal(t, "2026-07-05 08:00:00", domain.Str(updated.UpdatedAt))

			require.NoError(t, store.Receipts.UpdatePayment(ctx, rent.ID, "2026-07-11T09:15", 2600, "2026-07-11 09:15:00"))
			paid, err := store.Receipts.ByID(ctx, rent.ID)
			require.NoError(t, err)
			assert.Equal(t, "2026-07-11T09:15", domain.Str(paid.PaidAt))
			require.NotNil(t, paid.PaidAmount)
			assert.Equal(t, 2600.0, *paid.PaidAmount)
			assert.Equal(t, "2026-07-11 09:15:00", domain.Str(paid.UpdatedAt))

			require.NoError(t, store.Receipts.Delete(ctx, extra.ID))
			_, err = store.Receipts.ByID(ctx, extra.ID)
			assert.ErrorIs(t, err, domain.ErrNotFound)
		})
	}
}

// ── Configurações ──────────────────────────────────────────────────

func TestSettingsRepository(t *testing.T) {
	for _, tgt := range targets(t) {
		t.Run(tgt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t, tgt)

			_, err := store.Settings.Get(ctx, domain.SettingLandlordName)
			assert.ErrorIs(t, err, domain.ErrNotFound)

			require.NoError(t, store.Settings.Set(ctx, domain.SettingLandlordName, "Locador"))
			value, err := store.Settings.Get(ctx, domain.SettingLandlordName)
			require.NoError(t, err)
			assert.Equal(t, "Locador", value)

			require.NoError(t, store.Settings.Set(ctx, domain.SettingLandlordName, "Maria Locadora"))
			value, err = store.Settings.Get(ctx, domain.SettingLandlordName)
			require.NoError(t, err)
			assert.Equal(t, "Maria Locadora", value)

			require.NoError(t, store.Settings.Set(ctx, domain.SettingLandlordCPF, "000.000.000-00"))
			all, err := store.Settings.List(ctx)
			require.NoError(t, err)
			assert.Equal(t, map[string]string{
				domain.SettingLandlordName: "Maria Locadora",
				domain.SettingLandlordCPF:  "000.000.000-00",
			}, all)
		})
	}
}

// ── Auditoria ──────────────────────────────────────────────────────

func TestAuditRepository(t *testing.T) {
	for _, tgt := range targets(t) {
		t.Run(tgt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t, tgt)

			entries := []*domain.AuditLog{
				{
					ID: "audit-1", CreatedAt: "2026-03-05T08:00:00.000", Action: "tenant.create", Entity: "tenant",
					EntityID: domain.StrPtr("tenant-1"), EntityLabel: domain.StrPtr("Carlos Drummond"),
					ActorID: domain.StrPtr("user-1"), ActorName: domain.StrPtr("passoz"),
					Summary: "Locatário criado", Changes: domain.StrPtr(`[{"field":"name","label":"Nome","from":"","to":"Carlos Drummond"}]`),
					Origin: domain.AuditOriginWeb, IP: domain.StrPtr("127.0.0.1"), RequestID: domain.StrPtr("abcd1234"),
				},
				{
					ID: "audit-2", CreatedAt: "2026-03-05T09:00:00.000", Action: "receipt.pay", Entity: "receipt",
					EntityID: domain.StrPtr("receipt-1"), EntityLabel: domain.StrPtr("Carlos Drummond · Apto 101 · 07/2026"),
					ActorName: domain.StrPtr("passoz"), Summary: "Baixa registrada",
					Origin: domain.AuditOriginClient,
				},
				{
					ID: "audit-3", CreatedAt: "2026-03-06T07:30:00.000", Action: "document.contract", Entity: "document",
					EntityLabel: domain.StrPtr("Contrato de Carlos Drummond"), ActorName: domain.StrPtr("Maria"),
					Summary: "Contrato emitido", Origin: domain.AuditOriginPublic,
				},
			}
			for _, entry := range entries {
				require.NoError(t, store.Audit.Insert(ctx, entry))
			}

			// a trilha é lida da mais recente para a mais antiga quando o filtro pede
			list, err := store.Audit.List(ctx, domain.AuditFilter{Limit: 10, Descending: true})
			require.NoError(t, err)
			require.Len(t, list, 3)
			assert.Equal(t, []string{"audit-3", "audit-2", "audit-1"}, []string{list[0].ID, list[1].ID, list[2].ID})
			assert.Equal(t, *entries[0], list[2])
			assert.Equal(t, "client", string(list[1].Origin))

			list, err = store.Audit.List(ctx, domain.AuditFilter{Limit: 10})
			require.NoError(t, err)
			require.Len(t, list, 3)
			assert.Equal(t, []string{"audit-1", "audit-2", "audit-3"}, []string{list[0].ID, list[1].ID, list[2].ID})

			// filtros
			list, err = store.Audit.List(ctx, domain.AuditFilter{Entity: "receipt", Limit: 10, Descending: true})
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.Equal(t, "audit-2", list[0].ID)

			list, err = store.Audit.List(ctx, domain.AuditFilter{Action: "tenant.create", Limit: 10})
			require.NoError(t, err)
			require.Len(t, list, 1)

			list, err = store.Audit.List(ctx, domain.AuditFilter{Actor: "Maria", Limit: 10})
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.Equal(t, "audit-3", list[0].ID)

			// busca sem diferenciar maiúsculas, no resumo e no rótulo
			list, err = store.Audit.List(ctx, domain.AuditFilter{Search: "contrato", Limit: 10})
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.Equal(t, "audit-3", list[0].ID)
			list, err = store.Audit.List(ctx, domain.AuditFilter{Search: "APTO 101", Limit: 10})
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.Equal(t, "audit-2", list[0].ID)
			list, err = store.Audit.List(ctx, domain.AuditFilter{Search: "receipt-1", Limit: 10})
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.Equal(t, "audit-2", list[0].ID)

			// limite superior só com data alcança os eventos do próprio dia
			list, err = store.Audit.List(ctx, domain.AuditFilter{To: "2026-03-05", Limit: 10})
			require.NoError(t, err)
			assert.Len(t, list, 2)
			list, err = store.Audit.List(ctx, domain.AuditFilter{From: "2026-03-06", Limit: 10})
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.Equal(t, "audit-3", list[0].ID)

			// paginação
			page, err := store.Audit.List(ctx, domain.AuditFilter{Limit: 2, Descending: true})
			require.NoError(t, err)
			require.Len(t, page, 2)
			assert.Equal(t, []string{"audit-3", "audit-2"}, []string{page[0].ID, page[1].ID})
			page, err = store.Audit.List(ctx, domain.AuditFilter{Limit: 2, Offset: 2, Descending: true})
			require.NoError(t, err)
			require.Len(t, page, 1)
			assert.Equal(t, "audit-1", page[0].ID)

			total, err := store.Audit.Count(ctx, domain.AuditFilter{})
			require.NoError(t, err)
			assert.Equal(t, 3, total)
			total, err = store.Audit.Count(ctx, domain.AuditFilter{Entity: "tenant"})
			require.NoError(t, err)
			assert.Equal(t, 1, total)
			total, err = store.Audit.Count(ctx, domain.AuditFilter{To: "2026-03-05"})
			require.NoError(t, err)
			assert.Equal(t, 2, total)

			actors, err := store.Audit.Actors(ctx)
			require.NoError(t, err)
			assert.Equal(t, []string{"Maria", "passoz"}, actors)
		})
	}
}

// ── Cascatas de chave estrangeira ──────────────────────────────────

func TestForeignKeysCascade(t *testing.T) {
	for _, tgt := range targets(t) {
		t.Run(tgt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t, tgt)
			fixtures := seedPeople(t, store)

			lease := &domain.Lease{
				ID: "lease-1", TenantID: fixtures.tenantA, PropertyID: fixtures.propertyA,
				StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 2500,
				Status: domain.LeaseStatusActive, CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
			}
			require.NoError(t, store.Leases.Create(ctx, lease))
			require.NoError(t, store.Receipts.Create(ctx, &domain.Receipt{
				ID: "receipt-lease", LeaseID: domain.StrPtr(lease.ID), Category: domain.ReceiptCategoryRent,
				DueDate: "2026-07-10", Amount: 2500, CreatedAt: "2026-07-01 10:00:00",
			}))
			require.NoError(t, store.Receipts.Create(ctx, &domain.Receipt{
				ID: "receipt-extra", TenantID: domain.StrPtr(fixtures.tenantA), PropertyID: domain.StrPtr(fixtures.propertyA),
				Category: domain.ReceiptCategoryExtra, DueDate: "2026-07-20", Amount: 100,
				CreatedAt: "2026-07-02 10:00:00",
			}))

			// apagar o contrato leva os lançamentos dele
			require.NoError(t, store.Leases.Delete(ctx, lease.ID))
			_, err := store.Receipts.ByID(ctx, "receipt-lease")
			assert.ErrorIs(t, err, domain.ErrNotFound)

			// apagar o locatário zera o vínculo do lançamento avulso
			require.NoError(t, store.Tenants.Delete(ctx, fixtures.tenantA))
			extra, err := store.Receipts.ByID(ctx, "receipt-extra")
			require.NoError(t, err)
			assert.Nil(t, extra.TenantID)
			require.NotNil(t, extra.PropertyID)
			assert.Equal(t, fixtures.propertyA, *extra.PropertyID)

			// apagar o imóvel também zera o vínculo
			require.NoError(t, store.Properties.Delete(ctx, fixtures.propertyA))
			extra, err = store.Receipts.ByID(ctx, "receipt-extra")
			require.NoError(t, err)
			assert.Nil(t, extra.PropertyID)
			assert.Empty(t, extra.PropertyName)

			// o locatário B não foi afetado
			_, err = store.Tenants.ByID(ctx, fixtures.tenantB)
			require.NoError(t, err)
		})
	}
}

// ── WhatsApp ───────────────────────────────────────────────────────

func TestWhatsAppMessagesRepository(t *testing.T) {
	for _, tgt := range targets(t) {
		t.Run(tgt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t, tgt)
			fixtures := seedPeople(t, store)

			incoming := &domain.WhatsAppMessage{
				ID: "wa-1", CreatedAt: "2026-03-01T08:00:00.000", Direction: domain.WhatsAppDirectionIn,
				ChatID: "5522998881122@c.us", Phone: "5522998881122", TenantID: domain.StrPtr(fixtures.tenantA),
				Body: "Bom dia, já paguei", Status: domain.WhatsAppStatusReceived,
				ExternalID: domain.StrPtr("wamid.1"), Metadata: domain.StrPtr(`{"source":"waha"}`),
			}
			inserted, err := store.WhatsApp.InsertIfAbsent(ctx, incoming)
			require.NoError(t, err)
			assert.Equal(t, int64(1), inserted)

			// a reentrega do webhook não duplica
			redelivery := *incoming
			redelivery.ID = "wa-1-duplicada"
			inserted, err = store.WhatsApp.InsertIfAbsent(ctx, &redelivery)
			require.NoError(t, err)
			assert.Equal(t, int64(0), inserted)

			// mensagem sem locatário casado (telefone desconhecido)
			inserted, err = store.WhatsApp.InsertIfAbsent(ctx, &domain.WhatsAppMessage{
				ID: "wa-2", CreatedAt: "2026-03-02T08:00:00.000", Direction: domain.WhatsAppDirectionOut,
				ChatID: "5522999999999@c.us", Phone: "5522999999999", Body: "Olá",
				Status: domain.WhatsAppStatusSent, ExternalID: domain.StrPtr("wamid.2"),
			})
			require.NoError(t, err)
			assert.Equal(t, int64(1), inserted)

			inserted, err = store.WhatsApp.InsertIfAbsent(ctx, &domain.WhatsAppMessage{
				ID: "wa-3", CreatedAt: "2026-03-03T08:00:00.000", Direction: domain.WhatsAppDirectionIn,
				ChatID: "5522998881122@c.us", Phone: "5522998881122", TenantID: domain.StrPtr(fixtures.tenantA),
				Body: "Segunda mensagem", Status: domain.WhatsAppStatusReceived,
			})
			require.NoError(t, err)
			assert.Equal(t, int64(1), inserted)

			messages, err := store.WhatsApp.ByTenant(ctx, fixtures.tenantA, 10)
			require.NoError(t, err)
			require.Len(t, messages, 2)
			assert.Equal(t, []string{"wa-3", "wa-1"}, []string{messages[0].ID, messages[1].ID})
			assert.Equal(t, domain.WhatsAppDirectionIn, messages[0].Direction)
			assert.Equal(t, "wamid.1", domain.Str(messages[1].ExternalID))
			assert.Equal(t, `{"source":"waha"}`, domain.Str(messages[1].Metadata))
			assert.Nil(t, messages[0].ExternalID)

			messages, err = store.WhatsApp.ByTenant(ctx, fixtures.tenantA, 1)
			require.NoError(t, err)
			require.Len(t, messages, 1)
			assert.Equal(t, "wa-3", messages[0].ID)

			messages, err = store.WhatsApp.ByTenant(ctx, fixtures.tenantB, 10)
			require.NoError(t, err)
			assert.Empty(t, messages)
		})
	}
}

// ── Vistorias e caução (temporada) ─────────────────────────────────

func TestInspectionsRepository(t *testing.T) {
	for _, tgt := range targets(t) {
		t.Run(tgt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t, tgt)
			fixtures := seedPeople(t, store)
			lease := seedLease(t, store, "lease-1", fixtures.tenantA, fixtures.propertyA, "2026-01-01", "2026-12-31")
			other := seedLease(t, store, "lease-2", fixtures.tenantB, fixtures.propertyB, "2025-01-01", "2025-12-31")

			checkIn := &domain.Inspection{
				ID: "inspection-1", LeaseID: lease.ID, Kind: domain.InspectionKindCheckIn,
				PerformedAt: "2026-01-01", Condition: domain.InspectionConditionGood,
				Notes: domain.StrPtr("Imóvel entregue limpo"),
				Items: []domain.InspectionItem{
					{Area: "Sala", Item: "Sofá", State: "bom", Notes: "pequeno rasgo"},
					{Area: "Cozinha", Item: "Geladeira", State: "ótimo"},
				},
				Photos: []domain.InspectionPhoto{
					{Name: "sala-1.jpg", Label: "Sala", Caption: "vista geral"},
					{Name: "cozinha-1.jpg"},
				},
				AuthToken: domain.StrPtr("token-vistoria"),
				CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
			}
			require.NoError(t, store.Inspections.Create(ctx, checkIn))

			byID, err := store.Inspections.ByID(ctx, checkIn.ID)
			require.NoError(t, err)
			checkIn.TenantName, checkIn.PropertyName = byID.TenantName, byID.PropertyName
			assert.Equal(t, *checkIn, *byID)
			assert.Equal(t, "Carlos Drummond", byID.TenantName)
			assert.Equal(t, "Apto 101", byID.PropertyName)
			assert.Equal(t, "pequeno rasgo", byID.Items[0].Notes)
			assert.Equal(t, "vista geral", byID.Photos[0].Caption)

			_, err = store.Inspections.ByID(ctx, "inexistente")
			assert.ErrorIs(t, err, domain.ErrNotFound)

			// vistoria sem itens nem fotos grava NULL e volta como lista vazia
			checkOut := &domain.Inspection{
				ID: "inspection-2", LeaseID: lease.ID, Kind: domain.InspectionKindCheckOut,
				PerformedAt: "2026-06-30", Condition: domain.InspectionConditionFair,
				CreatedAt: "2026-06-30 18:00:00", UpdatedAt: "2026-06-30 18:00:00",
			}
			require.NoError(t, store.Inspections.Create(ctx, checkOut))

			byID, err = store.Inspections.ByID(ctx, checkOut.ID)
			require.NoError(t, err)
			assert.Empty(t, byID.Items)
			assert.Empty(t, byID.Photos)
			assert.Nil(t, byID.Notes)
			assert.Nil(t, byID.AuthToken)
			assert.Equal(t, domain.InspectionKindCheckOut, byID.Kind)
			assert.Equal(t, domain.InspectionConditionFair, byID.Condition)
			nullColumns := rawInspectionNulls(t, ctx, store)
			assert.True(t, nullColumns["inspection-2"]["items"], "lista vazia de itens deve virar NULL")
			assert.True(t, nullColumns["inspection-2"]["photos"], "lista vazia de fotos deve virar NULL")
			assert.False(t, nullColumns["inspection-1"]["items"])

			// listagem geral: da vistoria mais recente para a mais antiga
			list, err := store.Inspections.List(ctx)
			require.NoError(t, err)
			require.Len(t, list, 2)
			assert.Equal(t, []string{"inspection-2", "inspection-1"}, []string{list[0].ID, list[1].ID})

			byLease, err := store.Inspections.ByLease(ctx, lease.ID)
			require.NoError(t, err)
			require.Len(t, byLease, 2)
			assert.Equal(t, []string{"inspection-2", "inspection-1"}, []string{byLease[0].ID, byLease[1].ID})
			byLease, err = store.Inspections.ByLease(ctx, other.ID)
			require.NoError(t, err)
			assert.Empty(t, byLease)

			// edição troca o conteúdo e volta a gravar NULL quando esvazia
			checkOut.Condition = domain.InspectionConditionBad
			checkOut.Notes = domain.StrPtr("Marcas na parede da sala")
			checkOut.Items = []domain.InspectionItem{{Area: "Sala", Item: "Parede", State: "ruim"}}
			checkOut.Photos = []domain.InspectionPhoto{{Name: "parede-1.jpg"}}
			checkOut.UpdatedAt = "2026-07-01 09:00:00"
			require.NoError(t, store.Inspections.Update(ctx, checkOut))

			updated, err := store.Inspections.ByID(ctx, checkOut.ID)
			require.NoError(t, err)
			assert.Equal(t, domain.InspectionConditionBad, updated.Condition)
			assert.Equal(t, "Marcas na parede da sala", domain.Str(updated.Notes))
			assert.Equal(t, []domain.InspectionItem{{Area: "Sala", Item: "Parede", State: "ruim"}}, updated.Items)
			assert.Equal(t, []domain.InspectionPhoto{{Name: "parede-1.jpg"}}, updated.Photos)

			checkOut.Items = nil
			checkOut.Photos = nil
			require.NoError(t, store.Inspections.Update(ctx, checkOut))
			updated, err = store.Inspections.ByID(ctx, checkOut.ID)
			require.NoError(t, err)
			assert.Empty(t, updated.Items)
			assert.Empty(t, updated.Photos)
			nullColumns = rawInspectionNulls(t, ctx, store)
			assert.True(t, nullColumns["inspection-2"]["items"])
			assert.True(t, nullColumns["inspection-2"]["photos"])

			require.NoError(t, store.Inspections.Delete(ctx, checkOut.ID))
			_, err = store.Inspections.ByID(ctx, checkOut.ID)
			assert.ErrorIs(t, err, domain.ErrNotFound)

			// excluir o contrato leva as vistorias dele
			require.NoError(t, store.Leases.Delete(ctx, lease.ID))
			_, err = store.Inspections.ByID(ctx, checkIn.ID)
			assert.ErrorIs(t, err, domain.ErrNotFound)
			list, err = store.Inspections.List(ctx)
			require.NoError(t, err)
			assert.Empty(t, list)
		})
	}
}

func TestLeaseDeposit(t *testing.T) {
	for _, tgt := range targets(t) {
		t.Run(tgt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t, tgt)
			fixtures := seedPeople(t, store)

			// contrato comum (locação longa): nenhuma caução
			plain := seedLease(t, store, "lease-1", fixtures.tenantA, fixtures.propertyA, "2026-01-01", "2026-12-31")
			read, err := store.Leases.ByID(ctx, plain.ID)
			require.NoError(t, err)
			assert.Nil(t, read.DepositAmount)
			assert.Nil(t, read.DepositReceivedAt)
			assert.Nil(t, read.DepositRefundedAt)
			assert.Nil(t, read.DepositRefundedAmt)
			assert.Nil(t, read.DepositNotes)
			assert.Equal(t, domain.DepositStatusNone, read.DepositStatus)
			assert.False(t, domain.HasDeposit(read.DepositAmount, read.DepositStatus))
			nulls := rawLeaseDepositNulls(t, ctx, store)
			assert.True(t, nulls[plain.ID]["status"], "contrato sem caução grava NULL em deposit_status")
			assert.True(t, nulls[plain.ID]["amount"])

			// contrato de temporada com caução retida
			season := &domain.Lease{
				ID: "lease-2", TenantID: fixtures.tenantB, PropertyID: fixtures.propertyB,
				StartDate: "2026-02-01", EndDate: "2026-02-28", DueDay: 5, RentAmount: 1200,
				Status:        domain.LeaseStatusSeason,
				DepositAmount: domain.FloatPtr(1500), DepositStatus: domain.DepositStatusHeld,
				DepositReceivedAt: domain.StrPtr("2026-02-01T15:00"),
				DepositNotes:      domain.StrPtr("Caução recebida por PIX"),
				CreatedAt:         "2026-01-20 10:00:00", UpdatedAt: "2026-01-20 10:00:00",
			}
			require.NoError(t, store.Leases.Create(ctx, season))

			read, err = store.Leases.ByID(ctx, season.ID)
			require.NoError(t, err)
			require.NotNil(t, read.DepositAmount)
			assert.Equal(t, 1500.0, *read.DepositAmount)
			assert.Equal(t, domain.DepositStatusHeld, read.DepositStatus)
			assert.Equal(t, "2026-02-01T15:00", domain.Str(read.DepositReceivedAt))
			assert.Equal(t, "Caução recebida por PIX", domain.Str(read.DepositNotes))
			assert.True(t, domain.HasDeposit(read.DepositAmount, read.DepositStatus))
			nulls = rawLeaseDepositNulls(t, ctx, store)
			assert.False(t, nulls[season.ID]["status"])
			assert.False(t, nulls[season.ID]["amount"])

			// a devolução entra pela edição do contrato
			season.DepositStatus = domain.DepositStatusPartiallyRefunded
			season.DepositRefundedAt = domain.StrPtr("2026-03-01T11:30")
			season.DepositRefundedAmt = domain.FloatPtr(900)
			season.DepositNotes = domain.StrPtr("Retidos R$ 600,00 do sofá")
			season.UpdatedAt = "2026-03-01 11:30:00"
			require.NoError(t, store.Leases.Update(ctx, season))

			read, err = store.Leases.ByID(ctx, season.ID)
			require.NoError(t, err)
			assert.Equal(t, domain.DepositStatusPartiallyRefunded, read.DepositStatus)
			require.NotNil(t, read.DepositRefundedAmt)
			assert.Equal(t, 900.0, *read.DepositRefundedAmt)
			assert.Equal(t, "2026-03-01T11:30", domain.Str(read.DepositRefundedAt))
			assert.Equal(t, "Retidos R$ 600,00 do sofá", domain.Str(read.DepositNotes))

			// voltar para "sem caução" limpa as colunas
			season.DepositAmount = nil
			season.DepositStatus = domain.DepositStatusNone
			season.DepositReceivedAt = nil
			season.DepositRefundedAt = nil
			season.DepositRefundedAmt = nil
			season.DepositNotes = nil
			season.UpdatedAt = "2026-03-02 09:00:00"
			require.NoError(t, store.Leases.Update(ctx, season))

			read, err = store.Leases.ByID(ctx, season.ID)
			require.NoError(t, err)
			assert.Nil(t, read.DepositAmount)
			assert.Nil(t, read.DepositRefundedAmt)
			assert.Equal(t, domain.DepositStatusNone, read.DepositStatus)
			nulls = rawLeaseDepositNulls(t, ctx, store)
			assert.True(t, nulls[season.ID]["status"])
			assert.True(t, nulls[season.ID]["amount"])

			// a listagem também traz a caução
			list, err := store.Leases.List(ctx)
			require.NoError(t, err)
			require.Len(t, list, 2)
			for _, lease := range list {
				assert.Equal(t, domain.DepositStatusNone, lease.DepositStatus)
			}
		})
	}
}

// ── Apoio ──────────────────────────────────────────────────────────

type people struct {
	tenantA   string
	tenantB   string
	propertyA string
	propertyB string
}

func seedPeople(t *testing.T, store *db.Store) people {
	t.Helper()
	ctx := context.Background()

	people := people{tenantA: "tenant-1", tenantB: "tenant-2", propertyA: "property-1", propertyB: "property-2"}
	require.NoError(t, store.Tenants.Create(ctx, &domain.Tenant{
		ID: people.tenantA, Name: "Carlos Drummond", Email: "carlos@exemplo.com",
		CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
	}))
	require.NoError(t, store.Tenants.Create(ctx, &domain.Tenant{
		ID: people.tenantB, Name: "Clarice Lispector", Email: "clarice@exemplo.com",
		CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
	}))
	require.NoError(t, store.Properties.Create(ctx, &domain.Property{
		ID: people.propertyA, Name: "Apto 101", Address: "Av. do Contorno, 250", City: "Cabo Frio", State: "RJ",
		CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
	}))
	require.NoError(t, store.Properties.Create(ctx, &domain.Property{
		ID: people.propertyB, Name: "Casa das Dunas", Address: "Rua dos Diamantes, 14", City: "Cabo Frio", State: "RJ",
		CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
	}))
	return people
}

// TestOpenResolvesDialect confirma a resolução de dialeto e os acessórios do
// Store (caminho e tamanho do arquivo).
func TestOpenResolvesDialect(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := db.Open(ctx, "", dir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()

	assert.Equal(t, db.DialectSQLite, store.Dialect)
	assert.Equal(t, db.DialectPostgres, db.Dialect("postgres"))
	assert.Equal(t, dir+"/aluxen.db", store.Path())
	require.NoError(t, store.Ping(ctx))
	// as migrações rodaram: o arquivo existe e as tabelas respondem
	_, err = store.Tenants.Count(ctx)
	require.NoError(t, err)
	assert.Positive(t, store.SizeBytes())

	// URL sem esquema conhecido cai no SQLite, como no app atual
	dialect, target, err := db.ResolveDialect("mysql://localhost/aluxen", "")
	require.NoError(t, err)
	assert.Equal(t, db.DialectSQLite, dialect)
	assert.Equal(t, "mysql://localhost/aluxen", target)

	dialect, target, err = db.ResolveDialect("file:/tmp/outro.db", "")
	require.NoError(t, err)
	assert.Equal(t, db.DialectSQLite, dialect)
	assert.Equal(t, "/tmp/outro.db", target)

	dialect, target, err = db.ResolveDialect("postgres://user:pass@host:5432/aluxen", "")
	require.NoError(t, err)
	assert.Equal(t, db.DialectPostgres, dialect)
	assert.Equal(t, "postgres://user:pass@host:5432/aluxen", target)
}

// seedLease cria um contrato comum (sem caução) para os testes de vistoria.
func seedLease(t *testing.T, store *db.Store, id, tenantID, propertyID, startDate, endDate string) *domain.Lease {
	t.Helper()
	lease := &domain.Lease{
		ID: id, TenantID: tenantID, PropertyID: propertyID,
		StartDate: startDate, EndDate: endDate, DueDay: 10, RentAmount: 2500,
		Status: domain.LeaseStatusActive, CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
	}
	require.NoError(t, store.Leases.Create(context.Background(), lease))
	return lease
}

// rawInspectionNulls lê diretamente as colunas de itens e fotos de cada vistoria:
// prova que lista vazia é gravada como NULL, não como JSON vazio.
func rawInspectionNulls(t *testing.T, ctx context.Context, store *db.Store) map[string]map[string]bool {
	t.Helper()
	rows, err := store.DB().QueryContext(ctx, "SELECT id, items IS NULL, photos IS NULL FROM inspections")
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	nulls := map[string]map[string]bool{}
	for rows.Next() {
		var id string
		var itemsNull, photosNull bool
		require.NoError(t, rows.Scan(&id, &itemsNull, &photosNull))
		nulls[id] = map[string]bool{"items": itemsNull, "photos": photosNull}
	}
	require.NoError(t, rows.Err())
	return nulls
}

// rawLeaseDepositNulls lê diretamente as colunas de caução de cada contrato.
func rawLeaseDepositNulls(t *testing.T, ctx context.Context, store *db.Store) map[string]map[string]bool {
	t.Helper()
	rows, err := store.DB().QueryContext(ctx,
		"SELECT id, deposit_status IS NULL, deposit_amount IS NULL FROM leases")
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	nulls := map[string]map[string]bool{}
	for rows.Next() {
		var id string
		var statusNull, amountNull bool
		require.NoError(t, rows.Scan(&id, &statusNull, &amountNull))
		nulls[id] = map[string]bool{"status": statusNull, "amount": amountNull}
	}
	require.NoError(t, rows.Err())
	return nulls
}
