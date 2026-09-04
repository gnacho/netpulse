// updateErrors.ts — traduce los códigos de error del updater a texto
// humano (#512): "update_exit_-1" no le dice nada a nadie. El código crudo
// se sigue mostrando como detalle pequeño para depuración.
import type { TFunction } from 'i18next'

export function updateErrorText(t: TFunction, code: string | null | undefined): string {
  if (!code) return ''
  if (code === 'update_exit_-1') return t('update.errors.exitRestart')
  const m = /^update_exit_(-?\d+)$/.exec(code)
  if (m) return t('update.errors.exitCode', { code: m[1] })
  if (code === 'stable_no_target') return t('update.errors.noTarget')
  if (code === 'no_token') return t('update.errors.noToken')
  return t('update.errors.generic', { code })
}
