package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/domain"
)

// Testes de cobertura da fatia `csea`: caução, vistoria, calendário e a busca
// da vistoria por contrato. O foco é o comportamento observável — o que ficou
// gravado nas caixas, o que a função devolveu, qual erro saiu e o que foi
// registrado na trilha. Os dublês vêm do harness compartilhado (`cov_*`).

// cseaBlockingPeriod é um período ocupado de conveniência para a grade.
func cseaBlockingPeriod(leaseID, start, end string, status domain.LeaseStatus, tenant string) CalendarPeriod {
	return CalendarPeriod{
		LeaseID: leaseID, TenantName: tenant, PropertyID: "p", PropertyName: "Imóvel",
		StartDate: start, EndDate: end, Status: status, StatusLabel: domain.LeaseStatusLabel(status),
	}
}

// cseaSeedInspection deposita uma vistoria crua na caixa, sem passar pelo caso
// de uso — mantém o teste isolado do fluxo de criação.
func cseaSeedInspection(env *covEnv, inspection domain.Inspection) {
	env.inspections.items = append(env.inspections.items, inspection)
}

// ── Caução ─────────────────────────────────────────────────────────

func TestCseaSetLeaseDepositSeasonAndLongTerm(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	env.leases.items = []domain.Lease{
		covLeaseOf("l-season", "t1", "p1", domain.LeaseStatusSeason, "2026-09-01", "2026-09-30", 2500),
		covLeaseOf("l-active", "t2", "p2", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 3200),
	}
	env.leases.items[0].TenantName = "Carlos"
	env.leases.items[0].PropertyName = "Apto 101"
	env.leases.items[1].TenantName = "Clarice"
	env.leases.items[1].PropertyName = "Casa"

	season, err := SetLeaseDeposit(ctx, env.deps, covCtx(), DepositInput{
		LeaseID: "l-season", Amount: 1200, ReceivedAt: "2026-09-15 09:30", Notes: "caução de entrada",
	})
	if err != nil {
		t.Fatalf("caução de temporada: %v", err)
	}
	if season.DepositAmount == nil || *season.DepositAmount != 1200 {
		t.Fatalf("valor gravado = %v", season.DepositAmount)
	}
	if season.DepositStatus != domain.DepositStatusHeld {
		t.Fatalf("situação = %q, esperada held", season.DepositStatus)
	}
	if season.DepositReceivedAt == nil || *season.DepositReceivedAt != "2026-09-15 09:30" {
		t.Fatalf("recebimento gravado = %v", season.DepositReceivedAt)
	}
	if season.DepositNotes == nil || *season.DepositNotes != "caução de entrada" {
		t.Fatalf("observação gravada = %v", season.DepositNotes)
	}
	// O mesmo fluxo serve a locação longa: nada no contrato de temporada é
	// pré-requisito para reter a caução.
	longTerm, err := SetLeaseDeposit(ctx, env.deps, covCtx(), DepositInput{LeaseID: "l-active", Amount: 900})
	if err != nil {
		t.Fatalf("caução de locação longa: %v", err)
	}
	// Sem data informada o recebimento cai no relógio fixo do ambiente.
	if longTerm.DepositReceivedAt == nil || *longTerm.DepositReceivedAt == "" {
		t.Fatal("recebimento padrão não gravado")
	}
	if longTerm.DepositNotes != nil {
		t.Fatalf("observação ausente virou %v", longTerm.DepositNotes)
	}

	// A trilha guarda as duas retenções com o tipo marcado.
	if actions := covAuditActions(env.audit.entries); len(actions) != 2 {
		t.Fatalf("eventos = %v", actions)
	}
	for _, entry := range env.audit.entries {
		if entry.Action != domain.ActionLeaseUpdate {
			t.Fatalf("ação inesperada: %q", entry.Action)
		}
		if entry.Changes == nil || *entry.Changes == "" {
			t.Fatal("retenção sem diff de campos")
		}
	}
}

func TestCseaSetLeaseDepositRejections(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	env.leases.items = []domain.Lease{
		covLeaseOf("l1", "t1", "p1", domain.LeaseStatusSeason, "2026-09-01", "2026-09-30", 2500),
		{ID: "l-refunded", Status: domain.LeaseStatusSeason, DepositStatus: domain.DepositStatusRefunded},
		{ID: "l-partial", Status: domain.LeaseStatusSeason, DepositStatus: domain.DepositStatusPartiallyRefunded},
	}

	if _, err := SetLeaseDeposit(ctx, env.deps, covCtx(), DepositInput{LeaseID: "ghost", Amount: 100}); !domain.IsValidation(err) {
		t.Fatalf("contrato inexistente: veio %v", err)
	}
	if _, err := SetLeaseDeposit(ctx, env.deps, covCtx(), DepositInput{LeaseID: "l1"}); !domain.IsValidation(err) {
		t.Fatalf("valor zero deveria ser recusado: veio %v", err)
	}
	if _, err := SetLeaseDeposit(ctx, env.deps, covCtx(), DepositInput{LeaseID: "l1", Amount: -50}); !domain.IsValidation(err) {
		t.Fatalf("valor negativo deveria ser recusado: veio %v", err)
	}
	if _, err := SetLeaseDeposit(ctx, env.deps, covCtx(), DepositInput{LeaseID: "l-refunded", Amount: 300}); !domain.IsValidation(err) {
		t.Fatalf("caução já devolvida: veio %v", err)
	}
	if _, err := SetLeaseDeposit(ctx, env.deps, covCtx(), DepositInput{LeaseID: "l-partial", Amount: 300}); !domain.IsValidation(err) {
		t.Fatalf("caução parcialmente devolvida: veio %v", err)
	}
	// Nenhuma retenção pode ter vazado para a caixa nos casos recusados.
	for _, lease := range env.leases.items {
		if lease.DepositAmount != nil {
			t.Fatalf("caução gravada em caso recusado: %+v", lease)
		}
	}
}

func TestCseaSetLeaseDepositFailurePaths(t *testing.T) {
	ctx := context.Background()

	// O dublê devolve ponteiro para a própria caixa, então cada caso monta o seu
	// ambiente para não herdar mutação do caso anterior.
	newEnv := func(t *testing.T) *covEnv {
		t.Helper()
		env := newCovEnv(t)
		env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusSeason, "2026-09-01", "2026-09-30", 2500)}
		return env
	}

	t.Run("falha de leitura", func(t *testing.T) {
		env := newEnv(t)
		env.leases.byIDErr = errors.New("banco fora")
		if _, err := SetLeaseDeposit(ctx, env.deps, covCtx(), DepositInput{LeaseID: "l1", Amount: 100}); err == nil || domain.IsValidation(err) {
			t.Fatalf("falha de leitura deveria sair como erro técnico, veio %v", err)
		}
	})

	t.Run("falha ao gravar", func(t *testing.T) {
		env := newEnv(t)
		env.leases.updateErr = errors.New("disco cheio")
		if _, err := SetLeaseDeposit(ctx, env.deps, covCtx(), DepositInput{LeaseID: "l1", Amount: 100}); err == nil || domain.IsValidation(err) {
			t.Fatalf("falha ao gravar caução deveria sair como erro técnico, veio %v", err)
		}
	})

	t.Run("falha de auditoria", func(t *testing.T) {
		env := newEnv(t)
		env.audit.insertErr = errors.New("trilha fora")
		if _, err := SetLeaseDeposit(ctx, env.deps, covCtx(), DepositInput{LeaseID: "l1", Amount: 100}); err == nil || domain.IsValidation(err) {
			t.Fatalf("falha de auditoria deveria sair como erro técnico, veio %v", err)
		}
	})
}

