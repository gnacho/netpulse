/** #1818: etiquetas de cliente que no pueden inventarse.
 *
 * Una MAC que solo existe en una FDB (el NMRP de Netgear al reiniciar para
 * flashear firmware) no tiene concesión DHCP ni hostname. Antes se pintaba como
 * `Static IP (reservation)` estando online y como `Expired` al desaparecer, y un
 * nombre vacío producía el hostname "cliente" (slug de cadena vacía).
 *
 * Aquí van las dos decisiones, puras, para poder probarlas sin montar la UI.
 */

/** Formatea segundos restantes del lease a "2h 15min" · "1d 4h" · "45min". */
export function formatLeaseRemaining(totalSeconds: number): string {
  const days = Math.floor(totalSeconds / 86400)
  const hours = Math.floor((totalSeconds % 86400) / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const parts: string[] = []
  if (days > 0) parts.push(`${days}d`)
  if (hours > 0) parts.push(`${hours}h`)
  if (minutes > 0 || parts.length === 0) parts.push(`${minutes}min`)
  return parts.join(' ')
}

/**
 * Etiqueta del lease en formato canónico (el que traduce `dhcpLease()`).
 * Sin dato (`null`/`undefined`) NO se inventa nada: ni reserva estática ni
 * caducidad. Un 0 sí es un lease caducado de verdad.
 */
export function leaseLabel(remaining: number | null | undefined): string {
  if (remaining == null) return '—'
  if (remaining <= 0) return 'Expirado'
  return `renueva en ${formatLeaseRemaining(remaining)}`
}

/** El fallback del server sin hostname es la MAC (con dos puntos o guiones). */
export function isMACName(name: string): boolean {
  return /^([0-9a-f]{2}[:-]){5}[0-9a-f]{2}$/i.test(name.trim())
}

function slug(name: string): string {
  return (
    name
      .toLowerCase()
      .normalize('NFD')
      .replace(/[\u0300-\u036f]/g, '')
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-|-$/g, '') || 'cliente'
  )
}

/**
 * Hostname del cliente. La MAC o un nombre vacío no son un hostname: el slug de
 * una cadena vacía producía "cliente" en el detalle de una entrada sin nombre.
 */
export function hostnameLabel(name: string): string {
  const n = name.trim()
  if (n === '' || isMACName(n)) return ''
  return slug(n)
}
