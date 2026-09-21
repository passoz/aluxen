package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Onboarding por documento: contrato antigo, RG/CNH e comprovante de residência
// viram uma proposta de cadastro. A leitura assistida só preenche a proposta —
// nenhum locatário, imóvel ou contrato é gravado antes da conferência humana.
//
// O caminho é sempre o mesmo: o arquivo é guardado e a proposta nasce `pending`
// (`SubmitDocumentIntake`); a leitura roda fora da requisição, na fila de
// trabalho (`ProcessDocumentIntake`); a tela de conferência mostra o que foi
// lido já formatado (`LoadDocumentIntake`) e é a confirmação humana que cria o
// cadastro pelos casos de uso existentes (`ConfirmDocumentIntake`).

const (
	// maxIntakeBytes é o teto do arquivo aceito no onboarding.
	maxIntakeBytes = 20 << 20
	// defaultIntakeList é quantas propostas a tela de conferência carrega.
	defaultIntakeList = 50

	// intakeNoteNoProvider e intakeNoteNoFile explicam, na própria proposta, por
	// que ela chegou à conferência sem leitura — o humano preenche à mão.
	intakeNoteNoProvider = "Leitura assistida não configurada nesta instância: preencha os campos na conferência."
	intakeNoteNoFile     = "Arquivo do documento não encontrado no armazenamento: preencha os campos na conferência."
)

// Nomes dos campos do formulário de conferência. O handler lê exatamente estes
// nomes e a tela os renderiza a partir de `IntakeField.Name`.
const (
	intakeFieldTenantName    = "tenant_name"
	intakeFieldTenantCPF     = "tenant_cpf"
	intakeFieldTenantEmail   = "tenant_email"
	intakeFieldTenantPhone   = "tenant_phone"
	intakeFieldPropertyName  = "property_name"
	intakeFieldPropertyCity  = "property_city"
	intakeFieldPropertyState = "property_state"
	intakeFieldStartDate     = "start_date"
	intakeFieldEndDate       = "end_date"
	intakeFieldDueDay        = "due_day"
	intakeFieldRentAmount    = "rent_amount"
	intakeFieldAdjustIndex   = "adjust_index"
	intakeFieldGuarantee     = "guarantee"

	intakeFieldName    = "name"
	intakeFieldCPF     = "cpf"
	intakeFieldEmail   = "email"
	intakeFieldPhone   = "phone"
	intakeFieldAddress = "address"
	intakeFieldCity    = "city"
	intakeFieldState   = "state"
	intakeFieldZip     = "zip"
)

// intakeFileExtensions são as extensões aceitas quando o navegador não declara
// o tipo do arquivo.
var intakeFileExtensions = []string{".pdf", ".jpg", ".jpeg", ".png", ".webp", ".heic", ".heif"}

// intakeMimeTypes são os formatos que a leitura assistida aceita.
var intakeMimeTypes = map[string]bool{
	"application/pdf": true,
	"image/jpeg":      true,
	"image/png":       true,
	"image/webp":      true,
	"image/heic":      true,
	"image/heif":      true,
}

// IntakeInput é o documento enviado para leitura no onboarding.
type IntakeInput struct {
	Kind     domain.IntakeKind
	FileName string
	Mime     string
	Bytes    []byte
}

// ConfirmInput são os campos conferidos na tela de conferência. Os campos de
// locatário/imóvel vêm preenchidos com o que a leitura devolveu, já corrigidos
// por quem conferiu; `TenantID`/`PropertyID` reusam cadastro existente.
type ConfirmInput struct {
	IntakeID string
	Kind     domain.IntakeKind
	Lease    domain.LeaseIntakeFields
	Person   domain.PersonIntakeFields
	// CreateTenant, CreateProperty e CreateLease dizem o que a conferência
	// aprovou criar a partir do documento.
	CreateTenant   bool
	CreateProperty bool
	CreateLease    bool
	TenantID       string
	PropertyID     string
}

// IntakeResult é o que a confirmação criou (ids vazios = nada daquele tipo).
type IntakeResult struct {
	Intake     *domain.DocumentIntake
	TenantID   string
	PropertyID string
	LeaseID    string
}

