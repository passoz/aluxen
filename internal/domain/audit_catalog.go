package domain

// Catálogo de auditoria: vocabulário fechado de ações, áreas, origens e
// metadados. Os valores literais são os mesmos do app atual — a trilha já
// gravada continua legível e os filtros continuam casando.

// AuditTone agrupa visualmente as ações na tela de histórico.
type AuditTone string

const (
	ToneCreate  AuditTone = "create"
	ToneUpdate  AuditTone = "update"
	ToneDelete  AuditTone = "delete"
	ToneIssue   AuditTone = "issue"
	ToneAccess  AuditTone = "access"
	ToneFailure AuditTone = "failure"
)

// AuditActionMeta descreve uma ação registrável na trilha.
type AuditActionMeta struct {
	Action string
	Label  string
	Entity string
	Tone   AuditTone
}

// Ações de auditoria (valor persistido em `audit_logs.action`).
const (
	ActionTenantCreate            = "tenant.create"
	ActionTenantUpdate            = "tenant.update"
	ActionTenantDelete            = "tenant.delete"
	ActionPropertyCreate          = "property.create"
	ActionPropertyUpdate          = "property.update"
	ActionPropertyDelete          = "property.delete"
	ActionLeaseCreate             = "lease.create"
	ActionLeaseUpdate             = "lease.update"
	ActionLeaseTerminate          = "lease.terminate"
	ActionLeaseSuspend            = "lease.suspend"
	ActionLeaseReactivate         = "lease.reactivate"
	ActionLeaseDelete             = "lease.delete"
	ActionReceiptCreate           = "receipt.create"
	ActionReceiptUpdate           = "receipt.update"
	ActionReceiptPay              = "receipt.pay"
	ActionReceiptBatchPay         = "receipt.batch-pay"
	ActionReceiptGenerateMonth    = "receipt.generate-monthly"
	ActionReceiptDelete           = "receipt.delete"
	ActionReceiptBatchDelete      = "receipt.batch-delete"
	ActionSettingLandlord         = "setting.landlord"
	ActionSettingAppURL           = "setting.app-url"
	ActionUserPassword            = "user.password"
	ActionClientLogin             = "auth.client-login"
	ActionClientLoginFailed       = "auth.client-login-failed"
	ActionClientLogout            = "auth.client-logout"
	ActionClientPassword          = "auth.client-password"
	ActionLogin                   = "auth.login"
	ActionLoginFailed             = "auth.login-failed"
	ActionLogout                  = "auth.logout"
	ActionDocumentReceipt         = "document.receipt"
	ActionDocumentContract        = "document.contract"
	ActionWhatsAppSend            = "whatsapp.send"
	ActionWhatsAppSendFailed      = "whatsapp.send-failed"
	ActionWhatsAppLogout          = "whatsapp.logout"
	ActionWhatsAppMessageReceived = "whatsapp.message-received"
	ActionBackupExport            = "backup.export"
	ActionBackupImport            = "backup.import"
	ActionHistoryExport           = "history.export"
	ActionDepositRefund           = "deposit.refund"
	ActionInspectionCreate        = "inspection.create"
	ActionInspectionUpdate        = "inspection.update"
	ActionInspectionDelete        = "inspection.delete"
	ActionPaymentProofAccepted    = "payment-proof.accepted"
	ActionPaymentProofReview      = "payment-proof.review"
	ActionPaymentProofRejected    = "payment-proof.rejected"
	ActionDocumentIntakeCreated   = "document-intake.created"
	ActionCollectionSent          = "collection.sent"
	ActionAdjustmentApplied       = "adjustment.applied"
)

// Áreas da trilha (`audit_logs.entity`).
const (
	EntityTenant     = "tenant"
	EntityProperty   = "property"
	EntityLease      = "lease"
	EntityReceipt    = "receipt"
	EntitySetting    = "setting"
	EntityUser       = "user"
	EntityDocument   = "document"
	EntityWhatsApp   = "whatsapp"
	EntityBackup     = "backup"
	EntityDeposit    = "deposit"
	EntityInspection = "inspection"
	EntityPayment    = "payment-proof"
	EntityIntake     = "document-intake"
	EntityCollection = "collection"
)

