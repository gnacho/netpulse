import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { Check, Clipboard, Clock, Loader2, Radar, RotateCcw } from 'lucide-react'
import { motion } from 'framer-motion'
import { useNetPulse } from '@/data/DataProvider'
import { useAuth } from '@/data/AuthContext'
import type { AgentInfo, Router } from '@/data/types'
import { relTimeFromTs } from '@/i18n'
import { AgentBadge } from '@/components/routers/AgentBadge'
import { AgentRearmButton } from '@/components/routers/AgentRearmButton'
import { AgentUpgradeButton, activeUpgrade, upgradeStepText } from '@/components/routers/AgentUpgradeButton'
import { cn } from '@/lib/utils'

/**
 * Sección de agentes nativos (issue #245, reubicada en /routers por #284):
 * lista cada agente registrado con su estado (fresco / caído-stale / no
 * instalado) y last-seen, y para admin expone las acciones de recuperación:
 *   - Actualizar (POST /api/agents/{slug}/upgrade, #243): self-update del
 *     agente descargando el binario embebido, con progreso en vivo.
 *   - Rearmar (canal rearm existente): preferente cuando el proceso vive pero
 *     no reporta (heartbeat stale) - NO reinstala en seco.
 *   - Reinstalar (POST /api/agents/{slug}/reinstall, #246): despliega el
 *     agente vía SSH desde el server, con estados de progreso.
 *   - Copiar comando de instalación: one-liner manual como fallback para
 *     routers sin SSH.
 */

function isOpenWrtType(t: string | undefined): boolean {
  return t === undefined || t === '' || t === 'glinet' || t === 'openwrt'
}

async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    /* fallback abajo */
  }
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}

type ReinstallState = 'idle' | 'busy' | 'done' | 'fail'
type CopyState = 'idle' | 'busy' | 'done' | 'fail'

