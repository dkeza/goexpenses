// Pages carry one user's financial data, so they are never cached: a page
// request always goes to the server and only falls back to the offline page.
// Static files have a build version in their URL and are served cache-first.
const version = new URL(self.location.href).searchParams.get('v') || 'dev';
const cacheName = `goexpenses-${version}`;
const offlinePage = '/static/offline.html';

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(cacheName)
      .then((cache) => cache.addAll([offlinePage, '/static/icons/icon-192.png']))
      .then(() => self.skipWaiting()),
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys()
      .then((names) => Promise.all(
        names.filter((name) => name.startsWith('goexpenses-') && name !== cacheName)
          .map((name) => caches.delete(name)),
      ))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener('fetch', (event) => {
  const { request } = event;
  if (request.method !== 'GET') return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;

  if (request.mode === 'navigate') {
    event.respondWith(fetch(request).catch(() => caches.match(offlinePage)));
    return;
  }

  if (url.pathname.startsWith('/static/')) {
    event.respondWith(
      caches.open(cacheName).then((cache) => cache.match(request).then((cached) => {
        if (cached) return cached;
        return fetch(request).then((response) => {
          if (response.ok) cache.put(request, response.clone());
          return response;
        });
      })),
    );
  }
});
