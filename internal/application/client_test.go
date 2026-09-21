package application

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/domain"
)

// ── Dublês da Área do Cliente ──────────────────────────────────────
//
// Repositórios em memória: as consultas por titular devolvem apenas as linhas
// do titular pedido, como o `WHERE tenant_id = ?` (ou o `OR` do contrato) das
// queries do port.

type clientFakeTenants struct{ byID map[string]*domain.Tenant }

func newClientFakeTenants(tenants ...*domain.Tenant) *clientFakeTenants {
	repo := &clientFakeTenants{byID: map[string]*domain.Tenant{}}
	for _, tenant := range tenants {
		repo.byID[tenant.ID] = tenant
	}
	return repo
}

func (r *clientFakeTenants) List(context.Context) ([]domain.Tenant, error) {
	tenants := make([]domain.Tenant, 0, len(r.byID))
	for _, tenant := range r.byID {
		tenants = append(tenants, *tenant)
	}
	return tenants, nil
}

func (r *clientFakeTenants) ByID(_ context.Context, id string) (*domain.Tenant, error) {
	tenant, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return tenant, nil
}

func (r *clientFakeTenants) ByCPF(_ context.Context, digits string) (*domain.Tenant, error) {
	for _, tenant := range r.byID {
		if cpf := onlyDigits(domain.Str(tenant.CPF)); cpf != "" && cpf == digits {
			return tenant, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *clientFakeTenants) Create(_ context.Context, tenant *domain.Tenant) error {
	r.byID[tenant.ID] = tenant
	return nil
}

func (r *clientFakeTenants) Update(context.Context, *domain.Tenant) error { return nil }

func (r *clientFakeTenants) UpdatePassword(_ context.Context, id, passwordHash, changedAt, updatedAt string) error {
	tenant, ok := r.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	tenant.PasswordHash = &passwordHash
	tenant.PasswordChangedAt = &changedAt
	tenant.UpdatedAt = updatedAt
	return nil
}

func (r *clientFakeTenants) Delete(context.Context, string) error { return nil }

func (r *clientFakeTenants) Count(context.Context) (int, error) { return len(r.byID), nil }

type clientFakeProperties struct{ byID map[string]*domain.Property }

func newClientFakeProperties(properties ...*domain.Property) *clientFakeProperties {
	repo := &clientFakeProperties{byID: map[string]*domain.Property{}}
	for _, property := range properties {
		repo.byID[property.ID] = property
	}
	return repo
}

func (r *clientFakeProperties) List(context.Context) ([]domain.Property, error) { return nil, nil }

func (r *clientFakeProperties) ByID(_ context.Context, id string) (*domain.Property, error) {
	property, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return property, nil
}

func (r *clientFakeProperties) Create(context.Context, *domain.Property) error { return nil }
func (r *clientFakeProperties) Update(context.Context, *domain.Property) error { return nil }
func (r *clientFakeProperties) Delete(context.Context, string) error           { return nil }
func (r *clientFakeProperties) Count(context.Context) (int, error)             { return len(r.byID), nil }

type clientFakeLeases struct{ byTenant map[string][]domain.Lease }

func newClientFakeLeases(leases ...domain.Lease) *clientFakeLeases {
	repo := &clientFakeLeases{byTenant: map[string][]domain.Lease{}}
	for _, lease := range leases {
		repo.byTenant[lease.TenantID] = append(repo.byTenant[lease.TenantID], lease)
	}
	return repo
}

func (r *clientFakeLeases) List(context.Context) ([]domain.Lease, error) { return nil, nil }

func (r *clientFakeLeases) ByID(_ context.Context, id string) (*domain.Lease, error) {
	for _, leases := range r.byTenant {
		for index := range leases {
			if leases[index].ID == id {
				return &leases[index], nil
			}
		}
	}
	return nil, domain.ErrNotFound
}

func (r *clientFakeLeases) ByTenant(_ context.Context, tenantID string) ([]domain.Lease, error) {
	return r.byTenant[tenantID], nil
}

func (r *clientFakeLeases) ByProperty(context.Context, string) ([]domain.Lease, error) {
	return nil, nil
}

func (r *clientFakeLeases) Overlapping(context.Context, string, string, string, string) ([]domain.Lease, error) {
	return nil, nil
}

func (r *clientFakeLeases) ActiveInRange(context.Context, string, string) ([]domain.Lease, error) {
	return nil, nil
}

func (r *clientFakeLeases) Create(context.Context, *domain.Lease) error { return nil }
func (r *clientFakeLeases) Update(context.Context, *domain.Lease) error { return nil }

func (r *clientFakeLeases) UpdateStatus(context.Context, string, domain.LeaseStatus, string) error {
	return nil
}

func (r *clientFakeLeases) Delete(context.Context, string) error { return nil }

func (r *clientFakeLeases) Count(context.Context) (int, error) {
	total := 0
	for _, leases := range r.byTenant {
		total += len(leases)
	}
	return total, nil
}

func (r *clientFakeLeases) CountByStatus(context.Context, domain.LeaseStatus) (int, error) {
	return 0, nil
}

type clientFakeReceipts struct{ byTenant map[string][]domain.Receipt }

func newClientFakeReceipts(receipts ...domain.Receipt) *clientFakeReceipts {
	repo := &clientFakeReceipts{byTenant: map[string][]domain.Receipt{}}
	for _, receipt := range receipts {
		tenantID := domain.Str(receipt.TenantID)
		repo.byTenant[tenantID] = append(repo.byTenant[tenantID], receipt)
	}
	return repo
}

func (r *clientFakeReceipts) List(context.Context) ([]domain.Receipt, error) { return nil, nil }

func (r *clientFakeReceipts) ByID(_ context.Context, id string) (*domain.Receipt, error) {
	for _, receipts := range r.byTenant {
		for index := range receipts {
			if receipts[index].ID == id {
				return &receipts[index], nil
			}
		}
	}
	return nil, domain.ErrNotFound
}

func (r *clientFakeReceipts) ByTenant(_ context.Context, tenantID string) ([]domain.Receipt, error) {
	return r.byTenant[tenantID], nil
}

func (r *clientFakeReceipts) ByLease(context.Context, string) ([]domain.Receipt, error) {
	return nil, nil
}

func (r *clientFakeReceipts) Create(context.Context, *domain.Receipt) error { return nil }

func (r *clientFakeReceipts) CreateIfAbsent(context.Context, *domain.Receipt) (int64, error) {
	return 0, nil
}

func (r *clientFakeReceipts) Update(context.Context, *domain.Receipt) error { return nil }

func (r *clientFakeReceipts) UpdatePayment(context.Context, string, string, float64, string) error {
	return nil
}

func (r *clientFakeReceipts) Delete(context.Context, string) error { return nil }

func (r *clientFakeReceipts) Count(context.Context) (int, error) {
	total := 0
	for _, receipts := range r.byTenant {
		total += len(receipts)
	}
	return total, nil
}

// clientFakeSettings não tem nenhuma configuração gravada: a base pública cai
// para `Options.PublicBaseURL`.
type clientFakeSettings struct{}

func (clientFakeSettings) Get(context.Context, string) (string, error) { return "", domain.ErrNotFound }

func (clientFakeSettings) List(context.Context) (map[string]string, error) { return nil, nil }

func (clientFakeSettings) Set(context.Context, string, string) error { return nil }

// clientFakeAudit descarta a trilha: os casos de uso do portal são somente
// leitura e a gravação é exercitada nos testes de handler.
type clientFakeAudit struct{}

func (clientFakeAudit) Insert(context.Context, *domain.AuditLog) error { return nil }

func (clientFakeAudit) List(context.Context, domain.AuditFilter) ([]domain.AuditLog, error) {
	return nil, nil
}

func (clientFakeAudit) Count(context.Context, domain.AuditFilter) (int, error) { return 0, nil }

func (clientFakeAudit) Actors(context.Context) ([]string, error) { return nil, nil }

// clientFakeSigner emite chaves determinísticas, no formato da porta e com os
// caminhos por tipo do gerador de produção (`recibo`/`contrato`).
type clientFakeSigner struct{}

func (clientFakeSigner) Generate(kind, id string) string { return kind + "-token-" + id }

func (clientFakeSigner) FormatCode(token string) string { return "AUTH-" + strings.ToUpper(token) }

func (clientFakeSigner) ValidationURL(base, kind, id, token string) string {
	path := "recibo"
	if kind == "lease" {
		path = "contrato"
	}
	return strings.TrimSuffix(base, "/") + "/validar/" + path + "/" + id + "?token=" + token
}

// clientFakeHasher compara o prefixo `hash:` (rápido e suficiente aqui).
type clientFakeHasher struct{}

func (clientFakeHasher) Hash(password string) (string, error) { return "hash:" + password, nil }

func (clientFakeHasher) Compare(hash, password string) error {
	if hash == "hash:"+password {
		return nil
	}
	return domain.ErrUnauthorized
}

func onlyDigits(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ── Cenário ────────────────────────────────────────────────────────

const (
	clientTenantA = "tenant-a"
	clientTenantB = "tenant-b"
)

type clientFixture struct {
	deps    Deps
	tenants *clientFakeTenants
}

// newClientFixture monta dois titulares com contratos, recibos (quitados, em
// aberto e avulsos) e imóveis próprios — o cenário do isolamento.
func newClientFixture() *clientFixture {
	tcpfA := "123.456.789-00"
	cpfB := "987.654.321-00"
	hashB := "hash:segredo-b"
	changedAt := "2026-08-01 10:00:00"

	changedA := "2026-08-02 10:00:00"
	tenants := newClientFakeTenants(
		&domain.Tenant{ID: clientTenantA, Name: "Ana Alves", CPF: &tcpfA, Email: "ana@exemplo.com", PasswordChangedAt: &changedA},
		&domain.Tenant{
			ID: clientTenantB, Name: "Bruno Barros", CPF: &cpfB, Email: "bruno@exemplo.com",
			PasswordHash: &hashB, PasswordChangedAt: &changedAt,
		},
	)

	properties := newClientFakeProperties(
		&domain.Property{ID: "prop-a", Name: "Casa A", Address: "Rua Um, 100", City: "Curitiba", State: "PR"},
		&domain.Property{ID: "prop-b", Name: "Casa B", Address: "Rua Dois, 200", City: "Curitiba", State: "PR"},
	)

	leaseIDA, leaseIDB := "lease-a", "lease-b"
	receiptAPaid, receiptAPartial, receiptAOpen, receiptAExtra := "receipt-a-paid", "receipt-a-partial", "receipt-a-open", "receipt-a-extra"
	receiptBPaid := "receipt-b-paid"

	leases := newClientFakeLeases(
		domain.Lease{
			ID: leaseIDA, TenantID: clientTenantA, PropertyID: "prop-a", StartDate: "2026-01-01", EndDate: "2026-12-31",
			DueDay: 10, RentAmount: 1500, Status: domain.LeaseStatusActive, PropertyName: "Casa A",
		},
		domain.Lease{
			ID: leaseIDB, TenantID: clientTenantB, PropertyID: "prop-b", StartDate: "2026-03-01", EndDate: "2027-02-28",
			DueDay: 5, RentAmount: 2400, Status: domain.LeaseStatusTerminated, PropertyName: "Casa B",
		},
	)

	paidAt := func(value string) *string { return &value }
	paidAmount := func(value float64) *float64 { return &value }
	description := func(value string) *string { return &value }
	tenantARef, tenantBRef := clientTenantA, clientTenantB
	leaseARef, leaseBRef := leaseIDA, leaseIDB
	propARef, propBRef := "prop-a", "prop-b"

	receipts := newClientFakeReceipts(
		domain.Receipt{
			ID: receiptAPaid, LeaseID: &leaseARef, TenantID: &tenantARef, PropertyID: nil, Category: domain.ReceiptCategoryRent,
			DueDate: "2026-09-10", PaidAt: paidAt("2026-09-08T10:15"), Amount: 1500, PropertyName: "Casa A",
		},
		domain.Receipt{
			ID: receiptAPartial, LeaseID: &leaseARef, TenantID: &tenantARef, Category: domain.ReceiptCategoryRent,
			DueDate: "2026-08-10", PaidAt: paidAt("2026-08-10T09:30"), Amount: 1500, PaidAmount: paidAmount(750),
			PropertyName: "Casa A",
		},
		domain.Receipt{
			ID: receiptAOpen, LeaseID: &leaseARef, TenantID: &tenantARef, Category: domain.ReceiptCategoryRent,
			DueDate: "2026-10-10", Amount: 1500, PropertyName: "Casa A",
		},
		domain.Receipt{
			ID: receiptAExtra, TenantID: &tenantARef, PropertyID: &propARef, Category: domain.ReceiptCategoryExtra,
			Description: description("Conserto da torneira"), DueDate: "2026-07-01", PaidAt: paidAt("2026-07-01T12:00"),
			Amount: 200, PropertyName: "Casa A",
		},
		domain.Receipt{
			ID: receiptBPaid, LeaseID: &leaseBRef, TenantID: &tenantBRef, PropertyID: &propBRef, Category: domain.ReceiptCategoryRent,
			DueDate: "2026-09-05", PaidAt: paidAt("2026-09-05T08:00"), Amount: 2400, PropertyName: "Casa B",
		},
	)

	deps := Deps{
		Repos: Repositories{
			Tenants:    tenants,
			Properties: properties,
			Leases:     leases,
			Receipts:   receipts,
			Settings:   clientFakeSettings{},
			Audit:      clientFakeAudit{},
		},
		Clock:  &fakeClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)},
		Hasher: clientFakeHasher{},
		Signer: clientFakeSigner{},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Opts:   Options{PublicBaseURL: "https://aluxen.test"},
	}
	return &clientFixture{deps: deps, tenants: tenants}
}

// ── Login do locatário ─────────────────────────────────────────────

func TestClientLoginAcceptsCPFAsDefaultPassword(t *testing.T) {
	fixture := newClientFixture()

	for _, attempt := range []struct{ identifier, password string }{
		{"123.456.789-00", "12345678900"},
		{"12345678900", "123.456.789-00"},
	} {
		tenant, err := ClientLogin(context.Background(), fixture.deps, attempt.identifier, attempt.password)
		if err != nil {
			t.Fatalf("login %q/%q falhou: %v", attempt.identifier, attempt.password, err)
		}
		if tenant.ID != clientTenantA {
			t.Fatalf("titular = %q, esperado %q", tenant.ID, clientTenantA)
		}
	}
}

func TestClientLoginRejectsWrongCredentials(t *testing.T) {
	fixture := newClientFixture()

	for _, attempt := range []struct{ identifier, password, reason string }{
		{"123.456.789-00", "outra-senha", "senha diferente do CPF"},
		{"123.456.789-00", "", "senha vazia"},
		{"1234567890", "1234567890", "CPF incompleto"},
		{"999.999.999-99", "99999999999", "CPF sem cadastro"},
		{"111.222.333-44", "12345678900", "CPF de outro titular"},
	} {
		if _, err := ClientLogin(context.Background(), fixture.deps, attempt.identifier, attempt.password); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("%s: err = %v, esperado ErrUnauthorized", attempt.reason, err)
		}
	}
}

