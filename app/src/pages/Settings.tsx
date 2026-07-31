import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import {
  BadgeCheck,
  Check,
  Copy,
  Download,
  FileText,
  Github,
  Heart,
  KeyRound,
  LogOut,
  MonitorSmartphone,
  Moon,
  Plus,
  PlusSquare,
  Radar,
  Router as RouterIcon,
  Share,
  ShieldCheck,
  Sun,
  Trash2,
  Volume2,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { HealthRing } from '@/components/HealthRing'
import { SegmentedControl } from '@/components/SegmentedControl'
import { Slider } from '@/components/ui/slider'
import { Switch } from '@/components/ui/switch'
import { useNetPulse } from '@/data/DataProvider'
import { cn } from '@/lib/utils'

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
}

function SwitchRow({ icon: Icon, label, caption, checked, onCheckedChange, trailing }: SwitchRowProps) {
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
        <Switch checked={checked} onCheckedChange={onCheckedChange} aria-label={label} />
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// ② Apariencia — previews de tema
// ---------------------------------------------------------------------------

type ThemeMode = 'dark' | 'light' | 'system'

function ThemePreview({ variant }: { variant: ThemeMode }) {
  const dark = (
    <div className="flex h-full flex-col gap-1 bg-[#070B12] p-1.5">
      <div className="h-1.5 w-2/3 rounded-full bg-white/15" />
      <div className="flex flex-1 gap-1">
        <div className="w-1/4 rounded-sm bg-white/10" />
        <div className="flex flex-1 flex-col gap-1">
          <div className="h-1/2 rounded-sm bg-[#22D3EE]/60" />
          <div className="flex-1 rounded-sm bg-white/10" />
        </div>
      </div>
    </div>
  )
  const light = (
    <div className="flex h-full flex-col gap-1 bg-[#F3F5F9] p-1.5">
      <div className="h-1.5 w-2/3 rounded-full bg-black/15" />
      <div className="flex flex-1 gap-1">
        <div className="w-1/4 rounded-sm bg-black/10" />
        <div className="flex flex-1 flex-col gap-1">
          <div className="h-1/2 rounded-sm bg-[#0891B2]/60" />
          <div className="flex-1 rounded-sm bg-black/10" />
        </div>
      </div>
    </div>
  )
  if (variant === 'dark') return dark
  if (variant === 'light') return light
  return (
    <div className="grid h-full grid-cols-2">
      {dark}
      {light}
    </div>
  )
}

const THEME_OPTIONS: { value: ThemeMode; labelKey: string; icon: LucideIcon }[] = [
  { value: 'dark', labelKey: 'settings.themeDark', icon: Moon },
  { value: 'light', labelKey: 'settings.themeLight', icon: Sun },
  { value: 'system', labelKey: 'settings.themeSystem', icon: MonitorSmartphone },
]

const ACCENTS = [
  { id: 'cyan', dark: '34 211 238', light: '8 145 178', swatch: '#22D3EE', labelKey: 'settings.accentCyan' },
  { id: 'violet', dark: '167 139 250', light: '124 58 237', swatch: '#A78BFA', labelKey: 'settings.accentViolet' },
  { id: 'emerald', dark: '52 211 153', light: '5 150 105', swatch: '#34D399', labelKey: 'settings.accentEmerald' },
  { id: 'amber', dark: '251 191 36', light: '217 119 6', swatch: '#FBBF24', labelKey: 'settings.accentAmber' },
] as const

type AccentId = (typeof ACCENTS)[number]['id']

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

interface ConfigRouter {
  id: string
  name: string | null
  host: string
  type: 'glinet' | 'openwrt'
  is_gateway: boolean
}

interface DiscoverCandidate {
  host: string
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
  const [type, setType] = useState<'glinet' | 'openwrt'>('openwrt')
  const [gateway, setGateway] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [pubkey, setPubkey] = useState<{ publicKey: string; fingerprint: string } | null>(null)
  const [copied, setCopied] = useState(false)
  const [scanning, setScanning] = useState(false)
  const [candidates, setCandidates] = useState<DiscoverCandidate[] | null>(null)

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
    try {
      await navigator.clipboard.writeText(pubkey.publicKey)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      /* portapapeles no disponible */
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
      const res = await fetch('/api/config/routers', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          host: cand.host,
          name: cand.model || undefined,
          type: /GL[.-]?iNet|GL-[A-Z]/i.test(cand.model || '') ? 'glinet' : 'openwrt',
          gateway: cand.isGateway,
        }),
      })
      if (!res.ok && res.status !== 409) throw new Error(`HTTP ${res.status}`)
      setCandidates((prev) => prev?.map((x) => (x.host === cand.host ? { ...x, configured: true } : x)) ?? prev)
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
      await load()
      refresh()
      onSaved()
    } catch {
      setError(t('settings.routers.errorGeneric'))
    } finally {
      setSubmitting(false)
    }
  }

  const remove = async (r: ConfigRouter) => {
    if (!window.confirm(t('settings.routers.deleteConfirm', { name: r.name || r.host }))) return
    try {
      const res = await fetch(`/api/config/routers/${encodeURIComponent(r.id)}`, { method: 'DELETE' })
      if (!res.ok && res.status !== 204) throw new Error(`HTTP ${res.status}`)
      await load()
      refresh()
    } catch {
      setError(t('settings.routers.errorGeneric'))
    }
  }

  return (
    <Card title={t('settings.routers.title')} caption={t('settings.routers.caption')} index={4} reduce={reduce}>
      {/* Lista configurada */}
      {loading ? (
        <p className="text-caption text-text-muted">…</p>
      ) : list.length === 0 ? (
        <p className="rounded-xl bg-elevated px-3.5 py-2.5 text-caption leading-relaxed text-text-muted">
          {t('settings.routers.empty')}
        </p>
      ) : (
        <ul className="flex flex-col gap-2">
          {list.map((r) => (
            <li
              key={r.id}
              className="flex items-center gap-3 rounded-xl border border-border bg-elevated px-3.5 py-2.5"
            >
              <RouterIcon className="h-4 w-4 shrink-0 text-text-muted" strokeWidth={1.75} />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate text-sm font-medium text-text-primary">{r.name || r.host}</span>
                  {r.is_gateway && (
                    <span className="rounded-full bg-accent-soft px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-accent">
                      {t('settings.routers.gatewayBadge')}
                    </span>
                  )}
                </div>
                <div className="font-mono text-caption text-text-muted">
                  {r.host} · {r.type === 'glinet' ? 'GL.iNet' : 'OpenWrt'}
                </div>
              </div>
              <button
                type="button"
                onClick={() => void remove(r)}
                aria-label={t('settings.routers.delete')}
                className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-border text-text-muted transition-colors duration-150 hover:border-danger/40 hover:text-danger"
              >
                <Trash2 className="h-4 w-4" strokeWidth={1.75} />
              </button>
            </li>
          ))}
        </ul>
      )}

      {/* Descubrimiento en la LAN */}
      <div className="mt-4 border-t border-border pt-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-caption text-text-muted">{t('settings.routers.discoverCaption')}</p>
          <button
            type="button"
            onClick={() => void discover()}
            disabled={scanning}
            className="flex items-center gap-2 rounded-lg border border-border bg-elevated px-3.5 py-2 text-sm font-medium text-text-secondary transition-colors duration-150 hover:border-accent/40 hover:text-accent disabled:opacity-50"
          >
            <Radar className={cn('h-4 w-4', scanning && 'animate-pulse')} strokeWidth={1.75} />
            {scanning ? t('settings.routers.discovering') : t('settings.routers.discover')}
          </button>
        </div>
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
                      {cand.model || cand.host}
                    </span>
                    {cand.isGateway && (
                      <span className="rounded-full bg-accent-soft px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-accent">
                        {t('settings.routers.gatewayBadge')}
                      </span>
                    )}
                  </div>
                  <div className="font-mono text-caption text-text-muted">{cand.host}</div>
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
      </div>

      {/* Formulario de alta */}
      <form onSubmit={(e) => void add(e)} className="mt-4 border-t border-border pt-4">
        <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
          <input
            type="text"
            required
            value={host}
            onChange={(e) => setHost(e.target.value)}
            placeholder={t('settings.routers.host')}
            aria-label={t('settings.routers.host')}
            className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('settings.routers.name')}
            aria-label={t('settings.routers.name')}
            className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none"
          />
        </div>
        <div className="mt-2.5 flex flex-wrap items-center gap-3">
          <SegmentedControl
            options={[
              { value: 'openwrt', label: 'OpenWrt' },
              { value: 'glinet', label: 'GL.iNet' },
            ]}
            value={type}
            onChange={(v) => setType(v as 'glinet' | 'openwrt')}
            ariaLabel={t('settings.routers.type')}
          />
          <label className="flex cursor-pointer items-center gap-2 text-sm text-text-secondary">
            <Switch checked={gateway} onCheckedChange={setGateway} />
            {t('settings.routers.gateway')}
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
              {copied ? t('settings.routers.copied') : t('settings.routers.copy')}
            </button>
          </div>
        )}
      </div>
    </Card>
  )
}


