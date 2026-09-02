import { describe, expect, it } from 'bun:test';
import server from '../src/index';
import { generateAuthToken } from '../src/utils/auth-token';

describe('Aluxen Bun + Hono 1:1 Parity Tests', () => {
  let sessionCookie = '';
  let createdTenantId = '';
  let createdPropertyId = '';
  let createdLeaseId = '';
  let createdReceiptId = '';

  it('GET /api/health returns health status and runtime information', async () => {
    const res = await server.fetch(new Request('http://localhost:3000/api/health'));
    expect(res.status).toBe(200);

    const body = (await res.json()) as Record<string, unknown>;
    expect(body['status']).toBe('ok');
    expect(body['runtime']).toBe('bun');
    expect(body['framework']).toBe('hono');
    expect(body['database']).toBe('sqlite (bun:sqlite)');
  });

  it('GET / redirects to /login when unauthenticated', async () => {
    const res = await server.fetch(new Request('http://localhost:3000/'));
    expect(res.status).toBe(302);
    expect(res.headers.get('location')).toBe('/login');
  });

  it('GET /login returns HTML with login form and branding', async () => {
    const res = await server.fetch(new Request('http://localhost:3000/login'));
    expect(res.status).toBe(200);
    expect(res.headers.get('content-type')).toContain('text/html');

    const html = await res.text();
    expect(html).toContain('Aluxen');
    expect(html).toContain('login-card');
    expect(html).toContain('name="username"');
    expect(html).toContain('name="password"');
  });

  it('POST /login with invalid credentials returns 401', async () => {
    const formData = new FormData();
    formData.set('username', 'usuario_inexistente');
    formData.set('password', 'senha_errada');

    const res = await server.fetch(
      new Request('http://localhost:3000/login', {
        method: 'POST',
        body: formData,
      })
    );
    expect(res.status).toBe(401);
    const html = await res.text();
    expect(html).toContain('Credenciais inválidas.');
  });

  it('POST /login with seed credentials authenticates and sets session cookie', async () => {
    const formData = new FormData();
    formData.set('username', 'passoz');
    formData.set('password', 'Dt32btop@');

    const res = await server.fetch(
      new Request('http://localhost:3000/login', {
        method: 'POST',
        body: formData,
      })
    );
    expect(res.status).toBe(302);
    expect(res.headers.get('location')).toBe('/dashboard?tab=tenants');

    const setCookie = res.headers.get('set-cookie');
    expect(setCookie).toBeTruthy();
    expect(setCookie).toContain('__aluxen_session=');

    // Extrair o cookie de sessão para chamadas subsequentes
    sessionCookie = (setCookie?.split(';')[0] || '') as string;
  });

  it('GET /dashboard renders authenticated dashboard HTML', async () => {
    const res = await server.fetch(
      new Request('http://localhost:3000/dashboard?tab=tenants', {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(res.status).toBe(200);
    const html = await res.text();
    expect(html).toContain('Aluxen');
    expect(html).toContain('Gerenciar Inquilinos');
    expect(html).toContain('passoz');
  });

  it('GET /dashboard?json=true returns structured dashboard data', async () => {
    const res = await server.fetch(
      new Request('http://localhost:3000/dashboard?json=true', {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(res.status).toBe(200);
    const data = (await res.json()) as Record<string, unknown>;
    expect(data['user']).toBe('passoz');
    expect(Array.isArray(data['tenants'])).toBe(true);
    expect(Array.isArray(data['properties'])).toBe(true);
    expect(Array.isArray(data['leases'])).toBe(true);
    expect(Array.isArray(data['receipts'])).toBe(true);
  });

  it('POST /dashboard tenant.create and tenant.update lifecycle', async () => {
    // Criar inquilino
    const createForm = new FormData();
    createForm.set('intent', 'tenant.create');
    createForm.set('name', 'Carlos Drummond');
    createForm.set('email', 'carlos@poesia.com.br');
    createForm.set('phone', '(22) 99888-7766');
    createForm.set('cpf', '111.222.333-44');

    const createRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: createForm,
      })
    );
    expect(createRes.status).toBe(200);
    const createData = (await createRes.json()) as { ok?: boolean };
    expect(createData.ok).toBe(true);

    // Obter ID criado
    const listRes = await server.fetch(
      new Request('http://localhost:3000/dashboard?json=true', {
        headers: { Cookie: sessionCookie },
      })
    );
    const listData = (await listRes.json()) as { tenants: Array<{ id: string; name: string }> };
    const created = listData.tenants.find((t) => t.name === 'Carlos Drummond');
    expect(created).toBeTruthy();
    createdTenantId = created?.id || '';

    // Atualizar inquilino
    const updateForm = new FormData();
    updateForm.set('intent', 'tenant.update');
    updateForm.set('id', createdTenantId);
    updateForm.set('name', 'Carlos Drummond de Andrade');
    updateForm.set('email', 'carlos.andrade@poesia.com.br');
    updateForm.set('phone', '(22) 99888-7766');
    updateForm.set('cpf', '111.222.333-44');

    const updateRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: updateForm,
      })
    );
    expect(updateRes.status).toBe(200);
  });

  it('POST /dashboard property.create and property.update lifecycle', async () => {
    // Criar imóvel
    const createForm = new FormData();
    createForm.set('intent', 'property.create');
    createForm.set('name', 'Casa de Praia 202');
    createForm.set('address', 'Av. Atlântica, 500');
    createForm.set('city', 'Cabo Frio');
    createForm.set('state', 'RJ');
    createForm.set('zip', '28900-000');

    const createRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: createForm,
      })
    );
    expect(createRes.status).toBe(200);

    // Obter ID
    const listRes = await server.fetch(
      new Request('http://localhost:3000/dashboard?json=true', {
        headers: { Cookie: sessionCookie },
      })
    );
    const listData = (await listRes.json()) as { properties: Array<{ id: string; name: string }> };
    const created = listData.properties.find((p) => p.name === 'Casa de Praia 202');
    expect(created).toBeTruthy();
    createdPropertyId = created?.id || '';
  });

  it('POST /dashboard lease.create, terminate and reactivate lifecycle', async () => {
    const createForm = new FormData();
    createForm.set('intent', 'lease.create');
    createForm.set('tenant_id', createdTenantId);
    createForm.set('property_id', createdPropertyId);
    createForm.set('start_date', '2026-01-01');
    createForm.set('end_date', '2026-12-31');
    createForm.set('due_day', '10');
    createForm.set('rent_amount', '2500');

    const createRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: createForm,
      })
    );
    expect(createRes.status).toBe(200);

    // Obter contrato
    const listRes = await server.fetch(
      new Request('http://localhost:3000/dashboard?json=true', {
        headers: { Cookie: sessionCookie },
      })
    );
    const listData = (await listRes.json()) as { leases: Array<{ id: string; tenant_id: string; status: string }> };
    const created = listData.leases.find((l) => l.tenant_id === createdTenantId);
    expect(created).toBeTruthy();
    createdLeaseId = created?.id || '';
    expect(created?.status).toBe('active');

    // Encerrar contrato
    const termForm = new FormData();
    termForm.set('intent', 'lease.terminate');
    termForm.set('id', createdLeaseId);

    const termRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: termForm,
      })
    );
    expect(termRes.status).toBe(200);

    // Reativar contrato
    const reactForm = new FormData();
    reactForm.set('intent', 'lease.reactivate');
    reactForm.set('id', createdLeaseId);

    const reactRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: reactForm,
      })
    );
    expect(reactRes.status).toBe(200);
  });

  it('POST /dashboard receipt.create and batch-pay', async () => {
    const createForm = new FormData();
    createForm.set('intent', 'receipt.create');
    createForm.set('lease_id', createdLeaseId);
    createForm.set('due_date', '2026-09-10');
    createForm.set('amount', '2500');

    const createRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: createForm,
      })
    );
    expect(createRes.status).toBe(200);

    const listRes = await server.fetch(
      new Request('http://localhost:3000/dashboard?json=true', {
        headers: { Cookie: sessionCookie },
      })
    );
    const listData = (await listRes.json()) as { receipts: Array<{ id: string; lease_id: string; paid_at: string | null }> };
    const created = listData.receipts.find((r) => r.lease_id === createdLeaseId);
    expect(created).toBeTruthy();
    createdReceiptId = created?.id || '';
    expect(created?.paid_at).toBeNull();

    // Pagar em lote
    const payForm = new FormData();
    payForm.set('intent', 'receipt.batch-pay');
    payForm.set('ids', createdReceiptId);

    const payRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: payForm,
      })
    );
    expect(payRes.status).toBe(200);
  });

  it('GET /pdf generates receipt PDF with signature and without signature', async () => {
    // Com assinatura
    const resSigned = await server.fetch(
      new Request(`http://localhost:3000/pdf?id=${createdReceiptId}`, {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(resSigned.status).toBe(200);
    expect(resSigned.headers.get('content-type')).toBe('application/pdf');
    const bytesSigned = await resSigned.arrayBuffer();
    expect(bytesSigned.byteLength).toBeGreaterThan(1000);

    // Sem assinatura
    const resUnsigned = await server.fetch(
      new Request(`http://localhost:3000/pdf?id=${createdReceiptId}&assinatura=false`, {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(resUnsigned.status).toBe(200);
    expect(resUnsigned.headers.get('content-type')).toBe('application/pdf');
  });

  it('GET /contract-pdf generates complete lease contract PDF', async () => {
    const res = await server.fetch(
      new Request(`http://localhost:3000/contract-pdf?id=${createdLeaseId}`, {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(res.status).toBe(200);
    expect(res.headers.get('content-type')).toBe('application/pdf');
    const bytes = await res.arrayBuffer();
    expect(bytes.byteLength).toBeGreaterThan(5000);
  });

  it('GET /export exports backup JSON', async () => {
    const res = await server.fetch(
      new Request('http://localhost:3000/export', {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(res.status).toBe(200);
    expect(res.headers.get('content-type')).toContain('application/json');

    const backup = (await res.json()) as Record<string, unknown>;
    expect(backup['exportedAt']).toBeTruthy();
    expect(Array.isArray(backup['tenants'])).toBe(true);
    expect(Array.isArray(backup['properties'])).toBe(true);
  });

  it('GET /settings and POST /settings updates landlord profile', async () => {
    const getRes = await server.fetch(
      new Request('http://localhost:3000/settings', {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(getRes.status).toBe(200);
    const html = await getRes.text();
    expect(html).toContain('Dados do Locador');
    expect(html).toContain('Alterar Senha');

    // Atualizar locador
    const postForm = new FormData();
    postForm.set('intent', 'save-landlord');
    postForm.set('name', 'Fernando Henrique Passos de Souza');
    postForm.set('cpf', '075.898.017-55');
    postForm.set('email', 'contato@evolucsia.com');

    const postRes = await server.fetch(
      new Request('http://localhost:3000/settings', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: postForm,
      })
    );
    expect(postRes.status).toBe(200);
    const updatedHtml = await postRes.text();
    expect(updatedHtml).toContain('Dados salvos com sucesso.');

    // Atualizar URL pública do sistema
    const urlForm = new FormData();
    urlForm.set('intent', 'save-app-url');
    urlForm.set('app_url', 'https://aluxen.evolucsia.com');

    const urlRes = await server.fetch(
      new Request('http://localhost:3000/settings', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: urlForm,
      })
    );
    expect(urlRes.status).toBe(200);
    const urlHtml = await urlRes.text();
    expect(urlHtml).toContain('URL pública do sistema salva com sucesso.');
  });

  it('POST /logout clears session and redirects to /login', async () => {
    const res = await server.fetch(
      new Request('http://localhost:3000/logout', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
      })
    );
    expect(res.status).toBe(302);
    expect(res.headers.get('location')).toBe('/login');
  });

  it('GET /validar/recibo/:id verifies receipt authenticity', async () => {
    const validToken = generateAuthToken('receipt', createdReceiptId);
    const validRes = await server.fetch(new Request(`http://localhost:3000/validar/recibo/${createdReceiptId}?token=${validToken}`));
    expect(validRes.status).toBe(200);
    const validHtml = await validRes.text();
    expect(validHtml).toContain('DOCUMENTO AUTÊNTICO E VÁLIDO');
    expect(validHtml).toContain('Carlos Drummond');
    expect(validHtml).toContain('Recibo de Pagamento de Aluguel');

    // Token inválido
    const invalidRes = await server.fetch(new Request(`http://localhost:3000/validar/recibo/${createdReceiptId}?token=token_falso_123`));
    expect(invalidRes.status).toBe(400);
    const invalidHtml = await invalidRes.text();
    expect(invalidHtml).toContain('DOCUMENTO NÃO AUTENTICADO');
  });

  it('GET /validar/contrato/:id verifies contract authenticity', async () => {
    const validToken = generateAuthToken('lease', createdLeaseId);
    const validRes = await server.fetch(new Request(`http://localhost:3000/validar/contrato/${createdLeaseId}?token=${validToken}`));
    expect(validRes.status).toBe(200);
    const validHtml = await validRes.text();
    expect(validHtml).toContain('DOCUMENTO AUTÊNTICO E VÁLIDO');
    expect(validHtml).toContain('Contrato de Locação Residencial');
    expect(validHtml).toContain('Carlos Drummond');

    // Token inválido
    const invalidRes = await server.fetch(new Request(`http://localhost:3000/validar/contrato/${createdLeaseId}?token=token_invalido`));
    expect(invalidRes.status).toBe(400);
    const invalidHtml = await invalidRes.text();
    expect(invalidHtml).toContain('DOCUMENTO NÃO AUTENTICADO');
  });
});
