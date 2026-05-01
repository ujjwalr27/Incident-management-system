import { useState, useEffect, ReactNode, useCallback } from 'react'
import { BrowserRouter, Routes, Route, Navigate, Link, useNavigate } from 'react-router-dom'
import { AuthContext, AuthUser } from './hooks/useAuth'
import { ToastProvider } from './components/Toast'
import { login as apiLogin, getMe, setToken, clearToken } from './api/client'
import Login from './pages/Login'
import LiveFeed from './pages/LiveFeed'
import IncidentDetailPage from './pages/IncidentDetail'
import RcaForm from './pages/RcaForm'

function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null)
  const [token, setTokenState] = useState<string | null>(() => localStorage.getItem('access_token'))
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (token) {
      const rt = localStorage.getItem('refresh_token')
      setToken(token, rt ?? undefined)
      getMe().then(me => setUser(me)).catch(() => { clearToken(); setTokenState(null); localStorage.removeItem('access_token') }).finally(() => setLoading(false))
    } else {
      setLoading(false)
    }
  }, [token])

  const login = useCallback(async (email: string, password: string) => {
    const pair = await apiLogin(email, password)
    setToken(pair.access_token, pair.refresh_token)
    setTokenState(pair.access_token)
    localStorage.setItem('access_token', pair.access_token)
    localStorage.setItem('refresh_token', pair.refresh_token)
    const me = await getMe()
    setUser(me)
  }, [])

  const logout = useCallback(() => {
    clearToken()
    setTokenState(null)
    setUser(null)
    localStorage.removeItem('access_token')
  }, [])

  if (loading) return <div className="flex items-center justify-center min-h-screen text-gray-400">Loading…</div>

  return <AuthContext.Provider value={{ user, token, login, logout }}>{children}</AuthContext.Provider>
}

function RequireAuth({ children }: { children: ReactNode }) {
  const token = localStorage.getItem('access_token')
  if (!token) return <Navigate to="/login" replace />
  return <>{children}</>
}

function Layout({ children }: { children: ReactNode }) {
  const navigate = useNavigate()
  function logout() {
    clearToken()
    localStorage.removeItem('access_token')
    navigate('/login')
  }

  return (
    <div className="min-h-screen bg-gray-950">
      <header className="bg-gray-900 border-b border-gray-800 px-6 py-3 flex items-center justify-between">
        <Link to="/" className="flex items-center gap-2">
          <span className="text-xl">🚨</span>
          <span className="font-bold text-white text-sm">IMS</span>
          <span className="text-gray-500 text-xs hidden sm:block">Incident Management System</span>
        </Link>
        <button onClick={logout} className="text-xs text-gray-500 hover:text-gray-300 transition">Sign out</button>
      </header>
      <main className="py-6">{children}</main>
    </div>
  )
}

export default function App() {
  return (
    <ToastProvider>
      <BrowserRouter>
        <AuthProvider>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route path="/" element={<RequireAuth><Layout><LiveFeed /></Layout></RequireAuth>} />
            <Route path="/incidents/:id" element={<RequireAuth><Layout><IncidentDetailPage /></Layout></RequireAuth>} />
            <Route path="/incidents/:id/rca" element={<RequireAuth><Layout><RcaForm /></Layout></RequireAuth>} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </AuthProvider>
      </BrowserRouter>
    </ToastProvider>
  )
}
