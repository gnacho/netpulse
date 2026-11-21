import { Link, useLocation } from 'react-router'
import { useTranslation } from 'react-i18next'
import { Sparkles, X } from 'lucide-react'
import { useServerVersion } from '@/hooks/useServerVersion'
import { useWhatsNewBadge } from '@/whatsnew/useWhatsNewSeen'

/**
 * WhatsNewBanner — el aviso de versión nueva (#2116) con enlace a Novedades.
 *
 * Aparece cuando la versión que corre el servidor no es la que este navegador
 * ha abierto nunca, así que también sirve de "acabas de actualizar". El punto
 * del sello del sidebar sigue ahí, pero un punto se pierde: esto lo dice con
 * palabras y lleva a la página.
 */
export function WhatsNewBanner() {
  const { t } = useTranslation()
  const location = useLocation()
  const version = useServerVersion()
  const { unseen, markSeen } = useWhatsNewBadge(version)

  // En la propia página no pinta nada: se marca vista al montarla.
  if (!unseen || location.pathname === '/whatsnew') return null

  return (
    <div className="mb-4 flex items-center gap-3 rounded-xl border border-accent/30 bg-accent-soft px-4 py-3">
      <Sparkles className="h-4 w-4 shrink-0 text-accent" strokeWidth={1.75} aria-hidden="true" />
      <p className="min-w-0 flex-1 text-caption text-text-primary">
        {t('whatsnew.banner.text', { version })}
      </p>
      <Link
        to="/whatsnew"
        className="shrink-0 rounded-lg border border-accent/40 bg-surface px-3 py-1.5 text-caption font-semibold text-accent transition-colors hover:border-accent"
      >
        {t('whatsnew.banner.action')}
      </Link>
      <button
        type="button"
        onClick={markSeen}
        aria-label={t('whatsnew.banner.dismiss')}
        title={t('whatsnew.banner.dismiss')}
        className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-text-secondary transition-colors hover:bg-hover hover:text-text-primary"
      >
        <X className="h-4 w-4" strokeWidth={2} />
      </button>
    </div>
  )
}