func TestClientLoginAcceptsOwnPasswordAfterChange(t *testing.T) {
	fixture := newClientFixture()
	ctx := context.Background()

	changedAt := "2026-09-01 08:00:00"
	if err := fixture.tenants.UpdatePassword(ctx, clientTenantA, "hash:nova-senha", changedAt, changedAt); err != nil {
		t.Fatalf("gravar senha: %v", err)
	}

	if _, err := ClientLogin(ctx, fixture.deps, "123.456.789-00", "nova-senha"); err != nil {
		t.Fatalf("login com a senha própria falhou: %v", err)
	}
	// Com senha própria cadastrada, o CPF deixa de valer como senha.
	if _, err := ClientLogin(ctx, fixture.deps, "123.456.789-00", "12345678900"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("CPF não pode continuar valendo como senha, err = %v", err)
	}
}

func TestClientMustChangePasswordFollowsFirstAccess(t *testing.T) {
	fixture := newClientFixture()
	ctx := context.Background()

	mustChange, err := ClientMustChangePassword(ctx, fixture.deps, clientTenantA)
	if err != nil || mustChange {
		t.Fatalf("titular com senha própria: mustChange = %v, err = %v", mustChange, err)
	}

	cpf := "321.654.987-00"
	fresh := &domain.Tenant{ID: "tenant-novo", Name: "Novo", CPF: &cpf}
	if err := fixture.tenants.Create(ctx, fresh); err != nil {
		t.Fatalf("criar titular: %v", err)
	}
	mustChange, err = ClientMustChangePassword(ctx, fixture.deps, fresh.ID)
	if err != nil || !mustChange {
		t.Fatalf("primeiro acesso: mustChange = %v, err = %v", mustChange, err)
	}
}

