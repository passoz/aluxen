import { Hono } from 'hono';
import type { Handler } from 'hono';
import { serveStatic } from 'hono/bun';
import React from 'react';
import { renderToString } from 'react-dom/server';
import { existsSync, statSync } from 'fs';
import path from 'path';

import { initializeDatabase, getSetting, setSetting, db, sqlite } from './db';
import { tenants, properties, leases, receipts, users } from './db/schema';
import { sql, eq, and, desc, asc, isNull, isNotNull, lt, gt, lte, gte, ne, or, inArray, aliasedTable } from 'drizzle-orm';
import { createId } from './utils/id';
import { createLogger } from './utils/logger';
import { loginUser, changePassword } from './utils/auth';
import { getUserId, requireUserId, setUserSession, clearUserSession, getClientId, setClientSession, clearClientSession } from './utils/session';
import { loginTenant, getClientIdentity, changeTenantPassword } from './utils/tenant-auth';
import { getDaysInMonth, formatCurrency, formatDateBr, formatDateTimeBr, RECEIPT_CATEGORY_LABELS, LEASE_STATUS_LABELS } from './utils/format';
import {
  buildAuditCsv,
  clientIp,
  createAuditContext,
  describeLease,
  describeReceipt,
  diffAuditFields,
  getLeaseSnapshot,
  getPropertySnapshot,
  getReceiptSnapshot,
  getTenantSnapshot,
  listAudit,
  listAuditForExport,
  readAuditQuery,
  recordAudit,
  resolveAuditActor,
  LEASE_AUDIT_FIELDS,
  PROPERTY_AUDIT_FIELDS,
  RECEIPT_AUDIT_FIELDS,
  TENANT_AUDIT_FIELDS,
} from './utils/audit';
import type { AuditPage } from './utils/audit-meta';
import { DEFAULT_AUDIT_QUERY } from './utils/audit-meta';
import type { AppEnv } from './types/http';
import type { SystemInfo } from './types/system';
import { getBuildInfo } from './utils/build-info';
import { isValidEmail } from './utils/validation';
import { createReceiptPdfBuffer } from './utils/pdf';
import { createContractPdfBuffer } from './utils/contract-pdf';
import { buildClientBundle } from './build-client';
import { verifyAuthToken, formatAuthCode, resolvePublicBaseUrl, getLocalIpAddress, generateAuthToken, getValidationUrl } from './utils/auth-token';
import {
  formatWhatsAppNumber,
  buildDirectWhatsAppUrl,
  buildReceiptWhatsAppMessage,
  buildContractWhatsAppMessage,
  sendWhatsAppMessageHeadless,
  checkWhatsAppSessionStatus,
  whatsappLoginManager,
} from './whatsapp/service';
import { resolveProfileDir } from './whatsapp/browser';
import { createSendJob, updateSendJobStage, completeSendJob, failSendJob, getSendJob } from './whatsapp/send-jobs';

import { DashboardPage, type DashboardProps } from './client/dashboard';
import { LoginPage } from './client/login';
import { SettingsPage } from './client/settings';
import { VerificationPage } from './client/verify';
import {
  ClientLoginPage,
  ClientPortalPage,
  ClientPasswordPage,
  type ClientLeaseView,
  type ClientReceiptView,
} from './client/portal';
import type { Tenant, Property, Lease, Receipt, Tab, MutationResponse, MetricsPayload } from './types/dashboard';

// Garantir que o banco de dados e o bundle do cliente estão prontos
await initializeDatabase();
if (!existsSync(path.join(process.cwd(), 'public', 'dist', 'client.js'))) {
  await buildClientBundle();
}

const app = new Hono<AppEnv>();

const httpLog = createLogger('http');
const appLog = createLogger('app');

/** Página de erro mínima (sem React) para falhas de rota ou exceções não tratadas. */
function renderErrorPage(title: string, message: string, requestId: string): string {
  return `<!DOCTYPE html>
<html lang="pt-BR">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Aluxen | ${title}</title>
    <link rel="stylesheet" href="/styles/globals.css" />
    <link rel="icon" href="/favicon.svg" type="image/svg+xml" />
  </head>
  <body>
    <main class="login-page">
      <section class="card login-card">
        <h1>${title}</h1>
        <p class="subtitle">${message}</p>
        <p class="subtitle">Protocolo: <strong>${requestId || '—'}</strong></p>
        <a class="btn btn-primary" href="/dashboard?tab=dashboard">Voltar ao painel</a>
      </section>
    </main>
  </body>
</html>`;
}

// Log estruturado de todas as requisições: método, rota, status, duração, origem e usuário.
app.use('*', async (c, next) => {
  const requestId = crypto.randomUUID().slice(0, 8);
  c.set('requestId', requestId);
  const startedAt = performance.now();

  await next();

  const url = new URL(c.req.url);
  const status = c.res.status;
  const durationMs = Math.round((performance.now() - startedAt) * 10) / 10;
  const lastSegment = url.pathname.slice(url.pathname.lastIndexOf('/') + 1);
  const fields = {
    requestId,
    method: c.req.method,
    path: url.pathname,
    query: url.search ? url.search.slice(1) : undefined,
    status,
    durationMs,
    ip: clientIp(c),
    userId: getUserId(c) || undefined,
  };

  if (lastSegment.includes('.')) {
    httpLog.debug('Arquivo estático servido', fields);
    return;
  }
  if (status >= 500) {
    httpLog.error('Requisição falhou', fields);
    return;
  }
  if (status >= 400) {
    httpLog.warn('Requisição recusada', fields);
    return;
  }
  if (durationMs >= 1000) {
    httpLog.warn('Requisição lenta', fields);
    return;
  }
  httpLog.info('Requisição concluída', fields);
});

// Exceções não tratadas: log com stack + resposta amigável com protocolo de correlação.
app.onError((error, c) => {
  const url = new URL(c.req.url);
  const requestId = c.get('requestId') || '';
  httpLog.error('Erro não tratado', {
    requestId,
    method: c.req.method,
    path: url.pathname,
    error,
  });

  const wantsJson =
    url.pathname.startsWith('/api') ||
    c.req.query('json') === 'true' ||
    (c.req.header('accept') || '').includes('application/json');
  if (wantsJson) {
    return c.json({ error: 'Erro interno do servidor.', requestId }, 500);
  }
  return c.html(renderErrorPage('Erro interno', 'Não foi possível concluir a operação. Tente novamente.', requestId), 500);
});

app.notFound((c) => {
  const url = new URL(c.req.url);
  const requestId = c.get('requestId') || '';
  httpLog.warn('Rota não encontrada', { requestId, method: c.req.method, path: url.pathname });

  if (url.pathname.startsWith('/api') || c.req.query('json') === 'true') {
    return c.json({ error: 'Rota não encontrada.', requestId }, 404);
  }
  return c.html(renderErrorPage('Página não encontrada', 'O endereço acessado não existe no Aluxen.', requestId), 404);
});

// Servir arquivos estáticos (CSS, JS compilado, ícones, imagens)
app.use('/*', serveStatic({ root: './public' }));

// Rate limit em memória para login
const rateLimitMap = new Map<string, { count: number; resetAt: number }>();
const RATE_LIMIT_MAX = Number(process.env.RATE_LIMIT_MAX) || 5;
const RATE_LIMIT_WINDOW = 60_000;

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

// Template HTML idêntico ao root.tsx de main
function renderHtml(
  title: string,
  page: 'dashboard' | 'login' | 'settings' | 'verify' | 'client-login' | 'client-portal' | 'client-password',
  props: Record<string, unknown>,
  content: React.ReactElement
): string {
  const renderedContent = renderToString(content);
  return `<!DOCTYPE html>
<html lang="pt-BR">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <meta name="theme-color" content="#85e6c0" />
    <meta name="apple-mobile-web-app-capable" content="yes" />
    <meta name="apple-mobile-web-app-status-bar-style" content="default" />
    <meta name="apple-mobile-web-app-title" content="Aluxen" />
    <title>${title}</title>
    <link rel="stylesheet" href="/styles/globals.css" />
    <link rel="icon" href="/favicon.svg" type="image/svg+xml" />
    <link rel="manifest" href="/manifest.webmanifest" />
    <link rel="apple-touch-icon" href="/icon-192.png" />
  </head>
  <body>
    <div id="root">${renderedContent}</div>
    <script id="__DATA__" type="application/json">${JSON.stringify({ page, props })}</script>
    <script>
      window.__INITIAL_DATA__ = JSON.parse(document.getElementById('__DATA__').textContent);
    </script>
    <script src="/dist/client.js" defer></script>
  </body>
</html>`;
}

// Healthcheck API
app.get('/api/health', (c) => {
  return c.json({
    status: 'ok',
    runtime: 'bun',
    framework: 'hono',
    database: 'sqlite (bun:sqlite)',
    orm: 'drizzle-orm',
    timestamp: new Date().toISOString(),
  });
});

/** Identificação do build e estado geral do sistema exibidos no modal de Informações. */
async function collectSystemInfo(): Promise<SystemInfo> {
  const build = getBuildInfo();
  const dbPath = sqlite.filename;
  const [tenantRows, propertyRows, leaseRows, receiptRows] = await Promise.all([
    db.select({ value: sql<number>`count(*)` }).from(tenants),
    db.select({ value: sql<number>`count(*)` }).from(properties),
    db.select({ value: sql<number>`count(*)` }).from(leases),
    db.select({ value: sql<number>`count(*)` }).from(receipts),
  ]);

  return {
    app: 'Aluxen',
    version: build.version,
    runtime: `Bun ${Bun.version}`,
    nodeEnv: process.env.NODE_ENV || 'development',
    timezone: process.env.TZ || Intl.DateTimeFormat().resolvedOptions().timeZone,
    startedAt: new Date(Date.now() - process.uptime() * 1000).toISOString(),
    uptimeSeconds: Math.round(process.uptime()),
    database: {
      path: dbPath,
      sizeBytes: existsSync(dbPath) ? statSync(dbPath).size : 0,
    },
    counts: {
      tenants: Number(tenantRows[0]?.value ?? 0),
      properties: Number(propertyRows[0]?.value ?? 0),
      leases: Number(leaseRows[0]?.value ?? 0),
      receipts: Number(receiptRows[0]?.value ?? 0),
    },
    appUrl: (await getSetting('app_url')) || '',
    build,
  };
}

// Informações do sistema e do build em execução (modal de Informações do painel)
app.get('/api/info', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.json({ error: 'Não autorizado.' }, 401);
  return c.json(await collectSystemInfo());
});

// Raiz: Redireciona conforme autenticação
app.get('/', (c) => {
  const userId = getUserId(c);
  if (userId) {
    return c.redirect('/dashboard?tab=dashboard');
  }
  return c.redirect('/login');
});

// Login - GET
app.get('/login', (c) => {
  const userId = getUserId(c);
  if (userId) {
    return c.redirect('/dashboard?tab=dashboard');
  }
  const html = renderHtml('Aluxen | Entrar', 'login', {}, <LoginPage />);
  return c.html(html);
});

