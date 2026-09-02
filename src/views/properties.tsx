import type { FC } from 'hono/jsx';
import { Layout } from './layout';
import type { Property } from '../db/schema';

interface PropertiesPageProps {
  properties: Property[];
  error?: string;
  success?: string;
}

export const PropertiesPage: FC<PropertiesPageProps> = ({ properties, error, success }) => {
  return (
    <Layout title="Aluxen | Imóveis">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '2rem' }}>
        <div>
          <h1 style={{ fontSize: '1.875rem', fontWeight: 700, marginBottom: '0.5rem' }}>Imóveis</h1>
          <p style={{ color: 'var(--text-muted)' }}>Gerencie as propriedades disponíveis para locação.</p>
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
          <h2 style={{ fontSize: '1.25rem', fontWeight: 600, marginBottom: '1rem' }}>Novo Imóvel</h2>
          <form method="post" action="/properties">
            <div style={{ marginBottom: '1rem' }}>
              <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Identificação / Nome *</label>
              <input type="text" name="name" placeholder="Ex: Apartamento 101" required style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }} />
            </div>

            <div style={{ marginBottom: '1rem' }}>
              <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Endereço Completo *</label>
              <input type="text" name="address" placeholder="Rua, número, complemento" required style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }} />
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: '2fr 1fr', gap: '0.75rem', marginBottom: '1rem' }}>
              <div>
                <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Cidade *</label>
                <input type="text" name="city" required style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }} />
              </div>
              <div>
                <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>UF *</label>
                <input type="text" name="state" maxLength={2} placeholder="SP" required style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }} />
              </div>
            </div>

            <div style={{ marginBottom: '1.5rem' }}>
              <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>CEP</label>
              <input type="text" name="zip" placeholder="00000-000" style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }} />
            </div>

            <button type="submit" class="btn" style={{ width: '100%' }}>Cadastrar Imóvel</button>
          </form>
        </div>

        <div class="card">
          <h2 style={{ fontSize: '1.25rem', fontWeight: 600, marginBottom: '1rem' }}>Lista de Imóveis ({properties.length})</h2>
          {properties.length === 0 ? (
            <p style={{ color: 'var(--text-muted)', padding: '1rem 0' }}>Nenhum imóvel cadastrado.</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Nome</th>
                  <th>Endereço</th>
                  <th>Cidade/UF</th>
                  <th>CEP</th>
                  <th>Ações</th>
                </tr>
              </thead>
              <tbody>
                {properties.map((prop) => (
                  <tr key={prop.id}>
                    <td><strong>{prop.name}</strong></td>
                    <td>{prop.address}</td>
                    <td>{prop.city}/{prop.state}</td>
                    <td>{prop.zip || '—'}</td>
                    <td>
                      <form method="post" action={`/properties/${prop.id}/delete`} style={{ display: 'inline' }} onsubmit="return confirm('Excluir este imóvel?');">
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
