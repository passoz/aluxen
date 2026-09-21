package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Documentos da temporada: o auto de vistoria e o recibo de devolução de caução
// resolvem os mesmos dados de apresentação dos demais documentos (partes,
// imóvel, chave de autenticidade) e decidem o acesso pela titularidade do
// contrato.

func TestInspectionDocumentInputResolvesTheAuto(t *testing.T) {
	fixture := newDocumentFixture()

	data, err := application.InspectionDocumentInput(context.Background(), fixture.deps, docInspectionID)
	if err != nil {
		t.Fatalf("InspectionDocumentInput devolveu erro: %v", err)
	}

	if data.Kind != domain.InspectionKindCheckIn || data.KindLabel != "Vistoria de entrada" {
		t.Fatalf("tipo = %q/%q", data.Kind, data.KindLabel)
	}
	if data.Condition != domain.InspectionConditionGood || data.ConditionLabel != "Boa" {
		t.Fatalf("estado geral = %q/%q", data.Condition, data.ConditionLabel)
	}
	if data.TenantName != "Maria Silva" || data.TenantCPF != "123.456.789-00" {
		t.Fatalf("locatário = %q/%q", data.TenantName, data.TenantCPF)
	}
	if data.PropertyName != "Casa da Praia" || data.PropertyAddress != "Rua das Flores, 123, Cabo Frio, RJ" {
		t.Fatalf("imóvel = %q/%q", data.PropertyName, data.PropertyAddress)
	}
	if data.LandlordName != "João Souza" || data.LandlordCPF != "987.654.321-00" {
		t.Fatalf("locador = %q/%q", data.LandlordName, data.LandlordCPF)
	}
	// O período sai do contrato da vistoria e os itens e as fotos vêm da vistoria.
	if data.StartDate != "2026-03-01" || data.EndDate != "2026-09-30" {
		t.Fatalf("período = %q a %q", data.StartDate, data.EndDate)
	}
	if data.PerformedAt != "2026-03-01T10:00" || data.Notes == "" {
		t.Fatalf("vistoria = %q/%q", data.PerformedAt, data.Notes)
	}
	if len(data.Items) != 1 || data.Items[0].Item != "Sofá" {
		t.Fatalf("itens = %+v", data.Items)
	}
	if len(data.Photos) != 1 || data.Photos[0].Name != "fachada.jpg" {
		t.Fatalf("fotos = %+v", data.Photos)
	}
	if data.AuthCode != "AUTH-"+data.Token[:4] {
		t.Fatalf("chave AUTH = %q", data.AuthCode)
	}
	expectedURL := "https://aluxen.evolucsia.com/validar/inspection/" + docInspectionID + "?token=" + data.Token
	if data.ValidationURL != expectedURL {
		t.Fatalf("URL de validação = %q, esperada %q", data.ValidationURL, expectedURL)
	}
	if data.Label != "Maria Silva · Casa da Praia" {
		t.Fatalf("rótulo de auditoria = %q", data.Label)
	}
	if !data.IssuedAt.Equal(fixture.deps.Clock.Now()) {
		t.Fatalf("emissão = %v", data.IssuedAt)
	}

	// Vistoria de saída, sem observações, itens nem fotos.
	checkout, err := application.InspectionDocumentInput(context.Background(), fixture.deps, docCheckoutID)
	if err != nil {
		t.Fatalf("InspectionDocumentInput devolveu erro: %v", err)
	}
	if checkout.KindLabel != "Vistoria de saída" || checkout.ConditionLabel != "Regular" {
		t.Fatalf("vistoria de saída = %q/%q", checkout.KindLabel, checkout.ConditionLabel)
	}
	if len(checkout.Items) != 0 || len(checkout.Photos) != 0 {
		t.Fatalf("vistoria de saída com itens/fotos: %+v/%+v", checkout.Items, checkout.Photos)
	}

	if _, err := application.InspectionDocumentInput(context.Background(), fixture.deps, "inexistente"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("vistoria inexistente: erro = %v", err)
	}
	if _, err := application.InspectionDocumentInput(context.Background(), fixture.deps, "  "); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("id vazio: erro = %v", err)
	}
}

