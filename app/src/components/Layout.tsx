import { useEffect, useMemo, useRef, useState } from 'react'
import type { MouseEvent } from 'react'
import { flushSync } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router'
import { AnimatePresence, motion } from 'framer-motion'
import PullToRefresh from '@/components/PullToRefresh'
import {
  BarChart3,
  Bell,
  Bot,
  Bug,
  ChevronsLeft,
  ChevronsRight,
  Download,
  LayoutDashboard,
  MonitorSmartphone,
  MoreHorizontal,
  RefreshCw,
  Router as RouterIcon,
  Settings,
  Waypoints,
  Wifi,
  Wrench,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { useNetPulse } from '@/data/DataProvider'
import { DashboardProvider, useDashboard } from '@/hooks/useDashboard'
import { CommandPalette } from '@/components/CommandPalette'
import { HealthRing } from '@/components/HealthRing'
import InstallPrompt from '@/components/InstallPrompt'
import { ThemeToggle } from '@/components/ThemeToggle'
import { UpdateBanner } from '@/components/UpdateBanner'
import { UpdateConfirmToast } from '@/components/UpdateConfirmToast'
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from '@/components/ui/sheet'
import { useIsMobile } from '@/hooks/use-mobile'
import { cn, exitDemo } from '@/lib/utils'

// ---------------------------------------------------------------------------
// Navegación
// ---------------------------------------------------------------------------

interface NavItem {
  to: string
  labelKey: string
  icon: LucideIcon
  end?: boolean
  adminOnly?: boolean
  /** Badge de etiqueta junto al ítem (p. ej. "labs" para experimentos). */
  badge?: string
}

const NAV_ITEMS: NavItem[] = [
  { to: '/', labelKey: 'nav.overview', icon: LayoutDashboard, end: true },
  { to: '/routers', labelKey: 'nav.routers', icon: RouterIcon },
  { to: '/agents', labelKey: 'nav.agents', icon: Bot },
  { to: '/devices', labelKey: 'nav.devices', icon: MonitorSmartphone },
  { to: '/topology', labelKey: 'nav.topology', icon: Waypoints },
  { to: '/roaming', labelKey: 'nav.roaming', icon: Wifi },
  { to: '/alerts', labelKey: 'nav.alerts', icon: Bell },
  { to: '/reports', labelKey: 'nav.reports', icon: BarChart3 },
  { to: '/orchestration', labelKey: 'nav.orchestration', icon: Wrench, adminOnly: true, badge: 'labs' },
  { to: '/settings', labelKey: 'nav.settings', icon: Settings },
]

/** Badge de alertas sin leer, desde el DataProvider */
function useNavBadge(to: string): number {
  const { unreadAlerts } = useNetPulse()
  return to === '/alerts' ? unreadAlerts : 0
}

const PAGE_TITLE_KEYS: [RegExp, string][] = [
  [/^\/$/, 'nav.overview'],
  [/^\/routers\/[^/]+/, 'nav.routerDetail'],
  [/^\/routers/, 'nav.routers'],
  [/^\/agents/, 'nav.agents'],
  [/^\/devices/, 'nav.devices'],
  [/^\/topology/, 'nav.topology'],
  [/^\/roaming/, 'nav.roaming'],
  [/^\/alerts/, 'nav.alerts'],
  [/^\/reports/, 'nav.reports'],
  [/^\/orchestration/, 'nav.orchestration'],
  [/^\/settings/, 'nav.settings'],
]

function pageTitleKey(pathname: string): string {
  return PAGE_TITLE_KEYS.find(([re]) => re.test(pathname))?.[1] ?? 'nav.overview'
}

/** Pill "Modo demo" (amber sutil) — solo cuando no hay backend */
function DemoPill() {
  const { t } = useTranslation()
  const { isDemo } = useNetPulse()
  if (!isDemo) return null
  return (
    <span className="rounded-md border border-warn/30 bg-warn/10 px-1.5 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-wider text-warn">
      {t('topbar.demoMode')}
    </span>
  )
}

function Logo({ compact = false, onClick }: { compact?: boolean; onClick?: () => void }) {
  const { t } = useTranslation()
  return (
    <Link to="/" onClick={onClick} className="flex items-center gap-2.5" aria-label={`NetPulse — ${t('nav.overview')}`}>
      <span className="flex h-9 w-9 items-center justify-center rounded-xl bg-gradient-to-br from-accent/20 to-tunnel/20 ring-1 ring-accent/30">
        <img src="/logo.svg" alt="" className="h-6 w-6" />
      </span>
      {!compact && (
        <span className="flex items-center gap-2">
          <span className="font-display text-lg font-bold tracking-tight text-text-primary">NetPulse</span>
          <span className="rounded-md bg-elevated px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-text-muted">
            Home
          </span>
          <DemoPill />
        </span>
      )}
    </Link>
  )
}

function NavBadge({ count }: { count: number }) {
  return (
    <span className="ml-auto flex h-5 min-w-5 items-center justify-center rounded-full bg-warn px-1.5 font-mono text-[11px] font-semibold text-canvas">
      {count}
    </span>
  )
}

/** Badge de un item de navegación (solo «Alertas» lo tiene) */
function NavItemBadge({ to, variant }: { to: string; variant: 'sidebar' | 'rail' | 'tab' }) {
  const count = useNavBadge(to)
  if (!count) return null
  if (variant === 'sidebar') return <NavBadge count={count} />
  return (
    <span
      className={cn(
        'absolute flex h-4 min-w-4 items-center justify-center rounded-full bg-warn px-1 font-mono text-[10px] font-semibold text-canvas',
        variant === 'rail' ? 'right-1 top-1' : '-right-1.5 -top-1',
      )}
    >
      {count}
    </span>
  )
}

/** Tarjeta "Estado del gateway" al pie del sidebar (datos del provider) */
function GatewayStatus() {
  const { t } = useTranslation()
  const { routers } = useNetPulse()
  const gw = routers.find((r) => r.roleBadge === 'Principal') ?? routers[0]
  if (!gw) {
    return (
      <div className="flex items-center gap-2.5 rounded-xl bg-elevated px-3 py-2.5">
        <span className="relative flex h-2 w-2">
          <span className="relative inline-flex h-2 w-2 rounded-full bg-text-muted" />
        </span>
        <div className="min-w-0">
          <div className="truncate text-caption font-semibold text-text-secondary">{t('topbar.gatewayStatus')}</div>
          <div className="truncate font-mono text-caption text-text-muted">—</div>
        </div>
      </div>
    )
  }
  const statusLabel = t(`common.status.${gw.status}`)
  const dotClass = gw.status === 'online' ? 'bg-ok' : gw.status === 'warn' ? 'bg-warn' : 'bg-danger'
  return (
    <div className="flex items-center gap-2.5 rounded-xl bg-elevated px-3 py-2.5">
      <span className="relative flex h-2 w-2">
        <span className={cn('absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping-soft', dotClass)} />
        <span className={cn('relative inline-flex h-2 w-2 rounded-full', dotClass)} />
      </span>
      <div className="min-w-0">
        <div className="truncate text-caption font-semibold text-text-secondary">{t('topbar.gatewayStatus')}</div>
        <div className="truncate font-mono text-caption text-text-muted">
          {gw.modelShort.replace('GL.iNet ', '')} · {statusLabel} · {gw.uptime}
        </div>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Sidebar desktop (≥1024px) — 232px
// ---------------------------------------------------------------------------

/** Items de nav visibles según el overview. /roaming solo si hay DAWN. */
function useVisibleNavItems(): NavItem[] {
  const { dawn, orchestration } = useNetPulse()
  const dawnAvailable = !!dawn?.available
  return useMemo(
    () =>
      NAV_ITEMS.filter(
        (it) =>
          (it.to !== '/roaming' || dawnAvailable) &&
          // Orquestación: opt-in del admin (#121). Oculto por defecto.
          (it.to !== '/orchestration' || !!orchestration),
      ),
    [dawnAvailable, orchestration],
  )
}

function Sidebar({ collapsed, onToggleCollapse }: { collapsed: boolean; onToggleCollapse: () => void }) {
  const { t } = useTranslation()
  const items = useVisibleNavItems()
  const main = items.filter((i) => i.to !== '/settings')
  // Settings siempre es el último ítem de NAV_ITEMS y los filtros de
  // useVisibleNavItems nunca lo quitan → garantizado definido.
  const settings = items[items.length - 1]!

  if (collapsed) {
    // Sidebar colapsado = raíl de iconos en lg (persiste en localStorage)
    return (
      <aside className="fixed inset-y-0 left-0 z-40 hidden w-16 flex-col items-center border-r border-border bg-surface lg:flex">
        <div className="flex h-16 items-center pt-safe">
          <Logo compact />
        </div>
        <nav className="flex-1 space-y-1 py-2" aria-label={t('nav.mainNav')}>
          {items.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.end}
              title={t(item.labelKey)}
              className={({ isActive }) =>
                cn(
                  'group relative mx-auto flex h-11 w-11 items-center justify-center rounded-xl transition-colors duration-150',
                  isActive ? 'bg-accent-soft text-accent' : 'text-text-secondary hover:bg-hover hover:text-text-primary',
                )
              }
            >
              <item.icon className="h-5 w-5" strokeWidth={1.75} />
              {item.badge && (
                <span className="absolute right-1 top-1 h-1.5 w-1.5 rounded-full bg-danger" aria-hidden="true" />
              )}
              <NavItemBadge to={item.to} variant="rail" />
              <span className="pointer-events-none absolute left-full z-50 ml-3 hidden whitespace-nowrap rounded-lg border border-border-strong bg-elevated px-2.5 py-1.5 text-caption font-medium text-text-primary shadow-lg group-hover:block">
                {t(item.labelKey)}
                {item.badge && <span className="ml-1.5 text-danger">· {item.badge}</span>}
              </span>
            </NavLink>
          ))}
        </nav>
        <div className="flex flex-col items-center gap-2 border-t border-border py-3">
          <ThemeToggle />
          <button
            type="button"
            onClick={onToggleCollapse}
            aria-label={t('nav.expand')}
            className="flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-elevated text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
          >
            <ChevronsRight className="h-4 w-4" strokeWidth={1.75} />
          </button>
        </div>
      </aside>
    )
  }

  return (
    <aside className="fixed inset-y-0 left-0 z-40 hidden w-[232px] flex-col border-r border-border bg-surface lg:flex">
      <div className="flex h-16 items-center px-5 pt-safe">
        <Logo />
      </div>
      <nav className="flex-1 space-y-1 px-3 py-2" aria-label={t('nav.mainNav')}>
        {main.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.end}
            className={({ isActive }) =>
              cn(
                'group relative flex h-10 items-center gap-3 rounded-lg px-3 text-sm font-medium transition-colors duration-150',
                isActive ? 'bg-accent-soft text-accent' : 'text-text-secondary hover:bg-hover hover:text-text-primary',
              )
            }
          >
            {({ isActive }) => (
              <>
                {isActive && (
                  <motion.span
                    layoutId="nav-indicator"
                    className="absolute left-0 top-1/2 h-5 w-[3px] -translate-y-1/2 rounded-full bg-accent"
                  />
                )}
                <item.icon className="h-[18px] w-[18px] shrink-0" strokeWidth={1.75} />
                {t(item.labelKey)}
                {item.badge && (
                  <span className="ml-auto flex shrink-0 items-center gap-1 rounded-full bg-danger/10 px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider text-danger">
                    <Bug className="h-3 w-3" strokeWidth={2.25} aria-hidden="true" />
                    {item.badge}
                  </span>
                )}
                <NavItemBadge to={item.to} variant="sidebar" />
              </>
            )}
          </NavLink>
        ))}
      </nav>
      <div className="space-y-3 border-t border-border p-3">
        <GatewayStatus />
        <div className="flex items-center gap-2">
          <ThemeToggle />
          <NavLink
            to={settings.to}
            className={({ isActive }) =>
              cn(
                'flex h-9 flex-1 items-center gap-2 rounded-lg px-3 text-sm font-medium transition-colors duration-150',
                isActive ? 'bg-accent-soft text-accent' : 'text-text-secondary hover:bg-hover hover:text-text-primary',
              )
            }
          >
            <settings.icon className="h-[18px] w-[18px]" strokeWidth={1.75} />
            {t('nav.settings')}
          </NavLink>
          <button
            type="button"
            onClick={onToggleCollapse}
            aria-label={t('nav.collapse')}
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-border bg-elevated text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
          >
            <ChevronsLeft className="h-4 w-4" strokeWidth={1.75} />
          </button>
        </div>
      </div>
    </aside>
  )
}