// IntakeField é um campo do formulário de conferência, já formatado para a tela
// (datas em dd/mm/aaaa, valor em reais). `Missing` marca o campo vazio que
// precisa de atenção antes de confirmar.
type IntakeField struct {
	Name  string
	Label string
	Value string
	// Mask é a máscara do painel aplicada ao campo (`cpf`, `phone`, `cep`,
	// `money`); Type é o tipo do input (`email`, `tel`, `number`).
	Mask    string
	Type    string
	Date    bool
	Missing bool
}

// IntakeView é a proposta pronta para a tela de conferência.
type IntakeView struct {
	Intake      *domain.DocumentIntake
	Kind        domain.IntakeKind
	KindLabel   string
	StatusLabel string
	// StatusTone é a classe do selo de situação no painel.
	StatusTone string
	FileName   string
	// ConfidenceLabel é a confiança da leitura em porcentagem ("92%"), "-" sem leitura.
	ConfidenceLabel string
	// LowConfidence avisa que a leitura ficou abaixo do limiar configurado.
	LowConfidence bool
	Lease         []IntakeField
	Person        []IntakeField
	// Missing reúne os rótulos dos campos vazios que importam para o cadastro.
	Missing         []string
	CreatedEntity   string
	CreatedEntityID string
	Notes           string
}

// intakeJobPayload é o que a fila carrega para ler um documento do onboarding.
type intakeJobPayload struct {
	IntakeID string `json:"intakeId"`
}

// SubmitDocumentIntake guarda o documento e cria a proposta `pending`,
// enfileirando a leitura. `actx` identifica quem enviou (fica no log do envio: a
// trilha de auditoria registra a criação do cadastro, não o upload).
func SubmitDocumentIntake(ctx context.Context, deps Deps, actx AuditContext, input IntakeInput) (*domain.DocumentIntake, error) {
	intake, _, err := submitDocumentIntake(ctx, deps, actx, input)
	return intake, err
}

// SubmitDocumentIntakeJob é o `SubmitDocumentIntake` devolvendo também o
// trabalho enfileirado — o painel mostra o protocolo da leitura.
func SubmitDocumentIntakeJob(ctx context.Context, deps Deps, actx AuditContext, input IntakeInput) (*domain.DocumentIntake, *domain.Job, error) {
	return submitDocumentIntake(ctx, deps, actx, input)
}

func submitDocumentIntake(ctx context.Context, deps Deps, actx AuditContext, input IntakeInput) (*domain.DocumentIntake, *domain.Job, error) {
	kind := domain.IntakeKind(strings.TrimSpace(string(input.Kind)))
	if !kind.Valid() {
		return nil, nil, domain.Invalid("Tipo de documento não aceito.")
	}
	fileName := strings.TrimSpace(input.FileName)
	if len(input.Bytes) == 0 {
		return nil, nil, domain.Invalid("Envie o arquivo do documento para a leitura.")
	}
	if len(input.Bytes) > maxIntakeBytes {
		return nil, nil, domain.Invalid("O arquivo pode ter no máximo 20 MB.")
	}
	if !intakeFileSupported(input.Mime, fileName) {
		return nil, nil, domain.Invalid("Formato de arquivo não suportado. Envie PDF, JPG, PNG ou WEBP.")
	}
	if deps.Attachments == nil {
		return nil, nil, domain.Invalid("Armazenamento de arquivos indisponível nesta instância.")
	}
	if fileName == "" {
		fileName = "documento"
	}

	stored, err := deps.Attachments.Save("intakes", fileName, input.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("guardar documento do cadastro: %w", err)
	}

	stamp := format.FormatUTCStamp(deps.Clock.Now())
	intake := &domain.DocumentIntake{
		ID:        deps.IDs.New(),
		CreatedAt: stamp,
		UpdatedAt: stamp,
		Kind:      kind,
		FileName:  domain.StrPtr(fileName),
		FilePath:  domain.StrPtr(stored),
		Mime:      optionalString(strings.TrimSpace(input.Mime)),
		Status:    domain.IntakeStatusPending,
	}
	if err := deps.Repos.Intakes.Create(ctx, intake); err != nil {
		return nil, nil, fmt.Errorf("criar proposta de cadastro: %w", err)
	}

	job, err := Enqueue(ctx, deps, domain.JobKindDocumentIntake, intakeJobPayload{IntakeID: intake.ID}, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("enfileirar leitura do documento: %w", err)
	}

	deps.Log.Info("proposta de cadastro criada",
		"scope", "intake", "intake_id", intake.ID, "kind", string(kind),
		"status", string(intake.Status), "job_id", job.ID, "actor_id", domain.Str(actx.ActorID))
	return intake, job, nil
}

