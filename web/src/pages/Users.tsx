import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2, UserPlus, Shield } from 'lucide-react'
import { api, type AdminUser } from '../api'
import { Badge, Button, Card, CardHeader, Empty, Field, Input, Modal, Select, Spinner } from '../components/ui'
import { useToast } from '../components/toast'

const ROLES: { value: AdminUser['role']; label: string; desc: string }[] = [
  { value: 'viewer', label: 'Viewer', desc: 'Read-only: dashboards, ports, connections, configs, health' },
  { value: 'operator', label: 'Operator', desc: 'Viewer + deploy, scan, diagnostics, sync, restart services' },
  { value: 'admin', label: 'Admin', desc: 'Operator + manage mappings, certs, nodes, settings, backups' },
  { value: 'owner', label: 'Owner', desc: 'Everything, including users and roles' },
]

export default function Users() {
  const qc = useQueryClient()
  const { push } = useToast()

  const users = useQuery({ queryKey: ['users'], queryFn: () => api.listUsers() })
  const [modal, setModal] = useState(false)
  const [draft, setDraft] = useState({ username: '', password: '', role: 'operator' as AdminUser['role'] })
  const [saving, setSaving] = useState(false)

  const refresh = () => qc.invalidateQueries({ queryKey: ['users'] })

  const create = async () => {
    setSaving(true)
    try {
      await api.createUser(draft)
      push('success', `User ${draft.username} created.`)
      setModal(false)
      refresh()
    } catch (e: any) {
      push('error', e.message)
    } finally {
      setSaving(false)
    }
  }

  const setRole = useMutation({
    mutationFn: ({ id, role }: { id: number; role: AdminUser['role'] }) => api.updateUser(id, { role }),
    onSuccess: () => {
      push('success', 'Role updated.')
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const del = useMutation({
    mutationFn: (id: number) => api.deleteUser(id),
    onSuccess: () => {
      push('success', 'User removed.')
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold">Users & Roles</h1>
          <p className="text-xs text-muted-foreground">
            Owner-only. Viewers read; Operators deploy; Admins change configuration; Owners manage users.
          </p>
        </div>
        <Button onClick={() => { setDraft({ username: '', password: '', role: 'operator' }); setModal(true) }}>
          <UserPlus className="h-4 w-4" /> New user
        </Button>
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {ROLES.map((r) => (
          <div key={r.value} className="rounded-xl border border-border p-3">
            <div className="flex items-center gap-2">
              <Shield className={`h-4 w-4 ${r.value === 'owner' ? 'text-primary' : 'text-muted-foreground'}`} />
              <span className="text-xs font-bold">{r.label}</span>
            </div>
            <p className="mt-1 text-2xs leading-relaxed text-muted-foreground">{r.desc}</p>
          </div>
        ))}
      </div>

      <Card>
        <CardHeader title="Users" desc={`${users.data?.length ?? 0} accounts`} />
        {users.isLoading ? (
          <div className="flex justify-center py-12"><Spinner /></div>
        ) : !users.data?.length ? (
          <Empty message="No users found." />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-2xs uppercase tracking-wide text-muted-foreground">
                  <th className="px-5 py-2.5 font-medium">Username</th>
                  <th className="px-3 py-2.5 font-medium">Role</th>
                  <th className="px-3 py-2.5 font-medium">Created</th>
                  <th className="px-3 py-2.5 font-medium">Last login</th>
                  <th className="px-5 py-2.5 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {users.data.map((u) => (
                  <tr key={u.id} className="hover:bg-muted/60">
                    <td className="px-5 py-2.5 font-medium">{u.username}</td>
                    <td className="px-3 py-2.5">
                      <Select
                        className="h-8 w-32"
                        value={u.role}
                        onChange={(e) => setRole.mutate({ id: u.id, role: e.target.value as AdminUser['role'] })}
                      >
                        {ROLES.map((r) => <option key={r.value} value={r.value}>{r.label}</option>)}
                      </Select>
                    </td>
                    <td className="px-3 py-2.5 text-xs text-muted-foreground">{new Date(u.created_at).toLocaleDateString()}</td>
                    <td className="px-3 py-2.5 text-xs text-muted-foreground">
                      {u.last_login_at ? new Date(u.last_login_at).toLocaleString() : '—'}
                    </td>
                    <td className="px-5 py-2.5">
                      <div className="flex justify-end">
                        <Button variant="ghost" size="sm" className="text-red-500" onClick={() => del.mutate(u.id)}>
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
      </Card>

      <Modal open={modal} onClose={() => setModal(false)} title="New user">
        <div className="space-y-4">
          <Field label="Username"><Input value={draft.username} onChange={(e) => setDraft({ ...draft, username: e.target.value })} /></Field>
          <Field label="Password" hint="8+ characters">
            <Input type="password" value={draft.password} onChange={(e) => setDraft({ ...draft, password: e.target.value })} />
          </Field>
          <Field label="Role">
            <Select value={draft.role} onChange={(e) => setDraft({ ...draft, role: e.target.value as AdminUser['role'] })}>
              {ROLES.map((r) => <option key={r.value} value={r.value}>{r.label} — {r.desc}</option>)}
            </Select>
          </Field>
          <div className="flex justify-end gap-2 border-t border-border pt-4">
            <Button variant="secondary" onClick={() => setModal(false)}>Cancel</Button>
            <Button onClick={create} disabled={saving || !draft.username || draft.password.length < 8}>
              {saving ? 'Creating…' : 'Create user'}
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  )
}
