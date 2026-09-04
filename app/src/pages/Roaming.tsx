import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { motion, useReducedMotion } from 'framer-motion'
import { AlertTriangle, ArrowDownToLine, ArrowUpFromLine, CheckCircle2, GitFork, History, RefreshCw, Wifi, X, XCircle } from 'lucide-react'
import { cn, fetchJson } from '@/lib/utils'
import { useNetPulse, redirectLogin } from '@/data/DataProvider'
import ReanchorPanel from './ReanchorPanel'

// ---------------------------------------------------------------------------
// Tipos del contrato GET /api/usteer (server-go/internal/adapters/types.go).
// ---------------------------------------------------------------------------

interface UsteerClient {
  mac: string
  signal: number // -dBm
}
interface UsteerAP {
  ssid: string
  bssid: string
  hostname: string
  band: string
  freq: number
  utilizationPct: number
  clientCount: number
  clients: UsteerClient[]
  local: boolean
  iface: string
}
interface UsteerMesh {
  routerId: string
  name: string
  usteer: boolean
  apsSeen: number
}
interface Usteer {
  aps: UsteerAP[]
  mesh: UsteerMesh[]
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
  /** Anomalías detectadas en la configuración de roaming (#428). */
  anomalies?: { kind: string; ssid?: string; message: string; routerId?: string }[]
}

// ---------------------------------------------------------------------------
// Tipos del contrato GET /api/survey (server-go/internal/adapters/types.go).
// ---------------------------------------------------------------------------

interface SurveyChannel {
  freq: number
  channel: number
  inUse: boolean
  noiseDbm: number
  busyPct: number
  rxPct: number
  txPct: number
}
interface SurveyRadio {
  device: string
  band: string
  channels: SurveyChannel[]
}
interface SurveyRouter {
  routerId: string
  name: string
  available: boolean
  radios: SurveyRadio[]
}
interface SurveyOverview {
  available: boolean
  routers: SurveyRouter[]
}

// ---------------------------------------------------------------------------
// Tipos del contrato GET /api/roam-events (server-go/internal/roamevents).
// ---------------------------------------------------------------------------

interface RoamEvent {
  id: number
  ts_ms: number
  router_id: string
  type: 'connected' | 'disconnected' | 'dawn_decision'
  mac?: string
  iface?: string
  detail?: string
}

type Band = 'all' | '2.4 GHz' | '5 GHz'
type Tab = 'matrix' | '11r' | 'survey' | 'events' | 'reanchor'

/** Orden estable de pestañas (issue #229: navegación por teclado). */
const TAB_IDS: Tab[] = ['matrix', '11r', 'survey', 'events', 'reanchor']

/** Clase de color por señal (-dBm): verde óptimo, ámbar aceptable, rojo límite. */
function signalClass(s: number): string {
  if (s >= -65) return 'bg-ok/15 text-ok ring-ok/30'
  if (s >= -80) return 'bg-warn/15 text-warn ring-warn/30'
  return 'bg-danger/15 text-danger ring-danger/30'
}

/** Badge y estilo para el daemon de roaming activo (#428). */
function roamingDaemonMeta(
  daemon: string | undefined,
  t: (k: string) => string,
): { label: string; cls: string } | null {
  switch (daemon) {
    case 'usteer':
      return { label: t('roaming.daemon.usteer'), cls: 'bg-accent/15 text-accent ring-accent/30' }
    case 'dawn':
      return { label: t('roaming.daemon.dawn'), cls: 'bg-warn/15 text-warn ring-warn/30' }
    case 'both':
      return { label: t('roaming.daemon.both'), cls: 'bg-danger/15 text-danger ring-danger/30' }
    case 'none':
      return { label: t('roaming.daemon.none'), cls: 'bg-text-muted/15 text-text-muted ring-text-muted/30' }
    default:
      return null
  }
}

