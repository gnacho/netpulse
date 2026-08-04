/* ============================================================
   NetPulse landing — app.js
   Teatro sticky por scroll, widgets animados con datos reales
   del demo, widget de apariencia (tema + acento) y miscelánea.
   ============================================================ */

const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches

/* ---------- helpers ---------- */
function easeOutCubic(x) { return 1 - Math.pow(1 - x, 3) }

function animateNumber(el, target, { decimals = 0, suffix = '', duration = 1200, format } = {}) {
  if (reduceMotion) {
    el.textContent = format ? format(target) : target.toFixed(decimals) + suffix
    return
  }
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
    `<span><i class="dot" style="background:rgb(var(--ok))"></i><i data-i18n-inline="strip.health">${t('strip.health')}</i>&nbsp;<i class="font-mono">92/100</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--accent))"></i><i>${t('strip.down')}</i>&nbsp;<i class="font-mono">84.2 Mbps</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--tunnel))"></i><i>${t('strip.up')}</i>&nbsp;<i class="font-mono">12.6 Mbps</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--info))"></i><i>${t('strip.latency')}</i>&nbsp;<i class="font-mono">8 ms</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--text-secondary))"></i><i>${t('strip.devices')}</i>&nbsp;<i class="font-mono">67</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--danger))"></i><i>AdGuard</i>&nbsp;<i class="font-mono">60/65 ${t('adguard.clients')}</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--warn))"></i><i>DNS</i>&nbsp;<i class="font-mono">14 ms</i></span>`,
    `<span><i class="dot" style="background:rgb(var(--ok))"></i><i>WireGuard</i>&nbsp;<i class="font-mono">peer phone · ${t('alerts.ago')} 2 min</i></span>`,
  ]
  // duplicado para loop continuo
  el.innerHTML = items.join('') + items.join('')
}

/* ---------- stage 0: health ring ---------- */
let ringPlayed = false
function playRing() {
  if (ringPlayed) return
  ringPlayed = true
  const arc = document.getElementById('ringArc')
  const num = document.getElementById('ringNum')
  const C = 490.1
  const score = DEMO.score
  if (reduceMotion) {
    arc.style.strokeDashoffset = C * (1 - score / 100)
    num.textContent = score
    return
  }
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
  const out = []
  let s = seed
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
  const W = 400, H = 120
  const downData = genSeries(48, DEMO.down, 18, 7)
  const upData = genSeries(48, DEMO.up, 5, 13)
  const max = 110
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
  // dibujar progresivamente
  const Ld = lineDown.getTotalLength()
  const Lu = lineUp.getTotalLength()
  lineDown.style.strokeDasharray = Ld
  lineUp.style.strokeDasharray = Lu
  lineDown.style.strokeDashoffset = Ld
  lineUp.style.strokeDashoffset = Lu
  const dur = 1500
  const start = performance.now()
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
    const d = DEMO.down + (Math.random() - 0.5) * 6
    const u = DEMO.up + (Math.random() - 0.5) * 2
    downEl.textContent = Math.max(60, d).toFixed(1)
    upEl.textContent = Math.max(9, u).toFixed(1)
  }, 2500)
}

