interface Props { status: string }

const colors: Record<string, string> = {
  OPEN:          'bg-red-900 text-red-300 border border-red-700',
  INVESTIGATING: 'bg-orange-900 text-orange-300 border border-orange-700',
  RESOLVED:      'bg-blue-900 text-blue-300 border border-blue-700',
  CLOSED:        'bg-gray-700 text-gray-400 border border-gray-600',
}

export default function StatusBadge({ status }: Props) {
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-semibold ${colors[status] ?? 'bg-gray-700 text-gray-300'}`}>
      {status}
    </span>
  )
}