// ---------------------------------------------------------------------------
// Rail tablet (768–1023px) — 64px con tooltips
// ---------------------------------------------------------------------------

function Rail() {
  const { t } = useTranslation()
  const items = useVisibleNavItems()
  return (
    <aside className="fixed inset-y-0 left-0 z-40 hidden w-16 flex-col items-center border-r border-border bg-surface md:flex lg:hidden">
      <div className="flex h-16 items-center pt-safe">
        <Logo compact />
      </div>
      <nav className="flex-1 space-y-1 py-2" aria-label={t('nav.mainNav')}>
        {items.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.end}
            title={t(item.labelKey)}
            className={({ isActive }) =>
              cn(
                'group relative mx-auto flex h-11 w-11 items-center justify-center rounded-xl transition-colors duration-150',
                isActive ? 'bg-accent-soft text-accent' : 'text-text-secondary hover:bg-hover hover:text-text-primary',
              )
            }
          >
            <item.icon className="h-5 w-5" strokeWidth={1.75} />
            {item.badge && (
              <span className="absolute right-1 top-1 h-1.5 w-1.5 rounded-full bg-danger" aria-hidden="true" />
            )}
            <NavItemBadge to={item.to} variant="rail" />
            <span className="pointer-events-none absolute left-full ml-3 hidden whitespace-nowrap rounded-lg border border-border-strong bg-elevated px-2.5 py-1.5 text-caption font-medium text-text-primary shadow-lg group-hover:block">
              {t(item.labelKey)}
              {item.badge && <span className="ml-1.5 text-danger">· {item.badge}</span>}
            </span>
          </NavLink>
        ))}
      </nav>
      <div className="border-t border-border py-3">
        <ThemeToggle />
      </div>
    </aside>
  )
}

