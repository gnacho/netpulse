import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNetPulse } from '@/data/DataProvider'
import { Signal, Radio as RadioIcon, AlertCircle } from 'lucide-react'
import { cn } from '@/lib/utils'

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

  const sortedRouters = useMemo(() => {
    return [...routers].sort((a, b) => (a.roleBadge === 'Principal' ? -1 : 1) || a.name.localeCompare(b.name))
  }, [routers])

  useEffect(() => {
    if (!routerId && sortedRouters.length > 0) {
      setRouterId(sortedRouters[0]!.id)
    }
  }, [sortedRouters, routerId])

  useEffect(() => {
    if (!routerId) return
    setLoading(true)
    setError('')
    fetch(`/api/wifi/channel-plan?routerId=${encodeURIComponent(routerId)}`)
      .then(async (res) => {
        if (!res.ok) throw new Error(await res.text())
        return res.json()
      })
      .then((d) => setData(d))
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false))
  }, [routerId])

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
            onChange={(e) => setRouterId(e.target.value)}
            className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
          >
            {sortedRouters.map((r) => (
              <option key={r.id} value={r.id}>
                {r.name}{r.roleBadge === 'Principal' ? ' (Gateway)' : ''}
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
              {data.radios.map((r, idx) => (
                <div
                  key={idx}
                  className={cn(
                    'rounded-2xl border border-border bg-surface p-5',
                    r.recommended !== r.channel && r.recommended > 0 && 'border-accent/40'
                  )}
                >
                  <div className="mb-3 flex items-center gap-2">
                    <RadioIcon className="h-4 w-4 text-accent" strokeWidth={1.75} />
                    <h3 className="text-sm font-semibold text-text-primary">{r.name}</h3>
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
                </div>
              ))}
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
                      <th className="py-2 pr-4">Signal</th>
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
    </div>
  )
}
