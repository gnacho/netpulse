import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useDemoClock } from '../useDemoClock'

/**
 * Demo del selector de idioma (#2070 zh, #2092 ru): el mismo panel en los
 * cuatro idiomas de la UI, con una etiqueta de navegación y una fecha, porque
 * el trabajo no fueron solo las cadenas: fechas y números siguen a cada idioma.
 *
 * Las cuatro cadenas son datos de la demo, no copy de la UI: se muestran todas
 * a la vez y por eso no pueden salir de un catálogo que solo carga el idioma
 * activo.
 */
const LOCALES = [
  { code: 'en', short: 'EN', nav: 'Devices', date: '19 Nov 2026' },
  { code: 'es', short: 'ES', nav: 'Dispositivos', date: '19 nov 2026' },
  { code: 'zh', short: '中文', nav: '设备', date: '2026年11月19日' },
  { code: 'ru', short: 'Рус', nav: 'Устройства', date: '19 нояб. 2026 г.' },
] as const

export function LanguagesDemo() {
  const { t } = useTranslation()
  const step = useDemoClock(LOCALES.length, 1500)
  const active = LOCALES[step]!

  return (
    <div className="flex h-full min-h-[150px] flex-col justify-center gap-3">
      <div className="flex items-center gap-1 rounded-lg border border-border bg-surface p-1">
        {LOCALES.map((l) => (
          <span
            key={l.code}
            className={cn(
              'flex-1 rounded-md px-1 py-1 text-center text-[10px] font-medium transition-colors',
              active.code === l.code ? 'bg-accent-soft text-accent' : 'text-text-muted',
            )}
          >
            {l.short}
          </span>
        ))}
      </div>

      <div className="rounded-lg border border-border bg-canvas px-3 py-2">
        <p className="text-[10px] uppercase tracking-wider text-text-muted">{t('whatsnew.navigation')}</p>
        <div className="mt-1 flex items-baseline justify-between gap-2">
          <motion.span
            key={active.code + '-nav'}
            initial={{ opacity: 0, y: 4 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25 }}
            className="truncate text-caption font-medium text-text-primary"
          >
            {active.nav}
          </motion.span>
          <motion.span
            key={active.code + '-date'}
            initial={{ opacity: 0, y: 4 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25 }}
            className="shrink-0 font-mono text-[10px] text-text-secondary"
          >
            {active.date}
          </motion.span>
        </div>
      </div>

      <p className="text-[10px] leading-tight text-text-muted">{t('whatsnew.cards.languages.caption')}</p>
    </div>
  )
}
