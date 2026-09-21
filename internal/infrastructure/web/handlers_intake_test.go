package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/config"
	"github.com/stretchr/testify/require"
)

// ── Dublês do onboarding por documento ─────────────────────────────

type intakeIntakes struct{ items []domain.DocumentIntake }

func (r *intakeIntakes) Create(_ context.Context, intake *domain.DocumentIntake) error {
	r.items = append(r.items, *intake)
	return nil
}

func (r *intakeIntakes) ByID(_ context.Context, id string) (*domain.DocumentIntake, error) {
	for index := range r.items {
		if r.items[index].ID == id {
			found := r.items[index]
			return &found, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *intakeIntakes) List(context.Context, int) ([]domain.DocumentIntake, error) {
	return r.items, nil
}

func (r *intakeIntakes) ListByStatus(_ context.Context, status domain.IntakeStatus, limit int) ([]domain.DocumentIntake, error) {
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

func (r *intakeIntakes) Update(_ context.Context, intake *domain.DocumentIntake) error {
	for index := range r.items {
		if r.items[index].ID == intake.ID {
			r.items[index] = *intake
			return nil
		}
	}
	return domain.ErrNotFound
}

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

func (r *intakeJobs) ListByStatus(context.Context, domain.JobStatus, int) ([]domain.Job, error) {
	return r.items, nil
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

// intakeLeases é o dublê de contratos do painel: implementa o port inteiro
// porque o dashboard e a temporada percorrem quase todas as leituras.
type intakeLeases struct{ items []domain.Lease }

func (r *intakeLeases) List(context.Context) ([]domain.Lease, error) { return r.items, nil }

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

func (r *intakeLeases) ByProperty(_ context.Context, propertyID string) ([]domain.Lease, error) {
	items := make([]domain.Lease, 0, len(r.items))
	for _, lease := range r.items {
		if lease.PropertyID == propertyID {
			items = append(items, lease)
		}
	}
	return items, nil
}

func (r *intakeLeases) Overlapping(context.Context, string, string, string, string) ([]domain.Lease, error) {
	return nil, nil
}

func (r *intakeLeases) ActiveInRange(context.Context, string, string) ([]domain.Lease, error) {
	return nil, nil
}

func (r *intakeLeases) Create(_ context.Context, lease *domain.Lease) error {
	r.items = append(r.items, *lease)
	return nil
}

func (r *intakeLeases) Update(_ context.Context, lease *domain.Lease) error {
	for index := range r.items {
		if r.items[index].ID == lease.ID {
			r.items[index] = *lease
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *intakeLeases) UpdateStatus(_ context.Context, id string, status domain.LeaseStatus, updatedAt string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items[index].Status = status
			r.items[index].UpdatedAt = updatedAt
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *intakeLeases) Delete(_ context.Context, id string) error {
	for index := range r.items {
		if r.items[index].ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *intakeLeases) Count(context.Context) (int, error) { return len(r.items), nil }

func (r *intakeLeases) CountByStatus(_ context.Context, status domain.LeaseStatus) (int, error) {
	total := 0
	for _, lease := range r.items {
		if lease.Status == status {
			total++
		}
	}
	return total, nil
}

type intakeAttachments struct{ saved map[string][]byte }

func (s *intakeAttachments) Save(prefix, filename string, data []byte) (string, error) {
	name := prefix + "/" + filename
	s.saved[name] = data
	return name, nil
}

func (s *intakeAttachments) Open(name string) ([]byte, error) {
	data, ok := s.saved[name]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return data, nil
}

func (s *intakeAttachments) Delete(name string) error {
	delete(s.saved, name)
	return nil
}

// intakeExtractor é o provedor de leitura de mentira.
type intakeExtractor struct {
	lease  domain.LeaseIntakeFields
	person domain.PersonIntakeFields
}

func (e *intakeExtractor) ExtractLeaseDocument(context.Context, application.DocumentFile) (domain.LeaseIntakeFields, string, error) {
	return e.lease, `{"tenantName":"Carlos Drummond"}`, nil
}

func (e *intakeExtractor) ExtractPersonDocument(context.Context, application.DocumentFile) (domain.PersonIntakeFields, string, error) {
	return e.person, `{"name":"Clarice Lispector"}`, nil
}

// ── Painel de teste ────────────────────────────────────────────────

type intakePanel struct {
	*testServer
	intakes     *intakeIntakes
	jobs        *intakeJobs
	tenants     *catalogTenants
	properties  *catalogProperties
	leases      *intakeLeases
	attachments *intakeAttachments
}

func newIntakePanel(t *testing.T, users ...*domain.User) *intakePanel {
	t.Helper()

	userRepo := newFakeUsers(users...)
	auditRepo := &fakeAudit{}
	clock := &fakeClock{now: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	tenants := newCatalogTenants()
	properties := newCatalogProperties()
	leases := &intakeLeases{}
	intakes := &intakeIntakes{}
	jobs := &intakeJobs{}
	attachments := &intakeAttachments{saved: map[string][]byte{}}
	extractor := &intakeExtractor{
		lease: domain.LeaseIntakeFields{
			TenantName: "Carlos Drummond", TenantCPF: "111.222.333-44", TenantEmail: "carlos@poesia.com.br",
			TenantPhone: "(22) 99888-7766", PropertyName: "Casa de Praia 202", PropertyCity: "Cabo Frio",
			PropertyState: "RJ", StartDate: "2019-03-01", EndDate: "2022-02-28", DueDay: 10,
			RentAmount: 2500, AdjustIndex: "IGP-M", Guarantee: "fiador", Confidence: 0.93,
		},
		person: domain.PersonIntakeFields{
			Name: "Clarice Lispector", CPF: "222.333.444-55", Email: "clarice@letras.com.br",
			Confidence: 0.88,
		},
	}

	cfg := &config.Config{
		AppEnv:             "test",
		Host:               "127.0.0.1",
		Port:               0,
		SessionSecret:      "test-session-secret-with-enough-length",
		RateLimitMax:       5,
		RateLimitGlobalMax: 60,
		LoginLockThreshold: 10,
		SeedUsername:       "passoz",
	}

	deps := application.Deps{
		Repos: application.Repositories{
			Users:       userRepo,
			Tenants:     tenants,
			Properties:  properties,
			Leases:      leases,
			Receipts:    &catalogReceipts{},
			Settings:    newFakeSettings(),
			Audit:       auditRepo,
			Inspections: &emptyInspections{},
			Jobs:        jobs,
			Intakes:     intakes,
		},
		Clock:       clock,
		IDs:         &fakeIDs{},
		Hasher:      fakeHasher{},
		Attachments: attachments,
		Intakes:     extractor,
		Opts:        application.Options{AIConfidenceThreshold: 0.9},
		Log:         slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	renderer, err := NewRenderer()
	require.NoError(t, err)

	common := &Common{
		Cfg:       cfg,
		Log:       deps.Log,
		Render:    renderer,
		Session:   NewSessionManager(cfg.SessionSecret, false),
		CSRF:      NewCSRF(cfg.SessionSecret, false),
		Deps:      deps,
		Started:   clock.now,
		Dialect:   "sqlite",
		LoginGate: application.NewLoginGate(clock, cfg.RateLimitMax, cfg.RateLimitGlobalMax, cfg.LoginLockThreshold),
		MustChangePasswordFunc: func(ctx context.Context, userID string) (bool, error) {
			return application.AdminMustChangePassword(ctx, deps, userID)
		},
	}

	return &intakePanel{
		testServer:  &testServer{handler: NewHandlers(common), audit: auditRepo, users: userRepo, clock: clock},
		intakes:     intakes,
		jobs:        jobs,
		tenants:     tenants,
		properties:  properties,
		leases:      leases,
		attachments: attachments,
	}
}

// authenticate faz o login do administrador e devolve a sessão e o token CSRF
// (o token nasce no cookie emitido pela página de login).
func (p *intakePanel) authenticate(t *testing.T) (session string, csrf string) {
	t.Helper()

	login := p.login(t, "passoz", "segredo")
	require.Equal(t, http.StatusFound, login.Code, "login do administrador deveria redirecionar")
	session = cookieValue(login.Result().Cookies(), adminCookieName)
	require.NotEmpty(t, session, "login sem cookie de sessão")

	page := httptest.NewRequest(http.MethodGet, "/login", nil)
	recorder := p.do(page)
	require.Equal(t, http.StatusOK, recorder.Code, "página de login deveria renderizar")

	csrf = cookieValue(recorder.Result().Cookies(), csrfCookieName)
	require.NotEmpty(t, csrf, "login sem cookie de CSRF")
	return session, csrf
}

// upload envia o documento do onboarding como o formulário do painel faria.
func (p *intakePanel) upload(t *testing.T, session, csrf, kind, filename, mime string, content []byte) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField(csrfFieldName, csrf))
	require.NoError(t, writer.WriteField("kind", kind))
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	header.Set("Content-Type", mime)
	part, err := writer.CreatePart(header)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	request := httptest.NewRequest(http.MethodPost, "/api/intake/document", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	request.AddCookie(&http.Cookie{Name: adminCookieName, Value: session})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrf})
	return p.do(request)
}

// confirm envia a conferência como o formulário do painel faria.
func (p *intakePanel) confirm(t *testing.T, session, csrf string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	form.Set(csrfFieldName, csrf)
	request := httptest.NewRequest(http.MethodPost, "/api/intake/confirm", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.AddCookie(&http.Cookie{Name: adminCookieName, Value: session})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrf})
	return p.do(request)
}

// confirmJSON envia a conferência em JSON, com o token do formulário na query
// (é onde o CSRF procura o token quando o corpo não é um formulário).
func (p *intakePanel) confirmJSON(t *testing.T, session, csrf string, payload any) *httptest.ResponseRecorder {
	t.Helper()

	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/intake/confirm?"+csrfFieldName+"="+url.QueryEscape(csrf), bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.AddCookie(&http.Cookie{Name: adminCookieName, Value: session})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrf})
	return p.do(request)
}

// intakeEntries devolve os eventos de cadastro por documento na trilha.
func (p *intakePanel) intakeEntries() []domain.AuditLog {
	entries := make([]domain.AuditLog, 0, len(p.audit.entries))
	for _, entry := range p.audit.entries {
		if entry.Action == domain.ActionDocumentIntakeCreated {
			entries = append(entries, entry)
		}
	}
	return entries
}

// intakeResponse lê a resposta JSON das rotas de onboarding.
func intakeResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload), "corpo inesperado: %s", recorder.Body.String())
	return payload
}

// ── Upload ─────────────────────────────────────────────────────────

// Nada é gravado antes da confirmação humana: o upload só cria a proposta e o
// trabalho de leitura.
func TestIntakeUploadCreatesProposalWithoutCadastro(t *testing.T) {
	panel := newIntakePanel(t, catalogUser())
	session, csrf := panel.authenticate(t)

	recorder := panel.upload(t, session, csrf, "lease", "contrato-2019.pdf", "", []byte("%PDF-1.4 contrato antigo"))
	require.Equal(t, http.StatusOK, recorder.Code, "corpo: %s", recorder.Body.String())

	payload := intakeResponse(t, recorder)
	require.Equal(t, true, payload["ok"])
	require.NotEmpty(t, payload["intakeId"])
	require.NotEmpty(t, payload["jobId"])

	require.Len(t, panel.intakes.items, 1)
	require.Equal(t, domain.IntakeStatusPending, panel.intakes.items[0].Status)
	require.Len(t, panel.jobs.items, 1)
	require.Equal(t, domain.JobKindDocumentIntake, panel.jobs.items[0].Kind)
	require.Contains(t, panel.attachments.saved, "intakes/contrato-2019.pdf")

	require.Empty(t, panel.tenants.items, "nenhum locatário antes da conferência")
	require.Empty(t, panel.properties.items, "nenhum imóvel antes da conferência")
	require.Empty(t, panel.leases.items, "nenhum contrato antes da conferência")
	require.Empty(t, panel.intakeEntries(), "nada na trilha antes da conferência")
}

func TestIntakeUploadRejectsUnsupportedFile(t *testing.T) {
	panel := newIntakePanel(t, catalogUser())
	session, csrf := panel.authenticate(t)

	recorder := panel.upload(t, session, csrf, "lease", "planilha.xlsx", "application/vnd.ms-excel", []byte("planilha"))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "Formato de arquivo não suportado. Envie PDF, JPG, PNG ou WEBP.", mutationError(t, recorder))
	require.Empty(t, panel.intakes.items)
	require.Empty(t, panel.jobs.items)
}

func TestIntakeUploadRejectsOversizedFile(t *testing.T) {
	panel := newIntakePanel(t, catalogUser())
	session, csrf := panel.authenticate(t)

	recorder := panel.upload(t, session, csrf, "lease", "contrato-gigante.pdf", "application/pdf", make([]byte, 20<<20+1))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "O arquivo pode ter no máximo 20 MB.", mutationError(t, recorder))
	require.Empty(t, panel.intakes.items)
	require.Empty(t, panel.jobs.items)
	require.Empty(t, panel.attachments.saved)
}

