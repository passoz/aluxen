import React, { useEffect, useState } from 'react';
import { formatCurrency } from '../utils/format';
import type { MetricsPayload } from '../types/dashboard';
export type { MetricsPayload };

const STATUS_COLORS: Record<string, string> = {
  paid: '#10b981',
  partial: '#3b82f6',
  due: '#f59e0b',
  overdue: '#ef4444',
};

const CATEGORY_COLORS: Record<string, string> = {
  rent: '#6366f1',
  extra: '#14b8a6',
};

function MetricCard({ label, value, sub, tone }: { label: string; value: string; sub?: string; tone?: 'success' | 'danger' | 'neutral' }) {
  const toneColor = tone === 'success' ? '#10b981' : tone === 'danger' ? '#ef4444' : 'var(--text)';
  const isCurrency = value.startsWith('R$') || value.length > 5;
  return (
    <div className="card metric-card" style={{ padding: '1rem 1.25rem', minWidth: 0 }}>
      <div style={{ fontSize: '0.75rem', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)' }}>{label}</div>
      <div
        style={{
          fontSize: isCurrency ? '1.35rem' : '1.75rem',
          fontWeight: 700,
          color: toneColor,
          marginTop: '0.35rem',
          fontVariantNumeric: 'tabular-nums',
          whiteSpace: 'nowrap',
          overflow: 'visible',
        }}
        title={value}
      >
        {value}
      </div>
      {sub ? <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)', marginTop: '0.2rem' }}>{sub}</div> : null}
    </div>
  );
}

function Donut({ data }: { data: Array<{ status: string; label: string; count: number }> }) {
  const total = data.reduce((acc, d) => acc + d.count, 0);
  if (total === 0) {
    return <div style={{ color: 'var(--text-muted)', fontSize: '0.875rem' }}>Sem dados ainda.</div>;
  }
  const radius = 60;
  const circumference = 2 * Math.PI * radius;
  let offset = 0;
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: '1.5rem', flexWrap: 'wrap' }}>
      <svg width="160" height="160" viewBox="0 0 160 160" role="img" aria-label="Distribuição de status dos recibos">
        <g transform="translate(80,80) rotate(-90)">
          <circle r={radius} fill="none" stroke="var(--glass-border)" strokeWidth="20" />
          {data.map((d) => {
            const length = (d.count / total) * circumference;
            const dasharray = `${length} ${circumference - length}`;
            const dashoffset = -offset;
            offset += length;
            return (
              <circle
                key={d.status}
                r={radius}
                fill="none"
                stroke={STATUS_COLORS[d.status] || '#999'}
                strokeWidth="20"
                strokeDasharray={dasharray}
                strokeDashoffset={dashoffset}
              />
            );
          })}
        </g>
        <text x="80" y="78" textAnchor="middle" style={{ fontSize: '1.4rem', fontWeight: 700, fill: 'var(--text)' }}>{total}</text>
        <text x="80" y="98" textAnchor="middle" style={{ fontSize: '0.7rem', fill: 'var(--text-muted)' }}>recibos</text>
      </svg>
      <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: '0.4rem', minWidth: 140 }}>
        {data.map((d) => (
          <li key={d.status} style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.85rem' }}>
            <span style={{ width: 12, height: 12, borderRadius: 3, background: STATUS_COLORS[d.status] || '#999', flexShrink: 0 }} />
            <span style={{ flex: 1 }}>{d.label}</span>
            <strong>{d.count}</strong>
          </li>
        ))}
      </ul>
    </div>
  );
}

