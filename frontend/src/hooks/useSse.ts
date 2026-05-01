import { useEffect, useRef, useCallback } from 'react'

export type SseEvent = {
  type: string
  payload: unknown
}

export function useSse(onEvent: (e: SseEvent) => void) {
  const esRef = useRef<EventSource | null>(null)
  const onEventRef = useRef(onEvent)
  onEventRef.current = onEvent

  const connect = useCallback(() => {
    if (esRef.current) esRef.current.close()

    const es = new EventSource('/stream', { withCredentials: true })
    esRef.current = es

    es.onmessage = (e) => {
      try {
        const parsed = JSON.parse(e.data) as SseEvent
        onEventRef.current(parsed)
      } catch {
        // ignore malformed events
      }
    }

    es.onerror = () => {
      es.close()
      // Reconnect after 3s
      setTimeout(connect, 3000)
    }
  }, [])

  useEffect(() => {
    connect()
    return () => {
      esRef.current?.close()
    }
  }, [connect])
}