func TestCseaRefundDepositOutcomes(t *testing.T) {
	cases := []struct {
		name       string
		requested  float64
		notes      string
		wantStatus domain.DepositStatus
		wantAmount float64
		wantLedger bool
	}{
		{"integral", 1200, "", domain.DepositStatusRefunded, 1200, true},
		{"parcial", 700, "descontado o conserto", domain.DepositStatusPartiallyRefunded, 700, true},
		{"retenção total", 0, "dano na saída", domain.DepositStatusForfeited, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newCovEnv(t)
			ctx := context.Background()
			lease := covLeaseOf("l1", "t1", "p1", domain.LeaseStatusSeason, "2026-09-01", "2026-09-30", 2500)
			lease.TenantName = "Carlos"
			lease.PropertyName = "Apto 101"
			env.leases.items = []domain.Lease{lease}
			if _, err := SetLeaseDeposit(ctx, env.deps, covCtx(), DepositInput{LeaseID: "l1", Amount: 1200}); err != nil {
				t.Fatalf("preparar caução: %v", err)
			}
			env.audit.entries = nil

			view, err := RefundDeposit(ctx, env.deps, covCtx(), "l1", tc.requested, tc.notes)
			if err != nil {
				t.Fatalf("devolver: %v", err)
			}
			if view.Status != tc.wantStatus || view.Refunded != tc.wantAmount {
				t.Fatalf("situação=%q devolvido=%v", view.Status, view.Refunded)
			}
			if view.RefundedAt == "" {
				t.Fatal("data da devolução não registrada")
			}
			if tc.wantAmount > 0 && view.Outstanding != 1200-tc.wantAmount {
				t.Fatalf("saldo retido = %v", view.Outstanding)
			}
			if tc.notes != "" && view.Notes != tc.notes {
				t.Fatalf("observação = %q", view.Notes)
			}

			ledger := env.receipts.items
			if tc.wantLedger {
				if len(ledger) != 1 {
					t.Fatalf("lançamentos = %d, esperado 1", len(ledger))
				}
				if ledger[0].Category != domain.ReceiptCategoryDepositRefund || ledger[0].Amount != tc.wantAmount {
					t.Fatalf("lançamento inesperado: %+v", ledger[0])
				}
				if ledger[0].PaidAmount == nil || *ledger[0].PaidAmount != tc.wantAmount {
					t.Fatalf("lançamento sem baixa: %+v", ledger[0])
				}
			} else if len(ledger) != 0 {
				t.Fatalf("retenção total gerou lançamento: %+v", ledger)
			}

			if len(env.audit.entries) != 1 || env.audit.entries[0].Action != domain.ActionDepositRefund {
				t.Fatalf("trilha inesperada: %+v", env.audit.entries)
			}
		})
	}
}

func TestCseaRefundDepositRejections(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	env.leases.items = []domain.Lease{
		covLeaseOf("l1", "t1", "p1", domain.LeaseStatusSeason, "2026-09-01", "2026-09-30", 2500),
		covLeaseOf("l-none", "t2", "p2", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 3200),
	}

	if _, err := RefundDeposit(ctx, env.deps, covCtx(), "ghost", 10, ""); !domain.IsValidation(err) {
		t.Fatalf("contrato inexistente: veio %v", err)
	}
	if _, err := RefundDeposit(ctx, env.deps, covCtx(), "l-none", 10, ""); !domain.IsValidation(err) {
		t.Fatalf("contrato sem caução retida: veio %v", err)
	}
	if _, err := SetLeaseDeposit(ctx, env.deps, covCtx(), DepositInput{LeaseID: "l1", Amount: 1200}); err != nil {
		t.Fatalf("preparar caução: %v", err)
	}
	if _, err := RefundDeposit(ctx, env.deps, covCtx(), "l1", -5, ""); !domain.IsValidation(err) {
		t.Fatalf("valor negativo: veio %v", err)
	}
	if _, err := RefundDeposit(ctx, env.deps, covCtx(), "l1", 1300, ""); !domain.IsValidation(err) {
		t.Fatalf("acima do retido: veio %v", err)
	}
	// Nenhuma devolução recusada pode ter alterado o contrato nem lançado recibo.
	if env.leases.items[0].DepositStatus != domain.DepositStatusHeld {
		t.Fatalf("situação alterada em caso recusado: %q", env.leases.items[0].DepositStatus)
	}
	if len(env.receipts.items) != 0 {
		t.Fatalf("recibo gerado em caso recusado: %+v", env.receipts.items)
	}
}

func TestCseaRefundDepositFailurePaths(t *testing.T) {
	ctx := context.Background()

	// Cada falha deixa o contrato em estado diferente, então cada caso monta o
	// seu próprio ambiente.
	newHeldEnv := func(t *testing.T) *covEnv {
		t.Helper()
		env := newCovEnv(t)
		env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusSeason, "2026-09-01", "2026-09-30", 2500)}
		if _, err := SetLeaseDeposit(ctx, env.deps, covCtx(), DepositInput{LeaseID: "l1", Amount: 1200}); err != nil {
			t.Fatalf("preparar caução: %v", err)
		}
		return env
	}

	t.Run("falha ao gravar", func(t *testing.T) {
		env := newHeldEnv(t)
		env.leases.updateErr = errors.New("disco cheio")
		if _, err := RefundDeposit(ctx, env.deps, covCtx(), "l1", 1200, ""); err == nil || domain.IsValidation(err) {
			t.Fatalf("falha ao gravar devolução deveria ser técnica: %v", err)
		}
	})

	t.Run("falha ao lançar", func(t *testing.T) {
		env := newHeldEnv(t)
		env.receipts.createErr = errors.New("razão fora")
		if _, err := RefundDeposit(ctx, env.deps, covCtx(), "l1", 1200, ""); err == nil || domain.IsValidation(err) {
			t.Fatalf("falha ao lançar devolução deveria ser técnica: %v", err)
		}
	})

	t.Run("falha de auditoria", func(t *testing.T) {
		env := newHeldEnv(t)
		env.audit.insertErr = errors.New("trilha fora")
		if _, err := RefundDeposit(ctx, env.deps, covCtx(), "l1", 1200, ""); err == nil || domain.IsValidation(err) {
			t.Fatalf("falha de auditoria deveria ser técnica: %v", err)
		}
	})

	t.Run("falha de leitura", func(t *testing.T) {
		env := newHeldEnv(t)
		env.leases.byIDErr = errors.New("banco fora")
		if _, err := RefundDeposit(ctx, env.deps, covCtx(), "l1", 1200, ""); err == nil || domain.IsValidation(err) {
			t.Fatalf("falha de leitura deveria ser técnica: %v", err)
		}
	})
}

