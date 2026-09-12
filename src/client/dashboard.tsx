import React, { useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import {
  Brand,
  MenuGlyph,
  DataButtons,
  ThemeToggle,
  DeleteRowButton,
  SyncBanner,
  PendingBadge,
  Toast,
  Modal,
  FilterBar,
  DatePickerInput,
  DateTimePickerInput,
  MoneyInput,
  CpfInput,
  PhoneInput,
  CustomSelect,
  SortHeader,
  SmallSelect,
  Pagination,
  LoadingRow,
  WhatsAppConnectModal,
  ConfirmModal,
} from '../components/ui';
import type { SendJob, SendStage, WhatsAppStatus } from '../whatsapp/types';
import {
  IconTenants,
  IconProperties,
  IconLeases,
  IconReceipts,
  IconLogout,
  IconSettings,
  IconDashboard,
  IconWhatsApp,
} from '../components/icons';

import { MetricsView } from './metrics';

import type {
  Tenant,
  Property,
  Lease,
  Receipt,
  MutationResponse,
  SortState,
  Tab,
  QueuePayload,
  PendingIds,
  MetricsPayload,
} from '../types/dashboard';
import {
  getDaysInMonth,
  getReceiptStatus,
  getReceiptStatusLabel,
  formatCurrency,
  formatDateBr,
  formatDateTimeBr,
  sortItems,
  paginateItems,
} from '../utils/format';
import {
  readDashboardCache,
  writeDashboardCache,
  readOfflineQueue,
  writeOfflineQueue,
  enqueueOfflineMutation,
  serializeFormData,
  submitQueuedMutation,
  getPendingIds,
  getSyncErrorMessage,
  isCreateIntent,
} from '../utils/offline';
import { createId } from '../utils/id';

export interface DashboardProps {
  tab: Tab;
  user: string;
  tenants: Tenant[];
  properties: Property[];
  leases: Lease[];
  receipts: Receipt[];
  activeTenantIds: string[];
  metrics?: MetricsPayload;
}

function PdfDropdown({ id }: { id: string }) {
  const [open, setOpen] = useState(false);
  const [menuStyle, setMenuStyle] = useState<React.CSSProperties>({});
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    function reposition() {
      if (!triggerRef.current) return;
      const r = triggerRef.current.getBoundingClientRect();
      const menuH = 120; // approximate; menu is short
      const spaceBelow = window.innerHeight - r.bottom;
      const goUp = spaceBelow < menuH && r.top > spaceBelow;
      const goLeft = r.left + 170 > window.innerWidth;
      setMenuStyle({
        position: 'fixed',
        zIndex: 9999,
        minWidth: '170px',
        background: 'var(--surface)',
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius-md)',
        boxShadow: 'var(--shadow-lg)',
        padding: '0.25rem',
        display: 'flex',
        flexDirection: 'column',
        gap: '2px',
        ...(goUp
          ? { bottom: window.innerHeight - r.top + 4, top: 'auto' }
          : { top: r.bottom + 4, bottom: 'auto' }),
        ...(goLeft
          ? { right: window.innerWidth - r.right, left: 'auto' }
          : { left: r.left, right: 'auto' }),
      });
    }
    reposition();
    window.addEventListener('scroll', reposition, true);
    window.addEventListener('resize', reposition);
    function handleClickOutside(e: MouseEvent) {
      const t = e.target as Node;
      const inTrigger = triggerRef.current && triggerRef.current.contains(t);
      const inMenu = menuRef.current && menuRef.current.contains(t);
      if (!inTrigger && !inMenu) setOpen(false);
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => {
      window.removeEventListener('scroll', reposition, true);
      window.removeEventListener('resize', reposition);
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, [open]);

  const menu = (
    <div ref={menuRef} style={menuStyle} role="menu">
      <a
        className="btn btn-sm btn-secondary"
        href={`/pdf?id=${id}`}
        target="_blank"
        rel="noreferrer"
        role="menuitem"
        style={{ justifyContent: 'flex-start', fontWeight: 500 }}
        onClick={() => setOpen(false)}
      >
        Abrir PDF
      </a>
      <a
        className="btn btn-sm btn-secondary"
        href={`/pdf?id=${id}&assinatura=false`}
        target="_blank"
        rel="noreferrer"
        role="menuitem"
        style={{ justifyContent: 'flex-start', fontWeight: 500 }}
        onClick={() => setOpen(false)}
      >
        PDF s/ assinatura
      </a>
      <a
        className="btn btn-sm btn-secondary"
        href={`/pdf?id=${id}`}
        target="_blank"
        rel="noreferrer"
        download
        role="menuitem"
        style={{ justifyContent: 'flex-start', fontWeight: 500, display: 'inline-flex', alignItems: 'center', gap: '0.4rem' }}
        onClick={() => setOpen(false)}
      >
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
          <polyline points="7 10 12 15 17 10" />
          <line x1="12" y1="15" x2="12" y2="3" />
        </svg>
        Download
      </a>
    </div>
  );

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className="btn btn-sm btn-secondary"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="true"
        aria-expanded={open}
        style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem' }}
      >
        PDF
        <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>
      {open && createPortal(menu, document.body)}
    </>
  );
}


function delay(ms: number): Promise<void> {
  const { promise, resolve } = Promise.withResolvers<void>();
  setTimeout(resolve, ms);
  return promise;
}

function friendlyWhatsAppError(raw: string): string {
  const message = raw.toLowerCase();
  if (message.includes('already running')) {
    return 'Já existe um envio em andamento. Aguarde a conclusão e tente novamente.';
  }
  if (message.includes('chromium') || message.includes('failed to launch')) {
    return 'O envio automático está indisponível neste dispositivo no momento.';
  }
  if (message.includes('expirad') || message.includes('autenticad') || message.includes('whatsapp:login')) {
    return 'Sua conexão com o WhatsApp expirou. Conecte novamente para enviar.';
  }
  if (
    message.includes('waiting failed') ||
    message.includes('timeout') ||
    message.includes('exceeded') ||
    message.includes('botão de envio') ||
    message.includes('tempo esgotado')
  ) {
    return 'Não foi possível concluir o envio pelo WhatsApp agora. Tente novamente em instantes.';
  }
  return raw;
}

/** Etapas exibidas ao usuário durante o envio automático pelo WhatsApp. */
const WHATSAPP_SEND_STEPS: { stage: SendStage; label: string }[] = [
  { stage: 'preparing', label: 'Preparando o envio' },
  { stage: 'connecting', label: 'Conectando ao WhatsApp' },
  { stage: 'opening_chat', label: 'Abrindo a conversa' },
  { stage: 'sending', label: 'Enviando a mensagem' },
  { stage: 'confirming', label: 'Confirmando a entrega' },
];

