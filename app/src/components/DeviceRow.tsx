import {
  AirVent,
  Antenna,
  Armchair,
  Baby,
  Bath,
  BatteryCharging,
  BedDouble,
  BellRing,
  Blinds,
  Bot,
  BrainCircuit,
  BrushCleaning,
  Cable,
  Camera,
  Car,
  Cast,
  Cat,
  Cctv,
  ChefHat,
  CircuitBoard,
  Cloud,
  Coffee,
  Cog,
  CookingPot,
  Cpu,
  Database,
  Dog,
  DoorClosed,
  Droplets,
  Fan,
  Fingerprint,
  Fish,
  Flame,
  FlameKindling,
  Flower2,
  Gamepad2,
  Gauge,
  GlassWater,
  HardDrive,
  Headphones,
  Headset,
  HeartPulse,
  Heater,
  Home,
  Joystick,
  KeyRound,
  Lamp,
  LampCeiling,
  LampDesk,
  Laptop,
  Lightbulb,
  LockKeyhole,
  Microwave,
  Monitor,
  Network,
  Package,
  PawPrint,
  Plug,
  PlugZap,
  Power,
  Printer,
  Projector,
  Radio,
  Refrigerator,
  Router,
  SatelliteDish,
  Scale,
  ScanFace,
  Server,
  Shield,
  ShieldCheck,
  Shirt,
  ShowerHead,
  SignalHigh,
  SignalLow,
  SignalMedium,
  SignalZero,
  Siren,
  Smartphone,
  Snowflake,
  Sofa,
  SolarPanel,
  Speaker,
  Sprout,
  Tablet,
  Thermometer,
  Toilet,
  Tv,
  UtensilsCrossed,
  WashingMachine,
  Watch,
  Webcam,
  Wifi,
  Wrench,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Device, DeviceType } from '@/data/mock'
import { fmtEs, signalLevel } from '@/data/mock'
import { useNetPulse } from '@/data/DataProvider'
import { Sparkline } from '@/components/Sparkline'
import { cn } from '@/lib/utils'

/** Mapeo semántico tipo → icono (design.md §6) */
export const DEVICE_ICONS: Record<DeviceType, LucideIcon> = {
  ordenador: Monitor,
  tv: Tv,
  movil: Smartphone,
  portatil: Laptop,
  tablet: Tablet,
  consola: Gamepad2,
  iot: Lightbulb,
  camara: Cctv,
  aspirador: BrushCleaning,
  videoportero: BellRing,
  clima: AirVent,
  caldera: Heater,
  placa: CircuitBoard,
  altavoz: Speaker,
  servidor: Server,
  switch: Network,
  modem: Router,
  desconocido: CircuitBoard,
}

