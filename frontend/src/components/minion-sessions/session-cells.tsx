import type { ReactNode } from "react"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { KindName } from "@/components/modules/module-icon"
import type { ModuleIconSvgs } from "@/components/modules/module-icon-values"
import { platformLabel } from "@/components/builds/build-values"
import { formatAbsolute, relativeTime } from "@/lib/relative-time"
import { ListCell } from "./list-cell"
import { shownAddress, type SessionColumnId } from "./sessions-layout"

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

const DAY = 24 * 60 * 60 * 1000
const HOUR = 60 * 60 * 1000

/** Within the last day: how long ago ("just now", "5m ago", "3h ago"); older, the date and time. */
function Ago({ at, now }: { at: string; now: number }) {
  const diff = now - Date.parse(at)
  if (diff >= DAY) return <FormattedDateTimeText date={at} />
  return <>{diff < HOUR ? relativeTime(at, new Date(now)) : `${Math.floor(diff / HOUR)}h ago`}</>
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
        <div className="truncate" title={userOf(s) || undefined}>
          {userOf(s) || none}
          {s.integrity && ELEVATED.has(s.integrity) && <span className="font-medium"> · {s.integrity}</span>}
        </div>
      )
    case "domain":
      return text(s.domain)
    case "integrity":
      return text(s.integrity)
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
    case "lastSeen":
      return (
        <div
          className="truncate text-muted-foreground"
          title={[s.lastSeenAt && `Last seen ${formatAbsolute(s.lastSeenAt)}`, `Opened ${formatAbsolute(s.createdAt)}`]
            .filter(Boolean)
            .join("\n")}
        >
          {s.lastSeenAt ? <Ago at={s.lastSeenAt} now={ctx.now} /> : "not yet"}
        </div>
      )
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
  }
}
