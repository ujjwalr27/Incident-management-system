import { createContext, useContext } from 'react'

export interface AuthUser {
  id: string
  email: string
  role: string
}

export interface AuthCtx {
  user: AuthUser | null
  token: string | null
  login: (email: string, password: string) => Promise<void>
  logout: () => void
}

export const AuthContext = createContext<AuthCtx>({
  user: null,
  token: null,
  login: async () => {},
  logout: () => {},
})

export const useAuth = () => useContext(AuthContext)
