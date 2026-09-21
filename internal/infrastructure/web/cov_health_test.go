package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCovHealthzDoesNotTouchDependencies(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.Ping = func(context.Context) error { return errCovBoom }

	recorder := panel.do(httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 mesmo com o banco fora do ar", recorder.Code)
	}
	if recorder.Body.String() != "ok" {
		t.Fatalf("corpo = %q, esperado ok", recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
}

func TestCovReadyzWithoutDatabaseHook(t *testing.T) {
	panel := newCovPanel(t)

	recorder := panel.do(httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ready" {
		t.Fatalf("status = %d corpo = %q, esperado 200 ready", recorder.Code, recorder.Body.String())
	}
}

func TestCovReadyzWithHealthyDatabase(t *testing.T) {
	panel := newCovPanel(t)
	called := false
	panel.common.Ping = func(context.Context) error {
		called = true
		return nil
	}

	recorder := panel.do(httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ready" {
		t.Fatalf("status = %d corpo = %q, esperado 200 ready", recorder.Code, recorder.Body.String())
	}
	if !called {
		t.Fatal("readiness não consultou o banco")
	}
}

func TestCovReadyzReportsUnavailableDatabase(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.Ping = func(context.Context) error { return errCovBoom }

	recorder := panel.do(httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, esperado 503 com o banco fora do ar", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "banco indisponível") {
		t.Fatalf("corpo = %q", recorder.Body.String())
	}
	record, ok := panel.logs.last()
	if !ok {
		t.Fatal("falha de readiness não registrada")
	}
	if path, _ := covAttrs(record)["scope"].(string); path != "http" {
		t.Fatalf("log sem escopo http: %v", covAttrs(record))
	}
}

func TestCovHealthPayloadDescribesDatabasePerDialect(t *testing.T) {
	cases := map[string]string{
		"sqlite":   "sqlite (modernc)",
		"postgres": "postgres (pgx)",
		"mysql":    "desconhecido (mysql)",
		"":         "desconhecido ()",
	}

	for dialect, want := range cases {
		t.Run(dialect, func(t *testing.T) {
			panel := newCovPanel(t)
			panel.common.Dialect = dialect

			recorder := panel.do(httptest.NewRequest(http.MethodGet, "/api/health", nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, esperado 200", recorder.Code)
			}
			payload := covDecodeJSON(t, recorder)
			if payload["status"] != "ok" || payload["runtime"] != "go" || payload["framework"] != "net/http" || payload["orm"] != "sqlc" {
				t.Fatalf("payload = %v", payload)
			}
			if payload["database"] != want {
				t.Fatalf("database = %v, esperado %q", payload["database"], want)
			}
			if timestamp, _ := payload["timestamp"].(string); timestamp == "" {
				t.Fatalf("payload sem timestamp: %v", payload)
			}
		})
	}
}

func TestCovHealthIsReachableWithoutSession(t *testing.T) {
	panel := newCovPanel(t)

	recorder := panel.as(http.MethodGet, "/api/health", nil, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (rota isenta de sessão)", recorder.Code)
	}
}
