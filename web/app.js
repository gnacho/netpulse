/* ============================================================
   NetPulse landing — app.js v2
   Fondo canvas, tipografía grande, topología fiel a la app.
   ============================================================ */

const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches

const COLOR = {
  accent: '#22D3EE',
  tunnel: '#A78BFA',
  ok: '#34D399',
  warn: '#FBBF24',
  danger: '#F87171',
  info: '#60A5FA',
}

function getCssVar(name) {
  const s = getComputedStyle(document.documentElement).getPropertyValue(`--${name}`).trim().split(' ')
  return `rgb(${s.join(' ')})`
}

/* ---------- helpers ---------- */
function easeOutCubic(x) { return 1 - Math.pow(1 - x, 3) }
function easeOutExpo(x) { return x === 1 ? 1 : 1 - Math.pow(2, -10 * x) }

function animateNumber(el, target, { decimals = 0, suffix = '', duration = 1200, format } = {}) {
  if (reduceMotion) { el.textContent = format ? format(target) : target.toFixed(decimals) + suffix; return }
  const start = performance.now()
  function frame(now) {
    const p = Math.min(1, (now - start) / duration)
    const v = target * easeOutCubic(p)
    el.textContent = format ? format(v) : v.toFixed(decimals) + suffix
    if (p < 1) requestAnimationFrame(frame)
  }
  requestAnimationFrame(frame)
}

function fmtLocale(n, decimals = 0) {
  return n.toLocaleString(CURRENT === 'en' ? 'en-US' : CURRENT, {
    minimumFractionDigits: decimals, maximumFractionDigits: decimals,
  })
}

function mkSvgEl(tag, attrs = {}, parent) {
  const e = document.createElementNS('http://www.w3.org/2000/svg', tag)
  Object.entries(attrs).forEach(([k, v]) => e.setAttribute(k, v))
  if (parent) parent.appendChild(e)
  return e
}

/* ---------- fondo canvas dinámico (red de partículas) ---------- */
function initBackgroundCanvas() {
  const canvas = document.getElementById('bgCanvas')
  if (!canvas || reduceMotion) return
  const ctx = canvas.getContext('2d')
  let w, h, particles = [], mx = 0, my = 0, targetMx = 0, targetMy = 0

  function resize() {
    w = canvas.width = window.innerWidth
    h = canvas.height = window.innerHeight
    const count = Math.min(120, Math.max(40, Math.floor((w * h) / 18000)))
    particles = Array.from({ length: count }, () => ({
      x: Math.random() * w, y: Math.random() * h,
      vx: (Math.random() - 0.5) * 0.25, vy: (Math.random() - 0.5) * 0.25,
      r: 0.5 + Math.random() * 1.5, pulse: Math.random() * Math.PI * 2,
    }))
  }
  resize()
  window.addEventListener('resize', resize)
  document.addEventListener('mousemove', e => { targetMx = e.clientX; targetMy = e.clientY }, { passive: true })
  document.addEventListener('touchmove', e => { if (e.touches[0]) { targetMx = e.touches[0].clientX; targetMy = e.touches[0].clientY } }, { passive: true })

  function draw(t) {
    mx += (targetMx - mx) * 0.05
    my += (targetMy - my) * 0.05
    ctx.clearRect(0, 0, w, h)
    const accent = getCssVar('accent')
    const tunnel = getCssVar('tunnel')
    const ok = getCssVar('ok')

    for (let i = 0; i < particles.length; i++) {
      const p = particles[i]
      p.x += p.vx + (mx - w / 2) * 0.00004
      p.y += p.vy + (my - h / 2) * 0.00004
      if (p.x < -20) p.x = w + 20; if (p.x > w + 20) p.x = -20
      if (p.y < -20) p.y = h + 20; if (p.y > h + 20) p.y = -20
      const pulse = 0.5 + 0.5 * Math.sin(t * 0.001 + p.pulse)
      ctx.beginPath()
      ctx.arc(p.x, p.y, p.r * pulse, 0, Math.PI * 2)
      ctx.fillStyle = i % 7 === 0 ? accent : i % 5 === 0 ? tunnel : ok
      ctx.globalAlpha = 0.25 + 0.25 * pulse
      ctx.fill()
    }

    ctx.globalAlpha = 1
    const connectDist = 130
    for (let i = 0; i < particles.length; i++) {
      let a = particles[i]
      for (let j = i + 1; j < particles.length; j++) {
        let b = particles[j]
        const dx = a.x - b.x, dy = a.y - b.y, d2 = dx * dx + dy * dy
        if (d2 < connectDist * connectDist) {
          const d = Math.sqrt(d2)
          ctx.beginPath()
          ctx.moveTo(a.x, a.y)
          ctx.lineTo(b.x, b.y)
          const col = (i + j) % 3 === 0 ? accent : (i + j) % 3 === 1 ? tunnel : ok
          ctx.strokeStyle = col
          ctx.globalAlpha = 0.08 * (1 - d / connectDist)
          ctx.lineWidth = 0.8
          ctx.stroke()
        }
      }
    }
    ctx.globalAlpha = 1
    requestAnimationFrame(draw)
  }
  requestAnimationFrame(draw)
}

/* ---------- count-up hero ---------- */
function heroCountUps() {
  document.querySelectorAll('#heroStrip [data-count]').forEach(el => {
    const target = parseFloat(el.dataset.count)
    const decimals = parseInt(el.dataset.decimals || '0', 10)
    const suffix = el.dataset.suffix || ''
    animateNumber(el, target, { decimals, suffix, duration: 1400 })
  })
}