/* ---------- stage 2: topology ---------- */
let topoBuilt = false
function buildTopo() {
  if (topoBuilt) return
  topoBuilt = true
  const svg = document.getElementById('topoSvg')
  const NS = 'http://www.w3.org/2000/svg'
  const cx = 260, cy = 175
  // gateway central
  const gw = { x: cx, y: cy, r: 26, label: t('topo.gw') + ' · Flint 2', cls: 'gw' }
  // APs en anillo
  const aps = [
    { x: cx - 150, y: cy - 70, label: 'AP-Estudio' },
    { x: cx + 150, y: cy - 70, label: 'AP-Salon' },
    { x: cx, y: cy + 120, label: 'AP-Dormitorio' },
  ]
  // clientes
  const clients = [
    { x: cx - 210, y: cy + 20, label: 'tv-salon', wired: true },
    { x: cx - 190, y: cy - 140, label: 'laptop-nacho', wired: false },
    { x: cx + 205, y: cy - 130, label: 'phone', wired: false },
    { x: cx + 215, y: cy + 10, label: 'printer', wired: true },
    { x: cx + 70, y: cy + 150, label: 'tablet', wired: false },
    { x: cx - 60, y: cy + 155, label: 'cam-patio', wired: false },
  ]
  const wg = { x: cx, y: 28, label: t('topo.inet') }

  const mkEl = (tag, attrs = {}) => {
    const e = document.createElementNS(NS, tag)
    Object.entries(attrs).forEach(([k, v]) => e.setAttribute(k, v))
    return e
  }

  // edges: cliente→AP, AP→GW, GW→Internet
  // addEdge devuelve [line, dot?] — ambos deben ir al DOM y a la coreografía
  let edgeSeq = 0
  const addEdge = (a, b, color, width = 1.5, flow = false) => {
    const line = mkEl('line', {
      x1: a.x, y1: a.y, x2: b.x, y2: b.y,
      stroke: color, 'stroke-width': width, 'stroke-linecap': 'round',
      class: 'topo-edge', pathLength: 1,
    })
    const out = [line]
    // pulso de flujo (punto viajero)
    if (flow && !reduceMotion) {
      const dot = mkEl('circle', { r: 2.5, fill: color, class: 'topo-flow' })
      const anim = mkEl('animateMotion', { dur: `${2 + Math.random() * 2}s`, repeatCount: 'indefinite' })
      const mpath = mkEl('mpath')
      // animateMotion necesita un path: construimos uno invisible
      const pid = `tp-edge-${edgeSeq++}`
      const p = mkEl('path', { d: `M${a.x} ${a.y} L${b.x} ${b.y}`, id: pid, fill: 'none', stroke: 'none' })
      svg.appendChild(p)
      mpath.setAttribute('href', '#' + pid)
      mpath.setAttributeNS('http://www.w3.org/1999/xlink', 'href', '#' + pid)
      anim.appendChild(mpath)
      dot.appendChild(anim)
      out.push(dot)
    }
    return out
  }

  const C_OK = 'rgb(var(--ok))'      // cable
  const C_ACC = 'rgb(var(--accent))' // wifi
  const C_TUN = 'rgb(var(--tunnel))' // túnel

  // orden de construcción: GW, APs, clientes, túnel
  const order = []

  // gateway
  const gGw = mkEl('g', { class: 'topo-node' })
  gGw.appendChild(mkEl('circle', { cx: gw.x, cy: gw.y, r: gw.r, fill: 'rgb(var(--surface))', stroke: C_ACC, 'stroke-width': 2 }))
  const gwTxt = mkEl('text', { x: gw.x, y: gw.y + 4, 'text-anchor': 'middle', fill: 'rgb(var(--text-primary))', 'font-size': '9', 'font-family': 'JetBrains Mono, monospace' })
  gwTxt.textContent = 'GW'
  gGw.appendChild(gwTxt)
  const gwLbl = mkEl('text', { x: gw.x, y: gw.y + gw.r + 14, 'text-anchor': 'middle', fill: 'rgb(var(--text-muted))', 'font-size': '9' })
  gwLbl.textContent = gw.label
  gGw.appendChild(gwLbl)
  order.push(gGw)

  // APs + edges GW→AP
  aps.forEach(ap => {
    const g = mkEl('g', { class: 'topo-node' })
    g.appendChild(mkEl('circle', { cx: ap.x, cy: ap.y, r: 16, fill: 'rgb(var(--elevated))', stroke: C_ACC, 'stroke-width': 1.5 }))
    // arquito wifi dentro
    const w = mkEl('path', {
      d: `M${ap.x - 6} ${ap.y + 2} A8 8 0 0 1 ${ap.x + 6} ${ap.y + 2}`,
      stroke: C_ACC, 'stroke-width': 1.5, fill: 'none', 'stroke-linecap': 'round',
    })
    g.appendChild(w)
    const t1 = mkEl('text', { x: ap.x, y: ap.y - 24, 'text-anchor': 'middle', fill: 'rgb(var(--text-secondary))', 'font-size': '9' })
    t1.textContent = ap.label
    g.appendChild(t1)
    addEdge(gw, ap, C_ACC, 1.5, true).forEach(e => order.push(e))
    order.push(g)
  })

  // clientes + edges AP→cliente
  clients.forEach((c, i) => {
    const ap = aps[i % aps.length]
    const g = mkEl('g', { class: 'topo-node' })
    const col = c.wired ? C_OK : C_ACC
    g.appendChild(mkEl('circle', { cx: c.x, cy: c.y, r: 5, fill: col, opacity: 0.85 }))
    const t1 = mkEl('text', {
      x: c.x + (c.x > cx ? 10 : -10), y: c.y + 3,
      'text-anchor': c.x > cx ? 'start' : 'end',
      fill: 'rgb(var(--text-muted))', 'font-size': '8', 'font-family': 'JetBrains Mono, monospace',
    })
    t1.textContent = c.label
    g.appendChild(t1)
    addEdge(ap, c, col, 1, false).forEach(e => order.push(e))
    order.push(g)
  })

  // túnel WireGuard GW → Internet
  const gWg = mkEl('g', { class: 'topo-node' })
  gWg.appendChild(mkEl('circle', { cx: wg.x, cy: wg.y, r: 14, fill: 'rgb(var(--surface))', stroke: C_TUN, 'stroke-width': 1.5 }))
  const wgT = mkEl('text', { x: wg.x, y: wg.y + 3.5, 'text-anchor': 'middle', fill: C_TUN, 'font-size': '8', 'font-family': 'JetBrains Mono, monospace' })
  wgT.textContent = 'WG'
  gWg.appendChild(wgT)
  const wgLbl = mkEl('text', { x: wg.x + 24, y: wg.y + 3, fill: 'rgb(var(--text-muted))', 'font-size': '9' })
  wgLbl.textContent = wg.label
  gWg.appendChild(wgLbl)
  addEdge(gw, wg, C_TUN, 2, true).forEach(e => order.push(e))
  order.push(gWg)

  order.forEach(e => svg.appendChild(e))

  // leyenda
  const legend = [
    { c: C_OK, k: t('topo.wired') },
    { c: C_ACC, k: t('topo.wifi') },
    { c: C_TUN, k: t('topo.tunnel') },
  ]
  legend.forEach((l, i) => {
    const g = mkEl('g', { class: 'topo-node' })
    g.appendChild(mkEl('circle', { cx: 16, cy: 320 - i * 16, r: 4, fill: l.c }))
    const tx = mkEl('text', { x: 26, y: 323 - i * 16, fill: 'rgb(var(--text-muted))', 'font-size': '9' })
    tx.textContent = l.k
    g.appendChild(tx)
    order.push(g)
    svg.appendChild(g)
  })

  // coreografía secuencial
  if (reduceMotion) {
    svg.querySelectorAll('.topo-node,.topo-edge,.topo-flow').forEach(e => e.classList.add('on'))
    return
  }
  let delay = 0
  order.forEach(el => {
    setTimeout(() => el.classList.add('on'), delay)
    delay += el.classList.contains('topo-edge') ? 160 : 220
  })
}

