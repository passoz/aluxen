package db_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db"
)

// ── Administrador semeado ──────────────────────────────────────────

func TestEnsureAdminCreatesSeededUser(t *testing.T) {
	for _, tgt := range targets(t) {
		t.Run(tgt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t, tgt)

			require.NoError(t, store.EnsureAdmin(ctx, "passoz", "senha-de-teste", "Locador", ""))

			user, err := store.Users.ByUsername(ctx, "passoz")
			require.NoError(t, err)
			assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("senha-de-teste")))
			cost, err := bcrypt.Cost([]byte(user.PasswordHash))
			require.NoError(t, err)
			assert.Equal(t, 10, cost)

			// a troca de senha é obrigatória no primeiro acesso
			assert.Nil(t, user.PasswordChangedAt)
			assert.True(t, user.MustChangePassword())
			assert.NotEmpty(t, user.CreatedAt)

			// configurações padrão do locador
			name, err := store.Settings.Get(ctx, domain.SettingLandlordName)
			require.NoError(t, err)
			assert.Equal(t, "Locador", name)
			cpf, err := store.Settings.Get(ctx, domain.SettingLandlordCPF)
			require.NoError(t, err)
			assert.Empty(t, cpf)
		})
	}
}

func TestEnsureAdminKeepsExistingUser(t *testing.T) {
	for _, tgt := range targets(t) {
		t.Run(tgt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t, tgt)

			require.NoError(t, store.Users.Create(ctx, &domain.User{
				ID: "user-1", Username: "passoz", PasswordHash: "hash-do-usuario",
				PasswordChangedAt: domain.StrPtr("2026-02-01 09:00:00"), CreatedAt: "2026-01-01 10:00:00",
			}))
			require.NoError(t, store.Settings.Set(ctx, domain.SettingLandlordName, "Maria Locadora"))

			require.NoError(t, store.EnsureAdmin(ctx, "passoz", "senha-de-teste", "Locador", ""))

			user, err := store.Users.ByUsername(ctx, "passoz")
			require.NoError(t, err)
			assert.Equal(t, "hash-do-usuario", user.PasswordHash, "a senha existente não pode ser reescrita")
			require.NotNil(t, user.PasswordChangedAt)

			// as configurações padrão também não sobrescrevem valor existente
			name, err := store.Settings.Get(ctx, domain.SettingLandlordName)
			require.NoError(t, err)
			assert.Equal(t, "Maria Locadora", name)
		})
	}
}

func TestEnsureAdminRenamesLegacyUser(t *testing.T) {
	for _, tgt := range targets(t) {
		t.Run(tgt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t, tgt)

			require.NoError(t, store.Users.Create(ctx, &domain.User{
				ID: "user-1", Username: "admin", PasswordHash: "hash-antigo", CreatedAt: "2025-01-01 10:00:00",
			}))

			require.NoError(t, store.EnsureAdmin(ctx, "passoz", "senha-de-teste", "Locador", ""))

			_, err := store.Users.ByUsername(ctx, "admin")
			assert.ErrorIs(t, err, domain.ErrNotFound)

			renamed, err := store.Users.ByUsername(ctx, "passoz")
			require.NoError(t, err)
			assert.Equal(t, "user-1", renamed.ID)
			assert.Equal(t, "hash-antigo", renamed.PasswordHash, "a renomeação preserva a senha do usuário")
		})
	}
}

// ── Dados de demonstração ──────────────────────────────────────────

