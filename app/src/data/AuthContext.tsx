/**
 * Contexto del usuario autenticado (username + role) — lo rellena AuthGate
 * tras `GET /api/auth/me`. En modo demo (sin backend) user es null.
 */
import { createContext, useContext } from 'react'

export interface AuthUser {
  user: string
  role: 'admin' | 'user'
  /** 'auto' (navegador) | 'es' | 'en' — fuente de verdad: users.language */
  language?: 'auto' | 'es' | 'en'
}

const AuthContext = createContext<AuthUser | null>(null)

export function AuthProvider({ auth, children }: { auth: AuthUser | null; children: React.ReactNode }) {
  return <AuthContext.Provider value={auth}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthUser | null {
  return useContext(AuthContext)
}
