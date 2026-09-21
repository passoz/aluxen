package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// ── enums.go ───────────────────────────────────────────────────────

func TestCovDomLeaseStatusLabel(t *testing.T) {
	cases := map[LeaseStatus]string{
		LeaseStatusActive:        "Ativo",
		LeaseStatusSuspended:     "Suspenso",
		LeaseStatusSeason:        "Temporada",
		LeaseStatusTerminated:    "Encerrado",
		LeaseStatus("arquivado"): "arquivado",
	}
	for status, want := range cases {
		if got := LeaseStatusLabel(status); got != want {
			t.Fatalf("LeaseStatusLabel(%q) = %q, quer %q", status, got, want)
		}
	}
}

func TestCovDomLeaseStatusFlags(t *testing.T) {
	blocks := map[LeaseStatus]bool{
		LeaseStatusActive:     true,
		LeaseStatusSeason:     true,
		LeaseStatusSuspended:  false,
		LeaseStatusTerminated: false,
	}
	create := map[LeaseStatus]bool{
		LeaseStatusActive:     true,
		LeaseStatusSuspended:  true,
		LeaseStatusSeason:     true,
		LeaseStatusTerminated: false,
	}
	update := map[LeaseStatus]bool{
		LeaseStatusActive:     true,
		LeaseStatusSuspended:  true,
		LeaseStatusSeason:     true,
		LeaseStatusTerminated: true,
	}
	for status := range blocks {
		if got := status.BlocksOverlap(); got != blocks[status] {
			t.Fatalf("%q.BlocksOverlap() = %v", status, got)
		}
		if got := status.SelectableOnCreate(); got != create[status] {
			t.Fatalf("%q.SelectableOnCreate() = %v", status, got)
		}
		if got := status.SelectableOnUpdate(); got != update[status] {
			t.Fatalf("%q.SelectableOnUpdate() = %v", status, got)
		}
	}
}

func TestCovDomReceiptCategoryLabel(t *testing.T) {
	cases := map[ReceiptCategory]string{
		ReceiptCategoryRent:          "Aluguel",
		ReceiptCategoryExtra:         "Avulsa",
		ReceiptCategoryCleaning:      "Taxa de limpeza",
		ReceiptCategoryConsumables:   "Consumíveis",
		ReceiptCategoryDepositRefund: "Devolução de caução",
		ReceiptCategory("taxa-lixo"): "taxa-lixo",
	}
	for category, want := range cases {
		if got := ReceiptCategoryLabel(category); got != want {
			t.Fatalf("ReceiptCategoryLabel(%q) = %q, quer %q", category, got, want)
		}
	}
}

func TestCovDomReceiptStatusLabel(t *testing.T) {
	cases := map[ReceiptStatus]string{
		ReceiptStatusPaid:     "Pago",
		ReceiptStatusPartial:  "Parcial",
		ReceiptStatusOverdue:  "Vencido",
		ReceiptStatusDue:      "A vencer",
		ReceiptStatus("xpto"): "A vencer",
	}
	for status, want := range cases {
		if got := ReceiptStatusLabel(status); got != want {
			t.Fatalf("ReceiptStatusLabel(%q) = %q, quer %q", status, got, want)
		}
	}
}

func TestCovDomDirectionAndOriginLabels(t *testing.T) {
	directions := map[WhatsAppDirection]string{
		WhatsAppDirectionIn:       "Recebida",
		WhatsAppDirectionOut:      "Enviada",
		WhatsAppDirection("both"): "both",
	}
	for direction, want := range directions {
		if got := WhatsAppDirectionLabel(direction); got != want {
			t.Fatalf("WhatsAppDirectionLabel(%q) = %q", direction, got)
		}
	}

	origins := map[AuditOrigin]string{
		AuditOriginWeb:       "Painel",
		AuditOriginPublic:    "Link público",
		AuditOriginClient:    "Área do Cliente",
		AuditOriginJob:       "Rotina automática",
		AuditOrigin("batch"): "batch",
	}
	for origin, want := range origins {
		if got := AuditOriginLabel(origin); got != want {
			t.Fatalf("AuditOriginLabel(%q) = %q", origin, got)
		}
	}
}

// ── entities.go ────────────────────────────────────────────────────

func TestCovDomPointerHelpers(t *testing.T) {
	if got := Str(nil); got != "" {
		t.Fatalf("Str(nil) = %q", got)
	}
	if got := Str(StrPtr("valor")); got != "valor" {
		t.Fatalf("Str(ponteiro) = %q", got)
	}
	if got := Float(nil); got != 0 {
		t.Fatalf("Float(nil) = %v", got)
	}
	if got := Float(FloatPtr(12.5)); got != 12.5 {
		t.Fatalf("Float(ponteiro) = %v", got)
	}
}

func TestCovDomMustChangePassword(t *testing.T) {
	empty := ""
	changed := "2026-01-01 10:00:00"

	if !(User{}).MustChangePassword() {
		t.Fatal("usuário sem troca de senha não está travado")
	}
	if !(User{PasswordChangedAt: &empty}).MustChangePassword() {
		t.Fatal("data vazia equivale a senha nunca trocada")
	}
	if (User{PasswordChangedAt: &changed}).MustChangePassword() {
		t.Fatal("usuário com senha trocada não pode ser travado")
	}

	if !(Tenant{}).MustChangePassword() {
		t.Fatal("locatário sem troca de senha não está travado")
	}
	if !(Tenant{PasswordChangedAt: &empty}).MustChangePassword() {
		t.Fatal("data vazia equivale a senha nunca trocada")
	}
	if (Tenant{PasswordChangedAt: &changed}).MustChangePassword() {
		t.Fatal("locatário com senha trocada não pode ser travado")
	}
}

// ── audit_catalog.go ───────────────────────────────────────────────

