import { useEffect, useState } from 'react'
import { useNetPulse } from '@/data/DataProvider'

/** Fallback cuando el servidor aún no expone el ajuste (demo o build antiguo). */
export const WEAK_SIGNAL_FALLBACK_DBM = -70

/**
 * Umbral global de "señal débil" (Ajustes → /api/settings/thresholds,
 * #904). Lo comparten la página Clientes (#1003) y la matriz de roaming
 * (#906): un solo fetch por consumidor, sin cachear entre páginas.
 */
export function useWeakSignalDbm(): number {
  const { isDemo } = useNetPulse()
  const [dbm, setDbm] = useState(WEAK_SIGNAL_FALLBACK_DBM)
  useEffect(() => {
    if (isDemo) return
    let cancelled = false
    fetch('/api/settings/thresholds')
      .then((r) => (r.ok ? r.json() : null))
      .then((j) => {
        if (!cancelled && j && typeof j.weakSignalDbm === 'number') setDbm(j.weakSignalDbm)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [isDemo])
  return dbm
}
