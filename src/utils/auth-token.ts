import { createHmac, timingSafeEqual } from 'node:crypto';

const SECRET = process.env.SESSION_SECRET || 'aluxen-authenticity-secret-key-2026';

export function generateAuthToken(type: 'receipt' | 'lease', id: string): string {
  return createHmac('sha256', SECRET)
    .update(`${type}:${id}`)
    .digest('hex')
    .slice(0, 32);
}

export function formatAuthCode(token: string): string {
  const clean = token.toUpperCase().replace(/[^A-Z0-9]/g, '');
  const part1 = clean.slice(0, 4);
  const part2 = clean.slice(4, 8);
  const part3 = clean.slice(8, 12);
  const part4 = clean.slice(12, 16);
  return `AUTH-${part1}-${part2}-${part3}-${part4}`;
}

export function getValidationUrl(type: 'receipt' | 'lease', id: string, token: string, baseUrl?: string): string {
  const base = baseUrl || process.env.APP_URL || process.env.BASE_URL || 'http://localhost:3000';
  const path = type === 'receipt' ? 'recibo' : 'contrato';
  return `${base.replace(/\/$/, '')}/validar/${path}/${id}?token=${token}`;
}

export function verifyAuthToken(type: 'receipt' | 'lease', id: string, token: string): boolean {
  if (!token || typeof token !== 'string') return false;
  const expected = generateAuthToken(type, id);
  if (token.length !== expected.length) return false;

  try {
    return timingSafeEqual(Buffer.from(token), Buffer.from(expected));
  } catch {
    return false;
  }
}
