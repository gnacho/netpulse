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

/** Página Resumen `/` (home.md). Layout #965: hero compacto + alertas 50/50,
 *  tráfico WAN a todo lo ancho, flota, collector, SLEs y los servicios del
 *  gateway (AdGuard/WireGuard) AL FINAL. */
export default function Home() {
  const { isDemo } = useNetPulse()
  const [services] = useServicesVisibility()
  // Clave ESTABLE por tarjeta (#223): si AdGuard/WireGuard se deshabilitan en
  // Ajustes la lista se reordena y, con key={i}, React remontaría los paneles.
  // Cada id identifica al hijo, no su posición. Orden #983: AdGuard primero.
  const serviceCards: { id: string; node: ReactNode }[] = []
  if (services.adguard) serviceCards.push({ id: 'adguard', node: <AdGuardCard /> })
  if (services.wireguard) serviceCards.push({ id: 'wireguard', node: <WireGuardCard /> })
  const serviceSpan = serviceCards.length >= 2 ? 'lg:col-span-6' : 'lg:col-span-12'
  return (
    <div className="grid grid-cols-1 gap-4 md:gap-5 lg:grid-cols-12">
      {/* ① Hero compacto 50% + Alertas 50% (el alto lo marca la tarjeta de
          alertas; ambas secciones son h-full) */}
      <div className="lg:col-span-6">
        <HeroStrip compact />
      </div>
      <div className="lg:col-span-6">
        <RecentAlerts />
      </div>
      {/* ② Tráfico WAN a todo lo ancho (la gráfica de velocidad vive aquí) */}
      <div className="lg:col-span-12">
        <WanTraffic />
      </div>
      {/* ③ Tu flota */}
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
      {/* ⑥ Servicios del gateway (AdGuard/WireGuard) AL FINAL, span 6/12
          según cuántos sean visibles */}
      {serviceCards.map((c) => (
        <div key={c.id} className={serviceSpan}>
          {c.node}
        </div>
      ))}
      {/* Top dispositivos: solo demo (en live no hay tráfico por dispositivo) */}
      {isDemo && (
        <div className="lg:col-span-12">
          <TopDevices />
        </div>
      )}
    </div>
  )
}
