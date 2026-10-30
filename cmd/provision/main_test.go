package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/passoz/aluxen/internal/infrastructure/easypanel"
)

// ── Painel falso ───────────────────────────────────────────────────
//
// O provisionamento é testado contra um painel em memória: as chamadas são as
// mesmas do Easypanel e o estado sobrevive entre execuções, que é o que prova a
// idempotência e a remoção sem resíduo.

type fakeService struct {
	project string
	name    string
	image   string
	env     string
	notes   string
	mounts  []map[string]any
	domains []map[string]any
	deploys int
}

type fakePanel struct {
	mu       sync.Mutex
	services map[string]*fakeService
	order    []string
}

func newFakePanel(t *testing.T) *fakePanel {
	t.Helper()
	panel := &fakePanel{services: map[string]*fakeService{}}
	server := httptest.NewServer(http.HandlerFunc(panel.serve))
	t.Cleanup(server.Close)

	t.Setenv("EASYPANEL_URL", server.URL)
	t.Setenv("EASYPANEL_API_KEY", "chave-de-teste")
	return panel
}

func (p *fakePanel) key(project, service string) string { return project + "/" + service }

func (p *fakePanel) get(project, service string) *fakeService {
	return p.services[p.key(project, service)]
}

func (p *fakePanel) serve(writer http.ResponseWriter, request *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var body map[string]any
	if request.Body != nil {
		_ = json.NewDecoder(request.Body).Decode(&body)
	}
	query := request.URL.Query()
	project := firstString(body["projectName"], query.Get("projectName"))
	service := firstString(body["serviceName"], query.Get("serviceName"))
	// createDomain identifica o serviço no destino, não no topo do corpo.
	if destination, ok := body["serviceDestination"].(map[string]any); ok {
		project = firstString(destination["projectName"], project)
		service = firstString(destination["serviceName"], service)
	}
	current := p.get(project, service)

	switch strings.TrimPrefix(request.URL.Path, "/api/") {
	case "listProjectsAndServices":
		p.writeJSON(writer, p.projectsPayload())
	case "createAppService":
		source, _ := body["source"].(map[string]any)
		created := &fakeService{project: project, name: service, image: firstString(source["image"])}
		p.services[p.key(project, service)] = created
		p.order = append(p.order, p.key(project, service))
		p.writeJSON(writer, map[string]any{})
	case "updateAppSourceImage":
		current.image = firstString(body["image"])
		p.writeJSON(writer, map[string]any{})
	case "createMount":
		values, _ := body["values"].(map[string]any)
		current.mounts = append(current.mounts, values)
		p.writeJSON(writer, map[string]any{})
	case "deleteMount":
		index := int(body["index"].(float64))
		current.mounts = append(current.mounts[:index], current.mounts[index+1:]...)
		p.writeJSON(writer, map[string]any{})
	case "listMounts":
		p.writeJSON(writer, current.mounts)
	case "updateAppEnv":
		current.env = firstString(body["env"])
		p.writeJSON(writer, map[string]any{})
	case "createDomain":
		current.domains = append(current.domains, body)
		p.writeJSON(writer, map[string]any{})
	case "listDomains":
		p.writeJSON(writer, current.domains)
	case "deployAppService":
		current.deploys++
		p.writeJSON(writer, map[string]any{})
	case "destroyAppService":
		delete(p.services, p.key(project, service))
		p.writeJSON(writer, map[string]any{})
	case "setServiceNotes":
		current.notes = firstString(body["notes"])
		p.writeJSON(writer, map[string]any{})
	case "getServiceNotes":
		p.writeJSON(writer, map[string]any{"notes": current.notes})
	default:
		writer.WriteHeader(http.StatusNotFound)
		p.writeJSON(writer, map[string]any{"message": "rota não simulada"})
	}
}

// projectsPayload monta a resposta de listProjectsAndServices agrupada por projeto.
func (p *fakePanel) projectsPayload() map[string]any {
	byProject := map[string][]map[string]any{}
	for _, key := range p.order {
		service := p.services[key]
		if service == nil {
			continue
		}
		byProject[service.project] = append(byProject[service.project], map[string]any{"type": "app", "name": service.name})
	}
	projects := make([]map[string]any, 0, len(byProject))
	for project, services := range byProject {
		projects = append(projects, map[string]any{"name": project, "services": services})
	}
	return map[string]any{"projects": projects}
}

func (p *fakePanel) writeJSON(writer http.ResponseWriter, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(payload)
}

func firstString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			return text
		}
	}
	return ""
}

