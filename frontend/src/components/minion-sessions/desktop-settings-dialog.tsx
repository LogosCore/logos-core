import { CommandFormDialog } from "@/components/command-form/command-form-dialog"
import { errorMessage } from "@/components/modules/management/api-errors"
import { useMinionDesktopSettings } from "@/graphql/hooks/minion-tasks"
import { desktopName, type DesktopSettings } from "./desktop-values"

// What a desktop opens with, picked each time one opens (docs:
// contracts/minion-messages/tunnels.md, Desktops): its front's settings,
// as core defines them, each at its default until changed. The tunnel
// keeps its server and its login; Reconnect opens another session with
// the settings the last one had. Mounted closed, it reads the settings
// before it is opened.

export interface DesktopSettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  taskId: string
  /** The desktop's front: rdp, vnc, ssh or telnet. */
  front: string
  /** Opens it with these settings. */
  onOpen: (settings: DesktopSettings) => void
}

export function DesktopSettingsDialog({ open, onOpenChange, taskId, front, onOpen }: DesktopSettingsDialogProps) {
  const q = useMinionDesktopSettings(front)
  const fields = q.data?.minionDesktopSettings ?? []
  return (
    <CommandFormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Open the ${desktopName(front)} desktop`}
      description="How this session draws it: each desktop opened is a session of its own, by its own settings."
      before={
        q.isPending ? (
          <p className="text-sm text-muted-foreground">Reading the settings…</p>
        ) : q.isError ? (
          <p className="text-sm text-destructive">
            {errorMessage(q.error, "Could not read the settings")}: it opens with core&apos;s defaults.
          </p>
        ) : null
      }
      fields={fields}
      // The form starts over once the fields come, at their defaults.
      formKey={q.data ? "settings" : "none"}
      submitLabel="Open"
      idPrefix={`desktop-settings-${taskId}`}
      onSubmit={(values) => onOpen(values)}
    />
  )
}