// ── Troca de senha do locatário ────────────────────────────────────

func TestClientChangePasswordAcceptsCPFAsCurrentOnFirstAccess(t *testing.T) {
	fixture := newClientFixture()
	ctx := context.Background()

	cpf := "321.654.987-00"
	fresh := &domain.Tenant{ID: "tenant-novo", Name: "Novo", CPF: &cpf}
	if err := fixture.tenants.Create(ctx, fresh); err != nil {
		t.Fatalf("criar titular: %v", err)
	}

	// A senha atual no primeiro acesso é o CPF (com ou sem pontuação) e a nova
	// senha passa a valer no login seguinte.
	if err := ClientChangePassword(ctx, fixture.deps, fresh.ID, "321.654.987-00", "  senha-nova  ", "  senha-nova  "); err != nil {
		t.Fatalf("troca no primeiro acesso falhou: %v", err)
	}
	if _, err := ClientLogin(ctx, fixture.deps, cpf, "senha-nova"); err != nil {
		t.Fatalf("login com a senha nova falhou: %v", err)
	}
	if _, err := ClientLogin(ctx, fixture.deps, cpf, "32165498700"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("CPF não pode continuar valendo como senha, err = %v", err)
	}
}

func TestClientChangePasswordRejectsCPFAsNewPassword(t *testing.T) {
	fixture := newClientFixture()

	err := ClientChangePassword(context.Background(), fixture.deps, clientTenantA, "12345678900", "123.456.789-00", "123.456.789-00")
	if !domain.IsValidation(err) {
		t.Fatalf("err = %v, esperado erro de validação", err)
	}
	if message := domain.ValidationMessage(err); message != "A nova senha não pode ser o seu CPF." {
		t.Fatalf("mensagem = %q", message)
	}
}

