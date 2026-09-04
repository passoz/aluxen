import { defineConfig } from 'drizzle-kit';
import path from 'path';

function resolveDbUrl(): string {
  if (process.env.DATABASE_URL) return process.env.DATABASE_URL.replace(/^file:/, '');
  if (process.env.DB_FILE) return process.env.DB_FILE.replace(/^file:/, '');
  if (process.env.DB_DIR) return path.join(process.env.DB_DIR, 'aluxen.db');
  return 'aluxen.db';
}

export default defineConfig({
  schema: './src/db/schema.ts',
  out: './drizzle',
  dialect: 'sqlite',
  dbCredentials: {
    url: resolveDbUrl(),
  },
});

