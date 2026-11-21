import { useEffect } from 'react'
import { ExternalLink } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useServerVersion } from '@/hooks/useServerVersion'
import { cn } from '@/lib/utils'
import { CARD_VERSIONS, cardsForVersion } from '@/whatsnew/registry'
import type { WhatsNewCard } from '@/whatsnew/types'
import { markWhatsNewSeen } from '@/whatsnew/useWhatsNewSeen'

/**
 * Novedades (#2116): una sección por release y una tarjeta por cambio, cada una
 * con una demo animada de la interacción real. Sustituye a leer el changelog
 * para descubrir lo que trae la versión que acabas de instalar.
 *
 * Abrir la página marca la versión como vista, que es lo que apaga el punto del
 * sello del sidebar. Se marca al montar y no al pulsar un botón: llegar aquí ya
 * es haberla visto.
 */
/** Autor propio: sus tarjetas no llevan agradecimiento, es lo esperado. */
const OWN_AUTHOR = 'gnacho'

/**
 * Changelog completo, en la web. La página existe en los idiomas de la app
 * (español en la raíz, el resto en /<lang>/changelog/) y el resto de idiomas
 * cae al inglés, que es el idioma en que se escriben las notas.
 */
const CHANGELOG_LANGS = ['es', 'en', 'zh', 'ru']
function changelogUrl(lang: string): string {
  const base = String(lang ?? 'en').split('-')[0]?.toLowerCase() ?? 'en'
  if (base === 'es') return 'https://netpulse.cloudless.club/changelog'
  if (CHANGELOG_LANGS.includes(base)) return `https://netpulse.cloudless.club/${base}/changelog/`
  return 'https://netpulse.cloudless.club/en/changelog/'
}

function Card({ card }: { card: WhatsNewCard }) {
  const { t } = useTranslation()
  const Icon = card.icon
  const { Demo } = card
  return (
    <article className="flex h-full flex-col rounded-xl border border-border bg-surface p-4">
      <div className="flex items-start gap-3">
        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-border bg-elevated text-accent">
          <Icon className="h-4.5 w-4.5" strokeWidth={1.75} aria-hidden="true" />
        </span>
        <div className="min-w-0">
          <h3 className="text-body font-semibold text-text-primary">{t(`whatsnew.cards.${card.id}.title`)}</h3>
          <p className="mt-1 text-caption leading-relaxed text-text-secondary">{t(`whatsnew.cards.${card.id}.body`)}</p>
        </div>
      </div>

      {/* La demo no roba el foco y todo lo que dice está también en el texto o
          en la nota de la propia demo. */}
      {/* data-demo-id: ancla estable para el script que exporta las demos a
          GIF para las notas de release (docs/releases/README.md). */}
      <div data-demo-id={card.id} className="mt-3 flex-1 rounded-lg border border-border bg-canvas p-3">
        <Demo />
      </div>

      <footer className="mt-3 flex items-center justify-between gap-2 text-[11px] text-text-muted">
        {/* Lo nuestro no se agradece: solo se acredita lo que vino de fuera. */}
        <span className="truncate">
          {card.author && card.author !== OWN_AUTHOR ? t('whatsnew.by', { author: card.author }) : ''}
        </span>
        <a
          href={`https://github.com/gnacho/netpulse/pull/${card.pr}`}
          target="_blank"
          rel="noreferrer"
          className="inline-flex shrink-0 items-center gap-1 rounded-md border border-border px-1.5 py-0.5 font-mono text-[10px] text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
        >
          #{card.pr}
          <ExternalLink className="h-3 w-3" strokeWidth={2} aria-hidden="true" />
        </a>
      </footer>
    </article>
  )
}

export function WhatsNew() {
  const { t, i18n } = useTranslation()
  const version = useServerVersion()

  useEffect(() => {
    if (version) markWhatsNewSeen(version)
  }, [version])

  return (
    <div className="mx-auto w-full max-w-[1100px] space-y-6">
      {/* El título lo pinta el topbar (pageTitleKey); aquí solo el subtítulo,
          para no repetirlo dos veces en la misma pantalla. */}
      <p className="-mt-2 text-caption text-text-secondary">{t('whatsnew.subtitle')}</p>

      {CARD_VERSIONS.map((v, i) => (
        <section key={v} className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="font-display text-h2 text-text-primary">{v}</h2>
            {i === 0 && (
              <span className={cn('rounded-md border border-accent/30 bg-accent-soft px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-accent')}>
                {t('whatsnew.latest')}
              </span>
            )}
          </div>
          <div className="grid gap-4 lg:grid-cols-2">
            {cardsForVersion(v).map((card) => (
              <Card key={card.id} card={card} />
            ))}
          </div>
        </section>
      ))}

      <p className="pt-2 text-[11px] text-text-muted">{t('whatsnew.footnote')}</p>
      {/* Todavía en GitHub: la web tendrá su propia página de cambios. */}
      <a
        href={changelogUrl(i18n.language)}
        target="_blank"
        rel="noreferrer"
        className="inline-flex items-center gap-1 text-caption font-medium text-accent transition-colors hover:text-accent/80"
      >
        {t('whatsnew.changelog')}
        <ExternalLink className="h-3 w-3" strokeWidth={2} aria-hidden="true" />
      </a>
    </div>
  )
}
