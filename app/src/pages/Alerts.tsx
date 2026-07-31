import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useSearchParams } from 'react-router-dom'
import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import {
  AlertTriangle,
  ArrowRight,
  CheckCheck,
  CheckCircle2,
  ChevronRight,
  Info,
  Laptop,
  MoreVertical,
  OctagonX,
  Smartphone,
  Tablet,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { relTime } from '@/i18n'
import type { AlertSeverity } from '@/data/mock'
import { CountUp } from '@/components/CountUp'
import { EmptyState } from '@/components/EmptyState'
import { SegmentedControl } from '@/components/SegmentedControl'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { buildAlertFeed, buildLiveFeed, DAY_ORDER, KIND_LABELS } from '@/data/alertFeed'
import type { AlertKind, FeedDay, FeedEvent, FeedSpark } from '@/data/alertFeed'
import { useNetPulse } from '@/data/DataProvider'
import { cn } from '@/lib/utils'

// ---------------------------------------------------------------------------
// Severidad (misma paleta que AlertItem, design.md §6)
// ---------------------------------------------------------------------------

const SEVERITY: Record<AlertSeverity, { icon: LucideIcon; tile: string; dot: string }> = {
  warn: { icon: AlertTriangle, tile: 'bg-warn/10 text-warn', dot: 'bg-warn' },
  critical: { icon: OctagonX, tile: 'bg-danger/10 text-danger', dot: 'bg-danger' },
  info: { icon: Info, tile: 'bg-info/10 text-info', dot: 'bg-info' },
  ok: { icon: CheckCircle2, tile: 'bg-ok/10 text-ok', dot: 'bg-ok' },
}

const PEER_ICONS = { movil: Smartphone, portatil: Laptop, tablet: Tablet } as const

// ---------------------------------------------------------------------------
// Filtros
// ---------------------------------------------------------------------------

type SevFilter = 'todas' | 'avisos' | 'info' | 'resueltas'
type KindFilter = 'todos' | AlertKind

const SEV_OPTIONS = [
  { value: 'todas', labelKey: 'alerts.sevAll' },
  { value: 'avisos', labelKey: 'alerts.sevWarnings' },
  { value: 'info', labelKey: 'alerts.sevInfo' },
  { value: 'resueltas', labelKey: 'alerts.sevResolved' },
] as const

const SEV_MATCH: Record<Exclude<SevFilter, 'todas'>, AlertSeverity> = {
  avisos: 'warn',
  info: 'info',
  resueltas: 'ok',
}

// ---------------------------------------------------------------------------
// Sparkline con línea de umbral (contexto expandible, alerts.md §④)
// ---------------------------------------------------------------------------

function ThresholdSpark({ spark, animateIn }: { spark: FeedSpark; animateIn: boolean }) {
  const W = 260
  const H = 64
  const pad = 6
  const values = spark.threshold !== undefined ? [...spark.data, spark.threshold] : spark.data
  const min = Math.min(...values)
  const max = Math.max(...values)
  const span = max - min || 1
  const x = (i: number) => pad + (i * (W - pad * 2)) / (spark.data.length - 1)
  const y = (v: number) => pad + (1 - (v - min) / span) * (H - pad * 2 - 10)
  const pts = spark.data.map((v, i) => `${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(' ')

  return (
    <div>
      <div className="mb-2 flex items-baseline justify-between gap-3">
        <span className="text-caption font-semibold uppercase tracking-[0.06em] text-text-muted">{spark.label}</span>
        <span className="font-mono text-mono-sm text-text-primary">{spark.current}</span>
      </div>
      <svg viewBox={`0 0 ${W} ${H}`} className="h-16 w-full" role="img" aria-label={`${spark.label}: ${spark.current}`}>
        {spark.threshold !== undefined && (
          <>
            <line
              x1={pad}
              x2={W - pad}
              y1={y(spark.threshold)}
              y2={y(spark.threshold)}
              stroke="rgb(var(--warn))"
              strokeWidth="1"
              strokeDasharray="4 3"
              opacity="0.7"
            />
            <text
              x={W - pad}
              y={y(spark.threshold) - 3}
              textAnchor="end"
              className="fill-warn"
              fontSize="8"
              fontFamily="JetBrains Mono, monospace"
            >
              {spark.thresholdLabel}
            </text>
          </>
        )}
        <motion.polyline
          points={pts}
          fill="none"
          stroke={spark.color}
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          initial={animateIn ? { pathLength: 0 } : false}
          animate={{ pathLength: 1 }}
          transition={{ duration: 0.5, ease: 'easeOut' }}
        />
      </svg>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Panel de contexto expandible
// ---------------------------------------------------------------------------

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg bg-surface px-3 py-2">
      <span className="text-caption text-text-muted">{label}</span>
      <span className="font-mono text-mono-sm text-text-primary">{value}</span>
    </div>
  )
}

function ContextPanel({ ev, animateIn }: { ev: FeedEvent; animateIn: boolean }) {
  const { t } = useTranslation()
  const c = ev.context
  const PeerIcon = c?.wg ? PEER_ICONS[c.wg.peerType] : null
  return (
    <div className="space-y-3 rounded-xl border border-border bg-elevated/60 p-4">
      {c?.spark && <ThresholdSpark spark={c.spark} animateIn={animateIn} />}

      {c?.wg && PeerIcon && (
        <div className="space-y-2">
          <div className="flex items-center gap-2.5">
            <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-tunnel/10 text-tunnel">
              <PeerIcon className="h-4 w-4" strokeWidth={1.75} />
            </span>
            <div>
              <div className="text-sm font-medium text-text-primary">{c.wg.peer}</div>
              <div className="text-caption text-text-muted">{t('alerts.wgPeerCaption')}</div>
            </div>
          </div>
          <div className="grid gap-2 sm:grid-cols-3">
            <Fact label="Endpoint" value={c.wg.endpoint} />
            <Fact label="IP túnel" value={c.wg.tunnelIp} />
            <Fact label="Handshake" value={relTime(c.wg.handshake)} />
          </div>
        </div>
      )}

      {c?.device && (
        <div className="grid gap-2 sm:grid-cols-2">
          <Fact label="IP" value={c.device.ip} />
          <Fact label="MAC" value={c.device.mac} />
          <Fact label={t('devices.colBand')} value={c.device.band} />
          <Fact label={t('devices.colSignal')} value={c.device.signal} />
        </div>
      )}

      {c?.facts && (
        <div className="grid gap-2 sm:grid-cols-2">
          {c.facts.map((f) => (
            <Fact key={f.label} label={f.label} value={f.value} />
          ))}
        </div>
      )}

      {c?.note && <p className="text-caption leading-relaxed text-text-muted">{c.note}</p>}

      {ev.routerId && (
        <Link
          to={`/routers/${ev.routerId}`}
          className="group inline-flex items-center gap-1 text-caption font-semibold text-accent transition-colors hover:text-accent/80"
        >
          {t('alerts.viewRouter')}
          <ArrowRight className="h-3.5 w-3.5 transition-transform duration-150 group-hover:translate-x-0.5" strokeWidth={1.75} />
        </Link>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Fila del feed (timeline, alerts.md §④)
// ---------------------------------------------------------------------------

interface FeedRowProps {
  ev: FeedEvent
  index: number
  read: boolean
  expanded: boolean
  onToggle: () => void
  reduce: boolean
}

function FeedRow({ ev, index, read, expanded, onToggle, reduce }: FeedRowProps) {
  const { t } = useTranslation()
  const sev = SEVERITY[ev.severity]
  const Icon = ev.icon ?? sev.icon
  return (
    <motion.li
      initial={reduce ? false : { opacity: 0, y: 14 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, ease: 'easeOut', delay: reduce ? 0 : index * 0.06 }}
      className="relative pl-12"
    >
      {/* Tile de severidad sobre la línea del timeline */}
      <span
        className={cn(
          'absolute left-0 top-2.5 z-10 flex h-10 w-10 items-center justify-center rounded-lg ring-4 ring-canvas',
          sev.tile,
        )}
      >
        <Icon className="h-5 w-5" strokeWidth={1.75} />
      </span>

      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        className={cn(
          'group flex w-full items-center gap-3 rounded-xl px-3 py-3 text-left transition-colors duration-150 hover:bg-hover',
          !read && 'bg-elevated',
        )}
      >
        <div className="min-w-0 flex-1">
          <div className="flex items-center justify-between gap-2">
            <span className="truncate text-sm font-medium text-text-primary">{ev.title}</span>
            <span className="flex shrink-0 items-center gap-2">
              <AnimatePresence initial={false}>
                {!read && (
                  <motion.span
                    key="dot"
                    exit={{ scale: 0, opacity: 0 }}
                    transition={{ duration: 0.2, delay: reduce ? 0 : index * 0.04 }}
                    className={cn('h-1.5 w-1.5 rounded-full', sev.dot)}
                    aria-label={t('alerts.unread')}
                  />
                )}
              </AnimatePresence>
              <span className="font-mono text-caption text-text-muted">{relTime(ev.time)}</span>
            </span>
          </div>
          <p className="mt-0.5 truncate text-caption text-text-secondary">{ev.description}</p>
        </div>
        <ChevronRight
          className={cn('h-4 w-4 shrink-0 text-text-muted transition-transform duration-200', expanded && 'rotate-90')}
          strokeWidth={1.75}
        />
      </button>

      <AnimatePresence initial={false}>
        {expanded && (
          <motion.div
            key="ctx"
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ type: 'spring', stiffness: 320, damping: 32 }}
            className="overflow-hidden"
          >
            <div className="mx-3 mb-2 mt-1">
              <ContextPanel ev={ev} animateIn={!reduce} />
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </motion.li>
  )
}

// ---------------------------------------------------------------------------
// Página Alertas `/alerts` (alerts.md)
// ---------------------------------------------------------------------------

export default function Alerts() {
  const { t } = useTranslation()
  const reduce = useReducedMotion() ?? false
  const { alerts, wireguard, routers, isDemo } = useNetPulse()
  // Feed: demo = diseño enriquecido del mockup; live = SOLO alertas reales
  const alertFeed = useMemo(
    () =>
      isDemo
        ? buildAlertFeed(alerts, wireguard, (id) => routers.find((r) => r.id === id)?.modelShort)
        : buildLiveFeed(alerts),
    [alerts, wireguard, routers, isDemo],
  )
  const [readIds, setReadIds] = useState<ReadonlySet<string>>(new Set())
  const [sev, setSev] = useState<SevFilter>('todas')
  const [kind, setKind] = useState<KindFilter>('todos')
  const [searchParams] = useSearchParams()
  const [onlyUnread, setOnlyUnread] = useState(() => searchParams.get('unread') === '1')
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [burst, setBurst] = useState(0)
  const [loadState, setLoadState] = useState<'idle' | 'loading' | 'done'>('idle')
  const sentinelRef = useRef<HTMLDivElement>(null)

  const isRead = (ev: FeedEvent) => ev.read || readIds.has(ev.id)
  const unread = alertFeed.filter((ev) => !isRead(ev)).length

  const markAllRead = () => {
    setReadIds(new Set(alertFeed.map((ev) => ev.id)))
    setBurst((b) => b + 1)
  }

  const toggleEvent = (ev: FeedEvent) => {
    setReadIds((prev) => (prev.has(ev.id) ? prev : new Set(prev).add(ev.id)))
    setExpandedId((cur) => (cur === ev.id ? null : ev.id))
  }

  const filtered = useMemo(
    () =>
      alertFeed.filter((ev) => {
        if (sev !== 'todas' && ev.severity !== SEV_MATCH[sev]) return false
        if (kind !== 'todos' && ev.kind !== kind) return false
        if (onlyUnread && isRead(ev)) return false
        return true
      }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [alertFeed, sev, kind, onlyUnread, readIds],
  )

  const groups = useMemo(
    () =>
      DAY_ORDER.map((day) => ({ day, items: filtered.filter((ev) => ev.day === day) })).filter(
        (g) => g.items.length > 0,
      ),
    [filtered],
  )

  // Infinite scroll simulado: skeleton → fin del historial (alerts.md §Scroll)
  useEffect(() => {
    const el = sentinelRef.current
    if (!el || loadState !== 'idle') return
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          setLoadState('loading')
          window.setTimeout(() => setLoadState('done'), 1200)
        }
      },
      { rootMargin: '120px' },
    )
    io.observe(el)
    return () => io.disconnect()
  }, [loadState, filtered.length])

  const filterKey = `${sev}|${kind}|${onlyUnread}`

  return (
    <div className="mx-auto w-full max-w-[1100px]">
      {/* ① Page header */}
      <nav aria-label={t('common.breadcrumb')} className="mb-1 text-caption text-text-muted">
        <Link to="/" className="transition-colors hover:text-accent">{t('common.home')}</Link>
        <span className="mx-1.5">/</span>
        <span className="text-text-secondary">{t('nav.alerts')}</span>
      </nav>
      <div className="flex items-start justify-between gap-3">
        <div>
          <motion.h1
            initial={reduce ? false : { opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.3, ease: 'easeOut' }}
            className="font-display text-h1 text-text-primary"
          >
            {t('alerts.title')}
          </motion.h1>
          <motion.p
            initial={reduce ? false : { opacity: 0 }}
            animate={{ opacity: 1 }}
            transition={{ duration: 0.3, delay: reduce ? 0 : 0.15 }}
            className="mt-0.5 text-caption text-text-muted"
          >
            {unread > 0 ? (
              <>
                <CountUp value={unread} className="font-mono" /> {t('alerts.unreadCount', { count: unread })}
              </>
            ) : (
              t('alerts.allRead')
            )}
          </motion.p>
        </div>

        {/* Desktop: botón ghost · Móvil: menú MoreVertical */}
        <button
          type="button"
          onClick={markAllRead}
          disabled={unread === 0}
          className="hidden h-9 shrink-0 items-center gap-2 rounded-lg border border-border bg-elevated px-3 text-xs font-medium text-text-secondary transition-colors duration-150 hover:border-accent/40 hover:text-accent disabled:cursor-not-allowed disabled:opacity-40 md:inline-flex"
        >
          <motion.span key={burst} animate={reduce ? undefined : { scale: [1, 1.2, 1] }} transition={{ duration: 0.35 }}>
            <CheckCheck className="h-4 w-4" strokeWidth={1.75} />
          </motion.span>
          {t('alerts.markAllRead')}
        </button>
        <div className="md:hidden">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                type="button"
                aria-label={t('alerts.moreActions')}
                className="flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-elevated text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
              >
                <MoreVertical className="h-4 w-4" strokeWidth={1.75} />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="border-border bg-elevated">
              <DropdownMenuItem
                disabled={unread === 0}
                onSelect={markAllRead}
                className="gap-2 text-text-primary focus:bg-hover"
              >
                <CheckCheck className="h-4 w-4" strokeWidth={1.75} />
                {t('alerts.markAllRead')}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {/* ② Resumen — 3 chips-stats */}
      <div className="mt-5 grid grid-cols-3 gap-3 md:gap-4">
        {[
          { icon: AlertTriangle, tone: 'bg-warn/10 text-warn', value: 2, label: t('alerts.chipWarnings'), caption: t('alerts.chipWarningsCaption'), wiggle: false },
          { icon: Info, tone: 'bg-info/10 text-info', value: 14, label: t('alerts.chipToday'), caption: null, wiggle: false },
          { icon: CheckCircle2, tone: 'bg-ok/10 text-ok', value: 0, label: t('alerts.chipCritical'), caption: t('alerts.chipCriticalCaption'), wiggle: true },
        ].map((chip, i) => (
          <motion.div
            key={chip.label}
            initial={reduce ? false : { opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.3, ease: 'easeOut', delay: reduce ? 0 : 0.1 + i * 0.08 }}
            className="rounded-2xl border border-border bg-surface p-3 md:p-4"
          >
            <motion.div
              animate={chip.wiggle && !reduce ? { rotate: [0, 2, -2, 0] } : undefined}
              transition={{ duration: 0.4, delay: reduce ? 0 : 0.5 }}
              className="flex items-center gap-2.5 md:gap-3"
            >
              <span className={cn('flex h-9 w-9 shrink-0 items-center justify-center rounded-lg', chip.tone)}>
                <chip.icon className="h-[18px] w-[18px]" strokeWidth={1.75} />
              </span>
              <div className="min-w-0">
                <div className="font-display text-h2 leading-none text-text-primary md:text-stat">
                  <CountUp value={chip.value} />
                </div>
                <div className="mt-1 truncate text-caption font-semibold uppercase tracking-[0.06em] text-text-secondary">
                  {chip.label}
                </div>
              </div>
            </motion.div>
            {chip.caption && (
              <p className="mt-2 hidden text-caption leading-snug text-text-muted sm:block">{chip.caption}</p>
            )}
          </motion.div>
        ))}
      </div>

      {/* ③ Filtros */}
      <div className="mt-5 flex flex-wrap items-center gap-2.5 md:gap-3">
        <SegmentedControl options={SEV_OPTIONS.map((o) => ({ value: o.value, label: t(o.labelKey) }))} value={sev} onChange={(v) => setSev(v)} ariaLabel={t('alerts.filterBySeverity')} />
        <Select value={kind} onValueChange={(v) => setKind(v as KindFilter)}>
          <SelectTrigger
            size="sm"
            aria-label={t('alerts.filterByKind')}
            className="h-8 rounded-lg border-border bg-elevated text-xs font-medium text-text-secondary shadow-none"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent className="border-border bg-elevated">
            <SelectItem value="todos" className="text-text-primary focus:bg-hover">{t('alerts.allKinds')}</SelectItem>
            {(Object.keys(KIND_LABELS) as AlertKind[]).map((k) => (
              <SelectItem key={k} value={k} className="text-text-primary focus:bg-hover">
                {t(`alerts.kinds.${k}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <button
          type="button"
          role="switch"
          aria-checked={onlyUnread}
          onClick={() => setOnlyUnread((v) => !v)}
          className={cn(
            'inline-flex h-8 items-center gap-2 rounded-full border px-3 text-xs font-medium transition-colors duration-150',
            onlyUnread
              ? 'border-accent/40 bg-accent-soft text-accent'
              : 'border-border bg-elevated text-text-secondary hover:text-text-primary',
          )}
        >
          <span className={cn('h-1.5 w-1.5 rounded-full', onlyUnread ? 'bg-accent' : 'bg-text-muted')} />
          {t('alerts.onlyUnread')}
        </button>
        <span className="ml-auto font-mono text-caption text-text-muted">
          {t('alerts.eventCount', { count: filtered.length })}
        </span>
      </div>

      {/* ④ Feed agrupado por día */}
      {groups.length === 0 ? (
        <div className="mt-5 rounded-2xl border border-border bg-surface">
          <EmptyState
            image="/empty-alerts.svg"
            title={t('alerts.emptyTitle')}
            description={t('alerts.emptyDesc')}
          />
        </div>
      ) : (
        <motion.div
          key={filterKey}
          initial={reduce ? false : { opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.25 }}
          className="mt-2"
        >
          {groups.map((group) => (
            <section key={group.day} aria-label={t(`alerts.days.${group.day as FeedDay}`)}>
              <div className="sticky top-14 z-20 flex items-baseline gap-2 bg-canvas/90 py-2.5 backdrop-blur-md">
                <h2 className="font-display text-h2 text-text-primary">{t(`alerts.days.${group.day as FeedDay}`)}</h2>
                <span className="font-mono text-caption text-text-muted">
                  {t('alerts.eventCount', { count: group.items.length })}
                </span>
              </div>
              <ul className="relative pb-2">
                {/* Línea del timeline (se dibuja al montar) */}
                <motion.span
                  aria-hidden="true"
                  initial={reduce ? false : { scaleY: 0 }}
                  animate={{ scaleY: 1 }}
                  transition={{ duration: 0.8, ease: 'easeOut' }}
                  className="absolute bottom-2 left-5 top-2 w-px origin-top bg-border"
                />
                {group.items.map((ev, i) => (
                  <FeedRow
                    key={ev.id}
                    ev={ev}
                    index={i}
                    read={isRead(ev)}
                    expanded={expandedId === ev.id}
                    onToggle={() => toggleEvent(ev)}
                    reduce={reduce}
                  />
                ))}
              </ul>
            </section>
          ))}
        </motion.div>
      )}

      {/* Infinite scroll simulado */}
      {groups.length > 0 && (
        <div ref={sentinelRef} className="py-4">
          {loadState === 'loading' && (
            <div className="space-y-2" aria-live="polite">
              <p className="mb-3 text-center text-caption text-text-muted">{t('alerts.loadingOld')}</p>
              {[0, 1, 2].map((i) => (
                <div key={i} className="flex animate-pulse items-center gap-3 rounded-xl px-3 py-3">
                  <div className="h-10 w-10 rounded-lg bg-elevated" />
                  <div className="flex-1 space-y-2">
                    <div className="h-3 w-2/5 rounded bg-elevated" />
                    <div className="h-2.5 w-3/5 rounded bg-elevated" />
                  </div>
                </div>
              ))}
            </div>
          )}
          {loadState === 'done' && (
            <p className="text-center font-mono text-caption text-text-muted">
              {t('alerts.noMoreEvents')}
            </p>
          )}
        </div>
      )}
    </div>
  )
}
