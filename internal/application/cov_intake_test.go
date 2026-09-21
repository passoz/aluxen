package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Cobertura do onboarding por documento, da esteira do comprovante e dos dados
// de documento. Os dublês deste arquivo levam o prefixo `cint` e rodam sobre as
// caixas em memória do harness (`cov`): o que se prova é o comportamento
// observável — o que ficou gravado, o que a função devolveu e qual erro saiu.

// ── Dublês ─────────────────────────────────────────────────────────

// cintExtractor é o provedor de leitura dublado: devolve a extração (ou o erro)
// que o teste manda e guarda os documentos recebidos.
type cintExtractor struct {
	payment  domain.PaymentExtraction
	lease    domain.LeaseIntakeFields
	person   domain.PersonIntakeFields
	raw      string
	payErr   error
	leaseErr error
	persErr  error
	docs     []DocumentFile
}

func (e *cintExtractor) ExtractPaymentProof(_ context.Context, doc DocumentFile) (domain.PaymentExtraction, string, error) {
	e.docs = append(e.docs, doc)
	if e.payErr != nil {
		return domain.PaymentExtraction{}, e.raw, e.payErr
	}
	return e.payment, e.raw, nil
}

func (e *cintExtractor) ExtractLeaseDocument(_ context.Context, doc DocumentFile) (domain.LeaseIntakeFields, string, error) {
	e.docs = append(e.docs, doc)
	if e.leaseErr != nil {
		return domain.LeaseIntakeFields{}, e.raw, e.leaseErr
	}
	return e.lease, e.raw, nil
}

func (e *cintExtractor) ExtractPersonDocument(_ context.Context, doc DocumentFile) (domain.PersonIntakeFields, string, error) {
	e.docs = append(e.docs, doc)
	if e.persErr != nil {
		return domain.PersonIntakeFields{}, e.raw, e.persErr
	}
	return e.person, e.raw, nil
}

// cintSettingsKey falha ao ler uma chave específica, mantendo as demais — é o
// que falta no dublê do harness para exercitar cada erro de configuração.
type cintSettingsKey struct {
	*covSettings
	failKey string
	err     error
}

func (s *cintSettingsKey) Get(ctx context.Context, key string) (string, error) {
	if key == s.failKey {
		return "", s.err
	}
	return s.covSettings.Get(ctx, key)
}

// cintFileRepo falha ao abrir um arquivo do armazenamento.
type cintFileRepo struct {
	*covAttachments
	openErr error
}

func (s *cintFileRepo) Open(name string) ([]byte, error) {
	if s.openErr != nil {
		return nil, s.openErr
	}
	return s.covAttachments.Open(name)
}

// ── Apoio ─────────────────────────────────────────────────────────

// cintEnv monta o ambiente com o dublê de leitura ligado nas duas portas que o
// consomem (comprovante e onboarding) e com base pública determinística.
func cintEnv(t *testing.T) (*covEnv, *cintExtractor) {
	t.Helper()
	env := newCovEnv(t)
	ext := &cintExtractor{}
	env.deps.Proofs = ext
	env.deps.Intakes = ext
	env.deps.Opts.PublicBaseURL = "https://painel.example"
	return env, ext
}

// cintSeedIntake guarda o arquivo e cria a proposta com a situação informada.
func cintSeedIntake(t *testing.T, env *covEnv, kind domain.IntakeKind, status domain.IntakeStatus) *domain.DocumentIntake {
	t.Helper()
	stored, err := env.attachments.Save("cint", "documento.pdf", []byte("conteudo"))
	require.NoError(t, err)
	intake := &domain.DocumentIntake{
		ID: env.ids.New(), CreatedAt: "2026-09-20 09:00:00", UpdatedAt: "2026-09-20 09:00:00",
		Kind: kind, FileName: domain.StrPtr("documento.pdf"), FilePath: &stored,
		Mime: domain.StrPtr("application/pdf"), Status: status,
	}
	require.NoError(t, env.intakes.Create(context.Background(), intake))
	return intake
}

// cintRepoIntake devolve a cópia persistida da proposta.
func cintRepoIntake(t *testing.T, env *covEnv, id string) domain.DocumentIntake {
	t.Helper()
	stored, err := env.intakes.ByID(context.Background(), id)
	require.NoError(t, err)
	return *stored
}

// cintWantValidation confere a mensagem exata que o usuário lê.
func cintWantValidation(t *testing.T, err error, message string) {
	t.Helper()
	require.Error(t, err)
	require.True(t, domain.IsValidation(err), "erro não é de validação: %v", err)
	require.Equal(t, message, domain.ValidationMessage(err))
}

// cintSetting põe um valor nas configurações do ambiente.
func cintSetting(env *covEnv, key, value string) {
	env.settings.values[key] = value
}

// cintAuditMeta decodifica o metadado estruturado da linha da trilha.
func cintAuditMeta(t *testing.T, entry domain.AuditLog) map[string]any {
	t.Helper()
	require.NotNil(t, entry.Metadata)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(*entry.Metadata), &decoded))
	return decoded
}

// ── SubmitDocumentIntake ──────────────────────────────────────────

func TestCintSubmitDocumentIntakeValidations(t *testing.T) {
	cases := []struct {
		name  string
		adapt func(*covEnv, *IntakeInput)
		want  string
	}{
		{"tipo inválido", func(_ *covEnv, in *IntakeInput) { in.Kind = domain.IntakeKind("bogus") }, "Tipo de documento não aceito."},
		{"sem arquivo", func(_ *covEnv, in *IntakeInput) { in.Bytes = nil }, "Envie o arquivo do documento para a leitura."},
		{"arquivo grande", func(_ *covEnv, in *IntakeInput) { in.Bytes = make([]byte, maxIntakeBytes+1) }, "O arquivo pode ter no máximo 20 MB."},
		{"formato não suportado", func(_ *covEnv, in *IntakeInput) { in.FileName = "nota.txt"; in.Mime = "text/plain" }, "Formato de arquivo não suportado. Envie PDF, JPG, PNG ou WEBP."},
		{"sem armazenamento", func(env *covEnv, _ *IntakeInput) { env.deps.Attachments = nil }, "Armazenamento de arquivos indisponível nesta instância."},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			env, _ := cintEnv(t)
			input := IntakeInput{Kind: domain.IntakeKindLease, FileName: "contrato.pdf", Mime: "application/pdf", Bytes: []byte("pdf")}
			testCase.adapt(env, &input)

			_, _, err := submitDocumentIntake(context.Background(), env.deps, covCtx(), input)

			cintWantValidation(t, err, testCase.want)
			require.Empty(t, env.intakes.items, "nenhuma proposta pode nascer de entrada recusada")
			require.Empty(t, env.jobs.items, "nenhum trabalho pode ser enfileirado de entrada recusada")
		})
	}
}

func TestCintSubmitDocumentIntakeStorageFailures(t *testing.T) {
	t.Run("falha ao guardar o arquivo", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.attachments.saveErr = errors.New("disco cheio")

		_, err := SubmitDocumentIntake(context.Background(), env.deps, covCtx(), IntakeInput{
			Kind: domain.IntakeKindLease, FileName: "contrato.pdf", Mime: "application/pdf", Bytes: []byte("pdf"),
		})

		require.ErrorContains(t, err, "guardar documento do cadastro: disco cheio")
		require.Empty(t, env.intakes.items)
	})

	t.Run("falha ao criar a proposta", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.intakes.createErr = errors.New("banco fora")

		intake, _, err := submitDocumentIntake(context.Background(), env.deps, covCtx(), IntakeInput{
			Kind: domain.IntakeKindLease, FileName: "contrato.pdf", Mime: "application/pdf", Bytes: []byte("pdf"),
		})

		require.ErrorContains(t, err, "criar proposta de cadastro: banco fora")
		require.Nil(t, intake)
		require.Empty(t, env.jobs.items, "arquivo guardado não pode virar trabalho sem proposta")
	})

	t.Run("fila fora do ar deixa a proposta pendente", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.jobs.createErr = errors.New("fila fora")

		_, _, err := submitDocumentIntake(context.Background(), env.deps, covCtx(), IntakeInput{
			Kind: domain.IntakeKindLease, FileName: "contrato.pdf", Mime: "application/pdf", Bytes: []byte("pdf"),
		})

		require.ErrorContains(t, err, "enfileirar leitura do documento: fila fora")
		require.Len(t, env.intakes.items, 1)
		require.Equal(t, domain.IntakeStatusPending, env.intakes.items[0].Status)
	})
}

func TestCintSubmitDocumentIntakeKeepsDocumentAndQueuesRead(t *testing.T) {
	env, _ := cintEnv(t)
	input := IntakeInput{
		Kind:     domain.IntakeKindAddressProof,
		FileName: "  ",
		Mime:     "",
		Bytes:    []byte("foto-do-documento"),
	}

	intake, job, err := SubmitDocumentIntakeJob(context.Background(), env.deps, covCtx(), input)
	require.NoError(t, err)

	// Nome ausente vira "documento" e o tipo vazio passa pela extensão .pdf.
	require.Equal(t, []string{"intakes-documento"}, env.attachments.names)
	require.Equal(t, []byte("foto-do-documento"), env.attachments.files["intakes-documento"])
	require.Equal(t, domain.IntakeStatusPending, intake.Status)
	require.Equal(t, domain.IntakeKindAddressProof, intake.Kind)
	require.Equal(t, "documento", domain.Str(intake.FileName))
	require.Equal(t, "intakes-documento", domain.Str(intake.FilePath))
	require.Nil(t, intake.Mime, "mime vazio não é gravado")
	require.Nil(t, intake.Extracted, "o envio não faz leitura")

	require.Equal(t, domain.JobKindDocumentIntake, job.Kind)
	require.Equal(t, domain.JobStatusQueued, job.Status)
	require.NotNil(t, job.Payload)
	var payload intakeJobPayload
	require.NoError(t, json.Unmarshal([]byte(*job.Payload), &payload))
	require.Equal(t, intake.ID, payload.IntakeID)

	require.Empty(t, env.audit.entries, "o upload não entra na trilha; a criação do cadastro entra")

	// A variante sem o trabalho devolve o mesmo registro.
	same, err := SubmitDocumentIntake(context.Background(), env.deps, covCtx(), IntakeInput{
		Kind: domain.IntakeKindLease, FileName: "contrato.pdf", Mime: "application/pdf", Bytes: []byte("pdf"),
	})
	require.NoError(t, err)
	require.Equal(t, domain.IntakeKindLease, same.Kind)
	require.Len(t, env.intakes.items, 2)
}

// ── ProcessDocumentIntake ─────────────────────────────────────────

func TestCintProcessDocumentIntakeWithoutReading(t *testing.T) {
	t.Run("proposta inexistente", func(t *testing.T) {
		env, ext := cintEnv(t)

		_, err := ProcessDocumentIntake(context.Background(), env.deps, "nao-existe")

		cintWantValidation(t, err, "Proposta de cadastro não encontrada.")
		require.Empty(t, ext.docs)
	})

	t.Run("id vazio", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := ProcessDocumentIntake(context.Background(), env.deps, "   ")

		cintWantValidation(t, err, "Documento não informado.")
	})

	t.Run("proposta já conferida volta intacta", func(t *testing.T) {
		env, ext := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusConfirmed)

		intake, err := ProcessDocumentIntake(context.Background(), env.deps, seeded.ID)

		require.NoError(t, err)
		require.Equal(t, domain.IntakeStatusConfirmed, intake.Status)
		require.Empty(t, ext.docs, "releitura de proposta conferida não faz sentido")
	})

	t.Run("sem provedor de leitura anota a proposta", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.deps.Intakes = nil
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

		intake, err := ProcessDocumentIntake(context.Background(), env.deps, seeded.ID)

		require.NoError(t, err)
		require.Equal(t, intakeNoteNoProvider, domain.Str(intake.Notes))
		stored := cintRepoIntake(t, env, seeded.ID)
		require.Equal(t, intakeNoteNoProvider, domain.Str(stored.Notes))
		require.Equal(t, domain.IntakeStatusPending, stored.Status)
		require.Equal(t, format.FormatUTCStamp(covToday()), stored.UpdatedAt)
	})

	t.Run("arquivo ausente anota a proposta", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindIDDocument, domain.IntakeStatusPending)
		seeded.FilePath = domain.StrPtr("nao-existe")

		intake, err := ProcessDocumentIntake(context.Background(), env.deps, seeded.ID)

		require.NoError(t, err)
		require.Equal(t, intakeNoteNoFile, domain.Str(intake.Notes))
		require.Equal(t, intakeNoteNoFile, domain.Str(cintRepoIntake(t, env, seeded.ID).Notes))
	})

	t.Run("sem armazenamento anota a proposta", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindIDDocument, domain.IntakeStatusPending)
		env.deps.Attachments = nil

		intake, err := ProcessDocumentIntake(context.Background(), env.deps, seeded.ID)

		require.NoError(t, err)
		require.Equal(t, intakeNoteNoFile, domain.Str(intake.Notes))
	})

	t.Run("falha de leitura vira erro para o worker", func(t *testing.T) {
		env, ext := cintEnv(t)
		ext.leaseErr = errors.New("provedor fora do ar")
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

		_, err := ProcessDocumentIntake(context.Background(), env.deps, seeded.ID)

		require.ErrorContains(t, err, "ler documento do cadastro: provedor fora do ar")
		require.Equal(t, domain.IntakeStatusPending, cintRepoIntake(t, env, seeded.ID).Status)
	})

	t.Run("falha ao anotar a proposta", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.deps.Intakes = nil
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)
		env.intakes.updateErr = errors.New("banco fora")

		_, err := ProcessDocumentIntake(context.Background(), env.deps, seeded.ID)

		require.ErrorContains(t, err, "anotar proposta de cadastro: banco fora")
	})

	t.Run("falha ao gravar a leitura", func(t *testing.T) {
		env, ext := cintEnv(t)
		ext.lease = domain.LeaseIntakeFields{TenantName: "Ana", Confidence: 0.9}
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)
		env.intakes.updateErr = errors.New("banco fora")

		_, err := ProcessDocumentIntake(context.Background(), env.deps, seeded.ID)

		require.ErrorContains(t, err, "gravar leitura do documento: banco fora")
		require.Nil(t, cintRepoIntake(t, env, seeded.ID).Extracted)
	})
}

func TestCintProcessDocumentIntakeStoresReading(t *testing.T) {
	t.Run("contrato antigo", func(t *testing.T) {
		env, ext := cintEnv(t)
		ext.raw = "resposta-bruta"
		ext.lease = domain.LeaseIntakeFields{
			TenantName: "Ana Souza", TenantCPF: "123.456.789-09", PropertyName: "Casa Centro",
			PropertyCity: "Niterói", PropertyState: "RJ", StartDate: "2026-01-01", EndDate: "2026-12-31",
			DueDay: 10, RentAmount: 1500, Confidence: 0.91,
		}
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

		intake, err := ProcessDocumentIntake(context.Background(), env.deps, seeded.ID)
		require.NoError(t, err)

		require.Len(t, ext.docs, 1)
		require.Equal(t, []byte("conteudo"), ext.docs[0].Bytes)
		require.Equal(t, "application/pdf", ext.docs[0].Mime)
		require.Equal(t, "documento.pdf", ext.docs[0].FileName)

		require.NotNil(t, intake.Extracted)
		var decoded domain.LeaseIntakeFields
		require.NoError(t, json.Unmarshal([]byte(*intake.Extracted), &decoded))
		require.Equal(t, "Ana Souza", decoded.TenantName)
		require.Equal(t, 1500.0, decoded.RentAmount)
		require.Equal(t, 0.91, *intake.Confidence)
		require.Equal(t, "resposta-bruta", domain.Str(intake.RawResponse))

		stored := cintRepoIntake(t, env, seeded.ID)
		require.Equal(t, *intake.Extracted, domain.Str(stored.Extracted))
		require.Equal(t, 0.91, *stored.Confidence)
	})

	t.Run("documento pessoal", func(t *testing.T) {
		env, ext := cintEnv(t)
		ext.person = domain.PersonIntakeFields{Name: "Bruno", CPF: "111", Address: "Rua B, 2", City: "Niterói", State: "RJ", Zip: "24000-000", Confidence: 0.7}
		seeded := cintSeedIntake(t, env, domain.IntakeKindAddressProof, domain.IntakeStatusPending)

		intake, err := ProcessDocumentIntake(context.Background(), env.deps, seeded.ID)
		require.NoError(t, err)

		var decoded domain.PersonIntakeFields
		require.NoError(t, json.Unmarshal([]byte(*intake.Extracted), &decoded))
		require.Equal(t, "Bruno", decoded.Name)
		require.Equal(t, "24000-000", decoded.Zip)
		require.Equal(t, 0.7, *intake.Confidence)
	})
}

