// Aluxen — service worker mínimo.
//
// Escopo deliberadamente estreito: guarda apenas os estáticos versionados do
// design system (CSS, JS, ícones e manifest). NUNCA intercepta HTML autenticado
// (documento/navegação) nem PDF de recibo — esses vão sempre para a rede, para
// não servir página de painel ou documento de outro titular a partir do cache.

var CACHE_NAME = 'aluxen-static-v1';

var PRECACHE = [
  '/styles/globals.css',
  '/styles/panel.css',
  '/app.js',
  '/manifest.webmanifest',
  '/favicon.svg',
  '/favicon.png',
  '/icon-192.png',
  '/icon-512.png',
  '/logomarca.png',
  '/logomarca-light.png'
];

// Extensões que podem vir do cache. Qualquer outra coisa (HTML, JSON, PDF)
// passa direto para a rede.
var STATIC_EXTENSIONS = ['.css', '.js', '.png', '.svg', '.webmanifest', '.ico', '.woff', '.woff2'];

function isCacheable(url) {
  if (url.origin !== self.location.origin) return false;
  var path = url.pathname.toLowerCase();
  if (path.indexOf('/api/') === 0) return false;
  if (path === '/sw.js') return false;
  if (path.slice(-4) === '.pdf') return false;
  for (var index = 0; index < STATIC_EXTENSIONS.length; index += 1) {
    var extension = STATIC_EXTENSIONS[index];
    if (path.slice(-extension.length) === extension) return true;
  }
  return false;
}

self.addEventListener('install', function (event) {
  event.waitUntil(
    caches.open(CACHE_NAME).then(function (cache) {
      return cache.addAll(PRECACHE);
    }).then(function () {
      return self.skipWaiting();
    })
  );
});

self.addEventListener('activate', function (event) {
  event.waitUntil(
    caches.keys().then(function (keys) {
      return Promise.all(keys.map(function (key) {
        if (key === CACHE_NAME) return null;
        return caches.delete(key);
      }));
    }).then(function () {
      return self.clients.claim();
    })
  );
});

self.addEventListener('fetch', function (event) {
  var request = event.request;
  if (request.method !== 'GET') return;
  if (request.mode === 'navigate' || request.destination === 'document') return;

  var url;
  try {
    url = new URL(request.url);
  } catch (error) {
    return;
  }
  if (!isCacheable(url)) return;

  // Rede primeiro: um deploy novo de CSS/JS aparece na primeira visita. O cache
  // é só a rede de segurança para quando não há conexão.
  event.respondWith(
    fetch(request).then(function (response) {
      if (response && response.status === 200 && response.type === 'basic') {
        var copy = response.clone();
        caches.open(CACHE_NAME).then(function (cache) {
          cache.put(request, copy);
        });
      }
      return response;
    }).catch(function () {
      return caches.match(request).then(function (cached) {
        return cached || Response.error();
      });
    })
  );
});
