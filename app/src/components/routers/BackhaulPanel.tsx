import { Activity, Cable, Wifi } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Router } from '@/data/mock'
import { getRouterExtras } from '@/components/routers/routerExtras'
import type { RouterExtras } from '@/components/routers/routerExtras'
import { EMPTY_EXTRAS, useNetPulse } from '@/data/DataProvider'

/** ④ variante OpenWrt — fila compacta a ancho completo: backhaul, latencia al
 * gateway (con su IP REAL, no el literal del canon demo), tráfico y último
 * reinicio repartidos en columnas (decisión 28-Ago-2026). */
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

  const cell = 'flex min-w-0 flex-col gap-0.5'
  const label = 'text-[10px] font-medium uppercase tracking-[0.06em] text-text-muted'
  const value = 'truncate font-mono text-mono-sm font-semibold text-text-primary'

  return (
    <section
      className="grid grid-cols-2 items-center gap-x-6 gap-y-3 rounded-2xl border border-border bg-surface px-4 py-3.5 md:grid-cols-4 md:px-5 lg:col-span-12"
      aria-label="Backhaul y latencia al gateway"
    >
      <div className={cell}>
        <span className={`${label} flex items-center gap-1.5`}>
          <Icon className="h-3.5 w-3.5 text-accent" strokeWidth={1.75} aria-hidden="true" />
          Backhaul
        </span>
        <span className={value}>{backhaul.headline}</span>
        <span className="truncate text-caption text-text-muted">
          {wireless ? t('routerDetail.backhaul.wirelessLink') : t('routerDetail.backhaul.wiredLink')}
        </span>
      </div>
      <div className={cell}>
        <span className={`${label} flex items-center gap-1.5`}>
          <Activity className="h-3.5 w-3.5 text-ok" strokeWidth={1.75} aria-hidden="true" />
          {t('routers.gatewayLatency')}
        </span>
        <span className={value}>{latency}</span>
        <span className="truncate text-caption text-text-muted">
          {gwIp ? t('routerDetail.latency.toHost', { host: gwIp }) : ''}
        </span>
      </div>
      <div className={cell}>
        <span className={label}>{t('devices.colTraffic')}</span>
        <span className={value}>↓ {ex.trafficNow.toFixed(1)} Mbps</span>
        <span className="text-caption text-text-muted">WAN · LAN</span>
      </div>
      <div className={cell}>
        <span className={label}>{t('routerDetail.info.lastReboot')}</span>
        <span className={value}>{ex.lastReboot || '—'}</span>
        <span className="text-caption text-text-muted">{ex.soc}</span>
      </div>
    </section>
  )
}
