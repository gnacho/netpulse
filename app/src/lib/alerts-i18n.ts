/**
 * Traducción de alertas por clave i18n (#671).
 *
 * El motor de alertas del server genera los textos (título, descripción,
 * hint) como literales, históricamente en español. Desde #671 el evento
 * lleva además el slug estable del tipo (`type`, las claves del mapa Hints de
 * #310) y las variables de interpolación (`vars`), y el frontend traduce por
 * clave en el idioma del usuario: `alerts.types.<slug>.title|.description` y
 * `alerts.hints.<slug>`.
 *
 * Fallback: si el evento no lleva `type` (servidores viejos, dataset demo,
 * tipos aún sin migrar) o falta la clave, se muestra el literal del server.
 */
import type { TFunction } from 'i18next'
import type { AlertEvent } from '@/data/types'
import { formatAlertTempVars, readTempUnit } from '@/lib/temperature'

export function alertTitle(t: TFunction, ev: AlertEvent): string {
  if (!ev.type) return ev.title
  return t(`alerts.types.${ev.type}.title`, { defaultValue: ev.title, ...(ev.vars ?? {}) })
}

export function alertDescription(t: TFunction, ev: AlertEvent): string {
  if (!ev.type) return ev.description
  // Las alertas de temperatura se muestran en la unidad elegida (issue #717):
  // la clave `descriptionUnit` espera `{{temp}}`/`{{threshold}}` ya formateados
  // con su unidad (la `description` original, con °C literal, se conserva para
  // el push del service worker, que no conoce la preferencia).
  if (ev.type === 'high-temperature' || ev.type === 'sfp-temp-high') {
    const vars = formatAlertTempVars(ev.type, ev.vars, readTempUnit())
    return t(`alerts.types.${ev.type}.descriptionUnit`, { defaultValue: ev.description, ...(vars ?? {}) })
  }
  // #796: los tipos con descripciones por resultado (vars.result, p. ej. la
  // alerta de auto-update) traducen con alerts.types.<slug>.results.<result>;
  // el literal del server en español queda solo como fallback.
  const resultKey = ev.vars?.result
  if (resultKey) {
    const translated = t(`alerts.types.${ev.type}.results.${resultKey}`, {
      defaultValue: '',
      ...(ev.vars ?? {}),
    })
    if (translated !== '') return translated
  }
  // An unknown device is found by where it is, not by its MAC: when the
  // server sends the IP and what it is plugged into, use the variant that
  // says so. The original key stays for alerts already stored without those
  // vars, which would otherwise render the placeholders unsubstituted.
  if (ev.type === 'unknown-device' && ev.vars?.ip && ev.vars?.where) {
    return t(`alerts.types.${ev.type}.descriptionWhere`, { defaultValue: ev.description, ...ev.vars })
  }
  return t(`alerts.types.${ev.type}.description`, { defaultValue: ev.description, ...(ev.vars ?? {}) })
}

/** Hint traducido; undefined cuando la alerta no lleva hint. */
export function alertHint(t: TFunction, ev: AlertEvent): string | undefined {
  if (!ev.hint) return undefined
  if (!ev.type) return ev.hint
  return t(`alerts.hints.${ev.type}`, { defaultValue: ev.hint })
}