func TestCovDomAuditCatalogIsComplete(t *testing.T) {
	actions := []string{
		ActionTenantCreate, ActionTenantUpdate, ActionTenantDelete,
		ActionPropertyCreate, ActionPropertyUpdate, ActionPropertyDelete,
		ActionLeaseCreate, ActionLeaseUpdate, ActionLeaseTerminate, ActionLeaseSuspend,
		ActionLeaseReactivate, ActionLeaseDelete,
		ActionReceiptCreate, ActionReceiptUpdate, ActionReceiptPay, ActionReceiptBatchPay,
		ActionReceiptGenerateMonth, ActionReceiptDelete, ActionReceiptBatchDelete,
		ActionSettingLandlord, ActionSettingAppURL, ActionUserPassword,
		ActionClientLogin, ActionClientLoginFailed, ActionClientLogout, ActionClientPassword,
		ActionLogin, ActionLoginFailed, ActionLogout,
		ActionDocumentReceipt, ActionDocumentContract, ActionDocumentInspection, ActionDocumentDeposit,
		ActionWhatsAppSend, ActionWhatsAppSendFailed, ActionWhatsAppLogout, ActionWhatsAppMessageReceived,
		ActionBackupExport, ActionBackupImport, ActionHistoryExport,
		ActionDepositRefund, ActionInspectionCreate, ActionInspectionUpdate, ActionInspectionDelete,
		ActionPaymentProofAccepted, ActionPaymentProofReview, ActionPaymentProofRejected,
		ActionDocumentIntakeCreated, ActionCollectionSent, ActionAdjustmentApplied,
		ActionAgentAnswered, ActionAgentEscalated, ActionMaintenanceOpened,
	}
	for _, action := range actions {
		meta := AuditActionMetaFor(action)
		if meta.Label == "" || meta.Label == action {
			t.Fatalf("ação %q sem rótulo em português", action)
		}
		if meta.Entity == "" {
			t.Fatalf("ação %q sem área da trilha", action)
		}
		if meta.Tone == "" {
			t.Fatalf("ação %q sem tom visual", action)
		}
		if label := EntityLabel(meta.Entity); label == "" || label == meta.Entity {
			t.Fatalf("ação %q usa área %q sem rótulo", action, meta.Entity)
		}
	}
}

func TestCovDomAuditActionMetaFallback(t *testing.T) {
	meta := AuditActionMetaFor("desconhecido.acao")
	if meta.Action != "desconhecido.acao" || meta.Label != "desconhecido.acao" {
		t.Fatalf("fallback devolveu %+v", meta)
	}
	if meta.Entity != "" || meta.Tone != ToneUpdate {
		t.Fatalf("fallback deveria ser sem área e tom update: %+v", meta)
	}
}

func TestCovDomEntityLabel(t *testing.T) {
	cases := map[string]string{
		EntityTenant:     "Inquilino",
		EntityProperty:   "Imóvel",
		EntityLease:      "Contrato",
		EntityReceipt:    "Pagamento",
		EntitySetting:    "Configuração",
		EntityUser:       "Usuário",
		EntityDocument:   "Documento",
		EntityWhatsApp:   "WhatsApp",
		EntityBackup:     "Backup",
		EntityDeposit:    "Caução",
		EntityInspection: "Vistoria",
		EntityPayment:    "Comprovante",
		EntityIntake:     "Cadastro por documento",
		EntityCollection: "Cobrança",
		"desconhecida":   "desconhecida",
	}
	for entity, want := range cases {
		if got := EntityLabel(entity); got != want {
			t.Fatalf("EntityLabel(%q) = %q, quer %q", entity, got, want)
		}
	}
}

func TestCovDomMetadataLabel(t *testing.T) {
	cases := map[string]string{
		"month":      "Mês de referência",
		"count":      "Quantidade",
		"batch":      "Itens no lote",
		"channel":    "Canal",
		"signature":  "Com assinatura",
		"phone":      "Telefone",
		"jobId":      "Protocolo de envio",
		"kind":       "Tipo",
		"reason":     "Motivo",
		"username":   "Usuário",
		"tenants":    "Inquilinos",
		"properties": "Imóveis",
		"leases":     "Contratos",
		"receipts":   "Pagamentos",
		"fileName":   "Arquivo",
		"confidence": "Confiança",
		"extracted":  "Valor lido",
		"receiptId":  "Pagamento",
		"amount":     "Valor",
		"stage":      "Etapa",
		"outra":      "outra",
	}
	for key, want := range cases {
		if got := MetadataLabel(key); got != want {
			t.Fatalf("MetadataLabel(%q) = %q, quer %q", key, got, want)
		}
	}
}

// ── audit_fields.go ────────────────────────────────────────────────

func TestCovDomFormatAuditValue(t *testing.T) {
	cases := []struct {
		format AuditValueFormat
		raw    string
		want   string
	}{
		{AuditFormatText, "  Carlos  ", "Carlos"},
		{AuditFormatCurrency, "2500.5", "R$ 2.500,50"},
		{AuditFormatCurrency, "abc", "R$ 0,00"},
		{AuditFormatDate, "2026-03-10", "10/03/2026"},
		{AuditFormatDateTime, "2026-03-10T08:05", "10/03/2026 08:05"},
		{AuditFormatLeaseStatus, "season", "Temporada"},
		{AuditFormatReceiptCategory, "cleaning", "Taxa de limpeza"},
		{AuditFormatDepositStatus, "partially_refunded", "Devolvida parcialmente"},
		{AuditFormatInspectionKind, "checkout", "Vistoria de saída"},
		{AuditFormatCondition, "otima", "Ótima"},
		{AuditValueFormat("sem-formato"), "  bruto ", "bruto"},
	}
	for _, tc := range cases {
		got := FormatAuditValue(tc.format, tc.raw)
		if got == nil || *got != tc.want {
			t.Fatalf("FormatAuditValue(%q, %q) = %v, quer %q", tc.format, tc.raw, got, tc.want)
		}
	}
	if got := FormatAuditValue(AuditFormatText, "   "); got != nil {
		t.Fatalf("valor em branco deveria ser nulo, veio %q", *got)
	}
}

