package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// ── Dublês do lote de cobertura (`cov`) ────────────────────────────
//
// Este arquivo concentra os dublês em memória usados pelos testes de cobertura
// do pacote. Todo símbolo leva o prefixo `cov` para não colidir com os dublês
// dos testes já existentes (`ag`, `client`, `intake`, `doc`, `rcpt`, `wa`…).
//
// Os repositórios são caixas em memória com injeção de erro (`err*`), de modo
// que cada caso de uso possa ser exercitado pelo comportamento observável: o
// que ficou gravado, o que foi devolvido e qual erro saiu.

// ── Serviços ──────────────────────────────────────────────────────

// covClock é o relógio fixo dos testes.
type covClock struct{ now time.Time }

func (c covClock) Now() time.Time { return c.now }

// covIDs gera identificadores previsíveis na ordem de uso.
type covIDs struct {
	prefix string
	next   int
}

func (g *covIDs) New() string {
	g.next++
	return fmt.Sprintf("%s-%03d", g.prefix, g.next)
}

// covHasher deriva o hash por prefixo simples — o suficiente para conferir a
// regra de senha sem custo de bcrypt.
type covHasher struct{}

func (covHasher) Hash(password string) (string, error) { return "cov$" + password, nil }

func (covHasher) Compare(hash, password string) error {
	if hash == "cov$"+password {
		return nil
	}
	return errors.New("hash não confere")
}

// covSigner emite tokens legíveis.
type covSigner struct{}

func (covSigner) Generate(kind, id string) string { return kind + "-" + id + "-tok" }
func (covSigner) FormatCode(token string) string  { return "AUTH-" + strings.ToUpper(token) }
func (covSigner) ValidationURL(base, kind, id, token string) string {
	return base + "/validar/" + kind + "/" + id + "?t=" + token
}

// covMessages reproduz os textos do canal de forma determinística.
type covMessages struct{}

func (covMessages) SanitizePhone(raw string) string { return format.NormalizeDigits(raw) }
func (covMessages) DirectURLWithMessage(phone, message string) string {
	return "https://wa.me/" + phone + "?text=" + message
}
func (covMessages) Receipt(message WhatsAppReceiptMessage) string {
	return "recibo:" + message.TenantName + ":" + message.Amount + ":" + message.Category
}
func (covMessages) Contract(message WhatsAppContractMessage) string {
	return "contrato:" + message.TenantName + ":" + message.PropertyName
}

// ── Repositórios ──────────────────────────────────────────────────

type covKV struct{ Key, Value string }

// covSettings é o repositório de configurações em memória.
type covSettings struct {
	values                  map[string]string
	getErr, listErr, setErr error
	sets                    []covKV
}

func newCovSettings(values map[string]string) *covSettings {
	if values == nil {
		values = map[string]string{}
	}
	return &covSettings{values: values}
}

func (r *covSettings) Get(_ context.Context, key string) (string, error) {
	if r.getErr != nil {
		return "", r.getErr
	}
	value, ok := r.values[key]
	if !ok {
		return "", domain.ErrNotFound
	}
	return value, nil
}

