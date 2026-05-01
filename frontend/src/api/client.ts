const API_BASE = import.meta.env.VITE_API_URL ?? ''

let accessToken: string | null = null
let refreshToken: string | null = null
let refreshing: Promise<void> | null = null

export function setToken(access: string, refresh?: string) {
  accessToken = access
  if (refresh) refreshToken = refresh
}
export function clearToken() {
  accessToken = null
  refreshToken = null
  localStorage.removeItem('access_token')
  localStorage.removeItem('refresh_token')
}

async function tryRefresh(): Promise<boolean> {
  const rt = refreshToken ?? localStorage.getItem('refresh_token')
  if (!rt) return false
  try {
    const res = await fetch(`${API_BASE}/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: rt }),
      credentials: 'include',
    })
    if (!res.ok) return false
    const pair: { access_token: string; refresh_token: string } = await res.json()
    accessToken = pair.access_token
    refreshToken = pair.refresh_token
    localStorage.setItem('access_token', pair.access_token)
    localStorage.setItem('refresh_token', pair.refresh_token)
    return true
  } catch {
    return false
  }
}

async function request<T>(path: string, init: RequestInit = {}, retry = true): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(init.headers as Record<string, string>),
  }
  if (accessToken) headers['Authorization'] = `Bearer ${accessToken}`

  const res = await fetch(`${API_BASE}${path}`, { ...init, headers, credentials: 'include' })

  if (res.status === 401 && retry) {
    // Deduplicate concurrent refresh calls.
    if (!refreshing) refreshing = tryRefresh().then(() => { refreshing = null })
    await refreshing
    if (accessToken) return request<T>(path, init, false)
    clearToken()
    window.location.href = '/login'
    throw new Error('Unauthorized')
  }

  if (res.status === 401) {
    clearToken()
    window.location.href = '/login'
    throw new Error('Unauthorized')
  }

  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(body.error ?? 'Request failed')
  }

  return res.json() as Promise<T>
}

// Auth
export const login = (email: string, password: string) =>
  request<{ access_token: string; refresh_token: string }>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  })

export const getMe = () => request<{ id: string; email: string; role: string }>('/auth/me')

// Incidents
export const listIncidents = (limit = 50, offset = 0) =>
  request<Incident[]>(`/api/v1/incidents?limit=${limit}&offset=${offset}`)

export const getIncident = (id: string) =>
  request<IncidentDetail>(`/api/v1/incidents/${id}`)

export const getSignals = (id: string, limit = 100, offset = 0) =>
  request<Signal[]>(`/api/v1/incidents/${id}/signals?limit=${limit}&offset=${offset}`)

export const transitionIncident = (id: string, to_status: string, notes?: string) =>
  request<{ status: string }>(`/api/v1/incidents/${id}/transition`, {
    method: 'POST',
    body: JSON.stringify({ to_status, notes }),
  })

export const submitRCA = (id: string, data: RCARequest) =>
  request<RCA>(`/api/v1/incidents/${id}/rca`, {
    method: 'POST',
    body: JSON.stringify(data),
  })

export const getRCA = (id: string) =>
  request<RCA>(`/api/v1/incidents/${id}/rca`)

// Types
export interface Incident {
  id: string
  component_id: string
  component_type: string
  severity: 'P0' | 'P1' | 'P2' | 'P3'
  status: 'OPEN' | 'INVESTIGATING' | 'RESOLVED' | 'CLOSED'
  title: string
  signal_count: number
  first_signal_at: string
  last_signal_at: string
  mttr_seconds?: number
}

export interface Signal {
  component_id: string
  component_type: string
  severity: string
  message: string
  tags?: Record<string, string>
  timestamp: string
  work_item_id?: string
}

export interface StateTransition {
  id: string
  from_state?: string
  to_state: string
  transitioned_at: string
  notes?: string
}

export interface RCA {
  id: string
  work_item_id: string
  category: string
  fix_applied: string
  prevention_steps: string
  incident_start: string
  incident_end: string
  submitted_at: string
}

export interface IncidentDetail {
  incident: Incident
  rca: RCA | null
  transitions: StateTransition[]
}

export interface RCARequest {
  category: string
  fix_applied: string
  prevention_steps: string
  incident_start: string
  incident_end: string
}
