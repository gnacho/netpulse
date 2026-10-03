// MountNode.tsx - nodo montaje: faceplate SVG + selección. Los puertos
// viven en la Faceplate; los eventos llegan vía node.data.
import { memo } from 'react'
import type { NodeProps, Node } from '@xyflow/react'
import { X } from 'lucide-react'
import { Faceplate, type PortState } from './Faceplate'
import type { FaceplateTemplate, FaceplatePort } from '@/lib/rackFaceplates'

export type MountNodeData = {
  plate: FaceplateTemplate
  ports: FaceplatePort[]
  label: string
  color?: string
  status: 'online' | 'offline' | 'unknown'
  patchFacing: boolean
  portsVisible: boolean
  selected: boolean
  /** disponible para admin: botón desmontar en la esquina */
  onUnmount?: () => void
  /** clic en el montaje: selección propia (elementsSelectable off) */
  onSelect?: () => void
  portState: (portId: string) => PortState
  onPortClick?: (portId: string) => void
  onPortEnter?: (portId: string) => void
  onPortLeave?: (portId: string) => void
}

export type MountNodeType = Node<MountNodeData, 'mount'>

export const MountNode = memo(function MountNode({ data }: NodeProps<MountNodeType>) {
  // selected viene de nuestro modelo (data.selected): el rebuild de nodos
  // reemplaza los objetos y pisaría el estado de selección de React Flow.
  return (
    <div
      className={'h-full w-full transition-shadow ' + (data.selected ? 'rounded-[2px] ring-2 ring-accent' : '')}
      data-testid="mount-node"
      onClick={(e) => {
        e.stopPropagation()
        data.onSelect?.()
      }}
    >
      {data.selected && data.onUnmount && (
        <button
          type="button"
          aria-label="unmount"
          title="unmount"
          onClick={(e) => {
            e.stopPropagation()
            data.onUnmount?.()
          }}
          className="nodrag absolute -right-1.5 -top-1.5 z-10 flex h-5 w-5 items-center justify-center rounded-full bg-danger text-white shadow hover:bg-danger/90"
        >
          <X size={12} />
        </button>
      )}
      <Faceplate
        plate={data.plate}
        ports={data.ports}
        label={data.label}
        color={data.color}
        status={data.status}
        patchFacing={data.patchFacing}
        portsVisible={data.portsVisible}
        portState={data.portState}
        onPortClick={data.onPortClick}
        onPortEnter={data.onPortEnter}
        onPortLeave={data.onPortLeave}
      />
    </div>
  )
})
