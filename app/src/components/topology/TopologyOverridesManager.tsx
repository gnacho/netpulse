/**
 * NetPulse — Gestor de overrides manuales de topología (issue #142, Fase B).
 * Capa 2 sobre el autodiscover: el admin etiqueta hardware como hipervisor o
 * switch y asigna dispositivos a hosts (kind 'attach'). CRUD contra
 * /api/topology/overrides; los cambios se reflejan en el mapa al refrescar el
 * overview (el builder aplica los overrides server-side).
 */
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Pencil, Plus, Tag, Trash2 } from 'lucide-react'
import { SegmentedControl } from '@/components/SegmentedControl'
import { Switch } from '@/components/ui/switch'
import { useNetPulse } from '@/data/DataProvider'
import { cn } from '@/lib/utils'
import type { TopologyOverride, TopologyOverrideKind } from '@/data/types'

interface Props {
  onSaved?: () => void
  /** MAC pre-seleccionada (desde el clic en el mapa) */
  initialMac?: string
}

interface OverrideDraft {
  mac: string
  kind: TopologyOverrideKind
  name: string
  parent: string
}

const EMPTY: OverrideDraft = { mac: '', kind: 'hypervisor', name: '', parent: '' }

const KIND_STYLE: Record<TopologyOverrideKind, string> = {
  hypervisor: 'bg-ok/10 text-ok',
  switch: 'bg-accent-soft text-accent',
  attach: 'bg-info/10 text-info',
}