/* ---------- stage 3: routers ---------- */
const ROUTERS = [
  { name: 'GW-Flint2', model: 'GL.iNet Flint 2', cpu: 23, mem: 48, temp: 52, up: '34d 6h' },
  { name: 'AP-Estudio', model: 'Xiaomi AX6', cpu: 41, mem: 63, temp: 78, up: '12d 2h', warn: true },
  { name: 'AP-Salon', model: 'Xiaomi AX6', cpu: 18, mem: 55, temp: 49, up: '34d 6h' },
  { name: 'AP-Dormitorio', model: 'Xiaomi AX6', cpu: 33, mem: 59, temp: 55, up: '34d 5h' },
]
let routersBuilt = false
function buildRouters() {
  if (routersBuilt) return
  routersBuilt = true
  const grid = document.getElementById('routerGrid')
  grid.innerHTML = ROUTERS.map(r => `
    <div class="rcard card" style="background:rgb(var(--elevated)/0.4);">
      <div class="rt">
        <span class="name font-mono">${r.name}</span>
        <span class="caption text-muted">${r.model}</span>
      </div>
      <div class="meters">
        <div class="meter"><div class="ml"><span>${t('routers.cpu')}</span><span class="font-mono" data-v="${r.cpu}">0%</span></div><div class="bar"><i data-w="${r.cpu}"></i></div></div>
        <div class="meter"><div class="ml"><span>${t('routers.mem')}</span><span class="font-mono" data-v="${r.mem}">0%</span></div><div class="bar"><i data-w="${r.mem}"></i></div></div>
        <div class="meter ${r.warn ? 'warn' : ''}"><div class="ml"><span>${t('routers.temp')}</span><span class="font-mono" data-v="${r.temp}" data-suf="°C">0°C</span></div><div class="bar"><i data-w="${Math.min(100, r.temp)}"></i></div></div>
      </div>
      <div class="caption text-muted" style="margin-top:0.6rem;">${t('routers.up')} <span class="font-mono">${r.up}</span></div>
    </div>
  `).join('')
}
function renderRouterNames() {
  if (!routersBuilt) return
  routersBuilt = false
  buildRouters() // reconstruye con labels del nuevo idioma
  if (routersPlayed) {
    // ya animado antes: estado final sin re-animar
    const grid = document.getElementById('routerGrid')
    grid.querySelectorAll('.bar i').forEach(bar => { bar.style.width = bar.dataset.w + '%' })
    grid.querySelectorAll('.ml span[data-v]').forEach(sp => {
      const v = parseFloat(sp.dataset.v)
      const suf = sp.dataset.suf || '%'
      sp.textContent = Math.round(v) + suf
    })
  }
}
let routersPlayed = false
function playRouters() {
  buildRouters()
  if (routersPlayed) return
  routersPlayed = true
  const grid = document.getElementById('routerGrid')
  grid.querySelectorAll('.bar i').forEach(bar => {
    const w = bar.dataset.w
    if (reduceMotion) { bar.style.width = w + '%'; return }
    setTimeout(() => { bar.style.width = w + '%' }, 80)
  })
  grid.querySelectorAll('.ml span[data-v]').forEach(sp => {
    const v = parseFloat(sp.dataset.v)
    const suf = sp.dataset.suf || '%'
    animateNumber(sp, v, { duration: 900, format: x => Math.round(x) + suf })
  })
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
  // si ya se reprodujo, mantener visibles tras el re-render por cambio de idioma
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
  document.querySelectorAll('.stage').forEach((s, idx) => {
    s.classList.toggle('active', idx === i)
  })
  document.querySelectorAll('.tstep').forEach(s => {
    s.style.opacity = parseInt(s.dataset.stage, 10) === i ? '1' : ''
  })
  STAGE_PLAYERS[i]()
}

