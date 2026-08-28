import { useState } from 'react'
import { ChevronDown, Network } from 'lucide-react'
import { motion, AnimatePresence, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import type { VlanPort } from '@/data/types'
import { SectionHeader } from '@/components/SectionHeader'
import { cn } from '@/lib/utils'

function VlanBadge({ tagged }: { tagged: boolean }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full px-1.5 py-0.5 font-mono text-[10px] font-semibold',
        tagged
          ? 'bg-accent/15 text-accent'
          : 'bg-ok/15 text-ok',
      )}
    >
      {tagged ? t('routerDetail.vlans.tagged') : t('routerDetail.vlans.untagged')}
    </span>
  )
}

export function VlanPanel({ vlans }: { vlans: VlanPort[] }) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const [open, setOpen] = useState(true)

  if (!vlans || vlans.length === 0) return null

  const vlanCount = new Set(vlans.flatMap((p) => p.vlans.map((v) => v.id))).size

  return (
    <section className="rounded-2xl border border-border bg-surface p-5 md:p-6 lg:col-span-12">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center justify-between gap-3"
        aria-expanded={open}
        aria-label={t('routerDetail.vlans.toggleAria', { count: vlanCount })}
      >
        <SectionHeader title={t('routerDetail.vlans.title')} />
        <div className="flex items-center gap-2">
          <span className="rounded-full bg-accent/10 px-2 py-0.5 font-mono text-caption font-semibold text-accent">
            {vlanCount} VLAN{vlanCount !== 1 ? 's' : ''}
          </span>
          <ChevronDown
            className={cn(
              'h-4 w-4 text-text-muted transition-transform duration-200',
              open && 'rotate-180',
            )}
            strokeWidth={1.75}
          />
        </div>
      </button>

      <AnimatePresence initial={false}>
        {open && (
          <motion.div
            key="vlans-body"
            initial={{ height: reduce ? 'auto' : 0, opacity: reduce ? 1 : 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: reduce ? 'auto' : 0, opacity: reduce ? 1 : 0 }}
            transition={{ duration: 0.2, ease: 'easeInOut' }}
            className="overflow-hidden"
          >
            <div className="mt-4 overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead>
                  <tr className="border-b border-border/60 text-[10px] font-semibold uppercase tracking-[0.06em] text-text-muted">
                    <th className="pb-2 pr-4">{t('routerDetail.vlans.port')}</th>
                    <th className="pb-2 pr-4">{t('routerDetail.vlans.vlanId')}</th>
                    <th className="pb-2 pr-4">{t('routerDetail.vlans.mode')}</th>
                    <th className="pb-2">{t('routerDetail.vlans.pvid')}</th>
                  </tr>
                </thead>
                <tbody>
                  {vlans.map((vp) =>
                    vp.vlans.map((v, vi) => (
                      <tr
                        key={`${vp.port}-${v.id}`}
                        className={cn(
                          'border-b border-border/30 last:border-0',
                          vi > 0 && 'bg-elevated/20',
                        )}
                      >
                        <td className="py-2 pr-4">
                          {vi === 0 ? (
                            <span className="flex items-center gap-1.5 font-mono text-mono-sm font-semibold text-text-primary">
                              <Network className="h-3.5 w-3.5 text-text-muted" strokeWidth={1.75} />
                              {vp.port}
                            </span>
                          ) : (
                            <span className="pl-[22px] text-text-muted" aria-hidden="true" />
                          )}
                        </td>
                        <td className="py-2 pr-4 font-mono text-mono-sm text-text-primary">{v.id}</td>
                        <td className="py-2 pr-4"><VlanBadge tagged={v.tagged} /></td>
                        <td className="py-2">
                          {v.pvid ? (
                            <span className="inline-flex items-center rounded-full bg-warn/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-warn">
                              PVID
                            </span>
                          ) : (
                            <span className="text-text-muted">-</span>
                          )}
                        </td>
                      </tr>
                    )),
                  )}
                </tbody>
              </table>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </section>
  )
}
