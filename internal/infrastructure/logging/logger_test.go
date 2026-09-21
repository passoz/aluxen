package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/infrastructure/config"
)

// SampleError é um erro de tipo exportado, para conferir o campo `name`.
type SampleError struct{ msg string }

func (e *SampleError) Error() string { return e.msg }

func newTestLogger(t *testing.T, cfg *config.Config) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	return newLogger(cfg, &out), &out
}

func jsonEntries(t *testing.T, out *bytes.Buffer) []map[string]any {
	t.Helper()
	raw := strings.TrimSpace(out.String())
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	entries := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("linha não é JSON válido (%v): %s", err, line)
		}
		entries = append(entries, entry)
	}
	return entries
}

func jsonEntry(t *testing.T, out *bytes.Buffer) map[string]any {
	t.Helper()
	entries := jsonEntries(t, out)
	if len(entries) != 1 {
		t.Fatalf("esperado exatamente 1 linha de log, obtidas %d: %q", len(entries), out.String())
	}
	return entries[0]
}

func nested(t *testing.T, entry map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := entry[key].(map[string]any)
	if !ok {
		t.Fatalf("campo %q = %#v, esperado objeto", key, entry[key])
	}
	return value
}

func developmentJSON() *config.Config {
	return &config.Config{AppEnv: "development", LogFormat: "json", LogLevel: "debug"}
}

func TestRedactsSensitiveFields(t *testing.T) {
	logger, out := newTestLogger(t, developmentJSON())

	logger.Info("Login do usuário",
		"user", "passoz",
		"password", "segredo-123",
		"sessionToken", "abc999",
		slog.Group("auth", slog.String("user", "passoz"), slog.String("token", "tok-abc")),
		"payload", map[string]any{"password": "s3nha", "cpf": "123"},
		"headers", map[string]string{"cookie": "sess-42", "user-agent": "curl"},
	)

	entry := jsonEntry(t, out)
	if got := entry["user"]; got != "passoz" {
		t.Errorf("user = %#v, esperado passoz", got)
	}
	if got := entry["password"]; got != redacted {
		t.Errorf("password = %#v, esperado %q", got, redacted)
	}
	if got := entry["sessionToken"]; got != redacted {
		t.Errorf("sessionToken = %#v, esperado %q", got, redacted)
	}

	auth := nested(t, entry, "auth")
	if got := auth["token"]; got != redacted {
		t.Errorf("auth.token = %#v, esperado %q", got, redacted)
	}
	if got := auth["user"]; got != "passoz" {
		t.Errorf("auth.user = %#v, esperado passoz", got)
	}

	payload := nested(t, entry, "payload")
	if got := payload["password"]; got != redacted {
		t.Errorf("payload.password = %#v, esperado %q", got, redacted)
	}
	if got := payload["cpf"]; got != "123" {
		t.Errorf("payload.cpf = %#v, esperado 123", got)
	}

	headers := nested(t, entry, "headers")
	if got := headers["cookie"]; got != redacted {
		t.Errorf("headers.cookie = %#v, esperado %q", got, redacted)
	}
	if got := headers["user-agent"]; got != "curl" {
		t.Errorf("headers.user-agent = %#v, esperado curl", got)
	}

	raw := out.String()
	for _, secret := range []string{"segredo-123", "abc999", "tok-abc", "s3nha", "sess-42"} {
		if strings.Contains(raw, secret) {
			t.Errorf("segredo %q vazou para o log: %s", secret, raw)
		}
	}
}

func TestRedactsEverythingInsideSensitiveGroup(t *testing.T) {
	logger, out := newTestLogger(t, developmentJSON())

	logger.WithGroup("session").Info("Sessão criada", "id", "1234", "expires", "amanhã")

	entry := jsonEntry(t, out)
	session := nested(t, entry, "session")
	if got := session["id"]; got != redacted {
		t.Errorf("session.id = %#v, esperado %q", got, redacted)
	}
	if got := session["expires"]; got != redacted {
		t.Errorf("session.expires = %#v, esperado %q", got, redacted)
	}
}

