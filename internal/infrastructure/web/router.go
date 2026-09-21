package web

import "net/http"

// Tabela de rotas. Os padrões usam o ServeMux do Go 1.22+ (método + path com
// parâmetro). Cada handler vive no arquivo da sua área (handlers_*.go).
func (h *Handlers) routes() *http.ServeMux {
	mux := http.NewServeMux()

	// Sistema
	mux.HandleFunc("GET /healthz", h.Healthz)
	mux.HandleFunc("GET /readyz", h.Readyz)
	mux.HandleFunc("GET /api/health", h.Health)
	mux.HandleFunc("GET /api/info", h.SystemInfo)

	// Autenticação do administrador
	mux.HandleFunc("GET /{$}", h.Root)
	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /login", h.Login)
	mux.HandleFunc("GET /logout", h.Logout)
	mux.HandleFunc("POST /logout", h.Logout)

	// Painel
	mux.HandleFunc("GET /dashboard", h.Dashboard)
	mux.HandleFunc("POST /dashboard", h.DashboardMutation)
	mux.HandleFunc("GET /settings", h.SettingsPage)
	mux.HandleFunc("POST /settings", h.SettingsMutation)
	mux.HandleFunc("GET /metrics", h.Metrics)
	mux.HandleFunc("GET /audit", h.AuditList)
	mux.HandleFunc("GET /audit/export", h.AuditExport)
	mux.HandleFunc("GET /export", h.BackupExport)
	mux.HandleFunc("POST /import", h.BackupImport)

	// Documentos e prova pública
	mux.HandleFunc("GET /pdf", h.ReceiptPDF)
	mux.HandleFunc("GET /contract-pdf", h.ContractPDF)
	mux.HandleFunc("GET /validar/recibo/{id}", h.VerifyReceipt)
	mux.HandleFunc("GET /validar/contrato/{id}", h.VerifyContract)

	// Área do Cliente
	mux.HandleFunc("GET /portal/login", h.PortalLoginPage)
	mux.HandleFunc("POST /portal/login", h.PortalLogin)
	mux.HandleFunc("GET /portal/logout", h.PortalLogout)
	mux.HandleFunc("POST /portal/logout", h.PortalLogout)
	mux.HandleFunc("GET /portal", h.PortalHome)
	mux.HandleFunc("GET /portal/password", h.PortalPasswordPage)
	mux.HandleFunc("POST /portal/password", h.PortalPassword)

	// Temporada (novas funcionalidades)
	mux.HandleFunc("POST /dashboard/deposit", h.DepositMutation)
	mux.HandleFunc("POST /dashboard/inspection", h.InspectionMutation)
	mux.HandleFunc("GET /inspection-pdf", h.InspectionPDF)
	mux.HandleFunc("GET /deposit-pdf", h.DepositReceiptPDF)
	mux.HandleFunc("GET /validar/vistoria/{id}", h.VerifyInspection)
	mux.HandleFunc("GET /validar/caucao/{id}", h.VerifyDeposit)

	// Comprovantes e revisão assistida por IA
	mux.HandleFunc("POST /api/payments/proof", h.PaymentProofUpload)
	mux.HandleFunc("GET /api/payments/review", h.PaymentProofReviewList)
	mux.HandleFunc("POST /api/payments/review", h.PaymentProofReviewDecision)
	mux.HandleFunc("POST /api/intake/document", h.DocumentIntake)
	mux.HandleFunc("POST /api/intake/confirm", h.DocumentIntakeConfirm)

	// WhatsApp
	mux.HandleFunc("GET /api/whatsapp/status", h.WhatsAppStatus)
	mux.HandleFunc("POST /api/whatsapp/login/start", h.WhatsAppLoginStart)
	mux.HandleFunc("GET /api/whatsapp/login/status", h.WhatsAppLoginStatus)
	mux.HandleFunc("POST /api/whatsapp/login/cancel", h.WhatsAppLoginCancel)
	mux.HandleFunc("POST /api/whatsapp/logout", h.WhatsAppLogout)
	mux.HandleFunc("POST /api/whatsapp/preview", h.WhatsAppPreview)
	mux.HandleFunc("POST /api/whatsapp/send", h.WhatsAppSend)
	mux.HandleFunc("GET /api/whatsapp/send/status", h.WhatsAppSendStatus)
	mux.HandleFunc("POST /api/whatsapp/webhook", h.WhatsAppWebhook)

	// Rotas não casadas pelo mux caem no handler de 404 do pipeline.
	mux.HandleFunc("/", h.notFound)

	return mux
}
