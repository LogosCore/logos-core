import { useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { KindName } from "@/components/modules/module-icon"
import { errorMessage } from "@/components/modules/management/api-errors"
import { nameError, problemsFromError } from "@/components/endpoints/endpoint-values"
import { useCreateChannelEndpoint } from "@/graphql/hooks/channel-endpoints"
import { endpointFromConnection, type ChannelKind, type ConnectionDraft } from "./build-values"

/**
 * Saves a connection's settings as a new endpoint of the operation, to fill
 * connections from later. Nothing links the two afterwards. It stays on the
 * build form: the form's own state is not touched.
 */
export function SaveEndpointDialog({
  open,
  onClose,
  operationId,
  factory,
  channel,
  channelIcon,
  connection,
  index,
  taken,
}: {
  open: boolean
  onClose: () => void
  operationId: string
  /** The factory kind, whose parameters stay with the build. */
  factory: string
  channel: ChannelKind | undefined
  channelIcon?: string
  connection: ConnectionDraft | undefined
  index: number
  /** The operation's endpoint names: the new one must be free. */
  taken: readonly string[]
}) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        {/* Keyed so each opening starts from the connection as it is then. */}
        {open && channel && connection && (
          <SaveBody
            key={connection.key}
            operationId={operationId}
            factory={factory}
            channel={channel}
            channelIcon={channelIcon}
            connection={connection}
            index={index}
            taken={taken}
            onClose={onClose}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

function SaveBody({
  operationId,
  factory,
  channel,
  channelIcon,
  connection,
  index,
  taken,
  onClose,
}: {
  operationId: string
  factory: string
  channel: ChannelKind
  channelIcon?: string
  connection: ConnectionDraft
  index: number
  taken: readonly string[]
  onClose: () => void
}) {
  const create = useCreateChannelEndpoint()
  const [draft] = useState(() => endpointFromConnection(connection, index, channel, taken))
  const [name, setName] = useState(draft.name)
  const [description, setDescription] = useState(draft.description)
  const [submitted, setSubmitted] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const nameErr = nameError(name) ?? (taken.includes(name.trim()) ? "This operation already has an endpoint with this name" : null)
  const blocked = draft.problems.length > 0

  async function save() {
    setSubmitted(true)
    setError(null)
    if (nameErr || blocked || create.isPending) return
    try {
      await create.mutateAsync({
        operationId,
        moduleName: channel.name,
        input: { name: name.trim(), description: description.trim() || null, values: draft.values },
      })
      toast.success(`Saved as endpoint ${name.trim()}`)
      onClose()
    } catch (err) {
      const problems = problemsFromError(err)
      const fields = Object.entries(problems)
        .filter(([key]) => key !== "")
        .map(([key, message]) => `${channel.fields.find((f) => f.key === key)?.label ?? key}: ${message}`)
      setError(problems[""] ?? (fields.length > 0 ? fields.join("; ") : errorMessage(err, "Failed to save the endpoint")))
    }
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Save connection {index + 1} as an endpoint</DialogTitle>
        <DialogDescription>
          Its settings become a new endpoint of this operation, to fill connections from later. They are copied: changing
          the endpoint later changes nothing here. The {factory} parameters stay with the build.
        </DialogDescription>
      </DialogHeader>
      <KindName type="channel" name={channel.name} icon={channelIcon} className="text-sm" />
      {blocked && (
        <div className="rounded-md bg-amber-500/10 p-3 text-sm text-amber-800 dark:text-amber-300">
          <p>An endpoint needs every required setting, valid. Fix these in the connection first:</p>
          <ul className="mt-1 list-inside list-disc">
            {draft.problems.map((p) => (
              <li key={p.label}>
                {p.label}: {p.message}
              </li>
            ))}
          </ul>
        </div>
      )}
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="save-endpoint-name">Name</Label>
        <Input
          id="save-endpoint-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            // No form here: the dialog sits inside the build form's React tree,
            // where a submit would bubble up and start the build.
            if (e.key === "Enter") {
              e.preventDefault()
              void save()
            }
          }}
          autoComplete="off"
          spellCheck={false}
          disabled={blocked}
          aria-invalid={(submitted && !!nameErr) || undefined}
        />
        {submitted && nameErr ? (
          <p className="text-sm text-destructive">{nameErr}</p>
        ) : (
          <p className="text-xs text-muted-foreground">Unique in this operation: lowercase letters, digits, '.', '_' and '-'.</p>
        )}
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="save-endpoint-description">Description</Label>
        <Textarea
          id="save-endpoint-description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={2}
          maxLength={500}
          disabled={blocked}
        />
      </div>
      {error && <div className="rounded-md bg-destructive/15 p-3 text-sm text-destructive">{error}</div>}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button type="button" onClick={() => void save()} disabled={blocked || create.isPending}>
          {create.isPending ? "Saving..." : "Save endpoint"}
        </Button>
      </DialogFooter>
    </>
  )
}
