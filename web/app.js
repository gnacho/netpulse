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

/* ---------- stage 2: topologia fiel a la app (model.ts + TopologyMap.tsx) ---------- */
let topoBuilt = false
let routersBuilt = false
let routersPlayed = false

/* ===== datos demo canonicos ===== */
const DEMO_WAN = {
  plan: '600/600', downMbps: 84.2, upMbps: 12.6, latencyMs: 8, lossPct: 0.1,
  publicIp: '185.75.x.x', isp: 'Digi', peakTodayMbps: 412, peakTodayTime: '21:14',
  avgDownMbps: 61, total24h: '1.32',
}
const DEMO_WG = {
  interface: 'wg0', subnet: '10.200.0.0/24', status: 'active',
  peers: [
    { id: 'phone', name: 'Pixel 8 Pro', type: 'movil', tunnelIp: '10.200.0.2', active: true, lastHandshake: '38s', rx: '1.2 GB', tx: '340 MB' },
    { id: 'laptop', name: 'MacBook Air', type: 'portatil', tunnelIp: '10.200.0.3', active: true, lastHandshake: '2m', rx: '840 MB', tx: '120 MB' },
    { id: 'nas', name: 'NAS Remoto', type: 'sitio', tunnelIp: '10.200.0.4', active: true, lastHandshake: '5m', rx: '4.1 GB', tx: '1.8 GB' },
    { id: 'tablet', name: 'iPad', type: 'tablet', tunnelIp: '10.200.0.5', active: true, lastHandshake: '12m', rx: '210 MB', tx: '45 MB' },
    { id: 'otro', name: 'Work laptop', type: 'portatil', tunnelIp: '10.200.0.6', active: true, lastHandshake: '1h', rx: '90 MB', tx: '12 MB' },
  ],
}
const DEMO_DISTRIBUTION = [
  { id: 'dist-lan3', kind: 'inferred', routerId: 'flint2', port: 'lan3', macCount: 8 },
  { id: 'dist-lan4', kind: 'managed', routerId: 'flint2', port: 'lan4', macCount: 14, name: 'Switch 8P', ip: '192.168.1.10', lldp: { chassis: 'Switch 8P', mgmt: '192.168.1.10', caps: 'B', portDesc: 'Port 7' } },
  { id: 'dist-pve', kind: 'hypervisor', routerId: 'flint2', port: 'lan2', macCount: 6, hostDeviceId: 'pve' },
  { id: 'dist-salon', kind: 'inferred', routerId: 'salon', port: 'lan1', macCount: 5 },
]

const DEMO_DEVICES = (() => {
  const types = ['ordenador', 'tv', 'movil', 'portatil', 'consola', 'iot', 'camara', 'altavoz', 'servidor', 'tablet', 'switch', 'desconocido']
  const names = {
    ordenador: ['pc-salon', 'pc-estudio', 'pc-dormitorio', 'imac', 'ryzen-ai'],
    tv: ['tv-salon', 'tv-dormitorio', 'chromecast-salon'],
    movil: ['Pixel-8-Pro', 'iPhone-15', 'Galaxy-S24', 'Xiaomi-14'],
    portatil: ['MacBook-Air', 'ThinkPad', 'Framework', 'macbook-maria'],
    consola: ['PS5', 'Steam-Deck', 'Switch'],
    iot: ['termo', 'sensor-puerta', 'enchufe-jardin', 'aqara-humo'],
    camara: ['camara-patio', 'camara-entrada', 'gato-cam'],
    altavoz: ['sonos-salon', 'echo-dot', 'homepod-mini'],
    servidor: ['pve', 'nas', 'docker-host', 'homeassistant'],
    tablet: ['iPad', 'Tab-S9'],
    switch: ['switch-8p', 'switch-cuarto'],
    desconocido: ['?-01', '?-02'],
  }
  const devices = []
  let id = 0
  function add(name, type, band, opts = {}) {
    const signal = band === 'cable' ? null : band === '5 GHz' ? -52 : -67
    devices.push({
      id: 'd' + (++id), name, type, manufacturer: opts.manufacturer || 'NetPulse', ip: `192.168.1.${100 + id}`,
      mac: `a8:5e:45:${(id).toString(16).padStart(2, '0')}:00:00`, routerId: opts.routerId || 'flint2', band,
      signalDbm: signal, trafficMbps: Math.random() * 35 + 0.5, online: true, sparkline: [],
      port: band === 'cable' ? opts.port : null, attachTo: opts.attachTo, infra: opts.infra, lldp: opts.lldp,
    })
  }
  // cableados directos del gateway
  add('pc-salon', 'ordenador', 'cable', { port: 'lan1' })
  add('tv-salon', 'tv', 'cable', { port: 'lan1' })
  add('nas', 'servidor', 'cable', { port: 'lan1' })
  add('pve', 'servidor', 'cable', { port: 'lan2', infra: 'hypervisor' })
  add('docker-host', 'servidor', 'cable', { port: 'lan3', attachTo: 'dist-lan3' })
  add('homeassistant', 'servidor', 'cable', { port: 'lan3', attachTo: 'dist-lan3' })
  add('switch-8p', 'switch', 'cable', { port: 'lan4', attachTo: 'dist-lan4', lldp: { chassis: 'Switch 8P' } })
  add('printer', 'iot', 'cable', { port: 'lan4', attachTo: 'dist-lan4' })
  // CTs bajo pve
  for (let i = 1; i <= 5; i++) add(`ct-${i}`, 'servidor', 'cable', { attachTo: 'pve', infra: 'ct' })
  // hijos del switch gestionado
  add('pc-estudio', 'ordenador', 'cable', { attachTo: 'switch-8p' })
  add('camara-entrada', 'camara', 'cable', { attachTo: 'switch-8p' })
  // wifi gateway
  for (let i = 0; i < 9; i++) add(`gw-wifi-${i}`, ['movil', 'portatil', 'tablet', 'iot'][i % 4], i % 2 ? '5 GHz' : '2.4 GHz', { routerId: 'flint2' })
  // salon wifi
  for (let i = 0; i < 11; i++) add(`salon-wifi-${i}`, ['movil', 'tv', 'consola', 'altavoz', 'iot'][i % 5], i % 3 ? '5 GHz' : '2.4 GHz', { routerId: 'salon' })
  // pasillo wifi
  for (let i = 0; i < 7; i++) add(`pasillo-wifi-${i}`, ['movil', 'tablet', 'iot', 'camara'][i % 4], '2.4 GHz', { routerId: 'pasillo' })
  // dormitorio wifi
  for (let i = 0; i < 9; i++) add(`dorm-wifi-${i}`, ['movil', 'portatil', 'altavoz', 'iot'][i % 4], '5 GHz', { routerId: 'dormitorio' })
  // cableados salon
  add('chromecast-salon', 'tv', 'cable', { routerId: 'salon', port: 'lan1', attachTo: 'dist-salon' })
  add('ap-salon-uplink', 'switch', 'cable', { routerId: 'salon', port: 'lan2' })
  // algunos weak
  devices[devices.length - 3].signalDbm = -71
  devices[devices.length - 6].signalDbm = -69
  return devices
})()

