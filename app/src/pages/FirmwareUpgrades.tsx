import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNetPulse } from '@/data/DataProvider'
import { useAuth } from '@/data/AuthContext'
import { AlertCircle, CalendarClock, Cpu, Radar, RefreshCw, Rocket, ShieldCheck, Terminal, TriangleAlert, Download } from 'lucide-react'
import { cn } from '@/lib/utils'
import { relTimeFromTs } from '@/i18n'
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
  /** Epoch ms UTC de una programación desatendida (#494); ausente = flujo manual. */
  scheduledFor?: number
  /** Motor del upgrade (#761): owut (ASU) o vacío (URL+checksum). */
  engine?: string
}

interface FirmwareItem {
  routerId: string
  name: string
  model: string
  currentVersion: string
  targetVersion: string
  targetUrl: string
  checksum: string
  detectedModel?: string
  detectedBoard?: string
  detectedVersion?: string
  detectedTarget?: string
  upgrade?: FirmwareUpgrade
}

/** Resultado de la detección + check de owut por router (#695). */
interface OwutStatus {
  owutAvailable: boolean
  checkRan: boolean
  upgradeAvailable: boolean
  buildable: boolean
  missingPkgs?: string[]
  rawOutput?: string
  error?: string
}

/** Versiones destino que ASU puede construir (#761). */
interface OwutVersion {
  version: string
  branch: string
  latest?: boolean
  newer?: boolean
}

interface OwutVersionsResp {
  current: string
  owutAvailable: boolean
  vendorFirmware?: string
  versions: OwutVersion[]
  error?: string
}

/** Programación recurrente (#761). */
interface RecurrenceCfg {
  enabled: boolean
  kind: 'once' | 'weekly' | 'monthly'
  atMs?: number
  dayOfWeek?: number
  dayOfMonth?: number
  time?: string
  lastRunMs?: number
}

const STATUS_COLORS: Record<string, string> = {
  requested: 'bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/30',
  running: 'bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/30',
  downloading: 'bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/30',
  backing_up: 'bg-purple-500/10 text-purple-600 dark:text-purple-400 border-purple-500/30',
  verifying: 'bg-cyan-500/10 text-cyan-600 dark:text-cyan-400 border-cyan-500/30',
  flashing: 'bg-orange-500/10 text-orange-600 dark:text-orange-400 border-orange-500/30',
  rebooting: 'bg-indigo-500/10 text-indigo-600 dark:text-indigo-400 border-indigo-500/30',
  done: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/30',
  failed: 'bg-rose-500/10 text-rose-600 dark:text-rose-400 border-rose-500/30',
  scheduled: 'bg-indigo-500/10 text-indigo-600 dark:text-indigo-400 border-indigo-500/30',
}

/** Paquetes del propio stack: sus ficheros sobreviven al update
 * (NetPulse los añade a /etc/sysupgrade.conf antes de flashear). */
const STACK_PKGS = new Set(['netgrip', 'netpulse-agent', 'owpanel'])

/** versionRe: misma regla que el backend para aceptar un targetVersion. */
const versionRe = /^\d+\.\d+(\.\d+)?(-[a-zA-Z0-9.]+)?$/

/** majorJumpJS: mismo criterio que el backend (primer componente distinto). */
function majorJumpJS(current: string, target: string): boolean {
  if (!current || !target) return false
  return current.split('.')[0] !== target.split('.')[0]
}

/** dowLabel: nombre del día de la semana en el idioma de la UI. */
function dowLabel(lang: string, dow: number): string {
  // 13-Sep-2026 es domingo: día i = 13 + i.
  return new Date(2026, 8, 13 + dow).toLocaleDateString(lang, { weekday: 'long' })
}

