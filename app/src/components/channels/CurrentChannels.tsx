// CurrentChannels (#1214): la lente EN VIVO de la página Canales - lo que
// era el tab "Canales actuales" de Itinerancia WiFi, ahora para la unidad
// seleccionada del picker. Dos fuentes: GET /api/survey (ocupación y ruido
// por canal, iw survey dump) y el channel-plan de la unidad (vecinos
// recientes de wifi_scans) para superponer quién ocupa cada canal.
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { motion, useReducedMotion } from 'framer-motion'
import { Wifi } from 'lucide-react'
import { fetchJson, cn } from '@/lib/utils'
import { ssidColor } from '@/lib/ssidColor'

// Tipos del contrato GET /api/survey (server-go/internal/adapters/types.go).
interface SurveyChannel {
  freq: number
  channel: number
  inUse: boolean
  noiseDbm: number
  busyPct: number
  rxPct: number
  txPct: number
}
interface SurveyRadio {
  device: string
  band: string
  channels: SurveyChannel[]
}
interface SurveyRouter {
  routerId: string
  name: string
  available: boolean
  radios: SurveyRadio[]
}
interface SurveyOverview {
  available: boolean
  routers: SurveyRouter[]
}

// Neighbor scan del channel-plan (#538): red visible en un canal.
interface Scan {
  iface: string
  bssid: string
  ssid: string
  channel: number
  freq: number
  signal: number
  routerId: string
  own?: boolean
}

interface PlanRadio {
  name: string
  channel: number
  widthMhz: number
  band?: string
}

type SurveyBand = 'all' | '2.4 GHz' | '5 GHz'

function signalDot(signal: number): string {
  if (signal >= -55) return 'bg-ok'
  if (signal >= -70) return 'bg-warn'
  return 'bg-danger'
}

// bandOfFreq: 2.4 / 5 / 6 GHz a partir de la frecuencia del canal.
function bandOfFreq(freq: number): string {
  if (freq >= 2412 && freq <= 2484) return '2.4 GHz'
  if (freq >= 5180 && freq <= 5885) return '5 GHz'
  if (freq >= 5955) return '6 GHz'
  return `${freq} MHz`
}

// bandChannels: lista completa de canales de una banda, como el channel
// analysis del LuCI (2.4 → 1-13; 5 GHz → canales típicos UNII-1..4).
function bandChannels(band: string): number[] {
  if (band === '2.4 GHz') return Array.from({ length: 13 }, (_, i) => i + 1)
  if (band === '5 GHz') return [36, 40, 44, 48, 52, 56, 60, 64, 68, 100, 104, 108, 112, 116, 120, 124, 128, 132, 136, 140, 149, 153, 157, 161, 165]
  return []
}

// ChannelAnalysisChart (#542): gráfico de cascada de la ocupación por canal,
// réplica del "channel analysis" del LuCI. Dibuja UNA banda con TODOS sus
// canales en el eje X (slots uniformes, incluidos los vacíos), eje Y = señal
// dBm (-92 abajo, -20 arriba). Cada red es una "montaña" centrada en su canal
// (altura = señal); la del propio router (canal in use) se dibuja a -25 dBm.
function ChannelAnalysisChart({ bandName, channels, scans }: { bandName: string; channels: number[]; scans: Scan[] }) {
  const { t } = useTranslation()
  const W = 900
  const H = 240
  const padL = 46
  const padR = 16
  const padT = 18
  const padB = 26
  const dBmMax = -20
  const dBmMin = -92
  if (channels.length === 0 || scans.length === 0) {
    return <div className="text-caption text-text-muted">{t('roaming.survey.empty')}</div>
  }
  const innerW = W - padL - padR
  const slot = innerW / channels.length
  const cx = (i: number) => padL + slot * i + slot / 2
  const y = (dbm: number) => padT + ((dBmMax - dbm) / (dBmMax - dBmMin)) * (H - padT - padB)
  const gridDbm = [-25, -50, -75, -92]
  const half = Math.min(slot * 0.45, 22)
  const byChan = (c: number) => scans.filter((s) => s.channel === c)

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" role="img" aria-label={`${bandName} · ${t('roaming.survey.title')}`}>
      {gridDbm.map((d) => (
        <g key={d}>
          <line x1={padL} y1={y(d)} x2={W - padR} y2={y(d)} stroke="currentColor" className="text-border" strokeWidth={0.5} />
          <text x={padL - 6} y={y(d) + 3} textAnchor="end" className="fill-text-muted" fontSize={9}>
            {d} dBm
          </text>
        </g>
      ))}
      {channels.map((c, i) =>
        byChan(c).slice().sort((a, b) => a.signal - b.signal).map((s) => {
          const x0 = cx(i)
          const cy = y(s.signal)
          const base = y(dBmMin)
          const col = s.own ? ssidColor('') : ssidColor(s.ssid || s.bssid)
          return (
            <path
              key={`${s.bssid || 'own'}-${c}`}
              d={`M ${x0 - half} ${base + 8} C ${x0 - half} ${(base + 8 + cy) / 2}, ${x0 - half * 0.5} ${cy}, ${x0} ${cy} C ${x0 + half * 0.5} ${cy}, ${x0 + half} ${(base + 8 + cy) / 2}, ${x0 + half} ${base + 8} Z`}
              fill={s.own ? '#8b5cf6' : col}
              fillOpacity={s.own ? 0.55 : 0.28}
              stroke={s.own ? '#8b5cf6' : col}
              strokeOpacity={s.own ? 1 : 0.5}
              strokeWidth={s.own ? 2 : 1}
            >
              <title>{`${s.ssid || s.bssid}${s.own ? ' (red propia)' : ''} · ${c} · ${s.signal} dBm`}</title>
            </path>
          )
        })
      )}
      {channels.map((c, i) => (
        <text key={c} x={cx(i)} y={H - 6} textAnchor="middle" className="fill-text-muted" fontSize={9}>
          {c}
        </text>
      ))}
    </svg>
  )
}

