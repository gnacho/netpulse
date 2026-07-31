import { cn } from '@/lib/utils'

export type PillTone = 'ok' | 'warn' | 'danger' | 'accent' | 'tunnel' | 'info' | 'muted'

const TONE_STYLES: Record<PillTone, { dot: string; text: string; bg: string }> = {
  ok: { dot: 'bg-ok', text: 'text-ok', bg: 'bg-ok/10' },
  warn: { dot: 'bg-warn', text: 'text-warn', bg: 'bg-warn/10' },
  danger: { dot: 'bg-danger', text: 'text-danger', bg: 'bg-danger/10' },
  accent: { dot: 'bg-accent', text: 'text-accent', bg: 'bg-accent-soft' },
  tunnel: { dot: 'bg-tunnel', text: 'text-tunnel', bg: 'bg-tunnel/10' },
  info: { dot: 'bg-info', text: 'text-info', bg: 'bg-info/10' },
  muted: { dot: 'bg-text-muted', text: 'text-text-secondary', bg: 'bg-elevated' },
}

interface StatusPillProps {
  tone: PillTone
  label: string
  /** Dot pulsante (warn/danger, peers activos, EN VIVO) */
  pulse?: boolean
  className?: string
}

/** Chip con dot + texto — nunca solo color (design.md §13). */
export function StatusPill({ tone, label, pulse = false, className }: StatusPillProps) {
  const s = TONE_STYLES[tone]
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-caption font-semibold uppercase tracking-[0.06em]',
        s.bg,
        s.text,
        className,
      )}
    >
      <span className="relative flex h-1.5 w-1.5">
        {pulse && (
          <span className={cn('absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping-soft', s.dot)} />
        )}
        <span className={cn('relative inline-flex h-1.5 w-1.5 rounded-full', s.dot)} />
      </span>
      {label}
    </span>
  )
}
