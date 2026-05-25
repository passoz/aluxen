import type { LoaderFunctionArgs } from '@remix-run/node';
import { queryOne } from '../utils/db.server';
import { requireUserId } from '../utils/session.server';
import { createContractPdfBuffer } from '../utils/contract-pdf.server';

export async function loader({ request }: LoaderFunctionArgs) {
  await requireUserId(request);

  const url = new URL(request.url);
  const id = url.searchParams.get('id');

  if (!id) {
    throw new Response('ID inválido', { status: 400 });
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
  }>(`
    SELECT l.id, l.start_date, l.end_date, l.due_day, l.rent_amount,
           t.name AS tenant_name, t.phone AS tenant_phone, t.cpf AS tenant_cpf,
           p.name AS property_name, p.address AS property_address,
           p.city AS property_city, p.state AS property_state, p.zip AS property_zip
    FROM leases l
    JOIN tenants t ON t.id = l.tenant_id
    JOIN properties p ON p.id = l.property_id
    WHERE l.id = $1
  `, [id]);

  if (!lease) {
    throw new Response('Contrato não encontrado', { status: 404 });
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
  });

  return new Response(pdfBytes as BodyInit, {
    headers: {
      'Content-Type': 'application/pdf',
      'Content-Disposition': `inline; filename="contrato-${lease.id}.pdf"`,
    },
  });
}
