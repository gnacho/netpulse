import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import {
  AlertTriangle,
  Check,
  Copy,
  Loader2,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  RotateCw,
  Search,
  Square,
  Trash2,
} from 'lucide-react'
import { cn, copyToClipboard } from '@/lib/utils'
import { useAuth } from '@/data/AuthContext'
import { useNetPulse } from '@/data/DataProvider'
import { SectionHeader } from '@/components/SectionHeader'
import { EmptyState } from '@/components/EmptyState'
import { StatusPill, type PillTone } from '@/components/StatusPill'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  ApiError,
  checkIntegration,
  createIntegration,
  deleteIntegration,
  editIntegration,
  fetchIntegrations,
  fetchVendors,
  refreshVendors,
  startIntegration,
  stopIntegration,
  updateIntegration,
} from '@/lib/integrationsApi'
import type { CreateIntegrationInput, Integration, Vendor } from '@/lib/integrationsApi'

const POLL_MS = 15_000
const SLUG_RE = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/

/** Maps a thrown error to a human message: known backend code -> i18n key,
 *  falling back to the server message (or the raw code, or a generic label). */
function errMessage(err: unknown, t: TFunction): string {
  if (err instanceof ApiError) {
    return t(`integrations.errors.${err.code ?? ''}`, { defaultValue: err.message })
  }
  if (err instanceof Error) return err.message
  return t('integrations.errors.generic')
}

