import type { ReactNode } from "react"
import {
  CableIcon,
  CircleAlertIcon,
  PhoneIncomingIcon,
  PinIcon,
  PlugIcon,
  RefreshCwIcon,
  TriangleAlertIcon,
  UnplugIcon,
  VenetianMaskIcon,
  type LucideIcon,
} from "lucide-react"
import { kindLabels } from "@/components/endpoints/endpoint-values"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { KindName } from "@/components/modules/module-icon"
import type { ModuleIconSvgs } from "@/components/modules/module-icon-values"
import { platformLabel } from "@/components/builds/build-values"
import { formatAbsolute } from "@/lib/relative-time"
import { ListCell } from "./list-cell"
import { MarkIcon } from "./mark-icon"
import { OsIcon } from "./os-icon"
import { MARK_TONE_CLASSES, isMarkColor } from "./session-mark-colors"
import { cn } from "@/lib/utils"
import { useRecordModals } from "@/hooks/use-record-modals"
import { FEEDBACK_TONE, feedbackSummary } from "./feedback-values"
import { ELEVATED, identityLabel, identityUser, shortUser } from "./identity-values"
import { formatAgo, formatAgoExact, formatInterval, lateness, shownAddress, type SessionColumnId } from "./sessions-layout"

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
function scheduleNote(s: MinionSessionFieldsFragment, state: "live" | "late" | "lost", now: number): string {
  const every = s.sleepInterval != null ? `every ${formatInterval(s.sleepInterval)}${s.sleepJitter ? ` ± ${s.sleepJitter}%` : ""}` : ""
  const due = Date.parse(s.lateAt!) - now
  const when = due > 0 ? `late in ${formatInterval(due)}` : `due ${formatInterval(-due)} ago`
  const label = { live: "On time", late: "Late", lost: "Lost, 3 check-ins missed" }[state]
  return `${label}: ${[every, when].filter(Boolean).join(", ")}`
}


/**
 * What each connection kind shows before Last seen, in the cell's color:
 * polling repeats, a persistent connection stays plugged in; the bind kinds,
 * where core reaches the minion, a call coming in and a plug core opens:
 * plugged in while connected, unplugged while not (the session menu's
 * Connect and Disconnect use the same two).
 */
const CONN_KIND_ICONS: Record<string, LucideIcon> = {
  "reverse-call": RefreshCwIcon,
  "reverse-persistent": CableIcon,
  "bind-call": PhoneIncomingIcon,
  "bind-persistent": PlugIcon,
  "bind-persistent:off": UnplugIcon,
}

/** The kind's key in CONN_KIND_ICONS: a bind-persistent one follows its connection. */
function connKindKey(s: MinionSessionFieldsFragment): string {
  return s.connKind === "bind-persistent" && !s.connected ? "bind-persistent:off" : (s.connKind ?? "")
}

