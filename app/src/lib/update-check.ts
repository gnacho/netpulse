const CHECK_KEY = 'netpulse-last-update-check'
const CHECK_INTERVAL = 4 * 60 * 60 * 1000
const EVENT_NAME = 'netpulse-update-available'

export { CHECK_KEY, CHECK_INTERVAL }

export function invalidateThrottle(): void {
  try { localStorage.removeItem(CHECK_KEY) } catch { /* no storage */ }
}

export function notifyBanner(latest: string): void {
  invalidateThrottle()
  window.dispatchEvent(new CustomEvent(EVENT_NAME, { detail: { latest } }))
}

export function onBannerSignal(cb: (latest: string) => void): () => void {
  const handler = (e: Event) => {
    const detail = (e as CustomEvent<{ latest: string }>).detail
    if (detail?.latest) cb(detail.latest)
  }
  window.addEventListener(EVENT_NAME, handler)
  return () => window.removeEventListener(EVENT_NAME, handler)
}
