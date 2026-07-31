import { useEffect, useRef, useState } from 'react'
import { animate, useReducedMotion } from 'framer-motion'
import { fmtEs } from '@/data/mock'
import { numLocale } from '@/i18n'

interface CountUpProps {
  value: number
  decimals?: number
  duration?: number
  /** Al cambiar, re-anima desde 0 (p. ej. refresh global) */
  nonce?: number
  className?: string
}

/** Cifra con count-up 800ms easeOutCubic (design.md §8). Respeta reduced-motion. */
export function CountUp({ value, decimals = 0, duration = 0.8, nonce, className }: CountUpProps) {
  const [display, setDisplay] = useState(0)
  const prev = useRef(0)
  const reduce = useReducedMotion()

  useEffect(() => {
    const from = nonce !== undefined ? 0 : prev.current
    if (reduce) {
      setDisplay(value)
      prev.current = value
      return
    }
    const controls = animate(from, value, {
      duration,
      ease: [0.33, 1, 0.68, 1], // easeOutCubic
      onUpdate: (v) => setDisplay(v),
    })
    prev.current = value
    return () => controls.stop()
  }, [value, duration, reduce, nonce])

  return <span className={className}>{decimals > 0 ? fmtEs(display, decimals) : Math.round(display).toLocaleString(numLocale())}</span>
}
