import { useEffect, useState } from "react"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip"
import { Badge } from "@/components/ui/badge"
import { useAuthStore } from "@/stores/auth"
import { useUpdateWikiDocument } from "@/graphql/hooks/wiki"
import { WikiChecklistCoverageBar } from "@/components/wiki/wiki-checklist-coverage-bar"
import type { WikiDocumentFieldsFragment, WikiDocumentStatus } from "@/graphql/gql/graphql"

interface WikiDocumentMetaProps {
  document: WikiDocumentFieldsFragment
  isEditor?: boolean
}

interface Actor {
  id: string
  username: string
}

// Thresholds for cheap "just now" collapsing — avoids "0 seconds ago"
// churn for freshly saved docs where clock skew can produce negative deltas.
const JUST_NOW_THRESHOLD_MS = 30_000
// Refresh cadence — keeps the relative-time string fresh without polling the
// server. At one-minute ticks it's imperceptible for anything over an hour old.
const REFRESH_INTERVAL_MS = 60_000

export function WikiDocumentMeta({ document, isEditor }: WikiDocumentMetaProps) {
  const currentUserId = useAuthStore((s) => s.user?.userId)
  const updateDocument = useUpdateWikiDocument()

  const [, setTick] = useState(0)
  useEffect(() => {
    const id = window.setInterval(
      () => setTick((t) => t + 1),
      REFRESH_INTERVAL_MS,
    )
    return () => window.clearInterval(id)
  }, [])

  const hasUpdate = !!(document.lastUpdatedAt && document.lastUpdatedBy)

  // Inline editing state — initialized from props when entering edit mode,
  // so no sync effects needed.
  const [editingPageType, setEditingPageType] = useState(false)
  const [pageTypeValue, setPageTypeValue] = useState("")
  const [editingTags, setEditingTags] = useState(false)
  const [tagsValue, setTagsValue] = useState("")
  function startEditPageType() {
    setPageTypeValue(document.pageType ?? "")
    setEditingPageType(true)
  }

  function startEditTags() {
    setTagsValue(document.tags.join(", "))
    setEditingTags(true)
  }

  function savePageType() {
    const trimmed = pageTypeValue.trim()
    if (trimmed !== (document.pageType ?? "")) {
      updateDocument.mutate({ id: document.id, input: { pageType: trimmed || "" } })
    }
    setEditingPageType(false)
  }

  function saveTags() {
    const parsed = tagsValue.split(",").map((t) => t.trim()).filter(Boolean)
    const changed = parsed.length !== document.tags.length || parsed.some((t, i) => t !== document.tags[i])
    if (changed) {
      updateDocument.mutate({ id: document.id, input: { tags: parsed } })
    }
    setEditingTags(false)
  }

  function handleStatusChange(value: string | null) {
    if (value && value !== document.status) {
      updateDocument.mutate({ id: document.id, input: { status: value as WikiDocumentStatus } })
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 px-6 pt-4 pb-1 text-xs text-muted-foreground">
      <MetaEntry
        verb="Created"
        actor={document.createdBy}
        timestamp={document.createdAt}
        currentUserId={currentUserId}
      />
      {hasUpdate && (
        <MetaEntry
          verb="Updated"
          actor={document.lastUpdatedBy!}
          timestamp={document.lastUpdatedAt!}
          currentUserId={currentUserId}
        />
      )}

      {/* Status */}
      {isEditor ? (
        <Select value={document.status} onValueChange={handleStatusChange}>
          <SelectTrigger className="h-5 w-auto gap-1 rounded-none border-none bg-transparent px-0 py-0 text-xs text-muted-foreground shadow-none dark:bg-transparent dark:hover:bg-transparent">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="STABLE">Stable</SelectItem>
            <SelectItem value="DRAFT">Draft</SelectItem>
            <SelectItem value="DEPRECATED">Deprecated</SelectItem>
          </SelectContent>
        </Select>
      ) : document.status !== "STABLE" ? (
        <WikiStatusBadge status={document.status} />
      ) : null}

      {/* Page type */}
      {editingPageType ? (
        <Input
          value={pageTypeValue}
          onChange={(e) => setPageTypeValue(e.target.value)}
          onBlur={savePageType}
          onKeyDown={(e) => {
            if (e.key === "Enter") (e.target as HTMLInputElement).blur()
            if (e.key === "Escape") { setPageTypeValue(document.pageType ?? ""); setEditingPageType(false) }
          }}
          autoFocus
          placeholder="Page type"
          className="h-5 w-28 text-xs"
        />
      ) : document.pageType ? (
        <button
          className="text-muted-foreground/70 hover:text-foreground transition-colors"
          onClick={() => isEditor && startEditPageType()}
          disabled={!isEditor}
        >
          {document.pageType}
        </button>
      ) : isEditor ? (
        <button
          className="text-muted-foreground/40 hover:text-muted-foreground transition-colors"
          onClick={startEditPageType}
        >
          + type
        </button>
      ) : null}

      {/* Tags */}
      {editingTags ? (
        <Input
          value={tagsValue}
          onChange={(e) => setTagsValue(e.target.value)}
          onBlur={saveTags}
          onKeyDown={(e) => {
            if (e.key === "Enter") (e.target as HTMLInputElement).blur()
            if (e.key === "Escape") { setTagsValue(document.tags.join(", ")); setEditingTags(false) }
          }}
          autoFocus
          placeholder="tag1, tag2, ..."
          className="h-5 w-44 text-xs"
        />
      ) : document.tags.length > 0 ? (
        <button
          className="flex flex-wrap gap-1"
          onClick={() => isEditor && startEditTags()}
          disabled={!isEditor}
        >
          {document.tags.map((tag) => (
            <Badge key={tag} variant="secondary" className="text-[10px] px-1.5 py-0">
              {tag}
            </Badge>
          ))}
        </button>
      ) : isEditor ? (
        <button
          className="text-muted-foreground/40 hover:text-muted-foreground transition-colors"
          onClick={startEditTags}
        >
          + tags
        </button>
      ) : null}

      <WikiChecklistCoverageBar
        total={document.checklistTotal}
        required={document.checklistRequired}
        answered={document.checklistAnswered}
      />
    </div>
  )
}

interface MetaEntryProps {
  verb: string
  actor: Actor | null | undefined
  timestamp: string
  currentUserId: string | undefined
}

function MetaEntry({ verb, actor, timestamp, currentUserId }: MetaEntryProps) {
  const isSelf = actor?.id === currentUserId
  const actorLabel = isSelf ? "you" : (actor?.username ?? "unknown user")

  const parsed = new Date(timestamp)
  const relative = formatRelativeTime(parsed)

  return (
    <Tooltip>
      <TooltipTrigger render={<span className="cursor-default" />}>
        {verb} by {actorLabel} {relative}
      </TooltipTrigger>
      <TooltipContent>
        <FormattedDateTimeText date={parsed} />
      </TooltipContent>
    </Tooltip>
  )
}

const statusConfig: Record<WikiDocumentStatus, { label: string; variant: "secondary" | "destructive" | "outline" }> = {
  DRAFT: { label: "Draft", variant: "outline" },
  STABLE: { label: "Stable", variant: "secondary" },
  DEPRECATED: { label: "Deprecated", variant: "destructive" },
}

export function WikiStatusBadge({ status }: { status: WikiDocumentStatus }) {
  const config = statusConfig[status]
  if (!config || status === "STABLE") return null
  return (
    <Badge variant={config.variant} className="text-[10px] px-1.5 py-0">
      {config.label}
    </Badge>
  )
}

function formatRelativeTime(date: Date): string {
  const now = Date.now()
  const deltaMs = now - date.getTime()
  if (deltaMs < JUST_NOW_THRESHOLD_MS) return "just now"

  const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" })
  const deltaSec = Math.round(-deltaMs / 1000)
  const abs = Math.abs(deltaSec)

  if (abs < 60) return rtf.format(deltaSec, "second")
  if (abs < 3600) return rtf.format(Math.round(deltaSec / 60), "minute")
  if (abs < 86_400) return rtf.format(Math.round(deltaSec / 3600), "hour")
  if (abs < 604_800) return rtf.format(Math.round(deltaSec / 86_400), "day")
  if (abs < 2_592_000) return rtf.format(Math.round(deltaSec / 604_800), "week")
  if (abs < 31_536_000) return rtf.format(Math.round(deltaSec / 2_592_000), "month")
  return rtf.format(Math.round(deltaSec / 31_536_000), "year")
}

