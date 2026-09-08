import { migrate } from 'drizzle-orm/bun-sqlite/migrator';
import { db, getSqlite } from './index';
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';

export function runMigrations(customMigrationsFolder?: string) {
  const migrationsFolder = customMigrationsFolder || path.join(process.cwd(), 'drizzle');
  const sqlite = getSqlite();

  // 1. Garantir que a tabela de controle __drizzle_migrations existe
  sqlite.run(`
    CREATE TABLE IF NOT EXISTS "__drizzle_migrations" (
      id SERIAL PRIMARY KEY,
      hash text NOT NULL,
      created_at numeric
    )
  `);

  // 2. Verificação de sincronização segura (idempotência):
  // Se o banco de dados já possui tabelas e colunas aplicadas (por exemplo, originárias de main ou de execuções anteriores),
  // garantimos que as migrações cujas alterações já estejam presentes sejam registradas em __drizzle_migrations,
  // evitando erros como "duplicate column name" no SQLite e garantindo que os dados existentes nunca sejam corrompidos.
  try {
    const journalPath = path.join(migrationsFolder, 'meta', '_journal.json');
    if (fs.existsSync(journalPath)) {
      const journal = JSON.parse(fs.readFileSync(journalPath, 'utf-8'));
      for (const entry of journal.entries) {
        const migrationSqlPath = path.join(migrationsFolder, `${entry.tag}.sql`);
        if (!fs.existsSync(migrationSqlPath)) continue;

        const recorded = sqlite
          .query<{ created_at: number }, [number]>(
            'SELECT created_at FROM __drizzle_migrations WHERE created_at = ?'
          )
          .get(entry.when);

        if (!recorded) {
          if (entry.tag === '0001_heavy_otto_octavius') {
            // Verificar se as colunas auth_token já existem em leases e receipts
            const leaseCols = sqlite
              .query<{ name: string }, []>("PRAGMA table_info('leases')")
              .all();
            const receiptCols = sqlite
              .query<{ name: string }, []>("PRAGMA table_info('receipts')")
              .all();

            const hasLeaseAuthToken = leaseCols.some((c) => c.name === 'auth_token');
            const hasReceiptAuthToken = receiptCols.some((c) => c.name === 'auth_token');

            if (hasLeaseAuthToken && hasReceiptAuthToken) {
              const sqlContent = fs.readFileSync(migrationSqlPath, 'utf-8');
              const hash = crypto.createHash('sha256').update(sqlContent).digest('hex');
              sqlite
                .query('INSERT INTO __drizzle_migrations ("hash", "created_at") VALUES (?, ?)')
                .run(hash, entry.when);
            }
          } else if (entry.tag === '0002_fair_absorbing_man') {
            const receiptCols = sqlite
              .query<{ name: string; notnull: number }, []>("PRAGMA table_info('receipts')")
              .all();
            const hasCategory = receiptCols.some((c) => c.name === 'category');
            const leaseCol = receiptCols.find((c) => c.name === 'lease_id');
            const isLeaseNullable = leaseCol ? leaseCol.notnull === 0 : false;

            if (hasCategory && isLeaseNullable) {
              const sqlContent = fs.readFileSync(migrationSqlPath, 'utf-8');
              const hash = crypto.createHash('sha256').update(sqlContent).digest('hex');
              sqlite
                .query('INSERT INTO __drizzle_migrations ("hash", "created_at") VALUES (?, ?)')
                .run(hash, entry.when);
            }
          }
        }
      }
    }
  } catch (err) {
    console.warn('Pre-migration check warning:', err);
  }

  // 3. Executar migrações do Drizzle
  migrate(db, { migrationsFolder });
}

if (import.meta.main) {
  console.log('Running migrations...');
  runMigrations();
  console.log('Migrations complete!');
}

