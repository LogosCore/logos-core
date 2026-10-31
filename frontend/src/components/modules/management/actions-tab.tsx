import { Fragment, useState } from "react"
import { ChevronDownIcon, ChevronRightIcon, Loader2Icon, PlayIcon } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { FormattedDateTimeText } from "@/components/ui/formatted-date-time-text"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import type {
  ModuleActionInvocationFieldsFragment,
  ModuleActionStatus,
  ModuleDetailFieldsFragment,
} from "@/graphql/gql/graphql"
import {
  useInvokeModuleAction,
  useModuleActionInvocations,
} from "@/graphql/hooks/modules"
import { PLACEHOLDER_DELAY_MS, useDelayedFlag } from "@/hooks/use-delayed-flag"
import { actionBlocker } from "./action-blocker"
import { errorMessage, graphqlErrorCode } from "./api-errors"
import {
  durationMs,
  evaluateParams,
  formatElapsed,
  initialParamDrafts,
  prettyJson,
  type Draft,
} from "./field-values"
import { actorLabel } from "./labels"
import { ParamFields } from "./param-fields"

type Management = NonNullable<ModuleDetailFieldsFragment["management"]>
type Action = Management["actions"][number]
type Invocation = ModuleActionInvocationFieldsFragment

type RunState =
  | { state: "running" }
  | { state: "done"; invocation: Invocation }
  | { state: "error"; message: string }

interface ActionsTabProps {
  module: ModuleDetailFieldsFragment
  management: Management
  canInvoke: boolean
  canInvokeHigh: boolean
}

export function ActionsTab({ module, management, canInvoke, canInvokeHigh }: ActionsTabProps) {
  const invoke = useInvokeModuleAction()
  const [runs, setRuns] = useState<Record<string, RunState>>({})
  const [dialogAction, setDialogAction] = useState<Action | null>(null)
  const registered = module.status === "registered"

  async function run(action: Action, params: Record<string, unknown> | null) {
    setRuns((r) => ({ ...r, [action.name]: { state: "running" } }))
    try {
      const res = await invoke.mutateAsync({
        instance: module.instance,
        action: action.name,
        params,
      })
      const inv = res.invokeModuleAction
      setRuns((r) => ({ ...r, [action.name]: { state: "done", invocation: inv } }))
      if (inv.status === "SUCCEEDED") toast.success(`${action.label} succeeded`)
      else if (inv.status === "RUNNING") toast.info(`${action.label} is still running`)
      else
        toast.error(`${action.label} ${inv.status === "TIMED_OUT" ? "timed out" : "failed"}`, {
          description: inv.error ?? undefined,
        })
    } catch (err) {
      const message =
        graphqlErrorCode(err) === "FORBIDDEN"
          ? "You don't have permission to run this action"
          : errorMessage(err, "Failed to run action")
      setRuns((r) => ({ ...r, [action.name]: { state: "error", message } }))
      toast.error(`${action.label} failed`, { description: message })
    }
  }

  function start(action: Action) {
    const needsDialog = action.params.length > 0 || action.risk === "high" || !!action.confirm
    if (needsDialog) setDialogAction(action)
    else void run(action, null)
  }

  return (
    <div className="flex flex-col gap-6">
      {management.actions.length === 0 ? (
        <p className="py-4 text-center text-sm text-muted-foreground">
          This module declares no actions.
        </p>
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {management.actions.map((a) => {
            const high = a.risk === "high"
            const runState = runs[a.name]
            const running = runState?.state === "running"
            const reason = actionBlocker(a, { canInvoke, canInvokeHigh, registered })
            return (
              <Card key={a.name} size="sm">
                <CardHeader>
                  <CardTitle>{a.label}</CardTitle>
                  {a.description && <CardDescription>{a.description}</CardDescription>}
                  <CardAction>
                    <Badge variant={high ? "destructive" : "secondary"}>
                      {high ? "High risk" : "Low risk"}
                    </Badge>
                  </CardAction>
                </CardHeader>
                <CardContent className="flex flex-col gap-3">
                  <div className="flex items-center gap-2">
                    <Button
                      size="sm"
                      variant={high ? "destructive" : "default"}
                      onClick={() => start(a)}
                      disabled={reason !== undefined || running}
                      title={reason}
                    >
                      {running ? <Loader2Icon className="animate-spin" /> : <PlayIcon />}
                      {running ? "Running…" : a.params.length > 0 ? "Run…" : "Run"}
                    </Button>
                    <span className="text-xs text-muted-foreground">
                      {reason ?? `Times out after ${a.timeoutSeconds}s`}
                    </span>
                  </div>
                  {runState && runState.state !== "running" && <RunResult run={runState} />}
                </CardContent>
              </Card>
            )
          })}
        </div>
      )}

      <ActionDialog
        action={dialogAction}
        instance={module.instance}
        onClose={() => setDialogAction(null)}
        onRun={(action, params) => {
          setDialogAction(null)
          void run(action, params)
        }}
      />

      <InvocationsTable instance={module.instance} actions={management.actions} />
    </div>
  )
}

