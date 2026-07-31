import { Link } from 'react-router-dom'
import { ExternalLink, KeyRound, Laptop, ShieldCheck, Smartphone, Tablet, Waypoints } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { relTime } from '@/i18n'
import { fmtEs, fmtInt } from '@/data/mock'
import type { PeerType, WGPeer } from '@/data/mock'
import { useNetPulse } from '@/data/DataProvider'
import { CountUp } from '@/components/CountUp'
import { ServicePanel } from '@/components/ServicePanel'
import { StatusPill } from '@/components/StatusPill'
import { useDashboard } from '@/hooks/useDashboard'

// ---------------------------------------------------------------------------
// Mini donut AdGuard (88px): bloqueado emerald vs permitido --border
// ---------------------------------------------------------------------------

function MiniDonut({ pct }: { pct: number }) {
  const { t } = useTranslation()
  const size = 88
  const stroke = 9
  const r = (size - stroke) / 2
  const c = size / 2
  return (
    <div className="relative" style={{ width: size, height: size }} role="img" aria-label={t('home.services.blockedAria', { pct: fmtEs(pct) })}>
      <svg width={size} height={size} className="-rotate-90">
        <circle cx={c} cy={c} r={r} fill="none" stroke="rgb(var(--border))" strokeWidth={stroke} />
        <motion.circle
          cx={c}
          cy={c}
          r={r}
          fill="none"
          stroke="#34D399"
          strokeWidth={stroke}
          strokeLinecap="round"
          pathLength={1}
          strokeDasharray="1 1"
          initial={{ pathLength: 0 }}
          animate={{ pathLength: pct / 100 }}
          transition={{ duration: 0.8, ease: 'easeOut', delay: 0.3 }}
        />
      </svg>
      <span className="absolute inset-0 flex items-center justify-center font-mono text-caption font-semibold text-text-primary">
        {fmtEs(pct)}%
      </span>
    </div>
  )
}

const PEER_ICONS: Record<PeerType, LucideIcon> = {
  movil: Smartphone,
  portatil: Laptop,
  tablet: Tablet,
  sitio: Waypoints,
}

function PeerRow({ peer, index }: { peer: WGPeer; index: number }) {
  const { t } = useTranslation()
  const Icon = PEER_ICONS[peer.type]
  return (
    <motion.div
      initial={{ opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3, delay: 0.2 + index * 0.07 }}
      className="flex items-center gap-3 rounded-xl px-2 py-2 transition-colors hover:bg-hover"
    >
      <span className="relative flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-tunnel/10 font-display text-sm font-semibold text-tunnel">
        {peer.name.charAt(0)}
        <Icon className="absolute -bottom-0.5 -right-0.5 h-3.5 w-3.5 rounded-full bg-surface p-0.5 text-text-secondary" strokeWidth={1.75} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium text-text-primary">{peer.name}</div>
        <div className="flex items-center gap-1.5 text-caption text-text-muted">
          <span className="relative flex h-1.5 w-1.5">
            <span className="absolute inline-flex h-full w-full rounded-full bg-ok opacity-75 animate-ping-soft" />
            <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-ok" />
          </span>
          {t('home.services.handshake', { time: relTime(peer.lastHandshake) })}
        </div>
      </div>
      <span className="shrink-0 font-mono text-mono-sm text-text-secondary">↓ {peer.rx}</span>
    </motion.div>
  )
}