func TestDepositDocumentInputResolvesTheRefund(t *testing.T) {
	fixture := newDocumentFixture()

	data, err := application.DepositDocumentInput(context.Background(), fixture.deps, docSeasonLeaseID)
	if err != nil {
		t.Fatalf("DepositDocumentInput devolveu erro: %v", err)
	}

	if data.Amount != 1200 || data.Refunded != 500 || data.Retained != 700 {
		t.Fatalf("valores = %v/%v/%v", data.Amount, data.Refunded, data.Retained)
	}
	if data.Status != domain.DepositStatusPartiallyRefunded || data.StatusLabel != "Devolvida parcialmente" {
		t.Fatalf("situação = %q/%q", data.Status, data.StatusLabel)
	}
	if data.RefundedAt != "2026-09-21T15:04" || data.ReceivedAt != "2026-03-01T10:00" {
		t.Fatalf("datas = %q/%q", data.RefundedAt, data.ReceivedAt)
	}
	if !data.HasDeposit {
		t.Fatal("contrato com caução registrada não liberou o recibo")
	}
	if data.TenantName != "Maria Silva" || data.PropertyName != "Casa da Praia" {
		t.Fatalf("locatário/imóvel = %q/%q", data.TenantName, data.PropertyName)
	}
	if data.PropertyAddress != "Rua das Flores, 123, Cabo Frio, RJ" {
		t.Fatalf("endereço = %q", data.PropertyAddress)
	}
	if data.Notes != "Retenção pela pintura da sala." {
		t.Fatalf("observações = %q", data.Notes)
	}
	expectedURL := "https://aluxen.evolucsia.com/validar/deposit/" + docSeasonLeaseID + "?token=" + data.Token
	if data.ValidationURL != expectedURL {
		t.Fatalf("URL de validação = %q, esperada %q", data.ValidationURL, expectedURL)
	}

	// Contrato sem caução registrada: o documento existe, mas não há valor a
	// devolver.
	withoutDeposit, err := application.DepositDocumentInput(context.Background(), fixture.deps, docLeaseID)
	if err != nil {
		t.Fatalf("DepositDocumentInput devolveu erro: %v", err)
	}
	if withoutDeposit.HasDeposit || withoutDeposit.Amount != 0 {
		t.Fatalf("contrato sem caução: %v/%v", withoutDeposit.HasDeposit, withoutDeposit.Amount)
	}

	if _, err := application.DepositDocumentInput(context.Background(), fixture.deps, "inexistente"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("contrato inexistente: erro = %v", err)
	}
}

func TestVerifySeasonTokens(t *testing.T) {
	fixture := newDocumentFixture()
	ctx := context.Background()

	inspection, err := application.InspectionDocumentInput(ctx, fixture.deps, docInspectionID)
	if err != nil {
		t.Fatalf("preparar vistoria: %v", err)
	}
	verified, err := application.VerifyInspectionToken(ctx, fixture.deps, docInspectionID, inspection.Token)
	if err != nil {
		t.Fatalf("VerifyInspectionToken devolveu erro: %v", err)
	}
	if verified.InspectionID != docInspectionID {
		t.Fatalf("vistoria validada = %q", verified.InspectionID)
	}

	deposit, err := application.DepositDocumentInput(ctx, fixture.deps, docSeasonLeaseID)
	if err != nil {
		t.Fatalf("preparar caução: %v", err)
	}
	verifiedDeposit, err := application.VerifyDepositToken(ctx, fixture.deps, docSeasonLeaseID, deposit.Token)
	if err != nil {
		t.Fatalf("VerifyDepositToken devolveu erro: %v", err)
	}
	if verifiedDeposit.LeaseID != docSeasonLeaseID {
		t.Fatalf("caução validada = %q", verifiedDeposit.LeaseID)
	}

	// A chave de um documento nunca abre outro: tipo diferente, documento
	// diferente, chave vazia ou truncada.
	for name, testCase := range map[string]struct {
		verify func() (*application.InspectionDocumentData, error)
		valid  bool
	}{
		"chave da vistoria": {func() (*application.InspectionDocumentData, error) {
			return application.VerifyInspectionToken(ctx, fixture.deps, docInspectionID, inspection.Token)
		}, true},
		"chave da caução": {func() (*application.InspectionDocumentData, error) {
			return application.VerifyInspectionToken(ctx, fixture.deps, docInspectionID, deposit.Token)
		}, false},
		"chave do recibo": {func() (*application.InspectionDocumentData, error) {
			return application.VerifyInspectionToken(ctx, fixture.deps, docInspectionID, docSigner{}.Generate("receipt", docInspectionID))
		}, false},
		"outra vistoria": {func() (*application.InspectionDocumentData, error) {
			return application.VerifyInspectionToken(ctx, fixture.deps, docInspectionID, docSigner{}.Generate("inspection", docCheckoutID))
		}, false},
		"chave vazia": {func() (*application.InspectionDocumentData, error) {
			return application.VerifyInspectionToken(ctx, fixture.deps, docInspectionID, "")
		}, false},
		"chave truncada": {func() (*application.InspectionDocumentData, error) {
			return application.VerifyInspectionToken(ctx, fixture.deps, docInspectionID, inspection.Token[:8])
		}, false},
	} {
		_, err := testCase.verify()
		if testCase.valid {
			if err != nil {
				t.Fatalf("%s: chave válida recusada: %v", name, err)
			}
			continue
		}
		if !domain.IsValidation(err) {
			t.Fatalf("%s: erro = %v, esperado erro de validação", name, err)
		}
		if message := domain.ValidationMessage(err); message != "Chave de autenticação inválida ou documento não autenticado." {
			t.Fatalf("%s: mensagem = %q", name, message)
		}
	}

	if _, err := application.VerifyDepositToken(ctx, fixture.deps, docSeasonLeaseID, inspection.Token); !domain.IsValidation(err) {
		t.Fatalf("chave da vistoria na caução: erro = %v", err)
	}
	if _, err := application.VerifyDepositToken(ctx, fixture.deps, docSeasonLeaseID, deposit.Token[:8]); !domain.IsValidation(err) {
		t.Fatalf("chave truncada na caução: erro = %v", err)
	}

	// Chave válida de documento inexistente: a chave confere, o documento não.
	if _, err := application.VerifyInspectionToken(ctx, fixture.deps, "inexistente", docSigner{}.Generate("inspection", "inexistente")); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("vistoria inexistente: erro = %v", err)
	}
	if _, err := application.VerifyDepositToken(ctx, fixture.deps, "inexistente", docSigner{}.Generate("deposit", "inexistente")); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("contrato inexistente: erro = %v", err)
	}
}

