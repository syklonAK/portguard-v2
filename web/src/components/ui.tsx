import type { ReactNode, ButtonHTMLAttributes, InputHTMLAttributes, SelectHTMLAttributes } from 'react'
import { useState } from 'react'
import { X, Check, Copy } from 'lucide-react'

export function Button({
  variant = 'primary',
  size = 'md',
  className = '',
  children,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'primary' | 'secondary' | 'danger' | 'ghost' | 'success'
  size?: 'sm' | 'md'
}) {
  const base =
    'inline-flex items-center justify-center gap-1.5 font-medium rounded-lg transition-colors focus:outline-none focus:ring-2 focus:ring-indigo-500/50 disabled:opacity-50 disabled:cursor-not-allowed'
  const sizes = { sm: 'h-8 px-3 text-xs', md: 'h-9.5 px-4 text-sm' }
  const variants = {
    primary: 'bg-indigo-600 text-white hover:bg-indigo-500 shadow-sm',
    secondary:
      'bg-white text-slate-700 border border-slate-300 hover:bg-slate-50 dark:bg-slate-800 dark:text-slate-200 dark:border-slate-600 dark:hover:bg-slate-700',
    danger: 'bg-red-600 text-white hover:bg-red-500 shadow-sm',
    success: 'bg-emerald-600 text-white hover:bg-emerald-500 shadow-sm',
    ghost: 'text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800',
  }
  return (
    <button className={`${base} ${sizes[size]} ${variants[variant]} ${className}`} {...props}>
      {children}
    </button>
  )
}

export function Card({ children, className = '' }: { children: ReactNode; className?: string }) {
  return (
    <div
      className={`rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900 ${className}`}
    >
      {children}
    </div>
  )
}

export function CardHeader({ title, desc, right }: { title: string; desc?: string; right?: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 border-b border-slate-200 px-5 py-4 dark:border-slate-800">
      <div>
        <h3 className="text-sm font-semibold">{title}</h3>
        {desc && <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">{desc}</p>}
      </div>
      {right}
    </div>
  )
}

export function Input({ className = '', ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={`h-9.5 w-full rounded-lg border border-slate-300 bg-white px-3 text-sm placeholder:text-slate-400
        focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20
        dark:border-slate-600 dark:bg-slate-800 dark:placeholder:text-slate-500 ${className}`}
      {...props}
    />
  )
}

export function Select({ className = '', children, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={`h-9.5 w-full rounded-lg border border-slate-300 bg-white px-2.5 text-sm
        focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20
        dark:border-slate-600 dark:bg-slate-800 ${className}`}
      {...props}
    >
      {children}
    </select>
  )
}

export function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: string }) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-300">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-2xs text-slate-400">{hint}</span>}
    </label>
  )
}

export function Badge({
  color = 'slate',
  children,
}: {
  color?: 'slate' | 'green' | 'red' | 'amber' | 'blue' | 'purple' | 'cyan'
  children: ReactNode
}) {
  const colors = {
    slate: 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300',
    green: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300',
    red: 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300',
    amber: 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300',
    blue: 'bg-sky-100 text-sky-700 dark:bg-sky-900/40 dark:text-sky-300',
    purple: 'bg-violet-100 text-violet-700 dark:bg-violet-900/40 dark:text-violet-300',
    cyan: 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900/40 dark:text-cyan-300',
  }
  return (
    <span className={`inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-2xs font-medium ${colors[color]}`}>
      {children}
    </span>
  )
}

export function Modal({
  open,
  onClose,
  title,
  children,
  wide = false,
}: {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
  wide?: boolean
}) {
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/50 backdrop-blur-sm" onClick={onClose} />
      <div
        className={`relative max-h-[90vh] w-full ${wide ? 'max-w-3xl' : 'max-w-lg'} overflow-y-auto rounded-xl border
          border-slate-200 bg-white shadow-2xl dark:border-slate-700 dark:bg-slate-900`}
      >
        <div className="sticky top-0 flex items-center justify-between border-b border-slate-200 bg-white px-5 py-3.5 dark:border-slate-700 dark:bg-slate-900">
          <h2 className="text-sm font-semibold">{title}</h2>
          <button onClick={onClose} className="rounded-md p-1 text-slate-400 hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-800">
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="p-5">{children}</div>
      </div>
    </div>
  )
}

export function Spinner({ className = 'h-5 w-5' }: { className?: string }) {
  return (
    <svg className={`animate-spin text-indigo-500 ${className}`} viewBox="0 0 24 24" fill="none">
      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
      <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v4a4 4 0 00-4 4H4z" />
    </svg>
  )
}

