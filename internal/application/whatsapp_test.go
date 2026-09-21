package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// ── Dublês do canal do WhatsApp ────────────────────────────────────
//
// Os nomes levam o prefixo `wa` para não colidirem com os dublês das outras
// áreas do mesmo pacote.

type waClock struct{ now time.Time }

func (c waClock) Now() time.Time { return c.now }

type waIDs struct{ next int }

func (g *waIDs) New() string {
	g.next++
	return fmt.Sprintf("id-%d", g.next)
}

// waMessages é a superfície de texto do canal: devolve uma string determinística
// com tudo que recebeu, para o teste conferir os valores que o caso de uso
// resolveu (quem formata o texto final é testado em internal/infrastructure/whatsapp).
type waMessages struct{}

func (waMessages) SanitizePhone(raw string) string {
	clean := format.NormalizeDigits(raw)
	if clean == "" {
		return ""
	}
	if strings.HasPrefix(clean, "55") && len(clean) >= 12 {
		return clean
	}
	return "55" + clean
}

func (messages waMessages) DirectURLWithMessage(phone, message string) string {
	clean := messages.SanitizePhone(phone)
	if clean == "" {
		return ""
	}
	return "https://wa.me/" + clean + "?text=" + message
}

func (waMessages) Receipt(in WhatsAppReceiptMessage) string {
	paidAmount := in.PaidAmount
	if in.PaidAmountRaw == nil {
		paidAmount = "-"
	}
	return fmt.Sprintf("recibo|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s",
		in.TenantName, in.PropertyName, in.DueDate, in.Amount, paidAmount, in.PaidAt,
		in.Category, in.Description, in.PDFURL, in.ValidationURL, in.LandlordName)
}

func (waMessages) Contract(in WhatsAppContractMessage) string {
	return fmt.Sprintf("contrato|%s|%s|%s|%s|%d|%s|%s|%s|%s",
		in.TenantName, in.PropertyName, in.StartDate, in.EndDate, in.DueDay,
		in.RentAmount, in.PDFURL, in.ValidationURL, in.LandlordName)
}

// waSigner emite tokens previsíveis: o teste confere o link montado, não o HMAC
// (coberto no pacote de infraestrutura).
type waSigner struct{}

func (waSigner) Generate(kind, id string) string { return fmt.Sprintf("token-%s-%s", kind, id) }

func (waSigner) FormatCode(token string) string { return "AUTH-" + token }

func (waSigner) ValidationURL(base, kind, id, token string) string {
	path := "recibo"
	if kind == WhatsAppKindLease {
		path = "contrato"
	}
	return fmt.Sprintf("%s/validar/%s/%s?token=%s", base, path, id, token)
}

// waReceipts devolve um recibo por id. A porta embutida cobre os métodos que
// este caso de uso não chama.
type waReceipts struct {
	domain.ReceiptRepository
	byID map[string]*domain.Receipt
}

func (r *waReceipts) ByID(_ context.Context, id string) (*domain.Receipt, error) {
	receipt, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return receipt, nil
}

// waLeases devolve um contrato por id.
type waLeases struct {
	domain.LeaseRepository
	byID map[string]*domain.Lease
}

func (l *waLeases) ByID(_ context.Context, id string) (*domain.Lease, error) {
	lease, ok := l.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return lease, nil
}

// waTenants responde por id e lista todos (é a lista que casa o telefone).
type waTenants struct {
	domain.TenantRepository
	tenants []domain.Tenant
}

func (r *waTenants) ByID(_ context.Context, id string) (*domain.Tenant, error) {
	for index := range r.tenants {
		if r.tenants[index].ID == id {
			return &r.tenants[index], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *waTenants) List(context.Context) ([]domain.Tenant, error) { return r.tenants, nil }

// waSettings responde às configurações pedidas.
type waSettings struct {
	domain.SettingRepository
	values map[string]string
}

func (r *waSettings) Get(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", domain.ErrNotFound
	}
	return value, nil
}

// waAudit guarda os eventos gravados na trilha.
type waAudit struct {
	mu      sync.Mutex
	entries []domain.AuditLog
}

func (r *waAudit) Insert(_ context.Context, entry *domain.AuditLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, *entry)
	return nil
}

