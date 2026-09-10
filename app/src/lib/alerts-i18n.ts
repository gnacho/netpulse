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

export function alertTitle(t: TFunction, ev: AlertEvent): string {
  if (!ev.type) return ev.title
  return t(`alerts.types.${ev.type}.title`, { defaultValue: ev.title, ...(ev.vars ?? {}) })
}

export function alertDescription(t: TFunction, ev: AlertEvent): string {
  if (!ev.type) return ev.description
  return t(`alerts.types.${ev.type}.description`, { defaultValue: ev.description, ...(ev.vars ?? {}) })
}

/** Hint traducido; undefined cuando la alerta no lleva hint. */
export function alertHint(t: TFunction, ev: AlertEvent): string | undefined {
  if (!ev.hint) return undefined
  if (!ev.type) return ev.hint
  return t(`alerts.hints.${ev.type}`, { defaultValue: ev.hint })
}
