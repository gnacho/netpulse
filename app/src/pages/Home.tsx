import { AdGuardCard, WireGuardCard } from '@/sections/GatewayServices'
import { HeroStrip } from '@/sections/HeroStrip'
import { RecentAlerts } from '@/sections/RecentAlerts'
import { RoutersRow } from '@/sections/RoutersRow'
import { TopDevices } from '@/sections/TopDevices'
import { WanTraffic } from '@/sections/WanTraffic'
import { useNetPulse } from '@/data/DataProvider'
import { useServicesVisibility } from '@/hooks/useServicesVisibility'

/** Página Resumen `/` (home.md) */
export default function Home() {
  const { isDemo } = useNetPulse()
  const [services] = useServicesVisibility()
  const cards = [
    services.adguard && <AdGuardCard key="adguard" />,
    services.wireguard && <WireGuardCard key="wireguard" />,
    <RecentAlerts key="alerts" />,
  ].filter(Boolean)
  const span = Math.max(4, Math.floor(12 / cards.length))
  const spanCls = {
    4: 'lg:col-span-4',
    6: 'lg:col-span-6',
    12: 'lg:col-span-12',
  }[span]
  return (
    <div className="grid grid-cols-1 gap-4 md:gap-5 lg:grid-cols-12">
      {/* ① Hero 50% + Tráfico 50% */}
      <div className="lg:col-span-6">
        <HeroStrip />
      </div>
      <div className="lg:col-span-6">
        <WanTraffic />
      </div>
      {/* ② Servicios marcados en Ajustes + Alertas en una fila */}
      {cards.map((card, i) => (
        <div key={i} className={spanCls}>
          {card}
        </div>
      ))}
      {/* ③ Routers */}
      <div className="lg:col-span-12">
        <RoutersRow />
      </div>
      {/* Top dispositivos: solo demo (en live no hay tráfico por dispositivo) */}
      {isDemo && (
        <div className="lg:col-span-12">
          <TopDevices />
        </div>
      )}
    </div>
  )
}
