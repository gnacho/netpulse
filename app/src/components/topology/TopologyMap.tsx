/**
 * NetPulse — Mapa SVG animado de la red (v5, mockup aprobado 2-Ago-2026).
 * Chips por dispositivo (cable 26px / wifi 24px con badge de banda), nodos de
 * distribución inferidos del FDB (círculo dashed), CTs anidados bajo su
 * hipervisor (badge +N), túneles WG trazados peer → Internet, flujo de
 * paquetes SMIL ∝ Mbps, pan (drag) + zoom (wheel/pinch) sobre el viewBox,
 * tooltips y coordinación hover con la tabla de enlaces.
 */
import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { PointerEvent as ReactPointerEvent, RefObject } from 'react'
import { useNavigate } from 'react-router'
import { animate, motion, useReducedMotion } from 'framer-motion'
import { Cloud, Laptop, Router as RouterIcon, Smartphone } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { relTime } from '@/i18n'
import type { Device, DistributionNode, Router, WanInfo, WGPeer } from '@/data/mock'
import { StatusPill } from '@/components/StatusPill'
import { DEVICE_ICONS } from '@/components/DeviceRow'
import { cn } from '@/lib/utils'
import type { ChipNode, DistNodeView, RouterNode, TopologyModel } from './model'
import { COLOR, VB_H, VB_W, bandColor, linkColor, statusColor } from './model'

// ---------------------------------------------------------------------------
// API imperativa expuesta a la página (botones de zoom del header)
// ---------------------------------------------------------------------------

export interface TopologyMapApi {
  zoomIn?: () => void
  zoomOut?: () => void
  reset?: () => void
}

interface View {
  x: number
  y: number
  w: number
  h: number
}

const INITIAL_VIEW: View = { x: 0, y: 0, w: VB_W, h: VB_H }
/** zoom 2×–0.5× */
const MIN_W = VB_W / 2
const MAX_W = VB_W * 2
/** Chip "+N" de peers WG que exceden las 4 coordenadas canónicas (zona de
 *  peers, entre la órbita de Internet y el peer interior derecho) */
const PEERS_OVERFLOW_COORD = { x: 560, y: 26 }

function clamp(v: number, min: number, max: number) {
  return Math.min(max, Math.max(min, v))
}

function clampView(v: View): View {
  const w = clamp(v.w, MIN_W, MAX_W)
  const h = w * (VB_H / VB_W)
  return {
    w,
    h,
    x: clamp(v.x, -VB_W / 2, VB_W * 1.5 - w),
    y: clamp(v.y, -VB_H / 2, VB_H * 1.5 - h),
  }
}

// ---------------------------------------------------------------------------
// Tooltip
// ---------------------------------------------------------------------------

type TooltipData =
  | { kind: 'router'; id: string; router: Router; x: number; y: number }
  | { kind: 'internet'; id: string; x: number; y: number }
  | { kind: 'peer'; id: string; peer: WGPeer; x: number; y: number }
  | { kind: 'peersOverflow'; id: string; peers: WGPeer[]; x: number; y: number }
  | { kind: 'ringOverflow'; id: string; routerName: string; devices: Device[]; x: number; y: number }
  | {
      kind: 'chip'
      id: string
      chip: ChipNode
      /** >0 si el chip es un host hipervisor (nota "no es un switch") */
      hostCtCount?: number
      /** device host cuando el chip es un CT/VM anidado */
      ctHost?: Device
      x: number
      y: number
    }
  | { kind: 'dist'; id: string; node: DistributionNode; x: number; y: number }

type TooltipState = TooltipData & { left: number; top: number; below: boolean }

function MiniStat({ label, value, hot }: { label: string; value: string; hot?: boolean }) {
  return (
    <div className="rounded-lg bg-canvas/60 px-2 py-1.5">
      <div className="text-caption uppercase tracking-[0.06em] text-text-muted">{label}</div>
      <div className={cn('font-mono text-mono-sm', hot ? 'text-warn' : 'text-text-primary')}>{value}</div>
    </div>
  )
}

