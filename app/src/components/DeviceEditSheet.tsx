import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { LucideIcon } from 'lucide-react'
import { ALLOWED_ICONS, ICON_OVERRIDES } from '@/components/DeviceRow'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { cn, fetchJson } from '@/lib/utils'
import { useNetPulse } from '@/data/DataProvider'
import type { ClientDevice } from '@/pages/devices-data'

// #693: el server solo acepta hostnames DNS en la reserva DHCP. Un nombre
// visible libre ("TV Salón") viaja vacío y la reserva usa la MAC como name.
const DNS_HOSTNAME_RE = /^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/
const asDhcpHostname = (name: string) =>
  name.length > 0 && name.length <= 253 && DNS_HOSTNAME_RE.test(name) ? name : ''

export interface DeviceEditSheetProps {
  open: boolean
  device: ClientDevice | null
  isDemo: boolean
  saving?: boolean
  onClose: () => void
  /** Patch absoluto: '' en un campo = volver al valor automático (#797). */
  onSave: (device: ClientDevice, patch: { icon: string; name: string; type: string }) => void
  /** #1145: tras borrar el cliente del registro (el padre cierra y refresca). */
  onDeleted?: () => void
  /** #1151: resto de clientes (para el selector de enlace). */
  clients?: ClientDevice[]
  /** #1151: tras enlazar/desenlazar (el padre cierra y refresca). */
  onLinkChanged?: () => void
}

// #797: tipos válidos del clasificador (paridad con adapters.ValidDeviceTypes
// y las claves devices.types.* de los locales).
const DEVICE_TYPES = [
  'movil', 'portatil', 'ordenador', 'tablet', 'tv', 'consola',
  'camara', 'aspirador', 'videoportero', 'clima', 'caldera', 'placa', 'altavoz', 'servidor', 'iot', 'switch', 'desconocido',
] as const

