import { forwardRef, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNetPulse } from '@/data/DataProvider'
import { AlertCircle, ArrowDown, ArrowUp, ArrowUpDown, RefreshCw, Router, Sparkles } from 'lucide-react'
import { cn } from '@/lib/utils'
import { ssidColor } from '@/lib/ssidColor'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  ChannelSpectrum,
  channelFreq,
  isDfsChannel,
  themeColor,
  type SpectrumNet,
} from '@/components/channels/ChannelSpectrum'

interface Scan {
  iface: string
  bssid: string
  ssid: string
  channel: number
  freq: number
  signal: number
  widthMhz?: number
  routerId: string
  ts?: number
  own?: boolean
}

interface Score {
  channel: number
  score: number
  neighbors: number
  strongest: number // dBm de la vecina más fuerte considerada
  dfs: boolean
  recommendable: boolean
}

interface RadioRec {
  iface: string
  section?: string
  name: string
  channel: number
  widthMhz: number
  recommended: number
  currentScore: number
  bestScore: number
  scores?: Score[]
}

interface ChannelPlanData {
  routerId: string
  radios: RadioRec[]
  scans: Scan[]
}

function bandFromFreq(freq: number): string {
  if (freq >= 2412 && freq <= 2484) return '2.4 GHz'
  if (freq >= 5180 && freq <= 5885) return '5 GHz'
  if (freq >= 5955) return '6 GHz'
  return `${freq} MHz`
}

// Señal → nivel (4 barras) y calidad, como el channel analysis de LuCI.
function signalLevel(signal: number): 1 | 2 | 3 | 4 {
  if (signal >= -50) return 4
  if (signal >= -65) return 3
  if (signal >= -78) return 2
  return 1
}

function SignalBars({ signal, own }: { signal: number; own?: boolean }) {
  const lvl = signalLevel(signal)
  const colorCls = own ? 'bg-accent' : lvl >= 3 ? 'bg-ok' : lvl === 2 ? 'bg-warn' : 'bg-danger'
  const heights = [4, 7, 10, 13]
  return (
    <span className="inline-flex h-[14px] items-end gap-[2px]" aria-hidden>
      {heights.map((h, i) => (
        <span
          key={i}
          className={cn('w-[3.5px] rounded-[1.5px]', i < lvl ? colorCls : 'bg-border')}
          style={{ height: h }}
        />
      ))}
    </span>
  )
}

// Anillo de puntuación (SVG): pct alto = canal limpio.
function ScoreRing({ pct }: { pct: number }) {
  const color = pct >= 80 ? themeColor('--ok') : pct >= 55 ? themeColor('--warn') : themeColor('--danger')
  return (
    <div className="relative h-[52px] w-[52px] shrink-0" aria-hidden>
      <svg width="52" height="52" className="-rotate-90">
        <circle cx="26" cy="26" r="21" fill="none" stroke={themeColor('--border')} strokeWidth="6" />
        <circle
          cx="26"
          cy="26"
          r="21"
          fill="none"
          stroke={color}
          strokeWidth="6"
          strokeLinecap="round"
          strokeDasharray={`${(pct / 100) * 131.9} 131.9`}
        />
      </svg>
      <span className="absolute inset-0 grid place-items-center text-[13px] font-extrabold text-text-primary">{pct}</span>
    </div>
  )
}


// FleetMenuTrigger (#1070): selector de unidad de flota NO nativo, con el
// mismo look que los filtros desplegables de Clientes (Devices.tsx #989).
// forwardRef + spread OBLIGATORIOS (#1006): DropdownMenuTrigger asChild
// inyecta el ref y los handlers; sin ellos el menú no abre.
const FleetMenuTrigger = forwardRef<
  HTMLButtonElement,
  React.ComponentPropsWithoutRef<'button'> & { label: string; value: string }
>(function FleetMenuTrigger({ label, value, className, ...props }, ref) {
  return (
    <button
      ref={ref}
      className={cn(
        'inline-flex h-8 items-center gap-1.5 rounded-lg border border-border bg-elevated px-3 text-xs font-medium text-text-secondary transition-colors hover:bg-hover hover:text-text-primary',
        className,
      )}
      {...props}
    >
      <Router className="h-3.5 w-3.5" strokeWidth={1.75} />
      {label}
      <span className="font-semibold text-text-primary">{value}</span>
    </button>
  )
})

