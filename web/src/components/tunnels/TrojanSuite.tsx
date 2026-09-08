import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Pencil, Trash2, Zap, ShieldCheck, RefreshCw, CheckCircle2, XCircle, Gauge, Flame, Globe2, Wind, Stethoscope, Terminal, Bomb } from 'lucide-react'
import { api, type TrojanRelayRow, type TrojanIngressRow, type BBRStatus, type TunnelStatus, type RelayVerifyReport, type HedioumToolResult } from '../../api'
import { Badge, Button, Card, CardHeader, Empty, Field, Input, Modal, Select, Spinner, Toggle } from '../ui'
import { useToast } from '../toast'

/** Trojan L4 relay suite (hedioum-allinone port): Iran-side relays rendered
 *  through the nginx stream bridge + foreign-side ingress forwarders, plus
 *  hedioum hub wizards, BBR tuning and firewall helpers. Presentation of
 *  all state lives here; mutations go through the shared api client. */

function emptyTrojanRelay(): Partial<TrojanRelayRow> {
  return {
    name: '', domain: '', https_port: 443, foreign_ip: '', foreign_port: 10000,
    bridge_port: 0, route: '', socks_port: 0, enabled: true, notes: '',
  }
}

function emptyTrojanIngress(): Partial<TrojanIngressRow> {
  return { name: '', listen_ip: '0.0.0.0', listen_port: 35000, target_host: '127.0.0.1', target_port: 10000, udp: false, enabled: true, notes: '' }
}

export function EnvRowInline({ label, ok, detail, missing }: { label: string; ok: boolean; detail?: string; missing?: string }) {
  return (
    <div className="flex items-center justify-between rounded-lg border border-border px-3 py-2">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      {ok ? (
        <span className="flex items-center gap-1.5 text-2xs text-emerald-600 dark:text-emerald-400">
          <CheckCircle2 className="h-3.5 w-3.5" /> {detail || 'ok'}
        </span>
      ) : (
        <span className="flex items-center gap-1.5 text-2xs text-red-500">
          <XCircle className="h-3.5 w-3.5" /> {missing || 'no'}
        </span>
      )}
    </div>
  )
}

