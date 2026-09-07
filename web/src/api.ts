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
  routes: RouteRule[]
  service_id: number | null
  host_header: string
  decoy: '' | 'builtin' | 'custom'
  decoy_html: string
  notes: string
  created_at: string
  updated_at: string
}

export interface Cert {
  id: number
  name: string
  type: 'manual' | 'selfsigned' | 'acme'
  domains: string[]
  expires_at: string | null
  created_at: string
}

export interface ACMEEnvField {
  key: string
  label: string
  secret: boolean
  required: boolean
}

export interface ACMEProvider {
  id: string
  name: string
  acmesh_dns?: string
  certbot_pkg?: string
  certbot_flag?: string
  env: ACMEEnvField[]
}

export interface ACMEIssueResult {
  cert: Cert
  note: string
}

export interface DiscoveredPanel {
  kind: string
  source: string
  name: string
  url: string
  port: number
  tls: boolean
  alive: boolean
  env_path?: string
  env_username?: string
  env_has_password?: boolean
  note?: string
}

export interface Job {
  id: string
  name: string
  status: 'running' | 'success' | 'failed'
  error?: string
  started_at: string
  finished_at?: string | null
  output?: string[]
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
  created_at: string // RFC3339
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
  tunnel_socks_host?: string
  tunnel_socks_port?: string
  pasarguard_url?: string
  pasarguard_username?: string
  pasarguard_password_set?: boolean
  pasarguard_token_set?: boolean
  rate_limiting_enabled?: string
  rate_limiting_sync_interval?: string
  alerts_enabled?: string
  alert_cooldown_min?: string
  alert_telegram_token_set?: boolean
  alert_telegram_chat?: string
  alert_webhook_set?: boolean
  alert_cpu_min?: string
  alert_ram_min?: string
  alert_disk_min?: string
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
  me: () => req<{ username: string; role: string }>('GET', '/api/me'),
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

  // ACME (Let's Encrypt) issuance via acme.sh / certbot, wildcard-capable
  acmeProviders: () => req<ACMEProvider[]>('GET', '/api/certs/acme/providers'),
  issueCert: (r: {
    tool: string
    method: 'http01' | 'dns01'
    domains: string[]
    email?: string
    dns_provider?: string
    dns_env?: Record<string, string>
    name?: string
  }) => req<{ job_id: string }>('POST', '/api/certs/issue?stream=1', r),
  renewCert: (id: number) =>
    req<{ job_id: string }>('POST', `/api/certs/${id}/renew?stream=1`),

  healthList: () => req<TargetHealth[]>('GET', '/api/health'),
  audit: () => req<AuditLog[]>('GET', '/api/audit'),
  certValidate: (id: number) => req<CertValidation>('GET', `/api/certs/${id}/validate`),
  jobs: () => req<Job[]>('GET', '/api/jobs'),
  discover: () => req<DiscoveredPanel[]>('GET', '/api/discover'),
  discoverApply: (r: { kind: string; url: string; username?: string; password?: string; token?: string; env_path?: string }) =>
    req<{ ok: boolean }>('POST', '/api/discover/apply', r),
  job: (id: string) => req<Job>('GET', `/api/jobs/${id}`),

  // v2.2: Hedioum tunnels
  tunnelStatus: () => req<TunnelStatus>('GET', '/api/tunnels'),
  listRelays: () => req<TunnelRelay[]>('GET', '/api/tunnels/relays'),
  createRelay: (r: Partial<TunnelRelay>) => req<{ id: number }>('POST', '/api/tunnels/relays', r),
  updateRelay: (id: number, r: Partial<TunnelRelay>) => req<{ ok: boolean }>('PUT', `/api/tunnels/relays/${id}`, r),
  deleteRelay: (id: number) => req<{ ok: boolean }>('DELETE', `/api/tunnels/relays/${id}`),
  tunnelValidate: () => req<{ ok: boolean; error?: string; note?: string }>('POST', '/api/tunnels/validate'),
  tunnelApply: () => req<{ ok: boolean; relays: number }>('POST', '/api/tunnels/apply'),

  // v2.2: self-updater
  selfUpdate: () => req<{ ok: boolean; note: string }>('POST', '/api/update'),

