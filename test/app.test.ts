import { describe, expect, it } from 'bun:test';
import type { Receipt } from '../src/types/dashboard';
import type { AuditPage } from '../src/utils/audit-meta';
import { inflateSync } from 'node:zlib';
import server from '../src/index';
import { generateAuthToken } from '../src/utils/auth-token';
import { formatCurrency, formatDateBr, getReceiptStatus, getReceiptStatusLabel } from '../src/utils/format';
import { createId } from '../src/utils/id';

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
    // CPF do locatário emitido no documento
    expect(receiptText).toContain('CPF: 111.222.333-44');
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
    expect(validHtml).toContain('CPF do Locatário');
    expect(validHtml).toContain('111.222.333-44');

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

  async function fetchAudit(query: string): Promise<AuditPage> {
    const res = await server.fetch(
      new Request(`http://localhost:3000/audit?${query}`, {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(res.status).toBe(200);
    return (await res.json()) as AuditPage;
  }

  async function postDashboard(fields: Record<string, string>): Promise<Response> {
    const form = new FormData();
    for (const [key, value] of Object.entries(fields)) form.set(key, value);
    return server.fetch(
      new Request('http://localhost:3000/dashboard', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: form,
      })
    );
  }

  it('GET /audit exige autenticação e devolve página filtrada para sessões válidas', async () => {
    const anonymous = await server.fetch(new Request('http://localhost:3000/audit'));
    expect(anonymous.status).toBe(401);

    const page = await fetchAudit('entity=tenant&page=1&pageSize=5');
    expect(page.page).toBe(1);
    expect(page.pageSize).toBe(5);
    expect(page.total).toBeGreaterThan(0);
    expect(page.items.length).toBeLessThanOrEqual(5);
    expect(page.items.every((item) => item.entity === 'tenant')).toBe(true);
    expect(page.actors).toContain('passoz');
    expect(page.items[0]?.createdAt).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}$/);
  });

  it('trilha registra alteração de nome do inquilino com valores antes e depois', async () => {
    const tenantId = createId();

    const createRes = await postDashboard({
      intent: 'tenant.create',
      id: tenantId,
      name: 'Auditoria Antes',
      email: 'auditoria@aluxen.test',
      phone: '(22) 90000-0000',
    });
    expect(createRes.status).toBe(200);

    const updateRes = await postDashboard({
      intent: 'tenant.update',
      id: tenantId,
      name: 'Auditoria Depois',
      email: 'auditoria@aluxen.test',
      phone: '(22) 90000-0000',
    });
    expect(updateRes.status).toBe(200);

    const page = await fetchAudit(`entity=tenant&action=tenant.update&search=${tenantId}`);
    expect(page.total).toBe(1);

    const entry = page.items[0]!;
    expect(entry.actorName).toBe('passoz');
    expect(entry.origin).toBe('web');
    expect(entry.entityLabel).toBe('Auditoria Depois');
    expect(entry.summary).toContain('Auditoria Depois');
    expect(entry.changes).toEqual([
      { field: 'name', label: 'Nome', from: 'Auditoria Antes', to: 'Auditoria Depois' },
    ]);

    // Campos inalterados não geram ruído na trilha
    expect(entry.changes.some((change) => change.field === 'email')).toBe(false);

    const deleteRes = await postDashboard({ intent: 'tenant.delete', id: tenantId });
    expect(deleteRes.status).toBe(200);
  });

  it('trilha registra alteração de valor do pagamento em formato monetário', async () => {
    const receiptId = createId();
    const baseFields = {
      id: receiptId,
      lease_id: createdLeaseId,
      category: 'rent',
      due_date: '2026-07-10',
    };

    const createRes = await postDashboard({ intent: 'receipt.create', ...baseFields, amount: '1200.50' });
    expect(createRes.status).toBe(200);

    const updateRes = await postDashboard({ intent: 'receipt.update', ...baseFields, amount: '1350.75' });
    expect(updateRes.status).toBe(200);

    const page = await fetchAudit(`entity=receipt&action=receipt.update&search=${receiptId}`);
    expect(page.total).toBe(1);

    const entry = page.items[0]!;
    expect(entry.entity).toBe('receipt');
    const amountChange = entry.changes.find((change) => change.field === 'amount');
    expect(amountChange?.label).toBe('Valor');
    expect(amountChange?.from).toBe(formatCurrency(1200.5));
    expect(amountChange?.to).toBe(formatCurrency(1350.75));

    const deleteRes = await postDashboard({ intent: 'receipt.delete', id: receiptId });
    expect(deleteRes.status).toBe(200);
  });

  it('trilha registra emissão de recibo em PDF', async () => {
    const receiptId = createId();
    const createRes = await postDashboard({
      intent: 'receipt.create',
      id: receiptId,
      lease_id: createdLeaseId,
      category: 'rent',
      due_date: '2026-08-10',
      amount: '900',
      paid_at: '2026-08-10T09:30',
    });
    expect(createRes.status).toBe(200);

    const pdfRes = await server.fetch(
      new Request(`http://localhost:3000/pdf?id=${receiptId}`, { headers: { Cookie: sessionCookie } })
    );
    expect(pdfRes.status).toBe(200);

    const page = await fetchAudit(`entity=document&action=document.receipt&search=${receiptId}`);
    expect(page.total).toBeGreaterThanOrEqual(1);
    const entry = page.items[0]!;
    expect(entry.summary).toContain('emitido');
    expect(entry.metadata['kind']).toBe('Recibo');
    expect(entry.actorName).toBe('passoz');

    const deleteRes = await postDashboard({ intent: 'receipt.delete', id: receiptId });
    expect(deleteRes.status).toBe(200);
  });

  it('trilha registra troca de senha sem gravar as senhas informadas', async () => {
    const temporary = 'troca-temporaria-456';

    const changeForm = new FormData();
    changeForm.set('intent', 'change-password');
    changeForm.set('current_password', 'senha-de-teste');
    changeForm.set('new_password', temporary);
    changeForm.set('confirm_password', temporary);
    const changeRes = await server.fetch(
      new Request('http://localhost:3000/settings', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: changeForm,
      })
    );
    expect(changeRes.status).toBe(200);
    expect(await changeRes.text()).toContain('Senha alterada com sucesso.');

    const restoreForm = new FormData();
    restoreForm.set('intent', 'change-password');
    restoreForm.set('current_password', temporary);
    restoreForm.set('new_password', 'senha-de-teste');
    restoreForm.set('confirm_password', 'senha-de-teste');
    const restoreRes = await server.fetch(
      new Request('http://localhost:3000/settings', {
        method: 'POST',
        headers: { Cookie: sessionCookie },
        body: restoreForm,
      })
    );
    expect(restoreRes.status).toBe(200);

    const page = await fetchAudit('action=user.password');
    expect(page.total).toBeGreaterThanOrEqual(2);
    expect(page.items[0]!.summary).toContain('Senha do usuário');
    const serialized = JSON.stringify(page);
    expect(serialized).not.toContain(temporary);
    expect(serialized).not.toContain('senha-de-teste');
  });

  it('trilha registra tentativa de acesso negada sem expor a senha', async () => {
    const page = await fetchAudit('action=auth.login-failed&search=usuario_inexistente');
    expect(page.total).toBeGreaterThanOrEqual(1);

    const entry = page.items[0]!;
    expect(entry.entityLabel).toBe('usuario_inexistente');
    expect(entry.metadata['reason']).toBe('Credenciais inválidas');
    expect(JSON.stringify(page)).not.toContain('senha_errada');
  });

  it('GET /audit/export gera CSV com cabeçalho, BOM e eventos filtrados', async () => {
    const res = await server.fetch(
      new Request('http://localhost:3000/audit/export?entity=tenant&action=tenant.create', {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(res.status).toBe(200);
    expect(res.headers.get('content-type')).toContain('text/csv');
    expect(res.headers.get('content-disposition')).toContain('aluxen-historico-');

    const csv = await res.text();
    expect(csv.startsWith('\uFEFF')).toBe(true);
    expect(csv).toContain('"Data/Hora";"Usuário";"Ação";"Registro";"Resumo"');
    expect(csv).toContain('tenant.create');
    expect(csv).not.toContain('lease.create');
  });

  it('GET /api/info exige sessão e devolve identificação do build e estado do sistema', async () => {
    const anonymous = await server.fetch(new Request('http://localhost:3000/api/info'));
    expect(anonymous.status).toBe(401);

    const res = await server.fetch(
      new Request('http://localhost:3000/api/info', { headers: { Cookie: sessionCookie } })
    );
    expect(res.status).toBe(200);
    const info = (await res.json()) as {
      app: string;
      version: string;
      runtime: string;
      nodeEnv: string;
      uptimeSeconds: number;
      database: { path: string; sizeBytes: number };
      counts: { tenants: number; properties: number; leases: number; receipts: number };
      build: { shortCommit: string | null; source: string; version: string };
    };
    expect(info.app).toBe('Aluxen');
    expect(info.version).toMatch(/^\d+\.\d+\.\d+/);
    expect(info.runtime).toMatch(/^Bun \d+\./);
    expect(info.build.shortCommit).toMatch(/^[0-9a-f]{7,}$/);
    expect(info.build.source).toBe('local');
    expect(info.database.path).toBeTruthy();
    expect(info.database.sizeBytes).toBeGreaterThan(0);
    expect(info.uptimeSeconds).toBeGreaterThanOrEqual(0);
    expect(info.counts.tenants).toBeGreaterThan(0);
  });

  it('GET /dashboard renderiza o botão Informações no rodapé da barra lateral', async () => {
    const res = await server.fetch(
      new Request('http://localhost:3000/dashboard?tab=dashboard', { headers: { Cookie: sessionCookie } })
    );
    expect(res.status).toBe(200);
    const html = await res.text();
    expect(html).toContain('aria-label="Informações"');
    expect(html).toContain('Informações</span>');
  });

  it('GET /dashboard?tab=history renderiza a trilha de auditoria no SSR', async () => {
    const res = await server.fetch(
      new Request('http://localhost:3000/dashboard?tab=history', {
        headers: { Cookie: sessionCookie },
      })
    );
    expect(res.status).toBe(200);
    const html = await res.text();
    expect(html).toContain('Histórico e Auditoria');
    expect(html).toContain('Trilha de auditoria');
    expect(html).toContain('history-table');
    expect(html).toContain('Exportar CSV');
  });
});

describe('Área do Cliente (locatário)', () => {
  const CLIENT_IP = '203.0.113.10';
  const NEW_PASSWORD = 'portal-seguro-1';
  // Cada execução cria o próprio cadastro: o banco de testes pode ser reaproveitado entre execuções.
  const RUN_TAG = crypto.randomUUID().slice(0, 8).toUpperCase();
  const TENANT_A_NAME = `Locatário Portal A ${RUN_TAG}`;
  const TENANT_B_NAME = `Locatário Portal B ${RUN_TAG}`;
  const PROPERTY_A_NAME = `Imóvel Portal Cliente ${RUN_TAG}`;
  const PROPERTY_B_NAME = `Imóvel Portal Cliente B ${RUN_TAG}`;
  const CPF_A_DIGITS = Array.from(crypto.getRandomValues(new Uint8Array(11)), (byte) => byte % 10).join('');
  const CPF_B_DIGITS = Array.from(crypto.getRandomValues(new Uint8Array(11)), (byte) => byte % 10).join('');
  const formatCpf = (digits: string) => `${digits.slice(0, 3)}.${digits.slice(3, 6)}.${digits.slice(6, 9)}-${digits.slice(9)}`;
  const UNPAID_DESCRIPTION = `Taxa de Portal Não Paga ${RUN_TAG}`;

  let adminCookie = '';
  let clientCookie = '';
  let tenantAId = '';
  let tenantBId = '';
  let leaseAId = '';
  let leaseBId = '';
  let paidReceiptAId = '';
  let unpaidReceiptAId = '';
  let paidReceiptBId = '';

  async function adminPost(path: string, fields: Record<string, string>) {
    const form = new FormData();
    for (const [key, value] of Object.entries(fields)) form.set(key, value);
    return server.fetch(
      new Request(`http://localhost:3000${path}`, {
        method: 'POST',
        headers: { Cookie: adminCookie },
        body: form,
      })
    );
  }

  function clientLogin(password: string, cpf: string = CPF_A_DIGITS, ip: string = CLIENT_IP) {
    const form = new FormData();
    form.set('cpf', cpf);
    form.set('password', password);
    return server.fetch(
      new Request('http://localhost:3000/portal/login', {
        method: 'POST',
        headers: { 'x-forwarded-for': ip },
        body: form,
      })
    );
  }

  it('prepara inquilinos, contratos e recibos de apoio', async () => {
    const loginForm = new FormData();
    loginForm.set('username', 'passoz');
    loginForm.set('password', 'senha-de-teste');
    const loginRes = await server.fetch(new Request('http://localhost:3000/login', { method: 'POST', body: loginForm }));
    expect(loginRes.status).toBe(302);
    adminCookie = (loginRes.headers.get('set-cookie') || '').split(';')[0] || '';
    expect(adminCookie).toContain('__aluxen_session=');

    await adminPost('/dashboard', { intent: 'tenant.create', name: TENANT_A_NAME, email: `portal.a.${RUN_TAG}@teste.local`, cpf: formatCpf(CPF_A_DIGITS) });
    await adminPost('/dashboard', { intent: 'tenant.create', name: TENANT_B_NAME, email: `portal.b.${RUN_TAG}@teste.local`, cpf: formatCpf(CPF_B_DIGITS) });
    await adminPost('/dashboard', {
      intent: 'property.create',
      name: PROPERTY_A_NAME,
      address: 'Rua das Flores, 10',
      city: 'Cabo Frio',
      state: 'RJ',
    });
    await adminPost('/dashboard', {
      intent: 'property.create',
      name: PROPERTY_B_NAME,
      address: 'Rua das Conchas, 20',
      city: 'Cabo Frio',
      state: 'RJ',
    });

    const listRes = await server.fetch(
      new Request('http://localhost:3000/dashboard?json=true', { headers: { Cookie: adminCookie } })
    );
    const listData = (await listRes.json()) as {
      tenants: Array<{ id: string; name: string }>;
      properties: Array<{ id: string; name: string }>;
    };
    tenantAId = listData.tenants.find((t) => t.name === TENANT_A_NAME)?.id || '';
    tenantBId = listData.tenants.find((t) => t.name === TENANT_B_NAME)?.id || '';
    const propertyAId = listData.properties.find((p) => p.name === PROPERTY_A_NAME)?.id || '';
    const propertyBId = listData.properties.find((p) => p.name === PROPERTY_B_NAME)?.id || '';
    expect(tenantAId).toBeTruthy();
    expect(tenantBId).toBeTruthy();
    expect(propertyAId).toBeTruthy();
    expect(propertyBId).toBeTruthy();

    // Um imóvel por locatário: contrato vigente impede sobreposição no mesmo imóvel
    const leaseFixtures: Array<[string, string]> = [
      [tenantAId, propertyAId],
      [tenantBId, propertyBId],
    ];
    for (const [tenantId, propertyId] of leaseFixtures) {
      const leaseRes = await adminPost('/dashboard', {
        intent: 'lease.create',
        tenant_id: tenantId,
        property_id: propertyId,
        start_date: '2026-01-01',
        end_date: '2026-12-31',
        due_day: '10',
        rent_amount: '1800',
        status: 'active',
      });
      expect(leaseRes.status).toBe(200);
    }

    const leasesRes = await server.fetch(
      new Request('http://localhost:3000/dashboard?json=true', { headers: { Cookie: adminCookie } })
    );
    const leasesData = (await leasesRes.json()) as { leases: Array<{ id: string; tenant_id: string; property_name: string }> };
    leaseAId = leasesData.leases.find((l) => l.tenant_id === tenantAId && l.property_name === PROPERTY_A_NAME)?.id || '';
    leaseBId = leasesData.leases.find((l) => l.tenant_id === tenantBId && l.property_name === PROPERTY_B_NAME)?.id || '';
    expect(leaseAId).toBeTruthy();
    expect(leaseBId).toBeTruthy();

    // Recibo quitado do locatário A, cobrança avulsa em aberto de A e recibo quitado de B
    await adminPost('/dashboard', {
      intent: 'receipt.create',
      lease_id: leaseAId,
      due_date: '2026-05-10',
      amount: '1800',
      paid_at: '2026-05-09T10:00',
    });
    await adminPost('/dashboard', {
      intent: 'receipt.create',
      category: 'extra',
      description: UNPAID_DESCRIPTION,
      tenant_id: tenantAId,
      property_id: propertyAId,
      due_date: '2026-06-10',
      amount: '90',
    });
    await adminPost('/dashboard', {
      intent: 'receipt.create',
      lease_id: leaseBId,
      due_date: '2026-05-10',
      amount: '900',
      paid_at: '2026-05-09T11:00',
    });

    const receiptsRes = await server.fetch(
      new Request('http://localhost:3000/dashboard?json=true', { headers: { Cookie: adminCookie } })
    );
    const receiptsData = (await receiptsRes.json()) as {
      receipts: Array<{ id: string; lease_id: string | null; description: string | null; paid_at: string | null }>;
    };
    paidReceiptAId = receiptsData.receipts.find((r) => r.lease_id === leaseAId && r.paid_at)?.id || '';
    paidReceiptBId = receiptsData.receipts.find((r) => r.lease_id === leaseBId && r.paid_at)?.id || '';
    unpaidReceiptAId = receiptsData.receipts.find((r) => r.description === UNPAID_DESCRIPTION)?.id || '';
    expect(paidReceiptAId).toBeTruthy();
    expect(paidReceiptBId).toBeTruthy();
    expect(unpaidReceiptAId).toBeTruthy();
  });

  it('GET /portal/login é uma tela separada, com link para o acesso administrativo', async () => {
    const res = await server.fetch(new Request('http://localhost:3000/portal/login'));
    expect(res.status).toBe(200);
    const html = await res.text();
    expect(html).toContain('Área do Cliente');
    expect(html).toContain('action="/portal/login"');
    expect(html).toContain('name="cpf"');
    expect(html).toContain('href="/login"');
  });

  it('POST /portal/login autentica com o CPF sem pontuação e exige a troca da senha padrão', async () => {
    const res = await clientLogin(CPF_A_DIGITS);
    expect(res.status).toBe(302);
    expect(res.headers.get('location')).toBe('/portal/password');
    const setCookie = res.headers.get('set-cookie') || '';
    expect(setCookie).toContain('__aluxen_client_session=');
    clientCookie = setCookie.split(';')[0] || '';

    // Enquanto a senha padrão valer, a leitura fica bloqueada até a troca
    const areaRes = await server.fetch(
      new Request('http://localhost:3000/portal', { headers: { Cookie: clientCookie } })
    );
    expect(areaRes.status).toBe(302);
    expect(areaRes.headers.get('location')).toBe('/portal/password');

    const passwordRes = await server.fetch(
      new Request('http://localhost:3000/portal/password', { headers: { Cookie: clientCookie } })
    );
    expect(passwordRes.status).toBe(200);
    const html = await passwordRes.text();
    expect(html).toContain('action="/portal/password"');
    expect(html).toContain('name="current_password"');
    expect(html).toContain('name="new_password"');
    expect(html).toContain('Confirmar nova senha');
  });

  it('POST /portal/login recusa senha incorreta sem revelar se o CPF existe', async () => {
    const res = await clientLogin('senha-errada', CPF_A_DIGITS);
    expect(res.status).toBe(401);
    const html = await res.text();
    expect(html).toContain('CPF ou senha inválidos.');

    const unknownRes = await clientLogin('senha-errada', '00000000000', '203.0.113.11');
    expect(unknownRes.status).toBe(401);
  });

  it('POST /portal/password valida senha atual, confirmação e rejeita o próprio CPF', async () => {
    const wrongCurrent = new FormData();
    wrongCurrent.set('current_password', 'nao-e-o-cpf');
    wrongCurrent.set('new_password', NEW_PASSWORD);
    wrongCurrent.set('confirm_password', NEW_PASSWORD);
    const wrongRes = await server.fetch(
      new Request('http://localhost:3000/portal/password', { method: 'POST', headers: { Cookie: clientCookie }, body: wrongCurrent })
    );
    expect(wrongRes.status).toBe(400);
    expect(await wrongRes.text()).toContain('Senha atual incorreta.');

    const useCpf = new FormData();
    useCpf.set('current_password', CPF_A_DIGITS);
    useCpf.set('new_password', CPF_A_DIGITS);
    useCpf.set('confirm_password', CPF_A_DIGITS);
    const cpfRes = await server.fetch(
      new Request('http://localhost:3000/portal/password', { method: 'POST', headers: { Cookie: clientCookie }, body: useCpf })
    );
    expect(cpfRes.status).toBe(400);
    expect(await cpfRes.text()).toContain('não pode ser o seu CPF');

    const mismatch = new FormData();
    mismatch.set('current_password', CPF_A_DIGITS);
    mismatch.set('new_password', NEW_PASSWORD);
    mismatch.set('confirm_password', 'outra-senha-1');
    const mismatchRes = await server.fetch(
      new Request('http://localhost:3000/portal/password', { method: 'POST', headers: { Cookie: clientCookie }, body: mismatch })
    );
    expect(mismatchRes.status).toBe(400);
    expect(await mismatchRes.text()).toContain('confirmação não corresponde');

    const valid = new FormData();
    valid.set('current_password', CPF_A_DIGITS);
    valid.set('new_password', NEW_PASSWORD);
    valid.set('confirm_password', NEW_PASSWORD);
    const validRes = await server.fetch(
      new Request('http://localhost:3000/portal/password', { method: 'POST', headers: { Cookie: clientCookie }, body: valid })
    );
    expect(validRes.status).toBe(302);
    expect(validRes.headers.get('location')).toBe('/portal?ok=password');
  });

  it('GET /portal lista os contratos e somente os recibos quitados do titular', async () => {
    const res = await server.fetch(
      new Request('http://localhost:3000/portal', { headers: { Cookie: clientCookie } })
    );
    expect(res.status).toBe(200);
    const html = await res.text();

    expect(html).toContain(TENANT_A_NAME);
    expect(html).toContain('Meus contratos');
    expect(html).toContain(`/contract-pdf?id=${leaseAId}`);
    expect(html).toContain('/validar/contrato/');
    expect(html).toContain('Meus recibos quitados');
    expect(html).toContain(`/pdf?id=${paidReceiptAId}`);
    expect(html).toContain('/validar/recibo/');
    expect(html).toContain(formatDateBr('2026-05-09'));
    expect(html).toContain(formatCurrency(1800));

    // Recibo em aberto e dados de outro locatário não aparecem
    expect(html).not.toContain(unpaidReceiptAId);
    expect(html).not.toContain(UNPAID_DESCRIPTION);
    expect(html).not.toContain(TENANT_B_NAME);
    expect(html).not.toContain(paidReceiptBId);
  });

  it('sessão do locatário não acessa documentos de outro cadastro nem recibos em aberto', async () => {
    const ownPaid = await server.fetch(
      new Request(`http://localhost:3000/pdf?id=${paidReceiptAId}`, { headers: { Cookie: clientCookie } })
    );
    expect(ownPaid.status).toBe(200);
    expect(ownPaid.headers.get('content-type')).toBe('application/pdf');

    const foreignReceipt = await server.fetch(
      new Request(`http://localhost:3000/pdf?id=${paidReceiptBId}`, { headers: { Cookie: clientCookie } })
    );
    expect(foreignReceipt.status).toBe(403);
    expect(await foreignReceipt.text()).toContain('não pertence ao seu cadastro');

    const foreignContract = await server.fetch(
      new Request(`http://localhost:3000/contract-pdf?id=${leaseBId}`, { headers: { Cookie: clientCookie } })
    );
    expect(foreignContract.status).toBe(403);

    const ownUnpaid = await server.fetch(
      new Request(`http://localhost:3000/pdf?id=${unpaidReceiptAId}`, { headers: { Cookie: clientCookie } })
    );
    expect(ownUnpaid.status).toBe(403);
    expect(await ownUnpaid.text()).toContain('Recibo não pago');

    // O painel administrativo segue com acesso total
    const adminView = await server.fetch(
      new Request(`http://localhost:3000/pdf?id=${paidReceiptBId}`, { headers: { Cookie: adminCookie } })
    );
    expect(adminView.status).toBe(200);
  });

  it('sem sessão a Área do Cliente redireciona para o login do cliente', async () => {
    const res = await server.fetch(new Request('http://localhost:3000/portal'));
    expect(res.status).toBe(302);
    expect(res.headers.get('location')).toBe('/portal/login');
  });

  it('após a troca, a nova senha autentica e a senha padrão deixa de valer', async () => {
    const withNewPassword = await clientLogin(NEW_PASSWORD);
    expect(withNewPassword.status).toBe(302);
    expect(withNewPassword.headers.get('location')).toBe('/portal');

    const withDefault = await clientLogin(CPF_A_DIGITS);
    expect(withDefault.status).toBe(401);
  });

  it('a trilha registra os acessos e a troca de senha do locatário sem gravar senhas', async () => {
    const loginRes = await server.fetch(
      new Request('http://localhost:3000/audit?action=auth.client-login', { headers: { Cookie: adminCookie } })
    );
    expect(loginRes.status).toBe(200);
    const page = (await loginRes.json()) as { total: number; items: Array<{ origin: string; actorName: string | null; summary: string }> };
    expect(page.total).toBeGreaterThanOrEqual(1);
    expect(page.items[0]!.origin).toBe('client');
    expect(page.items[0]!.actorName).toBe(TENANT_A_NAME);

    const passwordRes = await server.fetch(
      new Request('http://localhost:3000/audit?action=auth.client-password', { headers: { Cookie: adminCookie } })
    );
    const passwordPage = (await passwordRes.json()) as {
      total: number;
      items: Array<{ entity: string; entityLabel: string | null; origin: string }>;
    };
    expect(passwordPage.total).toBeGreaterThanOrEqual(1);
    expect(passwordPage.items[0]!.entity).toBe('tenant');
    expect(passwordPage.items[0]!.entityLabel).toBe(TENANT_A_NAME);
    expect(passwordPage.items[0]!.origin).toBe('client');

    const auditHtml = await server.fetch(
      new Request('http://localhost:3000/audit', { headers: { Cookie: adminCookie } })
    );
    const serialized = JSON.stringify(await auditHtml.json());
    expect(serialized).not.toContain(NEW_PASSWORD);
  });

  it('POST /portal/logout encerra a sessão e devolve ao login do cliente', async () => {
    const res = await server.fetch(
      new Request('http://localhost:3000/portal/logout', { method: 'POST', headers: { Cookie: clientCookie } })
    );
    expect(res.status).toBe(302);
    expect(res.headers.get('location')).toBe('/portal/login');
    expect(res.headers.get('set-cookie') || '').toContain('__aluxen_client_session=');
  });
});
