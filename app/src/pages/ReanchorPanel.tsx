import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { motion, useReducedMotion } from 'framer-motion'
import { CheckCircle2, Loader2, MoveRight, RefreshCw, Wifi, XCircle } from 'lucide-react'
import { cn, fetchJson } from '@/lib/utils'
import { redirectLogin, useNetPulse } from '@/data/DataProvider'
import type { ReanchorRecommendation, ReanchorResponse } from '@/data/types'

interface Props {
  refreshTick: number
}

export default function ReanchorPanel({ refreshTick }: Props) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const { devices, isDemo } = useNetPulse()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [noApi, setNoApi] = useState(false)
  const [daemon, setDaemon] = useState<ReanchorResponse['daemon']>('none')
  const [items, setItems] = useState<ReanchorRecommendation[]>([])
  const [moving, setMoving] = useState<Set<string>>(new Set())
  const [moved, setMoved] = useState<Record<string, string>>({})
  const acRef = useRef<AbortController | null>(null)
  const initial = reduce ? false : { opacity: 0, y: 12 }

  const nameByMac = useMemo(() => {
    const m = new Map<string, string>()
    for (const d of devices) {
      if (d.mac) m.set(d.mac.toUpperCase(), d.name)
    }
    return m
  }, [devices])

  const load = useCallback(async () => {
    acRef.current?.abort()
    const ac = new AbortController()
    acRef.current = ac
    setLoading(true)
    setError(false)
    setNoApi(false)
    const result = await fetchJson<ReanchorResponse>('/api/wifi-reanchor/recommendations', {
      signal: ac.signal,
    })
    if (ac.signal.aborted) return
    if (result.ok) {
      setItems(result.data.recommendations ?? [])
      setDaemon(result.data.daemon ?? 'none')
    } else if (result.kind === 'unauthorized') {
      redirectLogin()
      return
    } else if (result.kind === 'no-api' && isDemo) {
      setNoApi(true)
      setItems([])
    } else {
      setError(true)
      setItems([])
    }
    setLoading(false)
  }, [isDemo])

  useEffect(() => {
    void load()
    return () => acRef.current?.abort()
  }, [load, refreshTick])

  const move = useCallback(
    async (rec: ReanchorRecommendation) => {
      setMoving((prev) => new Set(prev).add(rec.mac))
      setMoved((prev) => {
        const next = { ...prev }
        delete next[rec.mac]
        return next
      })
      try {
        const res = await fetch(`/api/wifi-reanchor/${encodeURIComponent(rec.mac)}/move`, {
          method: 'POST',
        })
        if (res.status === 401) {
          redirectLogin()
          return
        }
        if (!res.ok) {
          const body = (await res.json().catch(() => ({}))) as { message?: string }
          setMoved((prev) => ({ ...prev, [rec.mac]: body.message ?? t('roaming.reanchor.moveFailed') }))
          return
        }
        setMoved((prev) => ({ ...prev, [rec.mac]: 'ok' }))
      } catch {
        setMoved((prev) => ({ ...prev, [rec.mac]: t('roaming.reanchor.moveFailed') }))
      } finally {
        setMoving((prev) => {
          const next = new Set(prev)
          next.delete(rec.mac)
          return next
        })
      }
    },
    [t],
  )

  if (loading && items.length === 0) {
    return (
      <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
        {t('roaming.reanchor.loading')}
      </div>
    )
  }
  if (noApi) {
    return (
      <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
        {t('roaming.reanchor.noApi')}
      </div>
    )
  }
  if (error) {
    return (
      <div className="rounded-2xl border border-border bg-surface p-8 text-center text-caption text-text-muted">
        {t('roaming.reanchor.error')}
      </div>
    )
  }

  const daemonLabel =
    daemon === 'usteer'
      ? t('roaming.daemon.usteer')
      : daemon === 'dawn'
        ? t('roaming.daemon.dawn')
        : null

  return (
    <motion.section
      initial={initial}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, ease: 'easeOut', delay: 0.08 }}
      className="space-y-4"
    >
      <div className="rounded-2xl border border-border bg-surface p-5 md:p-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex items-start gap-2">
            <Wifi className="mt-0.5 h-4 w-4 shrink-0 text-accent" strokeWidth={1.75} />
            <div>
              <h2 className="font-display text-h2 text-text-primary">{t('roaming.reanchor.title')}</h2>
              <p className="mt-0.5 max-w-2xl text-caption text-text-muted">
                {t('roaming.reanchor.description')}
                {daemonLabel && (
                  <>
                    {' '}
                    ({t('roaming.reanchor.source', { daemon: daemonLabel })})
                  </>
                )}
              </p>
            </div>
          </div>
          <button
            onClick={() => void load()}
            disabled={loading}
            className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-elevated px-3 text-sm font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50"
          >
            <RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} strokeWidth={1.75} />
            {t('common.refresh')}
          </button>
        </div>

        <div className="mt-4 rounded-lg border border-border bg-elevated p-4 text-sm text-text-secondary">
          <p>{t('roaming.reanchor.hint')}</p>
        </div>

        {items.length === 0 ? (
          <div className="mt-5 rounded-xl border border-dashed border-border bg-surface p-8 text-center text-caption text-text-muted">
            {t('roaming.reanchor.empty')}
          </div>
        ) : (
          <div className="mt-5 overflow-x-auto">
            <table className="w-full border-separate border-spacing-0 text-left text-sm">
              <thead>
                <tr className="text-label uppercase text-text-muted">
                  <th className="pb-2.5 pr-3 font-medium">{t('roaming.reanchor.colClient')}</th>
                  <th className="pb-2.5 pr-3 font-medium">{t('roaming.reanchor.colCurrent')}</th>
                  <th className="pb-2.5 pr-3 font-medium">{t('roaming.reanchor.colRecommended')}</th>
                  <th className="pb-2.5 pr-3 font-medium">{t('roaming.reanchor.colDelta')}</th>
                  <th className="pb-2.5 pr-3 font-medium">{t('roaming.reanchor.colAction')}</th>
                </tr>
              </thead>
              <tbody>
                {items.map((rec) => {
                  const name = nameByMac.get(rec.mac) ?? rec.mac
                  const done = moved[rec.mac] === 'ok'
                  const failed = moved[rec.mac] && moved[rec.mac] !== 'ok'
                  return (
                    <tr key={rec.mac} className="group">
                      <td className="border-b border-border/60 py-2.5 pr-3">
                        <div className="flex flex-col">
                          <span className="font-medium text-text-primary">{name}</span>
                          {name !== rec.mac && <span className="font-mono text-caption text-text-muted">{rec.mac}</span>}
                        </div>
                      </td>
                      <td className="border-b border-border/60 py-2.5 pr-3">
                        <div className="flex flex-col gap-0.5">
                          <span className="font-medium text-text-primary">{rec.currentHostname}</span>
                          <span className="font-mono text-caption text-text-muted">
                            {rec.currentIface} · {rec.currentSignal} dBm
                          </span>
                        </div>
                      </td>
                      <td className="border-b border-border/60 py-2.5 pr-3">
                        <div className="flex flex-col gap-0.5">
                          <span className="font-medium text-ok">{rec.recommendedHostname}</span>
                          <span className="font-mono text-caption text-text-muted">
                            {rec.recommendedIface} · {rec.recommendedSignal} dBm
                          </span>
                        </div>
                      </td>
                      <td className="border-b border-border/60 py-2.5 pr-3">
                        <span className="inline-flex items-center gap-1 rounded-md bg-ok/15 px-2 py-0.5 text-sm font-medium text-ok ring-1 ring-inset ring-ok/30">
                          <MoveRight className="h-3.5 w-3.5" />
                          +{rec.deltaDbm} dBm
                        </span>
                      </td>
                      <td className="border-b border-border/60 py-2.5 pr-3">
                        {done ? (
                          <span className="inline-flex items-center gap-1.5 text-sm font-medium text-ok">
                            <CheckCircle2 className="h-4 w-4" />
                            {t('roaming.reanchor.moved')}
                          </span>
                        ) : failed ? (
                          <span className="inline-flex items-center gap-1.5 text-sm font-medium text-danger" title={moved[rec.mac]}>
                            <XCircle className="h-4 w-4" />
                            {t('roaming.reanchor.moveFailed')}
                          </span>
                        ) : (
                          <button
                            onClick={() => move(rec)}
                            disabled={moving.has(rec.mac)}
                            className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border bg-elevated px-2.5 text-sm font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50"
                          >
                            {moving.has(rec.mac) ? (
                              <Loader2 className="h-3.5 w-3.5 animate-spin" />
                            ) : (
                              <MoveRight className="h-3.5 w-3.5" />
                            )}
                            {t('roaming.reanchor.move')}
                          </button>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </motion.section>
  )
}
