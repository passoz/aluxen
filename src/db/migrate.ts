import { migrate } from 'drizzle-orm/bun-sqlite/migrator';
import { db } from './index';

export function runMigrations() {
  migrate(db, { migrationsFolder: './drizzle' });
}

if (import.meta.main) {
  console.log('Running migrations...');
  runMigrations();
  console.log('Migrations complete!');
}
