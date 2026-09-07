import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import {
  LayoutDashboard, Network, ArrowLeftRight, ShieldCheck, Activity, ScrollText, Settings as SettingsIcon,
  ShieldHalf, Moon, Sun, LogOut, Wrench, Gauge, History, Stethoscope, Waypoints, Radio, Server, Package,
  GaugeCircle, GitBranch, Bell, UsersRound, Terminal, Boxes, TrendingUp, Menu, PanelLeftClose, PanelLeftOpen,
} from 'lucide-react'
import { useEffect, useState } from 'react'
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

const SIDEBAR_KEY = 'pg_sidebar_open'

function SidebarContent({ me, system, dark, onToggleTheme, onLogout, collapsed, onNavigate }: {
  me?: { username?: string; role?: string }
  system?: string
  dark: boolean
  onToggleTheme: () => void
  onLogout: () => void
  collapsed: boolean
  onNavigate?: () => void
}) {
  return (
    <>
      {/* brand header — collapses to logo-only when the rail is icon-mode */}
      <div className={`flex items-center gap-2.5 border-b border-sidebar-border px-3 py-4 ${collapsed ? 'justify-center px-2' : 'px-4'}`}>
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-sm">
          <ShieldHalf className="h-5 w-5" />
        </div>
        {!collapsed && (
          <div className="min-w-0">
            <div className="flex items-center gap-1.5">
              <span className="truncate text-sm font-bold tracking-tight">PortGuard</span>
              <span className="rounded bg-accent px-1.5 py-0.5 font-mono text-2xs font-medium text-muted-foreground" title="panel version">
                v{system || '…'}
              </span>
            </div>
            <div className="truncate text-2xs text-muted-foreground">Port & Proxy Manager</div>
          </div>
        )}
      </div>

      <nav className="pg-scroll-hide flex-1 space-y-0.5 overflow-y-auto px-2 py-2" onClick={onNavigate}>
        {nav.map(({ to, label, icon: Icon, ...rest }) => (
          <NavLink
            key={to}
            to={to}
            end={'end' in rest ? (rest as { end?: boolean }).end : false}
            title={collapsed ? label : undefined}
            className={({ isActive }) =>
              `pg-press flex items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm font-medium transition-colors ${
                collapsed ? 'justify-center px-2' : ''
              } ${
                isActive
                  ? 'bg-sidebar-accent font-medium text-sidebar-accent-foreground'
                  : 'text-sidebar-foreground/80 hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground'
              }`
            }
          >
            <Icon className="h-4 w-4 shrink-0" />
            {!collapsed && label}
          </NavLink>
        ))}
      </nav>

      <div className="border-t border-sidebar-border p-2">
        <div className={`flex items-center justify-between rounded-lg px-2 py-1.5 ${collapsed ? 'flex-col gap-2' : ''}`}>
          <span className={`min-w-0 truncate text-xs font-medium text-muted-foreground ${collapsed ? 'hidden' : ''}`}>
            {me?.username || ''}
            {me?.role && <span className="ml-1 text-2xs text-muted-foreground/70">({me.role})</span>}
          </span>
          <div className="flex shrink-0 gap-0.5">
            <button onClick={onToggleTheme} className="pg-press cursor-pointer rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-foreground" title="Toggle theme">
              {dark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
            </button>
            <button onClick={onLogout} className="pg-press cursor-pointer rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-foreground" title="Logout">
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
  // PasarGuard-style collapsible desktop rail: 16rem expanded / 3rem icon-only,
  // toggled by Ctrl+B, persisted in localStorage, remembered across reloads.
  const [open, setOpen] = useState(() => {
    try {
      const stored = localStorage.getItem(SIDEBAR_KEY)
      return stored === null ? true : stored === 'true'
    } catch {
      return true
    }
  })
  const me = useQuery({ queryKey: ['me'], queryFn: () => api.me(), staleTime: 60_000 })
  const system = useQuery({ queryKey: ['version'], queryFn: () => api.system(), staleTime: Infinity, select: (s) => s.version })

  const toggleTheme = () => {
    const next = !dark
    setDark(next)
    document.documentElement.classList.toggle('dark', next)
    localStorage.setItem('pg_theme', next ? 'dark' : 'light')
  }

  const toggleSidebar = () => {
    // functional updater reads fresh state even from the long-lived keydown
    // listener; persistence happens inside the updater
    setOpen((v) => {
      const next = !v
      try {
        localStorage.setItem(SIDEBAR_KEY, String(next))
      } catch { /* private mode — state still works in-session */ }
      return next
    })
  }

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === 'b' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        toggleSidebar()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const logout = () => {
    setToken(null)
    qc.clear()
    navigate('/login', { replace: true })
  }

  const sidebarProps = { me: me.data, system: system.data, dark, onToggleTheme: toggleTheme, onLogout: logout }

  return (
    <div className="pg-shell flex h-full flex-col lg:flex-row" data-state={open ? 'expanded' : 'collapsed'}>
      {/* mobile top bar (Bootstrap offcanvas opens the nav) */}
      <header className="sticky top-0 z-40 flex items-center justify-between gap-2 border-b border-sidebar-border bg-sidebar/80 px-3 py-2.5 backdrop-blur-md lg:hidden">
        <button
          className="pg-press inline-flex h-9 w-9 cursor-pointer items-center justify-center rounded-lg border border-border text-foreground"
          type="button"
          data-bs-toggle="offcanvas"
          data-bs-target="#pgSidebar"
          aria-controls="pgSidebar"
          title="Menu"
        >
          <Menu className="h-5 w-5" />
        </button>
        <div className="flex min-w-0 items-center gap-2">
          <ShieldHalf className="h-4 w-4 shrink-0 text-primary" />
          <span className="truncate text-sm font-bold tracking-tight">PortGuard</span>
          <span className="rounded bg-accent px-1.5 py-0.5 font-mono text-2xs font-medium text-muted-foreground">
            v{system.data || '…'}
          </span>
        </div>
        <div className="flex gap-0.5">
          <button onClick={toggleTheme} className="pg-press cursor-pointer rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-foreground" title="Toggle theme">
            {dark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          </button>
          <button onClick={logout} className="pg-press cursor-pointer rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-foreground" title="Logout">
            <LogOut className="h-4 w-4" />
          </button>
        </div>
      </header>

      {/* Bootstrap offcanvas nav: phones / tablets */}
      <div className="offcanvas offcanvas-start pg-offcanvas" tabIndex={-1} id="pgSidebar" aria-label="Navigation">
        <div className="offcanvas-header border-b border-sidebar-border">
          <span className="text-sm font-semibold">Navigation</span>
          <button type="button" className="pg-press cursor-pointer rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-foreground" data-bs-dismiss="offcanvas" aria-label="Close">
            <Menu className="h-4 w-4" />
          </button>
        </div>
        <div className="offcanvas-body flex flex-col p-0">
          <SidebarContent {...sidebarProps} collapsed={false} />
        </div>
      </div>

      {/* desktop rail — PasarGuard-style collapsible (Ctrl+B / arrow button) */}
      <aside
        className="pg-sidebar bg-sidebar text-sidebar-foreground relative hidden shrink-0 flex-col border-r border-sidebar-border lg:flex"
        style={{ width: 'var(--pg-sidebar-width)' }}
      >
        <SidebarContent {...sidebarProps} collapsed={!open} />
        {/* collapse / expand trigger pinned to the rail edge */}
        <button
          type="button"
          onClick={toggleSidebar}
          title={open ? 'Collapse sidebar (Ctrl+B)' : 'Expand sidebar (Ctrl+B)'}
          className={`pg-press absolute top-[4.25rem] z-10 flex h-6 w-6 -translate-x-1/2 cursor-pointer items-center justify-center rounded-full border border-border bg-card text-muted-foreground shadow-sm hover:text-foreground ${
            open ? 'left-full' : 'left-full'
          }`}
        >
          {open ? <PanelLeftClose className="h-3.5 w-3.5" /> : <PanelLeftOpen className="h-3.5 w-3.5" />}
        </button>
      </aside>

      {/* Main */}
      <main className="pg-main min-w-0 flex-1 overflow-x-hidden overflow-y-auto bg-background">
        <div className="pg-page mx-auto flex min-h-full max-w-6xl flex-col p-4 sm:p-6">
          <div className="flex-1">
            <Outlet />
          </div>
          {/* PasarGuard-style quiet footer */}
          <div className="relative flex w-full pt-6 pb-3">
            <p className="inline-block flex-grow text-center text-xs text-muted-foreground">
              Made with &#10084;&#65039; by&nbsp;
              <a className="text-primary hover:underline" href="https://github.com/syklonAK/portguard-v2" target="_blank" rel="noreferrer">
                PortGuard
              </a>{' '}
              Team
            </p>
          </div>
        </div>
      </main>
    </div>
  )
}
