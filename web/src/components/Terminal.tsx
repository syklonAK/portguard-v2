import { useEffect, useRef } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, Loader2, XCircle } from 'lucide-react'
import { api, type Job } from '../api'

// Live terminal view of a backend job (tool install, ACME issuance, ...):
// polls the job transcript and re-checks instantly on every SSE "job" event.
export default function Terminal({ jobId, onDone }: { jobId: string; onDone?: (job: Job) => void }) {
  const qc = useQueryClient()
  const boxRef = useRef<HTMLDivElement>(null)
  const lastDone = useRef<string | null>(null)

  const job = useQuery({
    queryKey: ['job', jobId],
    queryFn: () => api.job(jobId),
    refetchInterval: (q) => (q.state.data?.status === 'running' ? 1200 : false),
  })

  useEffect(() => {
    const h = () => qc.invalidateQueries({ queryKey: ['job', jobId] })
    window.addEventListener('pg-sse-job', h)
    return () => window.removeEventListener('pg-sse-job', h)
  }, [jobId, qc])

  useEffect(() => {
    const el = boxRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [job.data?.output?.length])

  useEffect(() => {
    const j = job.data
    if (j && j.status !== 'running' && lastDone.current !== j.id) {
      lastDone.current = j.id
      onDone?.(j)
    }
  }, [job.data?.status])

  const status = job.data?.status ?? 'running'
  const lines = job.data?.output ?? []

  return (
    <div className="overflow-hidden rounded-lg border border-slate-700 bg-slate-950">
      <div className="flex items-center justify-between border-b border-slate-800 px-3 py-1.5">
        <span className="font-mono text-2xs text-slate-400">{job.data?.name ?? 'task'}</span>
        <span className="flex items-center gap-1.5 text-2xs">
          {status === 'running' && (
            <span className="flex items-center gap-1 text-sky-300">
              <Loader2 className="h-3 w-3 animate-spin" /> running
            </span>
          )}
          {status === 'success' && (
            <span className="flex items-center gap-1 text-emerald-400">
              <CheckCircle2 className="h-3 w-3" /> success
            </span>
          )}
          {status === 'failed' && (
            <span className="flex items-center gap-1 text-red-400">
              <XCircle className="h-3 w-3" /> failed
            </span>
          )}
        </span>
      </div>
      <div ref={boxRef} className="h-56 overflow-y-auto px-3 py-2 font-mono text-2xs leading-relaxed text-slate-300">
        {lines.length === 0 && status === 'running' && (
          <p className="text-slate-500">waiting for output…</p>
        )}
        {lines.map((l, i) => (
          <div key={i} className={`whitespace-pre-wrap ${l.startsWith('[portguard]') ? 'text-indigo-300' : ''}`}>
            {l || '\u00a0'}
          </div>
        ))}
        {job.data?.error && <div className="mt-1 text-red-400">{job.data.error}</div>}
      </div>
    </div>
  )
}