func TestCovDomBuildAuditChanges(t *testing.T) {
	// criação: o que existia vazio vira conteúdo
	created := BuildAuditChanges(TenantAuditFields, nil, map[string]string{"name": "Carlos", "cpf": "111.222.333-44"})
	if len(created) != 2 {
		t.Fatalf("criação deveria apontar 2 campos, veio %d: %+v", len(created), created)
	}
	if created[0].Field != "name" || created[0].Label != "Nome" || created[0].From != nil {
		t.Fatalf("linha de criação inesperada: %+v", created[0])
	}
	if created[0].To == nil || *created[0].To != "Carlos" {
		t.Fatalf("valor de destino errado: %+v", created[0].To)
	}

	// exclusão: o conteúdo vira vazio
	removed := BuildAuditChanges(TenantAuditFields, map[string]string{"name": "Carlos"}, nil)
	if len(removed) != 1 || removed[0].From == nil || *removed[0].From != "Carlos" || removed[0].To != nil {
		t.Fatalf("exclusão inesperada: %+v", removed)
	}

	// campos iguais somem, mesmo com espaços diferentes
	unchanged := BuildAuditChanges(TenantAuditFields,
		map[string]string{"name": "Carlos", "email": "c@x.com"},
		map[string]string{"name": " Carlos ", "email": "c@x.com"})
	if len(unchanged) != 0 {
		t.Fatalf("nada mudou, mas veio %+v", unchanged)
	}

	// moeda é comparada já formatada
	money := BuildAuditChanges(LeaseAuditFields,
		map[string]string{"rentAmount": "2500"},
		map[string]string{"rentAmount": "2500.00"})
	if len(money) != 0 {
		t.Fatalf("2500 e 2500.00 são iguais depois de formatados: %+v", money)
	}
	money = BuildAuditChanges(LeaseAuditFields,
		map[string]string{"rentAmount": "2500"},
		map[string]string{"rentAmount": "2600"})
	if len(money) != 1 || money[0].Field != "rentAmount" || money[0].Label != "Aluguel" {
		t.Fatalf("reajuste não apareceu: %+v", money)
	}
	if *money[0].From != "R$ 2.500,00" || *money[0].To != "R$ 2.600,00" {
		t.Fatalf("valores formatados inesperados: %+v", money[0])
	}
}

func TestCovDomFormatNumber(t *testing.T) {
	cases := map[float64]string{0: "0", 2500: "2500", 12.5: "12.5", -3: "-3"}
	for value, want := range cases {
		if got := FormatNumber(value); got != want {
			t.Fatalf("FormatNumber(%v) = %q, quer %q", value, got, want)
		}
	}
}

func TestCovDomUnderThousandInWordsBoundaries(t *testing.T) {
	// zero é vazio: quem escreve o número já tratou o valor nulo.
	if got := underThousandInWords(0); got != "" {
		t.Fatalf("underThousandInWords(0) = %q, quer vazio", got)
	}
	cases := map[int]string{
		200: "duzentos",
		20:  "vinte",
		30:  "trinta",
	}
	for value, want := range cases {
		if got := underThousandInWords(value); got != want {
			t.Fatalf("underThousandInWords(%d) = %q, quer %q", value, got, want)
		}
	}
}

func TestCovDomEncodeJSONListUnsupportedValue(t *testing.T) {
	// um valor que o json não sabe serializar devolve o erro cru.
	if _, err := encodeJSONList([]chan int{make(chan int)}); err == nil {
		t.Fatal("valor não serializável deveria devolver erro")
	}
}

// ── rules.go ───────────────────────────────────────────────────────

func TestCovDomOverlapsBoundaries(t *testing.T) {
	// encostar no limite conta como sobreposição (início de um é o fim do outro)
	if !Overlaps("2026-01-01", "2026-01-31", "2026-01-31", "2026-02-28") {
		t.Fatal("períodos encostados deveriam sobrepor")
	}
	if Overlaps("2026-01-01", "2026-01-30", "2026-01-31", "2026-02-28") {
		t.Fatal("períodos separados não podem sobrepor")
	}
	if !Overlaps("2026-01-01", "2026-12-31", "2026-06-01", "2026-06-30") {
		t.Fatal("período contido deveria sobrepor")
	}
}

func TestCovDomDaysInMonth(t *testing.T) {
	cases := []struct{ year, month, days int }{
		{2024, 2, 29}, // bissexto
		{2023, 2, 28},
		{2000, 2, 29}, // divisível por 400
		{1900, 2, 28}, // divisível por 100 mas não por 400
		{2026, 1, 31},
		{2026, 4, 30},
		{2026, 12, 31},
	}
	for _, tc := range cases {
		if got := DaysInMonth(tc.year, tc.month); got != tc.days {
			t.Fatalf("DaysInMonth(%d, %d) = %d, quer %d", tc.year, tc.month, got, tc.days)
		}
	}
}

func TestCovDomDueDateForMonth(t *testing.T) {
	cases := []struct {
		year, month, dueDay int
		want                string
	}{
		{2026, 1, 10, "2026-01-10"},
		{2026, 2, 31, "2026-02-28"}, // dia contratado não existe
		{2024, 2, 31, "2024-02-29"}, // bissexto
		{2026, 4, 31, "2026-04-30"},
		{2026, 5, 0, "2026-05-01"}, // dia inválido cai para o primeiro
		{2026, 6, -3, "2026-06-01"},
	}
	for _, tc := range cases {
		if got := DueDateForMonth(tc.year, tc.month, tc.dueDay); got != tc.want {
			t.Fatalf("DueDateForMonth(%d, %d, %d) = %q, quer %q", tc.year, tc.month, tc.dueDay, got, tc.want)
		}
	}
}

