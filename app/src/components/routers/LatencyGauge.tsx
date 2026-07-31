import { motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { CountUp } from '@/components/CountUp'

interface LatencyGaugeProps {
  /** Latencia actual en ms */
  valueMs: number
  /** Escala máxima del arco (ms) */
  maxMs?: number
  caption?: string
  size?: number
}

/** Gauge radial de latencia (router-detail.md §4b): arco 0–100 ms, emerald <20 ms. */
export function LatencyGauge({ valueMs, maxMs = 100, caption, size = 160 }: LatencyGaugeProps) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const stroke = 12
  const r = (size - stroke) / 2
  const c = size / 2
  const frac = Math.min(1, Math.max(0, valueMs / maxMs))
  const color = valueMs < 20 ? '#34D399' : valueMs < 50 ? '#FBBF24' : '#F87171'

  return (
    <div className="flex flex-col items-center">
      <div
        className="relative inline-flex items-center justify-center"
        style={{ width: size, height: size }}
        role="img"
        aria-label={t('routerDetail.latency.aria', { ms: valueMs, caption: caption ?? '' })}
      >
        <svg width={size} height={size} className="-rotate-90">
          <circle cx={c} cy={c} r={r} fill="none" stroke="rgb(var(--border))" strokeWidth={stroke} />
          <motion.circle
            cx={c}
            cy={c}
            r={r}
            fill="none"
            stroke={color}
            strokeWidth={stroke}
            strokeLinecap="round"
            pathLength={1}
            strokeDasharray="1 1"
            initial={reduce ? { pathLength: frac } : { pathLength: 0 }}
            animate={{ pathLength: frac }}
            transition={{ duration: 1, ease: 'easeOut' }}
          />
        </svg>
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <span className="font-mono text-3xl font-semibold text-text-primary">
            <CountUp value={valueMs} />
          </span>
          <span className="text-caption font-medium uppercase tracking-[0.06em] text-text-muted">ms</span>
        </div>
      </div>
      {caption && <span className="mt-1.5 text-caption text-text-muted">{caption}</span>}
    </div>
  )
}
