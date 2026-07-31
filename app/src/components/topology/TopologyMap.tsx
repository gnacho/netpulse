/**
 * NetPulse — Mapa SVG animado de la red (topology.md §②).
 * Pan (drag) + zoom (wheel/pinch 0.5×–2×) manipulando el viewBox,
 * flujo de paquetes SMIL, túneles WG dashed violeta, tooltips y
 * coordinación hover con la tabla de enlaces.
 */
import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { PointerEvent as ReactPointerEvent, RefObject } from 'react'
import { useNavigate } from 'react-router-dom'
import { animate, motion, useReducedMotion } from 'framer-motion'
import { Cloud, Laptop, Router as RouterIcon, Smartphone } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { relTime } from '@/i18n'
import type { Router, WanInfo, WGPeer } from '@/data/mock'
import { StatusPill } from '@/components/StatusPill'
import { cn } from '@/lib/utils'
import type { ClientDot, Cluster, RouterNode, TopoLink, TopologyModel } from './model'
import { COLOR, VB_H, VB_W, bandColor, statusColor } from './model'

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
  | { kind: 'dot'; id: string; dot: ClientDot; x: number; y: number }

type TooltipState = TooltipData & { left: number; top: number; below: boolean }

function MiniStat({ label, value, hot }: { label: string; value: string; hot?: boolean }) {
  return (
    <div className="rounded-lg bg-canvas/60 px-2 py-1.5">
      <div className="text-caption uppercase tracking-[0.06em] text-text-muted">{label}</div>
      <div className={cn('font-mono text-mono-sm', hot ? 'text-warn' : 'text-text-primary')}>{value}</div>
    </div>
  )
}