export function TopologyOverridesManager({ onSaved, initialMac }: Props) {
  const { t } = useTranslation()
  const { refresh } = useNetPulse()
  const [list, setList] = useState<TopologyOverride[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [draft, setDraft] = useState<OverrideDraft>(EMPTY)
  const [submitting, setSubmitting] = useState(false)
  const [showAddForm, setShowAddForm] = useState(false)
  const [confirmDeleteFor, setConfirmDeleteFor] = useState<string | null>(null)
  const [editing, setEditing] = useState<TopologyOverride | null>(null)
  const [editDraft, setEditDraft] = useState<OverrideDraft>(EMPTY)
  const [editSubmitting, setEditSubmitting] = useState(false)

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/topology/overrides')
      if (res.status === 401) {
        window.location.assign('/login')
        return
      }
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const json = (await res.json()) as { overrides: TopologyOverride[] }
      setList(json.overrides)
      setError(null)
    } catch {
      setError(t('settings.overrides.errorGeneric'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  // Pre-selección desde el mapa: rellena el draft de alta y lo abre.
  useEffect(() => {
    if (!initialMac) return
    setDraft((d) => ({ ...d, mac: initialMac }))
    setShowAddForm(true)
    setEditing(null)
  }, [initialMac])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!draft.mac.trim() || submitting) return
    setSubmitting(true)
    setError(null)
    try {
      const res = await fetch('/api/topology/overrides', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          mac: draft.mac.trim(),
          kind: draft.kind,
          name: draft.name.trim() || undefined,
          parent: draft.kind === 'attach' ? draft.parent.trim() || undefined : undefined,
        }),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setDraft(EMPTY)
      setShowAddForm(false)
      await load()
      refresh()
      onSaved?.()
    } catch {
      setError(t('settings.overrides.errorGeneric'))
    } finally {
      setSubmitting(false)
    }
  }

  const openEdit = (o: TopologyOverride) => {
    setEditing(o)
    setEditDraft({ mac: o.mac, kind: o.kind, name: o.name ?? '', parent: o.parent ?? '' })
    setError(null)
  }

  const saveEdit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editing || editSubmitting) return
    setEditSubmitting(true)
    setError(null)
    try {
      const res = await fetch(`/api/topology/overrides/${encodeURIComponent(editing.id)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          kind: editDraft.kind,
          name: editDraft.name.trim() || undefined,
          parent: editDraft.kind === 'attach' ? editDraft.parent.trim() || undefined : undefined,
        }),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setEditing(null)
      await load()
      refresh()
      onSaved?.()
    } catch {
      setError(t('settings.overrides.errorGeneric'))
    } finally {
      setEditSubmitting(false)
    }
  }

  const toggleEnabled = async (o: TopologyOverride) => {
    try {
      const res = await fetch(`/api/topology/overrides/${encodeURIComponent(o.id)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: !o.enabled }),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      await load()
      refresh()
      onSaved?.()
    } catch {
      setError(t('settings.overrides.errorGeneric'))
    }
  }

  const remove = async (o: TopologyOverride) => {
    try {
      const res = await fetch(`/api/topology/overrides/${encodeURIComponent(o.id)}`, { method: 'DELETE' })
      if (!res.ok && res.status !== 204) throw new Error(`HTTP ${res.status}`)
      setConfirmDeleteFor(null)
      await load()
      refresh()
      onSaved?.()
    } catch {
      setError(t('settings.overrides.errorGeneric'))
    }
  }

  const kindOptions = [
    { value: 'hypervisor' as const, label: t('settings.overrides.kindHypervisor') },
    { value: 'switch' as const, label: t('settings.overrides.kindSwitch') },
    { value: 'attach' as const, label: t('settings.overrides.kindAttach') },
  ]

  return (
    <div className="space-y-3">
      <p className="text-caption leading-relaxed text-text-muted">{t('settings.overrides.hint')}</p>

      {error && <p className="text-caption text-danger">{error}</p>}

      {/* Lista */}
      {loading ? (
        <p className="text-caption text-text-muted">…</p>
      ) : list.length === 0 ? (
        <p className="rounded-xl bg-elevated px-3.5 py-2.5 text-caption leading-relaxed text-text-muted">
          {t('settings.overrides.empty')}
        </p>
      ) : (
        <ul className="flex flex-col gap-2">
          {list.map((o) => (
            <li
              key={o.id}
              className={cn(
                'flex items-center gap-3 rounded-xl border border-border bg-elevated px-3.5 py-2.5',
                !o.enabled && 'opacity-50',
              )}
            >
              <Tag className="h-4 w-4 shrink-0 text-text-muted" strokeWidth={1.75} />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="truncate font-mono text-sm font-medium text-text-primary">{o.mac}</span>
                  <span
                    className={cn(
                      'rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider',
                      KIND_STYLE[o.kind],
                    )}
                  >
                    {t(`settings.overrides.kind.${o.kind}`)}
                  </span>
                  {o.kind === 'attach' && o.parent && (
                    <span className="truncate font-mono text-caption text-text-muted">→ {o.parent}</span>
                  )}
                </div>
                {o.name && <div className="truncate text-caption text-text-muted">{o.name}</div>}
              </div>
              <label className="flex shrink-0 cursor-pointer items-center" title={t('settings.overrides.enabled')}>
                <Switch checked={o.enabled} onCheckedChange={() => void toggleEnabled(o)} aria-label={t('settings.overrides.enabled')} />
              </label>
              {confirmDeleteFor === o.id ? (
                <span className="flex shrink-0 items-center gap-1.5">
                  <button
                    type="button"
                    onClick={() => void remove(o)}
                    className="rounded-lg bg-danger px-2.5 py-1.5 text-[11px] font-semibold text-canvas transition-opacity hover:opacity-90"
                  >
                    {t('settings.users.confirmDelete')}
                  </button>
                  <button
                    type="button"
                    onClick={() => setConfirmDeleteFor(null)}
                    className="rounded-lg border border-border px-2.5 py-1.5 text-[11px] font-medium text-text-secondary transition-colors hover:text-text-primary"
                  >
                    {t('settings.users.cancel')}
                  </button>
                </span>
              ) : (
                <span className="flex shrink-0 items-center gap-1.5">
                  <button
                    type="button"
                    onClick={() => openEdit(o)}
                    aria-label={t('settings.overrides.edit')}
                    className="flex h-8 w-8 items-center justify-center rounded-lg border border-border text-text-muted transition-colors duration-150 hover:border-accent/40 hover:text-accent"
                  >
                    <Pencil className="h-3.5 w-3.5" strokeWidth={1.75} />
                  </button>
                  <button
                    type="button"
                    onClick={() => setConfirmDeleteFor(o.id)}
                    aria-label={t('settings.overrides.delete')}
                    className="flex h-8 w-8 items-center justify-center rounded-lg border border-border text-text-muted transition-colors duration-150 hover:border-danger/40 hover:text-danger"
                  >
                    <Trash2 className="h-4 w-4" strokeWidth={1.75} />
                  </button>
                </span>
              )}
            </li>
          ))}
        </ul>
      )}

      {/* Alta */}
      <div className="border-t border-border pt-3">
        <button
          type="button"
          aria-expanded={showAddForm}
          onClick={() => setShowAddForm((v) => !v)}
          className="flex items-center gap-2 rounded-lg border border-border bg-elevated px-3.5 py-2 text-sm font-medium text-text-secondary transition-colors duration-150 hover:border-accent/40 hover:text-accent"
        >
          <Plus className="h-4 w-4" strokeWidth={1.75} />
          {t('settings.overrides.add')}
        </button>
        {showAddForm && (
          <form onSubmit={(e) => void submit(e)} className="mt-3 space-y-2.5">
            <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
              <input
                type="text"
                required
                value={draft.mac}
                onChange={(e) => setDraft((d) => ({ ...d, mac: e.target.value }))}
                placeholder="AA:BB:CC:DD:EE:FF"
                aria-label={t('settings.overrides.mac')}
                className="rounded-lg border border-border bg-canvas px-3 py-2 font-mono text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
              />
              <input
                type="text"
                value={draft.name}
                onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
                placeholder={t('settings.overrides.name')}
                aria-label={t('settings.overrides.name')}
                className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
              />
            </div>
            <SegmentedControl
              options={kindOptions}
              value={draft.kind}
              onChange={(v) => setDraft((d) => ({ ...d, kind: v }))}
              ariaLabel={t('settings.overrides.kind')}
            />
            {draft.kind === 'attach' && (
              <input
                type="text"
                required
                value={draft.parent}
                onChange={(e) => setDraft((d) => ({ ...d, parent: e.target.value }))}
                placeholder={t('settings.overrides.parent')}
                aria-label={t('settings.overrides.parent')}
                className="w-full rounded-lg border border-border bg-canvas px-3 py-2 font-mono text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
              />
            )}
            <div className="flex items-center justify-end gap-2">
              <button
                type="submit"
                disabled={submitting || !draft.mac.trim() || (draft.kind === 'attach' && !draft.parent.trim())}
                className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-canvas transition-opacity duration-150 hover:opacity-90 disabled:opacity-40"
              >
                <Plus className="h-4 w-4" strokeWidth={2} />
                {submitting ? t('settings.overrides.adding') : t('settings.overrides.add')}
              </button>
            </div>
          </form>
        )}
      </div>

      {/* Edición inline */}
      {editing && (
        <form onSubmit={(e) => void saveEdit(e)} className="mt-3 border-t border-border pt-3">
          <div className="mb-2 flex items-center gap-2">
            <Pencil className="h-4 w-4 text-accent" strokeWidth={1.75} />
            <span className="truncate font-mono text-sm font-medium text-text-primary">{editing.mac}</span>
          </div>
          <div className="space-y-2.5">
            <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
              <input
                type="text"
                value={editDraft.name}
                onChange={(e) => setEditDraft((d) => ({ ...d, name: e.target.value }))}
                placeholder={t('settings.overrides.name')}
                aria-label={t('settings.overrides.name')}
                className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
              />
            </div>
            <SegmentedControl
              options={kindOptions}
              value={editDraft.kind}
              onChange={(v) => setEditDraft((d) => ({ ...d, kind: v }))}
              ariaLabel={t('settings.overrides.kind')}
            />
            {editDraft.kind === 'attach' && (
              <input
                type="text"
                required
                value={editDraft.parent}
                onChange={(e) => setEditDraft((d) => ({ ...d, parent: e.target.value }))}
                placeholder={t('settings.overrides.parent')}
                aria-label={t('settings.overrides.parent')}
                className="w-full rounded-lg border border-border bg-canvas px-3 py-2 font-mono text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
              />
            )}
            <div className="flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={() => setEditing(null)}
                className="rounded-lg border border-border px-3 py-2 text-sm font-medium text-text-secondary transition-colors duration-150 hover:text-text-primary"
              >
                {t('settings.users.cancel')}
              </button>
              <button
                type="submit"
                disabled={editSubmitting || (editDraft.kind === 'attach' && !editDraft.parent.trim())}
                className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-canvas transition-opacity duration-150 hover:opacity-90 disabled:opacity-40"
              >
                <Pencil className="h-4 w-4" strokeWidth={2} />
                {editSubmitting ? t('settings.overrides.saving') : t('settings.overrides.save')}
              </button>
            </div>
          </div>
        </form>
      )}
    </div>
  )
}
