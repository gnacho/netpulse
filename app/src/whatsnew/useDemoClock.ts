import { useEffect, useState } from 'react'
import { useReducedMotion } from 'framer-motion'

/**
 * Reloj de una demo de novedades (#2116): un contador que avanza cada `ms` y
 * vuelve a cero, para que la animación se repita sin acumular timers.
 *
 * Con `prefers-reduced-motion` no hay reloj: devuelve siempre el último paso,
 * que cada demo dibuja como estado final. El texto de la tarjeta cuenta el
 * cambio, así que la versión quieta sigue explicando lo mismo.
 */
export function useDemoClock(steps: number, ms = 320): number {
  const reduce = useReducedMotion()
  const [step, setStep] = useState(0)

  useEffect(() => {
    if (reduce) return
    const id = window.setInterval(() => setStep((s) => (s + 1) % steps), ms)
    return () => window.clearInterval(id)
  }, [reduce, steps, ms])

  return reduce ? steps - 1 : step
}
