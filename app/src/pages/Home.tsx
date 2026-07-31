import { GatewayServices } from '@/sections/GatewayServices'
import { HeroStrip } from '@/sections/HeroStrip'
import { RecentAlerts } from '@/sections/RecentAlerts'
import { RoutersRow } from '@/sections/RoutersRow'
import { TopDevices } from '@/sections/TopDevices'
import { WanTraffic } from '@/sections/WanTraffic'

/** Página Resumen `/` (home.md) */
export default function Home() {
  return (
    <div className="grid grid-cols-1 gap-4 md:gap-5 lg:grid-cols-12">
      {/* ① Hero strip */}
      <div className="lg:col-span-12">
        <HeroStrip />
      </div>
      {/* ④ Routers (en móvil va tras el hero, home.md §Mobile) */}
      <div className="lg:col-span-12 lg:order-3">
        <RoutersRow />
      </div>
      {/* ② Tráfico WAN */}
      <div className="lg:col-span-8 lg:order-1">
        <WanTraffic />
      </div>
      {/* ③ Servicios del gateway */}
      <div className="lg:col-span-4 lg:order-2">
        <GatewayServices />
      </div>
      {/* ⑤ Top dispositivos */}
      <div className="lg:col-span-7 lg:order-4">
        <TopDevices />
      </div>
      {/* ⑥ Alertas recientes */}
      <div className="lg:col-span-5 lg:order-5">
        <RecentAlerts />
      </div>
    </div>
  )
}
