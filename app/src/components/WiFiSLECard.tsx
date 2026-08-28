import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Wifi, Clock, Server, Gauge } from 'lucide-react'
import { HealthRing } from '@/components/HealthRing'

type SLEItem = {
  routerId: string
  connectCount: number
  avgConnectMs: number
  dhcpSuccessPct: number
  avgDnsMs: number
  score: number
  label: string
}

function SLEMetric({
  icon: Icon,
  label,
  value,
  unit,
  tone,
}: {
  icon: React.ComponentType<{ className?: string; strokeWidth?: number }>
  label: string
  value: string
  unit: string
  tone: string
}) {
  return (
    <div className="flex items-center gap-2">
      <Icon className={`h-3.5 w-3.5 ${tone}`} strokeWidth={1.75} />
      <div>
        <span className="text-[10px] uppercase text-text-muted">{label}</span>
        <div className="font-mono text-sm font-semibold text-text-primary">
          {value} <span className="text-[10px] font-normal text-text-muted">{unit}</span>
        </div>
      </div>
    </div>
  )
}

function SLECard({ sle }: { sle: SLEItem }) {
  const { t } = useTranslation()
  const connectTone = sle.avgConnectMs > 1000 ? 'text-warn' : sle.avgConnectMs > 500 ? 'text-info' : 'text-ok'
  const dhcpTone = sle.dhcpSuccessPct < 95 ? 'text-danger' : sle.dhcpSuccessPct < 99 ? 'text-warn' : 'text-ok'
  const dnsTone = sle.avgDnsMs > 100 ? 'text-danger' : sle.avgDnsMs > 50 ? 'text-warn' : 'text-ok'

  return (
    <div className="flex items-center gap-4 rounded-xl border border-border bg-surface p-4">
      <HealthRing value={sle.score} size={56} stroke={5} animateIn={false}
        ariaLabel={`${sle.routerId} WiFi SLE: ${sle.score}/100`}
        center={<span className="font-mono text-xs font-bold text-text-primary">{sle.score}</span>}
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-semibold text-text-primary">{sle.routerId}</span>
          <span className="text-[10px] text-text-muted">{sle.label}</span>
        </div>
        <div className="mt-2 grid grid-cols-3 gap-3">
          <SLEMetric icon={Clock} label={t('wifiSle.connectTime')} value={sle.avgConnectMs.toFixed(0)} unit="ms" tone={connectTone} />
          <SLEMetric icon={Server} label={t('wifiSle.dhcpSuccess')} value={sle.dhcpSuccessPct.toFixed(1)} unit="%" tone={dhcpTone} />
          <SLEMetric icon={Gauge} label={t('wifiSle.dnsLatency')} value={sle.avgDnsMs.toFixed(1)} unit="ms" tone={dnsTone} />
        </div>
      </div>
    </div>
  )
}

export function WiFiSLECard() {
  const { t } = useTranslation()
  const [sles, setSles] = useState<SLEItem[]>([])
  const [available, setAvailable] = useState(false)
  const [loading, setLoading] = useState(true)

  const fetchSLEs = useCallback(async () => {
    try {
      const r = await fetch('/api/wifi-sles?hours=24')
      if (r.ok) {
        const d = (await r.json()) as { sles: SLEItem[]; available: boolean }
        setSles(d.sles ?? [])
        setAvailable(!!d.available)
      }
    } catch {
      /* ignore */
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchSLEs()
  }, [fetchSLEs])

  if (loading || !available || sles.length === 0) return null

  return (
    <div className="rounded-2xl border border-border bg-surface p-5">
      <div className="mb-4 flex items-center gap-2">
        <Wifi className="h-4 w-4 text-accent" strokeWidth={1.75} />
        <h3 className="text-sm font-semibold text-text-primary">{t('wifiSle.title')}</h3>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        {sles.map((sle) => (
          <SLECard key={sle.routerId} sle={sle} />
        ))}
      </div>
    </div>
  )
}
