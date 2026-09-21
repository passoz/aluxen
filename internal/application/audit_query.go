package application

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Leituras da trilha de auditoria: consulta paginada com filtros, exportação CSV
// e backup/restauração dos cadastros. A gravação dos eventos vive em audit.go;
// aqui só há leitura e a importação de backup — nenhuma das duas grava auditoria
// (quem audita a exportação/importação é o handler, que conhece a requisição).

const (
	// DefaultAuditPageSize é a página padrão da aba Histórico.
	DefaultAuditPageSize = 25
	// MinAuditPageSize e MaxAuditPageSize limitam o tamanho da página pedido.
	MinAuditPageSize = 5
	MaxAuditPageSize = 200
	// AuditExportLimit é o teto de linhas de uma exportação CSV — evita montar
	// arquivos gigantes em memória.
	AuditExportLimit = 5000
	// DefaultMaxBackupImportSize é o limite de tamanho aceito na importação de
	// backup quando o produto não configura outro (`Options.MaxBackupImportSize`).
	DefaultMaxBackupImportSize int64 = 10 << 20
)

// AuditFilter são os filtros e a paginação da consulta da trilha. Como em
// `domain.AuditFilter`, o valor zero de Descending é ascendente: quem monta o
// filtro é ReadAuditQuery, que já aplica o padrão do app (mais recente primeiro).
type AuditFilter struct {
	Page       int
	PageSize   int
	Entity     string
	Action     string
	Actor      string
	From       string
	To         string
	Search     string
	Descending bool
}

// ReadAuditQuery traduz a query string do endpoint `/audit` no filtro tipado.
//
// Regras idênticas às do app atual: página mínima 1; tamanho entre 5 e 200 (25
// por padrão); `from`/`to` só valem no formato `YYYY-MM-DD`; `order=asc` inverte
// a ordem (o padrão é do mais recente para o mais antigo).
func ReadAuditQuery(values url.Values) AuditFilter {
	pageSize := positiveInt(values.Get("pageSize"), DefaultAuditPageSize)
	if pageSize < MinAuditPageSize {
		pageSize = MinAuditPageSize
	}
	if pageSize > MaxAuditPageSize {
		pageSize = MaxAuditPageSize
	}
	return AuditFilter{
		Page:       positiveInt(values.Get("page"), 1),
		PageSize:   pageSize,
		Entity:     strings.TrimSpace(values.Get("entity")),
		Action:     strings.TrimSpace(values.Get("action")),
		Actor:      strings.TrimSpace(values.Get("actor")),
		From:       auditDateOnly(values.Get("from")),
		To:         auditDateOnly(values.Get("to")),
		Search:     strings.TrimSpace(values.Get("search")),
		Descending: values.Get("order") != "asc",
	}
}

// AuditRow é o evento da trilha já desserializado para a tela e para o CSV.
type AuditRow struct {
	ID          string               `json:"id"`
	CreatedAt   string               `json:"createdAt"`
	ActorID     *string              `json:"actorId"`
	ActorName   *string              `json:"actorName"`
	Action      string               `json:"action"`
	Entity      string               `json:"entity"`
	EntityID    *string              `json:"entityId"`
	EntityLabel *string              `json:"entityLabel"`
	Summary     string               `json:"summary"`
	Changes     []domain.AuditChange `json:"changes"`
	Metadata    map[string]any       `json:"metadata"`
	Origin      string               `json:"origin"`
	IP          *string              `json:"ip"`
	UserAgent   *string              `json:"userAgent"`
	RequestID   *string              `json:"requestId"`
}

