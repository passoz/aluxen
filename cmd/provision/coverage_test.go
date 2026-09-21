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

// ── Painel falso com falha injetada ────────────────────────────────
//
// O painel guarda o estado de cada instância (imagem, variáveis, volume,
// domínio, anotação) e permite escolher qual operação falha e com que status.
// É assim que se prova em que etapa o provisionamento para e o que ele deixa
// para trás quando a API recusa.

type covInstance struct {
	project string
	name    string
	typ     string
	image   string
	env     string
	notes   string
	mounts  []map[string]any
	domains []map[string]any
	deploys int
}

type covPanel struct {
	mu        sync.Mutex
	instances map[string]*covInstance
	order     []string
	fail      map[string]int // operação → status HTTP da recusa
	failFor   map[string]int // "operação/serviço" → recusa só daquele serviço
	calls     []string
}

func newCovPanel(t *testing.T) *covPanel {
	t.Helper()
	panel := &covPanel{
		instances: map[string]*covInstance{},
		fail:      map[string]int{},
		failFor:   map[string]int{},
	}
	server := httptest.NewServer(http.HandlerFunc(panel.serve))
	t.Cleanup(server.Close)

	t.Setenv("EASYPANEL_URL", server.URL)
	t.Setenv("EASYPANEL_API_KEY", "chave-de-teste")
	return panel
}

func covKey(project, service string) string { return project + "/" + service }

func (p *covPanel) instance(project, service string) *covInstance {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.instances[covKey(project, service)]
}

// seed coloca uma instância já provisionada no painel.
func (p *covPanel) seed(project, name, typ, notes, image string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.instances[covKey(project, name)] = &covInstance{
		project: project, name: name, typ: typ, notes: notes, image: image,
		mounts: []map[string]any{{"type": "volume", "name": name + "-data", "mountPath": "/data"}},
	}
	p.order = append(p.order, covKey(project, name))
}

// reject faz a operação falhar com o status informado.
func (p *covPanel) reject(operation string, status int) *covPanel {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fail[operation] = status
	return p
}

// rejectFor faz a operação falhar apenas para o serviço informado.
func (p *covPanel) rejectFor(operation, service string, status int) *covPanel {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failFor[operation+"/"+service] = status
	return p
}

func (p *covPanel) operations() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

func (p *covPanel) serve(writer http.ResponseWriter, request *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var body map[string]any
	if request.Body != nil {
		_ = json.NewDecoder(request.Body).Decode(&body)
	}
	query := request.URL.Query()
	project := covFirstString(body["projectName"], query.Get("projectName"))
	service := covFirstString(body["serviceName"], query.Get("serviceName"))
	if destination, ok := body["serviceDestination"].(map[string]any); ok {
		project = covFirstString(destination["projectName"], project)
		service = covFirstString(destination["serviceName"], service)
	}

	operation := strings.TrimPrefix(request.URL.Path, "/api/")
	p.calls = append(p.calls, operation)
	current := p.instances[covKey(project, service)]

	if status, failed := p.fail[operation]; failed {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(`{"message":"recusado pelo painel"}`))
		return
	}
	if status, failed := p.failFor[operation+"/"+service]; failed {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(`{"message":"recusado pelo painel"}`))
		return
	}

	switch operation {
	case "listProjectsAndServices":
		p.writeJSON(writer, p.projectsPayload())
	case "createAppService":
		source, _ := body["source"].(map[string]any)
		created := &covInstance{
			project: project, name: service, typ: "app",
			image: covFirstString(source["image"]), mounts: []map[string]any{},
		}
		p.instances[covKey(project, service)] = created
		p.order = append(p.order, covKey(project, service))
		p.writeJSON(writer, map[string]any{})
	case "updateAppSourceImage":
		current.image = covFirstString(body["image"])
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
		current.env = covFirstString(body["env"])
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
		delete(p.instances, covKey(project, service))
		p.writeJSON(writer, map[string]any{})
	case "setServiceNotes":
		current.notes = covFirstString(body["notes"])
		p.writeJSON(writer, map[string]any{})
	case "getServiceNotes":
		p.writeJSON(writer, map[string]any{"notes": current.notes})
	default:
		writer.WriteHeader(http.StatusNotFound)
		p.writeJSON(writer, map[string]any{"message": "rota não simulada"})
	}
}

