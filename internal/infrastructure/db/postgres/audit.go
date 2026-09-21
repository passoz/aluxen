package postgres

import (
	"context"
	"database/sql"
	"strings"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/infrastructure/db/postgres/sqlcgen"
)

// Audit implementa domain.AuditRepository sobre o SQL gerado pelo sqlc.
type Audit struct {
	queries *sqlcgen.Queries
}

// NewAudit monta o repositório da trilha de auditoria.
func NewAudit(db sqlcgen.DBTX) *Audit {
	return &Audit{queries: sqlcgen.New(db)}
}

var _ domain.AuditRepository = (*Audit)(nil)

func (r *Audit) Insert(ctx context.Context, entry *domain.AuditLog) error {
	return wrapError(r.queries.InsertAuditLog(ctx, sqlcgen.InsertAuditLogParams{
		ID:          entry.ID,
		CreatedAt:   entry.CreatedAt,
		ActorID:     nullString(entry.ActorID),
		ActorName:   nullString(entry.ActorName),
		Action:      entry.Action,
		Entity:      entry.Entity,
		EntityID:    nullString(entry.EntityID),
		EntityLabel: nullString(entry.EntityLabel),
		Summary:     entry.Summary,
		Changes:     nullString(entry.Changes),
		Metadata:    nullString(entry.Metadata),
		Origin:      auditOriginOrDefault(entry.Origin),
		Ip:          nullString(entry.IP),
		UserAgent:   nullString(entry.UserAgent),
		RequestID:   nullString(entry.RequestID),
	}))
}

// List devolve a página pedida, na direção indicada pelo filtro.
func (r *Audit) List(ctx context.Context, filter domain.AuditFilter) ([]domain.AuditLog, error) {
	if filter.Descending {
		rows, err := r.queries.ListAuditLogsDesc(ctx, auditListParams(filter))
		if err != nil {
			return nil, wrapError(err)
		}
		return auditLogsFromRows(rows), nil
	}
	rows, err := r.queries.ListAuditLogsAsc(ctx, sqlcgen.ListAuditLogsAscParams(auditListParams(filter)))
	if err != nil {
		return nil, wrapError(err)
	}
	return auditLogsFromRows(rows), nil
}

func (r *Audit) Count(ctx context.Context, filter domain.AuditFilter) (int, error) {
	total, err := r.queries.CountAuditLogs(ctx, sqlcgen.CountAuditLogsParams{
		Entity:     filter.Entity,
		Action:     filter.Action,
		Actor:      sql.NullString{String: filter.Actor, Valid: true},
		FromDate:   filter.From,
		ToDate:     auditToBound(filter.To),
		SearchLike: searchLike(filter.Search),
	})
	if err != nil {
		return 0, wrapError(err)
	}
	return int(total), nil
}

// Actors devolve os nomes distintos que já aparecem na trilha.
func (r *Audit) Actors(ctx context.Context) ([]string, error) {
	rows, err := r.queries.ListAuditActors(ctx)
	if err != nil {
		return nil, wrapError(err)
	}
	actors := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Valid && row.String != "" {
			actors = append(actors, row.String)
		}
	}
	return actors, nil
}

// auditListParams traduz o filtro de domínio nos parâmetros das consultas.
//
// O ator é sempre enviado como texto (nunca NULL): a consulta compara
// `coalesce(actor_name,”) = ? OR ? = ”`, e um NULL anularia a condição
// inteira.
func auditListParams(filter domain.AuditFilter) sqlcgen.ListAuditLogsDescParams {
	return sqlcgen.ListAuditLogsDescParams{
		Entity:     filter.Entity,
		Action:     filter.Action,
		Actor:      sql.NullString{String: filter.Actor, Valid: true},
		FromDate:   filter.From,
		ToDate:     auditToBound(filter.To),
		SearchLike: searchLike(filter.Search),
		PageOffset: int32(filter.Offset),
		PageLimit:  int32(filter.Limit),
	}
}

// searchLike monta o termo de busca do LIKE em minúsculas, porque as consultas
// comparam com `lower(coluna)`: o LIKE do SQLite já ignora maiúsculas, o do
// Postgres não.
func searchLike(search string) string {
	term := strings.TrimSpace(search)
	if term == "" {
		return ""
	}
	return "%" + strings.ToLower(term) + "%"
}

// auditToBound estende um limite superior só com data para o fim do dia. O app
// atual faz o mesmo ao montar a consulta (`${to}T23:59:59.999`), e sem isso os
// eventos do próprio dia ficariam de fora.
func auditToBound(to string) string {
	if len(to) == len("2006-01-02") {
		return to + "T23:59:59.999"
	}
	return to
}

func auditLogsFromRows(rows []sqlcgen.AuditLog) []domain.AuditLog {
	entries := make([]domain.AuditLog, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, domain.AuditLog{
			ID:          row.ID,
			CreatedAt:   row.CreatedAt,
			ActorID:     stringPtr(row.ActorID),
			ActorName:   stringPtr(row.ActorName),
			Action:      row.Action,
			Entity:      row.Entity,
			EntityID:    stringPtr(row.EntityID),
			EntityLabel: stringPtr(row.EntityLabel),
			Summary:     row.Summary,
			Changes:     stringPtr(row.Changes),
			Metadata:    stringPtr(row.Metadata),
			Origin:      domain.AuditOrigin(row.Origin),
			IP:          stringPtr(row.Ip),
			UserAgent:   stringPtr(row.UserAgent),
			RequestID:   stringPtr(row.RequestID),
		})
	}
	return entries
}