func TestCintProcessDocumentIntakeJob(t *testing.T) {
	t.Run("sem payload", func(t *testing.T) {
		env, _ := cintEnv(t)

		err := ProcessDocumentIntakeJob(context.Background(), env.deps, domain.Job{ID: "j1"})

		cintWantValidation(t, err, "Trabalho de leitura sem documento informado.")
	})

	t.Run("payload em branco", func(t *testing.T) {
		env, _ := cintEnv(t)

		err := ProcessDocumentIntakeJob(context.Background(), env.deps, domain.Job{ID: "j1", Payload: domain.StrPtr("  ")})

		cintWantValidation(t, err, "Trabalho de leitura sem documento informado.")
	})

	t.Run("payload ilegível", func(t *testing.T) {
		env, _ := cintEnv(t)

		err := ProcessDocumentIntakeJob(context.Background(), env.deps, domain.Job{ID: "j1", Payload: domain.StrPtr("{")})

		cintWantValidation(t, err, "Trabalho de leitura com payload ilegível.")
	})

	t.Run("proposta informada é lida", func(t *testing.T) {
		env, ext := cintEnv(t)
		ext.person = domain.PersonIntakeFields{Name: "Bruno", Confidence: 0.5}
		seeded := cintSeedIntake(t, env, domain.IntakeKindIDDocument, domain.IntakeStatusPending)

		err := ProcessDocumentIntakeJob(context.Background(), env.deps, domain.Job{
			ID: "j1", Payload: domain.StrPtr(`{"intakeId":"` + seeded.ID + `"}`),
		})

		require.NoError(t, err)
		require.Len(t, ext.docs, 1)
		require.Equal(t, 0.5, *cintRepoIntake(t, env, seeded.ID).Confidence)
	})

	t.Run("proposta inexistente devolve erro do caso de uso", func(t *testing.T) {
		env, _ := cintEnv(t)

		err := ProcessDocumentIntakeJob(context.Background(), env.deps, domain.Job{
			ID: "j1", Payload: domain.StrPtr(`{"intakeId":"nao-existe"}`),
		})

		cintWantValidation(t, err, "Proposta de cadastro não encontrada.")
	})
}

func TestCintPendingDocumentIntakes(t *testing.T) {
	newPending := func(t *testing.T, env *covEnv, kind domain.IntakeKind, confidence float64, extracted any) *domain.DocumentIntake {
		t.Helper()
		intake := cintSeedIntake(t, env, kind, domain.IntakeStatusPending)
		intake.Confidence = domain.FloatPtr(confidence)
		if extracted != nil {
			encoded, err := json.Marshal(extracted)
			require.NoError(t, err)
			text := string(encoded)
			intake.Extracted = &text
		}
		require.NoError(t, env.intakes.Update(context.Background(), intake))
		return intake
	}

	t.Run("lista pendentes com limite padrão", func(t *testing.T) {
		env, _ := cintEnv(t)
		newPending(t, env, domain.IntakeKindLease, 0.5, domain.LeaseIntakeFields{TenantName: "Ana", Confidence: 0.5})
		newPending(t, env, domain.IntakeKindIDDocument, 0.95, domain.PersonIntakeFields{Name: "Bruno"})
		cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusConfirmed)

		views, err := PendingDocumentIntakes(context.Background(), env.deps, 0)
		require.NoError(t, err)
		require.Len(t, views, 2)

		require.Equal(t, domain.IntakeStatusPending, views[0].Intake.Status)
		require.Equal(t, "due", views[0].StatusTone)
		require.Equal(t, "Aguardando conferência", views[0].StatusLabel)
		require.Equal(t, "50%", views[0].ConfidenceLabel)
		require.True(t, views[0].LowConfidence, "0,50 fica abaixo do limiar de 0,80")
		require.Equal(t, "Contrato antigo", views[0].KindLabel)
		require.Empty(t, views[0].Missing, "o contrato lido veio completo")
		require.False(t, views[1].LowConfidence)
		require.Contains(t, views[1].Missing, "CPF", "documento pessoal lido sem CPF pede atenção")
	})

	t.Run("limite informado é respeitado", func(t *testing.T) {
		env, _ := cintEnv(t)
		newPending(t, env, domain.IntakeKindLease, 0.5, nil)
		newPending(t, env, domain.IntakeKindLease, 0.5, nil)

		views, err := PendingDocumentIntakes(context.Background(), env.deps, 1)

		require.NoError(t, err)
		require.Len(t, views, 1)
	})

	t.Run("falha ao listar", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.intakes.listErr = errors.New("banco fora")

		_, err := PendingDocumentIntakes(context.Background(), env.deps, 0)

		require.ErrorContains(t, err, "listar propostas de cadastro: banco fora")
	})
}

func TestCintLoadDocumentIntake(t *testing.T) {
	t.Run("proposta inexistente", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := LoadDocumentIntake(context.Background(), env.deps, "nao-existe")

		cintWantValidation(t, err, "Proposta de cadastro não encontrada.")
	})

	t.Run("leitura ilegível deixa o formulário vazio", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)
		seeded.Extracted = domain.StrPtr("não é json")
		require.NoError(t, env.intakes.Update(context.Background(), seeded))

		view, err := LoadDocumentIntake(context.Background(), env.deps, seeded.ID)

		require.NoError(t, err)
		require.Len(t, view.Lease, 13)
		require.Equal(t, "-", view.ConfidenceLabel, "sem confiança gravada a tela mostra traço")
		require.False(t, view.LowConfidence)
		require.NotEmpty(t, view.Missing, "formulário vazio precisa de todos os campos")
	})

	t.Run("leitura completa alimenta o formulário", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)
		encoded, err := json.Marshal(domain.LeaseIntakeFields{
			TenantName: "Ana Souza", TenantCPF: "123.456.789-09", TenantEmail: "ana@example.com", TenantPhone: "21999990000",
			PropertyName: "Casa Centro", PropertyCity: "Niterói", PropertyState: "RJ",
			StartDate: "2026-01-01", EndDate: "2026-12-31", DueDay: 10, RentAmount: 1500, AdjustIndex: "IGP-M", Guarantee: "fiador",
			Confidence: 0.92,
		})
		require.NoError(t, err)
		text := string(encoded)
		seeded.Extracted = &text
		seeded.Confidence = domain.FloatPtr(0.92)
		require.NoError(t, env.intakes.Update(context.Background(), seeded))

		view, err := LoadDocumentIntake(context.Background(), env.deps, seeded.ID)
		require.NoError(t, err)

		require.Equal(t, "92%", view.ConfidenceLabel)
		require.False(t, view.LowConfidence)
		require.Empty(t, view.Missing)
		require.Equal(t, "documento.pdf", view.FileName)

		values := map[string]IntakeField{}
		for _, field := range view.Lease {
			values[field.Name] = field
		}
		require.Equal(t, "10", values[intakeFieldDueDay].Value)
		require.Equal(t, "01/01/2026", values[intakeFieldStartDate].Value)
		require.Equal(t, "31/12/2026", values[intakeFieldEndDate].Value)
		require.Contains(t, values[intakeFieldRentAmount].Value, "1.500")
		require.Equal(t, "cpf", values[intakeFieldTenantCPF].Mask)
		require.True(t, values[intakeFieldStartDate].Date)
	})
}

// ── ConfirmDocumentIntake ─────────────────────────────────────────

func TestCintConfirmDocumentIntakeGuards(t *testing.T) {
	t.Run("proposta inexistente", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{IntakeID: "nao-existe", CreateTenant: true})

		cintWantValidation(t, err, "Proposta de cadastro não encontrada.")
	})

	t.Run("proposta já conferida", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusConfirmed)

		_, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{IntakeID: seeded.ID, CreateTenant: true})

		cintWantValidation(t, err, "Esta proposta já foi conferida.")
	})

	t.Run("nada escolhido", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

		_, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{IntakeID: seeded.ID})

		cintWantValidation(t, err, "Escolha o que criar a partir do documento.")
		require.Empty(t, env.tenants.items)
		require.Empty(t, env.props.items)
	})

	t.Run("tipo inválido", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKind("bogus"), domain.IntakeStatusPending)

		_, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{
			IntakeID: seeded.ID, Kind: domain.IntakeKind("bogus"), CreateTenant: true,
		})

		cintWantValidation(t, err, "Tipo de documento não aceito.")
	})
}

func TestCintConfirmDocumentIntakeCreatesRegistration(t *testing.T) {
	env, _ := cintEnv(t)
	seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

	result, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{
		IntakeID:       seeded.ID,
		CreateTenant:   true,
		CreateProperty: true,
		Lease: domain.LeaseIntakeFields{
			TenantName: "Ana Souza", TenantEmail: "ana@example.com", TenantPhone: "21999990000", TenantCPF: "12345678909",
			PropertyName: "Casa Centro", PropertyCity: "Niterói", PropertyState: "RJ",
		},
	})
	require.NoError(t, err)

	// Locatário e imóvel criados a partir do documento.
	require.Len(t, env.tenants.items, 1)
	require.Equal(t, "Ana Souza", env.tenants.items[0].Name)
	require.Equal(t, "ana@example.com", env.tenants.items[0].Email)
	require.NotNil(t, env.tenants.items[0].Phone)
	require.Equal(t, result.TenantID, env.tenants.items[0].ID)

	require.Len(t, env.props.items, 1)
	require.Equal(t, "Casa Centro", env.props.items[0].Name)
	require.Equal(t, "Casa Centro", env.props.items[0].Address, "o contrato usa o imóvel como logradouro")
	require.Equal(t, "Niterói", env.props.items[0].City)
	require.Equal(t, result.PropertyID, env.props.items[0].ID)
	require.Empty(t, result.LeaseID, "contrato não foi pedido")

	// A proposta fica conferida com o cadastro de maior nível criado.
	stored := cintRepoIntake(t, env, seeded.ID)
	require.Equal(t, domain.IntakeStatusConfirmed, stored.Status)
	require.Equal(t, "property", domain.Str(stored.CreatedEntity))
	require.Equal(t, result.PropertyID, domain.Str(stored.CreatedEntityID))

	actions := covAuditActions(env.audit.entries)
	require.Contains(t, actions, domain.ActionTenantCreate)
	require.Contains(t, actions, domain.ActionPropertyCreate)
	require.Contains(t, actions, domain.ActionDocumentIntakeCreated)

	last := env.audit.entries[len(env.audit.entries)-1]
	require.Equal(t, domain.EntityIntake, last.Entity)
	require.Equal(t, seeded.ID, domain.Str(last.EntityID))
	require.Equal(t, "Contrato antigo · documento.pdf", domain.Str(last.EntityLabel))
	require.Equal(t, "Cadastro criado por documento (documento.pdf): locatário, imóvel", last.Summary)
	metadata := cintAuditMeta(t, last)
	require.Equal(t, string(domain.IntakeKindLease), metadata["kind"])
	require.Equal(t, result.TenantID, metadata["tenantId"])
	require.Equal(t, result.PropertyID, metadata["propertyId"])
	require.Equal(t, "", metadata["leaseId"])
}

func TestCintConfirmDocumentIntakeCreatesLease(t *testing.T) {
	env, _ := cintEnv(t)
	seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

	result, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtxOf(domain.AuditOriginJob), ConfirmInput{
		IntakeID:       seeded.ID,
		Kind:           domain.IntakeKindLease,
		CreateTenant:   true,
		CreateProperty: true,
		CreateLease:    true,
		Lease: domain.LeaseIntakeFields{
			TenantName: "Ana", TenantEmail: "ana@example.com",
			PropertyName: "Casa Centro", PropertyCity: "Niterói", PropertyState: "RJ",
			StartDate: "01/02/2026", EndDate: "31/01/2027", DueDay: 5, RentAmount: 1200,
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.LeaseID)

	require.Len(t, env.leases.items, 1)
	created := env.leases.items[0]
	require.Equal(t, "2026-02-01", created.StartDate, "a data da tela vira ISO no banco")
	require.Equal(t, "2027-01-31", created.EndDate)
	require.Equal(t, 5, created.DueDay)
	require.Equal(t, 1200.0, created.RentAmount)
	require.Equal(t, domain.LeaseStatusActive, created.Status)
	require.Equal(t, result.TenantID, created.TenantID)
	require.Equal(t, result.PropertyID, created.PropertyID)

	stored := cintRepoIntake(t, env, seeded.ID)
	require.Equal(t, "lease", domain.Str(stored.CreatedEntity))
	require.Equal(t, result.LeaseID, domain.Str(stored.CreatedEntityID))

	last := env.audit.entries[len(env.audit.entries)-1]
	require.Equal(t, "Cadastro criado por documento (documento.pdf): locatário, imóvel, contrato", last.Summary)
	require.Equal(t, result.LeaseID, cintAuditMeta(t, last)["leaseId"])
}

func TestCintConfirmDocumentIntakeReusesRegistration(t *testing.T) {
	env, _ := cintEnv(t)
	tenant := covTenantOf("t-1", "Ana Souza")
	property := covPropertyOf("p-1", "Casa Centro")
	require.NoError(t, env.tenants.Create(context.Background(), &tenant))
	require.NoError(t, env.props.Create(context.Background(), &property))
	seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

	result, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{
		IntakeID: seeded.ID, TenantID: "t-1", PropertyID: "p-1",
	})
	require.NoError(t, err)

	require.Equal(t, "t-1", result.TenantID)
	require.Equal(t, "p-1", result.PropertyID)
	require.Len(t, env.tenants.items, 1, "cadastro existente é reusado, não duplicado")
	require.Len(t, env.props.items, 1)

	stored := cintRepoIntake(t, env, seeded.ID)
	require.Equal(t, domain.IntakeStatusConfirmed, stored.Status)
	require.Equal(t, "property", domain.Str(stored.CreatedEntity))

	last := env.audit.entries[len(env.audit.entries)-1]
	require.Equal(t, "Cadastro criado por documento (documento.pdf): cadastro existente reusado: locatário, imóvel", last.Summary)
}

func TestCintConfirmDocumentIntakeInformatics(t *testing.T) {
	t.Run("locatário informado não existe", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

		_, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{IntakeID: seeded.ID, TenantID: "nao-existe"})

		cintWantValidation(t, err, "Locatário informado não encontrado.")
	})

	t.Run("imóvel informado não existe", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

		_, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{IntakeID: seeded.ID, PropertyID: "nao-existe"})

		cintWantValidation(t, err, "Imóvel informado não encontrado.")
	})

	t.Run("contrato sem locatário e imóvel", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

		_, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{IntakeID: seeded.ID, CreateLease: true})

		cintWantValidation(t, err, "Para criar o contrato, informe o locatário e o imóvel.")
	})

	t.Run("documento de identidade não descreve imóvel", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindIDDocument, domain.IntakeStatusPending)

		_, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{IntakeID: seeded.ID, CreateProperty: true})

		cintWantValidation(t, err, "Documento de identidade não descreve imóvel.")
		require.Empty(t, env.props.items)
	})

	t.Run("locatário sem nome nem email", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindIDDocument, domain.IntakeStatusPending)

		_, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{IntakeID: seeded.ID, CreateTenant: true})

		cintWantValidation(t, err, "Nome e email são obrigatórios.")
		require.Empty(t, env.tenants.items)
		require.Equal(t, domain.IntakeStatusPending, cintRepoIntake(t, env, seeded.ID).Status)
	})

	t.Run("falha ao criar o locatário", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.tenants.createErr = errors.New("banco fora")
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

		_, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{
			IntakeID: seeded.ID, CreateTenant: true,
			Lease: domain.LeaseIntakeFields{TenantName: "Ana", TenantEmail: "ana@example.com"},
		})

		require.ErrorContains(t, err, "cadastrar locatário: banco fora")
	})

	t.Run("falha ao criar o imóvel", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.props.createErr = errors.New("banco fora")
		seeded := cintSeedIntake(t, env, domain.IntakeKindAddressProof, domain.IntakeStatusPending)

		_, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{
			IntakeID: seeded.ID, CreateProperty: true,
			Person: domain.PersonIntakeFields{Address: "Rua B, 2", City: "Niterói", State: "RJ"},
		})

		require.ErrorContains(t, err, "cadastrar imóvel: banco fora")
	})

	t.Run("falha ao criar o contrato", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.leases.createErr = errors.New("banco fora")
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

		_, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{
			IntakeID: seeded.ID, CreateTenant: true, CreateProperty: true, CreateLease: true,
			Lease: domain.LeaseIntakeFields{
				TenantName: "Ana", TenantEmail: "ana@example.com",
				PropertyName: "Casa", PropertyCity: "Niterói", PropertyState: "RJ",
				StartDate: "01/02/2026", EndDate: "31/01/2027", DueDay: 5, RentAmount: 1200,
			},
		})

		require.ErrorContains(t, err, "cadastrar contrato: banco fora")
	})

	t.Run("falha ao gravar a proposta", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)
		env.intakes.updateErr = errors.New("banco fora")

		_, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{
			IntakeID: seeded.ID, CreateTenant: true,
			Lease: domain.LeaseIntakeFields{TenantName: "Ana", TenantEmail: "ana@example.com"},
		})

		require.ErrorContains(t, err, "confirmar proposta de cadastro: banco fora")
	})

	t.Run("falha na trilha", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)
		env.audit.insertErr = errors.New("trilha fora")

		_, err := ConfirmDocumentIntake(context.Background(), env.deps, covCtx(), ConfirmInput{
			IntakeID: seeded.ID, CreateTenant: true,
			Lease: domain.LeaseIntakeFields{TenantName: "Ana", TenantEmail: "ana@example.com"},
		})

		require.ErrorContains(t, err, "trilha fora")
	})
}

