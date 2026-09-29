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
	p1, p2, p3, p4, p5 := newID(), newID(), newID(), newID(), newID()
	l1, l2, l3, l4, l5 := newID(), newID(), newID(), newID(), newID()

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
		{ID: p5, Name: "Casa de Búzios", Address: "Rua das Conchas, 45, Geribá", City: "Búzios", State: "RJ", Zip: domain.StrPtr("28950-000"), CreatedAt: stamp, UpdatedAt: stamp},
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
		// Temporada: a demonstração mostra a caução retida e a vistoria de entrada
		// da estadia de fim de ano (o único contrato com caução, de propósito).
		{
			ID: l5, TenantID: t5, PropertyID: p5, StartDate: "2026-12-20", EndDate: "2027-01-05",
			DueDay: 20, RentAmount: 4800, Status: domain.LeaseStatusSeason,
			DepositAmount: domain.FloatPtr(4800), DepositStatus: domain.DepositStatusHeld,
			DepositReceivedAt: domain.StrPtr("2026-12-20T14:00"),
			DepositNotes:      domain.StrPtr("Caução recebida por PIX"),
			CreatedAt:         stamp, UpdatedAt: stamp,
		},
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

	inspection := &domain.Inspection{
		ID: newID(), LeaseID: l5, Kind: domain.InspectionKindCheckIn,
		PerformedAt: "2026-12-20", Condition: domain.InspectionConditionGood,
		Notes: domain.StrPtr("Imóvel entregue limpo e com enxoval novo"),
		Items: []domain.InspectionItem{
			{Area: "Sala", Item: "Sofá", State: "bom"},
			{Area: "Quarto", Item: "Ar-condicionado", State: "bom", Notes: "filtro limpo"},
			{Area: "Cozinha", Item: "Geladeira", State: "ótimo"},
		},
		CreatedAt: stamp, UpdatedAt: stamp,
	}
	if err := s.Inspections.Create(ctx, inspection); err != nil {
		return fmt.Errorf("semear vistoria de entrada: %w", err)
	}
	if s.PaymentProofs != nil {
		if err := seedDemoPaymentProofs(ctx, s, stamp, t1, t2, receipts[5].ID, receipts[3].ID, receipts[1].ID); err != nil {
			return fmt.Errorf("semear comprovantes de demonstração: %w", err)
		}
	}
	if s.WhatsApp != nil {
		if err := seedDemoWhatsApp(ctx, s, stamp, t1, t2); err != nil {
			return fmt.Errorf("semear mensagens de whatsapp da demonstração: %w", err)
		}
	}
	if s.Intakes != nil {
		if err := seedDemoIntakes(ctx, s, stamp); err != nil {
			return fmt.Errorf("semear cadastros propostos da demonstração: %w", err)
		}
	}
	if s.Audit != nil {
		if err := seedDemoAudit(ctx, s, stamp, l1, l2, l3, receipts[2].ID, inspection.ID); err != nil {
			return fmt.Errorf("semear trilha de auditoria da demonstração: %w", err)
		}
	}

	slog.Info("banco de demonstração populado", "scope", "db.seed",
		"tenants", len(tenants), "properties", len(properties),
		"leases", len(leases), "receipts", len(receipts), "inspections", 1)
	return nil
}