func (r *covSettings) List(context.Context) (map[string]string, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func (r *covSettings) Set(_ context.Context, key, value string) error {
	if r.setErr != nil {
		return r.setErr
	}
	r.values[key] = value
	r.sets = append(r.sets, covKV{key, value})
	return nil
}

// covUsers é o repositório de administradores em memória.
type covUsers struct {
	items                                        []domain.User
	byIDErr, byUsernameErr, updatePassErr        error
	passwordUpdates                              []covKV
}

func (r *covUsers) ByID(_ context.Context, id string) (*domain.User, error) {
	if r.byIDErr != nil {
		return nil, r.byIDErr
	}
	for index := range r.items {
		if r.items[index].ID == id {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covUsers) ByUsername(_ context.Context, username string) (*domain.User, error) {
	if r.byUsernameErr != nil {
		return nil, r.byUsernameErr
	}
	for index := range r.items {
		if r.items[index].Username == username {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covUsers) Create(_ context.Context, user *domain.User) error {
	r.items = append(r.items, *user)
	return nil
}

func (r *covUsers) UpdateUsername(_ context.Context, id, username string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items[index].Username = username
		}
	}
	return nil
}

func (r *covUsers) UpdatePassword(_ context.Context, id, hash, changedAt string) error {
	if r.updatePassErr != nil {
		return r.updatePassErr
	}
	for index := range r.items {
		if r.items[index].ID == id {
			r.items[index].PasswordHash = hash
			r.items[index].PasswordChangedAt = domain.StrPtr(changedAt)
		}
	}
	r.passwordUpdates = append(r.passwordUpdates, covKV{id, hash})
	return nil
}

// covTenants é o repositório de locatários em memória.
type covTenants struct {
	items                                                     []domain.Tenant
	listErr, byIDErr, byCPFErr, createErr, updateErr, delErr, countErr error
	deleted                                                   []string
	passwordUpdates                                           []covKV
}

func (r *covTenants) List(context.Context) ([]domain.Tenant, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return append([]domain.Tenant(nil), r.items...), nil
}

func (r *covTenants) ByID(_ context.Context, id string) (*domain.Tenant, error) {
	if r.byIDErr != nil {
		return nil, r.byIDErr
	}
	for index := range r.items {
		if r.items[index].ID == id {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covTenants) ByCPF(_ context.Context, digits string) (*domain.Tenant, error) {
	if r.byCPFErr != nil {
		return nil, r.byCPFErr
	}
	for index := range r.items {
		if format.NormalizeDigits(domain.Str(r.items[index].CPF)) == digits {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covTenants) Create(_ context.Context, tenant *domain.Tenant) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.items = append(r.items, *tenant)
	return nil
}

func (r *covTenants) Update(_ context.Context, tenant *domain.Tenant) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	for index := range r.items {
		if r.items[index].ID == tenant.ID {
			r.items[index] = *tenant
		}
	}
	return nil
}

func (r *covTenants) UpdatePassword(_ context.Context, id, hash, changedAt, updatedAt string) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	for index := range r.items {
		if r.items[index].ID == id {
			r.items[index].PasswordHash = domain.StrPtr(hash)
			r.items[index].PasswordChangedAt = domain.StrPtr(changedAt)
			r.items[index].UpdatedAt = updatedAt
		}
	}
	r.passwordUpdates = append(r.passwordUpdates, covKV{id, hash})
	return nil
}

func (r *covTenants) Delete(_ context.Context, id string) error {
	if r.delErr != nil {
		return r.delErr
	}
	r.deleted = append(r.deleted, id)
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			break
		}
	}
	return nil
}

func (r *covTenants) Count(context.Context) (int, error) {
	if r.countErr != nil {
		return 0, r.countErr
	}
	return len(r.items), nil
}

// covProperties é o repositório de imóveis em memória.
type covProperties struct {
	items                                                                  []domain.Property
	listErr, byIDErr, createErr, updateErr, delErr, countErr               error
	deleted                                                                []string
}

func (r *covProperties) List(context.Context) ([]domain.Property, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return append([]domain.Property(nil), r.items...), nil
}

func (r *covProperties) ByID(_ context.Context, id string) (*domain.Property, error) {
	if r.byIDErr != nil {
		return nil, r.byIDErr
	}
	for index := range r.items {
		if r.items[index].ID == id {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covProperties) Create(_ context.Context, property *domain.Property) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.items = append(r.items, *property)
	return nil
}

func (r *covProperties) Update(_ context.Context, property *domain.Property) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	for index := range r.items {
		if r.items[index].ID == property.ID {
			r.items[index] = *property
		}
	}
	return nil
}

func (r *covProperties) Delete(_ context.Context, id string) error {
	if r.delErr != nil {
		return r.delErr
	}
	r.deleted = append(r.deleted, id)
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			break
		}
	}
	return nil
}