func TestIntakeUploadRejectsInvalidKind(t *testing.T) {
	panel := newIntakePanel(t, catalogUser())
	session, csrf := panel.authenticate(t)

	recorder := panel.upload(t, session, csrf, "escritura", "doc.pdf", "", []byte("%PDF-1.4"))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "Tipo de documento não aceito.", mutationError(t, recorder))
	require.Empty(t, panel.intakes.items)
}

func TestIntakeUploadRequiresSession(t *testing.T) {
	panel := newIntakePanel(t, catalogUser())
	_, csrf := panel.authenticate(t)

	recorder := panel.upload(t, "", csrf, "lease", "contrato.pdf", "", []byte("%PDF-1.4"))
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Empty(t, panel.intakes.items)
}

// ── Conferência ────────────────────────────────────────────────────

func TestIntakeConfirmCreatesCadastroAndAudits(t *testing.T) {
	panel := newIntakePanel(t, catalogUser())
	session, csrf := panel.authenticate(t)
	upload := panel.upload(t, session, csrf, "lease", "contrato-2019.pdf", "", []byte("%PDF-1.4 contrato antigo"))
	require.Equal(t, http.StatusOK, upload.Code)
	intakeID, _ := intakeResponse(t, upload)["intakeId"].(string)

	recorder := panel.confirm(t, session, csrf, url.Values{
		"intakeId":       {intakeID},
		"kind":           {"lease"},
		"createTenant":   {"1"},
		"createLease":    {"1"},
		"createProperty": {"1"},
		"tenant_name":    {"Carlos Drummond"},
		"tenant_cpf":     {"11122233344"},
		"tenant_email":   {"carlos@poesia.com.br"},
		"property_name":  {"Casa de Praia 202"},
		"property_city":  {"Cabo Frio"},
		"property_state": {"RJ"},
		"start_date":     {"01/03/2019"},
		"end_date":       {"28/02/2022"},
		"due_day":        {"10"},
		"rent_amount":    {"R$ 2.500,00"},
	})
	require.Equal(t, http.StatusOK, recorder.Code, "corpo: %s", recorder.Body.String())

	payload := intakeResponse(t, recorder)
	require.Equal(t, true, payload["ok"])
	require.NotEmpty(t, payload["tenantId"])
	require.NotEmpty(t, payload["propertyId"])
	require.NotEmpty(t, payload["leaseId"])

	require.Len(t, panel.tenants.items, 1)
	require.Len(t, panel.properties.items, 1)
	require.Len(t, panel.leases.items, 1)
	lease := panel.leases.items[0]
	require.Equal(t, "2019-03-01", lease.StartDate, "a data da tela vira o formato do banco")
	require.Equal(t, 2500.0, lease.RentAmount, "o valor mascarado vira número")

	require.Equal(t, domain.IntakeStatusConfirmed, panel.intakes.items[0].Status)
	require.Equal(t, "lease", domain.Str(panel.intakes.items[0].CreatedEntity))

	entries := panel.intakeEntries()
	require.Len(t, entries, 1)
	require.Equal(t, domain.EntityIntake, entries[0].Entity)
	require.Contains(t, entries[0].Summary, "contrato-2019.pdf")
	require.Contains(t, domain.Str(entries[0].Metadata), "contrato-2019.pdf")
}

