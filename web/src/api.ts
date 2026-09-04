// Typed API client for the PortGuard backend.
export interface Target {
  host: string
  port: number
  weight?: number
  backup?: boolean
}

export type PathTransport = 'ws' | 'httpupgrade' | 'xhttp'

export interface PathRoute {
  transport: PathTransport
  prefix: string
  min_port: number
  max_port: number
}

export interface ACLRule {
  action: 'allow' | 'deny'
  value: string // IP or CIDR
}

export interface Mapping {
  id: number
  name: string
  enabled: boolean
  engine: 'nginx' | 'haproxy'
  protocol: 'http' | 'https' | 'tcp' | 'udp'
  listen_ip: string
  listen_port: number
  server_names: string[]
  ssl_cert_id: number | null
  redirect_to: string
  websocket: boolean
  http2: boolean
  targets: Target[]
  balance: string
  path_prefix: string
  access_rules: ACLRule[]
  extra_headers: Record<string, string>
  path_routes: PathRoute[]
  notes: string
  created_at: string
  updated_at: string
}

export interface Cert {
  id: number
  name: string
  type: 'manual' | 'selfsigned'
  domains: string[]
  expires_at: string | null
  created_at: string
}

export interface PortEntry {
  port: number
  proto: 'tcp' | 'udp'
  listen_ip: string
  process: string
  pid: number
  user: string
  classification: string
  managed: boolean
  self: boolean
}

export interface TargetHealth {
  mapping_id: number
  target_index: number
  host: string
  port: number
  status: 'up' | 'down' | 'unknown'
  latency_ms: number
  fail_count: number
  last_check_at: string | null
}

export interface AuditLog {
  id: number
  actor: string
  action: string
  detail: string
  status: 'ok' | 'error'
  created_at: number // unix seconds
}

export interface SystemInfo {
  system: {
    cpu_percent: number
    mem_total: number
    mem_used: number
    mem_percent: number
    disk_total: number
    disk_used: number
    disk_percent: number
    load1: number
    uptime: number
    platform: string
    kernel: string
    num_cpu: number
    go_version: string
  }
  mappings: { total: number; enabled: number }
  ports: { managed: number; unmanaged: number; total: number }
  health: { up: number; down: number; unknown: number }
  version: string
}

export interface Settings {
  paths: {
    nginx_conf: string
    haproxy_conf: string
    certs_dir: string
    backups_dir: string
    nginx_bin: string
    haproxy_bin: string
    haproxy_socket: string
  }
  check_interval: string
  scan_interval: string
  auto_apply: string
  panel_port: number
}

// ---- v2 feature types (ported from haproxy-manager) ----

export interface ServiceStatus {
  [engine: string]: {
    unit: string
    binary_installed: boolean
    active?: string
    sub?: string
    enabled_state?: string
    pid?: string
    description?: string
    error?: string
  }
}

export interface BackupFile {
  engine: string
  live_path: string
  size: number
}

export interface Backup {
  timestamp: string
  files: BackupFile[]
  created_at: string
}

export interface BackupDiff {
  engine: string
  live_path: string
  diff: string
  same: boolean
}

export interface HAProxyServerRow {
  pxname: string
  svname: string
  status: string
  scur?: string
  stot?: string
  bin?: string
  bout?: string
  check_status?: string
  check_duration?: string
  addr?: string
  [key: string]: string | undefined
}

export interface RuntimeData {
  available: boolean
  socket: string
  error?: string
  info?: Record<string, string>
  summary?: {
    frontends: number
    backends: number
    servers: number
    up: number
    down: number
    maint: number
    sessions: number
    bytes_in: number
    bytes_out: number
  }
  servers?: HAProxyServerRow[]
  stats_error?: string
}

export type DiagCheck = 'tcp' | 'dns' | 'tls' | 'http' | 'backend'

export interface DiagRequest {
  check: DiagCheck
  host: string
  port?: number
  use_tls?: boolean
  path?: string
  host_header?: string
}

export interface MappingTemplate {
  id: string
  name: string
  description: string
  mapping: Partial<Mapping>
}

export interface EngineValidation {
  ok: boolean
  error?: string
  diff?: string
}

export interface ConfigFile {
  path: string
  content: string
  exists: boolean
}

export interface CertValidation {
  overall: boolean
  error?: string
  cert?: {
    subject: string
    issuer: string
    not_before: string
    not_after: string
    days_left: number
    expired: boolean
    near_expiry: boolean
    dns_names: string[]
    key_algorithm: string
    serial_number: string
  }
  key_match?: { ok: boolean; msg: string }
}

export interface ImportResult {
  created: number
  skipped: number
  errors: string[]
}

