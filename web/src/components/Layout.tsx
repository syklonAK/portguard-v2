import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import {
  LayoutDashboard, Network, ArrowLeftRight, ShieldCheck, Activity, ScrollText, Settings as SettingsIcon,
  ShieldHalf, Moon, Sun, LogOut, Wrench, Gauge, History, Stethoscope, Waypoints, Radio, Server, Package, GaugeCircle, GitBranch, Bell, UsersRound, Terminal,
} from 'lucide-react'
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, setToken } from '../api'

const nav = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard, end: true },
  { to: '/servers', label: 'Servers', icon: Server },
  { to: '/ports', label: 'Ports', icon: Network },
  { to: '/connections', label: 'Connections', icon: Radio },
  { to: '/mappings', label: 'Mappings', icon: ArrowLeftRight },
  { to: '/tunnels', label: 'Tunnels', icon: Waypoints },
  { to: '/services', label: 'Services', icon: Wrench },
  { to: '/runtime', label: 'HAProxy Runtime', icon: Gauge },
  { to: '/certs', label: 'SSL Certs', icon: ShieldCheck },
  { to: '/backups', label: 'Backups', icon: History },
  { to: '/versions', label: 'Versions', icon: GitBranch },
  { to: '/diagnostics', label: 'Diagnostics', icon: Stethoscope },
  { to: '/logs', label: 'Logs', icon: Terminal },
  { to: '/tools', label: 'Tools', icon: Package },
  { to: '/bandwidth', label: 'Bandwidth', icon: GaugeCircle },
  { to: '/monitoring', label: 'Monitoring', icon: Activity },
  { to: '/alerts', label: 'Alerts', icon: Bell },
  { to: '/audit', label: 'Audit Log', icon: ScrollText },
  { to: '/users', label: 'Users', icon: UsersRound },
  { to: '/settings', label: 'Settings', icon: SettingsIcon },
]

export default function Layout() {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [dark, setDark] = useState(() => document.documentElement.classList.contains('dark'))
  const me = useQuery({ queryKey: ['me'], queryFn: () => api.me(), staleTime: 60_000 })
  const system = useQuery({ queryKey: ['version'], queryFn: () => api.system(), staleTime: Infinity, select: (s) => s.version })

  const toggleTheme = () => {
    const next = !dark
    setDark(next)
    document.documentElement.classList.toggle('dark', next)
    localStorage.setItem('pg_theme', next ? 'dark' : 'light')
  }

  const logout = () => {
    setToken(null)
    qc.clear()
    navigate('/login', { replace: true })
  }

  return (
    <div className="flex h-full">
      {/* Sidebar */}
      <aside className="flex w-60 shrink-0 flex-col border-r border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
        <div className="flex items-center gap-2.5 px-5 py-5">
          <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-indigo-600 text-white shadow-md shadow-indigo-600/30">
            <ShieldHalf className="h-5 w-5" />
          </div>
          <div>
            <div className="flex items-center gap-1.5">
              <span className="text-sm font-bold tracking-tight">PortGuard</span>
              <span className="rounded bg-indigo-50 px-1 py-0.5 font-mono text-2xs font-medium text-indigo-500 dark:bg-indigo-500/10 dark:text-indigo-300" title="panel version">
                v{system.data || '…'}
              </span>
            </div>
            <div className="text-2xs text-slate-400">Port & Proxy Manager</div>
          </div>
        </div>
        <nav className="flex-1 space-y-0.5 px-3 py-2">
          {nav.map(({ to, label, icon: Icon, ...rest }) => (
            <NavLink
              key={to}
              to={to}
              end={'end' in rest ? (rest as { end?: boolean }).end : false}
              className={({ isActive }) =>
                `flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
                  isActive
                    ? 'bg-indigo-50 text-indigo-700 dark:bg-indigo-500/10 dark:text-indigo-300'
                    : 'text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-200'
                }`
              }
            >
              <Icon className="h-4 w-4" />
              {label}
            </NavLink>
          ))}
        </nav>
        <div className="border-t border-slate-200 p-3 dark:border-slate-800">
          <div className="flex items-center justify-between rounded-lg px-2 py-1.5">
            <span className="text-xs font-medium text-slate-500 dark:text-slate-400">{me.data?.username || ''}</span>
            <div className="flex gap-0.5">
              <button onClick={toggleTheme} className="rounded-md p-1.5 text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800" title="Toggle theme">
                {dark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
              </button>
              <button onClick={logout} className="rounded-md p-1.5 text-slate-400 hover:bg-slate-100 dark:hover:bg-slate-800" title="Logout">
                <LogOut className="h-4 w-4" />
              </button>
            </div>
          </div>
        </div>
      </aside>

      {/* Main */}
      <main className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-6xl p-6">
          <Outlet />
        </div>
      </main>
    </div>
  )
}
