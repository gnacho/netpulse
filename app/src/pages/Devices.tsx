import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useSearchParams } from 'react-router'
import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  Check,
  ChevronDown,
  Filter,
  LayoutGrid,
  List,
  Pencil,
  Search,
  ShieldCheck,
  SignalLow,
  Sparkles,
  Wifi,
  X,
} from 'lucide-react'
import { DEVICE_ICONS, DeviceRow, SignalIcon } from '@/components/DeviceRow'
import { CountUp } from '@/components/CountUp'
import { EmptyState } from '@/components/EmptyState'
import { MetricBar } from '@/components/MetricBar'
import { SegmentedControl } from '@/components/SegmentedControl'
import { Sparkline } from '@/components/Sparkline'
import { StatusPill } from '@/components/StatusPill'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { dhcpLease, numLocale } from '@/i18n'
import { fmtEs, signalLevel } from '@/data/mock'
import { useNetPulse } from '@/data/DataProvider'
import { useDashboard } from '@/hooks/useDashboard'
import { cn } from '@/lib/utils'
import type { ClientDevice, FilterGroup } from '@/pages/devices-data'
import { buildClientDevices, GROUP_ORDER } from '@/pages/devices-data'

// ---------------------------------------------------------------------------
// Rename local (única escritura de la app — devices.md §④)
// ---------------------------------------------------------------------------

const NAMES_KEY = 'netpulse-device-names'

function loadRenames(): Record<string, string> {
  try {
    const raw = localStorage.getItem(NAMES_KEY)
    return raw ? (JSON.parse(raw) as Record<string, string>) : {}
  } catch {
    return {}
  }
}

function useRenames(): [Record<string, string>, (id: string, name: string | null) => void] {
  const [renames, setRenames] = useState<Record<string, string>>(loadRenames)
  const setName = useCallback((id: string, name: string | null) => {
    setRenames((prev) => {
      const next = { ...prev }
      if (name && name.trim()) next[id] = name.trim()
      else delete next[id]
      try {
        localStorage.setItem(NAMES_KEY, JSON.stringify(next))
      } catch {
        /* almacenamiento no disponible: el rename vive solo en memoria */
      }
      return next
    })
  }, [])
  return [renames, setName]
}

// ---------------------------------------------------------------------------
// Constantes de la página
// ---------------------------------------------------------------------------

type BandFilter = 'all' | '5 GHz' | '2.4 GHz' | 'cable'
type SortKey = 'name' | 'ip' | 'router' | 'band' | 'signal' | 'traffic'

/** IP a número para ordenar (ipv4 "a.b.c.d" → entero). */
function ipNum(ip: string): number {
  return ip.split('.').reduce((acc, o) => acc * 256 + (parseInt(o, 10) || 0), 0)
}

/** Cabecera de columna ordenable. */
function SortHeader({
  label,
  k,
  sort,
  onSort,
}: {
  label: string
  k: SortKey
  sort: { key: SortKey; dir: 1 | -1 } | null
  onSort: (k: SortKey) => void
}) {
  const active = sort?.key === k
  const Icon = active ? (sort?.dir === 1 ? ArrowUp : ArrowDown) : ArrowUpDown
  return (
    <button
      type="button"
      onClick={() => onSort(k)}
      aria-pressed={active}
      className={cn(
        'inline-flex items-center gap-1 uppercase transition-colors',
        active ? 'text-accent' : 'hover:text-text-secondary',
      )}
    >
      {label}
      <Icon className="h-3 w-3" strokeWidth={2} />
    </button>
  )
}

const BAND_OPTIONS = [
  { value: 'all', labelKey: 'devices.bandAll' },
  { value: '2.4 GHz', label: '2.4 GHz' },
  { value: '5 GHz', label: '5 GHz' },
  { value: 'cable', labelKey: 'common.cable' },
] as const

/** Color de identidad por router (devices.md §④) */
const ROUTER_DOT: Record<string, string> = {
  flint2: 'bg-accent',
  living: 'bg-info',
  estudio: 'bg-tunnel',
  patio: 'bg-warn',
}

function signalTextClass(dbm: number | null): string {
  if (dbm === null) return 'text-text-muted'
  if (dbm > -55) return 'text-ok'
  if (dbm >= -70) return 'text-accent'
  return 'text-warn'
}

// ---------------------------------------------------------------------------
// Taxonomía de infraestructura (D6): hipervisor (host con CTs), CT (contenedor
// anidado, tooltip con su host) y switch gestionado. SPEC-65 D65-2/B2: si el
// servidor sella `device.infra` manda; si no, fallback a la inferencia local
// de attachTo/lldp + los distributionNodes del provider.
// ---------------------------------------------------------------------------

type InfraKind = 'hypervisor' | 'ct' | 'managedSwitch'

interface InfraInfo {
  kind: InfraKind
  /** nombre del host (solo CT) */
  host?: string
}

const INFRA_BADGE_CLASS: Record<InfraKind, string> = {
  hypervisor: 'border-tunnel/40 bg-tunnel/10 text-tunnel',
  ct: 'border-border bg-elevated text-text-muted',
  managedSwitch: 'border-accent/30 bg-accent-soft text-accent',
}

/** Badge de infraestructura con tooltip explicativo (D6). */
function InfraBadge({ info }: { info: InfraInfo }) {
  const { t } = useTranslation()
  const tip = info.kind === 'ct' ? t('devices.badges.ctTip', { host: info.host }) : t(`devices.badges.${info.kind}Tip`)
  return (
    <span
      title={tip}
      className={cn(
        'inline-flex w-fit shrink-0 items-center rounded-full border px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide',
        INFRA_BADGE_CLASS[info.kind],
      )}
    >
      {t(`devices.badges.${info.kind}`)}
    </span>
  )
}

const EASE_OUT = [0.16, 1, 0.3, 1] as [number, number, number, number]

// ---------------------------------------------------------------------------
// Toast local (mini, en página — "IP copiada" / "Nombre actualizado")
// ---------------------------------------------------------------------------

