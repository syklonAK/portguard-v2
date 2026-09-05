import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, RefreshCw, CheckCircle2, XCircle, ExternalLink, Terminal } from 'lucide-react'
import { api, type ToolState, type ToolInstallResult } from '../api'
import { Badge, Button, Card, CardHeader, CodeBlock, Spinner } from '../components/ui'
import { useToast } from '../components/toast'

const CATEGORY_LABELS: Record<string, { label: string; color: 'green' | 'blue' | 'amber' | 'slate' }> = {
  proxy: { label: 'Proxy core', color: 'green' },
  tunnel: { label: 'Tunnel', color: 'blue' },
  security: { label: 'Security', color: 'amber' },
  infra: { label: 'Infrastructure', color: 'slate' },
}

const CATEGORY_ORDER = ['proxy', 'tunnel', 'security', 'infra']

export default function Tools() {
  const qc = useQueryClient()
  const { push } = useToast()

  const tools = useQuery({ queryKey: ['tools'], queryFn: () => api.tools() })
  const [output, setOutput] = useState<ToolInstallResult | null>(null)
  const [installing, setInstalling] = useState<string | null>(null)

  const refresh = () => qc.invalidateQueries({ queryKey: ['tools'] })

  const install = useMutation({
    mutationFn: (id: string) => api.installTool(id),
    onMutate: (id) => setInstalling(id),
    onSuccess: (res) => {
      setOutput(res)
      if (res.ok) {
        push('success', `${res.tool_id} installed in ${res.elapsed}.`)
      } else {
        push('error', `${res.tool_id} install reported problems — see output.`)
      }
      refresh()
    },
    onError: (e: any) => push('error', e.message),
    onSettled: () => setInstalling(null),
  })

  const byCategory = (cat: string): ToolState[] =>
    (tools.data?.tools ?? []).filter((t) => t.category === cat)

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold">Tools</h1>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            Software PortGuard depends on — auto-detected, one-click install via the official installers.
            To install on a remote server, use Servers → its Tools.
          </p>
        </div>
        <Button variant="secondary" onClick={refresh}>
          <RefreshCw className="h-4 w-4" /> Re-detect
        </Button>
      </div>

      {tools.isLoading ? (
        <div className="flex justify-center py-20"><Spinner className="h-8 w-8" /></div>
      ) : (
        CATEGORY_ORDER.map((cat) => {
          const list = byCategory(cat)
          if (!list.length) return null
          const meta = CATEGORY_LABELS[cat]
          return (
            <Card key={cat}>
              <CardHeader title={meta.label} desc={`${list.filter((t) => t.installed).length}/${list.length} installed`} />
              <div className="grid grid-cols-1 gap-3 p-5 sm:grid-cols-2">
                {list.map((t) => (
                  <div key={t.id} className="flex flex-col rounded-xl border border-slate-200 p-4 dark:border-slate-700">
                    <div className="flex items-start justify-between gap-2">
                      <div>
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-semibold">{t.name}</span>
                          {t.installed ? (
                            <Badge color="green"><CheckCircle2 className="h-3 w-3" /> {t.version || 'installed'}</Badge>
                          ) : (
                            <Badge color="red"><XCircle className="h-3 w-3" /> missing</Badge>
                          )}
                        </div>
                        <p className="mt-1.5 text-2xs leading-relaxed text-slate-500 dark:text-slate-400">
                          {DESCRIPTIONS[t.id] || t.name}
                        </p>
                        {t.binary && (
                          <p className="mt-1 font-mono text-2xs text-slate-400">{t.binary}</p>
                        )}
                      </div>
                    </div>
                    <div className="mt-auto flex items-center justify-between pt-3">
                      <a
                        href={DOCS[t.id] || '#'}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1 text-2xs text-slate-400 hover:text-indigo-500"
                      >
                        <ExternalLink className="h-3 w-3" /> docs
                      </a>
                      <Button
                        size="sm"
                        variant={t.installed ? 'secondary' : 'primary'}
                        disabled={install.isPending && installing === t.id}
                        onClick={() => install.mutate(t.id)}
                        title={t.installed ? 'Reinstall / update' : 'Install'}
                      >
                        <Download className={`h-3.5 w-3.5 ${install.isPending && installing === t.id ? 'animate-bounce' : ''}`} />
                        {install.isPending && installing === t.id
                          ? 'Installing…'
                          : t.installed
                            ? 'Reinstall'
                            : 'Install'}
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            </Card>
          )
        })
      )}

      {output && (
        <Card>
          <CardHeader
            title={`Installer output — ${output.tool_id}`}
            desc={`finished in ${output.elapsed} · ${output.ok ? 'success' : 'problems reported'}`}
            right={<Badge color={output.ok ? 'green' : 'red'}>{output.ok ? 'ok' : 'error'}</Badge>}
          />
          <div className="p-5">
            <CodeBlock code={output.output || '(no output)'} label="tail of the official installer log" />
          </div>
        </Card>
      )}

      <Card>
        <CardHeader title="Remote installation" desc="Install tools on your other servers from here" />
        <div className="flex flex-wrap items-center gap-3 p-5 text-xs text-slate-500 dark:text-slate-400">
          <Terminal className="h-4 w-4 shrink-0" />
          <span>
            Go to <b>Servers</b>, pick a server card and use its <b>Tools</b> action — the same registry runs on the
            remote node through the node API, and the install executes there with the official installers.
          </span>
        </div>
      </Card>
    </div>
  )
}

const DESCRIPTIONS: Record<string, string> = {
  xray: 'Proxy core used by PasarGuard nodes and the PortGuard tunnel bridge (dokodemo→SOCKS5).',
  hedioum: 'Hedioum Pool Tunnel — the egress/hub binary for the two-server tunnel topology.',
  certbot: "Let's Encrypt client for issuing and renewing real TLS certificates.",
  wireguard: 'Kernel VPN backend used by PasarGuard wireguard nodes.',
  nginx: 'Primary web/proxy engine with TCP/UDP stream support.',
  haproxy: 'TCP/HTTP load balancer — the second PortGuard engine.',
}

const DOCS: Record<string, string> = {
  xray: 'https://xtls.github.io/en/config/',
  hedioum: 'https://github.com/hedioum/Hedioum-Pool-Tunnel',
  certbot: 'https://certbot.eff.org/',
  wireguard: 'https://www.wireguard.com/',
  nginx: 'https://nginx.org/en/docs/',
  haproxy: 'https://docs.haproxy.org/',
}
