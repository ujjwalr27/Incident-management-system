import { useState, FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { useToast } from '../components/Toast'

export default function Login() {
  const { login } = useAuth()
  const { show } = useToast()
  const navigate = useNavigate()
  const [email, setEmail] = useState('responder@ims.local')
  const [password, setPassword] = useState('password123')
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setLoading(true)
    try {
      await login(email, password)
      navigate('/')
    } catch (err) {
      show((err as Error).message, 'error')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-950">
      <div className="bg-gray-900 border border-gray-700 rounded-xl p-8 w-full max-w-sm shadow-2xl">
        <div className="flex items-center gap-3 mb-8">
          <span className="text-3xl">🚨</span>
          <div>
            <h1 className="text-xl font-bold text-white">IMS</h1>
            <p className="text-xs text-gray-400">Incident Management System</p>
          </div>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm text-gray-300 mb-1">Email</label>
            <input
              type="email"
              value={email}
              onChange={e => setEmail(e.target.value)}
              required
              className="w-full bg-gray-800 border border-gray-600 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
            />
          </div>
          <div>
            <label className="block text-sm text-gray-300 mb-1">Password</label>
            <input
              type="password"
              value={password}
              onChange={e => setPassword(e.target.value)}
              required
              className="w-full bg-gray-800 border border-gray-600 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
            />
          </div>
          <button
            type="submit"
            disabled={loading}
            className="w-full bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white rounded py-2 text-sm font-semibold transition"
          >
            {loading ? 'Signing in…' : 'Sign In'}
          </button>
        </form>

        <div className="mt-6 text-xs text-gray-500 space-y-1">
          <p className="font-semibold text-gray-400">Demo credentials:</p>
          <p>responder@ims.local / password123</p>
          <p>admin@ims.local / password123</p>
          <p>producer@ims.local / password123</p>
        </div>
      </div>
    </div>
  )
}