// Login - POST
app.post('/login', async (c) => {
  const anonymousAudit = createAuditContext(c, null);
  if (!checkRateLimit(anonymousAudit.ip)) {
    await recordAudit(anonymousAudit, {
      action: 'auth.login-failed',
      entity: 'user',
      summary: 'Tentativa de acesso bloqueada pelo limite de tentativas',
      metadata: { reason: 'Limite de tentativas por minuto excedido' },
    });
    const html = renderHtml(
      'Aluxen | Entrar',
      'login',
      { error: 'Muitas tentativas. Aguarde 1 minuto para tentar novamente.' },
      <LoginPage error="Muitas tentativas. Aguarde 1 minuto para tentar novamente." />
    );
    return c.html(html, 429);
  }

  const body = await c.req.parseBody();
  const username = String(body['username'] || '').trim();
  const password = String(body['password'] || '');

  if (!username || !password) {
    await recordAudit(anonymousAudit, {
      action: 'auth.login-failed',
      entity: 'user',
      entityLabel: username || null,
      summary: 'Tentativa de acesso com usuário ou senha em branco',
      metadata: { reason: 'Campos obrigatórios ausentes' },
    });
    const html = renderHtml('Aluxen | Entrar', 'login', { error: 'Informe usuário e senha.' }, <LoginPage error="Informe usuário e senha." />);
    return c.html(html, 400);
  }

  const user = await loginUser(username, password);
  if (!user) {
    await recordAudit(anonymousAudit, {
      action: 'auth.login-failed',
      entity: 'user',
      entityLabel: username,
      summary: `Tentativa de acesso negada para "${username}"`,
      metadata: { reason: 'Credenciais inválidas' },
    });
    const html = renderHtml('Aluxen | Entrar', 'login', { error: 'Credenciais inválidas.' }, <LoginPage error="Credenciais inválidas." />);
    return c.html(html, 401);
  }

  setUserSession(c, user.id);
  await recordAudit(createAuditContext(c, { id: user.id, name: user.username }), {
    action: 'auth.login',
    entity: 'user',
    entityId: user.id,
    entityLabel: user.username,
    summary: `Acesso ao sistema por "${user.username}"`,
  });
  return c.redirect('/dashboard?tab=dashboard');
});

// Logout
const handleLogout: Handler<AppEnv> = async (c) => {
  const actor = await resolveAuditActor(getUserId(c));
  clearUserSession(c);
  await recordAudit(createAuditContext(c, actor), {
    action: 'auth.logout',
    entity: 'user',
    entityId: actor?.id ?? null,
    entityLabel: actor?.name ?? null,
    summary: actor ? `Saída do sistema de "${actor.name}"` : 'Saída do sistema',
  });
  return c.redirect('/login');
};

app.post('/logout', handleLogout);
app.get('/logout', handleLogout);

// ---------------------------------------------------------------------------
// Área do Cliente (locatário) — autenticação própria por CPF, separada do painel
// ---------------------------------------------------------------------------

/**
 * Contratos e recibos quitados que o locatário autenticado pode consultar.
 * Somente leitura: inclui os recibos com pagamento registrado (integral ou parcial).
 */
async function loadClientArea(tenantId: string): Promise<{ leases: ClientLeaseView[]; receipts: ClientReceiptView[] }> {
  const leaseProperty = aliasedTable(properties, 'client_lp');
  const receiptProperty = aliasedTable(properties, 'client_rp');

  const [leaseRows, receiptRows] = await Promise.all([
    db
      .select({
        id: leases.id,
        start_date: leases.startDate,
        end_date: leases.endDate,
        due_day: leases.dueDay,
        rent_amount: leases.rentAmount,
        status: leases.status,
        property_name: leaseProperty.name,
        property_address: leaseProperty.address,
        property_city: leaseProperty.city,
        property_state: leaseProperty.state,
      })
      .from(leases)
      .innerJoin(leaseProperty, eq(leases.propertyId, leaseProperty.id))
      .where(eq(leases.tenantId, tenantId))
      .orderBy(desc(leases.createdAt)),
    db
      .select({
        id: receipts.id,
        category: receipts.category,
        description: receipts.description,
        due_date: receipts.dueDate,
        paid_at: receipts.paidAt,
        amount: receipts.amount,
        paid_amount: receipts.paidAmount,
        property_name: sql<string>`coalesce(${leaseProperty.name}, ${receiptProperty.name}, '')`,
      })
      .from(receipts)
      .leftJoin(leases, eq(receipts.leaseId, leases.id))
      .leftJoin(leaseProperty, eq(leases.propertyId, leaseProperty.id))
      .leftJoin(receiptProperty, eq(receipts.propertyId, receiptProperty.id))
      .where(and(isNotNull(receipts.paidAt), or(eq(leases.tenantId, tenantId), eq(receipts.tenantId, tenantId))))
      .orderBy(desc(receipts.paidAt)),
  ]);

  return {
    leases: leaseRows.map((lease) => ({
      id: lease.id,
      propertyName: lease.property_name,
      propertyAddress: [lease.property_address, lease.property_city, lease.property_state].filter(Boolean).join(', '),
      period: `${formatDateBr(lease.start_date)} a ${formatDateBr(lease.end_date)}`,
      dueDay: lease.due_day,
      rentAmount: formatCurrency(lease.rent_amount),
      status: lease.status,
      statusLabel: LEASE_STATUS_LABELS[lease.status] || lease.status,
      pdfUrl: `/contract-pdf?id=${lease.id}`,
      validationUrl: `/validar/contrato/${lease.id}?token=${generateAuthToken('lease', lease.id)}`,
    })),
    receipts: receiptRows.map((receipt) => {
      const paidAmount = receipt.paid_amount != null ? Number(receipt.paid_amount) : receipt.amount;
      return {
        id: receipt.id,
        reference:
          receipt.category === 'extra'
            ? receipt.description || 'Cobrança avulsa'
            : `${RECEIPT_CATEGORY_LABELS[receipt.category] || 'Aluguel'} ${formatDateBr(receipt.due_date).slice(3)}`,
        propertyName: receipt.property_name,
        dueDate: formatDateBr(receipt.due_date),
        paidAt: formatDateTimeBr(receipt.paid_at),
        amountPaid: formatCurrency(paidAmount),
        isPartial: paidAmount > 0 && paidAmount < receipt.amount,
        pdfUrl: `/pdf?id=${receipt.id}`,
        validationUrl: `/validar/recibo/${receipt.id}?token=${generateAuthToken('receipt', receipt.id)}`,
      };
    }),
  };
}

// Login da Área do Cliente - GET
app.get('/portal/login', (c) => {
  if (getClientId(c)) {
    return c.redirect('/portal');
  }
  const html = renderHtml('Aluxen | Área do Cliente', 'client-login', {}, <ClientLoginPage />);
  return c.html(html);
});

// Login da Área do Cliente - POST
app.post('/portal/login', async (c) => {
  if (!checkRateLimit(clientIp(c))) {
    await recordAudit(createAuditContext(c, null, 'client'), {
      action: 'auth.client-login-failed',
      entity: 'tenant',
      summary: 'Acesso à Área do Cliente bloqueado pelo limite de tentativas',
      metadata: { reason: 'Limite de tentativas por minuto excedido' },
    });
    const message = 'Muitas tentativas. Aguarde 1 minuto para tentar novamente.';
    const html = renderHtml('Aluxen | Área do Cliente', 'client-login', { error: message }, <ClientLoginPage error={message} />);
    return c.html(html, 429);
  }

  const body = await c.req.parseBody();
  const cpf = String(body['cpf'] || '').trim();
  const password = String(body['password'] || '');

  if (!cpf || !password) {
    const message = 'Informe CPF e senha.';
    const html = renderHtml('Aluxen | Área do Cliente', 'client-login', { error: message, cpf }, <ClientLoginPage error={message} cpf={cpf} />);
    return c.html(html, 400);
  }

  const identity = await loginTenant(cpf, password);
  if (!identity) {
    await recordAudit(createAuditContext(c, null, 'client'), {
      action: 'auth.client-login-failed',
      entity: 'tenant',
      entityLabel: cpf,
      summary: `Tentativa de acesso negada na Área do Cliente para o CPF informado`,
      metadata: { reason: 'Credenciais inválidas' },
    });
    const message = 'CPF ou senha inválidos.';
    const html = renderHtml('Aluxen | Área do Cliente', 'client-login', { error: message, cpf }, <ClientLoginPage error={message} cpf={cpf} />);
    return c.html(html, 401);
  }

  setClientSession(c, identity.id);
  await recordAudit(createAuditContext(c, { id: identity.id, name: identity.name }, 'client'), {
    action: 'auth.client-login',
    entity: 'tenant',
    entityId: identity.id,
    entityLabel: identity.name,
    summary: `Acesso à Área do Cliente por "${identity.name}"`,
  });
  return c.redirect(identity.mustChangePassword ? '/portal/password' : '/portal');
});

// Logout da Área do Cliente
const handleClientLogout: Handler<AppEnv> = async (c) => {
  const tenantId = getClientId(c);
  const identity = tenantId ? await getClientIdentity(tenantId) : null;
  clearClientSession(c);
  await recordAudit(createAuditContext(c, identity ? { id: identity.id, name: identity.name } : null, 'client'), {
    action: 'auth.client-logout',
    entity: 'tenant',
    entityId: identity?.id ?? null,
    entityLabel: identity?.name ?? null,
    summary: identity ? `Saída da Área do Cliente de "${identity.name}"` : 'Saída da Área do Cliente',
  });
  return c.redirect('/portal/login');
};

app.post('/portal/logout', handleClientLogout);
app.get('/portal/logout', handleClientLogout);

// Área do Cliente - GET (contratos e recibos quitados, somente leitura)
app.get('/portal', async (c) => {
  const tenantId = getClientId(c);
  if (!tenantId) {
    return c.redirect('/portal/login');
  }
  const identity = await getClientIdentity(tenantId);
  if (!identity) {
    clearClientSession(c);
    return c.redirect('/portal/login');
  }
  // Senha padrão (CPF) ainda vigente: a troca é obrigatória antes de qualquer leitura.
  if (identity.mustChangePassword) {
    return c.redirect('/portal/password');
  }

  const area = await loadClientArea(identity.id);
  const tenantRows = await db.select({ cpf: tenants.cpf }).from(tenants).where(eq(tenants.id, identity.id)).limit(1);

  const props = {
    tenantName: identity.name,
    tenantCpf: tenantRows[0]?.cpf || '',
    leases: area.leases,
    receipts: area.receipts,
    message: c.req.query('ok') === 'senha' ? 'Senha alterada com sucesso.' : undefined,
  };
  const html = renderHtml('Aluxen | Área do Cliente', 'client-portal', props, <ClientPortalPage {...props} />);
  return c.html(html);
});

// Área do Cliente - Troca de senha (GET)
app.get('/portal/password', async (c) => {
  const tenantId = getClientId(c);
  if (!tenantId) {
    return c.redirect('/portal/login');
  }
  const identity = await getClientIdentity(tenantId);
  if (!identity) {
    clearClientSession(c);
    return c.redirect('/portal/login');
  }

  const props = { firstAccess: identity.mustChangePassword };
  const html = renderHtml('Aluxen | Trocar senha', 'client-password', props, <ClientPasswordPage {...props} />);
  return c.html(html);
});

// Área do Cliente - Troca de senha (POST)
app.post('/portal/password', async (c) => {
  const tenantId = getClientId(c);
  if (!tenantId) {
    return c.redirect('/portal/login');
  }
  const identity = await getClientIdentity(tenantId);
  if (!identity) {
    clearClientSession(c);
    return c.redirect('/portal/login');
  }

  const body = await c.req.parseBody();
  const currentPassword = String(body['current_password'] || '');
  const newPassword = String(body['new_password'] || '');
  const confirmation = String(body['confirm_password'] || '');

  const renderError = (message: string) => {
    const props = { firstAccess: identity.mustChangePassword, error: message };
    return c.html(renderHtml('Aluxen | Trocar senha', 'client-password', props, <ClientPasswordPage {...props} />), 400);
  };

  if (newPassword !== confirmation) {
    return renderError('A confirmação não corresponde à nova senha.');
  }

  try {
    await changeTenantPassword(identity.id, currentPassword, newPassword);
  } catch (error) {
    return renderError(error instanceof Error ? error.message : 'Não foi possível alterar a senha.');
  }

  await recordAudit(createAuditContext(c, { id: identity.id, name: identity.name }, 'client'), {
    action: 'auth.client-password',
    entity: 'tenant',
    entityId: identity.id,
    entityLabel: identity.name,
    summary: `Senha da Área do Cliente alterada por "${identity.name}"`,
  });
  return c.redirect('/portal?ok=password');
});

