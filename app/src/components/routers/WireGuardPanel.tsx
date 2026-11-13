import { useState } from 'react'
import { ChevronDown, KeyRound, Laptop, Network, Settings2, Smartphone, Tablet } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { relTime } from '@/i18n'
import type { PeerType, WGPeer } from '@/data/mock'
import { useNetPulse } from '@/data/DataProvider'
import { StatusPill } from '@/components/StatusPill'
import { Switch } from '@/components/ui/switch'
import { wgPeerExtras } from '@/components/routers/routerExtras'
import { cn } from '@/lib/utils'

const PEER_ICONS: Record<PeerType, LucideIcon> = {
  movil: Smartphone,
  portatil: Laptop,
  tablet: Tablet,
  sitio: Network,
  desconocido: Network,
}

/**
 * #1886: preferencia de la tarjeta, guardada en el navegador como el
 * intervalo de refresco (`netpulse-refresh`). Vive aquí y no en el servidor
 * porque solo decide qué se lista en esta vista. Sin localStorage o con un
 * valor raro, gana el defecto: los desconectados quedan ocultos.
 */
const SHOW_DISCONNECTED_KEY = 'netpulse-wireguard-show-disconnected'

function readShowDisconnected(): boolean {
  try {
    return localStorage.getItem(SHOW_DISCONNECTED_KEY) === '1'
  } catch {
    return false
  }
}

function writeShowDisconnected(value: boolean) {
  try {
    localStorage.setItem(SHOW_DISCONNECTED_KEY, value ? '1' : '0')
  } catch {
    /* sin localStorage: la preferencia vive solo en el estado */
  }
}