func TestCovDomMonthKeys(t *testing.T) {
	if got := MonthKey(2026, 3); got != "2026-03" {
		t.Fatalf("MonthKey = %q", got)
	}
	if got := GeneratedKeyForMonth("lease-1", 2026, 3); got != "lease-1:2026-03" {
		t.Fatalf("GeneratedKeyForMonth = %q", got)
	}
}

func TestCovDomReceiptStatusOf(t *testing.T) {
	today := time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)
	ptr := func(v float64) *float64 { return &v }
	paidAt := func(v string) *string { return &v }

	cases := []struct {
		name    string
		receipt Receipt
		want    ReceiptStatus
	}{
		{"pago com valor explícito igual", Receipt{Amount: 2500, PaidAmount: ptr(2500), DueDate: "2026-09-10"}, ReceiptStatusPaid},
		{"pago com valor explícito maior", Receipt{Amount: 2500, PaidAmount: ptr(3000), DueDate: "2026-09-10"}, ReceiptStatusPaid},
		{"parcial", Receipt{Amount: 2500, PaidAmount: ptr(1000), DueDate: "2026-09-10"}, ReceiptStatusPartial},
		{"pago só pela data de pagamento", Receipt{Amount: 2500, PaidAt: paidAt("2026-09-09"), DueDate: "2026-09-10"}, ReceiptStatusPaid},
		{"em aberto vencido", Receipt{Amount: 2500, DueDate: "2026-09-10"}, ReceiptStatusOverdue},
		{"em aberto a vencer", Receipt{Amount: 2500, DueDate: "2026-09-30"}, ReceiptStatusDue},
		{"vencimento com hora ainda conta", Receipt{Amount: 2500, DueDate: "2026-09-10T09:00"}, ReceiptStatusOverdue},
		{"vencimento inválido fica a vencer", Receipt{Amount: 2500, DueDate: "sem-data"}, ReceiptStatusDue},
		{"valor pago zero não quita", Receipt{Amount: 2500, PaidAmount: ptr(0), DueDate: "2026-09-10"}, ReceiptStatusOverdue},
	}
	for _, tc := range cases {
		if got := ReceiptStatusOf(tc.receipt, today); got != tc.want {
			t.Fatalf("%s: ReceiptStatusOf = %q, quer %q", tc.name, got, tc.want)
		}
	}
}

func TestCovDomValidators(t *testing.T) {
	validEmails := []string{"carlos@exemplo.com.br", " a@b.co ", "x.y+z@dominio.io"}
	for _, email := range validEmails {
		if err := ValidateEmail(email); err != nil {
			t.Fatalf("ValidateEmail(%q) = %v", email, err)
		}
	}
	invalidEmails := []string{"", "sem-arroba", "a@b", "@b.com", "a b@c.com", "a@b c.com"}
	for _, email := range invalidEmails {
		err := ValidateEmail(email)
		if err == nil {
			t.Fatalf("ValidateEmail(%q) deveria falhar", email)
		}
		if !IsValidation(err) || ValidationMessage(err) != "Informe um e-mail válido." {
			t.Fatalf("mensagem de e-mail inesperada: %v", err)
		}
	}

	for _, day := range []int{1, 15, 31} {
		if err := ValidateDueDay(day); err != nil {
			t.Fatalf("ValidateDueDay(%d) = %v", day, err)
		}
	}
	for _, day := range []int{0, 32, -1} {
		if err := ValidateDueDay(day); err == nil {
			t.Fatalf("ValidateDueDay(%d) deveria falhar", day)
		}
	}

	if err := ValidateRentAmount(0.01); err != nil {
		t.Fatalf("valor positivo recusado: %v", err)
	}
	if err := ValidateRentAmount(0); err == nil {
		t.Fatal("aluguel zero deveria falhar")
	}
	if err := ValidateRentAmount(-1); err == nil {
		t.Fatal("aluguel negativo deveria falhar")
	}

	if err := ValidatePeriod("2026-01-01", "2026-01-01"); err != nil {
		t.Fatalf("mesmo dia deveria valer: %v", err)
	}
	if err := ValidatePeriod("2026-01-01", "2026-02-01"); err != nil {
		t.Fatalf("período válido recusado: %v", err)
	}
	for _, tc := range [][2]string{{"", "2026-01-01"}, {"2026-01-01", ""}, {"2026-02-01", "2026-01-01"}} {
		if err := ValidatePeriod(tc[0], tc[1]); err == nil {
			t.Fatalf("período %v deveria falhar", tc)
		}
	}
}

// ── money_words.go ─────────────────────────────────────────────────

func TestCovDomNumberInWordsRanges(t *testing.T) {
	cases := map[int]string{
		0:          "zero",
		9:          "nove",
		-7:         "menos sete",
		10:         "dez",
		19:         "dezenove",
		20:         "vinte",
		42:         "quarenta e dois",
		100:        "cem",
		101:        "cento e um",
		999:        "novecentos e noventa e nove",
		1000:       "mil",
		2000:       "dois mil",
		1234:       "mil e duzentos e trinta e quatro",
		1000000:    "um milhão",
		2000000:    "dois milhões",
		1000001:    "um milhão e um",
		1000100:    "um milhão e cem",
		1100100:    "um milhão, cem mil e cem",
		1000000000: "um bilhão",
		2000000000: "dois bilhões",
		2000000100: "dois bilhões e cem",
		3000000000: "três bilhões",
	}
	for value, want := range cases {
		if got := NumberInWords(value); got != want {
			t.Fatalf("NumberInWords(%d) = %q, quer %q", value, got, want)
		}
	}
}

