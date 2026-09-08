import { Database } from 'bun:sqlite';
import type { SQLQueryBindings } from 'bun:sqlite';
import { drizzle } from 'drizzle-orm/bun-sqlite';
import bcrypt from 'bcryptjs';
import path from 'path';
import { existsSync, mkdirSync } from 'fs';
import * as schema from './schema';
import { createId } from '../utils/id';
import { runMigrations } from './migrate';

declare global {
  var __aluxenSqlite: Database | undefined;
  var __aluxenDbInit: Promise<void> | undefined;
}

function resolveDbPath(): string {
  const dbUrl = process.env.DATABASE_URL;
  if (dbUrl) {
    return dbUrl.replace(/^file:/, '');
  }
  const dbDir = process.env.DB_DIR;
  if (dbDir) {
    if (!existsSync(dbDir)) {
      mkdirSync(dbDir, { recursive: true });
    }
    return path.join(dbDir, 'aluxen.db');
  }
  return path.join(process.cwd(), 'aluxen.db');
}

export function getSqlite(): Database {
  if (!global.__aluxenSqlite) {
    const dbPath = resolveDbPath();
    const db = new Database(dbPath);
    db.run('PRAGMA journal_mode = WAL;');
    db.run('PRAGMA foreign_keys = ON;');
    global.__aluxenSqlite = db;
  }
  return global.__aluxenSqlite;
}

export const sqlite = getSqlite();
export const db = drizzle(sqlite, { schema });

export async function initializeDatabase(): Promise<void> {
  if (!global.__aluxenDbInit) {
    global.__aluxenDbInit = (async () => {
      // 1. Executa migrações Drizzle garantindo que tabelas e colunas estejam na versão mais recente
      runMigrations();

      const client = getSqlite();

      // 2. Garantias adicionais de compatibilidade e idempotência
      client.run('CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)');
      client.run('CREATE UNIQUE INDEX IF NOT EXISTS receipts_generated_key_idx ON receipts(generated_key)');

      // Migrações incrementais de colunas caso venham de schemas anteriores
      try { client.run('ALTER TABLE leases ADD COLUMN due_day INTEGER NOT NULL DEFAULT 1'); } catch {}
      try { client.run('ALTER TABLE tenants ADD COLUMN cpf TEXT'); } catch {}
      try { client.run("ALTER TABLE leases ADD COLUMN status TEXT NOT NULL DEFAULT 'active'"); } catch {}
      try { client.run('ALTER TABLE receipts ADD COLUMN generated_key TEXT'); } catch {}
      try { client.run('ALTER TABLE receipts ADD COLUMN updated_at TEXT'); } catch {}
      try { client.run('ALTER TABLE leases ADD COLUMN auth_token TEXT'); } catch {}
      try { client.run('ALTER TABLE receipts ADD COLUMN auth_token TEXT'); } catch {}

      // Seed do usuário administrador (apenas se não existir usuário cadastrado)
      const seedUsername = process.env.SEED_USERNAME || 'passoz';
      const seedPassword = process.env.SEED_PASSWORD || 'senha-de-teste';
      const passwordHash = await bcrypt.hash(seedPassword, 10);

      const existing = client.query<{ id: string }, [string]>('SELECT id FROM users WHERE username = ?').get(seedUsername);
      if (!existing) {
        const legacy = client.query<{ id: string }, [string]>('SELECT id FROM users WHERE username = ?').get('admin');
        if (legacy) {
          client.query('UPDATE users SET username = ? WHERE username = ?').run(seedUsername, 'admin');
        } else {
          client.query('INSERT INTO users (id, username, password_hash) VALUES (?, ?, ?)').run(createId(), seedUsername, passwordHash);
        }
      }

      // Seed de configurações padrão (sem dados pessoais sensíveis)
      const defaultLandlordName = client.query<{ value: string }, [string]>('SELECT value FROM settings WHERE key = ?').get('landlord_name');
      if (!defaultLandlordName) {
        client.query('INSERT INTO settings (key, value) VALUES (?, ?)').run('landlord_name', process.env.DEFAULT_LANDLORD_NAME || 'Locador');
      }
      const defaultLandlordCpf = client.query<{ value: string }, [string]>('SELECT value FROM settings WHERE key = ?').get('landlord_cpf');
      if (!defaultLandlordCpf) {
        client.query('INSERT INTO settings (key, value) VALUES (?, ?)').run('landlord_cpf', process.env.DEFAULT_LANDLORD_CPF || '');
      }
    })();
  }

  await global.__aluxenDbInit;
}

// Helpers compatíveis com a assinatura do main
export async function query<T>(sql: string, args: SQLQueryBindings[] = []): Promise<T[]> {
  await initializeDatabase();
  const normalizedSql = sql.replace(/\$(\d+)/g, '?');
  const stmt = getSqlite().query<T, SQLQueryBindings[]>(normalizedSql);
  return stmt.all(...args);
}

export async function queryOne<T>(sql: string, args: SQLQueryBindings[] = []): Promise<T | null> {
  const rows = await query<T>(sql, args);
  return rows[0] || null;
}

export async function execute(sql: string, args: SQLQueryBindings[] = []): Promise<void> {
  await initializeDatabase();
  const normalizedSql = sql.replace(/\$(\d+)/g, '?');
  const stmt = getSqlite().query<unknown, SQLQueryBindings[]>(normalizedSql);
  stmt.run(...args);
}

export async function getSetting(key: string): Promise<string | null> {
  const row = await queryOne<{ value: string }>('SELECT value FROM settings WHERE key = ?', [key]);
  return row?.value ?? null;
}

export async function setSetting(key: string, value: string): Promise<void> {
  await execute('INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)', [key, value]);
}
