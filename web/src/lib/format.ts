// Shared formatting helpers (previously duplicated across pages).

/** Human bandwidth: 300 -> "300 bps", 30000000 -> "30 Mbps". Non-positive is caller's choice to map (e.g. ∞). */
export function fmtBps(bps: number): string {
  if (!bps || bps <= 0) return '0 bps'
  const units = ['bps', 'Kbps', 'Mbps', 'Gbps', 'Tbps']
  let v = bps
  let i = 0
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i++
  }
  return `${v >= 100 ? Math.round(v) : Math.round(v * 100) / 100} ${units[i]}`
}

/** Human bytes: 1073741824 -> "1.00 GB". */
export function fmtBytes(b: number): string {
  if (!b || b <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let v = b
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${Math.round(v * 100) / 100} ${units[i]}`
}

/** Parse user bandwidth input ("30", "30Mbps", "512Kbps", "unlimited"/"∞") -> bps; -1 on garbage.
 * A bare number is interpreted as Mbps (the dashboard's historical convention). */
export function parseBpsInput(s: string): number {
  const t = s.trim().toLowerCase()
  if (!t || t === 'unlimited' || t === '∞' || t === '0') return 0
  const m = t.match(/^([0-9.]+)\s*(gbps|mbps|kbps|bps)?$/)
  if (!m) return -1
  const n = parseFloat(m[1])
  if (isNaN(n) || n < 0) return -1
  const mult: Record<string, number> = { bps: 1, kbps: 1e3, mbps: 1e6, gbps: 1e9 }
  return Math.round(n * (mult[m[2] ?? 'mbps']))
}

/** "3m ago" style relative time from an ISO timestamp. */
export function fmtAgo(iso: string | null): string {
  if (!iso) return '—'
  const s = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 0) return 'now'
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

/** Compact uptime: 90061 -> "1d 1h". */
export function fmtUptimeShort(sec: number): string {
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}
