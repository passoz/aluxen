package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Onboarding por documento (funcionalidades novas).
//
//	POST /api/intake/document — upload de contrato antigo/RG/CNH/comprovante → proposta de cadastro
//	POST /api/intake/confirm  — confirmação humana antes de gravar o cadastro
//
// O handler é fino: decodifica a requisição, chama o caso de uso em
// internal/application e devolve JSON. O upload só cria a proposta `pending`; a
// leitura roda na fila e o cadastro só nasce na confirmação.

const (
	// maxIntakeUploadBytes é o teto do corpo multipart do upload de documento.
	maxIntakeUploadBytes = 24 << 20
	// maxIntakeConfirmBytes é o teto do corpo JSON da conferência.
	maxIntakeConfirmBytes = 256 << 10
	// intakeConfirmActionDiscard descarta a proposta em vez de criar o cadastro
	// — a tela de conferência decide as duas coisas pela mesma rota.
	intakeConfirmActionDiscard = "discard"
)

// intakeTabContext é o contexto da aba de onboarding: as propostas aguardando
// conferência, já formatadas para a tela.
type intakeTabContext struct {
	Intakes []application.IntakeView
}

// intakeTabData carrega o contexto da aba de conferência (a aba carrega o seu
// próprio contexto; o painel não paga I/O de tela que ninguém abriu).
func (h *Handlers) intakeTabData(ctx context.Context) (any, error) {
	intakes, err := application.PendingDocumentIntakes(ctx, h.Deps, 0)
	if err != nil {
		return nil, err
	}
	return &intakeTabContext{Intakes: intakes}, nil
}

// DocumentIntake recebe o documento do onboarding (multipart: `file` e `kind`)
// e devolve a proposta criada com o protocolo da leitura.
func (h *Handlers) DocumentIntake(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxIntakeUploadBytes)
	if err := r.ParseMultipartForm(maxIntakeUploadBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "O arquivo pode ter no máximo 20 MB."})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Não foi possível ler o arquivo enviado."})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Envie o arquivo do documento."})
		return
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(io.LimitReader(file, maxIntakeUploadBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Não foi possível ler o arquivo enviado."})
		return
	}
	mime := strings.TrimSpace(header.Header.Get("Content-Type"))
	if mime == "" {
		mime = http.DetectContentType(content)
	}

	intake, job, err := application.SubmitDocumentIntakeJob(r.Context(), h.Deps, h.auditContext(r, domain.AuditOriginWeb), application.IntakeInput{
		Kind:     domain.IntakeKind(strings.TrimSpace(r.FormValue("kind"))),
		FileName: header.Filename,
		Mime:     mime,
		Bytes:    content,
	})
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "intakeId": intake.ID, "jobId": job.ID})
}

// DocumentIntakeConfirm aplica a conferência humana: cria o que foi aprovado
// (locatário, imóvel e contrato) ou descarta a proposta com o motivo.
func (h *Handlers) DocumentIntakeConfirm(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	payload, err := h.intakePayload(r)
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	actx := h.auditContext(r, domain.AuditOriginWeb)

	if payload.Action == intakeConfirmActionDiscard {
		if err := application.DiscardDocumentIntake(r.Context(), h.Deps, actx, payload.IntakeID, payload.Reason); err != nil {
			h.mapError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"intakeId": payload.IntakeID,
			"status":   string(domain.IntakeStatusDiscarded),
		})
		return
	}

	input, err := payload.confirmInput()
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	result, err := application.ConfirmDocumentIntake(r.Context(), h.Deps, actx, input)
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"intakeId":   result.Intake.ID,
		"tenantId":   result.TenantID,
		"propertyId": result.PropertyID,
		"leaseId":    result.LeaseID,
	})
}

// intakePayload é a conferência como ela chega. O formulário do painel manda
// urlencoded e a API pode mandar JSON; os nomes dos campos são os mesmos que a
// tela renderiza a partir de `application.IntakeField.Name`.
type intakePayload struct {
	IntakeID string `json:"intakeId"`
	Kind     string `json:"kind"`
	Action   string `json:"action"`
	Reason   string `json:"reason"`

	TenantID       string     `json:"tenantId"`
	PropertyID     string     `json:"propertyId"`
	CreateTenant   intakeFlag `json:"createTenant"`
	CreateProperty intakeFlag `json:"createProperty"`
	CreateLease    intakeFlag `json:"createLease"`

	TenantName  string `json:"tenant_name"`
	TenantCPF   string `json:"tenant_cpf"`
	TenantEmail string `json:"tenant_email"`
	TenantPhone string `json:"tenant_phone"`

	PropertyName  string `json:"property_name"`
	PropertyCity  string `json:"property_city"`
	PropertyState string `json:"property_state"`

	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	DueDay      string `json:"due_day"`
	RentAmount  string `json:"rent_amount"`
	AdjustIndex string `json:"adjust_index"`
	Guarantee   string `json:"guarantee"`

	Name    string `json:"name"`
	CPF     string `json:"cpf"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Address string `json:"address"`
	City    string `json:"city"`
	State   string `json:"state"`
	Zip     string `json:"zip"`
}

// intakeFlag aceita o que o formulário manda (`1`, `on`, `true`) e o que a API
// manda (booleano de verdade).
type intakeFlag bool

// UnmarshalJSON lê o sinalizador como booleano ou como texto do formulário.
func (f *intakeFlag) UnmarshalJSON(data []byte) error {
	*f = intakeFlag(intakeBool(strings.Trim(strings.TrimSpace(string(data)), `"`)))
	return nil
}