// Dashboard - GET
app.get('/dashboard', async (c) => {
  const userId = requireUserId(c);
  if (!userId) {
    return c.redirect('/login');
  }
  const urlTab = c.req.query('tab') as Tab;
  const tab: Tab = ['dashboard', 'tenants', 'properties', 'leases', 'receipts', 'history'].includes(urlTab) ? (urlTab as Tab) : 'dashboard';

  const tAlias = aliasedTable(tenants, 't');
  const pAlias = aliasedTable(properties, 'p');
  const rtAlias = aliasedTable(tenants, 'rt');
  const rpAlias = aliasedTable(properties, 'rp');


  const [userRow, tenantsList, propertiesList, leasesList, receiptsList, activeTenantIdRows, metrics] = await Promise.all([
    db.select({ username: users.username }).from(users).where(eq(users.id, userId)).limit(1).then((rows) => rows[0]),
    db.select({
      id: tenants.id,
      name: tenants.name,
      email: tenants.email,
      phone: tenants.phone,
      cpf: tenants.cpf,
    }).from(tenants).orderBy(desc(tenants.createdAt), asc(tenants.name)),
    db.select({
      id: properties.id,
      name: properties.name,
      address: properties.address,
      city: properties.city,
      state: properties.state,
      zip: properties.zip,
    }).from(properties).orderBy(desc(properties.createdAt), asc(properties.name)),
    db.select({
      id: leases.id,
      tenant_id: leases.tenantId,
      property_id: leases.propertyId,
      start_date: leases.startDate,
      end_date: leases.endDate,
      due_day: leases.dueDay,
      rent_amount: leases.rentAmount,
      status: leases.status,
      tenant_name: tAlias.name,
      property_name: pAlias.name,
    })
      .from(leases)
      .innerJoin(tAlias, eq(leases.tenantId, tAlias.id))
      .innerJoin(pAlias, eq(leases.propertyId, pAlias.id))
      .orderBy(asc(leases.status), desc(leases.createdAt)),
    db.select({
      id: receipts.id,
      lease_id: receipts.leaseId,
      tenant_id: receipts.tenantId,
      property_id: receipts.propertyId,
      category: receipts.category,
      description: receipts.description,
      due_date: receipts.dueDate,
      paid_at: receipts.paidAt,
      amount: receipts.amount,
      paid_amount: receipts.paidAmount,
      pdf_path: receipts.pdfPath,
      generated_key: receipts.generatedKey,
      tenant_name: sql<string>`coalesce(${tAlias.name}, ${rtAlias.name}, '')`,
      property_name: sql<string>`coalesce(${pAlias.name}, ${rpAlias.name}, '')`,
    })
      .from(receipts)
      .leftJoin(leases, eq(receipts.leaseId, leases.id))
      .leftJoin(tAlias, eq(leases.tenantId, tAlias.id))
      .leftJoin(pAlias, eq(leases.propertyId, pAlias.id))
      .leftJoin(rtAlias, eq(receipts.tenantId, rtAlias.id))
      .leftJoin(rpAlias, eq(receipts.propertyId, rpAlias.id))
      .orderBy(desc(receipts.createdAt)),
    db.selectDistinct({ tenant_id: leases.tenantId }).from(leases).where(eq(leases.status, 'active')),
    computeMetrics(),
  ]);

  const history: AuditPage | undefined = tab === 'history' ? await listAudit(DEFAULT_AUDIT_QUERY) : undefined;

  const dashboardData: DashboardProps = {
    tab,
    user: userRow?.username || 'admin',
    tenants: tenantsList as unknown as Tenant[],
    properties: propertiesList as unknown as Property[],
    leases: leasesList as unknown as Lease[],
    receipts: receiptsList as unknown as Receipt[],
    activeTenantIds: activeTenantIdRows.map((r) => r.tenant_id).filter((id): id is string => !!id),
    metrics,
    history,
  };

  if (c.req.query('json') === 'true') {
    return c.json(dashboardData);
  }

  const html = renderHtml(
    'Aluxen | Painel',
    'dashboard',
    dashboardData as unknown as Record<string, unknown>,
    <DashboardPage {...dashboardData} />
  );
  return c.html(html);
});