export const ICON_OVERRIDES: Record<string, LucideIcon> = {
  monitor: Monitor,
  laptop: Laptop,
  smartphone: Smartphone,
  tablet: Tablet,
  tv: Tv,
  gamepad: Gamepad2,
  camera: Camera,
  cctv: Cctv,
  vacuum: BrushCleaning,
  doorbell: BellRing,
  'air-vent': AirVent,
  heater: Heater,
  'circuit-board': CircuitBoard,
  speaker: Speaker,
  router: Network,
  server: Server,
  watch: Watch,
  car: Car,
  home: Home,
  printer: Printer,
  plug: Plug,
  shield: Shield,
  'help-circle': CircuitBoard,
  wifi: Wifi,
  ethernet: Cable,
  refrigerator: Refrigerator,
  'washing-machine': WashingMachine,
  microwave: Microwave,
  'cooking-pot': CookingPot,
  'chef-hat': ChefHat,
  coffee: Coffee,
  'utensils-crossed': UtensilsCrossed,
  'glass-water': GlassWater,
  'shower-head': ShowerHead,
  bath: Bath,
  toilet: Toilet,
  droplets: Droplets,
  flame: Flame,
  'flame-kindling': FlameKindling,
  thermometer: Thermometer,
  fan: Fan,
  snowflake: Snowflake,
  'plug-zap': PlugZap,
  'battery-charging': BatteryCharging,
  'solar-panel': SolarPanel,
  power: Power,
  lightbulb: Lightbulb,
  lamp: Lamp,
  'lamp-desk': LampDesk,
  'lamp-ceiling': LampCeiling,
  'lock-keyhole': LockKeyhole,
  'key-round': KeyRound,
  'door-closed': DoorClosed,
  'scan-face': ScanFace,
  fingerprint: Fingerprint,
  'shield-check': ShieldCheck,
  webcam: Webcam,
  siren: Siren,
  'paw-print': PawPrint,
  cat: Cat,
  dog: Dog,
  fish: Fish,
  baby: Baby,
  scale: Scale,
  'heart-pulse': HeartPulse,
  projector: Projector,
  cast: Cast,
  headphones: Headphones,
  headset: Headset,
  radio: Radio,
  joystick: Joystick,
  'hard-drive': HardDrive,
  database: Database,
  cloud: Cloud,
  cpu: Cpu,
  'brain-circuit': BrainCircuit,
  bot: Bot,
  antenna: Antenna,
  'satellite-dish': SatelliteDish,
  blinds: Blinds,
  armchair: Armchair,
  sofa: Sofa,
  'bed-double': BedDouble,
  sprout: Sprout,
  'flower-2': Flower2,
  wrench: Wrench,
  cog: Cog,
  package: Package,
  shirt: Shirt,
  gauge: Gauge,
}

export const ALLOWED_ICONS = Object.keys(ICON_OVERRIDES) as string[]

/** Orden de categorías del selector. Cada clave tiene su etiqueta bajo
    devices.edit.iconCategories y cada icono pertenece a un único grupo,
    para que la lista larga se pueda escanear en vez de una rejilla plana. */
export const ICON_CATEGORIES: { id: string; icons: string[] }[] = [
  {
    id: 'general',
    icons: [
      'monitor', 'laptop', 'smartphone', 'tablet', 'tv', 'gamepad', 'camera',
      'cctv', 'vacuum', 'doorbell', 'air-vent', 'heater', 'circuit-board',
      'speaker', 'router', 'server', 'watch', 'car', 'printer', 'plug',
      'shield', 'wifi', 'ethernet',
    ],
  },
  {
    id: 'kitchen',
    icons: [
      'refrigerator', 'washing-machine', 'microwave', 'cooking-pot', 'chef-hat',
      'coffee', 'utensils-crossed', 'glass-water',
    ],
  },
  {
    id: 'water',
    icons: [
      'shower-head', 'bath', 'toilet', 'droplets', 'flame', 'flame-kindling',
      'thermometer', 'fan', 'snowflake',
    ],
  },
  { id: 'energy', icons: ['plug-zap', 'battery-charging', 'solar-panel', 'power'] },
  { id: 'lighting', icons: ['lightbulb', 'lamp', 'lamp-desk', 'lamp-ceiling'] },
  {
    id: 'security',
    icons: [
      'lock-keyhole', 'key-round', 'door-closed', 'scan-face', 'fingerprint',
      'shield-check', 'webcam', 'siren',
    ],
  },
  {
    id: 'family',
    icons: ['paw-print', 'cat', 'dog', 'fish', 'baby', 'scale', 'heart-pulse'],
  },
  {
    id: 'media',
    icons: ['projector', 'cast', 'headphones', 'headset', 'radio', 'joystick'],
  },
  {
    id: 'network',
    icons: [
      'hard-drive', 'database', 'cloud', 'cpu', 'brain-circuit', 'bot',
      'antenna', 'satellite-dish',
    ],
  },
  {
    id: 'home',
    icons: [
      'blinds', 'armchair', 'sofa', 'bed-double', 'sprout', 'flower-2',
      'wrench', 'cog', 'package', 'shirt', 'gauge', 'home',
    ],
  },
]

