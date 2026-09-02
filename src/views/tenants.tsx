import type { FC } from 'hono/jsx';
import { Layout } from './layout';
import type { Tenant } from '../db/schema';

interface TenantsPageProps {
  tenants: Tenant[];
  error?: string;
  success?: string;
}

export const TenantsPage: FC<TenantsPageProps> = ({ tenants, error, success }) => {
  return (
    <Layout title="Aluxen | Inquilinos">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '2rem' }}>
        <div>
          <h1 style={{ fontSize: '1.875rem', fontWeight: 700, marginBottom: '0.5rem' }}>Inquilinos</h1>
          <p style={{ color: 'var(--text-muted)' }}>Gerencie os locatários cadastrados no sistema.</p>
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
          <h2 style={{ fontSize: '1.25rem', fontWeight: 600, marginBottom: '1rem' }}>Novo Inquilino</h2>
          <form method="post" action="/tenants">
            <div style={{ marginBottom: '1rem' }}>
              <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Nome Completo *</label>
              <input type="text" name="name" required style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }} />
            </div>

            <div style={{ marginBottom: '1rem' }}>
              <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>E-mail *</label>
              <input type="email" name="email" required style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }} />
            </div>

            <div style={{ marginBottom: '1rem' }}>
              <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Telefone</label>
              <input type="tel" name="phone" placeholder="(00) 00000-0000" style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }} />
            </div>

            <div style={{ marginBottom: '1.5rem' }}>
              <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>CPF</label>
              <input type="text" name="cpf" placeholder="000.000.000-00" style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }} />
            </div>

            <button type="submit" class="btn" style={{ width: '100%' }}>Cadastrar Inquilino</button>
          </form>
        </div>

        <div class="card">
          <h2 style={{ fontSize: '1.25rem', fontWeight: 600, marginBottom: '1rem' }}>Lista de Inquilinos ({tenants.length})</h2>
          {tenants.length === 0 ? (
            <p style={{ color: 'var(--text-muted)', padding: '1rem 0' }}>Nenhum inquilino cadastrado.</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Nome</th>
                  <th>E-mail</th>
                  <th>Telefone</th>
                  <th>CPF</th>
                  <th>Ações</th>
                </tr>
              </thead>
              <tbody>
                {tenants.map((tenant) => (
                  <tr key={tenant.id}>
                    <td><strong>{tenant.name}</strong></td>
                    <td>{tenant.email}</td>
                    <td>{tenant.phone || '—'}</td>
                    <td>{tenant.cpf || '—'}</td>
                    <td>
                      <form method="post" action={`/tenants/${tenant.id}/delete`} style={{ display: 'inline' }} onsubmit="return confirm('Excluir este inquilino?');">
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