// Dashboard - POST (Mutation handler de todas as ações de main)
app.post('/dashboard', async (c) => {
  const userId = requireUserId(c);
  if (!userId) {
    return c.json<MutationResponse>({ error: 'Sessão expirada.' }, 401);
  }

  const formData = await c.req.parseBody();
  const intent = String(formData['intent'] || '');
  const audit = createAuditContext(c, await resolveAuditActor(userId));

  try {
    switch (intent) {
      case 'tenant.create': {
        const id = String(formData['id'] || createId());
        const name = String(formData['name'] || '').trim();
        const email = String(formData['email'] || '').trim();
        const phone = String(formData['phone'] || '').trim();
        const cpf = String(formData['cpf'] || '').trim();
        if (!name || !email) throw new Error('Nome e email são obrigatórios.');
        if (!isValidEmail(email)) throw new Error('Informe um e-mail válido.');
        await db.insert(tenants).values({ id, name, email, phone: phone || null, cpf: cpf || null });
        await recordAudit(audit, {
          action: 'tenant.create',
          entity: 'tenant',
          entityId: id,
          entityLabel: name,
          summary: `Inquilino "${name}" cadastrado`,
          changes: diffAuditFields(null, { name, email, phone: phone || null, cpf: cpf || null }, TENANT_AUDIT_FIELDS),
        });
        break;
      }
      case 'tenant.update': {
        const id = String(formData['id'] || '');
        const name = String(formData['name'] || '').trim();
        const email = String(formData['email'] || '').trim();
        const phone = String(formData['phone'] || '').trim();
        const cpf = String(formData['cpf'] || '').trim();
        if (!id || !name || !email) throw new Error('Nome e email são obrigatórios.');
        if (!isValidEmail(email)) throw new Error('Informe um e-mail válido.');
        const before = await getTenantSnapshot(id);
        await db.update(tenants).set({ name, email, phone: phone || null, cpf: cpf || null, updatedAt: sql`(datetime('now'))` }).where(eq(tenants.id, id));
        const after = await getTenantSnapshot(id);
        await recordAudit(audit, {
          action: 'tenant.update',
          entity: 'tenant',
          entityId: id,
          entityLabel: after?.name ?? name,
          summary: `Inquilino "${after?.name ?? name}" alterado`,
          changes: diffAuditFields(before, after, TENANT_AUDIT_FIELDS),
        });
        break;
      }
      case 'tenant.delete': {
        const tenantId = String(formData['id']);
        const activeLease = await db.select({ id: leases.id }).from(leases).where(and(eq(leases.tenantId, tenantId), eq(leases.status, 'active'))).limit(1);
        if (activeLease.length > 0) throw new Error('Não é possível excluir inquilino com contrato ativo. Encerre o contrato primeiro.');
        const before = await getTenantSnapshot(tenantId);
        await db.delete(tenants).where(eq(tenants.id, tenantId));
        await recordAudit(audit, {
          action: 'tenant.delete',
          entity: 'tenant',
          entityId: tenantId,
          entityLabel: before?.name ?? null,
          summary: `Inquilino "${before?.name ?? tenantId}" excluído`,
          changes: diffAuditFields(before, null, TENANT_AUDIT_FIELDS),
        });
        break;
      }
      case 'property.create': {
        const id = String(formData['id'] || createId());
        const name = String(formData['name'] || '').trim();
        const address = String(formData['address'] || '').trim();
        const city = String(formData['city'] || '').trim();
        const state = String(formData['state'] || '').trim();
        const zip = String(formData['zip'] || '').trim();
        if (!name || !address || !city || !state) throw new Error('Preencha nome, endereço, cidade e estado.');
        await db.insert(properties).values({ id, name, address, city, state, zip: zip || null });
        await recordAudit(audit, {
          action: 'property.create',
          entity: 'property',
          entityId: id,
          entityLabel: name,
          summary: `Imóvel "${name}" cadastrado`,
          changes: diffAuditFields(null, { name, address, city, state, zip: zip || null }, PROPERTY_AUDIT_FIELDS),
        });
        break;
      }
      case 'property.update': {
        const id = String(formData['id'] || '');
        const name = String(formData['name'] || '').trim();
        const address = String(formData['address'] || '').trim();
        const city = String(formData['city'] || '').trim();
        const state = String(formData['state'] || '').trim();
        const zip = String(formData['zip'] || '').trim();
        if (!id || !name || !address || !city || !state) throw new Error('Preencha nome, endereço, cidade e estado.');
        const before = await getPropertySnapshot(id);
        await db.update(properties).set({ name, address, city, state, zip: zip || null, updatedAt: sql`(datetime('now'))` }).where(eq(properties.id, id));
        const after = await getPropertySnapshot(id);
        await recordAudit(audit, {
          action: 'property.update',
          entity: 'property',
          entityId: id,
          entityLabel: after?.name ?? name,
          summary: `Imóvel "${after?.name ?? name}" alterado`,
          changes: diffAuditFields(before, after, PROPERTY_AUDIT_FIELDS),
        });
        break;
      }
      case 'property.delete': {
        const id = String(formData['id']);
        const before = await getPropertySnapshot(id);
        await db.delete(properties).where(eq(properties.id, id));
        await recordAudit(audit, {
          action: 'property.delete',
          entity: 'property',
          entityId: id,
          entityLabel: before?.name ?? null,
          summary: `Imóvel "${before?.name ?? id}" excluído`,
          changes: diffAuditFields(before, null, PROPERTY_AUDIT_FIELDS),
        });
        break;
      }
      case 'lease.create': {
        const id = String(formData['id'] || createId());
        const tenantId = String(formData['tenant_id'] || '');
        const propertyId = String(formData['property_id'] || '');
        const startDate = String(formData['start_date'] || '');
        const endDate = String(formData['end_date'] || '');
        const dueDay = Number(formData['due_day']);
        const rentAmount = Number(formData['rent_amount']);
        const status = String(formData['status'] || 'active');
        if (!['active', 'suspended', 'season'].includes(status)) {
          throw new Error('Status de contrato inválido.');
        }
        if (!tenantId || !propertyId || !startDate || !endDate || !Number.isInteger(dueDay) || dueDay < 1 || dueDay > 31 || !Number.isFinite(rentAmount)) {
          throw new Error('Preencha todos os campos do contrato.');
        }
        // Contratos de locação comum ou temporada não podem sobrepor outro contrato vigente
        // no mesmo imóvel. Contrato suspenso (moradia própria / permuta) não bloqueia.
        if (status === 'active' || status === 'season') {
          const overlap = await db.select({ id: leases.id }).from(leases).where(
            and(
              eq(leases.propertyId, propertyId),
              inArray(leases.status, ['active', 'season']),
              lte(leases.startDate, endDate),
              gte(leases.endDate, startDate)
            )
          ).limit(1);
          if (overlap.length > 0) {
            throw new Error(
              status === 'season'
                ? 'Este imóvel já possui contrato vigente no período. Temporada só pode ser criada em imóvel sem contrato ativo ou com contrato suspenso.'
                : 'Este imóvel já possui contrato vigente no período.'
            );
          }
        }
        await db.insert(leases).values({ id, tenantId, propertyId, startDate, endDate, dueDay, rentAmount, status });
        const created = await getLeaseSnapshot(id);
        const createdLabel = describeLease(created);
        await recordAudit(audit, {
          action: 'lease.create',
          entity: 'lease',
          entityId: id,
          entityLabel: createdLabel,
          summary: `Contrato de ${createdLabel} criado`,
          changes: diffAuditFields(null, created, LEASE_AUDIT_FIELDS),
        });
        break;
      }
      case 'lease.update': {
        const id = String(formData['id'] || '');
        const tenantId = String(formData['tenant_id'] || '');
        const propertyId = String(formData['property_id'] || '');
        const startDate = String(formData['start_date'] || '');
        const endDate = String(formData['end_date'] || '');
        const dueDay = Number(formData['due_day']);
        const rentAmount = Number(formData['rent_amount']);
        const status = String(formData['status'] || '');
        if (!id || !tenantId || !propertyId || !startDate || !endDate || !Number.isInteger(dueDay) || dueDay < 1 || dueDay > 31 || !Number.isFinite(rentAmount)) {
          throw new Error('Preencha todos os campos do contrato.');
        }
        if (status && !['active', 'suspended', 'season', 'terminated'].includes(status)) {
          throw new Error('Status de contrato inválido.');
        }
        if ((status === 'active' || status === 'season')) {
          const overlap = await db.select({ id: leases.id }).from(leases).where(
            and(
              eq(leases.propertyId, propertyId),
              ne(leases.id, id),
              inArray(leases.status, ['active', 'season']),
              lte(leases.startDate, endDate),
              gte(leases.endDate, startDate)
            )
          ).limit(1);
          if (overlap.length > 0) {
            throw new Error(
              status === 'season'
                ? 'Este imóvel já possui contrato vigente no período. Temporada só pode ser criada em imóvel sem contrato ativo ou com contrato suspenso.'
                : 'Este imóvel já possui contrato vigente no período.'
            );
          }
        }
        const updateValues: Record<string, unknown> = { tenantId, propertyId, startDate, endDate, dueDay, rentAmount, updatedAt: sql`(datetime('now'))` };
        if (status) updateValues.status = status;
        const beforeLease = await getLeaseSnapshot(id);
        await db.update(leases).set(updateValues).where(eq(leases.id, id));
        const afterLease = await getLeaseSnapshot(id);
        const afterLabel = describeLease(afterLease);
        await recordAudit(audit, {
          action: 'lease.update',
          entity: 'lease',
          entityId: id,
          entityLabel: afterLabel,
          summary: `Contrato de ${afterLabel} alterado`,
          changes: diffAuditFields(beforeLease, afterLease, LEASE_AUDIT_FIELDS),
        });
        break;
      }
      case 'lease.terminate':
      case 'lease.suspend':
      case 'lease.reactivate': {
        const leaseId = String(formData['id']);
        const nextStatus = intent === 'lease.terminate' ? 'terminated' : intent === 'lease.suspend' ? 'suspended' : 'active';
        const actionLabel = intent === 'lease.terminate' ? 'encerrado' : intent === 'lease.suspend' ? 'suspenso' : 'reativado';
        const beforeLease = await getLeaseSnapshot(leaseId);
        await db.update(leases).set({ status: nextStatus, updatedAt: sql`(datetime('now'))` }).where(eq(leases.id, leaseId));
        const afterLease = await getLeaseSnapshot(leaseId);
        const leaseLabel = describeLease(afterLease);
        await recordAudit(audit, {
          action: intent,
          entity: 'lease',
          entityId: leaseId,
          entityLabel: leaseLabel,
          summary: `Contrato de ${leaseLabel} ${actionLabel}`,
          changes: diffAuditFields(beforeLease, afterLease, LEASE_AUDIT_FIELDS),
        });
        break;
      }
      case 'lease.delete': {
        const leaseId = String(formData['id']);
        const beforeLease = await getLeaseSnapshot(leaseId);
        const deletedLabel = describeLease(beforeLease);
        await db.delete(leases).where(eq(leases.id, leaseId));
        await recordAudit(audit, {
          action: 'lease.delete',
          entity: 'lease',
          entityId: leaseId,
          entityLabel: deletedLabel,
          summary: `Contrato de ${deletedLabel} excluído`,
          changes: diffAuditFields(beforeLease, null, LEASE_AUDIT_FIELDS),
        });
        break;
      }
      case 'receipt.create':
      case 'receipt.update': {
        const id = String(formData['id'] || createId());
        const leaseId = formData['lease_id'] ? String(formData['lease_id']) : null;
        let tenantId = formData['tenant_id'] ? String(formData['tenant_id']) : null;
        let propertyId = formData['property_id'] ? String(formData['property_id']) : null;
        const category = String(formData['category'] || (leaseId ? 'rent' : 'extra'));
        const description = formData['description'] ? String(formData['description']).trim() : null;
        const dueDate = String(formData['due_date'] || '');
        let paidAt = formData['paid_at'] ? String(formData['paid_at']).trim() : null;
        const amount = Number(formData['amount']);
        const rawPaidAmount = formData['paid_amount'] != null && String(formData['paid_amount']).trim() !== ''
          ? Number(formData['paid_amount'])
          : null;

        if (!dueDate || !Number.isFinite(amount)) throw new Error('Preencha vencimento e valor.');
        if (category === 'rent' && !leaseId) throw new Error('Selecione um contrato para recebimento de aluguel.');
        if (category === 'extra' && !description) throw new Error('Informe a descrição para cobrança avulsa.');

        if (leaseId) {
          const leaseRows = await db.select({ tenantId: leases.tenantId, propertyId: leases.propertyId }).from(leases).where(eq(leases.id, leaseId)).limit(1);
          const lease = leaseRows[0];
          if (lease) {
            tenantId = lease.tenantId;
            propertyId = lease.propertyId;
          }
        }

        let paidAmount = rawPaidAmount;
        if (paidAmount != null && paidAmount > 0 && !paidAt) {
          const now = new Date();
          paidAt = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}T${String(
            now.getHours()
          ).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`;
        } else if (paidAt && (paidAmount == null || paidAmount === 0)) {
          paidAmount = amount;
        }

        if (intent === 'receipt.create') {
          await db.insert(receipts).values({
            id,
            leaseId,
            tenantId,
            propertyId,
            category,
            description,
            dueDate,
            paidAt: paidAt || null,
            amount,
            paidAmount: paidAmount != null ? paidAmount : null,
            pdfPath: null,
          });
          const created = await getReceiptSnapshot(id);
          const createdLabel = describeReceipt(created);
          await recordAudit(audit, {
            action: 'receipt.create',
            entity: 'receipt',
            entityId: id,
            entityLabel: createdLabel,
            summary: `Pagamento lançado para ${createdLabel}`,
            changes: diffAuditFields(null, created, RECEIPT_AUDIT_FIELDS),
          });
        } else {
          const before = await getReceiptSnapshot(id);
          await db.update(receipts).set({
            leaseId,
            tenantId,
            propertyId,
            category,
            description,
            dueDate,
            paidAt: paidAt || null,
            amount,
            paidAmount: paidAmount != null ? paidAmount : null,
            pdfPath: null,
            updatedAt: sql`(datetime('now'))`,
          }).where(eq(receipts.id, id));
          const after = await getReceiptSnapshot(id);
          const afterLabel = describeReceipt(after);
          await recordAudit(audit, {
            action: 'receipt.update',
            entity: 'receipt',
            entityId: id,
            entityLabel: afterLabel,
            summary: `Pagamento de ${afterLabel} alterado`,
            changes: diffAuditFields(before, after, RECEIPT_AUDIT_FIELDS),
          });
        }
        break;
      }
      case 'receipt.pay': {
        const id = String(formData['id']);
        const receiptRows = await db.select({ amount: receipts.amount, paidAmount: receipts.paidAmount }).from(receipts).where(eq(receipts.id, id)).limit(1);
        const receipt = receiptRows[0];
        if (!receipt) throw new Error('Recibo não encontrado.');

        const payAmount = formData['pay_amount'] != null && String(formData['pay_amount']).trim() !== ''
          ? Number(formData['pay_amount'])
          : receipt.amount;
        let paidAt = formData['paid_at'] ? String(formData['paid_at']).trim() : null;
        if (!paidAt) {
          const now = new Date();
          paidAt = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}T${String(
            now.getHours()
          ).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`;
        }
        const beforePay = await getReceiptSnapshot(id);
        await db.update(receipts).set({ paidAt, paidAmount: payAmount, updatedAt: sql`(datetime('now'))` }).where(eq(receipts.id, id));
        const afterPay = await getReceiptSnapshot(id);
        const paidLabel = describeReceipt(afterPay);
        await recordAudit(audit, {
          action: 'receipt.pay',
          entity: 'receipt',
          entityId: id,
          entityLabel: paidLabel,
          summary: `Baixa de ${paidLabel} registrada`,
          changes: diffAuditFields(beforePay, afterPay, RECEIPT_AUDIT_FIELDS),
        });
        break;
      }
      case 'receipt.generate-monthly': {
        const now = new Date();
        const year = now.getFullYear();
        const month = now.getMonth() + 1;
        const monthKey = `${year}-${String(month).padStart(2, '0')}`;
        const monthStart = `${monthKey}-01`;
        const monthEnd = `${monthKey}-${String(getDaysInMonth(year, month)).padStart(2, '0')}`;
        const activeLeasesRows = await db.select({
          id: leases.id,
          tenantId: leases.tenantId,
          propertyId: leases.propertyId,
          dueDay: leases.dueDay,
          rentAmount: leases.rentAmount,
        })
          .from(leases)
          .where(and(eq(leases.status, 'active'), lte(leases.startDate, monthEnd), gte(leases.endDate, monthStart)));

        const inserted = [];
        for (const lease of activeLeasesRows) {
          const dueDay = Math.min(Number(lease.dueDay) || 1, getDaysInMonth(year, month));
          const dueDate = `${monthKey}-${String(dueDay).padStart(2, '0')}`;
          const rows = await db.insert(receipts).values({
            id: createId(),
            leaseId: lease.id,
            tenantId: lease.tenantId,
            propertyId: lease.propertyId,
            category: 'rent',
            description: null,
            dueDate,
            paidAt: null,
            amount: lease.rentAmount,
            pdfPath: null,
            generatedKey: `${lease.id}:${monthKey}`,
          }).onConflictDoNothing({ target: receipts.generatedKey }).returning({ id: receipts.id });
          inserted.push(...rows);
        }
        await recordAudit(audit, {
          action: 'receipt.generate-monthly',
          entity: 'receipt',
          entityId: monthKey,
          entityLabel: `Competência ${monthKey.slice(5)}/${monthKey.slice(0, 4)}`,
          summary: `Geração automática de ${inserted.length} pagamento(s) para ${monthKey.slice(5)}/${monthKey.slice(0, 4)}`,
          metadata: { month: monthKey, count: inserted.length },
        });
        break;
      }
      case 'receipt.delete': {
        const id = String(formData['id']);
        const beforeDelete = await getReceiptSnapshot(id);
        const deletedLabel = describeReceipt(beforeDelete);
        await db.delete(receipts).where(eq(receipts.id, id));
        await recordAudit(audit, {
          action: 'receipt.delete',
          entity: 'receipt',
          entityId: id,
          entityLabel: deletedLabel,
          summary: `Pagamento de ${deletedLabel} excluído`,
          changes: diffAuditFields(beforeDelete, null, RECEIPT_AUDIT_FIELDS),
        });
        break;
      }
      case 'receipt.batch-pay': {
        const ids = (formData['ids'] || '').toString().split(',').filter(Boolean);
        if (!ids.length) throw new Error('Nenhum recibo selecionado.');
        const now = new Date();
        const paidAt = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}T${String(
          now.getHours()
          ).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`;
        const beforeBatch = await Promise.all(ids.map((receiptId) => getReceiptSnapshot(receiptId)));
        for (const receiptId of ids) {
          await db.update(receipts).set({ paidAt, paidAmount: sql`${receipts.amount}`, updatedAt: sql`(datetime('now'))` }).where(eq(receipts.id, receiptId));
        }
        const afterBatch = await Promise.all(ids.map((receiptId) => getReceiptSnapshot(receiptId)));
        for (const [index, receiptId] of ids.entries()) {
          const after = afterBatch[index] ?? null;
          const batchLabel = describeReceipt(after);
          await recordAudit(audit, {
            action: 'receipt.batch-pay',
            entity: 'receipt',
            entityId: receiptId,
            entityLabel: batchLabel,
            summary: `Baixa em lote de ${batchLabel} registrada`,
            changes: diffAuditFields(beforeBatch[index] ?? null, after, RECEIPT_AUDIT_FIELDS),
            metadata: { batch: ids.length },
          });
        }
        break;
      }
      case 'receipt.batch-delete': {
        const ids = (formData['ids'] || '').toString().split(',').filter(Boolean);
        if (!ids.length) throw new Error('Nenhum recibo selecionado.');
        const beforeBatch = await Promise.all(ids.map((receiptId) => getReceiptSnapshot(receiptId)));
        await db.delete(receipts).where(inArray(receipts.id, ids));
        for (const [index, receiptId] of ids.entries()) {
          const before = beforeBatch[index] ?? null;
          const batchLabel = describeReceipt(before);
          await recordAudit(audit, {
            action: 'receipt.batch-delete',
            entity: 'receipt',
            entityId: receiptId,
            entityLabel: batchLabel,
            summary: `Pagamento de ${batchLabel} excluído em lote`,
            changes: diffAuditFields(before, null, RECEIPT_AUDIT_FIELDS),
            metadata: { batch: ids.length },
          });
        }
        break;
      }
      default:
        throw new Error('Ação inválida.');
    }

  } catch (error) {
    return c.json<MutationResponse>({ error: error instanceof Error ? error.message : 'Não foi possível salvar.' }, 400);
  }

  return c.json<MutationResponse>({ ok: true });
});

// Configurações - GET
app.get('/settings', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.redirect('/login');

  const userRows = await db.select({ username: users.username }).from(users).where(eq(users.id, userId)).limit(1);
  const user = userRows[0];
  const lanIp = getLocalIpAddress();
  const currentPort = process.env.PORT || '3000';
  const settingsProps = {
    landlordName: (await getSetting('landlord_name')) || 'Locador',
    landlordCpf: (await getSetting('landlord_cpf')) || '',
    landlordEmail: (await getSetting('landlord_email')) || '',
    appUrl: (await getSetting('app_url')) || '',
    detectedLanUrl: lanIp ? `http://${lanIp}:${currentPort}` : 'http://localhost:3000',
    username: user?.username || '',
  };

  const html = renderHtml('Aluxen | Configurações', 'settings', settingsProps, <SettingsPage {...settingsProps} />);
  return c.html(html);
});

// Configurações - POST
app.post('/settings', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.redirect('/login');

  const formData = await c.req.parseBody();
  const intent = String(formData['intent'] || '');

  const userRows = await db.select({ username: users.username }).from(users).where(eq(users.id, userId)).limit(1);
  const user = userRows[0];
  const lanIp = getLocalIpAddress();
  const currentPort = process.env.PORT || '3000';
  const detectedLanUrl = lanIp ? `http://${lanIp}:${currentPort}` : 'http://localhost:3000';

  let landlordName = (await getSetting('landlord_name')) || 'Locador';
  let landlordCpf = (await getSetting('landlord_cpf')) || '';
  let landlordEmail = (await getSetting('landlord_email')) || '';
  let appUrl = (await getSetting('app_url')) || '';
  const audit = createAuditContext(c, await resolveAuditActor(userId));

  try {
    if (intent === 'save-landlord') {
      const name = String(formData['name'] || '').trim();
      const cpf = String(formData['cpf'] || '').trim();
      const email = String(formData['email'] || '').trim();
      if (!name) throw new Error('Nome do locador é obrigatório.');
      if (email && !isValidEmail(email)) throw new Error('Informe um e-mail válido.');
      await setSetting('landlord_name', name);
      await setSetting('landlord_cpf', cpf);
      await setSetting('landlord_email', email);
      await recordAudit(audit, {
        action: 'setting.landlord',
        entity: 'setting',
        entityId: 'landlord',
        entityLabel: name,
        summary: `Dados do locador "${name}" alterados`,
        changes: diffAuditFields(
          { name: landlordName, cpf: landlordCpf, email: landlordEmail },
          { name, cpf, email },
          [
            { key: 'name', label: 'Nome' },
            { key: 'cpf', label: 'CPF' },
            { key: 'email', label: 'E-mail' },
          ]
        ),
      });
      landlordName = name;
      landlordCpf = cpf;
      landlordEmail = email;

      const html = renderHtml(
        'Aluxen | Configurações',
        'settings',
        { landlordName, landlordCpf, landlordEmail, appUrl, detectedLanUrl, username: user?.username || '', message: 'Dados salvos com sucesso.' },
        <SettingsPage landlordName={landlordName} landlordCpf={landlordCpf} landlordEmail={landlordEmail} appUrl={appUrl} detectedLanUrl={detectedLanUrl} username={user?.username || ''} message="Dados salvos com sucesso." />
      );
      return c.html(html);
    }

    if (intent === 'save-app-url') {
      const url = String(formData['app_url'] || '').trim();
      await setSetting('app_url', url);
      await recordAudit(audit, {
        action: 'setting.app-url',
        entity: 'setting',
        entityId: 'app_url',
        entityLabel: 'URL pública do sistema',
        summary: url ? `URL pública do sistema definida como "${url}"` : 'URL pública do sistema removida',
        changes: diffAuditFields({ url: appUrl }, { url }, [{ key: 'url', label: 'URL pública' }]),
      });
      appUrl = url;

      const html = renderHtml(
        'Aluxen | Configurações',
        'settings',
        { landlordName, landlordCpf, landlordEmail, appUrl, detectedLanUrl, username: user?.username || '', message: 'URL pública do sistema salva com sucesso.' },
        <SettingsPage landlordName={landlordName} landlordCpf={landlordCpf} landlordEmail={landlordEmail} appUrl={appUrl} detectedLanUrl={detectedLanUrl} username={user?.username || ''} message="URL pública do sistema salva com sucesso." />
      );
      return c.html(html);
    }

    if (intent === 'change-password') {
      const currentPassword = String(formData['current_password'] || '');
      const newPassword = String(formData['new_password'] || '');
      const confirmPassword = String(formData['confirm_password'] || '');
      if (newPassword !== confirmPassword) throw new Error('Nova senha e confirmação não conferem.');
      await changePassword(userId, currentPassword, newPassword);
      await recordAudit(audit, {
        action: 'user.password',
        entity: 'user',
        entityId: userId,
        entityLabel: user?.username ?? null,
        summary: `Senha do usuário "${user?.username || 'administrador'}" alterada`,
        metadata: { username: user?.username ?? null },
      });

      const html = renderHtml(
        'Aluxen | Configurações',
        'settings',
        { landlordName, landlordCpf, landlordEmail, appUrl, detectedLanUrl, username: user?.username || '', message: 'Senha alterada com sucesso.' },
        <SettingsPage landlordName={landlordName} landlordCpf={landlordCpf} landlordEmail={landlordEmail} appUrl={appUrl} detectedLanUrl={detectedLanUrl} username={user?.username || ''} message="Senha alterada com sucesso." />
      );
      return c.html(html);
    }

    throw new Error('Ação inválida.');
  } catch (error) {
    const errorMsg = error instanceof Error ? error.message : 'Erro ao salvar.';
    const html = renderHtml(
      'Aluxen | Configurações',
      'settings',
      { landlordName, landlordCpf, landlordEmail, appUrl, detectedLanUrl, username: user?.username || '', error: errorMsg },
      <SettingsPage landlordName={landlordName} landlordCpf={landlordCpf} landlordEmail={landlordEmail} appUrl={appUrl} detectedLanUrl={detectedLanUrl} username={user?.username || ''} error={errorMsg} />
    );
    return c.html(html, 400);
  }
});

// Função agregada de cálculo de métricas (utilizada por SSR em /dashboard e via API em GET /metrics)
export async function computeMetrics(): Promise<MetricsPayload> {
  const [allTenantsRows, allPropertiesRows, activeLeasesRows, terminatedLeasesRows, allReceiptsRows] = await Promise.all([
    db.select({ id: tenants.id, name: tenants.name }).from(tenants),
    db.select({ id: properties.id, name: properties.name }).from(properties),
    db.select({ id: leases.id, rentAmount: leases.rentAmount }).from(leases).where(eq(leases.status, 'active')),
    db.select({ id: leases.id }).from(leases).where(eq(leases.status, 'terminated')),
    db
      .select({
        id: receipts.id,
        dueDate: receipts.dueDate,
        paidAt: receipts.paidAt,
        amount: receipts.amount,
        paidAmount: receipts.paidAmount,
        category: receipts.category,
        tenantId: sql<string | null>`coalesce(${receipts.tenantId}, ${leases.tenantId})`,
      })
      .from(receipts)
      .leftJoin(leases, eq(receipts.leaseId, leases.id)),
  ]);

  const today = new Date();
  today.setHours(0, 0, 0, 0);

  let paidCount = 0, partialCount = 0, dueCount = 0, overdueCount = 0;
  let totalAmount = 0, paidAmount = 0, openAmount = 0, overdueAmount = 0;

  const monthlyMap = new Map<string, { received: number; expected: number; open: number }>();
  const categoryMap = new Map<string, { count: number; total: number; label: string }>();
  const tenantPaidMap = new Map<string, { total: number; count: number }>();

  const monthsBack = 6;

  for (const r of allReceiptsRows) {
    const amount = Number(r.amount) || 0;
    totalAmount += amount;
    const dueDate = new Date(`${r.dueDate}T00:00:00`);
    const hasExplicitPaidAmount = r.paidAmount != null;
    const paidAmt = hasExplicitPaidAmount ? Number(r.paidAmount) : (r.paidAt ? amount : 0);

    let status: 'paid' | 'partial' | 'due' | 'overdue';
    if (hasExplicitPaidAmount && paidAmt > 0 && amount > 0) {
      status = paidAmt < amount ? 'partial' : 'paid';
    } else if (r.paidAt) {
      status = 'paid';
    } else {
      status = dueDate < today ? 'overdue' : 'due';
    }

    if (status === 'paid') paidCount++;
    else if (status === 'partial') partialCount++;
    else if (status === 'due') dueCount++;
    else overdueCount++;

    if (status === 'paid' || status === 'partial') paidAmount += paidAmt;
    if (status === 'due') openAmount += amount;
    if (status === 'overdue') overdueAmount += amount;

    const d = dueDate;
    const monthKey = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
    const monthEntry = monthlyMap.get(monthKey) || { received: 0, expected: 0, open: 0 };
    if (status === 'paid' || status === 'partial') monthEntry.received += paidAmt;
    monthEntry.expected += amount;
    if (status !== 'paid' && status !== 'partial') monthEntry.open += amount;
    monthlyMap.set(monthKey, monthEntry);

    const cat = r.category || 'rent';
    const catEntry = categoryMap.get(cat) || { count: 0, total: 0, label: RECEIPT_CATEGORY_LABELS[cat] || cat };
    catEntry.count++;
    catEntry.total += amount;
    categoryMap.set(cat, catEntry);

    if ((status === 'paid' || status === 'partial') && r.tenantId) {
      const tenantEntry = tenantPaidMap.get(r.tenantId) || { total: 0, count: 0 };
      tenantEntry.total += paidAmt;
      tenantEntry.count++;
      tenantPaidMap.set(r.tenantId, tenantEntry);
    }
  }

  const monthly: Array<{ key: string; label: string; received: number; expected: number; open: number }> = [];
  const monthsLabel = ['jan', 'fev', 'mar', 'abr', 'mai', 'jun', 'jul', 'ago', 'set', 'out', 'nov', 'dez'];
  for (let i = 3; i >= -2; i--) {
    const d = new Date(today.getFullYear(), today.getMonth() - i, 1);
    const key = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
    const entry = monthlyMap.get(key) || { received: 0, expected: 0, open: 0 };
    monthly.push({
      key,
      label: `${monthsLabel[d.getMonth()]}/${String(d.getFullYear()).slice(-2)}`,
      received: entry.received,
      expected: entry.expected,
      open: entry.open,
    });
  }

  const statusList = [
    { status: 'paid', label: 'Pago', count: paidCount },
    { status: 'partial', label: 'Parcial', count: partialCount },
    { status: 'due', label: 'A vencer', count: dueCount },
    { status: 'overdue', label: 'Vencido', count: overdueCount },
  ];

  const categories = Array.from(categoryMap.entries()).map(([cat, v]) => ({ category: cat, label: v.label, count: v.count, total: v.total }));

  const tenantNameMap = new Map(allTenantsRows.map((t) => [t.id, t.name]));
  const topTenantsRaw = Array.from(tenantPaidMap.entries()).sort((a, b) => b[1].total - a[1].total).slice(0, 5);
  const topTenants = topTenantsRaw.map(([tid, v]) => ({
    id: tid,
    name: tenantNameMap.get(tid) || 'Inquilino',
    paidTotal: v.total,
    receipts: v.count,
  }));

  const propertyOccupation = await db.select({
    id: properties.id,
    name: properties.name,
    activeLeases: sql<number>`(SELECT COUNT(*) FROM "leases" WHERE "leases"."property_id" = "properties"."id" AND "leases"."status" = 'active')`,
  }).from(properties);

  const expectedMonthly = activeLeasesRows.reduce((acc, l) => acc + (Number(l.rentAmount) || 0), 0);

  return {
    totals: {
      tenants: allTenantsRows.length,
      properties: allPropertiesRows.length,
      activeLeases: activeLeasesRows.length,
      terminatedLeases: terminatedLeasesRows.length,
      receipts: allReceiptsRows.length,
      paidReceipts: paidCount,
      partialReceipts: partialCount,
      dueReceipts: dueCount,
      overdueReceipts: overdueCount,
      totalAmount,
      paidAmount,
      openAmount,
      overdueAmount,
      expectedMonthly,
    },
    monthly,
    status: statusList,
    categories,
    properties: propertyOccupation,
    topTenants,
  };
}

// Endpoint de métricas agregado para o Dashboard
app.get('/metrics', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.json({ error: 'Não autorizado.' }, 401);

  const metrics = await computeMetrics();
  return c.json(metrics);
});

