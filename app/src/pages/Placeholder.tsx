import { useTranslation } from 'react-i18next'
import { useParams } from 'react-router-dom'
import { routerName } from '@/data/mock'

interface PlaceholderProps {
  title: string
}

/** Stub de página: los page agents lo reemplazarán. */
export default function Placeholder({ title }: PlaceholderProps) {
  const { t } = useTranslation()
  const { id } = useParams()
  const resolved = id ? `${title}: ${routerName(id)}` : title
  return (
    <div className="flex min-h-[50dvh] flex-col items-center justify-center gap-3 rounded-2xl border border-dashed border-border-strong bg-surface p-10 text-center">
      <img src="/logo.svg" alt="" className="h-12 w-12 opacity-60" />
      <h1 className="font-display text-h1 text-text-primary">{resolved}</h1>
      <p className="max-w-sm text-sm text-text-secondary">
        {t('notFound.construction')}
      </p>
    </div>
  )
}