/* ---------- ticker ---------- */
function renderTicker() {
  const el = document.getElementById('ticker')
  if (!el) return
  const items = [
    `<span><i class="dot" style="background:rgb(var(--ok))"></i><i>${t('strip.health')}</i>&nbsp;<i class="font-mono">92/100</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--accent))"></i><i>${t('strip.down')}</i>&nbsp;<i class="font-mono">84.2 Mbps</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--tunnel))"></i><i>${t('strip.up')}</i>&nbsp;<i class="font-mono">12.6 Mbps</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--info))"></i><i>${t('strip.latency')}</i>&nbsp;<i class="font-mono">8 ms</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--text-secondary))"></i><i>${t('strip.devices')}</i>&nbsp;<i class="font-mono">67</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--danger))"></i><i>AdGuard</i>&nbsp;<i class="font-mono">60/65 ${t('adguard.clients')}</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--warn))"></i><i>DNS</i>&nbsp;<i class="font-mono">14 ms</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--ok))"></i><i>WireGuard</i>&nbsp;<i class="font-mono">peer phone · ${t('alerts.ago')} 2 min</i></span>`,
  ]
  el.innerHTML = items.join('') + items.join('')
}

/* ---------- stage 0: health ring ---------- */
let ringPlayed = false
function playRing() {
  if (ringPlayed) return
  ringPlayed = true
  const arc = document.getElementById('ringArc')
  const num = document.getElementById('ringNum')
  const C = 590.6
  const score = DEMO.score
  if (reduceMotion) { arc.style.strokeDashoffset = C * (1 - score / 100); num.textContent = score; return }
  const dur = 1300
  const start = performance.now()
  function frame(now) {
    const p = Math.min(1, (now - start) / dur)
    const e = easeOutCubic(p)
    const v = score * e
    num.textContent = Math.round(v)
    arc.style.strokeDashoffset = C * (1 - v / 100)
    if (p < 1) requestAnimationFrame(frame)
  }
  requestAnimationFrame(frame)
}

/* ---------- stage 1: traffic chart ---------- */
let trafficPlayed = false
let trafficTimer = null
function genSeries(n, base, amp, seed = 1) {
  const out = []; let s = seed
  for (let i = 0; i < n; i++) {
    s = (s * 9301 + 49297) % 233280
    const noise = s / 233280
    out.push(Math.max(2, base + Math.sin(i / 4) * amp * 0.6 + noise * amp))
  }
  return out
}
function pathFrom(data, W, H, max) {
  const step = W / (data.length - 1)
  return data.map((v, i) => `${i === 0 ? 'M' : 'L'}${(i * step).toFixed(1)} ${(H - (v / max) * (H - 10) - 5).toFixed(1)}`).join(' ')
}
function playTraffic() {
  if (trafficPlayed) return
  trafficPlayed = true
  const W = 480, H = 160
  const downData = genSeries(52, DEMO.down, 22, 7)
  const upData = genSeries(52, DEMO.up, 6, 13)
  const max = 120
  const lineDown = document.getElementById('lineDown')
  const lineUp = document.getElementById('lineUp')
  const areaDown = document.getElementById('areaDown')
  const dPath = pathFrom(downData, W, H, max)
  const uPath = pathFrom(upData, W, H, max)
  lineDown.setAttribute('d', dPath)
  lineUp.setAttribute('d', uPath)
  areaDown.setAttribute('d', dPath + ` L${W} ${H} L0 ${H} Z`)
  if (reduceMotion) {
    areaDown.setAttribute('opacity', '1')
    document.getElementById('wanDown').textContent = DEMO.down.toFixed(1)
    document.getElementById('wanUp').textContent = DEMO.up.toFixed(1)
    document.getElementById('wanLat').textContent = DEMO.latency
    return
  }
  const Ld = lineDown.getTotalLength()
  const Lu = lineUp.getTotalLength()
  lineDown.style.strokeDasharray = Ld
  lineUp.style.strokeDasharray = Lu
  lineDown.style.strokeDashoffset = Ld
  lineUp.style.strokeDashoffset = Lu
  const start = performance.now()
  const dur = 1500
  function frame(now) {
    const p = Math.min(1, (now - start) / dur)
    const e = easeOutCubic(p)
    lineDown.style.strokeDashoffset = Ld * (1 - e)
    lineUp.style.strokeDashoffset = Lu * (1 - e)
    areaDown.setAttribute('opacity', String(e * 0.9))
    if (p < 1) requestAnimationFrame(frame)
    else startLiveJitter()
  }
  requestAnimationFrame(frame)
  animateNumber(document.getElementById('wanDown'), DEMO.down, { decimals: 1, duration: 1400 })
  animateNumber(document.getElementById('wanUp'), DEMO.up, { decimals: 1, duration: 1400 })
  animateNumber(document.getElementById('wanLat'), DEMO.latency, { duration: 1400 })
}
function startLiveJitter() {
  if (trafficTimer) return
  const downEl = document.getElementById('wanDown')
  const upEl = document.getElementById('wanUp')
  trafficTimer = setInterval(() => {
    if (!downEl.isConnected) return
    const d = Math.max(55, DEMO.down + (Math.random() - 0.5) * 7)
    const u = Math.max(9, DEMO.up + (Math.random() - 0.5) * 2.5)
    downEl.textContent = d.toFixed(1)
    upEl.textContent = u.toFixed(1)
  }, 2600)
}