interface ToastMsg {
  id: number
  msg: string
}

function Toast({ toast }: { toast: ToastMsg | null }) {
  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-24 z-50 flex justify-center md:bottom-8" aria-live="polite">
      <AnimatePresence>
        {toast && (
          <motion.div
            key={toast.id}
            initial={{ opacity: 0, y: 16, scale: 0.95 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 8, scale: 0.95 }}
            transition={{ duration: 0.25, ease: EASE_OUT }}
            className="flex items-center gap-2 rounded-full border border-border-strong bg-elevated px-4 py-2 text-sm font-medium text-text-primary"
            role="status"
          >
            <Check className="h-4 w-4 text-ok" strokeWidth={1.75} />
            {toast.msg}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Stats strip (devices.md §②)
// ---------------------------------------------------------------------------

function StatsStrip({ allDevices }: { allDevices: ClientDevice[] }) {
  const { t } = useTranslation()
  const { refreshKey } = useDashboard()
  const { deviceTotals } = useNetPulse()
  const reduce = useReducedMotion()
  const newThisWeekDevices = allDevices.filter((d) => d.isNew)
  const weakSignalCount = allDevices.filter((d) => d.online && d.signalDbm !== null && d.signalDbm < -70).length
  const adguardProtected = allDevices.filter((d) => d.adguard).length
  const cards = [
    {
      key: 'online',
      label: t('devices.stats.onlineNow'),
      icon: Wifi,
      iconClass: 'text-accent',
      render: () => (
        <>
          <div className="font-mono text-stat text-text-primary">
            <CountUp value={deviceTotals.online} nonce={refreshKey} />
          </div>
          <motion.div
            initial={reduce ? false : { scale: 0.8, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            transition={{ type: 'spring', stiffness: 320, damping: 18, delay: 0.4 }}
            className="mt-1 inline-flex w-fit items-center rounded-full bg-ok/10 px-2 py-0.5 text-caption font-semibold text-ok"
          >
            {t('devices.stats.newToday', { count: deviceTotals.newToday })}
          </motion.div>
        </>
      ),
    },
    {
      key: 'nuevos',
      label: t('devices.stats.new7d'),
      icon: Sparkles,
      iconClass: 'text-tunnel',
      render: () => (
        <div className="group relative w-fit">
          <div className="font-mono text-stat text-text-primary">
            <CountUp value={newThisWeekDevices.length} nonce={refreshKey} />
          </div>
          <div className="mt-1 text-caption text-text-muted">{t('devices.stats.new7dCaption', { count: deviceTotals.newToday })}</div>
          <div className="pointer-events-none absolute left-0 top-full z-20 mt-2 hidden w-52 rounded-xl border border-border-strong bg-elevated p-3 group-hover:block">
            <div className="mb-1.5 text-label uppercase text-text-muted">{t('devices.stats.seenThisWeek')}</div>
            {newThisWeekDevices.map((d) => (
              <div key={d.id} className="truncate py-0.5 text-xs text-text-secondary">
                {d.name}
                <span className="text-text-muted"> · {d.firstSeen}</span>
              </div>
            ))}
          </div>
        </div>
      ),
    },
    {
      key: 'debil',
      label: t('topology.weakSignal'),
      icon: SignalLow,
      iconClass: 'text-warn',
      render: () => (
        <>
          <div className="font-mono text-stat text-text-primary">
            <CountUp value={weakSignalCount} nonce={refreshKey} />
          </div>
          <div className="mt-1 text-caption text-text-muted">&lt; −70 dBm</div>
        </>
      ),
    },
    {
      key: 'adguard',
      label: t('devices.stats.adguardProtected'),
      icon: ShieldCheck,
      iconClass: 'text-ok',
      render: () => (
        <>
          <div className="font-mono text-stat text-text-primary">
            <CountUp value={adguardProtected} nonce={refreshKey} />
            <span className="text-sm font-medium text-text-secondary">/{deviceTotals.total}</span>
          </div>
          <MetricBar value={Math.round((adguardProtected / deviceTotals.total) * 100)} className="mt-2 max-w-[140px]" />
          <div className="mt-1 text-caption text-text-muted">{t('devices.stats.pctClients', { pct: Math.round((adguardProtected / deviceTotals.total) * 100) })}</div>
        </>
      ),
    },
  ]
  return (
    <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
      {cards.map((c, i) => (
        <motion.div
          key={c.key}
          initial={reduce ? false : { opacity: 0, y: 16 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.4, ease: 'easeOut', delay: 0.05 + i * 0.08 }}
          className="flex min-h-[104px] flex-col justify-between rounded-2xl border border-border bg-surface p-4"
        >
          <div className="flex items-center justify-between gap-2">
            <span className="text-label uppercase text-text-muted">{c.label}</span>
            <c.icon className={cn('h-4 w-4', c.iconClass)} strokeWidth={1.75} />
          </div>
          <div className="mt-2">{c.render()}</div>
        </motion.div>
      ))}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Barra de filtros (devices.md §③)
// ---------------------------------------------------------------------------

interface FilterBarProps {
  router: string
  setRouter: (v: string) => void
  band: BandFilter
  setBand: (v: BandFilter) => void
  groups: FilterGroup[]
  toggleGroup: (g: FilterGroup) => void
  onlyOnline: boolean
  setOnlyOnline: (v: boolean) => void
  onlyWeak: boolean
  setOnlyWeak: (v: boolean) => void
  weakCount: number
  view: 'list' | 'grid'
  setView: (v: 'list' | 'grid') => void
  shown: number
  groupCounts: Record<FilterGroup, number>
}

function FilterBar(p: FilterBarProps) {
  const { t } = useTranslation()
  const { routers, deviceTotals } = useNetPulse()
  return (
    <div className="rounded-2xl border border-border bg-surface p-3 md:p-4">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-3">
        {/* Router: chips con dot de estado */}
        <div
          role="tablist"
          aria-label={t('devices.filterByRouter')}
          className="-mx-1 flex max-w-full items-center gap-1 overflow-x-auto px-1 py-0.5"
        >
          <RouterChip active={p.router === 'all'} label={t('devices.routerAll')} onClick={() => p.setRouter('all')} />
          {routers.map((r) => (
            <RouterChip
              key={r.id}
              active={p.router === r.id}
              label={r.name}
              dotClass={ROUTER_DOT[r.id] ?? 'bg-text-muted'}
              onClick={() => p.setRouter(r.id)}
            />
          ))}
        </div>
        {/* Banda */}
        <SegmentedControl<BandFilter>
          options={BAND_OPTIONS.map((o) => ({ value: o.value, label: 'label' in o ? o.label : t(o.labelKey!) }))}
          value={p.band}
          onChange={p.setBand}
          size="sm"
          ariaLabel={t('devices.filterByBand')}
        />
        {/* Tipo (multi) */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border bg-elevated px-3 text-xs font-medium text-text-secondary transition-colors hover:bg-hover hover:text-text-primary">
              <Filter className="h-3.5 w-3.5" strokeWidth={1.75} />
              {t('devices.colType')}
              {p.groups.length > 0 && (
                <span className="rounded-full bg-accent-soft px-1.5 py-0.5 text-[10px] font-semibold text-accent">
                  {p.groups.length}
                </span>
              )}
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-56">
            <DropdownMenuLabel>{t('devices.deviceType')}</DropdownMenuLabel>
            <DropdownMenuSeparator />
            {GROUP_ORDER.map((g) => (
              <DropdownMenuCheckboxItem
                key={g}
                checked={p.groups.includes(g)}
                onCheckedChange={() => p.toggleGroup(g)}
                onSelect={(e) => e.preventDefault()}
              >
                <span className="flex-1">{t(`devices.groups.${g}`)}</span>
                <span className="ml-2 font-mono text-caption text-text-muted">{p.groupCounts[g]}</span>
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
        {/* Solo online */}
        <label className="inline-flex h-8 cursor-pointer items-center gap-2 rounded-lg px-1 text-xs font-medium text-text-secondary">
          <Switch checked={p.onlyOnline} onCheckedChange={p.setOnlyOnline} aria-label={t('devices.onlyOnline')} />
          {t('devices.onlyOnline')}
        </label>
        {/* Señal débil */}
        {p.weakCount > 0 && (
          <button
            type="button"
            onClick={() => p.setOnlyWeak(!p.onlyWeak)}
            aria-pressed={p.onlyWeak}
            className={cn(
              'inline-flex h-8 items-center gap-1.5 rounded-full border px-3 text-xs font-medium transition-colors',
              p.onlyWeak ? 'border-warn/50 bg-warn/10 text-warn' : 'border-border text-text-secondary hover:border-warn/40 hover:text-warn',
            )}
          >
            <SignalLow className="h-3.5 w-3.5" strokeWidth={1.75} />
            {t('devices.weakChip', { count: p.weakCount })}
          </button>
        )}
        {/* Derecha: vista + caption */}
        <div className="ml-auto flex items-center gap-3">
          <span className="hidden text-caption text-text-muted sm:inline">
            {t('devices.showing')} <CountUp value={p.shown} className="font-semibold text-text-secondary" /> {t('devices.showingOf', { total: deviceTotals.total })}
          </span>
          <div className="inline-flex items-center gap-0.5 rounded-lg border border-border bg-elevated p-1" role="group" aria-label={t('devices.changeView')}>
            <ViewButton active={p.view === 'list'} onClick={() => p.setView('list')} label={t('devices.listView')}>
              <List className="h-3.5 w-3.5" strokeWidth={1.75} />
            </ViewButton>
            <ViewButton active={p.view === 'grid'} onClick={() => p.setView('grid')} label={t('devices.gridView')}>
              <LayoutGrid className="h-3.5 w-3.5" strokeWidth={1.75} />
            </ViewButton>
          </div>
        </div>
      </div>
    </div>
  )
}

function RouterChip({ active, label, dotClass, onClick }: { active: boolean; label: string; dotClass?: string; onClick: () => void }) {
  return (
    <button
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        'inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg border px-3 text-xs font-medium transition-colors duration-150',
        active
          ? 'border-accent/40 bg-accent-soft text-accent'
          : 'border-border bg-elevated text-text-secondary hover:bg-hover hover:text-text-primary',
      )}
    >
      {dotClass && <span className={cn('h-1.5 w-1.5 rounded-full', dotClass)} />}
      {label}
    </button>
  )
}

function ViewButton({ active, onClick, label, children }: { active: boolean; onClick: () => void; label: string; children: React.ReactNode }) {
  return (
    <button
      aria-pressed={active}
      aria-label={label}
      title={label}
      onClick={onClick}
      className={cn(
        'flex h-6 w-7 items-center justify-center rounded-md transition-colors duration-150',
        active ? 'bg-accent-soft text-accent' : 'text-text-secondary hover:bg-hover hover:text-text-primary',
      )}
    >
      {children}
    </button>
  )
}

// ---------------------------------------------------------------------------
// Pills de filtros activos
// ---------------------------------------------------------------------------

interface Pill {
  key: string
  label: string
  clear: () => void
}

function ActivePills({ pills, clearAll }: { pills: Pill[]; clearAll: () => void }) {
  const { t } = useTranslation()
  if (pills.length === 0) return null
  return (
    <div className="flex flex-wrap items-center gap-2">
      <AnimatePresence mode="popLayout">
        {pills.map((p) => (
          <motion.span
            layout
            key={p.key}
            initial={{ scale: 0.8, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            exit={{ scale: 0.8, opacity: 0, transition: { duration: 0.15 } }}
            transition={{ type: 'spring', stiffness: 400, damping: 24 }}
            className="inline-flex items-center gap-1 rounded-full border border-accent/30 bg-accent-soft py-1 pl-3 pr-1.5 text-caption font-semibold text-accent"
          >
            {p.label}
            <button
              onClick={p.clear}
              aria-label={t('devices.removeFilter', { label: p.label })}
              className="flex h-4 w-4 items-center justify-center rounded-full transition-colors hover:bg-accent/20"
            >
              <X className="h-3 w-3" strokeWidth={2} />
            </button>
          </motion.span>
        ))}
      </AnimatePresence>
      <button
        onClick={clearAll}
        className="text-caption font-semibold text-text-muted underline-offset-2 transition-colors hover:text-accent hover:underline"
      >
        {t('devices.clearFilters')}
      </button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Panel de detalle expandible (devices.md §④ — read-only + rename local)
// ---------------------------------------------------------------------------

function DetailItem({ label, children, mono }: { label: string; children: React.ReactNode; mono?: boolean }) {
  return (
    <div className="min-w-0">
      <div className="text-label uppercase text-text-muted">{label}</div>
      <div className={cn('mt-1 truncate text-sm text-text-primary', mono && 'font-mono text-mono-sm')}>{children}</div>
    </div>
  )
}

function DeviceDetail({
  device,
  name,
  infra,
  onRename,
}: {
  device: ClientDevice
  name: string
  infra?: InfraInfo
  onRename: (id: string, name: string | null) => void
}) {
  // El panel se desmonta al colapsar: el borrador siempre arranca del nombre actual
  const { t } = useTranslation()
  const [draft, setDraft] = useState(name)
  const inputRef = useRef<HTMLInputElement>(null)

  const commit = () => {
    const value = draft.trim()
    if (value === name) return
    onRename(device.id, value === device.name ? null : value || null)
  }

  return (
    <div className="grid grid-cols-2 gap-x-4 gap-y-4 px-4 py-4 md:grid-cols-3 md:px-5">
      <DetailItem label="MAC" mono>
        {device.mac}
      </DetailItem>
      <DetailItem label={t('devices.detail.dhcpLease')}>{dhcpLease(device.dhcpLease)}</DetailItem>
      <DetailItem label={t('devices.detail.firstSeen')}>{device.firstSeen}</DetailItem>
      <DetailItem label={t('devices.detail.manufacturer')}>{device.manufacturer}</DetailItem>
      <DetailItem label="Hostname" mono>
        {device.hostname}
      </DetailItem>
      <DetailItem label={t('devices.detail.traffic24h')} mono>
        ↓ {device.traffic24hRx} · ↑ {device.traffic24hTx}
      </DetailItem>
      <div className="min-w-0">
        <div className="text-label uppercase text-text-muted">AdGuard</div>
        <div className="mt-1.5">
          {device.adguard ? (
            <StatusPill tone="ok" label={t('devices.detail.protected')} />
          ) : (
            <StatusPill tone="muted" label={t('devices.detail.unfiltered')} />
          )}
        </div>
      </div>
      {infra && (
        <DetailItem label={t('devices.detail.infra')}>
          <InfraBadge info={infra} />
          {infra.kind === 'ct' && infra.host && (
            <span className="ml-1.5 text-caption text-text-muted">{t('devices.badges.ctTip', { host: infra.host })}</span>
          )}
        </DetailItem>
      )}
      <div className="col-span-2">
        <label htmlFor={`rename-${device.id}`} className="text-label uppercase text-text-muted">
          {t('devices.detail.rename')}
        </label>
        <div className="relative mt-1 max-w-xs">
          <Pencil className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-text-muted" strokeWidth={1.75} />
          <Input
            id={`rename-${device.id}`}
            ref={inputRef}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={commit}
            onKeyDown={(e) => {
              if (e.key === 'Enter') inputRef.current?.blur()
              if (e.key === 'Escape') {
                setDraft(name)
                inputRef.current?.blur()
              }
            }}
            maxLength={40}
            placeholder={device.name}
            className="h-9 rounded-lg border-border bg-elevated pl-9 text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent/50"
          />
        </div>
        <p className="mt-1 text-caption text-text-muted">{t('devices.detail.renameLocal')}</p>
      </div>
    </div>
  )
}

/** Contenedor animado del panel expandible (height auto spring 300ms) */
function ExpandPanel({ open, children }: { open: boolean; children: React.ReactNode }) {
  return (
    <AnimatePresence initial={false}>
      {open && (
        <motion.div
          key="panel"
          initial={{ height: 0, opacity: 0 }}
          animate={{ height: 'auto', opacity: 1 }}
          exit={{ height: 0, opacity: 0 }}
          transition={{ duration: 0.3, ease: EASE_OUT }}
          className="overflow-hidden"
        >
          <motion.div
            initial="hidden"
            animate="show"
            variants={{ show: { transition: { staggerChildren: 0.05 } } }}
            className="border-t border-border bg-elevated/40"
          >
            {children}
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  )
}

// ---------------------------------------------------------------------------
// Filas y tarjetas de dispositivo
// ---------------------------------------------------------------------------

/** Tile de icono con punto de luz si es nuevo (devices.md §④ "Nuevos") */
function DeviceTile({ device, size = 'md' }: { device: ClientDevice; size?: 'md' | 'lg' }) {
  const Icon = device.iconOverride ?? DEVICE_ICONS[device.type]
  return (
    <div
      className={cn(
        'relative flex shrink-0 items-center justify-center rounded-lg bg-elevated text-text-secondary',
        size === 'md' ? 'h-9 w-9' : 'h-12 w-12 rounded-xl',
      )}
    >
      <Icon className={size === 'md' ? 'h-[18px] w-[18px]' : 'h-6 w-6'} strokeWidth={1.75} />
      {device.isNew && (
        <span className="absolute -right-0.5 -top-0.5 flex h-2 w-2">
          <span className="absolute inline-flex h-full w-full rounded-full bg-accent opacity-75 animate-ping-soft" />
          <span className="relative inline-flex h-2 w-2 rounded-full bg-accent" />
        </span>
      )}
    </div>
  )
}

function NewPill() {
  const { t } = useTranslation()
  return (
    <span className="rounded-full bg-accent-soft px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-accent">
      {t('devices.new')}
    </span>
  )
}

/** Chip de router con dot de identidad; navega a /routers/:id */
function RouterChipLink({ routerId, onNavigate }: { routerId: string; onNavigate: (id: string) => void }) {
  const { t } = useTranslation()
  const { routers } = useNetPulse()
  const r = routers.find((x) => x.id === routerId)
  return (
    <button
      onClick={(e) => {
        e.stopPropagation()
        onNavigate(routerId)
      }}
      className="inline-flex w-fit items-center gap-1.5 rounded-full border border-transparent bg-elevated px-2.5 py-1 text-caption font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
      title={t('devices.viewRouter', { name: r?.name ?? routerId })}
    >
      <span className={cn('h-1.5 w-1.5 rounded-full', ROUTER_DOT[routerId] ?? 'bg-text-muted')} />
      {r?.name ?? routerId}
    </button>
  )
}

function BandChip({ band }: { band: ClientDevice['band'] }) {
  const { t } = useTranslation()
  return (
    <span className="inline-flex w-fit items-center gap-1 rounded-full border border-border-strong px-2.5 py-1 font-mono text-caption text-text-secondary">
      {band === 'cable' ? t('common.cable') : band}
    </span>
  )
}

function SignalCell({ device }: { device: ClientDevice }) {
  if (!device.online) return <span className="font-mono text-mono-sm text-text-muted">—</span>
  return (
    <span className="inline-flex items-center gap-1.5">
      <SignalIcon dbm={device.signalDbm} />
      {device.signalDbm !== null && (
        <span className={cn('font-mono text-mono-sm', signalTextClass(device.signalDbm))}>
          {device.signalDbm} dBm
        </span>
      )}
    </span>
  )
}

function TrafficCell({ device }: { device: ClientDevice }) {
  if (!device.online) return <span className="font-mono text-mono-sm text-text-muted">—</span>
  return (
    <span className="inline-flex items-center gap-2">
      <span className="font-mono text-mono-sm text-accent">
        {device.trafficMbps >= 1 ? fmtEs(device.trafficMbps, 1) : fmtEs(device.trafficMbps, 2)} Mbps
      </span>
      <Sparkline data={device.sparkline} width={60} height={20} className="hidden text-accent xl:block" />
    </span>
  )
}

const ROW_GRID =
  'md:grid-cols-[minmax(0,2.2fr)_minmax(0,1fr)_minmax(0,0.8fr)_minmax(0,1fr)_minmax(0,1.1fr)_1.5rem] lg:grid-cols-[minmax(0,2.2fr)_minmax(0,1.3fr)_minmax(0,1fr)_minmax(0,0.8fr)_minmax(0,1fr)_minmax(0,1.2fr)_1.5rem]'

/** Fila de tabla desktop (md+) */
function ListRow({
  device,
  name,
  infra,
  expanded,
  index,
  onToggle,
  onCopyIp,
  onNavigateRouter,
  onRename,
}: {
  device: ClientDevice
  name: string
  infra?: InfraInfo
  expanded: boolean
  index: number
  onToggle: () => void
  onCopyIp: (ip: string) => void
  onNavigateRouter: (id: string) => void
  onRename: (id: string, name: string | null) => void
}) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  return (
    <motion.div
      layout="position"
      initial={reduce ? false : { opacity: 0, y: 10 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, scale: 0.95, transition: { duration: 0.2 } }}
      transition={{ duration: 0.3, ease: 'easeOut', delay: Math.min(index, 12) * 0.035 }}
    >
      <div
        role="button"
        tabIndex={0}
        aria-expanded={expanded}
        onClick={onToggle}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            onToggle()
          }
        }}
        className={cn(
          'hidden cursor-pointer items-center gap-3 px-3 py-2 transition-colors duration-150 hover:bg-hover md:grid',
          ROW_GRID,
          !device.online && 'opacity-55',
          expanded && 'bg-hover/60',
        )}
      >
        {/* Dispositivo */}
        <div className="flex min-w-0 items-center gap-3">
          <DeviceTile device={device} />
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="truncate text-sm font-medium text-text-primary">{name}</span>
              {device.isNew && <NewPill />}
              {infra && <InfraBadge info={infra} />}
              {!device.online && <StatusPill tone="muted" label={t('common.status.offline')} />}
            </div>
            <div className="truncate text-caption text-text-muted">{device.manufacturer}</div>
          </div>
        </div>
        {/* IP / MAC */}
        <button
          onClick={(e) => {
            e.stopPropagation()
            onCopyIp(device.ip)
          }}
          title={t('devices.copyIp')}
          className="hidden w-fit min-w-0 flex-col items-start rounded-md px-1 py-0.5 text-left transition-colors hover:bg-elevated lg:flex"
        >
          <span className="font-mono text-mono-sm text-text-primary">{device.ip}</span>
          <span className="font-mono text-caption text-text-muted">{device.mac}</span>
        </button>
        {/* Router */}
        <div className="min-w-0">
          <RouterChipLink routerId={device.routerId} onNavigate={onNavigateRouter} />
        </div>
        {/* Banda */}
        <div>
          <BandChip band={device.band} />
        </div>
        {/* Señal */}
        <div>
          <SignalCell device={device} />
        </div>
        {/* Tráfico */}
        <div>
          <TrafficCell device={device} />
        </div>
        <ChevronDown
          className={cn('h-4 w-4 justify-self-end text-text-muted transition-transform duration-200', expanded && 'rotate-180')}
          strokeWidth={1.75}
        />
      </div>
      {/* Móvil: DeviceRow compartido en card */}
      <div className={cn('md:hidden', !device.online && 'opacity-55')}>
        <DeviceRow device={{ ...device, name }} variant="full" onClick={onToggle} />
      </div>
      <ExpandPanel open={expanded}>
        <DeviceDetail device={device} name={name} infra={infra} onRename={onRename} />
      </ExpandPanel>
    </motion.div>
  )
}

/** Barra de señal de 4 segmentos (vista grid) */
function signalBarClass(dbm: number): string {
  if (dbm > -55) return 'bg-ok'
  if (dbm >= -70) return 'bg-accent'
  return 'bg-warn'
}

function SignalBars({ device }: { device: ClientDevice }) {
  const { t } = useTranslation()
  if (!device.online || device.signalDbm === null) {
    return <span className="font-mono text-caption text-text-muted">{device.band === 'cable' ? t('common.cable') : '—'}</span>
  }
  const level = signalLevel(device.signalDbm)
  const active = level === 'high' ? 4 : level === 'medium' ? 3 : level === 'low' ? 2 : 1
  return (
    <span className="inline-flex items-end gap-0.5" aria-label={t('devices.signalDbm', { dbm: device.signalDbm })}>
      {[1, 2, 3, 4].map((i) => (
        <span
          key={i}
          className={cn('w-1 rounded-full', i <= active ? signalBarClass(device.signalDbm!) : 'bg-border')}
          style={{ height: `${4 + i * 2}px` }}
        />
      ))}
      <span className={cn('ml-1.5 font-mono text-caption', signalTextClass(device.signalDbm))}>{device.signalDbm} dBm</span>
    </span>
  )
}

/** Tarjeta de la vista grid (devices.md §④) */
function GridCard({
  device,
  name,
  infra,
  expanded,
  index,
  onToggle,
  onNavigateRouter,
  onRename,
}: {
  device: ClientDevice
  name: string
  infra?: InfraInfo
  expanded: boolean
  index: number
  onToggle: () => void
  onNavigateRouter: (id: string) => void
  onRename: (id: string, name: string | null) => void
}) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  return (
    <motion.div
      layout="position"
      initial={reduce ? false : { opacity: 0, scale: 0.96 }}
      animate={{ opacity: 1, scale: 1 }}
      exit={{ opacity: 0, scale: 0.95, transition: { duration: 0.2 } }}
      transition={{ duration: 0.3, ease: 'easeOut', delay: Math.min(index, 12) * 0.05 }}
      className={cn(
        'overflow-hidden rounded-2xl border bg-surface transition-colors duration-150',
        expanded ? 'border-accent/40' : 'border-border hover:border-accent/30',
        !device.online && 'opacity-55',
      )}
    >
      <div
        role="button"
        tabIndex={0}
        aria-expanded={expanded}
        onClick={onToggle}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            onToggle()
          }
        }}
        className="cursor-pointer p-4"
      >
        <div className="flex items-start justify-between gap-2">
          <DeviceTile device={device} size="lg" />
          {!device.online ? <StatusPill tone="muted" label={t('common.status.offline')} /> : device.isNew ? <NewPill /> : null}
        </div>
        <div className="mt-3 flex items-center gap-2">
          <span className="truncate text-sm font-medium text-text-primary">{name}</span>
          {infra && <InfraBadge info={infra} />}
        </div>
        <div className="truncate text-caption text-text-muted">
          {device.manufacturer} · <span className="font-mono">{device.ip}</span>
        </div>
        <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
          <RouterChipLink routerId={device.routerId} onNavigate={onNavigateRouter} />
          <BandChip band={device.band} />
        </div>
        <div className="mt-3 flex items-center justify-between gap-2 border-t border-border pt-3">
          <SignalBars device={device} />
          <span className="font-mono text-mono-sm text-accent">
            {device.online
              ? `${device.trafficMbps >= 1 ? fmtEs(device.trafficMbps, 1) : fmtEs(device.trafficMbps, 2)} Mbps`
              : '—'}
          </span>
        </div>
      </div>
      <ExpandPanel open={expanded}>
        <DeviceDetail device={device} name={name} infra={infra} onRename={onRename} />
      </ExpandPanel>
    </motion.div>
  )
}

// ---------------------------------------------------------------------------
// Página Dispositivos — /devices (devices.md)
// ---------------------------------------------------------------------------

export default function Devices() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const reduce = useReducedMotion()
  const { devices, deviceTotals, isDemo, routers, distributionNodes } = useNetPulse()
  const [renames, setRename] = useRenames()

  // Lista enriquecida de clientes: en demo local expande el canon a los 65
  // del dataset reconciliado; en live solo fusiona metadatos conocidos sobre
  // la API. D6: los equipos de infraestructura (host hipervisor, sus CTs y
  // switches gestionados con LLDP) se reclasifican al grupo de filtro 'infra'.
  const { allDevices, infraById } = useMemo(() => {
    const list = buildClientDevices(devices, isDemo)
    const hosts = new Set(
      distributionNodes.filter((n) => n.kind === 'hypervisor' && n.hostDeviceId).map((n) => n.hostDeviceId!),
    )
    const byId = new Map(list.map((d) => [d.id, d]))
    const infra = new Map<string, InfraInfo>()
    for (const d of list) {
      // SPEC-65 D65-2/B2: prioridad al sello server-side `device.infra`;
      // la inferencia local queda como fallback para datos viejos.
      const sealed = d.infra
      if (sealed === 'hypervisor' || (!sealed && hosts.has(d.id))) {
        infra.set(d.id, { kind: 'hypervisor' })
      } else if (sealed === 'ct' || (!sealed && d.attachTo && hosts.has(d.attachTo))) {
        infra.set(d.id, { kind: 'ct', host: byId.get(d.attachTo ?? '')?.name ?? d.attachTo })
      } else if (sealed === 'managed-switch' || (!sealed && d.lldp)) {
        infra.set(d.id, { kind: 'managedSwitch' })
      }
    }
    if (infra.size === 0) return { allDevices: list, infraById: infra }
    return { allDevices: list.map((d) => (infra.has(d.id) ? { ...d, group: 'infra' as const } : d)), infraById: infra }
  }, [devices, isDemo, distributionNodes])

  const [searchParams] = useSearchParams()
  const [query, setQuery] = useState(() => searchParams.get('q') ?? '')
  const [q, setQ] = useState(() => (searchParams.get('q') ?? '').trim().toLowerCase())

  // Enlaces entrantes tipo /devices?q=<mac|ip|nombre> (p.ej. desde Puertos)
  useEffect(() => {
    const incoming = searchParams.get('q')
    if (incoming !== null) {
      setQuery(incoming)
      setQ(incoming.trim().toLowerCase())
    }
  }, [searchParams])
  const [router, setRouter] = useState('all')
  const [band, setBand] = useState<BandFilter>('all')
  const [groups, setGroups] = useState<FilterGroup[]>([])
  const [onlyOnline, setOnlyOnline] = useState(true)
  const [onlyWeak, setOnlyWeak] = useState(false)
  const weakCount = useMemo(
    () => allDevices.filter((d) => d.online && d.signalDbm !== null && d.signalDbm < -70).length,
    [allDevices],
  )
  const [view, setView] = useState<'list' | 'grid'>('list')
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [toast, setToast] = useState<ToastMsg | null>(null)
  // Búsqueda con debounce 150ms (devices.md §Interacciones)
  useEffect(() => {
    const t = setTimeout(() => setQ(query.trim().toLowerCase()), 150)
    return () => clearTimeout(t)
  }, [query])

  // Auto-cierre del toast
  useEffect(() => {
    if (!toast) return
    const t = setTimeout(() => setToast(null), 2200)
    return () => clearTimeout(t)
  }, [toast])

  const showToast = useCallback((msg: string) => setToast({ id: Date.now(), msg }), [])

  const nameOf = useCallback((d: ClientDevice) => renames[d.id] ?? d.name, [renames])

  const groupCounts = useMemo(() => {
    const counts = Object.fromEntries(GROUP_ORDER.map((g) => [g, 0])) as Record<FilterGroup, number>
    for (const d of allDevices) counts[d.group]++
    return counts
  }, [allDevices])

  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 } | null>(null)
  const toggleSort = useCallback((key: SortKey) => {
    setSort((prev) => (prev?.key === key ? { key, dir: prev.dir === 1 ? -1 : 1 } : { key, dir: 1 }))
  }, [])

  const filtered = useMemo(() => {
    const out = allDevices.filter((d) => {
      if (onlyOnline && !d.online) return false
      if (onlyWeak && !(d.online && d.signalDbm !== null && d.signalDbm < -70)) return false
      if (router !== 'all' && d.routerId !== router) return false
      if (band !== 'all' && d.band !== band) return false
      if (groups.length > 0 && !groups.includes(d.group)) return false
      if (q) {
        const hay = `${nameOf(d)} ${d.name} ${d.ip} ${d.mac} ${d.manufacturer} ${d.hostname}`.toLowerCase()
        if (!hay.includes(q)) return false
      }
      return true
    })
    if (sort) {
      const routerNameOf = (id: string) => routers.find((r) => r.id === id)?.name ?? id
      return out.sort((a, b) => {
        const dir = sort.dir
        let c = 0
        switch (sort.key) {
          case 'name':
            c = nameOf(a).localeCompare(nameOf(b), numLocale())
            break
          case 'ip':
            c = ipNum(a.ip) - ipNum(b.ip)
            break
          case 'router':
            c = routerNameOf(a.routerId).localeCompare(routerNameOf(b.routerId), numLocale())
            break
          case 'band':
            c = a.band.localeCompare(b.band)
            break
          case 'signal':
            c = (a.signalDbm ?? -999) - (b.signalDbm ?? -999)
            break
          case 'traffic':
            c = a.trafficMbps - b.trafficMbps
            break
        }
        if (c !== 0) return dir * c
        // Desempate: online primero, luego nombre
        if (a.online !== b.online) return a.online ? -1 : 1
        return nameOf(a).localeCompare(nameOf(b), numLocale())
      })
    }
    // Online primero (por tráfico desc), conocidos offline al final (devices.md §④)
    return out.sort((a, b) => {
      if (a.online !== b.online) return a.online ? -1 : 1
      return a.online ? b.trafficMbps - a.trafficMbps : a.name.localeCompare(b.name, numLocale())
    })
  }, [allDevices, onlyOnline, onlyWeak, router, band, groups, q, nameOf, sort, routers])

  const toggleGroup = useCallback(
    (g: FilterGroup) => setGroups((prev) => (prev.includes(g) ? prev.filter((x) => x !== g) : [...prev, g])),
    [],
  )

  const clearFilters = useCallback(() => {
    setRouter('all')
    setBand('all')
    setGroups([])
    setOnlyOnline(true)
  }, [])

  const clearAll = useCallback(() => {
    clearFilters()
    setQuery('')
  }, [clearFilters])

  const copyIp = useCallback(
    (ip: string) => {
      void navigator.clipboard?.writeText(ip).catch(() => undefined)
      showToast(t('devices.ipCopied'))
    },
    [showToast, t],
  )

  const handleRename = useCallback(
    (id: string, name: string | null) => {
      setRename(id, name)
      showToast(t('devices.nameUpdated'))
    },
    [setRename, showToast, t],
  )

  const navigateRouter = useCallback((id: string) => navigate(`/routers/${id}`), [navigate])

  const pills = useMemo<Pill[]>(() => {
    const list: Pill[] = []
    if (router !== 'all') {
      const routerName = routers.find((r) => r.id === router)?.name ?? router
      list.push({ key: `router-${router}`, label: routerName, clear: () => setRouter('all') })
    }
    if (band !== 'all') {
      list.push({ key: `band-${band}`, label: band === 'cable' ? t('common.cable') : band, clear: () => setBand('all') })
    }
    for (const g of groups) {
      list.push({ key: `group-${g}`, label: t(`devices.groups.${g}`), clear: () => toggleGroup(g) })
    }
    if (onlyOnline) {
      list.push({ key: 'online', label: t('devices.onlyOnline'), clear: () => setOnlyOnline(false) })
    }
    return list
  }, [router, routers, band, groups, onlyOnline, toggleGroup, t])

  const searchBox = (className?: string, autoFocus = false) => (
    <div className={cn('relative', className)}>
      <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-text-muted" strokeWidth={1.75} />
      <Input
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder={t('devices.searchPlaceholder')}
        aria-label={t('devices.searchAria')}
        autoFocus={autoFocus}
        className="h-10 rounded-lg border-border bg-surface pl-9 pr-14 text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent/50"
      />
      <kbd className="pointer-events-none absolute right-3 top-1/2 hidden -translate-y-1/2 rounded border border-border-strong bg-elevated px-1.5 py-0.5 font-mono text-[10px] text-text-muted md:block">
        ⌘K
      </kbd>
    </div>
  )

  return (
    <div className="space-y-4 md:space-y-5">
      {/* ① Page header */}
      <header>
        <nav aria-label={t('common.breadcrumb')} className="text-caption text-text-muted">
          <Link to="/" className="transition-colors hover:text-accent">
            {t('common.home')}
          </Link>
          <span className="mx-1.5">/</span>
          <span className="text-text-secondary">{t('nav.devices')}</span>
        </nav>
        <div className="mt-1.5 flex flex-wrap items-end justify-between gap-x-4 gap-y-3">
          <div>
            <motion.h1
              initial={reduce ? false : { opacity: 0, y: 12 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.3, ease: 'easeOut' }}
              className="font-display text-h1 text-text-primary"
            >
              {t('nav.devices')}
            </motion.h1>
            <p className="mt-0.5 text-caption text-text-muted">
              {t('devices.summary', { total: deviceTotals.total, online: deviceTotals.online })}
            </p>
          </div>
          <motion.div
            initial={reduce ? false : { opacity: 0, x: 12 }}
            animate={{ opacity: 1, x: 0 }}
            transition={{ duration: 0.3, ease: 'easeOut', delay: 0.15 }}
            className="hidden md:block"
          >
            {searchBox('w-72')}
          </motion.div>
        </div>
      </header>

      {/* Búsqueda móvil sticky bajo el header */}
      <div className="sticky top-14 z-20 -mx-4 bg-canvas/90 px-4 py-2 backdrop-blur-md md:hidden">
        {searchBox()}
      </div>

      {/* ② Stats strip */}
      <StatsStrip allDevices={allDevices} />

      {/* ③ Filter bar */}
      <FilterBar
        router={router}
        setRouter={setRouter}
        band={band}
        setBand={setBand}
        groups={groups}
        toggleGroup={toggleGroup}
        onlyOnline={onlyOnline}
        setOnlyOnline={setOnlyOnline}
        onlyWeak={onlyWeak}
        setOnlyWeak={setOnlyWeak}
        weakCount={weakCount}
        view={view}
        setView={setView}
        shown={filtered.length}
        groupCounts={groupCounts}
      />

      {/* Pills de filtros activos */}
      <ActivePills pills={pills} clearAll={clearFilters} />

      {/* Caption resultado (móvil, donde no cabe en la barra) */}
      <div className="text-caption text-text-muted sm:hidden">
        {t('devices.showing')} {filtered.length} {t('devices.showingOf', { total: deviceTotals.total })}
      </div>

      {/* ④ Lista / grid / empty state */}
      {filtered.length === 0 ? (
        <motion.div
          initial={reduce ? false : { opacity: 0, scale: 0.95 }}
          animate={{ opacity: 1, scale: 1 }}
          transition={{ duration: 0.3, ease: 'easeOut' }}
          className="rounded-2xl border border-border bg-surface"
        >
          <EmptyState
            image="/empty-devices.svg"
            title={t('devices.emptyTitle')}
            description={t('devices.emptyDesc')}
          />
          <div className="-mt-4 flex justify-center pb-8">
            <button
              onClick={clearAll}
              className="rounded-lg border border-border px-4 py-2 text-sm font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
            >
              {t('devices.clearFilters')}
            </button>
          </div>
        </motion.div>
      ) : view === 'list' ? (
        <div className="rounded-2xl border border-border bg-surface">
          {/* Cabecera de tabla sticky (desktop) */}
          <div
            className={cn(
              'sticky top-14 z-10 hidden items-center gap-3 rounded-t-2xl border-b border-border bg-surface px-3 py-2 text-label uppercase text-text-muted md:grid',
              ROW_GRID,
            )}
          >
            <SortHeader label={t('devices.colDevice')} k="name" sort={sort} onSort={toggleSort} />
            <span className="hidden lg:block">
              <SortHeader label="IP / MAC" k="ip" sort={sort} onSort={toggleSort} />
            </span>
            <SortHeader label="Router" k="router" sort={sort} onSort={toggleSort} />
            <SortHeader label={t('devices.colBand')} k="band" sort={sort} onSort={toggleSort} />
            <SortHeader label={t('devices.colSignal')} k="signal" sort={sort} onSort={toggleSort} />
            <SortHeader label={t('devices.colTraffic')} k="traffic" sort={sort} onSort={toggleSort} />
            <span />
          </div>
          <div className="divide-y divide-border p-1.5 md:p-2">
            <AnimatePresence initial={false}>
              {filtered.map((d, i) => (
                <ListRow
                  key={d.id}
                  device={d}
                  name={nameOf(d)}
                  infra={infraById.get(d.id)}
                  expanded={expandedId === d.id}
                  index={i}
                  onToggle={() => setExpandedId((prev) => (prev === d.id ? null : d.id))}
                  onCopyIp={copyIp}
                  onNavigateRouter={navigateRouter}
                  onRename={handleRename}
                />
              ))}
            </AnimatePresence>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <AnimatePresence initial={false}>
            {filtered.map((d, i) => (
              <GridCard
                key={d.id}
                device={d}
                name={nameOf(d)}
                infra={infraById.get(d.id)}
                expanded={expandedId === d.id}
                index={i}
                onToggle={() => setExpandedId((prev) => (prev === d.id ? null : d.id))}
                onNavigateRouter={navigateRouter}
                onRename={handleRename}
              />
            ))}
          </AnimatePresence>
        </div>
      )}

      <Toast toast={toast} />
    </div>
  )
}
