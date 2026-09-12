import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Send, Loader2, Check, Eye, EyeOff, Bell } from 'lucide-react'
import { Switch } from '@/components/ui/switch'

type NtfyState = 'loading' | 'idle' | 'saving' | 'testing' | 'saved' | 'error'

interface NtfyConfig {
  server: string
  topic: string
  token: string
  enabled: boolean
}

// NtfyCard (#766): canal ntfy (ntfy.sh o self-hosted). El topic actúa como
// secreto (recomendado largo y aleatorio); el token (topics reservados) es
// write-only y solo se envía si se escribe.
export default function NtfyCard({ onSaved, bare = true }: { onSaved: () => void; bare?: boolean }) {
  const { t } = useTranslation()
  const [state, setState] = useState<NtfyState>('loading')
  const [cfg, setCfg] = useState<NtfyConfig>({ server: 'https://ntfy.sh', topic: '', token: '', enabled: false })
  const [tokenSet, setTokenSet] = useState(false)
  const [showToken, setShowToken] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    void fetch('/api/settings/ntfy')
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (!alive || !d) return
        setCfg({ server: d.server || 'https://ntfy.sh', topic: d.topic || '', token: '', enabled: !!d.enabled })
        setTokenSet(!!d.tokenSet)
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
    if (!cfg.topic.trim()) {
      setError(t('settings.ntfy.incomplete'))
      setState('error')
      return
    }
    setState('saving')
    setError('')
    try {
      const body: Record<string, unknown> = {
        server: cfg.server.trim() || 'https://ntfy.sh',
        topic: cfg.topic.trim(),
        enabled: cfg.enabled,
      }
      if (cfg.token.trim()) body.token = cfg.token.trim()
      const res = await fetch('/api/settings/ntfy', {
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
      if (cfg.token.trim()) setTokenSet(true)
      setCfg((c) => ({ ...c, token: '' }))
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
      const res = await fetch('/api/settings/ntfy/test', { method: 'POST' })
      if (!res.ok) {
        const data = (await res.json().catch(() => null)) as { message?: string; error?: { message?: string } } | null
        setError(data?.message ?? data?.error?.message ?? 'Test failed')
        setState('error')
        return
      }
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

  const busy = state === 'saving' || state === 'testing'

  const header = (
    <>
      <div className="mb-3 flex items-center gap-2">
        <Bell className="h-4 w-4 text-accent" strokeWidth={2} />
        <h3 className="text-sm font-semibold text-text-primary">{t('settings.ntfy.title')}</h3>
      </div>
      <p className="mb-3 text-xs text-text-secondary">{t('settings.ntfy.description')}</p>
    </>
  )

  const form = (
    <>
      <div className="flex items-center justify-between gap-4 py-1">
        <span className="text-sm font-medium text-text-primary">{t('settings.ntfy.enabled')}</span>
        <Switch
          checked={cfg.enabled}
          onCheckedChange={(v) => setCfg((c) => ({ ...c, enabled: v }))}
          aria-label={t('settings.ntfy.enabled')}
        />
      </div>

      <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <label className="mb-1 block text-[11px] font-medium uppercase tracking-wider text-text-muted">
            {t('settings.ntfy.server')}
          </label>
          <input
            type="text"
            value={cfg.server}
            onChange={(e) => setCfg((c) => ({ ...c, server: e.target.value }))}
            placeholder="https://ntfy.sh"
            className="w-full rounded-md border border-border bg-elevated px-2.5 py-1.5 text-xs text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
        </div>

        <div>
          <label className="mb-1 block text-[11px] font-medium uppercase tracking-wider text-text-muted">
            {t('settings.ntfy.topic')}
          </label>
          <input
            type="text"
            value={cfg.topic}
            onChange={(e) => setCfg((c) => ({ ...c, topic: e.target.value }))}
            placeholder="netpulse_Xy3kPq9Lm2"
            className="w-full rounded-md border border-border bg-elevated px-2.5 py-1.5 text-xs text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
        </div>

        <div className="sm:col-span-2">
          <label className="mb-1 block text-[11px] font-medium uppercase tracking-wider text-text-muted">
            {t('settings.ntfy.token')}
            {tokenSet ? <span className="ml-1.5 normal-case text-ok">{t('settings.ntfy.tokenSet')}</span> : null}
          </label>
          <div className="flex items-center gap-1">
            <input
              type={showToken ? 'text' : 'password'}
              value={cfg.token}
              onChange={(e) => setCfg((c) => ({ ...c, token: e.target.value }))}
              placeholder={tokenSet ? t('settings.ntfy.tokenKeep') : t('settings.ntfy.tokenOptional')}
              autoComplete="new-password"
              className="flex-1 rounded-md border border-border bg-elevated px-2.5 py-1.5 text-xs text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
            />
            <button
              type="button"
              onClick={() => setShowToken((v) => !v)}
              className="rounded-md border border-border p-1.5 text-text-muted hover:text-text-primary"
              title={showToken ? 'Hide' : 'Show'}
            >
              {showToken ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
            </button>
          </div>
        </div>
      </div>

      <p className="mt-2 text-[11px] leading-relaxed text-text-muted">{t('settings.ntfy.hint')}</p>

      {error && <p className="mt-2 text-xs text-danger">{error}</p>}

      <div className="mt-3 flex items-center gap-2">
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
          disabled={busy}
          className="inline-flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-xl border border-border bg-elevated px-3 text-[13px] font-medium text-text-primary transition-colors hover:bg-hover disabled:cursor-not-allowed disabled:opacity-50"
        >
          {state === 'testing' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" strokeWidth={1.75} />}
          {t('settings.ntfy.sendTest')}
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
