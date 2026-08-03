import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { Form, useFetcher } from '@remix-run/react';
import type { Tab, QueuePayload, MutationResponse, SortDirection } from '../types/dashboard';
import { formatMoneyInput, normalizeMoneyInput, formatPhoneInput, formatCpfInput, parseLocalDate, formatLocalDate, parseLocalDateTime, formatLocalDateTime } from '../utils/format';
import { IconExport, IconImport } from './icons';

export function LoadingRow({ colSpan }: { colSpan: number }) {
  return (
    <tr><td colSpan={colSpan} style={{ textAlign: 'center', padding: '2rem', color: 'var(--muted)' }}>
      <div className="loading-spinner" />
      <span style={{ marginLeft: '0.5rem' }}>Carregando...</span>
    </td></tr>
  );
}

export function DataButtons({ collapsed }: { collapsed: boolean }) {
  return (
    <>
      <a href="/export" className="data-btn" download>
        <span className="nav-icon"><IconExport /></span>
        {!collapsed && <span className="nav-label">Exportar</span>}
      </a>
      <Form action="/import" method="post" encType="multipart/form-data">
        <label className="data-btn data-btn-label">
          <input type="file" name="file" accept=".json" onChange={(e) => {
            if (e.currentTarget.files?.length && window.confirm('Importar dados do arquivo? Isso adicionará novos registros.')) {
              e.currentTarget.form?.submit();
            }
          }} hidden />
          <span className="nav-icon"><IconImport /></span>
          {!collapsed && <span className="nav-label">Importar</span>}
        </label>
      </Form>
    </>
  );
}

