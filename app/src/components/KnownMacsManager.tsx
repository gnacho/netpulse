/**
 * NetPulse — Gestor de dispositivos de confianza (issue #196).
 * Allowlist de MACs que no avisan como «dispositivo desconocido» y cuyo
 * nombre se usa como alias. CRUD contra /api/settings/known-macs (admin).
 */
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, ShieldCheck, Trash2 } from 'lucide-react'
import { useNetPulse } from '@/data/DataProvider'
import { cn } from '@/lib/utils'

interface KnownMacItem {
  mac: string
  name: string
  note?: string
}

interface Props {
  onSaved?: () => void
}

const MAC_RE = /^([0-9A-F]{2}:){5}[0-9A-F]{2}$/i

export function KnownMacsManager({ onSaved }: Props) {
  const { t } = useTranslation()
  const { refresh } = useNetPulse()
  const [list, setList] = useState<KnownMacItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [mac, setMac] = useState('')
  const [name, setName] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/settings/known-macs')
      if (res.status === 401) {
        window.location.assign('/login')
        return
      }
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const json = (await res.json()) as { items: KnownMacItem[] }
      setList(json.items)
      setError(null)
    } catch {
      setError(t('settings.knownMacs.loadError'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    const m = mac.trim().toUpperCase()
    if (!MAC_RE.test(m) || submitting) return
    setSubmitting(true)
    setError(null)
    try {
      const res = await fetch('/api/settings/known-macs', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mac: m, name: name.trim() }),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setMac('')
      setName('')
      await load()
      refresh()
      onSaved?.()
    } catch {
      setError(t('settings.knownMacs.saveError'))
    } finally {
      setSubmitting(false)
    }
  }

  const remove = async (m: string) => {
    try {
      const res = await fetch(`/api/settings/known-macs/${encodeURIComponent(m)}`, { method: 'DELETE' })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      await load()
      refresh()
      onSaved?.()
    } catch {
      setError(t('settings.knownMacs.deleteError'))
    }
  }

  const macInvalid = mac.trim() !== '' && !MAC_RE.test(mac.trim())

  return (
    <div className="mt-4 space-y-3 border-t border-border pt-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-sm font-medium text-text-primary">{t('settings.knownMacs.title')}</div>
          <div className="text-caption text-text-muted">{t('settings.knownMacs.caption')}</div>
          <p className="mt-1 text-caption text-text-muted">{t('settings.knownMacs.hint')}</p>
        </div>
        <ShieldCheck className="h-5 w-5 shrink-0 text-ok" strokeWidth={1.75} aria-hidden="true" />
      </div>

      {loading ? (
        <div className="py-4 text-caption text-text-muted">…</div>
      ) : list.length === 0 ? (
        <div className="rounded-lg border border-border bg-elevated px-3 py-4 text-center text-caption text-text-muted">
          {t('settings.knownMacs.empty')}
        </div>
      ) : (
        <ul className="space-y-2">
          {list.map((item) => (
            <li
              key={item.mac}
              className="flex items-center justify-between gap-3 rounded-lg border border-border bg-elevated px-3 py-2"
            >
              <div className="min-w-0">
                <div className="truncate font-mono text-mono-sm text-text-primary">{item.mac}</div>
                {item.name && (
                  <div className="truncate text-caption text-text-secondary">{item.name}</div>
                )}
              </div>
              <button
                type="button"
                onClick={() => void remove(item.mac)}
                aria-label={`${t('settings.knownMacs.delete')} ${item.mac}`}
                className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-border text-text-secondary transition-colors hover:border-danger/40 hover:text-danger"
              >
                <Trash2 className="h-4 w-4" strokeWidth={1.75} aria-hidden="true" />
              </button>
            </li>
          ))}
        </ul>
      )}

      <form onSubmit={submit} className="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_1fr_auto]">
        <label className="block">
          <span className="text-label uppercase text-text-muted">{t('settings.knownMacs.mac')}</span>
          <input
            type="text"
            inputMode="text"
            value={mac}
            onChange={(e) => setMac(e.target.value)}
            placeholder={t('settings.knownMacs.macPlaceholder')}
            aria-label={t('settings.knownMacs.mac')}
            aria-invalid={macInvalid}
            className={cn(
              'mt-1 w-full rounded-lg border bg-canvas px-3 py-2 font-mono text-mono-sm text-text-primary placeholder:text-text-muted focus:outline-none',
              macInvalid ? 'border-danger' : 'border-border focus:border-accent',
            )}
          />
        </label>
        <label className="block">
          <span className="text-label uppercase text-text-muted">{t('settings.knownMacs.name')}</span>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('settings.knownMacs.namePlaceholder')}
            aria-label={t('settings.knownMacs.name')}
            className="mt-1 w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
        </label>
        <div className="flex items-end">
          <button
            type="submit"
            disabled={submitting || macInvalid || mac.trim() === ''}
            className="inline-flex h-[38px] shrink-0 items-center gap-1.5 rounded-lg border border-accent bg-accent-soft px-3 text-[13px] font-medium text-accent transition-colors disabled:cursor-not-allowed disabled:opacity-50"
          >
            <Plus className="h-4 w-4" strokeWidth={1.75} aria-hidden="true" />
            {submitting ? t('settings.knownMacs.adding') : t('settings.knownMacs.add')}
          </button>
        </div>
      </form>

      {macInvalid && <p className="text-xs text-danger">{t('settings.knownMacs.invalidMac')}</p>}
      {error && <p role="alert" className="text-xs text-danger">{error}</p>}
    </div>
  )
}