// AuditPage é a página devolvida por `GET /audit`.
type AuditPage struct {
	Items    []AuditRow `json:"items"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
	Actors   []string   `json:"actors"`
}

// ListAudit devolve a página pedida, o total de eventos do filtro e os atores
// que já assinaram a trilha (alimentam o filtro de usuário).
func ListAudit(ctx context.Context, deps Deps, filter AuditFilter) (*AuditPage, error) {
	filter = normalizeAuditFilter(filter)

	rows, err := deps.Repos.Audit.List(ctx, filter.domainFilter(filter.PageSize, (filter.Page-1)*filter.PageSize))
	if err != nil {
		return nil, fmt.Errorf("listar trilha de auditoria: %w", err)
	}
	total, err := deps.Repos.Audit.Count(ctx, filter.domainFilter(0, 0))
	if err != nil {
		return nil, fmt.Errorf("contar trilha de auditoria: %w", err)
	}
	actors, err := deps.Repos.Audit.Actors(ctx)
	if err != nil {
		return nil, fmt.Errorf("listar autores da trilha: %w", err)
	}
	if actors == nil {
		actors = []string{}
	}

	items := make([]AuditRow, 0, len(rows))
	for i := range rows {
		items = append(items, auditRowFrom(&rows[i]))
	}
	return &AuditPage{
		Items:    items,
		Total:    total,
		Page:     filter.Page,
		PageSize: filter.PageSize,
		Actors:   actors,
	}, nil
}

// ListAuditForExport devolve até AuditExportLimit eventos que casam com o filtro,
// ignorando a paginação da tela.
func ListAuditForExport(ctx context.Context, deps Deps, filter AuditFilter) ([]AuditRow, error) {
	filter = normalizeAuditFilter(filter)

	rows, err := deps.Repos.Audit.List(ctx, filter.domainFilter(AuditExportLimit, 0))
	if err != nil {
		return nil, fmt.Errorf("listar trilha de auditoria para exportação: %w", err)
	}
	entries := make([]AuditRow, 0, len(rows))
	for i := range rows {
		entries = append(entries, auditRowFrom(&rows[i]))
	}
	return entries, nil
}

// BuildAuditCSV monta o CSV da trilha no formato aceito pelo Excel em português:
// BOM UTF-8, delimitador `;`, 13 colunas na ordem do app atual e quebras CRLF.
// A exportação é truncada em AuditExportLimit linhas.
func BuildAuditCSV(entries []AuditRow) string {
	if len(entries) > AuditExportLimit {
		entries = entries[:AuditExportLimit]
	}

	var builder strings.Builder
	builder.WriteString("\uFEFF")
	builder.WriteString(csvRow(auditCSVHeaders))
	for _, entry := range entries {
		builder.WriteString(csvRow([]string{
			auditCSVDateTime(entry.CreatedAt),
			orDefault(domain.Str(entry.ActorName), "Sistema"),
			entry.Action,
			domain.EntityLabel(entry.Entity),
			domain.Str(entry.EntityLabel),
			domain.Str(entry.EntityID),
			entry.Summary,
			describeAuditChanges(entry.Changes),
			describeAuditMetadata(entry.Metadata),
			domain.AuditOriginLabel(domain.AuditOrigin(entry.Origin)),
			domain.Str(entry.IP),
			domain.Str(entry.UserAgent),
			domain.Str(entry.RequestID),
		}))
	}
	return builder.String()
}

// auditCSVHeaders são as 13 colunas do CSV, na ordem do app atual.
var auditCSVHeaders = []string{
	"Data/Hora",
	"Usuário",
	"Ação",
	"Área",
	"Registro",
	"ID do registro",
	"Resumo",
	"Alterações",
	"Metadados",
	"Origem",
	"IP",
	"User-Agent",
	"Request",
}

func csvRow(cells []string) string {
	quoted := make([]string, len(cells))
	for i, cell := range cells {
		quoted[i] = `"` + strings.ReplaceAll(cell, `"`, `""`) + `"`
	}
	return strings.Join(quoted, ";") + "\r\n"
}

// ── Backup (exportação e importação) ───────────────────────────────

// Backup é o conteúdo do arquivo `aluxen-backup-*.json`.
type Backup struct {
	ExportedAt string           `json:"exportedAt"`
	Tenants    []BackupTenant   `json:"tenants"`
	Properties []BackupProperty `json:"properties"`
	Leases     []BackupLease    `json:"leases"`
	Receipts   []BackupReceipt  `json:"receipts"`
}

// BackupTenant é um locatário do backup. As credenciais da Área do Cliente
// ficam de fora: o backup não carrega hash de senha.
type BackupTenant struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	Phone     *string `json:"phone"`
	CPF       *string `json:"cpf"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

// BackupProperty é um imóvel do backup.
type BackupProperty struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Address   string  `json:"address"`
	City      string  `json:"city"`
	State     string  `json:"state"`
	Zip       *string `json:"zip"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