function TooltipCard({
  tip,
  touch,
  wan,
  routerName,
  onPointerEnter,
  onPointerLeave,
}: {
  tip: TooltipState
  touch: boolean
  wan: WanInfo
  /** nombre visible del router del que cuelga un nodo (D8: tooltip dist) */
  routerName: (id: string) => string
  /** puente de hover (issue #255): cancelar el cierre al entrar en el popup */
  onPointerEnter?: () => void
  onPointerLeave?: () => void
}) {
  const { t } = useTranslation()
  return (
    <div
      className={cn(
        // En puntero fino el popup participa del hover (puente para que no se
        // cierre al viajar del nodo al popup); en táctil sigue transparente.
        touch ? 'pointer-events-none' : 'pointer-events-auto',
        'absolute z-20 w-72 -translate-x-1/2 rounded-xl border border-border-strong bg-elevated p-3 shadow-lg',
        tip.below ? 'translate-y-[14px]' : '-translate-y-[calc(100%+14px)]',
      )}
      style={{ left: tip.left, top: tip.top }}
      role="tooltip"
      onPointerEnter={onPointerEnter}
      onPointerLeave={onPointerLeave}
    >
      {tip.kind === 'router' && (
        <div>
          <div className="flex items-center justify-between gap-2">
            <span className="font-display text-sm font-semibold text-text-primary">{tip.router.name}</span>
            <StatusPill
              tone={tip.router.status === 'warn' ? 'warn' : 'ok'}
              label={tip.router.status === 'warn' ? t('common.status.warn') : t('common.status.online')}
            />
          </div>
          <div className="mt-0.5 text-caption text-text-muted">{tip.router.modelShort} · {tip.router.ip}</div>
          <div className="mt-2 grid grid-cols-2 gap-1.5">
            <MiniStat label="CPU" value={`${tip.router.cpu} %`} />
            <MiniStat label="RAM" value={`${tip.router.ram} %`} />
            <MiniStat label={t('routers.colTemp')} value={`${tip.router.temp} °C`} hot={tip.router.hotMetric === 'temp'} />
            <MiniStat label={t('routers.colClients')} value={String(tip.router.clients)} />
          </div>
          <div className="mt-2 text-caption font-semibold text-accent">
            {touch ? t('topology.tapAgainDetail') : t('topology.clickDetail')}
          </div>
        </div>
      )}
      {tip.kind === 'internet' && (
        <div>
          <div className="flex items-center justify-between gap-2">
            <span className="font-display text-sm font-semibold text-text-primary">Internet · {wan.isp}</span>
            <StatusPill tone="ok" label={t('common.status.online')} />
          </div>
          <div className="mt-2 grid grid-cols-2 gap-1.5">
            <MiniStat label={t('routerDetail.wan.publicIp')} value={wan.publicIp} />
            <MiniStat label={t('topology.plan')} value={wan.plan} />
            <MiniStat label={t('home.latency')} value={`${wan.latencyMs} ms`} />
            <MiniStat label={t('home.traffic.loss')} value={`${wan.lossPct} %`} />
          </div>
        </div>
      )}
      {tip.kind === 'peer' && (
        <div>
          <div className="flex items-center justify-between gap-2">
            <span className="font-display text-sm font-semibold text-text-primary">{tip.peer.name}</span>
            <StatusPill tone="tunnel" label={t('common.active')} pulse />
          </div>
          <div className="mt-0.5 font-mono text-caption text-text-muted">
            {tip.peer.tunnelIp} · wg0 · {t('topology.viaInternet')}
          </div>
          <div className="mt-2 grid grid-cols-2 gap-1.5">
            <MiniStat label="Handshake" value={relTime(tip.peer.lastHandshake)} />
            <MiniStat label={t('topology.tunnel')} value="WireGuard" />
            <MiniStat label={t('topology.received')} value={`↓ ${tip.peer.rx}`} />
            <MiniStat label={t('topology.sent')} value={`↑ ${tip.peer.tx}`} />
          </div>
        </div>
      )}
      {tip.kind === 'peersOverflow' && (
        <div>
          <div className="flex items-center justify-between gap-2">
            <span className="font-display text-sm font-semibold text-text-primary">
              {t('topology.peers.overflowTitle', { count: tip.peers.length })}
            </span>
            <StatusPill tone="tunnel" label={t('common.active')} pulse />
          </div>
          <div className="mt-2 space-y-1.5">
            {tip.peers.map((p) => (
              <div key={p.id} className="flex items-center justify-between gap-3 text-caption">
                <span className="font-semibold text-text-primary">{p.name}</span>
                <span className="font-mono text-text-muted">Handshake {relTime(p.lastHandshake)}</span>
              </div>
            ))}
          </div>
        </div>
      )}
      {tip.kind === 'ringOverflow' && (
        <div>
          <div className="flex items-center justify-between gap-2">
            <span className="font-display text-sm font-semibold text-text-primary">
              {t('topology.ring.overflowTitle', { count: tip.devices.length, router: tip.routerName })}
            </span>
            <StatusPill tone="accent" label="+N" />
          </div>
          <div className="mt-2 max-h-48 space-y-1.5 overflow-y-auto">
            {tip.devices.length === 0 && <div className="text-caption text-text-muted">—</div>}
            {tip.devices.map((d) => {
              const Icon = DEVICE_ICONS[d.type] ?? Laptop
              return (
                <div key={d.id} className="flex items-center gap-2 text-caption">
                  <Icon className="h-3.5 w-3.5 shrink-0 text-text-muted" strokeWidth={1.75} />
                  <span className="min-w-0 flex-1 truncate font-semibold text-text-primary">{d.name}</span>
                  <span className="shrink-0 text-text-muted">{d.band === 'cable' ? t('common.cable') : d.band}</span>
                  {d.ip && <span className="shrink-0 font-mono text-text-muted">{d.ip}</span>}
                </div>
              )
            })}
          </div>
          <div className="mt-2 border-t border-border pt-1.5 text-caption font-semibold text-accent">
            {touch ? t('topology.tapAgainDetail') : t('topology.clickDetail')}
          </div>
        </div>
      )}
      {tip.kind === 'chip' && (
        <ChipTooltip chip={tip.chip} touch={touch} hostCtCount={tip.hostCtCount} ctHost={tip.ctHost} />
      )}
      {tip.kind === 'dist' && tip.node.kind === 'managed' && (
        <div>
          <div className="flex items-center justify-between gap-2">
            <span className="font-display text-sm font-semibold text-text-primary">
              {tip.node.name ?? t('topology.dist.managed')}
            </span>
            <StatusPill tone="accent" label="LLDP" />
          </div>
          <div className="mt-0.5 text-caption text-text-muted">
            {[tip.node.ip, tip.node.port].filter(Boolean).join(' · ')}
          </div>
          <div className="mt-2 grid grid-cols-2 gap-1.5">
            <MiniStat label={t('topology.dist.port', { router: routerName(tip.node.routerId) })} value={tip.node.port} />
            <MiniStat label={t('topology.dist.macs')} value={String(tip.node.macCount)} />
          </div>
          {tip.node.lldp && (
            <div className="mt-2 text-caption font-semibold text-accent">
              {t('topology.lldpIdentified', {
                chassis: tip.node.lldp.chassis ?? '—',
                mgmt: tip.node.lldp.mgmt ?? '—',
                caps: tip.node.lldp.caps ?? '—',
                port: tip.node.lldp.portDesc ?? '—',
              })}
            </div>
          )}
        </div>
      )}
      {tip.kind === 'dist' && tip.node.kind !== 'managed' && (
        <div>
          <div className="flex items-center justify-between gap-2">
            <span className="font-display text-sm font-semibold text-text-primary">{t('topology.dist.title')}</span>
            <StatusPill tone="muted" label={t('topology.dist.inferred')} />
          </div>
          <div className="mt-0.5 text-caption text-text-muted">{t('topology.dist.noIp')}</div>
          <div className="mt-2 grid grid-cols-2 gap-1.5">
            <MiniStat label={t('topology.dist.port', { router: routerName(tip.node.routerId) })} value={tip.node.port} />
            <MiniStat label={t('topology.dist.macs')} value={String(tip.node.macCount)} />
          </div>
          <div className="mt-2 text-caption leading-snug text-text-secondary">
            {t('topology.dist.fdbNote', { count: tip.node.macCount })}
          </div>
        </div>
      )}
    </div>
  )
}