func TestCseaLoadDepositAndView(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusSeason, "2026-09-01", "2026-09-30", 2500)}
	if _, err := SetLeaseDeposit(ctx, env.deps, covCtx(), DepositInput{LeaseID: "l1", Amount: 1500}); err != nil {
		t.Fatalf("preparar caução: %v", err)
	}

	view, err := LoadDeposit(ctx, env.deps, "l1")
	if err != nil {
		t.Fatalf("carregar caução: %v", err)
	}
	if view.Amount != 1500 || view.Status != domain.DepositStatusHeld || view.Outstanding != 1500 {
		t.Fatalf("visão inesperada: %+v", view)
	}
	if view.Lease == nil || view.Lease.ID != "l1" {
		t.Fatal("visão sem o contrato")
	}

	if _, err := LoadDeposit(ctx, env.deps, "ghost"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("contrato inexistente: veio %v", err)
	}
	env.leases.byIDErr = errors.New("banco fora")
	if _, err := LoadDeposit(ctx, env.deps, "l1"); err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("falha de leitura deveria ser técnica: %v", err)
	}
}

func TestCseaDepositViewOfNormalizes(t *testing.T) {
	// Contrato sem caução: situação cai para `none` e os textos ficam vazios.
	bare := &domain.Lease{ID: "l1", TenantName: "Ana", PropertyName: "Casa"}
	view := DepositViewOf(bare)
	if view.Status != domain.DepositStatusNone || view.Amount != 0 || view.Outstanding != 0 {
		t.Fatalf("contrato sem caução: %+v", view)
	}
	if view.ReceivedAt != "" || view.RefundedAt != "" || view.Notes != "" {
		t.Fatalf("textos deveriam ser vazios: %+v", view)
	}
	if view.TenantName != "Ana" || view.PropertyName != "Casa" {
		t.Fatalf("nomes não copiados: %+v", view)
	}

	// Devolução acima do retido não pode deixar saldo negativo na tela.
	over := &domain.Lease{
		ID: "l2", DepositStatus: domain.DepositStatusRefunded,
		DepositAmount:      domain.FloatPtr(100),
		DepositRefundedAmt: domain.FloatPtr(150),
		DepositReceivedAt:  domain.StrPtr("2026-09-01 10:00"),
		DepositRefundedAt:  domain.StrPtr("2026-09-30 10:00"),
		DepositNotes:       domain.StrPtr("ajuste"),
	}
	overView := DepositViewOf(over)
	if overView.Outstanding != 0 {
		t.Fatalf("saldo retido = %v, esperado 0", overView.Outstanding)
	}
	if overView.Refunded != 150 || overView.Status != domain.DepositStatusRefunded {
		t.Fatalf("visão inesperada: %+v", overView)
	}
	if overView.ReceivedAt != "2026-09-01 10:00" || overView.Notes != "ajuste" {
		t.Fatalf("campos textuais: %+v", overView)
	}

	// O snapshot de auditoria reflete ausência e presença de valor.
	if snapshot := depositSnapshot(nil); snapshot != nil {
		t.Fatalf("snapshot de contrato nulo = %v", snapshot)
	}
	empty := depositSnapshot(&domain.Lease{ID: "l3"})
	if empty["amount"] != "" || empty["refundedAmount"] != "" {
		t.Fatalf("snapshot sem valores = %v", empty)
	}
	full := depositSnapshot(over)
	if full["amount"] == "" || full["refundedAmount"] == "" || full["status"] != string(domain.DepositStatusRefunded) {
		t.Fatalf("snapshot com valores = %v", full)
	}
}

// ── Vistoria ───────────────────────────────────────────────────────

func TestCseaInspectionCreatePersists(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	lease := covLeaseOf("l1", "t1", "p1", domain.LeaseStatusSeason, "2026-09-01", "2026-09-30", 2500)
	lease.TenantName = "Carlos"
	lease.PropertyName = "Apto 101"
	env.leases.items = []domain.Lease{lease}

	created, err := InspectionCreate(ctx, env.deps, covCtx(), InspectionInput{
		LeaseID: "l1", Kind: domain.InspectionKindCheckIn, PerformedAt: "2026-09-01T14:00",
		Condition: domain.InspectionConditionGood, Notes: "  chuveiro com marca de uso  ",
		Items: []domain.InspectionItem{
			{Area: "Sala", Item: "Sofá", State: "bom"},
			{Area: "Cozinha", Item: "Geladeira", State: "bom"},
		},
		Photos: []domain.InspectionPhoto{{Name: "inspections/foto-1.jpg", Label: "sala.jpg"}},
	})
	if err != nil {
		t.Fatalf("criar vistoria: %v", err)
	}
	if created.ID == "" {
		t.Fatal("vistoria sem identificador")
	}
	if created.Notes == nil || *created.Notes != "chuveiro com marca de uso" {
		t.Fatalf("observação deveria vir aparada: %v", created.Notes)
	}
	if created.TenantName != "Carlos" || created.PropertyName != "Apto 101" {
		t.Fatalf("nomes do contrato não copiados: %+v", created)
	}
	if created.CreatedAt == "" || created.UpdatedAt == "" {
		t.Fatal("carimbos de tempo ausentes")
	}
	if len(env.inspections.items) != 1 || len(env.inspections.items[0].Items) != 2 {
		t.Fatalf("vistoria não persistida com itens: %+v", env.inspections.items)
	}
	if len(env.inspections.items[0].Photos) != 1 {
		t.Fatalf("fotos não persistidas: %+v", env.inspections.items[0].Photos)
	}
	if len(env.audit.entries) != 1 || env.audit.entries[0].Action != domain.ActionInspectionCreate {
		t.Fatalf("trilha inesperada: %+v", env.audit.entries)
	}
	if env.audit.entries[0].Changes == nil || *env.audit.entries[0].Changes == "" {
		t.Fatal("criação sem diff de campos")
	}

	// Observação em branco vira ausente, e a vistoria de saída usa outro rótulo.
	blank, err := InspectionCreate(ctx, env.deps, covCtx(), InspectionInput{
		LeaseID: "l1", Kind: domain.InspectionKindCheckOut, PerformedAt: "2026-09-30T11:00",
		Condition: domain.InspectionConditionBad, Notes: "   ",
	})
	if err != nil {
		t.Fatalf("criar vistoria de saída: %v", err)
	}
	if blank.Notes != nil {
		t.Fatalf("observação em branco deveria ser ausente: %v", blank.Notes)
	}
}

func TestCseaInspectionCreateRejections(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusSeason, "2026-09-01", "2026-09-30", 2500)}

	cases := map[string]InspectionInput{
		"contrato inexistente": {LeaseID: "ghost", Kind: domain.InspectionKindCheckIn, Condition: domain.InspectionConditionGood, PerformedAt: "2026-09-01"},
		"tipo inválido":        {LeaseID: "l1", Kind: "qualquer", Condition: domain.InspectionConditionGood, PerformedAt: "2026-09-01"},
		"estado inválido":      {LeaseID: "l1", Kind: domain.InspectionKindCheckIn, Condition: "perfeito", PerformedAt: "2026-09-01"},
		"data ausente":         {LeaseID: "l1", Kind: domain.InspectionKindCheckIn, Condition: domain.InspectionConditionGood},
	}
	for name, input := range cases {
		if _, err := InspectionCreate(ctx, env.deps, covCtx(), input); !domain.IsValidation(err) {
			t.Errorf("%s: esperava validação, veio %v", name, err)
		}
	}
	if len(env.inspections.items) != 0 {
		t.Fatalf("vistoria gravada em caso recusado: %+v", env.inspections.items)
	}
}

