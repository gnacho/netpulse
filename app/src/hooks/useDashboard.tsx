import { createContext, useCallback, useContext, useMemo, useState } from 'react'
import type { TimeRange } from '@/data/mock'

interface DashboardState {
  /** Rango temporal global del topbar */
  range: TimeRange
  setRange: (r: TimeRange) => void
  /** Se incrementa al pulsar refresh: las páginas re-tweenean sus números */
  refreshKey: number
  refresh: () => void
}

const DashboardContext = createContext<DashboardState | null>(null)

export function DashboardProvider({ children }: { children: React.ReactNode }) {
  const [range, setRange] = useState<TimeRange>('24h')
  const [refreshKey, setRefreshKey] = useState(0)
  const refresh = useCallback(() => setRefreshKey((k) => k + 1), [])
  const value = useMemo(() => ({ range, setRange, refreshKey, refresh }), [range, refreshKey, refresh])
  return <DashboardContext.Provider value={value}>{children}</DashboardContext.Provider>
}

export function useDashboard(): DashboardState {
  const ctx = useContext(DashboardContext)
  if (!ctx) throw new Error('useDashboard debe usarse dentro de <DashboardProvider>')
  return ctx
}
