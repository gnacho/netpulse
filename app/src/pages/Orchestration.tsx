import { useEffect, useMemo, useRef, useState, type ChangeEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { motion } from 'framer-motion'
import { ChevronRight, CheckCircle2, AlertCircle, Loader2, Wand2, ShieldAlert, Plus, X } from 'lucide-react'
import { useNetPulse } from '@/data/DataProvider'
import { useAuth } from '@/data/AuthContext'

type Module = 'adguard' | 'guestwifi' | 'ddns' | 'sqm' | 'wireguard' | 'usteer'

interface Op {
  kind: string
  desc: string
  args: Record<string, string>
}

interface Plan {
  id: string
  routerId: string
  resource: string
  diff: Op[]
  status: string
  method?: string
  result?: { status: string; error?: string; duration_ms?: number }
}

// methodLabel devuelve la clave i18n para el método detectado del módulo.
const methodLabel = (module: Module, method: string) => {
  const base = `orchestration.${module}.method`
  switch (method) {
    case 'apk': return `${base}.apk`
    case 'opkg': return `${base}.opkg`
    case 'none': return `${base}.none`
    case 'binary': return `${base}.binary`
    case 'enabled': return `${base}.enabled`
    case 'disabled': return `${base}.disabled`
    case 'active': return `${base}.active`
    case 'inactive': return `${base}.inactive`
    default: return ''
  }
}

export default function Orchestration() {
  const { t } = useTranslation()
  const { agents, routers } = useNetPulse()
  const auth = useAuth()
  // Módulo activo de orquestación (Fase 18).
  const [module, setModule] = useState<Module>('adguard')
  const [routerId, setRouterId] = useState('')

  // AdGuard (17.1): puerto + DNS upstream.
  const [agEnabled, setAgEnabled] = useState(true)
  const [agPort, setAgPort] = useState('3000')
  const [agDns, setAgDns] = useState('1.1.1.1')
  // Guest WiFi (17.2): SSID + password + banda.
  const [gwEnabled, setGwEnabled] = useState(true)
  const [gwSsid, setGwSsid] = useState('NetPulse-Guest')
  const [gwPassword, setGwPassword] = useState('')
  const [gwBand, setGwBand] = useState('2g')
  // DDNS (17.3): proveedor + dominio + credenciales.
  const [ddEnabled, setDdEnabled] = useState(true)
  const [ddService, setDdService] = useState('duckdns.org')
  const [ddDomain, setDdDomain] = useState('')
  const [ddUser, setDdUser] = useState('')
  const [ddPass, setDdPass] = useState('')
  // QoS / SQM (17.4): interfaz + ancho de banda + qdisc.
  const [sqEnabled, setSqEnabled] = useState(true)
  const [sqInterface, setSqInterface] = useState('eth1')
  const [sqDownload, setSqDownload] = useState('100000')
  const [sqUpload, setSqUpload] = useState('20000')
  const [sqQdisc, setSqQdisc] = useState('cake')
  const [sqScript, setSqScript] = useState('piece_of_cake.qos')
  const [sqLinklayer, setSqLinklayer] = useState('ethernet')
  const [sqOverhead, setSqOverhead] = useState('44')
  // WireGuard (10.3): interfaz + peers del túnel + pubkey del admin (anti-lockout).
  const [wgInterface, setWgInterface] = useState('wg0')
  const [wgPeers, setWgPeers] = useState([{ name: '', publicKey: '', allowedIps: '' }])
  const [wgAdminPeer, setWgAdminPeer] = useState('')
  // usteer (Fase 17.10; sustituye a DAWN).
  const [usteerEnabled, setUsteerEnabled] = useState(true)
  const [usteerSsid, setUsteerSsid] = useState('')
  const [usteerMobilityDomain, setUsteerMobilityDomain] = useState('2025')
  const [usteerAggressiveness, setUsteerAggressiveness] = useState('3')
  const [usteerBandSteeringThreshold, setUsteerBandSteeringThreshold] = useState('5')
  const [usteerLoadBalancingThreshold, setUsteerLoadBalancingThreshold] = useState('0')
  const [usteerDebugLevel, setUsteerDebugLevel] = useState('2')
  const [usteerK, setUsteerK] = useState(true)
  const [usteerR, setUsteerR] = useState(true)
  const [usteerV, setUsteerV] = useState(true)
  const [usteerBssTransition, setUsteerBssTransition] = useState(true)
  const [usteerFtOverDs, setUsteerFtOverDs] = useState(false)
  const [usteerFtPskGenerateLocal, setUsteerFtPskGenerateLocal] = useState(true)

  // issue #120: todos los módulos son gateway-only por defecto. El toggle
  // "advanced" permite un router no-gateway (con warning + checks).
  const [allowNonGateway, setAllowNonGateway] = useState(false)
  const [plan, setPlan] = useState<Plan | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  // warnCode: el último generate recibió un 422 con código conocido
  // (managed_by_firmware / gateway_only) → banner ámbar específico.
  const [warnCode, setWarnCode] = useState('')

  // El gateway del view-model (roleBadge 'Principal'); el selector lo lista
  // solo por defecto (gateway-only, #120).
  const gatewayId = routers.find((r) => r.roleBadge === 'Principal')?.id
  // Routers del dropdown: solo el gateway por defecto; todos si el toggle
  // advanced está activo. Se listan los AGENTES conectados (el apply usa SSE).
  // useMemo: identidad estable para no re-disparar el efecto de auto-select en
  // cada render (#225).
  const visibleRouters = useMemo(
    () => (allowNonGateway ? agents : agents.filter((a) => a.slug === gatewayId)),
    [agents, allowNonGateway, gatewayId],
  )
  const nonGatewaySelected = allowNonGateway && routerId !== gatewayId

  // Poll de applyPlan: el id vive en un ref para limpiarlo en unmount y al
  // generar un plan nuevo / cambiar de módulo (#220).
  const applyPollRef = useRef<number | null>(null)
  const stopApplyPoll = () => {
    if (applyPollRef.current !== null) {
      window.clearInterval(applyPollRef.current)
      applyPollRef.current = null
    }
  }
  useEffect(() => stopApplyPoll, [])

  // Auto-seleccionar: gateway por defecto (o el primer agente visible).
  useEffect(() => {
    if (!routerId || !visibleRouters.some((a) => a.slug === routerId)) {
      const first = visibleRouters[0]
      if (first) setRouterId(first.slug)
    }
  }, [agents, gatewayId, allowNonGateway, routerId, visibleRouters])

  // desiredForModule construye el objeto "desired" del módulo activo.
  const desiredForModule = (): Record<string, unknown> => {
    switch (module) {
      case 'guestwifi':
        return { enabled: gwEnabled, ssid: gwSsid, password: gwPassword, band: gwBand, allowNonGateway }
      case 'ddns':
        return { enabled: ddEnabled, serviceName: ddService, domain: ddDomain, username: ddUser, password: ddPass, allowNonGateway }
      case 'sqm':
        return {
          enabled: sqEnabled, interface: sqInterface, download: sqDownload, upload: sqUpload,
          qdisc: sqQdisc, script: sqScript, linklayer: sqLinklayer, overhead: sqOverhead,
          allowNonGateway,
        }
      case 'wireguard':
        return {
          interface: wgInterface,
          peers: wgPeers
            .filter((p) => p.publicKey.trim() !== '')
            .map((p) => ({ name: p.name, publicKey: p.publicKey.trim(), allowedIps: p.allowedIps.trim() })),
          adminPeerPubkey: wgAdminPeer.trim(),
        }
      case 'usteer':
        return {
          enabled: usteerEnabled,
          ssid: usteerSsid,
          mobilityDomain: usteerMobilityDomain,
          aggressiveness: usteerAggressiveness,
          bandSteeringThreshold: usteerBandSteeringThreshold,
          loadBalancingThreshold: usteerLoadBalancingThreshold,
          debugLevel: usteerDebugLevel,
          ieee80211k: usteerK,
          ieee80211r: usteerR,
          ieee80211v: usteerV,
          bssTransition: usteerBssTransition,
          ftOverDs: usteerFtOverDs,
          ftPskGenerateLocal: usteerFtPskGenerateLocal,
        }
      default:
        return { enabled: agEnabled, port: agPort, upstreamDns: agDns, allowNonGateway }
    }
  }

  const updateWgPeer = (i: number, field: 'name' | 'publicKey' | 'allowedIps') => (e: ChangeEvent<HTMLInputElement>) => {
    setWgPeers((prev) => prev.map((p, idx) => (idx === i ? { ...p, [field]: e.target.value } : p)))
  }
  const removeWgPeer = (i: number) => () => setWgPeers((prev) => prev.filter((_, idx) => idx !== i))

  const createPlan = async () => {
    stopApplyPoll()
    setBusy(true)
    setError('')
    setPlan(null)
    setWarnCode('')
    try {
      const res = await fetch('/api/plans', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          routerId,
          resource: module,
          desired: desiredForModule(),
        }),
      })
      if (!res.ok) {
        // Parsear el envelope de error de writeError ({error, message}).
        let code = '', message = ''
        try {
          const body = await res.json()
          code = body.error || body.code || ''
          message = body.message || ''
        } catch {
          message = await res.text().catch(() => res.statusText)
        }
        if (code === 'managed_by_firmware' || code === 'gateway_only') {
          setWarnCode(code)
        } else {
          setError(message || res.statusText)
        }
        return
      }
      setPlan(await res.json())
    } catch (e) {
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }

  const applyPlan = async () => {
    if (!plan) return
    stopApplyPoll()
    setBusy(true)
    setError('')
    try {
      const res = await fetch(`/api/plans/${plan.id}/apply`, { method: 'POST' })
      if (!res.ok) throw new Error(await res.text())
      setPlan({ ...plan, status: 'applying' })
      // Poll hasta que termine. El id vive en el ref para limpiarlo en unmount
      // y al generar un plan nuevo / cambiar de módulo (#220). El cuerpo va en
      // try/catch: un fetch que rechaza no debe quedar como promise rejection
      // sin manejar.
      applyPollRef.current = window.setInterval(async () => {
        try {
          const r = await fetch(`/api/plans/${plan.id}`)
          if (!r.ok) return
          const p: Plan = await r.json()
          setPlan(p)
          if (p.status !== 'applying') {
            stopApplyPoll()
            setBusy(false)
          }
        } catch {
          /* el siguiente tick reintenta; no propaga */
        }
      }, 2000)
    } catch (e) {
      setError(String(e))
      setBusy(false)
    }
  }

  if (auth?.role !== 'admin') {
    return (
      <div className="flex min-h-[50vh] items-center justify-center text-text-secondary">
        {t('common.adminOnly')}
      </div>
    )
  }

  return (
    <div className="space-y-4 md:space-y-5">
      <header>
        <nav className="mb-1 flex items-center gap-1 text-caption text-text-muted">
          <Link to="/" className="transition-colors hover:text-accent">{t('common.home')}</Link>
          <ChevronRight className="h-3 w-3" strokeWidth={1.75} aria-hidden />
          <span className="text-text-secondary">{t('nav.orchestration')}</span>
        </nav>
        <h1 className="font-display text-h1 text-text-primary">{t('orchestration.title')}</h1>
        <p className="mt-0.5 text-sm text-text-secondary">{t('orchestration.subtitle')}</p>
      </header>

      {/* Selector de módulo */}
      <div className="flex flex-wrap gap-2">
        {(['adguard', 'guestwifi', 'ddns', 'sqm', 'wireguard', 'usteer'] as Module[]).map((m) => (
          <button
            key={m}
            type="button"
            onClick={() => { stopApplyPoll(); setModule(m); setPlan(null); setBusy(false); setError(''); setWarnCode('') }}
            className={`rounded-full px-3.5 py-1.5 text-sm font-medium transition-colors ${
              module === m
                ? 'bg-accent text-canvas'
                : 'border border-border bg-surface text-text-secondary hover:text-text-primary'
            }`}
          >
            {t(`orchestration.module.${m}`)}
          </button>
        ))}
      </div>

      <div className="rounded-2xl border border-border bg-surface p-5">
        <div className="mb-4 flex items-center gap-2">
          <Wand2 className="h-4 w-4 text-accent" strokeWidth={1.75} />
          <h2 className="text-sm font-semibold text-text-primary">
            {t(`orchestration.module.${module}`)}
          </h2>
        </div>

        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <label className="flex flex-col gap-1">
            <span className="text-caption font-medium text-text-secondary">{t('orchestration.router')}</span>
            <select
              value={routerId}
              onChange={(e) => setRouterId(e.target.value)}
              className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
            >
              {visibleRouters.map((a) => (
                <option key={a.slug} value={a.slug}>
                  {a.slug}{a.slug === gatewayId ? ` (${t('orchestration.gateway')})` : ''}
                </option>
              ))}
            </select>
          </label>

          {/* Toggle advanced (#120): permitir un router no-gateway */}
          <label className="flex items-center gap-2 pt-6">
            <input
              type="checkbox"
              checked={allowNonGateway}
              onChange={(e) => setAllowNonGateway(e.target.checked)}
              className="h-4 w-4 rounded border-border"
            />
            <span className="text-sm text-text-primary">{t(`orchestration.${module}.allowNonGateway`)}</span>
          </label>

          {nonGatewaySelected && (
            <div
              role="alert"
              className="mt-2 rounded-xl border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-200 lg:col-span-4"
            >
              {t(`orchestration.${module}.nonGatewayWarning`)}
            </div>
          )}

          {module === 'adguard' && (
            <>
              <label className="flex items-center gap-2 pt-6">
                <input
                  type="checkbox"
                  checked={agEnabled}
                  onChange={(e) => setAgEnabled(e.target.checked)}
                  className="h-4 w-4 rounded border-border"
                />
                <span className="text-sm text-text-primary">{t('orchestration.adguard.enable')}</span>
              </label>

              {agEnabled && (
                <>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.adguard.port')}</span>
                    <input
                      type="text"
                      value={agPort}
                      onChange={(e) => setAgPort(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.adguard.upstream')}</span>
                    <input
                      type="text"
                      value={agDns}
                      onChange={(e) => setAgDns(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                </>
              )}
            </>
          )}

          {module === 'guestwifi' && (
            <>
              <label className="flex items-center gap-2 pt-6">
                <input
                  type="checkbox"
                  checked={gwEnabled}
                  onChange={(e) => setGwEnabled(e.target.checked)}
                  className="h-4 w-4 rounded border-border"
                />
                <span className="text-sm text-text-primary">{t('orchestration.guestwifi.enable')}</span>
              </label>

              {gwEnabled && (
                <>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.guestwifi.ssid')}</span>
                    <input
                      type="text"
                      value={gwSsid}
                      onChange={(e) => setGwSsid(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.guestwifi.password')}</span>
                    <input
                      type="text"
                      value={gwPassword}
                      onChange={(e) => setGwPassword(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.guestwifi.band')}</span>
                    <select
                      value={gwBand}
                      onChange={(e) => setGwBand(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    >
                      <option value="2g">{t('orchestration.guestwifi.band2g')}</option>
                      <option value="5g">{t('orchestration.guestwifi.band5g')}</option>
                      <option value="auto">{t('orchestration.guestwifi.bandAuto')}</option>
                    </select>
                  </label>
                </>
              )}
            </>
          )}

          {module === 'ddns' && (
            <>
              <label className="flex items-center gap-2 pt-6">
                <input
                  type="checkbox"
                  checked={ddEnabled}
                  onChange={(e) => setDdEnabled(e.target.checked)}
                  className="h-4 w-4 rounded border-border"
                />
                <span className="text-sm text-text-primary">{t('orchestration.ddns.enable')}</span>
              </label>

              {ddEnabled && (
                <>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.ddns.service')}</span>
                    <input
                      type="text"
                      value={ddService}
                      onChange={(e) => setDdService(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.ddns.domain')}</span>
                    <input
                      type="text"
                      value={ddDomain}
                      onChange={(e) => setDdDomain(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.ddns.username')}</span>
                    <input
                      type="text"
                      value={ddUser}
                      onChange={(e) => setDdUser(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.ddns.password')}</span>
                    <input
                      type="password"
                      value={ddPass}
                      onChange={(e) => setDdPass(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                </>
              )}
            </>
          )}

          {module === 'sqm' && (
            <>
              <label className="flex items-center gap-2 pt-6">
                <input
                  type="checkbox"
                  checked={sqEnabled}
                  onChange={(e) => setSqEnabled(e.target.checked)}
                  className="h-4 w-4 rounded border-border"
                />
                <span className="text-sm text-text-primary">{t('orchestration.sqm.enable')}</span>
              </label>

              {sqEnabled && (
                <>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.sqm.interface')}</span>
                    <input
                      type="text"
                      value={sqInterface}
                      onChange={(e) => setSqInterface(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.sqm.download')}</span>
                    <input
                      type="text"
                      value={sqDownload}
                      onChange={(e) => setSqDownload(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.sqm.upload')}</span>
                    <input
                      type="text"
                      value={sqUpload}
                      onChange={(e) => setSqUpload(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.sqm.qdisc')}</span>
                    <select
                      value={sqQdisc}
                      onChange={(e) => setSqQdisc(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    >
                      <option value="cake">{t('orchestration.sqm.qdiscCake')}</option>
                      <option value="fq_codel">{t('orchestration.sqm.qdiscFqCodel')}</option>
                    </select>
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.sqm.script')}</span>
                    <input
                      type="text"
                      value={sqScript}
                      onChange={(e) => setSqScript(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.sqm.linklayer')}</span>
                    <select
                      value={sqLinklayer}
                      onChange={(e) => setSqLinklayer(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    >
                      <option value="ethernet">{t('orchestration.sqm.linkEthernet')}</option>
                      <option value="none">{t('orchestration.sqm.linkNone')}</option>
                      <option value="atm">{t('orchestration.sqm.linkAtm')}</option>
                    </select>
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.sqm.overhead')}</span>
                    <input
                      type="text"
                      value={sqOverhead}
                      onChange={(e) => setSqOverhead(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                </>
              )}
            </>
          )}

          {module === 'wireguard' && (
            <>
              <label className="flex flex-col gap-1">
                <span className="text-caption font-medium text-text-secondary">{t('orchestration.wireguard.interface')}</span>
                <input
                  type="text"
                  value={wgInterface}
                  onChange={(e) => setWgInterface(e.target.value)}
                  className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                />
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-caption font-medium text-text-secondary">{t('orchestration.wireguard.adminPeer')}</span>
                <input
                  type="text"
                  value={wgAdminPeer}
                  onChange={(e) => setWgAdminPeer(e.target.value)}
                  placeholder={t('orchestration.wireguard.adminPeerPlaceholder')}
                  className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                />
              </label>

              <div className="lg:col-span-4">
                <div className="flex items-center justify-between">
                  <span className="text-caption font-medium text-text-secondary">{t('orchestration.wireguard.peers')}</span>
                  <button
                    type="button"
                    onClick={() => setWgPeers((prev) => [...prev, { name: '', publicKey: '', allowedIps: '' }])}
                    className="inline-flex items-center gap-1 rounded-md bg-accent-soft px-2.5 py-1 text-xs font-medium text-accent transition-colors hover:bg-accent-soft/70"
                  >
                    <Plus className="h-3.5 w-3.5" strokeWidth={1.75} />
                    {t('orchestration.wireguard.addPeer')}
                  </button>
                </div>
                {wgPeers.map((p, i) => (
                  <div key={i} className="mt-2 grid items-end gap-2 sm:grid-cols-[1fr_2fr_1.5fr_auto]">
                    <label className="flex flex-col gap-1">
                      <span className="text-caption text-text-muted">{t('orchestration.wireguard.peerName')}</span>
                      <input
                        type="text"
                        value={p.name}
                        onChange={updateWgPeer(i, 'name')}
                        className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                      />
                    </label>
                    <label className="flex flex-col gap-1">
                      <span className="text-caption text-text-muted">{t('orchestration.wireguard.peerPubkey')}</span>
                      <input
                        type="text"
                        value={p.publicKey}
                        onChange={updateWgPeer(i, 'publicKey')}
                        className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                      />
                    </label>
                    <label className="flex flex-col gap-1">
                      <span className="text-caption text-text-muted">{t('orchestration.wireguard.peerAllowedIps')}</span>
                      <input
                        type="text"
                        value={p.allowedIps}
                        onChange={updateWgPeer(i, 'allowedIps')}
                        placeholder="10.0.0.2/32,10.0.1.0/24"
                        className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                      />
                    </label>
                    <button
                      type="button"
                      onClick={removeWgPeer(i)}
                      aria-label={t('orchestration.wireguard.removePeer')}
                      className="inline-flex items-center justify-center rounded-lg border border-border p-2 text-text-secondary transition-colors hover:text-rose-500"
                    >
                      <X className="h-4 w-4" strokeWidth={1.75} />
                    </button>
                  </div>
                ))}
                <p className="mt-2 text-xs text-text-muted">{t('orchestration.wireguard.peersHint')}</p>
              </div>
            </>
          )}

          {module === 'usteer' && (
            <>
              <label className="flex items-center gap-2 pt-6">
                <input
                  type="checkbox"
                  checked={usteerEnabled}
                  onChange={(e) => setUsteerEnabled(e.target.checked)}
                  className="h-4 w-4 rounded border-border"
                />
                <span className="text-sm text-text-primary">{t('orchestration.usteer.enable')}</span>
              </label>

              {usteerEnabled && (
                <>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.usteer.ssid')}</span>
                    <input
                      type="text"
                      value={usteerSsid}
                      onChange={(e) => setUsteerSsid(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.usteer.mobilityDomain')}</span>
                    <input
                      type="text"
                      value={usteerMobilityDomain}
                      onChange={(e) => setUsteerMobilityDomain(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.usteer.aggressiveness')}</span>
                    <input
                      type="text"
                      value={usteerAggressiveness}
                      onChange={(e) => setUsteerAggressiveness(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.usteer.bandSteeringThreshold')}</span>
                    <input
                      type="text"
                      value={usteerBandSteeringThreshold}
                      onChange={(e) => setUsteerBandSteeringThreshold(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.usteer.loadBalancingThreshold')}</span>
                    <input
                      type="text"
                      value={usteerLoadBalancingThreshold}
                      onChange={(e) => setUsteerLoadBalancingThreshold(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.usteer.debugLevel')}</span>
                    <input
                      type="text"
                      value={usteerDebugLevel}
                      onChange={(e) => setUsteerDebugLevel(e.target.value)}
                      className="rounded-lg border border-border bg-canvas px-3 py-2 text-sm text-text-primary"
                    />
                  </label>

                  <div className="lg:col-span-2">
                    <span className="text-caption font-medium text-text-secondary">{t('orchestration.usteer.roaming')}</span>
                    <div className="mt-2 flex flex-wrap gap-4">
                      <label className="flex items-center gap-2">
                        <input type="checkbox" checked={usteerK} onChange={(e) => setUsteerK(e.target.checked)} className="h-4 w-4 rounded border-border" />
                        <span className="text-sm text-text-primary">{t('orchestration.usteer.ieee80211k')}</span>
                      </label>
                      <label className="flex items-center gap-2">
                        <input type="checkbox" checked={usteerR} onChange={(e) => setUsteerR(e.target.checked)} className="h-4 w-4 rounded border-border" />
                        <span className="text-sm text-text-primary">{t('orchestration.usteer.ieee80211r')}</span>
                      </label>
                      <label className="flex items-center gap-2">
                        <input type="checkbox" checked={usteerV} onChange={(e) => setUsteerV(e.target.checked)} className="h-4 w-4 rounded border-border" />
                        <span className="text-sm text-text-primary">{t('orchestration.usteer.ieee80211v')}</span>
                      </label>
                      <label className="flex items-center gap-2">
                        <input type="checkbox" checked={usteerBssTransition} onChange={(e) => setUsteerBssTransition(e.target.checked)} className="h-4 w-4 rounded border-border" />
                        <span className="text-sm text-text-primary">{t('orchestration.usteer.bssTransition')}</span>
                      </label>
                      <label className="flex items-center gap-2">
                        <input type="checkbox" checked={usteerFtPskGenerateLocal} onChange={(e) => setUsteerFtPskGenerateLocal(e.target.checked)} className="h-4 w-4 rounded border-border" />
                        <span className="text-sm text-text-primary">{t('orchestration.usteer.ftPskGenerateLocal')}</span>
                      </label>
                      <label className="flex items-center gap-2">
                        <input type="checkbox" checked={usteerFtOverDs} onChange={(e) => setUsteerFtOverDs(e.target.checked)} className="h-4 w-4 rounded border-border" />
                        <span className="text-sm text-text-primary">{t('orchestration.usteer.ftOverDs')}</span>
                      </label>
                    </div>
                  </div>
                </>
              )}
            </>
          )}
        </div>

        <div className="mt-4 flex gap-2">
          <button
            type="button"
            onClick={() => void createPlan()}
            disabled={busy || !routerId}
            className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-60"
          >
            <Wand2 className="h-4 w-4" strokeWidth={1.75} />
            {t('orchestration.generatePlan')}
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded-xl border border-rose-500/40 bg-rose-500/10 px-4 py-3 text-sm text-rose-600 dark:text-rose-400">
          {error}
        </div>
      )}

      {warnCode && (
        <div className="flex items-start gap-3 rounded-xl border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm text-amber-700 dark:text-amber-400">
          <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0" strokeWidth={1.75} aria-hidden />
          <span>{t(`orchestration.warn.${warnCode}`)}</span>
        </div>
      )}

      {plan && (
        <motion.div
          initial={{ opacity: 0, y: 8 }}
          animate={{ opacity: 1, y: 0 }}
          className="rounded-2xl border border-border bg-surface p-5"
        >
          <div className="mb-3 flex items-center justify-between">
            <h3 className="text-sm font-semibold text-text-primary">
              {t('orchestration.plan')} · {plan.id.slice(0, 8)}
            </h3>
            <div className="flex items-center gap-2">
              {plan.method && methodLabel(module, plan.method) && (
                <span className="rounded-full bg-accent-soft px-2.5 py-0.5 text-xs font-medium text-accent">
                  {t('orchestration.methodLabel')}: {t(methodLabel(module, plan.method)!)}
                </span>
              )}
              <PlanStatusBadge status={plan.status} />
            </div>
          </div>

          <div className="space-y-1">
            {plan.diff.map((op, i) => (
              <div key={i} className="flex items-center gap-2 rounded-lg bg-canvas/60 px-3 py-2">
                <span className="font-mono text-xs text-text-muted">{op.kind}</span>
                <span className="text-sm text-text-primary">{op.desc}</span>
              </div>
            ))}
          </div>

          {plan.status === 'pending' && (
            <button
              type="button"
              onClick={() => void applyPlan()}
              disabled={busy}
              className="mt-4 inline-flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-60"
            >
              {t('orchestration.apply')}
            </button>
          )}

          {plan.status === 'applying' && (
            <div className="mt-4 flex items-center gap-2 text-sm text-text-secondary">
              <Loader2 className="h-4 w-4 animate-spin" strokeWidth={1.75} />
              {t('orchestration.applying')}
            </div>
          )}

          {plan.result && plan.status !== 'applying' && plan.status !== 'pending' && (
            <div className={`mt-4 flex items-center gap-2 rounded-lg px-3 py-2 text-sm ${
              plan.status === 'applied'
                ? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
                : 'bg-rose-500/10 text-rose-600 dark:text-rose-400'
            }`}>
              {plan.status === 'applied'
                ? <CheckCircle2 className="h-4 w-4" strokeWidth={1.75} />
                : <AlertCircle className="h-4 w-4" strokeWidth={1.75} />}
              {plan.status === 'applied'
                ? t('orchestration.applied')
                : `${t('orchestration.failed')}: ${plan.result.error || plan.status}`}
              {plan.result.duration_ms ? ` (${plan.result.duration_ms}ms)` : ''}
            </div>
          )}
        </motion.div>
      )}
    </div>
  )
}

function PlanStatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    pending: 'bg-surface-2 text-text-secondary',
    applying: 'bg-accent-soft text-accent',
    applied: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
    failed: 'bg-rose-500/10 text-rose-600 dark:text-rose-400',
    rolled_back: 'bg-amber-500/10 text-amber-600 dark:text-amber-400',
  }
  return (
    <span className={`rounded-full px-2.5 py-0.5 text-xs font-medium ${colors[status] || colors.pending}`}>
      {status}
    </span>
  )
}