func (r *waAudit) List(context.Context, domain.AuditFilter) ([]domain.AuditLog, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.entries, nil
}

func (r *waAudit) Count(context.Context, domain.AuditFilter) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries), nil
}

func (r *waAudit) Actors(context.Context) ([]string, error) { return nil, nil }

func (r *waAudit) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	actions := make([]string, 0, len(r.entries))
	for _, entry := range r.entries {
		actions = append(actions, entry.Action)
	}
	return actions
}

// waMessageStore guarda as mensagens do canal.
type waMessageStore struct {
	mu       sync.Mutex
	messages []domain.WhatsAppMessage
	// inserted é o resultado devolvido por InsertIfAbsent (0 = reentrega).
	inserted int64
}

func newWAMessageStore() *waMessageStore { return &waMessageStore{inserted: 1} }

func (s *waMessageStore) InsertIfAbsent(_ context.Context, message *domain.WhatsAppMessage) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stored := range s.messages {
		if stored.ExternalID != nil && message.ExternalID != nil && *stored.ExternalID == *message.ExternalID {
			return 0, nil
		}
	}
	s.messages = append(s.messages, *message)
	return s.inserted, nil
}

func (s *waMessageStore) ByTenant(_ context.Context, tenantID string, _ int) ([]domain.WhatsAppMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found []domain.WhatsAppMessage
	for _, message := range s.messages {
		if domain.Str(message.TenantID) == tenantID {
			found = append(found, message)
		}
	}
	return found, nil
}

func (s *waMessageStore) stored() []domain.WhatsAppMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.WhatsAppMessage(nil), s.messages...)
}

// waChannel é o canal de envio: fila em memória e envio controlado.
type waChannel struct {
	mu        sync.Mutex
	jobs      map[string]WhatsAppJob
	stages    []WhatsAppStage
	messageID string
	sendErr   error
	next      int
}

func newWAChannel() *waChannel { return &waChannel{jobs: map[string]WhatsAppJob{}} }

func (c *waChannel) Create(userID string) WhatsAppJob {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	job := WhatsAppJob{
		ID:        fmt.Sprintf("job-%d", c.next),
		UserID:    userID,
		Status:    JobRunning,
		Stage:     StagePreparing,
		UpdatedAt: time.Now().UnixMilli(),
	}
	c.jobs[job.ID] = job
	return job
}

func (c *waChannel) Stage(jobID string, stage WhatsAppStage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stages = append(c.stages, stage)
	job := c.jobs[jobID]
	job.Stage = stage
	c.jobs[jobID] = job
}

func (c *waChannel) Complete(jobID string, result WhatsAppSendResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	job := c.jobs[jobID]
	job.Status = JobDone
	job.Stage = StageDone
	job.Result = &result
	c.jobs[jobID] = job
}

func (c *waChannel) Fail(jobID, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	job := c.jobs[jobID]
	job.Status = JobError
	job.Error = reason
	c.jobs[jobID] = job
}

func (c *waChannel) Get(jobID, userID string) (WhatsAppJob, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	job, ok := c.jobs[jobID]
	if !ok || job.UserID != userID {
		return WhatsAppJob{}, false
	}
	return job, true
}

func (c *waChannel) SendText(_ context.Context, phone, _ string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sendErr != nil {
		return "", c.sendErr
	}
	if phone == "" {
		return "", errors.New("telefone vazio")
	}
	return c.messageID, nil
}

func (c *waChannel) publishedStages() []WhatsAppStage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]WhatsAppStage(nil), c.stages...)
}

