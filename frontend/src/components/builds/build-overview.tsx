import { useMemo, type ReactNode } from "react"
import { CopyIcon, Trash2Icon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { CardColumns } from "@/components/ui/card-columns"
import { IdRows, InfoCard, LabelRow, NoteRow, SectionRow, type IdRow } from "@/components/ui/info-card"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { cn } from "@/lib/utils"
import { formatBytes } from "@/lib/format-bytes"
import { formatAbsolute, relativeTime } from "@/lib/relative-time"
import { useBuildFiles, useFactoryIcons, useFactoryKinds } from "@/graphql/hooks/builds"
import { useChannelIcons, useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import { kindsByName } from "@/components/endpoints/endpoint-values"
import { formatElapsed, formatValue, type FieldDef } from "@/components/modules/management/field-values"
import { KindName, ModuleIcon } from "@/components/modules/module-icon"
import { BuildStateBadge } from "@/components/builds/build-state-badge"
import {
  buildElapsed,
  factoryKindsByName,
  platformLabel,
  recordedConnections,
  targetFields,
  type Command,
} from "@/components/builds/build-values"
import { valueRows, type BuildRecord, type ValueRow } from "@/components/builds/build-record"
import { BuildNotices } from "@/components/builds/build-detail-parts"

/**
 * The build modal's Overview tab: what the JSON tab holds, for reading, in
 * cards each as tall as it needs, as a factory's page has them. What went
 * wrong first, then the build's summary, its artifact, what the minion can
 * do (its commands), the values it was given, and each connection with its
 * channel's settings and the factory's values for it. Values equal to their
 * default are dimmed, so the ones somebody chose stand out.
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
  const channelKinds = useChannelKinds()
  const files = useBuildFiles(b.operationId)
  const icons = useFactoryIcons()
  const channelIcons = useChannelIcons()
  const kind = useMemo(() => factoryKindsByName(kinds.data?.factoryKinds ?? []).get(b.moduleName), [kinds.data, b.moduleName])
  const channels = useMemo(() => kindsByName(channelKinds.data?.channelKinds ?? []), [channelKinds.data])
  const fileNames = useMemo(
    () => new Map((files.data?.buildFiles ?? []).map((f) => [f.id, f.name])),
    [files.data],
  )
  const target = kind?.targets.find((t) => t.id === b.target)
  const values = valueRows(kind ? targetFields(kind, target) : [], b.values)
  const connections = recordedConnections(b)
  const a = b.artifact

  return (
    <div className="flex min-w-0 flex-col gap-4 px-4 py-6">
      {/* First: what went wrong, what the factory warns about the result. */}
      <BuildNotices build={b} />
      <CardColumns>
        <InfoCard title="Summary">
          {b.description && (
            <LabelRow label="Description">
              <span className="whitespace-pre-line">{b.description}</span>
            </LabelRow>
          )}
          <LabelRow label="State">
            <BuildStateBadge build={b} />
          </LabelRow>
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

        <InfoCard title="Commands" note={b.commands.length}>
          {b.commands.length === 0 ? (
            <NoteRow>None: the minion was built without commands.</NoteRow>
          ) : (
            <IdRows rows={commandRows(b.commands, kind?.commands ?? [])} />
          )}
        </InfoCard>

        <InfoCard title="Values" note={values.length}>
          {values.length === 0 ? <NoteRow>None.</NoteRow> : <IdRows rows={rowsOf(values, fileNames)} />}
        </InfoCard>

        {connections.map((c, i) => {
          const settings = valueRows(channels.get(c.moduleName)?.fields ?? [], c.params)
          const own = valueRows(kind?.channelFields.get(c.moduleName) ?? [], c.values)
          // Headed only when the factory has values for the channel too.
          const parts = own.length > 0
          return (
            <InfoCard
              key={i}
              title={
                <span className="flex min-w-0 items-baseline gap-2">
                  <ModuleIcon type="channel" icon={channelIcons.get(c.moduleName)} className="size-4 self-center" />
                  <span className="truncate font-mono">{c.name}</span>
                </span>
              }
              note={`${c.moduleName} · connection ${i + 1} of ${connections.length}`}
            >
              {c.description && <NoteRow>{c.description}</NoteRow>}
              {parts && <SectionRow title="Settings" note={c.moduleName} />}
              {settings.length === 0 ? <NoteRow>No settings.</NoteRow> : <IdRows rows={rowsOf(settings, fileNames)} />}
              {parts && (
                <>
                  <SectionRow title={`${b.moduleName}'s values`} note={`for ${c.moduleName}`} />
                  <IdRows rows={rowsOf(own, fileNames)} />
                </>
              )}
            </InfoCard>
          )
        })}
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
 * The build's commands in the factory's order: each id, and its label and
 * description by today's schema; one the factory no longer has says so.
 */
function commandRows(ids: readonly string[], known: readonly Command[]): IdRow[] {
  const byId = new Map(known.map((c) => [c.id, c]))
  return ids.map((id) => {
    const c = byId.get(id)
    return {
      id,
      content: c ? (
        <>
          <div>
            {c.label}
            {c.required && <span className="text-xs text-muted-foreground"> · always built in</span>}
          </div>
          {c.description && <div className="text-xs text-muted-foreground">{c.description}</div>}
        </>
      ) : (
        <div className="text-muted-foreground">No longer a command of the factory</div>
      ),
    }
  })
}

/**
 * Values as rows: each key, its value (dimmed at its default, "not set" for
 * none; a file by its name), and under it the field's label.
 */
function rowsOf(rows: readonly ValueRow[], fileNames: ReadonlyMap<string, string>): IdRow[] {
  return rows.map((r) => ({
    id: r.key,
    content: (
      <>
        <div className={cn("whitespace-pre-line", (r.value === undefined || r.isDefault) && "text-muted-foreground")}>
          {valueText(r, fileNames)}
        </div>
        <div className="text-xs text-muted-foreground">
          {[r.field ? r.field.label : "no longer a field of the factory", r.isDefault ? "default" : ""]
            .filter(Boolean)
            .join(" · ")}
        </div>
      </>
    ),
  }))
}

function valueText(r: ValueRow, fileNames: ReadonlyMap<string, string>): ReactNode {
  if (r.value === undefined) return "not set"
  const f: FieldDef | null = r.field
  if (f?.type === "file" && typeof r.value === "string") return fileNames.get(r.value) ?? r.value
  return formatValue(f?.type ?? "", r.value, f?.unit)
}