// ---------------------------------------------------------------------------
// Campana con badge
// ---------------------------------------------------------------------------

function BellButton() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { unreadAlerts } = useNetPulse()
  return (
    <button
      type="button"
      onClick={() => navigate('/alerts?unread=1')}
      aria-label={t('topbar.alertsUnread', { count: unreadAlerts })}
      className="relative flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-elevated text-text-secondary transition-colors duration-150 hover:border-accent/40 hover:text-accent"
    >
      <Bell className="h-4 w-4" strokeWidth={1.75} />
      {unreadAlerts > 0 && (
        <span className="absolute -right-1 -top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-warn px-1 font-mono text-[10px] font-semibold text-canvas">
          {unreadAlerts}
        </span>
      )}
    </button>
  )
}

// ---------------------------------------------------------------------------
// Topbar (≥768px) — h-14
// ---------------------------------------------------------------------------

/** Pill de estado de conexión en el topbar (live: "En vivo"; reconectando: amber) */
function LivePill() {
  const { t } = useTranslation()
  const { connectionStatus } = useNetPulse()
  if (connectionStatus === 'reconnecting') {
    return (
      <span className="flex items-center gap-1.5 rounded-full bg-warn/10 px-2.5 py-1">
        <span className="relative flex h-1.5 w-1.5">
          <span className="absolute inline-flex h-full w-full rounded-full bg-warn opacity-75 animate-ping-soft" />
          <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-warn" />
        </span>
        <span className="font-mono text-[10px] font-semibold uppercase tracking-wider text-warn">{t('topbar.reconnecting')}</span>
      </span>
    )
  }
  return (
    <span className="flex items-center gap-1.5 rounded-full bg-ok/10 px-2.5 py-1">
      <span className="relative flex h-1.5 w-1.5">
        <span className="absolute inline-flex h-full w-full rounded-full bg-ok opacity-75 animate-ping-soft" />
        <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-ok" />
      </span>
      <span className="font-mono text-[10px] font-semibold uppercase tracking-wider text-ok">{t('topbar.live')}</span>
    </span>
  )
}