/* ---------- stage 2: topología fiel a la app ---------- */
let topoBuilt = false
function buildTopo() {
  if (topoBuilt) return
  topoBuilt = true
  const svg = document.getElementById('topoSvg')
  const NS = 'http://www.w3.org/2000/svg'
  svg.setAttribute('viewBox', '0 0 1000 680')
  const canvas = getCssVar('canvas')

  const mk = (tag, attrs, parent) => {
    const e = document.createElementNS(NS, tag)
    Object.entries(attrs).forEach(([k, v]) => e.setAttribute(k, v))
    if (parent) parent.appendChild(e)
    return e
  }

  const order = []
  const svgAppend = (e) => { svg.appendChild(e); order.push(e) }

  // --- definición de iconos (paths simples) ---
  const iconPaths = {
    cloud: 'M17 19.2a4 4 0 0 1-3.2-6.4 4.1 4.1 0 0 1 2.7-7.3 4 4 0 0 1 6.6-1.6 4 4 0 0 1 3.6 6.5M17 19.2H6.5a4.5 4.5 0 0 1 0-9h.6a5 5 0 0 1 9.3-1.2',
    router: 'M4 14h16v5a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-5zm4-8h8M6 9h12M8 6h8',
    ap: 'M12 20v-6m-3-4a3 3 0 0 1 6 0m-9-4a6 6 0 0 1 12 0',
    laptop: 'M4 6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v8H4V6zm-2 10h20l-2 3H4l-2-3z',
    phone: 'M12 22h5a2 2 0 0 0 2-2V4a2 2 0 0 0-2-2H7a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h5m0-13h.01',
    tv: 'M4 6h16v11H4zm4 17h8m-4-6v6',
    switch: 'M4 7h16M4 12h16M4 17h16',
  }
  const drawIcon = (name, x, y, size, color, parent) => {
    const g = mk('g', { transform: `translate(${x - size / 2}, ${y - size / 2})`, fill: 'none', stroke: color, 'stroke-width': 1.75, 'stroke-linecap': 'round', 'stroke-linejoin': 'round' }, parent)
    mk('path', { d: iconPaths[name], transform: `scale(${size / 24})` }, g)
    return g
  }

  // --- nodos canónicos de la app ---
  const gw = { id: 'gw', name: 'GW-Flint2', model: 'Flint 2', x: 500, y: 250, r: 40, status: 'ok', health: 92, clients: 34 }
  const aps = [
    { id: 'ap1', name: 'AP-Salon', x: 195, y: 470, r: 32, status: 'ok', health: 88, clients: 18 },
    { id: 'ap2', name: 'AP-Pasillo', x: 500, y: 505, r: 32, status: 'warn', health: 71, clients: 10 },
    { id: 'ap3', name: 'AP-Dormitorio', x: 805, y: 470, r: 32, status: 'ok', health: 95, clients: 5 },
  ]
  const internet = { x: 500, y: 58 }
  const peers = [
    { id: 'p1', name: 'phone', type: 'phone', x: 265, y: 26 },
    { id: 'p2', name: 'nas-wg', type: 'laptop', x: 735, y: 26 },
  ]
  const dist = { id: 'dist1', name: 'Switch inferido', x: 660, y: 230, r: 24, managed: false, port: 'lan3', macs: 8 }
  const allRouters = [gw, ...aps]

  // --- anillos guía wifi ---
  const guideRadii = [96, 130]
  allRouters.forEach(r => {
    guideRadii.forEach(rad => {
      svgAppend(mk('circle', { cx: r.x, cy: r.y, r: rad, class: 'topo-guide' }))
    })
  })

  // --- links ---
  const addLink = (d, kind, color, width = 1.5, wifi = false, flow = false, label = '') => {
    const id = `link-${kind}-${Math.random().toString(36).slice(2, 8)}`
    const path = mk('path', { d, fill: 'none', stroke: color, 'stroke-width': width, 'stroke-linecap': 'round', class: 'topo-edge', pathLength: 1, id, 'stroke-dasharray': kind === 'wg' || (kind === 'uplink' && wifi) ? (kind === 'wg' ? '7 7' : '8 6') : undefined }, svg)
    order.push(path)
    if (flow && !reduceMotion) {
      for (let i = 0; i < 2; i++) {
        const dot = mk('circle', { r: 2.6, fill: color, class: 'topo-flow' }, svg)
        order.push(dot)
        const am = mk('animateMotion', { dur: `${2.2 + i * 0.6}s`, repeatCount: 'indefinite', begin: `${-i * 1.1}s` }, dot)
        mk('mpath', { href: `#${id}` }, am)
      }
    }
    if (kind === 'wg' && !reduceMotion) {
      const dashed = mk('path', { d, fill: 'none', stroke: color, 'stroke-width': width, 'stroke-linecap': 'round', 'stroke-dasharray': '7 7' }, svg)
      order.push(dashed)
      mk('animate', { attributeName: 'stroke-dashoffset', from: '0', to: '-28', dur: '1.4s', repeatCount: 'indefinite' }, dashed)
    }
    if (label) {
      const mid = path.getPointAtLength ? path.getPointAtLength(path.getTotalLength() / 2) : { x: 500, y: 300 }
      const t = mk('text', { x: mid.x, y: mid.y - 8, 'text-anchor': 'middle', class: 'topo-label-sub', fill: 'rgb(var(--text-muted))', 'font-size': 10 }, svg)
      t.textContent = label
      order.push(t)
    }
    return path
  }

  // WAN internet-gateway
  addLink(`M${internet.x} ${internet.y + 28} C${internet.x} 160, ${gw.x} 160, ${gw.x} ${gw.y - gw.r - 6}`, 'wan', COLOR.ok, 2.5, false, true)
  // uplinks gateway-APs (cableados = verde)
  const uplinkPaths = [
    `M${gw.x - gw.r * 0.7} ${gw.y + gw.r * 0.7} C390 340, 290 400, ${aps[0].x + aps[0].r * 0.6} ${aps[0].y - aps[0].r * 0.6}`,
    `M${gw.x} ${gw.y + gw.r} C${gw.x} 360, ${gw.x} 420, ${aps[1].y - aps[1].r}`,
    `M${gw.x + gw.r * 0.7} ${gw.y + gw.r * 0.7} C610 340, 710 400, ${aps[2].x - aps[2].r * 0.6} ${aps[2].y - aps[2].r * 0.6}`,
  ]
  uplinkPaths.forEach((d, i) => addLink(d, 'uplink', COLOR.ok, 2, false, true, i === 1 ? 'uplink' : ''))
  // distnode link
  addLink(`M${gw.x + gw.r + 4} ${gw.y} L${dist.x - dist.r - 4} ${dist.y}`, 'wired', COLOR.ok, 1.5, false, true)

  // --- internet ---
  const inetG = mk('g', { transform: `translate(${internet.x} ${internet.y})`, class: 'topo-node' }, svg)
  order.push(inetG)
  mk('circle', { r: 42, fill: COLOR.ok, opacity: 0.08 }, inetG)
  mk('circle', { r: 38, fill: 'none', stroke: COLOR.ok, 'stroke-width': 1.2, 'stroke-dasharray': '3 7', opacity: 0.55 }, inetG)
  mk('circle', { r: 3, fill: COLOR.ok, cx: 0, cy: -38 }, inetG)
  mk('animateTransform', { attributeName: 'transform', type: 'rotate', from: '0', to: '360', dur: '24s', repeatCount: 'indefinite' }, inetG)
  mk('circle', { r: 28, fill: canvas, stroke: COLOR.ok, 'stroke-width': 2 }, inetG)
  drawIcon('cloud', 0, 0, 22, COLOR.ok, inetG)
  const inetLabel = mk('text', { x: 36, y: 6, class: 'topo-label', fill: 'rgb(var(--text-primary))' }, svg)
  inetLabel.textContent = 'Internet'
  order.push(inetLabel)

  // --- routers ---
  allRouters.forEach((r, i) => {
    const g = mk('g', { transform: `translate(${r.x} ${r.y})`, class: 'topo-node' }, svg)
    order.push(g)
    const isGw = i === 0
    const isWarn = r.status === 'warn'
    if (isGw) mk('circle', { r: r.r + 18, fill: COLOR.accent, opacity: 0.12 }, g)
    if (isWarn) {
      const pulse = mk('circle', { r: r.r + 8, fill: 'none', stroke: COLOR.warn, 'stroke-width': 2 }, g)
      mk('animate', { attributeName: 'r', values: `${r.r + 8};${r.r + 22}`, dur: '1.6s', repeatCount: 'indefinite' }, pulse)
      mk('animate', { attributeName: 'opacity', values: '0.6;0', dur: '1.6s', repeatCount: 'indefinite' }, pulse)
    }
    const gradId = 'topo-gw-grad' + (i === 0 ? '-gw' : '')
    if (isGw) {
      const defs = mk('defs', {}, svg)
      const grad = mk('linearGradient', { id: gradId, x1: 0, y1: 0, x2: 1, y2: 1 }, defs)
      mk('stop', { offset: '0%', 'stop-color': COLOR.accent, 'stop-opacity': 0.28 }, grad)
      mk('stop', { offset: '100%', 'stop-color': COLOR.tunnel, 'stop-opacity': 0.28 }, grad)
    }
    mk('circle', { r: r.r, fill: isGw ? `url(#${gradId})` : canvas, stroke: isWarn ? COLOR.warn : isGw ? COLOR.accent : 'rgb(var(--border-strong))', 'stroke-width': isGw || isWarn ? 2 : 1.5 }, g)
    // status ring
    const R = r.r + 6, C = 2 * Math.PI * R
    const target = C * (1 - r.health / 100)
    const ring = mk('circle', { r: R, fill: 'none', stroke: isWarn ? COLOR.warn : COLOR.ok, 'stroke-width': 3, 'stroke-linecap': 'round', 'stroke-dasharray': C, 'stroke-dashoffset': C, transform: 'rotate(-90)' }, g)
    ring.style.transition = 'stroke-dashoffset 900ms ease'
    setTimeout(() => ring.style.strokeDashoffset = target, 100 + i * 120)
    drawIcon(isGw ? 'router' : 'ap', 0, 0, isGw ? 34 : 28, isWarn ? COLOR.warn : isGw ? COLOR.accent : 'rgb(var(--text-primary))', g)

    const label = mk('text', { x: r.x - (isGw ? 54 : 60), y: r.y + (isGw ? 62 : 44), 'text-anchor': 'end', class: 'topo-label' }, svg)
    label.textContent = r.name
    order.push(label)
    const sub = mk('text', { x: r.x - (isGw ? 54 : 60), y: r.y + (isGw ? 78 : 60), 'text-anchor': 'end', class: 'topo-label-sub' }, svg)
    sub.textContent = `${r.clients} clients · ${r.health}%`
    order.push(sub)
  })

  // --- peers WG ---
  peers.forEach((p, i) => {
    const g = mk('g', { transform: `translate(${p.x} ${p.y})`, class: 'topo-node' }, svg)
    order.push(g)
    if (!reduceMotion) {
      const pulse = mk('circle', { r: 22, fill: 'none', stroke: COLOR.tunnel, 'stroke-width': 1.5 }, g)
      mk('animate', { attributeName: 'r', values: '18;27', dur: '2.2s', repeatCount: 'indefinite' }, pulse)
      mk('animate', { attributeName: 'opacity', values: '0.5;0', dur: '2.2s', repeatCount: 'indefinite' }, pulse)
    }
    mk('rect', { x: -18, y: -18, width: 36, height: 36, rx: 11, fill: canvas, stroke: COLOR.tunnel, 'stroke-width': 1.5 }, g)
    drawIcon(p.type, 0, 0, 20, COLOR.tunnel, g)
    const label = mk('text', { x: p.x + (i % 2 ? -26 : 26), y: p.y - 4, 'text-anchor': i % 2 ? 'end' : 'start', class: 'topo-label' }, svg)
    label.textContent = p.name
    order.push(label)
    const sub = mk('text', { x: p.x + (i % 2 ? -26 : 26), y: p.y + 11, 'text-anchor': i % 2 ? 'end' : 'start', class: 'topo-label-sub', fill: COLOR.tunnel }, svg)
    sub.textContent = 'WireGuard'
    order.push(sub)
    addLink(`M${p.x} ${p.y + 18} C${p.x} 80, ${gw.x} 80, ${gw.x} ${gw.y - gw.r - 10}`, 'wg', COLOR.tunnel, 2, false, true)
  })

  // --- distnode (switch inferido) ---
  const distG = mk('g', { transform: `translate(${dist.x} ${dist.y})`, class: 'topo-node' }, svg)
  order.push(distG)
  mk('circle', { r: dist.r + 5, fill: 'none', stroke: 'rgb(var(--text-muted))', 'stroke-width': 1, 'stroke-dasharray': '2 5', opacity: 0.6 }, distG)
  mk('circle', { r: dist.r, fill: canvas, stroke: 'rgb(var(--text-muted))', 'stroke-width': 1.5, 'stroke-dasharray': '4 4' }, distG)
  drawIcon('switch', 0, 0, 22, 'rgb(var(--text-muted))', distG)
  const distLabel = mk('text', { x: dist.x, y: dist.y - 34, 'text-anchor': 'middle', class: 'topo-label' }, svg)
  distLabel.textContent = t('topo.wired')
  order.push(distLabel)

  // --- chips de dispositivos ---
  const chips = [
    // cableados del distnode (verde)
    { id: 'c1', name: 'tv-salon', type: 'tv', x: 710, y: 180, wired: true, hub: dist, band: '' },
    { id: 'c2', name: 'printer', type: 'tv', x: 700, y: 290, wired: true, hub: dist, band: '' },
    // cableados del gateway oeste
    { id: 'c3', name: 'pve', type: 'laptop', x: 280, y: 230, wired: true, hub: gw, band: '' },
    { id: 'c4', name: 'ct-home', type: 'laptop', x: 320, y: 300, wired: true, hub: gw, band: '' },
    // wifi gateway
    { id: 'c5', name: 'laptop-nacho', type: 'laptop', x: 620, y: 150, wired: false, hub: gw, band: '5 GHz', weak: false },
    { id: 'c6', name: 'phone', type: 'phone', x: 410, y: 150, wired: false, hub: gw, band: '5 GHz', weak: false },
    { id: 'c7', name: 'tablet', type: 'phone', x: 580, y: 120, wired: false, hub: gw, band: '2.4 GHz', weak: false },
    // wifi AP salon
    { id: 'c8', name: 'chromecast', type: 'tv', x: 150, y: 410, wired: false, hub: aps[0], band: '2.4 GHz', weak: true },
    { id: 'c9', name: 'consola', type: 'tv', x: 230, y: 380, wired: false, hub: aps[0], band: '5 GHz', weak: false },
    { id: 'c10', name: 'kindle', type: 'phone', x: 120, y: 500, wired: false, hub: aps[0], band: '2.4 GHz', weak: true },
    // wifi AP pasillo
    { id: 'c11', name: 'cam-patio', type: 'tv', x: 430, y: 580, wired: false, hub: aps[1], band: '2.4 GHz', weak: true },
    { id: 'c12', name: 'robot', type: 'tv', x: 570, y: 600, wired: false, hub: aps[1], band: '2.4 GHz', weak: false },
    { id: 'c13', name: 'sensor', type: 'phone', x: 500, y: 620, wired: false, hub: aps[1], band: '2.4 GHz', weak: true },
    // wifi AP dormitorio
    { id: 'c14', name: 'phone-y', type: 'phone', x: 860, y: 410, wired: false, hub: aps[2], band: '5 GHz', weak: false },
    { id: 'c15', name: 'laptop-y', type: 'laptop', x: 900, y: 500, wired: false, hub: aps[2], band: '5 GHz', weak: false },
  ]

  chips.forEach((c, i) => {
    const g = mk('g', { transform: `translate(${c.x} ${c.y})`, class: 'topo-node' }, svg)
    order.push(g)
    const S = c.wired ? 26 : 24
    const half = S / 2
    const stroke = c.wired ? COLOR.ok : 'rgb(var(--border-strong))'
    mk('rect', { x: -half, y: -half, width: S, height: S, rx: 7, fill: canvas, stroke, 'stroke-width': c.wired ? 1.3 : 1.1 }, g)
    drawIcon(c.type, 0, 0, 16, c.wired ? COLOR.ok : 'rgb(var(--text-primary))', g)
    if (!c.wired) {
      const bandColor = c.weak ? COLOR.warn : c.band === '5 GHz' ? COLOR.accent : COLOR.info
      mk('circle', { cx: half - 2, cy: half - 2, r: 3.8, fill: bandColor, stroke: canvas, 'stroke-width': 1.4 }, g)
    }
    const title = mk('title', {}, g)
    title.textContent = c.name
    // link
    const target = c.wired ? { x: c.hub.x + (c.hub === dist ? -dist.r : c.hub === gw ? -gw.r : 0), y: c.hub.y } : { x: c.hub.x, y: c.hub.y }
    const kind = c.wired ? 'wired' : 'uplink'
    const color = c.wired ? COLOR.ok : COLOR.warn
    addLink(`M${c.x} ${c.y} L${target.x} ${target.y}`, kind, color, c.wired ? 1.2 : 1, !c.wired, false)
  })

  // coreografía secuencial
  if (reduceMotion) {
    svg.querySelectorAll('.topo-node,.topo-edge,.topo-flow').forEach(e => e.classList.add('on'))
    return
  }
  let delay = 0
  order.forEach(el => {
    setTimeout(() => el.classList.add('on'), delay)
    delay += el.classList.contains('topo-edge') ? 160 : 90
  })
}

