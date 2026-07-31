import { useEffect, useRef, useState } from 'react'
import { Check, ExternalLink, Terminal } from 'lucide-react'
import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { fmtUptime } from '@/i18n'
import type { Router } from '@/data/mock'
import { SectionHeader } from '@/components/SectionHeader'
import { StatusPill } from '@/components/StatusPill'
import { getRouterExtras } from '@/components/routers/routerExtras'
import type { RouterExtras } from '@/components/routers/routerExtras'
import { EMPTY_EXTRAS, useNetPulse } from '@/data/DataProvider'

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3 py-2">
      <dt className="shrink-0 text-label uppercase text-text-muted">{label}</dt>
      <dd className="text-right font-mono text-mono-sm text-text-primary">{children}</dd>
    </div>
  )
}

/** ③ Info + Red (router-detail.md §③) — definition list + acciones ghost. */
export function RouterInfo({ router, extras }: { router: Router; extras?: RouterExtras }) {
  const { t } = useTranslation()
  const { isDemo } = useNetPulse()
  const ex = extras ?? (isDemo ? getRouterExtras(router.id) : EMPTY_EXTRAS)
  const reduce = useReducedMotion()
  const [toast, setToast] = useState(false)
  const timer = useRef<number | null>(null)

  useEffect(() => () => {
    if (timer.current) window.clearTimeout(timer.current)
  }, [])

  async function copySsh() {
    try {
      await navigator.clipboard.writeText(`ssh root@${router.ip}`)
    } catch {
      // Portapapeles no disponible (mock) — el toast igualmente confirma la acción
    }
    setToast(true)
    if (timer.current) window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setToast(false), 1800)
  }

  const rows: { label: string; node: React.ReactNode }[] = [
    { label: 'IP LAN', node: router.ip },
    { label: 'MAC', node: ex.mac },
    {
      label: 'Firmware',
      node: (
        <span className="inline-flex items-center gap-2">
          {ex.firmware}
          {ex.firmwareBase && <span className="text-text-muted">{t('routerDetail.info.firmwareBase', { base: ex.firmwareBase })}</span>}
          {ex.firmwareUpdated ? (
            <StatusPill tone="ok" label={t('routerDetail.info.updated')} />
          ) : (
            <span title={t('routers.firmwareAvailable', { version: ex.firmwareAvailable })}>
              <StatusPill tone="warn" label={ex.firmwareAvailable ?? ''} />
            </span>
          )}
        </span>
      ),
    },
    { label: 'Uptime', node: fmtUptime(router.uptime) },
    { label: t('routerDetail.info.lastReboot'), node: ex.lastReboot },
    { label: t('routerDetail.info.timezone'), node: 'Europe/Madrid' },
    {
      label: t('routerDetail.info.access'),
      node: (
        <span className="inline-flex items-center gap-1.5">
          <Terminal className="h-3.5 w-3.5 text-text-muted" strokeWidth={1.75} />
          SSH
          <span className="rounded-full bg-danger/10 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-danger">root</span>
        </span>
      ),
    },
    { label: 'SoC', node: ex.soc },
    { label: 'Flash', node: ex.flash },
  ]

  return (
    <section className="flex h-full flex-col rounded-2xl border border-border bg-surface p-5 md:p-6">
      <SectionHeader title={t('routerDetail.info.title')} />
      <dl className="mt-2 flex-1 divide-y divide-border/60">
        {rows.map((r, i) => (
          <motion.div
            key={r.label}
            initial={reduce ? false : { opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, ease: 'easeOut', delay: 0.1 + i * 0.04 }}
          >
            <Row label={r.label}>{r.node}</Row>
          </motion.div>
        ))}
      </dl>
      <div className="mt-3 flex flex-wrap gap-2 border-t border-border pt-3.5">
        <a
          href="#"
          onClick={(e) => e.preventDefault()}
          className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border px-3 text-caption font-semibold text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
        >
          <ExternalLink className="h-3.5 w-3.5" strokeWidth={1.75} />
          {t('routerDetail.info.openLuci')}
        </a>
        <button
          onClick={copySsh}
          className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border px-3 text-caption font-semibold text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
        >
          <Terminal className="h-3.5 w-3.5" strokeWidth={1.75} />
          {t('routerDetail.info.copySsh')}
        </button>
      </div>

      {/* Toast "Comando copiado" */}
      <AnimatePresence>
        {toast && (
          <motion.div
            initial={reduce ? { opacity: 0 } : { opacity: 0, y: 16 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, transition: { duration: 0.3 } }}
            transition={{ duration: 0.25, ease: 'easeOut' }}
            role="status"
            className="fixed bottom-24 left-1/2 z-50 -translate-x-1/2 md:bottom-8"
          >
            <div className="flex items-center gap-2 rounded-[10px] border border-border-strong bg-elevated px-3.5 py-2.5 shadow-lg">
              <Check className="h-4 w-4 text-ok" strokeWidth={1.75} />
              <span className="text-sm font-medium text-text-primary">{t('routerDetail.info.copied')}</span>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </section>
  )
}
