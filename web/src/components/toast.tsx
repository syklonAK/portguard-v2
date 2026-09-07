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
      <div className="fixed right-4 bottom-4 z-100 flex max-w-sm flex-col gap-2">
        {toasts.map((t) => (
          <div
            key={t.id}
            className="flex items-start gap-2 rounded-lg border border-border bg-popover px-4 py-3 text-foreground shadow-lg backdrop-blur-sm text-sm"
          >
            {t.kind === 'success' && <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-success" />}
            {t.kind === 'error' && <XCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />}
            {t.kind === 'warning' && <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />}
            {t.kind === 'info' && <Info className="mt-0.5 h-4 w-4 shrink-0 text-primary" />}
            <span className="whitespace-pre-wrap break-words">{t.message}</span>
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  )
}