func TestCseaInspectionCreateFailurePaths(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusSeason, "2026-09-01", "2026-09-30", 2500)}
	input := InspectionInput{LeaseID: "l1", Kind: domain.InspectionKindCheckIn, Condition: domain.InspectionConditionGood, PerformedAt: "2026-09-01"}

	env.leases.byIDErr = errors.New("banco fora")
	if _, err := InspectionCreate(ctx, env.deps, covCtx(), input); err == nil || domain.IsValidation(err) {
		t.Fatalf("falha de leitura deveria ser técnica: %v", err)
	}
	env.leases.byIDErr = nil

	env.inspections.createErr = errors.New("disco cheio")
	if _, err := InspectionCreate(ctx, env.deps, covCtx(), input); err == nil || domain.IsValidation(err) {
		t.Fatalf("falha ao gravar deveria ser técnica: %v", err)
	}
	env.inspections.createErr = nil

	env.audit.insertErr = errors.New("trilha fora")
	if _, err := InspectionCreate(ctx, env.deps, covCtx(), input); err == nil || domain.IsValidation(err) {
		t.Fatalf("falha de auditoria deveria ser técnica: %v", err)
	}
}

func TestCseaInspectionUpdateKeepsOrReplacesPhotos(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	cseaSeedInspection(env, domain.Inspection{
		ID: "i1", LeaseID: "l1", Kind: domain.InspectionKindCheckOut, PerformedAt: "2026-09-30T11:00",
		Condition: domain.InspectionConditionFair, Notes: domain.StrPtr("antiga"),
		Photos: []domain.InspectionPhoto{{Name: "inspections/foto-2.jpg"}},
	})

	// Edição sem upload novo preserva as fotos já anexadas.
	updated, err := InspectionUpdate(ctx, env.deps, covCtx(), InspectionInput{
		ID: "i1", Kind: domain.InspectionKindCheckOut, PerformedAt: "2026-09-30T12:00",
		Condition: domain.InspectionConditionBad, Notes: "porta danificada",
	})
	if err != nil {
		t.Fatalf("alterar vistoria: %v", err)
	}
	if updated.Condition != domain.InspectionConditionBad {
		t.Fatalf("estado geral = %q", updated.Condition)
	}
	if len(updated.Photos) != 1 || updated.Photos[0].Name != "inspections/foto-2.jpg" {
		t.Fatalf("edição sem upload deve preservar as fotos: %+v", updated.Photos)
	}
	if env.inspections.items[0].Condition != domain.InspectionConditionBad {
		t.Fatal("alteração não persistida")
	}
	var updateEntry *domain.AuditLog
	for index := range env.audit.entries {
		if env.audit.entries[index].Action == domain.ActionInspectionUpdate {
			updateEntry = &env.audit.entries[index]
		}
	}
	if updateEntry == nil || updateEntry.Changes == nil || *updateEntry.Changes == "" {
		t.Fatal("alteração sem diff na trilha")
	}

	// Upload novo substitui a lista inteira de fotos.
	replaced, err := InspectionUpdate(ctx, env.deps, covCtx(), InspectionInput{
		ID: "i1", Kind: domain.InspectionKindCheckIn, PerformedAt: "2026-10-01T09:00",
		Condition: domain.InspectionConditionGood,
		Photos:    []domain.InspectionPhoto{{Name: "inspections/foto-3.jpg"}, {Name: "inspections/foto-4.jpg"}},
	})
	if err != nil {
		t.Fatalf("alterar com upload: %v", err)
	}
	if len(replaced.Photos) != 2 || replaced.Photos[0].Name != "inspections/foto-3.jpg" {
		t.Fatalf("fotos não substituídas: %+v", replaced.Photos)
	}
	if replaced.Notes != nil {
		t.Fatalf("observação vazia deveria ser ausente: %v", replaced.Notes)
	}
	if replaced.UpdatedAt == "" {
		t.Fatal("carimbo de edição ausente")
	}
}

func TestCseaInspectionUpdateRejectionsAndFailures(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	cseaSeedInspection(env, domain.Inspection{ID: "i1", LeaseID: "l1", Kind: domain.InspectionKindCheckIn, PerformedAt: "2026-09-01T14:00", Condition: domain.InspectionConditionGood})

	if _, err := InspectionUpdate(ctx, env.deps, covCtx(), InspectionInput{ID: "ghost", Kind: domain.InspectionKindCheckIn, Condition: domain.InspectionConditionGood, PerformedAt: "2026-09-01"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("vistoria inexistente: veio %v", err)
	}
	if _, err := InspectionUpdate(ctx, env.deps, covCtx(), InspectionInput{ID: "i1", Kind: "tipo", Condition: domain.InspectionConditionGood, PerformedAt: "2026-09-01"}); !domain.IsValidation(err) {
		t.Fatalf("campo inválido: veio %v", err)
	}
	if _, err := InspectionUpdate(ctx, env.deps, covCtx(), InspectionInput{ID: "i1", Kind: domain.InspectionKindCheckIn, Condition: domain.InspectionConditionGood}); !domain.IsValidation(err) {
		t.Fatalf("data ausente: veio %v", err)
	}

	env.inspections.byIDErr = errors.New("banco fora")
	if _, err := InspectionUpdate(ctx, env.deps, covCtx(), InspectionInput{ID: "i1", Kind: domain.InspectionKindCheckIn, Condition: domain.InspectionConditionGood, PerformedAt: "2026-09-01"}); err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("falha de leitura deveria ser técnica: %v", err)
	}
	env.inspections.byIDErr = nil

	env.inspections.updateErr = errors.New("disco cheio")
	if _, err := InspectionUpdate(ctx, env.deps, covCtx(), InspectionInput{ID: "i1", Kind: domain.InspectionKindCheckIn, Condition: domain.InspectionConditionGood, PerformedAt: "2026-09-01"}); err == nil || domain.IsValidation(err) {
		t.Fatalf("falha ao gravar deveria ser técnica: %v", err)
	}
	env.inspections.updateErr = nil

	env.audit.insertErr = errors.New("trilha fora")
	if _, err := InspectionUpdate(ctx, env.deps, covCtx(), InspectionInput{ID: "i1", Kind: domain.InspectionKindCheckIn, Condition: domain.InspectionConditionBad, PerformedAt: "2026-09-02"}); err == nil || domain.IsValidation(err) {
		t.Fatalf("falha de auditoria deveria ser técnica: %v", err)
	}
}

