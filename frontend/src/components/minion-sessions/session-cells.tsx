import type { ReactNode } from "react"
import { PinIcon } from "lucide-react"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { KindName } from "@/components/modules/module-icon"
import type { ModuleIconSvgs } from "@/components/modules/module-icon-values"
import { platformLabel } from "@/components/builds/build-values"
import { formatAbsolute } from "@/lib/relative-time"
import { ListCell } from "./list-cell"
import { cn } from "@/lib/utils"
import { formatAgo, lateness, shownAddress, type SessionColumnId } from "./sessions-layout"

// How each Sessions column shows a session: one line each, the full value on
// hover where a cell may cut it.

export interface CellContext {
  icons: ReadonlyMap<string, ModuleIconSvgs>
  now: number
  /** The address the filters or the search matched, if any. */
  matchAddress: (addrs: readonly string[]) => string | undefined
  /** Picks a session's shown address; absent where the viewer may not. */
  pickAddress?: (s: MinionSessionFieldsFragment, address: string) => void
}

const muted = (text: string) => <span className="text-muted-foreground">{text}</span>
const none = muted("—")

function text(value: string | null | undefined, className = "truncate"): ReactNode {
  return (
    <div className={className} title={value ?? undefined}>
      {value || none}
    </div>
  )
}

// Integrity levels the User cell names: elevated ones. The usual (user,
// medium, low) go unsaid.
const ELEVATED = new Set(["root", "system", "high"])

/** The user as the OS reports it, with the domain when the minion sent one apart. */
function userOf(s: MinionSessionFieldsFragment): string {
  if (!s.user) return ""
  return s.domain && !s.user.includes("\\") ? `${s.domain}\\${s.user}` : s.user
}

/** Within the last day, how long ago to the second (formatAgo); older, the date and time. */
function Ago({ at, now }: { at: string; now: number }) {
  const ago = formatAgo(now - Date.parse(at))
  return ago === null ? <FormattedDateTimeText date={at} /> : <>{ago}</>
}

/** The schedule and where the session stands by it: "Late: every 60s ± 20%, due 3m ago". */
function scheduleNote(s: MinionSessionFieldsFragment, state: "live" | "late", now: number): string {
  const every = s.sleepInterval != null ? `every ${formatInterval(s.sleepInterval)}${s.sleepJitter ? ` ± ${s.sleepJitter}%` : ""}` : ""
  const due = Date.parse(s.lateAt!) - now
  const when = due > 0 ? `late in ${formatInterval(due)}` : `due ${formatInterval(-due)} ago`
  return `${state === "live" ? "On time" : "Late"}: ${[every, when].filter(Boolean).join(", ")}`
}

