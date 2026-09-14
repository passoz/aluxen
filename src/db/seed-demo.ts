import { initializeDatabase, setSetting } from './index';
import { db } from './index';
import { tenants, properties, leases, receipts } from './schema';
import { createId } from '../utils/id';
import { createLogger } from '../utils/logger';

const log = createLogger('db.seed');

export async function seedDemoData(): Promise<void> {
  await initializeDatabase();

  log.info('Populando banco de dados de demonstração');

  // Limpar tabelas para dados frescos de teste (mantendo usuário admin)
  await db.delete(receipts);
  await db.delete(leases);
  await db.delete(properties);
  await db.delete(tenants);

  // Configurações do Locador
  await setSetting('landlord_name', 'Locador Demonstração');
  await setSetting('landlord_cpf', '000.000.000-00');
  await setSetting('landlord_email', 'demo@aluxen.local');

  // 1. Inquilinos
  const t1 = createId();
  const t2 = createId();
  const t3 = createId();
  const t4 = createId();
  const t5 = createId();

  await db.insert(tenants).values([
    { id: t1, name: 'Carlos Drummond de Andrade', email: 'carlos.drummond@exemplo.com.br', phone: '(22) 99888-1122', cpf: '111.222.333-44' },
    { id: t2, name: 'Clarice Lispector', email: 'clarice.lispector@exemplo.com.br', phone: '(22) 99777-2233', cpf: '222.333.444-55' },
    { id: t3, name: 'Machado de Assis', email: 'machado.assis@exemplo.com.br', phone: '(22) 99666-3344', cpf: '333.444.555-66' },
    { id: t4, name: 'Cecília Meireles', email: 'cecilia.meireles@exemplo.com.br', phone: '(22) 99555-4455', cpf: '444.555.666-77' },
    { id: t5, name: 'Vinicius de Moraes', email: 'vinicius.moraes@exemplo.com.br', phone: '(22) 99444-5566', cpf: '555.666.777-88' },
  ]);

  // 2. Imóveis
  const p1 = createId();
  const p2 = createId();
  const p3 = createId();
  const p4 = createId();

  await db.insert(properties).values([
    { id: p1, name: 'Apto 101 - Praia do Forte', address: 'Av. do Contorno, 250, Apto 101', city: 'Cabo Frio', state: 'RJ', zip: '28907-250' },
    { id: p2, name: 'Casa das Dunas', address: 'Rua dos Diamantes, 14, Braga', city: 'Cabo Frio', state: 'RJ', zip: '28908-120' },
    { id: p3, name: 'Cobertura Caiçara', address: 'Rua das Palmeiras, 300, Cobertura 01, Jardim Caiçara', city: 'Cabo Frio', state: 'RJ', zip: '28910-370' },
    { id: p4, name: 'Flat Passagem Histórica', address: 'Largo de São Benedito, 88, Passagem', city: 'Cabo Frio', state: 'RJ', zip: '28906-030' },
  ]);

  // 3. Contratos de Locação (Leases)
  const l1 = createId();
  const l2 = createId();
  const l3 = createId();
  const l4 = createId();

  await db.insert(leases).values([
    { id: l1, tenantId: t1, propertyId: p1, startDate: '2026-01-01', endDate: '2026-12-31', dueDay: 10, rentAmount: 2500.0, status: 'active' },
    { id: l2, tenantId: t2, propertyId: p2, startDate: '2026-02-01', endDate: '2027-01-31', dueDay: 5, rentAmount: 3200.0, status: 'active' },
    { id: l3, tenantId: t3, propertyId: p3, startDate: '2026-03-01', endDate: '2027-02-28', dueDay: 15, rentAmount: 4500.0, status: 'active' },
    { id: l4, tenantId: t4, propertyId: p4, startDate: '2025-01-01', endDate: '2025-12-31', dueDay: 1, rentAmount: 1800.0, status: 'terminated' },
  ]);

  // 4. Recibos / Pagamentos
  // Pagos em meses anteriores
  await db.insert(receipts).values([
    { id: createId(), leaseId: l1, dueDate: '2026-07-10', paidAt: '2026-07-09T14:30', amount: 2500.0, generatedKey: `${l1}:2026-07` },
    { id: createId(), leaseId: l1, dueDate: '2026-08-10', paidAt: '2026-08-10T10:15', amount: 2500.0, generatedKey: `${l1}:2026-08` },
    { id: createId(), leaseId: l2, dueDate: '2026-08-05', paidAt: '2026-08-04T16:45', amount: 3200.0, generatedKey: `${l2}:2026-08` },
    // Mês atual - Vencendo em breve (Due)
    { id: createId(), leaseId: l1, dueDate: '2026-09-10', paidAt: null, amount: 2500.0, generatedKey: `${l1}:2026-09` },
    { id: createId(), leaseId: l3, dueDate: '2026-09-15', paidAt: null, amount: 4500.0, generatedKey: `${l3}:2026-09` },
    // Vencido (Overdue)
    { id: createId(), leaseId: l2, dueDate: '2026-08-25', paidAt: null, amount: 3200.0, generatedKey: `${l2}:2026-08-late` },
  ]);

  log.info('Banco de demonstração populado', {
    tenants: 5,
    properties: 4,
    leases: 4,
    receipts: 6,
    user: 'passoz',
  });
}

if (import.meta.main) {
  await seedDemoData();
}