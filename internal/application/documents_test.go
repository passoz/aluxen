package application_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Dublês mínimos dos repositórios usados pelos casos de uso de documento. Os
// métodos que o fluxo de documento não exercita ficam de fora: a interface do
// domínio é embutida, então qualquer uso inesperado falha na hora.

type docTenants struct {
	domain.TenantRepository
	byID map[string]*domain.Tenant
}

func (r *docTenants) ByID(_ context.Context, id string) (*domain.Tenant, error) {
	tenant, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return tenant, nil
}

type docProperties struct {
	domain.PropertyRepository
	byID map[string]*domain.Property
}

func (r *docProperties) ByID(_ context.Context, id string) (*domain.Property, error) {
	property, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return property, nil
}

type docLeases struct {
	domain.LeaseRepository
	byID map[string]*domain.Lease
	err  error
}

func (r *docLeases) ByID(_ context.Context, id string) (*domain.Lease, error) {
	if r.err != nil {
		return nil, r.err
	}
	lease, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return lease, nil
}

type docReceipts struct {
	domain.ReceiptRepository
	byID map[string]*domain.Receipt
	err  error
}

func (r *docReceipts) ByID(_ context.Context, id string) (*domain.Receipt, error) {
	if r.err != nil {
		return nil, r.err
	}
	receipt, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return receipt, nil
}

type docInspections struct {
	domain.InspectionRepository
	byID map[string]*domain.Inspection
	err  error
}

func (r *docInspections) ByID(_ context.Context, id string) (*domain.Inspection, error) {
	if r.err != nil {
		return nil, r.err
	}
	inspection, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return inspection, nil
}

type docSettings struct{ values map[string]string }

func (r *docSettings) Get(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", domain.ErrNotFound
	}
	return value, nil
}

func (r *docSettings) List(context.Context) (map[string]string, error) { return r.values, nil }

func (r *docSettings) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

// docSigner reproduz o contrato da porta `TokenSigner` sem HMAC: o objetivo
// aqui é o fluxo dos casos de uso, não o algoritmo (coberto em authz).
type docSigner struct{}

func (docSigner) Generate(kind, id string) string {
	token := fmt.Sprintf("%s:%s", kind, id)
	for len(token) < 32 {
		token += "0"
	}
	return token[:32]
}

func (docSigner) FormatCode(token string) string { return "AUTH-" + token[:4] }

func (docSigner) ValidationURL(base, kind, id, token string) string {
	return base + "/validar/" + kind + "/" + id + "?token=" + token
}

type docClock struct{ now time.Time }

func (c docClock) Now() time.Time { return c.now }

// documentFixture monta o cenário: um contrato ativo do locatário `tenant-1`
// com o imóvel `property-1`, e um recibo pago vinculado ao contrato.
type documentFixture struct {
	deps        application.Deps
	receipts    *docReceipts
	leases      *docLeases
	settings    *docSettings
	inspections *docInspections
}

const (
	docLeaseID    = "018f2c1e-6b3d-7c4a-9e21-5f6a7b8c9d0e"
	docReceiptID  = "019a3b4c-5d6e-7f80-91a2-b3c4d5e6f708"
	docTenantID   = "tenant-1"
	docPropertyID = "property-1"
	// Contrato de temporada com caução devolvida em parte e vistoria de saída.
	docSeasonLeaseID = "019b1c2d-3e4f-7081-92a3-b4c5d6e7f809"
	docInspectionID  = "019c2d3e-4f50-7182-93a4-b5c6d7e8f90a"
	docCheckoutID    = "019d3e4f-5061-7283-94a5-b6c7d8e9f01b"
)