func TestClientChangePasswordRejectsInvalidInput(t *testing.T) {
	fixture := newClientFixture()
	ctx := context.Background()

	cases := []struct {
		name                                    string
		currentPassword, newPassword, confirmed string
		message                                 string
	}{
		{"senha atual incorreta", "errada", "senha-nova", "senha-nova", "Senha atual incorreta."},
		{"confirmação divergente", "12345678900", "senha-nova", "outra-senha", "A confirmação não corresponde à nova senha."},
		{"senha curta", "12345678900", "curta", "curta", "A nova senha deve ter no mínimo 6 caracteres."},
	}
	for _, testCase := range cases {
		err := ClientChangePassword(ctx, fixture.deps, clientTenantA, testCase.currentPassword, testCase.newPassword, testCase.confirmed)
		if !domain.IsValidation(err) {
			t.Fatalf("%s: err = %v, esperado erro de validação", testCase.name, err)
		}
		if message := domain.ValidationMessage(err); message != testCase.message {
			t.Fatalf("%s: mensagem = %q, esperada %q", testCase.name, message, testCase.message)
		}
	}
}

// ── Área do Cliente ────────────────────────────────────────────────

func TestLoadClientAreaReturnsOnlyTheTenantPaidReceipts(t *testing.T) {
	fixture := newClientFixture()

	area, err := LoadClientArea(context.Background(), fixture.deps, clientTenantA)
	if err != nil {
		t.Fatalf("carregar área: %v", err)
	}
	if area.Tenant == nil || area.Tenant.ID != clientTenantA {
		t.Fatalf("titular da área = %+v", area.Tenant)
	}

	byID := map[string]ClientReceiptView{}
	for _, receipt := range area.Receipts {
		byID[receipt.ID] = receipt
	}
	if _, ok := byID["receipt-a-open"]; ok {
		t.Fatal("recibo em aberto não pode aparecer na Área do Cliente")
	}
	if _, ok := byID["receipt-b-paid"]; ok {
		t.Fatal("recibo de outro titular não pode aparecer na Área do Cliente")
	}
	if len(byID) != 3 {
		t.Fatalf("recibos quitados = %d, esperado 3", len(byID))
	}

	paid := byID["receipt-a-paid"]
	if paid.Reference != "Aluguel 09/2026" {
		t.Fatalf("referência = %q, esperada \"Aluguel 09/2026\"", paid.Reference)
	}
	if paid.AmountPaid != "R$ 1.500,00" {
		t.Fatalf("valor pago = %q", paid.AmountPaid)
	}
	if paid.IsPartial {
		t.Fatal("pagamento integral marcado como parcial")
	}
	if paid.DueDate != "10/09/2026" || paid.PaidAt != "08/09/2026 10:15" {
		t.Fatalf("datas = %q / %q", paid.DueDate, paid.PaidAt)
	}

	partial := byID["receipt-a-partial"]
	if partial.AmountPaid != "R$ 750,00" || !partial.IsPartial {
		t.Fatalf("parcial = %q isPartial=%v", partial.AmountPaid, partial.IsPartial)
	}

	extra := byID["receipt-a-extra"]
	if extra.Reference != "Conserto da torneira" {
		t.Fatalf("referência avulsa = %q", extra.Reference)
	}
}