func TestIntakeConfirmDiscardsWithReason(t *testing.T) {
	panel := newIntakePanel(t, catalogUser())
	session, csrf := panel.authenticate(t)
	upload := panel.upload(t, session, csrf, "id-document", "rg.pdf", "", []byte("%PDF-1.4 rg"))
	require.Equal(t, http.StatusOK, upload.Code)
	intakeID, _ := intakeResponse(t, upload)["intakeId"].(string)

	recorder := panel.confirm(t, session, csrf, url.Values{
		"intakeId": {intakeID},
		"action":   {"discard"},
		"reason":   {"documento ilegível"},
	})
	require.Equal(t, http.StatusOK, recorder.Code, "corpo: %s", recorder.Body.String())
	require.Equal(t, string(domain.IntakeStatusDiscarded), intakeResponse(t, recorder)["status"])

	require.Equal(t, domain.IntakeStatusDiscarded, panel.intakes.items[0].Status)
	require.Equal(t, "documento ilegível", domain.Str(panel.intakes.items[0].Notes))
	require.Empty(t, panel.tenants.items)
	require.Empty(t, panel.intakeEntries())
}

func TestIntakeConfirmRejectsLeaseWithoutTenantAndProperty(t *testing.T) {
	panel := newIntakePanel(t, catalogUser())
	session, csrf := panel.authenticate(t)
	upload := panel.upload(t, session, csrf, "lease", "contrato.pdf", "", []byte("%PDF-1.4"))
	require.Equal(t, http.StatusOK, upload.Code)
	intakeID, _ := intakeResponse(t, upload)["intakeId"].(string)

	recorder := panel.confirm(t, session, csrf, url.Values{
		"intakeId":    {intakeID},
		"kind":        {"lease"},
		"createLease": {"1"},
	})
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "Para criar o contrato, informe o locatário e o imóvel.", mutationError(t, recorder))
	require.Empty(t, panel.leases.items)
	require.Equal(t, domain.IntakeStatusPending, panel.intakes.items[0].Status)
}