func TestCovDomMoneyInWords(t *testing.T) {
	cases := map[float64]string{
		0:       "zero reais",
		1:       "um real",
		2:       "dois reais",
		1.01:    "um real e um centavo",
		1.02:    "um real e dois centavos",
		2500:    "dois mil e quinhentos reais",
		1234.56: "mil e duzentos e trinta e quatro reais e cinquenta e seis centavos",
		0.999:   "um real", // centavos arredondam para o real seguinte
	}
	for value, want := range cases {
		if got := MoneyInWords(value); got != want {
			t.Fatalf("MoneyInWords(%v) = %q, quer %q", value, got, want)
		}
	}
}

func TestCovDomFormatFloat(t *testing.T) {
	if got := FormatFloat(1.5); got != "1.50" {
		t.Fatalf("FormatFloat(1.5) = %q", got)
	}
	if got := FormatFloat(2500); got != "2500.00" {
		t.Fatalf("FormatFloat(2500) = %q", got)
	}
}

// ── payments.go ────────────────────────────────────────────────────

func TestCovDomJobLabelsAndRetry(t *testing.T) {
	labels := map[JobStatus]string{
		JobStatusQueued:  "Na fila",
		JobStatusRunning: "Em execução",
		JobStatusDone:    "Concluído",
		JobStatusFailed:  "Falhou",
		JobStatus("x"):   "x",
	}
	for status, want := range labels {
		if got := JobStatusLabel(status); got != want {
			t.Fatalf("JobStatusLabel(%q) = %q", status, got)
		}
	}

	if !(Job{Attempts: 0, MaxAttempts: 3}).CanRetry() {
		t.Fatal("trabalho novo pode ser tentado")
	}
	if !(Job{Attempts: 2, MaxAttempts: 3}).CanRetry() {
		t.Fatal("trabalho com tentativa restante pode ser repetido")
	}
	if (Job{Attempts: 3, MaxAttempts: 3}).CanRetry() {
		t.Fatal("trabalho sem tentativas restantes não pode repetir")
	}
}

func TestCovDomPaymentProofLabels(t *testing.T) {
	statuses := map[PaymentProofStatus]string{
		PaymentProofStatusPending:  "Lendo",
		PaymentProofStatusAccepted: "Aceito",
		PaymentProofStatusReview:   "Em revisão",
		PaymentProofStatusRejected: "Recusado",
		PaymentProofStatus("x"):    "x",
	}
	for status, want := range statuses {
		if got := PaymentProofStatusLabel(status); got != want {
			t.Fatalf("PaymentProofStatusLabel(%q) = %q", status, got)
		}
	}

	decisions := map[PaymentDecision]string{
		PaymentDecisionPending: "Pendente",
		PaymentDecisionAuto:    "Automática",
		PaymentDecisionManual:  "Manual",
		PaymentDecision("x"):   "Pendente",
	}
	for decision, want := range decisions {
		if got := PaymentDecisionLabel(decision); got != want {
			t.Fatalf("PaymentDecisionLabel(%q) = %q", decision, got)
		}
	}
}

func TestCovDomPaymentProofExtraction(t *testing.T) {
	empty := PaymentProof{}.Extraction()
	if empty != (PaymentExtraction{}) {
		t.Fatalf("extração vazia deveria ser zero: %+v", empty)
	}

	proof := PaymentProof{
		ExtractedAmount:     FloatPtr(2500),
		ExtractedDate:       StrPtr("2026-09-10"),
		ExtractedPayer:      StrPtr("Carlos"),
		ExtractedPayee:      StrPtr("Locador"),
		ExtractedIdentifier: StrPtr("PIX-1"),
		Confidence:          FloatPtr(0.9),
	}
	got := proof.Extraction()
	if got.Amount != 2500 || got.Date != "2026-09-10" || got.Payee != "Locador" || got.Confidence != 0.9 {
		t.Fatalf("extração inesperada: %+v", got)
	}
}

func TestCovDomIntakeLabelsAndValidity(t *testing.T) {
	kinds := map[IntakeKind]string{
		IntakeKindLease:        "Contrato antigo",
		IntakeKindIDDocument:   "Documento de identidade",
		IntakeKindAddressProof: "Comprovante de residência",
		IntakeKind("outro"):    "outro",
	}
	for kind, want := range kinds {
		if got := IntakeKindLabel(kind); got != want {
			t.Fatalf("IntakeKindLabel(%q) = %q", kind, got)
		}
	}
	if !IntakeKindLease.Valid() || !IntakeKindIDDocument.Valid() || !IntakeKindAddressProof.Valid() {
		t.Fatal("tipos conhecidos deveriam ser válidos")
	}
	if IntakeKind("outro").Valid() {
		t.Fatal("tipo desconhecido não pode ser válido")
	}

	statuses := map[IntakeStatus]string{
		IntakeStatusPending:   "Aguardando conferência",
		IntakeStatusConfirmed: "Confirmado",
		IntakeStatusDiscarded: "Descartado",
		IntakeStatus("x"):     "x",
	}
	for status, want := range statuses {
		if got := IntakeStatusLabel(status); got != want {
			t.Fatalf("IntakeStatusLabel(%q) = %q", status, got)
		}
	}
}