// Histórico de auditoria (aba Histórico) — página de eventos com filtros.
app.get('/audit', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.json({ error: 'Não autorizado.' }, 401);

  const query = readAuditQuery(new URLSearchParams(c.req.url.split('?')[1] || ''));
  return c.json(await listAudit(query));
});

// Exportação CSV da trilha com os mesmos filtros da aba Histórico.
app.get('/audit/export', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.redirect('/login');

  const query = readAuditQuery(new URLSearchParams(c.req.url.split('?')[1] || ''));
  const entries = await listAuditForExport(query);
  const filename = `aluxen-historico-${new Date().toISOString().split('T')[0]}.csv`;

  await recordAudit(createAuditContext(c, await resolveAuditActor(userId)), {
    action: 'history.export',
    entity: 'backup',
    entityLabel: filename,
    summary: `Histórico de auditoria exportado em CSV com ${entries.length} evento(s)`,
    metadata: { fileName: filename, count: entries.length },
  });

  return new Response(buildAuditCsv(entries), {
    headers: {
      'Content-Type': 'text/csv; charset=utf-8',
      'Content-Disposition': `attachment; filename="${filename}"`,
    },
  });
});

app.get('/export', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.redirect('/login');

  const [tenantsRows, propertiesRows, leasesRows, receiptsRows] = await Promise.all([
    db.select().from(tenants),
    db.select().from(properties),
    db.select().from(leases),
    db.select({ id: receipts.id, lease_id: receipts.leaseId, due_date: receipts.dueDate, paid_at: receipts.paidAt, amount: receipts.amount, created_at: receipts.createdAt }).from(receipts),
  ]);

  const data = {
    exportedAt: new Date().toISOString(),
    tenants: tenantsRows,
    properties: propertiesRows,
    leases: leasesRows,
    receipts: receiptsRows,
  };

  const filename = `aluxen-backup-${new Date().toISOString().split('T')[0]}.json`;

  await recordAudit(createAuditContext(c, await resolveAuditActor(userId)), {
    action: 'backup.export',
    entity: 'backup',
    entityLabel: filename,
    summary: `Backup exportado com ${tenantsRows.length} inquilino(s), ${propertiesRows.length} imóvel(is), ${leasesRows.length} contrato(s) e ${receiptsRows.length} pagamento(s)`,
    metadata: {
      fileName: filename,
      tenants: tenantsRows.length,
      properties: propertiesRows.length,
      leases: leasesRows.length,
      receipts: receiptsRows.length,
    },
  });

  return new Response(JSON.stringify(data, null, 2), {
    headers: {
      'Content-Type': 'application/json',
      'Content-Disposition': `attachment; filename="${filename}"`,
    },
  });
});

