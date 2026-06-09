import { Form, Link, useFetcher, useLoaderData, useRevalidator } from '@remix-run/react';
import { json } from '@remix-run/node';
import type { ActionFunctionArgs, LoaderFunctionArgs, MetaFunction } from '@remix-run/node';
import React, { useEffect, useMemo, useRef, useState } from 'react';
import { execute, query, queryOne } from '../utils/db.server';
import { createId } from '../utils/id';
import { requireUserId } from '../utils/session.server';
import {
  Brand, MenuGlyph, DataButtons, ThemeToggle, DeleteRowButton,
  SyncBanner, PendingBadge, Toast, Modal, FilterBar,
  DatePickerInput, DateTimePickerInput, MoneyInput, CpfInput, PhoneInput,
  CustomSelect, SortHeader, SmallSelect, Pagination, LoadingRow
} from '../components/ui';
import {
  IconTenants, IconProperties, IconLeases, IconReceipts,
  IconLogout, IconSettings, IconTheme, IconExport, IconImport
} from '../components/icons';

import type {
  Tenant, Property, Lease, Receipt,
  MutationResponse, SortState, Tab, QueuePayload, PendingIds
} from '../types/dashboard';
import { tabs } from '../types/dashboard';
import {
  getDaysInMonth, getReceiptStatus, getReceiptStatusLabel,
  formatCurrency, formatDateBr, formatDateTimeBr,
  compareValues, sortItems, paginateItems
} from '../utils/format';
import {
  readDashboardCache, writeDashboardCache, readOfflineQueue, writeOfflineQueue,
  enqueueOfflineMutation, serializeFormData, submitQueuedMutation,
  getPendingIds, getSyncErrorMessage, isCreateIntent
} from '../utils/offline';

function getTab(url: URL): Tab {
  const tab = url.searchParams.get('tab');
  return tabs.includes(tab as Tab) ? (tab as Tab) : 'tenants';
}

export const meta: MetaFunction = () => [{ title: 'Aluxen | Painel' }];

export async function loader({ request }: LoaderFunctionArgs) {
  const userId = await requireUserId(request);
  const url = new URL(request.url);
  const tab = getTab(url);

  const [user, tenants, properties, leases, receipts, activeTenantIds] = await Promise.all([
    queryOne<{ username: string }>('SELECT username FROM users WHERE id = $1', [userId]),
    query<Tenant>('SELECT id, name, email, phone, cpf FROM tenants ORDER BY created_at DESC, name'),
    query<Property>('SELECT id, name, address, city, state, zip FROM properties ORDER BY created_at DESC, name'),
    query<Lease>(`
      SELECT l.id, l.tenant_id, l.property_id, l.start_date, l.end_date, l.due_day, l.rent_amount, l.status,
             t.name AS tenant_name, p.name AS property_name
      FROM leases l
      JOIN tenants t ON t.id = l.tenant_id
      JOIN properties p ON p.id = l.property_id
      ORDER BY l.status, l.created_at DESC
    `),
    query<Receipt>(`
      SELECT r.id, r.lease_id, r.due_date, r.paid_at, r.amount, r.pdf_path, r.generated_key,
             t.name AS tenant_name, p.name AS property_name
      FROM receipts r
      JOIN leases l ON l.id = r.lease_id
      JOIN tenants t ON t.id = l.tenant_id
      JOIN properties p ON p.id = l.property_id
      ORDER BY r.created_at DESC
    `),
    query<{ tenant_id: string }>('SELECT DISTINCT tenant_id FROM leases WHERE status = \'active\''),
  ]);

  return json({ tab, user: user?.username || 'admin', tenants, properties, leases, receipts, activeTenantIds: activeTenantIds.map((r) => r.tenant_id) });
}

