import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import {
  BadgeCheck,
  BellOff,
  BellRing,
  Check,
  ChevronDown,
  Copy,
  Database,
  Download,
  ExternalLink,
  Eye,
  EyeOff,
  FileText,
  FlaskConical,
  Gauge,
  Github,
  HardDrive,
  Heart,
  History,
  KeyRound,
  Loader2,
  LogOut,
  MonitorSmartphone,
  Moon,
  Pencil,
  Plus,
  Lock,
   Radar,
   RefreshCw,
   RotateCcw,
   RotateCw,
  Router as RouterIcon,
   Server,
    Shield,
    ShieldCheck,
   Sun,
   Trash2,
   UserCog,
   Users,
   Volume2,
   Wifi,
   X,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { HealthRing } from '@/components/HealthRing'
import { KnownMacsManager } from '@/components/KnownMacsManager'
import { SegmentedControl } from '@/components/SegmentedControl'
import { TokensManager } from '@/components/TokensManager'
import { TopologyOverridesManager } from '@/components/topology/TopologyOverridesManager'
import { ReadinessPanel, type UpdateReadiness } from '@/components/UpdateReadiness'
import { UpdateDialog } from '@/components/UpdateDialog'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Slider } from '@/components/ui/slider'
import { Switch } from '@/components/ui/switch'
import { useNetPulse } from '@/data/DataProvider'
import { useAuth } from '@/data/AuthContext'
import { getVapidKey, postPushSubscribe, postPushUnsubscribe, pushContext, urlBase64ToUint8Array } from '@/data/push'
import { useServicesVisibility } from '@/hooks/useServicesVisibility'
import type { ServicesVisibility } from '@/hooks/useServicesVisibility'
import { relTimeFromTs } from '@/i18n'
import { cn, copyToClipboard, exitDemo } from '@/lib/utils'
import { notifyBanner } from '@/lib/update-check'
import { PALETTES, type PaletteId, type ThemeMode } from '@/lib/theme-boot'
import TelegramCard from '@/components/TelegramCard'
import pkg from '../../package.json'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Estado persistido en localStorage (settings.md §Interactions) */
function useStoredState<T>(key: string, initial: T): [T, (v: T) => void] {
  const [state, setState] = useState<T>(() => {
    try {
      const raw = localStorage.getItem(key)
      return raw !== null ? (JSON.parse(raw) as T) : initial
    } catch {
      return initial
    }
  })
  const set = useCallback(
    (v: T) => {
      setState(v)
      try {
        localStorage.setItem(key, JSON.stringify(v))
      } catch {
        /* modo privado */
      }
    },
    [key],
  )
  return [state, set]
}

