import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useLocation, useParams } from 'react-router'
import { AlertTriangle, ArrowLeft, Gauge, KeyRound, Router as RouterIcon, ShieldAlert } from 'lucide-react'
import { motion, useReducedMotion } from 'framer-motion'
import { alertRelTime } from '@/i18n'
import { fmtTemp, useTempUnit } from '@/lib/temperature'
import { useNetPulse } from '@/data/DataProvider'
import type { RouterDetailData } from '@/data/DataProvider'
import { useAuth } from '@/data/AuthContext'
import { AdGuardPanel } from '@/components/routers/AdGuardPanel'
import { BackhaulPanel } from '@/components/routers/BackhaulPanel'
import { PortPanel } from '@/components/routers/PortPanel'
import { PortSeriesChart } from '@/components/routers/PortSeriesChart'
import { RadiosPorts } from '@/components/routers/RadiosPorts'
import { RouterClients } from '@/components/routers/RouterClients'
import { RouterDetailHeader } from '@/components/routers/RouterDetailHeader'
import { RouterInfo } from '@/components/routers/RouterInfo'
import { RouterPerformance } from '@/components/routers/RouterPerformance'
import { MultiWanPanel } from '@/components/routers/MultiWanPanel'
import { WanLatency } from '@/components/routers/WanLatency'
import { VlanPanel } from '@/components/routers/VlanPanel'
import { WireGuardPanel } from '@/components/routers/WireGuardPanel'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'

