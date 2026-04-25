import pg from 'pg';
import bcrypt from 'bcryptjs';
import { createId } from './app/utils/id';

const { Pool } = pg;

async function main() {
  const pool = new Pool({
    connectionString: process.env.DATABASE_URL || 'postgres://user:password@localhost:5432/rental?sslmode=disable',
  });

  const passwordHash = await bcrypt.hash('admin123', 10);
  await pool.query(
    'INSERT INTO users (id, username, password_hash) VALUES ($1, $2, $3)',
    [createId(), 'admin', passwordHash],
  );
  
  console.log('✅ Usuário admin criado');
  await pool.end();
}

main();