export function StatusDot({ status }: { status: 'up' | 'down' | 'unknown' | 'ok' | 'error' }) {
  const cls =
    status === 'up' || status === 'ok'
      ? 'bg-emerald-500'
      : status === 'down' || status === 'error'
        ? 'bg-red-500'
        : 'bg-slate-400'
  return <span className={`inline-block h-2 w-2 rounded-full ${cls} ${status === 'up' ? 'animate-pulse' : ''}`} />
}

export function Empty({ message }: { message: string }) {
  return <div className="px-5 py-12 text-center text-sm text-slate-400">{message}</div>
}

export function Toggle({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      type="button"
      onClick={() => onChange(!checked)}
      className={`relative h-5.5 w-10 rounded-full transition-colors ${checked ? 'bg-indigo-600' : 'bg-slate-300 dark:bg-slate-600'}`}
    >
      <span
        className={`absolute top-0.5 h-4.5 w-4.5 rounded-full bg-white shadow transition-all ${checked ? 'left-5' : 'left-0.5'}`}
      />
    </button>
  )
}

// ---- Tabs ----

export function Tabs({
  tabs,
  active,
  onChange,
}: {
  tabs: { id: string; label: string; icon?: React.ComponentType<{ className?: string }> }[]
  active: string
  onChange: (id: string) => void
}) {
  return (
    <div className="flex gap-1 rounded-xl border border-slate-200 bg-slate-50 p-1 dark:border-slate-700 dark:bg-slate-800/60">
      {tabs.map((t) => {
        const Icon = t.icon
        return (
          <button
            key={t.id}
            type="button"
            onClick={() => onChange(t.id)}
            className={`flex flex-1 items-center justify-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
              active === t.id
                ? 'bg-white text-indigo-700 shadow-sm dark:bg-slate-900 dark:text-indigo-300'
                : 'text-slate-500 hover:text-slate-700 dark:text-slate-400 dark:hover:text-slate-200'
            }`}
          >
            {Icon && <Icon className="h-3.5 w-3.5" />}
            {t.label}
          </button>
        )
      })}
    </div>
  )
}

// ---- Code / JSON editor ----

export function CodeEditor({
  value,
  onChange,
  onValidate,
  placeholder,
  invalid,
  rows = 10,
  readOnly = false,
  className = '',
}: {
  value: string
  onChange?: (v: string) => void
  onValidate?: (valid: boolean, parsed: unknown) => void
  placeholder?: string
  invalid?: boolean
  rows?: number
  readOnly?: boolean
  className?: string
}) {
  const handle = (v: string) => {
    onChange?.(v)
    if (onValidate) {
      try {
        onValidate(true, JSON.parse(v))
      } catch {
        onValidate(false, null)
      }
    }
  }
  return (
    <textarea
      rows={rows}
      spellCheck={false}
      readOnly={readOnly}
      placeholder={placeholder}
      value={value}
      onChange={(e) => handle(e.target.value)}
      className={`w-full resize-y rounded-lg border bg-slate-950 p-3 font-mono text-xs leading-relaxed text-slate-100
        placeholder:text-slate-600 focus:outline-none focus:ring-2
        ${invalid ? 'border-red-500 focus:ring-red-500/40' : 'border-slate-700 focus:ring-indigo-500/40'} ${className}`}
    />
  )
}

// ---- Copyable code block ----

export function CodeBlock({ code, label, onCopy }: { code: string; label?: string; onCopy?: () => void }) {
  const [copied, setCopied] = useState(false)
  const copy = () => {
    navigator.clipboard?.writeText(code)
    setCopied(true)
    onCopy?.()
    setTimeout(() => setCopied(false), 1500)
  }
  return (
    <div className="group relative">
      {label && (
        <div className="mb-1 flex items-center justify-between">
          <span className="text-2xs font-medium uppercase tracking-wide text-slate-400">{label}</span>
        </div>
      )}
      <pre className="max-h-64 overflow-auto rounded-lg bg-slate-950 p-3 pr-10 font-mono text-2xs leading-relaxed text-slate-300">
        {code}
      </pre>
      <button
        type="button"
        onClick={copy}
        className={`absolute right-2 ${label ? 'top-6' : 'top-2'} rounded-md p-1.5 text-slate-500 opacity-0 transition-all
          hover:bg-slate-800 hover:text-slate-200 group-hover:opacity-100 ${copied ? 'text-emerald-400 opacity-100' : ''}`}
        title={copied ? 'Copied!' : 'Copy'}
      >
        {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
      </button>
    </div>
  )
}
