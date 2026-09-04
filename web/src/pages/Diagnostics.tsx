import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Stethoscope, Globe, Wifi, Lock, FileSearch, Server } from 'lucide-react'
import { api, fmtBytes, type DiagCheck, type DiagRequest } from '../api'
import { Button, Card, CardHeader, Field, Input, Select, Spinner } from '../components/ui'
import { useToast } from '../components/toast'

const CHECKS: { id: DiagCheck; label: string; icon: any; desc: string; needsPort: boolean }[] = [
  { id: 'tcp', label: 'TCP', icon: Wifi, desc: 'TCP connection + latency', needsPort: true },
  { id: 'dns', label: 'DNS', icon: Globe, desc: 'Resolve hostname to IPs', needsPort: false },
  { id: 'tls', label: 'TLS', icon: Lock, desc: 'Handshake, certificate, cipher', needsPort: true },
  { id: 'http', label: 'HTTP', icon: FileSearch, desc: 'Status code, headers, latency', needsPort: true },
  { id: 'backend', label: 'Full backend test', icon: Server, desc: 'DNS → TCP → TLS → HTTP in one go', needsPort: true },
]

function KV({ k, v }: { k: string; v: any }) {
  return (
    <div className="contents">
      <span className="text-slate-400">{k}</span>
      <span className="font-mono text-slate-700 dark:text-slate-200">{v}</span>
    </div>
  )
}

