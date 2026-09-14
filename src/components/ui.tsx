import React, { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { Tab, QueuePayload, SortDirection } from '../types/dashboard';
import type { LoginState } from '../whatsapp/types';
import {
  formatMoneyInput,
  normalizeMoneyInput,
  formatPhoneInput,
  formatCpfInput,
  parseLocalDate,
  formatLocalDate,
  parseLocalDateTime,
  formatLocalDateTime,
} from '../utils/format';
import { IconExport, IconImport, IconTheme } from './icons';

export function LoadingRow({ colSpan }: { colSpan: number }) {
  return (
    <tr>
      <td colSpan={colSpan} style={{ textAlign: 'center', padding: '2rem', color: 'var(--text-muted)' }}>
        <div className="loading-spinner" />
        <span style={{ marginLeft: '0.5rem' }}>Carregando...</span>
      </td>
    </tr>
  );
}

export function DataButtons({ collapsed }: { collapsed: boolean }) {
  const [importOpen, setImportOpen] = useState(false);
  const [pendingFile, setPendingFile] = useState<File | null>(null);
  return (
    <>
      <a href="/export" className="data-btn" download>
        <span className="nav-icon"><IconExport /></span>
        {!collapsed && <span className="nav-label">Exportar</span>}
      </a>
      <form action="/import" method="post" encType="multipart/form-data">
        <label className="data-btn data-btn-label">
          <input
            type="file"
            name="file"
            accept=".json"
            onChange={(e) => {
              const file = e.currentTarget.files?.[0];
              if (!file) return;
              setPendingFile(file);
              setImportOpen(true);
            }}
            hidden
          />
          <span className="nav-icon"><IconImport /></span>
          {!collapsed && <span className="nav-label">Importar</span>}
        </label>
      </form>
      <ConfirmModal
        open={importOpen}
        title="Importar dados"
        message="Importar dados do arquivo? Isso adicionará novos registros ao banco."
        confirmLabel="Importar"
        tone="primary"
        onClose={() => {
          setImportOpen(false);
          setPendingFile(null);
        }}
        onConfirm={() => {
          const file = pendingFile;
          setImportOpen(false);
          setPendingFile(null);
          if (file) {
            const dt = new DataTransfer();
            dt.items.add(file);
            const input = document.querySelector<HTMLInputElement>('input[type="file"][name="file"]');
            if (input) {
              input.files = dt.files;
              input.form?.submit();
            }
          }
        }}
      />
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
    <button
      className="theme-toggle"
      type="button"
      onClick={() => setTheme((current) => (current === 'light' ? 'dark' : 'light'))}
    >
      <span className="nav-icon"><IconTheme /></span>
      {!collapsed && <span className="nav-label">Tema</span>}
    </button>
  );
}

export function Brand({ collapsed }: { collapsed?: boolean }) {
  return (
    <div className="logo">
      <img src="/logomarca.png" alt="Aluxen" className="sidebar-logomarca logo-theme-light" />
      <img src="/logomarca-light.png" alt="Aluxen" className="sidebar-logomarca logo-theme-dark" />
      <img src="/marca.png" alt="Aluxen" className="sidebar-marca" />
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
  confirmTitle,
  confirmLabel,
  confirmTone = 'danger',
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
  confirmTitle?: string;
  confirmLabel?: string;
  confirmTone?: 'primary' | 'danger';
  btnLabel?: string;
  btnClass?: string;
  disabled?: boolean;
  disabledTitle?: string;
}) {
  const [submitting, setSubmitting] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setConfirmOpen(true);
  };

  const performDelete = async () => {
    setConfirmOpen(false);
    if (!window.navigator.onLine) {
      onOptimisticDelete();
      onSettled();
      queueOfflineMutation({ intent, tab, id }, 'Ação pendente de sincronização.');
      return;
    }

    setSubmitting(true);
    try {
      const formData = new FormData();
      formData.set('intent', intent);
      formData.set('tab', tab);
      formData.set('id', id);

      const res = await fetch('/dashboard', {
        method: 'POST',
        body: formData,
        credentials: 'same-origin',
      });
      const data = await res.json() as { ok?: boolean; error?: string };
      if (data.ok) {
        onOptimisticDelete();
      }
    } catch {
      // offline fallback
      onOptimisticDelete();
      queueOfflineMutation({ intent, tab, id }, 'Ação pendente de sincronização.');
    } finally {
      setSubmitting(false);
      onSettled();
    }
  };

  return (
    <>
      <form onSubmit={handleSubmit} style={{ display: 'inline-flex', margin: 0 }}>
        <input type="hidden" name="intent" value={intent} />
        <input type="hidden" name="tab" value={tab} />
        <input type="hidden" name="id" value={id} />
        <button
          type="submit"
          className={btnClass || 'btn btn-sm btn-danger'}
          disabled={disabled || submitting}
          title={disabledTitle}
        >
          {btnLabel || 'Excluir'}
        </button>
      </form>
      <ConfirmModal
        open={confirmOpen}
        title={confirmTitle || 'Confirmar exclusão'}
        message={confirmMessage || 'Tem certeza que deseja excluir este item?'}
        confirmLabel={confirmLabel || 'Excluir'}
        tone={confirmTone}
        busy={submitting}
        onClose={() => (!submitting ? setConfirmOpen(false) : null)}
        onConfirm={performDelete}
      />
    </>
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

export function Modal({
  title,
  children,
  onClose,
  wide,
}: {
  title: string;
  children: React.ReactNode;
  onClose: () => void;
  wide?: boolean;
}) {
  return (
    <div className="modal-backdrop" role="presentation" onClick={onClose}>
      <div
        className={`modal-card${wide ? ' modal-card-wide' : ''}`}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="modal-header">
          <h3>{title}</h3>
          <button type="button" className="btn btn-sm btn-secondary" onClick={onClose}>
            Fechar
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

export function ConfirmModal({
  open,
  title,
  message,
  confirmLabel = 'Confirmar',
  cancelLabel = 'Cancelar',
  tone = 'primary',
  busy = false,
  onConfirm,
  onClose,
}: {
  open: boolean;
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  tone?: 'primary' | 'danger';
  busy?: boolean;
  onConfirm: () => void;
  onClose: () => void;
}) {
  if (!open) return null;
  return (
    <div className="modal-backdrop" role="presentation" onClick={() => (!busy ? onClose() : null)}>
      <div
        className="confirm-dialog-card confirm-modal"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onClick={(event) => event.stopPropagation()}
      >
        <div className={`confirm-dialog-icon ${tone === 'danger' ? 'danger' : 'primary'}`} aria-hidden="true">
          {tone === 'danger' ? (
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M12 9v4m0 4h.01M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0z" />
            </svg>
          ) : (
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M12 8v4m0 4h.01M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0z" />
            </svg>
          )}
        </div>

        <h3 className="confirm-dialog-title">{title}</h3>
        <p className="confirm-dialog-message">{message}</p>

        <div className="confirm-dialog-actions">
          <button
            type="button"
            className="btn btn-secondary"
            onClick={onClose}
            disabled={busy}
          >
            {cancelLabel}
          </button>
          <button
            type="button"
            className={`btn ${tone === 'danger' ? 'btn-danger' : 'btn-primary'}`}
            onClick={onConfirm}
            disabled={busy}
          >
            {busy ? 'Processando…' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}

export function FilterBar({
  value,
  onChange,
  placeholder,
  children,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="filter-bar">
      <input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        aria-label={placeholder}
      />
      {children}
    </div>
  );
}

export function SortHeader({
  label,
  active,
  direction,
  onClick,
}: {
  label: string;
  active: boolean;
  direction: SortDirection;
  onClick: () => void;
}) {
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

export function Pagination({
  page,
  totalPages,
  onChange,
  pageSize,
  onPageSizeChange,
  pageSizeOptions = [10, 25, 50, 100],
}: {
  page: number;
  totalPages: number;
  onChange: (page: number) => void;
  pageSize?: number;
  onPageSizeChange?: (size: number) => void;
  pageSizeOptions?: number[];
}) {
  const hasSizeSelector = typeof pageSize === 'number' && onPageSizeChange !== undefined;
  if (totalPages <= 1 && !hasSizeSelector) return null;
  return (
    <div className="pagination">
      <div className="pagination-nav">
        <button type="button" className="btn btn-sm btn-secondary" onClick={() => onChange(Math.max(1, page - 1))} disabled={page === 1}>
          Anterior
        </button>
        <span className="pagination-label">
          Página {page} de {totalPages}
        </span>
        <button type="button" className="btn btn-sm btn-secondary" onClick={() => onChange(Math.min(totalPages, page + 1))} disabled={page === totalPages}>
          Próxima
        </button>
      </div>
      {hasSizeSelector ? (
        <div className="pagination-size">
          <span className="pagination-label">Itens por página</span>
          <SmallSelect value={String(pageSize)} onChange={(v) => onPageSizeChange(Number(v))}>
            {pageSizeOptions.map((opt) => (
              <option key={opt} value={String(opt)}>{opt}</option>
            ))}
          </SmallSelect>
        </div>
      ) : null}
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
  const selectedOption = options.find((opt) => {
    const optReact = opt as React.ReactElement<{ value: string }>;
    return optReact.props?.value === value;
  });
  const displayText = selectedOption
    ? (selectedOption as React.ReactElement<{ children: React.ReactNode }>)?.props?.children
    : value;

  return (
    <div className="small-select" ref={wrapperRef}>
      <button type="button" className="small-select-display" onClick={() => setIsOpen(!isOpen)}>
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
                onClick={() => {
                  onChange(optReact.props.value);
                  setIsOpen(false);
                }}
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

function getPopoverPlacement(triggerRect: DOMRect, popoverHeight: number): { dropup: boolean; maxHeight: number; alignRight: boolean } {
  const margin = 16;
  const spaceAbove = Math.max(0, triggerRect.top - margin);
  const spaceBelow = Math.max(0, window.innerHeight - triggerRect.bottom - margin);
  const fitsBelow = spaceBelow >= popoverHeight;
  const fitsAbove = spaceAbove >= popoverHeight;

  const dropup = fitsBelow ? false : fitsAbove ? true : spaceAbove > spaceBelow;
  const maxHeight = dropup ? spaceAbove : spaceBelow;
  const alignRight = triggerRect.left + 290 > (typeof window !== 'undefined' ? window.innerWidth : 1000) && triggerRect.right >= 280;

  return { dropup, maxHeight, alignRight };
}

export function DatePickerInput({
  id,
  name,
  value,
  onChange,
  label,
  required,
}: {
  id: string;
  name: string;
  value: string;
  onChange: (value: string) => void;
  required?: boolean;
  label: string;
}) {
  const [isOpen, setIsOpen] = useState(false);
  const [viewDate, setViewDate] = useState(() => (value ? parseLocalDate(value) : new Date()));
  const wrapperRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const [dropup, setDropup] = useState(false);
  const [alignRight, setAlignRight] = useState(false);
  const [maxDropdownHeight, setMaxDropdownHeight] = useState(340);

  useEffect(() => {
    if (value) {
      setViewDate(parseLocalDate(value));
    }
  }, [value]);

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
      setAlignRight(placement.alignRight);
      setMaxDropdownHeight(Math.max(160, placement.maxHeight));
    }
  }, [isOpen]);

  const daysInMonth = new Date(viewDate.getFullYear(), viewDate.getMonth() + 1, 0).getDate();
  const firstDay = new Date(viewDate.getFullYear(), viewDate.getMonth(), 1).getDay();
  const year = viewDate.getFullYear();
  const month = viewDate.getMonth();

  const days = Array.from({ length: daysInMonth }, (_, i) => i + 1);
  const blanks = Array.from({ length: firstDay }, (_, i) => i);

  const currentYear = new Date().getFullYear();
  const startYear = Math.min(year, currentYear - 30);
  const endYear = Math.max(year, currentYear + 20);
  const years = Array.from({ length: endYear - startYear + 1 }, (_, i) => startYear + i);

  const selectDate = (day: number) => {
    const newDate = new Date(year, month, day);
    onChange(formatLocalDate(newDate));
    setIsOpen(false);
  };

  const prevMonth = () => setViewDate(new Date(year, month - 1, 1));
  const nextMonth = () => setViewDate(new Date(year, month + 1, 1));

  const monthNames = [
    'Janeiro', 'Fevereiro', 'Março', 'Abril', 'Maio', 'Junho',
    'Julho', 'Agosto', 'Setembro', 'Outubro', 'Novembro', 'Dezembro',
  ];

  const formattedValue = value ? parseLocalDate(value).toLocaleDateString('pt-BR') : '';

  return (
    <div className="form-group">
      <label htmlFor={id}>{label}</label>
      <div className="custom-date-picker" ref={wrapperRef}>
        <input type="hidden" id={id} name={name} value={value} />
        <button type="button" ref={buttonRef} className="date-picker-display" onClick={() => setIsOpen(!isOpen)}>
          <span>{formattedValue || 'Selecione...'}</span>
          <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <rect x="3" y="4" width="18" height="18" rx="2" />
            <path d="M16 2V6" />
            <path d="M8 2V6" />
            <path d="M3 10H21" />
          </svg>
        </button>
        {isOpen && (
          <div
            ref={dropdownRef}
            className={`date-picker-dropdown ${dropup ? 'dropup' : ''} ${alignRight ? 'align-right' : ''}`}
            style={{ maxHeight: `${maxDropdownHeight}px` }}
          >
            <div className="date-picker-header">
              <button type="button" className="date-picker-nav" onClick={prevMonth} title="Mês anterior" aria-label="Mês anterior">
                <svg viewBox="0 0 24 24" fill="none">
                  <path d="M15 18L9 12L15 6" />
                </svg>
              </button>
              <div className="date-picker-selectors">
                <select
                  className="date-picker-select"
                  value={month}
                  onChange={(e) => {
                    const newMonth = parseInt(e.target.value, 10);
                    setViewDate(new Date(year, newMonth, 1));
                  }}
                  onClick={(e) => e.stopPropagation()}
                  aria-label="Selecionar mês"
                >
                  {monthNames.map((name, index) => (
                    <option key={name} value={index}>
                      {name}
                    </option>
                  ))}
                </select>
                <select
                  className="date-picker-select date-picker-select-year"
                  value={year}
                  onChange={(e) => {
                    const newYear = parseInt(e.target.value, 10);
                    setViewDate(new Date(newYear, month, 1));
                  }}
                  onClick={(e) => e.stopPropagation()}
                  aria-label="Selecionar ano"
                >
                  {years.map((y) => (
                    <option key={y} value={y}>
                      {y}
                    </option>
                  ))}
                </select>
              </div>
              <button type="button" className="date-picker-nav" onClick={nextMonth} title="Próximo mês" aria-label="Próximo mês">
                <svg viewBox="0 0 24 24" fill="none">
                  <path d="M9 18L15 12L9 6" />
                </svg>
              </button>
            </div>
            <div className="date-picker-weekdays">
              <span>Dom</span><span>Seg</span><span>Ter</span><span>Qua</span><span>Qui</span><span>Sex</span><span>Sáb</span>
            </div>
            <div className="date-picker-days">
              {blanks.map((i) => (
                <span key={`blank-${i}`} className="blank" />
              ))}
              {days.map((day) => {
                const isSelected = value && formatLocalDate(new Date(year, month, day)) === value;
                return (
                  <button key={day} type="button" className={isSelected ? 'selected' : ''} onClick={() => selectDate(day)}>
                    {day}
                  </button>
                );
              })}
            </div>
            <div className="date-picker-footer">
              <button
                type="button"
                className="btn-text-sm"
                onClick={() => {
                  const today = new Date();
                  setViewDate(today);
                  selectDate(today.getDate());
                }}
              >
                Hoje
              </button>
              {!required && value ? (
                <button
                  type="button"
                  className="btn-text-sm text-muted"
                  onClick={() => {
                    onChange('');
                    setIsOpen(false);
                  }}
                >
                  Limpar
                </button>
              ) : null}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

export function DateTimePickerInput({
  id,
  name,
  value,
  onChange,
  label,
  required,
}: {
  id: string;
  name: string;
  value: string;
  onChange: (value: string) => void;
  label: string;
  required?: boolean;
}) {
  const [isOpen, setIsOpen] = useState(false);
  const [viewDate, setViewDate] = useState(() => (value ? parseLocalDateTime(value) : new Date()));
  const [showTime, setShowTime] = useState(false);
  const wrapperRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const [dropup, setDropup] = useState(false);
  const [alignRight, setAlignRight] = useState(false);
  const [maxDropdownHeight, setMaxDropdownHeight] = useState(380);

  useEffect(() => {
    if (value) {
      setViewDate(parseLocalDateTime(value));
    }
  }, [value]);

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
      setAlignRight(placement.alignRight);
      setMaxDropdownHeight(Math.max(160, placement.maxHeight));
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
  const selectedDay = currentDate.getDate();
  const selectedHour = currentDate.getHours();
  const selectedMinute = currentDate.getMinutes();

  const currentYear = new Date().getFullYear();
  const startYear = Math.min(year, currentYear - 30);
  const endYear = Math.max(year, currentYear + 20);
  const years = Array.from({ length: endYear - startYear + 1 }, (_, i) => startYear + i);

  const selectDay = (day: number) => {
    const newDate = new Date(year, month, day, selectedHour, selectedMinute);
    onChange(formatLocalDateTime(newDate));
    setIsOpen(false);
    setShowTime(false);
  };

  const updateTime = (hour: number, minute: number) => {
    const newDate = new Date(year, month, selectedDay, hour, minute);
    onChange(formatLocalDateTime(newDate));
  };

  const prevMonth = () => setViewDate(new Date(year, month - 1, 1));
  const nextMonth = () => setViewDate(new Date(year, month + 1, 1));

  const monthNames = [
    'Janeiro', 'Fevereiro', 'Março', 'Abril', 'Maio', 'Junho',
    'Julho', 'Agosto', 'Setembro', 'Outubro', 'Novembro', 'Dezembro',
  ];

  const formattedValue = value ? parseLocalDateTime(value).toLocaleString('pt-BR') : '';

  return (
    <div className="form-group">
      <label htmlFor={id}>{label}</label>
      <div className="custom-date-picker" ref={wrapperRef}>
        <input type="hidden" id={id} name={name} value={value} />
        <button type="button" ref={buttonRef} className="date-picker-display" onClick={() => setIsOpen(!isOpen)}>
          <span>{formattedValue || 'Selecione...'}</span>
          <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <circle cx="12" cy="12" r="10" />
            <path d="M12 6V12L16 14" />
          </svg>
        </button>
        {isOpen && (
          <div
            ref={dropdownRef}
            className={`date-picker-dropdown datetime ${dropup ? 'dropup' : ''} ${alignRight ? 'align-right' : ''}`}
            style={{ maxHeight: `${maxDropdownHeight}px` }}
          >
            <div className="date-picker-header">
              <button type="button" className="date-picker-nav" onClick={prevMonth} title="Mês anterior" aria-label="Mês anterior">
                <svg viewBox="0 0 24 24" fill="none">
                  <path d="M15 18L9 12L15 6" />
                </svg>
              </button>
              <div className="date-picker-selectors">
                <select
                  className="date-picker-select"
                  value={month}
                  onChange={(e) => {
                    const newMonth = parseInt(e.target.value, 10);
                    setViewDate(new Date(year, newMonth, 1));
                  }}
                  onClick={(e) => e.stopPropagation()}
                  aria-label="Selecionar mês"
                >
                  {monthNames.map((name, index) => (
                    <option key={name} value={index}>
                      {name}
                    </option>
                  ))}
                </select>
                <select
                  className="date-picker-select date-picker-select-year"
                  value={year}
                  onChange={(e) => {
                    const newYear = parseInt(e.target.value, 10);
                    setViewDate(new Date(newYear, month, 1));
                  }}
                  onClick={(e) => e.stopPropagation()}
                  aria-label="Selecionar ano"
                >
                  {years.map((y) => (
                    <option key={y} value={y}>
                      {y}
                    </option>
                  ))}
                </select>
              </div>
              <button type="button" className="date-picker-nav" onClick={nextMonth} title="Próximo mês" aria-label="Próximo mês">
                <svg viewBox="0 0 24 24" fill="none">
                  <path d="M9 18L15 12L9 6" />
                </svg>
              </button>
            </div>
            <div className="date-picker-weekdays">
              <span>Dom</span><span>Seg</span><span>Ter</span><span>Qua</span><span>Qui</span><span>Sex</span><span>Sáb</span>
            </div>
            <div className="date-picker-days">
              {blanks.map((i) => (
                <span key={`blank-${i}`} className="blank" />
              ))}
              {days.map((day) => {
                const isSelected = value && formatLocalDate(new Date(year, month, day)) === value?.split('T')[0];
                return (
                  <button key={day} type="button" className={isSelected ? 'selected' : ''} onClick={() => selectDay(day)}>
                    {day}
                  </button>
                );
              })}
            </div>
            <div className="time-picker-section">
              <button type="button" className="time-toggle" onClick={() => setShowTime(!showTime)}>
                <span>Horário: {String(selectedHour).padStart(2, '0')}:{String(selectedMinute).padStart(2, '0')}</span>
                <svg viewBox="0 0 24 24" fill="none">
                  <path d={showTime ? 'M18 15L12 9L6 15' : 'M6 9L12 15L18 9'} />
                </svg>
              </button>
              {showTime && (
                <div className="time-picker-grid">
                  <select
                    value={selectedHour}
                    onChange={(e) => updateTime(parseInt(e.target.value, 10), selectedMinute)}
                    className="time-select"
                  >
                    {hours.map((h) => (
                      <option key={h} value={h}>
                        {h.toString().padStart(2, '0')}h
                      </option>
                    ))}
                  </select>
                  <span>:</span>
                  <select
                    value={selectedMinute}
                    onChange={(e) => updateTime(selectedHour, parseInt(e.target.value, 10))}
                    className="time-select"
                  >
                    {minutes
                      .filter((_, i) => i % 5 === 0)
                      .map((m) => (
                        <option key={m} value={m}>
                          {m.toString().padStart(2, '0')}m
                        </option>
                      ))}
                  </select>
                </div>
              )}
            </div>
            <div className="date-picker-footer">
              <button
                type="button"
                className="btn-text-sm"
                onClick={() => {
                  const now = new Date();
                  setViewDate(now);
                  onChange(formatLocalDateTime(now));
                  setIsOpen(false);
                  setShowTime(false);
                }}
              >
                Agora
              </button>
              {!required && value ? (
                <button
                  type="button"
                  className="btn-text-sm text-muted"
                  onClick={() => {
                    onChange('');
                    setIsOpen(false);
                    setShowTime(false);
                  }}
                >
                  Limpar
                </button>
              ) : null}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

export function MoneyInput({
  id,
  name,
  value,
  onChange,
  label,
  required,
}: {
  id: string;
  name: string;
  value: string;
  onChange: (value: string) => void;
  label: string;
  required?: boolean;
}) {
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

export function CpfInput({
  id,
  name,
  value,
  onChange,
  label,
  required,
}: {
  id: string;
  name: string;
  value: string;
  onChange: (value: string) => void;
  label: string;
  required?: boolean;
}) {
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

export function PhoneInput({
  id,
  name,
  value,
  onChange,
  label,
  required,
}: {
  id: string;
  name: string;
  value: string;
  onChange: (value: string) => void;
  label: string;
  required?: boolean;
}) {
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

export function CustomSelect({
  id,
  name,
  value,
  onChange,
  label,
  children,
}: {
  id: string;
  name: string;
  value: string;
  onChange: (value: string) => void;
  required?: boolean;
  label: string;
  children: React.ReactNode;
}) {
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
    const optLabel = typeof opt.props.children === 'string' ? opt.props.children : '';
    return optLabel.toLowerCase().includes(search.toLowerCase());
  });
  const selectedOption = options.find((opt) => opt.props?.value === value);
  const displayText = selectedOption?.props?.children || 'Selecione...';

  return (
    <div className="form-group">
      <label htmlFor={id}>{label}</label>
      <div className="custom-select" ref={wrapperRef}>
        <input type="hidden" id={id} name={name} value={value} />
        <button type="button" className="select-display" onClick={() => setIsOpen(!isOpen)}>
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
                onClick={() => {
                  onChange('');
                  setIsOpen(false);
                }}
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
                    onClick={() => {
                      onChange(opt.props.value);
                      setIsOpen(false);
                    }}
                  >
                    {opt.props.children}
                  </button>
                );
              })}
              {filteredOptions.length === 0 && (
                <div className="select-option muted" style={{ cursor: 'default' }}>
                  Nenhum resultado
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

export interface WhatsAppConnectModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess?: () => void;
}

export function WhatsAppConnectModal({ open, onClose, onSuccess }: WhatsAppConnectModalProps) {
  const [state, setState] = useState<LoginState>({ status: 'starting', message: 'Iniciando...' });
  const [cancelling, setCancelling] = useState(false);

  useEffect(() => {
    if (!open) return;

    let timer: any = null;
    let active = true;

    const start = async () => {
      try {
        const res = await fetch('/api/whatsapp/login/start', { method: 'POST', credentials: 'same-origin' });
        const data = await res.json();
        if (active && data.state) {
          setState(data.state);
        }
      } catch {
        if (active) setState({ status: 'error', error: 'Erro de conexão ao iniciar WhatsApp.' });
      }

      timer = setInterval(async () => {
        if (!active) return;
        try {
          const res = await fetch('/api/whatsapp/login/status', { credentials: 'same-origin' });
          const data = await res.json();
          if (active && data.state) {
            setState(data.state);
            if (data.state.status === 'connected') {
              clearInterval(timer);
              onSuccess?.();
            }
          }
        } catch {}
      }, 1800);
    };

    void start();

    return () => {
      active = false;
      if (timer) clearInterval(timer);
    };
  }, [open]);

  if (!open) return null;

  const handleCancel = async () => {
    setCancelling(true);
    try {
      await fetch('/api/whatsapp/login/cancel', { method: 'POST', credentials: 'same-origin' });
    } catch {}
    setCancelling(false);
    onClose();
  };

  return (
    <Modal title="Conectar WhatsApp ao Aluxen" onClose={handleCancel}>
      <div className="stack" style={{ textAlign: 'center', padding: '0.5rem 0' }}>
        {state.status === 'starting' && (
          <div style={{ padding: '2rem 1rem' }}>
            <div className="loading-spinner" style={{ margin: '0 auto 1rem' }} />
            <p style={{ fontWeight: 600, fontSize: '1rem' }}>Iniciando conexão com o WhatsApp...</p>
            <p style={{ color: 'var(--text-muted)', fontSize: '0.875rem', marginTop: '0.25rem' }}>
              Gerando o QR Code seguro para a conexão.
            </p>
          </div>
        )}

        {state.status === 'scan_qr' && state.qrCode && (
          <div className="stack" style={{ alignItems: 'center' }}>
            <div
              style={{
                background: 'var(--surface-subtle)',
                padding: '0.75rem 1rem',
                borderRadius: 'var(--radius-md)',
                fontSize: '0.85rem',
                textAlign: 'left',
                width: '100%',
                maxWidth: 380,
                lineHeight: 1.5,
              }}
            >
              <strong>Como conectar:</strong>
              <ol style={{ margin: '0.375rem 0 0', paddingLeft: '1.25rem' }}>
                <li>Abra o <strong>WhatsApp</strong> no seu celular</li>
                <li>Toque em <strong>Configurações</strong> ou Menu <strong>(⋮)</strong></li>
                <li>Selecione <strong>Aparelhos conectados</strong></li>
                <li>Toque em <strong>Conectar um aparelho</strong> e aponte para a tela</li>
              </ol>
            </div>

            <div
              style={{
                background: '#ffffff',
                padding: '1rem',
                borderRadius: 'var(--radius-lg)',
                border: '2px solid var(--border)',
                display: 'inline-block',
                boxShadow: 'var(--shadow-md)',
                margin: '0.75rem auto',
              }}
            >
              <img
                src={state.qrCode}
                alt="QR Code do WhatsApp"
                style={{ width: 240, height: 240, display: 'block' }}
              />
            </div>

            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', color: 'var(--primary)', fontWeight: 500, fontSize: '0.875rem' }}>
              <div className="loading-spinner" style={{ width: 14, height: 14 }} />
              <span>Aguardando leitura do QR Code pelo celular...</span>
            </div>
          </div>
        )}

        {state.status === 'syncing' && (
          <div style={{ padding: '2rem 1rem' }}>
            <div className="loading-spinner" style={{ margin: '0 auto 1rem' }} />
            <p style={{ fontWeight: 600, fontSize: '1rem', color: 'var(--primary)' }}>
              QR Code lido com sucesso!
            </p>
            <p style={{ color: 'var(--text-muted)', fontSize: '0.875rem', marginTop: '0.25rem' }}>
              Sincronizando suas conversas e finalizando a conexão...
            </p>
          </div>
        )}

        {state.status === 'connected' && (
          <div style={{ padding: '2rem 1rem' }}>
            <div
              style={{
                width: 56,
                height: 56,
                borderRadius: '50%',
                background: 'rgba(16, 185, 129, 0.15)',
                color: 'var(--primary)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                margin: '0 auto 1rem',
                fontSize: '1.75rem',
              }}
            >
              ✓
            </div>
            <h3 style={{ fontSize: '1.125rem', marginBottom: '0.5rem' }}>WhatsApp Conectado!</h3>
            <p style={{ color: 'var(--text-muted)', fontSize: '0.875rem', maxWidth: 360, margin: '0 auto 1.5rem' }}>
              O WhatsApp está conectado ao Aluxen e pronto para enviar recibos e contratos automaticamente.
            </p>
            <button type="button" className="btn btn-primary" onClick={onClose} style={{ minWidth: 140 }}>
              Concluir
            </button>
          </div>
        )}

        {state.status === 'error' && (
          <div style={{ padding: '1.5rem 1rem' }}>
            <div className="error" style={{ textAlign: 'left', marginBottom: '1rem' }}>
              {state.error || 'Ocorreu um erro durante a conexão com o WhatsApp.'}
            </div>
            <button
              type="button"
              className="btn btn-primary"
              onClick={() => {
                setState({ status: 'starting', message: 'Tentando novamente...' });
                void fetch('/api/whatsapp/login/start', { method: 'POST', credentials: 'same-origin' })
                  .then((res) => res.json())
                  .then((d) => d.state && setState(d.state));
              }}
            >
              Tentar novamente
            </button>
          </div>
        )}

        {state.status !== 'connected' && (
          <div style={{ marginTop: '1rem', borderTop: '1px solid var(--border)', paddingTop: '0.75rem', display: 'flex', justifyContent: 'flex-end' }}>
            <button type="button" className="btn btn-outline" onClick={handleCancel} disabled={cancelling}>
              {cancelling ? 'Cancelando...' : 'Cancelar'}
            </button>
          </div>
        )}
      </div>
    </Modal>
  );
}
