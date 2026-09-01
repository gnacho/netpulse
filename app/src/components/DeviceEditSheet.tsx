import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { LucideIcon } from 'lucide-react'
import { Pencil, ShieldBan } from 'lucide-react'
import { ALLOWED_ICONS, ICON_OVERRIDES } from '@/components/DeviceRow'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from '@/components/ui/sheet'
import { cn } from '@/lib/utils'
import type { ClientDevice } from '@/pages/devices-data'

export interface DeviceEditSheetProps {
  open: boolean
  device: ClientDevice | null
  currentName: string
  isDemo: boolean
  saving?: boolean
  onClose: () => void
  onSave: (device: ClientDevice, name: string | null, icon: string | null) => void
}

export function DeviceEditSheet({
  open,
  device,
  currentName,
  isDemo,
  saving,
  onClose,
  onSave,
}: DeviceEditSheetProps) {
  const { t } = useTranslation()
  const [name, setName] = useState(currentName)
  const [icon, setIcon] = useState(device?.iconOverride ?? '')

  const activeName = name.trim() || device?.name || ''
  const selectedIconName = icon || null

  const handleSave = () => {
    if (!device) return
    const nextName = name.trim() === device.name ? null : name.trim() || null
    const nextIcon = icon || null
    onSave(device, nextName, nextIcon)
  }

  const PreviewIcon = (selectedIconName ? (ICON_OVERRIDES[selectedIconName] ?? ICON_OVERRIDES['help-circle']) : ICON_OVERRIDES['help-circle']) as LucideIcon

  const handleBan = (band: '2.4' | '5' | '6') => {
    // Fase 2 (#437): el baneo por banda todavía no está implementado.
    void band
  }

  return (
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent side="right" className="w-full sm:max-w-md">
        <SheetHeader>
          <SheetTitle>{t('devices.edit.title')}</SheetTitle>
          <SheetDescription>{t('devices.edit.description')}</SheetDescription>
        </SheetHeader>

        {device && (
          <div className="flex flex-col gap-5 overflow-y-auto px-4 py-2">
            {/* Vista previa */}
            <div className="flex items-center gap-3 rounded-xl border border-border bg-elevated/50 p-3">
              <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-elevated text-text-secondary">
                <PreviewIcon className="h-6 w-6" strokeWidth={1.75} />
              </div>
              <div className="min-w-0">
                <div className="truncate text-sm font-medium text-text-primary">{activeName}</div>
                <div className="truncate text-caption text-text-muted">{device.mac}</div>
              </div>
            </div>

            {/* Nombre */}
            <div className="space-y-1.5">
              <label htmlFor="device-edit-name" className="text-label uppercase text-text-muted">
                {t('devices.edit.name')}
              </label>
              <div className="relative">
                <Pencil className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-text-muted" strokeWidth={1.75} />
                <Input
                  id="device-edit-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder={device.name}
                  maxLength={40}
                  disabled={saving}
                  className="h-10 rounded-lg border-border bg-elevated pl-9 text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent/50"
                />
              </div>
            </div>

            {/* Icono */}
            <div className="space-y-2">
              <label className="text-label uppercase text-text-muted">{t('devices.edit.icon')}</label>
              <div className="grid grid-cols-5 gap-2">
                <button
                  type="button"
                  onClick={() => setIcon('')}
                  disabled={saving}
                  className={cn(
                    'flex h-11 items-center justify-center rounded-lg border bg-elevated text-xs font-medium text-text-secondary transition-colors hover:bg-hover',
                    icon === '' ? 'border-accent bg-accent-soft text-accent' : 'border-border'
                  )}
                >
                  {t('devices.edit.auto')}
                </button>
                {ALLOWED_ICONS.filter((n) => n !== 'help-circle').map((n) => {
                  const Icon = ICON_OVERRIDES[n]!
                  return (
                    <button
                      key={n}
                      type="button"
                      title={n}
                      onClick={() => setIcon(n)}
                      disabled={saving}
                      className={cn(
                        'flex h-11 items-center justify-center rounded-lg border transition-colors hover:bg-hover',
                        icon === n ? 'border-accent bg-accent-soft text-accent' : 'border-border bg-elevated text-text-secondary'
                      )}
                    >
                      <Icon className="h-5 w-5" strokeWidth={1.75} />
                    </button>
                  )
                })}
              </div>
            </div>

            {/* Detalles de red */}
            <div className="grid grid-cols-2 gap-3 rounded-xl border border-border bg-elevated/40 p-3 text-sm">
              <div>
                <div className="text-label uppercase text-text-muted">IP</div>
                <div className="font-mono text-text-primary">{device.ip}</div>
              </div>
              <div>
                <div className="text-label uppercase text-text-muted">{t('devices.colBand')}</div>
                <div className="text-text-primary">{device.band === 'cable' ? t('common.cable') : device.band}</div>
              </div>
            </div>

            {/* Baneo (stub Fase 2) */}
            <div className="space-y-2">
              <label className="text-label uppercase text-text-muted">{t('devices.edit.banTitle')}</label>
              <p className="text-caption text-text-muted">{t('devices.edit.banHint')}</p>
              <div className="flex flex-wrap gap-2">
                {(['2.4', '5', '6'] as const).map((band) => (
                  <Button
                    key={band}
                    variant="outline"
                    size="sm"
                    disabled
                    onClick={() => handleBan(band)}
                    className="gap-1.5"
                  >
                    <ShieldBan className="h-3.5 w-3.5" strokeWidth={1.75} />
                    {band} GHz
                  </Button>
                ))}
              </div>
            </div>

            {isDemo && (
              <p className="rounded-lg border border-warn/30 bg-warn/10 p-3 text-caption text-warn">
                {t('devices.edit.demoNotice')}
              </p>
            )}

            <div className="mt-auto flex gap-3 pt-2">
              <Button variant="outline" className="flex-1" onClick={onClose} disabled={saving}>
                {t('common.cancel')}
              </Button>
              <Button className="flex-1" onClick={handleSave} disabled={saving}>
                {saving ? t('common.loading') : t('devices.edit.save')}
              </Button>
            </div>
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}