const TOKEN_KEY = 'pg_token'

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}
export function setToken(t: string | null) {
  if (t) localStorage.setItem(TOKEN_KEY, t)
  else localStorage.removeItem(TOKEN_KEY)
}

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  const token = getToken()
  if (token) headers['Authorization'] = `Bearer ${token}`
  if (body !== undefined) headers['Content-Type'] = 'application/json'

  const res = await fetch(path, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })

  if (res.status === 401 && !path.startsWith('/api/login')) {
    setToken(null)
    window.location.href = '/login'
    throw new ApiError('unauthorized', 401)
  }

  let data: any = null
  try {
    data = await res.json()
  } catch {
    /* empty body */
  }
  if (!res.ok) {
    throw new ApiError(data?.error || `HTTP ${res.status}`, res.status)
  }
  return data as T
}

export const api = {
  setupStatus: () => req<{ setup_required: boolean }>('GET', '/api/setup-status'),
  setup: (username: string, password: string) =>
    req<{ status: string }>('POST', '/api/setup', { username, password }),
  login: (username: string, password: string) =>
    req<{ token: string; username: string }>('POST', '/api/login', { username, password }),
  me: () => req<{ username: string }>('GET', '/api/me'),
  changePassword: (oldPw: string, newPw: string) =>
    req<{ status: string }>('POST', '/api/account/password', { old: oldPw, new: newPw }),

  system: () => req<SystemInfo>('GET', '/api/system'),
  settings: () => req<Settings>('GET', '/api/settings'),
  putSettings: (s: Partial<Settings>) => req<Settings>('PUT', '/api/settings', s),

  listMappings: () => req<Mapping[]>('GET', '/api/mappings'),
  createMapping: (m: Partial<Mapping>) => req<Mapping>('POST', '/api/mappings', m),
  updateMapping: (id: number, m: Partial<Mapping>) => req<Mapping>('PUT', `/api/mappings/${id}`, m),
  deleteMapping: (id: number) => req<{ status: string }>('DELETE', `/api/mappings/${id}`),

  apply: () => req<{ status: string }>('POST', '/api/apply'),
  validate: () => req<Record<string, EngineValidation>>('POST', '/api/validate'),

  // v2: service control
  services: () => req<ServiceStatus>('GET', '/api/services'),
  serviceAction: (engine: string, action: string) =>
    req<{ status: string; output: string }>('POST', `/api/services/${engine}/${action}`),

  // v2: HAProxy runtime API
  runtime: () => req<RuntimeData>('GET', '/api/runtime/haproxy'),
  runtimeSetServerState: (backend: string, server: string, state: 'ready' | 'drain' | 'maint') =>
    req<{ status: string }>('POST', `/api/runtime/haproxy/servers/${encodeURIComponent(backend)}/${encodeURIComponent(server)}/state`, { state }),

  // v2: backups
  backups: () => req<Backup[]>('GET', '/api/backups'),
  backupDiff: (ts: string) => req<BackupDiff[]>('GET', `/api/backups/${encodeURIComponent(ts)}/diff`),
  backupRestore: (ts: string) => req<{ status: string }>('POST', `/api/backups/${encodeURIComponent(ts)}/restore`),
  backupDelete: (ts: string) => req<{ status: string }>('DELETE', `/api/backups/${encodeURIComponent(ts)}`),

  // v2: diagnostics
  diagnostics: (r: DiagRequest) => req<Record<string, any>>('POST', '/api/diagnostics', r),

  // v2: templates / import / export / config viewer
  templates: () => req<MappingTemplate[]>('GET', '/api/templates'),
  exportSnapshot: () => req<Record<string, any>>('GET', '/api/export'),
  importSnapshot: (mappings: Partial<Mapping>[]) => req<ImportResult>('POST', '/api/import', { mappings }),
  configView: (engine: string) => req<{ engine: string; files: ConfigFile[] }>('GET', `/api/config/${engine}`),

  listPorts: () => req<{ ports: PortEntry[]; last_scan_at: string }>('GET', '/api/ports'),
  scan: () => req<{ ports: PortEntry[]; count: number }>('POST', '/api/ports/scan'),

  listCerts: () => req<Cert[]>('GET', '/api/certs'),
  createCert: (c: { name: string; cert_pem: string; key_pem: string; domains?: string[] }) =>
    req<Cert>('POST', '/api/certs', c),
  selfSignedCert: (c: { name?: string; domains: string[]; days?: number }) =>
    req<Cert>('POST', '/api/certs/selfsigned', c),
  deleteCert: (id: number) => req<{ status: string }>('DELETE', `/api/certs/${id}`),

  healthList: () => req<TargetHealth[]>('GET', '/api/health'),
  audit: () => req<AuditLog[]>('GET', '/api/audit'),
  certValidate: (id: number) => req<CertValidation>('GET', `/api/certs/${id}/validate`),

  eventsUrl: () => `/api/events?token=${encodeURIComponent(getToken() || '')}`,
}

export function fmtBytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1)
  return `${(n / Math.pow(1024, i)).toFixed(1)} ${units[i]}`
}

export function fmtUptime(sec: number): string {
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d > 0) return `${d}d ${h}h ${m}m`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}
