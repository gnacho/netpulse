/**
 * NetPulse — capa de datos Web Push (SPEC-PUSH §2).
 *
 * Contrato con el backend (server-go, tras auth de sesión):
 * - GET  /api/push/vapid-key   → {"key":"<base64url>"}  (clave pública VAPID)
 * - POST /api/push/subscribe   body {endpoint, keys:{auth,p256dh}} → 201/200
 * - POST /api/push/unsubscribe body {endpoint} → 204
 *
 * En la demo local (sin backend) la tarjeta de Ajustes NO simula la
 * activación: una suscripción push sin servidor nunca recibiría nada y
 * daría una falsa sensación de funcionalidad. Se muestra el estado
 * degradado "no disponible en la demo local".
 */

/** Suscripción tal y como la espera el backend (PushSubscription.toJSON()). */
export interface PushSubscriptionPayload {
  endpoint: string
  keys: { auth: string; p256dh: string }
}

function redirectLogin(): never {
  window.dispatchEvent(new Event('netpulse-unauthorized'))
  window.location.assign('/login')
  throw new Error('unauthorized')
}

/** GET /api/push/vapid-key → clave pública VAPID (base64url). null si no hay backend push. */
export async function getVapidKey(): Promise<string | null> {
  try {
    const res = await fetch('/api/push/vapid-key')
    if (res.status === 401) redirectLogin()
    if (!res.ok) return null
    const json = (await res.json()) as { key?: string }
    return typeof json.key === 'string' && json.key.length > 0 ? json.key : null
  } catch {
    return null
  }
}

/** POST /api/push/subscribe (upsert por endpoint). true si el servidor la aceptó. */
export async function postPushSubscribe(sub: PushSubscriptionPayload): Promise<boolean> {
  try {
    const res = await fetch('/api/push/subscribe', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(sub),
    })
    if (res.status === 401) redirectLogin()
    return res.ok // 200/201
  } catch {
    return false
  }
}

/** POST /api/push/unsubscribe. El resultado no bloquea la baja local. */
export async function postPushUnsubscribe(endpoint: string): Promise<boolean> {
  try {
    const res = await fetch('/api/push/unsubscribe', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ endpoint }),
    })
    if (res.status === 401) redirectLogin()
    return res.ok // 204
  } catch {
    return false
  }
}

/** Clave VAPID pública (base64url) → applicationServerKey para pushManager.subscribe(). */
export function urlBase64ToUint8Array(base64url: string): Uint8Array {
  const base64 = base64url.replace(/-/g, '+').replace(/_/g, '/')
  const padded = base64 + '='.repeat((4 - (base64.length % 4)) % 4)
  const raw = atob(padded)
  const out = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

/**
 * Contexto de soporte Web Push (evaluar en cliente, no en SSR).
 * - `insecure`: Web Push exige contexto seguro; en LAN por HTTP solo
 *   funciona en localhost (o con HTTPS). Aviso propio en la tarjeta.
 * - `unsupported`: sin ServiceWorker, PushManager o Notification.
 */
export function pushContext(): 'ok' | 'insecure' | 'unsupported' {
  if (!window.isSecureContext) return 'insecure'
  if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) {
    return 'unsupported'
  }
  return 'ok'
}
