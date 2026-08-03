import { createCookieSessionStorage, redirect } from '@remix-run/node';

const sessionSecret = process.env.SESSION_SECRET || 'aluxen-persistent-dev-session-secret-key-2026';

const sessionStorage = createCookieSessionStorage({
  cookie: {
    name: '__aluxen_session',
    httpOnly: true,
    maxAge: 60 * 60 * 24 * 30, // 30 days
    path: '/',
    sameSite: 'lax',
    secrets: [sessionSecret],
    secure: process.env.COOKIE_SECURE === 'true',
  },
});

export function getSession(cookieHeader: string | null) {
  return sessionStorage.getSession(cookieHeader);
}

export async function getUserId(request: Request) {
  const session = await getSession(request.headers.get('Cookie'));
  const userId = session.get('userId');
  return typeof userId === 'string' ? userId : null;
}

export async function requireUserId(request: Request) {
  const userId = await getUserId(request);
  if (!userId) {
    throw redirect('/login');
  }
  return userId;
}

export async function createUserSession(userId: string, redirectTo: string) {
  const session = await getSession(null);
  session.set('userId', userId);
  throw redirect(redirectTo, {
    headers: {
      'Set-Cookie': await sessionStorage.commitSession(session),
    },
  });
}

export async function destroyUserSession(request: Request) {
  const session = await getSession(request.headers.get('Cookie'));
  throw redirect('/login', {
    headers: {
      'Set-Cookie': await sessionStorage.destroySession(session),
    },
  });
}
