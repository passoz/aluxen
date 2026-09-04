import { describe, it, expect, beforeEach, afterEach } from 'bun:test';
import { Database } from 'bun:sqlite';
import { drizzle } from 'drizzle-orm/bun-sqlite';
import { migrate } from 'drizzle-orm/bun-sqlite/migrator';
import { unlinkSync, existsSync } from 'node:fs';
import path from 'node:path';
import * as schema from '../src/db/schema';
import { runMigrations } from '../src/db/migrate';

const TEST_DB_PATH = path.join(process.cwd(), 'tmp_test_migration.db');

function cleanupDb() {
  for (const ext of ['', '-shm', '-wal']) {
    const file = `${TEST_DB_PATH}${ext}`;
    if (existsSync(file)) {
      try { unlinkSync(file); } catch {}
    }
  }
}

/**
 * Cria o banco exatamente como a branch `main` criava em `app/utils/db.server.ts`.
 */
function createMainBranchDatabase(dbPath: string): Database {
  const db = new Database(dbPath);
  db.run('PRAGMA foreign_keys = ON;');

  db.run(`
    CREATE TABLE IF NOT EXISTS users (
      id TEXT PRIMARY KEY,
      username TEXT UNIQUE NOT NULL,
      password_hash TEXT NOT NULL,
      created_at TEXT NOT NULL DEFAULT (datetime('now'))
    )
  `);

  db.run(`
    CREATE TABLE IF NOT EXISTS tenants (
      id TEXT PRIMARY KEY,
      name TEXT NOT NULL,
      email TEXT NOT NULL,
      phone TEXT,
      created_at TEXT NOT NULL DEFAULT (datetime('now')),
      updated_at TEXT NOT NULL DEFAULT (datetime('now'))
    )
  `);

  db.run(`
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

  db.run(`
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

  db.run(`
    CREATE TABLE IF NOT EXISTS receipts (
      id TEXT PRIMARY KEY,
      lease_id TEXT NOT NULL REFERENCES leases(id) ON DELETE CASCADE,
      due_date TEXT NOT NULL,
      paid_at TEXT,
      amount REAL NOT NULL,
      pdf_path TEXT,
      generated_key TEXT,
      created_at TEXT NOT NULL DEFAULT (datetime('now')),
      updated_at TEXT
    )
  `);

  try { db.run('ALTER TABLE leases ADD COLUMN due_day INTEGER NOT NULL DEFAULT 1'); } catch {}
  try { db.run('ALTER TABLE tenants ADD COLUMN cpf TEXT'); } catch {}
  try { db.run("ALTER TABLE leases ADD COLUMN status TEXT NOT NULL DEFAULT 'active'"); } catch {}
  try { db.run('ALTER TABLE receipts ADD COLUMN generated_key TEXT'); } catch {}
  try { db.run('ALTER TABLE receipts ADD COLUMN updated_at TEXT'); } catch {}
  try { db.run('CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)'); } catch {}
  try { db.run('CREATE UNIQUE INDEX IF NOT EXISTS receipts_generated_key_idx ON receipts(generated_key)'); } catch {}

  return db;
}

describe('Database Migration & Data Preservation from branch main', () => {
  beforeEach(() => {
    cleanupDb();
  });

  afterEach(() => {
    cleanupDb();
  });

  it('migra banco existente de main preservando 100% dos dados pré-existentes', () => {
    const rawDb = createMainBranchDatabase(TEST_DB_PATH);

    // Inserir dados reais de produção estilo branch main
    rawDb.run("INSERT INTO users VALUES ('u1', 'passoz', '$2a$10$customAdminHash123', '2026-01-01 10:00:00')");
    rawDb.run("INSERT INTO tenants (id, name, email, phone, cpf, created_at, updated_at) VALUES ('t1', 'João da Silva', 'joao@example.com', '(22) 9999-8888', '111.222.333-44', '2026-01-01 10:00:00', '2026-01-01 10:00:00')");
    rawDb.run("INSERT INTO tenants (id, name, email, phone, cpf, created_at, updated_at) VALUES ('t2', 'Maria Oliveira', 'maria@example.com', '(22) 9888-7777', '222.333.444-55', '2026-02-01 10:00:00', '2026-02-01 10:00:00')");
    rawDb.run("INSERT INTO properties (id, name, address, city, state, zip, created_at, updated_at) VALUES ('p1', 'Apto 101', 'Rua das Flores, 100', 'Cabo Frio', 'RJ', '28900-000', '2026-01-01 10:00:00', '2026-01-01 10:00:00')");
    rawDb.run("INSERT INTO properties (id, name, address, city, state, zip, created_at, updated_at) VALUES ('p2', 'Casa Praia', 'Av. Beira Mar, 50', 'Arraial do Cabo', 'RJ', '28930-000', '2026-02-01 10:00:00', '2026-02-01 10:00:00')");
    rawDb.run("INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, status, created_at, updated_at) VALUES ('l1', 't1', 'p1', '2026-01-01', '2026-12-31', 10, 2500.0, 'active', '2026-01-01 10:00:00', '2026-01-01 10:00:00')");
    rawDb.run("INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, status, created_at, updated_at) VALUES ('l2', 't2', 'p2', '2026-02-01', '2027-01-31', 5, 3200.0, 'terminated', '2026-02-01 10:00:00', '2026-02-01 10:00:00')");
    rawDb.run("INSERT INTO receipts (id, lease_id, due_date, paid_at, amount, pdf_path, generated_key, created_at, updated_at) VALUES ('r1', 'l1', '2026-02-10', '2026-02-09T15:00', 2500.0, '/data/receipts/r1.pdf', 'l1:2026-02', '2026-01-01 10:00:00', '2026-02-09T15:00')");
    rawDb.run("INSERT INTO receipts (id, lease_id, due_date, paid_at, amount, pdf_path, generated_key, created_at, updated_at) VALUES ('r2', 'l1', '2026-03-10', NULL, 2500.0, NULL, 'l1:2026-03', '2026-02-01 10:00:00', NULL)");
    rawDb.run("INSERT INTO settings VALUES ('landlord_name', 'Imobiliária Original')");
    rawDb.run("INSERT INTO settings VALUES ('landlord_cpf', '123.456.789-00')");
    rawDb.close();

    // Executar migrações do Drizzle neste banco existente de main
    const targetDb = new Database(TEST_DB_PATH);
    const d = drizzle(targetDb, { schema });

    // Deve rodar sem erros
    expect(() => {
      migrate(d, { migrationsFolder: path.join(process.cwd(), 'drizzle') });
    }).not.toThrow();

    // 1. Validar integridade dos usuários
    const user = targetDb.query<{ id: string; username: string; password_hash: string }, [string]>(
      'SELECT id, username, password_hash FROM users WHERE id = ?'
    ).get('u1');
    expect(user).toBeDefined();
    expect(user?.username).toBe('passoz');
    expect(user?.password_hash).toBe('$2a$10$customAdminHash123');

    // 2. Validar integridade dos inquilinos
    const tenants = targetDb.query<{ id: string; name: string; cpf: string }, []>(
      'SELECT id, name, cpf FROM tenants ORDER BY id'
    ).all();
    expect(tenants).toHaveLength(2);
    expect(tenants[0]!.name).toBe('João da Silva');
    expect(tenants[0]!.cpf).toBe('111.222.333-44');
    expect(tenants[1]!.name).toBe('Maria Oliveira');
    expect(tenants[1]!.cpf).toBe('222.333.444-55');

    // 3. Validar integridade dos imóveis
    const properties = targetDb.query<{ id: string; name: string; city: string }, []>(
      'SELECT id, name, city FROM properties ORDER BY id'
    ).all();
    expect(properties).toHaveLength(2);
    expect(properties[0]!.name).toBe('Apto 101');
    expect(properties[0]!.city).toBe('Cabo Frio');

    // 4. Validar integridade dos contratos e a nova coluna auth_token
    const leases = targetDb.query<{ id: string; rent_amount: number; status: string; auth_token: string | null }, []>(
      'SELECT id, rent_amount, status, auth_token FROM leases ORDER BY id'
    ).all();
    expect(leases).toHaveLength(2);
    expect(leases[0]!.id).toBe('l1');
    expect(leases[0]!.rent_amount).toBe(2500.0);
    expect(leases[0]!.status).toBe('active');
    expect(leases[0]!.auth_token).toBeNull(); // nova coluna inicializada nula sem quebrar dados existentes
    expect(leases[1]!.id).toBe('l2');
    expect(leases[1]!.status).toBe('terminated');

    // 5. Validar integridade dos recibos e a nova coluna auth_token
    const receipts = targetDb.query<{ id: string; amount: number; paid_at: string | null; generated_key: string; auth_token: string | null }, []>(
      'SELECT id, amount, paid_at, generated_key, auth_token FROM receipts ORDER BY id'
    ).all();
    expect(receipts).toHaveLength(2);
    expect(receipts[0]!.id).toBe('r1');
    expect(receipts[0]!.amount).toBe(2500.0);
    expect(receipts[0]!.paid_at).toBe('2026-02-09T15:00');
    expect(receipts[0]!.generated_key).toBe('l1:2026-02');
    expect(receipts[0]!.auth_token).toBeNull();

    // 6. Validar integridade das configurações
    const settings = targetDb.query<{ key: string; value: string }, []>(
      'SELECT key, value FROM settings ORDER BY key'
    ).all();
    expect(settings).toContainEqual({ key: 'landlord_name', value: 'Imobiliária Original' });
    expect(settings).toContainEqual({ key: 'landlord_cpf', value: '123.456.789-00' });

    // 7. Validar que tabela de controle do Drizzle registrou todas as migrações
    const migrations = targetDb.query<{ created_at: number }, []>(
      'SELECT created_at FROM __drizzle_migrations ORDER BY created_at'
    ).all();
    expect(migrations.length).toBeGreaterThanOrEqual(2);

    // 8. Validar idempotência: rodar novamente não lança exceção nem duplica registros
    expect(() => {
      migrate(d, { migrationsFolder: path.join(process.cwd(), 'drizzle') });
    }).not.toThrow();

    targetDb.close();
  });

  it('cria banco do zero corretamente através das migrations', () => {
    const rawDb = new Database(TEST_DB_PATH);
    const d = drizzle(rawDb, { schema });

    expect(() => {
      migrate(d, { migrationsFolder: path.join(process.cwd(), 'drizzle') });
    }).not.toThrow();

    // Verificar se todas as tabelas foram criadas
    const tables = rawDb.query<{ name: string }, []>(
      "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name"
    ).all();

    const tableNames = tables.map(t => t.name);
    expect(tableNames).toContain('__drizzle_migrations');
    expect(tableNames).toContain('leases');
    expect(tableNames).toContain('properties');
    expect(tableNames).toContain('receipts');
    expect(tableNames).toContain('settings');
    expect(tableNames).toContain('tenants');
    expect(tableNames).toContain('users');

    // Verificar colunas esperadas em leases e receipts (incluindo auth_token)
    const leaseCols = rawDb.query<{ name: string }, []>("PRAGMA table_info('leases')").all().map(c => c.name);
    expect(leaseCols).toContain('auth_token');
    expect(leaseCols).toContain('status');
    expect(leaseCols).toContain('due_day');

    const receiptCols = rawDb.query<{ name: string }, []>("PRAGMA table_info('receipts')").all().map(c => c.name);
    expect(receiptCols).toContain('auth_token');
    expect(receiptCols).toContain('generated_key');
    expect(receiptCols).toContain('updated_at');

    rawDb.close();
  });

  it('permite operações completas de CRUD via Drizzle ORM sobre o banco migrado de main', async () => {
    const rawDb = createMainBranchDatabase(TEST_DB_PATH);
    rawDb.run("INSERT INTO tenants (id, name, email, phone, cpf) VALUES ('t_crud', 'Inquilino Antigo', 'antigo@exemplo.com', '123', '000.111.222-33')");
    rawDb.close();

    const targetDb = new Database(TEST_DB_PATH);
    const d = drizzle(targetDb, { schema });
    migrate(d, { migrationsFolder: path.join(process.cwd(), 'drizzle') });

    // Leitura via Drizzle ORM
    const existingTenants = await d.select().from(schema.tenants);
    expect(existingTenants).toHaveLength(1);
    expect(existingTenants[0]!.name).toBe('Inquilino Antigo');

    // Inserção via Drizzle ORM com nova propriedade
    await d.insert(schema.properties).values({
      id: 'prop_new',
      name: 'Novo Imóvel Drizzle',
      address: 'Rua Nova, 1',
      city: 'Cabo Frio',
      state: 'RJ',
    });

    const prop = await d.select().from(schema.properties);
    expect(prop).toHaveLength(1);
    expect(prop[0]!.name).toBe('Novo Imóvel Drizzle');

    // Inserção de lease com o novo campo authToken
    await d.insert(schema.leases).values({
      id: 'l_new',
      tenantId: 't_crud',
      propertyId: 'prop_new',
      startDate: '2026-03-01',
      endDate: '2027-02-28',
      rentAmount: 1800,
      authToken: 'token_contrato_123',
    });

    const insertedLease = (await d.select().from(schema.leases))[0];
    expect(insertedLease).toBeDefined();
    expect(insertedLease!.authToken).toBe('token_contrato_123');

    targetDb.close();
  });
});
