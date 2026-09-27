import { useState } from "react"
import { Link } from "react-router"
import { toast } from "sonner"
import {
  ActivityIcon,
  EllipsisIcon,
  PencilIcon,
  RefreshCwIcon,
  Trash2Icon,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button, buttonVariants } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { ConfirmDialog } from "@/components/keys/confirm-dialog"
import {
  useDeleteAgentKey,
  useRegenerateAgentKey,
  useSetAgentKeyEnabled,
} from "@/graphql/hooks/agent-keys"
import { useAgentStore } from "@/stores/agents"
import { agentActivityPath } from "@/hooks/use-agent-activity-params"
import { formatAbsolute, relativeTime } from "@/lib/relative-time"
import { cn } from "@/lib/utils"
import type { AgentRow } from "@/lib/agent-rows"

/**
 * One agent: what it may reach, what it has been doing, and its controls.
 *
 * Compact on purpose. The scope summary is still the point — an operator should
 * answer "what can this thing do?" without opening the form — but the card now
 * tiles, so anything that can be a line instead of a block is a line, and the
 * three lifecycle actions live in a menu rather than a row of icon buttons.
 */
export function AgentCard({ row }: { row: AgentRow }) {
  const regenerate = useRegenerateAgentKey()
  const setEnabled = useSetAgentKeyEnabled()
  const remove = useDeleteAgentKey()
  const openEditForm = useAgentStore((s) => s.openEditForm)
  const setFreshToken = useAgentStore((s) => s.setFreshToken)
  const freshTokenKeyId = useAgentStore((s) => s.freshTokenKeyId)

  const [confirmRegen, setConfirmRegen] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  async function handleRegenerate() {
    try {
      const res = await regenerate.mutateAsync(row.id)
      setFreshToken(res.regenerateAgentKey.token, row.id)
      setConfirmRegen(false)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to regenerate agent key")
    }
  }

  async function handleToggle(enabled: boolean) {
    try {
      await setEnabled.mutateAsync({ id: row.id, enabled })
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update agent key")
    }
  }

  async function handleDelete() {
    try {
      await remove.mutateAsync(row.id)
      if (freshTokenKeyId === row.id) setFreshToken(null)
      setConfirmDelete(false)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to delete agent key")
    }
  }

  return (
    <div
      className={cn(
        "flex flex-col gap-3 rounded-md border p-3.5 transition-opacity",
        // A paused agent is still configuration worth reading, so it stays
        // legible — just visibly not running.
        !row.enabled && "opacity-70",
      )}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 space-y-0.5">
          <div className="truncate text-sm font-medium" title={row.name}>
            {row.name}
          </div>
          <div className="truncate font-mono text-xs text-muted-foreground">
            lga_{row.keyId}_…
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {row.enabled ? (
            <Badge variant="default">Enabled</Badge>
          ) : (
            <Badge variant="outline">Paused</Badge>
          )}
          <AgentMenu
            onEdit={() => openEditForm(row.id)}
            onRegenerate={() => setConfirmRegen(true)}
            onDelete={() => setConfirmDelete(true)}
            busy={regenerate.isPending || remove.isPending}
          />
        </div>
      </div>

      <div className="flex flex-wrap gap-1.5">
        <Badge variant="secondary">
          {row.maxRole === "OPERATOR" ? "Operator" : "Viewer"}
        </Badge>
        {row.canWrite ? (
          <Badge variant="secondary">Can write</Badge>
        ) : (
          <Badge
            variant="outline"
            // A viewer-capped key with writes switched on reads as misconfigured
            // rather than read-only, and the operator is the only one who can
            // tell which they meant.
            title={
              row.allowWrites
                ? "Writes are switched on, but a viewer-capped key cannot write. Raise the maximum role to operator."
                : undefined
            }
          >
            Read only{row.allowWrites && " ⚠"}
          </Badge>
        )}
        <ScopeBadges scopes={row.operationScopes} />
      </div>

      <div className="flex items-center justify-between gap-2 border-t pt-3 text-xs">
        <ActivitySummary row={row} />
        {/* Only when there is something to look at: a link into an empty feed
            is a dead end. A styled Link rather than a Button rendering one —
            Base UI's button expects a native <button>. */}
        {row.actions > 0 && (
          <Link
            to={agentActivityPath(row.id)}
            className={buttonVariants({ variant: "ghost", size: "sm" })}
          >
            <ActivityIcon className="size-3.5" />
            Activity
          </Link>
        )}
      </div>

      <div className="flex items-center gap-2 border-t pt-3">
        <Switch
          id={`agent-enabled-${row.id}`}
          checked={row.enabled}
          onCheckedChange={handleToggle}
          disabled={setEnabled.isPending}
        />
        <Label
          htmlFor={`agent-enabled-${row.id}`}
          className="text-xs text-muted-foreground"
        >
          {row.enabled ? "Active" : "Paused"}
        </Label>
        <span
          className="ml-auto text-xs text-muted-foreground/70"
          title={formatAbsolute(row.createdAt)}
        >
          created {relativeTime(row.createdAt)}
        </span>
      </div>

      <ConfirmDialog
        open={confirmRegen}
        title={`Regenerate "${row.name}"?`}
        description="The current token stops working immediately. Any agent using it will need the new one."
        confirmLabel={regenerate.isPending ? "Regenerating…" : "Regenerate"}
        onCancel={() => setConfirmRegen(false)}
        onConfirm={handleRegenerate}
        disabled={regenerate.isPending}
        destructive
      />
      <ConfirmDialog
        open={confirmDelete}
        title={`Delete "${row.name}"?`}
        description="The token stops working immediately and the key cannot be recovered. Everything the agent already did stays on the timeline."
        confirmLabel={remove.isPending ? "Deleting…" : "Delete"}
        onCancel={() => setConfirmDelete(false)}
        onConfirm={handleDelete}
        disabled={remove.isPending}
        destructive
      />
    </div>
  )
}