function Topbar() {
  const { t } = useTranslation()
  const { refresh } = useDashboard()
  const { refresh: refreshData } = useNetPulse()
  const [spinning, setSpinning] = useState(false)
  const location = useLocation()
  // Timer del spin del botón de refresh (#227): limpiado en unmount.
  const spinTimer = useRef<number | null>(null)
  useEffect(() => () => {
    if (spinTimer.current !== null) window.clearTimeout(spinTimer.current)
  }, [])

  const onRefresh = () => {
    setSpinning(true)
    refresh()
    refreshData()
    if (spinTimer.current !== null) window.clearTimeout(spinTimer.current)
    spinTimer.current = window.setTimeout(() => {
      spinTimer.current = null
      setSpinning(false)
    }, 450)
  }

  return (
    <header className="sticky top-0 z-30 hidden h-14 items-center gap-4 border-b border-border bg-canvas/80 px-6 backdrop-blur-md md:flex pt-safe">
      <h1 className="font-display text-h1 text-text-primary">{t(pageTitleKey(location.pathname))}</h1>
      <div className="ml-auto flex items-center gap-3">
        <button
          type="button"
          onClick={onRefresh}
          aria-label={t('topbar.refresh')}
          className="flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-elevated text-text-secondary transition-colors duration-150 hover:border-accent/40 hover:text-accent"
        >
          <RefreshCw
            className={cn('h-4 w-4 transition-transform duration-500', spinning && 'rotate-[360deg]')}
            strokeWidth={1.75}
          />
        </button>
        <LivePill />
        <BellButton />
      </div>
    </header>
  )
}

