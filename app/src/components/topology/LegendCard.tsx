/**
 * NetPulse — Leyenda / resumen de la topología (topology.md §③).
 * `LegendCard` para desktop, `LegendSheet` (bottom sheet arrastrable) en móvil.
 */
import { useRef, useState } from 'react'
import { motion, useReducedMotion } from 'framer-motion'
import { ChevronUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNetPulse } from '@/data/DataProvider'
import { cn } from '@/lib/utils'
import type { TopologyModel } from './model'
import { COLOR } from './model'

// ---------------------------------------------------------------------------
// Contenido compartido
// ---------------------------------------------------------------------------

const BAND_ROWS: { color: string; label?: string; labelKey?: string }[] = [
  { color: COLOR.accent, label: '5 GHz' },
  { color: COLOR.info, label: '2.4 GHz' },
  { color: COLOR.ok, labelKey: 'topology.legend.wired' },
  { color: COLOR.warn, labelKey: 'topology.weakSignal' },
]

const STATUS_ROWS: { color: string; labelKey: string }[] = [
  { color: COLOR.ok, labelKey: 'topology.legend.healthy' },
  { color: COLOR.warn, labelKey: 'common.status.warn' },
  { color: COLOR.danger, labelKey: 'common.status.offline' },
]

function RingSample({ color }: { color: string }) {
  return (
    <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true">
      <circle cx="7" cy="7" r="5" fill="none" stroke={color} strokeWidth="2.5" strokeLinecap="round" strokeDasharray="22 10" transform="rotate(-90 7 7)" />
    </svg>
  )
}

/** Muestra de línea dashed WireGuard, auto-animada en loop */
function WgSample() {
  const reduce = useReducedMotion()
  return (
    <svg width="26" height="8" viewBox="0 0 26 8" aria-hidden="true">
      <motion.line
        x1="1"
        y1="4"
        x2="25"
        y2="4"
        stroke={COLOR.tunnel}
        strokeWidth="2"
        strokeLinecap="round"
        strokeDasharray="5 4"
        animate={reduce ? undefined : { strokeDashoffset: [0, -18] }}
        transition={{ duration: 1.2, repeat: Infinity, ease: 'linear' }}
      />
    </svg>
  )
}

export function LegendContent({ model, compact = false }: { model: TopologyModel; compact?: boolean }) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const { deviceTotals } = useNetPulse()
  const { activeLinkCount, activePeerCount } = model
  const row = (i: number) =>
    reduce
      ? {}
      : {
          initial: { opacity: 0, y: 8 },
          animate: { opacity: 1, y: 0 },
          transition: { delay: 0.15 + i * 0.05, duration: 0.3, ease: 'easeOut' as const },
        }
  let i = 0
  return (
    <div className={cn('space-y-4', compact && 'space-y-3')}>
      <div>
        <div className="mb-2 text-label uppercase text-text-muted">{t('topology.legend.bandConnection')}</div>
        <ul className="grid grid-cols-2 gap-x-4 gap-y-2">
          {BAND_ROWS.map((r) => (
            <motion.li key={r.label ?? r.labelKey} className="flex items-center gap-2 text-sm text-text-secondary" {...row(i++)}>
              <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: r.color }} />
              {r.label ?? t(r.labelKey!)}
            </motion.li>
          ))}
          <motion.li className="col-span-2 flex items-center gap-2 text-sm text-text-secondary" {...row(i++)}>
            <WgSample />
            {t('topology.legend.wgTunnel')}
          </motion.li>
        </ul>
      </div>
      <div>
        <div className="mb-2 text-label uppercase text-text-muted">{t('routers.colStatus')}</div>
        <ul className="grid grid-cols-3 gap-x-4 gap-y-2">
          {STATUS_ROWS.map((r) => (
            <motion.li key={r.labelKey} className="flex items-center gap-2 text-sm text-text-secondary" {...row(i++)}>
              <RingSample color={r.color} />
              {t(r.labelKey)}
            </motion.li>
          ))}
        </ul>
      </div>
      <motion.p className="border-t border-border pt-3 text-caption text-text-muted" {...row(i++)}>
        {t('topology.legend.summary', { online: deviceTotals.online, peers: activePeerCount, links: activeLinkCount })}
      </motion.p>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Tarjeta desktop
// ---------------------------------------------------------------------------

export function LegendCard({ model }: { model: TopologyModel }) {
  const { t } = useTranslation()
  return (
    <section className="rounded-2xl border border-border bg-surface p-5">
      <h2 className="mb-4 font-display text-h2 text-text-primary">{t('topology.legend.title')}</h2>
      <LegendContent model={model} />
    </section>
  )
}

// ---------------------------------------------------------------------------
// Bottom sheet móvil (peek 64px, arrastrable) — vive dentro del mapa
// ---------------------------------------------------------------------------

const SHEET_H = 300
const PEEK = 64
const OFFSET = SHEET_H - PEEK

export function LegendSheet({ model }: { model: TopologyModel }) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const [open, setOpen] = useState(false)
  const dragged = useRef(false)
  return (
    <motion.div
      className="absolute inset-x-0 bottom-0 z-10 rounded-t-2xl border-t border-border-strong bg-elevated/95 backdrop-blur-md lg:hidden"
      style={{ height: SHEET_H }}
      onPointerDown={(e) => e.stopPropagation()}
      initial={false}
      animate={{ y: open ? 0 : OFFSET }}
      transition={reduce ? { duration: 0 } : { type: 'spring', stiffness: 320, damping: 32 }}
      drag={reduce ? false : 'y'}
      dragConstraints={{ top: 0, bottom: OFFSET }}
      dragElastic={0.04}
      dragMomentum={false}
      onDragStart={() => {
        dragged.current = true
      }}
      onDragEnd={(_, info) => {
        const shouldOpen = info.velocity.y < -300 || info.offset.y < -OFFSET / 2
        const shouldClose = info.velocity.y > 300 || info.offset.y > OFFSET / 2
        if (shouldOpen && !shouldClose) setOpen(true)
        else setOpen(false)
        window.setTimeout(() => {
          dragged.current = false
        }, 50)
      }}
    >
      <button
        type="button"
        className="flex h-16 w-full flex-col items-center justify-center gap-1"
        onClick={() => {
          if (dragged.current) return
          setOpen((o) => !o)
        }}
        aria-expanded={open}
        aria-label={open ? t('topology.legend.hide') : t('topology.legend.show')}
      >
        <span className="h-1 w-10 rounded-full bg-border-strong" aria-hidden />
        <span className="flex items-center gap-1.5 text-sm font-semibold text-text-primary">
          {t('topology.legend.title')}
          <ChevronUp
            className={cn('h-4 w-4 text-text-muted transition-transform duration-200', open && 'rotate-180')}
            strokeWidth={1.75}
          />
        </span>
      </button>
      <div className="px-5 pb-5">
        <LegendContent model={model} compact />
      </div>
    </motion.div>
  )
}
