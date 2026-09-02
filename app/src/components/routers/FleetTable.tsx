import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router'
import { ArrowUp, ArrowUpDown, ChevronDown, ChevronRight, Router as RouterIcon } from 'lucide-react'
import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import { fmtUptime, numLocale } from '@/i18n'
import type { Router } from '@/data/mock'
import { fmtEs } from '@/data/mock'
import { useNetPulse } from '@/data/DataProvider'
import { MetricBar } from '@/components/MetricBar'
import { StatusPill } from '@/components/StatusPill'
import { AgentBadge } from '@/components/routers/AgentBadge'
import { getRouterExtras, uptimeHours } from '@/components/routers/routerExtras'
import { cn } from '@/lib/utils'

const STATUS_ORDER: Record<Router['status'], number> = { online: 0, warn: 1, offline: 2 }

type SortKey = 'name' | 'status' | 'cpu' | 'ram' | 'temp' | 'clients' | 'traffic' | 'uptime'

interface ColumnDef {
  key: SortKey | null
  labelKey: string
  className?: string
}

const COLUMNS: ColumnDef[] = [
  { key: 'name', labelKey: 'routers.colDevice' },
  { key: 'status', labelKey: 'routers.colStatus' },
  { key: 'cpu', labelKey: 'routers.colCpu' },
  { key: 'ram', labelKey: 'routers.colRam' },
  { key: 'temp', labelKey: 'routers.colTemp' },
  { key: 'clients', labelKey: 'routers.colClients', className: 'text-right' },
  { key: 'traffic', labelKey: 'routers.colTraffic', className: 'text-right' },
  { key: 'uptime', labelKey: 'routers.colUptime' },
  { key: null, labelKey: '', className: 'w-8' },
]

function sortValue(r: Router, key: SortKey): string | number {
  switch (key) {
    case 'name':
      return r.name
    case 'status':
      return STATUS_ORDER[r.status]
    // #441: sin vitals → -1 para ordenarlos al final en asc.
    case 'cpu':
      return r.cpu ?? -1
    case 'ram':
      return r.ram ?? -1
    case 'temp':
      return r.temp ?? -1
    case 'clients':
      return r.clients
    case 'traffic':
      return getRouterExtras(r.id).trafficNow
    case 'uptime':
      return uptimeHours(r.uptime)
  }
}

function TempCell({ router }: { router: Router }) {
  const { t } = useTranslation()
  const hot = router.hotMetric === 'temp'
  if (router.temp === null) {
    return <span className="font-mono text-mono-sm text-text-faint">—</span>
  }
  return (
    <span
      className={cn(
        'inline-flex rounded-md px-1.5 py-0.5 font-mono text-mono-sm',
        hot ? 'bg-warn/10 text-warn' : 'text-text-primary',
      )}
      title={hot ? t('routers.tempThreshold') : undefined}
    >
      {router.temp} °C
    </span>
  )
}

/** Celda CPU/RAM con barra; sin vitals (#441) un guion discreto. */
function VitalCell({ value }: { value: number | null }) {
  if (value === null) {
    return <span className="font-mono text-mono-sm text-text-faint">—</span>
  }
  return (
    <div className="flex items-center gap-2">
      <span className="w-9 font-mono text-mono-sm text-text-primary">{value} %</span>
      <div className="w-[60px]"><MetricBar value={value} /></div>
    </div>
  )
}