// ---------------------------------------------------------------------------
// Header móvil (<768px)
// ---------------------------------------------------------------------------

function MobileHeader() {
  const { t } = useTranslation()
  const { health: healthScore } = useNetPulse()
  const location = useLocation()
  const reduceMotion = () =>
    typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
  const scrollTopIfActive = (to: string) => () => {
    if (location.pathname === to && window.scrollY > 0) {
      window.scrollTo({ top: 0, behavior: reduceMotion() ? 'auto' : 'smooth' })
    }
  }
  return (
    <header className="[view-transition-name:netpulse-header] sticky top-0 z-30 flex h-14 items-center justify-between border-b border-border bg-canvas/80 px-4 backdrop-blur-md md:hidden pt-safe">
      <Logo onClick={scrollTopIfActive('/')} />
      <div className="flex items-center gap-3">
        <Link to="/" aria-label={t('topbar.networkHealth100', { score: healthScore.score })}>
          <motion.div layoutId="health-ring">
            <HealthRing
              value={healthScore.score}
              size={32}
              stroke={3.5}
              animateIn={false}
              ariaLabel={t('topbar.networkHealth', { score: healthScore.score })}
              center={<span className="font-mono text-[9px] font-semibold text-text-primary">{healthScore.score}</span>}
            />
          </motion.div>
        </Link>
        <BellButton />
      </div>
    </header>
  )
}

// ---------------------------------------------------------------------------
// Bottom tab bar móvil (<768px) — 5 tabs + sheet "Más"
// ---------------------------------------------------------------------------

const TAB_ITEMS = NAV_ITEMS.filter((i) => ['/', '/routers', '/devices', '/alerts'].includes(i.to))

/** Orden completo de vistas (para la dirección del deslizamiento móvil). */
const NAV_ORDER: { to: string }[] = NAV_ITEMS

function navIndex(path: string): number {
  const active = (to: string) => (to === '/' ? path === '/' : path.startsWith(to))
  return NAV_ORDER.findIndex(({ to }) => active(to))
}

