/**
 * Visibilidad de servicios (AdGuard, WireGuard, OpenVPN…): checks en Ajustes,
 * persistidos en localStorage. La Home (y el detalle del gateway) solo
 * muestra las tarjetas marcadas.
 */
import { useCallback, useEffect, useState } from 'react'

export interface ServicesVisibility {
  adguard: boolean
  wireguard: boolean
  openvpn: boolean
  labs: boolean
  /** Funcionalidades individuales de Labs (rediseño v3): cada una se
   *  activa/desactiva por separado y controla su entrada en el nav/sidebar. */
  canales: boolean
  actualizaciones: boolean
}

const KEY = 'netpulse-services'
const DEFAULTS: ServicesVisibility = {
  adguard: true,
  wireguard: true,
  openvpn: false,
  labs: false,
  // Por defecto, al activar Labs se muestran las 3 funcionalidades activas;
  // el admin las puede desactivar individualmente.
  canales: true,
  actualizaciones: true,
}

export function getServicesVisibility(): ServicesVisibility {
  try {
    const raw = localStorage.getItem(KEY)
    return raw ? { ...DEFAULTS, ...JSON.parse(raw) } : { ...DEFAULTS }
  } catch {
    return { ...DEFAULTS }
  }
}

export function useServicesVisibility(): [ServicesVisibility, (k: keyof ServicesVisibility, v: boolean) => void] {
  const [services, setServices] = useState<ServicesVisibility>(getServicesVisibility)

  // Sincroniza entre pestañas/componentes (Ajustes y Home a la vez). El evento
  // 'storage' no salta en la misma pestaña: se emite además uno custom.
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === KEY) setServices(getServicesVisibility())
    }
    const onLocal = () => setServices(getServicesVisibility())
    window.addEventListener('storage', onStorage)
    window.addEventListener('netpulse-services-changed', onLocal)
    return () => {
      window.removeEventListener('storage', onStorage)
      window.removeEventListener('netpulse-services-changed', onLocal)
    }
  }, [])

  const set = useCallback((k: keyof ServicesVisibility, v: boolean) => {
    // localStorage es la fuente de verdad compartida: escribir ANTES de emitir
    // el evento (nunca dentro del updater: React puede diferirlo/re-ejecutarlo)
    const next = { ...getServicesVisibility(), [k]: v }
    try {
      localStorage.setItem(KEY, JSON.stringify(next))
    } catch {
      // cuota superada o storage deshabilitado: el estado en memoria sigue valiendo
    }
    setServices(next)
    window.dispatchEvent(new Event('netpulse-services-changed'))
  }, [])

  return [services, set]
}