export default function Roaming() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const { devices, isDemo, dawnDeprecated, roamingDaemon } = useNetPulse()
  const [tab, setTab] = useState<Tab>('matrix')
  const [band, setBand] = useState<Band>('all')
  const [weakOnly, setWeakOnly] = useState(false)
  const [usteer, setUsteer] = useState<Usteer | null>(null)
  const [dot11r, setDot11r] = useState<Dot11rOverview | null>(null)
  const [dot11rLoading, setDot11rLoading] = useState(false)
  const [dot11rError, setDot11rError] = useState(false)
  const [dot11rNoApi, setDot11rNoApi] = useState(false)
  const [survey, setSurvey] = useState<SurveyOverview | null>(null)
  const [surveyLoading, setSurveyLoading] = useState(false)
  const [surveyError, setSurveyError] = useState(false)
  const [surveyNoApi, setSurveyNoApi] = useState(false)
  const [surveyBand, setSurveyBand] = useState<'all' | '2.4 GHz' | '5 GHz'>('all')
  const [events, setEvents] = useState<RoamEvent[]>([])
  const [eventsLoading, setEventsLoading] = useState(false)
  const [eventsError, setEventsError] = useState(false)
  const [eventsNoApi, setEventsNoApi] = useState(false)
  const [eventsTypeFilter, setEventsTypeFilter] = useState<'all' | 'connected' | 'disconnected' | 'dawn_decision'>('all')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [noApi, setNoApi] = useState(false)
  const [spin, setSpin] = useState(false)
  const [kicking, setKicking] = useState<string | null>(null)
  const [kickError, setKickError] = useState<string | null>(null)
  const [reanchorTick, setReanchorTick] = useState(0)

  // MAC → nombre (resuelto desde la lista de dispositivos del overview).
  const nameByMac = useMemo(() => {
    const m = new Map<string, string>()
    for (const d of devices) {
      if (d.mac) m.set(d.mac.toUpperCase(), d.name)
    }
    return m
  }, [devices])

  // Un AbortController por fuente de datos (#221): cada carga aborta la
  // anterior en vuelo para que una respuesta vieja y lenta nunca sobrescriba
  // una más nueva (cambio de pestaña/rango o Refresh repetido). En unmount se
  // abortan todos.
  const usteerAc = useRef<AbortController | null>(null)
  const dot11rAc = useRef<AbortController | null>(null)
  const surveyAc = useRef<AbortController | null>(null)
  const eventsAc = useRef<AbortController | null>(null)
  // Timer del spin del botón Refresh (#227): limpiado en unmount.
  const spinTimer = useRef<number | null>(null)

  async function load() {
    usteerAc.current?.abort()
    const ac = new AbortController()
    usteerAc.current = ac
    setLoading(true)
    setError(false)
    setNoApi(false)
    const result = await fetchJson<Usteer>('/api/usteer', { signal: ac.signal })
    if (ac.signal.aborted) return
    if (result.ok) {
      setUsteer(result.data)
    } else if (result.kind === 'unauthorized') {
      redirectLogin()
    } else if (result.kind === 'no-api' && isDemo) {
      setNoApi(true)
      setUsteer(null)
    } else {
      setError(true)
      setUsteer(null)
    }
    setLoading(false)
  }

  // Carga perezosa de /api/dot11r solo cuando se abre la pestaña 11r (es un
  // SSH a cada router con wifi, más caro que la matriz DAWN que ya está cache
  // en el primer router). Recarga también al pulsar Refresh con 11r abierta.
  async function loadDot11r() {
    dot11rAc.current?.abort()
    const ac = new AbortController()
    dot11rAc.current = ac
    setDot11rLoading(true)
    setDot11rError(false)
    setDot11rNoApi(false)
    const result = await fetchJson<Dot11rOverview>('/api/dot11r', { signal: ac.signal })
    if (ac.signal.aborted) return
    if (result.ok) {
      setDot11r(result.data)
    } else if (result.kind === 'unauthorized') {
      redirectLogin()
    } else if (result.kind === 'no-api' && isDemo) {
      setDot11rNoApi(true)
      setDot11r(null)
    } else {
      setDot11rError(true)
      setDot11r(null)
    }
    setDot11rLoading(false)
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

  // Carga perezosa de /api/survey: igual que dot11r, un SSH por router con wifi.
  async function loadSurvey() {
    surveyAc.current?.abort()
    const ac = new AbortController()
    surveyAc.current = ac
    setSurveyLoading(true)
    setSurveyError(false)
    setSurveyNoApi(false)
    const result = await fetchJson<SurveyOverview>('/api/survey', { signal: ac.signal })
    if (ac.signal.aborted) return
    if (result.ok) {
      setSurvey(result.data)
    } else if (result.kind === 'unauthorized') {
      redirectLogin()
    } else if (result.kind === 'no-api' && isDemo) {
      setSurveyNoApi(true)
      setSurvey(null)
    } else {
      setSurveyError(true)
      setSurvey(null)
    }
    setSurveyLoading(false)
  }

  useEffect(() => {
    if (tab === 'survey' && survey === null && !surveyLoading && !surveyError) {
      void loadSurvey()
    }
  }, [tab, survey, surveyLoading, surveyError])

  // Carga perezosa de /api/roam-events. Polling cada 30s mientras la pestaña
  // está activa (los eventos llegan por ingest continua al SQLite).
  async function loadEvents() {
    eventsAc.current?.abort()
    const ac = new AbortController()
    eventsAc.current = ac
    setEventsLoading(true)
    setEventsError(false)
    setEventsNoApi(false)
    const result = await fetchJson<{ events: RoamEvent[] }>('/api/roam-events?limit=100', { signal: ac.signal })
    if (ac.signal.aborted) return
    if (result.ok) {
      setEvents(result.data.events ?? [])
    } else if (result.kind === 'unauthorized') {
      redirectLogin()
    } else if (result.kind === 'no-api' && isDemo) {
      setEventsNoApi(true)
      setEvents([])
    } else {
      setEventsError(true)
      setEvents([])
    }
    setEventsLoading(false)
  }

  useEffect(() => () => {
    usteerAc.current?.abort()
    dot11rAc.current?.abort()
    surveyAc.current?.abort()
    eventsAc.current?.abort()
    if (spinTimer.current !== null) window.clearTimeout(spinTimer.current)
  }, [])

  useEffect(() => {
    if (tab !== 'events') return
    void loadEvents()
    const id = window.setInterval(() => void loadEvents(), 30_000)
    return () => {
      window.clearInterval(id)
      eventsAc.current?.abort()
    }
  }, [tab])

  // Expulsar a un cliente de su AP actual para forzar la reconexión
  // (usteering manual cuando usteer no cambia al cliente).
  async function kick(mac: string) {
    if (kicking) return
    setKicking(mac)
    setKickError(null)
    try {
      const res = await fetch(`/api/usteer/${encodeURIComponent(mac)}/kick`, { method: 'POST' })
      if (res.status === 401) redirectLogin()
      if (!res.ok) {
        let msg = t('roaming.kick.error')
        try {
          const j = (await res.json()) as { error?: string; message?: string }
          if (j.message) msg = j.message
          else if (j.error) msg = j.error
        } catch { /* si el body no es JSON, usamos el mensaje por defecto */ }
        setKickError(`${nameByMac.get(mac) ?? mac}: ${msg}`)
        setKicking(null)
        return
      }
      void load()
      void loadEvents()
    } catch {
      setKickError(`${nameByMac.get(mac) ?? mac}: ${t('roaming.kick.error')}`)
    } finally {
      setKicking((cur) => (cur === mac ? null : cur))
    }
  }

  // APs visibles según el filtro de banda.
  const aps = useMemo(() => {
    if (!usteer) return []
    return usteer.aps.filter((a) => band === 'all' || a.band === band)
  }, [usteer, band])

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
        // usteer puede reportar la misma MAC varias veces bajo un BSSID; nos
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
    { id: 'survey', label: t('roaming.tabSurvey'), soon: false },
    { id: 'events', label: t('roaming.tabEvents'), soon: false },
    { id: 'reanchor', label: t('roaming.tabReanchor'), soon: false },
  ]

  // -- pestañas WAI-ARIA (issue #229): roving tabindex + flechas + Home/End
  const tabRefs = useRef<(HTMLButtonElement | null)[]>([])

  const onTablistKeyDown = useCallback(
    (e: ReactKeyboardEvent<HTMLDivElement>) => {
      const idx = TAB_IDS.indexOf(tab)
      let next = idx
      if (e.key === 'ArrowRight') next = (idx + 1) % TAB_IDS.length
      else if (e.key === 'ArrowLeft') next = (idx - 1 + TAB_IDS.length) % TAB_IDS.length
      else if (e.key === 'Home') next = 0
      else if (e.key === 'End') next = TAB_IDS.length - 1
      else return
      e.preventDefault()
      const target = TAB_IDS[next]!
      setTab(target)
      tabRefs.current[next]?.focus()
    },
    [tab],
  )

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
            {(() => {
              const meta = roamingDaemonMeta(roamingDaemon, t)
              if (!meta) return null
              return (
                <span
                  className={cn(
                    'mt-2 inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium ring-1 ring-inset',
                    meta.cls,
                  )}
                >
                  <Wifi className="h-3.5 w-3.5" strokeWidth={1.75} />
                  {meta.label}
                </span>
              )
            })()}
          </motion.div>
          <motion.button
            initial={initial}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, ease: 'easeOut', delay: 0.12 }}
            onClick={() => {
              void load()
              if (tab === '11r') void loadDot11r()
              if (tab === 'survey') void loadSurvey()
              if (tab === 'events') void loadEvents()
              if (tab === 'reanchor') setReanchorTick((n) => n + 1)
              if (reduce) return
              setSpin(true)
              if (spinTimer.current !== null) window.clearTimeout(spinTimer.current)
              spinTimer.current = window.setTimeout(() => {
                spinTimer.current = null
                setSpin(false)
              }, 650)
            }}
            className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-surface px-3 text-sm font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
          >
            <RefreshCw className={cn('h-4 w-4 transition-transform duration-500', spin && 'rotate-[360deg]')} strokeWidth={1.75} />
            {t('common.refresh')}
          </motion.button>
        </div>
      </header>

      {/* Banner de deprecación de DAWN (#426). Solo aparece cuando el backend
          detecta al menos un router con DAWN en uso. */}
      {dawnDeprecated && (
        <motion.div
          initial={initial}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.25, ease: 'easeOut', delay: 0.04 }}
          className="flex items-start gap-3 rounded-xl border border-warn/30 bg-warn/10 p-4 text-text-primary"
          role="status"
          aria-live="polite"
        >
          <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-warn" strokeWidth={1.75} />
          <div className="min-w-0 flex-1 space-y-1">
            <p className="font-medium">{t('roaming.dawnDeprecated.title')}</p>
            <p className="text-sm leading-relaxed text-text-secondary">{t('roaming.dawnDeprecated.body')}</p>
            <a
              href="https://github.com/gnacho/netpulse/issues/426"
              target="_blank"
              rel="noreferrer"
              className="inline-block text-sm font-medium text-warn hover:underline"
            >
              {t('roaming.dawnDeprecated.link')}
            </a>
          </div>
        </motion.div>
      )}

      {/* ② Pestañas */}
      <div
        role="tablist"
        aria-label={t('roaming.tabsLabel')}
        onKeyDown={onTablistKeyDown}
        className="inline-flex items-center gap-1 rounded-lg border border-border bg-surface p-1"
      >
        {tabs.map((tb, i) => (
          <button
            key={tb.id}
            ref={(el) => {
              tabRefs.current[i] = el
            }}
            id={`tab-${tb.id}`}
            role="tab"
            aria-selected={tab === tb.id}
            aria-controls={`panel-${tb.id}`}
            tabIndex={tab === tb.id ? 0 : -1}
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
      {tab !== 'matrix' && tab !== '11r' && tab !== 'survey' && tab !== 'events' && (
        <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
          {t('roaming.comingSoon')}
        </div>
      )}

      {tab === '11r' && (
        <div role="tabpanel" id="panel-11r" aria-labelledby="tab-11r" tabIndex={0}>
          <Dot11rPanel overview={dot11r} loading={dot11rLoading} error={dot11rError} noApi={dot11rNoApi} />
        </div>
      )}

      {tab === 'survey' && (
        <div role="tabpanel" id="panel-survey" aria-labelledby="tab-survey" tabIndex={0}>
          <SurveyPanel overview={survey} loading={surveyLoading} error={surveyError} noApi={surveyNoApi} band={surveyBand} setBand={setSurveyBand} />
        </div>
      )}

      {tab === 'events' && (
        <div role="tabpanel" id="panel-events" aria-labelledby="tab-events" tabIndex={0}>
          <EventsPanel events={events} loading={eventsLoading} error={eventsError} noApi={eventsNoApi} typeFilter={eventsTypeFilter} setTypeFilter={setEventsTypeFilter} nameByMac={nameByMac} />
        </div>
      )}

      {tab === 'reanchor' && (
        <div role="tabpanel" id="panel-reanchor" aria-labelledby="tab-reanchor" tabIndex={0}>
          <ReanchorPanel refreshTick={reanchorTick} />
        </div>
      )}

      {tab === 'matrix' && (
        <div role="tabpanel" id="panel-matrix" aria-labelledby="tab-matrix" tabIndex={0}>
          {loading && (!usteer || aps.length === 0) && (
            <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
              {t('roaming.loading')}
            </div>
          )}
          {noApi && (
            <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
              {t('roaming.noApi')}
            </div>
          )}
          {error && (
            <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
              {t('roaming.error')}
            </div>
          )}
          {!loading && !error && usteer && usteer.aps.length === 0 && (
            <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
              {t('roaming.empty')}
            </div>
          )}

          {!error && usteer && aps.length > 0 && (
            <motion.section
              initial={initial}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.25, ease: 'easeOut', delay: 0.08 }}
              className="rounded-2xl border border-border bg-surface p-5 md:p-6"
            >
              {kickError && (
                <div className="mb-4 flex items-center gap-2 rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">
                  <XCircle className="h-4 w-4 shrink-0" strokeWidth={1.75} />
                  {kickError}
                  <button
                    type="button"
                    onClick={() => setKickError(null)}
                    className="ml-auto rounded p-1 hover:bg-danger/10"
                    aria-label={t('common.close')}
                  >
                    <X className="h-3.5 w-3.5" strokeWidth={1.75} />
                  </button>
                </div>
              )}
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
                                {ap.band === '5 GHz' ? '5G' : '2.4G'}
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
                              <div className="flex items-center justify-between gap-2">
                                <div className="flex min-w-0 flex-col">
                                  <span className="truncate">{name}</span>
                                  {name !== r.mac && <span className="font-mono text-caption text-text-muted">{r.mac}</span>}
                                </div>
                                <button
                                  type="button"
                                  disabled={kicking === r.mac}
                                  onClick={() => void kick(r.mac)}
                                  title={t('roaming.kick.title')}
                                  className="shrink-0 rounded-md p-1 text-text-muted opacity-0 transition-colors hover:bg-danger/10 hover:text-danger group-hover:opacity-100 focus:opacity-100 disabled:cursor-not-allowed disabled:opacity-50"
                                  aria-label={t('roaming.kick.label', { name: nameByMac.get(r.mac) ?? r.mac })}
                                >
                                  <X className="h-4 w-4" strokeWidth={1.75} />
                                </button>
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
        </div>
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
  noApi,
}: {
  overview: Dot11rOverview | null
  loading: boolean
  error: boolean
  noApi: boolean
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
  if (noApi) {
    return (
      <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
        {t('roaming.noApi')}
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

      {/* Anomalías de roaming (#428) */}
      {(overview.anomalies?.length ?? 0) > 0 && (
        <div className="space-y-2">
          {overview.anomalies!.map((a, i) => (
            <div
              key={`${a.kind}-${a.ssid ?? ''}-${i}`}
              className={cn(
                'flex items-start gap-3 rounded-xl border p-4',
                a.kind === 'ft_mode_mismatch' || a.kind === 'mobility_domain_mismatch'
                  ? 'border-danger/30 bg-danger/10 text-text-primary'
                  : 'border-warn/30 bg-warn/10 text-text-primary',
              )}
              role="status"
            >
              <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-warn" strokeWidth={1.75} />
              <p className="text-sm leading-relaxed text-text-secondary">{a.message}</p>
            </div>
          ))}
        </div>
      )}

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

// ---------------------------------------------------------------------------
// Pestaña Survey (Fase 14.4) — utilización por canal, por router y radio
// ---------------------------------------------------------------------------

type SurveyBand = 'all' | '2.4 GHz' | '5 GHz'

function busyClass(p: number): string {
  if (p >= 70) return 'bg-danger/15 text-danger ring-danger/30'
  if (p >= 40) return 'bg-warn/15 text-warn ring-warn/30'
  return 'bg-ok/15 text-ok ring-ok/30'
}

function noiseClass(dbm: number): string {
  // Más cercano a 0 = peor. -90 óptimo, -70 malo.
  if (dbm >= -75) return 'text-danger'
  if (dbm >= -85) return 'text-warn'
  return 'text-ok'
}

// spectrumCellColor (#535): color de la celda de canal según la ocupación.
// Verde = libre, ámbar = ocupado, rojo = congestionado (misma escala busyClass).
function spectrumCellBg(p: number): string {
  if (p >= 70) return 'bg-danger/30'
  if (p >= 40) return 'bg-warn/30'
  return 'bg-ok/25'
}

// SurveySpectrum (#535): tira horizontal de espectro por radio. Un bloque por
// canal (ordenado por frecuencia), coloreado por uso (busyPct), con el canal
// en el que opera la radio (inUse) resaltado con borde. Tooltip con el detalle
// (canal, frecuencia, ruido, uso). Visualización al estilo channel_analysis de
// LuCI, sin tocar la tabla de detalle que se mantiene debajo.
function SurveySpectrum({ radio }: { radio: SurveyRadio }) {
  const { t } = useTranslation()
  const chans = useMemo(() => [...radio.channels].sort((a, b) => a.freq - b.freq), [radio.channels])
  if (chans.length === 0) return null
  return (
    <div className="overflow-x-auto">
      <div className="flex flex-col gap-1 pb-1">
        <div className="flex items-end gap-1">
          {chans.map((c) => (
            <div key={c.freq} className="flex flex-col items-center gap-0.5">
              <div
                title={`${t('roaming.survey.colChannel')} ${c.channel} · ${c.freq} MHz · ${t('roaming.survey.colNoise')} ${c.noiseDbm} dBm · ${t('roaming.survey.colBusy')} ${c.busyPct.toFixed(1)}%`}
                className={cn(
                  'h-7 w-7 sm:w-8 rounded-md ring-1 ring-inset transition-colors',
                  spectrumCellBg(c.busyPct),
                  c.inUse
                    ? 'ring-2 ring-accent'
                    : 'ring-border/60',
                )}
              />
              <span className={cn('font-mono text-[10px] leading-none', c.inUse ? 'font-semibold text-accent' : 'text-text-muted')}>
                {c.channel}
              </span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

function SurveyPanel({
  overview,
  loading,
  error,
  noApi,
  band,
  setBand,
}: {
  overview: SurveyOverview | null
  loading: boolean
  error: boolean
  noApi: boolean
  band: SurveyBand
  setBand: (b: SurveyBand) => void
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
  if (noApi) {
    return (
      <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
        {t('roaming.noApi')}
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
  if (!overview || !overview.available || overview.routers.length === 0) {
    return (
      <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
        {t('roaming.survey.empty')}
      </div>
    )
  }

  const bandOptions: SurveyBand[] = ['all', '2.4 GHz', '5 GHz']

  return (
    <motion.section
      initial={initial}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, ease: 'easeOut', delay: 0.08 }}
      className="space-y-4"
    >
      {/* Header + filtro banda */}
      <div className="rounded-2xl border border-border bg-surface p-5 md:p-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex items-start gap-2">
            <Wifi className="mt-0.5 h-4 w-4 shrink-0 text-accent" strokeWidth={1.75} />
            <div>
              <h2 className="font-display text-h2 text-text-primary">{t('roaming.survey.title')}</h2>
              <p className="mt-0.5 max-w-2xl text-caption text-text-muted">{t('roaming.survey.description')}</p>
            </div>
          </div>
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
        </div>
      </div>

      {/* Tabla por router */}
      {overview.routers.map((r) => {
        const radios = r.radios.filter((rd) => band === 'all' || rd.band === band)
        if (!r.available || radios.length === 0) {
          return (
            <div key={r.routerId} className="rounded-2xl border border-border bg-surface p-5">
              <h3 className="font-display text-h3 text-text-primary">{r.name}</h3>
              <p className="mt-2 text-caption text-text-muted">{t('roaming.survey.unreachable')}</p>
            </div>
          )
        }
        return (
          <div key={r.routerId} className="rounded-2xl border border-border bg-surface p-5 md:p-6">
            <h3 className="mb-3 font-display text-h3 text-text-primary">{r.name}</h3>
            {radios.map((radio) => (
              <div key={radio.device} className="mb-4 last:mb-0">
                <div className="mb-2 flex items-center gap-2">
                  <span className="rounded-md bg-elevated px-2 py-0.5 font-mono text-caption text-text-secondary">{radio.device}</span>
                  <span className="text-caption text-text-muted">{radio.band}</span>
                </div>
                {/* Espectro por canal (#535): vista gráfica al estilo channel_analysis */}
                <div className="mb-3">
                  <SurveySpectrum radio={radio} />
                </div>
                <div className="overflow-x-auto">
                  <table className="w-full border-separate border-spacing-0 text-left text-sm">
                    <thead>
                      <tr className="text-label uppercase text-text-muted">
                        <th className="pb-2 pr-3 font-medium">{t('roaming.survey.colChannel')}</th>
                        <th className="pb-2 pr-3 font-medium">{t('roaming.survey.colFreq')}</th>
                        <th className="pb-2 pr-3 font-medium">{t('roaming.survey.colNoise')}</th>
                        <th className="pb-2 pr-3 font-medium">{t('roaming.survey.colBusy')}</th>
                        <th className="pb-2 pr-3 font-medium">{t('roaming.survey.colRx')}</th>
                        <th className="pb-2 pr-3 font-medium">{t('roaming.survey.colTx')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {radio.channels.map((c) => (
                        <tr key={c.freq} className={cn(c.inUse && 'bg-accent/5')}>
                          <td className="border-b border-border/60 py-2 pr-3">
                            <span className="font-mono text-text-primary">
                              {c.channel || '—'}
                              {c.inUse && (
                                <span className="ml-1.5 rounded bg-accent-soft px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-accent">
                                  {t('roaming.survey.inUse')}
                                </span>
                              )}
                            </span>
                          </td>
                          <td className="border-b border-border/60 py-2 pr-3 font-mono text-caption text-text-muted">{c.freq}</td>
                          <td className={cn('border-b border-border/60 py-2 pr-3 font-mono text-mono-sm', noiseClass(c.noiseDbm))}>
                            {c.noiseDbm} dBm
                          </td>
                          <td className="border-b border-border/60 py-2 pr-3">
                            <span className={cn('inline-block min-w-[3.5rem] rounded-md px-2 py-0.5 text-center font-mono text-mono-sm ring-1 ring-inset', busyClass(c.busyPct))}>
                              {c.busyPct.toFixed(1)}%
                            </span>
                          </td>
                          <td className="border-b border-border/60 py-2 pr-3 font-mono text-caption text-text-secondary">{c.rxPct.toFixed(1)}%</td>
                          <td className="border-b border-border/60 py-2 pr-3 font-mono text-caption text-text-secondary">{c.txPct.toFixed(1)}%</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            ))}
          </div>
        )
      })}

      {/* Leyenda */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-caption text-text-muted">
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block h-2.5 w-2.5 rounded-sm bg-ok/40 ring-1 ring-inset ring-ok/40" />
          {t('roaming.survey.legendFree')}
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block h-2.5 w-2.5 rounded-sm bg-warn/40 ring-1 ring-inset ring-warn/40" />
          {t('roaming.survey.legendBusy')}
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block h-2.5 w-2.5 rounded-sm bg-danger/40 ring-1 ring-inset ring-danger/40" />
          {t('roaming.survey.legendCongested')}
        </span>
      </div>
    </motion.section>
  )
}

// ---------------------------------------------------------------------------
// Pestaña Eventos (Fase 14.5) — feed temporal de hostapd/DAWN con histórico
// ---------------------------------------------------------------------------

type EventTypeFilter = 'all' | 'connected' | 'disconnected' | 'dawn_decision'

function eventMeta(type: string): { icon: typeof Wifi; cls: string; label: string } {
  switch (type) {
    case 'connected':
      return { icon: ArrowDownToLine, cls: 'text-ok', label: 'roaming.events.connected' }
    case 'disconnected':
      return { icon: ArrowUpFromLine, cls: 'text-text-muted', label: 'roaming.events.disconnected' }
    case 'dawn_decision':
      return { icon: GitFork, cls: 'text-warn', label: 'roaming.events.dawn' }
    default:
      return { icon: Wifi, cls: 'text-text-muted', label: 'roaming.events.other' }
  }
}

function formatEventTime(tsMs: number): string {
  const d = new Date(tsMs)
  const now = new Date()
  const sameDay = d.toDateString() === now.toDateString()
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  if (sameDay) return `${hh}:${mm}:${ss}`
  const dd = String(d.getDate()).padStart(2, '0')
  const mo = String(d.getMonth() + 1).padStart(2, '0')
  return `${dd}/${mo} ${hh}:${mm}`
}

function EventsPanel({
  events,
  loading,
  error,
  noApi,
  typeFilter,
  setTypeFilter,
  nameByMac,
}: {
  events: RoamEvent[]
  loading: boolean
  error: boolean
  noApi: boolean
  typeFilter: EventTypeFilter
  setTypeFilter: (t: EventTypeFilter) => void
  nameByMac: Map<string, string>
}) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const initial = reduce ? false : { opacity: 0, y: 12 }

  const filtered = useMemo(() => {
    if (typeFilter === 'all') return events
    return events.filter((e) => e.type === typeFilter)
  }, [events, typeFilter])

  if (loading && events.length === 0) {
    return (
      <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
        {t('roaming.loading')}
      </div>
    )
  }
  if (noApi) {
    return (
      <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
        {t('roaming.noApi')}
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

  const typeOptions: EventTypeFilter[] = ['all', 'connected', 'disconnected', 'dawn_decision']

  return (
    <motion.section
      initial={initial}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, ease: 'easeOut', delay: 0.08 }}
      className="space-y-4"
    >
      <div className="rounded-2xl border border-border bg-surface p-5 md:p-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex items-start gap-2">
            <History className="mt-0.5 h-4 w-4 shrink-0 text-accent" strokeWidth={1.75} />
            <div>
              <h2 className="font-display text-h2 text-text-primary">{t('roaming.events.title')}</h2>
              <p className="mt-0.5 max-w-2xl text-caption text-text-muted">{t('roaming.events.description')}</p>
            </div>
          </div>
          <div className="inline-flex items-center gap-1 rounded-lg border border-border bg-elevated p-1" role="group" aria-label={t('roaming.events.filterType')}>
            {typeOptions.map((tf) => (
              <button
                key={tf}
                onClick={() => setTypeFilter(tf)}
                className={cn(
                  'rounded-md px-2.5 py-1 text-caption font-medium transition-colors',
                  typeFilter === tf ? 'bg-accent/15 text-accent' : 'text-text-muted hover:text-text-secondary',
                )}
              >
                {t(`roaming.events.type_${tf}`)}
              </button>
            ))}
          </div>
        </div>
      </div>

      {filtered.length === 0 ? (
        <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
          {t('roaming.events.empty')}
        </div>
      ) : (
        <div className="rounded-2xl border border-border bg-surface">
          <ul className="divide-y divide-border/60">
            {filtered.map((ev) => {
              const meta = eventMeta(ev.type)
              const Icon = meta.icon
              const clientName = ev.mac ? nameByMac.get(ev.mac.toUpperCase()) ?? ev.mac : ''
              return (
                <li key={ev.id} className="flex items-start gap-3 px-4 py-3 md:px-5">
                  <Icon className={cn('mt-0.5 h-4 w-4 shrink-0', meta.cls)} strokeWidth={1.75} />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-baseline gap-x-2">
                      <span className={cn('text-sm font-medium', meta.cls)}>{t(meta.label)}</span>
                      {clientName && (
                        <span className="truncate text-sm text-text-primary">{clientName}</span>
                      )}
                      {ev.iface && (
                        <span className="rounded bg-elevated px-1.5 py-0.5 font-mono text-[10px] text-text-secondary">{ev.iface}</span>
                      )}
                      <span className="font-mono text-caption text-text-muted">{ev.router_id}</span>
                    </div>
                    {ev.detail && (
                      <p className="mt-0.5 truncate text-caption text-text-muted">{ev.detail}</p>
                    )}
                  </div>
                  <time className="shrink-0 font-mono text-caption text-text-muted" title={new Date(ev.ts_ms).toLocaleString()}>
                    {formatEventTime(ev.ts_ms)}
                  </time>
                </li>
              )
            })}
          </ul>
        </div>
      )}
    </motion.section>
  )
}
