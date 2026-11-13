/** #2010: perfiles de rack sembrados antes de 2.51.0.
 *
 * El filtro por `family` (lag, vlan, bridge, tunnel, virtual, cpu) solo corría
 * al sembrar un perfil, así que los guardados antes de esa versión conservan
 * pseudo-puertos para siempre y el frontal nunca coincide con el panel de
 * puertos del detalle. Aquí se decide qué puertos del perfil ya no existen
 * según el sondeo, sin tocar nada: la pantalla pinta lo vivo y la limpieza es
 * una acción explícita del usuario.
 */

export interface StalePortsEntry {
  mac: string
  mountId: string
  /** ids guardados que el dispositivo ya no informa como físicos */
  ids: string[]
  /** subconjunto de `ids` con un cable encima: no se pueden quitar solos */
  cabled: string[]
}

export function computeStalePorts(
  mounts: { id: string; device_mac?: string }[],
  profiles: Map<string, { ports: { id: string }[] }>,
  liveByMac: Map<string, string[]>,
  hasCable: (mountId: string, portId: string) => boolean,
): StalePortsEntry[] {
  const out: StalePortsEntry[] = []
  for (const m of mounts) {
    const mac = m.device_mac?.toLowerCase()
    if (!mac) continue
    const prof = profiles.get(mac)
    const live = liveByMac.get(mac)
    // Sin perfil o sin sondeo (unidad caída, sin agente) no se marca nada:
    // una lista vacía no es prueba de que el puerto ya no exista.
    if (!prof || !live || live.length === 0) continue
    const liveSet = new Set(live)
    const ids = prof.ports.filter((p) => !liveSet.has(p.id)).map((p) => p.id)
    if (ids.length === 0) continue
    const cabled = ids.filter((id) => hasCable(m.id, id))
    out.push({ mac, mountId: m.id, ids, cabled })
  }
  return out
}
