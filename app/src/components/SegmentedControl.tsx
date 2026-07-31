import { cn } from '@/lib/utils'

interface SegmentedControlProps<T extends string> {
  options: readonly { value: T; label: string }[]
  value: T
  onChange: (value: T) => void
  size?: 'sm' | 'md'
  className?: string
  ariaLabel?: string
}

/** Segmented control estilo Tabs (design.md §10.8) — rangos temporales y filtros. */
export function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  size = 'md',
  className,
  ariaLabel,
}: SegmentedControlProps<T>) {
  return (
    <div
      role="tablist"
      aria-label={ariaLabel}
      className={cn(
        'inline-flex items-center gap-0.5 rounded-lg border border-border bg-elevated p-1',
        className,
      )}
    >
      {options.map((opt) => {
        const active = opt.value === value
        return (
          <button
            key={opt.value}
            role="tab"
            aria-selected={active}
            onClick={() => onChange(opt.value)}
            className={cn(
              'rounded-md font-medium transition-colors duration-150',
              size === 'sm' ? 'h-6 px-2 text-caption' : 'h-7 px-3 text-xs',
              active
                ? 'bg-accent-soft text-accent'
                : 'text-text-secondary hover:bg-hover hover:text-text-primary',
            )}
          >
            {opt.label}
          </button>
        )
      })}
    </div>
  )
}

export const TIME_RANGE_OPTIONS = [
  { value: '1h', label: '1h' },
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
] as const
