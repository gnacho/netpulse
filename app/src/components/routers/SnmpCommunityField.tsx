import { useEffect, useRef, useState } from 'react'
import { ChevronDown } from 'lucide-react'

import { cn } from '@/lib/utils'

/**
 * #2079: the SNMP community is an arbitrary string defined on the device, so it
 * cannot be a closed select. This field keeps it free-text but opens its own
 * suggestion list (not the browser datalist) with the values seen most often.
 */
export const SNMP_COMMUNITY_SUGGESTIONS = ['public', 'private', 'monitor', 'monitor-ro']

interface Props {
  id: string
  value: string
  onChange: (value: string) => void
  ariaLabel: string
  placeholder?: string
  suggestions?: string[]
}

export default function SnmpCommunityField({
  id,
  value,
  onChange,
  ariaLabel,
  placeholder = 'public',
  suggestions = SNMP_COMMUNITY_SUGGESTIONS,
}: Props) {
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const boxRef = useRef<HTMLDivElement>(null)

  const query = value.trim().toLowerCase()
  const filtered = query ? suggestions.filter((s) => s.toLowerCase().includes(query)) : suggestions

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onPointerDown)
    return () => document.removeEventListener('mousedown', onPointerDown)
  }, [open])

  const close = () => {
    setOpen(false)
    setActive(-1)
  }

  const pick = (option: string) => {
    onChange(option)
    close()
  }

  const onKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      if (!open) {
        setOpen(true)
        return
      }
      if (filtered.length === 0) return
      const step = event.key === 'ArrowDown' ? 1 : -1
      setActive((prev) => {
        const next = prev + step
        if (next < 0) return filtered.length - 1
        if (next >= filtered.length) return 0
        return next
      })
      return
    }
    if (event.key === 'Enter' && open && active >= 0) {
      const option = filtered[active]
      if (option) {
        event.preventDefault()
        pick(option)
      }
      return
    }
    if (event.key === 'Escape' && open) {
      event.preventDefault()
      event.stopPropagation()
      close()
    }
  }

  return (
    <div ref={boxRef} className="relative">
      <div className="flex items-stretch gap-1">
        <input
          id={id}
          type="text"
          value={value}
          onChange={(event) => {
            onChange(event.target.value)
            setOpen(true)
            setActive(-1)
          }}
          onFocus={() => setOpen(true)}
          onKeyDown={onKeyDown}
          placeholder={placeholder}
          aria-label={ariaLabel}
          role="combobox"
          aria-expanded={open}
          aria-controls={`${id}-suggestions`}
          aria-autocomplete="list"
          autoComplete="off"
          spellCheck={false}
          className="w-full rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
        />
        <button
          type="button"
          onClick={() => setOpen((prev) => !prev)}
          aria-label={ariaLabel}
          aria-expanded={open}
          aria-controls={`${id}-suggestions`}
          className="flex w-8 shrink-0 items-center justify-center rounded-lg border border-border bg-elevated text-text-muted hover:bg-hover hover:text-text-secondary"
        >
          <ChevronDown className={cn('h-3.5 w-3.5 transition-transform', open && 'rotate-180')} strokeWidth={1.75} />
        </button>
      </div>
      {open && filtered.length > 0 && (
        <ul
          id={`${id}-suggestions`}
          role="listbox"
          aria-label={ariaLabel}
          className="mt-1 overflow-hidden rounded-lg border border-border bg-elevated py-1"
        >
          {filtered.map((option, index) => (
            <li key={option} role="option" aria-selected={index === active}>
              <button
                type="button"
                onMouseEnter={() => setActive(index)}
                onClick={() => pick(option)}
                className={cn(
                  'block w-full px-3 py-1.5 text-left text-sm',
                  index === active ? 'bg-hover text-text-primary' : 'text-text-secondary',
                )}
              >
                {option}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
