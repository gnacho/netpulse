import { useState, useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router'
import { Loader2, Check, Lock, Download, Copy, ShieldCheck, TriangleAlert, ExternalLink } from 'lucide-react'
import { Switch } from '@/components/ui/switch'

// FORK: Settings > HTTPS (server: internal/httpapi/https_settings.go).
//
// Turning HTTPS on only adds the HTTPS port; plain HTTP keeps working until a
// stricter mode is chosen - and that only takes effect once confirmed from a
// page loaded over HTTPS, so a change here cannot lock the admin out. Every
// change asks for the admin's password again.

type Mode = 'full' | 'migrate' | 'redirect'

interface HttpsStatus {
  available: boolean
  unavailable?: string
  enabled: boolean
  enabledLocked: boolean
  mode: Mode
  modeLocked: boolean
  port: number
  rootSha256?: string
  fingerprint?: string
  names?: string[]
  validUntil?: string
  pending?: Mode
  pendingUntil?: string
  error?: string
  agentsOnHttp?: { slug: string; lastSeen: string }[] | null
}

type Change = { enabled?: boolean; mode?: Mode; force?: boolean }

const MODES: Mode[] = ['full', 'migrate', 'redirect']

export default function HttpsCard({ onSaved }: { onSaved: () => void }) {
  const { t } = useTranslation()
  const [params, setParams] = useSearchParams()
  const [st, setSt] = useState<HttpsStatus | null>(null)
  const [loadError, setLoadError] = useState('')
  const [change, setChange] = useState<Change | null>(null)
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [blocked, setBlocked] = useState<{ kind: 'http' | 'https'; agents: { slug: string }[] } | null>(null)
  const [confirmUrl, setConfirmUrl] = useState('')
  const [copied, setCopied] = useState(false)
  const confirming = useRef(false)

  const load = useCallback(async () => {
    try {
      const r = await fetch('/api/settings/https')
      if (!r.ok) {
        setLoadError(r.status === 404 ? '' : t('settings.https.loadError'))
        return
      }
      const d = (await r.json()) as HttpsStatus
      setSt({ ...d, agentsOnHttp: d.agentsOnHttp ?? [], names: d.names ?? [] })
    } catch {
      setLoadError(t('settings.https.loadError'))
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  // Arriving from the confirmation link: confirm once, then drop the code
  // from the address so a reload does not try again.
  useEffect(() => {
    const code = params.get('confirm')
    if (!code || confirming.current) return
    confirming.current = true
    void (async () => {
      try {
        const r = await fetch('/api/settings/https/confirm', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ code }),
        })
        const next = new URLSearchParams(params)
        next.delete('confirm')
        setParams(next, { replace: true })
        if (r.ok) {
          onSaved()
        } else {
          setError(t('settings.https.confirmFailed'))
        }
        await load()
      } catch {
        // A network error: keep the code in the address and allow a retry.
        confirming.current = false
        setError(t('settings.https.confirmFailed'))
      }
    })()
  }, [params, setParams, load, onSaved, t])

  const apply = useCallback(async () => {
    if (!change) return
    setBusy(true)
    setError('')
    setBlocked(null)
    try {
      const r = await fetch('/api/settings/https', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...change, password }),
      })
      const d = await r.json().catch(() => ({}))
      if (r.status === 409 && (d.error === 'agents_on_http' || d.error === 'agents_on_https')) {
        setBlocked({ kind: d.error === 'agents_on_http' ? 'http' : 'https', agents: d.agents ?? [] })
        return
      }
      if (!r.ok) {
        setError(
          r.status === 401
            ? t('settings.https.badPassword')
            : r.status === 429
              ? t('settings.https.rateLimited', { seconds: d.retryAfterSec ?? 60 })
              : r.status === 409 && d.error === 'not_full'
                ? t('settings.https.notFull')
                : d.message || t('settings.https.applyFailed'),
        )
        return
      }
      setConfirmUrl(d.confirmUrl ?? '')
      setChange(null)
      setPassword('')
      onSaved()
      await load()
    } catch {
      setError(t('settings.https.applyFailed'))
    } finally {
      setBusy(false)
    }
  }, [change, password, load, onSaved, t])

  if (loadError) return <p className="text-caption text-danger">{loadError}</p>
  if (!st) return <p className="text-caption text-text-muted">{t('common.loading')}</p>

  if (!st.available) {
    return (
      <p className="flex items-start gap-2 text-xs text-text-secondary">
        <Lock className="mt-0.5 h-3.5 w-3.5 shrink-0" strokeWidth={2} />
        {t('settings.https.unavailable', { reason: st.unavailable })}
      </p>
    )
  }

  const inputCls =
    'w-full rounded-md border border-border bg-elevated px-2.5 py-1.5 text-xs text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none'
  const onHttps = window.location.protocol === 'https:'

  return (
    <div className="space-y-4">
      {/* On / off */}
      <div className="flex items-center justify-between gap-4">
        <div>
          <div className="text-sm font-medium text-text-primary">{t('settings.https.enabled', { port: st.port })}</div>
          <p className="text-[11px] text-text-muted">{t('settings.https.enabledHint')}</p>
        </div>
        <Switch
          checked={change?.enabled ?? st.enabled}
          disabled={st.enabledLocked || busy}
          onCheckedChange={(v) => setChange(v === st.enabled ? null : { enabled: v })}
          aria-label={t('settings.https.enabled', { port: st.port })}
        />
      </div>
      {st.enabledLocked && <p className="text-[11px] text-text-muted">{t('settings.https.locked')}</p>}
      {st.error && <p className="text-xs text-danger">{st.error}</p>}

      {st.enabled && (
        <>
          {/* The root, for devices to install */}
          <div className="rounded-lg border border-border bg-elevated p-3">
            <div className="mb-1 flex items-center gap-2 text-sm font-medium text-text-primary">
              <ShieldCheck className="h-4 w-4 text-accent" strokeWidth={2} />
              {t('settings.https.rootTitle')}
            </div>
            <p className="mb-2 text-[11px] leading-relaxed text-text-secondary">{t('settings.https.rootHint')}</p>
            <div className="mb-2 break-all font-mono text-[11px] text-text-primary">{st.rootSha256}</div>
            <div className="flex flex-wrap gap-2">
              <a
                href="/netpulse-ca.crt"
                className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border bg-surface px-2.5 text-xs font-medium text-text-primary hover:bg-hover"
              >
                <Download className="h-3.5 w-3.5" /> netpulse-ca.crt
              </a>
              <a
                href="/netpulse-ca.pem"
                className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border bg-surface px-2.5 text-xs font-medium text-text-primary hover:bg-hover"
              >
                <Download className="h-3.5 w-3.5" /> netpulse-ca.pem
              </a>
            </div>
            {st.names && st.names.length > 0 && (
              <p className="mt-2 text-[11px] text-text-muted">
                {t('settings.https.names', {
                  names: st.names.join(', '),
                  until: st.validUntil ? new Date(st.validUntil).toLocaleDateString() : '',
                })}
              </p>
            )}
          </div>

          {/* The pin for agents */}
          <div>
            <div className="text-[11px] font-medium uppercase tracking-wider text-text-muted">{t('settings.https.agentPin')}</div>
            <div className="mt-1 flex items-center gap-2">
              <code className="flex-1 break-all rounded-md border border-border bg-elevated px-2 py-1 font-mono text-[11px] text-text-primary">
                {st.fingerprint}
              </code>
              <button
                type="button"
                onClick={() => {
                  void navigator.clipboard?.writeText(st.fingerprint ?? '')
                  setCopied(true)
                  setTimeout(() => setCopied(false), 1500)
                }}
                className="rounded-md border border-border p-1.5 text-text-muted hover:text-text-primary"
                title={t('settings.https.copy')}
              >
                {copied ? <Check className="h-3.5 w-3.5 text-ok" /> : <Copy className="h-3.5 w-3.5" />}
              </button>
            </div>
            <p className="mt-1 text-[11px] text-text-muted">{t('settings.https.agentPinHint', { port: st.port })}</p>
          </div>

          {/* What plain HTTP may still do */}
          <div>
            <div className="text-sm font-medium text-text-primary">{t('settings.https.modeTitle')}</div>
            <div className="mt-2 space-y-2">
              {MODES.map((m) => (
                <label
                  key={m}
                  className={`flex cursor-pointer items-start gap-2 rounded-lg border p-2.5 ${
                    (change?.mode ?? st.mode) === m ? 'border-accent bg-accent/[0.04]' : 'border-border'
                  } ${st.modeLocked ? 'cursor-not-allowed opacity-60' : ''}`}
                >
                  <input
                    type="radio"
                    name="https-mode"
                    className="mt-0.5"
                    checked={(change?.mode ?? st.mode) === m}
                    disabled={st.modeLocked || busy}
                    onChange={() => setChange(m === st.mode ? null : { mode: m })}
                  />
                  <span>
                    <span className="block text-xs font-semibold text-text-primary">
                      {t(`settings.https.modes.${m}.title`)}
                      {st.mode === m && <span className="ml-1.5 font-normal text-ok">{t('settings.https.current')}</span>}
                    </span>
                    <span className="block text-[11px] leading-relaxed text-text-secondary">
                      {t(`settings.https.modes.${m}.hint`)}
                    </span>
                  </span>
                </label>
              ))}
            </div>
            {st.modeLocked && <p className="mt-1 text-[11px] text-text-muted">{t('settings.https.locked')}</p>}
          </div>

          {/* Agents that would stop reporting */}
          {(st.agentsOnHttp ?? []).length > 0 && (
            <div className="rounded-lg border border-warn/30 bg-warn/10 p-3 text-[11px] text-warn">
              <div className="mb-1 flex items-center gap-1.5 font-semibold">
                <TriangleAlert className="h-3.5 w-3.5" /> {t('settings.https.agentsOnHttp')}
              </div>
              <ul className="space-y-0.5">
                {(st.agentsOnHttp ?? []).map((a) => (
                  <li key={a.slug}>
                    {a.slug} · {new Date(a.lastSeen).toLocaleString()}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}

      {/* A staged change waiting for confirmation over HTTPS */}
      {(confirmUrl || st.pending) && (
        <div className="rounded-lg border border-accent/40 bg-accent/[0.05] p-3 text-xs text-text-primary">
          <p className="mb-2">
            {t('settings.https.pending', {
              mode: t(`settings.https.modes.${st.pending ?? 'migrate'}.title`),
              until: st.pendingUntil ? new Date(st.pendingUntil).toLocaleTimeString() : '',
            })}
          </p>
          {confirmUrl && (
            <a
              href={confirmUrl}
              className="inline-flex items-center gap-1.5 font-semibold text-accent hover:underline"
            >
              <ExternalLink className="h-3.5 w-3.5" /> {t('settings.https.confirmLink')}
            </a>
          )}
          {!onHttps && <p className="mt-2 text-[11px] text-text-muted">{t('settings.https.confirmHint')}</p>}
        </div>
      )}

      {/* Password, for any change */}
      {change && (
        <div className="rounded-lg border border-border p-3">
          <label className="mb-1 block text-[11px] font-medium uppercase tracking-wider text-text-muted">
            {t('settings.https.password')}
          </label>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            className={inputCls}
          />
          {blocked && (
            <div className="mt-2 text-[11px] text-warn">
              <p>
                {t(blocked.kind === 'http' ? 'settings.https.blocked' : 'settings.https.blockedOff', {
                  agents: blocked.agents.map((a) => a.slug).join(', '),
                })}
              </p>
              <button
                type="button"
                className="mt-1 font-semibold underline"
                onClick={() => setChange((c) => (c ? { ...c, force: true } : c))}
              >
                {t('settings.https.switchAnyway')}
              </button>
            </div>
          )}
          {error && <p className="mt-2 text-xs text-danger">{error}</p>}
          <div className="mt-2 flex gap-2">
            <button
              type="button"
              onClick={() => void apply()}
              disabled={busy || !password}
              className="inline-flex h-9 items-center gap-1.5 rounded-xl bg-accent px-3 text-[13px] font-semibold text-canvas hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" strokeWidth={2.5} />}
              {t('settings.https.apply')}
            </button>
            <button
              type="button"
              onClick={() => {
                setChange(null)
                setPassword('')
                setError('')
                setBlocked(null)
              }}
              className="inline-flex h-9 items-center rounded-xl border border-border px-3 text-[13px] text-text-primary hover:bg-hover"
            >
              {t('common.cancel')}
            </button>
          </div>
        </div>
      )}
      {!change && error && <p className="text-xs text-danger">{error}</p>}
    </div>
  )
}
