package easypanel_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/infrastructure/easypanel"
)

// ── Servidor falso ─────────────────────────────────────────────────
//
// O painel é simulado por um httptest.Server: cada chamada fica registrada com
// método, caminho, corpo e credencial, e a resposta vem do roteiro do teste.

type panelCall struct {
	Method string
	Path   string
	Query  url.Values
	Body   map[string]any
	Auth   string
}

type fakePanel struct {
	t        *testing.T
	url      string
	calls    []panelCall
	status   int
	response string
	routes   map[string]string
}

func newFakePanel(t *testing.T, status int, response string) *fakePanel {
	t.Helper()
	panel := &fakePanel{t: t, status: status, response: response}
	server := httptest.NewServer(http.HandlerFunc(panel.serve))
	t.Cleanup(server.Close)

	panel.url = server.URL
	return panel
}

func (p *fakePanel) client() *easypanel.Client {
	return easypanel.NewClient(easypanel.Config{
		BaseURL: p.url, APIKey: "chave-de-teste", HTTPClient: &http.Client{Timeout: 5 * time.Second},
	})
}

func (p *fakePanel) serve(writer http.ResponseWriter, request *http.Request) {
	call := panelCall{
		Method: request.Method,
		Path:   request.URL.Path,
		Query:  request.URL.Query(),
		Auth:   request.Header.Get("Authorization"),
	}
	if request.Body != nil {
		_ = json.NewDecoder(request.Body).Decode(&call.Body)
	}
	p.calls = append(p.calls, call)

	status, response := p.status, p.response
	if scripted, ok := p.routes[strings.TrimPrefix(request.URL.Path, "/api/")]; ok {
		status, response = http.StatusOK, scripted
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write([]byte(response))
}

func (p *fakePanel) lastCall() panelCall {
	p.t.Helper()
	require.NotEmpty(p.t, p.calls, "nenhuma chamada chegou ao painel")
	return p.calls[len(p.calls)-1]
}

func (p *fakePanel) operations() []string {
	operations := make([]string, 0, len(p.calls))
	for _, call := range p.calls {
		operations = append(operations, strings.TrimPrefix(call.Path, "/api/"))
	}
	return operations
}

// ── Provisionamento ────────────────────────────────────────────────

func TestCreateAppServiceUsesPublishedImage(t *testing.T) {
	panel := newFakePanel(t, http.StatusOK, "{}")

	err := panel.client().CreateAppService(context.Background(), "aluxen", "aluxen-demo-buzios", "ghcr.io/passoz/aluxen:sha-abc123")
	require.NoError(t, err)

	call := panel.lastCall()
	assert.Equal(t, http.MethodPost, call.Method)
	assert.Equal(t, "/api/createAppService", call.Path)
	assert.Equal(t, "Bearer chave-de-teste", call.Auth)
	assert.Equal(t, "aluxen", call.Body["projectName"])
	assert.Equal(t, "aluxen-demo-buzios", call.Body["serviceName"])
	source, ok := call.Body["source"].(map[string]any)
	require.True(t, ok, "o corpo precisa trazer a fonte do serviço")
	assert.Equal(t, "image", source["type"])
	assert.Equal(t, "ghcr.io/passoz/aluxen:sha-abc123", source["image"])
}

func TestCreateServiceMount(t *testing.T) {
	panel := newFakePanel(t, http.StatusOK, "{}")

	err := panel.client().CreateServiceMount(context.Background(), "aluxen", "aluxen-demo-buzios",
		easypanel.Mount{Type: "volume", Name: "aluxen-demo-buzios-data", MountPath: "/data"})
	require.NoError(t, err)

	call := panel.lastCall()
	assert.Equal(t, "/api/createMount", call.Path)
	values, ok := call.Body["values"].(map[string]any)
	require.True(t, ok, "o corpo precisa trazer o ponto de montagem em values")
	assert.Equal(t, "volume", values["type"])
	assert.Equal(t, "aluxen-demo-buzios-data", values["name"])
	assert.Equal(t, "/data", values["mountPath"])
}

func TestUpdateAppEnvironment(t *testing.T) {
	panel := newFakePanel(t, http.StatusOK, "{}")
	env := easypanel.EnvLines(map[string]string{"DB_DIR": "/data", "SEED_DEMO": "true"})

	err := panel.client().UpdateAppEnvironment(context.Background(), "aluxen", "aluxen-demo-buzios", env)
	require.NoError(t, err)

	call := panel.lastCall()
	assert.Equal(t, "/api/updateAppEnv", call.Path)
	assert.Equal(t, "DB_DIR=/data\nSEED_DEMO=true\n", call.Body["env"])
}

func TestCreateDomainPublishesHTTPSWithLetsEncrypt(t *testing.T) {
	panel := newFakePanel(t, http.StatusOK, "{}")

	err := panel.client().CreateDomain(context.Background(), "aluxen", "aluxen-demo-buzios", "buzios.aluxen.evolucsia.com", 3000)
	require.NoError(t, err)

	call := panel.lastCall()
	assert.Equal(t, "/api/createDomain", call.Path)
	assert.Equal(t, "buzios.aluxen.evolucsia.com", call.Body["host"])
	assert.Equal(t, true, call.Body["https"])
	assert.Equal(t, "letsencrypt", call.Body["certificateResolver"])
	assert.NotEmpty(t, call.Body["id"], "a API exige um id para o domínio")

	destination, ok := call.Body["serviceDestination"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "aluxen", destination["projectName"])
	assert.Equal(t, "aluxen-demo-buzios", destination["serviceName"])
	assert.Equal(t, float64(3000), destination["port"])
}

func TestUpdateAppSourceImage(t *testing.T) {
	panel := newFakePanel(t, http.StatusOK, "{}")

	require.NoError(t, panel.client().UpdateAppSourceImage(context.Background(), "aluxen", "aluxen-passoz", "ghcr.io/passoz/aluxen:sha-novo"))

	call := panel.lastCall()
	assert.Equal(t, "/api/updateAppSourceImage", call.Path)
	assert.Equal(t, "aluxen-passoz", call.Body["serviceName"])
	assert.Equal(t, "ghcr.io/passoz/aluxen:sha-novo", call.Body["image"])
}

func TestDeployAppService(t *testing.T) {
	panel := newFakePanel(t, http.StatusOK, "{}")

	require.NoError(t, panel.client().DeployAppService(context.Background(), "aluxen", "aluxen-demo-buzios"))

	call := panel.lastCall()
	assert.Equal(t, "/api/deployAppService", call.Path)
	assert.Equal(t, "aluxen-demo-buzios", call.Body["serviceName"])
}

func TestListAppServicesReadsProjectsAndServices(t *testing.T) {
	response := `{"projects":[
		{"name":"aluxen","services":[{"type":"app","name":"aluxen-demo-buzios"},{"type":"app","name":"aluxen-passoz"}]},
		{"name":"outro","services":[{"type":"app","name":"outro-app"}]}
	]}`
	panel := newFakePanel(t, http.StatusOK, response)

	services, err := panel.client().ListAppServices(context.Background())
	require.NoError(t, err)

	assert.Equal(t, http.MethodGet, panel.lastCall().Method)
	assert.Equal(t, []easypanel.AppServiceRef{
		{Project: "aluxen", Service: "aluxen-demo-buzios", Type: "app"},
		{Project: "aluxen", Service: "aluxen-passoz", Type: "app"},
		{Project: "outro", Service: "outro-app", Type: "app"},
	}, services)
}

func TestListAppServicesAcceptsBareArray(t *testing.T) {
	panel := newFakePanel(t, http.StatusOK, `[{"name":"aluxen","services":[{"type":"app","name":"aluxen-demo"}]}]`)

	services, err := panel.client().ListAppServices(context.Background())
	require.NoError(t, err)
	require.Len(t, services, 1)
	assert.Equal(t, "aluxen-demo", services[0].Service)
}

func TestDeleteAppServiceRemovesVolumeThenService(t *testing.T) {
	panel := newFakePanel(t, http.StatusOK, "[]")
	panel.routes = map[string]string{
		"listMounts": `[{"type":"volume","name":"aluxen-demo-data","mountPath":"/data"}]`,
	}

	require.NoError(t, panel.client().DeleteAppService(context.Background(), "aluxen", "aluxen-demo-buzios"))

	assert.Equal(t, []string{"listMounts", "deleteMount", "destroyAppService"}, panel.operations())
	assert.Equal(t, float64(0), panel.calls[1].Body["index"], "o volume precisa ser removido pelo índice")
	assert.Equal(t, "aluxen-demo-buzios", panel.calls[2].Body["serviceName"])
}

func TestDeleteAppServiceSkipsNonVolumeMounts(t *testing.T) {
	panel := newFakePanel(t, http.StatusOK, "[]")
	panel.routes = map[string]string{
		"listMounts": `[{"type":"bind","hostPath":"/etc/hosts","mountPath":"/etc/hosts"},{"type":"volume","name":"aluxen-demo-data","mountPath":"/data"}]`,
	}

	require.NoError(t, panel.client().DeleteAppService(context.Background(), "aluxen", "aluxen-demo"))

	assert.Equal(t, []string{"listMounts", "deleteMount", "destroyAppService"}, panel.operations())
	assert.Equal(t, float64(1), panel.calls[1].Body["index"], "só o volume entra na limpeza, pelo índice original")
}

func TestListMountsAndDomainsUseQueryParameters(t *testing.T) {
	panel := newFakePanel(t, http.StatusOK, `[]`)

	_, err := panel.client().ListMounts(context.Background(), "aluxen", "aluxen-demo")
	require.NoError(t, err)
	call := panel.lastCall()
	assert.Equal(t, "/api/listMounts", call.Path)
	assert.Equal(t, "aluxen", call.Query.Get("projectName"))
	assert.Equal(t, "aluxen-demo", call.Query.Get("serviceName"))

	_, err = panel.client().ListDomains(context.Background(), "aluxen", "aluxen-demo")
	require.NoError(t, err)
	assert.Equal(t, "/api/listDomains", panel.lastCall().Path)
}

func TestServiceNotesRoundTrip(t *testing.T) {
	panel := newFakePanel(t, http.StatusOK, `{"notes":"expires_at=2026-10-05"}`)

	require.NoError(t, panel.client().SetServiceNotes(context.Background(), "aluxen", "aluxen-demo", "expires_at=2026-10-05"))
	call := panel.lastCall()
	assert.Equal(t, "/api/setServiceNotes", call.Path)
	assert.Equal(t, "expires_at=2026-10-05", call.Body["notes"])

	notes, err := panel.client().GetServiceNotes(context.Background(), "aluxen", "aluxen-demo")
	require.NoError(t, err)
	assert.Equal(t, "expires_at=2026-10-05", notes)
}

// ── Erros ──────────────────────────────────────────────────────────

func TestHTTPErrorCarriesStatusAndBody(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusInternalServerError} {
		panel := newFakePanel(t, status, `{"message":"nome de serviço inválido"}`)

		err := panel.client().CreateAppService(context.Background(), "aluxen", "Aluxen Demo", "img")
		require.Error(t, err)

		var apiErr *easypanel.APIError
		require.True(t, errors.As(err, &apiErr), "erro HTTP precisa ser *APIError, veio %T", err)
		assert.Equal(t, status, apiErr.StatusCode)
		assert.Equal(t, "createAppService", apiErr.Operation)
		assert.Contains(t, apiErr.Body, "nome de serviço inválido")
		assert.Contains(t, apiErr.Error(), http.StatusText(status))
	}
}

