package easypanel_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
// O painel responde às mesmas rotas do Easypanel e deixa o teste escolher a
// resposta de cada operação: é assim que se prova o que o cliente faz quando a
// API recusa por credencial, por permissão ou com um corpo ilegível.

type covCall struct {
	Operation string
	Body      map[string]any
}

type covReply struct {
	status int
	body   string
}

type covPanel struct {
	mu     sync.Mutex
	calls  []covCall
	routes map[string]covReply
	server *httptest.Server
}

func newCovPanel(t *testing.T) *covPanel {
	t.Helper()
	panel := &covPanel{routes: map[string]covReply{}}
	panel.server = httptest.NewServer(http.HandlerFunc(panel.serve))
	t.Cleanup(panel.server.Close)
	return panel
}

// reply roteiriza a resposta de uma operação do painel.
func (p *covPanel) reply(operation string, status int, body string) *covPanel {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.routes[operation] = covReply{status: status, body: body}
	return p
}

func (p *covPanel) serve(writer http.ResponseWriter, request *http.Request) {
	operation := strings.TrimPrefix(request.URL.Path, "/api/")
	call := covCall{Operation: operation}
	if request.Body != nil {
		_ = json.NewDecoder(request.Body).Decode(&call.Body)
	}

	p.mu.Lock()
	p.calls = append(p.calls, call)
	reply, scripted := p.routes[operation]
	p.mu.Unlock()

	if !scripted {
		reply = covReply{status: http.StatusOK, body: "{}"}
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(reply.status)
	_, _ = io.WriteString(writer, reply.body)
}

func (p *covPanel) client() *easypanel.Client {
	return easypanel.NewClient(easypanel.Config{
		BaseURL: p.server.URL, APIKey: "chave-de-teste", HTTPClient: &http.Client{Timeout: 5 * time.Second},
	})
}

func (p *covPanel) operations() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	operations := make([]string, 0, len(p.calls))
	for _, call := range p.calls {
		operations = append(operations, call.Operation)
	}
	return operations
}

// covMountPayload devolve o ponto de montagem enviado em `values`.
func covMountPayload(t *testing.T, panel *covPanel) map[string]any {
	t.Helper()
	panel.mu.Lock()
	defer panel.mu.Unlock()
	require.NotEmpty(t, panel.calls, "nenhuma chamada chegou ao painel")
	values, ok := panel.calls[len(panel.calls)-1].Body["values"].(map[string]any)
	require.True(t, ok, "o corpo precisa trazer o ponto de montagem em values")
	return values
}

// ── Corpo enviado ──────────────────────────────────────────────────

// Sem tipo declarado o ponto de montagem é um volume: é o `/data` onde vive o
// banco da instância.
func TestCovCreateServiceMountDefaultsToVolume(t *testing.T) {
	panel := newCovPanel(t)

	err := panel.client().CreateServiceMount(context.Background(), "aluxen", "aluxen-demo",
		easypanel.Mount{Name: "aluxen-demo-data", MountPath: "/data"})
	require.NoError(t, err)

	values := covMountPayload(t, panel)
	assert.Equal(t, "volume", values["type"])
	assert.Equal(t, "aluxen-demo-data", values["name"])
	assert.Equal(t, "/data", values["mountPath"])
}

// ── Recusa da API ──────────────────────────────────────────────────

// Toda chamada de leitura devolve a recusa da API com status e corpo: é o que o
// operador precisa ver quando a chave não tem permissão.
func TestCovReadsReportHTTPRefusal(t *testing.T) {
	const body = `{"message":"chave sem permissão"}`

	cases := []struct {
		name  string
		route string
		call  func(context.Context, *easypanel.Client) error
	}{
		{"listProjectsAndServices", "listProjectsAndServices", func(ctx context.Context, client *easypanel.Client) error {
			_, err := client.ListAppServices(ctx)
			return err
		}},
		{"listMounts", "listMounts", func(ctx context.Context, client *easypanel.Client) error {
			_, err := client.ListMounts(ctx, "aluxen", "aluxen-demo")
			return err
		}},
		{"listDomains", "listDomains", func(ctx context.Context, client *easypanel.Client) error {
			_, err := client.ListDomains(ctx, "aluxen", "aluxen-demo")
			return err
		}},
		{"getServiceNotes", "getServiceNotes", func(ctx context.Context, client *easypanel.Client) error {
			_, err := client.GetServiceNotes(ctx, "aluxen", "aluxen-demo")
			return err
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			panel := newCovPanel(t).reply(testCase.route, http.StatusUnauthorized, body)

			err := testCase.call(context.Background(), panel.client())
			require.Error(t, err)

			var apiErr *easypanel.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
			assert.Equal(t, testCase.route, apiErr.Operation)
			assert.Contains(t, apiErr.Error(), "401")
			assert.Contains(t, apiErr.Error(), "chave sem permissão")
		})
	}
}

