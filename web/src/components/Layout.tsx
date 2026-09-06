import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import {
  LayoutDashboard, Network, ArrowLeftRight, ShieldCheck, Activity, ScrollText, Settings as SettingsIcon,
  ShieldHalf, Moon, Sun, LogOut, Wrench, Gauge, History, Stethoscope, Waypoints, Radio, Server, Package, GaugeCircle, GitBranch, Bell, UsersRound, Terminal, Boxes, TrendingUp, Menu,
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
  { to: '/services', label: 'Services', icon: Boxes },
  { to: '/analytics', label: 'Analytics', icon: TrendingUp },
  { to: '/tunnels', label: 'Tunnels', icon: Waypoints },
  { to: '/services/systemd', label: 'Nginx/HAProxy', icon: Wrench },
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

// SidebarContent is shared between the desktop rail and the Bootstrap
// offcanvas that replaces it on phones/tablets. closeOnNav makes every
// link dismiss the offcanvas after navigation (no-op on desktop).
function SidebarContent({ me, system, dark, onToggleTheme, onLogout, closeOnNav }: {
  me?: { username?: string; role?: string }
  system?: string
  dark: boolean
  onToggleTheme: () => void
  onLogout: () => void
  closeOnNav?: boolean
}) {
  return (
    <>
      <div className="flex items-center gap-2.5 px-5 py-5">
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-neutral-900 text-white shadow-md dark:bg-white dark:text-neutral-900">
          <ShieldHalf className="h-5 w-5" />
        </div>
        <div className="min-w-0">
          <div className="flex items-center gap-1.5">
            <span className="text-sm font-bold tracking-tight">PortGuard</span>
            <span className="rounded bg-neutral-100 px-1 py-0.5 font-mono text-2xs font-medium text-neutral-500 dark:bg-neutral-800 dark:text-neutral-300" title="panel version">
              v{system || '…'}
            </span>
          </div>
          <div className="text-2xs text-neutral-400">Port & Proxy Manager</div>
        </div>
      </div>
      <nav className="flex-1 space-y-0.5 overflow-y-auto px-3 py-2">
        {nav.map(({ to, label, icon: Icon, ...rest }) => (
          <NavLink
            key={to}
            to={to}
            end={'end' in rest ? (rest as { end?: boolean }).end : false}
            data-bs-dismiss={closeOnNav ? 'offcanvas' : undefined}
            className={({ isActive }) =>
              `pg-press flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
                isActive
                  ? 'bg-neutral-900 text-white dark:bg-white dark:text-neutral-900'
                  : 'text-neutral-600 hover:bg-neutral-100 dark:text-neutral-400 dark:hover:bg-neutral-800 dark:hover:text-neutral-200'
              }`
            }
          >
            <Icon className="h-4 w-4 shrink-0" />
            {label}
          </NavLink>
        ))}
      </nav>
      <div className="border-t border-neutral-200/80 p-3 dark:border-neutral-800">
        <div className="flex items-center justify-between rounded-lg px-2 py-1.5">
          <span className="min-w-0 truncate text-xs font-medium text-neutral-500 dark:text-neutral-400">
            {me?.username || ''}
            {me?.role && <span className="ml-1 text-2xs text-neutral-400">({me.role})</span>}
          </span>
          <div className="flex shrink-0 gap-0.5">
            <button onClick={onToggleTheme} className="pg-press rounded-md p-1.5 text-neutral-400 hover:bg-neutral-100 dark:hover:bg-neutral-800" title="Toggle theme">
              {dark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
            </button>
            <button onClick={onLogout} className="pg-press rounded-md p-1.5 text-neutral-400 hover:bg-neutral-100 dark:hover:bg-neutral-800" title="Logout">
              <LogOut className="h-4 w-4" />
            </button>
          </div>
        </div>
      </div>
    </>
  )
}

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

  const sidebarProps = { me: me.data, system: system.data, dark, onToggleTheme: toggleTheme, onLogout: logout }

  return (
    <div className="flex h-full flex-col lg:flex-row">
      {/* mobile top bar (Bootstrap offcanvas opens the nav) */}
      <header className="flex items-center justify-between gap-2 border-b border-neutral-200/80 bg-white px-3 py-2 dark:border-neutral-800 dark:bg-neutral-900 lg:hidden">
        <button
          className="pg-press inline-flex h-9 w-9 items-center justify-center rounded-lg border border-neutral-200 text-neutral-600 dark:border-neutral-700 dark:text-neutral-300"
          type="button"
          data-bs-toggle="offcanvas"
          data-bs-target="#pgSidebar"
          aria-controls="pgSidebar"
          title="Menu"
        >
          <Menu className="h-5 w-5" />
        </button>
        <div className="flex min-w-0 items-center gap-2">
          <ShieldHalf className="h-4 w-4 shrink-0" />
          <span className="truncate text-sm font-bold tracking-tight">PortGuard</span>
          <span className="rounded bg-neutral-100 px-1 py-0.5 font-mono text-2xs text-neutral-500 dark:bg-neutral-800 dark:text-neutral-300">
            v{system.data || '…'}
          </span>
        </div>
        <div className="flex gap-0.5">
          <button onClick={toggleTheme} className="pg-press rounded-md p-1.5 text-neutral-400 hover:bg-neutral-100 dark:hover:bg-neutral-800" title="Toggle theme">
            {dark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          </button>
          <button onClick={logout} className="pg-press rounded-md p-1.5 text-neutral-400 hover:bg-neutral-100 dark:hover:bg-neutral-800" title="Logout">
            <LogOut className="h-4 w-4" />
          </button>
        </div>
      </header>

      {/* Bootstrap offcanvas nav: phones / tablets */}
      <div className="offcanvas offcanvas-start pg-offcanvas" tabIndex={-1} id="pgSidebar" aria-label="Navigation">
        <div className="offcanvas-header border-b border-neutral-200/80 dark:border-neutral-800">
          <span className="text-sm font-semibold">Navigation</span>
          <button type="button" className="pg-press rounded-md p-1.5 text-neutral-400 hover:bg-neutral-100 dark:hover:bg-neutral-800" data-bs-dismiss="offcanvas" aria-label="Close">
            <Menu className="h-4 w-4" />
          </button>
        </div>
        <div className="offcanvas-body flex flex-col p-0">
          <SidebarContent {...sidebarProps} closeOnNav />
        </div>
      </div>

      {/* desktop rail */}
      <aside className="hidden w-60 shrink-0 flex-col border-r border-neutral-200/80 bg-white dark:border-neutral-800 dark:bg-neutral-900 lg:flex">
        <SidebarContent {...sidebarProps} />
      </aside>

      {/* Main */}
      <main className="min-w-0 flex-1 overflow-x-hidden overflow-y-auto bg-neutral-50 dark:bg-neutral-950">
        <div className="pg-page mx-auto max-w-6xl p-4 sm:p-6">
          <Outlet />
        </div>
      </main>
    </div>
  )
}
