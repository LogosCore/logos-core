import { useEffect, useMemo, type ReactNode } from "react"
import { MonitorDotIcon, SquareTerminalIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { CardColumns } from "@/components/ui/card-columns"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { IdRows, InfoCard, LabelRow, NoteRow } from "@/components/ui/info-card"
import { LazyJsonEditor, preloadJsonEditor } from "@/components/ui/lazy-json-editor"
import {
  RECORD_TABS_LIST,
  RECORD_TABS_TRIGGER,
  RecordModal,
  RecordModalBody,
  RecordModalHeader,
  RecordModalMessage,
  RecordModalTitle,
} from "@/components/ui/record-modal"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { platformLabel } from "@/components/builds/build-values"
import { KindName, ModuleIcon } from "@/components/modules/module-icon"
import { MarkIcon } from "./mark-icon"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { useFactoryIcons } from "@/graphql/hooks/builds"
import { useMinionSession, useMinionSessionConnections } from "@/graphql/hooks/minion-sessions"
import { useNow } from "@/hooks/use-now"
import { useRecordModals } from "@/hooks/use-record-modals"
import { formatJson } from "@/lib/json-edit"
import { whenIdle } from "@/lib/preloadable"
import type { RecordModal as RecordModalTarget, SessionTab } from "@/lib/record-modals"
import { formatAbsolute } from "@/lib/relative-time"
import { cn } from "@/lib/utils"
import { formatAgo, formatAgoExact, formatInterval, lateness } from "./sessions-layout"
import { ConnectionsCard } from "./session-connections"
import { MaxTasksValue } from "./max-tasks"
import { FeedbackCounts } from "./feedback-parts"
import { FeedbackView } from "./feedback-view"
import { IdentityView } from "./identity-view"
import { useInteractWithSession } from "./use-interact"
import { identityLabel } from "./identity-values"

// One minion session (?session=<id>[&sessionTab=]), over any page, laid out
// as a build's details are: its header says which session it is (its number,
// mark and description) and how it stands (where it runs, live, late or
// lost, last seen), from every tab, and Interact, which takes it to the
// Sessions page's work pane; the tabs under it: Overview, what it is
// and where it runs, in cards; Connections, how its minion reaches core;
// Feedback; Identity; JSON, its record as the API gives it. Live where the
// page keeps sessions live (the Sessions page): the list's updates reach the
// session's own query too.

type Session = MinionSessionFieldsFragment

export function SessionModal({ modal }: { modal: Extract<RecordModalTarget, { kind: "session" }> }) {
  const { close } = useRecordModals()
  return (
    <RecordModal onClose={close}>
      <SessionDetails key={modal.id} id={modal.id} tab={modal.tab ?? "overview"} />
    </RecordModal>
  )
}

function SessionDetails({ id, tab }: { id: string; tab: SessionTab }) {
  const { data, isLoading, error } = useMinionSession(id)
  const session = data?.minionSession ?? null
  // Empty while loading, as the tables are.
  if (isLoading) return null
  if (error || !session) {
    return (
      <RecordModalMessage
        icon={<MonitorDotIcon className="size-8 opacity-50" />}
        title="Session"
        text={error ? error.message : "This session does not exist."}
      />
    )
  }
  return <SessionDetail s={session} tab={tab} />
}

function SessionDetail({ s, tab }: { s: Session; tab: SessionTab }) {
  const { open } = useRecordModals()
  const icons = useFactoryIcons()
  const json = useMemo(() => formatJson(s), [s])
  const connections = useMinionSessionConnections(s.id).data?.minionSession?.connections.length ?? 0
  const interact = useInteractWithSession()
  // Fetched once the modal has settled, so the JSON tab opens at once.
  useEffect(() => whenIdle(preloadJsonEditor), [])
  return (
    <Tabs
      value={tab}
      onValueChange={(v) => {
        if (v === "overview" || v === "connections" || v === "feedback" || v === "identity" || v === "json") {
          open({ kind: "session", id: s.id, tab: v })
        }
      }}
      className="min-h-0 flex-1 gap-0"
    >
      <RecordModalHeader
        below={
          <TabsList className={RECORD_TABS_LIST}>
            <TabsTrigger value="overview" className={RECORD_TABS_TRIGGER}>
              Overview
            </TabsTrigger>
            <TabsTrigger value="connections" className={RECORD_TABS_TRIGGER}>
              Connections
              {connections > 0 && <span className="text-xs text-muted-foreground tabular-nums">{connections}</span>}
            </TabsTrigger>
            <TabsTrigger value="feedback" className={RECORD_TABS_TRIGGER}>
              Feedback
              <FeedbackCounts errors={s.feedbackErrors} warnings={s.feedbackWarnings} className="text-xs" />
            </TabsTrigger>
            <TabsTrigger value="identity" className={RECORD_TABS_TRIGGER}>
              Identity
            </TabsTrigger>
            <TabsTrigger value="json" className={RECORD_TABS_TRIGGER}>
              JSON
            </TabsTrigger>
          </TabsList>
        }
      >
        {/* The factory's icon, beside the session's number, mark and
            description and, under them, where it runs and how it stands. */}
        <div className="flex min-w-0 flex-1 items-center gap-3">
          <ModuleIcon type="factory" icon={icons.get(s.moduleName)} className="size-9" />
          <div className="flex min-w-0 flex-col">
            <RecordModalTitle>
              <span className="flex min-w-0 items-center gap-2">
                <span className="shrink-0">{s.state === "RESERVED" ? "Reserved session" : `Session ${s.number}`}</span>
                {(s.mark?.icon || s.mark?.emoji) && <MarkIcon icon={s.mark.icon} emoji={s.mark.emoji} size={18} />}
                {s.description && (
                  <span className="truncate font-normal" title={s.description}>
                    · {s.description}
                  </span>
                )}
              </span>
            </RecordModalTitle>
            <SessionHeadline s={s} />
          </div>
        </div>
        <Button onClick={() => interact(s)}>
          <SquareTerminalIcon />
          Interact
        </Button>
      </RecordModalHeader>

      <TabsContent value="overview" className="flex min-h-0 flex-col">
        <RecordModalBody>
          <CardColumns className="p-4">
            <SessionCard s={s} />
            <HostCard s={s} />
            <BuildCard s={s} onOpenBuild={() => open({ kind: "build", id: s.buildId })} icons={icons} />
            <AddressesCard s={s} />
            {s.restarts.length > 0 && <RestartsCard s={s} />}
            {extras(s).length > 0 && <ExtrasCard s={s} />}
          </CardColumns>
        </RecordModalBody>
      </TabsContent>

      <TabsContent value="connections" className="flex min-h-0 flex-col">
        <RecordModalBody>
          <CardColumns className="p-4">
            <ConnectionsCard s={s} />
            <PeersCard s={s} />
            {s.connChanges.length > 0 && <ConnectionChangesCard s={s} />}
          </CardColumns>
        </RecordModalBody>
      </TabsContent>

      <TabsContent value="feedback" className="flex min-h-0 flex-col">
        <FeedbackView s={s} />
      </TabsContent>

      <TabsContent value="identity" className="flex min-h-0 flex-col">
        <IdentityView s={s} />
      </TabsContent>

      <TabsContent value="json" className="flex min-h-0 flex-col px-4 py-4">
        <LazyJsonEditor
          value={json}
          onChange={noChange}
          readOnly
          fileName={`${s.id}.session.json`}
          aria-label="The session as JSON"
          className="flex-1 rounded-md border"
        />
      </TabsContent>
    </Tabs>
  )
}

/** The JSON tab only shows the session: nothing it could change. */
function noChange() {}

function Badge({ children, className }: { children: ReactNode; className?: string }) {
  return <span className={cn("rounded-md border px-1.5 py-0.5 text-xs text-muted-foreground", className)}>{children}</span>
}

/** The process's user as the session shows it: domain\user where the domain is apart. */
function userOf(s: Session): string | null {
  return s.user ? (s.domain && !s.user.includes("\\") ? `${s.domain}\\${s.user}` : s.user) : null
}

/** How a session stands, as the badge by its last seen says it. */
const STANDING: Record<string, { label: string; className: string }> = {
  live: { label: "Live", className: "border-emerald-600/40 text-emerald-600 dark:text-emerald-400" },
  late: { label: "Late", className: "border-amber-600/40 text-amber-600 dark:text-amber-400" },
  lost: { label: "Lost", className: "border-red-600/40 text-red-600 dark:text-red-400" },
}

/**
 * Under the session's title: where it runs (host, user, OS), how it stands
 * (live, late or lost by its schedule, or connected; closed, reserved,
 * archived) and when it was last seen, ticking as the table's column does.
 */
function SessionHeadline({ s }: { s: Session }) {
  const now = useNow(1_000)
  const standing = s.connected ? "live" : lateness(s, now)
  const where = [s.host || "unknown host", userOf(s), platformLabel(`${s.os}/${s.arch}`)].filter(Boolean).join(" · ")
  const ago = s.lastSeenAt ? formatAgo(now - Date.parse(s.lastSeenAt)) : null
  return (
    <div className="flex min-w-0 items-baseline gap-2 text-sm text-muted-foreground">
      <span className="truncate">{where}</span>
      {s.state === "CLOSED" ? (
        <Badge>Closed</Badge>
      ) : s.state === "RESERVED" ? (
        <Badge>Reserved</Badge>
      ) : (
        standing && <Badge className={STANDING[standing].className}>{STANDING[standing].label}</Badge>
      )}
      {s.archived && <Badge>Archived</Badge>}
      <span className="shrink-0 tabular-nums" title={s.lastSeenAt ? formatAbsolute(s.lastSeenAt) : undefined}>
        {s.connected ? "connected" : s.lastSeenAt ? `seen ${ago ?? formatAbsolute(s.lastSeenAt)}` : "not seen yet"}
      </span>
    </div>
  )
}

const none = <span className="text-muted-foreground">—</span>

/** A time: the date and time, and how long ago within the last day. */
function When({ at, now }: { at: string; now: number }) {
  const ago = formatAgo(now - Date.parse(at))
  return (
    <span title={formatAbsolute(at)}>
      <FormattedDateTimeText date={at} />
      {ago !== null && <span className="text-muted-foreground"> · {ago}</span>}
    </span>
  )
}

function SessionCard({ s }: { s: Session }) {
  // Ticks, as the table does: Last seen counts a silent minion's seconds.
  const now = useNow(1_000)
  const state = s.connected ? "live" : lateness(s, now)
  return (
    <InfoCard title="Session" note={s.persisted ? "persisted" : undefined}>
      {s.state === "RESERVED" && <NoteRow>Reserved for its build&apos;s first minion.</NoteRow>}
      <LabelRow label="Opened">
        <When at={s.createdAt} now={now} />
      </LabelRow>
      <LabelRow label="Last seen">
        {s.lastSeenAt ? (
          <span
            className={cn(
              state === "live" && "text-emerald-600 dark:text-emerald-400",
              state === "late" && "text-amber-600 dark:text-amber-400",
              state === "lost" && "text-red-600 dark:text-red-400",
            )}
            title={`${formatAbsolute(s.lastSeenAt)} (${formatAgoExact(now - Date.parse(s.lastSeenAt))})`}
          >
            <FormattedDateTimeText date={s.lastSeenAt} />
            {formatAgo(now - Date.parse(s.lastSeenAt)) !== null && <> · {formatAgo(now - Date.parse(s.lastSeenAt))}</>}
            {s.connected ? <> · connected</> : state && <> · {state}</>}
          </span>
        ) : (
          <span className="text-muted-foreground">not yet</span>
        )}
      </LabelRow>
      <LabelRow label="Check-ins">
        {s.sleepInterval != null ? (
          <>
            every {formatInterval(s.sleepInterval)}
            {s.sleepJitter ? ` ± ${s.sleepJitter}%` : ""}
          </>
        ) : (
          <span className="text-muted-foreground">no schedule said</span>
        )}
      </LabelRow>
      {s.lateAt && (
        <LabelRow label="Late at">
          <When at={s.lateAt} now={now} />
        </LabelRow>
      )}
      {s.lostAt && (
        <LabelRow label="Lost at">
          <When at={s.lostAt} now={now} />
        </LabelRow>
      )}
      {s.codecDownSince && (
        <LabelRow label="Codec">
          <span className="text-red-600 dark:text-red-400" title="Its factory reads and writes this session's messages: until it answers, what the minion sends is refused and what core has for it waits">
            its factory does not answer since <When at={s.codecDownSince} now={now} />
          </span>
        </LabelRow>
      )}
      <LabelRow label="Tasks">
        <MaxTasksValue s={s} />
      </LabelRow>
      <LabelRow label="Restarts">{s.restartCount}</LabelRow>
      <LabelRow label="Session id">
        <code className="font-mono text-xs break-all">{s.id}</code>
      </LabelRow>
    </InfoCard>
  )
}

function HostCard({ s }: { s: Session }) {
  const user = userOf(s)
  return (
    <InfoCard title="Host">
      <LabelRow label="Host">{s.host || none}</LabelRow>
      <LabelRow label="Machine">{s.machineId ? <span className="font-mono text-xs break-all">{s.machineId}</span> : none}</LabelRow>
      <LabelRow label="User">{user || none}</LabelRow>
      <LabelRow label="Domain">{s.domain || none}</LabelRow>
      <LabelRow label="Integrity">{s.integrity || none}</LabelRow>
      {s.acting && <LabelRow label="Tasks run as">{identityLabel(s.acting)}</LabelRow>}
      <LabelRow label="OS">
        {s.os} · {s.arch}
      </LabelRow>
      <LabelRow label="Process">{s.proc || none}</LabelRow>
      <LabelRow label="PID">
        {s.pid === 0 ? (
          <span className="text-muted-foreground">none: no process of its own</span>
        ) : (
          <span className="font-mono">{s.pid}</span>
        )}
      </LabelRow>
      <LabelRow label="Minion version">{s.version || none}</LabelRow>
    </InfoCard>
  )
}

function BuildCard({
  s,
  icons,
  onOpenBuild,
}: {
  s: Session
  icons: ReturnType<typeof useFactoryIcons>
  onOpenBuild: () => void
}) {
  return (
    <InfoCard
      title="Build"
      actions={
        <Button variant="ghost" size="xs" onClick={onOpenBuild}>
          Open the build
        </Button>
      }
    >
      <LabelRow label="Factory">
        <KindName type="factory" name={s.moduleName} icon={icons.get(s.moduleName)} />
        <span className="text-muted-foreground"> · {s.target}</span>
      </LabelRow>
      <LabelRow label="Platform">{platformLabel(s.platform)}</LabelRow>
      <LabelRow label="Description">{s.buildDescription || none}</LabelRow>
      <LabelRow label="Keys">
        {s.staged ? "its own, from a stage" : "the build's own (no stage)"}
      </LabelRow>
      <LabelRow label="Build id">
        <code className="font-mono text-xs break-all">{s.buildId}</code>
      </LabelRow>
    </InfoCard>
  )
}

function AddressesCard({ s }: { s: Session }) {
  return (
    <InfoCard title="Addresses" note={s.addrs.length}>
      {s.addrs.length === 0 ? (
        <NoteRow>The hello sent none.</NoteRow>
      ) : (
        <IdRows
          rows={s.addrs.map((a) => ({
            id: a,
            content:
              a === s.address ? (
                <span className="text-muted-foreground">{s.addressPicked ? "shown, picked by an operator" : "shown"}</span>
              ) : (
                ""
              ),
          }))}
        />
      )}
    </InfoCard>
  )
}

function PeersCard({ s }: { s: Session }) {
  const now = useNow(1_000)
  return (
    <InfoCard title="External addresses" note={s.peers.length}>
      {s.peers.length === 0 ? (
        <NoteRow>No channel said where its messages came from.</NoteRow>
      ) : (
        <IdRows
          rows={s.peers.map((p) => ({
            id: p.address,
            content: (
              <span>
                last seen <When at={p.lastSeen} now={now} /> · {p.count}×
                {p.address === s.peerAddress && <span className="text-muted-foreground"> · current</span>}
                <div className="text-xs text-muted-foreground">
                  first seen <FormattedDateTimeText date={p.firstSeen} />
                </div>
              </span>
            ),
          }))}
        />
      )}
    </InfoCard>
  )
}

/** The moves to another connection the minion reported, most recent first. */
function ConnectionChangesCard({ s }: { s: Session }) {
  const now = useNow(60_000)
  return (
    <InfoCard title="Connection changes" note={s.connChanges.length}>
      {/* The same move may come again, so by place. */}
      {s.connChanges.map((c, i) => (
        <LabelRow key={i} label={`${c.from ?? "?"} → ${c.to}`}>
          <When at={c.at} now={now} />
          {c.reason && <span className="text-muted-foreground"> · {c.reason}</span>}
        </LabelRow>
      ))}
    </InfoCard>
  )
}

function RestartsCard({ s }: { s: Session }) {
  const now = useNow(60_000)
  return (
    <InfoCard title="Restarts" note={s.restartCount > s.restarts.length ? `latest ${s.restarts.length} of ${s.restartCount}` : s.restartCount}>
      {/* Most recent first; the same PID may come back, so by place. */}
      {s.restarts.map((r, i) => (
        <LabelRow key={i} label={`PID ${r.pid}`}>
          <When at={r.at} now={now} />
          {r.host && <span className="text-muted-foreground"> · {r.host}</span>}
        </LabelRow>
      ))}
    </InfoCard>
  )
}

/** The factory's own fields, if it sent any. */
function extras(s: Session): [string, unknown][] {
  return s.x && typeof s.x === "object" && !Array.isArray(s.x) ? Object.entries(s.x as Record<string, unknown>) : []
}

function ExtrasCard({ s }: { s: Session }) {
  return (
    <InfoCard title="Factory fields">
      <IdRows
        rows={extras(s).map(([key, value]) => ({
          id: key,
          content: typeof value === "string" ? value : <code className="font-mono text-xs">{JSON.stringify(value)}</code>,
        }))}
      />
    </InfoCard>
  )
}
