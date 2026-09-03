import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNetPulse } from '@/data/DataProvider'
import { useAuth } from '@/data/AuthContext'
import { AlertCircle, Cpu, RefreshCw, Rocket, ShieldCheck, TriangleAlert } from 'lucide-react'
import { cn } from '@/lib/utils'
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

interface FirmwareUpgrade {
  id: number
  routerId: string
  targetVersion: string
  targetUrl: string
  checksum: string
  status: string
  error?: string
  backupPath?: string
  startedAt: number
  finishedAt?: number
}

interface FirmwareItem {
  routerId: string
  name: string
  model: string
  currentVersion: string
  targetVersion: string
  targetUrl: string
  checksum: string
  upgrade?: FirmwareUpgrade
}

const STATUS_COLORS: Record<string, string> = {
  requested: 'bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/30',
  downloading: 'bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/30',
  backing_up: 'bg-purple-500/10 text-purple-600 dark:text-purple-400 border-purple-500/30',
  verifying: 'bg-cyan-500/10 text-cyan-600 dark:text-cyan-400 border-cyan-500/30',
  flashing: 'bg-orange-500/10 text-orange-600 dark:text-orange-400 border-orange-500/30',
  done: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/30',
  failed: 'bg-rose-500/10 text-rose-600 dark:text-rose-400 border-rose-500/30',
}