export default function FirmwareUpgrades() {
  const { t, i18n } = useTranslation()
  const auth = useAuth()
  const { routers } = useNetPulse()
  const isAdmin = auth?.role === 'admin'

  const [items, setItems] = useState<FirmwareItem[]>([])
  const [edits, setEdits] = useState<Record<string, Partial<FirmwareItem>>>({})
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState<Record<string, string>>({})
  const [error, setError] = useState('')
  const [confirmId, setConfirmId] = useState<string | null>(null)
  // #629: estado de "autodetectar imagen" por router.
  const [resolveBusy, setResolveBusy] = useState<Record<string, boolean>>({})
  // #695: estado del check owut por router.
  const [owutBusy, setOwutBusy] = useState<Record<string, boolean>>({})
  const [owutResult, setOwutResult] = useState<Record<string, OwutStatus>>({})
  // #761: versiones para el desplegable e instalación de owut.
  const [owutVersions, setOwutVersions] = useState<Record<string, OwutVersionsResp>>({})
  const [versionsBusy, setVersionsBusy] = useState<Record<string, boolean>>({})
  const [installBusy, setInstallBusy] = useState<Record<string, boolean>>({})
  const [owutConfirmId, setOwutConfirmId] = useState<string | null>(null)
  // #761: paquetes a excluir de la build ASU (locales sin feed, p. ej. netgrip).
  const [removePkgs, setRemovePkgs] = useState<Record<string, string>>({})
  // #761: programación recurrente.
  const [recurrence, setRecurrence] = useState<Record<string, RecurrenceCfg>>({})
  const [recNext, setRecNext] = useState<Record<string, number>>({})
  const [scheduleId, setScheduleId] = useState<string | null>(null)
  const [recKind, setRecKind] = useState<'once' | 'weekly' | 'monthly'>('once')
  const [recAt, setRecAt] = useState('')
  const [recDow, setRecDow] = useState(1)
  const [recDom, setRecDom] = useState(1)
  const [recTime, setRecTime] = useState('04:00')

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
        // #477 P2 / #761: modelo y versión actual SIEMPRE prefilleados con
        // lo detectado del board info; solo editables sin detección posible.
        initialEdits[it.routerId] = {
          model: it.detectedBoard || it.detectedModel || it.model || '',
          currentVersion: it.detectedVersion || it.currentVersion || '',
          targetVersion: it.targetVersion || it.detectedVersion || '',
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

  const loadRecurrence = async (id: string) => {
    try {
      const res = await fetch(`/api/firmware-upgrades/${encodeURIComponent(id)}/recurrence`)
      if (!res.ok) return
      const body = (await res.json()) as { recurrence: RecurrenceCfg; nextRunMs?: number }
      setRecurrence((prev) => ({ ...prev, [id]: body.recurrence }))
      const next = body.nextRunMs ?? 0
      setRecNext((prev) => ({ ...prev, [id]: next }))
    } catch {
      // fail-silent
    }
  }

  // #761: versiones del desplegable (owut versions en el router).
  const fetchVersions = async (id: string) => {
    setVersionsBusy((prev) => ({ ...prev, [id]: true }))
    try {
      const res = await fetch(`/api/firmware-upgrades/${encodeURIComponent(id)}/owut-versions`)
      const body = (await res.json().catch(() => ({}))) as OwutVersionsResp
      if (!res.ok) throw new Error((body as { error?: { message?: string } })?.error?.message ?? `HTTP ${res.status}`)
      setOwutVersions((prev) => ({ ...prev, [id]: body }))
    } catch (e) {
      setOwutVersions((prev) => ({ ...prev, [id]: { current: '', owutAvailable: false, versions: [], error: String(e) } }))
    } finally {
      setVersionsBusy((prev) => ({ ...prev, [id]: false }))
    }
  }

  useEffect(() => {
    fetchItems()
  }, [])

  // Al cargar items: recurrencia y versiones por router (admin).
  useEffect(() => {
    if (items.length && isAdmin) {
      items.forEach((it) => {
        void loadRecurrence(it.routerId)
        if (!owutVersions[it.routerId]) void fetchVersions(it.routerId)
      })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items])

  // #761: auto-refresco mientras haya un upgrade en curso (owut tarda y el
  // router se reinicia al final; el estado vive en el server).
  const anyActive = items.some((it) => {
    const s = it.upgrade?.status
    return !!s && s !== 'done' && s !== 'failed'
  })
  useEffect(() => {
    if (!anyActive) return
    const iv = setInterval(() => void fetchItems(), 10_000)
    return () => clearInterval(iv)
  }, [anyActive])

  const updateEdit = (id: string, patch: Partial<FirmwareItem>) => {
    setEdits((prev) => ({ ...prev, [id]: { ...prev[id], ...patch } }))
  }

  // #629: resolver la imagen del firmware a partir del board info detectado
  // (vía /image) y prerrellenar targetVersion/targetUrl/checksum. Se resuelve
  // para la versión objetivo ya escrita (edits), o la detectada si no hay.
  const resolveImage = async (id: string) => {
    const e = edits[id]
    if (!e) return
    setResolveBusy((prev) => ({ ...prev, [id]: true }))
    setError('')
    try {
      const item = items.find((x) => x.routerId === id)
      const ver = e.targetVersion || item?.detectedVersion || ''
      const qs = ver ? `?version=${encodeURIComponent(ver)}` : ''
      const res = await fetch(`/api/firmware-upgrades/${encodeURIComponent(id)}/image${qs}`)
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        throw new Error(body?.error?.message ?? body?.message ?? `HTTP ${res.status}`)
      }
      const img = (await res.json()) as { version?: string; url: string; checksum?: string }
      setEdits((prev) => ({
        ...prev,
        [id]: {
          ...prev[id],
          targetVersion: prev[id]?.targetVersion || img.version || '',
          targetUrl: img.url,
          checksum: img.checksum || '',
        },
      }))
    } catch (err) {
      setError(String(err))
    } finally {
      setResolveBusy((prev) => ({ ...prev, [id]: false }))
    }
  }

  // #695/#761: comprobar con owut ("Comprobar").
  const runOwutCheck = async (id: string) => {
    setOwutBusy((prev) => ({ ...prev, [id]: true }))
    setError('')
    try {
      const res = await fetch(`/api/firmware-upgrades/${encodeURIComponent(id)}/owut-check`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      })
      const body = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(body.message ?? body.error ?? (await res.text()))
      setOwutResult((prev) => ({ ...prev, [id]: body as OwutStatus }))
    } catch (err) {
      setError(String(err))
    } finally {
      setOwutBusy((prev) => ({ ...prev, [id]: false }))
    }
  }

  // #761: instalar owut en el router (apk/opkg).
  const installOwut = async (id: string) => {
    setInstallBusy((prev) => ({ ...prev, [id]: true }))
    setError('')
    try {
      const res = await fetch(`/api/firmware-upgrades/${encodeURIComponent(id)}/owut-install`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      })
      const body = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(body?.error?.message ?? body?.message ?? `HTTP ${res.status}`)
      await fetchVersions(id)
      await runOwutCheck(id)
    } catch (err) {
      setError(String(err))
    } finally {
      setInstallBusy((prev) => ({ ...prev, [id]: false }))
    }
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

  // #519: descartar el aviso de un intento de upgrade fallido/obsoleto.
  const dismissUpgrade = async (id: string) => {
    setBusy((prev) => ({ ...prev, [id]: 'dismiss' }))
    try {
      const res = await fetch(`/api/firmware-upgrades/${encodeURIComponent(id)}/failure`, { method: 'DELETE' })
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
      if (!res.ok) throw new Error(body.message ?? (await res.text()))
      await fetchItems()
    } catch (err) {
      setError(String(err))
    } finally {
      setBusy((prev) => ({ ...prev, [id]: '' }))
    }
  }

  // #761: attended upgrade vía owut/ASU desde lo detectado/elegido.
  const startOwutUpgrade = async (id: string) => {
    const e = edits[id]
    if (!e?.targetVersion) return
    setBusy((prev) => ({ ...prev, [id]: 'upgrade' }))
    try {
      const res = await fetch(`/api/firmware-upgrades/${encodeURIComponent(id)}/owut-upgrade`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          targetVersion: e.targetVersion,
          removePackages: (removePkgs[id] ?? '').split(/[\s,]+/).filter(Boolean),
        }),
      })
      const body = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(body?.error?.message ?? body?.message ?? `HTTP ${res.status}`)
      await fetchItems()
    } catch (err) {
      setError(String(err))
    } finally {
      setBusy((prev) => ({ ...prev, [id]: '' }))
    }
  }

  // #761: guardar la programación recurrente (una vez / semanal / mensual).
  const saveRecurrence = async (id: string) => {
    setError('')
    const payload: RecurrenceCfg = { enabled: true, kind: recKind }
    if (recKind === 'once') {
      if (!recAt) {
        setError(t('firmwareUpgrades.recNeedDate'))
        return
      }
      payload.atMs = new Date(recAt).getTime()
    } else if (recKind === 'weekly') {
      payload.dayOfWeek = recDow
      payload.time = recTime
    } else {
      payload.dayOfMonth = recDom
      payload.time = recTime
    }
    try {
      const res = await fetch(`/api/firmware-upgrades/${encodeURIComponent(id)}/recurrence`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
      const body = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error(body?.error?.message ?? body?.message ?? `HTTP ${res.status}`)
      setScheduleId(null)
      await loadRecurrence(id)
    } catch (err) {
      setError(String(err))
    }
  }

  const removeRecurrence = async (id: string) => {
    try {
      const res = await fetch(`/api/firmware-upgrades/${encodeURIComponent(id)}/recurrence`, { method: 'DELETE' })
      if (!res.ok) throw new Error(await res.text())
      await loadRecurrence(id)
    } catch (err) {
      setError(String(err))
    }
  }

  const upgradeActive = (item: FirmwareItem) => {
    const s = item.upgrade?.status
    return !!s && s !== 'done' && s !== 'failed' && s !== 'scheduled'
  }

  // #494: cancelar una programación one-shot aún no iniciada (legacy).
  const cancelSchedule = async (id: string) => {
    setBusy((prev) => ({ ...prev, [id]: 'cancel' }))
    try {
      const res = await fetch(`/api/firmware-upgrades/${encodeURIComponent(id)}/schedule`, { method: 'DELETE' })
      if (!res.ok) throw new Error(await res.text())
      await fetchItems()
    } catch (err) {
      setError(String(err))
    } finally {
      setBusy((prev) => ({ ...prev, [id]: '' }))
    }
  }

  const scheduledPending = (item: FirmwareItem) => item.upgrade?.status === 'scheduled'

  // #477: el upgrade usa el target GUARDADO (no el buffer de edición), así
  // que el modal resume exactamente lo que se va a flashear.
  const confirmItem = items.find((i) => i.routerId === confirmId)
  const confirmName = confirmItem
    ? sortedRouters.find((r) => r.id === confirmItem.routerId)?.name ?? confirmItem.name
    : ''
  const owutConfirm = items.find((i) => i.routerId === owutConfirmId)
  const owutConfirmName = owutConfirm
    ? sortedRouters.find((r) => r.id === owutConfirm.routerId)?.name ?? owutConfirm.name
    : ''
  const scheduleItem = items.find((i) => i.routerId === scheduleId)
  const scheduleName = scheduleItem
    ? sortedRouters.find((r) => r.id === scheduleItem.routerId)?.name ?? scheduleItem.name
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
          const scheduled = scheduledPending(item)
          const owut = owutResult[item.routerId]
          const versions = owutVersions[item.routerId]
          const vendorFw = versions?.vendorFirmware
          const owutAvail = !vendorFw && (owut?.owutAvailable ?? versions?.owutAvailable ?? false)
          const detectedCurrent = item.detectedVersion || e.currentVersion || ''
          const detectedModel = item.detectedBoard || item.detectedModel || ''
          const rec = recurrence[item.routerId]
          const targetSel = e.targetVersion ?? ''
          const majorWarn = majorJumpJS(detectedCurrent, targetSel)
          // Opciones del desplegable (#761): la instalada + superiores (si
          // las hubiera) + el target ya guardado (downgrade explícito previo).
          const currentVer = item.detectedVersion || ''
          const opts: string[] = []
          if (currentVer && !opts.includes(currentVer)) opts.push(currentVer)
          if (
            item.targetVersion &&
            item.targetVersion !== currentVer &&
            versionRe.test(item.targetVersion) &&
            !opts.includes(item.targetVersion)
          ) {
            opts.push(item.targetVersion)
          }
          ;(versions?.versions ?? [])
            .filter((v) => v.newer)
            .forEach((v) => {
              if (!opts.includes(v.version)) opts.push(v.version)
            })
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
                  <p className="text-xs text-text-muted">{detectedModel || item.model || t('common.unknown')}</p>
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

              {active && item.upgrade && (
                <div className="mb-4 flex items-start gap-2 rounded-lg border border-blue-500/30 bg-blue-500/10 px-3 py-2.5 text-sm text-blue-700 dark:text-blue-300">
                  <RefreshCw className="mt-0.5 h-4 w-4 shrink-0 animate-pulse" strokeWidth={1.75} />
                  <span>
                    {item.upgrade.engine === 'owut'
                      ? t('firmwareUpgrades.upgradeRunningOwut', {
                          elapsed: relTimeFromTs(item.upgrade.startedAt),
                          step:
                            item.upgrade.status === 'rebooting'
                              ? t('firmwareUpgrades.stepRebooting')
                              : t('firmwareUpgrades.stepWorking'),
                        })
                      : t('firmwareUpgrades.inProgress')}
                  </span>
                </div>
              )}

              <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                <label className="flex flex-col gap-1">
                  <span className="text-caption text-text-muted">{t('firmwareUpgrades.model')} *</span>
                  <input
                    type="text"
                    value={detectedModel || e.model || ''}
                    onChange={(ev) => updateEdit(item.routerId, { model: ev.target.value })}
                    disabled={!isAdmin || !!detectedModel}
                    className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary disabled:opacity-60"
                    placeholder={detectedModel ? '' : 'glinet-flint2'}
                  />
                </label>
                <label className="flex flex-col gap-1">
                  <span className="text-caption text-text-muted">{t('firmwareUpgrades.currentVersion')}</span>
                  <input
                    type="text"
                    value={detectedCurrent}
                    onChange={(ev) => updateEdit(item.routerId, { currentVersion: ev.target.value })}
                    disabled={!isAdmin || !!item.detectedVersion}
                    className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary disabled:opacity-60"
                    placeholder={item.detectedVersion ? '' : '23.05.3'}
                  />
                </label>
                {!vendorFw && (
                <label className="flex flex-col gap-1">
                  <span className="text-caption text-text-muted">{t('firmwareUpgrades.targetVersion')} *</span>
                  <select
                    value={targetSel}
                    onChange={(ev) => updateEdit(item.routerId, { targetVersion: ev.target.value })}
                    disabled={!isAdmin || active || versionsBusy[item.routerId]}
                    className="h-[38px] rounded-lg border border-border bg-canvas px-3 text-sm text-text-primary disabled:opacity-60"
                  >
                    {opts.length === 0 && <option value="">{t('firmwareUpgrades.notDetected')}</option>}
                    {opts.map((v) => {
                      const info = versions?.versions.find((x) => x.version === v)
                      const label = [
                        v,
                        v === currentVer ? t('firmwareUpgrades.installedTag') : '',
                        info?.latest ? t('firmwareUpgrades.latestTag') : '',
                      ]
                        .filter(Boolean)
                        .join(' · ')
                      return (
                        <option key={v} value={v}>
                          {label}
                        </option>
                      )
                    })}
                  </select>
                </label>
                )}
              </div>

              {vendorFw && (
                <div className="mt-3 flex items-start gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2.5 text-sm text-amber-700 dark:text-amber-300">
                  <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
                  <span>{t('firmwareUpgrades.vendorNote')}</span>
                </div>
              )}

              {!vendorFw && majorWarn && (
                <div className="mt-3 flex items-start gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-300">
                  <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
                  <span>{t('firmwareUpgrades.majorWarn', { from: detectedCurrent, to: targetSel })}</span>
                </div>
              )}

              {!vendorFw && (
              <details className="mt-3 rounded-lg border border-border bg-canvas px-3 py-2">
                <summary className="cursor-pointer text-sm font-medium text-text-secondary">
                  {t('firmwareUpgrades.advanced')}
                </summary>
                <div className="mt-3 grid gap-4 sm:grid-cols-2">
                  <label className="flex flex-col gap-1 sm:col-span-2">
                    <span className="text-caption text-text-muted">{t('firmwareUpgrades.targetUrl')}</span>
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
                  {isAdmin && item.detectedBoard && (
                    <div className="flex items-end">
                      <button
                        type="button"
                        onClick={() => void resolveImage(item.routerId)}
                        disabled={resolveBusy[item.routerId]}
                        className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border bg-elevated px-3 text-xs font-medium text-text-secondary transition-colors duration-150 hover:border-accent/40 hover:text-accent disabled:opacity-50"
                      >
                        <Radar className={cn('h-3.5 w-3.5', resolveBusy[item.routerId] && 'animate-pulse')} strokeWidth={1.75} />
                        {resolveBusy[item.routerId] ? t('firmwareUpgrades.detectImageBusy') : t('firmwareUpgrades.detectImage')}
                      </button>
                    </div>
                  )}
                </div>
              </details>
              )}

              {owut && (
                <div className="mt-3 space-y-2">
                  {owut.error && (
                    <div className="flex items-start gap-2 rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 py-2 text-sm text-rose-600 dark:text-rose-400">
                      <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
                      <span>{t('firmwareUpgrades.owutCheckFailed')}</span>
                    </div>
                  )}
                  {owut.checkRan && owut.buildable && owut.upgradeAvailable && (
                    <div className="flex items-start gap-2 rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-3 py-2.5 text-sm text-emerald-700 dark:text-emerald-300">
                      <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
                      <span>{t('firmwareUpgrades.owutUpgradeAvailable')}</span>
                    </div>
                  )}
                  {owut.checkRan && owut.buildable && !owut.upgradeAvailable && (
                    <div className="flex items-start gap-2 rounded-lg border border-border bg-canvas px-3 py-2.5 text-sm text-text-secondary">
                      <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-text-muted" strokeWidth={1.75} />
                      <span>{t('firmwareUpgrades.owutUpToDate')}</span>
                    </div>
                  )}
                  {owut.checkRan && !owut.buildable && (
                    <div className="flex items-start gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2.5 text-sm text-amber-700 dark:text-amber-300">
                      <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
                      <span>
                        {owut.missingPkgs?.length
                          ? (owut.missingPkgs as string[]).some((pkg) => STACK_PKGS.has(pkg))
                            ? t('firmwareUpgrades.missingPkgsBannerStack', { pkgs: owut.missingPkgs.join(', ') })
                            : t('firmwareUpgrades.missingPkgsBanner', { pkgs: owut.missingPkgs.join(', ') })
                          : `${t('firmwareUpgrades.owutNotBuildable')} ${t('firmwareUpgrades.owutNotBuildableHint')}`}
                      </span>
                    </div>
                  )}
                  {owut.rawOutput && (
                    <details className="rounded-lg border border-border bg-canvas px-3 py-2">
                      <summary className="cursor-pointer text-sm font-medium text-text-secondary">
                        {t('firmwareUpgrades.owutOutput')}
                      </summary>
                      <pre className="mt-2 overflow-x-auto whitespace-pre-wrap break-all font-mono text-xs leading-relaxed text-text-secondary">
                        {owut.rawOutput}
                      </pre>
                    </details>
                  )}
                </div>
              )}

              {item.upgrade?.error && (
                <div className="mt-4 flex items-center justify-between gap-3 rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 py-2 text-sm text-rose-600 dark:text-rose-400">
                  {/* Timestamp del fallo: un error antiguo NO debe leerse como
                      estado presente del agente (feedback #477). */}
                  <span>
                    {t('firmwareUpgrades.lastFailure')}
                    {relTimeFromTs(item.upgrade.startedAt) ? ` (${relTimeFromTs(item.upgrade.startedAt)})` : ''}:{' '}
                    {item.upgrade.error || t('firmwareUpgrades.emptyFailure')}
                  </span>
                  {isAdmin && (
                    <button
                      onClick={() => void dismissUpgrade(item.routerId)}
                      disabled={busy[item.routerId] === 'dismiss' || active}
                      className="inline-flex h-7 shrink-0 items-center gap-1.5 rounded-lg border border-rose-500/40 px-2.5 text-xs font-medium transition-colors hover:bg-rose-500/10 disabled:opacity-50"
                    >
                      {busy[item.routerId] === 'dismiss' ? t('common.loading') : t('firmwareUpgrades.dismiss')}
                    </button>
                  )}
                </div>
              )}

              {isAdmin && !vendorFw && (
                <div className="mt-4 space-y-3 border-t border-border pt-3">
                  <div className="flex flex-wrap items-center gap-3">
                    {!owutAvail && versions !== undefined && (
                      <button
                        type="button"
                        onClick={() => void installOwut(item.routerId)}
                        disabled={installBusy[item.routerId]}
                        className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-elevated px-4 text-sm font-medium text-text-primary transition-colors hover:bg-canvas disabled:opacity-50"
                      >
                        <Download className={cn('h-4 w-4', installBusy[item.routerId] && 'animate-pulse')} strokeWidth={1.75} />
                        {installBusy[item.routerId] ? t('firmwareUpgrades.installingOwut') : t('firmwareUpgrades.installOwut')}
                      </button>
                    )}
                    <button
                      type="button"
                      onClick={() => void runOwutCheck(item.routerId)}
                      disabled={owutBusy[item.routerId] || !owutAvail}
                      title={!owutAvail ? t('firmwareUpgrades.owutUnavailable') : undefined}
                      className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-elevated px-4 text-sm font-medium text-text-primary transition-colors hover:bg-canvas disabled:opacity-50"
                    >
                      <Terminal className={cn('h-4 w-4', owutBusy[item.routerId] && 'animate-pulse')} strokeWidth={1.75} />
                      {owutBusy[item.routerId] ? t('firmwareUpgrades.owutBusy') : t('firmwareUpgrades.check')}
                    </button>
                    <button
                      onClick={() => saveTarget(item.routerId)}
                      disabled={busy[item.routerId] === 'save' || !e.targetVersion || !e.model}
                      className="inline-flex h-9 items-center gap-2 rounded-lg bg-accent px-4 text-sm font-medium text-canvas transition-colors hover:bg-accent/90 disabled:opacity-50"
                    >
                      {busy[item.routerId] === 'save' ? t('common.loading') : t('common.save')}
                    </button>
                    <button
                      onClick={() => {
                        if (owutAvail) {
                          const missing = owut?.missingPkgs ?? []
                          if (missing.length && !(removePkgs[item.routerId] ?? '').trim()) {
                            setRemovePkgs((prev) => ({ ...prev, [item.routerId]: missing.join(' ') }))
                          }
                          setOwutConfirmId(item.routerId)
                        } else {
                          setConfirmId(item.routerId)
                        }
                      }}
                      disabled={active || busy[item.routerId] === 'upgrade' || scheduled || !targetSel}
                      className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-elevated px-4 text-sm font-medium text-text-primary transition-colors hover:bg-canvas disabled:opacity-50"
                    >
                      {active
                        ? t('firmwareUpgrades.inProgress')
                        : busy[item.routerId] === 'upgrade'
                          ? t('common.loading')
                          : t('firmwareUpgrades.upgrade')}
                    </button>
                    <button
                      onClick={() => {
                        const rc = recurrence[item.routerId]
                        setRecKind(rc?.enabled ? rc.kind : 'once')
                        setRecAt('')
                        setScheduleId(item.routerId)
                      }}
                      disabled={active}
                      className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-elevated px-4 text-sm font-medium text-text-primary transition-colors hover:bg-canvas disabled:opacity-50"
                    >
                      <CalendarClock className="h-4 w-4 text-accent" strokeWidth={1.75} />
                      {t('firmwareUpgrades.schedule')}
                    </button>
                  </div>

                  {scheduled ? (
                    // #494: programación one-shot pendiente (legacy).
                    <div className="flex flex-wrap items-center gap-3">
                      <span className="inline-flex items-center gap-1.5 text-sm text-text-secondary">
                        <CalendarClock className="h-4 w-4 text-accent" strokeWidth={1.75} />
                        {t('firmwareUpgrades.scheduledFor', {
                          time: item.upgrade?.scheduledFor ? new Date(item.upgrade.scheduledFor).toLocaleString() : '',
                        })}
                      </span>
                      <button
                        onClick={() => void cancelSchedule(item.routerId)}
                        disabled={busy[item.routerId] === 'cancel'}
                        className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-elevated px-4 text-sm font-medium text-text-primary transition-colors hover:bg-canvas disabled:opacity-50"
                      >
                        {busy[item.routerId] === 'cancel' ? t('common.loading') : t('firmwareUpgrades.cancelSchedule')}
                      </button>
                    </div>
                  ) : rec?.enabled ? (
                    <div className="flex flex-wrap items-center gap-3">
                      <span className="inline-flex items-center gap-1.5 text-sm text-text-secondary">
                        <CalendarClock className="h-4 w-4 text-accent" strokeWidth={1.75} />
                        {t('firmwareUpgrades.recScheduled', {
                          when: (recNext[item.routerId] ?? 0) > 0 ? new Date(recNext[item.routerId] ?? 0).toLocaleString() : '',
                          kind: t(`firmwareUpgrades.rec_${rec.kind}`),
                        })}
                      </span>
                      <button
                        onClick={() => void removeRecurrence(item.routerId)}
                        className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-elevated px-4 text-sm font-medium text-text-primary transition-colors hover:bg-canvas disabled:opacity-50"
                      >
                        {t('firmwareUpgrades.recRemove')}
                      </button>
                    </div>
                  ) : null}
                </div>
              )}
            </div>
          )
        })}
      </div>

      {/* #477: confirmación del motor clásico (URL + checksum). */}
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

      {/* #761: confirmación owut/ASU (actualizar desde lo detectado). */}
      <AlertDialog open={!!owutConfirm} onOpenChange={(open) => !open && setOwutConfirmId(null)}>
        <AlertDialogContent className="max-w-lg">
          <AlertDialogHeader>
            <AlertDialogTitle>{t('firmwareUpgrades.owutConfirmTitle')}</AlertDialogTitle>
            <AlertDialogDescription>{t('firmwareUpgrades.owutConfirmBody')}</AlertDialogDescription>
          </AlertDialogHeader>

          {owutConfirm && (
            <div className="space-y-3">
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
                <dt className="text-text-muted">{t('firmwareUpgrades.confirmRouter')}</dt>
                <dd className="min-w-0 font-medium text-text-primary">{owutConfirmName}</dd>
                <dt className="text-text-muted">{t('firmwareUpgrades.confirmVersion')}</dt>
                <dd className="font-mono text-text-primary">
                  {owutConfirm.detectedVersion || owutConfirm.currentVersion || t('common.unknown')} →{' '}
                  {edits[owutConfirm.routerId]?.targetVersion}
                </dd>
              </dl>

              <div className="flex items-start gap-2.5 rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-3 py-2.5 text-sm text-emerald-700 dark:text-emerald-300">
                <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
                <span>{t('firmwareUpgrades.owutConfirmPackages')}</span>
              </div>

              <div className="flex items-start gap-2.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2.5 text-sm text-amber-700 dark:text-amber-300">
                <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
                <span>{t('firmwareUpgrades.owutConfirmVendor')}</span>
              </div>

              <div className="flex items-start gap-2.5 rounded-lg border border-border bg-canvas px-3 py-2.5 text-sm text-text-secondary">
                <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0 text-text-muted" strokeWidth={1.75} />
                <span>{t('firmwareUpgrades.confirmDowntime')}</span>
              </div>

              <label className="flex flex-col gap-1">
                <span className="text-caption text-text-muted">{t('firmwareUpgrades.excludePkgs')}</span>
                <input
                  type="text"
                  value={removePkgs[owutConfirm.routerId] ?? ''}
                  onChange={(ev) => setRemovePkgs((prev) => ({ ...prev, [owutConfirm.routerId]: ev.target.value }))}
                  placeholder="netgrip"
                  className="rounded-lg border border-border bg-canvas px-3 py-2 font-mono text-sm text-text-primary"
                />
                <span className="text-xs text-text-muted">{t('firmwareUpgrades.excludePkgsHint')}</span>
              </label>
            </div>
          )}

          <AlertDialogFooter>
            <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault()
                if (!owutConfirmId) return
                const id = owutConfirmId
                setOwutConfirmId(null)
                void startOwutUpgrade(id)
              }}
              disabled={!!owutConfirmId && busy[owutConfirmId] === 'upgrade'}
            >
              <Rocket className="mr-1.5 h-4 w-4" strokeWidth={2} />
              {owutConfirmId && busy[owutConfirmId] === 'upgrade' ? t('common.loading') : t('firmwareUpgrades.confirmAction')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* #761: diálogo de programación (fecha/hora + recurrencia). */}
      <AlertDialog open={!!scheduleItem} onOpenChange={(open) => !open && setScheduleId(null)}>
        <AlertDialogContent className="max-w-md">
          <AlertDialogHeader>
            <AlertDialogTitle>{t('firmwareUpgrades.scheduleTitle')}</AlertDialogTitle>
            <AlertDialogDescription>{t('firmwareUpgrades.scheduleBody')}</AlertDialogDescription>
          </AlertDialogHeader>

          {scheduleItem && (
            <div className="space-y-4">
              <p className="text-sm text-text-secondary">
                {scheduleName} · <span className="font-mono">{detectedVersionOf(scheduleItem) || t('common.unknown')}</span> →{' '}
                <span className="font-mono">{edits[scheduleItem.routerId]?.targetVersion}</span>
              </p>

              <div className="flex gap-2">
                {(['once', 'weekly', 'monthly'] as const).map((k) => (
                  <button
                    key={k}
                    type="button"
                    onClick={() => setRecKind(k)}
                    className={cn(
                      'h-9 flex-1 rounded-lg border px-3 text-sm font-medium transition-colors',
                      recKind === k
                        ? 'border-accent bg-accent/10 text-accent'
                        : 'border-border bg-canvas text-text-secondary hover:bg-elevated'
                    )}
                  >
                    {t(`firmwareUpgrades.rec_${k}`)}
                  </button>
                ))}
              </div>

              {recKind === 'once' && (
                <label className="flex flex-col gap-1">
                  <span className="text-caption text-text-muted">{t('firmwareUpgrades.recDate')}</span>
                  <input
                    type="datetime-local"
                    value={recAt}
                    onChange={(ev) => setRecAt(ev.target.value)}
                    className="h-9 rounded-lg border border-border bg-canvas px-3 text-sm text-text-primary"
                  />
                </label>
              )}
              {recKind === 'weekly' && (
                <div className="grid grid-cols-2 gap-3">
                  <label className="flex flex-col gap-1">
                    <span className="text-caption text-text-muted">{t('firmwareUpgrades.recDay')}</span>
                    <select
                      value={recDow}
                      onChange={(ev) => setRecDow(Number(ev.target.value))}
                      className="h-9 rounded-lg border border-border bg-canvas px-3 text-sm text-text-primary"
                    >
                      {[1, 2, 3, 4, 5, 6, 0].map((d) => (
                        <option key={d} value={d}>
                          {dowLabel(i18n.language, d)}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption text-text-muted">{t('firmwareUpgrades.recTime')}</span>
                    <input
                      type="time"
                      value={recTime}
                      onChange={(ev) => setRecTime(ev.target.value)}
                      className="h-9 rounded-lg border border-border bg-canvas px-3 text-sm text-text-primary"
                    />
                  </label>
                </div>
              )}
              {recKind === 'monthly' && (
                <div className="grid grid-cols-2 gap-3">
                  <label className="flex flex-col gap-1">
                    <span className="text-caption text-text-muted">{t('firmwareUpgrades.recDayOfMonth')}</span>
                    <select
                      value={recDom}
                      onChange={(ev) => setRecDom(Number(ev.target.value))}
                      className="h-9 rounded-lg border border-border bg-canvas px-3 text-sm text-text-primary"
                    >
                      {Array.from({ length: 31 }, (_, i) => i + 1).map((d) => (
                        <option key={d} value={d}>
                          {d}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption text-text-muted">{t('firmwareUpgrades.recTime')}</span>
                    <input
                      type="time"
                      value={recTime}
                      onChange={(ev) => setRecTime(ev.target.value)}
                      className="h-9 rounded-lg border border-border bg-canvas px-3 text-sm text-text-primary"
                    />
                  </label>
                </div>
              )}

              <p className="text-xs text-text-muted">{t('firmwareUpgrades.recIdempotentHint')}</p>
            </div>
          )}

          <AlertDialogFooter>
            <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault()
                if (!scheduleId) return
                const id = scheduleId
                setScheduleId(null)
                void saveRecurrence(id)
              }}
            >
              <CalendarClock className="mr-1.5 h-4 w-4" strokeWidth={2} />
              {t('firmwareUpgrades.schedule')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

function detectedVersionOf(item: FirmwareItem): string {
  return item.detectedVersion || item.currentVersion || ''
}
