import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { AlertItem } from '@/components/AlertItem'
import { tomorrowAtNine } from '@/lib/snooze'
import type { AlertEvent } from '@/data/types'
import { useDemoClock } from '../useDemoClock'

/** Marca de tiempo fija: el relativo de la fila ("hace 4 min") no debe correr
 *  mientras se graba la demo. */
const NOW = Math.floor(Date.now() / 1000)

const ALERT: AlertEvent = {
  id: 'whatsnew-alert-actions',
  category: 'clients',
  urgent: true,
  severity: 'warn',
  type: 'unknown-device',
  title: 'Unknown device',
  description: '',
  time: '',
  ts: NOW - 240,
  read: false,
}

/**
 * Demo de las tres acciones de cada alerta (#2185): marcar leída, posponer y
 * ver más. El menú de posposición se abre, se elige "hasta mañana a las 9:00"
 * y la fila queda marcada: posponer pausa avisos, no borra el incidente.
 *
 * La fila es el componente real (`AlertItem`), con una alerta de muestra; el
 * menú repite las clases y las claves del `SnoozeMenu` de verdad porque el
 * original vive en un portal de Radix, fuera del recuadro que se captura.
 */
export function AlertActionsDemo() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  // 14 pasos: 0-1 fila con acciones, 2-7 menú abierto, 8-13 fila pospuesta.
  const step = useDemoClock(14, 280)
  const menuOpen = step >= 2 && step <= 7
  const focused = step >= 6 && step <= 7
  const snoozed = step >= 8

  const alert: AlertEvent = snoozed
    ? { ...ALERT, snoozed: true, snoozedUntil: tomorrowAtNine() }
    : ALERT

  const options = [
    t('alerts.snooze1h'),
    t('alerts.snooze2h'),
    t('alerts.snooze8h'),
    t('alerts.snoozeTomorrow'),
    t('alerts.snooze7d'),
  ]

  return (
    <div className="relative flex h-full min-h-[250px] flex-col gap-2">
      {/* La nota va arriba: el menú se despliega encima y no debe taparla. */}
      <p className="text-[10px] leading-tight text-text-muted">{t('whatsnew.cards.alert-actions.caption')}</p>
      <AlertItem
        alert={alert}
        onMarkRead={() => undefined}
        onSnooze={() => undefined}
        onUnsnooze={() => undefined}
        onClick={() => undefined}
      />

      <AnimatePresence>
        {menuOpen && (
          <motion.div
            initial={reduce ? false : { opacity: 0, y: -4, scale: 0.97 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -4, scale: 0.97 }}
            transition={{ duration: 0.18, ease: 'easeOut' }}
            className="absolute right-2 top-[3.6rem] z-10 w-60 rounded-lg border border-border bg-elevated p-1 shadow-xl"
            aria-hidden="true"
          >
            <div className="px-2 py-1.5">
              <span className="block text-xs font-medium text-text-primary">{t('alerts.snoozeTitle')}</span>
              <span className="mt-0.5 block text-[10px] leading-snug text-text-muted">{t('alerts.snoozeDesc')}</span>
            </div>
            <div className="my-1 h-px bg-border" />
            {options.map((label, i) => (
              <div
                key={label}
                className={
                  'rounded-sm px-2 py-1 text-[11px] transition-colors ' +
                  (focused && i === 3 ? 'bg-hover text-accent' : 'text-text-primary')
                }
              >
                {label}
              </div>
            ))}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