// Importar Backup JSON
app.post('/import', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.redirect('/login');

  const body = await c.req.parseBody();
  const file = body['file'] as File | undefined;

  if (!file) {
    return c.text('Arquivo não enviado.', 400);
  }

  let content: string;
  try {
    content = await file.text();
  } catch {
    return c.text('Não foi possível ler o arquivo.', 400);
  }

  interface BackupData {
    tenants: { id: string; name: string; email: string; phone: string | null; created_at: string; updated_at: string }[];
    properties: { id: string; name: string; address: string; city: string; state: string; zip: string | null; created_at: string; updated_at: string }[];
    leases: { id: string; tenant_id: string; property_id: string; start_date: string; end_date: string; due_day: number; rent_amount: number; created_at: string; updated_at: string }[];
    receipts: { id: string; lease_id: string; due_date: string; paid_at: string | null; amount: number; pdf_path: string | null; created_at: string }[];
  }

  let data: BackupData;
  try {
    data = JSON.parse(content);
  } catch {
    return c.text('Arquivo JSON inválido.', 400);
  }

  if (!data.tenants || !data.properties || !data.leases || !data.receipts) {
    return c.text('Formato de backup inválido.', 400);
  }

  try {
    const idMap = {
      tenants: new Map<string, string>(),
      properties: new Map<string, string>(),
      leases: new Map<string, string>(),
    };

    for (const t of data.tenants) {
      const newId = createId();
      idMap.tenants.set(t.id, newId);
      await db.insert(tenants).values({
        id: newId,
        name: t.name,
        email: t.email,
        phone: t.phone,
        createdAt: t.created_at,
        updatedAt: t.updated_at,
      });
    }

    for (const p of data.properties) {
      const newId = createId();
      idMap.properties.set(p.id, newId);
      await db.insert(properties).values({
        id: newId,
        name: p.name,
        address: p.address,
        city: p.city,
        state: p.state,
        zip: p.zip,
        createdAt: p.created_at,
        updatedAt: p.updated_at,
      });
    }

    for (const l of data.leases) {
      const newId = createId();
      idMap.leases.set(l.id, newId);
      await db.insert(leases).values({
        id: newId,
        tenantId: idMap.tenants.get(l.tenant_id) || l.tenant_id,
        propertyId: idMap.properties.get(l.property_id) || l.property_id,
        startDate: l.start_date,
        endDate: l.end_date,
        dueDay: l.due_day ?? 1,
        rentAmount: l.rent_amount,
        createdAt: l.created_at,
        updatedAt: l.updated_at,
      });
    }

    for (const r of data.receipts) {
      const newId = createId();
      await db.insert(receipts).values({
        id: newId,
        leaseId: idMap.leases.get(r.lease_id) || r.lease_id,
        dueDate: r.due_date,
        paidAt: r.paid_at,
        amount: r.amount,
        pdfPath: r.pdf_path,
        createdAt: r.created_at,
      });
    }
  } catch (error) {
    return c.text(error instanceof Error ? error.message : 'Erro ao importar dados.', 400);
  }

  await recordAudit(createAuditContext(c, await resolveAuditActor(userId)), {
    action: 'backup.import',
    entity: 'backup',
    entityLabel: file.name,
    summary: `Backup importado com ${data.tenants.length} inquilino(s), ${data.properties.length} imóvel(is), ${data.leases.length} contrato(s) e ${data.receipts.length} pagamento(s)`,
    metadata: {
      fileName: file.name,
      tenants: data.tenants.length,
      properties: data.properties.length,
      leases: data.leases.length,
      receipts: data.receipts.length,
    },
  });

  return c.redirect('/dashboard?tab=dashboard&toast=importado');
});

/** Confirma que o recibo pertence ao locatário autenticado (via contrato ou recibo avulso). */
async function tenantOwnsReceipt(tenantId: string, receiptId: string): Promise<boolean> {
  const rows = await db
    .select({ id: receipts.id })
    .from(receipts)
    .leftJoin(leases, eq(receipts.leaseId, leases.id))
    .where(and(eq(receipts.id, receiptId), or(eq(leases.tenantId, tenantId), eq(receipts.tenantId, tenantId))))
    .limit(1);
  return rows.length > 0;
}

/** Confirma que o contrato pertence ao locatário autenticado. */
async function tenantOwnsLease(tenantId: string, leaseId: string): Promise<boolean> {
  const rows = await db
    .select({ id: leases.id })
    .from(leases)
    .where(and(eq(leases.id, leaseId), eq(leases.tenantId, tenantId)))
    .limit(1);
  return rows.length > 0;
}

