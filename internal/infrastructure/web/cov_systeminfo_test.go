package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
)

func TestCovSystemInfoRequiresAdminSession(t *testing.T) {
	panel := newCovPanel(t)

	recorder := panel.as(http.MethodGet, "/api/info", nil, "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401", recorder.Code)
	}
}

func TestCovSystemInfoDescribesTheInstance(t *testing.T) {
	panel := newCovPanel(t)
	panel.tenants.items = []domain.Tenant{{ID: "t1", Name: "Ana"}, {ID: "t2", Name: "Bia"}}
	panel.properties.items = []domain.Property{{ID: "p1", Name: "Casa"}}
	panel.leases.items = []domain.Lease{{ID: "l1", TenantID: "t1", PropertyID: "p1"}}
	panel.receipts.items = []domain.Receipt{{ID: "r1", Amount: 2500}}
	panel.settings.values[domain.SettingAppURL] = "https://aluxen.example/"
	sizes := 0
	panel.common.DBSize = func() int64 {
		sizes++
		return 8192
	}

	recorder := panel.as(http.MethodGet, "/api/info", nil, panel.admin.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", recorder.Code, recorder.Body.String())
	}
	payload := covDecodeJSON(t, recorder)

	if payload["app"] != "Aluxen" || payload["runtime"] != "Go" || payload["user"] != panel.admin.Username {
		t.Fatalf("payload = %v", payload)
	}
	if payload["version"] != panel.common.Build.Version {
		t.Fatalf("version = %v, esperado %v", payload["version"], panel.common.Build.Version)
	}
	if payload["appUrl"] != "https://aluxen.example/" {
		t.Fatalf("appUrl = %v, esperado o valor literal da configuração", payload["appUrl"])
	}
	if payload["authenticityUrl"] != "https://aluxen.example" {
		t.Fatalf("authenticityUrl = %v, esperado a URL pública normalizada", payload["authenticityUrl"])
	}
	if payload["nodeEnv"] != "test" {
		t.Fatalf("nodeEnv = %v", payload["nodeEnv"])
	}
	if payload["timezone"] == "" {
		t.Fatalf("timezone vazio: %v", payload)
	}

	counts, ok := payload["counts"].(map[string]any)
	if !ok {
		t.Fatalf("counts ausente: %v", payload)
	}
	for key, want := range map[string]float64{"tenants": 2, "properties": 1, "leases": 1, "receipts": 1} {
		if counts[key] != want {
			t.Fatalf("counts[%s] = %v, esperado %v", key, counts[key], want)
		}
	}

	database, ok := payload["database"].(map[string]any)
	if !ok {
		t.Fatalf("database ausente: %v", payload)
	}
	if database["dialect"] != "sqlite" || database["path"] != "/tmp/cov-aluxen.sqlite" || database["sizeBytes"] != float64(8192) {
		t.Fatalf("database = %v", database)
	}
	if sizes == 0 {
		t.Fatal("tamanho do banco não foi consultado")
	}
}

func TestCovSystemInfoFallsBackToRequestHostForAuthenticityURL(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.Deps.Opts.PublicBaseURL = ""

	request := httptest.NewRequest(http.MethodGet, "https://aluxen.example/api/info", nil)
	request.Host = "aluxen.example"
	request.AddCookie(panel.sessionCookie(panel.admin.ID, true))

	recorder := panel.do(request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}
	payload := covDecodeJSON(t, recorder)
	if payload["authenticityUrl"] != "http://aluxen.example" {
		t.Fatalf("authenticityUrl = %v, esperado a URL derivada do host da requisição", payload["authenticityUrl"])
	}
}

func TestCovSystemInfoDerivesHTTPSForThePublicDomain(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.Deps.Opts.PublicBaseURL = ""

	request := httptest.NewRequest(http.MethodGet, "http://aluxen.evolucsia.com/api/info", nil)
	request.Host = "aluxen.evolucsia.com"
	request.AddCookie(panel.sessionCookie(panel.admin.ID, true))

	recorder := panel.do(request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}
	if payload := covDecodeJSON(t, recorder); payload["authenticityUrl"] != "https://aluxen.evolucsia.com" {
		t.Fatalf("authenticityUrl = %v, esperado https no domínio público", payload["authenticityUrl"])
	}
}

func TestCovSystemInfoWithoutDatabaseSizeHook(t *testing.T) {
	panel := newCovPanel(t)
	panel.common.DBSize = nil

	payload := covDecodeJSON(t, panel.as(http.MethodGet, "/api/info", nil, panel.admin.ID))
	database, _ := payload["database"].(map[string]any)
	if database["sizeBytes"] != float64(0) {
		t.Fatalf("sizeBytes = %v, esperado 0 sem hook de tamanho", database["sizeBytes"])
	}
}

func TestCovSystemInfoReportsCountFailure(t *testing.T) {
	panel := newCovPanel(t)
	panel.tenants.err = errCovBoom

	recorder := panel.as(http.MethodGet, "/api/info", nil, panel.admin.ID)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando a contagem falha", recorder.Code)
	}
	if payload := covDecodeJSON(t, recorder); payload["error"] != "Erro interno do servidor." {
		t.Fatalf("payload = %v", payload)
	}
}

func TestCovSystemInfoReportsSettingsFailure(t *testing.T) {
	panel := newCovPanel(t)
	panel.settings.err = errCovBoom

	recorder := panel.as(http.MethodGet, "/api/info", nil, panel.admin.ID)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, esperado 500 quando a leitura das configurações falha", recorder.Code)
	}
}

func TestCovSystemInfoAcceptsJSONClient(t *testing.T) {
	panel := newCovPanel(t)

	request := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	request.Header.Set("Accept", "application/json")
	request.AddCookie(panel.sessionCookie(panel.admin.ID, true))

	recorder := panel.do(request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
}
