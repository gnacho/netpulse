/**
 * Help — sección de ayuda in-app con walkthroughs guiados (issue #484).
 * Nació del feedback del foro: un admin nuevo se atascaba en "¿dónde doy de
 * alta un agente?" y la única referencia era el README, que ya había drift
 * de la UI al menos una vez.
 *
 * Los pasos citan las etiquetas REALES de la UI (mismo i18n que los
 * componentes) renderizadas como chips ⟦así⟧, de modo que la ayuda deriva
 * junto con la interfaz: si un botón cambia de nombre, la búsqueda del chip
 * en los locales lo delata. Sin capturas bitmap a propósito: envejecen mal
 * y nadie quiere mantenerlas.
 */
import { useTranslation } from 'react-i18next'
import {
  BookOpen,
  CircleHelp,
  Cpu,
  KeyRound,
  ListChecks,
  Router as RouterIcon,
  Wifi,
} from 'lucide-react'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'

/** Chip de etiqueta de UI dentro de un paso (botón, sección, página). */
function UiChip({ children }: { children: string }) {
  return (
    <span className="mx-0.5 inline-flex items-center rounded-md border border-border bg-surface-2 px-1.5 py-px align-middle text-[0.85em] font-medium text-text-primary">
      {children}
    </span>
  )
}

/**
 * RichText: renderiza un string i18n marcando ⟦etiqueta de UI⟧ como chip.
 * Sin marcadores, texto plano. Un solo delimitador a propósito: mantener la
 * ayuda escaneable y no convertir esto en un mini-lenguaje.
 */
function RichText({ text }: { text: string }) {
  const parts = text.split(/⟦([^⟧]+)⟧/g)
  return (
    <>
      {parts.map((p, i) => (i % 2 === 1 ? <UiChip key={i}>{p}</UiChip> : p))}
    </>
  )
}

const FLOWS = [
  { key: 'firstLogin', icon: KeyRound },
  { key: 'addRouter', icon: RouterIcon },
  { key: 'installAgent', icon: CircleHelp },
  { key: 'channelPlan', icon: Wifi },
  { key: 'firmware', icon: Cpu },
] as const

export default function Help() {
  const { t } = useTranslation()

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-4 py-6 sm:px-6">
      <header className="flex flex-col gap-2">
        <h1 className="flex items-center gap-2.5 text-xl font-semibold text-text-primary">
          <CircleHelp className="h-5 w-5 text-accent" strokeWidth={1.75} aria-hidden="true" />
          {t('help.title')}
        </h1>
        <p className="text-sm text-text-secondary">{t('help.subtitle')}</p>
      </header>

      <section
        aria-label={t('help.flowsAria')}
        className="rounded-xl border border-border bg-surface"
      >
        <Accordion type="multiple" defaultValue={['firstLogin']}>
          {FLOWS.map(({ key, icon: Icon }) => {
            const steps = t(`help.flows.${key}.steps`, { returnObjects: true }) as string[]
            return (
              <AccordionItem key={key} value={key}>
                <AccordionTrigger className="px-4 py-3.5 hover:no-underline">
                  <span className="flex items-center gap-2.5 text-left">
                    <Icon
                      className="h-4 w-4 shrink-0 text-accent"
                      strokeWidth={1.75}
                      aria-hidden="true"
                    />
                    <span className="flex flex-col">
                      <span className="text-sm font-medium text-text-primary">
                        {t(`help.flows.${key}.title`)}
                      </span>
                      <span className="text-caption text-text-muted">
                        {t(`help.flows.${key}.subtitle`)}
                      </span>
                    </span>
                  </span>
                </AccordionTrigger>
                <AccordionContent className="px-4 pb-4">
                  <p className="mb-3 text-caption leading-relaxed text-text-secondary">
                    <RichText text={t(`help.flows.${key}.intro`)} />
                  </p>
                  <ol className="flex flex-col gap-2.5">
                    {Array.isArray(steps)
                      ? steps.map((s, i) => (
                          <li key={i} className="flex items-start gap-2.5">
                            <span
                              className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full border border-accent/40 bg-accent-soft text-[11px] font-semibold text-accent"
                              aria-hidden="true"
                            >
                              {i + 1}
                            </span>
                            <span className="text-caption leading-relaxed text-text-secondary">
                              <RichText text={s} />
                            </span>
                          </li>
                        ))
                      : null}
                  </ol>
                  {t(`help.flows.${key}.tip`, { defaultValue: '' }) && (
                    <p className="mt-3 flex items-start gap-2 rounded-lg border border-border bg-surface-2 px-3 py-2 text-caption leading-relaxed text-text-secondary">
                      <ListChecks
                        className="mt-0.5 h-3.5 w-3.5 shrink-0 text-text-muted"
                        strokeWidth={1.75}
                        aria-hidden="true"
                      />
                      <RichText text={t(`help.flows.${key}.tip`)} />
                    </p>
                  )}
                </AccordionContent>
              </AccordionItem>
            )
          })}
        </Accordion>
      </section>

      <footer className="flex items-start gap-2.5 rounded-xl border border-border bg-surface px-4 py-3 text-caption text-text-secondary">
        <BookOpen className="mt-0.5 h-4 w-4 shrink-0 text-text-muted" strokeWidth={1.75} aria-hidden="true" />
        <span>
          <RichText text={t('help.docsFooter')} />{' '}
          <a
            href="https://github.com/gnacho/netpulse#readme"
            target="_blank"
            rel="noreferrer"
            className="text-accent hover:underline"
          >
            {t('help.docsLink')}
          </a>
        </span>
      </footer>
    </div>
  )
}
