/**
 * Devices — enriquecido local de clientes (design.md §11 / devices.md §④).
 *
 * SPEC-65 D65-1 (single-source): el canon de dispositivos (65 IDs únicos,
 * D3/D5: 59 online · 6 offline · 3 nuevos hoy; online por router Gateway 26,
 * Salón 20, Estudio 8, Patio 5) vive SOLO en `@/data/demo-canon.json`
 * (re-exportado por `@/data/mock`) y ya trae inline los metadatos de cliente
 * (hostname, DHCP, primer visto, tráfico 24h, AdGuard, grupo). Este módulo
 * ya NO expande ni duplica el canon: solo fusiona metadatos.
 * - Canon con metadatos inline (demo local o API demo): se usan tal cual
 *   (el JSON es la verdad reconciliada).
 * - Live sin metadatos (servidor real): se fusionan los detalles conocidos
 *   por id (CANON_DETAILS) o se derivan valores plausibles — nunca se
 *   inventan dispositivos que la API no ha reportado.
 */
import type { LucideIcon } from 'lucide-react'
import { BookOpen, Network } from 'lucide-react'
import type { Device } from '@/data/mock'
import { devices as canonDevices } from '@/data/mock'

// ---------------------------------------------------------------------------
// Tipos
// ---------------------------------------------------------------------------

export type FilterGroup = 'infra' | 'moviles' | 'ordenadores' | 'tv' | 'iot' | 'red' | 'otros'

export interface ClientDevice extends Device {
  hostname: string
  /** "renews in 9h 12min" · "Static IP (reservation)" · "Expired" */
  dhcpLease: string
  /** "84 days ago" · "today" */
  firstSeen: string
  /** Tráfico total 24h, ya formateado ES */
  traffic24hRx: string
  traffic24hTx: string
  /** Cliente protegido por AdGuard Home */
  adguard: boolean
  /** Grupo del filtro "Tipo" (devices.md §③) */
  group: FilterGroup
  /** Visto por primera vez en los últimos 7 días (stat "Nuevos (7 días)") */
  newThisWeek?: boolean
  /** Icono lucide específico cuando el tipo genérico no basta */
  iconOverride?: LucideIcon
}

// 'infra' primero (D6): hipervisores, CTs y switches gestionados de un vistazo.
// El grupo 'infra' no deriva del `type`: lo asigna Devices.tsx cruzando
// device.infra (sellado server-side, SPEC-65 D65-2) o, como fallback,
// attachTo/lldp con los distributionNodes del provider.
export const GROUP_ORDER: FilterGroup[] = ['infra', 'moviles', 'ordenadores', 'tv', 'iot', 'red', 'otros']

// ---------------------------------------------------------------------------
// Detalles extra de los dispositivos canónicos: fallback para live cuando la
// API no trae metadatos inline (el canon JSON del demo ya los trae todos).
// ---------------------------------------------------------------------------

const CANON_DETAILS: Record<string, Omit<ClientDevice, keyof Device>> = {
  'imac-salon': {
    hostname: 'imac-de-marc', dhcpLease: 'Static IP (reservation)', firstSeen: '320 days ago',
    traffic24hRx: '38 GB', traffic24hTx: '2.1 GB', adguard: true, group: 'ordenadores',
  },
  'tv-samsung': {
    hostname: 'samsung-tv-salon', dhcpLease: 'renews in 7h 48min', firstSeen: '290 days ago',
    traffic24hRx: '54 GB', traffic24hTx: '1.2 GB', adguard: true, group: 'tv',
  },
  'pixel-8-pro': {
    hostname: 'pixel-8-pro', dhcpLease: 'renews in 9h 12min', firstSeen: '214 days ago',
    traffic24hRx: '4.2 GB', traffic24hTx: '310 MB', adguard: true, group: 'moviles',
  },
  'macbook-air': {
    hostname: 'macbook-air-de-ana', dhcpLease: 'renews in 5h 31min', firstSeen: '260 days ago',
    traffic24hRx: '18 GB', traffic24hTx: '2.2 GB', adguard: true, group: 'ordenadores',
  },
  'ps5': {
    hostname: 'ps5-salon', dhcpLease: 'Static IP (reservation)', firstSeen: '300 days ago',
    traffic24hRx: '92 GB', traffic24hTx: '4.8 GB', adguard: true, group: 'tv',
  },
  'robot-aspirador': {
    hostname: 'roborock-s8', dhcpLease: 'renews in 10h 2min', firstSeen: '180 days ago',
    traffic24hRx: '340 MB', traffic24hTx: '28 MB', adguard: true, group: 'iot',
  },
  'camara-porche': {
    hostname: 'reolink-porche', dhcpLease: 'renews in 3h 57min', firstSeen: '150 days ago',
    traffic24hRx: '11 GB', traffic24hTx: '640 MB', adguard: true, group: 'iot',
  },
  'nest-mini': {
    hostname: 'nest-mini-estudio', dhcpLease: 'renews in 8h 20min', firstSeen: '240 days ago',
    traffic24hRx: '1.4 GB', traffic24hTx: '88 MB', adguard: true, group: 'iot',
  },
  'nas-synology': {
    hostname: 'diskstation', dhcpLease: 'Static IP (reservation)', firstSeen: '320 days ago',
    traffic24hRx: '96 GB', traffic24hTx: '1.1 TB', adguard: true, group: 'red',
  },
  'galaxy-tab-s9': {
    hostname: 'galaxy-tab-s9', dhcpLease: 'renews in 11h 5min', firstSeen: 'today',
    traffic24hRx: '640 MB', traffic24hTx: '48 MB', adguard: true, group: 'moviles', newThisWeek: true,
  },
  // D1: el GS308E vuelve a ser Device (además del distnode managed del canon).
  // Su grupo visible es 'infra' (lo reasigna Devices.tsx con device.infra/lldp).
  'switch-netgear': {
    hostname: 'gs308e', dhcpLease: 'Static IP (reservation)', firstSeen: '320 days ago',
    traffic24hRx: '96 MB', traffic24hTx: '42 MB', adguard: false, group: 'red',
  },
}