// waitJob espera o envio em segundo plano terminar.
func waitJob(t *testing.T, channel *waChannel, jobID string) WhatsAppJob {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if job, ok := channel.Get(jobID, "u1"); ok && job.Status != JobRunning {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("job %s não terminou; estado atual %+v", jobID, channel.jobs[jobID])
	return WhatsAppJob{}
}

// ── Montagem ──────────────────────────────────────────────────────

type waFixture struct {
	deps     Deps
	audit    *waAudit
	messages *waMessageStore
	channel  *waChannel
}

func newWAFixture(t *testing.T, receipts *waReceipts, leases *waLeases, tenants *waTenants) *waFixture {
	t.Helper()

	audit := &waAudit{}
	messages := newWAMessageStore()

	deps := Deps{
		Repos: Repositories{
			Receipts: receipts,
			Leases:   leases,
			Tenants:  tenants,
			Settings: &waSettings{values: map[string]string{domain.SettingLandlordName: "João Locador"}},
			Audit:    audit,
			WhatsApp: messages,
		},
		Clock:    waClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)},
		IDs:      &waIDs{},
		Signer:   waSigner{},
		Messages: waMessages{},
		// Base determinística: sem ela, ResolvePublicBaseURL cai no IP da rede
		// local da máquina que roda o teste (comportamento correto do produto,
		// mas não reproduzível em teste).
		Opts: Options{PublicBaseURL: "https://aluxen.test"},
		Log:  slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})),
	}
	return &waFixture{deps: deps, audit: audit, messages: messages, channel: newWAChannel()}
}

func paidReceiptFixture() (*waReceipts, *waLeases, *waTenants) {
	paidAt := "2026-10-05T14:30"
	paidAmount := 1200.0
	leaseID := "l1"
	receipts := &waReceipts{byID: map[string]*domain.Receipt{
		"r1": {
			ID:           "r1",
			LeaseID:      &leaseID,
			Category:     domain.ReceiptCategoryRent,
			DueDate:      "2026-10-10",
			PaidAt:       &paidAt,
			Amount:       1200,
			PaidAmount:   &paidAmount,
			TenantName:   "Maria Silva",
			PropertyName: "Casa Centro",
		},
	}}
	leases := &waLeases{byID: map[string]*domain.Lease{
		"l1": {
			ID:           "l1",
			TenantID:     "t1",
			PropertyID:   "p1",
			StartDate:    "2026-01-01",
			EndDate:      "2026-12-31",
			DueDay:       5,
			RentAmount:   1500,
			Status:       domain.LeaseStatusActive,
			TenantName:   "Maria Silva",
			PropertyName: "Casa Centro",
		},
	}}
	phone := "(11) 99999-9999"
	tenants := &waTenants{tenants: []domain.Tenant{{ID: "t1", Name: "Maria Silva", Phone: &phone}}}
	return receipts, leases, tenants
}

// ── Prévia ────────────────────────────────────────────────────────

func TestWhatsAppPreviewBuildsReceiptMessage(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	fixture := newWAFixture(t, receipts, leases, tenants)

	preview, err := WhatsAppPreview(context.Background(), fixture.deps, WhatsAppKindReceipt, "r1")
	if err != nil {
		t.Fatalf("WhatsAppPreview devolveu erro: %v", err)
	}

	if preview.Type != WhatsAppKindReceipt || preview.ID != "r1" {
		t.Fatalf("prévia = %+v", preview)
	}
	if preview.TenantName != "Maria Silva" || preview.PropertyName != "Casa Centro" {
		t.Fatalf("titular/imóvel = %q/%q", preview.TenantName, preview.PropertyName)
	}
	if preview.Phone != "(11) 99999-9999" || preview.FormattedPhone != "5511999999999" {
		t.Fatalf("telefone = %q / %q", preview.Phone, preview.FormattedPhone)
	}

	expectedMessage := strings.Join([]string{
		"recibo", "Maria Silva", "Casa Centro", "10/10/2026", "R$ 1.200,00", "R$ 1.200,00",
		"05/10/2026 14:30", string(domain.ReceiptCategoryRent), "",
		"https://aluxen.test/pdf?id=r1&token=token-receipt-r1",
		"https://aluxen.test/validar/recibo/r1?token=token-receipt-r1",
		"João Locador",
	}, "|")
	if preview.Message != expectedMessage {
		t.Fatalf("mensagem = %q\nesperado = %q", preview.Message, expectedMessage)
	}
	if !strings.HasPrefix(preview.DirectURL, "https://wa.me/5511999999999?text=recibo|") {
		t.Fatalf("link direto = %q", preview.DirectURL)
	}
}

