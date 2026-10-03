// RackNode.tsx - nodo rack: cabecera con nombre, raíles con numeración U
// (bottom-up por defecto; la opción numbering solo cambia etiquetas, nunca
// la geometría), interior con rejilla y ghost de drop.
import { memo } from 'react'
import type { NodeProps, Node } from '@xyflow/react'
import { COL_PX, U_PX, RAIL_PX, interiorWidthPx } from './layout'
import type { GhostState } from './types'

export type RackNodeData = {
  name: string
  uHeight: number
  numbering: string
  selected: boolean
  ghost: GhostState | null
  onSelect?: () => void
}

export type RackNodeType = Node<RackNodeData, 'rack'>

export const RackNode = memo(function RackNode({ data }: NodeProps<RackNodeType>) {
  const { name, uHeight, numbering, selected, ghost } = data
  const interiorW = interiorWidthPx()
  const topDown = numbering === 'top-down'

  return (
    <div
      className={
        'relative rounded-md border bg-surface transition-shadow ' +
        (selected ? 'border-accent shadow-[0_0_0_2px_rgb(var(--accent))]' : 'border-border')
      }
      style={{ width: RAIL_PX * 2 + interiorW, height: 22 + uHeight * U_PX + 4 }}
      onClick={(e) => {
        e.stopPropagation()
        data.onSelect?.()
      }}
    >
      {/* Cabecera: nombre del rack */}
      <div className="flex h-[22px] items-center justify-center truncate px-2 text-[11px] font-semibold text-text-secondary">
        {name}
      </div>

      {/* Raíl izquierdo: números de U. La U 1 está abajo siempre. */}
      <div className="absolute left-0 top-[22px] bottom-[4px] w-[18px]">
        {Array.from({ length: uHeight }, (_, i) => {
          const labelU = topDown ? uHeight - i : i + 1
          return (
            <div
              key={i}
              className="flex items-center justify-end pr-[3px] text-[9px] leading-none text-text-secondary/60"
              style={{ height: U_PX, marginTop: 0 }}
            >
              {labelU}
            </div>
          )
        })}
      </div>
      {/* Raíl derecho (simétrico, sin números) */}
      <div className="absolute right-0 top-[22px] bottom-[4px] w-[18px] border-l border-border/40" />

      {/* Interior con rejilla: línea por U + separadores de fracciones */}
      <div
        className="absolute border border-border/60"
        style={{
          left: RAIL_PX,
          top: 22,
          width: interiorW,
          height: uHeight * U_PX,
          backgroundImage:
            'repeating-linear-gradient(to bottom, transparent 0, transparent ' +
            (U_PX - 1) +
            'px, rgb(var(--border)) ' +
            (U_PX - 1) +
            'px, rgb(var(--border)) ' +
            U_PX +
            'px)',
          backgroundSize: '100% ' + U_PX + 'px',
        }}
      >
        {/* marcas de fracciones de ancho: 1/2, 1/3, 1/4 */}
        {[1 / 2, 1 / 3, 2 / 3, 1 / 4, 3 / 4].map((f) => (
          <div
            key={f}
            className="absolute top-0 h-full w-px bg-border/30"
            style={{ left: f * interiorW }}
          />
        ))}

        {/* Ghost de drop: verde = encaja, rojo = imposible */}
        {ghost && (
          <div
            className={
              'pointer-events-none absolute rounded-[3px] border-2 ' +
              (ghost.valid ? 'border-emerald-400/80 bg-emerald-400/10' : 'border-red-400/80 bg-red-400/10')
            }
            style={{
              left: ghost.colStart * COL_PX,
              top: (uHeight - ghost.uStart - ghost.uHeight + 1) * U_PX,
              width: ghost.colSpan * COL_PX,
              height: ghost.uHeight * U_PX,
            }}
          />
        )}
      </div>
    </div>
  )
})
