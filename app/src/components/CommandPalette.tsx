import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'
import { Bell, LayoutDashboard, MonitorSmartphone, Router as RouterIcon, Settings, Waypoints } from 'lucide-react'
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from '@/components/ui/command'
import { useNetPulse } from '@/data/DataProvider'

const PAGES = [
  { to: '/', labelKey: 'nav.overview', icon: LayoutDashboard },
  { to: '/routers', labelKey: 'nav.routers', icon: RouterIcon },
  { to: '/devices', labelKey: 'nav.devices', icon: MonitorSmartphone },
  { to: '/topology', labelKey: 'nav.topology', icon: Waypoints },
  { to: '/alerts', labelKey: 'nav.alerts', icon: Bell },
  { to: '/settings', labelKey: 'nav.settings', icon: Settings },
] as const

/** CommandPalette ⌘K (fase mockup: navegación entre páginas y routers). */
export function CommandPalette() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const navigate = useNavigate()
  const { routers } = useNetPulse()

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setOpen((o) => !o)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const go = (to: string) => {
    setOpen(false)
    navigate(to)
  }

  return (
    <CommandDialog open={open} onOpenChange={setOpen}>
      <CommandInput placeholder={t('command.placeholder')} />
      <CommandList>
        <CommandEmpty>{t('command.empty')}</CommandEmpty>
        <CommandGroup heading={t('command.pages')}>
          {PAGES.map((p) => (
            <CommandItem key={p.to} onSelect={() => go(p.to)}>
              <p.icon className="mr-2 h-4 w-4" strokeWidth={1.75} />
              {t(p.labelKey)}
            </CommandItem>
          ))}
        </CommandGroup>
        <CommandSeparator />
        <CommandGroup heading={t('command.routers')}>
          {routers.map((r) => (
            <CommandItem key={r.id} onSelect={() => go(`/routers/${r.id}`)}>
              <RouterIcon className="mr-2 h-4 w-4" strokeWidth={1.75} />
              {r.name}
              <span className="ml-2 font-mono text-caption text-text-muted">{r.ip}</span>
            </CommandItem>
          ))}
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  )
}