/** Lowercases a host and turns non-alphanumeric runs into dashes (auto slug). */
function slugifyHost(host: string): string {
  return host
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

function stateTone(state: string): PillTone {
  if (state === 'running') return 'ok'
  if (state === 'error') return 'danger'
  return 'muted'
}

export default function Integrations() {
  const { t, i18n } = useTranslation()
  const auth = useAuth()
  const { isDemo } = useNetPulse()
  const isAdmin = auth?.role === 'admin'

  const [vendors, setVendors] = useState<Vendor[]>([])
  const [lastRefreshedMs, setLastRefreshedMs] = useState(0)
  const [catalogRefreshing, setCatalogRefreshing] = useState(false)
  const [integrations, setIntegrations] = useState<Integration[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState('')

  // Formulario de alta.
  const [showForm, setShowForm] = useState(false)
  const [slug, setSlug] = useState('')
  const [vendorId, setVendorId] = useState('')
  const [host, setHost] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [intervalS, setIntervalS] = useState(30)
  const [arch, setArch] = useState('')
  const [submitting, setSubmitting] = useState(false)

  // Token modal + confirmación de borrado + acciones en vuelo por fila.
  const [token, setToken] = useState<string | null>(null)
  const [tokenCopied, setTokenCopied] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState<Integration | null>(null)
  // Edición en linea: se rellena con los valores actuales salvo la contraseña,
  // que en blanco significa "conservar la que ya tiene".
  const [editing, setEditing] = useState<Integration | null>(null)
  const [editHost, setEditHost] = useState('')
  const [editUsername, setEditUsername] = useState('')
  const [editPassword, setEditPassword] = useState('')
  const [editInterval, setEditInterval] = useState(30)
  const [savingEdit, setSavingEdit] = useState(false)
  const [busy, setBusy] = useState<Record<string, string>>({})

  const load = useCallback(
    async (silent = false) => {
      if (silent) setRefreshing(true)
      else setLoading(true)
      try {
        const [v, ints] = await Promise.all([fetchVendors(), fetchIntegrations()])
        setVendors(v.vendors)
        setLastRefreshedMs(v.lastRefreshedAt ?? 0)
        setIntegrations(ints.integrations)
        setError('')
      } catch (err) {
        setError(errMessage(err, t))
      } finally {
        if (silent) setRefreshing(false)
        else setLoading(false)
      }
    },
    [t],
  )

  useEffect(() => {
    if (isDemo) {
      setLoading(false)
      return
    }
    void load()
  }, [load, isDemo])

  // Polling cada 15 s mientras la página está montada (limpia en unmount).
  useEffect(() => {
    if (isDemo) return
    const iv = window.setInterval(() => {
      void load(true)
    }, POLL_MS)
    return () => window.clearInterval(iv)
  }, [load, isDemo])

  const selectedVendor = vendors.find((v) => v.id === vendorId)

  const onHostChange = (value: string) => {
    setHost(value)
    if (slug.trim() === '') setSlug(slugifyHost(value))
  }

  const onVendorChange = (value: string) => {
    setVendorId(value)
    const v = vendors.find((x) => x.id === value)
    setArch(v && v.arches.length > 0 ? (v.arches[0] ?? '') : '')
  }

  const slugInvalid = slug.trim() !== '' && !SLUG_RE.test(slug.trim())
  const interval = Number(intervalS)
  const intervalInvalid = !Number.isFinite(interval) || interval < 10 || interval > 86_400

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (submitting) return
    if (!vendorId || !host.trim() || !SLUG_RE.test(slug.trim()) || intervalInvalid || password === '') return
    setSubmitting(true)
    setError('')
    try {
      const input: CreateIntegrationInput = {
        slug: slug.trim(),
        vendor: vendorId,
        host: host.trim(),
        username: username.trim(),
        password,
        intervalS: interval,
      }
      if (selectedVendor && selectedVendor.arches.length > 1 && arch) {
        input.arch = arch
      } else if (selectedVendor && selectedVendor.arches.length === 1) {
        input.arch = selectedVendor.arches[0]
      }
      const res = await createIntegration(input)
      setToken(res.token)
      setShowForm(false)
      setSlug('')
      setHost('')
      setUsername('')
      setPassword('')
      setIntervalS(30)
      setArch('')
      setVendorId('')
      await load(true)
    } catch (err) {
      setError(errMessage(err, t))
    } finally {
      setSubmitting(false)
    }
  }

  const runAction = async (slug: string, key: string, fn: () => Promise<unknown>) => {
    setBusy((prev) => ({ ...prev, [slug]: key }))
    setError('')
    try {
      await fn()
      await load(true)
    } catch (err) {
      setError(errMessage(err, t))
    } finally {
      setBusy((prev) => {
        const next = { ...prev }
        delete next[slug]
        return next
      })
    }
  }

  const doDelete = async (it: Integration) => {
    await runAction(it.slug, 'delete', () => deleteIntegration(it.slug))
    setConfirmDelete(null)
  }

  const startEdit = (it: Integration) => {
    setEditing(it)
    setEditHost(it.host)
    setEditUsername(it.username)
    setEditPassword('')
    setEditInterval(it.intervalS)
    setError('')
  }

  const editInvalid =
    !editHost.trim() || !editUsername.trim() || !Number.isFinite(editInterval) || editInterval < 10 || editInterval > 86_400

  const submitEdit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editing || editInvalid) return
    setSavingEdit(true)
    setError('')
    try {
      const body: { host: string; username: string; intervalS: number; password?: string } = {
        host: editHost.trim(),
        username: editUsername.trim(),
        intervalS: editInterval,
      }
      if (editPassword) body.password = editPassword
      await editIntegration(editing.slug, body)
      setEditing(null)
      await load(true)
    } catch (err) {
      setError(errMessage(err, t))
    } finally {
      setSavingEdit(false)
    }
  }

  /** Forces the vendor catalog discovery (#1697) and swaps in the response. */
  const refreshCatalog = async () => {
    if (catalogRefreshing) return
    setCatalogRefreshing(true)
    setError('')
    try {
      const v = await refreshVendors()
      setVendors(v.vendors)
      setLastRefreshedMs(v.lastRefreshedAt ?? 0)
    } catch (err) {
      setError(errMessage(err, t))
    } finally {
      setCatalogRefreshing(false)
    }
  }

  const copyToken = async () => {
    if (!token) return
    const ok = await copyToClipboard(token)
    setTokenCopied(ok)
    window.setTimeout(() => setTokenCopied(false), 1500)
  }

  /** "hace X min" (unidades i18n) o fecha corta para eventos muy antiguos. */
  const agoLabel = (ms: number): string => {
    if (!ms || !Number.isFinite(ms) || ms <= 0) return t('integrations.never')
    const age = Date.now() - ms
    if (age < 0) return t('integrations.never')
    const sec = Math.floor(age / 1000)
    if (sec < 60) return t('integrations.ago.s', { count: sec })
    const min = Math.floor(sec / 60)
    if (min < 60) return t('integrations.ago.min', { count: min })
    const h = Math.floor(min / 60)
    if (h < 24) return t('integrations.ago.h', { count: h })
    const d = Math.floor(h / 24)
    if (d < 7) return t('integrations.ago.d', { count: d })
    return new Date(ms).toLocaleDateString(i18n.language, { day: 'numeric', month: 'short' })
  }

  return (
    <div className="space-y-4 md:space-y-5">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="font-display text-h1 text-text-primary">{t('integrations.title')}</h1>
          <p className="mt-0.5 text-sm text-text-secondary">{t('integrations.subtitle')}</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => void load(true)}
            disabled={refreshing}
            className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-surface px-3 text-sm font-medium text-text-primary transition-colors hover:bg-elevated disabled:opacity-50"
          >
            <RefreshCw className={cn('h-4 w-4', refreshing && 'animate-spin')} strokeWidth={1.75} />
            {t('common.refresh')}
          </button>
          <button
            type="button"
            aria-expanded={showForm}
            onClick={() => setShowForm((v) => !v)}
            className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-accent bg-accent-soft px-3 text-sm font-medium text-accent transition-colors hover:brightness-105"
          >
            <Plus className="h-4 w-4" strokeWidth={1.75} />
            {t('integrations.form.add')}
          </button>
        </div>
      </header>

      {!isAdmin && (
        <div className="flex items-start gap-3 rounded-xl border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-700 dark:text-amber-300">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
          <span>{t('common.adminOnly')}</span>
        </div>
      )}

      {error && (
        <div className="flex items-start gap-3 rounded-xl border border-rose-500/40 bg-rose-500/10 px-4 py-3 text-sm text-rose-600 dark:text-rose-400">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
          <span>{error}</span>
        </div>
      )}

      {showForm && (
        <form onSubmit={(e) => void submit(e)} className="rounded-2xl border border-border bg-surface p-5">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <label className="block">
              <span className="text-label uppercase text-text-muted">{t('integrations.form.fieldSlug')}</span>
              <input
                type="text"
                value={slug}
                onChange={(e) => setSlug(e.target.value)}
                placeholder={t('integrations.form.fieldSlugHint')}
                aria-label={t('integrations.form.fieldSlug')}
                aria-invalid={slugInvalid}
                spellCheck={false}
                className={cn(
                  'mt-1 w-full rounded-lg border bg-canvas px-3 py-2 font-mono text-mono-sm text-text-primary placeholder:text-text-muted focus:outline-none',
                  slugInvalid ? 'border-danger' : 'border-border focus:border-accent',
                )}
              />
              {slugInvalid && <span className="mt-1 block text-xs text-danger">{t('integrations.form.fieldSlugInvalid')}</span>}
            </label>
            <label className="block">
              <span className="flex items-center justify-between gap-2">
                <span className="text-label uppercase text-text-muted">{t('integrations.form.fieldVendor')}</span>
                {!isDemo && (
                  <button
                    type="button"
                    onClick={() => void refreshCatalog()}
                    disabled={catalogRefreshing}
                    aria-label={t('integrations.catalog.refresh')}
                    title={t('integrations.catalog.refresh')}
                    className="inline-flex h-6 items-center gap-1 rounded-md border border-border px-1.5 text-caption font-medium text-text-muted transition-colors duration-150 hover:border-accent/40 hover:text-accent disabled:opacity-50"
                  >
                    <RefreshCw className={cn('h-3 w-3', catalogRefreshing && 'animate-spin')} strokeWidth={1.75} />
                    {catalogRefreshing ? t('integrations.catalog.refreshing') : t('integrations.catalog.refresh')}
                  </button>
                )}
              </span>
              <select
                value={vendorId}
                onChange={(e) => onVendorChange(e.target.value)}
                aria-label={t('integrations.form.fieldVendor')}
                className="mt-1 h-[38px] w-full rounded-lg border border-border bg-canvas px-3 text-sm text-text-primary focus:border-accent focus:outline-none"
              >
                <option value="">{t('integrations.form.selectVendor')}</option>
                {vendors.map((v) => (
                  <option key={v.id} value={v.id}>
                    {v.id}
                  </option>
                ))}
              </select>
              {!isDemo && lastRefreshedMs > 0 && (
                <span className="mt-1 block text-caption text-text-muted">
                  {t('integrations.catalog.updated', { time: agoLabel(lastRefreshedMs) })}
                </span>
              )}
            </label>
            <label className="block">
              <span className="text-label uppercase text-text-muted">{t('integrations.form.fieldHost')}</span>
              <input
                type="text"
                value={host}
                onChange={(e) => onHostChange(e.target.value)}
                placeholder={t('integrations.form.fieldHostPlaceholder')}
                aria-label={t('integrations.form.fieldHost')}
                className="mt-1 w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
              />
            </label>
            <label className="block">
              <span className="text-label uppercase text-text-muted">{t('integrations.form.fieldUsername')}</span>
              <input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                aria-label={t('integrations.form.fieldUsername')}
                autoComplete="off"
                className="mt-1 w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none"
              />
            </label>
            <label className="block">
              <span className="text-label uppercase text-text-muted">{t('integrations.form.fieldPassword')}</span>
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                aria-label={t('integrations.form.fieldPassword')}
                autoComplete="new-password"
                className="mt-1 w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none"
              />
              {selectedVendor && selectedVendor.mode === 'ssh' && (
                <span className="mt-1 block text-xs text-text-muted">{t('integrations.form.fieldPasswordSshHint')}</span>
              )}
            </label>
            <label className="block">
              <span className="text-label uppercase text-text-muted">{t('integrations.form.fieldInterval')}</span>
              <input
                type="number"
                min={10}
                max={86_400}
                value={intervalS}
                onChange={(e) => setIntervalS(Number(e.target.value))}
                aria-label={t('integrations.form.fieldInterval')}
                aria-invalid={intervalInvalid}
                className={cn(
                  'mt-1 w-full rounded-lg border bg-canvas px-3 py-2 text-sm text-text-primary focus:outline-none',
                  intervalInvalid ? 'border-danger' : 'border-border focus:border-accent',
                )}
              />
              {intervalInvalid && <span className="mt-1 block text-xs text-danger">{t('integrations.form.fieldIntervalInvalid')}</span>}
            </label>
            {selectedVendor && selectedVendor.arches.length > 1 && (
              <label className="block">
                <span className="text-label uppercase text-text-muted">{t('integrations.form.fieldArch')}</span>
                <select
                  value={arch}
                  onChange={(e) => setArch(e.target.value)}
                  aria-label={t('integrations.form.fieldArch')}
                  className="mt-1 h-[38px] w-full rounded-lg border border-border bg-canvas px-3 text-sm text-text-primary focus:border-accent focus:outline-none"
                >
                  {selectedVendor.arches.map((a) => (
                    <option key={a} value={a}>
                      {a}
                    </option>
                  ))}
                </select>
              </label>
            )}
          </div>

          {selectedVendor && (
            <p className="mt-3 text-caption text-text-muted">
              {selectedVendor.mode === 'ssh'
                ? t('integrations.form.modeSshHint')
                : t('integrations.form.modeLocalHint')}
            </p>
          )}

          <div className="mt-3 flex items-center gap-2">
            <button
              type="submit"
              disabled={submitting || !vendorId || !host.trim() || slugInvalid || intervalInvalid}
              className="inline-flex h-9 items-center gap-1.5 rounded-lg bg-accent px-3 text-sm font-medium text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
            >
              {submitting && <Loader2 className="h-4 w-4 animate-spin" strokeWidth={1.75} />}
              {submitting ? t('integrations.form.creating') : t('integrations.form.submit')}
            </button>
            <button
              type="button"
              onClick={() => setShowForm(false)}
              className="inline-flex h-9 items-center rounded-lg border border-border px-3 text-sm font-medium text-text-secondary transition-colors hover:text-text-primary"
            >
              {t('common.cancel')}
            </button>
          </div>
        </form>
      )}

      <SectionHeader title={t('integrations.listTitle')} className="mt-6" />

      {loading ? (
        <p className="text-caption text-text-muted">{t('integrations.loading')}</p>
      ) : integrations.length === 0 ? (
        <div className="rounded-2xl border border-border bg-surface">
          <EmptyState
            image="/empty-devices.svg"
            title={t('integrations.emptyTitle')}
            description={t('integrations.emptyDesc')}
          />
        </div>
      ) : (
        <div className="space-y-3">
          {integrations.map((it) => {
            const running = it.state === 'running'
            const installing = it.state === 'installing'
            const isBusy = busy[it.slug] !== undefined
            return (
              <div key={it.slug} className="rounded-2xl border border-border bg-surface p-4">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-mono text-sm font-medium text-text-primary">{it.slug}</span>
                      <StatusPill
                        tone={stateTone(it.state)}
                        label={t(`integrations.states.${it.state}`, { defaultValue: it.state })}
                      />
                      {it.fresh && (
                        <span className="rounded-full bg-ok/10 px-2 py-0.5 text-caption font-semibold uppercase tracking-[0.06em] text-ok">
                          {t('integrations.fresh')}
                        </span>
                      )}
                    </div>
                    <div className="mt-1 font-mono text-caption text-text-muted">{it.host}</div>
                    {it.lastError && (
                      <div className="mt-1 text-xs text-danger">{it.lastError}</div>
                    )}
                  </div>
                  <div className="flex shrink-0 items-center gap-1.5">
                    <button
                      type="button"
                      onClick={() => void runAction(it.slug, 'start', () => startIntegration(it.slug))}
                      disabled={running || installing || isBusy}
                      aria-label={t('integrations.actions.start')}
                      title={t('integrations.actions.start')}
                      className="flex h-7 w-7 items-center justify-center rounded-lg border border-border text-text-muted transition-colors duration-150 hover:border-accent/40 hover:text-accent disabled:opacity-50"
                    >
                      {busy[it.slug] === 'start' ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" strokeWidth={1.75} />
                      ) : (
                        <Play className="h-3.5 w-3.5" strokeWidth={1.75} />
                      )}
                    </button>
                    <button
                      type="button"
                      onClick={() => void runAction(it.slug, 'stop', () => stopIntegration(it.slug))}
                      disabled={!running || installing || isBusy}
                      aria-label={t('integrations.actions.stop')}
                      title={t('integrations.actions.stop')}
                      className="flex h-7 w-7 items-center justify-center rounded-lg border border-border text-text-muted transition-colors duration-150 hover:border-accent/40 hover:text-accent disabled:opacity-50"
                    >
                      {busy[it.slug] === 'stop' ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" strokeWidth={1.75} />
                      ) : (
                        <Square className="h-3.5 w-3.5" strokeWidth={1.75} />
                      )}
                    </button>
                    <button
                      type="button"
                      onClick={() => void runAction(it.slug, 'update', () => updateIntegration(it.slug))}
                      disabled={installing || isBusy}
                      aria-label={t('integrations.actions.update')}
                      title={t('integrations.actions.update')}
                      className="flex h-7 w-7 items-center justify-center rounded-lg border border-border text-text-muted transition-colors duration-150 hover:border-accent/40 hover:text-accent disabled:opacity-50"
                    >
                      {busy[it.slug] === 'update' ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" strokeWidth={1.75} />
                      ) : (
                        <RotateCw className="h-3.5 w-3.5" strokeWidth={1.75} />
                      )}
                    </button>
                    <button
                      type="button"
                      onClick={() => void runAction(it.slug, 'check', () => checkIntegration(it.slug))}
                      disabled={isBusy}
                      aria-label={t('integrations.actions.check')}
                      title={t('integrations.actions.check')}
                      className="flex h-7 w-7 items-center justify-center rounded-lg border border-border text-text-muted transition-colors duration-150 hover:border-accent/40 hover:text-accent disabled:opacity-50"
                    >
                      {busy[it.slug] === 'check' ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" strokeWidth={1.75} />
                      ) : (
                        <Search className="h-3.5 w-3.5" strokeWidth={1.75} />
                      )}
                    </button>
                    <button
                      type="button"
                      onClick={() => startEdit(it)}
                      disabled={isBusy}
                      aria-label={t('integrations.actions.edit')}
                      title={t('integrations.actions.edit')}
                      className="flex h-7 w-7 items-center justify-center rounded-lg border border-border text-text-muted transition-colors duration-150 hover:border-accent/40 hover:text-accent disabled:opacity-50"
                    >
                      <Pencil className="h-3.5 w-3.5" strokeWidth={1.75} />
                    </button>
                    <button
                      type="button"
                      onClick={() => setConfirmDelete(it)}
                      disabled={isBusy}
                      aria-label={t('integrations.actions.delete')}
                      title={t('integrations.actions.delete')}
                      className="flex h-7 w-7 items-center justify-center rounded-lg border border-danger/30 text-danger transition-colors duration-150 hover:border-danger/60 hover:bg-danger/10 disabled:opacity-50"
                    >
                      <Trash2 className="h-3.5 w-3.5" strokeWidth={1.75} />
                    </button>
                  </div>
                </div>
                <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-caption text-text-muted">
                  <span>
                    {t('integrations.form.fieldVendor')}: {it.vendor}
                  </span>
                  <span>
                    {t('integrations.meta.version')}: {it.version || '-'}
                  </span>
                  <span>
                    {t('integrations.meta.interval')}: {it.intervalS} s
                  </span>
                  <span>
                    {t('integrations.meta.lastPush')}: {agoLabel(it.lastPushMs)}
                  </span>
                </div>
                {editing?.slug === it.slug && (
                  <form onSubmit={(e) => void submitEdit(e)} className="mt-3 border-t border-border pt-3">
                    <div className="text-label uppercase text-text-muted">{t('integrations.edit.title')}</div>
                    <div className="mt-2 grid gap-3 sm:grid-cols-2">
                      <label className="block">
                        <span className="text-label uppercase text-text-muted">{t('integrations.form.fieldHost')}</span>
                        <input
                          type="text"
                          value={editHost}
                          onChange={(e) => setEditHost(e.target.value)}
                          aria-label={t('integrations.form.fieldHost')}
                          spellCheck={false}
                          className="mt-1 w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none"
                        />
                      </label>
                      <label className="block">
                        <span className="text-label uppercase text-text-muted">{t('integrations.form.fieldUsername')}</span>
                        <input
                          type="text"
                          value={editUsername}
                          onChange={(e) => setEditUsername(e.target.value)}
                          aria-label={t('integrations.form.fieldUsername')}
                          autoComplete="off"
                          className="mt-1 w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none"
                        />
                      </label>
                      <label className="block">
                        <span className="text-label uppercase text-text-muted">{t('integrations.edit.fieldPassword')}</span>
                        <input
                          type="password"
                          value={editPassword}
                          onChange={(e) => setEditPassword(e.target.value)}
                          aria-label={t('integrations.edit.fieldPassword')}
                          autoComplete="new-password"
                          placeholder={t('integrations.edit.fieldPasswordKeep')}
                          className="mt-1 w-full rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                        />
                        {editing && editing.mode === 'ssh' && (
                          <span className="mt-1 block text-xs text-text-muted">{t('integrations.edit.fieldPasswordSshHint')}</span>
                        )}
                      </label>
                      <label className="block">
                        <span className="text-label uppercase text-text-muted">{t('integrations.form.fieldInterval')}</span>
                        <input
                          type="number"
                          min={10}
                          max={86_400}
                          value={editInterval}
                          onChange={(e) => setEditInterval(Number(e.target.value))}
                          aria-label={t('integrations.form.fieldInterval')}
                          className={`mt-1 w-full rounded-lg border bg-canvas px-3 py-2 text-sm text-text-primary focus:outline-none ${
                            editInvalid ? 'border-danger' : 'border-border focus:border-accent'
                          }`}
                        />
                      </label>
                    </div>
                    <p className="mt-2 text-caption text-text-muted">{t('integrations.edit.restartHint')}</p>
                    <div className="mt-3 flex items-center gap-2">
                      <button
                        type="submit"
                        disabled={savingEdit || editInvalid}
                        className="inline-flex h-9 items-center gap-1.5 rounded-lg bg-accent px-3 text-sm font-medium text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
                      >
                        {savingEdit && <Loader2 className="h-4 w-4 animate-spin" strokeWidth={1.75} />}
                        {savingEdit ? t('integrations.edit.saving') : t('integrations.edit.save')}
                      </button>
                      <button
                        type="button"
                        onClick={() => setEditing(null)}
                        className="inline-flex h-9 items-center rounded-lg border border-border px-3 text-sm font-medium text-text-secondary transition-colors hover:text-text-primary"
                      >
                        {t('common.cancel')}
                      </button>
                    </div>
                  </form>
                )}
              </div>
            )
          })}
        </div>
      )}

      <Dialog open={token !== null} onOpenChange={(open) => !open && setToken(null)}>
        {token !== null && (
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle>{t('integrations.token.tokenTitle')}</DialogTitle>
              <DialogDescription>{t('integrations.token.tokenDesc')}</DialogDescription>
            </DialogHeader>
            <div className="flex items-center gap-2 rounded-lg border border-border bg-canvas px-3 py-2">
              <code className="min-w-0 flex-1 break-all font-mono text-mono-sm text-text-primary">{token}</code>
              <button
                type="button"
                onClick={() => void copyToken()}
                aria-label={t('integrations.token.copyToken')}
                className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-border text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
              >
                {tokenCopied ? (
                  <Check className="h-4 w-4 text-ok" strokeWidth={1.75} />
                ) : (
                  <Copy className="h-4 w-4" strokeWidth={1.75} />
                )}
              </button>
            </div>
            <p className="text-caption text-text-muted">
              {tokenCopied ? t('integrations.token.copied') : t('integrations.token.copyHint')}
            </p>
          </DialogContent>
        )}
      </Dialog>

      <AlertDialog open={confirmDelete !== null} onOpenChange={(open) => !open && setConfirmDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('integrations.confirm.confirmDeleteTitle')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('integrations.confirm.confirmDeleteDesc', { slug: confirmDelete?.slug ?? '' })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (confirmDelete) void doDelete(confirmDelete)
              }}
              className="bg-danger text-canvas hover:bg-danger/90"
            >
              <Trash2 className="mr-1.5 h-4 w-4" strokeWidth={2} />
              {t('integrations.actions.delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