export async function action({ request }: ActionFunctionArgs) {
  await requireUserId(request);
  const formData = await request.formData();
  const intent = String(formData.get('intent') || '');

  try {
    switch (intent) {
      case 'tenant.create': {
        const id = String(formData.get('id') || createId());
        const name = String(formData.get('name') || '').trim();
        const email = String(formData.get('email') || '').trim();
        const phone = String(formData.get('phone') || '').trim();
        const cpf = String(formData.get('cpf') || '').trim();
        if (!name || !email) throw new Error('Nome e email são obrigatórios.');
        await execute('INSERT INTO tenants (id, name, email, phone, cpf) VALUES ($1, $2, $3, $4, $5)', [id, name, email, phone || null, cpf || null]);
        break;
      }
      case 'tenant.update': {
        const id = String(formData.get('id') || '');
        const name = String(formData.get('name') || '').trim();
        const email = String(formData.get('email') || '').trim();
        const phone = String(formData.get('phone') || '').trim();
        const cpf = String(formData.get('cpf') || '').trim();
        if (!id || !name || !email) throw new Error('Nome e email são obrigatórios.');
        await execute("UPDATE tenants SET name = $1, email = $2, phone = $3, cpf = $4, updated_at = datetime('now') WHERE id = $5", [name, email, phone || null, cpf || null, id]);
        break;
      }
      case 'tenant.delete': {
        const tenantId = String(formData.get('id'));
        const activeLease = await queryOne<{ id: string }>('SELECT id FROM leases WHERE tenant_id = $1 AND status = \'active\'', [tenantId]);
        if (activeLease) throw new Error('Não é possível excluir inquilino com contrato ativo. Encerre o contrato primeiro.');
        await execute('DELETE FROM tenants WHERE id = $1', [tenantId]);
        break;
      }
      case 'property.create': {
        const id = String(formData.get('id') || createId());
        const name = String(formData.get('name') || '').trim();
        const address = String(formData.get('address') || '').trim();
        const city = String(formData.get('city') || '').trim();
        const state = String(formData.get('state') || '').trim();
        const zip = String(formData.get('zip') || '').trim();
        if (!name || !address || !city || !state) throw new Error('Preencha nome, endereço, cidade e estado.');
        await execute('INSERT INTO properties (id, name, address, city, state, zip) VALUES ($1, $2, $3, $4, $5, $6)', [id, name, address, city, state, zip || null]);
        break;
      }
      case 'property.update': {
        const id = String(formData.get('id') || '');
        const name = String(formData.get('name') || '').trim();
        const address = String(formData.get('address') || '').trim();
        const city = String(formData.get('city') || '').trim();
        const state = String(formData.get('state') || '').trim();
        const zip = String(formData.get('zip') || '').trim();
        if (!id || !name || !address || !city || !state) throw new Error('Preencha nome, endereço, cidade e estado.');
        await execute("UPDATE properties SET name = $1, address = $2, city = $3, state = $4, zip = $5, updated_at = datetime('now') WHERE id = $6", [name, address, city, state, zip || null, id]);
        break;
      }
      case 'property.delete': {
        await execute('DELETE FROM properties WHERE id = $1', [String(formData.get('id'))]);
        break;
      }
      case 'lease.create': {
        const id = String(formData.get('id') || createId());
        const tenantId = String(formData.get('tenant_id') || '');
        const propertyId = String(formData.get('property_id') || '');
        const startDate = String(formData.get('start_date') || '');
        const endDate = String(formData.get('end_date') || '');
        const dueDay = Number(formData.get('due_day'));
        const rentAmount = Number(formData.get('rent_amount'));
        if (!tenantId || !propertyId || !startDate || !endDate || !Number.isInteger(dueDay) || dueDay < 1 || dueDay > 31 || !Number.isFinite(rentAmount)) throw new Error('Preencha todos os campos do contrato.');
        await execute('INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount) VALUES ($1, $2, $3, $4, $5, $6, $7)', [id, tenantId, propertyId, startDate, endDate, dueDay, rentAmount]);
        break;
      }
      case 'lease.update': {
        const id = String(formData.get('id') || '');
        const tenantId = String(formData.get('tenant_id') || '');
        const propertyId = String(formData.get('property_id') || '');
        const startDate = String(formData.get('start_date') || '');
        const endDate = String(formData.get('end_date') || '');
        const dueDay = Number(formData.get('due_day'));
        const rentAmount = Number(formData.get('rent_amount'));
        if (!id || !tenantId || !propertyId || !startDate || !endDate || !Number.isInteger(dueDay) || dueDay < 1 || dueDay > 31 || !Number.isFinite(rentAmount)) throw new Error('Preencha todos os campos do contrato.');
        await execute("UPDATE leases SET tenant_id = $1, property_id = $2, start_date = $3, end_date = $4, due_day = $5, rent_amount = $6, updated_at = datetime('now') WHERE id = $7", [tenantId, propertyId, startDate, endDate, dueDay, rentAmount, id]);
        break;
      }
      case 'lease.terminate': {
        await execute("UPDATE leases SET status = 'terminated', updated_at = datetime('now') WHERE id = $1", [String(formData.get('id'))]);
        break;
      }
      case 'lease.reactivate': {
        await execute("UPDATE leases SET status = 'active', updated_at = datetime('now') WHERE id = $1", [String(formData.get('id'))]);
        break;
      }
      case 'lease.delete': {
        await execute('DELETE FROM leases WHERE id = $1', [String(formData.get('id'))]);
        break;
      }
      case 'receipt.create':
      case 'receipt.update': {
        const id = String(formData.get('id') || createId());
        const leaseId = String(formData.get('lease_id') || '');
        const dueDate = String(formData.get('due_date') || '');
        const paidAt = String(formData.get('paid_at') || '');
        const amount = Number(formData.get('amount'));
        if (!leaseId || !dueDate || !Number.isFinite(amount)) throw new Error('Preencha contrato, vencimento e valor.');

        const lease = await queryOne<{ tenant_name: string; property_name: string }>(`
          SELECT t.name AS tenant_name, p.name AS property_name
          FROM leases l
          JOIN tenants t ON t.id = l.tenant_id
          JOIN properties p ON p.id = l.property_id
          WHERE l.id = $1
        `, [leaseId]);
        if (!lease) throw new Error('Contrato não encontrado.');

        const pdfPath = null;

        if (intent === 'receipt.create') {
          await execute('INSERT INTO receipts (id, lease_id, due_date, paid_at, amount, pdf_path) VALUES ($1, $2, $3, $4, $5, $6)', [id, leaseId, dueDate, paidAt || null, amount, pdfPath]);
        } else {
          await execute('UPDATE receipts SET lease_id = $1, due_date = $2, paid_at = $3, amount = $4, pdf_path = $5 WHERE id = $6', [leaseId, dueDate, paidAt || null, amount, pdfPath, id]);
        }
        break;
      }
      case 'receipt.generate-monthly': {
        const now = new Date();
        const year = now.getFullYear();
        const month = now.getMonth() + 1;
        const monthKey = `${year}-${String(month).padStart(2, '0')}`;
        const monthStart = `${monthKey}-01`;
        const monthEnd = `${monthKey}-${String(getDaysInMonth(year, month)).padStart(2, '0')}`;
        const activeLeases = await query<Lease>(`
          SELECT id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, '' AS tenant_name, '' AS property_name, status
          FROM leases
          WHERE status = 'active' AND start_date <= $1 AND end_date >= $2
        `, [monthEnd, monthStart]);

        for (const lease of activeLeases) {
          const dueDay = Math.min(Number(lease.due_day) || 1, getDaysInMonth(year, month));
          const dueDate = `${monthKey}-${String(dueDay).padStart(2, '0')}`;
          await execute(
            'INSERT OR IGNORE INTO receipts (id, lease_id, due_date, paid_at, amount, pdf_path, generated_key) VALUES ($1, $2, $3, NULL, $4, NULL, $5)',
            [createId(), lease.id, dueDate, lease.rent_amount, `${lease.id}:${monthKey}`],
          );
        }
        break;
      }
      case 'receipt.delete': {
        await execute('DELETE FROM receipts WHERE id = $1', [String(formData.get('id'))]);
        break;
      }
      case 'receipt.batch-pay': {
        const ids = (formData.get('ids') || '').toString().split(',').filter(Boolean);
        if (!ids.length) throw new Error('Nenhum recibo selecionado.');
        const now = new Date();
        const paidAt = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}T${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`;
        for (const receiptId of ids) {
          await execute("UPDATE receipts SET paid_at = $1, updated_at = datetime('now') WHERE id = $2 AND paid_at IS NULL", [paidAt, receiptId]);
        }
        break;
      }
      default:
        throw new Error('Ação inválida.');
    }
  } catch (error) {
    return json<MutationResponse>({ error: error instanceof Error ? error.message : 'Não foi possível salvar.' }, { status: 400 });
  }

  return json<MutationResponse>({ ok: true });
}

export default function AppRoute() {
  const { tab, user, tenants, properties, leases, receipts, activeTenantIds } = useLoaderData<typeof loader>();
  const revalidator = useRevalidator();
  const [collapsed, setCollapsed] = useState(() => {
    if (typeof window === 'undefined') return false;
    return window.localStorage.getItem('sidebar') === 'collapsed';
  });

  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);

  useEffect(() => {
    window.localStorage.setItem('sidebar', collapsed ? 'collapsed' : 'expanded');
  }, [collapsed]);

  useEffect(() => {
    setMobileMenuOpen(false);
  }, [tab]);

  useEffect(() => {
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMobileMenuOpen(false);
    };
    document.addEventListener('keydown', handleEsc);
    return () => document.removeEventListener('keydown', handleEsc);
  }, []);

  const [tenantItems, setTenantItems] = useState(tenants);
  const [propertyItems, setPropertyItems] = useState(properties);
  const [leaseItems, setLeaseItems] = useState(leases);
  const [receiptItems, setReceiptItems] = useState(receipts);

  const [tenantForm, setTenantForm] = useState({ name: '', email: '', phone: '', cpf: '' });
  const [propertyForm, setPropertyForm] = useState({ name: '', address: '', city: '', state: '', zip: '' });
  const [leaseForm, setLeaseForm] = useState({ tenant_id: '', property_id: '', start_date: '', end_date: '', due_day: '1', rent_amount: '' });
  const [receiptForm, setReceiptForm] = useState({ lease_id: '', due_date: '', paid_at: '', amount: '' });
  const [tenantDraft, setTenantDraft] = useState({ id: '', name: '', email: '', phone: '', cpf: '' });
  const [propertyDraft, setPropertyDraft] = useState({ id: '', name: '', address: '', city: '', state: '', zip: '' });
  const [leaseDraft, setLeaseDraft] = useState({ id: '', tenant_id: '', property_id: '', start_date: '', end_date: '', due_day: '1', rent_amount: '' });
  const [receiptDraft, setReceiptDraft] = useState({ id: '', lease_id: '', due_date: '', paid_at: '', amount: '' });
  const [modalType, setModalType] = useState<'' | Tab>('');
  const [createModalType, setCreateModalType] = useState<'' | Tab>('');
  const [toast, setToast] = useState<{ message: string; tone: 'success' | 'error' } | null>(null);
  const [tenantSearch, setTenantSearch] = useState('');
  const [propertySearch, setPropertySearch] = useState('');
  const [leaseSearch, setLeaseSearch] = useState('');
  const [leaseStatus, setLeaseStatus] = useState<'all' | 'active' | 'terminated'>('all');
  const [receiptSearch, setReceiptSearch] = useState('');
  const [receiptStatus, setReceiptStatus] = useState<'all' | 'due' | 'overdue' | 'paid'>('all');
  const [receiptMonth, setReceiptMonth] = useState(() => String(new Date().getMonth() + 1).padStart(2, '0'));
  const [receiptYear, setReceiptYear] = useState(() => String(new Date().getFullYear()));
  const [selectedReceipts, setSelectedReceipts] = useState<Set<string>>(new Set());
  const [tenantSort, setTenantSort] = useState<SortState>({ key: 'name', direction: 'asc' });
  const [propertySort, setPropertySort] = useState<SortState>({ key: 'name', direction: 'asc' });
  const [leaseSort, setLeaseSort] = useState<SortState>({ key: 'tenant_name', direction: 'asc' });
  const [receiptSort, setReceiptSort] = useState<SortState>({ key: 'due_date', direction: 'desc' });
  const [tenantPage, setTenantPage] = useState(1);
  const [propertyPage, setPropertyPage] = useState(1);
  const [leasePage, setLeasePage] = useState(1);
  const [receiptPage, setReceiptPage] = useState(1);
  const [isOnline, setIsOnline] = useState(() => (typeof navigator === 'undefined' ? true : navigator.onLine));
  const [pendingSyncCount, setPendingSyncCount] = useState(0);
  const [pendingIds, setPendingIds] = useState<PendingIds>({ tenants: new Set(), properties: new Set(), leases: new Set(), receipts: new Set() });
  const [initialLoading, setInitialLoading] = useState(true);
  const activeTenantSet = useMemo(() => new Set(activeTenantIds), [activeTenantIds]);

  useEffect(() => {
    if (tenants.length || properties.length || leases.length || receipts.length) {
      const timer = setTimeout(() => setInitialLoading(false), 300);
      return () => clearTimeout(timer);
    }
  }, [tenants, properties, leases, receipts]);

  const tenantFetcher = useFetcher<MutationResponse>();
  const propertyFetcher = useFetcher<MutationResponse>();
  const leaseFetcher = useFetcher<MutationResponse>();
  const receiptFetcher = useFetcher<MutationResponse>();
  const lastHandledTenantResponse = useRef<MutationResponse | undefined>(undefined);
  const lastHandledPropertyResponse = useRef<MutationResponse | undefined>(undefined);
  const lastHandledLeaseResponse = useRef<MutationResponse | undefined>(undefined);
  const lastHandledReceiptResponse = useRef<MutationResponse | undefined>(undefined);
  const syncInFlight = useRef(false);

  function queueOfflineMutation(payload: QueuePayload, successMessage: string) {
    const mutationPayload = { ...payload };
    if (isCreateIntent(mutationPayload.intent) && !mutationPayload.id) {
      mutationPayload.id = createId();
    }
    const queue = enqueueOfflineMutation({ id: createId(), payload: mutationPayload, successMessage });
    setPendingSyncCount(queue.length);
    setPendingIds(getPendingIds(queue));
    setToast({ message: successMessage, tone: 'success' });
  }

  async function syncOfflineQueue() {
    if (syncInFlight.current || !window.navigator.onLine) {
      return;
    }

    const queue = readOfflineQueue();
    if (!queue.length) {
      setPendingSyncCount(0);
      setPendingIds(getPendingIds([]));
      return;
    }

    syncInFlight.current = true;

    try {
      let remaining = [...queue];
      for (const mutation of queue) {
        await submitQueuedMutation(mutation.payload);
        remaining = remaining.filter((item) => item.id !== mutation.id);
        writeOfflineQueue(remaining);
        setPendingSyncCount(remaining.length);
        setPendingIds(getPendingIds(remaining));
      }
      revalidator.revalidate();
      setToast({ message: 'Sincronização concluída com o servidor.', tone: 'success' });
    } catch (error) {
      const queueHead = readOfflineQueue()[0];
      setToast({ message: getSyncErrorMessage(queueHead?.payload || {}, error), tone: 'error' });
    } finally {
      syncInFlight.current = false;
    }
  }

  useEffect(() => setTenantItems(tenants), [tenants]);
  useEffect(() => setPropertyItems(properties), [properties]);
  useEffect(() => setLeaseItems(leases), [leases]);
  useEffect(() => setReceiptItems(receipts), [receipts]);

  useEffect(() => {
    const cache = readDashboardCache();
    const queue = readOfflineQueue();
    setPendingSyncCount(queue.length);
    setPendingIds(getPendingIds(queue));
    if (!window.navigator.onLine && cache) {
      setTenantItems(cache.tenants);
      setPropertyItems(cache.properties);
      setLeaseItems(cache.leases);
      setReceiptItems(cache.receipts);
    }

    const handleOnline = () => {
      setIsOnline(true);
      void syncOfflineQueue();
    };
    const handleOffline = () => setIsOnline(false);

    window.addEventListener('online', handleOnline);
    window.addEventListener('offline', handleOffline);

    if (window.navigator.onLine) {
      void syncOfflineQueue();
    }

    return () => {
      window.removeEventListener('online', handleOnline);
      window.removeEventListener('offline', handleOffline);
    };
  }, []);

  useEffect(() => {
    writeDashboardCache({
      user,
      tenants: tenantItems,
      properties: propertyItems,
      leases: leaseItems,
      receipts: receiptItems,
      savedAt: new Date().toISOString(),
    });
  }, [user, tenantItems, propertyItems, leaseItems, receiptItems]);

  useEffect(() => {
    if (tenantFetcher.state !== 'idle' || !tenantFetcher.data || lastHandledTenantResponse.current === tenantFetcher.data) {
      return;
    }

    lastHandledTenantResponse.current = tenantFetcher.data;
    revalidator.revalidate();
    if (tenantFetcher.data.ok) {
      setTenantForm({ name: '', email: '', phone: '', cpf: '' });
      setTenantDraft({ id: '', name: '', email: '', phone: '', cpf: '' });
      setModalType('');
      setCreateModalType('');
      setToast({ message: 'Inquilino salvo com sucesso.', tone: 'success' });
    } else if (tenantFetcher.data.error) {
      setToast({ message: tenantFetcher.data.error, tone: 'error' });
    }
  }, [tenantFetcher.state, tenantFetcher.data, revalidator]);

  useEffect(() => {
    if (propertyFetcher.state !== 'idle' || !propertyFetcher.data || lastHandledPropertyResponse.current === propertyFetcher.data) {
      return;
    }

    lastHandledPropertyResponse.current = propertyFetcher.data;
    revalidator.revalidate();
    if (propertyFetcher.data.ok) {
      setPropertyForm({ name: '', address: '', city: '', state: '', zip: '' });
      setPropertyDraft({ id: '', name: '', address: '', city: '', state: '', zip: '' });
      setModalType('');
      setCreateModalType('');
      setToast({ message: 'Imóvel salvo com sucesso.', tone: 'success' });
    } else if (propertyFetcher.data.error) {
      setToast({ message: propertyFetcher.data.error, tone: 'error' });
    }
  }, [propertyFetcher.state, propertyFetcher.data, revalidator]);

  useEffect(() => {
    if (leaseFetcher.state !== 'idle' || !leaseFetcher.data || lastHandledLeaseResponse.current === leaseFetcher.data) {
      return;
    }

    lastHandledLeaseResponse.current = leaseFetcher.data;
    revalidator.revalidate();
    if (leaseFetcher.data.ok) {
      setLeaseForm({ tenant_id: '', property_id: '', start_date: '', end_date: '', due_day: '1', rent_amount: '' });
      setLeaseDraft({ id: '', tenant_id: '', property_id: '', start_date: '', end_date: '', due_day: '1', rent_amount: '' });
      setModalType('');
      setCreateModalType('');
      setToast({ message: 'Contrato salvo com sucesso.', tone: 'success' });
    } else if (leaseFetcher.data.error) {
      setToast({ message: leaseFetcher.data.error, tone: 'error' });
    }
  }, [leaseFetcher.state, leaseFetcher.data, revalidator]);

  useEffect(() => {
    if (receiptFetcher.state !== 'idle' || !receiptFetcher.data || lastHandledReceiptResponse.current === receiptFetcher.data) {
      return;
    }

    lastHandledReceiptResponse.current = receiptFetcher.data;
    revalidator.revalidate();
    if (receiptFetcher.data.ok) {
      setReceiptForm({ lease_id: '', due_date: '', paid_at: '', amount: '' });
      setReceiptDraft({ id: '', lease_id: '', due_date: '', paid_at: '', amount: '' });
      setModalType('');
      setCreateModalType('');
      setToast({ message: 'Pagamentos atualizados com sucesso.', tone: 'success' });
    } else if (receiptFetcher.data.error) {
      setToast({ message: receiptFetcher.data.error, tone: 'error' });
    }
  }, [receiptFetcher.state, receiptFetcher.data, revalidator]);

  const filteredTenants = useMemo(() => tenantItems.filter((item) => `${item.name} ${item.email} ${item.phone || ''}`.toLowerCase().includes(tenantSearch.toLowerCase())), [tenantItems, tenantSearch]);
  const filteredProperties = useMemo(() => propertyItems.filter((item) => `${item.name} ${item.city} ${item.state} ${item.address}`.toLowerCase().includes(propertySearch.toLowerCase())), [propertyItems, propertySearch]);
  const filteredLeases = useMemo(() => leaseItems.filter((item) => {
    if (leaseStatus === 'active' && item.status !== 'active') return false;
    if (leaseStatus === 'terminated' && item.status !== 'terminated') return false;
    return `${item.tenant_name} ${item.property_name} ${item.start_date} ${item.end_date}`.toLowerCase().includes(leaseSearch.toLowerCase());
  }), [leaseItems, leaseSearch, leaseStatus]);
  const filteredReceipts = useMemo(() => receiptItems.filter((item) => {
    const matchesText = `${item.tenant_name} ${item.property_name} ${item.due_date} ${item.amount}`.toLowerCase().includes(receiptSearch.toLowerCase());
    const matchesStatus = receiptStatus === 'all' || getReceiptStatus(item) === receiptStatus;
    const itemMonth = item.due_date.slice(5, 7);
    const itemYear = item.due_date.slice(0, 4);
    const matchesPeriod = !receiptMonth || !receiptYear || (itemMonth === receiptMonth && itemYear === receiptYear);
    return matchesText && matchesStatus && matchesPeriod;
  }), [receiptItems, receiptSearch, receiptStatus, receiptMonth, receiptYear]);
  const sortedTenants = useMemo(() => sortItems(filteredTenants, tenantSort), [filteredTenants, tenantSort]);
  const sortedProperties = useMemo(() => sortItems(filteredProperties, propertySort), [filteredProperties, propertySort]);
  const sortedLeases = useMemo(() => sortItems(filteredLeases, leaseSort), [filteredLeases, leaseSort]);
  const sortedReceipts = useMemo(() => sortItems(filteredReceipts, receiptSort), [filteredReceipts, receiptSort]);
  const tenantTotalPages = Math.max(1, Math.ceil(sortedTenants.length / 8));
  const propertyTotalPages = Math.max(1, Math.ceil(sortedProperties.length / 8));
  const leaseTotalPages = Math.max(1, Math.ceil(sortedLeases.length / 8));
  const receiptTotalPages = Math.max(1, Math.ceil(sortedReceipts.length / 8));
  const pagedTenants = useMemo(() => paginateItems(sortedTenants, tenantPage, 8), [sortedTenants, tenantPage]);
  const pagedProperties = useMemo(() => paginateItems(sortedProperties, propertyPage, 8), [sortedProperties, propertyPage]);
  const pagedLeases = useMemo(() => paginateItems(sortedLeases, leasePage, 8), [sortedLeases, leasePage]);
  const pagedReceipts = useMemo(() => paginateItems(sortedReceipts, receiptPage, 8), [sortedReceipts, receiptPage]);

  useEffect(() => setTenantPage(1), [tenantSearch, tenantSort]);
  useEffect(() => setPropertyPage(1), [propertySearch, propertySort]);
  useEffect(() => setLeasePage(1), [leaseSearch, leaseSort, leaseStatus]);
  useEffect(() => setReceiptPage(1), [receiptSearch, receiptStatus, receiptSort, receiptMonth, receiptYear]);
  useEffect(() => setTenantPage((page) => Math.min(page, tenantTotalPages)), [tenantTotalPages]);
  useEffect(() => setPropertyPage((page) => Math.min(page, propertyTotalPages)), [propertyTotalPages]);
  useEffect(() => setLeasePage((page) => Math.min(page, leaseTotalPages)), [leaseTotalPages]);
  useEffect(() => setReceiptPage((page) => Math.min(page, receiptTotalPages)), [receiptTotalPages]);

  function settleAfterMutation() {
    if (window.navigator.onLine) {
      revalidator.revalidate();
    }
  }

  function buildOfflinePayload(form: HTMLFormElement) {
    const payload = serializeFormData(new FormData(form));
    if (isCreateIntent(payload.intent) && !payload.id) {
      payload.id = createId();
    }
    return payload;
  }

  function handleOfflineSubmit(
    event: React.FormEvent<HTMLFormElement>,
    optimisticUpdate: (payload: QueuePayload) => void,
    successMessage: string,
    reset?: () => void,
  ) {
    if (window.navigator.onLine) {
      return false;
    }

    event.preventDefault();
    const form = event.currentTarget;
    if (!form.reportValidity()) {
      return true;
    }

    const payload = buildOfflinePayload(form);
    optimisticUpdate(payload);
    queueOfflineMutation(payload, successMessage);
    reset?.();
    return true;
  }

  function applyOfflineMonthlyReceiptGeneration() {
    const now = new Date();
    const year = now.getFullYear();
    const month = now.getMonth() + 1;
    const monthKey = `${year}-${String(month).padStart(2, '0')}`;
    const monthStart = `${monthKey}-01`;
    const monthEnd = `${monthKey}-${String(getDaysInMonth(year, month)).padStart(2, '0')}`;

    const activeLeases = leaseItems.filter((lease) => lease.status === 'active' && lease.start_date <= monthEnd && lease.end_date >= monthStart);
    const nextReceipts = [...receiptItems];

    for (const lease of activeLeases) {
      const generatedKey = `${lease.id}:${monthKey}`;
      if (nextReceipts.some((item) => item.generated_key === generatedKey)) {
        continue;
      }
      const dueDay = Math.min(Number(lease.due_day) || 1, getDaysInMonth(year, month));
      nextReceipts.unshift({
        id: createId(),
        lease_id: lease.id,
        due_date: `${monthKey}-${String(dueDay).padStart(2, '0')}`,
        paid_at: null,
        amount: lease.rent_amount,
        pdf_path: null,
        generated_key: generatedKey,
        tenant_name: lease.tenant_name,
        property_name: lease.property_name,
      });
    }

    setReceiptItems(nextReceipts);
  }

  function startTenantEdit(item: Tenant) {
    setTenantDraft({ id: item.id, name: item.name, email: item.email, phone: item.phone || '', cpf: item.cpf || '' });
    setModalType('tenants');
  }

  function startPropertyEdit(item: Property) {
    setPropertyDraft({ id: item.id, name: item.name, address: item.address, city: item.city, state: item.state, zip: item.zip || '' });
    setModalType('properties');
  }

  function startLeaseEdit(item: Lease) {
    setLeaseDraft({ id: item.id, tenant_id: item.tenant_id, property_id: item.property_id, start_date: item.start_date, end_date: item.end_date, due_day: String(item.due_day || '1'), rent_amount: item.rent_amount });
    setModalType('leases');
  }

  function startReceiptEdit(item: Receipt) {
    setReceiptDraft({ id: item.id, lease_id: item.lease_id, due_date: item.due_date, paid_at: item.paid_at ? item.paid_at.slice(0, 16) : '', amount: item.amount });
    setModalType('receipts');
  }

  function receiptLeaseMeta(leaseId: string) {
    return leaseItems.find((item) => item.id === leaseId);
  }

  function toggleSort(current: SortState, key: string, setter: (sort: SortState) => void) {
    setter({ key, direction: current.key === key && current.direction === 'asc' ? 'desc' : 'asc' });
  }

  return (
    <div className="app-shell">
      <button className="hamburger-float" type="button" onClick={() => setMobileMenuOpen(v => !v)} aria-label={mobileMenuOpen ? 'Fechar menu' : 'Abrir menu'}>
        {mobileMenuOpen ? (
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round"><path d="M18 6L6 18M6 6l12 12" /></svg>
        ) : (
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round"><path d="M3 12h18M3 6h18M3 18h18" /></svg>
        )}
      </button>
      {mobileMenuOpen && <div className="sidebar-backdrop" onClick={() => setMobileMenuOpen(false)} />}

      <nav className={`sidebar${(!collapsed || mobileMenuOpen) ? ' open' : ''}${mobileMenuOpen ? ' mobile-open' : ''}`}>
        <Brand collapsed={collapsed && !mobileMenuOpen} />
        <button className="menu-btn" type="button" onClick={() => setCollapsed((value) => !value)} aria-label={collapsed ? 'Expandir menu' : 'Recolher menu'}>
          <MenuGlyph collapsed={collapsed} />
        </button>
<ul>
            <li><Link to="/dashboard?tab=tenants" onClick={() => setMobileMenuOpen(false)}><span className="nav-icon"><IconTenants /></span>{(!collapsed || mobileMenuOpen) ? <span className="nav-label">Inquilinos</span> : null}</Link></li>
            <li><Link to="/dashboard?tab=properties" onClick={() => setMobileMenuOpen(false)}><span className="nav-icon"><IconProperties /></span>{(!collapsed || mobileMenuOpen) ? <span className="nav-label">Imóveis</span> : null}</Link></li>
            <li><Link to="/dashboard?tab=leases" onClick={() => setMobileMenuOpen(false)}><span className="nav-icon"><IconLeases /></span>{(!collapsed || mobileMenuOpen) ? <span className="nav-label">Contratos</span> : null}</Link></li>
            <li><Link to="/dashboard?tab=receipts" onClick={() => setMobileMenuOpen(false)}><span className="nav-icon"><IconReceipts /></span>{(!collapsed || mobileMenuOpen) ? <span className="nav-label">Pagamentos</span> : null}</Link></li>
          </ul>
          <div className="sidebar-footer">
            <DataButtons collapsed={collapsed && !mobileMenuOpen} />
            <ThemeToggle collapsed={collapsed && !mobileMenuOpen} />
            <a href="/settings" className="nav-link"><span className="nav-icon"><IconSettings /></span>{(!collapsed || mobileMenuOpen) ? <span className="nav-label">Configurações</span> : null}</a>
            <Form action="/logout" method="post">
              <button type="submit"><span className="nav-icon"><IconLogout /></span>{(!collapsed || mobileMenuOpen) ? <span className="nav-label">Sair</span> : null}</button>
            </Form>
          </div>
      </nav>

        <main className={collapsed ? '' : 'sidebar-open'}>
          <div className="page">
            <div className="card page-hero">
              <div>
                <p className="eyebrow">Painel</p>
                <h1>{tab === 'tenants' ? 'Inquilinos' : tab === 'properties' ? 'Imóveis' : tab === 'leases' ? 'Contratos' : 'Pagamentos e Recibos'}</h1>
                <p className="subtitle">Sessão ativa para <strong>{user}</strong>.</p>
              </div>
            </div>
            <SyncBanner isOnline={isOnline} pendingCount={pendingSyncCount} />
            {toast ? <Toast message={toast.message} tone={toast.tone} onClose={() => setToast(null)} /> : null}

            {tab === 'tenants' ? (
              <section className="card stack-lg">
                <div className="section-header"><h2>Gerenciar Inquilinos</h2><button type="button" className="btn btn-primary" onClick={() => setCreateModalType('tenants')}>Novo inquilino</button></div>
                <FilterBar value={tenantSearch} onChange={setTenantSearch} placeholder="Buscar por nome, email ou telefone" />
                <div className="table-wrap">
                  <table>
                    <thead><tr><th><SortHeader label="Nome" active={tenantSort.key === 'name'} direction={tenantSort.direction} onClick={() => toggleSort(tenantSort, 'name', setTenantSort)} /></th><th><SortHeader label="Email" active={tenantSort.key === 'email'} direction={tenantSort.direction} onClick={() => toggleSort(tenantSort, 'email', setTenantSort)} /></th><th><SortHeader label="CPF" active={tenantSort.key === 'cpf'} direction={tenantSort.direction} onClick={() => toggleSort(tenantSort, 'cpf', setTenantSort)} /></th><th><SortHeader label="Telefone" active={tenantSort.key === 'phone'} direction={tenantSort.direction} onClick={() => toggleSort(tenantSort, 'phone', setTenantSort)} /></th><th>Ações</th></tr></thead>
                    <tbody>
                      {initialLoading ? <LoadingRow colSpan={5} /> : (pagedTenants.length ? pagedTenants.map((tenant) => (
                        <tr key={tenant.id}>
                          <td data-label="Nome">{tenant.name} {pendingIds.tenants.has(tenant.id) ? <PendingBadge /> : null} {activeTenantSet.has(tenant.id) ? <span className="status-badge paid" style={{ fontSize: '0.7rem', marginLeft: '0.35rem' }}>Em contrato</span> : null}</td>
                          <td data-label="Email">{tenant.email}</td>
                          <td data-label="CPF">{tenant.cpf || '-'}</td>
                          <td data-label="Telefone">{tenant.phone || '-'}</td>
                          <td className="row-actions">
                            <button type="button" className="btn btn-sm btn-secondary" onClick={() => startTenantEdit(tenant)}>Editar</button>
                            <DeleteRowButton intent="tenant.delete" tab="tenants" id={tenant.id} onOptimisticDelete={() => setTenantItems((current) => current.filter((row) => row.id !== tenant.id))} onSettled={settleAfterMutation} queueOfflineMutation={queueOfflineMutation} disabled={activeTenantSet.has(tenant.id)} disabledTitle="Possui contrato ativo" />
                          </td>
                        </tr>
                      )) : <tr><td colSpan={5}>Nenhum inquilino cadastrado.</td></tr>)}
                    </tbody>
                  </table>
                </div>
                <Pagination page={tenantPage} totalPages={tenantTotalPages} onChange={setTenantPage} />
              </section>
            ) : null}

            {tab === 'properties' ? (
              <section className="card stack-lg">
                <div className="section-header"><h2>Gerenciar Imóveis</h2><button type="button" className="btn btn-primary" onClick={() => setCreateModalType('properties')}>Novo imóvel</button></div>
                <FilterBar value={propertySearch} onChange={setPropertySearch} placeholder="Buscar por nome, cidade, estado ou endereço" />
                <div className="table-wrap">
                  <table>
                    <thead><tr><th><SortHeader label="Nome" active={propertySort.key === 'name'} direction={propertySort.direction} onClick={() => toggleSort(propertySort, 'name', setPropertySort)} /></th><th><SortHeader label="Cidade" active={propertySort.key === 'city'} direction={propertySort.direction} onClick={() => toggleSort(propertySort, 'city', setPropertySort)} /></th><th><SortHeader label="Estado" active={propertySort.key === 'state'} direction={propertySort.direction} onClick={() => toggleSort(propertySort, 'state', setPropertySort)} /></th><th>Ações</th></tr></thead>
                    <tbody>
                      {pagedProperties.length ? pagedProperties.map((property) => (
                        <tr key={property.id}>
                          <td data-label="Nome">{property.name} {pendingIds.properties.has(property.id) ? <PendingBadge /> : null}</td>
                          <td data-label="Cidade">{property.city}</td>
                          <td data-label="Estado">{property.state}</td>
                          <td className="row-actions">
                            <button type="button" className="btn btn-sm btn-secondary" onClick={() => startPropertyEdit(property)}>Editar</button>
                            <DeleteRowButton intent="property.delete" tab="properties" id={property.id} onOptimisticDelete={() => setPropertyItems((current) => current.filter((row) => row.id !== property.id))} onSettled={settleAfterMutation} queueOfflineMutation={queueOfflineMutation} />
                          </td>
                        </tr>
                      )) : <tr><td colSpan={4}>Nenhum imóvel cadastrado.</td></tr>}
                    </tbody>
                  </table>
                </div>
                <Pagination page={propertyPage} totalPages={propertyTotalPages} onChange={setPropertyPage} />
              </section>
            ) : null}

            {tab === 'leases' ? (
              <section className="card stack-lg">
                <div className="section-header"><h2>Gerenciar Contratos</h2><button type="button" className="btn btn-primary" onClick={() => setCreateModalType('leases')}>Novo contrato</button></div>
                <FilterBar value={leaseSearch} onChange={setLeaseSearch} placeholder="Buscar por inquilino, imóvel ou período">
                  <SmallSelect value={leaseStatus} onChange={(v) => setLeaseStatus(v as 'all' | 'active' | 'terminated')}>
                    <option value="all">Todos</option>
                    <option value="active">Ativos</option>
                    <option value="terminated">Encerrados</option>
                  </SmallSelect>
                </FilterBar>
                <div className="table-wrap">
                  <table>
                    <thead><tr><th><SortHeader label="Inquilino" active={leaseSort.key === 'tenant_name'} direction={leaseSort.direction} onClick={() => toggleSort(leaseSort, 'tenant_name', setLeaseSort)} /></th><th><SortHeader label="Imóvel" active={leaseSort.key === 'property_name'} direction={leaseSort.direction} onClick={() => toggleSort(leaseSort, 'property_name', setLeaseSort)} /></th><th>Status</th><th><SortHeader label="Início" active={leaseSort.key === 'start_date'} direction={leaseSort.direction} onClick={() => toggleSort(leaseSort, 'start_date', setLeaseSort)} /></th><th><SortHeader label="Vencimento" active={leaseSort.key === 'due_day'} direction={leaseSort.direction} onClick={() => toggleSort(leaseSort, 'due_day', setLeaseSort)} /></th>                        <th><SortHeader label="Valor" active={leaseSort.key === 'rent_amount'} direction={leaseSort.direction} onClick={() => toggleSort(leaseSort, 'rent_amount', setLeaseSort)} /></th><th>PDF</th><th>Ações</th></tr></thead>
                    <tbody>
                      {pagedLeases.length ? pagedLeases.map((lease) => (
                        <tr key={lease.id} style={lease.status === 'terminated' ? { opacity: 0.55 } : undefined}>
                          <td data-label="Inquilino">{lease.tenant_name} {pendingIds.leases.has(lease.id) ? <PendingBadge /> : null}</td>
                          <td data-label="Imóvel">{lease.property_name}</td>
                          <td data-label="Status"><span className={`status-badge ${lease.status === 'active' ? 'paid' : 'overdue'}`}>{lease.status === 'active' ? 'Ativo' : 'Encerrado'}</span></td>
                          <td data-label="Período">{lease.start_date} a {lease.end_date}</td>
                          <td data-label="Vencimento">Dia {lease.due_day}</td>
                           <td data-label="Valor">{formatCurrency(lease.rent_amount)}</td>
                           <td data-label="PDF">
                             <div className="row-actions">
                               <a className="btn btn-sm btn-secondary" href={`/contract-pdf?id=${lease.id}`} target="_blank" rel="noreferrer">Contrato</a>
                             </div>
                           </td>
                           <td className="row-actions">
                             <button type="button" className="btn btn-sm btn-secondary" onClick={() => startLeaseEdit(lease)}>Editar</button>
                             {lease.status === 'active' ? (
                               <DeleteRowButton intent="lease.terminate" tab="leases" id={lease.id} onOptimisticDelete={() => setLeaseItems((current) => current.map((row) => row.id === lease.id ? { ...row, status: 'terminated' } : row))} onSettled={settleAfterMutation} queueOfflineMutation={queueOfflineMutation} btnLabel="Encerrar" btnClass="btn btn-sm btn-outline" confirmMessage="Encerrar este contrato? Ele ficará inativo." />
                             ) : (
                               <DeleteRowButton intent="lease.reactivate" tab="leases" id={lease.id} onOptimisticDelete={() => setLeaseItems((current) => current.map((row) => row.id === lease.id ? { ...row, status: 'active' } : row))} onSettled={settleAfterMutation} queueOfflineMutation={queueOfflineMutation} btnLabel="Reativar" btnClass="btn btn-sm btn-secondary" confirmMessage="Reativar este contrato?" />
                             )}
                             <span style={{ marginLeft: '0.25rem' }}>
                               <DeleteRowButton intent="lease.delete" tab="leases" id={lease.id} onOptimisticDelete={() => setLeaseItems((current) => current.filter((row) => row.id !== lease.id))} onSettled={settleAfterMutation} queueOfflineMutation={queueOfflineMutation} />
                             </span>
                           </td>
                        </tr>
                       )) : <tr><td colSpan={8}>Cadastre um inquilino e um imóvel para criar contratos.</td></tr>}
                    </tbody>
                  </table>
                </div>
                <Pagination page={leasePage} totalPages={leaseTotalPages} onChange={setLeasePage} />
              </section>
            ) : null}

            {tab === 'receipts' ? (
              <section className="card stack-lg receipts-section">
                <div className="section-header">
                  <h2>Pagamentos e Recibos</h2>
                  <div className="auth-actions">
                    {selectedReceipts.size > 0 ? (
                      <receiptFetcher.Form method="post" onSubmit={(event) => {
                        if (!window.confirm(`Marcar ${selectedReceipts.size} recibo(s) como pago(s)?`)) {
                          event.preventDefault();
                          return;
                        }
                      }}>
                        <input type="hidden" name="intent" value="receipt.batch-pay" />
                        <input type="hidden" name="ids" value={Array.from(selectedReceipts).join(',')} />
                        <button type="submit" className="btn btn-secondary" disabled={receiptFetcher.state !== 'idle'}>Pagar {selectedReceipts.size} selecionado(s)</button>
                      </receiptFetcher.Form>
                    ) : null}
                    <receiptFetcher.Form method="post" onSubmit={(event) => {
                      if (handleOfflineSubmit(event, () => applyOfflineMonthlyReceiptGeneration(), 'Geração mensal colocada na fila de sincronização.')) {
                        return;
                      }
                    }}>
                      <input type="hidden" name="intent" value="receipt.generate-monthly" />
                      <button type="submit" className="btn btn-secondary" disabled={receiptFetcher.state !== 'idle'}>Gerar pagamentos</button>
                    </receiptFetcher.Form>
                    <button type="button" className="btn btn-primary" onClick={() => setCreateModalType('receipts')}>Novo pagamento</button>
                  </div>
                </div>
                <FilterBar value={receiptSearch} onChange={setReceiptSearch} placeholder="Buscar por contrato, imóvel, vencimento ou valor">
                  <SmallSelect value={receiptMonth} onChange={(v) => setReceiptMonth(v)}>
                    {Array.from({ length: 12 }, (_, i) => String(i + 1).padStart(2, '0')).map((m) => (
                      <option key={m} value={m}>{m}</option>
                    ))}
                  </SmallSelect>
                  <SmallSelect value={receiptYear} onChange={(v) => setReceiptYear(v)}>
                    {Array.from({ length: 5 }, (_, i) => String(new Date().getFullYear() - 2 + i)).map((y) => (
                      <option key={y} value={y}>{y}</option>
                    ))}
                  </SmallSelect>
                  <SmallSelect value={receiptStatus} onChange={(v) => setReceiptStatus(v as 'all' | 'due' | 'overdue' | 'paid')}>
                    <option value="all">Todos</option>
                    <option value="due">A vencer</option>
                    <option value="overdue">Vencido</option>
                    <option value="paid">Pago</option>
                  </SmallSelect>
                </FilterBar>
                <div className="table-wrap">
                  <table className="receipts-table">
                    <thead><tr>
                      <th style={{ width: '2rem' }}><input type="checkbox" onChange={(e) => {
                        if (e.target.checked) {
                          setSelectedReceipts(new Set(pagedReceipts.filter((r) => !r.paid_at).map((r) => r.id)));
                        } else {
                          setSelectedReceipts(new Set());
                        }
                      }} checked={pagedReceipts.length > 0 && pagedReceipts.every((r) => !r.paid_at ? selectedReceipts.has(r.id) : true) && selectedReceipts.size > 0} /></th>
                      <th><SortHeader label="Contrato" active={receiptSort.key === 'tenant_name'} direction={receiptSort.direction} onClick={() => toggleSort(receiptSort, 'tenant_name', setReceiptSort)} /></th><th><SortHeader label="Status" active={receiptSort.key === 'paid_at'} direction={receiptSort.direction} onClick={() => toggleSort(receiptSort, 'paid_at', setReceiptSort)} /></th><th><SortHeader label="Vencimento" active={receiptSort.key === 'due_date'} direction={receiptSort.direction} onClick={() => toggleSort(receiptSort, 'due_date', setReceiptSort)} /></th><th><SortHeader label="Pagamento" active={receiptSort.key === 'paid_at'} direction={receiptSort.direction} onClick={() => toggleSort(receiptSort, 'paid_at', setReceiptSort)} /></th><th><SortHeader label="Valor" active={receiptSort.key === 'amount'} direction={receiptSort.direction} onClick={() => toggleSort(receiptSort, 'amount', setReceiptSort)} /></th><th>PDF</th><th>Ações</th></tr></thead>
                    <tbody>
                      {pagedReceipts.length ? pagedReceipts.map((receipt) => (
                        <tr key={receipt.id}>
                          <td data-label="Sel"><input type="checkbox" checked={selectedReceipts.has(receipt.id)} disabled={!!receipt.paid_at} onChange={() => {
                            const next = new Set(selectedReceipts);
                            if (next.has(receipt.id)) next.delete(receipt.id); else next.add(receipt.id);
                            setSelectedReceipts(next);
                          }} /></td>
                          <td data-label="Contrato" className="contract-cell"><span>{receipt.tenant_name} {pendingIds.receipts.has(receipt.id) ? <PendingBadge /> : null}</span><span>{receipt.property_name}</span></td>
                          <td data-label="Status"><span className={`status-badge ${getReceiptStatus(receipt)}`}>{getReceiptStatusLabel(getReceiptStatus(receipt))}</span></td>
                          <td data-label="Vencimento">{formatDateBr(receipt.due_date)}</td>
                          <td data-label="Pagamento">{formatDateTimeBr(receipt.paid_at)}</td>
                          <td data-label="Valor">{formatCurrency(receipt.amount)}</td>
                          <td data-label="PDF">
                              {receipt.paid_at ? (
                                <div className="row-actions">
                                  <a className="btn btn-sm btn-secondary" href={`/pdf?id=${receipt.id}`} target="_blank" rel="noreferrer">Abrir PDF</a>
                                  <a className="btn btn-sm btn-secondary" href={`/pdf?id=${receipt.id}&assinatura=false`} target="_blank" rel="noreferrer">PDF s/ assinatura</a>
                                  <a className="btn btn-sm btn-secondary" href={`/pdf?id=${receipt.id}`} target="_blank" rel="noreferrer" download>
                                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                                    {' '}Download
                                  </a>
                                </div>
                              ) : '-'}
                            </td>
                          <td className="row-actions">
                            <button type="button" className="btn btn-sm btn-secondary" onClick={() => startReceiptEdit(receipt)}>Editar</button>
                            <DeleteRowButton intent="receipt.delete" tab="receipts" id={receipt.id} onOptimisticDelete={() => setReceiptItems((current) => current.filter((row) => row.id !== receipt.id))} onSettled={settleAfterMutation} queueOfflineMutation={queueOfflineMutation} />
                          </td>
                        </tr>
                      )) : <tr><td colSpan={8}>Cadastre um contrato para gerar recibos.</td></tr>}
                    </tbody>
                  </table>
                </div>
                <Pagination page={receiptPage} totalPages={receiptTotalPages} onChange={setReceiptPage} />
              </section>
            ) : null}

            {modalType === 'tenants' ? <Modal title="Editar Inquilino" onClose={() => setModalType('')}>
              <tenantFetcher.Form method="post" className="stack" onSubmit={(event) => {
                if (handleOfflineSubmit(event, (payload) => {
                  setTenantItems((current) => current.map((row) => row.id === payload.id ? { ...row, name: payload.name, email: payload.email, phone: payload.phone || null, cpf: payload.cpf || null } : row));
                }, 'Alteração pendente de sincronização.', () => {
                  setTenantDraft({ id: '', name: '', email: '', phone: '', cpf: '' });
                  setModalType('');
                })) {
                  return;
                }
                setTenantItems((current) => current.map((row) => row.id === tenantDraft.id ? { ...row, name: tenantDraft.name, email: tenantDraft.email, phone: tenantDraft.phone || null, cpf: tenantDraft.cpf || null } : row));
              }}>
                <input type="hidden" name="intent" value="tenant.update" />
                <input type="hidden" name="id" value={tenantDraft.id} />
                <div className="form-group"><label htmlFor="modal-tenant-name">Nome</label><input id="modal-tenant-name" name="name" value={tenantDraft.name} onChange={(e) => setTenantDraft((current) => ({ ...current, name: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="modal-tenant-email">Email</label><input id="modal-tenant-email" name="email" type="email" value={tenantDraft.email} onChange={(e) => setTenantDraft((current) => ({ ...current, email: e.target.value }))} required /></div>
                <CpfInput id="modal-tenant-cpf" name="cpf" value={tenantDraft.cpf} onChange={(value) => setTenantDraft((current) => ({ ...current, cpf: value }))} label="CPF" />
                <PhoneInput id="modal-tenant-phone" name="phone" value={tenantDraft.phone} onChange={(value) => setTenantDraft((current) => ({ ...current, phone: value }))} label="Telefone" />
                <button type="submit" className="btn btn-primary" disabled={tenantFetcher.state !== 'idle'}>Salvar alterações</button>
                <button type="button" className="btn btn-sm btn-secondary" onClick={() => { setTenantDraft({ id: '', name: '', email: '', phone: '', cpf: '' }); setModalType(''); }}>Descartar</button>
              </tenantFetcher.Form>
            </Modal> : null}

            {createModalType === 'tenants' ? <Modal title="Novo Inquilino" onClose={() => setCreateModalType('')}>
              <tenantFetcher.Form method="post" className="stack" onSubmit={(event) => {
                if (handleOfflineSubmit(event, (payload) => {
                  const item = { id: payload.id, name: payload.name, email: payload.email, phone: payload.phone || null, cpf: payload.cpf || null };
                  setTenantItems((current) => [item, ...current]);
                }, 'Cadastro pendente de sincronização.', () => {
                  setTenantForm({ name: '', email: '', phone: '', cpf: '' });
                  setCreateModalType('');
                })) {
                  return;
                }
                const item = { id: `tmp-${createId()}`, name: tenantForm.name, email: tenantForm.email, phone: tenantForm.phone || null, cpf: tenantForm.cpf || null };
                setTenantItems((current) => [item, ...current]);
              }}>
                <input type="hidden" name="intent" value="tenant.create" />
                <input type="hidden" name="tab" value="tenants" />
                <div className="form-group"><label htmlFor="create-tenant-name">Nome</label><input id="create-tenant-name" name="name" value={tenantForm.name} onChange={(e) => setTenantForm((current) => ({ ...current, name: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="create-tenant-email">Email</label><input id="create-tenant-email" name="email" type="email" value={tenantForm.email} onChange={(e) => setTenantForm((current) => ({ ...current, email: e.target.value }))} required /></div>
                <CpfInput id="create-tenant-cpf" name="cpf" value={tenantForm.cpf} onChange={(value) => setTenantForm((current) => ({ ...current, cpf: value }))} label="CPF" />
                <PhoneInput id="create-tenant-phone" name="phone" value={tenantForm.phone} onChange={(value) => setTenantForm((current) => ({ ...current, phone: value }))} label="Telefone" />
                <button type="submit" className="btn btn-primary" disabled={tenantFetcher.state !== 'idle'}>Adicionar inquilino</button>
              </tenantFetcher.Form>
            </Modal> : null}

            {modalType === 'properties' ? <Modal title="Editar Imóvel" onClose={() => setModalType('')}>
              <propertyFetcher.Form method="post" className="stack" onSubmit={(event) => {
                if (handleOfflineSubmit(event, (payload) => {
                  setPropertyItems((current) => current.map((row) => row.id === payload.id ? { ...row, name: payload.name, address: payload.address, city: payload.city, state: payload.state, zip: payload.zip || null } : row));
                }, 'Alteração pendente de sincronização.', () => {
                  setPropertyDraft({ id: '', name: '', address: '', city: '', state: '', zip: '' });
                  setModalType('');
                })) {
                  return;
                }
                setPropertyItems((current) => current.map((row) => row.id === propertyDraft.id ? { ...row, name: propertyDraft.name, address: propertyDraft.address, city: propertyDraft.city, state: propertyDraft.state, zip: propertyDraft.zip || null } : row));
              }}>
                <input type="hidden" name="intent" value="property.update" />
                <input type="hidden" name="id" value={propertyDraft.id} />
                <div className="form-group"><label htmlFor="modal-property-name">Nome</label><input id="modal-property-name" name="name" value={propertyDraft.name} onChange={(e) => setPropertyDraft((current) => ({ ...current, name: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="modal-property-address">Endereço</label><input id="modal-property-address" name="address" value={propertyDraft.address} onChange={(e) => setPropertyDraft((current) => ({ ...current, address: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="modal-property-city">Cidade</label><input id="modal-property-city" name="city" value={propertyDraft.city} onChange={(e) => setPropertyDraft((current) => ({ ...current, city: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="modal-property-state">Estado</label><input id="modal-property-state" name="state" value={propertyDraft.state} onChange={(e) => setPropertyDraft((current) => ({ ...current, state: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="modal-property-zip">CEP</label><input id="modal-property-zip" name="zip" value={propertyDraft.zip} onChange={(e) => setPropertyDraft((current) => ({ ...current, zip: e.target.value }))} /></div>
                <button type="submit" className="btn btn-primary" disabled={propertyFetcher.state !== 'idle'}>Salvar alterações</button>
                <button type="button" className="btn btn-sm btn-secondary" onClick={() => { setPropertyDraft({ id: '', name: '', address: '', city: '', state: '', zip: '' }); setModalType(''); }}>Descartar</button>
              </propertyFetcher.Form>
            </Modal> : null}

            {createModalType === 'properties' ? <Modal title="Novo Imóvel" onClose={() => setCreateModalType('')}>
              <propertyFetcher.Form method="post" className="stack" onSubmit={(event) => {
                if (handleOfflineSubmit(event, (payload) => {
                  const item = { id: payload.id, name: payload.name, address: payload.address, city: payload.city, state: payload.state, zip: payload.zip || null };
                  setPropertyItems((current) => [item, ...current]);
                }, 'Cadastro pendente de sincronização.', () => {
                  setPropertyForm({ name: '', address: '', city: '', state: '', zip: '' });
                  setCreateModalType('');
                })) {
                  return;
                }
                const item = { id: `tmp-${createId()}`, name: propertyForm.name, address: propertyForm.address, city: propertyForm.city, state: propertyForm.state, zip: propertyForm.zip || null };
                setPropertyItems((current) => [item, ...current]);
              }}>
                <input type="hidden" name="intent" value="property.create" />
                <input type="hidden" name="tab" value="properties" />
                <div className="form-group"><label htmlFor="create-property-name">Nome</label><input id="create-property-name" name="name" value={propertyForm.name} onChange={(e) => setPropertyForm((current) => ({ ...current, name: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="create-property-address">Endereço</label><input id="create-property-address" name="address" value={propertyForm.address} onChange={(e) => setPropertyForm((current) => ({ ...current, address: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="create-property-city">Cidade</label><input id="create-property-city" name="city" value={propertyForm.city} onChange={(e) => setPropertyForm((current) => ({ ...current, city: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="create-property-state">Estado</label><input id="create-property-state" name="state" value={propertyForm.state} onChange={(e) => setPropertyForm((current) => ({ ...current, state: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="create-property-zip">CEP</label><input id="create-property-zip" name="zip" value={propertyForm.zip} onChange={(e) => setPropertyForm((current) => ({ ...current, zip: e.target.value }))} /></div>
                <button type="submit" className="btn btn-primary" disabled={propertyFetcher.state !== 'idle'}>Adicionar imóvel</button>
              </propertyFetcher.Form>
            </Modal> : null}

            {modalType === 'leases' ? <Modal title="Editar Contrato" onClose={() => setModalType('')}>
              <leaseFetcher.Form method="post" className="stack" onSubmit={(event) => {
                if (handleOfflineSubmit(event, (payload) => {
                  const tenant = tenantItems.find((item) => item.id === payload.tenant_id);
                  const property = propertyItems.find((item) => item.id === payload.property_id);
                  setLeaseItems((current) => current.map((row) => row.id === payload.id ? { ...row, tenant_id: payload.tenant_id, property_id: payload.property_id, start_date: payload.start_date, end_date: payload.end_date, due_day: payload.due_day, rent_amount: payload.rent_amount, tenant_name: tenant?.name || '-', property_name: property?.name || '-' } : row));
                }, 'Alteração pendente de sincronização.', () => {
                  setLeaseDraft({ id: '', tenant_id: '', property_id: '', start_date: '', end_date: '', due_day: '1', rent_amount: '' });
                  setModalType('');
                })) {
                  return;
                }
                const tenant = tenantItems.find((item) => item.id === leaseDraft.tenant_id);
                const property = propertyItems.find((item) => item.id === leaseDraft.property_id);
                setLeaseItems((current) => current.map((row) => row.id === leaseDraft.id ? { ...row, tenant_id: leaseDraft.tenant_id, property_id: leaseDraft.property_id, start_date: leaseDraft.start_date, end_date: leaseDraft.end_date, due_day: leaseDraft.due_day, rent_amount: leaseDraft.rent_amount, tenant_name: tenant?.name || '-', property_name: property?.name || '-' } : row));
              }}>
                <input type="hidden" name="intent" value="lease.update" />
                <input type="hidden" name="id" value={leaseDraft.id} />
                <CustomSelect id="modal-lease-tenant" name="tenant_id" value={leaseDraft.tenant_id} onChange={(v) => setLeaseDraft((current) => ({ ...current, tenant_id: v }))} label="Inquilino" required>
                  <option value="">Selecione</option>
                  {tenantItems.map((tenant) => <option key={tenant.id} value={tenant.id}>{tenant.name}</option>)}
                </CustomSelect>
                <CustomSelect id="modal-lease-property" name="property_id" value={leaseDraft.property_id} onChange={(v) => setLeaseDraft((current) => ({ ...current, property_id: v }))} label="Imóvel" required>
                  <option value="">Selecione</option>
                  {propertyItems.map((property) => <option key={property.id} value={property.id}>{property.name}</option>)}
                </CustomSelect>
                <DatePickerInput id="modal-lease-start" name="start_date" value={leaseDraft.start_date} onChange={(v) => setLeaseDraft((current) => ({ ...current, start_date: v }))} label="Início" required />
                <DatePickerInput id="modal-lease-end" name="end_date" value={leaseDraft.end_date} onChange={(v) => setLeaseDraft((current) => ({ ...current, end_date: v }))} label="Fim" required />
                <div className="form-group"><label htmlFor="modal-lease-due-day">Dia do vencimento</label><input id="modal-lease-due-day" name="due_day" type="number" min="1" max="31" value={leaseDraft.due_day} onChange={(e) => setLeaseDraft((current) => ({ ...current, due_day: e.target.value }))} required /></div>
                <MoneyInput id="modal-lease-rent" name="rent_amount" value={leaseDraft.rent_amount} onChange={(value) => setLeaseDraft((current) => ({ ...current, rent_amount: value }))} label="Aluguel" required />
                <button type="submit" className="btn btn-primary" disabled={leaseFetcher.state !== 'idle'}>Salvar alterações</button>
                <button type="button" className="btn btn-sm btn-secondary" onClick={() => { setLeaseDraft({ id: '', tenant_id: '', property_id: '', start_date: '', end_date: '', due_day: '1', rent_amount: '' }); setModalType(''); }}>Descartar</button>
              </leaseFetcher.Form>
            </Modal> : null}

            {createModalType === 'leases' ? <Modal title="Novo Contrato" onClose={() => setCreateModalType('')}>
              <leaseFetcher.Form method="post" className="stack" onSubmit={(event) => {
                if (handleOfflineSubmit(event, (payload) => {
                  const tenant = tenantItems.find((item) => item.id === payload.tenant_id);
                  const property = propertyItems.find((item) => item.id === payload.property_id);
                  const item = { id: payload.id, tenant_id: payload.tenant_id, property_id: payload.property_id, start_date: payload.start_date, end_date: payload.end_date, due_day: payload.due_day, rent_amount: payload.rent_amount, tenant_name: tenant?.name || '-', property_name: property?.name || '-', status: 'active' };
                  setLeaseItems((current) => [item, ...current]);
                }, 'Cadastro pendente de sincronização.', () => {
                  setLeaseForm({ tenant_id: '', property_id: '', start_date: '', end_date: '', due_day: '1', rent_amount: '' });
                  setCreateModalType('');
                })) {
                  return;
                }
                const tenant = tenantItems.find((item) => item.id === leaseForm.tenant_id);
                const property = propertyItems.find((item) => item.id === leaseForm.property_id);
                const item = { id: `tmp-${createId()}`, tenant_id: leaseForm.tenant_id, property_id: leaseForm.property_id, start_date: leaseForm.start_date, end_date: leaseForm.end_date, due_day: leaseForm.due_day, rent_amount: leaseForm.rent_amount, tenant_name: tenant?.name || '-', property_name: property?.name || '-', status: 'active' };
                setLeaseItems((current) => [item, ...current]);
              }}>
                <input type="hidden" name="intent" value="lease.create" />
                <input type="hidden" name="tab" value="leases" />
                <CustomSelect id="create-lease-tenant" name="tenant_id" value={leaseForm.tenant_id} onChange={(v) => setLeaseForm((current) => ({ ...current, tenant_id: v }))} label="Inquilino" required>
                  <option value="">Selecione</option>
                  {tenantItems.map((tenant) => <option key={tenant.id} value={tenant.id}>{tenant.name}</option>)}
                </CustomSelect>
                <CustomSelect id="create-lease-property" name="property_id" value={leaseForm.property_id} onChange={(v) => setLeaseForm((current) => ({ ...current, property_id: v }))} label="Imóvel" required>
                  <option value="">Selecione</option>
                  {propertyItems.map((property) => <option key={property.id} value={property.id}>{property.name}</option>)}
                </CustomSelect>
                <DatePickerInput id="create-lease-start" name="start_date" value={leaseForm.start_date} onChange={(v) => setLeaseForm((current) => ({ ...current, start_date: v }))} label="Início" required />
                <DatePickerInput id="create-lease-end" name="end_date" value={leaseForm.end_date} onChange={(v) => setLeaseForm((current) => ({ ...current, end_date: v }))} label="Fim" required />
                <div className="form-group"><label htmlFor="create-lease-due-day">Dia do vencimento</label><input id="create-lease-due-day" name="due_day" type="number" min="1" max="31" value={leaseForm.due_day} onChange={(e) => setLeaseForm((current) => ({ ...current, due_day: e.target.value }))} required /></div>
                <MoneyInput id="create-lease-rent" name="rent_amount" value={leaseForm.rent_amount} onChange={(value) => setLeaseForm((current) => ({ ...current, rent_amount: value }))} label="Aluguel" required />
                <button type="submit" className="btn btn-primary" disabled={leaseFetcher.state !== 'idle'}>Adicionar contrato</button>
              </leaseFetcher.Form>
            </Modal> : null}

            {modalType === 'receipts' ? <Modal title="Editar Pagamento" onClose={() => setModalType('')}>
              <receiptFetcher.Form method="post" className="stack" onSubmit={(event) => {
                if (handleOfflineSubmit(event, (payload) => {
                  const lease = receiptLeaseMeta(payload.lease_id);
                  setReceiptItems((current) => current.map((row) => row.id === payload.id ? { ...row, lease_id: payload.lease_id, due_date: payload.due_date, paid_at: payload.paid_at || null, amount: payload.amount, tenant_name: lease?.tenant_name || '-', property_name: lease?.property_name || '-', pdf_path: null } : row));
                }, 'Alteração pendente de sincronização.', () => {
                  setReceiptDraft({ id: '', lease_id: '', due_date: '', paid_at: '', amount: '' });
                  setModalType('');
                })) {
                  return;
                }
                const lease = receiptLeaseMeta(receiptDraft.lease_id);
                setReceiptItems((current) => current.map((row) => row.id === receiptDraft.id ? { ...row, lease_id: receiptDraft.lease_id, due_date: receiptDraft.due_date, paid_at: receiptDraft.paid_at || null, amount: receiptDraft.amount, tenant_name: lease?.tenant_name || '-', property_name: lease?.property_name || '-' } : row));
              }}>
                <input type="hidden" name="intent" value="receipt.update" />
                <input type="hidden" name="id" value={receiptDraft.id} />
                <CustomSelect id="modal-receipt-lease" name="lease_id" value={receiptDraft.lease_id} onChange={(v) => setReceiptDraft((current) => ({ ...current, lease_id: v }))} label="Contrato" required>
                  <option value="">Selecione</option>
                  {leaseItems.map((lease) => <option key={lease.id} value={lease.id}>{lease.tenant_name} / {lease.property_name}</option>)}
                </CustomSelect>
                <DatePickerInput id="modal-receipt-due" name="due_date" value={receiptDraft.due_date} onChange={(v) => setReceiptDraft((current) => ({ ...current, due_date: v }))} label="Vencimento" required />
                <DateTimePickerInput id="modal-receipt-paid" name="paid_at" value={receiptDraft.paid_at} onChange={(v) => setReceiptDraft((current) => ({ ...current, paid_at: v }))} label="Pago em" />
                <MoneyInput id="modal-receipt-amount" name="amount" value={receiptDraft.amount} onChange={(value) => setReceiptDraft((current) => ({ ...current, amount: value }))} label="Valor" required />
                <button type="submit" className="btn btn-primary" disabled={receiptFetcher.state !== 'idle'}>Salvar alterações</button>
                <button type="button" className="btn btn-sm btn-secondary" onClick={() => { setReceiptDraft({ id: '', lease_id: '', due_date: '', paid_at: '', amount: '' }); setModalType(''); }}>Descartar</button>
              </receiptFetcher.Form>
            </Modal> : null}

            {createModalType === 'receipts' ? <Modal title="Novo Pagamento" onClose={() => setCreateModalType('')}>
              <receiptFetcher.Form method="post" className="stack" onSubmit={(event) => {
                if (handleOfflineSubmit(event, (payload) => {
                  const lease = receiptLeaseMeta(payload.lease_id);
                  const item = { id: payload.id, lease_id: payload.lease_id, due_date: payload.due_date, paid_at: payload.paid_at || null, amount: payload.amount, pdf_path: null, generated_key: null, tenant_name: lease?.tenant_name || '-', property_name: lease?.property_name || '-' };
                  setReceiptItems((current) => [item, ...current]);
                }, 'Cadastro pendente de sincronização.', () => {
                  setReceiptForm({ lease_id: '', due_date: '', paid_at: '', amount: '' });
                  setCreateModalType('');
                })) {
                  return;
                }
                const lease = receiptLeaseMeta(receiptForm.lease_id);
                const item = { id: `tmp-${createId()}`, lease_id: receiptForm.lease_id, due_date: receiptForm.due_date, paid_at: receiptForm.paid_at || null, amount: receiptForm.amount, pdf_path: null, generated_key: null, tenant_name: lease?.tenant_name || '-', property_name: lease?.property_name || '-' };
                setReceiptItems((current) => [item, ...current]);
              }}>
                <input type="hidden" name="intent" value="receipt.create" />
                <input type="hidden" name="tab" value="receipts" />
                <CustomSelect id="create-receipt-lease" name="lease_id" value={receiptForm.lease_id} onChange={(v) => setReceiptForm((current) => ({ ...current, lease_id: v }))} label="Contrato" required>
                  <option value="">Selecione</option>
                  {leaseItems.map((lease) => <option key={lease.id} value={lease.id}>{lease.tenant_name} / {lease.property_name}</option>)}
                </CustomSelect>
                <DatePickerInput id="create-receipt-due" name="due_date" value={receiptForm.due_date} onChange={(v) => setReceiptForm((current) => ({ ...current, due_date: v }))} label="Vencimento" required />
                <DateTimePickerInput id="create-receipt-paid" name="paid_at" value={receiptForm.paid_at} onChange={(v) => setReceiptForm((current) => ({ ...current, paid_at: v }))} label="Pago em" />
                <MoneyInput id="create-receipt-amount" name="amount" value={receiptForm.amount} onChange={(value) => setReceiptForm((current) => ({ ...current, amount: value }))} label="Valor" required />
                <button type="submit" className="btn btn-primary" disabled={receiptFetcher.state !== 'idle'}>Adicionar pagamento</button>
              </receiptFetcher.Form>
            </Modal> : null}
          </div>
        </main>
      </div>
  );
}
