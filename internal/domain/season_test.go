package domain

import (
	"testing"
	"time"
)

func TestDepositRefundRules(t *testing.T) {
	cases := []struct {
		name      string
		held      float64
		requested float64
		want      DepositStatus
		wantValue float64
		wantErr   bool
	}{
		{"devolução integral", 1200, 1200, DepositStatusRefunded, 1200, false},
		{"devolução parcial", 1200, 500, DepositStatusPartiallyRefunded, 500, false},
		{"retenção total", 1200, 0, DepositStatusForfeited, 0, false},
		{"sem caução retida", 0, 100, "", 0, true},
		{"valor negativo", 1200, -10, "", 0, true},
		{"acima do retido", 1200, 1300, "", 0, true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			refund, err := ComputeDepositRefund(testCase.held, testCase.requested)
			if testCase.wantErr {
				if err == nil {
					t.Fatal("esperava erro de validação")
				}
				if !IsValidation(err) {
					t.Fatalf("erro deveria ser de validação: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("erro inesperado: %v", err)
			}
			if refund.Status != testCase.want || refund.RefundedAmount != testCase.wantValue {
				t.Fatalf("devolução = %+v, esperado %s/%v", refund, testCase.want, testCase.wantValue)
			}
		})
	}
}

func TestValidateDepositAndHasDeposit(t *testing.T) {
	if err := ValidateDeposit(nil, DepositStatusNone); err != nil {
		t.Fatalf("contrato sem caução é válido: %v", err)
	}
	if err := ValidateDeposit(nil, DepositStatusHeld); err == nil {
		t.Fatal("status de caução sem valor deveria falhar")
	}
	if err := ValidateDeposit(FloatPtr(0), DepositStatusHeld); err == nil {
		t.Fatal("caução zerada deveria falhar")
	}
	if err := ValidateDeposit(FloatPtr(-1), DepositStatusHeld); err == nil {
		t.Fatal("caução negativa deveria falhar")
	}
	if err := ValidateDeposit(FloatPtr(1200), DepositStatusHeld); err != nil {
		t.Fatalf("caução válida recusada: %v", err)
	}

	if HasDeposit(nil, DepositStatusHeld) {
		t.Fatal("sem valor não há caução")
	}
	if HasDeposit(FloatPtr(1200), DepositStatusNone) {
		t.Fatal("valor com situação 'sem caução' não conta como caução")
	}
	if !HasDeposit(FloatPtr(1200), DepositStatusHeld) {
		t.Fatal("caução retida deveria contar")
	}
	if DepositStatusLabel(DepositStatusPartiallyRefunded) != "Devolvida parcialmente" {
		t.Fatal("rótulo de devolução parcial inesperado")
	}
}

func TestInspectionValidation(t *testing.T) {
	if err := ValidateInspection(InspectionKindCheckIn, InspectionConditionGood, "2026-12-20T14:00"); err != nil {
		t.Fatalf("vistoria válida recusada: %v", err)
	}
	if err := ValidateInspection("outro", InspectionConditionGood, "2026-12-20T14:00"); err == nil {
		t.Fatal("tipo inválido deveria falhar")
	}
	if err := ValidateInspection(InspectionKindCheckOut, "perfeito", "2026-12-20T14:00"); err == nil {
		t.Fatal("estado geral inválido deveria falhar")
	}
	if err := ValidateInspection(InspectionKindCheckOut, InspectionConditionFair, "   "); err == nil {
		t.Fatal("data vazia deveria falhar")
	}
	if InspectionKindLabel(InspectionKindCheckOut) != "Vistoria de saída" {
		t.Fatal("rótulo de vistoria de saída inesperado")
	}
	if !InspectionKindCheckIn.Valid() || InspectionKind("x").Valid() {
		t.Fatal("validação de tipo de vistoria inesperada")
	}
	if InspectionConditionLabel(InspectionConditionBad) != "Ruim" || !InspectionConditionExcellent.Valid() {
		t.Fatal("rótulos/validação de estado geral inesperados")
	}
	if InspectionCondition("x").Valid() {
		t.Fatal("estado geral desconhecido não pode ser válido")
	}
}

func TestInspectionItemsAndPhotosRoundTrip(t *testing.T) {
	items := []InspectionItem{{Area: "Sala", Item: "Sofá", State: "bom", Notes: "pequeno rasgo"}}
	encoded, err := EncodeInspectionItems(items)
	if err != nil {
		t.Fatalf("serializar itens: %v", err)
	}
	if encoded == nil {
		t.Fatal("lista com item não pode virar nulo")
	}
	decoded, err := DecodeInspectionItems(encoded)
	if err != nil {
		t.Fatalf("desserializar itens: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Area != "Sala" || decoded[0].Notes != "pequeno rasgo" {
		t.Fatalf("itens perdidos na ida e volta: %+v", decoded)
	}

	empty, err := EncodeInspectionItems(nil)
	if err != nil || empty != nil {
		t.Fatalf("lista vazia deve virar nulo: %v %v", empty, err)
	}
	decoded, err = DecodeInspectionItems(nil)
	if err != nil || len(decoded) != 0 {
		t.Fatalf("nulo deve virar lista vazia: %+v %v", decoded, err)
	}
	broken := "{isso não é json"
	if _, err := DecodeInspectionItems(&broken); err == nil {
		t.Fatal("json inválido deveria falhar")
	}

	photos := []InspectionPhoto{{Name: "inspections/foto.jpg", Label: "sala", Caption: "antes"}}
	encodedPhotos, err := EncodeInspectionPhotos(photos)
	if err != nil || encodedPhotos == nil {
		t.Fatalf("serializar fotos: %v %v", encodedPhotos, err)
	}
	decodedPhotos, err := DecodeInspectionPhotos(encodedPhotos)
	if err != nil || len(decodedPhotos) != 1 || decodedPhotos[0].Caption != "antes" {
		t.Fatalf("fotos perdidas na ida e volta: %+v %v", decodedPhotos, err)
	}
	brokenPhotos := "nope"
	if _, err := DecodeInspectionPhotos(&brokenPhotos); err == nil {
		t.Fatal("json de fotos inválido deveria falhar")
	}
}

func TestEnumLabelsAndHelpers(t *testing.T) {
	if LeaseStatusLabel(LeaseStatusSuspended) != "Suspenso" || LeaseStatusLabel("x") != "x" {
		t.Fatal("rótulos de situação do contrato inesperados")
	}
	if ReceiptCategoryLabel(ReceiptCategoryCleaning) != "Taxa de limpeza" ||
		ReceiptCategoryLabel(ReceiptCategoryConsumables) != "Consumíveis" ||
		ReceiptCategoryLabel(ReceiptCategoryDepositRefund) != "Devolução de caução" {
		t.Fatal("rótulos de categoria inesperados")
	}
	if ReceiptStatusLabel(ReceiptStatusPartial) != "Parcial" || ReceiptStatusLabel("x") != "A vencer" {
		t.Fatal("rótulos de situação do recibo inesperados")
	}
	if AuditOriginLabel(AuditOriginClient) != "Área do Cliente" || AuditOriginLabel("x") != "x" {
		t.Fatal("rótulos de origem da trilha inesperados")
	}
}

func TestAuditCatalogLabels(t *testing.T) {
	meta := AuditActionMetaFor(ActionReceiptPay)
	if meta.Label != "Baixa registrada" || meta.Entity != EntityReceipt || meta.Tone != ToneUpdate {
		t.Fatalf("metadados de ação inesperados: %+v", meta)
	}
	unknown := AuditActionMetaFor("acao.desconhecida")
	if unknown.Label != "acao.desconhecida" || unknown.Tone != ToneUpdate {
		t.Fatalf("ação desconhecida deveria cair no padrão: %+v", unknown)
	}
	if EntityLabel(EntityInspection) != "Vistoria" || EntityLabel("x") != "x" {
		t.Fatal("rótulos de área inesperados")
	}
	if MetadataLabel("confidence") != "Confiança" || MetadataLabel("outro") != "outro" {
		t.Fatal("rótulos de metadado inesperados")
	}
	for _, action := range []string{ActionDepositRefund, ActionInspectionCreate, ActionPaymentProofAccepted, ActionCollectionSent, ActionAdjustmentApplied} {
		if AuditActionMetaFor(action).Label == action {
			t.Errorf("ação %s sem rótulo em português", action)
		}
	}
}

func TestEntityPointerHelpers(t *testing.T) {
	text := "valor"
	if Str(&text) != "valor" || Str(nil) != "" {
		t.Fatal("Str inesperado")
	}
	number := 12.5
	if Float(&number) != 12.5 || Float(nil) != 0 {
		t.Fatal("Float inesperado")
	}
	user := User{PasswordChangedAt: nil}
	if !user.MustChangePassword() {
		t.Fatal("usuário sem troca registrada deve exigir troca")
	}
	changed := "2026-09-21 12:00:00"
	user.PasswordChangedAt = &changed
	if user.MustChangePassword() {
		t.Fatal("usuário com troca registrada não deve exigir troca")
	}
	empty := ""
	tenant := Tenant{PasswordChangedAt: &empty}
	if !tenant.MustChangePassword() {
		t.Fatal("data de troca vazia deve exigir troca")
	}
}

func TestValidationErrors(t *testing.T) {
	err := Invalid("Informe um valor.")
	if !IsValidation(err) || ValidationMessage(err) != "Informe um valor." {
		t.Fatalf("erro de validação inesperado: %v / %q", err, ValidationMessage(err))
	}
	if err.Error() != "Informe um valor." {
		t.Fatalf("Error() = %q", err.Error())
	}
	if IsValidation(ErrNotFound) || ValidationMessage(ErrNotFound) != "" {
		t.Fatal("erro sentinela não é validação")
	}
}

func TestFormattingRulesUsedByDocuments(t *testing.T) {
	if got := FormatNumber(2500.5); got != "2500.5" {
		t.Fatalf("FormatNumber = %q", got)
	}
	if got := MonthKey(2026, 8); got != "2026-08" {
		t.Fatalf("MonthKey = %q", got)
	}
	if got := DaysInMonth(2028, 2); got != 29 {
		t.Fatalf("DaysInMonth = %d", got)
	}
	if err := ValidateDueDay(0); err == nil {
		t.Fatal("dia 0 deveria falhar")
	}
	if err := ValidateDueDay(31); err != nil {
		t.Fatalf("dia 31 é válido: %v", err)
	}
	if err := ValidateRentAmount(0); err == nil {
		t.Fatal("aluguel zerado deveria falhar")
	}
	if err := ValidatePeriod("2026-12-20", "2026-12-01"); err == nil {
		t.Fatal("período invertido deveria falhar")
	}
	if err := ValidatePeriod("", "2026-12-01"); err == nil {
		t.Fatal("período vazio deveria falhar")
	}
	if err := ValidatePeriod("2026-12-01", "2026-12-20"); err != nil {
		t.Fatalf("período válido recusado: %v", err)
	}
	_ = time.Now()
}
