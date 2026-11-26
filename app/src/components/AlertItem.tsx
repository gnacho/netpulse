import type { MouseEvent as ReactMouseEvent } from 'react'
import { AlertTriangle, Check, CheckCircle2, ChevronRight, Clock, Info, OctagonX } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { alertRelTime } from '@/i18n'
import type { AlertSeverity, AlertEvent } from '@/data/mock'
import { alertTitle } from '@/lib/alerts-i18n'
import { SnoozeMenu } from '@/components/SnoozeMenu'
import { snoozeLabel } from '@/lib/snooze'
import type { SnoozeWhen } from '@/lib/snooze'
import { cn } from '@/lib/utils'

const SEVERITY: Record<AlertSeverity, { icon: LucideIcon; tile: string; dot: string; stripe: string }> = {
  warn: { icon: AlertTriangle, tile: 'bg-warn/10 text-warn', dot: 'bg-warn', stripe: 'border-l-warn' },
  critical: { icon: OctagonX, tile: 'bg-danger/10 text-danger', dot: 'bg-danger', stripe: 'border-l-danger' },
  info: { icon: Info, tile: 'bg-info/10 text-info', dot: 'bg-info', stripe: 'border-l-info' },
  ok: { icon: CheckCircle2, tile: 'bg-ok/10 text-ok', dot: 'bg-ok', stripe: 'border-l-ok' },
}

const ROW_ACTION =
  'flex items-center justify-center rounded-md p-1.5 text-text-muted transition-colors hover:bg-hover hover:text-accent disabled:cursor-default disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-text-muted pointer-coarse:p-2.5'

interface AlertItemProps {
  alert: AlertEvent
  onClick?: () => void
  /** #2185: las tres acciones del ítem (leído, posponer, ver más). */
  onMarkRead?: (id: string) => void
  onSnooze?: (id: string, when: SnoozeWhen) => void
  onUnsnooze?: (id: string) => void
  className?: string
}

/** Ítem de alerta del resumen (home.md §⑥): tile de severidad, título,
 *  tiempo y dot no-leído. #2185: tres acciones explícitas (marcar leído,
 *  posponer y ver más); "ver más" abre el registro en `/alerts`, donde vive
 *  el detalle completo. div role=button (no <button>) para poder anidar los
 *  botones de acción; Enter/Espacio como un botón real y las acciones hacen
 *  stopPropagation. */
export function AlertItem({ alert, onClick, onMarkRead, onSnooze, onUnsnooze, className }: AlertItemProps) {
  const { t, i18n } = useTranslation()
  const s = SEVERITY[alert.severity]
  const Icon = s.icon
  const viewMore = () => onClick?.()
  return (
    <div
      role="button"
      tabIndex={0}
      onClick={clickGuard(viewMore)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          viewMore()
        }
      }}
      className={cn(
        'group flex w-full cursor-pointer items-center gap-3 rounded-xl border-l-[3px] px-3 py-3 text-left transition-colors duration-150 hover:bg-hover focus-visible:outline-2 focus-visible:outline-accent',
        s.stripe,
        !alert.read && 'bg-elevated',
        className,
      )}
    >
      <div className={cn('flex h-9 w-9 shrink-0 items-center justify-center rounded-lg', s.tile)}>
        <Icon className="h-[18px] w-[18px]" strokeWidth={1.75} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center justify-between gap-2 sm:flex-nowrap">
          <span className="truncate text-sm font-medium text-text-primary">{alertTitle(t, alert)}</span>
          <span className="flex shrink-0 items-center gap-1 pointer-coarse:basis-full">
            {onMarkRead && (
              <button
                type="button"
                disabled={alert.read}
                onClick={(e) => { e.stopPropagation(); onMarkRead(alert.id) }}
                className={ROW_ACTION}
                title={t('alerts.actions.markRead')}
                aria-label={t('alerts.actions.markRead')}
              >
                <Check className="h-4 w-4 shrink-0" strokeWidth={1.75} />
              </button>
            )}
            {onSnooze && onUnsnooze && (
              <SnoozeMenu
                snoozed={alert.snoozed}
                onSnooze={(when) => onSnooze(alert.id, when)}
                onUnsnooze={() => onUnsnooze(alert.id)}
              />
            )}
            {onClick && (
              <button
                type="button"
                onClick={(e) => { e.stopPropagation(); viewMore() }}
                className={cn(ROW_ACTION, 'hover:text-accent')}
                title={t('alerts.actions.viewMore')}
                aria-label={t('alerts.actions.viewMore')}
              >
                <ChevronRight className="h-4 w-4 shrink-0" strokeWidth={1.75} />
              </button>
            )}
            <span className="text-caption text-text-muted">{alertRelTime(alert)}</span>
          </span>
        </div>
        {alert.snoozed && (
          <p className="mt-0.5 flex items-center gap-1 truncate text-caption text-accent">
            <Clock className="h-3 w-3 shrink-0" strokeWidth={2} />
            {alert.snoozedUntil
              ? t('alerts.snoozedUntil', { time: snoozeLabel(alert.snoozedUntil, i18n.language || 'es') })
              : t('alerts.snoozedIndefinitely')}
          </p>
        )}
      </div>
      {!alert.read && !alert.snoozed && (
        <span className="relative flex h-1.5 w-1.5 shrink-0">
          <span className={cn('absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping-soft', s.dot)} />
          <span className={cn('relative inline-flex h-1.5 w-1.5 rounded-full', s.dot)} />
        </span>
      )}
    </div>
  )
}

/** Envuelve el click de la fila para ignorar el arrastre de selección de
 *  texto (mismo criterio que el feed, #393). */
function clickGuard(fn: () => void) {
  return (e: ReactMouseEvent<HTMLDivElement>) => {
    const sel = window.getSelection()
    if (sel && sel.toString().trim().length > 0) return
    if ((e.target as HTMLElement).closest('button')) return
    fn()
  }
}