func newDocumentFixture() *documentFixture {
	phone := "(22) 99999-0000"
	cpf := "123.456.789-00"
	propertyCPF := "987.654.321-00"
	paidAt := "2026-09-09T14:30"

	receipts := &docReceipts{byID: map[string]*domain.Receipt{
		docReceiptID: {
			ID:           docReceiptID,
			LeaseID:      domain.StrPtr(docLeaseID),
			Category:     domain.ReceiptCategoryRent,
			DueDate:      "2026-09-10",
			PaidAt:       &paidAt,
			Amount:       1200.50,
			PaidAmount:   domain.FloatPtr(500),
			TenantName:   "Maria Silva",
			PropertyName: "Casa da Praia",
		},
		"receipt-extra": {
			ID:           "receipt-extra",
			TenantID:     domain.StrPtr(docTenantID),
			PropertyID:   domain.StrPtr(docPropertyID),
			Category:     domain.ReceiptCategoryExtra,
			Description:  domain.StrPtr("Limpeza"),
			DueDate:      "2026-09-20",
			Amount:       250,
			PropertyName: "Casa da Praia",
			TenantName:   "Maria Silva",
		},
		"receipt-other-tenant": {
			ID:         "receipt-other-tenant",
			TenantID:   domain.StrPtr("tenant-2"),
			Category:   domain.ReceiptCategoryRent,
			DueDate:    "2026-09-10",
			Amount:     900,
			TenantName: "Outro Locatário",
		},
	}}
	leases := &docLeases{byID: map[string]*domain.Lease{
		docLeaseID: {
			ID:           docLeaseID,
			TenantID:     docTenantID,
			PropertyID:   docPropertyID,
			StartDate:    "2026-03-01",
			EndDate:      "2027-02-28",
			DueDay:       10,
			RentAmount:   1200.50,
			Status:       domain.LeaseStatusActive,
			TenantName:   "Maria Silva",
			PropertyName: "Casa da Praia",
		},
		docSeasonLeaseID: {
			ID:                 docSeasonLeaseID,
			TenantID:           docTenantID,
			PropertyID:         docPropertyID,
			StartDate:          "2026-03-01",
			EndDate:            "2026-09-30",
			DueDay:             10,
			RentAmount:         1200,
			Status:             domain.LeaseStatusSeason,
			TenantName:         "Maria Silva",
			PropertyName:       "Casa da Praia",
			DepositAmount:      domain.FloatPtr(1200),
			DepositStatus:      domain.DepositStatusPartiallyRefunded,
			DepositReceivedAt:  domain.StrPtr("2026-03-01T10:00"),
			DepositRefundedAt:  domain.StrPtr("2026-09-21T15:04"),
			DepositRefundedAmt: domain.FloatPtr(500),
			DepositNotes:       domain.StrPtr("Retenção pela pintura da sala."),
		},
	}}
	inspections := &docInspections{byID: map[string]*domain.Inspection{
		docInspectionID: {
			ID:           docInspectionID,
			LeaseID:      docSeasonLeaseID,
			Kind:         domain.InspectionKindCheckIn,
			PerformedAt:  "2026-03-01T10:00",
			Condition:    domain.InspectionConditionGood,
			Notes:        domain.StrPtr("Chaves entregues em duas vias."),
			Items:        []domain.InspectionItem{{Area: "Sala", Item: "Sofá", State: "Bom"}},
			Photos:       []domain.InspectionPhoto{{Name: "fachada.jpg", Label: "fachada.jpg"}},
			TenantName:   "Maria Silva",
			PropertyName: "Casa da Praia",
		},
		docCheckoutID: {
			ID:           docCheckoutID,
			LeaseID:      docSeasonLeaseID,
			Kind:         domain.InspectionKindCheckOut,
			PerformedAt:  "2026-09-20T09:00",
			Condition:    domain.InspectionConditionFair,
			TenantName:   "Maria Silva",
			PropertyName: "Casa da Praia",
		},
	}}
	tenants := &docTenants{byID: map[string]*domain.Tenant{
		docTenantID: {ID: docTenantID, Name: "Maria Silva", CPF: &cpf, Phone: &phone, Email: "maria@example.com"},
	}}
	properties := &docProperties{byID: map[string]*domain.Property{
		docPropertyID: {ID: docPropertyID, Name: "Casa da Praia", Address: "Rua das Flores, 123", City: "Cabo Frio", State: "RJ", Zip: domain.StrPtr("28900-000")},
	}}
	settings := &docSettings{values: map[string]string{
		domain.SettingLandlordName:    "João Souza",
		domain.SettingLandlordCPF:     propertyCPF,
		domain.SettingLandlordAddress: "Rua do Sol, 45, Cabo Frio - RJ",
		domain.SettingAppURL:          "https://aluxen.evolucsia.com",
	}}

	deps := application.Deps{
		Repos: application.Repositories{
			Tenants:     tenants,
			Properties:  properties,
			Leases:      leases,
			Receipts:    receipts,
			Inspections: inspections,
			Settings:    settings,
		},
		Clock:  docClock{now: time.Date(2026, 9, 21, 15, 4, 5, 0, time.UTC)},
		Signer: docSigner{},
		Log:    slog.New(slog.DiscardHandler),
	}
	return &documentFixture{deps: deps, receipts: receipts, leases: leases, settings: settings, inspections: inspections}
}

