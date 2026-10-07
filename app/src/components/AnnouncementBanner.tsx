/**
 * AnnouncementBanner — franja de avisos externos (announcements.json del
 * repo, servida por /api/announcement): mismo estilo que el ribbon de
 * actualizar. Visible para cualquier sesión; se descarta por id y el
 * descarte persiste en localStorage.
 */
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { motion, AnimatePresence, useReducedMotion } from 'framer-motion'
import { ExternalLink, Megaphone, X } from 'lucide-react'

const DISMISS_KEY = 'netpulse.announcement.dismissed'
const POLL_MS = 6 * 60 * 60 * 1000

interface Announcement {
  id: string
  urgency?: string
  title: Record<string, string>
  body?: Record<string, string>
  url?: string
  urlLabel?: Record<string, string>
  goal?: { current: number; target: number }
}

const pick = (m: Record<string, string> | undefined, lang: string) =>
  (m && (m[lang] || m.en || m.es || '')) || ''

export function AnnouncementBanner() {
  const { t, i18n } = useTranslation()
  const reduce = useReducedMotion()
  const [a, setA] = useState<Announcement | null>(null)
  const [dismissed, setDismissed] = useState<string | null>(null)

  const fetchA = useCallback(async () => {
    try {
      const res = await fetch('/api/announcement')
      if (!res.ok) return
      const json = (await res.json()) as { active: boolean; announcement?: Announcement }
      setA(json.active && json.announcement ? json.announcement : null)
    } catch {
      /* sin aviso */
    }
  }, [])

  useEffect(() => {
    try {
      setDismissed(localStorage.getItem(DISMISS_KEY))
    } catch {
      /* sin localStorage: siempre visible */
    }
    void fetchA()
    const id = window.setInterval(() => void fetchA(), POLL_MS)
    return () => window.clearInterval(id)
  }, [fetchA])

  if (!a || a.id === dismissed) return null

  const lang = i18n.language?.startsWith('en') ? 'en' : 'es'
  const title = pick(a.title, lang)
  const body = pick(a.body, lang)
  const urlLabel = pick(a.urlLabel, lang) || t('announcement.link')
  const warn = a.urgency === 'warn'

  const dismiss = () => {
    setDismissed(a.id)
    try {
      localStorage.setItem(DISMISS_KEY, a.id)
    } catch {
      /* sin localStorage: se descarta hasta el refresco */
    }
  }

  return (
    <AnimatePresence>
      <motion.div
        initial={reduce ? false : { opacity: 0, y: -8 }}
        animate={{ opacity: 1, y: 0 }}
        exit={{ opacity: 0, y: -8 }}
        transition={{ duration: 0.25, ease: 'easeOut' }}
        role="status"
        className="mb-2"
      >
        <div
          className={`rounded-xl border px-4 py-3 ${
            warn ? 'border-warn/40 bg-warn/10' : 'border-accent/40 bg-accent-soft'
          }`}
        >
          <div className="flex items-start gap-3">
            <Megaphone className={`mt-1 h-4 w-4 shrink-0 ${warn ? 'text-warn' : 'text-accent'}`} strokeWidth={1.75} />
            <div className="min-w-0 flex-1">
              {title && <p className="text-[22px] font-bold leading-tight text-text-primary">{title}</p>}
              {body && <p className="mt-0.5 whitespace-pre-line text-lg leading-snug text-text-secondary">{body}</p>}
              {/* Meta de campaña (opcional): barra de progreso con el punto
                  de la reta, p.ej. estrellas de GitHub (85/100). */}
              {a.goal && a.goal.target > 0 && (
                <div className="mt-2.5 max-w-md">
                  <div className="flex items-baseline justify-between text-caption">
                    <span className="font-semibold text-text-primary">
                      {t('announcement.goal', { current: a.goal.current, target: a.goal.target })}
                    </span>
                    <span className="font-mono text-text-muted">{Math.round((a.goal.current / a.goal.target) * 100)}%</span>
                  </div>
                  <div
                    className="mt-1 h-2 overflow-hidden rounded-full bg-border/60"
                    role="progressbar"
                    aria-valuenow={a.goal.current}
                    aria-valuemin={0}
                    aria-valuemax={a.goal.target}
                  >
                    <div
                      className={`h-full rounded-full ${warn ? 'bg-warn' : 'bg-accent'}`}
                      style={{ width: `${Math.min(100, Math.max(0, (a.goal.current / a.goal.target) * 100))}%` }}
                    />
                  </div>
                </div>
              )}
              {/* Botón DEBAJO del texto, prominente: fondo acento y fuente
                  mayor (antes iba en línea, pequeño y neutro). */}
              {a.url && (
                <a
                  href={a.url}
                  target="_blank"
                  rel="noreferrer"
                  className={`mt-2.5 inline-flex items-center gap-2 rounded-lg px-4 py-2.5 text-sm font-semibold transition-colors duration-150 ${
                    warn
                      ? 'bg-warn text-canvas hover:bg-warn/90'
                      : 'bg-accent text-canvas hover:bg-accent/90'
                  }`}
                >
                  <ExternalLink className="h-4 w-4" strokeWidth={2} />
                  {urlLabel}
                </a>
              )}
            </div>
            <button
              type="button"
              onClick={dismiss}
              aria-label={t('announcement.dismiss')}
              className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-text-muted transition-colors hover:text-text-primary"
            >
              <X className="h-4 w-4" strokeWidth={1.75} />
            </button>
          </div>
        </div>
      </motion.div>
    </AnimatePresence>
  )
}