func (p *covPanel) projectsPayload() map[string]any {
	byProject := map[string][]map[string]any{}
	for _, key := range p.order {
		instance := p.instances[key]
		if instance == nil {
			continue
		}
		byProject[instance.project] = append(byProject[instance.project],
			map[string]any{"type": instance.typ, "name": instance.name})
	}
	projects := make([]map[string]any, 0, len(byProject))
	for project, services := range byProject {
		projects = append(projects, map[string]any{"name": project, "services": services})
	}
	return map[string]any{"projects": projects}
}

func (p *covPanel) writeJSON(writer http.ResponseWriter, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(payload)
}

func covFirstString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			return text
		}
	}
	return ""
}

// covCommand executa o comando e devolve a saída e o erro.
func covCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(args, &out)
	return out.String(), err
}

func covEnvOf(t *testing.T, instance *covInstance) map[string]string {
	t.Helper()
	require.NotNil(t, instance, "a instância precisa existir no painel")
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(instance.env), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			values[key] = value
		}
	}
	return values
}

// covCreateArgs são os argumentos mínimos de um create válido.
func covCreateArgs(name string) []string {
	return []string{"create", "--name", name, "--domain", name + ".aluxen.local",
		"--image", "ghcr.io/passoz/aluxen:sha-cov"}
}

// ── Parsing e ajuda ────────────────────────────────────────────────

// Sem subcomando o operador recebe o uso; `help`, `-h` e `--help` imprimem o
// mesmo texto sem tocar no painel.
func TestCovRunPrintsUsage(t *testing.T) {
	out, err := covCommand(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Subcomandos:")
	assert.Empty(t, out, "o uso vai pelo erro para não se perder no meio da saída")

	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			out, err := covCommand(t, arg)
			require.NoError(t, err)
			assert.Contains(t, out, "Subcomandos:")
			assert.Contains(t, out, "--seed-demo")
		})
	}
}

// Flag desconhecida em qualquer subcomando é erro de argumento, não de rede: o
// painel não pode nem ser consultado sem uma linha de comando válida.
func TestCovSubcommandsRejectUnknownFlags(t *testing.T) {
	for _, subcommand := range []string{"create", "list", "expire"} {
		t.Run(subcommand, func(t *testing.T) {
			_, err := covCommand(t, subcommand, "--nome", "x")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "nome")
			assert.NotErrorIs(t, err, easypanel.ErrNotConfigured)
		})
	}
}

// O create exige o diretório do banco e recusa uma etiqueta de expiração que não
// sabe ler, antes de falar com o painel.
func TestCovCreateValidatesItsOwnArguments(t *testing.T) {
	t.Run("db-dir vazio", func(t *testing.T) {
		out, err := covCommand(t, append(covCreateArgs("aluxen-demo"), "--db-dir", "")...)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--db-dir não pode ser vazio")
		assert.Empty(t, out)
	})

	t.Run("etiqueta inválida", func(t *testing.T) {
		_, err := covCommand(t, append(covCreateArgs("aluxen-demo"), "--expires-at", "+xd")...)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prazo relativo inválido")
	})
}

// ── Ambiente repassado ─────────────────────────────────────────────

