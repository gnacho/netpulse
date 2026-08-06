import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { motion, useReducedMotion } from 'framer-motion'
import { CalendarDays, RefreshCw } from 'lucide-react'
import { cn } from '@/lib/utils'

// ---------------------------------------------------------------------------
// Tipos del contrato GET /api/reports/weekly (server-go/internal/httpapi/reports.go)
// ---------------------------------------------------------------------------

interface WeeklyEntry {
  routerId: string
  week: string // "2026-W31" (semana ISO, lunes-domingo)
  days: number
  upMin: number // minutos de recolección en la semana
  upPct: number // % de disponibilidad sobre los días con datos
  latAvg: number | null
  rxTotal: number
  txTotal: number
  cpuAvg: number
  ramAvg: number
}

const WEEK_OPTIONS = [2, 4, 8, 12]

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

/** Página `/reports` — Informe semanal de disponibilidad (reports.md) */
export default function Reports() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const [weeks, setWeeks] = useState(4)
  const [items, setItems] = useState<WeeklyEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [spin, setSpin] = useState(false)

  async function load(n: number) {
    setLoading(true)
    setError(false)
    try {
      const res = await fetch(`/api/reports/weekly?weeks=${n}`)
      if (!res.ok) throw new Error(`status ${res.status}`)
      const env = (await res.json()) as { items: WeeklyEntry[] }
      setItems(env.items)
    } catch {
      setError(true)
      setItems([])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load(weeks)
  }, [weeks])

  // Agrupa por router y coloca las semanas en columnas (fila = router).
  const routerIds = [...new Set(items.map((i) => i.routerId))].sort()
  const weeksPresent = [...new Set(items.map((i) => i.week))].sort().reverse()
  const byRouter = (id: string) => items.filter((i) => i.routerId === id)

  return (
    <div className="space-y-4 md:space-y-5">
      {/* ① Page header */}
      <header>
        <motion.nav
          initial={reduce ? false : { opacity: 0, y: 12 }}
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
            initial={reduce ? false : { opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, ease: 'easeOut', delay: 0.06 }}
          >
            <h1 className="font-display text-h1 text-text-primary">{t('nav.reports')}</h1>
            <p className="text-caption text-text-muted">{t('reports.subtitle')}</p>
          </motion.div>
          <motion.div
            initial={reduce ? false : { opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, ease: 'easeOut', delay: 0.12 }}
            className="flex items-center gap-3"
          >
            <div className="inline-flex items-center gap-1 rounded-lg border border-border bg-surface p-1" role="group" aria-label={t('reports.weeksLabel')}>
              {WEEK_OPTIONS.map((n) => (
                <button
                  key={n}
                  onClick={() => setWeeks(n)}
                  className={cn(
                    'rounded-md px-2.5 py-1 text-caption font-medium transition-colors',
                    weeks === n ? 'bg-accent/15 text-accent' : 'text-text-muted hover:text-text-secondary',
                  )}
                >
                  {n}w
                </button>
              ))}
            </div>
            <button
              onClick={() => {
                void load(weeks)
                if (reduce) return
                setSpin(true)
                window.setTimeout(() => setSpin(false), 650)
              }}
              className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-surface px-3 text-sm font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
            >
              <RefreshCw className={cn('h-4 w-4 transition-transform duration-500', spin && 'rotate-[360deg]')} strokeWidth={1.75} />
              {t('common.refresh')}
            </button>
          </motion.div>
        </div>
      </header>

      {/* ② Contenido */}
      {loading && items.length === 0 && (
        <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
          {t('reports.loading')}
        </div>
      )}
      {error && items.length === 0 && (
        <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
          {t('reports.error')}
        </div>
      )}
      {!loading && !error && items.length === 0 && (
        <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
          {t('reports.empty')}
        </div>
      )}

      {items.length > 0 && (
        <section className="rounded-2xl border border-border bg-surface p-5 md:p-6">
          <div className="mb-4 flex items-center gap-2">
            <CalendarDays className="h-4 w-4 text-accent" strokeWidth={1.75} />
            <h2 className="font-display text-h2 text-text-primary">{t('reports.availability')}</h2>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-border">
                  <th className="pb-2.5 pr-3 text-label font-medium uppercase text-text-muted">{t('reports.colRouter')}</th>
                  {weeksPresent.map((w) => (
                    <th key={w} className="pb-2.5 pr-3 text-label font-medium uppercase text-text-muted">{w}</th>
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
                      {weeksPresent.map((w) => {
                        const e = rows.find((r) => r.week === w)
                        return (
                          <td key={w} className="py-3 pr-3">
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

          {/* ③ Detalle por router: últimas semanas con tráfico/latencia */}
          <div className="mt-6 grid grid-cols-1 gap-4 md:grid-cols-2">
            {routerIds.map((id) => {
              const rows = [...byRouter(id)].sort((a, b) => b.week.localeCompare(a.week))
              return (
                <div key={id} className="rounded-xl border border-border bg-elevated p-4">
                  <h3 className="mb-3 font-display text-h3 text-text-primary">{id}</h3>
                  <div className="space-y-2">
                    {rows.map((r) => (
                      <div key={r.week} className="flex items-center justify-between gap-2 text-caption">
                        <span className="font-mono text-text-muted">{r.week}</span>
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
  )
}
