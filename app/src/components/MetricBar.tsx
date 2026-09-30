import { motion, useReducedMotion } from 'framer-motion'
import { cn } from '@/lib/utils'

interface MetricBarProps {
  /** 0–100 */
  value: number
  className?: string
}

/** Color semántico por umbral (design.md §10.11) */
export function metricColor(value: number): string {
  if (value > 90) return 'bg-danger'
  if (value >= 70) return 'bg-warn'
  return 'bg-accent'
}

/** Barra de progreso fina h-1.5 para CPU/RAM. */
export function MetricBar({ value, className }: MetricBarProps) {
  const reduce = useReducedMotion()
  const v = Math.min(100, Math.max(0, value))
  return (
    <div
      className={cn('h-1.5 w-full overflow-hidden rounded-full bg-border/50', className)}
      role="progressbar"
      aria-valuenow={v}
      aria-valuemin={0}
      aria-valuemax={100}
    >
      <motion.div
        className={cn('h-full rounded-full', metricColor(v))}
        initial={reduce ? { width: `${v}%` } : { width: 0 }}
        animate={{ width: `${v}%` }}
        transition={{ duration: 0.8, ease: 'easeOut' }}
      />
    </div>
  )
}