func TestReceiptDocumentInputResolvesTheTenantPropertyAndLandlord(t *testing.T) {
	fixture := newDocumentFixture()

	data, err := application.ReceiptDocumentInput(context.Background(), fixture.deps, docReceiptID)
	if err != nil {
		t.Fatalf("ReceiptDocumentInput devolveu erro: %v", err)
	}

	if data.TenantName != "Maria Silva" {
		t.Fatalf("locatário = %q", data.TenantName)
	}
	if data.TenantCPF != "123.456.789-00" {
		t.Fatalf("CPF do locatário = %q", data.TenantCPF)
	}
	if data.PropertyName != "Casa da Praia" {
		t.Fatalf("imóvel = %q", data.PropertyName)
	}
	if data.PropertyAddress != "Rua das Flores, 123, Cabo Frio, RJ" {
		t.Fatalf("endereço = %q", data.PropertyAddress)
	}
	if data.LandlordName != "João Souza" || data.LandlordCPF != "987.654.321-00" {
		t.Fatalf("locador = %q/%q", data.LandlordName, data.LandlordCPF)
	}
	if !data.IsPaid || !data.IsPartial {
		t.Fatalf("situação do recibo: pago=%v parcial=%v", data.IsPaid, data.IsPartial)
	}
	if data.PaidAmount != 500 || !data.HasPaidAmount {
		t.Fatalf("valor pago = %v (informado=%v)", data.PaidAmount, data.HasPaidAmount)
	}
	if data.AuthCode != "AUTH-"+data.Token[:4] {
		t.Fatalf("chave AUTH = %q", data.AuthCode)
	}
	expectedURL := "https://aluxen.evolucsia.com/validar/receipt/" + docReceiptID + "?token=" + data.Token
	if data.ValidationURL != expectedURL {
		t.Fatalf("URL de validação = %q, esperada %q", data.ValidationURL, expectedURL)
	}
	if !data.IssuedAt.Equal(fixture.deps.Clock.Now()) {
		t.Fatalf("emissão = %v", data.IssuedAt)
	}
	if data.Label != "Maria Silva · Casa da Praia · 09/2026" {
		t.Fatalf("rótulo de auditoria = %q", data.Label)
	}
}

func TestReceiptDocumentInputFallsBackToTheReceiptLinks(t *testing.T) {
	fixture := newDocumentFixture()

	// Lançamento avulso: sem contrato, os vínculos do próprio recibo valem.
	data, err := application.ReceiptDocumentInput(context.Background(), fixture.deps, "receipt-extra")
	if err != nil {
		t.Fatalf("ReceiptDocumentInput devolveu erro: %v", err)
	}
	if data.TenantName != "Maria Silva" || data.PropertyName != "Casa da Praia" {
		t.Fatalf("vínculos diretos não resolveram: %q/%q", data.TenantName, data.PropertyName)
	}
	if data.IsPaid {
		t.Fatal("lançamento em aberto marcado como pago")
	}
	if data.HasPaidAmount {
		t.Fatal("lançamento sem valor pago informou valor pago")
	}
	if data.Category != domain.ReceiptCategoryExtra || data.Description != "Limpeza" {
		t.Fatalf("categoria/descrição = %q/%q", data.Category, data.Description)
	}
}

