import bcrypt from 'bcryptjs';
import { execute, queryOne } from './db.server';

type UserRow = {
  id: string;
  username: string;
  password_hash: string;
};

export async function loginUser(username: string, password: string) {
  const user = await queryOne<UserRow>('SELECT id, username, password_hash FROM users WHERE username = $1', [username]);
  if (!user) return null;

  const valid = await bcrypt.compare(password, user.password_hash);
  if (!valid) return null;

  return { id: user.id, username: user.username };
}

export async function registerUser(username: string, password: string) {
  const existing = await queryOne<{ id: string }>('SELECT id FROM users WHERE username = $1', [username]);
  if (existing) {
    return null;
  }

  const passwordHash = await bcrypt.hash(password, 10);
  const id = crypto.randomUUID();
  await execute('INSERT INTO users (id, username, password_hash) VALUES ($1, $2, $3)', [id, username, passwordHash]);
  return { id, username };
}
