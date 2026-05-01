import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { formatDistanceToNow } from 'date-fns'
import { listIncidents, Incident } from '../api/client'
import { useSse } from '../hooks/useSse'
import SeverityBadge from '../components/SeverityBadge'
import StatusBadge from '../components/StatusBadge'

export default function LiveFeed() {
  const [incidents, setIncidents] = useState<Incident[]>([])
  const [loading, setLoading] = useState(true)
  const [liveCount, setLiveCount] = useState(0)

  const fetchIncidents = useCallback(async () => {
    try {
      const data = await listIncidents(100)
      setIncidents(data ?? [])
    } catch {
      // keep stale data
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchIncidents() }, [fetchIncidents])

  // Refetch when the user navigates back to this tab (e.g. after closing an incident on the detail page)
  useEffect(() => {
    const onVisible = () => { if (document.visibilityState === 'visible') fetchIncidents() }
    document.addEventListener('visibilitychange', onVisible)
    return () => document.removeEventListener('visibilitychange', onVisible)
  }, [fetchIncidents])

  useSse(useCallback((e) => {
    if (
      e.type === 'incident.created' ||
      e.type === 'incident.transitioned' ||
      e.type === 'incident.updated' ||
      e.type === 'rca.submitted'
    ) {
      setLiveCount(c => c + 1)
      fetchIncidents()
    }
  }, [fetchIncidents]))

  const active = incidents.filter(i => i.status !== 'CLOSED')
  const closed = incidents.filter(i => i.status === 'CLOSED')

  return (
    <div className="max-w-6xl mx-auto p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-white">Live Incident Feed</h1>
          <p className="text-sm text-gray-400 mt-1">
            {active.length} active · {closed.length} closed
            {liveCount > 0 && <span className="ml-2 text-green-400 animate-pulse">● {liveCount} live updates</span>}
          </p>
        </div>
        <div className="text-xs text-gray-500 flex items-center gap-2">
          <span className="w-2 h-2 rounded-full bg-green-500 animate-pulse inline-block" />
          SSE connected
        </div>
      </div>

      {loading ? (
        <div className="text-gray-400 py-12 text-center">Loading incidents…</div>
      ) : incidents.length === 0 ? (
        <div className="text-gray-500 py-12 text-center border border-dashed border-gray-700 rounded-xl">
          No incidents yet. Run <code className="text-green-400">go run scripts/simulate_outage.go</code> to generate some.
        </div>
      ) : (
        <div className="space-y-6">
          {/* Active incidents */}
          <div>
            <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-wider mb-2">
              Active ({active.length})
            </h2>
            {active.length === 0 ? (
              <p className="text-gray-600 text-sm py-4 text-center border border-dashed border-gray-800 rounded-lg">No active incidents</p>
            ) : (
              <div className="space-y-2">
                {active.map(incident => <IncidentRow key={incident.id} incident={incident} />)}
              </div>
            )}
          </div>

          {/* Closed incidents */}
          <div>
            <h2 className="text-sm font-semibold text-gray-400 uppercase tracking-wider mb-2">
              Closed ({closed.length})
            </h2>
            {closed.length === 0 ? (
              <p className="text-gray-600 text-sm py-4 text-center border border-dashed border-gray-800 rounded-lg">No closed incidents</p>
            ) : (
              <div className="space-y-2">
                {closed.map(incident => <IncidentRow key={incident.id} incident={incident} />)}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function IncidentRow({ incident }: { incident: Incident }) {
  const borderColor: Record<string, string> = {
    P0: 'border-l-red-500',
    P1: 'border-l-orange-500',
    P2: 'border-l-yellow-400',
    P3: 'border-l-green-500',
  }

  return (
    <Link
      to={`/incidents/${incident.id}`}
      className={`block bg-gray-900 hover:bg-gray-800 border border-gray-700 border-l-4 ${borderColor[incident.severity] ?? 'border-l-gray-500'} rounded-lg px-5 py-4 transition`}
    >
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-3 min-w-0">
          <SeverityBadge severity={incident.severity} />
          <StatusBadge status={incident.status} />
          <div className="min-w-0">
            <p className="font-semibold text-white truncate">{incident.title}</p>
            <p className="text-xs text-gray-400 mt-0.5">{incident.component_id} · {incident.component_type}</p>
          </div>
        </div>
        <div className="text-right shrink-0">
          <div className="text-sm font-mono text-gray-300">{incident.signal_count} signals</div>
          <div className="text-xs text-gray-500 mt-0.5">
            {formatDistanceToNow(new Date(incident.last_signal_at), { addSuffix: true })}
          </div>
          {incident.mttr_seconds != null && (
            <div className="text-xs text-blue-400 mt-0.5">MTTR {formatMTTR(incident.mttr_seconds)}</div>
          )}
        </div>
      </div>
    </Link>
  )
}

function formatMTTR(s: number): string {
  if (s < 60) return `${Math.round(s)}s`
  if (s < 3600) return `${Math.round(s / 60)}m`
  return `${(s / 3600).toFixed(1)}h`
}
