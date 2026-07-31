import { AdGuardCard, WireGuardCard } from '@/sections/GatewayServices'
import { HeroStrip } from '@/sections/HeroStrip'
import { RecentAlerts } from '@/sections/RecentAlerts'
import { RoutersRow } from '@/sections/RoutersRow'
import { TopDevices } from '@/sections/TopDevices'
import { WanTraffic } from '@/sections/WanTraffic'
import { useNetPulse } from '@/data/DataProvider'

/** Página Resumen `/` (home.md) */
export default function Home() {
  const { isDemo } = useNetPulse()
  return (
    <div className="grid grid-cols-1 gap-4 md:gap-5 lg:grid-cols-12">
      {/* ① Hero 50% + Tráfico 50% */}
      <div className="lg:col-span-6">
        <HeroStrip />
      </div>
      <div className="lg:col-span-6">
        <WanTraffic />
      </div>
      {/* ② AdGuard + WireGuard + Alertas en una fila */}
      <div className="lg:col-span-4">
        <AdGuardCard />
      </div>
      <div className="lg:col-span-4">
        <WireGuardCard />
      </div>
      <div className="lg:col-span-4">
        <RecentAlerts />
      </div>
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
