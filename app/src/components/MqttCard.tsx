import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Send, Loader2, Check, Eye, EyeOff, Radio, Share2 } from 'lucide-react'
import { Switch } from '@/components/ui/switch'

type MqttState = 'loading' | 'idle' | 'saving' | 'testing' | 'propagating' | 'saved' | 'error'

interface MqttConfig {
  enabled: boolean
  host: string
  port: number
  user: string
  pass: string
  instance: string
  intervalSec: number
}

interface PropagateResult {
  id: string
  name: string
  delegated: boolean
  error?: string
}

// MqttCard (#838): ajustes del publisher MQTT (estado de la flota en Home
// Assistant) y propagación de la configuración a los routers NetGrip.
export default function MqttCard({ onSaved, bare = true }: { onSaved: () => void; bare?: boolean }) {
  const { t } = useTranslation()
  const [state, setState] = useState<MqttState>('loading')
  const [cfg, setCfg] = useState<MqttConfig>({
    enabled: false, host: '', port: 1883, user: '', pass: '', instance: 'default', intervalSec: 30,
  })
  const [passSet, setPassSet] = useState(false)
  const [showPass, setShowPass] = useState(false)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState('')
  const [results, setResults] = useState<PropagateResult[] | null>(null)

  useEffect(() => {
    let alive = true
    void fetch('/api/settings/mqtt')
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (!alive || !d) return
        setCfg({
          enabled: !!d.enabled,
          host: d.host || '',
          port: d.port || 1883,
          user: d.user || '',
          pass: '',
          instance: d.instance || 'default',
          intervalSec: d.intervalSec || 30,
        })
        setPassSet(!!d.passSet)
        setRunning(!!d.running)
        setState('idle')
      })
      .catch(() => {
        if (alive) setState('idle')
      })
    return () => {
      alive = false
    }
  }, [])

  const save = useCallback(async () => {
    if (cfg.enabled && !cfg.host.trim()) {
      setError(t('settings.mqtt.incomplete'))
      setState('error')
      return
    }
    setState('saving')
    setError('')
    try {
      const body: Record<string, unknown> = {
        enabled: cfg.enabled,
        host: cfg.host.trim(),
        port: cfg.port,
        user: cfg.user.trim(),
        instance: cfg.instance.trim() || 'default',
        intervalSec: cfg.intervalSec,
      }
      if (cfg.pass.trim()) body.pass = cfg.pass.trim()
      const res = await fetch('/api/settings/mqtt', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      const data = (await res.json().catch(() => null)) as { message?: string; error?: { message?: string } } | null
      if (!res.ok) {
        setError(data?.message ?? data?.error?.message ?? 'Save failed')
        setState('error')
        return
      }
      if (cfg.pass.trim()) setPassSet(true)
      setCfg((c) => ({ ...c, pass: '' }))
      setRunning(cfg.enabled)
      setState('saved')
      onSaved()
      setTimeout(() => setState('idle'), 1500)
    } catch {
      setError('Network error')
      setState('error')
    }
  }, [cfg, onSaved, t])

  const test = useCallback(async () => {
    setState('testing')
    setError('')
    try {
      const res = await fetch('/api/settings/mqtt/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ host: cfg.host.trim(), port: cfg.port, user: cfg.user.trim(), pass: cfg.pass.trim() }),
      })
      const data = (await res.json().catch(() => null)) as { ok?: boolean; error?: string } | null
      if (!res.ok || !data?.ok) {
        setError(data?.error ?? 'Test failed')
        setState('error')
        return
      }
      setState('saved')
      setTimeout(() => setState('idle'), 1500)
    } catch {
      setError('Network error')
      setState('error')
    }
  }, [cfg])

  const propagate = useCallback(async () => {
    setState('propagating')
    setError('')
    setResults(null)
    try {
      const res = await fetch('/api/settings/mqtt/propagate', { method: 'POST' })
      const data = (await res.json().catch(() => null)) as { results?: PropagateResult[]; message?: string; error?: { message?: string } } | null
      if (!res.ok) {
        setError(data?.message ?? data?.error?.message ?? 'Propagate failed')
        setState('error')
        return
      }
      setResults(data?.results ?? [])
      setState('saved')
      setTimeout(() => setState('idle'), 1500)
    } catch {
      setError('Network error')
      setState('error')
    }
  }, [])

  if (state === 'loading') {
    return <p className="text-caption text-text-muted">{t('common.loading')}</p>
  }

  const busy = state === 'saving' || state === 'testing' || state === 'propagating'

  const header = (
    <>
      <div className="mb-3 flex items-center gap-2">
        <Radio className="h-4 w-4 text-accent" strokeWidth={2} />
        <h3 className="text-sm font-semibold text-text-primary">{t('settings.mqtt.title')}</h3>
        <span className={`ml-auto text-[11px] ${running ? 'text-ok' : 'text-text-muted'}`}>
          {running ? t('settings.mqtt.running') : t('settings.mqtt.notRunning')}
        </span>
      </div>
      <p className="mb-3 text-xs text-text-secondary">{t('settings.mqtt.description')}</p>
    </>
  )

  const inputCls =
    'w-full rounded-md border border-border bg-elevated px-2.5 py-1.5 text-xs text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none'

  const form = (
    <>
      <div className="flex items-center justify-between gap-4 py-1">
        <span className="text-sm font-medium text-text-primary">{t('settings.mqtt.enabled')}</span>
        <Switch
          checked={cfg.enabled}
          onCheckedChange={(v) => setCfg((c) => ({ ...c, enabled: v }))}
          aria-label={t('settings.mqtt.enabled')}
        />
      </div>

      <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <label className="mb-1 block text-[11px] font-medium uppercase tracking-wider text-text-muted">
            {t('settings.mqtt.host')}
          </label>
          <input
            type="text"
            value={cfg.host}
            onChange={(e) => setCfg((c) => ({ ...c, host: e.target.value }))}
            placeholder="10.0.0.10"
            className={inputCls}
          />
        </div>

        <div>
          <label className="mb-1 block text-[11px] font-medium uppercase tracking-wider text-text-muted">
            {t('settings.mqtt.port')}
          </label>
          <input
            type="number"
            value={cfg.port}
            onChange={(e) => setCfg((c) => ({ ...c, port: Number(e.target.value) }))}
            className={inputCls}
          />
        </div>

        <div>
          <label className="mb-1 block text-[11px] font-medium uppercase tracking-wider text-text-muted">
            {t('settings.mqtt.user')}
          </label>
          <input
            type="text"
            value={cfg.user}
            onChange={(e) => setCfg((c) => ({ ...c, user: e.target.value }))}
            className={inputCls}
          />
        </div>

        <div>
          <label className="mb-1 block text-[11px] font-medium uppercase tracking-wider text-text-muted">
            {t('settings.mqtt.pass')}
            {passSet ? <span className="ml-1.5 normal-case text-ok">{t('settings.mqtt.passSet')}</span> : null}
          </label>
          <div className="flex items-center gap-1">
            <input
              type={showPass ? 'text' : 'password'}
              value={cfg.pass}
              onChange={(e) => setCfg((c) => ({ ...c, pass: e.target.value }))}
              placeholder={passSet ? t('settings.mqtt.passKeep') : t('settings.mqtt.passOptional')}
              autoComplete="new-password"
              className={`flex-1 ${inputCls}`}
            />
            <button
              type="button"
              onClick={() => setShowPass((v) => !v)}
              className="rounded-md border border-border p-1.5 text-text-muted hover:text-text-primary"
              title={showPass ? 'Hide' : 'Show'}
            >
              {showPass ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
            </button>
          </div>
        </div>

        <div>
          <label className="mb-1 block text-[11px] font-medium uppercase tracking-wider text-text-muted">
            {t('settings.mqtt.instance')}
          </label>
          <input
            type="text"
            value={cfg.instance}
            onChange={(e) => setCfg((c) => ({ ...c, instance: e.target.value }))}
            placeholder="default"
            className={inputCls}
          />
        </div>

        <div>
          <label className="mb-1 block text-[11px] font-medium uppercase tracking-wider text-text-muted">
            {t('settings.mqtt.interval')}
          </label>
          <input
            type="number"
            value={cfg.intervalSec}
            onChange={(e) => setCfg((c) => ({ ...c, intervalSec: Number(e.target.value) }))}
            className={inputCls}
          />
        </div>
      </div>

      <p className="mt-2 text-[11px] leading-relaxed text-text-muted">{t('settings.mqtt.hint')}</p>

      {error && <p className="mt-2 text-xs text-danger">{error}</p>}

      {results && (
        <div className="mt-2 rounded-md border border-border bg-elevated p-2 text-[11px] text-text-secondary">
          {results.length === 0 ? (
            <p>{t('settings.mqtt.propagateEmpty')}</p>
          ) : (
            <ul className="space-y-0.5">
              {results.map((r) => (
                <li key={r.id} className="flex items-center gap-1.5">
                  {r.delegated && !r.error ? (
                    <Check className="h-3 w-3 text-ok" strokeWidth={2.5} />
                  ) : (
                    <span className="text-danger">!</span>
                  )}
                  <span>{r.name || r.id}</span>
                  {r.error ? <span className="text-danger">({r.error})</span> : null}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      <div className="mt-3 flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={() => void save()}
          disabled={busy}
          className="inline-flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-xl bg-accent px-3 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {state === 'saving' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Check className="h-4 w-4" strokeWidth={2.5} />}
          {t('common.save')}
        </button>
        <button
          type="button"
          onClick={() => void test()}
          disabled={busy || !cfg.host.trim()}
          className="inline-flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-xl border border-border bg-elevated px-3 text-[13px] font-medium text-text-primary transition-colors hover:bg-hover disabled:cursor-not-allowed disabled:opacity-50"
        >
          {state === 'testing' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" strokeWidth={1.75} />}
          {t('settings.mqtt.test')}
        </button>
        <button
          type="button"
          onClick={() => void propagate()}
          disabled={busy || !cfg.enabled}
          className="inline-flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-xl border border-border bg-elevated px-3 text-[13px] font-medium text-text-primary transition-colors hover:bg-hover disabled:cursor-not-allowed disabled:opacity-50"
        >
          {state === 'propagating' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Share2 className="h-4 w-4" strokeWidth={1.75} />}
          {t('settings.mqtt.propagate')}
        </button>
      </div>
    </>
  )

  if (bare) {
    return form
  }

  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      {header}
      <div className="space-y-3">{form}</div>
    </div>
  )
}