func TestCseaInspectionDeleteRemovesPhotos(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	if _, err := env.attachments.Save("inspections", "foto-2.jpg", []byte("x")); err != nil {
		t.Fatalf("preparar foto: %v", err)
	}
	cseaSeedInspection(env, domain.Inspection{
		ID: "i1", LeaseID: "l1", Kind: domain.InspectionKindCheckOut, PerformedAt: "2026-09-30T11:00",
		Condition: domain.InspectionConditionBad,
		Photos:    []domain.InspectionPhoto{{Name: "inspections-foto-2.jpg"}},
	})

	if err := InspectionDelete(ctx, env.deps, covCtx(), "i1"); err != nil {
		t.Fatalf("excluir vistoria: %v", err)
	}
	if len(env.inspections.items) != 0 {
		t.Fatal("vistoria não removida da caixa")
	}
	if _, ok := env.attachments.files["inspections-foto-2.jpg"]; ok {
		t.Fatal("foto não removida do armazenamento")
	}
	if len(env.audit.entries) != 1 || env.audit.entries[0].Action != domain.ActionInspectionDelete {
		t.Fatalf("trilha inesperada: %+v", env.audit.entries)
	}
	if env.audit.entries[0].Changes == nil || *env.audit.entries[0].Changes == "" {
		t.Fatal("exclusão sem diff de campos")
	}

	// Sem armazenamento configurado a exclusão não pode quebrar.
	env.deps.Attachments = nil
	cseaSeedInspection(env, domain.Inspection{ID: "i2", LeaseID: "l1", Kind: domain.InspectionKindCheckIn, PerformedAt: "2026-09-01T14:00", Condition: domain.InspectionConditionGood})
	if err := InspectionDelete(ctx, env.deps, covCtx(), "i2"); err != nil {
		t.Fatalf("excluir sem armazenamento: %v", err)
	}
}

func TestCseaInspectionDeleteFailurePaths(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	cseaSeedInspection(env, domain.Inspection{
		ID: "i1", LeaseID: "l1", Kind: domain.InspectionKindCheckOut, PerformedAt: "2026-09-30T11:00",
		Condition: domain.InspectionConditionBad,
		Photos:    []domain.InspectionPhoto{{Name: "inspections/foto-2.jpg"}},
	})

	if err := InspectionDelete(ctx, env.deps, covCtx(), "ghost"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("vistoria inexistente: veio %v", err)
	}

	env.inspections.byIDErr = errors.New("banco fora")
	if err := InspectionDelete(ctx, env.deps, covCtx(), "i1"); err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("falha de leitura deveria ser técnica: %v", err)
	}
	env.inspections.byIDErr = nil

	env.inspections.delErr = errors.New("disco cheio")
	if err := InspectionDelete(ctx, env.deps, covCtx(), "i1"); err == nil {
		t.Fatal("falha ao excluir deveria sair como erro")
	}
	env.inspections.delErr = nil

	// Falha ao remover uma foto apenas avisa: a vistoria sai e a trilha fica.
	env.attachments.delErr = errors.New("arquivo travado")
	if err := InspectionDelete(ctx, env.deps, covCtx(), "i1"); err != nil {
		t.Fatalf("falha de foto não pode derrubar a exclusão: %v", err)
	}
	if len(env.inspections.items) != 0 {
		t.Fatal("vistoria deveria ter sido excluída mesmo com foto travada")
	}
}

func TestCseaLoadInspectionAndView(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	env.leases.items = []domain.Lease{covLeaseOf("l1", "t1", "p1", domain.LeaseStatusSeason, "2026-09-01", "2026-09-30", 2500)}
	cseaSeedInspection(env, domain.Inspection{
		ID: "i1", LeaseID: "l1", Kind: domain.InspectionKindCheckIn, PerformedAt: "2026-09-01T14:00",
		Condition: domain.InspectionConditionGood,
	})

	view, err := LoadInspection(ctx, env.deps, "i1")
	if err != nil {
		t.Fatalf("carregar vistoria: %v", err)
	}
	if view.Inspection == nil || view.Inspection.ID != "i1" {
		t.Fatalf("visão sem a vistoria: %+v", view)
	}
	if view.Lease == nil || view.Lease.ID != "l1" {
		t.Fatal("visão sem o contrato associado")
	}
	if view.KindLabel != domain.InspectionKindLabel(domain.InspectionKindCheckIn) || view.Condition != domain.InspectionConditionLabel(domain.InspectionConditionGood) {
		t.Fatalf("rótulos inesperados: %+v", view)
	}

	if _, err := LoadInspection(ctx, env.deps, "ghost"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("vistoria inexistente: veio %v", err)
	}
	env.inspections.byIDErr = errors.New("banco fora")
	if _, err := LoadInspection(ctx, env.deps, "i1"); err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("falha de leitura deveria ser técnica: %v", err)
	}
}

func TestCseaInspectionViewOfWithoutLease(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	orphan := &domain.Inspection{ID: "i1", LeaseID: "ghost", Kind: domain.InspectionKindCheckOut, Condition: domain.InspectionConditionFair}

	view, err := InspectionViewOf(ctx, env.deps, orphan)
	if err != nil {
		t.Fatalf("contrato ausente não pode ser erro: %v", err)
	}
	if view.Lease != nil {
		t.Fatalf("visão trouxe contrato inexistente: %+v", view.Lease)
	}
	if view.Inspection != orphan || view.KindLabel != "Vistoria de saída" || view.Condition != "Regular" {
		t.Fatalf("visão inesperada: %+v", view)
	}

	env.leases.byIDErr = errors.New("banco fora")
	if _, err := InspectionViewOf(ctx, env.deps, orphan); err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("falha de leitura deveria ser técnica: %v", err)
	}
}

func TestCseaListInspectionsAndByLease(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	cseaSeedInspection(env, domain.Inspection{ID: "i1", LeaseID: "l1", Kind: domain.InspectionKindCheckIn})
	cseaSeedInspection(env, domain.Inspection{ID: "i2", LeaseID: "l2", Kind: domain.InspectionKindCheckOut})

	all, err := ListInspections(ctx, env.deps)
	if err != nil || len(all) != 2 {
		t.Fatalf("listar todas: %v (%d)", err, len(all))
	}
	byLease, err := ListInspectionsByLease(ctx, env.deps, "l1")
	if err != nil || len(byLease) != 1 || byLease[0].ID != "i1" {
		t.Fatalf("listar do contrato: %v (%+v)", err, byLease)
	}

	env.inspections.listErr = errors.New("banco fora")
	if _, err := ListInspections(ctx, env.deps); err == nil {
		t.Fatal("falha ao listar todas deveria sair como erro")
	}
	env.inspections.listErr = nil

	env.inspections.byLeaseErr = errors.New("banco fora")
	if _, err := ListInspectionsByLease(ctx, env.deps, "l1"); err == nil {
		t.Fatal("falha ao listar do contrato deveria sair como erro")
	}
}

