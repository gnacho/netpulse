/**
 * NetPulse — "Enlaces entre equipos" (topology.md §④).
 * Tabla en desktop, lista apilada en móvil. Hover de fila ilumina el
 * enlace correspondiente en el mapa (estado compartido vía props).
 */
import { memo } from 'react'
import { motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { StatusPill } from '@/components/StatusPill'
import { cn } from '@/lib/utils'
import type { BackhaulRow, TopologyModel } from './model'

// ---------------------------------------------------------------------------
// Sparkline con animación de dibujo (pathLength)
// ---------------------------------------------------------------------------

const DrawSparkline = memo(function DrawSparkline({
  data,
  color,
  delay,
  width = 96,
  height = 26,
}: {
  data: number[]
  color: string
  delay: number
  width?: number
  height?: number
}) {
  const reduce = useReducedMotion()
  if (data.length < 2) return null
  const min = Math.min(...data)
  const max = Math.max(...data)
  const span = max - min || 1
  const pad = 2
  const stepX = (width - pad * 2) / (data.length - 1)
  const pts = data
    .map((v, i) => {
      const x = pad + i * stepX
      const y = pad + (1 - (v - min) / span) * (height - pad * 2)
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
  return (
    <svg viewBox={`0 0 ${width} ${height}`} width={width} height={height} className="overflow-visible" aria-hidden="true">
      <motion.polyline
        points={pts}
        fill="none"
        stroke={color}
        strokeWidth={2}
        strokeLinecap="round"
        strokeLinejoin="round"
        initial={reduce ? { pathLength: 1 } : { pathLength: 0 }}
        animate={{ pathLength: 1 }}
        transition={reduce ? { duration: 0 } : { delay, duration: 0.6, ease: 'easeOut' }}
      />
    </svg>
  )
})

// ---------------------------------------------------------------------------
// Fila (compartida tabla/lista)
// ---------------------------------------------------------------------------

function rowMotion(reduce: boolean, i: number) {
  return reduce
    ? {}
    : {
        initial: { opacity: 0, y: 10 },
        animate: { opacity: 1, y: 0 },
        transition: { delay: 0.2 + i * 0.06, duration: 0.3, ease: 'easeOut' as const },
      }
}

function LinkNames({ row }: { row: BackhaulRow }) {
  const { t } = useTranslation()
  return (
    <span className="flex items-center gap-1.5">
      <span className="font-medium text-text-primary">{row.a}</span>
      <span className={cn('text-text-muted', row.kind === 'wg' && 'text-tunnel')} aria-hidden>
        {row.kind === 'wg' ? '⇢' : '↔'}
      </span>
      <span className="font-medium text-text-primary">{row.bKey ? t(row.bKey, row.bVars) : row.b}</span>
    </span>
  )
}

function LinkNote({ note }: { note: string }) {
  const { t } = useTranslation()
  return <>{t(note)}</>
}

interface LinksTableProps {
  model: TopologyModel
  hoverLink: string | null
  onHoverLink: (id: string | null) => void
}

export function LinksTable({ model, hoverLink, onHoverLink }: LinksTableProps) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const { backhauls } = model
  const rowProps = (row: BackhaulRow) => ({
    onPointerEnter: (e: React.PointerEvent) => {
      if (e.pointerType === 'mouse') onHoverLink(row.id)
    },
    onPointerLeave: (e: React.PointerEvent) => {
      if (e.pointerType === 'mouse') onHoverLink(null)
    },
    onFocus: () => onHoverLink(row.id),
    onBlur: () => onHoverLink(null),
  })

  return (
    <section className="rounded-2xl border border-border bg-surface p-5">
      <div className="mb-4 flex items-center justify-between gap-3">
        <h2 className="font-display text-h2 text-text-primary">{t('topology.links.title')}</h2>
        <StatusPill tone="ok" label={t('topbar.live')} pulse />
      </div>

      {/* Tabla (md+) */}
      <div className="hidden overflow-x-auto md:block">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-label uppercase text-text-muted">
              <th scope="col" className="pb-2 pr-4 font-medium">{t('topology.links.colLink')}</th>
              <th scope="col" className="pb-2 pr-4 font-medium">{t('devices.colType')}</th>
              <th scope="col" className="pb-2 pr-4 font-medium">{t('topology.links.colSpeed')}</th>
              <th scope="col" className="pb-2 pr-4 font-medium">{t('devices.colSignal')}</th>
              <th scope="col" className="pb-2 pr-4 font-medium">{t('routers.colStatus')}</th>
              <th scope="col" className="relative pb-2 font-medium">
                <span className="sr-only">{t('topology.links.throughput')}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {backhauls.map((row, i) => (
              <motion.tr
                key={row.id}
                {...rowMotion(reduce ?? false, i)}
                {...rowProps(row)}
                tabIndex={0}
                className={cn(
                  'cursor-default border-b border-border/60 transition-colors duration-150 last:border-0',
                  row.tone === 'warn' && 'bg-warn/5',
                  hoverLink === row.id && 'bg-hover',
                )}
              >
                <td className="py-3 pr-4 whitespace-nowrap">
                  <LinkNames row={row} />
                  {row.note && <div className="mt-0.5 pl-5 text-caption text-warn"><LinkNote note={row.note} /></div>}
                </td>
                <td className="py-3 pr-4 whitespace-nowrap text-text-secondary">{t(row.type)}</td>
                <td className="py-3 pr-4 whitespace-nowrap font-mono text-mono-sm text-text-primary">{row.speed}</td>
                <td className="py-3 pr-4 whitespace-nowrap font-mono text-mono-sm text-text-secondary">{row.signal}</td>
                <td className="py-3 pr-4 whitespace-nowrap">
                  <StatusPill tone={row.tone} label={t(row.statusLabel)} pulse={row.tone !== 'ok'} />
                </td>
                <td className="py-3">
                  <DrawSparkline data={row.spark} color={row.sparkColor} delay={0.3 + i * 0.08} />
                </td>
              </motion.tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Lista (móvil) */}
      <ul className="space-y-2 md:hidden">
        {backhauls.map((row, i) => (
          <motion.li
            key={row.id}
            {...rowMotion(reduce ?? false, i)}
            {...rowProps(row)}
            className={cn(
              'rounded-xl border border-border/60 bg-elevated/50 p-3 transition-colors duration-150',
              row.tone === 'warn' && 'border-warn/30 bg-warn/5',
              hoverLink === row.id && 'bg-hover',
            )}
          >
            <div className="flex items-center justify-between gap-2">
              <LinkNames row={row} />
              <StatusPill tone={row.tone} label={t(row.statusLabel)} pulse={row.tone !== 'ok'} />
            </div>
            <div className="mt-2 flex items-end justify-between gap-3">
              <div className="space-y-0.5 text-caption text-text-secondary">
                <div>
                  {t(row.type)} · <span className="font-mono text-text-primary">{row.speed}</span> ·{' '}
                  <span className="font-mono">{row.signal}</span>
                </div>
                {row.note && <div className="text-warn"><LinkNote note={row.note} /></div>}
              </div>
              <DrawSparkline data={row.spark} color={row.sparkColor} delay={0.3 + i * 0.08} width={80} height={22} />
            </div>
          </motion.li>
        ))}
      </ul>
    </section>
  )
}