var auditActions = map[string]AuditActionMeta{
	ActionTenantCreate:            {ActionTenantCreate, "Inquilino cadastrado", EntityTenant, ToneCreate},
	ActionTenantUpdate:            {ActionTenantUpdate, "Inquilino alterado", EntityTenant, ToneUpdate},
	ActionTenantDelete:            {ActionTenantDelete, "Inquilino excluído", EntityTenant, ToneDelete},
	ActionPropertyCreate:          {ActionPropertyCreate, "Imóvel cadastrado", EntityProperty, ToneCreate},
	ActionPropertyUpdate:          {ActionPropertyUpdate, "Imóvel alterado", EntityProperty, ToneUpdate},
	ActionPropertyDelete:          {ActionPropertyDelete, "Imóvel excluído", EntityProperty, ToneDelete},
	ActionLeaseCreate:             {ActionLeaseCreate, "Contrato criado", EntityLease, ToneCreate},
	ActionLeaseUpdate:             {ActionLeaseUpdate, "Contrato alterado", EntityLease, ToneUpdate},
	ActionLeaseTerminate:          {ActionLeaseTerminate, "Contrato encerrado", EntityLease, ToneUpdate},
	ActionLeaseSuspend:            {ActionLeaseSuspend, "Contrato suspenso", EntityLease, ToneUpdate},
	ActionLeaseReactivate:         {ActionLeaseReactivate, "Contrato reativado", EntityLease, ToneUpdate},
	ActionLeaseDelete:             {ActionLeaseDelete, "Contrato excluído", EntityLease, ToneDelete},
	ActionReceiptCreate:           {ActionReceiptCreate, "Pagamento lançado", EntityReceipt, ToneCreate},
	ActionReceiptUpdate:           {ActionReceiptUpdate, "Pagamento alterado", EntityReceipt, ToneUpdate},
	ActionReceiptPay:              {ActionReceiptPay, "Baixa registrada", EntityReceipt, ToneUpdate},
	ActionReceiptBatchPay:         {ActionReceiptBatchPay, "Baixa em lote", EntityReceipt, ToneUpdate},
	ActionReceiptGenerateMonth:    {ActionReceiptGenerateMonth, "Pagamentos do mês gerados", EntityReceipt, ToneCreate},
	ActionReceiptDelete:           {ActionReceiptDelete, "Pagamento excluído", EntityReceipt, ToneDelete},
	ActionReceiptBatchDelete:      {ActionReceiptBatchDelete, "Pagamentos excluídos em lote", EntityReceipt, ToneDelete},
	ActionSettingLandlord:         {ActionSettingLandlord, "Dados do locador alterados", EntitySetting, ToneUpdate},
	ActionSettingAppURL:           {ActionSettingAppURL, "URL pública alterada", EntitySetting, ToneUpdate},
	ActionUserPassword:            {ActionUserPassword, "Senha alterada", EntityUser, ToneUpdate},
	ActionClientLogin:             {ActionClientLogin, "Acesso do locatário", EntityTenant, ToneAccess},
	ActionClientLoginFailed:       {ActionClientLoginFailed, "Acesso do locatário negado", EntityTenant, ToneFailure},
	ActionClientLogout:            {ActionClientLogout, "Saída do locatário", EntityTenant, ToneAccess},
	ActionClientPassword:          {ActionClientPassword, "Senha do locatário alterada", EntityTenant, ToneUpdate},
	ActionLogin:                   {ActionLogin, "Acesso ao sistema", EntityUser, ToneAccess},
	ActionLoginFailed:             {ActionLoginFailed, "Tentativa de acesso negada", EntityUser, ToneFailure},
	ActionLogout:                  {ActionLogout, "Saída do sistema", EntityUser, ToneAccess},
	ActionDocumentReceipt:         {ActionDocumentReceipt, "Recibo emitido", EntityDocument, ToneIssue},
	ActionDocumentContract:        {ActionDocumentContract, "Contrato emitido", EntityDocument, ToneIssue},
	ActionWhatsAppSend:            {ActionWhatsAppSend, "Envio por WhatsApp", EntityWhatsApp, ToneIssue},
	ActionWhatsAppSendFailed:      {ActionWhatsAppSendFailed, "Falha no envio por WhatsApp", EntityWhatsApp, ToneFailure},
	ActionWhatsAppLogout:          {ActionWhatsAppLogout, "WhatsApp desconectado", EntityWhatsApp, ToneAccess},
	ActionWhatsAppMessageReceived: {ActionWhatsAppMessageReceived, "Mensagem recebida pelo WhatsApp", EntityWhatsApp, ToneAccess},
	ActionBackupExport:            {ActionBackupExport, "Backup exportado", EntityBackup, ToneAccess},
	ActionBackupImport:            {ActionBackupImport, "Backup importado", EntityBackup, ToneCreate},
	ActionHistoryExport:           {ActionHistoryExport, "Histórico exportado (CSV)", EntityBackup, ToneAccess},
	ActionDepositRefund:           {ActionDepositRefund, "Caução devolvida", EntityDeposit, ToneIssue},
	ActionInspectionCreate:        {ActionInspectionCreate, "Vistoria registrada", EntityInspection, ToneCreate},
	ActionInspectionUpdate:        {ActionInspectionUpdate, "Vistoria alterada", EntityInspection, ToneUpdate},
	ActionInspectionDelete:        {ActionInspectionDelete, "Vistoria excluída", EntityInspection, ToneDelete},
	ActionPaymentProofAccepted:    {ActionPaymentProofAccepted, "Comprovante aceito", EntityPayment, ToneCreate},
	ActionPaymentProofReview:      {ActionPaymentProofReview, "Comprovante em revisão", EntityPayment, ToneUpdate},
	ActionPaymentProofRejected:    {ActionPaymentProofRejected, "Comprovante recusado", EntityPayment, ToneFailure},
	ActionDocumentIntakeCreated:   {ActionDocumentIntakeCreated, "Cadastro criado por documento", EntityIntake, ToneCreate},
	ActionCollectionSent:          {ActionCollectionSent, "Cobrança enviada", EntityCollection, ToneIssue},
	ActionAdjustmentApplied:       {ActionAdjustmentApplied, "Reajuste aplicado", EntityLease, ToneUpdate},
}

