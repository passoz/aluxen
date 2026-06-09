import type { QueuePayload, OfflineMutation, MutationResponse, PendingIds, DashboardCache } from '../types/dashboard';

const DASHBOARD_CACHE_KEY = 'aluxen.dashboard-cache.v1';
const OFFLINE_QUEUE_KEY = 'aluxen.offline-queue.v1';

export function readDashboardCache() {
  if (typeof window === 'undefined') return null;
  const raw = window.localStorage.getItem(DASHBOARD_CACHE_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as DashboardCache;
  } catch {
    return null;
  }
}

export function writeDashboardCache(cache: DashboardCache) {
  if (typeof window === 'undefined') return;
  window.localStorage.setItem(DASHBOARD_CACHE_KEY, JSON.stringify(cache));
}

export function readOfflineQueue() {
  if (typeof window === 'undefined') return [] as OfflineMutation[];
  const raw = window.localStorage.getItem(OFFLINE_QUEUE_KEY);
  if (!raw) return [] as OfflineMutation[];
  try {
    return JSON.parse(raw) as OfflineMutation[];
  } catch {
    return [] as OfflineMutation[];
  }
}

export function writeOfflineQueue(queue: OfflineMutation[]) {
  if (typeof window === 'undefined') return;
  window.localStorage.setItem(OFFLINE_QUEUE_KEY, JSON.stringify(queue));
}

export function enqueueOfflineMutation(mutation: OfflineMutation) {
  const queue = [...readOfflineQueue(), mutation];
  writeOfflineQueue(queue);
  return queue;
}

export function serializeFormData(formData: FormData) {
  const payload: QueuePayload = {};
  for (const [key, value] of formData.entries()) {
    if (typeof value === 'string') {
      payload[key] = value;
    }
  }
  return payload;
}

export async function submitQueuedMutation(payload: QueuePayload) {
  const formData = new FormData();
  Object.entries(payload).forEach(([key, value]) => formData.set(key, value));
  const response = await fetch('/dashboard', {
    method: 'POST',
    body: formData,
    credentials: 'same-origin',
  });

  const contentType = response.headers.get('content-type') || '';
  const data = contentType.includes('application/json')
    ? (await response.json()) as MutationResponse
    : { error: response.ok ? undefined : 'Falha ao sincronizar dados pendentes.' };

  if (!response.ok || data.error) {
    throw new Error(data.error || 'Falha ao sincronizar dados pendentes.');
  }
}

export function getPendingIds(queue: OfflineMutation[]): PendingIds {
  const pending: PendingIds = {
    tenants: new Set<string>(),
    properties: new Set<string>(),
    leases: new Set<string>(),
    receipts: new Set<string>(),
  };

  for (const mutation of queue) {
    const { intent, id } = mutation.payload;
    if (!id) continue;
    if (intent.startsWith('tenant.')) pending.tenants.add(id);
    if (intent.startsWith('property.')) pending.properties.add(id);
    if (intent.startsWith('lease.')) pending.leases.add(id);
    if (intent.startsWith('receipt.')) pending.receipts.add(id);
  }

  return pending;
}

export function getSyncErrorMessage(payload: QueuePayload, error: unknown) {
  const fallback = error instanceof Error ? error.message : 'Falha ao sincronizar dados pendentes.';
  if (fallback.includes('UNIQUE constraint failed')) {
    if (payload.intent?.startsWith('tenant.')) return 'Conflito ao sincronizar inquilino: email ou identificador ja existe no servidor.';
    if (payload.intent?.startsWith('property.')) return 'Conflito ao sincronizar imovel: identificador ja existe no servidor.';
    if (payload.intent?.startsWith('lease.')) return 'Conflito ao sincronizar contrato: identificador ja existe no servidor.';
    if (payload.intent?.startsWith('receipt.')) return 'Conflito ao sincronizar pagamento: identificador ou geracao mensal ja existe no servidor.';
  }
  if (fallback.includes('Contrato não encontrado.')) return 'Sincronizacao bloqueada: um pagamento pendente referencia um contrato inexistente no servidor.';
  if (fallback.includes('Credenciais inválidas.') || fallback.includes('/login')) return 'Sessao expirada. Entre novamente para sincronizar os dados pendentes.';
  return fallback;
}

export function isCreateIntent(intent: string) {
  return intent === 'tenant.create' || intent === 'property.create' || intent === 'lease.create' || intent === 'receipt.create' || intent === 'lease.terminate' || intent === 'lease.reactivate';
}
