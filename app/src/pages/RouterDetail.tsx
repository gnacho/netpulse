import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useLocation, useParams } from 'react-router-dom'
import { AlertTriangle, ArrowLeft, Router as RouterIcon } from 'lucide-react'
import { motion, useReducedMotion } from 'framer-motion'
import { relTime } from '@/i18n'
import { useNetPulse } from '@/data/DataProvider'
import type { RouterDetailData } from '@/data/DataProvider'
import { AdGuardPanel } from '@/components/routers/AdGuardPanel'
import { BackhaulPanel } from '@/components/routers/BackhaulPanel'
import { PortPanel } from '@/components/routers/PortPanel'
import { RadiosPorts } from '@/components/routers/RadiosPorts'
import { RouterClients } from '@/components/routers/RouterClients'
import { RouterDetailHeader } from '@/components/routers/RouterDetailHeader'
import { RouterInfo } from '@/components/routers/RouterInfo'
import { RouterPerformance } from '@/components/routers/RouterPerformance'
import { WanLatency } from '@/components/routers/WanLatency'
import { WireGuardPanel } from '@/components/routers/WireGuardPanel'

/** Página `/routers/:id` — plantilla de detalle (router-detail.md) */
export default function RouterDetail() {
  const { t } = useTranslation()
  const { id = '' } = useParams()
  const location = useLocation()
  const reduce = useReducedMotion()
  const { routers, alerts, getRouterDetail } = useNetPulse()
  const router = routers.find((r) => r.id === id)
  const isGateway = router?.roleBadge === 'Principal'
  const [detail, setDetail] = useState<RouterDetailData | null>(null)

  // Detalle vivo del backend (extras: radios, bocas LAN con dispositivo, info)
  // NO se nullea al refrescar ni se sustituye si no cambia nada: evita
  // parpadeo/re-animación en cada snapshot SSE
  useEffect(() => {
    let disposed = false
    void getRouterDetail(id).then((d) => {
      if (disposed || !d) return
      setDetail((prev) => (prev && JSON.stringify(prev) === JSON.stringify(d) ? prev : d))
    })
    return () => {
      disposed = true
    }
  }, [id, getRouterDetail, routers])

  // Scroll al inicio al cambiar de router
  useEffect(() => {
    if (!location.hash) window.scrollTo({ top: 0 })
  }, [id, location.hash])

  // Scroll-spy: anchors #adguard / #wireguard → scroll suave + highlight flash
  useEffect(() => {
    if (!location.hash) return
    const el = document.querySelector(location.hash)
    if (!el) return
    const t = window.setTimeout(() => {
      el.scrollIntoView({ behavior: reduce ? 'auto' : 'smooth', block: 'start' })
      el.classList.add('ring-2', 'ring-accent', 'rounded-2xl')
      window.setTimeout(() => el.classList.remove('ring-2', 'ring-accent'), 1000)
    }, 350)
    return () => window.clearTimeout(t)
  }, [location.hash, reduce])

  if (!router) {
    return (
      <div className="flex flex-col items-center justify-center gap-4 rounded-2xl border border-border bg-surface px-6 py-16 text-center">
        <div className="flex h-14 w-14 items-center justify-center rounded-xl bg-elevated text-text-muted">
          <RouterIcon className="h-7 w-7" strokeWidth={1.75} />
        </div>
        <div>
          <h1 className="font-display text-h1 text-text-primary">{t('routerDetail.notFound')}</h1>
          <p className="mt-1 text-sm text-text-secondary">
            {t('routerDetail.notFoundDesc', { id })}
          </p>
        </div>
        <Link
          to="/routers"
          className="inline-flex items-center gap-2 rounded-lg border border-border px-4 py-2 text-sm font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
        >
          <ArrowLeft className="h-4 w-4" strokeWidth={1.75} />
          {t('routerDetail.backToRouters')}
        </Link>
      </div>
    )
  }

  const tempAlert = alerts.find((a) => a.routerId === router.id && a.id === 'alert-temp-patio')

  return (
    <div className="grid grid-cols-1 gap-4 md:gap-5 lg:grid-cols-12">
      {/* ① Detail header */}
      <div className="lg:col-span-12">
        <RouterDetailHeader router={router} />
      </div>

      {/* Banner contextual (Patio) */}
      {router.status === 'warn' && router.hotMetric === 'temp' && (
        <motion.div
          initial={reduce ? false : { opacity: 0, y: -8 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.3, ease: 'easeOut', delay: 0.4 }}
          className="lg:col-span-12"
        >
          <div className="flex items-start gap-3 rounded-2xl border border-warn/50 bg-warn/10 px-5 py-4 shadow-glow-warn">
            <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-warn" strokeWidth={1.75} />
            <div className="min-w-0 flex-1">
              <div className="text-sm font-semibold text-warn">
                {t('routerDetail.highTempBanner', { temp: router.temp })}
              </div>
              <p className="mt-0.5 text-sm text-text-secondary">
                {t('routerDetail.highTempAdvice')}
              </p>
            </div>
            {tempAlert && (
              <span className="shrink-0 font-mono text-caption text-text-muted">{relTime(tempAlert.time)}</span>
            )}
          </div>
        </motion.div>
      )}

      {/* ② Rendimiento */}
      <div className="lg:col-span-8">
        <RouterPerformance router={router} />
      </div>

      {/* ③ Info + Red (en móvil va la última) */}
      <div className="order-last lg:order-none lg:col-span-4">
        <RouterInfo router={router} extras={detail?.extras} />
      </div>

      {/* ④ WAN & Latencia (gateway) / Backhaul (APs) */}
      {isGateway ? <WanLatency /> : <BackhaulPanel router={router} extras={detail?.extras} />}

      {/* ⑤⑥ Servicios + Puertos (gateway) / Radios + Puertos (APs) */}
      {isGateway ? (
        <>
          <AdGuardPanel />
          <WireGuardPanel />
          <PortPanel router={router} extras={detail?.extras} className="lg:col-span-12" />
        </>
      ) : (
        <RadiosPorts router={router} extras={detail?.extras} />
      )}

      {/* ⑦ Clientes de este router */}
      <RouterClients router={router} />
    </div>
  )
}
