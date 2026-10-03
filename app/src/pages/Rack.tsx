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
  ViewportPortal,
  useNodesState,
  Background,
  BackgroundVariant,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Cable, Plus, Save, Server, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { SectionHeader } from '@/components/SectionHeader'
import { SegmentedControl } from '@/components/SegmentedControl'
import { RackNode, type RackNodeType } from '@/components/rack/RackNode'
import { MountNode, type MountNodeType } from '@/components/rack/MountNode'
import { COL_PX, U_PX, mountRelPos, relPosToCell } from '@/components/rack/layout'
import type { CableVisibility, GhostState } from '@/components/rack/types'
import { findSlot, type Footprint } from '@/lib/rackGeometry'
import { FACEPLATES, getFaceplate, seedPorts, suggestFaceplate } from '@/lib/rackFaceplates'
import * as api from '@/lib/rackApi'
import type { CableDTO, MountDTO, ProfileDTO, RackDTO } from '@/lib/rackApi'
import { useNetPulse } from '@/data/DataProvider'
import { useAuth } from '@/data/AuthContext'

const nodeTypes = { rack: RackNode, mount: MountNode }

interface Working {
  racks: RackDTO[]
  mounts: MountDTO[]
  cables: CableDTO[]
  profiles: Map<string, ProfileDTO>
}

let localSeq = 0
const localId = (p: string) => `${p}-local-${++localSeq}`

