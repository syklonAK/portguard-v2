import { KeyRound } from 'lucide-react'
import { Button, CodeBlock, Field, Input, Modal } from '../ui'

/** Deploy-a-new-node modal: the page mints AND saves the token before
 *  opening this — the one-liner's download authenticates with it. */
export default function DeployModal({ open, onClose, origin, token, onRegenerate }: {
  open: boolean
  onClose: () => void
  origin: string
  token: string
  onRegenerate: () => void
}) {
  return (
      <Modal open={open} onClose={onClose} wide
        title="Deploy a new node — no panel install needed">
        <div className="space-y-4">
          <p className="text-xs leading-relaxed text-muted-foreground">
            Run this single command on the new Ubuntu server — that's the whole flow. It downloads
            <b> this master's own binary</b>, installs it as a headless <b>portguard-agent</b> service
            (no account, no panel setup) and <b>self-registers</b> with this master. When the command
            finishes, the node appears in the list below as <b>online</b> and is fully managed from here.
          </p>
          <Field label="1. Node token (generated — copy it)">
            <div className="flex gap-2">
              <Input readOnly className="font-mono" value={token} />
              <Button
                variant="secondary"
                onClick={onRegenerate}
                title="Regenerate & save"
              >
                <KeyRound className="h-4 w-4" />
              </Button>
            </div>
          </Field>
          <Field label="2. Pick the node role">
            <div className="text-2xs leading-relaxed text-muted-foreground">
              Pass <code className="rounded bg-mutedslate px-1">-role iran</code> or
              <code className="mx-1 rounded bg-mutedslate px-1">-role foreign</code> for tunnel
              servers; default is <code className="rounded bg-mutedslate px-1">generic</code>.
            </div>
          </Field>
          <Field label="3. One-liner (run as root on the new server)">
            <CodeBlock
              label=""
              code={`sudo bash -c "$(curl -fsSL ${origin}/api/agent-install.sh)" -- -master ${origin} -token ${token}`}
            />
          </Field>
          <div className="rounded-xl border border-sky-200 bg-sky-50 p-3 text-2xs leading-relaxed text-sky-700 dark:border-sky-500/30 dark:bg-sky-500/10 dark:text-sky-300">
            The token above was <b>already saved</b> to this panel — the installer's binary download and
            self-registration authenticate with it. No inbound port is opened on the node (reverse mode);
            give it a friendly name with <code className="mx-1">-name mynode</code>. Re-running the command
            on the same server just refreshes it.
          </div>
        </div>
      </Modal>
  )
}
