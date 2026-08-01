import {
  Camera,
  Gamepad2,
  HelpCircle,
  Laptop,
  Lightbulb,
  Monitor,
  Network,
  Server,
  SignalHigh,
  SignalLow,
  SignalMedium,
  SignalZero,
  Smartphone,
  Speaker,
  Tablet,
  Tv,
  Cable,
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
  camara: Camera,
  altavoz: Speaker,
  servidor: Server,
  switch: Network,
  desconocido: HelpCircle,
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
  const Icon = DEVICE_ICONS[device.type] ?? HelpCircle
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
          <span className="truncate text-sm font-medium text-text-primary">{device.name}</span>
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
