import { Hono } from 'hono';
import { logger } from 'hono/logger';
import { serveStatic } from 'hono/bun';
import React from 'react';
import { renderToString } from 'react-dom/server';
import { existsSync } from 'fs';
import path from 'path';

import { initializeDatabase, query, queryOne, execute, getSetting, setSetting } from './db';
import { createId } from './utils/id';
import { loginUser, changePassword } from './utils/auth';
import { getUserId, requireUserId, setUserSession, clearUserSession } from './utils/session';
import { getDaysInMonth, formatCurrency, formatDateBr, formatDateTimeBr } from './utils/format';
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

import { DashboardPage, type DashboardProps } from './client/dashboard';
import { LoginPage } from './client/login';
import { SettingsPage } from './client/settings';
import { VerificationPage } from './client/verify';
import type { Tenant, Property, Lease, Receipt, Tab, MutationResponse } from './types/dashboard';

// Garantir que o banco de dados e o bundle do cliente estão prontos
await initializeDatabase();
if (!existsSync(path.join(process.cwd(), 'public', 'dist', 'client.js'))) {
  await buildClientBundle();
}

const app = new Hono();

app.use('*', logger());

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
function renderHtml(title: string, page: 'dashboard' | 'login' | 'settings' | 'verify', props: Record<string, unknown>, content: React.ReactElement): string {
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

// Raiz: Redireciona conforme autenticação
app.get('/', (c) => {
  const userId = getUserId(c);
  if (userId) {
    return c.redirect('/dashboard?tab=tenants');
  }
  return c.redirect('/login');
});

// Login - GET
app.get('/login', (c) => {
  const userId = getUserId(c);
  if (userId) {
    return c.redirect('/dashboard?tab=tenants');
  }
  const html = renderHtml('Aluxen | Entrar', 'login', {}, <LoginPage />);
  return c.html(html);
});

// Login - POST
app.post('/login', async (c) => {
  const ip = c.req.header('x-forwarded-for') || c.req.header('x-real-ip') || 'unknown';
  if (!checkRateLimit(ip)) {
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
    const html = renderHtml('Aluxen | Entrar', 'login', { error: 'Informe usuário e senha.' }, <LoginPage error="Informe usuário e senha." />);
    return c.html(html, 400);
  }

  const user = await loginUser(username, password);
  if (!user) {
    const html = renderHtml('Aluxen | Entrar', 'login', { error: 'Credenciais inválidas.' }, <LoginPage error="Credenciais inválidas." />);
    return c.html(html, 401);
  }

  setUserSession(c, user.id);
  return c.redirect('/dashboard?tab=tenants');
});

// Logout
app.post('/logout', (c) => {
  clearUserSession(c);
  return c.redirect('/login');
});

app.get('/logout', (c) => {
  clearUserSession(c);
  return c.redirect('/login');
});

// Dashboard - GET
app.get('/dashboard', async (c) => {
  const userId = requireUserId(c);
  if (!userId) {
    return c.redirect('/login');
  }

  const urlTab = c.req.query('tab') as Tab;
  const tab: Tab = ['tenants', 'properties', 'leases', 'receipts'].includes(urlTab) ? urlTab : 'tenants';

  const [user, tenants, properties, leases, receipts, activeTenantIds] = await Promise.all([
    queryOne<{ username: string }>('SELECT username FROM users WHERE id = ?', [userId]),
    query<Tenant>('SELECT id, name, email, phone, cpf FROM tenants ORDER BY created_at DESC, name'),
    query<Property>('SELECT id, name, address, city, state, zip FROM properties ORDER BY created_at DESC, name'),
    query<Lease>(`
      SELECT l.id, l.tenant_id, l.property_id, l.start_date, l.end_date, l.due_day, l.rent_amount, l.status,
             t.name AS tenant_name, p.name AS property_name
      FROM leases l
      JOIN tenants t ON t.id = l.tenant_id
      JOIN properties p ON p.id = l.property_id
      ORDER BY l.status, l.created_at DESC
    `),
    query<Receipt>(`
      SELECT r.id, r.lease_id, r.due_date, r.paid_at, r.amount, r.pdf_path, r.generated_key,
             t.name AS tenant_name, p.name AS property_name
      FROM receipts r
      JOIN leases l ON l.id = r.lease_id
      JOIN tenants t ON t.id = l.tenant_id
      JOIN properties p ON p.id = l.property_id
      ORDER BY r.created_at DESC
    `),
    query<{ tenant_id: string }>('SELECT DISTINCT tenant_id FROM leases WHERE status = ?', ['active']),
  ]);

  const dashboardData: DashboardProps = {
    tab,
    user: user?.username || 'admin',
    tenants,
    properties,
    leases,
    receipts,
    activeTenantIds: activeTenantIds.map((r) => r.tenant_id),
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

  try {
    switch (intent) {
      case 'tenant.create': {
        const id = String(formData['id'] || createId());
        const name = String(formData['name'] || '').trim();
        const email = String(formData['email'] || '').trim();
        const phone = String(formData['phone'] || '').trim();
        const cpf = String(formData['cpf'] || '').trim();
        if (!name || !email) throw new Error('Nome e email são obrigatórios.');
        await execute('INSERT INTO tenants (id, name, email, phone, cpf) VALUES (?, ?, ?, ?, ?)', [
          id,
          name,
          email,
          phone || null,
          cpf || null,
        ]);
        break;
      }
      case 'tenant.update': {
        const id = String(formData['id'] || '');
        const name = String(formData['name'] || '').trim();
        const email = String(formData['email'] || '').trim();
        const phone = String(formData['phone'] || '').trim();
        const cpf = String(formData['cpf'] || '').trim();
        if (!id || !name || !email) throw new Error('Nome e email são obrigatórios.');
        await execute("UPDATE tenants SET name = ?, email = ?, phone = ?, cpf = ?, updated_at = datetime('now') WHERE id = ?", [
          name,
          email,
          phone || null,
          cpf || null,
          id,
        ]);
        break;
      }
      case 'tenant.delete': {
        const tenantId = String(formData['id']);
        const activeLease = await queryOne<{ id: string }>('SELECT id FROM leases WHERE tenant_id = ? AND status = ?', [
          tenantId,
          'active',
        ]);
        if (activeLease) throw new Error('Não é possível excluir inquilino com contrato ativo. Encerre o contrato primeiro.');
        await execute('DELETE FROM tenants WHERE id = ?', [tenantId]);
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
        await execute('INSERT INTO properties (id, name, address, city, state, zip) VALUES (?, ?, ?, ?, ?, ?)', [
          id,
          name,
          address,
          city,
          state,
          zip || null,
        ]);
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
        await execute("UPDATE properties SET name = ?, address = ?, city = ?, state = ?, zip = ?, updated_at = datetime('now') WHERE id = ?", [
          name,
          address,
          city,
          state,
          zip || null,
          id,
        ]);
        break;
      }
      case 'property.delete': {
        await execute('DELETE FROM properties WHERE id = ?', [String(formData['id'])]);
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
        if (!tenantId || !propertyId || !startDate || !endDate || !Number.isInteger(dueDay) || dueDay < 1 || dueDay > 31 || !Number.isFinite(rentAmount)) {
          throw new Error('Preencha todos os campos do contrato.');
        }
        await execute('INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount) VALUES (?, ?, ?, ?, ?, ?, ?)', [
          id,
          tenantId,
          propertyId,
          startDate,
          endDate,
          dueDay,
          rentAmount,
        ]);
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
        if (!id || !tenantId || !propertyId || !startDate || !endDate || !Number.isInteger(dueDay) || dueDay < 1 || dueDay > 31 || !Number.isFinite(rentAmount)) {
          throw new Error('Preencha todos os campos do contrato.');
        }
        await execute("UPDATE leases SET tenant_id = ?, property_id = ?, start_date = ?, end_date = ?, due_day = ?, rent_amount = ?, updated_at = datetime('now') WHERE id = ?", [
          tenantId,
          propertyId,
          startDate,
          endDate,
          dueDay,
          rentAmount,
          id,
        ]);
        break;
      }
      case 'lease.terminate': {
        await execute("UPDATE leases SET status = 'terminated', updated_at = datetime('now') WHERE id = ?", [String(formData['id'])]);
        break;
      }
      case 'lease.reactivate': {
        await execute("UPDATE leases SET status = 'active', updated_at = datetime('now') WHERE id = ?", [String(formData['id'])]);
        break;
      }
      case 'lease.delete': {
        await execute('DELETE FROM leases WHERE id = ?', [String(formData['id'])]);
        break;
      }
      case 'receipt.create':
      case 'receipt.update': {
        const id = String(formData['id'] || createId());
        const leaseId = String(formData['lease_id'] || '');
        const dueDate = String(formData['due_date'] || '');
        const paidAt = String(formData['paid_at'] || '');
        const amount = Number(formData['amount']);
        if (!leaseId || !dueDate || !Number.isFinite(amount)) throw new Error('Preencha contrato, vencimento e valor.');

        const lease = await queryOne<{ tenant_name: string; property_name: string }>(
          `SELECT t.name AS tenant_name, p.name AS property_name
           FROM leases l
           JOIN tenants t ON t.id = l.tenant_id
           JOIN properties p ON p.id = l.property_id
           WHERE l.id = ?`,
          [leaseId]
        );
        if (!lease) throw new Error('Contrato não encontrado.');

        const pdfPath = null;

        if (intent === 'receipt.create') {
          await execute('INSERT INTO receipts (id, lease_id, due_date, paid_at, amount, pdf_path) VALUES (?, ?, ?, ?, ?, ?)', [
            id,
            leaseId,
            dueDate,
            paidAt || null,
            amount,
            pdfPath,
          ]);
        } else {
          await execute('UPDATE receipts SET lease_id = ?, due_date = ?, paid_at = ?, amount = ?, pdf_path = ? WHERE id = ?', [
            leaseId,
            dueDate,
            paidAt || null,
            amount,
            pdfPath,
            id,
          ]);
        }
        break;
      }
      case 'receipt.generate-monthly': {
        const now = new Date();
        const year = now.getFullYear();
        const month = now.getMonth() + 1;
        const monthKey = `${year}-${String(month).padStart(2, '0')}`;
        const monthStart = `${monthKey}-01`;
        const monthEnd = `${monthKey}-${String(getDaysInMonth(year, month)).padStart(2, '0')}`;
        const activeLeases = await query<Lease>(
          `SELECT id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, '' AS tenant_name, '' AS property_name, status
           FROM leases
           WHERE status = 'active' AND start_date <= ? AND end_date >= ?`,
          [monthEnd, monthStart]
        );

        for (const lease of activeLeases) {
          const dueDay = Math.min(Number(lease.due_day) || 1, getDaysInMonth(year, month));
          const dueDate = `${monthKey}-${String(dueDay).padStart(2, '0')}`;
          await execute(
            'INSERT OR IGNORE INTO receipts (id, lease_id, due_date, paid_at, amount, pdf_path, generated_key) VALUES (?, ?, ?, NULL, ?, NULL, ?)',
            [createId(), lease.id, dueDate, lease.rent_amount, `${lease.id}:${monthKey}`]
          );
        }
        break;
      }
      case 'receipt.delete': {
        await execute('DELETE FROM receipts WHERE id = ?', [String(formData['id'])]);
        break;
      }
      case 'receipt.batch-pay': {
        const ids = (formData['ids'] || '').toString().split(',').filter(Boolean);
        if (!ids.length) throw new Error('Nenhum recibo selecionado.');
        const now = new Date();
        const paidAt = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}T${String(
          now.getHours()
        ).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`;
        for (const receiptId of ids) {
          await execute("UPDATE receipts SET paid_at = ?, updated_at = datetime('now') WHERE id = ? AND paid_at IS NULL", [
            paidAt,
            receiptId,
          ]);
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

  const user = await queryOne<{ username: string }>('SELECT username FROM users WHERE id = ?', [userId]);
  const lanIp = getLocalIpAddress();
  const currentPort = process.env.PORT || '3000';
  const settingsProps = {
    landlordName: (await getSetting('landlord_name')) || 'Fernando Passos',
    landlordCpf: (await getSetting('landlord_cpf')) || '075.898.017-55',
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

  const user = await queryOne<{ username: string }>('SELECT username FROM users WHERE id = ?', [userId]);
  const lanIp = getLocalIpAddress();
  const currentPort = process.env.PORT || '3000';
  const detectedLanUrl = lanIp ? `http://${lanIp}:${currentPort}` : 'http://localhost:3000';

  let landlordName = (await getSetting('landlord_name')) || 'Fernando Passos';
  let landlordCpf = (await getSetting('landlord_cpf')) || '075.898.017-55';
  let landlordEmail = (await getSetting('landlord_email')) || '';
  let appUrl = (await getSetting('app_url')) || '';

  try {
    if (intent === 'save-landlord') {
      const name = String(formData['name'] || '').trim();
      const cpf = String(formData['cpf'] || '').trim();
      const email = String(formData['email'] || '').trim();
      if (!name) throw new Error('Nome do locador é obrigatório.');
      await setSetting('landlord_name', name);
      await setSetting('landlord_cpf', cpf);
      await setSetting('landlord_email', email);
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

// Exportar Backup JSON
app.get('/export', async (c) => {
  const userId = requireUserId(c);
  if (!userId) return c.redirect('/login');

  const [tenants, properties, leases, receipts] = await Promise.all([
    query('SELECT * FROM tenants'),
    query('SELECT * FROM properties'),
    query('SELECT * FROM leases'),
    query('SELECT id, lease_id, due_date, paid_at, amount, created_at FROM receipts'),
  ]);

  const data = {
    exportedAt: new Date().toISOString(),
    tenants,
    properties,
    leases,
    receipts,
  };

  const filename = `aluxen-backup-${new Date().toISOString().split('T')[0]}.json`;
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
      await execute(
        'INSERT INTO tenants (id, name, email, phone, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)',
        [newId, t.name, t.email, t.phone, t.created_at, t.updated_at]
      );
    }

    for (const p of data.properties) {
      const newId = createId();
      idMap.properties.set(p.id, newId);
      await execute(
        'INSERT INTO properties (id, name, address, city, state, zip, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)',
        [newId, p.name, p.address, p.city, p.state, p.zip, p.created_at, p.updated_at]
      );
    }

    for (const l of data.leases) {
      const newId = createId();
      idMap.leases.set(l.id, newId);
      await execute(
        'INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)',
        [
          newId,
          idMap.tenants.get(l.tenant_id) || l.tenant_id,
          idMap.properties.get(l.property_id) || l.property_id,
          l.start_date,
          l.end_date,
          l.due_day ?? 1,
          l.rent_amount,
          l.created_at,
          l.updated_at,
        ]
      );
    }

    for (const r of data.receipts) {
      const newId = createId();
      await execute(
        'INSERT INTO receipts (id, lease_id, due_date, paid_at, amount, pdf_path, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)',
        [newId, idMap.leases.get(r.lease_id) || r.lease_id, r.due_date, r.paid_at, r.amount, r.pdf_path, r.created_at]
      );
    }
  } catch (error) {
    return c.text(error instanceof Error ? error.message : 'Erro ao importar dados.', 400);
  }

  return c.redirect('/dashboard?tab=tenants&toast=importado');
});

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

  // Permite acesso se o usuário estiver logado (administrador) OU se possuir token de autenticação válido (inquilino)
  if (!userId && !isTokenValid) {
    return c.redirect('/login');
  }

  const receipt = await queryOne<{
    id: string;
    due_date: string;
    paid_at: string | null;
    amount: number;
    tenant_name: string;
    property_name: string;
    property_address: string;
    property_city: string;
    property_state: string;
  }>(
    `SELECT r.id, r.due_date, r.paid_at, r.amount,
            t.name AS tenant_name, p.name AS property_name,
            p.address AS property_address, p.city AS property_city, p.state AS property_state
     FROM receipts r
     JOIN leases l ON l.id = r.lease_id
     JOIN tenants t ON t.id = l.tenant_id
     JOIN properties p ON p.id = l.property_id
     WHERE r.id = ?`,
    [id]
  );

  if (!receipt) {
    return c.text('Recibo não encontrado', 404);
  }

  const propertyAddress = [receipt.property_address, receipt.property_city, receipt.property_state].filter(Boolean).join(', ');

  const pdfBytes = await createReceiptPdfBuffer({
    receiptId: receipt.id,
    tenantName: receipt.tenant_name,
    propertyName: receipt.property_name,
    propertyAddress,
    dueDate: receipt.due_date,
    paidAt: receipt.paid_at,
    amount: receipt.amount,
    signature: showSignature,
    baseUrl: await resolvePublicBaseUrl(c),
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

  // Permite acesso se o usuário estiver logado (administrador) OU se possuir token de autenticação válido (inquilino)
  if (!userId && !isTokenValid) {
    return c.redirect('/login');
  }

  const lease = await queryOne<{
    id: string;
    start_date: string;
    end_date: string;
    due_day: number;
    rent_amount: number;
    tenant_name: string;
    tenant_phone: string | null;
    tenant_cpf: string | null;
    property_name: string;
    property_address: string;
    property_city: string;
    property_state: string;
    property_zip: string | null;
  }>(
    `SELECT l.id, l.start_date, l.end_date, l.due_day, l.rent_amount,
            t.name AS tenant_name, t.phone AS tenant_phone, t.cpf AS tenant_cpf,
            p.name AS property_name, p.address AS property_address,
            p.city AS property_city, p.state AS property_state, p.zip AS property_zip
     FROM leases l
     JOIN tenants t ON t.id = l.tenant_id
     JOIN properties p ON p.id = l.property_id
     WHERE l.id = ?`,
    [id]
  );

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
    baseUrl: await resolvePublicBaseUrl(c),
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

  const receipt = await queryOne<{
    id: string;
    due_date: string;
    paid_at: string | null;
    amount: number;
    tenant_name: string;
    tenant_cpf: string | null;
    property_name: string;
    property_address: string;
    property_city: string;
    property_state: string;
  }>(
    `SELECT r.id, r.due_date, r.paid_at, r.amount,
            t.name AS tenant_name, t.cpf AS tenant_cpf,
            p.name AS property_name, p.address AS property_address,
            p.city AS property_city, p.state AS property_state
     FROM receipts r
     JOIN leases l ON l.id = r.lease_id
     JOIN tenants t ON t.id = l.tenant_id
     JOIN properties p ON p.id = l.property_id
     WHERE r.id = ?`,
    [id]
  );

  if (!receipt) {
    const html = renderHtml(
      'Aluxen | Autenticidade do Recibo',
      'verify',
      {},
      <VerificationPage valid={false} error="Recibo não localizado na base oficial de registros." verifiedAt={verifiedAt} />
    );
    return c.html(html, 404);
  }

  const landlordName = (await getSetting('landlord_name')) || 'Fernando Henrique Passos de Souza';
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

  const lease = await queryOne<{
    id: string;
    start_date: string;
    end_date: string;
    due_day: number;
    rent_amount: number;
    status: string;
    tenant_name: string;
    tenant_cpf: string | null;
    property_name: string;
    property_address: string;
    property_city: string;
    property_state: string;
  }>(
    `SELECT l.id, l.start_date, l.end_date, l.due_day, l.rent_amount, l.status,
            t.name AS tenant_name, t.cpf AS tenant_cpf,
            p.name AS property_name, p.address AS property_address,
            p.city AS property_city, p.state AS property_state
     FROM leases l
     JOIN tenants t ON t.id = l.tenant_id
     JOIN properties p ON p.id = l.property_id
     WHERE l.id = ?`,
    [id]
  );

  if (!lease) {
    const html = renderHtml(
      'Aluxen | Autenticidade do Contrato',
      'verify',
      {},
      <VerificationPage valid={false} error="Contrato não localizado na base oficial de registros." verifiedAt={verifiedAt} />
    );
    return c.html(html, 404);
  }

  const landlordName = (await getSetting('landlord_name')) || 'Fernando Henrique Passos de Souza';
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

  await whatsappLoginManager.logout();
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
  const landlordName = (await getSetting('landlord_name')) || 'Fernando Henrique Passos de Souza';

  if (type === 'receipt') {
    const receipt = await queryOne<{
      id: string;
      due_date: string;
      paid_at: string | null;
      amount: number;
      tenant_name: string;
      tenant_phone: string | null;
      property_name: string;
      property_address: string;
      property_city: string;
      property_state: string;
    }>(
      `SELECT r.id, r.due_date, r.paid_at, r.amount,
              t.name AS tenant_name, t.phone AS tenant_phone,
              p.name AS property_name, p.address AS property_address,
              p.city AS property_city, p.state AS property_state
       FROM receipts r
       JOIN leases l ON l.id = r.lease_id
       JOIN tenants t ON t.id = l.tenant_id
       JOIN properties p ON p.id = l.property_id
       WHERE r.id = ?`,
      [id]
    );

    if (!receipt) {
      return c.json({ error: 'Recibo não encontrado.' }, 404);
    }

    const phone = receipt.tenant_phone || '';
    const token = generateAuthToken('receipt', receipt.id);
    const validationUrl = await getValidationUrl('receipt', receipt.id, token, baseUrl);
    const pdfUrl = `${baseUrl}/pdf?id=${receipt.id}&token=${token}`;
    const fullPropertyAddress = [receipt.property_address, receipt.property_city, receipt.property_state]
      .filter(Boolean)
      .join(', ');

    const message = buildReceiptWhatsAppMessage({
      tenantName: receipt.tenant_name,
      propertyName: receipt.property_name,
      propertyAddress: fullPropertyAddress,
      dueDate: formatDateBr(receipt.due_date),
      amount: formatCurrency(receipt.amount),
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
  const lease = await queryOne<{
    id: string;
    start_date: string;
    end_date: string;
    due_day: number;
    rent_amount: number;
    status: string;
    tenant_name: string;
    tenant_phone: string | null;
    property_name: string;
    property_address: string;
    property_city: string;
    property_state: string;
  }>(
    `SELECT l.id, l.start_date, l.end_date, l.due_day, l.rent_amount, l.status,
            t.name AS tenant_name, t.phone AS tenant_phone,
            p.name AS property_name, p.address AS property_address,
            p.city AS property_city, p.state AS property_state
     FROM leases l
     JOIN tenants t ON t.id = l.tenant_id
     JOIN properties p ON p.id = l.property_id
     WHERE l.id = ?`,
    [id]
  );

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

  try {
    const result = await sendWhatsAppMessageHeadless(to, message);
    return c.json({ ok: true, result });
  } catch (error) {
    return c.json(
      {
        ok: false,
        error: error instanceof Error ? error.message : 'Falha ao enviar mensagem via WhatsApp.',
      },
      400
    );
  }
});

const port = Number(process.env.PORT) || 3000;
const lanIp = getLocalIpAddress();
console.log(`🚀 Aluxen rodando localmente em http://localhost:${port}`);
if (lanIp) {
  console.log(`📱 Acesso pelo celular (mesmo Wi-Fi): http://${lanIp}:${port}`);
}
export default {
  port,
  fetch: app.fetch,
};
