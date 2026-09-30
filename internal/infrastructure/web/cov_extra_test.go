package web

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/authz"
)

func TestCovCSRFValidTokenWithoutSeparator(t *testing.T) {
	csrf := NewCSRF("cov-secret", false)
	handler, calls := covCSRFProbe(csrf)

	// Cookie e campo iguais e sem assinatura: a checagem de assinatura é quem
	// barra — nenhum token sem `valor.assinatura` entra.
	request := covCSRFRequest(http.MethodPost, "/dashboard", url.Values{csrfFieldName: {"sem-separador"}}.Encode())
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "sem-separador"})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403", recorder.Code)
	}
	if *calls != 0 {
		t.Fatal("token sem assinatura passou pelo middleware")
	}
}

func TestCovCSRFExportedFieldNameIsTheOneTheMiddlewareReads(t *testing.T) {
	csrf := NewCSRF("cov-secret", false)
	handler, calls := covCSRFProbe(csrf)

	name := csrf.FieldName()
	if strings.TrimSpace(name) == "" {
		t.Fatal("FieldName vazio: o formulário não teria onde mandar o token")
	}

	issued := httptest.NewRecorder()
	token := csrf.Token(issued, httptest.NewRequest(http.MethodGet, "/login", nil))

	request := covCSRFRequest(http.MethodPost, "/dashboard", url.Values{name: {token}}.Encode())
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || *calls != 1 {
		t.Fatalf("campo publicado pelo FieldName não foi aceito: status = %d, chamadas = %d", recorder.Code, *calls)
	}
}

func TestCovSessionRejectsTokenWithUndecodablePayload(t *testing.T) {
	manager := NewSessionManager("cov-secret", false)

	// Assinatura correta sobre um payload que não é base64url (o `!` é válido em
	// cookie, então a requisição chega até a verificação da assinatura).
	encoded := "!!nao-e-base64!!"
	token := encoded + "." + manager.mac(encoded)

	if id := manager.AdminID(covRequestWithCookie(adminCookieName, token)); id != "" {
		t.Fatalf("AdminID = %q com payload indecifrável, esperado vazio", id)
	}
}

func TestCovDashboardTabRenderFailureAnswers500(t *testing.T) {
	panel := covSeedPanel(t)
	// A aba existe, mas a execução falha (o contexto da aba não tem o dado que o
	// template pede): o painel responde 500 em vez de meia página.
	broken := template.Must(template.New("aluxen").Parse(""))
	if _, err := broken.New("tab_metrics").Parse("{{.Intake.Inexistente}}"); err != nil {
		t.Fatalf("template de sondagem: %v", err)
	}
	panel.common.Render = &Renderer{templates: broken}

	recorder := panel.as(http.MethodGet, "/dashboard", nil, panel.admin.ID)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando a aba não executa", recorder.Code)
	}
	if _, ok := panel.logs.find("falha ao renderizar aba do painel"); !ok {
		t.Fatal("falha de execução da aba não registrada")
	}
}

func TestCovLeaseIntentRejectsUnknownAction(t *testing.T) {
	panel := covSeedPanel(t)

	recorder := panel.csrfPost("/dashboard", url.Values{"intent": {"lease.desconhecido"}})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "Ação inválida.") {
		t.Fatal("resposta sem a mensagem de ação inválida")
	}
}

func TestCovReceiptIntentRejectsUnknownAction(t *testing.T) {
	panel := covSeedPanel(t)

	recorder := panel.csrfPost("/dashboard", url.Values{"intent": {"receipt.desconhecido"}})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", recorder.Code)
	}
}