function MoreSheet() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const location = useLocation()
  const moreActive = ['/topology', '/settings'].some((p) => location.pathname.startsWith(p))
  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <button
          type="button"
          className={cn(
            'flex h-full min-w-[56px] flex-1 flex-col items-center justify-center gap-1 rounded-xl transition-colors',
            moreActive ? 'text-accent' : 'text-text-muted',
          )}
          aria-label={t('nav.moreOptions')}
        >
          <span className={cn('flex h-8 items-center rounded-full px-3', moreActive && 'bg-accent-soft')}>
            <MoreHorizontal className="h-5 w-5" strokeWidth={1.75} />
          </span>
          <span className="text-[10px] font-medium">{t('nav.more')}</span>
        </button>
      </SheetTrigger>
      <SheetContent side="bottom" className="rounded-t-2xl border-border bg-elevated pb-safe">
        <SheetHeader>
          <SheetTitle className="font-display text-text-primary">{t('nav.more')}</SheetTitle>
        </SheetHeader>
        <div className="mt-2 space-y-1">
          {[
            { to: '/topology', label: t('nav.topology'), icon: Waypoints, desc: t('nav.topologyDesc') },
            { to: '/settings', label: t('nav.settings'), icon: Settings, desc: t('nav.settingsDesc') },
          ].map((item) => (
            <Link
              key={item.to}
              to={item.to}
              onClick={() => setOpen(false)}
              className="flex items-center gap-3 rounded-xl px-3 py-3 transition-colors hover:bg-hover"
            >
              <span className="flex h-10 w-10 items-center justify-center rounded-xl bg-surface text-accent">
                <item.icon className="h-5 w-5" strokeWidth={1.75} />
              </span>
              <span>
                <span className="block text-sm font-medium text-text-primary">{item.label}</span>
                <span className="block text-caption text-text-muted">{item.desc}</span>
              </span>
            </Link>
          ))}
          <div className="flex items-center gap-3 rounded-xl px-3 py-3 opacity-70">
            <span className="flex h-10 w-10 items-center justify-center rounded-xl bg-surface text-text-secondary">
              <Download className="h-5 w-5" strokeWidth={1.75} />
            </span>
            <span>
              <span className="block text-sm font-medium text-text-primary">{t('nav.installApp')}</span>
              <span className="block text-caption text-text-muted">{t('nav.installFromSettings')}</span>
            </span>
          </div>
          <div className="flex items-center justify-between rounded-xl px-3 py-3">
            <span className="text-sm font-medium text-text-primary">{t('nav.theme')}</span>
            <ThemeToggle />
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}

function TabBar() {
  const { t } = useTranslation()
  const location = useLocation()
  const navigate = useNavigate()
  const reduceMotion = () =>
    typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
  const scrollTopIfActive = (to: string) => () => {
    if (location.pathname === to && window.scrollY > 0) {
      window.scrollTo({ top: 0, behavior: reduceMotion() ? 'auto' : 'smooth' })
    }
  }
  /* Navegación móvil con deslizamiento (#187): el modo declarativo
   * (BrowserRouter) no soporta la prop viewTransition de react-router (solo
   * RouterProvider), así que interceptamos el click y envolvemos la navegación
   * en document.startViewTransition (con flushSync, igual que hace react-router
   * internamente). La dirección se marca en <html data-nav-dir> antes del
   * snapshot; el shell (header + bottom nav) queda estático vía CSS. */
  const handleMobileNav = (to: string) => (event: MouseEvent<HTMLAnchorElement>) => {
    event.preventDefault()
    const from = navIndex(location.pathname)
    const target = navIndex(to)
    if (target !== -1 && from !== target) {
      try {
        document.documentElement.dataset.navDir = from === -1 || target > from ? 'forward' : 'back'
      } catch {
        /* sin dataset */
      }
      scrollTopIfActive(to)()
      const doNavigate = () => navigate(to)
      if (typeof document.startViewTransition === 'function') {
        document.startViewTransition(() => flushSync(doNavigate))
      } else {
        doNavigate()
      }
    } else {
      scrollTopIfActive(to)()
      navigate(to, { replace: true })
    }
  }
  return (
    <nav
      className="[view-transition-name:netpulse-nav] fixed inset-x-0 bottom-0 z-40 border-t border-border bg-elevated/90 backdrop-blur-md md:hidden"
      aria-label={t('nav.mainNav')}
    >
      <div className="flex h-16 items-stretch px-2 pb-[env(safe-area-inset-bottom)]">
        {TAB_ITEMS.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.end}
            onClick={handleMobileNav(item.to)}
            className={({ isActive }) =>
              cn(
                'flex h-full min-w-[56px] flex-1 flex-col items-center justify-center gap-1 rounded-xl transition-colors',
                isActive ? 'text-accent' : 'text-text-muted',
              )
            }
          >
            {({ isActive }) => (
              <>
                <span className={cn('relative flex h-8 items-center rounded-full px-3', isActive && 'bg-accent-soft')}>
                  <item.icon className="h-5 w-5" strokeWidth={1.75} />
                  <NavItemBadge to={item.to} variant="tab" />
                </span>
                <span className="text-[10px] font-medium">{t(item.labelKey)}</span>
              </>
            )}
          </NavLink>
        ))}
        <MoreSheet />
      </div>
    </nav>
  )
}