function rebuildTopo() {
  if (!topoBuilt) return
  topoBuilt = false
  const svg = document.getElementById('topoSvg')
  svg.innerHTML = ''
  buildTopo()
}

/* ---------- stage 3: routers cards fieles ---------- */
const ROUTERS = [
  { id: 'gw', name: 'GW-Flint2', model: 'GL.iNet Flint 2', role: 'Principal', cpu: 12, ram: 34, temp: 52, clients: 34, health: 92, status: 'ok', uptime: '34d 6h', spark: [12, 18, 14, 22, 16, 20, 13] },
  { id: 'ap1', name: 'AP-Salon', model: 'Xiaomi AX6', role: 'AP', cpu: 23, ram: 51, temp: 49, clients: 18, health: 88, status: 'ok', uptime: '14d 2h', spark: [21, 25, 30, 28, 35, 32, 29] },
  { id: 'ap2', name: 'AP-Pasillo', model: 'Xiaomi AX6', role: 'AP', cpu: 41, ram: 63, temp: 78, clients: 10, health: 71, status: 'warn', uptime: '9d 7h', spark: [35, 42, 58, 55, 61, 48, 52] },
  { id: 'ap3', name: 'AP-Dormitorio', model: 'Xiaomi AX6', role: 'AP', cpu: 18, ram: 48, temp: 44, clients: 5, health: 95, status: 'ok', uptime: '32d 5h', spark: [10, 14, 12, 18, 15, 13, 11] },
]
let routersBuilt = false
let routersPlayed = false
function buildRouters() {
  const grid = document.getElementById('routerGrid')
  const C = 2 * Math.PI * 31 // R=31 (size 56 /2 +stroke? inner r ~31)
  grid.innerHTML = ROUTERS.map(r => {
    const isWarn = r.status === 'warn'
    const statusColor = isWarn ? 'rgb(var(--warn))' : 'rgb(var(--ok))'
    const healthDash = C * (1 - r.health / 100)
    const sparkPath = r.spark.map((v, i) => `${i === 0 ? 'M' : 'L'}${(i / (r.spark.length - 1)) * 100} ${28 - (v / 70) * 28}`).join(' ')
    return `
    <div class="rcard card ${isWarn ? 'warn' : ''}">
      <div class="rt">
        <div class="nameblock">
          <div class="iconbox">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M4 14h16v5a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-5z"/><path d="M8 6h8"/><path d="M6 9h12"/><path d="M10 3h4"/></svg>
          </div>
          <div>
            <span class="name">${r.name}</span>
            <span class="model">${r.model}</span>
            <span class="rolepill">${t(r.role === 'Principal' ? 'routers.roleGW' : 'routers.roleAP')}</span>
          </div>
        </div>
        <div class="ring-wrap" style="width:56px;height:56px;">
          <svg width="56" height="56" viewBox="0 0 56 56" style="transform:rotate(-90deg);">
            <circle cx="28" cy="28" r="31" fill="none" stroke="rgb(var(--border))" stroke-width="5"/>
            <circle class="router-health-ring" cx="28" cy="28" r="31" fill="none" stroke="${statusColor}" stroke-width="5" stroke-linecap="round"
              stroke-dasharray="${C}" stroke-dashoffset="${C}" data-target="${healthDash}"/>
          </svg>
          <div class="ring-center" style="position:absolute;inset:0;display:flex;align-items:center;justify-content:center;">
            <span class="font-mono" style="font-size:0.78rem;font-weight:700;">${r.health}</span>
          </div>
        </div>
      </div>
      <div class="minimetrics">
        <div class="${r.cpu > 40 ? 'hot' : ''}"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="2"/><path d="M9 9h6v6H9z"/><path d="M9 1v3"/><path d="M15 1v3"/><path d="M9 20v3"/><path d="M15 20v3"/><path d="M20 9h3"/><path d="M20 15h3"/><path d="M1 9h3"/><path d="M1 15h3"/></svg> CPU <span class="font-mono">${r.cpu}%</span></div>
        <div class="${r.ram > 60 ? 'hot' : ''}"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/><path d="M9 7h6"/><path d="M9 11h6"/></svg> RAM <span class="font-mono">${r.ram}%</span></div>
        <div class="${r.temp > 70 ? 'hot' : ''}"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M14 4v10.54a4 4 0 1 1-4 0V4a2 2 0 0 1 4 0"/></svg> ${t('routers.temp')} <span class="font-mono">${r.temp}°C</span></div>
        <div><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg> ${t('routers.clients')} <span class="font-mono">${r.clients}</span></div>
      </div>
      <div class="bottom">
        <svg class="spark" viewBox="0 0 100 28" preserveAspectRatio="none"><path d="${sparkPath}" vector-effect="non-scaling-stroke"/></svg>
        <span class="font-mono" style="font-size:0.75rem;color:rgb(var(--text-muted));">${r.uptime}</span>
        <span class="statuspill ${r.status}"><span class="dot"></span>${t(r.status === 'ok' ? 'routers.online' : 'routers.warn')}</span>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width:1rem;height:1rem;color:rgb(var(--text-muted));"><path d="m9 18 6-6-6-6"/></svg>
      </div>
    </div>
    `
  }).join('')
  routersBuilt = true
}

