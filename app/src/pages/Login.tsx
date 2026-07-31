/**
 * NetPulse — Login `/login` (single-user, solo password).
 * Fiel al sistema visual (design.md): canvas #070B12, card surface #0D1420,
 * logo NetPulse, Space Grotesk, foco accent cyan, entrada con framer-motion.
 * - 204 → redirect `/`
 * - 401 → shake + mensaje de error
 * - 429 → cuenta atrás con `retryAfterSec`
 * - Sin backend (modo demo local) → redirect directo a `/`
 */
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { motion, useReducedMotion } from 'framer-motion'
import { ArrowRight, Lock, ShieldCheck, User } from 'lucide-react'
import { cn } from '@/lib/utils'

type Status = 'idle' | 'checking' | 'submitting' | 'error' | 'locked'

export default function Login() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const reduce = useReducedMotion() ?? false
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [status, setStatus] = useState<Status>('idle')
  const [shakeKey, setShakeKey] = useState(0)
  const [retryLeft, setRetryLeft] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const timerRef = useRef<number | undefined>(undefined)

  // Modo demo (sin backend) o sesión ya activa → directo al dashboard
  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const res = await fetch('/api/health', { signal: AbortSignal.timeout(2000) })
        const isApi = res.ok && (res.headers.get('content-type') ?? '').includes('application/json')
        if (!cancelled && !isApi) navigate('/', { replace: true })
      } catch {
        if (!cancelled) navigate('/', { replace: true })
      }
    })()
    return () => {
      cancelled = true
    }
  }, [navigate])

  // Cuenta atrás del rate-limit (429)
  useEffect(() => {
    if (retryLeft <= 0) return
    timerRef.current = window.setTimeout(() => setRetryLeft((s) => s - 1), 1000)
    return () => window.clearTimeout(timerRef.current)
  }, [retryLeft])

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!username || !password || status === 'submitting' || retryLeft > 0) return
    setStatus('submitting')
    try {
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      })
      if (res.status === 204) {
        window.dispatchEvent(new Event('netpulse-authed'))
        window.location.assign('/')
        return
      }
      if (res.status === 429) {
        const body = (await res.json().catch(() => null)) as { retryAfterSec?: number } | null
        setRetryLeft(Math.max(1, Math.round(body?.retryAfterSec ?? 60)))
        setStatus('locked')
        return
      }
      // 401 (u otro error): shake + mensaje
      setStatus('error')
      setShakeKey((k) => k + 1)
      setPassword('')
      inputRef.current?.focus()
    } catch {
      setStatus('error')
      setShakeKey((k) => k + 1)
    }
  }

  const locked = retryLeft > 0

  return (
    <div className="mesh-bg relative flex min-h-[100dvh] items-center justify-center bg-canvas px-4">
      {/* Halo radial cyan que respira */}
      {!reduce && (
        <motion.div
          aria-hidden
          className="pointer-events-none absolute h-96 w-96 rounded-full bg-accent/[0.07] blur-3xl"
          animate={{ scale: [1, 1.08, 1] }}
          transition={{ duration: 7, repeat: Infinity, ease: 'easeInOut' }}
        />
      )}

      <motion.main
        initial={reduce ? false : { opacity: 0, y: 16 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.4, ease: 'easeOut' }}
        className="relative w-full max-w-sm"
      >
        <div className="rounded-2xl border border-border bg-surface p-6 shadow-lg md:p-8">
          {/* Logo */}
          <div className="flex flex-col items-center gap-3">
            <motion.span
              initial={reduce ? false : { scale: 0.8, opacity: 0 }}
              animate={{ scale: 1, opacity: 1 }}
              transition={{ type: 'spring', stiffness: 320, damping: 20, delay: 0.1 }}
              className="flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br from-accent/20 to-tunnel/20 ring-1 ring-accent/30"
            >
              <img src="/logo.svg" alt="" className="h-9 w-9" />
            </motion.span>
            <div className="text-center">
              <h1 className="font-display text-2xl font-bold tracking-tight text-text-primary">NetPulse</h1>
              <p className="mt-1 text-caption text-text-muted">{t('login.tagline')}</p>
            </div>
          </div>

          {/* Formulario */}
          <motion.form
            key={shakeKey}
            animate={reduce || shakeKey === 0 ? undefined : { x: [0, -9, 9, -6, 6, -3, 3, 0] }}
            transition={{ duration: 0.45, ease: 'easeOut' }}
            onSubmit={(e) => void submit(e)}
            className="mt-6 space-y-4"
          >
            <div>
              <label htmlFor="login-username" className="text-caption font-semibold uppercase tracking-[0.06em] text-text-muted">
                {t('login.username')}
              </label>
              <div className="relative mt-2">
                <User
                  className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-text-muted"
                  strokeWidth={1.75}
                />
                <input
                  id="login-username"
                  ref={inputRef}
                  type="text"
                  autoComplete="username"
                  value={username}
                  onChange={(e) => {
                    setUsername(e.target.value)
                    if (status === 'error') setStatus('idle')
                  }}
                  disabled={locked}
                  placeholder={t('login.usernamePlaceholder')}
                  className={cn(
                    'h-11 w-full rounded-lg border bg-elevated pl-10 pr-3 text-sm text-text-primary',
                    'placeholder:text-text-muted focus:outline-none focus:ring-2',
                    status === 'error'
                      ? 'border-danger/60 focus:border-danger/60 focus:ring-danger/20'
                      : 'border-border focus:border-accent/60 focus:ring-accent/20',
                    locked && 'opacity-50',
                  )}
                />
              </div>
            </div>

            <div>
              <label htmlFor="login-password" className="text-caption font-semibold uppercase tracking-[0.06em] text-text-muted">
                {t('login.password')}
              </label>
              <div className="relative mt-2">
                <Lock
                  className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-text-muted"
                  strokeWidth={1.75}
                />
                <input
                  id="login-password"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => {
                    setPassword(e.target.value)
                    if (status === 'error') setStatus('idle')
                  }}
                  disabled={locked}
                  placeholder="••••••••"
                  className={cn(
                    'h-11 w-full rounded-lg border bg-elevated pl-10 pr-3 text-sm text-text-primary',
                    'placeholder:text-text-muted focus:outline-none focus:ring-2',
                    status === 'error'
                      ? 'border-danger/60 focus:border-danger/60 focus:ring-danger/20'
                      : 'border-border focus:border-accent/60 focus:ring-accent/20',
                    locked && 'opacity-50',
                  )}
                />
              </div>
            </div>

            {/* Mensajes de estado */}
            <div aria-live="polite" className="min-h-5">
              {status === 'error' && !locked && (
                <motion.p
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  className="text-caption font-medium text-danger"
                >
                  {t('login.wrongPassword')}
                </motion.p>
              )}
              {locked && (
                <p className="text-caption font-medium text-warn">
                  {t('login.tooManyAttempts')}{' '}
                  <span className="font-mono font-semibold">{retryLeft} s</span>
                </p>
              )}
            </div>

            <button
              type="submit"
              disabled={!username || !password || status === 'submitting' || locked}
              className={cn(
                'flex h-11 w-full items-center justify-center gap-2 rounded-lg bg-accent text-sm font-semibold text-canvas',
                'transition-colors duration-150 hover:bg-accent/90',
                'disabled:cursor-not-allowed disabled:opacity-40',
              )}
            >
              {status === 'submitting' ? (
                t('login.entering')
              ) : (
                <>
                  {t('login.enter')}
                  <ArrowRight className="h-4 w-4" strokeWidth={1.75} />
                </>
              )}
            </button>
          </motion.form>

          <p className="mt-6 flex items-center justify-center gap-1.5 text-center text-caption text-text-muted">
            <ShieldCheck className="h-3.5 w-3.5 text-ok" strokeWidth={1.75} />
            {t('login.localSession')}
          </p>

          {/* Entrar como demo: dataset simulado local, sin contraseña */}
          <button
            type="button"
            onClick={() => {
              sessionStorage.setItem('netpulse-demo', '1')
              navigate('/', { replace: true })
            }}
            className="mt-3 flex w-full items-center justify-center gap-2 rounded-lg border border-border py-2.5 text-sm font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
          >
            {t('login.demoEnter')}
          </button>
        </div>
      </motion.main>
    </div>
  )
}