// AuditActionMetaFor devolve os metadados da ação (tone padrão: update).
func AuditActionMetaFor(action string) AuditActionMeta {
	if meta, ok := auditActions[action]; ok {
		return meta
	}
	return AuditActionMeta{Action: action, Label: action, Entity: "", Tone: ToneUpdate}
}

// EntityLabel devolve o rótulo em português da área da trilha.
func EntityLabel(entity string) string {
	switch entity {
	case EntityTenant:
		return "Inquilino"
	case EntityProperty:
		return "Imóvel"
	case EntityLease:
		return "Contrato"
	case EntityReceipt:
		return "Pagamento"
	case EntitySetting:
		return "Configuração"
	case EntityUser:
		return "Usuário"
	case EntityDocument:
		return "Documento"
	case EntityWhatsApp:
		return "WhatsApp"
	case EntityBackup:
		return "Backup"
	case EntityDeposit:
		return "Caução"
	case EntityInspection:
		return "Vistoria"
	case EntityPayment:
		return "Comprovante"
	case EntityIntake:
		return "Cadastro por documento"
	case EntityCollection:
		return "Cobrança"
	default:
		return entity
	}
}

// MetadataLabel devolve o rótulo em português de uma chave de metadado.
func MetadataLabel(key string) string {
	switch key {
	case "month":
		return "Mês de referência"
	case "count":
		return "Quantidade"
	case "batch":
		return "Itens no lote"
	case "channel":
		return "Canal"
	case "signature":
		return "Com assinatura"
	case "phone":
		return "Telefone"
	case "jobId":
		return "Protocolo de envio"
	case "kind":
		return "Tipo"
	case "reason":
		return "Motivo"
	case "username":
		return "Usuário"
	case "tenants":
		return "Inquilinos"
	case "properties":
		return "Imóveis"
	case "leases":
		return "Contratos"
	case "receipts":
		return "Pagamentos"
	case "fileName":
		return "Arquivo"
	case "confidence":
		return "Confiança"
	case "extracted":
		return "Valor lido"
	case "receiptId":
		return "Pagamento"
	case "amount":
		return "Valor"
	case "stage":
		return "Etapa"
	default:
		return key
	}
}