/** Hora HH:MM:SS local desde unix segundos (timeline de upgrade, #284). */
function hhmmss(ts: number): string {
  return new Date(ts * 1000).toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

/** Fila de un agente con sus acciones de recuperación (estado local). */
function AgentRow({ agent, router }: { agent?: AgentInfo; router: Router | undefined }) {
  const { t } = useTranslation()
  const { reinstallAgent, createAgentInstall } = useNetPulse()
  const auth = useAuth()
  const [reinstallState, setReinstallState] = useState<ReinstallState>('idle')
  const [reinstallMsg, setReinstallMsg] = useState('')
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const [nowSec, setNowSec] = useState(() => Math.floor(Date.now() / 1000))

  const isOpenWrt = isOpenWrtType(router?.type)
  const isStale = agent !== undefined && !agent.fresh
  const isMissing = agent === undefined && router?.agentOnly === true
  const canRecover = isOpenWrt && (isStale || isMissing) && auth?.role === 'admin'

  const slug = agent?.slug ?? router?.id ?? ''
  const lastSeen = agent?.lastSeen ? relTimeFromTs(agent.lastSeen) ?? t('routers.agents.never') : t('routers.agents.never')
  const live = agent ? activeUpgrade(agent, nowSec) : undefined

  // Tick de 1 s mientras hay upgrade en marcha (elapsed de la timeline).
  const ticking = live !== undefined
  useEffect(() => {
    if (!ticking) return
    const timer = window.setInterval(() => setNowSec(Math.floor(Date.now() / 1000)), 1000)
    return () => window.clearInterval(timer)
  }, [ticking])

  // Timeline visible mientras el upgrade está en marcha o hasta 5 min después
  // del último paso: el recorrido completo queda visible aunque vaya rápido
  // y sobrevive al parpadeo del poll de agentes (#284).
  const up = agent?.upgrade
  const showTimeline =
    up !== undefined && (live !== undefined || (up.step !== 'queued' && nowSec - up.ts < 300))

  const reinstall = async () => {
    if (reinstallState === 'busy') return
    if (!window.confirm(t('routers.agents.reinstallConfirm', { router: router?.name ?? slug }))) return
    setReinstallState('busy')
    setReinstallMsg('')
    const res = await reinstallAgent(slug)
    if (res && !res.error) {
      setReinstallState('done')
      setReinstallMsg(res.recovered ? t('routers.agents.reinstallDone') : t('routers.agents.reinstallPending'))
    } else {
      setReinstallState('fail')
      setReinstallMsg(res?.error ?? t('routers.agents.reinstallFail'))
    }
    window.setTimeout(() => setReinstallState('idle'), 8000)
  }

  const copyCmd = async () => {
    if (copyState === 'busy') return
    setCopyState('busy')
    const res = await createAgentInstall(slug)
    const ok = res ? await copyText(res.install) : false
    setCopyState(ok ? 'done' : 'fail')
    window.setTimeout(() => setCopyState('idle'), 4000)
  }

  return (
    <>
    <motion.tr
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25, ease: 'easeOut' }}
      className="border-b border-border/60 last:border-0"
    >
      <td className="py-3 pr-3">
        <div className="flex min-w-0 items-center gap-2.5">
          {router ? (
            <Link
              to={`/routers/${router.id}`}
              className="font-medium text-text-primary transition-colors hover:text-accent"
            >
              {router.name}
            </Link>
          ) : (
            <span className="font-medium text-text-primary">{slug}</span>
          )}
          <span className="font-mono text-caption text-text-muted">{slug}</span>
        </div>
      </td>
      <td className="py-3 pr-3">
        <AgentBadge agent={agent} agentOnly={router?.agentOnly} deviceType={router?.type} />
      </td>
      <td className="py-3 pr-3">
        <span className="font-mono text-caption text-text-secondary">{lastSeen}</span>
      </td>
      <td className="py-3 pr-3">
        <span className="flex items-center gap-2">
          <span className="font-mono text-caption text-text-secondary">
            {agent?.version ? (agent.kind === 'external' ? agent.version : `v${agent.version}`) : '-'}
          </span>
          {agent?.kind === 'external' && (
            <span
              title={t('routers.agents.externalTip', { interval: agent.interval ?? 0 })}
              className="inline-flex items-center rounded-full border border-border bg-surface px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-text-muted"
            >
              {t('routers.agents.external')}
            </span>
          )}
        </span>
      </td>
      <td className="py-3">
        <div className="flex flex-wrap items-center gap-2">
          {/* Actualizar: self-update del agente con progreso en vivo (#243) */}
          <AgentUpgradeButton agent={agent} className="h-8" />
          {/* Rearm: canal preferente para heartbeat stale (proceso vivo) */}
          {agent && <AgentRearmButton agent={agent} />}
          {canRecover && (
            <button
              type="button"
              onClick={() => void reinstall()}
              disabled={reinstallState === 'busy'}
              title={t('routers.agents.reinstallTip')}
              className={cn(
                'inline-flex h-8 items-center gap-1.5 rounded-lg border px-2.5 text-caption font-semibold transition-colors disabled:opacity-50',
                reinstallState === 'done'
                  ? 'border-ok/40 bg-ok/10 text-ok'
                  : reinstallState === 'fail'
                    ? 'border-danger/40 bg-danger/10 text-danger'
                    : 'border-border text-text-secondary hover:border-accent/40 hover:text-accent',
              )}
            >
              <RotateCcw className={cn('h-3.5 w-3.5', reinstallState === 'busy' && 'animate-spin')} strokeWidth={1.75} />
              {reinstallState === 'busy'
                ? t('routers.agents.reinstalling')
                : reinstallState === 'done'
                  ? t('routers.agents.reinstalled')
                  : reinstallState === 'fail'
                    ? t('routers.agents.reinstallRetry')
                    : t('routers.agents.reinstall')}
            </button>
          )}
          {canRecover && (
            <button
              type="button"
              onClick={() => void copyCmd()}
              disabled={copyState === 'busy'}
              title={t('routers.agents.copyCmdTip')}
              className={cn(
                'inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-2.5 text-caption font-semibold text-text-secondary transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50',
                copyState === 'done' && 'border-ok/40 bg-ok/10 text-ok',
                copyState === 'fail' && 'border-danger/40 bg-danger/10 text-danger',
              )}
            >
              {copyState === 'busy' ? (
                <Clipboard className="h-3.5 w-3.5 animate-pulse" strokeWidth={1.75} />
              ) : copyState === 'done' ? (
                <Check className="h-3.5 w-3.5" strokeWidth={1.75} />
              ) : (
                <Clipboard className="h-3.5 w-3.5" strokeWidth={1.75} />
              )}
              {copyState === 'done'
                ? t('routers.agents.copied')
                : copyState === 'fail'
                  ? t('routers.agents.cmdFail')
                  : t('routers.agents.copyCmd')}
            </button>
          )}
        </div>
        {reinstallMsg && reinstallState !== 'idle' && (
          <p className={cn('mt-1.5 text-caption', reinstallState === 'fail' ? 'text-danger' : 'text-text-muted')}>
            {reinstallMsg}
          </p>
        )}
      </td>
    </motion.tr>
    {showTimeline && up && (
      <motion.tr
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: 0.2 }}
        className="border-b border-border/60 last:border-0"
      >
        <td colSpan={5} className="pb-3">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 rounded-xl bg-surface px-3.5 py-2.5">
            <span className="inline-flex items-center gap-1.5 text-label font-medium uppercase text-text-muted">
              <Clock className="h-3.5 w-3.5" strokeWidth={1.75} aria-hidden="true" />
              {t('routers.agent.timeline')}
              {(() => {
                const h = up.steps ?? []
                const first = h[0]
                const last = h[h.length - 1]
                if (!first || !last || h.length < 2) return null
                const dur = Math.max(1, last.ts - first.ts)
                return <span className="font-mono normal-case">{t('routers.agent.timelineSummary', { count: h.length, secs: dur })}</span>
              })()}
            </span>
            {(up.steps ?? []).map((s, i) => {
              const isCurrent = i === (up.steps?.length ?? 0) - 1 && live !== undefined
              return (
                <span key={`${s.step}-${s.ts}`} className="inline-flex items-center gap-1.5 text-caption">
                  {isCurrent ? (
                    <Loader2 className="h-3 w-3 animate-spin text-accent" strokeWidth={2} aria-hidden="true" />
                  ) : (
                    <Check className="h-3 w-3 text-ok" strokeWidth={2} aria-hidden="true" />
                  )}
                  <span className={isCurrent ? 'font-medium text-text-primary' : 'text-text-secondary'}>
                    {upgradeStepText(s, t)}
                    {isCurrent && s.step !== 'queued' ? ` · ${Math.max(0, nowSec - s.ts)}s` : ''}
                  </span>
                  <span className="font-mono text-text-muted">{hhmmss(s.ts)}</span>
                </span>
              )
            })}
            {live === undefined && <span className="text-caption text-ok">{t('routers.agent.timelineDone')}</span>}
          </div>
        </td>
      </motion.tr>
    )}
    </>
  )
}