  // v2.4: tools (local + remote)
  tools: () => req<{ tools: ToolState[] }>('GET', '/api/tools'),
  installTool: (id: string) => req<ToolInstallResult>('POST', `/api/tools/${id}/install`),
  installToolStream: (id: string) => req<{ job_id: string }>('POST', `/api/tools/${id}/install?stream=1`),
  nodeTools: (nodeId: number) => req<{ tools: ToolState[] }>('POST', `/api/nodes/${nodeId}/tools`),
  nodeInstallTool: (nodeId: number, tool: string) =>
    req<ToolInstallResult>('POST', `/api/nodes/${nodeId}/install`, { tool }),

  // v2.5: remote node management
  nodeMappings: (nodeId: number) => req<any[]>('GET', `/api/nodes/${nodeId}/mappings`),
  nodeCreateMapping: (nodeId: number, m: any) => req<any>('POST', `/api/nodes/${nodeId}/mappings`, m),
  nodeUpdateMapping: (nodeId: number, mid: number, m: any) => req<any>('PUT', `/api/nodes/${nodeId}/mappings/${mid}`, m),
  nodeDeleteMapping: (nodeId: number, mid: number) => req<any>('DELETE', `/api/nodes/${nodeId}/mappings/${mid}`),
  nodeCerts: (nodeId: number) => req<any[]>('GET', `/api/nodes/${nodeId}/certs`),
  nodeDeleteCert: (nodeId: number, cid: number) => req<any>('DELETE', `/api/nodes/${nodeId}/certs/${cid}`),
  nodeCreateCert: (nodeId: number, c: any) => req<any>('POST', `/api/nodes/${nodeId}/certs`, c),
  nodeApply: (nodeId: number) => req<any>('POST', `/api/nodes/${nodeId}/apply`),

  // v2.2: live connections
  connections: (params?: { dst_port?: string; managed?: '1' | '0' }) => {
    const q = new URLSearchParams()
    if (params?.dst_port) q.set('dst_port', params.dst_port)
    if (params?.managed) q.set('managed', params.managed)
    const qs = q.toString()
    return req<ConnectionsData>('GET', '/api/connections' + (qs ? `?${qs}` : ''))
  },

  // v2.3: multi-server management
  listNodes: () => req<ServerNode[]>('GET', '/api/nodes'),
  createNode: (n: Partial<ServerNode>) => req<{ id: number }>('POST', '/api/nodes', n),
  updateNode: (id: number, n: Partial<ServerNode>) => req<{ ok: boolean }>('PUT', `/api/nodes/${id}`, n),
  deleteNode: (id: number) => req<{ ok: boolean }>('DELETE', `/api/nodes/${id}`),
  nodeAction: (id: number, action: 'probe' | 'summary' | 'apply') =>
    req<any>('POST', `/api/nodes/${id}/${action}`),
  nodeSelf: () => req<{ configured: boolean; role: string }>('GET', '/api/node-self'),
  putNodeSelf: (body: { token?: string; role?: string }) =>
    req<{ configured: boolean; role: string }>('PUT', '/api/node-self', body),

  // v2.6: bandwidth / per-UUID rate limiting
  rateLimitStatus: () => req<RateLimitStatus>('GET', '/api/rate-limits/status'),
  listRateProfiles: () => req<RateProfile[]>('GET', '/api/rate-limits/profiles'),
  createRateProfile: (p: Partial<RateProfile>) => req<RateProfile>('POST', '/api/rate-limits/profiles', p),
  updateRateProfile: (id: number, p: Partial<RateProfile>) => req<RateProfile>('PUT', `/api/rate-limits/profiles/${id}`, p),
  deleteRateProfile: (id: number) => req<{ ok: boolean }>('DELETE', `/api/rate-limits/profiles/${id}`),
  listPasarguardUsers: () => req<PasarguardUserView[]>('GET', '/api/pasarguard/users'),
  listPasarguardUsersPaged: (p: { limit: number; offset: number; search?: string; status?: string }) => {
    const q = new URLSearchParams({ limit: String(p.limit), offset: String(p.offset) })
    if (p.search) q.set('search', p.search)
    if (p.status) q.set('status', p.status)
    return req<{ users: PasarguardUserView[]; total: number }>('GET', `/api/pasarguard/users?${q}`)
  },
  upsertRatePolicy: (p: { uuid: string; node_id: number; profile_id?: number | null; download_bps?: number; upload_bps?: number; custom?: boolean; enabled?: boolean }) =>
    req<{ ok: boolean }>('POST', '/api/rate-limits/policies', p),
  deleteRatePolicy: (uuid: string, nodeId: number) =>
    req<{ ok: boolean }>('DELETE', `/api/rate-limits/policies/${uuid}?node_id=${nodeId}`),
  pushRateLimits: () => req<{ results: { node_id: number; ok: boolean; error?: string }[] }>('POST', '/api/rate-limits/push'),
  syncPasarGuard: () => req<{ synced: number; ok: boolean }>('POST', '/api/rate-limits/sync'),

