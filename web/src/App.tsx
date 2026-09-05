import { Navigate, Route, Routes, useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api, getToken } from './api'
import Layout from './components/Layout'
import { Spinner } from './components/ui'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Ports from './pages/Ports'
import Mappings from './pages/Mappings'
import Services from './pages/Services'
import Runtime from './pages/Runtime'
import Backups from './pages/Backups'
import Diagnostics from './pages/Diagnostics'
import Certs from './pages/Certs'
import Monitoring from './pages/Monitoring'
import Audit from './pages/Audit'
import Settings from './pages/Settings'
import Tunnels from './pages/Tunnels'
import Connections from './pages/Connections'
import Servers from './pages/Servers'
import Tools from './pages/Tools'
import Bandwidth from './pages/Bandwidth'

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
        <Route path="/" element={<Dashboard />} />
        <Route path="/servers" element={<Servers />} />
        <Route path="/ports" element={<Ports />} />
        <Route path="/connections" element={<Connections />} />
        <Route path="/mappings" element={<Mappings />} />
        <Route path="/services" element={<Services />} />
        <Route path="/runtime" element={<Runtime />} />
        <Route path="/backups" element={<Backups />} />
        <Route path="/diagnostics" element={<Diagnostics />} />
        <Route path="/tools" element={<Tools />} />
        <Route path="/bandwidth" element={<Bandwidth />} />
        <Route path="/certs" element={<Certs />} />
        <Route path="/tunnels" element={<Tunnels />} />
        <Route path="/monitoring" element={<Monitoring />} />
        <Route path="/audit" element={<Audit />} />
        <Route path="/settings" element={<Settings />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
