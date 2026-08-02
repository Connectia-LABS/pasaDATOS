const CACHE = "pasadatos-shell-v1.1.1";
const SHELL = ["/", "/manifest.webmanifest", "/assets/app.css", "/assets/mobile.js", "/assets/theme.js", "/assets/brand-logo.png", "/assets/brand-symbol.png", "/assets/qrcode.min.js", "/assets/icon-192.png", "/assets/icon-512.png"];
self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(CACHE).then((cache) => cache.addAll(SHELL)).then(() => self.skipWaiting()));
});
self.addEventListener("activate", (event) => {
  event.waitUntil(caches.keys().then((keys) => Promise.all(keys.filter((key) => key !== CACHE).map((key) => caches.delete(key)))).then(() => self.clients.claim()));
});
self.addEventListener("fetch", (event) => {
  const request = event.request;
  if (request.method !== "GET") return;
  const url = new URL(request.url);
  if (url.origin !== location.origin || url.pathname.startsWith("/api/") || url.pathname.startsWith("/native/")) return;
  event.respondWith(fetch(request).then((response) => {
    const copy = response.clone();
    caches.open(CACHE).then((cache) => cache.put(request, copy));
    return response;
  }).catch(() => caches.match(request).then((cached) => cached || caches.match("/"))));
});
