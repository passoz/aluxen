import type { Context } from 'hono';
import { getCookie, setCookie, deleteCookie } from 'hono/cookie';

const COOKIE_NAME = '__aluxen_session';
const CLIENT_COOKIE_NAME = '__aluxen_client_session';
const THIRTY_DAYS = 60 * 60 * 24 * 30;

function sessionCookieOptions(): {
  path: string;
  httpOnly: boolean;
  maxAge: number;
  sameSite: 'Lax';
  secure: boolean;
} {
  return {
    path: '/',
    httpOnly: true,
    maxAge: THIRTY_DAYS,
    sameSite: 'Lax',
    secure: process.env.COOKIE_SECURE === 'true',
  };
}

export function getUserId(c: Context): string | null {
  const cookie = getCookie(c, COOKIE_NAME);
  return typeof cookie === 'string' && cookie ? cookie : null;
}

export function requireUserId(c: Context): string | null {
  const userId = getUserId(c);
  return userId;
}

/** Identificador do locatário autenticado na Área do Cliente (cookie independente do painel). */
export function getClientId(c: Context): string | null {
  const cookie = getCookie(c, CLIENT_COOKIE_NAME);
  return typeof cookie === 'string' && cookie ? cookie : null;
}

export function setUserSession(c: Context, userId: string): void {
  setCookie(c, COOKIE_NAME, userId, sessionCookieOptions());
}

export function clearUserSession(c: Context): void {
  deleteCookie(c, COOKIE_NAME, sessionCookieOptions());
}

export function setClientSession(c: Context, tenantId: string): void {
  setCookie(c, CLIENT_COOKIE_NAME, tenantId, sessionCookieOptions());
}

export function clearClientSession(c: Context): void {
  deleteCookie(c, CLIENT_COOKIE_NAME, sessionCookieOptions());
}