export default function ChannelPlan() {
  const { t, i18n } = useTranslation()
  const { routers } = useNetPulse()
  const [routerId, setRouterId] = useState('')
  const [data, setData] = useState<ChannelPlanData | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [activeRadio, setActiveRadio] = useState('')
  const [hidden, setHidden] = useState<Set<string>>(new Set())
  const [selected, setSelected] = useState<string | null>(null)
  const [hover, setHover] = useState<{ net: SpectrumNet; x: number; y: number } | null>(null)
  const [sort, setSort] = useState<{ key: 'signal' | 'ssid' | 'channel'; dir: 'asc' | 'desc' } | null>(null)
  const [focus, setFocus] = useState<number | null>(null)
  const [onlyMine, setOnlyMine] = useState(false)
  const [lastRefresh, setLastRefresh] = useState<number>(0)
  const pollTimer = useRef<ReturnType<typeof setInterval> | null>(null)

  const sortedRouters = useMemo(() => {
    // Sin unidades que no pueden tener WiFi (p. ej. switches gestionados):
    // en el picker de un análisis de canales solo estorban (#1085).
    return [...routers]
      .filter((r) => r.type !== 'managed-switch')
      .sort((a, b) => (a.roleBadge === 'Principal' ? -1 : 1) || a.name.localeCompare(b.name))
  }, [routers])

  useEffect(() => {
    if (!routerId && sortedRouters.length > 0) {
      setRouterId(sortedRouters[0]!.id)
    }
  }, [sortedRouters, routerId])

  const loadPlan = useCallback((rid: string, silent = false) => {
    if (!silent) {
      setLoading(true)
      setError('')
    }
    fetch(`/api/wifi/channel-plan?routerId=${encodeURIComponent(rid)}`)
      .then(async (res) => {
        if (!res.ok) throw new Error(await res.text())
        return res.json()
      })
      .then((d) => {
        setData({ ...d, radios: d.radios ?? [], scans: d.scans ?? [] })
        setLastRefresh(Date.now())
      })
      .catch((e) => {
        if (!silent) setError(String(e))
      })
      .finally(() => {
        if (!silent) setLoading(false)
      })
  }, [])

  useEffect(() => {
    if (!routerId) return
    setActiveRadio('')
    setHidden(new Set())
    setSelected(null)
    loadPlan(routerId)
    if (pollTimer.current) clearInterval(pollTimer.current)
    // Refresco silencioso: los agentes escanean solos; la página sigue el
    // último scan sin recargar a mano.
    pollTimer.current = setInterval(() => loadPlan(routerId, true), 60000)
    return () => {
      if (pollTimer.current) clearInterval(pollTimer.current)
    }
  }, [routerId, loadPlan])

  const radioKey = (r: RadioRec) => `${r.section || r.iface}-${r.name}-${r.channel}`
  const radios: RadioRec[] = data?.radios ?? []
  const active: RadioRec | undefined = radios.find((r) => radioKey(r) === activeRadio) ?? radios[0]
  const routerName = sortedRouters.find((r) => r.id === routerId)?.name ?? routerId

  // Vecinos de la radio activa: por banda; si el iface del radio casa con
  // iface de scan, se estrece a lo visto por esa interfaz (dos radios en la
  // misma banda no se mezclan).
  const bandScans = useMemo(() => {
    if (!data || !active) return []
    const inBand = data.scans.filter((s) => bandFromFreq(s.freq) === active.name)
    const ifaceHits = active.iface ? inBand.filter((s) => s.iface === active.iface) : []
    return ifaceHits.length > 0 ? ifaceHits : inBand
  }, [data, active])

  // Redes para el espectro: la propia del radio va al final para pintarse
  // encima de las campanas vecinas que se solapen.
  const nets = useMemo<SpectrumNet[]>(() => {
    if (!active) return []
    const own: SpectrumNet = {
      key: 'own',
      ssid: t('channelPlan.ownNetwork'),
      bssid: '',
      channel: active.channel,
      freq: channelFreq(active.name, active.channel),
      signal: -25,
      widthMhz: active.widthMhz > 0 ? active.widthMhz : 20,
      own: true,
      color: themeColor('--accent'),
    }
    const neighbors: SpectrumNet[] = bandScans.map((s) => ({
      key: s.bssid + s.channel,
      ssid: s.ssid || t('channelPlan.hidden'),
      bssid: s.bssid,
      channel: s.channel,
      freq: s.freq,
      signal: s.signal,
      // Ancho real del anuncio HT/VHT del vecino (#1087); 20 MHz de
      // fallback cuando el agente viejo/no lo trae.
      widthMhz: s.widthMhz && s.widthMhz > 0 ? s.widthMhz : 20,
      own: s.own ?? false,
      color: s.own ? themeColor('--accent') : ssidColor(s.ssid || s.bssid),
    }))
    return [...neighbors, own]
  }, [active, bandScans, t])

  const allKeys = useMemo(() => new Set(nets.map((n) => n.key)), [nets])
  const nonOwnKeys = useMemo(() => new Set(nets.filter((n) => !n.own).map((n) => n.key)), [nets])

  // "Solo mías": oculta las vecinas mientras el toggle esté activo.
  useEffect(() => {
    if (onlyMine) setHidden(new Set(nonOwnKeys))
  }, [onlyMine, nonOwnKeys])

  // Puntuación normalizada (0-100) de cada canal/bloque de la banda: 100 =
  // el más limpio. Incluye DFS informativos (#1076); el sugerido es el mejor
  // recomendable.
  const scored = useMemo(() => {
    if (!active?.scores || active.scores.length === 0) return []
    const raw = active.scores.map((c) => c.score)
    if (active.currentScore > 0 && active.currentScore < 900000) raw.push(active.currentScore)
    const worst = Math.max(...raw)
    const best = Math.min(...raw)
    const pctOf = (score: number) => (worst <= best ? 100 : Math.round((100 * (worst - score)) / (worst - best)))
    return active.scores
      .map((c) => ({
        ...c,
        pct: pctOf(c.score),
        isCurrent: c.channel === active.channel,
        isBest: c.channel === active.recommended && c.channel !== active.channel,
      }))
      .sort((a, b) => b.pct - a.pct || a.channel - b.channel)
  }, [active])

  // Grupos por congestión para la tira de puntuación (#1076).
  const tiers = useMemo(() => {
    const t: { key: string; items: typeof scored }[] = [
      { key: 'optimal', items: [] },
      { key: 'moderate', items: [] },
      { key: 'busy', items: [] },
    ]
    for (const s of scored) {
      (s.pct >= 80 ? t[0]! : s.pct >= 55 ? t[1]! : t[2]!).items.push(s)
    }
    return t.filter((g) => g.items.length > 0)
  }, [scored])

  const currentScoreValid = !!active && active.currentScore > 0 && active.currentScore < 900000
  const currentPct = useMemo(() => {
    if (!active) return 0
    if (scored.length > 0) {
      const cur = scored.find((s) => s.isCurrent)
      if (cur) return cur.pct
      const best = scored.reduce((a, b) => (b.pct > a.pct ? b : a))
      return best.pct
    }
    return 0
  }, [active, scored])

  // Mejor candidato = el RECOMENDADO por el motor (nunca un bloque DFS o no
  // ortodoxo que empate a 100 en la normalización relativa, #1082).
  const bestCand = useMemo(() => {
    if (scored.length === 0) return null
    if (active && active.recommended > 0) {
      const rec = scored.find((s) => s.channel === active.recommended)
      if (rec) return rec
    }
    const recs = scored.filter((s) => s.recommendable)
    return (recs.length > 0 ? recs : scored)[0]!
  }, [scored, active])
  const suggested = active && active.recommended > 0 && active.recommended !== active.channel ? active.recommended : 0
  const ownCount = bandScans.filter((s) => s.own).length
  const neighborCount = bandScans.length - ownCount
  const strongestNeighbor = useMemo(() => {
    const ns = bandScans.filter((s) => !s.own)
    if (ns.length === 0) return null
    return ns.reduce((a, b) => (b.signal > a.signal ? b : a))
  }, [bandScans])
  const lastScanAt = useMemo(() => {
    const tss = bandScans.map((s) => s.ts ?? 0).filter((v) => v > 0)
    if (tss.length === 0) return 0
    return Math.max(...tss)
  }, [bandScans])
  // Congestión ABSOLUTA del canal actual (#1082): el verdict no depende de
  // lo malo que sea el resto de la banda. Referencias del score (potencia
  // x1000): 1 vecina a -60 ≈ 1000; a -50 ≈ 10000. Dos -72 ≈ 126 → baja.
  const congestionAbs = useMemo(() => {
    if (!active || !currentScoreValid) return null
    const s = active.currentScore
    return s < 500 ? 'low' : s < 5000 ? 'moderate' : 'high'
  }, [active, currentScoreValid])

  // Limpieza ABSOLUTA del anillo (misma escala que el chip): decaimiento
  // logarítmico sobre el score de potencia, no el peor-vs-mejor de la banda.
  const ringPct = useMemo(() => {
    if (!active || !currentScoreValid) return currentPct
    const clean = Math.round(100 - Math.min(100, (100 * Math.log10(active.currentScore + 1)) / 4))
    return Math.max(0, clean)
  }, [active, currentScoreValid, currentPct])

  // Ordenación de la tabla: por defecto propias primero y luego señal desc;
  // al elegir columna se ordena puro asc/desc (la alterna al repetir clic).
  const toggleSort = (key: 'signal' | 'ssid' | 'channel') =>
    setSort((prev) => (prev && prev.key === key ? { key, dir: prev.dir === 'asc' ? 'desc' : 'asc' } : { key, dir: key === 'signal' ? 'desc' : 'asc' }))

  const sortedScans = useMemo(() => {
    const arr = bandScans.slice()
    if (!sort) return arr.sort((a, b) => Number(b.own ?? false) - Number(a.own ?? false) || b.signal - a.signal)
    const dir = sort.dir === 'asc' ? 1 : -1
    return arr.sort((a, b) => {
      if (sort.key === 'signal') return (a.signal - b.signal) * dir
      if (sort.key === 'channel') return (a.channel - b.channel) * dir
      return (a.ssid || a.bssid).localeCompare(b.ssid || b.bssid, undefined, { sensitivity: 'base' }) * dir
    })
  }, [bandScans, sort])

  const SortHeader = ({ id, label }: { id: 'signal' | 'ssid' | 'channel'; label: string }) => (
    <button
      onClick={() => toggleSort(id)}
      aria-label={`${label}: ${sort?.key === id ? (sort.dir === 'asc' ? t('channelPlan.sortAsc') : t('channelPlan.sortDesc')) : t('channelPlan.sortable')}`}
      className={cn('inline-flex items-center gap-1 uppercase tracking-wide transition-colors hover:text-text-primary', sort?.key === id ? 'text-accent' : '')}
    >
      {label}
      {sort?.key !== id ? (
        <ArrowUpDown className="h-3 w-3 opacity-50" strokeWidth={2} />
      ) : sort.dir === 'asc' ? (
        <ArrowUp className="h-3 w-3" strokeWidth={2} />
      ) : (
        <ArrowDown className="h-3 w-3" strokeWidth={2} />
      )}
    </button>
  )
  const currentDfs = active ? isDfsChannel(active.name, active.channel) : false

  return (
    <div className="space-y-4 md:space-y-5">
      <header>
        <h1 className="font-display text-h1 text-text-primary">{t('channelPlan.title')}</h1>
        <p className="mt-0.5 text-sm text-text-secondary">{t('channelPlan.subtitle')}</p>
      </header>

      {/* Barra de contexto: equipo + pestañas de radio + acciones */}
      <div className="flex flex-wrap items-center gap-3 rounded-2xl border border-border bg-surface p-4">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <FleetMenuTrigger label={t('channelPlan.router')} value={routerName} />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-56">
            <DropdownMenuLabel>{t('channelPlan.router')}</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuRadioGroup value={routerId} onValueChange={setRouterId}>
              {sortedRouters.map((r) => (
                <DropdownMenuRadioItem key={r.id} value={r.id}>
                  <span className="flex-1">
                    {r.name}
                    {r.roleBadge === 'Principal' ? ` ${t('channelPlan.gateway')}` : ''}
                  </span>
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        {radios.length > 1 && (
          <div className="inline-flex items-center gap-0.5 rounded-xl bg-canvas p-1" role="tablist" aria-label={t('channelPlan.band')}>
            {radios.map((r) => {
              const key = radioKey(r)
              const isActive = active !== undefined && radioKey(active) === key
              return (
                <button
                  key={key}
                  role="tab"
                  aria-selected={isActive}
                  onClick={() => {
                    setActiveRadio(key)
                    setHidden(new Set())
                    setSelected(null)
                    setFocus(null)
                  }}
                  className={cn(
                    'rounded-lg px-3.5 py-1.5 text-[13px] font-semibold transition-all',
                    isActive ? 'bg-surface text-accent shadow-sm' : 'text-text-muted hover:text-text-secondary',
                  )}
                >
                  {r.name}
                  <span className="ml-1.5 font-mono text-caption text-text-muted">ch {r.channel}</span>
                </button>
              )
            })}
          </div>
        )}

        <div className="ml-auto flex items-center gap-3">
          <span className="text-caption text-text-muted">
            {lastScanAt > 0
              ? t('channelPlan.lastScan', {
                  time: new Date(lastScanAt * 1000).toLocaleTimeString(i18n.language, { hour: '2-digit', minute: '2-digit' }),
                })
              : lastRefresh > 0 &&
                t('channelPlan.lastRefresh', {
                  time: new Date(lastRefresh).toLocaleTimeString(i18n.language, { hour: '2-digit', minute: '2-digit' }),
                })}
          </span>
          <Button size="sm" variant="outline" disabled={!routerId || loading} onClick={() => loadPlan(routerId)}>
            <RefreshCw className={cn('mr-1.5 h-3.5 w-3.5', loading && 'animate-spin')} strokeWidth={1.75} />
            {t('channelPlan.refresh')}
          </Button>
        </div>
      </div>

      {error && (
        <div className="flex items-start gap-3 rounded-xl border border-rose-500/40 bg-rose-500/10 px-4 py-3 text-sm text-rose-600 dark:text-rose-400">
          <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
          <span>{error}</span>
        </div>
      )}

      {loading && !data && (
        <div className="rounded-2xl border border-border bg-surface p-8 text-center text-sm text-text-secondary">
          {t('common.loading')}
        </div>
      )}

      {!loading && data && radios.length === 0 && (
        <div className="rounded-2xl border border-border bg-surface p-8 text-center text-sm text-text-secondary">
          {t('channelPlan.noRadios')}
        </div>
      )}

      {!loading && data && active && (
        <>
          {/* Resumen: sugerido / actual / redes detectadas */}
          <div className="grid gap-3 lg:grid-cols-3">
            <div className="relative overflow-hidden rounded-2xl border border-border bg-surface p-5">
              {bestCand && (
                <span className="absolute right-4 top-4 inline-flex items-center gap-1.5 rounded-full border border-ok/30 bg-ok/10 px-2.5 py-1 text-caption font-bold text-ok">
                  <Sparkles className="h-3 w-3" strokeWidth={2} />
                  {bestCand.pct}/100
                </span>
              )}
              <div className="text-label uppercase text-text-muted">{t('channelPlan.summarySuggested')}</div>
              {bestCand ? (
                <>
                  <div className="mt-1 flex items-baseline gap-2">
                    <span className="font-display text-[30px] font-extrabold tracking-tight text-text-primary">
                      {active.name === '2.4 GHz' || active.name === '6 GHz'
                        ? t('channelPlan.channelWord', { ch: bestCand.channel })
                        : bestCand.channel}
                    </span>
                    <span className="text-[13px] font-semibold text-text-secondary">
                      {active.widthMhz > 0 ? `${active.widthMhz} MHz` : '20 MHz'}
                    </span>
                  </div>
                  <p className="mt-1 text-xs leading-relaxed text-text-secondary">
                    {bestCand.neighbors === 0
                      ? t('channelPlan.noNeighborsBlock')
                      : t('channelPlan.neighborsBlock', { count: bestCand.neighbors, dbm: bestCand.strongest })}
                    {bestCand.channel === active.channel || bestCand.pct === currentPct
                      ? ` ${t('channelPlan.alreadyOptimal')}`
                      : ` ${t('channelPlan.betterThanCurrent', { score: ringPct })}`}
                  </p>
                  <p className="mt-1.5 text-xs text-text-muted">{t('channelPlan.applyHint')}</p>
                </>
              ) : (
                <p className="mt-2 text-sm text-text-secondary">{t('channelPlan.noBandScans')}</p>
              )}
            </div>

            <div className="rounded-2xl border border-border bg-surface p-5">
              <div className="text-label uppercase text-text-muted">{t('channelPlan.summaryCurrent')}</div>
              <div className="mt-2 flex items-center gap-3.5">
                <ScoreRing pct={ringPct} />
                <div>
                  <div className="text-[15px] font-bold text-text-primary">
                    {active.name === '2.4 GHz' || active.name === '6 GHz'
                      ? t('channelPlan.channelWord', { ch: active.channel })
                      : active.channel}
                    {active.widthMhz > 0 && <span className="ml-1 text-caption font-normal text-text-muted">{active.widthMhz} MHz</span>}
                  </div>
                  <div className="mt-1 flex items-center gap-1.5">
                    {congestionAbs && (
                      <span
                        className={cn(
                          'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-caption font-semibold',
                          congestionAbs === 'low' && 'bg-ok/10 text-ok',
                          congestionAbs === 'moderate' && 'bg-warn/10 text-warn',
                          congestionAbs === 'high' && 'bg-danger/10 text-danger',
                        )}
                      >
                        {t(`channelPlan.congestion.${congestionAbs}`)}
                      </span>
                    )}
                    {currentDfs && (
                      <span className="rounded-full bg-elevated px-2 py-0.5 text-caption font-semibold text-text-muted">
                        {t('channelPlan.tagDfs')}
                      </span>
                    )}
                  </div>
                </div>
              </div>
              <p className="mt-2 text-xs text-text-secondary">
                {(() => {
                  const cur = scored.find((s) => s.isCurrent)
                  if (cur) {
                    return cur.neighbors === 0 ? t('channelPlan.noOverlap') : t('channelPlan.neighborsInBlock', { count: cur.neighbors })
                  }
                  return currentScoreValid ? t('channelPlan.noOverlap') : t('channelPlan.noBandScans')
                })()}
              </p>
            </div>

            <div className="rounded-2xl border border-border bg-surface p-5">
              <div className="text-label uppercase text-text-muted">{t('channelPlan.summaryNetworks')}</div>
              <div className="mt-1 flex items-baseline gap-2">
                <span className="font-display text-[30px] font-extrabold tracking-tight text-text-primary">{bandScans.length}</span>
                <span className="text-[13px] font-semibold text-text-secondary">
                  {t('channelPlan.yours', { own: ownCount, neighbors: neighborCount })}
                </span>
              </div>
              <p className="mt-1 text-xs leading-relaxed text-text-secondary">
                {strongestNeighbor
                  ? t('channelPlan.strongestNeighbor', {
                      ssid: strongestNeighbor.ssid || t('channelPlan.hidden'),
                      dbm: strongestNeighbor.signal,
                      ch: strongestNeighbor.channel,
                    })
                  : t('channelPlan.noNeighborsBlock')}
              </p>
              <p className="mt-1.5 text-xs text-text-muted">
                {t('channelPlan.viewFrom', { name: routerName })} · {t('channelPlan.scanInfo')}
              </p>
            </div>
          </div>

          {/* Espectro a todo lo ancho (#1076); la puntuación baja a una
              tira debajo, agrupada por congestión. Espectro, tira y leyenda
              se muestran SIEMPRE (#1077): en una banda sin vecinos la
              puntuación es justo lo más útil (todo limpio, elige canal). */}
          <div>
            <div className="rounded-2xl border border-border bg-surface">
                  <div className="flex items-start justify-between gap-3 px-5 pt-4">
                    <div>
                      <h2 className="text-sm font-bold text-text-primary">{t('channelPlan.chartTitle', { band: active.name })}</h2>
                      <p className="mt-0.5 text-caption text-text-muted">{t('channelPlan.chartSub')}</p>
                    </div>
                    <div className="ml-auto flex flex-wrap items-center gap-x-4 gap-y-1 text-caption text-text-muted">
                      <span className="inline-flex items-center gap-1.5">
                        <span className="inline-block h-2.5 w-4 rounded-sm" style={{ backgroundColor: 'rgba(167,139,250,0.2)' }} />
                        {t('channelPlan.legendDfs')}
                      </span>
                      <span className="inline-flex items-center gap-1.5">
                        <span className="inline-block h-2.5 w-4 rounded-sm" style={{ backgroundColor: 'rgba(52,211,153,0.2)' }} />
                        {t('channelPlan.legendSuggested')}
                      </span>
                      <span className="inline-flex items-center gap-1.5">
                        <span className="inline-block h-0 w-4 border-t-2 border-dashed border-accent" />
                        {t('channelPlan.legendCurrent')}
                      </span>
                      {bandScans.length === 0 && (
                        <span className="rounded-lg bg-warn/10 px-2.5 py-1 text-caption text-warn">{t('channelPlan.noBandScans')}</span>
                      )}
                    </div>
                    <span className="rounded-full bg-elevated px-2.5 py-1 text-caption font-semibold text-text-secondary">
                      {t('channelPlan.netsCount', { n: nets.length })}
                    </span>
                  </div>

                  <div className="relative px-3 pt-2">
                    <ChannelSpectrum
                      band={active.name}
                      nets={nets}
                      suggested={suggested}
                      widthMhz={active.widthMhz}
                      hidden={hidden}
                      selected={selected}
                      focus={focus}
                      onHover={(net, x, y) => setHover(net ? { net, x, y } : null)}
                      tSuggest={t('channelPlan.suggested')}
                      tDfs={t('channelPlan.tagDfs')}
                      tActual={t('channelPlan.tActual')}
                      tDbm="dBm"
                    />
                    {hover && (
                      <div
                        className="pointer-events-none absolute z-10 -translate-x-1/2 whitespace-nowrap rounded-[10px] bg-elevated px-3 py-2 text-xs shadow-lg"
                        style={{ left: Math.max(Math.min(hover.x, 320), 90), top: Math.max(hover.y - 12, 30) }}
                      >
                        <div className="flex items-center gap-2 font-bold text-text-primary">
                          <span className="inline-block h-2 w-2 rounded-[3px]" style={{ backgroundColor: hover.net.color }} />
                          {hover.net.ssid}
                        </div>
                        <div className="mt-0.5 text-text-secondary">
                          {t('channelPlan.channelWord', { ch: hover.net.channel })}
                          {hover.net.key === 'own' && hover.net.widthMhz > 0 ? ` · ${hover.net.widthMhz} MHz` : ''} · {hover.net.signal} dBm
                        </div>
                        {hover.net.bssid && <div className="font-mono text-caption text-text-muted">{hover.net.bssid}</div>}
                      </div>
                    )}
                  </div>

                  <div className="px-5 pb-3 pt-1">
                    <div className="mb-1.5 flex items-center gap-2">
                      <span className="text-label uppercase text-text-muted">{t('channelPlan.heatTitle')}</span>
                      <span className="ml-auto text-caption font-normal normal-case tracking-normal text-text-muted">
                        {bandScans.some((s) => !s.own && !(s.widthMhz && s.widthMhz > 0))
                          ? t('channelPlan.heatNote')
                          : t('channelPlan.heatNoteKnown')}
                      </span>
                    </div>
                    <div className="flex justify-between text-[10.5px] text-text-muted">
                      <span>{t('channelPlan.heatClean')}</span>
                      <span>{t('channelPlan.heatBusy')}</span>
                    </div>
                  </div>

                  {/* Puntuación por canal: tira a todo lo ancho agrupada por
                      congestión; clic enfoca el bloque en el espectro (#1076) */}
                  <div className="border-t border-border px-5 py-3">
                    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                      <h2 className="text-sm font-bold text-text-primary">{t('channelPlan.scoresTitle')}</h2>
                      <span className="text-caption text-text-muted">
                        {active.name === '5 GHz'
                          ? t('channelPlan.scoresBlocks', { w: active.widthMhz > 0 ? active.widthMhz : 80 })
                          : t('channelPlan.scoresChannels')}
                        {' · '}
                        {t('channelPlan.scoresFoot')}
                      </span>
                    </div>
                    {tiers.map((g) => (
                      <div key={g.key} className="mt-2 flex flex-wrap items-center gap-1.5">
                        <span
                          className={cn(
                            'w-24 shrink-0 text-[11px] font-bold uppercase tracking-wide',
                            g.key === 'optimal' ? 'text-ok' : g.key === 'moderate' ? 'text-warn' : 'text-danger',
                          )}
                        >
                          {t(`channelPlan.tier.${g.key}`)}
                        </span>
                        {g.items.map((s) => (
                          <button
                            key={s.channel}
                            onClick={() => setFocus(focus === s.channel ? null : s.channel)}
                            title={
                              s.neighbors === 0
                                ? t('channelPlan.whyAlone')
                                : t('channelPlan.whyNeighbors', { n: s.neighbors, dbm: s.strongest })
                            }
                            className={cn(
                              'inline-flex items-center gap-1.5 rounded-lg border px-2 py-1 text-xs transition-colors',
                              focus === s.channel
                                ? 'border-accent/60 bg-accent/10'
                                : s.isCurrent
                                  ? 'border-accent/40 bg-accent/5'
                                  : s.isBest
                                    ? 'border-ok/40 bg-ok/5'
                                    : 'border-border hover:border-border-strong',
                              !s.recommendable && 'opacity-70',
                            )}
                          >
                            <span className="font-mono font-bold text-text-primary">{s.channel}</span>
                            {s.isCurrent && (
                              <span className="rounded bg-accent/15 px-1 py-px text-[9px] font-bold uppercase text-accent">
                                {t('channelPlan.tagCurrent')}
                              </span>
                            )}
                            {s.isBest && (
                              <span className="rounded bg-ok/15 px-1 py-px text-[9px] font-bold uppercase text-ok">
                                ★ {t('channelPlan.tagBest')}
                              </span>
                            )}
                            {s.dfs && (
                              <span className="rounded bg-elevated px-1 py-px text-[9px] font-bold uppercase text-text-muted">
                                {t('channelPlan.tagDfs')}
                              </span>
                            )}
                            <span
                              className={cn(
                                'font-mono font-bold',
                                s.pct >= 80 ? 'text-ok' : s.pct >= 55 ? 'text-warn' : 'text-danger',
                              )}
                            >
                              {s.pct}
                            </span>
                          </button>
                        ))}
                      </div>
                    ))}
                    {scored.length === 0 && <p className="mt-1 text-caption text-text-muted">{t('channelPlan.noBandScans')}</p>}
                  </div>

                  {/* Leyenda interactiva */}
                  <div className="flex flex-wrap gap-1.5 border-t border-border px-5 py-3">
                    <button
                      onClick={() => {
                        setOnlyMine(false)
                        setHidden(new Set(allKeys))
                      }}
                      className="inline-flex items-center rounded-full border border-border px-2.5 py-1 text-xs font-semibold text-text-secondary transition-colors hover:border-border-strong hover:text-text-primary"
                    >
                      {t('channelPlan.hideAll')}
                    </button>
                    <button
                      onClick={() => {
                        setOnlyMine(false)
                        setHidden(new Set())
                      }}
                      className="inline-flex items-center rounded-full border border-border px-2.5 py-1 text-xs font-semibold text-text-secondary transition-colors hover:border-border-strong hover:text-text-primary"
                    >
                      {t('channelPlan.showAll')}
                    </button>
                    <button
                      onClick={() => setOnlyMine((v) => !v)}
                      className={cn(
                        'inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-semibold transition-colors',
                        onlyMine ? 'border-accent/50 bg-accent/10 text-accent' : 'border-border text-text-secondary hover:border-border-strong hover:text-text-primary',
                      )}
                    >
                      {t('channelPlan.onlyMine')}
                    </button>
                    {nets.map((n) => (
                      <button
                        key={n.key}
                        onClick={() =>
                          setHidden((prev) => {
                            const next = new Set(prev)
                            if (next.has(n.key)) next.delete(n.key)
                            else next.add(n.key)
                            return next
                          })
                        }
                        className={cn(
                          'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-semibold transition-opacity',
                          n.own
                            ? 'border-accent/40 bg-accent/10 text-accent'
                            : 'border-border text-text-secondary hover:border-border-strong',
                          hidden.has(n.key) && 'opacity-40',
                        )}
                      >
                        <span className="inline-block h-2 w-2 rounded-[3px]" style={{ backgroundColor: n.color }} />
                        {n.ssid}
                        <span className="font-mono text-caption font-normal text-text-muted">ch {n.channel}</span>
                      </button>
                    ))}
                  </div>
                </div>
              </div>

              {/* Tabla de redes (solo si hay scans) */}
              {bandScans.length > 0 && (
              <div className="overflow-hidden rounded-2xl border border-border bg-surface">
                <div className="px-5 pt-4">
                  <h2 className="text-sm font-bold text-text-primary">{t('channelPlan.summaryNetworks')}</h2>
                  <p className="mt-0.5 text-caption text-text-muted">{t('channelPlan.tableSub', { n: bandScans.length })}</p>
                </div>
                <div className="mt-2 overflow-x-auto">
                  <table className="w-full border-collapse text-left text-sm">
                    <thead>
                      <tr className="text-label uppercase text-text-muted">
                        <th className="px-5 py-2.5 font-medium">
                          <SortHeader id="signal" label={t('channelPlan.signal')} />
                        </th>
                        <th className="px-3 py-2.5 font-medium">
                          <SortHeader id="ssid" label="SSID" />
                        </th>
                        <th className="px-3 py-2.5 font-medium">
                          <SortHeader id="channel" label={t('channelPlan.currentChannel')} />
                        </th>
                        <th className="px-3 py-2.5 font-medium">{t('channelPlan.colWidth')}</th>
                        <th className="px-3 py-2.5 font-medium">BSSID</th>
                      </tr>
                    </thead>
                    <tbody>
                      {sortedScans.map((s) => {
                          const lvl = signalLevel(s.signal)
                          const color = s.own ? themeColor('--accent') : ssidColor(s.ssid || s.bssid)
                          const key = s.bssid + s.channel
                          return (
                            <tr
                              key={key}
                              onClick={() => setSelected(selected === key ? null : key)}
                              className={cn(
                                'cursor-pointer border-t border-border/60 transition-colors hover:bg-hover',
                                selected === key && 'bg-accent/5',
                              )}
                            >
                              <td className="px-5 py-2.5">
                                <span className="inline-flex items-center gap-2 tabular-nums">
                                  <SignalBars signal={s.signal} own={s.own} />
                                  <b className="text-text-primary">{s.signal}</b>
                                  <small className="text-caption text-text-muted">
                                    dBm · {t(`channelPlan.quality.${lvl === 4 ? 'excellent' : lvl === 3 ? 'good' : lvl === 2 ? 'weak' : 'veryWeak'}`)}
                                  </small>
                                </span>
                              </td>
                              <td className="px-3 py-2.5">
                                <span className="flex items-center gap-2.5">
                                  <span className="inline-block h-2.5 w-2.5 shrink-0 rounded-[3px]" style={{ backgroundColor: color }} />
                                  <span>
                                    <span className="font-semibold text-text-primary">
                                      {s.ssid || <em className="font-normal text-text-muted">{t('channelPlan.hidden')}</em>}
                                    </span>
                                    {s.own && (
                                      <span className="ml-2 rounded-md bg-accent/15 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-accent">
                                        {t('channelPlan.ownNetwork')}
                                      </span>
                                    )}
                                  </span>
                                </span>
                              </td>
                              <td className="px-3 py-2.5 font-mono font-bold text-text-primary">{s.channel}</td>
                              <td className="px-3 py-2.5">
                                {s.own ? (
                                  active.widthMhz > 0 ? (
                                    <span className="rounded-md bg-elevated px-2 py-0.5 text-[11px] font-bold text-text-secondary">
                                      {active.widthMhz} MHz
                                    </span>
                                  ) : (
                                    <span className="text-text-muted">—</span>
                                  )
                                ) : s.widthMhz && s.widthMhz > 0 ? (
                                  <span className="rounded-md bg-elevated px-2 py-0.5 text-[11px] font-bold text-text-secondary">
                                    {s.widthMhz} MHz
                                  </span>
                                ) : (
                                  <span className="text-text-muted">—</span>
                                )}
                              </td>
                              <td className="px-3 py-2.5 font-mono text-caption text-text-muted">{s.bssid}</td>
                            </tr>
                          )
                        })}
                    </tbody>
                  </table>
                </div>
              </div>
              )}
        </>
      )}
    </div>
  )
}
