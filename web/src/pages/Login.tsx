import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { ShieldHalf, GitBranch, Waypoints, GaugeCircle, Bell, Boxes, Lock } from 'lucide-react'
import { api, setToken } from '../api'
import { Button, Input } from '../components/ui'

const FEATURES = [
  { icon: Boxes, title: 'Services & Mappings', desc: 'Group, route and deploy nginx + HAProxy config from one visual plane.' },
  { icon: Waypoints, title: 'Path routing engine', desc: '/ws/*, /xhttp/* rules per service — Xray-grade control, zero hand-editing.' },
  { icon: GaugeCircle, title: 'Live analytics', desc: 'Bandwidth, connections and health per node — 30s resolution history.' },
  { icon: GitBranch, title: 'Versioned deploys', desc: 'Every apply is snapshotted: diff, restore, automatic rollback.' },
  { icon: Bell, title: 'Alerting', desc: 'Node, backend, certificate and threshold alerts to Telegram & webhooks.' },
  { icon: Lock, title: 'RBAC & audit', desc: 'Owner/admin/operator/viewer roles with a complete audit trail.' },
]

export default function Login({ mode }: { mode: 'login' | 'setup' }) {
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    if (mode === 'setup' && password !== confirm) {
      setError('Passwords do not match')
      return
    }
    setBusy(true)
    try {
      if (mode === 'setup') {
        await api.setup(username, password)
        const res = await api.login(username, password)
        setToken(res.token)
      } else {
        const res = await api.login(username, password)
        setToken(res.token)
      }
      window.dispatchEvent(new Event('pg-auth'))
      navigate('/', { replace: true })
    } catch (err: any) {
      setError(err.message || 'Something went wrong')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-full flex-col items-center justify-center px-4 py-10">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex flex-col items-center gap-3 text-center">
          <div className="pg-3d flex h-14 w-14 items-center justify-center rounded-2xl bg-neutral-900 text-white shadow-lg dark:bg-white dark:text-neutral-900">
            <ShieldHalf className="h-7 w-7" />
          </div>
          <div>
            <h1 className="text-xl font-bold tracking-tight">PortGuard</h1>
            <p className="mt-0.5 text-xs text-neutral-500 dark:text-neutral-400">
              {mode === 'setup' ? 'Create the first owner account' : 'Sign in to your control plane'}
            </p>
          </div>
        </div>

        <form
          onSubmit={submit}
          className="pg-modal-card space-y-4 rounded-xl border border-neutral-200 bg-white p-6 dark:border-neutral-800 dark:bg-neutral-900"
        >
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-300">Username</span>
            <Input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus minLength={3} required />
          </label>
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-300">Password</span>
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} minLength={8} required />
          </label>
          {mode === 'setup' && (
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-neutral-600 dark:text-neutral-300">Confirm password</span>
              <Input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} minLength={8} required />
            </label>
          )}

          {error && (
            <div className="rounded-lg bg-red-50 px-3 py-2 text-xs text-red-600 dark:bg-red-900/30 dark:text-red-300">
              {error}
            </div>
          )}

          <Button type="submit" className="w-full" disabled={busy}>
            {busy ? 'Please wait…' : mode === 'setup' ? 'Create owner' : 'Sign in'}
          </Button>
        </form>
        <p className="mt-4 text-center text-2xs text-neutral-400">
          PortGuard — Network Edge Control Plane
        </p>
      </div>

      {/* features: compact, centered under the card */}
      <div className="mt-10 grid w-full max-w-3xl grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {FEATURES.map((f, i) => (
          <div
            key={f.title}
            className="pg-3d flex flex-col items-center rounded-xl border border-neutral-200/80 bg-white p-4 text-center dark:border-white/10 dark:bg-white/[0.03]"
            style={{ animation: `fade-up 0.5s cubic-bezier(0.22,1,0.36,1) ${i * 70}ms both` }}
          >
            <f.icon className="h-4 w-4 text-neutral-700 dark:text-neutral-200" />
            <div className="mt-2 text-xs font-semibold text-neutral-800 dark:text-white">{f.title}</div>
            <p className="mt-1 text-2xs leading-relaxed text-neutral-500 dark:text-neutral-400">{f.desc}</p>
          </div>
        ))}
      </div>
    </div>
  )
}
