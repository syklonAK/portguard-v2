import type { ReactNode, ButtonHTMLAttributes, InputHTMLAttributes, SelectHTMLAttributes } from 'react'
import { useState } from 'react'
import { createPortal } from 'react-dom'
import { X, Check, Copy } from 'lucide-react'

export function Button({
  variant = 'primary',
  size = 'md',
  className = '',
  children,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'primary' | 'secondary' | 'outline' | 'danger' | 'ghost' | 'success'
  size?: 'sm' | 'md'
}) {
  // PasarGuard button language: rounded-lg, sm:h-9, active:scale-[0.98],
  // focus ring on --ring, primary = steel blue
  const base =
    'pg-press inline-flex cursor-pointer items-center justify-center gap-1.5 whitespace-nowrap rounded-lg text-sm font-medium transition-[background-color,box-shadow,transform] duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 active:scale-[0.98]'
  const sizes = { sm: 'h-9 rounded-md px-3 [&>svg]:h-4 [&>svg]:w-4', md: 'h-10 px-4 py-2 [&>svg]:h-4.5 [&>svg]:w-4.5' }
  const variants = {
    primary: 'bg-primary text-primary-foreground shadow-sm hover:bg-primary/90',
    secondary: 'bg-secondary text-secondary-foreground hover:bg-secondary/80',
    outline: 'border border-border bg-background hover:bg-accent hover:text-accent-foreground dark:border-input dark:hover:bg-accent/60',
    danger: 'bg-destructive text-destructive-foreground shadow-sm hover:bg-destructive/90',
    success: 'bg-success text-success-foreground shadow-sm hover:bg-success/90',
    ghost: 'hover:bg-accent hover:text-accent-foreground',
  }
  return (
    <button className={`${base} ${sizes[size]} ${variants[variant]} ${className}`} {...props}>
      {children}
    </button>
  )
}

export function Card({ children, className = '' }: { children: ReactNode; className?: string }) {
  return (
    <div className={`rounded-xl border border-border/60 bg-card text-card-foreground shadow-sm ${className}`}>
      {children}
    </div>
  )
}

export function CardHeader({ title, desc, right }: { title: string; desc?: string; right?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-3 border-b border-border/60 px-6 py-5">
      <div className="min-w-0">
        <h3 className="text-sm font-semibold tracking-tight">{title}</h3>
        {desc && <p className="mt-0.5 text-sm text-muted-foreground">{desc}</p>}
      </div>
      {right && <div className="flex w-full flex-wrap items-center gap-2 sm:w-auto">{right}</div>}
    </div>
  )
}

export function Input({ className = '', ...props }: InputHTMLAttributes<HTMLInputElement>) {
  // PasarGuard input: h-9, bg-input surface, visible border, ring focus
  return (
    <input
      className={`flex h-9 w-full rounded-lg border border-border bg-input px-3 py-2 text-sm transition-colors
        placeholder:text-input-placeholder
        focus-visible:border-primary/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30
        disabled:cursor-not-allowed disabled:opacity-50 ${className}`}
      {...props}
    />
  )
}

export function Select({ className = '', children, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={`flex h-9 w-full rounded-lg border border-border bg-input px-2.5 py-2 text-sm transition-colors
        focus-visible:border-primary/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/30
        disabled:cursor-not-allowed disabled:opacity-50 ${className}`}
      {...props}
    >
      {children}
    </select>
  )
}

