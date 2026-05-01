interface Props { severity: string }

const colors: Record<string, string> = {
  P0: 'bg-red-600 text-white',
  P1: 'bg-orange-500 text-white',
  P2: 'bg-yellow-400 text-gray-900',
  P3: 'bg-green-600 text-white',
}

export default function SeverityBadge({ severity }: Props) {
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-bold ${colors[severity] ?? 'bg-gray-600 text-white'}`}>
      {severity}
    </span>
  )
}
