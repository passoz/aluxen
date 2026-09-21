package web

import "net/http"

// Histórico de auditoria e backup. (As métricas ficam em handlers_dashboard.go.)
//
//	GET  /audit        — JSON paginado com filtros
//	GET  /audit/export — CSV (delimitador ';', BOM, 13 colunas, limite 5000)
//	GET  /export       — backup JSON {exportedAt,tenants,properties,leases,receipts}
//	POST /import       — multipart, campo `file`; 302 /dashboard?tab=dashboard&toast=importado

func (h *Handlers) AuditList(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "audit.list")
}

func (h *Handlers) AuditExport(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "audit.export")
}

func (h *Handlers) BackupExport(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "backup.export")
}

func (h *Handlers) BackupImport(w http.ResponseWriter, r *http.Request) {
	h.notImplemented(w, r, "backup.import")
}