/** Switch embebido anunciándose por broadcast sin parar (#291). */
interface DiscoveredAgent {
  ip: string
  dev: string
  fw?: string
  ports?: number
  lastSeen: number
}

/** Franja de descubrimiento: switches RTLPlayground encontrados en la LAN
 * esperando pareado (slug + token + comando beacon). Solo admin (#291). */
function DiscoveredStrip() {
  const { t } = useTranslation()
  const auth = useAuth()
  const { createAgentInstall } = useNetPulse()
  const [found, setFound] = useState<DiscoveredAgent[]>([])
  const [slugByIp, setSlugByIp] = useState<Record<string, string>>({})
  const [pairByIp, setPairByIp] = useState<Record<string, { token?: string; error?: string; busy?: boolean }>>({})

  useEffect(() => {
    if (auth?.role !== 'admin') return
    let disposed = false
    const load = async () => {
      try {
        const res = await fetch('/api/agents/discovered', { signal: AbortSignal.timeout(4000) })
        if (!res.ok) return
        const j = (await res.json()) as { discovered?: DiscoveredAgent[] }
        if (!disposed) setFound(j.discovered ?? [])
      } catch {
        /* sin candidatos o sin sesión */
      }
    }
    void load()
    const id = window.setInterval(load, 30_000)
    return () => {
      disposed = true
      window.clearInterval(id)
    }
  }, [auth?.role])

  if (auth?.role !== 'admin' || found.length === 0) return null

  const pair = async (ip: string) => {
    const slug = (slugByIp[ip] ?? '').trim().toLowerCase().replace(/[^a-z0-9-]/g, '')
    if (!slug) return
    setPairByIp((m) => ({ ...m, [ip]: { busy: true } }))
    const res = await createAgentInstall(slug)
    setPairByIp((m) => ({
      ...m,
      [ip]: res && res.token ? { token: res.token } : { error: 'fail' },
    }))
  }

  return (
    <div className="mb-4 flex flex-col gap-2 rounded-2xl border border-accent/30 bg-accent/5 p-4" role="status">
      <p className="flex items-center gap-2 text-caption font-semibold text-accent">
        <Radar className="h-4 w-4 animate-pulse" strokeWidth={1.75} aria-hidden="true" />
        {t('routers.agents.discoveredTitle', { count: found.length })}
      </p>
      {found.map((c) => {
        const pairState = pairByIp[c.ip] ?? {}
        return (
          <div key={c.ip} className="flex flex-wrap items-center gap-2 rounded-xl bg-surface px-3.5 py-2.5">
            <span className="font-mono text-caption text-text-secondary">{c.ip}</span>
            <span className="text-caption font-medium text-text-primary">{c.dev}</span>
            {c.fw && <span className="rounded-full border border-border px-2 py-0.5 font-mono text-[10px] text-text-muted">{c.fw}</span>}
            {pairState.token ? (
              <code className="min-w-0 flex-1 truncate rounded-lg bg-canvas px-2 py-1 font-mono text-caption text-ok" title={`beacon <ip-netpulse> <slug> <token>`}>
                beacon &lt;ip-netpulse&gt; &lt;slug&gt; {pairState.token.slice(0, 12)}…
              </code>
            ) : (
              <>
                <input
                  value={slugByIp[c.ip] ?? ''}
                  onChange={(e) => setSlugByIp((m) => ({ ...m, [c.ip]: e.target.value }))}
                  placeholder={t('routers.agents.discoveredSlug')}
                  aria-label={t('routers.agents.discoveredSlug')}
                  className="h-8 w-36 rounded-lg border border-border bg-canvas px-2.5 font-mono text-caption text-text-primary outline-none focus:border-accent/50"
                />
                <button
                  type="button"
                  onClick={() => void pair(c.ip)}
                  disabled={pairState.busy}
                  className="inline-flex h-8 items-center rounded-lg bg-accent px-3 text-caption font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
                >
                  {pairState.busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" strokeWidth={2} aria-hidden="true" /> : t('routers.agents.discoveredPair')}
                </button>
                {pairState.error && <span className="text-caption text-danger">{t('routers.agents.discoveredFail')}</span>}
              </>
            )}
            <span className="ml-auto text-caption text-text-muted">{t('routers.agents.discoveredHint')}</span>
          </div>
        )
      })}
    </div>
  )
}

/** Sección de flota de agentes (issue #245): registrados + routers agent-only
 * sin agente. Vive al final de la página /routers (#284). */
export function AgentsSection() {
  const { t } = useTranslation()
  const { agents, routers } = useNetPulse()

  // Filas: agentes registrados (por slug) + routers agent-only sin agente.
  const rows = useMemo(() => {
    const agentBySlug = new Map(agents.map((a) => [a.slug, a]))
    const routerBySlug = new Map(routers.map((r) => [r.id, r]))
    const out: { agent?: AgentInfo; router?: Router }[] = agents.map((a) => ({
      agent: a,
      router: routerBySlug.get(a.slug),
    }))
    for (const r of routers) {
      if (r.agentOnly && !agentBySlug.has(r.id) && isOpenWrtType(r.type)) {
        out.push({ router: r })
      }
    }
    return out
  }, [agents, routers])

  const down = rows.filter(({ agent }) => (agent ? !agent.fresh : true)).length
  const total = rows.length

  return (
    <section className="rounded-2xl border border-border bg-surface p-5 md:p-6" aria-labelledby="agents-section-title">
      <div className="mb-4">
        <h2 id="agents-section-title" className="font-display text-h2 text-text-primary">{t('routers.agents.title')}</h2>
        <p className="text-caption text-text-muted">{t('routers.agents.subtitle', { total, down })}</p>
      </div>
      <DiscoveredStrip />
      {rows.length === 0 ? (
        <p className="py-8 text-center text-sm text-text-secondary">{t('routers.agents.empty')}</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="border-b border-border">
                <th className="pb-2.5 pr-3 text-label font-medium uppercase text-text-muted">{t('routers.agents.colDevice')}</th>
                <th className="pb-2.5 pr-3 text-label font-medium uppercase text-text-muted">{t('routers.agents.colStatus')}</th>
                <th className="pb-2.5 pr-3 text-label font-medium uppercase text-text-muted">{t('routers.agents.colLastSeen')}</th>
                <th className="pb-2.5 pr-3 text-label font-medium uppercase text-text-muted">{t('routers.agents.colVersion')}</th>
                <th className="pb-2.5 text-label font-medium uppercase text-text-muted">{t('routers.agents.colActions')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(({ agent, router }) => (
                <AgentRow key={agent?.slug ?? router?.id ?? ''} agent={agent} router={router} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
