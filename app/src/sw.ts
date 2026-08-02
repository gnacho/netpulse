/// <reference lib="webworker" />
/**
 * NetPulse — Service Worker propio (vite-plugin-pwa `injectManifest`).
 *
 * ¿Por qué injectManifest y no generateSW + importScripts?
 * generateSW no admite handlers custom (Web Push exige `push` y
 * `notificationclick`). La alternativa "mínima" (importScripts de un
 * push-sw.js estático en public/) dejaba la lógica push fuera del build TS
 * (sin type-check, sin minificar, archivo separado que versionar a mano) y
 * además ese archivo caía dentro del propio glob de precache. Con
 * injectManifest el SW es un único bundle TS versionado y el precache
 * app-shell se inyecta en self.__WB_MANIFEST con los mismos globPatterns
 * de siempre (16 entradas).
 */
import { clientsClaim } from 'workbox-core'
import { cleanupOutdatedCaches, createHandlerBoundToURL, precacheAndRoute } from 'workbox-precaching'
import { NavigationRoute, registerRoute } from 'workbox-routing'

declare let self: ServiceWorkerGlobalScope

// registerType: 'autoUpdate' — el SW nuevo toma el control sin esperar al
// cierre de pestañas (equivale al comportamiento anterior con generateSW).
self.skipWaiting()
clientsClaim()

// Punto de inyección de workbox-build: el literal `self.__WB_MANIFEST` se
// sustituye por el manifiesto de precache en el build (no refactorizar).
precacheAndRoute(self.__WB_MANIFEST)
cleanupOutdatedCaches()

// App-shell offline para navegaciones (equivale a `navigateFallback:
// 'index.html'` de generateSW). En dev NO se registra: el SW de desarrollo
// no precachea index.html y esta ruta serviría un HTML congelado.
if (!import.meta.env.DEV) {
  registerRoute(new NavigationRoute(createHandlerBoundToURL('/index.html')))
}

// ---------------------------------------------------------------------------
// Web Push (SPEC-PUSH §2)
// Payload: {title, body, category, severity, url:"/alerts", tag}
// ---------------------------------------------------------------------------

interface PushPayload {
  title?: string
  body?: string
  category?: string
  severity?: string
  url?: string
  tag?: string
}

self.addEventListener('push', (event: PushEvent) => {
  event.waitUntil(onPush(event))
})

async function onPush(event: PushEvent): Promise<void> {
  let payload: PushPayload = {}
  try {
    if (event.data) payload = event.data.json() as PushPayload
  } catch {
    // Payload no-JSON: notificación genérica (nunca romper el handler)
    payload = {}
  }
  await self.registration.showNotification(payload.title || 'NetPulse', {
    body: payload.body ?? '',
    icon: '/icon-192.png',
    badge: '/icon-192.png',
    // tag = dedup nativo del navegador (SPEC: tag = id del evento)
    tag: payload.tag ?? 'netpulse-alert',
    data: { url: payload.url ?? '/alerts' },
  })
}

self.addEventListener('notificationclick', (event: NotificationEvent) => {
  event.notification.close()
  const url = (event.notification.data as { url?: string } | undefined)?.url ?? '/alerts'
  event.waitUntil(focusOrOpen(url))
})

/** Foco a una ventana ya abierta de la app (navegándola a `url`) o abre una nueva. */
async function focusOrOpen(url: string): Promise<void> {
  const target = new URL(url, self.location.origin).pathname
  const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
  for (const client of windows) {
    if (new URL(client.url).origin !== self.location.origin) continue
    await client.focus()
    if (new URL(client.url).pathname !== target) {
      await client.navigate(target)
    }
    return
  }
  await self.clients.openWindow(target)
}
