import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, Upload, RefreshCw, Radar } from 'lucide-react'
import { api, type ImportResult, type DiscoveredPanel } from '../api'
import { Button, Card, CardHeader, Field, Input, Spinner, Toggle } from '../components/ui'
import { useToast } from '../components/toast'

export default function Settings() {
  const qc = useQueryClient()
  const { push } = useToast()
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api.settings() })
  const fileInput = useRef<HTMLInputElement>(null)

  // scan the host for installed panels (docker + /opt .env files) and offer
  // one-click application of the discovered integration profile
  const runDiscover = async () => {
    setScanning(true)
    try {
      const res = await api.discover()
      setDiscovered(res)
      if (!res.length) push('info', 'No supported panels found on this host.')
    } catch (e: any) {
      push('error', e.message)
    } finally {
      setScanning(false)
    }
  }

  const applyDiscovered = async (p: DiscoveredPanel) => {
    try {
      await api.discoverApply({ kind: p.kind, url: p.url, username: p.env_username || undefined, env_path: p.env_path })
      // a password found in .env never leaves the server: fetch-and-store is
      // not possible client-side, so only username is pre-filled when the
      // operator has not typed one
      const fresh = await api.settings()
      qc.setQueryData(['settings'], fresh)
      setPgURL(fresh.pasarguard_url || p.url)
      setPgUser(fresh.pasarguard_username || '')
      push('success', `${p.kind} (${p.name}) applied — enter the admin password if not found in .env`)
    } catch (e: any) {
      push('error', e.message)
    }
  }

  const [paths, setPaths] = useState({
    nginx_conf: '', haproxy_conf: '', certs_dir: '', backups_dir: '', nginx_bin: '', haproxy_bin: '', haproxy_socket: '',
  })
  const [checkInterval, setCheckInterval] = useState('30')
  const [scanInterval, setScanInterval] = useState('300')
  const [autoApply, setAutoApply] = useState(false)
  const [socksHost, setSocksHost] = useState('127.0.0.1')
  const [socksPort, setSocksPort] = useState('40001')
  const [discovered, setDiscovered] = useState<DiscoveredPanel[] | null>(null)
  const [scanning, setScanning] = useState(false)
  const [pgURL, setPgURL] = useState('')
  const [pgUser, setPgUser] = useState('')
  const [pgPass, setPgPass] = useState('')
  const [pgToken, setPgToken] = useState('')
  const [rlEnabled, setRlEnabled] = useState(false)
  const [rlSync, setRlSync] = useState('60')
  const [alertsEnabled, setAlertsEnabled] = useState(true)
  const [alertCooldown, setAlertCooldown] = useState('10')
  const [tgToken, setTgToken] = useState('')
  const [tgChat, setTgChat] = useState('')
  const [webhookURL, setWebhookURL] = useState('')
  const [cpuMin, setCpuMin] = useState('0')
  const [ramMin, setRamMin] = useState('0')
  const [diskMin, setDiskMin] = useState('0')

  const [pw, setPw] = useState({ old: '', new: '', confirm: '' })

  useEffect(() => {
    if (settings.data) {
      setPaths(settings.data.paths)
      setCheckInterval(settings.data.check_interval)
      setScanInterval(settings.data.scan_interval)
      setAutoApply(settings.data.auto_apply === 'true')
      setSocksHost(settings.data.tunnel_socks_host || '127.0.0.1')
      setSocksPort(settings.data.tunnel_socks_port || '40001')
      setPgURL(settings.data.pasarguard_url || '')
      setPgUser(settings.data.pasarguard_username || '')
      setRlEnabled(settings.data.rate_limiting_enabled === 'true')
      setRlSync(settings.data.rate_limiting_sync_interval || '60')
      setAlertsEnabled((settings.data.alerts_enabled ?? 'true') === 'true')
      setAlertCooldown(settings.data.alert_cooldown_min || '10')
      setTgChat(settings.data.alert_telegram_chat || '')
      setCpuMin(settings.data.alert_cpu_min || '0')
      setRamMin(settings.data.alert_ram_min || '0')
      setDiskMin(settings.data.alert_disk_min || '0')
    }
  }, [settings.data])

  const save = useMutation({
    mutationFn: () =>
      api.putSettings({
        paths,
        check_interval: checkInterval,
        scan_interval: scanInterval,
        auto_apply: autoApply ? 'true' : 'false',
        tunnel_socks_host: socksHost,
        tunnel_socks_port: socksPort,
        pasarguard_url: pgURL,
        pasarguard_username: pgUser,
        ...(pgPass.trim() ? { pasarguard_password: pgPass.trim() } : {}),
        ...(pgToken.trim() ? { pasarguard_token: pgToken.trim() } : {}),
        rate_limiting_enabled: rlEnabled ? 'true' : 'false',
        rate_limiting_sync_interval: rlSync,
        alerts_enabled: alertsEnabled ? 'true' : 'false',
        alert_cooldown_min: alertCooldown,
        ...(tgToken.trim() ? { alert_telegram_token: tgToken.trim() } : {}),
        alert_telegram_chat: tgChat,
        ...(webhookURL.trim() ? { alert_webhook_url: webhookURL.trim() } : {}),
        alert_cpu_min: cpuMin,
        alert_ram_min: ramMin,
        alert_disk_min: diskMin,
      }),
    onSuccess: () => {
      push('success', 'Settings saved')
      qc.invalidateQueries({ queryKey: ['settings'] })
    },
    onError: (e: any) => push('error', e.message),
  })

  const selfUpdate = useMutation({
    mutationFn: () => api.selfUpdate(),
    onSuccess: (res) => {
      push('info', res.note || 'Update started — the panel will restart in a few seconds.')
    },
    onError: (e: any) => push('error', e.message),
  })

  const changePw = useMutation({
    mutationFn: () => api.changePassword(pw.old, pw.new),
    onSuccess: () => {
      push('success', 'Password changed')
      setPw({ old: '', new: '', confirm: '' })
    },
    onError: (e: any) => push('error', e.message),
  })

  const exportAll = async () => {
    try {
      const data = await api.exportSnapshot()
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'portguard-snapshot.json'
      a.click()
      URL.revokeObjectURL(url)
      push('success', 'Snapshot exported (certificate private keys are never included)')
    } catch (e: any) {
      push('error', e.message)
    }
  }

  const importFile = useMutation({
    mutationFn: async (file: File) => {
      const text = await file.text()
      const parsed = JSON.parse(text)
      const mappings = Array.isArray(parsed) ? parsed : parsed.mappings
      if (!Array.isArray(mappings)) throw new Error('No mappings array found in file')
      return api.importSnapshot(mappings)
    },
    onSuccess: (res: ImportResult) => {
      push(res.errors.length ? 'warning' : 'success',
        `Imported ${res.created} mapping(s), skipped ${res.skipped}` +
        (res.errors.length ? ` — ${res.errors.length} error(s): ${res.errors[0]}` : ''))
      qc.invalidateQueries({ queryKey: ['mappings'] })
    },
    onError: (e: any) => push('error', e.message),
  })

  if (settings.isLoading) {
    return <div className="flex justify-center py-20"><Spinner className="h-8 w-8" /></div>
  }

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-lg font-bold">Settings</h1>
        <p className="text-xs text-slate-500 dark:text-slate-400">Engine paths, monitoring intervals and account</p>
      </div>

      <Card>
        <CardHeader title="Proxy engine paths" desc="Locations on this server used to stage and validate configs" />
        <div className="grid grid-cols-1 gap-3 p-5 sm:grid-cols-2">
          <Field label="nginx.conf path"><Input value={paths.nginx_conf} onChange={(e) => setPaths({ ...paths, nginx_conf: e.target.value })} /></Field>
          <Field label="nginx binary"><Input value={paths.nginx_bin} onChange={(e) => setPaths({ ...paths, nginx_bin: e.target.value })} /></Field>
          <Field label="haproxy.cfg path"><Input value={paths.haproxy_conf} onChange={(e) => setPaths({ ...paths, haproxy_conf: e.target.value })} /></Field>
          <Field label="haproxy binary"><Input value={paths.haproxy_bin} onChange={(e) => setPaths({ ...paths, haproxy_bin: e.target.value })} /></Field>
          <Field label="Certificates directory"><Input value={paths.certs_dir} onChange={(e) => setPaths({ ...paths, certs_dir: e.target.value })} /></Field>
          <Field label="Backups directory"><Input value={paths.backups_dir} onChange={(e) => setPaths({ ...paths, backups_dir: e.target.value })} /></Field>
          <Field label="HAProxy runtime socket" hint="Used for live stats & server maintenance (Runtime page)">
            <Input value={paths.haproxy_socket} onChange={(e) => setPaths({ ...paths, haproxy_socket: e.target.value })} />
          </Field>
        </div>
      </Card>

      <Card>
        <CardHeader title="Import / Export" desc="Move your mapping setup between servers (certificates are exported without private keys)" />
        <div className="flex flex-wrap gap-2 p-5">
          <Button variant="secondary" onClick={exportAll}>
            <Download className="h-4 w-4" /> Export snapshot
          </Button>
          <Button variant="secondary" onClick={() => fileInput.current?.click()} disabled={importFile.isPending}>
            <Upload className="h-4 w-4" /> {importFile.isPending ? 'Importing…' : 'Import snapshot'}
          </Button>
          <input
            ref={fileInput}
            type="file"
            accept="application/json,.json"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0]
              e.target.value = ''
              if (f) importFile.mutate(f)
            }}
          />
          <p className="w-full text-2xs text-slate-400">
            Imported mappings are validated first (name/port conflicts are skipped or reported) and saved without applying — press Apply on the Mappings page to activate.
          </p>
        </div>
      </Card>

      <Card>
        <CardHeader title="Automation" desc="Monitoring and apply behavior" />
        <div className="grid grid-cols-1 gap-3 p-5 sm:grid-cols-2">
          <Field label="Health check interval (seconds, min 5)">
            <Input type="number" value={checkInterval} min={5} onChange={(e) => setCheckInterval(e.target.value)} />
          </Field>
          <Field label="Port scan interval (seconds, min 30)">
            <Input type="number" value={scanInterval} min={30} onChange={(e) => setScanInterval(e.target.value)} />
          </Field>
          <label className="flex items-center gap-2.5 text-sm">
            <Toggle checked={autoApply} onChange={setAutoApply} />
            <span className="text-xs font-medium text-slate-600 dark:text-slate-300">
              Auto-apply configuration after every mapping change
            </span>
          </label>
        </div>
      </Card>

      <Card>
        <CardHeader title="Tunnel (Hedioum)" desc="SOCKS5 hub the Iran-side bridge routes through — must match your hedioum setup-iran port" />
        <div className="grid grid-cols-1 gap-3 p-5 sm:grid-cols-2">
          <Field label="SOCKS5 host" hint="usually 127.0.0.1 (the hub listens on loopback)">
            <Input value={socksHost} onChange={(e) => setSocksHost(e.target.value)} />
          </Field>
          <Field label="SOCKS5 port" hint="default 40001, range 40000-49999">
            <Input type="number" value={socksPort} onChange={(e) => setSocksPort(e.target.value)} />
          </Field>
        </div>
        <div className="border-t border-slate-200 px-5 py-4 dark:border-slate-800">
          <Button onClick={() => save.mutate()} disabled={save.isPending}>
            {save.isPending ? 'Saving…' : 'Save settings'}
          </Button>
        </div>
      </Card>

      <Card>
        <CardHeader
          title="Alerting"
          desc="Node offline, backend down, certificate expiry, resource thresholds — Telegram and webhook channels"
        />
        <div className="space-y-3 p-5">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <label className="flex items-center gap-2 pb-1.5 text-xs font-medium text-slate-600 dark:text-slate-300">
              <Toggle checked={alertsEnabled} onChange={setAlertsEnabled} /> Enable alerting
            </label>
            <Field label="Cooldown (minutes, 1-1440)" hint="min gap between repeat alerts of the same event">
              <Input type="number" value={alertCooldown} min={1} onChange={(e) => setAlertCooldown(e.target.value)} />
            </Field>
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field
              label="Telegram bot token"
              hint={settings.data?.alert_telegram_token_set ? 'configured — type a new one to rotate' : 'from @BotFather'}
            >
              <Input type="password" value={tgToken} onChange={(e) => setTgToken(e.target.value)} placeholder="123456:ABC-DEF…" />
            </Field>
            <Field label="Telegram chat ID" hint="the chat/group that receives alerts">
              <Input value={tgChat} onChange={(e) => setTgChat(e.target.value)} placeholder="-1001234567890" />
            </Field>
          </div>
          <Field
            label="Webhook URL"
            hint={settings.data?.alert_webhook_set ? 'configured — a new URL replaces it (Discord/Slack/generic)' : 'POSTs JSON {severity,title,detail,…}'}
          >
            <Input type="password" value={webhookURL} onChange={(e) => setWebhookURL(e.target.value)} placeholder="https://hooks.example.com/…" />
          </Field>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <Field label="CPU ≥ % (0=off)"><Input type="number" value={cpuMin} min={0} max={100} onChange={(e) => setCpuMin(e.target.value || '0')} /></Field>
            <Field label="RAM ≥ % (0=off)"><Input type="number" value={ramMin} min={0} max={100} onChange={(e) => setRamMin(e.target.value || '0')} /></Field>
            <Field label="Disk ≥ % (0=off)"><Input type="number" value={diskMin} min={0} max={100} onChange={(e) => setDiskMin(e.target.value || '0')} /></Field>
          </div>
        </div>
      </Card>

      <Card>
        <CardHeader
          title="Bandwidth / PasarGuard"
          desc="User sync source and per-UUID rate limiting (enforced on nodes with Linux tc)"
          right={
            <Button variant="secondary" size="sm" onClick={runDiscover} disabled={scanning}>
              <Radar className={`h-3.5 w-3.5 ${scanning ? 'animate-pulse' : ''}`} />
              {scanning ? 'Scanning…' : 'Auto-detect panels'}
            </Button>
          }
        />
        {discovered && discovered.length > 0 && (
          <div className="space-y-2 border-b border-slate-200 px-5 py-3 dark:border-slate-800">
            <p className="text-2xs uppercase tracking-wide text-slate-400">Detected on this host</p>
            {discovered.map((p) => (
              <div key={p.source + p.name} className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-slate-200 px-3 py-2 text-xs dark:border-slate-700">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-semibold">{p.kind}</span>
                  <span className="text-slate-400">·</span>
                  <span className="font-mono">{p.url || p.name}</span>
                  <span className={`rounded px-1.5 py-0.5 text-2xs ${p.alive ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' : 'bg-slate-100 text-slate-500 dark:bg-slate-800'}`}>
                    {p.alive ? 'API alive' : 'not responding'}
                  </span>
                  {p.env_has_password && <span className="text-2xs text-indigo-500">credentials in .env</span>}
                  {p.note && <span className="text-2xs text-slate-400">{p.note}</span>}
                </div>
                <Button size="sm" variant="secondary" disabled={!p.alive} onClick={() => applyDiscovered(p)}>
                  Apply
                </Button>
              </div>
            ))}
          </div>
        )}
        <div className="grid grid-cols-1 gap-3 p-5 sm:grid-cols-2">
          <Field label="PasarGuard panel URL" hint="admin API base, e.g. https://127.0.0.1:2096 — self-signed certs are accepted automatically">
            <Input value={pgURL} onChange={(e) => setPgURL(e.target.value)} placeholder="https://127.0.0.1:2096" />
          </Field>
          <Field label="PasarGuard admin username" hint="used to mint/refresh API tokens automatically">
            <Input value={pgUser} onChange={(e) => setPgUser(e.target.value)} placeholder="admin" />
          </Field>
          <Field
            label="PasarGuard admin password"
            hint={settings.data?.pasarguard_password_set ? 'configured — type a new one to rotate' : 'preferred over a manual token'}
          >
            <Input type="password" value={pgPass} onChange={(e) => setPgPass(e.target.value)} placeholder="••••••" />
          </Field>
          <Field
            label="PasarGuard API token (optional)"
            hint={settings.data?.pasarguard_token_set ? 'configured — auto-refreshed on expiry' : 'optional when username/password are set'}
          >
            <Input type="password" value={pgToken} onChange={(e) => setPgToken(e.target.value)} placeholder="••••••" />
          </Field>
          <label className="flex items-center gap-2 text-xs font-medium text-slate-600 dark:text-slate-300">
            <Toggle checked={rlEnabled} onChange={setRlEnabled} />
            Enable bandwidth limiting (auto sync + push)
          </label>
          <Field label="Sync/push interval (seconds, min 10)">
            <Input type="number" value={rlSync} min={10} onChange={(e) => setRlSync(e.target.value)} />
          </Field>
        </div>
      </Card>

      <Card>
        <CardHeader
          title="Panel updater"
          desc="Pull the latest release from GitHub, rebuild and restart — no reinstall needed"
        />
        <div className="flex flex-wrap items-center gap-3 p-5">
          <Button variant="secondary" onClick={() => selfUpdate.mutate()} disabled={selfUpdate.isPending}>
            <RefreshCw className={`h-4 w-4 ${selfUpdate.isPending ? 'animate-spin' : ''}`} />
            {selfUpdate.isPending ? 'Starting…' : 'Check & update now'}
          </Button>
          <p className="w-full text-2xs text-slate-400">
            Runs <code>deploy/update.sh</code>: git pull → rebuild binary (embedded frontend, no Node needed) → restart the systemd service.
            The panel goes down for a few seconds and comes back on the same port.
          </p>
        </div>
      </Card>

      <Card>
        <CardHeader title="Change password" desc="Your admin account credentials" />
        <div className="grid grid-cols-1 gap-3 p-5 sm:grid-cols-3">
          <Field label="Current password">
            <Input type="password" value={pw.old} onChange={(e) => setPw({ ...pw, old: e.target.value })} />
          </Field>
          <Field label="New password (8+ chars)">
            <Input type="password" value={pw.new} onChange={(e) => setPw({ ...pw, new: e.target.value })} />
          </Field>
          <Field label="Confirm new password">
            <Input type="password" value={pw.confirm} onChange={(e) => setPw({ ...pw, confirm: e.target.value })} />
          </Field>
        </div>
        <div className="border-t border-slate-200 px-5 py-4 dark:border-slate-800">
          <Button
            variant="secondary"
            onClick={() => {
              if (pw.new !== pw.confirm) {
                push('error', 'New passwords do not match')
                return
              }
              changePw.mutate()
            }}
            disabled={changePw.isPending || !pw.old || !pw.new}
          >
            {changePw.isPending ? 'Changing…' : 'Change password'}
          </Button>
        </div>
      </Card>
    </div>
  )
}
