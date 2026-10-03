// rackApi.ts - tipos del wire (snake_case, espejo del contrato Go) y
// llamadas al API del rack canvas.

export interface RackDTO {
  id: string
  name: string
  u_height: number
  width_standard: string
  numbering: string
  style: string
  location?: string
  position_x: number
  position_y: number
}

export interface MountDTO {
  id: string
  rack_id: string
  device_mac?: string
  faceplate_id: string
  u_start: number
  u_height: number
  col_start: number
  col_span: number
  label?: string
  status_pin: string
  port_visibility: string
}

export interface CableDTO {
  id: string
  from_mount: string
  from_port: string
  to_mount: string
  to_port: string
  type: string
  label?: string
  properties?: string
  origin: string
  created_at: number
}

export type RackPortKindWire = 'rj45' | 'sfp' | 'sfp+'

export interface ProfilePortDTO {
  id: string
  kind: RackPortKindWire
  x: number
  y: number
}

export interface ProfileDTO {
  mac: string
  faceplate_id: string
  u_height: number
  col_span: number
  color: string
  ports: ProfilePortDTO[]
}

export interface RackBundle {
  racks: RackDTO[]
  mounts: MountDTO[]
  cables: CableDTO[]
  profiles: ProfileDTO[]
}

async function req<T>(input: string, init?: RequestInit): Promise<T> {
  const res = await fetch(input, init)
  if (!res.ok) {
    let msg = `HTTP ${res.status}`
    try {
      const body = await res.json()
      if (body?.message) msg = body.message
    } catch {
      /* mensaje por defecto */
    }
    throw new Error(msg)
  }
  return res.json() as Promise<T>
}

const json = (method: string, body: unknown): RequestInit => ({
  method,
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
})

export function fetchRackBundle(): Promise<RackBundle> {
  return req<RackBundle>('/api/racks')
}

export function createRack(body: Partial<RackDTO>): Promise<RackDTO> {
  return req<RackDTO>('/api/racks', json('POST', body))
}

export function saveRack(rack: RackDTO): Promise<{ ok: boolean }> {
  return req<{ ok: boolean }>(`/api/racks/${rack.id}`, json('PUT', rack))
}

export function deleteRack(id: string): Promise<{ ok: boolean }> {
  return req<{ ok: boolean }>(`/api/racks/${id}`, { method: 'DELETE' })
}

export function saveLayout(body: { rack_id: string; upsert: MountDTO[]; delete: string[] }): Promise<{ ok: boolean }> {
  return req<{ ok: boolean }>('/api/racks/layout', json('PUT', body))
}

export function addCable(body: Partial<CableDTO>): Promise<CableDTO> {
  return req<CableDTO>('/api/racks/cables', json('POST', body))
}

export function deleteCable(id: string): Promise<{ ok: boolean }> {
  return req<{ ok: boolean }>(`/api/racks/cables/${id}`, { method: 'DELETE' })
}

export function upsertProfile(mac: string, body: Omit<ProfileDTO, 'mac'>): Promise<ProfileDTO> {
  return req<ProfileDTO>(`/api/racks/profiles/${encodeURIComponent(mac)}`, json('PUT', body))
}
