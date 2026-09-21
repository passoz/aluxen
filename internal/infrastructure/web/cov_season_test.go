package web

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
)

type covUpload struct {
	field    string
	filename string
	mime     string
	content  []byte
}

// covMultipartPost monta e envia um POST multipart autenticado, com o token de
// CSRF no corpo (o painel manda o formulário inteiro em multipart).
func covMultipartPost(panel *covPanel, target string, fields map[string][]string, uploads []covUpload) *httptest.ResponseRecorder {
	panel.t.Helper()

	page := httptest.NewRecorder()
	panel.handler.Router().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/login", nil))
	token := cookieValue(page.Result().Cookies(), csrfCookieName)
	if token == "" {
		panel.t.Fatal("cookie de CSRF não emitido")
	}
	if fields == nil {
		fields = map[string][]string{}
	}
	fields[csrfFieldName] = []string{token}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for field, values := range fields {
		for _, value := range values {
			if err := writer.WriteField(field, value); err != nil {
				panel.t.Fatalf("campo %s: %v", field, err)
			}
		}
	}
	for _, upload := range uploads {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="`+upload.field+`"; filename="`+upload.filename+`"`)
		if upload.mime != "" {
			header.Set("Content-Type", upload.mime)
		}
		part, err := writer.CreatePart(header)
		if err != nil {
			panel.t.Fatalf("parte %s: %v", upload.filename, err)
		}
		if _, err := part.Write(upload.content); err != nil {
			panel.t.Fatalf("conteúdo %s: %v", upload.filename, err)
		}
	}
	if err := writer.Close(); err != nil {
		panel.t.Fatalf("multipart: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, target, body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(panel.sessionCookie(panel.admin.ID, true))
	for _, cookie := range page.Result().Cookies() {
		request.AddCookie(cookie)
	}
	return panel.do(request)
}

func covLeaseWithDeposit(id string, status domain.DepositStatus, amount float64) domain.Lease {
	return domain.Lease{
		ID:            id,
		TenantID:      "tenant-1",
		PropertyID:    "property-1",
		TenantName:    "Ana",
		PropertyName:  "Casa da praia",
		RentAmount:    2500,
		Status:        domain.LeaseStatusActive,
		DepositAmount: covFloat(amount),
		DepositStatus: status,
	}
}

func TestCovDepositMutationRequiresAdmin(t *testing.T) {
	panel := newCovPanel(t)

	recorder := covFormPost(panel, "/dashboard/deposit", url.Values{"intent": {"deposit.set"}}, "")
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/login" {
		t.Fatalf("status = %d location = %q, esperado 302 /login", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestCovDepositSetHoldsTheAmount(t *testing.T) {
	panel := newCovPanel(t)
	panel.leases.items = []domain.Lease{covLeaseWithDeposit("lease-1", "", 0)}

	recorder := covFormPost(panel, "/dashboard/deposit", url.Values{
		"intent":              {"deposit.set"},
		"lease_id":            {"lease-1"},
		"deposit_amount":      {"2.500,50"},
		"deposit_received_at": {"2026-09-01"},
		"deposit_notes":       {"recebida em dinheiro"},
	}, panel.admin.ID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	lease := panel.leases.items[0]
	if lease.DepositStatus != domain.DepositStatusHeld {
		t.Fatalf("estado da caução = %q, esperado held", lease.DepositStatus)
	}
	if lease.DepositAmount == nil || *lease.DepositAmount != 2500.5 {
		t.Fatalf("valor da caução = %v, esperado 2500,50 (máscara pt-BR)", lease.DepositAmount)
	}
	if lease.DepositReceivedAt == nil || *lease.DepositReceivedAt != "2026-09-01" {
		t.Fatalf("data de recebimento = %v", lease.DepositReceivedAt)
	}
}

func TestCovDepositSetRejectsUnknownLeaseAndBadAmount(t *testing.T) {
	cases := []struct {
		name    string
		form    url.Values
		message string
	}{
		{
			name:    "contrato inexistente",
			form:    url.Values{"intent": {"deposit.set"}, "lease_id": {"nao-existe"}, "deposit_amount": {"100"}},
			message: "Contrato não encontrado.",
		},
		{
			name:    "valor ausente",
			form:    url.Values{"intent": {"deposit.set"}, "lease_id": {"lease-1"}, "deposit_amount": {"  "}},
			message: "Informe o valor.",
		},
		{
			name:    "valor ilegível",
			form:    url.Values{"intent": {"deposit.set"}, "lease_id": {"lease-1"}, "deposit_amount": {"abc"}},
			message: "Informe um valor válido.",
		},
		{
			name:    "caução já devolvida",
			form:    url.Values{"intent": {"deposit.set"}, "lease_id": {"lease-1"}, "deposit_amount": {"100"}},
			message: "A caução deste contrato já foi devolvida.",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			panel := newCovPanel(t)
			panel.leases.items = []domain.Lease{covLeaseWithDeposit("lease-1", domain.DepositStatusRefunded, 2500)}

			recorder := covFormPost(panel, "/dashboard/deposit", testCase.form, panel.admin.ID)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, esperado 400", recorder.Code)
			}
			if body := recorder.Body.String(); !strings.Contains(body, testCase.message) {
				t.Fatalf("resposta sem a mensagem %q", testCase.message)
			}
		})
	}
}

func TestCovDepositRefundReturnsTheMoney(t *testing.T) {
	panel := newCovPanel(t)
	panel.leases.items = []domain.Lease{covLeaseWithDeposit("lease-1", domain.DepositStatusHeld, 2500)}

	recorder := covFormPost(panel, "/dashboard/deposit", url.Values{
		"intent":        {"deposit.refund"},
		"lease_id":      {"lease-1"},
		"refund_amount": {"R$ 1.500,00"},
		"refund_notes":  {"descontado o conserto"},
	}, panel.admin.ID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	lease := panel.leases.items[0]
	if lease.DepositStatus != domain.DepositStatusPartiallyRefunded {
		t.Fatalf("estado da caução = %q, esperado partially_refunded", lease.DepositStatus)
	}
	if lease.DepositRefundedAmt == nil || *lease.DepositRefundedAmt != 1500 {
		t.Fatalf("valor devolvido = %v, esperado 1500", lease.DepositRefundedAmt)
	}
}

func TestCovDepositRefundRejectsLeaseWithoutHeldDeposit(t *testing.T) {
	panel := newCovPanel(t)
	panel.leases.items = []domain.Lease{covLeaseWithDeposit("lease-1", "", 0)}

	recorder := covFormPost(panel, "/dashboard/deposit", url.Values{
		"intent":        {"deposit.refund"},
		"lease_id":      {"lease-1"},
		"refund_amount": {"100"},
	}, panel.admin.ID)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "Este contrato não tem caução retida para devolver.") {
		t.Fatalf("resposta sem a mensagem da caução ausente")
	}
}

func TestCovDepositMutationRejectsUnknownIntent(t *testing.T) {
	panel := newCovPanel(t)

	recorder := covFormPost(panel, "/dashboard/deposit", url.Values{"intent": {"deposit.desconhecido"}}, panel.admin.ID)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "Ação inválida.") {
		t.Fatalf("resposta sem a mensagem de ação inválida")
	}
}

func TestCovInspectionCreateKeepsItemsAndPhotos(t *testing.T) {
	panel := newCovPanel(t)
	panel.leases.items = []domain.Lease{covLeaseWithDeposit("lease-1", "", 0)}

	recorder := covMultipartPost(panel, "/dashboard/inspection", map[string][]string{
		"intent":       {"inspection.create"},
		"lease_id":     {"lease-1"},
		"kind":         {"checkin"},
		"performed_at": {"2026-09-01"},
		"condition":    {"otima"},
		"notes":        {"chaves entregues"},
		"item_area":    {"Sala", "Quarto"},
		"item_name":    {"Sofá", ""},
		"item_state":   {"bom", "ruim"},
		"item_notes":   {"", "risco na parede"},
	}, []covUpload{
		{field: "photos", filename: "sala.jpg", mime: "image/jpeg", content: []byte("foto-da-sala")},
		{field: "photos", filename: "quarto.png", mime: "image/png", content: []byte("foto-do-quarto")},
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	if len(panel.inspections.items) != 1 {
		t.Fatalf("vistorias gravadas = %d, esperado 1", len(panel.inspections.items))
	}
	inspection := panel.inspections.items[0]
	if inspection.Kind != domain.InspectionKindCheckIn || inspection.PerformedAt != "2026-09-01" {
		t.Fatalf("vistoria = %+v", inspection)
	}
	if len(inspection.Items) != 1 || inspection.Items[0].Item != "Sofá" || inspection.Items[0].Area != "Sala" {
		t.Fatalf("itens = %+v (o item sem nome deveria ser descartado)", inspection.Items)
	}
	if len(inspection.Photos) != 2 || inspection.Photos[0].Label != "sala.jpg" {
		t.Fatalf("fotos = %+v", inspection.Photos)
	}
	if len(panel.attachments.files) != 2 {
		t.Fatalf("arquivos guardados = %d, esperado 2", len(panel.attachments.files))
	}
}

func TestCovInspectionCreateValidatesMandatoryFields(t *testing.T) {
	cases := []struct {
		name    string
		fields  map[string][]string
		message string
	}{
		{
			name:    "tipo inválido",
			fields:  map[string][]string{"intent": {"inspection.create"}, "lease_id": {"lease-1"}, "kind": {"qualquer"}, "performed_at": {"2026-09-01"}, "condition": {"boa"}},
			message: "Informe o tipo da vistoria (entrada ou saída).",
		},
		{
			name:    "estado inválido",
			fields:  map[string][]string{"intent": {"inspection.create"}, "lease_id": {"lease-1"}, "kind": {"checkin"}, "performed_at": {"2026-09-01"}, "condition": {""}},
			message: "Informe o estado geral do imóvel.",
		},
		{
			name:    "data ausente",
			fields:  map[string][]string{"intent": {"inspection.create"}, "lease_id": {"lease-1"}, "kind": {"checkin"}, "performed_at": {" "}, "condition": {"boa"}},
			message: "Informe a data da vistoria.",
		},
		{
			name:    "contrato inexistente",
			fields:  map[string][]string{"intent": {"inspection.create"}, "lease_id": {"nao-existe"}, "kind": {"checkin"}, "performed_at": {"2026-09-01"}, "condition": {"boa"}},
			message: "Contrato não encontrado.",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			panel := newCovPanel(t)
			panel.leases.items = []domain.Lease{covLeaseWithDeposit("lease-1", "", 0)}

			recorder := covMultipartPost(panel, "/dashboard/inspection", testCase.fields, nil)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, esperado 400", recorder.Code)
			}
			if body := recorder.Body.String(); !strings.Contains(body, testCase.message) {
				t.Fatalf("resposta sem a mensagem %q", testCase.message)
			}
			if len(panel.inspections.items) != 0 {
				t.Fatal("vistoria inválida foi gravada")
			}
		})
	}
}

func TestCovInspectionUpdateKeepsExistingPhotosWithoutNewUpload(t *testing.T) {
	panel := newCovPanel(t)
	panel.inspections.items = []domain.Inspection{{
		ID:          "insp-1",
		LeaseID:     "lease-1",
		Kind:        domain.InspectionKindCheckIn,
		PerformedAt: "2026-08-01",
		Condition:   domain.InspectionConditionGood,
		Photos:      []domain.InspectionPhoto{{Name: "inspections/antiga.jpg", Label: "antiga.jpg"}},
	}}

	recorder := covMultipartPost(panel, "/dashboard/inspection", map[string][]string{
		"intent":       {"inspection.update"},
		"id":           {"insp-1"},
		"lease_id":     {"lease-1"},
		"kind":         {"checkout"},
		"performed_at": {"2026-09-20"},
		"condition":    {"regular"},
		"item_name":    {"Porta"},
	}, nil)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	inspection := panel.inspections.items[0]
	if inspection.Kind != domain.InspectionKindCheckOut || inspection.PerformedAt != "2026-09-20" {
		t.Fatalf("vistoria = %+v", inspection)
	}
	if len(inspection.Photos) != 1 || inspection.Photos[0].Label != "antiga.jpg" {
		t.Fatalf("fotos = %+v, esperado preservar a foto anterior", inspection.Photos)
	}
}

func TestCovInspectionUpdateWithoutExistingInspection(t *testing.T) {
	panel := newCovPanel(t)

	recorder := covMultipartPost(panel, "/dashboard/inspection", map[string][]string{
		"intent":       {"inspection.update"},
		"id":           {"nao-existe"},
		"lease_id":     {"lease-1"},
		"kind":         {"checkin"},
		"performed_at": {"2026-09-20"},
		"condition":    {"boa"},
	}, nil)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "Registro não encontrado.") {
		t.Fatalf("resposta sem a mensagem de registro ausente")
	}
}

func TestCovInspectionDeleteRemovesTheRecord(t *testing.T) {
	panel := newCovPanel(t)
	panel.inspections.items = []domain.Inspection{{ID: "insp-1", LeaseID: "lease-1"}}

	recorder := covMultipartPost(panel, "/dashboard/inspection", map[string][]string{
		"intent": {"inspection.delete"},
		"id":     {"insp-1"},
	}, nil)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	if len(panel.inspections.items) != 0 {
		t.Fatal("vistoria não excluída")
	}

	missing := covMultipartPost(panel, "/dashboard/inspection", map[string][]string{
		"intent": {"inspection.delete"},
		"id":     {"insp-1"},
	}, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("exclusão repetida: status = %d, esperado 404", missing.Code)
	}
}

func TestCovInspectionMutationRejectsUnknownIntent(t *testing.T) {
	panel := newCovPanel(t)

	recorder := covMultipartPost(panel, "/dashboard/inspection", map[string][]string{
		"intent": {"inspection.qualquer"},
	}, nil)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "Ação inválida.") {
		t.Fatalf("resposta sem a mensagem de ação inválida")
	}
}

func TestCovInspectionPhotosRejectUnsupportedFormat(t *testing.T) {
	panel := newCovPanel(t)

	recorder := covMultipartPost(panel, "/dashboard/inspection", map[string][]string{
		"intent":       {"inspection.create"},
		"lease_id":     {"lease-1"},
		"kind":         {"checkin"},
		"performed_at": {"2026-09-01"},
		"condition":    {"boa"},
	}, []covUpload{
		{field: "photos", filename: "contrato.txt", mime: "text/plain", content: []byte("não é foto")},
	})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "Formato de foto não suportado. Use JPG, PNG ou WEBP.") {
		t.Fatalf("resposta sem a mensagem de formato não suportado")
	}
}

func TestCovInspectionPhotosRejectTooManyFiles(t *testing.T) {
	panel := newCovPanel(t)

	uploads := make([]covUpload, 0, maxPhotos+1)
	for index := range maxPhotos + 1 {
		uploads = append(uploads, covUpload{
			field:    "photos",
			filename: fmt.Sprintf("foto-%d.jpg", index),
			mime:     "image/jpeg",
			content:  []byte("x"),
		})
	}

	recorder := covMultipartPost(panel, "/dashboard/inspection", map[string][]string{
		"intent":       {"inspection.create"},
		"lease_id":     {"lease-1"},
		"kind":         {"checkin"},
		"performed_at": {"2026-09-01"},
		"condition":    {"boa"},
	}, uploads)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "no máximo 10 fotos") {
		t.Fatalf("resposta sem a mensagem do limite de fotos")
	}
}

func TestCovInspectionPhotosRejectOversizedFile(t *testing.T) {
	panel := newCovPanel(t)

	recorder := covMultipartPost(panel, "/dashboard/inspection", map[string][]string{
		"intent":       {"inspection.create"},
		"lease_id":     {"lease-1"},
		"kind":         {"checkin"},
		"performed_at": {"2026-09-01"},
		"condition":    {"boa"},
	}, []covUpload{
		{field: "photos", filename: "grande.jpg", mime: "image/jpeg", content: make([]byte, maxPhotoBytes+1)},
	})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "no máximo 8 MB") {
		t.Fatalf("resposta sem a mensagem do limite por foto")
	}
}

func TestCovInspectionPhotosReportStorageFailure(t *testing.T) {
	panel := newCovPanel(t)
	panel.attachments.err = errCovBoom

	recorder := covMultipartPost(panel, "/dashboard/inspection", map[string][]string{
		"intent":       {"inspection.create"},
		"lease_id":     {"lease-1"},
		"kind":         {"checkin"},
		"performed_at": {"2026-09-01"},
		"condition":    {"boa"},
	}, []covUpload{
		{field: "photos", filename: "sala.jpg", mime: "image/jpeg", content: []byte("x")},
	})

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando o armazenamento falha", recorder.Code)
	}
}

func TestCovInspectionWithoutAttachmentStorage(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.Deps.Attachments = nil
	panel.leases.items = []domain.Lease{covLeaseWithDeposit("lease-1", "", 0)}

	recorder := covMultipartPost(panel, "/dashboard/inspection", map[string][]string{
		"intent":       {"inspection.create"},
		"lease_id":     {"lease-1"},
		"kind":         {"checkin"},
		"performed_at": {"2026-09-01"},
		"condition":    {"boa"},
	}, []covUpload{
		{field: "photos", filename: "sala.jpg", mime: "image/jpeg", content: []byte("x")},
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 sem armazenamento (fotos ignoradas): %s", recorder.Code, recorder.Body.String())
	}
	if len(panel.inspections.items) != 1 || len(panel.inspections.items[0].Photos) != 0 {
		t.Fatalf("vistoria = %+v, esperado sem fotos", panel.inspections.items)
	}
}

func TestCovParseMoneyFieldAcceptsBothFormats(t *testing.T) {
	cases := map[string]float64{
		"2500.00":     2500,
		"2500,00":     2500,
		"R$ 2.500,50": 2500.5,
		" 1234 ":      1234,
		"0":           0,
	}
	for raw, want := range cases {
		got, err := parseMoneyField(raw)
		if err != nil {
			t.Fatalf("parseMoneyField(%q): %v", raw, err)
		}
		if got != want {
			t.Fatalf("parseMoneyField(%q) = %v, esperado %v", raw, got, want)
		}
	}

	for _, raw := range []string{"", "   ", "abc", "R$"} {
		if _, err := parseMoneyField(raw); err == nil {
			t.Fatalf("parseMoneyField(%q) aceitou valor inválido", raw)
		}
	}
}

func TestCovIsSupportedPhoto(t *testing.T) {
	cases := []struct {
		contentType string
		filename    string
		want        bool
	}{
		{contentType: "image/jpeg", filename: "foto.JPG", want: true},
		{contentType: "image/png", filename: "foto", want: true},
		{contentType: "", filename: "foto.webp", want: true},
		{contentType: "image/heic", filename: "foto.heic", want: true},
		{contentType: "text/plain", filename: "foto.txt", want: false},
		{contentType: "", filename: "foto.txt", want: false},
		{contentType: "application/pdf", filename: "", want: false},
	}
	for _, testCase := range cases {
		if got := isSupportedPhoto(testCase.contentType, testCase.filename); got != testCase.want {
			t.Fatalf("isSupportedPhoto(%q, %q) = %v, esperado %v", testCase.contentType, testCase.filename, got, testCase.want)
		}
	}
}

func TestCovInspectionItemsFromParallelFields(t *testing.T) {
	form := url.Values{
		"item_area":  {"Sala", "", "Cozinha"},
		"item_name":  {"Sofá", "  ", "Fogão"},
		"item_state": {"bom"},
		"item_notes": {"", "riscado", "novo"},
	}
	request := httptest.NewRequest(http.MethodPost, "/dashboard/inspection", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := request.ParseForm(); err != nil {
		t.Fatalf("parse: %v", err)
	}

	items := inspectionItemsFrom(request)
	if len(items) != 2 {
		t.Fatalf("itens = %+v, esperado 2 (nome vazio descartado)", items)
	}
	if items[0].Item != "Sofá" || items[0].Area != "Sala" || items[0].State != "bom" || items[0].Notes != "" {
		t.Fatalf("primeiro item = %+v", items[0])
	}
	if items[1].Item != "Fogão" || items[1].Area != "Cozinha" || items[1].State != "" || items[1].Notes != "novo" {
		t.Fatalf("segundo item = %+v (campos ausentes devem ficar vazios)", items[1])
	}
}
