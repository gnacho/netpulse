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

/** Defaults del SPEC §2 (mismo JSON que `alerts.config.v1` en el backend). */
export const DEFAULT_ALERTS_CONFIG: AlertsConfig = {
  router: 'urgent',
  internet: 'urgent',
  clients: 'urgent',
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
 * ¿Pasa un evento el filtro de su categoría? (semántica §2: none descarta,
 * urgent solo deja los urgentes). Categoría desconocida → 'all' (no esconder).
 */
export function passesAlertConfig(ev: Pick<AlertEvent, 'category' | 'urgent'>, cfg: AlertsConfig): boolean {
  const level: AlertConfigLevel = cfg[ev.category] ?? 'all'
  if (level === 'none') return false
  if (level === 'urgent') return ev.urgent
  return true
}

/** No leídas que pasan la config — el contador de la campana en demo. */
export function countUnreadAlerts(alerts: Pick<AlertEvent, 'category' | 'urgent' | 'read'>[], cfg: AlertsConfig): number {
  return alerts.filter((a) => !a.read && passesAlertConfig(a, cfg)).length
}
