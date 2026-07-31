import { ArrowDownRight, ArrowUpRight } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { numLocale } from '@/i18n'
import { CountUp } from '@/components/CountUp'
import { Sparkline } from '@/components/Sparkline'
import { cn } from '@/lib/utils'

interface StatCardProps {
  label: string
  value: number
  unit?: string
  decimals?: number
  /** Delta vs periodo anterior en % (signo = dirección) */
  delta?: number
  /** Invertir semántica (p. ej. latencia: bajar es bueno) */
  invertDelta?: boolean
  sparkline?: number[]
  sparkColor?: string
  icon?: LucideIcon
  /** Variante live: dot pulsante */
  live?: boolean
  className?: string
}

/** Tarjeta de métrica (design.md §10.1): label caps, cifra mono, delta, sparkline. */
export function StatCard({
  label,
  value,
  unit,
  decimals = 0,
  delta,
  invertDelta = false,
  sparkline,
  sparkColor = '#22D3EE',
  icon: Icon,
  live = false,
  className,
}: StatCardProps) {
  const { t } = useTranslation()
  const deltaGood = delta !== undefined && (invertDelta ? delta < 0 : delta > 0)
  const deltaBad = delta !== undefined && (invertDelta ? delta > 0 : delta < 0)
  return (
    <div
      className={cn(
        'flex min-h-[120px] flex-col justify-between rounded-2xl border border-border bg-surface p-5',
        className,
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="text-label uppercase text-text-muted">{label}</span>
        {live && (
          <span className="relative flex h-2 w-2">
            <span className="absolute inline-flex h-full w-full rounded-full bg-ok opacity-75 animate-ping-soft" />
            <span className="relative inline-flex h-2 w-2 rounded-full bg-ok" />
          </span>
        )}
        {Icon && !live && <Icon className="h-4 w-4 text-text-muted" strokeWidth={1.75} />}
      </div>
      <div className="mt-2 flex items-end justify-between gap-3">
        <div>
          <div className="font-mono text-stat text-text-primary">
            <CountUp value={value} decimals={decimals} />
            {unit && <span className="ml-1 text-sm font-medium text-text-secondary">{unit}</span>}
          </div>
          {delta !== undefined && (
            <div
              className={cn(
                'mt-1 inline-flex items-center gap-0.5 text-caption font-semibold',
                deltaGood && 'text-ok',
                deltaBad && 'text-danger',
                !deltaGood && !deltaBad && 'text-text-muted',
              )}
            >
              {delta > 0 ? (
                <ArrowUpRight className="h-3.5 w-3.5" strokeWidth={1.75} />
              ) : (
                <ArrowDownRight className="h-3.5 w-3.5" strokeWidth={1.75} />
              )}
              {Math.abs(delta).toLocaleString(numLocale())}%
              <span className="font-normal text-text-muted">{t('statcard.vsPrevious')}</span>
            </div>
          )}
        </div>
        {sparkline && <Sparkline data={sparkline} width={96} height={32} color={sparkColor} area />}
      </div>
    </div>
  )
}
