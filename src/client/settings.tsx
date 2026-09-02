import React, { useState } from 'react';

export interface SettingsProps {
  landlordName: string;
  landlordCpf: string;
  landlordEmail: string;
  username: string;
  message?: string;
  error?: string;
}

export function SettingsPage(props: SettingsProps) {
  const [busy, setBusy] = useState(false);

  return (
    <div className="settings-page">
      <div className="card stack-lg" style={{ maxWidth: 560, margin: '2rem auto' }}>
        <h1 style={{ fontSize: '1.5rem' }}>Configurações</h1>

        {props.message ? (
          <div className="success" style={{ padding: '0.75rem 1rem', background: 'var(--color2)', borderRadius: 8 }}>
            {props.message}
          </div>
        ) : null}
        {props.error ? (
          <div className="error" style={{ marginTop: 0 }}>
            {props.error}
          </div>
        ) : null}

        <div className="stack">
          <h2 style={{ fontSize: '1.1rem' }}>Dados do Locador</h2>
          <form method="post" action="/settings" className="stack" onSubmit={() => setBusy(true)}>
            <input type="hidden" name="intent" value="save-landlord" />
            <div className="form-group">
              <label htmlFor="landlord-name">Nome</label>
              <input id="landlord-name" name="name" defaultValue={props.landlordName} required />
            </div>
            <div className="form-group">
              <label htmlFor="landlord-cpf">CPF</label>
              <input id="landlord-cpf" name="cpf" defaultValue={props.landlordCpf} />
            </div>
            <div className="form-group">
              <label htmlFor="landlord-email">Email</label>
              <input id="landlord-email" name="email" type="email" defaultValue={props.landlordEmail} />
            </div>
            <button type="submit" className="btn btn-primary" disabled={busy}>
              Salvar dados
            </button>
          </form>
        </div>

        <hr style={{ border: 'none', borderTop: '1px solid var(--glass-border)' }} />

        <div className="stack">
          <h2 style={{ fontSize: '1.1rem' }}>Alterar Senha</h2>
          <form method="post" action="/settings" className="stack" onSubmit={() => setBusy(true)}>
            <input type="hidden" name="intent" value="change-password" />
            <div className="form-group">
              <label htmlFor="current-password">Senha atual</label>
              <input id="current-password" name="current_password" type="password" required />
            </div>
            <div className="form-group">
              <label htmlFor="new-password">Nova senha</label>
              <input id="new-password" name="new_password" type="password" required minLength={4} />
            </div>
            <div className="form-group">
              <label htmlFor="confirm-password">Confirmar nova senha</label>
              <input id="confirm-password" name="confirm_password" type="password" required minLength={4} />
            </div>
            <button type="submit" className="btn btn-primary" disabled={busy}>
              Alterar senha
            </button>
          </form>
        </div>

        <div style={{ textAlign: 'center' }}>
          <a href="/dashboard?tab=tenants" className="btn btn-sm btn-secondary">
            Voltar ao painel
          </a>
        </div>
      </div>
    </div>
  );
}