// Emissão de Recibo em PDF
app.get('/pdf', async (c) => {
  const id = c.req.query('id');
  const token = c.req.query('token');
  const showSignature = c.req.query('assinatura') !== 'false';

  if (!id) {
    return c.text('ID inválido', 400);
  }

  const userId = getUserId(c);
  const isTokenValid = token ? verifyAuthToken('receipt', id, token) : false;
  const clientId = getClientId(c);
  const clientOwnsReceipt = clientId ? await tenantOwnsReceipt(clientId, id) : false;

  // Permite acesso se o usuário estiver logado (administrador), se possuir token de
  // autenticação válido (link público) OU se o locatário autenticado for o titular.
  if (!userId && !isTokenValid && !clientOwnsReceipt) {
    if (clientId) {
      return c.text('Este documento não pertence ao seu cadastro.', 403);
    }
    return c.redirect('/login');
  }

  const pdfTAlias = aliasedTable(tenants, 't');
  const pdfPAlias = aliasedTable(properties, 'p');
  const pdfRtAlias = aliasedTable(tenants, 'rt');
  const pdfRpAlias = aliasedTable(properties, 'rp');

  const receiptRows = await db.select({
    id: receipts.id,
    due_date: receipts.dueDate,
    paid_at: receipts.paidAt,
    amount: receipts.amount,
    paid_amount: receipts.paidAmount,
    category: receipts.category,
    description: receipts.description,
    tenant_name: sql<string>`coalesce(${pdfTAlias.name}, ${pdfRtAlias.name}, '')`,
    tenant_cpf: sql<string>`coalesce(${pdfTAlias.cpf}, ${pdfRtAlias.cpf}, '')`,
    property_name: sql<string>`coalesce(${pdfPAlias.name}, ${pdfRpAlias.name}, '')`,
    property_address: sql<string>`coalesce(${pdfPAlias.address}, ${pdfRpAlias.address}, '')`,
    property_city: sql<string>`coalesce(${pdfPAlias.city}, ${pdfRpAlias.city}, '')`,
    property_state: sql<string>`coalesce(${pdfPAlias.state}, ${pdfRpAlias.state}, '')`,
  })
    .from(receipts)
    .leftJoin(leases, eq(receipts.leaseId, leases.id))
    .leftJoin(pdfTAlias, eq(leases.tenantId, pdfTAlias.id))
    .leftJoin(pdfPAlias, eq(leases.propertyId, pdfPAlias.id))
    .leftJoin(pdfRtAlias, eq(receipts.tenantId, pdfRtAlias.id))
    .leftJoin(pdfRpAlias, eq(receipts.propertyId, pdfRpAlias.id))
    .where(eq(receipts.id, id))
    .limit(1);

  const receipt = receiptRows[0];

  if (!receipt) {
    return c.text('Recibo não encontrado', 404);
  }

  // Recibo liberado para emissão/download apenas quando houver pagamento
  // (integral ou parcial). Pendente/vencido não gera recibo.
  if (!receipt.paid_at) {
    return c.text('Recibo não pago. O recibo só é liberado após o pagamento.', 403);
  }

  const propertyAddress = [receipt.property_address, receipt.property_city, receipt.property_state].filter(Boolean).join(', ');

  const pdfBytes = await createReceiptPdfBuffer({
    receiptId: receipt.id,
    tenantName: receipt.tenant_name || 'Locatário',
    tenantCpf: receipt.tenant_cpf || null,
    propertyName: receipt.property_name || '',
    propertyAddress,
    category: receipt.category,
    description: receipt.description,
    dueDate: receipt.due_date,
    paidAt: receipt.paid_at,
    amount: receipt.amount,
    paidAmount: receipt.paid_amount != null ? Number(receipt.paid_amount) : null,
    signature: showSignature,
    baseUrl: await resolvePublicBaseUrl(c),
  });

  const receiptLabel = describeReceipt({
    tenant_name: receipt.tenant_name,
    property_name: receipt.property_name,
    dueDate: receipt.due_date,
    description: receipt.description,
  });

  await recordAudit(createAuditContext(c, await resolveAuditActor(userId), userId ? 'web' : 'public'), {
    action: 'document.receipt',
    entity: 'document',
    entityId: receipt.id,
    entityLabel: receiptLabel,
    summary: `Recibo de ${receiptLabel} emitido`,
    metadata: { kind: 'Recibo', signature: showSignature },
  });

  return new Response(pdfBytes as BodyInit, {
    headers: {
      'Content-Type': 'application/pdf',
      'Content-Disposition': `inline; filename="recibo-${receipt.id}.pdf"`,
    },
  });
});

// Emissão de Contrato de Locação em PDF
app.get('/contract-pdf', async (c) => {
  const id = c.req.query('id');
  const token = c.req.query('token');

  if (!id) {
    return c.text('ID inválido', 400);
  }

  const userId = getUserId(c);
  const isTokenValid = token ? verifyAuthToken('lease', id, token) : false;
  const clientId = getClientId(c);
  const clientOwnsLease = clientId ? await tenantOwnsLease(clientId, id) : false;

  // Permite acesso se o usuário estiver logado (administrador), se possuir token de
  // autenticação válido (link público) OU se o locatário autenticado for o titular.
  if (!userId && !isTokenValid && !clientOwnsLease) {
    if (clientId) {
      return c.text('Este documento não pertence ao seu cadastro.', 403);
    }
    return c.redirect('/login');
  }

  const contractTAlias = aliasedTable(tenants, 't');
  const contractPAlias = aliasedTable(properties, 'p');

  const leaseRows = await db.select({
    id: leases.id,
    start_date: leases.startDate,
    end_date: leases.endDate,
    due_day: leases.dueDay,
    rent_amount: leases.rentAmount,
    status: leases.status,
    tenant_name: contractTAlias.name,
    tenant_phone: contractTAlias.phone,
    tenant_cpf: contractTAlias.cpf,
    property_name: contractPAlias.name,
    property_address: contractPAlias.address,
    property_city: contractPAlias.city,
    property_state: contractPAlias.state,
    property_zip: contractPAlias.zip,
  })
    .from(leases)
    .innerJoin(contractTAlias, eq(leases.tenantId, contractTAlias.id))
    .innerJoin(contractPAlias, eq(leases.propertyId, contractPAlias.id))
    .where(eq(leases.id, id))
    .limit(1);

  const lease = leaseRows[0];

  if (!lease) {
    return c.text('Contrato não encontrado', 404);
  }

  const pdfBytes = await createContractPdfBuffer({
    leaseId: lease.id,
    tenantName: lease.tenant_name,
    tenantPhone: lease.tenant_phone || '',
    tenantCpf: lease.tenant_cpf || '',
    propertyName: lease.property_name,
    propertyAddress: lease.property_address,
    propertyCity: lease.property_city,
    propertyState: lease.property_state,
    propertyZip: lease.property_zip || '',
    startDate: lease.start_date,
    endDate: lease.end_date,
    dueDay: lease.due_day,
    rentAmount: lease.rent_amount,
    status: lease.status,
    baseUrl: await resolvePublicBaseUrl(c),
  });

  const leaseLabel = describeLease(lease);
  await recordAudit(createAuditContext(c, await resolveAuditActor(userId), userId ? 'web' : 'public'), {
    action: 'document.contract',
    entity: 'document',
    entityId: lease.id,
    entityLabel: leaseLabel,
    summary: `Contrato de ${leaseLabel} emitido`,
    metadata: { kind: 'Contrato', signature: true },
  });

  return new Response(pdfBytes as BodyInit, {
    headers: {
      'Content-Type': 'application/pdf',
      'Content-Disposition': `inline; filename="contrato-${lease.id}.pdf"`,
    },
  });
});

// Validação Pública de Recibo
app.get('/validar/recibo/:id', async (c) => {
  const id = c.req.param('id');
  const token = c.req.query('token') || '';
  const now = new Date();
  const verifiedAt = new Intl.DateTimeFormat('pt-BR', { dateStyle: 'long', timeStyle: 'short' }).format(now);

  const isValidToken = verifyAuthToken('receipt', id, token);
  if (!isValidToken) {
    const html = renderHtml(
      'Aluxen | Autenticidade do Recibo',
      'verify',
      {},
      <VerificationPage valid={false} error="Chave de autenticação inválida ou documento não autenticado." verifiedAt={verifiedAt} />
    );
    return c.html(html, 400);
  }

  const vRecTAlias = aliasedTable(tenants, 't');
  const vRecPAlias = aliasedTable(properties, 'p');
  const vRecRtAlias = aliasedTable(tenants, 'rt');
  const vRecRpAlias = aliasedTable(properties, 'rp');

  const receiptRows = await db.select({
    id: receipts.id,
    due_date: receipts.dueDate,
    paid_at: receipts.paidAt,
    amount: receipts.amount,
    paid_amount: receipts.paidAmount,
    category: receipts.category,
    description: receipts.description,
    tenant_name: sql<string>`coalesce(${vRecTAlias.name}, ${vRecRtAlias.name}, '')`,
    tenant_cpf: sql<string>`coalesce(${vRecTAlias.cpf}, ${vRecRtAlias.cpf}, '')`,
    property_name: sql<string>`coalesce(${vRecPAlias.name}, ${vRecRpAlias.name}, '')`,
    property_address: sql<string>`coalesce(${vRecPAlias.address}, ${vRecRpAlias.address}, '')`,
    property_city: sql<string>`coalesce(${vRecPAlias.city}, ${vRecRpAlias.city}, '')`,
    property_state: sql<string>`coalesce(${vRecPAlias.state}, ${vRecRpAlias.state}, '')`,
  })
    .from(receipts)
    .leftJoin(leases, eq(receipts.leaseId, leases.id))
    .leftJoin(vRecTAlias, eq(leases.tenantId, vRecTAlias.id))
    .leftJoin(vRecPAlias, eq(leases.propertyId, vRecPAlias.id))
    .leftJoin(vRecRtAlias, eq(receipts.tenantId, vRecRtAlias.id))
    .leftJoin(vRecRpAlias, eq(receipts.propertyId, vRecRpAlias.id))
    .where(eq(receipts.id, id))
    .limit(1);

  const receipt = receiptRows[0];

  if (!receipt) {
    const html = renderHtml(
      'Aluxen | Autenticidade do Recibo',
      'verify',
      {},
      <VerificationPage valid={false} error="Recibo não localizado na base oficial de registros." verifiedAt={verifiedAt} />
    );
    return c.html(html, 404);
  }

  // Recibo ainda não pago: não disponível para validação/download público
  if (!receipt.paid_at) {
    const html = renderHtml(
      'Aluxen | Autenticidade do Recibo',
      'verify',
      {},
      <VerificationPage valid={false} error="Recibo ainda não pago. O documento só é liberado após o pagamento." verifiedAt={verifiedAt} />
    );
    return c.html(html, 403);
  }

  const landlordName = (await getSetting('landlord_name')) || 'Locador';
  const fullPropertyAddress = [receipt.property_address, receipt.property_city, receipt.property_state].filter(Boolean).join(', ');

  const html = renderHtml(
    'Aluxen | Autenticidade do Recibo',
    'verify',
    {},
    <VerificationPage
      valid={true}
      type="receipt"
      id={receipt.id}
      token={token}
      authCode={formatAuthCode(token)}
      tenantName={receipt.tenant_name}
      tenantCpf={receipt.tenant_cpf || undefined}
      propertyName={receipt.property_name}
      propertyAddress={fullPropertyAddress}
      landlordName={landlordName}
      amount={formatCurrency(receipt.amount)}
      amountRaw={receipt.amount}
      paidAmount={receipt.paid_amount != null ? formatCurrency(receipt.paid_amount) : undefined}
      paidAmountRaw={receipt.paid_amount != null ? Number(receipt.paid_amount) : undefined}
      category={receipt.category}
      description={receipt.description || undefined}
      dueDate={formatDateBr(receipt.due_date)}
      paidAt={formatDateTimeBr(receipt.paid_at)}
      verifiedAt={verifiedAt}
    />
  );
  return c.html(html);
});

// Validação Pública de Contrato
app.get('/validar/contrato/:id', async (c) => {
  const id = c.req.param('id');
  const token = c.req.query('token') || '';
  const now = new Date();
  const verifiedAt = new Intl.DateTimeFormat('pt-BR', { dateStyle: 'long', timeStyle: 'short' }).format(now);

  const isValidToken = verifyAuthToken('lease', id, token);
  if (!isValidToken) {
    const html = renderHtml(
      'Aluxen | Autenticidade do Contrato',
      'verify',
      {},
      <VerificationPage valid={false} error="Chave de autenticação inválida ou documento não autenticado." verifiedAt={verifiedAt} />
    );
    return c.html(html, 400);
  }

  const vLseTAlias = aliasedTable(tenants, 't');
  const vLsePAlias = aliasedTable(properties, 'p');

  const leaseRows = await db.select({
    id: leases.id,
    start_date: leases.startDate,
    end_date: leases.endDate,
    due_day: leases.dueDay,
    rent_amount: leases.rentAmount,
    status: leases.status,
    tenant_name: vLseTAlias.name,
    tenant_cpf: vLseTAlias.cpf,
    property_name: vLsePAlias.name,
    property_address: vLsePAlias.address,
    property_city: vLsePAlias.city,
    property_state: vLsePAlias.state,
  })
    .from(leases)
    .innerJoin(vLseTAlias, eq(leases.tenantId, vLseTAlias.id))
    .innerJoin(vLsePAlias, eq(leases.propertyId, vLsePAlias.id))
    .where(eq(leases.id, id))
    .limit(1);

  const lease = leaseRows[0];

  if (!lease) {
    const html = renderHtml(
      'Aluxen | Autenticidade do Contrato',
      'verify',
      {},
      <VerificationPage valid={false} error="Contrato não localizado na base oficial de registros." verifiedAt={verifiedAt} />
    );
    return c.html(html, 404);
  }

  const landlordName = (await getSetting('landlord_name')) || 'Locador';
  const fullPropertyAddress = [lease.property_address, lease.property_city, lease.property_state].filter(Boolean).join(', ');

  const html = renderHtml(
    'Aluxen | Autenticidade do Contrato',
    'verify',
    {},
    <VerificationPage
      valid={true}
      type="lease"
      id={lease.id}
      token={token}
      authCode={formatAuthCode(token)}
      tenantName={lease.tenant_name}
      tenantCpf={lease.tenant_cpf || undefined}
      propertyName={lease.property_name}
      propertyAddress={fullPropertyAddress}
      landlordName={landlordName}
      amount={formatCurrency(lease.rent_amount)}
      amountRaw={lease.rent_amount}
      startDate={formatDateBr(lease.start_date)}
      endDate={formatDateBr(lease.end_date)}
      dueDay={lease.due_day}
      status={lease.status}
      verifiedAt={verifiedAt}
    />
  );
  return c.html(html);
});

