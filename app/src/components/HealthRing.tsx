import { memo, useId } from 'react'
import { motion, useReducedMotion } from 'framer-motion'
import i18n from '@/i18n'
import type { Status } from '@/data/mock'
import { cn } from '@/lib/utils'

export const STATUS_COLORS: Record<Status, string> = {
  online: '#34D399',
  warn: '#FBBF24',
  offline: '#F87171',
}

interface HealthRingProps {
  /** 0–100 */
  value: number
  size?: number
  stroke?: number
  /** 'health' = gradiente cyan→emerald; 'status' = color de estado sólido */
  variant?: 'health' | 'status'
  status?: Status
  /** Contenido central; por defecto la cifra */
  center?: React.ReactNode
  animateIn?: boolean
  delay?: number
  className?: string
  ariaLabel?: string
}

/** Donut de salud (design.md §7): track 12px, arco animado 1.2s ease-out. */
export const HealthRing = memo(function HealthRing({
  value,
  size = 200,
  stroke = 12,
  variant = 'health',
  status = 'online',
  center,
  animateIn = true,
  delay = 0,
  className,
  ariaLabel,
}: HealthRingProps) {
  const id = useId()
  const reduce = useReducedMotion()
  const r = (size - stroke) / 2
  const c = size / 2
  const frac = Math.min(100, Math.max(0, value)) / 100
  const statusColor = STATUS_COLORS[status]

  return (
    <div
      className={cn('relative inline-flex items-center justify-center', className)}
      style={{ width: size, height: size }}
      role="img"
      aria-label={ariaLabel ?? i18n.t('common.healthDefault', { value })}
    >
      <svg width={size} height={size} className="-rotate-90">
        {variant === 'health' && (
          <defs>
            <linearGradient id={id} x1="0%" y1="0%" x2="100%" y2="100%">
              <stop offset="0%" stopColor="#22D3EE" />
              <stop offset="100%" stopColor="#34D399" />
            </linearGradient>
          </defs>
        )}
        <circle
          cx={c}
          cy={c}
          r={r}
          fill="none"
          stroke="rgb(var(--border))"
          strokeWidth={stroke}
        />
        <motion.circle
          cx={c}
          cy={c}
          r={r}
          fill="none"
          stroke={variant === 'health' ? `url(#${id})` : statusColor}
          strokeWidth={stroke}
          strokeLinecap="round"
          pathLength={1}
          strokeDasharray="1 1"
          initial={animateIn && !reduce ? { pathLength: 0 } : { pathLength: frac }}
          animate={{ pathLength: frac }}
          transition={{ duration: 1.2, ease: 'easeOut', delay }}
        />
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">
        {center ?? (
          <span className="font-display font-bold text-text-primary" style={{ fontSize: size * 0.28 }}>
            {value}
          </span>
        )}
      </div>
    </div>
  )
})