// As variáveis do operador definidas no ambiente vão para a instância; as que
// não estão definidas não são inventadas.
func TestCovCreatePassesOperatorEnvironmentThrough(t *testing.T) {
	panel := newCovPanel(t)
	t.Setenv("AI_API_KEY", "chave-da-ia")
	t.Setenv("WAHA_SESSION", "aluxen-demo")
	t.Setenv("STORAGE_DIR", "")

	out, err := covCommand(t, covCreateArgs("aluxen-demo")...)
	require.NoError(t, err, out)

	env := covEnvOf(t, panel.instance("aluxen", "aluxen-demo"))
	assert.Equal(t, "chave-da-ia", env["AI_API_KEY"])
	assert.Equal(t, "aluxen-demo", env["WAHA_SESSION"])
	assert.NotContains(t, env, "STORAGE_DIR", "variável vazia não entra no ambiente da instância")
	assert.NotContains(t, env, "WAHA_API_KEY", "o provisionamento não inventa segredo")
}

// ── Falha em cada etapa ────────────────────────────────────────────

// Uma recusa da API interrompe a sequência na etapa onde aconteceu: o relatório
// diz qual passo falhou e os passos seguintes não acontecem.
func TestCovCreateStopsAtTheFailingStep(t *testing.T) {
	cases := []struct {
		name       string
		operation  string
		seed       bool
		extraArgs  []string
		wantLabel  string
		wantBefore []string
	}{
		{name: "listar serviços", operation: "listProjectsAndServices", wantLabel: "listar serviços do painel"},
		{name: "convergir imagem", operation: "updateAppSourceImage", seed: true,
			wantLabel: "imagem ghcr.io/passoz/aluxen:sha-cov"},
		{name: "criar serviço", operation: "createAppService", wantLabel: "serviço de aplicação aluxen-demo"},
		{name: "listar volumes", operation: "listMounts", wantLabel: "listar volumes"},
		{name: "criar volume", operation: "createMount", wantLabel: "volume aluxen-demo-data"},
		{name: "gravar variáveis", operation: "updateAppEnv", wantLabel: "variáveis da instância"},
		{name: "listar domínios", operation: "listDomains", wantLabel: "listar domínios"},
		{name: "criar domínio", operation: "createDomain", wantLabel: "domínio aluxen-demo.aluxen.local"},
		{name: "deploy", operation: "deployAppService", wantLabel: "deploy"},
		{name: "gravar etiqueta", operation: "setServiceNotes", extraArgs: []string{"--expires-at", "2026-10-05"},
			wantLabel: "etiqueta de expiração 2026-10-05"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			panel := newCovPanel(t).reject(testCase.operation, http.StatusInternalServerError)
			if testCase.seed {
				panel.seed("aluxen", "aluxen-demo", "app", "", "ghcr.io/passoz/aluxen:sha-velho")
			}

			out, err := covCommand(t, append(covCreateArgs("aluxen-demo"), testCase.extraArgs...)...)
			require.Error(t, err, out)

			var apiErr *easypanel.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
			assert.Contains(t, out, "[falhou")
			assert.Contains(t, out, testCase.wantLabel)
			assert.Contains(t, out, "provisionamento interrompido")
			assert.Contains(t, out, "recusado pelo painel")

			operations := panel.operations()
			assert.Contains(t, operations, testCase.operation)
			switch testCase.operation {
			case "listProjectsAndServices":
				assert.Equal(t, []string{"listProjectsAndServices"}, operations, "nada acontece depois da primeira falha")
			case "createMount", "updateAppEnv", "createDomain", "deployAppService":
				assert.NotContains(t, operations, "destroyAppService")
			}
			if testCase.operation == "deployAppService" {
				assert.NotContains(t, operations, "setServiceNotes")
			}
		})
	}
}