// ==========================================================================
// WhatsApp API Endpoints
// ==========================================================================

app.get('/api/whatsapp/status', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.json({ error: 'Não autorizado.' }, 401);

  const profileDir = resolveProfileDir();
  const status = await checkWhatsAppSessionStatus();
  return c.json({ ...status, profileDir });
});

app.post('/api/whatsapp/login/start', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.json({ error: 'Não autorizado.' }, 401);

  const state = await whatsappLoginManager.startLogin();
  return c.json({ ok: true, state });
});

app.get('/api/whatsapp/login/status', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.json({ error: 'Não autorizado.' }, 401);

  const state = whatsappLoginManager.getState();
  return c.json({ ok: true, state });
});

app.post('/api/whatsapp/login/cancel', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.json({ error: 'Não autorizado.' }, 401);

  await whatsappLoginManager.cancelLogin();
  return c.json({ ok: true, state: whatsappLoginManager.getState() });
});

app.post('/api/whatsapp/logout', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.json({ error: 'Não autorizado.' }, 401);

  const audit = createAuditContext(c, await resolveAuditActor(userId));
  await whatsappLoginManager.logout();
  await recordAudit(audit, {
    action: 'whatsapp.logout',
    entity: 'whatsapp',
    entityLabel: 'Sessão do WhatsApp Web',
    summary: 'Sessão do WhatsApp Web desconectada',
  });
  return c.json({ ok: true, message: 'WhatsApp desconectado com sucesso.' });
});

app.post('/api/whatsapp/preview', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.json({ error: 'Não autorizado.' }, 401);

  const body = await c.req.parseBody();
  const type = String(body['type'] || '');
  const id = String(body['id'] || '');

  if (!id || (type !== 'receipt' && type !== 'lease')) {
    return c.json({ error: 'Parâmetros inválidos.' }, 400);
  }

  const baseUrl = await resolvePublicBaseUrl(c);
  const landlordName = (await getSetting('landlord_name')) || 'Locador';

  if (type === 'receipt') {
    const wpRecTAlias = aliasedTable(tenants, 't');
    const wpRecPAlias = aliasedTable(properties, 'p');
    const wpRecRtAlias = aliasedTable(tenants, 'rt');
    const wpRecRpAlias = aliasedTable(properties, 'rp');

    const receiptRows = await db.select({
      id: receipts.id,
      due_date: receipts.dueDate,
      paid_at: receipts.paidAt,
      amount: receipts.amount,
      paid_amount: receipts.paidAmount,
      category: receipts.category,
      description: receipts.description,
      tenant_name: sql<string>`coalesce(${wpRecTAlias.name}, ${wpRecRtAlias.name}, '')`,
      tenant_phone: sql<string>`coalesce(${wpRecTAlias.phone}, ${wpRecRtAlias.phone}, '')`,
      property_name: sql<string>`coalesce(${wpRecPAlias.name}, ${wpRecRpAlias.name}, '')`,
      property_address: sql<string>`coalesce(${wpRecPAlias.address}, ${wpRecRpAlias.address}, '')`,
      property_city: sql<string>`coalesce(${wpRecPAlias.city}, ${wpRecRpAlias.city}, '')`,
      property_state: sql<string>`coalesce(${wpRecPAlias.state}, ${wpRecRpAlias.state}, '')`,
    })
      .from(receipts)
      .leftJoin(leases, eq(receipts.leaseId, leases.id))
      .leftJoin(wpRecTAlias, eq(leases.tenantId, wpRecTAlias.id))
      .leftJoin(wpRecPAlias, eq(leases.propertyId, wpRecPAlias.id))
      .leftJoin(wpRecRtAlias, eq(receipts.tenantId, wpRecRtAlias.id))
      .leftJoin(wpRecRpAlias, eq(receipts.propertyId, wpRecRpAlias.id))
      .where(eq(receipts.id, id))
      .limit(1);

    const receipt = receiptRows[0];

    if (!receipt) {
      return c.json({ error: 'Recibo não encontrado.' }, 404);
    }

    // Bloqueio de pré-visualização/envio para recibos ainda não pagos
    if (!receipt.paid_at) {
      return c.json({ error: 'Recibo ainda não pago. O documento só é liberado após o pagamento.' }, 403);
    }

    const phone = receipt.tenant_phone || '';
    const wtoken = generateAuthToken('receipt', receipt.id);
    const validationUrl = await getValidationUrl('receipt', receipt.id, wtoken, baseUrl);
    const pdfUrl = `${baseUrl}/pdf?id=${receipt.id}&token=${wtoken}`;
    const fullPropertyAddress = [receipt.property_address, receipt.property_city, receipt.property_state]
      .filter(Boolean)
      .join(', ');

    const message = buildReceiptWhatsAppMessage({
      tenantName: receipt.tenant_name || 'Locatário',
      propertyName: receipt.property_name || '',
      propertyAddress: fullPropertyAddress,
      dueDate: formatDateBr(receipt.due_date),
      amount: formatCurrency(receipt.amount),
      paidAmount: receipt.paid_amount != null ? formatCurrency(receipt.paid_amount) : undefined,
      paidAmountRaw: receipt.paid_amount != null ? Number(receipt.paid_amount) : undefined,
      amountRaw: Number(receipt.amount),
      category: receipt.category,
      description: receipt.description || undefined,
      paidAt: receipt.paid_at ? formatDateTimeBr(receipt.paid_at) : null,
      pdfUrl,
      validationUrl,
      landlordName,
    });

    const directUrl = buildDirectWhatsAppUrl(phone, message);

    return c.json({
      ok: true,
      type: 'receipt',
      id: receipt.id,
      phone,
      formattedPhone: formatWhatsAppNumber(phone),
      tenantName: receipt.tenant_name,
      propertyName: receipt.property_name,
      message,
      directUrl,
    });
  }

  // type === 'lease'
  const wpLseTAlias = aliasedTable(tenants, 't');
  const wpLsePAlias = aliasedTable(properties, 'p');

  const leaseRows = await db.select({
    id: leases.id,
    start_date: leases.startDate,
    end_date: leases.endDate,
    due_day: leases.dueDay,
    rent_amount: leases.rentAmount,
    status: leases.status,
    tenant_name: wpLseTAlias.name,
    tenant_phone: wpLseTAlias.phone,
    property_name: wpLsePAlias.name,
    property_address: wpLsePAlias.address,
    property_city: wpLsePAlias.city,
    property_state: wpLsePAlias.state,
  })
    .from(leases)
    .innerJoin(wpLseTAlias, eq(leases.tenantId, wpLseTAlias.id))
    .innerJoin(wpLsePAlias, eq(leases.propertyId, wpLsePAlias.id))
    .where(eq(leases.id, id))
    .limit(1);

  const lease = leaseRows[0];

  if (!lease) {
    return c.json({ error: 'Contrato não encontrado.' }, 404);
  }

  const phone = lease.tenant_phone || '';
  const token = generateAuthToken('lease', lease.id);
  const validationUrl = await getValidationUrl('lease', lease.id, token, baseUrl);
  const pdfUrl = `${baseUrl}/contract-pdf?id=${lease.id}&token=${token}`;
  const fullPropertyAddress = [lease.property_address, lease.property_city, lease.property_state]
    .filter(Boolean)
    .join(', ');

  const message = buildContractWhatsAppMessage({
    tenantName: lease.tenant_name,
    propertyName: lease.property_name,
    propertyAddress: fullPropertyAddress,
    startDate: formatDateBr(lease.start_date),
    endDate: formatDateBr(lease.end_date),
    dueDay: lease.due_day,
    rentAmount: formatCurrency(lease.rent_amount),
    pdfUrl,
    validationUrl,
    landlordName,
  });

  const directUrl = buildDirectWhatsAppUrl(phone, message);

  return c.json({
    ok: true,
    type: 'lease',
    id: lease.id,
    phone,
    formattedPhone: formatWhatsAppNumber(phone),
    tenantName: lease.tenant_name,
    propertyName: lease.property_name,
    message,
    directUrl,
  });
});

app.post('/api/whatsapp/send', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.json({ error: 'Não autorizado.' }, 401);

  const body = await c.req.parseBody();
  const to = String(body['to'] || '').trim();
  const message = String(body['message'] || '').trim();

  if (!to || !message) {
    return c.json({ error: 'Destinatário e mensagem são obrigatórios.' }, 400);
  }

  const audit = createAuditContext(c, await resolveAuditActor(userId));
  const destination = formatWhatsAppNumber(to) || to;
  const job = createSendJob(userId);

  // O envio roda em segundo plano; o cliente acompanha as etapas por /api/whatsapp/send/status.
  void (async () => {
    try {
      const result = await sendWhatsAppMessageHeadless(to, message, (stage) =>
        updateSendJobStage(job.id, stage)
      );
      completeSendJob(job.id, result);
      await recordAudit(audit, {
        action: 'whatsapp.send',
        entity: 'whatsapp',
        entityId: job.id,
        entityLabel: destination,
        summary: `Mensagem enviada para ${destination} pelo WhatsApp`,
        metadata: { phone: to, jobId: job.id },
      });
    } catch (error) {
      const reason = error instanceof Error ? error.message : 'Falha ao enviar mensagem via WhatsApp.';
      failSendJob(job.id, reason);
      await recordAudit(audit, {
        action: 'whatsapp.send-failed',
        entity: 'whatsapp',
        entityId: job.id,
        entityLabel: destination,
        summary: `Falha ao enviar mensagem para ${destination} pelo WhatsApp`,
        metadata: { phone: to, jobId: job.id, reason: reason.slice(0, 300) },
      });
    }
  })();

  return c.json({ ok: true, jobId: job.id });
});

app.get('/api/whatsapp/send/status', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.json({ error: 'Não autorizado.' }, 401);

  const job = getSendJob(c.req.query('jobId') || '');
  if (!job || job.userId !== userId) {
    return c.json({ error: 'Envio não encontrado.' }, 404);
  }

  return c.json({ ok: true, job });
});

const port = Number(process.env.PORT) || 3000;
const lanIp = getLocalIpAddress();
appLog.info('Aluxen iniciado', {
  url: `http://localhost:${port}`,
  lanUrl: lanIp ? `http://${lanIp}:${port}` : undefined,
  database: sqlite.filename,
  nodeEnv: process.env.NODE_ENV || 'development',
  pid: process.pid,
});

export default {
  port,
  fetch: app.fetch,
};
