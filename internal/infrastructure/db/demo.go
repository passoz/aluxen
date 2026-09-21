package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// SeedDemo apaga os cadastros e recria os dados de demonstração — mesmos nomes,
// valores, datas e chaves de geração do app atual (`src/db/seed-demo.ts`).
// O administrador e as demais configurações são preservados.
//
// É a rotina usada nas instâncias de demonstração (provisionamento e modo demo
// do WhatsApp); nunca deve rodar em produção com dados reais.
func SeedDemo(ctx context.Context, s *Store) error {
	// O app atual chama initializeDatabase() aqui: o administrador precisa
	// existir para a instância de demonstração ser acessível.
	if err := s.EnsureAdmin(ctx, "", "", "", ""); err != nil {
		return fmt.Errorf("garantir administrador antes da demonstração: %w", err)
	}
	if err := clearDemoData(ctx, s); err != nil {
		return err
	}
	if err := seedDemoSettings(ctx, s); err != nil {
		return err
	}

	stamp := format.FormatUTCStamp(time.Now())
	t1, t2, t3, t4, t5 := newID(), newID(), newID(), newID(), newID()
	p1, p2, p3, p4 := newID(), newID(), newID(), newID()
	l1, l2, l3, l4 := newID(), newID(), newID(), newID()

	tenants := []domain.Tenant{
		{ID: t1, Name: "Carlos Drummond de Andrade", Email: "carlos.drummond@exemplo.com.br", Phone: domain.StrPtr("(22) 99888-1122"), CPF: domain.StrPtr("111.222.333-44"), CreatedAt: stamp, UpdatedAt: stamp},
		{ID: t2, Name: "Clarice Lispector", Email: "clarice.lispector@exemplo.com.br", Phone: domain.StrPtr("(22) 99777-2233"), CPF: domain.StrPtr("222.333.444-55"), CreatedAt: stamp, UpdatedAt: stamp},
		{ID: t3, Name: "Machado de Assis", Email: "machado.assis@exemplo.com.br", Phone: domain.StrPtr("(22) 99666-3344"), CPF: domain.StrPtr("333.444.555-66"), CreatedAt: stamp, UpdatedAt: stamp},
		{ID: t4, Name: "Cecília Meireles", Email: "cecilia.meireles@exemplo.com.br", Phone: domain.StrPtr("(22) 99555-4455"), CPF: domain.StrPtr("444.555.666-77"), CreatedAt: stamp, UpdatedAt: stamp},
		{ID: t5, Name: "Vinicius de Moraes", Email: "vinicius.moraes@exemplo.com.br", Phone: domain.StrPtr("(22) 99444-5566"), CPF: domain.StrPtr("555.666.777-88"), CreatedAt: stamp, UpdatedAt: stamp},
	}
	for i := range tenants {
		if err := s.Tenants.Create(ctx, &tenants[i]); err != nil {
			return fmt.Errorf("semear locatário %q: %w", tenants[i].Name, err)
		}
	}

	properties := []domain.Property{
		{ID: p1, Name: "Apto 101 - Praia do Forte", Address: "Av. do Contorno, 250, Apto 101", City: "Cabo Frio", State: "RJ", Zip: domain.StrPtr("28907-250"), CreatedAt: stamp, UpdatedAt: stamp},
		{ID: p2, Name: "Casa das Dunas", Address: "Rua dos Diamantes, 14, Braga", City: "Cabo Frio", State: "RJ", Zip: domain.StrPtr("28908-120"), CreatedAt: stamp, UpdatedAt: stamp},
		{ID: p3, Name: "Cobertura Caiçara", Address: "Rua das Palmeiras, 300, Cobertura 01, Jardim Caiçara", City: "Cabo Frio", State: "RJ", Zip: domain.StrPtr("28910-370"), CreatedAt: stamp, UpdatedAt: stamp},
		{ID: p4, Name: "Flat Passagem Histórica", Address: "Largo de São Benedito, 88, Passagem", City: "Cabo Frio", State: "RJ", Zip: domain.StrPtr("28906-030"), CreatedAt: stamp, UpdatedAt: stamp},
	}
	for i := range properties {
		if err := s.Properties.Create(ctx, &properties[i]); err != nil {
			return fmt.Errorf("semear imóvel %q: %w", properties[i].Name, err)
		}
	}

	leases := []domain.Lease{
		{ID: l1, TenantID: t1, PropertyID: p1, StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 2500, Status: domain.LeaseStatusActive, CreatedAt: stamp, UpdatedAt: stamp},
		{ID: l2, TenantID: t2, PropertyID: p2, StartDate: "2026-02-01", EndDate: "2027-01-31", DueDay: 5, RentAmount: 3200, Status: domain.LeaseStatusActive, CreatedAt: stamp, UpdatedAt: stamp},
		{ID: l3, TenantID: t3, PropertyID: p3, StartDate: "2026-03-01", EndDate: "2027-02-28", DueDay: 15, RentAmount: 4500, Status: domain.LeaseStatusActive, CreatedAt: stamp, UpdatedAt: stamp},
		{ID: l4, TenantID: t4, PropertyID: p4, StartDate: "2025-01-01", EndDate: "2025-12-31", DueDay: 1, RentAmount: 1800, Status: domain.LeaseStatusTerminated, CreatedAt: stamp, UpdatedAt: stamp},
	}
	for i := range leases {
		if err := s.Leases.Create(ctx, &leases[i]); err != nil {
			return fmt.Errorf("semear contrato %q: %w", leases[i].ID, err)
		}
	}

	receipts := []domain.Receipt{
		{LeaseID: domain.StrPtr(l1), DueDate: "2026-07-10", PaidAt: domain.StrPtr("2026-07-09T14:30"), Amount: 2500, GeneratedKey: domain.StrPtr(l1 + ":2026-07")},
		{LeaseID: domain.StrPtr(l1), DueDate: "2026-08-10", PaidAt: domain.StrPtr("2026-08-10T10:15"), Amount: 2500, GeneratedKey: domain.StrPtr(l1 + ":2026-08")},
		{LeaseID: domain.StrPtr(l2), DueDate: "2026-08-05", PaidAt: domain.StrPtr("2026-08-04T16:45"), Amount: 3200, GeneratedKey: domain.StrPtr(l2 + ":2026-08")},
		{LeaseID: domain.StrPtr(l1), DueDate: "2026-09-10", Amount: 2500, GeneratedKey: domain.StrPtr(l1 + ":2026-09")},
		{LeaseID: domain.StrPtr(l3), DueDate: "2026-09-15", Amount: 4500, GeneratedKey: domain.StrPtr(l3 + ":2026-09")},
		{LeaseID: domain.StrPtr(l2), DueDate: "2026-08-25", Amount: 3200, GeneratedKey: domain.StrPtr(l2 + ":2026-08-late")},
	}
	for i := range receipts {
		receipts[i].ID = newID()
		receipts[i].Category = domain.ReceiptCategoryRent
		receipts[i].CreatedAt = stamp
		if err := s.Receipts.Create(ctx, &receipts[i]); err != nil {
			return fmt.Errorf("semear lançamento %q: %w", domain.Str(receipts[i].GeneratedKey), err)
		}
	}

	slog.Info("banco de demonstração populado", "scope", "db.seed",
		"tenants", len(tenants), "properties", len(properties),
		"leases", len(leases), "receipts", len(receipts))
	return nil
}