function AgentMenu({
  onEdit,
  onRegenerate,
  onDelete,
  busy,
}: {
  onEdit: () => void
  onRegenerate: () => void
  onDelete: () => void
  busy: boolean
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button variant="ghost" size="icon-sm" disabled={busy} />}
      >
        <EllipsisIcon className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        <DropdownMenuItem onClick={onEdit}>
          <PencilIcon className="size-4" />
          Edit scope
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onRegenerate}>
          <RefreshCwIcon className="size-4" />
          Regenerate token
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onClick={onDelete}>
          <Trash2Icon className="size-4" />
          Delete
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/**
 * What this agent has actually done, as one line. Never-used is called out
 * rather than shown as an empty count: a key nothing ever authenticated with is
 * usually one that was created and forgotten, which is worth noticing.
 */
function ActivitySummary({ row }: { row: AgentRow }) {
  if (row.neverUsed) {
    return <span className="text-muted-foreground/70">Never used</span>
  }

  return (
    <span className="min-w-0 truncate text-muted-foreground">
      {row.actions === 0 ? (
        "No calls yet"
      ) : (
        <>
          {row.actions.toLocaleString()} {row.actions === 1 ? "call" : "calls"}
          {row.operationsTouched > 1 && ` · ${row.operationsTouched} ops`}
        </>
      )}
      {row.lastSeen && (
        <span title={formatAbsolute(row.lastSeen)}>
          {" · "}
          {relativeTime(row.lastSeen)}
        </span>
      )}
    </span>
  )
}

// An empty scope list is the widest setting, not the narrowest — it tracks
// membership. Saying so explicitly avoids reading "no operations" as "none".
function ScopeBadges({ scopes }: { scopes: AgentRow["operationScopes"] }) {
  if (scopes.length === 0) {
    return <Badge variant="outline">All my operations</Badge>
  }
  const shown = scopes.slice(0, 2)
  return (
    <>
      {shown.map((op) => (
        <Badge key={op.id} variant="outline" className="max-w-40 truncate">
          {op.name}
        </Badge>
      ))}
      {scopes.length > shown.length && (
        <Badge
          variant="outline"
          title={scopes.map((op) => op.name).join(", ")}
        >
          +{scopes.length - shown.length}
        </Badge>
      )}
    </>
  )
}
