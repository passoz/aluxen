package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/stretchr/testify/require"
)

// ── Dublês do onboarding por documento ─────────────────────────────

type intakeClock struct{ now time.Time }

func (c *intakeClock) Now() time.Time { return c.now }

type intakeIDs struct{ next int }

func (g *intakeIDs) New() string {
	g.next++
	return fmt.Sprintf("id-%d", g.next)
}

// intakeRepo guarda as propostas como o banco faria.
type intakeRepo struct{ items []domain.DocumentIntake }

func (r *intakeRepo) Create(_ context.Context, intake *domain.DocumentIntake) error {
	r.items = append(r.items, *intake)
	return nil
}

func (r *intakeRepo) ByID(_ context.Context, id string) (*domain.DocumentIntake, error) {
	for index := range r.items {
		if r.items[index].ID == id {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *intakeRepo) List(context.Context, int) ([]domain.DocumentIntake, error) {
	return r.items, nil
}

func (r *intakeRepo) ListByStatus(_ context.Context, status domain.IntakeStatus, limit int) ([]domain.DocumentIntake, error) {
	items := make([]domain.DocumentIntake, 0, len(r.items))
	for _, intake := range r.items {
		if intake.Status == status {
			items = append(items, intake)
		}
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *intakeRepo) Update(_ context.Context, intake *domain.DocumentIntake) error {
	for index := range r.items {
		if r.items[index].ID == intake.ID {
			r.items[index] = *intake
			return nil
		}
	}
	return domain.ErrNotFound
}

// intakeJobs é a fila de trabalho em memória.
type intakeJobs struct{ items []domain.Job }

func (r *intakeJobs) Create(_ context.Context, job *domain.Job) error {
	r.items = append(r.items, *job)
	return nil
}

func (r *intakeJobs) ByID(_ context.Context, id string) (*domain.Job, error) {
	for index := range r.items {
		if r.items[index].ID == id {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *intakeJobs) ListByStatus(_ context.Context, status domain.JobStatus, limit int) ([]domain.Job, error) {
	items := make([]domain.Job, 0, len(r.items))
	for _, job := range r.items {
		if job.Status == status {
			items = append(items, job)
		}
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *intakeJobs) List(context.Context, int) ([]domain.Job, error) { return r.items, nil }

func (r *intakeJobs) Update(_ context.Context, job *domain.Job) error {
	for index := range r.items {
		if r.items[index].ID == job.ID {
			r.items[index] = *job
			return nil
		}
	}
	return domain.ErrNotFound
}

type intakeTenants struct{ items []domain.Tenant }

func (r *intakeTenants) List(context.Context) ([]domain.Tenant, error) { return r.items, nil }

func (r *intakeTenants) ByID(_ context.Context, id string) (*domain.Tenant, error) {
	for index := range r.items {
		if r.items[index].ID == id {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *intakeTenants) ByCPF(context.Context, string) (*domain.Tenant, error) {
	return nil, domain.ErrNotFound
}

func (r *intakeTenants) Create(_ context.Context, tenant *domain.Tenant) error {
	r.items = append(r.items, *tenant)
	return nil
}

func (r *intakeTenants) Update(_ context.Context, tenant *domain.Tenant) error {
	for index := range r.items {
		if r.items[index].ID == tenant.ID {
			r.items[index] = *tenant
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *intakeTenants) UpdatePassword(context.Context, string, string, string, string) error {
	return nil
}

func (r *intakeTenants) Delete(context.Context, string) error { return nil }

func (r *intakeTenants) Count(context.Context) (int, error) { return len(r.items), nil }

type intakeProperties struct{ items []domain.Property }

func (r *intakeProperties) List(context.Context) ([]domain.Property, error) { return r.items, nil }

func (r *intakeProperties) ByID(_ context.Context, id string) (*domain.Property, error) {
	for index := range r.items {
		if r.items[index].ID == id {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *intakeProperties) Create(_ context.Context, property *domain.Property) error {
	r.items = append(r.items, *property)
	return nil
}

func (r *intakeProperties) Update(_ context.Context, property *domain.Property) error {
	for index := range r.items {
		if r.items[index].ID == property.ID {
			r.items[index] = *property
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *intakeProperties) Delete(context.Context, string) error { return nil }

func (r *intakeProperties) Count(context.Context) (int, error) { return len(r.items), nil }

// intakeLeases implementa só o que o cadastro por documento usa; o embed cobre o
// resto do port.
type intakeLeases struct {
	domain.LeaseRepository
	items []domain.Lease
}

func (r *intakeLeases) ByID(_ context.Context, id string) (*domain.Lease, error) {
	for index := range r.items {
		if r.items[index].ID == id {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *intakeLeases) ByTenant(_ context.Context, tenantID string) ([]domain.Lease, error) {
	items := make([]domain.Lease, 0, len(r.items))
	for _, lease := range r.items {
		if lease.TenantID == tenantID {
			items = append(items, lease)
		}
	}
	return items, nil
}

func (r *intakeLeases) Overlapping(context.Context, string, string, string, string) ([]domain.Lease, error) {
	return nil, nil
}

func (r *intakeLeases) Create(_ context.Context, lease *domain.Lease) error {
	r.items = append(r.items, *lease)
	return nil
}

type intakeAudit struct{ entries []domain.AuditLog }

func (r *intakeAudit) Insert(_ context.Context, entry *domain.AuditLog) error {
	r.entries = append(r.entries, *entry)
	return nil
}

func (r *intakeAudit) List(context.Context, domain.AuditFilter) ([]domain.AuditLog, error) {
	return r.entries, nil
}

func (r *intakeAudit) Count(context.Context, domain.AuditFilter) (int, error) {
	return len(r.entries), nil
}

func (r *intakeAudit) Actors(context.Context) ([]string, error) { return nil, nil }

type intakeAttachments struct{ saved map[string][]byte }

func newIntakeAttachments() *intakeAttachments {
	return &intakeAttachments{saved: map[string][]byte{}}
}

func (s *intakeAttachments) Save(prefix, filename string, data []byte) (string, error) {
	name := prefix + "/" + filename
	s.saved[name] = data
	return name, nil
}

func (s *intakeAttachments) Open(name string) ([]byte, error) {
	data, ok := s.saved[name]
	if !ok {
		return nil, errors.New("anexo ausente")
	}
	return data, nil
}

func (s *intakeAttachments) Delete(name string) error {
	delete(s.saved, name)
	return nil
}

// intakeExtractor é o provedor de leitura de mentira: devolve o que o teste
// mandar, sem chamar rede nenhuma.
type intakeExtractor struct {
	lease  domain.LeaseIntakeFields
	person domain.PersonIntakeFields
	raw    string
	err    error
	calls  []application.DocumentFile
}

func (e *intakeExtractor) ExtractLeaseDocument(_ context.Context, doc application.DocumentFile) (domain.LeaseIntakeFields, string, error) {
	e.calls = append(e.calls, doc)
	if e.err != nil {
		return domain.LeaseIntakeFields{}, e.raw, e.err
	}
	return e.lease, e.raw, nil
}

func (e *intakeExtractor) ExtractPersonDocument(_ context.Context, doc application.DocumentFile) (domain.PersonIntakeFields, string, error) {
	e.calls = append(e.calls, doc)
	if e.err != nil {
		return domain.PersonIntakeFields{}, e.raw, e.err
	}
	return e.person, e.raw, nil
}

// ── Montagem ───────────────────────────────────────────────────────

type intakeFixture struct {
	deps        application.Deps
	intakes     *intakeRepo
	jobs        *intakeJobs
	tenants     *intakeTenants
	properties  *intakeProperties
	leases      *intakeLeases
	audit       *intakeAudit
	attachments *intakeAttachments
	extractor   *intakeExtractor
	clock       *intakeClock
}

func newIntakeFixture(t *testing.T) *intakeFixture {
	t.Helper()

	clock := &intakeClock{now: time.Date(2026, 9, 21, 15, 30, 0, 0, time.UTC)}
	intakes := &intakeRepo{}
	jobs := &intakeJobs{}
	tenants := &intakeTenants{}
	properties := &intakeProperties{}
	leases := &intakeLeases{}
	audit := &intakeAudit{}
	attachments := newIntakeAttachments()
	extractor := &intakeExtractor{
		lease: domain.LeaseIntakeFields{
			TenantName: "Carlos Drummond", TenantCPF: "111.222.333-44", TenantEmail: "carlos@poesia.com.br",
			TenantPhone: "(22) 99888-7766", PropertyName: "Casa de Praia 202", PropertyCity: "Cabo Frio",
			PropertyState: "RJ", StartDate: "2019-03-01", EndDate: "2022-02-28", DueDay: 10,
			RentAmount: 2500, AdjustIndex: "IGP-M", Guarantee: "fiador", Confidence: 0.93,
		},
		person: domain.PersonIntakeFields{
			Name: "Clarice Lispector", CPF: "222.333.444-55", Email: "clarice@letras.com.br",
			Phone: "(21) 97777-6666", Address: "Rua das Acácias, 45", City: "Búzios", State: "RJ",
			Zip: "28950-000", Confidence: 0.88,
		},
		raw: `{"tenantName":"Carlos Drummond"}`,
	}

	deps := application.Deps{
		Repos: application.Repositories{
			Intakes:    intakes,
			Jobs:       jobs,
			Tenants:    tenants,
			Properties: properties,
			Leases:     leases,
			Audit:      audit,
		},
		Clock:       clock,
		IDs:         &intakeIDs{},
		Attachments: attachments,
		Intakes:     extractor,
		Opts:        application.Options{AIConfidenceThreshold: 0.9},
		Log:         slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})),
	}
	return &intakeFixture{
		deps: deps, intakes: intakes, jobs: jobs, tenants: tenants, properties: properties,
		leases: leases, audit: audit, attachments: attachments, extractor: extractor, clock: clock,
	}
}

func (f *intakeFixture) context() (context.Context, application.AuditContext) {
	actor := "passoz"
	return context.Background(), application.AuditContext{Origin: domain.AuditOriginWeb, ActorName: &actor}
}

// submit envia o contrato antigo e devolve a proposta criada.
func (f *intakeFixture) submit(t *testing.T, kind domain.IntakeKind) *domain.DocumentIntake {
	t.Helper()

	ctx, actx := f.context()
	intake, err := application.SubmitDocumentIntake(ctx, f.deps, actx, application.IntakeInput{
		Kind:     kind,
		FileName: "contrato-2019.pdf",
		Mime:     "application/pdf",
		Bytes:    []byte("%PDF-1.4 contrato antigo"),
	})
	require.NoError(t, err, "enviar documento do onboarding")
	return intake
}

// ── Envio ──────────────────────────────────────────────────────────

// Nada é gravado antes da confirmação humana: o envio só guarda o arquivo e
// cria a proposta pendente.
func TestSubmitDocumentIntakeCreatesProposalOnly(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindLease)

	require.Equal(t, domain.IntakeStatusPending, intake.Status)
	require.Equal(t, domain.IntakeKindLease, intake.Kind)
	require.Equal(t, "contrato-2019.pdf", domain.Str(intake.FileName))
	require.Equal(t, "intakes/contrato-2019.pdf", domain.Str(intake.FilePath))
	require.Nil(t, intake.Extracted, "o envio não pode gravar leitura")
	require.Nil(t, intake.Confidence)

	require.Len(t, fixture.intakes.items, 1, "uma proposta pendente")
	require.Empty(t, fixture.tenants.items, "nenhum locatário antes da conferência")
	require.Empty(t, fixture.properties.items, "nenhum imóvel antes da conferência")
	require.Empty(t, fixture.leases.items, "nenhum contrato antes da conferência")
	require.Empty(t, fixture.audit.entries, "nada na trilha antes da conferência")
	require.Contains(t, fixture.attachments.saved, "intakes/contrato-2019.pdf")
}

func TestSubmitDocumentIntakeQueuesReadingJob(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindLease)

	require.Len(t, fixture.jobs.items, 1, "a leitura precisa entrar na fila")
	job := fixture.jobs.items[0]
	require.Equal(t, domain.JobKindDocumentIntake, job.Kind)
	require.Equal(t, domain.JobStatusQueued, job.Status)
	require.NotNil(t, job.Payload)
	require.Contains(t, *job.Payload, intake.ID, "o trabalho carrega a proposta")
}

func TestSubmitDocumentIntakeRejectsInvalidKind(t *testing.T) {
	fixture := newIntakeFixture(t)
	ctx, actx := fixture.context()

	_, err := application.SubmitDocumentIntake(ctx, fixture.deps, actx, application.IntakeInput{
		Kind: "escritura", FileName: "doc.pdf", Mime: "application/pdf", Bytes: []byte("x"),
	})
	require.Error(t, err)
	require.Equal(t, "Tipo de documento não aceito.", domain.ValidationMessage(err))
	require.Empty(t, fixture.intakes.items)
	require.Empty(t, fixture.jobs.items)
}

func TestSubmitDocumentIntakeRejectsUnsupportedFile(t *testing.T) {
	fixture := newIntakeFixture(t)
	ctx, actx := fixture.context()

	_, err := application.SubmitDocumentIntake(ctx, fixture.deps, actx, application.IntakeInput{
		Kind: domain.IntakeKindLease, FileName: "planilha.xlsx",
		Mime: "application/vnd.ms-excel", Bytes: []byte("x"),
	})
	require.Error(t, err)
	require.Equal(t, "Formato de arquivo não suportado. Envie PDF, JPG, PNG ou WEBP.", domain.ValidationMessage(err))

	_, err = application.SubmitDocumentIntake(ctx, fixture.deps, actx, application.IntakeInput{
		Kind: domain.IntakeKindLease, FileName: "contrato.pdf", Mime: "application/pdf",
	})
	require.Error(t, err, "arquivo vazio")
	require.Empty(t, fixture.intakes.items)
	require.Empty(t, fixture.jobs.items)
}

func TestSubmitDocumentIntakeRejectsOversizedFile(t *testing.T) {
	fixture := newIntakeFixture(t)
	ctx, actx := fixture.context()

	_, err := application.SubmitDocumentIntake(ctx, fixture.deps, actx, application.IntakeInput{
		Kind: domain.IntakeKindLease, FileName: "contrato.pdf", Mime: "application/pdf",
		Bytes: make([]byte, 20<<20+1),
	})
	require.Error(t, err)
	require.Equal(t, "O arquivo pode ter no máximo 20 MB.", domain.ValidationMessage(err))
	require.Empty(t, fixture.intakes.items)
}

// ── Leitura ────────────────────────────────────────────────────────

func TestProcessDocumentIntakeStoresReadingWithoutCreatingCadastro(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindLease)

	processed, err := application.ProcessDocumentIntake(context.Background(), fixture.deps, intake.ID)
	require.NoError(t, err)
	require.Equal(t, domain.IntakeStatusPending, processed.Status, "a leitura não confirma nada")
	require.NotNil(t, processed.Extracted)
	require.NotNil(t, processed.Confidence)
	require.InDelta(t, 0.93, *processed.Confidence, 0.0001)
	require.Equal(t, `{"tenantName":"Carlos Drummond"}`, domain.Str(processed.RawResponse))

	var fields domain.LeaseIntakeFields
	require.NoError(t, json.Unmarshal([]byte(*processed.Extracted), &fields))
	require.Equal(t, "Carlos Drummond", fields.TenantName)
	require.Equal(t, 2500.0, fields.RentAmount)

	require.Len(t, fixture.extractor.calls, 1, "a leitura roda uma vez")
	require.Equal(t, "application/pdf", fixture.extractor.calls[0].Mime)
	require.Equal(t, []byte("%PDF-1.4 contrato antigo"), fixture.extractor.calls[0].Bytes)

	require.Empty(t, fixture.tenants.items)
	require.Empty(t, fixture.properties.items)
	require.Empty(t, fixture.leases.items)
	require.Empty(t, fixture.audit.entries, "a leitura não cria cadastro nem audita criação")
}

func TestProcessDocumentIntakeReadsPersonDocument(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindIDDocument)

	processed, err := application.ProcessDocumentIntake(context.Background(), fixture.deps, intake.ID)
	require.NoError(t, err)

	var fields domain.PersonIntakeFields
	require.NoError(t, json.Unmarshal([]byte(*processed.Extracted), &fields))
	require.Equal(t, "Clarice Lispector", fields.Name)
	require.InDelta(t, 0.88, *processed.Confidence, 0.0001)
}

func TestProcessDocumentIntakeWithoutProviderKeepsProposalForManualFill(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindLease)
	fixture.deps.Intakes = nil

	processed, err := application.ProcessDocumentIntake(context.Background(), fixture.deps, intake.ID)
	require.NoError(t, err, "instância sem leitura assistida não é falha do trabalho")
	require.Equal(t, domain.IntakeStatusPending, processed.Status)
	require.Nil(t, processed.Extracted)
	require.Contains(t, domain.Str(processed.Notes), "Leitura assistida não configurada")
	require.Empty(t, fixture.audit.entries)
}

func TestProcessDocumentIntakeFailsOnProviderError(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindLease)
	fixture.extractor.err = errors.New("provedor fora do ar")

	_, err := application.ProcessDocumentIntake(context.Background(), fixture.deps, intake.ID)
	require.Error(t, err, "falha do provedor é falha do trabalho")

	stored, err := fixture.intakes.ByID(context.Background(), intake.ID)
	require.NoError(t, err)
	require.Equal(t, domain.IntakeStatusPending, stored.Status)
	require.Nil(t, stored.Extracted, "saída fora de formato nunca vira dado")
}

func TestProcessDocumentIntakeJobReadsPayload(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindLease)
	require.Len(t, fixture.jobs.items, 1)

	require.NoError(t, application.ProcessDocumentIntakeJob(context.Background(), fixture.deps, fixture.jobs.items[0]))

	stored, err := fixture.intakes.ByID(context.Background(), intake.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.Extracted, "o handler da fila lê o documento da proposta")

	missing := domain.Job{ID: "job-2", Kind: domain.JobKindDocumentIntake, Payload: domain.StrPtr(`{"intakeId":"ausente"}`)}
	require.Error(t, application.ProcessDocumentIntakeJob(context.Background(), fixture.deps, missing))
}

func TestProcessDocumentIntakeSkipsAlreadyReviewedProposal(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindLease)
	ctx, actx := fixture.context()
	require.NoError(t, application.DiscardDocumentIntake(ctx, fixture.deps, actx, intake.ID, "documento ilegível"))

	processed, err := application.ProcessDocumentIntake(context.Background(), fixture.deps, intake.ID)
	require.NoError(t, err)
	require.Equal(t, domain.IntakeStatusDiscarded, processed.Status)
	require.Empty(t, fixture.extractor.calls, "proposta já conferida não é relida")
}

// ── Conferência ────────────────────────────────────────────────────

func TestLoadDocumentIntakeFormatsFieldsForConference(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindLease)
	_, err := application.ProcessDocumentIntake(context.Background(), fixture.deps, intake.ID)
	require.NoError(t, err)

	view, err := application.LoadDocumentIntake(context.Background(), fixture.deps, intake.ID)
	require.NoError(t, err)
	require.Equal(t, "Contrato antigo", view.KindLabel)
	require.Equal(t, "Aguardando conferência", view.StatusLabel)
	require.Equal(t, "due", view.StatusTone)
	require.Equal(t, "93%", view.ConfidenceLabel)
	require.False(t, view.LowConfidence)
	require.Empty(t, view.Missing, "a leitura preencheu os campos que importam")

	values := map[string]string{}
	missing := map[string]bool{}
	for _, field := range view.Lease {
		values[field.Name] = field.Value
		missing[field.Name] = field.Missing
	}
	require.Equal(t, "01/03/2019", values["start_date"], "data em dd/mm/aaaa")
	require.Equal(t, "28/02/2022", values["end_date"])
	require.Equal(t, "R$ 2.500,00", values["rent_amount"], "valor em reais")
	require.Equal(t, "10", values["due_day"])
	require.Equal(t, "Carlos Drummond", values["tenant_name"])
	require.Equal(t, "cpf", fieldMask(view.Lease, "tenant_cpf"))
	require.True(t, fieldDate(view.Lease, "start_date"), "data usa o datepicker do painel")
	require.False(t, missing["tenant_name"])
}

func TestLoadDocumentIntakeHighlightsEmptyFields(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindIDDocument)
	// Leitura parcial: só o nome veio do documento.
	fixture.extractor.person = domain.PersonIntakeFields{Name: "Clarice Lispector", Confidence: 0.4}
	_, err := application.ProcessDocumentIntake(context.Background(), fixture.deps, intake.ID)
	require.NoError(t, err)

	view, err := application.LoadDocumentIntake(context.Background(), fixture.deps, intake.ID)
	require.NoError(t, err)
	require.True(t, view.LowConfidence, "confiança abaixo do limiar avisa na tela")
	require.Equal(t, []string{"CPF"}, view.Missing)
	require.Equal(t, "40%", view.ConfidenceLabel)
}

func TestLoadDocumentIntakeWithoutReadingRendersEmptyForm(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindAddressProof)

	view, err := application.LoadDocumentIntake(context.Background(), fixture.deps, intake.ID)
	require.NoError(t, err)
	require.Equal(t, "-", view.ConfidenceLabel)
	require.Equal(t, []string{"Nome", "CPF"}, view.Missing)
	require.Len(t, view.Person, 8, "o formulário sai vazio, pronto para preencher à mão")
}

func TestLoadDocumentIntakeRejectsUnknownProposal(t *testing.T) {
	fixture := newIntakeFixture(t)

	_, err := application.LoadDocumentIntake(context.Background(), fixture.deps, "intake-ausente")
	require.Error(t, err)
	require.Equal(t, "Proposta de cadastro não encontrada.", domain.ValidationMessage(err))
}

func TestConfirmDocumentIntakeCreatesCadastroAndAudits(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindLease)
	_, err := application.ProcessDocumentIntake(context.Background(), fixture.deps, intake.ID)
	require.NoError(t, err)

	ctx, actx := fixture.context()
	result, err := application.ConfirmDocumentIntake(ctx, fixture.deps, actx, application.ConfirmInput{
		IntakeID:       intake.ID,
		Kind:           domain.IntakeKindLease,
		CreateTenant:   true,
		CreateProperty: true,
		CreateLease:    true,
		Lease: domain.LeaseIntakeFields{
			TenantName: "Carlos Drummond", TenantCPF: "11122233344", TenantEmail: "carlos@poesia.com.br",
			TenantPhone: "22998887766", PropertyName: "Casa de Praia 202", PropertyCity: "Cabo Frio",
			PropertyState: "RJ", StartDate: "01/03/2019", EndDate: "28/02/2022", DueDay: 10, RentAmount: 2500,
		},
	})
	require.NoError(t, err)

	require.Len(t, fixture.tenants.items, 1)
	require.Equal(t, "Carlos Drummond", fixture.tenants.items[0].Name)
	require.Equal(t, "111.222.333-44", domain.Str(fixture.tenants.items[0].CPF))
	require.Equal(t, "carlos@poesia.com.br", fixture.tenants.items[0].Email)
	require.Equal(t, result.TenantID, fixture.tenants.items[0].ID)

	require.Len(t, fixture.properties.items, 1)
	require.Equal(t, "Casa de Praia 202", fixture.properties.items[0].Name)
	require.Equal(t, "Cabo Frio", fixture.properties.items[0].City)

	require.Len(t, fixture.leases.items, 1)
	lease := fixture.leases.items[0]
	require.Equal(t, result.TenantID, lease.TenantID)
	require.Equal(t, result.PropertyID, lease.PropertyID)
	require.Equal(t, "2019-03-01", lease.StartDate, "dd/mm/aaaa vira o formato do banco")
	require.Equal(t, "2022-02-28", lease.EndDate)
	require.Equal(t, 10, lease.DueDay)
	require.Equal(t, 2500.0, lease.RentAmount)
	require.Equal(t, domain.LeaseStatusActive, lease.Status)
	require.Equal(t, result.LeaseID, lease.ID)

	stored, err := fixture.intakes.ByID(ctx, intake.ID)
	require.NoError(t, err)
	require.Equal(t, domain.IntakeStatusConfirmed, stored.Status)
	require.Equal(t, "lease", domain.Str(stored.CreatedEntity))
	require.Equal(t, result.LeaseID, domain.Str(stored.CreatedEntityID))

	// A trilha registra a criação, o arquivo usado e o que foi lido.
	require.Len(t, fixture.audit.entries, 4, "locatário, imóvel, contrato e o cadastro por documento")
	entry := fixture.audit.entries[3]
	require.Equal(t, domain.ActionDocumentIntakeCreated, entry.Action)
	require.Equal(t, domain.EntityIntake, entry.Entity)
	require.Equal(t, intake.ID, domain.Str(entry.EntityID))
	require.Equal(t, "Contrato antigo · contrato-2019.pdf", domain.Str(entry.EntityLabel))
	require.Contains(t, entry.Summary, "contrato-2019.pdf", "o resumo mostra o arquivo usado")
	require.Contains(t, entry.Summary, "locatário, imóvel, contrato")

	var metadata map[string]any
	require.NoError(t, json.Unmarshal([]byte(domain.Str(entry.Metadata)), &metadata))
	require.Equal(t, "contrato-2019.pdf", metadata["fileName"])
	require.Equal(t, "lease", metadata["kind"])
	require.Equal(t, result.LeaseID, metadata["leaseId"])
	require.InDelta(t, 0.93, metadata["confidence"].(float64), 0.0001)
}

func TestConfirmDocumentIntakeReusesExistingCadastro(t *testing.T) {
	fixture := newIntakeFixture(t)
	fixture.tenants.items = append(fixture.tenants.items, domain.Tenant{ID: "tenant-9", Name: "Existente", Email: "e@x.com"})
	fixture.properties.items = append(fixture.properties.items, domain.Property{ID: "property-9", Name: "Imóvel existente"})
	intake := fixture.submit(t, domain.IntakeKindLease)

	ctx, actx := fixture.context()
	result, err := application.ConfirmDocumentIntake(ctx, fixture.deps, actx, application.ConfirmInput{
		IntakeID:    intake.ID,
		Kind:        domain.IntakeKindLease,
		CreateLease: true,
		TenantID:    "tenant-9",
		PropertyID:  "property-9",
		Lease: domain.LeaseIntakeFields{
			StartDate: "2019-03-01", EndDate: "2022-02-28", DueDay: 10, RentAmount: 2500,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "tenant-9", result.TenantID)
	require.Equal(t, "property-9", result.PropertyID)
	require.Len(t, fixture.tenants.items, 1, "cadastro existente não é duplicado")
	require.Len(t, fixture.properties.items, 1)
	require.Len(t, fixture.leases.items, 1)
	require.Equal(t, "tenant-9", fixture.leases.items[0].TenantID)
}

func TestConfirmDocumentIntakeRejectsUnknownExistingCadastro(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindLease)
	ctx, actx := fixture.context()

	_, err := application.ConfirmDocumentIntake(ctx, fixture.deps, actx, application.ConfirmInput{
		IntakeID: intake.ID, CreateLease: true, TenantID: "tenant-fantasma", PropertyID: "property-fantasma",
	})
	require.Error(t, err)
	require.Equal(t, "Locatário informado não encontrado.", domain.ValidationMessage(err))
	require.Empty(t, fixture.leases.items)
	require.Empty(t, fixture.audit.entries)
}

// Recusa criar contrato sem locatário e imóvel — o documento sozinho não
// cadastra ninguém.
func TestConfirmDocumentIntakeRejectsLeaseWithoutTenantAndProperty(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindLease)
	ctx, actx := fixture.context()

	_, err := application.ConfirmDocumentIntake(ctx, fixture.deps, actx, application.ConfirmInput{
		IntakeID:    intake.ID,
		Kind:        domain.IntakeKindLease,
		CreateLease: true,
		Lease:       domain.LeaseIntakeFields{StartDate: "2019-03-01", EndDate: "2022-02-28", DueDay: 10, RentAmount: 2500},
	})
	require.Error(t, err)
	require.Equal(t, "Para criar o contrato, informe o locatário e o imóvel.", domain.ValidationMessage(err))
	require.Empty(t, fixture.leases.items)
	require.Empty(t, fixture.tenants.items)
	require.Empty(t, fixture.properties.items)

	stored, err := fixture.intakes.ByID(ctx, intake.ID)
	require.NoError(t, err)
	require.Equal(t, domain.IntakeStatusPending, stored.Status, "recusa não confirma a proposta")
	require.Empty(t, fixture.audit.entries)
}

func TestConfirmDocumentIntakeRequiresSomethingToCreate(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindLease)
	ctx, actx := fixture.context()

	_, err := application.ConfirmDocumentIntake(ctx, fixture.deps, actx, application.ConfirmInput{IntakeID: intake.ID})
	require.Error(t, err)
	require.Equal(t, "Escolha o que criar a partir do documento.", domain.ValidationMessage(err))
}

