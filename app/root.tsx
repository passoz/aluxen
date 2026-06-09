import type { LinksFunction } from '@remix-run/node';
import { Links, Meta, Outlet, Scripts, ScrollRestoration, isRouteErrorResponse, useRouteError } from '@remix-run/react';
import stylesheet from '../styles/globals.css?url';

export const links: LinksFunction = () => [
  { rel: 'stylesheet', href: stylesheet },
  { rel: 'icon', href: '/favicon.svg', type: 'image/svg+xml' },
  { rel: 'manifest', href: '/manifest.webmanifest' },
  { rel: 'apple-touch-icon', href: '/icon-192.png' },
];

export function ErrorBoundary() {
  const error = useRouteError();
  return (
    <html lang="pt-BR">
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <Meta />
        <Links />
        <title>Aluxen | Erro</title>
      </head>
      <body>
        <div className="login-page">
          <div className="card login-card" style={{ textAlign: 'center' }}>
            <h1 style={{ fontSize: '1.5rem', marginBottom: '1rem' }}>
              {isRouteErrorResponse(error) ? `${error.status} - ${error.statusText}` : 'Erro inesperado'}
            </h1>
            <p style={{ color: 'var(--muted)' }}>
              {isRouteErrorResponse(error) ? error.data?.message || error.statusText : 'Ocorreu um erro inesperado. Tente novamente.'}
            </p>
            <a href="/dashboard?tab=tenants" className="btn btn-primary" style={{ marginTop: '1rem', display: 'inline-block' }}>Voltar ao painel</a>
          </div>
        </div>
        <Scripts />
      </body>
    </html>
  );
}

export default function App() {
  return (
    <html lang="pt-BR">
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <meta name="theme-color" content="#85e6c0" />
        <meta name="apple-mobile-web-app-capable" content="yes" />
        <meta name="apple-mobile-web-app-status-bar-style" content="default" />
        <meta name="apple-mobile-web-app-title" content="Aluxen" />
        <Meta />
        <Links />
      </head>
      <body>
        <Outlet />
        <ScrollRestoration />
        <Scripts />
      </body>
    </html>
  );
}