function ResultView({ check, res }: { check: DiagCheck; res: Record<string, any> }) {
  const latency = (x: any) => (typeof x?.latency_ms === 'number' ? `${x.latency_ms.toFixed(1)} ms` : '—')
  const okBadge = (v: any) =>
    v?.skipped ? <span className="text-2xs text-slate-400">skipped</span>
      : v?.success ? <span className="text-2xs text-emerald-500">✓ OK</span>
      : <span className="text-2xs text-red-500">✗ fail{v?.error ? `: ${v.error}` : ''}</span>

  if (check === 'backend') {
    const dns = res.dns, tcp = res.tcp, tls = res.tls, http = res.http
    return (
      <div className="space-y-3">
        <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs sm:grid-cols-4">
          <div className="contents">
            <span className="text-slate-400">DNS</span>
            <span>{okBadge(dns)} {(dns?.ips || []).join(', ')}</span>
          </div>
          <div className="contents"><span className="text-slate-400">TCP</span><span>{okBadge(tcp)} {latency(tcp)}</span></div>
          <div className="contents"><span className="text-slate-400">TLS</span><span>{okBadge(tls)} {latency(tls)}</span></div>
          <div className="contents"><span className="text-slate-400">HTTP</span><span>{okBadge(http)} {http?.status ? `${http.status} · ${latency(http)}` : ''}</span></div>
        </div>
        {tls?.cert && typeof tls.cert === 'object' && (
          <div className="grid grid-cols-2 gap-x-4 gap-y-1 rounded-lg bg-slate-50 p-3 text-xs dark:bg-slate-800">
            <KV k="TLS" v={tls.protocol || '—'} />
            <KV k="Cipher" v={tls.cipher || '—'} />
            <KV k="Cert subject" v={tls.cert.subject || '—'} />
            <KV k="Cert expires" v={tls.cert.not_after || '—'} />
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
      {check === 'tcp' && <><KV k="Connected" v={res.success ? '✓ yes' : '✗ no'} /><KV k="Latency" v={latency(res)} />{res.error && <KV k="Error" v={res.error} />}</>}
      {check === 'dns' && <><KV k="Resolved" v={(res.ips || []).join(', ') || '—'} /><KV k="Latency" v={latency(res)} />{res.error && <KV k="Error" v={res.error} />}</>}
      {check === 'tls' && <><KV k="Handshake" v={res.success ? '✓ ok' : '✗ failed'} /><KV k="Latency" v={latency(res)} /><KV k="Protocol" v={res.protocol || '—'} /><KV k="Cipher" v={res.cipher || '—'} /><KV k="Cert expires" v={res.cert?.not_after || '—'} />{res.error && <KV k="Error" v={res.error} />}</>}
      {check === 'http' && <><KV k="Status" v={res.status ? `${res.status} ${res.reason || ''}` : '—'} /><KV k="Latency" v={latency(res)} />{res.error && <KV k="Error" v={res.error} />}</>}
    </div>
  )
}

export default function Diagnostics() {
  const { push } = useToast()
  const [check, setCheck] = useState<DiagCheck>('backend')
  const [host, setHost] = useState('')
  const [port, setPort] = useState('80')
  const [path, setPath] = useState('/')
  const [useTLS, setUseTLS] = useState(false)
  const [result, setResult] = useState<{ check: DiagCheck; res: Record<string, any> } | null>(null)

  const run = useMutation({
    mutationFn: (r: DiagRequest) => api.diagnostics(r),
    onSuccess: (res) => setResult({ check, res }),
    onError: (e: any) => push('error', e.message),
  })

  const submit = () => {
    const p = parseInt(port, 10) || 0
    run.mutate({
      check,
      host: host.trim(),
      port: CHECKS.find((c) => c.id === check)?.needsPort ? p : undefined,
      use_tls: useTLS,
      path: path || '/',
    })
  }

  const active = CHECKS.find((c) => c.id === check)!

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-lg font-bold">Network Diagnostics</h1>
        <p className="text-xs text-slate-500 dark:text-slate-400">
          Probe backends and upstreams directly from the server — TCP, DNS, TLS, HTTP or the full chain.
        </p>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-1">
          <CardHeader title="Check" desc="Pick a probe type" />
          <div className="space-y-1.5 p-4">
            {CHECKS.map((c) => (
              <button
                key={c.id}
                onClick={() => setCheck(c.id)}
                className={`flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left transition-colors ${
                  check === c.id
                    ? 'bg-indigo-50 ring-1 ring-indigo-300 dark:bg-indigo-500/10 dark:ring-indigo-500/40'
                    : 'hover:bg-slate-50 dark:hover:bg-slate-800'
                }`}
              >
                <c.icon className={`h-4 w-4 ${check === c.id ? 'text-indigo-500' : 'text-slate-400'}`} />
                <div className="min-w-0">
                  <div className="text-sm font-medium">{c.label}</div>
                  <div className="truncate text-2xs text-slate-400">{c.desc}</div>
                </div>
              </button>
            ))}
          </div>
        </Card>

        <Card className="lg:col-span-2">
          <CardHeader title="Target" desc={`${active.label} — ${active.desc}`} />
          <div className="space-y-3 p-5">
            <div className="grid grid-cols-2 gap-3">
              <Field label="Host">
                <Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="example.com or 10.0.0.5" />
              </Field>
              {active.needsPort && (
                <Field label="Port">
                  <Input type="number" min={1} max={65535} value={port} onChange={(e) => setPort(e.target.value)} />
                </Field>
              )}
              {!active.needsPort && (
                <Field label="(DNS only needs the hostname)">
                  <Select disabled><option>—</option></Select>
                </Field>
              )}
            </div>
            <div className="flex flex-wrap items-center gap-5">
              {(check === 'http' || check === 'backend') && (
                <Field label="Path">
                  <Input className="w-40" value={path} onChange={(e) => setPath(e.target.value)} placeholder="/" />
                </Field>
              )}
              {(check === 'http' || check === 'backend') && (
                <label className="flex items-center gap-2 pt-4 text-xs font-medium text-slate-600 dark:text-slate-300">
                  <input type="checkbox" checked={useTLS} onChange={(e) => setUseTLS(e.target.checked)} /> Use TLS/HTTPS
                </label>
              )}
            </div>
            <Button onClick={submit} disabled={run.isPending || !host.trim()}>
              <Stethoscope className="h-4 w-4" />
              {run.isPending ? 'Running…' : 'Run check'}
            </Button>

            {run.isPending && <div className="flex justify-center py-6"><Spinner /></div>}
            {result && !run.isPending && (
              <div className="border-t border-slate-100 pt-4 dark:border-slate-800">
                <ResultView check={result.check} res={result.res} />
              </div>
            )}
          </div>
        </Card>
      </div>
    </div>
  )
}
