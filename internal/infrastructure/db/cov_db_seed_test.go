package db

import (
	"context"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
)

// Repositórios de mentira para levar o seed e a demonstração pelos caminhos de
// erro sem depender de um banco quebrado: cada operação pode ser configurada
// para falhar e provamos que o erro sobe com o contexto certo.

type covDBUserRepo struct {
	domain.UserRepository
	users      map[string]*domain.User
	errByName  map[string]error
	createErr  error
	updateErr  error
}

func (r *covDBUserRepo) ByUsername(_ context.Context, username string) (*domain.User, error) {
	if err := r.errByName[username]; err != nil {
		return nil, err
	}
	if user, ok := r.users[username]; ok {
		return user, nil
	}
	return nil, domain.ErrNotFound
}

func (r *covDBUserRepo) Create(_ context.Context, user *domain.User) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.users[user.Username] = user
	return nil
}

func (r *covDBUserRepo) UpdateUsername(_ context.Context, id, username string) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	for name, user := range r.users {
		if user.ID == id {
			delete(r.users, name)
			user.Username = username
			r.users[username] = user
			return nil
		}
	}
	return domain.ErrNotFound
}

type covDBSettingRepo struct {
	domain.SettingRepository
	values     map[string]string
	getErr     error
	failSetKey string
	setErr     error
}

func (r *covDBSettingRepo) Get(_ context.Context, key string) (string, error) {
	if r.getErr != nil {
		return "", r.getErr
	}
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", domain.ErrNotFound
}

func (r *covDBSettingRepo) Set(_ context.Context, key, value string) error {
	if r.setErr != nil && (r.failSetKey == "" || r.failSetKey == key) {
		return r.setErr
	}
	r.values[key] = value
	return nil
}

type covDBTenantRepo struct {
	domain.TenantRepository
	items     []domain.Tenant
	listErr   error
	deleteErr error
	createErr error
}

func (r *covDBTenantRepo) List(context.Context) ([]domain.Tenant, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.items, nil
}

func (r *covDBTenantRepo) Delete(_ context.Context, id string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	kept := r.items[:0]
	for _, item := range r.items {
		if item.ID != id {
			kept = append(kept, item)
		}
	}
	r.items = kept
	return nil
}

func (r *covDBTenantRepo) Create(_ context.Context, tenant *domain.Tenant) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.items = append(r.items, *tenant)
	return nil
}

type covDBPropertyRepo struct {
	domain.PropertyRepository
	items     []domain.Property
	listErr   error
	deleteErr error
	createErr error
}

func (r *covDBPropertyRepo) List(context.Context) ([]domain.Property, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.items, nil
}

func (r *covDBPropertyRepo) Delete(_ context.Context, id string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	kept := r.items[:0]
	for _, item := range r.items {
		if item.ID != id {
			kept = append(kept, item)
		}
	}
	r.items = kept
	return nil
}

func (r *covDBPropertyRepo) Create(_ context.Context, property *domain.Property) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.items = append(r.items, *property)
	return nil
}

type covDBLeaseRepo struct {
	domain.LeaseRepository
	items     []domain.Lease
	listErr   error
	deleteErr error
	createErr error
}

func (r *covDBLeaseRepo) List(context.Context) ([]domain.Lease, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.items, nil
}

func (r *covDBLeaseRepo) Delete(_ context.Context, id string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	kept := r.items[:0]
	for _, item := range r.items {
		if item.ID != id {
			kept = append(kept, item)
		}
	}
	r.items = kept
	return nil
}

func (r *covDBLeaseRepo) Create(_ context.Context, lease *domain.Lease) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.items = append(r.items, *lease)
	return nil
}

type covDBReceiptRepo struct {
	domain.ReceiptRepository
	items     []domain.Receipt
	listErr   error
	deleteErr error
	createErr error
}

func (r *covDBReceiptRepo) List(context.Context) ([]domain.Receipt, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.items, nil
}

func (r *covDBReceiptRepo) Delete(_ context.Context, id string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	kept := r.items[:0]
	for _, item := range r.items {
		if item.ID != id {
			kept = append(kept, item)
		}
	}
	r.items = kept
	return nil
}

func (r *covDBReceiptRepo) Create(_ context.Context, receipt *domain.Receipt) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.items = append(r.items, *receipt)
	return nil
}

type covDBInspectionRepo struct {
	domain.InspectionRepository
	createErr error
	items     []domain.Inspection
}

func (r *covDBInspectionRepo) Create(_ context.Context, inspection *domain.Inspection) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.items = append(r.items, *inspection)
	return nil
}

// ── montagem ───────────────────────────────────────────────────────

type covDBFakes struct {
	users        *covDBUserRepo
	settings     *covDBSettingRepo
	tenants      *covDBTenantRepo
	properties   *covDBPropertyRepo
	leases       *covDBLeaseRepo
	receipts     *covDBReceiptRepo
	inspections  *covDBInspectionRepo
}