/** ③a Tarjeta AdGuard Home (home.md §③) */
export function AdGuardCard({ index = 0 }: { index?: number }) {
  const { t } = useTranslation()
  const { refreshKey } = useDashboard()
  const { adguard } = useNetPulse()
  const maxBlocked = Math.max(...adguard.topBlocked.map((d) => d.count))

  return (
      <ServicePanel
        icon={ShieldCheck}
        tone="ok"
        title="AdGuard Home"
        status={
          adguard.status === 'active' ? (
            <StatusPill tone="ok" label={t('common.active')} />
          ) : (
            <StatusPill tone="muted" label={t('home.services.adguardInactive')} />
          )
        }
        index={index}
        className="flex-1"
        footer={
          adguard.host ? (
            <a
              href={`http://${adguard.host}:${adguard.port}`}
              target="_blank"
              rel="noreferrer"
              className="group inline-flex items-center gap-1.5 text-caption font-semibold text-text-secondary transition-colors hover:text-accent"
            >
              {t('home.services.openPanel')}
              <ExternalLink className="h-3.5 w-3.5 transition-transform duration-150 group-hover:translate-x-0.5" strokeWidth={1.75} />
            </a>
          ) : undefined
        }
      >
        {adguard.status !== 'active' ? (
          <p className="rounded-xl bg-elevated px-3.5 py-3 text-caption leading-relaxed text-text-muted">
            {t('home.services.adguardNoData')}
          </p>
        ) : (
          <>
        <div className="flex items-center gap-4">
          <div>
            <div className="font-mono text-stat text-ok">
              <CountUp value={adguard.blockedPct} decimals={1} nonce={refreshKey} /> %
            </div>
            <div className="mt-1 text-caption text-text-muted">
              {t('home.services.queriesOf', { blocked: fmtInt(adguard.blocked24h), total: fmtInt(adguard.queries24h) })}
            </div>
          </div>
          <div className="ml-auto">
            <MiniDonut pct={adguard.blockedPct} />
          </div>
        </div>

        <div className="mt-4 grid grid-cols-2 gap-3">
          <div className="rounded-xl bg-elevated px-3 py-2">
            <div className="text-label uppercase text-text-muted">{t('home.services.trackers')}</div>
            <div className="mt-0.5 font-mono text-mono-sm font-semibold text-text-primary">
              <CountUp value={adguard.trackersBlocked} nonce={refreshKey} />
            </div>
          </div>
          <div className="rounded-xl bg-elevated px-3 py-2">
            <div className="text-label uppercase text-text-muted">{t('home.services.avgDns')}</div>
            <div className="mt-0.5 font-mono text-mono-sm font-semibold text-text-primary">
              <CountUp value={adguard.dnsLatencyMs} nonce={refreshKey} /> ms
            </div>
          </div>
        </div>

        <div className="mt-4 space-y-2">
          {adguard.topBlocked.slice(0, 3).map((d, i) => (
            <div key={d.domain} className="flex items-center gap-2">
              <span className="w-36 truncate font-mono text-mono-sm text-text-secondary">{d.domain}</span>
              <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-border/50">
                <motion.div
                  className="h-full rounded-full bg-ok"
                  initial={{ width: 0 }}
                  animate={{ width: `${(d.count / maxBlocked) * 100}%` }}
                  transition={{ duration: 0.6, ease: 'easeOut', delay: 0.3 + i * 0.06 }}
                />
              </div>
              <span className="w-10 text-right font-mono text-mono-sm text-text-primary">{fmtInt(d.count)}</span>
            </div>
          ))}
        </div>
          </>
        )}
      </ServicePanel>
  )
}

/** ③b Tarjeta WireGuard (home.md §③) */
export function WireGuardCard({ index = 1 }: { index?: number }) {
  const { t } = useTranslation()
  const { refreshKey } = useDashboard()
  const { wireguard, routers } = useNetPulse()
  const gwId = routers.find((r) => r.roleBadge === 'Principal')?.id ?? routers[0]?.id ?? ''
  const activePeers = wireguard.peers.filter((p) => p.active)
  const inactivePeers = wireguard.peers.filter((p) => !p.active)

  return (
      <ServicePanel
        icon={KeyRound}
        tone="tunnel"
        title="WireGuard"
        status={<StatusPill tone="tunnel" label={t('home.services.activePeers', { count: activePeers.length })} />}
        index={index}
        className="flex-1"
        footer={
          <Link
            to={`/routers/${gwId}#wireguard`}
            className="group inline-flex items-center gap-1.5 text-caption font-semibold text-text-secondary transition-colors hover:text-tunnel"
          >
            {t('home.services.viewTunnel')}
            <span className="transition-transform duration-150 group-hover:translate-x-0.5">→</span>
          </Link>
        }
      >
        <div className="grid grid-cols-3 gap-3">
          <div>
            <div className="font-mono text-lg font-semibold text-text-primary">
              <CountUp value={wireguard.peers.length} nonce={refreshKey} />
            </div>
            <div className="text-caption text-text-muted">{t('home.services.peersConfigured')}</div>
          </div>
          <div>
            <div className="font-mono text-lg font-semibold text-ok">
              <CountUp value={activePeers.length} nonce={refreshKey} />
            </div>
            <div className="text-caption text-text-muted">{t('home.services.connected')}</div>
          </div>
          <div>
            <div className="font-mono text-mono-sm font-semibold text-text-primary">{wireguard.subnet}</div>
            <div className="text-caption text-text-muted">{t('home.services.tunnelIface', { iface: wireguard.interface })}</div>
          </div>
        </div>

        <div className="mt-3 space-y-1">
          {activePeers.map((p, i) => (
            <PeerRow key={p.id} peer={p} index={i} />
          ))}
        </div>

        <div className="mt-3 flex items-center gap-2.5 px-2">
          <div className="flex -space-x-2">
            {inactivePeers.map((p) => (
              <span
                key={p.id}
                title={`${p.name} · ${relTime(p.lastHandshake)}`}
                className="flex h-7 w-7 items-center justify-center rounded-full border-2 border-surface bg-elevated font-display text-[11px] font-semibold text-text-muted"
              >
                {p.name.charAt(0)}
              </span>
            ))}
          </div>
          <span className="text-caption text-text-muted">{t('home.services.inactive', { count: inactivePeers.length })}</span>
        </div>
      </ServicePanel>
  )
}
