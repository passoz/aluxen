package web

import (
	"html"
	"html/template"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// covFailWriter simula a conexão caindo no meio da resposta.
type covFailWriter struct {
	header http.Header
	status int
}

func (w *covFailWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *covFailWriter) WriteHeader(status int) { w.status = status }

func (w *covFailWriter) Write([]byte) (int, error) { return 0, errCovBoom }

// covProbe cria um template de sondagem com o mesmo mapa de funções do
// renderizador, para exercitar as funções de formatação pelo caminho real de
// execução de template.
func covProbe(t *testing.T, renderer *Renderer, name, text string) (*template.Template, string) {
	t.Helper()
	clone, err := renderer.templates.Clone()
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	return template.Must(clone.New(name).Parse(text)), name
}

func TestCovRenderWritesTemplateWithStatusAndContentType(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	recorder := httptest.NewRecorder()
	if err := renderer.Render(recorder, http.StatusTeapot, "error.html", &PageData{
		Title: "Aluxen | Erro",
		Page:  "error",
		Data:  map[string]any{"Status": 418, "Message": "Sou um bule."},
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	if recorder.Code != http.StatusTeapot {
		t.Fatalf("status = %d, esperado 418", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	if !strings.Contains(recorder.Body.String(), "Sou um bule.") {
		t.Fatalf("corpo sem a mensagem: %s", recorder.Body.String())
	}
}

func TestCovRenderReturnsErrorForUnknownTemplate(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	recorder := httptest.NewRecorder()
	err = renderer.Render(recorder, http.StatusOK, "nao-existe.html", &PageData{})
	if err == nil {
		t.Fatal("Render aceitou template inexistente")
	}
	if !strings.Contains(err.Error(), "nao-existe.html") {
		t.Fatalf("erro = %v, esperado citar o template", err)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("resposta parcial escrita antes do erro: %s", recorder.Body.String())
	}
}

func TestCovRenderReportsWriteFailure(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	writer := &covFailWriter{}
	err = renderer.Render(writer, http.StatusOK, "error.html", &PageData{Data: map[string]any{"Status": 500, "Message": "x"}})
	if err == nil {
		t.Fatal("Render não reportou a falha de escrita")
	}
	if writer.status != http.StatusOK {
		t.Fatalf("status escrito = %d, esperado 200 antes da falha do corpo", writer.status)
	}
}

func TestCovRendererHasOnlyPublishedTemplates(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	for _, name := range []string{"login.html", "dashboard.html", "error.html", "tab_dashboard", "tab_tenants", "tab_leases", "tab_receipts", "tab_season", "tab_calendar", "tab_review", "tab_intake", "tab_conversations", "tab_adjustment", "tab_history", "tab_metrics", "tab_properties"} {
		if !renderer.Has(name) {
			t.Fatalf("template %q não publicado", name)
		}
	}
	if renderer.Has("tab_inexistente") {
		t.Fatal("Has aceitou template inexistente")
	}
}

func TestCovExecuteRendersPartialAndRejectsUnknown(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	html, err := renderer.Execute("tab_dashboard", &tabData{Snapshot: &application.DashboardData{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(html) == "" {
		t.Fatal("Execute devolveu HTML vazio para template existente")
	}

	if _, err := renderer.Execute("tab_inexistente", &tabData{}); err == nil {
		t.Fatal("Execute aceitou template inexistente")
	}
}

func TestCovStaticHandlerServesEmbeddedFiles(t *testing.T) {
	handler := StaticHandler()

	cases := []struct{ path, contentType string }{
		{"/favicon.svg", "image/svg+xml"},
		{"/manifest.webmanifest", "application/manifest+json"},
		{"/styles/globals.css", "text/css; charset=utf-8"},
		{"/sw.js", ""},
	}
	for _, testCase := range cases {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, testCase.path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, esperado 200", testCase.path, recorder.Code)
		}
		if testCase.contentType != "" {
			if got := recorder.Header().Get("Content-Type"); got != testCase.contentType {
				t.Fatalf("%s: Content-Type = %q, esperado %q", testCase.path, got, testCase.contentType)
			}
		}
		if recorder.Body.Len() == 0 {
			t.Fatalf("%s: corpo vazio", testCase.path)
		}
	}

	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/nao-existe.png", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("arquivo inexistente: status = %d, esperado 404", missing.Code)
	}
}

func TestCovManifestIsServedWithPWAMimeType(t *testing.T) {
	if got := mime.TypeByExtension(".webmanifest"); !strings.HasPrefix(got, "application/manifest+json") {
		t.Fatalf("mime do manifest = %q", got)
	}
	if got := mime.TypeByExtension(".webp"); !strings.HasPrefix(got, "image/webp") {
		t.Fatalf("mime do webp = %q", got)
	}
}

func TestCovLabelOfAcceptsDomainValueStringAndUnknown(t *testing.T) {
	label := labelOf[domain.LeaseStatus](domain.LeaseStatusLabel)

	fromDomain := label(domain.LeaseStatusActive)
	fromString := label(string(domain.LeaseStatusActive))
	if fromDomain == "" || fromDomain != fromString {
		t.Fatalf("rótulo divergente: domínio = %q, texto = %q", fromDomain, fromString)
	}
	if got := label(42); got != "42" {
		t.Fatalf("valor desconhecido virou %q, esperado 42", got)
	}
}

func TestCovTemplateFuncsRespectTheirBoundaries(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	probe, probeName := covProbe(t, renderer, "cov_probe", strings.Join([]string{
		`{{percent 1 4}}`,
		`{{percent -3 4}}`,
		`{{percent 9 4}}`,
		`{{percent 1 0}}`,
		`{{weekdayName 0}}`,
		`{{weekdayName 6}}`,
		`{{weekdayName 7}}`,
		`{{weekdayName -1}}`,
		`{{statusColor "paid"}}`,
		`{{statusColor "partial"}}`,
		`{{statusColor "due"}}`,
		`{{statusColor "overdue"}}`,
		`{{statusColor "outro"}}`,
		`{{add 2 3}}`,
		`{{upper "abc"}}`,
		`{{if hasPrefix "abc" "a"}}sim{{else}}não{{end}}`,
		`{{deref .Ptr}}`,
		`{{len (seq 1 3)}}`,
	}, "|"))

	builder := &strings.Builder{}
	if err := probe.ExecuteTemplate(builder, probeName, map[string]any{}); err != nil {
		t.Fatalf("probe: %v", err)
	}
	rendered := builder.String()

	parts := strings.Split(string(rendered), "|")
	want := []string{
		"25", "0", "100", "0",
		"domingo", "sábado", "", "",
		"#10b981", "#3b82f6", "#f59e0b", "#ef4444", "#999999",
		"5", "ABC", "sim", "", "3",
	}
	if len(parts) != len(want) {
		t.Fatalf("probe devolveu %d valores (%v)", len(parts), parts)
	}
	for index := range want {
		if parts[index] != want[index] {
			t.Fatalf("valor %d = %q, esperado %q", index, parts[index], want[index])
		}
	}
}

func TestCovTemplateJSONFuncEscapesAndFallsBackOnFailure(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}

	probe, probeName := covProbe(t, renderer, "cov_json", `{{json .}}`)
	rendered := &strings.Builder{}
	if err := probe.ExecuteTemplate(rendered, probeName, map[string]string{"a": "b"}); err != nil {
		t.Fatalf("json: %v", err)
	}
	if decoded := html.UnescapeString(rendered.String()); decoded != `{"a":"b"}` {
		t.Fatalf("json = %s, esperado um objeto JSON válido", decoded)
	}

	failing, failingName := covProbe(t, renderer, "cov_json_fail", `{{json .}}`)
	builder := &strings.Builder{}
	if err := failing.ExecuteTemplate(builder, failingName, map[string]any{"c": make(chan int)}); err != nil {
		t.Fatalf("json com valor não serializável deveria devolver null, não erro: %v", err)
	}
	if builder.String() != "null" {
		t.Fatalf("json não serializável = %q, esperado null", builder.String())
	}
}
