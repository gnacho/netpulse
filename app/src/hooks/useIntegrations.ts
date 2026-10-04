/**
 * Integraciones server-side (#968): toggles de Proxmox/ntfy/Telegram/MQTT
 * leídos de GET /api/settings/integrations y escritos con PUT partial-safe
 * (solo la clave tocada). Ausente = activo en el servidor; MQTT refleja la
 * config efectiva del publisher (env + kv).
 */
import { useCallback, useEffect, useState } from 'react'

export interface IntegrationsState {
  ntfy: boolean
  telegram: boolean
  proxmox: boolean
  mqtt: boolean
}

const DEFAULTS: IntegrationsState = { ntfy: true, telegram: true, proxmox: true, mqtt: false }

export function useIntegrations(enabled = true): {
  integrations: IntegrationsState
  loaded: boolean
  setIntegration: (k: keyof IntegrationsState, v: boolean) => void
} {
  const [integrations, setIntegrations] = useState<IntegrationsState>(DEFAULTS)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    if (!enabled) return
    let alive = true
    void fetch('/api/settings/integrations')
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (!alive || !d) return
        setIntegrations({
          ntfy: d.ntfy !== false,
          telegram: d.telegram !== false,
          proxmox: d.proxmox !== false,
          mqtt: d.mqtt === true,
        })
        setLoaded(true)
      })
      .catch(() => undefined)
    return () => {
      alive = false
    }
  }, [enabled])

  const setIntegration = useCallback((k: keyof IntegrationsState, v: boolean) => {
    // Optimista: el toggle responde al instante; si el PUT falla se revierte
    // leyendo el estado real del servidor.
    setIntegrations((prev) => ({ ...prev, [k]: v }))
    void fetch('/api/settings/integrations', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ [k]: v }),
    })
      .then((r) => {
        if (r.ok) return r.json()
        throw new Error(`HTTP ${r.status}`)
      })
      .then((d) => {
        if (d) {
          setIntegrations({
            ntfy: d.ntfy !== false,
            telegram: d.telegram !== false,
            proxmox: d.proxmox !== false,
            mqtt: d.mqtt === true,
          })
        }
      })
      .catch(() => {
        // Revertir con la verdad del servidor.
        void fetch('/api/settings/integrations')
          .then((r) => (r.ok ? r.json() : null))
          .then((d) => {
            if (d) {
              setIntegrations({
                ntfy: d.ntfy !== false,
                telegram: d.telegram !== false,
                proxmox: d.proxmox !== false,
                mqtt: d.mqtt === true,
              })
            }
          })
          .catch(() => undefined)
      })
  }, [])

  return { integrations, loaded, setIntegration }
}
