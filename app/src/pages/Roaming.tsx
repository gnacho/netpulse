import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { motion, useReducedMotion } from 'framer-motion'
import { AlertTriangle, CheckCircle2, RefreshCw, Wifi, XCircle } from 'lucide-react'
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

// ---------------------------------------------------------------------------
// Tipos del contrato GET /api/dot11r (server-go/internal/adapters/types.go).
// ---------------------------------------------------------------------------

interface Dot11rIface {
  section: string
  device: string
  ifname: string
  ssid: string
  mac: string
  channel?: number
  band?: string
  encryption?: string
  dot11rEnabled: boolean
  mobilityDomain?: string
  ftOverDs: boolean
  ftPskGenerateLocal: boolean
  pmkR1Push?: boolean
  nasid?: string
  dot11kEnabled?: boolean
  dot11vEnabled?: boolean
  bssTransition?: boolean
  mfp?: boolean
}
interface Dot11rRouter {
  routerId: string
  name: string
  available: boolean
  ifaces: Dot11rIface[]
}
interface Dot11rSSID {
  ssid: string
  enabledEverywhere: boolean
  enabledCount: number
  totalCount: number
  mobilityDomain?: string
  ftOverDs: boolean
  ftPskGenerateLocal: boolean
  ifaceCount: number
  routerIds: string[]
}
interface Dot11rOverview {
  available: boolean
  ssids: Dot11rSSID[]
  routers: Dot11rRouter[]
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
  const [dot11r, setDot11r] = useState<Dot11rOverview | null>(null)
  const [dot11rLoading, setDot11rLoading] = useState(false)
  const [dot11rError, setDot11rError] = useState(false)
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

