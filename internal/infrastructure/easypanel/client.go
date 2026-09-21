// Package easypanel é o cliente da API de provisionamento do Easypanel: cria o
// app, o volume, as variáveis, o domínio e dispara o deploy de uma instância do
// Aluxen — uma instância por cliente ou demo (ARQUITETURA-DEPLOY.md §8).
//
// Nenhum segredo vive aqui: a URL e a chave da API vêm do ambiente da operação
// (`EASYPANEL_URL`, `EASYPANEL_API_KEY`) e são injetadas pelo comando de
// provisionamento. Toda falha HTTP vira um *APIError com o status e o corpo,
// porque é isso que o operador precisa ver quando a API recusa uma chamada.
package easypanel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// defaultTimeout é o teto de cada chamada quando o chamador não injeta um
	// cliente HTTP próprio.
	defaultTimeout = 30 * time.Second
	// maxErrorBody limita o corpo guardado no erro: a API pode responder HTML de
	// proxy e não faz sentido carregar a página inteira para a mensagem.
	maxErrorBody = 4 << 10
	// maxResponseBody limita o corpo aceito nas leituras.
	maxResponseBody = 1 << 20
)

// ErrNotConfigured indica que a instância não tem as credenciais da API do
// Easypanel. O comando de provisionamento falha com isto em vez de chamar uma
// URL vazia.
var ErrNotConfigured = errors.New("easypanel: EASYPANEL_URL e EASYPANEL_API_KEY precisam estar definidos")

// Config é a configuração do cliente. HTTPClient vazio usa um cliente com
// timeout próprio.
type Config struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// Client fala com a API do Easypanel.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient monta o cliente. Uma configuração vazia é aceita — Configured
// informa se dá para chamar.
func NewClient(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:  strings.TrimSpace(cfg.APIKey),
		http:    httpClient,
	}
}

// Configured informa se há URL e chave para chamar a API.
func (c *Client) Configured() bool {
	return c.baseURL != "" && c.apiKey != ""
}

// APIError é a recusa da API do Easypanel: guarda a operação, o status HTTP e o
// corpo (truncado) para diagnóstico.
type APIError struct {
	Operation  string
	StatusCode int
	Status     string
	Body       string
}

// Error descreve a falha em português, com o corpo quando a API o enviou.
func (e *APIError) Error() string {
	message := fmt.Sprintf("easypanel: %s: HTTP %s", e.Operation, e.Status)
	if e.Body != "" {
		message += ": " + e.Body
	}
	return message
}

// AppServiceRef identifica um serviço já existente no painel.
type AppServiceRef struct {
	Project string
	Service string
	Type    string
}

// Mount é um ponto de montagem de um serviço.
type Mount struct {
	Type      string `json:"type"`
	Name      string `json:"name,omitempty"`
	MountPath string `json:"mountPath"`
	HostPath  string `json:"hostPath,omitempty"`
}

// Domain é um domínio publicado de um serviço.
type Domain struct {
	ID                  string `json:"id"`
	Host                string `json:"host"`
	Path                string `json:"path"`
	HTTPS               bool   `json:"https"`
	CertificateResolver string `json:"certificateResolver"`
}

// ── Chamadas do provisionamento ────────────────────────────────────

// CreateAppService cria o serviço de aplicação a partir da imagem publicada.
// Não constrói nada na VPS: a imagem vem do GHCR (BR-401).
func (c *Client) CreateAppService(ctx context.Context, project, service, image string) error {
	payload := map[string]any{
		"projectName": project,
		"serviceName": service,
		"source":      map[string]any{"type": "image", "image": image},
	}
	return c.post(ctx, "createAppService", payload, nil)
}

// UpdateAppSourceImage troca a imagem do serviço. É o que faz do provisionamento
// também o caminho da atualização canário: um cliente por vez, mesma sequência.
func (c *Client) UpdateAppSourceImage(ctx context.Context, project, service, image string) error {
	payload := map[string]any{"projectName": project, "serviceName": service, "image": image}
	return c.post(ctx, "updateAppSourceImage", payload, nil)
}

