import React, { useEffect, useMemo, useRef, useState } from 'react';
import { DatePickerInput, FilterBar, Modal, Pagination, SmallSelect } from '../components/ui';
import { formatDateTimeBr } from '../utils/format';
import {
  actionKeysForEntity,
  actionLabel,
  actionTone,
  auditQueryKey,
  AUDIT_METADATA_LABELS,
  AUDIT_ORIGIN_LABELS,
  DEFAULT_AUDIT_QUERY,
  ENTITY_LABELS,
  entityLabel,
  formatAuditValue,
  type AuditEntry,
  type AuditPage,
  type AuditQuery,
} from '../utils/audit-meta';

/** Monta a query string do endpoint /audit a partir dos filtros da tela. */
function toSearchParams(query: AuditQuery): URLSearchParams {
  const params = new URLSearchParams({ page: String(query.page), pageSize: String(query.pageSize), order: query.direction });
  if (query.entity) params.set('entity', query.entity);
  if (query.action) params.set('action', query.action);
  if (query.actor) params.set('actor', query.actor);
  if (query.from) params.set('from', query.from);
  if (query.to) params.set('to', query.to);
  if (query.search) params.set('search', query.search);
  return params;
}

function ChangesTable({ changes }: { changes: AuditEntry['changes'] }) {
  if (changes.length === 0) {
    return <p className="subtitle">Este evento não alterou valores de campos.</p>;
  }
  return (
    <div className="table-wrap">
      <table className="history-changes">
        <thead>
          <tr>
            <th>Campo</th>
            <th>Antes</th>
            <th>Depois</th>
          </tr>
        </thead>
        <tbody>
          {changes.map((change) => (
            <tr key={change.field}>
              <td><strong>{change.label}</strong></td>
              <td className="history-old">{formatAuditValue(change.from)}</td>
              <td className="history-new">{formatAuditValue(change.to)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function HistoryView({ initial }: { initial?: AuditPage }) {
  const [query, setQuery] = useState<AuditQuery>(DEFAULT_AUDIT_QUERY);
  const [data, setData] = useState<AuditPage | null>(initial ?? null);
  const [search, setSearch] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [detail, setDetail] = useState<AuditEntry | null>(null);
  const requestRef = useRef(0);

  const queryKey = auditQueryKey(query);
  // A primeira renderização já vem preenchida pelo SSR; só recarregamos quando o filtro muda.
  const skipKeyRef = useRef(initial ? auditQueryKey(DEFAULT_AUDIT_QUERY) : null);

  useEffect(() => {
    const timer = setTimeout(() => {
      setQuery((current) => (current.search === search.trim() ? current : { ...current, search: search.trim(), page: 1 }));
    }, 300);
    return () => clearTimeout(timer);
  }, [search]);

  useEffect(() => {
    if (skipKeyRef.current === queryKey) {
      skipKeyRef.current = null;
      return;
    }
    const requestId = requestRef.current + 1;
    requestRef.current = requestId;
    setLoading(true);
    setError('');
    fetch(`/audit?${toSearchParams(query).toString()}`, { headers: { accept: 'application/json' } })
      .then(async (response) => {
        if (!response.ok) throw new Error('Não foi possível carregar o histórico.');
        return (await response.json()) as AuditPage;
      })
      .then((page) => {
        if (requestRef.current !== requestId) return;
        setData(page);
      })
      .catch((err: unknown) => {
        if (requestRef.current !== requestId) return;
        setError(err instanceof Error ? err.message : 'Não foi possível carregar o histórico.');
      })
      .finally(() => {
        if (requestRef.current !== requestId) return;
        setLoading(false);
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [queryKey]);

  const actionOptions = useMemo(() => actionKeysForEntity(query.entity || ''), [query.entity]);
  const items = data?.items ?? [];
  const total = data?.total ?? 0;
  const pageSize = data?.pageSize ?? query.pageSize;
  const totalPages = Math.max(1, Math.ceil(total / Math.max(1, pageSize)));
  const exportUrl = `/audit/export?${toSearchParams({ ...query, page: 1 }).toString()}`;

  const updateQuery = (patch: Partial<AuditQuery>) => {
    setQuery((current) => ({ ...current, ...patch, page: patch.page ?? 1 }));
  };

  return (
    <section className="card stack-lg history-section">
      <div className="section-header">
        <h2>Histórico e Auditoria</h2>
        <div className="auth-actions">
          <span className="status-badge paid" style={{ fontSize: '0.8125rem', padding: '0.4rem 0.8rem' }}>
            {total} evento(s)
          </span>
          <a className="btn btn-secondary" href={exportUrl}>
            Exportar CSV
          </a>
        </div>
      </div>

      <FilterBar value={search} onChange={setSearch} placeholder="Buscar por resumo, registro ou usuário">
        <SmallSelect value={query.entity || ''} onChange={(value) => updateQuery({ entity: value || undefined, action: undefined })}>
          <option value="">Todas as áreas</option>
          {Object.entries(ENTITY_LABELS).map(([entity, label]) => (
            <option key={entity} value={entity}>{label}</option>
          ))}
        </SmallSelect>
        <SmallSelect value={query.action || ''} onChange={(value) => updateQuery({ action: value || undefined })}>
          <option value="">Todas as ações</option>
          {actionOptions.map((action) => (
            <option key={action} value={action}>{actionLabel(action)}</option>
          ))}
        </SmallSelect>
        <SmallSelect value={query.actor || ''} onChange={(value) => updateQuery({ actor: value || undefined })}>
          <option value="">Todos os usuários</option>
          {(data?.actors ?? []).map((actor) => (
            <option key={actor} value={actor}>{actor}</option>
          ))}
        </SmallSelect>
        <DatePickerInput id="history-from" name="history-from" label="De" value={query.from ?? ''} onChange={(value) => updateQuery({ from: value || undefined })} />
        <DatePickerInput id="history-to" name="history-to" label="Até" value={query.to ?? ''} onChange={(value) => updateQuery({ to: value || undefined })} />
      </FilterBar>

      {error ? <div className="error">{error}</div> : null}

      <div className="table-wrap">
        <table className="history-table">
          <thead>
            <tr>
              <th style={{ width: '11rem' }}>
                <button
                  type="button"
                  className={`sort-header${query.direction === 'desc' ? ' active' : ''}`}
                  onClick={() => updateQuery({ direction: query.direction === 'desc' ? 'asc' : 'desc' })}
                >
                  Data e hora
                  <span className="sort-arrows">
                    <svg className={`sort-arrow asc${query.direction === 'asc' ? ' active' : ''}`} width="8" height="5" viewBox="0 0 8 5" fill="none">
                      <path d="M4 0L7.5 5H0.5L4 0Z" />
                    </svg>
                    <svg className={`sort-arrow desc${query.direction === 'desc' ? ' active' : ''}`} width="8" height="5" viewBox="0 0 8 5" fill="none">
                      <path d="M4 5L0.5 0H7.5L4 5Z" />
                    </svg>
                  </span>
                </button>
              </th>
              <th style={{ width: '10rem' }}>Usuário</th>
              <th style={{ width: '12rem' }}>Ação</th>
              <th>Registro e resumo</th>
              <th style={{ width: '9rem' }} />
            </tr>
          </thead>
          <tbody>
            {items.length === 0 && !loading ? (
              <tr>
                <td colSpan={5} style={{ textAlign: 'center', color: 'var(--text-muted)' }}>
                  Nenhum evento encontrado com os filtros atuais.
                </td>
              </tr>
            ) : null}
            {loading && items.length === 0 ? (
              <tr>
                <td colSpan={5} style={{ textAlign: 'center', color: 'var(--text-muted)' }}>Carregando histórico...</td>
              </tr>
            ) : null}
            {items.map((entry) => (
              <tr key={entry.id}>
                <td className="history-when">{formatDateTimeBr(entry.createdAt)}</td>
                <td>{entry.actorName || 'Sistema'}</td>
                <td>
                  <span className={`history-badge ${actionTone(entry.action)}`}>{actionLabel(entry.action)}</span>
                </td>
                <td>
                  <div className="history-record">
                    <span className="history-entity">{entityLabel(entry.entity)}</span>
                    <strong>{entry.entityLabel || entry.entityId || '—'}</strong>
                  </div>
                  <div className="history-summary">
                    {entry.summary}
                    {entry.changes.length > 0 ? ` · ${entry.changes.length} campo(s) alterado(s)` : ''}
                  </div>
                </td>
                <td className="row-actions">
                  <button
                    type="button"
                    className="btn btn-sm btn-secondary"
                    aria-label={`Detalhes do evento: ${entry.summary}`}
                    onClick={() => setDetail(entry)}
                  >
                    Detalhes
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <Pagination
        page={data?.page ?? query.page}
        totalPages={totalPages}
        onChange={(page) => setQuery((current) => ({ ...current, page }))}
        pageSize={query.pageSize}
        onPageSizeChange={(size) => setQuery((current) => ({ ...current, pageSize: size, page: 1 }))}
        pageSizeOptions={[10, 25, 50, 100]}
      />

      {detail ? (
        <Modal title={actionLabel(detail.action)} onClose={() => setDetail(null)} wide>
          <div className="history-detail">
            <p className="history-summary">{detail.summary}</p>
            <dl className="history-meta">
              <div>
                <dt>Data e hora</dt>
                <dd>{formatDateTimeBr(detail.createdAt)}</dd>
              </div>
              <div>
                <dt>Usuário</dt>
                <dd>{detail.actorName || 'Sistema'}</dd>
              </div>
              <div>
                <dt>Área</dt>
                <dd>{entityLabel(detail.entity)}</dd>
              </div>
              <div>
                <dt>Registro</dt>
                <dd>{detail.entityLabel || detail.entityId || '—'}</dd>
              </div>
              <div>
                <dt>Origem</dt>
                <dd>{AUDIT_ORIGIN_LABELS[detail.origin] || detail.origin}</dd>
              </div>
              <div>
                <dt>IP</dt>
                <dd>{detail.ip || '—'}</dd>
              </div>
              <div>
                <dt>Protocolo</dt>
                <dd>{detail.requestId || '—'}</dd>
              </div>
              <div>
                <dt>Identificador</dt>
                <dd>{detail.entityId || '—'}</dd>
              </div>
            </dl>

            <ChangesTable changes={detail.changes} />

            {Object.keys(detail.metadata).length > 0 ? (
              <dl className="history-meta">
                {Object.entries(detail.metadata).map(([key, value]) => (
                  <div key={key}>
                    <dt>{AUDIT_METADATA_LABELS[key] || key}</dt>
                    <dd>{value === null ? '—' : String(value)}</dd>
                  </div>
                ))}
              </dl>
            ) : null}
          </div>
        </Modal>
      ) : null}
    </section>
  );
}
