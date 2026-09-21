// Package logging monta o logger estruturado (log/slog) da aplicação.
//
// Regras herdadas do app Bun (`src/utils/logger.ts`): uma linha por evento,
// JSON quando LOG_FORMAT=json ou em produção, texto legível nos demais casos,
// filtro por LOG_LEVEL e redação automática de campos sensíveis. A saída vai
// apenas para stdout — o processo não escreve arquivo de log.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/passoz/aluxen/internal/infrastructure/config"
)

// redacted é o marcador que substitui o valor de um campo sensível.
const redacted = "[redigido]"

// sensitiveKey casa as chaves cujo valor nunca vai para o log; é a mesma
// expressão do app Bun.
var sensitiveKey = regexp.MustCompile(`(?i)(pass(word|wd)?|senha|token|secret|segredo|cookie|authorization|api[-_]?key|hash|session)`)

// maxStackLines limita o número de linhas do rastreio gravado junto do erro.
const maxStackLines = 6

// sanitizeDepth é quantos níveis de objeto aninhado são percorridos antes de o
// valor virar um marcador — o mesmo limite do app Bun, que também impede
// recursão infinita em estruturas cíclicas.
const sanitizeDepth = 2

// objectPlaceholder substitui o valor profundo demais para ser percorrido.
const objectPlaceholder = "[objeto]"

// packagePath identifica os frames deste pacote na pilha.
const packagePath = "github.com/passoz/aluxen/internal/infrastructure/logging"

// New devolve o logger da aplicação: nível por LOG_LEVEL, JSON em produção ou
// com LOG_FORMAT=json e texto legível caso contrário, sempre em stdout.
func New(cfg *config.Config) *slog.Logger {
	return newLogger(cfg, os.Stdout)
}

// Scoped devolve um logger com o atributo `scope` fixado, preservando a
// configuração (nível e formato) do logger recebido. Chamar de novo substitui o
// escopo anterior em vez de duplicar a chave na saída.
func Scoped(logger *slog.Logger, scope string) *slog.Logger {
	if handler, ok := logger.Handler().(*redactHandler); ok {
		return slog.New(handler.withScope(scope))
	}
	return slog.New(&redactHandler{next: logger.Handler().WithAttrs([]slog.Attr{slog.String("scope", scope)})})
}

// newLogger monta o logger escrevendo em w (os.Stdout em produção; buffer nos testes).
func newLogger(cfg *config.Config, w io.Writer) *slog.Logger {
	level, silent := parseLevel(cfg.LogLevel)
	if silent {
		return slog.New(silentHandler{})
	}

	options := &slog.HandlerOptions{Level: level, ReplaceAttr: replaceAttr}
	var base slog.Handler
	if useJSON(cfg) {
		base = slog.NewJSONHandler(w, options)
	} else {
		base = slog.NewTextHandler(w, options)
	}
	return slog.New(&redactHandler{next: base})
}

// useJSON decide o formato: JSON em produção ou quando LOG_FORMAT=json pede.
func useJSON(cfg *config.Config) bool {
	return cfg.IsProduction() || strings.EqualFold(strings.TrimSpace(cfg.LogFormat), "json")
}

// parseLevel traduz LOG_LEVEL para o nível do slog. `silent` descarta tudo;
// `trace`/`verbose` são apelidos de `debug`; valor ausente ou inválido vira `info`.
func parseLevel(raw string) (level slog.Level, silent bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug", "trace", "verbose":
		return slog.LevelDebug, false
	case "warn":
		return slog.LevelWarn, false
	case "error":
		return slog.LevelError, false
	case "silent":
		return 0, true
	default:
		return slog.LevelInfo, false
	}
}

// replaceAttr renomeia as chaves do slog para o formato do app atual (`ts`,
// `message`, nível em minúsculas) e grava o instante em UTC com milissegundos.
func replaceAttr(_ []string, attr slog.Attr) slog.Attr {
	switch attr.Key {
	case slog.TimeKey:
		if at, ok := attr.Value.Any().(time.Time); ok {
			return slog.String("ts", at.UTC().Format("2006-01-02T15:04:05.000Z"))
		}
	case slog.LevelKey:
		if level, ok := attr.Value.Any().(slog.Level); ok {
			return slog.String(slog.LevelKey, strings.ToLower(level.String()))
		}
	case slog.MessageKey:
		return slog.String("message", attr.Value.String())
	}
	return attr
}

// silentHandler descarta todos os registros (LOG_LEVEL=silent).
type silentHandler struct{}