func TestWhatsAppPreviewUsesConfiguredBaseURL(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	fixture := newWAFixture(t, receipts, leases, tenants)
	fixture.deps.Repos.Settings = &waSettings{values: map[string]string{domain.SettingAppURL: "https://aluxen.test/"}}

	preview, err := WhatsAppPreview(context.Background(), fixture.deps, WhatsAppKindReceipt, "r1")
	if err != nil {
		t.Fatalf("WhatsAppPreview devolveu erro: %v", err)
	}
	if !strings.Contains(preview.Message, "https://aluxen.test/pdf?id=r1&token=token-receipt-r1") {
		t.Fatalf("link do PDF não usou a URL configurada: %q", preview.Message)
	}
	if !strings.Contains(preview.Message, "https://aluxen.test/validar/recibo/r1?token=token-receipt-r1") {
		t.Fatalf("link de validação não usou a URL configurada: %q", preview.Message)
	}
}

func TestWhatsAppPreviewRefusesUnpaidReceipt(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	receipts.byID["r1"].PaidAt = nil
	receipts.byID["r1"].PaidAmount = nil
	fixture := newWAFixture(t, receipts, leases, tenants)

	_, err := WhatsAppPreview(context.Background(), fixture.deps, WhatsAppKindReceipt, "r1")
	if !domain.IsValidation(err) || domain.ValidationMessage(err) != "Recibo ainda não pago." {
		t.Fatalf("erro = %v, esperado validação \"Recibo ainda não pago.\"", err)
	}
}

func TestWhatsAppPreviewRejectsInvalidParameters(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	fixture := newWAFixture(t, receipts, leases, tenants)

	cases := map[string]struct{ kind, id string }{
		"tipo desconhecido": {kind: "contract", id: "r1"},
		"tipo vazio":        {kind: "", id: "r1"},
		"id vazio":          {kind: WhatsAppKindReceipt, id: ""},
	}
	for name, input := range cases {
		_, err := WhatsAppPreview(context.Background(), fixture.deps, input.kind, input.id)
		if !domain.IsValidation(err) || domain.ValidationMessage(err) != "Parâmetros inválidos." {
			t.Fatalf("%s: erro = %v, esperado validação \"Parâmetros inválidos.\"", name, err)
		}
	}
}

func TestWhatsAppPreviewPropagatesMissingReceipt(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	fixture := newWAFixture(t, receipts, leases, tenants)

	_, err := WhatsAppPreview(context.Background(), fixture.deps, WhatsAppKindReceipt, "r9")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("erro = %v, esperado ErrNotFound", err)
	}
}

func TestWhatsAppPreviewBuildsContractMessage(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	fixture := newWAFixture(t, receipts, leases, tenants)

	preview, err := WhatsAppPreview(context.Background(), fixture.deps, WhatsAppKindLease, "l1")
	if err != nil {
		t.Fatalf("WhatsAppPreview devolveu erro: %v", err)
	}

	if preview.Type != WhatsAppKindLease || preview.ID != "l1" {
		t.Fatalf("prévia = %+v", preview)
	}
	expectedMessage := strings.Join([]string{
		"contrato", "Maria Silva", "Casa Centro", "01/01/2026", "31/12/2026", "5",
		"R$ 1.500,00",
		"https://aluxen.test/contract-pdf?id=l1&token=token-lease-l1",
		"https://aluxen.test/validar/contrato/l1?token=token-lease-l1",
		"João Locador",
	}, "|")
	if preview.Message != expectedMessage {
		t.Fatalf("mensagem = %q\nesperado = %q", preview.Message, expectedMessage)
	}
	if !strings.HasPrefix(preview.DirectURL, "https://wa.me/5511999999999?text=contrato|") {
		t.Fatalf("link direto = %q", preview.DirectURL)
	}
}

