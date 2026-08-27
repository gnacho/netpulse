import { Activity, Cable, Wifi } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Router } from '@/data/mock'
import { getRouterExtras } from '@/components/routers/routerExtras'
import type { RouterExtras } from '@/components/routers/routerExtras'
import { EMPTY_EXTRAS, useNetPulse } from '@/data/DataProvider'

/** ④ variante OpenWrt — Backhaul + latencia al gateway en UNA fila compacta
 * (decisión 28-Ago-2026: sin gráficas; la IP del caption es la REAL del
 * gateway de la flota, no un literal del canon demo). */
export function BackhaulPanel({ router, extras }: { router: Router; extras?: RouterExtras }) {
  const { t } = useTranslation()
  const { isDemo, routers } = useNetPulse()
  const ex = extras ?? (isDemo ? getRouterExtras(router.id) : EMPTY_EXTRAS)
  const backhaul = ex.backhaul
  if (!backhaul) return null

  const wireless = backhaul.kind === 'wireless'
  const Icon = wireless ? Wifi : Cable
  const gwIp = routers.find((r) => r.roleBadge === 'Principal')?.ip
  const latency = backhaul.latencyMs != null ? `${Math.round(backhaul.latencyMs)} ms` : '—'

  return (
    <section
      className="flex flex-wrap items-center gap-x-5 gap-y-1.5 rounded-2xl border border-border bg-surface px-4 py-3 md:px-5 lg:col-span-12"
      aria-label="Backhaul y latencia al gateway"
    >
      <span className="flex min-w-0 items-center gap-2.5">
        <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-accent-soft text-accent">
          <Icon className="h-4 w-4" strokeWidth={1.75} aria-hidden="true" />
        </span>
        <span className="min-w-0">
          <span className="block truncate font-mono text-mono-sm font-semibold text-text-primary">
            {backhaul.headline}
          </span>
          <span className="block text-caption text-text-muted">
            {wireless ? t('routerDetail.backhaul.wirelessLink') : t('routerDetail.backhaul.wiredLink')}
          </span>
        </span>
      </span>
      <span className="hidden h-6 w-px bg-border sm:block" aria-hidden="true" />
      <span className="flex items-center gap-2">
        <Activity className="h-4 w-4 text-ok" strokeWidth={1.75} aria-hidden="true" />
        <span className="font-mono text-mono-sm font-semibold text-text-primary">{latency}</span>
        <span className="text-caption text-text-muted">
          {gwIp ? t('routerDetail.latency.toHost', { host: gwIp }) : t('routers.gatewayLatency')}
        </span>
      </span>
    </section>
  )
}