func covDBNewFakes() *covDBFakes {
	return &covDBFakes{
		users:       &covDBUserRepo{users: map[string]*domain.User{}, errByName: map[string]error{}},
		settings:    &covDBSettingRepo{values: map[string]string{}},
		tenants:     &covDBTenantRepo{},
		properties:  &covDBPropertyRepo{},
		leases:      &covDBLeaseRepo{},
		receipts:    &covDBReceiptRepo{},
		inspections: &covDBInspectionRepo{},
	}
}

// adminJaSemeado poupa o bcrypt: o administrador existe, então EnsureAdmin só
// confere e segue para as configurações.
func (f *covDBFakes) adminJaSemeado() *covDBFakes {
	f.users.users[defaultSeedUsername] = &domain.User{ID: "user-existente", Username: defaultSeedUsername}
	return f
}

func (f *covDBFakes) store() *Store {
	return &Store{
		Dialect:     DialectSQLite,
		Users:       f.users,
		Settings:    f.settings,
		Tenants:     f.tenants,
		Properties:  f.properties,
		Leases:      f.leases,
		Receipts:    f.receipts,
		Inspections: f.inspections,
	}
}

// ── EnsureAdmin ────────────────────────────────────────────────────

func TestCovDBEnsureAdminPropagatesLookupError(t *testing.T) {
	fakes := covDBNewFakes()
	fakes.users.errByName[defaultSeedUsername] = domain.ErrUnauthorized
	if err := fakes.store().EnsureAdmin(context.Background(), defaultSeedUsername, "senha", "Locador", "000"); err == nil {
		t.Fatal("erro de leitura do administrador deveria subir")
	}
}

func TestCovDBEnsureAdminPropagatesCreateError(t *testing.T) {
	fakes := covDBNewFakes()
	fakes.users.createErr = domain.ErrConflict
	err := fakes.store().EnsureAdmin(context.Background(), "novo-admin", "senha-forte", "Locador", "000")
	if err == nil || !strings.Contains(err.Error(), "criar administrador semeado") {
		t.Fatalf("erro ao criar administrador inesperado: %v", err)
	}
}

func TestCovDBEnsureAdminPropagatesLegacyLookupError(t *testing.T) {
	fakes := covDBNewFakes()
	fakes.users.errByName[legacySeedUsername] = domain.ErrUnauthorized
	err := fakes.store().EnsureAdmin(context.Background(), "novo-admin", "senha-forte", "Locador", "000")
	if err == nil || !strings.Contains(err.Error(), "administrador legado") {
		t.Fatalf("erro de leitura do legado inesperado: %v", err)
	}
}

func TestCovDBEnsureAdminPropagatesRenameError(t *testing.T) {
	fakes := covDBNewFakes()
	fakes.users.users[legacySeedUsername] = &domain.User{ID: "user-legado", Username: legacySeedUsername}
	fakes.users.updateErr = domain.ErrConflict
	err := fakes.store().EnsureAdmin(context.Background(), "novo-admin", "senha-forte", "Locador", "000")
	if err == nil || !strings.Contains(err.Error(), "renomear administrador legado") {
		t.Fatalf("erro ao renomear inesperado: %v", err)
	}
}

func TestCovDBEnsureAdminRejectsTooLongPassword(t *testing.T) {
	// o bcrypt recusa senha acima de 72 bytes; o erro não pode ser engolido
	fakes := covDBNewFakes()
	senhaLonga := strings.Repeat("a", 100)
	err := fakes.store().EnsureAdmin(context.Background(), "novo-admin", senhaLonga, "Locador", "000")
	if err == nil || !strings.Contains(err.Error(), "hash da senha semeada") {
		t.Fatalf("senha longa demais deveria falhar no hash: %v", err)
	}
}

func TestCovDBEnsureAdminPropagatesSettingErrors(t *testing.T) {
	ctx := context.Background()

	leitura := covDBNewFakes().adminJaSemeado()
	leitura.settings.getErr = domain.ErrUnauthorized
	if err := leitura.store().EnsureAdmin(ctx, "", "", "Locador", "000"); err == nil {
		t.Fatal("erro de leitura da configuração deveria subir")
	}

	escrita := covDBNewFakes().adminJaSemeado()
	escrita.settings.setErr = domain.ErrConflict
	escrita.settings.failSetKey = domain.SettingLandlordName
	if err := escrita.store().EnsureAdmin(ctx, "", "", "Locador", "000"); err == nil {
		t.Fatal("erro ao gravar a configuração deveria subir")
	}
}

func TestCovDBEnsureAdminKeepsExistingSettings(t *testing.T) {
	ctx := context.Background()
	fakes := covDBNewFakes().adminJaSemeado()
	fakes.settings.values[domain.SettingLandlordName] = "Já configurado"
	fakes.settings.values[domain.SettingLandlordCPF] = "123"

	if err := fakes.store().EnsureAdmin(ctx, "", "", "Novo Locador", "999"); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if fakes.settings.values[domain.SettingLandlordName] != "Já configurado" {
		t.Fatal("a configuração existente não pode ser sobrescrita")
	}
}

