import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Gauge, Plus, RefreshCw, Trash2, Zap, Users, Link2, Search, ShieldCheck, XCircle, Pencil,
} from 'lucide-react'
import {
  api, type RateProfile, type PasarguardUserView, type RateLimitStatus, type ServerNode,
} from '../api'
import { Badge, Button, Card, CardHeader, Empty, Field, Input, Modal, Select, Spinner, Toggle } from '../components/ui'
import { useToast } from '../components/toast'
import { fmtBps, parseBpsInput } from '../lib/format'
import { useDebouncedValue } from '../lib/hooks'

// unlimited renders as ∞ here; limits key on the Xray UUID
const fmt = (bps: number): string => (bps <= 0 ? '∞' : fmtBps(bps))

export default function Bandwidth() {
  const qc = useQueryClient()
  const { push } = useToast()

  const status = useQuery({ queryKey: ['rate-limit-status'], queryFn: () => api.rateLimitStatus() })
  const profiles = useQuery({ queryKey: ['rate-profiles'], queryFn: () => api.listRateProfiles() })
  const nodes = useQuery({ queryKey: ['nodes'], queryFn: () => api.listNodes() })

  // thousands of users: the table is served server-side - one page of rows
  // per fetch, with search + state filter applied in SQL, not in the browser
  const [search, setSearch] = useState('')
  const [debounced, setDebounced] = useState('')
  const [stateFilter, setStateFilter] = useState<'' | 'active' | 'expired' | 'disabled'>('')
  const [page, setPage] = useState(0)
  const [pageSize, setPageSize] = useState(25)
  useEffect(() => {
    const t = setTimeout(() => {
      setDebounced(search.trim())
      setPage(0)
    }, 300)
    return () => clearTimeout(t)
  }, [search])
  const users = useQuery({
    queryKey: ['pg-users', page, pageSize, debounced, status],
    queryFn: () =>
      api.listPasarguardUsersPaged({ limit: pageSize, offset: page * pageSize, search: debounced, status: stateFilter }),
    placeholderData: (prev) => prev,
  })
  const [profileModal, setProfileModal] = useState<null | 'create' | 'edit'>()
  const [profileDraft, setProfileDraft] = useState<Partial<RateProfile> & { dl?: string; ul?: string }>({})
  const [userModal, setUserModal] = useState<PasarguardUserView | null>(null)
  const [userDraft, setUserDraft] = useState<{ nodeId: number; profileId: string; custom: boolean; dl: string; ul: string }>({
    nodeId: 0, profileId: '', custom: false, dl: '', ul: '',
  })
  const [saving, setSaving] = useState(false)

  const refresh = () => {
    qc.invalidateQueries({ queryKey: ['rate-limit-status'] })
    qc.invalidateQueries({ queryKey: ['rate-profiles'] })
    qc.invalidateQueries({ queryKey: ['pg-users'] })
  }

  const sync = useMutation({
    mutationFn: () => api.syncPasarGuard(),
    onSuccess: (res) => {
      push('success', `Synced ${res.synced} users from PasarGuard.`)
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const pushLimits = useMutation({
    mutationFn: () => api.pushRateLimits(),
    onSuccess: (res) => {
      const failed = res.results.filter((r) => !r.ok)
      if (failed.length === 0) {
        push('success', `Plans pushed to ${res.results.length} node(s).`)
      } else {
        push('error', `Push failed on ${failed.length} node(s): ${failed[0].error}`)
      }
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const delProfile = useMutation({
    mutationFn: (id: number) => api.deleteRateProfile(id),
    onSuccess: () => {
      push('success', 'Profile deleted. Policies using it keep their values.')
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const saveProfile = async () => {
    const dl = parseBpsInput(profileDraft.dl || '0')
    const ul = parseBpsInput(profileDraft.ul || '0')
    if (dl < 0 || ul < 0) {
      push('error', 'Invalid bandwidth — use e.g. "5Mbps", "512Kbps" or "unlimited".')
      return
    }
    setSaving(true)
    try {
      const payload = {
        name: profileDraft.name,
        download_bps: dl,
        upload_bps: ul,
        enabled: profileDraft.enabled ?? true,
        notes: profileDraft.notes || '',
      }
      if (profileModal === 'create') {
        await api.createRateProfile(payload)
        push('success', 'Profile created.')
      } else if (profileModal === 'edit' && profileDraft.id) {
        await api.updateRateProfile(profileDraft.id, payload)
        push('success', 'Profile updated.')
      }
      setProfileModal(null)
      refresh()
    } catch (e: any) {
      push('error', e.message)
    } finally {
      setSaving(false)
    }
  }

  const openUserModal = (u: PasarguardUserView, nodes: ServerNode[] | undefined) => {
    setUserModal(u)
    setUserDraft({
      nodeId: nodes?.[0]?.id ?? 0,
      profileId: u.profile_name || String(u.custom ? '' : ''),
      custom: u.custom,
      dl: u.policy_download_bps > 0 ? fmt(u.policy_download_bps).replace(' ', '') : '',
      ul: u.policy_upload_bps > 0 ? fmt(u.policy_upload_bps).replace(' ', '') : '',
    })
  }

  const saveUserPolicy = async () => {
    if (!userModal) return
    const dl = userDraft.custom ? parseBpsInput(userDraft.dl || '0') : undefined
    const ul = userDraft.custom ? parseBpsInput(userDraft.ul || '0') : undefined
    if (dl !== undefined && dl < 0) {
      push('error', 'Invalid download value.')
      return
    }
    setSaving(true)
    try {
      const profile = profiles.data?.find((p) => p.name === userDraft.profileId)
      await api.upsertRatePolicy({
        uuid: userModal.uuid,
        node_id: userDraft.nodeId,
        profile_id: userDraft.custom || !profile ? null : profile.id,
        download_bps: userDraft.custom ? (dl ?? 0) : undefined,
        upload_bps: userDraft.custom ? (ul ?? 0) : undefined,
        custom: userDraft.custom,
        enabled: true,
      })
      push('success', `Policy saved for ${userModal.username}. It will be pushed on the next sync.`)
      setUserModal(null)
      refresh()
    } catch (e: any) {
      push('error', e.message)
    } finally {
      setSaving(false)
    }
  }

  const removeUserPolicy = useMutation({
    mutationFn: (u: PasarguardUserView) => {
      const nodeId = nodes.data?.[0]?.id ?? 0
      return api.deleteRatePolicy(u.uuid, nodeId)
    },
    onSuccess: () => {
      push('success', 'Limit removed — the user goes unlimited on the next push.')
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const st: RateLimitStatus | undefined = status.data
  const rows = users.data?.users ?? []
  const total = users.data?.total ?? 0
  const pageCount = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold">Bandwidth / Rate Limits</h1>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Per-UUID bandwidth limits for PasarGuard Xray users, enforced on nodes with Linux tc.
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => sync.mutate()} disabled={sync.isPending}>
            <RefreshCw className={`h-4 w-4 ${sync.isPending ? 'animate-spin' : ''}`} /> Sync users
          </Button>
          <Button variant="secondary" onClick={() => pushLimits.mutate()} disabled={pushLimits.isPending}>
            <Zap className="h-4 w-4" /> Push plans
          </Button>
        </div>
      </div>

      {!st?.enabled && st && (
        <div className="rounded-xl border border-amber-200 bg-amber-50 p-3.5 text-xs text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300">
          Rate limiting is <b>disabled</b> — enable it in Settings → Bandwidth, configure the PasarGuard
          URL + token, then Sync users. Existing limits on nodes keep working until changed.
        </div>
      )}

      {/* summary cards */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
        <StatCard icon={<Users className="h-4 w-4" />} label="Users" value={String(st?.total_users ?? '—')} />
        <StatCard icon={<Gauge className="h-4 w-4" />} label="Limited" value={String(st?.limited_users ?? '—')} accent="indigo" />
        <StatCard icon={<Link2 className="h-4 w-4" />} label="Active nodes" value={String(st?.active_nodes ?? '—')} />
        <StatCard icon={<XCircle className="h-4 w-4" />} label="Failed policies" value={String(st?.failed ?? '—')} accent={st?.failed ? 'red' : undefined} />
        <StatCard
          icon={<ShieldCheck className="h-4 w-4" />}
          label="Last sync"
          value={st?.last_sync ? new Date(st.last_sync).toLocaleTimeString() : '—'}
        />
      </div>

      {/* profiles */}
      <Card>
        <CardHeader
          title="Profiles"
          desc="Reusable bandwidth presets assigned to users"
          right={
            <Button
              size="sm"
              onClick={() => {
                setProfileDraft({ name: '', dl: '', ul: '', enabled: true, notes: '' })
                setProfileModal('create')
              }}
            >
              <Plus className="h-3.5 w-3.5" /> New profile
            </Button>
          }
        />
        {profiles.isLoading ? (
          <div className="flex justify-center py-10"><Spinner /></div>
        ) : !profiles.data?.length ? (
          <Empty message="No profiles yet — create Basic / Standard / VIP presets to assign them with one click." />
        ) : (
          <div className="grid grid-cols-1 gap-3 p-5 sm:grid-cols-2 lg:grid-cols-4">
            {profiles.data.map((p) => (
              <div key={p.id} className="rounded-xl border border-slate-200 p-3.5 dark:border-slate-700">
                <div className="flex items-center justify-between">
                  <span className="text-sm font-semibold">{p.name}</span>
                  <div className="flex gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setProfileDraft({ ...p, dl: fmt(p.download_bps).replace(' ', ''), ul: fmt(p.upload_bps).replace(' ', '') })
                        setProfileModal('edit')
                      }}
                    >
                      <Pencil className="h-3.5 w-3.5" />
                    </Button>
                    <Button variant="ghost" size="sm" className="text-red-500" onClick={() => delProfile.mutate(p.id)}>
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
                <div className="mt-2 space-y-1 font-mono text-2xs">
                  <div className="flex justify-between"><span className="text-slate-400">down</span><span>{fmt(p.download_bps)}</span></div>
                  <div className="flex justify-between"><span className="text-slate-400">up</span><span>{fmt(p.upload_bps)}</span></div>
                </div>
                {!p.enabled && <Badge color="red">disabled</Badge>}
              </div>
            ))}
          </div>
        )}
      </Card>

      {/* users */}
      <Card>
        <CardHeader
          title="PasarGuard users"
          desc="limits key on the Xray UUID; enforcement maps the user's public source IP"
          right={
            <div className="flex flex-wrap items-center gap-2">
              <Input
                className="w-full sm:w-52"
                placeholder="search name or uuid"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
              <Select
                className="w-1/2 sm:w-36"
                value={stateFilter}
                onChange={(e) => { setStateFilter(e.target.value as typeof stateFilter); setPage(0) }}
              >
                <option value="">All states</option>
                <option value="active">Active</option>
                <option value="expired">Expired</option>
                <option value="disabled">Disabled</option>
              </Select>
              <Select
                className="w-1/3 sm:w-28"
                value={String(pageSize)}
                onChange={(e) => { setPageSize(parseInt(e.target.value, 10) || 25); setPage(0) }}
              >
                <option value="25">25 / page</option>
                <option value="50">50 / page</option>
                <option value="100">100 / page</option>
                <option value="250">250 / page</option>
              </Select>
            </div>
          }
        />
        {users.isLoading ? (
          <div className="flex justify-center py-10"><Spinner /></div>
        ) : !rows.length ? (
          <Empty message={debounced || stateFilter ? 'No users match the current search/filter.' : 'No synced users yet. Configure the PasarGuard URL/token in Settings, then click Sync users.'} />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-slate-200 text-left text-2xs uppercase tracking-wide text-slate-400 dark:border-slate-800">
                  <th className="px-5 py-2.5 font-medium">User</th>
                  <th className="px-3 py-2.5 font-medium">UUID</th>
                  <th className="px-3 py-2.5 font-medium">State</th>
                  <th className="px-3 py-2.5 font-medium">Profile</th>
                  <th className="px-3 py-2.5 font-medium">Down / Up</th>
                  <th className="px-3 py-2.5 font-medium">Policy</th>
                  <th className="px-5 py-2.5 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-slate-800/70">
                {rows.map((u) => (
                  <tr key={u.uuid} className="hover:bg-slate-50 dark:hover:bg-slate-800/40">
                    <td className="px-5 py-2.5 font-medium">{u.username || '—'}</td>
                    <td className="px-3 py-2.5 font-mono text-2xs text-slate-400" title={u.uuid}>{u.uuid.slice(0, 13)}…</td>
                    <td className="px-3 py-2.5">
                      {u.expired ? <Badge color="red">expired</Badge> : !u.enabled ? <Badge color="amber">disabled</Badge> : <Badge color="green">active</Badge>}
                    </td>
                    <td className="px-3 py-2.5 text-xs">
                      {u.has_policy ? (u.custom ? <Badge color="blue">custom</Badge> : u.profile_name || '—') : <span className="text-slate-400">unlimited</span>}
                    </td>
                    <td className="px-3 py-2.5 font-mono text-xs">
                      {u.has_policy ? `${fmt(u.policy_download_bps)} / ${fmt(u.policy_upload_bps)}` : '—'}
                    </td>
                    <td className="px-3 py-2.5">
                      {u.has_policy ? (
                        <Badge color={u.policy_state === 'synced' ? 'green' : u.policy_state === 'failed' ? 'red' : 'amber'}>{u.policy_state}</Badge>
                      ) : '—'}
                    </td>
                    <td className="px-5 py-2.5">
                      <div className="flex justify-end gap-1">
                        <Button variant="secondary" size="sm" onClick={() => openUserModal(u, nodes.data)}>
                          {u.has_policy ? 'Edit' : 'Set limit'}
                        </Button>
                        {u.has_policy && (
                          <Button variant="ghost" size="sm" className="text-red-500" onClick={() => removeUserPolicy.mutate(u)}>
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {rows.length > 0 && (
          <div className="flex flex-wrap items-center justify-between gap-2 border-t border-slate-200 px-5 py-2.5 text-xs text-slate-500 dark:border-slate-800">
            <span>
              {total.toLocaleString()} users{debounced || stateFilter ? ' (filtered)' : ''} · page {page + 1} / {pageCount}
            </span>
            <div className="flex gap-1">
              <Button variant="ghost" size="sm" disabled={page === 0}
                onClick={() => setPage((p) => Math.max(0, p - 1))}>Prev</Button>
              <Button variant="ghost" size="sm" disabled={page + 1 >= pageCount}
                onClick={() => setPage((p) => p + 1)}>Next</Button>
            </div>
          </div>
        )}
      </Card>

      {/* profile modal */}
      <Modal open={profileModal != null} onClose={() => setProfileModal(null)}
        title={profileModal === 'create' ? 'New bandwidth profile' : `Edit — ${profileDraft.name}`}>
        <div className="space-y-4">
          <Field label="Name"><Input value={profileDraft.name || ''} onChange={(e) => setProfileDraft({ ...profileDraft, name: e.target.value })} placeholder="Premium" /></Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label="Download" hint="e.g. 30Mbps, 512Kbps, unlimited">
              <Input value={profileDraft.dl || ''} onChange={(e) => setProfileDraft({ ...profileDraft, dl: e.target.value })} placeholder="30Mbps" />
            </Field>
            <Field label="Upload" hint="e.g. 10Mbps, unlimited">
              <Input value={profileDraft.ul || ''} onChange={(e) => setProfileDraft({ ...profileDraft, ul: e.target.value })} placeholder="10Mbps" />
            </Field>
          </div>
          <label className="flex items-center gap-2 text-xs font-medium text-slate-600 dark:text-slate-300">
            <Toggle checked={profileDraft.enabled ?? true} onChange={(v) => setProfileDraft({ ...profileDraft, enabled: v })} /> Enabled
          </label>
          <div className="flex justify-end gap-2 border-t border-slate-200 pt-4 dark:border-slate-700">
            <Button variant="secondary" onClick={() => setProfileModal(null)}>Cancel</Button>
            <Button onClick={saveProfile} disabled={saving || !profileDraft.name}>{saving ? 'Saving…' : 'Save profile'}</Button>
          </div>
        </div>
      </Modal>

      {/* user policy modal */}
      <Modal open={userModal !== null} onClose={() => setUserModal(null)}
        title={userModal ? `Bandwidth — ${userModal.username}` : ''}>
        {userModal && (
          <div className="space-y-4">
            <div className="rounded-lg bg-slate-50 p-3 font-mono text-2xs text-slate-500 dark:bg-slate-800/60 dark:text-slate-400">
              {userModal.uuid}
            </div>
            <Field label="Node" hint="which node enforces the limit">
              <Select value={userDraft.nodeId || 0} onChange={(e) => setUserDraft({ ...userDraft, nodeId: parseInt(e.target.value, 10) })}>
                {(nodes.data ?? []).filter((n) => n.enabled).map((n) => (
                  <option key={n.id} value={n.id}>{n.name} ({n.status})</option>
                ))}
              </Select>
            </Field>
            <Field label="Profile">
              <Select
                value={userDraft.profileId}
                onChange={(e) => setUserDraft({ ...userDraft, profileId: e.target.value, custom: e.target.value === '__custom__' })}
              >
                <option value="">Unlimited (no rule)</option>
                {(profiles.data ?? []).filter((p) => p.enabled).map((p) => (
                  <option key={p.id} value={p.name}>{p.name} — {fmt(p.download_bps)} / {fmt(p.upload_bps)}</option>
                ))}
                <option value="__custom__">Custom…</option>
              </Select>
            </Field>
            {userDraft.custom && (
              <div className="grid grid-cols-2 gap-3">
                <Field label="Download"><Input value={userDraft.dl} onChange={(e) => setUserDraft({ ...userDraft, dl: e.target.value })} placeholder="17Mbps" /></Field>
                <Field label="Upload"><Input value={userDraft.ul} onChange={(e) => setUserDraft({ ...userDraft, ul: e.target.value })} placeholder="3Mbps" /></Field>
              </div>
            )}
            <div className="flex justify-end gap-2 border-t border-slate-200 pt-4 dark:border-slate-700">
              <Button variant="secondary" onClick={() => setUserModal(null)}>Cancel</Button>
              <Button onClick={saveUserPolicy} disabled={saving || !userDraft.nodeId}>{saving ? 'Saving…' : 'Save policy'}</Button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  )
}

function StatCard({ icon, label, value, accent }: { icon: React.ReactNode; label: string; value: string; accent?: 'indigo' | 'red' }) {
  const accentCls = accent === 'indigo' ? 'text-indigo-600 dark:text-indigo-300'
    : accent === 'red' ? 'text-red-500' : ''
  return (
    <div className="min-w-0 rounded-xl border border-slate-200 p-3.5 dark:border-slate-700">
      <div className="flex items-center gap-1.5 text-2xs font-medium uppercase tracking-wide text-slate-400">
        {icon} <span className="truncate">{label}</span>
      </div>
      <div className={`mt-1 truncate text-lg font-bold ${accentCls}`} title={value}>{value}</div>
    </div>
  )
}