function fillRouterBars(instant) {
  const grid = document.getElementById('routerGrid')
  grid.querySelectorAll('.router-health-ring').forEach(ring => {
    const target = ring.dataset.target
    if (instant || reduceMotion) ring.style.strokeDashoffset = target
    else setTimeout(() => ring.style.strokeDashoffset = target, 80)
  })
}

function renderRouterNames() {
  if (!routersBuilt) return
  buildRouters()
  if (routersPlayed) fillRouterBars(true)
}

function playRouters() {
  buildRouters()
  routersBuilt = true
  if (routersPlayed) { fillRouterBars(true); return }
  routersPlayed = true
  fillRouterBars(false)
}

/* ---------- stage 4: adguard ---------- */
const AG_DOMAINS = [
  { d: 'graph.facebook.com', n: 12840 },
  { d: 'ads.doubleclick.net', n: 9315 },
  { d: 'app-measurement.com', n: 7602 },
  { d: 'telemetry.microsoft.com', n: 4188 },
  { d: 'tracking.miui.com', n: 2941 },
]
let agPlayed = false
function playAdGuard() {
  if (agPlayed) return
  agPlayed = true
  const q = document.getElementById('agQueries')
  const b = document.getElementById('agBlocked')
  const c = document.getElementById('agClients')
  animateNumber(q, 184523, { duration: 1400, format: x => fmtLocale(Math.round(x)) })
  animateNumber(b, 23, { duration: 1400, format: x => Math.round(x) + '%' })
  animateNumber(c, DEMO.clients, { duration: 1200, format: x => Math.round(x) + '/' + DEMO.clientsTotal })
  const bars = document.getElementById('agBars')
  if (!bars.childElementCount) {
    const max = AG_DOMAINS[0].n
    bars.innerHTML = AG_DOMAINS.map(x => `
      <div class="ag-bar-row">
        <span class="d">${x.d}</span>
        <div class="bar"><i data-w="${Math.round((x.n / max) * 100)}"></i></div>
        <span class="n">${fmtLocale(x.n)}</span>
      </div>
    `).join('')
  }
  bars.querySelectorAll('.bar i').forEach((bar, i) => {
    const w = bar.dataset.w
    if (reduceMotion) { bar.style.width = w + '%'; return }
    setTimeout(() => { bar.style.width = w + '%' }, 150 + i * 120)
  })
}

