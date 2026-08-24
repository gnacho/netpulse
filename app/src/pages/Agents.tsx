import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { Check, Clipboard, RotateCcw } from 'lucide-react'
import { motion, useReducedMotion } from 'framer-motion'
import { useNetPulse } from '@/data/DataProvider'
import { useAuth } from '@/data/AuthContext'
import type { AgentInfo, Router } from '@/data/types'
import { relTimeFromTs } from '@/i18n'
import { AgentBadge } from '@/components/routers/AgentBadge'
import { AgentRearmButton } from '@/components/routers/AgentRearmButton'
import { cn } from '@/lib/utils'

/**
 * Página `/agents` — vista de agentes nativos (issue #245: recuperar un
 * agente caído desde la app). Lista cada agente registrado con su estado
 * (fresco / caído-stale / no instalado) y last-seen, y para admin expone las
 * acciones de recuperación:
 *   - Rearmar (canal rearm existente): preferente cuando el proceso vive pero
 *     no reporta (heartbeat stale) — NO reinstala en seco.
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

/** Fila de un agente con sus acciones de recuperación (estado local). */
function AgentRow({ agent, router }: { agent?: AgentInfo; router: Router | undefined }) {
  const { t } = useTranslation()
  const { reinstallAgent, createAgentInstall } = useNetPulse()
  const auth = useAuth()
  const [reinstallState, setReinstallState] = useState<ReinstallState>('idle')
  const [reinstallMsg, setReinstallMsg] = useState('')
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const isOpenWrt = isOpenWrtType(router?.type)
  const isStale = agent !== undefined && !agent.fresh
  const isMissing = agent === undefined && router?.agentOnly === true
  const canRecover = isOpenWrt && (isStale || isMissing) && auth?.role === 'admin'

  const slug = agent?.slug ?? router?.id ?? ''
  const lastSeen = agent?.lastSeen ? relTimeFromTs(agent.lastSeen) ?? t('routers.agents.never') : t('routers.agents.never')

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
        <span className="font-mono text-caption text-text-secondary">{agent?.version ? `v${agent.version}` : '—'}</span>
      </td>
      <td className="py-3">
        <div className="flex flex-wrap items-center gap-2">
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
  )
}

/** Vista de agentes (issue #245): registrados + routers agent-only sin agente. */
export default function Agents() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
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
    <div className="space-y-4 md:space-y-5">
      <header>
        <motion.nav
          initial={reduce ? false : { opacity: 0, y: 12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.25, ease: 'easeOut' }}
          aria-label={t('common.breadcrumb')}
          className="font-mono text-caption text-text-muted"
        >
          <Link to="/" className="transition-colors hover:text-accent">{t('common.home')}</Link>
          <span className="mx-1.5">/</span>
          <span className="text-text-secondary">{t('nav.agents')}</span>
        </motion.nav>
        <motion.div
          initial={reduce ? false : { opacity: 0, y: 12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.25, ease: 'easeOut', delay: 0.06 }}
          className="mt-1.5"
        >
          <h1 className="font-display text-h1 text-text-primary">{t('nav.agents')}</h1>
          <p className="text-caption text-text-muted">{t('routers.agents.subtitle', { total, down })}</p>
        </motion.div>
      </header>

      <section className="rounded-2xl border border-border bg-surface p-5 md:p-6">
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
    </div>
  )
}
