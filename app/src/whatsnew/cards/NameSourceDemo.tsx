import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useDemoClock } from '../useDemoClock'

/** Fuentes que la ficha del cliente sabe nombrar (#2163). */
const SOURCES = ['dhcp', 'mdns', 'rdns', 'reservation', 'known_mac', 'manual'] as const

/** Nombre por fuente: el mismo equipo, contado con la evidencia de cada una. */
const NAMES: Record<(typeof SOURCES)[number], string> = {
  // Un placeholder del router no es un nombre: mDNS lo mejora y el resto de
  // fuentes ya traen el mismo nombre bueno. Los valores son neutros porque la
  // demo no se traduce.
  dhcp: 'esp-plug-4a2c',
  mdns: 'Shelly Plug S',
  rdns: 'plug.livingroom.lan',
  reservation: 'Shelly Plug S',
  known_mac: 'Shelly Plug S',
  manual: 'Shelly Plug S',
}

/**
 * Demo del origen del nombre (#2163): la ficha del cliente dice de dónde sale
 * el nombre que muestra, en lugar de elegirlo en silencio. La demo recorre las
 * fuentes que el servidor guarda, de la más débil a la que siempre gana.
 *
 * Los valores son de muestra, como los del resto de demos de la app.
 */
export function NameSourceDemo() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  // 12 pasos: dos por fuente, para que dé tiempo a leer cada una.
  const step = useDemoClock(12, 480)
  const idx = Math.floor(step / 2) % SOURCES.length
  const source: (typeof SOURCES)[number] = SOURCES[idx] ?? 'dhcp'
  const manual = source === 'manual'

  return (
    <div className="flex h-full min-h-[170px] flex-col gap-3">
      {/* Los dos bloques se centran: la tarjeta crece con la de al lado. */}
      <div className="flex flex-1 flex-col justify-center gap-3">
      <div className="flex items-center gap-2.5 rounded-lg border border-border bg-surface px-3 py-2">
        <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-accent-soft text-accent">
          {/* Sin icono de dispositivo: el nombre es el protagonista. */}
          <span className="font-display text-xs font-semibold">A</span>
        </span>
        <div className="min-w-0">
          <AnimatePresence mode="wait" initial={false}>
            <motion.p
              key={source}
              initial={reduce ? false : { opacity: 0, y: 4 }}
              animate={{ opacity: 1, y: 0 }}
              exit={reduce ? undefined : { opacity: 0, y: -4 }}
              transition={{ duration: 0.18, ease: 'easeOut' }}
              className="truncate text-caption font-medium text-text-primary"
            >
              {NAMES[source]}
            </motion.p>
          </AnimatePresence>
          <p className="font-mono text-[10px] text-text-muted">9C:53:22:1A:4B:7E</p>
        </div>
        <span className="ml-auto shrink-0 rounded-md bg-hover px-1.5 py-0.5 font-mono text-[10px] text-text-secondary">
          192.168.1.57
        </span>
      </div>

      <dl className="grid grid-cols-2 gap-x-3 gap-y-2 rounded-lg border border-border bg-canvas px-3 py-2">
        <div>
          {/* La ficha real usa el literal "MAC", igual que el resto de la app. */}
          <dt className="text-[9px] font-medium uppercase tracking-[0.06em] text-text-muted">MAC</dt>
          <dd className="font-mono text-[10px] text-text-secondary">9C:53:22:1A:4B:7E</dd>
        </div>
        <div>
          <dt className="text-[9px] font-medium uppercase tracking-[0.06em] text-text-muted">
            {t('devices.detail.nameSource')}
          </dt>
          <dd className="min-h-[14px]">
            <motion.span
              key={source}
              initial={reduce ? false : { opacity: 0, scale: 0.92 }}
              animate={{ opacity: 1, scale: 1 }}
              transition={{ duration: 0.18, ease: 'easeOut' }}
              className={cn(
                'inline-block rounded px-1.5 py-0.5 text-[10px] font-medium',
                manual ? 'bg-accent-soft text-accent' : 'bg-hover text-text-primary',
              )}
            >
              {t(`devices.detail.source.${source}`)}
            </motion.span>
          </dd>
        </div>
      </dl>
      </div>

      <p className="mt-auto text-[10px] leading-tight text-text-muted">{t('whatsnew.cards.name-source.caption')}</p>
    </div>
  )
}
