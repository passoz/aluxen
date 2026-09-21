package web

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/authz"
	"github.com/passoz/aluxen/internal/infrastructure/config"
)

// ── Dublês dos repositórios dos documentos ─────────────────────────
//
// A interface do domínio é embutida para que só os métodos exercitados pelo
// fluxo de documento precisem existir; qualquer uso inesperado falha na hora.

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
}

func (r *docLeases) ByID(_ context.Context, id string) (*domain.Lease, error) {
	lease, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return lease, nil
}

type docReceipts struct {
	domain.ReceiptRepository
	byID map[string]*domain.Receipt
}

func (r *docReceipts) ByID(_ context.Context, id string) (*domain.Receipt, error) {
	receipt, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return receipt, nil
}

type docInspections struct {
	domain.InspectionRepository
	byID map[string]*domain.Inspection
}

func (r *docInspections) ByID(_ context.Context, id string) (*domain.Inspection, error) {
	inspection, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return inspection, nil
}

// ── Montagem do servidor de documentos ─────────────────────────────

const (
	docLeaseID   = "018f2c1e-6b3d-7c4a-9e21-5f6a7b8c9d0e"
	docReceiptID = "019a3b4c-5d6e-7f80-91a2-b3c4d5e6f708"
	docTenantID  = "tenant-1"
	docAdminID   = "admin-1"
	docSecret    = "test-session-secret-with-enough-length"
	// Contrato de temporada com caução devolvida em parte, a vistoria de entrada
	// e a de saída.
	docSeasonLeaseID = "019b1c2d-3e4f-7081-92a3-b4c5d6e7f809"
	docInspectionID  = "019c2d3e-4f50-7182-93a4-b5c6d7e8f90a"
	docCheckoutID    = "019d3e4f-5061-7283-94a5-b6c7d8e9f01b"
)

type documentsServer struct {
	*testServer
	signer   *authz.TokenGenerator
	settings *fakeSettings
}

// newDocumentsServer monta o servidor com um contrato ativo, um recibo pago e um
// recibo em aberto do locatário `tenant-1`, além de um recibo de outro locatário.
func newDocumentsServer(t *testing.T) *documentsServer {
	t.Helper()

	phone := "(22) 99999-0000"
	cpf := "123.456.789-00"
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
		"receipt-open": {
			ID:           "receipt-open",
			LeaseID:      domain.StrPtr(docLeaseID),
			Category:     domain.ReceiptCategoryRent,
			DueDate:      "2026-10-10",
			Amount:       1200.50,
			TenantName:   "Maria Silva",
			PropertyName: "Casa da Praia",
		},
		"receipt-other": {
			ID:           "receipt-other",
			TenantID:     domain.StrPtr("tenant-2"),
			Category:     domain.ReceiptCategoryRent,
			DueDate:      "2026-09-10",
			Amount:       900,
			TenantName:   "Outro Locatário",
			PropertyName: "Casa do Centro",
		},
	}}
	leases := &docLeases{byID: map[string]*domain.Lease{
		docLeaseID: {
			ID:           docLeaseID,
			TenantID:     docTenantID,
			PropertyID:   "property-1",
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
			PropertyID:         "property-1",
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
		docTenantID: {ID: docTenantID, Name: "Maria Silva", CPF: &cpf, Phone: &phone},
		"tenant-2":  {ID: "tenant-2", Name: "Outro Locatário"},
	}}
	properties := &docProperties{byID: map[string]*domain.Property{
		"property-1": {ID: "property-1", Name: "Casa da Praia", Address: "Rua das Flores, 123", City: "Cabo Frio", State: "RJ", Zip: domain.StrPtr("28900-000")},
	}}
	settings := newFakeSettings()
	settings.values[domain.SettingLandlordName] = "João Souza"
	settings.values[domain.SettingLandlordCPF] = "987.654.321-00"
	settings.values[domain.SettingLandlordAddress] = "Rua do Sol, 45, Cabo Frio - RJ"
	settings.values[domain.SettingAppURL] = "https://aluxen.evolucsia.com"

	audit := &fakeAudit{}
	clock := &fakeClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	cfg := &config.Config{
		AppEnv:        "test",
		SessionSecret: docSecret,
		AppURL:        "https://aluxen.evolucsia.com",
	}
	signer := authz.NewTokenGenerator(docSecret)

	deps := application.Deps{
		Repos: application.Repositories{
			Users:       newFakeUsers(&domain.User{ID: docAdminID, Username: "passoz", PasswordHash: "hash:senha-de-teste"}),
			Tenants:     tenants,
			Properties:  properties,
			Leases:      leases,
			Receipts:    receipts,
			Settings:    settings,
			Audit:       audit,
			Inspections: inspections,
		},
		Clock:  clock,
		IDs:    &fakeIDs{},
		Hasher: fakeHasher{},
		Signer: signer,
		Log:    slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	common := &Common{
		Cfg:                    cfg,
		Log:                    deps.Log,
		Render:                 renderer,
		Session:                NewSessionManager(docSecret, false),
		CSRF:                   NewCSRF(docSecret, false),
		Deps:                   deps,
		Started:                clock.now,
		Dialect:                "sqlite",
		LoginGate:              application.NewLoginGate(clock, 5, 60, 10),
		MustChangePasswordFunc: func(context.Context, string) (bool, error) { return false, nil },
	}

	return &documentsServer{
		testServer: &testServer{handler: NewHandlers(common), audit: audit, users: deps.Repos.Users.(*fakeUsers), clock: clock},
		signer:     signer,
		settings:   settings,
	}
}

// get executa um GET opcionalmente autenticado por sessão.
func (s *documentsServer) get(t *testing.T, target, adminID, clientID string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, target, nil)
	if adminID != "" {
		request.AddCookie(sessionCookie(t, s.handler.Session, adminID, true))
	}
	if clientID != "" {
		request.AddCookie(sessionCookie(t, s.handler.Session, clientID, false))
	}
	return s.do(request)
}

