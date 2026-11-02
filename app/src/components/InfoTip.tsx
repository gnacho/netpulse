import { Info } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

// InfoTip (#975): icono (i) con el texto largo en un tooltip al pasar el
// ratón, para que la sección quede con lo justo. Reusa el Tooltip radix de
// la app (mismo patrón que PortPanel). Compartido por Settings y las cards
// de integración (#1002).
export function InfoTip({ text }: { text: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          // #1064: sin tabIndex={-1}, el diálogo que lo contiene focaliza este
          // botón al abrir (es el primer enfocable) y el tooltip salta solo.
          // El texto sigue en aria-label para lectores de pantalla.
          tabIndex={-1}
          aria-label={text}
          className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full text-text-muted transition-colors hover:text-accent"
        >
          <Info className="h-3.5 w-3.5" strokeWidth={1.75} />
        </button>
      </TooltipTrigger>
      <TooltipContent side="top" className="max-w-xs text-left leading-relaxed">
        {text}
      </TooltipContent>
    </Tooltip>
  )
}
