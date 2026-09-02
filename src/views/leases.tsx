import type { FC } from 'hono/jsx';
import { Layout } from './layout';
import type { Lease, Property, Tenant } from '../db/schema';

interface LeasesPageProps {
  leases: (Lease & { tenant?: Tenant | null; property?: Property | null })[];
  tenants: Tenant[];
  properties: Property[];
  error?: string;
  success?: string;
}

export const LeasesPage: FC<LeasesPageProps> = ({ leases, tenants, properties, error, success }) => {
  return (
    <Layout title="Aluxen | Locações">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '2rem' }}>
        <div>
          <h1 style={{ fontSize: '1.875rem', fontWeight: 700, marginBottom: '0.5rem' }}>Locações</h1>
          <p style={{ color: 'var(--text-muted)' }}>Contratos de aluguel ativos e encerrados.</p>
        </div>
      </div>

      {error && (
        <div style={{ background: '#7f1d1d', color: '#fca5a5', padding: '0.75rem 1rem', borderRadius: '0.375rem', marginBottom: '1.5rem' }}>
          {error}
        </div>
      )}

      {success && (
        <div style={{ background: '#065f46', color: '#6ee7b7', padding: '0.75rem 1rem', borderRadius: '0.375rem', marginBottom: '1.5rem' }}>
          {success}
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 2fr', gap: '2rem', alignItems: 'start' }}>
        <div class="card">
          <h2 style={{ fontSize: '1.25rem', fontWeight: 600, marginBottom: '1rem' }}>Novo Contrato de Locação</h2>
          <form method="post" action="/leases">
            <div style={{ marginBottom: '1rem' }}>
              <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Inquilino *</label>
              <select name="tenantId" required style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }}>
                <option value="">Selecione um inquilino</option>
                {tenants.map(t => (
                  <option key={t.id} value={t.id}>{t.name} ({t.email})</option>
                ))}
              </select>
            </div>

            <div style={{ marginBottom: '1rem' }}>
              <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Imóvel *</label>
              <select name="propertyId" required style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }}>
                <option value="">Selecione um imóvel</option>
                {properties.map(p => (
                  <option key={p.id} value={p.id}>{p.name} - {p.city}/{p.state}</option>
                ))}
              </select>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem', marginBottom: '1rem' }}>
              <div>
                <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Data Início *</label>
                <input type="date" name="startDate" required style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }} />
              </div>
              <div>
                <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Data Fim *</label>
                <input type="date" name="endDate" required style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }} />
              </div>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem', marginBottom: '1.5rem' }}>
              <div>
                <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Dia Vencimento *</label>
                <input type="number" name="dueDay" min="1" max="31" defaultValue="5" required style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }} />
              </div>
              <div>
                <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Valor Mensal (R$) *</label>
                <input type="number" step="0.01" name="rentAmount" placeholder="1500.00" required style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }} />
              </div>
            </div>

            <button type="submit" class="btn" style={{ width: '100%' }}>Criar Locação</button>
          </form>
        </div>

        <div class="card">
          <h2 style={{ fontSize: '1.25rem', fontWeight: 600, marginBottom: '1rem' }}>Locações ({leases.length})</h2>
          {leases.length === 0 ? (
            <p style={{ color: 'var(--text-muted)', padding: '1rem 0' }}>Nenhuma locação cadastrada.</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Inquilino</th>
                  <th>Imóvel</th>
                  <th>Vigência</th>
                  <th>Aluguel</th>
                  <th>Status</th>
                  <th>Ações</th>
                </tr>
              </thead>
              <tbody>
                {leases.map((lease) => (
                  <tr key={lease.id}>
                    <td>{lease.tenant?.name || lease.tenantId}</td>
                    <td>{lease.property?.name || lease.propertyId}</td>
                    <td>{lease.startDate} até {lease.endDate} (Dia {lease.dueDay})</td>
                    <td>R$ {lease.rentAmount.toFixed(2)}</td>
                    <td>
                      <span class={`badge ${lease.status === 'active' ? 'badge-active' : 'badge-inactive'}`}>
                        {lease.status}
                      </span>
                    </td>
                    <td>
                      <form method="post" action={`/leases/${lease.id}/delete`} style={{ display: 'inline' }} onsubmit="return confirm('Excluir esta locação?');">
                        <button type="submit" class="btn" style={{ background: 'var(--danger)', padding: '0.25rem 0.5rem', fontSize: '0.75rem' }}>Excluir</button>
                      </form>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </Layout>
  );
};