/** Tooltip de un chip de dispositivo (cliente, hub, host hipervisor o CT). */
function ChipTooltip({
  chip,
  touch,
  hostCtCount = 0,
  ctHost,
}: {
  chip: ChipNode
  touch: boolean
  /** >0 si el chip es un host hipervisor (badge +N en el mapa) */
  hostCtCount?: number
  /** device host cuando el chip es un CT/VM anidado */
  ctHost?: Device
}) {
  const { t } = useTranslation()
  const d = chip.device
  return (
    <div>
      <div className="flex items-center justify-between gap-2">
        <span className="font-display text-sm font-semibold text-text-primary">{d.name}</span>
        <StatusPill
          tone={chip.isCt ? 'muted' : chip.wired ? 'ok' : chip.weak ? 'warn' : 'ok'}
          label={chip.isCt ? t('topology.ct.pill') : chip.wired ? t('common.cable') : d.band}
        />
      </div>
      <div className="mt-0.5 font-mono text-caption text-text-muted">
        {d.manufacturer} · {d.mac}
      </div>
      <div className="mt-2 space-y-1.5">
        {/* Conexión + IP en una horizontal; la IP no se trunca (issue #149) */}
        <div className="flex items-center justify-between gap-2 rounded-lg bg-canvas/60 px-2 py-1.5">
          <span className="flex min-w-0 items-center gap-3">
            <span className="font-mono text-mono-sm font-semibold text-text-primary">
              {chip.wired ? 'Ethernet' : 'Wi-Fi'}
            </span>
            <span className="shrink-0 text-caption uppercase tracking-[0.12em] text-text-muted">IP</span>
          </span>
          <span className="truncate font-mono text-mono-sm text-text-primary">{d.ip || '—'}</span>
        </div>
        <div className="grid grid-cols-2 gap-1.5">
          <MiniStat
            label={chip.wired ? t('topology.port') : t('topology.signal')}
            value={chip.wired ? (d.port ?? '—') : `${d.signalDbm ?? '—'} dBm`}
            hot={!chip.wired && chip.weak}
          />
          <MiniStat label={t('topology.traffic')} value={`${d.trafficMbps} Mbps`} />
        </div>
      </div>
      {d.lldp && (
        <div className="mt-2 text-caption font-semibold text-accent">
          {t('topology.lldpIdentified', {
            chassis: d.lldp.chassis ?? '—',
            mgmt: d.lldp.mgmt ?? '—',
            caps: d.lldp.caps ?? '—',
            port: d.lldp.portDesc ?? '—',
          })}
        </div>
      )}
      {chip.isCt && (
        <div className="mt-2 text-caption leading-snug text-text-secondary">
          {ctHost
            ? t('topology.ct.noteIn', { host: ctHost.name, port: ctHost.port ?? '—' })
            : t('topology.ct.note')}
        </div>
      )}
      {!chip.isCt && hostCtCount > 0 && (
        <div className="mt-2 text-caption leading-snug text-text-secondary">
          {t('topology.host.note', { count: hostCtCount })}
        </div>
      )}
      {!chip.isCt && chip.weak && (
        <div className="mt-1 text-caption font-semibold text-warn">{t('topology.weakSignal')}</div>
      )}
      {!chip.isCt && (
        <div className="mt-2 text-caption font-semibold text-accent">
          {touch ? t('topology.tapAgainDevice') : t('topology.clickDevice')}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Paquete animado (SMIL animateMotion sobre el path del enlace)
// ---------------------------------------------------------------------------

const PacketDot = memo(function PacketDot({
  pathId,
  dur,
  begin,
  color,
}: {
  pathId: string
  dur: number
  begin: number
  color: string
}) {
  return (
    <circle r={2.6} fill={color} opacity={0.95}>
      <animateMotion dur={`${dur}s`} begin={`${begin}s`} repeatCount="indefinite">
        <mpath href={`#${pathId}`} />
      </animateMotion>
    </circle>
  )
})

// ---------------------------------------------------------------------------
// Anillos de estado alrededor de los nodos router
// ---------------------------------------------------------------------------

function StatusRing({ node, delay, reduce }: { node: RouterNode; delay: number; reduce: boolean }) {
  const R = node.r + 6
  const C = 2 * Math.PI * R
  const target = C * (1 - node.router.health / 100)
  return (
    <g transform="rotate(-90)">
      <circle r={R} fill="none" stroke="rgb(var(--border))" strokeWidth={3} />
      <motion.circle
        r={R}
        fill="none"
        stroke={statusColor(node.router.status)}
        strokeWidth={3}
        strokeLinecap="round"
        strokeDasharray={C}
        initial={reduce ? { strokeDashoffset: target } : { strokeDashoffset: C }}
        animate={{ strokeDashoffset: target }}
        transition={reduce ? { duration: 0 } : { delay, duration: 0.8, ease: 'easeOut' }}
      />
    </g>
  )
}

// ---------------------------------------------------------------------------
// Mapa
// ---------------------------------------------------------------------------

interface TopologyMapProps {
  model: TopologyModel
  apiRef: RefObject<TopologyMapApi>
  showLabels: boolean
  flow: boolean
  hoverLink: string | null
  onHoverLink: (id: string | null) => void
  /**
   * Modo etiquetado (issue #142 Fase B): si se pasa, el clic en un chip NO
   * navega a /devices sino que notifica el dispositivo para abrir el panel
   * de overrides manuales. Ausente → comportamiento normal.
   */
  onTagDevice?: (device: Device) => void
}

export function TopologyMap({ model, apiRef, showLabels, flow, hoverLink, onHoverLink, onTagDevice }: TopologyMapProps) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const navigate = useNavigate()
  const { chips, ctsByHost, ctCountByHost, distNodes, hiddenPeers, internetNode, links, peerNodes, relatedTo, ringOverflowChips, routerNodes, wan } = model
  const containerRef = useRef<HTMLDivElement>(null)
  const svgRef = useRef<SVGSVGElement>(null)
  const viewRef = useRef<View>({ ...INITIAL_VIEW })
  const pointers = useRef(new Map<number, { x: number; y: number }>())
  const drag = useRef<{ px: number; py: number } | null>(null)
  const pinch = useRef<{ view0: View; mid0: { x: number; y: number }; dist0: number } | null>(null)
  const moved = useRef(false)
  /** Retardo de cierre del tooltip (issue #255): el cursor debe poder viajar
   *  desde el nodo hasta el popup sin que este desaparezca. */
  const hoverCloseTimer = useRef<number | null>(null)

  const [hoverNode, setHoverNode] = useState<string | null>(null)
  const [tooltip, setTooltip] = useState<TooltipState | null>(null)

  const hoverCapable = useMemo(
    () =>
      typeof window !== 'undefined' &&
      window.matchMedia('(hover: hover) and (pointer: fine)').matches,
    [],
  )

  // -- aplicar vista (imperativo, sin re-render) -----------------------------
  const applyView = useCallback((v: View) => {
    const cv = clampView(v)
    viewRef.current = cv
    svgRef.current?.setAttribute('viewBox', `${cv.x} ${cv.y} ${cv.w} ${cv.h}`)
  }, [])

  const zoomAt = useCallback(
    (clientX: number, clientY: number, factor: number) => {
      const el = containerRef.current
      if (!el) return
      const rect = el.getBoundingClientRect()
      const v = viewRef.current
      const px = clientX - rect.left
      const py = clientY - rect.top
      const newW = clamp(v.w * factor, MIN_W, MAX_W)
      const newH = newW * (VB_H / VB_W)
      applyView({
        w: newW,
        h: newH,
        x: v.x + (px / rect.width) * v.w - (px / rect.width) * newW,
        y: v.y + (py / rect.height) * v.h - (py / rect.height) * newH,
      })
      setTooltip(null)
    },
    [applyView],
  )

  const zoomCenter = useCallback(
    (factor: number) => {
      const el = containerRef.current
      if (!el) return
      const rect = el.getBoundingClientRect()
      zoomAt(rect.left + rect.width / 2, rect.top + rect.height / 2, factor)
    },
    [zoomAt],
  )

  const resetView = useCallback(() => {
    const from = { ...viewRef.current }
    if (reduce) {
      applyView({ ...INITIAL_VIEW })
      return
    }
    animate(0, 1, {
      duration: 0.4,
      ease: [0.16, 1, 0.3, 1],
      onUpdate: (t) => {
        applyView({
          x: from.x + (INITIAL_VIEW.x - from.x) * t,
          y: from.y + (INITIAL_VIEW.y - from.y) * t,
          w: from.w + (INITIAL_VIEW.w - from.w) * t,
          h: from.h + (INITIAL_VIEW.h - from.h) * t,
        })
      },
    })
  }, [applyView, reduce])

  // API para los botones del header
  useEffect(() => {
    apiRef.current = {
      zoomIn: () => zoomCenter(1 / 1.3),
      zoomOut: () => zoomCenter(1.3),
      reset: resetView,
    }
  }, [apiRef, zoomCenter, resetView])

  // Rueda: zoom (listener no-pasivo para evitar scroll de página)
  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      zoomAt(e.clientX, e.clientY, Math.exp(e.deltaY * 0.0016))
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [zoomAt])

  // -- gestos pointer (pan con 1 dedo, pinch con 2) ---------------------------
  const onPointerDown = useCallback((e: ReactPointerEvent<HTMLDivElement>) => {
    containerRef.current?.setPointerCapture(e.pointerId)
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY })
    moved.current = false
    const rect = containerRef.current?.getBoundingClientRect()
    if (pointers.current.size === 1) {
      drag.current = { px: e.clientX, py: e.clientY }
      pinch.current = null
    } else if (pointers.current.size === 2 && rect) {
      drag.current = null
      const pts = [...pointers.current.values()]
      pinch.current = {
        view0: { ...viewRef.current },
        mid0: {
          x: (pts[0].x + pts[1].x) / 2 - rect.left,
          y: (pts[0].y + pts[1].y) / 2 - rect.top,
        },
        dist0: Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y) || 1,
      }
    }
  }, [])

  const onPointerMove = useCallback(
    (e: ReactPointerEvent<HTMLDivElement>) => {
      if (!pointers.current.has(e.pointerId)) return
      pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY })
      const el = containerRef.current
      if (!el) return
      const rect = el.getBoundingClientRect()

      if (pinch.current && pointers.current.size >= 2) {
        const pts = [...pointers.current.values()]
        const mid = {
          x: (pts[0].x + pts[1].x) / 2 - rect.left,
          y: (pts[0].y + pts[1].y) / 2 - rect.top,
        }
        const cur = Math.hypot(pts[0].x - pts[1].x, pts[0].y - pts[1].y) || 1
        const { view0, mid0, dist0 } = pinch.current
        const factor = dist0 / cur
        const newW = clamp(view0.w * factor, MIN_W, MAX_W)
        const newH = newW * (VB_H / VB_W)
        applyView({
          w: newW,
          h: newH,
          x: view0.x + (mid0.x / rect.width) * view0.w - (mid.x / rect.width) * newW,
          y: view0.y + (mid0.y / rect.height) * view0.h - (mid.y / rect.height) * newH,
        })
        moved.current = true
        setTooltip(null)
        return
      }

      if (drag.current && pointers.current.size === 1) {
        const v = viewRef.current
        const dx = ((e.clientX - drag.current.px) / rect.width) * v.w
        const dy = ((e.clientY - drag.current.py) / rect.height) * v.h
        if (Math.abs(e.clientX - drag.current.px) + Math.abs(e.clientY - drag.current.py) > 3) {
          moved.current = true
          setTooltip(null)
        }
        applyView({ ...v, x: v.x - dx, y: v.y - dy })
        drag.current = { px: e.clientX, py: e.clientY }
      }
    },
    [applyView],
  )

  const endPointer = useCallback((e: ReactPointerEvent<HTMLDivElement>) => {
    pointers.current.delete(e.pointerId)
    if (pointers.current.size < 2) pinch.current = null
    if (pointers.current.size === 0) drag.current = null
    if (containerRef.current?.hasPointerCapture(e.pointerId)) {
      containerRef.current.releasePointerCapture(e.pointerId)
    }
  }, [])

  // -- tooltips ---------------------------------------------------------------
  const openTooltip = useCallback((data: TooltipData) => {
    const el = containerRef.current
    if (!el) return
    const rect = el.getBoundingClientRect()
    const v = viewRef.current
    // nodos en la zona superior del mapa: tooltip hacia abajo
    const below = data.kind === 'internet' || data.kind === 'peer' || data.kind === 'peersOverflow'
    const left = clamp(((data.x - v.x) / v.w) * rect.width, 130, rect.width - 130)
    const rawTop = ((data.y - v.y) / v.h) * rect.height
    const top = below ? clamp(rawTop, 16, rect.height - 190) : clamp(rawTop, 190, rect.height - 20)
    setTooltip({ ...data, left, top, below })
  }, [])

  // El cierre no es inmediato: si el cursor deja el nodo rumbo al popup (que
  // flota sobre él), un retardo corto evita que el popup desaparezca antes de
  // poder clicarlo (issue #255). Entrar en otro nodo o en el propio popup lo
  // cancela.
  const scheduleClose = useCallback(() => {
    if (!hoverCapable) return
    if (hoverCloseTimer.current !== null) return
    hoverCloseTimer.current = window.setTimeout(() => {
      hoverCloseTimer.current = null
      setTooltip(null)
      setHoverNode(null)
    }, 180)
  }, [hoverCapable])

  const cancelClose = useCallback(() => {
    if (hoverCloseTimer.current !== null) {
      window.clearTimeout(hoverCloseTimer.current)
      hoverCloseTimer.current = null
    }
  }, [])

  // Limpieza del timer al desmontar (evita setState tras unmount).
  useEffect(
    () => () => {
      if (hoverCloseTimer.current !== null) window.clearTimeout(hoverCloseTimer.current)
    },
    [],
  )

  const closeHover = scheduleClose

  // -- interacción con nodos ---------------------------------------------------
  const handleNodeHover = useCallback(
    (data: TooltipData, e: ReactPointerEvent) => {
      if (e.pointerType !== 'mouse') return
      cancelClose()
      setHoverNode(data.id)
      openTooltip(data)
    },
    [cancelClose, openTooltip],
  )

  /** Teclado (issue #255): el foco abre el popup igual que el hover. */
  const handleNodeFocus = useCallback(
    (data: TooltipData) => {
      if (!hoverCapable) return
      cancelClose()
      setHoverNode(data.id)
      openTooltip(data)
    },
    [hoverCapable, cancelClose, openTooltip],
  )

  const handleNodeClick = useCallback(
    (data: TooltipData, navTo?: string) => {
      if (moved.current) return
      if (hoverCapable) {
        if (navTo) navigate(navTo)
        return
      }
      if (tooltip?.id === data.id) {
        if (navTo) navigate(navTo)
        return
      }
      setHoverNode(data.id)
      openTooltip(data)
    },
    [hoverCapable, navigate, openTooltip, tooltip?.id],
  )

  /** click en nodo: no propagar al fondo (que cerraría el tooltip) */
  const nodeClick = useCallback(
    (e: { stopPropagation: () => void }, data: TooltipData, navTo?: string) => {
      e.stopPropagation()
      handleNodeClick(data, navTo)
    },
    [handleNodeClick],
  )

  // -- atenuación por hover -----------------------------------------------------
  const related = useMemo(() => (hoverNode ? relatedTo(hoverNode) : null), [hoverNode, relatedTo])
  /** chip por id (para resolver el host de un CT en tooltips) */
  const chipById = useMemo(() => new Map(chips.map((c) => [c.id, c])), [chips])
  /** nombre visible de un router por id (D8: "Puerto de <router>") */
  const routerName = useCallback(
    (id: string) => routerNodes.find((n) => n.id === id)?.router.name ?? id,
    [routerNodes],
  )
  /** datos del tooltip de un chip: +N si es host hipervisor, host si es CT */
  const chipTip = useCallback(
    (chip: ChipNode): TooltipData => ({
      kind: 'chip',
      id: chip.id,
      chip,
      hostCtCount: ctCountByHost.get(chip.id) ?? 0,
      ctHost: chip.isCt ? chipById.get(chip.hubId)?.device : undefined,
      x: chip.x,
      y: chip.y - chip.size / 2 - 6,
    }),
    [ctCountByHost, chipById],
  )
  const nodeOpacity = useCallback(
    (id: string) => {
      if (!related) return 1
      return related.nodes.has(id) ? 1 : 0.4
    },
    [related],
  )
  const linkHighlighted = useCallback(
    (id: string) => hoverLink === id || (related?.links.has(id) ?? false),
    [hoverLink, related],
  )

  const showFlow = flow && !reduce

  // delays de la coreografía de montaje (0 si reduced-motion)
  const T = reduce ? 0 : 1

  const springPop = (delay: number) =>
    reduce
      ? { duration: 0 }
      : { delay, type: 'spring' as const, stiffness: 260, damping: 20 }

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  return (
    <div
      ref={containerRef}
      className="absolute inset-0 touch-none select-none"
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endPointer}
      onPointerCancel={endPointer}
    >
      <svg
        ref={svgRef}
        viewBox={`${INITIAL_VIEW.x} ${INITIAL_VIEW.y} ${INITIAL_VIEW.w} ${INITIAL_VIEW.h}`}
        preserveAspectRatio="xMidYMid meet"
        className="h-full w-full"
        role="img"
        aria-label={t('topology.mapAria')}
        onClick={() => {
          if (!moved.current) {
            setTooltip(null)
            setHoverNode(null)
          }
        }}
      >
        <defs>
          <linearGradient id="topo-gw-grad" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0%" stopColor={COLOR.accent} stopOpacity="0.28" />
            <stop offset="100%" stopColor={COLOR.tunnel} stopOpacity="0.28" />
          </linearGradient>
        </defs>

        {/* ------------------------- Anillos guía wifi ------------------------- */}
        {/* guías para TODOS los anillos usados (el modelo añade anillos extra
            si el wifi supera el aforo de los dos primeros) */}
        <g aria-hidden>
          {routerNodes.map((node) =>
            (model.ringRadii.get(node.id) ?? (node.id === model.gatewayNode?.id ? [96, 130] : [82, 120])).map((r) => (
              <circle
                key={`${node.id}-ring-${r}`}
                cx={node.x}
                cy={node.y}
                r={r}
                fill="none"
                stroke="rgb(var(--border-strong))"
                strokeWidth={1}
                strokeDasharray="2 6"
                opacity={0.3}
              />
            )),
          )}
        </g>

        {/* ------------------------- Enlaces ------------------------- */}
        <g>
          {links.map((link, i) => {
            const highlighted = linkHighlighted(link.id)
            const isWg = link.kind === 'wg'
            const isWifiUp = link.kind === 'uplink' && link.wifi
            const drawDelay = link.kind === 'wan' ? 0.25 : link.kind === 'uplink' ? 0.8 + i * 0.12 : 1.5
            const stroke = linkColor(link)
            const baseOpacity = link.kind === 'wired' ? 0.45 : link.kind === 'dist' ? 0.55 : isWifiUp ? 0.8 : link.kind === 'uplink' ? 0.55 : 0.9
            return (
              <g key={link.id}>
                {/* base */}
                <motion.path
                  id={`topo-link-${link.id}`}
                  d={link.d}
                  fill="none"
                  stroke={stroke}
                  strokeWidth={link.width}
                  strokeLinecap="round"
                  strokeDasharray={isWg ? '7 7' : isWifiUp ? '8 6' : undefined}
                  opacity={baseOpacity}
                  initial={reduce ? { pathLength: 1 } : { pathLength: 0 }}
                  animate={{ pathLength: 1 }}
                  transition={reduce ? { duration: 0 } : { delay: drawDelay * T, duration: 0.4, ease: 'easeOut' }}
                />
                {/* flujo del dash en túneles WG */}
                {isWg && !reduce && (
                  <motion.path
                    d={link.d}
                    fill="none"
                    stroke={COLOR.tunnel}
                    strokeWidth={link.width}
                    strokeLinecap="round"
                    strokeDasharray="7 7"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 0.9, strokeDashoffset: [0, -28] }}
                    transition={{
                      opacity: { delay: 1.8 * T, duration: 0.4 },
                      strokeDashoffset: { duration: 1.4, repeat: Infinity, ease: 'linear' },
                    }}
                  />
                )}
                {/* highlight (hover tabla / nodo) */}
                <motion.path
                  d={link.d}
                  fill="none"
                  stroke={isWg ? COLOR.tunnel : COLOR.accent}
                  strokeWidth={link.width + 1.5}
                  strokeLinecap="round"
                  style={{ filter: `drop-shadow(0 0 6px ${isWg ? COLOR.tunnel : COLOR.accent})` }}
                  initial={false}
                  animate={{ opacity: highlighted ? 1 : 0 }}
                  transition={{ duration: 0.3 }}
                />
                {/* zona de hit ampliada */}
                <path
                  d={link.d}
                  fill="none"
                  stroke="transparent"
                  strokeWidth={link.kind === 'wired' ? 8 : 16}
                  className="cursor-pointer"
                  onPointerEnter={(e) => {
                    if (e.pointerType === 'mouse') onHoverLink(link.id)
                  }}
                  onPointerLeave={(e) => {
                    if (e.pointerType === 'mouse') onHoverLink(null)
                  }}
                />
              </g>
            )
          })}

          {/* paquetes animados (∝ Mbps, color del enlace) */}
          {showFlow &&
            links
              .filter((l) => l.packets > 0)
              .map((l) =>
                Array.from({ length: l.packets }, (_, i) => (
                  <PacketDot
                    key={`${l.id}-p${i}`}
                    pathId={`topo-link-${l.id}`}
                    dur={l.packetDur}
                    begin={-((l.packetDur / l.packets) * i)}
                    color={linkColor(l)}
                  />
                )),
              )}
        </g>

        {/* ------------------------- Internet ------------------------- */}
        <g
          transform={`translate(${internetNode.x} ${internetNode.y})`}
          className="cursor-pointer outline-none"
          role="button"
          tabIndex={0}
          aria-label={t('topology.internetAria', { isp: wan.isp, plan: wan.plan })}
          onPointerEnter={(e) =>
            handleNodeHover({ kind: 'internet', id: 'internet', x: internetNode.x, y: internetNode.y + 46 }, e)
          }
          onPointerLeave={closeHover}
          onFocus={() => handleNodeFocus({ kind: 'internet', id: 'internet', x: internetNode.x, y: internetNode.y + 46 })}
          onBlur={closeHover}
          onClick={(e) =>
            nodeClick(e, { kind: 'internet', id: 'internet', x: internetNode.x, y: internetNode.y + 46 })
          }
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              handleNodeClick({ kind: 'internet', id: 'internet', x: internetNode.x, y: internetNode.y + 46 })
            }
          }}
        >
          <motion.g
            initial={reduce ? { opacity: 1 } : { opacity: 0, scale: 0 }}
            animate={{ opacity: nodeOpacity('internet'), scale: 1 }}
            transition={reduce ? { duration: 0.2 } : { ...springPop(0), opacity: { duration: 0.2 } }}
            style={{ transformBox: 'fill-box', transformOrigin: 'center' }}
          >
            {!reduce && (
              <motion.circle
                r={42}
                fill={COLOR.ok}
                initial={{ opacity: 0.08 }}
                animate={{ opacity: [0.05, 0.15, 0.05] }}
                transition={{ duration: 5, repeat: Infinity, ease: 'easeInOut' }}
              />
            )}
            {/* anillo orbital con punto rotando 24s (mockup v5) */}
            {!reduce && (
              <g aria-hidden>
                <circle r={38} fill="none" stroke={COLOR.ok} strokeWidth={1.2} strokeDasharray="3 7" opacity={0.55} />
                <circle r={3} fill={COLOR.ok} cx={0} cy={-38} />
                <animateTransform attributeName="transform" type="rotate" from="0" to="360" dur="24s" repeatCount="indefinite" />
              </g>
            )}
            <circle r={28} fill="rgb(var(--elevated))" stroke={COLOR.ok} strokeWidth={2} />
            <Cloud x={-12} y={-10} width={24} height={20} className="text-ok" strokeWidth={1.75} aria-hidden />
          </motion.g>
        </g>

        {/* ------------------------- Routers ------------------------- */}
        {routerNodes.map((node, i) => {
          const isGw = node.router.roleBadge === 'Principal'
          const isWarn = node.router.status === 'warn'
          const delay = isGw ? 0.55 : 1.0 + (i - 1) * 0.15
          const Icon = RouterIcon
          return (
            <motion.g
              key={node.id}
              transform={`translate(${node.x} ${node.y})`}
              className="cursor-pointer outline-none"
              role="button"
              tabIndex={0}
              aria-label={t('topology.routerAria', { name: node.router.name, model: node.router.modelShort, clients: node.router.clients })}
              animate={{ opacity: nodeOpacity(node.id) }}
              transition={{ duration: 0.2 }}
              onPointerEnter={(e) =>
                handleNodeHover({ kind: 'router', id: node.id, router: node.router, x: node.x, y: node.y - node.r - 10 }, e)
              }
              onPointerLeave={closeHover}
              onFocus={() =>
                handleNodeFocus({ kind: 'router', id: node.id, router: node.router, x: node.x, y: node.y - node.r - 10 })
              }
              onBlur={closeHover}
              onClick={(e) =>
                nodeClick(
                  e,
                  { kind: 'router', id: node.id, router: node.router, x: node.x, y: node.y - node.r - 10 },
                  `/routers/${node.id}`,
                )
              }
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  handleNodeClick(
                    { kind: 'router', id: node.id, router: node.router, x: node.x, y: node.y - node.r - 10 },
                    `/routers/${node.id}`,
                  )
                }
              }}
            >
              {/* halo permanente del gateway / pulso de aviso */}
              {isGw && !reduce && (
                <motion.circle
                  r={node.r + 18}
                  fill={COLOR.accent}
                  initial={{ opacity: 0.1 }}
                  animate={{ opacity: [0.08, 0.22, 0.08] }}
                  transition={{ duration: 6, repeat: Infinity, ease: 'easeInOut' }}
                />
              )}
              {isWarn && !reduce && (
                <circle r={node.r + 8} fill="none" stroke={COLOR.warn} strokeWidth={2}>
                  <animate attributeName="r" values={`${node.r + 8};${node.r + 22}`} dur="1.6s" repeatCount="indefinite" />
                  <animate attributeName="opacity" values="0.6;0" dur="1.6s" repeatCount="indefinite" />
                </circle>
              )}
              <motion.g
                initial={reduce ? { scale: 1 } : { scale: 0 }}
                animate={{ scale: 1 }}
                transition={springPop(delay * T)}
                style={{ transformBox: 'fill-box', transformOrigin: 'center' }}
              >
                <circle
                  r={node.r}
                  fill={isGw ? 'url(#topo-gw-grad)' : 'rgb(var(--elevated))'}
                  stroke={isWarn ? COLOR.warn : isGw ? COLOR.accent : 'rgb(var(--border-strong))'}
                  strokeWidth={isGw || isWarn ? 2 : 1.5}
                />
                <StatusRing node={node} delay={(delay + 0.2) * T} reduce={reduce ?? false} />
                <Icon
                  x={-node.r * 0.44}
                  y={-node.r * 0.44}
                  width={node.r * 0.88}
                  height={node.r * 0.88}
                  className={isWarn ? 'text-warn' : 'text-accent'}
                  strokeWidth={1.75}
                  aria-hidden
                />
              </motion.g>
            </motion.g>
          )
        })}

        {/* ------------------------- Peers WireGuard ------------------------- */}
        {peerNodes.map((node, i) => {
          const PeerIcon = node.peer.type === 'movil' ? Smartphone : Laptop
          return (
            <g key={node.id} transform={`translate(${node.x} ${node.y})`}>
              <motion.g
                initial={reduce ? { opacity: 1 } : { opacity: 0, scale: 0.6 }}
                animate={{ opacity: 1, scale: 1 }}
                transition={reduce ? { duration: 0 } : { delay: 1.8 * T + i * 0.12, duration: 0.35 }}
                style={{ transformBox: 'fill-box', transformOrigin: 'center' }}
              >
                <motion.g
                  className="cursor-pointer outline-none"
                  role="button"
                  tabIndex={0}
                  aria-label={t('topology.peerAria', { name: node.peer.name, ip: node.peer.tunnelIp })}
                  animate={{ opacity: nodeOpacity(node.id) }}
                  transition={{ duration: 0.2 }}
                  onPointerEnter={(e) =>
                    handleNodeHover({ kind: 'peer', id: node.id, peer: node.peer, x: node.x, y: node.y + 30 }, e)
                  }
                  onPointerLeave={closeHover}
                  onFocus={() => handleNodeFocus({ kind: 'peer', id: node.id, peer: node.peer, x: node.x, y: node.y + 30 })}
                  onBlur={closeHover}
                  onClick={(e) =>
                    nodeClick(e, { kind: 'peer', id: node.id, peer: node.peer, x: node.x, y: node.y + 30 })
                  }
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      handleNodeClick({ kind: 'peer', id: node.id, peer: node.peer, x: node.x, y: node.y + 30 })
                    }
                  }}
                >
                  {!reduce && (
                    <circle r={22} fill="none" stroke={COLOR.tunnel} strokeWidth={1.5}>
                      <animate attributeName="r" values="18;27" dur="2.2s" repeatCount="indefinite" />
                      <animate attributeName="opacity" values="0.5;0" dur="2.2s" repeatCount="indefinite" />
                    </circle>
                  )}
                  <rect x={-18} y={-18} width={36} height={36} rx={11} fill="rgb(var(--elevated))" stroke={COLOR.tunnel} strokeWidth={1.5} />
                  <PeerIcon x={-9} y={-9} width={18} height={18} className="text-tunnel" strokeWidth={1.75} aria-hidden />
                </motion.g>
              </motion.g>
            </g>
          )
        })}

        {/* ------------------------- Peers ocultos ("+N") ---------------------- */}
        {hiddenPeers.length > 0 && (
          <g transform={`translate(${PEERS_OVERFLOW_COORD.x} ${PEERS_OVERFLOW_COORD.y})`}>
            <motion.g
              initial={reduce ? { opacity: 1 } : { opacity: 0, scale: 0.6 }}
              animate={{ opacity: nodeOpacity('peers-overflow'), scale: 1 }}
              transition={reduce ? { duration: 0 } : { delay: 2.1 * T, duration: 0.35 }}
              style={{ transformBox: 'fill-box', transformOrigin: 'center' }}
            >
              <g
                className="cursor-pointer outline-none"
                role="button"
                tabIndex={0}
                aria-label={t('topology.peers.overflowAria', { count: hiddenPeers.length })}
                onPointerEnter={(e) =>
                  handleNodeHover(
                    { kind: 'peersOverflow', id: 'peers-overflow', peers: hiddenPeers, x: PEERS_OVERFLOW_COORD.x, y: PEERS_OVERFLOW_COORD.y + 30 },
                    e,
                  )
                }
                onPointerLeave={closeHover}
                onFocus={() =>
                  handleNodeFocus({
                    kind: 'peersOverflow',
                    id: 'peers-overflow',
                    peers: hiddenPeers,
                    x: PEERS_OVERFLOW_COORD.x,
                    y: PEERS_OVERFLOW_COORD.y + 30,
                  })
                }
                onBlur={closeHover}
                onClick={(e) =>
                  nodeClick(
                    e,
                    { kind: 'peersOverflow', id: 'peers-overflow', peers: hiddenPeers, x: PEERS_OVERFLOW_COORD.x, y: PEERS_OVERFLOW_COORD.y + 30 },
                  )
                }
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    handleNodeClick({
                      kind: 'peersOverflow',
                      id: 'peers-overflow',
                      peers: hiddenPeers,
                      x: PEERS_OVERFLOW_COORD.x,
                      y: PEERS_OVERFLOW_COORD.y + 30,
                    })
                  }
                }}
              >
                <rect x={-15} y={-11} width={30} height={22} rx={8} fill="rgb(var(--elevated))" stroke={COLOR.tunnel} strokeWidth={1.5} />
                <text x={0} y={3.5} textAnchor="middle" fontSize={9.5} fontWeight={700} fill={COLOR.tunnel}>
                  +{hiddenPeers.length}
                </text>
              </g>
            </motion.g>
          </g>
        )}

        {/* --------- Clientes ocultos del anillo ("+N", semántica server) ------ */}
        {ringOverflowChips.map((chip, i) => {
          const routerName = routerNodes.find((n) => n.id === chip.routerId)?.router.name ?? chip.routerId
          const tip: Extract<TooltipData, { kind: 'ringOverflow' }> = {
            kind: 'ringOverflow',
            id: `ring-overflow-${chip.routerId}`,
            routerName,
            devices: chip.devices,
            x: chip.x,
            y: chip.y + 30,
          }
          return (
            <g key={`ring-overflow-${chip.routerId}`} transform={`translate(${chip.x} ${chip.y})`}>
              <motion.g
                initial={reduce ? { opacity: 1 } : { opacity: 0, scale: 0.6 }}
                animate={{ opacity: nodeOpacity(`ring-overflow-${chip.routerId}`), scale: 1 }}
                transition={reduce ? { duration: 0 } : { delay: (2.2 + i * 0.1) * T, duration: 0.35 }}
                style={{ transformBox: 'fill-box', transformOrigin: 'center' }}
              >
                <g
                  className="cursor-pointer outline-none"
                  role="button"
                  tabIndex={0}
                  aria-label={t('topology.ring.overflowAria', { count: chip.count, router: routerName })}
                  onPointerEnter={(e) => handleNodeHover(tip, e)}
                  onPointerLeave={closeHover}
                  onFocus={() => handleNodeFocus(tip)}
                  onBlur={closeHover}
                  onClick={(e) => nodeClick(e, tip)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      handleNodeClick(tip)
                    }
                  }}
                >
                  <rect x={-15} y={-11} width={30} height={22} rx={8} fill="rgb(var(--elevated))" stroke={COLOR.accent} strokeWidth={1.5} />
                  <text x={0} y={3.5} textAnchor="middle" fontSize={9.5} fontWeight={700} fill={COLOR.accent}>
                    +{chip.count}
                  </text>
                </g>
              </motion.g>
            </g>
          )
        })}

        {/* ------------------------- Nodos de distribución (switch inferido) --- */}
        {distNodes.map((dv, i) => (
          <DistNodeGroup
            key={dv.id}
            dv={dv}
            delay={(1.5 + i * 0.1) * T}
            reduce={reduce ?? false}
            opacity={nodeOpacity(dv.id)}
            onHover={(e) => handleNodeHover({ kind: 'dist', id: dv.id, node: dv.node, x: dv.x, y: dv.y - dv.r - 12 }, e)}
            onLeave={closeHover}
            onFocus={() => handleNodeFocus({ kind: 'dist', id: dv.id, node: dv.node, x: dv.x, y: dv.y - dv.r - 12 })}
            onBlur={closeHover}
            onClick={(e) => nodeClick(e, { kind: 'dist', id: dv.id, node: dv.node, x: dv.x, y: dv.y - dv.r - 12 })}
          />
        ))}

        {/* ------------------------- Chips de dispositivos ------------------------- */}
        {chips.map((chip, i) => (
          <ChipGroup
            key={chip.id}
            chip={chip}
            ctCount={ctCountByHost.get(chip.id) ?? 0}
            delay={(1.6 + Math.min(i * 0.02, 0.8)) * T}
            reduce={reduce ?? false}
            opacity={nodeOpacity(chip.id)}
            onHover={(e) => handleNodeHover(chipTip(chip), e)}
            onLeave={closeHover}
            onFocus={() => handleNodeFocus(chipTip(chip))}
            onBlur={closeHover}
            onClick={(e) => {
              if (onTagDevice) {
                nodeClick(e, chipTip(chip))
                onTagDevice(chip.device)
                return
              }
              if (chip.isCt) {
                nodeClick(e, chipTip(chip))
                return
              }
              nodeClick(
                e,
                chipTip(chip),
                `/devices?q=${encodeURIComponent(chip.device.mac ?? chip.device.name)}`,
              )
            }}
          />
        ))}

        {/* ------------------------- Etiquetas ------------------------- */}
        <motion.g
          initial={false}
          animate={{ opacity: showLabels ? 1 : 0 }}
          transition={{ duration: 0.2 }}
          pointerEvents="none"
          aria-hidden={!showLabels}
        >
          {/* Internet */}
          <LabelText x={548} y={54} anchor="start" delay={2.0 * T} reduce={reduce ?? false}
            title={`Internet · ${wan.isp}`} sub={t('routerDetail.wan.fiber', { plan: wan.plan })} />
          {/* Gateway */}
          {model.gatewayNode && (
            <LabelText x={model.gatewayNode.label.x} y={model.gatewayNode.label.y} anchor={model.gatewayNode.label.anchor}
              delay={2.05 * T} reduce={reduce ?? false}
              title={model.gatewayNode.router.name}
              sub={`${model.gatewayNode.router.modelShort} · ${model.gatewayNode.router.roleBadge} · ${t('common.clientsCount', { count: model.gatewayNode.router.clients })}`} />
          )}
          {/* APs */}
          {model.apNodes.map((node, i) => (
            <LabelText key={node.id} x={node.label.x} y={node.label.y} anchor={node.label.anchor}
              delay={(2.1 + i * 0.03) * T} reduce={reduce ?? false}
              title={node.router.name}
              sub={node.router.status === 'warn'
                ? `${t('common.clientsCount', { count: node.router.clients })} · ${t('common.status.warn')}`
                : t('common.clientsCount', { count: node.router.clients })}
              subColor={node.router.status === 'warn' ? COLOR.warn : undefined} />
          ))}
          {/* Distnodes (inferidos / gestionados vía LLDP) */}
          {distNodes.map((dv, i) =>
            dv.node.kind === 'managed' ? (
              <LabelText key={dv.id} x={dv.x} y={dv.y - 34} anchor="middle" delay={(2.15 + i * 0.03) * T}
                reduce={reduce ?? false} title={dv.node.name ?? t('topology.dist.managed')}
                sub={`${[dv.node.ip ?? 'LLDP', dv.node.port].join(' · ')}`}
                subColor={COLOR.accent} />
            ) : (
              <LabelText key={dv.id} x={dv.x} y={dv.y - 34} anchor="middle" delay={(2.15 + i * 0.03) * T}
                reduce={reduce ?? false} title={t('topology.dist.title')}
                sub={`${t('topology.dist.inferred')} · ${dv.node.port}`} />
            ),
          )}
          {/* Hosts hipervisores (badge +N ya en el chip; etiqueta con puerto y nº CTs) */}
          {[...ctsByHost.keys()].map((hostId, i) => {
            const host = chips.find((c) => c.id === hostId)
            if (!host) return null
            return (
              <LabelText key={hostId} x={host.x} y={host.y - 38} anchor="middle" delay={(2.18 + i * 0.03) * T}
                reduce={reduce ?? false} title={host.device.name}
                sub={`${t('topology.host.hypervisor')} · ${host.device.port ?? ''} · ${ctCountByHost.get(hostId) ?? 0} CT`} />
            )
          })}
          {/* Peers */}
          {peerNodes.map((node, i) => (
            <LabelText key={node.id} x={node.x + (i % 2 ? -26 : 26)} y={node.y - 4} anchor={i % 2 ? 'end' : 'start'}
              delay={(2.2 + i * 0.03) * T} reduce={reduce ?? false}
              title={node.peer.name} sub={t('topology.peerVia')} subColor={COLOR.tunnel} />
          ))}
          {/* Etiquetas de enlace */}
          {links
            .filter((l) => l.label)
            .map((l, i) => (
              <motion.text
                key={l.id}
                x={l.lx}
                y={l.ly}
                fontSize={10}
                className="fill-text-muted font-mono"
                style={{ paintOrder: 'stroke' }}
                stroke="rgb(var(--canvas))"
                strokeWidth={3}
                initial={reduce ? { opacity: 0.9 } : { opacity: 0 }}
                animate={{ opacity: 0.9 }}
                transition={{ delay: (2.25 + i * 0.03) * T, duration: reduce ? 0 : 0.3 }}
              >
                {l.label}
              </motion.text>
            ))}
        </motion.g>
      </svg>

      {/* tooltip flotante */}
      {tooltip && (
        <TooltipCard
          tip={tooltip}
          touch={!hoverCapable}
          wan={wan}
          routerName={routerName}
          onPointerEnter={cancelClose}
          onPointerLeave={scheduleClose}
        />
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Etiqueta de nodo (título + subtítulo)
// ---------------------------------------------------------------------------

function LabelText({
  x,
  y,
  anchor,
  title,
  sub,
  subColor,
  delay,
  reduce,
}: {
  x: number
  y: number
  anchor: 'start' | 'middle' | 'end'
  title: string
  sub: string
  subColor?: string
  delay: number
  reduce: boolean
}) {
  return (
    <motion.g
      initial={reduce ? { opacity: 1 } : { opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ delay, duration: reduce ? 0 : 0.3 }}
    >
      <text x={x} y={y} textAnchor={anchor} fontSize={13} fontWeight={600} className="fill-text-primary"
        style={{ paintOrder: 'stroke' }} stroke="rgb(var(--canvas))" strokeWidth={4}>
        {title}
      </text>
      <text x={x} y={y + 15} textAnchor={anchor} fontSize={10.5} className={subColor ? undefined : 'fill-text-secondary'}
        style={{ paintOrder: 'stroke' }} stroke="rgb(var(--canvas))" strokeWidth={3} fill={subColor}>
        {sub}
      </text>
    </motion.g>
  )
}

// ---------------------------------------------------------------------------
// Nodo de distribución inferido (círculo dashed, sin IP)
// ---------------------------------------------------------------------------

const DistNodeGroup = memo(function DistNodeGroup({
  dv,
  delay,
  reduce,
  opacity,
  onHover,
  onLeave,
  onFocus,
  onBlur,
  onClick,
}: {
  dv: DistNodeView
  delay: number
  reduce: boolean
  opacity: number
  onHover: (e: ReactPointerEvent) => void
  onLeave: () => void
  onFocus: () => void
  onBlur: () => void
  onClick: (e: { stopPropagation: () => void }) => void
}) {
  const { t } = useTranslation()
  const SwitchIcon = DEVICE_ICONS.switch
  const managed = dv.node.kind === 'managed'
  return (
    <motion.g
      transform={`translate(${dv.x} ${dv.y})`}
      className="cursor-pointer outline-none"
      role="button"
      tabIndex={0}
      aria-label={
        managed
          ? `${dv.node.name ?? t('topology.dist.managed')}, LLDP, ${dv.node.ip ?? ''} ${dv.node.port}`
          : `${t('topology.dist.title')}, ${t('topology.dist.inferred')}, ${dv.node.port}`
      }
      animate={{ opacity }}
      transition={{ duration: 0.2 }}
      onPointerEnter={onHover}
      onPointerLeave={onLeave}
      onFocus={onFocus}
      onBlur={onBlur}
      onClick={onClick}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onClick(e)
        }
      }}
    >
      <motion.g
        initial={reduce ? { scale: 1 } : { scale: 0 }}
        animate={{ scale: 1 }}
        transition={reduce ? { duration: 0 } : { delay, type: 'spring', stiffness: 260, damping: 20 }}
        style={{ transformBox: 'fill-box', transformOrigin: 'center' }}
      >
        {/* managed: sólido con borde cyan (identificado vía LLDP);
            inferred: círculo dashed (no podemos afirmarlo) */}
        <circle
          r={dv.r + 5}
          fill="none"
          stroke={managed ? COLOR.accent : 'rgb(var(--text-muted))'}
          strokeWidth={1}
          strokeDasharray={managed ? undefined : '2 5'}
          opacity={managed ? 0.5 : 0.6}
        />
        <circle
          r={dv.r}
          fill={managed ? 'rgb(var(--elevated))' : 'rgb(var(--elevated) / 0.65)'}
          stroke={managed ? COLOR.accent : 'rgb(var(--text-muted))'}
          strokeWidth={1.5}
          strokeDasharray={managed ? undefined : '4 4'}
        />
        <SwitchIcon x={-10} y={-10} width={20} height={20} className={managed ? 'text-accent' : 'text-text-secondary'} strokeWidth={1.75} aria-hidden />
        {/* badge LLDP (misma geometría que el badge de los chips) */}
        {managed && (
          <g aria-hidden>
            <rect x={dv.r - 4} y={-dv.r - 4} width={22} height={10} rx={5} fill="rgb(var(--elevated))" stroke={COLOR.accent} strokeWidth={1} />
            <text x={dv.r + 7} y={-dv.r + 3.4} textAnchor="middle" fontSize={6.5} fontWeight={800} fill={COLOR.accent} letterSpacing="0.04em">
              LLDP
            </text>
          </g>
        )}
      </motion.g>
    </motion.g>
  )
})