/* ---------- stage 5: alerts ---------- */
const ALERT_ICONS = {
  warn: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>',
  ok: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>',
  info: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>',
  accent: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>',
}
function renderAlerts() {
  const feed = document.getElementById('alertFeed')
  if (!feed) return
  const items = [
    { tone: 'warn', text: t('alerts.a1'), sub: `2 min ${t('alerts.ago')}` },
    { tone: 'accent', text: t('alerts.a2'), sub: `11 min ${t('alerts.ago')}` },
    { tone: 'info', text: t('alerts.a3'), sub: `1 h ${t('alerts.ago')}` },
    { tone: 'ok', text: t('alerts.a4'), sub: `2 h ${t('alerts.ago')}` },
  ]
  feed.innerHTML = items.map(a => `
    <div class="alert-item ${a.tone}">
      <span class="ico">${ALERT_ICONS[a.tone]}</span>
      <span><span class="t">${a.text}</span><span class="s" style="display:block;">${a.sub}</span></span>
    </div>
  `).join('')
  if (alertsPlayed) feed.querySelectorAll('.alert-item').forEach(el => el.classList.add('on'))
}
let alertsPlayed = false
function playAlerts() {
  renderAlerts()
  if (alertsPlayed) return
  alertsPlayed = true
  document.querySelectorAll('#alertFeed .alert-item').forEach((el, i) => {
    if (reduceMotion) { el.classList.add('on'); return }
    setTimeout(() => el.classList.add('on'), 150 + i * 260)
  })
}

