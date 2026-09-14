import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { api, type User } from '../api/client'

interface AuthState {
  user: User | null
  loading: boolean
  login: (username: string, password: string, totpCode?: string) => Promise<void>
  logout: () => Promise<void>
  refresh: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    try {
      setUser(await api.me())
    } catch {
      setUser(null)
    }
  }, [])

  useEffect(() => {
    // La cookie de sesión (HttpOnly) ya viaja sola con la petición: si hay
    // una sesión válida de una visita anterior, /users/me la reconoce sin
    // que la SPA tenga que guardar ni leer ningún token.
    refresh().finally(() => setLoading(false))
  }, [refresh])

  const login = useCallback(async (username: string, password: string, totpCode?: string) => {
    const res = await api.login(username, password, totpCode)
    setUser(res.user)
  }, [])

  const logout = useCallback(async () => {
    await api.logout().catch(() => {
      // Si la petición falla (p.ej. la sesión ya había expirado), igualmente
      // limpiamos el estado local: el usuario ya no debe verse autenticado.
    })
    setUser(null)
  }, [])

  return <AuthContext.Provider value={{ user, loading, login, logout, refresh }}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth debe usarse dentro de <AuthProvider>')
  return ctx
}
