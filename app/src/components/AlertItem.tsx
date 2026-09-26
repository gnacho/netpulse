import { AlertTriangle, CheckCircle2, CircleArrowUp, Eraser, Fingerprint, Info, OctagonX } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'
import { alertRelTime } from '@/i18n'
import type { AlertSeverity, AlertEvent } from '@/data/mock'
import { alertTitle } from '@/lib/alerts-i18n'
import { RowAction } from '@/components/RowAction'
import { cn } from '@/lib/utils'

const SEVERITY: Record<AlertSeverity, { icon: LucideIcon; tile: string; dot: string; stripe: string }> = {
  warn: { icon: AlertTriangle, tile: 'bg-warn/10 text-warn', dot: 'bg-warn', stripe: 'border-l-warn' },
  critical: { icon: OctagonX, tile: 'bg-danger/10 text-danger', dot: 'bg-danger', stripe: 'border-l-danger' },
  info: { icon: Info, tile: 'bg-info/10 text-info', dot: 'bg-info', stripe: 'border-l-info' },
  ok: { icon: CheckCircle2, tile: 'bg-ok/10 text-ok', dot: 'bg-ok', stripe: 'border-l-ok' },
}

interface AlertItemProps {
  alert: AlertEvent
  onClick?: () => void
  onDismiss?: (id: string) => void
  className?: string
}

/** Ítem de alerta del resumen (home.md §⑥): tile de severidad, título,
 *  tiempo y dot no-leído. #862: sin descripción/hint, solo el título, y con
 *  las acciones del feed (contextual + limpiar) reveladas en hover de fila.
 *  div role=button (no <button>) para poder anidar los botones de acción;
 *  Enter/Espacio como un botón real y las acciones hacen stopPropagation. */
export function AlertItem({ alert, onClick, onDismiss, className }: AlertItemProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const s = SEVERITY[alert.severity]
  const Icon = s.icon
  // #772: la alerta de desconocido enlaza a la tarjeta de alta (intake).
  const intakeMac = alert.type === 'unknown-device' ? alert.vars?.mac : undefined
  // #862: acción contextual igual que en el feed (#833).
  const action = intakeMac
    ? {
        icon: Fingerprint,
        title: t('alerts.actions.identify'),
        onClick: () => navigate(`/devices?intake=${encodeURIComponent(intakeMac)}`),
      }
    : alert.type === 'agent-outdated'
      ? {
          icon: CircleArrowUp,
          title: t('alerts.actions.updateAgent'),
          onClick: () => navigate('/orchestration'),
        }
      : undefined
  const handleRowClick = () => {
    if (intakeMac) navigate(`/devices?intake=${encodeURIComponent(intakeMac)}`)
    onClick?.()
  }
  return (
    <div
      role="button"
      tabIndex={0}
      onClick={handleRowClick}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          handleRowClick()
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
        <div className="flex items-center justify-between gap-2">
          <span className="truncate text-sm font-medium text-text-primary">{alertTitle(t, alert)}</span>
          <span className="flex shrink-0 items-center gap-1">
            {action && (
              <RowAction
                icon={action.icon}
                label={action.title}
                title={action.title}
                onClick={(e) => { e.stopPropagation(); action.onClick() }}
                className="text-text-muted hover:text-accent"
              />
            )}
            {onDismiss && (
              <RowAction
                icon={Eraser}
                label={t('alerts.actions.dismiss')}
                title={t('alerts.actions.dismiss')}
                onClick={(e) => { e.stopPropagation(); onDismiss(alert.id) }}
                className="text-text-muted hover:text-text-primary"
              />
            )}
            <span className="text-caption text-text-muted">{alertRelTime(alert)}</span>
          </span>
        </div>
      </div>
      {!alert.read && (
        <span className="relative flex h-1.5 w-1.5 shrink-0">
          <span className={cn('absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping-soft', s.dot)} />
          <span className={cn('relative inline-flex h-1.5 w-1.5 rounded-full', s.dot)} />
        </span>
      )}
    </div>
  )
}