/** ③ Tabla comparativa (routers.md §③) — ordenable, accordion en móvil. */
export function FleetTable({ refreshKey = 0 }: { refreshKey?: number }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const reduce = useReducedMotion()
  const { routers, agents } = useNetPulse()
  const [sortKey, setSortKey] = useState<SortKey>('name')
  const [asc, setAsc] = useState(true)
  const [openId, setOpenId] = useState<string | null>(null)
  const agentBySlug = useMemo(() => new Map(agents.map((a) => [a.slug, a])), [agents])

  const sorted = useMemo(() => {
    const arr = [...routers]
    arr.sort((a, b) => {
      const va = sortValue(a, sortKey)
      const vb = sortValue(b, sortKey)
      const cmp = typeof va === 'string' ? va.localeCompare(String(vb), numLocale()) : va - (vb as number)
      return asc ? cmp : -cmp
    })
    return arr
  }, [routers, sortKey, asc])

  function toggleSort(key: SortKey) {
    if (key === sortKey) setAsc((v) => !v)
    else {
      setSortKey(key)
      setAsc(true)
    }
  }

  return (
    <section className="rounded-2xl border border-border bg-surface p-5 md:p-6">
      <div className="mb-4">
        <h2 className="font-display text-h2 text-text-primary">{t('routers.comparison')}</h2>
        <p className="text-caption text-text-muted">{t('routers.comparisonSub')}</p>
      </div>

      {/* Desktop: tabla */}
      <div className="hidden overflow-x-auto md:block">
        <table className="w-full text-left text-sm">
          <thead>
            <tr className="border-b border-border">
              {COLUMNS.map((col, i) => (
                <th
                  key={i}
                  className={cn('pb-2.5 pr-3 text-label font-medium uppercase text-text-muted', col.className)}
                >
                  {col.key ? (
                    <button
                      onClick={() => toggleSort(col.key as SortKey)}
                      className={cn(
                        'inline-flex items-center gap-1 uppercase transition-colors hover:text-text-primary',
                        sortKey === col.key && 'text-accent',
                      )}
                      aria-label={t('routers.sortBy', { column: t(col.labelKey) })}
                    >
                      {t(col.labelKey)}
                      {sortKey === col.key ? (
                        <ArrowUp className={cn('h-3 w-3 transition-transform', !asc && 'rotate-180')} strokeWidth={1.75} />
                      ) : (
                        <ArrowUpDown className="h-3 w-3 opacity-50" strokeWidth={1.75} />
                      )}
                    </button>
                  ) : (
                    col.labelKey && t(col.labelKey)
                  )}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {sorted.map((r, i) => {
              const extras = getRouterExtras(r.id)
              return (
                <motion.tr
                  key={r.id}
                  layout={reduce ? false : 'position'}
                  initial={reduce ? false : { opacity: 0, y: 8 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ duration: 0.25, delay: i * 0.04, layout: { type: 'spring', stiffness: 300, damping: 30 } }}
                  onClick={() => navigate(`/routers/${r.id}`)}
                  className="cursor-pointer border-b border-border/60 transition-colors last:border-0 hover:bg-hover"
                >
                  <td className="py-3 pr-3">
                    <div className="flex items-center gap-2.5">
                      <span className={cn(
                        'flex h-8 w-8 items-center justify-center rounded-lg',
                        r.id === 'flint2' ? 'bg-gradient-to-br from-accent to-tunnel text-canvas' : 'bg-accent-soft text-accent',
                      )}>
                        <RouterIcon className="h-4 w-4" strokeWidth={1.75} />
                      </span>
                      <span>
                        <span className="flex items-center gap-2">
                          <span className="font-medium text-text-primary">{r.name}</span>
                          <AgentBadge agent={agentBySlug.get(r.id)} agentOnly={r.agentOnly} deviceType={r.type} />
                        </span>
                        <span className="block text-caption text-text-muted">{r.modelShort}</span>
                      </span>
                    </div>
                  </td>
                  <td className="py-3 pr-3">
                    <StatusPill
                      tone={r.status === 'online' ? 'ok' : r.status === 'warn' ? 'warn' : 'danger'}
                      label={t(`common.status.${r.status}`)}
                      pulse={r.status !== 'online'}
                    />
                  </td>
                  <td className="py-3 pr-3">
                    <div key={refreshKey}><VitalCell value={r.cpu} /></div>
                  </td>
                  <td className="py-3 pr-3">
                    <div key={refreshKey}><VitalCell value={r.ram} /></div>
                  </td>
                  <td className="py-3 pr-3"><TempCell router={r} /></td>
                  <td className="py-3 pr-3 text-right font-mono text-mono-sm text-text-primary">{r.clients}</td>
                  <td className="py-3 pr-3 text-right font-mono text-mono-sm text-accent">
                    ↓ {fmtEs(extras.trafficNow, 1)}
                  </td>
                  <td className="py-3 pr-3 font-mono text-mono-sm text-text-secondary">{fmtUptime(r.uptime)}</td>
                  <td className="py-3">
                    <ChevronRight className="h-4 w-4 text-text-muted" strokeWidth={1.75} />
                  </td>
                </motion.tr>
              )
            })}
          </tbody>
        </table>
      </div>

      {/* Móvil: accordions */}
      <div className="space-y-2 md:hidden">
        {sorted.map((r) => {
          const extras = getRouterExtras(r.id)
          const open = openId === r.id
          return (
            <div key={r.id} className="overflow-hidden rounded-xl border border-border bg-elevated/40">
              <button
                onClick={() => setOpenId(open ? null : r.id)}
                aria-expanded={open}
                className="flex w-full items-center justify-between gap-3 px-3.5 py-3 text-left"
              >
                <span className="flex min-w-0 items-center gap-2.5">
                  <span className={cn(
                    'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
                    r.id === 'flint2' ? 'bg-gradient-to-br from-accent to-tunnel text-canvas' : 'bg-accent-soft text-accent',
                  )}>
                    <RouterIcon className="h-4 w-4" strokeWidth={1.75} />
                  </span>
                  <span className="min-w-0">
                    <span className="block truncate font-medium text-text-primary">{r.name}</span>
                    <AgentBadge agent={agentBySlug.get(r.id)} agentOnly={r.agentOnly} deviceType={r.type} className="mt-1" />
                  </span>
                </span>
                <span className="flex shrink-0 items-center gap-2">
                  <StatusPill
                    tone={r.status === 'online' ? 'ok' : r.status === 'warn' ? 'warn' : 'danger'}
                    label={t(`common.status.${r.status}`)}
                    pulse={r.status !== 'online'}
                  />
                  <span className={cn('font-mono text-mono-sm', r.hotMetric === 'temp' ? 'text-warn' : 'text-text-secondary')}>
                    {r.temp === null ? '—' : `${r.temp} °C`}
                  </span>
                  <ChevronDown className={cn('h-4 w-4 text-text-muted transition-transform', open && 'rotate-180')} strokeWidth={1.75} />
                </span>
              </button>
              <AnimatePresence initial={false}>
                {open && (
                  <motion.div
                    initial={reduce ? false : { height: 0, opacity: 0 }}
                    animate={{ height: 'auto', opacity: 1 }}
                    exit={reduce ? { opacity: 0 } : { height: 0, opacity: 0 }}
                    transition={{ duration: 0.25, ease: 'easeOut' }}
                  >
                    <div className="grid grid-cols-2 gap-x-4 gap-y-2.5 border-t border-border px-3.5 py-3">
                      {[
                        ['CPU', r.cpu === null ? '—' : `${r.cpu} %`],
                        ['RAM', r.ram === null ? '—' : `${r.ram} %`],
                        [t('routers.colTemp'), r.temp === null ? '—' : `${r.temp} °C`],
                        [t('routers.colClients'), String(r.clients)],
                        [t('routers.colTraffic'), `↓ ${fmtEs(extras.trafficNow, 1)} Mbps`],
                        ['Uptime', fmtUptime(r.uptime)],
                      ].map(([k, v]) => (
                        <div key={k}>
                          <span className="text-[10px] font-medium uppercase tracking-[0.06em] text-text-muted">{k}</span>
                          <div className={cn('font-mono text-mono-sm', k === t('routers.colTemp') && r.hotMetric === 'temp' ? 'text-warn' : 'text-text-primary')}>
                            {v}
                          </div>
                        </div>
                      ))}
                      <Link
                        to={`/routers/${r.id}`}
                        className="col-span-2 mt-1 inline-flex items-center justify-center gap-1 rounded-lg border border-border px-3 py-2 text-caption font-semibold text-accent transition-colors hover:border-accent/40"
                      >
                        {t('common.viewDetailShort')}
                        <ChevronRight className="h-3.5 w-3.5" strokeWidth={1.75} />
                      </Link>
                    </div>
                  </motion.div>
                )}
              </AnimatePresence>
            </div>
          )
        })}
      </div>
    </section>
  )
}