func TestReceiptDocumentInputUsesTheFallbackName(t *testing.T) {
	fixture := newDocumentFixture()
	receipt := *fixture.receipts.byID[docReceiptID]
	receipt.LeaseID = nil
	receipt.TenantID = nil
	receipt.PropertyID = nil
	receipt.TenantName = ""
	receipt.PropertyName = ""
	fixture.receipts.byID[docReceiptID] = &receipt

	data, err := application.ReceiptDocumentInput(context.Background(), fixture.deps, docReceiptID)
	if err != nil {
		t.Fatalf("ReceiptDocumentInput devolveu erro: %v", err)
	}
	if data.TenantName != "Locatário" {
		t.Fatalf("nome padrão do locatário = %q", data.TenantName)
	}
	fixture.settings.values[domain.SettingLandlordName] = ""
	data, err = application.ReceiptDocumentInput(context.Background(), fixture.deps, docReceiptID)
	if err != nil {
		t.Fatalf("ReceiptDocumentInput devolveu erro: %v", err)
	}
	if data.LandlordName != "Locador" {
		t.Fatalf("nome padrão do locador = %q", data.LandlordName)
	}
}

func TestReceiptDocumentInputMissingReceipt(t *testing.T) {
	fixture := newDocumentFixture()

	_, err := application.ReceiptDocumentInput(context.Background(), fixture.deps, "inexistente")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("erro = %v, esperado ErrNotFound", err)
	}
	_, err = application.ReceiptDocumentInput(context.Background(), fixture.deps, "  ")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("id vazio: erro = %v, esperado ErrNotFound", err)
	}
}

func TestLeaseDocumentInput(t *testing.T) {
	fixture := newDocumentFixture()

	data, err := application.LeaseDocumentInput(context.Background(), fixture.deps, docLeaseID)
	if err != nil {
		t.Fatalf("LeaseDocumentInput devolveu erro: %v", err)
	}
	if data.TenantName != "Maria Silva" || data.TenantPhone != "(22) 99999-0000" || data.TenantCPF != "123.456.789-00" {
		t.Fatalf("locatário = %q/%q/%q", data.TenantName, data.TenantPhone, data.TenantCPF)
	}
	if data.PropertyCity != "Cabo Frio" || data.PropertyState != "RJ" || data.PropertyZip != "28900-000" {
		t.Fatalf("imóvel = %q/%q/%q", data.PropertyCity, data.PropertyState, data.PropertyZip)
	}
	if data.RentAmount != 1200.50 || data.DueDay != 10 || data.Status != domain.LeaseStatusActive {
		t.Fatalf("contrato = %v/%v/%q", data.RentAmount, data.DueDay, data.Status)
	}
	if data.LandlordAddress != "Rua do Sol, 45, Cabo Frio - RJ" {
		t.Fatalf("endereço do locador = %q", data.LandlordAddress)
	}
	if data.ValidationURL != "https://aluxen.evolucsia.com/validar/lease/"+docLeaseID+"?token="+data.Token {
		t.Fatalf("URL de validação = %q", data.ValidationURL)
	}

	if _, err := application.LeaseDocumentInput(context.Background(), fixture.deps, "inexistente"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("contrato inexistente: erro = %v", err)
	}
}

func TestVerifyTokens(t *testing.T) {
	fixture := newDocumentFixture()
	ctx := context.Background()

	receipt, err := application.ReceiptDocumentInput(ctx, fixture.deps, docReceiptID)
	if err != nil {
		t.Fatalf("preparar recibo: %v", err)
	}
	verified, err := application.VerifyReceiptToken(ctx, fixture.deps, docReceiptID, receipt.Token)
	if err != nil {
		t.Fatalf("VerifyReceiptToken devolveu erro: %v", err)
	}
	if verified.ReceiptID != docReceiptID {
		t.Fatalf("recibo validado = %q", verified.ReceiptID)
	}

	// Chave de outro documento, chave de outro tipo e chave vazia são recusadas
	// com a mensagem do app atual.
	lease, err := application.LeaseDocumentInput(ctx, fixture.deps, docLeaseID)
	if err != nil {
		t.Fatalf("preparar contrato: %v", err)
	}
	for name, testCase := range map[string]struct {
		id    string
		token string
	}{
		"outro documento":   {docReceiptID, "00000000000000000000000000000000"},
		"chave do contrato": {docReceiptID, lease.Token},
		"chave vazia":       {docReceiptID, ""},
		"chave curta":       {docReceiptID, receipt.Token[:8]},
	} {
		_, err := application.VerifyReceiptToken(ctx, fixture.deps, testCase.id, testCase.token)
		if !domain.IsValidation(err) {
			t.Fatalf("%s: erro = %v, esperado erro de validação", name, err)
		}
		if message := domain.ValidationMessage(err); message != "Chave de autenticação inválida ou documento não autenticado." {
			t.Fatalf("%s: mensagem = %q", name, message)
		}
	}

	// Chave válida de documento inexistente: a chave confere, o documento não.
	_, err = application.VerifyReceiptToken(ctx, fixture.deps, "inexistente", docSigner{}.Generate("receipt", "inexistente"))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("documento inexistente: erro = %v, esperado ErrNotFound", err)
	}

	verifiedLease, err := application.VerifyLeaseToken(ctx, fixture.deps, docLeaseID, lease.Token)
	if err != nil {
		t.Fatalf("VerifyLeaseToken devolveu erro: %v", err)
	}
	if verifiedLease.LeaseID != docLeaseID {
		t.Fatalf("contrato validado = %q", verifiedLease.LeaseID)
	}
	if _, err := application.VerifyLeaseToken(ctx, fixture.deps, docLeaseID, receipt.Token); !domain.IsValidation(err) {
		t.Fatalf("chave do recibo no contrato: erro = %v", err)
	}
}

