package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
)

// ── Work 0008: ações do contrato em dropdown, temporada como status ──
//
// Os três testes abaixo cobrem as tasks 8.1 (dropdown + alinhamento), 8.2
// (fim da aba season) e 8.3 (bolinha no sanduíche aberto).

func TestCovLeasesRowDropdownGroupsSecondaryActions(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, _ := panel.authenticate(t)

	panel.leases.seed(domain.Lease{
		ID: "l1", TenantID: "t1", PropertyID: "p1", StartDate: "2026-01-01", EndDate: "2026-12-31",
		DueDay: 10, RentAmount: 2500, Status: domain.LeaseStatusActive,
		DepositStatus: domain.DepositStatusHeld, DepositAmount: domain.FloatPtr(2500),
	})

	recorder := panel.tab(t, session, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, esperado 200", recorder.Code)
	}
	body := recorder.Body.String()

	// Cada linha tem um dropdown igual ao de pagamentos para as ações secundárias.
	if !strings.Contains(body, `<details class="pdf-dropdown">`) {
		t.Fatal("linha de contrato sem <details class=\"pdf-dropdown\"> para as ações secundárias")
	}
	for _, inside := range []string{
		`data-modal-open="modal-deposit"`,
		`data-modal-open="modal-inspection"`,
		`data-modal-open="modal-refund"`,
		`value="lease.terminate"`,
		`/contract-pdf?id=l1`,
		`data-whatsapp-send="lease"`,
		`/deposit-pdf?id=l1`,
	} {
		if !strings.Contains(body, inside) {
			t.Fatalf("dropdown da linha sem %q", inside)
		}
	}

	// Editar e Excluir continuam visíveis fora do dropdown.
	for _, visible := range []string{
		`data-modal-open="lease-edit-modal"`,
		`value="lease.delete"`,
	} {
		if !strings.Contains(body, visible) {
			t.Fatalf("linha de contrato sem ação visível %q", visible)
		}
	}
}

func TestCovSeasonTabNormalizesToLeases(t *testing.T) {
	tenants, properties := leaseFixtures()
	panel := newLeasePanel(t, tenants, properties)
	session, _ := panel.authenticate(t)

	// ?tab=season cai nos contratos, sem erro nem tela em branco.
	for _, target := range []string{"/dashboard?tab=season", "/dashboard?tab=caucao"} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.AddCookie(&http.Cookie{Name: adminCookieName, Value: session})
		recorder := panel.server.do(request)
		body := recorder.Body.String()
		if recorder.Code != http.StatusOK || !strings.Contains(body, "Gerenciar Contratos") {
			t.Fatalf("%s não normalizou para a aba de contratos (status %d)", target, recorder.Code)
		}
	}

	// O menu não oferece mais a aba própria de temporada/caução.
	request := httptest.NewRequest(http.MethodGet, "/dashboard?tab=leases", nil)
	request.AddCookie(&http.Cookie{Name: adminCookieName, Value: session})
	body := panel.server.do(request).Body.String()
	if strings.Contains(body, `href="/dashboard?tab=season"`) {
		t.Fatal("menu ainda oferece a aba própria de temporada")
	}

	// Temporada continua como situação do contrato (filtro preservado).
	if !strings.Contains(body, `value="/dashboard?tab=leases&amp;status=season"`) {
		t.Fatal("filtro de temporada sumiu da aba de contratos")
	}

	// A comparação de vistorias mora na linha do contrato, não mais na aba.
	if !strings.Contains(body, `id="modal-comparison"`) {
		t.Fatal("aba de contratos sem o modal de comparação de vistorias")
	}
}

func TestCovOpenSidebarGlyphHasCircleStyle(t *testing.T) {
	recorder := httptest.NewRecorder()
	StaticHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/styles/panel.css", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("panel.css: status %d, esperado 200", recorder.Code)
	}
	css := recorder.Body.String()

	// Com o menu aberto o glifo '<' aparece dentro de um círculo. A regra é
	// lida por bloco: o seletor abre e as declarações vêm nas linhas seguintes.
	// O círculo vai no <svg> com o menu aberto — borda não renderiza no <path>
	// do glifo, então regra no path seria um não-op invisível.
	openRule, collapsedCircle := false, false
	inBlock, blockOpen := "", false
	for _, line := range strings.Split(css, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, "{") {
			inBlock, blockOpen = trimmed, true
			continue
		}
		if strings.HasPrefix(trimmed, "}") {
			inBlock, blockOpen = "", false
			continue
		}
		if !blockOpen || !strings.Contains(line, "border-radius") {
			continue
		}
		switch {
		// O seletor mira o <svg> com o menu aberto (`.menu-glyph` sem sufixo):
		// regra no path (`.menu-glyph-expanded`) seria um não-op invisível.
		case strings.Contains(inBlock, ".sidebar.open") && strings.Contains(inBlock, ".menu-glyph") && !strings.Contains(inBlock, ".menu-glyph-"):
			openRule = true
		case strings.Contains(inBlock, "collapsed") || strings.Contains(inBlock, "mobile-menu"):
			// Só do aberto: o hambúrguer do menu fechado não ganha círculo.
			collapsedCircle = true
		}
	}
	if !openRule {
		t.Fatal("CSS sem regra de círculo (border-radius) para o glifo do menu aberto")
	}
	if collapsedCircle {
		t.Fatal("CSS com círculo no glifo do menu fechado: a bolinha é só do aberto")
	}
}
