import { Form, Link, useFetcher, useLoaderData, useRevalidator } from '@remix-run/react';
import { json } from '@remix-run/node';
import type { ActionFunctionArgs, LoaderFunctionArgs, MetaFunction } from '@remix-run/node';
import React, { useEffect, useMemo, useRef, useState } from 'react';
import { createReceiptPdf } from '../utils/pdf.server';
import { execute, query, queryOne } from '../utils/db.server';
import { requireUserId } from '../utils/session.server';

type Tenant = {
  id: string;
  name: string;
  email: string;
  phone: string | null;
};

type Property = {
  id: string;
  name: string;
  address: string;
  city: string;
  state: string;
  zip: string | null;
};

type Lease = {
  id: string;
  tenant_id: string;
  property_id: string;
  start_date: string;
  end_date: string;
  rent_amount: string;
  tenant_name: string;
  property_name: string;
};

type Receipt = {
  id: string;
  lease_id: string;
  due_date: string;
  paid_at: string | null;
  amount: string;
  pdf_path: string | null;
  tenant_name: string;
  property_name: string;
};

type MutationResponse = { ok?: boolean; error?: string };
type SortDirection = 'asc' | 'desc';
type SortState = { key: string; direction: SortDirection };

const tabs = ['tenants', 'properties', 'leases', 'receipts'] as const;
type Tab = (typeof tabs)[number];

function getTab(url: URL): Tab {
  const tab = url.searchParams.get('tab');
  return tabs.includes(tab as Tab) ? (tab as Tab) : 'tenants';
}

function formatCurrency(value: string | number) {
  return new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' }).format(Number(value));
}

function compareValues(left: string | number | null | undefined, right: string | number | null | undefined, direction: SortDirection) {
  const a = left ?? '';
  const b = right ?? '';
  const result = typeof a === 'number' || typeof b === 'number'
    ? Number(a) - Number(b)
    : String(a).localeCompare(String(b), 'pt-BR', { numeric: true, sensitivity: 'base' });
  return direction === 'asc' ? result : -result;
}

function sortItems<T extends Record<string, unknown>>(items: T[], sort: SortState) {
  return [...items].sort((a, b) => compareValues(a[sort.key] as string | number | null | undefined, b[sort.key] as string | number | null | undefined, sort.direction));
}

function paginateItems<T>(items: T[], page: number, pageSize: number) {
  const start = (page - 1) * pageSize;
  return items.slice(start, start + pageSize);
}

export const meta: MetaFunction = () => [{ title: 'Aluxen | Painel' }];

export async function loader({ request }: LoaderFunctionArgs) {
  const userId = await requireUserId(request);
  const url = new URL(request.url);
  const tab = getTab(url);

  const [user, tenants, properties, leases, receipts] = await Promise.all([
    queryOne<{ username: string }>('SELECT username FROM users WHERE id = $1', [userId]),
    query<Tenant>('SELECT id, name, email, phone FROM tenants ORDER BY created_at DESC, name'),
    query<Property>('SELECT id, name, address, city, state, zip FROM properties ORDER BY created_at DESC, name'),
    query<Lease>(`
      SELECT l.id, l.tenant_id, l.property_id, l.start_date::text, l.end_date::text, l.rent_amount::text,
             t.name AS tenant_name, p.name AS property_name
      FROM leases l
      JOIN tenants t ON t.id = l.tenant_id
      JOIN properties p ON p.id = l.property_id
      ORDER BY l.created_at DESC
    `),
    query<Receipt>(`
      SELECT r.id, r.lease_id, r.due_date::text, r.paid_at::text, r.amount::text, r.pdf_path,
             t.name AS tenant_name, p.name AS property_name
      FROM receipts r
      JOIN leases l ON l.id = r.lease_id
      JOIN tenants t ON t.id = l.tenant_id
      JOIN properties p ON p.id = l.property_id
      ORDER BY r.created_at DESC
    `),
  ]);

  return json({ tab, user: user?.username || 'admin', tenants, properties, leases, receipts });
}

