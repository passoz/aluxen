import { Form, useActionData, useNavigation } from '@remix-run/react';
import { json, redirect } from '@remix-run/node';
import type { ActionFunctionArgs, LoaderFunctionArgs, MetaFunction } from '@remix-run/node';
import { loginUser } from '../utils/auth.server';
import { initializeDatabase } from '../utils/db.server';
import { createUserSession, getUserId } from '../utils/session.server';

const rateLimitMap = new Map<string, { count: number; resetAt: number }>();
const RATE_LIMIT_MAX = Number(process.env.RATE_LIMIT_MAX) || 5;
const RATE_LIMIT_WINDOW = 60_000; // 1 minute

function checkRateLimit(ip: string): boolean {
  const now = Date.now();
  const entry = rateLimitMap.get(ip);
  if (!entry || now > entry.resetAt) {
    rateLimitMap.set(ip, { count: 1, resetAt: now + RATE_LIMIT_WINDOW });
    return true;
  }
  if (entry.count >= RATE_LIMIT_MAX) return false;
  entry.count++;
  return true;
}

export const meta: MetaFunction = () => [{ title: 'Aluxen | Entrar' }];

export async function loader({ request }: LoaderFunctionArgs) {
  await initializeDatabase();
  const userId = await getUserId(request);
  if (userId) {
    throw redirect('/dashboard?tab=tenants');
  }
  return json({});
}

export async function action({ request }: ActionFunctionArgs) {
  await initializeDatabase();

  const ip = request.headers.get('x-forwarded-for') || request.headers.get('x-real-ip') || 'unknown';
  if (!checkRateLimit(ip)) {
    return json({ error: 'Muitas tentativas. Aguarde 1 minuto para tentar novamente.' }, { status: 429 });
  }

  const formData = await request.formData();
  const username = String(formData.get('username') || '').trim();
  const password = String(formData.get('password') || '');

  if (!username || !password) {
    return json({ error: 'Informe usuário e senha.' }, { status: 400 });
  }

  const user = await loginUser(username, password);
  if (!user) {
    return json({ error: 'Credenciais inválidas.' }, { status: 401 });
  }

  return createUserSession(user.id, '/dashboard?tab=tenants');
}

export default function LoginRoute() {
  const actionData = useActionData<typeof action>();
  const navigation = useNavigation();
  const busy = navigation.state === 'submitting';

  return (
    <div className="login-page">
      <div className="card login-card">
        <div className="brand-lockup">
          <div className="logo">
            <div className="logo-icon" aria-hidden="true">
              <svg className="logo-mark" viewBox="0 0 64 64" fill="none" xmlns="http://www.w3.org/2000/svg">
                <path d="M10 31L32 13L54 31" className="logo-stroke" strokeWidth="4.5" strokeLinecap="round" strokeLinejoin="round"/>
                <path d="M17 29V49C17 51.2 18.8 53 21 53H43C45.2 53 47 51.2 47 49V29" className="logo-stroke" strokeWidth="4.5" strokeLinecap="round" strokeLinejoin="round"/>
                <path d="M26 53V39C26 36.8 27.8 35 30 35H34C36.2 35 38 36.8 38 39V53" className="logo-stroke" strokeWidth="4.5" strokeLinecap="round" strokeLinejoin="round"/>
                <circle cx="32" cy="22" r="3.2" className="logo-fill"/>
              </svg>
            </div>
            <div className="logo-text">Alux<span>en</span></div>
          </div>
        </div>

        <Form method="post" className="stack">
          {actionData?.error ? <div className="error">{actionData.error}</div> : null}
          <div className="form-group">
            <label htmlFor="username">Usuário</label>
            <input id="username" name="username" type="text" required />
          </div>
          <div className="form-group">
            <label htmlFor="password">Senha</label>
            <input id="password" name="password" type="password" required />
          </div>
          <button type="submit" className="btn btn-primary" disabled={busy}>
            {busy ? 'Entrando...' : 'Entrar'}
          </button>
        </Form>
      </div>
    </div>
  );
}
