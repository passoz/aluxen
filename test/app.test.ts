import { describe, expect, it } from 'bun:test';
import server from '../src/index';

describe('Aluxen Hono Application', () => {
  it('GET /api/health returns health status', async () => {
    const res = await server.fetch(new Request('http://localhost:3000/api/health'));
    expect(res.status).toBe(200);

    const body = (await res.json()) as Record<string, unknown>;
    expect(body['status']).toBe('ok');
    expect(body['runtime']).toBe('bun');
    expect(body['framework']).toBe('hono');
    expect(body['orm']).toBe('drizzle-orm');
  });

  it('GET / renders dashboard with HTML', async () => {
    const res = await server.fetch(new Request('http://localhost:3000/'));
    expect(res.status).toBe(200);
    expect(res.headers.get('content-type')).toContain('text/html');

    const html = await res.text();
    expect(html).toContain('Aluxen');
    expect(html).toContain('Painel Geral');
    expect(html).toContain('Inquilinos');
  });

  it('POST /tenants creates a tenant and GET /tenants displays it', async () => {
    const formData = new FormData();
    formData.append('name', 'Maria Silva');
    formData.append('email', 'maria@exemplo.com');
    formData.append('phone', '(11) 98765-4321');
    formData.append('cpf', '123.456.789-00');

    const postRes = await server.fetch(
      new Request('http://localhost:3000/tenants', {
        method: 'POST',
        body: formData,
      })
    );
    expect(postRes.status).toBe(302);

    const getRes = await server.fetch(new Request('http://localhost:3000/tenants'));
    expect(getRes.status).toBe(200);
    const html = await getRes.text();
    expect(html).toContain('Maria Silva');
    expect(html).toContain('maria@exemplo.com');
  });

  it('POST /properties creates a property and GET /properties displays it', async () => {
    const formData = new FormData();
    formData.append('name', 'Apartamento 101');
    formData.append('address', 'Rua das Flores, 123');
    formData.append('city', 'São Paulo');
    formData.append('state', 'SP');
    formData.append('zip', '01000-000');

    const postRes = await server.fetch(
      new Request('http://localhost:3000/properties', {
        method: 'POST',
        body: formData,
      })
    );
    expect(postRes.status).toBe(302);

    const getRes = await server.fetch(new Request('http://localhost:3000/properties'));
    expect(getRes.status).toBe(200);
    const html = await getRes.text();
    expect(html).toContain('Apartamento 101');
    expect(html).toContain('Rua das Flores, 123');
  });
});