func TestRedactsFixedFields(t *testing.T) {
	logger, out := newTestLogger(t, developmentJSON())
	scoped := logger.With("apiKey", "chave-secreta", "tenant", "locador")

	scoped.Info("Chamada externa")

	entry := jsonEntry(t, out)
	if got := entry["apiKey"]; got != redacted {
		t.Errorf("apiKey = %#v, esperado %q", got, redacted)
	}
	if got := entry["tenant"]; got != "locador" {
		t.Errorf("tenant = %#v, esperado locador", got)
	}
	if strings.Contains(out.String(), "chave-secreta") {
		t.Errorf("valor fixado sensível vazou: %s", out.String())
	}
}

func TestDeepValuesAreSummarized(t *testing.T) {
	logger, out := newTestLogger(t, developmentJSON())

	logger.Info("Importação",
		"payload", map[string]any{
			"nivel1": map[string]any{
				"nivel2": map[string]any{
					"nivel3": "segredo-123",
				},
			},
		},
		"itens", []any{"a", "b"},
	)

	entry := jsonEntry(t, out)
	payload := nested(t, entry, "payload")
	nivel1 := nested(t, payload, "nivel1")
	if got := nivel1["nivel2"]; got != objectPlaceholder {
		t.Errorf("payload.nivel1.nivel2 = %#v, esperado %q", got, objectPlaceholder)
	}
	if strings.Contains(out.String(), "segredo-123") {
		t.Errorf("valor profundo vazou: %s", out.String())
	}
	items, ok := entry["itens"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("itens = %#v, esperado lista de 2", entry["itens"])
	}
}

func TestLevelFiltersRecords(t *testing.T) {
	cfg := &config.Config{AppEnv: "development", LogFormat: "json", LogLevel: "warn"}
	logger, out := newTestLogger(t, cfg)

	logger.Debug("detalhe irrelevante")
	logger.Info("informação")
	logger.Warn("atenção")
	logger.Error("falhou")

	entries := jsonEntries(t, out)
	if len(entries) != 2 {
		t.Fatalf("esperadas 2 linhas (warn e error), obtidas %d: %s", len(entries), out.String())
	}
	if got := entries[0]["level"]; got != "warn" {
		t.Errorf("nível da primeira linha = %#v, esperado warn", got)
	}
	if got := entries[0]["message"]; got != "atenção" {
		t.Errorf("mensagem da primeira linha = %#v, esperado atenção", got)
	}
	if got := entries[1]["level"]; got != "error" {
		t.Errorf("nível da segunda linha = %#v, esperado error", got)
	}
}

func TestSilentLevelDropsEverything(t *testing.T) {
	cfg := &config.Config{AppEnv: "development", LogLevel: "silent"}
	logger, out := newTestLogger(t, cfg)

	logger.Debug("detalhe")
	logger.Info("informação")
	logger.Warn("atenção")
	logger.Error("falhou")

	if out.Len() != 0 {
		t.Errorf("LOG_LEVEL=silent deveria descartar tudo, obtido: %s", out.String())
	}
}

func TestLevelAliasesAndInvalidValue(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"trace":   slog.LevelDebug,
		"verbose": slog.LevelDebug,
		"info":    slog.LevelInfo,
		"WARN":    slog.LevelWarn,
		"error":   slog.LevelError,
		"":        slog.LevelInfo,
		"barulho": slog.LevelInfo,
	}
	for raw, want := range cases {
		level, silent := parseLevel(raw)
		if silent {
			t.Errorf("parseLevel(%q) descartaria tudo, esperado nível %v", raw, want)
			continue
		}
		if level != want {
			t.Errorf("parseLevel(%q) = %v, esperado %v", raw, level, want)
		}
	}

	if _, silent := parseLevel("silent"); !silent {
		t.Error("parseLevel(\"silent\") deveria descartar tudo")
	}
}

