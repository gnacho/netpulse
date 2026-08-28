import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Send, Loader2, Check, Eye, EyeOff, Bell } from 'lucide-react'

type TelegramState = 'loading' | 'idle' | 'saving' | 'testing' | 'saved' | 'error'

interface TelegramConfig {
  botToken: string
  chatId: string
  enabled: boolean
  botName?: string
  chatName?: string
}

export default function TelegramCard({ onSaved }: { onSaved: () => void }) {
  const { t } = useTranslation()
  const [state, setState] = useState<TelegramState>('loading')
  const [cfg, setCfg] = useState<TelegramConfig>({ botToken: '', chatId: '', enabled: false })
  const [showToken, setShowToken] = useState(false)
  const [error, setError] = useState('')
  const [botName, setBotName] = useState('')
  const [chatName, setChatName] = useState('')

  useEffect(() => {
    let alive = true
    void fetch('/api/settings/telegram')
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (!alive || !d) return
        setCfg({
          botToken: d.botToken || '',
          chatId: d.chatId || '',
          enabled: !!d.enabled,
        })
        setBotName(d.botName || '')
        setChatName(d.chatName || '')
        setState('idle')
      })
      .catch(() => {
        if (alive) setState('idle')
      })
    return () => { alive = false }
  }, [])

  const save = useCallback(async () => {
    setState('saving')
    setError('')
    try {
      const res = await fetch('/api/settings/telegram', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          botToken: cfg.botToken,
          chatId: cfg.chatId,
          enabled: cfg.enabled,
        }),
      })
      const data = await res.json().catch(() => null)
      if (!res.ok) {
        setError(data?.message || data?.error || 'Save failed')
        setState('error')
        return
      }
      if (data?.botName) setBotName(data.botName)
      if (data?.chatName) setChatName(data.chatName)
      setState('saved')
      onSaved()
      setTimeout(() => setState('idle'), 1500)
    } catch {
      setError('Network error')
      setState('error')
    }
  }, [cfg, onSaved])

  const test = useCallback(async () => {
    setState('testing')
    setError('')
    try {
      const res = await fetch('/api/settings/telegram/test', { method: 'POST' })
      if (!res.ok) {
        const data = await res.json().catch(() => null)
        setError(data?.message || data?.error || 'Test failed')
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
    return (
      <div className="flex items-center gap-2 rounded-lg border border-border bg-surface p-4">
        <Loader2 className="h-4 w-4 animate-spin text-text-muted" />
        <span className="text-xs text-text-muted">{t('common.loading')}</span>
      </div>
    )
  }

  const busy = state === 'saving' || state === 'testing'

  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <div className="mb-3 flex items-center gap-2">
        <Bell className="h-4 w-4 text-accent" strokeWidth={2} />
        <h3 className="text-sm font-semibold text-text-primary">{t('settings.telegram.title')}</h3>
      </div>

      <p className="mb-3 text-xs text-text-secondary">{t('settings.telegram.description')}</p>

      <div className="space-y-3">
        {/* Enable toggle */}
        <label className="flex items-center gap-2 text-xs text-text-primary">
          <input
            type="checkbox"
            checked={cfg.enabled}
            onChange={(e) => setCfg((c) => ({ ...c, enabled: e.target.checked }))}
            className="h-4 w-4 rounded border-border accent-accent"
          />
          {t('settings.telegram.enabled')}
        </label>

        {/* Bot token */}
        <div>
          <label className="mb-1 block text-[11px] font-medium uppercase tracking-wider text-text-muted">
            {t('settings.telegram.botToken')}
          </label>
          <div className="flex items-center gap-1">
            <input
              type={showToken ? 'text' : 'password'}
              value={cfg.botToken}
              onChange={(e) => setCfg((c) => ({ ...c, botToken: e.target.value }))}
              placeholder="123456:ABC-DEF1234..."
              className="flex-1 rounded-md border border-border bg-canvas px-2.5 py-1.5 text-xs text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
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
          {botName && (
            <p className="mt-1 text-[10px] text-text-muted">
              {t('settings.telegram.bot')}: @{botName}
            </p>
          )}
        </div>

        {/* Chat ID */}
        <div>
          <label className="mb-1 block text-[11px] font-medium uppercase tracking-wider text-text-muted">
            {t('settings.telegram.chatId')}
          </label>
          <input
            type="text"
            value={cfg.chatId}
            onChange={(e) => setCfg((c) => ({ ...c, chatId: e.target.value }))}
            placeholder="-100123456789"
            className="w-full rounded-md border border-border bg-canvas px-2.5 py-1.5 text-xs text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
          {chatName && (
            <p className="mt-1 text-[10px] text-text-muted">
              {t('settings.telegram.chat')}: {chatName}
            </p>
          )}
        </div>

        {/* Error */}
        {error && (
          <p className="text-xs text-danger">{error}</p>
        )}

        {/* Actions */}
        <div className="flex items-center gap-2 pt-1">
          <button
            type="button"
            onClick={() => void save()}
            disabled={busy || !cfg.botToken || !cfg.chatId}
            className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
          >
            {state === 'saving' ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : state === 'saved' ? (
              <Check className="h-3.5 w-3.5" />
            ) : (
              <Check className="h-3.5 w-3.5" />
            )}
            {t('common.save')}
          </button>

          <button
            type="button"
            onClick={() => void test()}
            disabled={busy || !cfg.enabled}
            className="inline-flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-text-primary transition-colors hover:bg-hover disabled:cursor-not-allowed disabled:opacity-40"
          >
            {state === 'testing' ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Send className="h-3.5 w-3.5" />
            )}
            {t('settings.telegram.sendTest')}
          </button>
        </div>
      </div>
    </div>
  )
}
