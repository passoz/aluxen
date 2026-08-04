// Installation-only service worker.
// It intentionally has no fetch handler: application pages, assets, API data and
// authentication must always come from the network and must never be served from
// a stale offline cache.
const LEGACY_CACHE_PREFIX = 'aluxen-';

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => Promise.all(
      keys
        .filter((key) => key.startsWith(LEGACY_CACHE_PREFIX))
        .map((key) => caches.delete(key)),
    )),
  );
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim());
});