func TestLoadClientAreaDocumentsCarryTokenAndLinks(t *testing.T) {
	fixture := newClientFixture()
	signer := clientFakeSigner{}

	area, err := LoadClientArea(context.Background(), fixture.deps, clientTenantA)
	if err != nil {
		t.Fatalf("carregar área: %v", err)
	}
	if len(area.Leases) != 1 {
		t.Fatalf("contratos = %d, esperado 1", len(area.Leases))
	}

	lease := area.Leases[0]
	if lease.PDFURL != "/contract-pdf?id=lease-a" {
		t.Fatalf("PDF do contrato = %q", lease.PDFURL)
	}
	expectedLeaseURL := signer.ValidationURL("https://aluxen.test", "lease", "lease-a", signer.Generate("lease", "lease-a"))
	if lease.ValidationURL != expectedLeaseURL {
		t.Fatalf("validação do contrato = %q, esperada %q", lease.ValidationURL, expectedLeaseURL)
	}

	var paid ClientReceiptView
	for _, receipt := range area.Receipts {
		if receipt.ID == "receipt-a-paid" {
			paid = receipt
		}
	}
	if paid.PDFURL != "/pdf?id=receipt-a-paid" {
		t.Fatalf("PDF do recibo = %q", paid.PDFURL)
	}
	expectedReceiptURL := signer.ValidationURL("https://aluxen.test", "receipt", "receipt-a-paid", signer.Generate("receipt", "receipt-a-paid"))
	if paid.ValidationURL != expectedReceiptURL {
		t.Fatalf("validação do recibo = %q, esperada %q", paid.ValidationURL, expectedReceiptURL)
	}
}