  // Carga perezosa de /api/dot11r solo cuando se abre la pestaña 11r (es un
  // SSH a cada router con wifi, más caro que la matriz DAWN que ya está cache
  // en el primer router). Recarga también al pulsar Refresh con 11r abierta.
  async function loadDot11r() {
    setDot11rLoading(true)
    setDot11rError(false)
    try {
      const res = await fetch('/api/dot11r')
      if (!res.ok) throw new Error(`status ${res.status}`)
      setDot11r((await res.json()) as Dot11rOverview)
    } catch {
      setDot11rError(true)
      setDot11r(null)
    } finally {
      setDot11rLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  // Al cambiar a 11r, carga si aún no se ha hecho. Si vuelve a matrix no
  // invalidamos (los datos siguen siendo buenos hasta el próximo refresh).
  useEffect(() => {
    if (tab === '11r' && dot11r === null && !dot11rLoading && !dot11rError) {
      void loadDot11r()
    }
  }, [tab, dot11r, dot11rLoading, dot11rError])

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
    { id: '11r', label: t('roaming.tab11r'), soon: false },
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
              if (tab === '11r') void loadDot11r()
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
      {tab !== 'matrix' && tab !== '11r' && (
        <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
          {t('roaming.comingSoon')}
        </div>
      )}

      {tab === '11r' && <Dot11rPanel overview={dot11r} loading={dot11rLoading} error={dot11rError} />}

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

// ---------------------------------------------------------------------------
// Pestaña 802.11r (Fase 14.3) — estado global + tabla por SSID + detalle router
// ---------------------------------------------------------------------------

function Dot11rPanel({
  overview,
  loading,
  error,
}: {
  overview: Dot11rOverview | null
  loading: boolean
  error: boolean
}) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const initial = reduce ? false : { opacity: 0, y: 12 }

  if (loading && !overview) {
    return (
      <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
        {t('roaming.loading')}
      </div>
    )
  }
  if (error) {
    return (
      <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
        {t('roaming.error')}
      </div>
    )
  }
  if (!overview || !overview.available || overview.ssids.length === 0) {
    return (
      <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
        {t('roaming.dot11r.empty')}
      </div>
    )
  }

  const ssidsEnabled = overview.ssids.filter((s) => s.enabledCount > 0).length
  const ssidsEverywhere = overview.ssids.filter((s) => s.enabledEverywhere).length

  return (
    <motion.section
      initial={initial}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, ease: 'easeOut', delay: 0.08 }}
      className="space-y-4"
    >
      {/* ① Estado global */}
      <div className="rounded-2xl border border-border bg-surface p-5 md:p-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex items-start gap-2">
            <Wifi className="mt-0.5 h-4 w-4 shrink-0 text-accent" strokeWidth={1.75} />
            <div>
              <h2 className="font-display text-h2 text-text-primary">{t('roaming.dot11r.title')}</h2>
              <p className="mt-0.5 max-w-2xl text-caption text-text-muted">{t('roaming.dot11r.description')}</p>
            </div>
          </div>
          <StateBadge
            kind={ssidsEverywhere === overview.ssids.length ? 'ok' : ssidsEnabled > 0 ? 'warn' : 'bad'}
            text={
              ssidsEverywhere === overview.ssids.length
                ? t('roaming.dot11r.allOn', { n: overview.ssids.length })
                : ssidsEnabled > 0
                  ? t('roaming.dot11r.partial', { on: ssidsEnabled, total: overview.ssids.length })
                  : t('roaming.dot11r.allOff')
            }
          />
        </div>
      </div>

      {/* ② Tabla por SSID */}
      <div className="rounded-2xl border border-border bg-surface p-5 md:p-6">
        <h3 className="mb-3 font-display text-h3 text-text-primary">{t('roaming.dot11r.bySsid')}</h3>
        <div className="overflow-x-auto">
          <table className="w-full border-separate border-spacing-0 text-left text-sm">
            <thead>
              <tr className="text-label uppercase text-text-muted">
                <th className="pb-2.5 pr-3 font-medium">{t('roaming.dot11r.colSsid')}</th>
                <th className="pb-2.5 pr-3 font-medium">{t('roaming.dot11r.colState')}</th>
                <th className="pb-2.5 pr-3 font-medium">{t('roaming.dot11r.colCoverage')}</th>
                <th className="pb-2.5 pr-3 font-medium">{t('roaming.dot11r.colMobility')}</th>
                <th className="pb-2.5 pr-3 font-medium">{t('roaming.dot11r.colFtMode')}</th>
                <th className="pb-2.5 pr-3 font-medium">{t('roaming.dot11r.colAuth')}</th>
                <th className="pb-2.5 pr-3 font-medium">{t('roaming.dot11r.colRouters')}</th>
              </tr>
            </thead>
            <tbody>
              {overview.ssids.map((s) => (
                <tr key={s.ssid} className="group">
                  <th scope="row" className="border-b border-border/60 py-2 pr-3 text-left font-medium text-text-primary">
                    {s.ssid}
                  </th>
                  <td className="border-b border-border/60 py-2 pr-3">
                    <StatePill
                      kind={s.enabledEverywhere ? 'ok' : s.enabledCount > 0 ? 'warn' : 'bad'}
                      text={
                        s.enabledEverywhere
                          ? t('roaming.dot11r.stateOn')
                          : s.enabledCount > 0
                            ? t('roaming.dot11r.statePartial')
                            : t('roaming.dot11r.stateOff')
                      }
                    />
                  </td>
                  <td className="border-b border-border/60 py-2 pr-3 font-mono text-mono-sm text-text-secondary">
                    {s.enabledCount}/{s.totalCount}
                  </td>
                  <td className="border-b border-border/60 py-2 pr-3 font-mono text-mono-sm text-text-secondary">
                    {s.mobilityDomain || '—'}
                  </td>
                  <td className="border-b border-border/60 py-2 pr-3 text-text-secondary">
                    {s.enabledCount > 0
                      ? s.ftOverDs
                        ? t('roaming.dot11r.ftOverDs')
                        : t('roaming.dot11r.ftOverAir')
                      : '—'}
                  </td>
                  <td className="border-b border-border/60 py-2 pr-3 text-text-secondary">
                    {s.enabledCount > 0 ? (s.ftPskGenerateLocal ? t('roaming.dot11r.pskLocal') : t('roaming.dot11r.radius')) : '—'}
                  </td>
                  <td className="border-b border-border/60 py-2 pr-3 font-mono text-caption text-text-muted">
                    {s.ifaceCount}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="mt-3 text-caption text-text-muted">{t('roaming.dot11r.coverageHint')}</p>
      </div>

      {/* ③ Detalle por router */}
      <div className="rounded-2xl border border-border bg-surface p-5 md:p-6">
        <h3 className="mb-3 font-display text-h3 text-text-primary">{t('roaming.dot11r.byRouter')}</h3>
        <div className="overflow-x-auto">
          <table className="w-full border-separate border-spacing-0 text-left text-sm">
            <thead>
              <tr className="text-label uppercase text-text-muted">
                <th className="pb-2.5 pr-3 font-medium">{t('roaming.dot11r.colRouter')}</th>
                <th className="pb-2.5 pr-3 font-medium">{t('roaming.dot11r.colIface')}</th>
                <th className="pb-2.5 pr-3 font-medium">{t('roaming.dot11r.colSsidR')}</th>
                <th className="pb-2.5 pr-3 font-medium">{t('roaming.dot11r.colBandCh')}</th>
                <th className="pb-2.5 pr-3 font-medium">11r</th>
                <th className="pb-2.5 pr-3 font-medium">11k</th>
                <th className="pb-2.5 pr-3 font-medium">11v</th>
                <th className="pb-2.5 pr-3 font-medium">PMF</th>
                <th className="pb-2.5 pr-3 font-medium">{t('roaming.dot11r.colMac')}</th>
              </tr>
            </thead>
            <tbody>
              {overview.routers.flatMap((r) => {
                if (r.ifaces.length === 0) {
                  return [
                    <tr key={`${r.routerId}-none`}>
                      <th scope="row" className="border-b border-border/60 py-2 pr-3 text-left font-medium text-text-primary">
                        {r.name}
                      </th>
                      <td colSpan={8} className="border-b border-border/60 py-2 pr-3 text-caption text-text-muted">
                        {r.available ? t('roaming.dot11r.noIfaces') : t('roaming.dot11r.unreachable')}
                      </td>
                    </tr>,
                  ]
                }
                return r.ifaces.map((ifc, idx) => (
                  <tr key={`${r.routerId}-${ifc.section}`}>
                    <th
                      scope="row"
                      className={cn(
                        'border-b border-border/60 py-2 pr-3 text-left font-medium text-text-primary',
                        idx > 0 && 'text-text-muted',
                      )}
                    >
                      {idx === 0 ? r.name : ''}
                    </th>
                    <td className="border-b border-border/60 py-2 pr-3 font-mono text-mono-sm text-text-secondary">
                      {ifc.ifname || ifc.section}
                      <span className="text-text-muted"> · {ifc.device}</span>
                    </td>
                    <td className="border-b border-border/60 py-2 pr-3 text-text-secondary">{ifc.ssid || '—'}</td>
                    <td className="border-b border-border/60 py-2 pr-3 font-mono text-mono-sm text-text-secondary">
                      {ifc.band ? ifc.band.replace(' GHz', 'G') : '—'}
                      {ifc.channel ? ` · ch${ifc.channel}` : ''}
                    </td>
                    <td className="border-b border-border/60 py-2 pr-3">
                      <Flag on={ifc.dot11rEnabled} label={ifc.dot11rEnabled ? (ifc.mobilityDomain || '✓') : '✕'} />
                    </td>
                    <td className="border-b border-border/60 py-2 pr-3">
                      <Flag on={!!ifc.dot11kEnabled} />
                    </td>
                    <td className="border-b border-border/60 py-2 pr-3">
                      <Flag on={!!ifc.dot11vEnabled} />
                    </td>
                    <td className="border-b border-border/60 py-2 pr-3">
                      <Flag on={!!ifc.mfp} />
                    </td>
                    <td className="border-b border-border/60 py-2 pr-3 font-mono text-caption text-text-muted">{ifc.mac || '—'}</td>
                  </tr>
                ))
              })}
            </tbody>
          </table>
        </div>
      </div>
    </motion.section>
  )
}

type StateKind = 'ok' | 'warn' | 'bad'

function StateBadge({ kind, text }: { kind: StateKind; text: string }) {
  const cls = {
    ok: 'bg-ok/15 text-ok ring-ok/30',
    warn: 'bg-warn/15 text-warn ring-warn/30',
    bad: 'bg-danger/15 text-danger ring-danger/30',
  }[kind]
  const Icon = kind === 'ok' ? CheckCircle2 : kind === 'warn' ? AlertTriangle : XCircle
  return (
    <span className={cn('inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium ring-1 ring-inset', cls)}>
      <Icon className="h-4 w-4" strokeWidth={1.75} />
      {text}
    </span>
  )
}

function StatePill({ kind, text }: { kind: StateKind; text: string }) {
  const cls = {
    ok: 'bg-ok/15 text-ok ring-ok/30',
    warn: 'bg-warn/15 text-warn ring-warn/30',
    bad: 'bg-danger/15 text-danger ring-danger/30',
  }[kind]
  return (
    <span className={cn('inline-block rounded-md px-2 py-0.5 text-caption font-medium ring-1 ring-inset', cls)}>{text}</span>
  )
}

function Flag({ on, label }: { on: boolean; label?: string }) {
  return (
    <span
      className={cn(
        'inline-block min-w-[2rem] rounded-md px-2 py-0.5 text-center font-mono text-mono-sm ring-1 ring-inset',
        on ? 'bg-ok/15 text-ok ring-ok/30' : 'bg-elevated text-text-muted ring-border',
      )}
    >
      {label ?? (on ? '✓' : '✕')}
    </span>
  )
}