func (r *covProperties) Count(context.Context) (int, error) {
	if r.countErr != nil {
		return 0, r.countErr
	}
	return len(r.items), nil
}

// covLeases é o repositório de contratos em memória, com a mesma semântica de
// sobreposição que a porta promete.
type covLeases struct {
	items                                                                []domain.Lease
	listErr, byIDErr, byTenantErr, createErr, updateErr, statusErr, delErr, countErr error
	statusUpdates                                                        []covKV
	deleted                                                              []string
}

func (r *covLeases) List(context.Context) ([]domain.Lease, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return append([]domain.Lease(nil), r.items...), nil
}

func (r *covLeases) ByID(_ context.Context, id string) (*domain.Lease, error) {
	if r.byIDErr != nil {
		return nil, r.byIDErr
	}
	for index := range r.items {
		if r.items[index].ID == id {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covLeases) ByTenant(_ context.Context, tenantID string) ([]domain.Lease, error) {
	if r.byTenantErr != nil {
		return nil, r.byTenantErr
	}
	var out []domain.Lease
	for _, lease := range r.items {
		if lease.TenantID == tenantID {
			out = append(out, lease)
		}
	}
	return out, nil
}

func (r *covLeases) ByProperty(_ context.Context, propertyID string) ([]domain.Lease, error) {
	var out []domain.Lease
	for _, lease := range r.items {
		if lease.PropertyID == propertyID {
			out = append(out, lease)
		}
	}
	return out, nil
}

func (r *covLeases) Overlapping(_ context.Context, propertyID, excludeID, startDate, endDate string) ([]domain.Lease, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []domain.Lease
	for _, lease := range r.items {
		if lease.PropertyID != propertyID || lease.ID == excludeID || !lease.Status.BlocksOverlap() {
			continue
		}
		if domain.Overlaps(startDate, endDate, lease.StartDate, lease.EndDate) {
			out = append(out, lease)
		}
	}
	return out, nil
}

func (r *covLeases) ActiveInRange(_ context.Context, startDate, endDate string) ([]domain.Lease, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []domain.Lease
	for _, lease := range r.items {
		if lease.Status != domain.LeaseStatusActive {
			continue
		}
		if domain.Overlaps(startDate, endDate, lease.StartDate, lease.EndDate) {
			out = append(out, lease)
		}
	}
	return out, nil
}

func (r *covLeases) Create(_ context.Context, lease *domain.Lease) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.items = append(r.items, *lease)
	return nil
}

func (r *covLeases) Update(_ context.Context, lease *domain.Lease) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	for index := range r.items {
		if r.items[index].ID == lease.ID {
			r.items[index] = *lease
		}
	}
	return nil
}

func (r *covLeases) UpdateStatus(_ context.Context, id string, status domain.LeaseStatus, updatedAt string) error {
	if r.statusErr != nil {
		return r.statusErr
	}
	for index := range r.items {
		if r.items[index].ID == id {
			r.items[index].Status = status
			r.items[index].UpdatedAt = updatedAt
		}
	}
	r.statusUpdates = append(r.statusUpdates, covKV{id, string(status)})
	return nil
}

func (r *covLeases) Delete(_ context.Context, id string) error {
	if r.delErr != nil {
		return r.delErr
	}
	r.deleted = append(r.deleted, id)
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			break
		}
	}
	return nil
}

func (r *covLeases) Count(context.Context) (int, error) {
	if r.countErr != nil {
		return 0, r.countErr
	}
	return len(r.items), nil
}

func (r *covLeases) CountByStatus(_ context.Context, status domain.LeaseStatus) (int, error) {
	if r.countErr != nil {
		return 0, r.countErr
	}
	total := 0
	for _, lease := range r.items {
		if lease.Status == status {
			total++
		}
	}
	return total, nil
}

// covReceipts é o repositório de lançamentos em memória. CreateIfAbsent respeita
// a chave de geração mensal, como a porta promete.
type covReceipts struct {
	items                                                                             []domain.Receipt
	listErr, byIDErr, byTenantErr, byLeaseErr, createErr, updateErr, payErr, delErr, countErr error
	payments                                                                          []covKV
	deleted                                                                           []string
}