// intakePayload lê a conferência do corpo JSON ou do formulário do painel.
func (h *Handlers) intakePayload(r *http.Request) (intakePayload, error) {
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		var payload intakePayload
		if err := json.NewDecoder(io.LimitReader(r.Body, maxIntakeConfirmBytes)).Decode(&payload); err != nil {
			return intakePayload{}, domain.Invalid("Não foi possível ler os dados enviados.")
		}
		return payload, nil
	}
	if err := r.ParseForm(); err != nil {
		return intakePayload{}, domain.Invalid("Não foi possível ler os dados enviados.")
	}
	return intakePayload{
		IntakeID:       r.FormValue("intakeId"),
		Kind:           r.FormValue("kind"),
		Action:         r.FormValue("action"),
		Reason:         r.FormValue("reason"),
		TenantID:       r.FormValue("tenantId"),
		PropertyID:     r.FormValue("propertyId"),
		CreateTenant:   intakeFlag(intakeBool(r.FormValue("createTenant"))),
		CreateProperty: intakeFlag(intakeBool(r.FormValue("createProperty"))),
		CreateLease:    intakeFlag(intakeBool(r.FormValue("createLease"))),
		TenantName:     r.FormValue("tenant_name"),
		TenantCPF:      r.FormValue("tenant_cpf"),
		TenantEmail:    r.FormValue("tenant_email"),
		TenantPhone:    r.FormValue("tenant_phone"),
		PropertyName:   r.FormValue("property_name"),
		PropertyCity:   r.FormValue("property_city"),
		PropertyState:  r.FormValue("property_state"),
		StartDate:      r.FormValue("start_date"),
		EndDate:        r.FormValue("end_date"),
		DueDay:         r.FormValue("due_day"),
		RentAmount:     r.FormValue("rent_amount"),
		AdjustIndex:    r.FormValue("adjust_index"),
		Guarantee:      r.FormValue("guarantee"),
		Name:           r.FormValue("name"),
		CPF:            r.FormValue("cpf"),
		Email:          r.FormValue("email"),
		Phone:          r.FormValue("phone"),
		Address:        r.FormValue("address"),
		City:           r.FormValue("city"),
		State:          r.FormValue("state"),
		Zip:            r.FormValue("zip"),
	}, nil
}

// confirmInput traduz a conferência para o caso de uso.
func (p intakePayload) confirmInput() (application.ConfirmInput, error) {
	dueDay, err := intakeDueDay(p.DueDay)
	if err != nil {
		return application.ConfirmInput{}, err
	}
	rentAmount, err := intakeMoney(p.RentAmount)
	if err != nil {
		return application.ConfirmInput{}, err
	}
	return application.ConfirmInput{
		IntakeID: p.IntakeID,
		Kind:     domain.IntakeKind(strings.TrimSpace(p.Kind)),
		Lease: domain.LeaseIntakeFields{
			TenantName:    p.TenantName,
			TenantCPF:     p.TenantCPF,
			TenantEmail:   p.TenantEmail,
			TenantPhone:   p.TenantPhone,
			PropertyName:  p.PropertyName,
			PropertyCity:  p.PropertyCity,
			PropertyState: p.PropertyState,
			StartDate:     p.StartDate,
			EndDate:       p.EndDate,
			DueDay:        dueDay,
			RentAmount:    rentAmount,
			AdjustIndex:   p.AdjustIndex,
			Guarantee:     p.Guarantee,
		},
		Person: domain.PersonIntakeFields{
			Name:    p.Name,
			CPF:     p.CPF,
			Email:   p.Email,
			Phone:   p.Phone,
			Address: p.Address,
			City:    p.City,
			State:   p.State,
			Zip:     p.Zip,
		},
		CreateTenant:   bool(p.CreateTenant),
		CreateProperty: bool(p.CreateProperty),
		CreateLease:    bool(p.CreateLease),
		TenantID:       p.TenantID,
		PropertyID:     p.PropertyID,
	}, nil
}

// intakeBool interpreta o sinalizador do formulário e da API.
func intakeBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on", "sim", "yes":
		return true
	default:
		return false
	}
}

// intakeMoney interpreta o valor do aluguel; vazio é zero (o contrato só é
// criado quando a conferência pede, e aí o valor é validado pelo domínio).
func intakeMoney(raw string) (float64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	return parseMoneyField(raw)
}

// intakeDueDay interpreta o dia de vencimento; vazio é zero.
func intakeDueDay(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}
	dueDay, err := strconv.Atoi(value)
	if err != nil {
		return 0, domain.Invalid("Informe um dia de vencimento válido.")
	}
	return dueDay, nil
}