func TestCintDiscardDocumentIntake(t *testing.T) {
	t.Run("proposta inexistente", func(t *testing.T) {
		env, _ := cintEnv(t)

		err := DiscardDocumentIntake(context.Background(), env.deps, covCtx(), "nao-existe", "duplicado")

		cintWantValidation(t, err, "Proposta de cadastro não encontrada.")
	})

	t.Run("proposta já conferida", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusDiscarded)

		err := DiscardDocumentIntake(context.Background(), env.deps, covCtx(), seeded.ID, "duplicado")

		cintWantValidation(t, err, "Esta proposta já foi conferida.")
	})

	t.Run("motivo obrigatório", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

		err := DiscardDocumentIntake(context.Background(), env.deps, covCtx(), seeded.ID, "   ")

		cintWantValidation(t, err, "Informe o motivo do descarte.")
		require.Equal(t, domain.IntakeStatusPending, cintRepoIntake(t, env, seeded.ID).Status)
	})

	t.Run("falha ao gravar", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)
		env.intakes.updateErr = errors.New("banco fora")

		err := DiscardDocumentIntake(context.Background(), env.deps, covCtx(), seeded.ID, "duplicado")

		require.ErrorContains(t, err, "descartar proposta de cadastro: banco fora")
	})

	t.Run("descarte grava situação e motivo", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

		err := DiscardDocumentIntake(context.Background(), env.deps, covCtx(), seeded.ID, "  documento ilegível  ")
		require.NoError(t, err)

		stored := cintRepoIntake(t, env, seeded.ID)
		require.Equal(t, domain.IntakeStatusDiscarded, stored.Status)
		require.Equal(t, "documento ilegível", domain.Str(stored.Notes))
		require.Equal(t, format.FormatUTCStamp(covToday()), stored.UpdatedAt)
		require.Empty(t, env.tenants.items, "descarte não cria cadastro")
		require.Empty(t, env.audit.entries, "a trilha registra o cadastro criado, não o upload descartado")
	})
}

// ── Auxiliares do onboarding ──────────────────────────────────────

func TestCintIntakeStatusAndLabels(t *testing.T) {
	require.Equal(t, "paid", intakeStatusTone(domain.IntakeStatusConfirmed))
	require.Equal(t, "terminated", intakeStatusTone(domain.IntakeStatusDiscarded))
	require.Equal(t, "due", intakeStatusTone(domain.IntakeStatusPending))
	require.Equal(t, "due", intakeStatusTone(domain.IntakeStatus("outro")))

	require.Equal(t, "-", intakeConfidenceLabel(nil))
	require.Equal(t, "0%", intakeConfidenceLabel(domain.FloatPtr(0)))
	require.Equal(t, "93%", intakeConfidenceLabel(domain.FloatPtr(0.925)))

	require.Equal(t, "", intakeDateDisplay(""))
	require.Equal(t, "21/09/2026", intakeDateDisplay(" 2026-09-21 "))
	require.Equal(t, "sexta", intakeDateDisplay("sexta"), "data ilegível é mostrada como veio")

	require.Equal(t, "", intakeDueDayDisplay(0))
	require.Equal(t, "", intakeDueDayDisplay(-3))
	require.Equal(t, "10", intakeDueDayDisplay(10))

	require.Equal(t, "nada criado", intakeCreatedLabel(nil, nil))
	require.Equal(t, "cadastro existente reusado: locatário", intakeCreatedLabel(nil, []string{"locatário"}))
	require.Equal(t, "locatário, imóvel", intakeCreatedLabel([]string{"locatário", "imóvel"}, nil))
	require.Equal(t, "imóvel (reusado: locatário)", intakeCreatedLabel([]string{"imóvel"}, []string{"locatário"}))

	require.Equal(t, "Contrato antigo · a.pdf", intakeLabel(&domain.DocumentIntake{Kind: domain.IntakeKindLease, FileName: domain.StrPtr("a.pdf")}))
	require.Equal(t, "Contrato antigo", intakeLabel(&domain.DocumentIntake{Kind: domain.IntakeKindLease}))
	require.Equal(t, "Comprovante de residência", intakeLabel(&domain.DocumentIntake{Kind: domain.IntakeKindAddressProof, FileName: domain.StrPtr("  ")}))

	labels := intakeMissingLabels([]IntakeField{
		{Label: "Nome", Missing: true},
		{Label: "E-mail"},
		{Label: "CPF", Missing: true},
	})
	require.Equal(t, []string{"Nome", "CPF"}, labels)
	require.Empty(t, intakeMissingLabels(nil))
}

func TestCintIntakeDateISO(t *testing.T) {
	cases := map[string]string{
		"":            "",
		"   ":         "",
		"2026-09-21":  "2026-09-21",
		"21/09/2026":  "2026-09-21",
		" 1/2/2026 ":  "2026-02-01",
		"1/2":         "1/2",
		"ab/09/2026":  "ab/09/2026",
		"21/09/26":    "21/09/26",
		"21/09/2026x": "21/09/2026x",
	}
	for value, want := range cases {
		require.Equal(t, want, intakeDateISO(value), "intakeDateISO(%q)", value)
	}
}

func TestCintIntakeFileSupported(t *testing.T) {
	cases := []struct {
		mime, fileName string
		want           bool
	}{
		{"application/pdf", "contrato", true},
		{"IMAGE/JPEG; charset=utf-8", "foto.bin", true},
		{"", "FOTO.JPG", true},
		{"", "comprovante.heic", true},
		{"application/octet-stream", "doc.PDF", true},
		{"text/plain", "nota.pdf", false},
		{"", "nota.txt", false},
		{"application/octet-stream", "sem-extensao", false},
	}
	for _, testCase := range cases {
		require.Equal(t, testCase.want, intakeFileSupported(testCase.mime, testCase.fileName),
			"intakeFileSupported(%q, %q)", testCase.mime, testCase.fileName)
	}
}

func TestCintIntakeCreatedEntity(t *testing.T) {
	cases := []struct {
		result     IntakeResult
		entity, id string
	}{
		{IntakeResult{TenantID: "t", PropertyID: "p", LeaseID: "l"}, "lease", "l"},
		{IntakeResult{TenantID: "t", PropertyID: "p"}, "property", "p"},
		{IntakeResult{TenantID: "t"}, "tenant", "t"},
		{IntakeResult{}, "", ""},
	}
	for _, testCase := range cases {
		entity, id := intakeCreatedEntity(&testCase.result)
		require.Equal(t, testCase.entity, entity)
		require.Equal(t, testCase.id, id)
	}
}

func TestCintReadIntakeFile(t *testing.T) {
	t.Run("sem caminho", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := readIntakeFile(env.deps, &domain.DocumentIntake{})

		require.EqualError(t, err, "arquivo do documento ausente")
	})

	t.Run("sem armazenamento", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.deps.Attachments = nil

		_, err := readIntakeFile(env.deps, &domain.DocumentIntake{FilePath: domain.StrPtr("x")})

		require.EqualError(t, err, "arquivo do documento ausente")
	})

	t.Run("falha ao abrir", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.deps.Attachments = &cintFileRepo{covAttachments: env.attachments, openErr: errors.New("storage fora")}

		_, err := readIntakeFile(env.deps, &domain.DocumentIntake{FilePath: domain.StrPtr("x")})

		require.ErrorContains(t, err, "storage fora")
	})

	t.Run("arquivo guardado", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

		content, err := readIntakeFile(env.deps, seeded)

		require.NoError(t, err)
		require.Equal(t, []byte("conteudo"), content)
	})
}

func TestCintIntakeByID(t *testing.T) {
	t.Run("id vazio", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := intakeByID(context.Background(), env.deps, "  ")

		cintWantValidation(t, err, "Documento não informado.")
	})

	t.Run("proposta inexistente", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := intakeByID(context.Background(), env.deps, "nao-existe")

		cintWantValidation(t, err, "Proposta de cadastro não encontrada.")
	})

	t.Run("falha do repositório", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.intakes.byIDErr = errors.New("banco fora")

		_, err := intakeByID(context.Background(), env.deps, "cov-001")

		require.ErrorContains(t, err, "buscar proposta de cadastro: banco fora")
	})

	t.Run("proposta encontrada", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedIntake(t, env, domain.IntakeKindLease, domain.IntakeStatusPending)

		intake, err := intakeByID(context.Background(), env.deps, " "+seeded.ID+" ")

		require.NoError(t, err)
		require.Equal(t, seeded.ID, intake.ID)
	})
}

func TestCintExtractIntakeDocument(t *testing.T) {
	_, ext := cintEnv(t)
	ext.lease = domain.LeaseIntakeFields{TenantName: "Ana", Confidence: 0.8}
	ext.person = domain.PersonIntakeFields{Name: "Bruno", Confidence: 0.4}
	ext.raw = "bruto"

	t.Run("contrato antigo usa a leitura de contrato", func(t *testing.T) {
		ext.leaseErr = nil
		ext.raw = "bruto"

		fields, raw, confidence, err := extractIntakeDocument(context.Background(), ext, domain.IntakeKindLease, DocumentFile{Bytes: []byte("x")})

		require.NoError(t, err)
		require.Equal(t, "Ana", fields.(domain.LeaseIntakeFields).TenantName)
		require.Equal(t, "bruto", raw)
		require.Equal(t, 0.8, confidence)
	})

	t.Run("documento pessoal usa a leitura de pessoa", func(t *testing.T) {
		ext.raw = "bruto"

		fields, _, confidence, err := extractIntakeDocument(context.Background(), ext, domain.IntakeKindIDDocument, DocumentFile{})

		require.NoError(t, err)
		require.Equal(t, "Bruno", fields.(domain.PersonIntakeFields).Name)
		require.Equal(t, 0.4, confidence)
	})

	t.Run("erro do provedor sobe", func(t *testing.T) {
		ext.persErr = errors.New("provedor fora")
		defer func() { ext.persErr = nil }()

		_, _, _, err := extractIntakeDocument(context.Background(), ext, domain.IntakeKindAddressProof, DocumentFile{})

		require.ErrorContains(t, err, "provedor fora")
	})
}

func TestCintIntakeTenantFields(t *testing.T) {
	fromLease := ConfirmInput{
		Kind:   domain.IntakeKindLease,
		Lease:  domain.LeaseIntakeFields{TenantName: "Ana", TenantEmail: "ana@example.com", TenantPhone: "219", TenantCPF: "111"},
		Person: domain.PersonIntakeFields{Name: "Bruno", Email: "bruno@example.com", Phone: "119", CPF: "222"},
	}
	fromPerson := ConfirmInput{Kind: domain.IntakeKindIDDocument, Person: fromLease.Person}

	require.Equal(t, "Ana", intakeTenantName(domain.IntakeKindLease, fromLease))
	require.Equal(t, "ana@example.com", intakeTenantEmail(domain.IntakeKindLease, fromLease))
	require.Equal(t, "219", intakeTenantPhone(domain.IntakeKindLease, fromLease))
	require.Equal(t, "111", intakeTenantCPF(domain.IntakeKindLease, fromLease))

	require.Equal(t, "Bruno", intakeTenantName(domain.IntakeKindIDDocument, fromPerson))
	require.Equal(t, "bruno@example.com", intakeTenantEmail(domain.IntakeKindIDDocument, fromPerson))
	require.Equal(t, "119", intakeTenantPhone(domain.IntakeKindIDDocument, fromPerson))
	require.Equal(t, "222", intakeTenantCPF(domain.IntakeKindIDDocument, fromPerson))
}

func TestCintIntakePropertyFields(t *testing.T) {
	t.Run("contrato descreve o imóvel", func(t *testing.T) {
		fields, err := intakePropertyFields(domain.IntakeKindLease, ConfirmInput{
			Lease: domain.LeaseIntakeFields{PropertyName: "Casa Centro", PropertyCity: "Niterói", PropertyState: "RJ"},
		})

		require.NoError(t, err)
		require.Equal(t, PropertyInput{Name: "Casa Centro", Address: "Casa Centro", City: "Niterói", State: "RJ"}, fields)
	})

	t.Run("comprovante de residência vira endereço e nome", func(t *testing.T) {
		fields, err := intakePropertyFields(domain.IntakeKindAddressProof, ConfirmInput{
			Person: domain.PersonIntakeFields{Address: "Rua B, 2", City: "Niterói", State: "RJ", Zip: "24000-000"},
		})

		require.NoError(t, err)
		require.Equal(t, "Rua B, 2", fields.Name)
		require.Equal(t, "Rua B, 2", fields.Address)
		require.Equal(t, "24000-000", fields.Zip)
	})

	t.Run("documento de identidade não descreve imóvel", func(t *testing.T) {
		_, err := intakePropertyFields(domain.IntakeKindIDDocument, ConfirmInput{Person: domain.PersonIntakeFields{Address: "Rua B, 2"}})

		cintWantValidation(t, err, "Documento de identidade não descreve imóvel.")
	})
}

// ── Esteira do comprovante ────────────────────────────────────────

// cintSeedProof guarda o arquivo e cria o comprovante na situação informada.
func cintSeedProof(t *testing.T, env *covEnv, status domain.PaymentProofStatus) *domain.PaymentProof {
	t.Helper()
	stored, err := env.attachments.Save(paymentProofStorePrefix, "comprovante.pdf", []byte("comprovante"))
	require.NoError(t, err)
	proof := &domain.PaymentProof{
		ID: env.ids.New(), CreatedAt: "2026-09-20 09:00:00", UpdatedAt: "2026-09-20 09:00:00",
		Source: domain.PaymentProofSourcePanel, TenantID: domain.StrPtr("t-1"),
		FileName: domain.StrPtr("comprovante.pdf"), FilePath: &stored, Mime: domain.StrPtr("application/pdf"),
		Status: status, Decision: domain.PaymentDecisionPending,
	}
	require.NoError(t, env.proofs.Create(context.Background(), proof))
	return proof
}

// cintProofWorld prepara o locatário, o lançamento em aberto e o locador que a
// baixa automática precisa encontrar.
func cintProofWorld(t *testing.T, env *covEnv) domain.Receipt {
	t.Helper()
	tenant := covTenantOf("t-1", "Ana")
	require.NoError(t, env.tenants.Create(context.Background(), &tenant))
	receipt := covReceiptOf("r-1", "t-1", "", "2026-09-20", 900)
	receipt.TenantName = "Ana"
	receipt.PropertyName = "Casa"
	require.NoError(t, env.receipts.Create(context.Background(), &receipt))
	cintSetting(env, domain.SettingLandlordName, "Evolucsia Locadora")
	return receipt
}

// cintRepoProof devolve a cópia persistida do comprovante.
func cintRepoProof(t *testing.T, env *covEnv, id string) domain.PaymentProof {
	t.Helper()
	stored, err := env.proofs.ByID(context.Background(), id)
	require.NoError(t, err)
	return *stored
}

func TestCintSupportedPaymentProofFile(t *testing.T) {
	cases := []struct {
		mime, fileName string
		want           bool
	}{
		{"image/jpeg", "foto.jpg", true},
		{"", "FOTO.PNG", true},
		{"application/pdf", "", true},
		{"image/webp", "sem-extensao", true},
		{"text/plain", "nota.pdf", false},
		{"", "nota.txt", false},
		{"", "", false},
	}
	for _, testCase := range cases {
		require.Equal(t, testCase.want, supportedPaymentProofFile(testCase.mime, testCase.fileName),
			"supportedPaymentProofFile(%q, %q)", testCase.mime, testCase.fileName)
	}
}

