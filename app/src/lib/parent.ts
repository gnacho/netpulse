import { useMemo } from 'react'
import { useNetPulse } from '@/data/DataProvider'

/**
 * What a client actually hangs off: the AP or switch its controller reports,
 * a hypervisor host, or null when the router is all we know.
 *
 * The router alone says "OpenWrt" for every client in the house, which is
 * true and useless once there is a switch and a couple of APs in between.
 * `attachTo` points at either a distribution node (switch, AP, hypervisor)
 * or another device acting as a hub, so both are resolved here.
 */
export function useParentName(attachTo: string | undefined): string | null {
  const { distributionNodes, devices } = useNetPulse()
  return useMemo(() => {
    if (!attachTo) return null
    const node = distributionNodes.find((n) => n.id === attachTo)
    if (node) return node.name ?? node.ip ?? null
    return devices.find((d) => d.id === attachTo)?.name ?? null
  }, [attachTo, distributionNodes, devices])
}
