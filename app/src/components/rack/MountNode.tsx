// MountNode.tsx - nodo montaje: faceplate SVG + selección. Los puertos
// viven en la Faceplate; los eventos llegan vía node.data.
import { memo } from 'react'
import type { NodeProps, Node } from '@xyflow/react'
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
  portState: (portId: string) => PortState
  onPortClick?: (portId: string) => void
  onPortEnter?: (portId: string) => void
  onPortLeave?: (portId: string) => void
}

export type MountNodeType = Node<MountNodeData, 'mount'>

export const MountNode = memo(function MountNode({ data, selected }: NodeProps<MountNodeType>) {
  return (
    <div
      className={'h-full w-full transition-shadow ' + (selected ? 'rounded-[3px] shadow-[0_0_0_2px_rgb(var(--accent))]' : '')}
      data-testid="mount-node"
    >
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