func TestCintPaymentProofPaidAtAndLabel(t *testing.T) {
	require.Equal(t, "", paymentProofPaidAt("", covToday()))
	require.Equal(t, "", paymentProofPaidAt("21/09/2026", covToday()))

	expected := format.FormatLocalDateTime(time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
	require.Equal(t, expected, paymentProofPaidAt("2026-09-18", covToday()))
	require.Equal(t, expected, paymentProofPaidAt("2026-09-18T10:00:00Z", covToday()), "a hora do provedor é descartada")

	require.Equal(t, "comprovante.pdf", paymentProofLabel(&domain.PaymentProof{ID: "p-1", FileName: domain.StrPtr("comprovante.pdf")}))
	require.Equal(t, "Comprovante p-1", paymentProofLabel(&domain.PaymentProof{ID: "p-1"}))

	// Log ausente não pode derrubar o processamento.
	logPaymentProof(Deps{}, &domain.PaymentProof{ID: "p-1"}, "motivo")
}

func TestCintSubmitPaymentProofGuards(t *testing.T) {
	cases := []struct {
		name  string
		adapt func(*covEnv, *PaymentProofInput)
		want  string
	}{
		{"sem armazenamento", func(env *covEnv, _ *PaymentProofInput) { env.deps.Attachments = nil }, "armazenamento de anexos indisponível"},
		{"sem arquivo", func(_ *covEnv, in *PaymentProofInput) { in.Bytes = nil }, "Envie o arquivo do comprovante."},
		{"formato não suportado", func(_ *covEnv, in *PaymentProofInput) { in.FileName = "nota.txt"; in.Mime = "text/plain" }, "Formato de comprovante não suportado. Envie uma imagem ou um PDF."},
		{"inquilino não encontrado", func(_ *covEnv, in *PaymentProofInput) { in.TenantID = "nao-existe" }, "Inquilino não encontrado."},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			env, _ := cintEnv(t)
			input := PaymentProofInput{TenantID: "t-1", FileName: "comprovante.pdf", Mime: "application/pdf", Bytes: []byte("pdf")}
			testCase.adapt(env, &input)

			_, err := SubmitPaymentProof(context.Background(), env.deps, covCtx(), input)

			require.Error(t, err)
			require.Contains(t, err.Error(), testCase.want)
			require.Empty(t, env.proofs.items)
			require.Empty(t, env.jobs.items)
		})
	}
}

func TestCintSubmitPaymentProofFailures(t *testing.T) {
	input := PaymentProofInput{TenantID: "t-1", FileName: "comprovante.pdf", Mime: "application/pdf", Bytes: []byte("pdf")}

	t.Run("falha ao buscar o inquilino", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.tenants.byIDErr = errors.New("banco fora")

		_, err := SubmitPaymentProof(context.Background(), env.deps, covCtx(), input)

		require.ErrorContains(t, err, "buscar inquilino do comprovante: banco fora")
	})

	t.Run("falha ao guardar o arquivo", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.attachments.saveErr = errors.New("disco cheio")

		_, err := SubmitPaymentProof(context.Background(), env.deps, covCtx(), input)

		require.ErrorContains(t, err, "guardar comprovante: disco cheio")
	})

	t.Run("falha ao registrar", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.proofs.createErr = errors.New("banco fora")

		_, err := SubmitPaymentProof(context.Background(), env.deps, covCtx(), input)

		require.ErrorContains(t, err, "registrar comprovante: banco fora")
		require.Empty(t, env.jobs.items)
	})

	t.Run("fila fora do ar manda para a revisão", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.jobs.createErr = errors.New("fila fora")

		outcome, err := SubmitPaymentProof(context.Background(), env.deps, covCtx(), input)
		require.NoError(t, err)

		require.Equal(t, domain.PaymentProofStatusReview, outcome.Status)
		require.Equal(t, domain.PaymentDecisionPending, outcome.Decision)
		require.Equal(t, reasonProofNotQueued, outcome.Reason)
		require.Empty(t, outcome.JobID)

		stored := cintRepoProof(t, env, outcome.ProofID)
		require.Equal(t, domain.PaymentProofStatusReview, stored.Status)
		require.Equal(t, reasonProofNotQueued, domain.Str(stored.DecisionReason))
		require.Contains(t, covAuditActions(env.audit.entries), domain.ActionPaymentProofReview)
	})

	t.Run("fila fora e revisão indisponível", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.jobs.createErr = errors.New("fila fora")
		env.proofs.updateErr = errors.New("banco fora")

		_, err := SubmitPaymentProof(context.Background(), env.deps, covCtx(), input)

		require.ErrorContains(t, err, "gravar comprovante em revisão: banco fora")
	})
}

func TestCintSubmitPaymentProofQueuesReading(t *testing.T) {
	env, _ := cintEnv(t)
	tenant := covTenantOf("t-1", "Ana")
	require.NoError(t, env.tenants.Create(context.Background(), &tenant))

	outcome, err := SubmitPaymentProof(context.Background(), env.deps, covCtx(), PaymentProofInput{
		Phone: " 21999990000 ", TenantID: " t-1 ", FileName: " comprovante.jpg ", Mime: " image/jpeg ", Bytes: []byte("foto"),
	})
	require.NoError(t, err)

	require.Equal(t, domain.PaymentProofStatusPending, outcome.Status)
	require.Equal(t, domain.PaymentDecisionPending, outcome.Decision)
	require.NotEmpty(t, outcome.JobID)

	stored := cintRepoProof(t, env, outcome.ProofID)
	require.Equal(t, domain.PaymentProofSourcePanel, stored.Source, "origem vazia cai no painel")
	require.Equal(t, "21999990000", domain.Str(stored.Phone))
	require.Equal(t, "t-1", domain.Str(stored.TenantID))
	require.Equal(t, "comprovante.jpg", domain.Str(stored.FileName))
	require.Equal(t, "image/jpeg", domain.Str(stored.Mime))
	require.Equal(t, []byte("foto"), env.attachments.files[paymentProofStorePrefix+"- comprovante.jpg "])

	require.Len(t, env.jobs.items, 1)
	require.Equal(t, domain.JobKindPaymentProof, env.jobs.items[0].Kind)
	var payload PaymentProofJobPayload
	require.NoError(t, json.Unmarshal([]byte(domain.Str(env.jobs.items[0].Payload)), &payload))
	require.Equal(t, outcome.ProofID, payload.ProofID)
}

func TestCintProcessPaymentProofJob(t *testing.T) {
	t.Run("payload ilegível", func(t *testing.T) {
		env, _ := cintEnv(t)

		err := ProcessPaymentProofJob(context.Background(), env.deps, domain.Job{ID: "j-1", Payload: domain.StrPtr("{")})

		require.ErrorContains(t, err, "ler o conteúdo do trabalho j-1")
	})

	t.Run("sem comprovante", func(t *testing.T) {
		env, _ := cintEnv(t)

		err := ProcessPaymentProofJob(context.Background(), env.deps, domain.Job{ID: "j-1"})

		require.ErrorContains(t, err, "trabalho j-1 sem comprovante")
	})

	t.Run("payload em branco", func(t *testing.T) {
		env, _ := cintEnv(t)

		err := ProcessPaymentProofJob(context.Background(), env.deps, domain.Job{ID: "j-1", Payload: domain.StrPtr("  ")})

		require.ErrorContains(t, err, "trabalho j-1 sem comprovante")
	})

	t.Run("comprovante inexistente devolve erro", func(t *testing.T) {
		env, _ := cintEnv(t)

		err := ProcessPaymentProofJob(context.Background(), env.deps, domain.Job{
			ID: "j-1", Payload: domain.StrPtr(`{"proofId":"nao-existe"}`),
		})

		require.ErrorContains(t, err, "buscar comprovante")
	})

	t.Run("decisão já tomada termina sem erro", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusAccepted)

		err := ProcessPaymentProofJob(context.Background(), env.deps, domain.Job{
			ID: "j-1", Payload: domain.StrPtr(`{"proofId":"` + seeded.ID + `"}`),
		})

		require.NoError(t, err)
	})
}

func TestCintProcessPaymentProofGuards(t *testing.T) {
	t.Run("sem id", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := ProcessPaymentProof(context.Background(), env.deps, "  ")

		cintWantValidation(t, err, "Comprovante não informado.")
	})

	t.Run("falha ao buscar", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.proofs.byIDErr = errors.New("banco fora")

		_, err := ProcessPaymentProof(context.Background(), env.deps, "p-1")

		require.ErrorContains(t, err, "buscar comprovante: banco fora")
	})

	t.Run("comprovante inexistente", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := ProcessPaymentProof(context.Background(), env.deps, "nao-existe")

		require.ErrorContains(t, err, "buscar comprovante")
	})

	t.Run("decisão já tomada não é refeita", func(t *testing.T) {
		env, ext := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusAccepted)

		outcome, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)

		require.NoError(t, err)
		require.Equal(t, domain.PaymentProofStatusAccepted, outcome.Status)
		require.Empty(t, ext.docs, "leitura já decidida não roda de novo")
	})
}

func TestCintProcessPaymentProofUnreadable(t *testing.T) {
	t.Run("sem provedor de leitura", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.deps.Proofs = nil
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)

		outcome, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)
		require.NoError(t, err, "configuração ausente não melhora na retentativa")

		require.Equal(t, domain.PaymentProofStatusReview, outcome.Status)
		require.Equal(t, reasonProofNoExtractor, outcome.Reason)
		require.Equal(t, reasonProofNoExtractor, domain.Str(cintRepoProof(t, env, seeded.ID).DecisionReason))
	})

	t.Run("sem armazenamento", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)
		env.deps.Attachments = nil

		outcome, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)

		require.ErrorContains(t, err, reasonProofNoFile)
		require.Equal(t, reasonProofNoFile, outcome.Reason)
		require.Equal(t, domain.PaymentProofStatusReview, cintRepoProof(t, env, seeded.ID).Status)
	})

	t.Run("arquivo indisponível", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)
		env.deps.Attachments = &cintFileRepo{covAttachments: env.attachments, openErr: errors.New("storage fora")}

		outcome, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)

		require.ErrorContains(t, err, "storage fora")
		require.Equal(t, reasonProofNoFile, outcome.Reason)
	})

	t.Run("provedor fora do ar", func(t *testing.T) {
		env, ext := cintEnv(t)
		ext.payErr = errors.New("provedor fora")
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)

		outcome, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)

		require.ErrorContains(t, err, "ler comprovante: provedor fora")
		require.Equal(t, reasonProofUnreadable, outcome.Reason)
		require.Equal(t, reasonProofUnreadable, domain.Str(cintRepoProof(t, env, seeded.ID).DecisionReason))
	})

	t.Run("falha ao gravar a revisão", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.deps.Proofs = nil
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)
		env.proofs.updateErr = errors.New("banco fora")

		outcome, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)

		require.ErrorContains(t, err, "gravar comprovante em revisão: banco fora")
		require.Nil(t, outcome)
	})
}

func TestCintProcessPaymentProofReviewPath(t *testing.T) {
	t.Run("confiança baixa vai para revisão com o lançamento conferido", func(t *testing.T) {
		env, ext := cintEnv(t)
		cintProofWorld(t, env)
		ext.payment = domain.PaymentExtraction{Amount: 900, Date: "2026-09-18", Payee: "Evolucsia Locadora", Confidence: 0.5}
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)

		outcome, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)
		require.NoError(t, err)

		require.Equal(t, domain.PaymentProofStatusReview, outcome.Status)
		require.Equal(t, "r-1", outcome.ReceiptID, "o lançamento conferido fica gravado para a revisão")
		require.Equal(t, "confiança da leitura abaixo do limite", outcome.Reason)

		stored := cintRepoProof(t, env, seeded.ID)
		require.Equal(t, domain.PaymentProofStatusReview, stored.Status)
		require.Equal(t, domain.PaymentDecisionPending, stored.Decision)
		require.Equal(t, "r-1", domain.Str(stored.ReceiptID))
		require.Equal(t, 0.5, *stored.Confidence)
		require.Equal(t, "Evolucsia Locadora", domain.Str(stored.ExtractedPayee))
		require.Equal(t, []byte("comprovante"), ext.docs[0].Bytes)

		receipt := env.receipts.items[0]
		require.Nil(t, receipt.PaidAt, "revisão não dá baixa")
		require.Equal(t, []string{domain.ActionPaymentProofReview}, covAuditActions(env.audit.entries))
	})

	t.Run("sem lançamento em aberto", func(t *testing.T) {
		env, ext := cintEnv(t)
		ext.payment = domain.PaymentExtraction{Amount: 900, Date: "2026-09-18", Payee: "Evolucsia Locadora", Confidence: 0.95}
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)
		seeded.TenantID = nil
		require.NoError(t, env.proofs.UpdateDecision(context.Background(), seeded))

		outcome, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)
		require.NoError(t, err)

		require.Equal(t, domain.PaymentProofStatusReview, outcome.Status)
		require.Equal(t, "nenhum lançamento em aberto para este titular", outcome.Reason)
		require.Empty(t, outcome.ReceiptID)
		require.Nil(t, cintRepoProof(t, env, seeded.ID).ReceiptID)
	})

	t.Run("valor incompatível com o recibo", func(t *testing.T) {
		env, ext := cintEnv(t)
		cintProofWorld(t, env)
		ext.payment = domain.PaymentExtraction{Amount: 450, Date: "2026-09-18", Payee: "Evolucsia Locadora", Confidence: 0.95}
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)

		outcome, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)
		require.NoError(t, err)

		require.Equal(t, domain.PaymentProofStatusReview, outcome.Status)
		require.Equal(t, "valor do comprovante diferente do lançamento", outcome.Reason)
		require.Empty(t, outcome.ReceiptID, "sem lançamento aberto de valor compatível nada é gravado")
	})

	t.Run("falha ao buscar lançamentos", func(t *testing.T) {
		env, ext := cintEnv(t)
		ext.payment = domain.PaymentExtraction{Amount: 900, Date: "2026-09-18", Confidence: 0.95}
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)
		env.receipts.byTenantErr = errors.New("banco fora")

		_, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)

		require.ErrorContains(t, err, "buscar lançamentos do titular: banco fora")
	})

	t.Run("falha ao ler o nome do locador", func(t *testing.T) {
		env, ext := cintEnv(t)
		ext.payment = domain.PaymentExtraction{Amount: 900, Date: "2026-09-18", Confidence: 0.95}
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)
		env.deps.Repos.Settings = &cintSettingsKey{covSettings: env.settings, failKey: domain.SettingLandlordName, err: errors.New("config fora")}

		_, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)

		require.ErrorContains(t, err, "ler configuração landlord_name: config fora")
	})

	t.Run("falha ao ler o CPF do locador", func(t *testing.T) {
		env, ext := cintEnv(t)
		ext.payment = domain.PaymentExtraction{Amount: 900, Date: "2026-09-18", Confidence: 0.95}
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)
		env.deps.Repos.Settings = &cintSettingsKey{covSettings: env.settings, failKey: domain.SettingLandlordCPF, err: errors.New("config fora")}

		_, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)

		require.ErrorContains(t, err, "ler configuração landlord_cpf: config fora")
	})
}

func TestCintProcessPaymentProofAcceptsAutomatically(t *testing.T) {
	env, ext := cintEnv(t)
	cintProofWorld(t, env)
	ext.raw = "resposta-bruta"
	ext.payment = domain.PaymentExtraction{
		Amount: 900, Date: "2026-09-18", Payer: "Ana", Payee: "Evolucsia Locadora", Identifier: "E2E-1", Confidence: 0.95,
	}
	seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)

	outcome, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)
	require.NoError(t, err)

	require.Equal(t, domain.PaymentProofStatusAccepted, outcome.Status)
	require.Equal(t, domain.PaymentDecisionAuto, outcome.Decision)
	require.Equal(t, "r-1", outcome.ReceiptID)
	require.Equal(t, "valor, destinatário e data conferem", outcome.Reason)
	require.Equal(t, 900.0, outcome.Extracted.Amount)
	require.Equal(t, "E2E-1", outcome.Extracted.Identifier)

	expectedPaidAt := format.FormatLocalDateTime(time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
	receipt := env.receipts.items[0]
	require.Equal(t, expectedPaidAt, domain.Str(receipt.PaidAt), "a data do comprovante vira data da baixa")
	require.Equal(t, 900.0, *receipt.PaidAmount)

	stored := cintRepoProof(t, env, seeded.ID)
	require.Equal(t, domain.PaymentProofStatusAccepted, stored.Status)
	require.Equal(t, domain.PaymentDecisionAuto, stored.Decision)
	require.Equal(t, "r-1", domain.Str(stored.ReceiptID))
	require.Nil(t, stored.DecidedBy, "baixa automática não tem autor humano")
	require.Equal(t, format.FormatUTCStamp(covToday()), domain.Str(stored.DecidedAt))
	require.Equal(t, "resposta-bruta", domain.Str(stored.RawResponse))
	require.Equal(t, "Evolucsia Locadora", domain.Str(stored.ExtractedPayee))

	require.Equal(t, []string{domain.ActionReceiptPay, domain.ActionPaymentProofAccepted}, covAuditActions(env.audit.entries))
	last := env.audit.entries[len(env.audit.entries)-1]
	require.Equal(t, domain.EntityPayment, last.Entity)
	require.Equal(t, "comprovante.pdf", domain.Str(last.EntityLabel))
	require.Equal(t, "Baixa automática por comprovante em Ana · Casa · 09/2026", last.Summary)
	require.Equal(t, "auto", cintAuditMeta(t, last)["decision"])
}

