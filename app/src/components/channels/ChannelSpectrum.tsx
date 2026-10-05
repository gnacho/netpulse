import { useEffect, useRef } from 'react'

// ChannelSpectrum (#1070): cascada de ocupación por canal dibujada en canvas
// (campanas gaussianas con degradado, réplica del channel analysis de LuCI
// pero con los tokens de NetPulse y eje de frecuencia real). Incluye la
// franja de calor de ocupación bajo el gráfico. Las campanas usan SIEMPRE
// frecuencias reales (channelFreq); el ancho es el real de la red propia y
// 20 MHz para vecinas (el scan no reporta ancho, follow-up de #1070).

export interface SpectrumNet {
  key: string // bssid + canal (id estable)
  ssid: string
  bssid: string
  channel: number
  freq: number
  signal: number
  widthMhz: number
  own: boolean
  color: string
}

export const BAND_RANGE: Record<string, [number, number]> = {
  // Ajustados a la extensión real de las campanas (#1076): antes el margen
  // sobrante dejaba hueco antes del ch1 y después del 13 (y tras el 165).
  '2.4 GHz': [2402, 2482],
  '5 GHz': [5170, 5862],
  '6 GHz': [5955, 7115],
}

export const BAND_CHANNELS: Record<string, number[]> = {
  '2.4 GHz': Array.from({ length: 13 }, (_, i) => i + 1),
  '5 GHz': [36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116, 120, 124, 128, 132, 136, 140, 144, 149, 153, 157, 161, 165],
}

const DBM_TOP = -25
const DBM_BOTTOM = -95
const PAD = { l: 44, r: 14, t: 30, b: 30 }

export function channelFreq(band: string, ch: number): number {
  if (band === '2.4 GHz') {
    if (ch === 14) return 2484
    if (ch >= 1 && ch <= 13) return 2407 + ch * 5
    return 0
  }
  if (band === '5 GHz') {
    if (ch >= 36 && ch <= 165) return 5000 + ch * 5
    return 0
  }
  if (band === '6 GHz') {
    if (ch >= 1 && ch <= 229) return 5955 + (ch - 1) * 5
    return 0
  }
  return 0
}

// channelOfFreq redondea una frecuencia a su canal: el eje del gráfico usa
// SLOTS uniformes por canal (como LuCI), no el eje de MHz continuo (#1214).
export function channelOfFreq(f: number): number {
  if (f <= 0) return 0
  if (f < 2500) return Math.round((f - 2407) / 5)
  if (f >= 5000 && f <= 6000) return Math.round((f - 5000) / 5)
  return 0
}

export function isDfsChannel(band: string, ch: number): boolean {
  if (band !== '5 GHz') return false
  return (ch >= 52 && ch <= 64) || (ch >= 100 && ch <= 144)
}

// themeColor resuelve un token del tema (triplete "r g b" de index.css) para
// pintarlo en canvas/SVG; se re-lee en cada render para que el cambio claro/
// oscuro repinte el gráfico.
export function themeColor(name: string): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v ? `rgb(${v})` : '#888'
}


// withAlpha aplica opacidad a un color en formato "#rrggbb" o "rgb(r g b)":
// NO vale concatenar sufijos hex (rompe con los rgb() de themeColor; bug que
// tiraba la pagina con "CanvasGradient.addColorStop: Invalid color").
export function withAlpha(color: string, alpha: number): string {
  const hexMatch = /^#([0-9a-f]{6})$/i.exec(color)
  if (hexMatch) {
    return `${color}${Math.round(alpha * 255).toString(16).padStart(2, '0')}`
  }
  const rgbMatch = /^rgb\(([^)]+)\)$/.exec(color)
  if (rgbMatch) {
    const parts = rgbMatch[1]!.trim().split(/[\s,]+/)
    return `rgba(${parts[0]}, ${parts[1]}, ${parts[2]}, ${alpha})`
  }
  return color
}

// Fuentes del canvas: la stack real del tema (Space Grotesk/Inter viven
// locales, sin CDN).
const CANVAS_FONT = '11px Inter, ui-sans-serif, system-ui, sans-serif'
const CANVAS_FONT_BOLD = '700 11.5px Inter, ui-sans-serif, system-ui, sans-serif'
const CANVAS_FONT_SEMI = '600 11px Inter, ui-sans-serif, system-ui, sans-serif'

