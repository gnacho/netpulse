/**
 * NetPulse - Historial extendido de alertas/actividad para `/alerts` (alerts.md §④).
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
import i18n, { fmtUptime } from '@/i18n'
import { alerts as mockAlerts, getRouter, wireguard as mockWireguard } from '@/data/mock'
import type { AlertEvent, WireGuardStats } from '@/data/mock'

// ---------------------------------------------------------------------------
// Tipos
// ---------------------------------------------------------------------------

export type AlertKind = 'router' | 'dispositivos' | 'wireguard' | 'adguard' | 'sistema'

export type FeedDay = 'hoy' | 'ayer' | '12nov'

export const DAY_ORDER: FeedDay[] = ['hoy', 'ayer', '12nov']

export const ALERT_KINDS: readonly AlertKind[] = ['router', 'dispositivos', 'wireguard', 'adguard', 'sistema']

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
// Helpers bilingües demo (mismo patrón que @/data/mock.ts, issue #239 / #378)
// ---------------------------------------------------------------------------

/** Texto bilingüe de alerta demo: es/en según el idioma activo. */
function demoText({ es, en }: { es: string; en: string }): string {
  return i18n.language?.startsWith('es') ? es : en
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
    // ----- Hoy -----
    {
      ...canon['alert-temp-patio']!,
      get description() {
        return demoText({
          es: '71 °C, por encima del umbral de 65 °C - revisa la ventilación',
          en: '71 °C, above the 65 °C threshold - check ventilation',
        })
      },
      day: 'hoy',
      kind: 'router',
      context: {
        spark: {
          data: [58, 59, 61, 62, 64, 66, 68, 70, 72, 73, 72, 71],
          color: '#FBBF24',
          get label() {
            return demoText({ es: 'Temperatura · últimas 6 h', en: 'Temperature · last 6 h' })
          },
          unit: '°C',
          current: '71 °C',
          threshold: 65,
          get thresholdLabel() {
            return demoText({ es: 'Umbral 65 °C', en: 'Threshold 65 °C' })
          },
        },
        get note() {
          return demoText({
            es: 'El Patio lleva 4 días encendido; la subida empezó al mediodía.',
            en: 'Patio has been on for 4 days; the rise started at noon.',
          })
        },
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
          handshake: '38 s ago',
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
      get title() {
        return demoText({ es: 'Pico de tráfico', en: 'Traffic peak' })
      },
      get description() {
        return demoText({
          es: '412 Mbps de bajada - streaming 4K en TV Samsung',
          en: '412 Mbps download - 4K streaming on Samsung TV',
        })
      },
      time: '1 h ago',
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
          get label() {
            return demoText({ es: 'Bajada WAN · últimas 6 h', en: 'WAN download · last 6 h' })
          },
          unit: 'Mbps',
          current: '412 Mbps',
        },
        get note() {
          return demoText({
            es: 'Coincide con el backup del NAS por subida (96 Mbps a las 21:14).',
            en: 'Matches the NAS upload backup (96 Mbps at 21:14).',
          })
        },
      },
    },
    {
      id: 'evt-backup-nas',
      category: 'system',
      urgent: false,
      severity: 'info',
      get title() {
        return demoText({ es: 'Backup del NAS completado', en: 'NAS backup completed' })
      },
      get description() {
        return demoText({
          es: 'Copia nocturna adelantada - pico de subida de 96 Mbps',
          en: 'Overnight copy brought forward - upload peak of 96 Mbps',
        })
      },
      time: '1 h ago',
      ts: ago(3600),
      read: true,
      routerId: 'flint2',
      day: 'hoy',
      kind: 'sistema',
      icon: Server,
      context: {
        facts: [
          {
            get label() {
              return demoText({ es: 'Destino', en: 'Destination' })
            },
            value: 'NAS Synology · 192.168.8.10',
          },
          {
            get label() {
              return demoText({ es: 'Subida máx.', en: 'Max upload' })
            },
            value: '96 Mbps',
          },
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
          {
            get label() {
              return demoText({ es: 'Versión actual', en: 'Current version' })
            },
            value: 'OpenWrt 24.10.0',
          },
          {
            get label() {
              return demoText({ es: 'Disponible', en: 'Available' })
            },
            value: 'OpenWrt 24.10.1',
          },
        ],
        get note() {
          return demoText({
            es: 'Actualización menor de seguridad. NetPulse es de solo lectura: aplícala desde LuCI.',
            en: 'Minor security update. NetPulse is read-only: apply it from LuCI.',
          })
        },
      },
    },
    {
      id: 'evt-dns-robot',
      category: 'system',
      urgent: false,
      severity: 'info',
      get title() {
        return demoText({ es: 'Consultas DNS inusuales', en: 'Unusual DNS queries' })
      },
      get description() {
        return demoText({
          es: "AdGuard: +340 % desde 'Robot aspirador' (reintentos)",
          en: "AdGuard: +340 % from 'Robot vacuum' (retries)",
        })
      },
      time: '5 h ago',
      ts: ago(5 * 3600),
      read: true,
      routerId: 'patio',
      day: 'hoy',
      kind: 'adguard',
      context: {
        spark: {
          data: [38, 40, 42, 60, 120, 240, 380, 410, 392, 300, 180, 90],
          color: '#60A5FA',
          get label() {
            return demoText({
              es: 'Consultas/min del Robot aspirador',
              en: 'Robot vacuum queries/min',
            })
          },
          unit: '/min',
          current: '90/min',
        },
        get note() {
          return demoText({
            es: 'Suele deberse a reintentos por señal débil en el Patio (−67 dBm).',
            en: 'Usually due to retries from weak signal on Patio (−67 dBm).',
          })
        },
      },
    },
    {
      id: 'evt-wg-trabajo',
      category: 'vpn',
      urgent: false,
      severity: 'info',
      get title() {
        return demoText({ es: 'Túnel WireGuard cerrado', en: 'WireGuard tunnel closed' })
      },
      get description() {
        return demoText({
          es: 'Portátil trabajo se desconectó tras 6 h de sesión',
          en: 'Work laptop disconnected after 6 h session',
        })
      },
      time: '6 h ago',
      ts: ago(6 * 3600),
      read: true,
      routerId: 'flint2',
      day: 'hoy',
      kind: 'wireguard',
      icon: KeyRound,
      context: {
        wg: {
          get peer() {
            return demoText({ es: 'Portátil trabajo', en: 'Work laptop' })
          },
          peerType: 'portatil',
          endpoint: '83.44.x.x',
          tunnelIp: '10.0.0.5',
          handshake: '6 h ago',
        },
      },
    },

    // ----- Ayer -----
    {
      ...canon['alert-backup-adguard']!,
      get title() {
        return demoText({
          es: 'Copia de seguridad de AdGuard completada',
          en: 'AdGuard backup completed',
        })
      },
      day: 'ayer',
      kind: 'adguard',
      icon: DownloadCloud,
      context: {
        facts: [
          {
            get label() {
              return demoText({ es: 'Tamaño', en: 'Size' })
            },
            get value() {
              return demoText({ es: '2,1 MB', en: '2.1 MB' })
            },
          },
          {
            get label() {
              return demoText({ es: 'Destino', en: 'Destination' })
            },
            value: 'NAS Synology · 192.168.8.10',
          },
        ],
      },
    },
    {
      id: 'evt-macbook-vpn',
      category: 'vpn',
      urgent: false,
      severity: 'info',
      get title() {
        return demoText({ es: 'MacBook Air conectado por VPN', en: 'MacBook Air connected via VPN' })
      },
      get description() {
        return demoText({
          es: 'Handshake desde 79.153.x.x · túnel 10.0.0.3',
          en: 'Handshake from 79.153.x.x · tunnel 10.0.0.3',
        })
      },
      time: '1 day ago',
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
          handshake: '1 day ago',
        },
      },
    },
    {
      id: 'evt-canal-patio',
      category: 'signal',
      urgent: false,
      severity: 'info',
      get title() {
        return demoText({ es: 'Canal 2.4 GHz cambiado', en: '2.4 GHz channel changed' })
      },
      get description() {
        return demoText({
          es: 'Patio: canal 6 → 11 (selección automática)',
          en: 'Patio: channel 6 → 11 (auto selection)',
        })
      },
      time: '1 day ago',
      ts: ago(24 * 3600),
      read: true,
      routerId: 'patio',
      day: 'ayer',
      kind: 'router',
      icon: Wifi,
      context: {
        facts: [
          {
            get label() {
              return demoText({ es: 'Motivo', en: 'Reason' })
            },
            get value() {
              return demoText({
                es: 'Interferencia detectada en canal 6',
                en: 'Interference detected on channel 6',
              })
            },
          },
          {
            get label() {
              return demoText({ es: 'Modo', en: 'Mode' })
            },
            get value() {
              return demoText({ es: 'Selección automática', en: 'Auto selection' })
            },
          },
        ],
      },
    },
    {
      id: 'evt-perdida-ok',
      category: 'internet',
      urgent: false,
      severity: 'ok',
      get title() {
        return demoText({ es: 'Pérdida de paquetes resuelta', en: 'Packet loss resolved' })
      },
      get description() {
        return demoText({
          es: 'La WAN vuelve a estar estable (0 % de pérdida)',
          en: 'WAN is stable again (0 % loss)',
        })
      },
      time: '1 day ago',
      ts: ago(25 * 3600),
      read: true,
      routerId: 'flint2',
      day: 'ayer',
      kind: 'sistema',
      icon: Activity,
      context: {
        facts: [
          {
            get label() {
              return demoText({ es: 'Pérdida actual', en: 'Current loss' })
            },
            value: '0 %',
          },
          {
            get label() {
              return demoText({ es: 'Latencia WAN', en: 'WAN latency' })
            },
            value: '8 ms',
          },
        ],
        get note() {
          return demoText({
            es: 'Duró ~4 min por un microcorte del ISP (Digi).',
            en: 'Lasted ~4 min due to a micro-outage from the ISP (Digi).',
          })
        },
      },
    },
    {
      id: 'evt-nest-hub',
      category: 'clients',
      urgent: false,
      severity: 'info',
      get title() {
        return demoText({ es: 'Nuevo dispositivo', en: 'New device' })
      },
      get description() {
        return demoText({
          es: "'Nest Hub' se ha unido a Estudio",
          en: "'Nest Hub' joined Study",
        })
      },
      time: '1 day ago',
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
      get title() {
        return demoText({ es: 'Listas de AdGuard actualizadas', en: 'AdGuard filter lists updated' })
      },
      get description() {
        return demoText({
          es: '218 442 reglas activas en 6 listas de filtros',
          en: '218,442 active rules across 6 filter lists',
        })
      },
      time: '1 day ago',
      ts: ago(27 * 3600),
      read: true,
      routerId: 'flint2',
      day: 'ayer',
      kind: 'adguard',
      icon: ShieldCheck,
    },

    // ----- 12 nov (arranque del historial, 32 días) -----
    {
      id: 'evt-reinicio-gateway',
      category: 'router',
      urgent: false,
      severity: 'info',
      get title() {
        return demoText({ es: 'Reinicio programado del Gateway', en: 'Scheduled Gateway reboot' })
      },
      get description() {
        return demoText({
          es: 'Mantenimiento a las 03:12 - el uptime actual arranca aquí',
          en: 'Maintenance at 03:12 - current uptime starts here',
        })
      },
      time: '32 days ago',
      ts: ago(32 * 24 * 3600),
      read: true,
      routerId: 'flint2',
      day: '12nov',
      kind: 'router',
      icon: RefreshCw,
      context: {
        facts: [
          {
            get label() {
              return demoText({ es: 'Equipo', en: 'Device' })
            },
            value: `${modelShort('flint2') ?? 'Flint 2'} · 192.168.8.1`,
          },
          {
            get label() {
              return demoText({ es: 'Uptime desde entonces', en: 'Uptime since then' })
            },
            get value() {
              return fmtUptime('32d 14h')
            },
          },
        ],
      },
    },
    {
      id: 'evt-wg-setup',
      category: 'vpn',
      urgent: false,
      severity: 'info',
      get title() {
        return demoText({ es: 'WireGuard configurado', en: 'WireGuard configured' })
      },
      get description() {
        return demoText({
          es: `Servidor ${wg.interface} activo con ${wg.peers.length} peers (${wg.subnet})`,
          en: `${wg.interface} server active with ${wg.peers.length} peers (${wg.subnet})`,
        })
      },
      time: '32 days ago',
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
      get title() {
        return demoText({ es: 'AdGuard Home activado', en: 'AdGuard Home activated' })
      },
      get description() {
        return demoText({
          es: 'DNS de la red apuntando a 192.168.8.1:3000',
          en: 'Network DNS pointing to 192.168.8.1:3000',
        })
      },
      time: '32 days ago',
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
      get title() {
        return demoText({ es: 'NetPulse empezó a monitorizar', en: 'NetPulse started monitoring' })
      },
      get description() {
        return demoText({
          es: 'Primer evento del historial - bienvenido a bordo',
          en: 'First event in history - welcome aboard',
        })
      },
      time: '32 days ago',
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
// Feed LIVE: solo alertas reales del backend - NADA de datos del mockup
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