func TestCintProcessPaymentProofAcceptFailures(t *testing.T) {
	t.Run("falha na baixa", func(t *testing.T) {
		env, ext := cintEnv(t)
		cintProofWorld(t, env)
		ext.payment = domain.PaymentExtraction{Amount: 900, Date: "2026-09-18", Payee: "Evolucsia Locadora", Confidence: 0.95}
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)
		env.receipts.payErr = errors.New("baixa recusada")

		outcome, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)

		require.ErrorContains(t, err, "dar baixa pelo comprovante: baixa recusada")
		require.Nil(t, outcome)
		require.Equal(t, domain.PaymentProofStatusPending, cintRepoProof(t, env, seeded.ID).Status)
	})

	t.Run("falha ao gravar a decisão", func(t *testing.T) {
		env, ext := cintEnv(t)
		cintProofWorld(t, env)
		ext.payment = domain.PaymentExtraction{Amount: 900, Date: "2026-09-18", Payee: "Evolucsia Locadora", Confidence: 0.95}
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)
		env.proofs.updateErr = errors.New("banco fora")

		_, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)

		require.ErrorContains(t, err, "gravar a decisão do comprovante: banco fora")
	})

	t.Run("falha na trilha da baixa automática", func(t *testing.T) {
		env, ext := cintEnv(t)
		cintProofWorld(t, env)
		ext.payment = domain.PaymentExtraction{Amount: 900, Date: "2026-09-18", Payee: "Evolucsia Locadora", Confidence: 0.95}
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)
		env.audit.insertErr = errors.New("trilha fora")

		_, err := ProcessPaymentProof(context.Background(), env.deps, seeded.ID)

		require.ErrorContains(t, err, "trilha fora")
	})
}

func TestCintReadPaymentProof(t *testing.T) {
	t.Run("sem provedor", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.deps.Proofs = nil

		_, _, err := readPaymentProof(context.Background(), env.deps, &domain.PaymentProof{})

		require.ErrorIs(t, err, errProofExtractorUnavailable)
	})

	t.Run("sem armazenamento", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.deps.Attachments = nil

		_, _, err := readPaymentProof(context.Background(), env.deps, &domain.PaymentProof{FilePath: domain.StrPtr("x")})

		require.ErrorIs(t, err, errProofFileUnavailable)
	})

	t.Run("arquivo ausente", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, _, err := readPaymentProof(context.Background(), env.deps, &domain.PaymentProof{FilePath: domain.StrPtr("nao-existe")})

		require.ErrorIs(t, err, errProofFileUnavailable)
	})

	t.Run("lê o arquivo guardado", func(t *testing.T) {
		env, ext := cintEnv(t)
		ext.payment = domain.PaymentExtraction{Amount: 10, Confidence: 0.9}
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)

		extraction, raw, err := readPaymentProof(context.Background(), env.deps, seeded)

		require.NoError(t, err)
		require.Equal(t, 10.0, extraction.Amount)
		require.Empty(t, raw)
		require.Len(t, ext.docs, 1)
		require.Equal(t, []byte("comprovante"), ext.docs[0].Bytes)
		require.Equal(t, "application/pdf", ext.docs[0].Mime)
		require.Equal(t, "comprovante.pdf", ext.docs[0].FileName)
	})
}

func TestCintReviewPaymentProof(t *testing.T) {
	t.Run("falha ao gravar", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)
		env.proofs.updateErr = errors.New("banco fora")

		_, err := reviewPaymentProof(context.Background(), env.deps, covCtx(), seeded, "motivo")

		require.ErrorContains(t, err, "gravar comprovante em revisão: banco fora")
	})

	t.Run("falha na trilha", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)
		env.audit.insertErr = errors.New("trilha fora")

		_, err := reviewPaymentProof(context.Background(), env.deps, covCtx(), seeded, "motivo")

		require.ErrorContains(t, err, "trilha fora")
	})

	t.Run("grava a revisão com o motivo", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusPending)

		updated, err := reviewPaymentProof(context.Background(), env.deps, covCtxOf(domain.AuditOriginJob), seeded, "valor divergente")
		require.NoError(t, err)

		require.Equal(t, domain.PaymentProofStatusReview, updated.Status)
		require.Equal(t, domain.PaymentDecisionPending, updated.Decision)
		require.Equal(t, "valor divergente", domain.Str(updated.DecisionReason))

		stored := cintRepoProof(t, env, seeded.ID)
		require.Equal(t, domain.PaymentProofStatusReview, stored.Status)
		require.Equal(t, format.FormatUTCStamp(covToday()), stored.UpdatedAt)

		require.Len(t, env.audit.entries, 1)
		require.Equal(t, domain.ActionPaymentProofReview, env.audit.entries[0].Action)
		require.Equal(t, domain.AuditOriginJob, env.audit.entries[0].Origin)
		require.Equal(t, "Comprovante em revisão: valor divergente", env.audit.entries[0].Summary)
		require.Equal(t, "comprovante.pdf", domain.Str(env.audit.entries[0].EntityLabel))
	})
}

func TestCintOpenReceiptForTenant(t *testing.T) {
	t.Run("sem titular", func(t *testing.T) {
		env, _ := cintEnv(t)

		receipt, err := openReceiptForTenant(context.Background(), env.deps, "  ")

		require.NoError(t, err)
		require.Nil(t, receipt)
	})

	t.Run("falha ao listar", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.receipts.byTenantErr = errors.New("banco fora")

		_, err := openReceiptForTenant(context.Background(), env.deps, "t-1")

		require.ErrorContains(t, err, "buscar lançamentos do titular: banco fora")
	})

	t.Run("sem lançamentos", func(t *testing.T) {
		env, _ := cintEnv(t)

		receipt, err := openReceiptForTenant(context.Background(), env.deps, "t-1")

		require.NoError(t, err)
		require.Nil(t, receipt)
	})

	t.Run("todos quitados", func(t *testing.T) {
		env, _ := cintEnv(t)
		paid := covPaidReceipt(covReceiptOf("r-1", "t-1", "", "2026-09-10", 900), "10/09/2026 09:00", 900)
		require.NoError(t, env.receipts.Create(context.Background(), &paid))

		receipt, err := openReceiptForTenant(context.Background(), env.deps, "t-1")

		require.NoError(t, err)
		require.Nil(t, receipt)
	})

	t.Run("escolhe o vencimento mais antigo em aberto", func(t *testing.T) {
		env, _ := cintEnv(t)
		paid := covPaidReceipt(covReceiptOf("r-pago", "t-1", "", "2026-08-10", 900), "10/08/2026 09:00", 900)
		late := covReceiptOf("r-tarde", "t-1", "", "2026-11-10", 900)
		early := covReceiptOf("r-cedo", "t-1", "", "2026-10-10", 900)
		other := covReceiptOf("r-outro", "t-2", "", "2026-01-10", 900)
		for _, receipt := range []domain.Receipt{late, paid, early, other} {
			copied := receipt
			require.NoError(t, env.receipts.Create(context.Background(), &copied))
		}

		receipt, err := openReceiptForTenant(context.Background(), env.deps, "t-1")

		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, "r-cedo", receipt.ID)
	})
}

// ── Fila de revisão ───────────────────────────────────────────────

func TestCintPaymentReviewLabels(t *testing.T) {
	require.Equal(t, "WhatsApp", paymentProofSourceLabel(domain.PaymentProofSourceWhatsApp))
	require.Equal(t, "Área do cliente", paymentProofSourceLabel(domain.PaymentProofSourcePortal))
	require.Equal(t, "Painel", paymentProofSourceLabel(domain.PaymentProofSourcePanel))
	require.Equal(t, "origem-x", paymentProofSourceLabel(domain.PaymentProofSource("origem-x")))

	require.Equal(t, "-", paymentProofConfidenceLabel(0))
	require.Equal(t, "-", paymentProofConfidenceLabel(-1))
	require.Equal(t, "90%", paymentProofConfidenceLabel(0.9))

	require.Equal(t, "", paymentProofFileURL(&domain.PaymentProof{ID: "p-1"}))
	require.Equal(t, "/api/payments/proof/p-1/file", paymentProofFileURL(&domain.PaymentProof{ID: "p-1", FilePath: domain.StrPtr("store/p-1")}))

	require.Equal(t, "comprovante.pdf · Ana", paymentProofDocumentLabel(&domain.PaymentProof{ID: "p-1", FileName: domain.StrPtr("comprovante.pdf")}, "Ana"))
	require.Equal(t, "comprovante.pdf", paymentProofDocumentLabel(&domain.PaymentProof{ID: "p-1", FileName: domain.StrPtr("comprovante.pdf")}, ""))
	require.Equal(t, "Comprovante p-1 · Ana", paymentProofDocumentLabel(&domain.PaymentProof{ID: "p-1"}, "Ana"))

	require.Equal(t, "aceito na revisão", paymentProofDecisionReason("", "aceito na revisão"))
	require.Equal(t, "aceito na revisão", paymentProofDecisionReason("   ", "aceito na revisão"))
	require.Equal(t, "aceito na revisão: valor confere", paymentProofDecisionReason(" valor confere ", "aceito na revisão"))
}

func TestCintListPaymentProofReview(t *testing.T) {
	t.Run("limite padrão ignora comprovante já decidido", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintSeedProof(t, env, domain.PaymentProofStatusAccepted)
		pending := cintSeedProof(t, env, domain.PaymentProofStatusReview)

		rows, err := ListPaymentProofReview(context.Background(), env.deps, 0)

		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, pending.ID, rows[0].ID)
	})

	t.Run("limite informado é respeitado", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintSeedProof(t, env, domain.PaymentProofStatusReview)
		cintSeedProof(t, env, domain.PaymentProofStatusReview)

		rows, err := ListPaymentProofReview(context.Background(), env.deps, 1)

		require.NoError(t, err)
		require.Len(t, rows, 1)
	})

	t.Run("falha ao listar", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.proofs.listErr = errors.New("banco fora")

		_, err := ListPaymentProofReview(context.Background(), env.deps, 0)

		require.ErrorContains(t, err, "listar comprovantes em revisão: banco fora")
	})

	t.Run("falha ao montar a linha", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)
		seeded.Source = domain.PaymentProofSourceWhatsApp
		env.tenants.byIDErr = errors.New("banco fora")

		_, err := ListPaymentProofReview(context.Background(), env.deps, 0)

		require.ErrorContains(t, err, "buscar titular do comprovante: banco fora")
	})
}

func TestCintPaymentProofRow(t *testing.T) {
	t.Run("linha completa", func(t *testing.T) {
		env, _ := cintEnv(t)
		tenant := covTenantOf("t-1", "Ana Souza")
		require.NoError(t, env.tenants.Create(context.Background(), &tenant))
		receipt := covReceiptOf("r-1", "t-1", "", "2026-09-20", 900)
		receipt.TenantName = "Ana Souza"
		receipt.PropertyName = "Casa"
		require.NoError(t, env.receipts.Create(context.Background(), &receipt))

		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)
		seeded.ReceiptID = domain.StrPtr("r-1")
		seeded.Source = domain.PaymentProofSourceWhatsApp
		seeded.Phone = domain.StrPtr("21999990000")
		seeded.ExtractedAmount = domain.FloatPtr(900)
		seeded.ExtractedDate = domain.StrPtr("2026-09-18")
		seeded.ExtractedPayer = domain.StrPtr("Ana")
		seeded.ExtractedPayee = domain.StrPtr("Locadora")
		seeded.ExtractedIdentifier = domain.StrPtr("E2E-1")
		seeded.Confidence = domain.FloatPtr(0.75)
		seeded.DecisionReason = domain.StrPtr("confiança baixa")

		row, err := paymentProofRow(context.Background(), env.deps, seeded)

		require.NoError(t, err)
		require.Equal(t, seeded.ID, row.ID)
		require.Equal(t, "Ana Souza", row.TenantName)
		require.Equal(t, "t-1", row.TenantID)
		require.Equal(t, "WhatsApp", row.SourceLabel)
		require.Equal(t, "21999990000", row.Phone)
		require.Equal(t, "r-1", row.ReceiptID)
		require.Equal(t, 900.0, row.ReceiptAmount)
		require.Equal(t, "2026-09-20", row.ReceiptDueDate)
		require.Equal(t, "Ana Souza · Casa · 09/2026", row.ReceiptLabel)
		require.False(t, row.ReceiptPaid)
		require.Equal(t, 900.0, row.ExtractedAmount)
		require.Equal(t, "2026-09-18", row.ExtractedDate)
		require.Equal(t, "Ana", row.ExtractedPayer)
		require.Equal(t, "Locadora", row.ExtractedPayee)
		require.Equal(t, "E2E-1", row.ExtractedIdentifier)
		require.Equal(t, "75%", row.ConfidenceLabel)
		require.Equal(t, "confiança baixa", row.Reason)
		require.Equal(t, "comprovante.pdf · Ana Souza", row.DocumentLabel)
		require.Equal(t, "/api/payments/proof/"+seeded.ID+"/file", row.FileURL)
		require.Equal(t, string(domain.PaymentProofStatusReview), row.Status)
		require.Equal(t, domain.PaymentProofStatusLabel(domain.PaymentProofStatusReview), row.StatusLabel)
		require.Equal(t, domain.PaymentDecisionLabel(domain.PaymentDecisionPending), row.DecisionLabel)
	})

	t.Run("lançamento quitado aparece marcado", func(t *testing.T) {
		env, _ := cintEnv(t)
		paid := covPaidReceipt(covReceiptOf("r-1", "t-1", "", "2026-09-20", 900), "18/09/2026 10:00", 900)
		require.NoError(t, env.receipts.Create(context.Background(), &paid))
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)
		seeded.ReceiptID = domain.StrPtr("r-1")

		row, err := paymentProofRow(context.Background(), env.deps, seeded)

		require.NoError(t, err)
		require.True(t, row.ReceiptPaid)
		require.Equal(t, "Comprovante "+seeded.ID, row.DocumentLabel, "sem título cadastrado a linha mostra o protocolo")
	})

	t.Run("cadastro apagado não derruba a fila", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)
		seeded.ReceiptID = domain.StrPtr("r-apagado")

		row, err := paymentProofRow(context.Background(), env.deps, seeded)

		require.NoError(t, err, "titular ou lançamento ausente não é erro")
		require.Empty(t, row.TenantName)
		require.Empty(t, row.ReceiptLabel)
		require.Zero(t, row.ReceiptAmount)
	})

	t.Run("falha ao buscar o lançamento", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)
		seeded.ReceiptID = domain.StrPtr("r-1")
		env.receipts.byIDErr = errors.New("banco fora")

		_, err := paymentProofRow(context.Background(), env.deps, seeded)

		require.ErrorContains(t, err, "buscar lançamento do comprovante: banco fora")
	})
}

func TestCintDecidePaymentProof(t *testing.T) {
	t.Run("sem comprovante", func(t *testing.T) {
		env, _ := cintEnv(t)

		err := DecidePaymentProof(context.Background(), env.deps, covCtx(), DecideInput{ProofID: "  "})

		cintWantValidation(t, err, "Comprovante não informado.")
	})

	t.Run("comprovante inexistente", func(t *testing.T) {
		env, _ := cintEnv(t)

		err := DecidePaymentProof(context.Background(), env.deps, covCtx(), DecideInput{ProofID: "nao-existe", Approve: true})

		require.ErrorContains(t, err, "buscar comprovante")
	})

	t.Run("decisão repetida", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusRejected)

		err := DecidePaymentProof(context.Background(), env.deps, covCtx(), DecideInput{ProofID: seeded.ID, Approve: true})

		cintWantValidation(t, err, "Este comprovante já foi decidido.")
	})

	t.Run("recusa é encaminhada", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)

		err := DecidePaymentProof(context.Background(), env.deps, covCtx(), DecideInput{ProofID: seeded.ID, Reason: "valor divergente"})

		require.NoError(t, err)
		require.Equal(t, domain.PaymentProofStatusRejected, cintRepoProof(t, env, seeded.ID).Status)
	})

	t.Run("aceite é encaminhado", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintProofWorld(t, env)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)

		err := DecidePaymentProof(context.Background(), env.deps, covCtx(), DecideInput{ProofID: seeded.ID, Approve: true, ReceiptID: "r-1"})

		require.NoError(t, err)
		require.Equal(t, domain.PaymentProofStatusAccepted, cintRepoProof(t, env, seeded.ID).Status)
		require.Equal(t, format.FormatLocalDateTime(covToday()), domain.Str(env.receipts.items[0].PaidAt))
	})
}