// clearDemoData apaga os cadastros na ordem das dependências (filhos primeiro).
// As listas são percorridas em transação pela aplicação, como manda o contrato:
// nenhuma lista de ids entra em SQL dinâmico.
func clearDemoData(ctx context.Context, s *Store) error {
	receipts, err := s.Receipts.List(ctx)
	if err != nil {
		return fmt.Errorf("listar lançamentos da demonstração: %w", err)
	}
	for i := range receipts {
		if err := s.Receipts.Delete(ctx, receipts[i].ID); err != nil {
			return fmt.Errorf("apagar lançamento %q: %w", receipts[i].ID, err)
		}
	}

	leases, err := s.Leases.List(ctx)
	if err != nil {
		return fmt.Errorf("listar contratos da demonstração: %w", err)
	}
	for i := range leases {
		if err := s.Leases.Delete(ctx, leases[i].ID); err != nil {
			return fmt.Errorf("apagar contrato %q: %w", leases[i].ID, err)
		}
	}

	properties, err := s.Properties.List(ctx)
	if err != nil {
		return fmt.Errorf("listar imóveis da demonstração: %w", err)
	}
	for i := range properties {
		if err := s.Properties.Delete(ctx, properties[i].ID); err != nil {
			return fmt.Errorf("apagar imóvel %q: %w", properties[i].ID, err)
		}
	}

	tenants, err := s.Tenants.List(ctx)
	if err != nil {
		return fmt.Errorf("listar locatários da demonstração: %w", err)
	}
	for i := range tenants {
		if err := s.Tenants.Delete(ctx, tenants[i].ID); err != nil {
			return fmt.Errorf("apagar locatário %q: %w", tenants[i].ID, err)
		}
	}
	return nil
}

// seedDemoSettings grava as configurações do locador da demonstração.
func seedDemoSettings(ctx context.Context, s *Store) error {
	settings := []struct{ key, value string }{
		{domain.SettingLandlordName, "Locador Demonstração"},
		{domain.SettingLandlordCPF, "000.000.000-00"},
		{domain.SettingLandlordEmail, "demo@aluxen.local"},
	}
	for _, setting := range settings {
		if err := s.Settings.Set(ctx, setting.key, setting.value); err != nil {
			return fmt.Errorf("gravar configuração de demonstração %q: %w", setting.key, err)
		}
	}
	return nil
}
