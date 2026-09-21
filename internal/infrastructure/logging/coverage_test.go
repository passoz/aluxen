package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/passoz/aluxen/internal/infrastructure/config"
)

// covSentinel é a causa embrulhada usada para conferir a identidade do erro.
var covSentinel = errors.New("recibo não encontrado")

// covInternalError é um erro de tipo interno (nome minúsculo): o campo `name`
// do registro precisa sair como `error`, não com o nome do tipo.
type covInternalError struct{}

func (covInternalError) Error() string { return "privado" }

func covLogger(t *testing.T, cfg *config.Config) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	return newLogger(cfg, &out), &out
}

func covJSONConfig() *config.Config {
	return &config.Config{AppEnv: "development", LogFormat: "json", LogLevel: "debug"}
}

func covEntry(t *testing.T, out *bytes.Buffer) map[string]any {
	t.Helper()
	raw := strings.TrimSpace(out.String())
	if raw == "" {
		t.Fatal("nenhuma linha de log foi escrita")
	}
	entries := strings.Split(raw, "\n")
	if len(entries) != 1 {
		t.Fatalf("esperada exatamente 1 linha de log, obtidas %d: %q", len(entries), raw)
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(entries[0]), &entry); err != nil {
		t.Fatalf("linha não é JSON válido (%v): %s", err, entries[0])
	}
	return entry
}

func covObject(t *testing.T, entry map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := entry[key].(map[string]any)
	if !ok {
		t.Fatalf("campo %q = %#v, esperado objeto", key, entry[key])
	}
	return value
}

// New é o logger do processo: em produção a linha vai para stdout em JSON. O
// cano substitui o stdout só durante a chamada para conferir isso.
func TestCovNewWritesJSONToStandardOutput(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("criar cano: %v", err)
	}
	saved := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = saved
		_ = reader.Close()
		_ = writer.Close()
	})

	logger := New(&config.Config{
		AppEnv: "production", LogLevel: "info", SessionSecret: strings.Repeat("s", 32),
	})
	logger.Info("instância pronta", "port", 3000)

	os.Stdout = saved
	if err := writer.Close(); err != nil {
		t.Fatalf("fechar o cano: %v", err)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ler a saída: %v", err)
	}

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(raw), &entry); err != nil {
		t.Fatalf("stdout não tem uma linha JSON (%v): %q", err, raw)
	}
	if got := entry["message"]; got != "instância pronta" {
		t.Errorf("message = %#v, esperado instância pronta", got)
	}
	if got := entry["level"]; got != "info" {
		t.Errorf("level = %#v, esperado info", got)
	}
	if got := entry["port"]; got != float64(3000) {
		t.Errorf("port = %#v, esperado 3000", got)
	}
}

// Scoped também envolve um handler que não é o deste pacote (o caso de um
// logger montado por outra biblioteca): o escopo entra e a redação continua
// valendo por cima dele.
func TestCovScopedWrapsAForeignHandler(t *testing.T) {
	var out bytes.Buffer
	foreign := slog.New(slog.NewJSONHandler(&out, nil))

	Scoped(foreign, "http").Warn("Requisição recusada", "password", "segredo-123", "status", 401)

	entry := covEntry(t, &out)
	if got := entry["scope"]; got != "http" {
		t.Errorf("scope = %#v, esperado http", got)
	}
	if got := entry["password"]; got != redacted {
		t.Errorf("password = %#v, esperado %q", got, redacted)
	}
	if strings.Contains(out.String(), "segredo-123") {
		t.Errorf("handler estranho não foi redigido: %s", out.String())
	}
}

// LOG_LEVEL=silent descarta tudo, inclusive depois de fixar atributos e abrir
// um grupo; o handler também aceita um registro sem escrever nada.
func TestCovSilentLevelDiscardsStructuredRecords(t *testing.T) {
	cfg := &config.Config{AppEnv: "development", LogLevel: "silent"}
	logger, out := covLogger(t, cfg)
	ctx := context.Background()

	if logger.Enabled(ctx, slog.LevelError) {
		t.Error("nível silent deveria desabilitar todos os níveis")
	}
	logger.With("user", "passoz").WithGroup("payload").Info("nada disto aparece")
	logger.Error("nem os erros", "error", covSentinel)

	if out.Len() != 0 {
		t.Errorf("silent escreveu no destino: %s", out.String())
	}

	record := slog.NewRecord(time.Time{}, slog.LevelError, "descartado", 0)
	if err := (silentHandler{}).Handle(ctx, record); err != nil {
		t.Errorf("Handle devolveu erro: %v", err)
	}
	if (silentHandler{}).Enabled(ctx, slog.LevelDebug) {
		t.Error("silentHandler deveria recusar o nível debug")
	}
}