func seedDemoPaymentProofs(ctx context.Context, s *Store, stamp, t1, t2, lateReceiptID, openReceiptID, paidReceiptID string) error {
	proofs := []domain.PaymentProof{
		{
			ID:                  newID(),
			CreatedAt:           stamp,
			UpdatedAt:           stamp,
			Source:              domain.PaymentProofSourceWhatsApp,
			Phone:               domain.StrPtr("(22) 99777-2233"),
			TenantID:            domain.StrPtr(t2),
			ReceiptID:           domain.StrPtr(lateReceiptID),
			FileName:            domain.StrPtr("comprovante-pix-clarice.png"),
			FilePath:            domain.StrPtr("comprovante-pix-clarice.png"),
			Mime:                domain.StrPtr("image/png"),
			ExtractedAmount:     domain.FloatPtr(3200),
			ExtractedDate:       domain.StrPtr("2026-08-26"),
			ExtractedPayer:      domain.StrPtr("Clarice Lispector"),
			ExtractedPayee:      domain.StrPtr("Locador Demonstração"),
			ExtractedIdentifier: domain.StrPtr("E2026082614300012345"),
			Confidence:          domain.FloatPtr(0.92),
			Status:              domain.PaymentProofStatusReview,
			Decision:            domain.PaymentDecisionPending,
		},
		{
			ID:                  newID(),
			CreatedAt:           stamp,
			UpdatedAt:           stamp,
			Source:              domain.PaymentProofSourcePortal,
			TenantID:            domain.StrPtr(t1),
			ReceiptID:           domain.StrPtr(openReceiptID),
			FileName:            domain.StrPtr("recibo-ted-carlos.pdf"),
			FilePath:            domain.StrPtr("recibo-ted-carlos.pdf"),
			Mime:                domain.StrPtr("application/pdf"),
			ExtractedAmount:     domain.FloatPtr(2500),
			ExtractedDate:       domain.StrPtr("2026-09-09"),
			ExtractedPayer:      domain.StrPtr("Carlos Drummond de Andrade"),
			ExtractedPayee:      domain.StrPtr("Locador"),
			ExtractedIdentifier: domain.StrPtr("TED-994821"),
			Confidence:          domain.FloatPtr(0.85),
			Status:              domain.PaymentProofStatusReview,
			Decision:            domain.PaymentDecisionPending,
		},
		{
			ID:                  newID(),
			CreatedAt:           stamp,
			UpdatedAt:           stamp,
			Source:              domain.PaymentProofSourceWhatsApp,
			Phone:               domain.StrPtr("(22) 99888-1122"),
			TenantID:            domain.StrPtr(t1),
			ReceiptID:           domain.StrPtr(paidReceiptID),
			FileName:            domain.StrPtr("pix-agosto-carlos.jpg"),
			FilePath:            domain.StrPtr("pix-agosto-carlos.jpg"),
			Mime:                domain.StrPtr("image/jpeg"),
			ExtractedAmount:     domain.FloatPtr(2500),
			ExtractedDate:       domain.StrPtr("2026-08-10"),
			ExtractedPayer:      domain.StrPtr("Carlos Drummond"),
			ExtractedPayee:      domain.StrPtr("Locador"),
			ExtractedIdentifier: domain.StrPtr("PIX-0810-99"),
			Confidence:          domain.FloatPtr(0.99),
			Status:              domain.PaymentProofStatusAccepted,
			Decision:            domain.PaymentDecisionAuto,
			DecisionReason:      domain.StrPtr("Valor, vencimento e destinatário conferem com o lançamento."),
		},
	}
	for i := range proofs {
		if err := s.PaymentProofs.Create(ctx, &proofs[i]); err != nil {
			return err
		}
	}
	return nil
}

func seedDemoWhatsApp(ctx context.Context, s *Store, stamp, t1, t2 string) error {
	ext1, ext2, ext3, ext4 := newID(), newID(), newID(), newID()
	messages := []domain.WhatsAppMessage{
		{
			ID:         newID(),
			CreatedAt:  "2026-08-26 09:15:00",
			Direction:  domain.WhatsAppDirectionOut,
			ChatID:     "5522997772233@c.us",
			Phone:      "5522997772233",
			TenantID:   domain.StrPtr(t2),
			Body:       "Olá Clarice, seu aluguel da Casa das Dunas venceu em 25/08 no valor de R$ 3.200,00. Segue a chave PIX para pagamento.",
			Status:     "delivered",
			ExternalID: &ext1,
		},
		{
			ID:         newID(),
			CreatedAt:  "2026-08-26 10:20:00",
			Direction:  domain.WhatsAppDirectionIn,
			ChatID:     "5522997772233@c.us",
			Phone:      "5522997772233",
			TenantID:   domain.StrPtr(t2),
			Body:       "Bom dia! Fiz o PIX agora mesmo, segue o comprovante anexo. Desculpe pelo atraso!",
			Status:     "received",
			ExternalID: &ext2,
		},
		{
			ID:         newID(),
			CreatedAt:  "2026-08-10 10:30:00",
			Direction:  domain.WhatsAppDirectionOut,
			ChatID:     "5522998881122@c.us",
			Phone:      "5522998881122",
			TenantID:   domain.StrPtr(t1),
			Body:       "Olá Carlos, confirmamos o recebimento do seu aluguel referente a Agosto/2026. Obrigado!",
			Status:     "delivered",
			ExternalID: &ext3,
		},
		{
			ID:         newID(),
			CreatedAt:  "2026-08-10 10:35:00",
			Direction:  domain.WhatsAppDirectionIn,
			ChatID:     "5522998881122@c.us",
			Phone:      "5522998881122",
			TenantID:   domain.StrPtr(t1),
			Body:       "Muito obrigado pelo envio rápido do recibo!",
			Status:     "received",
			ExternalID: &ext4,
		},
	}
	for i := range messages {
		if _, err := s.WhatsApp.InsertIfAbsent(ctx, &messages[i]); err != nil {
			return err
		}
	}
	return nil
}

