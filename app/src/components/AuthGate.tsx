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
import { Navigate } from 'react-router-dom'
import { motion } from 'framer-motion'

type GateState = 'loading' | 'authed' | 'login' | 'demo'

async function checkSession(): Promise<GateState> {
  try {
    const res = await fetch('/api/auth/me', { signal: AbortSignal.timeout(2500) })
    if (res.status === 401) return 'login'
    if (!res.ok) return 'demo'
    // La preview estática responde HTML con 200 (SPA fallback): no es sesión
    if (!(res.headers.get('content-type') ?? '').includes('application/json')) return 'demo'
    const data = (await res.json()) as { user?: string }
    return data?.user ? 'authed' : 'demo'
  } catch {
    return 'demo'
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

  useEffect(() => {
    let cancelled = false
    void checkSession().then((s) => {
      if (!cancelled) setState(s)
    })
    return () => {
      cancelled = true
    }
  }, [])

  if (state === 'loading') return <GateSkeleton />
  if (state === 'login') return <Navigate to="/login" replace />
  return <>{children}</>
}