// ── SeedDemo ───────────────────────────────────────────────────────

func TestCovDBSeedDemoPropagatesAdminError(t *testing.T) {
	fakes := covDBNewFakes()
	fakes.users.errByName[defaultSeedUsername] = domain.ErrUnauthorized
	err := SeedDemo(context.Background(), fakes.store())
	if err == nil || !strings.Contains(err.Error(), "garantir administrador") {
		t.Fatalf("erro do administrador deveria vir primeiro: %v", err)
	}
}

func TestCovDBSeedDemoPropagatesSettingsError(t *testing.T) {
	fakes := covDBNewFakes().adminJaSemeado()
	// o administrador já existe, então só a gravação da demonstração falha
	fakes.settings.setErr = domain.ErrConflict
	fakes.settings.failSetKey = domain.SettingLandlordEmail
	err := SeedDemo(context.Background(), fakes.store())
	if err == nil || !strings.Contains(err.Error(), "configuração de demonstração") {
		t.Fatalf("erro da configuração de demonstração inesperado: %v", err)
	}
}

func TestCovDBSeedDemoPropagatesClearErrors(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name   string
		config func(*covDBFakes)
		want   string
	}{
		{"listar lançamentos", func(f *covDBFakes) { f.receipts.listErr = domain.ErrConflict }, "listar lançamentos"},
		{"apagar lançamento", func(f *covDBFakes) {
			f.receipts.items = []domain.Receipt{{ID: "r1"}}
			f.receipts.deleteErr = domain.ErrConflict
		}, "apagar lançamento"},
		{"listar contratos", func(f *covDBFakes) { f.leases.listErr = domain.ErrConflict }, "listar contratos"},
		{"apagar contrato", func(f *covDBFakes) {
			f.leases.items = []domain.Lease{{ID: "l1"}}
			f.leases.deleteErr = domain.ErrConflict
		}, "apagar contrato"},
		{"listar imóveis", func(f *covDBFakes) { f.properties.listErr = domain.ErrConflict }, "listar imóveis"},
		{"apagar imóvel", func(f *covDBFakes) {
			f.properties.items = []domain.Property{{ID: "p1"}}
			f.properties.deleteErr = domain.ErrConflict
		}, "apagar imóvel"},
		{"listar locatários", func(f *covDBFakes) { f.tenants.listErr = domain.ErrConflict }, "listar locatários"},
		{"apagar locatário", func(f *covDBFakes) {
			f.tenants.items = []domain.Tenant{{ID: "t1"}}
			f.tenants.deleteErr = domain.ErrConflict
		}, "apagar locatário"},
	}
	for _, tc := range cases {
		fakes := covDBNewFakes().adminJaSemeado()
		tc.config(fakes)
		err := SeedDemo(ctx, fakes.store())
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: erro inesperado: %v", tc.name, err)
		}
	}
}

func TestCovDBSeedDemoPropagatesCreateErrors(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name   string
		config func(*covDBFakes)
		want   string
	}{
		{"semear locatário", func(f *covDBFakes) { f.tenants.createErr = domain.ErrConflict }, "semear locatário"},
		{"semear imóvel", func(f *covDBFakes) { f.properties.createErr = domain.ErrConflict }, "semear imóvel"},
		{"semear contrato", func(f *covDBFakes) { f.leases.createErr = domain.ErrConflict }, "semear contrato"},
		{"semear lançamento", func(f *covDBFakes) { f.receipts.createErr = domain.ErrConflict }, "semear lançamento"},
		{"semear vistoria", func(f *covDBFakes) { f.inspections.createErr = domain.ErrConflict }, "semear vistoria"},
	}
	for _, tc := range cases {
		fakes := covDBNewFakes().adminJaSemeado()
		tc.config(fakes)
		err := SeedDemo(ctx, fakes.store())
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: erro inesperado: %v", tc.name, err)
		}
	}
}

func TestCovDBSeedDemoRebuildsTheCatalog(t *testing.T) {
	ctx := context.Background()
	fakes := covDBNewFakes().adminJaSemeado()
	fakes.tenants.items = []domain.Tenant{{ID: "antigo"}}
	fakes.properties.items = []domain.Property{{ID: "antigo"}}
	fakes.leases.items = []domain.Lease{{ID: "antigo"}}
	fakes.receipts.items = []domain.Receipt{{ID: "antigo"}}

	if err := SeedDemo(ctx, fakes.store()); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}
	if len(fakes.tenants.items) != 5 || len(fakes.properties.items) != 5 || len(fakes.leases.items) != 5 || len(fakes.receipts.items) != 6 {
		t.Fatalf("catálogo de demonstração incompleto: %d/%d/%d/%d",
			len(fakes.tenants.items), len(fakes.properties.items), len(fakes.leases.items), len(fakes.receipts.items))
	}
	if len(fakes.inspections.items) != 1 {
		t.Fatalf("a vistoria de entrada deveria existir: %d", len(fakes.inspections.items))
	}
}
