import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { Activity, ArrowLeftRight, Cpu, HardDrive, MemoryStick, Network, ShieldCheck, Wrench } from 'lucide-react'
import { api, fmtBytes, fmtUptime, type SystemInfo } from '../api'
import { Badge, Card, CardHeader, Empty, Spinner, StatusDot } from '../components/ui'

function Stat({ icon: Icon, label, value, sub }: { icon: any; label: string; value: string; sub?: string }) {
  return (
    <Card className="p-4">
      <div className="flex items-center gap-3">
        <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-indigo-50 text-indigo-600 dark:bg-indigo-500/10 dark:text-indigo-300">
          <Icon className="h-4.5 w-4.5" />
        </div>
        <div className="min-w-0">
          <div className="truncate text-lg font-semibold leading-tight">{value}</div>
          <div className="text-2xs text-slate-400">{sub || label}</div>
        </div>
      </div>
    </Card>
  )
}

function Bar({ pct, color }: { pct: number; color: string }) {
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-slate-200 dark:bg-slate-700">
      <div className={`h-full rounded-full ${color}`} style={{ width: `${Math.min(pct, 100)}%` }} />
    </div>
  )
}

export default function Dashboard() {
  const system = useQuery({ queryKey: ['system'], queryFn: () => api.system(), refetchInterval: 5000 })
  const mappings = useQuery({ queryKey: ['mappings'], queryFn: () => api.listMappings() })
  const services = useQuery({ queryKey: ['services'], queryFn: () => api.services(), refetchInterval: 10000 })
  const certs = useQuery({ queryKey: ['certs'], queryFn: () => api.listCerts(), staleTime: 60_000 })

  if (system.isLoading) {
    return (
      <div className="flex justify-center py-20">
        <Spinner className="h-8 w-8" />
      </div>
    )
  }

  const d = system.data
  const s = d?.system
  const nearExpiry = (certs.data || []).filter((c) => {
    if (!c.expires_at) return false
    const days = Math.floor((new Date(c.expires_at).getTime() - Date.now()) / 86400000)
    return days < 30
  })

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-lg font-bold">Dashboard</h1>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            {s?.platform} · kernel {s?.kernel} · PortGuard v{d?.version}
          </p>
        </div>
        <Badge color="slate">uptime {s ? fmtUptime(s.uptime) : '—'}</Badge>
      </div>

      {/* Engine service status */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        {['nginx', 'haproxy'].map((engine) => {
          const st = services.data?.[engine]
          const active = st?.active
          return (
            <Link key={engine} to="/services">
              <Card className="flex items-center justify-between p-4 transition-shadow hover:shadow-md">
                <div className="flex items-center gap-3">
                  <div className={`flex h-9 w-9 items-center justify-center rounded-lg ${engine === 'nginx' ? 'bg-emerald-50 text-emerald-600 dark:bg-emerald-500/10 dark:text-emerald-300' : 'bg-violet-50 text-violet-600 dark:bg-violet-500/10 dark:text-violet-300'}`}>
                    <Wrench className="h-4.5 w-4.5" />
                  </div>
                  <div>
                    <div className="text-sm font-semibold">{engine}</div>
                    <div className="text-2xs text-slate-400">
                      {st?.binary_installed ? `unit ${st?.unit} · pid ${st?.pid || '—'}` : 'binary not installed'}
                    </div>
                  </div>
                </div>
                <Badge color={active === 'active' ? 'green' : active === 'failed' ? 'red' : 'slate'}>{active || 'unknown'}</Badge>
              </Card>
            </Link>
          )
        })}
      </div>

      {/* Certificate expiry warnings */}
      {nearExpiry.length > 0 && (
        <Link to="/certs">
          <Card className="border-amber-300 bg-amber-50 p-4 dark:border-amber-500/40 dark:bg-amber-900/20">
            <div className="flex items-center gap-2 text-sm text-amber-700 dark:text-amber-300">
              <ShieldCheck className="h-4 w-4" />
              <span>
                {nearExpiry.length === 1
                  ? `Certificate “${nearExpiry[0].name}” expires soon — renew it from the SSL Certs page.`
                  : `${nearExpiry.length} certificates expire soon (${nearExpiry.map((c) => c.name).join(', ')}).`}
              </span>
            </div>
          </Card>
        </Link>
      )}

      {/* System stats */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Stat icon={Cpu} label="CPU" value={`${s?.cpu_percent?.toFixed(0) ?? '—'}%`} sub={`${s?.num_cpu ?? '—'} cores · load ${s?.load1?.toFixed(2) ?? '—'}`} />
        <Stat icon={MemoryStick} label="Memory" value={s ? fmtBytes(s.mem_used) : '—'} sub={s ? `of ${fmtBytes(s.mem_total)} (${s.mem_percent.toFixed(0)}%)` : ''} />
        <Stat icon={HardDrive} label="Disk" value={s ? fmtBytes(s.disk_used) : '—'} sub={s ? `of ${fmtBytes(s.disk_total)} (${s.disk_percent.toFixed(0)}%)` : ''} />
        <Stat icon={Network} label="Ports" value={`${d?.ports.total ?? 0}`} sub={`${d?.ports.unmanaged ?? 0} unmanaged · ${d?.ports.managed ?? 0} managed`} />
      </div>

      {/* Resource bars */}
      <Card>
        <CardHeader title="Resources" desc="Live utilization (updates every 5s)" />
        <div className="space-y-4 p-5">
          <div>
            <div className="mb-1.5 flex justify-between text-xs text-slate-500 dark:text-slate-400">
              <span>CPU</span>
              <span>{s?.cpu_percent?.toFixed(1) ?? '—'}%</span>
            </div>
            <Bar pct={s?.cpu_percent ?? 0} color="bg-indigo-500" />
          </div>
          <div>
            <div className="mb-1.5 flex justify-between text-xs text-slate-500 dark:text-slate-400">
              <span>Memory</span>
              <span>{s?.mem_percent?.toFixed(1) ?? '—'}%</span>
            </div>
            <Bar pct={s?.mem_percent ?? 0} color="bg-emerald-500" />
          </div>
          <div>
            <div className="mb-1.5 flex justify-between text-xs text-slate-500 dark:text-slate-400">
              <span>Disk /</span>
              <span>{s?.disk_percent?.toFixed(1) ?? '—'}%</span>
            </div>
            <Bar pct={s?.disk_percent ?? 0} color="bg-amber-500" />
          </div>
        </div>
      </Card>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {/* Mappings overview */}
        <Card>
          <CardHeader
            title="Mappings"
            desc={`${d?.mappings.enabled ?? 0} enabled of ${d?.mappings.total ?? 0}`}
            right={
              <Link to="/mappings" className="text-xs font-medium text-indigo-500 hover:underline">
                Manage →
              </Link>
            }
          />
          {mappings.data && mappings.data.length > 0 ? (
            <div className="divide-y divide-slate-100 dark:divide-slate-800">
              {mappings.data.slice(0, 6).map((m) => (
                <div key={m.id} className="flex items-center justify-between px-5 py-2.5 text-sm">
                  <div className="flex min-w-0 items-center gap-2.5">
                    <StatusDot status={m.enabled ? 'up' : 'unknown'} />
                    <span className="truncate font-medium">{m.name}</span>
                    <Badge color={m.engine === 'nginx' ? 'green' : 'purple'}>{m.engine}</Badge>
                  </div>
                  <span className="shrink-0 font-mono text-xs text-slate-500">
                    :{m.listen_port} → {m.protocol}
                  </span>
                </div>
              ))}
            </div>
          ) : (
            <Empty message="No mappings yet — create one from the Mappings page." />
          )}
        </Card>

        {/* Health overview */}
        <Card>
          <CardHeader
            title="Backend Health"
            desc="From the last monitoring round"
            right={
              <Link to="/monitoring" className="text-xs font-medium text-indigo-500 hover:underline">
                Details →
              </Link>
            }
          />
          <div className="grid grid-cols-3 gap-3 p-5">
            <div className="rounded-lg bg-emerald-50 p-3 text-center dark:bg-emerald-900/20">
              <div className="text-2xl font-bold text-emerald-600 dark:text-emerald-300">{d?.health.up ?? 0}</div>
              <div className="text-2xs text-emerald-700/70 dark:text-emerald-400/70">up</div>
            </div>
            <div className="rounded-lg bg-red-50 p-3 text-center dark:bg-red-900/20">
              <div className="text-2xl font-bold text-red-600 dark:text-red-300">{d?.health.down ?? 0}</div>
              <div className="text-2xs text-red-700/70 dark:text-red-400/70">down</div>
            </div>
            <div className="rounded-lg bg-slate-100 p-3 text-center dark:bg-slate-800">
              <div className="text-2xl font-bold text-slate-500">{d?.health.unknown ?? 0}</div>
              <div className="text-2xs text-slate-400">unknown</div>
            </div>
          </div>
          <div className="border-t border-slate-200 px-5 py-3 text-xs text-slate-500 dark:border-slate-800 dark:text-slate-400">
            <Activity className="mr-1 inline h-3.5 w-3.5" />
            Checks run every few seconds against each target (TCP + HTTP).
          </div>
        </Card>
      </div>

      {/* Quick links */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Link to="/ports">
          <Card className="p-4 transition-shadow hover:shadow-md">
            <div className="flex items-center gap-3">
              <Network className="h-5 w-5 text-indigo-500" />
              <div>
                <div className="text-sm font-semibold">Scan Ports</div>
                <div className="text-2xs text-slate-400">Detect services & unmapped ports</div>
              </div>
            </div>
          </Card>
        </Link>
        <Link to="/mappings">
          <Card className="p-4 transition-shadow hover:shadow-md">
            <div className="flex items-center gap-3">
              <ArrowLeftRight className="h-5 w-5 text-indigo-500" />
              <div>
                <div className="text-sm font-semibold">Create Mapping</div>
                <div className="text-2xs text-slate-400">Route a domain or port to a backend</div>
              </div>
            </div>
          </Card>
        </Link>
        <Link to="/certs">
          <Card className="p-4 transition-shadow hover:shadow-md">
            <div className="flex items-center gap-3">
              <ShieldCheck className="h-5 w-5 text-indigo-500" />
              <div>
                <div className="text-sm font-semibold">SSL Certificates</div>
                <div className="text-2xs text-slate-400">Upload or generate certificates</div>
              </div>
            </div>
          </Card>
        </Link>
      </div>
    </div>
  )
}
