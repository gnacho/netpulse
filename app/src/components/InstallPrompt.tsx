/**
 * NetPulse — InstallPrompt (issue #172)
 *
 * Snackbar descartable que sugiere instalar NetPulse como PWA tras el primer
 * login autenticado de cada usuario en este navegador. Sigue las mejores
 * prácticas de web.dev / Chrome DevRel:
 *
 *  - Solo se muestra cuando el navegador lo soporta: `beforeinstallprompt`
 *    disparado (Chromium: Chrome/Edge/Android) o iOS Safari (instrucciones
 *    manuales de "Añadir a pantalla de inicio").
 *  - Nunca cuando la app ya corre en modo instalado
 *    (`display-mode: standalone` / `navigator.standalone`).
 *  - `preventDefault()` sobre `beforeinstallprompt`: lanzamos nosotros el
 *    prompt (evita duplicar la mini-infobar nativa de Chrome).
 *  - Descarte recordado por usuario (localStorage): el botón "Ahora no" o un
 *    rechazo del diálogo nativo evita re-promocionar.
 *  - Auto-cierre tras ~8 s si se ignora; no se vuelve a mostrar en la misma
 *    sesión, sí en una visita futura (cambio de relación).
 */
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AnimatePresence, motion } from 'framer-motion'
import { Download } from 'lucide-react'
import { useAuth } from '@/data/AuthContext'

interface BeforeInstallPromptEvent extends Event {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

const AUTO_HIDE_MS = 8000
const SHOW_DELAY_MS = 1200

function isStandalone(): boolean {
  return (
    window.matchMedia('(display-mode: standalone)').matches ||
    (navigator as unknown as { standalone?: boolean }).standalone === true
  )
}

function isIOS(): boolean {
  return (
    /iPad|iPhone|iPod/.test(navigator.userAgent) ||
    (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
  )
}

export default function InstallPrompt() {
  const { t } = useTranslation()
  const auth = useAuth()
  const [deferred, setDeferred] = useState<BeforeInstallPromptEvent | null>(null)
  const [installed, setInstalled] = useState<boolean>(() => isStandalone())
  const [visible, setVisible] = useState(false)
  const shownRef = useRef(false)
  const hideTimer = useRef<number | undefined>(undefined)

  const dismissedKey = `netpulse-install-prompt-dismissed-${auth?.user ?? 'guest'}`

  // Captura el evento de instalación (Chromium) y lo que ocurra al instalar.
  useEffect(() => {
    const onPrompt = (e: Event) => {
      e.preventDefault()
      setDeferred(e as BeforeInstallPromptEvent)
    }
    const onInstalled = () => {
      setInstalled(true)
      setVisible(false)
      localStorage.setItem(dismissedKey, '1')
    }
    window.addEventListener('beforeinstallprompt', onPrompt)
    window.addEventListener('appinstalled', onInstalled)
    return () => {
      window.removeEventListener('beforeinstallprompt', onPrompt)
      window.removeEventListener('appinstalled', onInstalled)
      window.clearTimeout(hideTimer.current)
    }
    // dismissedKey cambia de sesión en sesión; el listener se re-registra para
    // el usuario correcto.
  }, [dismissedKey])

  // Muestra el snackbar tras el primer login: con soporte real, sin instalar
  // y sin descarte previo. Una vez por sesión de navegación.
  const canInstall = deferred !== null || isIOS()
  useEffect(() => {
    if (!visible || installed) return
    hideTimer.current = window.setTimeout(() => setVisible(false), AUTO_HIDE_MS)
    return () => window.clearTimeout(hideTimer.current)
  }, [visible, installed])

  useEffect(() => {
    if (installed || shownRef.current || localStorage.getItem(dismissedKey) === '1' || !canInstall) return
    shownRef.current = true
    const delay = window.setTimeout(() => setVisible(true), SHOW_DELAY_MS)
    return () => window.clearTimeout(delay)
  }, [canInstall, installed, dismissedKey])

  const install = async () => {
    if (!deferred) return
    setVisible(false)
    await deferred.prompt()
    const { outcome } = await deferred.userChoice
    // Instalado, o rechazado el diálogo nativo: no re-promocionar.
    localStorage.setItem(dismissedKey, '1')
    setDeferred(null)
    if (outcome === 'accepted') setInstalled(true)
  }

  const dismiss = () => {
    localStorage.setItem(dismissedKey, '1')
    setVisible(false)
  }

  return (
    <AnimatePresence>
      {visible && !installed && (
        <motion.div
          role="dialog"
          aria-label={t('installPrompt.title')}
          initial={{ opacity: 0, y: 16 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: 8 }}
          transition={{ duration: 0.22, ease: 'easeOut' }}
          className="fixed bottom-20 left-1/2 z-50 w-[calc(100%-2rem)] max-w-sm -translate-x-1/2 md:bottom-6"
        >
          <div className="rounded-xl border border-border-strong bg-elevated p-4 shadow-xl">
            <div className="flex items-start gap-3">
              <span className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-accent/15 text-accent">
                <Download className="h-4 w-4" strokeWidth={2} />
              </span>
              <div className="min-w-0">
                <p className="text-sm font-semibold text-text-primary">{t('installPrompt.title')}</p>
                <p className="mt-0.5 text-caption text-text-muted">
                  {isIOS() ? t('installPrompt.iosDesc') : t('installPrompt.desc')}
                </p>
              </div>
            </div>
            <div className="mt-3 flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={dismiss}
                className="flex h-8 items-center rounded-lg px-3 text-caption font-medium text-text-secondary transition-colors hover:bg-hover hover:text-text-primary"
              >
                {t('installPrompt.later')}
              </button>
              {deferred && (
                <button
                  type="button"
                  onClick={() => void install()}
                  className="flex h-8 items-center gap-1.5 rounded-lg bg-accent px-3 text-caption font-semibold text-canvas transition-opacity hover:opacity-90"
                >
                  <Download className="h-3.5 w-3.5" strokeWidth={2} />
                  {t('installPrompt.install')}
                </button>
              )}
            </div>
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  )
}