export default function TrojanSuite({ status }: { status?: TunnelStatus }) {
  const qc = useQueryClient()
  const { push } = useToast()

  const relays = useQuery({ queryKey: ['trojan-relays'], queryFn: () => api.listTrojanRelays() })
  const ingresses = useQuery({ queryKey: ['trojan-ingresses'], queryFn: () => api.listTrojanIngresses() })
  const egress = useQuery({ queryKey: ['hedioum-egress'], queryFn: () => api.hedioumEgress(), refetchInterval: 30_000 })
  const bbr = useQuery({ queryKey: ['bbr'], queryFn: () => api.bbrStatus() })
  const fw = useQuery({ queryKey: ['firewall'], queryFn: () => api.firewallStatus() })

  // relay modal
  const [relayModal, setRelayModal] = useState<null | 'create' | 'edit'>(null)
  const [relayDraft, setRelayDraft] = useState<Partial<TrojanRelayRow>>(emptyTrojanRelay())
  const [savingRelay, setSavingRelay] = useState(false)
  // ingress modal
  const [ingModal, setIngModal] = useState<null | 'create'>(null)
  const [ingDraft, setIngDraft] = useState<Partial<TrojanIngressRow>>(emptyTrojanIngress())
  // hedioum wizard state
  const [wizard, setWizard] = useState<null | 'foreign' | 'iran'>(null)
  const [iranDraft, setIranDraft] = useState({ alias: 'relay01', token: '', force: false })
  const [pairToken, setPairToken] = useState('')
  const [pairInfo, setPairInfo] = useState<{ exit_ip?: string; persona?: string; endpoints?: Record<string, number>; is_v2?: boolean } | null>(null)
  const [fwPort, setFwPort] = useState('')
  // hedioum-suite v3 extras: verify modal, tool output modal, purge confirm
  const [verifyRep, setVerifyRep] = useState<RelayVerifyReport | null>(null)
  const [verifying, setVerifying] = useState(false)
  const [toolModal, setToolModal] = useState<null | { title: string; output: string; ok: boolean }>(null)
  const [toolBusy, setToolBusy] = useState<string | null>(null)
  const [purgeConfirm, setPurgeConfirm] = useState(false)
  const [purging, setPurging] = useState(false)

  const hygiene = useQuery({ queryKey: ['node-hygiene'], queryFn: () => api.nodeHygiene() })

  const rSet = <K extends keyof TrojanRelayRow>(k: K, v: TrojanRelayRow[K]) => setRelayDraft((d) => ({ ...d, [k]: v }))

  const verifyRelay = async (r: TrojanRelayRow) => {
    setVerifying(true)
    try {
      const rep = await api.trojanRelayVerify(r.id)
      setVerifyRep(rep)
    } catch (e: any) {
      push('error', e.message)
    } finally {
      setVerifying(false)
    }
  }

  const runTool = async (title: string, fn: () => Promise<HedioumToolResult>) => {
    setToolBusy(title)
    try {
      const res = await fn()
      setToolModal({ title, output: res.output || res.error || '(no output)', ok: res.ok })
    } catch (e: any) {
      push('error', e.message)
    } finally {
      setToolBusy(null)
    }
  }

  const runPurge = async () => {
    setPurging(true)
    try {
      const res = await api.trojanPurge()
      push('success', `Purged ${res.relays_removed} relay(s) and ${res.ingresses_removed} forwarder(s). Hedioum/nginx/node kept.`)
      setPurgeConfirm(false)
      qc.invalidateQueries({ queryKey: ['trojan-relays'] })
      qc.invalidateQueries({ queryKey: ['trojan-ingresses'] })
    } catch (e: any) {
      push('error', e.message)
    } finally {
      setPurging(false)
    }
  }

  const saveRelay = async () => {
    setSavingRelay(true)
    try {
      if (relayModal === 'create') {
        await api.createTrojanRelay(relayDraft)
        push('success', 'Trojan relay saved. Apply bridge to activate.')
      } else if (relayModal === 'edit' && relayDraft.id) {
        await api.updateTrojanRelay(relayDraft.id, relayDraft)
        push('success', 'Trojan relay updated. Apply bridge to activate.')
      }
      setRelayModal(null)
      qc.invalidateQueries({ queryKey: ['trojan-relays'] })
    } catch (e: any) {
      push('error', e.message)
    } finally {
      setSavingRelay(false)
    }
  }

  const delRelay = async (id: number) => {
    try {
      await api.deleteTrojanRelay(id)
      push('success', 'Relay removed. Apply bridge to update.')
      qc.invalidateQueries({ queryKey: ['trojan-relays'] })
    } catch (e: any) {
      push('error', e.message)
    }
  }

  const saveIngress = async () => {
    try {
      await api.createTrojanIngress(ingDraft)
      push('success', 'Ingress forwarder saved. Apply ingress to activate.')
      setIngModal(null)
      qc.invalidateQueries({ queryKey: ['trojan-ingresses'] })
    } catch (e: any) {
      push('error', e.message)
    }
  }

  const delIngress = async (id: number) => {
    try {
      await api.deleteTrojanIngress(id)
      push('success', 'Forwarder removed.')
      qc.invalidateQueries({ queryKey: ['trojan-ingresses'] })
    } catch (e: any) {
      push('error', e.message)
    }
  }

  const bridgeApply = useMutation({
    mutationFn: () => api.trojanBridgeApply(),
    onSuccess: (res) => {
      push(res.note ? 'info' : 'success', res.note || `Trojan bridge applied (${res.relays} relay(s)).`)
      qc.invalidateQueries({ queryKey: ['trojan-relays'] })
    },
    onError: (e: any) => push('error', e.message),
  })
  const bridgeValidate = useMutation({
    mutationFn: () => api.trojanBridgeValidate(),
    onSuccess: (res) => {
      if (res.ok) push('success', res.note || 'Trojan bridge config is valid.')
      else push('error', res.error || 'Validation failed')
    },
    onError: (e: any) => push('error', e.message),
  })
  const ingressApply = useMutation({
    mutationFn: () => api.trojanIngressApply(),
    onSuccess: (res) => {
      push(res.note ? 'info' : 'success', res.note || `Trojan ingress applied (${res.forwarders} forwarder(s)).`)
      qc.invalidateQueries({ queryKey: ['trojan-ingresses'] })
    },
    onError: (e: any) => push('error', e.message),
  })

  const runForeign = useMutation({
    mutationFn: (opts?: { force?: boolean }) => api.hedioumSetupForeign(opts),
    onSuccess: (res) => {
      setPairToken(res.token || '')
      setPairInfo(res.token_is_v2
        ? { exit_ip: res.exit_ip, persona: res.persona, endpoints: res.endpoints, is_v2: true }
        : { is_v2: false })
      push(res.ok ? 'success' : 'warning', res.ok ? 'Egress configured — pairing token captured below.' : 'setup-foreign reported problems.')
      qc.invalidateQueries({ queryKey: ['tunnel-status'] })
      qc.invalidateQueries({ queryKey: ['hedioum-egress'] })
    },
    onError: (e: any) => push('error', e.message),
  })
  const runIran = useMutation({
    mutationFn: () => api.hedioumSetupIran(iranDraft),
    onSuccess: (res) => {
      push(res.ok ? 'success' : 'error', res.ok
        ? `Hub configured — SOCKS ${res.socks}${res.egress_ip ? `, exit IP ${res.egress_ip}` : ''}.`
        : 'setup-iran failed (check the token).')
      setWizard(null)
      qc.invalidateQueries({ queryKey: ['tunnel-status'] })
      qc.invalidateQueries({ queryKey: ['hedioum-egress'] })
    },
    onError: (e: any) => push('error', e.message),
  })

  const bbrApply = useMutation({
    mutationFn: () => api.bbrApply(),
    onSuccess: (res) => {
      push(res.ok ? 'success' : 'warning', res.note || res.error || 'Tuning applied.')
      qc.invalidateQueries({ queryKey: ['bbr'] })
    },
    onError: (e: any) => push('error', e.message),
  })
  const fwAllow = useMutation({
    mutationFn: () => api.firewallAllow({ port: parseInt(fwPort, 10) }),
    onSuccess: (res) => {
      push('success', `${res.firewall}: ${res.message}`)
      setFwPort('')
    },
    onError: (e: any) => push('error', e.message),
  })

  const b: BBRStatus | undefined = bbr.data

  return (
    <>
      <Card>
        <CardHeader
          title="Trojan L4 relays"
          desc="user → domain:443 (TLS) → local bridge → tunnel/direct → foreign node inbound — the hedioum-allinone 'trojan' suite"
          right={
            <div className="flex gap-2">
              <Button variant="secondary" size="sm" onClick={() => bridgeValidate.mutate()} disabled={bridgeValidate.isPending}>
                <ShieldCheck className="h-3.5 w-3.5" /> Validate
              </Button>
              <Button variant="success" size="sm" onClick={() => bridgeApply.mutate()} disabled={bridgeApply.isPending}>
                <Zap className={`h-3.5 w-3.5 ${bridgeApply.isPending ? 'animate-pulse' : ''}`} /> Apply bridge
              </Button>
              <Button size="sm" onClick={() => { setRelayDraft(emptyTrojanRelay()); setRelayModal('create') }}>
                <Plus className="h-3.5 w-3.5" /> New relay
              </Button>
            </div>
          }
        />
        {relays.isLoading ? (
          <div className="flex justify-center py-8"><Spinner /></div>
        ) : !relays.data?.length ? (
          <Empty message="No trojan relays yet. Add one to serve trojan/tcp through the tunnel." />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-2xs uppercase tracking-wide text-muted-foreground">
                  <th className="px-5 py-2.5 font-medium">Name</th>
                  <th className="px-3 py-2.5 font-medium">Entry (TLS)</th>
                  <th className="px-3 py-2.5 font-medium">Foreign node</th>
                  <th className="px-3 py-2.5 font-medium">Route</th>
                  <th className="px-3 py-2.5 font-medium">Bridge</th>
                  <th className="px-3 py-2.5 font-medium">On</th>
                  <th className="px-5 py-2.5 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {relays.data.map((r) => (
                  <tr key={r.id} className={`hover:bg-muted/60 ${!r.enabled ? 'opacity-60' : ''}`}>
                    <td className="px-5 py-3 font-medium">{r.name}</td>
                    <td className="px-3 py-3 font-mono text-xs">{r.domain}:{r.https_port}</td>
                    <td className="px-3 py-3 font-mono text-xs">{r.foreign_ip}:{r.foreign_port}</td>
                    <td className="px-3 py-3">
                      <Badge color={r.route === 'direct' ? 'amber' : 'cyan'}>{r.route || 'tunnel'}</Badge>
                    </td>
                    <td className="px-3 py-3 font-mono text-xs">127.0.0.1:{r.bridge_port}</td>
                    <td className="px-3 py-3">
                      <Toggle checked={r.enabled} onChange={() => api.updateTrojanRelay(r.id, { ...r, enabled: !r.enabled })
                        .then(() => qc.invalidateQueries({ queryKey: ['trojan-relays'] }))
                        .catch((e: any) => push('error', e.message))} />
                    </td>
                    <td className="px-5 py-3">
                      <div className="flex justify-end gap-1">
                        <Button variant="ghost" size="sm" title="Verify end-to-end (through the tunnel)"
                          onClick={() => verifyRelay(r)} disabled={verifying}>
                          <Stethoscope className="h-3.5 w-3.5" />
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => { setRelayDraft(JSON.parse(JSON.stringify(r))); setRelayModal('edit') }}>
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button variant="ghost" size="sm" className="text-red-500" onClick={() => delRelay(r.id)}>
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <div className="border-t border-border px-5 py-3 text-2xs leading-relaxed text-muted-foreground">
          Master panel — node inbound: <code>trojan, listen 127.0.0.1, port {relays.data?.[0]?.foreign_port ?? 10000}, security=none</code> ·
          Host row: <code>address={relays.data?.[0]?.domain ?? 'domain'} port={relays.data?.[0]?.https_port ?? 443} security=tls sni={relays.data?.[0]?.domain ?? 'domain'}</code>
        </div>
      </Card>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader
            title="Trojan ingress forwarders (foreign side)"
            desc="public listen → node inbound on the egress box (any target host, optional UDP)"
            right={
              <Button size="sm" onClick={() => { setIngDraft(emptyTrojanIngress()); setIngModal('create') }}>
                <Plus className="h-3.5 w-3.5" /> New forwarder
              </Button>
            }
          />
          {!ingresses.data?.length ? (
            <Empty message="No forwarders — run this on the foreign node's panel." />
          ) : (
            <div className="space-y-2 p-4">
              {ingresses.data.map((i) => (
                <div key={i.id} className="flex items-center justify-between rounded-lg border border-border px-3 py-2">
                  <div className="text-xs">
                    <b>{i.name}</b>
                    <span className="ml-2 font-mono text-muted-foreground">
                      {i.listen_ip || '0.0.0.0'}:{i.listen_port} → {i.target_host}:{i.target_port}{i.udp ? ' (+udp)' : ''}
                    </span>
                  </div>
                  <div className="flex items-center gap-1">
                    <Toggle checked={i.enabled} onChange={() => api.updateTrojanIngress(i.id, { ...i, enabled: !i.enabled })
                      .then(() => qc.invalidateQueries({ queryKey: ['trojan-ingresses'] }))
                      .catch((e: any) => push('error', e.message))} />
                    <Button variant="ghost" size="sm" className="text-red-500" onClick={() => delIngress(i.id)}>
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
              ))}
              <Button variant="success" size="sm" onClick={() => ingressApply.mutate()} disabled={ingressApply.isPending}>
                <Zap className={`h-3.5 w-3.5 ${ingressApply.isPending ? 'animate-pulse' : ''}`} /> Apply ingress
              </Button>
            </div>
          )}
        </Card>

        <Card>
          <CardHeader
            title="Hedioum hub wizards"
            desc="configure the egress (foreign) or the SOCKS5 hub (iran) from the panel"
            right={
              <div className="flex gap-2">
                <Button variant="secondary" size="sm" onClick={() => { setWizard('foreign'); setPairToken('') }}>
                  <Globe2 className="h-3.5 w-3.5" /> Foreign (egress)
                </Button>
                <Button variant="secondary" size="sm" onClick={() => setWizard('iran')}>
                  <Flame className="h-3.5 w-3.5" /> Iran (hub)
                </Button>
              </div>
            }
          />
          <div className="space-y-2 p-4">
            <EnvRowInline
              label="SOCKS5 hub"
              ok={!!egress.data?.hub_alive}
              detail={egress.data?.socks}
              missing="not answering"
            />
            <EnvRowInline
              label="Tunnel egress"
              ok={!!egress.data?.egress_ip}
              detail={egress.data?.egress_ip ? `exit IP ${egress.data.egress_ip}` : undefined}
              missing="no traffic through the hub"
            />
            {wizard === 'foreign' && (
              <div className="space-y-2 rounded-xl border border-border p-3">
                <p className="text-2xs leading-relaxed text-muted-foreground">
                  Runs <code>hedioum-tunnel setup-foreign</code> on this server and captures the printed
                  <b> v2 pairing token</b> (exit IP + every mimic port + persona + key). This is for the FOREIGN node —
                  on a live Iran hub it is refused (it would overwrite the hub config) unless you tick the override.
                </p>
                {pairInfo && (
                  <div className="rounded-lg bg-sky-50 p-2.5 text-2xs leading-relaxed text-sky-800 dark:bg-sky-500/10 dark:text-sky-300">
                    {pairInfo.is_v2 ? (
                      <>Decoded token — persona <b>{pairInfo.persona || 'custom'}</b>, exit IP <b>{pairInfo.exit_ip}</b>,
                        endpoints: {Object.entries(pairInfo.endpoints || {}).map(([k, v]) => `${k}:${v}`).join(', ') || '—'}</>
                    ) : (
                      <>Legacy raw hex token captured — consider re-running setup-foreign on a newer hedioum for a v2 paste-only token.</>
                    )}
                  </div>
                )}
                <Button size="sm" onClick={() => runForeign.mutate({ force: false })} disabled={runForeign.isPending}>
                  {runForeign.isPending ? 'Running…' : 'Run setup-foreign'}
                </Button>
                {pairToken && (
                  <div className="rounded-lg bg-amber-50 p-2.5 font-mono text-xs break-all dark:bg-amber-500/10">
                    <div className="mb-1 text-2xs font-bold uppercase text-amber-700 dark:text-amber-400">Pairing token — paste into the Iran hub</div>
                    {pairToken}
                  </div>
                )}
              </div>
            )}
            {wizard === 'iran' && (
              <div className="space-y-2 rounded-xl border border-border p-3">
                <Field label="Pairing token (from the foreign node)" hint="the long base64 v2 token — raw 32-hex keys are rejected">
                  <Input value={iranDraft.token} onChange={(e) => setIranDraft({ ...iranDraft, token: e.target.value })} placeholder="paste the v2 pairing token" />
                </Field>
                <Field label="Hub alias">
                  <Input value={iranDraft.alias} onChange={(e) => setIranDraft({ ...iranDraft, alias: e.target.value })} />
                </Field>
                <label className="flex items-center gap-2 text-2xs text-amber-600 dark:text-amber-400">
                  <input type="checkbox" checked={iranDraft.force} onChange={(e) => setIranDraft({ ...iranDraft, force: e.target.checked })} />
                  Override an existing FOREIGN config on this box (dangerous)
                </label>
                <Button size="sm" onClick={() => runIran.mutate()} disabled={runIran.isPending || !iranDraft.token.trim()}>
                  {runIran.isPending ? 'Running…' : 'Run setup-iran'}
                </Button>
              </div>
            )}
          </div>
        </Card>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader
            title="Network tuning (BBR + fq)"
            desc="the classic relay tuning set — buffers, fastopen, backlog"
            right={
              <Button variant="secondary" size="sm" onClick={() => bbrApply.mutate()} disabled={bbrApply.isPending}>
                <Gauge className="h-3.5 w-3.5" /> {b?.active ? 'Re-apply' : 'Apply tuning'}
              </Button>
            }
          />
          <div className="space-y-2 p-4">
            <EnvRowInline label="sysctl drop-in" ok={!!b?.sysctl_file} detail="/etc/sysctl.d/99-portguard-tuning.conf" missing="not installed" />
            <EnvRowInline
              label="BBR"
              ok={!!b?.active}
              detail={b?.active ? `active (qdisc ${b.qdisc || 'default'})` : undefined}
              missing={b?.available ? `inactive — current: ${b.current_cc || 'unknown'}` : 'kernel lacks tcp_bbr'}
            />
          </div>
        </Card>

        <Card>
          <CardHeader
            title="Firewall"
            desc={`detected: ${fw.data?.kind || '…'}`}
            right={
              <Button variant="secondary" size="sm" onClick={() => fwAllow.mutate()}
                disabled={fwAllow.isPending || !fwPort || !/^\d+$/.test(fwPort) || +fwPort < 1 || +fwPort > 65535}>
                <Wind className="h-3.5 w-3.5" /> Open TCP port
              </Button>
            }
          />
          <div className="p-4">
            <Field label="Port to open (TCP)" hint="ufw / firewalld / iptables — whichever is active">
              <Input type="number" value={fwPort} onChange={(e) => setFwPort(e.target.value)} placeholder="443" />
            </Field>
            {fw.data?.kind === 'none' && (
              <p className="mt-2 text-2xs text-muted-foreground">No active firewall detected — nothing to open.</p>
            )}
          </div>
        </Card>
      </div>

      {/* relay modal */}
      <Modal open={relayModal !== null} onClose={() => setRelayModal(null)} wide
        title={relayModal === 'create' ? 'New trojan relay' : `Edit relay — ${relayDraft.name}`}>
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Relay name" hint="letters, digits, dashes">
              <Input value={relayDraft.name || ''} onChange={(e) => rSet('name', e.target.value)} placeholder="de-trojan-01" />
            </Field>
            <Field label="Public domain" hint="must point at THIS server">
              <Input value={relayDraft.domain || ''} onChange={(e) => rSet('domain', e.target.value)} placeholder="tj.example.com" />
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label="TLS port users connect to" hint="nginx stream listener">
              <Input type="number" value={relayDraft.https_port ?? ''} onChange={(e) => rSet('https_port', parseInt(e.target.value, 10))} />
            </Field>
            <Field label="Bridge port" hint="0 = auto-pick (22000+)">
              <Input type="number" value={relayDraft.bridge_port ?? ''} onChange={(e) => rSet('bridge_port', parseInt(e.target.value, 10) || 0)} />
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Foreign node IP" hint="public = through the tunnel, 127.0.0.1/private = direct">
              <Input value={relayDraft.foreign_ip || ''} onChange={(e) => rSet('foreign_ip', e.target.value)} placeholder="5.6.7.8" />
            </Field>
            <Field label="Foreign ingress port" hint="the tunnel port on the node">
              <Input type="number" value={relayDraft.foreign_port ?? ''} onChange={(e) => rSet('foreign_port', parseInt(e.target.value, 10))} />
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Route" hint="empty = auto (private/loopback → direct)">
              <Select value={relayDraft.route ?? ''} onChange={(e) => rSet('route', e.target.value as any)}>
                <option value="">auto</option>
                <option value="tunnel">tunnel — via the SOCKS hub</option>
                <option value="direct">direct — dial it from this box</option>
              </Select>
            </Field>
            <Field label="SOCKS hub port" hint="0 = the global Settings port (40001)">
              <Input type="number" value={relayDraft.socks_port ?? ''} onChange={(e) => rSet('socks_port', parseInt(e.target.value, 10) || 0)} />
            </Field>
          </div>
          <div className="flex justify-end gap-2 border-t border-border pt-4">
            <Button variant="secondary" onClick={() => setRelayModal(null)}>Cancel</Button>
            <Button onClick={saveRelay} disabled={savingRelay || !relayDraft.name || !relayDraft.domain || !relayDraft.foreign_ip}>
              {savingRelay ? 'Saving…' : 'Save relay'}
            </Button>
          </div>
        </div>
      </Modal>

      {/* ingress modal */}
      <Modal open={ingModal !== null} onClose={() => setIngModal(null)}
        title="New ingress forwarder (foreign node)">
        <div className="space-y-4">
          <Field label="Forwarder name">
            <Input value={ingDraft.name || ''} onChange={(e) => setIngDraft({ ...ingDraft, name: e.target.value })} placeholder="ing01" />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Public listen port" hint="what relays dial from Iran">
              <Input type="number" value={ingDraft.listen_port ?? ''} onChange={(e) => setIngDraft({ ...ingDraft, listen_port: parseInt(e.target.value, 10) })} />
            </Field>
            <Field label="Listen IP" hint="0.0.0.0 = all IPv4">
              <Input value={ingDraft.listen_ip || '0.0.0.0'} onChange={(e) => setIngDraft({ ...ingDraft, listen_ip: e.target.value })} />
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Node address" hint="127.0.0.1 if the node is on this box">
              <Input value={ingDraft.target_host || ''} onChange={(e) => setIngDraft({ ...ingDraft, target_host: e.target.value })} placeholder="127.0.0.1" />
            </Field>
            <Field label="Node port" hint="the node inbound port">
              <Input type="number" value={ingDraft.target_port ?? ''} onChange={(e) => setIngDraft({ ...ingDraft, target_port: parseInt(e.target.value, 10) })} />
            </Field>
          </div>
          <label className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
            <Toggle checked={!!ingDraft.udp} onChange={(v) => setIngDraft({ ...ingDraft, udp: v })} />
            Also forward UDP (QUIC / Hysteria style inbounds)
          </label>
          <div className="flex justify-end gap-2 border-t border-border pt-4">
            <Button variant="secondary" onClick={() => setIngModal(null)}>Cancel</Button>
            <Button onClick={saveIngress} disabled={!ingDraft.name || !ingDraft.listen_port}>
              Save forwarder
            </Button>
          </div>
        </div>
      </Modal>

      {/* relay end-to-end verify modal (hedioum-suite relay_verify) */}
      <Modal open={verifyRep !== null} onClose={() => setVerifyRep(null)}
        title={`Verify relay — ${verifyRep?.name ?? ''}`}>
        {verifyRep && (
          <div className="space-y-2">
            <EnvRowInline label="Local listeners (https + bridge)" ok={verifyRep.listen_up} detail="listening" missing="down — apply the bridge" />
            <EnvRowInline label="SOCKS5 hub" ok={verifyRep.hub_alive} detail="reachable" missing="not answering" />
            <EnvRowInline label={`Foreign target (${verifyRep.target_probe})`} ok={verifyRep.target_ok} detail="answers" missing="no answer" />
            {verifyRep.note && (
              <p className="rounded-lg bg-sky-50 p-2.5 text-2xs leading-relaxed text-sky-800 dark:bg-sky-500/10 dark:text-sky-300">{verifyRep.note}</p>
            )}
            <div className="flex justify-end pt-2">
              <Button variant="secondary" size="sm" onClick={() => setVerifyRep(null)}>Close</Button>
            </div>
          </div>
        )}
      </Modal>

      {/* hedioum-suite v3: node hygiene + tools + purge */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader
            title="Node check (foreign side)"
            desc="PasarGuard/marzban service, xray listeners, mimic clashes and fail2ban"
            right={
              <Button variant="secondary" size="sm" onClick={() => hygiene.refetch()} disabled={hygiene.isFetching}>
                <RefreshCw className={`h-3.5 w-3.5 ${hygiene.isFetching ? 'animate-spin' : ''}`} /> Refresh
              </Button>
            }
          />
          <div className="space-y-2 p-4">
            {hygiene.isLoading ? (
              <div className="flex justify-center py-6"><Spinner /></div>
            ) : hygiene.data ? (
              <>
                <EnvRowInline label="Node service" ok={!!hygiene.data.node.kind} detail={hygiene.data.node.kind || 'none on this box'} missing="none on this box" />
                {hygiene.data.node.kind && (
                  <EnvRowInline
                    label="Node inbound path"
                    ok={!hygiene.data.node.loopback_only}
                    detail={hygiene.data.node.public_bind || 'public bind found'}
                    missing="loopback-only — the egress cannot dial it (add a forwarder or rebind)"
                  />
                )}
                {!!hygiene.data.node.xray_ports?.length && (
                  <div className="rounded-lg border border-border px-3 py-2 text-2xs text-muted-foreground">
                    xray ports: <span className="font-mono">{hygiene.data.node.xray_ports.join(', ')}</span>
                    {!!hygiene.data.node.rpc_ports?.length && <> · panel RPC: <span className="font-mono">{hygiene.data.node.rpc_ports.join(', ')}</span></>}
                  </div>
                )}
                {(hygiene.data.clashes.length > 0 || hygiene.data.overlap.length > 0) && (
                  <div className="rounded-lg border border-amber-200 bg-amber-50 p-2.5 text-2xs text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300">
                    {hygiene.data.overlap.length > 0 && <div><b>Node ports on mimic ports:</b> {hygiene.data.overlap.join(', ')} — move them, a forwarder cannot fix this</div>}
                    {hygiene.data.clashes.map((c) => <div key={c.port}>mimic port {c.port} held by {c.owner}</div>)}
                  </div>
                )}
                {hygiene.data.fail2ban && (
                  <div className="rounded-lg border border-amber-200 bg-amber-50 p-2.5 text-2xs text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300">
                    fail2ban is active — whitelist the Hedioum egress address
                  </div>
                )}
                {(hygiene.data.advice ?? []).map((a, i) => (
                  <p key={i} className="text-2xs leading-relaxed text-muted-foreground">• {a}</p>
                ))}
              </>
            ) : null}
          </div>
        </Card>

        <Card>
          <CardHeader
            title="Hedioum tools & suite purge"
            desc="non-interactive runs of the built-in diagnostics; purge removes every relay/forwarder but keeps hedioum, nginx and the node"
            right={
              <div className="flex gap-2">
                <Button variant="secondary" size="sm" disabled={toolBusy !== null}
                  onClick={() => runTool('check-ip', () => api.hedioumCheckIP())}>
                  <Terminal className="h-3.5 w-3.5" /> {toolBusy === 'check-ip' ? '…' : 'Check egress IP'}
                </Button>
                <Button variant="secondary" size="sm" disabled={toolBusy !== null}
                  onClick={() => {
                    const alias = window.prompt('Node alias to probe (e.g. DE-01):', 'DE-01')
                    if (alias && /^[A-Za-z0-9_-]{1,64}$/.test(alias)) runTool(`probe ${alias}`, () => api.hedioumProbe(alias))
                  }}>
                  <Stethoscope className="h-3.5 w-3.5" /> Probe
                </Button>
                <Button variant="secondary" size="sm" disabled={toolBusy !== null}
                  onClick={() => {
                    const alias = window.prompt('Node alias to speedtest:', 'DE-01')
                    if (alias && /^[A-Za-z0-9_-]{1,64}$/.test(alias)) runTool(`speedtest ${alias}`, () => api.hedioumSpeedtest(alias, 'tls', 'down'))
                  }}>
                  <Gauge className="h-3.5 w-3.5" /> Speedtest
                </Button>
              </div>
            }
          />
          <div className="space-y-3 p-4">
            {!purgeConfirm ? (
              <Button variant="ghost" size="sm" className="text-red-500" onClick={() => setPurgeConfirm(true)}>
                <Bomb className="h-3.5 w-3.5" /> Purge the trojan suite…
              </Button>
            ) : (
              <div className="space-y-2 rounded-xl border border-red-300 bg-red-50 p-3 text-2xs leading-relaxed text-red-800 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300">
                <p>Deletes <b>all trojan relays and ingress forwarders</b> from this panel and removes the bridge/ingress units and configs from this server.</p>
                <p><b>Kept:</b> the Hedioum tunnel itself, nginx, and your node service.</p>
                <div className="flex gap-2 pt-1">
                  <Button variant="secondary" size="sm" onClick={() => setPurgeConfirm(false)} disabled={purging}>Cancel</Button>
                  <Button size="sm" className="bg-red-600 hover:bg-red-700" onClick={runPurge} disabled={purging}>
                    {purging ? 'Purging…' : 'Yes, purge everything'}
                  </Button>
                </div>
              </div>
            )}
          </div>
        </Card>
      </div>

      {/* hedioum tool output modal */}
      <Modal open={toolModal !== null} onClose={() => setToolModal(null)} wide
        title={toolModal ? `hedioum-tunnel ${toolModal.title}` : ''}>
        {toolModal && (
          <div className="space-y-3">
            <Badge color={toolModal.ok ? 'green' : 'red'}>{toolModal.ok ? 'ok' : 'error'}</Badge>
            <pre className="max-h-96 overflow-auto rounded-xl border border-border bg-mutedslate p-3 font-mono text-2xs leading-relaxed whitespace-pre-wrap">{toolModal.output}</pre>
            <div className="flex justify-end">
              <Button variant="secondary" size="sm" onClick={() => setToolModal(null)}>Close</Button>
            </div>
          </div>
        )}
      </Modal>
    </>
  )
}
