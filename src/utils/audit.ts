/**
 * Trilha de auditoria do Aluxen (server-only).
 *
 * Responsabilidades:
 * - Montar o contexto do evento (quem, de onde, qual request).
 * - Diferenciar snapshots antes/depois e gravar a alteração em `audit_logs`.
 * - Consultar a trilha com filtros, paginação e exportação CSV.
 *
 * O registro é sempre "melhor esforço": uma falha ao gravar auditoria nunca
 * derruba a operação de negócio já efetivada, mas é reportada nos logs.
 */
import type { Context } from 'hono';
import { and, asc, desc, eq, gte, like, lte, or, sql } from 'drizzle-orm';
import { db } from '../db';
import { auditLogs, leases, properties, receipts, tenants, users } from '../db/schema';
import { createId } from './id';
import { createLogger } from './logger';
import {
  formatCurrency,
  formatDateBr,
  formatDateTimeBr,
  formatLocalDateTime,
  LEASE_STATUS_LABELS,
  RECEIPT_CATEGORY_LABELS,
} from './format';
import type { AuditChange, AuditEntry, AuditPage, AuditQuery as SharedAuditQuery } from './audit-meta';
import { DEFAULT_AUDIT_QUERY, describeAuditChanges } from './audit-meta';

const log = createLogger('audit');

/**
 * Timestamp da trilha no horário local do servidor, com milissegundos:
 * `YYYY-MM-DDTHH:mm:ss.SSS`. Segue a mesma convenção dos demais horários do
 * sistema (texto de relógio de parede), o que mantém a ordenação lexicográfica
 * correta e evita divergência entre o HTML renderizado no servidor e a hidratação.
 */
function auditTimestamp(now: Date): string {
  const seconds = String(now.getSeconds()).padStart(2, '0');
  const millis = String(now.getMilliseconds()).padStart(3, '0');
  return `${formatLocalDateTime(now)}:${seconds}.${millis}`;
}

export type AuditOrigin = 'web' | 'public' | 'job';

export type AuditActor = { id: string; name: string };

export type AuditContext = {
  actorId: string | null;
  actorName: string | null;
  origin: AuditOrigin;
  ip: string;
  userAgent: string;
  requestId: string;
};

export type AuditEvent = {
  action: string;
  entity: string;
  entityId?: string | null;
  entityLabel?: string | null;
  summary: string;
  changes?: AuditChange[];
  metadata?: Record<string, string | number | boolean | null>;
};

type AuditValueFormat = 'currency' | 'date' | 'datetime' | 'leaseStatus' | 'receiptCategory';
type AuditFieldSpec = { key: string; label: string; format?: AuditValueFormat };

export const TENANT_AUDIT_FIELDS: AuditFieldSpec[] = [
  { key: 'name', label: 'Nome' },
  { key: 'email', label: 'E-mail' },
  { key: 'phone', label: 'Telefone' },
  { key: 'cpf', label: 'CPF' },
];

export const PROPERTY_AUDIT_FIELDS: AuditFieldSpec[] = [
  { key: 'name', label: 'Nome' },
  { key: 'address', label: 'Endereço' },
  { key: 'city', label: 'Cidade' },
  { key: 'state', label: 'UF' },
  { key: 'zip', label: 'CEP' },
];

export const LEASE_AUDIT_FIELDS: AuditFieldSpec[] = [
  { key: 'tenant_name', label: 'Inquilino' },
  { key: 'property_name', label: 'Imóvel' },
  { key: 'startDate', label: 'Início', format: 'date' },
  { key: 'endDate', label: 'Término', format: 'date' },
  { key: 'dueDay', label: 'Dia de vencimento' },
  { key: 'rentAmount', label: 'Aluguel', format: 'currency' },
  { key: 'status', label: 'Situação', format: 'leaseStatus' },
];

export const RECEIPT_AUDIT_FIELDS: AuditFieldSpec[] = [
  { key: 'tenant_name', label: 'Inquilino' },
  { key: 'property_name', label: 'Imóvel' },
  { key: 'category', label: 'Categoria', format: 'receiptCategory' },
  { key: 'description', label: 'Descrição' },
  { key: 'dueDate', label: 'Vencimento', format: 'date' },
  { key: 'paidAt', label: 'Pago em', format: 'datetime' },
  { key: 'amount', label: 'Valor', format: 'currency' },
  { key: 'paidAmount', label: 'Valor pago', format: 'currency' },
];

