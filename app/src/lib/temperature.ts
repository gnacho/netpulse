/**
 * Preferencia de unidad de temperatura (issue #717).
 *
 * El servidor almacena y expone las temperaturas SIEMPRE en Celsius (grados).
 * Esta preferencia es SOLO de presentación: convierte al formatear para
 * pintar, sin tocar el dato ni el contrato del servidor.
 *
 * Persistencia en localStorage (`netpulse-temp-unit`, 'c' | 'f') con un
 * evento de cambio (`netpulse-temp-unit-change`) para que los componentes ya
 * montados se actualicen sin recargar, patrón análogo a `netpulse-refresh-change`.
 */
import { useEffect, useState } from 'react'
import { fmtEs } from '@/data/mock'

export type TempUnit = 'c' | 'f'

const STORAGE_KEY = 'netpulse-temp-unit'
const CHANGE_EVENT = 'netpulse-temp-unit-change'

/** Lee la unidad guardada; 'c' por defecto (o si el valor no es válido). */
export function readTempUnit(): TempUnit {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw === 'f' ? 'f' : 'c'
  } catch {
    return 'c'
  }
}

/** Convierte un valor en Celsius al valor en la unidad elegida. */
export function toTempUnit(celsius: number, unit: TempUnit): number {
  return unit === 'f' ? (celsius * 9) / 5 + 32 : celsius
}

/** Símbolo de la unidad. */
export function tempUnitSymbol(unit: TempUnit): string {
  return unit === 'f' ? '°F' : '°C'
}

/**
 * Formatea una temperatura en Celsius como string con la unidad elegida:
 * `fmtTemp(65, 'f')` → "149 °F", `fmtTemp(21.1, 'f', 1)` → "70.0 °F".
 * `celsius === null` → "—".
 */
export function fmtTemp(celsius: number | null, unit: TempUnit, decimals = 0): string {
  if (celsius === null) return '—'
  const v = toTempUnit(celsius, unit)
  const rounded = Number(v.toFixed(decimals))
  return `${fmtEs(rounded, decimals)} ${tempUnitSymbol(unit)}`
}

/** Escribe la unidad y notifica a los componentes suscritos. */
export function setTempUnit(unit: TempUnit): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(unit))
  } catch {
    /* modo privado */
  }
  window.dispatchEvent(new Event(CHANGE_EVENT))
}

/** Hook: unidad actual + setter reactivo a cambios desde Ajustes. */
export function useTempUnit(): [TempUnit, (u: TempUnit) => void] {
  const [unit, setUnit] = useState<TempUnit>(readTempUnit)
  useEffect(() => {
    const onChange = () => setUnit(readTempUnit())
    window.addEventListener(CHANGE_EVENT, onChange)
    return () => window.removeEventListener(CHANGE_EVENT, onChange)
  }, [])
  return [unit, setTempUnit]
}

/**
 * Formatea las variables de temperatura de una alerta (`temp` y, si existe,
 * `threshold`) según la unidad elegida. El resto de variables se conserva tal
 * cual. Solo afecta a los tipos de alerta que llevan temperaturas.
 */
export function formatAlertTempVars(
  type: string | undefined,
  vars: Record<string, string> | undefined,
  unit: TempUnit,
): Record<string, string> | undefined {
  if (!vars) return vars
  if (type !== 'high-temperature' && type !== 'sfp-temp-high') return vars
  const next: Record<string, string> = { ...vars }
  const fmt = (raw: string) => {
    const n = Number(raw)
    if (!Number.isFinite(n)) return raw
    return fmtTemp(n, unit, raw.includes('.') ? 1 : 0)
  }
  if (next.temp !== undefined) next.temp = fmt(next.temp)
  if (next.threshold !== undefined) next.threshold = fmt(next.threshold)
  return next
}