func (r *covReceipts) List(context.Context) ([]domain.Receipt, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return append([]domain.Receipt(nil), r.items...), nil
}

func (r *covReceipts) ByID(_ context.Context, id string) (*domain.Receipt, error) {
	if r.byIDErr != nil {
		return nil, r.byIDErr
	}
	for index := range r.items {
		if r.items[index].ID == id {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covReceipts) ByTenant(_ context.Context, tenantID string) ([]domain.Receipt, error) {
	if r.byTenantErr != nil {
		return nil, r.byTenantErr
	}
	var out []domain.Receipt
	for _, receipt := range r.items {
		if domain.Str(receipt.TenantID) == tenantID {
			out = append(out, receipt)
		}
	}
	return out, nil
}

func (r *covReceipts) ByLease(_ context.Context, leaseID string) ([]domain.Receipt, error) {
	if r.byLeaseErr != nil {
		return nil, r.byLeaseErr
	}
	var out []domain.Receipt
	for _, receipt := range r.items {
		if domain.Str(receipt.LeaseID) == leaseID {
			out = append(out, receipt)
		}
	}
	return out, nil
}

func (r *covReceipts) Create(_ context.Context, receipt *domain.Receipt) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.items = append(r.items, *receipt)
	return nil
}

func (r *covReceipts) CreateIfAbsent(_ context.Context, receipt *domain.Receipt) (int64, error) {
	if r.createErr != nil {
		return 0, r.createErr
	}
	if receipt.GeneratedKey != nil {
		for _, existing := range r.items {
			if existing.GeneratedKey != nil && *existing.GeneratedKey == *receipt.GeneratedKey {
				return 0, nil
			}
		}
	}
	r.items = append(r.items, *receipt)
	return 1, nil
}

func (r *covReceipts) Update(_ context.Context, receipt *domain.Receipt) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	for index := range r.items {
		if r.items[index].ID == receipt.ID {
			r.items[index] = *receipt
		}
	}
	return nil
}

func (r *covReceipts) UpdatePayment(_ context.Context, id, paidAt string, paidAmount float64, updatedAt string) error {
	if r.payErr != nil {
		return r.payErr
	}
	for index := range r.items {
		if r.items[index].ID == id {
			r.items[index].PaidAt = domain.StrPtr(paidAt)
			r.items[index].PaidAmount = domain.FloatPtr(paidAmount)
			r.items[index].UpdatedAt = domain.StrPtr(updatedAt)
		}
	}
	r.payments = append(r.payments, covKV{id, fmt.Sprintf("%s|%.2f", paidAt, paidAmount)})
	return nil
}

func (r *covReceipts) Delete(_ context.Context, id string) error {
	if r.delErr != nil {
		return r.delErr
	}
	r.deleted = append(r.deleted, id)
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			break
		}
	}
	return nil
}

func (r *covReceipts) Count(context.Context) (int, error) {
	if r.countErr != nil {
		return 0, r.countErr
	}
	return len(r.items), nil
}

// covInspections é o repositório de vistorias em memória.
type covInspections struct {
	items                                                                []domain.Inspection
	listErr, byIDErr, byLeaseErr, createErr, updateErr, delErr           error
	deleted                                                              []string
}

func (r *covInspections) List(context.Context) ([]domain.Inspection, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return append([]domain.Inspection(nil), r.items...), nil
}

func (r *covInspections) ByID(_ context.Context, id string) (*domain.Inspection, error) {
	if r.byIDErr != nil {
		return nil, r.byIDErr
	}
	for index := range r.items {
		if r.items[index].ID == id {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covInspections) ByLease(_ context.Context, leaseID string) ([]domain.Inspection, error) {
	if r.byLeaseErr != nil {
		return nil, r.byLeaseErr
	}
	var out []domain.Inspection
	for _, inspection := range r.items {
		if inspection.LeaseID == leaseID {
			out = append(out, inspection)
		}
	}
	return out, nil
}

func (r *covInspections) Create(_ context.Context, inspection *domain.Inspection) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.items = append(r.items, *inspection)
	return nil
}

func (r *covInspections) Update(_ context.Context, inspection *domain.Inspection) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	for index := range r.items {
		if r.items[index].ID == inspection.ID {
			r.items[index] = *inspection
		}
	}
	return nil
}

