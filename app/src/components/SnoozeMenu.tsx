/**
 * Menú "Posponer" (#2185) compartido por el feed de Alertas y la tarjeta del
 * resumen. Posponer NO oculta la alerta: pausa avisos, escalado y
 * re-emisiones mientras dure, y la fila queda marcada hasta que expire o se
 * quite. Las opciones (1 h, 2 h, 8 h, mañana a las 9:00, 7 días) se resuelven
 * en el cliente: "hasta mañana a las 9:00" depende del reloj de quien mira,
 * no del servidor.
 */
import { useTranslation } from 'react-i18next'
import type { MouseEvent as ReactMouseEvent } from 'react'
import { BellOff, Clock } from 'lucide-react'
import { tomorrowAtNine } from '@/lib/snooze'
import type { SnoozeDuration, SnoozeWhen } from '@/lib/snooze'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'

interface SnoozeMenuProps {
  snoozed?: boolean
  onSnooze: (when: SnoozeWhen) => void
  onUnsnooze: () => void
  className?: string
  align?: 'start' | 'end' | 'center'
}

export function SnoozeMenu({ snoozed, onSnooze, onUnsnooze, className, align = 'end' }: SnoozeMenuProps) {
  const { t } = useTranslation()
  const stop = (e: ReactMouseEvent) => e.stopPropagation()
  const options: { key: SnoozeDuration | 'tomorrow'; label: string; when: SnoozeWhen }[] = [
    { key: '1h', label: t('alerts.snooze1h'), when: { duration: '1h' } },
    { key: '2h', label: t('alerts.snooze2h'), when: { duration: '2h' } },
    { key: '8h', label: t('alerts.snooze8h'), when: { duration: '8h' } },
    { key: 'tomorrow', label: t('alerts.snoozeTomorrow'), when: { until: tomorrowAtNine() } },
    { key: '7d', label: t('alerts.snooze7d'), when: { duration: '7d' } },
  ]
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          onPointerDown={stop}
          onClick={stop}
          title={t('alerts.actions.snooze')}
          aria-label={t('alerts.actions.snooze')}
          className={cn(
            'flex items-center justify-center rounded-md p-1.5 text-text-muted transition-colors hover:bg-hover hover:text-accent pointer-coarse:p-2.5',
            snoozed && 'text-accent',
            className,
          )}
        >
          <Clock className="h-4 w-4 shrink-0" strokeWidth={1.75} />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align={align}
        className="w-64 border-border bg-elevated"
        onPointerDown={stop}
        onClick={stop}
      >
        <DropdownMenuLabel className="font-normal">
          <span className="block text-sm font-medium text-text-primary">{t('alerts.snoozeTitle')}</span>
          <span className="mt-1 block text-caption leading-snug text-text-muted">{t('alerts.snoozeDesc')}</span>
        </DropdownMenuLabel>
        <DropdownMenuSeparator className="bg-border" />
        {options.map((opt) => (
          <DropdownMenuItem
            key={opt.key}
            onSelect={() => onSnooze(opt.when)}
            className="text-text-primary focus:bg-hover focus:text-text-primary"
          >
            {opt.label}
          </DropdownMenuItem>
        ))}
        {snoozed && (
          <>
            <DropdownMenuSeparator className="bg-border" />
            <DropdownMenuItem
              onSelect={onUnsnooze}
              className="gap-2 text-accent focus:bg-hover focus:text-accent"
            >
              <BellOff className="h-3.5 w-3.5" strokeWidth={1.75} />
              {t('alerts.unsnooze')}
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