export function DeviceEditSheet({
  open,
  device,
  isDemo,
  saving,
  onClose,
  onSave,
  onDeleted,
  clients,
  onLinkChanged,
}: DeviceEditSheetProps) {
  const { t } = useTranslation()
  const { refresh } = useNetPulse()
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [linkTarget, setLinkTarget] = useState('')
  const [linkBusy, setLinkBusy] = useState(false)
  const [icon, setIcon] = useState(device?.iconOverride ?? '')

  const otherClients = (clients ?? []).filter(
    (c) => c.mac && c.mac !== device?.mac && c.mac !== device?.id && !(device?.aliasMacs ?? []).includes(c.mac),
  )

  const linkClient = async () => {
    if (!linkTarget || !device?.mac) return
    setLinkBusy(true)
    try {
      await fetchJson(`/api/devices/${encodeURIComponent(linkTarget)}/link`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ target: device.mac }),
      })
      onLinkChanged?.()
      onClose()
    } catch {
      // fallo silencioso: el botón vuelve a habilitarse
    } finally {
      setLinkBusy(false)
    }
  }

  const unlinkAlias = async (alias: string) => {
    if (!alias) return
    setLinkBusy(true)
    try {
      await fetchJson(`/api/devices/${encodeURIComponent(alias)}/link`, { method: 'DELETE' })
      onLinkChanged?.()
      onClose()
    } catch {
      // fallo silencioso
    } finally {
      setLinkBusy(false)
    }
  }
  const [name, setName] = useState(device?.nameOverride ?? '')
  const [devType, setDevType] = useState(device?.typeOverride ?? '')
  const [reservation, setReservation] = useState<{ reserved: boolean; ip: string; loading: boolean }>({ reserved: false, ip: '', loading: false })
  const [reserveDraft, setReserveDraft] = useState(device?.ip ?? '')
  const [block, setBlock] = useState<{ blocked: boolean; loading: boolean }>({ blocked: false, loading: false })
  // #754: dry-run de comandos. Antes de escribir en el router (reservar o
  // bloquear) se pide el plan al server y se muestra; nada se aplica hasta
  // la confirmación explícita sobre esos comandos.
  const [plan, setPlan] = useState<{ host: string; apply: string[]; rollback: string[] } | null>(null)
  const [planRun, setPlanRun] = useState<null | (() => Promise<void>)>(null)
  const [planBusy, setPlanBusy] = useState(false)

  const closePlan = () => {
    setPlan(null)
    setPlanRun(null)
  }

  const openPlan = async (path: string, body: unknown, run: () => Promise<void>) => {
    const sep = path.includes('?') ? '&' : '?'
    const res = await fetchJson<{ host?: string; apply?: string[]; rollback?: string[] }>(path + sep + 'dry_run=1', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    if (!res.ok || !res.data) return false
    setPlan({ host: res.data.host ?? '', apply: res.data.apply ?? [], rollback: res.data.rollback ?? [] })
    setPlanRun(() => run)
    return true
  }

  useEffect(() => {
    setIcon(device?.iconOverride ?? '')
    setName(device?.nameOverride ?? '')
    setDevType(device?.typeOverride ?? '')
    setReserveDraft(device?.ip ?? '')
  }, [device?.id, device?.iconOverride, device?.nameOverride, device?.typeOverride, device?.ip])

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
    onSave(device, { icon, name: name.trim(), type: devType })
  }

  // #800: hostname DNS derivado del nombre visible (input del sheet o nombre
  // actual). Vacío = el nombre visible no es aplicable como hostname DNS.
  const appliedHostname = device ? asDhcpHostname(name.trim() || device.name) : ''

  const selectedIconName = icon || null

  const PreviewIcon = (selectedIconName ? (ICON_OVERRIDES[selectedIconName] ?? ICON_OVERRIDES['help-circle']) : ICON_OVERRIDES['help-circle']) as LucideIcon

  return (
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent side="right" className="w-full sm:max-w-md">
        {/* #828: cabecera con título para que el X de cierre quede sobre ella
            (patrón estándar de los sheets) y no montado encima de la tarjeta
            de vista previa con la MAC. */}
        <SheetHeader className="px-4 pb-0 pt-4">
          <SheetTitle className="pr-8 font-display text-h2 text-text-primary">
            {t('devices.edit.title')}
          </SheetTitle>
        </SheetHeader>
        {device && (
          <div className="flex flex-col gap-5 overflow-y-auto px-4 py-2">
            {/* Vista previa */}
            <div className="flex items-center gap-3 rounded-xl border border-border bg-elevated/50 p-3">
              <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-elevated text-text-secondary">
                <PreviewIcon className="h-6 w-6" strokeWidth={1.75} />
              </div>
              <div className="min-w-0">
                <div className="truncate text-sm font-medium text-text-primary">{name.trim() || device.name}</div>
                <div className="truncate text-caption text-text-muted">{device.mac}</div>
              </div>
            </div>

            {/* Nombre visible (#797) */}
            <div className="space-y-2">
              <label className="text-label uppercase text-text-muted">{t('devices.edit.name')}</label>
              <Input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={device.name}
                disabled={saving}
                maxLength={64}
                className="h-9 rounded-lg border-border bg-elevated text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent/50"
              />
              <p className="text-caption text-text-muted">{t('devices.edit.nameHint')}</p>
            </div>

            {/* Tipo (#797) */}
            <div className="space-y-2">
              <label className="text-label uppercase text-text-muted">{t('devices.edit.type')}</label>
              <Select value={devType || 'auto'} onValueChange={(v) => setDevType(v === 'auto' ? '' : v)} disabled={saving}>
                <SelectTrigger className="h-9 rounded-lg border-border bg-elevated text-sm text-text-primary">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="auto">{t('devices.edit.auto')}</SelectItem>
                  {DEVICE_TYPES.map((ty) => (
                    <SelectItem key={ty} value={ty}>{t(`devices.types.${ty}`)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
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

            {/* #1096: Save/Cancel junto a la zona que editan (nombre, tipo,
                icono); abajo quedaban lejos, tras Reserva/Bloqueo, que son
                acciones independientes con sus propios botones. */}
            <div className="flex gap-3">
              <Button variant="outline" className="flex-1" onClick={onClose} disabled={saving}>
                {t('common.cancel')}
              </Button>
              <Button className="flex-1" onClick={handleSave} disabled={saving}>
                {saving ? t('common.loading') : t('devices.edit.save')}
              </Button>
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
                    const path = `/api/devices/${encodeURIComponent(device.mac)}/reservation`
                    const body = { ip: reserveDraft, hostname: asDhcpHostname(device.name) }
                    const applyReserve = async () => {
                      const res = await fetchJson(path, {
                        method: 'PUT',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(body),
                      })
                      if (res.ok) {
                        setReservation({ reserved: true, ip: reserveDraft, loading: false })
                      } else {
                        setReservation((p) => ({ ...p, loading: false }))
                      }
                    }
                    // #754: primero el plan de comandos; se aplica al confirmar.
                    const planned = await openPlan(path, body, applyReserve)
                    if (!planned) {
                      await applyReserve()
                    } else {
                      setReservation((p) => ({ ...p, loading: false }))
                    }
                  }}
                >
                  {t('devices.edit.saveReserve')}
                </Button>
              </div>
              {/* #800: aplicar el nombre visible como hostname de la reserva */}
              <div className="space-y-1.5">
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full"
                  disabled={reservation.loading || !appliedHostname}
                  onClick={async () => {
                    if (!device || !appliedHostname) return
                    setReservation((p) => ({ ...p, loading: true }))
                    const path = `/api/devices/${encodeURIComponent(device.mac)}/reservation-hostname`
                    const body = { hostname: appliedHostname, ip: reserveDraft || device.ip }
                    const applyName = async () => {
                      const res = await fetchJson<{ ip?: string }>(path, {
                        method: 'PUT',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(body),
                      })
                      if (res.ok) {
                        setReservation({ reserved: true, ip: res.data?.ip ?? body.ip, loading: false })
                      } else {
                        setReservation((p) => ({ ...p, loading: false }))
                      }
                    }
                    // #754: primero el plan de comandos; se aplica al confirmar.
                    const planned = await openPlan(path, body, applyName)
                    if (!planned) {
                      await applyName()
                    } else {
                      setReservation((p) => ({ ...p, loading: false }))
                    }
                  }}
                >
                  {t('devices.edit.applyName')}
                </Button>
                <p className="text-caption text-text-muted">
                  {appliedHostname ? t('devices.edit.applyNameHint') : t('devices.edit.applyNameInvalid')}
                </p>
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
                    const url = `/api/devices/${encodeURIComponent(device.mac)}/block`
                    const body = { router: device.routerId }
                    if (block.blocked) {
                      // Desbloquear (DELETE): quitar la regla no necesita plan.
                      setBlock((p) => ({ ...p, loading: true }))
                      const res = await fetchJson(url, {
                        method: 'DELETE',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(body),
                      })
                      if (res.ok) {
                        setBlock({ blocked: false, loading: false })
                      } else {
                        setBlock((p) => ({ ...p, loading: false }))
                      }
                      return
                    }
                    // Bloquear (#754): plan de comandos primero, aplicar al confirmar.
                    setBlock((p) => ({ ...p, loading: true }))
                    const applyBlock = async () => {
                      const res = await fetchJson(url, {
                        method: 'PUT',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(body),
                      })
                      if (res.ok) {
                        setBlock({ blocked: true, loading: false })
                      } else {
                        setBlock((p) => ({ ...p, loading: false }))
                      }
                    }
                    const planned = await openPlan(url, body, applyBlock)
                    if (!planned) {
                      await applyBlock()
                    } else {
                      setBlock((p) => ({ ...p, loading: false }))
                    }
                  }}
                >
                  {block.blocked ? t('devices.edit.unblock') : t('devices.edit.block')}
                </Button>
              </div>
            </div>

            {/* Plan de comandos (#754): nada se aplica hasta confirmar aquí */}
            {plan && (
              <div className="space-y-2 rounded-xl border border-warn/40 bg-warn/5 p-3" role="dialog" aria-label={t('devices.edit.planTitle', { host: plan.host })}>
                <div className="text-label uppercase tracking-wide text-warn">
                  {t('devices.edit.planTitle', { host: plan.host || device?.routerId || '' })}
                </div>
                <pre className="overflow-x-auto rounded-lg border border-border bg-canvas p-2 font-mono text-caption leading-relaxed text-text-primary">
                  {plan.apply.length ? plan.apply.join('\n') : t('devices.edit.planNone')}
                </pre>
                {plan.rollback.length > 0 && (
                  <details className="text-caption text-text-muted">
                    <summary className="cursor-pointer select-none">{t('devices.edit.planRollback')}</summary>
                    <pre className="mt-1 overflow-x-auto rounded-lg border border-border bg-canvas p-2 font-mono text-caption leading-relaxed">
                      {plan.rollback.join('\n')}
                    </pre>
                  </details>
                )}
                <div className="flex items-center gap-2">
                  <Button
                    size="sm"
                    disabled={planBusy}
                    onClick={async () => {
                      setPlanBusy(true)
                      try {
                        await planRun?.()
                      } finally {
                        setPlanBusy(false)
                        closePlan()
                      }
                    }}
                  >
                    {planBusy ? t('common.loading') : t('devices.edit.planRun')}
                  </Button>
                  <Button size="sm" variant="outline" disabled={planBusy} onClick={closePlan}>
                    {t('common.cancel')}
                  </Button>
                </div>
              </div>
            )}

            {/* #1151: MACs enlazadas (mismo dispositivo, varias MACs) */}
            {!isDemo && device?.mac && (
              <div className="rounded-xl border border-border bg-elevated/40 p-3">
                <div className="text-label uppercase tracking-wide text-text-muted">{t('devices.edit.linkedMacs')}</div>
                {(device.aliasMacs?.length ?? 0) > 0 && (
                  <ul className="mb-2 mt-1.5 space-y-1">
                    {device.aliasMacs?.map((alias) => (
                      <li key={alias} className="flex items-center justify-between gap-2 rounded-lg bg-canvas/60 px-2 py-1.5">
                        <span className="font-mono text-mono-sm text-text-primary">{alias}</span>
                        <button
                          type="button"
                          disabled={linkBusy}
                          onClick={() => void unlinkAlias(alias)}
                          className="rounded-md px-2 py-0.5 text-caption text-text-secondary transition-colors hover:bg-hover hover:text-text-primary"
                        >
                          {t('devices.edit.unlink')}
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
                {otherClients.length > 0 && (
                  <div className="mt-2 flex items-center gap-2">
                    <Select value={linkTarget} onValueChange={setLinkTarget}>
                      <SelectTrigger className="h-9 flex-1">
                        <SelectValue placeholder={t('devices.edit.linkPlaceholder')} />
                      </SelectTrigger>
                      <SelectContent>
                        {otherClients.map((c) => (
                          <SelectItem key={c.mac} value={c.mac ?? ''}>
                            {c.name || c.mac}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Button size="sm" variant="outline" disabled={!linkTarget || linkBusy} onClick={() => void linkClient()}>
                      {linkBusy ? t('common.loading') : t('devices.edit.link')}
                    </Button>
                  </div>
                )}
              </div>
            )}

            {isDemo && (
              <p className="rounded-lg border border-warn/30 bg-warn/10 p-3 text-caption text-warn">
                {t('devices.edit.demoNotice')}
              </p>
            )}

            {/* #1145: borrado del cliente del registro. Un cliente borrado
                solo reaparece si se le vuelve a ver en vivo. #1205: el botón
                solo tiene sentido para clientes offline (los online los
                re-detecta el siguiente ciclo del poller y parece que "no
                borra"): para ellos se sugiere Bloquear. */}
            {!isDemo && device?.mac && device.online === false && (
              <div className="rounded-xl border border-danger/30 bg-danger/5 p-3">
                <div className="text-label uppercase tracking-wide text-danger">{t('devices.edit.dangerZone')}</div>
                <p className="mb-2 mt-1 text-caption text-text-muted">{t('devices.edit.deleteDesc')}</p>
                <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)} disabled={deleting}>
                  {t('devices.edit.delete')}
                </Button>
              </div>
            )}
            {!isDemo && device?.mac && device.online && (
              <div className="rounded-xl border border-border bg-elevated p-3">
                <div className="text-label uppercase tracking-wide text-text-muted">{t('devices.edit.dangerZone')}</div>
                <p className="mt-1 text-caption text-text-muted">{t('devices.edit.deleteOnlineHint')}</p>
              </div>
            )}

          </div>
        )}
        <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
          {/* #1205: el aviso lleva el mismo tinte rojo que la zona que lo abre */}
          <AlertDialogContent className="border-danger/40 bg-surface">
            <AlertDialogHeader>
              <AlertDialogTitle>{t('devices.edit.deleteTitle')}</AlertDialogTitle>
              <AlertDialogDescription>
                {t('devices.edit.deleteConfirmDesc', { name: device?.name || device?.mac || '' })}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={deleting}>{t('common.cancel')}</AlertDialogCancel>
              <AlertDialogAction
                disabled={deleting}
                className="bg-destructive text-white hover:bg-destructive/90 focus-visible:ring-destructive/20"
                onClick={async (e) => {
                  e.preventDefault()
                  if (!device?.mac) return
                  setDeleting(true)
                  try {
                    await fetchJson(`/api/devices/${encodeURIComponent(device.mac)}`, { method: 'DELETE' })
                    // #1205: refresco inmediato - si no, la fila seguiría en
                    // pantalla hasta el próximo ciclo y parecería que no borra
                    void refresh()
                    setDeleteOpen(false)
                    onDeleted?.()
                    onClose()
                  } catch {
                    // el fallo deja la hoja abierta; el botón vuelve a habilitarse
                  } finally {
                    setDeleting(false)
                  }
                }}
              >
                {deleting ? t('common.loading') : t('devices.edit.delete')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </SheetContent>
    </Sheet>
  )
}