export function CurrentChannels({
  routerId,
  routerName,
  band: bandProp,
  onBandChange,
}: {
  routerId: string
  routerName?: string
  /** #1283: banda controlada por el selector superior de Canales. Cuando
   *  llega, el selector interno se oculta (el de arriba manda). */
  band?: '2.4 GHz' | '5 GHz'
  onBandChange?: (b: '2.4 GHz' | '5 GHz') => void
}) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const [overview, setOverview] = useState<SurveyOverview | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(false)
  const [noApi, setNoApi] = useState(false)
  const controlled = bandProp !== undefined
  const [internalBand, setInternalBand] = useState<SurveyBand>('all')
  const band: SurveyBand = controlled ? (bandProp as SurveyBand) : internalBand
  const setBand = (b: SurveyBand) => {
    if (controlled) onBandChange?.(b as '2.4 GHz' | '5 GHz')
    else setInternalBand(b)
  }
  // Vecinos recientes del channel-plan de la unidad (capa opcional sobre el
  // survey, #538).
  const [scans, setScans] = useState<Scan[]>([])
  const [planRadios, setPlanRadios] = useState<PlanRadio[]>([])

  useEffect(() => {
    if (!routerId) return
    let active = true
    setLoading(true)
    setError(false)
    setNoApi(false)
    setOverview(null)
    // El survey es un barrido SSH por unidad con wifi: se pide una vez al
    // montar (igual que hacía el tab de Itinerancia al activarse).
    fetchJson<SurveyOverview>('/api/survey')
      .then((res) => {
        if (active && res.ok) setOverview(res.data)
      })
      .catch(() => {
        if (active) setError(true)
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    fetchJson<{ scans?: Scan[]; radios?: PlanRadio[] }>(`/api/wifi/channel-plan?routerId=${encodeURIComponent(routerId)}`)
      .then((res) => {
        if (active && res.ok) {
          setScans(res.data.scans ?? [])
          setPlanRadios(res.data.radios ?? [])
        }
      })
      .catch(() => { /* capa opcional */ })
    return () => { active = false }
  }, [routerId])

  const initial = reduce ? false : { opacity: 0, y: 12 }
  const bandOptions: SurveyBand[] = ['2.4 GHz', '5 GHz']
  const activeBand = band === '5 GHz' ? '5 GHz' : '2.4 GHz'
  const channels = bandChannels(activeBand)
  const router = (overview?.routers ?? []).find((r) => r.routerId === routerId)

  if (loading && !overview) {
    return (
      <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
        {t('roaming.loading')}
      </div>
    )
  }
  if (noApi) {
    return (
      <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
        {t('roaming.noApi')}
      </div>
    )
  }
  if (error) {
    return (
      <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
        {t('roaming.error')}
      </div>
    )
  }
  // La unidad seleccionada no participa en el survey (agent-only/SNMP sin
  // SSH, o el gateway en flotas con APs): estado vacío propio.
  if (!overview || !overview.available || !router) {
    return (
      <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
        {t('roaming.survey.empty')}
      </div>
    )
  }

  const name = routerName || router.name
  const allScans = (scans ?? []).filter((s) => s.signal >= -90)
  // Red propia: montaña a -25 dBm en el canal en uso de cada radio.
  const ownScans: Scan[] = (router.radios ?? []).flatMap((radio) => {
    if (radio.band !== activeBand) return []
    const ch = radio.channels.find((c) => c.inUse)
    if (!ch) return []
    return [{ iface: radio.device, bssid: `own-${radio.device}`, ssid: `${t('roaming.survey.ownLabel')} (${name})`, channel: ch.channel, freq: ch.freq, signal: -25, routerId: router.routerId, own: true }]
  })
  const bandScans = allScans.filter((s) => bandOfFreq(s.freq) === activeBand)
  const cardScans = [...ownScans, ...bandScans]
  const noData = allScans.length === 0 && ownScans.length === 0
  // #602: ancho de canal de la radio propia, cruzando con el channel-plan.
  const ownWidth = (bnd: string, channel: number): number => {
    const r = planRadios.find((x) => x.name === bnd && x.channel === channel)
    return r && r.widthMhz > 0 ? r.widthMhz : 0
  }

  return (
    <motion.section
      initial={initial}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, ease: 'easeOut' }}
      className="space-y-4"
    >
      {/* Header + filtro banda */}
      <div className="rounded-2xl border border-border bg-surface p-5 md:p-6">
        {!controlled && (
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="flex items-start gap-2">
              <Wifi className="mt-0.5 h-4 w-4 shrink-0 text-accent" strokeWidth={1.75} />
              <div>
                <h2 className="font-display text-h2 text-text-primary">{t('roaming.survey.title')}</h2>
              </div>
            </div>
            <div className="inline-flex items-center gap-1 rounded-lg border border-border bg-elevated p-1" role="group" aria-label={t('roaming.matrix.filterBand')}>
              {bandOptions.map((b) => (
                <button
                  key={b}
                  onClick={() => setBand(b)}
                  className={cn(
                    'rounded-md px-2.5 py-1 text-caption font-medium transition-colors',
                    band === b ? 'bg-accent/15 text-accent' : 'text-text-muted hover:text-text-secondary',
                  )}
                >
                  {b}
                </button>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* La tarjeta de la unidad seleccionada */}
      <div className="rounded-2xl border border-border bg-surface p-5 md:p-6">
        <h3 className="mb-2 font-display text-h3 text-text-primary">{name}</h3>
        <div className="mb-1 font-mono text-caption text-text-muted">{activeBand}</div>
        {!router.available && (
          <p className="rounded-lg bg-warn/10 px-3 py-2 text-caption text-warn">
            {t('roaming.survey.noScans')}
          </p>
        )}
        {noData && (
          <p className="rounded-lg bg-warn/10 px-3 py-2 text-caption text-warn">
            {t('roaming.survey.noScans')}
          </p>
        )}
        {!noData && router.available && (
          <>
            <ChannelAnalysisChart bandName={activeBand} channels={channels} scans={cardScans} />
            {bandScans.length > 0 && (
              <div className="mt-4 overflow-x-auto">
                <table className="w-full border-separate border-spacing-0 text-left text-sm">
                  <thead>
                    <tr className="text-label uppercase text-text-muted">
                      <th className="pb-2 pr-3 font-medium">{t('roaming.survey.colSignal')}</th>
                      <th className="pb-2 pr-3 font-medium">{t('roaming.survey.colSsid')}</th>
                      <th className="pb-2 pr-3 font-medium">{t('roaming.survey.colChannel')}</th>
                      <th className="pb-2 pr-3 font-medium">{t('roaming.survey.colBssid')}</th>
                      <th className="pb-2 pr-3 font-medium">{t('roaming.survey.colFreq')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {cardScans.slice().sort((a, b) => b.signal - a.signal).map((s) => (
                      <tr key={s.bssid + s.channel}>
                        <td className="border-b border-border/60 py-2 pr-3">
                          <span className="inline-flex items-center gap-2">
                            <span className={cn('inline-block h-2 w-8 rounded-full', s.own ? 'bg-accent' : signalDot(s.signal))} />
                            <span className="font-mono text-mono-sm text-text-secondary">{s.signal} dBm</span>
                          </span>
                        </td>
                        <td className="border-b border-border/60 py-2 pr-3">
                          <span className="inline-flex items-center gap-2">
                            <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ backgroundColor: s.own ? '#8b5cf6' : ssidColor(s.ssid || s.bssid) }} />
                            <span className="text-text-primary">{s.ssid || <em className="text-text-muted">hidden</em>}</span>
                          </span>
                        </td>
                        <td className="border-b border-border/60 py-2 pr-3 font-mono text-text-primary">
                          {s.channel}
                          {s.own && (() => {
                            const w = ownWidth(activeBand, s.channel)
                            return w > 0 ? <span className="ml-1 text-caption text-text-muted">· {w} MHz</span> : null
                          })()}
                        </td>
                        <td className="border-b border-border/60 py-2 pr-3 font-mono text-caption text-text-muted">{s.bssid}</td>
                        <td className="border-b border-border/60 py-2 pr-3 font-mono text-caption text-text-muted">{s.freq} MHz</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </>
        )}
      </div>

      {/* Leyenda */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-caption text-text-muted">
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block h-2.5 w-2.5 rounded-sm bg-ok/40 ring-1 ring-inset ring-ok/40" />
          {t('roaming.survey.legendFree')}
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block h-2.5 w-2.5 rounded-sm bg-warn/40 ring-1 ring-inset ring-warn/40" />
          {t('roaming.survey.legendBusy')}
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block h-2.5 w-2.5 rounded-sm bg-danger/40 ring-1 ring-inset ring-danger/40" />
          {t('roaming.survey.legendCongested')}
        </span>
      </div>
    </motion.section>
  )
}