function Bars({ data, valueKey, format }: { data: Array<{ id?: string; label: string; [k: string]: number | string | undefined }>; valueKey: string; format?: (n: number) => string }) {
  const max = Math.max(1, ...data.map((d) => Number(d[valueKey]) || 0));
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.625rem' }}>
      {data.length === 0 ? <div style={{ color: 'var(--text-muted)' }}>Sem dados.</div> : null}
      {data.map((d, idx) => {
        const v = Number(d[valueKey]) || 0;
        const pct = (v / max) * 100;
        return (
          <div key={d.id || `${d.label}-${idx}`} style={{ display: 'grid', gridTemplateColumns: 'minmax(140px, 240px) 1fr 90px', gap: '0.75rem', alignItems: 'center', fontSize: '0.85rem' }}>
            <div style={{ color: 'var(--text)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={String(d.label)}>{String(d.label)}</div>
            <div style={{ background: 'var(--glass-border)', borderRadius: 4, height: 14, overflow: 'hidden' }}>
              <div style={{ width: `${pct}%`, height: '100%', background: 'linear-gradient(90deg, #6366f1, #818cf8)', borderRadius: 4, transition: 'width 0.4s' }} />
            </div>
            <div style={{ textAlign: 'right', fontVariantNumeric: 'tabular-nums', color: 'var(--text-muted)' }}>{format ? format(v) : v}</div>
          </div>
        );
      })}
    </div>
  );
}

function MonthlyChart({ data }: { data: MetricsPayload['monthly'] }) {
  const max = Math.max(1, ...data.map((d) => Math.max(d.received, d.expected)));
  const width = 600;
  const height = 200;
  const padding = { top: 16, right: 16, bottom: 28, left: 42 };
  const chartW = width - padding.left - padding.right;
  const chartH = height - padding.top - padding.bottom;
  const barWidth = Math.max(12, chartW / Math.max(data.length, 1) - 16);
  return (
    <div>
      <svg width="100%" height="auto" viewBox={`0 0 ${width} ${height}`} style={{ maxHeight: 220, display: 'block' }} role="img" aria-label="Recebimentos por mês">
        <line x1={padding.left} x2={width - padding.right} y1={height - padding.bottom} y2={height - padding.bottom} stroke="var(--glass-border)" />
        <line x1={padding.left} x2={padding.left} y1={padding.top} y2={height - padding.bottom} stroke="var(--glass-border)" />
        {[0, 0.5, 1].map((p) => {
          const y = height - padding.bottom - chartH * p;
          return (
            <g key={p}>
              <line x1={padding.left} x2={width - padding.right} y1={y} y2={y} stroke="var(--glass-border)" strokeDasharray="3 3" />
              <text x={padding.left - 4} y={y + 4} textAnchor="end" style={{ fontSize: '0.65rem', fill: 'var(--text-muted)' }}>{formatCurrency(max * p).replace('R$', 'R$')}</text>
            </g>
          );
        })}
        {data.map((d, i) => {
          const x = padding.left + (chartW / data.length) * i + (chartW / data.length - barWidth) / 2;
          const expectedH = (d.expected / max) * chartH;
          const receivedH = (d.received / max) * chartH;
          return (
            <g key={d.key}>
              <rect x={x} y={height - padding.bottom - expectedH} width={barWidth} height={expectedH} fill="#cbd5e1" rx={2} />
              <rect x={x} y={height - padding.bottom - receivedH} width={barWidth} height={receivedH} fill="#6366f1" rx={2} />
              <text x={x + barWidth / 2} y={height - 8} textAnchor="middle" style={{ fontSize: '0.65rem', fill: 'var(--text-muted)' }}>{d.label}</text>
            </g>
          );
        })}
      </svg>
      <div style={{ display: 'flex', gap: '1rem', fontSize: '0.75rem', color: 'var(--text-muted)', marginTop: '0.5rem' }}>
        <span><span style={{ display: 'inline-block', width: 10, height: 10, background: '#6366f1', borderRadius: 2, marginRight: 4 }} />Recebido</span>
        <span><span style={{ display: 'inline-block', width: 10, height: 10, background: '#cbd5e1', borderRadius: 2, marginRight: 4 }} />Esperado</span>
      </div>
    </div>
  );
}

export function MetricsView({ initialMetrics }: { initialMetrics?: MetricsPayload | null } = {}) {
  const [data, setData] = useState<MetricsPayload | null>(initialMetrics || null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (initialMetrics) return;
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch('/metrics', { credentials: 'same-origin' });
        if (!res.ok) {
          setError(`Falha ao carregar métricas (${res.status}).`);
          return;
        }
        const json = (await res.json()) as MetricsPayload;
        if (!cancelled) setData(json);
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Erro de conexão.');
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [initialMetrics]);
  if (error) {
    return <div className="card" style={{ padding: '2rem', textAlign: 'center', color: 'var(--text-muted)' }}>{error}</div>;
  }
  if (!data) {
    return <div className="card" style={{ padding: '2rem', textAlign: 'center', color: 'var(--text-muted)' }}>Carregando métricas…</div>;
  }

  const t = data.totals;

  return (
    <div className="stack-lg">
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(230px, 1fr))', gap: '0.75rem' }}>
        <MetricCard label="Inquilinos" value={String(t.tenants)} />
        <MetricCard label="Imóveis" value={String(t.properties)} sub={`${t.activeLeases} contrato(s) ativo(s)`} />
        <MetricCard label="Recibos" value={String(t.receipts)} sub={`${t.paidReceipts + t.partialReceipts} pagos`} />
        <MetricCard label="Recebido" value={formatCurrency(t.paidAmount)} tone="success" />
        <MetricCard label="Em aberto" value={formatCurrency(t.openAmount)} sub={`${t.dueReceipts} a vencer`} />
        <MetricCard label="Em atraso" value={formatCurrency(t.overdueAmount)} sub={`${t.overdueReceipts} vencido(s)`} tone={t.overdueReceipts > 0 ? 'danger' : 'neutral'} />
        <MetricCard label="Esperado mensal" value={formatCurrency(t.expectedMonthly)} sub="Soma dos aluguéis ativos" />
        <MetricCard label="Total previsto" value={formatCurrency(t.totalAmount)} sub="Soma de todos os recibos" />
      </div>

      <section className="card" style={{ padding: '1.25rem' }}>
        <h2 style={{ fontSize: '1.05rem', margin: '0 0 1rem' }}>Recebimentos mensais (últimos 6 meses)</h2>
        <MonthlyChart data={data.monthly} />
      </section>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))', gap: '0.75rem' }}>
        <section className="card" style={{ padding: '1.25rem' }}>
          <h2 style={{ fontSize: '1.05rem', margin: '0 0 1rem' }}>Distribuição por status</h2>
          <Donut data={data.status} />
        </section>

        <section className="card" style={{ padding: '1.25rem' }}>
          <h2 style={{ fontSize: '1.05rem', margin: '0 0 1rem' }}>Por categoria</h2>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
            {data.categories.length === 0 ? <div style={{ color: 'var(--text-muted)' }}>Sem recibos.</div> : null}
            {data.categories.map((c) => {
              const max = Math.max(...data.categories.map((x) => x.total), 1);
              const pct = (c.total / max) * 100;
              return (
                <div key={c.category} style={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: '0.25rem' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.85rem' }}>
                    <span>{c.label}</span>
                    <span style={{ color: 'var(--text-muted)' }}>{c.count} recibo(s)</span>
                  </div>
                  <div style={{ gridColumn: '1 / -1', background: 'var(--glass-border)', height: 10, borderRadius: 5, overflow: 'hidden' }}>
                    <div style={{ width: `${pct}%`, height: '100%', background: CATEGORY_COLORS[c.category] || '#6366f1', transition: 'width 0.4s' }} />
                  </div>
                  <div style={{ fontSize: '0.85rem', fontVariantNumeric: 'tabular-nums', color: 'var(--text-muted)' }}>{formatCurrency(c.total)}</div>
                </div>
              );
            })}
          </div>
        </section>
      </div>

      <section className="card" style={{ padding: '1.25rem' }}>
        <h2 style={{ fontSize: '1.05rem', margin: '0 0 1rem' }}>Ocupação por imóvel</h2>
        {data.properties.length === 0 ? <div style={{ color: 'var(--text-muted)' }}>Sem imóveis cadastrados.</div> : (
          <Bars
            data={data.properties.slice(0, 10).map((p) => ({ id: p.id, label: p.name.length > 30 ? p.name.slice(0, 30) + '…' : p.name, count: p.activeLeases }))}
            valueKey="count"
            format={(n) => `${n} contrato(s)`}
          />
        )}
      </section>

      <section className="card" style={{ padding: '1.25rem' }}>
        <h2 style={{ fontSize: '1.05rem', margin: '0 0 1rem' }}>Top inquilinos por valor pago</h2>
        {data.topTenants.length === 0 ? <div style={{ color: 'var(--text-muted)' }}>Sem pagamentos registrados.</div> : (
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.875rem' }}>
            <thead>
              <tr style={{ textAlign: 'left', color: 'var(--text-muted)', fontSize: '0.75rem', textTransform: 'uppercase' }}>
                <th style={{ padding: '0.5rem' }}>Inquilino</th>
                <th style={{ padding: '0.5rem', textAlign: 'right' }}>Recibos pagos</th>
                <th style={{ padding: '0.5rem', textAlign: 'right' }}>Total pago</th>
              </tr>
            </thead>
            <tbody>
              {data.topTenants.map((t2) => (
                <tr key={t2.id} style={{ borderTop: '1px solid var(--glass-border)' }}>
                  <td style={{ padding: '0.5rem' }}>{t2.name}</td>
                  <td style={{ padding: '0.5rem', textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>{t2.receipts}</td>
                  <td style={{ padding: '0.5rem', textAlign: 'right', fontVariantNumeric: 'tabular-nums', fontWeight: 600 }}>{formatCurrency(t2.paidTotal)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}
