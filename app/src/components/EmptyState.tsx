import { Link } from 'react-router-dom'
import { cn } from '@/lib/utils'

interface EmptyStateProps {
  /** Ruta de ilustración SVG en public/ */
  image: string
  title: string
  description?: string
  actionLabel?: string
  actionTo?: string
  className?: string
}

/** Estado vacío cálido (design.md §10.9) */
export function EmptyState({ image, title, description, actionLabel, actionTo, className }: EmptyStateProps) {
  return (
    <div className={cn('flex flex-col items-center justify-center gap-3 px-6 py-12 text-center', className)}>
      <img src={image} alt="" width={240} height={160} className="h-40 w-60 opacity-90" />
      <h3 className="font-display text-h2 text-text-primary">{title}</h3>
      {description && <p className="max-w-sm text-sm text-text-secondary">{description}</p>}
      {actionLabel && actionTo && (
        <Link
          to={actionTo}
          className="mt-1 rounded-lg border border-border px-4 py-2 text-sm font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
        >
          {actionLabel}
        </Link>
      )}
    </div>
  )
}
