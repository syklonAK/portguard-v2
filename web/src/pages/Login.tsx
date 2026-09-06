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
    <div className="grid min-h-full grid-cols-1 lg:grid-cols-2">
      {/* ---- form side ---- */}
      <div className="flex items-center justify-center px-6 py-12">
        <div className="w-full max-w-sm">
          <div className="mb-8 flex flex-col items-center gap-3 lg:items-start">
            <div className="pg-3d flex h-14 w-14 items-center justify-center rounded-2xl bg-neutral-900 text-white shadow-lg dark:bg-white dark:text-neutral-900">
              <ShieldHalf className="h-7 w-7" />
            </div>
            <div className="text-center lg:text-left">
              <h1 className="text-xl font-bold tracking-tight">PortGuard</h1>
              <p className="text-xs text-neutral-500 dark:text-neutral-400">
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
          <p className="mt-4 text-center text-2xs text-neutral-400 lg:text-left">
            PortGuard — Network Edge Control Plane
          </p>
        </div>
      </div>

      {/* ---- landing side ---- */}
      <div className="relative hidden overflow-hidden bg-neutral-950 lg:block">
        {/* minimal grid backdrop */}
        <div
          className="pointer-events-none absolute inset-0 opacity-[0.06]"
          style={{
            backgroundImage:
              'linear-gradient(to right, white 1px, transparent 1px), linear-gradient(to bottom, white 1px, transparent 1px)',
            backgroundSize: '48px 48px',
          }}
        />
        <div className="relative flex h-full flex-col justify-center px-14 py-16">
          <p className="pg-float mb-4 w-fit rounded-full border border-white/10 bg-white/5 px-3 py-1 text-2xs font-medium text-neutral-300">
            Network Edge Control Plane
          </p>
          <h2 className="max-w-md text-4xl font-bold leading-tight tracking-tight text-white">
            Every port, every proxy,
            <br />
            <span className="text-neutral-400">one control plane.</span>
          </h2>
          <p className="mt-4 max-w-md text-sm leading-relaxed text-neutral-400">
            PortGuard runs nginx and HAProxy as code: mappings, path routing, services, certificates and
            fleet nodes — validated, versioned and rolled back automatically.
          </p>

          <div className="mt-10 grid max-w-lg grid-cols-2 gap-4">
            {FEATURES.map((f, i) => (
              <div
                key={f.title}
                className="pg-3d rounded-xl border border-white/10 bg-white/[0.03] p-4"
                style={{ animation: `fade-up 0.5s cubic-bezier(0.22,1,0.36,1) ${i * 70}ms both` }}
              >
                <f.icon className="h-4 w-4 text-neutral-200" />
                <div className="mt-2 text-xs font-semibold text-white">{f.title}</div>
                <p className="mt-1 text-2xs leading-relaxed text-neutral-400">{f.desc}</p>
              </div>
            ))}
          </div>

          <div className="mt-10 font-mono text-2xs text-neutral-500">
            render → validate → backup → apply → health-check → rollback
          </div>
        </div>
      </div>
    </div>
  )
}