function playBeep() {
  try {
    const Ctor =
      window.AudioContext ??
      (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
    if (!Ctor) return
    const ctx = new Ctor()
    const osc = ctx.createOscillator()
    const gain = ctx.createGain()
    osc.type = 'sine'
    osc.frequency.value = 880
    gain.gain.setValueAtTime(0.0001, ctx.currentTime)
    gain.gain.exponentialRampToValueAtTime(0.08, ctx.currentTime + 0.05)
    gain.gain.exponentialRampToValueAtTime(0.0001, ctx.currentTime + 0.6)
    osc.connect(gain)
    gain.connect(ctx.destination)
    osc.start()
    osc.stop(ctx.currentTime + 0.65)
    osc.onended = () => void ctx.close()
  } catch {
    /* audio no disponible */
  }
}

// ---------------------------------------------------------------------------
// Tarjeta base de sección
// ---------------------------------------------------------------------------

interface CardProps {
  title: string
  caption?: string
  index: number
  reduce: boolean
  children: React.ReactNode
  headerSlot?: React.ReactNode
  className?: string
}

function Card({ title, caption, index, reduce, children, headerSlot, className }: CardProps) {
  return (
    <motion.section
      initial={reduce ? false : { opacity: 0, y: 12 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3, ease: 'easeOut', delay: reduce ? 0 : 0.08 + index * 0.06 }}
      className={cn('h-full rounded-2xl border border-border bg-surface p-5', className)}
    >
      <div className="mb-4 flex items-start justify-between gap-3">
        <div>
          <h2 className="font-display text-h2 text-text-primary">{title}</h2>
          {caption && <p className="mt-0.5 text-caption text-text-muted">{caption}</p>}
        </div>
        {headerSlot}
      </div>
      {children}
    </motion.section>
  )
}

// ---------------------------------------------------------------------------
// Fila con switch
// ---------------------------------------------------------------------------

interface SwitchRowProps {
  icon?: LucideIcon
  label: string
  caption?: string
  checked: boolean
  onCheckedChange: (v: boolean) => void
  trailing?: React.ReactNode
  disabled?: boolean
}

function SwitchRow({ icon: Icon, label, caption, checked, onCheckedChange, trailing, disabled = false }: SwitchRowProps) {
  return (
    <div className="flex items-center justify-between gap-4 py-2.5">
      <div className="flex min-w-0 items-center gap-3">
        {Icon && (
          <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-elevated text-text-secondary">
            <Icon className="h-4 w-4" strokeWidth={1.75} />
          </span>
        )}
        <div className="min-w-0">
          <div className="text-sm font-medium text-text-primary">{label}</div>
          {caption && <div className="text-caption text-text-muted">{caption}</div>}
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {trailing}
        <Switch checked={checked} onCheckedChange={onCheckedChange} disabled={disabled} aria-label={label} />
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// ② Apariencia — previews de tema
// ---------------------------------------------------------------------------

function ThemePreview({ variant }: { variant: ThemeMode }) {
  // Preview pintado con los tokens reales (scope .light/.dark) mediante estilos
  // inline: así el color cambia con el scope del div (las clases bg-* de
  // Tailwind resuelven --color-* en :root y NO heredan el scope interno).
  // Prohibido hex duplicados: siempre via var(--token).
  const half = (
    <div
      className="flex h-full w-full flex-col gap-1 p-1.5"
      style={{ backgroundColor: 'rgb(var(--canvas))' }}
    >
      <div className="h-1.5 w-2/3 rounded-full" style={{ backgroundColor: 'rgb(var(--text-primary) / 0.25)' }} />
      <div className="flex flex-1 gap-1">
        <div className="w-1/3 rounded-sm" style={{ backgroundColor: 'rgb(var(--elevated))' }} />
        <div className="flex flex-1 flex-col gap-1">
          <div className="h-1/2 rounded-sm" style={{ backgroundColor: 'rgb(var(--accent))' }} />
          <div className="flex-1 rounded-sm" style={{ backgroundColor: 'rgb(var(--elevated))' }} />
        </div>
      </div>
    </div>
  )
  if (variant === 'dark') return <div className="dark h-full">{half}</div>
  if (variant === 'light') return <div className="light h-full">{half}</div>
  return (
    <div className="grid h-full grid-cols-2">
      <div className="dark">{half}</div>
      <div className="light">{half}</div>
    </div>
  )
}

const THEME_OPTIONS: { value: ThemeMode; labelKey: string; icon: LucideIcon }[] = [
  { value: 'dark', labelKey: 'settings.themeDark', icon: Moon },
  { value: 'light', labelKey: 'settings.themeLight', icon: Sun },
  { value: 'system', labelKey: 'settings.themeSystem', icon: MonitorSmartphone },
]

// ---------------------------------------------------------------------------
// ③ PWA — evento beforeinstallprompt
// ---------------------------------------------------------------------------

interface BeforeInstallPromptEvent extends Event {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

function Confetti({ burstKey, reduce }: { burstKey: number; reduce: boolean }) {
  const parts = useMemo(
    () =>
      Array.from({ length: 24 }, (_, i) => ({
        x: (Math.random() - 0.5) * 260,
        y: -(Math.random() * 140 + 40) + 240,
        r: Math.random() * 360,
        c: i % 2 === 0 ? '#22D3EE' : '#A78BFA',
        s: 4 + Math.random() * 4,
        d: Math.random() * 0.15,
      })),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [burstKey],
  )
  if (!burstKey || reduce) return null
  return (
    <div className="pointer-events-none absolute inset-x-0 top-10 z-20 flex justify-center overflow-visible" aria-hidden="true">
      {parts.map((p, i) => (
        <motion.span
          key={`${burstKey}-${i}`}
          initial={{ x: 0, y: 0, opacity: 1, scale: 1 }}
          animate={{ x: p.x, y: p.y, opacity: 0, rotate: p.r, scale: 0.6 }}
          transition={{ duration: 1.2, delay: p.d, ease: 'easeOut' }}
          style={{ backgroundColor: p.c, width: p.s, height: p.s }}
          className="absolute rounded-full"
        />
      ))}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Gestión de routers (modo live): CRUD contra /api/config/routers
// ---------------------------------------------------------------------------

type RouterType = 'glinet' | 'openwrt' | 'managed-switch' | 'external'

interface ConfigRouter {
  id: string
  name: string | null
  host: string
  type: RouterType
  is_gateway: boolean
  agent_only: boolean
  firmware_target: string
  snmp_enabled: boolean
  snmp_community: string
  snmp_port: number
  snmp_poll_interval: number
  ssh_port?: number
}

interface DiscoverCandidate {
  host: string
  hostname?: string
  isGateway: boolean
  authorized: boolean
  model: string | null
  configured: boolean
}

function RoutersManager({ reduce, onSaved }: { reduce: boolean; onSaved: () => void }) {
  const { t } = useTranslation()
  const { refresh } = useNetPulse()
  const [list, setList] = useState<ConfigRouter[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [host, setHost] = useState('')
  const [name, setName] = useState('')
  const [type, setType] = useState<RouterType>('openwrt')
  const [gateway, setGateway] = useState(false)
  const [agentOnly, setAgentOnly] = useState(false)
  const [addSshPort, setAddSshPort] = useState(22)
  const [submitting, setSubmitting] = useState(false)
  const [confirmDeleteFor, setConfirmDeleteFor] = useState<string | null>(null)
  const [editing, setEditing] = useState<ConfigRouter | null>(null)
  const [editHost, setEditHost] = useState('')
  const [editName, setEditName] = useState('')
  const [editType, setEditType] = useState<RouterType>('openwrt')
  const [editGateway, setEditGateway] = useState(false)
  const [editAgentOnly, setEditAgentOnly] = useState(false)
  const [editFirmwareTarget, setEditFirmwareTarget] = useState('')
  const [editSnmpEnabled, setEditSnmpEnabled] = useState(false)
  const [editSnmpCommunity, setEditSnmpCommunity] = useState('')
  const [editSnmpPort, setEditSnmpPort] = useState(161)
  const [editSnmpPollInterval, setEditSnmpPollInterval] = useState(60)
  const [editSshPort, setEditSshPort] = useState(22)
  const [editSubmitting, setEditSubmitting] = useState(false)
  const [pubkey, setPubkey] = useState<{ publicKey: string; fingerprint: string } | null>(null)
  const [copied, setCopied] = useState(false)
  const [copyFailed, setCopyFailed] = useState(false)
  const [rotating, setRotating] = useState(false)
  const [rotateConfirmOpen, setRotateConfirmOpen] = useState(false)
  const [rotateError, setRotateError] = useState<string | null>(null)
  const [scanning, setScanning] = useState(false)
  const [candidates, setCandidates] = useState<DiscoverCandidate[] | null>(null)
  const [showAddForm, setShowAddForm] = useState(false)

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/config/routers')
      if (res.status === 401) {
        window.location.assign('/login')
        return
      }
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const json = (await res.json()) as { routers: ConfigRouter[] }
      setList(json.routers)
      setError(null)
    } catch {
      setError(t('settings.routers.errorGeneric'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    let disposed = false
    void (async () => {
      try {
        const res = await fetch('/api/config/routers')
        if (res.status === 401) {
          window.location.assign('/login')
          return
        }
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const json = (await res.json()) as { routers: ConfigRouter[] }
        if (disposed) return
        setList(json.routers)
        setError(null)
      } catch {
        if (!disposed) setError(t('settings.routers.errorGeneric'))
      } finally {
        if (!disposed) setLoading(false)
      }
    })()
    return () => {
      disposed = true
    }
  }, [t])

  // Clave pública SSH del servidor (para autorizarla en los routers)
  useEffect(() => {
    let disposed = false
    void (async () => {
      try {
        const res = await fetch('/api/config/sshkey')
        if (!res.ok) return
        const json = (await res.json()) as { publicKey: string; fingerprint: string }
        if (!disposed) setPubkey(json)
      } catch {
        /* sin clave: se muestra el bloque vacío */
      }
    })()
    return () => {
      disposed = true
    }
  }, [])

  const copyKey = async () => {
    if (!pubkey) return
    // Helper con fallback para orígenes no seguros (http://<ip> de la LAN):
    // navigator.clipboard no existe ahí y el botón no copiaba nada (#466).
    const ok = await copyToClipboard(pubkey.publicKey)
    setCopyFailed(!ok)
    setCopied(ok)
    window.setTimeout(() => {
      setCopied(false)
      setCopyFailed(false)
    }, 1500)
  }

  const rotateKey = async () => {
    if (rotating) return
    setRotating(true)
    setRotateError(null)
    try {
      const res = await fetch('/api/config/sshkey/rotate', { method: 'POST' })
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { message?: string }
        throw new Error(body.message ?? `HTTP ${res.status}`)
      }
      const json = (await res.json()) as { publicKey: string; fingerprint: string }
      setPubkey({ publicKey: json.publicKey, fingerprint: json.fingerprint })
      setCopied(false)
      setRotateConfirmOpen(false)
    } catch (err) {
      setRotateError(err instanceof Error ? err.message : t('settings.routers.errorGeneric'))
    } finally {
      setRotating(false)
    }
  }

  const discover = async () => {
    if (scanning) return
    setScanning(true)
    setError(null)
    try {
      const res = await fetch('/api/config/discover?force=1')
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const json = (await res.json()) as { results: DiscoverCandidate[] }
      setCandidates(json.results)
    } catch {
      setError(t('settings.routers.errorGeneric'))
    } finally {
      setScanning(false)
    }
  }

  const addCandidate = async (cand: DiscoverCandidate) => {
    try {
      const host = cand.hostname?.trim() || cand.host
      const res = await fetch('/api/config/routers', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          host,
          name: cand.hostname?.trim() || undefined,
          type: /GL[.-]?iNet|GL-[A-Z]/i.test(cand.model || '') ? 'glinet' : 'openwrt',
          gateway: cand.isGateway,
        }),
      })
      if (!res.ok && res.status !== 409) throw new Error(`HTTP ${res.status}`)
      setCandidates((prev) =>
        prev?.map((x) => (x.host === cand.host ? { ...x, configured: true } : x)) ?? prev,
      )
      await load()
      refresh()
      onSaved()
    } catch {
      setError(t('settings.routers.errorGeneric'))
    }
  }

  const add = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!host.trim() || submitting) return
    setSubmitting(true)
    setError(null)
    try {
      const res = await fetch('/api/config/routers', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          host: host.trim(),
          name: name.trim() || undefined,
          type,
          gateway,
          agent_only: agentOnly,
          ssh_port: addSshPort,
        }),
      })
      if (res.status === 409) {
        setError(t('settings.routers.errorDuplicate'))
        return
      }
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setHost('')
      setName('')
      setType('openwrt')
      setGateway(false)
      setAgentOnly(false)
      setAddSshPort(22)
      setShowAddForm(false)
      await load()
      refresh()
      onSaved()
    } catch {
      setError(t('settings.routers.errorGeneric'))
    } finally {
      setSubmitting(false)
    }
  }

  const openEdit = (r: ConfigRouter) => {
    setEditing(r)
    setEditHost(r.host)
    setEditName(r.name ?? '')
    setEditType(r.type)
    setEditGateway(r.is_gateway)
    setEditAgentOnly(r.agent_only)
    setEditFirmwareTarget(r.firmware_target ?? '')
    setEditSnmpEnabled(r.snmp_enabled ?? false)
    setEditSnmpCommunity(r.snmp_community ?? '')
    setEditSnmpPort(r.snmp_port ?? 161)
    setEditSnmpPollInterval(r.snmp_poll_interval ?? 60)
    setEditSshPort(r.ssh_port ?? 22)
    setError(null)
  }

  const saveEdit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editing || editSubmitting) return
    setEditSubmitting(true)
    setError(null)
    try {
      const res = await fetch(`/api/config/routers/${encodeURIComponent(editing.id)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          host: editHost.trim(),
          name: editName.trim() || undefined,
          type: editType,
          gateway: editGateway,
          agent_only: editAgentOnly,
          firmware_target: editFirmwareTarget.trim(),
          snmp_enabled: editSnmpEnabled,
          snmp_community: editSnmpCommunity.trim() || undefined,
          snmp_port: editSnmpPort,
          snmp_poll_interval: editSnmpPollInterval,
          ssh_port: editSshPort,
        }),
      })
      if (res.status === 409) {
        setError(t('settings.routers.errorDuplicate'))
        return
      }
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setEditing(null)
      await load()
      refresh()
      onSaved()
    } catch {
      setError(t('settings.routers.errorGeneric'))
    } finally {
      setEditSubmitting(false)
    }
  }

  const remove = async (r: ConfigRouter) => {
    try {
      const res = await fetch(`/api/config/routers/${encodeURIComponent(r.id)}`, { method: 'DELETE' })
      if (!res.ok && res.status !== 204) throw new Error(`HTTP ${res.status}`)
      setConfirmDeleteFor(null)
      await load()
      refresh()
    } catch {
      setError(t('settings.routers.errorGeneric'))
    }
  }

  return (
    <Card
      title={t('settings.routers.title')}
      caption={t('settings.routers.caption')}
      index={4}
      reduce={reduce}
      headerSlot={
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => void discover()}
            disabled={scanning}
            className="flex items-center gap-1.5 rounded-lg border border-border bg-elevated px-3 py-2 text-sm font-medium text-text-secondary transition-colors duration-150 hover:border-accent/40 hover:text-accent disabled:opacity-50"
          >
            <Radar className={cn('h-4 w-4', scanning && 'animate-pulse')} strokeWidth={1.75} />
            {scanning ? t('settings.routers.discovering') : t('settings.routers.discover')}
          </button>
          <button
            type="button"
            aria-expanded={showAddForm}
            onClick={() => setShowAddForm((v) => !v)}
            className="flex items-center gap-1.5 rounded-lg border border-accent bg-accent-soft px-3 py-2 text-sm font-medium text-accent transition-colors duration-150 hover:brightness-105"
          >
            <Plus className="h-4 w-4" strokeWidth={1.75} />
            {t('settings.routers.addDevice')}
          </button>
        </div>
      }
    >
      {/* Lista configurada (tabla como en el mockup: IP | Nombre | Rol | Acciones) */}
      {loading ? (
        <p className="text-caption text-text-muted">…</p>
      ) : list.length === 0 ? (
        <p className="rounded-xl bg-elevated px-3.5 py-2.5 text-caption leading-relaxed text-text-muted">
          {t('settings.routers.empty')}
        </p>
      ) : (
        <div className="overflow-x-auto rounded-xl border border-border">
          <table className="w-full border-collapse text-left text-[13px]">
            <thead>
              <tr className="border-b border-border">
                <th className="px-3.5 py-2.5 text-[10px] font-semibold uppercase tracking-wider text-text-muted">IP</th>
                <th className="px-3.5 py-2.5 text-[10px] font-semibold uppercase tracking-wider text-text-muted">{t('settings.routers.name')}</th>
                <th className="px-3.5 py-2.5 text-[10px] font-semibold uppercase tracking-wider text-text-muted">{t('settings.routers.role')}</th>
                <th className="px-3.5 py-2.5 text-right text-[10px] font-semibold uppercase tracking-wider text-text-muted">{t('settings.routers.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {list.map((r) => (
                <tr key={r.id} className="border-b border-border/60 transition-colors last:border-0 hover:bg-hover/40">
                  <td className="px-3.5 py-2.5 font-mono text-[12px] font-medium text-text-primary">{r.host}</td>
                  <td className="px-3.5 py-2.5 text-text-secondary">{r.name && r.name !== r.host ? r.name : '—'}</td>
                  <td className="px-3.5 py-2.5">
                    {r.is_gateway ? (
                      <span className="rounded bg-accent-soft px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-accent">
                        {t('settings.routers.gatewayBadge')}
                      </span>
                    ) : r.agent_only ? (
                      <span className="rounded bg-elevated px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-text-muted ring-1 ring-inset ring-border">
                        {t('settings.routers.agentOnlyBadge')}
                      </span>
                    ) : (
                      <span className="rounded bg-elevated px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-text-muted ring-1 ring-inset ring-border">
                        {t('settings.routers.typeManaged')}
                      </span>
                    )}
                  </td>
                  <td className="px-3.5 py-2.5">
                    {confirmDeleteFor === r.id ? (
                      <span className="flex items-center justify-end gap-1.5">
                        <button
                          type="button"
                          onClick={() => void remove(r)}
                          className="rounded-lg bg-danger px-2.5 py-1 text-[11px] font-semibold text-canvas transition-opacity hover:opacity-90"
                        >
                          {t('settings.users.confirmDelete')}
                        </button>
                        <button
                          type="button"
                          onClick={() => setConfirmDeleteFor(null)}
                          className="rounded-lg border border-border px-2.5 py-1 text-[11px] font-medium text-text-secondary transition-colors hover:text-text-primary"
                        >
                          {t('settings.users.cancel')}
                        </button>
                      </span>
                    ) : (
                      <span className="flex items-center justify-end gap-1.5">
                        <button
                          type="button"
                          onClick={() => openEdit(r)}
                          aria-label={t('settings.routers.edit')}
                          className="flex h-7 w-7 items-center justify-center rounded-lg border border-border text-text-muted transition-colors duration-150 hover:border-accent/40 hover:text-accent"
                        >
                          <Pencil className="h-3.5 w-3.5" strokeWidth={1.75} />
                        </button>
                        <button
                          type="button"
                          onClick={() => setConfirmDeleteFor(r.id)}
                          aria-label={t('settings.routers.delete')}
                          className="flex h-7 w-7 items-center justify-center rounded-lg border border-border text-text-muted transition-colors duration-150 hover:border-danger/40 hover:text-danger"
                        >
                          <Trash2 className="h-4 w-4" strokeWidth={1.75} />
                        </button>
                      </span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Panel de descubrimiento (candidatos) y alta manual (issue #144) */}
      <div className="mt-4 border-t border-border pt-4">
        <p className="text-caption text-text-muted">{t('settings.routers.discoverCaption')}</p>
        {candidates !== null && (
          <ul className="mt-3 flex flex-col gap-2">
            {candidates.length === 0 && (
              <li className="rounded-xl bg-elevated px-3.5 py-2.5 text-caption text-text-muted">
                {t('settings.routers.discoverNone')}
              </li>
            )}
            {candidates.map((cand) => (
              <li
                key={cand.host}
                className="flex items-center gap-3 rounded-xl border border-border bg-elevated px-3.5 py-2.5"
              >
                <RouterIcon className="h-4 w-4 shrink-0 text-text-muted" strokeWidth={1.75} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-medium text-text-primary">
                      {cand.model || cand.hostname || cand.host}
                    </span>
                    {cand.isGateway && (
                      <span className="rounded-full bg-accent-soft px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-accent">
                        {t('settings.routers.gatewayBadge')}
                      </span>
                    )}
                  </div>
                  <div className="font-mono text-caption text-text-muted">{cand.hostname ? `${cand.host} (${cand.hostname})` : cand.host}</div>
                </div>
                {cand.configured ? (
                  <span className="shrink-0 text-caption text-text-muted">{t('settings.routers.alreadyAdded')}</span>
                ) : cand.authorized ? (
                  <button
                    type="button"
                    onClick={() => void addCandidate(cand)}
                    className="flex shrink-0 items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-canvas transition-opacity hover:opacity-90"
                  >
                    <Plus className="h-3.5 w-3.5" strokeWidth={2} />
                    {t('settings.routers.add')}
                  </button>
                ) : (
                  <span className="flex shrink-0 items-center gap-1.5 rounded-full bg-warn/10 px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wider text-warn">
                    <KeyRound className="h-3 w-3" strokeWidth={2} />
                    {t('settings.routers.needsKey')}
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}

        {showAddForm && (
        <form onSubmit={(e) => void add(e)} className="mt-3">
        <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
          <input
            type="text"
            required
            value={host}
            onChange={(e) => setHost(e.target.value)}
            placeholder={t('settings.routers.host')}
            aria-label={t('settings.routers.host')}
            className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('settings.routers.name')}
            aria-label={t('settings.routers.name')}
            className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
          <input
            type="text"
            min={1}
            max={65535}
            value={addSshPort}
            onChange={(e) => setAddSshPort(Number(e.target.value))}
            placeholder={t('settings.routers.sshPort')}
            aria-label={t('settings.routers.sshPort')}
            title={t('settings.routers.sshPortHint')}
            className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
        </div>
        <div className="mt-2.5 flex flex-wrap items-center gap-3">
          <SegmentedControl
            options={[
              { value: 'openwrt', label: 'OpenWrt' },
              { value: 'glinet', label: 'GL.iNet' },
              { value: 'managed-switch', label: t('settings.routers.typeManaged') },
              { value: 'external', label: t('settings.routers.typeExternal') },
            ]}
            value={type}
            onChange={(v) => setType(v as RouterType)}
            ariaLabel={t('settings.routers.type')}
          />
          <label className="flex cursor-pointer items-center gap-2 text-sm text-text-secondary">
            <Switch checked={gateway} onCheckedChange={setGateway} />
            {t('settings.routers.gateway')}
          </label>
          <label className="flex cursor-pointer items-center gap-2 text-sm text-text-secondary">
            <Switch checked={agentOnly} onCheckedChange={setAgentOnly} />
            {t('settings.routers.agentOnly')}
          </label>
          <button
            type="submit"
            disabled={submitting || !host.trim()}
            className="ml-auto flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-canvas transition-opacity duration-150 hover:opacity-90 disabled:opacity-40"
          >
            <Plus className="h-4 w-4" strokeWidth={2} />
            {submitting ? t('settings.routers.adding') : t('settings.routers.add')}
          </button>
        </div>
        {error && <p className="mt-2 text-caption text-danger">{error}</p>}
        <p className="mt-3 text-caption leading-relaxed text-text-muted">{t('settings.routers.hint')}</p>
        </form>
        )}
      </div>

      {/* Edición en popup (issue #144): el form de edición ya no se cuela al
          fondo de la tarjeta; abre un diálogo centrado. */}
      <Dialog open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
        {editing && (
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <Pencil className="h-4 w-4 text-accent" strokeWidth={1.75} />
                {t('settings.routers.editTitle', { id: editing.id })}
              </DialogTitle>
              <DialogDescription>{t('settings.routers.editHint')}</DialogDescription>
            </DialogHeader>
            <form onSubmit={(e) => void saveEdit(e)} className="space-y-3">
              <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
                <input
                  type="text"
                  required
                  value={editHost}
                  onChange={(e) => setEditHost(e.target.value)}
                  placeholder={t('settings.routers.host')}
                  aria-label={t('settings.routers.host')}
                  className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                />
                <input
                  type="text"
                  value={editName}
                  onChange={(e) => setEditName(e.target.value)}
                  placeholder={t('settings.routers.name')}
                  aria-label={t('settings.routers.name')}
                  className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                />
              </div>
              <div>
                <label htmlFor="ssh-port" className="mb-1 block text-caption font-medium uppercase tracking-[0.06em] text-text-muted">
                  {t('settings.routers.sshPort')}
                </label>
                <input
                  id="ssh-port"
                  type="text"
                  min={1}
                  max={65535}
                  value={editSshPort}
                  onChange={(e) => setEditSshPort(Number(e.target.value))}
                  aria-label={t('settings.routers.sshPort')}
                  title={t('settings.routers.sshPortHint')}
                  className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                />
              </div>
              <div className="flex flex-wrap items-center gap-3">
                <SegmentedControl
                  options={[
                    { value: 'openwrt', label: 'OpenWrt' },
                    { value: 'glinet', label: 'GL.iNet' },
                    { value: 'managed-switch', label: t('settings.routers.typeManaged') },
                    { value: 'external', label: t('settings.routers.typeExternal') },
                  ]}
                  value={editType}
                  onChange={(v) => setEditType(v as RouterType)}
                  ariaLabel={t('settings.routers.type')}
                />
                <label className="flex cursor-pointer items-center gap-2 text-sm text-text-secondary">
                  <Switch checked={editGateway} onCheckedChange={setEditGateway} />
                  {t('settings.routers.gateway')}
                </label>
                <label className="flex cursor-pointer items-center gap-2 text-sm text-text-secondary">
                  <Switch checked={editAgentOnly} onCheckedChange={setEditAgentOnly} />
                  {t('settings.routers.agentOnly')}
                </label>
              </div>
              <div>
                <label htmlFor="firmware-target" className="mb-1 block text-caption font-medium uppercase tracking-[0.06em] text-text-muted">
                  {t('settings.routers.firmwareTarget')}
                </label>
                <input
                  id="firmware-target"
                  type="text"
                  value={editFirmwareTarget}
                  onChange={(e) => setEditFirmwareTarget(e.target.value)}
                  placeholder={t('settings.routers.firmwareTargetPlaceholder')}
                  aria-label={t('settings.routers.firmwareTarget')}
                  className="w-full rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                />
                <p className="mt-1 text-caption leading-relaxed text-text-muted">{t('settings.routers.firmwareTargetHint')}</p>
              </div>
              {(editType === 'managed-switch' || editType === 'external' || editSnmpEnabled) && (
                <div className="space-y-2.5 rounded-lg border border-border bg-canvas/50 p-3">
                  <label className="flex cursor-pointer items-center gap-2 text-sm text-text-secondary">
                    <Switch checked={editSnmpEnabled} onCheckedChange={setEditSnmpEnabled} />
                    {t('settings.routers.snmpEnabled')}
                  </label>
                  {editSnmpEnabled && (
                    <div className="grid grid-cols-2 gap-2.5">
                      <div>
                        <label htmlFor="snmp-community" className="mb-1 block text-caption font-medium uppercase tracking-[0.06em] text-text-muted">
                          {t('settings.routers.snmpCommunity')}
                        </label>
                        <input
                          id="snmp-community"
                          type="text"
                          value={editSnmpCommunity}
                          onChange={(e) => setEditSnmpCommunity(e.target.value)}
                          placeholder="public"
                          aria-label={t('settings.routers.snmpCommunity')}
                          className="w-full rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                        />
                      </div>
                        <div>
                          <label htmlFor="snmp-port" className="mb-1 block text-caption font-medium uppercase tracking-[0.06em] text-text-muted">
                            {t('settings.routers.snmpPort')}
                          </label>
                          <input
                            id="snmp-port"
                            type="text"
                            min={1}
                            max={65535}
                            value={editSnmpPort}
                            onChange={(e) => setEditSnmpPort(Number(e.target.value))}
                            aria-label={t('settings.routers.snmpPort')}
                            className="w-full rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                          />
                        </div>
                        <div>
                          <label htmlFor="snmp-poll-interval" className="mb-1 block text-caption font-medium uppercase tracking-[0.06em] text-text-muted">
                            {t('settings.routers.snmpPollInterval')}
                          </label>
                          <input
                            id="snmp-poll-interval"
                            type="text"
                            min={10}
                            max={3600}
                            value={editSnmpPollInterval}
                            onChange={(e) => setEditSnmpPollInterval(Number(e.target.value))}
                            aria-label={t('settings.routers.snmpPollInterval')}
                            className="w-full rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                          />
                        </div>
                    </div>
                  )}
                </div>
              )}
              {error && <p className="text-caption text-danger">{error}</p>}
              <div className="flex justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setEditing(null)}
                  className="rounded-lg border border-border px-3 py-2 text-sm font-medium text-text-secondary transition-colors duration-150 hover:text-text-primary"
                >
                  {t('settings.users.cancel')}
                </button>
                <button
                  type="submit"
                  disabled={editSubmitting || !editHost.trim()}
                  className="flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-canvas transition-opacity duration-150 hover:opacity-90 disabled:opacity-40"
                >
                  <Pencil className="h-4 w-4" strokeWidth={2} />
                  {editSubmitting ? t('settings.routers.saving') : t('settings.routers.save')}
                </button>
              </div>
            </form>
          </DialogContent>
        )}
      </Dialog>

      {/* Clave pública SSH del servidor */}
      <div className="mt-4 border-t border-border pt-4">
        <div className="flex items-center gap-2">
          <KeyRound className="h-4 w-4 text-text-muted" strokeWidth={1.75} />
          <span className="text-sm font-medium text-text-primary">{t('settings.routers.sshKeyTitle')}</span>
        </div>
        <p className="mt-1 text-caption leading-relaxed text-text-muted">{t('settings.routers.sshKeyCaption')}</p>
        {pubkey && (
          <div className="mt-2.5 flex items-start gap-2">
            <code className="min-w-0 flex-1 break-all rounded-lg bg-canvas px-3 py-2 font-mono text-[11px] leading-relaxed text-text-secondary">
              {pubkey.publicKey}
            </code>
            <button
              type="button"
              onClick={() => void copyKey()}
              className="flex shrink-0 items-center gap-1.5 rounded-lg border border-border bg-elevated px-3 py-2 text-xs font-medium text-text-secondary transition-colors duration-150 hover:border-accent/40 hover:text-accent"
            >
              {copied ? <Check className="h-3.5 w-3.5 text-ok" strokeWidth={2} /> : <Copy className="h-3.5 w-3.5" strokeWidth={1.75} />}
              {copied ? t('settings.routers.copied') : copyFailed ? t('settings.routers.copyFailed') : t('settings.routers.copy')}
            </button>
            <button
              type="button"
              onClick={() => {
                setRotateError(null)
                setRotateConfirmOpen(true)
              }}
              className="flex shrink-0 items-center gap-1.5 rounded-lg border border-border bg-elevated px-3 py-2 text-xs font-medium text-text-secondary transition-colors duration-150 hover:border-red-500/40 hover:text-red-500"
            >
              <RotateCw className="h-3.5 w-3.5" strokeWidth={1.75} />
              {t('settings.routers.rotateKey')}
            </button>
          </div>
        )}
      </div>

      <AlertDialog open={rotateConfirmOpen} onOpenChange={setRotateConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('settings.routers.rotateKeyTitle')}</AlertDialogTitle>
            <AlertDialogDescription>{t('settings.routers.rotateKeyCaption')}</AlertDialogDescription>
          </AlertDialogHeader>
          {rotateError && <p className="text-sm text-red-500">{rotateError}</p>}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={rotating}>{t('settings.users.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault()
                void rotateKey()
              }}
              disabled={rotating}
              className="bg-red-600 text-white hover:bg-red-700"
            >
              <RotateCw className="mr-1.5 h-4 w-4" strokeWidth={2} />
              {rotating ? t('settings.routers.rotating') : t('settings.routers.rotateKeyConfirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}


// ---------------------------------------------------------------------------
// Gestión de usuarios (solo admin): CRUD contra /api/users
// ---------------------------------------------------------------------------

interface ManagedUser {
  id: number
  username: string
  role: 'admin' | 'user'
}

function UsersManager({ reduce, onSaved }: { reduce: boolean; onSaved: () => void }) {
  const { t } = useTranslation()
  const auth = useAuth()
  const [list, setList] = useState<ManagedUser[]>([])
  const [error, setError] = useState<string | null>(null)
  const [editingPassFor, setEditingPassFor] = useState<number | null>(null)
  const [passDraft, setPassDraft] = useState('')
  const [confirmDeleteFor, setConfirmDeleteFor] = useState<number | null>(null)
  const [newUser, setNewUser] = useState('')
  const [newPass, setNewPass] = useState('')
  const [newAdmin, setNewAdmin] = useState(false)
  const [showCreateForm, setShowCreateForm] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/users')
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const json = (await res.json()) as { users: ManagedUser[] }
      setList(json.users)
      setError(null)
    } catch {
      setError(t('settings.users.errorGeneric'))
    }
  }, [t])

  useEffect(() => {
    let disposed = false
    void (async () => {
      try {
        const res = await fetch('/api/users')
        if (!res.ok) return
        const json = (await res.json()) as { users: ManagedUser[] }
        if (!disposed) setList(json.users)
      } catch {
        /* sin permisos o sin backend: la tarjeta no se muestra */
      }
    })()
    return () => {
      disposed = true
    }
  }, [])

  const add = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!newUser.trim() || !newPass || submitting) return
    setSubmitting(true)
    setError(null)
    try {
      const res = await fetch('/api/users', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: newUser.trim(), password: newPass, role: newAdmin ? 'admin' : 'user' }),
      })
      if (res.status === 409) {
        setError(t('settings.users.errorDuplicate'))
        return
      }
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setNewUser('')
      setNewPass('')
      setNewAdmin(false)
      setShowCreateForm(false)
      await load()
      onSaved()
    } catch {
      setError(t('settings.users.errorGeneric'))
    } finally {
      setSubmitting(false)
    }
  }

  const remove = async (u: ManagedUser) => {
    try {
      const res = await fetch(`/api/users/${u.id}`, { method: 'DELETE' })
      if (!res.ok && res.status !== 204) {
        const body = (await res.json().catch(() => null)) as { message?: string } | null
        throw new Error(body?.message ?? `HTTP ${res.status}`)
      }
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('settings.users.errorGeneric'))
    }
  }

  const changeRole = async (u: ManagedUser, admin: boolean) => {
    try {
      const res = await fetch(`/api/users/${u.id}/role?role=${admin ? 'admin' : 'user'}`, { method: 'PUT' })
      if (!res.ok && res.status !== 204) {
        const body = (await res.json().catch(() => null)) as { message?: string } | null
        throw new Error(body?.message ?? `HTTP ${res.status}`)
      }
      await load()
      onSaved()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('settings.users.errorGeneric'))
    }
  }

  const changePassword = async (u: ManagedUser) => {
    if (passDraft.length < 6) return
    try {
      const res = await fetch(`/api/users/${u.id}/password`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: passDraft }),
      })
      if (!res.ok && res.status !== 204) {
        const body = (await res.json().catch(() => null)) as { message?: string } | null
        throw new Error(body?.message ?? `HTTP ${res.status}`)
      }
      setEditingPassFor(null)
      setPassDraft('')
      onSaved()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('settings.users.errorGeneric'))
    }
  }

  return (
    <Card title={t('settings.users.title')} caption={t('settings.users.caption')} index={5} reduce={reduce}>
      <ul className="flex flex-col gap-2">
        {list.map((u) => (
          <li key={u.id} className="rounded-xl border border-border bg-elevated px-3.5 py-2.5">
            <div className="flex items-center gap-3">
              <UserCog className="h-4 w-4 shrink-0 text-text-muted" strokeWidth={1.75} />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium text-text-primary">{u.username}</span>
                  {auth?.user === u.username && (
                    <span className="rounded-full bg-ok/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-ok">
                      {t('settings.users.you')}
                    </span>
                  )}
                </div>
              </div>
              <label className="flex shrink-0 cursor-pointer items-center gap-2 text-caption text-text-secondary" title={t('settings.users.roleAdmin')}>
                {t('settings.users.roleAdmin')}
                <Switch
                  checked={u.role === 'admin'}
                  onCheckedChange={(v) => void changeRole(u, v)}
                  disabled={auth?.user === u.username}
                />
              </label>
              <button
                type="button"
                onClick={() => {
                  setEditingPassFor(editingPassFor === u.id ? null : u.id)
                  setPassDraft('')
                  setConfirmDeleteFor(null)
                }}
                aria-label={t('settings.users.changePassword')}
                title={t('settings.users.changePassword')}
                className={cn(
                  'flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border transition-colors duration-150',
                  editingPassFor === u.id ? 'border-accent/50 text-accent' : 'border-border text-text-muted hover:border-accent/40 hover:text-accent',
                )}
              >
                <KeyRound className="h-4 w-4" strokeWidth={1.75} />
              </button>
              {auth?.user !== u.username &&
                (confirmDeleteFor === u.id ? (
                  <span className="flex shrink-0 items-center gap-1.5">
                    <button
                      type="button"
                      onClick={() => void remove(u)}
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
                  <button
                    type="button"
                    onClick={() => {
                      setConfirmDeleteFor(u.id)
                      setEditingPassFor(null)
                    }}
                    aria-label={t('settings.users.delete')}
                    className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-border text-text-muted transition-colors duration-150 hover:border-danger/40 hover:text-danger"
                  >
                    <Trash2 className="h-4 w-4" strokeWidth={1.75} />
                  </button>
                ))}
            </div>
            {editingPassFor === u.id && (
              <form
                className="mt-2.5 flex items-center gap-2 border-t border-border pt-2.5"
                onSubmit={(e) => {
                  e.preventDefault()
                  void changePassword(u)
                }}
              >
                <input
                  type="password"
                  required
                  minLength={6}
                  value={passDraft}
                  onChange={(e) => setPassDraft(e.target.value)}
                  placeholder={t('settings.users.newPassword')}
                  aria-label={t('settings.users.newPassword')}
                  autoComplete="new-password"
                  className="min-w-0 flex-1 rounded-lg border border-border bg-elevated px-3 py-1.5 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                />
                <button
                  type="submit"
                  disabled={passDraft.length < 6}
                  className="shrink-0 rounded-lg bg-accent px-3 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
                >
                  {t('settings.users.savePassword')}
                </button>
              </form>
            )}
          </li>
        ))}
      </ul>

      {/* Alta de usuario colapsada tras botón (SPEC-65 D65-7b) */}
      <div className="mt-4 border-t border-border pt-4">
        <button
          type="button"
          aria-expanded={showCreateForm}
          onClick={() => setShowCreateForm((v) => !v)}
          className="flex items-center gap-2 rounded-lg border border-border bg-elevated px-3.5 py-2 text-sm font-medium text-text-secondary transition-colors duration-150 hover:border-accent/40 hover:text-accent"
        >
          <Plus className="h-4 w-4" strokeWidth={1.75} />
          {t('settings.users.createUser')}
        </button>
        {showCreateForm && (
        <form onSubmit={(e) => void add(e)} className="mt-3">
        <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
          <input
            type="text"
            required
            value={newUser}
            onChange={(e) => setNewUser(e.target.value)}
            placeholder={t('settings.users.username')}
            aria-label={t('settings.users.username')}
            className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
          <input
            type="password"
            required
            minLength={6}
            value={newPass}
            onChange={(e) => setNewPass(e.target.value)}
            placeholder={t('settings.users.password')}
            aria-label={t('settings.users.password')}
            autoComplete="new-password"
            className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
        </div>
        <div className="mt-2.5 flex flex-wrap items-center gap-3">
          <label className="flex cursor-pointer items-center gap-2 text-sm text-text-secondary">
            <Switch checked={newAdmin} onCheckedChange={setNewAdmin} />
            {t('settings.users.roleAdmin')}
          </label>
          <button
            type="submit"
            disabled={submitting || !newUser.trim() || newPass.length < 6}
            className="ml-auto flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-canvas transition-opacity duration-150 hover:opacity-90 disabled:opacity-40"
          >
            <Plus className="h-4 w-4" strokeWidth={2} />
            {submitting ? t('settings.users.adding') : t('settings.users.add')}
          </button>
        </div>
        {error && <p className="mt-2 text-caption text-danger">{error}</p>}
        </form>
        )}
      </div>
    </Card>
  )
}


// ---------------------------------------------------------------------------
// AdGuard Home: GL.iNet on-router o estándar remoto (kv servidor)
// ---------------------------------------------------------------------------

function AdGuardManager({ reduce, onSaved }: { reduce: boolean; onSaved: () => void }) {
  const { t } = useTranslation()
  const { routers } = useNetPulse()
  const gwIp = routers.find((r) => r.roleBadge === 'Principal')?.ip ?? routers[0]?.ip ?? ''
  const [mode, setMode] = useState<'glinet' | 'standard'>('glinet')
  const [host, setHost] = useState('')
  const [port, setPort] = useState<string>('3000')
  const [user, setUser] = useState('root')
  const [password, setPassword] = useState('')
  const [passSet, setPassSet] = useState(false)
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let disposed = false
    void (async () => {
      try {
        const res = await fetch('/api/config/adguard')
        if (!res.ok) return
        const json = (await res.json()) as {
          mode: 'glinet' | 'standard'
          host: string
          port: number
          user: string
          passSet: boolean
        }
        if (disposed) return
        setMode(json.mode || 'glinet')
        setHost(json.host || (json.mode === 'glinet' ? gwIp : ''))
        setPort(json.port ? String(json.port) : '3000')
        setUser(json.user || 'root')
        setPassSet(json.passSet)
      } catch {
        if (!disposed && gwIp) setHost((h) => h || gwIp)
      }
    })()
    return () => {
      disposed = true
    }
  }, [gwIp])

  const displayHost = mode === 'glinet' ? host : `${host}:${port}`

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!host.trim() || saving) return
    const portNum = parseInt(port, 10)
    if (mode === 'standard' && (Number.isNaN(portNum) || portNum < 1 || portNum > 65535)) {
      setError(t('settings.adguard.errorGeneric'))
      return
    }
    setSaving(true)
    setError(null)
    try {
      const payload: Record<string, unknown> = {
        mode,
        host: host.trim(),
        user: user.trim() || 'root',
        password: password || undefined,
      }
      if (mode === 'standard') {
        payload.port = portNum
      }
      const res = await fetch('/api/config/adguard', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
      if (!res.ok && res.status !== 204) throw new Error(`HTTP ${res.status}`)
      setPassSet(true)
      setPassword('')
      setEditing(false)
      onSaved()
    } catch {
      setError(t('settings.adguard.errorGeneric'))
    } finally {
      setSaving(false)
    }
  }

  // SPEC-65 D65-7c: configurado y sin editar → vista compacta (icono + host +
  // chip ok + Editar). Sin configurar → form directo como antes.
  if (passSet && !editing) {
    return (
      <Card title={t('settings.adguard.title')} caption={t('settings.adguard.caption')} index={5} reduce={reduce}>
        <div className="flex items-center gap-3 rounded-xl border border-border bg-elevated px-3.5 py-2.5">
          <ShieldCheck className="h-4 w-4 shrink-0 text-text-muted" strokeWidth={1.75} />
          <span className="min-w-0 flex-1 truncate font-mono text-sm font-medium text-text-primary">{displayHost}</span>
          <span className="flex shrink-0 items-center gap-1.5 rounded-full bg-ok/10 px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wider text-ok">
            {t('settings.adguard.configured')}
          </span>
          <button
            type="button"
            onClick={() => setEditing(true)}
            aria-label={t('settings.adguard.edit')}
            title={t('settings.adguard.edit')}
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-border text-text-muted transition-colors duration-150 hover:border-accent/40 hover:text-accent"
          >
            <Pencil className="h-4 w-4" strokeWidth={1.75} />
          </button>
        </div>
        <p className="mt-3 text-caption leading-relaxed text-text-muted">{t('settings.adguard.hint')}</p>
      </Card>
    )
  }

  return (
    <Card title={t('settings.adguard.title')} caption={t('settings.adguard.caption')} index={5} reduce={reduce}>
      <form onSubmit={(e) => void save(e)}>
        <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-4">
          <select
            value={mode}
            onChange={(e) => setMode(e.target.value as 'glinet' | 'standard')}
            aria-label={t('settings.adguard.mode')}
            className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none"
          >
            <option value="glinet">{t('settings.adguard.modeGlinet')}</option>
            <option value="standard">{t('settings.adguard.modeStandard')}</option>
          </select>
          <input
            type="text"
            required
            value={host}
            onChange={(e) => setHost(e.target.value)}
            placeholder={mode === 'glinet' ? t('settings.adguard.hostGlinet') : t('settings.adguard.hostStandard')}
            aria-label={t('settings.adguard.host')}
            className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
          {mode === 'standard' && (
            <input
              type="text"
              min={1}
              max={65535}
              required
              value={port}
              onChange={(e) => setPort(e.target.value)}
              placeholder={t('settings.adguard.port')}
              aria-label={t('settings.adguard.port')}
              className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
            />
          )}
          <input
            type="text"
            value={user}
            onChange={(e) => setUser(e.target.value)}
            placeholder={mode === 'glinet' ? t('settings.adguard.userGlinet') : t('settings.adguard.userStandard')}
            aria-label={t('settings.adguard.user')}
            className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={passSet ? t('settings.adguard.passKeep') : t('settings.adguard.pass')}
            aria-label={t('settings.adguard.pass')}
            autoComplete="new-password"
            className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
        </div>
        <div className="mt-2.5 flex flex-wrap items-center gap-3">
          <span className={cn('flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wider', passSet ? 'bg-ok/10 text-ok' : 'bg-warn/10 text-warn')}>
            {passSet ? t('settings.adguard.configured') : t('settings.adguard.notConfigured')}
          </span>
          <button
            type="submit"
            disabled={saving || !host.trim() || (mode === 'standard' && (!port || Number.isNaN(parseInt(port, 10)))) || (!passSet && !password)}
            className="ml-auto flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-canvas transition-opacity duration-150 hover:opacity-90 disabled:opacity-40"
          >
            {saving ? t('settings.adguard.saving') : t('settings.adguard.save')}
          </button>
          {passSet && editing && (
            <button
              type="button"
              onClick={() => {
                setEditing(false)
                setPassword('')
                setError(null)
              }}
              className="rounded-lg border border-border px-3 py-2 text-sm font-medium text-text-secondary transition-colors duration-150 hover:text-text-primary"
            >
              {t('settings.users.cancel')}
            </button>
          )}
        </div>
        {error && <p className="mt-2 text-caption text-danger">{error}</p>}
        <p className="mt-3 text-caption leading-relaxed text-text-muted">{t('settings.adguard.hint')}</p>
      </form>
    </Card>
  )
}

// Proxmox VE (#561): inventario read-only del cluster para sellar
// hypervisor/ct en Dispositivos. Config en kv del servidor (url + API token).
function ProxmoxManager({ reduce, onSaved }: { reduce: boolean; onSaved: () => void }) {
  const { t } = useTranslation()
  const [url, setURL] = useState('')
  const [tokenId, setTokenId] = useState('')
  const [secret, setSecret] = useState('')
  const [tokenSet, setTokenSet] = useState(false)
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let disposed = false
    void (async () => {
      try {
        const res = await fetch('/api/config/proxmox')
        if (!res.ok) return
        const json = (await res.json()) as { url: string; tokenId: string; tokenSet: boolean }
        if (disposed) return
        setURL(json.url || '')
        setTokenId(json.tokenId || '')
        setTokenSet(json.tokenSet)
      } catch {
        // sin servidor → no-op
      }
    })()
    return () => {
      disposed = true
    }
  }, [])

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (saving) return
    setSaving(true)
    setError(null)
    try {
      const payload: Record<string, unknown> = {}
      // url siempre se manda ("" = desactivar; si hay url previa y no se tocó,
      // se conserva igualmente porque el input está precargado).
      payload.url = url.trim()
      if (tokenId.trim()) payload.tokenId = tokenId.trim()
      if (secret) payload.secret = secret
      const res = await fetch('/api/config/proxmox', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
      if (!res.ok && res.status !== 204) throw new Error(`HTTP ${res.status}`)
      if (secret) {
        setTokenSet(true)
        setSecret('')
      }
      setEditing(false)
      onSaved()
    } catch {
      setError(t('settings.proxmox.errorGeneric'))
    } finally {
      setSaving(false)
    }
  }

  const disable = async () => {
    if (saving) return
    setSaving(true)
    setError(null)
    try {
      const res = await fetch('/api/config/proxmox', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url: '' }),
      })
      if (!res.ok && res.status !== 204) throw new Error(`HTTP ${res.status}`)
      setURL('')
      setTokenId('')
      setTokenSet(false)
      setEditing(false)
      onSaved()
    } catch {
      setError(t('settings.proxmox.errorGeneric'))
    } finally {
      setSaving(false)
    }
  }

  if (tokenSet && !editing) {
    return (
      <Card title={t('settings.proxmox.title')} caption={t('settings.proxmox.caption')} index={5} reduce={reduce}>
        <div className="flex items-center gap-3 rounded-xl border border-border bg-elevated px-3.5 py-2.5">
          <Server className="h-4 w-4 shrink-0 text-text-muted" strokeWidth={1.75} />
          <span className="min-w-0 flex-1 truncate font-mono text-sm font-medium text-text-primary">{url}</span>
          <span className="flex shrink-0 items-center gap-1.5 rounded-full bg-ok/10 px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wider text-ok">
            {t('settings.proxmox.configured')}
          </span>
          <button
            type="button"
            onClick={() => setEditing(true)}
            aria-label={t('settings.proxmox.edit')}
            title={t('settings.proxmox.edit')}
            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-border text-text-muted transition-colors duration-150 hover:border-accent/40 hover:text-accent"
          >
            <Pencil className="h-4 w-4" strokeWidth={1.75} />
          </button>
        </div>
        <p className="mt-3 text-caption leading-relaxed text-text-muted">{t('settings.proxmox.hint')}</p>
      </Card>
    )
  }

  return (
    <Card title={t('settings.proxmox.title')} caption={t('settings.proxmox.caption')} index={5} reduce={reduce}>
      <form onSubmit={(e) => void save(e)}>
        <div className="grid grid-cols-1 gap-2.5">
          <input
            type="text"
            value={url}
            onChange={(e) => setURL(e.target.value)}
            placeholder="https://192.168.1.100:8006"
            aria-label={t('settings.proxmox.url')}
            className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
          <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
            <input
              type="text"
              value={tokenId}
              onChange={(e) => setTokenId(e.target.value)}
              placeholder="root@pam!netpulse"
              aria-label={t('settings.proxmox.tokenId')}
              className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
            />
            <input
              type="password"
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              placeholder={tokenSet ? t('settings.proxmox.secretKeep') : t('settings.proxmox.secret')}
              aria-label={t('settings.proxmox.secret')}
              autoComplete="new-password"
              className="rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
            />
          </div>
        </div>
        <div className="mt-2.5 flex flex-wrap items-center gap-3">
          <span className={cn('flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wider', tokenSet ? 'bg-ok/10 text-ok' : 'bg-warn/10 text-warn')}>
            {tokenSet ? t('settings.proxmox.configured') : t('settings.proxmox.notConfigured')}
          </span>
          <button
            type="submit"
            disabled={saving || (!url.trim() && !tokenSet)}
            className="ml-auto flex items-center gap-2 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-canvas transition-opacity duration-150 hover:opacity-90 disabled:opacity-40"
          >
            {saving ? t('settings.proxmox.saving') : t('settings.proxmox.save')}
          </button>
          {tokenSet && editing && (
            <button
              type="button"
              onClick={() => {
                setEditing(false)
                setSecret('')
                setError(null)
              }}
              className="rounded-lg border border-border px-3 py-2 text-sm font-medium text-text-secondary transition-colors duration-150 hover:text-text-primary"
            >
              {t('settings.users.cancel')}
            </button>
          )}
          {tokenSet && (
            <button
              type="button"
              onClick={() => void disable()}
              disabled={saving}
              className="rounded-lg border border-danger/30 px-3 py-2 text-sm font-medium text-danger transition-colors duration-150 hover:border-danger/60 disabled:opacity-40"
            >
              {t('settings.proxmox.disable')}
            </button>
          )}
        </div>
        {error && <p className="mt-2 text-caption text-danger">{error}</p>}
        <p className="mt-3 text-caption leading-relaxed text-text-muted">{t('settings.proxmox.hint')}</p>
      </form>
    </Card>
  )
}


// ---------------------------------------------------------------------------
// Bloque «Sistema» en Acerca de (SPEC-65 D65-6/7e): GET /api/system/info
// ---------------------------------------------------------------------------

interface SystemInfoData {
  version: string
  goVersion: string
  os: string
  arch: string
  distro: string
  kernel: string
  cpuModel: string
  cpuCores: number
  memTotalMb: number
  uptimeS: number
  demo: boolean
}

function fmtUptime(s: number): string {
  if (!s || s <= 0) return '—'
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m`
  return `${Math.floor(s)}s`
}

function SystemInfoBlock({ bare = false }: { bare?: boolean }) {
  const { t } = useTranslation()
  const [info, setInfo] = useState<SystemInfoData | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let disposed = false
    void (async () => {
      try {
        const res = await fetch('/api/system/info')
        // La preview estática responde HTML con 200: no es el endpoint
        if (!res.ok || !(res.headers.get('content-type') ?? '').includes('application/json')) {
          throw new Error(`HTTP ${res.status}`)
        }
        const json = (await res.json()) as SystemInfoData
        if (!disposed) setInfo(json)
      } catch {
        if (!disposed) setFailed(true)
      }
    })()
    return () => {
      disposed = true
    }
  }, [])

  // Fetch fallido o servidor en demo: solo App + React + caption (SPEC-65 D65-7e)
  const server = info && !info.demo ? info : null
  const rows: { label: string; value: string }[] = [
    { label: t('settings.about.sysApp'), value: `v${server?.version || pkg.version}` },
    ...(server ? [{ label: t('settings.about.sysGo'), value: server.goVersion || '—' }] : []),
    { label: t('settings.about.sysReact'), value: React.version },
    ...(server
      ? [
          { label: t('settings.about.sysOs'), value: `${server.distro || server.os} ${server.arch}`.trim() || '—' },
          { label: t('settings.about.sysKernel'), value: server.kernel || '—' },
          {
            label: t('settings.about.sysCpu'),
            value: server.cpuModel ? `${server.cpuModel} (${server.cpuCores})` : server.cpuCores > 0 ? `${server.cpuCores}` : '—',
          },
          {
            label: t('settings.about.sysRam'),
            value: server.memTotalMb > 0 ? `${(server.memTotalMb / 1024).toFixed(1)} GiB` : '—',
          },
          { label: t('settings.about.sysUptime'), value: fmtUptime(server.uptimeS) },
        ]
      : []),
  ]

  return (
    <div className={bare ? '' : 'mt-5 border-t border-border pt-4'}>
      <div className="text-caption font-semibold uppercase tracking-[0.06em] text-text-muted">
        {t('settings.about.system')}
      </div>
      {!info && !failed ? (
        <div className="mt-2.5 grid animate-pulse grid-cols-1 gap-x-6 gap-y-2.5 sm:grid-cols-2" aria-hidden="true">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="flex items-baseline justify-between gap-3">
              <span className="h-3 w-14 rounded bg-elevated" />
              <span className="h-3 w-24 rounded bg-elevated" />
            </div>
          ))}
        </div>
      ) : (
        <>
          <dl className="mt-2.5 grid grid-cols-1 gap-x-6 gap-y-1.5 sm:grid-cols-2">
            {rows.map((r) => (
              <div key={r.label} className="flex items-baseline justify-between gap-3">
                <dt className="shrink-0 text-caption text-text-muted">{r.label}</dt>
                <dd className="truncate font-mono text-caption text-text-secondary">{r.value}</dd>
              </div>
            ))}
          </dl>
          {!server && <p className="mt-2 text-caption text-text-muted">{t('settings.about.demoNoServer')}</p>}
        </>
      )}
    </div>
  )
}