/** Milliseconds as the schedule says them: "800ms", "60s", "5m", "1h 30m". */
function formatInterval(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`
  const s = Math.round(ms / 1000)
  if (s < 120) return `${s}s`
  const m = Math.round(s / 60)
  if (m < 120) return `${m}m`
  const h = Math.floor(m / 60)
  return m % 60 ? `${h}h ${m % 60}m` : `${h}h`
}

/** When an external address was last seen, and how many messages came from it. */
function PeerSeen({ lastSeen, count, now }: { lastSeen: string; count: number; now: number }) {
  return (
    <span title={`Last seen ${formatAbsolute(lastSeen)}`}>
      <Ago at={lastSeen} now={now} /> · {count}×
    </span>
  )
}

/** One column's cell for a session. */
export function SessionCell({ id, session: s, ctx }: { id: SessionColumnId; session: MinionSessionFieldsFragment; ctx: CellContext }) {
  switch (id) {
    case "number":
      return (
        <div className="truncate font-mono font-medium" title={`Session ${s.number}, ${s.id}`}>
          {s.number}
        </div>
      )
    case "sessionId":
      return text(s.id, "truncate font-mono text-xs")
    case "addrs":
      return (
        <ListCell
          items={s.addrs}
          shown={shownAddress(s, ctx.matchAddress)}
          noun="addresses"
          onPick={ctx.pickAddress && ((a) => ctx.pickAddress!(s, a))}
        />
      )
    case "peer":
      return (
        <ListCell
          items={s.peers.map((p) => p.address)}
          shown={s.peerAddress ?? null}
          noun="external addresses"
          detail={(address) => {
            const p = s.peers.find((x) => x.address === address)
            return p && <PeerSeen lastSeen={p.lastSeen} count={p.count} now={ctx.now} />
          }}
        />
      )
    case "host":
      return (
        <div className="truncate font-medium" title={s.host ?? undefined}>
          {s.host || muted("unknown host")}
        </div>
      )
    case "user":
      return (
        <div className="truncate" title={[userOf(s), s.integrity && `integrity: ${s.integrity}`].filter(Boolean).join("\n") || undefined}>
          {userOf(s) || none}
          {s.integrity && ELEVATED.has(s.integrity) && <span className="font-medium"> · {s.integrity}</span>}
        </div>
      )
    case "domain":
      return text(s.domain)
    case "proc":
      return (
        <div className="truncate" title={s.proc ?? undefined}>
          {s.proc || muted("pid")} {s.pid}
        </div>
      )
    case "pid":
      return <div className="truncate font-mono">{s.pid}</div>
    case "os":
      return <div className="truncate">{platformLabel(`${s.os}/${s.arch}`)}</div>
    case "arch":
      return text(s.arch, "truncate font-mono")
    case "version":
      return text(s.version, "truncate font-mono")
    case "lastSeen": {
      // Green while on time by its schedule, yellow once late; plain when
      // the minion never said its schedule.
      const state = lateness(s, ctx.now)
      return (
        <div
          className={cn(
            "truncate",
            state === "live" && "text-emerald-600 dark:text-emerald-400",
            state === "late" && "text-amber-600 dark:text-amber-400",
            state === null && "text-muted-foreground",
          )}
          title={[
            state && scheduleNote(s, state, ctx.now),
            s.lastSeenAt && `Last seen ${formatAbsolute(s.lastSeenAt)}`,
            `Opened ${formatAbsolute(s.createdAt)}`,
          ]
            .filter(Boolean)
            .join("\n")}
        >
          {s.lastSeenAt ? <Ago at={s.lastSeenAt} now={ctx.now} /> : "not yet"}
        </div>
      )
    }
    case "opened":
      return (
        <div className="truncate text-muted-foreground" title={`Opened ${formatAbsolute(s.createdAt)}`}>
          <Ago at={s.createdAt} now={ctx.now} />
        </div>
      )
    case "build":
      return (
        <div className="flex min-w-0 items-baseline" title={`Build ${s.buildId}${s.staged ? "" : ", without staging"}`}>
          <KindName type="factory" name={s.moduleName} icon={ctx.icons.get(s.moduleName)} className="font-medium" />
          <span className="truncate text-muted-foreground">&nbsp;· {s.target}</span>
          {s.persisted && (
            <span
              className="ml-1.5 shrink-0 self-center text-muted-foreground"
              title={`Persisted: the build's minions come back to this session when they restart (${s.restartCount} so far)`}
            >
              <PinIcon className="size-3" aria-label="Persisted" />
            </span>
          )}
        </div>
      )
    case "target":
      return text(s.target, "truncate font-mono")
    case "platform":
      return <div className="truncate">{platformLabel(s.platform)}</div>
    case "buildDescription":
      return text(s.buildDescription)
    case "buildId":
      return text(s.buildId, "truncate font-mono text-xs")
    case "staged":
      return <div className="truncate">{s.staged ? "Yes" : muted("No")}</div>
    case "persisted":
      return <div className="truncate">{s.persisted ? "Yes" : muted("No")}</div>
    case "restarts":
      return (
        <div
          className="truncate font-mono"
          title={s.restarts.map((r) => `${formatAbsolute(r.at)}: pid ${r.pid}${r.host ? ` on ${r.host}` : ""}`).join("\n") || undefined}
        >
          {s.restartCount > 0 ? s.restartCount : muted("0")}
        </div>
      )
  }
}
