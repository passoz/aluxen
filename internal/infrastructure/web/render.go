package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
	"github.com/passoz/aluxen/internal/infrastructure/buildinfo"
)

//go:embed templates/*.html templates/partials/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Renderer renderiza as páginas server-side. Os templates são parseados uma
// única vez no boot e ficam imutáveis.
type Renderer struct {
	templates *template.Template
}

// PageData é o contexto base de toda página renderizada.
type PageData struct {
	Title     string
	Page      string
	CSRFToken string
	Build     buildinfo.Info
	AppURL    string
	Theme     string

	// Data é o contexto específico da página (tipado por template).
	Data any
}

// NewRenderer parseia os templates embutidos.
func NewRenderer() (*Renderer, error) {
	funcs := template.FuncMap{
		"money":        format.FormatCurrency,
		"moneyText":    format.FormatCurrencyFromString,
		"date":         format.FormatDateBR,
		"datetime":     format.FormatDateTimeBR,
		"leaseStatus":  func(status string) string { return domain.LeaseStatusLabel(domain.LeaseStatus(status)) },
		"category":     func(category string) string { return domain.ReceiptCategoryLabel(domain.ReceiptCategory(category)) },
		"receiptState": func(status string) string { return domain.ReceiptStatusLabel(domain.ReceiptStatus(status)) },
		"entityLabel":  domain.EntityLabel,
		"actionLabel":  func(action string) string { return domain.AuditActionMetaFor(action).Label },
		"originLabel":  func(origin string) string { return domain.AuditOriginLabel(domain.AuditOrigin(origin)) },
		"metadataLabel": func(key string) string {
			return domain.MetadataLabel(key)
		},
		"deref": func(value *string) string { return domain.Str(value) },
		"dict": func(values ...any) map[string]any {
			dict := make(map[string]any, len(values)/2)
			for index := 0; index+1 < len(values); index += 2 {
				key, _ := values[index].(string)
				dict[key] = values[index+1]
			}
			return dict
		},
		"json": func(value any) template.JS {
			encoded, err := json.Marshal(value)
			if err != nil {
				return template.JS("null")
			}
			return template.JS(encoded)
		},
		"hasPrefix": strings.HasPrefix,
		"upper":     strings.ToUpper,
		"add":       func(a, b int) int { return a + b },
		"seq": func(from, to int) []int {
			values := make([]int, 0, to-from+1)
			for value := from; value <= to; value++ {
				values = append(values, value)
			}
			return values
		},
	}

	templates, err := template.New("aluxen").Funcs(funcs).ParseFS(templateFS, "templates/*.html", "templates/partials/*.html")
	if err != nil {
		return nil, fmt.Errorf("parsear templates: %w", err)
	}
	return &Renderer{templates: templates}, nil
}

// Render executa o template da página com o status informado.
func (r *Renderer) Render(w http.ResponseWriter, status int, page string, data *PageData) error {
	var buffer bytes.Buffer
	if err := r.templates.ExecuteTemplate(&buffer, page, data); err != nil {
		return fmt.Errorf("renderizar %s: %w", page, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(buffer.Bytes()); err != nil {
		return fmt.Errorf("escrever resposta de %s: %w", page, err)
	}
	return nil
}

// StaticHandler serve os arquivos embutidos de static/.
func StaticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(fmt.Sprintf("static embutido inválido: %v", err))
	}
	return http.FileServer(http.FS(sub))
}