export function DashboardPage(initialData: DashboardProps) {
  const [tab, setTab] = useState<Tab>(initialData.tab);
  const [user] = useState(initialData.user);

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
    const handlePopState = () => {
      const url = new URL(window.location.href);
      const urlTab = url.searchParams.get('tab') as Tab;
      if (urlTab && ['dashboard', 'tenants', 'properties', 'leases', 'receipts'].includes(urlTab)) {
        setTab(urlTab);
      }
    };
    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, []);

  const switchTab = (nextTab: Tab, e?: React.MouseEvent) => {
    if (e) e.preventDefault();
    setTab(nextTab);
    const url = new URL(window.location.href);
    url.searchParams.set('tab', nextTab);
    window.history.pushState({}, '', url.toString());
  };

  const [tenantItems, setTenantItems] = useState(initialData.tenants);
  const [propertyItems, setPropertyItems] = useState(initialData.properties);
  const [leaseItems, setLeaseItems] = useState(initialData.leases);
  const [receiptItems, setReceiptItems] = useState(initialData.receipts);

  const [tenantForm, setTenantForm] = useState({ name: '', email: '', phone: '', cpf: '' });
  const [propertyForm, setPropertyForm] = useState({ name: '', address: '', city: '', state: '', zip: '' });
  const [leaseForm, setLeaseForm] = useState({ tenant_id: '', property_id: '', start_date: '', end_date: '', due_day: '1', rent_amount: '', status: 'active' });
  const [receiptForm, setReceiptForm] = useState({ lease_id: '', due_date: '', paid_at: '', amount: '', category: 'rent' as 'rent' | 'extra', description: '', tenant_id: '', property_id: '' });

  const [tenantDraft, setTenantDraft] = useState({ id: '', name: '', email: '', phone: '', cpf: '' });
  const [propertyDraft, setPropertyDraft] = useState({ id: '', name: '', address: '', city: '', state: '', zip: '' });
  const [leaseDraft, setLeaseDraft] = useState({ id: '', tenant_id: '', property_id: '', start_date: '', end_date: '', due_day: '1', rent_amount: '', status: 'active' });
  const [receiptDraft, setReceiptDraft] = useState({ id: '', lease_id: '', due_date: '', paid_at: '', amount: '' });
  const [payDraft, setPayDraft] = useState<{ id: string; amount: string; pay_amount: string; paid_at: string }>({ id: '', amount: '', pay_amount: '', paid_at: '' });
  const [payModalOpen, setPayModalOpen] = useState(false);

  const [modalType, setModalType] = useState<'' | Tab>('');
  const [createModalType, setCreateModalType] = useState<'' | Tab>('');
  const [toast, setToast] = useState<{ message: string; tone: 'success' | 'error' } | null>(null);
  const [tenantSearch, setTenantSearch] = useState('');
  const [propertySearch, setPropertySearch] = useState('');
  const [leaseSearch, setLeaseSearch] = useState('');
  const [leaseStatus, setLeaseStatus] = useState<'all' | 'active' | 'suspended' | 'season' | 'terminated'>('all');
  // Persistência dos filtros + ordenação da tabela de Recibos via localStorage
  // (chave única por usuário evita mistura entre contas no mesmo navegador).
  const receiptFiltersKey = `aluxen:filters:receipts:${user}`;

  const readReceiptFilters = () => {
    if (typeof window === 'undefined') return null;
    try {
      const raw = window.localStorage.getItem(receiptFiltersKey);
      if (!raw) return null;
      const parsed = JSON.parse(raw) as Record<string, unknown>;
      return {
        search: typeof parsed.search === 'string' ? parsed.search : '',
        status: (['all', 'due', 'overdue', 'partial', 'paid'].includes(parsed.status as string)
          ? (parsed.status as 'all' | 'due' | 'overdue' | 'partial' | 'paid')
          : 'all') as 'all' | 'due' | 'overdue' | 'partial' | 'paid',
        month: typeof parsed.month === 'string' ? parsed.month : 'all',
        year: typeof parsed.year === 'string' ? parsed.year : 'all',
        sortKey: typeof parsed.sortKey === 'string' ? parsed.sortKey : 'due_date',
        sortDir: (parsed.sortDir === 'asc' || parsed.sortDir === 'desc'
          ? parsed.sortDir
          : 'desc') as 'asc' | 'desc',
      };
    } catch {
      return null;
    }
  };

  const [receiptSearch, setReceiptSearch] = useState<string>(() => readReceiptFilters()?.search ?? '');
  const [receiptStatus, setReceiptStatus] = useState<'all' | 'due' | 'overdue' | 'partial' | 'paid'>(
    () => readReceiptFilters()?.status ?? 'all'
  );
  const [receiptMonth, setReceiptMonth] = useState<string>(() => readReceiptFilters()?.month ?? 'all');
  const [receiptYear, setReceiptYear] = useState<string>(() => readReceiptFilters()?.year ?? 'all');
  const [receiptSort, setReceiptSort] = useState<SortState>(() => {
    const stored = readReceiptFilters();
    return { key: stored?.sortKey ?? 'due_date', direction: stored?.sortDir ?? 'desc' };
  });
  const [selectedReceipts, setSelectedReceipts] = useState<Set<string>>(new Set());

  const [tenantSort, setTenantSort] = useState<SortState>({ key: 'name', direction: 'asc' });
  const [propertySort, setPropertySort] = useState<SortState>({ key: 'name', direction: 'asc' });
  const [leaseSort, setLeaseSort] = useState<SortState>({ key: 'tenant_name', direction: 'asc' });
  const [tenantPage, setTenantPage] = useState(1);
  const [propertyPage, setPropertyPage] = useState(1);
  const [leasePage, setLeasePage] = useState(1);
  const [receiptPage, setReceiptPage] = useState(1);

  // Tamanho de página por entidade, persistido no localStorage.
  const readPageSize = (entity: string): number => {
    if (typeof window === 'undefined') return 10;
    try {
      const stored = Number(window.localStorage.getItem(`aluxen:pageSize:${entity}`));
      return [10, 25, 50, 100].includes(stored) ? stored : 10;
    } catch {
      return 10;
    }
  };

  const [tenantPageSize, setTenantPageSize] = useState<number>(() => readPageSize('tenants'));
  const [propertyPageSize, setPropertyPageSize] = useState<number>(() => readPageSize('properties'));
  const [leasePageSize, setLeasePageSize] = useState<number>(() => readPageSize('leases'));
  const [receiptPageSize, setReceiptPageSize] = useState<number>(() => readPageSize('receipts'));

  useEffect(() => {
    try { window.localStorage.setItem('aluxen:pageSize:tenants', String(tenantPageSize)); } catch {}
  }, [tenantPageSize]);
  useEffect(() => {
    try { window.localStorage.setItem('aluxen:pageSize:properties', String(propertyPageSize)); } catch {}
  }, [propertyPageSize]);
  useEffect(() => {
    try { window.localStorage.setItem('aluxen:pageSize:leases', String(leasePageSize)); } catch {}
  }, [leasePageSize]);
  useEffect(() => {
    try { window.localStorage.setItem('aluxen:pageSize:receipts', String(receiptPageSize)); } catch {}
  }, [receiptPageSize]);

  const [isOnline, setIsOnline] = useState(() => (typeof navigator === 'undefined' ? true : navigator.onLine));
  const [pendingSyncCount, setPendingSyncCount] = useState(0);
  const [pendingIds, setPendingIds] = useState<PendingIds>({
    tenants: new Set(),
    properties: new Set(),
    leases: new Set(),
    receipts: new Set(),
  });
  const [initialLoading, setInitialLoading] = useState(false);
  const activeTenantSet = useMemo(() => new Set(initialData.activeTenantIds), [initialData.activeTenantIds]);

  const [busy, setBusy] = useState(false);
  const syncInFlight = useRef(false);
  const sendPollRef = useRef<{ cancelled: boolean } | null>(null);
  const [whatsappModal, setWhatsappModal] = useState<{
    open: boolean;
    loading: boolean;
    type: 'receipt' | 'lease';
    id: string;
    phone: string;
    formattedPhone: string;
    tenantName: string;
    propertyName: string;
    message: string;
    directUrl: string;
    sending: boolean;
    sendingStage?: SendStage;
    error?: string;
    success?: string;
  } | null>(null);
  const [batchPayConfirm, setBatchPayConfirm] = useState(false);
  const [batchDeleteConfirm, setBatchDeleteConfirm] = useState(false);
  const [connectWhatsappOpen, setConnectWhatsappOpen] = useState(false);
  const [whatsappStatus, setWhatsappStatus] = useState<WhatsAppStatus | null>(null);

  const loadWhatsAppStatus = React.useCallback(async () => {
    try {
      const res = await fetch('/api/whatsapp/status', { credentials: 'same-origin' });
      const data = (await res.json()) as WhatsAppStatus;
      setWhatsappStatus(data);
    } catch {}
  }, []);

  useEffect(() => {
    void loadWhatsAppStatus();
  }, [loadWhatsAppStatus]);

  useEffect(() => () => stopSendPolling(), []);

  const openWhatsAppModal = async (type: 'receipt' | 'lease', id: string) => {
    stopSendPolling();
    void loadWhatsAppStatus();
    setWhatsappModal({
      open: true,
      loading: true,
      type,
      id,
      phone: '',
      formattedPhone: '',
      tenantName: '',
      propertyName: '',
      message: '',
      directUrl: '',
      sending: false,
    });

    try {
      const formData = new FormData();
      formData.set('type', type);
      formData.set('id', id);

      const res = await fetch('/api/whatsapp/preview', {
        method: 'POST',
        body: formData,
        credentials: 'same-origin',
      });
      const data = (await res.json()) as {
        ok?: boolean;
        phone?: string;
        formattedPhone?: string;
        tenantName?: string;
        propertyName?: string;
        message?: string;
        directUrl?: string;
        error?: string;
      };

      if (data.ok) {
        setWhatsappModal((prev) =>
          prev
            ? {
                ...prev,
                loading: false,
                phone: data.phone || '',
                formattedPhone: data.formattedPhone || '',
                tenantName: data.tenantName || '',
                propertyName: data.propertyName || '',
                message: data.message || '',
                directUrl: data.directUrl || '',
              }
            : null
        );
      } else {
        setWhatsappModal((prev) =>
          prev
            ? {
                ...prev,
                loading: false,
                error: data.error || 'Não foi possível carregar prévia.',
              }
            : null
        );
      }
    } catch {
      setWhatsappModal((prev) =>
        prev
          ? {
              ...prev,
              loading: false,
              error: 'Erro de conexão ao carregar dados do WhatsApp.',
            }
          : null
      );
    }
  };

  const stopSendPolling = () => {
    if (sendPollRef.current) sendPollRef.current.cancelled = true;
    sendPollRef.current = null;
  };

  const whatsappSendStepIndex =
    whatsappModal?.sending && whatsappModal.sendingStage
      ? WHATSAPP_SEND_STEPS.findIndex((step) => step.stage === whatsappModal.sendingStage)
      : -1;

  const failWhatsAppSend = (rawMessage: string) => {
    stopSendPolling();
    const raw = rawMessage.toLowerCase();
    const isLoginErr =
      !raw.includes('chromium') &&
      (raw.includes('expirad') ||
        raw.includes('autenticad') ||
        raw.includes('whatsapp:login') ||
        raw.includes('nenhum') ||
        raw.includes('encontrado'));
    setWhatsappModal((prev) =>
      prev
        ? { ...prev, sending: false, sendingStage: undefined, error: friendlyWhatsAppError(rawMessage) }
        : null
    );
    if (isLoginErr) {
      setConnectWhatsappOpen(true);
    }
  };

  const sendAutomatedWhatsApp = async () => {
    if (!whatsappModal) return;
    if (whatsappStatus && !whatsappStatus.isLoggedIn) {
      setConnectWhatsappOpen(true);
      return;
    }

    stopSendPolling();
    setWhatsappModal((prev) =>
      prev
        ? { ...prev, sending: true, sendingStage: 'preparing', error: undefined, success: undefined }
        : null
    );

    let jobId = '';
    try {
      const formData = new FormData();
      formData.set('to', whatsappModal.phone);
      formData.set('message', whatsappModal.message);

      const res = await fetch('/api/whatsapp/send', {
        method: 'POST',
        body: formData,
        credentials: 'same-origin',
      });
      const data = (await res.json()) as { ok?: boolean; jobId?: string; error?: string };

      if (!data.ok || !data.jobId) {
        failWhatsAppSend(data.error || 'Não foi possível iniciar o envio.');
        return;
      }
      jobId = data.jobId;
    } catch {
      failWhatsAppSend('Erro de comunicação com o servidor.');
      return;
    }

    const poll = { cancelled: false };
    sendPollRef.current = poll;
    const deadline = Date.now() + 4 * 60 * 1000;

    while (!poll.cancelled && Date.now() < deadline) {
      await delay(1200);
      if (poll.cancelled) return;

      let job: SendJob | undefined;
      try {
        const statusRes = await fetch(
          `/api/whatsapp/send/status?jobId=${encodeURIComponent(jobId)}`,
          { credentials: 'same-origin' }
        );
        const statusData = (await statusRes.json()) as { ok?: boolean; job?: SendJob };
        job = statusData.job;
      } catch {
        // Oscilação momentânea de rede: mantém o acompanhamento.
        continue;
      }
      if (poll.cancelled) return;
      if (!job) continue;

      if (job.status === 'done') {
        sendPollRef.current = null;
        setWhatsappModal((prev) =>
          prev
            ? {
                ...prev,
                sending: false,
                sendingStage: undefined,
                success: 'Mensagem enviada com sucesso pelo WhatsApp!',
              }
            : null
        );
        return;
      }

      if (job.status === 'error') {
        failWhatsAppSend(job.error || 'Falha ao enviar mensagem.');
        return;
      }

      const stage = job.stage;
      setWhatsappModal((prev) => (prev ? { ...prev, sendingStage: stage } : null));
    }

    if (!poll.cancelled) {
      failWhatsAppSend(
        'O envio está demorando mais que o esperado. Confira a conversa no WhatsApp antes de tentar novamente.'
      );
    }
  };

  const reloadData = async () => {
    try {
      const res = await fetch('/dashboard?json=true');
      if (res.ok) {
        const data = await res.json() as DashboardProps;
        setTenantItems(data.tenants);
        setPropertyItems(data.properties);
        setLeaseItems(data.leases);
        setReceiptItems(data.receipts);
      }
    } catch {}
  };

  function queueOfflineMutation(payload: QueuePayload, successMessage: string) {
    const mutationPayload = { ...payload };
    if (isCreateIntent(mutationPayload.intent || '') && !mutationPayload.id) {
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
      await reloadData();
      setToast({ message: 'Sincronização concluída com o servidor.', tone: 'success' });
    } catch (error) {
      const queueHead = readOfflineQueue()[0];
      setToast({ message: getSyncErrorMessage(queueHead?.payload || {}, error), tone: 'error' });
    } finally {
      syncInFlight.current = false;
    }
  }

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

  const sendMutation = async (formData: FormData, successMsg: string, onSuccess: () => void) => {
    setBusy(true);
    try {
      const res = await fetch('/dashboard', {
        method: 'POST',
        body: formData,
        credentials: 'same-origin',
      });
      const data = await res.json() as MutationResponse;
      if (data.ok) {
        setToast({ message: successMsg, tone: 'success' });
        onSuccess();
        await reloadData();
      } else {
        setToast({ message: data.error || 'Erro ao processar.', tone: 'error' });
      }
    } catch {
      setToast({ message: 'Erro de conexão.', tone: 'error' });
    } finally {
      setBusy(false);
    }
  };

  const filteredTenants = useMemo(
    () =>
      tenantItems.filter((item) =>
        `${item.name} ${item.email} ${item.phone || ''} ${item.cpf || ''}`
          .toLowerCase()
          .includes(tenantSearch.toLowerCase())
      ),
    [tenantItems, tenantSearch]
  );

  const filteredProperties = useMemo(
    () =>
      propertyItems.filter((item) =>
        `${item.name} ${item.city} ${item.state} ${item.address}`
          .toLowerCase()
          .includes(propertySearch.toLowerCase())
      ),
    [propertyItems, propertySearch]
  );

  const filteredLeases = useMemo(
    () =>
      leaseItems.filter((item) => {
        if (leaseStatus === 'active' && item.status !== 'active') return false;
        if (leaseStatus === 'suspended' && item.status !== 'suspended') return false;
        if (leaseStatus === 'season' && item.status !== 'season') return false;
        if (leaseStatus === 'terminated' && item.status !== 'terminated') return false;
        return `${item.tenant_name} ${item.property_name} ${item.start_date} ${item.end_date}`
          .toLowerCase()
          .includes(leaseSearch.toLowerCase());
      }),
    [leaseItems, leaseSearch, leaseStatus]
  );

  const filteredReceipts = useMemo(
    () =>
      receiptItems.filter((item) => {
        const matchesText = `${item.tenant_name} ${item.property_name} ${item.due_date} ${item.amount}`
          .toLowerCase()
          .includes(receiptSearch.toLowerCase());
        const matchesStatus = receiptStatus === 'all' || getReceiptStatus(item) === receiptStatus;
        const itemMonth = item.due_date.slice(5, 7);
        const itemYear = item.due_date.slice(0, 4);
        const matchesPeriod =
          (receiptMonth === 'all' && receiptYear === 'all') ||
          (receiptMonth !== 'all' && receiptYear !== 'all' && itemMonth === receiptMonth && itemYear === receiptYear) ||
          (receiptMonth !== 'all' && receiptYear === 'all' && itemMonth === receiptMonth) ||
          (receiptMonth === 'all' && receiptYear !== 'all' && itemYear === receiptYear);
        return matchesText && matchesStatus && matchesPeriod;
      }),
    [receiptItems, receiptSearch, receiptStatus, receiptMonth, receiptYear]
  );

  const sortedTenants = useMemo(() => sortItems(filteredTenants, tenantSort), [filteredTenants, tenantSort]);
  const sortedProperties = useMemo(() => sortItems(filteredProperties, propertySort), [filteredProperties, propertySort]);
  const sortedLeases = useMemo(() => sortItems(filteredLeases, leaseSort), [filteredLeases, leaseSort]);
  const sortedReceipts = useMemo(() => sortItems(filteredReceipts, receiptSort), [filteredReceipts, receiptSort]);

  const tenantTotalPages = Math.max(1, Math.ceil(sortedTenants.length / tenantPageSize));
  const propertyTotalPages = Math.max(1, Math.ceil(sortedProperties.length / propertyPageSize));
  const leaseTotalPages = Math.max(1, Math.ceil(sortedLeases.length / leasePageSize));
  const receiptTotalPages = Math.max(1, Math.ceil(sortedReceipts.length / receiptPageSize));

  const pagedTenants = useMemo(() => paginateItems(sortedTenants, tenantPage, tenantPageSize), [sortedTenants, tenantPage, tenantPageSize]);
  const pagedProperties = useMemo(() => paginateItems(sortedProperties, propertyPage, propertyPageSize), [sortedProperties, propertyPage, propertyPageSize]);
  const pagedLeases = useMemo(() => paginateItems(sortedLeases, leasePage, leasePageSize), [sortedLeases, leasePage, leasePageSize]);
  const pagedReceipts = useMemo(() => paginateItems(sortedReceipts, receiptPage, receiptPageSize), [sortedReceipts, receiptPage, receiptPageSize]);

  useEffect(() => setTenantPage(1), [tenantSearch, tenantSort]);
  useEffect(() => setPropertyPage(1), [propertySearch, propertySort]);
  useEffect(() => setLeasePage(1), [leaseSearch, leaseSort, leaseStatus]);
  useEffect(() => setReceiptPage(1), [receiptSearch, receiptStatus, receiptSort, receiptMonth, receiptYear]);

  useEffect(() => setTenantPage((page) => Math.min(page, tenantTotalPages)), [tenantTotalPages]);
  useEffect(() => setPropertyPage((page) => Math.min(page, propertyTotalPages)), [propertyTotalPages]);
  useEffect(() => setLeasePage((page) => Math.min(page, leaseTotalPages)), [leaseTotalPages]);
  useEffect(() => setReceiptPage((page) => Math.min(page, receiptTotalPages)), [receiptTotalPages]);

  // Persistir filtros + ordenação de Recibos no localStorage sempre que mudarem.
  useEffect(() => {
    if (typeof window === 'undefined') return;
    try {
      window.localStorage.setItem(
        receiptFiltersKey,
        JSON.stringify({
          search: receiptSearch,
          status: receiptStatus,
          month: receiptMonth,
          year: receiptYear,
          sortKey: receiptSort.key,
          sortDir: receiptSort.direction,
        })
      );
    } catch {
      /* ignore quota / privacy errors */
    }
  }, [receiptFiltersKey, receiptSearch, receiptStatus, receiptMonth, receiptYear, receiptSort]);

  function settleAfterMutation() {
    if (window.navigator.onLine) {
      void reloadData();
    }
  }

  function buildOfflinePayload(form: HTMLFormElement) {
    const payload = serializeFormData(new FormData(form));
    if (isCreateIntent(payload.intent || '') && !payload.id) {
      payload.id = createId();
    }
    return payload;
  }

  function handleOfflineSubmit(
    event: React.FormEvent<HTMLFormElement>,
    optimisticUpdate: (payload: QueuePayload) => void,
    successMessage: string,
    reset?: () => void
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

    const activeLeases = leaseItems.filter(
      (lease) => lease.status === 'active' && lease.start_date <= monthEnd && lease.end_date >= monthStart
    );
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
        tenant_id: lease.tenant_id,
        property_id: lease.property_id,
        category: 'rent',
        description: null,
        due_date: `${monthKey}-${String(dueDay).padStart(2, '0')}`,
        paid_at: null,
        amount: lease.rent_amount,
        paid_amount: null,
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
    setPropertyDraft({
      id: item.id,
      name: item.name,
      address: item.address,
      city: item.city,
      state: item.state,
      zip: item.zip || '',
    });
    setModalType('properties');
  }

  function startLeaseEdit(item: Lease) {
    setLeaseDraft({
      id: item.id,
      tenant_id: item.tenant_id,
      property_id: item.property_id,
      start_date: item.start_date,
      end_date: item.end_date,
      due_day: String(item.due_day || '1'),
      rent_amount: item.rent_amount,
      status: item.status || 'active',
    });
    setModalType('leases');
  }

  function startReceiptEdit(item: Receipt) {
    setReceiptDraft({
      id: item.id,
      lease_id: item.lease_id || '',
      due_date: item.due_date,
      paid_at: item.paid_at ? item.paid_at.slice(0, 16) : '',
      amount: item.amount,
    });
    setModalType('receipts');
  }

  function startReceiptPay(item: Receipt) {
    const now = new Date();
    const localDT = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}T${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`;
    const remaining = Math.max(0, Number(item.amount) - (Number(item.paid_amount) || 0));
    setPayDraft({
      id: item.id,
      amount: item.amount,
      pay_amount: String(remaining || item.amount),
      paid_at: localDT,
    });
    setPayModalOpen(true);
  }

  function receiptLeaseMeta(leaseId: string) {
    return leaseItems.find((item) => item.id === leaseId);
  }

  function toggleSort(current: SortState, key: string, setter: (sort: SortState) => void) {
    setter({ key, direction: current.key === key && current.direction === 'asc' ? 'desc' : 'asc' });
  }

  return (
    <div className="app-shell">
      <button
        className="hamburger-float"
        type="button"
        onClick={() => setMobileMenuOpen((v) => !v)}
        aria-label={mobileMenuOpen ? 'Fechar menu' : 'Abrir menu'}
      >
        {mobileMenuOpen ? (
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round">
            <path d="M18 6L6 18M6 6l12 12" />
          </svg>
        ) : (
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round">
            <path d="M3 12h18M3 6h18M3 18h18" />
          </svg>
        )}
      </button>
      {mobileMenuOpen && <div className="sidebar-backdrop" onClick={() => setMobileMenuOpen(false)} />}

      <nav className={`sidebar${!collapsed || mobileMenuOpen ? ' open' : ''}${mobileMenuOpen ? ' mobile-open' : ''}`}>
        <Brand collapsed={collapsed && !mobileMenuOpen} />
        <button
          className="menu-btn"
          type="button"
          onClick={() => setCollapsed((value) => !value)}
          aria-label={collapsed ? 'Expandir menu' : 'Recolher menu'}
        >
          <MenuGlyph collapsed={collapsed} />
        </button>
        <ul>
          <li>
            <a
              href="/dashboard?tab=dashboard"
              className={tab === 'dashboard' ? 'active' : ''}
              onClick={(e) => {
                setMobileMenuOpen(false);
                switchTab('dashboard', e);
              }}
            >
              <span className="nav-icon"><IconDashboard /></span>
              {!collapsed || mobileMenuOpen ? <span className="nav-label">Dashboard</span> : null}
            </a>
          </li>
          <li>
            <a
              href="/dashboard?tab=tenants"
              className={tab === 'tenants' ? 'active' : ''}
              onClick={(e) => {
                setMobileMenuOpen(false);
                switchTab('tenants', e);
              }}
            >
              <span className="nav-icon"><IconTenants /></span>
              {!collapsed || mobileMenuOpen ? <span className="nav-label">Inquilinos</span> : null}
            </a>
          </li>
          <li>
            <a
              href="/dashboard?tab=properties"
              className={tab === 'properties' ? 'active' : ''}
              onClick={(e) => {
                setMobileMenuOpen(false);
                switchTab('properties', e);
              }}
            >
              <span className="nav-icon"><IconProperties /></span>
              {!collapsed || mobileMenuOpen ? <span className="nav-label">Imóveis</span> : null}
            </a>
          </li>
          <li>
            <a
              href="/dashboard?tab=leases"
              className={tab === 'leases' ? 'active' : ''}
              onClick={(e) => {
                setMobileMenuOpen(false);
                switchTab('leases', e);
              }}
            >
              <span className="nav-icon"><IconLeases /></span>
              {!collapsed || mobileMenuOpen ? <span className="nav-label">Contratos</span> : null}
            </a>
          </li>
          <li>
            <a
              href="/dashboard?tab=receipts"
              className={tab === 'receipts' ? 'active' : ''}
              onClick={(e) => {
                setMobileMenuOpen(false);
                switchTab('receipts', e);
              }}
            >
              <span className="nav-icon"><IconReceipts /></span>
              {!collapsed || mobileMenuOpen ? <span className="nav-label">Pagamentos</span> : null}
            </a>
          </li>
        </ul>
        <div className="sidebar-footer">
          <DataButtons collapsed={collapsed && !mobileMenuOpen} />
          <ThemeToggle collapsed={collapsed && !mobileMenuOpen} />
          <a href="/settings" className="nav-link">
            <span className="nav-icon"><IconSettings /></span>
            {!collapsed || mobileMenuOpen ? <span className="nav-label">Configurações</span> : null}
          </a>
          <form action="/logout" method="post">
            <button type="submit">
              <span className="nav-icon"><IconLogout /></span>
              {!collapsed || mobileMenuOpen ? <span className="nav-label">Sair</span> : null}
            </button>
          </form>
        </div>
      </nav>

      <main className={collapsed ? '' : 'sidebar-open'}>
        <div className="page">
          <div className="page-header">
            <div className="page-header-text">
              <h1>
                {tab === 'dashboard'
                  ? 'Dashboard'
                  : tab === 'tenants'
                  ? 'Inquilinos'
                  : tab === 'properties'
                  ? 'Imóveis'
                  : tab === 'leases'
                  ? 'Contratos de Locação'
                  : 'Pagamentos e Recibos'}
              </h1>
              <p className="subtitle">
                Aluxen &bull; Sessão ativa para <strong>{user}</strong>
              </p>
            </div>
            <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center' }}>
              <span className="status-badge paid" style={{ fontSize: '0.8125rem', padding: '0.4rem 0.8rem' }}>
                {tab === 'dashboard'
                  ? 'Visão Geral'
                  : tab === 'tenants'
                  ? `${tenantItems.length} inquilino(s)`
                  : tab === 'properties'
                  ? `${propertyItems.length} imóvel(is)`
                  : tab === 'leases'
                  ? (() => {
                      const act = leaseItems.filter((l) => l.status === 'active').length;
                      const sus = leaseItems.filter((l) => l.status === 'suspended').length;
                      const sea = leaseItems.filter((l) => l.status === 'season').length;
                      const parts = [];
                      if (act) parts.push(`${act} ativo(s)`);
                      if (sus) parts.push(`${sus} suspenso(s)`);
                      if (sea) parts.push(`${sea} temporada(s)`);
                      return parts.join(' • ') || `${leaseItems.length} contrato(s)`;
                    })()
                  : `${receiptItems.length} recibo(s)`}
              </span>
            </div>
          </div>
          <SyncBanner isOnline={isOnline} pendingCount={pendingSyncCount} />
          {toast ? <Toast message={toast.message} tone={toast.tone} onClose={() => setToast(null)} /> : null}

          {tab === 'dashboard' ? <MetricsView initialMetrics={initialData.metrics} /> : null}

          {tab === 'tenants' ? (
            <section className="card stack-lg">
              <div className="section-header">
                <h2>Gerenciar Inquilinos</h2>
                <button type="button" className="btn btn-primary" onClick={() => setCreateModalType('tenants')}>
                  Novo inquilino
                </button>
              </div>
              <FilterBar value={tenantSearch} onChange={setTenantSearch} placeholder="Buscar por nome, email ou telefone" />
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>
                        <SortHeader
                          label="Nome"
                          active={tenantSort.key === 'name'}
                          direction={tenantSort.direction}
                          onClick={() => toggleSort(tenantSort, 'name', setTenantSort)}
                        />
                      </th>
                      <th>
                        <SortHeader
                          label="Email"
                          active={tenantSort.key === 'email'}
                          direction={tenantSort.direction}
                          onClick={() => toggleSort(tenantSort, 'email', setTenantSort)}
                        />
                      </th>
                      <th>
                        <SortHeader
                          label="CPF"
                          active={tenantSort.key === 'cpf'}
                          direction={tenantSort.direction}
                          onClick={() => toggleSort(tenantSort, 'cpf', setTenantSort)}
                        />
                      </th>
                      <th>
                        <SortHeader
                          label="Telefone"
                          active={tenantSort.key === 'phone'}
                          direction={tenantSort.direction}
                          onClick={() => toggleSort(tenantSort, 'phone', setTenantSort)}
                        />
                      </th>
                      <th>Ações</th>
                    </tr>
                  </thead>
                  <tbody>
                    {initialLoading ? (
                      <LoadingRow colSpan={5} />
                    ) : pagedTenants.length ? (
                      pagedTenants.map((tenant) => (
                        <tr key={tenant.id}>
                          <td data-label="Nome">
                            {tenant.name} {pendingIds.tenants.has(tenant.id) ? <PendingBadge /> : null}{' '}
                            {activeTenantSet.has(tenant.id) ? (
                              <span className="status-badge paid" style={{ fontSize: '0.7rem', marginLeft: '0.35rem' }}>
                                Em contrato
                              </span>
                            ) : null}
                          </td>
                          <td data-label="Email">{tenant.email}</td>
                          <td data-label="CPF">{tenant.cpf || '-'}</td>
                          <td data-label="Telefone">{tenant.phone || '-'}</td>
                          <td className="row-actions">
                            <button
                              type="button"
                              className="btn btn-sm btn-secondary"
                              onClick={() => startTenantEdit(tenant)}
                            >
                              Editar
                            </button>
                            <DeleteRowButton
                              intent="tenant.delete"
                              tab="tenants"
                              id={tenant.id}
                              onOptimisticDelete={() =>
                                setTenantItems((current) => current.filter((row) => row.id !== tenant.id))
                              }
                              onSettled={settleAfterMutation}
                              queueOfflineMutation={queueOfflineMutation}
                              disabled={activeTenantSet.has(tenant.id)}
                              disabledTitle="Possui contrato ativo"
                            />
                          </td>
                        </tr>
                      ))
                    ) : (
                      <tr>
                        <td colSpan={5}>Nenhum inquilino cadastrado.</td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
              <Pagination
                page={tenantPage}
                totalPages={tenantTotalPages}
                onChange={setTenantPage}
                pageSize={tenantPageSize}
                onPageSizeChange={(n) => {
                  setTenantPageSize(n);
                  setTenantPage(1);
                }}
              />
            </section>
          ) : null}

          {tab === 'properties' ? (
            <section className="card stack-lg">
              <div className="section-header">
                <h2>Gerenciar Imóveis</h2>
                <button type="button" className="btn btn-primary" onClick={() => setCreateModalType('properties')}>
                  Novo imóvel
                </button>
              </div>
              <FilterBar
                value={propertySearch}
                onChange={setPropertySearch}
                placeholder="Buscar por nome, cidade, estado ou endereço"
              />
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>
                        <SortHeader
                          label="Nome"
                          active={propertySort.key === 'name'}
                          direction={propertySort.direction}
                          onClick={() => toggleSort(propertySort, 'name', setPropertySort)}
                        />
                      </th>
                      <th>
                        <SortHeader
                          label="Cidade"
                          active={propertySort.key === 'city'}
                          direction={propertySort.direction}
                          onClick={() => toggleSort(propertySort, 'city', setPropertySort)}
                        />
                      </th>
                      <th>
                        <SortHeader
                          label="Estado"
                          active={propertySort.key === 'state'}
                          direction={propertySort.direction}
                          onClick={() => toggleSort(propertySort, 'state', setPropertySort)}
                        />
                      </th>
                      <th>Ações</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pagedProperties.length ? (
                      pagedProperties.map((property) => (
                        <tr key={property.id}>
                          <td data-label="Nome">
                            {property.name} {pendingIds.properties.has(property.id) ? <PendingBadge /> : null}
                          </td>
                          <td data-label="Cidade">{property.city}</td>
                          <td data-label="Estado">{property.state}</td>
                          <td className="row-actions">
                            <button
                              type="button"
                              className="btn btn-sm btn-secondary"
                              onClick={() => startPropertyEdit(property)}
                            >
                              Editar
                            </button>
                            <DeleteRowButton
                              intent="property.delete"
                              tab="properties"
                              id={property.id}
                              onOptimisticDelete={() =>
                                setPropertyItems((current) => current.filter((row) => row.id !== property.id))
                              }
                              onSettled={settleAfterMutation}
                              queueOfflineMutation={queueOfflineMutation}
                            />
                          </td>
                        </tr>
                      ))
                    ) : (
                      <tr>
                        <td colSpan={4}>Nenhum imóvel cadastrado.</td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
              <Pagination
                page={propertyPage}
                totalPages={propertyTotalPages}
                onChange={setPropertyPage}
                pageSize={propertyPageSize}
                onPageSizeChange={(n) => {
                  setPropertyPageSize(n);
                  setPropertyPage(1);
                }}
              />
            </section>
          ) : null}

          {tab === 'leases' ? (
            <section className="card stack-lg">
              <div className="section-header">
                <h2>Gerenciar Contratos</h2>
                <button type="button" className="btn btn-primary" onClick={() => setCreateModalType('leases')}>
                  Novo contrato
                </button>
              </div>
              <FilterBar value={leaseSearch} onChange={setLeaseSearch} placeholder="Buscar por inquilino, imóvel ou período">
                <SmallSelect value={leaseStatus} onChange={(v) => setLeaseStatus(v as 'all' | 'active' | 'suspended' | 'season' | 'terminated')}>
                  <option value="all">Todos</option>
                  <option value="active">Ativos</option>
                  <option value="suspended">Suspensos</option>
                  <option value="season">Temporada</option>
                  <option value="terminated">Encerrados</option>
                </SmallSelect>
              </FilterBar>
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>
                        <SortHeader
                          label="Inquilino"
                          active={leaseSort.key === 'tenant_name'}
                          direction={leaseSort.direction}
                          onClick={() => toggleSort(leaseSort, 'tenant_name', setLeaseSort)}
                        />
                      </th>
                      <th>
                        <SortHeader
                          label="Imóvel"
                          active={leaseSort.key === 'property_name'}
                          direction={leaseSort.direction}
                          onClick={() => toggleSort(leaseSort, 'property_name', setLeaseSort)}
                        />
                      </th>
                      <th>Status</th>
                      <th>
                        <SortHeader
                          label="Início"
                          active={leaseSort.key === 'start_date'}
                          direction={leaseSort.direction}
                          onClick={() => toggleSort(leaseSort, 'start_date', setLeaseSort)}
                        />
                      </th>
                      <th>
                        <SortHeader
                          label="Vencimento"
                          active={leaseSort.key === 'due_day'}
                          direction={leaseSort.direction}
                          onClick={() => toggleSort(leaseSort, 'due_day', setLeaseSort)}
                        />
                      </th>
                      <th>
                        <SortHeader
                          label="Aluguel"
                          active={leaseSort.key === 'rent_amount'}
                          direction={leaseSort.direction}
                          onClick={() => toggleSort(leaseSort, 'rent_amount', setLeaseSort)}
                        />
                      </th>
                      <th>PDF</th>
                      <th>Ações</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pagedLeases.length ? (
                      pagedLeases.map((lease) => (
                        <tr key={lease.id} style={lease.status === 'terminated' ? { opacity: 0.55 } : undefined}>
                          <td data-label="Inquilino">
                            {lease.tenant_name} {pendingIds.leases.has(lease.id) ? <PendingBadge /> : null}
                          </td>
                          <td data-label="Imóvel">{lease.property_name}</td>
                          <td data-label="Status">
                            <span className={`status-badge ${lease.status === 'active' ? 'paid' : lease.status === 'suspended' ? 'due' : lease.status === 'season' ? 'partial' : 'overdue'}`}>
                              {lease.status === 'active' ? 'Ativo' : lease.status === 'suspended' ? 'Suspenso' : lease.status === 'season' ? 'Temporada' : 'Encerrado'}
                            </span>
                          </td>
                          <td data-label="Período">
                            {lease.start_date} a {lease.end_date}
                          </td>
                          <td data-label="Vencimento">Dia {lease.due_day}</td>
                          <td data-label="Valor">{formatCurrency(lease.rent_amount)}</td>
                          <td data-label="PDF">
                            <div className="row-actions">
                              <a className="btn btn-sm btn-secondary" href={`/contract-pdf?id=${lease.id}`} target="_blank" rel="noreferrer">
                                Contrato
                              </a>
                              <button
                                type="button"
                                className="btn btn-sm btn-outline"
                                style={{ color: 'var(--primary)', borderColor: 'var(--primary-tint-border)' }}
                                onClick={() => openWhatsAppModal('lease', lease.id)}
                                title="Enviar contrato por WhatsApp"
                              >
                                <span className="nav-icon" style={{ width: '13px', height: '13px' }}><IconWhatsApp /></span>
                                WhatsApp
                              </button>
                            </div>
                          </td>
                          <td className="row-actions">
                            <button type="button" className="btn btn-sm btn-secondary" onClick={() => startLeaseEdit(lease)}>
                              Editar
                            </button>
                            {lease.status === 'active' ? (
                              <DeleteRowButton
                                intent="lease.terminate"
                                tab="leases"
                                id={lease.id}
                                onOptimisticDelete={() =>
                                  setLeaseItems((current) =>
                                    current.map((row) => (row.id === lease.id ? { ...row, status: 'terminated' } : row))
                                  )
                                }
                                onSettled={settleAfterMutation}
                                queueOfflineMutation={queueOfflineMutation}
                                btnLabel="Encerrar"
                                btnClass="btn btn-sm btn-outline"
                                confirmTitle="Encerrar contrato"
                                confirmLabel="Encerrar"
                                confirmMessage="Encerrar este contrato? Ele ficará inativo."
                              />
                            ) : lease.status === 'season' ? (
                              <DeleteRowButton
                                intent="lease.terminate"
                                tab="leases"
                                id={lease.id}
                                onOptimisticDelete={() =>
                                  setLeaseItems((current) =>
                                    current.map((row) => (row.id === lease.id ? { ...row, status: 'terminated' } : row))
                                  )
                                }
                                onSettled={settleAfterMutation}
                                queueOfflineMutation={queueOfflineMutation}
                                btnLabel="Encerrar temporada"
                                btnClass="btn btn-sm btn-outline"
                                confirmTitle="Encerrar temporada"
                                confirmLabel="Encerrar"
                                confirmMessage="Encerrar esta temporada?"
                              />
                            ) : lease.status === 'suspended' ? (
                              <>
                                <DeleteRowButton
                                  intent="lease.reactivate"
                                  tab="leases"
                                  id={lease.id}
                                  onOptimisticDelete={() =>
                                    setLeaseItems((current) =>
                                      current.map((row) => (row.id === lease.id ? { ...row, status: 'active' } : row))
                                    )
                                  }
                                  onSettled={settleAfterMutation}
                                  queueOfflineMutation={queueOfflineMutation}
                                  btnLabel="Ativar"
                                  btnClass="btn btn-sm btn-secondary"
                                  confirmTitle="Ativar contrato"
                                  confirmLabel="Ativar"
                                  confirmTone="primary"
                                  confirmMessage="Tornar este contrato ativo novamente?"
                                />
                                <DeleteRowButton
                                  intent="lease.terminate"
                                  tab="leases"
                                  id={lease.id}
                                  onOptimisticDelete={() =>
                                    setLeaseItems((current) =>
                                      current.map((row) => (row.id === lease.id ? { ...row, status: 'terminated' } : row))
                                    )
                                  }
                                  onSettled={settleAfterMutation}
                                  queueOfflineMutation={queueOfflineMutation}
                                  btnLabel="Encerrar"
                                  btnClass="btn btn-sm btn-outline"
                                  confirmTitle="Encerrar contrato"
                                  confirmLabel="Encerrar"
                                  confirmMessage="Encerrar este contrato suspenso?"
                                />
                              </>
                            ) : (
                              <DeleteRowButton
                                intent="lease.reactivate"
                                tab="leases"
                                id={lease.id}
                                onOptimisticDelete={() =>
                                  setLeaseItems((current) =>
                                    current.map((row) => (row.id === lease.id ? { ...row, status: 'active' } : row))
                                  )
                                }
                                onSettled={settleAfterMutation}
                                queueOfflineMutation={queueOfflineMutation}
                                btnLabel="Reativar"
                                btnClass="btn btn-sm btn-secondary"
                                confirmTitle="Reativar contrato"
                                confirmLabel="Reativar"
                                confirmTone="primary"
                                confirmMessage="Reativar este contrato?"
                              />
                            )}
                            <span style={{ marginLeft: '0.25rem' }}>
                              <DeleteRowButton
                                intent="lease.delete"
                                tab="leases"
                                id={lease.id}
                                onOptimisticDelete={() =>
                                  setLeaseItems((current) => current.filter((row) => row.id !== lease.id))
                                }
                                onSettled={settleAfterMutation}
                                queueOfflineMutation={queueOfflineMutation}
                              />
                            </span>
                          </td>
                        </tr>
                      ))
                    ) : (
                      <tr>
                        <td colSpan={8}>Cadastre um inquilino e um imóvel para criar contratos.</td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
              <Pagination
                page={leasePage}
                totalPages={leaseTotalPages}
                onChange={setLeasePage}
                pageSize={leasePageSize}
                onPageSizeChange={(n) => {
                  setLeasePageSize(n);
                  setLeasePage(1);
                }}
              />
            </section>
          ) : null}

          {tab === 'receipts' ? (
            <section className="card stack-lg receipts-section">
              <div className="section-header">
                <h2>Pagamentos e Recibos</h2>
                <div className="auth-actions">
                  {selectedReceipts.size > 0 ? (
                    <>
                      {(() => {
                        const payableCount = pagedReceipts.filter(
                          (r) => selectedReceipts.has(r.id) && getReceiptStatus(r) !== 'paid'
                        ).length;
                        if (payableCount === 0) return null;
                        return (
                          <button
                            type="button"
                            className="btn btn-secondary"
                            disabled={busy}
                            onClick={() => setBatchPayConfirm(true)}
                          >
                            Pagar {payableCount} selecionado(s)
                          </button>
                        );
                      })()}
                      <button
                        type="button"
                        className="btn btn-danger"
                        disabled={busy}
                        onClick={() => setBatchDeleteConfirm(true)}
                      >
                        Excluir {selectedReceipts.size} selecionado(s)
                      </button>
                    </>
                  ) : null}
                  <button
                    type="button"
                    className="btn btn-secondary"
                    onClick={() => {
                      if (!window.navigator.onLine) {
                        applyOfflineMonthlyReceiptGeneration();
                        queueOfflineMutation({ intent: 'receipt.generate-monthly' }, 'Geração mensal colocada na fila de sincronização.');
                        return;
                      }
                      const formData = new FormData();
                      formData.set('intent', 'receipt.generate-monthly');
                      void sendMutation(formData, 'Pagamentos do mês gerados.', () => {});
                    }}
                  >
                    Gerar pagamentos
                  </button>
                  <button type="button" className="btn btn-primary" onClick={() => setCreateModalType('receipts')}>
                    Novo pagamento
                  </button>
                </div>
              </div>
              <FilterBar value={receiptSearch} onChange={setReceiptSearch} placeholder="Buscar por contrato, imóvel, vencimento ou valor">
                <SmallSelect value={receiptMonth} onChange={(v) => setReceiptMonth(v)}>
                  <option value="all">Todos</option>
                  {Array.from({ length: 12 }, (_, i) => String(i + 1).padStart(2, '0')).map((m) => (
                    <option key={m} value={m}>{m}</option>
                  ))}
                </SmallSelect>
                <SmallSelect value={receiptYear} onChange={(v) => setReceiptYear(v)}>
                  <option value="all">Todos</option>
                  {Array.from({ length: 5 }, (_, i) => String(new Date().getFullYear() - 2 + i)).map((y) => (
                    <option key={y} value={y}>{y}</option>
                  ))}
                </SmallSelect>
                <SmallSelect value={receiptStatus} onChange={(v) => setReceiptStatus(v as 'all' | 'due' | 'overdue' | 'partial' | 'paid')}>
                  <option value="all">Todos</option>
                  <option value="due">A vencer</option>
                  <option value="overdue">Vencido</option>
                  <option value="partial">Parcial</option>
                  <option value="paid">Pago</option>
                </SmallSelect>
              </FilterBar>
              <div className="table-wrap">
                <table className="receipts-table">
                  <thead>
                    <tr>
                      <th style={{ width: '2rem' }}>
                        <input
                          type="checkbox"
                          onChange={(e) => {
                            if (e.target.checked) {
                              setSelectedReceipts(new Set(pagedReceipts.map((r) => r.id)));
                            } else {
                              setSelectedReceipts(new Set());
                            }
                          }}
                          checked={
                            pagedReceipts.length > 0 &&
                            pagedReceipts.every((r) => (getReceiptStatus(r) !== 'paid' ? selectedReceipts.has(r.id) : true)) &&
                            selectedReceipts.size > 0
                          }
                        />
                      </th>
                      <th>
                        <SortHeader
                          label="Identificação / Contrato"
                          active={receiptSort.key === 'tenant_name'}
                          direction={receiptSort.direction}
                          onClick={() => toggleSort(receiptSort, 'tenant_name', setReceiptSort)}
                        />
                      </th>
                      <th>
                        <SortHeader
                          label="Status"
                          active={receiptSort.key === 'paid_at'}
                          direction={receiptSort.direction}
                          onClick={() => toggleSort(receiptSort, 'paid_at', setReceiptSort)}
                        />
                      </th>
                      <th>
                        <SortHeader
                          label="Vencimento"
                          active={receiptSort.key === 'due_date'}
                          direction={receiptSort.direction}
                          onClick={() => toggleSort(receiptSort, 'due_date', setReceiptSort)}
                        />
                      </th>
                      <th>
                        <SortHeader
                          label="Pagamento"
                          active={receiptSort.key === 'paid_at'}
                          direction={receiptSort.direction}
                          onClick={() => toggleSort(receiptSort, 'paid_at', setReceiptSort)}
                        />
                      </th>
                      <th>
                        <SortHeader
                          label="Valor"
                          active={receiptSort.key === 'amount'}
                          direction={receiptSort.direction}
                          onClick={() => toggleSort(receiptSort, 'amount', setReceiptSort)}
                        />
                      </th>
                      <th>PDF</th>
                      <th>Ações</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pagedReceipts.length ? (
                      pagedReceipts.map((receipt) => (
                        <tr key={receipt.id}>
                          <td data-label="Sel">
                            <input
                              type="checkbox"
                              checked={selectedReceipts.has(receipt.id)}

                              onChange={() => {
                                const next = new Set(selectedReceipts);
                                if (next.has(receipt.id)) next.delete(receipt.id);
                                else next.add(receipt.id);
                                setSelectedReceipts(next);
                              }}
                            />
                          </td>
                          <td data-label="Identificação" className="contract-cell">
                            {receipt.category === 'extra' || !receipt.lease_id ? (
                              <>
                                <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.35rem', flexWrap: 'wrap' }}>
                                  <span style={{ fontSize: '0.7rem', padding: '0.1rem 0.4rem', borderRadius: '4px', background: 'rgba(10, 179, 120, 0.15)', color: 'var(--primary)', fontWeight: 600 }}>Avulso</span>
                                  <strong>{receipt.description || 'Taxa Avulsa'}</strong>
                                  {pendingIds.receipts.has(receipt.id) ? <PendingBadge /> : null}
                                </span>
                                <span style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>
                                  {[receipt.tenant_name, receipt.property_name].filter(Boolean).join(' • ') || 'Sem vínculo'}
                                </span>
                              </>
                            ) : (
                              <>
                                <span>
                                  {receipt.tenant_name || '-'} {pendingIds.receipts.has(receipt.id) ? <PendingBadge /> : null}
                                </span>
                                <span>{receipt.property_name || '-'}</span>
                              </>
                            )}
                          </td>
                          <td data-label="Status">
                            <span className={`status-badge ${getReceiptStatus(receipt)}`}>
                              {getReceiptStatusLabel(getReceiptStatus(receipt))}
                            </span>
                          </td>
                          <td data-label="Vencimento">{formatDateBr(receipt.due_date)}</td>
                          <td data-label="Pagamento">{formatDateTimeBr(receipt.paid_at)}</td>
                          <td data-label="Valor">
                            <div>{formatCurrency(receipt.amount)}</div>
                            {getReceiptStatus(receipt) === 'partial' ? (
                              <div style={{ fontSize: '0.75rem', color: '#3b82f6', marginTop: '0.15rem' }}>
                                <span>Pago: {formatCurrency(receipt.paid_amount || 0)}</span>
                                <div style={{ color: 'var(--text-muted)' }}>
                                  Resta: {formatCurrency(Math.max(0, Number(receipt.amount) - (Number(receipt.paid_amount) || 0)))}
                                </div>
                              </div>
                            ) : null}
                          </td>
                          <td data-label="PDF">
                            <div className="row-actions">
                              {getReceiptStatus(receipt) === 'paid' || getReceiptStatus(receipt) === 'partial' ? (
                                <PdfDropdown id={receipt.id} />
                              ) : (
                                <span style={{ color: 'var(--text-muted)', fontSize: '0.85rem' }} title="Disponível após o pagamento">
                                  —
                                </span>
                              )}
                              {getReceiptStatus(receipt) === 'paid' || getReceiptStatus(receipt) === 'partial' ? (
                                <button
                                  type="button"
                                  className="btn btn-sm btn-outline"
                                  style={{ color: 'var(--primary)', borderColor: 'var(--primary-tint-border)' }}
                                  onClick={() => openWhatsAppModal('receipt', receipt.id)}
                                  title="Enviar recibo por WhatsApp"
                                >
                                  <span className="nav-icon" style={{ width: '13px', height: '13px' }}><IconWhatsApp /></span>
                                  WhatsApp
                                </button>
                              ) : null}
                            </div>
                          </td>
                          <td className="row-actions">
                            {getReceiptStatus(receipt) !== 'paid' ? (
                              <button
                                type="button"
                                className="btn btn-sm btn-primary"
                                onClick={() => startReceiptPay(receipt)}
                                title="Lançar pagamento parcial ou integral"
                              >
                                Baixa
                              </button>
                            ) : null}
                            <button
                              type="button"
                              className="btn btn-sm btn-secondary"
                              onClick={() => startReceiptEdit(receipt)}
                            >
                              Editar
                            </button>
                            <DeleteRowButton
                              intent="receipt.delete"
                              tab="receipts"
                              id={receipt.id}
                              onOptimisticDelete={() =>
                                setReceiptItems((current) => current.filter((row) => row.id !== receipt.id))
                              }
                              onSettled={settleAfterMutation}
                              queueOfflineMutation={queueOfflineMutation}
                            />
                          </td>
                        </tr>
                      ))
                    ) : (
                      <tr>
                        <td colSpan={8}>Cadastre um contrato para gerar recibos.</td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
              <Pagination
                page={receiptPage}
                totalPages={receiptTotalPages}
                onChange={setReceiptPage}
                pageSize={receiptPageSize}
                onPageSizeChange={(n) => {
                  setReceiptPageSize(n);
                  setReceiptPage(1);
                }}
              />
            </section>
          ) : null}

          {modalType === 'tenants' ? (
            <Modal title="Editar Inquilino" onClose={() => setModalType('')}>
              <form
                className="stack"
                onSubmit={(event) => {
                  if (
                    handleOfflineSubmit(
                      event,
                      (payload) => {
                        setTenantItems((current) =>
                          current.map((row) =>
                            row.id === payload.id
                              ? {
                                  ...row,
                                  name: payload.name || '',
                                  email: payload.email || '',
                                  phone: payload.phone || null,
                                  cpf: payload.cpf || null,
                                }
                              : row
                          )
                        );
                      },
                      'Alteração pendente de sincronização.',
                      () => {
                        setTenantDraft({ id: '', name: '', email: '', phone: '', cpf: '' });
                        setModalType('');
                      }
                    )
                  ) {
                    return;
                  }
                  event.preventDefault();
                  const formData = new FormData(event.currentTarget);
                  void sendMutation(formData, 'Inquilino salvo com sucesso.', () => {
                    setTenantDraft({ id: '', name: '', email: '', phone: '', cpf: '' });
                    setModalType('');
                  });
                }}
              >
                <input type="hidden" name="intent" value="tenant.update" />
                <input type="hidden" name="id" value={tenantDraft.id} />
                <div className="form-group">
                  <label htmlFor="modal-tenant-name">Nome</label>
                  <input
                    id="modal-tenant-name"
                    name="name"
                    value={tenantDraft.name}
                    onChange={(e) => setTenantDraft((current) => ({ ...current, name: e.target.value }))}
                    required
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="modal-tenant-email">Email</label>
                  <input
                    id="modal-tenant-email"
                    name="email"
                    type="email"
                    value={tenantDraft.email}
                    onChange={(e) => setTenantDraft((current) => ({ ...current, email: e.target.value }))}
                    required
                  />
                </div>
                <CpfInput
                  id="modal-tenant-cpf"
                  name="cpf"
                  value={tenantDraft.cpf}
                  onChange={(value) => setTenantDraft((current) => ({ ...current, cpf: value }))}
                  label="CPF"
                />
                <PhoneInput
                  id="modal-tenant-phone"
                  name="phone"
                  value={tenantDraft.phone}
                  onChange={(value) => setTenantDraft((current) => ({ ...current, phone: value }))}
                  label="Telefone"
                />
                <button type="submit" className="btn btn-primary" disabled={busy}>
                  Salvar alterações
                </button>
                <button
                  type="button"
                  className="btn btn-sm btn-secondary"
                  onClick={() => {
                    setTenantDraft({ id: '', name: '', email: '', phone: '', cpf: '' });
                    setModalType('');
                  }}
                >
                  Descartar
                </button>
              </form>
            </Modal>
          ) : null}

          {createModalType === 'tenants' ? (
            <Modal title="Novo Inquilino" onClose={() => setCreateModalType('')}>
              <form
                className="stack"
                onSubmit={(event) => {
                  if (
                    handleOfflineSubmit(
                      event,
                      (payload) => {
                        const item = {
                          id: payload.id || createId(),
                          name: payload.name || '',
                          email: payload.email || '',
                          phone: payload.phone || null,
                          cpf: payload.cpf || null,
                        };
                        setTenantItems((current) => [item, ...current]);
                      },
                      'Cadastro pendente de sincronização.',
                      () => {
                        setTenantForm({ name: '', email: '', phone: '', cpf: '' });
                        setCreateModalType('');
                      }
                    )
                  ) {
                    return;
                  }
                  event.preventDefault();
                  const formData = new FormData(event.currentTarget);
                  void sendMutation(formData, 'Inquilino cadastrado com sucesso.', () => {
                    setTenantForm({ name: '', email: '', phone: '', cpf: '' });
                    setCreateModalType('');
                  });
                }}
              >
                <input type="hidden" name="intent" value="tenant.create" />
                <input type="hidden" name="tab" value="tenants" />
                <div className="form-group">
                  <label htmlFor="create-tenant-name">Nome</label>
                  <input
                    id="create-tenant-name"
                    name="name"
                    value={tenantForm.name}
                    onChange={(e) => setTenantForm((current) => ({ ...current, name: e.target.value }))}
                    required
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="create-tenant-email">Email</label>
                  <input
                    id="create-tenant-email"
                    name="email"
                    type="email"
                    value={tenantForm.email}
                    onChange={(e) => setTenantForm((current) => ({ ...current, email: e.target.value }))}
                    required
                  />
                </div>
                <CpfInput
                  id="create-tenant-cpf"
                  name="cpf"
                  value={tenantForm.cpf}
                  onChange={(value) => setTenantForm((current) => ({ ...current, cpf: value }))}
                  label="CPF"
                />
                <PhoneInput
                  id="create-tenant-phone"
                  name="phone"
                  value={tenantForm.phone}
                  onChange={(value) => setTenantForm((current) => ({ ...current, phone: value }))}
                  label="Telefone"
                />
                <button type="submit" className="btn btn-primary" disabled={busy}>
                  Adicionar inquilino
                </button>
              </form>
            </Modal>
          ) : null}

          {modalType === 'properties' ? (
            <Modal title="Editar Imóvel" onClose={() => setModalType('')}>
              <form
                className="stack"
                onSubmit={(event) => {
                  if (
                    handleOfflineSubmit(
                      event,
                      (payload) => {
                        setPropertyItems((current) =>
                          current.map((row) =>
                            row.id === payload.id
                              ? {
                                  ...row,
                                  name: payload.name || '',
                                  address: payload.address || '',
                                  city: payload.city || '',
                                  state: payload.state || '',
                                  zip: payload.zip || null,
                                }
                              : row
                          )
                        );
                      },
                      'Alteração pendente de sincronização.',
                      () => {
                        setPropertyDraft({ id: '', name: '', address: '', city: '', state: '', zip: '' });
                        setModalType('');
                      }
                    )
                  ) {
                    return;
                  }
                  event.preventDefault();
                  const formData = new FormData(event.currentTarget);
                  void sendMutation(formData, 'Imóvel salvo com sucesso.', () => {
                    setPropertyDraft({ id: '', name: '', address: '', city: '', state: '', zip: '' });
                    setModalType('');
                  });
                }}
              >
                <input type="hidden" name="intent" value="property.update" />
                <input type="hidden" name="id" value={propertyDraft.id} />
                <div className="form-group">
                  <label htmlFor="modal-property-name">Nome</label>
                  <input
                    id="modal-property-name"
                    name="name"
                    value={propertyDraft.name}
                    onChange={(e) => setPropertyDraft((current) => ({ ...current, name: e.target.value }))}
                    required
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="modal-property-address">Endereço</label>
                  <input
                    id="modal-property-address"
                    name="address"
                    value={propertyDraft.address}
                    onChange={(e) => setPropertyDraft((current) => ({ ...current, address: e.target.value }))}
                    required
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="modal-property-city">Cidade</label>
                  <input
                    id="modal-property-city"
                    name="city"
                    value={propertyDraft.city}
                    onChange={(e) => setPropertyDraft((current) => ({ ...current, city: e.target.value }))}
                    required
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="modal-property-state">Estado</label>
                  <input
                    id="modal-property-state"
                    name="state"
                    value={propertyDraft.state}
                    onChange={(e) => setPropertyDraft((current) => ({ ...current, state: e.target.value }))}
                    required
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="modal-property-zip">CEP</label>
                  <input
                    id="modal-property-zip"
                    name="zip"
                    value={propertyDraft.zip}
                    onChange={(e) => setPropertyDraft((current) => ({ ...current, zip: e.target.value }))}
                  />
                </div>
                <button type="submit" className="btn btn-primary" disabled={busy}>
                  Salvar alterações
                </button>
                <button
                  type="button"
                  className="btn btn-sm btn-secondary"
                  onClick={() => {
                    setPropertyDraft({ id: '', name: '', address: '', city: '', state: '', zip: '' });
                    setModalType('');
                  }}
                >
                  Descartar
                </button>
              </form>
            </Modal>
          ) : null}

          {createModalType === 'properties' ? (
            <Modal title="Novo Imóvel" onClose={() => setCreateModalType('')}>
              <form
                className="stack"
                onSubmit={(event) => {
                  if (
                    handleOfflineSubmit(
                      event,
                      (payload) => {
                        const item = {
                          id: payload.id || createId(),
                          name: payload.name || '',
                          address: payload.address || '',
                          city: payload.city || '',
                          state: payload.state || '',
                          zip: payload.zip || null,
                        };
                        setPropertyItems((current) => [item, ...current]);
                      },
                      'Cadastro pendente de sincronização.',
                      () => {
                        setPropertyForm({ name: '', address: '', city: '', state: '', zip: '' });
                        setCreateModalType('');
                      }
                    )
                  ) {
                    return;
                  }
                  event.preventDefault();
                  const formData = new FormData(event.currentTarget);
                  void sendMutation(formData, 'Imóvel cadastrado com sucesso.', () => {
                    setPropertyForm({ name: '', address: '', city: '', state: '', zip: '' });
                    setCreateModalType('');
                  });
                }}
              >
                <input type="hidden" name="intent" value="property.create" />
                <input type="hidden" name="tab" value="properties" />
                <div className="form-group">
                  <label htmlFor="create-property-name">Nome</label>
                  <input
                    id="create-property-name"
                    name="name"
                    value={propertyForm.name}
                    onChange={(e) => setPropertyForm((current) => ({ ...current, name: e.target.value }))}
                    required
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="create-property-address">Endereço</label>
                  <input
                    id="create-property-address"
                    name="address"
                    value={propertyForm.address}
                    onChange={(e) => setPropertyForm((current) => ({ ...current, address: e.target.value }))}
                    required
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="create-property-city">Cidade</label>
                  <input
                    id="create-property-city"
                    name="city"
                    value={propertyForm.city}
                    onChange={(e) => setPropertyForm((current) => ({ ...current, city: e.target.value }))}
                    required
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="create-property-state">Estado</label>
                  <input
                    id="create-property-state"
                    name="state"
                    value={propertyForm.state}
                    onChange={(e) => setPropertyForm((current) => ({ ...current, state: e.target.value }))}
                    required
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="create-property-zip">CEP</label>
                  <input
                    id="create-property-zip"
                    name="zip"
                    value={propertyForm.zip}
                    onChange={(e) => setPropertyForm((current) => ({ ...current, zip: e.target.value }))}
                  />
                </div>
                <button type="submit" className="btn btn-primary" disabled={busy}>
                  Adicionar imóvel
                </button>
              </form>
            </Modal>
          ) : null}

          {modalType === 'leases' ? (
            <Modal title="Editar Contrato" onClose={() => setModalType('')}>
              <form
                className="stack"
                onSubmit={(event) => {
                  if (
                    handleOfflineSubmit(
                      event,
                      (payload) => {
                        const tenant = tenantItems.find((item) => item.id === payload.tenant_id);
                        const property = propertyItems.find((item) => item.id === payload.property_id);
                        setLeaseItems((current) =>
                          current.map((row) =>
                            row.id === payload.id
                              ? {
                                  ...row,
                                  tenant_id: payload.tenant_id || '',
                                  property_id: payload.property_id || '',
                                  start_date: payload.start_date || '',
                                  end_date: payload.end_date || '',
                                  due_day: payload.due_day || '1',
                                  rent_amount: payload.rent_amount || '0',
                                  status: payload.status || row.status || 'active',
                                  tenant_name: tenant?.name || '-',
                                  property_name: property?.name || '-',
                                }
                              : row
                          )
                        );
                      },
                      'Alteração pendente de sincronização.',
                      () => {
                        setLeaseDraft({
                          id: '',
                          tenant_id: '',
                          property_id: '',
                          start_date: '',
                          end_date: '',
                          due_day: '1',
                          rent_amount: '',
                          status: 'active',
                        });
                        setModalType('');
                      }
                    )
                  ) {
                    return;
                  }
                  event.preventDefault();
                  const formData = new FormData(event.currentTarget);
                  void sendMutation(formData, 'Contrato salvo com sucesso.', () => {
                    setLeaseDraft({
                      id: '',
                      tenant_id: '',
                      property_id: '',
                      start_date: '',
                      end_date: '',
                      due_day: '1',
                      rent_amount: '',
                      status: 'active',
                    });
                    setModalType('');
                  });
                }}
              >
                <input type="hidden" name="intent" value="lease.update" />
                <input type="hidden" name="id" value={leaseDraft.id} />
                <CustomSelect
                  id="modal-lease-tenant"
                  name="tenant_id"
                  value={leaseDraft.tenant_id}
                  onChange={(v) => setLeaseDraft((current) => ({ ...current, tenant_id: v }))}
                  label="Inquilino"
                  required
                >
                  <option value="">Selecione</option>
                  {tenantItems.map((tenant) => (
                    <option key={tenant.id} value={tenant.id}>
                      {tenant.name}
                    </option>
                  ))}
                </CustomSelect>
                <CustomSelect
                  id="modal-lease-property"
                  name="property_id"
                  value={leaseDraft.property_id}
                  onChange={(v) => setLeaseDraft((current) => ({ ...current, property_id: v }))}
                  label="Imóvel"
                  required
                >
                  <option value="">Selecione</option>
                  {propertyItems.map((property) => (
                    <option key={property.id} value={property.id}>
                      {property.name}
                    </option>
                  ))}
                </CustomSelect>
                <CustomSelect
                  id="modal-lease-status"
                  name="status"
                  value={leaseDraft.status}
                  onChange={(v) => setLeaseDraft((current) => ({ ...current, status: v }))}
                  label="Status"
                >
                  <option value="active">Ativo</option>
                  <option value="suspended">Suspenso (sem cobrança automática)</option>
                  <option value="season">Temporada</option>
                  <option value="terminated">Encerrado</option>
                </CustomSelect>
                <DatePickerInput
                  id="modal-lease-start"
                  name="start_date"
                  value={leaseDraft.start_date}
                  onChange={(v) => setLeaseDraft((current) => ({ ...current, start_date: v }))}
                  label="Início"
                  required
                />
                <DatePickerInput
                  id="modal-lease-end"
                  name="end_date"
                  value={leaseDraft.end_date}
                  onChange={(v) => setLeaseDraft((current) => ({ ...current, end_date: v }))}
                  label="Fim"
                  required
                />
                <div className="form-group">
                  <label htmlFor="modal-lease-due-day">Dia do vencimento</label>
                  <input
                    id="modal-lease-due-day"
                    name="due_day"
                    type="number"
                    min="1"
                    max="31"
                    value={leaseDraft.due_day}
                    onChange={(e) => setLeaseDraft((current) => ({ ...current, due_day: e.target.value }))}
                    required
                  />
                </div>
                <MoneyInput
                  id="modal-lease-rent"
                  name="rent_amount"
                  value={leaseDraft.rent_amount}
                  onChange={(value) => setLeaseDraft((current) => ({ ...current, rent_amount: value }))}
                  label="Aluguel"
                  required
                />
                <button type="submit" className="btn btn-primary" disabled={busy}>
                  Salvar alterações
                </button>
                <button
                  type="button"
                  className="btn btn-sm btn-secondary"
                  onClick={() => {
                    setLeaseDraft({
                      id: '',
                      tenant_id: '',
                      property_id: '',
                      start_date: '',
                      end_date: '',
                      due_day: '1',
                      rent_amount: '',
                      status: 'active',
                    });
                    setModalType('');
                  }}
                >
                  Descartar
                </button>
              </form>
            </Modal>
          ) : null}

          {createModalType === 'leases' ? (
            <Modal title="Novo Contrato" onClose={() => setCreateModalType('')}>
              <form
                className="stack"
                onSubmit={(event) => {
                  if (
                    handleOfflineSubmit(
                      event,
                      (payload) => {
                        const tenant = tenantItems.find((item) => item.id === payload.tenant_id);
                        const property = propertyItems.find((item) => item.id === payload.property_id);
                        const item = {
                          id: payload.id || createId(),
                          tenant_id: payload.tenant_id || '',
                          property_id: payload.property_id || '',
                          start_date: payload.start_date || '',
                          end_date: payload.end_date || '',
                          due_day: payload.due_day || '1',
                          rent_amount: payload.rent_amount || '0',
                          tenant_name: tenant?.name || '-',
                          property_name: property?.name || '-',
                          status: payload.status || 'active',
                        };
                        setLeaseItems((current) => [item, ...current]);
                      },
                      'Cadastro pendente de sincronização.',
                      () => {
                        setLeaseForm({
                          tenant_id: '',
                          property_id: '',
                          start_date: '',
                          end_date: '',
                          due_day: '1',
                          rent_amount: '',
                          status: 'active',
                        });
                        setCreateModalType('');
                      }
                    )
                  ) {
                    return;
                  }
                  event.preventDefault();
                  const formData = new FormData(event.currentTarget);
                  void sendMutation(formData, 'Contrato cadastrado com sucesso.', () => {
                    setLeaseForm({
                      tenant_id: '',
                      property_id: '',
                      start_date: '',
                      end_date: '',
                      due_day: '1',
                      rent_amount: '',
                      status: 'active',
                    });
                    setCreateModalType('');
                  });
                }}
              >
                <input type="hidden" name="intent" value="lease.create" />
                <input type="hidden" name="tab" value="leases" />
                <CustomSelect
                  id="create-lease-tenant"
                  name="tenant_id"
                  value={leaseForm.tenant_id}
                  onChange={(v) => setLeaseForm((current) => ({ ...current, tenant_id: v }))}
                  label="Inquilino"
                  required
                >
                  <option value="">Selecione</option>
                  {tenantItems.map((tenant) => (
                    <option key={tenant.id} value={tenant.id}>
                      {tenant.name}
                    </option>
                  ))}
                </CustomSelect>
                <CustomSelect
                  id="create-lease-property"
                  name="property_id"
                  value={leaseForm.property_id}
                  onChange={(v) => setLeaseForm((current) => ({ ...current, property_id: v }))}
                  label="Imóvel"
                  required
                >
                  <option value="">Selecione</option>
                  {propertyItems.map((property) => (
                    <option key={property.id} value={property.id}>
                      {property.name}
                    </option>
                  ))}
                </CustomSelect>
                <CustomSelect
                  id="create-lease-status"
                  name="status"
                  value={leaseForm.status}
                  onChange={(v) => setLeaseForm((current) => ({ ...current, status: v }))}
                  label="Status"
                >
                  <option value="active">Ativo</option>
                  <option value="suspended">Suspenso (sem cobrança automática)</option>
                  <option value="season">Temporada</option>
                </CustomSelect>
                <DatePickerInput
                  id="create-lease-start"
                  name="start_date"
                  value={leaseForm.start_date}
                  onChange={(v) => setLeaseForm((current) => ({ ...current, start_date: v }))}
                  label="Início"
                  required
                />
                <DatePickerInput
                  id="create-lease-end"
                  name="end_date"
                  value={leaseForm.end_date}
                  onChange={(v) => setLeaseForm((current) => ({ ...current, end_date: v }))}
                  label="Fim"
                  required
                />
                <div className="form-group">
                  <label htmlFor="create-lease-due-day">Dia do vencimento</label>
                  <input
                    id="create-lease-due-day"
                    name="due_day"
                    type="number"
                    min="1"
                    max="31"
                    value={leaseForm.due_day}
                    onChange={(e) => setLeaseForm((current) => ({ ...current, due_day: e.target.value }))}
                    required
                  />
                </div>
                <MoneyInput
                  id="create-lease-rent"
                  name="rent_amount"
                  value={leaseForm.rent_amount}
                  onChange={(value) => setLeaseForm((current) => ({ ...current, rent_amount: value }))}
                  label="Aluguel"
                  required
                />
                <button type="submit" className="btn btn-primary" disabled={busy}>
                  Adicionar contrato
                </button>
              </form>
            </Modal>
          ) : null}

          {modalType === 'receipts' ? (
            <Modal title="Editar Pagamento" onClose={() => setModalType('')}>
              <form
                className="stack"
                onSubmit={(event) => {
                  if (
                    handleOfflineSubmit(
                      event,
                      (payload) => {
                        const lease = receiptLeaseMeta(payload.lease_id || '');
                        setReceiptItems((current) =>
                          current.map((row) =>
                            row.id === payload.id
                              ? {
                                  ...row,
                                  lease_id: payload.lease_id || '',
                                  due_date: payload.due_date || '',
                                  paid_at: payload.paid_at || null,
                                  amount: payload.amount || '0',
                                  tenant_name: lease?.tenant_name || '-',
                                  property_name: lease?.property_name || '-',
                                  pdf_path: null,
                                }
                              : row
                          )
                        );
                      },
                      'Alteração pendente de sincronização.',
                      () => {
                        setReceiptDraft({ id: '', lease_id: '', due_date: '', paid_at: '', amount: '' });
                        setModalType('');
                      }
                    )
                  ) {
                    return;
                  }
                  event.preventDefault();
                  const formData = new FormData(event.currentTarget);
                  void sendMutation(formData, 'Pagamento salvo com sucesso.', () => {
                    setReceiptDraft({ id: '', lease_id: '', due_date: '', paid_at: '', amount: '' });
                    setModalType('');
                  });
                }}
              >
                <input type="hidden" name="intent" value="receipt.update" />
                <input type="hidden" name="id" value={receiptDraft.id} />
                <CustomSelect
                  id="modal-receipt-lease"
                  name="lease_id"
                  value={receiptDraft.lease_id}
                  onChange={(v) => setReceiptDraft((current) => ({ ...current, lease_id: v }))}
                  label="Contrato"
                  required
                >
                  <option value="">Selecione</option>
                  {leaseItems.map((lease) => (
                    <option key={lease.id} value={lease.id}>
                      {lease.tenant_name} / {lease.property_name}
                    </option>
                  ))}
                </CustomSelect>
                <DatePickerInput
                  id="modal-receipt-due"
                  name="due_date"
                  value={receiptDraft.due_date}
                  onChange={(v) => setReceiptDraft((current) => ({ ...current, due_date: v }))}
                  label="Vencimento"
                  required
                />
                <DateTimePickerInput
                  id="modal-receipt-paid"
                  name="paid_at"
                  value={receiptDraft.paid_at}
                  onChange={(v) => setReceiptDraft((current) => ({ ...current, paid_at: v }))}
                  label="Pago em"
                />
                <MoneyInput
                  id="modal-receipt-amount"
                  name="amount"
                  value={receiptDraft.amount}
                  onChange={(value) => setReceiptDraft((current) => ({ ...current, amount: value }))}
                  label="Valor"
                  required
                />
                <button type="submit" className="btn btn-primary" disabled={busy}>
                  Salvar alterações
                </button>
                <button
                  type="button"
                  className="btn btn-sm btn-secondary"
                  onClick={() => {
                    setReceiptDraft({ id: '', lease_id: '', due_date: '', paid_at: '', amount: '' });
                    setModalType('');
                  }}
                >
                  Descartar
                </button>
              </form>
            </Modal>
          ) : null}

          {payModalOpen ? (
            <Modal
              title="Lançar Pagamento"
              onClose={() => {
                setPayModalOpen(false);
                setPayDraft({ id: '', amount: '', pay_amount: '', paid_at: '' });
              }}
            >
              <form
                className="stack"
                onSubmit={(event) => {
                  event.preventDefault();
                  const formData = new FormData(event.currentTarget);
                  void sendMutation(formData, 'Pagamento lançado com sucesso.', () => {
                    setPayModalOpen(false);
                    setPayDraft({ id: '', amount: '', pay_amount: '', paid_at: '' });
                  });
                }}
              >
                <input type="hidden" name="intent" value="receipt.pay" />
                <input type="hidden" name="id" value={payDraft.id} />
                <div style={{ padding: '0.5rem 0', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
                  Valor total: <strong style={{ color: 'var(--text)' }}>{formatCurrency(payDraft.amount)}</strong>
                </div>
                <MoneyInput
                  id="pay-modal-amount"
                  name="pay_amount"
                  value={payDraft.pay_amount}
                  onChange={(v) => setPayDraft((d) => ({ ...d, pay_amount: v }))}
                  label="Valor pago"
                  required
                />
                <DateTimePickerInput
                  id="pay-modal-paid-at"
                  name="paid_at"
                  value={payDraft.paid_at}
                  onChange={(v) => setPayDraft((d) => ({ ...d, paid_at: v }))}
                  label="Data/hora do pagamento"
                />
                <button type="submit" className="btn btn-primary" disabled={busy}>
                  Confirmar pagamento
                </button>
                <button
                  type="button"
                  className="btn btn-sm btn-secondary"
                  onClick={() => {
                    setPayModalOpen(false);
                    setPayDraft({ id: '', amount: '', pay_amount: '', paid_at: '' });
                  }}
                >
                  Cancelar
                </button>
              </form>
            </Modal>
          ) : null}

          {createModalType === 'receipts' ? (
            <Modal
              title={receiptForm.category === 'extra' ? 'Nova Taxa Avulsa' : 'Novo Pagamento de Aluguel'}
              onClose={() => {
                setReceiptForm({ lease_id: '', due_date: '', paid_at: '', amount: '', category: 'rent', description: '', tenant_id: '', property_id: '' });
                setCreateModalType('');
              }}
            >
              <form
                className="stack"
                onSubmit={(event) => {
                  if (
                    handleOfflineSubmit(
                      event,
                      (payload) => {
                        const lease = receiptLeaseMeta(payload.lease_id || '');
                        const tenant = tenantItems.find((t) => t.id === (payload.tenant_id || lease?.tenant_id || ''));
                        const property = propertyItems.find((p) => p.id === (payload.property_id || lease?.property_id || ''));
                        const item = {
                          id: payload.id || createId(),
                          lease_id: payload.lease_id || null,
                          tenant_id: payload.tenant_id || lease?.tenant_id || null,
                          property_id: payload.property_id || lease?.property_id || null,
                          category: (payload.category === 'extra' ? 'extra' : 'rent') as 'rent' | 'extra',
                          description: payload.description || null,
                          due_date: payload.due_date || '',
                          paid_at: payload.paid_at || null,
                          amount: payload.amount || '0',
                          paid_amount: null,
                          pdf_path: null,
                          generated_key: null,
                          tenant_name: tenant?.name || lease?.tenant_name || '-',
                          property_name: property?.name || lease?.property_name || '-',
                        };
                        setReceiptItems((current) => [item, ...current]);
                      },
                      'Cadastro pendente de sincronização.',
                      () => {
                        setReceiptForm({ lease_id: '', due_date: '', paid_at: '', amount: '', category: 'rent', description: '', tenant_id: '', property_id: '' });
                        setCreateModalType('');
                      }
                    )
                  ) {
                    return;
                  }
                  event.preventDefault();
                  const formData = new FormData(event.currentTarget);
                  void sendMutation(formData, receiptForm.category === 'extra' ? 'Taxa avulsa cadastrada com sucesso.' : 'Pagamento cadastrado com sucesso.', () => {
                    setReceiptForm({ lease_id: '', due_date: '', paid_at: '', amount: '', category: 'rent', description: '', tenant_id: '', property_id: '' });
                    setCreateModalType('');
                  });
                }}
              >
                <input type="hidden" name="intent" value="receipt.create" />
                <input type="hidden" name="tab" value="receipts" />
                <input type="hidden" name="category" value={receiptForm.category} />

                {/* Mode toggle */}
                <div style={{ display: 'flex', gap: '0.5rem', padding: '0.25rem', background: 'var(--surface-subtle)', borderRadius: 'var(--radius-md)', marginBottom: '0.25rem' }}>
                  <button
                    type="button"
                    style={{
                      flex: 1, padding: '0.45rem 0.75rem', borderRadius: 'var(--radius-sm)', fontSize: '0.85rem', fontWeight: 600, border: 'none', cursor: 'pointer',
                      background: receiptForm.category === 'rent' ? 'var(--surface)' : 'transparent',
                      color: receiptForm.category === 'rent' ? 'var(--text)' : 'var(--text-muted)',
                      boxShadow: receiptForm.category === 'rent' ? 'var(--shadow-sm)' : 'none',
                    }}
                    onClick={() => setReceiptForm((f) => ({ ...f, category: 'rent', description: '', tenant_id: '', property_id: '' }))}
                  >
                    Aluguel / Contrato
                  </button>
                  <button
                    type="button"
                    style={{
                      flex: 1, padding: '0.45rem 0.75rem', borderRadius: 'var(--radius-sm)', fontSize: '0.85rem', fontWeight: 600, border: 'none', cursor: 'pointer',
                      background: receiptForm.category === 'extra' ? 'var(--surface)' : 'transparent',
                      color: receiptForm.category === 'extra' ? 'var(--text)' : 'var(--text-muted)',
                      boxShadow: receiptForm.category === 'extra' ? 'var(--shadow-sm)' : 'none',
                    }}
                    onClick={() => setReceiptForm((f) => ({ ...f, category: 'extra', lease_id: '' }))}
                  >
                    Taxa Avulsa
                  </button>
                </div>

                {receiptForm.category === 'rent' ? (
                  <CustomSelect
                    id="create-receipt-lease"
                    name="lease_id"
                    value={receiptForm.lease_id}
                    onChange={(v) => setReceiptForm((current) => ({ ...current, lease_id: v }))}
                    label="Contrato"
                    required
                  >
                    <option value="">Selecione</option>
                    {leaseItems.map((lease) => (
                      <option key={lease.id} value={lease.id}>
                        {lease.tenant_name} / {lease.property_name}
                      </option>
                    ))}
                  </CustomSelect>
                ) : (
                  <>
                    <div>
                      <label htmlFor="create-receipt-desc" style={{ display: 'block', fontSize: '0.8rem', fontWeight: 600, color: 'var(--text-secondary)', marginBottom: '0.35rem' }}>
                        Descrição <span style={{ color: 'var(--danger)' }}>*</span>
                      </label>
                      <input
                        id="create-receipt-desc"
                        name="description"
                        type="text"
                        className="input"
                        placeholder="Ex: Taxa de condomínio, Multa, Pintura…"
                        value={receiptForm.description}
                        onChange={(e) => setReceiptForm((f) => ({ ...f, description: e.target.value }))}
                        required
                        autoComplete="off"
                      />
                    </div>
                    <CustomSelect
                      id="create-receipt-tenant"
                      name="tenant_id"
                      value={receiptForm.tenant_id}
                      onChange={(v) => setReceiptForm((f) => ({ ...f, tenant_id: v }))}
                      label="Inquilino (opcional)"
                    >
                      <option value="">Nenhum</option>
                      {tenantItems.map((t) => (
                        <option key={t.id} value={t.id}>{t.name}</option>
                      ))}
                    </CustomSelect>
                    <CustomSelect
                      id="create-receipt-property"
                      name="property_id"
                      value={receiptForm.property_id}
                      onChange={(v) => setReceiptForm((f) => ({ ...f, property_id: v }))}
                      label="Imóvel (opcional)"
                    >
                      <option value="">Nenhum</option>
                      {propertyItems.map((p) => (
                        <option key={p.id} value={p.id}>{p.name}</option>
                      ))}
                    </CustomSelect>
                  </>
                )}

                <DatePickerInput
                  id="create-receipt-due"
                  name="due_date"
                  value={receiptForm.due_date}
                  onChange={(v) => setReceiptForm((current) => ({ ...current, due_date: v }))}
                  label="Vencimento"
                  required
                />
                <DateTimePickerInput
                  id="create-receipt-paid"
                  name="paid_at"
                  value={receiptForm.paid_at}
                  onChange={(v) => setReceiptForm((current) => ({ ...current, paid_at: v }))}
                  label="Pago em"
                />
                <MoneyInput
                  id="create-receipt-amount"
                  name="amount"
                  value={receiptForm.amount}
                  onChange={(value) => setReceiptForm((current) => ({ ...current, amount: value }))}
                  label="Valor"
                  required
                />
                <button type="submit" className="btn btn-primary" disabled={busy}>
                  {receiptForm.category === 'extra' ? 'Cadastrar taxa avulsa' : 'Adicionar pagamento'}
                </button>
              </form>
            </Modal>
          ) : null}

          {whatsappModal?.open ? (
            <Modal
              title={`Enviar ${whatsappModal.type === 'receipt' ? 'Recibo' : 'Contrato'} por WhatsApp`}
              onClose={() => {
                if (whatsappModal.sending) return;
                stopSendPolling();
                setWhatsappModal(null);
              }}
            >
              <div className="stack">
                {whatsappModal.loading ? (
                  <div style={{ textAlign: 'center', padding: '2rem', color: 'var(--text-muted)' }}>
                    <div className="loading-spinner" />
                    <p style={{ marginTop: '0.75rem', fontSize: '0.875rem' }}>Carregando dados e gerando links oficiais...</p>
                  </div>
                ) : (
                  <>
                    {whatsappModal.success ? <div className="success">{whatsappModal.success}</div> : null}
                    {whatsappModal.error ? <div className="error">{whatsappModal.error}</div> : null}

                    {whatsappModal.sending ? (
                      <div
                        style={{
                          background: 'var(--surface-subtle)',
                          border: '1px solid var(--border)',
                          borderRadius: 'var(--radius-md)',
                          padding: '0.875rem 1rem',
                          display: 'flex',
                          flexDirection: 'column',
                          gap: '0.5rem',
                        }}
                      >
                        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontWeight: 600, fontSize: '0.875rem' }}>
                          <div className="loading-spinner" style={{ width: '14px', height: '14px' }} />
                          <span>Enviando pelo WhatsApp...</span>
                        </div>
                        <ul
                          style={{
                            listStyle: 'none',
                            margin: 0,
                            padding: 0,
                            display: 'flex',
                            flexDirection: 'column',
                            gap: '0.3rem',
                          }}
                        >
                          {WHATSAPP_SEND_STEPS.map((step, index) => {
                            const isDone = whatsappSendStepIndex > index;
                            const isActive = whatsappSendStepIndex === index;
                            return (
                              <li
                                key={step.stage}
                                style={{
                                  display: 'flex',
                                  alignItems: 'center',
                                  gap: '0.5rem',
                                  fontSize: '0.8125rem',
                                  color: isDone
                                    ? 'var(--text-secondary)'
                                    : isActive
                                      ? 'var(--text-primary)'
                                      : 'var(--text-muted)',
                                }}
                              >
                                <span aria-hidden="true">{isDone ? '✓' : isActive ? '●' : '○'}</span>
                                <span style={{ fontWeight: isActive ? 600 : 400 }}>{step.label}</span>
                              </li>
                            );
                          })}
                        </ul>
                        <p style={{ fontSize: '0.75rem', color: 'var(--text-muted)', margin: 0 }}>
                          Mantenha esta janela aberta até a conclusão. O envio leva alguns segundos.
                        </p>
                      </div>
                    ) : null}

                    <div
                      style={{
                        background: 'var(--surface-subtle)',
                        padding: '0.75rem 1rem',
                        borderRadius: 'var(--radius-md)',
                        fontSize: '0.875rem',
                      }}
                    >
                      <div>
                        <strong>Locatário:</strong> {whatsappModal.tenantName}
                      </div>
                      <div style={{ color: 'var(--text-muted)', fontSize: '0.8125rem' }}>
                        <strong>Imóvel:</strong> {whatsappModal.propertyName}
                      </div>
                    </div>

                    {whatsappStatus ? (
                      <div
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'space-between',
                          padding: '0.625rem 0.875rem',
                          borderRadius: 'var(--radius-md)',
                          background: whatsappStatus.isLoggedIn
                            ? 'rgba(16, 185, 129, 0.08)'
                            : 'rgba(239, 68, 68, 0.08)',
                          border: `1px solid ${whatsappStatus.isLoggedIn ? 'rgba(16, 185, 129, 0.25)' : 'rgba(239, 68, 68, 0.25)'}`,
                          fontSize: '0.8125rem',
                          gap: '0.5rem',
                          flexWrap: 'wrap',
                        }}
                      >
                        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                          <span style={{ fontSize: '0.875rem' }}>{whatsappStatus.isLoggedIn ? '🟢' : '🔴'}</span>
                          <span>
                            {whatsappStatus.isLoggedIn
                              ? 'WhatsApp conectado — envio automático disponível.'
                              : 'WhatsApp não conectado.'}
                          </span>
                        </div>
                        {!whatsappStatus.isLoggedIn && (
                          <button
                            type="button"
                            className="btn btn-sm btn-outline"
                            onClick={() => setConnectWhatsappOpen(true)}
                            style={{ padding: '0.25rem 0.625rem', fontSize: '0.75rem' }}
                          >
                            Conectar WhatsApp (QR Code)
                          </button>
                        )}
                      </div>
                    ) : null}

                    <div className="form-group">
                      <label htmlFor="whatsapp-phone">Número de WhatsApp (com DDD)</label>
                      <input
                        id="whatsapp-phone"
                        type="text"
                        disabled={whatsappModal.sending}
                        value={whatsappModal.phone}
                        onChange={(e) => {
                          const val = e.target.value;
                          setWhatsappModal((prev) =>
                            prev
                              ? {
                                  ...prev,
                                  phone: val,
                                  directUrl: `https://wa.me/${val.replace(/\D/g, '').startsWith('55') ? val.replace(/\D/g, '') : `55${val.replace(/\D/g, '')}`}?text=${encodeURIComponent(prev.message)}`,
                                }
                              : null
                          );
                        }}
                        placeholder="(22) 99999-9999"
                      />
                    </div>

                    <div className="form-group">
                      <label htmlFor="whatsapp-message">Mensagem de Envio</label>
                      <textarea
                        id="whatsapp-message"
                        rows={8}
                        disabled={whatsappModal.sending}
                        style={{ fontFamily: 'inherit', resize: 'vertical' }}
                        value={whatsappModal.message}
                        onChange={(e) => {
                          const msg = e.target.value;
                          setWhatsappModal((prev) =>
                            prev
                              ? {
                                  ...prev,
                                  message: msg,
                                  directUrl: `https://wa.me/${prev.phone.replace(/\D/g, '').startsWith('55') ? prev.phone.replace(/\D/g, '') : `55${prev.phone.replace(/\D/g, '')}`}?text=${encodeURIComponent(msg)}`,
                                }
                              : null
                          );
                        }}
                      />
                    </div>

                    <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap', marginTop: '0.5rem' }}>
                      <a
                        href={whatsappModal.directUrl}
                        target="_blank"
                        rel="noreferrer"
                        className="btn btn-primary"
                        style={{
                          flex: 1,
                          minWidth: '180px',
                          display: 'inline-flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                          gap: '0.5rem',
                        }}
                      >
                        <span className="nav-icon" style={{ width: '16px', height: '16px' }}>
                          <IconWhatsApp />
                        </span>
                        Abrir no WhatsApp
                      </a>

                      <button
                        type="button"
                        className="btn btn-outline"
                        disabled={whatsappModal.sending || !whatsappModal.phone}
                        onClick={sendAutomatedWhatsApp}
                        style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', gap: '0.5rem' }}
                      >
                        {whatsappModal.sending ? (
                          <>
                            <div className="loading-spinner" style={{ width: '14px', height: '14px' }} />
                            Enviando...
                          </>
                        ) : (
                          'Envio automático pelo WhatsApp'
                        )}
                      </button>
                    </div>

                    <p style={{ fontSize: '0.75rem', color: 'var(--text-muted)', marginTop: '0.25rem', textAlign: 'center' }}>
                      &bull; <strong>Abrir no WhatsApp:</strong> abre o WhatsApp com a mensagem pronta para envio manual.<br />
                      &bull; <strong>Envio automático:</strong> envia a mensagem pelo WhatsApp conectado e acompanha o envio até a confirmação.
                    </p>
                  </>
                )}
              </div>
            </Modal>
          ) : null}

          <WhatsAppConnectModal
            open={connectWhatsappOpen}
            onClose={() => setConnectWhatsappOpen(false)}
            onSuccess={() => {
              void loadWhatsAppStatus();
            }}
          />

          <ConfirmModal
            open={batchPayConfirm}
            title="Confirmar baixa em lote"
            message={`Marcar ${selectedReceipts.size} recibo(s) selecionado(s) como pago(s)?`}
            confirmLabel="Marcar como pagos"
            tone="primary"
            busy={busy}
            onClose={() => (!busy ? setBatchPayConfirm(false) : null)}
            onConfirm={() => {
              setBatchPayConfirm(false);
              const formData = new FormData();
              formData.set('intent', 'receipt.batch-pay');
              formData.set('ids', Array.from(selectedReceipts).join(','));
              void sendMutation(formData, 'Recibos marcados como pagos.', () => {
                setSelectedReceipts(new Set());
              });
            }}
          />

          <ConfirmModal
            open={batchDeleteConfirm}
            title="Excluir recibos em lote"
            message={`Esta ação é permanente e removerá ${selectedReceipts.size} recibo(s) selecionado(s) do banco. Deseja continuar?`}
            confirmLabel={`Excluir ${selectedReceipts.size}`}
            tone="danger"
            busy={busy}
            onClose={() => (!busy ? setBatchDeleteConfirm(false) : null)}
            onConfirm={() => {
              setBatchDeleteConfirm(false);
              const formData = new FormData();
              formData.set('intent', 'receipt.batch-delete');
              formData.set('ids', Array.from(selectedReceipts).join(','));
              void sendMutation(formData, `${selectedReceipts.size} recibo(s) excluído(s).`, () => {
                setSelectedReceipts(new Set());
              });
            }}
          />
        </div>
      </main>
    </div>
  );
}