function BackupsPanel() {
  const { t, i18n } = useTranslation()
  const [cfg, setCfg] = useState<{ enabled: boolean; frequency_h: number; retention_days: number; last_run: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const [backupBusy, setBackupBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [okMsg, setOkMsg] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/settings/backup')
      if (res.ok) setCfg(await res.json())
    } catch { /* ignore */ }
  }, [])

  useEffect(() => { void load() }, [load])

  const save = async (patch: Record<string, unknown>) => {
    setBusy(true)
    setError(null)
    try {
      const res = await fetch('/api/settings/backup', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(patch),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setCfg(await res.json())
    } catch {
      setError(t('settings.admin.backup.saveError'))
    } finally {
      setBusy(false)
    }
  }

  const runBackup = async () => {
    setBackupBusy(true)
    setError(null)
    try {
      const res = await fetch('/api/backup/run', { method: 'POST' })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setOkMsg(t('settings.admin.backup.done'))
      setTimeout(() => setOkMsg(null), 3000)
      await load()
    } catch {
      setError(t('settings.admin.backup.error'))
    } finally {
      setBackupBusy(false)
    }
  }

  const locale = i18n.language?.startsWith('en') ? 'en' : 'es'

  const fmtDate = (iso: string | null) => {
    if (!iso) return '—'
    try {
      return new Date(iso).toLocaleString(locale === 'en' ? 'en-GB' : 'es-ES', {
        day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit',
      })
    } catch { return iso }
  }

  if (!cfg) return <div className="animate-pulse space-y-3"><div className="h-6 w-48 rounded bg-elevated" /><div className="h-6 w-32 rounded bg-elevated" /></div>

  return (
    <div className="w-full space-y-3">
      <div className="flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-2 cursor-pointer">
          <Switch
            checked={cfg.enabled}
            onCheckedChange={() => void save({ enabled: !cfg.enabled })}
            disabled={busy}
          />
          <span className="text-sm text-text-primary">{t('settings.admin.backup.autoBackup')}</span>
        </label>

        <button
          type="button"
          disabled={backupBusy}
          onClick={() => void runBackup()}
          className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border bg-elevated px-3 text-xs font-medium text-text-secondary transition-colors hover:bg-hover hover:text-text-primary disabled:opacity-60"
        >
          <HardDrive className="h-3.5 w-3.5" strokeWidth={1.75} />
          {backupBusy ? t('settings.admin.backup.running') : t('settings.admin.backup.runNow')}
        </button>

        <a
          href="/api/backup/download"
          className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border bg-elevated px-3 text-xs font-medium text-text-secondary transition-colors hover:bg-hover hover:text-text-primary"
        >
          <Download className="h-3.5 w-3.5" strokeWidth={1.75} />
          {t('settings.admin.backup.export')}
        </a>
      </div>

      {cfg.enabled && (
        <div className="flex flex-wrap items-center gap-3">
          <label className="flex items-center gap-2 text-caption text-text-muted">
            <span>{t('settings.admin.backup.frequency')}</span>
            <select
              value={cfg.frequency_h}
              disabled={busy}
              onChange={(e) => void save({ frequency_h: parseInt(e.target.value, 10) })}
              className="rounded-lg border border-border bg-elevated px-2 py-1 text-xs text-text-primary"
            >
              {[1, 6, 12, 24, 48, 72].map((h) => (
                <option key={h} value={h}>{h}h</option>
              ))}
            </select>
          </label>
          <label className="flex items-center gap-2 text-caption text-text-muted">
            <span>{t('settings.admin.backup.retention')}</span>
            <select
              value={cfg.retention_days}
              disabled={busy}
              onChange={(e) => void save({ retention_days: parseInt(e.target.value, 10) })}
              className="rounded-lg border border-border bg-elevated px-2 py-1 text-xs text-text-primary"
            >
              {[1, 3, 7, 14, 30].map((d) => (
                <option key={d} value={d}>{d} {t('settings.admin.backup.days')}</option>
              ))}
            </select>
          </label>
        </div>
      )}

      <p className="text-caption text-text-muted">
        {cfg.last_run
          ? t('settings.admin.backup.lastRun', { date: fmtDate(cfg.last_run) })
          : t('settings.admin.backup.noBackups')}
      </p>

      {error && <p role="alert" className="text-xs text-danger">{error}</p>}
      {okMsg && <p role="status" className="text-xs text-ok">{okMsg}</p>}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Velocidad WAN contratada (issue #151) — declarada por el admin en kv y
// persistida vía GET/PUT /api/settings/wanspeed. Se muestra como sub-sección
// de la tarjeta «Datos y umbrales» (mismo ámbito: métricas WAN).
// ---------------------------------------------------------------------------

function WanSpeedCard({ onSaved, disabled = false }: { onSaved: () => void; disabled?: boolean }) {
  const { t } = useTranslation()
  const { refresh: refreshOverview } = useNetPulse()
  const [down, setDown] = useState('')
  const [up, setUp] = useState('')
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)
  const loaded = useRef(false)

  // ——— Test manual real (issue #627 / #511): POST /api/speedtest/run y poll
  // de /api/speedtest/status. La API solo expone running/last (no la fase
  // concreta), así que la barra de fases es una progresión estimada local.
  const [testing, setTesting] = useState(false)
  const [testDone, setTestDone] = useState(false)
  const [testError, setTestError] = useState<string | null>(null)
  const [testProgress, setTestProgress] = useState(0)
  const pollTimer = useRef<number | undefined>(undefined)
  const progTimer = useRef<number | undefined>(undefined)
  const testStarted = useRef(0)
  const testPhase = testProgress < 8 ? 0 : testProgress < 55 ? 1 : 2

  const stopTestTimers = () => {
    window.clearInterval(pollTimer.current)
    window.clearInterval(progTimer.current)
    pollTimer.current = undefined
    progTimer.current = undefined
  }

  const runTest = async () => {
    if (testing || disabled) return
    setTesting(true)
    setTestDone(false)
    setTestError(null)
    setTestProgress(0)
    testStarted.current = Date.now()
    try {
      const res = await fetch('/api/speedtest/run', { method: 'POST' })
      if (res.status === 503) {
        setTesting(false)
        setTestError(t('settings.speedtest.unavailable'))
        return
      }
      if (!res.ok && res.status !== 409) {
        setTesting(false)
        setTestError(t('settings.wanSpeed.testFailed'))
        return
      }
    } catch {
      setTesting(false)
      setTestError(t('settings.wanSpeed.testFailed'))
      return
    }

    // Progresión visual (~30 s de estimación) mientras el poll confirma running.
    progTimer.current = window.setInterval(() => {
      setTestProgress((p) => Math.min(100, Math.max(p, ((Date.now() - testStarted.current) / 30000) * 100)))
    }, 300)

    const poll = async () => {
      try {
        const r = await fetch('/api/speedtest/status')
        if (!r.ok) return
        const st = (await r.json()) as {
          running: boolean
          lastError?: string
          last?: { downMbps?: number; upMbps?: number } | null
        }
        if (st.running) return
        // Terminó: aplicar el último resultado real a los campos (sin guardar).
        stopTestTimers()
        setTestProgress(100)
        if (st.last && typeof st.last.downMbps === 'number' && typeof st.last.upMbps === 'number') {
          setDown(String(Math.round(st.last.downMbps)))
          setUp(String(Math.round(st.last.upMbps)))
          setTestDone(true)
          window.setTimeout(() => setTestDone(false), 4000)
        } else {
          setTestError(st.lastError || t('settings.wanSpeed.testFailed'))
        }
        setTesting(false)
      } catch {
        /* el siguiente poll reintentará */
      }
    }
    pollTimer.current = window.setInterval(() => void poll(), 1500)
  }

  // Limpieza al desmontar.
  useEffect(() => stopTestTimers, [])

  // Carga el valor persistido una sola vez al montar (no se pisa al guardar).
  useEffect(() => {
    let alive = true
    void fetch('/api/settings/wanspeed')
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (!alive || !d || loaded.current) return
        loaded.current = true
        if (typeof d.downMbps === 'number') setDown(String(d.downMbps))
        if (typeof d.upMbps === 'number') setUp(String(d.upMbps))
      })
      .catch(() => undefined)
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
    }
  }, [])

  const invalid = (v: string) => {
    const n = Number(v)
    return !Number.isFinite(n) || n <= 0 || n > 100000
  }

  const save = useCallback(async () => {
    if (invalid(down) || invalid(up)) {
      setError(t('settings.wanSpeed.invalid'))
      return
    }
    setError(null)
    setBusy(true)
    try {
      const res = await fetch('/api/settings/wanspeed', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ downMbps: Number(down), upMbps: Number(up) }),
      })
      if (!res.ok) {
        setError(t('settings.wanSpeed.saveError'))
        return
      }
      loaded.current = true
      setSaved(true)
      window.setTimeout(() => setSaved(false), 2000)
      refreshOverview() // la capacidad contratada del gráfico WAN se actualiza en vivo
      onSaved()
    } finally {
      setBusy(false)
    }
  }, [down, up, onSaved, refreshOverview, t])

  const phaseLabel =
    testPhase === 0
      ? t('settings.wanSpeed.phaseServer')
      : testPhase === 1
        ? t('settings.wanSpeed.phaseDown')
        : t('settings.wanSpeed.phaseUp')

  return (
    <div className="space-y-3">
      <div>
        <div className="text-sm font-medium text-text-primary">{t('settings.wanSpeed.title')}</div>
        <div className="text-caption text-text-muted">{t('settings.wanSpeed.caption')}</div>
      </div>

      {/* Descarga + Subida + test de velocidad en la MISMA fila */}
      <div className="grid grid-cols-1 items-end gap-3 sm:grid-cols-2 lg:grid-cols-[1fr_1fr_auto]">
        <label className="block">
          <span className="text-label uppercase text-text-muted">{t('settings.wanSpeed.down')}</span>
          <input
            type="text"
            inputMode="decimal"
            value={down}
            onChange={(e) => setDown(e.target.value)}
            disabled={disabled || loading}
            aria-label={t('settings.wanSpeed.down')}
            className="mt-1 w-full rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
        </label>
        <label className="block">
          <span className="text-label uppercase text-text-muted">{t('settings.wanSpeed.up')}</span>
          <input
            type="text"
            inputMode="decimal"
            value={up}
            onChange={(e) => setUp(e.target.value)}
            disabled={disabled || loading}
            aria-label={t('settings.wanSpeed.up')}
            className="mt-1 w-full rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
        </label>
        {!disabled && (
          <div className="lg:justify-self-end">
            {testing ? (
              <button
                type="button"
                disabled
                className="inline-flex h-9 shrink-0 cursor-not-allowed items-center gap-1.5 rounded-xl border border-accent/40 bg-accent-soft px-3 text-[13px] font-medium text-accent opacity-70"
              >
                <Loader2 className="h-4 w-4 animate-spin" strokeWidth={1.75} />
                {phaseLabel}…
              </button>
            ) : (
              <button
                type="button"
                onClick={() => void runTest()}
                disabled={disabled || busy || loading}
                className="inline-flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-xl border border-border bg-elevated px-3 text-[13px] font-medium text-text-primary transition-colors hover:border-accent/40 hover:text-accent disabled:cursor-not-allowed disabled:opacity-60"
              >
                <Gauge className="h-4 w-4" strokeWidth={1.75} />
                {t('settings.wanSpeed.runTest')}
              </button>
            )}
          </div>
        )}
      </div>

      <p className="text-caption text-text-muted">{t('settings.wanSpeed.hint')}</p>

      {/* Barra de fases cuando corre el test */}
      {!disabled && testing && (
        <div className="space-y-2.5 rounded-xl border border-border bg-elevated/60 p-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="flex items-center gap-2 text-caption font-medium text-accent">
              <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
              {phaseLabel}…
            </span>
            <span className="font-mono text-[10px] text-text-muted">{Math.round(testProgress)} %</span>
          </div>
          {/* Barra de progreso de fases */}
          <div className="flex items-center gap-1.5">
            {[t('settings.wanSpeed.phaseServer'), t('settings.wanSpeed.phaseDown'), t('settings.wanSpeed.phaseUp')].map(
              (label, i) => {
                const active = i === testPhase
                const done = i < testPhase || testProgress >= 100
                return (
                  <span
                    key={label}
                    className={cn(
                      'h-1.5 flex-1 rounded-full transition-colors duration-300',
                      done || active ? 'bg-accent' : 'bg-border-strong',
                    )}
                  />
                )
              },
            )}
          </div>
          <div className="h-1 w-full overflow-hidden rounded-full bg-border/60">
            <div
              className="h-full rounded-full bg-accent/70 transition-[width] duration-300 ease-linear"
              style={{ width: `${testProgress}%` }}
            />
          </div>
          <p className="text-caption text-text-muted">{t('settings.wanSpeed.testHint')}</p>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          onClick={() => void save()}
          disabled={disabled || busy || loading || testing}
          className="inline-flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-xl bg-accent px-3 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {busy ? t('settings.wanSpeed.saving') : t('settings.wanSpeed.save')}
        </button>
        {saved && (
          <span role="status" className="text-caption text-ok">
            {t('settings.wanSpeed.saved')}
          </span>
        )}
        {testDone && (
          <span role="status" className="text-caption text-ok">
            {t('settings.wanSpeed.testApplied', { down, up })}
          </span>
        )}
        {testError && (
          <span role="alert" className="text-caption text-danger">
            {testError}
          </span>
        )}
        {error && (
          <span role="alert" className="text-caption text-danger">
            {error}
          </span>
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Test de velocidad WAN (issue #511) — configuración del test periódico.
// Sub-sección de «Datos y umbrales» junto a la velocidad contratada: el
// umbral de alerta (% del plan) depende de ella.
// ---------------------------------------------------------------------------

function SpeedtestCard({ onSaved, disabled = false }: { onSaved: () => void; disabled?: boolean }) {
  const { t } = useTranslation()
  const [enabled, setEnabled] = useState(false)
  const [intervalHours, setIntervalHours] = useState(12)
  const [alertPct, setAlertPct] = useState(50)
  const [serverId, setServerId] = useState('0')
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)
  const loaded = useRef(false)

  useEffect(() => {
    let alive = true
    void fetch('/api/settings/speedtest')
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (!alive || !d || loaded.current) return
        loaded.current = true
        setEnabled(!!d.enabled)
        if (typeof d.intervalHours === 'number') setIntervalHours(d.intervalHours)
        if (typeof d.alertPct === 'number') setAlertPct(d.alertPct)
        if (typeof d.serverId === 'number') setServerId(String(d.serverId))
      })
      .catch(() => undefined)
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
    }
  }, [])

  const save = useCallback(async () => {
    const raw = serverId.trim()
    const sid = raw === '' || raw === '0' || raw.toLowerCase() === 'speedtest' ? 0 : Number(raw)
    if (![6, 12, 24].includes(intervalHours) || !Number.isInteger(sid) || sid < 0 || alertPct < 0 || alertPct > 90) {
      setError(t('settings.speedtest.invalid'))
      return
    }
    setError(null)
    setBusy(true)
    try {
      const res = await fetch('/api/settings/speedtest', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled, intervalHours, serverId: sid, alertPct }),
      })
      if (!res.ok) {
        setError(t('settings.speedtest.saveError'))
        return
      }
      setSaved(true)
      window.setTimeout(() => setSaved(false), 2000)
      onSaved()
    } finally {
      setBusy(false)
    }
  }, [enabled, intervalHours, serverId, alertPct, onSaved, t])

  return (
    <div className="space-y-3">
      <div>
        <div className="text-sm font-medium text-text-primary">{t('settings.speedtest.title')}</div>
        <div className="text-caption text-text-muted">{t('settings.speedtest.caption')}</div>
      </div>
      <label className="flex items-center justify-between gap-3">
        <span className="text-label uppercase text-text-muted">{t('settings.speedtest.enabled')}</span>
        <input
          type="checkbox"
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
          disabled={disabled || loading}
          aria-label={t('settings.speedtest.enabled')}
          className="h-4 w-4 accent-[rgb(var(--accent))]"
        />
      </label>
      <div className="grid grid-cols-2 gap-3">
        <label className="block">
          <span className="text-label uppercase text-text-muted">{t('settings.speedtest.interval')}</span>
          <select
            value={intervalHours}
            onChange={(e) => setIntervalHours(Number(e.target.value))}
            disabled={disabled || loading}
            aria-label={t('settings.speedtest.interval')}
            className="mt-1 w-full rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none"
          >
            {[6, 12, 24].map((h) => (
              <option key={h} value={h}>
                {t('settings.speedtest.intervalH', { hours: h })}
              </option>
            ))}
          </select>
        </label>
        <label className="block">
          <span className="text-label uppercase text-text-muted">{t('settings.speedtest.alertPctLabel')}</span>
          <input
            type="text"
            inputMode="numeric"
            value={alertPct}
            onChange={(e) => setAlertPct(Number(e.target.value))}
            disabled={disabled || loading}
            aria-label={t('settings.speedtest.alertPctLabel')}
            className="mt-1 w-full rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary focus:border-accent focus:outline-none"
          />
        </label>
      </div>
      <p className="text-caption text-text-muted">{t('settings.speedtest.alertPctHint')}</p>
      <div>
        <span className="text-label uppercase text-text-muted">{t('settings.speedtest.serverId')}</span>
        <div className="mt-1 flex items-center gap-2">
          <input
            type="text"
            inputMode="text"
            value={serverId === '0' || serverId === '' ? 'speedtest' : serverId}
            onChange={(e) => setServerId(e.target.value)}
            disabled={disabled || loading}
            placeholder="speedtest"
            aria-label={t('settings.speedtest.serverId')}
            className="w-full rounded-lg border border-border bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
          <button
            type="button"
            onClick={() => setServerId('0')}
            disabled={disabled || loading}
            title={t('settings.speedtest.serverRestore')}
            aria-label={t('settings.speedtest.serverRestore')}
            className="flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-lg border border-border bg-elevated px-3 text-[13px] font-medium text-text-secondary transition-colors hover:bg-hover hover:text-text-primary disabled:cursor-not-allowed disabled:opacity-60"
          >
            <RotateCcw className="h-3.5 w-3.5" strokeWidth={1.75} />
            <span className="hidden sm:inline">{t('settings.speedtest.serverRestore')}</span>
          </button>
        </div>
      </div>
      <p className="text-caption text-text-muted">{t('settings.speedtest.hint')}</p>
      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          onClick={() => void save()}
          disabled={disabled || busy || loading}
          className="inline-flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-xl bg-accent px-3 text-[13px] font-semibold text-canvas transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {busy ? t('settings.speedtest.saving') : t('settings.speedtest.save')}
        </button>
        {saved && (
          <span role="status" className="text-caption text-ok">
            {t('settings.speedtest.saved')}
          </span>
        )}
        {error && (
          <span role="alert" className="text-caption text-danger">
            {error}
          </span>
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Página Ajustes `/settings` (settings.md)
// ---------------------------------------------------------------------------

function ServicesCard({
  reduce,
  onSaved,
  disabled = false,
  orchOn,
  orchBusy,
  toggleOrchestration,
}: {
  reduce: boolean
  onSaved: () => void
  disabled?: boolean
  orchOn: boolean
  orchBusy: boolean
  toggleOrchestration: (enabled: boolean) => void
}) {
  const { t } = useTranslation()
  const [services, setService] = useServicesVisibility()
  const rows: { key: keyof ServicesVisibility; label: string; caption: string }[] = [
    { key: 'adguard', label: 'AdGuard Home', caption: t('settings.services.adguardCaption') },
    { key: 'wireguard', label: 'WireGuard', caption: t('settings.services.wireguardCaption') },
    { key: 'openvpn', label: 'OpenVPN', caption: t('settings.services.openvpnCaption') },
  ]
  return (
    <Card title={t('settings.services.title')} caption={t('settings.services.caption')} index={3} reduce={reduce}>
      <div className="grid grid-cols-1 gap-x-6 gap-y-0 sm:grid-cols-2">
        <div className="divide-y divide-border/60">
          <SwitchRow
            label={rows[0]!.label}
            caption={rows[0]!.caption}
            checked={services[rows[0]!.key]}
            disabled={disabled}
            onCheckedChange={(v) => {
              setService(rows[0]!.key, v)
              onSaved()
            }}
          />
          <SwitchRow
            label={rows[1]!.label}
            caption={rows[1]!.caption}
            checked={services[rows[1]!.key]}
            disabled={disabled}
            onCheckedChange={(v) => {
              setService(rows[1]!.key, v)
              onSaved()
            }}
          />
        </div>
        <div className="divide-y divide-border/60">
          <SwitchRow
            label={rows[2]!.label}
            caption={rows[2]!.caption}
            checked={services[rows[2]!.key]}
            disabled={disabled}
            onCheckedChange={(v) => {
              setService(rows[2]!.key, v)
              onSaved()
            }}
          />
          <div className="py-3">
            <SwitchRow
              label={t('settings.services.labs')}
              caption={t('settings.services.labsCaption')}
              checked={services.labs}
              disabled={disabled}
              onCheckedChange={(v) => {
                setService('labs', v)
                onSaved()
              }}
            />
          </div>
        </div>
      </div>

      {services.labs && (
        <div className="mt-4 space-y-3 border-t border-border pt-4">
          <div className="rounded-xl bg-elevated px-4 py-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <span className="text-sm font-medium text-text-primary">{t('settings.admin.orchestration')}</span>
                <p className="text-caption text-text-muted">{t('settings.services.orchestrationHint')}</p>
              </div>
              <Switch
                checked={orchOn}
                onCheckedChange={(v) => void toggleOrchestration(v)}
                disabled={orchBusy || disabled}
                aria-label={t('settings.admin.orchestration')}
              />
            </div>
          </div>

          <ExternalDevicesManager onSaved={onSaved} />
        </div>
      )}

      <p className="mt-3 rounded-xl bg-elevated px-3.5 py-2.5 text-caption leading-relaxed text-text-muted">
        {t('settings.services.note')}
      </p>
    </Card>
  )
}

// ---------------------------------------------------------------------------
// Tarjeta «Notificaciones push» (SPEC-PUSH §2)
// ---------------------------------------------------------------------------

type PushCardState =
  | 'loading'
  | 'insecure' // sin contexto seguro (HTTP en LAN que no sea localhost)
  | 'unsupported' // sin SW / PushManager / Notification
  | 'demo' // demo local: no se simula (una suscripción sin servidor nunca recibiría nada)
  | 'denied' // permiso de notificaciones denegado en el navegador
  | 'enabled' // suscripción push activa
  | 'disabled' // todo listo, falta activar

function PushNotificationsCard({ reduce, onSaved, compact }: { reduce: boolean; onSaved: () => void; compact?: boolean }) {
  const { t } = useTranslation()
  const { isDemo } = useNetPulse()
  const [state, setState] = useState<PushCardState>('loading')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Detección del estado real: soporte → demo → permiso → suscripción viva
  useEffect(() => {
    let cancelled = false
    void (async () => {
      const ctx = pushContext()
      if (ctx !== 'ok') {
        setState(ctx)
        return
      }
      if (isDemo) {
        setState('demo')
        return
      }
      if (Notification.permission === 'denied') {
        setState('denied')
        return
      }
      try {
        const reg = await navigator.serviceWorker.getRegistration()
        const sub = reg ? await reg.pushManager.getSubscription() : null
        if (!cancelled) setState(sub ? 'enabled' : 'disabled')
      } catch {
        if (!cancelled) setState('disabled')
      }
    })()
    return () => {
      cancelled = true
    }
  }, [isDemo])

  /** requestPermission → pushManager.subscribe(VAPID) → POST /api/push/subscribe */
  const enable = useCallback(async () => {
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      const perm = await Notification.requestPermission()
      if (perm !== 'granted') {
        setState(perm === 'denied' ? 'denied' : 'disabled')
        return
      }
      const vapid = await getVapidKey()
      if (!vapid) {
        setError(t('settings.push.errorServer'))
        return
      }
      const reg = await navigator.serviceWorker.ready
      // Timeout de seguridad: si el push service del navegador no responde
      // (sin red, FCM inalcanzable…) el botón no se queda «Activando…»
      const sub = await Promise.race([
        reg.pushManager.subscribe({
          userVisibleOnly: true,
          applicationServerKey: urlBase64ToUint8Array(vapid) as BufferSource,
        }),
        new Promise<never>((_, reject) =>
          window.setTimeout(() => reject(new Error('push subscribe timeout')), 15000),
        ),
      ])
      const json = sub.toJSON()
      const ok = await postPushSubscribe({
        endpoint: sub.endpoint,
        keys: { auth: json.keys?.auth ?? '', p256dh: json.keys?.p256dh ?? '' },
      })
      if (!ok) {
        // El servidor no la guardó: baja local para no dejar estado fantasma
        await sub.unsubscribe().catch(() => false)
        setError(t('settings.push.errorServer'))
        return
      }
      setState('enabled')
      onSaved()
    } catch {
      setError(t('settings.push.errorGeneric'))
    } finally {
      setBusy(false)
    }
  }, [busy, t, onSaved])

  /** Baja local + POST /api/push/unsubscribe (best-effort: el servidor purga 404/410) */
  const disable = useCallback(async () => {
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      const reg = await navigator.serviceWorker.getRegistration()
      const sub = reg ? await reg.pushManager.getSubscription() : null
      if (sub) {
        const endpoint = sub.endpoint
        await sub.unsubscribe().catch(() => false)
        await postPushUnsubscribe(endpoint)
      }
      setState('disabled')
      onSaved()
    } catch {
      setError(t('settings.push.errorGeneric'))
    } finally {
      setBusy(false)
    }
  }, [busy, t, onSaved])

  const btnBase =
    'flex shrink-0 items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-semibold transition-opacity hover:opacity-90 disabled:opacity-50'

  const inner = (
    <>
      {state === 'loading' && <p className="text-caption text-text-muted">{t('settings.push.checking')}</p>}

      {state === 'enabled' && (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-2.5">
            <span className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-ok/10 px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wider text-ok">
              <BellRing className="h-3.5 w-3.5" strokeWidth={2} />
              {t('settings.push.stateOn')}
            </span>
            {!compact && <p className="text-caption leading-snug text-text-muted">{t('settings.push.stateOnCaption')}</p>}
          </div>
          <button type="button" disabled={busy} onClick={() => void disable()} className={cn(btnBase, 'border border-border bg-elevated text-text-primary')}>
            <BellOff className="h-3.5 w-3.5" strokeWidth={2} />
            {busy ? t('settings.push.disabling') : t('settings.push.disable')}
          </button>
        </div>
      )}

      {state === 'disabled' && (
        <div className="flex flex-wrap items-center justify-between gap-3">
          {!compact && <p className="min-w-0 flex-1 text-caption leading-snug text-text-secondary">{t('settings.push.stateOffCaption')}</p>}
          <button type="button" disabled={busy} onClick={() => void enable()} className={cn(btnBase, 'bg-accent text-canvas')}>
            <BellRing className="h-3.5 w-3.5" strokeWidth={2} />
            {busy ? t('settings.push.enabling') : t('settings.push.enable')}
          </button>
        </div>
      )}

      {state === 'denied' && (
        <p className="rounded-xl bg-warn/10 px-3 py-2 text-caption leading-relaxed text-warn">
          {t('settings.push.denied')}
        </p>
      )}

      {state === 'unsupported' && (
        <p className="rounded-xl bg-elevated px-3 py-2 text-caption leading-relaxed text-text-muted">
          {t('settings.push.unsupported')}
        </p>
      )}

      {state === 'insecure' && (
        <p className="rounded-xl bg-warn/10 px-3 py-2 text-caption leading-relaxed text-warn">
          {t('settings.push.insecure')}
        </p>
      )}

      {state === 'demo' && (
        <p className="rounded-xl bg-elevated px-3 py-2 text-caption leading-relaxed text-text-muted">
          {t('settings.push.demoNote')}
        </p>
      )}

      {error && (
        <p role="alert" className="mt-3 rounded-lg bg-danger/10 px-3 py-2 text-caption text-danger">
          {error}
        </p>
      )}

      {!compact && (state === 'enabled' || state === 'disabled') && (
        <p className="mt-3 text-caption leading-relaxed text-text-muted">{t('settings.push.note')}</p>
      )}
    </>
  )

  if (compact) return inner

  return (
    <Card title={t('settings.push.title')} caption={t('settings.push.caption')} index={4} reduce={reduce}>
      {inner}
    </Card>
  )
}

// ---------------------------------------------------------------------------
// Modo demo (issue #4): activar/desactivar desde la UI sin reinstalar.
// El instalador ya no pregunta por demo (default BD limpia); este card es la
// vía de explorarla después. Solo visible para admin con backend real (en el
// demo local sin backend no hay .env que tocar).
// ---------------------------------------------------------------------------
function DemoCard({ onSaved }: { reduce?: boolean; onSaved: () => void }) {
  const { t } = useTranslation()
  const [serverMode, setServerMode] = useState<'demo' | 'live' | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void (async () => {
      try {
        const res = await fetch('/api/health', { signal: AbortSignal.timeout(3000) })
        if (!res.ok) return
        const json = (await res.json()) as { mode?: string }
        if (json.mode === 'demo' || json.mode === 'live') setServerMode(json.mode)
      } catch {
        /* sin backend: el card no se muestra */
      }
    })()
  }, [])

  const switchMode = useCallback(
    async (enable: boolean) => {
      if (busy) return
      setBusy(true)
      setError(null)
      try {
        const res = await fetch('/api/demo/enable', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ enable }),
          signal: AbortSignal.timeout(15_000),
        })
        if (!res.ok) {
          setError(`${t('settings.demo.title')} — HTTP ${res.status}`)
          setBusy(false)
          return
        }
        onSaved()
        // El servidor se está reiniciando (flag .restart-me → systemd.path).
        // Recargar en unos segundos para recoger el nuevo modo.
        window.setTimeout(() => window.location.reload(), 6000)
      } catch {
        setError(`${t('settings.demo.title')} — fetch error`)
        setBusy(false)
      }
    },
    [busy, onSaved, t],
  )

  if (serverMode === null) return null

  // Switch compacto de la AdminBar (issue #118): el checkbox de la tarjeta
  // grande se sustituye por este toggle de una línea.
  return (
    <label
      className="inline-flex h-9 shrink-0 cursor-pointer items-center gap-2 rounded-xl border border-border bg-elevated px-3 text-[13px] font-medium text-text-secondary transition-colors hover:bg-hover hover:text-text-primary"
      title={t('settings.demo.caption')}
    >
      <FlaskConical className="h-4 w-4 shrink-0 text-text-muted" strokeWidth={1.75} aria-hidden="true" />
      <span className="hidden sm:inline">{t('settings.demo.title')}</span>
      <Switch
        checked={serverMode === 'demo'}
        onCheckedChange={(v) => void switchMode(v)}
        disabled={busy}
        aria-label={t('settings.demo.title')}
      />
      {busy && <span className="h-2 w-2 animate-ping-soft rounded-full bg-accent" aria-hidden="true" />}
      {error && (
        <span role="alert" className="text-caption text-danger">
          {error}
        </span>
      )}
    </label>
  )
}