  // v2.7: config versions
  listVersions: () => req<ConfigVersion[]>('GET', '/api/versions'),
  getVersion: (v: number) => req<{ version: ConfigVersion; mappings: any[]; relays: any[] }>('GET', `/api/versions/${v}`),
  diffVersions: (from: number, to: number) => req<{ diff: string[] }>('GET', `/api/versions/${from}/diff/${to}`),
  restoreVersion: (v: number) => req<{ ok: boolean; restored_mappings: number }>('POST', `/api/versions/${v}/restore`),
  downloadVersion: (v: number) => `/api/versions/${v}/download`,

  // v2.7: alerts
  listAlerts: (limit = 100) => req<AlertsData>('GET', `/api/alerts?limit=${limit}`),
  ackAlert: (id: number) => req<{ ok: boolean }>('POST', `/api/alerts/${id}/ack`),
  testAlert: () => req<{ ok: boolean; note: string }>('POST', '/api/alerts/test'),

  // v2.7: user management (owner)
  listUsers: () => req<AdminUser[]>('GET', '/api/users'),
  createUser: (u: { username: string; password: string; role: string }) =>
    req<{ id: number }>('POST', '/api/users', u),
  updateUser: (id: number, body: { role?: string }) => req<{ ok: boolean }>('PUT', `/api/users/${id}`, body),
  deleteUser: (id: number) => req<{ ok: boolean }>('DELETE', `/api/users/${id}`),

  // v2.7: node logs
  nodeLogs: (nodeId: number, source: string, lines = 200) =>
    req<{ source: string; lines: string }>('GET', `/api/nodes/${nodeId}/logs/${source}?lines=${lines}`),

  // v2.7: config import from live files
  importScan: () => req<ImportScan>('GET', '/api/import/scan'),
  importConfirm: (indices: number[]) =>
    req<{ created: number; skipped: number; issues: { section: string; reason: string }[] }>('POST', '/api/import/confirm', { indices }),

  // v2.8: services + metrics
  listServices: () => req<ServiceView[]>('GET', '/api/app-services'),
  createService: (s: Partial<Service>) => req<Service>('POST', '/api/app-services', s),
  updateService: (id: number, s: Partial<Service>) => req<Service>('PUT', `/api/app-services/${id}`, s),
  deleteService: (id: number) => req<{ ok: boolean }>('DELETE', `/api/app-services/${id}`),
  serviceDetail: (id: number) => req<{ service: Service; mappings: Mapping[]; health: any[] }>('GET', `/api/app-services/${id}`),
  metrics: (nodeId: number, range: '5m' | '1h' | '24h' | '7d') =>
    req<MetricsData>('GET', `/api/metrics/${nodeId}?range=${range}`),

  eventsUrl: () => `/api/events?token=${encodeURIComponent(getToken() || '')}`,
}

// ---- v2.8 services + metrics ----

export interface Service {
  id: number
  name: string
  description: string
  enabled: boolean
  notes: string
  created_at: string
  updated_at: string
}

export interface ServiceView extends Service {
  mapping_count: number
  enabled_count: number
  backends_up: number
  backends_down: number
}

export interface MetricPoint {
  node_id: number
  cpu_percent: number
  mem_percent: number
  disk_percent: number
  rx_bytes: number
  tx_bytes: number
  rx_bps: number
  tx_bps: number
  conns: number
  ts: number
}

export interface MetricsData {
  node_id: number
  points: MetricPoint[]
  rx_total: number
  tx_total: number
  rx_peak: number
  tx_peak: number
  active_conns: number
}