// ProcessDocumentIntake lê o documento da proposta e grava o que foi lido, sem
// criar cadastro nenhum. É chamado pelo worker da fila.
//
// Proposta já conferida é devolvida intacta (a releitura não faz sentido) e
// falha de leitura vira erro para o worker decidir a retentativa — a proposta
// continua `pending` e pode ser preenchida à mão na conferência.
func ProcessDocumentIntake(ctx context.Context, deps Deps, intakeID string) (*domain.DocumentIntake, error) {
	intake, err := intakeByID(ctx, deps, intakeID)
	if err != nil {
		return nil, err
	}
	if intake.Status != domain.IntakeStatusPending {
		return intake, nil
	}
	if deps.Intakes == nil {
		return intake, noteIntake(ctx, deps, intake, intakeNoteNoProvider)
	}

	content, err := readIntakeFile(deps, intake)
	if err != nil {
		return intake, noteIntake(ctx, deps, intake, intakeNoteNoFile)
	}

	extracted, raw, confidence, err := extractIntakeDocument(ctx, deps.Intakes, intake.Kind, DocumentFile{
		Bytes:    content,
		Mime:     domain.Str(intake.Mime),
		FileName: domain.Str(intake.FileName),
	})
	if err != nil {
		return nil, fmt.Errorf("ler documento do cadastro: %w", err)
	}

	encoded, err := json.Marshal(extracted)
	if err != nil {
		return nil, fmt.Errorf("serializar leitura do documento: %w", err)
	}
	text := string(encoded)
	intake.Extracted = &text
	intake.Confidence = &confidence
	intake.RawResponse = optionalString(raw)
	intake.UpdatedAt = format.FormatUTCStamp(deps.Clock.Now())
	if err := deps.Repos.Intakes.Update(ctx, intake); err != nil {
		return nil, fmt.Errorf("gravar leitura do documento: %w", err)
	}

	// Dado pessoal: o log registra apenas id, tipo, estado e confiança.
	deps.Log.Info("documento do cadastro lido",
		"scope", "intake", "intake_id", intake.ID, "kind", string(intake.Kind),
		"status", string(intake.Status), "confidence", confidence)
	return intake, nil
}

// ProcessDocumentIntakeJob é o handler da fila para `document-intake`: lê o id
// da proposta do payload e delega ao caso de uso.
func ProcessDocumentIntakeJob(ctx context.Context, deps Deps, job domain.Job) error {
	var payload intakeJobPayload
	if job.Payload == nil || strings.TrimSpace(*job.Payload) == "" {
		return domain.Invalid("Trabalho de leitura sem documento informado.")
	}
	if err := json.Unmarshal([]byte(*job.Payload), &payload); err != nil {
		return domain.Invalid("Trabalho de leitura com payload ilegível.")
	}
	_, err := ProcessDocumentIntake(ctx, deps, payload.IntakeID)
	return err
}

// LoadDocumentIntake carrega a proposta com o que foi lido já formatado para a
// tela de conferência.
func LoadDocumentIntake(ctx context.Context, deps Deps, id string) (*IntakeView, error) {
	intake, err := intakeByID(ctx, deps, id)
	if err != nil {
		return nil, err
	}
	return buildIntakeView(deps, intake), nil
}

