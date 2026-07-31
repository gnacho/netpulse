/**
 * NetPulse — Página Topología `/topology` (topology.md).
 * Header con controles → mapa SVG animado (pan/zoom) → leyenda + enlaces.
 */
import { useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { motion, useReducedMotion } from 'framer-motion'
import { ChevronRight, Maximize, ZoomIn, ZoomOut } from 'lucide-react'
import { Switch } from '@/components/ui/switch'
import { LegendCard, LegendSheet } from '@/components/topology/LegendCard'
import { LinksTable } from '@/components/topology/LinksTable'
import { DawnPanel } from '@/components/topology/DawnPanel'
import { TopologyMap } from '@/components/topology/TopologyMap'
import type { TopologyMapApi } from '@/components/topology/TopologyMap'
import { buildTopologyModel } from '@/components/topology/model'
import { useNetPulse } from '@/data/DataProvider'

const EASE = [0.16, 1, 0.3, 1] as [number, number, number, number]

function ControlButton({
  label,
  onClick,
  children,
}: {
  label: string
  onClick?: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={label}
      className="flex h-9 w-9 items-center justify-center rounded-lg text-text-secondary transition-colors duration-150 hover:bg-hover hover:text-text-primary active:bg-hover"
    >
      {children}
    </button>
  )
}

export default function Topology() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const { routers, devices, wan, wireguard } = useNetPulse()
  const model = useMemo(
    () => buildTopologyModel({ routers, devices, wan, wireguard }),
    [routers, devices, wan, wireguard],
  )
  const [showLabels, setShowLabels] = useState(true)
  const [flow, setFlow] = useState(true)
  const [hoverLink, setHoverLink] = useState<string | null>(null)
  const mapApi = useRef<TopologyMapApi>({})

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
            role="group"
            aria-label={t('topology.zoomControls')}
          >
            {zoomControls}
          </motion.div>
          <motion.label
            {...controlMotion(1)}
            className="flex cursor-pointer items-center gap-2 rounded-xl border border-border bg-surface px-3 py-2 text-sm font-medium text-text-secondary"
          >
            <Switch checked={showLabels} onCheckedChange={setShowLabels} aria-label={t('topology.showLabels')} />
            {t('topology.labels')}
          </motion.label>
          <motion.label
            {...controlMotion(2)}
            className="flex cursor-pointer items-center gap-2 rounded-xl border border-border bg-surface px-3 py-2 text-sm font-medium text-text-secondary"
          >
            <Switch checked={flow} onCheckedChange={setFlow} aria-label={t('topology.animateFlow')} />
            {t('topology.flow')}
          </motion.label>
        </div>
      </header>

      {/* ② Map canvas */}
      <section
        className="relative h-[70dvh] min-h-[420px] overflow-hidden rounded-2xl border border-border bg-canvas lg:h-[600px]"
        aria-label={t('topology.interactiveMap')}
      >
        <div className="mesh-bg pointer-events-none absolute inset-0" aria-hidden />
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

      {/* ⑤ DAWN (roaming/band-steering) */}
      <DawnPanel />

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
