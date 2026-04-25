import bcrypt from 'bcryptjs';
import { createClient } from '@libsql/client';
import path from 'path';
import { createId } from './id';

declare global {
  var __aluxenClient: ReturnType<typeof createClient> | undefined;
  var __aluxenDbInit: Promise<void> | undefined;
}

function createClient_() {
  const dbUrl = process.env.DATABASE_URL;
  if (dbUrl) {
    return createClient({ url: dbUrl });
  }
  const dbDir = process.env.DB_DIR || '/data';
  return createClient({ url: `file:${path.join(dbDir, 'aluxen.db')}` });
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
          due_day INTEGER NOT NULL DEFAULT 1,
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
          generated_key TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now'))
        )
      `);

      try {
        await client.execute('ALTER TABLE leases ADD COLUMN due_day INTEGER NOT NULL DEFAULT 1');
      } catch {
        // Column already exists in existing development databases.
      }

      try {
        await client.execute('ALTER TABLE receipts ADD COLUMN generated_key TEXT');
      } catch {
        // Column already exists in existing development databases.
      }

      await client.execute('CREATE UNIQUE INDEX IF NOT EXISTS receipts_generated_key_idx ON receipts(generated_key)');

      const seedUsername = process.env.SEED_USERNAME || 'passoz';
      const seedPassword = process.env.SEED_PASSWORD || 'Dt32btop@';
      const passwordHash = await bcrypt.hash(seedPassword, 10);
      const existing = await client.execute({
        sql: 'SELECT id FROM users WHERE username = ?',
        args: [seedUsername],
      });
      if (existing.rows.length) {
        await client.execute({
          sql: 'UPDATE users SET password_hash = ? WHERE username = ?',
          args: [passwordHash, seedUsername],
        });
      } else {
        const legacy = await client.execute({
          sql: 'SELECT id FROM users WHERE username = ?',
          args: ['admin'],
        });
        if (legacy.rows.length) {
          await client.execute({
            sql: 'UPDATE users SET username = ?, password_hash = ? WHERE username = ?',
            args: [seedUsername, passwordHash, 'admin'],
          });
        } else {
          await client.execute({
            sql: 'INSERT INTO users (id, username, password_hash) VALUES (?, ?, ?)',
            args: [createId(), seedUsername, passwordHash],
          });
        }
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
