import { useMemo, useState } from "react"
import { RotateCcwIcon, TriangleAlertIcon } from "lucide-react"
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
import type { ModuleConfigScope, ModuleDetailFieldsFragment } from "@/graphql/gql/graphql"
import { useUpdateModuleConfig } from "@/graphql/hooks/modules"
import { cn } from "@/lib/utils"
import { errorMessage, graphqlErrorCode } from "./api-errors"
import { FieldControl } from "./field-control"
import {
  emptyDraft,
  evaluateForm,
  fallbackValue,
  formatValue,
  groupByField,
  initialDraft,
  scopeValue,
  secretSetAtScope,
  type DiffEntry,
  type Draft,
} from "./field-values"
import { scopeLabel, sourceLabel } from "./labels"

type Management = NonNullable<ModuleDetailFieldsFragment["management"]>
type Property = Management["properties"][number]

interface ConfigTabProps {
  module: ModuleDetailFieldsFragment
  management: Management
  canWrite: boolean
  refetch: () => unknown
}

const SCOPES: ModuleConfigScope[] = ["MODULE", "INSTANCE"]

export function ConfigTab({ module, management, canWrite, refetch }: ConfigTabProps) {
  const [scope, setScope] = useState<ModuleConfigScope>("MODULE")
  // Only what the user touched, per scope. Untouched fields render from the
  // latest server data, so a refetch (after a conflict, or another admin's
  // save arriving via the subscription) updates them without losing edits.
  const [edits, setEdits] = useState<Record<ModuleConfigScope, Record<string, Draft>>>({
    MODULE: {},
    INSTANCE: {},
  })
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const update = useUpdateModuleConfig()

  const properties = management.properties
  const scopeEdits = edits[scope]

  const drafts = useMemo(() => {
    const out: Record<string, Draft> = {}
    for (const p of properties) {
      out[p.field.key] = scopeEdits[p.field.key] ?? initialDraft(p.field, scopeValue(p, scope))
    }
    return out
  }, [properties, scopeEdits, scope])

  const { diff, errors } = useMemo(
    () => evaluateForm(properties, drafts, scope),
    [properties, drafts, scope],
  )
  const groups = useMemo(() => groupByField(properties, (p) => p.field), [properties])
  const dirty = Object.keys(scopeEdits).length > 0
  const errorCount = Object.keys(errors).length

  function setDraft(key: string, draft: Draft) {
    setEdits((prev) => ({ ...prev, [scope]: { ...prev[scope], [key]: draft } }))
  }

  function resetProperty(p: Property) {
    setDraft(
      p.field.key,
      p.field.type === "secret"
        ? { kind: "secret", mode: "clear", text: "" }
        : emptyDraft(p.field),
    )
  }

  function discard() {
    setEdits((prev) => ({ ...prev, [scope]: {} }))
  }

  async function save() {
    setSaveError(null)
    const expectedRevision =
      scope === "MODULE" ? management.moduleRevision : management.instanceRevision
    try {
      const res = await update.mutateAsync({
        instance: module.instance,
        scope,
        expectedRevision,
        values: diff.map((d) => ({ key: d.key, value: d.new })),
      })
      setEdits((prev) => ({ ...prev, [scope]: {} }))
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
        <div className="flex flex-col gap-1">
          <div
            role="radiogroup"
            aria-label="Configuration scope"
            className="inline-flex h-9 w-fit items-center rounded-md bg-muted p-1 text-muted-foreground"
          >
            {SCOPES.map((s) => (
              <button
                key={s}
                type="button"
                role="radio"
                aria-checked={scope === s}
                onClick={() => {
                  setScope(s)
                  setSaveError(null)
                }}
                className={cn(
                  "inline-flex h-7 items-center rounded-sm px-3 text-sm font-medium whitespace-nowrap transition-colors",
                  scope === s && "bg-background text-foreground shadow-sm",
                )}
              >
                {scopeLabel(s, module.name)}
                {Object.keys(edits[s]).length > 0 && (
                  <span className="ml-1.5 size-1.5 rounded-full bg-primary" aria-label="unsaved changes" />
                )}
              </button>
            ))}
          </div>
          <p className="text-xs text-muted-foreground">
            {scope === "MODULE"
              ? `Values set here apply to every ${module.name || "module"} instance unless an instance overrides them. Revision ${management.moduleRevision}.`
              : `Overrides for ${module.instance} only. Revision ${management.instanceRevision}.`}
          </p>
        </div>
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

      {groups.map(({ group, items }) => (
        <section key={group} className="rounded-xl border">
          <h3 className="border-b px-4 py-2 text-sm font-semibold">{group}</h3>
          <div className="divide-y">
            {items.map((p) => (
              <PropertyRow
                key={p.field.key}
                property={p}
                scope={scope}
                moduleName={module.name}
                draft={drafts[p.field.key]}
                edited={p.field.key in scopeEdits}
                error={errors[p.field.key]}
                disabled={!canWrite}
                onChange={(d) => setDraft(p.field.key, d)}
                onReset={() => resetProperty(p)}
                onUndo={() =>
                  setEdits((prev) => {
                    const next = { ...prev[scope] }
                    delete next[p.field.key]
                    return { ...prev, [scope]: next }
                  })
                }
              />
            ))}
          </div>
        </section>
      ))}

      <ConfirmSaveDialog
        open={confirmOpen}
        onOpenChange={(o) => {
          if (!update.isPending) setConfirmOpen(o)
        }}
        diff={diff}
        scopeText={scopeLabel(scope, module.name)}
        instance={module.instance}
        error={saveError}
        pending={update.isPending}
        onConfirm={save}
      />
    </div>
  )
}

function PropertyRow({
  property: p,
  scope,
  moduleName,
  draft,
  edited,
  error,
  disabled,
  onChange,
  onReset,
  onUndo,
}: {
  property: Property
  scope: ModuleConfigScope
  moduleName: string
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
  const setHere = isSecret ? secretSetAtScope(p, scope) : scopeValue(p, scope) !== null
  // "Set here" from the draft's point of view, so reset hides once used.
  const draftEmpty =
    (draft.kind === "text" && draft.text.trim() === "") ||
    (draft.kind === "bool" && draft.value === null) ||
    (draft.kind === "bytes" && draft.text.trim() === "") ||
    (draft.kind === "secret" && draft.mode === "clear")
  const canReset = !disabled && (setHere || edited) && !draftEmpty
  const sourceIsHere = p.source === (scope === "MODULE" ? "module" : "instance")

  return (
    <Field className="gap-2 px-4 py-3" data-invalid={error ? true : undefined}>
      <div className="flex flex-wrap items-center gap-2">
        <FieldLabel htmlFor={id} className="w-auto">
          {f.label}
          {f.required && <span className="text-destructive">*</span>}
        </FieldLabel>
        <code className="text-xs text-muted-foreground">{f.key}</code>
        <Badge variant={p.source === "default" ? "outline" : "secondary"}>
          {sourceLabel(p.source, moduleName)}
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
              title={`Clear the value set for ${scope === "MODULE" ? "all instances" : "this instance"}`}
            >
              <RotateCcwIcon />
              Reset
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
        inherited={fallbackValue(p, scope)}
        secretSet={isSecret ? p.isSet : undefined}
      />
      {error && <FieldError>{error}</FieldError>}
      {!isSecret && !sourceIsHere && (
        <p className="text-xs text-muted-foreground">
          Effective: {formatValue(f.type, p.effective, f.unit)}
          {p.source !== "default" && ` (from ${sourceLabel(p.source, moduleName).toLowerCase()})`}
        </p>
      )}
    </Field>
  )
}

function ConfirmSaveDialog({
  open,
  onOpenChange,
  diff,
  scopeText,
  instance,
  error,
  pending,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  diff: DiffEntry[]
  scopeText: string
  instance: string
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
            {diff.length} {diff.length === 1 ? "change" : "changes"} for{" "}
            <span className="font-medium text-foreground">{scopeText}</span>
            {scopeText === "This instance only" && (
              <>
                {" "}
                (<span className="font-medium text-foreground">{instance}</span>)
              </>
            )}
            .
          </DialogDescription>
        </DialogHeader>
        <ul className="flex max-h-80 flex-col gap-2 overflow-y-auto">
          {diff.map((d) => (
            <li key={d.key} className="rounded-md border px-3 py-2">
              <div className="font-medium">{d.label}</div>
              <div className="mt-1 flex flex-wrap items-center gap-2 font-mono text-xs">
                <span className="break-all text-muted-foreground line-through">{d.oldText}</span>
                <span aria-hidden>→</span>
                <span className="break-all">
                  {d.new === null ? `${d.newText} (reset)` : d.newText}
                </span>
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
