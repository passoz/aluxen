import React, { useState } from 'react';

/** Contrato do locatário na Área do Cliente (somente leitura). */
export interface ClientLeaseView {
  id: string;
  propertyName: string;
  propertyAddress: string;
  period: string;
  dueDay: number;
  rentAmount: string;
  status: string;
  statusLabel: string;
  pdfUrl: string;
  validationUrl: string;
}

/** Recibo já quitado do locatário (somente leitura). */
export interface ClientReceiptView {
  id: string;
  reference: string;
  propertyName: string;
  dueDate: string;
  paidAt: string;
  amountPaid: string;
  isPartial: boolean;
  pdfUrl: string;
  validationUrl: string;
}

/** Classe de badge usada para a situação do contrato na Área do Cliente. */
const LEASE_BADGE_CLASS: Record<string, string> = {
  active: 'paid',
  suspended: 'due',
  season: 'partial',
  terminated: 'terminated',
};

export interface ClientPortalProps {
  tenantName: string;
  tenantCpf: string;
  leases: ClientLeaseView[];
  receipts: ClientReceiptView[];
  message?: string;
  error?: string;
}

export interface ClientLoginProps {
  error?: string;
  cpf?: string;
}

export interface ClientPasswordProps {
  firstAccess?: boolean;
  message?: string;
  error?: string;
}

/** Rodapé de identidade da Área do Cliente, com as ações disponíveis. */
function ClientHeader({ tenantName, tenantCpf }: { tenantName: string; tenantCpf: string }) {
  return (
    <header className="stack" style={{ gap: '0.75rem' }}>
      <div className="brand-lockup" style={{ marginBottom: 0 }}>
        <img src="/logomarca.png" alt="Aluxen" className="login-logomarca" />
      </div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-end', gap: '1rem', flexWrap: 'wrap' }}>
        <div>
          <h1 style={{ fontSize: '1.4rem', margin: 0 }}>Área do Cliente</h1>
          <p className="subtitle" style={{ margin: '0.25rem 0 0' }}>
            {tenantName}
            {tenantCpf ? ` • CPF ${tenantCpf}` : ''}
          </p>
        </div>
        <div className="auth-actions">
          <a className="btn btn-outline" href="/portal/password">
            Trocar senha
          </a>
          <form method="post" action="/portal/logout" style={{ display: 'inline' }}>
            <button type="submit" className="btn btn-secondary">
              Sair
            </button>
          </form>
        </div>
      </div>
    </header>
  );
}

export function ClientLoginPage({ error: initialError, cpf }: ClientLoginProps) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(initialError);

  return (
    <div className="login-page">
      <div className="card login-card">
        <div className="brand-lockup">
          <img src="/logomarca.png" alt="Aluxen" className="login-logomarca" />
          <p className="subtitle" style={{ margin: 0 }}>
            Área do Cliente
          </p>
        </div>

        <form method="post" action="/portal/login" className="stack" onSubmit={() => setBusy(true)}>
          {error ? <div className="error">{error}</div> : null}
          <div className="form-group">
            <label htmlFor="cpf">CPF</label>
            <input
              id="cpf"
              name="cpf"
              type="text"
              inputMode="numeric"
              autoComplete="username"
              placeholder="000.000.000-00"
              defaultValue={cpf}
              required
              autoFocus
            />
          </div>
          <div className="form-group">
            <label htmlFor="password">Senha</label>
            <input id="password" name="password" type="password" autoComplete="current-password" required />
            <small style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>
              No primeiro acesso a senha é o seu CPF (somente números).
            </small>
          </div>
          <button type="submit" className="btn btn-primary" disabled={busy}>
            {busy ? 'Entrando...' : 'Entrar'}
          </button>
        </form>

        <p className="subtitle" style={{ marginTop: '1.25rem', marginBottom: 0, textAlign: 'center', fontSize: '0.85rem' }}>
          <a href="/login">Acesso administrativo</a>
        </p>
      </div>
    </div>
  );
}

