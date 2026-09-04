import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, Upload } from 'lucide-react'
import { api, type ImportResult } from '../api'
import { Button, Card, CardHeader, Field, Input, Spinner, Toggle } from '../components/ui'
import { useToast } from '../components/toast'

export default function Settings() {
  const qc = useQueryClient()
  const { push } = useToast()
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api.settings() })
  const fileInput = useRef<HTMLInputElement>(null)

  const [paths, setPaths] = useState({
    nginx_conf: '', haproxy_conf: '', certs_dir: '', backups_dir: '', nginx_bin: '', haproxy_bin: '', haproxy_socket: '',
  })
  const [checkInterval, setCheckInterval] = useState('30')
  const [scanInterval, setScanInterval] = useState('300')
  const [autoApply, setAutoApply] = useState(false)

  const [pw, setPw] = useState({ old: '', new: '', confirm: '' })

  useEffect(() => {
    if (settings.data) {
      setPaths(settings.data.paths)
      setCheckInterval(settings.data.check_interval)
      setScanInterval(settings.data.scan_interval)
      setAutoApply(settings.data.auto_apply === 'true')
    }
  }, [settings.data])

  const save = useMutation({
    mutationFn: () =>
      api.putSettings({
        paths,
        check_interval: checkInterval,
        scan_interval: scanInterval,
        auto_apply: autoApply ? 'true' : 'false',
      }),
    onSuccess: () => {
      push('success', 'Settings saved')
      qc.invalidateQueries({ queryKey: ['settings'] })
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
        <div className="border-t border-slate-200 px-5 py-4 dark:border-slate-800">
          <Button onClick={() => save.mutate()} disabled={save.isPending}>
            {save.isPending ? 'Saving…' : 'Save settings'}
          </Button>
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
