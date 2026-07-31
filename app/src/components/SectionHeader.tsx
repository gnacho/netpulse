import { Link } from 'react-router-dom'
import { ArrowRight } from 'lucide-react'
import { cn } from '@/lib/utils'

interface SectionHeaderProps {
  title: string
  /** Ruta del link "Ver todo →" */
  linkTo?: string
  linkLabel?: string
  /** Slot extra a la derecha (pills, badges, controles) */
  children?: React.ReactNode
  className?: string
}

/** Título SG-600 + link accent (design.md §10.10) */
export function SectionHeader({ title, linkTo, linkLabel = 'Ver todo', children, className }: SectionHeaderProps) {
  return (
    <div className={cn('flex flex-wrap items-center justify-between gap-x-3 gap-y-2', className)}>
      <div className="flex min-w-0 items-center gap-2.5">
        <h2 className="font-display text-h2 text-text-primary truncate">{title}</h2>
        {children}
      </div>
      {linkTo && (
        <Link
          to={linkTo}
          className="group inline-flex items-center gap-1 text-caption font-semibold text-accent transition-colors hover:text-accent/80"
        >
          {linkLabel}
          <ArrowRight className="h-3.5 w-3.5 transition-transform duration-150 group-hover:translate-x-0.5" strokeWidth={1.75} />
        </Link>
      )}
    </div>
  )
}
