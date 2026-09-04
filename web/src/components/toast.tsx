import { createContext, useCallback, useContext, useState, type ReactNode } from 'react'
import { CheckCircle2, XCircle, AlertTriangle, Info } from 'lucide-react'

type ToastKind = 'success' | 'error' | 'warning' | 'info'
interface Toast {
  id: number
  kind: ToastKind
  message: string
}

const ToastCtx = createContext<{ push: (kind: ToastKind, message: string) => void }>({
  push: () => {},
})

export function useToast() {
  return useContext(ToastCtx)
}

let nextId = 1

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])

  const push = useCallback((kind: ToastKind, message: string) => {
    const id = nextId++
    setToasts((t) => [...t, { id, kind, message }])
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 5000)
  }, [])

  return (
    <ToastCtx.Provider value={{ push }}>
      {children}
      <div className="fixed bottom-4 right-4 z-100 flex flex-col gap-2 max-w-sm">
        {toasts.map((t) => (
          <div
            key={t.id}
            className="flex items-start gap-2 rounded-lg border px-4 py-3 shadow-lg backdrop-blur bg-white/95 dark:bg-slate-900/95
              border-slate-200 dark:border-slate-700 text-sm"
          >
            {t.kind === 'success' && <CheckCircle2 className="h-4 w-4 mt-0.5 text-emerald-500 shrink-0" />}
            {t.kind === 'error' && <XCircle className="h-4 w-4 mt-0.5 text-red-500 shrink-0" />}
            {t.kind === 'warning' && <AlertTriangle className="h-4 w-4 mt-0.5 text-amber-500 shrink-0" />}
            {t.kind === 'info' && <Info className="h-4 w-4 mt-0.5 text-sky-500 shrink-0" />}
            <span className="whitespace-pre-wrap break-words">{t.message}</span>
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  )
}