// runProvision executa o comando e devolve a saída.
func runProvision(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(args, &out)
	return out.String(), err
}

// ── create ─────────────────────────────────────────────────────────

func TestCreateProvisionsInstanceEndToEnd(t *testing.T) {
	panel := newFakePanel(t)

	out, err := runProvision(t, "create",
		"--name", "aluxen-demo-buzios",
		"--domain", "buzios.aluxen.evolucsia.com",
		"--image", "ghcr.io/passoz/aluxen:sha-abc123",
		"--db-dir", "/data",
		"--seed-password", "senha-teste",
		"--seed-demo",
		"--expires-at", "2026-10-05",
	)
	require.NoError(t, err, out)

	instance := panel.get("aluxen", "aluxen-demo-buzios")
	require.NotNil(t, instance, "o serviço precisa existir no painel")
	assert.Equal(t, "ghcr.io/passoz/aluxen:sha-abc123", instance.image)
	require.Len(t, instance.mounts, 1)
	assert.Equal(t, "/data", instance.mounts[0]["mountPath"])
	assert.Equal(t, "volume", instance.mounts[0]["type"])

	env := parseEnv(t, instance.env)
	assert.Equal(t, "/data", env["DB_DIR"])
	assert.Equal(t, "true", env["SEED_DEMO"], "a demo sobe já com dados de demonstração")
	assert.Equal(t, "https://buzios.aluxen.evolucsia.com", env["APP_URL"])
	assert.Equal(t, "3000", env["PORT"])
	assert.NotEmpty(t, env["SESSION_SECRET"], "sem segredo informado, o comando gera um")
	assert.NotContains(t, instance.env, "DATABASE_URL", "a instância nasce SQLite no volume")

	require.Len(t, instance.domains, 1)
	assert.Equal(t, "buzios.aluxen.evolucsia.com", instance.domains[0]["host"])
	assert.Equal(t, true, instance.domains[0]["https"])
	assert.Equal(t, "letsencrypt", instance.domains[0]["certificateResolver"])
	assert.Equal(t, 1, instance.deploys)
	assert.Equal(t, "expires_at=2026-10-05", instance.notes)

	assert.Contains(t, out, "[criado")
	assert.Contains(t, out, "aponte o DNS de buzios.aluxen.evolucsia.com")
}

func TestCreateIsIdempotentForSameName(t *testing.T) {
	panel := newFakePanel(t)
	args := []string{"create", "--name", "aluxen-demo-arraial", "--domain", "arraial.aluxen.evolucsia.com",
		"--image", "ghcr.io/passoz/aluxen:sha-abc123", "--seed-password", "senha-teste"}

	first, err := runProvision(t, args...)
	require.NoError(t, err, first)
	second, err := runProvision(t, args...)
	require.NoError(t, err, second)

	instance := panel.get("aluxen", "aluxen-demo-arraial")
	require.NotNil(t, instance)
	assert.Len(t, instance.mounts, 1, "o volume não pode ser duplicado")
	assert.Len(t, instance.domains, 1, "o domínio não pode ser duplicado")
	assert.Equal(t, 2, instance.deploys, "a segunda execução ainda sobe a instância")

	assert.Contains(t, first, "[criado")
	assert.Contains(t, second, "[já existia")
}

func TestCreateConvergesImageOnRerun(t *testing.T) {
	panel := newFakePanel(t)
	base := []string{"create", "--name", "aluxen-cliente-x", "--domain", "x.aluxen.evolucsia.com", "--seed-password", "senha-teste"}

	_, err := runProvision(t, append(base, "--image", "ghcr.io/passoz/aluxen:sha-velho")...)
	require.NoError(t, err)

	out, err := runProvision(t, append(base, "--image", "ghcr.io/passoz/aluxen:sha-novo")...)
	require.NoError(t, err)

	assert.Equal(t, "ghcr.io/passoz/aluxen:sha-novo", panel.get("aluxen", "aluxen-cliente-x").image,
		"a reexecução ajusta a imagem: é o caminho da atualização canário")
	assert.Contains(t, out, "[ajustada")
}

func TestCreateRequiresNameDomainAndImage(t *testing.T) {
	newFakePanel(t)

	_, err := runProvision(t, "create", "--name", "aluxen-demo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--name, --domain e --image")
}

func TestCreateWithoutPanelCredentials(t *testing.T) {
	t.Setenv("EASYPANEL_URL", "")
	t.Setenv("EASYPANEL_API_KEY", "")

	_, err := runProvision(t, "create", "--name", "aluxen-demo", "--domain", "d.aluxen.local", "--image", "img", "--seed-password", "senha-teste")
	assert.ErrorIs(t, err, easypanel.ErrNotConfigured)
}