func TestCovDomEvaluatePaymentMatchRejections(t *testing.T) {
	today := time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)
	open := &Receipt{ID: "r1", DueDate: "2026-09-20", Amount: 2500}
	base := PaymentMatchInput{
		Extracted:    PaymentExtraction{Amount: 2500, Date: "2026-09-20", Payee: "Locador Silva", Confidence: 0.9},
		Receipt:      open,
		LandlordName: "Locador Silva",
		Today:        today,
		WindowDays:   5,
	}
	if !EvaluatePaymentMatch(base).Approved {
		t.Fatal("caso base deveria aprovar")
	}

	lowConfidence := base
	lowConfidence.MinConfidence = 0.95
	if result := EvaluatePaymentMatch(lowConfidence); result.Approved || result.Reason == "" {
		t.Fatalf("confiança abaixo do limite deveria reprovar: %+v", result)
	}

	noReceipt := base
	noReceipt.Receipt = nil
	if result := EvaluatePaymentMatch(noReceipt); result.Approved || !strings.Contains(result.Reason, "lançamento") {
		t.Fatalf("sem lançamento em aberto deveria reprovar: %+v", result)
	}

	alreadyPaid := base
	paidAt := "2026-09-20"
	alreadyPaid.Receipt = &Receipt{ID: "r2", DueDate: "2026-09-20", Amount: 2500, PaidAt: &paidAt}
	if result := EvaluatePaymentMatch(alreadyPaid); result.Approved || !strings.Contains(result.Reason, "quitado") {
		t.Fatalf("lançamento quitado deveria reprovar: %+v", result)
	}

	zeroAmount := base
	zeroAmount.Extracted.Amount = 0
	if result := EvaluatePaymentMatch(zeroAmount); result.Approved || !strings.Contains(result.Reason, "valor") {
		t.Fatalf("valor ausente deveria reprovar: %+v", result)
	}

	mismatch := base
	mismatch.Extracted.Amount = 2400
	if result := EvaluatePaymentMatch(mismatch); result.Approved || !strings.Contains(result.Reason, "diferente") {
		t.Fatalf("valor diferente deveria reprovar: %+v", result)
	}

	wrongPayee := base
	wrongPayee.Extracted.Payee = "Outra Pessoa"
	if result := EvaluatePaymentMatch(wrongPayee); result.Approved || !strings.Contains(result.Reason, "destinatário") {
		t.Fatalf("destinatário errado deveria reprovar: %+v", result)
	}

	outOfWindow := base
	outOfWindow.Extracted.Date = "2026-08-01"
	if result := EvaluatePaymentMatch(outOfWindow); result.Approved || !strings.Contains(result.Reason, "data") {
		t.Fatalf("data fora da janela deveria reprovar: %+v", result)
	}
}

func TestCovDomPayeeMatchesLandlord(t *testing.T) {
	cases := []struct {
		name  string
		payee string
		lor   string
		cpf   string
		want  bool
	}{
		{"vazio", "   ", "Locador Silva", "111.222.333-44", false},
		{"CPF pontuado casa", "111.222.333-44", "Locador Silva", "11122233344", true},
		{"CPF do comprovante sem pontos casa", "Pagamento PIX 11122233344", "Locador Silva", "111.222.333-44", true},
		{"nome igual completo", "LOCADOR SILVA", "Locador Silva", "", true},
		{"nome sem acento casa", "Jose Antonio", "José Antônio", "", true},
		{"nome composto por primeiro e último", "Recebedor: Jose Silva ME", "José Carlos da Silva", "", true},
		{"só o primeiro nome não basta", "Jose", "José Carlos da Silva", "", true},
		{"nome estranho não casa", "Maria Souza", "José Carlos da Silva", "", false},
		{"locador de um nome só não casa", "Maria Souza", "José", "", false},
		{"sem nome cadastrado não casa", "Qualquer Um", "", "", false},
	}
	for _, tc := range cases {
		if got := payeeMatchesLandlord(tc.payee, tc.lor, tc.cpf); got != tc.want {
			t.Fatalf("%s: payeeMatchesLandlord(%q, %q, %q) = %v, quer %v",
				tc.name, tc.payee, tc.lor, tc.cpf, got, tc.want)
		}
	}
}

func TestCovDomDateInsideWindow(t *testing.T) {
	today := time.Date(2026, 9, 21, 12, 0, 0, 0, time.Local)
	cases := []struct {
		name      string
		date, due string
		window    int
		want      bool
	}{
		{"data no vencimento", "2026-09-20", "2026-09-20", 5, true},
		{"limite inferior da janela", "2026-09-15", "2026-09-20", 5, true},
		{"um dia antes do limite", "2026-09-14", "2026-09-20", 5, false},
		{"fora do futuro distante", "2026-12-01", "2026-09-20", 5, false},
		{"amanhã ainda é aceito", "2026-09-22", "2026-09-20", 5, true},
		{"depois de amanhã não", "2026-09-23", "2026-09-20", 5, false},
		{"data inválida", "sem-data", "2026-09-20", 5, false},
		{"vencimento inválido não restringe", "2026-09-20", "sem-data", 5, true},
		{"janela zero usa o padrão", "2026-09-20", "2026-09-20", 0, true},
		{"data com hora", "2026-09-20T08:00", "2026-09-20", 5, true},
	}
	for _, tc := range cases {
		if got := dateInsideWindow(tc.date, tc.due, today, tc.window); got != tc.want {
			t.Fatalf("%s: dateInsideWindow(%q, %q, %d) = %v", tc.name, tc.date, tc.due, tc.window, got)
		}
	}
}

// ── season.go ──────────────────────────────────────────────────────

func TestCovDomDepositStatusLabel(t *testing.T) {
	cases := map[DepositStatus]string{
		DepositStatusNone:              "Sem caução",
		DepositStatusHeld:              "Retida",
		DepositStatusRefunded:          "Devolvida",
		DepositStatusPartiallyRefunded: "Devolvida parcialmente",
		DepositStatusForfeited:         "Retida integralmente",
		DepositStatus("x"):             "Sem caução",
	}
	for status, want := range cases {
		if got := DepositStatusLabel(status); got != want {
			t.Fatalf("DepositStatusLabel(%q) = %q", status, got)
		}
	}
}