func TestCovReceiptBatchPayPaysTheSelectedReceipts(t *testing.T) {
	panel := covSeedPanel(t)
	panel.receipts.items = []domain.Receipt{
		{ID: "receipt-1", Amount: 2500, DueDate: "2026-09-10"},
		{ID: "receipt-2", Amount: 300, DueDate: "2026-09-10"},
		{ID: "receipt-3", Amount: 50, DueDate: "2026-09-10"},
	}

	recorder := panel.csrfPost("/dashboard", url.Values{
		"intent": {"receipt.batch-pay"},
		"ids":    {"receipt-1,receipt-2"},
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	if panel.receipts.items[0].PaidAt == nil || panel.receipts.items[1].PaidAt == nil {
		t.Fatalf("lote não deu baixa nos selecionados: %+v", panel.receipts.items)
	}
	if panel.receipts.items[2].PaidAt != nil {
		t.Fatal("lote deu baixa em lançamento fora da seleção")
	}
}

func TestCovReceiptBatchRequiresASelection(t *testing.T) {
	panel := covSeedPanel(t)

	recorder := panel.csrfPost("/dashboard", url.Values{"intent": {"receipt.batch-pay"}, "ids": {"  "}})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 sem seleção", recorder.Code)
	}
}

func TestCovReceiptIDsFromFormSplitsTheCSV(t *testing.T) {
	if ids := receiptIDsFromForm("  "); ids != nil {
		t.Fatalf("lista vazia = %v, esperado nil", ids)
	}
	ids := receiptIDsFromForm("receipt-1,receipt-2")
	if len(ids) != 2 || ids[0] != "receipt-1" || ids[1] != "receipt-2" {
		t.Fatalf("ids = %v", ids)
	}
	if single := receiptIDsFromForm("receipt-1"); len(single) != 1 {
		t.Fatalf("id único = %v", single)
	}
}

func TestCovLeaseRentAmountAcceptsRawAndMaskedValues(t *testing.T) {
	cases := map[string]float64{
		"2500":      2500,
		"2500.5":    2500.5,
		"2.500,00":  2500,
		" 1.200,5 ": 1200.5,
	}
	for raw, want := range cases {
		if got := leaseRentAmount(raw); got != want {
			t.Fatalf("leaseRentAmount(%q) = %v, esperado %v", raw, got, want)
		}
	}
	for _, raw := range []string{"", "  ", "abc", "R$"} {
		if got := leaseRentAmount(raw); got != 0 {
			t.Fatalf("leaseRentAmount(%q) = %v, esperado 0", raw, got)
		}
	}
}

func TestCovTabDataWithoutSnapshotHasNoRows(t *testing.T) {
	empty := &tabData{}
	if rows := empty.LeaseRows(); rows != nil {
		t.Fatalf("LeaseRows = %v, esperado nil sem instantâneo", rows)
	}
	if count := empty.TenantLeaseCount("tenant-1"); count != 0 {
		t.Fatalf("TenantLeaseCount = %d, esperado 0 sem instantâneo", count)
	}
}

func TestCovSettingsPageReportsPasswordStateFailure(t *testing.T) {
	panel := newCovPanel(t)
	// A sessão já foi validada; a checagem seguinte do estado da senha falha.
	panel.users.failAfterIDReads = 1

	recorder := panel.as(http.MethodGet, "/settings", nil, panel.admin.ID)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando o estado da senha não carrega", recorder.Code)
	}
}

func TestCovSettingsPageFallsBackWhenTemplateFails(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.Render = &Renderer{templates: template.Must(template.New("cov-sem-settings").Parse(""))}

	recorder := panel.as(http.MethodGet, "/settings", nil, panel.admin.ID)
	if body := recorder.Body.String(); strings.Contains(body, "<html") {
		t.Fatalf("fallback das configurações devolveu HTML: %s", body)
	}
	if _, ok := panel.logs.find("falha ao renderizar configurações"); !ok {
		t.Fatal("falha de renderização das configurações não registrada")
	}
}

func TestCovInspectionMutationRejectsUnreadableBody(t *testing.T) {
	panel := newCovPanel(t)

	// Multipart ilegível: o token de CSRF vai na query, o corpo é lixo.
	recorder := covMalformedPost(panel, "/dashboard/inspection", panel.admin.ID, true)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "Não foi possível ler os dados enviados.") {
		t.Fatalf("resposta sem a mensagem de corpo ilegível: %s", body)
	}
}

