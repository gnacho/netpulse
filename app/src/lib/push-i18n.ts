/**
 * i18n de las notificaciones push (#689).
 *
 * El motor de alertas del server compone el push con literales en español y,
 * desde #689, envía también el slug del tipo (`type`) y sus variables (`vars`).
 * El Service Worker traduce con el idioma del dispositivo usando los mismos
 * catálogos de la app (`public/locales/<lang>/translation.json`), sin duplicar
 * textos: claves `alerts.types.<slug>.title|.description` y `alerts.hints.<slug>`.
 *
 * Funciones puras para poder probarlas fuera del SW.
 */

/** Subconjunto del catálogo de traducciones que necesita el push. */
export interface PushCatalog {
  alerts?: {
      types?: Record<string, { title?: string; description?: string; results?: Record<string, string>; descriptionWhere?: string }>
    hints?: Record<string, string>
  }
}

export interface PushFallback {
  title?: string
  body?: string
  hint?: string
}

export interface LocalizedPush {
  title: string
  body: string
  hint?: string
}

/** Idioma de la notificación a partir del idioma del navegador (en* → en, resto → es). */
export function pushLang(navLang: string | undefined): 'es' | 'en' {
  return (navLang ?? '').toLowerCase().startsWith('en') ? 'en' : 'es'
}

/** Interpola `{{var}}` con las variables del evento (mismo formato que i18next). */
export function interpolate(tpl: string, vars: Record<string, string> | undefined): string {
  if (!vars) return tpl
  return tpl.replace(/\{\{(\w+)\}\}/g, (match, key: string) => (key in vars ? vars[key]! : match))
}

/** True when every `{{var}}` of the template has a value in `vars`. */
function resolvable(tpl: string, vars: Record<string, string> | undefined): boolean {
  return [...tpl.matchAll(/\{\{(\w+)\}\}/g)].every((m) => !!vars?.[m[1]!])
}

/**
 * Traduce title/description/hint de un evento tipado. Si no hay `type`, falta
 * la clave o el catálogo no cargó, cae a los literales del server (fallback).
 * Los tipos con descripciones por resultado (vars.result, #796) usan
 * `results.<result>` antes que la descripción genérica.
 */
export function localizePush(
  catalog: PushCatalog | null,
  type: string | undefined,
  vars: Record<string, string> | undefined,
  fallback: PushFallback,
): LocalizedPush {
  const entry = type ? catalog?.alerts?.types?.[type] : undefined
  const title = entry?.title ? interpolate(entry.title, vars) : fallback.title || 'NetPulse'
    // Upstream's per-result template wins when the event carries a result
    // code; otherwise a richer variant (`descriptionWhere`, which names the IP
    // and what the client is plugged into) when every value it needs is
    // present; otherwise the plain description, which always resolves.
    const resultKey = vars?.result
    const resultTpl = resultKey ? entry?.results?.[resultKey] : undefined
    const tpl =
      resultTpl ??
      (entry?.descriptionWhere && resolvable(entry.descriptionWhere, vars)
        ? entry.descriptionWhere
        : entry?.description)
    const body = tpl ? interpolate(tpl, vars) : fallback.body ?? ''
  // El hint solo se muestra si el evento traía uno; se prefiere la clave i18n.
  const hint = fallback.hint ? (type ? catalog?.alerts?.hints?.[type] ?? fallback.hint : fallback.hint) : undefined
  return { title, body, hint }
}
