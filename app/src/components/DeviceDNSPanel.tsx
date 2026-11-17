/**
 * DeviceDNSPanel — actividad DNS de un dispositivo desde el agregado del
 * query log de AdGuard (#2078).
 *
 * Se carga al abrir el detalle (lazy), con ventana de 1 h o 24 h. Si la
 * ingesta no puede atribuir consultas, el panel lo dice: deshabilitado,
 * IPs anonimizadas, integración ausente o un hueco por rotación del log.
 */
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ShieldBan } from 'lucide-react'
import { SegmentedControl } from '@/components/SegmentedControl'
import { fmtInt } from '@/data/mock'

interface DNSDomain {
  domain: string
  queries: number
  blocked: number
}

interface DNSResponse {
  status: string
  window: string
  gap_ts: number
  total_queries: number
  total_blocked: number
  top: DNSDomain[] | null
}

type WindowKey = '1h' | '24h'

export function DeviceDNSPanel({ mac }: { mac: string }) {
  const { t } = useTranslation()
  const [win, setWin] = useState<WindowKey>('24h')
  const [data, setData] = useState<DNSResponse | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    const ctl = new AbortController()
    setData(null)
    setFailed(false)
    fetch(`/api/devices/${encodeURIComponent(mac)}/dns?window=${win}`, {
      signal: ctl.signal,
      credentials: 'same-origin',
    })
      .then((res) => (res.ok ? res.json() : Promise.reject(new Error(String(res.status)))))
      .then((body: DNSResponse) => setData(body))
      .catch((err: Error) => {
        if (err.name !== 'AbortError') setFailed(true)
      })
    return () => ctl.abort()
  }, [mac, win])

  const top = data?.top ?? []
  const max = top.reduce((m, d) => Math.max(m, d.queries), 0)

  let notice: string | null = null
  if (failed) notice = t('devices.dns.error')
  else if (data) {
    if (data.status === 'unavailable') notice = t('devices.dns.unavailable')
    else if (data.status === 'disabled') notice = t('devices.dns.disabled')
    else if (data.status === 'anonymized') notice = t('devices.dns.anonymized')
    else if (data.status === 'error') notice = t('devices.dns.error')
    else if (top.length === 0) notice = t('devices.dns.empty')
  }

  return (
    <div className="border-t border-border/60 px-4 py-4 md:px-5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="text-label uppercase text-text-muted">{t('devices.dns.title')}</div>
        <SegmentedControl<WindowKey>
          size="sm"
          value={win}
          onChange={setWin}
          ariaLabel={t('devices.dns.title')}
          options={[
            { value: '1h', label: '1 h' },
            { value: '24h', label: '24 h' },
          ]}
        />
      </div>

      {!data && !failed && (
        <div className="mt-3 h-10 animate-pulse rounded-lg bg-elevated" aria-hidden />
      )}

      {notice && (
        <p className="mt-3 rounded-lg bg-elevated px-3 py-2 text-caption leading-relaxed text-text-muted">
          {notice}
        </p>
      )}

      {data && !notice && (
        <>
          <div className="mt-3 flex items-baseline gap-4 text-caption text-text-muted">
            <span>
              {t('devices.dns.queries', { n: fmtInt(data.total_queries) })}
            </span>
            {data.total_blocked > 0 && (
              <span className="inline-flex items-center gap-1 text-ok">
                <ShieldBan className="h-3.5 w-3.5" strokeWidth={1.75} />
                {t('devices.dns.blocked', { n: fmtInt(data.total_blocked) })}
              </span>
            )}
          </div>
          <div className="mt-3 space-y-2">
            {top.slice(0, 8).map((d) => (
              <div key={d.domain} className="flex items-center gap-2">
                <span
                  className="w-40 shrink-0 truncate font-mono text-mono-sm text-text-secondary"
                  title={d.domain}
                  translate="no"
                >
                  {d.domain}
                </span>
                <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-border/50">
                  <div
                    className={d.blocked > 0 ? 'h-full rounded-full bg-ok' : 'h-full rounded-full bg-accent'}
                    style={{ width: `${max > 0 ? (d.queries / max) * 100 : 0}%` }}
                  />
                </div>
                <span className="w-12 shrink-0 text-right font-mono text-mono-sm text-text-primary">
                  {fmtInt(d.queries)}
                </span>
              </div>
            ))}
          </div>
          {data.gap_ts > 0 && (
            <p className="mt-3 text-caption text-text-muted">{t('devices.dns.gap')}</p>
          )}
        </>
      )}
    </div>
  )
}
