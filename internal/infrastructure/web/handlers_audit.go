package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/passoz/aluxen/internal/application"
	"github.com/passoz/aluxen/internal/domain"
)

// Histórico de auditoria e backup. (As métricas ficam em handlers_dashboard.go.)
//
//	GET  /audit        — JSON paginado com filtros
//	GET  /audit/export — CSV (delimitador ';', BOM, 13 colunas, limite 5000)
//	GET  /export       — backup JSON {exportedAt,tenants,properties,leases,receipts}
//	POST /import       — multipart, campo `file`; 302 /dashboard?tab=dashboard&toast=importado

// AuditList devolve a trilha paginada. É um endpoint JSON: sessão ausente vira
// 401 JSON (como no app atual), nunca um redirecionamento para a tela de login.
func (h *Handlers) AuditList(w http.ResponseWriter, r *http.Request) {
	if h.Session.AdminID(r) == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Não autorizado."})
		return
	}
	filter := application.ReadAuditQuery(r.URL.Query())
	page, err := application.ListAudit(r.Context(), h.Deps, filter)
	if err != nil {
		h.mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// AuditExport baixa o CSV da trilha com os mesmos filtros da aba Histórico.
func (h *Handlers) AuditExport(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	filter := application.ReadAuditQuery(r.URL.Query())
	entries, err := application.ListAuditForExport(r.Context(), h.Deps, filter)
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	filename := h.backupFilename("aluxen-historico", "csv")
	h.recordAudit(r, domain.ActionHistoryExport, filename,
		fmt.Sprintf("Histórico de auditoria exportado em CSV com %d evento(s)", len(entries)),
		map[string]any{"fileName": filename, "count": len(entries)})

	writeDownload(w, "text/csv; charset=utf-8", filename, []byte(application.BuildAuditCSV(entries)))
}

// BackupExport baixa o backup completo dos cadastros em JSON.
func (h *Handlers) BackupExport(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}
	backup, err := application.ExportBackup(r.Context(), h.Deps)
	if err != nil {
		h.mapError(w, r, err)
		return
	}

	filename := h.backupFilename("aluxen-backup", "json")
	h.recordAudit(r, domain.ActionBackupExport, filename,
		fmt.Sprintf("Backup exportado com %d inquilino(s), %d imóvel(is), %d contrato(s) e %d pagamento(s)",
			len(backup.Tenants), len(backup.Properties), len(backup.Leases), len(backup.Receipts)),
		map[string]any{
			"fileName":   filename,
			"tenants":    len(backup.Tenants),
			"properties": len(backup.Properties),
			"leases":     len(backup.Leases),
			"receipts":   len(backup.Receipts),
		})

	encoded, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		h.Log.Error("falha ao serializar backup", "scope", "http", "err", err)
		h.mapError(w, r, fmt.Errorf("serializar backup: %w", err))
		return
	}
	writeDownload(w, "application/json", filename, encoded)
}

// BackupImport restaura um backup enviado como multipart no campo `file`.
func (h *Handlers) BackupImport(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireAdmin(w, r); !ok {
		return
	}

	limit := h.Deps.Opts.MaxBackupImportSize
	if limit <= 0 {
		limit = application.DefaultMaxBackupImportSize
	}
	if r.ContentLength > limit {
		http.Error(w, "Arquivo muito grande.", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Arquivo não enviado.", http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		http.Error(w, "Não foi possível ler o arquivo.", http.StatusBadRequest)
		return
	}
	if int64(len(data)) > limit {
		http.Error(w, "Arquivo muito grande.", http.StatusBadRequest)
		return
	}

	fileName := header.Filename
	if fileName == "" {
		fileName = "backup.json"
	}
	result, err := application.ImportBackup(r.Context(), h.Deps, h.auditContext(r, domain.AuditOriginWeb), data)
	if err != nil {
		if domain.IsValidation(err) {
			http.Error(w, domain.ValidationMessage(err), http.StatusBadRequest)
			return
		}
		h.Log.Warn("falha ao importar backup", "scope", "http", "err", err)
		http.Error(w, "Não foi possível importar os dados.", http.StatusBadRequest)
		return
	}

	h.recordAudit(r, domain.ActionBackupImport, fileName,
		fmt.Sprintf("Backup importado com %d inquilino(s), %d imóvel(is), %d contrato(s) e %d pagamento(s)",
			result.Tenants, result.Properties, result.Leases, result.Receipts),
		map[string]any{
			"fileName":   fileName,
			"tenants":    result.Tenants,
			"properties": result.Properties,
			"leases":     result.Leases,
			"receipts":   result.Receipts,
		})

	http.Redirect(w, r, "/dashboard?tab=dashboard&toast=importado", http.StatusFound)
}

// recordAudit registra o evento de exportação/importação. Best-effort: falha de
// auditoria nunca derruba o download já pronto.
func (h *Handlers) recordAudit(r *http.Request, action, entityLabel, summary string, metadata map[string]any) {
	if err := application.Record(r.Context(), h.Deps, h.auditContext(r, domain.AuditOriginWeb), application.AuditEntry{
		Action:      action,
		Entity:      domain.EntityBackup,
		EntityLabel: entityLabel,
		Summary:     summary,
		Metadata:    metadata,
	}); err != nil {
		h.Log.Warn("auditoria de backup não gravada", "scope", "http", "action", action, "err", err)
	}
}

// backupFilename monta `aluxen-<kind>-YYYY-MM-DD.<ext>` com a data em UTC, a
// mesma referência do app atual.
func (h *Handlers) backupFilename(prefix, extension string) string {
	return fmt.Sprintf("%s-%s.%s", prefix, h.Deps.Clock.Now().UTC().Format("2006-01-02"), extension)
}

// writeDownload entrega um arquivo anexo com o nome pedido.
func writeDownload(w http.ResponseWriter, contentType, filename string, content []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
