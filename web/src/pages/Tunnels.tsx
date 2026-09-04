import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Pencil, Trash2, RefreshCw, ShieldCheck, Globe, ArrowRight, CheckCircle2, XCircle, Zap } from 'lucide-react'
import { api, type TunnelRelay, type TunnelStatus } from '../api'
import { Badge, Button, Card, CardHeader, Empty, Field, Input, Modal, Select, Spinner, Toggle } from '../components/ui'
import { useToast } from '../components/toast'

function emptyRelay(): Partial<TunnelRelay> {
  return {
    name: '',
    mode: 'tls',
    enabled: true,
    target_host: '',
    target_port: 443,
    listen_ip: '0.0.0.0',
    listen_port: 443,
    bridge_port: 21000,
    udp: false,
    host_header: '',
    domain: '',
    ssl_cert_id: null,
    notes: '',
  }
}

function StateBadge({ state }: { state?: string }) {
  if (!state) return <Badge color="slate">unknown</Badge>
  if (state === 'active' || state === 'iran' || state === 'installed')
    return <Badge color="green">{state}</Badge>
  if (state === 'foreign') return <Badge color="blue">{state}</Badge>
  return <Badge color="red">{state}</Badge>
}

function EnvRow({ label, ok, detail, missing }: { label: string; ok: boolean; detail?: string; missing?: string }) {
  return (
    <div className="flex items-center justify-between rounded-lg border border-slate-200 px-3 py-2 dark:border-slate-700">
      <span className="text-xs font-medium text-slate-600 dark:text-slate-300">{label}</span>
      {ok ? (
        <span className="flex items-center gap-1.5 text-2xs text-emerald-600 dark:text-emerald-400">
          <CheckCircle2 className="h-3.5 w-3.5" /> {detail || 'ok'}
        </span>
      ) : (
        <span className="flex items-center gap-1.5 text-2xs text-red-500">
          <XCircle className="h-3.5 w-3.5" /> {missing || 'not installed'}
        </span>
      )}
    </div>
  )
}