func TestIntakeConfirmAcceptsJSONBody(t *testing.T) {
	panel := newIntakePanel(t, catalogUser())
	session, csrf := panel.authenticate(t)
	upload := panel.upload(t, session, csrf, "id-document", "rg.pdf", "", []byte("%PDF-1.4"))
	require.Equal(t, http.StatusOK, upload.Code)
	intakeID, _ := intakeResponse(t, upload)["intakeId"].(string)

	recorder := panel.confirmJSON(t, session, csrf, map[string]any{
		"intakeId":     intakeID,
		"kind":         "id-document",
		"createTenant": true,
		"name":         "Clarice Lispector",
		"cpf":          "222.333.444-55",
		"email":        "clarice@letras.com.br",
	})
	require.Equal(t, http.StatusOK, recorder.Code, "corpo: %s", recorder.Body.String())
	require.NotEmpty(t, intakeResponse(t, recorder)["tenantId"])
	require.Len(t, panel.tenants.items, 1)
	for _, tenant := range panel.tenants.items {
		require.Equal(t, "Clarice Lispector", tenant.Name)
	}
}

func TestIntakeConfirmRequiresSession(t *testing.T) {
	panel := newIntakePanel(t, catalogUser())
	session, csrf := panel.authenticate(t)
	upload := panel.upload(t, session, csrf, "lease", "contrato.pdf", "", []byte("%PDF-1.4"))
	require.Equal(t, http.StatusOK, upload.Code)
	intakeID, _ := intakeResponse(t, upload)["intakeId"].(string)

	recorder := panel.confirm(t, "", csrf, url.Values{"intakeId": {intakeID}, "createTenant": {"1"}})
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

// ── Tela de conferência ────────────────────────────────────────────

func TestIntakeTabRendersConferenceScreen(t *testing.T) {
	panel := newIntakePanel(t, catalogUser())
	session, csrf := panel.authenticate(t)
	upload := panel.upload(t, session, csrf, "lease", "contrato-2019.pdf", "", []byte("%PDF-1.4 contrato antigo"))
	require.Equal(t, http.StatusOK, upload.Code)
	intakeID, _ := intakeResponse(t, upload)["intakeId"].(string)
	require.NoError(t, application.ProcessDocumentIntakeJob(context.Background(), panel.handler.Deps, panel.jobs.items[0]))
	panel.tenants.items["tenant-1"] = domain.Tenant{ID: "tenant-1", Name: "Carlos Drummond"}
	panel.properties.items["property-1"] = domain.Property{ID: "property-1", Name: "Casa de Praia 202"}

	request := httptest.NewRequest(http.MethodGet, "/dashboard?tab=intake", nil)
	request.AddCookie(&http.Cookie{Name: adminCookieName, Value: session})
	recorder := panel.do(request)
	require.Equal(t, http.StatusOK, recorder.Code, "aba de conferência deveria renderizar")
	body := recorder.Body.String()

	for _, expected := range []string{
		"Cadastro por documento",
		"Enviar documento",
		`action="/api/intake/document"`,
		`enctype="multipart/form-data"`,
		`name="kind"`,
		"Contrato antigo",
		"Aguardando conferência",
		"93%",
		"contrato-2019.pdf",
		"Conferir",
		"Descartar",
		`action="/api/intake/confirm"`,
		`name="intakeId" value="` + intakeID + `"`,
		`name="csrf_token"`,
		"data-table",
		"data-pagination",
		`name="tenant_name"`,
		`value="Carlos Drummond"`,
		`name="start_date"`,
		`value="01/03/2019"`,
		"data-datepicker",
		`name="rent_amount"`,
		`value="R$ 2.500,00"`,
		`data-mask="money"`,
		`name="createTenant"`,
		`name="createLease"`,
		`value="tenant-1"`,
		`value="property-1"`,
	} {
		require.Contains(t, body, expected, "aba de conferência sem %q", expected)
	}
	require.NotContains(t, body, "Nenhum documento aguardando conferência")
}

func TestIntakeTabRendersEmptyState(t *testing.T) {
	panel := newIntakePanel(t, catalogUser())
	session, _ := panel.authenticate(t)

	request := httptest.NewRequest(http.MethodGet, "/dashboard?tab=intake", nil)
	request.AddCookie(&http.Cookie{Name: adminCookieName, Value: session})
	recorder := panel.do(request)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Nenhum documento aguardando conferência.")
	require.Contains(t, recorder.Body.String(), "0 proposta(s) aguardando conferência")
}
