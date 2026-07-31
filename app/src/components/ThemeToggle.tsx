import { useCallback, useEffect, useState } from 'react'
import { Moon, Sun } from 'lucide-react'
import { cn } from '@/lib/utils'

const STORAGE_KEY = 'netpulse-theme'

function applyTheme(light: boolean) {
  const el = document.documentElement
  el.classList.toggle('light', light)
  el.classList.toggle('dark', !light)
}

/** Toggle de tema dark-first (default oscuro; .light para claro). Persiste en localStorage. */
export function ThemeToggle({ className }: { className?: string }) {
  const [light, setLight] = useState<boolean>(() => {
    if (typeof window === 'undefined') return false
    return localStorage.getItem(STORAGE_KEY) === 'light'
  })

  useEffect(() => {
    applyTheme(light)
  }, [light])

  const toggle = useCallback(() => {
    setLight((prev) => {
      const next = !prev
      localStorage.setItem(STORAGE_KEY, next ? 'light' : 'dark')
      return next
    })
  }, [])

  return (
    <button
      type="button"
      onClick={toggle}
      aria-label={light ? 'Cambiar a tema oscuro' : 'Cambiar a tema claro'}
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
