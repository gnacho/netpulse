/**
 * NetPulse — Página Topología `/topology` (topology.md).
 * Header con controles → mapa SVG animado (pan/zoom) → leyenda + enlaces.
 */
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { motion, useReducedMotion } from 'framer-motion'
import { ChevronRight, Maximize, RefreshCw, ZoomIn, ZoomOut } from 'lucide-react'
import { Switch } from '@/components/ui/switch'
import { LegendCard, LegendSheet } from '@/components/topology/LegendCard'
import { LinksTable } from '@/components/topology/LinksTable'

import { TopologyMap } from '@/components/topology/TopologyMap'
import type { TopologyMapApi } from '@/components/topology/TopologyMap'
import { buildTopologyModel } from '@/components/topology/model'
import { useNetPulse } from '@/data/DataProvider'

const EASE = [0.16, 1, 0.3, 1] as [number, number, number, number]

function ControlButton({
  label,
  onClick,
  disabled,
  children,
}: {
  label: string
  onClick?: () => void
  disabled?: boolean
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      title={label}
      className="flex h-9 w-9 items-center justify-center rounded-lg text-text-secondary transition-colors duration-150 hover:bg-hover hover:text-text-primary active:bg-hover disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent disabled:hover:text-text-secondary"
    >
      {children}
    </button>
  )
}

