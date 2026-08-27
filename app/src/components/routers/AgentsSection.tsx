import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { Check, Clipboard, Clock, Loader2, RotateCcw } from 'lucide-react'
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

  // Timeline visible mientras el upgrade está en marcha o hasta 90 s después
  // del último paso (así se ve el recorrido completo aunque vaya rápido).
  const up = agent?.upgrade
  const showTimeline =
    up !== undefined && (live !== undefined || (up.step !== 'queued' && nowSec - up.ts < 90))

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
        <span className="font-mono text-caption text-text-secondary">{agent?.version ? `v${agent.version}` : '-'}</span>
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
