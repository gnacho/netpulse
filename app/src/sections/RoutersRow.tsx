import { useTranslation } from 'react-i18next'
import { RouterCard } from '@/components/RouterCard'
import { SectionHeader } from '@/components/SectionHeader'
import { StatusPill } from '@/components/StatusPill'
import { useNetPulse } from '@/data/DataProvider'

/** ④ Routers — 4 RouterCards en fila (home.md §④) */
export function RoutersRow() {
  const { t } = useTranslation()
  const { routers } = useNetPulse()
  const online = routers.filter((r) => r.status === 'online').length
  const warnings = routers.filter((r) => r.status === 'warn').length
  return (
    <section>
      <SectionHeader title={t('home.routersRow.title')} linkTo="/routers" linkLabel={t('common.viewAll')} className="mb-4">
        <StatusPill tone={warnings > 0 ? 'warn' : 'ok'} label={t('home.routersRow.summary', { online, warnings })} />
      </SectionHeader>
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 md:gap-5 xl:grid-cols-4">
        {routers.map((r, i) => (
          <RouterCard key={r.id} router={r} index={i} />
        ))}
      </div>
    </section>
  )
}