// ---------------------------------------------------------------------------
// Builder: enriquece una lista de `Device` (mock local o API) con los
// metadatos de cliente (hostname, DHCP, AdGuard, grupo de filtro).
// ---------------------------------------------------------------------------

const TYPE_TO_GROUP: Record<Device['type'], FilterGroup> = {
  movil: 'moviles',
  tablet: 'moviles',
  ordenador: 'ordenadores',
  portatil: 'ordenadores',
  tv: 'tv',
  consola: 'tv',
  iot: 'iot',
  camara: 'iot',
  altavoz: 'iot',
  servidor: 'red',
  switch: 'red',
  desconocido: 'otros',
}

/** Iconos lucide referenciados POR NOMBRE en el canon JSON (iconOverride). */
const ICON_BY_NAME: Record<string, LucideIcon> = { BookOpen, Network }

function slug(name: string): string {
  return (
    name
      .toLowerCase()
      .normalize('NFD')
      .replace(/[̀-ͯ]/g, '')
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-|-$/g, '') || 'cliente'
  )
}

/** Detalles por defecto para un cliente que la API reporta sin metadatos. */
function defaultDetails(d: Device): Omit<ClientDevice, keyof Device> {
  return {
    hostname: slug(d.name),
    dhcpLease: d.online ? 'renews in 12h 0min' : 'Expired',
    firstSeen: '—',
    traffic24hRx: '—',
    traffic24hTx: '—',
    adguard: true,
    group: TYPE_TO_GROUP[d.type],
  }
}

/** Metadatos de cliente que pueden venir inline (canon JSON / API demo). */
interface InlineDetails {
  hostname?: string
  dhcpLease?: string
  firstSeen?: string
  traffic24hRx?: string
  traffic24hTx?: string
  adguard?: boolean
  group?: FilterGroup
  iconOverride?: string
  newThisWeek?: boolean
}

/** Extrae los metadatos inline del canon; null si el Device no los trae. */
function inlineDetails(d: Device): Omit<ClientDevice, keyof Device> | null {
  const raw = d as Device & InlineDetails
  if (typeof raw.hostname !== 'string' || typeof raw.group !== 'string') return null
  return {
    hostname: raw.hostname,
    dhcpLease: raw.dhcpLease ?? '—',
    firstSeen: raw.firstSeen ?? '—',
    traffic24hRx: raw.traffic24hRx ?? '—',
    traffic24hTx: raw.traffic24hTx ?? '—',
    adguard: raw.adguard ?? true,
    group: raw.group ?? TYPE_TO_GROUP[d.type],
    newThisWeek: raw.newThisWeek,
    iconOverride: raw.iconOverride ? ICON_BY_NAME[raw.iconOverride] : undefined,
  }
}

/**
 * Fusiona metadatos de cliente sobre la lista de Devices. Prioridad:
 * inline (canon JSON / API demo) > CANON_DETAILS (ids conocidos) > defaults.
 * `_withSynthetic` se conserva por compatibilidad de firma: desde D65-1 el
 * canon ya llega completo (65 IDs) y nunca se sintetizan devices extra.
 */
export function buildClientDevices(canon: Device[], _withSynthetic = false): ClientDevice[] {
  return canon.map((d) => ({ ...d, ...(inlineDetails(d) ?? CANON_DETAILS[d.id] ?? defaultDetails(d)) }))
}

export const allDevices: ClientDevice[] = buildClientDevices(canonDevices, true)

/** Señal débil: online con < −70 dBm (devices.md §②) */
export const weakSignalCount = allDevices.filter((d) => d.online && d.signalDbm !== null && d.signalDbm < -70).length

export const newThisWeekDevices = allDevices.filter((d) => d.newThisWeek)

export const adguardProtected = allDevices.filter((d) => d.adguard).length
