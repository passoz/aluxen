import React, { useState } from 'react';

export interface LoginProps {
  error?: string;
}

export function LoginPage({ error: initialError }: LoginProps) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(initialError);

  const handleSubmit = (e: React.FormEvent<HTMLFormElement>) => {
    setBusy(true);
  };

  return (
    <div className="login-page">
      <div className="card login-card">
        <div className="brand-lockup">
          <img src="/logomarca.png" alt="Aluxen" className="login-logomarca" />
        </div>

        <form method="post" action="/login" className="stack" onSubmit={handleSubmit}>
          {error ? <div className="error">{error}</div> : null}
          <div className="form-group">
            <label htmlFor="username">Usuário</label>
            <input id="username" name="username" type="text" required autoFocus />
          </div>
          <div className="form-group">
            <label htmlFor="password">Senha</label>
            <input id="password" name="password" type="password" required />
          </div>
          <button type="submit" className="btn btn-primary" disabled={busy}>
            {busy ? 'Entrando...' : 'Entrar'}
          </button>
        </form>

        <p className="subtitle" style={{ marginTop: '1.25rem', marginBottom: 0, textAlign: 'center', fontSize: '0.85rem' }}>
          <a href="/portal/login">Área do Cliente</a>
        </p>
      </div>
    </div>
  );
}
