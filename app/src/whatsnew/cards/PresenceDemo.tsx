import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import { Router } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useDemoClock } from '../useDemoClock'

/**
 * Demo del recheck de vecinos ARP (#2073): el equipo lleva un rato en silencio,
 * el kernel deja su entrada en STALE y la unidad le manda un echo corto. Al
 * volver la respuesta la entrada pasa a REACHABLE y el equipo sigue online, en
 * lugar de desaparecer del mapa como antes.
 *
 * La demo alterna el ciclo entero, incluyendo el estado viejo, porque el
 * cambio se entiende mejor viendo lo que pasaba antes.
 */
export function PresenceDemo() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  // 12 pasos: 0..3 STALE, 4..6 ping, 7..11 REACHABLE.
  const step = useDemoClock(12, 340)
  const pinging = step >= 4 && step < 7
  const reachable = step >= 7

  const state = reachable ? 'REACHABLE' : 'STALE'

  return (
    <div className="flex h-full min-h-[150px] flex-col justify-center gap-3">
      <div className="flex items-center gap-2.5 rounded-lg border border-border bg-surface px-3 py-2">
        <Router className="h-4 w-4 shrink-0 text-text-secondary" strokeWidth={1.75} aria-hidden="true" />
        <div className="min-w-0">
          <p className="truncate text-caption font-medium text-text-primary">{t('whatsnew.demoDevice')}</p>
          <p className="font-mono text-[10px] text-text-muted">192.168.1.40</p>
        </div>
        <span
          className={cn(
            'ml-auto flex shrink-0 items-center gap-1.5 rounded-md px-1.5 py-0.5 text-[10px] font-medium transition-colors',
            reachable ? 'bg-ok/10 text-ok' : 'bg-hover text-text-muted',
          )}
        >
          <span className={cn('h-1.5 w-1.5 rounded-full', reachable ? 'bg-ok' : 'bg-text-muted')} aria-hidden="true" />
          {reachable ? t('whatsnew.online') : t('whatsnew.offline')}
        </span>
      </div>

      <div className="relative flex items-center gap-2 rounded-lg border border-border bg-canvas px-3 py-2 font-mono text-[10px]">
        <span className="text-text-muted">ip neigh</span>
        <span className="min-w-0 flex-1 truncate text-text-primary">192.168.1.40</span>
        <span className={cn('shrink-0', reachable ? 'text-ok' : 'text-warn')}>{state}</span>
        <AnimatePresence>
          {pinging && !reduce && (
            <motion.span
              key="ping"
              initial={{ opacity: 0, scale: 0.6 }}
              animate={{ opacity: [0.9, 0], scale: [0.6, 2.1] }}
              exit={{ opacity: 0 }}
              transition={{ duration: 1.1, repeat: Infinity }}
              className="pointer-events-none absolute right-2 h-6 w-6 rounded-full border border-accent"
              aria-hidden="true"
            />
          )}
        </AnimatePresence>
      </div>

      <p className="text-[10px] leading-tight text-text-muted">{t('whatsnew.cards.presence.caption')}</p>
    </div>
  )
}