export async function action({ request }: ActionFunctionArgs) {
  await requireUserId(request);
  const formData = await request.formData();
  const intent = String(formData.get('intent') || '');

  try {
    switch (intent) {
      case 'tenant.create': {
        const name = String(formData.get('name') || '').trim();
        const email = String(formData.get('email') || '').trim();
        const phone = String(formData.get('phone') || '').trim();
        if (!name || !email) throw new Error('Nome e email são obrigatórios.');
        await execute('INSERT INTO tenants (id, name, email, phone) VALUES ($1, $2, $3, $4)', [crypto.randomUUID(), name, email, phone || null]);
        break;
      }
      case 'tenant.update': {
        const id = String(formData.get('id') || '');
        const name = String(formData.get('name') || '').trim();
        const email = String(formData.get('email') || '').trim();
        const phone = String(formData.get('phone') || '').trim();
        if (!id || !name || !email) throw new Error('Nome e email são obrigatórios.');
        await execute('UPDATE tenants SET name = $1, email = $2, phone = $3, updated_at = CURRENT_TIMESTAMP WHERE id = $4', [name, email, phone || null, id]);
        break;
      }
      case 'tenant.delete': {
        await execute('DELETE FROM tenants WHERE id = $1', [String(formData.get('id'))]);
        break;
      }
      case 'property.create': {
        const name = String(formData.get('name') || '').trim();
        const address = String(formData.get('address') || '').trim();
        const city = String(formData.get('city') || '').trim();
        const state = String(formData.get('state') || '').trim();
        const zip = String(formData.get('zip') || '').trim();
        if (!name || !address || !city || !state) throw new Error('Preencha nome, endereço, cidade e estado.');
        await execute('INSERT INTO properties (id, name, address, city, state, zip) VALUES ($1, $2, $3, $4, $5, $6)', [crypto.randomUUID(), name, address, city, state, zip || null]);
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
        await execute('UPDATE properties SET name = $1, address = $2, city = $3, state = $4, zip = $5, updated_at = CURRENT_TIMESTAMP WHERE id = $6', [name, address, city, state, zip || null, id]);
        break;
      }
      case 'property.delete': {
        await execute('DELETE FROM properties WHERE id = $1', [String(formData.get('id'))]);
        break;
      }
      case 'lease.create': {
        const tenantId = String(formData.get('tenant_id') || '');
        const propertyId = String(formData.get('property_id') || '');
        const startDate = String(formData.get('start_date') || '');
        const endDate = String(formData.get('end_date') || '');
        const rentAmount = Number(formData.get('rent_amount'));
        if (!tenantId || !propertyId || !startDate || !endDate || !Number.isFinite(rentAmount)) throw new Error('Preencha todos os campos do contrato.');
        await execute('INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, rent_amount) VALUES ($1, $2, $3, $4, $5, $6)', [crypto.randomUUID(), tenantId, propertyId, startDate, endDate, rentAmount]);
        break;
      }
      case 'lease.update': {
        const id = String(formData.get('id') || '');
        const tenantId = String(formData.get('tenant_id') || '');
        const propertyId = String(formData.get('property_id') || '');
        const startDate = String(formData.get('start_date') || '');
        const endDate = String(formData.get('end_date') || '');
        const rentAmount = Number(formData.get('rent_amount'));
        if (!id || !tenantId || !propertyId || !startDate || !endDate || !Number.isFinite(rentAmount)) throw new Error('Preencha todos os campos do contrato.');
        await execute('UPDATE leases SET tenant_id = $1, property_id = $2, start_date = $3, end_date = $4, rent_amount = $5, updated_at = CURRENT_TIMESTAMP WHERE id = $6', [tenantId, propertyId, startDate, endDate, rentAmount, id]);
        break;
      }
      case 'lease.delete': {
        await execute('DELETE FROM leases WHERE id = $1', [String(formData.get('id'))]);
        break;
      }
      case 'receipt.create':
      case 'receipt.update': {
        const id = String(formData.get('id') || crypto.randomUUID());
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

        const pdfPath = await createReceiptPdf({
          receiptId: id,
          tenantName: lease.tenant_name,
          propertyName: lease.property_name,
          dueDate,
          paidAt: paidAt || null,
          amount,
        });

        if (intent === 'receipt.create') {
          await execute('INSERT INTO receipts (id, lease_id, due_date, paid_at, amount, pdf_path) VALUES ($1, $2, $3, $4, $5, $6)', [id, leaseId, dueDate, paidAt || null, amount, pdfPath]);
        } else {
          await execute('UPDATE receipts SET lease_id = $1, due_date = $2, paid_at = $3, amount = $4, pdf_path = $5 WHERE id = $6', [leaseId, dueDate, paidAt || null, amount, pdfPath, id]);
        }
        break;
      }
      case 'receipt.delete': {
        await execute('DELETE FROM receipts WHERE id = $1', [String(formData.get('id'))]);
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

function ThemeToggle({ collapsed }: { collapsed: boolean }) {
  const [theme, setTheme] = useState('light');

  useEffect(() => {
    const stored = window.localStorage.getItem('theme') || 'light';
    setTheme(stored);
    document.documentElement.setAttribute('data-theme', stored);
  }, []);

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
    window.localStorage.setItem('theme', theme);
  }, [theme]);

  return (
    <button className="theme-toggle" type="button" onClick={() => setTheme((current) => current === 'light' ? 'dark' : 'light')}>
      <span className="nav-icon"><IconTheme /></span>
      {!collapsed && <span className="nav-label">Tema</span>}
    </button>
  );
}

function Brand({ collapsed }: { collapsed: boolean }) {
  return (
    <div className="logo">
      <div className="logo-icon" aria-hidden="true">
        <svg className="logo-mark" viewBox="0 0 64 64" fill="none" xmlns="http://www.w3.org/2000/svg">
          <path d="M10 31L32 13L54 31" className="logo-stroke" strokeWidth="4.5" strokeLinecap="round" strokeLinejoin="round"/>
          <path d="M17 29V49C17 51.2 18.8 53 21 53H43C45.2 53 47 51.2 47 49V29" className="logo-stroke" strokeWidth="4.5" strokeLinecap="round" strokeLinejoin="round"/>
          <path d="M26 53V39C26 36.8 27.8 35 30 35H34C36.2 35 38 36.8 38 39V53" className="logo-stroke" strokeWidth="4.5" strokeLinecap="round" strokeLinejoin="round"/>
          <circle cx="32" cy="22" r="3.2" className="logo-fill"/>
        </svg>
      </div>
      {!collapsed ? <div className="logo-text">Alux<span>en</span></div> : null}
    </div>
  );
}

function MenuGlyph({ collapsed }: { collapsed: boolean }) {
  return (
    <svg className="menu-glyph" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      {collapsed ? (
        <>
          <path d="M5 7H19" />
          <path d="M5 12H19" />
          <path d="M5 17H19" />
        </>
      ) : (
        <>
          <path d="M15 7L9 12L15 17" />
        </>
      )}
    </svg>
  );
}

function IconTenants() {
  return (
    <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="8" r="4" />
      <path d="M4 20C4 16.6863 7.58172 14 12 14C16.4183 14 20 16.6863 20 20" />
    </svg>
  );
}

function IconProperties() {
  return (
    <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M3 21H21" />
      <path d="M5 21V7L12 3L19 7V21" />
      <rect x="8" y="13" width="3" height="3" />
      <rect x="13" y="13" width="3" height="3" />
    </svg>
  );
}

function IconLeases() {
  return (
    <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M9 12H15" />
      <path d="M9 16H15" />
      <path d="M6 3V21" />
      <path d="M18 3V21" />
      <path d="M6 21H18C19.1046 21 20 20.1046 20 19V5C20 3.89543 18.6569 3 17 3H7C5.34315 3 4 3.89543 4 5V19C4 20.1046 4.89543 21 6 21Z" />
    </svg>
  );
}

function IconReceipts() {
  return (
    <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M12 2V22" />
      <path d="M17 5H9C7.89543 5 7 5.89543 7 7V17C7 18.1046 7.89543 19 9 19H17C18.1046 19 19 18.1046 19 17V7C19 5.89543 18.1046 5 17 5Z" />
      <path d="M9 9H17" />
    </svg>
  );
}

function IconLogout() {
  return (
    <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M9 21H5C4.46957 21 3.96086 20.7893 3.58579 20.4142C3.21071 20.0391 3 19.5304 3 19V5C3 4.46957 3.21071 3.96086 3.58579 3.58579C3.96086 3.21071 4.46957 3 5 3H9" />
      <path d="M16 17L21 12M21 12L16 7M21 12H9" />
    </svg>
  );
}

function IconTheme() {
  return (
    <svg viewBox="0 0 24 24" width="24" height="24" fill="none" aria-label="Tema" role="img" xmlns="http://www.w3.org/2000/svg">
      <path d="M21 12.79A9 9 0 1 1 11.21 3.01 7 7 0 0 0 21 12.79z" fill="currentColor"/>
    </svg>
  );
}

function DeleteRowButton({ intent, tab, id, onOptimisticDelete, onSettled }: { intent: string; tab: Tab; id: string; onOptimisticDelete: () => void; onSettled: () => void }) {
  const fetcher = useFetcher<MutationResponse>();

  useEffect(() => {
    if (fetcher.state === 'idle' && fetcher.data) {
      onSettled();
    }
  }, [fetcher.state, fetcher.data, onSettled]);

  return (
    <fetcher.Form method="post" onSubmit={(event) => {
      if (!window.confirm('Tem certeza que deseja excluir este item?')) {
        event.preventDefault();
        return;
      }
      onOptimisticDelete();
    }}>
      <input type="hidden" name="intent" value={intent} />
      <input type="hidden" name="tab" value={tab} />
      <input type="hidden" name="id" value={id} />
      <button type="submit" className="btn btn-sm btn-danger" disabled={fetcher.state !== 'idle'}>Excluir</button>
    </fetcher.Form>
  );
}

function Toast({ message, tone, onClose }: { message: string; tone: 'success' | 'error'; onClose: () => void }) {
  useEffect(() => {
    const timer = window.setTimeout(onClose, 2600);
    return () => window.clearTimeout(timer);
  }, [message, onClose]);

  return <div className={`toast ${tone}`}>{message}</div>;
}

function Modal({ title, children, onClose }: { title: string; children: React.ReactNode; onClose: () => void }) {
  return (
    <div className="modal-backdrop" role="presentation" onClick={onClose}>
      <div className="modal-card" role="dialog" aria-modal="true" aria-label={title} onClick={(event) => event.stopPropagation()}>
        <div className="modal-header">
          <h3>{title}</h3>
          <button type="button" className="btn btn-sm btn-secondary" onClick={onClose}>Fechar</button>
        </div>
        {children}
      </div>
    </div>
  );
}

function FilterBar({ value, onChange, placeholder, children }: { value: string; onChange: (value: string) => void; placeholder: string; children?: React.ReactNode }) {
  return (
    <div className="filter-bar">
      <input value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} aria-label={placeholder} />
      {children}
    </div>
  );
}

function DatePickerInput({ id, name, value, onChange, required, label }: { id: string; name: string; value: string; onChange: (value: string) => void; required?: boolean; label: string }) {
  const [isOpen, setIsOpen] = useState(false);
  const [viewDate, setViewDate] = useState(() => value ? new Date(value) : new Date());
  const wrapperRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const daysInMonth = new Date(viewDate.getFullYear(), viewDate.getMonth() + 1, 0).getDate();
  const firstDay = new Date(viewDate.getFullYear(), viewDate.getMonth(), 1).getDay();
  const year = viewDate.getFullYear();
  const month = viewDate.getMonth();

  const days = Array.from({ length: daysInMonth }, (_, i) => i + 1);
  const blanks = Array.from({ length: firstDay }, (_, i) => i);

  const selectDate = (day: number) => {
    const newDate = new Date(year, month, day);
    onChange(newDate.toISOString().split('T')[0]);
    setIsOpen(false);
  };

  const prevMonth = () => setViewDate(new Date(year, month - 1, 1));
  const nextMonth = () => setViewDate(new Date(year, month + 1, 1));

  const monthNames = ['Janeiro', 'Fevereiro', 'Março', 'Abril', 'Maio', 'Junho', 'Julho', 'Agosto', 'Setembro', 'Outubro', 'Novembro', 'Dezembro'];

  const formattedValue = value ? new Date(value).toLocaleDateString('pt-BR') : '';

  return (
    <div className="form-group">
      <label htmlFor={id}>{label}</label>
      <div className="custom-date-picker" ref={wrapperRef}>
        <button
          type="button"
          className="date-picker-display"
          onClick={() => setIsOpen(!isOpen)}
        >
          <span>{formattedValue || 'Selecione...'}</span>
          <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <rect x="3" y="4" width="18" height="18" rx="2" />
            <path d="M16 2V6" />
            <path d="M8 2V6" />
            <path d="M3 10H21" />
          </svg>
        </button>
        {isOpen && (
          <div className="date-picker-dropdown">
            <div className="date-picker-header">
              <button type="button" onClick={prevMonth}>
                <svg viewBox="0 0 24 24" fill="none"><path d="M15 18L9 12L15 6" /></svg>
              </button>
              <span>{monthNames[month]} {year}</span>
              <button type="button" onClick={nextMonth}>
                <svg viewBox="0 0 24 24" fill="none"><path d="M9 18L15 12L9 6" /></svg>
              </button>
            </div>
            <div className="date-picker-weekdays">
              <span>Dom</span><span>Seg</span><span>Ter</span><span>Qua</span><span>Qui</span><span>Sex</span><span>Sáb</span>
            </div>
            <div className="date-picker-days">
              {blanks.map((i) => <span key={`blank-${i}`} className="blank" />)}
              {days.map((day) => {
                const isSelected = value && new Date(year, month, day).toISOString().split('T')[0] === value;
                return (
                  <button
                    key={day}
                    type="button"
                    className={isSelected ? 'selected' : ''}
                    onClick={() => selectDate(day)}
                  >
                    {day}
                  </button>
                );
              })}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function DateTimePickerInput({ id, name, value, onChange, label }: { id: string; name: string; value: string; onChange: (value: string) => void; label: string }) {
  const [isOpen, setIsOpen] = useState(false);
  const [viewDate, setViewDate] = useState(() => value ? new Date(value) : new Date());
  const [showTime, setShowTime] = useState(false);
  const wrapperRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
        setIsOpen(false);
        setShowTime(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const daysInMonth = new Date(viewDate.getFullYear(), viewDate.getMonth() + 1, 0).getDate();
  const firstDay = new Date(viewDate.getFullYear(), viewDate.getMonth(), 1).getDay();
  const year = viewDate.getFullYear();
  const month = viewDate.getMonth();

  const days = Array.from({ length: daysInMonth }, (_, i) => i + 1);
  const blanks = Array.from({ length: firstDay }, (_, i) => i);
  const hours = Array.from({ length: 24 }, (_, i) => i);
  const minutes = Array.from({ length: 60 }, (_, i) => i);

  const currentDate = value ? new Date(value) : new Date();
  const selectedHour = currentDate.getHours();
  const selectedMinute = currentDate.getMinutes();

  const selectDateTime = (day: number, hour?: number, minute?: number) => {
    const newDate = new Date(year, month, day, hour ?? selectedHour, minute ?? selectedMinute);
    onChange(newDate.toISOString().slice(0, 16));
    setIsOpen(false);
    setShowTime(false);
  };

  const prevMonth = () => setViewDate(new Date(year, month - 1, 1));
  const nextMonth = () => setViewDate(new Date(year, month + 1, 1));

  const monthNames = ['Janeiro', 'Fevereiro', 'Março', 'Abril', 'Maio', 'Junho', 'Julho', 'Agosto', 'Setembro', 'Outubro', 'Novembro', 'Dezembro'];

  const formattedValue = value ? new Date(value).toLocaleString('pt-BR') : '';

  return (
    <div className="form-group">
      <label htmlFor={id}>{label}</label>
      <div className="custom-date-picker" ref={wrapperRef}>
        <button
          type="button"
          className="date-picker-display"
          onClick={() => setIsOpen(!isOpen)}
        >
          <span>{formattedValue || 'Selecione...'}</span>
          <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <circle cx="12" cy="12" r="10" />
            <path d="M12 6V12L16 14" />
          </svg>
        </button>
        {isOpen && (
          <div className="date-picker-dropdown datetime">
            <div className="date-picker-header">
              <button type="button" onClick={prevMonth}>
                <svg viewBox="0 0 24 24" fill="none"><path d="M15 18L9 12L15 6" /></svg>
              </button>
              <span>{monthNames[month]} {year}</span>
              <button type="button" onClick={nextMonth}>
                <svg viewBox="0 0 24 24" fill="none"><path d="M9 18L15 12L9 6" /></svg>
              </button>
            </div>
            <div className="date-picker-weekdays">
              <span>Dom</span><span>Seg</span><span>Ter</span><span>Qua</span><span>Qui</span><span>Sex</span><span>Sáb</span>
            </div>
            <div className="date-picker-days">
              {blanks.map((i) => <span key={`blank-${i}`} className="blank" />)}
              {days.map((day) => {
                const isSelected = value && new Date(year, month, day).toISOString().split('T')[0] === value?.split('T')[0];
                return (
                  <button
                    key={day}
                    type="button"
                    className={isSelected ? 'selected' : ''}
                    onClick={() => selectDateTime(day)}
                  >
                    {day}
                  </button>
                );
              })}
            </div>
            <div className="time-picker-section">
              <button type="button" className="time-toggle" onClick={() => setShowTime(!showTime)}>
                <span>Horário</span>
                <svg viewBox="0 0 24 24" fill="none"><path d={showTime ? "M18 15L12 9L6 15" : "M6 9L12 15L18 9"} /></svg>
              </button>
              {showTime && (
                <div className="time-picker-grid">
                  <select 
                    value={selectedHour} 
                    onChange={(e) => selectDateTime(daysInMonth, parseInt(e.target.value), selectedMinute)}
                    className="time-select"
                  >
                    {hours.map(h => <option key={h} value={h}>{h.toString().padStart(2, '0')}</option>)}
                  </select>
                  <span>:</span>
                  <select 
                    value={selectedMinute} 
                    onChange={(e) => selectDateTime(daysInMonth, selectedHour, parseInt(e.target.value))}
                    className="time-select"
                  >
                    {minutes.filter((_, i) => i % 5 === 0).map(m => <option key={m} value={m}>{m.toString().padStart(2, '0')}</option>)}
                  </select>
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function CustomSelect({ id, value, onChange, required, label, children }: { id: string; value: string; onChange: (value: string) => void; required?: boolean; label: string; children: React.ReactNode }) {
  const [isOpen, setIsOpen] = useState(false);
  const wrapperRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const options = React.Children.toArray(children);
  const selectedLabel = options.find((opt) => {
    const optReact = opt as React.ReactElement<{ value: string }>;
    return optReact.props?.value === value;
  });
  const displayText = selectedLabel 
    ? (selectedLabel as React.ReactElement<{ children: React.ReactNode }>)?.props?.children 
    : 'Selecione...';

  return (
    <div className="form-group">
      <label htmlFor={id}>{label}</label>
      <div className="custom-select" ref={wrapperRef}>
        <button
          type="button"
          className="select-display"
          onClick={() => setIsOpen(!isOpen)}
        >
          <span>{displayText}</span>
          <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="m6 9 6 6 6-6" />
          </svg>
        </button>
        {isOpen && (
          <div className="select-dropdown">
            <button
              type="button"
              className={`select-option ${!value ? 'selected' : ''}`}
              onClick={() => { onChange(''); setIsOpen(false); }}
            >
              Selecione...
            </button>
            {options.map((opt, i) => {
              const optReact = opt as React.ReactElement<{ value: string; children: React.ReactNode }>;
              const isSelected = optReact.props.value === value;
              return (
                <button
                  key={i}
                  type="button"
                  className={`select-option ${isSelected ? 'selected' : ''}`}
                  onClick={() => { onChange(optReact.props.value); setIsOpen(false); }}
                >
                  {optReact.props.children}
                </button>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

function SortHeader({ label, active, direction, onClick }: { label: string; active: boolean; direction: SortDirection; onClick: () => void }) {
  return <button type="button" className={`sort-header ${active ? 'active' : ''}`} onClick={onClick}>{label}{active ? (direction === 'asc' ? ' ↑' : ' ↓') : ''}</button>;
}

function SmallSelect({ value, onChange, children }: { value: string; onChange: (value: string) => void; children: React.ReactNode }) {
  const [isOpen, setIsOpen] = useState(false);
  const wrapperRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const options = React.Children.toArray(children);
  const selectedLabel = options.find((opt) => {
    const optReact = opt as React.ReactElement<{ value: string }>;
    return optReact.props?.value === value;
  });
  const displayText = selectedLabel 
    ? (selectedLabel as React.ReactElement<{ children: React.ReactNode }>)?.props?.children 
    : value;

  return (
    <div className="small-select" ref={wrapperRef}>
      <button
        type="button"
        className="small-select-display"
        onClick={() => setIsOpen(!isOpen)}
      >
        <span>{displayText}</span>
        <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>
      {isOpen && (
        <div className="small-select-dropdown">
          {options.map((opt, i) => {
            const optReact = opt as React.ReactElement<{ value: string; children: React.ReactNode }>;
            const isSelected = optReact.props.value === value;
            return (
              <button
                key={i}
                type="button"
                className={`small-select-option ${isSelected ? 'selected' : ''}`}
                onClick={() => { onChange(optReact.props.value); setIsOpen(false); }}
              >
                {optReact.props.children}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

function Pagination({ page, totalPages, onChange }: { page: number; totalPages: number; onChange: (page: number) => void }) {
  if (totalPages <= 1) return null;
  return (
    <div className="pagination">
      <button type="button" className="btn btn-sm btn-secondary" onClick={() => onChange(Math.max(1, page - 1))} disabled={page === 1}>Anterior</button>
      <span className="pagination-label">Página {page} de {totalPages}</span>
      <button type="button" className="btn btn-sm btn-secondary" onClick={() => onChange(Math.min(totalPages, page + 1))} disabled={page === totalPages}>Próxima</button>
    </div>
  );
}

export default function AppRoute() {
  const { tab, user, tenants, properties, leases, receipts } = useLoaderData<typeof loader>();
  const revalidator = useRevalidator();
  const [collapsed, setCollapsed] = useState(true);

  const [tenantItems, setTenantItems] = useState(tenants);
  const [propertyItems, setPropertyItems] = useState(properties);
  const [leaseItems, setLeaseItems] = useState(leases);
  const [receiptItems, setReceiptItems] = useState(receipts);

  const [tenantForm, setTenantForm] = useState({ name: '', email: '', phone: '' });
  const [propertyForm, setPropertyForm] = useState({ name: '', address: '', city: '', state: '', zip: '' });
  const [leaseForm, setLeaseForm] = useState({ tenant_id: '', property_id: '', start_date: '', end_date: '', rent_amount: '' });
  const [receiptForm, setReceiptForm] = useState({ lease_id: '', due_date: '', paid_at: '', amount: '' });
  const [tenantDraft, setTenantDraft] = useState({ id: '', name: '', email: '', phone: '' });
  const [propertyDraft, setPropertyDraft] = useState({ id: '', name: '', address: '', city: '', state: '', zip: '' });
  const [leaseDraft, setLeaseDraft] = useState({ id: '', tenant_id: '', property_id: '', start_date: '', end_date: '', rent_amount: '' });
  const [receiptDraft, setReceiptDraft] = useState({ id: '', lease_id: '', due_date: '', paid_at: '', amount: '' });
  const [modalType, setModalType] = useState<'' | Tab>('');
  const [createModalType, setCreateModalType] = useState<'' | Tab>('');
  const [toast, setToast] = useState<{ message: string; tone: 'success' | 'error' } | null>(null);
  const [tenantSearch, setTenantSearch] = useState('');
  const [propertySearch, setPropertySearch] = useState('');
  const [leaseSearch, setLeaseSearch] = useState('');
  const [receiptSearch, setReceiptSearch] = useState('');
  const [receiptStatus, setReceiptStatus] = useState<'all' | 'paid' | 'pending'>('all');
  const [tenantSort, setTenantSort] = useState<SortState>({ key: 'name', direction: 'asc' });
  const [propertySort, setPropertySort] = useState<SortState>({ key: 'name', direction: 'asc' });
  const [leaseSort, setLeaseSort] = useState<SortState>({ key: 'tenant_name', direction: 'asc' });
  const [receiptSort, setReceiptSort] = useState<SortState>({ key: 'due_date', direction: 'desc' });
  const [tenantPage, setTenantPage] = useState(1);
  const [propertyPage, setPropertyPage] = useState(1);
  const [leasePage, setLeasePage] = useState(1);
  const [receiptPage, setReceiptPage] = useState(1);

  const tenantFetcher = useFetcher<MutationResponse>();
  const propertyFetcher = useFetcher<MutationResponse>();
  const leaseFetcher = useFetcher<MutationResponse>();
  const receiptFetcher = useFetcher<MutationResponse>();
  const lastHandledTenantResponse = useRef<MutationResponse | undefined>(undefined);
  const lastHandledPropertyResponse = useRef<MutationResponse | undefined>(undefined);
  const lastHandledLeaseResponse = useRef<MutationResponse | undefined>(undefined);
  const lastHandledReceiptResponse = useRef<MutationResponse | undefined>(undefined);

  useEffect(() => setTenantItems(tenants), [tenants]);
  useEffect(() => setPropertyItems(properties), [properties]);
  useEffect(() => setLeaseItems(leases), [leases]);
  useEffect(() => setReceiptItems(receipts), [receipts]);

  useEffect(() => {
    if (tenantFetcher.state !== 'idle' || !tenantFetcher.data || lastHandledTenantResponse.current === tenantFetcher.data) {
      return;
    }

    lastHandledTenantResponse.current = tenantFetcher.data;
    revalidator.revalidate();
    if (tenantFetcher.data.ok) {
      setTenantForm({ name: '', email: '', phone: '' });
      setTenantDraft({ id: '', name: '', email: '', phone: '' });
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
      setLeaseForm({ tenant_id: '', property_id: '', start_date: '', end_date: '', rent_amount: '' });
      setLeaseDraft({ id: '', tenant_id: '', property_id: '', start_date: '', end_date: '', rent_amount: '' });
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
      setToast({ message: 'Recibo salvo com sucesso.', tone: 'success' });
    } else if (receiptFetcher.data.error) {
      setToast({ message: receiptFetcher.data.error, tone: 'error' });
    }
  }, [receiptFetcher.state, receiptFetcher.data, revalidator]);

  const filteredTenants = useMemo(() => tenantItems.filter((item) => `${item.name} ${item.email} ${item.phone || ''}`.toLowerCase().includes(tenantSearch.toLowerCase())), [tenantItems, tenantSearch]);
  const filteredProperties = useMemo(() => propertyItems.filter((item) => `${item.name} ${item.city} ${item.state} ${item.address}`.toLowerCase().includes(propertySearch.toLowerCase())), [propertyItems, propertySearch]);
  const filteredLeases = useMemo(() => leaseItems.filter((item) => `${item.tenant_name} ${item.property_name} ${item.start_date} ${item.end_date}`.toLowerCase().includes(leaseSearch.toLowerCase())), [leaseItems, leaseSearch]);
  const filteredReceipts = useMemo(() => receiptItems.filter((item) => {
    const matchesText = `${item.tenant_name} ${item.property_name} ${item.due_date} ${item.amount}`.toLowerCase().includes(receiptSearch.toLowerCase());
    const matchesStatus = receiptStatus === 'all' || (receiptStatus === 'paid' ? Boolean(item.paid_at) : !item.paid_at);
    return matchesText && matchesStatus;
  }), [receiptItems, receiptSearch, receiptStatus]);
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
  useEffect(() => setLeasePage(1), [leaseSearch, leaseSort]);
  useEffect(() => setReceiptPage(1), [receiptSearch, receiptStatus, receiptSort]);
  useEffect(() => setTenantPage((page) => Math.min(page, tenantTotalPages)), [tenantTotalPages]);
  useEffect(() => setPropertyPage((page) => Math.min(page, propertyTotalPages)), [propertyTotalPages]);
  useEffect(() => setLeasePage((page) => Math.min(page, leaseTotalPages)), [leaseTotalPages]);
  useEffect(() => setReceiptPage((page) => Math.min(page, receiptTotalPages)), [receiptTotalPages]);

  function settleAfterMutation() {
    revalidator.revalidate();
  }

  function startTenantEdit(item: Tenant) {
    setTenantDraft({ id: item.id, name: item.name, email: item.email, phone: item.phone || '' });
    setModalType('tenants');
  }

  function startPropertyEdit(item: Property) {
    setPropertyDraft({ id: item.id, name: item.name, address: item.address, city: item.city, state: item.state, zip: item.zip || '' });
    setModalType('properties');
  }

  function startLeaseEdit(item: Lease) {
    setLeaseDraft({ id: item.id, tenant_id: item.tenant_id, property_id: item.property_id, start_date: item.start_date, end_date: item.end_date, rent_amount: item.rent_amount });
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
      <nav className={collapsed ? 'sidebar' : 'sidebar open'}>
        <Brand collapsed={collapsed} />
        <button className="menu-btn" type="button" onClick={() => setCollapsed((value) => !value)} aria-label={collapsed ? 'Expandir menu' : 'Recolher menu'}>
          <MenuGlyph collapsed={collapsed} />
        </button>
<ul>
            <li><Link to="/dashboard?tab=tenants"><span className="nav-icon"><IconTenants /></span>{!collapsed ? <span className="nav-label">Inquilinos</span> : null}</Link></li>
            <li><Link to="/dashboard?tab=properties"><span className="nav-icon"><IconProperties /></span>{!collapsed ? <span className="nav-label">Imóveis</span> : null}</Link></li>
            <li><Link to="/dashboard?tab=leases"><span className="nav-icon"><IconLeases /></span>{!collapsed ? <span className="nav-label">Contratos</span> : null}</Link></li>
            <li><Link to="/dashboard?tab=receipts"><span className="nav-icon"><IconReceipts /></span>{!collapsed ? <span className="nav-label">Recibos</span> : null}</Link></li>
          </ul>
          <div className="sidebar-footer">
            <ThemeToggle collapsed={collapsed} />
            <Form action="/logout" method="post">
              <button type="submit"><span className="nav-icon"><IconLogout /></span>{!collapsed ? <span className="nav-label">Sair</span> : null}</button>
            </Form>
          </div>
      </nav>

        <main className={collapsed ? '' : 'sidebar-open'}>
          <div className="page">
            <div className="card page-hero">
              <div>
                <p className="eyebrow">Painel</p>
                <h1>{tab === 'tenants' ? 'Inquilinos' : tab === 'properties' ? 'Imóveis' : tab === 'leases' ? 'Contratos' : 'Recibos'}</h1>
                <p className="subtitle">Sessão ativa para <strong>{user}</strong>.</p>
              </div>
            </div>
            {toast ? <Toast message={toast.message} tone={toast.tone} onClose={() => setToast(null)} /> : null}

            {tab === 'tenants' ? (
              <section className="card stack-lg">
                <div className="section-header"><h2>Gerenciar Inquilinos</h2><button type="button" className="btn btn-primary" onClick={() => setCreateModalType('tenants')}>Novo inquilino</button></div>
                <FilterBar value={tenantSearch} onChange={setTenantSearch} placeholder="Buscar por nome, email ou telefone" />
                <div className="table-wrap">
                  <table>
                    <thead><tr><th><SortHeader label="Nome" active={tenantSort.key === 'name'} direction={tenantSort.direction} onClick={() => toggleSort(tenantSort, 'name', setTenantSort)} /></th><th><SortHeader label="Email" active={tenantSort.key === 'email'} direction={tenantSort.direction} onClick={() => toggleSort(tenantSort, 'email', setTenantSort)} /></th><th><SortHeader label="Telefone" active={tenantSort.key === 'phone'} direction={tenantSort.direction} onClick={() => toggleSort(tenantSort, 'phone', setTenantSort)} /></th><th>Ações</th></tr></thead>
                    <tbody>
                      {pagedTenants.length ? pagedTenants.map((tenant) => (
                        <tr key={tenant.id}>
                          <td data-label="Nome">{tenant.name}</td>
                          <td data-label="Email">{tenant.email}</td>
                          <td data-label="Telefone">{tenant.phone || '-'}</td>
                          <td className="row-actions">
                            <button type="button" className="btn btn-sm btn-secondary" onClick={() => startTenantEdit(tenant)}>Editar</button>
                            <DeleteRowButton intent="tenant.delete" tab="tenants" id={tenant.id} onOptimisticDelete={() => setTenantItems((current) => current.filter((row) => row.id !== tenant.id))} onSettled={settleAfterMutation} />
                          </td>
                        </tr>
                      )) : <tr><td colSpan={4}>Nenhum inquilino cadastrado.</td></tr>}
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
                          <td data-label="Nome">{property.name}</td>
                          <td data-label="Cidade">{property.city}</td>
                          <td data-label="Estado">{property.state}</td>
                          <td className="row-actions">
                            <button type="button" className="btn btn-sm btn-secondary" onClick={() => startPropertyEdit(property)}>Editar</button>
                            <DeleteRowButton intent="property.delete" tab="properties" id={property.id} onOptimisticDelete={() => setPropertyItems((current) => current.filter((row) => row.id !== property.id))} onSettled={settleAfterMutation} />
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
                <FilterBar value={leaseSearch} onChange={setLeaseSearch} placeholder="Buscar por inquilino, imóvel ou período" />
                <div className="table-wrap">
                  <table>
                    <thead><tr><th><SortHeader label="Inquilino" active={leaseSort.key === 'tenant_name'} direction={leaseSort.direction} onClick={() => toggleSort(leaseSort, 'tenant_name', setLeaseSort)} /></th><th><SortHeader label="Imóvel" active={leaseSort.key === 'property_name'} direction={leaseSort.direction} onClick={() => toggleSort(leaseSort, 'property_name', setLeaseSort)} /></th><th><SortHeader label="Início" active={leaseSort.key === 'start_date'} direction={leaseSort.direction} onClick={() => toggleSort(leaseSort, 'start_date', setLeaseSort)} /></th><th><SortHeader label="Valor" active={leaseSort.key === 'rent_amount'} direction={leaseSort.direction} onClick={() => toggleSort(leaseSort, 'rent_amount', setLeaseSort)} /></th><th>Ações</th></tr></thead>
                    <tbody>
                      {pagedLeases.length ? pagedLeases.map((lease) => (
                        <tr key={lease.id}>
                          <td data-label="Inquilino">{lease.tenant_name}</td>
                          <td data-label="Imóvel">{lease.property_name}</td>
                          <td data-label="Período">{lease.start_date} a {lease.end_date}</td>
                          <td data-label="Valor">{formatCurrency(lease.rent_amount)}</td>
                          <td className="row-actions">
                            <button type="button" className="btn btn-sm btn-secondary" onClick={() => startLeaseEdit(lease)}>Editar</button>
                            <DeleteRowButton intent="lease.delete" tab="leases" id={lease.id} onOptimisticDelete={() => setLeaseItems((current) => current.filter((row) => row.id !== lease.id))} onSettled={settleAfterMutation} />
                          </td>
                        </tr>
                      )) : <tr><td colSpan={5}>Cadastre um inquilino e um imóvel para criar contratos.</td></tr>}
                    </tbody>
                  </table>
                </div>
                <Pagination page={leasePage} totalPages={leaseTotalPages} onChange={setLeasePage} />
              </section>
            ) : null}

            {tab === 'receipts' ? (
              <section className="card stack-lg receipts-section">
                <div className="section-header"><h2>Gerenciar Recebimentos</h2><button type="button" className="btn btn-primary" onClick={() => setCreateModalType('receipts')}>Novo recibo</button></div>
                <FilterBar value={receiptSearch} onChange={setReceiptSearch} placeholder="Buscar por contrato, imóvel, vencimento ou valor">
                  <SmallSelect value={receiptStatus} onChange={(v) => setReceiptStatus(v as 'all' | 'paid' | 'pending')}>
                    <option value="all">Todos</option>
                    <option value="paid">Pago</option>
                    <option value="pending">Pendente</option>
                  </SmallSelect>
                </FilterBar>
                <div className="table-wrap">
                  <table className="receipts-table">
                    <thead><tr><th><SortHeader label="Contrato" active={receiptSort.key === 'tenant_name'} direction={receiptSort.direction} onClick={() => toggleSort(receiptSort, 'tenant_name', setReceiptSort)} /></th><th><SortHeader label="Status" active={receiptSort.key === 'paid_at'} direction={receiptSort.direction} onClick={() => toggleSort(receiptSort, 'paid_at', setReceiptSort)} /></th><th><SortHeader label="Vencimento" active={receiptSort.key === 'due_date'} direction={receiptSort.direction} onClick={() => toggleSort(receiptSort, 'due_date', setReceiptSort)} /></th><th><SortHeader label="Pagamento" active={receiptSort.key === 'paid_at'} direction={receiptSort.direction} onClick={() => toggleSort(receiptSort, 'paid_at', setReceiptSort)} /></th><th><SortHeader label="Valor" active={receiptSort.key === 'amount'} direction={receiptSort.direction} onClick={() => toggleSort(receiptSort, 'amount', setReceiptSort)} /></th><th>PDF</th><th>Ações</th></tr></thead>
                    <tbody>
                      {pagedReceipts.length ? pagedReceipts.map((receipt) => (
                        <tr key={receipt.id}>
                          <td data-label="Contrato" className="contract-cell"><span>{receipt.tenant_name}</span><span>{receipt.property_name}</span></td>
                          <td data-label="Status"><span className={`status-badge ${receipt.paid_at ? 'paid' : 'pending'}`}>{receipt.paid_at ? 'Pago' : 'Pendente'}</span></td>
                          <td data-label="Vencimento">{receipt.due_date}</td>
                          <td data-label="Pagamento">{receipt.paid_at || '-'}</td>
                          <td data-label="Valor">{formatCurrency(receipt.amount)}</td>
                          <td data-label="PDF">{receipt.pdf_path ? <a className="btn btn-sm btn-secondary" href={receipt.pdf_path} target="_blank" rel="noreferrer">Abrir PDF</a> : '-'}</td>
                          <td className="row-actions">
                            <button type="button" className="btn btn-sm btn-secondary" onClick={() => startReceiptEdit(receipt)}>Editar</button>
                            <DeleteRowButton intent="receipt.delete" tab="receipts" id={receipt.id} onOptimisticDelete={() => setReceiptItems((current) => current.filter((row) => row.id !== receipt.id))} onSettled={settleAfterMutation} />
                          </td>
                        </tr>
                      )) : <tr><td colSpan={7}>Cadastre um contrato para gerar recibos.</td></tr>}
                    </tbody>
                  </table>
                </div>
                <Pagination page={receiptPage} totalPages={receiptTotalPages} onChange={setReceiptPage} />
              </section>
            ) : null}

            {modalType === 'tenants' ? <Modal title="Editar Inquilino" onClose={() => setModalType('')}>
              <tenantFetcher.Form method="post" className="stack" onSubmit={() => setTenantItems((current) => current.map((row) => row.id === tenantDraft.id ? { ...row, name: tenantDraft.name, email: tenantDraft.email, phone: tenantDraft.phone || null } : row))}>
                <input type="hidden" name="intent" value="tenant.update" />
                <input type="hidden" name="id" value={tenantDraft.id} />
                <div className="form-group"><label htmlFor="modal-tenant-name">Nome</label><input id="modal-tenant-name" name="name" value={tenantDraft.name} onChange={(e) => setTenantDraft((current) => ({ ...current, name: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="modal-tenant-email">Email</label><input id="modal-tenant-email" name="email" type="email" value={tenantDraft.email} onChange={(e) => setTenantDraft((current) => ({ ...current, email: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="modal-tenant-phone">Telefone</label><input id="modal-tenant-phone" name="phone" value={tenantDraft.phone} onChange={(e) => setTenantDraft((current) => ({ ...current, phone: e.target.value }))} /></div>
                <button type="submit" className="btn btn-primary" disabled={tenantFetcher.state !== 'idle'}>Salvar alterações</button>
              </tenantFetcher.Form>
            </Modal> : null}

            {createModalType === 'tenants' ? <Modal title="Novo Inquilino" onClose={() => setCreateModalType('')}>
              <tenantFetcher.Form method="post" className="stack" onSubmit={() => {
                const item = { id: `tmp-${crypto.randomUUID()}`, name: tenantForm.name, email: tenantForm.email, phone: tenantForm.phone || null };
                setTenantItems((current) => [item, ...current]);
              }}>
                <input type="hidden" name="intent" value="tenant.create" />
                <input type="hidden" name="tab" value="tenants" />
                <div className="form-group"><label htmlFor="create-tenant-name">Nome</label><input id="create-tenant-name" name="name" value={tenantForm.name} onChange={(e) => setTenantForm((current) => ({ ...current, name: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="create-tenant-email">Email</label><input id="create-tenant-email" name="email" type="email" value={tenantForm.email} onChange={(e) => setTenantForm((current) => ({ ...current, email: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="create-tenant-phone">Telefone</label><input id="create-tenant-phone" name="phone" value={tenantForm.phone} onChange={(e) => setTenantForm((current) => ({ ...current, phone: e.target.value }))} /></div>
                <button type="submit" className="btn btn-primary" disabled={tenantFetcher.state !== 'idle'}>Adicionar inquilino</button>
              </tenantFetcher.Form>
            </Modal> : null}

            {modalType === 'properties' ? <Modal title="Editar Imóvel" onClose={() => setModalType('')}>
              <propertyFetcher.Form method="post" className="stack" onSubmit={() => setPropertyItems((current) => current.map((row) => row.id === propertyDraft.id ? { ...row, name: propertyDraft.name, address: propertyDraft.address, city: propertyDraft.city, state: propertyDraft.state, zip: propertyDraft.zip || null } : row))}>
                <input type="hidden" name="intent" value="property.update" />
                <input type="hidden" name="id" value={propertyDraft.id} />
                <div className="form-group"><label htmlFor="modal-property-name">Nome</label><input id="modal-property-name" name="name" value={propertyDraft.name} onChange={(e) => setPropertyDraft((current) => ({ ...current, name: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="modal-property-address">Endereço</label><input id="modal-property-address" name="address" value={propertyDraft.address} onChange={(e) => setPropertyDraft((current) => ({ ...current, address: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="modal-property-city">Cidade</label><input id="modal-property-city" name="city" value={propertyDraft.city} onChange={(e) => setPropertyDraft((current) => ({ ...current, city: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="modal-property-state">Estado</label><input id="modal-property-state" name="state" value={propertyDraft.state} onChange={(e) => setPropertyDraft((current) => ({ ...current, state: e.target.value }))} required /></div>
                <div className="form-group"><label htmlFor="modal-property-zip">CEP</label><input id="modal-property-zip" name="zip" value={propertyDraft.zip} onChange={(e) => setPropertyDraft((current) => ({ ...current, zip: e.target.value }))} /></div>
                <button type="submit" className="btn btn-primary" disabled={propertyFetcher.state !== 'idle'}>Salvar alterações</button>
              </propertyFetcher.Form>
            </Modal> : null}

            {createModalType === 'properties' ? <Modal title="Novo Imóvel" onClose={() => setCreateModalType('')}>
              <propertyFetcher.Form method="post" className="stack" onSubmit={() => {
                const item = { id: `tmp-${crypto.randomUUID()}`, name: propertyForm.name, address: propertyForm.address, city: propertyForm.city, state: propertyForm.state, zip: propertyForm.zip || null };
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
              <leaseFetcher.Form method="post" className="stack" onSubmit={() => {
                const tenant = tenantItems.find((item) => item.id === leaseDraft.tenant_id);
                const property = propertyItems.find((item) => item.id === leaseDraft.property_id);
                setLeaseItems((current) => current.map((row) => row.id === leaseDraft.id ? { ...row, tenant_id: leaseDraft.tenant_id, property_id: leaseDraft.property_id, start_date: leaseDraft.start_date, end_date: leaseDraft.end_date, rent_amount: leaseDraft.rent_amount, tenant_name: tenant?.name || '-', property_name: property?.name || '-' } : row));
              }}>
                <input type="hidden" name="intent" value="lease.update" />
                <input type="hidden" name="id" value={leaseDraft.id} />
                <CustomSelect id="modal-lease-tenant" value={leaseDraft.tenant_id} onChange={(v) => setLeaseDraft((current) => ({ ...current, tenant_id: v }))} label="Inquilino" required>
                  <option value="">Selecione</option>
                  {tenantItems.map((tenant) => <option key={tenant.id} value={tenant.id}>{tenant.name}</option>)}
                </CustomSelect>
                <CustomSelect id="modal-lease-property" value={leaseDraft.property_id} onChange={(v) => setLeaseDraft((current) => ({ ...current, property_id: v }))} label="Imóvel" required>
                  <option value="">Selecione</option>
                  {propertyItems.map((property) => <option key={property.id} value={property.id}>{property.name}</option>)}
                </CustomSelect>
                <DatePickerInput id="modal-lease-start" name="start_date" value={leaseDraft.start_date} onChange={(v) => setLeaseDraft((current) => ({ ...current, start_date: v }))} label="Início" required />
                <DatePickerInput id="modal-lease-end" name="end_date" value={leaseDraft.end_date} onChange={(v) => setLeaseDraft((current) => ({ ...current, end_date: v }))} label="Fim" required />
                <div className="form-group"><label htmlFor="modal-lease-rent">Aluguel</label><input id="modal-lease-rent" name="rent_amount" type="number" step="0.01" value={leaseDraft.rent_amount} onChange={(e) => setLeaseDraft((current) => ({ ...current, rent_amount: e.target.value }))} required /></div>
                <button type="submit" className="btn btn-primary" disabled={leaseFetcher.state !== 'idle'}>Salvar alterações</button>
              </leaseFetcher.Form>
            </Modal> : null}

            {createModalType === 'leases' ? <Modal title="Novo Contrato" onClose={() => setCreateModalType('')}>
              <leaseFetcher.Form method="post" className="stack" onSubmit={() => {
                const tenant = tenantItems.find((item) => item.id === leaseForm.tenant_id);
                const property = propertyItems.find((item) => item.id === leaseForm.property_id);
                const item = { id: `tmp-${crypto.randomUUID()}`, tenant_id: leaseForm.tenant_id, property_id: leaseForm.property_id, start_date: leaseForm.start_date, end_date: leaseForm.end_date, rent_amount: leaseForm.rent_amount, tenant_name: tenant?.name || '-', property_name: property?.name || '-' };
                setLeaseItems((current) => [item, ...current]);
              }}>
                <input type="hidden" name="intent" value="lease.create" />
                <input type="hidden" name="tab" value="leases" />
                <CustomSelect id="create-lease-tenant" value={leaseForm.tenant_id} onChange={(v) => setLeaseForm((current) => ({ ...current, tenant_id: v }))} label="Inquilino" required>
                  <option value="">Selecione</option>
                  {tenantItems.map((tenant) => <option key={tenant.id} value={tenant.id}>{tenant.name}</option>)}
                </CustomSelect>
                <CustomSelect id="create-lease-property" value={leaseForm.property_id} onChange={(v) => setLeaseForm((current) => ({ ...current, property_id: v }))} label="Imóvel" required>
                  <option value="">Selecione</option>
                  {propertyItems.map((property) => <option key={property.id} value={property.id}>{property.name}</option>)}
                </CustomSelect>
                <DatePickerInput id="create-lease-start" name="start_date" value={leaseForm.start_date} onChange={(v) => setLeaseForm((current) => ({ ...current, start_date: v }))} label="Início" required />
                <DatePickerInput id="create-lease-end" name="end_date" value={leaseForm.end_date} onChange={(v) => setLeaseForm((current) => ({ ...current, end_date: v }))} label="Fim" required />
                <div className="form-group"><label htmlFor="create-lease-rent">Aluguel</label><input id="create-lease-rent" name="rent_amount" type="number" step="0.01" value={leaseForm.rent_amount} onChange={(e) => setLeaseForm((current) => ({ ...current, rent_amount: e.target.value }))} required /></div>
                <button type="submit" className="btn btn-primary" disabled={leaseFetcher.state !== 'idle'}>Adicionar contrato</button>
              </leaseFetcher.Form>
            </Modal> : null}

            {modalType === 'receipts' ? <Modal title="Editar Recibo" onClose={() => setModalType('')}>
              <receiptFetcher.Form method="post" className="stack" onSubmit={() => {
                const lease = receiptLeaseMeta(receiptDraft.lease_id);
                setReceiptItems((current) => current.map((row) => row.id === receiptDraft.id ? { ...row, lease_id: receiptDraft.lease_id, due_date: receiptDraft.due_date, paid_at: receiptDraft.paid_at || null, amount: receiptDraft.amount, tenant_name: lease?.tenant_name || '-', property_name: lease?.property_name || '-' } : row));
              }}>
                <input type="hidden" name="intent" value="receipt.update" />
                <input type="hidden" name="id" value={receiptDraft.id} />
                <CustomSelect id="modal-receipt-lease" value={receiptDraft.lease_id} onChange={(v) => setReceiptDraft((current) => ({ ...current, lease_id: v }))} label="Contrato" required>
                  <option value="">Selecione</option>
                  {leaseItems.map((lease) => <option key={lease.id} value={lease.id}>{lease.tenant_name} / {lease.property_name}</option>)}
                </CustomSelect>
                <DatePickerInput id="modal-receipt-due" name="due_date" value={receiptDraft.due_date} onChange={(v) => setReceiptDraft((current) => ({ ...current, due_date: v }))} label="Vencimento" required />
                <DateTimePickerInput id="modal-receipt-paid" name="paid_at" value={receiptDraft.paid_at} onChange={(v) => setReceiptDraft((current) => ({ ...current, paid_at: v }))} label="Pago em" />
                <div className="form-group"><label htmlFor="modal-receipt-amount">Valor</label><input id="modal-receipt-amount" name="amount" type="number" step="0.01" value={receiptDraft.amount} onChange={(e) => setReceiptDraft((current) => ({ ...current, amount: e.target.value }))} required /></div>
                <button type="submit" className="btn btn-primary" disabled={receiptFetcher.state !== 'idle'}>Salvar alterações</button>
              </receiptFetcher.Form>
            </Modal> : null}

            {createModalType === 'receipts' ? <Modal title="Novo Recibo" onClose={() => setCreateModalType('')}>
              <receiptFetcher.Form method="post" className="stack" onSubmit={() => {
                const lease = receiptLeaseMeta(receiptForm.lease_id);
                const item = { id: `tmp-${crypto.randomUUID()}`, lease_id: receiptForm.lease_id, due_date: receiptForm.due_date, paid_at: receiptForm.paid_at || null, amount: receiptForm.amount, pdf_path: null, tenant_name: lease?.tenant_name || '-', property_name: lease?.property_name || '-' };
                setReceiptItems((current) => [item, ...current]);
              }}>
                <input type="hidden" name="intent" value="receipt.create" />
                <input type="hidden" name="tab" value="receipts" />
                <CustomSelect id="create-receipt-lease" value={receiptForm.lease_id} onChange={(v) => setReceiptForm((current) => ({ ...current, lease_id: v }))} label="Contrato" required>
                  <option value="">Selecione</option>
                  {leaseItems.map((lease) => <option key={lease.id} value={lease.id}>{lease.tenant_name} / {lease.property_name}</option>)}
                </CustomSelect>
                <DatePickerInput id="create-receipt-due" name="due_date" value={receiptForm.due_date} onChange={(v) => setReceiptForm((current) => ({ ...current, due_date: v }))} label="Vencimento" required />
                <DateTimePickerInput id="create-receipt-paid" name="paid_at" value={receiptForm.paid_at} onChange={(v) => setReceiptForm((current) => ({ ...current, paid_at: v }))} label="Pago em" />
                <div className="form-group"><label htmlFor="create-receipt-amount">Valor</label><input id="create-receipt-amount" name="amount" type="number" step="0.01" value={receiptForm.amount} onChange={(e) => setReceiptForm((current) => ({ ...current, amount: e.target.value }))} required /></div>
                <button type="submit" className="btn btn-primary" disabled={receiptFetcher.state !== 'idle'}>Adicionar recibo</button>
              </receiptFetcher.Form>
            </Modal> : null}
          </div>
        </main>
      </div>
  );
}
