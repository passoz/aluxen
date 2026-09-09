import { Database } from 'bun:sqlite';
import { drizzle } from 'drizzle-orm/bun-sqlite';
import { eq } from 'drizzle-orm';
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

      // 2. Garantias adicionais de compatibilidade e idempotência (plumbing de schema, sem acesso de negócio)
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
      try { client.run('ALTER TABLE receipts ADD COLUMN tenant_id TEXT'); } catch {}
      try { client.run('ALTER TABLE receipts ADD COLUMN property_id TEXT'); } catch {}
      try { client.run("ALTER TABLE receipts ADD COLUMN category TEXT NOT NULL DEFAULT 'rent'"); } catch {}
      try { client.run('ALTER TABLE receipts ADD COLUMN description TEXT'); } catch {}
      try { client.run('ALTER TABLE receipts ADD COLUMN paid_amount REAL'); } catch {}

      // Permitir recibos avulsos sem lease_id caso venha de schema antigo
      try {
        const receiptCols = client.query<{ name: string; notnull: number }, []>("PRAGMA table_info('receipts')").all();
        const leaseCol = receiptCols.find((c) => c.name === 'lease_id');
        if (leaseCol && leaseCol.notnull === 1) {
          client.run('PRAGMA foreign_keys=OFF;');
          client.run(`CREATE TABLE __new_receipts (
            id text PRIMARY KEY NOT NULL,
            lease_id text,
            tenant_id text,
            property_id text,
            category text NOT NULL DEFAULT 'rent',
            description text,
            due_date text NOT NULL,
            paid_at text,
            amount real NOT NULL,
            paid_amount real,
            pdf_path text,
            generated_key text,
            auth_token text,
            created_at text DEFAULT (datetime('now')) NOT NULL,
            updated_at text,
            FOREIGN KEY (lease_id) REFERENCES leases(id) ON UPDATE no action ON DELETE cascade,
            FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON UPDATE no action ON DELETE set null,
            FOREIGN KEY (property_id) REFERENCES properties(id) ON UPDATE no action ON DELETE set null
          );`);
          client.run(`INSERT INTO __new_receipts (id, lease_id, due_date, paid_at, amount, pdf_path, generated_key, auth_token, created_at, updated_at)
            SELECT id, lease_id, due_date, paid_at, amount, pdf_path, generated_key, auth_token, created_at, updated_at FROM receipts;`);
          client.run('DROP TABLE receipts;');
          client.run('ALTER TABLE __new_receipts RENAME TO receipts;');
          client.run('CREATE UNIQUE INDEX IF NOT EXISTS receipts_generated_key_idx ON receipts(generated_key);');
          client.run('PRAGMA foreign_keys=ON;');
        }
      } catch (err) {
        console.warn('Receipts schema alignment notice:', err);
      }

      // Seed do usuário administrador (apenas se não existir usuário cadastrado)
      const seedUsername = process.env.SEED_USERNAME || 'passoz';
      const seedPassword = process.env.SEED_PASSWORD || 'senha-de-teste';
      const passwordHash = await bcrypt.hash(seedPassword, 10);

      const existing = await db.select({ id: schema.users.id }).from(schema.users).where(eq(schema.users.username, seedUsername)).limit(1);
      if (existing.length === 0) {
        const legacy = await db.select({ id: schema.users.id }).from(schema.users).where(eq(schema.users.username, 'admin')).limit(1);
        if (legacy.length > 0) {
          await db.update(schema.users).set({ username: seedUsername }).where(eq(schema.users.username, 'admin'));
        } else {
          await db.insert(schema.users).values({ id: createId(), username: seedUsername, passwordHash });
        }
      }

      // Seed de configurações padrão (sem dados pessoais sensíveis)
      const defaultLandlordName = await db.select({ value: schema.settings.value }).from(schema.settings).where(eq(schema.settings.key, 'landlord_name')).limit(1);
      if (defaultLandlordName.length === 0) {
        await db.insert(schema.settings).values({ key: 'landlord_name', value: process.env.DEFAULT_LANDLORD_NAME || 'Locador' });
      }
      const defaultLandlordCpf = await db.select({ value: schema.settings.value }).from(schema.settings).where(eq(schema.settings.key, 'landlord_cpf')).limit(1);
      if (defaultLandlordCpf.length === 0) {
        await db.insert(schema.settings).values({ key: 'landlord_cpf', value: process.env.DEFAULT_LANDLORD_CPF || '' });
      }
    })();
  }

  await global.__aluxenDbInit;
}

export async function getSetting(key: string): Promise<string | null> {
  await initializeDatabase();
  const rows = await db.select({ value: schema.settings.value }).from(schema.settings).where(eq(schema.settings.key, key)).limit(1);
  return rows[0]?.value ?? null;
}

export async function setSetting(key: string, value: string): Promise<void> {
  await initializeDatabase();
  await db
    .insert(schema.settings)
    .values({ key, value })
    .onConflictDoUpdate({ target: schema.settings.key, set: { value } });
}