// Uma resposta ilegível não vira lista vazia: a leitura falha alto, porque uma
// instância "inexistente" por resposta corrompida recriaria recursos.
func TestCovReadsRejectUnreadableBodies(t *testing.T) {
	cases := []struct {
		name  string
		route string
		call  func(context.Context, *easypanel.Client) error
	}{
		{"listProjectsAndServices", "listProjectsAndServices", func(ctx context.Context, client *easypanel.Client) error {
			_, err := client.ListAppServices(ctx)
			return err
		}},
		{"listMounts", "listMounts", func(ctx context.Context, client *easypanel.Client) error {
			_, err := client.ListMounts(ctx, "aluxen", "aluxen-demo")
			return err
		}},
		{"listDomains", "listDomains", func(ctx context.Context, client *easypanel.Client) error {
			_, err := client.ListDomains(ctx, "aluxen", "aluxen-demo")
			return err
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			panel := newCovPanel(t).reply(testCase.route, http.StatusOK, "\"texto no lugar do objeto\"")

			err := testCase.call(context.Background(), panel.client())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "resposta ilegível")
			assert.Contains(t, err.Error(), testCase.route)
		})
	}
}

// As leituras toleram as duas formas que a API usa (lista crua ou embrulhada) e
// tratam resposta vazia ou sem a chave esperada como lista vazia.
func TestCovReadsTolerateEmptyAndUnexpectedBodies(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		wants func(t *testing.T, mounts []easypanel.Mount, err error)
	}{
		{"corpo vazio", "", func(t *testing.T, mounts []easypanel.Mount, err error) {
			require.NoError(t, err)
			assert.Empty(t, mounts)
		}},
		{"sem a chave esperada", `{"outro":[]}`, func(t *testing.T, mounts []easypanel.Mount, err error) {
			require.NoError(t, err)
			assert.Empty(t, mounts)
		}},
		{"lista crua", `[{"type":"volume","mountPath":"/data"}]`, func(t *testing.T, mounts []easypanel.Mount, err error) {
			require.NoError(t, err)
			require.Len(t, mounts, 1)
			assert.Equal(t, "/data", mounts[0].MountPath)
		}},
		{"chave com lista", `{"mounts":[{"type":"volume","mountPath":"/data"},{"type":"bind","mountPath":"/etc"}]}`,
			func(t *testing.T, mounts []easypanel.Mount, err error) {
				require.NoError(t, err)
				require.Len(t, mounts, 2)
				assert.Equal(t, "bind", mounts[1].Type)
			}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			panel := newCovPanel(t).reply("listMounts", http.StatusOK, testCase.body)

			mounts, err := panel.client().ListMounts(context.Background(), "aluxen", "aluxen-demo")
			testCase.wants(t, mounts, err)
		})
	}
}

// A anotação do serviço vem como string crua ou embrulhada em `notes`; qualquer
// outra forma devolve anotação vazia (serviço sem prazo não expira sozinho) em
// vez de propagar erro.
func TestCovServiceNotesTolerateEveryResponseShape(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"etiqueta embrulhada", `{"notes":"expires_at=2026-10-05"}`, "expires_at=2026-10-05"},
		{"anotação crua", `"expires_at=2026-10-05"`, "expires_at=2026-10-05"},
		{"corpo vazio", "", ""},
		{"string sem fechamento", `"expires_at=2026-10-05`, ""},
		{"corpo ilegível", `{notes`, ""},
		{"objeto sem a chave", `{"outro":"valor"}`, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			panel := newCovPanel(t).reply("getServiceNotes", http.StatusOK, testCase.body)

			notes, err := panel.client().GetServiceNotes(context.Background(), "aluxen", "aluxen-demo")
			require.NoError(t, err)
			assert.Equal(t, testCase.want, notes)
		})
	}
}

// ── Remoção sem resíduo (BR-402) ───────────────────────────────────