func TestWhatsAppPreviewWithoutSignerFails(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	fixture := newWAFixture(t, receipts, leases, tenants)
	fixture.deps.Signer = nil

	if _, err := WhatsAppPreview(context.Background(), fixture.deps, WhatsAppKindReceipt, "r1"); err == nil {
		t.Fatal("esperava erro sem assinador de tokens")
	}
}

// ── Envio ─────────────────────────────────────────────────────────

func TestWhatsAppSendJobCompletesAndAudits(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	fixture := newWAFixture(t, receipts, leases, tenants)
	fixture.channel.messageID = "false_5511999999999@c.us_ABC"

	jobID, err := WhatsAppSendJob(context.Background(), fixture.deps, fixture.channel, WhatsAppSendRequest{
		UserID:  "u1",
		To:      "(11) 99999-9999",
		Message: "Olá!",
		Actor:   AuditContext{Origin: domain.AuditOriginWeb, IP: "127.0.0.1"},
	})
	if err != nil {
		t.Fatalf("WhatsAppSendJob devolveu erro: %v", err)
	}
	if jobID == "" {
		t.Fatal("jobId vazio")
	}

	job := waitJob(t, fixture.channel, jobID)
	if job.Status != JobDone || job.Result == nil || !job.Result.Success {
		t.Fatalf("job = %+v", job)
	}
	if job.Result.To != "5511999999999" || job.Result.Message != "Olá!" {
		t.Fatalf("resultado = %+v", job.Result)
	}
	if job.Result.Timestamp != "2026-09-21T12:00:00.000Z" {
		t.Fatalf("timestamp = %q, esperado o instante do relógio injetado", job.Result.Timestamp)
	}

	stages := fixture.channel.publishedStages()
	if len(stages) != 2 || stages[0] != StageSending || stages[1] != StageConfirming {
		t.Fatalf("etapas publicadas = %v, esperado sending e confirming", stages)
	}
	if actions := fixture.audit.actions(); len(actions) != 1 || actions[0] != domain.ActionWhatsAppSend {
		t.Fatalf("trilha = %v, esperado whatsapp.send", actions)
	}
}

func TestWhatsAppSendJobRecordsFailure(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	fixture := newWAFixture(t, receipts, leases, tenants)
	fixture.channel.sendErr = errors.New("WAHA POST /api/sendText: status 422: sessão não está WORKING")

	jobID, err := WhatsAppSendJob(context.Background(), fixture.deps, fixture.channel, WhatsAppSendRequest{
		UserID:  "u1",
		To:      "11999999999",
		Message: "Olá!",
		Actor:   AuditContext{ActorName: domain.StrPtr("passoz"), Origin: domain.AuditOriginWeb},
	})
	if err != nil {
		t.Fatalf("WhatsAppSendJob devolveu erro: %v", err)
	}

	job := waitJob(t, fixture.channel, jobID)
	if job.Status != JobError || !strings.Contains(job.Error, "status 422") {
		t.Fatalf("job = %+v, esperado falha com a razão do WAHA", job)
	}

	if actions := fixture.audit.actions(); len(actions) != 1 || actions[0] != domain.ActionWhatsAppSendFailed {
		t.Fatalf("trilha = %v, esperado whatsapp.send-failed", actions)
	}
}