function formatAuditFieldValue(value: unknown, format?: AuditValueFormat): string | null {
  if (value === null || value === undefined || value === '') return null;
  switch (format) {
    case 'currency': {
      const numeric = Number(value);
      return Number.isFinite(numeric) ? formatCurrency(numeric) : String(value);
    }
    case 'date':
      return formatDateBr(String(value));
    case 'datetime':
      return formatDateTimeBr(String(value));
    case 'leaseStatus':
      return LEASE_STATUS_LABELS[String(value)] || String(value);
    case 'receiptCategory':
      return RECEIPT_CATEGORY_LABELS[String(value)] || String(value);
    default:
      return String(value);
  }
}

/** Compara dois snapshots e devolve apenas os campos que realmente mudaram, já formatados. */
export function diffAuditFields(
  before: Record<string, unknown> | null,
  after: Record<string, unknown> | null,
  specs: AuditFieldSpec[]
): AuditChange[] {
  const changes: AuditChange[] = [];
  for (const spec of specs) {
    const from = formatAuditFieldValue(before?.[spec.key], spec.format);
    const to = formatAuditFieldValue(after?.[spec.key], spec.format);
    if (from === to) continue;
    changes.push({ field: spec.key, label: spec.label, from, to });
  }
  return changes;
}

/** IP do cliente, considerando proxy reverso (mesma regra usada pelo rate limit de login). */
export function clientIp(c: Context): string {
  return c.req.header('x-forwarded-for')?.split(',')[0]?.trim() || c.req.header('x-real-ip') || 'desconhecido';
}

/** Monta o contexto de auditoria a partir da requisição. Não toca no banco. */
export function createAuditContext(c: Context, actor: AuditActor | null, origin: AuditOrigin = 'web'): AuditContext {
  return {
    actorId: actor?.id ?? null,
    actorName: actor?.name ?? null,
    origin,
    ip: clientIp(c),
    userAgent: c.req.header('user-agent')?.slice(0, 300) || '',
    requestId: (c.get('requestId') as string | undefined) || '',
  };
}

/** Busca o nome do usuário autenticado para assinar os eventos da trilha. */
export async function resolveAuditActor(userId: string | null): Promise<AuditActor | null> {
  if (!userId) return null;
  const rows = await db.select({ id: users.id, name: users.username }).from(users).where(eq(users.id, userId)).limit(1);
  return rows[0] ?? null;
}

/**
 * Grava um evento na trilha. Eventos sem nenhuma alteração real (update que não
 * mudou nada) são descartados para não poluir o histórico.
 */
export async function recordAudit(context: AuditContext, event: AuditEvent): Promise<boolean> {
  if (event.changes && event.changes.length === 0) return false;

  const changes = event.changes && event.changes.length > 0 ? event.changes : null;
  const metadata = event.metadata && Object.keys(event.metadata).length > 0 ? event.metadata : null;

  try {
    await db.insert(auditLogs).values({
      id: createId(),
      createdAt: auditTimestamp(new Date()),
      actorId: context.actorId,
      actorName: context.actorName,
      action: event.action,
      entity: event.entity,
      entityId: event.entityId ?? null,
      entityLabel: event.entityLabel ?? null,
      summary: event.summary,
      changes: changes ? JSON.stringify(changes) : null,
      metadata: metadata ? JSON.stringify(metadata) : null,
      origin: context.origin,
      ip: context.ip,
      userAgent: context.userAgent,
      requestId: context.requestId,
    });
  } catch (error) {
    log.error('Falha ao gravar evento de auditoria', {
      action: event.action,
      entity: event.entity,
      entityId: event.entityId,
      error,
    });
    return false;
  }

  log.info('Evento de auditoria', {
    action: event.action,
    entity: event.entity,
    entityId: event.entityId,
    entityLabel: event.entityLabel,
    actor: context.actorName,
    origin: context.origin,
    changes: changes?.length ?? 0,
  });
  return true;
}

// ---------------------------------------------------------------------------
// Leituras de estado usadas para montar antes/depois
// ---------------------------------------------------------------------------

export async function getTenantSnapshot(id: string) {
  const rows = await db
    .select({ id: tenants.id, name: tenants.name, email: tenants.email, phone: tenants.phone, cpf: tenants.cpf })
    .from(tenants)
    .where(eq(tenants.id, id))
    .limit(1);
  return rows[0] ?? null;
}

