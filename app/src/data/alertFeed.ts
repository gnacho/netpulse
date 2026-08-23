/**
 * NetPulse — Historial extendido de alertas/actividad para `/alerts` (alerts.md §④).
 * Extiende el mock canónico de `@/data/mock` (los 5 eventos canónicos se toman
 * directamente de `alerts` y aquí solo se les añade día, tipo y contexto visual).
 */

import {
  DownloadCloud,
  KeyRound,
  PartyPopper,
  ShieldCheck,
  TrendingUp,
  Wifi,
  Activity,
  Server,
  RefreshCw,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { alerts as mockAlerts, getRouter, wireguard as mockWireguard } from '@/data/mock'
import type { AlertEvent, WireGuardStats } from '@/data/mock'

// ---------------------------------------------------------------------------
// Tipos
// ---------------------------------------------------------------------------

export type AlertKind = 'router' | 'dispositivos' | 'wireguard' | 'adguard' | 'sistema'

export type FeedDay = 'hoy' | 'ayer' | '12nov'

export const DAY_LABELS: Record<FeedDay, string> = {
  hoy: 'Hoy',
  ayer: 'Ayer',
  '12nov': '12 nov',
}

export const DAY_ORDER: FeedDay[] = ['hoy', 'ayer', '12nov']

export const KIND_LABELS: Record<AlertKind, string> = {
  router: 'Router',
  dispositivos: 'Dispositivos',
  wireguard: 'WireGuard',
  adguard: 'AdGuard',
  sistema: 'Sistema',
}

export interface FeedSpark {
  data: number[]
  color: string
  label: string
  unit: string
  current: string
  /** Línea de umbral punteada (misma escala que data) */
  threshold?: number
  thresholdLabel?: string
}

export interface FeedContext {
  spark?: FeedSpark
  /** Detalle de túnel WireGuard (eventos WG) */
  wg?: { peer: string; peerType: 'movil' | 'portatil' | 'tablet'; endpoint: string; tunnelIp: string; handshake: string }
  /** Detalle de dispositivo (altas de clientes) */
  device?: { name: string; ip: string; mac: string; band: string; signal: string }
  /** Pares label/valor mono extra */
  facts?: { label: string; value: string }[]
  /** Nota cálida bajo el contexto */
  note?: string
}

export interface FeedEvent extends AlertEvent {
  day: FeedDay
  kind: AlertKind
  /** Icono lucide personalizado (por defecto se usa el de severidad) */
  icon?: LucideIcon
  context?: FeedContext
}

// ---------------------------------------------------------------------------
// Eventos canónicos (desde @/data/mock, como fallback de presentación)
// ---------------------------------------------------------------------------

const fallbackCanon = Object.fromEntries(mockAlerts.map((a) => [a.id, a]))

/**
 * Construye el feed completo (18 eventos, alerts.md §④) a partir de las
 * alertas y stats WireGuard actuales (provider). Los 5 eventos canónicos se
 * toman de `canonAlerts` si existen; si no, del mock de presentación.
 */
export function buildAlertFeed(
  canonAlerts: AlertEvent[],
  wg: WireGuardStats,
  modelShort: (routerId: string) => string | undefined = (id) => getRouter(id)?.modelShort,
): FeedEvent[] {
  const byId = Object.fromEntries(canonAlerts.map((a) => [a.id, a]))
  // El proxy SIEMPRE devuelve un AlertEvent (fallback a fallbackCanon); los
  // accesos llevan `!` por noUncheckedIndexedAccess.
  const canon = new Proxy(byId as Record<string, AlertEvent>, {
    get: (target, id: string) => target[id] ?? fallbackCanon[id],
  })
  // Ts de los eventos de presentación (no canónicos): escalonados en
  // coherencia con su string "hace X" (SPEC-ALERTAS §5)
  const now = Math.floor(Date.now() / 1000)
  const ago = (s: number) => now - s

  return [
  // ————— Hoy —————
  {
    ...canon['alert-temp-patio']!,
    description: '71 °C, por encima del umbral de 65 °C — revisa la ventilación',
    day: 'hoy',
    kind: 'router',
    context: {
      spark: {
        data: [58, 59, 61, 62, 64, 66, 68, 70, 72, 73, 72, 71],
        color: '#FBBF24',
        label: 'Temperatura · últimas 6 h',
        unit: '°C',
        current: '71 °C',
        threshold: 65,
        thresholdLabel: 'Umbral 65 °C',
      },
      note: 'El Patio lleva 4 días encendido; la subida empezó al mediodía.',
    },
  },
  {
    ...canon['alert-handshake-wg']!,
    day: 'hoy',
    kind: 'wireguard',
    icon: KeyRound,
    context: {
      wg: {
        peer: 'Pixel 8 Pro',
        peerType: 'movil',
        endpoint: '5.224.x.x',
        tunnelIp: '10.0.0.2',
        handshake: 'hace 38 s',
      },
    },
  },
  {
    ...canon['alert-nuevo-tab']!,
    day: 'hoy',
    kind: 'dispositivos',
    context: {
      device: {
        name: 'Galaxy Tab S9',
        ip: '192.168.8.48',
        mac: 'D6:91:2F:07:B3:55',
        band: '5 GHz',
        signal: '−50 dBm',
      },
    },
  },
  {
    id: 'evt-pico-trafico',
    category: 'system',
    urgent: false,
    severity: 'info',
    title: 'Pico de tráfico',
    description: '412 Mbps de bajada — streaming 4K en TV Samsung',
    time: 'hace 1 h',
    ts: ago(3600),
    read: true,
    routerId: 'flint2',
    day: 'hoy',
    kind: 'sistema',
    icon: TrendingUp,
    context: {
      spark: {
        data: [180, 224, 260, 298, 340, 380, 412, 396, 356, 320, 284, 240],
        color: '#22D3EE',
        label: 'Bajada WAN · últimas 6 h',
        unit: 'Mbps',
        current: '412 Mbps',
      },
      note: 'Coincide con el backup del NAS por subida (96 Mbps a las 21:14).',
    },
  },
  {
    id: 'evt-backup-nas',
    category: 'system',
    urgent: false,
    severity: 'info',
    title: 'Backup del NAS completado',
    description: 'Copia nocturna adelantada — pico de subida de 96 Mbps',
    time: 'hace 1 h',
    ts: ago(3600),
    read: true,
    routerId: 'flint2',
    day: 'hoy',
    kind: 'sistema',
    icon: Server,
    context: {
      facts: [
        { label: 'Destino', value: 'NAS Synology · 192.168.8.10' },
        { label: 'Subida máx.', value: '96 Mbps' },
      ],
    },
  },
  {
    ...canon['alert-firmware-estudio']!,
    day: 'hoy',
    kind: 'router',
    icon: DownloadCloud,
    context: {
      facts: [
        { label: 'Versión actual', value: 'OpenWrt 24.10.0' },
        { label: 'Disponible', value: 'OpenWrt 24.10.1' },
      ],
      note: 'Actualización menor de seguridad. NetPulse es de solo lectura: aplícala desde LuCI.',
    },
  },
  {
    id: 'evt-dns-robot',
    category: 'system',
    urgent: false,
    severity: 'info',
    title: 'Consultas DNS inusuales',
    description: "AdGuard: +340 % desde 'Robot aspirador' (reintentos)",
    time: 'hace 5 h',
    ts: ago(5 * 3600),
    read: true,
    routerId: 'patio',
    day: 'hoy',
    kind: 'adguard',
    context: {
      spark: {
        data: [38, 40, 42, 60, 120, 240, 380, 410, 392, 300, 180, 90],
        color: '#60A5FA',
        label: 'Consultas/min del Robot aspirador',
        unit: '/min',
        current: '90/min',
      },
      note: 'Suele deberse a reintentos por señal débil en el Patio (−67 dBm).',
    },
  },
  {
    id: 'evt-wg-trabajo',
    category: 'vpn',
    urgent: false,
    severity: 'info',
    title: 'Túnel WireGuard cerrado',
    description: 'Portátil trabajo se desconectó tras 6 h de sesión',
    time: 'hace 6 h',
    ts: ago(6 * 3600),
    read: true,
    routerId: 'flint2',
    day: 'hoy',
    kind: 'wireguard',
    icon: KeyRound,
    context: {
      wg: {
        peer: 'Portátil trabajo',
        peerType: 'portatil',
        endpoint: '83.44.x.x',
        tunnelIp: '10.0.0.5',
        handshake: 'hace 6 h',
      },
    },
  },

  // ————— Ayer —————
  {
    ...canon['alert-backup-adguard']!,
    title: 'Copia de seguridad de AdGuard completada',
    day: 'ayer',
    kind: 'adguard',
    icon: DownloadCloud,
    context: {
      facts: [
        { label: 'Tamaño', value: '2,1 MB' },
        { label: 'Destino', value: 'NAS Synology · 192.168.8.10' },
      ],
    },
  },
  {
    id: 'evt-macbook-vpn',
    category: 'vpn',
    urgent: false,
    severity: 'info',
    title: 'MacBook Air conectado por VPN',
    description: 'Handshake desde 79.153.x.x · túnel 10.0.0.3',
    time: 'hace 1 día',
    ts: ago(24 * 3600),
    read: true,
    routerId: 'flint2',
    day: 'ayer',
    kind: 'wireguard',
    icon: KeyRound,
    context: {
      wg: {
        peer: 'MacBook Air',
        peerType: 'portatil',
        endpoint: '79.153.x.x',
        tunnelIp: '10.0.0.3',
        handshake: 'hace 1 día',
      },
    },
  },
  {
    id: 'evt-canal-patio',
    category: 'signal',
    urgent: false,
    severity: 'info',
    title: 'Canal 2.4 GHz cambiado',
    description: 'Patio: canal 6 → 11 (selección automática)',
    time: 'hace 1 día',
    ts: ago(24 * 3600),
    read: true,
    routerId: 'patio',
    day: 'ayer',
    kind: 'router',
    icon: Wifi,
    context: {
      facts: [
        { label: 'Motivo', value: 'Interferencia detectada en canal 6' },
        { label: 'Modo', value: 'Selección automática' },
      ],
    },
  },
  {
    id: 'evt-perdida-ok',
    category: 'internet',
    urgent: false,
    severity: 'ok',
    title: 'Pérdida de paquetes resuelta',
    description: 'La WAN vuelve a estar estable (0 % de pérdida)',
    time: 'hace 1 día',
    ts: ago(25 * 3600),
    read: true,
    routerId: 'flint2',
    day: 'ayer',
    kind: 'sistema',
    icon: Activity,
    context: {
      facts: [
        { label: 'Pérdida actual', value: '0 %' },
        { label: 'Latencia WAN', value: '8 ms' },
      ],
      note: 'Duró ~4 min por un microcorte del ISP (Digi).',
    },
  },
  {
    id: 'evt-nest-hub',
    category: 'clients',
    urgent: false,
    severity: 'info',
    title: 'Nuevo dispositivo',
    description: "'Nest Hub' se ha unido a Estudio",
    time: 'hace 1 día',
    ts: ago(26 * 3600),
    read: true,
    routerId: 'estudio',
    day: 'ayer',
    kind: 'dispositivos',
    context: {
      device: {
        name: 'Nest Hub',
        ip: '192.168.8.53',
        mac: '7A:11:C9:4E:02:8D',
        band: '2.4 GHz',
        signal: '−58 dBm',
      },
    },
  },
  {
    id: 'evt-adguard-listas',
    category: 'system',
    urgent: false,
    severity: 'info',
    title: 'Listas de AdGuard actualizadas',
    description: '218 442 reglas activas en 6 listas de filtros',
    time: 'hace 1 día',
    ts: ago(27 * 3600),
    read: true,
    routerId: 'flint2',
    day: 'ayer',
    kind: 'adguard',
    icon: ShieldCheck,
  },

  // ————— 12 nov (arranque del historial, 32 días) —————
  {
    id: 'evt-reinicio-gateway',
    category: 'router',
    urgent: false,
    severity: 'info',
    title: 'Reinicio programado del Gateway',
    description: 'Mantenimiento a las 03:12 — el uptime actual arranca aquí',
    time: 'hace 32 días',
    ts: ago(32 * 24 * 3600),
    read: true,
    routerId: 'flint2',
    day: '12nov',
    kind: 'router',
    icon: RefreshCw,
    context: {
      facts: [
        { label: 'Equipo', value: `${modelShort('flint2') ?? 'Flint 2'} · 192.168.8.1` },
        { label: 'Uptime desde entonces', value: '32d 14h' },
      ],
    },
  },
  {
    id: 'evt-wg-setup',
    category: 'vpn',
    urgent: false,
    severity: 'info',
    title: 'WireGuard configurado',
    description: `Servidor ${wg.interface} activo con ${wg.peers.length} peers (${wg.subnet})`,
    time: 'hace 32 días',
    ts: ago(32 * 24 * 3600),
    read: true,
    routerId: 'flint2',
    day: '12nov',
    kind: 'wireguard',
    icon: KeyRound,
  },
  {
    id: 'evt-adguard-on',
    category: 'system',
    urgent: false,
    severity: 'info',
    title: 'AdGuard Home activado',
    description: 'DNS de la red apuntando a 192.168.8.1:3000',
    time: 'hace 32 días',
    ts: ago(32 * 24 * 3600),
    read: true,
    routerId: 'flint2',
    day: '12nov',
    kind: 'adguard',
    icon: ShieldCheck,
  },
  {
    id: 'evt-netpulse-start',
    category: 'system',
    urgent: false,
    severity: 'info',
    title: 'NetPulse empezó a monitorizar',
    description: 'Primer evento del historial — bienvenido a bordo',
    time: 'hace 32 días',
    ts: ago(32 * 24 * 3600),
    read: true,
    day: '12nov',
    kind: 'sistema',
    icon: PartyPopper,
  },
  ]
}

/** Feed estático del mockup (compat); las páginas usan `buildAlertFeed` con el provider. */
export const alertFeed: FeedEvent[] = buildAlertFeed(mockAlerts, mockWireguard)

// ---------------------------------------------------------------------------
// Feed LIVE: solo alertas reales del backend — NADA de datos del mockup
// ---------------------------------------------------------------------------

const LIVE_KIND: [RegExp, AlertKind][] = [
  [/^alert-(offline|temp)/, 'router'],
  [/^alert-wg/, 'wireguard'],
  [/^alert-adguard/, 'adguard'],
  [/^alert-(nuevo|device)/, 'dispositivos'],
]

/** Respaldo por categoría (SPEC-ALERTAS §1) cuando el id no casa con LIVE_KIND. */
const KIND_BY_CATEGORY: Record<AlertEvent['category'], AlertKind> = {
  router: 'router',
  internet: 'router',
  clients: 'dispositivos',
  signal: 'router',
  vpn: 'wireguard',
  system: 'sistema',
}

/** Mapea las alertas reales a FeedEvent sin contexto inventado. */
export function buildLiveFeed(canonAlerts: AlertEvent[]): FeedEvent[] {
  return canonAlerts.map((a) => ({
    ...a,
    day: 'hoy' as FeedDay,
    kind:
      LIVE_KIND.find(([re]) => re.test(a.id))?.[1] ??
      KIND_BY_CATEGORY[a.category] ??
      (a.routerId ? 'router' : 'sistema'),
  }))
}

export const feedRouterName = (id?: string) => (id ? (getRouter(id)?.name ?? id) : undefined)
