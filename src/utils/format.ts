import type { Receipt, SortDirection, SortState } from '../types/dashboard';

export function getDaysInMonth(year: number, month: number) {
  return new Date(year, month, 0).getDate();
}

export function getReceiptStatus(receipt: Pick<Receipt, 'paid_at' | 'due_date'>) {
  if (receipt.paid_at) return 'paid';
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  const dueDate = new Date(`${receipt.due_date}T00:00:00`);
  return dueDate < today ? 'overdue' : 'due';
}

export function getReceiptStatusLabel(status: string) {
  if (status === 'paid') return 'Pago';
  if (status === 'overdue') return 'Vencido';
  return 'A vencer';
}

export function formatCurrency(value: string | number) {
  return new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' }).format(Number(value));
}

export function normalizeMoneyInput(value: string) {
  const digits = value.replace(/\D/g, '');
  if (!digits) return '';
  return (Number(digits) / 100).toFixed(2);
}

export function formatMoneyInput(value: string) {
  if (!value) return '';
  return formatCurrency(value);
}

export function formatPhoneInput(value: string) {
  const digits = value.replace(/\D/g, '').slice(0, 11);
  if (!digits) return '';
  if (digits.length <= 2) return `(${digits}`;
  if (digits.length <= 6) return `(${digits.slice(0, 2)}) ${digits.slice(2)}`;
  if (digits.length <= 10) return `(${digits.slice(0, 2)}) ${digits.slice(2, 6)}-${digits.slice(6)}`;
  return `(${digits.slice(0, 2)}) ${digits.slice(2, 7)}-${digits.slice(7)}`;
}

export function formatCpfInput(value: string) {
  const digits = value.replace(/\D/g, '').slice(0, 11);
  if (!digits) return '';
  if (digits.length <= 3) return digits;
  if (digits.length <= 6) return `${digits.slice(0, 3)}.${digits.slice(3)}`;
  if (digits.length <= 9) return `${digits.slice(0, 3)}.${digits.slice(3, 6)}.${digits.slice(6)}`;
  return `${digits.slice(0, 3)}.${digits.slice(3, 6)}.${digits.slice(6, 9)}-${digits.slice(9)}`;
}

export function parseLocalDate(value: string) {
  const [year, month, day] = value.split('-').map(Number);
  return new Date(year ?? 2000, (month || 1) - 1, day || 1);
}

export function formatLocalDate(date: Date) {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
}

export function parseLocalDateTime(value: string) {
  const [datePart = '', timePart = '00:00'] = value.split('T');
  const [year, month, day] = datePart.split('-').map(Number);
  const [hour, minute] = timePart.split(':').map(Number);
  return new Date(year ?? 2000, (month || 1) - 1, day || 1, hour || 0, minute || 0);
}

export function formatLocalDateTime(date: Date) {
  const datePart = formatLocalDate(date);
  const hour = String(date.getHours()).padStart(2, '0');
  const minute = String(date.getMinutes()).padStart(2, '0');
  return `${datePart}T${hour}:${minute}`;
}

export function formatDateBr(value: string | null) {
  if (!value) return '-';
  const [year, month, day] = value.split(/[-T]/).map(Number);
  return `${String(day).padStart(2, '0')}/${String(month).padStart(2, '0')}/${year}`;
}

export function formatDateTimeBr(value: string | null) {
  if (!value) return '-';
  const [datePart = '', timePart = ''] = value.split('T');
  const [year, month, day] = datePart.split('-').map(Number);
  if (timePart) {
    const [hour, minute] = timePart.split(':');
    return `${String(day).padStart(2, '0')}/${String(month).padStart(2, '0')}/${year} ${hour}:${minute}`;
  }
  return `${String(day).padStart(2, '0')}/${String(month).padStart(2, '0')}/${year}`;
}

export function compareValues(left: string | number | null | undefined, right: string | number | null | undefined, direction: SortDirection) {
  const a = left ?? '';
  const b = right ?? '';
  const result = typeof a === 'number' || typeof b === 'number'
    ? Number(a) - Number(b)
    : String(a).localeCompare(String(b), 'pt-BR', { numeric: true, sensitivity: 'base' });
  return direction === 'asc' ? result : -result;
}

export function sortItems<T extends Record<string, unknown>>(items: T[], sort: SortState) {
  return [...items].sort((a, b) => compareValues(a[sort.key] as string | number | null | undefined, b[sort.key] as string | number | null | undefined, sort.direction));
}

export function paginateItems<T>(items: T[], page: number, pageSize: number) {
  const start = (page - 1) * pageSize;
  return items.slice(start, start + pageSize);
}
