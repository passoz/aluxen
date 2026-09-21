package application

import (
	"context"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestPropertyCreateMasksZipAndRecordsAudit(t *testing.T) {
	fixture := newCatalogFixture()

	property, err := PropertyCreate(context.Background(), fixture.deps, fixture.actx(), PropertyInput{
		Name:    "  Casa de Praia 202  ",
		Address: " Av. Atlântica, 500 ",
		City:    "Cabo Frio",
		State:   "RJ",
		Zip:     "28900000",
	})
	require.NoError(t, err)

	require.Equal(t, "gen-1", property.ID)
	require.Equal(t, "Casa de Praia 202", property.Name)
	require.Equal(t, "Av. Atlântica, 500", property.Address)
	require.Equal(t, "Cabo Frio", property.City)
	require.Equal(t, "RJ", property.State)
	require.Equal(t, "28900-000", domain.Str(property.Zip))
	require.Equal(t, catalogStamp, property.CreatedAt)
	require.Equal(t, catalogStamp, property.UpdatedAt)

	stored, err := fixture.properties.ByID(context.Background(), property.ID)
	require.NoError(t, err)
	require.Equal(t, *property, *stored)

	entry := fixture.requireAudit(t)
	require.Equal(t, domain.ActionPropertyCreate, entry.Action)
	require.Equal(t, domain.EntityProperty, entry.Entity)
	require.Equal(t, "Casa de Praia 202", domain.Str(entry.EntityLabel))
	require.Equal(t, `Imóvel "Casa de Praia 202" cadastrado`, entry.Summary)

	changes := auditChanges(t, entry)
	require.Len(t, changes, 5)
	require.Equal(t, "Endereço", changeFor(t, changes, "address").Label)
	require.Equal(t, "UF", changeFor(t, changes, "state").Label)
	require.Equal(t, "28900-000", domain.Str(changeFor(t, changes, "zip").To))
}

func TestPropertyCreateRejectsMissingRequiredFields(t *testing.T) {
	cases := []struct {
		name  string
		input PropertyInput
	}{
		{"sem nome", PropertyInput{Address: "Rua 1", City: "Cabo Frio", State: "RJ"}},
		{"sem endereço", PropertyInput{Name: "Casa", City: "Cabo Frio", State: "RJ"}},
		{"sem cidade", PropertyInput{Name: "Casa", Address: "Rua 1", State: "RJ"}},
		{"sem estado", PropertyInput{Name: "Casa", Address: "Rua 1", City: "Cabo Frio"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newCatalogFixture()

			_, err := PropertyCreate(context.Background(), fixture.deps, fixture.actx(), testCase.input)
			require.Equal(t, "Preencha nome, endereço, cidade e estado.", domain.ValidationMessage(err))
			require.Empty(t, fixture.properties.items)
			require.Empty(t, fixture.audit.entries)
		})
	}
}

func TestPropertyCreateLeavesZipEmpty(t *testing.T) {
	fixture := newCatalogFixture()

	property, err := PropertyCreate(context.Background(), fixture.deps, fixture.actx(), PropertyInput{
		ID: "property-fixo", Name: "Casa das Dunas", Address: "Rua dos Diamantes, 14", City: "Cabo Frio", State: "RJ",
	})
	require.NoError(t, err)
	require.Equal(t, "property-fixo", property.ID)
	require.Nil(t, property.Zip)
	require.Len(t, auditChanges(t, fixture.requireAudit(t)), 4, "CEP vazio não entra no diff")
}

func TestPropertyUpdateRecordsOnlyChangedFields(t *testing.T) {
	fixture := newCatalogFixture()
	fixture.properties.items["p1"] = domain.Property{
		ID: "p1", Name: "Casa de Praia 202", Address: "Av. Atlântica, 500",
		City: "Cabo Frio", State: "RJ", Zip: domain.StrPtr("28900-000"),
		CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
	}

	property, err := PropertyUpdate(context.Background(), fixture.deps, fixture.actx(), PropertyInput{
		ID: "p1", Name: "Casa de Praia 202", Address: "Av. Atlântica, 500",
		City: "Arraial do Cabo", State: "RJ", Zip: "",
	})
	require.NoError(t, err)
	require.Equal(t, "Arraial do Cabo", property.City)
	require.Nil(t, property.Zip)
	require.Equal(t, "2026-01-01 10:00:00", property.CreatedAt)
	require.Equal(t, catalogStamp, property.UpdatedAt)

	entry := fixture.requireAudit(t)
	require.Equal(t, domain.ActionPropertyUpdate, entry.Action)
	require.Equal(t, `Imóvel "Casa de Praia 202" alterado`, entry.Summary)

	changes := auditChanges(t, entry)
	require.Len(t, changes, 2, "nome, endereço e UF iguais ficam fora do diff")
	require.Equal(t, "Cabo Frio", domain.Str(changeFor(t, changes, "city").From))
	require.Equal(t, "Arraial do Cabo", domain.Str(changeFor(t, changes, "city").To))
	require.Equal(t, "28900-000", domain.Str(changeFor(t, changes, "zip").From))
	require.Nil(t, changeFor(t, changes, "zip").To)
}

func TestPropertyUpdateRequiresIDAndExistingRecord(t *testing.T) {
	fixture := newCatalogFixture()

	_, err := PropertyUpdate(context.Background(), fixture.deps, fixture.actx(), PropertyInput{
		Name: "Casa", Address: "Rua 1", City: "Cabo Frio", State: "RJ",
	})
	require.Equal(t, "Preencha nome, endereço, cidade e estado.", domain.ValidationMessage(err))

	_, err = PropertyUpdate(context.Background(), fixture.deps, fixture.actx(), PropertyInput{
		ID: "inexistente", Name: "Casa", Address: "Rua 1", City: "Cabo Frio", State: "RJ",
	})
	require.ErrorIs(t, err, domain.ErrNotFound)
	require.Empty(t, fixture.audit.entries)
}

func TestPropertyDeleteDoesNotBlockOnActiveLease(t *testing.T) {
	fixture := newCatalogFixture()
	fixture.properties.items["p1"] = domain.Property{
		ID: "p1", Name: "Casa de Praia 202", Address: "Av. Atlântica, 500",
		City: "Cabo Frio", State: "RJ", Zip: domain.StrPtr("28900-000"),
	}
	fixture.leases.items = []domain.Lease{{ID: "l1", TenantID: "t1", PropertyID: "p1", Status: domain.LeaseStatusActive}}

	require.NoError(t, PropertyDelete(context.Background(), fixture.deps, fixture.actx(), "p1"))
	require.NotContains(t, fixture.properties.items, "p1")

	entry := fixture.requireAudit(t)
	require.Equal(t, domain.ActionPropertyDelete, entry.Action)
	require.Equal(t, domain.EntityProperty, entry.Entity)
	require.Equal(t, "Casa de Praia 202", domain.Str(entry.EntityLabel))
	require.Equal(t, `Imóvel "Casa de Praia 202" excluído`, entry.Summary)

	changes := auditChanges(t, entry)
	require.Len(t, changes, 5)
	for _, change := range changes {
		require.NotNil(t, change.From)
		require.Nil(t, change.To, "exclusão apaga todos os campos no diff")
	}
}

func TestPropertyDeleteRequiresIDAndExistingRecord(t *testing.T) {
	fixture := newCatalogFixture()

	require.Equal(t, "Registro não informado.", domain.ValidationMessage(
		PropertyDelete(context.Background(), fixture.deps, fixture.actx(), "")))
	require.ErrorIs(t, PropertyDelete(context.Background(), fixture.deps, fixture.actx(), "inexistente"), domain.ErrNotFound)
}
