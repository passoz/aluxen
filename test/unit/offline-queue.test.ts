import { describe, it, expect, beforeEach } from 'vitest';

type QueuePayload = Record<string, string>;
type OfflineMutation = {
  id: string;
  payload: QueuePayload;
  successMessage: string;
};

const OFFLINE_QUEUE_KEY = 'aluxen.offline-queue.v1';

function readOfflineQueue(): OfflineMutation[] {
  if (typeof window === 'undefined') return [];
  const raw = window.localStorage.getItem(OFFLINE_QUEUE_KEY);
  if (!raw) return [];
  try {
    return JSON.parse(raw) as OfflineMutation[];
  } catch {
    return [];
  }
}

function writeOfflineQueue(queue: OfflineMutation[]) {
  if (typeof window === 'undefined') return;
  window.localStorage.setItem(OFFLINE_QUEUE_KEY, JSON.stringify(queue));
}

function enqueueOfflineMutation(mutation: OfflineMutation) {
  const queue = [...readOfflineQueue(), mutation];
  writeOfflineQueue(queue);
  return queue;
}

function serializeFormData(formData: FormData) {
  const payload: QueuePayload = {};
  for (const [key, value] of formData.entries()) {
    if (typeof value === 'string') {
      payload[key] = value;
    }
  }
  return payload;
}

function isCreateIntent(intent: string) {
  return intent === 'tenant.create' || intent === 'property.create' || intent === 'lease.create' || intent === 'receipt.create' || intent === 'lease.terminate' || intent === 'lease.reactivate';
}

function buildOfflinePayload(form: HTMLFormElement) {
  const payload = serializeFormData(new FormData(form));
  if (isCreateIntent(payload.intent) && !payload.id) {
    payload.id = crypto.randomUUID();
  }
  return payload;
}

type PendingIds = {
  tenants: Set<string>;
  properties: Set<string>;
  leases: Set<string>;
  receipts: Set<string>;
};

function getPendingIds(queue: OfflineMutation[]): PendingIds {
  const pending: PendingIds = {
    tenants: new Set(),
    properties: new Set(),
    leases: new Set(),
    receipts: new Set(),
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

describe('Offline Queue', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it('should start with empty queue', () => {
    expect(readOfflineQueue()).toEqual([]);
  });

  it('should enqueue a mutation', () => {
    const mutation: OfflineMutation = {
      id: '1',
      payload: { intent: 'tenant.create', name: 'John', email: 'john@test.com' },
      successMessage: 'Ok',
    };

    const queue = enqueueOfflineMutation(mutation);
    expect(queue).toHaveLength(1);
    expect(queue[0].id).toBe('1');
  });

  it('should enqueue multiple mutations', () => {
    enqueueOfflineMutation({ id: '1', payload: { intent: 'tenant.create' }, successMessage: 'Ok' });
    enqueueOfflineMutation({ id: '2', payload: { intent: 'property.create' }, successMessage: 'Ok' });

    const queue = readOfflineQueue();
    expect(queue).toHaveLength(2);
  });

  it('should persist mutations across reads', () => {
    enqueueOfflineMutation({ id: 'persist-test', payload: { intent: 'lease.create' }, successMessage: 'Ok' });
    expect(readOfflineQueue()).toHaveLength(1);
    expect(readOfflineQueue()[0].id).toBe('persist-test');
  });

  it('should handle empty localStorage gracefully', () => {
    window.localStorage.removeItem(OFFLINE_QUEUE_KEY);
    expect(readOfflineQueue()).toEqual([]);
  });

  it('should handle corrupted localStorage gracefully', () => {
    window.localStorage.setItem(OFFLINE_QUEUE_KEY, 'not-json');
    expect(readOfflineQueue()).toEqual([]);
  });
});

describe('getPendingIds', () => {
  it('should return empty sets for empty queue', () => {
    const result = getPendingIds([]);
    expect(result.tenants.size).toBe(0);
    expect(result.properties.size).toBe(0);
    expect(result.leases.size).toBe(0);
    expect(result.receipts.size).toBe(0);
  });

  it('should categorize mutations by entity type', () => {
    const queue: OfflineMutation[] = [
      { id: 'a', payload: { intent: 'tenant.create', id: 'tenant-1' }, successMessage: '' },
      { id: 'b', payload: { intent: 'property.create', id: 'prop-1' }, successMessage: '' },
      { id: 'c', payload: { intent: 'lease.create', id: 'lease-1' }, successMessage: '' },
      { id: 'd', payload: { intent: 'receipt.create', id: 'receipt-1' }, successMessage: '' },
    ];

    const result = getPendingIds(queue);
    expect(result.tenants.has('tenant-1')).toBe(true);
    expect(result.properties.has('prop-1')).toBe(true);
    expect(result.leases.has('lease-1')).toBe(true);
    expect(result.receipts.has('receipt-1')).toBe(true);
  });

  it('should handle mutations without id', () => {
    const queue: OfflineMutation[] = [
      { id: 'x', payload: { intent: 'tenant.create' }, successMessage: '' },
    ];

    const result = getPendingIds(queue);
    expect(result.tenants.size).toBe(0);
  });

  it('should handle unknown intent types', () => {
    const queue: OfflineMutation[] = [
      { id: 'x', payload: { intent: 'unknown.action', id: 'foo' }, successMessage: '' },
    ];

    const result = getPendingIds(queue);
    expect(result.tenants.size).toBe(0);
    expect(result.properties.size).toBe(0);
    expect(result.leases.size).toBe(0);
    expect(result.receipts.size).toBe(0);
  });
});

describe('buildOfflinePayload', () => {
  it('should add id for create intents', () => {
    const form = document.createElement('form');
    form.innerHTML = '<input name="intent" value="tenant.create" /><input name="name" value="John" />';

    const payload = buildOfflinePayload(form);
    expect(payload.id).toBeDefined();
    expect(payload.name).toBe('John');
  });

  it('should not override existing id', () => {
    const form = document.createElement('form');
    form.innerHTML = '<input name="intent" value="tenant.create" /><input name="id" value="existing-id" />';

    const payload = buildOfflinePayload(form);
    expect(payload.id).toBe('existing-id');
  });

  it('should not add id for non-create intents', () => {
    const form = document.createElement('form');
    form.innerHTML = '<input name="intent" value="tenant.delete" />';

    const payload = buildOfflinePayload(form);
    expect(payload.id).toBeUndefined();
  });
});
