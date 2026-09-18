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

export interface OnboardingIntakeProps {
  open: boolean
  device: ClientDevice | null
  isDemo: boolean
  onClose: () => void
  onSaved: () => void
}

// Alta guiada de un dispositivo desconocido (#772): nombre (known_macs, que
// además silencia la alerta first-seen), icono (device_overrides) y reserva
// DHCP opcional (mismo endpoint y plan dry-run que DeviceEditSheet, #754).
// "Dejar como anónimo" llama a /api/onboarding/dismiss: silencia sin nombrar.
export function OnboardingIntake({ open, device, isDemo, onClose, onSaved }: OnboardingIntakeProps) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [icon, setIcon] = useState('')
  const [reserve, setReserve] = useState(false)
  const [ip, setIp] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [plan, setPlan] = useState<{ host: string; apply: string[]; rollback: string[] } | null>(null)
  const [planRun, setPlanRun] = useState<null | (() => Promise<void>)>(null)
  const [planBusy, setPlanBusy] = useState(false)

  useEffect(() => {
    setName(device && device.name !== device.mac ? device.name : '')
    setIcon(device?.iconOverride ?? '')
    setReserve(false)
    setIp(device?.ip ?? '')
    setError(null)
    setPlan(null)
    setPlanRun(null)
  }, [device])

  if (!device) return null

  const PreviewIcon = ((icon ? ICON_OVERRIDES[icon] : null) ?? ICON_OVERRIDES['help-circle']) as LucideIcon
  const nameless = device.name === device.mac

  const applyReservation = async (): Promise<boolean> => {
    const res = await fetchJson(`/api/devices/${encodeURIComponent(device.mac)}/reservation`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ip }),
    })
    return res.ok
  }

  const handleIdentify = async () => {
    const trimmed = name.trim()
    if (!trimmed) {
      setError(t('devices.onboarding.errorName'))
      return
    }
    setBusy(true)
    setError(null)
    try {
      if (isDemo) {
        onSaved()
        onClose()
        return
      }
      // 1) Nombre de confianza: known_macs (alias + silencia la alerta).
      const km = await fetchJson('/api/settings/known-macs', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mac: device.mac, name: trimmed }),
      })
      if (!km.ok) {
        setError(t('devices.onboarding.saveError'))
        return
      }
      // 2) Icono explícito (opcional).
      if (icon) {
        await fetchJson(`/api/devices/${encodeURIComponent(device.mac)}/override`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ icon }),
        })
      }
      // 3) Reserva DHCP opcional, con plan dry-run (#754) si aplica.
      if (reserve && ip) {
        const path = `/api/devices/${encodeURIComponent(device.mac)}/reservation`
        const body = { ip }
        const sep = '?'
        const planned = await fetchJson<{ host?: string; apply?: string[]; rollback?: string[] }>(path + sep + 'dry_run=1', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        })
        if (planned.ok && planned.data) {
          setPlan({ host: planned.data.host ?? '', apply: planned.data.apply ?? [], rollback: planned.data.rollback ?? [] })
          setPlanRun(() => async () => { await applyReservation() })
          return // el plan se confirma en el panel; onSaved va tras planRun
        }
        if (!(await applyReservation())) {
          setError(t('devices.onboarding.reserveError'))
          return
        }
      }
      onSaved()
      onClose()
    } finally {
      setBusy(false)
    }
  }

  const handleDismiss = async () => {
    setBusy(true)
    setError(null)
    try {
      if (!isDemo) {
        const res = await fetchJson('/api/onboarding/dismiss', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ mac: device.mac }),
        })
        if (!res.ok) {
          setError(t('devices.onboarding.saveError'))
          return
        }
      }
      onSaved()
      onClose()
    } finally {
      setBusy(false)
    }
  }

  return (
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent side="right" className="w-full sm:max-w-md">
        <div className="flex flex-col gap-5 overflow-y-auto px-4 py-2">
          <div>
            <h2 className="text-lg font-semibold text-text-primary">{t('devices.onboarding.title')}</h2>
            <p className="mt-1 text-caption text-text-muted">{t('devices.onboarding.subtitle')}</p>
          </div>

          {/* Vista previa */}
          <div className="flex items-center gap-3 rounded-xl border border-border bg-elevated/50 p-3">
            <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-elevated text-text-secondary">
              <PreviewIcon className="h-6 w-6" strokeWidth={1.75} />
            </div>
            <div className="min-w-0">
              <div className="truncate text-sm font-medium text-text-primary">
                {nameless ? t('devices.onboarding.unknownName') : device.name}
              </div>
              <div className="truncate font-mono text-caption text-text-muted">{device.mac}</div>
              <div className="truncate text-caption text-text-muted">
                {[device.band !== 'cable' ? device.band : null, device.signalDbm != null ? `${device.signalDbm} dBm` : null]
                  .filter(Boolean)
                  .join(' · ')}
              </div>
            </div>
          </div>

          {/* Nombre */}
          <div className="space-y-2">
            <label className="text-label uppercase text-text-muted" htmlFor="onboarding-name">
              {t('devices.onboarding.nameLabel')}
            </label>
            <Input
              id="onboarding-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('devices.onboarding.namePlaceholder')}
              disabled={busy}
              className="h-9 rounded-lg border-border bg-elevated text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent/50"
            />
          </div>

          {/* Icono */}
          <div className="space-y-2">
            <label className="text-label uppercase text-text-muted">{t('devices.onboarding.iconLabel')}</label>
            <div className="grid grid-cols-6 gap-2">
              <button
                type="button"
                onClick={() => setIcon('')}
                disabled={busy}
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
                    disabled={busy}
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

          {/* Reserva DHCP opcional */}
          <div className="space-y-2 rounded-xl border border-border bg-elevated/40 p-3">
            <label className="flex items-center justify-between gap-2">
              <span className="text-label uppercase text-text-muted">{t('devices.onboarding.reserve')}</span>
              <input
                type="checkbox"
                checked={reserve}
                onChange={(e) => setReserve(e.target.checked)}
                disabled={busy}
                className="h-4 w-4 accent-accent"
              />
            </label>
            {reserve && (
              <div className="flex items-center gap-2">
                <Input
                  value={ip}
                  onChange={(e) => setIp(e.target.value)}
                  placeholder={device.ip}
                  disabled={busy}
                  className="h-9 rounded-lg border-border bg-elevated font-mono text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent/50"
                />
              </div>
            )}
            <p className="text-caption text-text-muted">{t('devices.onboarding.reserveHint')}</p>
          </div>

          {error && (
            <p className="rounded-lg border border-danger/30 bg-danger/10 p-3 text-caption text-danger">{error}</p>
          )}

          {isDemo && (
            <p className="rounded-lg border border-warn/30 bg-warn/10 p-3 text-caption text-warn">
              {t('devices.edit.demoNotice')}
            </p>
          )}

          {/* Plan de comandos (#754) */}
          {plan && (
            <div className="space-y-2 rounded-xl border border-warn/40 bg-warn/5 p-3" role="dialog" aria-label={t('devices.edit.planTitle', { host: plan.host })}>
              <div className="text-label uppercase tracking-wide text-warn">
                {t('devices.edit.planTitle', { host: plan.host || device.routerId || '' })}
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
                      setPlan(null)
                      setPlanRun(null)
                      onSaved()
                      onClose()
                    } finally {
                      setPlanBusy(false)
                    }
                  }}
                >
                  {planBusy ? t('common.loading') : t('devices.edit.planRun')}
                </Button>
                <Button size="sm" variant="outline" disabled={planBusy} onClick={() => { setPlan(null); setPlanRun(null) }}>
                  {t('common.cancel')}
                </Button>
              </div>
            </div>
          )}

          <div className="mt-auto flex flex-col gap-2 pt-2">
            <div className="flex gap-3">
              <Button variant="outline" className="flex-1" onClick={handleDismiss} disabled={busy}>
                {t('devices.onboarding.dismiss')}
              </Button>
              <Button className="flex-1" onClick={handleIdentify} disabled={busy}>
                {busy ? t('common.loading') : t('devices.onboarding.save')}
              </Button>
            </div>
            <p className="text-center text-caption text-text-muted">{t('devices.onboarding.dismissHint')}</p>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}