// CreateServiceMount monta o volume persistente do serviço (o `/data` da
// instância, onde vive o banco).
func (c *Client) CreateServiceMount(ctx context.Context, project, service string, mount Mount) error {
	if mount.Type == "" {
		mount.Type = "volume"
	}
	payload := map[string]any{
		"projectName": project,
		"serviceName": service,
		"values":      mount,
	}
	return c.post(ctx, "createMount", payload, nil)
}

// UpdateAppEnvironment grava as variáveis de ambiente da instância. O formato é
// o `.env` do Easypanel: uma linha `CHAVE=valor` por variável.
func (c *Client) UpdateAppEnvironment(ctx context.Context, project, service, env string) error {
	payload := map[string]any{
		"projectName": project,
		"serviceName": service,
		"env":         env,
	}
	return c.post(ctx, "updateAppEnv", payload, nil)
}

// CreateDomain publica o subdomínio com HTTPS e certificado do Let's Encrypt. O
// registro DNS continua manual (decisão de operação).
func (c *Client) CreateDomain(ctx context.Context, project, service, host string, port int) error {
	payload := map[string]any{
		"id":                  newDomainID(),
		"host":                host,
		"path":                "/",
		"https":               true,
		"certificateResolver": "letsencrypt",
		"middlewares":         []string{},
		"wildcard":            false,
		"destinationType":     "service",
		"serviceDestination": map[string]any{
			"projectName": project,
			"serviceName": service,
			"port":        port,
			"protocol":    "http",
			"path":        "/",
		},
	}
	return c.post(ctx, "createDomain", payload, nil)
}

// DeployAppService sobe (ou atualiza) o serviço com a imagem configurada.
func (c *Client) DeployAppService(ctx context.Context, project, service string) error {
	payload := map[string]any{"projectName": project, "serviceName": service}
	return c.post(ctx, "deployAppService", payload, nil)
}

// DeleteAppService remove o serviço e os volumes dele: apagar uma instância não
// pode deixar resíduo (BR-402).
func (c *Client) DeleteAppService(ctx context.Context, project, service string) error {
	mounts, err := c.ListMounts(ctx, project, service)
	if err != nil {
		return err
	}
	// Os índices são lidos uma vez e consumidos do maior para o menor: cada
	// remoção reordena a lista no painel.
	for i := len(mounts) - 1; i >= 0; i-- {
		if mounts[i].Type != "" && mounts[i].Type != "volume" {
			continue
		}
		if err := c.DeleteMount(ctx, project, service, i); err != nil {
			return err
		}
	}
	payload := map[string]any{"projectName": project, "serviceName": service}
	return c.post(ctx, "destroyAppService", payload, nil)
}

