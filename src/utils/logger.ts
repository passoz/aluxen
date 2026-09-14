/**
 * Logger estruturado do Aluxen.
 *
 * Objetivos:
 * - Uma linha por evento, sempre com timestamp ISO, nível, escopo e campos tipados.
 * - Saída legível no terminal (TTY) e JSON Lines em produção/containers (`LOG_FORMAT=json`).
 * - Redação automática de campos sensíveis (senha, token, cookie, hash, segredo).
 * - Filtro global por `LOG_LEVEL` (debug | info | warn | error | silent).
 *
 * O logger não conhece HTTP nem o banco de dados: quem loga é o chamador.
 */

export type LogLevel = 'debug' | 'info' | 'warn' | 'error' | 'silent';

export type LogFields = Record<string, unknown>;

export interface Logger {
  debug(message: string, fields?: LogFields): void;
  info(message: string, fields?: LogFields): void;
  warn(message: string, fields?: LogFields): void;
  error(message: string, fields?: LogFields): void;
  child(scope: string, fields?: LogFields): Logger;
}

const LEVEL_WEIGHT: Record<LogLevel, number> = {
  debug: 10,
  info: 20,
  warn: 30,
  error: 40,
  silent: 100,
};

const REDACTED = '[redigido]';
const SENSITIVE_KEY = /(pass(word|wd)?|senha|token|secret|segredo|cookie|authorization|api[-_]?key|hash|session)/i;
const LEVEL_TAG: Record<Exclude<LogLevel, 'silent'>, string> = {
  debug: 'DEBUG',
  info: 'INFO ',
  warn: 'WARN ',
  error: 'ERROR',
};

export interface LoggerOptions {
  level: LogLevel;
  format: 'pretty' | 'json';
}

function parseLevel(raw: string | undefined): LogLevel | null {
  if (!raw) return null;
  const value = raw.trim().toLowerCase();
  if (value === 'debug' || value === 'info' || value === 'warn' || value === 'error' || value === 'silent') {
    return value;
  }
  if (value === 'trace' || value === 'verbose') return 'debug';
  return null;
}

function defaultOptions(): LoggerOptions {
  const level = parseLevel(process.env.LOG_LEVEL) || 'info';
  const requested = process.env.LOG_FORMAT?.trim().toLowerCase();
  const format: LoggerOptions['format'] =
    requested === 'json' ? 'json' : requested === 'pretty' ? 'pretty' : process.stdout.isTTY ? 'pretty' : 'json';
  return { level, format };
}

let options: LoggerOptions = defaultOptions();

/** Reconfigura o logger (usado por scripts/testes e por `LOG_LEVEL`/`LOG_FORMAT`). */
export function configureLogger(next: Partial<LoggerOptions>): void {
  options = { ...options, ...next };
}

/** Opções ativas do logger — permite que scripts e testes salvem/restaurem a configuração. */
export function getLoggerOptions(): LoggerOptions {
  return { ...options };
}

function serializeError(error: unknown): LogFields {
  if (error instanceof Error) {
    const fields: LogFields = { name: error.name, message: error.message };
    if (error.stack) fields.stack = error.stack.split('\n').slice(0, 6).join('\n');
    const cause = (error as Error & { cause?: unknown }).cause;
    if (cause !== undefined) fields.cause = cause instanceof Error ? cause.message : cause;
    return fields;
  }
  return { message: String(error) };
}

function sanitizeValue(key: string, value: unknown, depth: number): unknown {
  if (value === null || value === undefined) return value;
  if (SENSITIVE_KEY.test(key)) return REDACTED;
  if (value instanceof Error) return serializeError(value);
  if (typeof value === 'bigint') return value.toString();
  if (typeof value === 'function') return `[função ${value.name || 'anônima'}]`;
  if (Array.isArray(value)) {
    return depth > 0 ? value.map((item) => sanitizeValue(key, item, depth - 1)) : `[${value.length} itens]`;
  }
  if (typeof value === 'object') {
    if (depth <= 0) return '[objeto]';
    const out: LogFields = {};
    for (const [childKey, childValue] of Object.entries(value as Record<string, unknown>)) {
      out[childKey] = sanitizeValue(childKey, childValue, depth - 1);
    }
    return out;
  }
  return value;
}

function sanitizeFields(fields: LogFields): LogFields {
  const out: LogFields = {};
  for (const [key, value] of Object.entries(fields)) {
    out[key] = sanitizeValue(key, value, 2);
  }
  return out;
}

function stringifyValue(value: unknown): string {
  if (typeof value === 'string') return /\s/.test(value) ? JSON.stringify(value) : value;
  if (value === null || value === undefined) return String(value);
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  try {
    return JSON.stringify(value) ?? String(value);
  } catch {
    return String(value);
  }
}

function emit(level: Exclude<LogLevel, 'silent'>, scope: string, message: string, fields: LogFields): void {
  if (LEVEL_WEIGHT[level] < LEVEL_WEIGHT[options.level]) return;

  const timestamp = new Date().toISOString();
  const payload = sanitizeFields(fields);

  if (options.format === 'json') {
    process.stdout.write(`${JSON.stringify({ ts: timestamp, level, scope, message, ...payload })}\n`);
    return;
  }

  const extra = Object.entries(payload)
    .map(([key, value]) => `${key}=${stringifyValue(value)}`)
    .join(' ');
  process.stdout.write(`${timestamp} ${LEVEL_TAG[level]} [${scope}] ${message}${extra ? ` ${extra}` : ''}\n`);
}

class ScopedLogger implements Logger {
  constructor(private readonly scope: string, private readonly base: LogFields = {}) {}

  private log(level: Exclude<LogLevel, 'silent'>, message: string, fields?: LogFields): void {
    emit(level, this.scope, message, fields ? { ...this.base, ...fields } : this.base);
  }

  debug(message: string, fields?: LogFields): void {
    this.log('debug', message, fields);
  }

  info(message: string, fields?: LogFields): void {
    this.log('info', message, fields);
  }

  warn(message: string, fields?: LogFields): void {
    this.log('warn', message, fields);
  }

  error(message: string, fields?: LogFields): void {
    this.log('error', message, fields);
  }

  child(scope: string, fields?: LogFields): Logger {
    return new ScopedLogger(scope, { ...this.base, ...fields });
  }
}

/** Cria um logger com escopo fixo (ex.: `createLogger('http')`) e campos padrão opcionais. */
export function createLogger(scope: string, fields?: LogFields): Logger {
  return new ScopedLogger(scope, fields);
}

/** Logger raiz — prefira `createLogger` com escopo específico. */
export const logger = createLogger('aluxen');

export { serializeError };