func TestCintApprovePaymentProof(t *testing.T) {
	cintOpenReceipts := func(t *testing.T, env *covEnv) {
		t.Helper()
		tenant := covTenantOf("t-1", "Ana Souza")
		require.NoError(t, env.tenants.Create(context.Background(), &tenant))
		first := covReceiptOf("r-1", "t-1", "", "2026-09-20", 900)
		second := covReceiptOf("r-2", "t-1", "", "2026-10-20", 900)
		require.NoError(t, env.receipts.Create(context.Background(), &first))
		require.NoError(t, env.receipts.Create(context.Background(), &second))
	}

	t.Run("lançamento escolhido na revisão", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintOpenReceipts(t, env)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)
		seeded.ExtractedDate = domain.StrPtr("2026-09-15")

		err := approvePaymentProof(context.Background(), env.deps, covCtx(), seeded, DecideInput{
			Approve: true, ReceiptID: "r-2", Reason: " valor confere ", DecidedBy: " user-9 ",
		})
		require.NoError(t, err)

		require.Equal(t, "15/09/2026 00:00", domain.Str(env.receipts.items[1].PaidAt), "a data do comprovante vira data da baixa")
		require.Nil(t, env.receipts.items[0].PaidAt, "o outro lançamento fica em aberto")

		stored := cintRepoProof(t, env, seeded.ID)
		require.Equal(t, domain.PaymentProofStatusAccepted, stored.Status)
		require.Equal(t, domain.PaymentDecisionManual, stored.Decision)
		require.Equal(t, "r-2", domain.Str(stored.ReceiptID))
		require.Equal(t, "aceito na revisão: valor confere", domain.Str(stored.DecisionReason))
		require.Equal(t, "user-9", domain.Str(stored.DecidedBy))
		require.Equal(t, format.FormatUTCStamp(covToday()), domain.Str(stored.DecidedAt))

		last := env.audit.entries[len(env.audit.entries)-1]
		require.Equal(t, domain.ActionPaymentProofAccepted, last.Action)
		require.Equal(t, "Comprovante aceito na revisão em Competência 10/2026", last.Summary)
		metadata := cintAuditMeta(t, last)
		require.Equal(t, "r-2", metadata["receiptId"])
		require.Equal(t, "manual", metadata["decision"])
		require.Equal(t, "user-9", metadata["decidedBy"])
	})

	t.Run("lançamento conferido na leitura", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintOpenReceipts(t, env)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)
		seeded.ReceiptID = domain.StrPtr("r-2")

		err := approvePaymentProof(context.Background(), env.deps, covCtx(), seeded, DecideInput{Approve: true})
		require.NoError(t, err)

		require.Equal(t, "r-2", domain.Str(cintRepoProof(t, env, seeded.ID).ReceiptID))
		require.NotNil(t, env.receipts.items[1].PaidAt)
		require.Equal(t, "aceito na revisão", domain.Str(cintRepoProof(t, env, seeded.ID).DecisionReason))
	})

	t.Run("sem escolha vale o lançamento em aberto do titular", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintOpenReceipts(t, env)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)

		err := approvePaymentProof(context.Background(), env.deps, covCtx(), seeded, DecideInput{Approve: true})
		require.NoError(t, err)

		require.Equal(t, "r-1", domain.Str(cintRepoProof(t, env, seeded.ID).ReceiptID))
		require.NotNil(t, env.receipts.items[0].PaidAt)
	})

	t.Run("sem lançamento em aberto", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)
		seeded.TenantID = nil

		err := approvePaymentProof(context.Background(), env.deps, covCtx(), seeded, DecideInput{Approve: true})

		cintWantValidation(t, err, "Nenhum lançamento em aberto para dar baixa.")
		require.Equal(t, domain.PaymentProofStatusReview, cintRepoProof(t, env, seeded.ID).Status)
	})

	t.Run("falha ao buscar o lançamento em aberto", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)
		env.receipts.byTenantErr = errors.New("banco fora")

		err := approvePaymentProof(context.Background(), env.deps, covCtx(), seeded, DecideInput{Approve: true})

		require.ErrorContains(t, err, "buscar lançamentos do titular: banco fora")
	})

	t.Run("falha na baixa", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintOpenReceipts(t, env)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)
		env.receipts.payErr = errors.New("baixa recusada")

		err := approvePaymentProof(context.Background(), env.deps, covCtx(), seeded, DecideInput{Approve: true, ReceiptID: "r-1"})

		require.ErrorContains(t, err, "dar baixa no lançamento: baixa recusada")
		require.Equal(t, domain.PaymentProofStatusReview, cintRepoProof(t, env, seeded.ID).Status)
	})

	t.Run("falha ao gravar o aceite", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintOpenReceipts(t, env)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)
		env.proofs.updateErr = errors.New("banco fora")

		err := approvePaymentProof(context.Background(), env.deps, covCtx(), seeded, DecideInput{Approve: true, ReceiptID: "r-1"})

		require.ErrorContains(t, err, "gravar o aceite do comprovante: banco fora")
	})

	t.Run("falha na trilha do aceite", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintOpenReceipts(t, env)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)
		env.audit.insertErr = errors.New("trilha fora")

		err := approvePaymentProof(context.Background(), env.deps, covCtx(), seeded, DecideInput{Approve: true, ReceiptID: "r-1"})

		require.ErrorContains(t, err, "trilha fora")
	})
}

func TestCintRejectPaymentProof(t *testing.T) {
	t.Run("motivo obrigatório", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)

		err := rejectPaymentProof(context.Background(), env.deps, covCtx(), seeded, DecideInput{Reason: "   "})

		cintWantValidation(t, err, "Informe o motivo da recusa.")
		require.Equal(t, domain.PaymentProofStatusReview, cintRepoProof(t, env, seeded.ID).Status)
	})

	t.Run("falha ao gravar", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)
		env.proofs.updateErr = errors.New("banco fora")

		err := rejectPaymentProof(context.Background(), env.deps, covCtx(), seeded, DecideInput{Reason: "valor divergente"})

		require.ErrorContains(t, err, "gravar a recusa do comprovante: banco fora")
	})

	t.Run("falha na trilha", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)
		env.audit.insertErr = errors.New("trilha fora")

		err := rejectPaymentProof(context.Background(), env.deps, covCtx(), seeded, DecideInput{Reason: "valor divergente"})

		require.ErrorContains(t, err, "trilha fora")
	})

	t.Run("recusa grava motivo e autor", func(t *testing.T) {
		env, _ := cintEnv(t)
		seeded := cintSeedProof(t, env, domain.PaymentProofStatusReview)

		err := rejectPaymentProof(context.Background(), env.deps, covCtx(), seeded, DecideInput{Reason: "  valor divergente  ", DecidedBy: "user-9"})
		require.NoError(t, err)

		stored := cintRepoProof(t, env, seeded.ID)
		require.Equal(t, domain.PaymentProofStatusRejected, stored.Status)
		require.Equal(t, domain.PaymentDecisionManual, stored.Decision)
		require.Equal(t, "valor divergente", domain.Str(stored.DecisionReason))
		require.Equal(t, "user-9", domain.Str(stored.DecidedBy))
		require.Equal(t, format.FormatUTCStamp(covToday()), domain.Str(stored.DecidedAt))
		require.Nil(t, stored.ReceiptID, "recusa não vincula lançamento")
		require.Empty(t, env.receipts.payments, "recusa não dá baixa")

		last := env.audit.entries[len(env.audit.entries)-1]
		require.Equal(t, domain.ActionPaymentProofRejected, last.Action)
		require.Equal(t, "Comprovante recusado na revisão: valor divergente", last.Summary)
		require.Equal(t, "manual", cintAuditMeta(t, last)["decision"])
	})
}

// ── Dados de documento ────────────────────────────────────────────

// cintDocumentWorld prepara o locatário, o imóvel e as configurações do locador
// usados pelos documentos.
func cintDocumentWorld(t *testing.T, env *covEnv) {
	t.Helper()
	tenant := covTenantOf("t-1", "Ana")
	tenant.CPF = domain.StrPtr("123.456.789-09")
	tenant.Phone = domain.StrPtr("(21) 99999-0000")
	require.NoError(t, env.tenants.Create(context.Background(), &tenant))
	property := covPropertyOf("p-1", "Casa")
	property.Zip = domain.StrPtr("24000-000")
	require.NoError(t, env.props.Create(context.Background(), &property))
	cintSetting(env, domain.SettingLandlordName, " Evolucsia Locadora ")
	cintSetting(env, domain.SettingLandlordCPF, " 123 ")
	cintSetting(env, domain.SettingLandlordAddress, " Rua do Locador, 10 ")
}

// cintReceiptForDocument grava um lançamento ligado ao locatário e ao imóvel.
func cintReceiptForDocument(t *testing.T, env *covEnv) domain.Receipt {
	t.Helper()
	receipt := covReceiptOf("r-1", "t-1", "", "2026-09-10", 900)
	receipt.PropertyID = domain.StrPtr("p-1")
	receipt.Description = domain.StrPtr("Aluguel de setembro")
	require.NoError(t, env.receipts.Create(context.Background(), &receipt))
	return receipt
}

func TestCintDocumentFieldReaders(t *testing.T) {
	lease := &domain.Lease{
		TenantID: "t-1", PropertyID: "p-1", TenantName: "Ana", PropertyName: "Casa",
		StartDate: "2026-01-01", EndDate: "2026-12-31",
	}
	require.Equal(t, "t-1", leaseTenantID(lease))
	require.Equal(t, "p-1", leasePropertyID(lease))
	require.Equal(t, "Ana", leaseTenantName(lease))
	require.Equal(t, "Casa", leasePropertyName(lease))
	require.Equal(t, "2026-01-01", leaseStartDate(lease))
	require.Equal(t, "2026-12-31", leaseEndDate(lease))

	require.Empty(t, leaseTenantID(nil))
	require.Empty(t, leasePropertyID(nil))
	require.Empty(t, leaseTenantName(nil))
	require.Empty(t, leasePropertyName(nil))
	require.Empty(t, leaseStartDate(nil))
	require.Empty(t, leaseEndDate(nil))

	tenant := &domain.Tenant{Name: "Ana", CPF: domain.StrPtr("111"), Phone: domain.StrPtr("219")}
	require.Equal(t, "Ana", tenantName(tenant))
	require.Equal(t, "111", domain.Str(tenantCpf(tenant)))
	require.Equal(t, "219", domain.Str(tenantPhone(tenant)))
	require.Empty(t, tenantName(nil))
	require.Nil(t, tenantCpf(nil))
	require.Nil(t, tenantPhone(nil))

	property := &domain.Property{Name: "Casa"}
	require.Equal(t, "Casa", propertyName(property))
	require.Empty(t, propertyName(nil))
}

func TestCintJoinAddressAndFirstNonEmpty(t *testing.T) {
	require.Empty(t, joinAddress(nil))
	require.Equal(t, "Rua A, 1, Niterói, RJ", joinAddress(&domain.Property{Address: "Rua A, 1", City: "Niterói", State: "RJ"}))
	require.Equal(t, "Rua A, 1", joinAddress(&domain.Property{Address: " Rua A, 1 ", City: " ", State: ""}))
	require.Empty(t, joinAddress(&domain.Property{}))

	require.Empty(t, firstNonEmpty("", "   ", "\t"))
	require.Equal(t, "b", firstNonEmpty(" ", " b ", "c"))
	require.Equal(t, "c", firstNonEmpty("", "", "c"))
	require.Equal(t, "a", firstNonEmpty("a"))
}

func TestCintLeaseLabelSource(t *testing.T) {
	lease := &domain.Lease{TenantName: "Ana", PropertyName: "Casa"}
	require.Same(t, lease, leaseLabelSource(lease, "Outro", "Outro Imóvel"))

	unnamed := &domain.Lease{ID: "l-1"}
	synthetic := leaseLabelSource(unnamed, "Ana", "Casa")
	require.Equal(t, "Ana", synthetic.TenantName)
	require.Equal(t, "Casa", synthetic.PropertyName)
	require.Empty(t, synthetic.ID, "contrato sintético só carrega os nomes do documento")

	fromNil := leaseLabelSource(nil, "Locatário", "Imóvel")
	require.Equal(t, "Locatário", fromNil.TenantName)
	require.Equal(t, "Imóvel", fromNil.PropertyName)
}

func TestCintDocumentAuth(t *testing.T) {
	t.Run("sem emissor", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.deps.Signer = nil

		_, _, _, err := documentAuth(context.Background(), env.deps, documentKindReceipt, "r-1")

		require.ErrorContains(t, err, "emissor de tokens de autenticidade não configurado")
	})

	t.Run("emite token, chave e link público", func(t *testing.T) {
		env, _ := cintEnv(t)

		token, code, url, err := documentAuth(context.Background(), env.deps, documentKindReceipt, "r-1")
		require.NoError(t, err)

		require.Equal(t, "receipt-r-1-tok", token)
		require.Equal(t, "AUTH-RECEIPT-R-1-TOK", code)
		require.Equal(t, "https://painel.example/validar/receipt/r-1?t=receipt-r-1-tok", url)
	})

	t.Run("link usa a base configurada na instância", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintSetting(env, domain.SettingAppURL, "https://aluxen.example/")

		_, _, url, err := documentAuth(context.Background(), env.deps, documentKindLease, "l-1")
		require.NoError(t, err)

		require.Equal(t, "https://aluxen.example/validar/lease/l-1?t=lease-l-1-tok", url)
	})
}

func TestCintLandlordSettings(t *testing.T) {
	t.Run("padrões do app quando nada está configurado", func(t *testing.T) {
		env, _ := cintEnv(t)

		name, cpf, err := landlordSettings(context.Background(), env.deps)

		require.NoError(t, err)
		require.Equal(t, landlordFallback, name)
		require.Empty(t, cpf)
	})

	t.Run("valores configurados vêm aparados", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintSetting(env, domain.SettingLandlordName, "  Evolucsia Locadora  ")
		cintSetting(env, domain.SettingLandlordCPF, " 123.456.789-09 ")

		name, cpf, err := landlordSettings(context.Background(), env.deps)

		require.NoError(t, err)
		require.Equal(t, "Evolucsia Locadora", name)
		require.Equal(t, "123.456.789-09", cpf)
	})

	t.Run("falha ao ler o nome", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.deps.Repos.Settings = &cintSettingsKey{covSettings: env.settings, failKey: domain.SettingLandlordName, err: errors.New("config fora")}

		_, _, err := landlordSettings(context.Background(), env.deps)

		require.ErrorContains(t, err, "ler configuração landlord_name: config fora")
	})

	t.Run("falha ao ler o CPF", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.deps.Repos.Settings = &cintSettingsKey{covSettings: env.settings, failKey: domain.SettingLandlordCPF, err: errors.New("config fora")}

		_, _, err := landlordSettings(context.Background(), env.deps)

		require.ErrorContains(t, err, "ler configuração landlord_cpf: config fora")
	})
}

func TestCintOptionalLoaders(t *testing.T) {
	t.Run("locatário", func(t *testing.T) {
		env, _ := cintEnv(t)
		tenant := covTenantOf("t-1", "Ana")
		require.NoError(t, env.tenants.Create(context.Background(), &tenant))

		loaded, err := optionalTenant(context.Background(), env.deps, " t-1 ")
		require.NoError(t, err)
		require.Equal(t, "Ana", loaded.Name)

		missing, err := optionalTenant(context.Background(), env.deps, "nao-existe")
		require.NoError(t, err)
		require.Nil(t, missing, "cadastro incompleto não impede o documento")

		empty, err := optionalTenant(context.Background(), env.deps, "  ")
		require.NoError(t, err)
		require.Nil(t, empty)

		env.tenants.byIDErr = errors.New("banco fora")
		_, err = optionalTenant(context.Background(), env.deps, "t-1")
		require.ErrorContains(t, err, "carregar o locatário t-1: banco fora")
	})

	t.Run("imóvel", func(t *testing.T) {
		env, _ := cintEnv(t)
		property := covPropertyOf("p-1", "Casa")
		require.NoError(t, env.props.Create(context.Background(), &property))

		loaded, err := optionalProperty(context.Background(), env.deps, "p-1")
		require.NoError(t, err)
		require.Equal(t, "Casa", loaded.Name)

		missing, err := optionalProperty(context.Background(), env.deps, "nao-existe")
		require.NoError(t, err)
		require.Nil(t, missing)

		empty, err := optionalProperty(context.Background(), env.deps, "")
		require.NoError(t, err)
		require.Nil(t, empty)

		env.props.byIDErr = errors.New("banco fora")
		_, err = optionalProperty(context.Background(), env.deps, "p-1")
		require.ErrorContains(t, err, "carregar o imóvel p-1: banco fora")
	})

	t.Run("contrato", func(t *testing.T) {
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))

		loaded, err := optionalLease(context.Background(), env.deps, "l-1")
		require.NoError(t, err)
		require.Equal(t, "t-1", loaded.TenantID)

		missing, err := optionalLease(context.Background(), env.deps, "nao-existe")
		require.NoError(t, err)
		require.Nil(t, missing)

		empty, err := optionalLease(context.Background(), env.deps, "   ")
		require.NoError(t, err)
		require.Nil(t, empty)

		env.leases.byIDErr = errors.New("banco fora")
		_, err = optionalLease(context.Background(), env.deps, "l-1")
		require.ErrorContains(t, err, "carregar o contrato l-1: banco fora")
	})
}

