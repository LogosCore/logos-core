import { toast } from "sonner"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { AgentKeyForm, type AgentKeyFormValues } from "@/components/keys/agent-key-form"
import { useCreateAgentKey, useUpdateAgentKey } from "@/graphql/hooks/agent-keys"
import { useAgentStore } from "@/stores/agents"
import type { AgentRow } from "@/lib/agent-rows"

/**
 * Create and edit in a panel beside the grid.
 *
 * One sheet for both, because the fields are the same and the only difference is
 * which mutation runs. A sheet rather than replacing the grid contents: an
 * operator editing one agent's scope is usually comparing it against another,
 * and taking the list off screen to do it is what made the old dialog awkward.
 */
export function AgentFormSheet({ rows }: { rows: AgentRow[] }) {
  const formTarget = useAgentStore((s) => s.formTarget)
  const closeForm = useAgentStore((s) => s.closeForm)
  const setFreshToken = useAgentStore((s) => s.setFreshToken)

  const create = useCreateAgentKey()
  const update = useUpdateAgentKey()

  const editing =
    formTarget?.mode === "edit"
      ? rows.find((row) => row.id === formTarget.id) ?? null
      : null

  // The target can vanish under us — another session deleting the key, or a
  // refetch dropping it. Closing beats rendering a form bound to nothing.
  const open = formTarget?.mode === "create" || editing != null

  async function handleCreate(values: AgentKeyFormValues) {
    try {
      const res = await create.mutateAsync({
        name: values.name,
        // null ⇒ omit, which the server reads as "every operation the owner
        // belongs to".
        operationScopes: values.operationScopes ?? undefined,
        maxRole: values.maxRole,
        allowWrites: values.allowWrites,
      })
      setFreshToken(res.createAgentKey.token, res.createAgentKey.agentKey.id)
      closeForm()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to create agent key")
    }
  }

  async function handleUpdate(id: string, values: AgentKeyFormValues) {
    try {
      await update.mutateAsync({
        id,
        input: {
          name: values.name,
          // null means "all my operations"; the server reads an empty list the
          // same way, and omitting the field would leave the old scope in
          // place — so the widening case must send [] explicitly.
          operationScopes: values.operationScopes ?? [],
          maxRole: values.maxRole,
          allowWrites: values.allowWrites,
        },
      })
      closeForm()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update agent key")
    }
  }

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        if (!next) closeForm()
      }}
    >
      {/* Important, not just a wider utility: SheetContent's own width is set
          behind a data-side selector, which outranks a plain sm:max-w-* on
          specificity and would silently keep the narrow default. */}
      <SheetContent className="w-full overflow-y-auto sm:max-w-lg!">
        <SheetHeader>
          <SheetTitle>{editing ? "Edit agent" : "New agent"}</SheetTitle>
          <SheetDescription>
            {editing
              ? "Changes take effect on the agent's next call. The token is unchanged."
              : "Each agent acts on your behalf under a ceiling you set, is revocable on its own, and is attributed separately everywhere it acts."}
          </SheetDescription>
        </SheetHeader>
        <div className="px-4 pb-6">
          {/* Keyed on the target so switching from one agent to another — or
              from edit to create — rebuilds the uncontrolled field state
              instead of carrying the previous agent's values over. */}
          {editing ? (
            <AgentKeyForm
              key={editing.id}
              initial={{
                name: editing.name,
                operationScopes:
                  editing.operationScopes.length === 0
                    ? null
                    : editing.operationScopes.map((op) => op.id),
                maxRole: editing.maxRole,
                allowWrites: editing.allowWrites,
              }}
              submitLabel="Save"
              pending={update.isPending}
              onSubmit={(values) => handleUpdate(editing.id, values)}
              onCancel={closeForm}
            />
          ) : (
            <AgentKeyForm
              key="create"
              submitLabel="Create agent"
              pending={create.isPending}
              onSubmit={handleCreate}
              onCancel={closeForm}
            />
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
