import { createContext, useContext, useState, useCallback, ReactNode } from 'react'

type ToastType = 'success' | 'error' | 'info'
interface Toast { id: number; message: string; type: ToastType }

interface ToastCtx { show: (message: string, type?: ToastType) => void }
const Ctx = createContext<ToastCtx>({ show: () => {} })

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  let counter = 0

  const show = useCallback((message: string, type: ToastType = 'info') => {
    const id = ++counter
    setToasts(t => [...t, { id, message, type }])
    setTimeout(() => setToasts(t => t.filter(x => x.id !== id)), 4000)
  }, [])

  const bg: Record<ToastType, string> = {
    success: 'bg-green-700',
    error:   'bg-red-700',
    info:    'bg-gray-700',
  }

  return (
    <Ctx.Provider value={{ show }}>
      {children}
      <div className="fixed bottom-4 right-4 flex flex-col gap-2 z-50">
        {toasts.map(t => (
          <div key={t.id} className={`${bg[t.type]} text-white px-4 py-2 rounded shadow-lg text-sm max-w-xs animate-fade-in`}>
            {t.message}
          </div>
        ))}
      </div>
    </Ctx.Provider>
  )
}

export const useToast = () => useContext(Ctx)