function TooltipCard({ tip, touch, wan }: { tip: TooltipState; touch: boolean; wan: WanInfo }) {
  const { t } = useTranslation()
  return (
    <div
      className={cn(
        'pointer-events-none absolute z-20 w-56 -translate-x-1/2 rounded-xl border border-border-strong bg-elevated p-3 shadow-lg',
        tip.below ? 'translate-y-[14px]' : '-translate-y-[calc(100%+14px)]',
      )}
      style={{ left: tip.left, top: tip.top }}
      role="tooltip"
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
          <div className="mt-0.5 font-mono text-caption text-text-muted">{tip.peer.tunnelIp} · wg0</div>
          <div className="mt-2 grid grid-cols-2 gap-1.5">
            <MiniStat label="Handshake" value={relTime(tip.peer.lastHandshake)} />
            <MiniStat label={t('topology.tunnel')} value="WireGuard" />
            <MiniStat label={t('topology.received')} value={`↓ ${tip.peer.rx}`} />
            <MiniStat label={t('topology.sent')} value={`↑ ${tip.peer.tx}`} />
          </div>
        </div>
      )}
      {tip.kind === 'dot' && (
        <div>
          <div className="font-display text-sm font-semibold text-text-primary">
            {tip.dot.device?.name ?? t('topology.connectedClient')}
          </div>
          <div className="mt-0.5 font-mono text-caption text-text-muted">
            {tip.dot.device?.ip ?? tip.dot.band}
            {tip.dot.device ? ` · ${tip.dot.band}` : ''}
            {tip.dot.device?.signalDbm != null ? ` · ${tip.dot.device.signalDbm} dBm` : ''}
          </div>
          {tip.dot.weak && <div className="mt-1 text-caption font-semibold text-warn">{t('topology.weakSignal')}</div>}
          {tip.dot.device && (
            <div className="mt-2 text-caption font-semibold text-accent">
              {touch ? t('topology.tapAgainDevice') : t('topology.clickDevice')}
            </div>
          )}
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
    <circle r={3} fill={color} opacity={0.95}>
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
}

export function TopologyMap({ model, apiRef, showLabels, flow, hoverLink, onHoverLink }: TopologyMapProps) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const navigate = useNavigate()
  const { clusters, internetNode, links, peerNodes, relatedTo, routerNodes, wan } = model
  const containerRef = useRef<HTMLDivElement>(null)
  const svgRef = useRef<SVGSVGElement>(null)
  const viewRef = useRef<View>({ ...INITIAL_VIEW })
  const pointers = useRef(new Map<number, { x: number; y: number }>())
  const drag = useRef<{ px: number; py: number } | null>(null)
  const pinch = useRef<{ view0: View; mid0: { x: number; y: number }; dist0: number } | null>(null)
  const moved = useRef(false)

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
    const below = data.kind === 'internet' || data.kind === 'peer'
    const left = clamp(((data.x - v.x) / v.w) * rect.width, 120, rect.width - 120)
    const rawTop = ((data.y - v.y) / v.h) * rect.height
    const top = below ? clamp(rawTop, 16, rect.height - 190) : clamp(rawTop, 190, rect.height - 20)
    setTooltip({ ...data, left, top, below })
  }, [])

  const closeHover = useCallback(() => {
    if (hoverCapable) {
      setTooltip(null)
      setHoverNode(null)
    }
  }, [hoverCapable])

  // -- interacción con nodos ---------------------------------------------------
  const handleNodeHover = useCallback(
    (data: TooltipData, e: ReactPointerEvent) => {
      if (e.pointerType !== 'mouse') return
      setHoverNode(data.id)
      openTooltip(data)
    },
    [openTooltip],
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
  const related = useMemo(() => (hoverNode ? relatedTo(hoverNode) : null), [hoverNode])
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

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  const springPop = (delay: number) =>
    reduce
      ? { duration: 0 }
      : { delay, type: 'spring' as const, stiffness: 260, damping: 20 }

  const linkStroke = (link: TopoLink) =>
    link.kind === 'wg' ? COLOR.tunnel : 'rgb(var(--border-strong))'

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
          <linearGradient id="topo-wan-grad" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={COLOR.accent} stopOpacity="0.9" />
            <stop offset="100%" stopColor={COLOR.accent} stopOpacity="0.35" />
          </linearGradient>
          <linearGradient id="topo-gw-grad" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0%" stopColor={COLOR.accent} stopOpacity="0.28" />
            <stop offset="100%" stopColor={COLOR.tunnel} stopOpacity="0.28" />
          </linearGradient>
        </defs>

        {/* ------------------------- Enlaces ------------------------- */}
        <g>
          {links.map((link, i) => {
            const highlighted = linkHighlighted(link.id)
            const isWg = link.kind === 'wg'
            const drawDelay = link.kind === 'wan' ? 0.25 : link.kind === 'uplink' ? 0.8 + i * 0.15 : 1.7
            return (
              <g key={link.id}>
                {/* base */}
                <motion.path
                  id={`topo-link-${link.id}`}
                  d={link.d}
                  fill="none"
                  stroke={link.kind === 'wan' ? 'url(#topo-wan-grad)' : linkStroke(link)}
                  strokeWidth={link.width}
                  strokeLinecap="round"
                  strokeDasharray={isWg ? '7 7' : undefined}
                  opacity={link.kind === 'cluster' ? 0.5 : 0.9}
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
                  strokeWidth={16}
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

          {/* paquetes animados */}
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
                    color={l.kind === 'wg' ? COLOR.tunnel : COLOR.accent}
                  />
                )),
              )}
        </g>

        {/* ------------------------- Internet ------------------------- */}
        <motion.g
          transform={`translate(${internetNode.x} ${internetNode.y})`}
          initial={reduce ? { opacity: 1 } : { opacity: 0, scale: 0 }}
          animate={{ opacity: nodeOpacity('internet'), scale: 1 }}
          transition={reduce ? { duration: 0.2 } : { ...springPop(0), opacity: { duration: 0.2 } }}
          style={{ transformBox: 'fill-box', transformOrigin: 'center' }}
          className="cursor-pointer outline-none"
          role="button"
          tabIndex={0}
          aria-label={t('topology.internetAria', { isp: wan.isp, plan: wan.plan })}
          onPointerEnter={(e) =>
            handleNodeHover({ kind: 'internet', id: 'internet', x: internetNode.x, y: internetNode.y + 46 }, e)
          }
          onPointerLeave={closeHover}
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
          {!reduce && (
            <motion.circle
              r={52}
              fill={COLOR.accent}
              initial={{ opacity: 0.1 }}
              animate={{ opacity: [0.08, 0.18, 0.08] }}
              transition={{ duration: 5, repeat: Infinity, ease: 'easeInOut' }}
            />
          )}
          <rect x={-46} y={-38} width={92} height={76} rx={18} fill="rgb(var(--elevated))" stroke="rgb(var(--border-strong))" />
          <Cloud x={-24} y={-20} width={48} height={40} className="text-accent" strokeWidth={1.75} aria-hidden />
        </motion.g>

        {/* ------------------------- Routers ------------------------- */}
        {routerNodes.map((node, i) => {
          const isGw = node.id === 'flint2'
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
              {/* halo permanente del gateway / pulso de aviso del Patio */}
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
              {/* entrada escalonada */}
              <motion.g
                initial={reduce ? { opacity: 1 } : { opacity: 0, scale: 0.6 }}
                animate={{ opacity: 1, scale: 1 }}
                transition={reduce ? { duration: 0 } : { delay: 1.8 * T + i * 0.12, duration: 0.35 }}
                style={{ transformBox: 'fill-box', transformOrigin: 'center' }}
              >
                {/* atenuación por hover */}
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
                      <animate attributeName="r" values="20;30" dur="2.2s" repeatCount="indefinite" />
                      <animate attributeName="opacity" values="0.5;0" dur="2.2s" repeatCount="indefinite" />
                    </circle>
                  )}
                  <rect x={-20} y={-20} width={40} height={40} rx={12} fill="rgb(var(--elevated))" stroke={COLOR.tunnel} strokeWidth={1.5} />
                  <PeerIcon x={-10} y={-10} width={20} height={20} className="text-tunnel" strokeWidth={1.75} aria-hidden />
                </motion.g>
              </motion.g>
            </g>
          )
        })}

        {/* ------------------------- Clusters de clientes ------------------------- */}
        {clusters.map((cluster, ci) => (
          <ClusterGroup
            key={cluster.id}
            cluster={cluster}
            baseDelay={(1.4 + ci * 0.15) * T}
            reduce={reduce ?? false}
            opacity={nodeOpacity(cluster.id) * nodeOpacity(cluster.routerId)}
            onDotHover={(dot, e) => {
              if (e.pointerType !== 'mouse') return
              setHoverNode(cluster.routerId)
              openTooltip({ kind: 'dot', id: dot.id, dot, x: dot.x, y: dot.y - 10 })
            }}
            onDotLeave={closeHover}
            onDotClick={(dot, e) => {
              if (moved.current) return
              const data: TooltipData = { kind: 'dot', id: dot.id, dot, x: dot.x, y: dot.y - 10 }
              const navTo = dot.device ? `/devices?q=${encodeURIComponent(dot.device.name)}` : undefined
              nodeClick(e, data, navTo)
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
          <LabelText x={internetNode.x + 56} y={internetNode.y - 4} anchor="start" delay={2.0 * T} reduce={reduce ?? false}
            title={`Internet · ${wan.isp}`} sub={t('routerDetail.wan.fiber', { plan: wan.plan })} />
          {/* Gateway */}
          {model.gatewayNode && (
            <LabelText x={556} y={240} anchor="start" delay={2.05 * T} reduce={reduce ?? false}
              title="Gateway" sub={`${model.gatewayNode.router.modelShort} · ${t('common.clientsCount', { count: model.gatewayNode.router.clients })}`} />
          )}
          {/* APs */}
          {routerNodes.slice(1).map((node, i) => (
            <LabelText key={node.id} x={node.label.x} y={node.label.y} anchor={node.label.anchor}
              delay={(2.1 + i * 0.03) * T} reduce={reduce ?? false}
              title={node.router.name} sub={t('common.clientsCount', { count: node.router.clients })} />
          ))}
          {/* Peers */}
          {peerNodes.map((node, i) => (
            <LabelText key={node.id} x={node.x + 28} y={node.y - 2} anchor="start" delay={(2.2 + i * 0.03) * T}
              reduce={reduce ?? false} title={node.peer.name} sub="VPN" subColor={COLOR.tunnel} />
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
      {tooltip && <TooltipCard tip={tooltip} touch={!hoverCapable} wan={wan} />}
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
// Cluster de clientes
// ---------------------------------------------------------------------------

const ClusterGroup = memo(function ClusterGroup({
  cluster,
  baseDelay,
  reduce,
  opacity,
  onDotHover,
  onDotLeave,
  onDotClick,
}: {
  cluster: Cluster
  baseDelay: number
  reduce: boolean
  opacity: number
  onDotHover: (dot: ClientDot, e: ReactPointerEvent) => void
  onDotLeave: () => void
  onDotClick: (dot: ClientDot, e: { stopPropagation: () => void }) => void
}) {
  return (
    <motion.g animate={{ opacity }} transition={{ duration: 0.2 }}>
      {cluster.dots.map((dot, i) => (
        <motion.circle
          key={dot.id}
          cx={dot.x}
          cy={dot.y}
          r={5}
          fill={bandColor(dot.band, dot.weak)}
          stroke="rgb(var(--canvas))"
          strokeWidth={1.5}
          className="cursor-pointer outline-none"
          role="button"
          tabIndex={0}
          aria-label={dot.device ? `${dot.device.name}, ${dot.band}` : `Cliente ${dot.band}`}
          initial={reduce ? { scale: 1 } : { scale: 0 }}
          animate={{ scale: 1 }}
          transition={
            reduce
              ? { duration: 0 }
              : { delay: baseDelay + i * 0.02, type: 'spring', stiffness: 300, damping: 18 }
          }
          style={{ transformBox: 'fill-box', transformOrigin: 'center' }}
          onPointerEnter={(e) => onDotHover(dot, e)}
          onPointerLeave={onDotLeave}
          onClick={(e) => onDotClick(dot, e)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              onDotClick(dot, e)
            }
          }}
        >
          <title>{dot.device?.name ?? `Cliente ${dot.band}`}</title>
        </motion.circle>
      ))}
      {cluster.extra > 0 && (
        <motion.g
          initial={reduce ? { opacity: 1 } : { opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ delay: baseDelay + 0.3, duration: reduce ? 0 : 0.3 }}
        >
          <rect
            x={cluster.cx - 17}
            y={cluster.cy + 56}
            width={34}
            height={18}
            rx={9}
            fill="rgb(var(--elevated))"
            stroke="rgb(var(--border-strong))"
          />
          <text
            x={cluster.cx}
            y={cluster.cy + 69}
            textAnchor="middle"
            fontSize={10}
            fontWeight={600}
            className="fill-text-secondary font-mono"
          >
            +{cluster.extra}
          </text>
        </motion.g>
      )}
    </motion.g>
  )
})
