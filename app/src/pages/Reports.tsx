import { useCallback, useEffect, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { motion, useReducedMotion } from 'framer-motion'
import { CalendarDays, Download, RefreshCw } from 'lucide-react'
import { cn, fetchJson } from '@/lib/utils'
import { useNetPulse } from '@/data/DataProvider'

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

/** Formatea bytes a unidad legible (mismo estilo que fmtEs de tráfico). */
function fmtBytes(b: number): string {
  const gb = b / 1e9
  if (gb >= 1) return `${gb.toFixed(1)} GB`
  const mb = b / 1e6
  if (mb >= 1) return `${mb.toFixed(0)} MB`
  return `${(b / 1e3).toFixed(0)} KB`
}

/** Barra de disponibilidad con color semántico (design.md §10.11). */
function AvailabilityBar({ pct }: { pct: number }) {
  const color = pct >= 99 ? 'bg-ok' : pct >= 95 ? 'bg-warn' : 'bg-danger'
  return (
    <div className="flex items-center gap-2">
      <div className="h-1.5 w-20 overflow-hidden rounded-full bg-elevated" role="presentation">
        <motion.div
          initial={{ width: 0 }}
          animate={{ width: `${Math.min(100, Math.max(0, pct))}%` }}
          transition={{ duration: 0.4, ease: 'easeOut' }}
          className={cn('h-full rounded-full', color)}
        />
      </div>
      <span className="font-mono text-mono-sm text-text-secondary">{pct >= 99.9 ? '100%' : `${pct.toFixed(1)}%`}</span>
    </div>
  )
}

/** Página `/reports` — Informe de disponibilidad (reports.md). */
export default function Reports() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const { isDemo } = useNetPulse()
  const [range, setRange] = useState<Range>('week')
  const [n, setN] = useState<number>(DEFAULT_N.week)
  const [items, setItems] = useState<AvailabilityEntry[]>([])
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
    } else if (result.kind === 'no-api' && isDemo) {
      setNoApi(true)
      setItems([])
    } else {
      setError(true)
      setItems([])
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
      let next = idx
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

  // Agrupa por router y coloca los buckets en columnas (fila = router).
  const routerIds = [...new Set(items.map((i) => i.routerId))].sort()
  const buckets = [...new Set(items.map((i) => i.bucket))].sort().reverse()
  const byRouter = (id: string) => items.filter((i) => i.routerId === id)

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
            <p className="text-caption text-text-muted">{t('reports.subtitle')}</p>
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
        <section className="rounded-2xl border border-border bg-surface p-5 md:p-6">
          <div className="mb-4 flex items-center gap-2">
            <CalendarDays className="h-4 w-4 text-accent" strokeWidth={1.75} />
            <h2 className="font-display text-h2 text-text-primary">{t('reports.availability')}</h2>
            {items.length > 0 && (
              <a
                href={`/api/reports/availability?range=${range}&n=${n}&format=csv`}
                download
                className="ml-auto inline-flex items-center gap-1 rounded-md border border-border px-2 py-1 text-[11px] font-medium text-text-secondary transition-colors hover:bg-hover hover:text-text-primary"
                title={t('reports.downloadCsv')}
              >
                <Download className="h-3 w-3" strokeWidth={2} />
                CSV
              </a>
            )}
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-border">
                  <th className="pb-2.5 pr-3 text-label font-medium uppercase text-text-muted">{t('reports.colRouter')}</th>
                  {buckets.map((b) => (
                    <th key={b} className="pb-2.5 pr-3 font-mono text-caption font-medium normal-case text-text-muted">{b}</th>
                  ))}
                  <th className="pb-2.5 text-label font-medium uppercase text-text-muted">{t('reports.colAvg')}</th>
                </tr>
              </thead>
              <tbody>
                {routerIds.map((id) => {
                  const rows = byRouter(id)
                  const avg = rows.length ? rows.reduce((a, r) => a + r.upPct, 0) / rows.length : 0
                  return (
                    <tr key={id} className="border-b border-border/60 last:border-0 hover:bg-hover">
                      <td className="py-3 pr-3 font-medium text-text-primary">{id}</td>
                      {buckets.map((b) => {
                        const e = rows.find((r) => r.bucket === b)
                        return (
                          <td key={b} className="py-3 pr-3">
                            {e ? <AvailabilityBar pct={e.upPct} /> : <span className="text-caption text-text-muted">—</span>}
                          </td>
                        )
                      })}
                      <td className="py-3">
                        <AvailabilityBar pct={avg} />
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>

          {/* ④ Detalle por router: últimos buckets con tráfico/latencia */}
          <div className="mt-6 grid grid-cols-1 gap-4 md:grid-cols-2">
            {routerIds.map((id) => {
              const rows = [...byRouter(id)].sort((a, b) => b.bucket.localeCompare(a.bucket))
              return (
                <div key={id} className="rounded-xl border border-border bg-elevated p-4">
                  <h3 className="mb-3 font-display text-h3 text-text-primary">{id}</h3>
                  <div className="space-y-2">
                    {rows.map((r) => (
                      <div key={r.bucket} className="flex items-center justify-between gap-2 text-caption">
                        <span className="font-mono text-text-muted">{r.bucket}</span>
                        <span className="flex items-center gap-3 font-mono text-mono-sm text-text-secondary">
                          <span title={t('reports.latency')}>
                            {r.latAvg !== null ? `${r.latAvg.toFixed(1)} ms` : '—'}
                          </span>
                          <span title={t('reports.traffic')}>
                            {fmtBytes(r.rxTotal + r.txTotal)}
                          </span>
                          <span title={t('reports.upMin')}>
                            {r.upMin}′
                          </span>
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )
            })}
          </div>

          <p className="mt-6 text-caption text-text-muted">{t('reports.footnote')}</p>
        </section>
      )}
      </div>
    </div>
  )
}