func TestCseaInspectionSnapshotAndForLease(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	cseaSeedInspection(env, domain.Inspection{
		ID: "i1", LeaseID: "l1", Kind: domain.InspectionKindCheckIn, PerformedAt: "2026-09-01T14:00",
		Condition: domain.InspectionConditionGood, Notes: domain.StrPtr("marca de uso"),
		TenantName: "Carlos", PropertyName: "Apto 101",
	})
	cseaSeedInspection(env, domain.Inspection{ID: "i2", LeaseID: "l1", Kind: domain.InspectionKindCheckOut, PerformedAt: "2026-09-30T11:00", Condition: domain.InspectionConditionBad})

	if snapshot := InspectionSnapshot(nil); snapshot != nil {
		t.Fatalf("snapshot de vistoria nula = %v", snapshot)
	}
	snapshot := InspectionSnapshot(&env.inspections.items[0])
	if snapshot["kind"] != string(domain.InspectionKindCheckIn) || snapshot["condition"] != string(domain.InspectionConditionGood) {
		t.Fatalf("snapshot inesperado: %v", snapshot)
	}
	if snapshot["tenant_name"] != "Carlos" || snapshot["property_name"] != "Apto 101" || snapshot["notes"] != "marca de uso" {
		t.Fatalf("snapshot sem nomes/observação: %v", snapshot)
	}

	checkIn, err := InspectionForLease(ctx, env.deps, "l1", domain.InspectionKindCheckIn)
	if err != nil || checkIn.ID != "i1" {
		t.Fatalf("vistoria de entrada: %v (%+v)", err, checkIn)
	}
	checkOut, err := InspectionForLease(ctx, env.deps, "l1", domain.InspectionKindCheckOut)
	if err != nil || checkOut.ID != "i2" {
		t.Fatalf("vistoria de saída: %v (%+v)", err, checkOut)
	}
	if _, err := InspectionForLease(ctx, env.deps, "l2", domain.InspectionKindCheckIn); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("contrato sem vistoria: veio %v", err)
	}

	env.inspections.byLeaseErr = errors.New("banco fora")
	if _, err := InspectionForLease(ctx, env.deps, "l1", domain.InspectionKindCheckIn); err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("falha de leitura deveria ser técnica: %v", err)
	}
}

func TestCseaIntakeStatusTone(t *testing.T) {
	if tone := intakeStatusTone(domain.IntakeStatusConfirmed); tone != "paid" {
		t.Fatalf("confirmado = %q", tone)
	}
	if tone := intakeStatusTone(domain.IntakeStatusDiscarded); tone != "terminated" {
		t.Fatalf("descartado = %q", tone)
	}
	if tone := intakeStatusTone(domain.IntakeStatusPending); tone != "due" {
		t.Fatalf("pendente = %q", tone)
	}
}

// ── Calendário ─────────────────────────────────────────────────────

func TestCseaBuildCalendarMonthShapes(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	env.props.items = []domain.Property{covPropertyOf("p1", "Apto 101")}

	cases := []struct {
		name  string
		year  int
		month int
		days  int
		label string
	}{
		{"fevereiro bissexto", 2028, 2, 29, "fevereiro de 2028"},
		{"fevereiro comum", 2026, 2, 28, "fevereiro de 2026"},
		{"mês de 31 dias", 2026, 7, 31, "julho de 2026"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calendar, err := BuildCalendar(ctx, env.deps, tc.year, tc.month)
			if err != nil {
				t.Fatalf("montar calendário: %v", err)
			}
			if calendar.Month.Days != tc.days || calendar.Month.Label != tc.label {
				t.Fatalf("mês inesperado: %+v", calendar.Month)
			}
			if len(calendar.Properties) != 1 || len(calendar.Properties[0].Days) != tc.days {
				t.Fatalf("grade de dias inesperada: %+v", calendar.Properties)
			}
		})
	}
}

func TestCseaBuildCalendarOccupancyAndNavigation(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	env.props.items = []domain.Property{covPropertyOf("p1", "Apto 101"), covPropertyOf("p2", "Casa das Dunas")}
	inside := covLeaseOf("l-in", "t1", "p1", domain.LeaseStatusSeason, "2026-09-05", "2026-09-08", 2500)
	inside.TenantName = "Carlos"
	spanning := covLeaseOf("l-span", "t2", "p2", domain.LeaseStatusActive, "2026-08-15", "2026-10-20", 3200)
	spanning.TenantName = "Clarice"
	suspended := covLeaseOf("l-susp", "t3", "p2", domain.LeaseStatusSuspended, "2026-09-10", "2026-09-20", 1800)
	noOverlap := covLeaseOf("l-out", "t4", "p1", domain.LeaseStatusSeason, "2026-11-01", "2026-11-10", 2000)
	terminated := covLeaseOf("l-term", "t5", "p1", domain.LeaseStatusTerminated, "2026-09-01", "2026-09-30", 1500)
	env.leases.items = []domain.Lease{inside, spanning, suspended, noOverlap, terminated}

	calendar, err := BuildCalendar(ctx, env.deps, 2026, 9)
	if err != nil {
		t.Fatalf("montar calendário: %v", err)
	}
	if calendar.Month.Previous != "/dashboard?tab=calendar&year=2026&month=8" || calendar.Month.Next != "/dashboard?tab=calendar&year=2026&month=10" {
		t.Fatalf("navegação inesperada: %q / %q", calendar.Month.Previous, calendar.Month.Next)
	}
	if len(calendar.Month.Months) != 12 || len(calendar.Month.Years) == 0 {
		t.Fatalf("opções de mês/ano ausentes: %+v", calendar.Month)
	}
	if calendar.Summary.TotalProperties != 2 || calendar.Summary.TotalOccupiedDays != 34 {
		t.Fatalf("resumo de ocupação inesperado: %+v", calendar.Summary)
	}

	byID := map[string]CalendarProperty{}
	for _, property := range calendar.Properties {
		byID[property.PropertyID] = property
	}

	// p1: só o contrato de temporada que cabe no mês; encerrado e fora do mês
	// não aparecem.
	p1 := byID["p1"]
	if len(p1.Periods) != 1 || p1.Periods[0].LeaseID != "l-in" {
		t.Fatalf("períodos de p1 = %+v", p1.Periods)
	}
	if !p1.Periods[0].StartsHere || !p1.Periods[0].EndsHere {
		t.Fatalf("período contido deve começar e terminar no mês: %+v", p1.Periods[0])
	}
	if p1.OccupiedDays != 4 {
		t.Fatalf("dias ocupados de p1 = %d, esperado 4", p1.OccupiedDays)
	}
	if !p1.Days[4].Occupied() || p1.Days[4].TenantName != "Carlos" || !p1.Days[4].Starts {
		t.Fatalf("dia 5 deveria ser o início ocupado: %+v", p1.Days[4])
	}
	if p1.Days[7].Occupied() != true || !p1.Days[7].Ends {
		t.Fatalf("dia 8 deveria ser o fim: %+v", p1.Days[7])
	}
	if p1.Days[8].Occupied() {
		t.Fatalf("dia 9 deveria estar livre: %+v", p1.Days[8])
	}

	// p2: contrato que atravessa o mês não começa nem termina aqui, e o
	// suspenso aparece como período mas não conta como ocupação.
	p2 := byID["p2"]
	if len(p2.Periods) != 2 {
		t.Fatalf("períodos de p2 = %+v", p2.Periods)
	}
	if p2.Periods[0].StartsHere || p2.Periods[0].EndsHere {
		t.Fatalf("período que atravessa o mês: %+v", p2.Periods[0])
	}
	if p2.OccupiedDays != 30 {
		t.Fatalf("dias ocupados de p2 = %d, esperado 30", p2.OccupiedDays)
	}
}