export default function Topology() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const { routers, devices, wan, wireguard, distributionNodes, topology, vm, lastSnapshotAt, requestServerRefresh } =
    useNetPulse()
  const model = useMemo(
    () => buildTopologyModel({ routers, devices, wan, wireguard, distributionNodes, topology, vm }),
    [routers, devices, wan, wireguard, distributionNodes, topology, vm],
  )
  const [showLabels, setShowLabels] = useState(true)
  const [flow, setFlow] = useState(true)
  const [hoverLink, setHoverLink] = useState<string | null>(null)
  const mapApi = useRef<TopologyMapApi>({})

  // Botón "Refrescar": POST /api/refresh → el backend sondea ya y empuja el
  // snapshot por SSE. El icono gira hasta que llega ese snapshot (sin recargar
  // ni re-montar el grafo) o hasta el timeout de seguridad de 10 s.
  const [refreshing, setRefreshing] = useState(false)
  const refreshStartRef = useRef(0)
  const refreshTimeoutRef = useRef<number | undefined>(undefined)

  const stopRefreshSpin = () => {
    window.clearTimeout(refreshTimeoutRef.current)
    refreshTimeoutRef.current = undefined
    refreshStartRef.current = 0
    setRefreshing(false)
  }

  useEffect(() => {
    if (refreshing && refreshStartRef.current > 0 && lastSnapshotAt >= refreshStartRef.current) {
      stopRefreshSpin()
    }
  }, [lastSnapshotAt, refreshing])

  useEffect(() => () => window.clearTimeout(refreshTimeoutRef.current), [])

  const handleRefresh = () => {
    if (refreshing) return
    setRefreshing(true)
    refreshStartRef.current = Date.now()
    void requestServerRefresh().then((waiting) => {
      if (waiting) {
        // 202/429: espera al próximo snapshot SSE (o al timeout de 10 s)
        refreshTimeoutRef.current = window.setTimeout(stopRefreshSpin, 10_000)
      } else {
        // Demo local o petición fallida: no llegará snapshot; spin breve
        refreshTimeoutRef.current = window.setTimeout(stopRefreshSpin, 800)
      }
    })
  }

  const controlMotion = (i: number) =>
    reduce
      ? {}
      : {
          initial: { opacity: 0, x: 12 },
          animate: { opacity: 1, x: 0 },
          transition: { delay: 0.1 + i * 0.06, duration: 0.3, ease: EASE },
        }

  const zoomControls = (
    <>
      <ControlButton label={t('topology.zoomIn')} onClick={() => mapApi.current.zoomIn?.()}>
        <ZoomIn className="h-[18px] w-[18px]" strokeWidth={1.75} />
      </ControlButton>
      <ControlButton label={t('topology.zoomOut')} onClick={() => mapApi.current.zoomOut?.()}>
        <ZoomOut className="h-[18px] w-[18px]" strokeWidth={1.75} />
      </ControlButton>
      <ControlButton label={t('topology.resetView')} onClick={() => mapApi.current.reset?.()}>
        <Maximize className="h-[18px] w-[18px]" strokeWidth={1.75} />
      </ControlButton>
    </>
  )

  return (
    <div className="space-y-4 md:space-y-5">
      {/* ① Page header */}
      <header className="flex flex-wrap items-end justify-between gap-x-4 gap-y-3">
        <motion.div
          initial={reduce ? undefined : { opacity: 0, y: 12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={reduce ? { duration: 0 } : { duration: 0.3, ease: EASE }}
        >
          <nav aria-label={t('common.breadcrumb')} className="mb-1 flex items-center gap-1 text-caption text-text-muted">
            <Link to="/" className="transition-colors hover:text-accent">
              {t('common.home')}
            </Link>
            <ChevronRight className="h-3 w-3" strokeWidth={1.75} aria-hidden />
            <span className="text-text-secondary" aria-current="page">
              {t('nav.topology')}
            </span>
          </nav>
          <h1 className="font-display text-h1 text-text-primary">{t('nav.topology')}</h1>
          <p className="mt-0.5 text-sm text-text-secondary">{t('topology.subtitle')}</p>
        </motion.div>

        <div className="flex flex-wrap items-center gap-2">
          <motion.div
            {...controlMotion(0)}
            className="flex items-center gap-0.5 rounded-xl border border-border bg-surface p-1"
          >
            <ControlButton
              label={refreshing ? t('topology.refreshing') : t('topology.refreshNow')}
              onClick={handleRefresh}
              disabled={refreshing}
            >
              <RefreshCw
                className={`h-[18px] w-[18px] ${refreshing ? 'animate-spin' : ''}`}
                strokeWidth={1.75}
              />
            </ControlButton>
          </motion.div>
          <motion.div
            {...controlMotion(1)}
            className="flex items-center gap-0.5 rounded-xl border border-border bg-surface p-1"
            role="group"
            aria-label={t('topology.zoomControls')}
          >
            {zoomControls}
          </motion.div>
          <motion.label
            {...controlMotion(2)}
            className="flex cursor-pointer items-center gap-2 rounded-xl border border-border bg-surface px-3 py-2 text-sm font-medium text-text-secondary"
          >
            <Switch checked={showLabels} onCheckedChange={setShowLabels} aria-label={t('topology.showLabels')} />
            {t('topology.labels')}
          </motion.label>
          <motion.label
            {...controlMotion(3)}
            className="flex cursor-pointer items-center gap-2 rounded-xl border border-border bg-surface px-3 py-2 text-sm font-medium text-text-secondary"
          >
            <Switch checked={flow} onCheckedChange={setFlow} aria-label={t('topology.animateFlow')} />
            {t('topology.flow')}
          </motion.label>
        </div>
      </header>

      {/* ② Map canvas */}
      <section
        className="relative h-[70dvh] min-h-[420px] rounded-2xl border border-border bg-canvas lg:h-[600px]"
        aria-label={t('topology.interactiveMap')}
      >
        <div className="mesh-bg pointer-events-none absolute inset-0 overflow-hidden rounded-2xl" aria-hidden />
        <TopologyMap
          model={model}
          apiRef={mapApi}
          showLabels={showLabels}
          flow={flow}
          hoverLink={hoverLink}
          onHoverLink={setHoverLink}
        />
        {/* Zoom flotante (móvil: los controles del header quedan lejos del mapa) */}
        <div
          className="absolute right-3 top-3 z-10 flex flex-col gap-0.5 rounded-xl border border-border bg-elevated/90 p-1 backdrop-blur-md lg:hidden"
          onPointerDown={(e) => e.stopPropagation()}
        >
          {zoomControls}
        </div>
        {/* ③ Leyenda como bottom sheet en móvil */}
        <LegendSheet model={model} />
      </section>



      {/* ③ + ④ Leyenda y enlaces (desktop: 5/7 cols; móvil: solo enlaces en lista) */}
      <div className="grid grid-cols-1 gap-4 md:gap-5 lg:grid-cols-12">
        <div className="hidden lg:col-span-5 lg:block">
          <LegendCard model={model} />
        </div>
        <div className="lg:col-span-7">
          <LinksTable model={model} hoverLink={hoverLink} onHoverLink={setHoverLink} />
        </div>
      </div>
    </div>
  )
}
