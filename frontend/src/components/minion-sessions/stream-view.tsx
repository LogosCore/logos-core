import { useState } from "react"
import { ArrowDownToLineIcon, CheckIcon, CopyIcon, DownloadIcon, LoaderIcon, PauseIcon, TerminalIcon, XIcon } from "lucide-react"
import { toast } from "sonner"
import { fileTypeName } from "@/components/file-preview/type-name"
import { errorMessage } from "@/components/modules/management/api-errors"
import { Button } from "@/components/ui/button"
import { InfoCard, LabelRow } from "@/components/ui/info-card"
import type { MinionTaskFieldsFragment } from "@/graphql/gql/graphql"
import { fetchMinionStreamLink } from "@/graphql/hooks/minion-tasks"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { formatDateTime } from "@/lib/date-format"
import { formatBytes } from "@/lib/format-bytes"
import {
  FileOverviewBody,
  FileOverviewCards,
  FileProgress,
  FileStrip,
  ShaValue,
  SizeValue,
} from "./file-overview-parts"
import { rateText } from "./file-overview-values"
import { savedName, shellQuote } from "./verify-commands"
import { VerifyHashCard } from "./verify-hash-card"

// A stream_file task's stream (docs: contracts/task-results-and-actions.md,
// file_stream), laid out as a kept file's Overview (file-view.tsx): the
// file's name in the strip above; then Download (the browser takes the
// stream, resumable) and Copy curl (a signed link, which resumes when run
// again) while it is open, and how far the downloader is; then its details
// and, once the minion has read it whole, the commands that check the
// download. Live: it follows the task's updates as the downloader goes on.

const API_URL = import.meta.env.VITE_API_URL as string

type Task = MinionTaskFieldsFragment
type Stream = NonNullable<Task["stream"]>

const STATE: Record<string, { label: string; className: string }> = {
  WAITING: { label: "Waiting for a downloader", className: "text-muted-foreground" },
  STREAMING: { label: "Streaming", className: "text-sky-600 dark:text-sky-400" },
  PAUSED: { label: "Paused: no downloader", className: "text-amber-600 dark:text-amber-400" },
  DONE: { label: "Downloaded", className: "text-emerald-600 dark:text-emerald-400" },
  FAILED: { label: "Failed", className: "text-red-600 dark:text-red-400" },
  CANCELLED: { label: "Stopped", className: "text-muted-foreground" },
}

/** A path's last part, by either separator. */
function baseName(path: string): string {
  return path.slice(Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\")) + 1) || path
}

/** The downloader's rate since it took the stream, bytes a second. */
function rate(st: Stream): number | null {
  if (!st.attachedAt || !st.readerAt) return null
  const secs = (Date.parse(st.readerAt) - Date.parse(st.attachedAt)) / 1000
  return secs > 0 && st.delivered > st.from ? (st.delivered - st.from) / secs : null
}


export function StreamView({ task: t, stream: st }: { task: Task; stream: Stream }) {
  const state = STATE[st.state] ?? { label: st.state.toLowerCase(), className: "text-muted-foreground" }
  const open = st.state === "WAITING" || st.state === "STREAMING" || st.state === "PAUSED"
  const moving = st.state === "STREAMING" || st.state === "PAUSED"
  const pct = st.size > 0 ? Math.min(100, Math.floor((st.delivered / st.size) * 100)) : 100
  const bps = st.state === "STREAMING" ? rate(st) : null
  const name = baseName(st.name)
  // What core's download saves it as: curl's -o, and what the checks name.
  const saved = savedName(st.name, `stream-${t.id.slice(0, 8)}`)
  const typeName = fileTypeName("", name)
  const url = `${API_URL}/minion-streams/${encodeURIComponent(t.id)}`
  const [copying, setCopying] = useState(false)
  const Icon =
    st.state === "DONE" ? CheckIcon : st.state === "PAUSED" ? PauseIcon : st.state === "STREAMING" ? LoaderIcon : st.state === "WAITING" ? ArrowDownToLineIcon : XIcon
  const sha256 = st.sha256 ?? null

  const copyCurl = async () => {
    setCopying(true)
    try {
      const path = await fetchMinionStreamLink(t.id)
      const link = new URL(`${API_URL}${path}`, window.location.origin).toString()
      await copyToClipboard(`curl -f -C - -o ${shellQuote(saved)} ${shellQuote(link)}`, "curl command")
    } catch (err) {
      toast.error(errorMessage(err, "Could not make a link"))
    } finally {
      setCopying(false)
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <FileStrip name={st.name} size={st.size} />
      <FileOverviewBody>
        {(open || sha256) && (
          <div className="flex flex-wrap items-center gap-2">
            {open && (
              <>
                <Button nativeButton={false} render={<a href={url} download />} title="Download it in this browser: it comes as the minion sends it, and resumes">
                  <DownloadIcon />
                  Download
                </Button>
                <Button variant="outline" disabled={copying} onClick={() => void copyCurl()}
                  title="A curl command with a signed link: it resumes when run again (24 hours at most)">
                  <TerminalIcon />
                  Copy curl
                </Button>
              </>
            )}
            {sha256 && (
              <Button variant="outline" onClick={() => void copyToClipboard(sha256, "SHA-256")}>
                <CopyIcon />
                Copy SHA-256
              </Button>
            )}
          </div>
        )}
        {st.state !== "DONE" && (
          <FileProgress
            label={state.label}
            className={state.className}
            icon={Icon}
            spin={st.state === "STREAMING"}
            detail={moving ? `${formatBytes(st.delivered)} of ${formatBytes(st.size)} (${pct}%)${rateText(bps, st.size - st.delivered)}` : null}
            pct={moving ? pct : null}
            barClassName={st.state === "PAUSED" ? "bg-amber-500" : "bg-sky-500"}
            problem={st.state === "FAILED" ? t.err : null}
          >
            {st.state === "WAITING" && (
              <p className="text-xs text-muted-foreground">
                The minion holds the file open: it streams as it is downloaded
                {t.file ? ", and core keeps a copy as it passes" : ". Core keeps no copy: the file is over its size cap"}. With
                no downloader for an hour, the stream stops.
              </p>
            )}
          </FileProgress>
        )}

        <FileOverviewCards>
          <InfoCard labelWidth="w-24">
            <LabelRow label="Path">
              <span className="font-mono text-xs break-all">{st.name}</span>
            </LabelRow>
            <LabelRow label="Size">
              <SizeValue size={st.size} />
            </LabelRow>
            <LabelRow label="Type">
              {typeName ? <span>{typeName}</span> : <span className="text-muted-foreground">by its name, not known</span>}
            </LabelRow>
            <LabelRow label="SHA-256">
              <ShaValue sha256={sha256} pending="once the minion has read it whole, in order" />
            </LabelRow>
            <LabelRow label="State">
              <span className={state.className}>{state.label}</span>
              {moving && <span className="text-muted-foreground tabular-nums"> · {pct}%</span>}
              {t.doneAt && <span className="text-muted-foreground tabular-nums"> · {formatDateTime(t.doneAt)}</span>}
            </LabelRow>
            <LabelRow label="Announced">
              <span className="tabular-nums">{formatDateTime(st.announcedAt)}</span>
            </LabelRow>
          </InfoCard>
          {sha256 && <VerifyHashCard name={saved} sha256={sha256} />}
        </FileOverviewCards>
      </FileOverviewBody>
    </div>
  )
}