// ---------------------------------------------------------------------------
// Chip de dispositivo (cliente wifi/cable, hub, host hipervisor o CT)
// ---------------------------------------------------------------------------

const ChipGroup = memo(function ChipGroup({
  chip,
  ctCount,
  delay,
  reduce,
  opacity,
  onHover,
  onLeave,
  onFocus,
  onBlur,
  onClick,
}: {
  chip: ChipNode
  ctCount: number
  delay: number
  reduce: boolean
  opacity: number
  onHover: (e: ReactPointerEvent) => void
  onLeave: () => void
  onFocus: () => void
  onBlur: () => void
  onClick: (e: { stopPropagation: () => void }) => void
}) {
  const d = chip.device
  const S = chip.size
  const half = S / 2
  const Icon = DEVICE_ICONS[d.type] ?? DEVICE_ICONS.desconocido
  const stroke = d.lldp ? COLOR.accent : chip.wired ? COLOR.ok : 'rgb(var(--border-strong))'
  return (
    <g transform={`translate(${chip.x} ${chip.y})`}>
      <motion.g
        className="cursor-pointer outline-none"
        role="button"
        tabIndex={0}
        aria-label={`${d.name}, ${chip.wired ? 'cable' : d.band}`}
        initial={reduce ? { opacity: 1, scale: 1 } : { opacity: 0, scale: 0 }}
        animate={{ opacity, scale: 1 }}
        transition={reduce ? { duration: 0 } : { delay, type: 'spring', stiffness: 300, damping: 18 }}
        style={{ transformBox: 'fill-box', transformOrigin: 'center' }}
        onPointerEnter={onHover}
        onPointerLeave={onLeave}
        onFocus={onFocus}
        onBlur={onBlur}
        onClick={onClick}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            onClick(e)
          }
        }}
      >
      <rect
        x={-half}
        y={-half}
        width={S}
        height={S}
        rx={7}
        fill="rgb(var(--elevated))"
        stroke={stroke}
        strokeWidth={chip.wired ? 1.3 : 1.1}
      />
      <Icon
        x={-7}
        y={-7}
        width={14}
        height={14}
        style={{ color: chip.wired ? COLOR.ok : 'rgb(var(--text-primary))' }}
        strokeWidth={1.9}
        aria-hidden
      />
      {/* badge de banda (wifi): esquina inferior derecha */}
      {!chip.wired && (
        <circle
          cx={half - 2}
          cy={half - 2}
          r={3.8}
          fill={bandColor(chip.band, chip.weak)}
          stroke="rgb(var(--canvas))"
          strokeWidth={1.4}
        />
      )}
      {/* badge LLDP: esquina superior derecha */}
      {d.lldp && (
        <g aria-hidden>
          <rect x={half - 4} y={-half - 4} width={22} height={10} rx={5} fill="rgb(var(--elevated))" stroke={COLOR.accent} strokeWidth={1} />
          <text x={half + 7} y={-half + 3.4} textAnchor="middle" fontSize={6.5} fontWeight={800} fill={COLOR.accent} letterSpacing="0.04em">
            LLDP
          </text>
        </g>
      )}
      {/* badge +N de CTs (host hipervisor) */}
      {ctCount > 0 && (
        <g aria-hidden>
          <circle cx={half + 1} cy={-half - 1} r={8} fill="rgb(var(--elevated))" stroke={COLOR.ok} strokeWidth={1.2} />
          <text x={half + 1} y={-half + 2} textAnchor="middle" fontSize={8} fontWeight={700} fill={COLOR.ok}>
            +{ctCount}
          </text>
        </g>
      )}
      <title>{d.name}</title>
      </motion.g>
    </g>
  )
})