/* ---------- teatro: control de stages por scroll ---------- */
const STAGE_PLAYERS = [playRing, playTraffic, buildTopo, playRouters, playAdGuard, playAlerts]
let currentStage = -1

function setStage(i) {
  if (i === currentStage) return
  currentStage = i
  document.querySelectorAll('.stage').forEach((s, idx) => s.classList.toggle('active', idx === i))
  document.querySelectorAll('.tstep').forEach(s => {
    s.style.opacity = parseInt(s.dataset.stage, 10) === i ? '1' : ''
  })
  STAGE_PLAYERS[i]()
}

function initTheater() {
  const steps = [...document.querySelectorAll('.tstep')]
  if (!('IntersectionObserver' in window)) { setStage(0); return }
  const io = new IntersectionObserver(entries => {
    entries.forEach(en => { if (en.isIntersecting) setStage(parseInt(en.target.dataset.stage, 10)) })
  }, { rootMargin: '-42% 0px -42% 0px', threshold: 0 })
  steps.forEach(s => io.observe(s))
  setStage(0)
}

/* ---------- reveals on scroll ---------- */
function initReveals() {
  const els = document.querySelectorAll('.reveal')
  if (reduceMotion || !('IntersectionObserver' in window)) { els.forEach(e => e.classList.add('in')); return }
  const io = new IntersectionObserver(entries => {
    entries.forEach(en => { if (en.isIntersecting) { en.target.classList.add('in'); io.unobserve(en.target) } })
  }, { threshold: 0.12 })
  els.forEach(e => io.observe(e))
}

