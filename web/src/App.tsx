import { lazy, Suspense } from 'react'
import { Navigate, Route, Routes, useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api, getToken } from './api'
import Layout from './components/Layout'
import { Spinner } from './components/ui'
import Login from './pages/Login'

// Every page is code-split: the initial bundle only carries the shell
// (layout + login + router), and each page loads on first visit.
const Dashboard = lazy(() => import('./pages/Dashboard'))
const Ports = lazy(() => import('./pages/Ports'))
const Mappings = lazy(() => import('./pages/Mappings'))
const NginxServices = lazy(() => import('./pages/Services'))
const Runtime = lazy(() => import('./pages/Runtime'))
const Backups = lazy(() => import('./pages/Backups'))
const Diagnostics = lazy(() => import('./pages/Diagnostics'))
const Certs = lazy(() => import('./pages/Certs'))
const Monitoring = lazy(() => import('./pages/Monitoring'))
const Audit = lazy(() => import('./pages/Audit'))
const Settings = lazy(() => import('./pages/Settings'))
const Tunnels = lazy(() => import('./pages/Tunnels'))
const Connections = lazy(() => import('./pages/Connections'))
const Servers = lazy(() => import('./pages/Servers'))
const Tools = lazy(() => import('./pages/Tools'))
const Bandwidth = lazy(() => import('./pages/Bandwidth'))
const Versions = lazy(() => import('./pages/Versions'))
const Alerts = lazy(() => import('./pages/Alerts'))
const Users = lazy(() => import('./pages/Users'))
const Logs = lazy(() => import('./pages/Logs'))
const Analytics = lazy(() => import('./pages/Analytics'))
const AppServices = lazy(() => import('./pages/AppServices'))

function Page() {
  return (
    <div className="flex h-64 items-center justify-center">
      <Spinner className="h-7 w-7" />
    </div>
  )
}

function RequireAuth({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const me = useQuery({
    queryKey: ['me'],
    queryFn: () => api.me(),
    retry: false,
    staleTime: 60_000,
  })

  useEffect(() => {
    if (me.isError) {
      qc.clear()
      navigate('/login', { replace: true })
    }
  }, [me.isError, navigate, qc])

  if (me.isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner className="h-8 w-8" />
      </div>
    )
  }
  if (me.isError) return null
  return children as React.ReactElement
}

function AuthFlow() {
  const status = useQuery({ queryKey: ['setup-status'], queryFn: () => api.setupStatus() })
  if (status.isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner className="h-8 w-8" />
      </div>
    )
  }
  return <Navigate to={status.data?.setup_required ? '/setup' : '/login'} replace />
}

export default function App() {
  const [hasToken, setHasToken] = useState(() => !!getToken())

  useEffect(() => {
    const check = () => setHasToken(!!getToken())
    const interval = setInterval(check, 400)
    window.addEventListener('pg-auth', check)
    return () => {
      clearInterval(interval)
      window.removeEventListener('pg-auth', check)
    }
  }, [])

  if (!hasToken) {
    return (
      <Routes>
        <Route path="/login" element={<Login mode="login" />} />
        <Route path="/setup" element={<Login mode="setup" />} />
        <Route path="*" element={<AuthFlow />} />
      </Routes>
    )
  }

  return (
    <Routes>
      <Route path="/login" element={<Navigate to="/" replace />} />
      <Route path="/setup" element={<Navigate to="/" replace />} />
      <Route
        element={
          <RequireAuth>
            <Layout />
          </RequireAuth>
        }
      >
        <Route path="/" element={<Suspense fallback={<Page />}><Dashboard /></Suspense>} />
        <Route path="/servers" element={<Suspense fallback={<Page />}><Servers /></Suspense>} />
        <Route path="/ports" element={<Suspense fallback={<Page />}><Ports /></Suspense>} />
        <Route path="/connections" element={<Suspense fallback={<Page />}><Connections /></Suspense>} />
        <Route path="/mappings" element={<Suspense fallback={<Page />}><Mappings /></Suspense>} />
        <Route path="/services" element={<Suspense fallback={<Page />}><AppServices /></Suspense>} />
        <Route path="/analytics" element={<Suspense fallback={<Page />}><Analytics /></Suspense>} />
        <Route path="/services/systemd" element={<Suspense fallback={<Page />}><NginxServices /></Suspense>} />
        <Route path="/runtime" element={<Suspense fallback={<Page />}><Runtime /></Suspense>} />
        <Route path="/backups" element={<Suspense fallback={<Page />}><Backups /></Suspense>} />
        <Route path="/versions" element={<Suspense fallback={<Page />}><Versions /></Suspense>} />
        <Route path="/diagnostics" element={<Suspense fallback={<Page />}><Diagnostics /></Suspense>} />
        <Route path="/tools" element={<Suspense fallback={<Page />}><Tools /></Suspense>} />
        <Route path="/bandwidth" element={<Suspense fallback={<Page />}><Bandwidth /></Suspense>} />
        <Route path="/certs" element={<Suspense fallback={<Page />}><Certs /></Suspense>} />
        <Route path="/tunnels" element={<Suspense fallback={<Page />}><Tunnels /></Suspense>} />
        <Route path="/monitoring" element={<Suspense fallback={<Page />}><Monitoring /></Suspense>} />
        <Route path="/alerts" element={<Suspense fallback={<Page />}><Alerts /></Suspense>} />
        <Route path="/audit" element={<Suspense fallback={<Page />}><Audit /></Suspense>} />
        <Route path="/users" element={<Suspense fallback={<Page />}><Users /></Suspense>} />
        <Route path="/logs" element={<Suspense fallback={<Page />}><Logs /></Suspense>} />
        <Route path="/settings" element={<Suspense fallback={<Page />}><Settings /></Suspense>} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