// ListAppServices devolve os serviços de aplicação de todos os projetos do
// painel. O provisionamento filtra pelo projeto da instalação.
func (c *Client) ListAppServices(ctx context.Context) ([]AppServiceRef, error) {
	body, err := c.get(ctx, "listProjectsAndServices", nil)
	if err != nil {
		return nil, err
	}
	var projects []struct {
		Name     string `json:"name"`
		Services []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"services"`
	}
	if err := decodeList(body, "projects", &projects); err != nil {
		return nil, fmt.Errorf("easypanel: listProjectsAndServices: resposta ilegível: %w", err)
	}

	refs := make([]AppServiceRef, 0, len(projects))
	for _, project := range projects {
		for _, service := range project.Services {
			refs = append(refs, AppServiceRef{Project: project.Name, Service: service.Name, Type: service.Type})
		}
	}
	return refs, nil
}

// ListMounts devolve os pontos de montagem de um serviço.
func (c *Client) ListMounts(ctx context.Context, project, service string) ([]Mount, error) {
	query := url.Values{"projectName": {project}, "serviceName": {service}}
	body, err := c.get(ctx, "listMounts", query)
	if err != nil {
		return nil, err
	}
	var mounts []Mount
	if err := decodeList(body, "mounts", &mounts); err != nil {
		return nil, fmt.Errorf("easypanel: listMounts: resposta ilegível: %w", err)
	}
	return mounts, nil
}

// DeleteMount remove o ponto de montagem de índice `index`.
func (c *Client) DeleteMount(ctx context.Context, project, service string, index int) error {
	payload := map[string]any{"projectName": project, "serviceName": service, "index": index}
	return c.post(ctx, "deleteMount", payload, nil)
}

// ListDomains devolve os domínios publicados de um serviço.
func (c *Client) ListDomains(ctx context.Context, project, service string) ([]Domain, error) {
	query := url.Values{"projectName": {project}, "serviceName": {service}}
	body, err := c.get(ctx, "listDomains", query)
	if err != nil {
		return nil, err
	}
	var domains []Domain
	if err := decodeList(body, "domains", &domains); err != nil {
		return nil, fmt.Errorf("easypanel: listDomains: resposta ilegível: %w", err)
	}
	return domains, nil
}

// GetServiceNotes lê a anotação do serviço — é onde a etiqueta de expiração da
// demo é guardada (ARQUITETURA-DEPLOY.md §9).
func (c *Client) GetServiceNotes(ctx context.Context, project, service string) (string, error) {
	query := url.Values{"projectName": {project}, "serviceName": {service}}
	body, err := c.get(ctx, "getServiceNotes", query)
	if err != nil {
		return "", err
	}
	return decodeNotes(body), nil
}

// SetServiceNotes grava a anotação do serviço.
func (c *Client) SetServiceNotes(ctx context.Context, project, service, notes string) error {
	payload := map[string]any{"projectName": project, "serviceName": service, "notes": notes}
	return c.post(ctx, "setServiceNotes", payload, nil)
}

// ── Transporte ─────────────────────────────────────────────────────

// post envia um corpo JSON e decodifica a resposta em `out` (quando houver).
func (c *Client) post(ctx context.Context, operation string, payload any, out any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("easypanel: %s: montar requisição: %w", operation, err)
	}
	body, err := c.do(ctx, operation, http.MethodPost, "/api/"+operation, encoded)
	if err != nil {
		return err
	}
	return decodeInto(operation, body, out)
}

// get lê um recurso e devolve o corpo cru.
func (c *Client) get(ctx context.Context, operation string, query url.Values) ([]byte, error) {
	path := "/api/" + operation
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return c.do(ctx, operation, http.MethodGet, path, nil)
}

// do executa a chamada e traduz a falha HTTP em *APIError.
func (c *Client) do(ctx context.Context, operation, method, path string, body []byte) ([]byte, error) {
	if !c.Configured() {
		return nil, ErrNotConfigured
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("easypanel: %s: montar requisição: %w", operation, err)
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("easypanel: %s: %w", operation, err)
	}
	defer func() { _ = response.Body.Close() }()

	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("easypanel: %s: ler resposta: %w", operation, err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, &APIError{
			Operation:  operation,
			StatusCode: response.StatusCode,
			Status:     response.Status,
			Body:       strings.TrimSpace(truncate(payload, maxErrorBody)),
		}
	}
	return payload, nil
}

// decodeInto decodifica a resposta quando o chamador espera corpo.
func decodeInto(operation string, body []byte, out any) error {
	if out == nil || len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("easypanel: %s: resposta ilegível: %w", operation, err)
	}
	return nil
}

// decodeList aceita tanto uma lista crua quanto a lista embrulhada na chave que
// a API usa (`{"projects": [...]}`). O contrato da API não é tipado, então a
// leitura tolera as duas formas.
func decodeList(body []byte, key string, out any) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '[' {
		return json.Unmarshal(trimmed, out)
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &wrapper); err != nil {
		return err
	}
	inner, ok := wrapper[key]
	if !ok {
		return nil
	}
	return json.Unmarshal(inner, out)
}

// decodeNotes aceita a anotação como string crua ou embrulhada em `notes`.
func decodeNotes(body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return ""
	}
	if trimmed[0] == '"' {
		var notes string
		if err := json.Unmarshal(trimmed, &notes); err == nil {
			return notes
		}
		return ""
	}
	var wrapper struct {
		Notes string `json:"notes"`
	}
	if err := json.Unmarshal(trimmed, &wrapper); err != nil {
		return ""
	}
	return wrapper.Notes
}

// truncate corta o corpo no limite, marcando o corte.
func truncate(body []byte, limit int) string {
	if len(body) <= limit {
		return string(body)
	}
	return string(body[:limit]) + "…"
}

// newDomainID gera o identificador do domínio no painel (a API exige um).
func newDomainID() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("aluxen%d", time.Now().UnixNano())
	}
	for i := range raw {
		raw[i] = alphabet[int(raw[i])%len(alphabet)]
	}
	return string(raw)
}