export function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: string }) {
  return (
    <label className="block">
      <span className="mb-1 block text-sm font-medium text-foreground">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-2xs text-muted-foreground">{hint}</span>}
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
  // PasarGuard badge variants: soft tinted chips with explicit borders
  const colors = {
    slate: 'border-transparent bg-muted text-muted-foreground',
    green: 'border-green-300 bg-green-100 text-green-800 dark:border-green-700 dark:bg-green-900 dark:text-green-300',
    red: 'border-red-300 bg-red-100 text-red-800 dark:border-red-700 dark:bg-red-900 dark:text-red-300',
    amber: 'border-yellow-300 bg-yellow-100 text-yellow-800 dark:border-yellow-700 dark:bg-yellow-900 dark:text-yellow-300',
    blue: 'border-blue-300 bg-blue-100 text-blue-800 dark:border-blue-700 dark:bg-blue-900 dark:text-blue-300',
    purple: 'border-violet-300 bg-violet-100 text-violet-800 dark:border-violet-700 dark:bg-violet-900 dark:text-violet-300',
    cyan: 'border-cyan-300 bg-cyan-100 text-cyan-800 dark:border-cyan-700 dark:bg-cyan-900 dark:text-cyan-300',
  }
  return (
    <span className={`inline-flex items-center gap-1 rounded-md border px-2.5 py-0 text-xs font-normal transition-colors ${colors[color]}`}>
      {children}
    </span>
  )
}

export function Modal({
  open,
  onClose,
  title,
  children,
  footer,
  wide = false,
}: {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
  footer?: ReactNode
  wide?: boolean
}) {
  if (!open) return null
  // PasarGuard dialog: centered fixed card over black/50 + backdrop-blur-sm.
  // Portal to <body> so page animations can't trap the fixed positioning.
  // The card is clamped to the viewport and only its body scrolls between
  // the pinned header and footer; pg-modal-* hooks compact it on short screens.
  return createPortal(
    <div className="pg-modal-bg fixed inset-0 z-50">
      <div className="absolute inset-0 bg-black/50 backdrop-blur-sm" onClick={onClose} />
      <div className="pg-modal-wrap short:items-stretch relative flex h-full items-center justify-center p-4 sm:p-6">
        <div
          className={`pg-modal-card short:max-w-5xl short:rounded-xl flex max-h-full w-full ${wide ? 'max-w-3xl' : 'max-w-lg'} flex-col rounded-xl border
            border-border bg-background shadow-lg`}
        >
          <div className="pg-modal-header flex shrink-0 items-center justify-between border-b border-border px-6 py-4 short:py-2">
            <h2 className="truncate text-lg leading-tight font-semibold tracking-tight short:text-sm">{title}</h2>
            <button onClick={onClose} className="pg-press shrink-0 cursor-pointer rounded-sm p-1 text-muted-foreground opacity-70 transition-opacity hover:opacity-100 hover:text-foreground">
              <X className="h-4 w-4" />
            </button>
          </div>
          <div className="pg-modal-body min-h-0 flex-1 overflow-y-auto p-6">{children}</div>
          {footer && (
            <div className="pg-modal-footer shrink-0 rounded-b-xl border-t border-border bg-card px-6 py-4">
              {footer}
            </div>
          )}
        </div>
      </div>
    </div>,
    document.body,
  )
}

export function Spinner({ className = 'h-5 w-5' }: { className?: string }) {
  return (
    <svg className={`animate-spin text-muted-foreground ${className}`} viewBox="0 0 24 24" fill="none">
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
        : 'bg-neutral-400'
  return <span className={`inline-block h-2 w-2 rounded-full ${cls} ${status === 'up' ? 'animate-pulse' : ''}`} />
}

export function Empty({ message }: { message: string }) {
  return <div className="px-6 py-12 text-center text-sm text-muted-foreground short:py-8 short:text-xs">{message}</div>
}

export function Toggle({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  // PasarGuard switch: h-5 w-9, checked = bg-primary
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className={`pg-press inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full border-2 border-transparent shadow-sm transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none ${
        checked ? 'bg-primary' : 'bg-input'
      }`}
    >
      <span
        className={`pointer-events-none block h-4 w-4 rounded-full bg-background shadow transition-transform ${
          checked ? 'translate-x-4' : 'translate-x-0'
        }`}
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
  // PasarGuard TabsList: muted pill container, active tab lifts with bg-background
  return (
    <div className="flex gap-1 rounded-lg bg-muted p-1 text-muted-foreground short:rounded-md short:p-0.5">
      {tabs.map((t) => {
        const Icon = t.icon
        return (
          <button
            key={t.id}
            type="button"
            onClick={() => onChange(t.id)}
            className={`pg-press flex flex-1 items-center justify-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium whitespace-nowrap transition-all short:py-1 short:text-xs ${
              active === t.id
                ? 'bg-background text-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
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
      className={`w-full resize-y rounded-lg border bg-popover p-3 font-mono text-xs leading-relaxed text-foreground
        placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2
        ${invalid ? 'border-destructive focus:ring-destructive/40' : 'border-border focus:ring-ring/30'} ${className}`}
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
          <span className="text-2xs font-medium tracking-wide text-muted-foreground uppercase">{label}</span>
        </div>
      )}
      <pre className="max-h-64 overflow-auto rounded-lg bg-popover p-3 pr-10 font-mono text-2xs leading-relaxed text-foreground/90">
        {code}
      </pre>
      <button
        type="button"
        onClick={copy}
        className={`pg-press absolute right-2 ${label ? 'top-6' : 'top-2'} cursor-pointer rounded-md p-1.5 text-muted-foreground opacity-0 transition-all
          hover:text-foreground group-hover:opacity-100 ${copied ? 'text-emerald-500 opacity-100' : ''}`}
        title={copied ? 'Copied!' : 'Copy'}
      >
        {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
      </button>
    </div>
  )
}