// ---------------------------------------------------------------------------
// Página Ajustes `/settings` (settings.md)
// ---------------------------------------------------------------------------

/** Widget inline de "Comprobar actualizaciones" para la AdminBar. Usa los
 *  endpoints /api/update/status y /api/update/apply (misma lógica que
 *  UpdateBanner, sin banner): al pulsar comprueba y muestra estado compacto.
 *  Con readiness (issue #160): los checks previos bloquean el botón. */
interface NetPulseUpdateStatus {
  current: string
  latest: string | null
  latestMsg: string | null
  latestBody?: string | null
  commits?: { sha: string; subject: string }[] | null
  compareUrl?: string | null
  updateAvailable: boolean
  canApply: boolean
  repo: string
  updating: false | { step: string }
  readiness?: UpdateReadiness | null
}

function UpdateCheckInline() {
  const { t } = useTranslation()
  const [status, setStatus] = useState<NetPulseUpdateStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)
  const [dialogOpen, setDialogOpen] = useState(false)

  const check = async () => {
    setBusy(true)
    setError(false)
    try {
      const res = await fetch('/api/update/status')
      if (!res.ok) throw new Error('status')
      const data = (await res.json()) as NetPulseUpdateStatus
      setStatus(data)
      if (data.updateAvailable && data.latest) notifyBanner(data.latest)
    } catch {
      setError(true)
    } finally {
      setBusy(false)
    }
  }

  // Al cerrar el asistente: refrescar el estado (issue #280).
  const handleDialogChange = useCallback(
    (open: boolean) => {
      setDialogOpen(open)
      if (!open) void check()
    },
    [],
  )

  const ready = status?.readiness ? status.readiness.ready : true

  return (
    <div className="flex flex-col gap-1">
      {status?.updateAvailable && status.latest ? (
        status.canApply ? (
          <button
            type="button"
            onClick={() => setDialogOpen(true)}
            disabled={!ready}
            className="inline-flex h-9 shrink-0 items-center gap-1.5 rounded-xl border border-accent bg-accent-soft px-3 text-[13px] font-medium text-accent transition-colors hover:brightness-105 disabled:cursor-not-allowed disabled:opacity-60"
          >
            <Download className="h-4 w-4 shrink-0" strokeWidth={1.75} aria-hidden="true" />
            <span className="hidden sm:inline">{t('update.button')}</span>
          </button>
        ) : (
          <a
            href={`https://github.com/${status.repo || 'gnacho/netpulse'}/releases`}
            target="_blank"
            rel="noreferrer"
            className="inline-flex h-9 shrink-0 items-center gap-1.5 rounded-xl border border-border bg-elevated px-3 text-[13px] font-medium text-text-primary transition-colors hover:bg-hover"
          >
            <ExternalLink className="h-4 w-4 shrink-0" strokeWidth={1.75} aria-hidden="true" />
            <span className="hidden sm:inline">{t('update.getRelease')}</span>
          </a>
        )
      ) : (
        <button
          type="button"
          onClick={() => void check()}
          disabled={busy}
          className="inline-flex h-9 shrink-0 items-center gap-1.5 rounded-xl border border-border bg-elevated px-3 text-[13px] font-medium text-text-secondary transition-colors hover:bg-hover hover:text-text-primary disabled:opacity-60"
        >
          <RefreshCw className={`h-4 w-4 shrink-0 ${busy ? 'animate-spin' : ''}`} strokeWidth={1.75} aria-hidden="true" />
          <span className="hidden sm:inline">{t('settings.about.checkUpdates')}</span>
        </button>
      )}
      {status?.updateAvailable && status.latest ? (
        <span className="text-[10px] font-medium text-accent">
          {t('settings.about.updateAvailable', { version: status.latest })}
        </span>
      ) : status?.updateAvailable === false ? (
        <span className="text-[10px] font-medium text-ok">{t('settings.about.upToDateShort')}</span>
      ) : error ? (
        <span role="alert" className="text-[10px] font-medium text-rose-500">
          {t('settings.about.updateError')}
        </span>
       ) : null}
      {status?.updateAvailable && status.canApply && status.readiness && (
        <div className="mt-2 w-[260px]">
          <ReadinessPanel readiness={status.readiness} compact />
        </div>
      )}
      <UpdateDialog open={dialogOpen} onOpenChange={handleDialogChange} initialStatus={status} />
    </div>
  )
}

