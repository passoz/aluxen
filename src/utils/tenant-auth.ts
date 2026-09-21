import bcrypt from 'bcryptjs';
import { eq, sql } from 'drizzle-orm';
import { db, initializeDatabase } from '../db';
import { tenants } from '../db/schema';

/** Senha inicial (e login) do locatário: o próprio CPF, sempre sem pontuação. */
export const CLIENT_PASSWORD_MIN_LENGTH = 6;

export type ClientIdentity = {
  id: string;
  name: string;
  /** `true` enquanto a senha do primeiro acesso (CPF) não foi trocada. */
  mustChangePassword: boolean;
};

/** Remove máscara do CPF — login e senha padrão usam apenas dígitos. */
export function normalizeCpf(value: string | null | undefined): string {
  return (value || '').replace(/\D/g, '');
}

/** CPF do cadastro reduzido a dígitos, direto no SQL, para casar com o login informado. */
const cpfDigits = sql<string>`replace(replace(replace(coalesce(${tenants.cpf}, ''), '.', ''), '-', ''), ' ', '')`;

async function findTenantByCpf(cpf: string) {
  const rows = await db
    .select({
      id: tenants.id,
      name: tenants.name,
      cpf: tenants.cpf,
      passwordHash: tenants.passwordHash,
      passwordChangedAt: tenants.passwordChangedAt,
    })
    .from(tenants)
    .where(sql`${cpfDigits} = ${cpf}`)
    .orderBy(tenants.createdAt)
    .limit(1);
  return rows[0] ?? null;
}

/** Compara a senha informada com a credencial vigente (hash) ou com o CPF (senha padrão). */
async function passwordMatches(
  tenant: { cpf: string | null; passwordHash: string | null },
  password: string
): Promise<boolean> {
  if (tenant.passwordHash) return bcrypt.compare(password, tenant.passwordHash);
  const cpf = normalizeCpf(tenant.cpf);
  return cpf.length > 0 && normalizeCpf(password) === cpf;
}

/**
 * Autentica o locatário na Área do Cliente pelo CPF (com ou sem pontuação).
 * Sem senha própria cadastrada, a senha válida é o próprio CPF.
 */
export async function loginTenant(identifier: string, password: string): Promise<ClientIdentity | null> {
  await initializeDatabase();
  const cpf = normalizeCpf(identifier);
  if (cpf.length !== 11 || !password) return null;

  const tenant = await findTenantByCpf(cpf);
  if (!tenant) return null;
  if (!(await passwordMatches(tenant, password))) return null;

  return {
    id: tenant.id,
    name: tenant.name,
    mustChangePassword: !tenant.passwordChangedAt,
  };
}

/** Estado da sessão do locatário a cada requisição (a senha padrão reabre a troca obrigatória). */
export async function getClientIdentity(tenantId: string): Promise<ClientIdentity | null> {
  await initializeDatabase();
  const rows = await db
    .select({ id: tenants.id, name: tenants.name, passwordChangedAt: tenants.passwordChangedAt })
    .from(tenants)
    .where(eq(tenants.id, tenantId))
    .limit(1);
  const tenant = rows[0];
  if (!tenant) return null;
  return { id: tenant.id, name: tenant.name, mustChangePassword: !tenant.passwordChangedAt };
}

/** Troca de senha do locatário: exige a senha atual (ou o CPF, no primeiro acesso). */
export async function changeTenantPassword(tenantId: string, currentPassword: string, newPassword: string): Promise<void> {
  await initializeDatabase();
  const rows = await db
    .select({ id: tenants.id, cpf: tenants.cpf, passwordHash: tenants.passwordHash })
    .from(tenants)
    .where(eq(tenants.id, tenantId))
    .limit(1);
  const tenant = rows[0];
  if (!tenant) throw new Error('Cadastro de locatário não encontrado.');

  if (!(await passwordMatches(tenant, currentPassword))) {
    throw new Error('Senha atual incorreta.');
  }

  const password = newPassword.trim();
  if (password.length < CLIENT_PASSWORD_MIN_LENGTH) {
    throw new Error(`A nova senha deve ter no mínimo ${CLIENT_PASSWORD_MIN_LENGTH} caracteres.`);
  }

  const cpf = normalizeCpf(tenant.cpf);
  if (cpf && normalizeCpf(password) === cpf) {
    throw new Error('A nova senha não pode ser o seu CPF.');
  }

  const passwordHash = await bcrypt.hash(password, 10);
  await db
    .update(tenants)
    .set({ passwordHash, passwordChangedAt: sql`(datetime('now'))`, updatedAt: sql`(datetime('now'))` })
    .where(eq(tenants.id, tenantId));
}
