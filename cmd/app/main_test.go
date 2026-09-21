package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/infrastructure/config"
)

func TestResolveStorageDir(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{"STORAGE_DIR explícito vence", &config.Config{StorageDir: "/dados/anexos", DBDir: "/dados"}, "/dados/anexos"},
		{"sem STORAGE_DIR usa o volume do banco", &config.Config{DBDir: "/data"}, "/data/storage"},
		{"sem nada usa o diretório corrente", &config.Config{}, "storage"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := resolveStorageDir(testCase.cfg); got != testCase.want {
				t.Fatalf("resolveStorageDir = %q, esperado %q", got, testCase.want)
			}
		})
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "https://aluxen.exemplo.com"); got != "https://aluxen.exemplo.com" {
		t.Fatalf("firstNonEmpty = %q", got)
	}
	if got := firstNonEmpty("primeiro", "segundo"); got != "primeiro" {
		t.Fatalf("firstNonEmpty = %q", got)
	}
	if got := firstNonEmpty(); got != "" {
		t.Fatalf("firstNonEmpty sem argumentos = %q", got)
	}
}

func TestHealthcheckAcceptsHealthyInstance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("healthcheck consultou %s, esperado /healthz", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	t.Setenv("PORT", strings.TrimPrefix(server.URL, "http://127.0.0.1:"))
	if err := healthcheck(); err != nil {
		t.Fatalf("healthcheck deveria passar: %v", err)
	}
}

func TestHealthcheckFailsOnUnhealthyInstance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "indisponível", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	t.Setenv("PORT", strings.TrimPrefix(server.URL, "http://127.0.0.1:"))
	err := healthcheck()
	if err == nil {
		t.Fatal("healthcheck deveria falhar com 503")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("erro inesperado: %v", err)
	}
}

func TestHealthcheckFailsWhenNothingIsListening(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	server.Close()

	t.Setenv("PORT", port)
	if err := healthcheck(); err == nil {
		t.Fatal("healthcheck deveria falhar sem servidor escutando")
	}
}
