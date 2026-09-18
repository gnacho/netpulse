import { Lock, Split } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { MultiWanInfo, MultiWanUplink } from '@/data/types'
import { SectionHeader } from '@/components/SectionHeader'
import { StatusPill } from '@/components/StatusPill'
import type { PillTone } from '@/components/StatusPill'
import { cn } from '@/lib/utils'

const MODE_LABEL: Record<string, string> = {
  off: 'routerDetail.multiWan.modeOff',
  failover: 'routerDetail.multiWan.modeFailover',
  balance: 'routerDetail.multiWan.modeBalance',
  custom: 'routerDetail.multiWan.modeCustom',
}

function modeTone(mode: string): PillTone {
  if (mode === 'failover' || mode === 'balance') return 'accent'
  if (mode === 'custom') return 'info'
  return 'muted'
}

/** The one-word verdict on a connection. The policy manager's own opinion
 *  wins when it has one: a line can be plugged in and up while the provider
 *  behind it is unreachable, and those are different problems. */
function state(u: MultiWanUplink): { key: string; tone: PillTone } {
  if (!u.up) return { key: 'routerDetail.multiWan.offline', tone: 'danger' }
  if (u.online === 'offline') return { key: 'routerDetail.multiWan.failed', tone: 'danger' }
  if (u.active) return { key: 'routerDetail.multiWan.active', tone: 'ok' }
  return { key: 'routerDetail.multiWan.standby', tone: 'muted' }
}

/** Share of traffic one connection carries. Deliberately not MetricBar: that
 *  turns red above 90%, and 90% of the traffic on one line is a setting, not
 *  a warning. Decorative — the figure is in the pill beside it. */
function ShareBar({ pct }: { pct: number }) {
  return (
    <div className="mt-1.5 h-1.5 w-full overflow-hidden rounded-full bg-border/50" aria-hidden="true">
      <div
        className="h-full rounded-full bg-accent transition-[width] duration-500"
        style={{ width: `${Math.min(100, Math.max(0, pct))}%` }}
      />
    </div>
  )
}

function UplinkRow({ u, balancing }: { u: MultiWanUplink; balancing: boolean }) {
  const { t } = useTranslation()
  const s = state(u)
  const where = [u.proto, u.port ? t('routerDetail.multiWan.viaPort', { port: u.port }) : '']
    .filter(Boolean)
    .join(' · ')
  return (
    <li className="py-2.5">
      <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
        <span
          className={cn(
            'h-2 w-2 shrink-0 rounded-full',
            s.tone === 'ok' ? 'bg-ok' : s.tone === 'danger' ? 'bg-danger' : 'bg-text-muted',
          )}
          aria-hidden="true"
        />
        <span className="font-medium text-text-primary">{u.name}</span>
        <span className="font-mono text-mono-sm text-text-secondary">
          {u.ip || t('routerDetail.multiWan.noAddress')}
        </span>
        <span className="text-caption text-text-muted">{where}</span>
        <span className="flex-1" />
        {u.metered && <StatusPill tone="warn" label={t('routerDetail.multiWan.metered')} />}
        {u.primary && <StatusPill tone="accent" label={t('routerDetail.multiWan.primary')} />}
        <StatusPill
          tone={s.tone}
          label={
            balancing && u.active && (u.sharePct ?? 0) > 0
              ? t('routerDetail.multiWan.activeShare', { pct: u.sharePct })
              : t(s.key)
          }
        />
      </div>
      {balancing && <ShareBar pct={u.sharePct ?? 0} />}
    </li>
  )
}

/**
 * The router's internet connections and which one is carrying traffic.
 *
 * Read-only on purpose: the router's own panel decides all of this, and two
 * places to change the same setting is how they end up disagreeing. What
 * this answers is the question the connection card above cannot — why the
 * address changed, and which line the house is on right now.
 */
export function MultiWanPanel({ info }: { info: MultiWanInfo }) {
  const { t } = useTranslation()
  // Defensive: the section comes from a router, and a router can report
  // whatever it likes. Anything short of two connections renders nothing.
  if (!info || (info.uplinks?.length ?? 0) < 2) return null
  const balancing = info.mode === 'balance'

  return (
    <section className="rounded-2xl border border-border bg-surface p-5 md:p-6 lg:col-span-12">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <Split className="h-4 w-4 text-text-muted" strokeWidth={1.75} aria-hidden="true" />
          <SectionHeader title={t('routerDetail.multiWan.title')} />
        </div>
        <StatusPill tone={modeTone(info.mode)} label={t(MODE_LABEL[info.mode] ?? 'routerDetail.multiWan.modeOff')} />
      </div>

      <p className="mt-1 text-caption text-text-muted">{t(`routerDetail.multiWan.hint.${info.mode}`)}</p>

      <ul className="mt-3 divide-y divide-border/60">
        {info.uplinks.map((u) => (
          <UplinkRow key={u.name} u={u} balancing={balancing} />
        ))}
      </ul>

      {/* Without this the percentages read as a per-device mix, and the
          first thing anyone does is check their own address twice. */}
      {balancing && info.sticky && (
        <p className="mt-3 text-caption text-text-muted">{t('routerDetail.multiWan.stickyNote')}</p>
      )}

      <p className="mt-3 flex items-center gap-1.5 text-caption text-text-muted">
        <Lock className="h-3 w-3 shrink-0" strokeWidth={1.75} aria-hidden="true" />
        {t('routerDetail.multiWan.source')}
      </p>
    </section>
  )
}
