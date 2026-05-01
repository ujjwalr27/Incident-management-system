import { useState, useEffect, useCallback } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { formatDistanceToNow, format } from 'date-fns'
import {
  getIncident, getSignals, transitionIncident,
  IncidentDetail as Detail, Signal, StateTransition,
} from '../api/client'
import SeverityBadge from '../components/SeverityBadge'
import StatusBadge from '../components/StatusBadge'
import { useToast } from '../components/Toast'
import { useSse } from '../hooks/useSse'

const ALLOWED_TRANSITIONS: Record<string, string[]> = {
  OPEN:          ['INVESTIGATING'],
  INVESTIGATING: ['RESOLVED', 'OPEN'],
  RESOLVED:      ['CLOSED', 'INVESTIGATING'],
  CLOSED:        [],
}

// Numeric order of states — used to classify forward vs backward transitions.
const STATE_ORDER: Record<string, number> = {
  OPEN: 0, INVESTIGATING: 1, RESOLVED: 2, CLOSED: 3,
}

function isForward(from: string, to: string) {
  return (STATE_ORDER[to] ?? 0) > (STATE_ORDER[from] ?? 0)
}

// Bright distinct color per TARGET state (same color whether forward or backward).
const STATE_COLORS: Record<string, string> = {
  OPEN:          'bg-rose-600 hover:bg-rose-500 text-white',
  INVESTIGATING: 'bg-amber-500 hover:bg-amber-400 text-gray-900',
  RESOLVED:      'bg-cyan-600 hover:bg-cyan-500 text-white',
  CLOSED:        'bg-emerald-600 hover:bg-emerald-500 text-white',
}

export default function IncidentDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { show } = useToast()
  const [detail, setDetail] = useState<Detail | null>(null)
  const [signals, setSignals] = useState<Signal[]>([])
  const [tab, setTab] = useState<'signals' | 'timeline'>('signals')
  const [transitioning, setTransitioning] = useState(false)

  const fetch = useCallback(async () => {
    if (!id) return
    try {
      const [d, s] = await Promise.all([getIncident(id), getSignals(id)])
      setDetail(d)
      setSignals(s)
    } catch (e) {
      show((e as Error).message, 'error')
    }
  }, [id, show])

  useEffect(() => { fetch() }, [fetch])

  useSse(useCallback((e) => {
    const payload = e.payload as { id?: string; work_item_id?: string }
    if (payload?.id === id || payload?.work_item_id === id) fetch()
  }, [id, fetch]))

  async function handleTransition(to: string) {
    if (!id || !detail) return
    setTransitioning(true)
    try {
      await transitionIncident(id, to)
      show(`Status → ${to}`, 'success')
      if (to === 'RESOLVED') navigate(`/incidents/${id}/rca`)
      else fetch()
    } catch (e) {
      show((e as Error).message, 'error')
    } finally {
      setTransitioning(false)
    }
  }

  if (!detail) return <div className="p-6 text-gray-400">Loading…</div>

  const { incident, rca, transitions } = detail
  const allowed = ALLOWED_TRANSITIONS[incident.status] ?? []

  return (
    <div className="max-w-5xl mx-auto p-6 space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <Link to="/" className="text-xs text-gray-500 hover:text-gray-300 mb-2 block">← Back to feed</Link>
          <h1 className="text-xl font-bold text-white">{incident.title}</h1>
          <p className="text-sm text-gray-400 mt-1">{incident.component_id} · {incident.component_type}</p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <SeverityBadge severity={incident.severity} />
          <StatusBadge status={incident.status} />
        </div>
      </div>

      {/* Stats row */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <Stat label="Signals" value={String(incident.signal_count)} />
        <Stat label="First seen" value={formatDistanceToNow(new Date(incident.first_signal_at), { addSuffix: true })} />
        <Stat label="Last seen" value={formatDistanceToNow(new Date(incident.last_signal_at), { addSuffix: true })} />
        <Stat label="MTTR" value={incident.mttr_seconds != null ? formatMTTR(incident.mttr_seconds) : rca ? '—' : 'Ongoing'} />
      </div>

      {/* Transition buttons */}
      {allowed.length > 0 && (
        <div className="flex gap-2 flex-wrap">
          {/* Backward (regression) transitions — ← arrow, muted style, sorted first */}
          {[...allowed]
            .sort((a, b) => {
              const aFwd = isForward(incident.status, a)
              const bFwd = isForward(incident.status, b)
              if (aFwd === bFwd) return 0
              return aFwd ? 1 : -1   // backward first
            })
            .map(s => {
              const forward = isForward(incident.status, s)
              const colorClass = STATE_COLORS[s] ?? 'bg-gray-600 hover:bg-gray-500 text-white'
              return (
                <button
                  key={s}
                  onClick={() => handleTransition(s)}
                  disabled={transitioning}
                  className={`px-4 py-1.5 rounded text-sm font-semibold transition disabled:opacity-50 ${colorClass}`}
                >
                  {s === 'CLOSED'
                    ? '✓ CLOSE'
                    : forward
                      ? `${s} →`
                      : `← ${s}`}
                </button>
              )
            })}
          {incident.status === 'RESOLVED' && (
            <Link
              to={`/incidents/${id}/rca`}
              className="px-4 py-1.5 rounded text-sm font-semibold bg-purple-600 hover:bg-purple-500 text-white transition"
            >
              {rca ? '✏️ Edit RCA' : 'Submit RCA'}
            </Link>
          )}
        </div>
      )}

      {/* RCA summary if present */}
      {rca && (
        <div className="bg-gray-900 border border-green-700 rounded-lg p-4">
          <div className="flex items-center gap-2 mb-3">
            <span className="text-green-400 font-semibold text-sm">Root Cause Analysis</span>
            <span className="text-xs text-gray-500">submitted {formatDistanceToNow(new Date(rca.submitted_at), { addSuffix: true })}</span>
          </div>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4 text-sm">
            <div><p className="text-gray-400 text-xs mb-1">Category</p><p className="text-white">{rca.category}</p></div>
            <div><p className="text-gray-400 text-xs mb-1">Fix applied</p><p className="text-white">{rca.fix_applied}</p></div>
            <div><p className="text-gray-400 text-xs mb-1">Prevention</p><p className="text-white">{rca.prevention_steps}</p></div>
          </div>
        </div>
      )}

      {/* Tabs */}
      <div>
        <div className="flex gap-1 border-b border-gray-700 mb-4">
          {(['signals', 'timeline'] as const).map(t => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`px-4 py-2 text-sm font-medium transition capitalize ${tab === t ? 'text-white border-b-2 border-blue-500' : 'text-gray-500 hover:text-gray-300'}`}
            >
              {t}
            </button>
          ))}
        </div>

        {tab === 'signals' && <SignalsTab signals={signals} />}
        {tab === 'timeline' && <TimelineTab transitions={transitions} />}
      </div>
    </div>
  )
}

