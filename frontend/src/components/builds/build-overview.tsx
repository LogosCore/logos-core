import { Fragment, useMemo } from "react"
import { CopyIcon, Trash2Icon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { copyToClipboard } from "@/lib/copy-to-clipboard"
import { cn } from "@/lib/utils"
import { formatBytes } from "@/lib/format-bytes"
import { formatAbsolute, relativeTime } from "@/lib/relative-time"
import { useBuildFiles, useFactoryIcons, useFactoryKinds } from "@/graphql/hooks/builds"
import { useChannelIcons, useChannelKinds } from "@/graphql/hooks/channel-endpoints"
import { kindsByName } from "@/components/endpoints/endpoint-values"
import { formatElapsed, formatValue, groupByField, type FieldDef } from "@/components/modules/management/field-values"
import { ModuleIcon } from "@/components/modules/module-icon"
import { BuildStateBadge } from "@/components/builds/build-state-badge"
import {
  buildElapsed,
  factoryKindsByName,
  platformLabel,
  recordedConnections,
  targetFields,
} from "@/components/builds/build-values"
import { valueRows, type BuildRecord, type ValueRow } from "@/components/builds/build-record"
import { Item, ItemList, Section } from "@/components/builds/build-detail-parts"

/**
 * The build modal's Overview tab: what the JSON tab holds, for reading. The
 * build itself, its artifact, the values it was given under the factory's
 * labels, and its connections, each with its channel's settings and the
 * factory's values for it. Values equal to their default are dimmed, so the
 * ones somebody chose stand out.
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
  const fields = kind ? targetFields(kind, target) : []
  const connections = recordedConnections(b)

  return (
    <div className="flex min-w-0 flex-col gap-6 px-4 py-6">
      <Section title="Summary">
        <ItemList>
          {b.description && (
            <Item label="Description">
              <span className="whitespace-pre-line">{b.description}</span>
            </Item>
          )}
          <Item label="State">
            <BuildStateBadge build={b} />
          </Item>
          <Item label="Factory">
            <span className="inline-flex items-center gap-1.5">
              <ModuleIcon type="factory" icon={icons.get(b.moduleName)} />
              <span className="font-mono">{b.moduleName}</span>
            </span>
            <span className="text-muted-foreground"> · schema v{b.schemaVersion}</span>
            {kind && kind.version !== b.schemaVersion && (
              <span className="text-muted-foreground"> (the factory is at v{kind.version} now)</span>
            )}
          </Item>
          <Item label="Target">
            {target ? target.label : <span className="font-mono">{b.target}</span>}
            {target && <span className="font-mono text-muted-foreground"> · {b.target}</span>}
          </Item>
          <Item label="Platform">
            {platformLabel(b.platform)} <span className="font-mono text-muted-foreground">· {b.platform}</span>
          </Item>
          <Item label="Minion id">
            <span className="inline-flex items-center gap-1">
              <code className="font-mono text-xs">{b.id}</code>
              <Button
                variant="ghost"
                size="icon-sm"
                title="Copy"
                aria-label="Copy the minion id"
                onClick={() => copyToClipboard(b.id, "Minion id")}
              >
                <CopyIcon />
              </Button>
            </span>
          </Item>
          <Item label="Factory instance">
            {b.instance ? (
              <>
                <span className="font-mono">{b.instance}</span>
                {b.factoryVersion && <span className="text-muted-foreground"> · {b.factoryVersion}</span>}
              </>
            ) : (
              <span className="text-muted-foreground">not taken yet</span>
            )}
          </Item>
          <Item label="Started">
            {formatAbsolute(b.createdAt)}
            {b.createdBy.name && <span className="text-muted-foreground"> by {b.createdBy.name}</span>}
          </Item>
          {b.finishedAt ? (
            <Item label="Finished">
              {formatAbsolute(b.finishedAt)}{" "}
              <span className="text-muted-foreground">· took {formatElapsed(buildElapsed(b))}</span>
              {b.cancelledBy?.name && <span className="text-muted-foreground"> · cancelled by {b.cancelledBy.name}</span>}
            </Item>
          ) : (
            <Item label="Deadline">
              {formatAbsolute(b.deadline)} <span className="text-muted-foreground">({relativeTime(b.deadline)})</span>
            </Item>
          )}
        </ItemList>
      </Section>

      {b.artifact && (
        <Section title="Artifact">
          <ItemList>
            <Item label="File">
              <span className="font-mono">{b.artifact.filename}</span>{" "}
              <span className="text-muted-foreground">· {formatBytes(b.artifact.size)}</span>
              {b.artifact.mediaType && <span className="text-muted-foreground"> · {b.artifact.mediaType}</span>}
            </Item>
            <Item label="SHA-256">
              <span className="inline-flex min-w-0 items-center gap-1">
                <code className="break-all font-mono text-xs">{b.artifact.sha256}</code>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  title="Copy"
                  aria-label="Copy the SHA-256"
                  onClick={() => copyToClipboard(b.artifact?.sha256 ?? "", "SHA-256")}
                >
                  <CopyIcon />
                </Button>
              </span>
            </Item>
            <Item label="Capabilities">
              {b.capabilities.length === 0 ? (
                <span className="text-muted-foreground">none reported</span>
              ) : (
                <span className="flex flex-wrap gap-1">
                  {b.capabilities.map((c) => (
                    <Badge key={c} variant="secondary" className="font-mono">
                      {c}
                    </Badge>
                  ))}
                </span>
              )}
            </Item>
          </ItemList>
          {b.artifact.deleted ? (
            <p className="pb-3 text-sm text-muted-foreground">The artifact&apos;s bytes were deleted; the build record stays.</p>
          ) : (
            canWrite && (
              <div className="pb-3">
                <Button variant="outline" size="sm" onClick={onDeleteArtifact}>
                  <Trash2Icon />
                  Delete the artifact
                </Button>
              </div>
            )
          )}
        </Section>
      )}

      <Section title="Values">
        <GroupedValues fields={fields} values={b.values} fileNames={fileNames} />
      </Section>

      {connections.length > 0 && (
        <Section title="Connections">
          <ol className="divide-y">
            {connections.map((c, i) => (
              <li key={i} className="flex min-w-0 flex-col gap-1 py-3">
                <div className="flex min-w-0 items-center gap-2 text-sm">
                  <span className="text-xs text-muted-foreground tabular-nums">{i + 1}.</span>
                  <ModuleIcon type="channel" icon={channelIcons.get(c.moduleName)} className="size-5" />
                  <span className="truncate font-mono font-medium">{c.name}</span>
                  <span className="font-mono text-muted-foreground">· {c.moduleName}</span>
                </div>
                {c.description && <p className="whitespace-pre-line text-sm text-muted-foreground">{c.description}</p>}
                <SubHeading>Settings</SubHeading>
                <ValueList rows={valueRows(channels.get(c.moduleName)?.fields ?? [], c.params)} fileNames={fileNames} />
                {(kind?.channelFields.get(c.moduleName)?.length || Object.keys(c.values).length > 0) && (
                  <>
                    <SubHeading>{b.moduleName}&apos;s values for it</SubHeading>
                    <ValueList
                      rows={valueRows(kind?.channelFields.get(c.moduleName) ?? [], c.values)}
                      fileNames={fileNames}
                    />
                  </>
                )}
              </li>
            ))}
          </ol>
        </Section>
      )}
    </div>
  )
}

function SubHeading({ children }: { children: React.ReactNode }) {
  return <h3 className="mt-2 text-xs font-medium text-muted-foreground">{children}</h3>
}

/** The build's values under their fields' groups, as the form has them. */
function GroupedValues({
  fields,
  values,
  fileNames,
}: {
  fields: readonly FieldDef[]
  values: unknown
  fileNames: ReadonlyMap<string, string>
}) {
  const rows = valueRows(fields, values)
  const known = rows.filter((r) => r.field !== null)
  const gone = rows.filter((r) => r.field === null)
  const groups = groupByField(known, (r) => r.field!)
  return (
    <div className="flex flex-col">
      {groups.map((g) => (
        <Fragment key={g.group}>
          {groups.length > 1 && <SubHeading>{g.group}</SubHeading>}
          <ValueList rows={g.items} fileNames={fileNames} />
        </Fragment>
      ))}
      {gone.length > 0 && (
        <>
          <SubHeading>No longer fields of the factory</SubHeading>
          <ValueList rows={gone} fileNames={fileNames} />
        </>
      )}
    </div>
  )
}

function ValueList({ rows, fileNames }: { rows: readonly ValueRow[]; fileNames: ReadonlyMap<string, string> }) {
  if (rows.length === 0) return <p className="py-3 text-sm text-muted-foreground">None.</p>
  return (
    <ItemList>
      {rows.map((r) => (
        <Item key={r.key} label={r.field?.label ?? r.key} title={r.key}>
          <ValueText row={r} fileNames={fileNames} />
        </Item>
      ))}
    </ItemList>
  )
}

function ValueText({ row, fileNames }: { row: ValueRow; fileNames: ReadonlyMap<string, string> }) {
  if (row.value === undefined) return <span className="text-muted-foreground">not given</span>
  const type = row.field?.type ?? ""
  const text =
    type === "file" && typeof row.value === "string"
      ? (fileNames.get(row.value) ?? row.value)
      : formatValue(type, row.value, row.field?.unit)
  return (
    <span className={cn("break-words whitespace-pre-line", row.isDefault && "text-muted-foreground")}>
      {text}
      {row.isDefault && <span className="ml-2 text-xs">default</span>}
    </span>
  )
}