function RackCanvas() {
  const { t } = useTranslation()
  const np = useNetPulse()
  const auth = useAuth()
  const isAdmin = auth?.role === 'admin'

  const [working, setWorking] = useState<Working>({ racks: [], mounts: [], cables: [], profiles: new Map() })
  const [loaded, setLoaded] = useState(false)
  const [activeRackId, setActiveRackId] = useState<string | null>(null)
  const [patchMode, setPatchMode] = useState(false)
  const [draft, setDraft] = useState<{ mountId: string; portId: string } | null>(null)
  const [visibility, setVisibility] = useState<CableVisibility>('hover')
  const [hoverMountId, setHoverMountId] = useState<string | null>(null)
  const [selectedCableId, setSelectedCableId] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

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
  const [nodes, setNodes, onNodesChange] = useNodesState<RackNodeType | MountNodeType>([])

  const reload = useCallback(async () => {
    const b = await api.fetchRackBundle()
    setWorking({
      racks: b.racks,
      mounts: b.mounts,
      cables: b.cables,
      profiles: new Map(b.profiles.map((p) => [p.mac, p])),
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
  }, [])

  useEffect(() => {
    reload().catch((e) => toast.error(String(e)))
  }, [reload])

  // --- Modelo derivado ---
  const rackById = useMemo(() => new Map(working.racks.map((r) => [r.id, r])), [working.racks])
  const mountById = useMemo(() => new Map(working.mounts.map((m) => [m.id, m])), [working.mounts])

  const plateFor = useCallback(
    (m: MountDTO) => {
      if (m.device_mac) {
        const prof = working.profiles.get(m.device_mac)
        return getFaceplate(prof?.faceplate_id || m.faceplate_id || '') || getFaceplate('blank-1u')!
      }
      return getFaceplate(m.faceplate_id || 'blank-1u') || getFaceplate('blank-1u')!
    },
    [working.profiles],
  )

  const portsFor = useCallback(
    (m: MountDTO) => {
      if (m.device_mac) return working.profiles.get(m.device_mac)?.ports ?? []
      return seedPorts(plateFor(m))
    },
    [working.profiles, plateFor],
  )

  const deviceByMac = useMemo(() => new Map(np.devices.map((d) => [d.mac.toLowerCase(), d])), [np.devices])

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
          uHeight: r.u_height,
          numbering: r.numbering,
          selected: activeRackId === r.id,
          ghost: ghostByRack.get(r.id) ?? null,
          onSelect: () => setActiveRackId(r.id),
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
          label: m.label || deviceByMac.get(m.device_mac?.toLowerCase() ?? '')?.name || '',
          color: m.device_mac ? working.profiles.get(m.device_mac)?.color || undefined : undefined,
          status: statusFor(m),
          patchFacing: plate.passThrough === true || plate.rows.reduce((n, r) => n + r.count, 0) >= 8,
          portsVisible: patchMode || hoverMountId === m.id,
          selected: false,
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
  }, [loaded, working, activeRackId, patchMode, draft, rackMoves, deletedCables, hoverMountId])

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
      const rack = m && rackById.get(m.rack_id)
      if (!m || !rack) return
      const cell = relPosToCell(rack.u_height, node.position.x, node.position.y, m.u_height)
      const fp = findSlot(rack.u_height, footprintsOfRack(rack.id, mountId), cell.uStart, cell.colStart, m.u_height, m.col_span)
      const ghost: GhostState | null = fp
        ? { ...fp, valid: true }
        : {
            uStart: Math.min(Math.max(cell.uStart, 1), rack.u_height - m.u_height + 1),
            uHeight: m.u_height,
            colStart: Math.min(Math.max(cell.colStart, 0), 12 - m.col_span),
            colSpan: m.col_span,
            valid: false,
          }
      setNodes((ns) =>
        ns.map((n) =>
          n.id === `rack-${rack.id}` && n.type === 'rack'
            ? { ...n, data: { ...n.data, ghost } }
            : n,
        ),
      )
    },
    [mountById, rackById, footprintsOfRack, setNodes],
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
      const rack = m && rackById.get(m.rack_id)
      if (!m || !rack) return
      const cell = relPosToCell(rack.u_height, node.position.x, node.position.y, m.u_height)
      const fp = findSlot(rack.u_height, footprintsOfRack(rack.id, mountId), cell.uStart, cell.colStart, m.u_height, m.col_span)
      setNodes((ns) =>
        ns.map((n) =>
          n.id === `rack-${rack.id}` && n.type === 'rack' ? { ...n, data: { ...n.data, ghost: null } } : n,
        ),
      )
      if (!fp) {
        toast.error(t('rack.noSpace'))
        return
      }
      if (fp.uStart === m.u_start && fp.colStart === m.col_start) return
      setWorking((w) => ({
        ...w,
        mounts: w.mounts.map((mm) => (mm.id === mountId ? { ...mm, u_start: fp.uStart, col_start: fp.colStart } : mm)),
      }))
      if (!newMountIds.has(mountId)) setMovedMountIds((s) => new Set(s).add(mountId))
    },
    [mountById, rackById, footprintsOfRack, newMountIds, setNodes, t],
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

  const mountDevice = useCallback(
    (mac: string, deviceType: string, label: string) => {
      const rackId = activeRackId ?? working.racks[0]?.id
      const rack = rackId ? rackById.get(rackId) : undefined
      if (!rack) {
        toast.error(t('rack.emptyHint'))
        return
      }
      const existing = working.profiles.get(mac)
      const plate = existing ? getFaceplate(existing.faceplate_id) : suggestFaceplate(deviceType)
      const uHeight = existing?.u_height ?? plate?.uHeight ?? 1
      const colSpan = existing?.col_span ?? plate?.colSpan ?? 12
      const fp = findSlot(rack.u_height, footprintsOfRack(rack.id), 1, 0, uHeight, colSpan)
      if (!fp) {
        toast.error(t('rack.noSpace'))
        return
      }
      if (!existing && plate) {
        const prof: ProfileDTO = {
          mac,
          faceplate_id: plate.id,
          u_height: plate.uHeight,
          col_span: plate.colSpan,
          color: '',
          ports: seedPorts(plate),
        }
        setProfileChanges((pm) => new Map(pm).set(mac, prof))
        setWorking((w) => ({ ...w, profiles: new Map(w.profiles).set(mac, prof) }))
      }
      mountAt(rack.id, fp, {
        device_mac: mac,
        faceplate_id: plate?.id ?? '',
        u_height: uHeight,
        col_span: colSpan,
        label,
      })
    },
    [activeRackId, working.racks, rackById, working.profiles, footprintsOfRack, mountAt, t],
  )

  const mountAccessory = useCallback(
    (faceplateId: string) => {
      const rackId = activeRackId ?? working.racks[0]?.id
      const rack = rackId ? rackById.get(rackId) : undefined
      const plate = getFaceplate(faceplateId)
      if (!rack || !plate) {
        toast.error(t('rack.emptyHint'))
        return
      }
      const fp = findSlot(rack.u_height, footprintsOfRack(rack.id), 1, 0, plate.uHeight, plate.colSpan)
      if (!fp) {
        toast.error(t('rack.noSpace'))
        return
      }
      mountAt(rack.id, fp, {
        device_mac: '',
        faceplate_id: faceplateId,
        u_height: plate.uHeight,
        col_span: plate.colSpan,
        label: '',
      })
    },
    [activeRackId, working.racks, rackById, footprintsOfRack, mountAt, t],
  )

  // --- Rack ops ---
  const addRack = useCallback(async () => {
    try {
      const r = await api.createRack({ name: `Rack ${working.racks.length + 1}`, u_height: 12, position_x: 40 + working.racks.length * 280, position_y: 40 })
      setWorking((w) => ({ ...w, racks: [...w.racks, r] }))
      setActiveRackId(r.id)
    } catch (e) {
      toast.error(String(e))
    }
  }, [working.racks])

  const removeRack = useCallback(async () => {
    const rackId = activeRackId
    if (!rackId) return
    if (!window.confirm(t('rack.deleteRackConfirm'))) return
    try {
      await api.deleteRack(rackId)
      toast.success(t('rack.rackDeleted'))
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
      for (const cid of deletedCables) {
        if (!newCables.has(cid)) await api.deleteCable(cid).catch(() => undefined)
      }
      for (const c of working.cables) {
        if (!newCables.has(c.id)) continue
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
  // --- Cable selection + Delete ---
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setDraft(null)
        setSelectedCableId(null)
      }
      if ((e.key === 'Delete' || e.key === 'Backspace') && selectedCableId) {
        setDeletedCables((s) => new Set(s).add(selectedCableId))
        setWorking((w) => ({ ...w, cables: w.cables.filter((c) => c.id !== selectedCableId) }))
        setSelectedCableId(null)
        toast.success(t('rack.cableDeleted'))
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [selectedCableId, t])

  // --- Cables: posiciones de puerto absolutas (flow coords) ---
  const absPort = useCallback(
    (mountId: string, portId: string): { x: number; y: number } | null => {
      const m = mountById.get(mountId)
      const rack = m && rackById.get(m.rack_id)
      const rackNode = nodes.find((n) => n.id === `rack-${m?.rack_id}`)
      if (!m || !rack || !rackNode) return null
      const rel = mountRelPos(rack.u_height, m.u_start, m.u_height, m.col_start)
      const p = portsFor(m).find((pp) => pp.id === portId)
      if (!p) return null
      return {
        x: rackNode.position.x + rel.x + p.x * m.col_span * COL_PX,
        y: rackNode.position.y + rel.y + p.y * m.u_height * U_PX,
      }
    },
    [mountById, rackById, nodes, portsFor],
  )

  const cableVisible = useCallback(
    (c: CableDTO) => {
      if (c.id === selectedCableId) return true
      if (patchMode || visibility === 'always') return true
      if (visibility === 'hidden') return false
      return c.from_mount === hoverMountId || c.to_mount === hoverMountId
    },
    [selectedCableId, patchMode, visibility, hoverMountId],
  )

  // --- Picker data ---
  const mountedMacs = useMemo(
    () => new Set(working.mounts.map((m) => m.device_mac?.toLowerCase() ?? '')),
    [working.mounts],
  )
  const unmountedDevices = useMemo(
    () => np.devices.filter((d) => !mountedMacs.has(d.mac.toLowerCase())),
    [np.devices, mountedMacs],
  )
  const unmountedRouters = useMemo(
    () => np.routers.filter((r) => r.mac && !mountedMacs.has(r.mac.toLowerCase())),
    [np.routers, mountedMacs],
  )
  const accessoryPlates = useMemo(() => FACEPLATES.filter((p) => p.group === 'accessory'), [])

  return (
    <div className="flex h-full flex-col">
      <SectionHeader title={t('rack.title')}>
        <p className="mt-0.5 text-sm text-text-secondary">{t('rack.subtitle')}</p>
      </SectionHeader>

      {/* Toolbar */}
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={addRack}
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
        <div className="ml-auto flex items-center gap-2">
          {activeRackId && (
            <button
              type="button"
              onClick={removeRack}
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

      <div className="mt-3 flex min-h-0 flex-1 gap-3">
        {/* Canvas */}
        <div className="relative min-w-0 flex-1 overflow-hidden rounded-xl border border-border bg-canvas">
          {!loaded ? null : working.racks.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center gap-3 text-text-secondary">
              <Server size={32} className="opacity-40" />
              <p className="text-sm">{t('rack.empty')}</p>
              {isAdmin && (
                <button
                  type="button"
                  onClick={addRack}
                  className="flex h-9 items-center gap-1.5 rounded-xl bg-accent px-4 text-[13px] font-semibold text-black"
                >
                  <Plus size={15} /> {t('rack.addRack')}
                </button>
              )}
            </div>
          ) : (
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
              minZoom={0.25}
              maxZoom={2.5}
              proOptions={{ hideAttribution: true }}
              fitView
            >
              <Background variant={BackgroundVariant.Dots} gap={24} size={1} />
              <ViewportPortal>
                <svg style={{ position: 'absolute', left: 0, top: 0 }}>
                  {working.cables.map((c) => {
                    if (!cableVisible(c)) return null
                    const a = absPort(c.from_mount, c.from_port)
                    const b = absPort(c.to_mount, c.to_port)
                    if (!a || !b) return null
                    const sel = c.id === selectedCableId
                    const stroke = c.type === 'fiber' ? 'rgb(var(--warn))' : sel ? 'rgb(var(--accent))' : 'rgb(var(--text-muted))'
                    return (
                      <g key={c.id} onClick={(e) => { e.stopPropagation(); setSelectedCableId(sel ? null : c.id) }} style={{ cursor: 'pointer' }}>
                        <line x1={a.x} y1={a.y} x2={b.x} y2={b.y} stroke="transparent" strokeWidth={12} />
                        <line
                          x1={a.x}
                          y1={a.y}
                          x2={b.x}
                          y2={b.y}
                          stroke={stroke}
                          strokeWidth={sel ? 2.5 : 1.5}
                          strokeDasharray={c.origin === 'manual' ? undefined : '6 4'}
                        />
                      </g>
                    )
                  })}
                </svg>
              </ViewportPortal>
            </ReactFlow>
          )}
          {draft && (
            <div className="pointer-events-none absolute bottom-3 left-1/2 -translate-x-1/2 rounded-lg border border-border bg-surface px-3 py-1.5 text-[12px] text-text-secondary">
              {t('rack.drafting', { port: draft.portId })}
            </div>
          )}
        </div>

        {/* Picker */}
        <aside className="hidden w-64 shrink-0 flex-col gap-4 overflow-y-auto rounded-xl border border-border bg-surface p-3 md:flex">
          <p className="text-[12px] leading-snug text-text-secondary">{t('rack.pickHint')}</p>
          <PickerGroup title={t('rack.pickFleet')}>
            {unmountedRouters.map((r) => (
              <PickerItem key={r.id} label={r.name} meta={r.modelShort || 'router'} onClick={() => r.mac && mountDevice(r.mac, 'router', r.name)} disabled={!isAdmin} />
            ))}
          </PickerGroup>
          <PickerGroup title={t('rack.pickDevice')}>
            {unmountedDevices.length === 0 && <p className="text-[11px] text-text-muted">—</p>}
            {unmountedDevices.slice(0, 60).map((d) => (
              <PickerItem key={d.id} label={d.name} meta={d.type} onClick={() => mountDevice(d.mac, d.type, d.name)} disabled={!isAdmin} />
            ))}
          </PickerGroup>
          <PickerGroup title={t('rack.pickAccessory')}>
            {accessoryPlates.map((p) => (
              <PickerItem key={p.id} label={t(p.nameKey)} meta={`${p.uHeight}U · ${p.colSpan}/12`} onClick={() => mountAccessory(p.id)} disabled={!isAdmin} />
            ))}
          </PickerGroup>
        </aside>
      </div>
    </div>
  )
}

function PickerGroup({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <h3 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-text-muted">{title}</h3>
      <div className="flex flex-col gap-1">{children}</div>
    </div>
  )
}

function PickerItem({ label, meta, onClick, disabled }: { label: string; meta: string; onClick: () => void; disabled?: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className="flex items-center justify-between gap-2 rounded-lg border border-border/60 px-2.5 py-1.5 text-left text-[12px] text-text-secondary transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50"
    >
      <span className="truncate">{label}</span>
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