function initTheater() {
  const steps = [...document.querySelectorAll('.tstep')]
  if (!('IntersectionObserver' in window)) { setStage(0); return }
  const io = new IntersectionObserver(entries => {
    entries.forEach(en => {
      if (en.isIntersecting) setStage(parseInt(en.target.dataset.stage, 10))
    })
  }, { rootMargin: '-42% 0px -42% 0px', threshold: 0 })
  steps.forEach(s => io.observe(s))
  setStage(0)
}

/* ---------- reveals on scroll ---------- */
function initReveals() {
  const els = document.querySelectorAll('.reveal')
  if (reduceMotion || !('IntersectionObserver' in window)) {
    els.forEach(e => e.classList.add('in'))
    return
  }
  const io = new IntersectionObserver(entries => {
    entries.forEach(en => {
      if (en.isIntersecting) { en.target.classList.add('in'); io.unobserve(en.target) }
    })
  }, { threshold: 0.12 })
  els.forEach(e => io.observe(e))
}

/* ---------- widget apariencia ---------- */
const MODE_KEY = 'netpulse-web-theme-mode'
const ACCENT_KEY = 'netpulse-web-accent'
const ACCENTS = {
  cyan:    { dark: '34 211 238', light: '8 145 178' },
  violet:  { dark: '167 139 250', light: '124 58 237' },
  emerald: { dark: '52 211 153', light: '5 150 105' },
  amber:   { dark: '251 191 36', light: '217 119 6' },
}
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
function applyAccent() {
  const root = document.documentElement
  root.dataset.accent = accentId
  // el acento cyan es el :root por defecto; los demás vía data-accent + CSS
}
function updateAppearancePanel() {
  document.querySelectorAll('#themeSeg button').forEach(b => {
    b.classList.toggle('on', b.dataset.mode === themeMode)
  })
  document.querySelectorAll('#accentSwatches .swatch').forEach(b => {
    b.classList.toggle('on', b.dataset.accent === accentId)
  })
}
function setMode(m) {
  themeMode = m
  writeStore(MODE_KEY, m)
  applyTheme()
  updateAppearancePanel()
}
function setAccent(a) {
  accentId = a
  writeStore(ACCENT_KEY, a)
  applyAccent()
  updateAppearancePanel()
}
// expuestos para QA
window.setMode = setMode
window.setAccent = setAccent

