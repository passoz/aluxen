import { describe, expect, it } from 'bun:test';
import { formatDateBr, formatDateTimeBr, parseLocalDateTime } from '../src/utils/format';

describe('Formatação de datas', () => {
  it('formata datas do SQLite (separador de espaço) e do app (separador T)', () => {
    expect(formatDateBr('2026-09-21')).toBe('21/09/2026');
    expect(formatDateBr('2026-09-21T10:30')).toBe('21/09/2026');
    // `datetime('now')` do SQLite: "YYYY-MM-DD HH:MM:SS"
    expect(formatDateBr('2026-09-21 10:30:00')).toBe('21/09/2026');
    expect(formatDateTimeBr('2026-09-21T10:30')).toBe('21/09/2026 10:30');
    expect(formatDateTimeBr('2026-09-21 10:30:00')).toBe('21/09/2026 10:30');
    expect(formatDateTimeBr('2026-09-21')).toBe('21/09/2026');
    expect(formatDateBr(null)).toBe('-');
    expect(formatDateTimeBr('')).toBe('-');
  });

  it('interpreta timestamps dos dois formatos como data local', () => {
    const fromApp = parseLocalDateTime('2026-09-21T10:30');
    const fromSqlite = parseLocalDateTime('2026-09-21 10:30:00');
    expect(fromApp.getFullYear()).toBe(2026);
    expect(fromApp.getMonth()).toBe(8);
    expect(fromApp.getDate()).toBe(21);
    expect(fromApp.getHours()).toBe(10);
    expect(fromApp.getMinutes()).toBe(30);
    expect(fromSqlite.getDate()).toBe(21);
    expect(fromSqlite.getHours()).toBe(10);
  });
});
