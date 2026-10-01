import { useCallback, useEffect, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { motion, useReducedMotion } from 'framer-motion'
import { Activity, AlertTriangle, CalendarDays, Clock, Download, RefreshCw } from 'lucide-react'
import { cn, fetchJson } from '@/lib/utils'
import { routerName } from '@/data/mock'
import { useNetPulse } from '@/data/DataProvider'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

// ---------------------------------------------------------------------------
// Tipos del contrato GET /api/reports/availability (server-go/internal/httpapi/reports.go)
// ---------------------------------------------------------------------------

interface AvailabilityEntry {
  routerId: string
  bucket: string // day "2026-08-07" | week "2026-W31" | month "2026-07"
  days: number
  upMin: number
  upPct: number
  latAvg: number | null
  rxTotal: number
  txTotal: number
  cpuAvg: number
  ramAvg: number
}

type Range = 'day' | 'week' | 'month'

const N_OPTIONS: Record<Range, number[]> = {
  day: [7, 14, 30, 60],
  week: [2, 4, 8, 12],
  month: [3, 6, 12, 24],
}
const DEFAULT_N: Record<Range, number> = { day: 30, week: 8, month: 12 }
const SUFFIX: Record<Range, string> = { day: 'd', week: 'w', month: 'm' }
const RANGE_TABS: Range[] = ['day', 'week', 'month']

// ---------------------------------------------------------------------------
// Umbrales de color (issue #973): el rojo solo marca un problema accionable.
//   >= 99%  ok      (verde)  menos de ~1 h 40 min de huecos a la semana: ruido normal.
//   >= 95%  warn    (ámbar)  huecos menores, hasta ~8 h a la semana.
//   <  95%  danger  (rojo)   más de ~8 h sin datos a la semana (~1,5 días al mes): requiere atención.
// Un 94% semanal son >8 h sin datos, así que sigue siendo rojo, pero ahora el
// rojo aparece UNA vez por router con su tiempo sin datos, no como muro de barras.
// Lo que se mide son minutos con datos recibidos (buckets de 5 min, #987): un
// hueco suele ser el router caído, pero también puede ser el monitor sin datos.
// ---------------------------------------------------------------------------

type Status = 'ok' | 'warn' | 'danger'

function statusOf(pct: number): Status {
  if (pct >= 99) return 'ok'
  if (pct >= 95) return 'warn'
  return 'danger'
}

const STATUS_DOT: Record<Status, string> = {
  ok: 'bg-ok',
  warn: 'bg-warn',
  danger: 'bg-danger',
}
const STATUS_TEXT: Record<Status, string> = {
  ok: 'text-ok',
  warn: 'text-warn',
  danger: 'text-danger',
}

/**
 * Minutos medibles de un bucket según la semántica del server (#987): el
 * divisor de upPct son los minutos del bucket en los que se puede afirmar
 * algo (1440 por día con datos; el día en curso solo cuenta los minutos
 * transcurridos, medidos del raw). Como upPct = upMin/divisor, se recupera
 * el divisor sin rehacer aritmética de fechas en el cliente.
 */
function bucketMinutes(e: AvailabilityEntry): number {
  if (e.upPct > 0) return e.upMin / (e.upPct / 100)
  return e.days * 1440
}

/** Minutos sin datos de un bucket = duración medible - minutos con datos. */
function bucketDownMin(e: AvailabilityEntry): number {
  return Math.max(0, bucketMinutes(e) - e.upMin)
}

/** Días que cubre la ventana seleccionada (mes = media de 30,44 días). */
function expectedDays(range: Range, n: number): number {
  if (range === 'day') return n
  if (range === 'week') return n * 7
  return Math.round(n * 30.44)
}

/**
 * Cobertura mínima para resumir la ventana con un número (#987): con menos
 * de la mitad de los días con datos, un % o un tiempo sin datos no representa
 * la ventana y la UI dice "sin datos suficientes" en vez de inventar cifras.
 */
const MIN_COVERAGE = 0.5

/** Formatea minutos a unidades humanas: "45 min", "2 h 14 min", "1 d 3 h". */
function fmtDuration(min: number): string {
  const m = Math.round(min)
  if (m < 60) return `${m} min`
  const h = Math.floor(m / 60)
  const mm = m % 60
  if (h < 48) return mm > 0 ? `${h} h ${mm} min` : `${h} h`
  const d = Math.floor(h / 24)
  const hh = h % 24
  return hh > 0 ? `${d} d ${hh} h` : `${d} d`
}

interface RouterSummary {
  id: string
  pct: number
  downMin: number
  status: Status
  days: number // días con datos en la ventana
  sufficient: boolean // cobertura suficiente para resumir la ventana (#987)
  buckets: AvailabilityEntry[] // ordenados de más antiguo a más reciente
}

/** Lunes y domingo (UTC) de una semana ISO "2026-W31", para el tooltip de la barra (#993). */
function isoWeekSpan(bucket: string): [Date, Date] | null {
  const m = /^(\d{4})-W(\d{2})$/.exec(bucket)
  if (!m) return null
  const year = Number(m[1])
  const week = Number(m[2])
  const jan4 = new Date(Date.UTC(year, 0, 4))
  const dow = jan4.getUTCDay() || 7 // lunes = 1
  const monday = new Date(jan4)
  monday.setUTCDate(jan4.getUTCDate() - dow + 1 + (week - 1) * 7)
  const sunday = new Date(monday)
  sunday.setUTCDate(monday.getUTCDate() + 6)
  return [monday, sunday]
}

/** Página `/reports` - Informe de disponibilidad (reports.md, rediseño #973). */
export default function Reports() {
  const { t, i18n } = useTranslation()
  const reduce = useReducedMotion()
  const { isDemo } = useNetPulse()
  // #988: la UI muestra el nombre dado por el usuario, con el slug entre
  // paréntesis cuando difieren (patrón LogLabel #953); nunca el slug a secas.
  const routerLabel = (id: string): string => {
    const n = routerName(id)
    return n !== id ? `${n} (${id})` : id
  }
  const [range, setRange] = useState<Range>('week')
  const [n, setN] = useState<number>(DEFAULT_N.week)
  const [items, setItems] = useState<AvailabilityEntry[]>([])
  // #1029: rango de los items cargados. Al cambiar de pestaña (p.ej. week ->
  // day) el render inmediato aún ve los items VIEJOS con el rango NUEVO, y
  // bucketLabel formateaba buckets semanales ("2026-W40") como si fueran
  // días o meses: Invalid Date -> RangeError -> pantalla en blanco.
  const [itemsRange, setItemsRange] = useState<Range | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [noApi, setNoApi] = useState(false)
  const [spin, setSpin] = useState(false)

  // AbortController del fetch actual (#221): un cambio rápido de rango/n o un
  // Refresh doble aborta la carga anterior en vuelo y descarta la respuesta
  // vieja. En unmount se aborta.
  const loadAc = useRef<AbortController | null>(null)
  // Timer del spin del botón Refresh (#227): limpiado en unmount.
  const spinTimer = useRef<number | null>(null)

  async function load(r: Range, count: number) {
    loadAc.current?.abort()
    const ac = new AbortController()
    loadAc.current = ac
    setLoading(true)
    setError(false)
    setNoApi(false)
    const result = await fetchJson<{ items: AvailabilityEntry[] }>(`/api/reports/availability?range=${r}&n=${count}`, { signal: ac.signal })
    if (ac.signal.aborted) return
    if (result.ok) {
      setItems(result.data.items)
      setItemsRange(r)
    } else if (result.kind === 'no-api' && isDemo) {
      setNoApi(true)
      setItems([])
      setItemsRange(r)
    } else {
      setError(true)
      setItems([])
      setItemsRange(r)
    }
    setLoading(false)
  }

  useEffect(() => () => {
    loadAc.current?.abort()
    if (spinTimer.current !== null) window.clearTimeout(spinTimer.current)
  }, [])

  useEffect(() => {
    void load(range, n)
  }, [range, n])

  function changeRange(r: Range) {
    if (r === range) return
    setRange(r)
    setN(DEFAULT_N[r])
  }

  // -- pestañas WAI-ARIA (issue #229): roving tabindex + flechas + Home/End
  const tabRefs = useRef<(HTMLButtonElement | null)[]>([])

  const onTablistKeyDown = useCallback(
    (e: ReactKeyboardEvent<HTMLDivElement>) => {
      const idx = RANGE_TABS.indexOf(range)
      let next: number
      if (e.key === 'ArrowRight') next = (idx + 1) % RANGE_TABS.length
      else if (e.key === 'ArrowLeft') next = (idx - 1 + RANGE_TABS.length) % RANGE_TABS.length
      else if (e.key === 'Home') next = 0
      else if (e.key === 'End') next = RANGE_TABS.length - 1
      else return
      e.preventDefault()
      const target = RANGE_TABS[next]!
      setRange(target)
      setN(DEFAULT_N[target])
      tabRefs.current[next]?.focus()
    },
    [range],
  )

  // Agregado por router sobre TODA la ventana seleccionada (no por bucket):
  // un solo % y un solo tiempo sin datos por router, ponderado por la
  // duración medible de cada bucket. Si el router tiene datos de menos de la
  // mitad de la ventana, no se resume con un número (#987).
  // Los agregados solo se calculan con items DEL rango activo (#1029): los
  // items stale de la pestaña anterior no se renderizan ni se agregan.
  const viewItems = itemsRange === range ? items : []
  const routers: RouterSummary[] = [...new Set(viewItems.map((i) => i.routerId))]
    .sort()
    .map((id) => {
      const rows = viewItems.filter((i) => i.routerId === id)
      const totalMin = rows.reduce((a, r) => a + bucketMinutes(r), 0)
      const upMin = rows.reduce((a, r) => a + r.upMin, 0)
      const days = rows.reduce((a, r) => a + r.days, 0)
      const pct = totalMin > 0 ? Math.min(100, (upMin / totalMin) * 100) : 100
      return {
        id,
        pct,
        downMin: Math.max(0, totalMin - upMin),
        status: statusOf(pct),
        days,
        sufficient: days >= Math.max(2, expectedDays(range, n) * MIN_COVERAGE),
        buckets: [...rows].sort((a, b) => a.bucket.localeCompare(b.bucket)),
      }
    })

  // Los agregados de flota solo cuentan routers con cobertura suficiente:
  // un router nuevo o sin datos no arrastra la media ni infla los huecos.
  const rated = routers.filter((r) => r.sufficient)
  const fleetPct = rated.length ? rated.reduce((a, r) => a + r.pct, 0) / rated.length : null
  const worst = rated.length ? rated.reduce((a, r) => (r.pct < a.pct ? r : a)) : null
  const totalDown = rated.reduce((a, r) => a + r.downMin, 0)

  const windowText = t(range === 'day' ? 'reports.windowDay' : range === 'week' ? 'reports.windowWeek' : 'reports.windowMonth', { n })

  // -- Barra de disponibilidad estilo Uptime Kuma (#993) -------------------
  // Un segmento por bucket (día/semana/mes según la pestaña, la granularidad
  // real del endpoint). El tooltip muestra la fecha del segmento; el día en
  // curso, único bucket con hora real, muestra además la hora de corte.
  const dayFmt = new Intl.DateTimeFormat(i18n.language, { day: 'numeric', month: 'short', timeZone: 'UTC' })
  const dayYearFmt = new Intl.DateTimeFormat(i18n.language, { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' })
  const monthFmt = new Intl.DateTimeFormat(i18n.language, { month: 'long', year: 'numeric', timeZone: 'UTC' })
  const todayKey = new Date().toISOString().slice(0, 10)
  const nowTime = new Date().toLocaleTimeString(i18n.language, { hour: '2-digit', minute: '2-digit' })

  /** Formatea solo fechas finitas: un bucket inesperado cae al texto crudo
   *  en vez de tumbar la página con un RangeError (#1029). */
  const fmtDate = (d: Date, fmt: Intl.DateTimeFormat, fallback: string): string =>
    Number.isFinite(d.getTime()) ? fmt.format(d) : fallback

  function bucketLabel(b: AvailabilityEntry): string {
    if (range === 'day') {
      if (b.bucket === todayKey) return t('reports.tipToday', { time: nowTime })
      return fmtDate(new Date(`${b.bucket}T00:00:00Z`), dayYearFmt, b.bucket)
    }
    if (range === 'week') {
      const span = isoWeekSpan(b.bucket)
      if (!span) return b.bucket
      return `${fmtDate(span[0], dayFmt, b.bucket)} - ${fmtDate(span[1], dayYearFmt, b.bucket)}`
    }
    return fmtDate(new Date(`${b.bucket}-01T00:00:00Z`), monthFmt, b.bucket)
  }

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
          <span className="text-text-secondary">{t('nav.reports')}</span>
        </motion.nav>
        <div className="mt-1.5 flex flex-wrap items-end justify-between gap-x-4 gap-y-3">
          <motion.div
            initial={initial}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, ease: 'easeOut', delay: 0.06 }}
          >
            <h1 className="font-display text-h1 text-text-primary">{t('nav.reports')}</h1>
            <p className="text-caption text-text-muted">{t('reports.explain', { window: windowText })}</p>
          </motion.div>
          <motion.div
            initial={initial}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, ease: 'easeOut', delay: 0.12 }}
            className="flex items-center gap-3"
          >
            <div className="inline-flex items-center gap-1 rounded-lg border border-border bg-surface p-1" role="group" aria-label={t('reports.rangeLabel')}>
              {N_OPTIONS[range].map((opt) => (
                <button
                  key={opt}
                  onClick={() => setN(opt)}
                  className={cn(
                    'rounded-md px-2.5 py-1 text-caption font-medium transition-colors',
                    n === opt ? 'bg-accent/15 text-accent' : 'text-text-muted hover:text-text-secondary',
                  )}
                >
                  {opt}{SUFFIX[range]}
                </button>
              ))}
            </div>
            <button
              onClick={() => {
                void load(range, n)
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
            </button>
          </motion.div>
        </div>
      </header>

      {/* ② Pestañas de rango */}
      <div
        role="tablist"
        aria-label={t('reports.rangeLabel')}
        onKeyDown={onTablistKeyDown}
        className="inline-flex items-center gap-1 rounded-lg border border-border bg-surface p-1"
      >
        {RANGE_TABS.map((r, i) => (
          <button
            key={r}
            ref={(el) => {
              tabRefs.current[i] = el
            }}
            id={`tab-${r}`}
            role="tab"
            aria-selected={range === r}
            aria-controls={`panel-${r}`}
            tabIndex={range === r ? 0 : -1}
            onClick={() => changeRange(r)}
            className={cn(
              'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
              range === r ? 'bg-accent/15 text-accent' : 'text-text-muted hover:text-text-secondary',
            )}
          >
            {r === 'day' ? t('reports.tabDay') : r === 'week' ? t('reports.tabWeek') : t('reports.tabMonth')}
          </button>
        ))}
      </div>

      {/* ③ Contenido */}
      <div role="tabpanel" id={`panel-${range}`} aria-labelledby={`tab-${range}`} tabIndex={0}>
      {loading && items.length === 0 && (
        <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
          {t('reports.loading')}
        </div>
      )}
      {noApi && (
        <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
          {t('reports.noApi')}
        </div>
      )}
      {error && items.length === 0 && (
        <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
          {t('reports.error')}
        </div>
      )}
      {!loading && !error && !noApi && items.length === 0 && (
        <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
          {t('reports.empty')}
        </div>
      )}

      {items.length > 0 && (
        <>
          {/* ④ Resumen de la flota: media, peor router y tiempo sin datos total */}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <div className="rounded-2xl border border-border bg-surface p-4">
              <div className="flex items-center gap-2 text-caption text-text-muted">
                <Activity className="h-4 w-4 text-accent" strokeWidth={1.75} />
                {t('reports.fleetAvg')}
              </div>
              <p className={cn('mt-1 font-display text-h2', fleetPct !== null ? STATUS_TEXT[statusOf(fleetPct)] : 'text-text-muted')}>
                {fleetPct === null ? t('reports.insufficient') : fleetPct >= 99.9 ? '100%' : `${fleetPct.toFixed(1)}%`}
              </p>
            </div>
            <div className="rounded-2xl border border-border bg-surface p-4">
              <div className="flex items-center gap-2 text-caption text-text-muted">
                <AlertTriangle className={cn('h-4 w-4', worst && worst.status !== 'ok' ? STATUS_TEXT[worst.status] : 'text-text-muted')} strokeWidth={1.75} />
                {t('reports.worstRouter')}
              </div>
              {worst ? (
                <p className="mt-1 font-display text-h2 text-text-primary">
                  {routerLabel(worst.id)}
                  <span className={cn('ml-2 text-sm font-normal', STATUS_TEXT[worst.status])}>
                    {worst.pct >= 99.9 ? '100%' : `${worst.pct.toFixed(1)}%`}
                  </span>
                </p>
              ) : (
                <p className="mt-1 font-display text-h2 text-text-muted">{t('reports.insufficient')}</p>
              )}
            </div>
            <div className="rounded-2xl border border-border bg-surface p-4">
              <div className="flex items-center gap-2 text-caption text-text-muted">
                <Clock className="h-4 w-4 text-accent" strokeWidth={1.75} />
                {t('reports.totalDown')}
              </div>
              <p className={cn('mt-1 font-display text-h2', rated.length === 0 ? 'text-text-muted' : totalDown >= 1 ? STATUS_TEXT[statusOf(fleetPct ?? 100)] : 'text-ok')}>
                {rated.length === 0 ? t('reports.insufficient') : totalDown >= 1 ? fmtDuration(totalDown) : t('reports.downNone')}
              </p>
            </div>
          </div>

          {/* ⑤ Lista por router: un % y un tiempo sin datos por router */}
          <section className="rounded-2xl border border-border bg-surface p-5 md:p-6">
            <div className="mb-4 flex items-center gap-2">
              <CalendarDays className="h-4 w-4 text-accent" strokeWidth={1.75} />
              <h2 className="font-display text-h2 text-text-primary">{t('reports.availability')}</h2>
              <a
                href={`/api/reports/availability?range=${range}&n=${n}&format=csv`}
                download
                className="ml-auto inline-flex items-center gap-1 rounded-md border border-border px-2 py-1 text-[11px] font-medium text-text-secondary transition-colors hover:bg-hover hover:text-text-primary"
                title={t('reports.downloadCsv')}
              >
                <Download className="h-3 w-3" strokeWidth={2} />
                CSV
              </a>
            </div>
            <ul className="divide-y divide-border/60">
              {routers.map((r) => (
                <li key={r.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 py-3">
                  {/* Nombre + estado */}
                  <div className="flex min-w-40 items-center gap-2">
                    <span className={cn('h-2 w-2 shrink-0 rounded-full', r.sufficient ? STATUS_DOT[r.status] : 'bg-border-strong')} aria-hidden="true" />
                    <span className="font-medium text-text-primary">{routerLabel(r.id)}</span>
                    {r.sufficient ? (
                      <span className={cn('text-caption', STATUS_TEXT[r.status])}>{t(`reports.status_${r.status}`)}</span>
                    ) : (
                      <span className="text-caption text-text-muted">{t('reports.insufficient')}</span>
                    )}
                  </div>
                  {/* Barra de disponibilidad (#993): segmentos rectangulares
                      contiguos, uno por bucket, con tooltip de fecha y tiempo
                      sin datos. Atenuada si la cobertura es insuficiente (#987) */}
                  <div className={cn('flex min-w-24 flex-1 items-center gap-0.5', !r.sufficient && 'opacity-60')}>
                    {r.buckets.map((b) => {
                      const down = bucketDownMin(b)
                      const st = statusOf(b.upPct)
                      const label = bucketLabel(b)
                      const pctText = b.upPct >= 99.9 ? '100%' : `${b.upPct.toFixed(1)}%`
                      const downText = down >= 1 ? t('reports.downTime', { duration: fmtDuration(down) }) : t('reports.downNone')
                      return (
                        <Tooltip key={b.bucket}>
                          <TooltipTrigger
                            aria-label={`${label}: ${pctText}, ${downText}`}
                            className={cn(
                              'h-4 min-w-1 flex-1 rounded-[2px] transition-transform duration-150 hover:scale-y-125',
                              STATUS_DOT[st],
                            )}
                          />
                          <TooltipContent
                            side="top"
                            sideOffset={6}
                            className="border border-border-strong bg-elevated text-text-primary shadow-xl"
                          >
                            <p className="font-medium">{label}</p>
                            <p className={cn('text-caption', STATUS_TEXT[st])}>
                              {pctText} · {t(`reports.status_${st}`)}
                            </p>
                            <p className="text-caption text-text-secondary">{downText}</p>
                          </TooltipContent>
                        </Tooltip>
                      )
                    })}
                  </div>
                  {/* % agregado + tiempo sin datos en unidades humanas; con
                      cobertura insuficiente no se inventa un número (#987) */}
                  <div className="ml-auto text-right">
                    {r.sufficient ? (
                      <>
                        <span className={cn('font-mono text-mono-sm font-semibold', STATUS_TEXT[r.status])}>
                          {r.pct >= 99.9 ? '100%' : `${r.pct.toFixed(1)}%`}
                        </span>
                        <p className="text-caption text-text-muted">
                          {r.downMin >= 1 ? t('reports.downTime', { duration: fmtDuration(r.downMin) }) : t('reports.downNone')}
                        </p>
                      </>
                    ) : (
                      <p className="text-caption text-text-muted">
                        {t('reports.coverage', { days: r.days, total: expectedDays(range, n) })}
                      </p>
                    )}
                  </div>
                </li>
              ))}
            </ul>
            <p className="mt-4 text-caption text-text-muted">{t('reports.footnote')}</p>
          </section>
        </>
      )}
      </div>
    </div>
  )
}
