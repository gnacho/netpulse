/**
 * Navegación de novedades (#2116): qué versión de las que traen tarjetas ha
 * visto ya este navegador.
 *
 * Se guarda la versión en `localStorage`, no un booleano, para que un update
 * vuelva a encender el punto sin tocar código y para que bajar de versión no
 * lo encienda. Si el almacenamiento no está disponible (modo privado, cuota),
 * el punto no se muestra: mejor no avisar que avisar siempre en falso.
 */
import { useEffect, useState } from 'react'

const SEEN_KEY = 'netpulse.whatsnew.seen'

export function seenWhatsNewVersion(): string {
  try {
    return localStorage.getItem(SEEN_KEY) ?? ''
  } catch {
    return ''
  }
}

export function markWhatsNewSeen(version: string) {
  if (!version) return
  try {
    localStorage.setItem(SEEN_KEY, version)
  } catch {
    // sin almacenamiento: el punto se queda encendido en esta sesión
  }
}

/** ¿Hay novedades sin ver para la versión que corre el servidor? */
export function hasUnseenWhatsNew(version: string): boolean {
  if (!version) return false
  return seenWhatsNewVersion() !== version
}

/** Estado reactivo del punto, para que se apague al abrir la página sin
 *  recargar. */
export function useWhatsNewBadge(version: string): { unseen: boolean; markSeen: () => void } {
  const [seen, setSeen] = useState(() => seenWhatsNewVersion())
  useEffect(() => {
    // la versión llega async: el visto se compara al renderizar, no al montar
    setSeen(seenWhatsNewVersion())
  }, [version])
  return {
    unseen: !!version && seen !== version,
    markSeen: () => {
      markWhatsNewSeen(version)
      setSeen(version)
    },
  }
}