func seedDemoIntakes(ctx context.Context, s *Store, stamp string) error {
	intake := &domain.DocumentIntake{
		ID:         newID(),
		CreatedAt:  stamp,
		UpdatedAt:  stamp,
		Kind:       domain.IntakeKindLease,
		FileName:   domain.StrPtr("minuta-contrato-machado.pdf"),
		FilePath:   domain.StrPtr("minuta-contrato-machado.pdf"),
		Mime:       domain.StrPtr("application/pdf"),
		Status:     domain.IntakeStatusPending,
		Confidence: domain.FloatPtr(0.95),
		Notes:      domain.StrPtr("Contrato de locação residencial lido via IA, aguardando aprovação."),
	}
	return s.Intakes.Create(ctx, intake)
}

func seedDemoAudit(ctx context.Context, s *Store, stamp, l1, l2, l3, rPaidID, inspID string) error {
	entries := []domain.AuditLog{
		{
			ID:          newID(),
			CreatedAt:   "2026-09-01 10:00:00",
			ActorName:   domain.StrPtr("passoz"),
			Action:      domain.ActionLogin,
			Entity:      domain.EntityUser,
			EntityID:    domain.StrPtr("passoz"),
			EntityLabel: domain.StrPtr("passoz"),
			Origin:      domain.AuditOriginWeb,
			Summary:     "Login realizado com sucesso · passoz",
		},
		{
			ID:          newID(),
			CreatedAt:   "2026-09-02 11:30:00",
			ActorName:   domain.StrPtr("passoz"),
			Action:      domain.ActionLeaseCreate,
			Entity:      domain.EntityLease,
			EntityID:    domain.StrPtr(l3),
			EntityLabel: domain.StrPtr("Machado de Assis · Cobertura Caiçara"),
			Origin:      domain.AuditOriginWeb,
			Summary:     "Contrato criado · Machado de Assis (Cobertura Caiçara)",
		},
		{
			ID:          newID(),
			CreatedAt:   "2026-09-05 14:15:00",
			ActorName:   domain.StrPtr("passoz"),
			Action:      domain.ActionReceiptPay,
			Entity:      domain.EntityReceipt,
			EntityID:    domain.StrPtr(rPaidID),
			EntityLabel: domain.StrPtr("Clarice Lispector · R$ 3.200,00"),
			Origin:      domain.AuditOriginWeb,
			Summary:     "Baixa de pagamento confirmada · R$ 3.200,00",
		},
		{
			ID:          newID(),
			CreatedAt:   "2026-09-10 16:45:00",
			ActorName:   domain.StrPtr("passoz"),
			Action:      domain.ActionInspectionCreate,
			Entity:      domain.EntityInspection,
			EntityID:    domain.StrPtr(inspID),
			EntityLabel: domain.StrPtr("Vistoria de entrada · Casa de Búzios"),
			Origin:      domain.AuditOriginWeb,
			Summary:     "Vistoria de entrada realizada · Casa de Búzios",
		},
		{
			ID:          newID(),
			CreatedAt:   "2026-09-15 09:00:00",
			ActorName:   domain.StrPtr("passoz"),
			Action:      domain.ActionSettingLandlord,
			Entity:      domain.EntitySetting,
			EntityID:    domain.StrPtr(domain.SettingLandlordName),
			EntityLabel: domain.StrPtr("Locador Demonstração"),
			Origin:      domain.AuditOriginWeb,
			Summary:     "Configurações do locador atualizadas",
		},
	}
	for i := range entries {
		if err := s.Audit.Insert(ctx, &entries[i]); err != nil {
			return err
		}
	}
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
	if s.db != nil {
		tables := []string{"payment_proofs", "whatsapp_messages", "document_intakes", "audit_logs"}
		for _, table := range tables {
			if _, err := s.db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
				slog.Warn("limpeza auxiliar de demonstração", "table", table, "err", err)
			}
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
