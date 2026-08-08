import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { motion, useReducedMotion } from 'framer-motion'
import { RefreshCw, Wifi } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useNetPulse } from '@/data/DataProvider'

// ---------------------------------------------------------------------------
// Tipos del contrato GET /api/dawn (server-go/internal/adapters/types.go).
// Los clientes vienen del hearing map distribuido de DAWN: cada AP reporta
// los clientes que ve con su señal, así que un mismo cliente puede aparecer
// bajo varios BSSIDs (lo que pinta varias celdas en la matriz).
// ---------------------------------------------------------------------------

interface DawnClient {
  mac: string
  signal: number // -dBm
  ht: boolean
  vht: boolean
}
interface DawnAP {
  ssid: string
  bssid: string
  hostname: string
  band: string
  channel: number
  utilizationPct: number
  clientCount: number
  clients: DawnClient[]
  local: boolean
  iface: string
}
interface DawnMesh {
  routerId: string
  name: string
  dawn: boolean
  apsSeen: number
}
interface Dawn {
  aps: DawnAP[]
  mesh: DawnMesh[]
}

type Band = 'all' | '2.4 GHz' | '5 GHz'
type Tab = 'matrix' | '11r' | 'survey' | 'events'

/** Clase de color por señal (-dBm): verde óptimo, ámbar aceptable, rojo límite. */
function signalClass(s: number): string {
  if (s >= -65) return 'bg-ok/15 text-ok ring-ok/30'
  if (s >= -80) return 'bg-warn/15 text-warn ring-warn/30'
  return 'bg-danger/15 text-danger ring-danger/30'
}