func TestCseaBuildCalendarFallbacksAndErrors(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()

	// Mês fora da faixa: cai no mês do relógio fixo (setembro/2026).
	fallback, err := BuildCalendar(ctx, env.deps, 2026, 99)
	if err != nil {
		t.Fatalf("fallback de mês: %v", err)
	}
	if fallback.Month.Year != 2026 || fallback.Month.Month != 9 {
		t.Fatalf("esperava setembro de 2026, veio %+v", fallback.Month)
	}

	// Ano fora da faixa: mantém o mês pedido e cai no ano do relógio.
	yearFallback, err := BuildCalendar(ctx, env.deps, 1900, 6)
	if err != nil {
		t.Fatalf("fallback de ano: %v", err)
	}
	if yearFallback.Month.Year != 2026 || yearFallback.Month.Month != 6 {
		t.Fatalf("esperava junho de 2026, veio %+v", yearFallback.Month)
	}

	env.props.listErr = errors.New("imóveis fora")
	if _, err := BuildCalendar(ctx, env.deps, 2026, 9); err == nil {
		t.Fatal("falha ao listar imóveis deveria sair como erro")
	}
	env.props.listErr = nil

	env.leases.listErr = errors.New("contratos fora")
	if _, err := BuildCalendar(ctx, env.deps, 2026, 9); err == nil {
		t.Fatal("falha ao listar contratos deveria sair como erro")
	}
}

func TestCseaCalendarDaysAndOverlap(t *testing.T) {
	// Grade: o primeiro período que bloqueia sobreposição prevalece; um período
	// suspenso posterior não sobrescreve o ocupado.
	periods := []CalendarPeriod{
		cseaBlockingPeriod("l-active", "2026-09-05", "2026-09-10", domain.LeaseStatusActive, "Ana"),
		cseaBlockingPeriod("l-susp", "2026-09-08", "2026-09-12", domain.LeaseStatusSuspended, "Bia"),
	}
	days := buildCalendarDays(periods, 2026, 9, 30)
	if len(days) != 30 {
		t.Fatalf("dias = %d", len(days))
	}
	if days[4].Status != domain.LeaseStatusActive || days[4].TenantName != "Ana" || !days[4].Starts {
		t.Fatalf("dia 5 = %+v", days[4])
	}
	// Dia 9 está dentro dos dois períodos; o ativo (bloqueante) vence.
	if days[8].Status != domain.LeaseStatusActive {
		t.Fatalf("dia 9 deveria seguir com o ativo: %+v", days[8])
	}
	if days[9].Status != domain.LeaseStatusActive || !days[9].Ends {
		t.Fatalf("dia 10 deveria encerrar o ativo: %+v", days[9])
	}
	// Dias 11 e 12 só têm o suspenso.
	if days[10].Status != domain.LeaseStatusSuspended || days[10].TenantName != "Bia" {
		t.Fatalf("dia 11 = %+v", days[10])
	}
	// O período suspenso não "gruda" nas bordas do mês.
	if days[11].Starts {
		t.Fatalf("dia 12 não deveria marcar início: %+v", days[11])
	}
	if days[0].Status != "" || days[0].Occupied() {
		t.Fatalf("dia 1 deveria estar livre: %+v", days[0])
	}

	// Ordem inversa: o período bloqueante posterior sobrescreve o suspenso.
	reversed := buildCalendarDays([]CalendarPeriod{
		cseaBlockingPeriod("l-susp", "2026-09-08", "2026-09-12", domain.LeaseStatusSuspended, "Bia"),
		cseaBlockingPeriod("l-active", "2026-09-05", "2026-09-10", domain.LeaseStatusActive, "Ana"),
	}, 2026, 9, 30)
	if reversed[8].Status != domain.LeaseStatusActive || reversed[8].TenantName != "Ana" {
		t.Fatalf("dia 9 deveria ser do ativo: %+v", reversed[8])
	}

	// Período que começa no dia 1 e passa do mês: o dia 1 é o início do período,
	// mas o dia 30 NÃO é o fim (o contrato segue em outubro).
	full := buildCalendarDays([]CalendarPeriod{
		cseaBlockingPeriod("l-full", "2026-09-01", "2026-10-10", domain.LeaseStatusSeason, "Caio"),
	}, 2026, 9, 30)
	if !full[0].Starts || full[0].Ends {
		t.Fatalf("dia 1 deveria marcar início e não fim: %+v", full[0])
	}
	if full[29].Ends || full[29].Starts {
		t.Fatalf("dia 30 não é borda de um período que segue em outubro: %+v", full[29])
	}

	monthStart := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	monthEnd := time.Date(2026, 9, 30, 0, 0, 0, 0, time.Local)
	if got := overlapDays("2026-09-05", "2026-09-08", monthStart, monthEnd); got != 4 {
		t.Fatalf("período contido = %d, esperado 4", got)
	}
	if got := overlapDays("2026-08-20", "2026-09-05", monthStart, monthEnd); got != 5 {
		t.Fatalf("início antes do mês = %d, esperado 5", got)
	}
	if got := overlapDays("2026-09-25", "2026-10-10", monthStart, monthEnd); got != 6 {
		t.Fatalf("fim depois do mês = %d, esperado 6", got)
	}
	if got := overlapDays("2026-10-20", "2026-10-05", monthStart, monthEnd); got != 0 {
		t.Fatalf("período invertido = %d, esperado 0", got)
	}
}

// TestCseaBuildCalendarDaysAcrossMonthBoundary fixa o comportamento correto da
// grade: o dia é montado no mês EXIBIDO, então um contrato que começou em agosto
// e cobre setembro inteiro pinta os 30 dias de setembro (antes a grade usava o
// mês de início do período e deixava os primeiros dias livres).
func TestCseaBuildCalendarDaysAcrossMonthBoundary(t *testing.T) {
	days := buildCalendarDays([]CalendarPeriod{
		cseaBlockingPeriod("l-span", "2026-08-15", "2026-10-20", domain.LeaseStatusSeason, "Clarice"),
	}, 2026, 9, 30)

	if len(days) != 30 {
		t.Fatalf("grade com %d dias, esperado 30", len(days))
	}
	for _, day := range days {
		if !day.Occupied() || day.TenantName != "Clarice" {
			t.Fatalf("dia %d deveria estar ocupado pelo período que atravessa o mês: %+v", day.Day, day)
		}
	}
	if days[0].Starts {
		t.Fatalf("o período começou antes do mês exibido: o dia 1 não é o início: %+v", days[0])
	}
	if days[29].Ends {
		t.Fatalf("o período termina depois do mês exibido: o dia 30 não é o fim: %+v", days[29])
	}

	// Período que começa e termina dentro do mês marca início e fim nos dias certos.
	inner := buildCalendarDays([]CalendarPeriod{
		cseaBlockingPeriod("l-inner", "2026-09-05", "2026-09-10", domain.LeaseStatusActive, "Carlos"),
	}, 2026, 9, 30)
	if inner[3].Occupied() {
		t.Fatalf("dia 4 fica livre antes do início do período: %+v", inner[3])
	}
	if !inner[4].Occupied() {
		t.Fatalf("dia 5 é o início do período e deve estar ocupado: %+v", inner[4])
	}
	if !inner[4].Starts || !inner[9].Ends {
		t.Fatalf("início/fim do período dentro do mês: dia 5 começa=%v, dia 10 termina=%v", inner[4].Starts, inner[9].Ends)
	}
	if inner[10].Occupied() {
		t.Fatalf("dia 11 deveria ficar livre depois do fim do período")
	}
}

