import type { ComponentType } from 'react'
import type { LucideIcon } from 'lucide-react'

/**
 * Una novedad de una release (#2116): un cambio contado con una demo de la
 * interacción real, no con una línea de changelog.
 *
 * Los textos NO viven aquí: viven en `whatsnew.cards.<id>.{title,body}` de los
 * cuatro catálogos. Así una tarjeta nueva se traduce igual que el resto de la
 * UI y el catálogo se puede revisar de una vez.
 */
export interface WhatsNewCard {
  /** Clave estable: sufijo i18n, y lo que enlaza release notes y página. */
  id: string
  /** Versión en la que entró el cambio, sin la `v`. */
  version: string
  icon: LucideIcon
  /** Login de GitHub de quien lo escribió. */
  author: string
  /** Pull request que lo trajo. */
  pr: number
  /** Demo animada; se mide sola y respeta `prefers-reduced-motion`. */
  Demo: ComponentType
}
