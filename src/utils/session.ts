import type { Context } from 'hono';
import { getCookie, setCookie, deleteCookie } from 'hono/cookie';

const COOKIE_NAME = '__aluxen_session';
const THIRTY_DAYS = 60 * 60 * 24 * 30;

export function getUserId(c: Context): string | null {
  const cookie = getCookie(c, COOKIE_NAME);
  return typeof cookie === 'string' && cookie ? cookie : null;
}

export function requireUserId(c: Context): string | null {
  const userId = getUserId(c);
  return userId;
}

export function setUserSession(c: Context, userId: string): void {
  setCookie(c, COOKIE_NAME, userId, {
    path: '/',
    httpOnly: true,
    maxAge: THIRTY_DAYS,
    sameSite: 'Lax',
    secure: process.env.COOKIE_SECURE === 'true',
  });
}

export function clearUserSession(c: Context): void {
  deleteCookie(c, COOKIE_NAME, {
    path: '/',
    httpOnly: true,
    sameSite: 'Lax',
    secure: process.env.COOKIE_SECURE === 'true',
  });
}