func TestConfirmDocumentIntakeCreatesTenantFromPersonDocument(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindAddressProof)
	_, err := application.ProcessDocumentIntake(context.Background(), fixture.deps, intake.ID)
	require.NoError(t, err)

	ctx, actx := fixture.context()
	result, err := application.ConfirmDocumentIntake(ctx, fixture.deps, actx, application.ConfirmInput{
		IntakeID:       intake.ID,
		Kind:           domain.IntakeKindAddressProof,
		CreateTenant:   true,
		CreateProperty: true,
		Person: domain.PersonIntakeFields{
			Name: "Clarice Lispector", CPF: "22233344455", Email: "clarice@letras.com.br",
			Phone: "21977776666", Address: "Rua das Acácias, 45", City: "Búzios", State: "RJ", Zip: "28950000",
		},
	})
	require.NoError(t, err)

	require.Len(t, fixture.tenants.items, 1)
	require.Equal(t, "Clarice Lispector", fixture.tenants.items[0].Name)
	require.Len(t, fixture.properties.items, 1)
	require.Equal(t, "Rua das Acácias, 45", fixture.properties.items[0].Address)
	require.Equal(t, "28950-000", domain.Str(fixture.properties.items[0].Zip))
	require.Empty(t, fixture.leases.items, "comprovante de residência não cria contrato")
	require.Equal(t, "property", domain.Str(result.Intake.CreatedEntity))
}

