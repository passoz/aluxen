package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/buildinfo"
	"github.com/passoz/aluxen/internal/infrastructure/config"
)

// ── Dublês (prefixo cov) ───────────────────────────────────────────
//
// Harness próprio dos testes novos de cobertura: nenhum dublê de outro arquivo
// é reaproveitado, para que os testes daqui não dependam do harness alheio.
// Todo repositório aceita `err` injetado, para exercitar os ramos de erro dos
// handlers.

type covClock struct{ now time.Time }

func (c *covClock) Now() time.Time { return c.now }

type covIDs struct{ next int }

func (g *covIDs) New() string {
	g.next++
	return fmt.Sprintf("cov-%02d", g.next)
}

type covHasher struct{}

func (covHasher) Hash(password string) (string, error) { return "covhash:" + password, nil }

func (covHasher) Compare(hash, password string) error {
	if hash == "covhash:"+password {
		return nil
	}
	return domain.ErrUnauthorized
}

type covSigner struct{}

func (covSigner) Generate(kind, id string) string { return "covtoken-" + kind + "-" + id }

func (covSigner) FormatCode(token string) string { return token }

func (covSigner) ValidationURL(base, kind, id, token string) string {
	return base + "/validar/" + kind + "/" + id + "?token=" + token
}

type covAttachments struct {
	files map[string][]byte
	err   error
}

func newCovAttachments() *covAttachments {
	return &covAttachments{files: map[string][]byte{}}
}

func (r *covAttachments) Save(prefix, filename string, data []byte) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	name := prefix + "/" + filename
	r.files[name] = data
	return name, nil
}

func (r *covAttachments) Open(name string) ([]byte, error) {
	data, ok := r.files[name]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return data, nil
}

func (r *covAttachments) Delete(name string) error {
	delete(r.files, name)
	return nil
}

// covLogHandler guarda os registros de log para inspeção dos testes.
type covLogHandler struct {
	mutex   sync.Mutex
	records []slog.Record
}

func (h *covLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *covLogHandler) Handle(_ context.Context, record slog.Record) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.records = append(h.records, record.Clone())
	return nil
}

func (h *covLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *covLogHandler) WithGroup(string) slog.Handler { return h }

func (h *covLogHandler) last() (slog.Record, bool) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if len(h.records) == 0 {
		return slog.Record{}, false
	}
	return h.records[len(h.records)-1], true
}

// ── Repositórios ───────────────────────────────────────────────────

type covUsers struct {
	items  map[string]*domain.User
	err    error
	byName error
}

func newCovUsers(users ...*domain.User) *covUsers {
	repo := &covUsers{items: map[string]*domain.User{}}
	for _, user := range users {
		repo.items[user.ID] = user
	}
	return repo
}