export default function FirmwareUpgrades() {
  const { t } = useTranslation()
  const auth = useAuth()
  const { routers } = useNetPulse()
  const isAdmin = auth?.role === 'admin'

  const [items, setItems] = useState<FirmwareItem[]>([])
  const [edits, setEdits] = useState<Record<string, Partial<FirmwareItem>>>({})
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState<Record<string, string>>({})
  const [error, setError] = useState('')
  const [confirmId, setConfirmId] = useState<string | null>(null)

  const sortedRouters = useMemo(() => {
    return [...routers].sort((a, b) => (a.roleBadge === 'Principal' ? -1 : 1) || a.name.localeCompare(b.name))
  }, [routers])

  const fetchItems = async () => {
    setLoading(true)
    setError('')
    try {
      const res = await fetch('/api/firmware-upgrades')
      if (!res.ok) throw new Error(await res.text())
      const data = (await res.json()) as FirmwareItem[]
      setItems(data)
      const initialEdits: Record<string, Partial<FirmwareItem>> = {}
      data.forEach((it) => {
        initialEdits[it.routerId] = {
          model: it.model,
          currentVersion: it.currentVersion,
          targetVersion: it.targetVersion,
          targetUrl: it.targetUrl,
          checksum: it.checksum,
        }
      })
      setEdits(initialEdits)
    } catch (e) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchItems()
  }, [])

  const updateEdit = (id: string, patch: Partial<FirmwareItem>) => {
    setEdits((prev) => ({ ...prev, [id]: { ...prev[id], ...patch } }))
  }

  const saveTarget = async (id: string) => {
    const e = edits[id]
    if (!e) return
    setBusy((prev) => ({ ...prev, [id]: 'save' }))
    try {
      const res = await fetch(`/api/firmware-upgrades/${encodeURIComponent(id)}/target`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          model: e.model ?? '',
          currentVersion: e.currentVersion ?? '',
          targetVersion: e.targetVersion ?? '',
          targetUrl: e.targetUrl ?? '',
          checksum: e.checksum ?? '',
        }),
      })
      if (!res.ok) throw new Error(await res.text())
      await fetchItems()
    } catch (err) {
      setError(String(err))
    } finally {
      setBusy((prev) => ({ ...prev, [id]: '' }))
    }
  }

  const startUpgrade = async (id: string) => {
    setBusy((prev) => ({ ...prev, [id]: 'upgrade' }))
    try {
      const res = await fetch(`/api/firmware-upgrades/${encodeURIComponent(id)}/upgrade`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      })
      const body = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(body.message ?? await res.text())
      await fetchItems()
    } catch (err) {
      setError(String(err))
    } finally {
      setBusy((prev) => ({ ...prev, [id]: '' }))
    }
  }

  const upgradeActive = (item: FirmwareItem) => {
    const s = item.upgrade?.status
    return !!s && s !== 'done' && s !== 'failed'
  }

  // #477: el upgrade usa el target GUARDADO (no el buffer de edición), así
  // que el modal resume exactamente lo que se va a flashear.
  const confirmItem = items.find((i) => i.routerId === confirmId)
  const confirmName = confirmItem
    ? sortedRouters.find((r) => r.id === confirmItem.routerId)?.name ?? confirmItem.name
    : ''

  return (
    <div className="space-y-4 md:space-y-5">
      <header className="flex items-start justify-between gap-4">
        <div>
          <h1 className="font-display text-h1 text-text-primary">{t('firmwareUpgrades.title')}</h1>
          <p className="mt-0.5 text-sm text-text-secondary">{t('firmwareUpgrades.subtitle')}</p>
        </div>
        <button
          onClick={fetchItems}
          disabled={loading}
          className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-surface px-3 text-sm font-medium text-text-primary transition-colors hover:bg-elevated disabled:opacity-50"
        >
          <RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} strokeWidth={1.75} />
          {t('common.refresh')}
        </button>
      </header>

      {!isAdmin && (
        <div className="flex items-start gap-3 rounded-xl border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-700 dark:text-amber-300">
          <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
          <span>{t('common.adminOnly')}</span>
        </div>
      )}

      {error && (
        <div className="flex items-start gap-3 rounded-xl border border-rose-500/40 bg-rose-500/10 px-4 py-3 text-sm text-rose-600 dark:text-rose-400">
          <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
          <span>{error}</span>
        </div>
      )}

      {items.length === 0 && !loading && (
        <div className="rounded-2xl border border-border bg-surface p-8 text-center text-sm text-text-secondary">
          {t('firmwareUpgrades.empty')}
        </div>
      )}

      <div className="grid gap-4">
        {items.map((item) => {
          const e = edits[item.routerId] ?? {}
          const active = upgradeActive(item)
          return (
            <div
              key={item.routerId}
              className="rounded-2xl border border-border bg-surface p-5"
            >
              <div className="mb-4 flex items-center gap-3">
                <Cpu className="h-5 w-5 text-accent" strokeWidth={1.75} />
                <div>
                  <h2 className="text-base font-semibold text-text-primary">
                    {sortedRouters.find((r) => r.id === item.routerId)?.name ?? item.name}
                  </h2>
                  <p className="text-xs text-text-muted">{item.model || t('common.unknown')}</p>
                </div>
                {item.upgrade && (
                  <span
                    className={cn(
                      'ml-auto rounded-full border px-2.5 py-0.5 text-xs font-medium',
                      STATUS_COLORS[item.upgrade.status] ?? 'bg-text-muted/10 text-text-muted border-text-muted/30'
                    )}
                  >
                    {t(`firmwareUpgrades.status.${item.upgrade.status}`, { defaultValue: item.upgrade.status })}
                  </span>
                )}
              </div>

              <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                <label className="flex flex-col gap-1">
                  <span className="text-caption text-text-muted">{t('firmwareUpgrades.currentVersion')}</span>
                  <input
                    type="text"
                    value={e.currentVersion ?? ''}
                    onChange={(ev) => updateEdit(item.routerId, { currentVersion: ev.target.value })}
                    disabled={!isAdmin}
                    className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary disabled:opacity-60"
                    placeholder="23.05.3"
                  />
                </label>
                <label className="flex flex-col gap-1">
                  <span className="text-caption text-text-muted">{t('firmwareUpgrades.targetVersion')} *</span>
                  <input
                    type="text"
                    value={e.targetVersion ?? ''}
                    onChange={(ev) => updateEdit(item.routerId, { targetVersion: ev.target.value })}
                    disabled={!isAdmin}
                    className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary disabled:opacity-60"
                    placeholder="23.05.4"
                  />
                </label>
                <label className="flex flex-col gap-1">
                  <span className="text-caption text-text-muted">{t('firmwareUpgrades.model')} *</span>
                  <input
                    type="text"
                    value={e.model ?? ''}
                    onChange={(ev) => updateEdit(item.routerId, { model: ev.target.value })}
                    disabled={!isAdmin}
                    className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary disabled:opacity-60"
                    placeholder="glinet-flint2"
                  />
                </label>
                <label className="flex flex-col gap-1 sm:col-span-2">
                  <span className="text-caption text-text-muted">{t('firmwareUpgrades.targetUrl')} *</span>
                  <input
                    type="text"
                    value={e.targetUrl ?? ''}
                    onChange={(ev) => updateEdit(item.routerId, { targetUrl: ev.target.value })}
                    disabled={!isAdmin}
                    className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary disabled:opacity-60"
                    placeholder="https://downloads.openwrt.org/.../openwrt-...-squashfs-sysupgrade.bin"
                  />
                </label>
                <label className="flex flex-col gap-1">
                  <span className="text-caption text-text-muted">{t('firmwareUpgrades.checksum')}</span>
                  <input
                    type="text"
                    value={e.checksum ?? ''}
                    onChange={(ev) => updateEdit(item.routerId, { checksum: ev.target.value })}
                    disabled={!isAdmin}
                    className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary disabled:opacity-60"
                    placeholder="sha256"
                  />
                </label>
              </div>

              {item.upgrade?.error && (
                <div className="mt-4 rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 py-2 text-sm text-rose-600 dark:text-rose-400">
                  {item.upgrade.error}
                </div>
              )}

              {isAdmin && (
                <div className="mt-4 flex items-center gap-3">
                  <button
                    onClick={() => saveTarget(item.routerId)}
                    disabled={busy[item.routerId] === 'save' || !e.targetVersion || !e.targetUrl || !e.model}
                    className="inline-flex h-9 items-center gap-2 rounded-lg bg-accent px-4 text-sm font-medium text-canvas transition-colors hover:bg-accent/90 disabled:opacity-50"
                  >
                    {busy[item.routerId] === 'save' ? t('common.loading') : t('common.save')}
                  </button>
                  <button
                    onClick={() => setConfirmId(item.routerId)}
                    disabled={active || busy[item.routerId] === 'upgrade' || !item.targetVersion}
                    className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-elevated px-4 text-sm font-medium text-text-primary transition-colors hover:bg-canvas disabled:opacity-50"
                  >
                    {active
                      ? t('firmwareUpgrades.inProgress')
                      : busy[item.routerId] === 'upgrade'
                        ? t('common.loading')
                        : t('firmwareUpgrades.upgrade')}
                  </button>
                </div>
              )}
            </div>
          )
        })}
      </div>

      {/* #477: confirmación antes de descargar/flashear, con el resumen del
          target guardado y los avisos de verificación y reinicio. */}
      <AlertDialog open={!!confirmItem} onOpenChange={(open) => !open && setConfirmId(null)}>
        <AlertDialogContent className="max-w-lg">
          <AlertDialogHeader>
            <AlertDialogTitle>{t('firmwareUpgrades.confirmTitle')}</AlertDialogTitle>
            <AlertDialogDescription>{t('firmwareUpgrades.confirmReboot')}</AlertDialogDescription>
          </AlertDialogHeader>

          {confirmItem && (
            <div className="space-y-3">
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
                <dt className="text-text-muted">{t('firmwareUpgrades.confirmRouter')}</dt>
                <dd className="min-w-0 font-medium text-text-primary">
                  {confirmName}
                  {confirmItem.model ? <span className="font-normal text-text-muted"> · {confirmItem.model}</span> : null}
                </dd>
                <dt className="text-text-muted">{t('firmwareUpgrades.confirmVersion')}</dt>
                <dd className="font-mono text-text-primary">
                  {confirmItem.currentVersion || t('common.unknown')} → {confirmItem.targetVersion}
                </dd>
                <dt className="text-text-muted">{t('firmwareUpgrades.confirmImage')}</dt>
                <dd className="min-w-0 break-all font-mono text-xs leading-relaxed text-text-secondary">
                  {confirmItem.targetUrl}
                </dd>
                <dt className="text-text-muted">{t('firmwareUpgrades.checksum')}</dt>
                <dd className="min-w-0 break-all font-mono text-xs leading-relaxed text-text-secondary">
                  {confirmItem.checksum || t('firmwareUpgrades.confirmNoChecksumShort')}
                </dd>
              </dl>

              {confirmItem.checksum ? (
                <div className="flex items-start gap-2.5 rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-3 py-2.5 text-sm text-emerald-700 dark:text-emerald-300">
                  <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
                  <span>{t('firmwareUpgrades.confirmVerified')}</span>
                </div>
              ) : (
                <div className="flex items-start gap-2.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2.5 text-sm text-amber-700 dark:text-amber-300">
                  <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
                  <span>{t('firmwareUpgrades.confirmNoChecksum')}</span>
                </div>
              )}

              <div className="flex items-start gap-2.5 rounded-lg border border-border bg-canvas px-3 py-2.5 text-sm text-text-secondary">
                <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0 text-text-muted" strokeWidth={1.75} />
                <span>{t('firmwareUpgrades.confirmDowntime')}</span>
              </div>
            </div>
          )}

          <AlertDialogFooter>
            <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault()
                if (!confirmId) return
                const id = confirmId
                setConfirmId(null)
                void startUpgrade(id)
              }}
              disabled={!!confirmId && busy[confirmId] === 'upgrade'}
            >
              <Rocket className="mr-1.5 h-4 w-4" strokeWidth={2} />
              {confirmId && busy[confirmId] === 'upgrade' ? t('common.loading') : t('firmwareUpgrades.confirmAction')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
