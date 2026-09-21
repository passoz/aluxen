package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/authz"
)

// covUploadFile monta um POST multipart autenticado com um arquivo no campo
// informado e o token de CSRF no próprio multipart.
func covUploadFile(panel *covPanel, target, field, filename, mime string, content []byte) *httptest.ResponseRecorder {
	panel.t.Helper()

	page := httptest.NewRecorder()
	panel.handler.Router().ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/login", nil))
	token := covCookieValue(page.Result().Cookies(), csrfCookieName)
	if token == "" {
		panel.t.Fatal("cookie de CSRF não emitido")
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField(csrfFieldName, token); err != nil {
		panel.t.Fatalf("csrf: %v", err)
	}
	if field != "" {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="`+field+`"; filename="`+filename+`"`)
		if mime != "" {
			header.Set("Content-Type", mime)
		}
		part, err := writer.CreatePart(header)
		if err != nil {
			panel.t.Fatalf("parte: %v", err)
		}
		if _, err := part.Write(content); err != nil {
			panel.t.Fatalf("conteúdo: %v", err)
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

func TestCovAuditListIsAJSONEndpoint(t *testing.T) {
	panel := covSeedPanel(t)

	anonymous := panel.as(http.MethodGet, "/audit", nil, "")
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 JSON sem sessão", anonymous.Code)
	}
	if contentType := anonymous.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, esperado JSON (nunca redirecionamento)", contentType)
	}
	if payload := covDecodeJSON(t, anonymous); payload["error"] != "Não autorizado." {
		t.Fatalf("payload = %v", payload)
	}

	ok := panel.as(http.MethodGet, "/audit", nil, panel.admin.ID)
	if ok.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", ok.Code, ok.Body.String())
	}
	if !strings.HasPrefix(ok.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("Content-Type = %q", ok.Header().Get("Content-Type"))
	}

	panel.audit.err = errCovBoom
	failing := panel.as(http.MethodGet, "/audit", nil, panel.admin.ID)
	if failing.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando a trilha não carrega", failing.Code)
	}
}

func TestCovAuditExportDownloadsCSV(t *testing.T) {
	panel := covSeedPanel(t)

	anonymous := panel.as(http.MethodGet, "/audit/export", nil, "")
	if anonymous.Code != http.StatusFound || anonymous.Header().Get("Location") != "/login" {
		t.Fatalf("status = %d location = %q, esperado 302 /login", anonymous.Code, anonymous.Header().Get("Location"))
	}

	recorder := panel.as(http.MethodGet, "/audit/export", nil, panel.admin.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/csv") {
		t.Fatalf("Content-Type = %q, esperado CSV", contentType)
	}
	disposition := recorder.Header().Get("Content-Disposition")
	if !strings.Contains(disposition, "aluxen-historico-2026-09-21.csv") {
		t.Fatalf("Content-Disposition = %q, esperado o nome datado pelo relógio da instância", disposition)
	}
	entry, ok := covLastAuditEntry(panel)
	if !ok || entry.Action != domain.ActionHistoryExport {
		t.Fatalf("exportação não auditada: %+v", entry)
	}

	panel.audit.err = errCovBoom
	failing := panel.as(http.MethodGet, "/audit/export", nil, panel.admin.ID)
	if failing.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando a leitura da trilha falha", failing.Code)
	}
}

func TestCovBackupExportStillDownloadsWhenAuditFails(t *testing.T) {
	panel := covSeedPanel(t)
	panel.audit.err = errCovBoom

	recorder := panel.as(http.MethodGet, "/export", nil, panel.admin.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (a auditoria é best-effort)", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, esperado application/json", contentType)
	}
	if !strings.Contains(recorder.Header().Get("Content-Disposition"), "aluxen-backup-2026-09-21.json") {
		t.Fatalf("Content-Disposition = %q", recorder.Header().Get("Content-Disposition"))
	}
	if _, ok := panel.logs.find("auditoria de backup não gravada"); !ok {
		t.Fatal("falha de auditoria do backup não registrada")
	}
	if !strings.Contains(recorder.Body.String(), "tenants") {
		t.Fatalf("backup sem o conteúdo esperado: %s", recorder.Body.String())
	}
}

func TestCovBackupExportReportsReadFailure(t *testing.T) {
	panel := covSeedPanel(t)
	panel.tenants.err = errCovBoom

	recorder := panel.as(http.MethodGet, "/export", nil, panel.admin.ID)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando os cadastros não carregam", recorder.Code)
	}
}

func TestCovBackupImportValidatesTheUpload(t *testing.T) {
	cases := []struct {
		name    string
		field   string
		content string
		message string
	}{
		{name: "sem arquivo", field: "", message: "Arquivo não enviado."},
		{name: "JSON inválido", field: "file", content: "não é json", message: "Arquivo JSON inválido."},
		{name: "formato errado", field: "file", content: `{"tenants":[]}`, message: "Formato de backup inválido."},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			panel := covSeedPanel(t)
			recorder := covUploadFile(panel, "/import", testCase.field, "backup.json", "application/json", []byte(testCase.content))

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, esperado 400", recorder.Code)
			}
			if body := recorder.Body.String(); !strings.Contains(body, testCase.message) {
				t.Fatalf("resposta sem a mensagem %q: %s", testCase.message, body)
			}
		})
	}
}

func TestCovBackupImportRejectsOversizedUpload(t *testing.T) {
	panel := covSeedPanel(t)
	panel.common.Deps.Opts.MaxBackupImportSize = 32

	recorder := covUploadFile(panel, "/import", "file", "backup.json", "application/json",
		[]byte(`{"tenants":[],"properties":[],"leases":[],"receipts":[],"padding":"mmmmmmmmmmmmmmmmmmmmmmmmmm"}`))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "Arquivo muito grande.") {
		t.Fatalf("resposta sem a mensagem de tamanho: %s", body)
	}
}

func TestCovBackupImportRestoresEmptyBackup(t *testing.T) {
	panel := covSeedPanel(t)

	recorder := covUploadFile(panel, "/import", "file", "backup.json", "application/json",
		[]byte(`{"tenants":[],"properties":[],"leases":[],"receipts":[]}`))

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, esperado 302: %s", recorder.Code, recorder.Body.String())
	}
	if location := recorder.Header().Get("Location"); location != "/dashboard?tab=dashboard&toast=importado" {
		t.Fatalf("Location = %q, esperado o painel com o aviso de importação", location)
	}
	entry, ok := covLastAuditEntry(panel)
	if !ok || entry.Action != domain.ActionBackupImport {
		t.Fatalf("importação não auditada: %+v", entry)
	}
}

func TestCovBackupImportReportsStorageFailure(t *testing.T) {
	panel := covSeedPanel(t)
	panel.tenants.err = errCovBoom

	recorder := covUploadFile(panel, "/import", "file", "backup.json", "application/json",
		[]byte(`{"tenants":[{"id":"t1","name":"Ana"}],"properties":[],"leases":[],"receipts":[]}`))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 quando a importação falha", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "Não foi possível importar os dados.") {
		t.Fatalf("resposta sem a mensagem de falha: %s", body)
	}
}

func TestCovDocumentVerifierPrefersTheInjectedSigner(t *testing.T) {
	panel := covSeedPanel(t)
	generator := authz.NewTokenGenerator(panel.common.Cfg.SessionSecret)
	panel.common.Deps.Signer = generator

	if verifier := panel.handler.verifier(); verifier == nil {
		t.Fatal("verifier devolveu nil com gerador injetado")
	}
	token := generator.Generate(authz.KindInspection, "insp-1")
	if !panel.handler.verifier().Verify(authz.KindInspection, "insp-1", token) {
		t.Fatal("verifier não reconheceu o token do gerador injetado")
	}

	// Sem gerador injetado a instância constrói um equivalente com o segredo da
	// configuração: aceita o token do próprio segredo e recusa o de outro.
	panel.common.Deps.Signer = covSigner{}
	fallback := panel.handler.verifier()
	if !fallback.Verify(authz.KindInspection, "insp-1", token) {
		t.Fatal("gerador de reserva não confere o token do segredo da instância")
	}
	foreign := authz.NewTokenGenerator("outro-segredo-qualquer").Generate(authz.KindInspection, "insp-1")
	if fallback.Verify(authz.KindInspection, "insp-1", foreign) {
		t.Fatal("gerador de reserva aceitou token de outro segredo")
	}
}