func (r *covInspections) Delete(_ context.Context, id string) error {
	if r.delErr != nil {
		return r.delErr
	}
	r.deleted = append(r.deleted, id)
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			break
		}
	}
	return nil
}

// covAudit é a trilha em memória.
type covAudit struct {
	entries                        []domain.AuditLog
	insertErr                      error
	filter                         domain.AuditFilter
}

func (r *covAudit) Insert(_ context.Context, entry *domain.AuditLog) error {
	if r.insertErr != nil {
		return r.insertErr
	}
	r.entries = append(r.entries, *entry)
	return nil
}

func (r *covAudit) List(_ context.Context, filter domain.AuditFilter) ([]domain.AuditLog, error) {
	r.filter = filter
	return append([]domain.AuditLog(nil), r.entries...), nil
}

func (r *covAudit) Count(context.Context, domain.AuditFilter) (int, error) {
	return len(r.entries), nil
}

func (r *covAudit) Actors(context.Context) ([]string, error) { return nil, nil }

// covMessages é a caixa de mensagens do canal.
type covMessages2 struct {
	items        []domain.WhatsAppMessage
	insertErr    error
	inserts      int
	byTenantErr  error
}

func (r *covMessages2) InsertIfAbsent(_ context.Context, message *domain.WhatsAppMessage) (int64, error) {
	if r.insertErr != nil {
		return 0, r.insertErr
	}
	if message.ExternalID != nil {
		for _, existing := range r.items {
			if existing.ExternalID != nil && *existing.ExternalID == *message.ExternalID {
				return 0, nil
			}
		}
	}
	r.items = append(r.items, *message)
	r.inserts++
	return 1, nil
}

