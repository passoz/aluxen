import { describe, expect, it } from 'bun:test';
import type { Receipt } from '../src/types/dashboard';
import { inflateSync } from 'node:zlib';
import server from '../src/index';
import { generateAuthToken } from '../src/utils/auth-token';
import { getReceiptStatus, getReceiptStatusLabel } from '../src/utils/format';

function extractPdfText(bytes: ArrayBuffer): string {
  const binary = Buffer.from(bytes);
  const streamRegex = /stream[\r\n]+([\s\S]*?)[\r\n]+endstream/g;
  let match: RegExpExecArray | null;
  let text = '';
  while ((match = streamRegex.exec(binary.toString('binary'))) !== null) {
    const chunk = match[1];
    if (!chunk) continue;
    try {
      const buffer = Buffer.from(chunk, 'binary');
      const uncompressed = inflateSync(buffer).toString('latin1');
      const hexRegex = /<([0-9A-Fa-f]+)>/g;
      let mHex: RegExpExecArray | null;
      while ((mHex = hexRegex.exec(uncompressed)) !== null) {
        const hexVal = mHex[1];
        if (hexVal) {
          text += ' ' + Buffer.from(hexVal, 'hex').toString('latin1');
        }
      }
      const parenRegex = /\(([^)]+)\)/g;
      let mParen: RegExpExecArray | null;
      while ((mParen = parenRegex.exec(uncompressed)) !== null) {
        if (mParen[1]) {
          text += ' ' + mParen[1];
        }
      }
    } catch {}
  }
  return text;
}

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
    formData.set('password', 'senha-de-teste');

    const res = await server.fetch(
      new Request('http://localhost:3000/login', {
        method: 'POST',
        body: formData,
      })
    );
    expect(res.status).toBe(302);
    expect(res.headers.get('location')).toBe('/dashboard?tab=dashboard');

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

  it('POST /dashboard lease.create supports suspended status and enforces season overlap rule', async () => {
    // Tenant e imóvel dedicados
    const tenantForm = new FormData();
    tenantForm.set('intent', 'tenant.create');
    tenantForm.set('id', 'tenant-status-test');
    tenantForm.set('name', 'Tenant Status Teste');
    tenantForm.set('email', 'status@teste.local');
    const tenantRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: tenantForm,
      })
    );
    expect(tenantRes.status).toBe(200);

    const propForm = new FormData();
    propForm.set('intent', 'property.create');
    propForm.set('id', 'property-status-test');
    propForm.set('name', 'Imóvel Status Teste');
    propForm.set('address', 'Rua Teste 1');
    propForm.set('city', 'Niterói');
    propForm.set('state', 'RJ');
    propForm.set('zip', '24000-000');
    const propRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: propForm,
      })
    );
    expect(propRes.status).toBe(200);

    const postLease = async (id: string, status: string, start: string, end: string) => {
      const fd = new FormData();
      fd.set('intent', 'lease.create');
      fd.set('id', id);
      fd.set('tenant_id', 'tenant-status-test');
      fd.set('property_id', 'property-status-test');
      fd.set('start_date', start);
      fd.set('end_date', end);
      fd.set('due_day', '10');
      fd.set('rent_amount', '1500');
      fd.set('status', status);
      return server.fetch(
        new Request('http://localhost:3000/dashboard', {
          method: 'POST',
          headers: { Cookie: sessionCookie },
          body: fd,
        })
      );
    };

    // 1. Contrato suspenso: não bloqueia nada e é criado com sucesso
    const suspRes = await postLease('lease-status-susp', 'suspended', '2026-01-01', '2026-12-31');
    expect(suspRes.status).toBe(200);

    // 2. Temporada sobre imóvel com contrato suspenso -> permitida
    const seasonOkRes = await postLease('lease-status-season', 'season', '2026-06-01', '2026-06-20');
    expect(seasonOkRes.status).toBe(200);

    // 3. Nova temporada sobrepondo a anterior -> bloqueada (400)
    const seasonOverlapRes = await postLease('lease-status-season2', 'season', '2026-06-10', '2026-06-25');
    expect(seasonOverlapRes.status).toBe(400);
    const overlapJson = (await seasonOverlapRes.json()) as { error: string };
    expect(overlapJson.error).toContain('Temporada só pode ser criada');

    // 4. Status inválido -> bloqueado
    const invalidRes = await postLease('lease-status-invalid', 'invalid-status', '2026-06-10', '2026-06-25');
    expect(invalidRes.status).toBe(400);

    // 5. Reativar o suspenso (vira ativo) e confirmar que ativo não conflita
    const reactForm = new FormData();
    reactForm.set('intent', 'lease.reactivate');
    reactForm.set('id', 'lease-status-susp');
    const reactRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: reactForm,
      })
    );
    expect(reactRes.status).toBe(200);

    // Cleanup
    const del = async (intent: string, id: string) => {
      const fd = new FormData();
      fd.set('intent', intent);
      fd.set('id', id);
      await server.fetch(
        new Request('http://localhost:3000/dashboard', {
          method: 'POST',
          headers: { Cookie: sessionCookie },
          body: fd,
        })
      );
    };
    await del('lease.delete', 'lease-status-susp');
    await del('lease.delete', 'lease-status-season');
    await del('property.delete', 'property-status-test');
    await del('tenant.delete', 'tenant-status-test');
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

    // Valida que o nome do imóvel aparece na declaração do recibo

    const receiptText = extractPdfText(bytesSigned);
    expect(receiptText).toContain('Casa de Praia 202');
    expect(receiptText).toContain('referente ao aluguel do imóvel Casa de Praia 202 na');
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


    const contractText = extractPdfText(bytes);
    expect(contractText).toContain('Casa de Praia 202');
    expect(contractText).toContain('o imóvel Casa de Praia 202 de propriedade do LOCADOR, situado');
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
    postForm.set('name', 'Locador Teste');
    postForm.set('cpf', '000.000.000-00');
    postForm.set('email', 'locador@teste.local');

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

  it('GET /api/whatsapp/status returns profile status', async () => {
    const res = await server.fetch(
      new Request('http://localhost:3000/api/whatsapp/status', {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(res.status).toBe(200);
    const data = (await res.json()) as Record<string, unknown>;
    expect(typeof data['profileDir']).toBe('string');
  });

  it('POST /api/whatsapp/preview generates receipt and contract message with wa.me link', async () => {
    // Preview de Recibo
    const receiptForm = new FormData();
    receiptForm.set('type', 'receipt');
    receiptForm.set('id', createdReceiptId);

    const resReceipt = await server.fetch(
      new Request('http://localhost:3000/api/whatsapp/preview', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: receiptForm,
      })
    );
    expect(resReceipt.status).toBe(200);
    const dataReceipt = (await resReceipt.json()) as Record<string, unknown>;
    expect(dataReceipt['ok']).toBe(true);
    expect(String(dataReceipt['message'])).toContain('Recibo de Aluguel');
    expect(String(dataReceipt['message'])).toContain('Carlos Drummond');
    expect(String(dataReceipt['directUrl'])).toContain('https://wa.me/55');

    // Preview de Contrato
    const leaseForm = new FormData();
    leaseForm.set('type', 'lease');
    leaseForm.set('id', createdLeaseId);

    const resLease = await server.fetch(
      new Request('http://localhost:3000/api/whatsapp/preview', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: leaseForm,
      })
    );
    expect(resLease.status).toBe(200);
    const dataLease = (await resLease.json()) as Record<string, unknown>;
    expect(dataLease['ok']).toBe(true);
    expect(String(dataLease['message'])).toContain('Contrato de Locação Residencial');
    expect(String(dataLease['directUrl'])).toContain('https://wa.me/55');
  });

  it('POST /api/whatsapp/send validates parameters', async () => {
    const emptyForm = new FormData();
    const res = await server.fetch(
      new Request('http://localhost:3000/api/whatsapp/send', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: emptyForm,
      })
    );
    expect(res.status).toBe(400);
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

  it('GET /pdf and GET /contract-pdf allow public access with valid token without login', async () => {
    // 1. Acesso sem login e sem token redireciona para /login
    const unauthReceipt = await server.fetch(new Request(`http://localhost:3000/pdf?id=${createdReceiptId}`));
    expect(unauthReceipt.status).toBe(302);
    expect(unauthReceipt.headers.get('location')).toBe('/login');

    const unauthContract = await server.fetch(new Request(`http://localhost:3000/contract-pdf?id=${createdLeaseId}`));
    expect(unauthContract.status).toBe(302);
    expect(unauthContract.headers.get('location')).toBe('/login');

    // 2. Acesso sem login mas COM token válido retorna 200 PDF para o inquilino
    const receiptToken = generateAuthToken('receipt', createdReceiptId);
    const publicReceiptRes = await server.fetch(new Request(`http://localhost:3000/pdf?id=${createdReceiptId}&token=${receiptToken}`));
    expect(publicReceiptRes.status).toBe(200);
    expect(publicReceiptRes.headers.get('content-type')).toBe('application/pdf');

    const leaseToken = generateAuthToken('lease', createdLeaseId);
    const publicContractRes = await server.fetch(new Request(`http://localhost:3000/contract-pdf?id=${createdLeaseId}&token=${leaseToken}`));
    expect(publicContractRes.status).toBe(200);
    expect(publicContractRes.headers.get('content-type')).toBe('application/pdf');
  });

  it('GET /api/whatsapp/login/status and POST /api/whatsapp/login/cancel lifecycle', async () => {
    const statusRes = await server.fetch(
      new Request('http://localhost:3000/api/whatsapp/login/status', {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(statusRes.status).toBe(200);
    const statusData = (await statusRes.json()) as { ok: boolean; state: { status: string } };
    expect(statusData.ok).toBe(true);
    expect(['idle', 'starting', 'scan_qr', 'error']).toContain(statusData.state.status);

    const cancelRes = await server.fetch(
      new Request('http://localhost:3000/api/whatsapp/login/cancel', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
      })
    );
    expect(cancelRes.status).toBe(200);
    const cancelData = (await cancelRes.json()) as { ok: boolean; state: { status: string } };
    expect(cancelData.ok).toBe(true);
    expect(cancelData.state.status).toBe('idle');
  });

  it('POST /api/whatsapp/logout resets profile and returns ok', async () => {
    const res = await server.fetch(
      new Request('http://localhost:3000/api/whatsapp/logout', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
      })
    );
    expect(res.status).toBe(200);
    const data = (await res.json()) as { ok: boolean; message: string };
    expect(data.ok).toBe(true);
    expect(data.message).toContain('desconectado');
  });

  it('supports partial payments with status partial and balance calculation', async () => {
    // 1. Criar recibo de aluguel no valor de 1500
    const createForm = new FormData();
    createForm.set('intent', 'receipt.create');
    createForm.set('lease_id', createdLeaseId);
    createForm.set('due_date', '2026-10-10');
    createForm.set('amount', '1500');
    createForm.set('category', 'rent');

    const createRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: createForm,
      })
    );
    expect(createRes.status).toBe(200);

    const dashRes = await server.fetch(
      new Request('http://localhost:3000/dashboard?json=true', {
        headers: { Cookie: sessionCookie },
      })
    );
    const dashData = (await dashRes.json()) as { receipts: Receipt[] };
    const receipt = dashData.receipts.find((r) => r.due_date === '2026-10-10' && r.lease_id === createdLeaseId);
    expect(receipt).toBeDefined();
    if (!receipt) throw new Error('Receipt not found');
    expect(getReceiptStatus(receipt)).toBe('due');

    // 2. Lançar pagamento parcial de 600
    const payForm = new FormData();
    payForm.set('intent', 'receipt.pay');
    payForm.set('id', receipt.id);
    payForm.set('pay_amount', '600');

    const payRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: payForm,
      })
    );
    expect(payRes.status).toBe(200);

    const afterPartialRes = await server.fetch(
      new Request('http://localhost:3000/dashboard?json=true', {
        headers: { Cookie: sessionCookie },
      })
    );
    const afterPartialData = (await afterPartialRes.json()) as { receipts: Receipt[] };
    const partialReceipt = afterPartialData.receipts.find((r) => r.id === receipt?.id);
    expect(partialReceipt).toBeDefined();
    if (!partialReceipt) throw new Error('Partial receipt not found');
    expect(Number(partialReceipt.paid_amount)).toBe(600);
    expect(getReceiptStatus(partialReceipt)).toBe('partial');
    expect(getReceiptStatusLabel(getReceiptStatus(partialReceipt))).toBe('Parcial');

    // 3. Verificar PDF do pagamento parcial
    const pdfRes = await server.fetch(
      new Request(`http://localhost:3000/pdf?id=${receipt.id}`, {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(pdfRes.status).toBe(200);
    expect(pdfRes.headers.get('content-type')).toBe('application/pdf');

    // 4. Completar pagamento para 1500
    const fullPayForm = new FormData();
    fullPayForm.set('intent', 'receipt.pay');
    fullPayForm.set('id', receipt.id);
    fullPayForm.set('pay_amount', '1500');

    const fullPayRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: fullPayForm,
      })
    );
    expect(fullPayRes.status).toBe(200);

    const afterFullRes = await server.fetch(
      new Request('http://localhost:3000/dashboard?json=true', {
        headers: { Cookie: sessionCookie },
      })
    );
    const afterFullData = (await afterFullRes.json()) as { receipts: Receipt[] };
    const fullReceipt = afterFullData.receipts.find((r) => r.id === receipt?.id);
    expect(fullReceipt).toBeDefined();
    if (!fullReceipt) throw new Error('Full receipt not found');
    expect(Number(fullReceipt.paid_amount)).toBe(1500);
    expect(getReceiptStatus(fullReceipt)).toBe('paid');
    expect(getReceiptStatusLabel(getReceiptStatus(fullReceipt))).toBe('Pago');
  });

  it('supports avulso charges without lease_id (taxas avulsas)', async () => {
    // 1. Criar cobrança de taxa avulsa (sem lease_id)
    const avulsoForm = new FormData();
    avulsoForm.set('intent', 'receipt.create');
    avulsoForm.set('category', 'extra');
    avulsoForm.set('description', 'Taxa de Pintura');
    avulsoForm.set('tenant_id', createdTenantId);
    avulsoForm.set('property_id', createdPropertyId);
    avulsoForm.set('due_date', '2026-11-15');
    avulsoForm.set('amount', '350');

    const createRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: avulsoForm,
      })
    );
    expect(createRes.status).toBe(200);

    const dashRes = await server.fetch(
      new Request('http://localhost:3000/dashboard?json=true', {
        headers: { Cookie: sessionCookie },
      })
    );
    const dashData = (await dashRes.json()) as { receipts: Receipt[] };
    const avulsoReceipt = dashData.receipts.find((r) => r.description === 'Taxa de Pintura');
    expect(avulsoReceipt).toBeDefined();
    if (!avulsoReceipt) throw new Error('Avulso receipt not found');
    expect(avulsoReceipt.category).toBe('extra');
    expect(avulsoReceipt.lease_id).toBeNull();
    expect(avulsoReceipt.tenant_name).toContain('Carlos Drummond');
    // Recibo avulso ainda não pago: PDF não deve ser emitido/downloadable
    const unpaidPdfRes = await server.fetch(
      new Request(`http://localhost:3000/pdf?id=${avulsoReceipt.id}`, {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(unpaidPdfRes.status).toBe(403);

    // Recibo não pago: portal público de autenticidade também deve bloquear (403)
    const unpaidToken = generateAuthToken('receipt', avulsoReceipt.id);
    const unpaidVerifyRes = await server.fetch(
      new Request(`http://localhost:3000/validar/recibo/${avulsoReceipt.id}?token=${unpaidToken}`)
    );
    expect(unpaidVerifyRes.status).toBe(403);
    const unpaidVerifyHtml = await unpaidVerifyRes.text();
    expect(unpaidVerifyHtml).toContain('Recibo ainda não pago');

    // Pagar a taxa avulsa (integral)
    const payForm = new FormData();
    payForm.set('intent', 'receipt.pay');
    payForm.set('id', avulsoReceipt.id);
    payForm.set('pay_amount', '350');
    const payRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: payForm,
      })
    );
    expect(payRes.status).toBe(200);

    const pdfRes = await server.fetch(
      new Request(`http://localhost:3000/pdf?id=${avulsoReceipt.id}`, {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(pdfRes.status).toBe(200);
    expect(pdfRes.headers.get('content-type')).toBe('application/pdf');

    // 3. Validação pública via autenticidade digital (após pagamento)
    const token = generateAuthToken('receipt', avulsoReceipt.id);
    const verifyRes = await server.fetch(
      new Request(`http://localhost:3000/validar/recibo/${avulsoReceipt.id}?token=${token}`)
    );
    expect(verifyRes.status).toBe(200);
    const verifyHtml = await verifyRes.text();
    expect(verifyHtml).toContain('Taxa de Pintura');
    expect(verifyHtml).toContain('DOCUMENTO AUTÊNTICO E VÁLIDO');

    // 4. WhatsApp preview
    const waRes = await server.fetch(
      new Request('http://localhost:3000/api/whatsapp/preview', {
        method: 'POST',
        headers: { Cookie: sessionCookie, 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({ type: 'receipt', id: avulsoReceipt.id }).toString(),
      })
    );
    expect(waRes.status).toBe(200);
    const waData = (await waRes.json()) as { message: string };
    expect(waData.message).toContain('Taxa de Pintura');
  });

  it('verifies period filter matching logic with "all" months and "all" years', () => {
    const receiptA = { due_date: '2026-05-10', paid_at: null, amount: '100', paid_amount: null };
    const receiptB = { due_date: '2025-09-20', paid_at: null, amount: '200', paid_amount: null };

    // Filtrar com mês específico e ano específico
    expect(receiptA.due_date.slice(5, 7) === '05' && receiptA.due_date.slice(0, 4) === '2026').toBe(true);

    // Filtrar com "all" para mês e ano fixo
    const matchAllMonths2026 = (r: typeof receiptA) => {
      const m: string = 'all';
      const y: string = '2026';
      const matchesMonth = m === 'all' || r.due_date.slice(5, 7) === m;
      const matchesYear = y === 'all' || r.due_date.slice(0, 4) === y;
      return matchesMonth && matchesYear;
    };
    expect(matchAllMonths2026(receiptA)).toBe(true);
    expect(matchAllMonths2026(receiptB)).toBe(false);

    // Filtrar com "all" para ano e "all" para mês (retorna todos)
    const matchAll = (r: typeof receiptA) => {
      const m: string = 'all';
      const y: string = 'all';
      return (m === 'all' || r.due_date.slice(5, 7) === m) && (y === 'all' || r.due_date.slice(0, 4) === y);
    };
    expect(matchAll(receiptA)).toBe(true);
    expect(matchAll(receiptB)).toBe(true);
  });

  it('GET /metrics returns 401 when unauthenticated and 200 with full aggregate metrics when authenticated', async () => {
    // 1. Não autenticado -> 401
    const unauthRes = await server.fetch(new Request('http://localhost:3000/metrics'));
    expect(unauthRes.status).toBe(401);

    // 2. Autenticado -> 200 com estrutura completa de métricas
    const authRes = await server.fetch(
      new Request('http://localhost:3000/metrics', {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(authRes.status).toBe(200);
    const data = (await authRes.json()) as {
      totals: {
        tenants: number;
        properties: number;
        activeLeases: number;
        receipts: number;
        paidReceipts: number;
        paidAmount: number;
        expectedMonthly: number;
      };
      monthly: Array<{ key: string; label: string; received: number; expected: number }>;
      status: Array<{ status: string; label: string; count: number }>;
      categories: Array<{ category: string; label: string; count: number }>;
      properties: Array<{ id: string; name: string; activeLeases: number }>;
      topTenants: Array<{ id: string; name: string; paidTotal: number }>;
    };

    expect(data.totals).toBeDefined();
    expect(typeof data.totals.tenants).toBe('number');
    expect(typeof data.totals.properties).toBe('number');
    expect(typeof data.totals.activeLeases).toBe('number');
    expect(typeof data.totals.receipts).toBe('number');
    expect(typeof data.totals.paidAmount).toBe('number');
    expect(Array.isArray(data.monthly)).toBe(true);
    expect(data.monthly.length).toBeGreaterThanOrEqual(1);
    expect(Array.isArray(data.status)).toBe(true);
    expect(data.status.some((s) => s.status === 'paid')).toBe(true);
    expect(Array.isArray(data.categories)).toBe(true);
    expect(Array.isArray(data.properties)).toBe(true);
    expect(Array.isArray(data.topTenants)).toBe(true);
    expect(data.topTenants.length).toBeGreaterThan(0);
    const firstTopTenant = data.topTenants[0];
    expect(firstTopTenant).toBeDefined();
    if (!firstTopTenant) throw new Error('topTenants is empty');
    expect(typeof firstTopTenant.name).toBe('string');
    expect(firstTopTenant.paidTotal).toBeGreaterThan(0);
  });

  it('POST /dashboard receipt.batch-delete removes multiple receipts in one request', async () => {
    // 1. Criar 2 recibos avulsos não pagos para exclusão
    const idsToDelete: string[] = [];
    for (let i = 0; i < 2; i++) {
      const form = new FormData();
      form.set('intent', 'receipt.create');
      form.set('category', 'extra');
      form.set('description', `Taxa Delete ${i}`);
      form.set('tenant_id', createdTenantId);
      form.set('property_id', createdPropertyId);
      form.set('due_date', '2026-12-15');
      form.set('amount', '100');
      const res = await server.fetch(
        new Request('http://localhost:3000/dashboard', {
          method: 'POST',
          headers: { Cookie: sessionCookie },
          body: form,
        })
      );
      expect(res.status).toBe(200);
      const dashRes = await server.fetch(
        new Request('http://localhost:3000/dashboard?json=true', {
          headers: { Cookie: sessionCookie },
        })
      );
      const data = (await dashRes.json()) as { receipts: Array<{ id: string; description: string }> };
      const r = data.receipts.find((x) => x.description === `Taxa Delete ${i}`);
      expect(r).toBeDefined();
      if (r) idsToDelete.push(r.id);
    }
    expect(idsToDelete.length).toBe(2);

    // 2. Acionar exclusão em lote
    const deleteForm = new FormData();
    deleteForm.set('intent', 'receipt.batch-delete');
    deleteForm.set('ids', idsToDelete.join(','));
    const delRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: deleteForm,
      })
    );
    expect(delRes.status).toBe(200);
    const delJson = (await delRes.json()) as { ok?: boolean };
    expect(delJson.ok).toBe(true);

    // 3. Confirmar que os IDs não existem mais
    const afterDashRes = await server.fetch(
      new Request('http://localhost:3000/dashboard?json=true', {
        headers: { Cookie: sessionCookie },
      })
    );
    const afterData = (await afterDashRes.json()) as { receipts: Array<{ id: string; description: string }> };
    for (const id of idsToDelete) {
      expect(afterData.receipts.find((r) => r.id === id)).toBeUndefined();
    }

    // 4. Edge case: ids vazios devem retornar 400 com mensagem
    const emptyForm = new FormData();
    emptyForm.set('intent', 'receipt.batch-delete');
    emptyForm.set('ids', '');
    const emptyRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: emptyForm,
      })
    );
    expect(emptyRes.status).toBe(400);
  });

  it('POST /api/whatsapp/preview gates unpaid receipts with 403 status', async () => {
    // 1. Criar recibo avulso não pago
    const unpaidForm = new FormData();
    unpaidForm.set('intent', 'receipt.create');
    unpaidForm.set('category', 'extra');
    unpaidForm.set('description', 'Taxa Não Paga');
    unpaidForm.set('tenant_id', createdTenantId);
    unpaidForm.set('property_id', createdPropertyId);
    unpaidForm.set('due_date', '2026-12-01');
    unpaidForm.set('amount', '500');

    const createRes = await server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: unpaidForm,
      })
    );
    expect(createRes.status).toBe(200);

    const dashRes = await server.fetch(
      new Request('http://localhost:3000/dashboard?json=true', {
        headers: { Cookie: sessionCookie },
      })
    );
    const dashData = (await dashRes.json()) as { receipts: Array<{ id: string; description: string; paid_at: string | null }> };
    const unpaidReceipt = dashData.receipts.find((r) => r.description === 'Taxa Não Paga');
    expect(unpaidReceipt).toBeDefined();
    if (!unpaidReceipt) throw new Error('Unpaid receipt not found');
    expect(unpaidReceipt.paid_at).toBeNull();

    // 2. Tentar gerar preview WhatsApp para recibo não pago -> deve retornar 403
    const previewRes = await server.fetch(
      new Request('http://localhost:3000/api/whatsapp/preview', {
        method: 'POST',
        headers: { Cookie: sessionCookie, 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({ type: 'receipt', id: unpaidReceipt.id }).toString(),
      })
    );
    expect(previewRes.status).toBe(403);
    const previewData = (await previewRes.json()) as { error: string };
    expect(previewData.error).toContain('ainda não pago');
  });

  it('GET / and GET /login redirect authenticated sessions to /dashboard?tab=dashboard', async () => {
    const rootRes = await server.fetch(
      new Request('http://localhost:3000/', {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(rootRes.status).toBe(302);
    expect(rootRes.headers.get('location')).toBe('/dashboard?tab=dashboard');

    const loginRes = await server.fetch(
      new Request('http://localhost:3000/login', {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(loginRes.status).toBe(302);
    expect(loginRes.headers.get('location')).toBe('/dashboard?tab=dashboard');
  });

  it('GET /dashboard pre-renders metrics during SSR with instant HTML without loading skeleton', async () => {
    const res = await server.fetch(
      new Request('http://localhost:3000/dashboard?tab=dashboard', {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(res.status).toBe(200);
    const html = await res.text();
    // Valida SSR: conteúdo de métricas já está presente no HTML gerado pelo servidor
    expect(html).toContain('metric-card');
    expect(html).toContain('Top inquilinos por valor pago');
    expect(html).toContain('Ocupação por imóvel');
    expect(html).not.toContain('Carregando métricas…');
  });
});