func (silentHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (silentHandler) Handle(context.Context, slog.Record) error { return nil }
func (silentHandler) WithAttrs([]slog.Attr) slog.Handler        { return silentHandler{} }
func (silentHandler) WithGroup(string) slog.Handler             { return silentHandler{} }

// redactHandler redige os campos sensíveis de cada registro, inclusive os
// fixados por With e dentro de grupos; guarda o escopo para não duplicá-lo.
type redactHandler struct {
	next     slog.Handler
	scope    string
	hasScope bool
	hidden   bool
}

func (h *redactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *redactHandler) Handle(ctx context.Context, record slog.Record) error {
	out := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	if h.hasScope {
		out.AddAttrs(slog.String("scope", h.scope))
	}
	record.Attrs(func(attr slog.Attr) bool {
		out.AddAttrs(h.sanitize(attr))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	sanitized := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		sanitized = append(sanitized, h.sanitize(attr))
	}
	clone := *h
	clone.next = h.next.WithAttrs(sanitized)
	return &clone
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	clone := *h
	// Um grupo sensível esconde tudo o que estiver dentro dele.
	clone.hidden = h.hidden || sensitiveKey.MatchString(name)
	clone.next = h.next.WithGroup(name)
	return &clone
}

// withScope devolve uma cópia do handler com o escopo fixado.
func (h *redactHandler) withScope(scope string) *redactHandler {
	clone := *h
	clone.scope, clone.hasScope = scope, true
	return &clone
}

// sanitize redige o atributo — a chave inteira quando sensível, o valor em
// profundidade nos demais casos.
func (h *redactHandler) sanitize(attr slog.Attr) slog.Attr {
	if h.hidden || sensitiveKey.MatchString(attr.Key) {
		return slog.String(attr.Key, redacted)
	}
	return sanitizeAttr(attr, sanitizeDepth)
}

// sanitizeAttr percorre o atributo aplicando a mesma regra de chave sensível.
func sanitizeAttr(attr slog.Attr, depth int) slog.Attr {
	if sensitiveKey.MatchString(attr.Key) {
		return slog.String(attr.Key, redacted)
	}
	return slog.Attr{Key: attr.Key, Value: sanitizeValue(attr.Value, depth)}
}

// sanitizeValue percorre grupos, erros, mapas e listas redigindo o que casa
// com sensitiveKey e resumindo o que passa do limite de profundidade.
func sanitizeValue(value slog.Value, depth int) slog.Value {
	resolved := value.Resolve()
	switch resolved.Kind() {
	case slog.KindGroup:
		if depth <= 0 {
			return slog.StringValue(objectPlaceholder)
		}
		attrs := resolved.Group()
		out := make([]slog.Attr, 0, len(attrs))
		for _, attr := range attrs {
			out = append(out, sanitizeAttr(attr, depth-1))
		}
		return slog.GroupValue(out...)
	case slog.KindAny:
		return slog.AnyValue(sanitizeAny(resolved.Any(), depth))
	default:
		return resolved
	}
}

// sanitizeAny redige valores arbitrários: erros viram {name,message,stack},
// mapas e listas são percorridos com o mesmo limite de profundidade.
func sanitizeAny(value any, depth int) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case loggedError:
		return typed
	case error:
		return loggedError{err: typed}
	case map[string]any:
		if depth <= 0 {
			return objectPlaceholder
		}
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if sensitiveKey.MatchString(key) {
				out[key] = redacted
				continue
			}
			out[key] = sanitizeAny(item, depth-1)
		}
		return out
	case map[string]string:
		if depth <= 0 {
			return objectPlaceholder
		}
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			if sensitiveKey.MatchString(key) {
				out[key] = redacted
				continue
			}
			out[key] = item
		}
		return out
	case []any:
		if depth <= 0 {
			return "[" + strconv.Itoa(len(typed)) + " itens]"
		}
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, sanitizeAny(item, depth-1))
		}
		return out
	default:
		return value
	}
}

// loggedError serializa o erro como {name, message, stack} sem perder a
// identidade do erro original: Unwrap mantém errors.Is/errors.As funcionando
// sobre o valor que ficou no registro.
type loggedError struct {
	err error
}

func (e loggedError) Error() string { return e.err.Error() }

// Unwrap devolve o erro original para errors.Is/errors.As.
func (e loggedError) Unwrap() error { return e.err }

// LogValue devolve o grupo {name, message, stack} que vai para a saída.
func (e loggedError) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("name", errorName(e.err)),
		slog.String("message", e.err.Error()),
		slog.String("stack", loggedStack()),
	)
}

// errorName devolve o nome do tipo concreto do erro (`ValidationError`,
// `SampleError`, …) ou `error` quando o tipo é interno ou anônimo.
func errorName(err error) string {
	typ := reflect.TypeOf(err)
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == nil {
		return "error"
	}
	name := typ.Name()
	if name == "" {
		return "error"
	}
	if first, _ := utf8.DecodeRuneInString(name); !unicode.IsUpper(first) {
		return "error"
	}
	return name
}

// loggedStack devolve até maxStackLines linhas apontando quem registrou o erro,
// ignorando os frames internos do log/slog e deste pacote. O Go não guarda a
// pilha de criação do erro, então o rastreio disponível é o do registro.
func loggedStack() string {
	pcs := make([]uintptr, 32)
	count := runtime.Callers(2, pcs)
	if count == 0 {
		return ""
	}

	frames := runtime.CallersFrames(pcs[:count])
	lines := make([]string, 0, maxStackLines)
	for len(lines) < maxStackLines {
		frame, more := frames.Next()
		if len(lines) == 0 && internalFrame(frame.Function) {
			if !more {
				break
			}
			continue
		}
		lines = append(lines, fmt.Sprintf("%s (%s:%d)", frame.Function, frame.File, frame.Line))
		if !more {
			break
		}
	}
	return strings.Join(lines, "\n")
}

// internalFrame reconhece os frames do log/slog e deste pacote.
func internalFrame(function string) bool {
	return strings.HasPrefix(function, "log/slog.") || strings.HasPrefix(function, packagePath+".")
}