func TestCintLeaseTenantMatches(t *testing.T) {
	newEnv := func(t *testing.T) (*covEnv, domain.Lease) {
		t.Helper()
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		return env, lease
	}

	t.Run("titular confere", func(t *testing.T) {
		env, _ := newEnv(t)

		ok, err := leaseTenantMatches(context.Background(), env.deps, "l-1", "t-1")

		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("contrato de outro titular", func(t *testing.T) {
		env, _ := newEnv(t)

		ok, err := leaseTenantMatches(context.Background(), env.deps, "l-1", "t-9")

		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("contrato inexistente não pertence a ninguém", func(t *testing.T) {
		env, _ := newEnv(t)

		ok, err := leaseTenantMatches(context.Background(), env.deps, "nao-existe", "t-1")

		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("falha ao buscar o contrato", func(t *testing.T) {
		env, _ := newEnv(t)
		env.leases.byIDErr = errors.New("banco fora")

		_, err := leaseTenantMatches(context.Background(), env.deps, "l-1", "t-1")

		require.ErrorContains(t, err, "carregar o contrato l-1: banco fora")
	})
}

func TestCintReceiptDocumentInput(t *testing.T) {
	t.Run("sem id", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := ReceiptDocumentInput(context.Background(), env.deps, "  ")

		require.ErrorIs(t, err, domain.ErrNotFound)
	})

	t.Run("lançamento inexistente", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := ReceiptDocumentInput(context.Background(), env.deps, "r-1")

		require.ErrorIs(t, err, domain.ErrNotFound)
	})

	t.Run("falha ao buscar", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.receipts.byIDErr = errors.New("banco fora")

		_, err := ReceiptDocumentInput(context.Background(), env.deps, "r-1")

		require.ErrorContains(t, err, "carregar o recibo r-1: banco fora")
	})

	t.Run("recibo pago parcialmente", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintDocumentWorld(t, env)
		receipt := cintReceiptForDocument(t, env)
		receipt.PaidAt = domain.StrPtr("05/09/2026 10:00")
		receipt.PaidAmount = domain.FloatPtr(300)
		env.receipts.items[0] = receipt

		data, err := ReceiptDocumentInput(context.Background(), env.deps, "r-1")
		require.NoError(t, err)

		require.Equal(t, "r-1", data.ReceiptID)
		require.Equal(t, "Ana", data.TenantName, "sem nome no lançamento o cadastro resolve")
		require.Equal(t, "123.456.789-09", data.TenantCPF)
		require.Equal(t, "Casa", data.PropertyName)
		require.Equal(t, "Rua A, 1, Niterói, RJ", data.PropertyAddress)
		require.Equal(t, domain.ReceiptCategoryRent, data.Category)
		require.Equal(t, "Aluguel de setembro", data.Description)
		require.Equal(t, "2026-09-10", data.DueDate)
		require.Equal(t, "05/09/2026 10:00", data.PaidAt)
		require.Equal(t, 900.0, data.Amount)
		require.Equal(t, 300.0, data.PaidAmount)
		require.True(t, data.HasPaidAmount)
		require.True(t, data.IsPaid)
		require.True(t, data.IsPartial)
		require.Equal(t, "Evolucsia Locadora", data.LandlordName)
		require.Equal(t, "123", data.LandlordCPF)
		require.Equal(t, covToday(), data.IssuedAt)
		require.Equal(t, "receipt-r-1-tok", data.Token)
		require.Equal(t, "AUTH-RECEIPT-R-1-TOK", data.AuthCode)
		require.Contains(t, data.ValidationURL, "/validar/receipt/r-1?t=receipt-r-1-tok")
		require.Equal(t, "Aluguel de setembro · 09/2026", data.Label)
	})

	t.Run("recibo em aberto sem cadastro", func(t *testing.T) {
		env, _ := cintEnv(t)
		receipt := covReceiptOf("r-1", "t-1", "", "2026-09-10", 900)
		receipt.TenantName = "Titular do Lançamento"
		require.NoError(t, env.receipts.Create(context.Background(), &receipt))

		data, err := ReceiptDocumentInput(context.Background(), env.deps, "r-1")
		require.NoError(t, err)

		require.Equal(t, "Titular do Lançamento", data.TenantName)
		require.Empty(t, data.TenantCPF)
		require.Empty(t, data.PropertyName)
		require.Empty(t, data.PropertyAddress)
		require.False(t, data.HasPaidAmount)
		require.Zero(t, data.PaidAmount)
		require.False(t, data.IsPaid)
		require.False(t, data.IsPartial)
		require.Equal(t, landlordFallback, data.LandlordName)
	})

	t.Run("contrato tem precedência sobre o vínculo do lançamento", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintDocumentWorld(t, env)
		otherTenant := covTenantOf("t-2", "Do Contrato")
		otherProperty := covPropertyOf("p-2", "Imóvel do Contrato")
		require.NoError(t, env.tenants.Create(context.Background(), &otherTenant))
		require.NoError(t, env.props.Create(context.Background(), &otherProperty))
		lease := covLeaseOf("l-1", "t-2", "p-2", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		receipt := cintReceiptForDocument(t, env)
		receipt.LeaseID = domain.StrPtr("l-1")
		env.receipts.items[0] = receipt

		data, err := ReceiptDocumentInput(context.Background(), env.deps, "r-1")
		require.NoError(t, err)

		require.Equal(t, "Do Contrato", data.TenantName)
		require.Equal(t, "Imóvel do Contrato", data.PropertyName)
	})

	t.Run("contrato removido mantém o vínculo do lançamento", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintDocumentWorld(t, env)
		receipt := cintReceiptForDocument(t, env)
		receipt.LeaseID = domain.StrPtr("l-removido")
		env.receipts.items[0] = receipt

		data, err := ReceiptDocumentInput(context.Background(), env.deps, "r-1")
		require.NoError(t, err)

		require.Equal(t, "Ana", data.TenantName)
		require.Equal(t, "Casa", data.PropertyName)
	})

	t.Run("falha ao ler o contrato", func(t *testing.T) {
		env, _ := cintEnv(t)
		receipt := covReceiptOf("r-1", "t-1", "l-1", "2026-09-10", 900)
		require.NoError(t, env.receipts.Create(context.Background(), &receipt))
		env.leases.byIDErr = errors.New("banco fora")

		_, err := ReceiptDocumentInput(context.Background(), env.deps, "r-1")

		require.ErrorContains(t, err, "carregar o contrato l-1 do recibo: banco fora")
	})

	t.Run("falha ao ler o locatário", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintReceiptForDocument(t, env)
		env.tenants.byIDErr = errors.New("banco fora")

		_, err := ReceiptDocumentInput(context.Background(), env.deps, "r-1")

		require.ErrorContains(t, err, "carregar o locatário t-1: banco fora")
	})

	t.Run("falha ao ler o imóvel", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintReceiptForDocument(t, env)
		env.props.byIDErr = errors.New("banco fora")

		_, err := ReceiptDocumentInput(context.Background(), env.deps, "r-1")

		require.ErrorContains(t, err, "carregar o imóvel p-1: banco fora")
	})

	t.Run("falha nas configurações do locador", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintReceiptForDocument(t, env)
		env.deps.Repos.Settings = &cintSettingsKey{covSettings: env.settings, failKey: domain.SettingLandlordName, err: errors.New("config fora")}

		_, err := ReceiptDocumentInput(context.Background(), env.deps, "r-1")

		require.ErrorContains(t, err, "ler configuração landlord_name: config fora")
	})

	t.Run("sem emissor de token", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintReceiptForDocument(t, env)
		env.deps.Signer = nil

		_, err := ReceiptDocumentInput(context.Background(), env.deps, "r-1")

		require.ErrorContains(t, err, "emissor de tokens de autenticidade não configurado")
	})
}

func TestCintLeaseDocumentInput(t *testing.T) {
	t.Run("sem id", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := LeaseDocumentInput(context.Background(), env.deps, "  ")

		require.ErrorIs(t, err, domain.ErrNotFound)
	})

	t.Run("contrato inexistente", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := LeaseDocumentInput(context.Background(), env.deps, "l-1")

		require.ErrorIs(t, err, domain.ErrNotFound)
	})

	t.Run("falha ao buscar", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.leases.byIDErr = errors.New("banco fora")

		_, err := LeaseDocumentInput(context.Background(), env.deps, "l-1")

		require.ErrorContains(t, err, "carregar o contrato l-1: banco fora")
	})

	t.Run("dados completos do contrato", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintDocumentWorld(t, env)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		lease.TenantName = "Ana"
		lease.PropertyName = "Casa"
		require.NoError(t, env.leases.Create(context.Background(), &lease))

		data, err := LeaseDocumentInput(context.Background(), env.deps, "l-1")
		require.NoError(t, err)

		require.Equal(t, "Ana", data.TenantName)
		require.Equal(t, "(21) 99999-0000", data.TenantPhone)
		require.Equal(t, "123.456.789-09", data.TenantCPF)
		require.Equal(t, "Casa", data.PropertyName)
		require.Equal(t, "Rua A, 1, Niterói, RJ", data.PropertyAddress)
		require.Equal(t, "Rua A, 1", data.PropertyStreet)
		require.Equal(t, "Niterói", data.PropertyCity)
		require.Equal(t, "RJ", data.PropertyState)
		require.Equal(t, "24000-000", data.PropertyZip)
		require.Equal(t, "2026-01-01", data.StartDate)
		require.Equal(t, "2026-12-31", data.EndDate)
		require.Equal(t, 10, data.DueDay)
		require.Equal(t, 900.0, data.RentAmount)
		require.Equal(t, domain.LeaseStatusActive, data.Status)
		require.Equal(t, "Evolucsia Locadora", data.LandlordName)
		require.Equal(t, "123", data.LandlordCPF)
		require.Equal(t, "Rua do Locador, 10", data.LandlordAddress)
		require.Equal(t, "lease-l-1-tok", data.Token)
		require.Equal(t, "Ana · Casa", data.Label)
		require.Equal(t, covToday(), data.IssuedAt)
	})

	t.Run("contrato sem cadastro vinculado", func(t *testing.T) {
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))

		data, err := LeaseDocumentInput(context.Background(), env.deps, "l-1")
		require.NoError(t, err)

		require.Equal(t, receiptTenantFallback, data.TenantName)
		require.Empty(t, data.TenantPhone)
		require.Empty(t, data.PropertyName)
		require.Empty(t, data.PropertyStreet)
		require.Empty(t, data.PropertyCity)
		require.Empty(t, data.PropertyZip)
		require.Equal(t, landlordFallback, data.LandlordName)
		require.Empty(t, data.LandlordAddress)
	})

	t.Run("falha ao ler o locatário", func(t *testing.T) {
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		env.tenants.byIDErr = errors.New("banco fora")

		_, err := LeaseDocumentInput(context.Background(), env.deps, "l-1")

		require.ErrorContains(t, err, "carregar o locatário t-1: banco fora")
	})

	t.Run("falha ao ler o imóvel", func(t *testing.T) {
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		env.props.byIDErr = errors.New("banco fora")

		_, err := LeaseDocumentInput(context.Background(), env.deps, "l-1")

		require.ErrorContains(t, err, "carregar o imóvel p-1: banco fora")
	})

	t.Run("falha nas configurações do locador", func(t *testing.T) {
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		env.deps.Repos.Settings = &cintSettingsKey{covSettings: env.settings, failKey: domain.SettingLandlordCPF, err: errors.New("config fora")}

		_, err := LeaseDocumentInput(context.Background(), env.deps, "l-1")

		require.ErrorContains(t, err, "ler configuração landlord_cpf: config fora")
	})

	t.Run("falha no endereço do locador", func(t *testing.T) {
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		env.deps.Repos.Settings = &cintSettingsKey{covSettings: env.settings, failKey: domain.SettingLandlordAddress, err: errors.New("config fora")}

		_, err := LeaseDocumentInput(context.Background(), env.deps, "l-1")

		require.ErrorContains(t, err, "ler configuração landlord_address: config fora")
	})

	t.Run("sem emissor de token", func(t *testing.T) {
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		env.deps.Signer = nil

		_, err := LeaseDocumentInput(context.Background(), env.deps, "l-1")

		require.ErrorContains(t, err, "emissor de tokens de autenticidade não configurado")
	})
}

// cintInspection grava uma vistoria do contrato informado.
func cintInspection(t *testing.T, env *covEnv, leaseID string) domain.Inspection {
	t.Helper()
	inspection := domain.Inspection{
		ID: "i-1", LeaseID: leaseID, Kind: domain.InspectionKindCheckIn,
		PerformedAt: "2026-09-01 10:00:00", Condition: domain.InspectionConditionGood,
		Notes:  domain.StrPtr("tudo certo"),
		Items:  []domain.InspectionItem{{Area: "Sala", Item: "Piso", State: "bom"}},
		Photos: []domain.InspectionPhoto{{Name: "foto1.jpg"}},
	}
	require.NoError(t, env.inspections.Create(context.Background(), &inspection))
	return inspection
}

func TestCintInspectionDocumentInput(t *testing.T) {
	t.Run("sem id", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := InspectionDocumentInput(context.Background(), env.deps, "  ")

		require.ErrorIs(t, err, domain.ErrNotFound)
	})

	t.Run("vistoria inexistente", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := InspectionDocumentInput(context.Background(), env.deps, "i-1")

		require.ErrorIs(t, err, domain.ErrNotFound)
	})

	t.Run("falha ao buscar", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.inspections.byIDErr = errors.New("banco fora")

		_, err := InspectionDocumentInput(context.Background(), env.deps, "i-1")

		require.ErrorContains(t, err, "carregar a vistoria i-1: banco fora")
	})

	t.Run("nomes do próprio documento vencem", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintDocumentWorld(t, env)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		lease.TenantName = "Nome do Contrato"
		lease.PropertyName = "Imóvel do Contrato"
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		inspection := cintInspection(t, env, "l-1")
		inspection.TenantName = "Nome da Vistoria"
		inspection.PropertyName = "Imóvel da Vistoria"
		env.inspections.items[0] = inspection

		data, err := InspectionDocumentInput(context.Background(), env.deps, "i-1")
		require.NoError(t, err)

		require.Equal(t, "Nome da Vistoria", data.TenantName)
		require.Equal(t, "Imóvel da Vistoria", data.PropertyName)
		require.Equal(t, "123.456.789-09", data.TenantCPF)
		require.Equal(t, "Rua A, 1, Niterói, RJ", data.PropertyAddress)
		require.Equal(t, "2026-01-01", data.StartDate)
		require.Equal(t, "2026-12-31", data.EndDate)
		require.Equal(t, domain.InspectionKindCheckIn, data.Kind)
		require.NotEmpty(t, data.KindLabel)
		require.Equal(t, domain.InspectionConditionGood, data.Condition)
		require.NotEmpty(t, data.ConditionLabel)
		require.Equal(t, "2026-09-01 10:00:00", data.PerformedAt)
		require.Equal(t, "tudo certo", data.Notes)
		require.Len(t, data.Items, 1)
		require.Equal(t, "Sala", data.Items[0].Area)
		require.Len(t, data.Photos, 1)
		require.Equal(t, "foto1.jpg", data.Photos[0].Name)
		require.Equal(t, "Evolucsia Locadora", data.LandlordName)
		require.Equal(t, "inspection-i-1-tok", data.Token)
		require.Equal(t, "Nome do Contrato · Imóvel do Contrato", data.Label, "com contrato, o rótulo é o do contrato")
		require.Equal(t, covToday(), data.IssuedAt)
	})

	t.Run("nomes vêm do contrato e do cadastro", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintDocumentWorld(t, env)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		lease.TenantName = "Ana do Contrato"
		lease.PropertyName = "Casa do Contrato"
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		cintInspection(t, env, "l-1")

		data, err := InspectionDocumentInput(context.Background(), env.deps, "i-1")
		require.NoError(t, err)

		require.Equal(t, "Ana do Contrato", data.TenantName)
		require.Equal(t, "Casa do Contrato", data.PropertyName)
	})

	t.Run("vistoria sem contrato usa o cadastro e o padrão", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintDocumentWorld(t, env)
		cintInspection(t, env, "")

		data, err := InspectionDocumentInput(context.Background(), env.deps, "i-1")
		require.NoError(t, err)

		require.Equal(t, receiptTenantFallback, data.TenantName)
		require.Empty(t, data.PropertyName)
		require.Empty(t, data.PropertyAddress)
		require.Empty(t, data.StartDate)
		require.Empty(t, data.EndDate)
		require.Equal(t, "Locador · ", data.Label, "sem contrato o rótulo nasce dos nomes resolvidos")
	})

	t.Run("falha ao ler o contrato", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintInspection(t, env, "l-1")
		env.leases.byIDErr = errors.New("banco fora")

		_, err := InspectionDocumentInput(context.Background(), env.deps, "i-1")

		require.ErrorContains(t, err, "carregar o contrato l-1: banco fora")
	})

	t.Run("falha ao ler o locatário", func(t *testing.T) {
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		cintInspection(t, env, "l-1")
		env.tenants.byIDErr = errors.New("banco fora")

		_, err := InspectionDocumentInput(context.Background(), env.deps, "i-1")

		require.ErrorContains(t, err, "carregar o locatário t-1: banco fora")
	})

	t.Run("falha ao ler o imóvel", func(t *testing.T) {
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		cintInspection(t, env, "l-1")
		env.props.byIDErr = errors.New("banco fora")

		_, err := InspectionDocumentInput(context.Background(), env.deps, "i-1")

		require.ErrorContains(t, err, "carregar o imóvel p-1: banco fora")
	})

	t.Run("falha nas configurações do locador", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintInspection(t, env, "")
		env.deps.Repos.Settings = &cintSettingsKey{covSettings: env.settings, failKey: domain.SettingLandlordName, err: errors.New("config fora")}

		_, err := InspectionDocumentInput(context.Background(), env.deps, "i-1")

		require.ErrorContains(t, err, "ler configuração landlord_name: config fora")
	})

	t.Run("sem emissor de token", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintInspection(t, env, "")
		env.deps.Signer = nil

		_, err := InspectionDocumentInput(context.Background(), env.deps, "i-1")

		require.ErrorContains(t, err, "emissor de tokens de autenticidade não configurado")
	})
}

