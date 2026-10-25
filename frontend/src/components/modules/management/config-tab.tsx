import { useMemo, useState } from "react"
import { RotateCcwIcon, SearchXIcon, TriangleAlertIcon } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { SearchInput } from "@/components/ui/search-input"
import { cn } from "@/lib/utils"
import type { ModuleDetailFieldsFragment } from "@/graphql/gql/graphql"
import { useUpdateModuleConfig } from "@/graphql/hooks/modules"
import { errorMessage, graphqlErrorCode } from "./api-errors"
import { FieldControl } from "./field-control"
import {
  evaluateForm,
  groupByField,
  initialDraft,
  initialValue,
  resetDraft,
  type DiffEntry,
  type Draft,
} from "./field-values"
import { sourceLabel } from "./labels"

type Management = NonNullable<ModuleDetailFieldsFragment["management"]>
type Property = Management["properties"][number]

interface ConfigTabProps {
  module: ModuleDetailFieldsFragment
  management: Management
  canWrite: boolean
  refetch: () => unknown
}

export function ConfigTab({ module, management, canWrite, refetch }: ConfigTabProps) {
  // Only what the user touched. Untouched fields render from the latest
  // server data, so a refetch (after a conflict, or another admin's save
  // arriving via the subscription) updates them without losing edits.
  const [edits, setEdits] = useState<Record<string, Draft>>({})
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [search, setSearch] = useState("")
  const update = useUpdateModuleConfig()

  const properties = management.properties

  const drafts = useMemo(() => {
    const out: Record<string, Draft> = {}
    for (const p of properties) {
      out[p.field.key] = edits[p.field.key] ?? initialDraft(p.field, initialValue(p))
    }
    return out
  }, [properties, edits])

  const { diff, errors } = useMemo(() => evaluateForm(properties, drafts), [properties, drafts])
  // Search narrows what is shown only; edits to hidden properties still count
  // towards Save and appear in the confirm dialog.
  const visible = useMemo(() => filterProperties(properties, search), [properties, search])
  // One grid, ordered by group; each card names its group.
  const cards = useMemo(
    () => groupByField(visible, (p) => p.field).flatMap((g) => g.items),
    [visible],
  )
  const dirty = Object.keys(edits).length > 0
  const errorCount = Object.keys(errors).length

  function setDraft(key: string, draft: Draft) {
    setEdits((prev) => ({ ...prev, [key]: draft }))
  }

  function undo(key: string) {
    setEdits((prev) => {
      const next = { ...prev }
      delete next[key]
      return next
    })
  }

  function discard() {
    setEdits({})
  }

  async function save() {
    setSaveError(null)
    try {
      const res = await update.mutateAsync({
        instance: module.instance,
        expectedRevision: management.revision,
        values: diff.map((d) => ({ key: d.key, value: d.new })),
      })
      setEdits({})
      setConfirmOpen(false)
      const rev = res.updateModuleConfig.management?.revision
      toast.success("Configuration saved", {
        description: rev !== undefined ? `Applying revision ${rev}…` : undefined,
      })
    } catch (err) {
      const code = graphqlErrorCode(err)
      if (code === "CONFLICT") {
        setConfirmOpen(false)
        toast.error("Someone else changed this configuration", {
          description: "Reloaded the latest values — your edits are kept. Review and save again.",
        })
        refetch()
        return
      }
      if (code === "FORBIDDEN") {
        setConfirmOpen(false)
        toast.error("You don't have permission to change this configuration")
        return
      }
      setSaveError(errorMessage(err, "Failed to save configuration"))
    }
  }

  if (properties.length === 0) {
    return (
      <p className="py-8 text-center text-sm text-muted-foreground">
        This module declares no configurable properties.
      </p>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <SearchInput
          value={search}
          onValueChange={setSearch}
          placeholder="Search settings..."
          debounceMs={100}
          title=""
        />
        {canWrite ? (
          <div className="flex items-center gap-2">
            {errorCount > 0 && (
              <span className="text-sm text-destructive">
                {errorCount} {errorCount === 1 ? "error" : "errors"}
              </span>
            )}
            <Button variant="outline" onClick={discard} disabled={!dirty}>
              Discard
            </Button>
            <Button
              onClick={() => {
                setSaveError(null)
                setConfirmOpen(true)
              }}
              disabled={diff.length === 0 || errorCount > 0}
            >
              Save{diff.length > 0 ? ` (${diff.length})` : ""}
            </Button>
          </div>
        ) : (
          <span className="text-sm text-muted-foreground">
            Read-only — requires module:config:write
          </span>
        )}
      </div>

      {cards.length === 0 && (
        <div className="flex flex-col items-center gap-2 py-12 text-muted-foreground">
          <SearchXIcon className="size-8 opacity-50" />
          <p className="text-sm">No settings match “{search}”.</p>
        </div>
      )}

      {/* auto-fill sizes columns to the space beside the sidebar, not the
          viewport. */}
      <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,24rem),1fr))] gap-3">
        {cards.map((p) => (
          <PropertyCard
            key={p.field.key}
            property={p}
            draft={drafts[p.field.key]}
            edited={p.field.key in edits}
            error={errors[p.field.key]}
            disabled={!canWrite}
            onChange={(d) => setDraft(p.field.key, d)}
            onReset={() => setDraft(p.field.key, resetDraft(p.field))}
            onUndo={() => undo(p.field.key)}
          />
        ))}
      </div>

      <ConfirmSaveDialog
        open={confirmOpen}
        onOpenChange={(o) => {
          if (!update.isPending) setConfirmOpen(o)
        }}
        diff={diff}
        error={saveError}
        pending={update.isPending}
        onConfirm={save}
      />
    </div>
  )
}