// ---------------------------------------------------------------------------
// Página Ajustes `/settings` (settings.md)
// ---------------------------------------------------------------------------

export default function Settings() {
  const { t, i18n } = useTranslation()
  const reduce = useReducedMotion() ?? false
  const { devices, routers, wan, isDemo } = useNetPulse()

  // ——— Idioma ('auto' sigue al navegador; persistido como string plano) ———
  const [lang, setLang] = useState<'auto' | 'es' | 'en'>(() => {
    const raw = localStorage.getItem('netpulse-lang')
    return raw === 'es' || raw === 'en' ? raw : 'auto'
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
  }, [i18n])

  // ——— Toast "Preferencias guardadas" ———
  const [toastKey, setToastKey] = useState<number | null>(null)
  const toastTimer = useRef<number | undefined>(undefined)
  const notify = useCallback(() => {
    window.clearTimeout(toastTimer.current)
    setToastKey(Date.now())
    toastTimer.current = window.setTimeout(() => setToastKey(null), 1800)
  }, [])
  useEffect(() => () => window.clearTimeout(toastTimer.current), [])

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

  // ——— Acento en vivo (--accent) ———
  const [accentId, setAccentId] = useStoredState<AccentId>('netpulse-accent', 'cyan')
  useEffect(() => {
    const a = ACCENTS.find((x) => x.id === accentId) ?? ACCENTS[0]
    document.documentElement.style.setProperty('--accent', resolvedLight ? a.light : a.dark)
  }, [accentId, resolvedLight])

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
  const [showManualHelp, setShowManualHelp] = useState(false)
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
    if (!deferred) {
      setShowManualHelp(true)
      return
    }
    await deferred.prompt()
    const choice = await deferred.userChoice
    if (choice.outcome === 'accepted') {
      setInstalled(true)
      setConfettiKey((k) => k + 1)
    }
    setDeferred(null)
  }

  return (
    <div className="mx-auto w-full max-w-[1100px]">
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

      <div className="mt-5 grid grid-cols-1 gap-4 md:gap-5 lg:grid-cols-12">
        {/* ② Apariencia */}
        <div className="lg:col-span-7">
          <Card title={t('settings.appearance')} caption={t('settings.appearanceCaption')} index={0} reduce={reduce}>
            {/* Tema: 3 cards visuales */}
            <div className="grid grid-cols-3 gap-3" role="radiogroup" aria-label={t('nav.theme')}>
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
                      'group relative flex flex-col gap-2 rounded-xl border p-2 text-left transition-colors duration-150',
                      active ? 'border-accent bg-accent-soft' : 'border-border bg-elevated hover:border-accent/40',
                    )}
                  >
                    <span className="relative block h-20 overflow-hidden rounded-lg sm:h-24">
                      <ThemePreview variant={opt.value} />
                      <AnimatePresence>
                        {active && (
                          <motion.span
                            initial={{ scale: 0 }}
                            animate={{ scale: 1 }}
                            exit={{ scale: 0 }}
                            transition={{ type: 'spring', stiffness: 500, damping: 25 }}
                            className="absolute right-1.5 top-1.5 flex h-5 w-5 items-center justify-center rounded-full bg-accent text-canvas"
                          >
                            <Check className="h-3 w-3" strokeWidth={2.5} />
                          </motion.span>
                        )}
                      </AnimatePresence>
                    </span>
                    <span className="flex items-center gap-1.5 px-0.5 text-xs font-medium text-text-primary">
                      <opt.icon className={cn('h-3.5 w-3.5', active ? 'text-accent' : 'text-text-muted')} strokeWidth={1.75} />
                      {t(opt.labelKey)}
                    </span>
                  </button>
                )
              })}
            </div>

            {/* Acento */}
            <div className="mt-5">
              <div className="text-caption font-semibold uppercase tracking-[0.06em] text-text-muted">{t('settings.accent')}</div>
              <div className="mt-2.5 flex items-center gap-3">
                {ACCENTS.map((a) => {
                  const active = accentId === a.id
                  return (
                    <motion.button
                      key={a.id}
                      type="button"
                      aria-label={t('settings.accentAria', { label: t(a.labelKey) })}
                      aria-pressed={active}
                      whileTap={reduce ? undefined : { scale: 0.8 }}
                      onClick={() => {
                        setAccentId(a.id)
                        notify()
                      }}
                      className={cn(
                        'flex h-9 w-9 items-center justify-center rounded-full transition-shadow duration-150',
                        active ? 'ring-2 ring-accent ring-offset-2 ring-offset-surface' : 'hover:ring-2 hover:ring-border-strong hover:ring-offset-2 hover:ring-offset-surface',
                      )}
                      style={{ backgroundColor: a.swatch }}
                    >
                      {active && <Check className="h-4 w-4 text-[#070B12]" strokeWidth={2.5} />}
                    </motion.button>
                  )
                })}
                <span className="ml-1 text-caption text-text-muted">{t('settings.accentCaption')}</span>
              </div>
            </div>

            {/* Densidad + idioma + animaciones */}
            <div className="mt-5 flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
              <div>
                <div className="text-sm font-medium text-text-primary">{t('settings.density')}</div>
                <div className="text-caption text-text-muted">{t('settings.densityCaption')}</div>
              </div>
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
            {/* Idioma */}
            <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
              <div>
                <div className="text-sm font-medium text-text-primary">{t('settings.language')}</div>
                <div className="text-caption text-text-muted">{t('settings.languageCaption')}</div>
              </div>
              <SegmentedControl
                options={[
                  { value: 'auto', label: t('settings.languageAuto') },
                  { value: 'es', label: 'Español' },
                  { value: 'en', label: 'English' },
                ]}
                value={lang}
                onChange={(v) => {
                  setLanguage(v)
                  notify()
                }}
                ariaLabel={t('settings.language')}
              />
            </div>
            <div className="border-t border-border pt-2">
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
          </Card>
        </div>

        {/* ③ Instalar app PWA */}
        <div className="lg:col-span-5">
          <motion.div
            initial={reduce ? false : { opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.3, ease: 'easeOut', delay: reduce ? 0 : 0.14 }}
            className="relative h-full overflow-hidden rounded-2xl p-[1px]"
          >
            {/* Borde gradiente fluido cyan→violet */}
            <motion.div
              aria-hidden="true"
              className="absolute inset-0 opacity-70"
              style={{
                backgroundImage:
                  'linear-gradient(120deg, rgb(var(--accent) / 0.9) 0%, rgb(var(--tunnel) / 0.9) 50%, rgb(var(--accent) / 0.9) 100%)',
                backgroundSize: '200% 200%',
              }}
              animate={reduce ? undefined : { backgroundPosition: ['0% 50%', '100% 50%', '0% 50%'] }}
              transition={{ duration: 8, repeat: Infinity, ease: 'linear' }}
            />
            <div className="relative flex h-full flex-col rounded-2xl bg-surface p-5">
              <Confetti burstKey={confettiKey} reduce={reduce} />
              <div className="flex items-center gap-3">
                <img src="/icon-192.png" alt="" className="h-16 w-16 rounded-2xl ring-1 ring-border" />
                <div>
                  <h2 className="font-display text-h2 text-text-primary">{t('settings.pwa.title')}</h2>
                  <p className="mt-0.5 text-caption text-text-muted">{t('settings.pwa.caption')}</p>
                </div>
              </div>
              <p className="mt-3 text-sm text-text-secondary">
                {t('settings.pwa.desc')}
              </p>

              <div className="mt-4">
                {installed ? (
                  <div className="flex items-center gap-2.5">
                    <span className="inline-flex items-center gap-1.5 rounded-full bg-ok/10 px-3 py-1.5 text-caption font-semibold uppercase tracking-[0.06em] text-ok">
                      <BadgeCheck className="h-4 w-4" strokeWidth={1.75} />
                      {t('settings.pwa.installed')}
                    </span>
                    <span className="font-mono text-caption text-text-muted">{t('settings.pwa.shellVersion')}</span>
                  </div>
                ) : isIOS ? (
                  <div className="rounded-xl border border-border bg-elevated p-3.5">
                    <div className="text-sm font-medium text-text-primary">{t('settings.pwa.iosHow')}</div>
                    <ol className="mt-2 space-y-2 text-sm text-text-secondary">
                      <li className="flex items-center gap-2.5">
                        <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-surface text-accent">
                          <Share className="h-4 w-4" strokeWidth={1.75} />
                        </span>
                        {t('settings.pwa.iosStep1Pre')} <strong className="font-semibold text-text-primary">{t('settings.pwa.iosStep1Strong')}</strong> {t('settings.pwa.iosStep1Post')}
                      </li>
                      <li className="flex items-center gap-2.5">
                        <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-surface text-accent">
                          <PlusSquare className="h-4 w-4" strokeWidth={1.75} />
                        </span>
                        {t('settings.pwa.iosStep2Pre')} <strong className="font-semibold text-text-primary">{t('settings.pwa.iosStep2Strong')}</strong>
                      </li>
                    </ol>
                  </div>
                ) : (
                  <>
                    <motion.button
                      type="button"
                      onClick={() => void install()}
                      animate={
                        reduce
                          ? undefined
                          : {
                              boxShadow: [
                                '0 0 0 0 rgb(var(--accent) / 0.35)',
                                '0 0 22px 2px rgb(var(--accent) / 0.12)',
                                '0 0 0 0 rgb(var(--accent) / 0.35)',
                              ],
                            }
                      }
                      transition={{ duration: 2.2, repeat: Infinity, ease: 'easeInOut' }}
                      className="inline-flex h-10 items-center gap-2 rounded-lg bg-accent px-4 text-sm font-semibold text-canvas transition-colors hover:bg-accent/90"
                    >
                      <Download className="h-4 w-4" strokeWidth={1.75} />
                      {t('settings.pwa.install')}
                    </motion.button>
                    {showManualHelp && !deferred && (
                      <p className="mt-2.5 text-caption leading-relaxed text-text-muted">
                        {t('settings.pwa.manualPre')}{' '}
                        <strong className="text-text-secondary">{t('settings.pwa.manualStrong')}</strong>.
                      </p>
                    )}
                  </>
                )}
              </div>

              <div className="mt-auto space-y-1.5 pt-4">
                {[t('settings.pwa.specOffline'), t('settings.pwa.specUpdates'), '2,1 MB'].map((spec) => (
                  <div key={spec} className="flex items-center gap-2 font-mono text-caption text-text-muted">
                    <span className="h-1 w-1 rounded-full bg-accent" />
                    {spec}
                  </div>
                ))}
              </div>
            </div>
          </motion.div>
        </div>

        {/* ④ Datos y umbrales */}
        <div className="lg:col-span-7">
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
            {/* Unidades + refresco */}
            <div className="grid gap-4 sm:grid-cols-2">
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
                <div className="mt-2">
                  <SwitchRow
                    label={t('settings.data.decimalEs')}
                    checked={decimalEs}
                    onCheckedChange={(v) => {
                      setDecimalEs(v)
                      notify()
                    }}
                  />
                </div>
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
            <div className="mt-4 space-y-5 border-t border-border pt-4">
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
          </Card>
        </div>

        {/* ⑤ Notificaciones visuales */}
        <div className="lg:col-span-5">
          <Card title={t('settings.notif.title')} caption={t('settings.notif.caption')} index={3} reduce={reduce}>
            <div className="divide-y divide-border">
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
                  <span className="relative flex h-8 w-8 items-center justify-center rounded-lg bg-elevated text-text-secondary">
                    <Volume2 className="h-4 w-4" strokeWidth={1.75} />
                    {sound && !reduce && (
                      <motion.span
                        key={waveKey}
                        className="absolute inset-0 rounded-lg border border-accent"
                        initial={{ opacity: 0.8, scale: 1 }}
                        animate={{ opacity: 0, scale: 1.5 }}
                        transition={{ duration: 0.6, ease: 'easeOut' }}
                      />
                    )}
                  </span>
                }
              />
            </div>
            <p className="mt-3 rounded-xl bg-elevated px-3.5 py-2.5 text-caption leading-relaxed text-text-muted">
              {t('settings.notif.note')}
            </p>
          </Card>
        </div>

        {/* Gestión de routers — solo con backend (modo live) */}
        {!isDemo && (
          <div className="lg:col-span-12">
            <RoutersManager reduce={reduce} onSaved={notify} />
          </div>
        )}

        {/* Cerrar sesión — solo con backend (modo live), webapp-stack §Logout */}
        {!isDemo && (
          <div className="lg:col-span-12">
            <Card title={t('settings.session.title')} index={5} reduce={reduce}>
              <div className="flex flex-wrap items-center justify-between gap-4">
                <div>
                  <p className="text-sm font-medium text-text-primary">{t('settings.session.active')}</p>
                  <p className="mt-0.5 text-caption text-text-muted">
                    {t('settings.session.activeCaption')}
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => {
                    void fetch('/api/auth/logout', { method: 'POST' })
                      .catch(() => undefined)
                      .finally(() => window.location.assign('/login'))
                  }}
                  className="flex items-center gap-2 rounded-lg border border-danger/30 bg-danger/10 px-4 py-2 text-sm font-medium text-danger transition-colors duration-150 hover:bg-danger/15"
                >
                  <LogOut className="h-4 w-4" strokeWidth={1.75} />
                  {t('settings.session.logout')}
                </button>
              </div>
            </Card>
          </div>
        )}

        {/* ⑥ Acerca de */}
        <div className="lg:col-span-12">
          <Card title={t('settings.about.title')} index={6} reduce={reduce}>
            <div className="grid gap-6 md:grid-cols-2">
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
                    <span className="font-mono text-caption text-text-muted">{t('settings.about.versionNote')}</span>
                  </div>
                  <p className="mt-1.5 text-sm leading-relaxed text-text-secondary">
                    {t('settings.about.desc')}
                  </p>
                </div>
              </div>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                {[
                  { icon: Github, label: t('settings.about.code') },
                  { icon: FileText, label: t('settings.about.changelog') },
                  { icon: Heart, label: t('settings.about.madeAtHome') },
                  { icon: ShieldCheck, label: t('settings.about.privacy') },
                ].map((item, i) => (
                  <motion.div
                    key={item.label}
                    initial={reduce ? false : { opacity: 0, y: 8 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.25, ease: 'easeOut', delay: reduce ? 0 : 0.2 + i * 0.06 }}
                    className="flex items-center gap-2.5 rounded-xl border border-border px-3.5 py-2.5 text-sm text-text-secondary transition-colors duration-150 hover:border-accent/40 hover:text-accent"
                  >
                    <item.icon className="h-4 w-4 shrink-0" strokeWidth={1.75} />
                    <span className="leading-snug">{item.label}</span>
                  </motion.div>
                ))}
              </div>
            </div>
            <p className="mt-5 border-t border-border pt-3 font-mono text-caption text-text-muted">
              {t('settings.about.footer')}
            </p>
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