export function ClientPortalPage(props: ClientPortalProps) {
  const { leases, receipts } = props;

  return (
    <div className="settings-page" style={{ alignItems: 'flex-start' }}>
      <div className="card stack-lg" style={{ width: '100%', maxWidth: 880 }}>
        <ClientHeader tenantName={props.tenantName} tenantCpf={props.tenantCpf} />

        {props.message ? (
          <div className="success" style={{ padding: '0.75rem 1rem', background: 'var(--color2)', borderRadius: 8 }}>
            {props.message}
          </div>
        ) : null}
        {props.error ? <div className="error" style={{ marginTop: 0 }}>{props.error}</div> : null}

        <section className="stack">
          <h2 style={{ fontSize: '1.1rem' }}>Meus contratos</h2>
          {leases.length === 0 ? (
            <p className="subtitle" style={{ margin: 0 }}>
              Nenhum contrato vinculado ao seu cadastro.
            </p>
          ) : (
            <div className="stack">
              {leases.map((lease) => (
                <div
                  key={lease.id}
                  className="card"
                  style={{ padding: '1rem 1.25rem', display: 'grid', gap: '0.35rem' }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
                    <strong style={{ fontSize: '1rem' }}>{lease.propertyName}</strong>
                    <span className={`status-badge ${LEASE_BADGE_CLASS[lease.status] || 'due'}`}>{lease.statusLabel}</span>
                  </div>
                  {lease.propertyAddress ? (
                    <span style={{ color: 'var(--text-muted)', fontSize: '0.875rem' }}>{lease.propertyAddress}</span>
                  ) : null}
                  <span style={{ color: 'var(--text-muted)', fontSize: '0.875rem' }}>
                    Vigência: {lease.period} &bull; Vencimento todo dia {lease.dueDay} &bull; Aluguel {lease.rentAmount}
                  </span>
                  <div className="auth-actions" style={{ marginTop: '0.5rem' }}>
                    <a className="btn btn-outline btn-sm" href={lease.pdfUrl} target="_blank" rel="noreferrer">
                      Ver contrato (PDF)
                    </a>
                    <a className="btn btn-outline btn-sm" href={lease.validationUrl} target="_blank" rel="noreferrer">
                      Autenticidade
                    </a>
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>

        <section className="stack">
          <h2 style={{ fontSize: '1.1rem' }}>Meus recibos quitados</h2>
          {receipts.length === 0 ? (
            <p className="subtitle" style={{ margin: 0 }}>
              Nenhum recibo quitado até o momento.
            </p>
          ) : (
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Referência</th>
                    <th>Imóvel</th>
                    <th>Vencimento</th>
                    <th>Pago em</th>
                    <th>Valor pago</th>
                    <th style={{ textAlign: 'right' }}>Documento</th>
                  </tr>
                </thead>
                <tbody>
                  {receipts.map((receipt) => (
                    <tr key={receipt.id}>
                      <td data-label="Referência">
                        {receipt.reference}
                        {receipt.isPartial ? <span className="status-badge partial" style={{ marginLeft: '0.5rem' }}>Parcial</span> : null}
                      </td>
                      <td data-label="Imóvel">{receipt.propertyName || '—'}</td>
                      <td data-label="Vencimento">{receipt.dueDate}</td>
                      <td data-label="Pago em">{receipt.paidAt}</td>
                      <td data-label="Valor pago">{receipt.amountPaid}</td>
                      <td data-label="Documento" style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
                        <a className="btn btn-outline btn-sm" href={receipt.pdfUrl} target="_blank" rel="noreferrer">
                          PDF
                        </a>
                        <a className="btn btn-outline btn-sm" href={receipt.validationUrl} target="_blank" rel="noreferrer" style={{ marginLeft: '0.35rem' }}>
                          Autenticidade
                        </a>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>
    </div>
  );
}

export function ClientPasswordPage({ firstAccess, message, error }: ClientPasswordProps) {
  const [busy, setBusy] = useState(false);

  return (
    <div className="settings-page">
      <div className="card stack" style={{ width: '100%', maxWidth: 440, padding: '2.25rem' }}>
        <h1 style={{ fontSize: '1.35rem', margin: 0 }}>Trocar senha</h1>
        <p className="subtitle" style={{ margin: 0 }}>
          {firstAccess
            ? 'Você entrou com a senha padrão (seu CPF). Defina uma senha pessoal para continuar.'
            : 'Informe sua senha atual e escolha uma nova senha.'}
        </p>

        {message ? <div className="success">{message}</div> : null}
        {error ? <div className="error" style={{ marginTop: 0 }}>{error}</div> : null}

        <form method="post" action="/portal/password" className="stack" onSubmit={() => setBusy(true)}>
          <div className="form-group">
            <label htmlFor="current_password">Senha atual</label>
            <input id="current_password" name="current_password" type="password" autoComplete="current-password" required autoFocus />
            {firstAccess ? (
              <small style={{ color: 'var(--text-muted)', fontSize: '0.8rem' }}>No primeiro acesso, informe o seu CPF (somente números).</small>
            ) : null}
          </div>
          <div className="form-group">
            <label htmlFor="new_password">Nova senha</label>
            <input id="new_password" name="new_password" type="password" autoComplete="new-password" minLength={6} required />
          </div>
          <div className="form-group">
            <label htmlFor="confirm_password">Confirmar nova senha</label>
            <input id="confirm_password" name="confirm_password" type="password" autoComplete="new-password" minLength={6} required />
          </div>
          <button type="submit" className="btn btn-primary" disabled={busy}>
            {busy ? 'Salvando...' : 'Salvar nova senha'}
          </button>
        </form>

        <p className="subtitle" style={{ margin: 0, textAlign: 'center', fontSize: '0.85rem' }}>
          <a href="/portal">Voltar para a Área do Cliente</a>
        </p>
      </div>
    </div>
  );
}
