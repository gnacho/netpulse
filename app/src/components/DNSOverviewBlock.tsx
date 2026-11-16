/**
 * DNSOverviewBlock — vista de red de la actividad DNS (#2078): top de
 * dominios y dominios vistos por primera vez en la ventana.
 *
 * Vive dentro de la tarjeta de AdGuard Home en el Resumen: es la misma
 * integración, sin abrir un menú nuevo para dos listas.
 */
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNetPulse } from '@/data/DataProvider'
import { fmtInt } from '@/data/mock'

interface DNSDomain {
  domain: string
  queries: number
  blocked: number
}

interface DNSNewDomain {
  domain: string
  mac: string
  first_seen_ms: number
}

interface DNSResponse {
  status: string
  gap_ts: number
  total_queries: number
  total_blocked: number
  top: DNSDomain[] | null
  new_domains: DNSNewDomain[] | null
}

export function DNSOverviewBlock() {
  const { t } = useTranslation()
  const { isDemo } = useNetPulse()
    const [data, setData] = useState<DNSResponse | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    const ctl = new AbortController()
    fetch('/api/dns/overview?window=24h', { signal: ctl.signal, credentials: 'same-origin' })
      .then((res) => (res.ok ? res.json() : Promise.reject(new Error(String(res.status)))))
      .then((body: DNSResponse) => setData(body))
      .catch((err: Error) => {
        if (err.name !== 'AbortError') setFailed(true)
      })
    return () => ctl.abort()
  }, [])

  // La demo enseña un AdGuard simulado: pedir el agregado real daría un
  // aviso de "sin integración" que contradice la tarjeta.
  // La demo enseña un AdGuard simulado: pedir el agregado real daría un
  // aviso de "sin integración" que contradice la tarjeta.
  if (isDemo || failed) return null
  if (!data) return <div className="mt-4 h-8 animate-pulse rounded-lg bg-elevated" aria-hidden />

  const top = data.top ?? []
  const fresh = data.new_domains ?? []
  const max = top.reduce((m, d) => Math.max(m, d.queries), 0)

  let notice: string | null = null
  if (data.status === 'unavailable') notice = t('dns.overview.unavailable')
  else if (data.status === 'disabled') notice = t('dns.overview.disabled')
  else if (data.status === 'anonymized') notice = t('dns.overview.anonymized')
  else if (data.status === 'error') notice = t('dns.overview.error')
  else if (top.length === 0) notice = t('dns.overview.empty')

  if (notice) {
    return (
      <p className="mt-4 rounded-xl bg-elevated px-3 py-2 text-caption leading-relaxed text-text-muted">
        {notice}
      </p>
    )
  }

  return (
    <div className="mt-4 border-t border-border/60 pt-3">
      <div className="flex items-baseline justify-between">
        <div className="text-label uppercase text-text-muted">{t('dns.overview.title')}</div>
        <div className="font-mono text-mono-sm text-text-secondary">
          {t('dns.overview.totals', {
            queries: fmtInt(data.total_queries),
            blocked: fmtInt(data.total_blocked),
          })}
        </div>
      </div>

      <div className="mt-2 space-y-1.5">
        {top.slice(0, 3).map((d) => (
          <div key={d.domain} className="flex items-center gap-2">
            <span
              className="w-36 shrink-0 truncate font-mono text-mono-sm text-text-secondary"
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
            <span className="w-10 shrink-0 text-right font-mono text-mono-sm text-text-primary">
              {fmtInt(d.queries)}
            </span>
          </div>
        ))}
      </div>

      {fresh.length > 0 && (
        <div className="mt-3">
          <div className="text-label uppercase text-text-muted">{t('dns.overview.newDomains')}</div>
          <ul className="mt-1 space-y-0.5">
            {fresh.slice(0, 3).map((d) => (
              <li key={d.domain} className="flex items-baseline justify-between gap-2">
                <span className="truncate font-mono text-mono-sm text-text-secondary" translate="no">
                  {d.domain}
                </span>
                <span className="shrink-0 font-mono text-caption text-text-muted">{d.mac || '—'}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {data.gap_ts > 0 && (
        <p className="mt-2 text-caption text-text-muted">{t('dns.overview.gap')}</p>
      )}
    </div>
  )
}