func TestLoadClientAreaFormatsContracts(t *testing.T) {
	fixture := newClientFixture()

	area, err := LoadClientArea(context.Background(), fixture.deps, clientTenantA)
	if err != nil {
		t.Fatalf("carregar área: %v", err)
	}
	lease := area.Leases[0]
	if lease.PropertyName != "Casa A" {
		t.Fatalf("imóvel = %q", lease.PropertyName)
	}
	if lease.PropertyAddress != "Rua Um, 100, Curitiba, PR" {
		t.Fatalf("endereço = %q", lease.PropertyAddress)
	}
	if lease.Period != "01/01/2026 a 31/12/2026" {
		t.Fatalf("vigência = %q", lease.Period)
	}
	if lease.RentAmount != "R$ 1.500,00" || lease.DueDay != 10 {
		t.Fatalf("aluguel = %q dia %d", lease.RentAmount, lease.DueDay)
	}
	if lease.StatusLabel != "Ativo" || lease.StatusClass != "paid" {
		t.Fatalf("situação = %q classe %q", lease.StatusLabel, lease.StatusClass)
	}
}

func TestLoadClientAreaIsolatesTenants(t *testing.T) {
	fixture := newClientFixture()

	area, err := LoadClientArea(context.Background(), fixture.deps, clientTenantB)
	if err != nil {
		t.Fatalf("carregar área: %v", err)
	}
	if len(area.Leases) != 1 || area.Leases[0].ID != "lease-b" {
		t.Fatalf("contratos do titular B = %+v", area.Leases)
	}
	if area.Leases[0].StatusClass != "terminated" || area.Leases[0].StatusLabel != "Encerrado" {
		t.Fatalf("situação = %q classe %q", area.Leases[0].StatusLabel, area.Leases[0].StatusClass)
	}
	if len(area.Receipts) != 1 || area.Receipts[0].ID != "receipt-b-paid" {
		t.Fatalf("recibos do titular B = %+v", area.Receipts)
	}
}

