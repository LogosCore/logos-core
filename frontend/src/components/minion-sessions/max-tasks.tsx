import { useState } from "react"
import { PencilIcon } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { errorMessage } from "@/components/modules/management/api-errors"
import type { MinionSessionFieldsFragment } from "@/graphql/gql/graphql"
import { useMinionSessionRunningTasks, useSetMinionSessionMaxTasks } from "@/graphql/hooks/minion-sessions"
import { MAX_TASKS_LIMIT, parseMaxTasks } from "./max-tasks-values"

// A session's tasks at once (docs: contracts/minion-messages/tasks-and-results,
// Max tasks): how many it runs and its limit, as the minion last said. Where
// the build's factory declares max tasks the limit is the operators': Change
// sets it (0 pauses the minion: no new tasks, what it runs goes on), and it
// shows on its way (core sends it with every message to the minion,
// whatever it runs, until the minion says it has it) or not taken (5 sent,
// none answered). Elsewhere its factory sets it: shown when the minion says
// it, never changed.

export function MaxTasksValue({ s }: { s: MinionSessionFieldsFragment }) {
  const counts = useMinionSessionRunningTasks(s.id, s.rev).data?.minionSession
  const running = counts?.runningTasks
  const editable = s.maxTasksSettable && s.state !== "CLOSED" && s.state !== "RESERVED"
  // Tunnels (SOCKS proxies, port forwards) run beside the limit: they hold
  // no place in it.
  const tunnels = counts?.runningTunnels ?? 0
  const runningText =
    running != null ? `${running} running${tunnels > 0 ? ` · ${tunnels} ${tunnels === 1 ? "tunnel" : "tunnels"} beside the limit` : ""}` : null
  if (s.maxTasks == null) {
    return (
      <span className="tabular-nums">
        {runningText && `${runningText} · `}
        <span className="text-muted-foreground" title="The minion did not say how many tasks it runs at once">
          max not reported
        </span>
      </span>
    )
  }
  return (
    <span className="inline-flex flex-wrap items-baseline gap-x-2">
      <span className="tabular-nums">
        {runningText && `${runningText} · `}max {s.maxTasks}
        {s.maxTasks === 0 && <span className="text-muted-foreground"> (paused: it takes no new tasks)</span>}
        {!s.maxTasksSettable && (
          <span className="text-muted-foreground" title="Its factory sets how many tasks its minions run at once: it cannot be changed">
            {" "}
            · set by its factory
          </span>
        )}
      </span>
      {s.maxTasksPending != null && (
        <span className="text-amber-700 dark:text-amber-400" title="Core sends it with every message to the minion until the minion says it has it">
          → {s.maxTasksPending}, on its way
        </span>
      )}
      {s.maxTasksNotTaken != null && (
        <span
          className="text-red-600 dark:text-red-400"
          title="Core sent it with 5 messages and the minion never said it has it. Set it again to try again."
        >
          → {s.maxTasksNotTaken}: the minion did not take it
        </span>
      )}
      {editable && <ChangeMaxTasks s={s} />}
    </span>
  )
}

function ChangeMaxTasks({ s }: { s: MinionSessionFieldsFragment }) {
  const [open, setOpen] = useState(false)
  const [text, setText] = useState("")
  const set = useSetMinionSessionMaxTasks()
  const parsed = parseMaxTasks(text)
  const id = s.id
  const submit = () => {
    if (!parsed.ok) return
    const max = parsed.value
    set.mutate(
      { id, max },
      {
        onSuccess: () => {
          setOpen(false)
          toast.success(max === 0 ? "Pausing the minion's tasks" : `Max tasks ${max}: on its way to the minion`)
        },
        onError: (err) => toast.error(errorMessage(err, "Could not set it")),
      },
    )
  }
  return (
    <Popover
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (o) setText(String(s.maxTasksPending ?? s.maxTasks ?? ""))
      }}
    >
      <PopoverTrigger
        render={
          <Button variant="ghost" size="icon-xs" className="self-center" title="Change how many tasks it runs at once" aria-label="Change max tasks">
            <PencilIcon />
          </Button>
        }
      />
      <PopoverContent align="start" className="w-72">
        <form
          className="flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            submit()
          }}
        >
          <label htmlFor={`max-tasks-${id}`} className="text-sm font-medium">
            Tasks at once
          </label>
          <div className="flex items-center gap-2">
            <Input
              id={`max-tasks-${id}`}
              value={text}
              onChange={(e) => setText(e.target.value)}
              inputMode="numeric"
              className="w-20"
              aria-invalid={!parsed.ok && text !== ""}
              autoFocus
            />
            <Button type="submit" size="sm" disabled={!parsed.ok || set.isPending}>
              Set
            </Button>
          </div>
          <p className={!parsed.ok && text !== "" ? "text-xs text-red-600 dark:text-red-400" : "text-xs text-muted-foreground"}>
            {!parsed.ok && text !== ""
              ? parsed.error
              : `0 to ${MAX_TASKS_LIMIT}. 0 pauses it: no new tasks, what it runs goes on. It reaches the minion with core's next message, whatever it runs.`}
          </p>
        </form>
      </PopoverContent>
    </Popover>
  )
}
