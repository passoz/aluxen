import bcrypt from 'bcryptjs';
import { createClient } from '@libsql/client';
import path from 'path';

declare global {
  var __aluxenClient: ReturnType<typeof createClient> | undefined;
  var __aluxenDbInit: Promise<void> | undefined;
}

function getDbPath() {
  const dbDir = process.env.DB_DIR || '/data';
  return path.join(dbDir, 'aluxen.db');
}

function createClient_() {
  const dbPath = getDbPath();
  return createClient({
    url: `file:${dbPath}`,
  });
}

export function getClient() {
  if (!global.__aluxenClient) {
    global.__aluxenClient = createClient_();
  }
  return global.__aluxenClient;
}

export async function initializeDatabase() {
  if (!global.__aluxenDbInit) {
    global.__aluxenDbInit = (async () => {
      const client = getClient();
      await client.execute(`
        CREATE TABLE IF NOT EXISTS users (
          id TEXT PRIMARY KEY,
          username TEXT UNIQUE NOT NULL,
          password_hash TEXT NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now'))
        )
      `);

      await client.execute(`
        CREATE TABLE IF NOT EXISTS tenants (
          id TEXT PRIMARY KEY,
          name TEXT NOT NULL,
          email TEXT NOT NULL,
          phone TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        )
      `);

      await client.execute(`
        CREATE TABLE IF NOT EXISTS properties (
          id TEXT PRIMARY KEY,
          name TEXT NOT NULL,
          address TEXT NOT NULL,
          city TEXT NOT NULL,
          state TEXT NOT NULL,
          zip TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        )
      `);

      await client.execute(`
        CREATE TABLE IF NOT EXISTS leases (
          id TEXT PRIMARY KEY,
          tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
          property_id TEXT NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
          start_date TEXT NOT NULL,
          end_date TEXT NOT NULL,
          rent_amount REAL NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        )
      `);

      await client.execute(`
        CREATE TABLE IF NOT EXISTS receipts (
          id TEXT PRIMARY KEY,
          lease_id TEXT NOT NULL REFERENCES leases(id) ON DELETE CASCADE,
          due_date TEXT NOT NULL,
          paid_at TEXT,
          amount REAL NOT NULL,
          pdf_path TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now'))
        )
      `);

      const seedUsername = process.env.SEED_USERNAME || 'admin';
      const seedPassword = process.env.SEED_PASSWORD || 'admin123';
      const existing = await client.execute({
        sql: 'SELECT id FROM users WHERE username = ?',
        args: [seedUsername],
      });
      if (!existing.rows.length) {
        const passwordHash = await bcrypt.hash(seedPassword, 10);
        await client.execute({
          sql: 'INSERT INTO users (id, username, password_hash) VALUES (?, ?, ?)',
          args: [crypto.randomUUID(), seedUsername, passwordHash],
        });
      }
    })();
  }

  await global.__aluxenDbInit;
}

export async function query<T>(sql: string, args: unknown[] = []): Promise<T[]> {
  await initializeDatabase();
  const result = await getClient().execute({ sql, args });
  return result.rows as T[];
}

export async function queryOne<T>(sql: string, args: unknown[] = []): Promise<T | null> {
  const rows = await query<T>(sql, args);
  return rows[0] || null;
}

export async function execute(sql: string, args: unknown[] = []): Promise<void> {
  await initializeDatabase();
  await getClient().execute({ sql, args });
}