// Se o painel não deixa ler os volumes, o serviço não é destruído: apagar o app
// e deixar o volume para trás seria resíduo de um cliente.
func TestCovDeleteAppServiceStopsWhenMountsCannotBeRead(t *testing.T) {
	panel := newCovPanel(t).reply("listMounts", http.StatusForbidden, `{"message":"sem permissão"}`)

	err := panel.client().DeleteAppService(context.Background(), "aluxen", "aluxen-demo")
	require.Error(t, err)

	var apiErr *easypanel.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.Equal(t, []string{"listMounts"}, panel.operations(), "o serviço não pode ser destruído sem remover o volume")
}

// Se um volume não pode ser removido, a sequência para antes de destruir o
// serviço: o que sobrou continua visível no painel para o operador.
func TestCovDeleteAppServiceStopsWhenAMountCannotBeRemoved(t *testing.T) {
	panel := newCovPanel(t).
		reply("listMounts", http.StatusOK, `[{"type":"volume","mountPath":"/data"},{"type":"volume","mountPath":"/cache"}]`).
		reply("deleteMount", http.StatusInternalServerError, `{"message":"volume em uso"}`)

	err := panel.client().DeleteAppService(context.Background(), "aluxen", "aluxen-demo")
	require.Error(t, err)

	var apiErr *easypanel.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	assert.Equal(t, "deleteMount", apiErr.Operation)
	assert.Equal(t, []string{"listMounts", "deleteMount"}, panel.operations())
}

// Os volumes são removidos do maior índice para o menor — cada remoção
// reordena a lista no painel — e pontos de montagem que não são volume ficam.
func TestCovDeleteAppServiceRemovesVolumesFromTheHighestIndex(t *testing.T) {
	panel := newCovPanel(t).
		reply("listMounts", http.StatusOK,
			`[{"type":"volume","mountPath":"/data"},{"type":"bind","mountPath":"/etc/hosts"},{"type":"volume","mountPath":"/cache"}]`)

	require.NoError(t, panel.client().DeleteAppService(context.Background(), "aluxen", "aluxen-demo"))

	assert.Equal(t, []string{"listMounts", "deleteMount", "deleteMount", "destroyAppService"}, panel.operations())

	panel.mu.Lock()
	defer panel.mu.Unlock()
	indices := make([]float64, 0, 2)
	for _, call := range panel.calls {
		if call.Operation == "deleteMount" {
			indices = append(indices, call.Body["index"].(float64))
		}
	}
	assert.Equal(t, []float64{2, 0}, indices, "o bind no índice 1 não é removido e os volumes vêm de trás para frente")
}

// ── Recusa longa ───────────────────────────────────────────────────

// Um corpo enorme (página de erro de proxy) entra no erro truncado: a mensagem
// fica legível e não carrega megabytes para o terminal do operador.
func TestCovHTTPErrorBodyIsTruncated(t *testing.T) {
	huge := strings.Repeat("x", 5000)
	panel := newCovPanel(t).reply("createAppService", http.StatusBadGateway, huge)

	err := panel.client().CreateAppService(context.Background(), "aluxen", "aluxen-demo", "img")
	require.Error(t, err)

	var apiErr *easypanel.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.Equal(t, "502 Bad Gateway", apiErr.Status)
	assert.True(t, strings.HasSuffix(apiErr.Body, "…"), "o corte precisa ser marcado: %q", apiErr.Body)
	assert.Less(t, len(apiErr.Body), len(huge), "o corpo guardado precisa ser menor que a resposta")
	assert.True(t, strings.HasPrefix(huge, strings.TrimSuffix(apiErr.Body, "…")))
	assert.Contains(t, apiErr.Error(), "502 Bad Gateway")
}

// ── Falhas de transporte ───────────────────────────────────────────

// covTransport é o transporte de teste: falha a chamada ou devolve uma resposta
// pronta, sem abrir soquete nenhum.
type covTransport struct {
	err    error
	status int
	body   io.ReadCloser
}

func (t covTransport) RoundTrip(*http.Request) (*http.Response, error) {
	if t.err != nil {
		return nil, t.err
	}
	return &http.Response{
		StatusCode: t.status,
		Status:     http.StatusText(t.status),
		Header:     http.Header{},
		Body:       t.body,
	}, nil
}

// covBrokenBody é um corpo de resposta que falha na leitura no meio do caminho.
type covBrokenBody struct{}

func (covBrokenBody) Read([]byte) (int, error) { return 0, errors.New("conexão interrompida") }
func (covBrokenBody) Close() error             { return nil }

func covClientWithTransport(transport http.RoundTripper) *easypanel.Client {
	return easypanel.NewClient(easypanel.Config{
		BaseURL: "https://painel.exemplo.com", APIKey: "chave-de-teste", HTTPClient: &http.Client{Transport: transport},
	})
}

