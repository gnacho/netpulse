import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate, useSearchParams } from 'react-router'
import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import {
  ArrowDown,
  ArrowLeftRight,
  ArrowUp,
  ArrowUpDown,
  Check,
  ChevronDown,
  Columns3,
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
import type { LucideIcon } from 'lucide-react'
import { DeviceRow, deviceIcon, SignalIcon } from '@/components/DeviceRow'
import { CountUp } from '@/components/CountUp'
import { EmptyState } from '@/components/EmptyState'
import { SegmentedControl } from '@/components/SegmentedControl'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { dhcpLease, fmtSeenAgo, manufacturerLabel, numLocale } from '@/i18n'
import { fmtEs, signalLevel } from '@/data/mock'
import { useNetPulse } from '@/data/DataProvider'
import { useDashboard } from '@/hooks/useDashboard'
import { useWeakSignalDbm } from '@/hooks/useWeakSignalDbm'
import { DeviceEditSheet } from '@/components/DeviceEditSheet'
import { OnboardingIntake } from '@/components/OnboardingIntake'
import { cn, copyToClipboard, fetchJson } from '@/lib/utils'
import { useParentName } from '@/lib/parent'
import { networkHasBand6 } from '@/components/topology/model'
import type { ClientDevice, FilterGroup } from '@/pages/devices-data'
import { buildClientDevices, GROUP_ORDER } from '@/pages/devices-data'
import type { DeviceType } from '@/data/mock'


// ---------------------------------------------------------------------------
// Constantes de la página
// ---------------------------------------------------------------------------

type BandFilter = 'all' | '6 GHz' | '5 GHz' | '2.4 GHz' | 'cable'
// #989: valor de banda seleccionable en el filtro multi (sin 'all': la
// selección vacía ya equivale a "todas").
type BandValue = Exclude<BandFilter, 'all'>
type SortKey = 'name' | 'ip' | 'router' | 'band' | 'lease' | 'signal' | 'type' | 'traffic' | 'firstSeen' | 'lastSeen'

// Persistencia de preferencias de visualización (issue #778): el modo
// lista/rejilla y el orden activo se guardan en el navegador y se restauran
// al volver a abrir la sección de clientes.
const DEVICES_PREFS_KEY = 'netpulse.devices.prefs'
type SortState = { key: SortKey; dir: 1 | -1 } | null
// #923: filtro de estado online triestado, persistente junto al resto de
// preferencias de la página.
type OnlineFilter = 'all' | 'online' | 'offline'
// #958: columnas ocultables de la tabla (todas menos el nombre). Se guardan
// las OCULTAS; por defecto ninguna (tabla completa, como siempre).
type DeviceColumn = 'type' | 'ip' | 'lease' | 'router' | 'band' | 'signal' | 'traffic' | 'firstSeen' | 'lastSeen'
const DEVICE_COLUMNS: DeviceColumn[] = ['type', 'ip', 'lease', 'router', 'band', 'signal', 'traffic', 'firstSeen', 'lastSeen']
type DevicesPrefs = { view: 'list' | 'grid'; sort: SortState; online: OnlineFilter; columns: DeviceColumn[] }

const SORT_KEYS: SortKey[] = ['name', 'ip', 'router', 'band', 'lease', 'signal', 'type', 'traffic', 'firstSeen', 'lastSeen']

function loadDevicesPrefs(): DevicesPrefs {
  const fallback: DevicesPrefs = { view: 'list', sort: null, online: 'online', columns: [] }
  try {
    const raw = localStorage.getItem(DEVICES_PREFS_KEY)
    if (!raw) return fallback
    const v = JSON.parse(raw) as Partial<DevicesPrefs>
    const view = v.view === 'grid' ? 'grid' : 'list'
    let sort: SortState = null
    if (v.sort && SORT_KEYS.includes(v.sort.key) && (v.sort.dir === 1 || v.sort.dir === -1)) {
      sort = { key: v.sort.key, dir: v.sort.dir }
    }
    const online: OnlineFilter = v.online === 'all' || v.online === 'offline' ? v.online : 'online'
    const columns = Array.isArray(v.columns)
      ? v.columns.filter((c): c is DeviceColumn => DEVICE_COLUMNS.includes(c as DeviceColumn))
      : []
    return { view, sort, online, columns }
  } catch {
    return fallback
  }
}

function saveDevicesPrefs(p: DevicesPrefs) {
  try {
    localStorage.setItem(DEVICES_PREFS_KEY, JSON.stringify(p))
  } catch {
    /* localStorage no disponible */
  }
}

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

/** Selector de columnas visibles (#958): checkbox por columna en la cabecera
 * de la tabla. El nombre (Dispositivo) es fijo y no se puede ocultar. */
function ColumnPicker({ hidden, onToggle }: { hidden: DeviceColumn[]; onToggle: (c: DeviceColumn) => void }) {
  const { t } = useTranslation()
  const labels: Record<DeviceColumn, string> = {
    type: t('devices.colType'),
    ip: 'IP / MAC',
    lease: t('devices.colLease'),
    router: 'Router',
    band: t('devices.colBand'),
    signal: t('devices.colSignal'),
    traffic: t('devices.colTraffic'),
    firstSeen: t('devices.colFirstSeen'),
    lastSeen: t('devices.colLastSeen'),
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={t('devices.columns.action')}
          title={t('devices.columns.action')}
          onClick={(e) => e.stopPropagation()}
          className="flex h-5 w-5 items-center justify-center justify-self-end rounded-md text-text-muted transition-colors hover:text-accent"
        >
          <Columns3 className="h-3.5 w-3.5" strokeWidth={1.75} />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
        <DropdownMenuLabel>{t('devices.columns.title')}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuCheckboxItem checked disabled onSelect={(e) => e.preventDefault()}>
          {t('devices.colDevice')}
        </DropdownMenuCheckboxItem>
        {DEVICE_COLUMNS.map((c) => (
          <DropdownMenuCheckboxItem
            key={c}
            checked={!hidden.includes(c)}
            onCheckedChange={() => onToggle(c)}
            onSelect={(e) => e.preventDefault()}
          >
            {labels[c]}
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

// #970: la banda se filtra con un desplegable (era un SegmentedControl); #989
// lo convierte en multi-selección acumulativa con el patrón del filtro "Tipo".
// #991: la opción 6 GHz solo se ofrece si la red TIENE 6 GHz
// (networkHasBand6: algún router con bandSplit.band6 > 0 o algún cliente con
// banda '6 GHz'; no hay flag de capacidad hardware en los tipos). Sin 6G la
// opción desaparece del menú y cualquier selección residual se ignora.
const BAND_FILTERS: ReadonlyArray<{ value: BandValue; label?: string; labelKey?: string; band6?: boolean }> = [
  { value: '2.4 GHz', label: '2.4 GHz' },
  { value: '5 GHz', label: '5 GHz' },
  { value: '6 GHz', label: '6 GHz', band6: true },
  { value: 'cable', labelKey: 'common.cable' },
]

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

type InfraKind = 'hypervisor' | 'ct' | 'vm' | 'managedSwitch'
interface InfraInfo {
  kind: InfraKind
  /** nombre del host (solo CT) */
  host?: string
}

const INFRA_BADGE_CLASS: Record<InfraKind, string> = {
  hypervisor: 'border-tunnel/40 bg-tunnel/10 text-tunnel',
  ct: 'border-border bg-elevated text-text-muted',
  vm: 'border-border bg-elevated text-text-muted',
  managedSwitch: 'border-accent/30 bg-accent-soft text-accent',
}

/** Badge de infraestructura con tooltip explicativo (D6). */
function InfraBadge({ info }: { info: InfraInfo }) {
  const { t } = useTranslation()
  const tip = t(`devices.badges.${info.kind}Tip`, { host: info.host })
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

/** Etiqueta del tipo de dispositivo (auto-descubierto por el clasificador). */
function TypeBadge({ type, className }: { type: DeviceType; className?: string }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        'inline-flex w-fit shrink-0 items-center rounded-full border border-border bg-elevated px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-text-secondary',
        className,
      )}
    >
      {t(`devices.types.${type}`)}
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

interface StatCard {
  key: string
  label: string
  icon: LucideIcon
  iconClass: string
  /** Resalta la tarjeta cuando su filtro asociado está activo. */
  active?: boolean
  /** Si existe, la tarjeta entera es clicable (toggle de filtro). */
  onClick?: () => void
  /** aria-label/title cuando la tarjeta es clicable. */
  actionLabel?: string
  render: () => React.ReactNode
}

function StatsStrip({
  allDevices,
  onlyUnprotected,
  onToggleUnprotected,
}: {
  allDevices: ClientDevice[]
  onlyUnprotected: boolean
  onToggleUnprotected: () => void
}) {
  const { t } = useTranslation()
  const { refreshKey } = useDashboard()
  const { deviceTotals } = useNetPulse()
  const reduce = useReducedMotion()
  const newThisWeekDevices = allDevices.filter((d) => d.isNew)
  // #1003: el umbral de señal débil es el ajuste global (/api/settings/thresholds),
  // no un -70 hardcodeado; mismo criterio que la matriz de roaming (#906).
  const weakDbm = useWeakSignalDbm()
  const weakSignalCount = allDevices.filter((d) => d.online && d.signalDbm !== null && d.signalDbm < weakDbm).length
  const adguardProtected = allDevices.filter((d) => d.adguard).length
  // Salud de roaming (#771): MACs con más conexiones AP-STA en 24 h (los
  // primeros puestos son los que más rebotan). En demo, muestra local (el
  // modo demo no tiene sesión API).
  const { isDemo } = useNetPulse()
  const [roaming, setRoaming] = useState<{ mac: string; name: string; connects: number }[]>([])
  useEffect(() => {
    if (isDemo) {
      setRoaming([{ mac: 'demo', name: 'Robot aspirador', connects: 6 }])
      return
    }
    let cancelled = false
    fetchJson<{ items: { mac: string; name: string; connects: number }[] }>('/api/presence/roaming')
      .then((res) => {
        if (!cancelled && res.ok && res.data) setRoaming(res.data.items)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [refreshKey, isDemo])
  const roamingTop = roaming[0]
  // Cobertura AdGuard completa: solo cuando TODOS los clientes están
  // protegidos (no basta pct==100: el redondeo daría 100 con 199/200).
  const allProtected = deviceTotals.total > 0 && adguardProtected >= deviceTotals.total
  const cards: StatCard[] = [
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
              <div key={d.id} className="truncate py-0.5 text-xs text-text-secondary" translate="no">
                {d.name}
                <span className="text-text-muted"> · {fmtSeenAgo(d.firstSeenMs)}</span>
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
          <div className="mt-1 text-caption text-text-muted">&lt; {weakDbm} dBm</div>
        </>
      ),
    },
    {
      key: 'adguard',
      label: t('devices.stats.adguardProtected'),
      icon: ShieldCheck,
      iconClass: 'text-ok',
      // Con cobertura parcial la tarjeta es clicable: filtra la tabla para
      // mostrar solo los clientes SIN proteger (#959). Con cobertura total
      // no hay nada que filtrar: se muestra el estado de éxito.
      active: !allProtected && onlyUnprotected,
      onClick: allProtected ? undefined : onToggleUnprotected,
      actionLabel: t('devices.stats.showUnprotected'),
      render: () => {
        if (allProtected) {
          return (
            <div className="flex items-center gap-3">
              <ShieldCheck className="h-8 w-8 shrink-0 text-ok" strokeWidth={1.75} />
              <div className="text-caption text-text-muted">{t('devices.stats.pctClients', { pct: 100 })}</div>
            </div>
          )
        }
        // Guard NaN (#222): antes del primer snapshot live total es 0 y la
        // división daría NaN en el texto del porcentaje.
        const pct = deviceTotals.total > 0 ? Math.round((adguardProtected / deviceTotals.total) * 100) : 0
        return (
          <>
            <div className="font-mono text-stat text-text-primary">
              <CountUp value={adguardProtected} nonce={refreshKey} />
              <span className="text-sm font-medium text-text-secondary">/{deviceTotals.total}</span>
            </div>
            <div className="mt-1 text-caption text-text-muted">{t('devices.stats.pctClients', { pct })}</div>
          </>
        )
      },
    },
    {
      key: 'roaming',
      label: t('devices.stats.roaming'),
      icon: ArrowLeftRight,
      iconClass: roamingTop && roamingTop.connects > 3 ? 'text-warn' : 'text-ok',
      render: () => (
        <>
          <div className="font-mono text-stat text-text-primary">
            <CountUp value={roamingTop?.connects ?? 0} nonce={refreshKey} />
          </div>
          <div className="mt-1 truncate text-caption text-text-muted" translate="no">
            {roamingTop ? roamingTop.name || roamingTop.mac : t('devices.stats.roamingHealthy')}
          </div>
        </>
      ),
    },
  ]
  return (
    <div className="grid grid-cols-2 gap-4 lg:grid-cols-5">
      {cards.map((c, i) => (
        <motion.div
          key={c.key}
          initial={reduce ? false : { opacity: 0, y: 16 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.4, ease: 'easeOut', delay: 0.05 + i * 0.08 }}
          role={c.onClick ? 'button' : undefined}
          tabIndex={c.onClick ? 0 : undefined}
          aria-pressed={c.onClick ? c.active : undefined}
          aria-label={c.onClick ? c.actionLabel : undefined}
          title={c.onClick ? c.actionLabel : undefined}
          onClick={c.onClick}
          onKeyDown={
            c.onClick
              ? (e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    c.onClick!()
                  }
                }
              : undefined
          }
          className={cn(
            'flex min-h-[104px] flex-col justify-between rounded-2xl border bg-surface p-4',
            c.active ? 'border-accent/50 bg-accent-soft/40' : 'border-border',
            c.onClick && 'cursor-pointer transition-colors hover:border-accent/40 hover:bg-hover/50',
          )}
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

// #989: trigger homogéneo de los filtros desplegables (Flota, Conexión y
// Tipo): icono Filter + etiqueta + badge con el nº de valores activos.
function FilterMenuTrigger({ label, active }: { label: string; active: number }) {
  return (
    <button className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border bg-elevated px-3 text-xs font-medium text-text-secondary transition-colors hover:bg-hover hover:text-text-primary">
      <Filter className="h-3.5 w-3.5" strokeWidth={1.75} />
      {label}
      {active > 0 && (
        <span className="rounded-full bg-accent-soft px-1.5 py-0.5 text-[10px] font-semibold text-accent">
          {active}
        </span>
      )}
    </button>
  )
}

interface FilterBarProps {
  routerIds: string[]
  toggleRouterId: (id: string) => void
  routerCounts: Record<string, number>
  bands: BandValue[]
  toggleBand: (b: BandValue) => void
  bandCounts: Partial<Record<BandValue, number>>
  /** #991: la red tiene banda 6 GHz (si no, la opción no se ofrece) */
  hasBand6: boolean
  groups: FilterGroup[]
  toggleGroup: (g: FilterGroup) => void
  online: OnlineFilter
  setOnline: (v: OnlineFilter) => void
  onlyWeak: boolean
  setOnlyWeak: (v: boolean) => void
  weakCount: number
  /** #1003: umbral configurado, se muestra en la etiqueta del chip */
  weakDbm: number
  view: 'list' | 'grid'
  setView: (v: 'list' | 'grid') => void
  shown: number
  groupCounts: Record<FilterGroup, number>
  sort: SortState
  setSortKey: (key: SortKey | null) => void
}

function FilterBar(p: FilterBarProps) {
  const { t } = useTranslation()
  const { routers, deviceTotals } = useNetPulse()
  return (
    <div className="rounded-2xl border border-border bg-surface p-3 md:p-4">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-3">
        {/* Flota (#989): multi-selección acumulativa de routers; una píldora
            por router no escala cuando la flota crece (6 -> ~15 routers). */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <FilterMenuTrigger label={t('devices.fleet')} active={p.routerIds.length} />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-56">
            <DropdownMenuLabel>{t('devices.filterByRouter')}</DropdownMenuLabel>
            <DropdownMenuSeparator />
            {routers.map((r) => (
              <DropdownMenuCheckboxItem
                key={r.id}
                checked={p.routerIds.includes(r.id)}
                onCheckedChange={() => p.toggleRouterId(r.id)}
                onSelect={(e) => e.preventDefault()}
              >
                <span className={cn('mr-1.5 h-1.5 w-1.5 shrink-0 rounded-full', ROUTER_DOT[r.id] ?? 'bg-text-muted')} />
                <span className="flex-1">{r.name}</span>
                <span className="ml-2 font-mono text-caption text-text-muted">{p.routerCounts[r.id] ?? 0}</span>
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
        {/* Conexión (#989): multi-selección de banda/cable; 6 GHz solo si la
            red la tiene (#991) */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <FilterMenuTrigger label={t('devices.connection')} active={p.bands.length} />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-56">
            <DropdownMenuLabel>{t('devices.filterByBand')}</DropdownMenuLabel>
            <DropdownMenuSeparator />
            {BAND_FILTERS.filter((o) => !o.band6 || p.hasBand6).map((o) => (
              <DropdownMenuCheckboxItem
                key={o.value}
                checked={p.bands.includes(o.value)}
                onCheckedChange={() => p.toggleBand(o.value)}
                onSelect={(e) => e.preventDefault()}
              >
                <span className="flex-1">{o.label ?? t(o.labelKey!)}</span>
                <span className="ml-2 font-mono text-caption text-text-muted">{p.bandCounts[o.value] ?? 0}</span>
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
        {/* Tipo (multi) */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <FilterMenuTrigger label={t('devices.colType')} active={p.groups.length} />
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
        {/* Estado online: All / Online / Offline (#923) */}
        <SegmentedControl<OnlineFilter>
          options={[
            { value: 'all', label: t('devices.onlineAll') },
            { value: 'online', label: t('devices.onlineOnly') },
            { value: 'offline', label: t('devices.onlineOffline') },
          ]}
          value={p.online}
          onChange={p.setOnline}
          size="sm"
          ariaLabel={t('devices.filterByOnline')}
        />
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
            {t('devices.weakChip', { count: p.weakCount, dbm: p.weakDbm })}
          </button>
        )}
        {/* Orden (#799): visible solo en móvil; en desktop la cabecera sticky
            de la lista lleva los SortHeader */}
        <div className="md:hidden">
          <Select
            value={p.sort?.key ?? 'default'}
            onValueChange={(v) => p.setSortKey(v === 'default' ? null : (v as SortKey))}
          >
            <SelectTrigger
              aria-label={t('devices.sortBy')}
              className="h-8 w-auto gap-1.5 rounded-lg border-border bg-elevated px-2.5 text-xs font-medium text-text-secondary"
            >
              <ArrowUpDown className="h-3.5 w-3.5" strokeWidth={1.75} />
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="default">{t('devices.sortDefault')}</SelectItem>
              <SelectItem value="name">{t('devices.colDevice')}</SelectItem>
              <SelectItem value="traffic">{t('devices.colTraffic')}</SelectItem>
              <SelectItem value="ip">IP / MAC</SelectItem>
              <SelectItem value="router">Router</SelectItem>
              <SelectItem value="band">{t('devices.colBand')}</SelectItem>
              <SelectItem value="signal">{t('devices.colSignal')}</SelectItem>
              <SelectItem value="lease">{t('devices.colLease')}</SelectItem>
              <SelectItem value="type">{t('devices.colType')}</SelectItem>
              <SelectItem value="firstSeen">{t('devices.colFirstSeen')}</SelectItem>
              <SelectItem value="lastSeen">{t('devices.colLastSeen')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
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
  infra,
  onEdit,
}: {
  device: ClientDevice
  infra?: InfraInfo
  onEdit: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="grid grid-cols-2 gap-x-4 gap-y-4 px-4 py-4 md:grid-cols-3 md:px-5">
      <DetailItem label="MAC" mono>
        {device.mac}
      </DetailItem>
      <DetailItem label={t('devices.detail.dhcpLease')}>{dhcpLease(device.dhcpLease)}</DetailItem>
      <DetailItem label={t('devices.detail.firstSeen')}>{fmtSeenAgo(device.firstSeenMs)}</DetailItem>
      <DetailItem label={t('devices.detail.lastSeen')}>{fmtSeenAgo(device.lastSeenMs)}</DetailItem>
      <DetailItem label={t('devices.detail.manufacturer')}>{manufacturerLabel(device.manufacturer)}</DetailItem>
      <DetailItem label="Hostname" mono>
        {device.hostname}
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
      <ConnectedToItem device={device} />
      {infra && (
        <DetailItem label={t('devices.detail.infra')}>
          <InfraBadge info={infra} />
          {(infra.kind === 'ct' || infra.kind === 'vm') && infra.host && (
            <span className="ml-1.5 text-caption text-text-muted">
              {t(`devices.badges.${infra.kind}Tip`, { host: infra.host })}
            </span>
          )}
        </DetailItem>
      )}
      {/* Editar a la derecha, sin texto auxiliar (#985). */}
      <div className="col-span-2 flex justify-end md:col-span-3">
        <button
          type="button"
          onClick={onEdit}
          className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-elevated px-3 py-2 text-sm font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
        >
          <Pencil className="h-3.5 w-3.5" strokeWidth={1.75} />
          {t('devices.edit.action')}
        </button>
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
  const Icon = deviceIcon(device)
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
/** "via <AP or switch>", under the router chip; nothing when unknown. */
function ParentLine({ device, className }: { device: ClientDevice; className?: string }) {
  const { t } = useTranslation()
  const parent = useParentName(device.attachTo)
  if (!parent) return null
  return (
    <div className={cn('truncate text-caption text-text-muted', className)} title={parent}>
      {t('devices.via', { name: parent })}
    </div>
  )
}

/** Detail row: the parent box and, for a wired client, the port on it. */
function ConnectedToItem({ device }: { device: ClientDevice }) {
  const { t } = useTranslation()
  const parent = useParentName(device.attachTo)
  if (!parent) return null
  const port = device.portLabel ?? device.port ?? ''
  return (
    <DetailItem label={t('devices.detail.connectedTo')}>
      {port ? `${parent} · ${port}` : parent}
    </DetailItem>
  )
}

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

function LeaseCell({ device }: { device: ClientDevice }) {
  const { t } = useTranslation()
  if (device.leaseRemaining == null || device.leaseRemaining <= 0) {
    return <span className="font-mono text-mono-sm text-text-muted">{t('devices.leaseUnlimited')}</span>
  }
  const days = Math.floor(device.leaseRemaining / 86400)
  const hours = Math.floor((device.leaseRemaining % 86400) / 3600)
  const minutes = Math.floor((device.leaseRemaining % 3600) / 60)
  if (days > 0) return <span className="font-mono text-mono-sm text-text-secondary">{t('devices.leaseDaysHours', { days, hours })}</span>
  if (hours > 0) return <span className="font-mono text-mono-sm text-text-secondary">{t('devices.leaseHoursMinutes', { hours, minutes })}</span>
  return <span className="font-mono text-mono-sm text-warn">{t('devices.leaseMinutes', { minutes })}</span>
}

// #958: la tabla es un grid CSS cuyas columnas dependen de las columnas
// visibles elegidas por el usuario. Las plantillas se generan aquí y se
// inyectan como custom properties (--devices-cols-*) que la clase
// `devices-cols` (index.css) aplica en cada breakpoint; Tailwind no puede
// generar clases dinámicas para todas las combinaciones posibles.
// Pesos fr NATURALES de cada columna (con todo visible, la plantilla es
// idéntica al ROW_GRID original).
const COLUMN_SPECS: Record<DeviceColumn, { md?: number; lg: number; xl?: number; xlOnly?: boolean }> = {
  type: { lg: 0.55 },
  ip: { lg: 0.95 },
  lease: { lg: 0.7 },
  router: { md: 1.1, lg: 0.85 },
  band: { md: 0.85, lg: 0.6 },
  signal: { md: 0.95, lg: 0.75 },
  traffic: { md: 0.75, lg: 0.75, xl: 0.7 },
  firstSeen: { lg: 0.7, xlOnly: true },
  lastSeen: { lg: 0.7, xlOnly: true },
}

const DEVICE_COL_FR = { md: 3, lg: 3.4, xl: 3.4 } as const

/** Reparto al ocultar columnas: el fr liberado se distribuye A PARTES IGUALES
 *  entre las columnas visibles (dispositivo incluida). El reparto nativo de
 *  CSS es proporcional al peso, así que la columna de dispositivo (3.4fr
 *  frente a 0.55-1.1fr del resto) absorbía casi todo el hueco liberado. */
function distributeColumns(all: { key: string; fr: number }[], vis: Set<DeviceColumn>): string {
  const visibleCols = all.filter((c) => vis.has(c.key as DeviceColumn) || c.key === 'device')
  const hiddenFr = all.filter((c) => !vis.has(c.key as DeviceColumn) && c.key !== 'device').reduce((a, c) => a + c.fr, 0)
  const bonus = hiddenFr / visibleCols.length
  return visibleCols.map((c) => `minmax(0,${(c.fr + bonus).toFixed(2)}fr)`).join(' ')
}

function columnTemplates(vis: Set<DeviceColumn>) {
  const frOf = (c: DeviceColumn, bp: 'md' | 'lg' | 'xl'): number =>
    bp === 'md' ? COLUMN_SPECS[c].md! : bp === 'lg' ? COLUMN_SPECS[c].lg : (COLUMN_SPECS[c].xl ?? COLUMN_SPECS[c].lg)
  const colsFor = (bp: 'md' | 'lg' | 'xl'): { key: string; fr: number }[] => [
    { key: 'device', fr: DEVICE_COL_FR[bp] },
    ...DEVICE_COLUMNS.filter((c) =>
      bp === 'md' ? !!COLUMN_SPECS[c].md : bp === 'lg' ? !COLUMN_SPECS[c].xlOnly : true,
    ).map((c) => ({ key: c as string, fr: frOf(c, bp) })),
  ]
  const md = [distributeColumns(colsFor('md'), vis), '1.5rem'].join(' ')
  const lg = [distributeColumns(colsFor('lg'), vis), '1.5rem'].join(' ')
  const xl = [distributeColumns(colsFor('xl'), vis), '1.5rem'].join(' ')
  return { '--devices-cols-md': md, '--devices-cols-lg': lg, '--devices-cols-xl': xl } as React.CSSProperties
}

/** Fila de tabla desktop (md+) */
function ListRow({
  device,
  infra,
  expanded,
  index,
  vis,
  colsStyle,
  onToggle,
  onCopyIp,
  onNavigateRouter,
  onEdit,
}: {
  device: ClientDevice
  infra?: InfraInfo
  expanded: boolean
  index: number
  vis: Set<DeviceColumn>
  colsStyle: React.CSSProperties
  onToggle: () => void
  onCopyIp: (ip: string) => void
  onNavigateRouter: (id: string) => void
  onEdit: () => void
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
        style={colsStyle}
        className={cn(
          'devices-cols hidden cursor-pointer items-center gap-4 pr-6 pl-3 py-2 transition-colors duration-150 hover:bg-hover md:grid',
          !device.online && 'opacity-55',
          expanded && 'bg-hover/60',
        )}
      >
        {/* Dispositivo */}
        <div className="group flex min-w-0 items-center gap-3">
          <DeviceTile device={device} />
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="truncate text-sm font-medium text-text-primary" translate="no">{device.name}</span>
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation()
                  onEdit()
                }}
                aria-label={t('devices.edit.action')}
                className="rounded-md p-1 text-text-muted opacity-0 transition-colors hover:text-accent group-hover:opacity-100 focus:opacity-100"
              >
                <Pencil className="h-3.5 w-3.5" strokeWidth={1.75} />
              </button>
              {device.isNew && <NewPill />}
              {infra && <InfraBadge info={infra} />}
              <TypeBadge type={device.type} className="lg:hidden" />
              {!device.online && <StatusPill tone="muted" label={t('common.status.offline')} />}
            </div>
            <div className="truncate text-caption text-text-muted">{manufacturerLabel(device.manufacturer)}</div>
          </div>
        </div>
        {/* Tipo */}
        {vis.has('type') && (
          <div className="hidden lg:block">
            <TypeBadge type={device.type} />
          </div>
        )}
        {/* IP / MAC */}
        {vis.has('ip') && (
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
        )}
        {/* Tiempo restante del lease */}
        {vis.has('lease') && (
          <div className="hidden lg:block">
            <LeaseCell device={device} />
          </div>
        )}
        {/* Router, and the box the client actually hangs off */}
        {vis.has('router') && (
          <div className="min-w-0">
            <RouterChipLink routerId={device.routerId} onNavigate={onNavigateRouter} />
            <ParentLine device={device} className="mt-0.5 pl-1" />
          </div>
        )}
        {/* Banda */}
        {vis.has('band') && (
          <div>
            <BandChip band={device.band} />
          </div>
        )}
        {/* Señal */}
        {vis.has('signal') && (
          <div>
            <SignalCell device={device} />
          </div>
        )}
        {/* Tráfico en vivo (#799): la columna que ordena el default */}
        {vis.has('traffic') && (
          <div>
            <span className="font-mono text-mono-sm text-accent">
              {device.online
                ? `${device.trafficMbps >= 1 ? fmtEs(device.trafficMbps, 1) : fmtEs(device.trafficMbps, 2)} Mbps`
                : '—'}
            </span>
          </div>
        )}
        {/* First/Last seen (#954): solo en xl para no engordar la tabla */}
        {vis.has('firstSeen') && (
          <div className="hidden xl:block">
            <span className="text-caption text-text-secondary">{fmtSeenAgo(device.firstSeenMs)}</span>
          </div>
        )}
        {vis.has('lastSeen') && (
          <div className="hidden xl:block">
            <span className="text-caption text-text-secondary">{fmtSeenAgo(device.lastSeenMs)}</span>
          </div>
        )}
        <ChevronDown
          className={cn('h-4 w-4 justify-self-end text-text-muted transition-transform duration-200', expanded && 'rotate-180')}
          strokeWidth={1.75}
        />
      </div>
      {/* Móvil: DeviceRow compartido en card */}
      <div className={cn('md:hidden', !device.online && 'opacity-55')}>
        <DeviceRow device={device} variant="full" onClick={onToggle} />
      </div>
      <ExpandPanel open={expanded}>
        <DeviceDetail device={device} infra={infra} onEdit={onEdit} />
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
  infra,
  expanded,
  index,
  onToggle,
  onNavigateRouter,
  onEdit,
}: {
  device: ClientDevice
  infra?: InfraInfo
  expanded: boolean
  index: number
  onToggle: () => void
  onNavigateRouter: (id: string) => void
  onEdit: () => void
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
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation()
                onEdit()
              }}
              aria-label={t('devices.edit.action')}
              className="rounded-md p-1.5 text-text-muted transition-colors hover:bg-hover hover:text-accent"
            >
              <Pencil className="h-3.5 w-3.5" strokeWidth={1.75} />
            </button>
            {!device.online ? <StatusPill tone="muted" label={t('common.status.offline')} /> : device.isNew ? <NewPill /> : null}
          </div>
        </div>
        <div className="mt-3 flex items-center gap-2">
          <span className="truncate text-sm font-medium text-text-primary" translate="no">{device.name}</span>
          {infra && <InfraBadge info={infra} />}
          <TypeBadge type={device.type} />
        </div>
        <div className="truncate text-caption text-text-muted">
          {manufacturerLabel(device.manufacturer)} · <span className="font-mono">{device.ip}</span>
        </div>
        <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
          <RouterChipLink routerId={device.routerId} onNavigate={onNavigateRouter} />
          <BandChip band={device.band} />
        </div>
        <ParentLine device={device} className="mt-1 pl-1" />
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
        <DeviceDetail device={device} infra={infra} onEdit={onEdit} />
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
  const { devices, deviceTotals, isDemo, routers, distributionNodes, refresh } = useNetPulse()
  const [deviceOverrides, setDeviceOverrides] = useState<Record<string, { iconOverride?: string; nameOverride?: string; typeOverride?: string }>>(() => {
    try {
      const raw = localStorage.getItem('netpulse-device-overrides')
      return raw ? (JSON.parse(raw) as Record<string, { iconOverride?: string; nameOverride?: string; typeOverride?: string }>) : {}
    } catch {
      return {}
    }
  })

  // Lista enriquecida de clientes: en demo local expande el canon a los 65
  // del dataset reconciliado; en live solo fusiona metadatos conocidos sobre
  // la API. D6: los equipos de infraestructura (host hipervisor, sus CTs y
  // switches gestionados con LLDP) se reclasifican al grupo de filtro 'infra'.
  const { allDevices, infraById } = useMemo(() => {
    const list = buildClientDevices(devices, isDemo).map((d) => {
      const ov = deviceOverrides[d.id]
      // '' o ausente = automático: en ese caso manda el valor del server
      // (que en live ya trae los overrides aplicados, #797).
      return {
        ...d,
        iconOverride: ov?.iconOverride || d.iconOverride,
        name: ov?.nameOverride || d.name,
        type: (ov?.typeOverride || d.type) as ClientDevice['type'],
        nameOverride: ov?.nameOverride || d.nameOverride,
        typeOverride: ov?.typeOverride || d.typeOverride,
      }
    })
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
      } else if (sealed === 'vm') {
        infra.set(d.id, { kind: 'vm', host: byId.get(d.attachTo ?? '')?.name ?? d.attachTo })
      } else if (sealed === 'ct' || (!sealed && d.attachTo && hosts.has(d.attachTo))) {
        infra.set(d.id, { kind: 'ct', host: byId.get(d.attachTo ?? '')?.name ?? d.attachTo })
      } else if (sealed === 'managed-switch' || (!sealed && d.lldp)) {
        infra.set(d.id, { kind: 'managedSwitch' })
      }
    }
    if (infra.size === 0) return { allDevices: list, infraById: infra }
    return { allDevices: list.map((d) => (infra.has(d.id) ? { ...d, group: 'infra' as const } : d)), infraById: infra }
  }, [devices, isDemo, distributionNodes, deviceOverrides])

  const [searchParams, setSearchParams] = useSearchParams()
  const [query, setQuery] = useState(() => searchParams.get('q') ?? '')
  const [q, setQ] = useState(() => (searchParams.get('q') ?? '').trim().toLowerCase())

  // Deep-link de onboarding (#772): /devices?intake=<mac> abre la tarjeta de
  // alta para ese dispositivo (la alerta "desconocido" lo enlaza).
  useEffect(() => {
    const mac = searchParams.get('intake')
    if (!mac) return
    const hit = allDevices.find((d) => d.mac.toUpperCase() === mac.toUpperCase())
    if (hit) setIntakeId(hit.id)
  }, [searchParams, allDevices])
  // #989: flota y conexión son multi-selección acumulativa (varios routers o
  // bandas a la vez, OR dentro del filtro y AND entre filtros), con el mismo
  // patrón de desplegable con checkboxes que el filtro "Tipo".
  const [routerIds, setRouterIds] = useState<string[]>([])
  const [bands, setBands] = useState<BandValue[]>([])
  const [groups, setGroups] = useState<FilterGroup[]>([])
  const [online, setOnlineState] = useState<OnlineFilter>(() => loadDevicesPrefs().online)
  const setOnline = useCallback((v: OnlineFilter) => {
    setOnlineState(v)
  }, [])
  const [onlyWeak, setOnlyWeak] = useState(false)
  // #1003: umbral de señal débil desde el ajuste global (como Roaming #906).
  const weakDbm = useWeakSignalDbm()
  // Filtro "sin proteger por AdGuard" (#959): se activa/desactiva desde la
  // stat card de AdGuard; se muestra como pill de filtro activo.
  const [onlyUnprotected, setOnlyUnprotected] = useState(false)
  const toggleUnprotected = useCallback(() => setOnlyUnprotected((v) => !v), [])

  // Enlaces entrantes tipo /devices?q=<mac|ip|nombre> (p.ej. desde Puertos)
  useEffect(() => {
    const incoming = searchParams.get('q')
    if (incoming !== null) {
      setQuery(incoming)
      setQ(incoming.trim().toLowerCase())
      // A deep link asks for one specific device, so the online-only default
      // must not hide it: an alert about a device that has since dropped off
      // would otherwise land on an empty list.
      setOnline('all')
    }
  }, [searchParams, setOnline])

  const weakCount = useMemo(
    () => allDevices.filter((d) => d.online && d.signalDbm !== null && d.signalDbm < weakDbm).length,
    [allDevices, weakDbm],
  )
  // #772: conectados ahora sin identificar (sin nombre ni hostname DHCP):
  // candidatos a la tarjeta de alta. Un dispositivo conocido que reconecta
  // resuelve su lease en 1-2 ticks y desaparece de aquí solo.
  const namelessDevices = useMemo(
    () => allDevices.filter((d) => d.name === d.mac && d.online),
    [allDevices],
  )
  const [view, setView] = useState<'list' | 'grid'>(() => loadDevicesPrefs().view)
  // #958: columnas ocultas de la tabla (persistidas en DevicesPrefs).
  const [hiddenColumns, setHiddenColumns] = useState<DeviceColumn[]>(() => loadDevicesPrefs().columns)
  const toggleColumn = useCallback((c: DeviceColumn) => {
    setHiddenColumns((prev) => (prev.includes(c) ? prev.filter((x) => x !== c) : [...prev, c]))
  }, [setHiddenColumns])
  const visibleColumns = useMemo(() => new Set(DEVICE_COLUMNS.filter((c) => !hiddenColumns.includes(c))), [hiddenColumns])
  const colsStyle = useMemo(() => columnTemplates(visibleColumns), [visibleColumns])
  const [expandedId, setExpandedId] = useState<string | null>(null)
  // Alta guiada de desconocidos (#772): se abre con ?intake=<mac> (deep-link
  // desde la alerta "dispositivo desconocido") o desde el banner de sin
  // identificar.
  const [intakeId, setIntakeId] = useState<string | null>(null)
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

  const groupCounts = useMemo(() => {
    const counts = Object.fromEntries(GROUP_ORDER.map((g) => [g, 0])) as Record<FilterGroup, number>
    for (const d of allDevices) counts[d.group]++
    return counts
  }, [allDevices])

  // #989: contadores por router y por banda para los menús multi-selección.
  const routerCounts = useMemo(() => {
    const counts: Record<string, number> = {}
    for (const d of allDevices) counts[d.routerId] = (counts[d.routerId] ?? 0) + 1
    return counts
  }, [allDevices])

  const bandCounts = useMemo(() => {
    const counts: Partial<Record<BandValue, number>> = {}
    for (const d of allDevices) {
      const b = d.band as BandValue
      counts[b] = (counts[b] ?? 0) + 1
    }
    return counts
  }, [allDevices])

  // #991: 6 GHz solo existe en la UI si la red la tiene. bandsEffective
  // descarta una selección 6 GHz residual si la banda desaparece del bundle.
  const hasBand6 = useMemo(() => networkHasBand6(routers, devices), [routers, devices])
  const bandsEffective = useMemo(
    () => (hasBand6 ? bands : bands.filter((b) => b !== '6 GHz')),
    [bands, hasBand6],
  )

  const [sort, setSort] = useState<SortState>(() => loadDevicesPrefs().sort)
  const toggleSort = useCallback((key: SortKey) => {
    setSort((prev) => (prev?.key === key ? { key, dir: prev.dir === 1 ? -1 : 1 } : { key, dir: 1 }))
  }, [setSort])

  // Selector compacto de orden (móvil, #799): fija la clave en ascendente o
  // vuelve al orden por defecto (tráfico) con null.
  const setSortKey = useCallback((key: SortKey | null) => {
    setSort(key === null ? null : { key, dir: 1 })
  }, [setSort])

  // Persiste vista + orden + columnas en cada cambio (issues #778, #958).
  // El write inicial reescribe el mismo valor cargado; es inofensivo.
  useEffect(() => {
    saveDevicesPrefs({ view, sort, online, columns: hiddenColumns })
  }, [view, sort, online, hiddenColumns])

  const filtered = useMemo(() => {
    const out = allDevices.filter((d) => {
      if (online === 'online' && !d.online) return false
      if (online === 'offline' && d.online) return false
      if (onlyWeak && !(d.online && d.signalDbm !== null && d.signalDbm < weakDbm)) return false
      if (onlyUnprotected && d.adguard) return false
      if (routerIds.length > 0 && !routerIds.includes(d.routerId)) return false
      if (bandsEffective.length > 0 && !bandsEffective.includes(d.band as BandValue)) return false
      if (groups.length > 0 && !groups.includes(d.group)) return false
      if (q) {
        const hay = `${d.name} ${d.ip} ${d.mac} ${d.manufacturer} ${d.hostname}`.toLowerCase()
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
            c = a.name.localeCompare(b.name, numLocale())
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
          case 'lease':
            c = (a.leaseRemaining ?? Number.MAX_SAFE_INTEGER) - (b.leaseRemaining ?? Number.MAX_SAFE_INTEGER)
            break
          case 'signal':
            c = (a.signalDbm ?? -999) - (b.signalDbm ?? -999)
            break
          case 'type':
            c = a.type.localeCompare(b.type)
            break
          case 'traffic':
            c = a.trafficMbps - b.trafficMbps
            break
          case 'firstSeen':
            c = (a.firstSeenMs ?? 0) - (b.firstSeenMs ?? 0)
            break
          case 'lastSeen':
            c = (a.lastSeenMs ?? 0) - (b.lastSeenMs ?? 0)
            break
        }
        if (c !== 0) return dir * c
        // Desempate: online primero, luego nombre
        if (a.online !== b.online) return a.online ? -1 : 1
        return a.name.localeCompare(b.name, numLocale())
      })
    }
    // Online primero (por tráfico desc), conocidos offline al final (devices.md §④)
    // #799: orden por defecto ESTABLE: el tráfico se cuantiza en buckets de
    // 0.1 Mbps para que las micro-fluctuaciones de equipos inactivos no
    // reordenen la lista en cada refresco; desempate por nombre.
    const trafficBucket = (mbps: number) => Math.round(mbps * 10) / 10
    return out.sort((a, b) => {
      if (a.online !== b.online) return a.online ? -1 : 1
      if (a.online) {
        const c = trafficBucket(b.trafficMbps) - trafficBucket(a.trafficMbps)
        if (c !== 0) return c
      }
      return a.name.localeCompare(b.name, numLocale())
    })
  }, [allDevices, online, onlyWeak, weakDbm, onlyUnprotected, routerIds, bandsEffective, groups, q, sort, routers])

  const toggleRouterId = useCallback(
    (id: string) => setRouterIds((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id])),
    [setRouterIds],
  )

  const toggleBand = useCallback(
    (b: BandValue) => setBands((prev) => (prev.includes(b) ? prev.filter((x) => x !== b) : [...prev, b])),
    [setBands],
  )

  const toggleGroup = useCallback(
    (g: FilterGroup) => setGroups((prev) => (prev.includes(g) ? prev.filter((x) => x !== g) : [...prev, g])),
    [setGroups],
  )

  const clearFilters = useCallback(() => {
    setRouterIds([])
    setBands([])
    setGroups([])
    setOnline('online')
    setOnlyUnprotected(false)
  }, [setRouterIds, setBands, setGroups, setOnline, setOnlyUnprotected])

  const clearAll = useCallback(() => {
    clearFilters()
    setQuery('')
  }, [clearFilters])

  const copyIp = useCallback(
    (ip: string) => {
      void copyToClipboard(ip).then((ok) => {
        showToast(t(ok ? 'devices.ipCopied' : 'devices.ipCopyFailed'))
      })
    },
    [showToast, t],
  )

  const [editingId, setEditingId] = useState<string | null>(null)
  const [savingOverride, setSavingOverride] = useState(false)

  const persistOverride = useCallback(
    (id: string, patch: { iconOverride: string; nameOverride: string; typeOverride: string } | null) => {
      setDeviceOverrides((prev) => {
        const next = { ...prev }
        if (patch && (patch.iconOverride || patch.nameOverride || patch.typeOverride)) {
          next[id] = {
            iconOverride: patch.iconOverride,
            nameOverride: patch.nameOverride,
            typeOverride: patch.typeOverride,
          }
        } else {
          delete next[id]
        }
        try {
          localStorage.setItem('netpulse-device-overrides', JSON.stringify(next))
        } catch {
          /* ignore */
        }
        return next
      })
    },
    []
  )

  // #797: el patch es absoluto ('' = volver al automático); antes el early
  // return de "sin cambios" impedía limpiar un override (p. ej. volver al
  // icono Auto).
  const handleEditSave = useCallback(
    async (device: ClientDevice, patch: { icon: string; name: string; type: string }) => {
      persistOverride(device.id, {
        iconOverride: patch.icon,
        nameOverride: patch.name,
        typeOverride: patch.type,
      })

      if (!isDemo) {
        setSavingOverride(true)
        const res = await fetchJson(`/api/devices/${encodeURIComponent(device.mac)}/override`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ icon: patch.icon, name: patch.name, type: patch.type }),
        })
        setSavingOverride(false)
        if (!res.ok) {
          persistOverride(device.id, null)
          showToast(t('devices.edit.saveError'))
          return
        }
      }

      setEditingId(null)
      showToast(t('devices.edit.saved'))
    },
    [isDemo, persistOverride, showToast, t]
  )

  const navigateRouter = useCallback((id: string) => navigate(`/routers/${id}`), [navigate])

  const pills = useMemo<Pill[]>(() => {
    const list: Pill[] = []
    for (const id of routerIds) {
      const routerName = routers.find((r) => r.id === id)?.name ?? id
      list.push({ key: `router-${id}`, label: routerName, clear: () => toggleRouterId(id) })
    }
    for (const b of bandsEffective) {
      list.push({ key: `band-${b}`, label: b === 'cable' ? t('common.cable') : b, clear: () => toggleBand(b) })
    }
    for (const g of groups) {
      list.push({ key: `group-${g}`, label: t(`devices.groups.${g}`), clear: () => toggleGroup(g) })
    }
    if (onlyUnprotected) {
      list.push({ key: 'unprotected', label: t('devices.stats.unprotectedPill'), clear: () => setOnlyUnprotected(false) })
    }
    if (online !== 'all') {
      list.push({ key: 'online', label: t(online === 'online' ? 'devices.onlineOnly' : 'devices.onlineOffline'), clear: () => setOnline('all') })
    }
    return list
  }, [routerIds, routers, bandsEffective, groups, online, onlyUnprotected, setOnline, toggleRouterId, toggleBand, toggleGroup, t])

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

      {/* Onboarding (#772): dispositivos conectados sin identificar */}
      {namelessDevices.length > 0 && (
        <div className="mt-3 flex flex-wrap items-center gap-3 rounded-xl border border-info/30 bg-info/5 px-4 py-3">
          <Sparkles className="h-4 w-4 shrink-0 text-info" />
          <span className="flex-1 text-sm text-text-secondary">
            {t('devices.onboarding.banner', { count: namelessDevices.length })}
          </span>
          <button
            type="button"
            onClick={() => setIntakeId(namelessDevices[0]!.id)}
            className="rounded-lg bg-accent-soft px-3 py-1.5 text-sm font-medium text-accent transition-colors hover:bg-accent-soft/70"
          >
            {t('devices.onboarding.bannerAction')}
          </button>
        </div>
      )}

      {/* Búsqueda móvil sticky bajo el header */}
      <div className="sticky top-14 z-20 -mx-4 bg-canvas/90 px-4 py-2 backdrop-blur-md md:hidden">
        {searchBox()}
      </div>

      {/* ② Stats strip */}
      <StatsStrip allDevices={allDevices} onlyUnprotected={onlyUnprotected} onToggleUnprotected={toggleUnprotected} />

      {/* ③ Filter bar */}
      <FilterBar
        routerIds={routerIds}
        toggleRouterId={toggleRouterId}
        routerCounts={routerCounts}
        bands={bandsEffective}
        toggleBand={toggleBand}
        bandCounts={bandCounts}
        hasBand6={hasBand6}
        groups={groups}
        toggleGroup={toggleGroup}
        online={online}
        setOnline={setOnline}
        onlyWeak={onlyWeak}
        setOnlyWeak={setOnlyWeak}
        weakCount={weakCount}
        weakDbm={weakDbm}
        view={view}
        setView={setView}
        shown={filtered.length}
        groupCounts={groupCounts}
        sort={sort}
        setSortKey={setSortKey}
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
            style={colsStyle}
            className={cn(
              'devices-cols sticky top-14 z-10 hidden items-center gap-4 pr-6 pl-3 py-2 rounded-t-2xl border-b border-border bg-surface text-label uppercase text-text-muted md:grid',
            )}
          >
            <SortHeader label={t('devices.colDevice')} k="name" sort={sort} onSort={toggleSort} />
            {visibleColumns.has('type') && (
              <span className="hidden lg:block">
                <SortHeader label={t('devices.colType')} k="type" sort={sort} onSort={toggleSort} />
              </span>
            )}
            {visibleColumns.has('ip') && (
              <span className="hidden lg:block">
                <SortHeader label="IP / MAC" k="ip" sort={sort} onSort={toggleSort} />
              </span>
            )}
            {visibleColumns.has('lease') && (
              <span className="hidden lg:block">
                <SortHeader label={t('devices.colLease')} k="lease" sort={sort} onSort={toggleSort} />
              </span>
            )}
            {visibleColumns.has('router') && <SortHeader label="Router" k="router" sort={sort} onSort={toggleSort} />}
            {visibleColumns.has('band') && <SortHeader label={t('devices.colBand')} k="band" sort={sort} onSort={toggleSort} />}
            {visibleColumns.has('signal') && <SortHeader label={t('devices.colSignal')} k="signal" sort={sort} onSort={toggleSort} />}
            {visibleColumns.has('traffic') && <SortHeader label={t('devices.colTraffic')} k="traffic" sort={sort} onSort={toggleSort} />}
            {visibleColumns.has('firstSeen') && (
              <span className="hidden xl:block">
                <SortHeader label={t('devices.colFirstSeen')} k="firstSeen" sort={sort} onSort={toggleSort} />
              </span>
            )}
            {visibleColumns.has('lastSeen') && (
              <span className="hidden xl:block">
                <SortHeader label={t('devices.colLastSeen')} k="lastSeen" sort={sort} onSort={toggleSort} />
              </span>
            )}
            {/* #958: selector de columnas visibles (la última celda, sobre el
                chevron de expansión). El nombre no se puede ocultar. */}
            <ColumnPicker hidden={hiddenColumns} onToggle={toggleColumn} />
          </div>
          <div className="divide-y divide-border p-1.5 md:p-2">
            <AnimatePresence initial={false}>
              {filtered.map((d, i) => (
                <ListRow
                  key={d.id}
                  device={d}
                  infra={infraById.get(d.id)}
                  expanded={expandedId === d.id}
                  index={i}
                  vis={visibleColumns}
                  colsStyle={colsStyle}
                  onToggle={() => setExpandedId((prev) => (prev === d.id ? null : d.id))}
                  onCopyIp={copyIp}
                  onNavigateRouter={navigateRouter}
                  onEdit={() => setEditingId(d.id)}
                />
              ))}
            </AnimatePresence>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5">
          <AnimatePresence initial={false}>
            {filtered.map((d, i) => (
              <GridCard
                key={d.id}
                device={d}
                infra={infraById.get(d.id)}
                expanded={expandedId === d.id}
                index={i}
                onToggle={() => setExpandedId((prev) => (prev === d.id ? null : d.id))}
                onNavigateRouter={navigateRouter}
                onEdit={() => setEditingId(d.id)}
              />
            ))}
          </AnimatePresence>
        </div>
      )}

      <DeviceEditSheet
        open={editingId !== null}
        device={filtered.find((d) => d.id === editingId) ?? null}
        isDemo={isDemo}
        saving={savingOverride}
        onClose={() => setEditingId(null)}
        onSave={handleEditSave}
      />

      <OnboardingIntake
        open={intakeId !== null}
        device={allDevices.find((d) => d.id === intakeId) ?? null}
        isDemo={isDemo}
        onClose={() => {
          setIntakeId(null)
          // Drop ?intake= from the URL once the dialog is closed. The effect
          // that opens it re-runs every time the device list refreshes, so
          // leaving the parameter in place reopened the dialog after every
          // dismissal, indefinitely.
          if (searchParams.has('intake')) {
            const next = new URLSearchParams(searchParams)
            next.delete('intake')
            setSearchParams(next, { replace: true })
          }
        }}
        onSaved={(outcome) => {
          refresh()
          showToast(t(outcome === 'dismissed' ? 'devices.onboarding.dismissed' : 'devices.onboarding.saved'))
        }}
      />

      <Toast toast={toast} />
    </div>
  )
}
