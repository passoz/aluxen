package domain

import "testing"

func TestBuildAuditChangesOmitsUnchangedFields(t *testing.T) {
	before := map[string]string{
		"name": "Carlos Drummond", "email": "carlos@exemplo.com", "phone": "(22) 99888-1122", "cpf": "111.222.333-44",
	}
	after := map[string]string{
		"name": "Carlos Drummond de Andrade", "email": "carlos@exemplo.com", "phone": "(22) 99888-1122", "cpf": "111.222.333-44",
	}

	changes := BuildAuditChanges(TenantAuditFields, before, after)
	if len(changes) != 1 {
		t.Fatalf("esperava 1 mudança, veio %d: %+v", len(changes), changes)
	}
	if changes[0].Field != "name" || changes[0].Label != "Nome" {
		t.Fatalf("mudança inesperada: %+v", changes[0])
	}
	if changes[0].From == nil || *changes[0].From != "Carlos Drummond" {
		t.Fatalf("valor anterior inesperado: %+v", changes[0].From)
	}
	if changes[0].To == nil || *changes[0].To != "Carlos Drummond de Andrade" {
		t.Fatalf("valor novo inesperado: %+v", changes[0].To)
	}
}

func TestBuildAuditChangesFormatsValues(t *testing.T) {
	before := map[string]string{"amount": "2500", "dueDate": "2026-09-10", "category": "rent", "paidAt": ""}
	after := map[string]string{"amount": "2600.5", "dueDate": "2026-10-10", "category": "extra", "paidAt": "2026-10-08T14:30"}

	changes := BuildAuditChanges(ReceiptAuditFields, before, after)
	byField := make(map[string]AuditChange, len(changes))
	for _, change := range changes {
		byField[change.Field] = change
	}

	if got := byField["amount"]; got.From == nil || *got.From != "R$ 2.500,00" || got.To == nil || *got.To != "R$ 2.600,50" {
		t.Fatalf("valor monetário formatado errado: %+v", got)
	}
	if got := byField["dueDate"]; got.From == nil || *got.From != "10/09/2026" || got.To == nil || *got.To != "10/10/2026" {
		t.Fatalf("data formatada errada: %+v", got)
	}
	if got := byField["category"]; got.From == nil || *got.From != "Aluguel" || got.To == nil || *got.To != "Avulsa" {
		t.Fatalf("categoria formatada errada: %+v", got)
	}
	if got := byField["paidAt"]; got.From != nil || got.To == nil || *got.To != "08/10/2026 14:30" {
		t.Fatalf("pagamento formatado errado: %+v", got)
	}
}

func TestBuildAuditChangesOnCreationAndDeletion(t *testing.T) {
	after := map[string]string{"name": "Clarice Lispector", "email": "clarice@exemplo.com", "phone": "", "cpf": ""}
	creation := BuildAuditChanges(TenantAuditFields, nil, after)
	if len(creation) != 2 {
		t.Fatalf("criação deve registrar apenas campos preenchidos, veio %d: %+v", len(creation), creation)
	}
	for _, change := range creation {
		if change.From != nil {
			t.Fatalf("criação não pode ter valor anterior: %+v", change)
		}
	}

	deletion := BuildAuditChanges(TenantAuditFields, after, nil)
	if len(deletion) != 2 {
		t.Fatalf("exclusão deve registrar apenas campos preenchidos, veio %d: %+v", len(deletion), deletion)
	}
	for _, change := range deletion {
		if change.To != nil {
			t.Fatalf("exclusão não pode ter valor novo: %+v", change)
		}
	}
}

func TestFormatAuditValueBlankBecomesNil(t *testing.T) {
	if value := FormatAuditValue(AuditFormatText, "   "); value != nil {
		t.Fatalf("valor em branco deve virar nulo, veio %q", *value)
	}
	if value := FormatAuditValue(AuditFormatLeaseStatus, "season"); value == nil || *value != "Temporada" {
		t.Fatalf("situação de contrato formatada errada: %+v", value)
	}
}