// sessionCookie emite um cookie de sessão válido usando o próprio gerenciador —
// evita depender do fluxo de login nos testes de documento.
func sessionCookie(t *testing.T, sessions *SessionManager, id string, admin bool) *http.Cookie {
	t.Helper()

	recorder := httptest.NewRecorder()
	if admin {
		sessions.SetAdmin(recorder, id)
	} else {
		sessions.SetClient(recorder, id)
	}
	name := clientCookieName
	if admin {
		name = adminCookieName
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %s não foi emitido", name)
	return nil
}

func (s *documentsServer) receiptToken(kind, id string) string {
	return s.signer.Generate(kind, id)
}

// ── /pdf ───────────────────────────────────────────────────────────

func TestReceiptPDFRejectsRequestWithoutID(t *testing.T) {
	server := newDocumentsServer(t)

	response := server.get(t, "/pdf", docAdminID, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", response.Code)
	}
	if body := response.Body.String(); !strings.Contains(body, "ID inválido") {
		t.Fatalf("corpo = %q", body)
	}
}

func TestReceiptPDFSendsAnonymousVisitorToLogin(t *testing.T) {
	server := newDocumentsServer(t)

	response := server.get(t, "/pdf?id="+docReceiptID, "", "")
	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, esperado 302", response.Code)
	}
	if location := response.Header().Get("Location"); location != "/login" {
		t.Fatalf("destino = %q, esperado /login", location)
	}
}

func TestReceiptPDFWithValidTokenServesTheDocument(t *testing.T) {
	server := newDocumentsServer(t)
	token := server.receiptToken(authz.KindReceipt, docReceiptID)

	response := server.get(t, "/pdf?id="+docReceiptID+"&token="+token, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/pdf" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	expectedDisposition := `inline; filename="recibo-` + docReceiptID + `.pdf"`
	if disposition := response.Header().Get("Content-Disposition"); disposition != expectedDisposition {
		t.Fatalf("Content-Disposition = %q, esperado %q", disposition, expectedDisposition)
	}
	if !bytes.HasPrefix(response.Body.Bytes(), []byte("%PDF-")) {
		t.Fatal("resposta não é um PDF")
	}
	if actions := server.audit.actions(); len(actions) != 1 || actions[0] != domain.ActionDocumentReceipt {
		t.Fatalf("auditoria = %v", actions)
	}
	if origin := server.audit.entries[0].Origin; origin != domain.AuditOriginPublic {
		t.Fatalf("origem da auditoria = %q, esperado link público", origin)
	}
	if label := domain.Str(server.audit.entries[0].EntityLabel); label == "" {
		t.Fatal("auditoria sem rótulo do lançamento")
	}
}

func TestReceiptPDFAsAdminAndAsOwnerTenant(t *testing.T) {
	server := newDocumentsServer(t)

	admin := server.get(t, "/pdf?id="+docReceiptID, docAdminID, "")
	if admin.Code != http.StatusOK {
		t.Fatalf("administrador: status = %d", admin.Code)
	}
	if origin := server.audit.entries[0].Origin; origin != domain.AuditOriginWeb {
		t.Fatalf("origem da auditoria do administrador = %q", origin)
	}

	owner := server.get(t, "/pdf?id="+docReceiptID, "", docTenantID)
	if owner.Code != http.StatusOK {
		t.Fatalf("titular: status = %d, corpo %s", owner.Code, owner.Body.String())
	}

	// O modo "sem assinatura" continua servindo o documento.
	withoutSignature := server.get(t, "/pdf?id="+docReceiptID+"&assinatura=false", docAdminID, "")
	if withoutSignature.Code != http.StatusOK {
		t.Fatalf("sem assinatura: status = %d", withoutSignature.Code)
	}
}

func TestReceiptPDFRefusesAnotherTenant(t *testing.T) {
	server := newDocumentsServer(t)

	response := server.get(t, "/pdf?id="+docReceiptID, "", "tenant-2")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403", response.Code)
	}
	if body := response.Body.String(); !strings.Contains(body, "Este documento não pertence ao seu cadastro.") {
		t.Fatalf("corpo = %q", body)
	}
}

