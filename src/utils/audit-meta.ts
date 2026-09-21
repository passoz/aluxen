/**
 * Metadados de exibição da trilha de auditoria.
 *
 * Módulo puro (sem banco de dados e sem Node) para ser compartilhado entre o
 * servidor e o bundle do cliente: rótulos em português, tons visuais e
 * formatação dos valores registrados.
 */

export type AuditChange = {
  /** Campo técnico alterado (ex.: `rentAmount`). */
  field: string;
  /** Rótulo humano do campo (ex.: "Aluguel"). */
  label: string;
  /** Valor anterior já formatado; `null` em criação. */
  from: string | null;
  /** Valor novo já formatado; `null` em exclusão. */
  to: string | null;
};

export type AuditEntry = {
  id: string;
  createdAt: string;
  actorId: string | null;
  actorName: string | null;
  action: string;
  entity: string;
  entityId: string | null;
  entityLabel: string | null;
  summary: string;
  changes: AuditChange[];
  metadata: Record<string, string | number | boolean | null>;
  origin: string;
  ip: string | null;
  userAgent: string | null;
  requestId: string | null;
};

export type AuditPage = {
  items: AuditEntry[];
  total: number;
  page: number;
  pageSize: number;
  actors: string[];
};

export type AuditTone = 'create' | 'update' | 'delete' | 'issue' | 'access' | 'failure';

/** Filtros e paginação da trilha de auditoria (compartilhado cliente/servidor). */
export type AuditQuery = {
  page: number;
  pageSize: number;
  entity?: string;
  action?: string;
  actor?: string;
  from?: string;
  to?: string;
  search?: string;
  direction: 'asc' | 'desc';
};

/** Consulta padrão da aba Histórico: 25 eventos mais recentes, sem filtros. */
export const DEFAULT_AUDIT_QUERY: AuditQuery = {
  page: 1,
  pageSize: 25,
  direction: 'desc',
};

/** Chave estável de uma consulta — o cliente recarrega a trilha quando ela muda. */
export function auditQueryKey(query: AuditQuery): string {
  return [
    query.page,
    query.pageSize,
    query.direction,
    query.entity || '',
    query.action || '',
    query.actor || '',
    query.from || '',
    query.to || '',
    query.search || '',
  ].join('|');
}

export const ENTITY_LABELS: Record<string, string> = {
  tenant: 'Inquilino',
  property: 'Imóvel',
  lease: 'Contrato',
  receipt: 'Pagamento',
  setting: 'Configuração',
  user: 'Usuário',
  document: 'Documento',
  whatsapp: 'WhatsApp',
  backup: 'Backup',
};

type ActionMeta = { label: string; entity: string; tone: AuditTone };