export function ChannelSpectrum({
  band,
  nets,
  suggested,
  widthMhz,
  hidden,
  selected,
  focus,
  onHover,
  tSuggest,
  tDfs,
  tActual,
  tDbm,
}: {
  band: string
  nets: SpectrumNet[]
  suggested: number
  widthMhz: number
  hidden: Set<string>
  selected: string | null
  /** Canal con foco (click en la tira de puntuación): el resto se atenúa. */
  focus: number | null
  onHover: (net: SpectrumNet | null, x: number, y: number) => void
  tSuggest: string
  tDfs: string
  tActual: string
  tDbm: string
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const heatRef = useRef<HTMLCanvasElement>(null)
  const geomRef = useRef<{ x: (ch: number) => number; y: (d: number) => number } | null>(null)

  useEffect(() => {
    const cv = canvasRef.current
    const heat = heatRef.current
    if (!cv || !heat) return
    const range = BAND_RANGE[band]
    if (!range) return

    const draw = () => {
      const colors = {
        border: themeColor('--border'),
        muted: themeColor('--text-muted'),
        faint: themeColor('--border-strong'),
        tunnel: themeColor('--tunnel'),
        ok: themeColor('--ok'),
        accent: themeColor('--accent'),
      }
      const dpr = window.devicePixelRatio || 1

      const setup = (c: HTMLCanvasElement, cssH: number) => {
        const r = c.getBoundingClientRect()
        c.width = Math.max(r.width * dpr, 1)
        c.height = cssH * dpr
        c.style.height = `${cssH}px`
        const g = c.getContext('2d')
        if (g) g.setTransform(dpr, 0, 0, dpr, 0, 0)
        return { g, w: r.width, h: cssH }
      }

      const { g, w, h } = setup(cv, 340)
      if (!g) return
      const plotW = w - PAD.l - PAD.r
      const plotH = h - PAD.t - PAD.b
      // Eje de SLOTS uniformes por canal (como LuCI, #1214): cada canal
      // ocupa una franja fija y una red se dibuja sobre el slot de su canal.
      const bandChs = BAND_CHANNELS[band] ?? []
      const slotW = plotW / Math.max(bandChs.length, 1)
      const slotOf = (ch: number) => bandChs.indexOf(ch)
      const xch = (ch: number) => {
        const i = slotOf(ch)
        if (i < 0) return PAD.l
        return PAD.l + slotW * (i + 0.5)
      }
      const y = (dbm: number) => PAD.t + ((DBM_TOP - dbm) / (DBM_TOP - DBM_BOTTOM)) * plotH
      geomRef.current = { x: xch, y }
      const baseline = y(DBM_BOTTOM)
      g.clearRect(0, 0, w, h)
      g.font = CANVAS_FONT

      // Zonas DFS (solo 5 GHz): slots de los canales DFS sombreados + etiqueta.
      if (band === '5 GHz') {
        const dfsChs = bandChs.filter((c) => isDfsChannel(band, c))
        if (dfsChs.length > 0) {
          const i0 = slotOf(dfsChs[0]!)
          const i1 = slotOf(dfsChs[dfsChs.length - 1]!)
          g.fillStyle = 'rgba(167,139,250,0.07)'
          g.fillRect(PAD.l + slotW * i0, PAD.t, slotW * (i1 - i0 + 1), plotH)
          g.fillStyle = colors.tunnel
          g.globalAlpha = 0.75
          g.textAlign = 'left'
          g.fillText(tDfs, PAD.l + slotW * i0 + 7, PAD.t + 14)
          g.globalAlpha = 1
        }
      }

      // Zona recomendada (bloque del canal sugerido).
      const own = nets.find((n) => n.own)
      if (suggested > 0 && suggested !== (own?.channel ?? 0) && slotOf(suggested) >= 0) {
        const halfSlots = Math.max((widthMhz > 0 ? widthMhz : 20) / 20, 1) / 2
        const i = slotOf(suggested)
        g.fillStyle = 'rgba(52,211,153,0.08)'
        g.fillRect(PAD.l + slotW * (i - halfSlots + 0.5), PAD.t, slotW * halfSlots * 2, plotH)
        g.fillStyle = colors.ok
        g.textAlign = 'center'
        const lx = Math.min(Math.max(xch(suggested), PAD.l + 60), w - PAD.r - 60)
        g.fillText(`★ ${tSuggest} · ${suggested}`, lx, PAD.t - 8)
      }

      // Rejilla de señal.
      for (const d of [-25, -50, -75, -92]) {
        g.strokeStyle = colors.border
        g.lineWidth = 1
        g.beginPath()
        g.moveTo(PAD.l, y(d))
        g.lineTo(w - PAD.r, y(d))
        g.stroke()
        g.fillStyle = colors.muted
        g.textAlign = 'right'
        g.fillText(String(d), PAD.l - 8, y(d) + 3.5)
      }
      g.fillStyle = colors.muted
      g.textAlign = 'left'
      g.fillText(tDbm, 6, PAD.t - 8)

      // Eje de canales (slots uniformes).
      const known = new Set<number>(bandChs)
      for (const n of nets) known.add(n.channel)
      g.textAlign = 'center'
      for (const c of [...known].sort((a, b) => a - b)) {
        const i = slotOf(c)
        if (i < 0) continue
        g.strokeStyle = colors.border
        g.globalAlpha = 0.5
        g.beginPath()
        g.moveTo(PAD.l + slotW * i, PAD.t)
        g.lineTo(PAD.l + slotW * i, PAD.t + plotH)
        g.stroke()
        g.globalAlpha = 1
        g.fillStyle = colors.muted
        g.fillText(String(c), PAD.l + slotW * (i + 0.5), h - 10)
      }

      // Redes como campanas de techo plano (#1214): techo plano a su señal
      // con subida y caída curvas (pelín de curva), huella visual acotada
      // por slot. Las gaussianas 1.35x se fundían en una sopa y los
      // trapecios afilados quedaban rígidos.
      const visible = nets.filter((n) => !hidden.has(n.key))
      for (const n of visible) {
        if (n.channel <= 0 || slotOf(n.channel) < 0) continue
        const xc = xch(n.channel)
        // las propias se dibujan a 20 MHz de ancho visual (su bloque de
        // canal): el ancho real (40/80) queda en hover y tabla, pero
        // dibujarlo entero convierte la vista densa en una pared (#1214)
        const drawW = n.own ? Math.min(n.widthMhz, 20) : n.widthMhz
        const topHalf = (0.75 * slotW * Math.max(drawW, 20)) / 20
        const sigma = slotW
        const reach = topHalf + 1.75 * slotW
        const peak = y(Math.max(n.signal, DBM_TOP))
        g.beginPath()
        for (let d = -reach; d <= reach; d += 1) {
          const fall = Math.max(Math.abs(d) - topHalf, 0)
          const amp = Math.exp(-(fall * fall) / (sigma * sigma))
          const yy = baseline - (baseline - peak) * amp
          if (d <= -reach) g.moveTo(xc + d, yy)
          else g.lineTo(xc + d, yy)
        }
        g.closePath()
        const grad = g.createLinearGradient(0, peak, 0, baseline)
        grad.addColorStop(0, withAlpha(n.color, n.own ? 0.26 : 0.18))
        grad.addColorStop(1, withAlpha(n.color, 0.03))
        g.fillStyle = grad
        g.strokeStyle = n.color
        g.lineWidth = n.own ? 2.2 : 1.5
        const dimmed = (selected && selected !== n.key) || (focus != null && n.channel !== focus)
        g.globalAlpha = dimmed ? 0.14 : 1
        g.fill()
        g.stroke()
        g.globalAlpha = 1
      }

      // Etiquetas: dos filas sobre el pico, anti-solapado por slot.
      {
        const labelables = visible
          .filter((n) => n.channel > 0 && (n.own || n.signal > -80))
          .sort((a, b) => Number(b.own ?? false) - Number(a.own ?? false) || b.signal - a.signal)
        const rows: { px: number; py: number }[][] = [[], []]
        for (const n of labelables) {
          const px = Math.min(Math.max(xch(n.channel), PAD.l + 42), w - PAD.r - 42)
          const peakY = y(Math.max(n.signal, DBM_TOP))
          for (const rowIdx of [0, 1]) {
            const py = peakY - 8 - rowIdx * 13
            const clash = rows[rowIdx]!.some((q) => Math.abs(q.px - px) < slotW * 1.6)
            if (!clash || (n.own && rowIdx === 0 && !rows[0]!.some((q) => Math.abs(q.px - px) < slotW * 1.2))) {
              g.fillStyle = n.color
              g.textAlign = 'center'
              g.font = n.own ? CANVAS_FONT_BOLD : '10px Inter, ui-sans-serif, system-ui, sans-serif'
              g.globalAlpha = selected && selected !== n.key ? 0.3 : 1
              g.fillText(n.ssid, px, py)
              g.globalAlpha = 1
              g.font = CANVAS_FONT
              rows[rowIdx]!.push({ px, py })
              break
            }
          }
        }
      }

      // Marcador del canal actual (red propia del radio).
      if (own && own.channel > 0 && slotOf(own.channel) >= 0) {
        const xc = xch(own.channel)
        g.setLineDash([4, 4])
        g.strokeStyle = colors.accent
        g.lineWidth = 1.4
        g.beginPath()
        g.moveTo(xc, PAD.t + 6)
        g.lineTo(xc, PAD.t + plotH)
        g.stroke()
        g.setLineDash([])
        g.fillStyle = colors.accent
        g.textAlign = 'left'
        g.font = CANVAS_FONT_SEMI
        g.fillText(tActual, xc + 7, PAD.t + plotH - 8)
        g.font = CANVAS_FONT
      }

      // Franja de calor por canal: una barra por slot, ocupación = potencia
      // de los vecinos (las propias no congestian, #1080).
      const { g: hg, w: hw, h: hh } = setup(heat, 22)
      if (hg) {
        const stops: [number, string][] = [
          [0, '#3a4757'],
          [0.35, '#2dd4bf'],
          [0.62, '#f5c26b'],
          [1, '#ef6b5b'],
        ]
        const barW = hw / Math.max(bandChs.length, 1)
        for (let i = 0; i < bandChs.length; i++) {
          const ch = bandChs[i]!
          let sum = 0
          for (const n of visible) {
            if (n.own || n.channel !== ch) continue
            sum += Math.pow(10, n.signal / 10)
          }
          const db = sum > 0 ? 10 * Math.log10(sum) : DBM_BOTTOM
          const tt = Math.min(Math.max((db + 92) / 48, 0), 1)
          let c1 = stops[0]!
          let c2 = stops[stops.length - 1]!
          let tt2 = 0
          for (let k = 0; k < stops.length - 1; k++) {
            if (tt >= stops[k]![0] && tt <= stops[k + 1]![0]) {
              c1 = stops[k]!
              c2 = stops[k + 1]!
              tt2 = (tt - c1[0]) / (c2[0] - c1[0])
              break
            }
          }
          hg.fillStyle = mixHex(c1[1], c2[1], tt2)
          hg.fillRect(i * barW + 1, 0, barW - 2, hh)
        }
      }
    }

    draw()
    const ro = new ResizeObserver(draw)
    ro.observe(cv)
    const mo = new MutationObserver(draw)
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => {
      ro.disconnect()
      mo.disconnect()
    }
  }, [band, nets, suggested, widthMhz, hidden, selected, focus, tSuggest, tDfs, tActual, tDbm])

  const onMove = (e: React.MouseEvent<HTMLCanvasElement>) => {
    const geom = geomRef.current
    const cv = canvasRef.current
    if (!geom || !cv) return
    const r = cv.getBoundingClientRect()
    const mx = e.clientX - r.left
    const bandChs = BAND_CHANNELS[band] ?? []
    if (bandChs.length === 0) return
    const plotW = r.width - PAD.l - PAD.r
    const slotW = plotW / bandChs.length
    const slotIdx = Math.min(Math.max(Math.floor((mx - PAD.l) / slotW), 0), bandChs.length - 1)
    const ch = bandChs[slotIdx]!
    let best: SpectrumNet | null = null
    let bestD = Infinity
    for (const n of nets) {
      if (hidden.has(n.key)) continue
      const d = Math.abs(n.channel - ch)
      if (d <= 1 && d < bestD) {
        bestD = d
        best = n
      }
    }
    onHover(best, geom.x(best?.channel ?? ch), geom.y(best?.signal ?? DBM_BOTTOM))
  }

  return (
    <div>
      <canvas
        ref={canvasRef}
        className="block w-full cursor-crosshair"
        onMouseMove={onMove}
        onMouseLeave={() => onHover(null, 0, 0)}
      />
      <canvas ref={heatRef} className="mt-3 block w-full rounded-md" />
    </div>
  )
}

function mixHex(a: string, b: string, t: number): string {
  const pa = [1, 3, 5].map((i) => parseInt(a.substring(i, i + 2), 16))
  const pb = [1, 3, 5].map((i) => parseInt(b.substring(i, i + 2), 16))
  return `#${pa.map((v, i) => Math.round(v + (pb[i]! - v) * t).toString(16).padStart(2, '0')).join('')}`
}