export function ThemeToggle({ collapsed }: { collapsed: boolean }) {
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

function IconTheme() {
  return (
    <svg viewBox="0 0 24 24" width="24" height="24" fill="none" aria-label="Tema" role="img" xmlns="http://www.w3.org/2000/svg">
      <path d="M21 12.79A9 9 0 1 1 11.21 3.01 7 7 0 0 0 21 12.79z" fill="currentColor"/>
    </svg>
  );
}

export function Brand({ collapsed }: { collapsed: boolean }) {
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

export function MenuGlyph({ collapsed }: { collapsed: boolean }) {
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

export function DeleteRowButton({
  intent,
  tab,
  id,
  onOptimisticDelete,
  onSettled,
  queueOfflineMutation,
  confirmMessage,
  btnLabel,
  btnClass,
  disabled,
  disabledTitle,
}: {
  intent: string;
  tab: Tab;
  id: string;
  onOptimisticDelete: () => void;
  onSettled: () => void;
  queueOfflineMutation: (payload: QueuePayload, successMessage: string) => void;
  confirmMessage?: string;
  btnLabel?: string;
  btnClass?: string;
  disabled?: boolean;
  disabledTitle?: string;
}) {
  const fetcher = useFetcher<MutationResponse>();

  useEffect(() => {
    if (fetcher.state === 'idle' && fetcher.data) {
      if (fetcher.data.ok) {
        onOptimisticDelete();
      }
      onSettled();
    }
  }, [fetcher.state, fetcher.data, onOptimisticDelete, onSettled]);

  return (
    <fetcher.Form method="post" onSubmit={(event) => {
      if (!window.confirm(confirmMessage || 'Tem certeza que deseja excluir este item?')) {
        event.preventDefault();
        return;
      }

      if (!window.navigator.onLine) {
        event.preventDefault();
        onOptimisticDelete();
        onSettled();
        queueOfflineMutation({ intent, tab, id }, 'Ação pendente de sincronização.');
      }
    }}>
      <input type="hidden" name="intent" value={intent} />
      <input type="hidden" name="tab" value={tab} />
      <input type="hidden" name="id" value={id} />
      <button type="submit" className={btnClass || 'btn btn-sm btn-danger'} disabled={disabled || fetcher.state !== 'idle'} title={disabledTitle}>{btnLabel || 'Excluir'}</button>
    </fetcher.Form>
  );
}

export function SyncBanner({ isOnline, pendingCount }: { isOnline: boolean; pendingCount: number }) {
  if (isOnline && pendingCount === 0) return null;

  return (
    <div className={`sync-banner ${isOnline ? 'pending' : 'offline'}`}>
      {!isOnline
        ? 'Modo offline ativo. As alterações serão sincronizadas quando a conexão voltar.'
        : `${pendingCount} alteração(ões) pendente(s) de sincronização.`}
    </div>
  );
}

export function PendingBadge() {
  return <span className="pending-badge">Pendente sync</span>;
}

export function Toast({ message, tone, onClose }: { message: string; tone: 'success' | 'error'; onClose: () => void }) {
  useEffect(() => {
    const timer = window.setTimeout(onClose, 2600);
    return () => window.clearTimeout(timer);
  }, [message, onClose]);

  return <div className={`toast ${tone}`}>{message}</div>;
}

export function Modal({ title, children, onClose }: { title: string; children: React.ReactNode; onClose: () => void }) {
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

export function FilterBar({ value, onChange, placeholder, children }: { value: string; onChange: (value: string) => void; placeholder: string; children?: React.ReactNode }) {
  return (
    <div className="filter-bar">
      <input value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} aria-label={placeholder} />
      {children}
    </div>
  );
}

export function SortHeader({ label, active, direction, onClick }: { label: string; active: boolean; direction: SortDirection; onClick: () => void }) {
  return (
    <button type="button" className={`sort-header${active ? ' active' : ''}`} onClick={onClick}>
      <span>{label}</span>
      <span className="sort-arrows">
        <svg className={`sort-arrow asc${active && direction === 'asc' ? ' active' : ''}`} width="8" height="5" viewBox="0 0 8 5" fill="none">
          <path d="M4 0L7.5 5H0.5L4 0Z" />
        </svg>
        <svg className={`sort-arrow desc${active && direction === 'desc' ? ' active' : ''}`} width="8" height="5" viewBox="0 0 8 5" fill="none">
          <path d="M4 5L0.5 0H7.5L4 5Z" />
        </svg>
      </span>
    </button>
  );
}

export function Pagination({ page, totalPages, onChange }: { page: number; totalPages: number; onChange: (page: number) => void }) {
  if (totalPages <= 1) return null;
  return (
    <div className="pagination">
      <button type="button" className="btn btn-sm btn-secondary" onClick={() => onChange(Math.max(1, page - 1))} disabled={page === 1}>Anterior</button>
      <span className="pagination-label">Página {page} de {totalPages}</span>
      <button type="button" className="btn btn-sm btn-secondary" onClick={() => onChange(Math.min(totalPages, page + 1))} disabled={page === totalPages}>Próxima</button>
    </div>
  );
}

export function SmallSelect({ value, onChange, children }: { value: string; onChange: (value: string) => void; children: React.ReactNode }) {
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

function getPopoverPlacement(triggerRect: DOMRect, popoverHeight: number): { dropup: boolean; maxHeight: number } {
  const margin = 16;
  const spaceAbove = Math.max(0, triggerRect.top - margin);
  const spaceBelow = Math.max(0, window.innerHeight - triggerRect.bottom - margin);
  const fitsBelow = spaceBelow >= popoverHeight;
  const fitsAbove = spaceAbove >= popoverHeight;

  if (fitsBelow || (!fitsAbove && spaceBelow >= spaceAbove)) {
    return { dropup: false, maxHeight: spaceBelow };
  }

  return { dropup: true, maxHeight: spaceAbove };
}

export function DatePickerInput({ id, name, value, onChange, required, label }: { id: string; name: string; value: string; onChange: (value: string) => void; required?: boolean; label: string }) {
  const [isOpen, setIsOpen] = useState(false);
  const [viewDate, setViewDate] = useState(() => value ? parseLocalDate(value) : new Date());
  const wrapperRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const [dropup, setDropup] = useState(false);
  const [maxDropdownHeight, setMaxDropdownHeight] = useState(320);

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  useLayoutEffect(() => {
    if (isOpen && buttonRef.current && dropdownRef.current) {
      const rect = buttonRef.current.getBoundingClientRect();
      const dropdownHeight = dropdownRef.current.getBoundingClientRect().height;
      const placement = getPopoverPlacement(rect, dropdownHeight + 8);
      setDropup(placement.dropup);
      setMaxDropdownHeight(Math.max(120, placement.maxHeight));
    }
  }, [isOpen]);

  const daysInMonth = new Date(viewDate.getFullYear(), viewDate.getMonth() + 1, 0).getDate();
  const firstDay = new Date(viewDate.getFullYear(), viewDate.getMonth(), 1).getDay();
  const year = viewDate.getFullYear();
  const month = viewDate.getMonth();

  const days = Array.from({ length: daysInMonth }, (_, i) => i + 1);
  const blanks = Array.from({ length: firstDay }, (_, i) => i);

  const selectDate = (day: number) => {
    const newDate = new Date(year, month, day);
    onChange(formatLocalDate(newDate));
    setIsOpen(false);
  };

  const prevMonth = () => setViewDate(new Date(year, month - 1, 1));
  const nextMonth = () => setViewDate(new Date(year, month + 1, 1));

  const monthNames = ['Janeiro', 'Fevereiro', 'Março', 'Abril', 'Maio', 'Junho', 'Julho', 'Agosto', 'Setembro', 'Outubro', 'Novembro', 'Dezembro'];

  const formattedValue = value ? parseLocalDate(value).toLocaleDateString('pt-BR') : '';

  return (
    <div className="form-group">
      <label htmlFor={id}>{label}</label>
      <div className="custom-date-picker" ref={wrapperRef}>
        <input type="hidden" id={id} name={name} value={value} />
        <button
          type="button"
          ref={buttonRef}
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
          <div ref={dropdownRef} className={`date-picker-dropdown ${dropup ? 'dropup' : ''}`} style={{ maxHeight: `${maxDropdownHeight}px` }}>
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
                const isSelected = value && formatLocalDate(new Date(year, month, day)) === value;
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

export function DateTimePickerInput({ id, name, value, onChange, label }: { id: string; name: string; value: string; onChange: (value: string) => void; label: string }) {
  const [isOpen, setIsOpen] = useState(false);
  const [viewDate, setViewDate] = useState(() => value ? parseLocalDateTime(value) : new Date());
  const [showTime, setShowTime] = useState(false);
  const wrapperRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const [dropup, setDropup] = useState(false);
  const [maxDropdownHeight, setMaxDropdownHeight] = useState(360);

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

  useLayoutEffect(() => {
    if (isOpen && buttonRef.current && dropdownRef.current) {
      const rect = buttonRef.current.getBoundingClientRect();
      const dropdownHeight = dropdownRef.current.getBoundingClientRect().height;
      const placement = getPopoverPlacement(rect, dropdownHeight + 8);
      setDropup(placement.dropup);
      setMaxDropdownHeight(Math.max(140, placement.maxHeight));
    }
  }, [isOpen, showTime]);

  const daysInMonth = new Date(viewDate.getFullYear(), viewDate.getMonth() + 1, 0).getDate();
  const firstDay = new Date(viewDate.getFullYear(), viewDate.getMonth(), 1).getDay();
  const year = viewDate.getFullYear();
  const month = viewDate.getMonth();

  const days = Array.from({ length: daysInMonth }, (_, i) => i + 1);
  const blanks = Array.from({ length: firstDay }, (_, i) => i);
  const hours = Array.from({ length: 24 }, (_, i) => i);
  const minutes = Array.from({ length: 60 }, (_, i) => i);

  const currentDate = value ? parseLocalDateTime(value) : new Date();
  const selectedHour = currentDate.getHours();
  const selectedMinute = currentDate.getMinutes();

  const selectDateTime = (day: number, hour?: number, minute?: number) => {
    const newDate = new Date(year, month, day, hour ?? selectedHour, minute ?? selectedMinute);
    onChange(formatLocalDateTime(newDate));
    setIsOpen(false);
    setShowTime(false);
  };

  const prevMonth = () => setViewDate(new Date(year, month - 1, 1));
  const nextMonth = () => setViewDate(new Date(year, month + 1, 1));

  const monthNames = ['Janeiro', 'Fevereiro', 'Março', 'Abril', 'Maio', 'Junho', 'Julho', 'Agosto', 'Setembro', 'Outubro', 'Novembro', 'Dezembro'];

  const formattedValue = value ? parseLocalDateTime(value).toLocaleString('pt-BR') : '';

  return (
    <div className="form-group">
      <label htmlFor={id}>{label}</label>
      <div className="custom-date-picker" ref={wrapperRef}>
        <input type="hidden" id={id} name={name} value={value} />
        <button
          type="button"
          ref={buttonRef}
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
          <div ref={dropdownRef} className={`date-picker-dropdown datetime ${dropup ? 'dropup' : ''}`} style={{ maxHeight: `${maxDropdownHeight}px` }}>
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
                const isSelected = value && formatLocalDate(new Date(year, month, day)) === value?.split('T')[0];
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

export function MoneyInput({ id, name, value, onChange, label, required }: { id: string; name: string; value: string; onChange: (value: string) => void; label: string; required?: boolean }) {
  return (
    <div className="form-group">
      <label htmlFor={id}>{label}</label>
      <input type="hidden" name={name} value={value} />
      <input
        id={id}
        type="text"
        inputMode="numeric"
        value={formatMoneyInput(value)}
        onChange={(e) => onChange(normalizeMoneyInput(e.target.value))}
        placeholder="R$ 0,00"
        required={required}
      />
    </div>
  );
}

export function CpfInput({ id, name, value, onChange, label, required }: { id: string; name: string; value: string; onChange: (value: string) => void; label: string; required?: boolean }) {
  return (
    <div className="form-group">
      <label htmlFor={id}>{label}</label>
      <input
        id={id}
        name={name}
        type="text"
        inputMode="numeric"
        value={formatCpfInput(value)}
        onChange={(e) => onChange(e.target.value.replace(/\D/g, '').slice(0, 11))}
        placeholder="000.000.000-00"
        required={required}
      />
    </div>
  );
}

export function PhoneInput({ id, name, value, onChange, label, required }: { id: string; name: string; value: string; onChange: (value: string) => void; label: string; required?: boolean }) {
  return (
    <div className="form-group">
      <label htmlFor={id}>{label}</label>
      <input
        id={id}
        name={name}
        type="text"
        inputMode="tel"
        value={formatPhoneInput(value)}
        onChange={(e) => onChange(formatPhoneInput(e.target.value))}
        placeholder="(22) 99999-9999"
        required={required}
      />
    </div>
  );
}

export function CustomSelect({ id, name, value, onChange, required, label, children }: { id: string; name: string; value: string; onChange: (value: string) => void; required?: boolean; label: string; children: React.ReactNode }) {
  const [isOpen, setIsOpen] = useState(false);
  const [search, setSearch] = useState('');
  const wrapperRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  useEffect(() => {
    if (isOpen && searchRef.current) {
      searchRef.current.focus();
    }
    if (!isOpen) setSearch('');
  }, [isOpen]);

  const options = React.Children.toArray(children) as React.ReactElement<{ value: string; children: React.ReactNode }>[];
  const filteredOptions = options.filter((opt) => {
    if (!search) return true;
    const label = typeof opt.props.children === 'string' ? opt.props.children : '';
    return label.toLowerCase().includes(search.toLowerCase());
  });
  const selectedLabel = options.find((opt) => opt.props?.value === value);
  const displayText = selectedLabel?.props?.children || 'Selecione...';

  return (
    <div className="form-group">
      <label htmlFor={id}>{label}</label>
      <div className="custom-select" ref={wrapperRef}>
        <input type="hidden" id={id} name={name} value={value} />
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
            <input
              ref={searchRef}
              type="text"
              className="select-search"
              placeholder="Buscar..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Escape') setIsOpen(false);
              }}
            />
            <div className="select-options">
              <button
                type="button"
                className={`select-option ${!value ? 'selected' : ''}`}
                onClick={() => { onChange(''); setIsOpen(false); }}
              >
                Selecione...
              </button>
              {filteredOptions.map((opt, i) => {
                const isSelected = opt.props.value === value;
                return (
                  <button
                    key={i}
                    type="button"
                    className={`select-option ${isSelected ? 'selected' : ''}`}
                    onClick={() => { onChange(opt.props.value); setIsOpen(false); }}
                  >
                    {opt.props.children}
                  </button>
                );
              })}
              {filteredOptions.length === 0 && (
                <div className="select-option muted" style={{ cursor: 'default' }}>Nenhum resultado</div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
