/**
 * UpdateReadiness — panel de pre-flight checks del updater (issue #160):
 * muestra cada check (disco, git, red, concurrencia) con su estado ok/fail y
 * un resumen ready/notReady. Lo usan UpdateBanner y UpdateCheckInline.
 */
import { useTranslation } from 'react-i18next'
import { CircleCheck, CircleX, ShieldCheck, ShieldAlert } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface ReadinessCheck {
  ok: boolean
  detail?: string
}

export interface UpdateReadiness {
  disk: ReadinessCheck
  git: ReadinessCheck
  network: ReadinessCheck
  concurrent: ReadinessCheck
  ready: boolean
}

const CHECKS = ['disk', 'git', 'network', 'concurrent'] as const

export function ReadinessPanel({
  readiness,
  compact,
  className,
}: {
  readiness: UpdateReadiness
  compact?: boolean
  className?: string
}) {
  const { t } = useTranslation()
  const ready = readiness.ready
  return (
    <div
      role="group"
      aria-label={t('update.checks.title')}
      className={cn(
        'flex flex-col gap-2 rounded-xl border border-border bg-surface p-3',
        compact && 'gap-1.5 p-2.5',
        className,
      )}
    >
      <div className="flex items-center gap-1.5">
        {ready ? (
          <ShieldCheck className="h-4 w-4 shrink-0 text-ok" strokeWidth={1.75} aria-hidden="true" />
        ) : (
          <ShieldAlert className="h-4 w-4 shrink-0 text-rose-500" strokeWidth={1.75} aria-hidden="true" />
        )}
        <span
          className={cn(
            'text-xs font-semibold',
            ready ? 'text-text-primary' : 'text-rose-500',
          )}
        >
          {ready ? t('update.checks.ready') : t('update.checks.notReady')}
        </span>
      </div>
      <ul className={cn('flex flex-col gap-1', compact && 'gap-0.5')}>
        {CHECKS.map((k) => {
          const c = readiness[k]
          return (
            <li key={k} className="flex items-center gap-1.5 text-caption">
              {c.ok ? (
                <CircleCheck className="h-3.5 w-3.5 shrink-0 text-ok" strokeWidth={2} aria-hidden="true" />
              ) : (
                <CircleX className="h-3.5 w-3.5 shrink-0 text-rose-500" strokeWidth={2} aria-hidden="true" />
              )}
              <span className="text-text-secondary">{t(`update.checks.${k}`)}</span>
              {c.detail && (
                <span className="truncate text-text-muted" title={c.detail}>
                  {c.detail}
                </span>
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}
