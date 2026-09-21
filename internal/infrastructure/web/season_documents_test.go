package web

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/authz"
)

// ── /inspection-pdf ────────────────────────────────────────────────

func TestInspectionPDFRejectsRequestWithoutID(t *testing.T) {
	server := newDocumentsServer(t)

	response := server.get(t, "/inspection-pdf", docAdminID, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400", response.Code)
	}
	if body := response.Body.String(); !strings.Contains(body, "ID inválido") {
		t.Fatalf("corpo = %q", body)
	}
}

func TestInspectionPDFWithValidTokenServesTheDocument(t *testing.T) {
	server := newDocumentsServer(t)
	token := server.receiptToken(authz.KindInspection, docInspectionID)

	response := server.get(t, "/inspection-pdf?id="+docInspectionID+"&token="+token, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/pdf" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	expectedDisposition := `inline; filename="vistoria-` + docInspectionID + `.pdf"`
	if disposition := response.Header().Get("Content-Disposition"); disposition != expectedDisposition {
		t.Fatalf("Content-Disposition = %q, esperado %q", disposition, expectedDisposition)
	}
	if !bytes.HasPrefix(response.Body.Bytes(), []byte("%PDF-")) {
		t.Fatal("resposta não é um PDF")
	}
	if actions := server.audit.actions(); len(actions) != 1 || actions[0] != domain.ActionDocumentInspection {
		t.Fatalf("auditoria = %v", actions)
	}
	if origin := server.audit.entries[0].Origin; origin != domain.AuditOriginPublic {
		t.Fatalf("origem da auditoria = %q, esperado link público", origin)
	}
	if label := domain.Str(server.audit.entries[0].EntityLabel); label != "Maria Silva · Casa da Praia" {
		t.Fatalf("rótulo da auditoria = %q", label)
	}
}

func TestInspectionPDFAsAdminAndAsOwnerTenant(t *testing.T) {
	server := newDocumentsServer(t)

	admin := server.get(t, "/inspection-pdf?id="+docInspectionID, docAdminID, "")
	if admin.Code != http.StatusOK {
		t.Fatalf("administrador: status = %d, corpo %s", admin.Code, admin.Body.String())
	}
	if origin := server.audit.entries[0].Origin; origin != domain.AuditOriginWeb {
		t.Fatalf("origem da auditoria do administrador = %q", origin)
	}

	owner := server.get(t, "/inspection-pdf?id="+docInspectionID, "", docTenantID)
	if owner.Code != http.StatusOK {
		t.Fatalf("titular: status = %d, corpo %s", owner.Code, owner.Body.String())
	}

	checkout := server.get(t, "/inspection-pdf?id="+docCheckoutID, "", docTenantID)
	if checkout.Code != http.StatusOK {
		t.Fatalf("vistoria de saída do titular: status = %d", checkout.Code)
	}
}

func TestInspectionPDFRefusesAnotherTenantAndAnonymous(t *testing.T) {
	server := newDocumentsServer(t)

	response := server.get(t, "/inspection-pdf?id="+docInspectionID, "", "tenant-2")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403", response.Code)
	}
	if body := response.Body.String(); !strings.Contains(body, "Este documento não pertence ao seu cadastro.") {
		t.Fatalf("corpo = %q", body)
	}

	anonymous := server.get(t, "/inspection-pdf?id="+docInspectionID, "", "")
	if anonymous.Code != http.StatusFound {
		t.Fatalf("visitante: status = %d, esperado 302", anonymous.Code)
	}
	if location := anonymous.Header().Get("Location"); location != "/login" {
		t.Fatalf("destino = %q, esperado /login", location)
	}
}

func TestInspectionPDFNotFound(t *testing.T) {
	server := newDocumentsServer(t)
	token := server.receiptToken(authz.KindInspection, "inexistente")

	for name, target := range map[string]string{
		"administrador": "/inspection-pdf?id=inexistente",
		"chave válida":  "/inspection-pdf?id=inexistente&token=" + token,
	} {
		response := server.get(t, target, docAdminID, "")
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, esperado 404", name, response.Code)
		}
		if body := response.Body.String(); !strings.Contains(body, "Vistoria não encontrada") {
			t.Fatalf("%s: corpo = %q", name, body)
		}
	}
	if actions := server.audit.actions(); len(actions) != 0 {
		t.Fatalf("documento inexistente foi auditado como emitido: %v", actions)
	}
}