func TestReceiptPDFRefusesUnpaidReceipt(t *testing.T) {
	server := newDocumentsServer(t)

	response := server.get(t, "/pdf?id=receipt-open", docAdminID, "")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403", response.Code)
	}
	if body := response.Body.String(); !strings.Contains(body, "Recibo não pago. O recibo só é liberado após o pagamento.") {
		t.Fatalf("corpo = %q", body)
	}
	if actions := server.audit.actions(); len(actions) != 0 {
		t.Fatalf("recibo não pago foi auditado como emitido: %v", actions)
	}
}

func TestReceiptPDFNotFound(t *testing.T) {
	server := newDocumentsServer(t)
	token := server.receiptToken(authz.KindReceipt, "inexistente")

	for name, target := range map[string]string{
		"administrador": "/pdf?id=inexistente",
		"chave válida":  "/pdf?id=inexistente&token=" + token,
	} {
		response := server.get(t, target, docAdminID, "")
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, esperado 404", name, response.Code)
		}
		if body := response.Body.String(); !strings.Contains(body, "Recibo não encontrado") {
			t.Fatalf("%s: corpo = %q", name, body)
		}
	}

	// Chave inválida e nenhuma sessão: ninguém autorizado.
	response := server.get(t, "/pdf?id=inexistente&token=00000000000000000000000000000000", "", "")
	if response.Code != http.StatusFound {
		t.Fatalf("chave inválida: status = %d, esperado 302", response.Code)
	}
}

// ── /contract-pdf ──────────────────────────────────────────────────

func TestContractPDFServesTheContract(t *testing.T) {
	server := newDocumentsServer(t)
	token := server.receiptToken(authz.KindLease, docLeaseID)

	response := server.get(t, "/contract-pdf?id="+docLeaseID+"&token="+token, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", response.Code, response.Body.String())
	}
	expectedDisposition := `inline; filename="contrato-` + docLeaseID + `.pdf"`
	if disposition := response.Header().Get("Content-Disposition"); disposition != expectedDisposition {
		t.Fatalf("Content-Disposition = %q, esperado %q", disposition, expectedDisposition)
	}
	if !bytes.HasPrefix(response.Body.Bytes(), []byte("%PDF-")) {
		t.Fatal("resposta não é um PDF")
	}
	if actions := server.audit.actions(); len(actions) != 1 || actions[0] != domain.ActionDocumentContract {
		t.Fatalf("auditoria = %v", actions)
	}

	missing := server.get(t, "/contract-pdf?id=inexistente", docAdminID, "")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "Contrato não encontrado") {
		t.Fatalf("contrato inexistente: status = %d, corpo %q", missing.Code, missing.Body.String())
	}

	invalid := server.get(t, "/contract-pdf?id="+docLeaseID, "", "tenant-2")
	if invalid.Code != http.StatusForbidden {
		t.Fatalf("outro locatário: status = %d, esperado 403", invalid.Code)
	}
}

// ── /validar/recibo/{id} ───────────────────────────────────────────

func TestVerifyReceiptPageShowsTheOfficialData(t *testing.T) {
	server := newDocumentsServer(t)
	token := server.receiptToken(authz.KindReceipt, docReceiptID)

	response := server.get(t, "/validar/recibo/"+docReceiptID+"?token="+token, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, expected := range []string{
		"DOCUMENTO AUTÊNTICO E VÁLIDO",
		"Recibo de Pagamento de Aluguel",
		server.signer.FormatCode(token),
		"Maria Silva",
		"123.456.789-00",
		"João Souza",
		"Casa da Praia",
		"Rua das Flores, 123, Cabo Frio, RJ",
		"R$ 1.200,50",
		"R$ 500,00",
		"R$ 700,50",
		"10/09/2026",
		"09/09/2026 14:30",
		"21/09/2026 12:00",
		"/pdf?id=" + docReceiptID + "&amp;token=" + token,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("página sem %q", expected)
		}
	}
	if !strings.Contains(body, "Parcial") {
		t.Fatal("página não marca o pagamento parcial")
	}
}