// O relatório do create aponta o próximo passo manual (o DNS) quando tudo
// passa, e as etapas que já existiam aparecem como "já existia".
func TestCovCreateReportsEveryStepAndTheNextManualStep(t *testing.T) {
	panel := newCovPanel(t)

	out, err := covCommand(t, append(covCreateArgs("aluxen-demo"), "--seed-demo", "--expires-at", "2026-10-05")...)
	require.NoError(t, err, out)

	for _, want := range []string{
		"projeto:   aluxen", "instância: aluxen-demo", "ghcr.io/passoz/aluxen:sha-cov",
		"demo:      sim", "expira em: 2026-10-05", "aponte o DNS de aluxen-demo.aluxen.local",
	} {
		assert.Contains(t, out, want)
	}

	out, err = covCommand(t, append(covCreateArgs("aluxen-demo"), "--seed-demo", "--expires-at", "2026-10-05")...)
	require.NoError(t, err, out)
	assert.Contains(t, out, "já existia", "a segunda execução converge a instância que já existe")

	instance := panel.instance("aluxen", "aluxen-demo")
	require.NotNil(t, instance)
	assert.Len(t, instance.mounts, 1, "o volume de uma instância existente não é duplicado")
	assert.Len(t, instance.domains, 1, "o domínio de uma instância existente não é duplicado")
	assert.Equal(t, "expires_at=2026-10-05", instance.notes)
	assert.Equal(t, "true", covEnvOf(t, instance)["SEED_DEMO"])
	assert.Equal(t, 2, instance.deploys, "a convergência ainda sobe a instância")
}

// ── list ───────────────────────────────────────────────────────────

// A listagem mostra só os serviços de aplicação do projeto: o banco do painel e
// as instâncias de outros projetos ficam fora.
func TestCovListShowsOnlyTheApplicationServicesOfTheProject(t *testing.T) {
	panel := newCovPanel(t)
	panel.seed("aluxen", "aluxen-demo", "app", "expires_at=2999-01-01", "img")
	panel.seed("aluxen", "aluxen-banco", "postgres", "", "postgres:17")
	panel.seed("outro", "aluxen-cliente", "app", "expires_at=2020-01-01", "img")

	out, err := covCommand(t, "list")
	require.NoError(t, err, out)

	assert.Contains(t, out, "aluxen-demo")
	assert.Contains(t, out, "vigente")
	assert.NotContains(t, out, "aluxen-banco", "serviço que não é aplicação não é uma instância")
	assert.NotContains(t, out, "aluxen-cliente", "instância de outro projeto não aparece")
}

// A falha do painel é relatada com o serviço cuja etiqueta não pôde ser lida.
func TestCovListReportsPanelFailures(t *testing.T) {
	t.Run("listar serviços", func(t *testing.T) {
		panel := newCovPanel(t).reject("listProjectsAndServices", http.StatusUnauthorized)

		_, err := covCommand(t, "list")
		require.Error(t, err)
		assert.Equal(t, []string{"listProjectsAndServices"}, panel.operations(),
			"a listagem para na primeira chamada recusada")
		var apiErr *easypanel.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	})

	t.Run("ler etiqueta", func(t *testing.T) {
		panel := newCovPanel(t).reject("getServiceNotes", http.StatusInternalServerError)
		panel.seed("aluxen", "aluxen-demo", "app", "expires_at=2999-01-01", "img")

		_, err := covCommand(t, "list")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ler etiqueta de aluxen-demo")
	})
}

// Sem as credenciais do painel, nem list nem expire chamam a API.
func TestCovCommandsWithoutPanelCredentials(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"expire"}} {
		t.Run(args[0], func(t *testing.T) {
			t.Setenv("EASYPANEL_URL", "")
			t.Setenv("EASYPANEL_API_KEY", "")

			_, err := covCommand(t, args...)
			assert.ErrorIs(t, err, easypanel.ErrNotConfigured)
		})
	}
}

// ── expire ─────────────────────────────────────────────────────────

// Os argumentos do expire são validados antes de qualquer chamada: dias
// negativos e prazo ilegível são erro do operador.
func TestCovExpireValidatesItsArguments(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"dias negativos", []string{"expire", "--days", "-1"}, "--days não pode ser negativo"},
		{"prazo ilegível", []string{"expire", "--expires-at", "amanhã"}, "data de expiração inválida"},
		{"prazo relativo ilegível", []string{"expire", "--expires-at", "+xd"}, "prazo relativo inválido"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := covCommand(t, testCase.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.want)
		})
	}
}