function PropertyCard({
  property: p,
  draft,
  edited,
  error,
  disabled,
  onChange,
  onReset,
  onUndo,
}: {
  property: Property
  draft: Draft
  edited: boolean
  error?: string
  disabled: boolean
  onChange: (d: Draft) => void
  onReset: () => void
  onUndo: () => void
}) {
  const f = p.field
  const id = `mod-prop-${f.key}`
  const isSecret = f.type === "secret"
  // Offered while a value is stored; hidden once the draft already resets it.
  const draftEmpty =
    (draft.kind === "text" && draft.text.trim() === "") ||
    (draft.kind === "bool" && draft.value === null) ||
    (draft.kind === "bytes" && draft.text.trim() === "") ||
    (draft.kind === "secret" && draft.mode === "clear")
  const canReset = !disabled && p.source === "set" && !draftEmpty

  return (
    <Field
      className={cn(
        "min-w-0",
        "gap-2 rounded-xl border bg-card p-4",
        edited && "border-primary/50",
        error && "border-destructive/60",
      )}
      data-invalid={error ? true : undefined}
    >
      {f.group && (
        <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {f.group}
        </div>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <FieldLabel htmlFor={id} className="w-auto">
          {f.label}
          {f.required && <span className="text-destructive">*</span>}
        </FieldLabel>
        <code className="text-xs text-muted-foreground">{f.key}</code>
        <Badge variant={p.source === "default" ? "outline" : "secondary"}>
          {sourceLabel(p.source)}
        </Badge>
        {p.restartRequired && (
          <Badge variant="outline" className="border-amber-600/40 text-amber-700 dark:text-amber-400">
            Restart required
          </Badge>
        )}
        {p.apply === "restart" && !p.restartRequired && (
          <Badge variant="ghost" className="text-muted-foreground">
            Applies on restart
          </Badge>
        )}
        {edited && <Badge variant="default">Edited</Badge>}
        <div className="ml-auto flex items-center gap-1">
          {edited && (
            <Button type="button" variant="ghost" size="xs" onClick={onUndo}>
              Undo
            </Button>
          )}
          {canReset && (
            <Button
              type="button"
              variant="ghost"
              size="xs"
              onClick={onReset}
              title="Clear the stored value so the module's default applies"
            >
              <RotateCcwIcon />
              Reset to default
            </Button>
          )}
        </div>
      </div>
      {f.description && <FieldDescription>{f.description}</FieldDescription>}
      {p.rejectedReason && (
        <div className="flex items-start gap-2 rounded-md bg-amber-500/10 px-3 py-2 text-sm text-amber-800 dark:text-amber-300">
          <TriangleAlertIcon className="mt-0.5 size-4 shrink-0" />
          <span>The module rejected this value: {p.rejectedReason}</span>
        </div>
      )}
      <FieldControl
        field={f}
        id={id}
        draft={draft}
        onChange={onChange}
        disabled={disabled}
        invalid={!!error}
        defaultValue={isSecret ? undefined : f.default}
        secretSet={isSecret ? p.isSet : undefined}
      />
      {error && <FieldError>{error}</FieldError>}
    </Field>
  )
}

// filterProperties keeps properties whose label, key, description or group
// contains every whitespace-separated term (case-insensitive).
function filterProperties(properties: Property[], query: string): Property[] {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (terms.length === 0) return properties
  return properties.filter((p) => {
    const f = p.field
    const text = [f.label, f.key, f.description, f.group].filter(Boolean).join(" ").toLowerCase()
    return terms.every((t) => text.includes(t))
  })
}

function ConfirmSaveDialog({
  open,
  onOpenChange,
  diff,
  error,
  pending,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  diff: DiffEntry[]
  error: string | null
  pending: boolean
  onConfirm: () => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Save configuration</DialogTitle>
          <DialogDescription>
            {diff.length} {diff.length === 1 ? "change" : "changes"} to this channel's settings.
          </DialogDescription>
        </DialogHeader>
        <ul className="flex max-h-80 flex-col gap-2 overflow-y-auto">
          {diff.map((d) => (
            <li key={d.key} className="rounded-md border px-3 py-2">
              <div className="font-medium">{d.label}</div>
              <div className="mt-1 flex flex-wrap items-center gap-2 font-mono text-xs">
                <span className="break-all text-muted-foreground line-through">{d.oldText}</span>
                <span aria-hidden>→</span>
                <span className="break-all">{d.newText}</span>
              </div>
            </li>
          ))}
        </ul>
        {error && (
          <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error}</div>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button onClick={onConfirm} disabled={pending}>
            {pending ? "Saving..." : "Save"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
