import { useState, useEffect, FormEvent } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { getIncident, submitRCA, RCARequest, RCA } from '../api/client'
import { useToast } from '../components/Toast'

const CATEGORIES = [
  'Configuration Change',
  'Code Deployment',
  'Hardware Failure',
  'Network Issue',
  'Capacity / Scaling',
  'Third-party Service',
  'Human Error',
  'Unknown',
]

export default function RcaForm() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { show } = useToast()

  const [incidentTitle, setIncidentTitle] = useState('')
  const [isEditing, setIsEditing] = useState(false)
  const [form, setForm] = useState<RCARequest>({
    category: '',
    fix_applied: '',
    prevention_steps: '',
    incident_start: '',
    incident_end: '',
  })
  const [submitting, setSubmitting] = useState(false)
  const [errors, setErrors] = useState<Partial<Record<keyof RCARequest, string>>>({})

  useEffect(() => {
    if (!id) return
    getIncident(id).then(d => {
      setIncidentTitle(d.incident.title)
      const existingRca: RCA | null = d.rca ?? null
      if (existingRca) {
        // Pre-fill all fields from the existing RCA for editing
        setIsEditing(true)
        setForm({
          category: existingRca.category,
          fix_applied: existingRca.fix_applied,
          prevention_steps: existingRca.prevention_steps,
          incident_start: new Date(existingRca.incident_start).toISOString().slice(0, 16),
          incident_end: new Date(existingRca.incident_end).toISOString().slice(0, 16),
        })
      } else {
        // New RCA — pre-fill start time only
        const start = new Date(d.incident.first_signal_at).toISOString().slice(0, 16)
        setForm(f => ({ ...f, incident_start: start }))
      }
    }).catch(() => {})
  }, [id])

  function validate(): boolean {
    const e: typeof errors = {}
    if (!form.category) e.category = 'Category is required'
    if (!form.fix_applied.trim()) e.fix_applied = 'Fix applied is required'
    if (!form.prevention_steps.trim()) e.prevention_steps = 'Prevention steps are required'
    if (!form.incident_start) e.incident_start = 'Start time is required'
    if (!form.incident_end) e.incident_end = 'End time is required'
    if (form.incident_start && form.incident_end && form.incident_end <= form.incident_start)
      e.incident_end = 'End time must be after start time'
    setErrors(e)
    return Object.keys(e).length === 0
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!validate() || !id) return
    setSubmitting(true)
    try {
      await submitRCA(id, {
        ...form,
        incident_start: new Date(form.incident_start).toISOString(),
        incident_end: new Date(form.incident_end).toISOString(),
      })
      show('RCA submitted successfully', 'success')
      navigate(`/incidents/${id}`)
    } catch (err) {
      show((err as Error).message, 'error')
    } finally {
      setSubmitting(false)
    }
  }

  function set<K extends keyof RCARequest>(k: K, v: RCARequest[K]) {
    setForm(f => ({ ...f, [k]: v }))
    setErrors(e => ({ ...e, [k]: undefined }))
  }

  return (
    <div className="max-w-2xl mx-auto p-6">
      <Link to={`/incidents/${id}`} className="text-xs text-gray-500 hover:text-gray-300 mb-4 block">
        ← Back to incident
      </Link>
      <h1 className="text-xl font-bold text-white mb-1">
        {isEditing ? '✏️ Edit Root Cause Analysis' : 'Root Cause Analysis'}
      </h1>
      {incidentTitle && <p className="text-sm text-gray-400 mb-6">{incidentTitle}</p>}
      {isEditing && (
        <p className="text-xs text-yellow-400 bg-yellow-400/10 border border-yellow-400/30 rounded px-3 py-2 mb-4">
          You are editing an existing RCA. All fields will be updated on submit.
        </p>
      )}

      <form onSubmit={handleSubmit} className="space-y-5">
        {/* Category */}
        <Field label="Root Cause Category *" error={errors.category}>
          <select
            value={form.category}
            onChange={e => set('category', e.target.value)}
            className="w-full bg-gray-800 border border-gray-600 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
          >
            <option value="">Select a category…</option>
            {CATEGORIES.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
        </Field>

        {/* Date/time pickers */}
        <div className="grid grid-cols-2 gap-4">
          <Field label="Incident Start *" error={errors.incident_start}>
            <input
              type="datetime-local"
              value={form.incident_start}
              onChange={e => set('incident_start', e.target.value)}
              className="w-full bg-gray-800 border border-gray-600 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
            />
          </Field>
          <Field label="Incident End *" error={errors.incident_end}>
            <input
              type="datetime-local"
              value={form.incident_end}
              onChange={e => set('incident_end', e.target.value)}
              className="w-full bg-gray-800 border border-gray-600 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
            />
          </Field>
        </div>

        {/* Fix applied */}
        <Field label="Fix Applied *" error={errors.fix_applied}>
          <textarea
            rows={3}
            placeholder="Describe the fix that was applied to resolve the incident…"
            value={form.fix_applied}
            onChange={e => set('fix_applied', e.target.value)}
            className="w-full bg-gray-800 border border-gray-600 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 resize-none"
          />
        </Field>

        {/* Prevention steps */}
        <Field label="Prevention Steps *" error={errors.prevention_steps}>
          <textarea
            rows={3}
            placeholder="What steps will prevent this from happening again…"
            value={form.prevention_steps}
            onChange={e => set('prevention_steps', e.target.value)}
            className="w-full bg-gray-800 border border-gray-600 rounded px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500 resize-none"
          />
        </Field>

        <div className="flex gap-3 pt-2">
          <button
            type="submit"
            disabled={submitting}
            className="px-6 py-2 bg-purple-600 hover:bg-purple-500 disabled:opacity-50 text-white rounded font-semibold text-sm transition"
          >
            {submitting ? (isEditing ? 'Updating…' : 'Submitting…') : (isEditing ? 'Update RCA' : 'Submit RCA')}
          </button>
          <Link
            to={`/incidents/${id}`}
            className="px-6 py-2 bg-gray-700 hover:bg-gray-600 text-white rounded font-semibold text-sm transition"
          >
            Cancel
          </Link>
        </div>
      </form>
    </div>
  )
}

function Field({ label, error, children }: { label: string; error?: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="block text-sm text-gray-300 mb-1">{label}</label>
      {children}
      {error && <p className="text-xs text-red-400 mt-1">{error}</p>}
    </div>
  )
}