func TestWhatsAppSendJobValidatesRequest(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	fixture := newWAFixture(t, receipts, leases, tenants)

	cases := []struct {
		name    string
		request WhatsAppSendRequest
		want    string
	}{
		{
			name:    "sem destinatário",
			request: WhatsAppSendRequest{UserID: "u1", Message: "Olá"},
			want:    "Destinatário e mensagem são obrigatórios.",
		},
		{
			name:    "sem mensagem",
			request: WhatsAppSendRequest{UserID: "u1", To: "11999999999"},
			want:    "Destinatário e mensagem são obrigatórios.",
		},
		{
			name:    "número curto",
			request: WhatsAppSendRequest{UserID: "u1", To: "1234", Message: "Olá"},
			want:    "Número de telefone \"1234\" inválido para envio via WhatsApp.",
		},
	}
	for _, testCase := range cases {
		_, err := WhatsAppSendJob(context.Background(), fixture.deps, fixture.channel, testCase.request)
		if !domain.IsValidation(err) || domain.ValidationMessage(err) != testCase.want {
			t.Fatalf("%s: erro = %v, esperado %q", testCase.name, err, testCase.want)
		}
	}
	if len(fixture.audit.actions()) != 0 {
		t.Fatal("erro de validação não pode auditar nem abrir job")
	}
}

func TestWhatsAppJobStatusHidesOtherOwnersJob(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	fixture := newWAFixture(t, receipts, leases, tenants)

	job := fixture.channel.Create("u1")

	if _, err := WhatsAppJobStatus(context.Background(), fixture.deps, fixture.channel, job.ID, "u1"); err != nil {
		t.Fatalf("dono do job não conseguiu consultá-lo: %v", err)
	}
	if _, err := WhatsAppJobStatus(context.Background(), fixture.deps, fixture.channel, job.ID, "u2"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("erro = %v, esperado ErrNotFound para job de outro dono", err)
	}
	if _, err := WhatsAppJobStatus(context.Background(), fixture.deps, fixture.channel, "inexistente", "u1"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("erro = %v, esperado ErrNotFound", err)
	}
}

// ── Mensagem recebida ─────────────────────────────────────────────

func TestRegisterIncomingMessageLinksTenantByPhone(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	fixture := newWAFixture(t, receipts, leases, tenants)

	err := RegisterIncomingMessage(context.Background(), fixture.deps, IncomingMessage{
		Event:       "message",
		Session:     "aluxen",
		ID:          "false_5511999999999@c.us_ABC",
		ChatID:      "5511999999999@c.us",
		Phone:       "5511999999999",
		ContactName: "Maria",
		Body:        "Já paguei o aluguel",
		Timestamp:   time.Date(2026, 9, 20, 18, 0, 0, 0, time.UTC),
		HasMedia:    false,
	})
	if err != nil {
		t.Fatalf("RegisterIncomingMessage devolveu erro: %v", err)
	}

	stored := fixture.messages.stored()
	if len(stored) != 1 {
		t.Fatalf("mensagens gravadas = %d, esperado 1", len(stored))
	}
	message := stored[0]
	if message.Direction != domain.WhatsAppDirectionIn || message.Status != domain.WhatsAppStatusReceived {
		t.Fatalf("sentido/status = %s/%s", message.Direction, message.Status)
	}
	if domain.Str(message.TenantID) != "t1" {
		t.Fatalf("tenant = %q, esperado o locatário do telefone", domain.Str(message.TenantID))
	}
	if message.Phone != "5511999999999" || message.ChatID != "5511999999999@c.us" {
		t.Fatalf("remetente gravado = %q / %q", message.Phone, message.ChatID)
	}
	if domain.Str(message.ExternalID) != "false_5511999999999@c.us_ABC" {
		t.Fatalf("externalId = %q", domain.Str(message.ExternalID))
	}
	if message.CreatedAt != "2026-09-21 12:00:00" {
		t.Fatalf("createdAt = %q, esperado o carimbo UTC do relógio injetado", message.CreatedAt)
	}
	if metadata := domain.Str(message.Metadata); !strings.Contains(metadata, "2026-09-20T18:00:00.000Z") {
		t.Fatalf("metadata sem o instante do WhatsApp: %q", metadata)
	}

	if actions := fixture.audit.actions(); len(actions) != 1 || actions[0] != domain.ActionWhatsAppMessageReceived {
		t.Fatalf("trilha = %v, esperado whatsapp.message-received", actions)
	}
}

