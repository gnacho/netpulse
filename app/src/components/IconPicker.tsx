import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, Search } from 'lucide-react'
import { ALLOWED_ICONS, ICON_CATEGORIES, ICON_OVERRIDES } from '@/components/DeviceRow'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

interface IconPickerProps {
  value: string
  onChange: (icon: string) => void
  disabled?: boolean
  columns?: 5 | 6
}

/** Selector de iconos compartido: búsqueda sobre una rejilla por categorías.
    Las cabeceras plegan su rejilla (la primera categoría y la que contiene la
    selección actual empiezan abiertas), y al escribir se aplana a las
    coincidencias de todas las categorías. Auto es la primera casilla de la
    primera categoría. Sustituye las dos rejillas inline que duplicaban este
    marcado en DeviceEditSheet y OnboardingIntake. */
export function IconPicker({ value, onChange, disabled, columns = 5 }: IconPickerProps) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const q = query.trim().toLowerCase()
  const gridCols = columns === 6 ? 'grid-cols-6' : 'grid-cols-5'

  const label = (n: string) => t(`devices.edit.iconNames.${n}`, { defaultValue: n })
  const selectable = useMemo(() => ALLOWED_ICONS.filter((n) => n !== 'help-circle'), [])

  const [collapsed, setCollapsed] = useState<Record<string, boolean>>(() => {
    const firstId = ICON_CATEGORIES[0]?.id ?? 'general'
    const owner = ICON_CATEGORIES.find((c) => c.icons.includes(value))?.id ?? firstId
    return Object.fromEntries(
      ICON_CATEGORIES.map((c) => [c.id, c.id !== owner && c.id !== firstId]),
    )
  })

  const cell = (n: string) => {
    const Icon = ICON_OVERRIDES[n]!
    const name = label(n)
    return (
      <button
        key={n}
        type="button"
        title={name}
        aria-label={name}
        aria-pressed={value === n}
        onClick={() => onChange(n)}
        disabled={disabled}
        className={cn(
          'flex h-11 items-center justify-center rounded-lg border transition-colors hover:bg-hover',
          value === n ? 'border-accent bg-accent-soft text-accent' : 'border-border bg-elevated text-text-secondary'
        )}
      >
        <Icon className="h-5 w-5" strokeWidth={1.75} />
      </button>
    )
  }

  const autoCell = (
    <button
      type="button"
      title={t('devices.edit.auto')}
      aria-label={t('devices.edit.auto')}
      aria-pressed={value === ''}
      onClick={() => onChange('')}
      disabled={disabled}
      className={cn(
        'flex h-11 items-center justify-center rounded-lg border text-xs font-medium transition-colors hover:bg-hover',
        value === '' ? 'border-accent bg-accent-soft text-accent' : 'border-border bg-elevated text-text-secondary'
      )}
    >
      {t('devices.edit.auto')}
    </button>
  )

  const matches = q
    ? selectable.filter((n) => n.toLowerCase().includes(q) || label(n).toLowerCase().includes(q))
    : []

  return (
    <div className="space-y-2">
      <div className="relative">
        <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-text-muted" strokeWidth={1.75} />
        <Input
          type="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t('devices.edit.iconSearch')}
          aria-label={t('devices.edit.iconSearch')}
          disabled={disabled}
          className="h-9 rounded-lg border-border bg-elevated pl-9 text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent/50"
        />
      </div>

      <div className="max-h-64 space-y-1.5 overflow-y-auto pr-1">
        {q ? (
          matches.length > 0 ? (
            <div className={cn('grid gap-2', gridCols)}>{matches.map(cell)}</div>
          ) : (
            <p className="py-4 text-center text-caption text-text-muted">{t('devices.edit.iconNoResults')}</p>
          )
        ) : (
          ICON_CATEGORIES.map((cat, i) => {
            const icons = cat.icons.filter((n) => selectable.includes(n))
            if (icons.length === 0) return null
            const open = !collapsed[cat.id]
            return (
              <div key={cat.id} className="space-y-1.5">
                <button
                  type="button"
                  onClick={() => setCollapsed((s) => ({ ...s, [cat.id]: !s[cat.id] }))}
                  aria-expanded={open}
                  className="flex w-full items-center justify-between rounded-md px-1 py-0.5 text-[11px] font-medium uppercase tracking-wide text-text-muted transition-colors hover:bg-hover hover:text-text-secondary"
                >
                  <span>{t(`devices.edit.iconCategories.${cat.id}`, { defaultValue: cat.id })}</span>
                  <span className="flex items-center gap-1">
                    {!open && <span className="text-[10px] normal-case">{icons.length}</span>}
                    <ChevronDown
                      className={cn('h-3.5 w-3.5 transition-transform', !open && '-rotate-90')}
                      strokeWidth={1.75}
                    />
                  </span>
                </button>
                {open && (
                  <div className={cn('grid gap-2', gridCols)}>
                    {i === 0 && autoCell}
                    {icons.map(cell)}
                  </div>
                )}
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}