func (r *covMessages2) ByTenant(_ context.Context, tenantID string, limit int) ([]domain.WhatsAppMessage, error) {
	if r.byTenantErr != nil {
		return nil, r.byTenantErr
	}
	var out []domain.WhatsAppMessage
	for _, message := range r.items {
		if domain.Str(message.TenantID) == tenantID {
			out = append(out, message)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// covJobs é a fila em memória.
type covJobs struct {
	items                                                       []domain.Job
	createErr, byIDErr, listErr, updateErr                      error
	updates                                                     []domain.Job
}

func (r *covJobs) Create(_ context.Context, job *domain.Job) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.items = append(r.items, *job)
	return nil
}

func (r *covJobs) ByID(_ context.Context, id string) (*domain.Job, error) {
	if r.byIDErr != nil {
		return nil, r.byIDErr
	}
	for index := range r.items {
		if r.items[index].ID == id {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covJobs) ListByStatus(_ context.Context, status domain.JobStatus, limit int) ([]domain.Job, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []domain.Job
	for _, job := range r.items {
		if job.Status == status {
			out = append(out, job)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *covJobs) List(context.Context, int) ([]domain.Job, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return append([]domain.Job(nil), r.items...), nil
}

func (r *covJobs) Update(_ context.Context, job *domain.Job) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.updates = append(r.updates, *job)
	for index := range r.items {
		if r.items[index].ID == job.ID {
			r.items[index] = *job
		}
	}
	return nil
}

// covProofs é o repositório de comprovantes em memória.
type covProofs struct {
	items                                                                    []domain.PaymentProof
	createErr, byIDErr, listErr, updateErr                                   error
}

func (r *covProofs) Create(_ context.Context, proof *domain.PaymentProof) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.items = append(r.items, *proof)
	return nil
}

func (r *covProofs) ByID(_ context.Context, id string) (*domain.PaymentProof, error) {
	if r.byIDErr != nil {
		return nil, r.byIDErr
	}
	for index := range r.items {
		if r.items[index].ID == id {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covProofs) ListByStatus(_ context.Context, status domain.PaymentProofStatus, limit int) ([]domain.PaymentProof, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []domain.PaymentProof
	for _, proof := range r.items {
		if proof.Status == status {
			out = append(out, proof)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *covProofs) List(context.Context, int) ([]domain.PaymentProof, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return append([]domain.PaymentProof(nil), r.items...), nil
}

func (r *covProofs) ByReceipt(_ context.Context, receiptID string) ([]domain.PaymentProof, error) {
	var out []domain.PaymentProof
	for _, proof := range r.items {
		if domain.Str(proof.ReceiptID) == receiptID {
			out = append(out, proof)
		}
	}
	return out, nil
}

func (r *covProofs) UpdateDecision(_ context.Context, proof *domain.PaymentProof) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	for index := range r.items {
		if r.items[index].ID == proof.ID {
			r.items[index] = *proof
		}
	}
	return nil
}

// covIntakes é o repositório de cadastros propostos em memória.
type covIntakes struct {
	items                                                       []domain.DocumentIntake
	createErr, byIDErr, listErr, updateErr                      error
}

func (r *covIntakes) Create(_ context.Context, intake *domain.DocumentIntake) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.items = append(r.items, *intake)
	return nil
}

func (r *covIntakes) ByID(_ context.Context, id string) (*domain.DocumentIntake, error) {
	if r.byIDErr != nil {
		return nil, r.byIDErr
	}
	for index := range r.items {
		if r.items[index].ID == id {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *covIntakes) List(_ context.Context, limit int) ([]domain.DocumentIntake, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := append([]domain.DocumentIntake(nil), r.items...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *covIntakes) ListByStatus(_ context.Context, status domain.IntakeStatus, limit int) ([]domain.DocumentIntake, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []domain.DocumentIntake
	for _, intake := range r.items {
		if intake.Status == status {
			out = append(out, intake)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *covIntakes) Update(_ context.Context, intake *domain.DocumentIntake) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	for index := range r.items {
		if r.items[index].ID == intake.ID {
			r.items[index] = *intake
		}
	}
	return nil
}

// covAttachments é o armazenamento de anexos em memória.
type covAttachments struct {
	files                       map[string][]byte
	names                       []string
	saveErr, openErr, delErr    error
}

func newCovAttachments() *covAttachments {
	return &covAttachments{files: map[string][]byte{}}
}

func (s *covAttachments) Save(prefix, filename string, data []byte) (string, error) {
	if s.saveErr != nil {
		return "", s.saveErr
	}
	name := prefix + "-" + filename
	s.files[name] = append([]byte(nil), data...)
	s.names = append(s.names, name)
	return name, nil
}

func (s *covAttachments) Open(name string) ([]byte, error) {
	if s.openErr != nil {
		return nil, s.openErr
	}
	data, ok := s.files[name]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return data, nil
}

func (s *covAttachments) Delete(name string) error {
	if s.delErr != nil {
		return s.delErr
	}
	delete(s.files, name)
	return nil
}

// ── Montagem ──────────────────────────────────────────────────────

// covEnv reúne as dependências e as caixas do teste.
type covEnv struct {
	deps    Deps
	clock   covClock
	ids     *covIDs
	audit   *covAudit
	users   *covUsers
	tenants *covTenants
	// Os campos abaixo repetem o nome da porta para leitura fácil nos testes.
	props       *covProperties
	leases      *covLeases
	receipts    *covReceipts
	inspections *covInspections
	settings    *covSettings
	messages    *covMessages2
	jobs        *covJobs
	proofs      *covProofs
	intakes     *covIntakes
	attachments *covAttachments
}

// covToday é o "hoje" do lote: mesma data dos demais testes do pacote.
func covToday() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) }

// newCovEnv monta o ambiente com tudo em memória.
func newCovEnv(t interface{ Helper() }) *covEnv {
	t.Helper()
	env := &covEnv{
		clock:       covClock{now: covToday()},
		ids:         &covIDs{prefix: "cov"},
		audit:       &covAudit{},
		users:       &covUsers{},
		tenants:     &covTenants{},
		props:       &covProperties{},
		leases:      &covLeases{},
		receipts:    &covReceipts{},
		inspections: &covInspections{},
		settings:    newCovSettings(nil),
		messages:    &covMessages2{},
		jobs:        &covJobs{},
		proofs:      &covProofs{},
		intakes:     &covIntakes{},
		attachments: newCovAttachments(),
	}
	env.deps = Deps{
		Repos: Repositories{
			Users:        env.users,
			Tenants:      env.tenants,
			Properties:   env.props,
			Leases:       env.leases,
			Receipts:     env.receipts,
			Settings:     env.settings,
			Audit:        env.audit,
			WhatsApp:     env.messages,
			Inspections:  env.inspections,
			Jobs:         env.jobs,
			PaymentProofs: env.proofs,
			Intakes:      env.intakes,
		},
		Clock:       env.clock,
		IDs:         env.ids,
		Hasher:      covHasher{},
		Signer:      covSigner{},
		Messages:    covMessages{},
		Attachments: env.attachments,
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Opts: Options{
			SeedUsername:          "admin",
			RateLimitMax:          5,
			RateLimitGlobalMax:    100,
			LoginLockThreshold:    3,
			MaxBackupImportSize:   1 << 20,
			AIConfidenceThreshold: 0.8,
		},
	}
	return env
}

// covCtx é o contexto de auditoria do painel.
func covCtx() AuditContext {
	return AuditContext{
		ActorID:   domain.StrPtr("user-1"),
		ActorName: domain.StrPtr("Admin"),
		Origin:    domain.AuditOriginWeb,
		IP:        "10.0.0.1",
		UserAgent: "cov-test",
		RequestID: "req-1",
	}
}

// covCtxOf monta um contexto de auditoria com a origem informada.
func covCtxOf(origin domain.AuditOrigin) AuditContext {
	ctx := covCtx()
	ctx.Origin = origin
	return ctx
}

// ── Construtores de entidades ─────────────────────────────────────

func covTenantOf(id, name string) domain.Tenant {
	return domain.Tenant{ID: id, Name: name, Email: name + "@example.com"}
}

func covPropertyOf(id, name string) domain.Property {
	return domain.Property{ID: id, Name: name, Address: "Rua A, 1", City: "Niterói", State: "RJ"}
}

func covLeaseOf(id, tenantID, propertyID string, status domain.LeaseStatus, start, end string, rent float64) domain.Lease {
	return domain.Lease{
		ID: id, TenantID: tenantID, PropertyID: propertyID,
		Status: status, StartDate: start, EndDate: end,
		DueDay: 10, RentAmount: rent,
	}
}

func covReceiptOf(id, tenantID, leaseID, dueDate string, amount float64) domain.Receipt {
	return domain.Receipt{
		ID: id, TenantID: domain.StrPtr(tenantID), LeaseID: domain.StrPtr(leaseID),
		Category: domain.ReceiptCategoryRent, DueDate: dueDate, Amount: amount,
	}
}

// covPaidReceipt marca a baixa informada no lançamento.
func covPaidReceipt(receipt domain.Receipt, paidAt string, paidAmount float64) domain.Receipt {
	receipt.PaidAt = domain.StrPtr(paidAt)
	receipt.PaidAmount = domain.FloatPtr(paidAmount)
	return receipt
}

// covAuditActions devolve as ações registradas, na ordem.
func covAuditActions(entries []domain.AuditLog) []string {
	actions := make([]string, 0, len(entries))
	for _, entry := range entries {
		actions = append(actions, entry.Action)
	}
	return actions
}

// covSortedNames ordena nomes para comparação estável.
func covSortedNames(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

// covJoined entrega uma lista legível para o relatório de falha.
func covJoined(values ...string) string { return strings.Join(values, ",") }
