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
  currentChannel,
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
  /** Canal del radio activo: marca "Tu canal" y excluye la zona sugerida. */
  currentChannel: number
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
  const geomRef = useRef<{ x: (ch: number) => number; y: (d: number) => number; topDbm: number; botDbm: number } | null>(null)
  // Red bajo el cursor (#1214): sombreada al pasar por encima; draw se
  // invoca a mano cuando cambia para que el brillo siga al raton.
  const hoverKeyRef = useRef<string | null>(null)
  const drawRef = useRef<(() => void) | null>(null)

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

      const { g, w, h } = setup(cv, 400)
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
      // #1214: eje Y ADAPTATIVO. Con la escala fija -25..-95 y un entorno
      // real de -50..-95, media gráfica era espacio muerto y las débiles
      // quedaban aplastadas en el suelo. La escala ahora se ajusta a lo que
      // hay en la vista (techo = señal más fuerte +5 dB, sin pasar de -25;
      // suelo = la más débil -4 dB, sin bajar de -95) con un recorrido
      // mínimo de 30 dB para no exagerar cuando casi todo está junto.
      const signals = nets.filter((n) => n.channel > 0 && n.signal < 0).map((n) => n.signal)
      let topDbm = DBM_TOP
      let botDbm = DBM_BOTTOM
      if (signals.length > 0) {
        topDbm = Math.min(DBM_TOP, Math.max(...signals) + 5)
        // El suelo se estira hasta la señal más débil real (hasta -105):
        // con el piso clavado en -95, una red a -97 se aplastaba contra la
        // línea base y se veia como una linea recta (Hobbiton, #1214).
        botDbm = Math.max(-105, Math.min(...signals) - 4)
        if (topDbm - botDbm < 30) botDbm = Math.max(-105, topDbm - 30)
      }
      const y = (dbm: number) => PAD.t + ((topDbm - dbm) / (topDbm - botDbm)) * plotH
      geomRef.current = { x: xch, y, topDbm, botDbm }
      const baseline = y(botDbm)
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

      // Zona recomendada (bloque del canal sugerido): comparar contra el
      // canal REAL del radio, no contra una red propia cualquiera (#1214).
      if (suggested > 0 && suggested !== currentChannel && slotOf(suggested) >= 0) {
        const halfSlots = Math.max((widthMhz > 0 ? widthMhz : 20) / 20, 1) / 2
        const i = slotOf(suggested)
        g.fillStyle = 'rgba(52,211,153,0.08)'
        g.fillRect(PAD.l + slotW * (i - halfSlots + 0.5), PAD.t, slotW * halfSlots * 2, plotH)
        g.fillStyle = colors.ok
        g.textAlign = 'center'
        const lx = Math.min(Math.max(xch(suggested), PAD.l + 60), w - PAD.r - 60)
        g.fillText(`★ ${tSuggest} · ${suggested}`, lx, PAD.t - 8)
      }

      // Rejilla de señal: múltiplos de 10 dentro del rango real de la vista.
      for (let d = Math.floor(topDbm / 10) * 10; d >= botDbm; d -= 10) {
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
        // #1214, el diseño final pedido ("los valores de OpenWrt con un
        // pelín de curva"): la geometría del análisis de canales de LuCI -
        // techo plano que cubre el rango del canal (0.5 slot por lado a
        // 20 MHz, escala con el ancho anunciado) y faldones de ~1.5 slots -
        // pero con subida y caída curvas en vez de rectas. TODAS las redes
        // a su altura real: las propias fuertes (-20/-54, la malla cercana)
        // son capuchas en el techo como el "Local Interface" de LuCI, y las
        // débiles (-80..-100) bultos visibles gracias al fondo en -100.
        const topHalf = (0.5 * slotW * Math.max(n.widthMhz, 20)) / 20
        const sigma = 0.75 * slotW
        const reach = topHalf + 1.5 * slotW
        // #1214: clamp a AMBOS lados. Sin el inferior, una red < -100
        // dBm ponía el pico bajo la línea base y la campana salía
        // INVERTIDA (borde en la base, centro colgando del canvas).
        const sig = Math.max(botDbm, Math.min(n.signal, topDbm))
        const peak = y(sig)
        g.beginPath()
        for (let d = -reach; d <= reach; d += 1) {
          const fall = Math.max(Math.abs(d) - topHalf, 0)
          const amp = Math.exp(-(fall * fall) / (sigma * sigma))
          const yy = baseline - (baseline - peak) * amp
          if (d <= -reach) g.moveTo(xc + d, yy)
          else g.lineTo(xc + d, yy)
        }
        g.closePath()
        const dimmed = (selected && selected !== n.key) || (focus != null && n.channel !== focus)
        const hovered = hoverKeyRef.current === n.key
        const grad = g.createLinearGradient(0, peak, 0, baseline)
        grad.addColorStop(0, withAlpha(n.color, hovered ? (n.own ? 0.4 : 0.32) : n.own ? 0.26 : 0.18))
        grad.addColorStop(1, withAlpha(n.color, hovered ? 0.08 : 0.03))
        g.fillStyle = grad
        g.strokeStyle = n.color
        g.lineWidth = hovered ? (n.own ? 2.8 : 2.1) : n.own ? 2.2 : 1.5
        g.globalAlpha = dimmed ? 0.14 : 1
        // Sombreado al pasar por encima (#1214): glow del color de la red.
        if (hovered && !dimmed) {
          g.shadowColor = n.color
          g.shadowBlur = 14
        }
        g.fill()
        g.stroke()
        g.shadowBlur = 0
        g.globalAlpha = 1
      }

      // Etiquetas: dos filas sobre el pico, anti-solapado por slot.
      {
        // #1214: propias siempre + las 6 vecinas más fuertes. Con todo el
        // entorno a -85..-97 las 13 etiquetas compartían altura, chocaban
        // y solo sobrevivía una; el resto vive en la tabla de abajo.
        const neighborsSorted = visible
          .filter((n) => n.channel > 0 && !n.own)
          .sort((a, b) => b.signal - a.signal)
          .slice(0, 6)
        const labelables = visible
          .filter((n) => n.channel > 0 && (n.own || neighborsSorted.includes(n)))
          .sort((a, b) => Number(b.own ?? false) - Number(a.own ?? false) || b.signal - a.signal)
        const rows: { px: number; py: number }[][] = [[], []]
        for (const n of labelables) {
          const px = Math.min(Math.max(xch(n.channel), PAD.l + 42), w - PAD.r - 42)
          const peakY = y(Math.max(botDbm, Math.min(n.signal, topDbm)))
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
        // #1214: banda del suelo para el resto de redes visibles. Las
        // débiles (-80..-100) no tenían etiqueta (el top-6 se las comía) y
        // el usuario no las veía frente a "Redes detectadas" ni a LuCI,
        // que etiqueta también las bajas. Tres filas escalonadas por
        // paridad de slot sobre la línea base; si aun así chocan, se caen
        // (la tabla de abajo siempre las lista).
        const labeled = new Set(labelables.map((n) => n.key))
        const floorRows: { px: number }[][] = [[], [], []]
        for (const n of visible) {
          if (n.channel <= 0 || labeled.has(n.key) || hidden.has(n.key)) continue
          const slot = slotOf(n.channel)
          if (slot < 0) continue
          const px = Math.min(Math.max(xch(n.channel), PAD.l + 36), w - PAD.r - 36)
          const rowIdx = slot % 3
          if (floorRows[rowIdx]!.some((q) => Math.abs(q.px - px) < slotW * 0.9)) continue
          const py = baseline - 8 - rowIdx * 11
          g.fillStyle = n.color
          g.textAlign = 'center'
          g.font = '9px Inter, ui-sans-serif, system-ui, sans-serif'
          g.globalAlpha = selected && selected !== n.key ? 0.25 : 0.8
          g.fillText(n.ssid, px, py)
          g.globalAlpha = 1
          g.font = CANVAS_FONT
          floorRows[rowIdx]!.push({ px })
        }
      }

      // Marcador del canal actual: el canal del radio (#1214: antes se
      // usaba la primera red propia, que podía ser un BSSID de la malla en
      // OTRO canal).
      if (currentChannel > 0 && slotOf(currentChannel) >= 0) {
        const xc = xch(currentChannel)
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
        // #1214: dos arreglos para entornos limpios. (1) El solapamiento es
        // físico: una red de 20 MHz en el 6 tapa del 4 al 8, así que su
        // potencia se reparte por todos los slots que cubre su ancho (no
        // solo el canal sintonizado - antes la franja estaba a rayas).
        // (2) Escala RELATIVA a la vista: la absoluta arrancaba en -92 dBm
        // y con vecinos a -85..-97 todo quedaba gris ("todos los canales
        // en gris"). La congestión absoluta ya la dice el chip del panel.
        const sums: number[] = bandChs.map(() => 0)
        for (const n of visible) {
          if (n.own) continue
          const i = slotOf(n.channel)
          if (i < 0) continue
          const half = Math.max(1, Math.round((n.widthMhz > 0 ? n.widthMhz : 20) / 10))
          const p = Math.pow(10, n.signal / 10)
          for (let k = Math.max(0, i - half); k <= Math.min(bandChs.length - 1, i + half); k++) {
            sums[k]! += p
          }
        }
        const dbs = sums.map((p) => (p > 0 ? 10 * Math.log10(p) : -Infinity))
        const withData = dbs.filter((d) => d > -Infinity)
        let loDb = withData.length > 0 ? Math.min(...withData) : DBM_BOTTOM
        const hiDbRaw = withData.length > 0 ? Math.max(...withData) : DBM_BOTTOM
        if (hiDbRaw - loDb < 12) loDb = hiDbRaw - 12
        // Rango tonal COMPLETO (pizarra -> teal -> ambar -> rojo): el
        // tramo corto 0.12..0.62 dejaba la franja sin matices.
        const norm = (d: number) => 0.04 + 0.96 * ((d - loDb) / (hiDbRaw - loDb))
        for (let i = 0; i < bandChs.length; i++) {
          const tt = dbs[i]! === -Infinity ? 0 : Math.min(Math.max(norm(dbs[i]!), 0), 1)
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

    drawRef.current = draw
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
    // #1214: hit-test con la Y del cursor. Las campanas se anidan (una red
    // debil dentro de una grande en el mismo canal): el cursor cerca del
    // suelo esta DENTRO de la pequena y debe elegir a ella, no a la
    // dominante. Regla de graficos por capas: gana la campana mas interior
    // (curva mas baja) que contenga el punto; por encima de todas, la mas
    // fuerte a igual distancia de canal.
    const baseline = geom.y(geom.botDbm)
    const my = Math.min(e.clientY - r.top, baseline)
    let best: SpectrumNet | null = null
    let bestV = -Infinity
    let fb: SpectrumNet | null = null
    let fbD = Infinity
    for (const n of nets) {
      if (hidden.has(n.key)) continue
      const i = bandChs.indexOf(n.channel)
      if (i < 0) continue
      const d = Math.abs(n.channel - ch)
      if (d > 1) continue
      const xc = PAD.l + slotW * (i + 0.5)
      const topHalf = (0.5 * slotW * Math.max(n.widthMhz, 20)) / 20
      const sigma = 0.75 * slotW
      const fall = Math.max(Math.abs(mx - xc) - topHalf, 0)
      const amp = Math.exp(-(fall * fall) / (sigma * sigma))
      const peak = geom.y(Math.max(geom.botDbm, Math.min(n.signal, geom.topDbm)))
      const v = baseline - (baseline - peak) * amp
      if (v <= my) {
        // El punto cae dentro del cuerpo de esta campana: gana la mas
        // interior (curva mas baja) de las que lo contienen.
        if (v > bestV) {
          bestV = v
          best = n
        }
      } else if (fb == null || d < fbD || (d === fbD && n.signal > fb.signal)) {
        // Fallback si el cursor esta por encima de todas: mas cercana, y a
        // igual distancia la mas fuerte.
        fbD = d
        fb = n
      }
    }
    if (best == null) best = fb
    onHover(best, geom.x(best?.channel ?? ch), geom.y(best?.signal ?? DBM_BOTTOM))
    const hk = best?.key ?? null
    if (hk !== hoverKeyRef.current) {
      hoverKeyRef.current = hk
      drawRef.current?.()
    }
  }

  return (
    <div>
      <canvas
        ref={canvasRef}
        className="block w-full cursor-crosshair"
        onMouseMove={onMove}
        onMouseLeave={() => {
          onHover(null, 0, 0)
          if (hoverKeyRef.current !== null) {
            hoverKeyRef.current = null
            drawRef.current?.()
          }
        }}
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
