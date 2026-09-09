import React from 'react';

export type VerifyDocType = 'receipt' | 'lease';

export interface VerificationData {
  valid: boolean;
  type?: VerifyDocType;
  id?: string;
  token?: string;
  authCode?: string;
  tenantName?: string;
  tenantCpf?: string;
  propertyName?: string;
  propertyAddress?: string;
  landlordName?: string;
  amount?: string;
  amountRaw?: number;
  paidAmount?: string | null;
  paidAmountRaw?: number;
  category?: string;
  description?: string | null;
  dueDate?: string;
  paidAt?: string | null;
  startDate?: string;
  endDate?: string;
  dueDay?: number;
  status?: string;
  error?: string;
  verifiedAt: string;
}

export function VerificationPage(props: VerificationData) {
  const isReceipt = props.type === 'receipt';

  return (
    <div className="login-page" style={{ padding: '2rem 1rem', minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <div className="card" style={{ width: '100%', maxWidth: '640px', padding: '2rem' }}>
        <div className="brand-lockup" style={{ textAlign: 'center', marginBottom: '1.5rem' }}>
          <img src="/logomarca.png" alt="Aluxen" className="login-logomarca" />
          <p style={{ color: 'var(--text-muted)', fontSize: '0.9rem', marginTop: '0.25rem' }}>
            Portal Oficial de Verificação e Autenticidade Digital
          </p>
        </div>

        {props.valid ? (
          <div>
            <div
              style={{
                background: 'rgba(16, 185, 129, 0.12)',
                border: '1px solid rgba(16, 185, 129, 0.3)',
                borderRadius: '14px',
                padding: '1.25rem',
                textAlign: 'center',
                marginBottom: '1.75rem',
              }}
            >
              <div style={{ fontSize: '2rem', marginBottom: '0.25rem' }}>✓</div>
              <h2 style={{ color: '#10b981', fontSize: '1.25rem', fontWeight: 700, margin: 0 }}>
                DOCUMENTO AUTÊNTICO E VÁLIDO
              </h2>
              <p style={{ color: 'var(--text)', fontSize: '0.875rem', marginTop: '0.25rem' }}>
                Este documento foi emitido e assinado digitalmente pelo sistema Aluxen.
              </p>
            </div>

            <div style={{ display: 'grid', gap: '1rem', marginBottom: '1.5rem' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', paddingBottom: '0.5rem', borderBottom: '1px solid var(--border)' }}>
                <span style={{ color: 'var(--text-muted)', fontSize: '0.9rem' }}>Tipo de Documento:</span>
                <strong style={{ fontSize: '0.95rem' }}>
                  {isReceipt ? 'Recibo de Pagamento de Aluguel' : 'Contrato de Locação Residencial'}
                </strong>
              </div>

              <div style={{ display: 'flex', justifyContent: 'space-between', paddingBottom: '0.5rem', borderBottom: '1px solid var(--border)' }}>
                <span style={{ color: 'var(--text-muted)', fontSize: '0.9rem' }}>Chave de Autenticação:</span>
                <span style={{ fontFamily: 'monospace', fontWeight: 700, color: 'var(--primary)' }}>
                  {props.authCode}
                </span>
              </div>

              <div style={{ display: 'flex', justifyContent: 'space-between', paddingBottom: '0.5rem', borderBottom: '1px solid var(--border)' }}>
                <span style={{ color: 'var(--text-muted)', fontSize: '0.9rem' }}>Locatário:</span>
                <strong>{props.tenantName}</strong>
              </div>

              <div style={{ display: 'flex', justifyContent: 'space-between', paddingBottom: '0.5rem', borderBottom: '1px solid var(--border)' }}>
                <span style={{ color: 'var(--text-muted)', fontSize: '0.9rem' }}>Locador:</span>
                <span>{props.landlordName || 'Locador'}</span>
              </div>

              <div style={{ display: 'flex', justifyContent: 'space-between', paddingBottom: '0.5rem', borderBottom: '1px solid var(--border)' }}>
                <span style={{ color: 'var(--text-muted)', fontSize: '0.9rem' }}>Imóvel:</span>
                <span style={{ textAlign: 'right', maxWidth: '60%' }}>
                  <strong>{props.propertyName}</strong>
                  {props.propertyAddress ? (
                    <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>{props.propertyAddress}</div>
                  ) : null}
                </span>
              </div>

              {props.description ? (
                <div style={{ display: 'flex', justifyContent: 'space-between', paddingBottom: '0.5rem', borderBottom: '1px solid var(--border)' }}>
                  <span style={{ color: 'var(--text-muted)', fontSize: '0.9rem' }}>Referência:</span>
                  <strong>{props.description}</strong>
                </div>
              ) : null}

              <div style={{ display: 'flex', justifyContent: 'space-between', paddingBottom: '0.5rem', borderBottom: '1px solid var(--border)' }}>
                <span style={{ color: 'var(--text-muted)', fontSize: '0.9rem' }}>Valor Total:</span>
                <strong style={{ fontSize: '1.15rem', color: 'var(--primary)' }}>{props.amount}</strong>
              </div>

              {isReceipt ? (
                <>
                  {props.paidAmountRaw != null && props.amountRaw != null && props.paidAmountRaw > 0 && props.paidAmountRaw < props.amountRaw ? (
                    <>
                      <div style={{ display: 'flex', justifyContent: 'space-between', paddingBottom: '0.5rem', borderBottom: '1px solid var(--border)' }}>
                        <span style={{ color: 'var(--text-muted)', fontSize: '0.9rem' }}>Valor Pago (Parcial):</span>
                        <strong style={{ color: '#3b82f6' }}>{props.paidAmount}</strong>
                      </div>
                      <div style={{ display: 'flex', justifyContent: 'space-between', paddingBottom: '0.5rem', borderBottom: '1px solid var(--border)' }}>
                        <span style={{ color: 'var(--text-muted)', fontSize: '0.9rem' }}>Saldo Restante:</span>
                        <strong style={{ color: 'var(--danger)' }}>
                          {new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' }).format(props.amountRaw - props.paidAmountRaw)}
                        </strong>
                      </div>
                    </>
                  ) : null}
                  <div style={{ display: 'flex', justifyContent: 'space-between', paddingBottom: '0.5rem', borderBottom: '1px solid var(--border)' }}>
                    <span style={{ color: 'var(--text-muted)', fontSize: '0.9rem' }}>Vencimento:</span>
                    <span>{props.dueDate}</span>
                  </div>
                  <div style={{ display: 'flex', justifyContent: 'space-between', paddingBottom: '0.5rem', borderBottom: '1px solid var(--border)' }}>
                    <span style={{ color: 'var(--text-muted)', fontSize: '0.9rem' }}>Status da Quitação:</span>
                    <span>
                      {props.paidAmountRaw != null && props.amountRaw != null && props.paidAmountRaw > 0 && props.paidAmountRaw < props.amountRaw ? (
                        <span className="status-badge partial">
                          Parcial {props.paidAt ? `(pago em ${props.paidAt})` : ''}
                        </span>
                      ) : props.paidAt ? (
                        <span className="status-badge paid">Pago em {props.paidAt}</span>
                      ) : (
                        <span className="status-badge due">Aguardando Pagamento</span>
                      )}
                    </span>
                  </div>
                </>
              ) : (
                <>
                  <div style={{ display: 'flex', justifyContent: 'space-between', paddingBottom: '0.5rem', borderBottom: '1px solid var(--border)' }}>
                    <span style={{ color: 'var(--text-muted)', fontSize: '0.9rem' }}>Vigência do Contrato:</span>
                    <span>
                      {props.startDate} até {props.endDate}
                    </span>
                  </div>
                  <div style={{ display: 'flex', justifyContent: 'space-between', paddingBottom: '0.5rem', borderBottom: '1px solid var(--border)' }}>
                    <span style={{ color: 'var(--text-muted)', fontSize: '0.9rem' }}>Vencimento Mensal:</span>
                    <span>Todo dia {props.dueDay}</span>
                  </div>
                  <div style={{ display: 'flex', justifyContent: 'space-between', paddingBottom: '0.5rem', borderBottom: '1px solid var(--border)' }}>
                    <span style={{ color: 'var(--text-muted)', fontSize: '0.9rem' }}>Status de Vigência:</span>
                    <span>
                      <span className={`status-badge ${props.status === 'active' ? 'paid' : props.status === 'suspended' ? 'due' : props.status === 'season' ? 'partial' : 'overdue'}`}>
                        {props.status === 'active' ? 'Ativo' : props.status === 'suspended' ? 'Suspenso' : props.status === 'season' ? 'Temporada' : 'Encerrado'}
                      </span>
                    </span>
                  </div>
                </>
              )}
            </div>

            <div style={{ textAlign: 'center', color: 'var(--text-muted)', fontSize: '0.8rem', borderTop: '1px solid var(--border)', paddingTop: '1rem' }}>
              Consulta realizada em <strong>{props.verifiedAt}</strong>.
            </div>

            {props.id && props.token ? (
              <div style={{ textAlign: 'center', marginTop: '1.25rem' }}>
                <a
                  href={isReceipt ? `/pdf?id=${props.id}&token=${props.token}` : `/contract-pdf?id=${props.id}&token=${props.token}`}
                  target="_blank"
                  rel="noreferrer"
                  className="btn btn-primary"
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    gap: '0.5rem',
                    padding: '0.75rem 1.5rem',
                    fontSize: '0.95rem',
                    width: '100%',
                    maxWidth: '360px',
                    textDecoration: 'none',
                    margin: '0 auto',
                  }}
                >
                  <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                    <polyline points="7 10 12 15 17 10" />
                    <line x1="12" y1="15" x2="12" y2="3" />
                  </svg>
                  {isReceipt ? 'Visualizar / Baixar Recibo (PDF)' : 'Visualizar / Baixar Contrato (PDF)'}
                </a>
              </div>
            ) : null}
          </div>
        ) : (
          <div>
            <div
              style={{
                background: 'rgba(239, 68, 68, 0.12)',
                border: '1px solid rgba(239, 68, 68, 0.3)',
                borderRadius: '14px',
                padding: '1.5rem',
                textAlign: 'center',
                marginBottom: '1.5rem',
              }}
            >
              <div style={{ fontSize: '2rem', marginBottom: '0.25rem', color: '#ef4444' }}>✕</div>
              <h2 style={{ color: '#ef4444', fontSize: '1.25rem', fontWeight: 700, margin: 0 }}>
                DOCUMENTO NÃO AUTENTICADO
              </h2>
              <p style={{ color: 'var(--text)', fontSize: '0.9rem', marginTop: '0.5rem' }}>
                {props.error || 'A chave de verificação informada é inválida ou o documento não consta nos registros oficiais do sistema.'}
              </p>
            </div>

            <p style={{ textAlign: 'center', color: 'var(--text-muted)', fontSize: '0.85rem' }}>
              Se você acredita que isso é um erro, solicite uma nova via diretamente ao locador.
            </p>
          </div>
        )}

        <div style={{ textAlign: 'center', marginTop: '1.5rem' }}>
          <a href="/login" className="btn btn-sm btn-secondary">
            Ir para a página inicial
          </a>
        </div>
      </div>
    </div>
  );
}