// ── /deposit-pdf ───────────────────────────────────────────────────

func TestDepositPDFServesTheDocument(t *testing.T) {
	server := newDocumentsServer(t)
	token := server.receiptToken(authz.KindDeposit, docSeasonLeaseID)

	response := server.get(t, "/deposit-pdf?id="+docSeasonLeaseID+"&token="+token, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/pdf" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	expectedDisposition := `inline; filename="caucao-` + docSeasonLeaseID + `.pdf"`
	if disposition := response.Header().Get("Content-Disposition"); disposition != expectedDisposition {
		t.Fatalf("Content-Disposition = %q, esperado %q", disposition, expectedDisposition)
	}
	if !bytes.HasPrefix(response.Body.Bytes(), []byte("%PDF-")) {
		t.Fatal("resposta não é um PDF")
	}
	if actions := server.audit.actions(); len(actions) != 1 || actions[0] != domain.ActionDocumentDeposit {
		t.Fatalf("auditoria = %v", actions)
	}
	if origin := server.audit.entries[0].Origin; origin != domain.AuditOriginPublic {
		t.Fatalf("origem da auditoria = %q, esperado link público", origin)
	}

	admin := server.get(t, "/deposit-pdf?id="+docSeasonLeaseID, docAdminID, "")
	if admin.Code != http.StatusOK {
		t.Fatalf("administrador: status = %d", admin.Code)
	}
	owner := server.get(t, "/deposit-pdf?id="+docSeasonLeaseID, "", docTenantID)
	if owner.Code != http.StatusOK {
		t.Fatalf("titular: status = %d, corpo %s", owner.Code, owner.Body.String())
	}
}

func TestDepositPDFRefusesContractWithoutDeposit(t *testing.T) {
	server := newDocumentsServer(t)

	response := server.get(t, "/deposit-pdf?id="+docLeaseID, docAdminID, "")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403", response.Code)
	}
	if body := response.Body.String(); !strings.Contains(body, "Este contrato não tem caução registrada.") {
		t.Fatalf("corpo = %q", body)
	}
	if actions := server.audit.actions(); len(actions) != 0 {
		t.Fatalf("contrato sem caução foi auditado como emitido: %v", actions)
	}
}

func TestDepositPDFRefusesAnotherTenantAndMissingContract(t *testing.T) {
	server := newDocumentsServer(t)

	response := server.get(t, "/deposit-pdf?id="+docSeasonLeaseID, "", "tenant-2")
	if response.Code != http.StatusForbidden {
		t.Fatalf("outro locatário: status = %d, esperado 403", response.Code)
	}

	missing := server.get(t, "/deposit-pdf?id=inexistente", docAdminID, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("contrato inexistente: status = %d, esperado 404", missing.Code)
	}
	if body := missing.Body.String(); !strings.Contains(body, "Caução não encontrada") {
		t.Fatalf("corpo = %q", body)
	}
}

// ── /validar/vistoria/{id} ─────────────────────────────────────────

func TestVerifyInspectionPageShowsTheOfficialData(t *testing.T) {
	server := newDocumentsServer(t)
	token := server.receiptToken(authz.KindInspection, docInspectionID)

	response := server.get(t, "/validar/vistoria/"+docInspectionID+"?token="+token, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, expected := range []string{
		"DOCUMENTO AUTÊNTICO E VÁLIDO",
		"Auto de Vistoria de Entrada",
		"Vistoria de entrada",
		server.signer.FormatCode(token),
		"Maria Silva",
		"123.456.789-00",
		"João Souza",
		"Casa da Praia",
		"Rua das Flores, 123, Cabo Frio, RJ",
		"01/03/2026 10:00",
		"Boa",
		"01/03/2026 até 30/09/2026",
		"21/09/2026 12:00",
		"/inspection-pdf?id=" + docInspectionID + "&amp;token=" + token,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("página sem %q", expected)
		}
	}

	// A vistoria de saída muda o rótulo do documento.
	checkoutToken := server.receiptToken(authz.KindInspection, docCheckoutID)
	checkout := server.get(t, "/validar/vistoria/"+docCheckoutID+"?token="+checkoutToken, "", "")
	if checkout.Code != http.StatusOK {
		t.Fatalf("vistoria de saída: status = %d", checkout.Code)
	}
	if body := checkout.Body.String(); !strings.Contains(body, "Auto de Vistoria de Saída") || !strings.Contains(body, "Regular") {
		t.Fatalf("página da vistoria de saída sem o tipo ou o estado geral")
	}
}

