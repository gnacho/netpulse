import { Activity, Languages, Radar, Shapes } from 'lucide-react'
import type { WhatsNewCard } from './types'
import { IconPickerDemo } from './cards/IconPickerDemo'
import { DnsDemo } from './cards/DnsDemo'
import { PresenceDemo } from './cards/PresenceDemo'
import { LanguagesDemo } from './cards/LanguagesDemo'

/**
 * Registro de novedades (#2116): la única fuente de qué se cuenta de cada
 * release. Añadir una release es añadir tarjetas aquí y sus textos en los
 * cuatro catálogos, nada más.
 *
 * La demo de cada tarjeta usa los tokens y los componentes reales cuando puede
 * (el catálogo de iconos, las etiquetas traducidas), y datos de muestra cuando
 * la pantalla necesita un backend. Nunca una captura: envejecen y no siguen al
 * tema ni al idioma.
 */
export const CARDS: WhatsNewCard[] = [
  {
    id: 'icon-picker',
    version: '2.54.0',
    icon: Shapes,
    author: 'Bann-DEV',
    pr: 2107,
    Demo: IconPickerDemo,
  },
  {
    id: 'dns-activity',
    version: '2.54.0',
    icon: Radar,
    author: 'gnacho',
    pr: 2081,
    Demo: DnsDemo,
  },
  {
    id: 'presence',
    version: '2.54.0',
    icon: Activity,
    author: 'gnacho',
    pr: 2108,
    Demo: PresenceDemo,
  },
  {
    id: 'languages',
    version: '2.54.0',
    icon: Languages,
    author: 'Bann-DEV',
    pr: 2070,
    Demo: LanguagesDemo,
  },
]

/** Compara versiones numéricas ("2.54.0" contra "2.53.99"), sin prerelease. */
function compareVersions(a: string, b: string): number {
  const pa = a.split('.').map((n) => Number.parseInt(n, 10) || 0)
  const pb = b.split('.').map((n) => Number.parseInt(n, 10) || 0)
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const d = (pa[i] ?? 0) - (pb[i] ?? 0)
    if (d !== 0) return d
  }
  return 0
}

/** Releases con tarjetas, la más nueva primero. */
export const CARD_VERSIONS: string[] = [...new Set(CARDS.map((c) => c.version))].sort(compareVersions).reverse()

/** La release más nueva del registro: la que enciende el punto del sidebar. */
export const LATEST_CARD_VERSION: string = CARD_VERSIONS[0] ?? ''

export function cardsForVersion(version: string): WhatsNewCard[] {
  return CARDS.filter((c) => c.version === version)
}
