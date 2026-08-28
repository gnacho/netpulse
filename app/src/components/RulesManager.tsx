import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { motion } from 'framer-motion'
import {
  AlertTriangle,
  Check,
  Globe,
  Info,
  OctagonX,
  Plus,
  Router,
  Server,
  Shield,
  Signal,
  Trash2,
  Users,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { cn } from '@/lib/utils'

interface AlertRule {
  id: string
  name: string
  category: string
  enabled: boolean
  condition: {
    metric: string
    operator: string
    threshold: number
    duration: number
  }
  scope: {
    type: 'global' | 'router'
    routerIds?: string[]
  }
  severity: 'info' | 'warn' | 'critical'
}

const CAT_ICONS: Record<string, { icon: LucideIcon; color: string }> = {
  router: { icon: Router, color: 'text-warn' },
  internet: { icon: Globe, color: 'text-info' },
  clients: { icon: Users, color: 'text-accent' },
  signal: { icon: Signal, color: 'text-tunnel' },
  vpn: { icon: Shield, color: 'text-tunnel' },
  system: { icon: Server, color: 'text-ok' },
}

const SEV_COLORS: Record<string, string> = {
  info: 'bg-info/10 text-info',
  warn: 'bg-warn/10 text-warn',
  critical: 'bg-danger/10 text-danger',
}

const METRICS = [
  'cpu', 'ram', 'temp', 'latency_ms', 'rx_bps', 'tx_bps',
] as const

const OPERATORS = [
  { value: 'gt', label: '>' },
  { value: 'gte', label: '>=' },
  { value: 'lt', label: '<' },
  { value: 'lte', label: '<=' },
  { value: 'eq', label: '=' },
] as const

const DURATIONS = [
  { value: 60_000_000_000, labelKey: 'alertRules.duration1m' },
  { value: 300_000_000_000, labelKey: 'alertRules.duration5m' },
  { value: 600_000_000_000, labelKey: 'alertRules.duration10m' },
  { value: 1_800_000_000_000, labelKey: 'alertRules.duration30m' },
  { value: 3_600_000_000_000, labelKey: 'alertRules.duration1h' },
] as const

const CATEGORIES = ['router', 'internet', 'clients', 'signal', 'vpn', 'system'] as const
const SEVERITIES = ['info', 'warn', 'critical'] as const

function emptyRule(): Omit<AlertRule, 'id' | 'createdAt' | 'updatedAt'> {
  return {
    name: '',
    category: 'router',
    enabled: true,
    condition: { metric: 'cpu', operator: 'gt', threshold: 90, duration: 600_000_000_000 },
    scope: { type: 'global' },
    severity: 'warn',
  }
}

export function RulesManager() {
  const { t } = useTranslation()
  const [rules, setRules] = useState<AlertRule[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<Omit<AlertRule, 'id' | 'createdAt' | 'updatedAt'> | null>(null)
  const [editId, setEditId] = useState<string | null>(null)

  const fetchRules = useCallback(async () => {
    try {
      const res = await fetch('/api/alert-rules')
      if (res.ok) setRules(await res.json())
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void fetchRules() }, [fetchRules])

  const saveRule = async () => {
    if (!editing) return
    const body = JSON.stringify(editing)
    if (editId) {
      await fetch(`/api/alert-rules/${editId}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body })
    } else {
      await fetch('/api/alert-rules', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body })
    }
    setEditing(null)
    setEditId(null)
    await fetchRules()
  }

  const deleteRule = async (id: string) => {
    await fetch(`/api/alert-rules/${id}`, { method: 'DELETE' })
    await fetchRules()
  }

  const toggleRule = async (rule: AlertRule) => {
    await fetch(`/api/alert-rules/${rule.id}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...rule, enabled: !rule.enabled }),
    })
    await fetchRules()
  }

  const startEdit = (rule: AlertRule) => {
    setEditing({ name: rule.name, category: rule.category, enabled: rule.enabled, condition: rule.condition, scope: rule.scope, severity: rule.severity })
    setEditId(rule.id)
  }

  if (loading) return <p className="text-caption text-text-muted">{t('common.loading')}</p>

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-semibold text-text-primary">{t('alertRules.title')}</h3>
          <p className="mt-0.5 text-caption text-text-muted">{t('alertRules.description')}</p>
        </div>
        {!editing && (
          <Button
            size="sm"
            variant="outline"
            onClick={() => { setEditing(emptyRule()); setEditId(null) }}
            className="gap-1.5"
          >
            <Plus className="h-3.5 w-3.5" />
            {t('alertRules.create')}
          </Button>
        )}
      </div>

      {rules.length === 0 && !editing && (
        <p className="py-4 text-center text-caption text-text-muted">{t('alertRules.empty')}</p>
      )}

      {rules.map((rule) => {
        const meta = CAT_ICONS[rule.category] ?? { icon: Info, color: 'text-text-muted' }
        const CatIcon = meta.icon
        return (
          <div key={rule.id} className="flex items-center gap-3 rounded-xl border border-border bg-elevated p-3">
            <span className={cn('flex h-9 w-9 shrink-0 items-center justify-center rounded-lg', SEV_COLORS[rule.severity])}>
              <CatIcon className="h-4 w-4" strokeWidth={1.75} />
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="truncate text-sm font-medium text-text-primary">{rule.name}</span>
                <span className={cn('rounded-md px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-[0.05em]', SEV_COLORS[rule.severity])}>
                  {rule.severity}
                </span>
              </div>
              <p className="mt-0.5 truncate text-caption text-text-muted">
                {rule.condition.metric} {OPERATORS.find(o => o.value === rule.condition.operator)?.label} {rule.condition.threshold}
                {' / '}
                {rule.condition.duration / 1_000_000_000}s
                {' / '}
                {rule.scope.type === 'global' ? t('alertRules.scopeGlobal') : t('alertRules.scopeRouter')}
              </p>
            </div>
            <Switch checked={rule.enabled} onCheckedChange={() => toggleRule(rule)} />
            <Button size="icon" variant="ghost" onClick={() => startEdit(rule)} className="h-8 w-8 text-text-muted hover:text-accent">
              <Info className="h-3.5 w-3.5" />
            </Button>
            <Button size="icon" variant="ghost" onClick={() => deleteRule(rule.id)} className="h-8 w-8 text-text-muted hover:text-danger">
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        )
      })}

      {editing && (
        <motion.div
          initial={{ opacity: 0, y: -8 }}
          animate={{ opacity: 1, y: 0 }}
          className="rounded-xl border border-accent/30 bg-accent-soft/20 p-4"
        >
          <h4 className="mb-3 text-sm font-semibold text-text-primary">
            {editId ? t('alertRules.edit') : t('alertRules.create')}
          </h4>
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <label className="mb-1 block text-caption font-medium text-text-secondary">{t('alertRules.name')}</label>
              <Input
                value={editing.name}
                onChange={(e) => setEditing({ ...editing, name: e.target.value })}
                placeholder={t('alertRules.namePlaceholder')}
                className="h-8 text-sm"
              />
            </div>
            <div>
              <label className="mb-1 block text-caption font-medium text-text-secondary">{t('alertRules.category')}</label>
              <Select value={editing.category} onValueChange={(v) => setEditing({ ...editing, category: v })}>
                <SelectTrigger size="sm" className="h-8">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {CATEGORIES.map((c) => (
                    <SelectItem key={c} value={c}>{t(`alerts.categories.${c}`)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div>
              <label className="mb-1 block text-caption font-medium text-text-secondary">{t('alertRules.metric')}</label>
              <Select value={editing.condition.metric} onValueChange={(v) => setEditing({ ...editing, condition: { ...editing.condition, metric: v } })}>
                <SelectTrigger size="sm" className="h-8">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {METRICS.map((m) => (
                    <SelectItem key={m} value={m}>{t(`alertRules.metrics.${m}`)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex gap-2">
              <div className="flex-1">
                <label className="mb-1 block text-caption font-medium text-text-secondary">{t('alertRules.operator')}</label>
                <Select value={editing.condition.operator} onValueChange={(v) => setEditing({ ...editing, condition: { ...editing.condition, operator: v } })}>
                  <SelectTrigger size="sm" className="h-8">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {OPERATORS.map((o) => (
                      <SelectItem key={o.value} value={o.value}>{o.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="flex-1">
                <label className="mb-1 block text-caption font-medium text-text-secondary">{t('alertRules.threshold')}</label>
                <Input
                  type="number"
                  value={editing.condition.threshold}
                  onChange={(e) => setEditing({ ...editing, condition: { ...editing.condition, threshold: Number(e.target.value) } })}
                  className="h-8 text-sm"
                />
              </div>
            </div>
            <div>
              <label className="mb-1 block text-caption font-medium text-text-secondary">{t('alertRules.duration')}</label>
              <Select
                value={String(editing.condition.duration)}
                onValueChange={(v) => setEditing({ ...editing, condition: { ...editing.condition, duration: Number(v) } })}
              >
                <SelectTrigger size="sm" className="h-8">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {DURATIONS.map((d) => (
                    <SelectItem key={d.value} value={String(d.value)}>{t(d.labelKey)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div>
              <label className="mb-1 block text-caption font-medium text-text-secondary">{t('alertRules.severity')}</label>
              <Select value={editing.severity} onValueChange={(v) => setEditing({ ...editing, severity: v })}>
                <SelectTrigger size="sm" className="h-8">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {SEVERITIES.map((s) => (
                    <SelectItem key={s} value={s}>{t(`alertRules.sev.${s}`)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div>
              <label className="mb-1 block text-caption font-medium text-text-secondary">{t('alertRules.scope')}</label>
              <Select
                value={editing.scope.type}
                onValueChange={(v) => setEditing({ ...editing, scope: { type: v as 'global' | 'router', routerIds: editing.scope.routerIds } })}
              >
                <SelectTrigger size="sm" className="h-8">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="global">{t('alertRules.scopeGlobal')}</SelectItem>
                  <SelectItem value="router">{t('alertRules.scopeRouter')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="mt-4 flex justify-end gap-2">
            <Button
              size="sm"
              variant="ghost"
              onClick={() => { setEditing(null); setEditId(null) }}
            >
              {t('common.cancel')}
            </Button>
            <Button size="sm" onClick={saveRule} disabled={!editing.name} className="gap-1.5">
              <Check className="h-3.5 w-3.5" />
              {editId ? t('alertRules.update') : t('alertRules.create')}
            </Button>
          </div>
        </motion.div>
      )}
    </div>
  )
}