function PeerRow({ peer, index }: { peer: WGPeer; index: number }) {
  const { t } = useTranslation()
  const { setPeerUrgent } = useNetPulse()
  const [open, setOpen] = useState(false)
  const reduce = useReducedMotion()
  const Icon = PEER_ICONS[peer.type] ?? Smartphone
  const extra = wgPeerExtras[peer.id]

  return (
    <motion.div
      initial={reduce ? false : { opacity: 0, x: -10 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.3, ease: 'easeOut', delay: 0.1 + index * 0.07 }}
      className={cn(!peer.active && 'opacity-65')}
    >
      <div className="flex items-center gap-1">
        <button
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          className="flex min-w-0 flex-1 items-center gap-3 rounded-xl px-2.5 py-2.5 text-left transition-colors hover:bg-hover"
        >
          {/* Avatar con inicial + icono de tipo superpuesto */}
          <span className="relative shrink-0">
            <span
              className={cn(
                'flex h-10 w-10 items-center justify-center rounded-full font-display text-sm font-bold',
                peer.active ? 'bg-gradient-to-br from-tunnel to-accent text-canvas' : 'bg-elevated text-text-muted',
              )}
            >
              {peer.name.charAt(0)}
            </span>
            <span className="absolute -bottom-0.5 -right-0.5 flex h-4 w-4 items-center justify-center rounded-full border border-surface bg-elevated text-text-secondary">
              <Icon className="h-2.5 w-2.5" strokeWidth={1.75} />
            </span>
            {/* #1441: urgencia sin depender del color (glifo "!" + aria-label del switch) */}
            {peer.urgent && (
              <span
                aria-hidden
                className="absolute -right-1.5 -top-1.5 flex h-4 w-4 items-center justify-center rounded-full border border-surface bg-surface text-[9px] font-bold text-warn"
              >
                !
              </span>
            )}
          </span>

          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm font-medium text-text-primary">{peer.name}</span>
            <span className="flex items-center gap-1.5">
              <span className="relative flex h-1.5 w-1.5">
                {peer.active && (
                  <span className="absolute inline-flex h-full w-full rounded-full bg-ok opacity-75 animate-ping-soft" />
                )}
                <span className={cn('relative inline-flex h-1.5 w-1.5 rounded-full', peer.active ? 'bg-ok' : 'bg-text-muted')} />
              </span>
              <span className="font-mono text-caption text-text-muted">
                {peer.tunnelIp} · {relTime(peer.lastHandshake)}
              </span>
            </span>
          </span>

          <span className="hidden shrink-0 text-right font-mono text-caption text-text-secondary sm:block">
            ↓ {peer.rx} ↑ {peer.tx}
          </span>
          <ChevronDown className={cn('h-4 w-4 shrink-0 text-text-muted transition-transform', open && 'rotate-180')} strokeWidth={1.75} />
        </button>
        <Switch
          checked={peer.urgent}
          onCheckedChange={(checked) => setPeerUrgent(peer.key, checked)}
          aria-label={t('topology.peer.urgentAria', { name: peer.name })}
          className="mr-1 shrink-0"
        />
      </div>

      <AnimatePresence initial={false}>
        {open && extra && (
          <motion.div
            initial={reduce ? false : { height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={reduce ? { opacity: 0 } : { height: 0, opacity: 0 }}
            transition={{ duration: 0.25, ease: 'easeOut' }}
            className="overflow-hidden"
          >
            <dl className="mx-2.5 mb-2 grid grid-cols-1 gap-x-4 gap-y-1.5 rounded-lg bg-elevated/60 px-3.5 py-3 sm:grid-cols-3">
              {[
                ['Endpoint', extra.endpoint],
                ['Allowed IPs', extra.allowedIps],
                [t('routerDetail.wireguard.lastIp'), extra.lastIp],
              ].map(([k, v]) => (
                <div key={k}>
                  <dt className="text-[10px] font-medium uppercase tracking-[0.06em] text-text-muted">{k}</dt>
                  <dd className="font-mono text-mono-sm text-text-primary">{v}</dd>
                </div>
              ))}
            </dl>
          </motion.div>
        )}
      </AnimatePresence>
    </motion.div>
  )
}

/** ⑥ WireGuard panel (router-detail.md §⑥) — id="wireguard". */
export function WireGuardPanel() {
  const { t } = useTranslation()
  const { wireguard } = useNetPulse()
  const [configOpen, setConfigOpen] = useState(false)
  const [showDisconnected, setShowDisconnected] = useState(readShowDisconnected)
  const reduce = useReducedMotion()
  // #1441: activos primero y, dentro de cada grupo, los urgentes primero
  // (mismo criterio que los slots del mapa).
  const peers = [...wireguard.peers].sort(
    (a, b) => Number(b.active) - Number(a.active) || Number(b.urgent) - Number(a.urgent),
  )
  const connected = peers.filter((p) => p.active).length
  // #1886: la preferencia solo decide qué se lista. El contador de arriba
  // sigue dando el total real, filtro o no, para que ocultar no falsee la
  // lectura de la tarjeta.
  const visible = showDisconnected ? peers : peers.filter((p) => p.active)
  const hidden = peers.length - visible.length

  const toggleDisconnected = (checked: boolean) => {
    setShowDisconnected(checked)
    writeShowDisconnected(checked)
  }

  return (
    <section id="wireguard" className="scroll-mt-4 rounded-2xl border border-border bg-surface p-5 transition-shadow duration-500 md:p-6 lg:col-span-5">
      {/* Cabecera */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-tunnel/10 text-tunnel">
            <KeyRound className="h-[18px] w-[18px]" strokeWidth={1.75} />
          </div>
          <h3 className="font-display text-h2 text-text-primary">WireGuard</h3>
        </div>
        <div className="flex items-center gap-2">
          <StatusPill tone="tunnel" label={t('routerDetail.wireguard.serverActive')} />
          {/* #1886: chip de configuración de la tarjeta, abierto por defecto
              en el estado cerrado y solo con preferencias de esta vista. */}
          <button
            type="button"
            onClick={() => setConfigOpen((v) => !v)}
            aria-expanded={configOpen}
            className={cn(
              'inline-flex h-8 items-center gap-1.5 rounded-lg border px-2.5 text-caption font-medium transition-colors duration-150',
              configOpen
                ? 'border-accent/40 bg-accent-soft text-accent'
                : 'border-border bg-elevated text-text-secondary hover:border-accent/40 hover:text-accent',
            )}
          >
            <Settings2 className="h-3.5 w-3.5" strokeWidth={1.75} />
            {t('routerDetail.wireguard.configButton')}
          </button>
        </div>
      </div>
      <p className="mt-1.5 font-mono text-caption text-text-muted">
        {wireguard.interface} · {wireguard.subnet} · :51820
      </p>

      {/* ②bis Configuración de la tarjeta (#1886) */}
      <AnimatePresence initial={false}>
        {configOpen && (
          <motion.div
            key="wg-config"
            initial={reduce ? false : { height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={reduce ? { opacity: 0 } : { height: 0, opacity: 0 }}
            transition={{ duration: 0.25, ease: 'easeOut' }}
            className="overflow-hidden"
          >
            <div className="mt-3 rounded-xl border border-border bg-elevated/60 px-3.5 py-3" role="group" aria-label={t('routerDetail.wireguard.configTitle')}>
              <div className="flex items-center justify-between gap-4">
                <div className="min-w-0">
                  <div className="text-sm font-medium text-text-primary">{t('routerDetail.wireguard.showDisconnected')}</div>
                  <div className="text-caption text-text-muted">{t('routerDetail.wireguard.showDisconnectedHint')}</div>
                </div>
                <Switch
                  checked={showDisconnected}
                  onCheckedChange={toggleDisconnected}
                  aria-label={t('routerDetail.wireguard.showDisconnected')}
                  className="shrink-0"
                />
              </div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Stats */}
      <div className="mt-4 grid grid-cols-2 gap-3">
        <div className="rounded-xl bg-elevated/60 px-3.5 py-3">
          <div className="text-[10px] font-medium uppercase tracking-[0.06em] text-text-muted">{t('routerDetail.wireguard.peersConnected')}</div>
          <div className="mt-1 font-mono text-lg font-semibold text-text-primary">
            {connected}<span className="text-text-muted">/{peers.length}</span>
          </div>
        </div>
        <div className="rounded-xl bg-elevated/60 px-3.5 py-3">
          <div className="text-[10px] font-medium uppercase tracking-[0.06em] text-text-muted">{t('routerDetail.wireguard.transfer')}</div>
          <div className="mt-1 font-mono text-mono-sm font-semibold text-text-primary">
            ↓ {wireguard.transfer?.rx ?? '—'} ↑ {wireguard.transfer?.tx ?? '—'}
          </div>
          {/* #2017: es acumulado desde que el túnel arrancó, no una ventana de
              30 días. Se dice para que el número no se lea como un periodo. */}
          <div className="mt-0.5 text-[10px] text-text-muted">{t('routerDetail.wireguard.transferHint')}</div>
        </div>
      </div>

      {/* Lista de peers */}
      <div className="mt-3 divide-y divide-border/50">
        {visible.map((p, i) => (
          <PeerRow key={p.key || p.id} peer={p} index={i} />
        ))}
      </div>
      {hidden > 0 && (
        <button
          type="button"
          onClick={() => toggleDisconnected(true)}
          className="mt-3 w-full rounded-lg border border-dashed border-border px-3 py-2 text-caption text-text-muted transition-colors duration-150 hover:border-accent/40 hover:text-accent"
        >
          {t('routerDetail.wireguard.hiddenPeers', { count: hidden })}
        </button>
      )}
    </section>
  )
}
