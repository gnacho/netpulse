import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis } from 'recharts'
import { motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { Activity, ChevronDown, ChevronUp, Loader2 } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { fmtBytes, fmtEs } from '@/data/mock'
import { useNetPulse } from '@/data/DataProvider'
import { SectionHeader } from '@/components/SectionHeader'
import { Sparkline } from '@/components/Sparkline'
import { LatencyGauge } from '@/components/routers/LatencyGauge'
import { relTimeFromTs } from '@/i18n'
import type { RouterExtras } from '@/components/routers/routerExtras'

function WanTooltip({
  active,
  payload,
  label,
}: {
  active?: boolean
  payload?: { dataKey?: string | number; value?: number | string }[]
  label?: string
}) {
  if (!active || !payload?.length) return null
  const down = payload.find((p) => p.dataKey === 'down')?.value
  const up = payload.find((p) => p.dataKey === 'up')?.value
  return (
    <div className="rounded-[10px] border border-border-strong bg-elevated px-3 py-2 shadow-lg">
      <div className="mb-1 font-mono text-caption text-text-muted">{label}</div>
      <div className="flex items-center gap-2 font-mono text-mono-sm text-text-primary">
        <span className="h-2 w-2 rounded-full bg-accent" />↓ {fmtEs(Number(down), 1)} Mbps
      </div>
      <div className="flex items-center gap-2 font-mono text-mono-sm text-text-primary">
        <span className="h-2 w-2 rounded-full bg-tunnel" />↑ {fmtEs(Number(up), 1)} Mbps
      </div>
    </div>
  )
}

/** ④ WAN & Latencia del gateway (router-detail.md §④) — split 7/5.
 *  #1148: el spark de latencia y sus stats salen de la serie REAL 24h
 *  (extras.gatewayLatencySpark, AVG horario de metrics.latency_ms); sin
 *  sonda/historial se pinta "sin datos" en vez de ceros ni mocks. */
export function WanLatency({ extras }: { extras?: RouterExtras }) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const { traffic: trafficByRange, wan } = useNetPulse()
  const data = trafficByRange['24h']

  // Serie real 24h; stats derivadas. Sin muestras (sonda sin datos, unidad
  // nueva) todo cae a undefined y la tarjeta muestra su estado vacío.
  const spark = extras?.gatewayLatencySpark ?? []
  const hasLatency = spark.length > 0
  const avgMs = hasLatency ? spark.reduce((a, b) => a + b, 0) / spark.length : undefined
  const jitterMs = hasLatency
    ? spark.slice(1).reduce((acc, v, i) => acc + Math.abs(v - spark[i]), 0) / (spark.length - 1)
    : undefined
  // La pérdida solo es significativa si la sonda corre (latencia > 0).
  const hasProbe = wan.latencyMs > 0
  const lossPct = hasProbe ? wan.lossPct : undefined
  const fmtMs = (v?: number) => (v === undefined ? '—' : `${fmtEs(v, v < 10 ? 1 : 0)} ms`)

  const WAN_METRICS = [
    { label: t('routerDetail.wan.publicIp'), value: wan.publicIp },
    { label: 'ISP', value: `${wan.isp} · ${t('routerDetail.wan.fiber', { plan: wan.plan.replace(' Mbps', '') })}` },
    { label: t('routerDetail.wan.totalToday'), value: fmtBytes(wan.total24h) },
  ] as const
  return (
    <div className="grid grid-cols-1 gap-4 md:gap-5 lg:col-span-12 lg:grid-cols-12">
      {/* 4a. WAN */}
      <section className="rounded-2xl border border-border bg-surface p-5 md:p-6 lg:col-span-7">
        <SectionHeader title={t('routerDetail.wan.title')} />
        <motion.div
          initial={reduce ? false : { opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.3 }}
          className="mt-4 h-[200px]"
          role="img"
          aria-label={t('routerDetail.wan.chartAria', { down: wan.downMbps, up: wan.upMbps })}
        >
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={data} margin={{ top: 8, right: 4, bottom: 0, left: 4 }}>
              <defs>
                <linearGradient id="wan-detail-down" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="#22D3EE" stopOpacity={0.25} />
                  <stop offset="100%" stopColor="#22D3EE" stopOpacity={0} />
                </linearGradient>
                <linearGradient id="wan-detail-up" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="#A78BFA" stopOpacity={0.2} />
                  <stop offset="100%" stopColor="#A78BFA" stopOpacity={0} />
                </linearGradient>
              </defs>
              <CartesianGrid vertical={false} stroke="rgb(var(--border) / 0.5)" strokeDasharray="3 6" />
              <XAxis
                dataKey="t"
                tickLine={false}
                axisLine={false}
                tick={{ fill: 'rgb(var(--text-muted))', fontSize: 10, fontFamily: '"JetBrains Mono", monospace' }}
                interval="preserveStartEnd"
                minTickGap={40}
              />
              <Tooltip content={<WanTooltip />} cursor={{ stroke: 'rgb(var(--border-strong))', strokeWidth: 1 }} />
              <Area type="monotone" dataKey="down" stroke="#22D3EE" strokeWidth={2} fill="url(#wan-detail-down)" dot={false} animationDuration={900} animationEasing="ease-out" />
              <Area type="monotone" dataKey="up" stroke="#A78BFA" strokeWidth={2} fill="url(#wan-detail-up)" dot={false} animationDuration={900} animationEasing="ease-out" />
            </AreaChart>
          </ResponsiveContainer>
        </motion.div>
        <div className="mt-4 grid grid-cols-3 gap-3 border-t border-border pt-4">
          {WAN_METRICS.map((m) => (
            <div key={m.label}>
              <div className="text-label uppercase text-text-muted">{m.label}</div>
              <div className="mt-0.5 font-mono text-mono-sm font-semibold text-text-primary">{m.value}</div>
            </div>
          ))}
        </div>
      </section>

      {/* 4b. Latencia */}
      <section className="flex flex-col rounded-2xl border border-border bg-surface p-5 md:p-6 lg:col-span-5">
        <SectionHeader title={t('home.latency')} />
        <div className="mt-4 flex flex-1 flex-col items-center justify-center gap-4">
          {hasProbe ? (
            <LatencyGauge valueMs={wan.latencyMs} caption={t('routerDetail.latency.toHost', { host: '1.1.1.1' })} />
          ) : (
            <div
              className="flex h-[160px] w-[160px] flex-col items-center justify-center rounded-full border-2 border-dashed border-border"
              role="img"
              aria-label={t('routerDetail.latency.noData')}
            >
              <span className="font-mono text-3xl font-semibold text-text-muted">—</span>
              <span className="text-caption font-medium uppercase tracking-[0.06em] text-text-muted">ms</span>
            </div>
          )}
          <div className="w-full">
            <div className="mb-1 text-[10px] font-medium uppercase tracking-[0.06em] text-text-muted">{t('routerDetail.latency.24h')}</div>
            {hasLatency ? (
              <Sparkline data={spark} width={560} height={48} color="#34D399" area className="w-full" />
            ) : (
              <div className="flex h-12 items-center justify-center text-caption text-text-muted">
                {t('routerDetail.latency.noData')}
              </div>
            )}
          </div>
          <div className="grid w-full grid-cols-3 gap-3 border-t border-border pt-3.5">
            {[
              [t('home.traffic.average'), fmtMs(avgMs)],
              ['Jitter', fmtMs(jitterMs)],
              [t('home.traffic.loss'), lossPct === undefined ? '—' : `${fmtEs(lossPct, 1)} %`],
            ].map(([k, v]) => (
              <div key={k} className="text-center">
                <div className="text-label uppercase text-text-muted">{k}</div>
                <div className="mt-0.5 font-mono text-mono-sm font-semibold text-text-primary">{v}</div>
              </div>
            ))}
          </div>
        </div>
      </section>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Velocidad medida (issue #511): último speedtest real + histórico de 7 días
// + % del plan contratado (#151) + botón de ejecución manual. Vive dentro de
// la tarjeta WAN porque es la métrica que sustituye a las aproximaciones.
// ---------------------------------------------------------------------------

type SpeedtestItem = {
  /** ISO 8601 (time.Time serializado por el API). */
  ts: string
  downMbps: number
  upMbps: number
  pingMs?: number
  serverName?: string
  /** manual | auto (#744), para el desplegable de recientes (#1092). */
  origin?: string
}

/** Punto de la serie del histórico de speedtests (etiqueta ya formateada). */
type SpeedtestPoint = { t: string; down: number; up: number }

function toPoints(items: SpeedtestItem[]): SpeedtestPoint[] {
  const fmt = new Intl.DateTimeFormat(undefined, { weekday: 'short', hour: '2-digit' })
  return items.map((it) => ({ t: fmt.format(new Date(it.ts)), down: it.downMbps, up: it.upMbps }))
}

/** Gráfica del histórico de velocidad medida: SOLO la serie, sin botón de
 *  ejecutar test ni línea de valores. La reutiliza SpeedtestStrip, que vive
 *  en el overview (restaurado en #1092) y vivió en el detalle del gateway.
 *  Lleva sus propios gradientes (antes dependía de los ids del chart WAN del
 *  detalle). */
export function SpeedtestHistoryChart({ points, height = 64 }: { points: SpeedtestPoint[]; height?: number }) {
  const { t } = useTranslation()
  if (points.length <= 1) return null
  return (
    <div className="mt-2" style={{ height }} role="img" aria-label={t('routerDetail.wan.speedtestTitle')}>
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={points} margin={{ top: 6, right: 4, bottom: 0, left: 4 }}>
          <defs>
            <linearGradient id="speedtest-hist-down" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="#22D3EE" stopOpacity={0.22} />
              <stop offset="100%" stopColor="#22D3EE" stopOpacity={0} />
            </linearGradient>
            <linearGradient id="speedtest-hist-up" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="#A78BFA" stopOpacity={0.18} />
              <stop offset="100%" stopColor="#A78BFA" stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid vertical={false} stroke="rgb(var(--border) / 0.4)" strokeDasharray="3 6" />
          <XAxis
            dataKey="t"
            tickLine={false}
            axisLine={false}
            interval="preserveStartEnd"
            minTickGap={48}
            tick={{ fill: 'rgb(var(--text-muted))', fontSize: 9, fontFamily: '"JetBrains Mono", monospace' }}
          />
          <Tooltip content={<WanTooltip />} cursor={{ stroke: 'rgb(var(--border-strong))', strokeWidth: 1 }} />
          <Area type="stepAfter" dataKey="down" stroke="#22D3EE" strokeWidth={1.5} fill="url(#speedtest-hist-down)" dot={false} />
          <Area type="stepAfter" dataKey="up" stroke="#A78BFA" strokeWidth={1.5} fill="url(#speedtest-hist-up)" dot={false} />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}

function useSpeedtestState() {
  const [running, setRunning] = useState(false)
  const [lastError, setLastError] = useState<string | null>(null)
  const [last, setLast] = useState<SpeedtestItem | null>(null)
  const [points, setPoints] = useState<SpeedtestPoint[]>([])
  const [items, setItems] = useState<SpeedtestItem[]>([])
  const [starting, setStarting] = useState(false)

  const load = useCallback(async () => {
    try {
      const [stRes, hRes] = await Promise.all([
        fetch('/api/speedtest/status'),
        fetch('/api/speedtest/history?hours=168'),
      ])
      if (stRes.ok) {
        const st = await stRes.json()
        setRunning(!!st.running)
        setLastError(st.lastError || null)
        setLast(st.last ?? null)
      }
      if (hRes.ok) {
        const h = await hRes.json()
        const list: SpeedtestItem[] = h.items ?? []
        setPoints(toPoints(list))
        // Recientes para el desplegable (#1092): los 10 últimos, más nuevo
        // arriba. Los mismos datos que ya trae el histórico.
        setItems([...list].slice(-10).reverse())
      }
    } catch {
      // Sin red / sesión caducada: se conserva el último estado pintado.
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  // Mientras corre un test, refresco cada 3 s hasta que termina.
  useEffect(() => {
    if (!running) return
    const id = window.setInterval(() => void load(), 3000)
    return () => window.clearInterval(id)
  }, [running, load])

  const run = useCallback(async () => {
    setStarting(true)
    try {
      const res = await fetch('/api/speedtest/run', { method: 'POST' })
      if (res.ok || res.status === 409) setRunning(true)
      else if (res.status === 503) setLastError('unavailable')
    } catch {
      /* el siguiente poll reflejará el estado real */
    } finally {
      setStarting(false)
    }
  }, [])

  return { running, lastError, last, points, items, starting, run, reload: load }
}

export function SpeedtestStrip({ contractDown }: { contractDown?: number }) {
  const { t, i18n } = useTranslation()
  const { running, lastError, last, points, items, starting, run } = useSpeedtestState()
  const [recentOpen, setRecentOpen] = useState(false)

  const busy = running || starting
  // El API serializa ts como ISO 8601; relTimeFromTs espera unix SEGUNDOS
  // (SPEC-ALERTAS, igual que las alertas).
  const when = last ? (relTimeFromTs(Math.floor(new Date(last.ts).getTime() / 1000)) ?? '') : ''
  const planPct =
    contractDown && contractDown > 0 && last ? Math.round((last.downMbps / contractDown) * 100) : null
  const recentFmt = new Intl.DateTimeFormat(i18n.language?.startsWith('en') ? 'en-GB' : 'es-ES', {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })

  return (
    <div className="mt-4 border-t border-border pt-4" aria-label={t('routerDetail.wan.speedtestTitle')}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-1.5">
          <Activity className="h-4 w-4 text-accent" aria-hidden />
          <span className="text-label uppercase text-text-muted">{t('routerDetail.wan.speedtestTitle')}</span>
          {planPct !== null && (
            <span
              className={`ml-1 rounded-full border px-2 py-0.5 font-mono text-[11px] ${
                planPct >= 80
                  ? 'border-ok/40 bg-ok/10 text-ok'
                  : planPct >= 50
                    ? 'border-warn/40 bg-warn/10 text-warn'
                    : 'border-danger/40 bg-danger/10 text-danger'
              }`}
            >
              {t('routerDetail.wan.planPct', { pct: planPct })}
            </span>
          )}
        </div>
        <button
          type="button"
          onClick={() => void run()}
          disabled={busy}
          aria-label={t('routerDetail.wan.speedtestRun')}
          className="inline-flex h-8 cursor-pointer items-center gap-1.5 rounded-xl border border-border bg-canvas px-3 text-[13px] font-medium text-text-primary transition-colors hover:border-border-strong disabled:cursor-not-allowed disabled:opacity-60"
        >
          {busy ? (
            <>
              <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
              {t('routerDetail.wan.speedtestRunning')}
            </>
          ) : (
            t('routerDetail.wan.speedtestRun')
          )}
        </button>
      </div>

      {lastError && lastError !== 'unavailable' && (
        <p role="alert" className="mt-2 text-caption text-danger">
          {t('routerDetail.wan.speedtestFailed')}
        </p>
      )}

      {last ? (
        <>
          <div className="mt-2 flex flex-wrap items-baseline gap-x-3 gap-y-1 font-mono text-mono-sm text-text-primary">
            <span>
              <span className="text-accent">↓</span> {fmtEs(last.downMbps, 0)} Mbps
            </span>
            <span>
              <span className="text-tunnel">↑</span> {fmtEs(last.upMbps, 0)} Mbps
            </span>
            {last.pingMs !== undefined && <span className="text-text-muted">{fmtEs(last.pingMs, 0)} ms</span>}
            {when && <span className="text-text-muted">· {when}</span>}
            {last.serverName && (
              <span className="text-text-muted">· {t('routerDetail.wan.speedtestServer', { name: last.serverName })}</span>
            )}
          </div>
          <SpeedtestHistoryChart points={points} />
        </>
      ) : (
        <p className="mt-2 text-caption text-text-muted">
          {lastError === 'unavailable' ? t('settings.speedtest.unavailable') : t('routerDetail.wan.speedtestEmpty')}
        </p>
      )}

      {/* Tests recientes (#1092): desplegable compacto con los 10 últimos
          resultados (origen, bajada/subida, ping y servidor). Los datos ya
          los trae /api/speedtest/history. */}
      {items.length > 0 && (
        <div className="mt-1">
          <button
            type="button"
            onClick={() => setRecentOpen((v) => !v)}
            aria-expanded={recentOpen}
            className="inline-flex cursor-pointer items-center gap-1 text-caption text-accent hover:underline"
          >
            {recentOpen ? (
              <ChevronUp className="h-3.5 w-3.5" strokeWidth={1.75} aria-hidden />
            ) : (
              <ChevronDown className="h-3.5 w-3.5" strokeWidth={1.75} aria-hidden />
            )}
            {recentOpen ? t('settings.speedtest.recentHide') : t('settings.speedtest.recentShow')}
          </button>
          {recentOpen && (
            <div className="mt-1.5 overflow-x-auto">
              <table className="w-full text-caption">
                <thead>
                  <tr className="text-left text-text-muted">
                    <th className="py-1 pr-3 font-medium">{t('settings.speedtest.colWhen')}</th>
                    <th className="py-1 pr-3 font-medium">{t('settings.speedtest.colOrigin')}</th>
                    <th className="py-1 pr-3 font-medium">↓</th>
                    <th className="py-1 pr-3 font-medium">↑</th>
                    <th className="py-1 pr-3 font-medium">ping</th>
                    <th className="py-1 font-medium">{t('settings.speedtest.colServer')}</th>
                  </tr>
                </thead>
                <tbody className="font-mono text-text-secondary">
                  {items.map((it, idx) => (
                    <tr key={`${it.ts}-${idx}`} className="border-t border-border/60">
                      <td className="py-1.5 pr-3">
                        {(() => {
                          try {
                            return recentFmt.format(new Date(it.ts))
                          } catch {
                            return it.ts
                          }
                        })()}
                      </td>
                      <td className="py-1.5 pr-3">
                        <span
                          className={`rounded-full border px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide ${
                            it.origin === 'manual'
                              ? 'border-accent/40 bg-accent/10 text-accent'
                              : 'border-border bg-surface text-text-muted'
                          }`}
                        >
                          {it.origin === 'manual' ? t('settings.speedtest.originManual') : t('settings.speedtest.originAuto')}
                        </span>
                      </td>
                      <td className="py-1.5 pr-3 text-accent">{fmtEs(it.downMbps, 1)}</td>
                      <td className="py-1.5 pr-3 text-tunnel">{fmtEs(it.upMbps, 1)}</td>
                      <td className="py-1.5 pr-3">{it.pingMs !== undefined ? `${fmtEs(it.pingMs, 0)} ms` : '-'}</td>
                      <td className="py-1.5">{it.serverName ?? '-'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