// BackupLease é um contrato do backup.
type BackupLease struct {
	ID         string  `json:"id"`
	TenantID   string  `json:"tenantId"`
	PropertyID string  `json:"propertyId"`
	StartDate  string  `json:"startDate"`
	EndDate    string  `json:"endDate"`
	DueDay     int     `json:"dueDay"`
	RentAmount float64 `json:"rentAmount"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
}

// BackupReceipt é um lançamento do backup. Só o essencial é levado: o restante
// (categoria, descrição, valor pago, chave de geração e token) é recalculado ou
// emitido de novo pelo sistema.
type BackupReceipt struct {
	ID        string  `json:"id"`
	LeaseID   *string `json:"leaseId"`
	DueDate   string  `json:"dueDate"`
	PaidAt    *string `json:"paidAt"`
	Amount    float64 `json:"amount"`
	CreatedAt string  `json:"createdAt"`
}

// ImportResult são as contagens do backup importado.
type ImportResult struct {
	Tenants    int `json:"tenants"`
	Properties int `json:"properties"`
	Leases     int `json:"leases"`
	Receipts   int `json:"receipts"`
}

// ExportBackup lê todos os cadastros numa passada e monta o arquivo de backup.
func ExportBackup(ctx context.Context, deps Deps) (*Backup, error) {
	tenants, err := ListTenants(ctx, deps)
	if err != nil {
		return nil, err
	}
	properties, err := ListProperties(ctx, deps)
	if err != nil {
		return nil, err
	}
	leases, err := ListLeases(ctx, deps)
	if err != nil {
		return nil, err
	}
	receipts, err := ListReceipts(ctx, deps)
	if err != nil {
		return nil, err
	}

	backup := &Backup{
		ExportedAt: deps.Clock.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Tenants:    make([]BackupTenant, 0, len(tenants)),
		Properties: make([]BackupProperty, 0, len(properties)),
		Leases:     make([]BackupLease, 0, len(leases)),
		Receipts:   make([]BackupReceipt, 0, len(receipts)),
	}
	for i := range tenants {
		backup.Tenants = append(backup.Tenants, BackupTenant{
			ID:        tenants[i].ID,
			Name:      tenants[i].Name,
			Email:     tenants[i].Email,
			Phone:     tenants[i].Phone,
			CPF:       tenants[i].CPF,
			CreatedAt: tenants[i].CreatedAt,
			UpdatedAt: tenants[i].UpdatedAt,
		})
	}
	for i := range properties {
		backup.Properties = append(backup.Properties, BackupProperty{
			ID:        properties[i].ID,
			Name:      properties[i].Name,
			Address:   properties[i].Address,
			City:      properties[i].City,
			State:     properties[i].State,
			Zip:       properties[i].Zip,
			CreatedAt: properties[i].CreatedAt,
			UpdatedAt: properties[i].UpdatedAt,
		})
	}
	for i := range leases {
		backup.Leases = append(backup.Leases, BackupLease{
			ID:         leases[i].ID,
			TenantID:   leases[i].TenantID,
			PropertyID: leases[i].PropertyID,
			StartDate:  leases[i].StartDate,
			EndDate:    leases[i].EndDate,
			DueDay:     leases[i].DueDay,
			RentAmount: leases[i].RentAmount,
			Status:     string(leases[i].Status),
			CreatedAt:  leases[i].CreatedAt,
			UpdatedAt:  leases[i].UpdatedAt,
		})
	}
	for i := range receipts {
		backup.Receipts = append(backup.Receipts, BackupReceipt{
			ID:        receipts[i].ID,
			LeaseID:   receipts[i].LeaseID,
			DueDate:   receipts[i].DueDate,
			PaidAt:    receipts[i].PaidAt,
			Amount:    receipts[i].Amount,
			CreatedAt: receipts[i].CreatedAt,
		})
	}
	return backup, nil
}

// ImportBackup insere o conteúdo de um backup gerando ids novos e remapeando as
// chaves estrangeiras de locatário, imóvel e contrato. Os timestamps originais
// são preservados; `dueDay` cai para 1 quando ausente. Categoria, descrição,
// valor pago, chave de geração e tokens não são importados — recomeçam do zero.
//
// actx identifica quem importou (origem, ator, requisição); fica registrado no
// log para correlacionar a operação.
func ImportBackup(ctx context.Context, deps Deps, actx AuditContext, data []byte) (*ImportResult, error) {
	var payload Backup
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, domain.Invalid("Arquivo JSON inválido.")
	}
	if payload.Tenants == nil || payload.Properties == nil || payload.Leases == nil || payload.Receipts == nil {
		return nil, domain.Invalid("Formato de backup inválido.")
	}

	tenantIDs := make(map[string]string, len(payload.Tenants))
	propertyIDs := make(map[string]string, len(payload.Properties))
	leaseIDs := make(map[string]string, len(payload.Leases))
	now := format.FormatUTCStamp(deps.Clock.Now())

	for _, item := range payload.Tenants {
		id := deps.IDs.New()
		tenantIDs[item.ID] = id
		createdAt, updatedAt := stampPair(item.CreatedAt, item.UpdatedAt, now)
		if err := deps.Repos.Tenants.Create(ctx, &domain.Tenant{
			ID:        id,
			Name:      item.Name,
			Email:     item.Email,
			Phone:     item.Phone,
			CPF:       item.CPF,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		}); err != nil {
			return nil, fmt.Errorf("importar locatário: %w", err)
		}
	}

	for _, item := range payload.Properties {
		id := deps.IDs.New()
		propertyIDs[item.ID] = id
		createdAt, updatedAt := stampPair(item.CreatedAt, item.UpdatedAt, now)
		if err := deps.Repos.Properties.Create(ctx, &domain.Property{
			ID:        id,
			Name:      item.Name,
			Address:   item.Address,
			City:      item.City,
			State:     item.State,
			Zip:       item.Zip,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		}); err != nil {
			return nil, fmt.Errorf("importar imóvel: %w", err)
		}
	}

	for _, item := range payload.Leases {
		id := deps.IDs.New()
		leaseIDs[item.ID] = id
		dueDay := item.DueDay
		if dueDay <= 0 {
			dueDay = 1
		}
		status := domain.LeaseStatus(item.Status)
		if status == "" {
			status = domain.LeaseStatusActive
		}
		createdAt, updatedAt := stampPair(item.CreatedAt, item.UpdatedAt, now)
		if err := deps.Repos.Leases.Create(ctx, &domain.Lease{
			ID:         id,
			TenantID:   remapID(tenantIDs, item.TenantID),
			PropertyID: remapID(propertyIDs, item.PropertyID),
			StartDate:  item.StartDate,
			EndDate:    item.EndDate,
			DueDay:     dueDay,
			RentAmount: item.RentAmount,
			Status:     status,
			CreatedAt:  createdAt,
			UpdatedAt:  updatedAt,
		}); err != nil {
			return nil, fmt.Errorf("importar contrato: %w", err)
		}
	}

	for _, item := range payload.Receipts {
		createdAt, _ := stampPair(item.CreatedAt, item.CreatedAt, now)
		if err := deps.Repos.Receipts.Create(ctx, &domain.Receipt{
			ID:        deps.IDs.New(),
			LeaseID:   remapOptionalID(leaseIDs, item.LeaseID),
			Category:  domain.ReceiptCategoryRent,
			DueDate:   item.DueDate,
			PaidAt:    item.PaidAt,
			Amount:    item.Amount,
			CreatedAt: createdAt,
		}); err != nil {
			return nil, fmt.Errorf("importar lançamento: %w", err)
		}
	}

	result := &ImportResult{
		Tenants:    len(payload.Tenants),
		Properties: len(payload.Properties),
		Leases:     len(payload.Leases),
		Receipts:   len(payload.Receipts),
	}
	deps.Log.Info("backup importado", "scope", "backup",
		"actor", domain.Str(actx.ActorName), "request_id", actx.RequestID,
		"tenants", result.Tenants, "properties", result.Properties,
		"leases", result.Leases, "receipts", result.Receipts)
	return result, nil
}

// remapID devolve o id novo da entidade; sem correspondência, mantém o original
// (o banco acusa a chave estrangeira inexistente).
func remapID(mapping map[string]string, id string) string {
	if mapped, ok := mapping[id]; ok {
		return mapped
	}
	return id
}

func remapOptionalID(mapping map[string]string, id *string) *string {
	if id == nil {
		return nil
	}
	mapped := remapID(mapping, *id)
	return &mapped
}

// stampPair trata os timestamps do backup: preserva o valor original e cai para
// o instante da importação quando o campo está ausente (as colunas são NOT NULL).
func stampPair(createdAt, updatedAt, fallback string) (string, string) {
	if createdAt == "" {
		createdAt = fallback
	}
	if updatedAt == "" {
		updatedAt = fallback
	}
	return createdAt, updatedAt
}

// ── Auxiliares internos ────────────────────────────────────────────

func normalizeAuditFilter(filter AuditFilter) AuditFilter {
	if filter.Page < 1 {
		filter.Page = 1
	}
	switch {
	case filter.PageSize <= 0:
		filter.PageSize = DefaultAuditPageSize
	case filter.PageSize < MinAuditPageSize:
		filter.PageSize = MinAuditPageSize
	case filter.PageSize > MaxAuditPageSize:
		filter.PageSize = MaxAuditPageSize
	}
	return filter
}

func (f AuditFilter) domainFilter(limit, offset int) domain.AuditFilter {
	return domain.AuditFilter{
		Entity:     f.Entity,
		Action:     f.Action,
		Actor:      f.Actor,
		From:       f.From,
		To:         f.To,
		Search:     f.Search,
		Descending: f.Descending,
		Limit:      limit,
		Offset:     offset,
	}
}

func auditRowFrom(row *domain.AuditLog) AuditRow {
	return AuditRow{
		ID:          row.ID,
		CreatedAt:   row.CreatedAt,
		ActorID:     row.ActorID,
		ActorName:   row.ActorName,
		Action:      row.Action,
		Entity:      row.Entity,
		EntityID:    row.EntityID,
		EntityLabel: row.EntityLabel,
		Summary:     row.Summary,
		Changes:     decodeAuditChanges(row.Changes),
		Metadata:    decodeAuditMetadata(row.Metadata),
		Origin:      string(row.Origin),
		IP:          row.IP,
		UserAgent:   row.UserAgent,
		RequestID:   row.RequestID,
	}
}

// decodeAuditChanges desserializa a coluna `changes`. Conteúdo inválido vira
// lista vazia — a trilha já gravada continua legível mesmo se um JSON quebrar.
func decodeAuditChanges(raw *string) []domain.AuditChange {
	changes := []domain.AuditChange{}
	if raw == nil || *raw == "" {
		return changes
	}
	if err := json.Unmarshal([]byte(*raw), &changes); err != nil {
		return []domain.AuditChange{}
	}
	if changes == nil {
		return []domain.AuditChange{}
	}
	return changes
}

// decodeAuditMetadata desserializa a coluna `metadata` como objeto.
func decodeAuditMetadata(raw *string) map[string]any {
	metadata := map[string]any{}
	if raw == nil || *raw == "" {
		return metadata
	}
	if err := json.Unmarshal([]byte(*raw), &metadata); err != nil {
		return map[string]any{}
	}
	if metadata == nil {
		return map[string]any{}
	}
	return metadata
}

// describeAuditChanges resume o diff no formato `Rótulo: antes → depois`, usado
// nas colunas do CSV e no detalhe da tela. Valor ausente aparece como travessão.
func describeAuditChanges(changes []domain.AuditChange) string {
	parts := make([]string, 0, len(changes))
	for _, change := range changes {
		parts = append(parts, fmt.Sprintf("%s: %s → %s", change.Label, auditDisplayValue(change.From), auditDisplayValue(change.To)))
	}
	return strings.Join(parts, " | ")
}

// describeAuditMetadata resume os metadados como `Rótulo: valor`, em ordem
// alfabética de chave (a iteração de mapa em Go é aleatória).
func describeAuditMetadata(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	keys := make([]string, 0, len(metadata))
	for key, value := range metadata {
		if value == nil || value == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s: %v", domain.MetadataLabel(key), metadata[key]))
	}
	return strings.Join(parts, " | ")
}

func auditDisplayValue(value *string) string {
	if value == nil || *value == "" {
		return "—"
	}
	return *value
}

// auditCSVDateTime formata o carimbo da trilha para leitura humana; se o valor
// não for uma data reconhecível, devolve o original em vez de perder o dado.
func auditCSVDateTime(value string) string {
	if value == "" {
		return ""
	}
	formatted := format.FormatDateTimeBR(value)
	if formatted == "-" {
		return ""
	}
	return formatted
}

func positiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

// auditDateOnly aceita apenas `YYYY-MM-DD`; qualquer outro formato é descartado,
// como no app atual.
func auditDateOnly(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) != len("2006-01-02") {
		return ""
	}
	for i, r := range trimmed {
		switch {
		case i == 4 || i == 7:
			if r != '-' {
				return ""
			}
		default:
			if r < '0' || r > '9' {
				return ""
			}
		}
	}
	return trimmed
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