export async function getPropertySnapshot(id: string) {
  const rows = await db
    .select({
      id: properties.id,
      name: properties.name,
      address: properties.address,
      city: properties.city,
      state: properties.state,
      zip: properties.zip,
    })
    .from(properties)
    .where(eq(properties.id, id))
    .limit(1);
  return rows[0] ?? null;
}

export async function getLeaseSnapshot(id: string) {
  const rows = await db
    .select({
      id: leases.id,
      tenantId: leases.tenantId,
      propertyId: leases.propertyId,
      startDate: leases.startDate,
      endDate: leases.endDate,
      dueDay: leases.dueDay,
      rentAmount: leases.rentAmount,
      status: leases.status,
      tenant_name: tenants.name,
      property_name: properties.name,
    })
    .from(leases)
    .innerJoin(tenants, eq(leases.tenantId, tenants.id))
    .innerJoin(properties, eq(leases.propertyId, properties.id))
    .where(eq(leases.id, id))
    .limit(1);
  return rows[0] ?? null;
}

export async function getReceiptSnapshot(id: string) {
  const rows = await db
    .select({
      id: receipts.id,
      leaseId: receipts.leaseId,
      tenantId: receipts.tenantId,
      propertyId: receipts.propertyId,
      category: receipts.category,
      description: receipts.description,
      dueDate: receipts.dueDate,
      paidAt: receipts.paidAt,
      amount: receipts.amount,
      paidAmount: receipts.paidAmount,
      tenant_name: sql<string>`coalesce(${tenants.name}, '')`,
      property_name: sql<string>`coalesce(${properties.name}, '')`,
    })
    .from(receipts)
    .leftJoin(leases, eq(receipts.leaseId, leases.id))
    .leftJoin(tenants, sql`${tenants.id} = coalesce(${leases.tenantId}, ${receipts.tenantId})`)
    .leftJoin(properties, sql`${properties.id} = coalesce(${leases.propertyId}, ${receipts.propertyId})`)
    .where(eq(receipts.id, id))
    .limit(1);
  return rows[0] ?? null;
}

/** Rótulo humano de um pagamento: inquilino, imóvel ou descrição, e competência. */
export function describeReceipt(snapshot: { tenant_name: string; property_name: string; dueDate: string; description?: string | null } | null): string {
  if (!snapshot) return 'Pagamento';
  const period = formatDateBr(snapshot.dueDate).slice(3);
  const who = [snapshot.tenant_name, snapshot.property_name].filter(Boolean).join(' · ');
  if (who) return `${who} · ${period}`;
  return snapshot.description ? `${snapshot.description} · ${period}` : `Competência ${period}`;
}

/** Rótulo humano de um contrato: inquilino, imóvel e vigência. */
export function describeLease(snapshot: { tenant_name: string; property_name: string } | null): string {
  if (!snapshot) return 'Contrato';
  return [snapshot.tenant_name, snapshot.property_name].filter(Boolean).join(' · ') || 'Contrato';
}

// ---------------------------------------------------------------------------
// Consulta da trilha
// ---------------------------------------------------------------------------

export type AuditQuery = SharedAuditQuery;

const DATE_ONLY = /^\d{4}-\d{2}-\d{2}$/;

export function readAuditQuery(params: URLSearchParams): AuditQuery {
  const page = Math.max(1, Number(params.get('page')) || DEFAULT_AUDIT_QUERY.page);
  const requestedSize = Number(params.get('pageSize')) || DEFAULT_AUDIT_QUERY.pageSize;
  const from = params.get('from') || '';
  const to = params.get('to') || '';
  return {
    page,
    pageSize: Math.min(200, Math.max(5, requestedSize)),
    entity: params.get('entity')?.trim() || undefined,
    action: params.get('action')?.trim() || undefined,
    actor: params.get('actor')?.trim() || undefined,
    from: DATE_ONLY.test(from) ? from : undefined,
    to: DATE_ONLY.test(to) ? to : undefined,
    search: params.get('search')?.trim() || undefined,
    direction: params.get('order') === 'asc' ? 'asc' : 'desc',
  };
}

function parseJsonRecord(raw: string | null): Record<string, string | number | null> {
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? (parsed as Record<string, string | number | null>) : {};
  } catch {
    return {};
  }
}

function parseChanges(raw: string | null): AuditChange[] {
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? (parsed as AuditChange[]) : [];
  } catch {
    return [];
  }
}