// PendingDocumentIntakes lista as propostas aguardando conferência, prontas
// para a tela. Limite não positivo usa o padrão da tela.
func PendingDocumentIntakes(ctx context.Context, deps Deps, limit int) ([]IntakeView, error) {
	if limit <= 0 {
		limit = defaultIntakeList
	}
	intakes, err := deps.Repos.Intakes.ListByStatus(ctx, domain.IntakeStatusPending, limit)
	if err != nil {
		return nil, fmt.Errorf("listar propostas de cadastro: %w", err)
	}
	views := make([]IntakeView, 0, len(intakes))
	for index := range intakes {
		views = append(views, *buildIntakeView(deps, &intakes[index]))
	}
	return views, nil
}

// ConfirmDocumentIntake grava o cadastro aprovado na conferência — locatário e
// imóvel pelos casos de uso existentes e o contrato por `LeaseCreate` — e marca
// a proposta como confirmada, deixando a trilha com o arquivo usado.
//
// Nada é criado sem que a conferência peça, e contrato exige locatário e imóvel:
// é a garantia de que o documento sozinho não cria cadastro.
func ConfirmDocumentIntake(ctx context.Context, deps Deps, actx AuditContext, input ConfirmInput) (*IntakeResult, error) {
	intake, err := intakeByID(ctx, deps, input.IntakeID)
	if err != nil {
		return nil, err
	}
	if intake.Status != domain.IntakeStatusPending {
		return nil, domain.Invalid("Esta proposta já foi conferida.")
	}

	kind := input.Kind
	if !kind.Valid() {
		kind = intake.Kind
	}
	if !kind.Valid() {
		return nil, domain.Invalid("Tipo de documento não aceito.")
	}

	tenantID := strings.TrimSpace(input.TenantID)
	propertyID := strings.TrimSpace(input.PropertyID)
	if !input.CreateTenant && !input.CreateProperty && !input.CreateLease && tenantID == "" && propertyID == "" {
		return nil, domain.Invalid("Escolha o que criar a partir do documento.")
	}
	result := &IntakeResult{Intake: intake}

	created := make([]string, 0, 3)
	reused := make([]string, 0, 2)

	if input.CreateTenant && tenantID == "" {
		tenant, err := TenantCreate(ctx, deps, actx, TenantInput{
			Name:  intakeTenantName(kind, input),
			Email: intakeTenantEmail(kind, input),
			Phone: intakeTenantPhone(kind, input),
			CPF:   intakeTenantCPF(kind, input),
		})
		if err != nil {
			return nil, err
		}
		tenantID = tenant.ID
		result.TenantID = tenantID
		created = append(created, "locatário")
	} else if tenantID != "" {
		if _, err := deps.Repos.Tenants.ByID(ctx, tenantID); err != nil {
			return nil, domain.Invalid("Locatário informado não encontrado.")
		}
		result.TenantID = tenantID
		reused = append(reused, "locatário")
	}

	if input.CreateProperty && propertyID == "" {
		fields, err := intakePropertyFields(kind, input)
		if err != nil {
			return nil, err
		}
		property, err := PropertyCreate(ctx, deps, actx, fields)
		if err != nil {
			return nil, err
		}
		propertyID = property.ID
		result.PropertyID = propertyID
		created = append(created, "imóvel")
	} else if propertyID != "" {
		if _, err := deps.Repos.Properties.ByID(ctx, propertyID); err != nil {
			return nil, domain.Invalid("Imóvel informado não encontrado.")
		}
		result.PropertyID = propertyID
		reused = append(reused, "imóvel")
	}

	if input.CreateLease {
		if result.TenantID == "" || result.PropertyID == "" {
			return nil, domain.Invalid("Para criar o contrato, informe o locatário e o imóvel.")
		}
		lease, err := LeaseCreate(ctx, deps, actx, LeaseInput{
			TenantID:   result.TenantID,
			PropertyID: result.PropertyID,
			StartDate:  intakeDateISO(input.Lease.StartDate),
			EndDate:    intakeDateISO(input.Lease.EndDate),
			DueDay:     input.Lease.DueDay,
			RentAmount: input.Lease.RentAmount,
		})
		if err != nil {
			return nil, err
		}
		result.LeaseID = lease.ID
		created = append(created, "contrato")
	}

	// `created_entity` guarda o cadastro de maior nível criado (contrato >
	// imóvel > locatário) e `created_entity_id` o id dele; a trilha registra os
	// três ids.
	entity, entityID := intakeCreatedEntity(result)
	intake.Status = domain.IntakeStatusConfirmed
	intake.CreatedEntity = optionalString(entity)
	intake.CreatedEntityID = optionalString(entityID)
	intake.UpdatedAt = format.FormatUTCStamp(deps.Clock.Now())
	if err := deps.Repos.Intakes.Update(ctx, intake); err != nil {
		return nil, fmt.Errorf("confirmar proposta de cadastro: %w", err)
	}

	summary := fmt.Sprintf("Cadastro criado por documento (%s): %s", domain.Str(intake.FileName), intakeCreatedLabel(created, reused))
	if err := Record(ctx, deps, actx, AuditEntry{
		Action:      domain.ActionDocumentIntakeCreated,
		Entity:      domain.EntityIntake,
		EntityID:    intake.ID,
		EntityLabel: intakeLabel(intake),
		Summary:     summary,
		Metadata: map[string]any{
			"kind":       string(kind),
			"fileName":   domain.Str(intake.FileName),
			"confidence": domain.Float(intake.Confidence),
			"tenantId":   result.TenantID,
			"propertyId": result.PropertyID,
			"leaseId":    result.LeaseID,
		},
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// DiscardDocumentIntake descarta a proposta com o motivo informado: o documento
// não vira cadastro nenhum.
func DiscardDocumentIntake(ctx context.Context, deps Deps, actx AuditContext, id, reason string) error {
	intake, err := intakeByID(ctx, deps, id)
	if err != nil {
		return err
	}
	if intake.Status != domain.IntakeStatusPending {
		return domain.Invalid("Esta proposta já foi conferida.")
	}
	note := strings.TrimSpace(reason)
	if note == "" {
		return domain.Invalid("Informe o motivo do descarte.")
	}

	intake.Status = domain.IntakeStatusDiscarded
	intake.Notes = optionalString(note)
	intake.UpdatedAt = format.FormatUTCStamp(deps.Clock.Now())
	if err := deps.Repos.Intakes.Update(ctx, intake); err != nil {
		return fmt.Errorf("descartar proposta de cadastro: %w", err)
	}
	deps.Log.Info("proposta de cadastro descartada",
		"scope", "intake", "intake_id", intake.ID, "kind", string(intake.Kind),
		"status", string(intake.Status), "actor_id", domain.Str(actx.ActorID))
	return nil
}

// ── Auxiliares ─────────────────────────────────────────────────────

// intakeByID carrega a proposta, traduzindo ausência em erro de validação.
func intakeByID(ctx context.Context, deps Deps, id string) (*domain.DocumentIntake, error) {
	intakeID := strings.TrimSpace(id)
	if intakeID == "" {
		return nil, domain.Invalid("Documento não informado.")
	}
	intake, err := deps.Repos.Intakes.ByID(ctx, intakeID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.Invalid("Proposta de cadastro não encontrada.")
		}
		return nil, fmt.Errorf("buscar proposta de cadastro: %w", err)
	}
	return intake, nil
}

// readIntakeFile devolve o arquivo guardado no envio.
func readIntakeFile(deps Deps, intake *domain.DocumentIntake) ([]byte, error) {
	path := strings.TrimSpace(domain.Str(intake.FilePath))
	if path == "" || deps.Attachments == nil {
		return nil, errors.New("arquivo do documento ausente")
	}
	return deps.Attachments.Open(path)
}

// extractIntakeDocument chama a leitura assistida conforme o tipo do documento.
func extractIntakeDocument(ctx context.Context, extractor IntakeExtractor, kind domain.IntakeKind, doc DocumentFile) (any, string, float64, error) {
	if kind == domain.IntakeKindLease {
		fields, raw, err := extractor.ExtractLeaseDocument(ctx, doc)
		return fields, raw, fields.Confidence, err
	}
	fields, raw, err := extractor.ExtractPersonDocument(ctx, doc)
	return fields, raw, fields.Confidence, err
}

// noteIntake registra na proposta o motivo de ela chegar à conferência sem
// leitura e devolve a proposta atualizada (a conferência manual continua
// possível; o trabalho da fila terminou bem).
func noteIntake(ctx context.Context, deps Deps, intake *domain.DocumentIntake, note string) error {
	intake.Notes = domain.StrPtr(note)
	intake.UpdatedAt = format.FormatUTCStamp(deps.Clock.Now())
	if err := deps.Repos.Intakes.Update(ctx, intake); err != nil {
		return fmt.Errorf("anotar proposta de cadastro: %w", err)
	}
	deps.Log.Info("proposta de cadastro sem leitura assistida",
		"scope", "intake", "intake_id", intake.ID, "kind", string(intake.Kind),
		"status", string(intake.Status))
	return nil
}

// buildIntakeView monta a tela de conferência. Sem leitura (ou com leitura
// ilegível) o formulário sai vazio: a conferência pode ser preenchida à mão.
func buildIntakeView(deps Deps, intake *domain.DocumentIntake) *IntakeView {
	view := &IntakeView{
		Intake:          intake,
		Kind:            intake.Kind,
		KindLabel:       domain.IntakeKindLabel(intake.Kind),
		StatusLabel:     domain.IntakeStatusLabel(intake.Status),
		StatusTone:      intakeStatusTone(intake.Status),
		FileName:        domain.Str(intake.FileName),
		ConfidenceLabel: intakeConfidenceLabel(intake.Confidence),
		CreatedEntity:   domain.Str(intake.CreatedEntity),
		CreatedEntityID: domain.Str(intake.CreatedEntityID),
		Notes:           domain.Str(intake.Notes),
	}
	if intake.Confidence != nil && deps.Opts.AIConfidenceThreshold > 0 {
		view.LowConfidence = *intake.Confidence < deps.Opts.AIConfidenceThreshold
	}

	var lease domain.LeaseIntakeFields
	var person domain.PersonIntakeFields
	if intake.Extracted != nil {
		if intake.Kind == domain.IntakeKindLease {
			_ = json.Unmarshal([]byte(*intake.Extracted), &lease)
		} else {
			_ = json.Unmarshal([]byte(*intake.Extracted), &person)
		}
	}
	if intake.Kind == domain.IntakeKindLease {
		view.Lease = intakeLeaseFields(lease)
		view.Missing = intakeMissingLabels(view.Lease)
		return view
	}
	view.Person = intakePersonFields(person)
	view.Missing = intakeMissingLabels(view.Person)
	return view
}

// intakeLeaseFields formata os campos do contrato para o formulário.
func intakeLeaseFields(fields domain.LeaseIntakeFields) []IntakeField {
	return []IntakeField{
		{Name: intakeFieldTenantName, Label: "Nome do locatário", Value: strings.TrimSpace(fields.TenantName), Missing: strings.TrimSpace(fields.TenantName) == ""},
		{Name: intakeFieldTenantCPF, Label: "CPF do locatário", Value: strings.TrimSpace(fields.TenantCPF), Mask: "cpf", Missing: strings.TrimSpace(fields.TenantCPF) == ""},
		{Name: intakeFieldTenantEmail, Label: "E-mail do locatário", Value: strings.TrimSpace(fields.TenantEmail), Type: "email"},
		{Name: intakeFieldTenantPhone, Label: "Telefone do locatário", Value: strings.TrimSpace(fields.TenantPhone), Mask: "phone", Type: "tel"},
		{Name: intakeFieldPropertyName, Label: "Imóvel", Value: strings.TrimSpace(fields.PropertyName), Missing: strings.TrimSpace(fields.PropertyName) == ""},
		{Name: intakeFieldPropertyCity, Label: "Cidade", Value: strings.TrimSpace(fields.PropertyCity), Missing: strings.TrimSpace(fields.PropertyCity) == ""},
		{Name: intakeFieldPropertyState, Label: "UF", Value: strings.TrimSpace(fields.PropertyState), Missing: strings.TrimSpace(fields.PropertyState) == ""},
		{Name: intakeFieldStartDate, Label: "Início", Value: intakeDateDisplay(fields.StartDate), Date: true, Missing: strings.TrimSpace(fields.StartDate) == ""},
		{Name: intakeFieldEndDate, Label: "Fim", Value: intakeDateDisplay(fields.EndDate), Date: true, Missing: strings.TrimSpace(fields.EndDate) == ""},
		{Name: intakeFieldDueDay, Label: "Dia de vencimento", Value: intakeDueDayDisplay(fields.DueDay), Type: "number", Missing: fields.DueDay <= 0},
		{Name: intakeFieldRentAmount, Label: "Aluguel", Value: format.FormatCurrency(fields.RentAmount), Mask: "money", Missing: fields.RentAmount <= 0},
		{Name: intakeFieldAdjustIndex, Label: "Índice de reajuste", Value: strings.TrimSpace(fields.AdjustIndex)},
		{Name: intakeFieldGuarantee, Label: "Garantia", Value: strings.TrimSpace(fields.Guarantee)},
	}
}

// intakePersonFields formata os campos de pessoa para o formulário.
func intakePersonFields(fields domain.PersonIntakeFields) []IntakeField {
	return []IntakeField{
		{Name: intakeFieldName, Label: "Nome", Value: strings.TrimSpace(fields.Name), Missing: strings.TrimSpace(fields.Name) == ""},
		{Name: intakeFieldCPF, Label: "CPF", Value: strings.TrimSpace(fields.CPF), Mask: "cpf", Missing: strings.TrimSpace(fields.CPF) == ""},
		{Name: intakeFieldEmail, Label: "E-mail", Value: strings.TrimSpace(fields.Email), Type: "email"},
		{Name: intakeFieldPhone, Label: "Telefone", Value: strings.TrimSpace(fields.Phone), Mask: "phone", Type: "tel"},
		{Name: intakeFieldAddress, Label: "Endereço", Value: strings.TrimSpace(fields.Address)},
		{Name: intakeFieldCity, Label: "Cidade", Value: strings.TrimSpace(fields.City)},
		{Name: intakeFieldState, Label: "UF", Value: strings.TrimSpace(fields.State)},
		{Name: intakeFieldZip, Label: "CEP", Value: strings.TrimSpace(fields.Zip), Mask: "cep"},
	}
}

// intakeMissingLabels reúne os rótulos dos campos que a conferência precisa
// completar antes de confirmar.
func intakeMissingLabels(fields []IntakeField) []string {
	missing := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.Missing {
			missing = append(missing, field.Label)
		}
	}
	return missing
}

// intakeStatusTone escolhe o selo de situação usado no painel.
func intakeStatusTone(status domain.IntakeStatus) string {
	switch status {
	case domain.IntakeStatusConfirmed:
		return "paid"
	case domain.IntakeStatusDiscarded:
		return "terminated"
	default:
		return "due"
	}
}

// intakeConfidenceLabel mostra a confiança da leitura em porcentagem.
func intakeConfidenceLabel(confidence *float64) string {
	if confidence == nil {
		return "-"
	}
	return fmt.Sprintf("%d%%", int(*confidence*100+0.5))
}

// intakeDateDisplay mostra a data do banco como dd/mm/aaaa (campo vazio fica
// vazio para o formulário de conferência).
func intakeDateDisplay(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return format.FormatDateBR(trimmed)
}

// intakeDueDayDisplay mostra o dia de vencimento; zero é "sem vencimento".
func intakeDueDayDisplay(dueDay int) string {
	if dueDay <= 0 {
		return ""
	}
	return strconv.Itoa(dueDay)
}

// intakeDateISO normaliza a data da conferência para o formato do banco. A tela
// mostra dd/mm/aaaa; o formato do banco (ISO) também é aceito.
func intakeDateISO(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || !strings.Contains(trimmed, "/") {
		return trimmed
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) != 3 {
		return trimmed
	}
	day, dayErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	month, monthErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	year, yearErr := strconv.Atoi(strings.TrimSpace(parts[2]))
	if dayErr != nil || monthErr != nil || yearErr != nil || year < 1000 {
		return trimmed
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

// intakeFileSupported aceita o que o celular envia: tipo declarado conhecido ou
// extensão conhecida quando o navegador não declara o tipo.
func intakeFileSupported(mime, fileName string) bool {
	contentType := strings.ToLower(strings.TrimSpace(mime))
	if index := strings.Index(contentType, ";"); index >= 0 {
		contentType = strings.TrimSpace(contentType[:index])
	}
	if contentType != "" && contentType != "application/octet-stream" {
		return intakeMimeTypes[contentType]
	}
	lower := strings.ToLower(strings.TrimSpace(fileName))
	for _, extension := range intakeFileExtensions {
		if strings.HasSuffix(lower, extension) {
			return true
		}
	}
	return false
}

// intakeTenantName devolve o nome do locatário conferido: contrato traz o
// titular; documento pessoal traz a pessoa.
func intakeTenantName(kind domain.IntakeKind, input ConfirmInput) string {
	if kind == domain.IntakeKindLease {
		return input.Lease.TenantName
	}
	return input.Person.Name
}

func intakeTenantEmail(kind domain.IntakeKind, input ConfirmInput) string {
	if kind == domain.IntakeKindLease {
		return input.Lease.TenantEmail
	}
	return input.Person.Email
}

func intakeTenantPhone(kind domain.IntakeKind, input ConfirmInput) string {
	if kind == domain.IntakeKindLease {
		return input.Lease.TenantPhone
	}
	return input.Person.Phone
}

func intakeTenantCPF(kind domain.IntakeKind, input ConfirmInput) string {
	if kind == domain.IntakeKindLease {
		return input.Lease.TenantCPF
	}
	return input.Person.CPF
}

// intakePropertyFields monta o imóvel a partir do que o documento descreve: o
// contrato traz o imóvel e a cidade; o comprovante de residência traz o
// endereço (que vira também o nome, editável depois na aba de imóveis).
func intakePropertyFields(kind domain.IntakeKind, input ConfirmInput) (PropertyInput, error) {
	switch kind {
	case domain.IntakeKindLease:
		return PropertyInput{
			Name:    input.Lease.PropertyName,
			Address: input.Lease.PropertyName,
			City:    input.Lease.PropertyCity,
			State:   input.Lease.PropertyState,
		}, nil
	case domain.IntakeKindAddressProof:
		return PropertyInput{
			Name:    input.Person.Address,
			Address: input.Person.Address,
			City:    input.Person.City,
			State:   input.Person.State,
			Zip:     input.Person.Zip,
		}, nil
	default:
		return PropertyInput{}, domain.Invalid("Documento de identidade não descreve imóvel.")
	}
}

// intakeCreatedEntity escolhe o cadastro de maior nível criado pela confirmação.
func intakeCreatedEntity(result *IntakeResult) (string, string) {
	switch {
	case result.LeaseID != "":
		return "lease", result.LeaseID
	case result.PropertyID != "":
		return "property", result.PropertyID
	case result.TenantID != "":
		return "tenant", result.TenantID
	default:
		return "", ""
	}
}

// intakeCreatedLabel descreve, em português, o que a confirmação criou e o que
// ela apenas reusou.
func intakeCreatedLabel(created, reused []string) string {
	switch {
	case len(created) == 0 && len(reused) == 0:
		return "nada criado"
	case len(created) == 0:
		return "cadastro existente reusado: " + strings.Join(reused, ", ")
	case len(reused) == 0:
		return strings.Join(created, ", ")
	default:
		return strings.Join(created, ", ") + " (reusado: " + strings.Join(reused, ", ") + ")"
	}
}

// intakeLabel monta o rótulo da proposta usado na trilha.
func intakeLabel(intake *domain.DocumentIntake) string {
	kindLabel := domain.IntakeKindLabel(intake.Kind)
	fileName := strings.TrimSpace(domain.Str(intake.FileName))
	if fileName == "" {
		return kindLabel
	}
	return fmt.Sprintf("%s · %s", kindLabel, fileName)
}
