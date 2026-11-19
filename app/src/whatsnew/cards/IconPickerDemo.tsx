import { motion, useReducedMotion } from 'framer-motion'
import { Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ICON_OVERRIDES } from '@/components/DeviceRow'
import { cn } from '@/lib/utils'
import { useDemoClock } from '../useDemoClock'

/** Iconos de la demo: seis dispositivos de cocina y baño, con etiqueta real. */
const POOL = ['refrigerator', 'washing-machine', 'microwave', 'coffee', 'toilet', 'droplets']
const TARGET = 'refrigerator'

/**
 * Demo del selector de iconos (#2107): se escribe el nombre del dispositivo en
 * el idioma de la UI, la rejilla se filtra y la fila del cliente cambia de
 * icono. La búsqueda va contra la etiqueta traducida, igual que la real, así
 * que en chino se escribe en chino y en ruso en ruso.
 */
export function IconPickerDemo() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const label = (n: string) => t(`devices.edit.iconNames.${n}`, { defaultValue: n })
  const term = label(TARGET)

  const step = useDemoClock(12, 300)
  // 0..4 escribe el término, 5..6 resalta, 7..11 ya está elegido.
  const typed = step < 7 ? term.slice(0, Math.ceil((term.length * Math.min(step + 1, 5)) / 5)) : term
  const picked = step >= 7 ? TARGET : 'monitor'
  const matches = POOL.filter((n) => label(n).toLowerCase().includes(typed.toLowerCase()))
  const Current = ICON_OVERRIDES[picked]!

  return (
    <div className="flex h-full min-h-[150px] flex-col justify-center gap-2">
      <div className="flex items-center gap-2 rounded-lg border border-border bg-surface px-2.5 py-1.5">
        <Current className="h-4 w-4 shrink-0 text-text-secondary" strokeWidth={1.75} aria-hidden="true" />
        <span className="truncate text-caption text-text-primary">{label(TARGET)}</span>
      </div>

      <div className="rounded-lg border border-border bg-surface p-2">
        <div className="flex items-center gap-1.5 rounded-md border border-border-strong bg-canvas px-2 py-1">
          <Search className="h-3 w-3 shrink-0 text-text-muted" aria-hidden="true" />
          <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-text-primary">
            {typed || <span className="text-text-muted">{t('whatsnew.search')}</span>}
          </span>
          {!reduce && <span className="h-3 w-px animate-pulse bg-accent" aria-hidden="true" />}
        </div>
        <div className="mt-1.5 flex gap-1.5">
          {matches.map((n) => {
            const Icon = ICON_OVERRIDES[n]!
            const isTarget = n === TARGET && step >= 5
            return (
              <motion.span
                key={n}
                animate={isTarget ? { scale: [1, 1.12, 1] } : { scale: 1 }}
                transition={{ duration: 0.5 }}
                className={cn(
                  'flex h-8 w-8 items-center justify-center rounded-md border',
                  isTarget ? 'border-accent bg-accent-soft text-accent' : 'border-border bg-elevated text-text-secondary',
                )}
              >
                <Icon className="h-4 w-4" strokeWidth={1.75} aria-hidden="true" />
              </motion.span>
            )
          })}
          {matches.length === 0 && <span className="text-[11px] text-text-muted">{t('whatsnew.noMatch')}</span>}
        </div>
      </div>
    </div>
  )
}