func TestCintDepositDocumentInput(t *testing.T) {
	t.Run("sem id", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := DepositDocumentInput(context.Background(), env.deps, "  ")

		require.ErrorIs(t, err, domain.ErrNotFound)
	})

	t.Run("contrato inexistente", func(t *testing.T) {
		env, _ := cintEnv(t)

		_, err := DepositDocumentInput(context.Background(), env.deps, "l-1")

		require.ErrorIs(t, err, domain.ErrNotFound)
	})

	t.Run("falha ao buscar", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.leases.byIDErr = errors.New("banco fora")

		_, err := DepositDocumentInput(context.Background(), env.deps, "l-1")

		require.ErrorContains(t, err, "carregar o contrato l-1: banco fora")
	})

	t.Run("caução recebida e parcialmente devolvida", func(t *testing.T) {
		env, _ := cintEnv(t)
		cintDocumentWorld(t, env)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		lease.TenantName = "Ana"
		lease.PropertyName = "Casa"
		lease.DepositAmount = domain.FloatPtr(2000)
		lease.DepositStatus = domain.DepositStatusPartiallyRefunded
		lease.DepositReceivedAt = domain.StrPtr("2026-01-05")
		lease.DepositRefundedAt = domain.StrPtr("2026-12-20")
		lease.DepositRefundedAmt = domain.FloatPtr(500)
		lease.DepositNotes = domain.StrPtr("descontado o piso")
		require.NoError(t, env.leases.Create(context.Background(), &lease))

		data, err := DepositDocumentInput(context.Background(), env.deps, "l-1")
		require.NoError(t, err)

		require.Equal(t, "l-1", data.LeaseID)
		require.Equal(t, "Ana", data.TenantName)
		require.Equal(t, "123.456.789-09", data.TenantCPF)
		require.Equal(t, "Casa", data.PropertyName)
		require.Equal(t, "Rua A, 1, Niterói, RJ", data.PropertyAddress)
		require.Equal(t, 2000.0, data.Amount)
		require.Equal(t, 500.0, data.Refunded)
		require.Equal(t, 1500.0, data.Retained)
		require.Equal(t, domain.DepositStatusPartiallyRefunded, data.Status)
		require.NotEmpty(t, data.StatusLabel)
		require.Equal(t, "2026-01-05", data.ReceivedAt)
		require.Equal(t, "2026-12-20", data.RefundedAt)
		require.Equal(t, "descontado o piso", data.Notes)
		require.True(t, data.HasDeposit)
		require.Equal(t, "Evolucsia Locadora", data.LandlordName)
		require.Equal(t, "deposit-l-1-tok", data.Token)
		require.Equal(t, "Ana · Casa", data.Label)
	})

	t.Run("contrato sem caução não libera o recibo", func(t *testing.T) {
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))

		data, err := DepositDocumentInput(context.Background(), env.deps, "l-1")
		require.NoError(t, err)

		require.Zero(t, data.Amount)
		require.Zero(t, data.Retained)
		require.False(t, data.HasDeposit)
		require.Equal(t, domain.DepositStatusNone, data.Status)
		require.Equal(t, receiptTenantFallback, data.TenantName)
	})

	t.Run("falha ao ler o locatário", func(t *testing.T) {
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		env.tenants.byIDErr = errors.New("banco fora")

		_, err := DepositDocumentInput(context.Background(), env.deps, "l-1")

		require.ErrorContains(t, err, "carregar o locatário t-1: banco fora")
	})

	t.Run("falha ao ler o imóvel", func(t *testing.T) {
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		env.props.byIDErr = errors.New("banco fora")

		_, err := DepositDocumentInput(context.Background(), env.deps, "l-1")

		require.ErrorContains(t, err, "carregar o imóvel p-1: banco fora")
	})

	t.Run("falha nas configurações do locador", func(t *testing.T) {
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		env.deps.Repos.Settings = &cintSettingsKey{covSettings: env.settings, failKey: domain.SettingLandlordName, err: errors.New("config fora")}

		_, err := DepositDocumentInput(context.Background(), env.deps, "l-1")

		require.ErrorContains(t, err, "ler configuração landlord_name: config fora")
	})

	t.Run("sem emissor de token", func(t *testing.T) {
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		env.deps.Signer = nil

		_, err := DepositDocumentInput(context.Background(), env.deps, "l-1")

		require.ErrorContains(t, err, "emissor de tokens de autenticidade não configurado")
	})
}

// ── Chave de autenticidade e acesso ao documento ───────────────────

func TestCintDocumentTokenMatches(t *testing.T) {
	env, _ := cintEnv(t)

	require.True(t, documentTokenMatches(env.deps, documentKindReceipt, "r-1", "receipt-r-1-tok"))
	require.False(t, documentTokenMatches(env.deps, documentKindReceipt, "r-1", ""))
	require.False(t, documentTokenMatches(env.deps, documentKindReceipt, "  ", "receipt-r-1-tok"))
	require.False(t, documentTokenMatches(env.deps, documentKindReceipt, "r-1", "receipt-r-1-tok-maior"))
	require.False(t, documentTokenMatches(env.deps, documentKindReceipt, "r-1", "receipt-r-2-tok"), "chave de outro documento não vale")

	env.deps.Signer = nil
	require.False(t, documentTokenMatches(env.deps, documentKindReceipt, "r-1", "receipt-r-1-tok"))
}

func TestCintVerifyDocumentTokens(t *testing.T) {
	newEnv := func(t *testing.T) (*covEnv, domain.Receipt) {
		t.Helper()
		env, _ := cintEnv(t)
		cintDocumentWorld(t, env)
		return env, cintReceiptForDocument(t, env)
	}

	t.Run("chave inválida", func(t *testing.T) {
		env, _ := newEnv(t)

		_, err := VerifyReceiptToken(context.Background(), env.deps, "r-1", "chave-errada")

		cintWantValidation(t, err, "Chave de autenticação inválida ou documento não autenticado.")
	})

	t.Run("chave confere devolve o documento", func(t *testing.T) {
		env, _ := newEnv(t)

		data, err := VerifyReceiptToken(context.Background(), env.deps, "r-1", "receipt-r-1-tok")
		require.NoError(t, err)
		require.Equal(t, "r-1", data.ReceiptID)

		_, err = VerifyLeaseToken(context.Background(), env.deps, "l-1", "chave-errada")
		cintWantValidation(t, err, "Chave de autenticação inválida ou documento não autenticado.")

		_, err = VerifyInspectionToken(context.Background(), env.deps, "i-1", "chave-errada")
		cintWantValidation(t, err, "Chave de autenticação inválida ou documento não autenticado.")

		_, err = VerifyDepositToken(context.Background(), env.deps, "l-1", "chave-errada")
		cintWantValidation(t, err, "Chave de autenticação inválida ou documento não autenticado.")
	})

	t.Run("chave válida de documento inexistente", func(t *testing.T) {
		env, _ := newEnv(t)

		_, err := VerifyReceiptToken(context.Background(), env.deps, "r-9", "receipt-r-9-tok")
		require.ErrorIs(t, err, domain.ErrNotFound)

		_, err = VerifyLeaseToken(context.Background(), env.deps, "l-9", "lease-l-9-tok")
		require.ErrorIs(t, err, domain.ErrNotFound)

		_, err = VerifyInspectionToken(context.Background(), env.deps, "i-9", "inspection-i-9-tok")
		require.ErrorIs(t, err, domain.ErrNotFound)

		_, err = VerifyDepositToken(context.Background(), env.deps, "l-9", "deposit-l-9-tok")
		require.ErrorIs(t, err, domain.ErrNotFound)
	})
}

func TestCintCanAccessDocuments(t *testing.T) {
	newEnv := func(t *testing.T) *covEnv {
		t.Helper()
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-1", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		cintReceiptForDocument(t, env)
		cintInspection(t, env, "l-1")
		return env
	}

	t.Run("administrador vê tudo", func(t *testing.T) {
		env := newEnv(t)

		ok, err := CanAccessReceipt(context.Background(), env.deps, "r-1", "user-1", "")
		require.NoError(t, err)
		require.True(t, ok)

		ok, err = CanAccessLease(context.Background(), env.deps, "l-1", "user-1", "")
		require.NoError(t, err)
		require.True(t, ok)

		ok, err = CanAccessDeposit(context.Background(), env.deps, "l-1", "user-1", "")
		require.NoError(t, err)
		require.True(t, ok)

		ok, err = CanAccessInspection(context.Background(), env.deps, "i-1", "user-1", "")
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("sem solicitante nada é liberado", func(t *testing.T) {
		env := newEnv(t)

		ok, err := CanAccessReceipt(context.Background(), env.deps, "r-1", "", "")
		require.NoError(t, err)
		require.False(t, ok)

		ok, err = CanAccessLease(context.Background(), env.deps, "l-1", "", "")
		require.NoError(t, err)
		require.False(t, ok)

		ok, err = CanAccessDeposit(context.Background(), env.deps, "l-1", "", "")
		require.NoError(t, err)
		require.False(t, ok)

		ok, err = CanAccessInspection(context.Background(), env.deps, "i-1", "", "")
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("documento não informado não pertence a ninguém", func(t *testing.T) {
		env := newEnv(t)

		ok, err := CanAccessReceipt(context.Background(), env.deps, "  ", "", "t-1")
		require.NoError(t, err)
		require.False(t, ok)

		ok, err = CanAccessLease(context.Background(), env.deps, "  ", "", "t-1")
		require.NoError(t, err)
		require.False(t, ok)

		ok, err = CanAccessDeposit(context.Background(), env.deps, "  ", "", "t-1")
		require.NoError(t, err)
		require.False(t, ok)

		ok, err = CanAccessInspection(context.Background(), env.deps, "  ", "", "t-1")
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("titular acessa o próprio documento", func(t *testing.T) {
		env := newEnv(t)

		ok, err := CanAccessReceipt(context.Background(), env.deps, "r-1", "", "t-1")
		require.NoError(t, err)
		require.True(t, ok)

		ok, err = CanAccessLease(context.Background(), env.deps, "l-1", "", "t-1")
		require.NoError(t, err)
		require.True(t, ok)

		ok, err = CanAccessInspection(context.Background(), env.deps, "i-1", "", "t-1")
		require.NoError(t, err)
		require.True(t, ok)

		ok, err = CanAccessReceipt(context.Background(), env.deps, "r-1", "", "t-2")
		require.NoError(t, err)
		require.False(t, ok, "outro titular não vê o recibo")

		ok, err = CanAccessLease(context.Background(), env.deps, "l-1", "", "t-2")
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("documento inexistente não pertence a ninguém", func(t *testing.T) {
		env := newEnv(t)

		ok, err := CanAccessReceipt(context.Background(), env.deps, "r-9", "", "t-1")
		require.NoError(t, err)
		require.False(t, ok)

		ok, err = CanAccessInspection(context.Background(), env.deps, "i-9", "", "t-1")
		require.NoError(t, err)
		require.False(t, ok)

		ok, err = CanAccessLease(context.Background(), env.deps, "l-9", "", "t-1")
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("falha ao ler o recibo", func(t *testing.T) {
		env := newEnv(t)
		env.receipts.byIDErr = errors.New("banco fora")

		_, err := CanAccessReceipt(context.Background(), env.deps, "r-1", "", "t-1")

		require.ErrorContains(t, err, "carregar o recibo r-1: banco fora")
	})

	t.Run("falha ao ler a vistoria", func(t *testing.T) {
		env := newEnv(t)
		env.inspections.byIDErr = errors.New("banco fora")

		_, err := CanAccessInspection(context.Background(), env.deps, "i-1", "", "t-1")

		require.ErrorContains(t, err, "carregar a vistoria i-1: banco fora")
	})
}

func TestCintReceiptOwnerTenantID(t *testing.T) {
	t.Run("contrato tem precedência", func(t *testing.T) {
		env, _ := cintEnv(t)
		lease := covLeaseOf("l-1", "t-2", "p-1", domain.LeaseStatusActive, "2026-01-01", "2026-12-31", 900)
		require.NoError(t, env.leases.Create(context.Background(), &lease))
		receipt := covReceiptOf("r-1", "t-1", "l-1", "2026-09-10", 900)
		require.NoError(t, env.receipts.Create(context.Background(), &receipt))

		owner, err := receiptOwnerTenantID(context.Background(), env.deps, "r-1")

		require.NoError(t, err)
		require.Equal(t, "t-2", owner)
	})

	t.Run("contrato removido cai no vínculo do lançamento", func(t *testing.T) {
		env, _ := cintEnv(t)
		receipt := covReceiptOf("r-1", "t-1", "l-removido", "2026-09-10", 900)
		require.NoError(t, env.receipts.Create(context.Background(), &receipt))

		owner, err := receiptOwnerTenantID(context.Background(), env.deps, "r-1")

		require.NoError(t, err)
		require.Equal(t, "t-1", owner)
	})

	t.Run("lançamento sem titular", func(t *testing.T) {
		env, _ := cintEnv(t)
		receipt := covReceiptOf("r-1", "", "", "2026-09-10", 900)
		require.NoError(t, env.receipts.Create(context.Background(), &receipt))

		owner, err := receiptOwnerTenantID(context.Background(), env.deps, "r-1")

		require.NoError(t, err)
		require.Empty(t, owner)
	})

	t.Run("lançamento inexistente", func(t *testing.T) {
		env, _ := cintEnv(t)

		owner, err := receiptOwnerTenantID(context.Background(), env.deps, "r-9")

		require.NoError(t, err)
		require.Empty(t, owner)
	})

	t.Run("falha ao ler o lançamento", func(t *testing.T) {
		env, _ := cintEnv(t)
		env.receipts.byIDErr = errors.New("banco fora")

		_, err := receiptOwnerTenantID(context.Background(), env.deps, "r-1")

		require.ErrorContains(t, err, "carregar o recibo r-1: banco fora")
	})

	t.Run("falha ao ler o contrato do lançamento", func(t *testing.T) {
		env, _ := cintEnv(t)
		receipt := covReceiptOf("r-1", "t-1", "l-1", "2026-09-10", 900)
		require.NoError(t, env.receipts.Create(context.Background(), &receipt))
		env.leases.byIDErr = errors.New("banco fora")

		_, err := receiptOwnerTenantID(context.Background(), env.deps, "r-1")

		require.ErrorContains(t, err, "carregar o contrato l-1 do recibo: banco fora")
	})
}