func TestRegisterIncomingMessageMatchesPhoneWithoutCountryCode(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	phone := "11999999999"
	tenants.tenants[0].Phone = &phone
	fixture := newWAFixture(t, receipts, leases, tenants)

	err := RegisterIncomingMessage(context.Background(), fixture.deps, IncomingMessage{
		Event: "message",
		ID:    "m1",
		Phone: "5511999999999",
		Body:  "bom dia",
	})
	if err != nil {
		t.Fatalf("RegisterIncomingMessage devolveu erro: %v", err)
	}
	stored := fixture.messages.stored()
	if len(stored) != 1 || domain.Str(stored[0].TenantID) != "t1" {
		t.Fatalf("mensagem = %+v, esperado vínculo com o locatário pelo sufixo do número", stored)
	}
}

func TestRegisterIncomingMessageWithoutTenant(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	fixture := newWAFixture(t, receipts, leases, tenants)

	err := RegisterIncomingMessage(context.Background(), fixture.deps, IncomingMessage{
		Event: "message",
		ID:    "m1",
		Phone: "5521977776666",
		Body:  "número desconhecido",
	})
	if err != nil {
		t.Fatalf("RegisterIncomingMessage devolveu erro: %v", err)
	}
	stored := fixture.messages.stored()
	if len(stored) != 1 || stored[0].TenantID != nil {
		t.Fatalf("mensagem = %+v, esperado tenant nulo", stored)
	}
	if actions := fixture.audit.actions(); len(actions) != 1 {
		t.Fatalf("trilha = %v, esperado uma linha mesmo sem locatário", actions)
	}
}

func TestRegisterIncomingMessageIsIdempotent(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	fixture := newWAFixture(t, receipts, leases, tenants)

	message := IncomingMessage{Event: "message", ID: "m1", Phone: "5511999999999", Body: "já paguei"}
	for attempt := range 2 {
		if err := RegisterIncomingMessage(context.Background(), fixture.deps, message); err != nil {
			t.Fatalf("tentativa %d: erro %v", attempt+1, err)
		}
	}

	if stored := fixture.messages.stored(); len(stored) != 1 {
		t.Fatalf("mensagens = %d, esperado 1 (reentrega do webhook não duplica)", len(stored))
	}
	if actions := fixture.audit.actions(); len(actions) != 1 {
		t.Fatalf("trilha = %v, esperado uma linha", actions)
	}
}

func TestRegisterOurOwnEchoIsStoredWithoutAudit(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	fixture := newWAFixture(t, receipts, leases, tenants)

	err := RegisterIncomingMessage(context.Background(), fixture.deps, IncomingMessage{
		Event:  "message.any",
		ID:     "true_5511999999999@c.us_ABC",
		ChatID: "5511999999999@c.us",
		Phone:  "5511999999999",
		Body:   "Segue o recibo",
		FromMe: true,
	})
	if err != nil {
		t.Fatalf("RegisterIncomingMessage devolveu erro: %v", err)
	}

	stored := fixture.messages.stored()
	if len(stored) != 1 || stored[0].Direction != domain.WhatsAppDirectionOut || stored[0].Status != domain.WhatsAppStatusSent {
		t.Fatalf("mensagem = %+v, esperado sentido de saída", stored)
	}
	if actions := fixture.audit.actions(); len(actions) != 0 {
		t.Fatalf("trilha = %v, esperado nada: o envio já foi auditado no job", actions)
	}
}

func TestRegisterIncomingMessageWithoutStore(t *testing.T) {
	receipts, leases, tenants := paidReceiptFixture()
	fixture := newWAFixture(t, receipts, leases, tenants)
	fixture.deps.Repos.WhatsApp = nil

	if err := RegisterIncomingMessage(context.Background(), fixture.deps, IncomingMessage{ID: "m1"}); err == nil {
		t.Fatal("esperava erro sem persistência de mensagens")
	}
}