/* ---------- widget apariencia ---------- */
const MODE_KEY = 'netpulse-web-theme-mode'
const ACCENT_KEY = 'netpulse-web-accent'
let themeMode = 'dark'
let accentId = 'cyan'

function readStore(k) { try { return localStorage.getItem(k) } catch { return null } }
function writeStore(k, v) { try { localStorage.setItem(k, v) } catch { /* privado */ } }

function applyTheme() {
  const root = document.documentElement
  let light = false
  if (themeMode === 'light') light = true
  else if (themeMode === 'system') light = window.matchMedia('(prefers-color-scheme: light)').matches
  root.classList.toggle('light', light)
  const meta = document.querySelector('meta[name="theme-color"]')
  if (meta) meta.content = light ? '#F3F5F9' : '#070B12'
}
function applyAccent() { document.documentElement.dataset.accent = accentId }
function updateAppearancePanel() {
  document.querySelectorAll('#themeSeg button').forEach(b => b.classList.toggle('on', b.dataset.mode === themeMode))
  document.querySelectorAll('#accentSwatches .swatch').forEach(b => b.classList.toggle('on', b.dataset.accent === accentId))
}
function setMode(m) {
  themeMode = m; writeStore(MODE_KEY, m); applyTheme(); updateAppearancePanel()
}
function setAccent(a) {
  accentId = a; writeStore(ACCENT_KEY, a); applyAccent(); updateAppearancePanel()
}
window.setMode = setMode; window.setAccent = setAccent

function initAppearance() {
  themeMode = readStore(MODE_KEY) || 'dark'
  accentId = readStore(ACCENT_KEY) || 'cyan'
  applyTheme(); applyAccent(); updateAppearancePanel()
  const fab = document.getElementById('appFab')
  const panel = document.getElementById('appPanel')
  fab.addEventListener('click', () => { panel.classList.toggle('open'); fab.setAttribute('aria-expanded', panel.classList.contains('open')) })
  document.addEventListener('click', e => { if (!panel.contains(e.target) && !fab.contains(e.target)) panel.classList.remove('open') })
  document.addEventListener('keydown', e => { if (e.key === 'Escape') panel.classList.remove('open') })
  document.querySelectorAll('#themeSeg button').forEach(b => b.addEventListener('click', () => setMode(b.dataset.mode)))
  document.querySelectorAll('#accentSwatches .swatch').forEach(b => b.addEventListener('click', () => setAccent(b.dataset.accent)))
  window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => { if (themeMode === 'system') applyTheme() })
}

/* ---------- copiar comando ---------- */
function initCopy() {
  const btn = document.getElementById('copyBtn')
  if (!btn) return
  btn.addEventListener('click', async () => {
    const cmd = document.getElementById('installCmd').textContent
    try { await navigator.clipboard.writeText(cmd) } catch {
      const ta = document.createElement('textarea'); ta.value = cmd; document.body.appendChild(ta); ta.select(); document.execCommand('copy'); ta.remove()
    }
    const orig = t('misc.copy')
    btn.textContent = t('misc.copied')
    setTimeout(() => { btn.textContent = orig }, 1600)
  })
}

/* ---------- ko-fi placeholder ---------- */
function initKofi() {
  const link = document.getElementById('kofiLink')
  // TODO(nacho): URL definitiva de Ko-fi — pendiente de que la cree.
  link.href = 'https://github.com/gnacho/netpulse'
}

/* ---------- boot ---------- */
document.addEventListener('DOMContentLoaded', () => {
  initAppearance()
  initBackgroundCanvas()
  renderTicker()
  renderAlerts()
  initReveals()
  initTheater()
  initCopy()
  initKofi()
  heroCountUps()
})
