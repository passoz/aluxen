import type { FC, PropsWithChildren } from 'hono/jsx';

interface LayoutProps {
  title?: string;
}

export const Layout: FC<PropsWithChildren<LayoutProps>> = ({ title = 'Aluxen', children }) => {
  return (
    <html lang="pt-BR">
      <head>
        <meta charset="UTF-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1.0" />
        <title>{title}</title>
        <style>{`
          :root {
            --bg: #0f172a;
            --surface: #1e293b;
            --border: #334155;
            --text: #f8fafc;
            --text-muted: #94a3b8;
            --primary: #3b82f6;
            --primary-hover: #2563eb;
            --accent: #10b981;
            --danger: #ef4444;
          }
          * { box-sizing: border-box; margin: 0; padding: 0; }
          body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
            background: var(--bg);
            color: var(--text);
            min-height: 100vh;
            display: flex;
            flex-direction: column;
          }
          header {
            background: var(--surface);
            border-bottom: 1px solid var(--border);
            padding: 1rem 2rem;
            display: flex;
            align-items: center;
            justify-content: space-between;
          }
          .brand {
            font-size: 1.25rem;
            font-weight: 700;
            color: var(--primary);
            text-decoration: none;
            display: flex;
            align-items: center;
            gap: 0.5rem;
          }
          nav {
            display: flex;
            gap: 1.5rem;
          }
          nav a {
            color: var(--text-muted);
            text-decoration: none;
            font-weight: 500;
            transition: color 0.2s;
          }
          nav a:hover, nav a.active {
            color: var(--text);
          }
          main {
            flex: 1;
            padding: 2rem;
            max-width: 1200px;
            width: 100%;
            margin: 0 auto;
          }
          .card {
            background: var(--surface);
            border: 1px solid var(--border);
            border-radius: 0.5rem;
            padding: 1.5rem;
            margin-bottom: 1.5rem;
          }
          .grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
            gap: 1.5rem;
            margin-bottom: 2rem;
          }
          .stat-card {
            background: var(--surface);
            border: 1px solid var(--border);
            border-radius: 0.5rem;
            padding: 1.25rem;
          }
          .stat-value {
            font-size: 2rem;
            font-weight: 700;
            margin-top: 0.5rem;
            color: var(--primary);
          }
          .stat-label {
            color: var(--text-muted);
            font-size: 0.875rem;
            text-transform: uppercase;
            letter-spacing: 0.05em;
          }
          table {
            width: 100%;
            border-collapse: collapse;
            margin-top: 1rem;
          }
          th, td {
            text-align: left;
            padding: 0.75rem 1rem;
            border-bottom: 1px solid var(--border);
          }
          th {
            color: var(--text-muted);
            font-weight: 600;
            font-size: 0.875rem;
          }
          .btn {
            display: inline-flex;
            align-items: center;
            justify-content: center;
            background: var(--primary);
            color: white;
            padding: 0.5rem 1rem;
            border-radius: 0.375rem;
            font-weight: 500;
            text-decoration: none;
            border: none;
            cursor: pointer;
            transition: background 0.2s;
          }
          .btn:hover {
            background: var(--primary-hover);
          }
          .badge {
            display: inline-block;
            padding: 0.25rem 0.5rem;
            border-radius: 0.25rem;
            font-size: 0.75rem;
            font-weight: 600;
          }
          .badge-active { background: #065f46; color: #6ee7b7; }
          .badge-inactive { background: #7f1d1d; color: #fca5a5; }
          footer {
            text-align: center;
            padding: 1.5rem;
            color: var(--text-muted);
            font-size: 0.875rem;
            border-top: 1px solid var(--border);
          }
        `}</style>
      </head>
      <body>
        <header>
          <a href="/" class="brand">
            <span>🏠</span> Aluxen
          </a>
          <nav>
            <a href="/">Dashboard</a>
            <a href="/tenants">Inquilinos</a>
            <a href="/properties">Imóveis</a>
            <a href="/leases">Locações</a>
            <a href="/receipts">Recibos</a>
          </nav>
        </header>
        <main>{children}</main>
        <footer>
          <p>Aluxen &copy; {new Date().getFullYear()} — Bun + Hono + Drizzle ORM</p>
        </footer>
      </body>
    </html>
  );
};
