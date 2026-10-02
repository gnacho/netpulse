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
  '2.4 GHz': [2401, 2495],
  '5 GHz': [5170, 5895],
  '6 GHz': [5955, 7115],
}

export const BAND_CHANNELS: Record<string, number[]> = {
  '2.4 GHz': Array.from({ length: 13 }, (_, i) => i + 1),
  '5 GHz': [36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116, 120, 124, 128, 132, 136, 140, 144, 149, 153, 157, 161, 165],
}

// DFS (ETSI) en MHz para el sombreado de zona: UNII-2A y UNII-2C.
const DFS_RANGES_MHZ: [number, number][] = [
  [5260, 5320],
  [5500, 5720],
]

const DBM_TOP = -20
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
  onHover: (net: SpectrumNet | null, x: number, y: number) => void
  tSuggest: string
  tDfs: string
  tActual: string
  tDbm: string
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const heatRef = useRef<HTMLCanvasElement>(null)
  const geomRef = useRef<{ x: (f: number) => number; y: (d: number) => number } | null>(null)

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
      const x = (mhz: number) => PAD.l + ((mhz - range[0]) / (range[1] - range[0])) * plotW
      const y = (dbm: number) => PAD.t + ((DBM_TOP - dbm) / (DBM_TOP - DBM_BOTTOM)) * plotH
      geomRef.current = { x, y }
      const baseline = y(DBM_BOTTOM)
      g.clearRect(0, 0, w, h)
      g.font = CANVAS_FONT

      // Zonas DFS (solo 5 GHz), tinte del token tunnel.
      if (band === '5 GHz') {
        for (const [f0, f1] of DFS_RANGES_MHZ) {
          g.fillStyle = 'rgba(167,139,250,0.07)'
          g.fillRect(x(f0), PAD.t, x(f1) - x(f0), plotH)
          g.fillStyle = colors.tunnel
          g.globalAlpha = 0.75
          g.textAlign = 'left'
          g.fillText(tDfs, x(f0) + 7, PAD.t + 14)
          g.globalAlpha = 1
        }
      }

      // Zona recomendada (bloque del canal sugerido).
      const sugFreq = suggested > 0 ? channelFreq(band, suggested) : 0
      const own = nets.find((n) => n.own)
      if (sugFreq > 0 && suggested !== (own?.channel ?? 0)) {
        const half = (widthMhz > 0 ? widthMhz : 20) / 2
        g.fillStyle = 'rgba(52,211,153,0.08)'
        g.fillRect(x(sugFreq - half), PAD.t, x(sugFreq + half) - x(sugFreq - half), plotH)
        g.fillStyle = colors.ok
        g.textAlign = 'center'
        const lx = Math.min(Math.max(x(sugFreq), PAD.l + 60), w - PAD.r - 60)
        g.fillText(`★ ${tSuggest} · ${suggested}`, lx, PAD.t - 8)
      }

      // Rejilla de señal.
      for (const d of [-30, -50, -70, -90]) {
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

      // Eje de canales (frecuencias reales).
      const known = new Set<number>(BAND_CHANNELS[band] ?? [])
      for (const n of nets) known.add(n.channel)
      g.textAlign = 'center'
      for (const c of [...known].sort((a, b) => a - b)) {
        const f = channelFreq(band, c)
        if (f <= 0) continue
        g.strokeStyle = colors.border
        g.globalAlpha = 0.5
        g.beginPath()
        g.moveTo(x(f), PAD.t)
        g.lineTo(x(f), PAD.t + plotH)
        g.stroke()
        g.globalAlpha = 1
        g.fillStyle = colors.muted
        g.fillText(String(c), x(f), h - 10)
      }

      // Campanas.
      const visible = nets.filter((n) => !hidden.has(n.key))
      const placed: { px: number; py: number }[] = []
      for (const n of visible) {
        if (n.freq <= 0) continue
        const sigma = Math.max(n.widthMhz * 0.62, 8)
        const span = n.widthMhz * 1.35
        const f0 = Math.max(n.freq - span, range[0])
        const f1 = Math.min(n.freq + span, range[1])
        const peak = y(Math.max(n.signal, DBM_TOP))
        g.beginPath()
        g.moveTo(x(f0), baseline)
        for (let f = f0; f <= f1; f += 1) {
          const amp = Math.exp(-Math.pow((f - n.freq) / sigma, 2))
          g.lineTo(x(f), baseline - (baseline - peak) * amp)
        }
        g.lineTo(x(f1), baseline)
        g.closePath()
        const grad = g.createLinearGradient(0, peak, 0, baseline)
        grad.addColorStop(0, withAlpha(n.color, 0.3))
        grad.addColorStop(1, withAlpha(n.color, 0.04))
        g.fillStyle = grad
        g.fill()
        g.strokeStyle = n.color
        g.lineWidth = n.own ? 2.2 : 1.5
        g.globalAlpha = selected && selected !== n.key ? 0.22 : 1
        g.stroke()
        g.globalAlpha = 1

        if (n.own || n.signal > -75) {
          const px = Math.min(Math.max(x(n.freq), PAD.l + 42), w - PAD.r - 42)
          const py = peak - 8
          const clash = placed.some((p) => Math.abs(p.px - px) < 95 && Math.abs(p.py - py) < 15)
          if (!clash || n.own) {
            g.fillStyle = n.color
            g.textAlign = 'center'
            g.font = n.own ? CANVAS_FONT_BOLD : CANVAS_FONT_SEMI
            g.fillText(n.ssid, px, py)
            g.font = CANVAS_FONT
            placed.push({ px, py })
          }
        }
      }

      // Marcador del canal actual (red propia del radio).
      if (own && own.freq > 0) {
        g.setLineDash([4, 4])
        g.strokeStyle = colors.accent
        g.lineWidth = 1.4
        g.beginPath()
        g.moveTo(x(own.freq), PAD.t + 6)
        g.lineTo(x(own.freq), PAD.t + plotH)
        g.stroke()
        g.setLineDash([])
        g.fillStyle = colors.accent
        g.textAlign = 'left'
        g.font = CANVAS_FONT_SEMI
        g.fillText(tActual, x(own.freq) + 7, PAD.t + plotH - 8)
        g.font = CANVAS_FONT
      }

      // Franja de calor: ocupación continua por MHz (ponderación lineal,
      // mismas convenciones que el mockup del informe).
      const { g: hg, w: hw, h: hh } = setup(heat, 22)
      if (hg) {
        const lin = (dbm: number) => Math.pow(10, dbm / 10)
        const overlap = (fc: number, wMhz: number, f: number) => {
          const sigma = (wMhz + 20) / 6
          return Math.exp(-Math.pow((f - fc) / sigma, 2))
        }
        const stops: [number, string][] = [
          [0, '#3a4757'],
          [0.35, '#2dd4bf'],
          [0.62, '#f5c26b'],
          [1, '#ef6b5b'],
        ]
        for (let px = 0; px < hw; px++) {
          const f = range[0] + (px / hw) * (range[1] - range[0])
          let sum = 0
          for (const n of visible) {
            sum += lin(n.signal) * overlap(n.freq, n.widthMhz, f) * (n.own ? 0.6 : 1)
          }
          const db = sum > 0 ? 10 * Math.log10(sum) : DBM_BOTTOM
          const tt = Math.min(Math.max((db + 92) / 48, 0), 1)
          let c1 = stops[0]!
          let c2 = stops[stops.length - 1]!
          let tt2 = 0
          for (let i = 0; i < stops.length - 1; i++) {
            if (tt >= stops[i]![0] && tt <= stops[i + 1]![0]) {
              c1 = stops[i]!
              c2 = stops[i + 1]!
              tt2 = (tt - c1[0]) / (c2[0] - c1[0])
              break
            }
          }
          hg.fillStyle = mixHex(c1[1], c2[1], tt2)
          hg.fillRect(px, 0, 1.5, hh)
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
  }, [band, nets, suggested, widthMhz, hidden, selected, tSuggest, tDfs, tActual, tDbm])

  const onMove = (e: React.MouseEvent<HTMLCanvasElement>) => {
    const geom = geomRef.current
    const cv = canvasRef.current
    if (!geom || !cv) return
    const r = cv.getBoundingClientRect()
    const mx = e.clientX - r.left
    const range = BAND_RANGE[band]
    if (!range) return
    const plotW = r.width - PAD.l - PAD.r
    const f = range[0] + ((mx - PAD.l) / plotW) * (range[1] - range[0])
    let best: SpectrumNet | null = null
    let bestD = Infinity
    for (const n of nets) {
      if (hidden.has(n.key)) continue
      const d = Math.abs(n.freq - f)
      if (d < n.widthMhz * 1.2 && d < bestD) {
        bestD = d
        best = n
      }
    }
    onHover(best, geom.x(best?.freq ?? f), geom.y(best?.signal ?? DBM_BOTTOM))
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