func TestConfirmDocumentIntakeRejectsPropertyFromIdentityDocument(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindIDDocument)
	ctx, actx := fixture.context()

	_, err := application.ConfirmDocumentIntake(ctx, fixture.deps, actx, application.ConfirmInput{
		IntakeID:       intake.ID,
		Kind:           domain.IntakeKindIDDocument,
		CreateProperty: true,
		Person:         domain.PersonIntakeFields{Name: "Clarice", Email: "c@x.com"},
	})
	require.Error(t, err)
	require.Equal(t, "Documento de identidade não descreve imóvel.", domain.ValidationMessage(err))
	require.Empty(t, fixture.properties.items)
}

func TestConfirmDocumentIntakeRefusesSecondReview(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindIDDocument)
	ctx, actx := fixture.context()

	input := application.ConfirmInput{
		IntakeID:     intake.ID,
		Kind:         domain.IntakeKindIDDocument,
		CreateTenant: true,
		Person:       domain.PersonIntakeFields{Name: "Clarice", Email: "c@x.com"},
	}
	_, err := application.ConfirmDocumentIntake(ctx, fixture.deps, actx, input)
	require.NoError(t, err)

	_, err = application.ConfirmDocumentIntake(ctx, fixture.deps, actx, input)
	require.Error(t, err)
	require.Equal(t, "Esta proposta já foi conferida.", domain.ValidationMessage(err))
	require.Len(t, fixture.tenants.items, 1, "a segunda conferência não cria de novo")
}

