import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Copy, KeyRound, Plus, Trash2 } from 'lucide-react'

type TokenItem = {
  id: string
  name: string
  scope: string
  userId: number
  createdAt: number
  expiresAt: number
  lastUsedAt: number
}

type CreatedToken = { id: string; name: string; scope: string; token: string }

export function TokensManager() {
  const { t } = useTranslation()
  const [tokens, setTokens] = useState<TokenItem[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [name, setName] = useState('')
  const [scope, setScope] = useState<'read' | 'write' | 'admin'>('read')
  const [expiresIn, setExpiresIn] = useState(0)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [created, setCreated] = useState<CreatedToken | null>(null)
  const [copied, setCopied] = useState(false)

  const load = useCallback(async () => {
    try {
      const r = await fetch('/api/tokens')
      if (r.ok) {
        const d = (await r.json()) as { tokens: TokenItem[] }
        setTokens(d.tokens ?? [])
      }
    } catch {
      /* ignore */
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const create = useCallback(async () => {
    if (busy || !name.trim()) return
    setBusy(true)
    setError(null)
    setCreated(null)
    try {
      const r = await fetch('/api/tokens', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: name.trim(), scope, expiresInDays: expiresIn }),
      })
      if (!r.ok) {
        const d = (await r.json().catch(() => null)) as { message?: string } | null
        throw new Error(d?.message ?? t('tokens.createError'))
      }
      const d = (await r.json()) as CreatedToken
      setCreated(d)
      setName('')
      setShowForm(false)
      void load()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('tokens.createError'))
    } finally {
      setBusy(false)
    }
  }, [busy, name, scope, expiresIn, t, load])

  const revoke = useCallback(
    async (id: string) => {
      try {
        await fetch(`/api/tokens/${id}`, { method: 'DELETE' })
        setTokens((prev) => prev.filter((x) => x.id !== id))
      } catch {
        /* ignore */
      }
    },
    [],
  )

  const copyToken = useCallback(async (raw: string) => {
    try {
      await navigator.clipboard.writeText(raw)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      /* ignore */
    }
  }, [])

  const fmtDate = (ms: number) => {
    if (!ms) return t('tokens.never')
    return new Date(ms).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
  }

  return (
    <div>
      <div className="flex items-center justify-between">
        <p className="text-sm text-text-secondary">{t('tokens.desc')}</p>
        {!showForm && !created && (
          <button
            type="button"
            onClick={() => setShowForm(true)}
            className="flex h-8 items-center gap-1.5 rounded-lg bg-accent px-3 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
          >
            <Plus className="h-3.5 w-3.5" strokeWidth={2} />
            {t('tokens.create')}
          </button>
        )}
      </div>

      {showForm && !created && (
        <div className="mt-4 flex flex-col gap-3 rounded-xl border border-border bg-surface p-4">
          <div className="grid gap-3 sm:grid-cols-3">
            <div className="sm:col-span-2">
              <label className="text-xs font-medium text-text-muted">{t('tokens.nameLabel')}</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t('tokens.namePlaceholder')}
                maxLength={60}
                autoFocus
                className="mt-1 h-9 w-full rounded-lg border border-border bg-elevated px-3 text-sm text-text-primary"
              />
            </div>
            <div>
              <label className="text-xs font-medium text-text-muted">{t('tokens.scopeLabel')}</label>
              <select
                value={scope}
                onChange={(e) => setScope(e.target.value as 'read' | 'write' | 'admin')}
                className="mt-1 h-9 w-full rounded-lg border border-border bg-elevated px-2.5 text-sm text-text-primary"
              >
                <option value="read">{t('tokens.scopeRead')}</option>
                <option value="write">{t('tokens.scopeWrite')}</option>
                <option value="admin">{t('tokens.scopeAdmin')}</option>
              </select>
            </div>
          </div>
          <div>
            <label className="text-xs font-medium text-text-muted">{t('tokens.expiresLabel')}</label>
            <select
              value={expiresIn}
              onChange={(e) => setExpiresIn(Number(e.target.value))}
              className="mt-1 h-9 w-full rounded-lg border border-border bg-elevated px-2.5 text-sm text-text-primary sm:w-48"
            >
              <option value={0}>{t('tokens.neverExpires')}</option>
              <option value={30}>{t('tokens.expires30d')}</option>
              <option value={90}>{t('tokens.expires90d')}</option>
              <option value={365}>{t('tokens.expires1y')}</option>
            </select>
          </div>
          {error && <p role="alert" className="text-caption text-danger">{error}</p>}
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => void create()}
              disabled={busy || !name.trim()}
              className="flex h-9 items-center justify-center rounded-lg bg-accent px-4 text-sm font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
            >
              {busy ? t('common.loading') : t('tokens.createBtn')}
            </button>
            <button
              type="button"
              onClick={() => {
                setShowForm(false)
                setError(null)
              }}
              className="flex h-9 items-center justify-center rounded-lg border border-border px-4 text-sm text-text-muted transition-colors hover:bg-hover"
            >
              {t('common.cancel')}
            </button>
          </div>
        </div>
      )}

      {created && (
        <div className="mt-4 rounded-xl border border-ok/30 bg-ok/5 p-4">
          <p className="text-sm font-medium text-ok">{t('tokens.createdMsg')}</p>
          <p className="mt-1 text-xs text-text-muted">{t('tokens.createdWarn')}</p>
          <div className="mt-3 flex items-center gap-2">
            <code className="flex-1 break-all rounded-lg border border-border bg-elevated px-3 py-2 font-mono text-xs text-text-primary">
              {created.token}
            </code>
            <button
              type="button"
              onClick={() => void copyToken(created.token)}
              className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-border text-text-muted transition-colors hover:bg-hover hover:text-text-primary"
              title={t('tokens.copy')}
            >
              <Copy className="h-4 w-4" strokeWidth={1.75} />
            </button>
          </div>
          {copied && <p className="mt-1 text-xs text-ok">{t('tokens.copied')}</p>}
          <button
            type="button"
            onClick={() => setCreated(null)}
            className="mt-3 text-xs font-medium text-text-muted underline hover:text-text-primary"
          >
            {t('tokens.dismiss')}
          </button>
        </div>
      )}

      {loading && <p className="mt-4 text-sm text-text-muted">{t('common.loading')}</p>}

      {!loading && tokens.length === 0 && !created && (
        <p className="mt-4 text-sm text-text-muted">{t('tokens.empty')}</p>
      )}

      {!loading && tokens.length > 0 && (
        <div className="mt-4 space-y-2">
          {tokens.map((tok) => (
            <div
              key={tok.id}
              className="flex items-center justify-between rounded-xl border border-border bg-surface px-4 py-3"
            >
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <KeyRound className="h-4 w-4 shrink-0 text-text-muted" strokeWidth={1.75} />
                  <span className="truncate text-sm font-medium text-text-primary">{tok.name}</span>
                  <span className="shrink-0 rounded-full bg-accent/10 px-2 py-0.5 text-[10px] font-semibold uppercase text-accent">
                    {tok.scope}
                  </span>
                </div>
                <div className="mt-1 flex flex-wrap gap-x-3 text-[11px] text-text-muted">
                  <span>{t('tokens.created')}: {fmtDate(tok.createdAt)}</span>
                  {tok.expiresAt > 0 && <span>{t('tokens.expires')}: {fmtDate(tok.expiresAt)}</span>}
                  <span>{t('tokens.lastUsed')}: {fmtDate(tok.lastUsedAt)}</span>
                </div>
              </div>
              <button
                type="button"
                onClick={() => void revoke(tok.id)}
                className="ml-3 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-text-muted transition-colors hover:bg-danger/10 hover:text-danger"
                title={t('tokens.revoke')}
              >
                <Trash2 className="h-4 w-4" strokeWidth={1.75} />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
