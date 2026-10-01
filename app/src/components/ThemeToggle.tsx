import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Moon, Sun } from 'lucide-react'
import { cn } from '@/lib/utils'
import { readMode, resolveLight, setMode, THEME_CHANGE_EVENT } from '@/lib/theme-boot'

/**
 * Toggle de tema claro/oscuro (#980). Delega en theme-boot.setMode, que
 * persiste `netpulse-theme-mode`, conmuta las clases .light/.dark Y re-aplica
 * la paleta como variables inline en <html> (sin esto ultimo las vars inline
 * oscuras del boot ganaban a la regla `html.light` del stylesheet y el click
 * no cambiaba ningun color). Se sincroniza entre instancias via
 * THEME_CHANGE_EVENT.
 */
export function ThemeToggle({ className }: { className?: string }) {
  const { t } = useTranslation()
  const [light, setLight] = useState<boolean>(() => resolveLight(readMode()))

  useEffect(() => {
    const sync = () => setLight(resolveLight(readMode()))
    window.addEventListener(THEME_CHANGE_EVENT, sync)
    return () => window.removeEventListener(THEME_CHANGE_EVENT, sync)
  }, [])

  const toggle = useCallback(() => {
    const next = !light
    setMode(next ? 'light' : 'dark')
    setLight(next)
  }, [light])

  return (
    <button
      type="button"
      onClick={toggle}
      aria-label={light ? t('nav.themeToDark') : t('nav.themeToLight')}
      title={light ? t('nav.themeToDark') : t('nav.themeToLight')}
      className={cn(
        'flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-elevated text-text-secondary',
        'transition-colors duration-150 hover:border-accent/40 hover:text-accent',
        className,
      )}
    >
      {light ? <Moon className="h-4 w-4" strokeWidth={1.75} /> : <Sun className="h-4 w-4" strokeWidth={1.75} />}
    </button>
  )
}
