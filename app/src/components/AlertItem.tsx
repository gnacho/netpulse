import { AlertTriangle, CheckCircle2, Info, OctagonX } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { relTime } from '@/i18n'
import type { AlertSeverity, AlertEvent } from '@/data/mock'
import { cn } from '@/lib/utils'

const SEVERITY: Record<AlertSeverity, { icon: LucideIcon; tile: string; dot: string }> = {
  warn: { icon: AlertTriangle, tile: 'bg-warn/10 text-warn', dot: 'bg-warn' },
  critical: { icon: OctagonX, tile: 'bg-danger/10 text-danger', dot: 'bg-danger' },
  info: { icon: Info, tile: 'bg-info/10 text-info', dot: 'bg-info' },
  ok: { icon: CheckCircle2, tile: 'bg-ok/10 text-ok', dot: 'bg-ok' },
}

interface AlertItemProps {
  alert: AlertEvent
  onClick?: () => void
  className?: string
}

/** Ítem de alerta (design.md §10.6): tile de severidad, título, descripción, tiempo, dot no-leído. */
export function AlertItem({ alert, onClick, className }: AlertItemProps) {
  const s = SEVERITY[alert.severity]
  const Icon = s.icon
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'group flex w-full items-start gap-3 rounded-xl px-3 py-3 text-left transition-colors duration-150 hover:bg-hover',
        !alert.read && 'bg-elevated',
        className,
      )}
    >
      <div className={cn('flex h-9 w-9 shrink-0 items-center justify-center rounded-lg', s.tile)}>
        <Icon className="h-[18px] w-[18px]" strokeWidth={1.75} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <span className="truncate text-sm font-medium text-text-primary">{alert.title}</span>
          <span className="shrink-0 text-caption text-text-muted">{relTime(alert.time)}</span>
        </div>
        <p className="mt-0.5 truncate text-caption text-text-secondary">{alert.description}</p>
      </div>
      {!alert.read && (
        <span className="relative mt-2 flex h-1.5 w-1.5 shrink-0">
          <span className={cn('absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping-soft', s.dot)} />
          <span className={cn('relative inline-flex h-1.5 w-1.5 rounded-full', s.dot)} />
        </span>
      )}
    </button>
  )
}
