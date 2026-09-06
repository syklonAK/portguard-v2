import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import App from './App'
import { ToastProvider } from './components/toast'
import { api, getToken } from './api'
import 'bootstrap/dist/css/bootstrap.min.css'
import 'bootstrap/dist/js/bootstrap.bundle.min.js'
import './index.css'

const theme = localStorage.getItem('pg_theme') || 'dark'
document.documentElement.classList.toggle('dark', theme === 'dark')

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, refetchOnWindowFocus: false },
  },
})

// ---- global SSE bridge ----
// One EventSource for the whole app; each broker event becomes a window event
// ("pg-sse-<topic>") that pages listen to for instant refreshes.
let sse: EventSource | null = null
let sseRetries = 0
function connectSSE() {
  const token = getToken()
  if (!token || sse) return
  sse = new EventSource(api.eventsUrl())
  for (const topic of ['system', 'conns', 'scan', 'health', 'service', 'alert', 'cert', 'job']) {
    sse.addEventListener(topic, () => {
      window.dispatchEvent(new Event(`pg-sse-${topic}`))
    })
  }
  sse.onerror = () => {
    sse?.close()
    sse = null
    // token may have rotated or the panel restarted; back off so a down
    // panel doesn't spin reconnect loops (capped at 30s, reset on success)
    sseRetries = Math.min(sseRetries + 1, 6)
    const delay = Math.min(1000 * 2 ** sseRetries, 30_000)
    setTimeout(() => {
      if (getToken()) connectSSE()
    }, delay)
  }
  sse.onopen = () => {
    sseRetries = 0
  }
}
connectSSE()
window.addEventListener('pg-auth', () => {
  // token changed (login/logout): reconnect with the fresh one
  sse?.close()
  sse = null
  connectSSE()
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <ToastProvider>
          <App />
        </ToastProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)