export const ACTION_META: Record<string, ActionMeta> = {
  'tenant.create': { label: 'Inquilino cadastrado', entity: 'tenant', tone: 'create' },
  'tenant.update': { label: 'Inquilino alterado', entity: 'tenant', tone: 'update' },
  'tenant.delete': { label: 'Inquilino excluído', entity: 'tenant', tone: 'delete' },
  'property.create': { label: 'Imóvel cadastrado', entity: 'property', tone: 'create' },
  'property.update': { label: 'Imóvel alterado', entity: 'property', tone: 'update' },
  'property.delete': { label: 'Imóvel excluído', entity: 'property', tone: 'delete' },
  'lease.create': { label: 'Contrato criado', entity: 'lease', tone: 'create' },
  'lease.update': { label: 'Contrato alterado', entity: 'lease', tone: 'update' },
  'lease.terminate': { label: 'Contrato encerrado', entity: 'lease', tone: 'update' },
  'lease.suspend': { label: 'Contrato suspenso', entity: 'lease', tone: 'update' },
  'lease.reactivate': { label: 'Contrato reativado', entity: 'lease', tone: 'update' },
  'lease.delete': { label: 'Contrato excluído', entity: 'lease', tone: 'delete' },
  'receipt.create': { label: 'Pagamento lançado', entity: 'receipt', tone: 'create' },
  'receipt.update': { label: 'Pagamento alterado', entity: 'receipt', tone: 'update' },
  'receipt.pay': { label: 'Baixa registrada', entity: 'receipt', tone: 'update' },
  'receipt.batch-pay': { label: 'Baixa em lote', entity: 'receipt', tone: 'update' },
  'receipt.generate-monthly': { label: 'Pagamentos do mês gerados', entity: 'receipt', tone: 'create' },
  'receipt.delete': { label: 'Pagamento excluído', entity: 'receipt', tone: 'delete' },
  'receipt.batch-delete': { label: 'Pagamentos excluídos em lote', entity: 'receipt', tone: 'delete' },
  'setting.landlord': { label: 'Dados do locador alterados', entity: 'setting', tone: 'update' },
  'setting.app-url': { label: 'URL pública alterada', entity: 'setting', tone: 'update' },
  'user.password': { label: 'Senha alterada', entity: 'user', tone: 'update' },
  'auth.client-login': { label: 'Acesso do locatário', entity: 'tenant', tone: 'access' },
  'auth.client-login-failed': { label: 'Acesso do locatário negado', entity: 'tenant', tone: 'failure' },
  'auth.client-logout': { label: 'Saída do locatário', entity: 'tenant', tone: 'access' },
  'auth.client-password': { label: 'Senha do locatário alterada', entity: 'tenant', tone: 'update' },
  'auth.login': { label: 'Acesso ao sistema', entity: 'user', tone: 'access' },
  'auth.login-failed': { label: 'Tentativa de acesso negada', entity: 'user', tone: 'failure' },
  'auth.logout': { label: 'Saída do sistema', entity: 'user', tone: 'access' },
  'document.receipt': { label: 'Recibo emitido', entity: 'document', tone: 'issue' },
  'document.contract': { label: 'Contrato emitido', entity: 'document', tone: 'issue' },
  'whatsapp.send': { label: 'Envio por WhatsApp', entity: 'whatsapp', tone: 'issue' },
  'whatsapp.send-failed': { label: 'Falha no envio por WhatsApp', entity: 'whatsapp', tone: 'failure' },
  'whatsapp.logout': { label: 'WhatsApp desconectado', entity: 'whatsapp', tone: 'access' },
  'backup.export': { label: 'Backup exportado', entity: 'backup', tone: 'access' },
  'backup.import': { label: 'Backup importado', entity: 'backup', tone: 'create' },
  'history.export': { label: 'Histórico exportado (CSV)', entity: 'backup', tone: 'access' },
};

export const AUDIT_ORIGIN_LABELS: Record<string, string> = {
  web: 'Painel',
  public: 'Link público',
  client: 'Área do Cliente',
  job: 'Rotina automática',
};

export const AUDIT_METADATA_LABELS: Record<string, string> = {
  month: 'Mês de referência',
  count: 'Quantidade',
  batch: 'Itens no lote',
  channel: 'Canal',
  signature: 'Com assinatura',
  phone: 'Telefone',
  jobId: 'Protocolo de envio',
  kind: 'Tipo',
  reason: 'Motivo',
  username: 'Usuário',
  tenants: 'Inquilinos',
  properties: 'Imóveis',
  leases: 'Contratos',
  receipts: 'Pagamentos',
  fileName: 'Arquivo',
};

export function actionLabel(action: string): string {
  return ACTION_META[action]?.label || action;
}

export function entityLabel(entity: string): string {
  return ENTITY_LABELS[entity] || entity;
}

export function actionTone(action: string): AuditTone {
  return ACTION_META[action]?.tone || 'update';
}

export function formatAuditValue(value: string | number | null | undefined): string {
  if (value === null || value === undefined || value === '') return '—';
  return String(value);
}

/** Ações disponíveis para um filtro de entidade (`''` = todas). */
export function actionKeysForEntity(entity: string): string[] {
  return Object.entries(ACTION_META)
    .filter(([, meta]) => !entity || meta.entity === entity)
    .map(([key]) => key);
}

/** Resumo textual das alterações — usado no CSV e no detalhe da tabela. */
export function describeAuditChanges(changes: AuditChange[]): string {
  return changes
    .map((change) => `${change.label}: ${formatAuditValue(change.from)} → ${formatAuditValue(change.to)}`)
    .join(' | ');
}
