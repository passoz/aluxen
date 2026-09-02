import type { FC } from 'hono/jsx';
import { Layout } from './layout';
import type { Property, Tenant } from '../db/schema';

interface HomeProps {
  stats: {
    tenantsCount: number;
    propertiesCount: number;
    activeLeasesCount: number;
    receiptsCount: number;
  };
  recentTenants: Tenant[];
  recentProperties: Property[];
}

export const Home: FC<HomeProps> = ({ stats, recentTenants, recentProperties }) => {
  return (
    <Layout title="Aluxen | Dashboard">
      <div style={{ marginBottom: '2rem' }}>
        <h1 style={{ fontSize: '1.875rem', fontWeight: 700, marginBottom: '0.5rem' }}>Painel Geral</h1>
        <p style={{ color: 'var(--text-muted)' }}>Gestão simplificada de locações, imóveis e contratos.</p>
      </div>

      <div class="grid">
        <div class="stat-card">
          <div class="stat-label">Inquilinos</div>
          <div class="stat-value">{stats.tenantsCount}</div>
        </div>
        <div class="stat-card">
          <div class="stat-label">Imóveis</div>
          <div class="stat-value">{stats.propertiesCount}</div>
        </div>
        <div class="stat-card">
          <div class="stat-label">Locações Ativas</div>
          <div class="stat-value">{stats.activeLeasesCount}</div>
        </div>
        <div class="stat-card">
          <div class="stat-label">Recibos</div>
          <div class="stat-value">{stats.receiptsCount}</div>
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(400px, 1fr))', gap: '1.5rem' }}>
        <div class="card">
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
            <h2 style={{ fontSize: '1.25rem', fontWeight: 600 }}>Inquilinos Recentes</h2>
            <a href="/tenants" class="btn" style={{ fontSize: '0.875rem', padding: '0.35rem 0.75rem' }}>Ver todos</a>
          </div>
          {recentTenants.length === 0 ? (
            <p style={{ color: 'var(--text-muted)', padding: '1rem 0' }}>Nenhum inquilino cadastrado.</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Nome</th>
                  <th>E-mail</th>
                  <th>Telefone</th>
                </tr>
              </thead>
              <tbody>
                {recentTenants.map((tenant) => (
                  <tr key={tenant.id}>
                    <td>{tenant.name}</td>
                    <td>{tenant.email}</td>
                    <td>{tenant.phone || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>

        <div class="card">
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
            <h2 style={{ fontSize: '1.25rem', fontWeight: 600 }}>Imóveis Recentes</h2>
            <a href="/properties" class="btn" style={{ fontSize: '0.875rem', padding: '0.35rem 0.75rem' }}>Ver todos</a>
          </div>
          {recentProperties.length === 0 ? (
            <p style={{ color: 'var(--text-muted)', padding: '1rem 0' }}>Nenhum imóvel cadastrado.</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Nome</th>
                  <th>Endereço</th>
                  <th>Cidade/UF</th>
                </tr>
              </thead>
              <tbody>
                {recentProperties.map((prop) => (
                  <tr key={prop.id}>
                    <td>{prop.name}</td>
                    <td>{prop.address}</td>
                    <td>{prop.city}/{prop.state}</td>
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
