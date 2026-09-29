package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/infrastructure/config"
	"github.com/passoz/aluxen/internal/infrastructure/db"
	"github.com/passoz/aluxen/internal/infrastructure/platform"
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
func TestDevBootstrapAndRestartPreservesPasswordAndData(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// 1. Primeiro boot em dev
	store, err := db.Open(ctx, "", dir)
	if err != nil {
		t.Fatalf("abrir banco: %v", err)
	}

	if err := store.EnsureAdmin(ctx, "passoz", "senha-de-teste", "Locador", ""); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}

	// Auto-seed em dev quando banco vazio
	count, err := store.Tenants.Count(ctx)
	if err != nil {
		t.Fatalf("contar locatários: %v", err)
	}
	if count == 0 {
		if err := db.SeedDemo(ctx, store); err != nil {
			t.Fatalf("SeedDemo: %v", err)
		}
	}

	// Verifica dados de demonstração
	count, err = store.Tenants.Count(ctx)
	if err != nil || count == 0 {
		t.Fatalf("esperava dados semeados, count=%d err=%v", count, err)
	}

	// Verifica usuário criado com senha padrão
	user, err := store.Users.ByUsername(ctx, "passoz")
	if err != nil {
		t.Fatalf("buscar usuário: %v", err)
	}
	if !user.MustChangePassword() {
		t.Fatal("novo usuário deveria exigir troca de senha")
	}

	// Usuário troca a senha
	hasher := platform.BcryptHasher{}
	newHash, err := hasher.Hash("minhasenha123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := store.Users.UpdatePassword(ctx, user.ID, newHash, "2026-09-22 15:00:00"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}

	// Fecha o banco (simulando parar make dev)
	if err := store.Close(); err != nil {
		t.Fatalf("fechar banco: %v", err)
	}

	// 2. Segundo boot em dev (reabrindo a mesma pasta)
	store2, err := db.Open(ctx, "", dir)
	if err != nil {
		t.Fatalf("reabrir banco: %v", err)
	}
	defer store2.Close()

	if err := store2.EnsureAdmin(ctx, "passoz", "senha-de-teste", "Locador", ""); err != nil {
		t.Fatalf("EnsureAdmin no restart: %v", err)
	}

	// Auto-seed não roda no restart porque já tem dados
	count2, err := store2.Tenants.Count(ctx)
	if err != nil {
		t.Fatalf("contar locatários no restart: %v", err)
	}
	if count2 == 0 {
		t.Fatal("dados de demonstração deveriam ter persistido após restart")
	}

	// Verifica que a senha alterada persistiu e NÃO voltou para a padrão
	user2, err := store2.Users.ByUsername(ctx, "passoz")
	if err != nil {
		t.Fatalf("buscar usuário no restart: %v", err)
	}
	if user2.MustChangePassword() {
		t.Fatal("usuário com senha alterada NÃO pode voltar a exigir troca de senha")
	}
	if err := hasher.Compare(user2.PasswordHash, "senha-de-teste"); err == nil {
		t.Fatal("senha padrão senha-de-teste NÃO pode mais ser aceita após troca de senha")
	}
	if err := hasher.Compare(user2.PasswordHash, "minhasenha123"); err != nil {
		t.Fatalf("nova senha minhasenha123 deveria ser aceita: %v", err)
	}
}