func TestCovDomHasDeposit(t *testing.T) {
	cases := []struct {
		amount *float64
		status DepositStatus
		want   bool
	}{
		{nil, DepositStatusHeld, false},
		{FloatPtr(0), DepositStatusHeld, false},
		{FloatPtr(4800), DepositStatusNone, false},
		{FloatPtr(4800), DepositStatusHeld, true},
	}
	for _, tc := range cases {
		if got := HasDeposit(tc.amount, tc.status); got != tc.want {
			t.Fatalf("HasDeposit(%v, %q) = %v", tc.amount, tc.status, got)
		}
	}
}

func TestCovDomValidateDeposit(t *testing.T) {
	if err := ValidateDeposit(nil, ""); err != nil {
		t.Fatalf("sem caução e sem situação é válido: %v", err)
	}
	if err := ValidateDeposit(nil, DepositStatusNone); err != nil {
		t.Fatalf("sem caução explícita é válido: %v", err)
	}
	if err := ValidateDeposit(nil, DepositStatusHeld); err == nil {
		t.Fatal("situação com caução exige valor")
	}
	if err := ValidateDeposit(FloatPtr(-1), DepositStatusHeld); err == nil {
		t.Fatal("caução negativa deveria falhar")
	}
	if err := ValidateDeposit(FloatPtr(0), DepositStatusHeld); err == nil {
		t.Fatal("caução zero deveria falhar")
	}
	if err := ValidateDeposit(FloatPtr(4800), DepositStatusHeld); err != nil {
		t.Fatalf("caução válida recusada: %v", err)
	}
}

func TestCovDomComputeDepositRefund(t *testing.T) {
	if _, err := ComputeDepositRefund(0, 0); err == nil {
		t.Fatal("sem caução retida não há devolução")
	}
	if _, err := ComputeDepositRefund(4800, -1); err == nil {
		t.Fatal("devolução negativa deveria falhar")
	}
	if _, err := ComputeDepositRefund(4800, 5000); err == nil {
		t.Fatal("devolução maior que a caução deveria falhar")
	}

	forfeited, err := ComputeDepositRefund(4800, 0)
	if err != nil {
		t.Fatalf("retenção integral: %v", err)
	}
	if forfeited.Status != DepositStatusForfeited || forfeited.RefundedAmount != 0 {
		t.Fatalf("retenção integral inesperada: %+v", forfeited)
	}

	partial, err := ComputeDepositRefund(4800, 2000)
	if err != nil {
		t.Fatalf("devolução parcial: %v", err)
	}
	if partial.Status != DepositStatusPartiallyRefunded || partial.RefundedAmount != 2000 {
		t.Fatalf("devolução parcial inesperada: %+v", partial)
	}

	full, err := ComputeDepositRefund(4800, 4800)
	if err != nil {
		t.Fatalf("devolução integral: %v", err)
	}
	if full.Status != DepositStatusRefunded || full.RefundedAmount != 4800 {
		t.Fatalf("devolução integral inesperada: %+v", full)
	}
}

func TestCovDomInspectionLabelsAndValidity(t *testing.T) {
	kinds := map[InspectionKind]string{
		InspectionKindCheckIn:  "Vistoria de entrada",
		InspectionKindCheckOut: "Vistoria de saída",
		InspectionKind("x"):    "x",
	}
	for kind, want := range kinds {
		if got := InspectionKindLabel(kind); got != want {
			t.Fatalf("InspectionKindLabel(%q) = %q", kind, got)
		}
	}
	if !InspectionKindCheckIn.Valid() || !InspectionKindCheckOut.Valid() || InspectionKind("x").Valid() {
		t.Fatal("validade do tipo de vistoria errada")
	}

	conditions := map[InspectionCondition]string{
		InspectionConditionExcellent: "Ótima",
		InspectionConditionGood:      "Boa",
		InspectionConditionFair:      "Regular",
		InspectionConditionBad:       "Ruim",
		InspectionCondition("x"):     "x",
	}
	for condition, want := range conditions {
		if got := InspectionConditionLabel(condition); got != want {
			t.Fatalf("InspectionConditionLabel(%q) = %q", condition, got)
		}
	}
	for _, condition := range []InspectionCondition{
		InspectionConditionExcellent, InspectionConditionGood, InspectionConditionFair, InspectionConditionBad,
	} {
		if !condition.Valid() {
			t.Fatalf("%q deveria ser válida", condition)
		}
	}
	if InspectionCondition("x").Valid() {
		t.Fatal("estado desconhecido não é válido")
	}
}