// Grupos aninhados além do limite de profundidade viram um marcador: o valor
// fundo demais não vaza para a linha de log.
func TestCovGroupsBeyondTheDepthLimitAreSummarized(t *testing.T) {
	logger, out := covLogger(t, covJSONConfig())

	logger.Info("Sincronização",
		slog.Group("nivel1",
			slog.Group("nivel2",
				slog.Group("nivel3", slog.String("token", "segredo-123")))))

	entry := covEntry(t, out)
	nivel1 := covObject(t, entry, "nivel1")
	nivel2 := covObject(t, nivel1, "nivel2")
	if got := nivel2["nivel3"]; got != objectPlaceholder {
		t.Errorf("nivel1.nivel2.nivel3 = %#v, esperado %q", got, objectPlaceholder)
	}
	if strings.Contains(out.String(), "segredo-123") {
		t.Errorf("valor profundo vazou: %s", out.String())
	}
}

// Um valor nulo é preservado como nulo, e mapas fundos demais — inclusive os de
// texto, como o mapa de variáveis de ambiente — viram marcador em vez de
// derramar o conteúdo inteiro.
func TestCovNilAndOversizedMapsInPayloads(t *testing.T) {
	logger, out := covLogger(t, covJSONConfig())

	logger.Info("Importação",
		"vazio", nil,
		"aninhado", map[string]any{
			"nivel1": map[string]any{
				"nivel2": map[string]any{"token": "segredo-123"},
			},
		},
		"variaveis", map[string]any{
			"nivel1": map[string]any{
				"nivel2": map[string]string{"SESSION_SECRET": "segredo-456"},
			},
		},
	)

	entry := covEntry(t, out)
	value, present := entry["vazio"]
	if !present || value != nil {
		t.Errorf("vazio = %#v, esperado null", value)
	}

	aninhado := covObject(t, entry, "aninhado")
	nivel1 := covObject(t, aninhado, "nivel1")
	if got := nivel1["nivel2"]; got != objectPlaceholder {
		t.Errorf("aninhado.nivel1.nivel2 = %#v, esperado %q", got, objectPlaceholder)
	}

	variaveis := covObject(t, entry, "variaveis")
	nivel1 = covObject(t, variaveis, "nivel1")
	if got := nivel1["nivel2"]; got != objectPlaceholder {
		t.Errorf("variaveis.nivel1.nivel2 = %#v, esperado %q", got, objectPlaceholder)
	}

	raw := out.String()
	for _, secret := range []string{"segredo-123", "segredo-456"} {
		if strings.Contains(raw, secret) {
			t.Errorf("segredo %q vazou: %s", secret, raw)
		}
	}
}

// O erro registrado conserva a mensagem e a identidade original
// (errors.Is/errors.As continuam valendo sobre ele), e um valor já embrulhado
// pelo handler não é embrulhado de novo — o registro não muda de forma.
func TestCovLoggedErrorKeepsMessageAndIdentity(t *testing.T) {
	original := fmt.Errorf("dar baixa no recibo: %w", covSentinel)
	logged := loggedError{err: original}

	if got := logged.Error(); got != "dar baixa no recibo: recibo não encontrado" {
		t.Errorf("Error() = %q", got)
	}
	if !errors.Is(logged, covSentinel) {
		t.Error("errors.Is perdeu a causa após a sanitização")
	}
	var typed covInternalError
	wrapped := loggedError{err: fmt.Errorf("leituras: %w", covInternalError{})}
	if !errors.As(wrapped, &typed) {
		t.Errorf("errors.As devolveu %#v, esperado covInternalError", typed)
	}

	again, ok := sanitizeAny(logged, sanitizeDepth).(loggedError)
	if !ok {
		t.Fatalf("erro já embrulhado deixou de ser loggedError: %#v", sanitizeAny(logged, sanitizeDepth))
	}
	if again.err != original {
		t.Errorf("erro embrulhado de novo: %#v", again.err)
	}
}

// O campo `name` do erro registrado usa o nome do tipo concreto e cai em
// `error` quando o tipo é interno ou anônimo — inclusive sem tipo nenhum.
func TestCovErrorNameForNilAnonymousAndInternalTypes(t *testing.T) {
	if got := errorName(nil); got != "error" {
		t.Errorf("errorName(nil) = %q, esperado error", got)
	}
	if got := errorName(&covInternalError{}); got != "error" {
		t.Errorf("errorName(tipo interno) = %q, esperado error", got)
	}

	logger, out := covLogger(t, covJSONConfig())
	logger.Error("Falha anônima", "error", struct{ error }{errors.New("quebrou por dentro")})

	errorField := covObject(t, covEntry(t, out), "error")
	if got := errorField["name"]; got != "error" {
		t.Errorf("name = %#v, esperado error", got)
	}
	if got := errorField["message"]; got != "quebrou por dentro" {
		t.Errorf("message = %#v, esperado quebrou por dentro", got)
	}
}
