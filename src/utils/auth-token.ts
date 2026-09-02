import { createHmac, timingSafeEqual } from 'node:crypto';
import os from 'node:os';

const SECRET = process.env.SESSION_SECRET || 'aluxen-authenticity-secret-key-2026';
export const DEFAULT_PRODUCTION_URL = 'https://aluxen.evolucsia.com';

export function getLocalIpAddress(): string | null {
  const nets = os.networkInterfaces();
  for (const name of Object.keys(nets)) {
    if (
      name.startsWith('docker') ||
      name.startsWith('br-') ||
      name.startsWith('veth') ||
      name.startsWith('virbr') ||
      name.startsWith('lxc')
    ) {
      continue;
    }
    for (const net of nets[name] || []) {
      if (net.family === 'IPv4' && !net.internal) {
        return net.address;
      }
    }
  }
  return null;
}

export function resolvePublicBaseUrl(c?: { req: { header: (name: string) => string | undefined; url: string } }): string {
  // 1. Variável de ambiente explícita tem prioridade máxima
  if (process.env.APP_URL) return process.env.APP_URL.replace(/\/$/, '');
  if (process.env.BASE_URL) return process.env.BASE_URL.replace(/\/$/, '');

  // 2. Em produção, o padrão absoluto é aluxen.evolucsia.com
  if (process.env.NODE_ENV === 'production') {
    return DEFAULT_PRODUCTION_URL;
  }

  // 3. Se vier requisição através de domínio público (Proxy reverso / Host real)
  if (c) {
    const forwardedHost = c.req.header('x-forwarded-host');
    const host = forwardedHost || c.req.header('host');
    const proto = c.req.header('x-forwarded-proto') || (host?.includes('evolucsia.com') ? 'https' : 'http');
    if (host && !host.includes('localhost') && !host.includes('127.0.0.1')) {
      return `${proto}://${host}`.replace(/\/$/, '');
    }
  }

  // 4. Em desenvolvimento local: usa o IP da rede Wi-Fi/LAN (ex: http://192.168.10.115:3000)
  // para que qualquer celular na mesma rede consiga escanear o QR Code e abrir na hora!
  const lanIp = getLocalIpAddress();
  const port = process.env.PORT || '3000';
  if (lanIp) {
    return `http://${lanIp}:${port}`;
  }

  return DEFAULT_PRODUCTION_URL;
}

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
  const base = baseUrl || resolvePublicBaseUrl();
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