export default function Roaming() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const { devices } = useNetPulse()
  const [tab, setTab] = useState<Tab>('matrix')
  const [band, setBand] = useState<Band>('all')
  const [weakOnly, setWeakOnly] = useState(false)
  const [dawn, setDawn] = useState<Dawn | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [spin, setSpin] = useState(false)

  // MAC → nombre (resuelto desde la lista de dispositivos del overview).
  const nameByMac = useMemo(() => {
    const m = new Map<string, string>()
    for (const d of devices) {
      if (d.mac) m.set(d.mac.toUpperCase(), d.name)
    }
    return m
  }, [devices])

  async function load() {
    setLoading(true)
    setError(false)
    try {
      const res = await fetch('/api/dawn')
      if (!res.ok) throw new Error(`status ${res.status}`)
      setDawn((await res.json()) as Dawn)
    } catch {
      setError(true)
      setDawn(null)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  // APs visibles según el filtro de banda.
  const aps = useMemo(() => {
    if (!dawn) return []
    return dawn.aps.filter((a) => band === 'all' || a.band === band)
  }, [dawn, band])

  // Filas = un cliente por MAC, con su señal vista desde cada AP (BSSID).
  const rows = useMemo(() => {
    const byMac = new Map<string, { mac: string; signals: Map<string, number> }>()
    for (const ap of aps) {
      for (const c of ap.clients) {
        let r = byMac.get(c.mac)
        if (!r) {
          r = { mac: c.mac, signals: new Map() }
          byMac.set(c.mac, r)
        }
        // DAWN puede reportar la misma MAC varias veces bajo un BSSID; nos
        // quedamos con la señal más fuerte (la medición más reciente/sana).
        const prev = r.signals.get(ap.bssid)
        if (prev === undefined || c.signal > prev) r.signals.set(ap.bssid, c.signal)
      }
    }
    let arr = [...byMac.values()]
    if (weakOnly) {
      arr = arr.filter((r) => [...r.signals.values()].every((s) => s < -70))
    }
    // Orden: mejor señal vista ascendente (los clientes peor vistos arriba).
    arr.sort((a, b) => {
      const am = Math.max(...a.signals.values())
      const bm = Math.max(...b.signals.values())
      if (am !== bm) return am - bm
      return (nameByMac.get(a.mac) ?? a.mac).localeCompare(nameByMac.get(b.mac) ?? b.mac)
    })
    return arr
  }, [aps, weakOnly, nameByMac])

  const bandOptions: Band[] = ['all', '2.4 GHz', '5 GHz']
  const tabs: { id: Tab; label: string; soon: boolean }[] = [
    { id: 'matrix', label: t('roaming.tabMatrix'), soon: false },
    { id: '11r', label: t('roaming.tab11r'), soon: true },
    { id: 'survey', label: t('roaming.tabSurvey'), soon: true },
    { id: 'events', label: t('roaming.tabEvents'), soon: true },
  ]

  const initial = reduce ? false : { opacity: 0, y: 12 }

  return (
    <div className="space-y-4 md:space-y-5">
      {/* ① Page header */}
      <header>
        <motion.nav
          initial={initial}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.25, ease: 'easeOut' }}
          aria-label={t('common.breadcrumb')}
          className="font-mono text-caption text-text-muted"
        >
          <Link to="/" className="transition-colors hover:text-accent">{t('common.home')}</Link>
          <span className="mx-1.5">/</span>
          <span className="text-text-secondary">{t('nav.roaming')}</span>
        </motion.nav>
        <div className="mt-1.5 flex flex-wrap items-end justify-between gap-x-4 gap-y-3">
          <motion.div
            initial={initial}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, ease: 'easeOut', delay: 0.06 }}
          >
            <h1 className="font-display text-h1 text-text-primary">{t('nav.roaming')}</h1>
            <p className="text-caption text-text-muted">{t('roaming.subtitle')}</p>
          </motion.div>
          <motion.button
            initial={initial}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, ease: 'easeOut', delay: 0.12 }}
            onClick={() => {
              void load()
              if (reduce) return
              setSpin(true)
              window.setTimeout(() => setSpin(false), 650)
            }}
            className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-surface px-3 text-sm font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
          >
            <RefreshCw className={cn('h-4 w-4 transition-transform duration-500', spin && 'rotate-[360deg]')} strokeWidth={1.75} />
            {t('common.refresh')}
          </motion.button>
        </div>
      </header>

      {/* ② Pestañas */}
      <div className="inline-flex items-center gap-1 rounded-lg border border-border bg-surface p-1" role="tablist">
        {tabs.map((tb) => (
          <button
            key={tb.id}
            role="tab"
            aria-selected={tab === tb.id}
            onClick={() => setTab(tb.id)}
            className={cn(
              'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
              tab === tb.id ? 'bg-accent/15 text-accent' : 'text-text-muted hover:text-text-secondary',
            )}
          >
            {tb.label}
          </button>
        ))}
      </div>

      {/* ③ Contenido */}
      {tab !== 'matrix' && (
        <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
          {t('roaming.comingSoon')}
        </div>
      )}

      {tab === 'matrix' && (
        <>
          {loading && (!dawn || aps.length === 0) && (
            <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
              {t('roaming.loading')}
            </div>
          )}
          {error && (
            <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
              {t('roaming.error')}
            </div>
          )}
          {!loading && !error && dawn && dawn.aps.length === 0 && (
            <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
              {t('roaming.empty')}
            </div>
          )}

          {!error && dawn && aps.length > 0 && (
            <motion.section
              initial={initial}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.25, ease: 'easeOut', delay: 0.08 }}
              className="rounded-2xl border border-border bg-surface p-5 md:p-6"
            >
              {/* Título + filtros */}
              <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
                <div className="flex items-start gap-2">
                  <Wifi className="mt-0.5 h-4 w-4 shrink-0 text-accent" strokeWidth={1.75} />
                  <div>
                    <h2 className="font-display text-h2 text-text-primary">{t('roaming.matrix.title')}</h2>
                    <p className="mt-0.5 max-w-2xl text-caption text-text-muted">{t('roaming.matrix.description')}</p>
                  </div>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <div className="inline-flex items-center gap-1 rounded-lg border border-border bg-elevated p-1" role="group" aria-label={t('roaming.matrix.filterBand')}>
                    {bandOptions.map((b) => (
                      <button
                        key={b}
                        onClick={() => setBand(b)}
                        className={cn(
                          'rounded-md px-2.5 py-1 text-caption font-medium transition-colors',
                          band === b ? 'bg-accent/15 text-accent' : 'text-text-muted hover:text-text-secondary',
                        )}
                      >
                        {b === 'all' ? t('roaming.matrix.allBands') : b}
                      </button>
                    ))}
                  </div>
                  <label className="inline-flex cursor-pointer items-center gap-2 rounded-lg border border-border bg-elevated px-2.5 py-1.5 text-caption text-text-secondary">
                    <input
                      type="checkbox"
                      checked={weakOnly}
                      onChange={(e) => setWeakOnly(e.target.checked)}
                      className="h-3.5 w-3.5 accent-[rgb(var(--accent))]"
                    />
                    {t('roaming.matrix.filterWeak')}
                  </label>
                </div>
              </div>

              {/* Contadores */}
              <div className="mb-4 flex flex-wrap gap-x-5 gap-y-1 text-caption text-text-muted">
                <span>{t('roaming.matrix.aps', { count: aps.length })}</span>
                <span>{t('roaming.matrix.clients', { count: rows.length })}</span>
              </div>

              {/* Matriz */}
              {rows.length === 0 ? (
                <p className="py-6 text-center text-caption text-text-muted">{t('roaming.matrix.noClients')}</p>
              ) : (
                <div className="overflow-x-auto">
                  <table className="w-full border-separate border-spacing-0 text-left text-sm">
                    <thead>
                      <tr>
                        <th className="sticky left-0 z-10 bg-surface pb-2.5 pr-3 text-label font-medium uppercase text-text-muted">
                          {t('roaming.matrix.colClient')}
                        </th>
                        {aps.map((ap) => (
                          <th key={ap.bssid} className="pb-2.5 pl-2 pr-3 text-label font-medium text-text-secondary">
                            <div className="flex flex-col gap-0.5">
                              <span className="font-medium text-text-primary">{ap.hostname}</span>
                              <span className="font-mono text-caption text-text-muted">
                                {ap.band === '5 GHz' ? '5G' : '2.4G'} · ch{ap.channel}
                              </span>
                            </div>
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {rows.map((r) => {
                        const name = nameByMac.get(r.mac) ?? r.mac
                        return (
                          <tr key={r.mac} className="group">
                            <th scope="row" className="sticky left-0 z-10 border-b border-border/60 bg-surface py-2 pr-3 text-left text-sm font-medium text-text-primary">
                              <div className="flex flex-col">
                                <span className="truncate">{name}</span>
                                {name !== r.mac && <span className="font-mono text-caption text-text-muted">{r.mac}</span>}
                              </div>
                            </th>
                            {aps.map((ap) => {
                              const s = r.signals.get(ap.bssid)
                              return (
                                <td key={ap.bssid} className="border-b border-border/60 py-2 pl-2 pr-3">
                                  {s !== undefined ? (
                                    <span
                                      title={`${name} → ${ap.hostname} (${ap.band}): ${s} dBm`}
                                      className={cn('inline-block min-w-[3rem] rounded-md px-2 py-0.5 text-center font-mono text-mono-sm ring-1 ring-inset', signalClass(s))}
                                    >
                                      {s}
                                    </span>
                                  ) : (
                                    <span className="text-text-muted">·</span>
                                  )}
                                </td>
                              )
                            })}
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                </div>
              )}

              {/* Leyenda */}
              <div className="mt-5 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-caption text-text-muted">
                <span className="inline-flex items-center gap-1.5">
                  <span className="inline-block h-2.5 w-2.5 rounded-sm bg-ok/40 ring-1 ring-inset ring-ok/40" />
                  {t('roaming.matrix.legendGood')}
                </span>
                <span className="inline-flex items-center gap-1.5">
                  <span className="inline-block h-2.5 w-2.5 rounded-sm bg-warn/40 ring-1 ring-inset ring-warn/40" />
                  {t('roaming.matrix.legendFair')}
                </span>
                <span className="inline-flex items-center gap-1.5">
                  <span className="inline-block h-2.5 w-2.5 rounded-sm bg-danger/40 ring-1 ring-inset ring-danger/40" />
                  {t('roaming.matrix.legendWeak')}
                </span>
              </div>
            </motion.section>
          )}
        </>
      )}
    </div>
  )
}
