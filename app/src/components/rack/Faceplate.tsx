// Faceplate.tsx - renderer de la cara frontal del equipo. Diseño plano y
// limpio: LED de estado en punto nítido (HTML, no SVG estirado), nombre a
// tamaño legible que escala con la altura U, y bocas como siluetas RJ45
// (muesca de traba) o SFP rectangulares. Nada de monogramas ni dobles
// textos: un nombre, un LED, las bocas.
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

/** RJ45 plano: rect con la muesca de traba recortada arriba (clip-path). */
const RJ45_CLIP = 'polygon(0 42%, 28% 42%, 28% 0, 72% 0, 72% 42%, 100% 42%, 100% 100%, 0 100%)'

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

      {/* Nombre + conteo de bocas detectadas (único texto) */}
      <div
        className="absolute flex items-center gap-[0.6em] overflow-hidden whitespace-nowrap font-semibold leading-none"
        style={{
          left: 'max(18px, 9%)',
          top: `${plate.labelBox.y * 100}%`,
          height: `${plate.labelBox.h * 100}%`,
          right: `${(1 - plate.labelBox.x - plate.labelBox.w) * 100}%`,
          fontSize: 'min(26cqh, 15px)',
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
                'nodrag absolute flex -translate-x-1/2 -translate-y-1/2 items-center justify-center transition-colors ' +
                (st.drafting
                  ? ' bg-amber-400'
                  : st.cabled
                    ? ' bg-emerald-500 hover:bg-emerald-400'
                    : st.interactive
                      ? ' bg-zinc-500 hover:bg-accent'
                      : ' bg-zinc-600')
              }
              style={{
                left: `${p.x * 100}%`,
                top: `${p.y * 100}%`,
                ...(sfp
                  ? { width: '11%', height: '9%', minWidth: 14, minHeight: 5, borderRadius: 1 }
                  : { width: '7%', height: '12%', minWidth: 8, minHeight: 7, clipPath: RJ45_CLIP }),
              }}
            />
          )
        })}
    </div>
  )
}
