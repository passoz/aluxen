import type { FC } from 'hono/jsx';
import { Layout } from './layout';
import type { Lease, Property, Receipt, Tenant } from '../db/schema';

interface ReceiptsPageProps {
  receipts: (Receipt & {
    lease?: (Lease & { tenant?: Tenant | null; property?: Property | null }) | null;
  })[];
  activeLeases: (Lease & { tenant?: Tenant | null; property?: Property | null })[];
  error?: string;
  success?: string;
}

export const ReceiptsPage: FC<ReceiptsPageProps> = ({ receipts, activeLeases, error, success }) => {
  return (
    <Layout title="Aluxen | Recibos">
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '2rem' }}>
        <div>
          <h1 style={{ fontSize: '1.875rem', fontWeight: 700, marginBottom: '0.5rem' }}>Recibos</h1>
          <p style={{ color: 'var(--text-muted)' }}>Controle de cobranças e recibos de pagamento de aluguel.</p>
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
          <h2 style={{ fontSize: '1.25rem', fontWeight: 600, marginBottom: '1rem' }}>Gerar Recibo</h2>
          <form method="post" action="/receipts">
            <div style={{ marginBottom: '1rem' }}>
              <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Locação *</label>
              <select name="leaseId" required style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }}>
                <option value="">Selecione a locação</option>
                {activeLeases.map(l => (
                  <option key={l.id} value={l.id}>
                    {l.tenant?.name || 'Inquilino'} - {l.property?.name || 'Imóvel'} (R$ {l.rentAmount.toFixed(2)})
                  </option>
                ))}
              </select>
            </div>

            <div style={{ marginBottom: '1rem' }}>
              <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Data Vencimento *</label>
              <input type="date" name="dueDate" required style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }} />
            </div>

            <div style={{ marginBottom: '1.5rem' }}>
              <label style={{ display: 'block', fontSize: '0.875rem', color: 'var(--text-muted)', marginBottom: '0.25rem' }}>Valor Cobrado (R$) *</label>
              <input type="number" step="0.01" name="amount" placeholder="1500.00" required style={{ width: '100%', padding: '0.5rem 0.75rem', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: '0.25rem', color: 'var(--text)' }} />
            </div>

            <button type="submit" class="btn" style={{ width: '100%' }}>Gerar Recibo</button>
          </form>
        </div>

        <div class="card">
          <h2 style={{ fontSize: '1.25rem', fontWeight: 600, marginBottom: '1rem' }}>Histórico de Recibos ({receipts.length})</h2>
          {receipts.length === 0 ? (
            <p style={{ color: 'var(--text-muted)', padding: '1rem 0' }}>Nenhum recibo gerado.</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Inquilino / Imóvel</th>
                  <th>Vencimento</th>
                  <th>Valor</th>
                  <th>Status</th>
                  <th>Ações</th>
                </tr>
              </thead>
              <tbody>
                {receipts.map((receipt) => {
                  const isPaid = !!receipt.paidAt;
                  return (
                    <tr key={receipt.id}>
                      <td>
                        <strong>{receipt.lease?.tenant?.name || '—'}</strong>
                        <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>{receipt.lease?.property?.name || '—'}</div>
                      </td>
                      <td>{receipt.dueDate}</td>
                      <td>R$ {receipt.amount.toFixed(2)}</td>
                      <td>
                        <span class={`badge ${isPaid ? 'badge-active' : 'badge-inactive'}`}>
                          {isPaid ? `Pago em ${receipt.paidAt}` : 'Pendente'}
                        </span>
                      </td>
                      <td>
                        <div style={{ display: 'flex', gap: '0.5rem' }}>
                          {!isPaid && (
                            <form method="post" action={`/receipts/${receipt.id}/pay`}>
                              <button type="submit" class="btn" style={{ background: 'var(--accent)', padding: '0.25rem 0.5rem', fontSize: '0.75rem' }}>Pagar</button>
                            </form>
                          )}
                          <form method="post" action={`/receipts/${receipt.id}/delete`} onsubmit="return confirm('Excluir este recibo?');">
                            <button type="submit" class="btn" style={{ background: 'var(--danger)', padding: '0.25rem 0.5rem', fontSize: '0.75rem' }}>Excluir</button>
                          </form>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </Layout>
  );
};
