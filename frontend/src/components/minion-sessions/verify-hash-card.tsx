import { CopyIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { InfoCard, LabelRow } from "@/components/ui/info-card"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { verifyCommands } from "./verify-commands"

// Commands that check a downloaded file against its SHA-256, one for each
// system, ready to copy and run where the file was saved.

export function VerifyHashCard({ name, sha256 }: { name: string; sha256: string }) {
  return (
    <InfoCard title="Verify the download" note="run where the file was saved" labelWidth="w-24">
      {verifyCommands(name, sha256).map((c) => (
        <LabelRow key={c.label} label={c.label}>
          {/* Whole, wrapped: a command is read before it is run. */}
          <span className="flex min-w-0 items-start gap-1">
            <code className="min-w-0 flex-1 font-mono text-xs break-all">{c.command}</code>
            <Button variant="ghost" size="icon-xs" className="-my-0.5 shrink-0" title={`Copy the ${c.label} command`} aria-label={`Copy the ${c.label} command`}
              onClick={() => void copyToClipboard(c.command, `${c.label} command`)}>
              <CopyIcon />
            </Button>
          </span>
        </LabelRow>
      ))}
    </InfoCard>
  )
}
