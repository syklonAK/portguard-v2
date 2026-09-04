import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { ShieldHalf } from 'lucide-react'
import { api, setToken } from '../api'
import { Button, Input } from '../components/ui'

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
    if (mode === 'setup') {
      if (password !== confirm) {
        setError('Passwords do not match')
        return
      }
    }
    setBusy(true)
    try {
      if (mode === 'setup') {
        await api.setup(username, password)
        // auto-login after setup
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
    <div className="flex h-full items-center justify-center bg-slate-100 px-4 dark:bg-slate-950">
      <div className="w-full max-w-sm">
        <div className="mb-8 flex flex-col items-center gap-3">
          <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-indigo-600 text-white shadow-lg shadow-indigo-600/30">
            <ShieldHalf className="h-7 w-7" />
          </div>
          <div className="text-center">
            <h1 className="text-xl font-bold tracking-tight">PortGuard</h1>
            <p className="text-xs text-slate-500 dark:text-slate-400">
              {mode === 'setup' ? 'Create the first admin account' : 'Sign in to your panel'}
            </p>
          </div>
        </div>

        <form
          onSubmit={submit}
          className="space-y-4 rounded-xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900"
        >
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-300">Username</span>
            <Input value={username} onChange={(e) => setUsername(e.target.value)} autoFocus minLength={3} required />
          </label>
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-300">Password</span>
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} minLength={8} required />
          </label>
          {mode === 'setup' && (
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-300">Confirm password</span>
              <Input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} minLength={8} required />
            </label>
          )}

          {error && (
            <div className="rounded-lg bg-red-50 px-3 py-2 text-xs text-red-600 dark:bg-red-900/30 dark:text-red-300">
              {error}
            </div>
          )}

          <Button type="submit" className="w-full" disabled={busy}>
            {busy ? 'Please wait…' : mode === 'setup' ? 'Create admin' : 'Sign in'}
          </Button>
        </form>
        <p className="mt-4 text-center text-2xs text-slate-400">PortGuard — Port & Reverse Proxy Manager</p>
      </div>
    </div>
  )
}