/** Historial de actualizaciones (issue #159): tabla con los últimos applies
 *  (SHA from→to, fecha, estado, duración) servida por GET /api/updates/history. */
interface UpdateHistoryRow {
  id: number
  ts: number
  action: string
  channel: string
  versionFrom?: string
  versionTo?: string
  initiatedBy: string
  status: string
  durationMs?: number
  error?: string
}

function UpdateHistoryCard() {
  const { t } = useTranslation()
  const [rows, setRows] = useState<UpdateHistoryRow[] | null>(null)
  const [open, setOpen] = useState(false)

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/updates/history')
      if (!res.ok) return
      const json = (await res.json()) as { history?: UpdateHistoryRow[] }
      setRows(json.history ?? [])
    } catch {
      /* sin historial: la tarjeta muestra el estado vacío */
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const statusLabel = (s: string) =>
    s === 'success' ? t('update.history.success') : s === 'failed' ? t('update.history.failed') : t('update.history.running')
  const statusCls = (s: string) =>
    s === 'success' ? 'text-ok' : s === 'failed' ? 'text-rose-500' : 'text-amber-600 dark:text-amber-400'

  const downloadLog = () => {
    if (!rows) return
    const lines = ['# NetPulse — historial de actualizaciones', '# exportado ' + new Date().toISOString(), '', 'fecha\tdesde\thasta\tiniciado_por\tduracion\testado', ...rows.map((r) => [new Date(r.ts).toISOString(), r.versionFrom ?? '', r.versionTo ?? '', r.initiatedBy ?? '', r.durationMs != null ? (r.durationMs / 1000).toFixed(1) + 's' : '', r.status].join('\t'))]
    const blob = new Blob([lines.join('\n')], { type: 'text/plain' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = 'netpulse-historial-actualizaciones.log'
    a.click()
    URL.revokeObjectURL(a.href)
  }

  const count = rows?.length ?? 0
  const last = rows && rows[0] ? rows[0] : null

  return (
    <div className="rounded-2xl border border-border bg-surface p-4 md:p-5">
      {/* Cabecera colapsable (como el mockup) */}
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center justify-between gap-3 text-left"
      >
        <div className="flex items-center gap-2">
          <History className="h-4 w-4 text-accent" strokeWidth={1.75} aria-hidden="true" />
          <div>
            <h3 className="text-sm font-semibold text-text-primary">{t('update.history.title')}</h3>
            <p className="text-xs text-text-muted">
              {rows === null
                ? t('settings.about.checking')
                : last
                  ? t('update.history.summary', { count, date: relTimeFromTs(last.ts) ?? new Date(last.ts).toLocaleString(), status: statusLabel(last.status) })
                  : t('update.history.empty')}
            </p>
          </div>
        </div>
        <span className="flex shrink-0 items-center gap-2">
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation()
              downloadLog()
            }}
            disabled={!rows || rows.length === 0}
            className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-elevated px-3 py-1.5 text-xs font-medium text-text-secondary transition-colors duration-150 hover:border-accent/40 hover:text-accent disabled:cursor-not-allowed disabled:opacity-50"
          >
            <Download className="h-3.5 w-3.5" strokeWidth={1.75} />
            {t('update.history.download')}
          </button>
          <ChevronDown className={cn('h-4 w-4 text-text-muted transition-transform duration-200', open && 'rotate-180')} strokeWidth={2} />
        </span>
      </button>

      {open && (
        <div className="mt-3">
          {rows === null ? (
            <p className="text-xs text-text-muted">{t('settings.about.checking')}</p>
          ) : rows.length === 0 ? (
            <p className="text-xs text-text-secondary">{t('update.history.empty')}</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[540px] text-left text-xs">
                <thead>
                  <tr className="border-b border-border text-text-muted">
                    <th className="py-2 pr-3 font-medium">{t('update.history.date')}</th>
                    <th className="py-2 pr-3 font-medium">{t('update.history.from')}</th>
                    <th className="py-2 pr-3 font-medium">{t('update.history.to')}</th>
                    <th className="py-2 pr-3 font-medium">{t('update.history.initiatedBy')}</th>
                    <th className="py-2 pr-3 font-medium">{t('update.history.duration')}</th>
                    <th className="py-2 font-medium">{t('update.history.status')}</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((r) => (
                    <tr key={r.id} className="border-b border-border/60 last:border-0">
                      <td className="whitespace-nowrap py-2 pr-3 text-text-secondary">
                        {relTimeFromTs(r.ts) ?? new Date(r.ts).toLocaleString()}
                      </td>
                      <td className="py-2 pr-3 font-mono text-text-primary">{r.versionFrom ?? '—'}</td>
                      <td className="py-2 pr-3 font-mono text-text-primary">{r.versionTo ?? '—'}</td>
                      <td className="py-2 pr-3 text-text-secondary">{r.initiatedBy || '—'}</td>
                      <td className="py-2 pr-3 text-text-secondary">
                        {r.durationMs != null ? `${(r.durationMs / 1000).toFixed(1)} s` : '—'}
                      </td>
                      <td className={cn('py-2 font-semibold', statusCls(r.status))}>{statusLabel(r.status)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function AdoptionCard() {
  const { t } = useTranslation()
  const [data, setData] = useState<{ token: string; server_fp: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState<string | null>(null)
  const [reveal, setReveal] = useState(false)
  const [confirmRotate, setConfirmRotate] = useState(false)

  const fetchToken = async () => {
    setBusy(true)
    try {
      const res = await fetch('/api/pairing/token')
      if (res.ok) setData(await res.json())
    } catch { /* ignore */ } finally { setBusy(false) }
  }

  useEffect(() => { void fetchToken() }, [])

  const rotate = async () => {
    setBusy(true)
    try {
      const res = await fetch('/api/pairing/rotate', { method: 'POST' })
      if (res.ok) setData(await res.json())
    } catch { /* ignore */ } finally { setBusy(false) }
  }

  const copy = (val: string, label: string) => {
    void copyToClipboard(val).then((ok) => {
      if (ok) {
        setCopied(label)
        setTimeout(() => setCopied(null), 2000)
      }
    })
  }

  if (!data?.token) return null

  // Máscara: solo los últimos 4 caracteres visibles (issue #145). El token
  // completo se copia sin necesidad de revelarlo.
  const masked = `••••••••••••${data.token.slice(-4)}`

  return (
    <div className="rounded-xl border border-border bg-surface p-4">
      <div className="mb-3 flex items-center gap-2">
        <Wifi className="h-4 w-4 text-accent" strokeWidth={1.75} aria-hidden="true" />
        <h3 className="text-sm font-semibold text-text-primary">{t('settings.adoption.title')}</h3>
      </div>
      <p className="mb-3 text-xs text-text-secondary">{t('settings.adoption.hint')}</p>

      <div className="space-y-2">
        <div className="flex items-center gap-2">
          <code className="flex-1 truncate rounded-lg bg-elevated px-2.5 py-1.5 font-mono text-xs text-text-primary">
            {reveal ? data.token : masked}
          </code>
          <button
            type="button"
            onClick={() => setReveal((v) => !v)}
            aria-label={reveal ? t('settings.adoption.hide') : t('settings.adoption.reveal')}
            title={reveal ? t('settings.adoption.hide') : t('settings.adoption.reveal')}
            className="rounded-lg border border-border px-2 py-1.5 text-text-secondary hover:bg-hover"
          >
            {reveal ? <EyeOff className="h-3.5 w-3.5" strokeWidth={1.75} /> : <Eye className="h-3.5 w-3.5" strokeWidth={1.75} />}
          </button>
          <button type="button" onClick={() => copy(data.token, 'token')} className="rounded-lg border border-border px-2 py-1.5 text-xs text-text-secondary hover:bg-hover" title={t('common.copy')}>
            <Copy className="h-3.5 w-3.5" strokeWidth={1.75} />
          </button>
          {copied === 'token' && <span className="text-[10px] text-ok">✓</span>}
          <button
            type="button"
            onClick={() => setConfirmRotate(true)}
            disabled={busy}
            className="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-border bg-elevated px-3 py-1.5 text-xs font-medium text-text-secondary transition-colors hover:bg-hover disabled:opacity-60"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${busy ? 'animate-spin' : ''}`} strokeWidth={1.75} />
            {t('settings.adoption.rotate')}
          </button>
        </div>

        {data.server_fp && (
          <div className="flex items-center gap-2">
            <code className="flex-1 truncate rounded-lg bg-elevated px-2.5 py-1.5 font-mono text-xs text-text-primary">{data.server_fp}</code>
            <button type="button" onClick={() => copy(data.server_fp, 'fp')} className="rounded-lg border border-border px-2 py-1.5 text-xs text-text-secondary hover:bg-hover" title={t('common.copy')}>
              <Copy className="h-3.5 w-3.5" strokeWidth={1.75} />
            </button>
            {copied === 'fp' && <span className="text-[10px] text-ok">✓</span>}
          </div>
        )}
      </div>

      {/* Confirmación de rotación (issue #145): invalida todos los pairings
          pendientes, no puede ser un clic suelto. */}
      <AlertDialog open={confirmRotate} onOpenChange={setConfirmRotate}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('settings.adoption.rotateTitle')}</AlertDialogTitle>
            <AlertDialogDescription>{t('settings.adoption.rotateDesc')}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('settings.users.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmRotate(false)
                void rotate()
              }}
              className="bg-danger text-canvas hover:bg-danger/90"
            >
              {t('settings.adoption.rotateConfirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

// --- External devices / scrapers (Labs, issue #154) ---

interface ExternalDevice {
  slug: string
  host: string
  type: RouterType
  lastSeen: number | null
  version: string
  fresh: boolean
  hasToken: boolean
}

function ExternalDevicesManager({ onSaved }: { onSaved: () => void }) {
  const { t, i18n } = useTranslation()
  const { refresh } = useNetPulse()
  const [devices, setDevices] = useState<ExternalDevice[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState<string | null>(null)

  // Add form
  const [showAdd, setShowAdd] = useState(false)
  const [addName, setAddName] = useState('')
  const [addHost, setAddHost] = useState('')
  const [addType, setAddType] = useState<RouterType>('managed-switch')
  const [submitting, setSubmitting] = useState(false)

  // Token reveal state
  const [generatingFor, setGeneratingFor] = useState<string | null>(null)
  const [revealed, setRevealed] = useState<{ slug: string; token: string } | null>(null)

  const [confirmDelete, setConfirmDelete] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const [rRes, aRes] = await Promise.all([fetch('/api/config/routers'), fetch('/api/agents')])
      if (!rRes.ok || !aRes.ok) throw new Error(`HTTP ${rRes.status}/${aRes.status}`)
      const { routers } = (await rRes.json()) as { routers: ConfigRouter[] }
      const { agents } = (await aRes.json()) as {
        agents: Array<{ slug: string; lastSeen: number | null; version: string; fresh: boolean }>
      }
      const agentMap = new Map(agents.map((a) => [a.slug, a]))
      const ext: ExternalDevice[] = []
      for (const r of routers) {
        if (!r.agent_only) continue
        const a = agentMap.get(r.id)
        ext.push({
          slug: r.id,
          host: r.host,
          type: r.type,
          lastSeen: a?.lastSeen ?? null,
          version: a?.version ?? '',
          fresh: a?.fresh ?? false,
          hasToken: agentMap.has(r.id),
        })
      }
      for (const a of agents) {
        if (!ext.some((d) => d.slug === a.slug)) {
          ext.push({
            slug: a.slug,
            host: '',
            type: 'external',
            lastSeen: a.lastSeen,
            version: a.version,
            fresh: a.fresh,
            hasToken: true,
          })
        }
      }
      setDevices(ext)
      setError(null)
    } catch {
      setError(t('settings.routers.errorGeneric'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!addHost.trim() || submitting) return
    const name = addName.trim() || addHost.trim()
    setSubmitting(true)
    setError(null)
    try {
      const rRes = await fetch('/api/config/routers', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, host: addHost.trim(), type: addType, agent_only: true }),
      })
      if (rRes.status === 409) {
        setError(t('settings.routers.errorDuplicate'))
        setSubmitting(false)
        return
      }
      if (!rRes.ok) throw new Error(`HTTP ${rRes.status}`)
      const { router } = (await rRes.json()) as { router: { id: string } }

      const aRes = await fetch('/api/agents', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ slug: router.id }),
      })
      if (!aRes.ok) throw new Error(`HTTP ${aRes.status}`)
      const agent = (await aRes.json()) as { slug: string; token: string; install: string }

      setAddName('')
      setAddHost('')
      setAddType('managed-switch')
      setShowAdd(false)
      setRevealed({ slug: agent.slug, token: agent.token })
      await load()
      refresh()
      onSaved()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('settings.routers.errorGeneric'))
    } finally {
      setSubmitting(false)
    }
  }

  const regenerate = async (slug: string) => {
    setGeneratingFor(slug)
    try {
      const res = await fetch('/api/agents', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ slug }),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const agent = (await res.json()) as { slug: string; token: string; install: string }
      setRevealed({ slug: agent.slug, token: agent.token })
      await load()
      onSaved()
    } catch {
      setError(t('settings.routers.errorGeneric'))
    } finally {
      setGeneratingFor(null)
    }
  }

  const remove = async (slug: string) => {
    try {
      const res = await fetch(`/api/agents/${encodeURIComponent(slug)}`, { method: 'DELETE' })
      if (!res.ok && res.status !== 404) throw new Error(`HTTP ${res.status}`)
      setConfirmDelete(null)
      await load()
      refresh()
      onSaved()
    } catch {
      setError(t('settings.routers.errorGeneric'))
    }
  }

  const copy = async (text: string, label: string) => {
    const ok = await copyToClipboard(text)
    if (ok) {
      setCopied(label)
      window.setTimeout(() => setCopied(null), 1500)
    }
  }

  const fmtLastSeen = (ts: number | null): string => {
    if (!ts) return '\u2014'
    const diff = Date.now() - ts * 1000
    if (diff < 60_000) return t('settings.labs.justNow')
    if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m`
    if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h`
    return new Date(ts * 1000).toLocaleDateString(i18n.language, { day: 'numeric', month: 'short' })
  }

  if (loading) return <p className="py-3 text-caption text-text-muted">{t('settings.labs.loading')}</p>

  return (
    <div className="space-y-4">
      {error && <p className="rounded-lg bg-danger/10 px-3 py-2 text-caption text-danger">{error}</p>}

      {/* New token reveal */}
      {revealed && (
        <div className="rounded-xl border border-amber-500/40 bg-amber-500/5 p-4">
          <div className="mb-2 flex items-center justify-between">
            <p className="text-sm font-semibold text-text-primary">
              {t('settings.labs.tokenFor', { slug: revealed.slug })}
            </p>
            <button
              type="button"
              onClick={() => setRevealed(null)}
              className="rounded-lg p-1 text-text-muted hover:text-text-primary"
              aria-label={t('settings.labs.dismiss')}
            >
              <X className="h-4 w-4" />
            </button>
          </div>
          <p className="mb-2 text-caption text-amber-600 dark:text-amber-400">{t('settings.labs.tokenOnce')}</p>
          <div className="flex items-center gap-2 rounded-lg bg-elevated px-3 py-2 font-mono text-xs break-all text-text-primary">
            <span className="flex-1 select-all">{revealed.token}</span>
            <button
              type="button"
              onClick={() => copy(revealed.token, 'token')}
              className="shrink-0 rounded-md p-1 text-text-muted transition-colors hover:bg-hover hover:text-accent"
              aria-label={t('settings.labs.copyToken')}
            >
              {copied === 'token' ? <Check className="h-3.5 w-3.5 text-ok" /> : <Copy className="h-3.5 w-3.5" />}
            </button>
          </div>
          <p className="mt-2 text-caption text-text-muted">{t('settings.labs.ingestHint')}</p>
          <pre className="mt-1 overflow-x-auto rounded-lg bg-elevated p-3 text-[11px] text-text-secondary">
            {`curl -X POST http://${window.location.hostname}:3000/api/ingest/agent \\
  -H 'Authorization: Bearer ${revealed.token}' \\
  -H 'Content-Type: application/json' \\
  -d '{...}'`}
          </pre>
        </div>
      )}

      {/* Device list */}
      {devices.length === 0 ? (
        <p className="py-2 text-caption text-text-muted">{t('settings.labs.empty')}</p>
      ) : (
        <div className="space-y-2">
          {devices.map((d) => (
            <div
              key={d.slug}
              className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl border border-border bg-elevated px-4 py-3"
            >
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-sm font-medium text-text-primary">{d.slug}</span>
                  {d.fresh && (
                    <span className="inline-flex h-2 w-2 rounded-full bg-ok" title={t('settings.labs.fresh')} />
                  )}
                </div>
                <div className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-text-muted">
                  <span>{d.host || '\u2014'}</span>
                  <span className="inline-flex rounded-full bg-elevated-alt px-2 py-0.5 text-[10px] font-medium text-text-secondary">
                    {d.type}
                  </span>
                  <span>{t('settings.labs.lastSeen')}: {fmtLastSeen(d.lastSeen)}</span>
                </div>
              </div>
              <div className="flex shrink-0 items-center gap-1.5">
                {d.hasToken ? (
                  <button
                    type="button"
                    disabled={generatingFor === d.slug}
                    onClick={() => regenerate(d.slug)}
                    className="flex items-center gap-1 rounded-lg px-2.5 py-1.5 text-[11px] font-medium text-text-secondary transition-colors hover:bg-hover hover:text-accent disabled:opacity-50"
                  >
                    <KeyRound className="h-3.5 w-3.5" />
                    {generatingFor === d.slug ? '\u2026' : t('settings.labs.rotateToken')}
                  </button>
                ) : (
                  <button
                    type="button"
                    onClick={() => regenerate(d.slug)}
                    className="flex items-center gap-1 rounded-lg px-2.5 py-1.5 text-[11px] font-medium text-amber-600 transition-colors hover:bg-amber-500/10"
                  >
                    <KeyRound className="h-3.5 w-3.5" />
                    {t('settings.labs.noToken')}
                  </button>
                )}
                <button
                  type="button"
                  onClick={() => setConfirmDelete(d.slug)}
                  className="rounded-lg p-1.5 text-text-muted transition-colors hover:bg-danger/10 hover:text-danger"
                  aria-label={`${t('settings.routers.delete')} ${d.slug}`}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Add button */}
      {showAdd ? (
        <form onSubmit={create} className="space-y-3 rounded-xl border border-border bg-elevated p-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <input
              type="text"
              className="h-9 rounded-lg border border-border bg-elevated px-3 text-[13px] text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-2 focus:ring-accent/50"
              placeholder={t('settings.labs.namePlaceholder')}
              value={addName}
              onChange={(e) => setAddName(e.target.value)}
              autoFocus
            />
            <input
              type="text"
              className="h-9 rounded-lg border border-border bg-elevated px-3 text-[13px] text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-2 focus:ring-accent/50"
              placeholder={t('settings.routers.host')}
              value={addHost}
              onChange={(e) => setAddHost(e.target.value)}
              required
            />
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <SegmentedControl
              options={[
                { value: 'managed-switch', label: t('settings.routers.typeManaged') },
                { value: 'external', label: t('settings.routers.typeExternal') },
              ]}
              value={addType}
              onChange={(v) => setAddType(v as RouterType)}
              ariaLabel={t('settings.routers.type')}
            />
            <div className="ml-auto flex items-center gap-2">
              <button
                type="button"
                onClick={() => setShowAdd(false)}
                className="rounded-lg px-3 py-1.5 text-[13px] font-medium text-text-muted hover:text-text-primary"
              >
                {t('settings.labs.cancel')}
              </button>
              <button
                type="submit"
                disabled={submitting || !addHost.trim()}
                className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-[13px] font-medium text-canvas transition-opacity hover:opacity-90 disabled:opacity-40"
              >
                <Plus className="h-4 w-4" />
                {submitting ? t('settings.routers.adding') : t('settings.labs.addDevice')}
              </button>
            </div>
          </div>
        </form>
      ) : (
        <button
          type="button"
          onClick={() => setShowAdd(true)}
          className="flex w-full items-center justify-center gap-2 rounded-xl border border-dashed border-border bg-elevated/50 py-3 text-[13px] font-medium text-text-muted transition-colors hover:border-accent/50 hover:text-accent"
        >
          <Plus className="h-4 w-4" />
          {t('settings.labs.addDevice')}
        </button>
      )}

      {/* Delete confirmation */}
      {confirmDelete && (
        <AlertDialog open onOpenChange={() => setConfirmDelete(null)}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t('settings.labs.deleteTitle')}</AlertDialogTitle>
              <AlertDialogDescription>
                {t('settings.labs.deleteDesc', { slug: confirmDelete })}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t('settings.labs.cancel')}</AlertDialogCancel>
              <AlertDialogAction onClick={() => remove(confirmDelete)} className="bg-danger text-white hover:bg-danger/90">
                {t('settings.routers.delete')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Rediseño columna única (mockup v3): etiqueta de sección con línea
// ---------------------------------------------------------------------------

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex scroll-mt-32 items-center gap-3">
      <h2 className="font-display text-xs font-semibold uppercase tracking-[0.12em] text-text-muted">
        {children}
      </h2>
      <span className="h-px flex-1 bg-border" aria-hidden="true" />
    </div>
  )
}

interface SettingsIndexItem {
  href: string
  label: string
  /** Sección visible solo con backend real (no demo). */
  adminOnly?: boolean
}

/** Índice sticky de la página (scroll-spy). Las secciones se anclan por id. */
function SettingsIndex({ items }: { items: SettingsIndexItem[] }) {
  const { t } = useTranslation()
  const [active, setActive] = useState<string>(items[0]?.href ?? '')
  useEffect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) setActive(`#${entry.target.id}`)
        }
      },
      // La sección "activa" es la que cruza la franja central del viewport.
      { rootMargin: '-20% 0px -65% 0px', threshold: 0 },
    )
    for (const item of items) {
      const el = document.getElementById(item.href.slice(1))
      if (el) observer.observe(el)
    }
    return () => observer.disconnect()
  }, [items])

  const go = (e: React.MouseEvent<HTMLAnchorElement>, href: string) => {
    e.preventDefault()
    const el = document.getElementById(href.slice(1))
    if (!el) return
    el.scrollIntoView({ behavior: 'smooth', block: 'start' })
    // Corrige el ancla tras el scrollIntoView sin CSS scroll-margin.
    const topbar = 56 + 8
    const y = el.getBoundingClientRect().top + window.scrollY - topbar
    window.scrollTo({ top: Math.max(0, y), behavior: 'smooth' })
    setActive(href)
    history.replaceState(null, '', href)
  }

  return (
    <nav
      aria-label={t('settings.sections.label')}
      className="sticky top-14 z-20 -mx-4 mt-5 border-b border-border bg-canvas/85 px-4 py-2 backdrop-blur-md md:-mx-6 md:px-6"
    >
      <div className="flex gap-1.5 overflow-x-auto pb-0.5">
        {items.map((item) => (
          <a
            key={item.href}
            href={item.href}
            onClick={(e) => go(e, item.href)}
            className={cn(
              'shrink-0 rounded-full px-3 py-1 text-xs font-medium transition-colors duration-150',
              active === item.href
                ? 'bg-accent-soft text-accent'
                : 'text-text-secondary hover:bg-hover hover:text-text-primary',
            )}
          >
            {item.label}
          </a>
        ))}
      </div>
    </nav>
  )
}

export default function Settings() {
  const { t, i18n } = useTranslation()
  const reduce = useReducedMotion() ?? false
  const { devices, routers, wan, isDemo, refresh: refreshOverview } = useNetPulse()
  const auth = useAuth()
  // SPEC-65 D65-7c: la tarjeta AdGuard entera desaparece si el servicio está oculto
  const [services] = useServicesVisibility()
  const [adminPanel, setAdminPanel] = useState<'users' | 'backups' | null>(null)

  // ——— Orquestación (issue #121): toggle opt-in en la AdminBar ———
  const [orchOn, setOrchOn] = useState(false)
  const [orchBusy, setOrchBusy] = useState(false)
  const orchTouched = useRef(false)
  useEffect(() => {
    let alive = true
    void fetch('/api/settings/orchestration')
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (alive && d && !orchTouched.current) setOrchOn(!!d.enabled)
      })
      .catch(() => undefined)
    return () => {
      alive = false
    }
  }, [])
  const toggleOrchestration = useCallback(async (enabled: boolean) => {
    orchTouched.current = true
    setOrchOn(enabled)
    setOrchBusy(true)
    try {
      const res = await fetch('/api/settings/orchestration', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled }),
      })
      if (!res.ok) {
        setOrchOn(!enabled)
        return
      }
      refreshOverview() // el overview propaga el flag → el nav se actualiza
    } finally {
      setOrchBusy(false)
    }
  }, [refreshOverview])

  // ——— Idioma ('auto' por defecto sigue al navegador; elección explícita persistida) ———
  const [lang, setLang] = useState<'auto' | 'es' | 'en'>(() => {
    const raw = localStorage.getItem('netpulse-lang')
    return raw === 'es' || raw === 'en' || raw === 'auto' ? raw : 'auto'
  })
  const setLanguage = useCallback((v: 'auto' | 'es' | 'en') => {
    setLang(v)
    localStorage.setItem('netpulse-lang', v)
    if (v === 'auto') {
      localStorage.removeItem('i18nextLng')
      i18n.services.languageDetector.detect()
      void i18n.changeLanguage()
      localStorage.removeItem('i18nextLng')
    } else {
      void i18n.changeLanguage(v)
    }
    // Fuente de verdad: users.language en el backend (modo live)
    if (!isDemo) {
      void fetch('/api/users/me/language', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ language: v }),
      }).catch(() => undefined)
    }
  }, [i18n, isDemo])

  // ——— Toast "Preferencias guardadas" ———
  const [toastKey, setToastKey] = useState<number | null>(null)
  const toastTimer = useRef<number | undefined>(undefined)
  const notify = useCallback(() => {
    window.clearTimeout(toastTimer.current)
    setToastKey(Date.now())
    toastTimer.current = window.setTimeout(() => setToastKey(null), 1800)
  }, [])
  useEffect(() => () => window.clearTimeout(toastTimer.current), [])

  // ——— Cambio de la propia contraseña (Mi sesión) ———
  const [showPwdForm, setShowPwdForm] = useState(false)
  const [pwCurrent, setPwCurrent] = useState('')
  const [pwNew, setPwNew] = useState('')
  const [pwConfirm, setPwConfirm] = useState('')
  const [pwBusy, setPwBusy] = useState(false)
  const [pwError, setPwError] = useState<string | null>(null)
  const [pwChanged, setPwChanged] = useState(false)

  const submitOwnPassword = useCallback(async () => {
    if (pwBusy || !pwCurrent || pwNew.length < 10 || pwNew !== pwConfirm) return
    setPwBusy(true)
    setPwError(null)
    setPwChanged(false)
    try {
      const res = await fetch('/api/auth/password', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ current: pwCurrent, password: pwNew }),
      })
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { message?: string } | null
        throw new Error(body?.message ?? t('settings.session.pwError'))
      }
      setPwChanged(true)
      setPwCurrent('')
      setPwNew('')
      setPwConfirm('')
      notify()
    } catch (err) {
      setPwError(err instanceof Error ? err.message : t('settings.session.pwError'))
    } finally {
      setPwBusy(false)
    }
  }, [pwBusy, pwCurrent, pwNew, pwConfirm, t, notify])

  // ——— Nombre para el saludo del Resumen (SPEC-65 D65-5/7d) ———
  const [nameDraft, setNameDraft] = useState('')
  const [nameBaseline, setNameBaseline] = useState('')
  const [nameBusy, setNameBusy] = useState(false)
  const [nameError, setNameError] = useState<string | null>(null)
  // Patrón shared-shell (#119): el nombre es texto clickable; al editar se
  // muestra un input inline con ✓/✕.
  const [editingName, setEditingName] = useState(false)
  useEffect(() => {
    let v = auth?.displayName ?? ''
    if (isDemo) {
      try {
        v = localStorage.getItem('netpulse-displayname') ?? v
      } catch {
        /* modo privado */
      }
    }
    setNameBaseline(v)
    setNameDraft(v)
  }, [isDemo, auth?.displayName])

  const saveDisplayName = useCallback(async () => {
    const v = nameDraft.trim()
    if (nameBusy || v === nameBaseline) return
    setNameBusy(true)
    setNameError(null)
    if (isDemo) {
      try {
        localStorage.setItem('netpulse-displayname', v)
      } catch {
        /* modo privado */
      }
      window.dispatchEvent(new Event('netpulse-auth-refresh'))
      setNameBaseline(v)
      setNameDraft(v)
      setNameBusy(false)
      notify()
      return
    }
    try {
      const res = await fetch('/api/users/me/display-name', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ displayName: v }),
      })
      if (res.status === 404 || res.status === 405) {
        // Endpoint aún no desplegado: fallback local (mismo almacén que el demo)
        try {
          localStorage.setItem('netpulse-displayname', v)
        } catch {
          /* modo privado */
        }
      } else if (!res.ok && res.status !== 204) {
        throw new Error(`HTTP ${res.status}`)
      }
      window.dispatchEvent(new Event('netpulse-auth-refresh'))
      setNameBaseline(v)
      setNameDraft(v)
      notify()
    } catch {
      setNameError(t('settings.users.errorGeneric'))
    } finally {
      setNameBusy(false)
    }
  }, [nameDraft, nameBaseline, nameBusy, isDemo, notify, t])


  // ——— Tema (compatible con ThemeToggle: 'netpulse-theme' = light|dark) ———
  const [mode, setMode] = useStoredState<ThemeMode>(
    'netpulse-theme-mode',
    typeof localStorage !== 'undefined' && localStorage.getItem('netpulse-theme') === 'light' ? 'light' : 'dark',
  )
  const [resolvedLight, setResolvedLight] = useState(false)
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: light)')
    const apply = () => {
      const light = mode === 'light' || (mode === 'system' && mq.matches)
      document.documentElement.classList.toggle('light', light)
      document.documentElement.classList.toggle('dark', !light)
      try {
        localStorage.setItem('netpulse-theme', light ? 'light' : 'dark')
      } catch {
        /* noop */
      }
      setResolvedLight(light)
    }
    apply()
    if (mode !== 'system') return
    mq.addEventListener('change', apply)
    return () => mq.removeEventListener('change', apply)
  }, [mode])

  // ——— Paleta completa (canvas, surface, accent, semantic...) ———
  const [paletteId, setPaletteId] = useStoredState<PaletteId>('netpulse-palette', 'netpulse')
  useEffect(() => {
    const palette = PALETTES.find((x) => x.id === paletteId) ?? PALETTES[0]!
    const vars = resolvedLight ? palette.light : palette.dark
    const root = document.documentElement
    for (const [key, value] of Object.entries(vars)) {
      root.style.setProperty(`--${key}`, value)
    }
    root.setAttribute('data-palette', paletteId)
  }, [paletteId, resolvedLight])

  // ——— Densidad (compacta ≈ −15 % de tamaños/paddings vía rem) ———
  const [density, setDensity] = useStoredState<'comoda' | 'compacta'>('netpulse-density', 'comoda')
  useEffect(() => {
    document.documentElement.style.fontSize = density === 'compacta' ? '13.5px' : ''
    return () => {
      document.documentElement.style.fontSize = ''
    }
  }, [density])

  // ——— Reducir animaciones (fuerza las reglas de prefers-reduced-motion) ———
  const [reduceMotion, setReduceMotion] = useStoredState('netpulse-reduce-motion', false)
  useEffect(() => {
    const STYLE_ID = 'netpulse-reduce-motion-style'
    if (reduceMotion && !document.getElementById(STYLE_ID)) {
      const el = document.createElement('style')
      el.id = STYLE_ID
      el.textContent =
        'html.reduce-motion *,html.reduce-motion *::before,html.reduce-motion *::after{animation-duration:0.01ms !important;animation-iteration-count:1 !important;transition-duration:0.01ms !important;scroll-behavior:auto !important}'
      document.head.appendChild(el)
    }
    document.documentElement.classList.toggle('reduce-motion', reduceMotion)
  }, [reduceMotion])

  // ——— Datos y umbrales ———
  const [units, setUnits] = useStoredState<'mbps' | 'mbs'>('netpulse-units', 'mbps')
  const [decimalEs, setDecimalEs] = useStoredState('netpulse-decimal-es', true)
  const [refresh, setRefresh] = useStoredState<'3' | '5' | '10' | '0'>('netpulse-refresh', '3')
  const [tempT, setTempT] = useStoredState('netpulse-th-temp', 65)
  const [signalT, setSignalT] = useStoredState('netpulse-th-signal', -70)
  const [latencyT, setLatencyT] = useStoredState('netpulse-th-latency', 50)

  const patio = routers.find((r) => r.id === 'patio') ?? routers[routers.length - 1]
  const tempHot = (patio?.temp ?? 0) > tempT
  const weakCount = devices.filter((d) => d.signalDbm !== null && d.signalDbm < signalT).length
  const latencyHot = wan.latencyMs > latencyT
  const previewScore = Math.max(
    40,
    Math.min(100, 100 - (tempHot ? 8 : 0) - weakCount * 2 - (latencyHot ? 6 : 0)),
  )

  // ——— Notificaciones visuales ———
  const [navBadge, setNavBadge] = useStoredState('netpulse-notif-badge', true)
  const [pulseDots, setPulseDots] = useStoredState('netpulse-notif-pulse', true)
  const [sound, setSound] = useStoredState('netpulse-notif-sound', false)
  const [waveKey, setWaveKey] = useState(0)

  // ——— PWA install ———
  const [deferred, setDeferred] = useState<BeforeInstallPromptEvent | null>(null)
  const [installed, setInstalled] = useState<boolean>(
    () =>
      window.matchMedia('(display-mode: standalone)').matches ||
      (navigator as unknown as { standalone?: boolean }).standalone === true,
  )
  const [confettiKey, setConfettiKey] = useState(0)
  const isIOS = useMemo(
    () =>
      /iPad|iPhone|iPod/.test(navigator.userAgent) ||
      (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1),
    [],
  )
  useEffect(() => {
    const onPrompt = (e: Event) => {
      e.preventDefault()
      setDeferred(e as BeforeInstallPromptEvent)
    }
    const onInstalled = () => {
      setInstalled(true)
      setConfettiKey((k) => k + 1)
    }
    window.addEventListener('beforeinstallprompt', onPrompt)
    window.addEventListener('appinstalled', onInstalled)
    return () => {
      window.removeEventListener('beforeinstallprompt', onPrompt)
      window.removeEventListener('appinstalled', onInstalled)
    }
  }, [])

  const install = async () => {
    if (!deferred) return
    await deferred.prompt()
    const choice = await deferred.userChoice
    if (choice.outcome === 'accepted') {
      setInstalled(true)
      setConfettiKey((k) => k + 1)
    }
    setDeferred(null)
  }

  // ——— Índice de secciones (rediseño v3): estable entre renders ———
  const settingsIndexItems = useMemo(
    () => [
      { href: '#sec-personalizacion', label: t('settings.sections.personalization') },
      { href: '#sec-notificaciones', label: t('settings.sections.notifications') },
      ...(!isDemo && auth?.role === 'admin'
        ? [
            { href: '#sec-red', label: t('settings.sections.network') },
            { href: '#sec-integraciones', label: t('settings.sections.integrations') },
            { href: '#sec-cuenta', label: t('settings.sections.account') },
            { href: '#sec-admin', label: t('settings.sections.administration') },
          ]
        : []),
      { href: '#sec-acerca', label: t('settings.sections.about') },
    ],
    [t, isDemo, auth?.role],
  )

  return (
    <div className="mx-auto w-full max-w-[1180px]">
      {/* ① Page header */}
      <nav aria-label={t('common.breadcrumb')} className="mb-1 text-caption text-text-muted">
        <Link to="/" className="transition-colors hover:text-accent">{t('common.home')}</Link>
        <span className="mx-1.5">/</span>
        <span className="text-text-secondary">{t('nav.settings')}</span>
      </nav>
      <motion.h1
        initial={reduce ? false : { opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3, ease: 'easeOut' }}
        className="font-display text-h1 text-text-primary"
      >
        {t('nav.settings')}
      </motion.h1>
      <motion.p
        initial={reduce ? false : { opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: 0.3, delay: reduce ? 0 : 0.15 }}
        className="mt-0.5 text-caption text-text-muted"
      >
        {t('settings.subtitle')}
      </motion.p>

      {/* Modo demo: Ajustes en solo lectura — no se puede cambiar la configuración de la red */}
      {isDemo && (
        <motion.div
          initial={reduce ? false : { opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.3, delay: reduce ? 0 : 0.2 }}
          className="mt-4 flex items-start gap-2.5 rounded-xl border border-warn/30 bg-warn/10 px-4 py-3 text-caption leading-relaxed text-warn"
          role="status"
        >
          <Lock className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} />
          <span>{t('settings.demoReadOnly')}</span>
        </motion.div>
      )}

      {/* Índice de la página (rediseño v3): navegación rápida entre secciones */}
      <SettingsIndex items={settingsIndexItems} />
      {/* Tarjetas apiladas a ancho completo (rediseño v3): una columna, con
          etiquetas de sección. El orden visual lo controla `order-*`; los
          ids de cada SectionLabel alimentan el índice sticky y el scroll-spy. */}
      <div className="mt-4 flex flex-col gap-4 md:gap-5">
        <div className="scroll-mt-32 order-10" id="sec-personalizacion">
          <SectionLabel>{t('settings.sections.personalization')}</SectionLabel>
        </div>

        {/* El resto de etiquetas viven aquí con su order; flex las coloca
            junto a la primera tarjeta de cada sección. */}
        <div className="scroll-mt-32 order-50" id="sec-notificaciones">
          <SectionLabel>{t('settings.sections.notifications')}</SectionLabel>
        </div>
        {!isDemo && auth?.role === 'admin' && (
          <div className="scroll-mt-32 order-80" id="sec-red">
            <SectionLabel>{t('settings.sections.network')}</SectionLabel>
          </div>
        )}
        {!isDemo && auth?.role === 'admin' && (
          <div className="scroll-mt-32 order-130" id="sec-integraciones">
            <SectionLabel>{t('settings.sections.integrations')}</SectionLabel>
          </div>
        )}
        {!isDemo && auth?.role === 'admin' && (
          <div className="scroll-mt-32 order-160" id="sec-cuenta">
            <SectionLabel>{t('settings.sections.account')}</SectionLabel>
          </div>
        )}
        <div className="scroll-mt-32 order-190" id="sec-admin">
          <SectionLabel>{t('settings.sections.administration')}</SectionLabel>
        </div>
        <div className="scroll-mt-32 order-230" id="sec-acerca">
          <SectionLabel>{t('settings.sections.about')}</SectionLabel>
        </div>

        {/* ④ Datos y umbrales (primera tarjeta de Personalización) */}
        <div className="order-30">
          <Card
            title={t('settings.data.title')}
            caption={t('settings.data.caption')}
            index={2}
            reduce={reduce}
            headerSlot={
              <div className="flex flex-col items-center gap-1">
                <HealthRing
                  value={previewScore}
                  size={56}
                  stroke={5}
                  animateIn={false}
                  ariaLabel={t('settings.data.simHealthAria', { score: previewScore })}
                  center={<span className="font-mono text-[11px] font-semibold text-text-primary">{previewScore}</span>}
                />
                <span className="text-[10px] font-medium text-text-muted">{t('settings.data.simHealth')}</span>
              </div>
            }
          >
            {/* cols-2: Unidades + decimal + Refresco (izq) | umbrales (der) */}
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-4">
                <div>
                  <div className="text-sm font-medium text-text-primary">{t('settings.data.units')}</div>
                  <div className="mt-2">
                    <SegmentedControl
                      options={[
                        { value: 'mbps', label: 'Mbps' },
                        { value: 'mbs', label: 'MB/s' },
                      ]}
                      value={units}
                      onChange={(v) => {
                        setUnits(v)
                        notify()
                      }}
                      ariaLabel={t('settings.data.units')}
                    />
                  </div>
                </div>
                <div>
                  <SwitchRow
                    label={t('settings.data.decimalEs')}
                    checked={decimalEs}
                    onCheckedChange={(v) => {
                      setDecimalEs(v)
                      notify()
                    }}
                  />
                </div>
                <div>
                  <div className="text-sm font-medium text-text-primary">{t('settings.data.refresh')}</div>
                  <div className="mt-2">
                    <SegmentedControl
                      options={[
                        { value: '3', label: '3 s' },
                        { value: '5', label: '5 s' },
                        { value: '10', label: '10 s' },
                        { value: '0', label: t('settings.data.paused') },
                      ]}
                      value={refresh}
                      onChange={(v) => {
                        setRefresh(v)
                        notify()
                      }}
                      ariaLabel={t('settings.data.refresh')}
                    />
                  </div>
                  <p className="mt-2 text-caption text-text-muted">{t('settings.data.mockNote')}</p>
                </div>
              </div>

              {/* Sliders de umbrales */}
              <div className="space-y-5">
                {(
                  [
                    {
                      key: 'temp',
                      label: t('settings.data.tempLabel'),
                      value: tempT,
                      set: setTempT,
                      min: 50,
                      max: 85,
                      format: (v: number) => `${v} °C`,
                      caption: tempHot
                        ? t('settings.data.tempCaptionHot', { name: patio?.name ?? '—', temp: patio?.temp ?? 0 })
                        : t('settings.data.tempCaptionOk', { name: patio?.name ?? '—', temp: patio?.temp ?? 0 }),
                      captionHot: tempHot,
                    },
                    {
                      key: 'signal',
                      label: t('topology.weakSignal'),
                      value: signalT,
                      set: setSignalT,
                      min: -80,
                      max: -60,
                      format: (v: number) => `${v} dBm`.replace('-', '−'),
                      caption:
                        weakCount === 0
                          ? t('settings.data.signalCaptionNone')
                          : t('settings.data.signalCaption', { count: weakCount }),
                      captionHot: weakCount > 0,
                    },
                    {
                      key: 'latency',
                      label: t('settings.data.latencyLabel'),
                      value: latencyT,
                      set: setLatencyT,
                      min: 20,
                      max: 200,
                      format: (v: number) => `${v} ms`,
                      caption: latencyHot
                        ? t('settings.data.latencyCaptionHot', { ms: wan.latencyMs })
                        : t('settings.data.latencyCaptionOk', { ms: wan.latencyMs }),
                      captionHot: latencyHot,
                    },
                  ] as const
                ).map((s) => (
                  <div key={s.key}>
                    <div className="flex items-baseline justify-between gap-3">
                      <label htmlFor={`th-${s.key}`} className="text-sm font-medium text-text-primary">
                        {s.label}
                      </label>
                      <motion.span
                        key={s.value}
                        animate={reduce ? undefined : { scale: [1.15, 1] }}
                        transition={{ duration: 0.18 }}
                        className="font-mono text-mono-sm text-accent"
                      >
                        {s.format(s.value)}
                      </motion.span>
                    </div>
                    <Slider
                      id={`th-${s.key}`}
                      min={s.min}
                      max={s.max}
                      step={1}
                      value={[s.value]}
                      onValueChange={([v]) => {
                        if (v === undefined) return
                        s.set(v)
                        notify()
                      }}
                      className="mt-3"
                      aria-label={s.label}
                    />
                    <AnimatePresence mode="wait" initial={false}>
                      <motion.p
                        key={s.caption}
                        initial={reduce ? false : { opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={reduce ? undefined : { opacity: 0 }}
                        transition={{ duration: 0.15 }}
                        className={cn('mt-1.5 text-caption', s.captionHot ? 'text-warn' : 'text-text-muted')}
                      >
                        {s.caption}
                      </motion.p>
                    </AnimatePresence>
                  </div>
                ))}
              </div>
            </div>

            {/* divider: Velocidad WAN contratada (izq) | Test de velocidad periódico (der) */}
            <div className="mt-4 grid gap-6 border-t border-border pt-4 sm:grid-cols-2">
              {/* Velocidad WAN contratada (issue #151) — sub-sección del mismo ámbito */}
              <WanSpeedCard onSaved={notify} disabled={isDemo} />

              {/* Test de velocidad WAN periódico (issue #511) */}
              <SpeedtestCard onSaved={notify} disabled={isDemo} />
            </div>
          </Card>
        </div>

        {/* ② Apariencia: tema (40%) | paleta + acento + densidad (60%) */}
        <div className="order-20">
          <Card title={t('settings.appearance')} caption={t('settings.appearanceCaption')} index={0} reduce={reduce}>
            <div className="grid grid-cols-1 gap-6 xl:grid-cols-5">
              {/* Tema (40%): 3 tarjetas en fila, previews que distinguen claro/oscuro/sistema */}
              <div className="xl:col-span-2">
                <div className="text-caption font-semibold uppercase tracking-[0.06em] text-text-muted">{t('settings.theme')}</div>
                <div className="mt-2 grid grid-cols-3 gap-2" role="radiogroup" aria-label={t('nav.theme')}>
                  {THEME_OPTIONS.map((opt) => {
                    const active = mode === opt.value
                    return (
                      <button
                        key={opt.value}
                        type="button"
                        role="radio"
                        aria-checked={active}
                        onClick={() => {
                          setMode(opt.value)
                          notify()
                        }}
                        className={cn(
                          'group relative flex flex-col gap-1.5 rounded-xl border p-1.5 text-left transition-colors duration-150',
                          active ? 'border-accent bg-accent-soft' : 'border-border bg-elevated hover:border-accent/40',
                        )}
                      >
                        <span className="relative block aspect-[4/3] overflow-hidden rounded-lg">
                          <ThemePreview variant={opt.value} />
                          {active && (
                            <motion.span
                              initial={{ scale: 0 }}
                              animate={{ scale: 1 }}
                              className="absolute right-1 top-1 flex h-4 w-4 items-center justify-center rounded-full bg-accent text-canvas"
                            >
                              <Check className="h-2.5 w-2.5" strokeWidth={3} />
                            </motion.span>
                          )}
                        </span>
                        <span className="flex items-center gap-1 px-0.5 text-[11px] font-medium text-text-primary">
                          <opt.icon className={cn('h-3 w-3', active ? 'text-accent' : 'text-text-muted')} strokeWidth={1.75} />
                          {t(opt.labelKey)}
                        </span>
                      </button>
                    )
                  })}
                </div>
              </div>

              {/* Paleta (2 columnas) | densidad + animaciones (60%) */}
              <div className="xl:col-span-3">
                <div className="grid grid-cols-1 gap-6 sm:grid-cols-2">
                  {/* Paleta en 2 columnas, tarjetas altas */}
                  <div className="flex flex-col gap-6">
                    <div>
                      <div className="text-caption font-semibold uppercase tracking-[0.06em] text-text-muted">{t('settings.palette')}</div>
                      <div className="mt-2 grid grid-cols-2 gap-2">
                        {PALETTES.map((p) => {
                          const active = paletteId === p.id
                          return (
                            <motion.button
                              key={p.id}
                              type="button"
                              aria-label={t(p.labelKey)}
                              aria-pressed={active}
                              whileTap={reduce ? undefined : { scale: 0.98 }}
                              onClick={() => {
                                setPaletteId(p.id)
                                notify()
                              }}
                              className={cn(
                                'group relative flex flex-col items-center gap-2 rounded-lg border px-2 py-4 transition-all duration-150',
                                active
                                  ? 'border-accent shadow-[0_0_0_1px_rgb(var(--accent)/0.3)]'
                                  : 'border-border hover:border-border-strong',
                              )}
                            >
                              <span className="flex shrink-0 items-center gap-1">
                                <span
                                  className="h-3.5 w-3.5 rounded-full"
                                  style={{ backgroundColor: `rgb(${p.dark.accent})` }}
                                />
                                <span
                                  className="h-2.5 w-2.5 rounded-full"
                                  style={{ backgroundColor: `rgb(${p.dark.tunnel})` }}
                                />
                                <span
                                  className="ml-0.5 h-2.5 w-5 rounded"
                                  style={{ backgroundColor: `rgb(${p.dark.canvas})`, border: `1px solid rgb(${p.dark.border})` }}
                                />
                              </span>
                              <span className="text-[11px] font-medium text-text-primary">{t(p.labelKey)}</span>
                              {active && (
                                <Check className="absolute right-2 top-2 h-3 w-3 shrink-0 text-accent" strokeWidth={2.5} />
                              )}
                            </motion.button>
                          )
                        })}
                      </div>
                    </div>
                  </div>

                  {/* Densidad + animaciones (a la derecha) */}
                  <div className="flex flex-col gap-6">
                    {/* Densidad */}
                    <div>
                      <div className="text-caption font-semibold uppercase tracking-[0.06em] text-text-muted">{t('settings.density')}</div>
                      <div className="mt-2">
                        <SegmentedControl
                          options={[
                            { value: 'comoda', label: t('settings.densityComfy') },
                            { value: 'compacta', label: t('settings.densityCompact') },
                          ]}
                          value={density}
                          onChange={(v) => {
                            setDensity(v)
                            notify()
                          }}
                          ariaLabel={t('settings.density')}
                        />
                      </div>
                    </div>
                    <div className="border-t border-border pt-1">
                      <SwitchRow
                        label={t('settings.reduceMotion')}
                        caption={t('settings.reduceMotionCaption')}
                        checked={reduceMotion}
                        onCheckedChange={(v) => {
                          setReduceMotion(v)
                          notify()
                        }}
                      />
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </Card>
        </div>

        {/* Servicios visibles (checks) */}
        <div className="order-40">
          <ServicesCard reduce={reduce} onSaved={notify} disabled={isDemo} orchOn={orchOn} orchBusy={orchBusy} toggleOrchestration={toggleOrchestration} />
        </div>

        {/* ⑤ Notificaciones visuales */}
        <div className="order-60">
          <Card title={t('settings.notif.title')} caption={t('settings.notif.caption')} index={3} reduce={reduce}>
            <div className="grid grid-cols-1 gap-x-6 gap-y-0 sm:grid-cols-3">
              <SwitchRow
                label={t('settings.notif.badge')}
                caption={t('settings.notif.badgeCaption')}
                checked={navBadge}
                onCheckedChange={(v) => {
                  setNavBadge(v)
                  notify()
                }}
              />
              <SwitchRow
                label={t('settings.notif.pulse')}
                caption={t('settings.notif.pulseCaption')}
                checked={pulseDots}
                onCheckedChange={(v) => {
                  setPulseDots(v)
                  notify()
                }}
              />
              <SwitchRow
                label={t('settings.notif.sound')}
                caption={sound ? t('settings.notif.soundOn') : t('settings.notif.soundOff')}
                checked={sound}
                onCheckedChange={(v) => {
                  setSound(v)
                  notify()
                  if (v) {
                    playBeep()
                    setWaveKey((k) => k + 1)
                  }
                }}
                trailing={
                  <button
                    type="button"
                    onClick={() => {
                      playBeep()
                      setWaveKey((k) => k + 1)
                    }}
                    aria-label={t('settings.notif.testSound')}
                    title={t('settings.notif.testSound')}
                    className="relative flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-elevated text-text-secondary transition-colors hover:text-accent"
                  >
                    <Volume2 className="h-4 w-4" strokeWidth={1.75} />
                    {!reduce && (
                      <motion.span
                        key={waveKey}
                        className="pointer-events-none absolute inset-0 rounded-lg border border-accent"
                        initial={{ opacity: 0.8, scale: 1 }}
                        animate={{ opacity: 0, scale: 1.5 }}
                        transition={{ duration: 0.6, ease: 'easeOut' }}
                      />
                    )}
                  </button>
                }
              />
            </div>
            <p className="mt-3 rounded-xl bg-elevated px-3.5 py-2.5 text-caption leading-relaxed text-text-muted">
              {t('settings.notif.note')}
            </p>
          </Card>
        </div>

        {/* Telegram (#326): notificaciones directas al bot — sección Notificaciones */}
        {!isDemo && (
          <div className="order-70">
            <Card title={t('settings.telegram.title')} caption={t('settings.telegram.description')} index={4} reduce={reduce}>
              <TelegramCard onSaved={notify} bare />
            </Card>
          </div>
        )}

        {/* AdminBar canónica: Actualizaciones → Usuarios → Modo demo (derecha).
            Solo admin y modo live. Los paneles (Usuarios) se despliegan debajo;
            Routers y AdGuard son tarjetas de dominio que siguen en el grid. */}
        {!isDemo && auth?.role === 'admin' && (
          <div className="order-200">
            <div className="rounded-2xl border border-l-4 border-l-accent bg-accent/[0.03] p-4 shadow-soft md:p-5">
              <div className="flex flex-wrap items-start gap-3 sm:gap-4">
                <div className="flex h-9 shrink-0 items-center gap-2">
                  <Shield className="h-5 w-5 text-accent" strokeWidth={1.75} aria-hidden="true" />
                  <h2 className="font-display text-[15px] font-semibold text-text-primary">{t('settings.admin.title')}</h2>
                </div>
                <div className="hidden h-6 w-px bg-border sm:block" />

                {/* 1. Comprobar actualizaciones (widget inline) */}
                <UpdateCheckInline />

                {/* 2. Respaldos (desplegable) */}
                <button
                  type="button"
                  aria-expanded={adminPanel === 'backups'}
                  onClick={() => setAdminPanel(adminPanel === 'backups' ? null : 'backups')}
                  className={[
                    'inline-flex h-9 shrink-0 items-center gap-1.5 rounded-xl border px-3 text-[13px] font-medium transition-colors',
                    adminPanel === 'backups'
                      ? 'border-accent bg-accent-soft text-accent'
                      : 'border-border bg-elevated text-text-secondary hover:bg-hover hover:text-text-primary',
                  ].join(' ')}
                >
                  <Database className="h-4 w-4 shrink-0" strokeWidth={1.75} aria-hidden="true" />
                  <span className="hidden sm:inline">{t('settings.admin.backup.button')}</span>
                  <ChevronDown
                    className={`h-3.5 w-3.5 shrink-0 transition-transform ${adminPanel === 'backups' ? 'rotate-180' : ''}`}
                    aria-hidden="true"
                  />
                </button>

                {/* 3. Usuarios (desplegable) */}
                <button
                  type="button"
                  aria-expanded={adminPanel === 'users'}
                  onClick={() => setAdminPanel(adminPanel === 'users' ? null : 'users')}
                  className={[
                    'inline-flex h-9 shrink-0 items-center gap-1.5 rounded-xl border px-3 text-[13px] font-medium transition-colors',
                    adminPanel === 'users'
                      ? 'border-accent bg-accent-soft text-accent'
                      : 'border-border bg-elevated text-text-secondary hover:bg-hover hover:text-text-primary',
                  ].join(' ')}
                >
                  <Users className="h-4 w-4 shrink-0" strokeWidth={1.75} aria-hidden="true" />
                  <span className="hidden sm:inline">{t('settings.users.title')}</span>
                  <ChevronDown
                    className={`h-3.5 w-3.5 shrink-0 transition-transform ${adminPanel === 'users' ? 'rotate-180' : ''}`}
                    aria-hidden="true"
                  />
                </button>

                {/* 4. Modo demo a la derecha */}
                <div className="ml-auto">
                  <DemoCard onSaved={notify} />
                </div>
              </div>

              {adminPanel === 'backups' && (
                <div className="mt-4 border-t border-border pt-4">
                  <BackupsPanel />
                </div>
              )}
              {adminPanel === 'users' && (
                <div className="mt-4 border-t border-border pt-4">
                  <UsersManager reduce={reduce} onSaved={notify} />
                </div>
              )}
            </div>
          </div>
        )}

        {/* Historial de actualizaciones (issue #159) — solo admin y modo live;
            el updater es un mecanismo de auto-aplicación que no existe en demo */}
        {!isDemo && auth?.role === 'admin' && (
          <div className="order-210">
            <UpdateHistoryCard />
          </div>
        )}

        {/* Gestión de routers — solo admin y con backend (modo live); la API
            exige rol admin en las mutaciones (auditoría v2.4.0 §2, #7) */}
        {!isDemo && auth?.role === 'admin' && (
          <div className="order-90">
            <RoutersManager reduce={reduce} onSaved={notify} />
          </div>
        )}

        {/* Overrides manuales de topología (issue #142): etiquetar hardware
            como hipervisor/switch y asignar dispositivos a hosts. Solo admin
            y modo live; los cambios se aplican server-side en el overview. */}
        {!isDemo && auth?.role === 'admin' && (
          <div className="order-100">
            <Card
              title={t('settings.overrides.title')}
              caption={t('settings.overrides.caption')}
              index={5}
              reduce={reduce}
            >
              <TopologyOverridesManager onSaved={notify} />
            </Card>
          </div>
        )}

        {/* Dispositivos de confianza (issue #196): allowlist de MACs que no
            avisan como «desconocido» y cuyo nombre se usa como alias. */}
        {!isDemo && auth?.role === 'admin' && (
          <div className="order-110">
            <Card
              title={t('settings.knownMacs.title')}
              caption={t('settings.knownMacs.caption')}
              index={5}
              reduce={reduce}
            >
              <KnownMacsManager onSaved={notify} />
            </Card>
          </div>
        )}

        {/* Adopción de agentes (pairing token + server fingerprint) */}
        {!isDemo && auth?.role === 'admin' && (
          <div className="order-120">
            <AdoptionCard />
          </div>
        )}

        {/* AdGuard Home (GL.iNet) — solo admin, modo live y servicio visible */}
        {!isDemo && auth?.role === 'admin' && services.adguard && (
          <div className="order-140">
            <AdGuardManager reduce={reduce} onSaved={notify} />
          </div>
        )}

        {/* Proxmox VE (#561): inventario read-only del cluster para sellar
            hypervisor/ct. */}
        {!isDemo && auth?.role === 'admin' && (
          <div className="order-150">
            <ProxmoxManager reduce={reduce} onSaved={notify} />
          </div>
        )}

        {/* Mi perfil (issue #119): card canónica del shared-shell — avatar,
            nombre editable (clic → input inline ✓/✕), idioma, contraseña y
            salir en UNA línea en desktop (envuelve en móvil). */}
        <div className="order-170">
          <Card title={t('settings.session.title')} index={5} reduce={reduce}>
            <div className="flex flex-wrap items-center gap-x-5 gap-y-4 lg:flex-nowrap">
              {/* Avatar */}
              <div
                aria-hidden="true"
                className="flex h-11 w-11 shrink-0 items-center justify-center rounded-full bg-accent-soft font-display text-lg font-bold uppercase text-accent"
              >
                {(nameBaseline || auth?.user || 'N').slice(0, 1)}
              </div>

              {/* Nombre editable inline (patrón shared-shell) */}
              <div className="min-w-0 flex-1">
                {editingName ? (
                  <div className="flex items-center gap-1.5">
                    <input
                      id="session-display-name"
                      type="text"
                      value={nameDraft}
                      maxLength={40}
                      autoFocus
                      onChange={(e) => setNameDraft(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault()
                          void saveDisplayName()
                        }
                        if (e.key === 'Escape') {
                          setNameDraft(nameBaseline)
                          setEditingName(false)
                        }
                      }}
                      placeholder={auth?.user ?? ''}
                      className="h-9 w-full min-w-[140px] rounded-lg border border-border bg-elevated px-3 text-sm font-medium text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
                    />
                    <button
                      type="button"
                      onClick={() => void saveDisplayName()}
                      disabled={nameBusy || nameDraft.trim() === nameBaseline}
                      aria-label={t('settings.adguard.save')}
                      className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-accent text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
                    >
                      <Check className="h-4 w-4" strokeWidth={2.5} />
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        setNameDraft(nameBaseline)
                        setEditingName(false)
                      }}
                      aria-label={t('settings.users.cancel')}
                      className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-border text-text-muted transition-colors hover:bg-hover hover:text-text-primary"
                    >
                      <X className="h-4 w-4" strokeWidth={2} />
                    </button>
                  </div>
                ) : (
                  <button
                    type="button"
                    onClick={() => {
                      setNameDraft(nameBaseline)
                      setEditingName(true)
                    }}
                    title={t('settings.session.editName')}
                    className="group flex min-w-0 items-center gap-1.5 text-left"
                  >
                    <span className="truncate text-base font-semibold leading-tight text-text-primary">
                      {nameBaseline || auth?.user || '—'}
                    </span>
                    <Pencil
                      className="h-3.5 w-3.5 shrink-0 text-text-muted opacity-0 transition-opacity duration-150 group-hover:opacity-100"
                      strokeWidth={1.75}
                      aria-hidden="true"
                    />
                  </button>
                )}
                {nameError && (
                  <p role="alert" className="mt-1.5 text-caption text-danger">
                    {nameError}
                  </p>
                )}
              </div>

              {/* Idioma (única fuente de verdad; movido de Apariencia, #119) */}
              <select
                aria-label={t('settings.language')}
                value={lang}
                onChange={(e) => {
                  setLanguage(e.target.value as 'auto' | 'es' | 'en')
                  notify()
                }}
                className="h-9 shrink-0 rounded-lg border border-border bg-elevated px-2.5 text-sm text-text-primary"
              >
                <option value="auto">🌐 {t('settings.languageAuto')}</option>
                <option value="es">🇪🇸 Español</option>
                <option value="en">🇬🇧 English</option>
              </select>

              {/* Contraseña */}
              {!isDemo && (
                <button
                  type="button"
                  aria-expanded={showPwdForm}
                  onClick={() => setShowPwdForm((v) => !v)}
                  className="flex h-9 shrink-0 items-center gap-2 rounded-lg border border-border bg-elevated px-3 text-sm font-medium text-text-primary transition-colors duration-150 hover:bg-hover"
                >
                  <KeyRound className="h-4 w-4" strokeWidth={1.75} />
                  <span className="hidden sm:inline">{t('settings.session.changePassword')}</span>
                </button>
              )}

              {/* Salir (derecha, destructivo) */}
              <button
                type="button"
                onClick={() => {
                  if (isDemo) {
                    exitDemo()
                    return
                  }
                  void fetch('/api/auth/logout', { method: 'POST' })
                    .catch(() => undefined)
                    .finally(() => {
                      window.dispatchEvent(new Event('netpulse-unauthorized'))
                      window.location.assign('/login')
                    })
                }}
                className="ml-auto flex h-9 shrink-0 items-center gap-2 rounded-lg border border-danger/30 bg-danger/10 px-3 text-sm font-medium text-danger transition-colors duration-150 hover:bg-danger/15"
              >
                <LogOut className="h-4 w-4" strokeWidth={1.75} />
                <span className="hidden sm:inline">{isDemo ? t('demo.exit') : t('settings.session.logout')}</span>
              </button>
            </div>

            {showPwdForm && !isDemo && (
              <form
                className="mt-4 flex max-w-md flex-col gap-3 border-t border-border pt-4"
                onSubmit={(e) => {
                  e.preventDefault()
                  void submitOwnPassword()
                }}
              >
                <input
                  type="password"
                  autoComplete="current-password"
                  value={pwCurrent}
                  onChange={(e) => setPwCurrent(e.target.value)}
                  placeholder={t('settings.session.pwCurrent')}
                  aria-label={t('settings.session.pwCurrent')}
                  className="h-10 rounded-lg border border-border bg-elevated px-3 text-sm text-text-primary"
                />
                <input
                  type="password"
                  autoComplete="new-password"
                  value={pwNew}
                  onChange={(e) => setPwNew(e.target.value)}
                  placeholder={t('settings.session.pwNew')}
                  aria-label={t('settings.session.pwNew')}
                  className="h-10 rounded-lg border border-border bg-elevated px-3 text-sm text-text-primary"
                />
                <p className="-mt-1 text-caption text-text-muted">{t('settings.session.pwMinLength')}</p>
                <input
                  type="password"
                  autoComplete="new-password"
                  value={pwConfirm}
                  onChange={(e) => setPwConfirm(e.target.value)}
                  placeholder={t('settings.session.pwConfirm')}
                  aria-label={t('settings.session.pwConfirm')}
                  className="h-10 rounded-lg border border-border bg-elevated px-3 text-sm text-text-primary"
                />
                {pwError && (
                  <p role="alert" className="rounded-lg bg-danger/10 px-3 py-2 text-caption text-danger">
                    {pwError}
                  </p>
                )}
                {pwChanged && (
                  <p role="status" className="rounded-lg bg-ok/10 px-3 py-2 text-caption text-ok">
                    {t('settings.session.pwChanged')}
                  </p>
                )}
                <button
                  type="submit"
                  disabled={pwBusy || !pwCurrent || pwNew.length < 10 || pwNew !== pwConfirm}
                  className="flex h-10 items-center justify-center rounded-lg bg-accent px-4 text-sm font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
                >
                  {pwBusy ? t('settings.session.pwSubmitting') : t('settings.session.pwSubmit')}
                </button>
                {pwNew.length > 0 && pwConfirm.length > 0 && pwNew !== pwConfirm && (
                  <p role="alert" className="text-caption text-danger">{t('settings.session.pwMismatch')}</p>
                )}
              </form>
            )}
          </Card>
        </div>

        {/* API Tokens (#330): bearer tokens con scopes para integraciones */}
        {!isDemo && (
          <div className="order-180">
            <Card title={t('tokens.title')} caption={t('tokens.caption')} index={6} reduce={reduce}>
              <TokensManager />
            </Card>
          </div>
        )}

        {/* ⑥ Acerca de */}
        <div className="order-230">
          <Card title={t('settings.about.title')} index={6} reduce={reduce}>
            {/* Dos columnas como el mockup: izq descripción+enlaces | der Sistema */}
            <div className="grid gap-6 md:grid-cols-2">
              <div>
                <div className="flex items-start gap-4">
                  <motion.img
                    src="/logo.svg"
                    alt=""
                    className="h-12 w-12 shrink-0"
                    initial={reduce ? false : { opacity: 0, scale: 0.85 }}
                    animate={{ opacity: 1, scale: 1 }}
                    transition={{ duration: 0.5, ease: 'easeOut' }}
                  />
                  <div>
                    <div className="flex flex-wrap items-baseline gap-x-2">
                      <span className="font-display text-h2 font-bold text-text-primary">NetPulse</span>
                      <span className="font-mono text-caption text-text-muted">v{pkg.version}</span>
                    </div>
                    <p className="mt-1.5 text-sm leading-relaxed text-text-secondary">
                      {t('settings.about.desc')}
                    </p>
                  </div>
                </div>

                <div className="mt-4 grid grid-cols-1 gap-2 sm:grid-cols-2">
                  {[
                    { icon: Github, label: t('settings.about.code'), href: 'https://github.com/gnacho/netpulse' },
                    { icon: FileText, label: t('settings.about.changelog'), href: 'https://netpulse.cloudless.club' },
                    { icon: Heart, label: t('settings.about.madeAtHome'), href: 'https://ko-fi.com/gnacho' },
                    { icon: ShieldCheck, label: t('settings.about.privacy'), href: 'https://cloudless.club' },
                  ].map((item, i) => {
                    const cls = "flex items-center gap-2.5 rounded-xl border border-border px-3.5 py-2.5 text-sm text-text-secondary transition-colors duration-150 hover:border-accent/40 hover:text-accent"
                    return (
                      <motion.div
                        key={item.label}
                        initial={reduce ? false : { opacity: 0, y: 8 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ duration: 0.25, ease: 'easeOut', delay: reduce ? 0 : 0.2 + i * 0.06 }}
                      >
                        {item.href ? (
                          <a href={item.href} target="_blank" rel="noreferrer" className={cls}>
                            <item.icon className="h-4 w-4 shrink-0" strokeWidth={1.75} />
                            <span className="leading-snug">{item.label}</span>
                          </a>
                        ) : (
                          <div className={cls}>
                            <item.icon className="h-4 w-4 shrink-0" strokeWidth={1.75} />
                            <span className="leading-snug">{item.label}</span>
                          </div>
                        )}
                      </motion.div>
                    )
                  })}
                </div>

                {/* Push + PWA compactos (issue #156) */}
                <div className="mt-4 flex flex-wrap items-center gap-2">
                  <PushNotificationsCard reduce={reduce} onSaved={notify} compact />
                  {!installed && (
                    <Confetti burstKey={confettiKey} reduce={reduce} />
                  )}
                  {installed ? (
                    <span className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-ok/10 px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wider text-ok">
                      <BadgeCheck className="h-3.5 w-3.5" strokeWidth={2} />
                      {t('settings.pwa.installed')}
                    </span>
                  ) : isIOS ? (
                    <span className="shrink-0 text-caption text-text-muted">{t('settings.pwa.iosHow')}</span>
                  ) : deferred ? (
                    <button
                      type="button"
                      onClick={() => void install()}
                      className="flex shrink-0 items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
                    >
                      <Download className="h-3.5 w-3.5" strokeWidth={2} />
                      {t('settings.pwa.install')}
                    </button>
                  ) : null}
                </div>
              </div>

              {/* Derecha: Sistema */}
              <div>
                <SystemInfoBlock bare />
              </div>
            </div>
          </Card>
        </div>
      </div>

      {/* Toast de confirmación (bottom-center; sobre la tab bar en móvil) */}
      <AnimatePresence>
        {toastKey !== null && (
          <motion.div
            key={toastKey}
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 8 }}
            transition={{ duration: 0.2, ease: 'easeOut' }}
            role="status"
            className="fixed bottom-20 left-1/2 z-50 -translate-x-1/2 md:bottom-6"
          >
            <span className="inline-flex items-center gap-2 rounded-full border border-border bg-elevated px-4 py-2 text-xs font-medium text-text-primary shadow-lg">
              <Check className="h-3.5 w-3.5 text-ok" strokeWidth={2} />
              {t('settings.saved')}
            </span>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
