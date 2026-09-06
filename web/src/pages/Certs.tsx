import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2, ShieldCheck, BadgeCheck, Globe, RefreshCw } from 'lucide-react'
import { api, type Cert, type CertValidation, type ACMEProvider } from '../api'
import { Badge, Button, Card, CardHeader, Empty, Field, Input, Modal, Select, Spinner } from '../components/ui'
import { useToast } from '../components/toast'

export default function Certs() {
  const qc = useQueryClient()
  const { push } = useToast()
  const certs = useQuery({ queryKey: ['certs'], queryFn: () => api.listCerts() })
  const acmeTools = useQuery({ queryKey: ['tools'], queryFn: () => api.tools() })

  const [uploadOpen, setUploadOpen] = useState(false)
  const [selfOpen, setSelfOpen] = useState(false)
  const [issueOpen, setIssueOpen] = useState(false)
  const [validation, setValidation] = useState<{ name: string; data: CertValidation } | null>(null)
  const [checking, setChecking] = useState<number | null>(null)

  const [up, setUp] = useState({ name: '', cert_pem: '', key_pem: '' })
  const [ss, setSS] = useState({ name: '', domains: '', days: 825 })

  // ACME issue form state
  const providers = useQuery({ queryKey: ['acme-providers'], queryFn: () => api.acmeProviders(), enabled: issueOpen })
  const [issueForm, setIssueForm] = useState({
    tool: 'acmesh',
    method: 'dns01' as 'http01' | 'dns01',
    domains: '',
    email: '',
    dns_provider: 'cloudflare',
    name: '',
  })
  const [dnsEnv, setDnsEnv] = useState<Record<string, string>>({})
  const activeProvider: ACMEProvider | undefined = providers.data?.find((p) => p.id === issueForm.dns_provider)

  const refresh = () => qc.invalidateQueries({ queryKey: ['certs'] })

  const validateCert = async (c: Cert) => {
    setChecking(c.id)
    try {
      setValidation({ name: c.name, data: await api.certValidate(c.id) })
    } catch (e: any) {
      push('error', e.message)
    } finally {
      setChecking(null)
    }
  }

  const upload = useMutation({
    mutationFn: () => api.createCert(up),
    onSuccess: () => {
      push('success', 'Certificate uploaded')
      setUploadOpen(false)
      setUp({ name: '', cert_pem: '', key_pem: '' })
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const selfSigned = useMutation({
    mutationFn: () =>
      api.selfSignedCert({ name: ss.name, domains: ss.domains.split(',').map((s) => s.trim()).filter(Boolean), days: ss.days }),
    onSuccess: () => {
      push('success', 'Self-signed certificate generated')
      setSelfOpen(false)
      setSS({ name: '', domains: '', days: 825 })
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const del = useMutation({
    mutationFn: (id: number) => api.deleteCert(id),
    onSuccess: () => {
      push('success', 'Certificate deleted')
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const acmeInstalled = acmeTools.data?.tools?.some((t) => (t.id === 'acmesh' || t.id === 'certbot') && t.installed)

  const issue = useMutation({
    mutationFn: () =>
      api.issueCert({
        tool: issueForm.tool,
        method: issueForm.method,
        domains: issueForm.domains.split(',').map((s) => s.trim()).filter(Boolean),
        email: issueForm.email || undefined,
        dns_provider: issueForm.method === 'dns01' ? issueForm.dns_provider : undefined,
        dns_env: issueForm.method === 'dns01' ? dnsEnv : undefined,
        name: issueForm.name || undefined,
      }),
    onSuccess: (res) => {
      push('success', `Certificate issued and imported: ${res.cert.name}`)
      setIssueOpen(false)
      setDnsEnv({})
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const renew = useMutation({
    mutationFn: (id: number) => api.renewCert(id),
    onSuccess: () => {
      push('success', 'Certificate renewed')
      refresh()
    },
    onError: (e: any) => push('error', e.message),
  })

  const typeBadge = (t: string) =>
    t === 'acme' ? 'green' : t === 'manual' ? 'blue' : 'purple'

  const expiryBadge = (c: Cert) => {
    if (!c.expires_at) return <Badge color="slate">no expiry</Badge>
    const days = Math.floor((new Date(c.expires_at).getTime() - Date.now()) / 86400000)
    if (days < 0) return <Badge color="red">expired</Badge>
    if (days < 30) return <Badge color="amber">{days}d left</Badge>
    return <Badge color="green">{days}d left</Badge>
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold">SSL Certificates</h1>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Used by https mappings — nginx reads .crt/.key, HAProxy reads a combined .pem (generated automatically)
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => setUploadOpen(true)}>
            <Plus className="h-4 w-4" /> Upload PEM
          </Button>
          <Button variant="secondary" onClick={() => setSelfOpen(true)}>
            <ShieldCheck className="h-4 w-4" /> Self-signed
          </Button>
          <Button onClick={() => setIssueOpen(true)}>
            <Globe className="h-4 w-4" /> Issue ACME certificate
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader title="Certificates" desc={`${certs.data?.length ?? 0} stored`} />
        {certs.isLoading ? (
          <div className="flex justify-center py-16"><Spinner /></div>
        ) : !certs.data?.length ? (
          <Empty message="No certificates. Upload a PEM pair or generate a self-signed one." />
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-slate-200 text-left text-2xs uppercase tracking-wide text-slate-400 dark:border-slate-800">
                <th className="px-5 py-2.5 font-medium">Name</th>
                <th className="px-3 py-2.5 font-medium">Type</th>
                <th className="px-3 py-2.5 font-medium">Domains</th>
                <th className="px-3 py-2.5 font-medium">Expires</th>
                <th className="px-5 py-2.5 text-right font-medium">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 dark:divide-slate-800/70">
              {certs.data.map((c) => (
                <tr key={c.id} className="hover:bg-slate-50 dark:hover:bg-slate-800/40">
                  <td className="px-5 py-3 font-medium">{c.name}</td>
                  <td className="px-3 py-3"><Badge color={typeBadge(c.type)}>{c.type}</Badge></td>
                  <td className="px-3 py-3 text-xs text-slate-500">{c.domains.join(', ') || '—'}</td>
                  <td className="px-3 py-3">{expiryBadge(c)}</td>
                  <td className="px-5 py-3 text-right">
                    <div className="flex justify-end gap-1">
                      <Button variant="ghost" size="sm" title="Validate certificate & key match"
                        disabled={checking === c.id}
                        onClick={() => validateCert(c)}>
                        <BadgeCheck className={`h-3.5 w-3.5 ${checking === c.id ? 'animate-pulse' : ''}`} />
                      </Button>
                      {c.type === 'acme' && (
                        <Button variant="ghost" size="sm" title="Re-run ACME issuance (renew)"
                          disabled={renew.isPending}
                          onClick={() => renew.mutate(c.id)}>
                          <RefreshCw className={`h-3.5 w-3.5 ${renew.isPending ? 'animate-spin' : ''}`} />
                        </Button>
                      )}
                      <Button variant="ghost" size="sm" className="text-red-500 hover:bg-red-50 dark:hover:bg-red-900/20"
                        onClick={() => del.mutate(c.id)}><Trash2 className="h-3.5 w-3.5" /></Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>

      {/* Upload modal */}
      <Modal open={uploadOpen} onClose={() => setUploadOpen(false)} title="Upload certificate" wide>
        <div className="space-y-3">
          <Field label="Name">
            <Input value={up.name} onChange={(e) => setUp({ ...up, name: e.target.value })} placeholder="example.com" />
          </Field>
          <Field label="Certificate PEM (fullchain)">
            <textarea
              className="h-40 w-full rounded-lg border border-slate-300 bg-white p-3 font-mono text-xs focus:border-indigo-500 focus:outline-none dark:border-slate-600 dark:bg-slate-800"
              placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
              value={up.cert_pem}
              onChange={(e) => setUp({ ...up, cert_pem: e.target.value })}
            />
          </Field>
          <Field label="Private key PEM">
            <textarea
              className="h-32 w-full rounded-lg border border-slate-300 bg-white p-3 font-mono text-xs focus:border-indigo-500 focus:outline-none dark:border-slate-600 dark:bg-slate-800"
              placeholder="-----BEGIN PRIVATE KEY-----&#10;...&#10;-----END PRIVATE KEY-----"
              value={up.key_pem}
              onChange={(e) => setUp({ ...up, key_pem: e.target.value })}
            />
          </Field>
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="secondary" onClick={() => setUploadOpen(false)}>Cancel</Button>
            <Button onClick={() => upload.mutate()} disabled={upload.isPending || !up.cert_pem || !up.key_pem}>
              {upload.isPending ? 'Uploading…' : 'Upload'}
            </Button>
          </div>
        </div>
      </Modal>

      {/* ACME issue modal */}
      <Modal open={issueOpen} onClose={() => setIssueOpen(false)} title="Issue ACME certificate (Let's Encrypt)" wide>
        <div className="space-y-3">
          {providers.data && !acmeInstalled && (
            <p className="rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">
              Neither acme.sh nor certbot is installed yet — the client will be auto-installed on first issue
              (acme.sh is recommended, especially for wildcards). You can also install it from the Tools page.
            </p>
          )}
          <div className="grid grid-cols-2 gap-3">
            <Field label="ACME client">
              <Select value={issueForm.tool} onChange={(e) => setIssueForm({ ...issueForm, tool: e.target.value })}>
                <option value="acmesh">acme.sh (recommended)</option>
                <option value="certbot">certbot</option>
              </Select>
            </Field>
            <Field label="Challenge type" hint="Wildcard requires DNS-01">
              <Select value={issueForm.method} onChange={(e) => setIssueForm({ ...issueForm, method: e.target.value as 'http01' | 'dns01' })}>
                <option value="dns01">DNS-01 (works everywhere)</option>
                <option value="http01">HTTP-01 (standalone :80)</option>
              </Select>
            </Field>
          </div>
          <Field label="Domains" hint="Comma separated — wildcards allowed, e.g. example.com, *.example.com">
            <Input value={issueForm.domains} onChange={(e) => setIssueForm({ ...issueForm, domains: e.target.value })}
              placeholder="example.com, *.example.com" />
          </Field>
          {issueForm.method === 'dns01' && (
            <>
              <Field label="DNS provider">
                <Select value={issueForm.dns_provider}
                  onChange={(e) => { setIssueForm({ ...issueForm, dns_provider: e.target.value }); setDnsEnv({}) }}>
                  {(providers.data ?? []).map((p) => (
                    <option key={p.id} value={p.id} disabled={issueForm.tool === 'certbot' ? !p.certbot_pkg : !p.acmesh_dns}>
                      {p.name}{issueForm.tool === 'certbot' && !p.certbot_pkg ? ' (acme.sh only)' : ''}
                    </option>
                  ))}
                </Select>
              </Field>
              {activeProvider?.env?.length ? (
                <div className="space-y-2 rounded-lg border border-slate-200 p-3 dark:border-slate-700">
                  <p className="text-2xs font-medium uppercase tracking-wide text-slate-400">
                    {activeProvider.name} API credentials
                  </p>
                  {activeProvider.env.map((f) => (
                    <Field key={f.key} label={f.label + (f.required ? ' *' : '')}>
                      <Input
                        type={f.secret ? 'password' : 'text'}
                        value={dnsEnv[f.key] ?? ''}
                        onChange={(e) => setDnsEnv({ ...dnsEnv, [f.key]: e.target.value })}
                        placeholder={f.key}
                      />
                    </Field>
                  ))}
                </div>
              ) : null}
            </>
          )}
          {issueForm.method === 'http01' && (
            <p className="rounded-lg bg-slate-100 px-3 py-2 text-xs text-slate-600 dark:bg-slate-800 dark:text-slate-300">
              HTTP-01 runs a standalone server on port 80. Stop nginx/HAProxy first if they already own port 80,
              or use DNS-01 instead.
            </p>
          )}
          {issueForm.tool === 'certbot' ? (
            <Field label="Email (ACME account)" hint="Required by certbot for account registration">
              <Input type="email" value={issueForm.email} onChange={(e) => setIssueForm({ ...issueForm, email: e.target.value })}
                placeholder="you@example.com" />
            </Field>
          ) : (
            <Field label="Email (optional)" hint="ACME account contact — defaults to portguard@<primary domain>">
              <Input type="email" value={issueForm.email} onChange={(e) => setIssueForm({ ...issueForm, email: e.target.value })}
                placeholder="you@example.com" />
            </Field>
          )}
          <Field label="Certificate name (optional)">
            <Input value={issueForm.name} onChange={(e) => setIssueForm({ ...issueForm, name: e.target.value })}
              placeholder="defaults to the primary domain" />
          </Field>
          <div className="flex items-center justify-end gap-2 pt-2">
            {issue.isPending && (
              <span className="mr-auto flex items-center gap-2 text-xs text-slate-500">
                <Spinner className="h-3.5 w-3.5" /> Issuing… (DNS-01 can take 1–2 minutes)
              </span>
            )}
            <Button variant="secondary" onClick={() => setIssueOpen(false)}>Cancel</Button>
            <Button onClick={() => issue.mutate()}
              disabled={issue.isPending || !issueForm.domains || (issueForm.method === 'dns01' && issueForm.tool === 'certbot' && !issueForm.email)}>
              {issue.isPending ? 'Issuing…' : 'Issue certificate'}
            </Button>
          </div>
        </div>
      </Modal>

      {/* Validation modal */}
      <Modal open={validation !== null} onClose={() => setValidation(null)} title={validation ? `Validate: ${validation.name}` : ''}>
        {validation && (
          <div className="space-y-3 text-xs">
            {validation.data.error ? (
              <p className="rounded-lg bg-red-50 px-3 py-2 text-red-600 dark:bg-red-900/20 dark:text-red-300">
                {validation.data.error}
              </p>
            ) : (
              <>
                <div className="grid grid-cols-2 gap-x-4 gap-y-1">
                  <span className="text-slate-400">Subject</span>
                  <span className="font-mono">{validation.data.cert?.subject}</span>
                  <span className="text-slate-400">Issuer</span>
                  <span className="font-mono">{validation.data.cert?.issuer}</span>
                  <span className="text-slate-400">Valid</span>
                  <span>{validation.data.cert?.not_before} → {validation.data.cert?.not_after}</span>
                  <span className="text-slate-400">Days left</span>
                  <span>{validation.data.cert?.days_left}</span>
                  <span className="text-slate-400">Domains</span>
                  <span>{validation.data.cert?.dns_names?.join(', ') || '—'}</span>
                  <span className="text-slate-400">Key type</span>
                  <span>{validation.data.cert?.key_algorithm}</span>
                  <span className="text-slate-400">Key match</span>
                  <span className={validation.data.key_match?.ok ? 'text-emerald-600' : 'text-red-500'}>
                    {validation.data.key_match?.ok ? '✓ ' : '✗ '}{validation.data.key_match?.msg}
                  </span>
                </div>
                <p className={`rounded-lg px-3 py-2 ${validation.data.overall ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300' : 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300'}`}>
                  {validation.data.overall ? 'Certificate is valid and ready to use.' : 'Certificate needs attention (expired or key mismatch).'}
                </p>
              </>
            )}
            <div className="flex justify-end border-t border-slate-200 pt-3 dark:border-slate-700">
              <Button variant="secondary" onClick={() => setValidation(null)}>Close</Button>
            </div>
          </div>
        )}
      </Modal>

      {/* Self-signed modal */}
      <Modal open={selfOpen} onClose={() => setSelfOpen(false)} title="Generate self-signed certificate">
        <div className="space-y-3">
          <Field label="Domains" hint="Comma separated, e.g. example.com, *.example.com">
            <Input value={ss.domains} onChange={(e) => setSS({ ...ss, domains: e.target.value })} placeholder="example.com" />
          </Field>
          <Field label="Name (optional)">
            <Input value={ss.name} onChange={(e) => setSS({ ...ss, name: e.target.value })} />
          </Field>
          <Field label="Valid for (days)">
            <Input type="number" value={ss.days} min={1} max={3650} onChange={(e) => setSS({ ...ss, days: parseInt(e.target.value, 10) || 825 })} />
          </Field>
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="secondary" onClick={() => setSelfOpen(false)}>Cancel</Button>
            <Button onClick={() => selfSigned.mutate()} disabled={selfSigned.isPending || !ss.domains}>
              {selfSigned.isPending ? 'Generating…' : 'Generate'}
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  )
}
