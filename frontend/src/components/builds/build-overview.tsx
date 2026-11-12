import { useMemo, type ReactNode } from "react"
import { CopyIcon, Trash2Icon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { CardColumns } from "@/components/ui/card-columns"
import { IdRows, InfoCard, LabelRow, NoteRow } from "@/components/ui/info-card"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { formatBytes } from "@/lib/format-bytes"
import { formatAbsolute, relativeTime } from "@/lib/relative-time"
import { useBuildFiles, useFactoryIcons, useFactoryKinds } from "@/graphql/hooks/builds"
import { useMinionSession } from "@/graphql/hooks/minion-sessions"
import { formatElapsed } from "@/components/modules/management/field-values"
import { KindName } from "@/components/modules/module-icon"
import {
  buildElapsed,
  factoryKindsByName,
  platformLabel,
  targetFields,
} from "@/components/builds/build-values"
import { valueRows, type BuildRecord } from "@/components/builds/build-record"
import { BuildNotices } from "@/components/builds/build-detail-parts"
import { rowsOf } from "@/components/builds/build-value-rows"
import { ErrorCodes } from "@/components/builds/error-codes"

/**
 * The build modal's Overview tab: what the JSON tab holds, for reading, in
 * cards each as tall as it needs, as a factory's page has them. What went
 * wrong first, then the build's summary (its description and state are the
 * modal's header) and the values it was given side by side, its artifact
 * under the summary. Its connections and what the minion can do have tabs
 * of their own. Values equal to their default are dimmed, so the ones
 * somebody chose stand out. Last, the factory's error codes, when it
 * declares any (by today's schema, as the Commands tab's labels): what the
 * minion's exit code means.
 */
export function BuildOverview({
  build: b,
  canWrite,
  onDeleteArtifact,
}: {
  build: BuildRecord
  canWrite: boolean
  onDeleteArtifact: () => void
}) {
  const kinds = useFactoryKinds()
  const files = useBuildFiles(b.operationId)
  const icons = useFactoryIcons()
  const kind = useMemo(() => factoryKindsByName(kinds.data?.factoryKinds ?? []).get(b.moduleName), [kinds.data, b.moduleName])
  const fileNames = useMemo(
    () => new Map((files.data?.buildFiles ?? []).map((f) => [f.id, f.name])),
    [files.data],
  )
  const target = kind?.targets.find((t) => t.id === b.target)
  const values = valueRows(kind ? targetFields(kind, target) : [], b.values)
  const a = b.artifact

  return (
    <div className="flex min-w-0 flex-col gap-4 px-4 py-6">
      {/* First: what went wrong, what the factory warns about the result. */}
      <BuildNotices build={b} />
      <CardColumns>
        <InfoCard title="Summary">
          {/* The header shows the description's first line: here, one longer. */}
          {b.description?.includes("\n") && (
            <LabelRow label="Description">
              <span className="whitespace-pre-line">{b.description}</span>
            </LabelRow>
          )}
          <LabelRow label="Factory">
            {/* KindName keeps the name on the text's baseline, with what follows. */}
            <KindName type="factory" name={b.moduleName} icon={icons.get(b.moduleName)} />
            <span className="text-muted-foreground"> · schema v{b.schemaVersion}</span>
            {kind && kind.version !== b.schemaVersion && (
              <span className="text-muted-foreground"> (the factory is at v{kind.version} now)</span>
            )}
            {b.factoryVersion && <span className="text-muted-foreground"> · version {b.factoryVersion}</span>}
          </LabelRow>
          <LabelRow label="Target">
            {target ? target.label : <span className="font-mono">{b.target}</span>}
            {target && <span className="font-mono text-muted-foreground"> · {b.target}</span>}
          </LabelRow>
          <LabelRow label="Platform">
            {platformLabel(b.platform)} <span className="font-mono text-muted-foreground">· {b.platform}</span>
          </LabelRow>
          <LabelRow label="Build id">
            <Copyable text={b.id} what="Build id" />
          </LabelRow>
          {b.maxTasks != null && (
            <LabelRow label="Tasks at once">
              <span className="tabular-nums">{b.maxTasks}</span>
              <span className="text-muted-foreground">{b.maxTasks === 0 ? " · none until a session sets more" : " · as it starts; its sessions change it"}</span>
            </LabelRow>
          )}
          {/* Packed messages: the build's algorithm, or off where its factory's minions inflate. */}
          {(b.compression || kind?.compression) && (
            <LabelRow label="Compression">
              {b.compression ? (
                <>
                  <span className="font-mono">{b.compression}</span>
                  <span className="text-muted-foreground"> · each item that shrinks goes packed, both ways</span>
                </>
              ) : (
                <span className="text-muted-foreground">off</span>
              )}
            </LabelRow>
          )}
          {/* Exit codes: drawn for the build, or its factory's where it turned that off. */}
          {(b.uniqueErrorCodes || kind?.errorCodesPerBuild) && (
            <LabelRow label="Exit codes">
              {b.uniqueErrorCodes ? (
                <>
                  drawn for this build
                  <span className="text-muted-foreground"> · Minion error codes lists them</span>
                </>
              ) : (
                <span className="text-muted-foreground">the factory&apos;s: unique error codes off</span>
              )}
            </LabelRow>
          )}
          {/* How its messages are written and sealed, where not core's CBOR alone (docs: Codecs). */}
          {(b.envelope !== "cbor" || b.codec != null) && (
            <LabelRow label="Codec">
              <span className="font-mono">{b.envelope}</span>
              {cryptoSuite(b.crypto) && <span className="font-mono"> · {cryptoSuite(b.crypto)}</span>}
              {b.codec != null && <span className="text-muted-foreground"> · its factory translates its own</span>}
            </LabelRow>
          )}
          {b.persistedSession && (
            <LabelRow label="Persisted session">
              <PersistedSession id={b.persistedSession} />
            </LabelRow>
          )}
          <LabelRow label="Started">
            {formatAbsolute(b.createdAt)}
            {b.createdBy.name && <span className="text-muted-foreground"> by {b.createdBy.name}</span>}
          </LabelRow>
          {b.finishedAt ? (
            <LabelRow label="Finished">
              {formatAbsolute(b.finishedAt)}{" "}
              <span className="text-muted-foreground">· took {formatElapsed(buildElapsed(b))}</span>
              {b.cancelledBy?.name && <span className="text-muted-foreground"> · cancelled by {b.cancelledBy.name}</span>}
            </LabelRow>
          ) : (
            <LabelRow label="Deadline">
              {formatAbsolute(b.deadline)} <span className="text-muted-foreground">({relativeTime(b.deadline)})</span>
            </LabelRow>
          )}
        </InfoCard>

        <InfoCard title="Values" note={values.length}>
          {values.length === 0 ? <NoteRow>None.</NoteRow> : <IdRows rows={rowsOf(values, fileNames)} />}
        </InfoCard>

        {a && (
          <InfoCard
            title="Artifact"
            note={formatBytes(a.size)}
            actions={
              canWrite &&
              !a.deleted && (
                <Button type="button" variant="ghost" size="xs" onClick={onDeleteArtifact}>
                  <Trash2Icon />
                  Delete
                </Button>
              )
            }
          >
            <LabelRow label="File">
              <span className="font-mono">{a.filename}</span>
              {a.mediaType && <span className="text-muted-foreground"> · {a.mediaType}</span>}
            </LabelRow>
            <LabelRow label="SHA-256">
              <Copyable text={a.sha256} what="SHA-256" />
            </LabelRow>
            {a.deleted && <NoteRow>The artifact&apos;s bytes were deleted; the build record stays.</NoteRow>}
          </InfoCard>
        )}

        {/* The build's own codes; one queued before builds kept them shows its factory's. */}
        {b.errorCodes.length > 0 ? (
          <ErrorCodes codes={b.errorCodes} drawn={b.uniqueErrorCodes} />
        ) : (
          kind && kind.errorCodes.length > 0 && <ErrorCodes codes={kind.errorCodes} />
        )}
      </CardColumns>
    </div>
  )
}

/** A hash or an id, with a button that copies it. */
function Copyable({ text, what }: { text: string; what: string }) {
  return (
    <span className="flex min-w-0 items-center gap-1">
      <code className="min-w-0 font-mono text-xs break-all">{text}</code>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        title="Copy"
        aria-label={`Copy the ${what}`}
        onClick={() => copyToClipboard(text, what)}
      >
        <CopyIcon />
      </Button>
    </span>
  )
}

/**
 * The session the build's minions come back to when they restart: its
 * number and host once open, "reserved" until the first start claims it.
 */
function PersistedSession({ id }: { id: string }) {
  const { data, isLoading } = useMinionSession(id)
  const s = data?.minionSession
  let what: ReactNode = null
  if (!isLoading) {
    if (!s) what = <span className="text-muted-foreground">gone</span>
    else if (s.state === "RESERVED") what = <span className="text-muted-foreground">reserved, not claimed yet</span>
    else {
      what = (
        <span>
          #{s.number}
          {s.host && <span className="text-muted-foreground"> · {s.host}</span>}
          {s.state === "CLOSED" && <span className="text-muted-foreground"> · closed</span>}
        </span>
      )
    }
  }
  return (
    <span className="flex min-w-0 flex-col">
      {what}
      <Copyable text={id} what="Session id" />
    </span>
  )
}

/** The suite of a build's crypto ({suite, material}), if any. */
function cryptoSuite(crypto: unknown): string | null {
  const suite = (crypto as { suite?: unknown } | null)?.suite
  return typeof suite === "string" ? suite : null
}
