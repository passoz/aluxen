import { RemixBrowser } from '@remix-run/react';
import { startTransition, StrictMode } from 'react';
import { hydrateRoot } from 'react-dom/client';

startTransition(() => {
  hydrateRoot(
    document,
    <StrictMode>
      <RemixBrowser />
    </StrictMode>,
  );
});

if (typeof window !== 'undefined' && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    // The manifest keeps the app installable. This worker intentionally has no
    // offline cache or network interception, preventing stale sessions/assets.
    void navigator.serviceWorker.register('/sw.js?v=4', {
      updateViaCache: 'none',
    });
  });
}
