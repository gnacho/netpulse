/**
 * Panel usteer (band-steering/roaming): APs de la malla con banda, frecuencia,
 * utilización y clientes. Datos de /api/usteer (ubus usteer local_info).
 */
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Wifi } from 'lucide-react'
import { SectionHeader } from '@/components/SectionHeader'
import { redirectLogin } from '@/data/DataProvider'
import { cn } from '@/lib/utils'

interface UsteerAp {
  ssid: string
  bssid: string
  hostname: string
  band: string
  freq: number
  utilizationPct: number
  clientCount: number
  iface: string
}

interface UsteerMesh {
  routerId: string
  name: string
  usteer: boolean
  apsSeen: number
}

export function UsteerPanel() {
  const { t } = useTranslation()
  const [aps, setAps] = useState<UsteerAp[] | null>(null)
  const [mesh, setMesh] = useState<UsteerMesh[] | null>(null)

  useEffect(() => {
    let disposed = false
    const load = async () => {
      try {
        const res = await fetch('/api/usteer')
        if (res.status === 401) redirectLogin()
        if (!res.ok) return
        const json = (await res.json()) as { aps: UsteerAp[]; mesh?: UsteerMesh[] }
        if (!disposed) {
          setAps(json.aps)
          setMesh(json.mesh ?? null)
        }
      } catch {
        /* sin usteer */
      }
    }
    void load()
    const id = window.setInterval(() => void load(), 30000)
    return () => {
      disposed = true
      window.clearInterval(id)
    }
  }, [])

  if (!aps || aps.length === 0) return null
  const active = mesh?.filter((m) => m.usteer).length ?? 0
  const total = mesh?.length ?? 0

  return (
    <section className="rounded-2xl border border-border bg-surface p-5 md:p-6">
      <SectionHeader title={t('topology.usteer.title')} />
      <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-text-muted">
        <span>{t('topology.usteer.caption')}</span>
        {mesh && (
          <span className="inline-flex items-center gap-1.5">
            <span className={cn('h-1.5 w-1.5 rounded-full', active === total ? 'bg-ok' : 'bg-warn')} />
            usteer {active}/{total}
          </span>
        )}
        {mesh?.map((m) => (
          <span key={m.routerId} className="inline-flex items-center gap-1 rounded-full bg-elevated px-2 py-0.5 font-mono text-[10px]">
            <span className={cn('h-1.5 w-1.5 rounded-full', m.usteer ? 'bg-ok' : 'bg-danger')} />
            {m.name} · {m.apsSeen} APs
          </span>
        ))}
      </div>
      <div className="mt-4 grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-4">
        {aps.map((ap) => (
          <div
            key={ap.bssid}
            className="flex items-center gap-3 rounded-xl border border-border bg-elevated px-3.5 py-2.5"
          >
            <span className={cn(
              'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg',
              ap.band === '5 GHz' ? 'bg-accent-soft text-accent' : 'bg-info/10 text-info',
            )}>
              <Wifi className="h-4 w-4" strokeWidth={1.75} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium text-text-primary">{ap.hostname}</div>
              <div className="font-mono text-caption text-text-muted">
                {ap.band} · {ap.freq} MHz · {t('topology.usteer.util', { pct: ap.utilizationPct })}
              </div>
            </div>
            <span className="shrink-0 font-mono text-caption text-text-secondary">
              {t('topology.usteer.clients', { count: ap.clientCount })}
            </span>
          </div>
        ))}
      </div>
    </section>
  )
}