func (r *covUsers) ByID(_ context.Context, id string) (*domain.User, error) {
	if r.err != nil {
		return nil, r.err
	}
	user, ok := r.items[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return user, nil
}

func (r *covUsers) ByUsername(_ context.Context, username string) (*domain.User, error) {
	if r.err != nil {
		return nil, r.err
	}
	for _, user := range r.items {
		if user.Username == username {
			return user, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covUsers) Create(_ context.Context, user *domain.User) error {
	r.items[user.ID] = user
	return nil
}

func (r *covUsers) UpdateUsername(_ context.Context, id, username string) error {
	user, ok := r.items[id]
	if !ok {
		return domain.ErrNotFound
	}
	user.Username = username
	return nil
}

func (r *covUsers) UpdatePassword(_ context.Context, id, passwordHash, changedAt string) error {
	user, ok := r.items[id]
	if !ok {
		return domain.ErrNotFound
	}
	user.PasswordHash = passwordHash
	user.PasswordChangedAt = &changedAt
	return nil
}

type covTenants struct {
	items []domain.Tenant
	err   error
}

func (r *covTenants) List(context.Context) ([]domain.Tenant, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.items, nil
}

func (r *covTenants) ByID(_ context.Context, id string) (*domain.Tenant, error) {
	if r.err != nil {
		return nil, r.err
	}
	for index := range r.items {
		if r.items[index].ID == id {
			return &r.items[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covTenants) ByCPF(_ context.Context, digits string) (*domain.Tenant, error) {
	for index := range r.items {
		if r.items[index].CPF != nil && *r.items[index].CPF == digits {
			return &r.items[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covTenants) Create(_ context.Context, tenant *domain.Tenant) error {
	r.items = append(r.items, *tenant)
	return nil
}

func (r *covTenants) Update(_ context.Context, tenant *domain.Tenant) error {
	for index := range r.items {
		if r.items[index].ID == tenant.ID {
			r.items[index] = *tenant
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *covTenants) UpdatePassword(_ context.Context, id, hash, changedAt, updatedAt string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items[index].PasswordHash = &hash
			r.items[index].PasswordChangedAt = &changedAt
			r.items[index].UpdatedAt = updatedAt
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *covTenants) Delete(_ context.Context, id string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *covTenants) Count(context.Context) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	return len(r.items), nil
}

type covProperties struct {
	items []domain.Property
	err   error
}

func (r *covProperties) List(context.Context) ([]domain.Property, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.items, nil
}

func (r *covProperties) ByID(_ context.Context, id string) (*domain.Property, error) {
	if r.err != nil {
		return nil, r.err
	}
	for index := range r.items {
		if r.items[index].ID == id {
			return &r.items[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covProperties) Create(_ context.Context, property *domain.Property) error {
	r.items = append(r.items, *property)
	return nil
}

func (r *covProperties) Update(_ context.Context, property *domain.Property) error {
	for index := range r.items {
		if r.items[index].ID == property.ID {
			r.items[index] = *property
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *covProperties) Delete(_ context.Context, id string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *covProperties) Count(context.Context) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	return len(r.items), nil
}

type covLeases struct {
	items []domain.Lease
	err   error
}

func (r *covLeases) List(context.Context) ([]domain.Lease, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.items, nil
}

func (r *covLeases) ByID(_ context.Context, id string) (*domain.Lease, error) {
	if r.err != nil {
		return nil, r.err
	}
	for index := range r.items {
		if r.items[index].ID == id {
			return &r.items[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covLeases) ByTenant(_ context.Context, tenantID string) ([]domain.Lease, error) {
	filtered := make([]domain.Lease, 0, len(r.items))
	for _, lease := range r.items {
		if lease.TenantID == tenantID {
			filtered = append(filtered, lease)
		}
	}
	return filtered, nil
}

func (r *covLeases) ByProperty(_ context.Context, propertyID string) ([]domain.Lease, error) {
	filtered := make([]domain.Lease, 0, len(r.items))
	for _, lease := range r.items {
		if lease.PropertyID == propertyID {
			filtered = append(filtered, lease)
		}
	}
	return filtered, nil
}

func (r *covLeases) Overlapping(context.Context, string, string, string, string) ([]domain.Lease, error) {
	return nil, nil
}

func (r *covLeases) ActiveInRange(context.Context, string, string) ([]domain.Lease, error) {
	return r.items, nil
}

func (r *covLeases) Create(_ context.Context, lease *domain.Lease) error {
	r.items = append(r.items, *lease)
	return nil
}

func (r *covLeases) Update(_ context.Context, lease *domain.Lease) error {
	if r.err != nil {
		return r.err
	}
	for index := range r.items {
		if r.items[index].ID == lease.ID {
			r.items[index] = *lease
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *covLeases) UpdateStatus(_ context.Context, id string, status domain.LeaseStatus, updatedAt string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items[index].Status = status
			r.items[index].UpdatedAt = updatedAt
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *covLeases) Delete(_ context.Context, id string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *covLeases) Count(context.Context) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	return len(r.items), nil
}

func (r *covLeases) CountByStatus(_ context.Context, status domain.LeaseStatus) (int, error) {
	count := 0
	for _, lease := range r.items {
		if lease.Status == status {
			count++
		}
	}
	return count, nil
}

type covReceipts struct {
	items []domain.Receipt
	err   error
}

func (r *covReceipts) List(context.Context) ([]domain.Receipt, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.items, nil
}

func (r *covReceipts) ByID(_ context.Context, id string) (*domain.Receipt, error) {
	if r.err != nil {
		return nil, r.err
	}
	for index := range r.items {
		if r.items[index].ID == id {
			return &r.items[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covReceipts) ByTenant(_ context.Context, tenantID string) ([]domain.Receipt, error) {
	filtered := make([]domain.Receipt, 0, len(r.items))
	for _, receipt := range r.items {
		if receipt.TenantID != nil && *receipt.TenantID == tenantID {
			filtered = append(filtered, receipt)
		}
	}
	return filtered, nil
}

func (r *covReceipts) ByLease(_ context.Context, leaseID string) ([]domain.Receipt, error) {
	filtered := make([]domain.Receipt, 0, len(r.items))
	for _, receipt := range r.items {
		if receipt.LeaseID != nil && *receipt.LeaseID == leaseID {
			filtered = append(filtered, receipt)
		}
	}
	return filtered, nil
}

func (r *covReceipts) Create(_ context.Context, receipt *domain.Receipt) error {
	r.items = append(r.items, *receipt)
	return nil
}

func (r *covReceipts) CreateIfAbsent(_ context.Context, receipt *domain.Receipt) (int64, error) {
	r.items = append(r.items, *receipt)
	return 1, nil
}

func (r *covReceipts) Update(_ context.Context, receipt *domain.Receipt) error {
	for index := range r.items {
		if r.items[index].ID == receipt.ID {
			r.items[index] = *receipt
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *covReceipts) UpdatePayment(_ context.Context, id, paidAt string, paidAmount float64, updatedAt string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items[index].PaidAt = &paidAt
			r.items[index].PaidAmount = &paidAmount
			r.items[index].UpdatedAt = &updatedAt
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *covReceipts) Delete(_ context.Context, id string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *covReceipts) Count(context.Context) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	return len(r.items), nil
}

type covSettings struct {
	values map[string]string
	err    error
	setErr error
}

func newCovSettings(values map[string]string) *covSettings {
	if values == nil {
		values = map[string]string{}
	}
	return &covSettings{values: values}
}

func (r *covSettings) Get(_ context.Context, key string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	value, ok := r.values[key]
	if !ok {
		return "", domain.ErrNotFound
	}
	return value, nil
}

func (r *covSettings) List(context.Context) (map[string]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.values, nil
}

func (r *covSettings) Set(_ context.Context, key, value string) error {
	if r.setErr != nil {
		return r.setErr
	}
	r.values[key] = value
	return nil
}

type covAudit struct {
	entries []domain.AuditLog
	err     error
}

func (r *covAudit) Insert(_ context.Context, entry *domain.AuditLog) error {
	if r.err != nil {
		return r.err
	}
	r.entries = append(r.entries, *entry)
	return nil
}

func (r *covAudit) List(_ context.Context, filter domain.AuditFilter) ([]domain.AuditLog, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.entries, nil
}

func (r *covAudit) Count(context.Context, domain.AuditFilter) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	return len(r.entries), nil
}

func (r *covAudit) Actors(context.Context) ([]string, error) { return nil, nil }

func (r *covAudit) actions() []string {
	actions := make([]string, 0, len(r.entries))
	for _, entry := range r.entries {
		actions = append(actions, entry.Action)
	}
	return actions
}

type covInspections struct {
	items []domain.Inspection
	err   error
}

func (r *covInspections) List(context.Context) ([]domain.Inspection, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.items, nil
}

func (r *covInspections) ByID(_ context.Context, id string) (*domain.Inspection, error) {
	for index := range r.items {
		if r.items[index].ID == id {
			return &r.items[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covInspections) ByLease(_ context.Context, leaseID string) ([]domain.Inspection, error) {
	filtered := make([]domain.Inspection, 0, len(r.items))
	for _, inspection := range r.items {
		if inspection.LeaseID == leaseID {
			filtered = append(filtered, inspection)
		}
	}
	return filtered, nil
}

func (r *covInspections) Create(_ context.Context, inspection *domain.Inspection) error {
	if r.err != nil {
		return r.err
	}
	r.items = append(r.items, *inspection)
	return nil
}

func (r *covInspections) Update(_ context.Context, inspection *domain.Inspection) error {
	for index := range r.items {
		if r.items[index].ID == inspection.ID {
			r.items[index] = *inspection
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *covInspections) Delete(_ context.Context, id string) error {
	if r.err != nil {
		return r.err
	}
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

type covJobs struct {
	items []domain.Job
	err   error
}

func (r *covJobs) Create(_ context.Context, job *domain.Job) error {
	r.items = append(r.items, *job)
	return nil
}

func (r *covJobs) ByID(_ context.Context, id string) (*domain.Job, error) {
	for index := range r.items {
		if r.items[index].ID == id {
			return &r.items[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covJobs) ListByStatus(_ context.Context, status domain.JobStatus, limit int) ([]domain.Job, error) {
	if r.err != nil {
		return nil, r.err
	}
	filtered := make([]domain.Job, 0, len(r.items))
	for _, job := range r.items {
		if job.Status == status && (limit <= 0 || len(filtered) < limit) {
			filtered = append(filtered, job)
		}
	}
	return filtered, nil
}

func (r *covJobs) List(_ context.Context, limit int) ([]domain.Job, error) {
	if r.err != nil {
		return nil, r.err
	}
	if limit > 0 && len(r.items) > limit {
		return r.items[:limit], nil
	}
	return r.items, nil
}

func (r *covJobs) Update(_ context.Context, job *domain.Job) error {
	if r.err != nil {
		return r.err
	}
	for index := range r.items {
		if r.items[index].ID == job.ID {
			r.items[index] = *job
			return nil
		}
	}
	return domain.ErrNotFound
}

type covProofs struct {
	items []domain.PaymentProof
	err   error
}

func (r *covProofs) Create(_ context.Context, proof *domain.PaymentProof) error {
	r.items = append(r.items, *proof)
	return nil
}

func (r *covProofs) ByID(_ context.Context, id string) (*domain.PaymentProof, error) {
	if r.err != nil {
		return nil, r.err
	}
	for index := range r.items {
		if r.items[index].ID == id {
			return &r.items[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covProofs) ListByStatus(_ context.Context, status domain.PaymentProofStatus, limit int) ([]domain.PaymentProof, error) {
	if r.err != nil {
		return nil, r.err
	}
	filtered := make([]domain.PaymentProof, 0, len(r.items))
	for _, proof := range r.items {
		if limit > 0 && len(filtered) >= limit {
			break
		}
		if proof.Status == status {
			filtered = append(filtered, proof)
		}
	}
	return filtered, nil
}

func (r *covProofs) List(_ context.Context, limit int) ([]domain.PaymentProof, error) {
	if r.err != nil {
		return nil, r.err
	}
	if limit > 0 && len(r.items) > limit {
		return r.items[:limit], nil
	}
	return r.items, nil
}

func (r *covProofs) ByReceipt(_ context.Context, receiptID string) ([]domain.PaymentProof, error) {
	filtered := make([]domain.PaymentProof, 0, len(r.items))
	for _, proof := range r.items {
		if proof.ReceiptID != nil && *proof.ReceiptID == receiptID {
			filtered = append(filtered, proof)
		}
	}
	return filtered, nil
}

func (r *covProofs) UpdateDecision(_ context.Context, proof *domain.PaymentProof) error {
	if r.err != nil {
		return r.err
	}
	for index := range r.items {
		if r.items[index].ID == proof.ID {
			r.items[index] = *proof
			return nil
		}
	}
	return domain.ErrNotFound
}

type covIntakes struct {
	items []domain.DocumentIntake
	err   error
}

func (r *covIntakes) Create(_ context.Context, intake *domain.DocumentIntake) error {
	r.items = append(r.items, *intake)
	return nil
}

func (r *covIntakes) ByID(_ context.Context, id string) (*domain.DocumentIntake, error) {
	if r.err != nil {
		return nil, r.err
	}
	for index := range r.items {
		if r.items[index].ID == id {
			return &r.items[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covIntakes) List(_ context.Context, limit int) ([]domain.DocumentIntake, error) {
	if r.err != nil {
		return nil, r.err
	}
	if limit > 0 && len(r.items) > limit {
		return r.items[:limit], nil
	}
	return r.items, nil
}

func (r *covIntakes) ListByStatus(_ context.Context, status domain.IntakeStatus, limit int) ([]domain.DocumentIntake, error) {
	if r.err != nil {
		return nil, r.err
	}
	filtered := make([]domain.DocumentIntake, 0, len(r.items))
	for _, intake := range r.items {
		if limit > 0 && len(filtered) >= limit {
			break
		}
		if intake.Status == status {
			filtered = append(filtered, intake)
		}
	}
	return filtered, nil
}

func (r *covIntakes) Update(_ context.Context, intake *domain.DocumentIntake) error {
	for index := range r.items {
		if r.items[index].ID == intake.ID {
			r.items[index] = *intake
			return nil
		}
	}
	return domain.ErrNotFound
}

type covMessages struct {
	items []domain.WhatsAppMessage
	err   error
}

func (r *covMessages) InsertIfAbsent(_ context.Context, message *domain.WhatsAppMessage) (int64, error) {
	if r.err != nil {
		return 0, r.err
	}
	r.items = append(r.items, *message)
	return 1, nil
}

func (r *covMessages) ByTenant(_ context.Context, tenantID string, limit int) ([]domain.WhatsAppMessage, error) {
	if r.err != nil {
		return nil, r.err
	}
	filtered := make([]domain.WhatsAppMessage, 0, len(r.items))
	for _, message := range r.items {
		if limit > 0 && len(filtered) >= limit {
			break
		}
		if message.TenantID != nil && *message.TenantID == tenantID {
			filtered = append(filtered, message)
		}
	}
	return filtered, nil
}

// ── Montagem do painel de teste ────────────────────────────────────

type covPanel struct {
	t            *testing.T
	handler      *Handlers
	common       *Common
	admin        *domain.User
	clock        *covClock
	users        *covUsers
	tenants      *covTenants
	properties   *covProperties
	leases       *covLeases
	receipts     *covReceipts
	settings     *covSettings
	audit        *covAudit
	inspections  *covInspections
	jobs         *covJobs
	proofs       *covProofs
	intakes      *covIntakes
	messages     *covMessages
	attachments  *covAttachments
	logs         *covLogHandler
	mustChange   func(context.Context, string) (bool, error)
	ping         func(context.Context) error
	dbSize       func() int64
	whatsappGate bool
}

// newCovPanel monta o painel com todos os repositórios dublês e um
// administrador já com senha trocada (a trava de senha não interfere).
func newCovPanel(t *testing.T) *covPanel {
	t.Helper()
	admin := &domain.User{
		ID:                "cov-admin",
		Username:          "passoz",
		PasswordHash:      "covhash:segredo",
		PasswordChangedAt: covString("2026-01-01T00:00:00Z"),
	}
	panel := newCovPanelWith(t, admin)
	return panel
}

// newCovPanelWith monta o painel com o administrador informado.
func newCovPanelWith(t *testing.T, admin *domain.User) *covPanel {
	t.Helper()

	clock := &covClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	panel := &covPanel{
		t:           t,
		admin:       admin,
		clock:       clock,
		users:       newCovUsers(admin),
		tenants:     &covTenants{},
		properties:  &covProperties{},
		leases:      &covLeases{},
		receipts:    &covReceipts{},
		settings:    newCovSettings(nil),
		audit:       &covAudit{},
		inspections: &covInspections{},
		jobs:        &covJobs{},
		proofs:      &covProofs{},
		intakes:     &covIntakes{},
		messages:    &covMessages{},
		attachments: newCovAttachments(),
		logs:        &covLogHandler{},
	}

	cfg := &config.Config{
		AppEnv:             "test",
		Host:               "127.0.0.1",
		Port:               0,
		AppURL:             "http://127.0.0.1:8080",
		SessionSecret:      "cov-panel-session-secret-with-length",
		RateLimitMax:       5,
		RateLimitGlobalMax: 60,
		LoginLockThreshold: 10,
		SeedUsername:       admin.Username,
	}

	deps := application.Deps{
		Repos: application.Repositories{
			Users:         panel.users,
			Tenants:       panel.tenants,
			Properties:    panel.properties,
			Leases:        panel.leases,
			Receipts:      panel.receipts,
			Settings:      panel.settings,
			Audit:         panel.audit,
			WhatsApp:      panel.messages,
			Inspections:   panel.inspections,
			Jobs:          panel.jobs,
			PaymentProofs: panel.proofs,
			Intakes:       panel.intakes,
		},
		Clock:       clock,
		IDs:         &covIDs{},
		Hasher:      covHasher{},
		Signer:      covSigner{},
		Attachments: panel.attachments,
		Log:         slog.New(panel.logs),
		Opts: application.Options{
			PublicBaseURL:         "http://127.0.0.1:8080",
			SeedUsername:          admin.Username,
			MaxBackupImportSize:   4 << 20,
			AIConfidenceThreshold: 0.6,
		},
	}

	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	common := &Common{
		Cfg:       cfg,
		Log:       deps.Log,
		Render:    renderer,
		Session:   NewSessionManager(cfg.SessionSecret, false),
		CSRF:      NewCSRF(cfg.SessionSecret, false),
		Deps:      deps,
		Build:     buildinfo.Info{Version: "0.0.0-cov", PackageVersion: "0.0.0", BuildNumber: 1, StartedAt: clock.now},
		Started:   clock.now,
		Dialect:   "sqlite",
		DBPath:    "/tmp/cov-aluxen.sqlite",
		DBSize:    func() int64 { return 4096 },
		LoginGate: application.NewLoginGate(clock, cfg.RateLimitMax, cfg.RateLimitGlobalMax, cfg.LoginLockThreshold),
	}
	common.MustChangePasswordFunc = func(ctx context.Context, userID string) (bool, error) {
		if panel.mustChange != nil {
			return panel.mustChange(ctx, userID)
		}
		return false, nil
	}
	panel.common = common
	panel.handler = NewHandlers(common)
	return panel
}

// covString devolve o ponteiro para o valor.
func covString(value string) *string { return &value }

// covFloat devolve o ponteiro para o valor.
func covFloat(value float64) *float64 { return &value }

// sessionCookie emite o cookie de sessão assinado do painel.
func (p *covPanel) sessionCookie(id string, admin bool) *http.Cookie {
	recorder := httptest.NewRecorder()
	if admin {
		p.common.Session.SetAdmin(recorder, id)
	} else {
		p.common.Session.SetClient(recorder, id)
	}
	for _, cookie := range recorder.Result().Cookies() {
		return cookie
	}
	return nil
}

// do executa a requisição pelo router completo.
func (p *covPanel) do(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	p.handler.Router().ServeHTTP(recorder, request)
	return recorder
}

// asAdmin executa a requisição com a sessão do administrador.
func (p *covPanel) asAdmin(method, target string, form url.Values) *httptest.ResponseRecorder {
	return p.as(method, target, form, p.admin.ID)
}

// as executa a requisição com a sessão do usuário informado (string vazia sem
// sessão).
func (p *covPanel) as(method, target string, form url.Values, userID string) *httptest.ResponseRecorder {
	var request *http.Request
	if form != nil {
		request = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		request = httptest.NewRequest(method, target, nil)
	}
	if userID != "" {
		request.AddCookie(p.sessionCookie(userID, true))
	}
	return p.do(request)
}

// csrfPost executa um POST autenticado com o token de CSRF válido.
func (p *covPanel) csrfPost(target string, form url.Values) *httptest.ResponseRecorder {
	page := httptest.NewRecorder()
	p.handler.Router().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/login", nil))
	token := cookieValue(page.Result().Cookies(), csrfCookieName)
	if token == "" {
		p.t.Fatal("cookie de CSRF não emitido")
	}
	if form == nil {
		form = url.Values{}
	}
	form.Set(csrfFieldName, token)

	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(p.sessionCookie(p.admin.ID, true))
	for _, cookie := range page.Result().Cookies() {
		request.AddCookie(cookie)
	}
	return p.do(request)
}

// covDecodeJSON decodifica o corpo JSON da resposta.
func covDecodeJSON(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("corpo não é JSON válido (%v): %s", err, recorder.Body.String())
	}
	return payload
}

var errCovBoom = errors.New("cov: falha injetada")