// A URL malformada do painel vira erro na montagem da requisição, com a operação
// no texto: é o que o operador vê quando EASYPANEL_URL está errada.
func TestCovUnusableBaseURLFailsBuildingTheRequest(t *testing.T) {
	client := easypanel.NewClient(easypanel.Config{BaseURL: "https://painel com espaço.com", APIKey: "chave"})

	err := client.CreateAppService(context.Background(), "aluxen", "aluxen-demo", "img")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "createAppService")
	assert.Contains(t, err.Error(), "montar requisição")
}

// Falha de rede e resposta cortada no meio são relatadas com a operação e o
// erro original preservado (errors.Is), nunca como sucesso.
func TestCovTransportFailuresKeepTheOperationAndTheCause(t *testing.T) {
	offline := errors.New("painel fora do ar")

	t.Run("transporte", func(t *testing.T) {
		client := covClientWithTransport(covTransport{err: offline})

		err := client.DeployAppService(context.Background(), "aluxen", "aluxen-demo")
		require.Error(t, err)
		assert.ErrorIs(t, err, offline)
		assert.Contains(t, err.Error(), "deployAppService")
	})

	t.Run("leitura da resposta", func(t *testing.T) {
		client := covClientWithTransport(covTransport{status: http.StatusOK, body: covBrokenBody{}})

		_, err := client.GetServiceNotes(context.Background(), "aluxen", "aluxen-demo")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ler resposta")
		assert.Contains(t, err.Error(), "getServiceNotes")
	})

	t.Run("resposta saudável", func(t *testing.T) {
		client := covClientWithTransport(covTransport{
			status: http.StatusOK, body: io.NopCloser(strings.NewReader(`{"notes":"expires_at=2026-10-05"}`)),
		})

		notes, err := client.GetServiceNotes(context.Background(), "aluxen", "aluxen-demo")
		require.NoError(t, err)
		assert.Equal(t, "expires_at=2026-10-05", notes)
	})
}

// A query de leitura identifica o projeto e o serviço, e a credencial vai no
// cabeçalho Authorization.
func TestCovReadsSendQueryAndCredential(t *testing.T) {
	panel := newCovPanel(t).reply("listMounts", http.StatusOK, "[]")
	query := make(chan url.Values, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		query <- request.URL.Query()
		panel.serve(writer, request)
	}))
	t.Cleanup(server.Close)

	client := easypanel.NewClient(easypanel.Config{BaseURL: server.URL, APIKey: "chave-de-teste"})
	_, err := client.ListMounts(context.Background(), "aluxen", "aluxen-demo")
	require.NoError(t, err)

	values := <-query
	assert.Equal(t, "aluxen", values.Get("projectName"))
	assert.Equal(t, "aluxen-demo", values.Get("serviceName"))
}

// ── Etiqueta de expiração inválida ─────────────────────────────────

// Etiqueta com data impossível é tratada como ausência de prazo: a demo não
// pode ser removida por causa de uma anotação corrompida.
func TestCovUnparsableExpiryLabelNeverExpires(t *testing.T) {
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)

	for _, notes := range []string{
		"expires_at=2026-13-45",
		"expires_at=",
		"expires_at=05/10/2026",
		"expires_at=0000-00-00",
	} {
		expiresAt, ok := easypanel.ParseNotes(notes)
		assert.False(t, ok, "anotação %q não tem prazo legível", notes)
		assert.True(t, expiresAt.IsZero())
		assert.False(t, easypanel.Expired(notes, now, 365*24*time.Hour),
			"anotação ilegível %q não pode vencer sozinha nem com janela de um ano", notes)
	}
}

// Prazo relativo malformado é recusado com erro: o operador precisa saber que o
// `--expires-at` não foi entendido em vez de subir uma demo sem prazo.
func TestCovParseExpiryRejectsMalformedRelativeSpans(t *testing.T) {
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)

	zero, err := easypanel.ParseExpiry("+0d", now)
	require.NoError(t, err, "+0d é hoje: o prazo relativo começa em zero")
	assert.Equal(t, "2026-09-21", easypanel.ExpiryDate(zero))

	for _, value := range []string{"+xd", "+-1d", "+1.5d", "+d", "+99999999999999999999d"} {
		if _, err := easypanel.ParseExpiry(value, now); err == nil {
			t.Errorf("ParseExpiry(%q) deveria devolver erro", value)
		}
	}
}