func TestLoadClientAreaRejectsUnknownTenant(t *testing.T) {
	fixture := newClientFixture()

	if _, err := LoadClientArea(context.Background(), fixture.deps, "tenant-inexistente"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("err = %v, esperado ErrUnauthorized", err)
	}
}

// O isolamento vale também para o download: o locatário só abre o documento em
// que é titular, mesmo conhecendo o id do documento do outro.
func TestClientDocumentsAreRestrictedToTheOwner(t *testing.T) {
	fixture := newClientFixture()
	ctx := context.Background()

	allowed, err := CanAccessReceipt(ctx, fixture.deps, "receipt-a-paid", "", clientTenantA)
	if err != nil || !allowed {
		t.Fatalf("titular do recibo: allowed = %v, err = %v", allowed, err)
	}
	allowed, err = CanAccessReceipt(ctx, fixture.deps, "receipt-a-paid", "", clientTenantB)
	if err != nil || allowed {
		t.Fatalf("outro titular: allowed = %v, err = %v", allowed, err)
	}

	allowed, err = CanAccessLease(ctx, fixture.deps, "lease-a", "", clientTenantA)
	if err != nil || !allowed {
		t.Fatalf("titular do contrato: allowed = %v, err = %v", allowed, err)
	}
	allowed, err = CanAccessLease(ctx, fixture.deps, "lease-a", "", clientTenantB)
	if err != nil || allowed {
		t.Fatalf("outro titular no contrato: allowed = %v, err = %v", allowed, err)
	}
}
