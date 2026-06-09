import type { LoaderFunctionArgs } from '@remix-run/node';
import { queryOne } from '../utils/db.server';
import { requireUserId } from '../utils/session.server';
import { createReceiptPdfBuffer } from '../utils/pdf.server';

export async function loader({ request }: LoaderFunctionArgs) {
  await requireUserId(request);

  const url = new URL(request.url);
  const id = url.searchParams.get('id');
  const showSignature = url.searchParams.get('assinatura') !== 'false';

  if (!id) {
    throw new Response('ID inválido', { status: 400 });
  }

  const receipt = await queryOne<{
    id: string;
    due_date: string;
    paid_at: string | null;
    amount: number;
    tenant_name: string;
    property_name: string;
  }>(`
    SELECT r.id, r.due_date, r.paid_at, r.amount,
           t.name AS tenant_name, p.name AS property_name
    FROM receipts r
    JOIN leases l ON l.id = r.lease_id
    JOIN tenants t ON t.id = l.tenant_id
    JOIN properties p ON p.id = l.property_id
    WHERE r.id = $1
  `, [id]);

  if (!receipt) {
    throw new Response('Recibo não encontrado', { status: 404 });
  }

  if (!receipt.paid_at) {
    throw new Response('Recibo não está pago', { status: 400 });
  }

  const pdfBytes = await createReceiptPdfBuffer({
    receiptId: receipt.id,
    tenantName: receipt.tenant_name,
    propertyName: receipt.property_name,
    dueDate: receipt.due_date,
    paidAt: receipt.paid_at,
    amount: receipt.amount,
    signature: showSignature,
  });

  return new Response(pdfBytes as BodyInit, {
    headers: {
      'Content-Type': 'application/pdf',
      'Content-Disposition': `inline; filename="recibo-${receipt.id}.pdf"`,
    },
  });
}