import { initializeDatabase, getSqlite, execute, setSetting } from './index';
import { createId } from '../utils/id';

export async function seedDemoData(): Promise<void> {
  await initializeDatabase();
  const client = getSqlite();

  console.log('Populando banco de dados de teste com dados de demonstração...');

  // Limpar tabelas para dados frescos de teste (mantendo usuário admin)
  client.run('DELETE FROM receipts');
  client.run('DELETE FROM leases');
  client.run('DELETE FROM properties');
  client.run('DELETE FROM tenants');

  // Configurações do Locador
  await setSetting('landlord_name', 'Fernando Henrique Passos de Souza');
  await setSetting('landlord_cpf', '075.898.017-55');
  await setSetting('landlord_email', 'contato@evolucsia.com');

  // 1. Inquilinos
  const t1 = createId();
  const t2 = createId();
  const t3 = createId();
  const t4 = createId();
  const t5 = createId();

  await execute(
    'INSERT INTO tenants (id, name, email, phone, cpf) VALUES (?, ?, ?, ?, ?)',
    [t1, 'Carlos Drummond de Andrade', 'carlos.drummond@exemplo.com.br', '(22) 99888-1122', '111.222.333-44']
  );
  await execute(
    'INSERT INTO tenants (id, name, email, phone, cpf) VALUES (?, ?, ?, ?, ?)',
    [t2, 'Clarice Lispector', 'clarice.lispector@exemplo.com.br', '(22) 99777-2233', '222.333.444-55']
  );
  await execute(
    'INSERT INTO tenants (id, name, email, phone, cpf) VALUES (?, ?, ?, ?, ?)',
    [t3, 'Machado de Assis', 'machado.assis@exemplo.com.br', '(22) 99666-3344', '333.444.555-66']
  );
  await execute(
    'INSERT INTO tenants (id, name, email, phone, cpf) VALUES (?, ?, ?, ?, ?)',
    [t4, 'Cecília Meireles', 'cecilia.meireles@exemplo.com.br', '(22) 99555-4455', '444.555.666-77']
  );
  await execute(
    'INSERT INTO tenants (id, name, email, phone, cpf) VALUES (?, ?, ?, ?, ?)',
    [t5, 'Vinicius de Moraes', 'vinicius.moraes@exemplo.com.br', '(22) 99444-5566', '555.666.777-88']
  );

  // 2. Imóveis
  const p1 = createId();
  const p2 = createId();
  const p3 = createId();
  const p4 = createId();

  await execute(
    'INSERT INTO properties (id, name, address, city, state, zip) VALUES (?, ?, ?, ?, ?, ?)',
    [p1, 'Apto 101 - Praia do Forte', 'Av. do Contorno, 250, Apto 101', 'Cabo Frio', 'RJ', '28907-250']
  );
  await execute(
    'INSERT INTO properties (id, name, address, city, state, zip) VALUES (?, ?, ?, ?, ?, ?)',
    [p2, 'Casa das Dunas', 'Rua dos Diamantes, 14, Braga', 'Cabo Frio', 'RJ', '28908-120']
  );
  await execute(
    'INSERT INTO properties (id, name, address, city, state, zip) VALUES (?, ?, ?, ?, ?, ?)',
    [p3, 'Cobertura Caiçara', 'Rua Alemanha, 132, Cobertura 01, Jardim Caiçara', 'Cabo Frio', 'RJ', '28910-370']
  );
  await execute(
    'INSERT INTO properties (id, name, address, city, state, zip) VALUES (?, ?, ?, ?, ?, ?)',
    [p4, 'Flat Passagem Histórica', 'Largo de São Benedito, 88, Passagem', 'Cabo Frio', 'RJ', '28906-030']
  );

  // 3. Contratos de Locação (Leases)
  const l1 = createId();
  const l2 = createId();
  const l3 = createId();
  const l4 = createId();

  await execute(
    'INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)',
    [l1, t1, p1, '2026-01-01', '2026-12-31', 10, 2500.0, 'active']
  );
  await execute(
    'INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)',
    [l2, t2, p2, '2026-02-01', '2027-01-31', 5, 3200.0, 'active']
  );
  await execute(
    'INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)',
    [l3, t3, p3, '2026-03-01', '2027-02-28', 15, 4500.0, 'active']
  );
  await execute(
    'INSERT INTO leases (id, tenant_id, property_id, start_date, end_date, due_day, rent_amount, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)',
    [l4, t4, p4, '2025-01-01', '2025-12-31', 1, 1800.0, 'terminated']
  );

  // 4. Recibos / Pagamentos
  // Pagos em meses anteriores
  await execute(
    'INSERT INTO receipts (id, lease_id, due_date, paid_at, amount, generated_key) VALUES (?, ?, ?, ?, ?, ?)',
    [createId(), l1, '2026-07-10', '2026-07-09T14:30', 2500.0, `${l1}:2026-07`]
  );
  await execute(
    'INSERT INTO receipts (id, lease_id, due_date, paid_at, amount, generated_key) VALUES (?, ?, ?, ?, ?, ?)',
    [createId(), l1, '2026-08-10', '2026-08-10T10:15', 2500.0, `${l1}:2026-08`]
  );
  await execute(
    'INSERT INTO receipts (id, lease_id, due_date, paid_at, amount, generated_key) VALUES (?, ?, ?, ?, ?, ?)',
    [createId(), l2, '2026-08-05', '2026-08-04T16:45', 3200.0, `${l2}:2026-08`]
  );

  // Mês atual - Vencendo em breve (Due)
  await execute(
    'INSERT INTO receipts (id, lease_id, due_date, paid_at, amount, generated_key) VALUES (?, ?, ?, NULL, ?, ?)',
    [createId(), l1, '2026-09-10', 2500.0, `${l1}:2026-09`]
  );
  await execute(
    'INSERT INTO receipts (id, lease_id, due_date, paid_at, amount, generated_key) VALUES (?, ?, ?, NULL, ?, ?)',
    [createId(), l3, '2026-09-15', 4500.0, `${l3}:2026-09`]
  );

  // Vencido (Overdue)
  await execute(
    'INSERT INTO receipts (id, lease_id, due_date, paid_at, amount, generated_key) VALUES (?, ?, ?, NULL, ?, ?)',
    [createId(), l2, '2026-08-25', 3200.0, `${l2}:2026-08-late`]
  );

  console.log('✓ Banco de demonstração populado com sucesso!');
  console.log('- 5 Inquilinos cadastrados');
  console.log('- 4 Imóveis cadastrados');
  console.log('- 4 Contratos de Locação (3 ativos, 1 encerrado)');
  console.log('- 6 Recibos de Pagamento (3 pagos, 2 a vencer, 1 vencido)');
  console.log('- Usuário: passoz | Senha: Dt32btop@');
}

if (import.meta.main) {
  await seedDemoData();
}
