import type { ReactNode } from 'react'
import { CollectorCharts } from '@/components/CollectorCharts'
import { WiFiSLECard } from '@/components/WiFiSLECard'
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
  // Clave ESTABLE por tarjeta (#223): si AdGuard/WireGuard se deshabilitan en
  // Ajustes la lista se reordena y, con key={i}, React remontaría RecentAlerts
  // y perdería su readIds. Cada id identifica al hijo, no su posición.
  const cards: { id: string; node: ReactNode }[] = []
  if (services.adguard) cards.push({ id: 'adguard', node: <AdGuardCard /> })
  if (services.wireguard) cards.push({ id: 'wireguard', node: <WireGuardCard /> })
  cards.push({ id: 'alerts', node: <RecentAlerts /> })
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
      {cards.map((c) => (
        <div key={c.id} className={spanCls}>
          {c.node}
        </div>
      ))}
      {/* ③ Routers */}
      <div className="lg:col-span-12">
        <RoutersRow />
      </div>
      {/* ④ Collector sidecar (#328): latencia TCP por router desde el daemon */}
      {!isDemo && (
        <div className="lg:col-span-12">
          <CollectorCharts />
        </div>
      )}
      {/* ⑤ WiFi SLEs (#342): Service Level Expectations por router */}
      {!isDemo && (
        <div className="lg:col-span-12">
          <WiFiSLECard />
        </div>
      )}
      {/* Top dispositivos: solo demo (en live no hay tráfico por dispositivo) */}
      {isDemo && (
        <div className="lg:col-span-12">
          <TopDevices />
        </div>
      )}
    </div>
  )
}
