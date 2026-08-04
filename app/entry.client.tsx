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
    let reloadedForWorkerUpdate = false;
    navigator.serviceWorker.addEventListener('controllerchange', () => {
      if (!reloadedForWorkerUpdate) {
        reloadedForWorkerUpdate = true;
        window.location.reload();
      }
    });

    void navigator.serviceWorker.register('/sw.js?v=3', {
      updateViaCache: 'none',
    });
  });
}
