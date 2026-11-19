import { motion } from 'framer-motion'
import { Ban } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useDemoClock } from '../useDemoClock'

/** Dominios de la demo: dos normales y uno bloqueado por el filtro. */
const ROWS = [
  { domain: 'api.netflix.com', hour: 34, day: 62, blocked: false },
  { domain: 'ads.doubleclick.net', hour: 18, day: 41, blocked: true },
  { domain: 'time.cloudflare.com', hour: 12, day: 27, blocked: false },
]

/**
 * Demo del panel DNS por dispositivo (#2078): las barras crecen con la ventana
 * de una hora y luego con la de un día, y el dominio bloqueado va en rojo.
 * Los contadores son de muestra, como los datos de la demo de la app.
 */
export function DnsDemo() {
  const { t } = useTranslation()
  // 8 pasos: 4 con la ventana de 1 h y 4 con la de 24 h.
  const step = useDemoClock(8, 380)
  const day = step >= 4
  const max = day ? 62 : 34

  return (
    <div className="flex h-full min-h-[150px] flex-col gap-2">
      <div className="flex items-center justify-between">
        <span className="text-[11px] font-medium text-text-secondary">{t('whatsnew.topDomains')}</span>
        <span className="rounded-md border border-border bg-elevated px-1.5 py-0.5 font-mono text-[10px] text-text-primary">
          {day ? '24 h' : '1 h'}
        </span>
      </div>
      <div className="flex flex-1 flex-col justify-center gap-2">
        {ROWS.map((row) => (
          <div key={row.domain} className="flex items-center gap-2">
            <span className={cn('w-28 shrink-0 truncate font-mono text-[10px]', row.blocked ? 'text-danger' : 'text-text-secondary')}>
              {row.domain}
            </span>
            <span className="relative h-2 min-w-0 flex-1 overflow-hidden rounded-full bg-hover">
              <motion.span
                className={cn('absolute inset-y-0 left-0 rounded-full', row.blocked ? 'bg-danger/70' : 'bg-accent/70')}
                animate={{ width: `${Math.round(((day ? row.day : row.hour) / max) * 100)}%` }}
                transition={{ duration: 0.45, ease: 'easeOut' }}
              />
            </span>
            <span className="w-6 shrink-0 text-right font-mono text-[10px] tabular-nums text-text-primary">
              {day ? row.day : row.hour}
            </span>
            {row.blocked && <Ban className="h-3 w-3 shrink-0 text-danger" aria-hidden="true" />}
          </div>
        ))}
      </div>
      <p className="text-[10px] leading-tight text-text-muted">{t('whatsnew.cards.dns-activity.caption')}</p>
    </div>
  )
}