function RunResult({ run }: { run: Exclude<RunState, { state: "running" }> }) {
  if (run.state === "error") {
    return (
      <div className="rounded-md bg-destructive/15 p-2 text-sm text-destructive">{run.message}</div>
    )
  }
  const inv = run.invocation
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-2 text-xs">
        <InvocationStatusBadge status={inv.status} />
        {(() => {
          const ms = durationMs(inv.startedAt, inv.finishedAt)
          return ms !== null ? <span className="text-muted-foreground">{formatElapsed(ms)}</span> : null
        })()}
      </div>
      {inv.error && (
        <div className="rounded-md bg-destructive/15 p-2 text-sm text-destructive">{inv.error}</div>
      )}
      {inv.result !== null && inv.result !== undefined && <JsonBlock value={inv.result} />}
    </div>
  )
}

function JsonBlock({ value }: { value: unknown }) {
  return (
    <pre className="max-h-64 overflow-auto rounded-md bg-muted p-2 font-mono text-xs whitespace-pre-wrap break-all">
      {prettyJson(value)}
    </pre>
  )
}

const STATUS_BADGE: Record<ModuleActionStatus, { label: string; className: string }> = {
  RUNNING: { label: "Running", className: "border-blue-600/40 text-blue-700 dark:text-blue-400" },
  SUCCEEDED: { label: "Succeeded", className: "border-green-600/40 text-green-700 dark:text-green-400" },
  FAILED: { label: "Failed", className: "border-red-600/40 text-red-700 dark:text-red-400" },
  TIMED_OUT: { label: "Timed out", className: "border-amber-600/40 text-amber-700 dark:text-amber-400" },
}

export function InvocationStatusBadge({ status }: { status: ModuleActionStatus }) {
  const s = STATUS_BADGE[status] ?? STATUS_BADGE.FAILED
  return (
    <Badge variant="outline" className={s.className}>
      {status === "RUNNING" && <Loader2Icon className="animate-spin" />}
      {s.label}
    </Badge>
  )
}

function ActionDialog({
  action,
  instance,
  onClose,
  onRun,
}: {
  action: Action | null
  instance: string
  onClose: () => void
  onRun: (action: Action, params: Record<string, unknown> | null) => void
}) {
  return (
    <Dialog open={action !== null} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        {/* Keyed so each opening starts from the action's defaults. */}
        {action && (
          <ActionDialogBody key={action.name} action={action} instance={instance} onClose={onClose} onRun={onRun} />
        )}
      </DialogContent>
    </Dialog>
  )
}

function ActionDialogBody({
  action,
  instance,
  onClose,
  onRun,
}: {
  action: Action
  instance: string
  onClose: () => void
  onRun: (action: Action, params: Record<string, unknown> | null) => void
}) {
  const [drafts, setDrafts] = useState<Record<string, Draft>>(() => initialParamDrafts(action.params))
  const [submitted, setSubmitted] = useState(false)
  const high = action.risk === "high"
  // A high-risk action, or one with a confirm prompt, opens to be confirmed;
  // others open only for their params.
  const confirming = high || !!action.confirm
  const { values, errors } = evaluateParams(action.params, drafts)
  const hasErrors = Object.keys(errors).length > 0

  function submit() {
    setSubmitted(true)
    if (hasErrors) return
    onRun(action, action.params.length > 0 ? values : null)
  }

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        submit()
      }}
    >
      <DialogHeader>
        <DialogTitle>{action.label}</DialogTitle>
        <DialogDescription>
          {action.description || `Run ${action.name} on ${instance}.`}
        </DialogDescription>
      </DialogHeader>

      {action.params.length > 0 && (
        <ParamFields
          params={action.params}
          drafts={drafts}
          onChange={(key, d) => setDrafts((prev) => ({ ...prev, [key]: d }))}
          errors={submitted ? errors : {}}
          idPrefix={`action-${action.name}`}
        />
      )}

      {confirming && (
        <div
          className={
            high
              ? "rounded-md bg-destructive/10 p-3 text-sm text-destructive"
              : "rounded-md bg-muted p-3 text-sm"
          }
        >
          {action.confirm || "This is a high-risk action."}
        </div>
      )}

      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" variant={high ? "destructive" : "default"} disabled={submitted && hasErrors}>
          {confirming ? "Confirm" : "Run"}
        </Button>
      </DialogFooter>
    </form>
  )
}

