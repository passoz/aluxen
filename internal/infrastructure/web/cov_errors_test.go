package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
)

func TestCovMapErrorTranslatesDomainErrorsToStatuses(t *testing.T) {
	panel := newCovPanel(t)

	cases := []struct {
		name        string
		err         error
		wantStatus  int
		wantMessage string
	}{
		{name: "não encontrado", err: domain.ErrNotFound, wantStatus: http.StatusNotFound, wantMessage: "Registro não encontrado."},
		{name: "conflito", err: domain.ErrConflict, wantStatus: http.StatusConflict, wantMessage: "Operação não permitida pelo estado atual do registro."},
		{name: "não autorizado", err: domain.ErrUnauthorized, wantStatus: http.StatusUnauthorized, wantMessage: "Não autorizado."},
		{name: "validação", err: domain.Invalid("Informe o valor da caução."), wantStatus: http.StatusBadRequest, wantMessage: "Informe o valor da caução."},
		{name: "embrulhado", err: fmt.Errorf("buscar contrato: %w", domain.ErrNotFound), wantStatus: http.StatusNotFound, wantMessage: "Registro não encontrado."},
		{name: "inesperado", err: errCovBoom, wantStatus: http.StatusInternalServerError, wantMessage: "Erro interno do servidor."},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/info", nil)
			request = request.WithContext(context.WithValue(request.Context(), requestIDKey, "abcd1234"))
			recorder := httptest.NewRecorder()

			panel.handler.mapError(recorder, request, testCase.err)

			if recorder.Code != testCase.wantStatus {
				t.Fatalf("status = %d, esperado %d", recorder.Code, testCase.wantStatus)
			}
			payload := covDecodeJSON(t, recorder)
			if payload["error"] != testCase.wantMessage {
				t.Fatalf("erro = %v, esperado %q", payload["error"], testCase.wantMessage)
			}
			if payload["requestId"] != "abcd1234" {
				t.Fatalf("requestId = %v, esperado o id do contexto", payload["requestId"])
			}
		})
	}
}

func TestCovMapErrorNeverLeaksUnexpectedDetail(t *testing.T) {
	panel := newCovPanel(t)

	recorder := httptest.NewRecorder()
	panel.handler.mapError(recorder, httptest.NewRequest(http.MethodGet, "/api/info", nil), fmt.Errorf("senha do banco: %w", errCovBoom))

	body := recorder.Body.String()
	if strings.Contains(body, "senha do banco") || strings.Contains(body, "cov: falha injetada") {
		t.Fatalf("detalhe interno vazou na resposta: %s", body)
	}
	if !strings.Contains(body, "Erro interno do servidor.") {
		t.Fatalf("resposta sem a mensagem genérica: %s", body)
	}
}

func TestCovMapErrorRendersErrorPageForHTMLRequests(t *testing.T) {
	panel := newCovPanel(t)

	recorder := httptest.NewRecorder()
	panel.handler.mapError(recorder, httptest.NewRequest(http.MethodGet, "/dashboard", nil), domain.ErrNotFound)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, esperado HTML", contentType)
	}
	if !strings.Contains(recorder.Body.String(), "Registro não encontrado.") {
		t.Fatalf("página de erro sem a mensagem: %s", recorder.Body.String())
	}
}

func TestCovMapErrorServesJSONForJSONClients(t *testing.T) {
	panel := newCovPanel(t)

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	panel.handler.mapError(recorder, request, domain.ErrConflict)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, esperado 409", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
}

func TestCovRenderErrorFallsBackToPlainText(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.Render = &Renderer{templates: template.Must(template.New("cov-sem-error").Parse(""))}

	recorder := httptest.NewRecorder()
	panel.handler.renderError(recorder, httptest.NewRequest(http.MethodGet, "/dashboard", nil), http.StatusNotFound, "Página não encontrada.")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "Página não encontrada.") {
		t.Fatalf("corpo = %q, esperado a mensagem em texto puro", body)
	}
}

func TestCovNotFoundHandlerDistinguishesHTMLFromAPI(t *testing.T) {
	panel := newCovPanel(t)

	page := panel.do(httptest.NewRequest(http.MethodGet, "/rota-inexistente", nil))
	if page.Code != http.StatusNotFound {
		t.Fatalf("página: status = %d, esperado 404", page.Code)
	}
	if !strings.Contains(page.Body.String(), "Página não encontrada.") {
		t.Fatalf("página 404 sem mensagem: %s", page.Body.String())
	}

	api := panel.do(httptest.NewRequest(http.MethodGet, "/api/rota-inexistente", nil))
	if api.Code != http.StatusNotFound {
		t.Fatalf("API: status = %d, esperado 404", api.Code)
	}
	if payload := covDecodeJSON(t, api); payload["error"] != "Rota não encontrada." {
		t.Fatalf("payload = %v", payload)
	}

	jsonClient := httptest.NewRequest(http.MethodGet, "/rota-inexistente", nil)
	jsonClient.Header.Set("Accept", "application/json")
	if recorder := panel.do(jsonClient); !strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("cliente JSON recebeu %q", recorder.Header().Get("Content-Type"))
	}
}

func TestCovRequestIDFromContext(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := requestIDFrom(request); got != "" {
		t.Fatalf("requestIDFrom = %q sem contexto, esperado vazio", got)
	}

	withID := request.WithContext(context.WithValue(request.Context(), requestIDKey, "deadbeef"))
	if got := requestIDFrom(withID); got != "deadbeef" {
		t.Fatalf("requestIDFrom = %q, esperado deadbeef", got)
	}
}

func TestCovNotFoundIsLoggedWithPath(t *testing.T) {
	panel := newCovPanel(t)

	panel.do(httptest.NewRequest(http.MethodGet, "/sem-rota", nil))

	record, ok := panel.logs.last()
	if !ok {
		t.Fatal("404 não registrado")
	}
	if path, _ := covAttrs(record)["path"].(string); path != "/sem-rota" {
		t.Fatalf("log do 404 sem path: %v", covAttrs(record))
	}
}
