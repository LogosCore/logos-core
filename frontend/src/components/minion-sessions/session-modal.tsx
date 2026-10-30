import { useEffect, useMemo, type ReactNode } from "react"
import { MonitorDotIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { CardColumns } from "@/components/ui/card-columns"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { IdRows, InfoCard, LabelRow, NoteRow } from "@/components/ui/info-card"
import { LazyJsonEditor, preloadJsonEditor } from "@/components/ui/lazy-json-editor"
import {
  RecordModal,
  RecordModalBody,
  RecordModalHeader,
  RecordModalMessage,
  RecordModalTitle,
} from "@/components/ui/record-modal"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { platformLabel } from "@/components/builds/build-values"
import { KindName, ModuleIcon } from "@/components/modules/module-icon"
import { DocumentIcon } from "@/components/wiki/document-icon"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { useFactoryIcons } from "@/graphql/hooks/builds"
import { useMinionSession } from "@/graphql/hooks/minion-sessions"
import { useNow } from "@/hooks/use-now"
import { useRecordModals } from "@/hooks/use-record-modals"
import { formatJson } from "@/lib/json-edit"
import { whenIdle } from "@/lib/preloadable"
import type { RecordModal as RecordModalTarget, SessionTab } from "@/lib/record-modals"
import { formatAbsolute } from "@/lib/relative-time"
import { cn } from "@/lib/utils"
import { formatAgo, formatAgoExact, formatInterval, lateness } from "./sessions-layout"

// One minion session (?session=<id>[&sessionTab=]), over any page: Overview,
// what it is and where it runs, in cards; JSON, its record as the API gives
// it. Live where the page keeps sessions live (the Sessions page): the list's
// updates reach the session's own query too.

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
  // Fetched once the modal has settled, so the JSON tab opens at once.
  useEffect(() => whenIdle(preloadJsonEditor), [])
  return (
    <Tabs
      value={tab}
      onValueChange={(v) => {
        if (v === "overview" || v === "json") open({ kind: "session", id: s.id, tab: v })
      }}
      className="min-h-0 flex-1 gap-0"
    >
      <RecordModalHeader>
        <div className="flex min-w-0 items-center gap-3">
          <ModuleIcon type="factory" icon={icons.get(s.moduleName)} className="size-9" />
          <RecordModalTitle>
            <span className="inline-flex items-center gap-2">
              {s.state === "RESERVED" ? "Reserved session" : `Session ${s.number}`}
              {(s.mark?.icon || s.mark?.emoji) && <DocumentIcon icon={s.mark.icon} emoji={s.mark.emoji} size={18} />}
            </span>
          </RecordModalTitle>
          {s.archived && <Badge>Archived</Badge>}
          {s.state === "CLOSED" && <Badge>Closed</Badge>}
        </div>
        <div className="flex items-center gap-2 sm:ml-auto">
          <TabsList>
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="json">JSON</TabsTrigger>
          </TabsList>
        </div>
      </RecordModalHeader>

      <TabsContent value="overview" className="flex min-h-0 flex-col">
        <RecordModalBody>
          <CardColumns className="p-4">
            <SessionCard s={s} />
            <HostCard s={s} />
            <BuildCard s={s} onOpenBuild={() => open({ kind: "build", id: s.buildId })} icons={icons} />
            <AddressesCard s={s} />
            <PeersCard s={s} />
            {s.restarts.length > 0 && <RestartsCard s={s} />}
            {extras(s).length > 0 && <ExtrasCard s={s} />}
          </CardColumns>
        </RecordModalBody>
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

function Badge({ children }: { children: ReactNode }) {
  return <span className="rounded-md border px-1.5 py-0.5 text-xs text-muted-foreground">{children}</span>
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

const STATE_LABEL: Record<string, string> = { OPEN: "Open", CLOSED: "Closed", RESERVED: "Reserved for its build's first minion" }

function SessionCard({ s }: { s: Session }) {
  // Ticks, as the table does: Last seen counts a silent minion's seconds.
  const now = useNow(1_000)
  const state = lateness(s, now)
  return (
    <InfoCard title="Session" note={s.persisted ? "persisted" : undefined}>
      <LabelRow label="Description">{s.description || none}</LabelRow>
      <LabelRow label="State">{STATE_LABEL[s.state] ?? s.state}</LabelRow>
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
            {state && <> · {state}</>}
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
      <LabelRow label="Restarts">{s.restartCount}</LabelRow>
      <LabelRow label="Session id">
        <code className="font-mono text-xs break-all">{s.id}</code>
      </LabelRow>
    </InfoCard>
  )
}

function HostCard({ s }: { s: Session }) {
  const user = s.user ? (s.domain && !s.user.includes("\\") ? `${s.domain}\\${s.user}` : s.user) : null
  return (
    <InfoCard title="Host">
      <LabelRow label="Host">{s.host || none}</LabelRow>
      <LabelRow label="User">{user || none}</LabelRow>
      <LabelRow label="Domain">{s.domain || none}</LabelRow>
      <LabelRow label="Integrity">{s.integrity || none}</LabelRow>
      <LabelRow label="OS">
        {s.os} · {s.arch}
      </LabelRow>
      <LabelRow label="Process">{s.proc || none}</LabelRow>
      <LabelRow label="PID">
        <span className="font-mono">{s.pid}</span>
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