const DEMO_ROUTERS = [
  { id: 'flint2', name: 'Gateway', model: 'GL.iNet Flint 2 (GL-MT6000)', modelShort: 'GL.iNet Flint 2', role: 'Principal', roleBadge: 'Principal', ip: '192.168.1.1', status: 'online', health: 92, cpu: 18, ram: 34, temp: 48, uptime: '14d 3h', clients: 42, backhaul: 'cable', sparkline: [40,55,80,65,90,120,110,130,95,85,70,60,50,45,55,80,120,200,350,412,280,180,90,70] },
  { id: 'salon', name: 'Salón', model: 'Xiaomi AX6', modelShort: 'Xiaomi AX6', role: 'AP', roleBadge: 'AP', ip: '192.168.1.2', status: 'online', health: 88, cpu: 22, ram: 41, temp: 54, uptime: '10d 8h', clients: 18, backhaul: 'cable', sparkline: [20,30,40,35,50,60,55,45,40,35,30,25,30,45,60,70,80,75,60,50,40,30,25,20] },
  { id: 'pasillo', name: 'Patio', model: 'Xiaomi AX6', modelShort: 'Xiaomi AX6', role: 'AP', roleBadge: 'AP', ip: '192.168.1.3', status: 'warn', health: 71, cpu: 45, ram: 62, temp: 78, uptime: '6d 12h', clients: 10, hotMetric: 'temp', backhaul: 'wifi', sparkline: [10,15,20,18,25,30,28,22,20,18,15,12,18,25,35,40,38,30,25,20,18,15,12,10] },
  { id: 'dormitorio', name: 'Dormitorio', model: 'Xiaomi AX6', modelShort: 'Xiaomi AX6', role: 'AP', roleBadge: 'AP', ip: '192.168.1.4', status: 'online', health: 90, cpu: 19, ram: 38, temp: 52, uptime: '9d 4h', clients: 12, backhaul: 'cable', sparkline: [15,20,25,30,28,25,20,18,22,30,35,40,38,35,30,25,20,18,15,20,25,30,28,22] },
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
  const inetG = mk('g', { transform: `translate(${model.internetNode.x} ${model.internetNode.y})`, class: 'topo-node on' })
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
    const g = mk('g', { transform: `translate(${node.x} ${node.y})`, class: 'topo-node on' })
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
    const g = mk('g', { transform: `translate(${node.x} ${node.y})`, class: 'topo-node on' })
    const pulse = mk('circle', { r: 22, fill: 'none', stroke: TOPO_COLOR.tunnel, 'stroke-width': 1.5, class: 'topo-peer-pulse' }, g)
    mk('rect', { x: -18, y: -18, width: 36, height: 36, rx: 11, fill: 'rgb(var(--elevated))', stroke: TOPO_COLOR.tunnel, 'stroke-width': 1.5 }, g)
    const path = isMovil
      ? 'M7 2h10a2 2 0 0 1 2 2v16a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2z'
      : 'M4 5h16v10H4z M2 17h20v2H2z'
    mk('path', { d: path, fill: 'none', stroke: TOPO_COLOR.tunnel, 'stroke-width': 1.75, 'stroke-linecap': 'round', 'stroke-linejoin': 'round' }, g)
  })

  // hidden peers +N
  if (model.hiddenPeers.length > 0) {
    const g = mk('g', { transform: `translate(${PEERS_OVERFLOW_COORD.x} ${PEERS_OVERFLOW_COORD.y})`, class: 'topo-node on' })
    mk('rect', { x: -15, y: -11, width: 30, height: 22, rx: 8, fill: 'rgb(var(--elevated))', stroke: TOPO_COLOR.tunnel, 'stroke-width': 1.5 }, g)
    mk('text', { x: 0, y: 3.5, 'text-anchor': 'middle', 'font-size': 9.5, 'font-weight': 700, fill: TOPO_COLOR.tunnel }, g).textContent = '+' + model.hiddenPeers.length
  }

  // distnodes
  model.distNodes.forEach((dv) => {
    const managed = dv.node.kind === 'managed'
    const g = mk('g', { transform: `translate(${dv.x} ${dv.y})`, class: 'topo-node on' })
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
    const g = mk('g', { transform: `translate(${chip.x} ${chip.y})`, class: 'topo-node on' })
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
    mk('title', {}, g).textContent = d.name
  })

  // etiquetas
  const labelsG = mk('g', { class: 'topo-labels', 'pointer-events': 'none', 'aria-hidden': 'true' })
  function label(x, y, anchor, title, sub, subColor) {
    const g = mk('g', {}, labelsG)
    mk('text', { x, y, 'text-anchor': anchor, 'font-size': 13, 'font-weight': 600, fill: 'rgb(var(--text-primary))', stroke: 'rgb(var(--canvas))', 'stroke-width': 4, style: 'paint-order: stroke' }, g).textContent = title
    mk('text', { x, y: y + 15, 'text-anchor': anchor, 'font-size': 10.5, fill: subColor || 'rgb(var(--text-secondary))', stroke: 'rgb(var(--canvas))', 'stroke-width': 3, style: 'paint-order: stroke' }, g).textContent = sub
  }
  label(548, 54, 'start', `Internet · ${model.wan.isp}`, `${model.wan.plan} · ${model.wan.latencyMs} ms`)
  if (model.gatewayNode) {
    const n = model.gatewayNode
    label(n.label.x, n.label.y, n.label.anchor, n.router.name, `${n.router.modelShort} · ${n.router.roleBadge} · ${n.router.clients} clients`)
  }
  model.apNodes.forEach((n) => {
    const warn = n.router.status === 'warn'
    label(n.label.x, n.label.y, n.label.anchor, n.router.name, warn ? `${n.router.clients} clients · warn` : `${n.router.clients} clients`, warn ? TOPO_COLOR.warn : undefined)
  })
  model.distNodes.forEach((dv) => {
    if (dv.node.kind === 'managed') label(dv.x, dv.y - 34, 'middle', dv.node.name || 'Switch', `${dv.node.ip || 'LLDP'} · ${dv.node.port}`, TOPO_COLOR.accent)
    else label(dv.x, dv.y - 34, 'middle', 'Switch inferido', `${dv.node.port}`)
  })
  for (const [hostId] of model.ctsByHost) {
    const host = model.chips.find((c) => c.id === hostId); if (!host) continue
    label(host.x, host.y - 38, 'middle', host.device.name, `hipervisor · ${model.ctCountByHost.get(hostId) || 0} CT`)
  }
  model.peerNodes.forEach((n, i) => label(n.x + (i % 2 ? -26 : 26), n.y - 4, i % 2 ? 'end' : 'start', n.peer.name, 'vía Internet', TOPO_COLOR.tunnel))
  model.links.filter((l) => l.label).forEach((l) => {
    mk('text', { x: l.lx, y: l.ly, 'font-size': 10, fill: 'rgb(var(--text-muted))', stroke: 'rgb(var(--canvas))', 'stroke-width': 3, style: 'paint-order: stroke', class: 'font-mono' }, labelsG).textContent = l.label
  })
}

function buildTopo() {
  if (topoBuilt) return
  topoBuilt = true
  drawTopology()
}

function buildRouters() {
  const grid = document.getElementById('routerGrid')
  const C = 2 * Math.PI * 31 // R=31 (size 56 /2 +stroke? inner r ~31)
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