func TestCanAccessInspection(t *testing.T) {
	fixture := newDocumentFixture()
	ctx := context.Background()

	cases := map[string]struct {
		inspectionID string
		adminID      string
		clientID     string
		allowed      bool
	}{
		"administrador":        {docInspectionID, "admin-1", "", true},
		"titular":              {docInspectionID, "", docTenantID, true},
		"vistoria de saída":    {docCheckoutID, "", docTenantID, true},
		"outro locatário":      {docInspectionID, "", "tenant-2", false},
		"sem sessão":           {docInspectionID, "", "", false},
		"vistoria inexistente": {"inexistente", "", docTenantID, false},
	}
	for name, testCase := range cases {
		allowed, err := application.CanAccessInspection(ctx, fixture.deps, testCase.inspectionID, testCase.adminID, testCase.clientID)
		if err != nil {
			t.Fatalf("%s: erro inesperado: %v", name, err)
		}
		if allowed != testCase.allowed {
			t.Fatalf("%s: acesso = %v, esperado %v", name, allowed, testCase.allowed)
		}
	}

	fixture.inspections.err = errors.New("banco fora do ar")
	if _, err := application.CanAccessInspection(ctx, fixture.deps, docInspectionID, "", docTenantID); err == nil {
		t.Fatal("falha de leitura não propagou erro")
	}
}

func TestCanAccessDeposit(t *testing.T) {
	fixture := newDocumentFixture()
	ctx := context.Background()

	cases := map[string]struct {
		leaseID  string
		adminID  string
		clientID string
		allowed  bool
	}{
		"administrador":        {docSeasonLeaseID, "admin-1", "", true},
		"titular":              {docSeasonLeaseID, "", docTenantID, true},
		"contrato sem caução":  {docLeaseID, "", docTenantID, true},
		"outro locatário":      {docSeasonLeaseID, "", "tenant-2", false},
		"sem sessão":           {docSeasonLeaseID, "", "", false},
		"contrato inexistente": {"inexistente", "", docTenantID, false},
	}
	for name, testCase := range cases {
		allowed, err := application.CanAccessDeposit(ctx, fixture.deps, testCase.leaseID, testCase.adminID, testCase.clientID)
		if err != nil {
			t.Fatalf("%s: erro inesperado: %v", name, err)
		}
		if allowed != testCase.allowed {
			t.Fatalf("%s: acesso = %v, esperado %v", name, allowed, testCase.allowed)
		}
	}
}
