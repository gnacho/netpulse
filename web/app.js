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

/* ---------- fondo constelación viva (red con flujo de datos) ---------- */
function mulberry32(seed) {
  let a = seed >>> 0
  return function () {
    a |= 0; a = (a + 0x6D2B79F5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

function initBackgroundCanvas() {
  const canvas = document.getElementById('bgCanvas')
  if (!canvas) return
  const ctx = canvas.getContext('2d')
  const reduce = reduceMotion
  const dpr = Math.min(window.devicePixelRatio || 1, 2)
  let w = 0, h = 0
  let layers = [], bursts = []
  let mx = -1e4, my = -1e4, tmx = -1e4, tmy = -1e4
  const rand = mulberry32(0xC0FFEE)
  const alphaColor = (c, a) => c.replace(')', ` / ${a})`)
  const PALETTE = ['accent', 'tunnel', 'ok', 'info']

  function buildLayout() {
    layers = []
    bursts = []
    const specs = [
      { depth: 0.5, count: 42, size: [1.6, 2.6], aMin: 0.22, aMax: 0.38, linkDist: 150, linkA: 0.10, gw: 0.02 },
      { depth: 0.75, count: 30, size: [2.2, 3.4], aMin: 0.35, aMax: 0.55, linkDist: 190, linkA: 0.14, gw: 0.08 },
      { depth: 1.0, count: 18, size: [3, 4.6], aMin: 0.5, aMax: 0.75, linkDist: 230, linkA: 0.18, gw: 0.15 },
    ]
    for (const s of specs) {
      const nodes = []
      let guard = 0
      while (nodes.length < s.count && guard++ < 2500) {
        const x = rand() * w
        const y = rand() * h
        const minD = 34 + rand() * 30
        if (nodes.some((n) => Math.hypot(n.x - x, n.y - y) < minD)) continue
        nodes.push({
          x, y,
          r: s.size[0] + rand() * (s.size[1] - s.size[0]),
          a: s.aMin + rand() * (s.aMax - s.aMin),
          gw: rand() < s.gw,
          hue: Math.floor(rand() * PALETTE.length),
          ph: rand() * Math.PI * 2,
          wob: 1.5 + rand() * 2.5,
        })
      }
      const links = []
      for (let i = 0; i < nodes.length; i++) {
        const dists = []
        for (let j = 0; j < nodes.length; j++) {
          if (i === j) continue
          dists.push([Math.hypot(nodes[i].x - nodes[j].x, nodes[i].y - nodes[j].y), j])
        }
        dists.sort((a, b) => a[0] - b[0])
        for (let k = 0; k < 2; k++) {
          const [d, j] = dists[k]
          if (d > s.linkDist || i > j) continue
          links.push({ i, j, len: d, a: s.linkA * (0.6 + rand() * 0.8), ph: rand() * Math.PI * 2 })
        }
      }
      const packets = []
      links.forEach((l, li) => {
        if (rand() < 0.42) {
          const n = l.len > 190 ? 2 : 1
          for (let k = 0; k < n; k++) packets.push({ li, off: rand(), dur: 4 + rand() * 5, hue: Math.floor(rand() * PALETTE.length) })
        }
      })
      layers.push({ depth: s.depth, nodes, links, packets })
    }
  }

  function render(t) {
    ctx.clearRect(0, 0, w, h)
    for (const L of layers) {
      const k = L.depth
      ctx.save()
      ctx.translate((mx - w / 2) * 0.05 * k, (my - h / 2) * 0.05 * k)
      // wobble sutil de cada nodo (vida, sin mover los enlaces)
      for (const n of L.nodes) {
        n.wx = n.x + Math.sin(t * 0.5 + n.ph) * n.wob
        n.wy = n.y + Math.cos(t * 0.42 + n.ph) * n.wob
      }
      // enlaces (respiran muy lentamente)
      ctx.lineWidth = 1
      for (const l of L.links) {
        const a = L.nodes[l.i], b = L.nodes[l.j]
        ctx.strokeStyle = alphaColor('text-secondary', l.a * (0.65 + 0.35 * Math.sin(t * 0.4 + l.ph)))
        ctx.beginPath()
        ctx.moveTo(a.wx, a.wy)
        ctx.lineTo(b.wx, b.wy)
        ctx.stroke()
      }
      // nodos
      for (const n of L.nodes) {
        ctx.fillStyle = alphaColor(PALETTE[n.hue], n.a)
        ctx.beginPath()
        ctx.arc(n.wx, n.wy, n.r, 0, Math.PI * 2)
        ctx.fill()
        if (n.gw) {
          ctx.strokeStyle = alphaColor(PALETTE[n.hue], n.a * 0.5)
          ctx.lineWidth = 1
          ctx.beginPath()
          ctx.arc(n.wx, n.wy, n.r + 3 + Math.sin(t * 1.2 + n.ph) * 1.2, 0, Math.PI * 2)
          ctx.stroke()
        }
      }
      // paquetes de datos viajando por los enlaces
      for (const p of L.packets) {
        const l = L.links[p.li]
        const a = L.nodes[l.i], b = L.nodes[l.j]
        const frac = (p.off + t / p.dur) % 1
        const col = PALETTE[p.hue]
        ctx.save()
        ctx.shadowColor = getCssVar(col)
        ctx.shadowBlur = 8
        ctx.fillStyle = alphaColor(col, 0.9)
        ctx.beginPath()
        ctx.arc(a.wx + (b.wx - a.wx) * frac, a.wy + (b.wy - a.wy) * frac, 1.8, 0, Math.PI * 2)
        ctx.fill()
        ctx.restore()
      }
      // ráfagas ocasionales (evento que recorre un enlace)
      if (L.depth >= 0.7) {
        for (const b of bursts) {
          if (b.layer !== L.depth) continue
          const l = L.links[b.li]
          if (!l) continue
          const a = L.nodes[l.i], bb = L.nodes[l.j]
          const prog = (t - b.t0) / b.dur
          if (prog >= 0 && prog <= 1) {
            ctx.save()
            ctx.shadowColor = getCssVar('accent')
            ctx.shadowBlur = 14
            ctx.fillStyle = alphaColor('accent', 0.95 * (1 - prog))
            ctx.beginPath()
            ctx.arc(a.wx + (bb.wx - a.wx) * prog, a.wy + (bb.wy - a.wy) * prog, 2.6 * (1 - prog * 0.5), 0, Math.PI * 2)
            ctx.fill()
            ctx.restore()
          }
        }
      }
      ctx.restore()
    }
    bursts = bursts.filter((b) => t - b.t0 < b.dur)
  }

  function maybeBurst(t) {
    const L = layers[1] || layers[layers.length - 1]
    if (!L || L.links.length === 0) return
    bursts.push({ layer: L.depth, li: Math.floor(rand() * L.links.length), t0: t, dur: 1.6 })
  }

  function resize() {
    w = window.innerWidth
    h = window.innerHeight
    canvas.width = Math.round(w * dpr)
    canvas.height = Math.round(h * dpr)
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    buildLayout()
    if (reduce) render(0)
  }
  resize()
  window.addEventListener('resize', resize)
  document.addEventListener('mousemove', (e) => { tmx = e.clientX; tmy = e.clientY }, { passive: true })
  document.addEventListener('touchmove', (e) => { if (e.touches[0]) { tmx = e.touches[0].clientX; tmy = e.touches[0].clientY } }, { passive: true })
  if (reduce) return
  let nextBurst = 3 + rand() * 4
  function loop(now) {
    const t = now / 1000
    mx += (tmx - mx) * 0.06
    my += (tmy - my) * 0.06
    if (t > nextBurst) { maybeBurst(t); nextBurst = t + 5 + rand() * 6 }
    render(t)
    requestAnimationFrame(loop)
  }
  requestAnimationFrame(loop)
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
    `<span><i class="dot" style="background:rgb(var(--text-secondary))"></i><i>${t('strip.devices')}</i>&nbsp;<i class="font-mono">65</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--danger))"></i><i>AdGuard</i>&nbsp;<i class="font-mono">60/65 ${t('adguard.clients')}</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--warn))"></i><i>DNS</i>&nbsp;<i class="font-mono">14 ms</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--ok))"></i><i>WireGuard</i>&nbsp;<i class="font-mono">peer phone · ${t('alerts.ago')} 2 min</i></span>`,
  ]
  el.innerHTML = items.join('') + items.join('')
}

/* ---------- stage 0: health ring + advertencias + ancho de banda ---------- */
let ringPlayed = false
function playRing() {
  if (ringPlayed) return
  ringPlayed = true
  const arc = document.getElementById('ringArc')
  const num = document.getElementById('ringNum')
  const C = 590.6
  const score = DEMO.score
  animateNumber(document.getElementById('hDown'), DEMO_WAN.downMbps, { decimals: 1, duration: 1400 })
  animateNumber(document.getElementById('hUp'), DEMO_WAN.upMbps, { decimals: 1, duration: 1400 })
  animateNumber(document.getElementById('hLat'), DEMO_WAN.latencyMs, { duration: 1400 })
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

/* ---------- stage 1: tráfico WAN por rango (1h/24h/7d/30d) ---------- */
let trafficPlayed = false
let trafficTimer = null
let trafficRange = '24h'
const TRAFFIC_TICKS = { '1h': 5, '24h': 6, '7d': 7, '30d': 5 }
const NS_T = 'http://www.w3.org/2000/svg'

function renderTrafficChart(animate) {
  const data = DEMO_TRAFFIC[trafficRange]
  if (!data || data.length < 2) return
  const W = 480, H = 200, padL = 8, padR = 8, padT = 14, padB = 26
  const max = Math.max(1, ...data.map((p) => Math.max(p.down, p.up))) * 1.15
  const stepX = (W - padL - padR) / (data.length - 1)
  const X = (i) => padL + i * stepX
  const Y = (v) => padT + (H - padT - padB) * (1 - v / max)
  const path = (key) => data.map((p, i) => `${i ? 'L' : 'M'}${X(i).toFixed(1)} ${Y(p[key]).toFixed(1)}`).join(' ')
  const down = path('down'), up = path('up')
  const lineDown = document.getElementById('lineDown')
  const lineUp = document.getElementById('lineUp')
  const areaDown = document.getElementById('areaDown')
  const areaUp = document.getElementById('areaUp')
  const x0 = X(0).toFixed(1), xN = X(data.length - 1).toFixed(1)
  lineDown.setAttribute('d', down)
  lineUp.setAttribute('d', up)
  areaDown.setAttribute('d', `${down} L${xN} ${H} L${x0} ${H} Z`)
  areaUp.setAttribute('d', `${up} L${xN} ${H} L${x0} ${H} Z`)
  // rejilla horizontal
  const grid = document.getElementById('trafficGrid')
  grid.innerHTML = ''
  for (let g = 1; g <= 3; g++) {
    const gy = padT + (H - padT - padB) * (g / 4)
    const el = document.createElementNS(NS_T, 'line')
    el.setAttribute('x1', padL); el.setAttribute('x2', W - padR); el.setAttribute('y1', gy); el.setAttribute('y2', gy)
    el.setAttribute('stroke', 'rgb(var(--border))'); el.setAttribute('stroke-dasharray', '3 6'); el.setAttribute('opacity', '0.5')
    grid.appendChild(el)
  }
  // ticks del eje X
  const ticks = document.getElementById('trafficXTicks')
  ticks.innerHTML = ''
  const n = TRAFFIC_TICKS[trafficRange] || 5
  for (let k = 0; k < n; k++) {
    const idx = Math.round((k * (data.length - 1)) / (n - 1))
    const tx = document.createElementNS(NS_T, 'text')
    tx.setAttribute('x', X(idx)); tx.setAttribute('y', H - 8); tx.setAttribute('text-anchor', 'middle')
    tx.textContent = data[idx].t
    ticks.appendChild(tx)
  }
  if (animate) {
    const Ld = lineDown.getTotalLength()
    const Lu = lineUp.getTotalLength()
    lineDown.style.strokeDasharray = Ld; lineDown.style.strokeDashoffset = Ld
    lineUp.style.strokeDasharray = Lu; lineUp.style.strokeDashoffset = Lu
    const start = performance.now(), dur = 1400
    function frame(now) {
      const p = Math.min(1, (now - start) / dur)
      const e = easeOutCubic(p)
      lineDown.style.strokeDashoffset = Ld * (1 - e)
      lineUp.style.strokeDashoffset = Lu * (1 - e)
      areaDown.setAttribute('opacity', String(e * 0.9))
      areaUp.setAttribute('opacity', String(e * 0.8))
      if (p < 1) requestAnimationFrame(frame)
    }
    requestAnimationFrame(frame)
  } else {
    lineDown.style.strokeDasharray = ''
    lineDown.style.strokeDashoffset = ''
    lineUp.style.strokeDasharray = ''
    lineUp.style.strokeDashoffset = ''
    areaDown.setAttribute('opacity', '0.9')
    areaUp.setAttribute('opacity', '0.8')
  }
}

function renderWanFooter() {
  const w = DEMO_WAN
  const set = (id, v) => { const el = document.getElementById(id); if (el) el.textContent = v }
  set('wanPeak', `${w.peakTodayMbps} Mbps ↓`)
  set('wanAvg', `${w.avgDownMbps} Mbps ↓`)
  set('wanTotal', w.total24h)
  set('wanLoss', `${w.lossPct} %`)
}

function playTraffic() {
  if (trafficPlayed) return
  trafficPlayed = true
  renderTrafficChart(true)
  renderWanFooter()
  animateNumber(document.getElementById('wanDown'), DEMO_WAN.downMbps, { decimals: 1, duration: 1400 })
  animateNumber(document.getElementById('wanUp'), DEMO_WAN.upMbps, { decimals: 1, duration: 1400 })
  animateNumber(document.getElementById('wanLat'), DEMO_WAN.latencyMs, { duration: 1400 })
  if (reduceMotion) return
  trafficTimer = setInterval(() => {
    const downEl = document.getElementById('wanDown')
    if (!downEl.isConnected) return
    const d = Math.max(55, DEMO_WAN.downMbps + (Math.random() - 0.5) * 7)
    const u = Math.max(9, DEMO_WAN.upMbps + (Math.random() - 0.5) * 2.5)
    downEl.textContent = d.toFixed(1)
    document.getElementById('wanUp').textContent = u.toFixed(1)
  }, 2600)
}

function setTrafficRange(r) {
  if (trafficRange === r) return
  trafficRange = r
  document.querySelectorAll('#trafficRanges button').forEach((b) => b.classList.toggle('on', b.dataset.range === r))
  renderTrafficChart(false)
}

/* ---------- stage 2: topologia fiel a la app (model.ts + TopologyMap.tsx) ---------- */
let topoBuilt = false
let routersBuilt = false
let routersPlayed = false

/* ===== datos demo canonicos (demo-canon.json, D65-1) ===== */
const DEMO_WAN = {"plan":"600/600 Mbps","downMbps":84.2,"upMbps":12.6,"latencyMs":8,"lossPct":0,"publicIp":"84.122.x.x","isp":"Digi","peakTodayMbps":412,"peakTodayTime":"21:14","avgDownMbps":61,"total24h":"1,32 TB"}
/* tráfico WAN por rango (mock.ts, canon) y AdGuard Home (demo-canon.json) */
const DEMO_TRAFFIC = {"1h":[{"t":"21:30","down":68,"up":9},{"t":"21:33","down":74,"up":10},{"t":"21:36","down":81,"up":11},{"t":"21:39","down":92,"up":12},{"t":"21:42","down":88,"up":10},{"t":"21:45","down":96,"up":13},{"t":"21:48","down":104,"up":14},{"t":"21:51","down":98,"up":12},{"t":"21:54","down":90,"up":11},{"t":"21:57","down":86,"up":10},{"t":"22:00","down":82,"up":11},{"t":"22:03","down":78,"up":12},{"t":"22:06","down":84,"up":13},{"t":"22:09","down":91,"up":14},{"t":"22:12","down":88,"up":12},{"t":"22:15","down":84,"up":11},{"t":"22:18","down":80,"up":10},{"t":"22:21","down":83,"up":12},{"t":"22:24","down":86,"up":13},{"t":"22:27","down":84,"up":12.6}],"24h":[{"t":"00","down":42,"up":8},{"t":"01","down":28,"up":6},{"t":"02","down":15,"up":5},{"t":"03","down":9,"up":4},{"t":"04","down":7,"up":4},{"t":"05","down":8,"up":5},{"t":"06","down":14,"up":6},{"t":"07","down":32,"up":9},{"t":"08","down":58,"up":14},{"t":"09","down":74,"up":18},{"t":"10","down":88,"up":21},{"t":"11","down":96,"up":24},{"t":"12","down":112,"up":26},{"t":"13","down":124,"up":28},{"t":"14","down":118,"up":25},{"t":"15","down":132,"up":27},{"t":"16","down":148,"up":30},{"t":"17","down":176,"up":34},{"t":"18","down":224,"up":42},{"t":"19","down":298,"up":55},{"t":"20","down":356,"up":71},{"t":"21","down":412,"up":96},{"t":"22","down":284,"up":48},{"t":"23","down":122,"up":21}],"7d":[{"t":"Mon","down":61,"up":12},{"t":"Tue","down":58,"up":11},{"t":"Wed","down":64,"up":13},{"t":"Thu","down":71,"up":15},{"t":"Fri","down":88,"up":19},{"t":"Sat","down":96,"up":22},{"t":"Sun","down":84,"up":18}],"30d":[{"t":"1","down":54,"up":10},{"t":"4","down":58,"up":11},{"t":"7","down":62,"up":12},{"t":"10","down":57,"up":11},{"t":"13","down":66,"up":13},{"t":"16","down":72,"up":15},{"t":"19","down":68,"up":14},{"t":"22","down":75,"up":16},{"t":"25","down":81,"up":18},{"t":"28","down":78,"up":17},{"t":"30","down":84,"up":19}]}
const DEMO_ADGUARD = {"blockedPct":18.6,"blocked24h":15687,"queries24h":84312,"trackersBlocked":9204,"dnsLatencyMs":14,"clientsUsing":60,"clientsTotal":65,"topBlocked":[{"domain":"graph.facebook.com","count":1204},{"domain":"adservice.google.com","count":986},{"domain":"metrics.icloud.com","count":731}]}
const DEMO_WG = {
  interface: 'wg0', subnet: '10.0.0.1/24', status: 'active',
  peers: [
    {"id":"pixel-8-pro","name":"Pixel 8 Pro","type":"movil","tunnelIp":"10.0.0.2","active":true,"lastHandshake":"hace 38 s","rx":"1,2 GB","tx":"214 MB"},
    {"id":"macbook-air","name":"MacBook Air","type":"portatil","tunnelIp":"10.0.0.3","active":true,"lastHandshake":"hace 1 min","rx":"640 MB","tx":"88 MB"},
    {"id":"ipad-air","name":"iPad Air","type":"tablet","tunnelIp":"10.0.0.4","active":false,"lastHandshake":"hace 2 días","rx":"3,1 GB","tx":"402 MB"},
    {"id":"portatil-trabajo","name":"Portátil trabajo","type":"portatil","tunnelIp":"10.0.0.5","active":false,"lastHandshake":"hace 6 h","rx":"812 MB","tx":"121 MB"},
    {"id":"casa-familia","name":"Casa familia","type":"sitio","tunnelIp":"10.0.0.6","active":false,"lastHandshake":"hace 9 días","rx":"12 GB","tx":"4,2 GB"},
  ],
}
const DEMO_DISTRIBUTION = [
  {"id":"dist-flint2-lan3","kind":"inferred","routerId":"flint2","port":"lan3","macCount":8},
  {"id":"dist-pve","kind":"hypervisor","routerId":"flint2","port":"lan5","macCount":11,"hostDeviceId":"pve","name":"Proxmox pve"},
  {"id":"dist-living-lan3","kind":"managed","routerId":"living","port":"lan3","macCount":4,"name":"GS308E","ip":"192.168.8.13","mac":"28:C6:8E:1D:90:44","lldp":{"chassis":"GS308E","mgmt":"192.168.8.13","caps":"Bridge","portDesc":"ge5"}},
]
const DEMO_DEVICES = [
  {"id":"imac-salon","name":"iMac Salón","type":"ordenador","manufacturer":"Apple","ip":"192.168.8.21","mac":"A4:83:E7:21:0B:3C","routerId":"living","band":"5 GHz","signalDbm":-48,"trafficMbps":32.4,"online":true},
  {"id":"tv-samsung","name":"TV Samsung","type":"tv","manufacturer":"Samsung","ip":"192.168.8.34","mac":"8C:EA:48:5D:2F:91","routerId":"living","band":"5 GHz","signalDbm":-52,"trafficMbps":18.1,"online":true},
  {"id":"pixel-8-pro","name":"Pixel 8 Pro","type":"movil","manufacturer":"Google","ip":"192.168.8.45","mac":"F2:6D:19:A8:44:C2","routerId":"flint2","band":"5 GHz","signalDbm":-41,"trafficMbps":6.2,"online":true},
  {"id":"macbook-air","name":"MacBook Air","type":"portatil","manufacturer":"Apple","ip":"192.168.8.23","mac":"3C:22:FB:71:9E:05","routerId":"estudio","band":"5 GHz","signalDbm":-45,"trafficMbps":4.8,"online":true},
  {"id":"ps5","name":"PS5","type":"consola","manufacturer":"Sony","ip":"192.168.8.31","mac":"78:C8:81:0A:6B:D4","routerId":"living","band":"cable","signalDbm":null,"trafficMbps":12.7,"online":true,"port":"lan1"},
  {"id":"robot-aspirador","name":"Robot aspirador","type":"iot","manufacturer":"Roborock","ip":"192.168.8.61","mac":"B0:4A:39:2E:77:10","routerId":"patio","band":"2.4 GHz","signalDbm":-67,"trafficMbps":0.02,"online":true},
  {"id":"camara-porche","name":"Cámara porche","type":"camara","manufacturer":"Reolink","ip":"192.168.8.71","mac":"EC:71:DB:44:12:8A","routerId":"patio","band":"2.4 GHz","signalDbm":-72,"trafficMbps":1.1,"online":true},
  {"id":"nest-mini","name":"Nest Mini","type":"altavoz","manufacturer":"Google","ip":"192.168.8.52","mac":"1A:2B:3C:4D:5E:6F","routerId":"estudio","band":"2.4 GHz","signalDbm":-55,"trafficMbps":0.4,"online":true},
  {"id":"nas-synology","name":"NAS Synology","type":"servidor","manufacturer":"Synology","ip":"192.168.8.10","mac":"00:11:32:9C:51:B7","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":2.3,"online":true,"port":"lan4"},
  {"id":"galaxy-tab-s9","name":"Galaxy Tab S9","type":"tablet","manufacturer":"Samsung","ip":"192.168.8.48","mac":"D6:91:2F:07:B3:55","routerId":"living","band":"5 GHz","signalDbm":-50,"trafficMbps":1.8,"online":true},
  {"id":"iphone-ana","name":"iPhone de Ana","type":"movil","manufacturer":"Apple","ip":"192.168.8.44","mac":"F4:D4:88:19:C2:71","routerId":"flint2","band":"5 GHz","signalDbm":-46,"trafficMbps":2.1,"online":true},
  {"id":"macbook-pro","name":"MacBook Pro de Marc","type":"portatil","manufacturer":"Apple","ip":"192.168.8.26","mac":"F0:18:98:5A:11:E9","routerId":"flint2","band":"5 GHz","signalDbm":-44,"trafficMbps":8.6,"online":true},
  {"id":"pc-sobremesa","name":"PC de sobremesa","type":"ordenador","manufacturer":"ASUSTeK","ip":"192.168.8.11","mac":"04:D4:C4:8B:30:A7","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":21.3,"online":true,"attachTo":"dist-flint2-lan3"},
  {"id":"raspberry-pi","name":"Raspberry Pi 4","type":"servidor","manufacturer":"Raspberry Pi","ip":"192.168.8.12","mac":"DC:A6:32:4F:77:02","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":0.8,"online":true,"attachTo":"dist-flint2-lan3"},
  {"id":"timbre-nest","name":"Timbre Nest","type":"camara","manufacturer":"Google","ip":"192.168.8.72","mac":"F4:F5:D8:66:01:B8","routerId":"flint2","band":"2.4 GHz","signalDbm":-58,"trafficMbps":0.6,"online":true},
  {"id":"enchufe-lavadora","name":"Enchufe lavadora","type":"iot","manufacturer":"TP-Link","ip":"192.168.8.81","mac":"50:C7:BF:22:E1:9C","routerId":"flint2","band":"2.4 GHz","signalDbm":-62,"trafficMbps":0.01,"online":true},
  {"id":"pixel-7","name":"Pixel 7","type":"movil","manufacturer":"Google","ip":"192.168.8.46","mac":"3C:5A:B4:08:D7:5E","routerId":"flint2","band":"5 GHz","signalDbm":-49,"trafficMbps":1.4,"online":true},
  {"id":"ipad-air","name":"iPad Air","type":"tablet","manufacturer":"Apple","ip":"192.168.8.47","mac":"8C:85:90:2F:B4:11","routerId":"flint2","band":"5 GHz","signalDbm":-54,"trafficMbps":0,"online":false},
  {"id":"portatil-trabajo","name":"Portátil trabajo","type":"portatil","manufacturer":"Lenovo","ip":"192.168.8.27","mac":"54:EE:75:9A:03:F1","routerId":"flint2","band":"5 GHz","signalDbm":-50,"trafficMbps":0,"online":false},
  {"id":"kindle","name":"Kindle Paperwhite","type":"desconocido","manufacturer":"Amazon","ip":"192.168.8.49","mac":"44:65:0D:71:28:C3","routerId":"flint2","band":"2.4 GHz","signalDbm":-58,"trafficMbps":0,"online":false},
  {"id":"bombilla-1","name":"Bombilla salón 1","type":"iot","manufacturer":"Ikea Trådfri","ip":"192.168.8.90","mac":"CC:86:EC:10:04:21","routerId":"living","band":"2.4 GHz","signalDbm":-58,"trafficMbps":0,"online":true},
  {"id":"bombilla-2","name":"Bombilla salón 2","type":"iot","manufacturer":"Ikea Trådfri","ip":"192.168.8.91","mac":"CC:86:EC:10:04:22","routerId":"living","band":"2.4 GHz","signalDbm":-59,"trafficMbps":0,"online":true},
  {"id":"bombilla-3","name":"Bombilla lámpara pie","type":"iot","manufacturer":"Ikea Trådfri","ip":"192.168.8.92","mac":"CC:86:EC:10:04:23","routerId":"living","band":"2.4 GHz","signalDbm":-61,"trafficMbps":0,"online":true},
  {"id":"bombilla-4","name":"Bombilla entrada","type":"iot","manufacturer":"Ikea Trådfri","ip":"192.168.8.93","mac":"CC:86:EC:10:04:24","routerId":"living","band":"2.4 GHz","signalDbm":-64,"trafficMbps":0,"online":true},
  {"id":"bombilla-5","name":"Bombilla pasillo","type":"iot","manufacturer":"Ikea Trådfri","ip":"192.168.8.94","mac":"CC:86:EC:10:04:25","routerId":"living","band":"2.4 GHz","signalDbm":-66,"trafficMbps":0,"online":true},
  {"id":"bombilla-6","name":"Bombilla cocina","type":"iot","manufacturer":"Ikea Trådfri","ip":"192.168.8.95","mac":"CC:86:EC:10:04:26","routerId":"living","band":"2.4 GHz","signalDbm":-60,"trafficMbps":0,"online":true},
  {"id":"chromecast","name":"Chromecast HD","type":"tv","manufacturer":"Google","ip":"192.168.8.36","mac":"54:60:09:E3:5B:0A","routerId":"living","band":"5 GHz","signalDbm":-54,"trafficMbps":3.9,"online":true},
  {"id":"homepod-mini","name":"HomePod mini","type":"altavoz","manufacturer":"Apple","ip":"192.168.8.53","mac":"F0:D1:A9:3E:77:5C","routerId":"living","band":"5 GHz","signalDbm":-47,"trafficMbps":0.3,"online":true},
  {"id":"galaxy-s23","name":"Galaxy S23","type":"movil","manufacturer":"Samsung","ip":"192.168.8.42","mac":"5C:0A:5B:88:1D:E4","routerId":"living","band":"5 GHz","signalDbm":-51,"trafficMbps":0.9,"online":true},
  {"id":"echo-dot","name":"Echo Dot","type":"altavoz","manufacturer":"Amazon","ip":"192.168.8.54","mac":"74:C2:46:19:F0:6B","routerId":"living","band":"2.4 GHz","signalDbm":-56,"trafficMbps":0.2,"online":true},
  {"id":"nintendo-switch","name":"Nintendo Switch","type":"consola","manufacturer":"Nintendo","ip":"192.168.8.33","mac":"58:BD:A3:4C:E2:09","routerId":"living","band":"5 GHz","signalDbm":-53,"trafficMbps":0.1,"online":true},
  {"id":"portatil-invitado","name":"Portátil invitado","type":"portatil","manufacturer":"Desconocido","ip":"192.168.8.29","mac":"A2:7E:9C:41:0B:6D","routerId":"living","band":"5 GHz","signalDbm":-58,"trafficMbps":0.7,"online":true},
  {"id":"portatil-antiguo","name":"Portátil antiguo","type":"portatil","manufacturer":"HP","ip":"192.168.8.28","mac":"3C:52:82:5D:90:17","routerId":"living","band":"2.4 GHz","signalDbm":-62,"trafficMbps":0,"online":false},
  {"id":"mac-mini","name":"Mac mini","type":"ordenador","manufacturer":"Apple","ip":"192.168.8.22","mac":"A4:83:E7:66:2C:98","routerId":"estudio","band":"cable","signalDbm":null,"trafficMbps":1.9,"online":true},
  {"id":"enchufe-ventilador","name":"Enchufe ventilador","type":"iot","manufacturer":"TP-Link","ip":"192.168.8.82","mac":"9C:53:22:B1:4E:70","routerId":"estudio","band":"2.4 GHz","signalDbm":-59,"trafficMbps":0.01,"online":true},
  {"id":"ipad-pro","name":"iPad Pro","type":"tablet","manufacturer":"Apple","ip":"192.168.8.51","mac":"F0:18:98:91:5A:2B","routerId":"estudio","band":"5 GHz","signalDbm":-49,"trafficMbps":0.8,"online":true},
  {"id":"hue-hub","name":"Hub Philips Hue","type":"iot","manufacturer":"Signify","ip":"192.168.8.15","mac":"00:17:88:2A:91:CE","routerId":"estudio","band":"cable","signalDbm":null,"trafficMbps":0.02,"online":true},
  {"id":"sonos-one","name":"Sonos One","type":"altavoz","manufacturer":"Sonos","ip":"192.168.8.55","mac":"48:A6:B8:14:72:E0","routerId":"estudio","band":"2.4 GHz","signalDbm":-57,"trafficMbps":0.5,"online":true},
  {"id":"iphone-trabajo","name":"iPhone de trabajo","type":"movil","manufacturer":"Apple","ip":"192.168.8.50","mac":"8C:85:90:47:C1:93","routerId":"estudio","band":"5 GHz","signalDbm":-47,"trafficMbps":0.3,"online":true},
  {"id":"macbook-viejo","name":"MacBook viejo","type":"portatil","manufacturer":"Apple","ip":"192.168.8.25","mac":"3C:22:FB:0E:66:A1","routerId":"estudio","band":"2.4 GHz","signalDbm":-60,"trafficMbps":0,"online":false},
  {"id":"camara-jardin","name":"Cámara jardín","type":"camara","manufacturer":"Reolink","ip":"192.168.8.73","mac":"EC:71:DB:44:12:9B","routerId":"patio","band":"2.4 GHz","signalDbm":-74,"trafficMbps":1.4,"online":true},
  {"id":"sensor-riego","name":"Sensor de riego","type":"iot","manufacturer":"Tuya","ip":"192.168.8.89","mac":"D8:1F:12:5B:08:44","routerId":"patio","band":"2.4 GHz","signalDbm":-71,"trafficMbps":0.01,"online":true},
  {"id":"enchufe-calefactor","name":"Enchufe calefactor","type":"iot","manufacturer":"TP-Link","ip":"192.168.8.80","mac":"50:C7:BF:31:7A:05","routerId":"patio","band":"2.4 GHz","signalDbm":-75,"trafficMbps":0.02,"online":true},
  {"id":"camara-garaje","name":"Cámara garaje","type":"camara","manufacturer":"Reolink","ip":"192.168.8.74","mac":"EC:71:DB:44:13:02","routerId":"patio","band":"2.4 GHz","signalDbm":-78,"trafficMbps":0,"online":false},
  {"id":"switch-netgear","name":"Switch GS308E","type":"switch","manufacturer":"Netgear","ip":"192.168.8.13","mac":"28:C6:8E:1D:90:44","routerId":"living","band":"cable","signalDbm":null,"trafficMbps":0.02,"online":true,"port":"lan3","attachTo":"living","lldp":{"chassis":"GS308E","mgmt":"192.168.8.13","caps":"Bridge","portDesc":"ge5"}},
  {"id":"xbox-series-s","name":"Xbox Series S","type":"consola","manufacturer":"Microsoft","ip":"192.168.8.35","mac":"7C:ED:8D:4A:11:22","routerId":"living","band":"cable","signalDbm":null,"trafficMbps":9.8,"online":true,"attachTo":"dist-living-lan3"},
  {"id":"apple-tv-4k","name":"Apple TV 4K","type":"tv","manufacturer":"Apple","ip":"192.168.8.36","mac":"F0:18:98:2B:33:44","routerId":"living","band":"cable","signalDbm":null,"trafficMbps":15.2,"online":true,"attachTo":"dist-living-lan3"},
  {"id":"receptor-denon","name":"Receptor Denon","type":"altavoz","manufacturer":"Denon","ip":"192.168.8.37","mac":"00:05:CD:55:66:77","routerId":"living","band":"cable","signalDbm":null,"trafficMbps":0.6,"online":true,"attachTo":"dist-living-lan3"},
  {"id":"pve","name":"Proxmox pve","type":"servidor","manufacturer":"Supermicro","ip":"192.168.8.5","mac":"3C:52:82:10:20:30","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":12.3,"online":true,"port":"lan5"},
  {"id":"ct-pihole","name":"Pi-hole","type":"servidor","manufacturer":"Proxmox VE (CT)","ip":"192.168.8.41","mac":"BC:24:11:00:20:10","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":6.1,"online":true,"attachTo":"pve"},
  {"id":"ct-home-assistant","name":"Home Assistant","type":"iot","manufacturer":"Proxmox VE (CT)","ip":"192.168.8.42","mac":"BC:24:11:00:21:11","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":4.2,"online":true,"attachTo":"pve"},
  {"id":"ct-nextcloud","name":"Nextcloud","type":"servidor","manufacturer":"Proxmox VE (CT)","ip":"192.168.8.43","mac":"BC:24:11:00:22:12","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":7.8,"online":true,"attachTo":"pve"},
  {"id":"ct-jellyfin","name":"Jellyfin","type":"servidor","manufacturer":"Proxmox VE (CT)","ip":"192.168.8.44","mac":"BC:24:11:00:23:13","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":9.4,"online":true,"attachTo":"pve"},
  {"id":"ct-immich","name":"Immich","type":"servidor","manufacturer":"Proxmox VE (CT)","ip":"192.168.8.45","mac":"BC:24:11:00:24:14","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":3.3,"online":true,"attachTo":"pve"},
  {"id":"ct-gitea","name":"Gitea","type":"servidor","manufacturer":"Proxmox VE (CT)","ip":"192.168.8.46","mac":"BC:24:11:00:25:15","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":1.2,"online":true,"attachTo":"pve"},
  {"id":"ct-uptime-kuma","name":"Uptime Kuma","type":"iot","manufacturer":"Proxmox VE (CT)","ip":"192.168.8.47","mac":"BC:24:11:00:26:16","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":0.4,"online":true,"attachTo":"pve"},
  {"id":"ct-adguard-sync","name":"AdGuard sync","type":"servidor","manufacturer":"Proxmox VE (CT)","ip":"192.168.8.48","mac":"BC:24:11:00:27:17","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":0.8,"online":true,"attachTo":"pve"},
  {"id":"ct-postgres","name":"Postgres","type":"servidor","manufacturer":"Proxmox VE (CT)","ip":"192.168.8.49","mac":"BC:24:11:00:28:18","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":2.1,"online":true,"attachTo":"pve"},
  {"id":"ct-redis","name":"Redis","type":"servidor","manufacturer":"Proxmox VE (CT)","ip":"192.168.8.50","mac":"BC:24:11:00:29:19","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":0.9,"online":true,"attachTo":"pve"},
  {"id":"tv-salon-cable","name":"TV Salón (cable)","type":"tv","manufacturer":"Samsung","ip":"192.168.8.61","mac":"8C:EA:48:AA:02:02","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":24.4,"online":true,"attachTo":"dist-flint2-lan3"},
  {"id":"impresora-hp","name":"Impresora HP","type":"iot","manufacturer":"HP","ip":"192.168.8.62","mac":"3C:D9:2B:AA:03:03","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":0.1,"online":true,"attachTo":"dist-flint2-lan3"},
  {"id":"xbox-one","name":"Xbox One","type":"consola","manufacturer":"Microsoft","ip":"192.168.8.64","mac":"7C:ED:8D:AA:05:05","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":4.2,"online":true,"attachTo":"dist-flint2-lan3"},
  {"id":"receptor-av","name":"Receptor AV","type":"altavoz","manufacturer":"Denon","ip":"192.168.8.65","mac":"00:05:CD:AA:06:06","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":0.3,"online":true,"attachTo":"dist-flint2-lan3"},
  {"id":"deco-orange","name":"Deco Orange","type":"tv","manufacturer":"Sagemcom","ip":"192.168.8.66","mac":"48:83:B4:AA:07:07","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":1.1,"online":true,"attachTo":"dist-flint2-lan3"},
  {"id":"pc-invitado","name":"PC invitado","type":"ordenador","manufacturer":"—","ip":"192.168.8.67","mac":"A2:F4:11:AA:08:08","routerId":"flint2","band":"cable","signalDbm":null,"trafficMbps":0.8,"online":true,"attachTo":"dist-flint2-lan3"},
]
const DEMO_ROUTERS = [
  {"id":"flint2","name":"Gateway","model":"GL.iNet Flint 2 (GL-MT6000)","modelShort":"GL.iNet Flint 2","role":"Gateway principal","roleBadge":"Principal","ip":"192.168.8.1","status":"online","health":98,"cpu":23,"ram":41,"temp":54,"uptime":"32d 14h","clients":26,"backhaul":"cable","sparkline":[8,6,5,5,6,9,18,32,41,38,35,44,52,48,45,55,68,84,96,120,150,110,84,40]},
  {"id":"living","name":"Salón","model":"OpenWrt AP (Xiaomi AX3000T)","modelShort":"Xiaomi AX3000T","role":"Punto de acceso","roleBadge":"AP","ip":"192.168.8.2","status":"online","health":95,"cpu":12,"ram":38,"temp":47,"uptime":"32d 14h","clients":20,"backhaul":"cable","sparkline":[4,3,3,2,3,5,10,22,28,26,24,30,38,35,33,42,55,72,88,105,132,92,61,28],"lldp":{"chassis":"Flint 2","mgmt":"192.168.8.1","caps":"Bridge, Router","portDesc":"lan1"}},
  {"id":"estudio","name":"Estudio","model":"OpenWrt (NanoPi R4S)","modelShort":"NanoPi R4S","role":"AP + switch","roleBadge":"AP","ip":"192.168.8.3","status":"online","health":92,"cpu":18,"ram":44,"temp":51,"uptime":"11d 3h","clients":8,"backhaul":"cable","sparkline":[2,2,1,1,2,4,8,15,22,25,24,22,26,24,21,24,28,31,29,24,18,12,8,4],"lldp":{"chassis":"Flint 2","mgmt":"192.168.8.1","caps":"Bridge, Router","portDesc":"lan2"}},
  {"id":"patio","name":"Patio","model":"OpenWrt (TP-Link EAP225)","modelShort":"TP-Link EAP225","role":"AP exterior","roleBadge":"AP","ip":"192.168.8.4","status":"warn","health":68,"cpu":31,"ram":57,"temp":71,"uptime":"4d 2h","clients":5,"hotMetric":"temp","backhaul":"wifi","sparkline":[1,1,1,1,1,2,3,5,7,8,8,9,10,9,8,9,11,12,13,12,9,6,4,2]},
]

/* ===== utilidades de geometria (de model.ts) ===== */
const VB_W = 1000, VB_H = 680
const GATEWAY_COORD = { x: 500, y: 250, r: 40, label: { x: 446, y: 312, anchor: 'end' } }
const AP_COORDS = [
  { x: 195, y: 470, r: 32, label: { x: 124, y: 464, anchor: 'end' } },
  { x: 500, y: 505, r: 32, label: { x: 554, y: 500, anchor: 'start' } },
  { x: 805, y: 470, r: 32, label: { x: 858, y: 464, anchor: 'start' } },
]
const INTERNET_COORD = { x: 500, y: 58 }
const PEER_COORDS = [{ x: 265, y: 26 }, { x: 735, y: 26 }, { x: 140, y: 26 }, { x: 860, y: 26 }]
const PEERS_OVERFLOW_COORD = { x: 560, y: 26 }
const UPLINK_PATHS = [
  { d: 'M 464 282 C 390 340, 290 400, 224 440', lx: 292, ly: 366 },
  { d: 'M 500 292 C 500 360, 500 420, 500 468', lx: 514, ly: 418 },
  { d: 'M 536 282 C 610 340, 710 400, 776 440', lx: 664, ly: 366 },
]
const WG_PATHS = [
  { d: 'M 272 44 C 330 82, 410 94, 474 80', dur: 3 },
  { d: 'M 728 44 C 670 82, 590 94, 526 80', dur: 3.4 },
]
const GATEWAY_RINGS = [{ r: 96, cap: 5 }, { r: 130, cap: 8 }]
const AP_RINGS = [{ r: 82, cap: 7 }, { r: 120, cap: 13 }]
const GW_EAST_FAN = [336, 24]
const GW_WEST_FAN = [150, 210]
const AP_WIRED_FAN = [50, 130]
const DIST_FAN_RADIUS = 96
const DIST_FAN_MAX = 5
const DIST_RINGS = [{ r: 72, cap: 7 }, { r: 118, cap: 12 }]
const HUB_FAN_RADIUS = 68
const ROUTER_FAN_RADIUS = 150
const HYPERVISOR_FAN_RADIUS = 320
const CT_COLS = 5, CT_DX = 46, CT_DY = 44, CT_OFFSET_Y = 64
const GW_GRID_MIN = 6, GW_GRID_ROWS = 4, GW_GRID_DX = 56, GW_GRID_DY = 50, GW_GRID_R0 = 160
const TOPO_COLOR = { accent: '#22D3EE', tunnel: '#A78BFA', ok: '#34D399', warn: '#FBBF24', danger: '#F87171', info: '#60A5FA' }

function bandColor(band, weak) { if (weak) return TOPO_COLOR.warn; if (band === '5 GHz') return TOPO_COLOR.accent; if (band === '2.4 GHz') return TOPO_COLOR.info; return TOPO_COLOR.ok }
function statusColor(s) { if (s === 'warn') return TOPO_COLOR.warn; if (s === 'offline') return TOPO_COLOR.danger; return TOPO_COLOR.ok }
function linkColor(k, wifi) { if (k === 'wg') return TOPO_COLOR.tunnel; if (k === 'uplink' && wifi) return TOPO_COLOR.warn; return TOPO_COLOR.ok }
function flowFor(mbps, alive = false) { const packets = mbps >= 35 ? 3 : mbps >= 15 ? 2 : mbps >= 2 ? 1 : alive ? 1 : 0; return { packets, packetDur: Math.max(1.6, 5 - mbps / 12) } }

const rad = (d) => (d * Math.PI) / 180
const norm = (a) => ((a % 360) + 360) % 360
function angleTo(x, y, tx, ty) { return norm((Math.atan2(ty - y, tx - x) * 180) / Math.PI) }
function pos(cx, cy, degA, r) { return { x: Math.round((cx + r * Math.cos(rad(degA))) * 10) / 10, y: Math.round((cy + r * Math.sin(rad(degA))) * 10) / 10 } }
function arcAround(center, half) { const s = norm(center - half); const e = norm(center + half); return s <= e ? [[s, e]] : [[s, 360], [0, e]] }
function freeArcs(excludes) {
  const ex = excludes.map(([s, e]) => [Math.max(0, s), Math.min(360, e)]).filter(([s, e]) => e > s).sort((a, b) => a[0] - b[0])
  const free = []; let cur = 0
  for (const [s, e] of ex) { if (s > cur) free.push([cur, s]); cur = Math.max(cur, e) }
  if (cur < 360) free.push([cur, 360])
  return free
}
function ringLayout(items, node, rings, excludes) {
  const free = freeArcs(excludes); const total = free.reduce((a, [s, e]) => a + (e - s), 0)
  if (total <= 0) return
  let idx = 0
  for (const ring of rings) {
    if (idx >= items.length) break
    const n = Math.min(ring.cap, items.length - idx)
    let placed = 0
    for (let ai = 0; ai < free.length && placed < n; ai++) {
      const [s, e] = free[ai]
      let k = Math.min(Math.round((n * (e - s)) / total), n - placed)
      if (ai === free.length - 1) k = n - placed
      for (let i = 0; i < k; i++) {
        const a = s + ((e - s) * (i + 0.5)) / k
        const p = pos(node.x, node.y, a, ring.r)
        Object.assign(items[idx++], p); placed++
      }
    }
  }
}
function fanLayout(items, node, r, a0, a1) {
  if (a1 < a0) a1 += 360
  const n = items.length
  items.forEach((d, i) => { const a = a0 + ((a1 - a0) * (n === 1 ? 0.5 : i / (n - 1))); Object.assign(d, pos(node.x, node.y, norm(a), r)) })
}
function gridLayoutWest(items, node) {
  items.forEach((d, i) => {
    const col = Math.floor(i / GW_GRID_ROWS), row = i % GW_GRID_ROWS
    const inThisCol = Math.min(GW_GRID_ROWS, items.length - col * GW_GRID_ROWS)
    d.x = node.x - GW_GRID_R0 - col * GW_GRID_DX
    d.y = node.y + (row - (inThisCol - 1) / 2) * GW_GRID_DY
  })
}
function subtreeTraffic(childrenOf, hubId, seen = new Set()) {
  if (seen.has(hubId)) return 0
  seen.add(hubId)
  let sum = 0
  for (const d of childrenOf(hubId)) { sum += d.trafficMbps + subtreeTraffic(childrenOf, d.id, seen) }
  return sum
}

/* ===== builder de modelo (simplificado: sin semantica server-side) ===== */
function buildTopologyModel({ routers, devices, wan, wireguard, distributionNodes = [] }) {
  const gateway = routers.find((r) => r.roleBadge === 'Principal') || routers[0]
  const aps = routers.filter((r) => r.id !== gateway.id).slice(0, AP_COORDS.length)
  const gatewayNode = gateway ? { kind: 'router', id: gateway.id, router: gateway, ...GATEWAY_COORD } : null
  const apNodes = aps.map((router, i) => ({ kind: 'router', id: router.id, router, ...AP_COORDS[i] }))
  const routerNodes = gatewayNode ? [gatewayNode, ...apNodes] : apNodes
  const routerById = new Map(routerNodes.map((n) => [n.id, n]))
  const internetNode = { id: 'internet', ...INTERNET_COORD }

  const managedMacs = new Set(distributionNodes.filter((n) => n.kind === 'managed' && n.mac).map((n) => n.mac.toUpperCase()))
  const online = devices.filter((d) => d.online && !managedMacs.has(d.mac.toUpperCase()))
  const deviceById = new Map(online.map((d) => [d.id, d]))
  const distById = new Map(distributionNodes.map((n) => [n.id, n]))
  const hubOf = (d) => {
    if (d.attachTo && (routerById.has(d.attachTo) || distById.has(d.attachTo) || deviceById.has(d.attachTo))) return d.attachTo
    if (isWired(d) && !d.port && gatewayNode) return gatewayNode.id
    return d.routerId
  }
  const isWired = (d) => d.band === 'cable'
  const childrenOf = (hubId) => online.filter((d) => hubOf(d) === hubId)
  const deviceHubs = new Set()
  for (const d of online) { if (d.attachTo && deviceById.has(d.attachTo)) deviceHubs.add(d.attachTo) }

  const distNodes = []
  const anchorPos = new Map()
  const fanArcByRouter = new Map()
  const hypervisorHosts = new Set(distributionNodes.filter((n) => n.kind === 'hypervisor' && n.hostDeviceId).map((n) => n.hostDeviceId))

  for (const node of routerNodes) {
    const isGw = node.id === gatewayNode?.id
    const dists = distributionNodes.filter((n) => n.routerId === node.id && (n.kind === 'inferred' || n.kind === 'managed'))
    const directWired = childrenOf(node.id).filter((d) => isWired(d))
    const hubs = directWired.filter((d) => deviceHubs.has(d.id))
    const plain = directWired.filter((d) => !deviceHubs.has(d.id))
    const anchors = [
      ...dists.map((n) => ({ id: n.id, kind: 'dist', ref: n })),
      ...hubs.map((d) => ({ id: d.id, kind: 'hub', ref: d })),
      ...plain.map((d) => ({ id: d.id, kind: 'dev', ref: d })),
    ]
    if (anchors.length === 0) continue
    const placed = []
    if (isGw) {
      const east = anchors.filter((a) => a.kind === 'dist')
      const farWest = anchors.filter((a) => a.kind === 'hub' && hypervisorHosts.has(a.id))
      const west = anchors.filter((a) => a.kind !== 'dist' && !farWest.includes(a))
      const eastItems = east.map((a) => ({ ...a, x: 0, y: 0 }))
      const farWestItems = farWest.map((a) => ({ ...a, x: 0, y: 0 }))
      const westItems = west.map((a) => ({ ...a, x: 0, y: 0 }))
      fanLayout(eastItems, node, ROUTER_FAN_RADIUS, GW_EAST_FAN[0], GW_EAST_FAN[1])
      fanLayout(farWestItems, node, HYPERVISOR_FAN_RADIUS, 160, 200)
      if (westItems.length >= GW_GRID_MIN) gridLayoutWest(westItems, node)
      else fanLayout(westItems, node, ROUTER_FAN_RADIUS, GW_WEST_FAN[0], GW_WEST_FAN[1])
      placed.push(...eastItems, ...farWestItems, ...westItems)
      fanArcByRouter.set(node.id, [...arcAround(0, 26), [GW_WEST_FAN[0] - 8, GW_WEST_FAN[1] + 8]])
    } else {
      const items = anchors.map((a) => ({ ...a, x: 0, y: 0 }))
      fanLayout(items, node, ROUTER_FAN_RADIUS, AP_WIRED_FAN[0], AP_WIRED_FAN[1])
      placed.push(...items)
      fanArcByRouter.set(node.id, [[AP_WIRED_FAN[0] - 8, AP_WIRED_FAN[1] + 8]])
    }
    for (const p of placed) {
      anchorPos.set(p.id, { x: p.x, y: p.y })
      if (p.kind === 'dist') distNodes.push({ kind: 'dist', id: p.ref.id, node: p.ref, x: p.x, y: p.y, r: 20 })
    }
  }

  const chips = []
  const mkChip = (d, hubId, isCt = false) => ({
    kind: 'chip', id: d.id, device: d, x: 0, y: 0,
    size: isWired(d) ? 26 : 24, wired: isWired(d), band: d.band,
    weak: d.signalDbm != null && d.signalDbm <= -65, hubId, isCt,
  })

  const ringRadii = new Map()
  for (const node of routerNodes) {
    const isGw = node.id === gatewayNode?.id
    const wifi = childrenOf(node.id).filter((d) => !isWired(d)).map((d) => mkChip(d, node.id))
    if (wifi.length === 0) continue
    const excludes = []
    if (isGw) {
      excludes.push(...arcAround(270, 14))
      if (gatewayNode) excludes.push(...arcAround(angleTo(gatewayNode.x, gatewayNode.y, gatewayNode.label.x, gatewayNode.label.y), 22))
      for (const ap of apNodes) excludes.push(...arcAround(angleTo(node.x, node.y, ap.x, ap.y), 15))
    } else {
      if (gatewayNode) excludes.push(...arcAround(angleTo(node.x, node.y, gatewayNode.x, gatewayNode.y), 27))
      excludes.push(...arcAround(angleTo(node.x, node.y, node.label.x, node.label.y), 22))
    }
    excludes.push(...(fanArcByRouter.get(node.id) || []))
    const baseRings = isGw ? GATEWAY_RINGS : AP_RINGS
    const rings = [...baseRings]
    let cap = rings.reduce((a, r) => a + r.cap, 0)
    while (cap < wifi.length) { const last = rings[rings.length - 1]; rings.push({ r: last.r + 30, cap: last.cap + 5 }); cap = rings.reduce((a, r) => a + r.cap, 0) }
    ringRadii.set(node.id, rings.map((r) => r.r))
    ringLayout(wifi, node, rings, excludes)
    wifi.forEach((c, i) => { if (i >= baseRings[0].cap) c.size = 22 })
    chips.push(...wifi)
  }

  for (const node of routerNodes) {
    for (const d of childrenOf(node.id).filter(isWired)) {
      if (deviceHubs.has(d.id)) continue
      const p = anchorPos.get(d.id); if (!p) continue
      const c = mkChip(d, node.id); Object.assign(c, p); chips.push(c)
    }
  }

  const hubChips = []
  for (const hubId of deviceHubs) {
    const hubDev = deviceById.get(hubId); if (!hubDev) continue
    const parentHub = hubOf(hubDev)
    const p = anchorPos.get(hubId); if (!p) continue
    const hc = mkChip(hubDev, parentHub); Object.assign(hc, p); hubChips.push(hc)
    const kids = hypervisorHosts.has(hubId) ? [] : childrenOf(hubId).map((d) => mkChip(d, hubId))
    const center = angleTo((routerById.get(parentHub) || p).x, (routerById.get(parentHub) || p).y, p.x, p.y)
    fanLayout(kids, p, HUB_FAN_RADIUS, center - 45, center + 45)
    chips.push(...kids)
  }
  chips.push(...hubChips)

  for (const dv of distNodes) {
    const kids = childrenOf(dv.id).map((d) => mkChip(d, dv.id))
    const rn = routerById.get(dv.node.routerId)
    const center = rn ? angleTo(rn.x, rn.y, dv.x, dv.y) : 0
    if (kids.length > DIST_FAN_MAX) {
      const excludes = rn ? arcAround(angleTo(dv.x, dv.y, rn.x, rn.y), 20) : []
      const rings = [...DIST_RINGS]
      let cap = rings.reduce((a, r) => a + r.cap, 0)
      while (cap < kids.length) { const last = rings[rings.length - 1]; rings.push({ r: last.r + 34, cap: last.cap + 5 }); cap = rings.reduce((a, r) => a + r.cap, 0) }
      ringLayout(kids, dv, rings, excludes)
    } else {
      fanLayout(kids, dv, DIST_FAN_RADIUS, center - 68, center + 68)
    }
    chips.push(...kids)
  }

  const ctsByHost = new Map(), ctCountByHost = new Map()
  for (const dn of distributionNodes.filter((n) => n.kind === 'hypervisor' && n.hostDeviceId)) {
    const hostId = dn.hostDeviceId
    const hostChip = chips.find((c) => c.id === hostId)
    const kids = childrenOf(hostId).map((d) => mkChip(d, hostId, true))
    if (!hostChip || kids.length === 0) continue
    kids.forEach((c, i) => { c.x = hostChip.x - ((Math.min(kids.length, CT_COLS) - 1) * CT_DX) / 2 + (i % CT_COLS) * CT_DX; c.y = hostChip.y + CT_OFFSET_Y + Math.floor(i / CT_COLS) * CT_DY; c.size = 22 })
    ctsByHost.set(hostId, kids); ctCountByHost.set(hostId, kids.length)
    chips.push(...kids)
  }

  const ringOverflowChips = []
  const activePeers = wireguard.peers.filter((p) => p.active)
  const peerNodes = activePeers.slice(0, PEER_COORDS.length).map((peer, i) => ({ kind: 'peer', id: `peer-${peer.id}`, peer, ...PEER_COORDS[i] }))
  const hiddenPeers = activePeers.slice(PEER_COORDS.length)

  const chipById = new Map(chips.map((c) => [c.id, c]))
  const hubPos = (hubId) => {
    const rn = routerById.get(hubId); if (rn) return { x: rn.x, y: rn.y, r: rn.r }
    const dv = distNodes.find((n) => n.id === hubId); if (dv) return { x: dv.x, y: dv.y, r: dv.r }
    const c = chipById.get(hubId); if (c) return { x: c.x, y: c.y, r: c.size / 2 }
    return null
  }

  const links = []
  if (gatewayNode) {
    links.push({
      id: 'wan', kind: 'wan',
      d: 'M 500 92 C 500 122, 500 162, 500 208',
      lx: 518, ly: 162, label: `Fibra ${wan.plan} · ${wan.latencyMs} ms`,
      width: 3, ...flowFor(600), from: 'internet', to: gatewayNode.id,
    })
  }
  apNodes.forEach((node, i) => {
    const p = UPLINK_PATHS[i]
    const isWifi = node.router.backhaul === 'wifi'
    const traffic = subtreeTraffic(childrenOf, node.id)
    const label = isWifi ? 'WiFi uplink' : `Cable 1G${node.router.lldp ? ' · LLDP' : ''}`
    links.push({
      id: `uplink-${node.id}`, kind: 'uplink', wifi: isWifi,
      d: p.d, lx: p.lx, ly: p.ly, label,
      width: isWifi ? 2 : 3, ...flowFor(Math.max(traffic, isWifi ? 40 : 120)),
      from: gatewayNode?.id || 'internet', to: node.id,
    })
  })
  for (const dv of distNodes) {
    const rn = routerById.get(dv.node.routerId); if (!rn) continue
    const edge = pos(rn.x, rn.y, angleTo(rn.x, rn.y, dv.x, dv.y), rn.r + 2)
    const mid = { x: (edge.x + dv.x) / 2, y: (edge.y + dv.y) / 2 }
    links.push({
      id: `dist-${dv.id}`, kind: 'dist',
      d: `M ${edge.x} ${edge.y} Q ${mid.x + 6} ${mid.y - 6}, ${dv.x} ${dv.y}`,
      lx: 0, ly: 0, label: '',
      width: 2.5, ...flowFor(subtreeTraffic(childrenOf, dv.id), true),
      from: dv.node.routerId, to: dv.id,
    })
  }
  for (const chip of chips) {
    if (chip.isCt || !chip.wired) continue
    const hub = hubPos(chip.hubId); if (!hub) continue
    const a = angleTo(hub.x, hub.y, chip.x, chip.y)
    const edge = pos(hub.x, hub.y, a, hub.r + 2)
    const dx = chip.x - edge.x, dy = chip.y - edge.y
    const c1x = edge.x + dx * 0.5 - dy * 0.1
    const c1y = edge.y + dy * 0.5 + dx * 0.1
    const mbps = deviceHubs.has(chip.id) ? chip.device.trafficMbps + subtreeTraffic(childrenOf, chip.id) : chip.device.trafficMbps
    links.push({
      id: `wired-${chip.id}`, kind: 'wired',
      d: `M ${edge.x} ${edge.y} Q ${c1x} ${c1y}, ${chip.x} ${chip.y}`,
      lx: 0, ly: 0, label: '',
      width: 1.4, ...flowFor(mbps, true),
      from: chip.hubId, to: chip.id,
    })
  }
  for (const [hostId, cts] of ctsByHost) {
    const hostChip = chipById.get(hostId); if (!hostChip) continue
    for (const ct of cts) {
      links.push({
        id: `wired-${ct.id}`, kind: 'wired',
        d: `M ${hostChip.x} ${hostChip.y + hostChip.size / 2} L ${ct.x} ${ct.y - ct.size / 2}`,
        lx: 0, ly: 0, label: '',
        width: 1.2, ...flowFor(ct.device.trafficMbps, true),
        from: hostId, to: ct.id,
      })
    }
  }
  peerNodes.forEach((node, i) => {
    const p = WG_PATHS[i] || { d: '', dur: 3.2 }
    const d = p.d || `M ${node.x + 7} ${node.y + 18} C ${node.x + 60} ${node.y + 56}, ${internetNode.x - 40} ${internetNode.y + 36}, ${internetNode.x - 26} ${internetNode.y + 22}`
    links.push({
      id: `wg-${node.peer.id}`, kind: 'wg',
      d, lx: 0, ly: 0, label: '',
      width: 2, packets: 1, packetDur: p.dur,
      from: node.id, to: 'internet',
    })
  })

  return {
    gatewayNode, apNodes, routerNodes, internetNode, peerNodes, hiddenPeers,
    chips, distNodes, ctsByHost, ctCountByHost, ringRadii, ringOverflowChips, links,
    wan,
  }
}

/* ===== iconos SVG inline ===== */
function drawIcon(type, size, parent) {
  const s = size || 14
  const half = s / 2
  const scale = s / 24
  const mkp = (d) => {
    const el = document.createElementNS('http://www.w3.org/2000/svg', 'path')
    el.setAttribute('d', d)
    el.setAttribute('transform', `translate(${-half},${-half}) scale(${scale})`)
    el.setAttribute('fill', 'none')
    el.setAttribute('stroke', 'currentColor')
    el.setAttribute('stroke-width', '1.75')
    el.setAttribute('stroke-linecap', 'round')
    el.setAttribute('stroke-linejoin', 'round')
    parent.appendChild(el)
    return el
  }
  const map = {
    ordenador: 'M3 4h18v12H3z M8 16v4 M16 16v4 M6 20h12',
    portatil: 'M4 5h16v10H4z M2 17h20v2H2z',
    movil: 'M7 2h10a2 2 0 0 1 2 2v16a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2z',
    tablet: 'M5 2h14a2 2 0 0 1 2 2v16a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2z',
    tv: 'M2 7h20v12H2z M8 3l4 4 4-4',
    consola: 'M6 11h2v2H6zm10 0h2v2h-2zm-6 2h4v-4h-4z M4 7h16a2 2 0 0 1 2 2v6a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V9a2 2 0 0 1 2-2z',
    iot: 'M12 2a6 6 0 0 0-6 6c0 2.22 1.21 4.16 3 5.2V19a3 3 0 0 0 6 0v-5.8a6 6 0 0 0 0-10.4z',
    camara: 'M23 19a2 2 0 0 1-2 2H3a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h4l2-3h6l2 3h4a2 2 0 0 1 2 2z M12 17a4 4 0 1 0 0-8 4 4 0 0 0 0 8z',
    altavoz: 'M18 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V4a2 2 0 0 0-2-2z M12 18a4 4 0 0 0 0-8',
    servidor: 'M6 2h12a2 2 0 0 1 2 2v16a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2z M6 7h12 M6 12h12 M6 17h12',
    switch: 'M4 10h16v8H4z M8 10V6 M16 10V6',
    desconocido: 'M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20z M9 9a3 3 0 1 1 6 0c0 2-3 3-3 5',
  }
  mkp(map[type] || map.desconocido)
}

/* ===== render SVG fiel ===== */
function drawTopology() {
  const model = buildTopologyModel({
    routers: DEMO_ROUTERS, devices: DEMO_DEVICES, wan: DEMO_WAN,
    wireguard: DEMO_WG, distributionNodes: DEMO_DISTRIBUTION,
  })
  const svg = document.getElementById('topoSvg')
  svg.innerHTML = ''
  const ns = 'http://www.w3.org/2000/svg'
  const mk = (tag, attrs = {}, parent = svg) => {
    const el = document.createElementNS(ns, tag)
    for (const [k, v] of Object.entries(attrs)) if (v !== undefined && v !== null) el.setAttribute(k, v)
    if (parent) parent.appendChild(el)
    return el
  }
  // Lección de la app (bug chips invisibles): nunca atributo transform y
  // animación CSS de scale en el mismo <g> — el transform CSS machaca el
  // translate del atributo y los nodos colapsan en (0,0). El translate va en
  // el <g> padre y la animación (opacity/scale con transform-box) en un hijo.
  const nodeGroup = (x, y) => {
    const outer = mk('g', { transform: `translate(${x} ${y})` })
    return mk('g', { class: 'topo-node on' }, outer)
  }

  // defs
  const defs = mk('defs')
  const grad = mk('linearGradient', { id: 'topo-gw-grad', x1: 0, y1: 0, x2: 1, y2: 1 }, defs)
  mk('stop', { offset: '0%', 'stop-color': TOPO_COLOR.accent, 'stop-opacity': 0.28 }, grad)
  mk('stop', { offset: '100%', 'stop-color': TOPO_COLOR.tunnel, 'stop-opacity': 0.28 }, grad)

  // anillos guia wifi
  const guidesG = mk('g', { 'class': 'topo-guides', 'aria-hidden': 'true' })
  for (const node of model.routerNodes) {
    const radii = model.ringRadii.get(node.id) || (node.id === model.gatewayNode?.id ? [96, 130] : [82, 120])
    for (const r of radii) mk('circle', { cx: node.x, cy: node.y, r, class: 'topo-guide' }, guidesG)
  }

  // enlaces
  const linksG = mk('g')
  for (const link of model.links) {
    const isWg = link.kind === 'wg'
    const isWifiUp = link.kind === 'uplink' && link.wifi
    const stroke = linkColor(link.kind, link.wifi)
    const baseOpacity = link.kind === 'wired' ? 0.45 : link.kind === 'dist' ? 0.55 : isWifiUp ? 0.8 : link.kind === 'uplink' ? 0.55 : 0.9
    const path = mk('path', { d: link.d, fill: 'none', stroke, 'stroke-width': link.width, 'stroke-linecap': 'round',
      'stroke-dasharray': isWg ? '7 7' : isWifiUp ? '8 6' : undefined, opacity: baseOpacity,
      id: `topo-link-${link.id}`, class: 'topo-edge on' }, linksG)
    if (isWg) {
      mk('path', { d: link.d, fill: 'none', stroke: TOPO_COLOR.tunnel, 'stroke-width': link.width, 'stroke-linecap': 'round',
        'stroke-dasharray': '7 7', opacity: 0.9, class: 'topo-wg-flow' }, linksG)
    }
    // paquetes animados via CSS offset-path
    if (link.packets > 0) {
      for (let i = 0; i < link.packets; i++) {
        const begin = -((link.packetDur / link.packets) * i)
        mk('circle', {
          r: 2.6, fill: stroke, opacity: 0.95, class: 'topo-packet',
          style: `offset-path: path('${link.d.replace(/'/g, "\\'")}'); animation: packetMove ${link.packetDur}s linear infinite; animation-delay: ${begin}s`,
        }, linksG)
      }
    }
  }

  // internet
  const inetG = nodeGroup(model.internetNode.x, model.internetNode.y)
  mk('circle', { r: 42, fill: TOPO_COLOR.ok, opacity: 0.08 }, inetG)
  const orbit = mk('g', { 'aria-hidden': 'true', class: 'topo-orbit' }, inetG)
  mk('circle', { r: 38, fill: 'none', stroke: TOPO_COLOR.ok, 'stroke-width': 1.2, 'stroke-dasharray': '3 7', opacity: 0.55 }, orbit)
  mk('circle', { r: 3, fill: TOPO_COLOR.ok, cx: 0, cy: -38 }, orbit)
  mk('circle', { r: 28, fill: 'rgb(var(--elevated))', stroke: TOPO_COLOR.ok, 'stroke-width': 2 }, inetG)
  mk('path', { d: 'M19.3 11.7a5.6 5.6 0 0 0-8.2-3.5A4.5 4.5 0 0 0 4.5 12.5 4.5 4.5 0 0 0 9 17h11a3.5 3.5 0 0 0 0-7h-1.2l-1.5-1.3z',
    fill: 'none', stroke: TOPO_COLOR.ok, 'stroke-width': 1.75, 'stroke-linecap': 'round', 'stroke-linejoin': 'round' }, inetG)

  // routers
  model.routerNodes.forEach((node, i) => {
    const isGw = node.router.roleBadge === 'Principal'
    const isWarn = node.router.status === 'warn'
    const g = nodeGroup(node.x, node.y)
    if (isGw) mk('circle', { r: node.r + 18, fill: TOPO_COLOR.accent, opacity: 0.1, class: 'topo-halo' }, g)
    if (isWarn) {
      mk('circle', { r: node.r + 8, fill: 'none', stroke: TOPO_COLOR.warn, 'stroke-width': 2, class: 'topo-warn-pulse' }, g)
    }
    mk('circle', { r: node.r, fill: isGw ? 'url(#topo-gw-grad)' : 'rgb(var(--elevated))',
      stroke: isWarn ? TOPO_COLOR.warn : isGw ? TOPO_COLOR.accent : 'rgb(var(--border-strong))', 'stroke-width': isGw || isWarn ? 2 : 1.5 }, g)
    // status ring
    const R = node.r + 6, C = 2 * Math.PI * R
    const target = C * (1 - node.router.health / 100)
    const ring = mk('circle', { r: R, fill: 'none', stroke: statusColor(node.router.status), 'stroke-width': 3,
      'stroke-dasharray': C, 'stroke-dashoffset': target, 'stroke-linecap': 'round', transform: 'rotate(-90)' }, g)
    // router icon
    mk('path', { d: 'M4 10h16v8H4z M8 10V6 M16 10V6 M12 2v4', fill: 'none', stroke: isWarn ? TOPO_COLOR.warn : TOPO_COLOR.accent,
      'stroke-width': 1.75, 'stroke-linecap': 'round', 'stroke-linejoin': 'round' }, g)
  })

  // peers
  model.peerNodes.forEach((node, i) => {
    const isMovil = node.peer.type === 'movil'
    const g = nodeGroup(node.x, node.y)
    const pulse = mk('circle', { r: 22, fill: 'none', stroke: TOPO_COLOR.tunnel, 'stroke-width': 1.5, class: 'topo-peer-pulse' }, g)
    mk('rect', { x: -18, y: -18, width: 36, height: 36, rx: 11, fill: 'rgb(var(--elevated))', stroke: TOPO_COLOR.tunnel, 'stroke-width': 1.5 }, g)
    const path = isMovil
      ? 'M7 2h10a2 2 0 0 1 2 2v16a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2z'
      : 'M4 5h16v10H4z M2 17h20v2H2z'
    mk('path', { d: path, fill: 'none', stroke: TOPO_COLOR.tunnel, 'stroke-width': 1.75, 'stroke-linecap': 'round', 'stroke-linejoin': 'round' }, g)
  })

  // hidden peers +N
  if (model.hiddenPeers.length > 0) {
    const g = nodeGroup(PEERS_OVERFLOW_COORD.x, PEERS_OVERFLOW_COORD.y)
    mk('rect', { x: -15, y: -11, width: 30, height: 22, rx: 8, fill: 'rgb(var(--elevated))', stroke: TOPO_COLOR.tunnel, 'stroke-width': 1.5 }, g)
    mk('text', { x: 0, y: 3.5, 'text-anchor': 'middle', 'font-size': 9.5, 'font-weight': 700, fill: TOPO_COLOR.tunnel }, g).textContent = '+' + model.hiddenPeers.length
  }

  // distnodes
  model.distNodes.forEach((dv) => {
    const managed = dv.node.kind === 'managed'
    const g = nodeGroup(dv.x, dv.y)
    mk('circle', { r: dv.r + 5, fill: 'none', stroke: managed ? TOPO_COLOR.accent : 'rgb(var(--text-muted))', 'stroke-width': 1,
      'stroke-dasharray': managed ? undefined : '2 5', opacity: managed ? 0.5 : 0.6 }, g)
    mk('circle', { r: dv.r, fill: managed ? 'rgb(var(--elevated))' : 'rgb(var(--elevated) / 0.65)',
      stroke: managed ? TOPO_COLOR.accent : 'rgb(var(--text-muted))', 'stroke-width': 1.5, 'stroke-dasharray': managed ? undefined : '4 4' }, g)
    mk('path', { d: 'M4 10h16v8H4z M8 10V6 M16 10V6', fill: 'none', stroke: managed ? TOPO_COLOR.accent : 'rgb(var(--text-secondary))', 'stroke-width': 1.75 }, g)
    if (managed) {
      mk('rect', { x: dv.r - 4, y: -dv.r - 4, width: 22, height: 10, rx: 5, fill: 'rgb(var(--elevated))', stroke: TOPO_COLOR.accent, 'stroke-width': 1 }, g)
      mk('text', { x: dv.r + 7, y: -dv.r + 3.4, 'text-anchor': 'middle', 'font-size': 6.5, 'font-weight': 800, fill: TOPO_COLOR.accent, 'letter-spacing': '0.04em' }, g).textContent = 'LLDP'
    }
  })

  // chips
  model.chips.forEach((chip) => {
    const d = chip.device, S = chip.size, half = S / 2
    const stroke = d.lldp ? TOPO_COLOR.accent : chip.wired ? TOPO_COLOR.ok : 'rgb(var(--border-strong))'
    const g = nodeGroup(chip.x, chip.y)
    const rect = mk('rect', { x: -half, y: -half, width: S, height: S, rx: 7, fill: 'rgb(var(--elevated))', stroke, 'stroke-width': chip.wired ? 1.3 : 1.1 }, g)
    const iconG = mk('g', { style: `color: ${chip.wired ? TOPO_COLOR.ok : 'rgb(var(--text-primary))'}` }, g)
    drawIcon(d.type, 14, iconG)
    if (!chip.wired) {
      mk('circle', { cx: half - 2, cy: half - 2, r: 3.8, fill: bandColor(chip.band, chip.weak), stroke: 'rgb(var(--canvas))', 'stroke-width': 1.4 }, g)
    }
    if (d.lldp) {
      mk('rect', { x: half - 4, y: -half - 4, width: 22, height: 10, rx: 5, fill: 'rgb(var(--elevated))', stroke: TOPO_COLOR.accent, 'stroke-width': 1 }, g)
      mk('text', { x: half + 7, y: -half + 3.4, 'text-anchor': 'middle', 'font-size': 6.5, 'font-weight': 800, fill: TOPO_COLOR.accent, 'letter-spacing': '0.04em' }, g).textContent = 'LLDP'
    }
    const ctCount = model.ctCountByHost.get(chip.id) || 0
    if (ctCount > 0) {
      mk('circle', { cx: half + 1, cy: -half - 1, r: 8, fill: 'rgb(var(--elevated))', stroke: TOPO_COLOR.ok, 'stroke-width': 1.2 }, g)
      mk('text', { x: half + 1, y: -half + 2, 'text-anchor': 'middle', 'font-size': 8, 'font-weight': 700, fill: TOPO_COLOR.ok }, g).textContent = '+' + ctCount
    }
    mk('title', {}, g).textContent = tName(d.name)
  })

  // etiquetas
  const labelsG = mk('g', { class: 'topo-labels', 'pointer-events': 'none', 'aria-hidden': 'true' })
  function label(x, y, anchor, title, sub, subColor) {
    const g = mk('g', {}, labelsG)
    mk('text', { x, y, 'text-anchor': anchor, 'font-size': 13, 'font-weight': 600, fill: 'rgb(var(--text-primary))', stroke: 'rgb(var(--canvas))', 'stroke-width': 4, style: 'paint-order: stroke' }, g).textContent = title
    mk('text', { x, y: y + 15, 'text-anchor': anchor, 'font-size': 10.5, fill: subColor || 'rgb(var(--text-secondary))', stroke: 'rgb(var(--canvas))', 'stroke-width': 3, style: 'paint-order: stroke' }, g).textContent = sub
  }
  const linkLabelText = (l) => {
    if (l.kind === 'wan') return t('topo.fiber').replace('{plan}', model.wan.plan).replace('{latency}', model.wan.latencyMs)
    if (l.kind === 'uplink') return l.wifi ? t('topo.wifiUplink') : t('topo.cable1g') + (l.label.includes('LLDP') ? ' · LLDP' : '')
    return ''
  }
  label(548, 54, 'start', `${t('topo.inet')} · ${model.wan.isp}`, `${model.wan.plan} · ${model.wan.latencyMs} ms`)
  if (model.gatewayNode) {
    const n = model.gatewayNode
    label(n.label.x, n.label.y, n.label.anchor, tName(n.router.name), `${n.router.modelShort} · ${n.router.roleBadge} · ${n.router.clients} ${t('topo.clients')}`)
  }
  model.apNodes.forEach((n) => {
    const warn = n.router.status === 'warn'
    label(n.label.x, n.label.y, n.label.anchor, tName(n.router.name), warn ? `${n.router.clients} ${t('topo.clients')} · ${t('routers.warn')}` : `${n.router.clients} ${t('topo.clients')}`, warn ? TOPO_COLOR.warn : undefined)
  })
  model.distNodes.forEach((dv) => {
    if (dv.node.kind === 'managed') label(dv.x, dv.y - 34, 'middle', dv.node.name ? tName(dv.node.name) : t('topo.managed'), `${dv.node.ip || 'LLDP'} · ${dv.node.port}`, TOPO_COLOR.accent)
    else label(dv.x, dv.y - 34, 'middle', t('topo.inferredSwitch'), `${dv.node.port}`)
  })
  for (const [hostId] of model.ctsByHost) {
    const host = model.chips.find((c) => c.id === hostId); if (!host) continue
    label(host.x, host.y - 38, 'middle', tName(host.device.name), `${t('topo.hypervisor')} · ${model.ctCountByHost.get(hostId) || 0} CT`)
  }
  model.peerNodes.forEach((n, i) => label(n.x + (i % 2 ? -26 : 26), n.y - 4, i % 2 ? 'end' : 'start', tName(n.peer.name), t('topo.viaInternet'), TOPO_COLOR.tunnel))
  model.links.filter((l) => l.label).forEach((l) => {
    mk('text', { x: l.lx, y: l.ly, 'font-size': 10, fill: 'rgb(var(--text-muted))', stroke: 'rgb(var(--canvas))', 'stroke-width': 3, style: 'paint-order: stroke', class: 'font-mono' }, labelsG).textContent = linkLabelText(l)
  })
}

function buildTopo() {
  if (topoBuilt) return
  topoBuilt = true
  drawTopology()
}

function renderTopoNames() {
  if (topoBuilt) drawTopology()
}

function buildRouters() {
  const grid = document.getElementById('routerGrid')
  const C = 2 * Math.PI * 25 // r=25 para que el anillo (stroke 5) quepa en el viewBox 56x56 y no se recorte
  grid.innerHTML = DEMO_ROUTERS.map(r => {
    const isWarn = r.status === 'warn'
    const statusColor = isWarn ? 'rgb(var(--warn))' : 'rgb(var(--ok))'
    const healthDash = C * (1 - r.health / 100)
    const sparkPath = r.sparkline.map((v, i) => `${i === 0 ? 'M' : 'L'}${(i / (r.sparkline.length - 1)) * 100} ${28 - (v / 70) * 28}`).join(' ')
    return `
    <div class="rcard card ${isWarn ? 'warn' : ''}">
      <div class="rt">
        <div class="nameblock">
          <div class="iconbox">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M4 14h16v5a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-5z"/><path d="M8 6h8"/><path d="M6 9h12"/><path d="M10 3h4"/></svg>
          </div>
          <div>
            <span class="name">${tName(r.name)}</span>
            <span class="model">${r.model}</span>
            <span class="rolepill">${t(r.roleBadge === 'Principal' ? 'routers.roleGW' : 'routers.roleAP')}</span>
          </div>
        </div>
        <div class="ring-wrap" style="width:56px;height:56px;">
          <svg width="56" height="56" viewBox="0 0 56 56" style="transform:rotate(-90deg);">
            <circle cx="28" cy="28" r="25" fill="none" stroke="rgb(var(--border))" stroke-width="5"/>
            <circle class="router-health-ring" cx="28" cy="28" r="25" fill="none" stroke="${statusColor}" stroke-width="5" stroke-linecap="round"
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

/* ---------- stage 4: adguard (como la tarjeta de la app) ---------- */
let agPlayed = false
const AG_DONUT_C = 248.2
function renderAdGuardTexts() {
  const ag = DEMO_ADGUARD
  const set = (id, v) => { const el = document.getElementById(id); if (el) el.textContent = v }
  set('agBlockedPct', fmtLocale(ag.blockedPct, 1) + '%')
  set('agBlockedOf', t('adguard.blockedOf', { blocked: fmtLocale(ag.blocked24h), total: fmtLocale(ag.queries24h) }))
  set('agTrackers', fmtLocale(ag.trackersBlocked))
  set('agDns', fmtLocale(ag.dnsLatencyMs) + ' ms')
}
function playAdGuard() {
  renderAdGuardTexts()
  const ag = DEMO_ADGUARD
  const arc = document.getElementById('agDonutArc')
  if (agPlayed) return
  agPlayed = true
  // animación del arco del donut
  if (reduceMotion) {
    arc.style.strokeDashoffset = AG_DONUT_C * (1 - ag.blockedPct / 100)
  } else {
    const start = performance.now(), dur = 1100
    function frame(now) {
      const p = Math.min(1, (now - start) / dur)
      const e = easeOutCubic(p)
      arc.style.strokeDashoffset = AG_DONUT_C * (1 - (ag.blockedPct / 100) * e)
      if (p < 1) requestAnimationFrame(frame)
    }
    requestAnimationFrame(frame)
  }
  // barras de dominios bloqueados
  const bars = document.getElementById('agBars')
  if (!bars.childElementCount) {
    const max = ag.topBlocked[0].count
    bars.innerHTML = ag.topBlocked.map((x) => `
      <div class="ag-bar-row">
        <span class="d">${x.domain}</span>
        <div class="bar"><i data-w="${Math.round((x.count / max) * 100)}"></i></div>
        <span class="n">${fmtLocale(x.count)}</span>
      </div>
    `).join('')
  }
  bars.querySelectorAll('.bar i').forEach((bar, i) => {
    const w = bar.dataset.w
    if (reduceMotion) { bar.style.width = w + '%'; return }
    setTimeout(() => { bar.style.width = w + '%' }, 200 + i * 120)
  })
}

/* ---------- stage 6: agente OpenWrt (terminal) ---------- */
let agentPlayed = false
function playAgent() {
  if (agentPlayed) return
  agentPlayed = true
  const lines = document.querySelectorAll('#stage6 .term-line')
  if (!lines.length) return
  if (reduceMotion) { lines.forEach(l => l.style.opacity = '1'); return }
  lines.forEach((l, i) => {
    l.style.opacity = '0'
    setTimeout(() => { l.style.opacity = '1'; l.style.transition = 'opacity 180ms ease' }, 200 + i * 160)
  })
}
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
const STAGE_PLAYERS = [playRing, playTraffic, buildTopo, playRouters, playAdGuard, playAlerts, playAgent]
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
    copyText(cmd, btn)
  })
  const agentBtn = document.getElementById('agentCopyBtn')
  if (agentBtn) {
    agentBtn.addEventListener('click', async () => {
      const cmd = document.getElementById('agentCmd').textContent
      copyText(cmd, agentBtn)
    })
  }
}

async function copyText(text, btn) {
  try { await navigator.clipboard.writeText(text) } catch {
    const ta = document.createElement('textarea'); ta.value = text; document.body.appendChild(ta); ta.select(); document.execCommand('copy'); ta.remove()
  }
  const orig = t('misc.copy')
  btn.textContent = t('misc.copied')
  setTimeout(() => { btn.textContent = orig }, 1600)
}

/* ---------- ko-fi placeholder ---------- */
function initKofi() {
  // TODO(nacho): URL definitiva de Ko-fi — pendiente de que la cree.
  const url = 'https://github.com/gnacho/netpulse'
  document.querySelectorAll('.kofi').forEach((a) => { a.href = url })
}

/* ---------- boot ---------- */
function initTrafficRanges() {
  const wrap = document.getElementById('trafficRanges')
  if (!wrap) return
  wrap.addEventListener('click', (e) => {
    const btn = e.target.closest('button[data-range]')
    if (btn) setTrafficRange(btn.dataset.range)
  })
}

document.addEventListener('DOMContentLoaded', () => {
  initAppearance()
  initBackgroundCanvas()
  renderTicker()
  renderAlerts()
  initReveals()
  initTheater()
  initCopy()
  initKofi()
  initTrafficRanges()
  heroCountUps()
})
