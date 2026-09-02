import { Database } from 'bun:sqlite';
import { drizzle } from 'drizzle-orm/bun-sqlite';
import * as schema from './schema';

const dbPath = process.env.DB_FILE || 'aluxen.db';
export const sqlite = new Database(dbPath);

sqlite.run('PRAGMA journal_mode = WAL;');
sqlite.run('PRAGMA foreign_keys = ON;');

export const db = drizzle(sqlite, { schema });
