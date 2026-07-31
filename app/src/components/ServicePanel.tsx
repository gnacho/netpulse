import type { LucideIcon } from 'lucide-react'
import { motion } from 'framer-motion'
import { cn } from '@/lib/utils'

interface ServicePanelProps {
  icon: LucideIcon
  /** Tono del tile de icono */
  tone: 'ok' | 'tunnel' | 'accent' | 'warn'
  title: string
  /** Pill de estado (slot) */
  status: React.ReactNode
  /** Footer: CTA ghost (slot) */
  footer?: React.ReactNode
  children: React.ReactNode
  index?: number
  className?: string
}

const TONE_TILE: Record<ServicePanelProps['tone'], string> = {
  ok: 'bg-ok/10 text-ok',
  tunnel: 'bg-tunnel/10 text-tunnel',
  accent: 'bg-accent-soft text-accent',
  warn: 'bg-warn/10 text-warn',
}

/** Panel de servicio del gateway (design.md §10.4): cabecera icono+estado, stats propias, CTA ghost. */
export function ServicePanel({ icon: Icon, tone, title, status, footer, children, index = 0, className }: ServicePanelProps) {
  return (
    <motion.section
      initial={{ opacity: 0, y: 20 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.4, ease: 'easeOut', delay: index * 0.12 }}
      className={cn(
        'flex flex-col rounded-2xl border border-border bg-surface p-5 transition-all duration-150',
        'hover:-translate-y-0.5 hover:border-accent/40',
        className,
      )}
    >
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className={cn('flex h-9 w-9 items-center justify-center rounded-lg', TONE_TILE[tone])}>
            <Icon className="h-[18px] w-[18px]" strokeWidth={1.75} />
          </div>
          <h3 className="font-display text-h2 text-text-primary">{title}</h3>
        </div>
        {status}
      </div>
      <div className="mt-4 flex-1">{children}</div>
      {footer && <div className="mt-4 border-t border-border pt-3">{footer}</div>}
    </motion.section>
  )
}