func TestVerifyWithoutSignerIsRefused(t *testing.T) {
	fixture := newDocumentFixture()
	fixture.deps.Signer = nil

	_, err := application.VerifyReceiptToken(context.Background(), fixture.deps, docReceiptID, "qualquer")
	if !domain.IsValidation(err) {
		t.Fatalf("erro = %v, esperado erro de validação", err)
	}
}

func TestCanAccessReceipt(t *testing.T) {
	fixture := newDocumentFixture()
	ctx := context.Background()

	cases := map[string]struct {
		docReceiptID string
		adminID      string
		clientID     string
		allowed      bool
	}{
		"administrador sempre pode":          {docReceiptID, "admin-1", "", true},
		"administrador com documento alheio": {"receipt-other-tenant", "admin-1", "", true},
		"titular pelo contrato":              {docReceiptID, "", docTenantID, true},
		"titular pelo vínculo do lançamento": {"receipt-extra", "", docTenantID, true},
		"outro locatário":                    {docReceiptID, "", "tenant-2", false},
		"sem sessão":                         {docReceiptID, "", "", false},
		"documento inexistente":              {"inexistente", "", docTenantID, false},
	}
	for name, testCase := range cases {
		allowed, err := application.CanAccessReceipt(ctx, fixture.deps, testCase.docReceiptID, testCase.adminID, testCase.clientID)
		if err != nil {
			t.Fatalf("%s: erro inesperado: %v", name, err)
		}
		if allowed != testCase.allowed {
			t.Fatalf("%s: acesso = %v, esperado %v", name, allowed, testCase.allowed)
		}
	}

	// Falha inesperada do repositório não vira autorização.
	fixture.receipts.err = errors.New("banco fora do ar")
	if _, err := application.CanAccessReceipt(ctx, fixture.deps, docReceiptID, "", docTenantID); err == nil {
		t.Fatal("falha de leitura não propagou erro")
	}
}

func TestCanAccessLease(t *testing.T) {
	fixture := newDocumentFixture()
	ctx := context.Background()

	cases := map[string]struct {
		docLeaseID string
		adminID    string
		clientID   string
		allowed    bool
	}{
		"administrador":        {docLeaseID, "admin-1", "", true},
		"titular":              {docLeaseID, "", docTenantID, true},
		"outro locatário":      {docLeaseID, "", "tenant-2", false},
		"sem sessão":           {docLeaseID, "", "", false},
		"contrato inexistente": {"inexistente", "", docTenantID, false},
	}
	for name, testCase := range cases {
		allowed, err := application.CanAccessLease(ctx, fixture.deps, testCase.docLeaseID, testCase.adminID, testCase.clientID)
		if err != nil {
			t.Fatalf("%s: erro inesperado: %v", name, err)
		}
		if allowed != testCase.allowed {
			t.Fatalf("%s: acesso = %v, esperado %v", name, allowed, testCase.allowed)
		}
	}
}
