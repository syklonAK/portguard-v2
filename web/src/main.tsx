import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import App from './App'
import { ToastProvider } from './components/toast'
import { api, getToken } from './api'
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
function connectSSE() {
  const token = getToken()
  if (!token || sse) return
  sse = new EventSource(api.eventsUrl())
  for (const topic of ['system', 'conns', 'scan', 'health', 'service', 'alert']) {
    sse.addEventListener(topic, () => {
      window.dispatchEvent(new Event(`pg-sse-${topic}`))
    })
  }
  sse.onerror = () => {
    sse?.close()
    sse = null
    // token may have rotated or the panel restarted; retry after a pause
    setTimeout(() => {
      if (getToken()) connectSSE()
    }, 5000)
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