func TestVerifyInspectionPageRefusesInvalidTokenAndMissingInspection(t *testing.T) {
	server := newDocumentsServer(t)

	invalid := server.get(t, "/validar/vistoria/"+docInspectionID+"?token=00000000000000000000000000000000", "", "")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("chave inválida: status = %d, esperado 400", invalid.Code)
	}
	body := invalid.Body.String()
	if !strings.Contains(body, "DOCUMENTO NÃO AUTENTICADO") {
		t.Fatal("página não mostra o selo de documento não autenticado")
	}
	if !strings.Contains(body, "Chave de autenticação inválida ou documento não autenticado.") {
		t.Fatalf("página sem a mensagem do app atual: %s", body)
	}
	if strings.Contains(body, "Maria Silva") {
		t.Fatal("página expôs os dados da vistoria com chave inválida")
	}

	// Sem chave não há validação pública, mesmo com o documento existindo.
	withoutToken := server.get(t, "/validar/vistoria/"+docInspectionID, "", "")
	if withoutToken.Code != http.StatusBadRequest {
		t.Fatalf("sem chave: status = %d, esperado 400", withoutToken.Code)
	}

	missing := server.get(t, "/validar/vistoria/inexistente?token="+server.receiptToken(authz.KindInspection, "inexistente"), "", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("vistoria inexistente: status = %d, esperado 404", missing.Code)
	}
	if body := missing.Body.String(); !strings.Contains(body, "Vistoria não localizada na base oficial de registros.") {
		t.Fatalf("corpo = %q", body)
	}
}

// ── /validar/caucao/{id} ───────────────────────────────────────────

func TestVerifyDepositPageShowsTheOfficialData(t *testing.T) {
	server := newDocumentsServer(t)
	token := server.receiptToken(authz.KindDeposit, docSeasonLeaseID)

	response := server.get(t, "/validar/caucao/"+docSeasonLeaseID+"?token="+token, "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200: %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, expected := range []string{
		"DOCUMENTO AUTÊNTICO E VÁLIDO",
		"Recibo de Devolução de Caução",
		server.signer.FormatCode(token),
		"Maria Silva",
		"João Souza",
		"Casa da Praia",
		"Rua das Flores, 123, Cabo Frio, RJ",
		"R$ 1.200,00",
		"R$ 500,00",
		"R$ 700,00",
		"Devolvida parcialmente",
		"21/09/2026 15:04",
		"21/09/2026 12:00",
		"/deposit-pdf?id=" + docSeasonLeaseID + "&amp;token=" + token,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("página sem %q", expected)
		}
	}
}

func TestVerifyDepositPageRefusesInvalidTokenAndMissingLease(t *testing.T) {
	server := newDocumentsServer(t)

	invalid := server.get(t, "/validar/caucao/"+docSeasonLeaseID+"?token=00000000000000000000000000000000", "", "")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("chave inválida: status = %d, esperado 400", invalid.Code)
	}
	if body := invalid.Body.String(); !strings.Contains(body, "DOCUMENTO NÃO AUTENTICADO") {
		t.Fatalf("página não mostra o selo de documento não autenticado: %s", body)
	}

	// A chave da vistoria não valida a caução.
	crossed := server.get(t, "/validar/caucao/"+docSeasonLeaseID+"?token="+server.receiptToken(authz.KindInspection, docSeasonLeaseID), "", "")
	if crossed.Code != http.StatusBadRequest {
		t.Fatalf("chave de outro tipo: status = %d, esperado 400", crossed.Code)
	}

	missing := server.get(t, "/validar/caucao/inexistente?token="+server.receiptToken(authz.KindDeposit, "inexistente"), "", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("caução inexistente: status = %d, esperado 404", missing.Code)
	}
	if body := missing.Body.String(); !strings.Contains(body, "Caução não localizada na base oficial de registros.") {
		t.Fatalf("corpo = %q", body)
	}
}
