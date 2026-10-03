// Faceplate.tsx - renderer de la cara frontal del equipo. Diseño plano y
// limpio: LED de estado en punto nítido (HTML), un único nombre que escala
// con la altura U y nunca invade la zona de bocas, y bocas con el icono
// EthernetPort de Lucide (SFP = ranura rectangular). Un nombre, un LED,
// las bocas.
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { EthernetPort } from 'lucide-react'
import type { FaceplateTemplate, FaceplatePort } from '@/lib/rackFaceplates'

export interface PortState {
  /** el puerto tiene al menos un cable */
  cabled: boolean
  /** es el origen del draft de patch */
  drafting: boolean
  /** puede pincharse ahora (modo patch) */
  interactive: boolean
}

export function Faceplate({
  plate,
  ports,
  label,
  color,
  status,
  patchFacing,
  portsVisible,
  portState,
  onPortClick,
  onPortEnter,
  onPortLeave,
}: {
  plate: FaceplateTemplate
  ports: FaceplatePort[]
  label: string
  color?: string
  status: 'online' | 'offline' | 'unknown'
  /** true = puertos siempre visibles (equipos patch-facing) */
  patchFacing: boolean
  /** false = puertos ocultos salvo cable (modo no-patch, no hover) */
  portsVisible: boolean
  portState: (portId: string) => PortState
  onPortClick?: (portId: string) => void
  onPortEnter?: (portId: string) => void
  onPortLeave?: (portId: string) => void
}) {
  const { t } = useTranslation()
  const minPortX = ports.length > 0 ? Math.min(...ports.map((p) => p.x)) : 1
  // Tamaño de icono según el hueco real: 48 bocas en 1U deben caber sin
  // superponerse (pitch mínimo entre bocas de la misma fila).
  const iconSize = useMemo(() => {
    if (ports.length < 2) return 14
    let minPitch = 1
    const byY = new Map<number, number[]>()
    for (const p of ports) {
      const key = Math.round(p.y * 20)
      byY.set(key, [...(byY.get(key) ?? []), p.x].sort((a, b) => a - b))
    }
    for (const xs of byY.values()) {
      for (let i = 1; i < xs.length; i++) {
        const a = xs[i]
        const b = xs[i - 1]
        if (a !== undefined && b !== undefined) minPitch = Math.min(minPitch, a - b)
      }
    }
    return Math.max(6, Math.min(14, Math.round(minPitch * 100 * 0.85)))
  }, [ports])
  const ledColor =
    status === 'online'
      ? 'bg-emerald-400'
      : status === 'offline'
        ? 'bg-red-400'
        : 'bg-zinc-500'
  const bg = color || 'rgb(var(--elevated))'

  return (
    <div
      className="relative h-full w-full overflow-hidden rounded-[2px] border border-black/30"
      style={{ background: bg, containerType: 'size' }}
      data-faceplate={plate.id}
    >
      {/* LED de estado: punto nítido de tamaño fijo */}
      <div
        className={'absolute rounded-full ' + ledColor}
        style={{
          left: 'max(6px, 3%)',
          top: `${plate.led.y * 100}%`,
          width: 8,
          height: 8,
          transform: 'translateY(-50%)',
        }}
        aria-hidden
      />

      {/* Nombre + conteo de bocas: la banda termina antes de la primera
          boca real, así nunca se montan unos sobre otros. */}
      <div
        className="absolute flex items-center gap-[0.6em] overflow-hidden whitespace-nowrap font-semibold leading-none"
        style={{
          left: 'max(18px, 9%)',
          top: `${plate.labelBox.y * 100}%`,
          height: `${plate.labelBox.h * 100}%`,
          width: `${Math.max(0.12, minPortX - plate.labelBox.x - 0.06) * 100}%`,
          fontSize: 'min(21cqh, 13px)',
          color: 'rgb(var(--text-secondary))',
        }}
      >
        <span className="truncate">{label || t(plate.nameKey)}</span>
        {ports.length > 0 && (
          <span style={{ fontSize: '0.72em', color: 'rgb(var(--text-muted))' }} title={t('rack.portCount')}>
            ×{ports.length}
          </span>
        )}
      </div>

      {/* Bocas: siluetas planas sobre las coords 0..1 de la plantilla */}
      {(patchFacing || portsVisible) &&
        ports.map((p) => {
          const st = portState(p.id)
          const visible = patchFacing || portsVisible || st.cabled || st.drafting
          if (!visible) return null
          const sfp = p.kind !== 'rj45'
          const tone = st.drafting
            ? 'text-amber-400'
            : st.cabled
              ? 'text-emerald-500 hover:text-emerald-400'
              : st.interactive
                ? 'text-zinc-500 hover:text-accent'
                : 'text-zinc-600'
          return (
            <button
              key={p.id}
              type="button"
              tabIndex={st.interactive ? 0 : -1}
              aria-label={p.id}
              title={`${p.id} · ${p.kind}`}
              onClick={(e) => {
                e.stopPropagation()
                if (st.interactive) onPortClick?.(p.id)
              }}
              onMouseEnter={() => onPortEnter?.(p.id)}
              onMouseLeave={() => onPortLeave?.(p.id)}
              className={'nodrag absolute flex -translate-x-1/2 -translate-y-1/2 items-center justify-center ' + tone}
              style={{ left: `${p.x * 100}%`, top: `${p.y * 100}%` }}
            >
              {sfp ? (
                <span
                  className="block rounded-[1px] bg-current"
                  style={{ width: Math.max(9, iconSize + 1), height: Math.max(4, Math.round(iconSize / 3)) }}
                  aria-hidden
                />
              ) : (
                <EthernetPort size={iconSize} strokeWidth={2.2} aria-hidden />
              )}
            </button>
          )
        })}
    </div>
  )
}
