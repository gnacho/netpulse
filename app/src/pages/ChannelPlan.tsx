import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNetPulse } from '@/data/DataProvider'
import { Signal, Radio as RadioIcon, AlertCircle, CheckCircle2, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'

interface Scan {
  iface: string
  bssid: string
  ssid: string
  channel: number
  freq: number
  signal: number
  routerId: string
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
}

interface ChannelPlanData {
  routerId: string
  radios: RadioRec[]
  scans: Scan[]
}

interface RadioApplyState {
  busy: boolean
  status?: 'applied' | 'failed'
  error?: string
  prevChannel?: number
}

function bandFromFreq(freq: number): string {
  if (freq >= 2412 && freq <= 2484) return '2.4 GHz'
  if (freq >= 5180 && freq <= 5885) return '5 GHz'
  if (freq >= 5955) return '6 GHz'
  return `${freq} MHz`
}

export default function ChannelPlan() {
  const { t } = useTranslation()
  const { routers } = useNetPulse()
  const [routerId, setRouterId] = useState('')
  const [data, setData] = useState<ChannelPlanData | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [applyState, setApplyState] = useState<Record<string, RadioApplyState>>({})
  const [confirmTarget, setConfirmTarget] = useState<RadioRec | null>(null)
  const pollTimer = useRef<ReturnType<typeof setInterval> | null>(null)

  const sortedRouters = useMemo(() => {
    return [...routers].sort((a, b) => (a.roleBadge === 'Principal' ? -1 : 1) || a.name.localeCompare(b.name))
  }, [routers])

  useEffect(() => {
    if (!routerId && sortedRouters.length > 0) {
      setRouterId(sortedRouters[0]!.id)
    }
  }, [sortedRouters, routerId])

  const loadPlan = useCallback((rid: string) => {
    setLoading(true)
    setError('')
    fetch(`/api/wifi/channel-plan?routerId=${encodeURIComponent(rid)}`)
      .then(async (res) => {
        if (!res.ok) throw new Error(await res.text())
        return res.json()
      })
      .then((d) => setData({ ...d, radios: d.radios ?? [], scans: d.scans ?? [] }))
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    if (!routerId) return
    loadPlan(routerId)
  }, [routerId, loadPlan])

  useEffect(() => {
    return () => {
      if (pollTimer.current) clearInterval(pollTimer.current)
    }
  }, [])

  const radioKey = (r: RadioRec) => r.section || r.name

  const applyChannel = useCallback(
    (r: RadioRec, target: number, prevChannel: number) => {
      const key = radioKey(r)
      setApplyState((s) => ({ ...s, [key]: { busy: true } }))
      const ops = [
        {
          kind: 'uci_set',
          args: { config: 'wireless', section: r.section, option: 'channel', value: String(target) },
          desc: `channel ${r.section} -> ${target}`,
        },
        { kind: 'uci_commit', args: { config: 'wireless' }, desc: 'commit wireless' },
        { kind: 'service', args: { name: 'network', action: 'restart' }, desc: 'reload network' },
      ]
      const fail = (err: string) => {
        if (pollTimer.current) clearInterval(pollTimer.current)
        pollTimer.current = null
        setApplyState((s) => ({ ...s, [key]: { busy: false, status: 'failed', error: err } }))
      }
      fetch('/api/plans', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ routerId, resource: 'wireless', diff: ops }),
      })
        .then(async (res) => {
          if (!res.ok) throw new Error(await res.text())
          return res.json()
        })
        .then((plan) =>
          fetch(`/api/plans/${plan.id}/apply`, { method: 'POST' }).then(async (res) => {
            if (!res.ok) throw new Error(await res.text())
          }).then(() => plan),
        )
        .then((plan) => {
          let ticks = 0
          pollTimer.current = setInterval(() => {
            ticks++
            if (ticks > 60) return fail(t('channelPlan.timeout'))
            fetch(`/api/plans/${plan.id}`)
              .then(async (res) => {
                if (!res.ok) throw new Error(await res.text())
                return res.json()
              })
              .then((p: { status?: string; result?: { error?: string } }) => {
                if (p.status === 'applied') {
                  if (pollTimer.current) clearInterval(pollTimer.current)
                  pollTimer.current = null
                  setApplyState((s) => ({ ...s, [key]: { busy: false, status: 'applied', prevChannel } }))
                  loadPlan(routerId)
                } else if (p.status === 'failed' || p.status === 'rolled_back') {
                  fail(p.result?.error || t('channelPlan.failedState'))
                }
              })
              .catch((e) => fail(String(e)))
          }, 3000)
        })
        .catch((e) => fail(String(e)))
    },
    [routerId, loadPlan, t],
  )

  const onConfirmApply = () => {
    if (!confirmTarget) return
    const r = confirmTarget
    setConfirmTarget(null)
    if (r.recommended > 0 && r.recommended !== r.channel) {
      applyChannel(r, r.recommended, r.channel)
    }
  }

  const onRevert = (r: RadioRec) => {
    const key = radioKey(r)
    const prev = applyState[key]?.prevChannel
    if (!prev) return
    applyChannel(r, prev, r.channel)
  }

  return (
    <div className="space-y-4 md:space-y-5">
      <header>
        <h1 className="font-display text-h1 text-text-primary">{t('channelPlan.title')}</h1>
        <p className="mt-0.5 text-sm text-text-secondary">{t('channelPlan.subtitle')}</p>
      </header>

      <div className="rounded-2xl border border-border bg-surface p-5">
        <label className="flex flex-col gap-1">
          <span className="text-caption font-medium text-text-secondary">{t('channelPlan.router')}</span>
          <select
            value={routerId}
            onChange={(e) => {
              setApplyState({})
              setRouterId(e.target.value)
            }}
            className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
          >
            {sortedRouters.map((r) => (
              <option key={r.id} value={r.id}>
                {r.name}
                {r.roleBadge === 'Principal' ? ` ${t('channelPlan.gateway')}` : ''}
              </option>
            ))}
          </select>
        </label>
      </div>

      {error && (
        <div className="flex items-start gap-3 rounded-xl border border-rose-500/40 bg-rose-500/10 px-4 py-3 text-sm text-rose-600 dark:text-rose-400">
          <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
          <span>{error}</span>
        </div>
      )}

      {loading && (
        <div className="rounded-2xl border border-border bg-surface p-8 text-center text-sm text-text-secondary">
          {t('common.loading')}
        </div>
      )}

      {!loading && data && (
        <>
          {data.radios.length === 0 ? (
            <div className="rounded-2xl border border-border bg-surface p-8 text-center text-sm text-text-secondary">
              {t('channelPlan.noRadios')}
            </div>
          ) : (
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {data.radios.map((r, idx) => {
                const key = radioKey(r)
                const st: RadioApplyState = applyState[key] ?? { busy: false }
                const canApply = !!r.section && r.recommended > 0 && r.recommended !== r.channel && !st.busy
                const showScore = r.bestScore > 0 && r.currentScore > 0 && r.currentScore < 900000
                return (
                  <div
                    key={idx}
                    className={cn(
                      'rounded-2xl border border-border bg-surface p-5',
                      r.recommended !== r.channel && r.recommended > 0 && 'border-accent/40',
                    )}
                  >
                    <div className="mb-3 flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2">
                        <RadioIcon className="h-4 w-4 text-accent" strokeWidth={1.75} />
                        <h3 className="text-sm font-semibold text-text-primary">{r.name}</h3>
                      </div>
                      {showScore && (
                        <span className="text-caption text-text-muted">
                          {t('channelPlan.score')}: {r.currentScore} → {r.bestScore}
                        </span>
                      )}
                    </div>
                    <div className="grid grid-cols-2 gap-4">
                      <div>
                        <div className="text-caption text-text-muted">{t('channelPlan.currentChannel')}</div>
                        <div className="text-2xl font-bold text-text-primary">{r.channel}</div>
                      </div>
                      <div>
                        <div className="text-caption text-text-muted">{t('channelPlan.recommendedChannel')}</div>
                        <div className={cn('text-2xl font-bold', r.recommended === r.channel ? 'text-text-primary' : 'text-accent')}>
                          {r.recommended > 0 ? r.recommended : '-'}
                        </div>
                      </div>
                    </div>
                    <div className="mt-4 flex flex-wrap items-center gap-2">
                      {canApply ? (
                        <Button size="sm" onClick={() => setConfirmTarget(r)}>
                          {t('channelPlan.apply')}
                        </Button>
                      ) : r.recommended > 0 && r.recommended !== r.channel && !r.section ? (
                        <span className="text-caption text-text-muted">{t('channelPlan.updateAgent')}</span>
                      ) : null}
                      {st.busy && (
                        <span className="inline-flex items-center gap-1.5 text-caption text-text-secondary">
                          <Loader2 className="h-3.5 w-3.5 animate-spin" strokeWidth={1.75} />
                          {t('channelPlan.applyingState')}
                        </span>
                      )}
                      {st.status === 'applied' && st.prevChannel && (
                        <>
                          <span className="inline-flex items-center gap-1.5 text-caption text-emerald-600 dark:text-emerald-400">
                            <CheckCircle2 className="h-3.5 w-3.5" strokeWidth={1.75} />
                            {t('channelPlan.appliedState')}
                          </span>
                          <Button size="sm" variant="outline" disabled={st.busy} onClick={() => onRevert(r)}>
                            {t('channelPlan.revertTo', { ch: st.prevChannel })}
                          </Button>
                        </>
                      )}
                      {st.status === 'failed' && (
                        <span className="inline-flex items-center gap-1.5 text-caption text-rose-600 dark:text-rose-400">
                          <AlertCircle className="h-3.5 w-3.5" strokeWidth={1.75} />
                          {t('channelPlan.failedState')}
                          {st.error ? `: ${st.error}` : ''}
                        </span>
                      )}
                    </div>
                  </div>
                )
              })}
            </div>
          )}

          <div className="rounded-2xl border border-border bg-surface p-5">
            <h2 className="mb-3 text-sm font-semibold text-text-primary">{t('channelPlan.neighbors')}</h2>
            {data.scans.length === 0 ? (
              <div className="py-4 text-sm text-text-secondary">{t('channelPlan.noScans')}</div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead className="border-b border-border text-left text-caption text-text-muted">
                    <tr>
                      <th className="py-2 pr-4">SSID</th>
                      <th className="py-2 pr-4">BSSID</th>
                      <th className="py-2 pr-4">{t('channelPlan.band')}</th>
                      <th className="py-2 pr-4">{t('channelPlan.currentChannel')}</th>
                      <th className="py-2 pr-4">{t('channelPlan.signal')}</th>
                    </tr>
                  </thead>
                  <tbody className="text-text-primary">
                    {data.scans.map((s, idx) => (
                      <tr key={idx} className="border-b border-border/50 last:border-0">
                        <td className="py-2 pr-4 font-medium">{s.ssid || t('common.unknown')}</td>
                        <td className="py-2 pr-4 font-mono text-text-secondary">{s.bssid}</td>
                        <td className="py-2 pr-4">{bandFromFreq(s.freq)}</td>
                        <td className="py-2 pr-4">{s.channel}</td>
                        <td className="py-2 pr-4">
                          <span className="inline-flex items-center gap-1">
                            <Signal className="h-3.5 w-3.5" strokeWidth={1.75} />
                            {s.signal} dBm
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </>
      )}

      <AlertDialog open={!!confirmTarget} onOpenChange={(open) => !open && setConfirmTarget(null)}>
        <AlertDialogContent className="max-w-lg">
          <AlertDialogHeader>
            <AlertDialogTitle>{t('channelPlan.applyConfirmTitle', { band: confirmTarget?.name ?? '' })}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('channelPlan.applyConfirmBody', {
                band: confirmTarget?.name ?? '',
                from: confirmTarget?.channel ?? 0,
                to: confirmTarget?.recommended ?? 0,
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
            <AlertDialogAction onClick={onConfirmApply}>{t('channelPlan.applyConfirmAction')}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