func TestSeedDemo(t *testing.T) {
	for _, tgt := range targets(t) {
		t.Run(tgt.name, func(t *testing.T) {
			ctx := context.Background()
			store := openStore(t, tgt)

			require.NoError(t, db.SeedDemo(ctx, store))

			tenants, err := store.Tenants.List(ctx)
			require.NoError(t, err)
			assert.Equal(t, []string{
				"Carlos Drummond de Andrade", "Cecília Meireles", "Clarice Lispector",
				"Machado de Assis", "Vinicius de Moraes",
			}, tenantNames(tenants))
			assert.Equal(t, "(22) 99888-1122", domain.Str(tenants[0].Phone))
			assert.Equal(t, "111.222.333-44", domain.Str(tenants[0].CPF))

			properties, err := store.Properties.List(ctx)
			require.NoError(t, err)
			assert.Equal(t, []string{
				"Apto 101 - Praia do Forte", "Casa das Dunas", "Casa de Búzios",
				"Cobertura Caiçara", "Flat Passagem Histórica",
			}, propertyNames(properties))

			leases, err := store.Leases.List(ctx)
			require.NoError(t, err)
			require.Len(t, leases, 5)
			byTenant := map[string]domain.Lease{}
			for _, lease := range leases {
				byTenant[lease.TenantName] = lease
			}
			carlos := byTenant["Carlos Drummond de Andrade"]
			assert.Equal(t, "2026-01-01", carlos.StartDate)
			assert.Equal(t, "2026-12-31", carlos.EndDate)
			assert.Equal(t, 10, carlos.DueDay)
			assert.Equal(t, 2500.0, carlos.RentAmount)
			assert.Equal(t, domain.LeaseStatusActive, carlos.Status)
			assert.Equal(t, "Apto 101 - Praia do Forte", carlos.PropertyName)
			assert.Equal(t, domain.LeaseStatusTerminated, byTenant["Cecília Meireles"].Status)

			// a temporada de fim de ano aparece com a caução retida
			season := byTenant["Vinicius de Moraes"]
			assert.Equal(t, domain.LeaseStatusSeason, season.Status)
			assert.Equal(t, "2026-12-20", season.StartDate)
			assert.Equal(t, "2027-01-05", season.EndDate)
			assert.Equal(t, 4800.0, season.RentAmount)
			assert.Equal(t, "Casa de Búzios", season.PropertyName)
			require.NotNil(t, season.DepositAmount)
			assert.Equal(t, 4800.0, *season.DepositAmount)
			assert.Equal(t, domain.DepositStatusHeld, season.DepositStatus)
			assert.Equal(t, "2026-12-20T14:00", domain.Str(season.DepositReceivedAt))
			assert.Equal(t, "Caução recebida por PIX", domain.Str(season.DepositNotes))
			assert.True(t, domain.HasDeposit(season.DepositAmount, season.DepositStatus))
			assert.Nil(t, season.DepositRefundedAt)
			assert.Nil(t, season.DepositRefundedAmt)

			// e a vistoria de entrada dessa estadia
			inspections, err := store.Inspections.List(ctx)
			require.NoError(t, err)
			require.Len(t, inspections, 1)
			checkIn := inspections[0]
			assert.Equal(t, season.ID, checkIn.LeaseID)
			assert.Equal(t, domain.InspectionKindCheckIn, checkIn.Kind)
			assert.Equal(t, domain.InspectionConditionGood, checkIn.Condition)
			assert.Equal(t, "2026-12-20", checkIn.PerformedAt)
			assert.Equal(t, "Vinicius de Moraes", checkIn.TenantName)
			assert.Equal(t, "Casa de Búzios", checkIn.PropertyName)
			assert.Len(t, checkIn.Items, 3)
			assert.Equal(t, []domain.InspectionItem{
				{Area: "Sala", Item: "Sofá", State: "bom"},
				{Area: "Quarto", Item: "Ar-condicionado", State: "bom", Notes: "filtro limpo"},
				{Area: "Cozinha", Item: "Geladeira", State: "ótimo"},
			}, checkIn.Items)
			assert.Empty(t, checkIn.Photos)

			byLease, err := store.Inspections.ByLease(ctx, season.ID)
			require.NoError(t, err)
			require.Len(t, byLease, 1)
			assert.Equal(t, checkIn.ID, byLease[0].ID)

			receipts, err := store.Receipts.List(ctx)
			require.NoError(t, err)
			require.Len(t, receipts, 6)

			byKey := map[string]domain.Receipt{}
			for _, receipt := range receipts {
				byKey[domain.Str(receipt.GeneratedKey)] = receipt
			}
			clarice := byTenant["Clarice Lispector"]
			late := byKey[clarice.ID+":2026-08-late"]
			assert.Equal(t, 3200.0, late.Amount)
			assert.Equal(t, "2026-08-25", late.DueDate)
			assert.Nil(t, late.PaidAt)
			assert.Equal(t, "Clarice Lispector", late.TenantName)
			assert.Equal(t, "Casa das Dunas", late.PropertyName)
			assert.Equal(t, domain.ReceiptCategoryRent, late.Category)

			paid := byKey[carlos.ID+":2026-08"]
			assert.Equal(t, "2026-08-10T10:15", domain.Str(paid.PaidAt))
			assert.Equal(t, 2500.0, paid.Amount)

			// três pagos, três em aberto (um vencido)
			paidCount := 0
			for _, receipt := range receipts {
				if receipt.PaidAt != nil {
					paidCount++
				}
			}
			assert.Equal(t, 3, paidCount)

			// configurações do locador de demonstração
			name, err := store.Settings.Get(ctx, domain.SettingLandlordName)
			require.NoError(t, err)
			assert.Equal(t, "Locador Demonstração", name)
			cpf, err := store.Settings.Get(ctx, domain.SettingLandlordCPF)
			require.NoError(t, err)
			assert.Equal(t, "000.000.000-00", cpf)
			email, err := store.Settings.Get(ctx, domain.SettingLandlordEmail)
			require.NoError(t, err)
			assert.Equal(t, "demo@aluxen.local", email)

			// o administrador continua existindo
			_, err = store.Users.ByUsername(ctx, "passoz")
			require.NoError(t, err)

			// rodar de novo limpa e recria, sem duplicar
			require.NoError(t, db.SeedDemo(ctx, store))
			count, err := store.Receipts.Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, 6, count)
			count, err = store.Tenants.Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, 5, count)
			count, err = store.Leases.Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, 5, count)
			count, err = store.Properties.Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, 5, count)
			inspections, err = store.Inspections.List(ctx)
			require.NoError(t, err)
			assert.Len(t, inspections, 1, "a vistoria antiga sai junto com o contrato excluído")
		})
	}
}

func tenantNames(tenants []domain.Tenant) []string {
	names := make([]string, 0, len(tenants))
	for _, tenant := range tenants {
		names = append(names, tenant.Name)
	}
	return names
}

func propertyNames(properties []domain.Property) []string {
	names := make([]string, 0, len(properties))
	for _, property := range properties {
		names = append(names, property.Name)
	}
	return names
}