func TestVerifyReceiptPageRefusesInvalidToken(t *testing.T) {
	server := newDocumentsServer(t)

	response := server.get(t, "/validar/recibo/"+docReceiptID+"?token=00000000000000000000000000000000", "", "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, "DOCUMENTO NÃO AUTENTICADO") {
		t.Fatal("página não mostra o selo de documento não autenticado")
	}
	if !strings.Contains(body, "Chave de autenticação inválida ou documento não autenticado.") {
		t.Fatalf("página sem a mensagem do app atual: %s", body)
	}
	if strings.Contains(body, "Maria Silva") {
		t.Fatal("página expôs os dados do documento com chave inválida")
	}
}

func TestVerifyReceiptPageWithoutTokenIsRefused(t *testing.T) {
	server := newDocumentsServer(t)

	response := server.get(t, "/validar/recibo/"+docReceiptID, "", "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", response.Code)
	}
}

func TestVerifyReceiptPageNotFound(t *testing.T) {
	server := newDocumentsServer(t)
	token := server.receiptToken(authz.KindReceipt, "inexistente")

	response := server.get(t, "/validar/recibo/inexistente?token="+token, "", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404", response.Code)
	}
	if body := response.Body.String(); !strings.Contains(body, "Recibo não localizado na base oficial de registros.") {
		t.Fatalf("corpo = %q", body)
	}
}

func TestVerifyReceiptPageRefusesUnpaidReceipt(t *testing.T) {
	server := newDocumentsServer(t)
	token := server.receiptToken(authz.KindReceipt, "receipt-open")

	response := server.get(t, "/validar/recibo/receipt-open?token="+token, "", "")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403", response.Code)
	}
	if body := response.Body.String(); !strings.Contains(body, "Recibo ainda não pago. O documento só é liberado após o pagamento.") {
		t.Fatalf("corpo = %q", body)
	}
}

// ── /validar/contrato/{id} ─────────────────────────────────────────

func TestVerifyContractPageShowsTheOfficialData(t *testing.T) {
	server := newDocumentsServer(t)
	token := server.receiptToken(authz.KindLease, docLeaseID)

	response := server.get(t, "/validar/contrato/"+docLeaseID+"?token="+token, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, expected := range []string{
		"DOCUMENTO AUTÊNTICO E VÁLIDO",
		"Contrato de Locação Residencial",
		server.signer.FormatCode(token),
		"Maria Silva",
		"João Souza",
		"Casa da Praia",
		"Rua das Flores, 123, Cabo Frio, RJ",
		"R$ 1.200,50",
		"01/03/2026 até 28/02/2027",
		"Todo dia 10",
		"Ativo",
		"21/09/2026 12:00",
		"/contract-pdf?id=" + docLeaseID + "&amp;token=" + token,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("página sem %q", expected)
		}
	}

	invalid := server.get(t, "/validar/contrato/"+docLeaseID+"?token=00000000000000000000000000000000", "", "")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("chave inválida: status = %d, esperado 400", invalid.Code)
	}

	missing := server.get(t, "/validar/contrato/inexistente?token="+server.receiptToken(authz.KindLease, "inexistente"), "", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("contrato inexistente: status = %d, esperado 404", missing.Code)
	}
	if body := missing.Body.String(); !strings.Contains(body, "Contrato não localizado na base oficial de registros.") {
		t.Fatalf("corpo = %q", body)
	}
}

// A página pública não depende de sessão: o link do documento é a credencial.
func TestVerifyPagesDoNotRequireSession(t *testing.T) {
	server := newDocumentsServer(t)
	token := server.receiptToken(authz.KindReceipt, docReceiptID)

	response := server.get(t, "/validar/recibo/"+docReceiptID+"?token="+token, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if value := response.Header().Get("Content-Type"); !strings.HasPrefix(value, "text/html") {
		t.Fatalf("Content-Type = %q", value)
	}
}

// O token de formulário continua indo para páginas públicas, como no login.
func TestVerifyPageCarriesTheCSRFToken(t *testing.T) {
	server := newDocumentsServer(t)
	token := server.receiptToken(authz.KindReceipt, docReceiptID)

	response := server.get(t, "/validar/recibo/"+docReceiptID+"?token="+token, "", "")
	if cookieValue(response.Result().Cookies(), csrfCookieName) == "" {
		t.Fatal("página pública não emitiu o cookie de CSRF")
	}
}
