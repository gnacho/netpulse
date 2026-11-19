import { useEffect, useState } from 'react'

/** Versión del server, pedida una vez y cacheada en el módulo.
 *
 *  El sello del sidebar muestra "version · build" (#1274) y la página de
 *  novedades decide con la misma versión si hay cambios sin ver (#2116). La
 *  caché de módulo evita que Sidebar, Rail y la página dupliquen la petición
 *  y, sobre todo, que dos consumidores muestren versiones distintas. */
let serverVersionCache = ''

export function useServerVersion(): string {
  const [v, setV] = useState(serverVersionCache)
  useEffect(() => {
    if (serverVersionCache) return
    let disposed = false
    fetch('/api/health', { signal: AbortSignal.timeout(3000) })
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        const ver = typeof d?.version === 'string' ? d.version : ''
        if (!disposed && ver) {
          serverVersionCache = ver
          setV(ver)
        }
      })
      .catch(() => {})
    return () => {
      disposed = true
    }
  }, [])
  return v
}