/** Página `/routers/:id` — plantilla de detalle (router-detail.md) */
export default function RouterDetail() {
  const { t } = useTranslation()
  const { id = '' } = useParams()
  const location = useLocation()
  const reduce = useReducedMotion()
  const auth = useAuth()
  const isAdmin = auth?.role === 'admin'
  const { routers, alerts, getRouterDetail, isDemo } = useNetPulse()
  const [tempUnit] = useTempUnit()
  const router = routers.find((r) => r.id === id)
  const isGateway = router?.roleBadge === 'Principal'
  // #1477: forgetting the host key only makes sense on units NetPulse polls
  // over SSH (OpenWrt/GL.iNet). Agent-only units and external scrapers do not
  // talk SSH to the server.
  const sshPolled = !!router && !router.agentOnly &&
    (router.type === 'openwrt' || router.type === 'glinet')
  const [detail, setDetail] = useState<RouterDetailData | null>(null)
  const [hkBusy, setHkBusy] = useState(false)
  const [hkDone, setHkDone] = useState(false)
  const [hkErr, setHkErr] = useState('')
  const [forgetOpen, setForgetOpen] = useState(false)
  // #930: resumen de salud SNMP del switch (solo routers sondeados por SNMP).
  const snmpStats = detail?.snmpOk !== undefined
    ? { ok: detail.snmpOk, fail: detail.snmpFail, lastErr: detail.lastSnmpErr }
    : undefined

  // #603: confirmación explícita del re-onboard tras una host key cambiada.
  // #1477: la misma llamada sirve como acción proactiva ("olvidar la clave").
  async function handleAcceptHostKey() {
    if (hkBusy) return
    setHkBusy(true)
    setHkErr('')
    setHkDone(false)
    try {
      const res = await fetch(`/api/routers/${encodeURIComponent(id)}/accept-host-key`, { method: 'POST' })
      if (res.status === 202 || res.status === 200) {
        setHkDone(true)
      } else {
        setHkErr(t('routerDetail.hostKeyForgetError', { status: res.status }))
      }
    } catch {
      setHkErr(t('routerDetail.hostKeyForgetError', { status: 'network' }))
    } finally {
      setHkBusy(false)
    }
  }

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

      {/* #1477: acción proactiva de admin para olvidar la host key SSH. Los
          casos que la necesitan (firmware que regenera las claves, reflash,
          unidad sustituida en la misma IP) rompen el SSH antes de que el
          sondeo pueda marcar hostKeyChanged, así que el aviso reactivo de
          #603 no basta: aquí está la salida con confirmación explícita. */}
      {isAdmin && sshPolled && (
        <div className="lg:col-span-12">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-2xl border border-border bg-surface px-5 py-3">
            <KeyRound className="h-4 w-4 shrink-0 text-text-muted" strokeWidth={1.75} />
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-text-primary">{t('routerDetail.hostKeyForgetTitle')}</div>
              <p className="mt-0.5 text-sm text-text-secondary">{t('routerDetail.hostKeyForgetBody')}</p>
              {hkDone && <p className="mt-1 text-sm font-medium text-ok">{t('routerDetail.hostKeyForgetDone')}</p>}
              {hkErr && <p role="alert" className="mt-1 text-sm font-medium text-danger">{hkErr}</p>}
            </div>
            <button
              type="button"
              onClick={() => setForgetOpen(true)}
              disabled={hkBusy}
              className="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-border bg-canvas px-3 py-2 text-sm font-medium text-text-secondary transition-colors hover:border-danger/50 hover:text-danger disabled:opacity-50"
            >
              {t('routerDetail.hostKeyForgetAction')}
            </button>
          </div>
        </div>
      )}

      <AlertDialog open={forgetOpen} onOpenChange={setForgetOpen}>
        <AlertDialogContent className="border-danger/40 bg-surface">
          <AlertDialogHeader>
            <AlertDialogTitle>{t('routerDetail.hostKeyForgetDialogTitle')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('routerDetail.hostKeyForgetDialogDesc', { name: router.name || router.id })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('common.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90 focus-visible:ring-destructive/20"
              onClick={(e) => {
                e.preventDefault()
                setForgetOpen(false)
                void handleAcceptHostKey()
              }}
            >
              {t('routerDetail.hostKeyForgetConfirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* #603: host key cambiada → requiere re-onboard explícito (MITM) */}
      {router.hostKeyChanged && (
        <motion.div
          initial={reduce ? false : { opacity: 0, y: -8 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.3, ease: 'easeOut', delay: 0.35 }}
          className="lg:col-span-12"
        >
          <div role="alert" className="flex items-start gap-3 rounded-2xl border border-danger/50 bg-danger/10 px-5 py-4">
            <ShieldAlert className="mt-0.5 h-5 w-5 shrink-0 text-danger" strokeWidth={1.75} />
            <div className="min-w-0 flex-1">
              <div className="text-sm font-semibold text-danger">{t('routerDetail.hostKeyTitle')}</div>
              <p className="mt-0.5 text-sm text-text-secondary">{t('routerDetail.hostKeyBody')}</p>
              {hkDone && <p className="mt-1 text-sm font-medium text-ok">{t('routerDetail.hostKeyDone')}</p>}
            </div>
            {isAdmin && !hkDone && (
              <button
                type="button"
                onClick={() => void handleAcceptHostKey()}
                disabled={hkBusy}
                className="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-danger/40 bg-canvas px-3 py-2 text-sm font-semibold text-danger transition-colors hover:border-danger disabled:opacity-50"
              >
                {hkBusy ? t('routerDetail.hostKeyBusy') : t('routerDetail.hostKeyConfirm')}
              </button>
            )}
          </div>
        </motion.div>
      )}

      {/* #1369: lldpd escuchando solo en el bridge (br-lan) → 0 vecinos LLDP
          silencioso en nss-dp/DSA. El agente lo detecta y avisa aquí. */}
      {router.lldpHint === 'lldpd-bridge-only' && (
        <motion.div
          initial={reduce ? false : { opacity: 0, y: -8 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.3, ease: 'easeOut', delay: 0.38 }}
          className="lg:col-span-12"
        >
          <div role="alert" className="flex items-start gap-3 rounded-2xl border border-warn/50 bg-warn/10 px-5 py-4">
            <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-warn" strokeWidth={1.75} />
            <div className="min-w-0 flex-1">
              <div className="text-sm font-semibold text-warn">{t('routerDetail.lldpBridgeOnlyTitle')}</div>
              <p className="mt-0.5 text-sm text-text-secondary">{t('routerDetail.lldpBridgeOnlyBody')}</p>
            </div>
          </div>
        </motion.div>
      )}

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
                {t('routerDetail.highTempBanner', { temp: fmtTemp(router.temp, tempUnit), threshold: fmtTemp(router.tempThreshold ?? 65, tempUnit) })}
              </div>
              <p className="mt-0.5 text-sm text-text-secondary">
                {t('routerDetail.highTempAdvice')}
              </p>
            </div>
            {tempAlert && (
              <span className="shrink-0 font-mono text-caption text-text-muted">{alertRelTime(tempAlert)}</span>
            )}
          </div>
        </motion.div>
      )}

      {/* ② Rendimiento (#441: placeholder si la fuente no puede dar vitals) */}
      <div className="lg:col-span-8">
        {router.vitalsAvailable === false ? (
          <section className="flex h-full flex-col items-center justify-center gap-3 rounded-2xl border border-border bg-surface p-8 text-center">
            <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-elevated text-text-muted">
              <Gauge className="h-6 w-6" strokeWidth={1.75} />
            </div>
            <div>
              <h2 className="font-display text-h2 text-text-primary">{t('routerDetail.perf.unavailableTitle')}</h2>
              <p className="mx-auto mt-1 max-w-md text-sm text-text-secondary">{t('routerDetail.perf.unavailableBody')}</p>
            </div>
          </section>
        ) : (
          <RouterPerformance router={router} liveSeries={detail?.series} totalRamMb={detail?.extras?.ramMb} usedRamMb={detail?.extras?.usedMb} />
        )}
      </div>

      {/* ③ Info + Red (en móvil va la última) */}
      <div className="order-last lg:order-none lg:col-span-4">
        <RouterInfo router={router} extras={detail?.extras} />
      </div>

      {/* ④ WAN & Latencia (gateway) / Backhaul (APs) */}
      {isGateway ? <WanLatency extras={detail?.extras} /> : <BackhaulPanel router={router} extras={detail?.extras} />}

      {/* Las varias conexiones a internet, si el router reporta más de una.
          Va junto a la tarjeta de conexión porque explica la IP que muestra. */}
      {(detail?.multiWan?.uplinks?.length ?? 0) >= 2 && detail?.multiWan && (
        <MultiWanPanel info={detail.multiWan} />
      )}

      {/* ⑤⑥ Servicios + Puertos (gateway) / Radios + Puertos (APs) */}
      {isGateway ? (
        <>
          <AdGuardPanel />
          <WireGuardPanel />
          <PortPanel router={router} extras={detail?.extras} snmpStats={snmpStats} className="lg:col-span-12" />
        </>
      ) : (
        <RadiosPorts router={router} extras={detail?.extras} snmpStats={snmpStats} />
      )}

      {/* Historial de tráfico por puerto: tarjeta propia a todo el ancho
          (issue #654; no va embebido en la tarjeta de Puertos Ethernet). */}
      {!isDemo && detail?.extras?.ethPorts && detail.extras.ethPorts.length > 0 && (
        <PortSeriesChart
          routerId={router.id}
          ports={detail.extras.ethPorts}
          // #641/#661: los beacons RTLPlayground solo reportan tramas (fps),
          // pero un managed-switch sondeado por SNMP sí tiene contadores de
          // bytes → se pinta en bps. Las unidades con agente (external sin
          // SNMP) TAMBIÉN llevan contadores de bytes: mismo criterio que
          // FleetCard (vitalsAvailable === false identifica al beacon). El
          // gráfico además se autocorrige desde los datos (#1502).
          unit={router.snmpEnabled
            ? 'bps'
            : router.vitalsAvailable === false
              ? 'fps'
              : 'bps'}
          className="lg:col-span-12"
        />
      )}

      {/* VLANs del bridge (issue #315, read-only) */}
      {detail?.vlans && detail.vlans.length > 0 && (
        <VlanPanel vlans={detail.vlans} />
      )}

      {/* ⑦ Clientes de este router */}
      <RouterClients router={router} />
    </div>
  )
}
