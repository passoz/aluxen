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
          <div className="logo">
            <div className="logo-icon" aria-hidden="true">
              <svg className="logo-mark" viewBox="0 0 64 64" fill="none" xmlns="http://www.w3.org/2000/svg">
                <path d="M10 31L32 13L54 31" className="logo-stroke" strokeWidth="4.5" strokeLinecap="round" strokeLinejoin="round" />
                <path d="M17 29V49C17 51.2 18.8 53 21 53H43C45.2 53 47 51.2 47 49V29" className="logo-stroke" strokeWidth="4.5" strokeLinecap="round" strokeLinejoin="round" />
                <path d="M26 53V39C26 36.8 27.8 35 30 35H34C36.2 35 38 36.8 38 39V53" className="logo-stroke" strokeWidth="4.5" strokeLinecap="round" strokeLinejoin="round" />
                <circle cx="32" cy="22" r="3.2" className="logo-fill" />
              </svg>
            </div>
            <div className="logo-text">
              Alux<span>en</span>
            </div>
          </div>
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
      </div>
    </div>
  );
}