func TestCseaMonthNavigation(t *testing.T) {
	if year, month := previousMonth(2026, 1); year != 2025 || month != 12 {
		t.Fatalf("janeiro volta para %d/%d", year, month)
	}
	if year, month := previousMonth(2026, 9); year != 2026 || month != 8 {
		t.Fatalf("setembro volta para %d/%d", year, month)
	}
	if year, month := nextMonth(2026, 12); year != 2027 || month != 1 {
		t.Fatalf("dezembro avança para %d/%d", year, month)
	}
	if year, month := nextMonth(2026, 9); year != 2026 || month != 10 {
		t.Fatalf("setembro avança para %d/%d", year, month)
	}
}

// ── Bancada de temporada ───────────────────────────────────────────

func TestCseaBuildSeasonOverviewGroupsDepositsAndCheckouts(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	season := covLeaseOf("l1", "t1", "p1", domain.LeaseStatusSeason, "2026-09-01", "2026-09-30", 2500)
	active := covLeaseOf("l2", "t2", "p2", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 3200)
	terminated := covLeaseOf("l3", "t3", "p3", domain.LeaseStatusTerminated, "2025-01-01", "2025-12-31", 1800)
	// Temporada sem nenhuma vistoria: a saída fica pendente.
	pending := covLeaseOf("l4", "t4", "p4", domain.LeaseStatusSeason, "2026-10-01", "2026-10-31", 2100)
	env.leases.items = []domain.Lease{season, active, terminated, pending}

	lease := env.leases.items[0]
	lease.DepositAmount = domain.FloatPtr(1200)
	lease.DepositStatus = domain.DepositStatusHeld
	lease.DepositRefundedAmt = domain.FloatPtr(700)
	env.leases.items[0] = lease
	cseaSeedInspection(env, domain.Inspection{ID: "i-in", LeaseID: "l1", Kind: domain.InspectionKindCheckIn})
	cseaSeedInspection(env, domain.Inspection{ID: "i-out", LeaseID: "l1", Kind: domain.InspectionKindCheckOut})
	// Contrato ativo com caução e sem vistoria de saída.
	env.leases.items[1].DepositAmount = domain.FloatPtr(500)
	env.leases.items[1].DepositStatus = domain.DepositStatusHeld
	cseaSeedInspection(env, domain.Inspection{ID: "i2-in", LeaseID: "l2", Kind: domain.InspectionKindCheckIn})

	overview, err := BuildSeasonOverview(ctx, env.deps)
	if err != nil {
		t.Fatalf("montar bancada: %v", err)
	}
	if len(overview.Rows) != 3 {
		t.Fatalf("linhas = %d, esperado 3 (encerrado fora)", len(overview.Rows))
	}
	if overview.SeasonCount != 2 || overview.PendingCheckOut != 1 {
		t.Fatalf("temporada=%d saídas pendentes=%d", overview.SeasonCount, overview.PendingCheckOut)
	}
	if overview.WithDeposit != 2 || overview.DepositsHeld != 500+500 || overview.DepositsReturned != 700 {
		t.Fatalf("cauções: %d retidas %v devolvidas %v", overview.WithDeposit, overview.DepositsHeld, overview.DepositsReturned)
	}
	if overview.Rows[0].CheckIn == nil || overview.Rows[0].CheckOut == nil || !overview.Rows[0].IsSeason {
		t.Fatalf("linha de temporada incompleta: %+v", overview.Rows[0])
	}
	if overview.Rows[0].Deposit.Outstanding != 500 {
		t.Fatalf("saldo retido = %v", overview.Rows[0].Deposit.Outstanding)
	}
	if overview.Rows[1].IsSeason {
		t.Fatalf("contrato ativo marcado como temporada: %+v", overview.Rows[1])
	}
	// A temporada sem vistoria aparece como pendente de saída.
	if !overview.Rows[2].IsSeason || overview.Rows[2].CheckOut != nil || overview.Rows[2].CheckIn != nil {
		t.Fatalf("linha pendente inesperada: %+v", overview.Rows[2])
	}

	// Falha ao listar vistorias derruba a bancada inteira.
	env.inspections.listErr = errors.New("vistorias fora")
	if _, err := BuildSeasonOverview(ctx, env.deps); err == nil {
		t.Fatal("falha ao listar vistorias deveria sair como erro")
	}
	env.inspections.listErr = nil

	env.leases.listErr = errors.New("contratos fora")
	if _, err := BuildSeasonOverview(ctx, env.deps); err == nil {
		t.Fatal("falha ao listar contratos deveria sair como erro")
	}
}

// TestCseaPendingCheckoutCountsEndedLeases (Work 0007, task 7.6): a saída
// pendente conta a temporada sem vistoria de saída e o fixo com prazo
// encerrado sem vistoria de saída, sem contar o fixo ainda vigente.
//
// O "hoje" do ambiente é 21/09/2026.
func TestCseaPendingCheckoutCountsEndedLeases(t *testing.T) {
	env := newCovEnv(t)
	ctx := context.Background()
	env.leases.items = []domain.Lease{
		covLeaseOf("s1", "t1", "p1", domain.LeaseStatusSeason, "2026-09-01", "2026-09-30", 2500),
		covLeaseOf("a1", "t2", "p2", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 3200),
		covLeaseOf("e1", "t3", "p3", domain.LeaseStatusActive, "2026-01-01", "2026-08-31", 1800),
		covLeaseOf("e2", "t4", "p4", domain.LeaseStatusSuspended, "2026-01-01", "2026-07-31", 1500),
	}
	cseaSeedInspection(env, domain.Inspection{ID: "i-a1", LeaseID: "a1", Kind: domain.InspectionKindCheckIn})
	cseaSeedInspection(env, domain.Inspection{ID: "i-e1", LeaseID: "e1", Kind: domain.InspectionKindCheckIn})
	cseaSeedInspection(env, domain.Inspection{ID: "i-e2-in", LeaseID: "e2", Kind: domain.InspectionKindCheckIn})
	cseaSeedInspection(env, domain.Inspection{ID: "i-e2-out", LeaseID: "e2", Kind: domain.InspectionKindCheckOut})

	overview, err := BuildSeasonOverview(ctx, env.deps)
	if err != nil {
		t.Fatalf("montar bancada: %v", err)
	}
	// s1 (temporada sem saída) + e1 (fixo com prazo encerrado sem saída).
	// a1 segue vigente e e2 já tem saída: nenhum dos dois conta.
	if overview.PendingCheckOut != 2 {
		t.Fatalf("saídas pendentes = %d, esperadas 2 (temporada + fixo encerrado)", overview.PendingCheckOut)
	}
}
