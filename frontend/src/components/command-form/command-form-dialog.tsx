import { useState, type KeyboardEvent, type ReactNode } from "react"
import { Loader2Icon, PlayIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { errorMessage } from "@/components/modules/management/api-errors"
import { ParamFields } from "@/components/modules/management/param-fields"
import { evaluateParams, initialParamDrafts, type Draft, type FieldDef } from "@/components/modules/management/field-values"

// The app's one form for running something with typed params: a session's
// command (the console's Shift+Enter, or its form button), a module's
// action. It is the fields, checked as they change, and a Run: what is
// around them is its owner's (a command picker above, a confirmation or an
// equivalent command line below). Enter runs (Ctrl+Enter from a text
// area), Esc closes. Each opening starts from initialDrafts.

export interface CommandFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  /** Above the fields: a command picker, say. */
  before?: ReactNode
  fields: readonly FieldDef[]
  /** Where the fields start; the fields' defaults when absent. */
  initialDrafts?: Record<string, Draft>
  /** Fields whose problems show from the start (what was typed for them);
   *  the others' show once Run is tried. */
  showErrorsFor?: readonly string[]
  /** Below the fields, given the values as they stand (null while any is
   *  wrong): a confirmation, an equivalent command line. */
  after?: (values: Record<string, unknown> | null) => ReactNode
  submitLabel?: string
  destructive?: boolean
  /** Runs it. The dialog closes when it resolves, and shows why when it
   *  throws. */
  onSubmit: (values: Record<string, unknown>) => Promise<unknown> | void
  /** For the fields' ids. */
  idPrefix: string
  /** A new key starts the form over (another command picked). */
  formKey?: string
  className?: string
}

export function CommandFormDialog(props: CommandFormDialogProps) {
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className={props.className ?? "sm:max-w-lg"}>
        {/* Keyed so each opening starts from its drafts; the dialog drops
            it once closed. */}
        <CommandForm key={props.formKey ?? "form"} {...props} />
      </DialogContent>
    </Dialog>
  )
}

function CommandForm({
  onOpenChange,
  title,
  description,
  before,
  fields,
  initialDrafts,
  showErrorsFor,
  after,
  submitLabel = "Run",
  destructive,
  onSubmit,
  idPrefix,
}: CommandFormDialogProps) {
  const [drafts, setDrafts] = useState<Record<string, Draft>>(() => initialDrafts ?? initialParamDrafts(fields))
  const [tried, setTried] = useState(false)
  // Fields changed since opening show their problems as they are typed.
  const [touched, setTouched] = useState<ReadonlySet<string>>(() => new Set(showErrorsFor ?? []))
  const [pending, setPending] = useState(false)
  const [failure, setFailure] = useState<string | null>(null)
  const { values, errors } = evaluateParams(fields, drafts)
  const valid = Object.keys(errors).length === 0
  const shown = tried ? errors : Object.fromEntries(Object.entries(errors).filter(([k]) => touched.has(k)))

  const submit = async () => {
    setTried(true)
    if (!valid || pending) return
    setPending(true)
    setFailure(null)
    try {
      await onSubmit(values)
      onOpenChange(false)
    } catch (err) {
      setFailure(errorMessage(err, "Could not run it"))
      setPending(false)
    }
  }

  // Ctrl+Enter runs from anywhere in the form, a text area too (where
  // Enter is a new line); Enter in an input runs by submitting the form.
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      void submit()
    }
  }

  return (
    <form
      className="flex min-w-0 flex-col gap-4"
      onKeyDown={onKeyDown}
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        {description && <DialogDescription>{description}</DialogDescription>}
      </DialogHeader>
      {before}
      {fields.length > 0 && (
        <ParamFields
          params={fields}
          drafts={drafts}
          onChange={(key, d) => {
            setDrafts((prev) => ({ ...prev, [key]: d }))
            setTouched((prev) => (prev.has(key) ? prev : new Set([...prev, key])))
          }}
          errors={shown}
          idPrefix={idPrefix}
          disabled={pending}
        />
      )}
      {after?.(valid ? values : null)}
      {failure && <p className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">{failure}</p>}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
          Cancel
        </Button>
        <Button type="submit" variant={destructive ? "destructive" : "default"} disabled={pending || (tried && !valid)}>
          {pending ? <Loader2Icon className="animate-spin" /> : <PlayIcon />}
          {submitLabel}
        </Button>
      </DialogFooter>
    </form>
  )
}
