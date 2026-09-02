import { Hono } from 'hono';
import { logger } from 'hono/logger';
import { count, desc, eq } from 'drizzle-orm';
import { db } from './db';
import { leases, properties, receipts, tenants } from './db/schema';
import { runMigrations } from './db/migrate';
import { Home } from './views/home';
import { TenantsPage } from './views/tenants';
import { PropertiesPage } from './views/properties';
import { LeasesPage } from './views/leases';
import { ReceiptsPage } from './views/receipts';

// Executar migrações do SQLite na inicialização
runMigrations();

const app = new Hono();

app.use('*', logger());

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

// Dashboard / Home
app.get('/', async (c) => {
  const [
    tenantsCountRes,
    propertiesCountRes,
    leasesCountRes,
    receiptsCountRes,
    recentTenants,
    recentProperties,
  ] = await Promise.all([
    db.select({ value: count() }).from(tenants),
    db.select({ value: count() }).from(properties),
    db.select({ value: count() }).from(leases).where(eq(leases.status, 'active')),
    db.select({ value: count() }).from(receipts),
    db.select().from(tenants).orderBy(desc(tenants.createdAt)).limit(5),
    db.select().from(properties).orderBy(desc(properties.createdAt)).limit(5),
  ]);

  return c.html(
    <Home
      stats={{
        tenantsCount: tenantsCountRes[0]?.value ?? 0,
        propertiesCount: propertiesCountRes[0]?.value ?? 0,
        activeLeasesCount: leasesCountRes[0]?.value ?? 0,
        receiptsCount: receiptsCountRes[0]?.value ?? 0,
      }}
      recentTenants={recentTenants}
      recentProperties={recentProperties}
    />
  );
});

// --- Inquilinos ---
app.get('/tenants', async (c) => {
  const allTenants = await db.select().from(tenants).orderBy(desc(tenants.createdAt));
  return c.html(<TenantsPage tenants={allTenants} />);
});

app.post('/tenants', async (c) => {
  const body = await c.req.parseBody();
  const name = String(body['name'] || '').trim();
  const email = String(body['email'] || '').trim();
  const phone = body['phone'] ? String(body['phone']).trim() : null;
  const cpf = body['cpf'] ? String(body['cpf']).trim() : null;

  if (!name || !email) {
    const allTenants = await db.select().from(tenants).orderBy(desc(tenants.createdAt));
    return c.html(<TenantsPage tenants={allTenants} error="Nome e e-mail são obrigatórios." />, 400);
  }

  const id = crypto.randomUUID();
  await db.insert(tenants).values({
    id,
    name,
    email,
    phone,
    cpf,
  });

  return c.redirect('/tenants');
});

app.post('/tenants/:id/delete', async (c) => {
  const id = c.req.param('id');
  await db.delete(tenants).where(eq(tenants.id, id));
  return c.redirect('/tenants');
});

// --- Imóveis ---
app.get('/properties', async (c) => {
  const allProperties = await db.select().from(properties).orderBy(desc(properties.createdAt));
  return c.html(<PropertiesPage properties={allProperties} />);
});

app.post('/properties', async (c) => {
  const body = await c.req.parseBody();
  const name = String(body['name'] || '').trim();
  const address = String(body['address'] || '').trim();
  const city = String(body['city'] || '').trim();
  const state = String(body['state'] || '').trim().toUpperCase();
  const zip = body['zip'] ? String(body['zip']).trim() : null;

  if (!name || !address || !city || !state) {
    const allProperties = await db.select().from(properties).orderBy(desc(properties.createdAt));
    return c.html(<PropertiesPage properties={allProperties} error="Preencha os campos obrigatórios." />, 400);
  }

  const id = crypto.randomUUID();
  await db.insert(properties).values({
    id,
    name,
    address,
    city,
    state,
    zip,
  });

  return c.redirect('/properties');
});

app.post('/properties/:id/delete', async (c) => {
  const id = c.req.param('id');
  await db.delete(properties).where(eq(properties.id, id));
  return c.redirect('/properties');
});

// --- Locações ---
app.get('/leases', async (c) => {
  const [allLeases, allTenants, allProperties] = await Promise.all([
    db.query.leases.findMany({
      with: {
        tenant: true,
        property: true,
      },
      orderBy: [desc(leases.createdAt)],
    }),
    db.select().from(tenants).orderBy(tenants.name),
    db.select().from(properties).orderBy(properties.name),
  ]);

  return c.html(
    <LeasesPage
      leases={allLeases}
      tenants={allTenants}
      properties={allProperties}
    />
  );
});

app.post('/leases', async (c) => {
  const body = await c.req.parseBody();
  const tenantId = String(body['tenantId'] || '').trim();
  const propertyId = String(body['propertyId'] || '').trim();
  const startDate = String(body['startDate'] || '').trim();
  const endDate = String(body['endDate'] || '').trim();
  const dueDay = parseInt(String(body['dueDay'] || '5'), 10);
  const rentAmount = parseFloat(String(body['rentAmount'] || '0'));

  if (!tenantId || !propertyId || !startDate || !endDate || isNaN(rentAmount)) {
    return c.redirect('/leases');
  }

  const id = crypto.randomUUID();
  await db.insert(leases).values({
    id,
    tenantId,
    propertyId,
    startDate,
    endDate,
    dueDay,
    rentAmount,
    status: 'active',
  });

  return c.redirect('/leases');
});

app.post('/leases/:id/delete', async (c) => {
  const id = c.req.param('id');
  await db.delete(leases).where(eq(leases.id, id));
  return c.redirect('/leases');
});

// --- Recibos ---
app.get('/receipts', async (c) => {
  const [allReceipts, activeLeases] = await Promise.all([
    db.query.receipts.findMany({
      with: {
        lease: {
          with: {
            tenant: true,
            property: true,
          },
        },
      },
      orderBy: [desc(receipts.dueDate)],
    }),
    db.query.leases.findMany({
      where: eq(leases.status, 'active'),
      with: {
        tenant: true,
        property: true,
      },
    }),
  ]);

  return c.html(
    <ReceiptsPage
      receipts={allReceipts}
      activeLeases={activeLeases}
    />
  );
});

app.post('/receipts', async (c) => {
  const body = await c.req.parseBody();
  const leaseId = String(body['leaseId'] || '').trim();
  const dueDate = String(body['dueDate'] || '').trim();
  const amount = parseFloat(String(body['amount'] || '0'));

  if (!leaseId || !dueDate || isNaN(amount)) {
    return c.redirect('/receipts');
  }

  const id = crypto.randomUUID();
  await db.insert(receipts).values({
    id,
    leaseId,
    dueDate,
    amount,
  });

  return c.redirect('/receipts');
});

app.post('/receipts/:id/pay', async (c) => {
  const id = c.req.param('id');
  const today = new Date().toISOString().split('T')[0]!;
  await db.update(receipts).set({ paidAt: today }).where(eq(receipts.id, id));
  return c.redirect('/receipts');
});

app.post('/receipts/:id/delete', async (c) => {
  const id = c.req.param('id');
  await db.delete(receipts).where(eq(receipts.id, id));
  return c.redirect('/receipts');
});

const port = Number(process.env.PORT) || 3000;
console.log(`🚀 Servidor rodando em http://localhost:${port}`);

export default {
  port,
  fetch: app.fetch,
};