function SignalsTab({ signals }: { signals: Signal[] | null }) {
  if (!signals || signals.length === 0)
    return <p className="text-gray-500 text-sm">No raw signals linked yet.</p>

  return (
    <div className="space-y-2 max-h-96 overflow-y-auto scrollbar-thin">
      {signals.map((s, i) => (
        <div key={i} className="bg-gray-900 border border-gray-700 rounded px-4 py-2 text-sm">
          <div className="flex items-center justify-between gap-2">
            <span className="text-gray-200 truncate">{s.message || '(no message)'}</span>
            <span className="text-xs text-gray-500 shrink-0">{format(new Date(s.timestamp), 'HH:mm:ss.SSS')}</span>
          </div>
          {s.tags && Object.keys(s.tags).length > 0 && (
            <div className="flex gap-1 mt-1 flex-wrap">
              {Object.entries(s.tags).map(([k, v]) => (
                <span key={k} className="text-xs bg-gray-800 text-gray-400 px-1.5 rounded">{k}={v}</span>
              ))}
            </div>
          )}
        </div>
      ))}
    </div>
  )
}

function TimelineTab({ transitions }: { transitions: StateTransition[] | null }) {
  if (!transitions || transitions.length === 0)
    return <p className="text-gray-500 text-sm">No transitions yet.</p>

  return (
    <div className="space-y-3">
      {transitions.map((t) => (
        <div key={t.id} className="flex items-start gap-3">
          <div className="w-2 h-2 rounded-full bg-blue-500 mt-1.5 shrink-0" />
          <div>
            <p className="text-sm text-white">
              {t.from_state ? <span className="text-gray-400">{t.from_state} → </span> : null}
              <span className="font-semibold">{t.to_state}</span>
            </p>
            <p className="text-xs text-gray-500">{format(new Date(t.transitioned_at), 'MMM d, HH:mm:ss')}</p>
            {t.notes && <p className="text-xs text-gray-400 mt-0.5">{t.notes}</p>}
          </div>
        </div>
      ))}
    </div>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-gray-900 border border-gray-700 rounded-lg px-4 py-3">
      <p className="text-xs text-gray-400">{label}</p>
      <p className="text-sm font-semibold text-white mt-0.5">{value}</p>
    </div>
  )
}

function formatMTTR(s: number) {
  if (s < 60) return `${Math.round(s)}s`
  if (s < 3600) return `${Math.round(s / 60)}m`
  return `${(s / 3600).toFixed(1)}h`
}