// ── list e expire ──────────────────────────────────────────────────

func TestListShowsExpiryLabel(t *testing.T) {
	panel := newFakePanel(t)
	seedService(t, panel, "aluxen", "aluxen-demo-buzios", "expires_at=2020-01-01")
	seedService(t, panel, "aluxen", "aluxen-demo-arraial", "expires_at=2999-01-01")
	seedService(t, panel, "aluxen", "aluxen-passoz", "")

	out, err := runProvision(t, "list")
	require.NoError(t, err, out)
	assert.Contains(t, out, "SERVIÇO")
	assert.Contains(t, out, "aluxen-demo-buzios")
	assert.Contains(t, out, "vencida")
	assert.Contains(t, out, "vigente")
	assert.Contains(t, out, "sem prazo")
}

func TestExpireRemovesOnlyDueDemos(t *testing.T) {
	panel := newFakePanel(t)
	seedService(t, panel, "aluxen", "aluxen-demo-vencida", "expires_at=2020-01-01")
	seedService(t, panel, "aluxen", "aluxen-demo-vigente", "expires_at=2999-01-01")
	seedService(t, panel, "aluxen", "aluxen-passoz", "")

	out, err := runProvision(t, "expire", "--days", "0")
	require.NoError(t, err, out)

	assert.Nil(t, panel.get("aluxen", "aluxen-demo-vencida"), "a demo vencida sai do painel com o volume")
	assert.NotNil(t, panel.get("aluxen", "aluxen-demo-vigente"), "a demo vigente fica")
	assert.NotNil(t, panel.get("aluxen", "aluxen-passoz"), "serviço sem etiqueta nunca é removido")
	assert.Contains(t, out, "removido aluxen-demo-vencida")
	assert.Contains(t, out, "1 instância(s) removida(s)")
}

func TestExpireDryRunRemovesNothing(t *testing.T) {
	panel := newFakePanel(t)
	seedService(t, panel, "aluxen", "aluxen-demo-vencida", "expires_at=2020-01-01")

	out, err := runProvision(t, "expire", "--dry-run")
	require.NoError(t, err, out)

	assert.NotNil(t, panel.get("aluxen", "aluxen-demo-vencida"))
	assert.Contains(t, out, "removeria aluxen-demo-vencida")
	assert.Contains(t, out, "nada foi removido")
}

func TestExpireHorizonAnticipatesCleanup(t *testing.T) {
	panel := newFakePanel(t)
	seedService(t, panel, "aluxen", "aluxen-demo-breve", "expires_at="+futureDate(3))

	out, err := runProvision(t, "expire", "--days", "0")
	require.NoError(t, err, out)
	assert.NotNil(t, panel.get("aluxen", "aluxen-demo-breve"), "sem janela, o que ainda não venceu fica")

	out, err = runProvision(t, "expire", "--days", "7", "--dry-run")
	require.NoError(t, err, out)
	assert.Contains(t, out, "removeria aluxen-demo-breve", "a janela de 7 dias antecipa a limpeza")
}

func TestExpireRejectsBothFlags(t *testing.T) {
	newFakePanel(t)

	_, err := runProvision(t, "expire", "--days", "3", "--expires-at", "2026-10-05")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "não os dois")
}

func TestUnknownSubcommandShowsUsage(t *testing.T) {
	_, err := runProvision(t, "voar")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subcomando desconhecido")
}

// ── Apoio ──────────────────────────────────────────────────────────

// seedService coloca um serviço já provisionado no painel falso.
func seedService(t *testing.T, panel *fakePanel, project, name, notes string) {
	t.Helper()
	panel.mu.Lock()
	defer panel.mu.Unlock()
	panel.services[panel.key(project, name)] = &fakeService{
		project: project, name: name, image: "img", notes: notes,
		mounts: []map[string]any{{"type": "volume", "name": name + "-data", "mountPath": "/data"}},
	}
	panel.order = append(panel.order, panel.key(project, name))
}

// parseEnv lê o `.env` devolvido pelo painel.
func parseEnv(t *testing.T, env string) map[string]string {
	t.Helper()
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(env), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		values[key] = value
	}
	return values
}

// futureDate devolve a data de hoje mais N dias no formato da etiqueta.
func futureDate(days int) string {
	return time.Now().UTC().AddDate(0, 0, days).Format("2006-01-02")
}
