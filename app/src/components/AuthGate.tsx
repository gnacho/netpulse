/**
 * NetPulse — AuthGate (webapp-stack §AuthGate + Login).
 *
 * - Backend presente: `GET /api/auth/me` → 401 ⇒ redirect a `/login`;
 *   200 ⇒ render de la app.
 * - Sin backend (modo demo local): pasa directo — la preview estática sigue
 *   funcionando sin login.
 * - Mientras resuelve: skeleton de carga acorde al sistema visual.
 */
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import i18n from '@/i18n'
import { Navigate } from 'react-router'
import { motion } from 'framer-motion'
import { AuthProvider } from '@/data/AuthContext'
import type { AuthUser } from '@/data/AuthContext'

type GateState = 'loading' | 'authed' | 'login' | 'demo'

async function checkSession(): Promise<{ state: GateState; auth: AuthUser | null }> {
  // Modo demo local (botón "Entrar como demo" del login): sin backend ni sesión
  if (sessionStorage.getItem('netpulse-demo') === '1') return { state: 'demo', auth: null }
  try {
    const res = await fetch('/api/auth/me', { signal: AbortSignal.timeout(2500) })
    if (res.status === 401) return { state: 'login', auth: null }
    if (!res.ok) return { state: 'demo', auth: null }
    // La preview estática responde HTML con 200 (SPA fallback): no es sesión
    if (!(res.headers.get('content-type') ?? '').includes('application/json')) return { state: 'demo', auth: null }
    const data = (await res.json()) as {
      user?: string
      role?: 'admin' | 'user'
      language?: 'auto' | 'es' | 'en'
      displayName?: string
    }
    if (!data?.user) return { state: 'demo', auth: null }
    return {
      state: 'authed',
      auth: { user: data.user, role: data.role ?? 'user', language: data.language ?? 'auto', displayName: data.displayName ?? '' },
    }
  } catch {
    return { state: 'demo', auth: null }
  }
}

function GateSkeleton() {
  const { t } = useTranslation()
  return (
    <div className="flex min-h-[100dvh] items-center justify-center bg-canvas" role="status" aria-label={t('auth.checking')}>
      <motion.div
        initial={{ opacity: 0, scale: 0.9 }}
        animate={{ opacity: 1, scale: 1 }}
        transition={{ duration: 0.3, ease: 'easeOut' }}
        className="flex flex-col items-center gap-4"
      >
        <span className="flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br from-accent/20 to-tunnel/20 ring-1 ring-accent/30">
          <img src="/logo.svg" alt="" className="h-9 w-9" />
        </span>
        <span className="flex items-center gap-2 font-mono text-caption text-text-muted">
          <span className="relative flex h-2 w-2">
            <span className="absolute inline-flex h-full w-full rounded-full bg-accent opacity-75 animate-ping-soft" />
            <span className="relative inline-flex h-2 w-2 rounded-full bg-accent" />
          </span>
          {t('auth.connecting')}
        </span>
      </motion.div>
    </div>
  )
}

export function AuthGate({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<GateState>('loading')
  const [auth, setAuth] = useState<AuthUser | null>(null)

  useEffect(() => {
    let cancelled = false
    void checkSession().then(({ state: s, auth: a }) => {
      if (cancelled) return
      setState(s)
      setAuth(a)
      if (s === 'authed') window.dispatchEvent(new Event('netpulse-authed'))
      // Idioma por usuario (BD) sobre la autodetección del navegador
      if (a?.language && a.language !== 'auto') {
        void i18n.changeLanguage(a.language)
        localStorage.setItem('i18nextLng', a.language)
      }
    })
    return () => {
      cancelled = true
    }
  }, [])

  // Refetch bajo demanda (SPEC-65 D65-5): tras cambiar displayName/idioma, la
  // página emite 'netpulse-auth-refresh' y el contexto se actualiza sin reload.
  useEffect(() => {
    const onRefresh = () => {
      void checkSession().then(({ state: s, auth: a }) => {
        if (s === 'authed') setAuth(a)
      })
    }
    window.addEventListener('netpulse-auth-refresh', onRefresh)
    return () => window.removeEventListener('netpulse-auth-refresh', onRefresh)
  }, [])

  if (state === 'loading') return <GateSkeleton />
  if (state === 'login') return <Navigate to="/login" replace />
  return <AuthProvider auth={auth}>{children}</AuthProvider>
}
