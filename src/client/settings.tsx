import React, { useState, useEffect, useCallback } from 'react';
import { WhatsAppConnectModal, ConfirmModal } from '../components/ui';
import type { WhatsAppStatus } from '../whatsapp/types';

export interface SettingsProps {
  landlordName: string;
  landlordCpf: string;
  landlordEmail: string;
  appUrl?: string;
  detectedLanUrl?: string;
  username: string;
  message?: string;
  error?: string;
}

export function SettingsPage(props: SettingsProps) {
  const [busy, setBusy] = useState(false);
  const [appUrl, setAppUrl] = useState(props.appUrl || '');
  const [connectWhatsappOpen, setConnectWhatsappOpen] = useState(false);
  const [whatsappStatus, setWhatsappStatus] = useState<WhatsAppStatus | null>(null);
  const [loadingStatus, setLoadingStatus] = useState(true);
  const [disconnecting, setDisconnecting] = useState(false);
  const [logoutWhatsappConfirm, setLogoutWhatsappConfirm] = useState(false);
  const [statusMessage, setStatusMessage] = useState<string | null>(null);


  const fetchStatus = useCallback(async () => {
    try {
      setLoadingStatus(true);
      const res = await fetch('/api/whatsapp/status', { credentials: 'same-origin' });
      const data = (await res.json()) as WhatsAppStatus;
      setWhatsappStatus(data);
    } catch {
    } finally {
      setLoadingStatus(false);
    }
  }, []);

  useEffect(() => {
    void fetchStatus();
  }, [fetchStatus]);

  const handleLogoutWhatsApp = () => {
    setLogoutWhatsappConfirm(true);
  };

  const performLogoutWhatsApp = async () => {
    setLogoutWhatsappConfirm(false);
    setDisconnecting(true);
    setStatusMessage(null);
    try {
      const res = await fetch('/api/whatsapp/logout', { method: 'POST', credentials: 'same-origin' });
      const data = await res.json().catch(() => ({}));
      if (res.ok) {
        setStatusMessage(data.message || 'WhatsApp desconectado com sucesso.');
        await fetchStatus();
      } else {
        setStatusMessage(data.error || 'Erro ao desconectar WhatsApp.');
      }
    } catch {
      setStatusMessage('Erro ao desconectar WhatsApp.');
    } finally {
      setDisconnecting(false);
    }
  };

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

        {/* 1. Dados do Locador */}
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

        {/* 2. URL Base Pública (para QR Code e Validação) */}
        <div className="stack">
          <h2 style={{ fontSize: '1.1rem' }}>URL Pública do Sistema (QR Code)</h2>
          <p style={{ fontSize: '0.85rem', color: 'var(--text-muted)', margin: 0 }}>
            Endereço utilizado para gerar o link e o QR Code de validação nos recibos e contratos.
          </p>
          <form method="post" action="/settings" className="stack" onSubmit={() => setBusy(true)}>
            <input type="hidden" name="intent" value="save-app-url" />
            <div className="form-group">
              <label htmlFor="app-url">Endereço Web / Domínio</label>
              <input
                id="app-url"
                name="app_url"
                value={appUrl}
                onChange={(e) => setAppUrl(e.target.value)}
                placeholder="https://aluxen.evolucsia.com"
              />
            </div>
            <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap' }}>
              <button
                type="button"
                className="btn btn-sm btn-outline"
                onClick={() => setAppUrl('https://aluxen.evolucsia.com')}
              >
                Usar Produção (aluxen.evolucsia.com)
              </button>
              {props.detectedLanUrl ? (
                <button
                  type="button"
                  className="btn btn-sm btn-outline"
                  onClick={() => setAppUrl(props.detectedLanUrl || '')}
                >
                  Usar IP Wi-Fi ({props.detectedLanUrl})
                </button>
              ) : null}
            </div>
            <button type="submit" className="btn btn-primary" disabled={busy} style={{ marginTop: '0.5rem' }}>
              Salvar URL
            </button>
          </form>
        </div>

        <hr style={{ border: 'none', borderTop: '1px solid var(--glass-border)' }} />

        {/* 3. Robô do WhatsApp (Disparo Automático) */}
        <div className="stack">
          <h2 style={{ fontSize: '1.1rem' }}>Robô do WhatsApp (Disparo Automático)</h2>
          <p style={{ fontSize: '0.85rem', color: 'var(--text-muted)', margin: 0 }}>
            Conecte o WhatsApp Web para permitir que o sistema envie recibos e contratos automaticamente pelo robô em segundo plano.
          </p>

          {statusMessage ? (
            <div className="success" style={{ padding: '0.75rem 1rem', borderRadius: 8 }}>
              {statusMessage}
            </div>
          ) : null}

          <div
            style={{
              padding: '1rem',
              borderRadius: 'var(--radius-md)',
              background: 'var(--surface-subtle)',
              border: '1px solid var(--border)',
            }}
          >
            {loadingStatus ? (
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', color: 'var(--text-muted)' }}>
                <div className="loading-spinner" style={{ width: 14, height: 14 }} />
                <span>Verificando status do WhatsApp...</span>
              </div>
            ) : whatsappStatus?.isLoggedIn ? (
              <div className="stack" style={{ gap: '0.75rem' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <span style={{ fontSize: '1rem' }}>🟢</span>
                  <strong style={{ color: 'var(--primary)' }}>WhatsApp Conectado e Ativo</strong>
                </div>
                <p style={{ fontSize: '0.8125rem', color: 'var(--text-secondary)', margin: 0 }}>
                  O robô está pronto para disparar recibos e contratos de forma 100% headless.
                </p>
                <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', marginTop: '0.25rem' }}>
                  <button
                    type="button"
                    className="btn btn-sm btn-outline"
                    onClick={() => setConnectWhatsappOpen(true)}
                  >
                    Reconectar outro número
                  </button>
                  <button
                    type="button"
                    className="btn btn-sm btn-outline"
                    onClick={handleLogoutWhatsApp}
                    disabled={disconnecting}
                    style={{ color: 'var(--danger)', borderColor: 'rgba(239, 68, 68, 0.4)' }}
                  >
                    {disconnecting ? 'Desconectando...' : 'Desconectar WhatsApp'}
                  </button>
                </div>
              </div>
            ) : (
              <div className="stack" style={{ gap: '0.75rem' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                  <span style={{ fontSize: '1rem' }}>🔴</span>
                  <strong style={{ color: 'var(--text-muted)' }}>Nenhum WhatsApp Conectado</strong>
                </div>
                <p style={{ fontSize: '0.8125rem', color: 'var(--text-muted)', margin: 0 }}>
                  Faça login escaneando o QR Code pelo seu celular para habilitar o envio automático via robô.
                </p>
                <div>
                  <button
                    type="button"
                    className="btn btn-sm btn-primary"
                    onClick={() => setConnectWhatsappOpen(true)}
                  >
                    Conectar WhatsApp (Ler QR Code)
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>

        <WhatsAppConnectModal
          open={connectWhatsappOpen}
          onClose={() => setConnectWhatsappOpen(false)}
          onSuccess={() => {
            setStatusMessage('WhatsApp conectado com sucesso!');
            void fetchStatus();
          }}
        />

        <hr style={{ border: 'none', borderTop: '1px solid var(--glass-border)' }} />

        {/* 4. Alterar Senha */}
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

      <ConfirmModal
        open={logoutWhatsappConfirm}
        title="Desconectar WhatsApp"
        message="Deseja realmente desconectar o WhatsApp do robô?"
        confirmLabel="Desconectar"
        tone="danger"
        busy={disconnecting}
        onClose={() => (!disconnecting ? setLogoutWhatsappConfirm(false) : null)}
        onConfirm={performLogoutWhatsApp}
      />
    </div>
  );
}