// ---------------------------------------------------------------------------
// Layout
// ---------------------------------------------------------------------------

/** Barra de modo demo (patrón zfsctl): visible en todas las vistas con sesión demo. */
function DemoBanner() {
  const { t } = useTranslation()
  return (
    <div
      role="status"
      className="mb-4 flex items-center gap-2.5 rounded-xl border border-warn/35 bg-warn/10 px-3.5 py-2.5 text-[13px] font-semibold text-warn"
    >
      <span className="h-2 w-2 shrink-0 animate-ping-soft rounded-full bg-warn" />
      <span>{t('demo.banner')}</span>
      <button
        type="button"
        onClick={exitDemo}
        className="ml-auto flex h-8 items-center rounded-lg border border-warn/40 px-3 text-caption font-medium text-warn transition-colors hover:bg-warn/15"
      >
        {t('demo.exit')}
      </button>
    </div>
  )
}

function Shell() {
  const location = useLocation()
  const key = useMemo(() => location.pathname, [location.pathname])
  const { isDemo } = useNetPulse()
  const isMobile = useIsMobile()
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem('netpulse-sidebar-collapsed') === '1')

  // Cada cambio de ruta resetea el scroll al principio (no hay ScrollRestoration).
  useEffect(() => {
    window.scrollTo(0, 0)
  }, [location.pathname])

  const toggleCollapse = () => {
    setCollapsed((prev) => {
      localStorage.setItem('netpulse-sidebar-collapsed', prev ? '0' : '1')
      return !prev
    })
  }

  return (
    <div className="min-h-[100dvh] bg-canvas">
      <Sidebar collapsed={collapsed} onToggleCollapse={toggleCollapse} />
      <Rail />
      <div className={collapsed ? 'md:pl-16 lg:pl-16' : 'md:pl-16 lg:pl-[232px]'}>
        <Topbar />
        <MobileHeader />
        <main className="[view-transition-name:netpulse-content] mx-auto w-full max-w-[1400px] px-4 pb-24 pt-4 md:px-6 md:pb-10 md:pt-6">
          {isDemo && <DemoBanner />}
          <UpdateBanner />
          {/* Confirmación post-update (issue #161): toast al cargar si el
              último apply llegó a arrancar con el commit nuevo. */}
          <UpdateConfirmToast />
          {/* Pull-to-refresh (issue #139): gesto móvil recarga la vista para
              coger un deploy nuevo sin reabrir la app. Solo actúa en touch y
              con scroll arriba; no interfiere con carruseles/tablas. */}
          <PullToRefresh>
            {/* En móvil usamos view transitions para el deslizamiento entre
                vistas (#187): el fade/y de framer-motion se desactiva porque su
                estado inicial (opacity 0) quedaría congelado en el snapshot. */}
            {isMobile ? (
              <div>
                <Outlet />
              </div>
            ) : (
              <AnimatePresence mode="wait">
                <motion.div
                  key={key}
                  initial={{ opacity: 0, y: 8 }}
                  animate={{ opacity: 1, y: 0 }}
                  exit={{ opacity: 0, y: -4 }}
                  transition={{ duration: 0.2, ease: 'easeOut' }}
                >
                  <Outlet />
                </motion.div>
              </AnimatePresence>
            )}
          </PullToRefresh>
        </main>
      </div>
      <TabBar />
      <CommandPalette />
      <InstallPrompt />
    </div>
  )
}

/** App shell del dashboard (design.md §9). Sustituye a Navbar/Footer. */
export default function Layout() {
  return (
    <DashboardProvider>
      <Shell />
    </DashboardProvider>
  )
}
