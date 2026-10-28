import { SettingsSection } from "@/components/ui/settings-rows"
import { TransferList, type TransferItem } from "@/components/ui/transfer-list"
import { DEFAULT_GROUP } from "@/components/modules/management/field-values"
import type { Command } from "@/components/builds/build-values"

/**
 * The build form's commands: what the minion will be able to do, and nothing
 * else. The target's commands to pick from, and the ones picked, in two lists
 * under the factory's headings. A new build starts with the target's default
 * and required ones picked; the required ones cannot be taken out. onChange
 * gets the picks as they are: the form keeps them to the target's.
 */
export function CommandPicker({
  id,
  title,
  commands,
  picked,
  defaults,
  onChange,
  error,
  disabled,
}: {
  /** The section's id, for the side list to scroll to. */
  id: string
  title: string
  /** The target's commands, in the factory's order. */
  commands: readonly Command[]
  picked: readonly string[]
  /** What a new build of the target starts with. */
  defaults: readonly string[]
  onChange: (picked: string[]) => void
  error?: string
  disabled: boolean
}) {
  const items = commands.map(
    (c): TransferItem => ({
      id: c.id,
      label: c.label,
      hint: c.id,
      description: [c.description, c.params.length > 0 ? `Takes ${c.params.map((f) => f.key).join(", ")}.` : ""]
        .filter(Boolean)
        .join("\n"),
      group: c.group.trim() || DEFAULT_GROUP,
      locked: c.required,
    }),
  )
  return (
    <SettingsSection id={id} title={title}>
      <div className="flex flex-col gap-3 py-3">
        <p className="text-sm text-muted-foreground">
          {commands.length === 0
            ? "This target has no commands."
            : "The minion can do the selected commands and nothing else. The locked ones are built into every minion of the target."}
        </p>
        {commands.length > 0 && (
          <TransferList
            items={items}
            selected={picked}
            onChange={onChange}
            defaults={defaults}
            titles={{ available: "Available", selected: "Selected" }}
            lockedNote="Built into every minion of the target"
            searchPlaceholder="Search commands…"
            empty={{ available: "Every command is selected.", selected: "No commands selected." }}
            invalid={!!error}
            disabled={disabled}
          />
        )}
        {error && <p className="text-sm text-destructive">{error}</p>}
      </div>
    </SettingsSection>
  )
}