export function deviceIcon(device: Device): LucideIcon {
  const override = device.iconOverride ? ICON_OVERRIDES[device.iconOverride] : undefined
  return override ?? DEVICE_ICONS[device.type] ?? CircuitBoard
}

const SIGNAL_ICONS = {
  high: SignalHigh,
  medium: SignalMedium,
  low: SignalLow,
  zero: SignalZero,
  cable: Cable,
} as const

export function SignalIcon({ dbm, className }: { dbm: number | null; className?: string }) {
  const { t } = useTranslation()
  const level = signalLevel(dbm)
  const Icon = SIGNAL_ICONS[level]
  return (
    <Icon
      className={cn(
        'h-4 w-4',
        level === 'high' && 'text-ok',
        level === 'medium' && 'text-accent',
        level === 'low' && 'text-warn',
        level === 'zero' && 'text-danger',
        level === 'cable' && 'text-text-muted',
        className,
      )}
      strokeWidth={1.75}
      aria-label={dbm === null ? t('devices.wiredConnection') : t('devices.signalDbm', { dbm })}
    />
  )
}

interface DeviceRowProps {
  device: Device
  /** compact: nombre + caption + sparkline + tráfico (home); full: IP/MAC + chips */
  variant?: 'compact' | 'full'
  className?: string
  onClick?: () => void
}

/** Fila de dispositivo 56px (design.md §10.5) */
export function DeviceRow({ device, variant = 'compact', className, onClick }: DeviceRowProps) {
  const { t } = useTranslation()
  const { routers } = useNetPulse()
  const routerName = (id: string) => routers.find((r) => r.id === id)?.name ?? id
  const Icon = deviceIcon(device)
  return (
    <div
      onClick={onClick}
      className={cn(
        'group flex h-14 items-center gap-3 rounded-xl px-3 transition-colors duration-150 hover:bg-hover',
        onClick && 'cursor-pointer',
        className,
      )}
    >
      <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-elevated text-text-secondary">
        <Icon className="h-[18px] w-[18px]" strokeWidth={1.75} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium text-text-primary" translate="no">{device.name}</span>
          {device.isNew && (
            <span className="rounded-full bg-accent-soft px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-accent">
              {t('devices.new')}
            </span>
          )}
        </div>
        <div className="truncate text-caption text-text-muted">
          {variant === 'compact' ? (
            <span>
              {routerName(device.routerId)} · {device.band}
            </span>
          ) : (
            <span className="font-mono text-mono-sm">
              {device.ip}
              {device.mac !== '—' && <span className="text-text-muted/70"> · {device.mac}</span>}
            </span>
          )}
        </div>
      </div>
      {variant === 'full' && (
        <div className="hidden items-center gap-2 md:flex">
          <span className="rounded-full bg-elevated px-2 py-0.5 text-caption font-medium text-text-secondary">
            {routerName(device.routerId)}
          </span>
          <span className="rounded-full bg-elevated px-2 py-0.5 font-mono text-caption text-text-secondary">
            {device.band}
          </span>
          {device.signalDbm !== null && (
            <span className="font-mono text-mono-sm text-text-muted">{device.signalDbm} dBm</span>
          )}
        </div>
      )}
      {variant === 'compact' && (
        <Sparkline
          data={device.sparkline}
          width={60}
          height={20}
          className="hidden shrink-0 text-accent transition-all sm:block"
        />
      )}
      <div className="flex shrink-0 items-center gap-2">
        <span className="font-mono text-mono-sm text-accent">
          {device.trafficMbps >= 1 ? fmtEs(device.trafficMbps, 1) : fmtEs(device.trafficMbps, 2)} Mbps
        </span>
        <SignalIcon dbm={device.signalDbm} />
      </div>
    </div>
  )
}