/** The connection a session uses, for Last seen's hover: "Connection 2 of 2: ws · reverse-persistent (…)". */
function connNote(s: MinionSessionFieldsFragment, now: number): string[] {
  if (!s.conn) return []
  const kind = s.connKind ? ` · ${s.connKind}${kindLabels[s.connKind] ? ` (${kindLabels[s.connKind]})` : ""}` : ""
  const lines = [`Connection ${s.conn} of ${s.connCount}: ${s.connName ?? "?"}${kind}`]
  const last = s.connChanges[0]
  if (last) {
    const ago = formatAgo(now - Date.parse(last.at)) ?? formatAbsolute(last.at)
    lines.push(`Moved ${last.from ?? "?"} → ${last.to}${last.reason ? ` · ${last.reason}` : ""} · ${ago}`)
  }
  return lines
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
        <div className="flex min-w-0 items-baseline gap-1.5" title={`Session ${s.number}, ${s.id}`}>
          <span className="truncate font-mono font-medium">{s.number}</span>
          {/* The caller's mark, in its color's tone (the row sets it); two
              icons at most: with errors and warnings both, theirs. */}
          {(s.mark?.icon || s.mark?.emoji) && !(s.feedbackErrors > 0 && s.feedbackWarnings > 0) && (
            <span data-mark-icon className={cn("self-center", isMarkColor(s.mark.color) && MARK_TONE_CLASSES)}>
              <MarkIcon icon={s.mark.icon} emoji={s.mark.emoji} size={14} className="block" />
            </span>
          )}
          <FeedbackFlag s={s} />
        </div>
      )
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
        <div className="truncate font-medium" title={s.host || undefined}>
          {s.host || muted("unknown host")}
        </div>
      )
    case "user":
      return (
        <div
          className="truncate"
          title={
            [
              userOf(s) && `Process: ${userOf(s)}${s.integrity ? `, integrity ${s.integrity}` : ""}`,
              s.acting && `Tasks run as: ${identityLabel(s.acting)}${s.acting.integrity ? `, integrity ${s.acting.integrity}` : ""}`,
            ]
              .filter(Boolean)
              .join("\n") || undefined
          }
        >
          {s.acting ? (
            // Its tasks run as another: that one, behind a mark it changed;
            // who the process is stays in the title.
            <>
              <VenetianMaskIcon className="mr-1 inline size-3.5 align-[-2px] text-muted-foreground" aria-label="Identity changed" />
              <span className="font-medium">{shortUser(identityUser(s.acting), s.host)}</span>
              {s.acting.integrity && ELEVATED.has(s.acting.integrity) && <span className="font-medium"> · {s.acting.integrity}</span>}
            </>
          ) : (
            <>
              {userOf(s) ? shortUser(userOf(s), s.host) : none}
              {s.integrity && ELEVATED.has(s.integrity) && <span className="font-medium"> · {s.integrity}</span>}
            </>
          )}
        </div>
      )
    case "domain":
      return text(s.domain)
    case "proc":
      // The name alone: the PID has a column of its own.
      return text(s.proc)
    case "pid":
      // 0: no process of its own.
      return s.pid === 0 ? <div className="truncate">{muted("—")}</div> : <div className="truncate font-mono">{s.pid}</div>
    case "os":
      // Its mark, then the arch; the name in the title.
      return (
        <div className="flex min-w-0 items-center gap-1.5" title={platformLabel(`${s.os}/${s.arch}`)}>
          <OsIcon os={s.os} size={18} />
          <span className="truncate font-mono text-xs text-muted-foreground">{s.arch}</span>
        </div>
      )
    case "lastSeen": {
      // Green while on time by its schedule, yellow once late, red once
      // lost; plain when the minion never said its schedule.
      // A persistent connection open: the minion is there now.
      const state = s.connected ? "live" : lateness(s, ctx.now)
      const KindIcon = CONN_KIND_ICONS[connKindKey(s)]
      return (
        <div
          // A marked row's style recolors these (session-mark-styles.css).
          data-seen={state ?? undefined}
          className={cn(
            "flex min-w-0 items-baseline gap-1",
            state === "live" && "text-emerald-600 dark:text-emerald-400",
            state === "late" && "text-amber-600 dark:text-amber-400",
            state === "lost" && "text-red-600 dark:text-red-400",
            state === null && "text-muted-foreground",
          )}
          title={[
            s.connected ? "Connected: a connection to the minion is open, and core sends to it at once" : state && scheduleNote(s, state, ctx.now),
            s.lastSeenAt &&
              `Last seen ${formatAbsolute(s.lastSeenAt)} (${formatAgoExact(ctx.now - Date.parse(s.lastSeenAt))})`,
            ...connNote(s, ctx.now),
            `Opened ${formatAbsolute(s.createdAt)}`,
          ]
            .filter(Boolean)
            .join("\n")}
        >
          {KindIcon && <KindIcon className="size-3.5 shrink-0 self-center" aria-label={s.connKind ?? undefined} />}
          <span className="truncate">
            {s.connected ? "connected" : s.lastSeenAt ? <Ago at={s.lastSeenAt} now={ctx.now} /> : "not yet"}
          </span>
        </div>
      )
    }
    case "build":
      return (
        <div className="flex min-w-0 items-baseline" title={buildTitle(s)}>
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
    case "description":
      return text(s.description)
  }
}

/** The Build cell's title: the build, and what the columns it took the place of showed. */
function buildTitle(s: MinionSessionFieldsFragment): string {
  return [
    `Build ${s.buildId}`,
    s.buildDescription && `Description: ${s.buildDescription}`,
    `Target: ${s.target}`,
    `Platform: ${platformLabel(s.platform)}`,
    s.version && `Minion version: ${s.version}`,
    s.staged ? "Staged" : "Not staged: the session keeps the build's keys",
    s.persisted && `Persisted (${s.restartCount} ${s.restartCount === 1 ? "restart" : "restarts"})`,
  ]
    .filter(Boolean)
    .join("\n")
}

/**
 * A session's unread feedback beside its number, only when there is some: an
 * error's icon if any is an error, a warning's if any is a warning (how many
 * in their title); each opens the session's Feedback.
 */
function FeedbackFlag({ s }: { s: MinionSessionFieldsFragment }) {
  const { open } = useRecordModals()
  if (s.feedbackErrors + s.feedbackWarnings === 0) return null
  const id = s.id
  const summary = feedbackSummary(s.feedbackErrors, s.feedbackWarnings)
  const levels = [...(s.feedbackErrors > 0 ? (["error"] as const) : []), ...(s.feedbackWarnings > 0 ? (["warning"] as const) : [])]
  return (
    <span className="inline-flex shrink-0 items-center self-center">
      {levels.map((level) => {
        const Icon = level === "error" ? CircleAlertIcon : TriangleAlertIcon
        return (
          <button
            key={level}
            type="button"
            className={cn("inline-flex cursor-pointer items-center rounded p-0.5 hover:bg-muted", FEEDBACK_TONE[level])}
            title={`Unread feedback: ${summary}. Open it`}
            aria-label={`Unread feedback: ${summary}`}
            onClick={(e) => {
              e.stopPropagation()
              open({ kind: "session", id, tab: "feedback" })
            }}
          >
            <Icon className="size-3.5" aria-hidden />
          </button>
        )
      })}
    </span>
  )
}

