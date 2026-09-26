import type { MouseEvent as ReactMouseEvent } from 'react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

// #862: acción de fila con icono grande y etiqueta de texto que aparece con
// el hover de la FILA (no solo del icono). La etiqueta reserva su espacio con
// una transición max-width, así no empuja el resto del contenido de la fila.
// En pantallas estrechas (<sm) queda solo el icono (el hover de fila apenas
// existe en táctil; ver issue aparte de coarse pointers). Compartida por el
// feed (Alerts.tsx) y el resumen (AlertItem.tsx).
export function RowAction({
  icon: Icon,
  label,
  onClick,
  className,
  title,
}: {
  icon: LucideIcon
  label: string
  onClick: (e: ReactMouseEvent) => void
  className?: string
  title: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'flex items-center gap-1 rounded-md px-1.5 py-1 opacity-0 transition-opacity duration-150 group-hover:opacity-100',
        className,
      )}
      title={title}
    >
      <Icon className="h-4 w-4 shrink-0" strokeWidth={1.75} />
      <span className="hidden max-w-0 overflow-hidden whitespace-nowrap text-caption opacity-0 transition-all duration-200 group-hover:opacity-100 sm:inline-block sm:group-hover:max-w-40">
        {label}
      </span>
    </button>
  )
}
