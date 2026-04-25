import { json } from '@remix-run/node';
import type { LoaderFunctionArgs } from '@remix-run/node';
import { query } from '../utils/db.server';
import { requireUserId } from '../utils/session.server';

export async function loader({ request }: LoaderFunctionArgs) {
  await requireUserId(request);

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

  return json(data, {
    headers: {
      'Content-Type': 'application/json',
      'Content-Disposition': `attachment; filename="aluxen-backup-${new Date().toISOString().split('T')[0]}.json"`,
    },
  });
}