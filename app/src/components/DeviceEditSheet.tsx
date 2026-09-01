import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { LucideIcon } from 'lucide-react'
import { ALLOWED_ICONS, ICON_OVERRIDES } from '@/components/DeviceRow'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetContent,
} from '@/components/ui/sheet'
import { cn, fetchJson } from '@/lib/utils'
import type { ClientDevice } from '@/pages/devices-data'

export interface DeviceEditSheetProps {
  open: boolean
  device: ClientDevice | null
  isDemo: boolean
  saving?: boolean
  onClose: () => void
  onSave: (device: ClientDevice, icon: string | null) => void
}

export function DeviceEditSheet({
  open,
  device,
  isDemo,
  saving,
  onClose,
  onSave,
}: DeviceEditSheetProps) {
  const { t } = useTranslation()
  const [icon, setIcon] = useState(device?.iconOverride ?? '')
  const [reservation, setReservation] = useState<{ reserved: boolean; ip: string; loading: boolean }>({ reserved: false, ip: '', loading: false })
  const [reserveDraft, setReserveDraft] = useState(device?.ip ?? '')
  const [block, setBlock] = useState<{ blocked: boolean; loading: boolean }>({ blocked: false, loading: false })

  useEffect(() => {
    setIcon(device?.iconOverride ?? '')
    setReserveDraft(device?.ip ?? '')
  }, [device?.id, device?.iconOverride, device?.ip])

  useEffect(() => {
    if (!device || isDemo) return
    let cancelled = false
    const load = async () => {
      setReservation((p) => ({ ...p, loading: true }))
      setBlock((p) => ({ ...p, loading: true }))
      const [res, blk] = await Promise.all([
        fetchJson<{ reserved: boolean; ip?: string }>(`/api/devices/${encodeURIComponent(device.mac)}/reservation`),
        fetchJson<{ blocked: boolean }>(`/api/devices/${encodeURIComponent(device.mac)}/block?router=${encodeURIComponent(device.routerId)}`),
      ])
      if (cancelled) return
      if (res.ok) {
        setReservation({ reserved: res.data.reserved, ip: res.data.ip ?? '', loading: false })
        if (res.data.ip) setReserveDraft(res.data.ip)
      } else {
        setReservation({ reserved: false, ip: '', loading: false })
      }
      if (blk.ok) {
        setBlock({ blocked: blk.data.blocked, loading: false })
      } else {
        setBlock({ blocked: false, loading: false })
      }
    }
    void load()
    return () => { cancelled = true }
  }, [device, isDemo])

  const handleSave = () => {
    if (!device) return
    onSave(device, icon || null)
  }

  const selectedIconName = icon || null

  const PreviewIcon = (selectedIconName ? (ICON_OVERRIDES[selectedIconName] ?? ICON_OVERRIDES['help-circle']) : ICON_OVERRIDES['help-circle']) as LucideIcon

  return (
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent side="right" className="w-full sm:max-w-md">
        {device && (
          <div className="flex flex-col gap-5 overflow-y-auto px-4 py-2">
            {/* Vista previa */}
            <div className="flex items-center gap-3 rounded-xl border border-border bg-elevated/50 p-3">
              <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-elevated text-text-secondary">
                <PreviewIcon className="h-6 w-6" strokeWidth={1.75} />
              </div>
              <div className="min-w-0">
                <div className="truncate text-sm font-medium text-text-primary">{device.name}</div>
                <div className="truncate text-caption text-text-muted">{device.mac}</div>
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

            {/* Reserva DHCP */}
            <div className="space-y-2 rounded-xl border border-border bg-elevated/40 p-3">
              <div className="flex items-center justify-between">
                <label className="text-label uppercase text-text-muted">{t('devices.edit.reserveTitle')}</label>
                {reservation.loading && <span className="text-caption text-text-muted">{t('common.loading')}</span>}
              </div>
              {reservation.reserved ? (
                <>
                  <p className="text-sm text-text-secondary">
                    {t('devices.edit.reservedAs', { ip: reservation.ip })}
                  </p>
                  <div className="flex gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      className="flex-1"
                      disabled={reservation.loading || reserveDraft === device.ip}
                      onClick={() => setReserveDraft(device.ip)}
                    >
                      {t('devices.edit.useCurrentIp')}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      className="flex-1 border-danger/30 text-danger hover:bg-danger/10"
                      disabled={reservation.loading}
                      onClick={async () => {
                        if (!device) return
                        setReservation((p) => ({ ...p, loading: true }))
                        const res = await fetchJson(`/api/devices/${encodeURIComponent(device.mac)}/reservation`, { method: 'DELETE' })
                        setReservation({ reserved: !res.ok, ip: res.ok ? '' : reservation.ip, loading: false })
                      }}
                    >
                      {t('devices.edit.removeReserve')}
                    </Button>
                  </div>
                </>
              ) : (
                <p className="text-caption text-text-muted">{t('devices.edit.reserveHint')}</p>
              )}
              <div className="flex items-center gap-2">
                <Input
                  value={reserveDraft}
                  onChange={(e) => setReserveDraft(e.target.value)}
                  placeholder={device.ip}
                  disabled={reservation.loading}
                  className="h-9 rounded-lg border-border bg-elevated text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent/50"
                />
                <Button
                  size="sm"
                  disabled={reservation.loading || !reserveDraft}
                  onClick={async () => {
                    if (!device) return
                    setReservation((p) => ({ ...p, loading: true }))
                    const res = await fetchJson(`/api/devices/${encodeURIComponent(device.mac)}/reservation`, {
                      method: 'PUT',
                      headers: { 'Content-Type': 'application/json' },
                      body: JSON.stringify({ ip: reserveDraft, hostname: device.name }),
                    })
                    if (res.ok) {
                      setReservation({ reserved: true, ip: reserveDraft, loading: false })
                    } else {
                      setReservation((p) => ({ ...p, loading: false }))
                    }
                  }}
                >
                  {t('devices.edit.saveReserve')}
                </Button>
              </div>
            </div>

            {/* Bloqueo de dispositivo */}
            <div className="space-y-2 rounded-xl border border-border bg-elevated/40 p-3">
              <div className="flex items-center justify-between">
                <label className="text-label uppercase text-text-muted">{t('devices.edit.blockTitle')}</label>
                {block.loading && <span className="text-caption text-text-muted">{t('common.loading')}</span>}
              </div>
              <p className="text-caption text-text-muted">{t('devices.edit.blockHint')}</p>
              <div className="flex items-center justify-between rounded-lg border border-border bg-elevated p-2">
                <span className={cn('text-sm font-medium', block.blocked ? 'text-danger' : 'text-text-secondary')}>
                  {block.blocked ? t('devices.edit.blocked') : t('devices.edit.allowed')}
                </span>
                <Button
                  variant={block.blocked ? 'default' : 'destructive'}
                  size="sm"
                  disabled={block.loading}
                  onClick={async () => {
                    if (!device) return
                    setBlock((p) => ({ ...p, loading: true }))
                    const url = `/api/devices/${encodeURIComponent(device.mac)}/block`
                    const res = await fetchJson(url, {
                      method: block.blocked ? 'DELETE' : 'PUT',
                      headers: { 'Content-Type': 'application/json' },
                      body: JSON.stringify({ router: device.routerId }),
                    })
                    if (res.ok) {
                      setBlock({ blocked: !block.blocked, loading: false })
                    } else {
                      setBlock((p) => ({ ...p, loading: false }))
                    }
                  }}
                >
                  {block.blocked ? t('devices.edit.unblock') : t('devices.edit.block')}
                </Button>
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