function initAppearance() {
  themeMode = readStore(MODE_KEY) || 'dark'
  accentId = readStore(ACCENT_KEY) || 'cyan'
  applyTheme()
  applyAccent()
  updateAppearancePanel()

  const fab = document.getElementById('appFab')
  const panel = document.getElementById('appPanel')
  fab.addEventListener('click', () => {
    panel.classList.toggle('open')
    fab.setAttribute('aria-expanded', panel.classList.contains('open'))
  })
  document.addEventListener('click', e => {
    if (!panel.contains(e.target) && !fab.contains(e.target)) panel.classList.remove('open')
  })
  document.addEventListener('keydown', e => { if (e.key === 'Escape') panel.classList.remove('open') })

  document.querySelectorAll('#themeSeg button').forEach(b => {
    b.addEventListener('click', () => setMode(b.dataset.mode))
  })
  document.querySelectorAll('#accentSwatches .swatch').forEach(b => {
    b.addEventListener('click', () => setAccent(b.dataset.accent))
  })
  window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => {
    if (themeMode === 'system') applyTheme()
  })
}

/* ---------- copiar comando ---------- */
function initCopy() {
  const btn = document.getElementById('copyBtn')
  if (!btn) return
  btn.addEventListener('click', async () => {
    const cmd = document.getElementById('installCmd').textContent
    try {
      await navigator.clipboard.writeText(cmd)
    } catch {
      // fallback sin clipboard API
      const ta = document.createElement('textarea')
      ta.value = cmd
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      ta.remove()
    }
    const orig = btn.textContent
    btn.textContent = t('misc.copied')
    setTimeout(() => { btn.textContent = orig }, 1600)
  })
}

/* ---------- ko-fi placeholder ---------- */
function initKofi() {
  const link = document.getElementById('kofiLink')
  // TODO(nacho): URL definitiva de Ko-fi — pendiente de que la cree.
  // Cuando exista, ponerla aquí y listo. Mientras tanto enlaza al repositorio.
  link.href = 'https://github.com/gnacho/netpulse'
}

/* ---------- boot ---------- */
document.addEventListener('DOMContentLoaded', () => {
  initAppearance()
  renderTicker()
  renderAlerts()
  initReveals()
  initTheater()
  initCopy()
  initKofi()
  heroCountUps()
})
