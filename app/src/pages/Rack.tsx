/**
 * NetPulse - Página Rack `/rack` (rack canvas, fase 1).
 * Canvas con racks y montajes (snap al grid, save explícito), patch de
 * cables port-to-port y visibilidad de cables. Dominio en lib/rackGeometry
 * + lib/rackFaceplates; wire en lib/rackApi (snake_case).
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  ReactFlow,
  ReactFlowProvider,
  useNodesState,
  useReactFlow,
  Background,
  BackgroundVariant,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Cable, Cctv, Link2, Monitor, Network, Pencil, Plus, Router as RouterIcon, Save, Server, Trash2, TriangleAlert } from 'lucide-react'
import { toast } from 'sonner'
import { SectionHeader } from '@/components/SectionHeader'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { SegmentedControl } from '@/components/SegmentedControl'
import { RackNode, type RackNodeType } from '@/components/rack/RackNode'
import { MountNode, type MountNodeType } from '@/components/rack/MountNode'
import { COL_PX, U_PX, mountRelPos, rackHeightPx, rackWidthPx, relPosToCell } from '@/components/rack/layout'
import type { CableVisibility, GhostState } from '@/components/rack/types'
import { findSlot, type Footprint } from '@/lib/rackGeometry'
import { FACEPLATES, getFaceplate, layoutPhysicalPorts, seedPorts, suggestFaceplate, type RackPortKind } from '@/lib/rackFaceplates'
import * as api from '@/lib/rackApi'
import type { CableDTO, MountDTO, ProfileDTO, RackDTO } from '@/lib/rackApi'
import type { Router } from '@/data/types'
import { useNetPulse } from '@/data/DataProvider'
import { useServicesVisibility } from '@/hooks/useServicesVisibility'
import { useAuth } from '@/data/AuthContext'

const nodeTypes = { rack: RackNode, mount: MountNode }

interface Working {
  racks: RackDTO[]
  mounts: MountDTO[]
  cables: CableDTO[]
  profiles: Map<string, ProfileDTO>
  audit: Record<string, string>
}

let localSeq = 0
const localId = (p: string) => `${p}-local-${++localSeq}`

function RackCanvas() {
  const { t } = useTranslation()
  const np = useNetPulse()
  const services = useServicesVisibility()[0]
  const auth = useAuth()
  const isAdmin = auth?.role === 'admin'

  const [working, setWorking] = useState<Working>({ racks: [], mounts: [], cables: [], profiles: new Map(), audit: {} })
  const [loaded, setLoaded] = useState(false)
  const [activeRackId, setActiveRackId] = useState<string | null>(null)
  const [patchMode, setPatchMode] = useState(false)
  const [draft, setDraft] = useState<{ mountId: string; portId: string } | null>(null)
  const [visibility, setVisibility] = useState<CableVisibility>('always')
  const [hoverMountId, setHoverMountId] = useState<string | null>(null)
  const [selectedCableId, setSelectedCableId] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const [editOpen, setEditOpen] = useState(false)
  const [editName, setEditName] = useState('')
  const [editU, setEditU] = useState('12')
  const [editLocation, setEditLocation] = useState('')
  const [addName, setAddName] = useState('')
  const [addU, setAddU] = useState('12')
  const [addLocation, setAddLocation] = useState('')
  const [deleteRackOpen, setDeleteRackOpen] = useState(false)
  const [selectedMountId, setSelectedMountId] = useState<string | null>(null)
  // Diálogo de colocación: se pregunta SIEMPRE (altura U; puertos si es
  // patch panel), venga del picker por clic o por drag & drop.
  const [mountAsk, setMountAsk] = useState<{
    rackId: string
    mac?: string
    deviceType: string
    label: string
    faceplateId?: string
    physicalPorts?: { id: string; kind: RackPortKind }[]
    defaultU: number
    isPatch: boolean
    /** punto de drop en coords relativas al rack (coloca donde se soltó) */
    dropRel?: { x: number; y: number }
  } | null>(null)
  const [askU, setAskU] = useState('1')
  const [askPorts, setAskPorts] = useState('24')

  // Dirty tracking (todo lo que Save debe persistir).
  const [newMountIds, setNewMountIds] = useState<Set<string>>(new Set())
  const [movedMountIds, setMovedMountIds] = useState<Set<string>>(new Set())
  const [deletedMounts, setDeletedMounts] = useState<{ id: string; rackId: string }[]>([])
  const [newCables, setNewCables] = useState<Set<string>>(new Set())
  const [deletedCables, setDeletedCables] = useState<Set<string>>(new Set())
  const [profileChanges, setProfileChanges] = useState<Map<string, ProfileDTO>>(new Map())
  const [rackMoves, setRackMoves] = useState<Map<string, { x: number; y: number }>>(new Map())

  const dirty =
    newMountIds.size > 0 ||
    movedMountIds.size > 0 ||
    deletedMounts.length > 0 ||
    newCables.size > 0 ||
    deletedCables.size > 0 ||
    profileChanges.size > 0 ||
    rackMoves.size > 0

  const draggingRef = useRef(false)
  const { setViewport, screenToFlowPosition } = useReactFlow()

  // Altura restante real de la ventana para el canvas: el contenido superior
  // (header, toolbar, banner de avisos) es variable, un calc fijo se queda corto.
  const canvasWrapRef = useRef<HTMLDivElement>(null)
  const [canvasH, setCanvasH] = useState(560)
  useEffect(() => {
    const el = canvasWrapRef.current
    if (!el) return
    const update = () => setCanvasH(Math.max(320, window.innerHeight - el.getBoundingClientRect().top - 12))
    update()
    const t = setTimeout(update, 400)
    window.addEventListener('resize', update)
    return () => {
      clearTimeout(t)
      window.removeEventListener('resize', update)
    }
  }, [loaded, working.racks.length])

  const fitCanvas = useCallback(() => {
    const row = canvasWrapRef.current
    if (!row || working.racks.length === 0) return
    const W = row.clientWidth - 268 // aside w-64 + gap-3
    const H = canvasH
    let minX = Infinity
    let minY = Infinity
    let maxX = -Infinity
    let maxY = -Infinity
    for (const r of working.racks) {
      const move = rackMoves.get(r.id)
      const x = move?.x ?? r.position_x
      const y = move?.y ?? r.position_y
      minX = Math.min(minX, x)
      minY = Math.min(minY, y)
      maxX = Math.max(maxX, x + rackWidthPx())
      maxY = Math.max(maxY, y + rackHeightPx(r.u_height))
    }
    const pad = 48
    const bw = Math.max(1, maxX - minX)
    const bh = Math.max(1, maxY - minY)
    const zoom = Math.min(1, (W - pad) / bw, (H - pad) / bh)
    setViewport({
      x: (W - bw * zoom) / 2 - minX * zoom,
      y: (H - bh * zoom) / 2 - minY * zoom,
      zoom,
    })
  }, [working.racks, rackMoves, canvasH, setViewport])

  // RF v12 añade la clase light/dark propia; sin colorMode el canvas cae en
  // el tema claro y pisaría los tokens del app. Ligamos al tema real.
  const [lightTheme, setLightTheme] = useState(() => document.documentElement.classList.contains('light'))
  useEffect(() => {
    const obs = new MutationObserver(() => setLightTheme(document.documentElement.classList.contains('light')))
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => obs.disconnect()
  }, [])
  const [nodes, setNodes, onNodesChange] = useNodesState<RackNodeType | MountNodeType>([])
  // Cables: capa overlay con coordenadas de PANTALLA medidas del DOM.
  // Nada de matemáticas de flujo: se mide donde está pintada cada boca.
  const cableLayerRef = useRef<HTMLDivElement>(null)
  const [portRects, setPortRects] = useState<Record<string, { x: number; y: number }>>({})
  const recomputePortRects = useCallback(() => {
    const layer = cableLayerRef.current
    if (!layer) return
    const base = layer.getBoundingClientRect()
    const out: Record<string, { x: number; y: number }> = {}
    for (const btn of layer.parentElement?.querySelectorAll('button[data-port]') ?? []) {
      const mountId = (btn.closest('[data-mount]') as HTMLElement | null)?.getAttribute('data-mount')
      if (!mountId) continue
      const r = (btn as HTMLElement).getBoundingClientRect()
      out[`${mountId}|${btn.getAttribute('data-port')}`] = {
        x: r.x + r.width / 2 - base.x,
        y: r.y + r.height / 2 - base.y,
      }
    }
    setPortRects(out)
  }, [])
  useEffect(() => {
    const id = requestAnimationFrame(() => recomputePortRects())
    return () => cancelAnimationFrame(id)
  }, [nodes, visibility, recomputePortRects])
  const cancelRef = useRef(0)
  // Última posición de drag (RF puede disparar dragStop con posición previa).
  const dragPosRef = useRef<{ x: number; y: number } | null>(null)
  // Posiciones actuales de los nodos rack (para hit-testing de drops y drags).
  const rackPosRef = useRef<Map<string, { x: number; y: number }>>(new Map())
  useEffect(() => {
    const m = new Map<string, { x: number; y: number }>()
    for (const n of nodes) {
      if (n.type === 'rack') m.set(n.id.replace('rack-', ''), n.position)
    }
    rackPosRef.current = m
  }, [nodes])

  /** Rack cuyo rect contiene el punto (flow coords), con el punto relativo. */
  const rackAtPoint = useCallback((x: number, y: number): { rack: RackDTO; relX: number; relY: number } | null => {
    for (const r of working.racks) {
      const pos = rackPosRef.current.get(r.id) ?? { x: r.position_x, y: r.position_y }
      if (x >= pos.x && x <= pos.x + rackWidthPx() && y >= pos.y && y <= pos.y + rackHeightPx(r.u_height)) {
        return { rack: r, relX: x - pos.x, relY: y - pos.y }
      }
    }
    return null
  }, [working.racks])
  // Los nodos cargan asíncronos: fitView de init no los ve. Reenfocamos cada
  // vez que cambia el número de nodos, con margen y sin pasar de escala 1.
  // Encuadre determinista: calculamos el viewport desde el MODELO (posiciones
  // de rack y tamaños conocidos), sin depender de la medición de RF ni de
  // fitView con nodos asíncronos (race conocida: encuadres a escala 0.3).
  useEffect(() => {
    if (nodes.length === 0) return
    const t1 = requestAnimationFrame(() => {
      cancelRef.current = requestAnimationFrame(() => fitCanvas())
    })
    cancelRef.current = t1
    return () => cancelAnimationFrame(cancelRef.current)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nodes.length, canvasH])


  const reload = useCallback(async () => {
    const b = await api.fetchRackBundle()
    setWorking({
      racks: b.racks,
      mounts: b.mounts,
      cables: b.cables,
      profiles: new Map(b.profiles.map((p) => [p.mac, p])),
      audit: b.audit ?? {},
    })
    setNewMountIds(new Set())
    setMovedMountIds(new Set())
    setDeletedMounts([])
    setNewCables(new Set())
    setDeletedCables(new Set())
    setProfileChanges(new Map())
    setRackMoves(new Map())
    setDraft(null)
    setSelectedCableId(null)
    setLoaded(true)
    // Rack activo por defecto: descubrible el botón Eliminar y el destino de
    // los montajes del picker sin necesidad de un clic previo.
    setActiveRackId((cur) => (cur && b.racks.some((r) => r.id === cur) ? cur : b.racks[0]?.id ?? null))
  }, [])

  useEffect(() => {
    reload().catch((e) => toast.error(String(e)))
  }, [reload])

  // --- Modelo derivado ---
  const deviceByMac = useMemo(() => new Map(np.devices.map((d) => [d.mac.toLowerCase(), d])), [np.devices])
  const rackById = useMemo(() => new Map(working.racks.map((r) => [r.id, r])), [working.racks])
  const mountById = useMemo(() => new Map(working.mounts.map((m) => [m.id, m])), [working.mounts])

  const plateFor = useCallback(
    (m: MountDTO) => {
      if (m.device_mac) {
        const prof = working.profiles.get(m.device_mac)
        const plate = getFaceplate(prof?.faceplate_id || m.faceplate_id || '')
        if (plate) return plate
        // Perfil placeholder/vacío: sugerimos por el tipo detectado del
        // dispositivo (un switch nunca debe quedarse sin bocas LAN).
        const devType = deviceByMac.get(m.device_mac.toLowerCase())?.type ?? ''
        return suggestFaceplate(devType) ?? getFaceplate('blank-1u')!
      }
      return getFaceplate(m.faceplate_id || 'blank-1u') || getFaceplate('blank-1u')!
    },
    [working.profiles, deviceByMac],
  )

  const portsFor = useCallback(
    (m: MountDTO) => {
      const plate = plateFor(m)
      if (m.device_mac) {
        const declared = working.profiles.get(m.device_mac)?.ports
        if (declared && declared.length > 0) {
          // Sanado de layouts legacy: si las bocas declaradas invaden la
          // zona de la etiqueta de su plantilla, se re-disponen con la zona
          // correcta (p. ej. perfiles creados antes de la zona por plantilla).
          const zone = plate.portZone ?? { xStart: 0.24, xEnd: 0.94 }
          const minX = Math.min(...declared.map((p) => p.x))
          if (minX < zone.xStart - 0.02) {
            return layoutPhysicalPorts(
              declared.map((p) => ({ id: p.id, kind: p.kind as RackPortKind })),
              zone,
            )
          }
          return declared
        }
        return seedPorts(plate)
      }
      return seedPorts(plate)
    },
    [working.profiles, plateFor],
  )


  const statusFor = useCallback(
    (m: MountDTO): 'online' | 'offline' | 'unknown' => {
      if (m.status_pin && m.status_pin !== 'auto') return m.status_pin as 'online' | 'offline' | 'unknown'
      if (!m.device_mac) return 'unknown'
      const d = deviceByMac.get(m.device_mac.toLowerCase())
      if (!d) return 'unknown'
      return d.online ? 'online' : 'offline'
    },
    [deviceByMac],
  )

  const footprintsOfRack = useCallback(
    (rackId: string, excludeMountId?: string): Footprint[] =>
      working.mounts
        .filter((m) => m.rack_id === rackId && m.id !== excludeMountId)
        .map((m) => ({ uStart: m.u_start, uHeight: m.u_height, colStart: m.col_start, colSpan: m.col_span })),
    [working.mounts],
  )

  const capacityFor = useCallback(
    (m: MountDTO) => (plateFor(m).passThrough ? 2 : 1),
    [plateFor],
  )

  const cablesOnPort = useCallback(
    (mountId: string, portId: string) =>
      working.cables.filter(
        (c) =>
          !deletedCables.has(c.id) &&
          ((c.from_mount === mountId && c.from_port === portId) || (c.to_mount === mountId && c.to_port === portId)),
      ),
    [working.cables, deletedCables],
  )

  // --- Nodos ---
  useEffect(() => {
    if (!loaded) return
    const ghostByRack = new Map<string, GhostState | null>()
    const next: (RackNodeType | MountNodeType)[] = []
    for (const r of working.racks) {
      const move = rackMoves.get(r.id)
      next.push({
        id: `rack-${r.id}`,
        type: 'rack',
        position: move ?? { x: r.position_x, y: r.position_y },
        draggable: true,
        data: {
          name: r.name,
          location: r.location ?? '',
          uHeight: r.u_height,
          numbering: r.numbering,
          selected: activeRackId === r.id,
          ghost: ghostByRack.get(r.id) ?? null,
          onSelect: () => {
            setActiveRackId(r.id)
            setSelectedMountId(null)
            setSelectedCableId(null)
          },
        },
      })
    }
    for (const m of working.mounts) {
      const rack = rackById.get(m.rack_id)
      if (!rack) continue
      const plate = plateFor(m)
      next.push({
        id: `mount-${m.id}`,
        type: 'mount',
        parentId: `rack-${m.rack_id}`,
        position: mountRelPos(rack.u_height, m.u_start, m.u_height, m.col_start),
        draggable: true,
        selectable: true,
        style: { width: m.col_span * COL_PX, height: m.u_height * U_PX },
        data: {
          plate,
          ports: portsFor(m),
          mountId: m.id,
          label:
            m.label ||
            deviceByMac.get(m.device_mac?.toLowerCase() ?? '')?.name ||
            routerNameByMac.get(m.device_mac?.toLowerCase() ?? '') ||
            '',
          color: m.device_mac ? working.profiles.get(m.device_mac)?.color || undefined : undefined,
          status: statusFor(m),
          selected: selectedMountId === m.id,
          onSelect: () => setSelectedMountId((cur) => (cur === m.id ? null : m.id)),
          onUnmount: isAdmin ? () => unmount(m.id) : undefined,
          patchFacing:
            plate.passThrough === true ||
            plate.artwork === 'switch' ||
            plate.artwork === 'router' ||
            plate.artwork === 'panel' ||
            plate.rows.reduce((n, r) => n + r.count, 0) >= 8,
          portsVisible: patchMode || hoverMountId === m.id,
          portState: (portId: string) => {
            const cabled = cablesOnPort(m.id, portId).length > 0
            const drafting = draft?.mountId === m.id && draft?.portId === portId
            return { cabled, drafting, interactive: patchMode }
          },
          onPortClick: (portId: string) => handlePortClick(m.id, portId),
          onPortEnter: () => setHoverMountId(m.id),
          onPortLeave: () => setHoverMountId((h) => (h === m.id ? null : h)),
        },
      })
    }
    if (!draggingRef.current) setNodes(next)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded, working, activeRackId, patchMode, draft, rackMoves, deletedCables, hoverMountId, selectedMountId, isAdmin])

  // handlePortClick se declara después; referencia estable vía ref.
  const portClickRef = useRef<(mountId: string, portId: string) => void>(() => {})
  const handlePortClick = useCallback(
    (mountId: string, portId: string) => portClickRef.current(mountId, portId),
    [],
  )
  useEffect(() => {
    portClickRef.current = (mountId: string, portId: string) => {
      if (!patchMode) return
      if (!draft) {
        setDraft({ mountId, portId })
        return
      }
      if (draft.mountId === mountId && draft.portId === portId) {
        setDraft(null)
        return
      }
      const target = mountById.get(mountId)
      if (!target) return
      const busy = cablesOnPort(mountId, portId).length
      if (busy >= capacityFor(target)) {
        toast.error(t('rack.patchBusy'))
        return
      }
      const c: CableDTO = {
        id: localId('cable'),
        from_mount: draft.mountId,
        from_port: draft.portId,
        to_mount: mountId,
        to_port: portId,
        type: 'ethernet',
        origin: 'manual',
        created_at: Date.now(),
      }
      setWorking((w) => ({ ...w, cables: [...w.cables, c] }))
      setNewCables((s) => new Set(s).add(c.id))
      setDraft(null)
    }
  }, [patchMode, draft, mountById, cablesOnPort, capacityFor, t])

  // --- Drag & snap ---
  const onNodeDrag = useCallback(
    (_: unknown, node: (typeof nodes)[number]) => {
      if (node.type !== 'mount') return
      const mountId = node.id.replace('mount-', '')
      const m = mountById.get(mountId)
      const origRack = m && rackById.get(m.rack_id)
      if (!m || !origRack) return
      const origPos = rackPosRef.current.get(origRack.id) ?? { x: origRack.position_x, y: origRack.position_y }
      dragPosRef.current = node.position
      // El rack destino se decide por el CENTRO del montaje (el top-left
      // puede quedar 6px fuera del rect con agarres centrales).
      const absX = origPos.x + node.position.x
      const absY = origPos.y + node.position.y
      const absCx = absX + (m.col_span * COL_PX) / 2
      const absCy = absY + (m.u_height * U_PX) / 2
      const hit = rackAtPoint(absCx, absCy) ?? { rack: origRack, relX: node.position.x, relY: node.position.y }
      const cell = relPosToCell(hit.rack.u_height, hit.relX, hit.relY, m.u_height)
      const fp = findSlot(hit.rack.u_height, footprintsOfRack(hit.rack.id, mountId), cell.uStart, cell.colStart, m.u_height, m.col_span)
      const ghost: GhostState | null = fp
        ? { ...fp, valid: true }
        : {
            uStart: Math.min(Math.max(cell.uStart, 1), hit.rack.u_height - m.u_height + 1),
            uHeight: m.u_height,
            colStart: Math.min(Math.max(cell.colStart, 0), 12 - m.col_span),
            colSpan: m.col_span,
            valid: false,
          }
      setNodes((ns) =>
        ns.map((n) =>
          n.id === `rack-${hit.rack.id}` && n.type === 'rack'
            ? { ...n, data: { ...n.data, ghost } }
            : n,
        ),
      )
    },
    [mountById, rackById, footprintsOfRack, rackAtPoint, setNodes],
  )

  const onNodeDragStop = useCallback(
    (_: unknown, node: (typeof nodes)[number]) => {
      draggingRef.current = false
      if (node.type === 'rack') {
        const id = node.id.replace('rack-', '')
        setRackMoves((m) => new Map(m).set(id, node.position))
        return
      }
      if (node.type !== 'mount') return
      const mountId = node.id.replace('mount-', '')
      const m = mountById.get(mountId)
      const origRack = m && rackById.get(m.rack_id)
      if (!m || !origRack) return
      const origPos = rackPosRef.current.get(origRack.id) ?? { x: origRack.position_x, y: origRack.position_y }
      const pos = dragPosRef.current ?? node.position
      dragPosRef.current = null
      const absX = origPos.x + pos.x
      const absY = origPos.y + pos.y
      const absCx = absX + (m.col_span * COL_PX) / 2
      const absCy = absY + (m.u_height * U_PX) / 2
      const hit = rackAtPoint(absCx, absCy) ?? { rack: origRack, relX: pos.x, relY: pos.y }
      const cell = relPosToCell(hit.rack.u_height, hit.relX, hit.relY, m.u_height)
      const fp = findSlot(hit.rack.u_height, footprintsOfRack(hit.rack.id, mountId), cell.uStart, cell.colStart, m.u_height, m.col_span)
      setNodes((ns) =>
        ns.map((n) =>
          n.id === `rack-${hit.rack.id}` && n.type === 'rack' ? { ...n, data: { ...n.data, ghost: null } } : n,
        ),
      )
      if (!fp) {
        toast.error(t('rack.noSpace'))
        return
      }
      if (fp.uStart === m.u_start && fp.colStart === m.col_start && hit.rack.id === m.rack_id) return
      // Entre racks el upsert MUEVE la fila (ON CONFLICT rack_id): los
      // cables del montaje sobreviven; no hay delete en el origen.
      setWorking((w) => ({
        ...w,
        mounts: w.mounts.map((mm) =>
          mm.id === mountId ? { ...mm, rack_id: hit.rack.id, u_start: fp.uStart, col_start: fp.colStart } : mm,
        ),
      }))
      if (!newMountIds.has(mountId)) setMovedMountIds((s) => new Set(s).add(mountId))
    },
    [mountById, rackById, footprintsOfRack, rackAtPoint, newMountIds, setNodes, t],
  )

  // --- Montar desde el picker ---
  const mountAt = useCallback(
    (rackId: string, fp: Footprint, partial: Omit<MountDTO, 'id' | 'rack_id' | 'u_start' | 'col_start' | 'status_pin' | 'port_visibility'>) => {
      const m: MountDTO = {
        ...partial,
        id: localId('mount'),
        rack_id: rackId,
        u_start: fp.uStart,
        col_start: fp.colStart,
        status_pin: 'auto',
        port_visibility: 'auto',
      }
      setWorking((w) => ({ ...w, mounts: [...w.mounts, m] }))
      setNewMountIds((s) => new Set(s).add(m.id))
      setActiveRackId(rackId)
    },
    [],
  )

  const prepareDeviceMount = useCallback(
    (
      mac: string,
      deviceType: string,
      label: string,
      physicalPorts?: { id: string; kind: RackPortKind }[],
      rackId?: string,
      dropRel?: { x: number; y: number },
    ) => {
      const rid = rackId ?? activeRackId ?? working.racks[0]?.id
      if (!rid || !rackById.get(rid)) {
        toast.error(t('rack.emptyHint'))
        return
      }
      const existing = working.profiles.get(mac)
      // Sin sugerencia para el tipo detectado: faceplate genérico 1U (el
      // usuario lo afina después); mejor eso que un placeholder sin puertos
      // por el que el auto-cableado no puede parchar. Un perfil placeholder
      // (faceplate vacío) también se sustituye por la sugerencia.
      const plate =
        (existing ? getFaceplate(existing.faceplate_id) : undefined) ??
        suggestFaceplate(deviceType) ??
        getFaceplate('server-1u')
      const defU = existing?.u_height ?? plate?.uHeight ?? 1
      setMountAsk({
        rackId: rid,
        mac,
        deviceType,
        label,
        physicalPorts,
        faceplateId: plate?.id ?? 'server-1u',
        defaultU: defU,
        isPatch: false,
        dropRel,
      })
      setAskU(String(defU))
    },
    [activeRackId, working.racks, rackById, working.profiles, t],
  )

  const prepareAccessoryMount = useCallback(
    (faceplateId: string, rackId?: string, dropRel?: { x: number; y: number }) => {
      const rid = rackId ?? activeRackId ?? working.racks[0]?.id
      const rack = rid ? rackById.get(rid) : undefined
      const plate = getFaceplate(faceplateId)
      if (!rack || !plate) {
        toast.error(t('rack.emptyHint'))
        return
      }
      const isPatch = plate.passThrough === true
      setMountAsk({ rackId: rid!, deviceType: '', label: '', faceplateId, defaultU: plate.uHeight, isPatch, dropRel })
      setAskU(String(plate.uHeight))
      setAskPorts(faceplateId === 'patch-panel-12p' ? '12' : faceplateId === 'patch-panel-48p' ? '48' : '24')
    },
    [activeRackId, working.racks, rackById, t],
  )

  // Confirmación del diálogo: hueco final según la altura elegida y staging.
  const commitMount = useCallback(() => {
    const ask = mountAsk
    if (!ask) return
    const rack = rackById.get(ask.rackId)
    if (!rack) return
    // Patch panel: el nº de puertos elegido decide la variante del catálogo.
    let faceplateId = ask.faceplateId ?? ''
    let uHeight = Math.min(45, Math.max(1, parseInt(askU, 10) || ask.defaultU))
    if (ask.isPatch) {
      faceplateId = askPorts === '12' ? 'patch-panel-12p' : askPorts === '48' ? 'patch-panel-48p' : 'patch-panel-1u'
      // La altura la impone la variante (48p = 2U).
      uHeight = getFaceplate(faceplateId)?.uHeight ?? uHeight
    }
    const plate = getFaceplate(faceplateId)
    const colSpan = plate?.colSpan ?? 12
    // Coloca donde se soltó (snap al hueco libre más cercano a ese punto);
    // sin punto de drop, el primer hueco libre desde abajo.
    let dropU = 1
    let dropCol = 0
    if (ask.dropRel) {
      const cell = relPosToCell(rack.u_height, ask.dropRel.x, ask.dropRel.y, uHeight)
      dropU = cell.uStart
      dropCol = cell.colStart
    }
    const fp = findSlot(rack.u_height, footprintsOfRack(rack.id), dropU, dropCol, uHeight, colSpan)
    if (!fp) {
      toast.error(t('rack.noSpace'))
      return
    }
    if (ask.mac) {
      const existing = working.profiles.get(ask.mac)
      if (plate && (!existing || (existing.ports.length === 0 && !existing.faceplate_id) || existing.u_height !== uHeight)) {
        const prof: ProfileDTO = {
          mac: ask.mac,
          faceplate_id: plate.id,
          u_height: uHeight,
          col_span: colSpan,
          color: '',
          // Bocas físicas reales cuando el poller las conoce (flota);
          // si no, la plantilla siembra las típicas. La zona horizontal
          // viene de la plantilla para no pisar la etiqueta.
          ports:
            ask.physicalPorts && ask.physicalPorts.length > 0
              ? layoutPhysicalPorts(ask.physicalPorts, plate.portZone)
              : seedPorts(plate),
        }
        setProfileChanges((pm) => new Map(pm).set(ask.mac!, prof))
        setWorking((w) => ({ ...w, profiles: new Map(w.profiles).set(ask.mac!, prof) }))
      }
    }
    mountAt(rack.id, fp, {
      device_mac: ask.mac ?? '',
      faceplate_id: faceplateId,
      u_height: uHeight,
      col_span: colSpan,
      label: ask.label,
    })
    setMountAsk(null)
  }, [mountAsk, askU, askPorts, rackById, footprintsOfRack, working.profiles, mountAt, t])

  // Los routers de flota traen sus bocas reales del poller (extras.ethPorts).
  const mountRouter = useCallback(
    async (router: { id: string; mac?: string; name: string }, rackId?: string, dropRel?: { x: number; y: number }) => {
      if (!router.mac) return
      let physical: { id: string; kind: RackPortKind }[] | undefined
      try {
        const res = await fetch(`/api/routers/${encodeURIComponent(router.id)}`)
        if (res.ok) {
          const detail = await res.json()
          const eth: { id?: string; iface?: string; name?: string }[] = detail?.extras?.ethPorts ?? []
          physical = eth
            .map((p) => p.id || p.iface || p.name || '')
            .filter((id) => id && !id.startsWith('wlan'))
            .map((id) => {
              const k = /sfp\+|10g|fiber|fibre/i.test(id) ? 'sfp+' : /sfp/i.test(id) ? 'sfp' : 'rj45'
              return { id, kind: k as RackPortKind }
            })
        }
      } catch {
        physical = undefined
      }
      prepareDeviceMount(router.mac, 'router', router.name, physical, rackId, dropRel)
    },
    [prepareDeviceMount],
  )

  // --- Rack ops ---
  const openAddRack = useCallback(() => {
    setAddName(`Rack ${working.racks.length + 1}`)
    setAddU('12')
    setAddLocation('')
    setAddOpen(true)
  }, [working.racks.length])

  const submitAddRack = useCallback(async () => {
    // A la derecha del último rack para que nazcan separados visualmente.
    const maxX = working.racks.reduce((acc, r) => Math.max(acc, (rackMoves.get(r.id)?.x ?? r.position_x) + rackWidthPx()), 0)
    try {
      const r = await api.createRack({
        name: addName.trim() || 'Rack',
        u_height: Math.min(45, Math.max(1, parseInt(addU, 10) || 12)),
        location: addLocation.trim(),
        position_x: working.racks.length === 0 ? 40 : maxX + 40,
        position_y: 40,
      })
      setWorking((w) => ({ ...w, racks: [...w.racks, r] }))
      setActiveRackId(r.id)
      setAddOpen(false)
    } catch (e) {
      toast.error(String(e))
    }
  }, [working.racks, rackMoves, addName, addU, addLocation])

  const openEditRack = useCallback(() => {
    const r = activeRackId ? rackById.get(activeRackId) : undefined
    if (!r) return
    setEditName(r.name)
    setEditU(String(r.u_height))
    setEditLocation(r.location ?? '')
    setEditOpen(true)
  }, [activeRackId, rackById])

  const submitEditRack = useCallback(async () => {
    const r = activeRackId ? rackById.get(activeRackId) : undefined
    if (!r) return
    try {
      await api.saveRack({
        ...r,
        name: editName.trim() || r.name,
        u_height: Math.min(45, Math.max(1, parseInt(editU, 10) || r.u_height)),
        location: editLocation.trim(),
      })
      toast.success(t('rack.rackUpdated'))
      setEditOpen(false)
      await reload()
    } catch (e) {
      toast.error(String(e))
    }
  }, [activeRackId, rackById, editName, editU, editLocation, reload, t])

  const confirmDeleteRack = useCallback(async () => {
    const rackId = activeRackId
    if (!rackId) return
    try {
      await api.deleteRack(rackId)
      toast.success(t('rack.rackDeleted'))
      setDeleteRackOpen(false)
      await reload()
    } catch (e) {
      toast.error(String(e))
    }
  }, [activeRackId, reload, t])

  // --- Save explícito: nada persiste sin Save ---
  const save = useCallback(async () => {
    setSaving(true)
    try {
      for (const [id, pos] of rackMoves) {
        const r = rackById.get(id)
        if (r) await api.saveRack({ ...r, position_x: pos.x, position_y: pos.y })
      }
      const rackIds = new Set<string>()
      for (const m of working.mounts) {
        if (newMountIds.has(m.id) || movedMountIds.has(m.id)) rackIds.add(m.rack_id)
      }
      for (const d of deletedMounts) rackIds.add(d.rackId)
      for (const rid of rackIds) {
        const upsert = working.mounts.filter((m) => m.rack_id === rid && (newMountIds.has(m.id) || movedMountIds.has(m.id)))
        const del = deletedMounts.filter((d) => d.rackId === rid).map((d) => d.id)
        await api.saveLayout({ rack_id: rid, upsert, delete: del })
      }
      for (const [mac, prof] of profileChanges) {
        await api.upsertProfile(mac, {
          faceplate_id: prof.faceplate_id,
          u_height: prof.u_height,
          col_span: prof.col_span,
          color: prof.color,
          ports: prof.ports,
        })
      }
      // Un cable persistido que toca un montaje borrado no se borra a pie:
      // la cascada del layout (ON DELETE CASCADE) ya se lo llevó.
      const deletedMountIds = new Set(deletedMounts.map((d) => d.id))
      for (const cid of deletedCables) {
        if (newCables.has(cid)) continue
        const c = working.cables.find((cc) => cc.id === cid)
        if (c && (deletedMountIds.has(c.from_mount) || deletedMountIds.has(c.to_mount))) continue
        await api.deleteCable(cid).catch(() => undefined)
      }
      for (const c of working.cables) {
        if (!newCables.has(c.id)) continue
        if (deletedMountIds.has(c.from_mount) || deletedMountIds.has(c.to_mount)) continue
        const created = await api.addCable({
          from_mount: c.from_mount,
          from_port: c.from_port,
          to_mount: c.to_mount,
          to_port: c.to_port,
          type: c.type,
          label: c.label,
          properties: c.properties,
          origin: c.origin,
        })
        setWorking((w) => ({ ...w, cables: w.cables.map((cc) => (cc.id === c.id ? created : cc)) }))
      }
      toast.success(t('rack.saved'))
      await reload()
      np.refresh()
    } catch (e) {
      toast.error(`${t('rack.saveFailed')}: ${String(e)}`)
    } finally {
      setSaving(false)
    }
  }, [working, rackById, rackMoves, newMountIds, movedMountIds, deletedMounts, profileChanges, deletedCables, newCables, reload, np, t])

  // api.saveRack no existe aún: lo añadimos (UpdateRack endpoint ya existe en Go).
  // Desmontar: el montaje sale del rack (staging); sus cables se descartan
  // del working copy (al guardar, el layout borra el montaje y la cascada
  // del server se lleva los cables).
  // Refs para que el compiler pueda estabilizar el callback (deps de maps/sets).
  const mountByIdRef = useRef<Map<string, MountDTO>>(new Map())
  useEffect(() => {
    mountByIdRef.current = mountById
  }, [mountById])
  const newMountIdsRef = useRef<Set<string>>(new Set())
  useEffect(() => {
    newMountIdsRef.current = newMountIds
  }, [newMountIds])

  // Función plana (no useCallback): el compiler no logra preservar su
  // memoización manual por las lecturas de ref; los consumidores usan ref.
  const unmount = (mountId: string) => {
      const m = mountByIdRef.current.get(mountId)
      if (!m) return
      setWorking((w) => ({
        ...w,
        mounts: w.mounts.filter((mm) => mm.id !== mountId),
        cables: w.cables.filter((c) => c.from_mount !== mountId && c.to_mount !== mountId),
      }))
      if (newMountIdsRef.current.has(mountId)) {
        setNewMountIds((prev) => {
          const n = new Set(prev)
          n.delete(mountId)
          return n
        })
      } else {
        setDeletedMounts((d) => [...d, { id: mountId, rackId: m.rack_id }])
        setMovedMountIds((prev) => {
          const n = new Set(prev)
          n.delete(mountId)
          return n
        })
      }
      setSelectedMountId(null)
      toast.success(t('rack.unmounted'))
  }
  const unmountRef = useRef(unmount)
  useEffect(() => {
    unmountRef.current = unmount
  })

  const runImport = useCallback(async () => {
    if (dirty) {
      toast.error(t('rack.importNeedsSave'))
      return
    }
    try {
      const res = await api.importCables()
      toast.success(
        t('rack.importResult', { created: res.created, skipped: res.skipped, noFree: res.noFreePort }),
      )
      await reload()
    } catch (e) {
      toast.error(String(e))
    }
  }, [dirty, reload, t])

  // Drop desde el picker: punto en coords de flow, rack destino y diálogo
  // de colocación (mismo flujo que el clic).
  const onPickerDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault()
      const raw = e.dataTransfer.getData(DND_MIME)
      if (!raw) return
      let payload: PickerDragPayload
      try {
        payload = JSON.parse(raw)
      } catch {
        return
      }
      const flow = screenToFlowPosition({ x: e.clientX, y: e.clientY })
      const hit = rackAtPoint(flow.x, flow.y)
      if (!hit) {
        toast.error(t('rack.dropNoRack'))
        return
      }
      const dropRel = { x: hit.relX, y: hit.relY }
      if (payload.type === 'accessory' && payload.faceplateId) {
        prepareAccessoryMount(payload.faceplateId, hit.rack.id, dropRel)
      } else if (payload.type === 'router' && payload.routerId && payload.mac) {
        void mountRouter({ id: payload.routerId, mac: payload.mac, name: payload.label ?? '' }, hit.rack.id, dropRel)
      } else if (payload.type === 'device' && payload.mac) {
        prepareDeviceMount(payload.mac, payload.deviceType ?? '', payload.label ?? '', undefined, hit.rack.id, dropRel)
      }
    },
    [screenToFlowPosition, rackAtPoint, prepareAccessoryMount, mountRouter, prepareDeviceMount, t],
  )

  // --- Selección: montajes vía onSelectionChange; Delete gestiona cable o montaje ---
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setDraft(null)
        setSelectedCableId(null)
        setSelectedMountId(null)
      }
      if (e.key === 'Delete' || e.key === 'Backspace') {
        if (selectedCableId) {
          setDeletedCables((s) => new Set(s).add(selectedCableId))
          setWorking((w) => ({ ...w, cables: w.cables.filter((c) => c.id !== selectedCableId) }))
          setSelectedCableId(null)
          toast.success(t('rack.cableDeleted'))
        } else if (selectedMountId) {
          unmountRef.current(selectedMountId)
        }
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [selectedCableId, selectedMountId, t])

  // --- Picker data ---
  const mountedMacs = useMemo(
    () => new Set(working.mounts.map((m) => m.device_mac?.toLowerCase() ?? '')),
    [working.mounts],
  )
  // Solo cableados: el rack es físico; WiFi (2.4/5/6/60 GHz) queda fuera.
  const unmountedDevices = useMemo(
    () =>
      // El rack es hardware de red: switches, servidores, ordenadores de
      // torre y CCTV. Fuera TVs, altavoces, IoT y resto que no tenga sentido.
      // #rack-mini: los `desconocido` fuera también - si no sabemos qué es,
      // no se ofrece para el rack.
      np.devices.filter(
        (d) =>
          !mountedMacs.has(d.mac.toLowerCase()) &&
          (d.band === 'cable' || d.band === '—') &&
          d.infra !== 'ct' &&
          d.infra !== 'vm' &&
          ['switch', 'servidor', 'ordenador', 'camara'].includes(d.type),
      ),
    [np.devices, mountedMacs],
  )
  const unmountedRouters = useMemo(
    () => np.routers.filter((r) => r.mac && !mountedMacs.has(r.mac.toLowerCase())),
    [np.routers, mountedMacs],
  )
  // #rack-mini: nombre real para los mounts de unidades de flota - los
  // routers/switches de la flota NO están en np.devices (eso son clientes),
  // así que el label del montaje caía vacío y la placa pintaba su nombre por
  // defecto ("Router") para todos.
  const routerNameByMac = useMemo(
    () => new Map(np.routers.filter((r) => r.mac).map((r) => [r.mac.toLowerCase(), r.name])),
    [np.routers],
  )
  const accessoryPlates = useMemo(() => FACEPLATES.filter((p) => p.group === 'accessory'), [])

  if (!services.rack) {
    return (
      <div className="flex h-full flex-col">
        <div className="mx-auto my-auto max-w-md rounded-xl border border-border bg-surface p-6 text-center">
          <TriangleAlert size={24} className="mx-auto mb-2 text-warn" aria-hidden />
          <p className="text-sm font-semibold text-text-primary">{t('rack.disabledTitle')}</p>
          <p className="mt-1 text-[13px] text-text-secondary">{t('rack.disabledText')}</p>
        </div>
      </div>
    )
  }

  return (
      <div className="flex h-full flex-col">
      {/* Aviso beta: vista en desarrollo, no usar en producción */}
      <div
        className="mb-3 flex items-start gap-3 rounded-xl border border-warn/40 bg-warn/10 px-4 py-3"
        role="note"
        data-testid="rack-beta-banner"
      >
        <TriangleAlert size={18} className="mt-0.5 shrink-0 text-warn" aria-hidden />
        <div className="min-w-0">
          <p className="text-[13px] font-semibold text-text-primary">{t('rack.betaTitle')}</p>
          <p className="mt-0.5 text-[12px] leading-snug text-text-secondary">{t('rack.betaText')}</p>
        </div>
      </div>

      <SectionHeader title={t('rack.title')}>
        <p className="mt-0.5 text-sm text-text-secondary">{t('rack.subtitle')}</p>
      </SectionHeader>

      {/* Toolbar */}
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={openAddRack}
          disabled={!isAdmin}
          className="flex h-9 items-center gap-1.5 rounded-xl border border-border bg-surface px-3 text-[13px] font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50"
        >
          <Plus size={15} /> {t('rack.addRack')}
        </button>
        <button
          type="button"
          onClick={() => setPatchMode((p) => !p)}
          disabled={!isAdmin}
          aria-pressed={patchMode}
          className={
            'flex h-9 items-center gap-1.5 rounded-xl border px-3 text-[13px] font-medium transition-colors disabled:opacity-50 ' +
            (patchMode ? 'border-accent/60 bg-accent/10 text-accent' : 'border-border bg-surface text-text-secondary hover:border-accent/40 hover:text-accent')
          }
        >
          <Cable size={15} /> {t('rack.patch')}
        </button>
        <SegmentedControl<CableVisibility>
          ariaLabel={t('rack.visibility')}
          options={[
            { value: 'hover', label: t('rack.visibilityHover') },
            { value: 'always', label: t('rack.visibilityAlways') },
            { value: 'hidden', label: t('rack.visibilityHidden') },
          ]}
          value={visibility}
          onChange={setVisibility}
          size="sm"
        />
        <button
          type="button"
          onClick={() => void runImport()}
          disabled={!isAdmin}
          className="flex h-9 items-center gap-1.5 rounded-xl border border-border bg-surface px-3 text-[13px] font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50"
        >
          <Link2 size={15} /> {t('rack.import')}
        </button>
        <div className="ml-auto flex items-center gap-2">
          {activeRackId && (
            <button
              type="button"
              onClick={openEditRack}
              disabled={!isAdmin}
              className="flex h-9 items-center gap-1.5 rounded-xl border border-border bg-surface px-3 text-[13px] font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50"
            >
              <Pencil size={15} /> {t('rack.editRack')}
            </button>
          )}
          {activeRackId && (
            <button
              type="button"
              onClick={() => setDeleteRackOpen(true)}
              disabled={!isAdmin}
              className="flex h-9 items-center gap-1.5 rounded-xl border border-border bg-surface px-3 text-[13px] font-medium text-text-secondary transition-colors hover:border-danger/50 hover:text-danger disabled:opacity-50"
            >
              <Trash2 size={15} /> {t('rack.deleteRack')}
            </button>
          )}
          <button
            type="button"
            onClick={save}
            disabled={!dirty || saving || !isAdmin}
            className="flex h-9 items-center gap-1.5 rounded-xl bg-accent px-4 text-[13px] font-semibold text-black transition-opacity disabled:opacity-40"
          >
            <Save size={15} /> {saving ? t('rack.saving') : t('rack.save')}
            {dirty && <span className="ml-0.5 inline-block h-1.5 w-1.5 rounded-full bg-black/70" aria-label={t('rack.unsaved')} />}
          </button>
        </div>
      </div>

      <div ref={canvasWrapRef} className="mt-3 flex gap-3" style={{ height: canvasH }}>
        {/* Picker (izquierda, arrastrable al canvas) */}
        <RackPicker
          isAdmin={isAdmin}
          unmountedRouters={unmountedRouters}
          unmountedDevices={unmountedDevices}
          accessoryPlates={accessoryPlates}
          onRouter={(r) => void mountRouter(r)}
          onDevice={(d) => prepareDeviceMount(d.mac, d.type, d.name)}
          onAccessory={(id) => prepareAccessoryMount(id)}
        />

        {/* Canvas */}
        <div className="relative min-w-0 flex-1 overflow-hidden rounded-xl border border-border bg-canvas">
          {!loaded ? null : working.racks.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center gap-3 text-text-secondary">
              <Server size={32} className="opacity-40" />
              <p className="text-sm">{t('rack.empty')}</p>
              {isAdmin && (
                <button
                  type="button"
                  onClick={openAddRack}
                  className="flex h-9 items-center gap-1.5 rounded-xl bg-accent px-4 text-[13px] font-semibold text-black"
                >
                  <Plus size={15} /> {t('rack.addRack')}
                </button>
              )}
            </div>
          ) : (
            <>
            <ReactFlow
              nodes={nodes}
              onNodesChange={onNodesChange}
              onNodeDragStart={() => {
                draggingRef.current = true
              }}
              onNodeDrag={onNodeDrag}
              onNodeDragStop={onNodeDragStop}
              nodeTypes={nodeTypes}
              deleteKeyCode={null}
              nodesConnectable={false}
              elementsSelectable={false}
              onPaneClick={() => {
                setSelectedMountId(null)
                setSelectedCableId(null)
              }}
              onMove={() => recomputePortRects()}
              onDragOver={(e) => {
                e.preventDefault()
                e.dataTransfer.dropEffect = 'copy'
              }}
              onDrop={onPickerDrop}
              minZoom={0.25}
              maxZoom={2.5}
              colorMode={lightTheme ? 'light' : 'dark'}
              proOptions={{ hideAttribution: true }}
            >
              <Background variant={BackgroundVariant.Dots} gap={24} size={1} />
            </ReactFlow>
            {/* Capa de cables: overlay en coordenadas de pantalla medidas del
                DOM (rects reales de las bocas), curva con comba colgante. */}
            <div ref={cableLayerRef} className="pointer-events-none absolute inset-0 z-10">
              <svg className="absolute inset-0 h-full w-full" style={{ pointerEvents: 'none' }}>
                {working.cables.map((c) => {
                  const sel = c.id === selectedCableId
                  const audit = working.audit[c.id] ?? (c.origin === 'manual' ? 'manual-only' : 'detected')
                  const manualOnly = audit === 'manual-only'
                  const A = portRects[`${c.from_mount}|${c.from_port}`]
                  const B = portRects[`${c.to_mount}|${c.to_port}`]
                  if (!A || !B) return null
                  const dist = Math.hypot(B.x - A.x, B.y - A.y) || 1
                  const sag = Math.min(dist * 0.2, 70)
                  let px = (B.y - A.y) / dist
                  let py = -(B.x - A.x) / dist
                  if (py < 0) {
                    px = -px
                    py = -py
                  }
                  const cx = (A.x + B.x) / 2 + px * sag
                  const cy = (A.y + B.y) / 2 + py * sag
                  const stroke = sel
                    ? 'rgb(var(--accent))'
                    : manualOnly
                      ? 'rgb(var(--warn))'
                      : c.type === 'fiber'
                        ? 'rgb(var(--tunnel))'
                        : 'rgb(var(--text-muted))'
                  return (
                    <g
                      key={c.id}
                      style={{ pointerEvents: 'stroke', cursor: 'pointer' }}
                      onClick={(e) => {
                        e.stopPropagation()
                        setSelectedCableId(sel ? null : c.id)
                      }}
                    >
                      <path d={`M ${A.x} ${A.y} Q ${cx} ${cy} ${B.x} ${B.y}`} stroke="transparent" strokeWidth={12} fill="none" />
                      <path
                        d={`M ${A.x} ${A.y} Q ${cx} ${cy} ${B.x} ${B.y}`}
                        stroke={stroke}
                        strokeWidth={sel ? 2.5 : 1.5}
                        strokeDasharray={manualOnly ? '6 4' : undefined}
                        fill="none"
                      />
                      {audit === 'confirmed' && <circle cx={cx} cy={cy} r={3} fill="rgb(var(--ok))" />}
                    </g>
                  )
                })}
              </svg>
            </div>
            </>
          )}
          {draft && (
            <div className="pointer-events-none absolute bottom-3 left-1/2 -translate-x-1/2 rounded-lg border border-border bg-surface px-3 py-1.5 text-[12px] text-text-secondary">
              {t('rack.drafting', { port: draft.portId })}
            </div>
          )}
        </div>

      </div>

      {/* Diálogo de creación: nombre + altura U (los racks nuevos nacen a la
          derecha del último, separados visualmente). */}
      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>{t('rack.createTitle')}</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col gap-3 py-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rack-name">{t('rack.createName')}</Label>
              <Input id="rack-name" value={addName} onChange={(e) => setAddName(e.target.value)} maxLength={40} />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rack-u">{t('rack.createU')}</Label>
              <Select value={addU} onValueChange={setAddU}>
                <SelectTrigger id="rack-u" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {['6', '9', '12', '18', '24', '42'].map((u) => (
                    <SelectItem key={u} value={u}>
                      {u}U
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rack-location">{t('rack.createLocation')}</Label>
              <Input id="rack-location" value={addLocation} onChange={(e) => setAddLocation(e.target.value)} maxLength={60} placeholder={t('rack.locationPlaceholder')} />
            </div>
          </div>
          <DialogFooter>
            <button
              type="button"
              onClick={() => setAddOpen(false)}
              className="h-9 rounded-xl border border-border px-4 text-[13px] font-medium text-text-secondary transition-colors hover:text-text-primary"
            >
              {t('rack.createCancel')}
            </button>
            <button
              type="button"
              onClick={() => void submitAddRack()}
              className="h-9 rounded-xl bg-accent px-4 text-[13px] font-semibold text-black"
            >
              {t('rack.createSubmit')}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Diálogo de colocación: altura U (y puertos en patch panels). */}
      <Dialog open={mountAsk !== null} onOpenChange={(o) => !o && setMountAsk(null)}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>
              {mountAsk?.isPatch ? t('rack.askTitlePatch') : t('rack.askTitle', { name: mountAsk?.label || t(getFaceplate(mountAsk?.faceplateId ?? '')?.nameKey ?? '') })}
            </DialogTitle>
          </DialogHeader>
          <div className="flex flex-col gap-3 py-2">
            {mountAsk?.isPatch && (
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="ask-ports">{t('rack.askPorts')}</Label>
                <Select
                  value={askPorts}
                  onValueChange={(v) => {
                    setAskPorts(v)
                    const variant = getFaceplate(v === '12' ? 'patch-panel-12p' : v === '48' ? 'patch-panel-48p' : 'patch-panel-1u')
                    if (variant) setAskU(String(variant.uHeight))
                  }}
                >
                  <SelectTrigger id="ask-ports" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {['12', '24', '48'].map((n) => (
                      <SelectItem key={n} value={n}>
                        {n}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="ask-u">{t('rack.askHeight')}</Label>
              <Input
                id="ask-u"
                type="number"
                min={1}
                max={45}
                value={askU}
                onChange={(e) => setAskU(e.target.value)}
                className="w-full"
              />
            </div>
          </div>
          <DialogFooter>
            <button
              type="button"
              onClick={() => setMountAsk(null)}
              className="h-9 rounded-xl border border-border px-4 text-[13px] font-medium text-text-secondary transition-colors hover:text-text-primary"
            >
              {t('rack.createCancel')}
            </button>
            <button
              type="button"
              onClick={() => commitMount()}
              className="h-9 rounded-xl bg-accent px-4 text-[13px] font-semibold text-black"
            >
              {t('rack.askPlace')}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edición de propiedades del rack activo (nombre + altura U). */}
      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>{t('rack.editTitle')}</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col gap-3 py-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rack-edit-name">{t('rack.createName')}</Label>
              <Input id="rack-edit-name" value={editName} onChange={(e) => setEditName(e.target.value)} maxLength={40} />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rack-edit-u">{t('rack.createU')}</Label>
              <Select value={editU} onValueChange={setEditU}>
                <SelectTrigger id="rack-edit-u" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {['6', '9', '12', '18', '24', '42'].map((u) => (
                    <SelectItem key={u} value={u}>
                      {u}U
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rack-edit-location">{t('rack.createLocation')}</Label>
              <Input id="rack-edit-location" value={editLocation} onChange={(e) => setEditLocation(e.target.value)} maxLength={60} placeholder={t('rack.locationPlaceholder')} />
            </div>
          </div>
          <DialogFooter>
            <button
              type="button"
              onClick={() => setEditOpen(false)}
              className="h-9 rounded-xl border border-border px-4 text-[13px] font-medium text-text-secondary transition-colors hover:text-text-primary"
            >
              {t('rack.createCancel')}
            </button>
            <button
              type="button"
              onClick={() => void submitEditRack()}
              className="h-9 rounded-xl bg-accent px-4 text-[13px] font-semibold text-black"
            >
              {t('rack.editSubmit')}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Confirmación in-app de borrado de rack (cascada incluida). */}
      <AlertDialog open={deleteRackOpen} onOpenChange={setDeleteRackOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('rack.deleteTitle')}</AlertDialogTitle>
            <AlertDialogDescription>{t('rack.deleteDesc', { name: rackById.get(activeRackId ?? '')?.name ?? '' })}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('rack.deleteCancel')}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void confirmDeleteRack()} className="bg-danger text-white hover:bg-danger/90">
              {t('rack.deleteConfirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

interface PickerDragPayload {
  type: 'device' | 'router' | 'accessory'
  mac?: string
  routerId?: string
  deviceType?: string
  label?: string
  faceplateId?: string
}

const DND_MIME = 'application/x-netpulse-rack'

function RackPicker({
  isAdmin,
  unmountedRouters,
  unmountedDevices,
  accessoryPlates,
  onRouter,
  onDevice,
  onAccessory,
}: {
  isAdmin: boolean
  unmountedRouters: Router[]
  unmountedDevices: { id: string; mac: string; type: string; name: string }[]
  accessoryPlates: typeof FACEPLATES
  onRouter: (r: Router) => void
  onDevice: (d: { mac: string; type: string; name: string }) => void
  onAccessory: (faceplateId: string) => void
}) {
  const { t } = useTranslation()
  const dragStart = (payload: PickerDragPayload) => (e: React.DragEvent) => {
    e.dataTransfer.setData(DND_MIME, JSON.stringify(payload))
    e.dataTransfer.effectAllowed = 'copy'
  }
  return (
    <aside className="hidden w-64 shrink-0 flex-col gap-4 overflow-y-auto rounded-xl border border-border bg-surface p-3 md:flex">
      <p className="text-[12px] leading-snug text-text-secondary">{t('rack.pickHint')}</p>
      <PickerGroup title={t('rack.pickFleet')}>
        {unmountedRouters.map((r) => (
          <PickerItem
            key={r.id}
            icon={<RouterIcon size={13} />}
            label={r.name}
            meta={r.modelShort || 'router'}
            disabled={!isAdmin}
            draggable={isAdmin}
            onDragStart={dragStart({ type: 'router', routerId: r.id, mac: r.mac, label: r.name })}
            onClick={() => onRouter(r)}
          />
        ))}
      </PickerGroup>
      <PickerGroup title={t('rack.pickDevice')}>
        {unmountedDevices.length === 0 && <p className="text-[11px] text-text-muted">—</p>}
        {unmountedDevices.slice(0, 60).map((d) => (
          <PickerItem
            key={d.id}
            icon={<TypeIcon type={d.type} />}
            label={d.name}
            meta={d.type}
            disabled={!isAdmin}
            draggable={isAdmin}
            onDragStart={dragStart({ type: 'device', mac: d.mac, deviceType: d.type, label: d.name })}
            onClick={() => onDevice(d)}
          />
        ))}
      </PickerGroup>
      <PickerGroup title={t('rack.pickAccessory')}>
        {accessoryPlates.map((p) => (
          <PickerItem
            key={p.id}
            label={t(p.nameKey)}
            meta={`${p.uHeight}U · ${p.colSpan}/12`}
            disabled={!isAdmin}
            draggable={isAdmin}
            onDragStart={dragStart({ type: 'accessory', faceplateId: p.id })}
            onClick={() => onAccessory(p.id)}
          />
        ))}
      </PickerGroup>
    </aside>
  )
}

function TypeIcon({ type }: { type: string }) {
  const cls = 'text-text-muted'
  switch (type) {
    case 'switch':
      return <Network size={13} className={cls} aria-hidden />
    case 'servidor':
      return <Server size={13} className={cls} aria-hidden />
    case 'ordenador':
      return <Monitor size={13} className={cls} aria-hidden />
    case 'camara':
      return <Cctv size={13} className={cls} aria-hidden />
    default:
      return <Network size={13} className={cls} aria-hidden />
  }
}

function PickerGroup({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <h3 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-text-muted">{title}</h3>
      <div className="flex flex-col gap-1">{children}</div>
    </div>
  )
}

function PickerItem({
  icon,
  label,
  meta,
  onClick,
  disabled,
  draggable,
  onDragStart,
}: {
  label: string
  meta: string
  onClick: () => void
  disabled?: boolean
  draggable?: boolean
  onDragStart?: (e: React.DragEvent) => void
  icon?: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      draggable={draggable}
      onDragStart={onDragStart}
      className="flex cursor-grab items-center justify-between gap-2 rounded-lg border border-border/60 px-2.5 py-1.5 text-left text-[13px] text-text-secondary transition-colors hover:border-accent/40 hover:text-accent active:cursor-grabbing disabled:opacity-50"
    >
      <span className="flex min-w-0 items-center gap-1.5">
        {icon}
        <span className="truncate">{label}</span>
      </span>
      <span className="shrink-0 text-[10px] uppercase text-text-muted">{meta}</span>
    </button>
  )
}

export default function Rack() {
  return (
    <ReactFlowProvider>
      <RackCanvas />
    </ReactFlowProvider>
  )
}
