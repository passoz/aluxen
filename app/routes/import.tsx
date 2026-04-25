import { json, redirect } from '@remix-run/node';
import type { ActionFunctionArgs } from '@remix-run/node';
import { execute } from '../utils/db.server';
import { createId } from '../utils/id';
import { requireUserId } from '../utils/session.server';

interface BackupData {
  tenants: { id: string; name: string; email: string; phone: string | null; created_at: string; updated_at: string }[];
  properties: { id: string; name: string; address: string; city: string; state: string; zip: string | null; created_at: string; updated_at: string }[];
  leases: { id: string; tenant_id: string; property_id: string; start_date: string; end_date: string; due_day: number; rent_amount: number; created_at: string; updated_at: string }[];
  receipts: { id: string; lease_id: string; due_date: string; paid_at: string | null; amount: number; pdf_path: string | null; created_at: string }[];
}

export async function action({ request }: ActionFunctionArgs) {
  await requireUserId(request);

  const formData = await request.formData();
  const file = formData.get('file') as File | null;

  if (!file) {
    return json({ error: 'Arquivo não enviado.' }, { status: 400 });
  }

  let content: string;
  try {
    content = await file.text();
  } catch {
    return json({ error: 'Não foi possível ler o arquivo.' }, { status: 400 });
  }

  let data: BackupData;
  try {
    data = JSON.parse(content);
  } catch {
    return json({ error: 'Arquivo JSON inválido.' }, { status: 400 });
  }

  if (!data.tenants || !data.properties || !data.leases || !data.receipts) {
    return json({ error: 'Formato de backup inválido.' }, { status: 400 });
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
        'INSERT INTO tenants (id, name, email, phone, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)',
        [newId, t.name, t.email, t.phone, t.created_at, t.updated_at],
      );
    }

    for (const p of data.properties) {
      const newId = createId();
      idMap.properties.set(p.id, newId);
      await execute(
        'INSERT INTO properties (id, name, address, city, state, zip, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)',
        [newId, p.name, p.address, p.city, p.state, p.zip, p.created_at, p.updated_at],
      );
    }

    for (const l of data.leases) {
      const newId = createId();
      idMap.leases.set(l.id, newId);
      await execute(
        'INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)',
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
        ],
      );
    }

    for (const r of data.receipts) {
      const newId = createId();
      await execute(
        'INSERT INTO receipts (id, lease_id, due_date, paid_at, amount, pdf_path, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)',
        [
          newId,
          idMap.leases.get(r.lease_id) || r.lease_id,
          r.due_date,
          r.paid_at,
          r.amount,
          r.pdf_path,
          r.created_at,
        ],
      );
    }
  } catch (error) {
    return json(
      { error: error instanceof Error ? error.message : 'Erro ao importar dados.' },
      { status: 400 },
    );
  }

  return redirect('/dashboard?tab=tenants&toast=importado');
}