func TestErrorIsSerializedAsNameMessageAndStack(t *testing.T) {
	logger, out := newTestLogger(t, developmentJSON())

	logger.Error("Falha ao processar", "error", fmt.Errorf("dar baixa no recibo: %w", &SampleError{msg: "quebrou"}))

	entry := jsonEntry(t, out)
	errorField := nested(t, entry, "error")
	if got := errorField["message"]; got != "dar baixa no recibo: quebrou" {
		t.Errorf("error.message = %#v", got)
	}
	if got := errorField["name"]; got != "error" {
		t.Errorf("error.name = %#v, esperado error (erro embrulhado)", got)
	}
	stack, ok := errorField["stack"].(string)
	if !ok || stack == "" {
		t.Fatalf("error.stack = %#v, esperado texto não vazio", errorField["stack"])
	}
	if lines := strings.Split(stack, "\n"); len(lines) > maxStackLines {
		t.Errorf("error.stack tem %d linhas, máximo %d", len(lines), maxStackLines)
	}
	if !strings.Contains(stack, ".go:") {
		t.Errorf("error.stack sem frames: %q", stack)
	}
}

func TestErrorKeepsConcreteTypeName(t *testing.T) {
	logger, out := newTestLogger(t, developmentJSON())

	logger.Error("Falha ao processar", "error", &SampleError{msg: "quebrou"})

	errorField := nested(t, jsonEntry(t, out), "error")
	if got := errorField["name"]; got != "SampleError" {
		t.Errorf("error.name = %#v, esperado SampleError", got)
	}
	if got := errorField["message"]; got != "quebrou" {
		t.Errorf("error.message = %#v, esperado quebrou", got)
	}
}

func TestSanitizedErrorKeepsIdentity(t *testing.T) {
	cause := &SampleError{msg: "quebrou"}
	wrapped := fmt.Errorf("dar baixa: %w", cause)

	attr := sanitizeAttr(slog.Any("error", wrapped), sanitizeDepth)

	logged, ok := attr.Value.Any().(error)
	if !ok {
		t.Fatalf("valor sanitizado = %#v, esperado um erro", attr.Value.Any())
	}
	if !errors.Is(logged, cause) {
		t.Error("errors.Is perdeu o erro original após a sanitização")
	}
	var typed *SampleError
	if !errors.As(logged, &typed) || typed != cause {
		t.Errorf("errors.As devolveu %#v, esperado o erro original", typed)
	}
}

func TestTextFormatInDevelopment(t *testing.T) {
	cfg := &config.Config{AppEnv: "development", LogLevel: "debug"}
	logger, out := newTestLogger(t, cfg)
	Scoped(logger, "http").Warn("Requisição recusada", "status", 404)

	line := strings.TrimSpace(out.String())
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err == nil {
		t.Fatalf("desenvolvimento deveria emitir texto legível, obtido JSON: %s", line)
	}
	for _, want := range []string{"level=warn", `message="Requisição recusada"`, "scope=http", "status=404"} {
		if !strings.Contains(line, want) {
			t.Errorf("linha de texto %q não contém %q", line, want)
		}
	}
}

func TestProductionForcesJSON(t *testing.T) {
	cfg := &config.Config{AppEnv: "production", LogLevel: "info", SessionSecret: strings.Repeat("s", 32)}
	logger, out := newTestLogger(t, cfg)

	Scoped(logger, "boot").Info("Aluxen iniciado", "port", 3000)

	entry := jsonEntry(t, out)
	if got := entry["level"]; got != "info" {
		t.Errorf("level = %#v, esperado info", got)
	}
	if got := entry["scope"]; got != "boot" {
		t.Errorf("scope = %#v, esperado boot", got)
	}
	if got := entry["port"]; got != float64(3000) {
		t.Errorf("port = %#v, esperado 3000", got)
	}
	if _, ok := entry["ts"]; !ok {
		t.Errorf("linha sem timestamp: %v", entry)
	}
	if _, ok := entry["time"]; ok {
		t.Errorf("timestamp deveria usar a chave ts: %v", entry)
	}
}

func TestScopedReplacesPreviousScope(t *testing.T) {
	logger, out := newTestLogger(t, developmentJSON())

	Scoped(Scoped(logger, "web"), "login").Info("Tentativa de acesso")

	entry := jsonEntry(t, out)
	if got := entry["scope"]; got != "login" {
		t.Errorf("scope = %#v, esperado login", got)
	}
	if count := strings.Count(out.String(), `"scope"`); count != 1 {
		t.Errorf("scope apareceu %d vezes, esperado 1: %s", count, out.String())
	}
}