func TestDiscardDocumentIntakeRecordsReason(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindLease)
	ctx, actx := fixture.context()

	require.NoError(t, application.DiscardDocumentIntake(ctx, fixture.deps, actx, intake.ID, "  contrato de outro imóvel  "))

	stored, err := fixture.intakes.ByID(ctx, intake.ID)
	require.NoError(t, err)
	require.Equal(t, domain.IntakeStatusDiscarded, stored.Status)
	require.Equal(t, "contrato de outro imóvel", domain.Str(stored.Notes))
	require.Empty(t, fixture.tenants.items)
	require.Empty(t, fixture.leases.items)
	require.Empty(t, fixture.audit.entries, "descarte não cria cadastro")
}

func TestDiscardDocumentIntakeRequiresReason(t *testing.T) {
	fixture := newIntakeFixture(t)
	intake := fixture.submit(t, domain.IntakeKindLease)
	ctx, actx := fixture.context()

	err := application.DiscardDocumentIntake(ctx, fixture.deps, actx, intake.ID, "   ")
	require.Error(t, err)
	require.Equal(t, "Informe o motivo do descarte.", domain.ValidationMessage(err))

	stored, err := fixture.intakes.ByID(ctx, intake.ID)
	require.NoError(t, err)
	require.Equal(t, domain.IntakeStatusPending, stored.Status)
}

func TestPendingDocumentIntakesListsProposalsForConference(t *testing.T) {
	fixture := newIntakeFixture(t)
	first := fixture.submit(t, domain.IntakeKindLease)
	fixture.submit(t, domain.IntakeKindIDDocument)
	ctx, actx := fixture.context()
	require.NoError(t, application.DiscardDocumentIntake(ctx, fixture.deps, actx, first.ID, "duplicado"))

	views, err := application.PendingDocumentIntakes(context.Background(), fixture.deps, 0)
	require.NoError(t, err)
	require.Len(t, views, 1, "só as propostas aguardando conferência")
	require.Equal(t, "Documento de identidade", views[0].KindLabel)
	require.Contains(t, strings.Join(viewFieldNames(views[0].Person), ","), "name")
}

func viewFieldNames(fields []application.IntakeField) []string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.Name)
	}
	return names
}

func fieldMask(fields []application.IntakeField, name string) string {
	for _, field := range fields {
		if field.Name == name {
			return field.Mask
		}
	}
	return ""
}

func fieldDate(fields []application.IntakeField, name string) bool {
	for _, field := range fields {
		if field.Name == name {
			return field.Date
		}
	}
	return false
}