// `--expires-at` limpa o que vence até o fim da data informada, e só isso.
func TestCovExpireHonoursTheInformedDeadline(t *testing.T) {
	panel := newCovPanel(t)
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	panel.seed("aluxen", "aluxen-demo-amanha", "app", "expires_at="+tomorrow, "img")
	panel.seed("aluxen", "aluxen-demo-distante", "app",
		"expires_at="+time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02"), "img")

	out, err := covCommand(t, "expire", "--expires-at", tomorrow, "--dry-run")
	require.NoError(t, err, out)

	assert.Contains(t, out, "removeria aluxen-demo-amanha")
	assert.NotContains(t, out, "aluxen-demo-distante")
	assert.NotNil(t, panel.instance("aluxen", "aluxen-demo-amanha"), "--dry-run não remove nada")
}

// A falha ao remover uma instância não impede a limpeza das outras, é relatada
// no fim e volta como erro do comando.
func TestCovExpireReportsRemovalFailuresAndKeepsCleaning(t *testing.T) {
	panel := newCovPanel(t)
	panel.seed("aluxen", "aluxen-demo-quebrada", "app", "expires_at=2020-01-01", "img")
	panel.seed("aluxen", "aluxen-demo-vencida", "app", "expires_at=2020-01-01", "img")
	panel.rejectFor("destroyAppService", "aluxen-demo-quebrada", http.StatusInternalServerError)

	out, err := covCommand(t, "expire", "--days", "0")
	require.Error(t, err, out)

	assert.Contains(t, out, "falhou ao remover aluxen-demo-quebrada: easypanel: destroyAppService: HTTP 500")
	assert.Contains(t, out, "recusado pelo painel")
	assert.Contains(t, out, "removido aluxen-demo-vencida (app e volume)")
	assert.Contains(t, out, "1 instância(s) removida(s)")
	assert.Contains(t, err.Error(), "remover aluxen-demo-quebrada")
	assert.Nil(t, panel.instance("aluxen", "aluxen-demo-vencida"))
	require.NotNil(t, panel.instance("aluxen", "aluxen-demo-quebrada"),
		"a instância que o painel recusou remover continua no painel")
}

// A leitura dos serviços falha antes de qualquer remoção.
func TestCovExpireReportsPanelFailureBeforeRemoving(t *testing.T) {
	panel := newCovPanel(t).reject("listProjectsAndServices", http.StatusServiceUnavailable)
	panel.seed("aluxen", "aluxen-demo-vencida", "app", "expires_at=2020-01-01", "img")

	_, err := covCommand(t, "expire", "--days", "0")
	require.Error(t, err)
	assert.NotContains(t, panel.operations(), "destroyAppService")
	assert.NotNil(t, panel.instance("aluxen", "aluxen-demo-vencida"))

	var apiErr *easypanel.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
}

// Um serviço sem etiqueta nunca entra na limpeza, nem com janela de um ano.
func TestCovExpireNeverRemovesInstancesWithoutALabel(t *testing.T) {
	panel := newCovPanel(t)
	panel.seed("aluxen", "aluxen-passoz", "app", "", "img")

	out, err := covCommand(t, "expire", "--days", "3650")
	require.NoError(t, err, out)

	assert.Contains(t, out, "0 instância(s) removida(s)")
	assert.NotNil(t, panel.instance("aluxen", "aluxen-passoz"))
}

// ── Apoio ──────────────────────────────────────────────────────────

// O segredo gerado para uma instância sem `--session-secret` são 32 bytes em
// hexadecimal, diferentes a cada chamada — nenhuma instância nasce com o mesmo
// segredo de sessão de outra.
func TestCovNewSecretIsRandomHex(t *testing.T) {
	first, err := newSecret()
	require.NoError(t, err)
	second, err := newSecret()
	require.NoError(t, err)

	assert.Regexp(t, "^[0-9a-f]{64}$", first)
	assert.NotEqual(t, first, second)
}