export async function listAuditActors(): Promise<string[]> {
  const rows = await db
    .selectDistinct({ actorName: auditLogs.actorName })
    .from(auditLogs)
    .orderBy(asc(auditLogs.actorName));
  return rows.map((row) => row.actorName).filter((name): name is string => !!name);
}

function auditFilterConditions(query: AuditQuery) {
  const filters = [];
  if (query.entity) filters.push(eq(auditLogs.entity, query.entity));
  if (query.action) filters.push(eq(auditLogs.action, query.action));
  if (query.actor) filters.push(eq(auditLogs.actorName, query.actor));
  if (query.from) filters.push(gte(auditLogs.createdAt, query.from));
  if (query.to) filters.push(lte(auditLogs.createdAt, `${query.to}T23:59:59.999`));
  if (query.search) {
    const term = `%${query.search}%`;
    filters.push(
      or(
        like(auditLogs.summary, term),
        like(auditLogs.entityLabel, term),
        like(auditLogs.actorName, term),
        like(auditLogs.entityId, term)
      )
    );
  }
  return filters.length > 0 ? and(...filters) : undefined;
}

function toAuditEntry(row: typeof auditLogs.$inferSelect): AuditEntry {
  return {
    id: row.id,
    createdAt: row.createdAt,
    actorId: row.actorId,
    actorName: row.actorName,
    action: row.action,
    entity: row.entity,
    entityId: row.entityId,
    entityLabel: row.entityLabel,
    summary: row.summary,
    changes: parseChanges(row.changes),
    metadata: parseJsonRecord(row.metadata),
    origin: row.origin,
    ip: row.ip,
    userAgent: row.userAgent,
    requestId: row.requestId,
  };
}

/** Limite de linhas de uma exportação CSV — evita gerar arquivos gigantes em memória. */
export const AUDIT_EXPORT_LIMIT = 5000;

export async function listAudit(query: AuditQuery): Promise<AuditPage> {
  const where = auditFilterConditions(query);

  const [totalRow, rows] = await Promise.all([
    db.select({ total: sql<number>`count(*)` }).from(auditLogs).where(where),
    db
      .select()
      .from(auditLogs)
      .where(where)
      .orderBy(query.direction === 'asc' ? asc(auditLogs.createdAt) : desc(auditLogs.createdAt))
      .limit(query.pageSize)
      .offset((query.page - 1) * query.pageSize),
  ]);

  return {
    items: rows.map(toAuditEntry),
    total: Number(totalRow[0]?.total ?? 0),
    page: query.page,
    pageSize: query.pageSize,
    actors: await listAuditActors(),
  };
}

/** Todos os eventos que casam com o filtro (até {@link AUDIT_EXPORT_LIMIT}) para exportação. */
export async function listAuditForExport(query: AuditQuery): Promise<AuditEntry[]> {
  const rows = await db
    .select()
    .from(auditLogs)
    .where(auditFilterConditions(query))
    .orderBy(query.direction === 'asc' ? asc(auditLogs.createdAt) : desc(auditLogs.createdAt))
    .limit(AUDIT_EXPORT_LIMIT);
  return rows.map(toAuditEntry);
}

const CSV_HEADERS = ['Data/Hora', 'Usuário', 'Ação', 'Registro', 'Resumo', 'Alterações', 'Origem', 'IP', 'Request'];

function csvCell(value: string | number | null | undefined): string {
  const text = value === null || value === undefined ? '' : String(value);
  return `"${text.replace(/"/g, '""')}"`;
}

/** CSV (separador `;`, BOM UTF-8) pronto para o Excel em português. */
export function buildAuditCsv(entries: AuditEntry[]): string {
  const lines = [CSV_HEADERS.map(csvCell).join(';')];
  for (const entry of entries) {
    lines.push(
      [
        csvCell(formatDateTimeBr(entry.createdAt) || entry.createdAt),
        csvCell(entry.actorName || 'Sistema'),
        csvCell(entry.action),
        csvCell(entry.entityLabel || entry.entityId || ''),
        csvCell(entry.summary),
        csvCell(describeAuditChanges(entry.changes)),
        csvCell(entry.origin),
        csvCell(entry.ip || ''),
        csvCell(entry.requestId || ''),
      ].join(';')
    );
  }
  return `\uFEFF${lines.join('\r\n')}\r\n`;
}