function InvocationsTable({ instance, actions }: { instance: string; actions: readonly Action[] }) {
  const { data, isLoading } = useModuleActionInvocations(instance)
  const slowLoad = useDelayedFlag(isLoading, PLACEHOLDER_DELAY_MS)
  const [expanded, setExpanded] = useState<string | null>(null)
  const invocations = data?.moduleActionInvocations ?? []
  const labelOf = (name: string) => actions.find((a) => a.name === name)?.label ?? name

  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-sm font-semibold">Recent invocations</h3>
      <div className="rounded-xl border bg-card">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-8" />
              <TableHead>Time</TableHead>
              <TableHead>Action</TableHead>
              <TableHead>Actor</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Duration</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {/* Like the other tables: empty while loading, "Loading…" once it is slow. */}
            {isLoading ? (
              slowLoad && (
                <TableRow className="animate-in duration-200 fade-in-0">
                  <TableCell colSpan={6} className="py-6 text-center text-muted-foreground">
                    Loading…
                  </TableCell>
                </TableRow>
              )
            ) : invocations.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="py-6 text-center text-muted-foreground">
                  No actions have been run on this instance.
                </TableCell>
              </TableRow>
            ) : (
              invocations.map((inv) => {
                const open = expanded === inv.id
                const ms = durationMs(inv.startedAt, inv.finishedAt)
                return (
                  <Fragment key={inv.id}>
                    <TableRow
                      className="cursor-pointer"
                      onClick={() => setExpanded(open ? null : inv.id)}
                      aria-expanded={open}
                    >
                      <TableCell>
                        {open ? <ChevronDownIcon className="size-4" /> : <ChevronRightIcon className="size-4" />}
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        <FormattedDateTimeText date={inv.startedAt} />
                      </TableCell>
                      <TableCell>{labelOf(inv.action)}</TableCell>
                      <TableCell>{actorLabel(inv.actor)}</TableCell>
                      <TableCell>
                        <InvocationStatusBadge status={inv.status} />
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {ms !== null ? formatElapsed(ms) : "—"}
                      </TableCell>
                    </TableRow>
                    {open && (
                      <TableRow className="hover:bg-transparent">
                        <TableCell />
                        <TableCell colSpan={5} className="whitespace-normal">
                          <InvocationDetails invocation={inv} />
                        </TableCell>
                      </TableRow>
                    )}
                  </Fragment>
                )
              })
            )}
          </TableBody>
        </Table>
      </div>
    </section>
  )
}

export function InvocationDetails({ invocation: inv }: { invocation: Invocation }) {
  return (
    <div className="grid grid-cols-1 gap-3 py-1 lg:grid-cols-2">
      <div className="flex flex-col gap-1">
        <span className="text-xs font-medium text-muted-foreground">Params</span>
        {inv.params === null || inv.params === undefined ? (
          <span className="text-sm text-muted-foreground">—</span>
        ) : (
          <JsonBlock value={inv.params} />
        )}
      </div>
      <div className="flex flex-col gap-1">
        <span className="text-xs font-medium text-muted-foreground">
          {inv.error ? "Error" : "Result"}
        </span>
        {inv.error ? (
          <div className="rounded-md bg-destructive/15 p-2 text-sm text-destructive">{inv.error}</div>
        ) : inv.result === null || inv.result === undefined ? (
          <span className="text-sm text-muted-foreground">—</span>
        ) : (
          <JsonBlock value={inv.result} />
        )}
      </div>
    </div>
  )
}