func TestCovSeasonDocumentsRejectRequestsWithoutID(t *testing.T) {
	panel := covSeedPanel(t)

	for _, target := range []string{"/inspection-pdf", "/deposit-pdf"} {
		recorder := panel.as(http.MethodGet, target, nil, panel.admin.ID)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, esperado 400 sem id", target, recorder.Code)
		}
		if body := recorder.Body.String(); !strings.Contains(body, "ID inválido") {
			t.Fatalf("%s: corpo = %q", target, body)
		}
	}
}

func TestCovSeasonDocumentsReportMissingDocuments(t *testing.T) {
	panel := covSeedPanel(t)

	inspection := panel.as(http.MethodGet, "/inspection-pdf?id=nao-existe", nil, panel.admin.ID)
	if inspection.Code != http.StatusNotFound {
		t.Fatalf("vistoria: status = %d, esperado 404", inspection.Code)
	}
	if body := inspection.Body.String(); !strings.Contains(body, "Vistoria não encontrada") {
		t.Fatalf("vistoria: corpo = %q", body)
	}

	deposit := panel.as(http.MethodGet, "/deposit-pdf?id=nao-existe", nil, panel.admin.ID)
	if deposit.Code != http.StatusNotFound {
		t.Fatalf("caução: status = %d, esperado 404", deposit.Code)
	}
	if body := deposit.Body.String(); !strings.Contains(body, "Caução não encontrada") {
		t.Fatalf("caução: corpo = %q", body)
	}
}

func TestCovDepositReceiptRefusesContractWithoutDeposit(t *testing.T) {
	panel := covSeedPanel(t)
	panel.leases.items = []domain.Lease{{ID: "lease-1", TenantID: "tenant-1", PropertyID: "property-1", TenantName: "Ana", PropertyName: "Casa da praia"}}

	recorder := panel.as(http.MethodGet, "/deposit-pdf?id=lease-1", nil, panel.admin.ID)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403 para contrato sem caução", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "Este contrato não tem caução registrada.") {
		t.Fatalf("corpo = %q", body)
	}
}

func TestCovSeasonDocumentsRequireAccess(t *testing.T) {
	panel := covSeedPanel(t)

	for _, target := range []string{"/inspection-pdf?id=insp-1", "/deposit-pdf?id=lease-1"} {
		recorder := panel.as(http.MethodGet, target, nil, "")
		if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "/login" {
			t.Fatalf("%s: status = %d location = %q, esperado 302 /login", target, recorder.Code, recorder.Header().Get("Location"))
		}
	}
}

func TestCovDocumentVerificationPagesRejectLinksWithoutToken(t *testing.T) {
	panel := covSeedPanel(t)

	for _, target := range []string{"/validar/vistoria/insp-1", "/validar/caucao/lease-1"} {
		recorder := panel.as(http.MethodGet, target, nil, "")
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, esperado 400 sem chave de autenticidade", target, recorder.Code)
		}
	}
}

func TestCovDocumentVerificationPagesReportMissingDocuments(t *testing.T) {
	panel := covSeedPanel(t)

	cases := []struct {
		kind    string
		id      string
		message string
	}{
		{kind: authz.KindInspection, id: "insp-nao-existe", message: "Vistoria não localizada na base oficial de registros."},
		{kind: authz.KindDeposit, id: "lease-nao-existe", message: "Caução não localizada na base oficial de registros."},
	}
	for _, testCase := range cases {
		token := panel.common.Deps.Signer.Generate(testCase.kind, testCase.id)
		base := "/validar/vistoria/"
		if testCase.kind == authz.KindDeposit {
			base = "/validar/caucao/"
		}
		target := base + testCase.id + "?token=" + url.QueryEscape(token)

		recorder := panel.as(http.MethodGet, target, nil, "")
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, esperado 404: %s", target, recorder.Code, recorder.Body.String())
		}
		if body := recorder.Body.String(); !strings.Contains(body, testCase.message) {
			t.Fatalf("%s: página sem a mensagem %q", target, testCase.message)
		}
	}
}
