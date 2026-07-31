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
}

const KEY = 'netpulse-services'
const DEFAULTS: ServicesVisibility = { adguard: true, wireguard: true, openvpn: false }

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

  // Sincroniza entre pestañas/componentes (Ajustes y Home a la vez)
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === KEY) setServices(getServicesVisibility())
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [])

  const set = useCallback((k: keyof ServicesVisibility, v: boolean) => {
    setServices((prev) => {
      const next = { ...prev, [k]: v }
      try {
        localStorage.setItem(KEY, JSON.stringify(next))
      } catch {}
      return next
    })
  }, [])

  return [services, set]
}
