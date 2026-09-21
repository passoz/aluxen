import bcrypt from 'bcryptjs';
import { eq, sql } from 'drizzle-orm';
import { db, initializeDatabase } from '../db';
import { users } from '../db/schema';
import { createId } from './id';

export async function loginUser(
  username: string,
  password: string
): Promise<{ id: string; username: string; mustChangePassword: boolean } | null> {
  await initializeDatabase();
  const rows = await db
    .select({ id: users.id, username: users.username, passwordHash: users.passwordHash, passwordChangedAt: users.passwordChangedAt })
    .from(users)
    .where(eq(users.username, username))
    .limit(1);
  const user = rows[0];
  if (!user) return null;

  const valid = await bcrypt.compare(password, user.passwordHash);
  if (!valid) return null;

  // Enquanto a senha semeada (env `SEED_PASSWORD`) não for trocada, o painel exige a troca.
  return { id: user.id, username: user.username, mustChangePassword: !user.passwordChangedAt };
}

/** `true` enquanto o administrador ainda usa a senha padrão semeada no primeiro boot. */
export async function adminMustChangePassword(userId: string): Promise<boolean> {
  await initializeDatabase();
  const rows = await db
    .select({ passwordChangedAt: users.passwordChangedAt })
    .from(users)
    .where(eq(users.id, userId))
    .limit(1);
  const user = rows[0];
  if (!user) return false;
  return !user.passwordChangedAt;
}

export async function registerUser(username: string, password: string): Promise<{ id: string; username: string } | null> {
  await initializeDatabase();
  const existing = await db.select({ id: users.id }).from(users).where(eq(users.username, username)).limit(1);
  if (existing.length > 0) {
    return null;
  }

  const passwordHash = await bcrypt.hash(password, 10);
  const id = createId();
  await db.insert(users).values({ id, username, passwordHash });
  return { id, username };
}

export async function changePassword(userId: string, currentPassword: string, newPassword: string): Promise<void> {
  await initializeDatabase();
  const rows = await db
    .select({ id: users.id, username: users.username, passwordHash: users.passwordHash })
    .from(users)
    .where(eq(users.id, userId))
    .limit(1);
  const user = rows[0];
  if (!user) throw new Error('Usuário não encontrado.');

  const valid = await bcrypt.compare(currentPassword, user.passwordHash);
  if (!valid) throw new Error('Senha atual incorreta.');

  if (!newPassword || newPassword.length < 6) throw new Error('Nova senha deve ter no mínimo 6 caracteres.');

  const newHash = await bcrypt.hash(newPassword, 10);
  await db
    .update(users)
    .set({ passwordHash: newHash, passwordChangedAt: sql`(datetime('now'))` })
    .where(eq(users.id, userId));
}