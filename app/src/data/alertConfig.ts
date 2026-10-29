/**
 * NetPulse — Configuración de alertas por categoría (SPEC-ALERTAS §2).
 *
 * Niveles: "urgent" (solo eventos urgentes) · "all" (todo) · "none" (se
 * descarta: no se guarda, no cuenta, no empuja). En modo live la fuente de
 * verdad es el backend (GET/PUT /api/alerts/config, persistencia kv); en demo
 * (sin backend) vive en localStorage para que la UI sea demostrable.
 */

import type { AlertCategory, AlertConfigLevel, AlertEvent, AlertsConfig } from '@/data/types'

export const ALERT_CATEGORIES: readonly AlertCategory[] = [
  'router',
  'internet',
  'clients',
  'signal',
  'vpn',
  'system',
]

export const ALERT_CONFIG_LEVELS: readonly AlertConfigLevel[] = ['urgent', 'all', 'none']

/**
 * Defaults del SPEC §2. Deben coincidir con `DefaultConfig()` del backend
 * (`server-go/internal/alerts/alerts.go`), que es la fuente de verdad: el test
 * `TestFrontendDefaultsMatchBackend` falla si las dos copias se separan (#1672).
 */
export const DEFAULT_ALERTS_CONFIG: AlertsConfig = {
  router: 'urgent',
  internet: 'urgent',
  clients: 'all',
  signal: 'none',
  vpn: 'none',
  system: 'all',
}

const DEMO_CONFIG_KEY = 'netpulse-alerts-config-v1'

const isLevel = (v: unknown): v is AlertConfigLevel =>
  typeof v === 'string' && (ALERT_CONFIG_LEVELS as readonly string[]).includes(v)

/** Normaliza un mapa cualquiera a las 6 claves (rellena defaults, descarta inválidos). */
export function normalizeAlertsConfig(raw: unknown): AlertsConfig {
  const cfg = { ...DEFAULT_ALERTS_CONFIG }
  if (raw && typeof raw === 'object') {
    for (const cat of ALERT_CATEGORIES) {
      const v = (raw as Record<string, unknown>)[cat]
      if (isLevel(v)) cfg[cat] = v
    }
  }
  return cfg
}

/** Config demo: localStorage con defaults si no hay nada guardado. */
export function loadDemoAlertsConfig(): AlertsConfig {
  try {
    const raw = localStorage.getItem(DEMO_CONFIG_KEY)
    if (raw) return normalizeAlertsConfig(JSON.parse(raw))
  } catch {
    /* localStorage no disponible o JSON corrupto → defaults */
  }
  return { ...DEFAULT_ALERTS_CONFIG }
}

export function saveDemoAlertsConfig(cfg: AlertsConfig): void {
  try {
    localStorage.setItem(DEMO_CONFIG_KEY, JSON.stringify(cfg))
  } catch {
    /* sin localStorage: la config vive solo en estado */
  }
}

/**
 * Nivel "none" = la categoría no existe para el operador: el servidor no la
 * registra ni la sirve (#1659). Es distinto de "urgent", que solo limita los
 * avisos y deja el evento en el historial (#1413).
 */
export function isHiddenByLevel(ev: Pick<AlertEvent, 'category'>, cfg: AlertsConfig): boolean {
  return (cfg[ev.category] ?? 'all') === 'none'
}

/** No leídas que el motor sirve — el contador de la campana. */
export function countUnreadAlerts(alerts: Pick<AlertEvent, 'category' | 'read'>[], cfg: AlertsConfig): number {
  return alerts.filter((a) => !a.read && !isHiddenByLevel(a, cfg)).length
}