export default function Tunnels() {
  const qc = useQueryClient()
  const { push } = useToast()

  const status = useQuery({ queryKey: ['tunnel-status'], queryFn: () => api.tunnelStatus() })
  const relays = useQuery({ queryKey: ['relays'], queryFn: () => api.listRelays() })
  const certs = useQuery({ queryKey: ['certs'], queryFn: () => api.listCerts() })

  const [modal, setModal] = useState<null | 'create' | 'edit'>(null)
  const [draft, setDraft] = useState<Partial<TunnelRelay>>(emptyRelay())
  const [saving, setSaving] = useState(false)

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['tunnel-status'] })
    qc.invalidateQueries({ queryKey: ['relays'] })
  }

  const set = <K extends keyof TunnelRelay>(k: K, v: TunnelRelay[K]) => setDraft((d) => ({ ...d, [k]: v }))

  const save = async () => {
    setSaving(true)
    try {
      if (modal === 'create') {
        await api.createRelay(draft)
        push('success', 'Relay created. Apply to activate it.')
      } else if (modal === 'edit' && draft.id) {
        await api.updateRelay(draft.id, draft)
        push('success', 'Relay updated. Apply to activate it.')
      }
      setModal(null)
      refresh()
    } catch (e: any) {
      push('error', e.message)
    } finally {
      setSaving(false)
    }
  }

  const del = useMutation({
    mutationFn: (id: number) => api.deleteRelay(id),
    onSuccess: () => {
      push('success', 'Relay deleted. Apply to update the bridge.')
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const validate = useMutation({
    mutationFn: () => api.tunnelValidate(),
    onSuccess: (res) => {
      if (res.ok) push('success', res.note || 'Bridge config is valid.')
      else push('error', res.error || 'Validation failed')
    },
    onError: (e: any) => push('error', e.message),
  })

  const apply = useMutation({
    mutationFn: () => api.tunnelApply(),
    onSuccess: (res) => {
      push('success', `Bridge applied (${res.relays} relay(s)).`)
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const st: TunnelStatus | undefined = status.data

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold">Tunnels — Hedioum</h1>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Iran-side relays: users → this server → SOCKS5 hub → foreign egress → node.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => { status.refetch(); relays.refetch() }}>
            <RefreshCw className="h-4 w-4" /> Refresh
          </Button>
          <Button variant="secondary" onClick={() => validate.mutate()} disabled={validate.isPending || !st?.xray_installed}>
            <ShieldCheck className="h-4 w-4" /> Validate
          </Button>
          <Button variant="success" onClick={() => apply.mutate()} disabled={apply.isPending || !st?.xray_installed}>
            <Zap className="h-4 w-4" /> Apply bridge
          </Button>
          <Button onClick={() => { setDraft(emptyRelay()); setModal('create') }} disabled={!st?.xray_installed}>
            <Plus className="h-4 w-4" /> New relay
          </Button>
        </div>
      </div>

      {st && !st.xray_installed && (
        <div className="rounded-xl border border-amber-200 bg-amber-50 p-4 text-xs text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300">
          <b>xray-core is not installed on this server.</b> The bridge needs the xray binary:
          <code className="mx-1 rounded bg-amber-100 px-1.5 py-0.5 dark:bg-amber-500/20">
            bash -c "$(curl -fsSL https://github.com/XTLS/Xray-install/raw/main/install-release.sh)" @ install
          </code>
          then disable the default xray service (<code className="rounded bg-amber-100 px-1 dark:bg-amber-500/20">systemctl disable --now xray</code>) — PortGuard runs it as its own unit.
        </div>
      )}

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader title="Relays" desc="Each relay forwards one public entry to a foreign node through the tunnel" />
          {relays.isLoading ? (
            <div className="flex justify-center py-12"><Spinner /></div>
          ) : !relays.data?.length ? (
            <Empty message="No relays yet. Add one to start forwarding through the tunnel." />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-slate-200 text-left text-2xs uppercase tracking-wide text-slate-400 dark:border-slate-800">
                    <th className="px-5 py-2.5 font-medium">Name</th>
                    <th className="px-3 py-2.5 font-medium">Mode</th>
                    <th className="px-3 py-2.5 font-medium">Entry point</th>
                    <th className="px-3 py-2.5 font-medium">Foreign node</th>
                    <th className="px-3 py-2.5 font-medium">Enabled</th>
                    <th className="px-5 py-2.5 text-right font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100 dark:divide-slate-800/70">
                  {relays.data.map((r) => (
                    <tr key={r.id} className={`hover:bg-slate-50 dark:hover:bg-slate-800/40 ${!r.enabled ? 'opacity-60' : ''}`}>
                      <td className="px-5 py-3 font-medium">{r.name}</td>
                      <td className="px-3 py-3"><Badge color={r.mode === 'tls' ? 'purple' : 'blue'}>{r.mode}</Badge></td>
                      <td className="px-3 py-3 font-mono text-xs">
                        {r.mode === 'raw' ? `${r.listen_ip}:${r.listen_port}` : (
                          <span title="nginx vhost (Mappings) + local bridge port">{r.domain} → 127.0.0.1:{r.bridge_port}</span>
                        )}
                      </td>
                      <td className="px-3 py-3 font-mono text-xs">
                        {r.target_host}:{r.target_port}
                        {r.host_header && <span className="block text-2xs text-slate-400">Host: {r.host_header}</span>}
                      </td>
                      <td className="px-3 py-3">
                        <Toggle checked={r.enabled} onChange={() => api.updateRelay(r.id, { ...r, enabled: !r.enabled }).then(refresh).catch((e: any) => push('error', e.message))} />
                      </td>
                      <td className="px-5 py-3">
                        <div className="flex justify-end gap-1">
                          <Button variant="ghost" size="sm" onClick={() => { setDraft(JSON.parse(JSON.stringify(r))); setModal('edit') }}><Pencil className="h-3.5 w-3.5" /></Button>
                          <Button variant="ghost" size="sm" className="text-red-500" onClick={() => del.mutate(r.id)}><Trash2 className="h-3.5 w-3.5" /></Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>

        <Card>
          <CardHeader title="Environment" desc="Detected on this server" />
          {status.isLoading ? (
            <div className="flex justify-center py-12"><Spinner /></div>
          ) : st ? (
            <div className="space-y-2 p-4">
              <EnvRow label="hedioum-tunnel" ok={st.hedioum_installed} detail={st.hedioum_version || st.hedioum_binary} missing="not installed" />
              <EnvRow label="hedioum service" ok={st.hedioum_active === 'active'} detail={st.hedioum_active} missing={st.hedioum_active} />
              <EnvRow label="xray-core" ok={st.xray_installed} detail={st.xray_version} missing="not installed (see hint above)" />
              <EnvRow label="bridge service" ok={st.bridge_active === 'active'} detail={st.bridge_active} missing="not applied yet" />
              <EnvRow label="SOCKS5 hub" ok={!!st.socks_listening} detail={st.socks_listening} missing="no 40000-49999 loopback listener" />
              <div className="flex items-center justify-between rounded-lg border border-slate-200 px-3 py-2 dark:border-slate-700">
                <span className="text-xs font-medium text-slate-600 dark:text-slate-300">Detected role</span>
                <StateBadge state={st.role} />
              </div>
              {st.role === 'iran' && st.socks_listening && (
                <p className="rounded-lg bg-sky-50 p-2.5 text-2xs leading-relaxed text-sky-700 dark:bg-sky-500/10 dark:text-sky-300">
                  Iran hub detected. Set the SOCKS host/port in Settings → Tunnel to match ({st.socks_listening}) so the bridge routes through it.
                </p>
              )}
            </div>
          ) : null}
        </Card>
      </div>

      <Card>
        <CardHeader
          title="How the topology works"
          desc="PortGuard automates the Iran side of the hedioum-suite relay bridge"
        />
        <div className="space-y-2 p-5 text-xs leading-relaxed text-slate-600 dark:text-slate-400">
          <div className="flex flex-wrap items-center gap-2 font-mono">
            <span className="rounded-lg bg-sky-100 px-2 py-1 dark:bg-sky-500/20">user</span>
            <ArrowRight className="h-3 w-3 text-slate-400" />
            <span className="rounded-lg bg-sky-100 px-2 py-1 dark:bg-sky-500/20">Iran: entry</span>
            <ArrowRight className="h-3 w-3 text-slate-400" />
            <span className="rounded-lg bg-violet-100 px-2 py-1 dark:bg-violet-500/20">xray dokodemo (bridge)</span>
            <ArrowRight className="h-3 w-3 text-slate-400" />
            <span className="rounded-lg bg-violet-100 px-2 py-1 dark:bg-violet-500/20">SOCKS5 hub</span>
            <ArrowRight className="h-3 w-3 text-slate-400" />
            <span className="rounded-lg bg-emerald-100 px-2 py-1 dark:bg-emerald-500/20">Hedioum egress</span>
          </div>
          <p><b>raw mode</b> — the bridge listens publicly on <code>listen_ip:listen_port</code> and passes TCP straight through; TLS/REALITY stays on the foreign node.</p>
          <p><b>tls mode</b> — terminate TLS locally: create an https mapping (engine: nginx) with the same domain, proxying to <code>127.0.0.1:bridge_port</code> and the foreign node's Host header; the bridge then relays through the SOCKS hub.</p>
          <p>The bridge config (<code>/etc/hedioum-suite/portguard-bridge.json</code>) is generated, validated with <code>xray run -test</code>, backed up, applied atomically and rolled back automatically on failure — same pipeline as nginx/haproxy.</p>
        </div>
      </Card>

      <Modal open={modal !== null} onClose={() => setModal(null)} wide
        title={modal === 'create' ? 'New tunnel relay' : `Edit relay — ${draft.name}`}>
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Name" hint="letters, digits, dashes">
              <Input value={draft.name || ''} onChange={(e) => set('name', e.target.value)} placeholder="DE-01" />
            </Field>
            <Field label="Mode">
              <Select value={draft.mode} onChange={(e) => set('mode', e.target.value as 'raw' | 'tls')}>
                <option value="tls">tls — terminate TLS here (nginx vhost)</option>
                <option value="raw">raw — public TCP passthrough</option>
              </Select>
            </Field>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <Field label="Foreign node address" hint="public IP or domain of the egress/node">
              <Input value={draft.target_host || ''} onChange={(e) => set('target_host', e.target.value)} placeholder="de.example.com" />
            </Field>
            <Field label="Foreign node port">
              <Input type="number" value={draft.target_port || ''} onChange={(e) => set('target_port', parseInt(e.target.value, 10))} />
            </Field>
          </div>

          {draft.mode === 'raw' ? (
            <div className="grid grid-cols-2 gap-3">
              <Field label="Listen IP" hint="0.0.0.0 = all interfaces">
                <Input value={draft.listen_ip || '0.0.0.0'} onChange={(e) => set('listen_ip', e.target.value)} />
              </Field>
              <Field label="Listen port" hint="public port users connect to">
                <Input type="number" value={draft.listen_port || ''} onChange={(e) => set('listen_port', parseInt(e.target.value, 10))} />
              </Field>
            </div>
          ) : (
            <>
              <div className="grid grid-cols-2 gap-3">
                <Field label="Public domain" hint="server_name of the local nginx vhost">
                  <Input value={draft.domain || ''} onChange={(e) => set('domain', e.target.value)} placeholder="relay.example.com" />
                </Field>
                <Field label="Bridge port" hint="local dokodemo port (vhost proxies here)">
                  <Input type="number" value={draft.bridge_port || ''} onChange={(e) => set('bridge_port', parseInt(e.target.value, 10))} />
                </Field>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <Field label="Host header" hint="sent to the foreign node (its server_name)">
                  <Input value={draft.host_header || ''} onChange={(e) => set('host_header', e.target.value)} placeholder="node.example.com" />
                </Field>
                <Field label="TLS certificate">
                  <Select value={draft.ssl_cert_id ?? ''} onChange={(e) => set('ssl_cert_id', e.target.value ? parseInt(e.target.value, 10) : null)}>
                    <option value="">— select certificate —</option>
                    {certs.data?.map((c) => <option key={c.id} value={c.id}>{c.name} ({c.type})</option>)}
                  </Select>
                </Field>
              </div>
            </>
          )}

          <label className="flex items-center gap-2 text-xs font-medium text-slate-600 dark:text-slate-300">
            <Toggle checked={!!draft.udp} onChange={(v) => set('udp', v)} /> Forward UDP too (raw mode only meaningful)
          </label>

          <Field label="Notes (optional)">
            <Input value={draft.notes || ''} onChange={(e) => set('notes', e.target.value)} />
          </Field>

          <div className="flex justify-end gap-2 border-t border-slate-200 pt-4 dark:border-slate-700">
            <Button variant="secondary" onClick={() => setModal(null)}>Cancel</Button>
            <Button onClick={save} disabled={saving || !draft.name || !draft.target_host}>
              {saving ? 'Saving…' : 'Save relay'}
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  )
}