export interface RouteRule {
  id: number
  path: string
  enabled: boolean
  targets: Target[]
  redirect?: string
  notes?: string
}

export interface ImportScan {
  mappings: Partial<Mapping>[]
  issues: { section: string; reason: string }[]
  found: boolean
}

// ---- v2.7 users ----

export interface AdminUser {
  id: number
  username: string
  role: 'owner' | 'admin' | 'operator' | 'viewer'
  created_at: string
  last_login_at: string | null
}

// ---- v2.3 multi-server types ----

export interface ServerNode {
  id: number
  name: string
  host: string
  port: number
  role: 'standalone' | 'master' | 'iran' | 'foreign' | 'generic'
  enabled: boolean
  notes: string
  status: 'online' | 'offline' | 'unknown'
  last_seen: string | null
  conn_mode: 'direct' | 'reverse'
  uid: string
  created_at: string
  updated_at: string
}

// ---- v2.4 tools ----

export interface ToolState {
  id: string
  name: string
  installed: boolean
  version?: string
  binary?: string
  category: 'proxy' | 'tunnel' | 'security' | 'infra'
}

export interface ToolInstallResult {
  tool_id: string
  ok: boolean
  output: string
  elapsed: string
}

// ---- v2.6 bandwidth ----

export interface RateProfile {
  id: number
  name: string
  download_bps: number
  upload_bps: number
  enabled: boolean
  notes: string
  created_at: string
  updated_at: string
}

export interface PasarguardUserView {
  id: number
  uuid: string
  username: string
  node_id: number | null
  enabled: boolean
  expired: boolean
  last_ip: string
  synced_at: string
  has_policy: boolean
  custom: boolean
  policy_download_bps: number
  policy_upload_bps: number
  profile_name: string
  policy_state: string
}

// ---- v2.7 config versions ----

// ---- v2.7 alerts ----

export interface Alert {
  id: number
  severity: 'info' | 'warning' | 'critical'
  category: string
  title: string
  detail: string
  target: string
  acknowledged: boolean
  created_at: string
}

export interface AlertsData {
  alerts: Alert[]
  unacknowledged: number
  enabled: boolean
}

export interface ConfigVersion {
  id: number
  version: number
  author: string
  description: string
  node_count: number
  deploy_result: string
  created_at: string
}

export interface RateLimitStatus {
  enabled: boolean
  total_users: number
  limited_users: number
  failed: number
  pending: number
  active_nodes: number
  last_sync: string
}

export interface NodeSummary {
  version: string
  role: string
  system: {
    cpu_percent: number
    mem_total: number
    mem_used: number
    mem_percent: number
    disk_percent: number
    load1: number
    uptime: number
    platform: string
    num_cpu: number
  }
  mappings: { total: number; enabled: number }
  ports: { total: number; unmanaged: number }
  health: { up: number; down: number }
  tunnel: {
    hedioum_installed: boolean
    hedioum_active: string
    xray_installed: boolean
    bridge_active: string
    socks_listening?: string
    role: string
  }
}

// ---- v2.2 live connections ----

export interface ConnEntry {
  src_ip: string
  src_port: number
  dst_ip: string
  dst_port: number
  process: string
  pid: number
  state: string
  managed: boolean
  inner: boolean
  self: boolean
  first_seen: number
  last_seen: number
}

export interface TopTalker {
  src_ip: string
  conns: number
  targets: string
  first_seen: number
}

export interface ConnectionsData {
  connections: ConnEntry[]
  top_talkers: TopTalker[]
  total: number
}

// ---- v2.2 tunnel types ----

export interface TunnelStatus {
  hedioum_installed: boolean
  hedioum_version?: string
  hedioum_active: string
  hedioum_binary?: string
  xray_installed: boolean
  xray_version?: string
  xray_binary?: string
  bridge_active: string
  socks_listening?: string
  role: string
}

export interface TunnelRelay {
  id: number
  name: string
  mode: 'raw' | 'tls'
  enabled: boolean
  target_host: string
  target_port: number
  listen_ip: string
  listen_port: number
  bridge_port: number
  udp: boolean
  host_header: string
  domain: string
  ssl_cert_id: number | null
  notes: string
  created_at: string
  updated_at: string
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