func TestNotConfigured(t *testing.T) {
	client := easypanel.NewClient(easypanel.Config{})
	assert.False(t, client.Configured())

	err := client.DeployAppService(context.Background(), "aluxen", "aluxen-demo")
	assert.ErrorIs(t, err, easypanel.ErrNotConfigured)
}

// ── Ciclo de vida da demo ──────────────────────────────────────────

func TestExpiryLabelRoundTrip(t *testing.T) {
	expiresAt := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	notes := easypanel.EncodeNotes(expiresAt)
	assert.Equal(t, "expires_at=2026-10-05", notes)

	parsed, ok := easypanel.ParseNotes(notes)
	require.True(t, ok)
	assert.Equal(t, "2026-10-05", easypanel.ExpiryDate(parsed))

	_, ok = easypanel.ParseNotes("anotação sem etiqueta")
	assert.False(t, ok, "serviço sem etiqueta não expira")
}

func TestExpiredHonoursHorizon(t *testing.T) {
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	notes := easypanel.EncodeNotes(time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC))

	assert.False(t, easypanel.Expired(notes, now, 0), "ainda não venceu")
	assert.True(t, easypanel.Expired(notes, now, 7*24*time.Hour), "vence dentro da janela de limpeza")
	assert.True(t, easypanel.Expired(easypanel.EncodeNotes(now.AddDate(0, 0, -1)), now, 0), "já venceu")
	assert.False(t, easypanel.Expired("", now, 30*24*time.Hour), "sem etiqueta, nunca")
}

func TestParseExpiryAcceptsAbsoluteAndRelative(t *testing.T) {
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)

	absolute, err := easypanel.ParseExpiry("2026-10-05", now)
	require.NoError(t, err)
	assert.Equal(t, "2026-10-05", easypanel.ExpiryDate(absolute))

	relative, err := easypanel.ParseExpiry("+30d", now)
	require.NoError(t, err)
	assert.Equal(t, "2026-10-21", easypanel.ExpiryDate(relative))

	_, err = easypanel.ParseExpiry("amanhã", now)
	assert.Error(t, err)
}

func TestEnvLinesIsStableAndSorted(t *testing.T) {
	env := easypanel.EnvLines(map[string]string{
		"SESSION_SECRET": "segredo", "DB_DIR": "/data", "APP_URL": "https://buzios.aluxen.evolucsia.com",
	})
	assert.Equal(t, "APP_URL=https://buzios.aluxen.evolucsia.com\nDB_DIR=/data\nSESSION_SECRET=segredo\n", env)
}
