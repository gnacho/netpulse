// Faceplate.tsx - renderer SVG de una plantilla de faceplate. Bandas no
// solapadas: LED a la izquierda, labelBox, artwork del equipo y puertos como
// botones HTML (targets de click reales, tooltip y hover) en coords 0..1.
import { useTranslation } from 'react-i18next'
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
  const ledColor =
    status === 'online' ? 'fill-emerald-400' : status === 'offline' ? 'fill-red-400' : 'fill-zinc-500'
  const bg = color || 'rgb(var(--elevated))'

  return (
    <div
      className="relative h-full w-full overflow-hidden rounded-[3px] border border-black/40 shadow-inner"
      style={{ background: bg }}
      data-faceplate={plate.id}
    >
      <svg className="absolute inset-0 h-full w-full" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden>
        {/* LED de estado, fijo a la izquierda */}
        <circle cx={plate.led.x * 100} cy={plate.led.y * 100} r={Math.max(plate.led.r * 100, 1.4)} className={ledColor} />
        {/* labelBox: nombre, clipped */}
        <foreignObject x={plate.labelBox.x * 100} y={plate.labelBox.y * 100} width={plate.labelBox.w * 100} height={plate.labelBox.h * 100}>
          <div
            className="flex h-full items-center overflow-hidden whitespace-nowrap text-[9px] font-semibold leading-none tracking-wide"
            style={{ color: 'rgb(var(--text-secondary))' }}
          >
            <span className="truncate">{label || t(plate.nameKey)}</span>
          </div>
        </foreignObject>
        {/* artwork mínimo por tipo de equipo */}
        <Artwork kind={plate.artwork} />
      </svg>

      {/* Puertos: botiones HTML sobre las coords 0..1 */}
      {(patchFacing || portsVisible) &&
        ports.map((p) => {
          const st = portState(p.id)
          const visible = patchFacing || portsVisible || st.cabled || st.drafting
          if (!visible) return null
          const sfp = p.kind !== 'rj45'
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
              className={
                'nodrag absolute flex -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-[2px] transition-colors ' +
                (sfp ? 'h-[10px] w-[16px]' : 'h-3 w-2.5') +
                (st.drafting
                  ? ' bg-amber-400 ring-2 ring-amber-300'
                  : st.cabled
                    ? ' bg-emerald-500 hover:bg-emerald-400'
                    : st.interactive
                      ? ' bg-zinc-600 hover:bg-accent'
                      : ' bg-zinc-700')
              }
              style={{ left: `${p.x * 100}%`, top: `${p.y * 100}%` }}
            />
          )
        })}
    </div>
  )
}

/** Artwork declarativo mínimo: pistas visuales por tipo, sin iconografía. */
function Artwork({ kind }: { kind: string }) {
  const stroke = 'rgba(255,255,255,0.10)'
  const common = { fill: 'none', stroke, strokeWidth: 1, vectorEffect: 'non-scaling-stroke' as const }
  switch (kind) {
    case 'server':
      return (
        <g>
          <rect x="42" y="18" width="52" height="10" rx="1.5" {...common} />
          <rect x="42" y="72" width="52" height="10" rx="1.5" {...common} />
        </g>
      )
    case 'switch':
      return <rect x="24" y="12" width="70" height="76" rx="2" {...common} />
    case 'router':
      return (
        <g>
          <line x1="40" y1="20" x2="88" y2="20" {...common} />
          <line x1="40" y1="80" x2="88" y2="80" {...common} />
        </g>
      )
    case 'nas':
      return (
        <g>
          {[0, 1, 2].map((i) => (
            <rect key={i} x="18" y={28 + i * 16} width="64" height="10" rx="1.5" {...common} />
          ))}
        </g>
      )
    case 'ups':
      return (
        <g>
          <rect x="10" y="20" width="80" height="60" rx="3" {...common} />
          <rect x="14" y="24" width="30" height="52" rx="2" {...common} />
        </g>
      )
    case 'pdu':
      return (
        <g>
          {[0, 1, 2, 3, 4, 5].map((i) => (
            <circle key={i} cx={70 + (i % 3) * 9} cy={i < 3 ? 38 : 62} r="2.4" {...common} />
          ))}
        </g>
      )
    case 'shelf':
      return <line x1="4" y1="88" x2="96" y2="88" {...common} />
    case 'panel':
      return <rect x="22" y="10" width="42" height="80" rx="2" {...common} />
    case 'blank':
    default:
      return null
  }
}