func TestCovDomInspectionJSONRoundTrip(t *testing.T) {
	// lista vazia vira NULL e volta vazia
	raw, err := EncodeInspectionItems(nil)
	if err != nil || raw != nil {
		t.Fatalf("lista vazia de itens deveria virar nil: %v %v", raw, err)
	}
	items, err := DecodeInspectionItems(raw)
	if err != nil || len(items) != 0 {
		t.Fatalf("decodificar nil deveria dar lista vazia: %v %v", items, err)
	}
	blank := "   "
	items, err = DecodeInspectionItems(&blank)
	if err != nil || len(items) != 0 {
		t.Fatalf("decodificar branco deveria dar lista vazia: %v %v", items, err)
	}

	want := []InspectionItem{
		{Area: "Sala", Item: "Sofá", State: "bom", Notes: "sem manchas"},
		{Area: "Cozinha", Item: "Geladeira", State: "ótimo"},
	}
	raw, err = EncodeInspectionItems(want)
	if err != nil || raw == nil {
		t.Fatalf("codificar itens: %v", err)
	}
	back, err := DecodeInspectionItems(raw)
	if err != nil {
		t.Fatalf("decodificar itens: %v", err)
	}
	if len(back) != 2 || back[0] != want[0] || back[1] != want[1] {
		t.Fatalf("ida e volta dos itens perdeu dado: %+v", back)
	}

	broken := "{ não é json"
	if _, err := DecodeInspectionItems(&broken); err == nil {
		t.Fatal("JSON inválido de itens deveria falhar")
	} else if !IsValidation(err) {
		t.Fatalf("erro de itens deveria ser de validação: %v", err)
	}

	photosRaw, err := EncodeInspectionPhotos(nil)
	if err != nil || photosRaw != nil {
		t.Fatalf("lista vazia de fotos deveria virar nil: %v %v", photosRaw, err)
	}
	photosBack, err := DecodeInspectionPhotos(photosRaw)
	if err != nil || len(photosBack) != 0 {
		t.Fatalf("decodificar nil deveria dar lista vazia: %v %v", photosBack, err)
	}
	photos := []InspectionPhoto{{Name: "foto-1.jpg", Label: "Sala", Caption: "antes"}, {Name: "foto-2.jpg"}}
	photosRaw, err = EncodeInspectionPhotos(photos)
	if err != nil || photosRaw == nil {
		t.Fatalf("codificar fotos: %v", err)
	}
	photosBack, err = DecodeInspectionPhotos(photosRaw)
	if err != nil || len(photosBack) != 2 || photosBack[0] != photos[0] {
		t.Fatalf("ida e volta das fotos perdeu dado: %+v %v", photosBack, err)
	}
	if _, err := DecodeInspectionPhotos(&broken); err == nil || !IsValidation(err) {
		t.Fatalf("JSON inválido de fotos deveria ser erro de validação: %v", err)
	}
}

func TestCovDomValidateInspection(t *testing.T) {
	if err := ValidateInspection(InspectionKindCheckIn, InspectionConditionGood, "2026-12-20"); err != nil {
		t.Fatalf("vistoria válida recusada: %v", err)
	}
	if err := ValidateInspection(InspectionKind("x"), InspectionConditionGood, "2026-12-20"); err == nil {
		t.Fatal("tipo inválido deveria falhar")
	}
	if err := ValidateInspection(InspectionKindCheckOut, InspectionCondition("x"), "2026-12-20"); err == nil {
		t.Fatal("estado inválido deveria falhar")
	}
	if err := ValidateInspection(InspectionKindCheckOut, InspectionConditionGood, "   "); err == nil {
		t.Fatal("data vazia deveria falhar")
	}
}

// ── collection.go ──────────────────────────────────────────────────

func TestCovDomCollectionStages(t *testing.T) {
	labels := map[CollectionStage]string{
		CollectionStageFriendly:    "Lembrete amigável",
		CollectionStageFirm:        "Cobrança firme",
		CollectionStageInstallment: "Proposta de parcelamento",
		CollectionStage(0):         "",
		CollectionStage(9):         "",
	}
	for stage, want := range labels {
		if got := CollectionStageLabel(stage); got != want {
			t.Fatalf("CollectionStageLabel(%d) = %q, quer %q", stage, got, want)
		}
		if got := stage.Label(); got != want {
			t.Fatalf("Label(%d) = %q, quer %q", stage, got, want)
		}
		if got := stage.Valid(); got != (want != "") {
			t.Fatalf("Valid(%d) = %v", stage, got)
		}
	}

	byDelay := map[int]CollectionStage{1: CollectionStageFriendly, 6: CollectionStageFriendly, 7: CollectionStageFirm}
	for days, want := range byDelay {
		stage, ok := CollectionStageByDelay(days)
		if !ok || stage != want {
			t.Fatalf("CollectionStageByDelay(%d) = %v/%v", days, stage, ok)
		}
	}
	for _, days := range []int{-1, 0} {
		if _, ok := CollectionStageByDelay(days); ok {
			t.Fatalf("atraso %d não deveria sustentar estágio", days)
		}
	}
	stage, ok := CollectionStageByDelay(15)
	if !ok || stage != CollectionStageInstallment {
		t.Fatalf("atraso de 15 dias deveria propor parcelamento: %v/%v", stage, ok)
	}

	if stage, ok := CollectionStageFromValue(int(CollectionStageFirm)); !ok || stage != CollectionStageFirm {
		t.Fatalf("CollectionStageFromValue(Firm) = %v/%v", stage, ok)
	}
	if _, ok := CollectionStageFromValue(0); ok {
		t.Fatal("estágio zero não existe na trilha")
	}
	if _, ok := CollectionStageFromValue(42); ok {
		t.Fatal("estágio desconhecido deveria ser descartado")
	}

	options := CollectionStageOptions()
	if len(options) != 3 {
		t.Fatalf("esperava 3 estágios, veio %d", len(options))
	}
	want := []CollectionStageOption{
		{int(CollectionStageFriendly), "Lembrete amigável", CollectionFriendlyDays},
		{int(CollectionStageFirm), "Cobrança firme", CollectionFirmDays},
		{int(CollectionStageInstallment), "Proposta de parcelamento", CollectionInstallmentDays},
	}
	for i, option := range options {
		if option != want[i] {
			t.Fatalf("opção %d = %+v, quer %+v", i, option, want[i])
		}
	}
}

// ── errors.go ──────────────────────────────────────────────────────

func TestCovDomValidationErrorHelpers(t *testing.T) {
	err := Invalid("falha %d", 7)
	if err.Error() != "falha 7" {
		t.Fatalf("Error() = %q", err.Error())
	}
	if !IsValidation(err) || ValidationMessage(err) != "falha 7" {
		t.Fatal("erro de validação não reconhecido")
	}
	if IsValidation(errors.New("outro")) || ValidationMessage(errors.New("outro")) != "" {
		t.Fatal("erro comum não pode ser de validação")
	}
	if IsValidation(nil) {
		t.Fatal("nil não é erro de validação")
	}
}
